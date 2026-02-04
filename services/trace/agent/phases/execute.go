// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/grounding"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/integration"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/routing"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// executePhaseTracer is the OpenTelemetry tracer for the execute phase.
var executePhaseTracer = otel.Tracer("aleutian.agent.execute")

// ucb1SessionContexts stores per-session UCB1 contexts.
// Key is session ID, value is *ucb1SelectionContext.
var ucb1SessionContexts = &sync.Map{}

// getUCB1Context returns or creates the UCB1 context for a session.
func getUCB1Context(sessionID string) *ucb1SelectionContext {
	if ctx, ok := ucb1SessionContexts.Load(sessionID); ok {
		return ctx.(*ucb1SelectionContext)
	}
	newCtx := initUCB1Context()
	actual, _ := ucb1SessionContexts.LoadOrStore(sessionID, newCtx)
	return actual.(*ucb1SelectionContext)
}

// cleanupUCB1Context removes the UCB1 context for a session.
// Call this when a session ends.
func cleanupUCB1Context(sessionID string) {
	ucb1SessionContexts.Delete(sessionID)
}

// CleanupUCB1Context removes the UCB1 context for a session.
// This is the exported version for use by the agent loop during session cleanup.
//
// Description:
//
//	Removes all UCB1-related state (scorer, selection counts, cache, key builder)
//	for the specified session. Should be called when a session is definitively
//	closed (not just completed, as sessions can be continued with follow-up questions).
//
// Inputs:
//
//	sessionID - The session ID to clean up.
//
// Thread Safety: Safe for concurrent use.
func CleanupUCB1Context(sessionID string) {
	cleanupUCB1Context(sessionID)
}

// init registers the UCB1 cleanup hook with the agent loop.
// This ensures UCB1 session state is cleaned up when sessions are closed.
func init() {
	agent.RegisterSessionCleanupHook("ucb1", cleanupUCB1Context)
}

// ExecutePhase handles the main tool execution loop.
//
// This phase is responsible for:
//   - Sending requests to the LLM with current context
//   - Parsing and executing tool calls from LLM responses
//   - Running safety checks on proposed changes
//   - Updating context with tool results
//   - Forcing tool usage for analytical queries
//   - Determining when to reflect or complete
//
// Thread Safety: ExecutePhase is safe for concurrent use.
type ExecutePhase struct {
	// maxTokens is the maximum tokens for LLM responses.
	maxTokens int

	// reflectionThreshold triggers reflection after this many steps.
	reflectionThreshold int

	// requireSafetyCheck requires safety checks for modifications.
	requireSafetyCheck bool

	// maxGroundingRetries is the max retries on grounding failures (circuit breaker).
	maxGroundingRetries int

	// forcingPolicy determines when to force tool usage.
	forcingPolicy ToolForcingPolicy

	// maxToolForcingRetries limits tool forcing attempts (circuit breaker).
	maxToolForcingRetries int

	// maxStepForForcing is the maximum step number where tool forcing applies.
	maxStepForForcing int

	// toolChoiceSelector selects tool_choice based on query classification.
	// This enables API-level tool forcing rather than prompt-only.
	toolChoiceSelector *classifier.ToolChoiceSelector

	// responseValidator validates LLM responses for quality.
	// Checks for prohibited patterns and tool call requirements.
	responseValidator *classifier.RetryableValidator

	// qualityValidator validates response quality (hedging, citations).
	// Checks for evidence-based claims in analytical responses.
	qualityValidator *classifier.QualityValidator
}

// ExecutePhaseOption configures an ExecutePhase.
type ExecutePhaseOption func(*ExecutePhase)

// WithMaxTokens sets the maximum response tokens.
//
// Inputs:
//
//	tokens - Maximum token count.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithMaxTokens(tokens int) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.maxTokens = tokens
	}
}

// WithReflectionThreshold sets when to trigger reflection.
//
// Inputs:
//
//	steps - Number of steps before reflection.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithReflectionThreshold(steps int) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.reflectionThreshold = steps
	}
}

// WithSafetyCheck enables or disables safety checks.
//
// Inputs:
//
//	required - Whether safety checks are required.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithSafetyCheck(required bool) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.requireSafetyCheck = required
	}
}

// WithMaxGroundingRetries sets the maximum grounding validation retries.
//
// Inputs:
//
//	retries - Maximum retry count (circuit breaker threshold).
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithMaxGroundingRetries(retries int) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.maxGroundingRetries = retries
	}
}

// WithToolForcingPolicy sets the policy for forcing tool usage.
//
// Inputs:
//
//	policy - The tool forcing policy. If nil, forcing is disabled.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithToolForcingPolicy(policy ToolForcingPolicy) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.forcingPolicy = policy
	}
}

// WithMaxToolForcingRetries sets the maximum tool forcing retries.
//
// Inputs:
//
//	retries - Maximum retry count (circuit breaker threshold).
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithMaxToolForcingRetries(retries int) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.maxToolForcingRetries = retries
	}
}

// WithMaxStepForForcing sets the maximum step for tool forcing.
//
// Inputs:
//
//	step - Maximum step number where forcing applies.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
func WithMaxStepForForcing(step int) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		p.maxStepForForcing = step
	}
}

// WithQueryClassifier sets the query classifier for both tool forcing and
// tool choice selection.
//
// Description:
//
//	Sets a custom QueryClassifier that will be used by both the forcing
//	policy (to determine when to force tool usage) and the tool choice
//	selector (to select API-level tool_choice parameter).
//
//	This enables using the LLMClassifier instead of the default RegexClassifier
//	for more accurate query classification.
//
// Inputs:
//
//	c - The query classifier to use. If nil, defaults to RegexClassifier.
//
// Outputs:
//
//	ExecutePhaseOption - The configuration function.
//
// Example:
//
//	// Use LLM-based classifier
//	llmClassifier, _ := classifier.NewLLMClassifier(client, toolDefs, config)
//	phase := NewExecutePhase(WithQueryClassifier(llmClassifier))
//
// Thread Safety: The option is safe for concurrent use if the classifier is.
func WithQueryClassifier(c classifier.QueryClassifier) ExecutePhaseOption {
	return func(p *ExecutePhase) {
		if c == nil {
			c = classifier.NewRegexClassifier()
		}
		// Update both components to use the same classifier
		p.forcingPolicy = NewDefaultForcingPolicyWithClassifier(c)
		p.toolChoiceSelector = classifier.NewToolChoiceSelector(c, nil)
	}
}

// NewExecutePhase creates a new execution phase.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*ExecutePhase - The configured phase.
func NewExecutePhase(opts ...ExecutePhaseOption) *ExecutePhase {
	// Create classifier for hybrid stack
	queryClassifier := classifier.NewRegexClassifier()

	p := &ExecutePhase{
		maxTokens:             4096,
		reflectionThreshold:   10,
		requireSafetyCheck:    true,
		maxGroundingRetries:   3, // Circuit breaker for hallucination retries
		forcingPolicy:         NewDefaultForcingPolicy(),
		maxToolForcingRetries: 2, // Circuit breaker for tool forcing
		maxStepForForcing:     2, // Only force on early steps
		// Hybrid stack components
		toolChoiceSelector: classifier.NewToolChoiceSelector(queryClassifier, nil),
		responseValidator:  classifier.NewRetryableValidator(2), // Max 2 retries
		qualityValidator:   classifier.NewQualityValidator(nil), // Default config
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Name implements Phase.
//
// Outputs:
//
//	string - "execute"
func (p *ExecutePhase) Name() string {
	return "execute"
}

// Execute implements Phase.
//
// Description:
//
//	Runs a single step of the execution loop:
//	1. Build prompt with current context
//	2. Call LLM for response
//	3. Parse tool calls from response
//	4. For each tool call: run safety check, execute tool
//	5. Update context with results
//	6. Check if reflection is needed
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	deps - Phase dependencies.
//
// Outputs:
//
//	agent.AgentState - Next state (EXECUTE, REFLECT, COMPLETE, or ERROR).
//	error - Non-nil only for unrecoverable errors.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ExecutePhase) Execute(ctx context.Context, deps *Dependencies) (agent.AgentState, error) {
	if err := p.validateDependencies(deps); err != nil {
		slog.Error("ExecutePhase validation failed", slog.String("error", err.Error()))
		return agent.StateError, err
	}

	slog.Info("ExecutePhase starting",
		slog.String("session_id", deps.Session.ID),
		slog.String("query", deps.Query),
	)

	stepStart := time.Now()
	stepNumber := 0
	if deps.EventEmitter != nil {
		stepNumber = deps.EventEmitter.IncrementStep()
	}

	slog.Info("Building LLM request",
		slog.String("session_id", deps.Session.ID),
		slog.Int("step", stepNumber),
	)

	// Build the LLM request
	request, hardForcing := p.buildLLMRequest(deps)

	// Check if hard forcing is enabled (router selected a real tool with high confidence)
	if hardForcing != nil {
		// TR-2 Fix: Execute tool directly with full observability
		slog.Info("Router hard-forcing tool, attempting direct execution (CB-31d)",
			slog.String("session_id", deps.Session.ID),
			slog.String("forced_tool", hardForcing.Tool),
		)

		// Get tool definitions for parameter extraction
		var toolDefs []tools.ToolDefinition
		if deps.ToolRegistry != nil {
			toolDefs = deps.ToolRegistry.GetDefinitions()
		}

		// TR-1 Fix: Extract tool parameters from query/context
		params, paramErr := p.extractToolParameters(deps.Query, hardForcing.Tool, toolDefs, deps.Context)
		if paramErr != nil {
			// TR-7 Fix: Fallback to Main LLM on parameter extraction failure
			slog.Warn("Parameter extraction failed, falling back to Main LLM (CB-31d)",
				slog.String("tool", hardForcing.Tool),
				slog.String("error", paramErr.Error()),
			)
			grounding.RecordRouterFallback(hardForcing.Tool, "param_extraction_failed")
			// Continue with normal LLM flow using ToolChoiceRequired
			request.ToolChoice = llm.ToolChoiceRequired(hardForcing.Tool)
		} else {
			execResult, execErr := p.executeToolDirectlyWithFallback(ctx, deps, hardForcing.Tool, params, toolDefs)
			if execErr != nil {
				// TR-3 Fix: Fallback to Main LLM if direct execution fails
				slog.Warn("Hard-forced tool execution failed, falling back to Main LLM (CB-31d)",
					slog.String("tool", hardForcing.Tool),
					slog.String("error", execErr.Error()),
				)
				grounding.RecordRouterFallback(hardForcing.Tool, "execution_failed")
				// Continue with normal LLM flow
				request.ToolChoice = llm.ToolChoiceRequired(hardForcing.Tool)
			} else {
				// Success! Tool executed directly - return early
				grounding.RecordRouterHardForced(hardForcing.Tool, true)
				p.emitToolRouting(deps, hardForcing)

				// Convert PhaseResult to state and return
				return execResult.NextState, nil
			}
		}
	}

	// Send request to LLM
	slog.Info("Sending LLM request",
		slog.String("session_id", deps.Session.ID),
		slog.Int("max_tokens", request.MaxTokens),
		slog.Int("tool_count", len(request.Tools)),
	)

	response, err := p.callLLM(ctx, deps, request)
	if err != nil {
		slog.Error("LLM request failed",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		return p.handleLLMError(deps, err)
	}

	slog.Info("LLM response received",
		slog.String("session_id", deps.Session.ID),
		slog.Int("output_tokens", response.OutputTokens),
		slog.Bool("has_tool_calls", response.HasToolCalls()),
		slog.Int("content_len", len(response.Content)),
	)

	// TR-5 Fix: Circuit Breaker "Desperation" Trap
	// Check if circuit breaker was active and LLM tried to escape constraint
	// by outputting tool calls in its text response.
	if !response.HasToolCalls() {
		// Check if this was a circuit breaker forced answer by looking at the request
		circuitBreakerActive := request.ToolChoice != nil && request.ToolChoice.Type == "none" && request.Tools == nil
		if circuitBreakerActive && containsToolCallPattern(response.Content) {
			// LLM escaped the constraint! It output tool calls in text despite ToolChoiceNone()
			slog.Warn("Circuit breaker desperation trap: LLM escaped ToolChoiceNone() constraint (CB-31d)",
				slog.String("session_id", deps.Session.ID),
				slog.Int("response_len", len(response.Content)),
				slog.String("response_preview", truncateForLog(response.Content, 200)),
			)

			// Retry with stronger prompt that explicitly forbids tool calls
			return p.retryDesperationWithStrongerPrompt(ctx, deps, stepStart, stepNumber)
		}

		// Normal completion - no circuit breaker or no escaped patterns
		slog.Info("No tool calls, completing",
			slog.String("session_id", deps.Session.ID),
		)

		return p.handleCompletion(ctx, deps, response, stepStart, stepNumber)
	}

	// Parse and execute tool calls
	invocations := llm.ParseToolCalls(response)

	// CRITICAL: Add assistant message with tool calls to conversation history BEFORE execution.
	// This ensures the LLM sees "I requested tool X" followed by "tool X returned Y".
	// Without this, tool results become orphaned and the LLM keeps re-requesting the same tool.
	p.addAssistantToolCallToHistory(deps, response, invocations)

	toolResults, blocked := p.executeToolCalls(ctx, deps, invocations)

	// Update context with results
	p.updateContextWithResults(ctx, deps, toolResults)

	// Handle safety block
	if blocked {
		p.emitError(deps, fmt.Errorf("execution blocked by safety check"), true)
	}

	// Emit step complete event
	p.emitStepComplete(deps, stepStart, stepNumber, len(invocations))

	// Check if reflection is needed
	if p.shouldReflect(deps, stepNumber) {
		p.emitStateTransition(deps, agent.StateExecute, agent.StateReflect, "reflection threshold reached")
		return agent.StateReflect, nil
	}

	// Continue execution
	p.emitStateTransition(deps, agent.StateExecute, agent.StateExecute, "continuing execution")
	return agent.StateExecute, nil
}

// validateDependencies checks that required dependencies are present.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	error - Non-nil if dependencies are missing.
func (p *ExecutePhase) validateDependencies(deps *Dependencies) error {
	if deps == nil {
		return fmt.Errorf("dependencies are nil")
	}
	if deps.Session == nil {
		return fmt.Errorf("session is nil")
	}
	if deps.Context == nil {
		return fmt.Errorf("context is nil")
	}
	if deps.LLMClient == nil {
		return fmt.Errorf("LLM client is nil")
	}
	// ToolExecutor is optional - if nil, we skip tool execution
	return nil
}

// buildLLMRequest creates an LLM request from current context.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	*llm.Request - The LLM request.
//	*agent.ToolRouterSelection - Non-nil if hard forcing is enabled.
func (p *ExecutePhase) buildLLMRequest(deps *Dependencies) (*llm.Request, *agent.ToolRouterSelection) {
	// Get available tools
	var toolDefs []tools.ToolDefinition
	var toolNames []string
	if deps.ToolRegistry != nil {
		toolDefs = deps.ToolRegistry.GetDefinitions()
		toolNames = make([]string, len(toolDefs))
		for i, def := range toolDefs {
			toolNames[i] = def.Name
		}
	}

	request := llm.BuildRequest(deps.Context, toolDefs, p.maxTokens)

	// History-Aware Routing: Route on EVERY step with full context.
	// The router (Granite4:3b-h with Mamba2 architecture) handles O(n) linear
	// complexity and can efficiently process tool history to avoid the
	// "Amnesiac Router" bug where it keeps suggesting the same tool.
	//
	// Key insight: Mamba2's 1M token context window and linear complexity
	// means we can pass full tool history without performance penalty.
	routerUsed := false

	// CB-31d: Detailed logging for router usage decision
	sessionExists := deps.Session != nil
	routerEnabled := sessionExists && deps.Session.IsToolRouterEnabled()
	hasQuery := deps.Query != ""
	hasTools := len(toolDefs) > 0

	slog.Info("CB-31d Router decision point",
		slog.Bool("session_exists", sessionExists),
		slog.Bool("router_enabled", routerEnabled),
		slog.Bool("has_query", hasQuery),
		slog.Bool("has_tools", hasTools),
		slog.Int("num_tools", len(toolDefs)),
	)

	if sessionExists && routerEnabled && hasQuery && hasTools {
		router := deps.Session.GetToolRouter()
		slog.Info("CB-31d Router check passed, getting router",
			slog.Bool("router_is_nil", router == nil),
		)
		if router != nil {
			routerSelection := p.tryToolRouterSelection(context.Background(), deps, router, toolDefs)
			if routerSelection != nil {
				// Handle meta-actions vs real tools.
				// "answer" and "clarify" are meta-actions that aren't real tools.
				// Using ToolChoiceRequired("answer") would tell the LLM to call a non-existent
				// tool, which it ignores. Fixed in cb_30a after trace_logs_19 analysis.
				if routerSelection.Tool == "answer" || routerSelection.Tool == "clarify" {
					// Meta-action: disable tool calling to force text response
					request.ToolChoice = llm.ToolChoiceNone()
					// Also remove tools from request to reinforce no-tools directive
					request.Tools = nil
					routerUsed = true

					// Log if circuit breaker forced this answer
					circuitBreakerFired := strings.Contains(routerSelection.Reasoning, "Circuit breaker:")

					// CB-31d: When circuit breaker forces answer, add synthesis instruction
					// to override any previous "call a tool" retry messages in conversation.
					if circuitBreakerFired {
						synthesisPrompt := `You have already gathered information from tools. Now synthesize a complete answer.

DO NOT:
- Say you need to call more tools
- Output tool names
- Say "I'll analyze..." without actually answering

DO:
- Use the information already gathered from previous tool calls
- Provide a direct, comprehensive answer to the user's question
- If information is incomplete, state what you found and what's missing

Answer the user's question now based on the information gathered.`
						request.Messages = append(request.Messages, llm.Message{
							Role:    "user",
							Content: synthesisPrompt,
						})
					}

					slog.Info("Router selected meta-action, disabling tools (cb_30a fix)",
						slog.String("session_id", deps.Session.ID),
						slog.String("meta_action", routerSelection.Tool),
						slog.Float64("confidence", routerSelection.Confidence),
						slog.Duration("routing_duration", routerSelection.Duration),
						slog.String("reasoning", routerSelection.Reasoning),
						slog.Bool("circuit_breaker_fired", circuitBreakerFired),
					)
				} else {
					// Real tool: Mark for HARD FORCING
					// This will be handled in Execute() before LLM call
					slog.Debug("Router selected real tool, marking for hard forcing",
						slog.String("session_id", deps.Session.ID),
						slog.String("tool", routerSelection.Tool),
						slog.Float64("confidence", routerSelection.Confidence),
					)
					// Return request with hard forcing selection
					return request, routerSelection
				}

				// Emit routing event if we didn't exit early
				if routerUsed {
					p.emitToolRouting(deps, routerSelection)
				}
			}
		}
	}

	// Fall back to hybrid stack classifier if router wasn't used or failed.
	if !routerUsed && p.toolChoiceSelector != nil && deps.Query != "" && len(toolDefs) > 0 {
		selection := p.toolChoiceSelector.SelectToolChoice(context.Background(), deps.Query, toolNames)

		// Only set tool_choice for analytical queries
		if selection.IsAnalytical {
			request.ToolChoice = selection.ToolChoice

			slog.Debug("tool_choice selected (fallback classifier)",
				slog.String("session_id", deps.Session.ID),
				slog.String("tool_choice_type", selection.ToolChoice.Type),
				slog.String("suggested_tool", selection.SuggestedTool),
				slog.Float64("confidence", selection.Confidence),
				slog.String("reason", selection.Reason),
			)
		}
	}

	return request, nil
}

// tryToolRouterSelection attempts to get a tool selection from the ToolRouter.
//
// Description:
//
//	Converts tool definitions to ToolSpecs, calls the router, and returns
//	the selection if confidence is above threshold. Returns nil on error
//	or low confidence to allow fallback to other mechanisms.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	router - The ToolRouter to use.
//	toolDefs - Available tool definitions.
//
// Outputs:
//
//	*agent.ToolRouterSelection - The selection if confident, nil otherwise.
func (p *ExecutePhase) tryToolRouterSelection(ctx context.Context, deps *Dependencies, router agent.ToolRouter, toolDefs []tools.ToolDefinition) *agent.ToolRouterSelection {
	slog.Info("CB-31d tryToolRouterSelection CALLED",
		slog.String("session_id", deps.Session.ID),
		slog.Int("num_tool_defs", len(toolDefs)),
		slog.String("router_model", router.Model()),
	)

	ctx, span := executePhaseTracer.Start(ctx, "ExecutePhase.tryToolRouterSelection")
	defer span.End()

	// Convert tool definitions to ToolSpecs for the router
	toolSpecs := toolDefsToSpecs(toolDefs)

	// Build code context for the router
	codeContext := p.buildCodeContext(deps)

	span.SetAttributes(
		attribute.Int("num_tools", len(toolSpecs)),
		attribute.String("query_preview", truncateQuery(deps.Query, 100)),
	)

	// Call the router
	slog.Info("CB-31d tryToolRouterSelection calling router.SelectTool",
		slog.String("session_id", deps.Session.ID),
		slog.Int("num_specs", len(toolSpecs)),
		slog.String("query_preview", truncateQuery(deps.Query, 100)),
	)

	selection, err := router.SelectTool(ctx, deps.Query, toolSpecs, codeContext)
	if err != nil {
		// Log but don't fail - we'll fall back to the classifier
		slog.Warn("CB-31d ToolRouter selection FAILED, falling back to classifier",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		span.SetAttributes(attribute.String("fallback_reason", "router_error"))
		return nil
	}

	slog.Info("CB-31d tryToolRouterSelection router.SelectTool RETURNED",
		slog.String("session_id", deps.Session.ID),
		slog.String("selected_tool", selection.Tool),
		slog.Float64("confidence", selection.Confidence),
	)

	// Check confidence threshold
	threshold := 0.7 // Default
	if deps.Session.Config != nil && deps.Session.Config.ToolRouterConfidence > 0 {
		threshold = deps.Session.Config.ToolRouterConfidence
	}

	if selection.Confidence < threshold {
		slog.Debug("ToolRouter confidence below threshold",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", selection.Tool),
			slog.Float64("confidence", selection.Confidence),
			slog.Float64("threshold", threshold),
		)
		span.SetAttributes(
			attribute.String("fallback_reason", "low_confidence"),
			attribute.Float64("confidence", selection.Confidence),
			attribute.Float64("threshold", threshold),
		)
		return nil
	}

	span.SetAttributes(
		attribute.String("selected_tool", selection.Tool),
		attribute.Float64("confidence", selection.Confidence),
		attribute.Int64("routing_duration_ms", selection.Duration.Milliseconds()),
	)

	// Circuit breaker: Check if the tool path is disproven or has been called too many times.
	// CRS-02: Prefer CRS proof status check when available, fall back to count-based check.
	// This prevents infinite loops where the model ignores tool history and keeps suggesting
	// the same tool. Fixed in cb_30a after trace_logs_18 showed repeated find_entry_points.
	var cbShouldFire bool
	var cbReason string

	// CRS-02: Use proof-based circuit breaker when CRS is available
	if deps.Session != nil && deps.Session.HasCRS() {
		cbResult := deps.Session.GetCRS().CheckCircuitBreaker(deps.Session.ID, selection.Tool)
		cbShouldFire = cbResult.ShouldFire
		cbReason = cbResult.Reason
		slog.Debug("CRS-02 circuit breaker check",
			slog.String("session_id", deps.Session.ID),
			slog.String("suggested_tool", selection.Tool),
			slog.Bool("should_fire", cbShouldFire),
			slog.String("reason", cbReason),
			slog.Uint64("proof_number", cbResult.ProofNumber),
			slog.String("status", cbResult.Status.String()),
		)
	} else if codeContext != nil && codeContext.ToolHistory != nil {
		// CB-31d: Fall back to count-based check when CRS not available
		callCount := countToolCalls(codeContext.ToolHistory, selection.Tool)
		slog.Debug("CB-31d circuit breaker check (legacy)",
			slog.String("session_id", deps.Session.ID),
			slog.String("suggested_tool", selection.Tool),
			slog.Int("call_count", callCount),
			slog.Int("max_allowed", maxRepeatedToolCalls),
			slog.Int("history_size", len(codeContext.ToolHistory)),
		)
		if callCount >= maxRepeatedToolCalls {
			cbShouldFire = true
			cbReason = fmt.Sprintf("%s already called %d times", selection.Tool, callCount)
		}
	}

	if cbShouldFire {
		slog.Warn("Circuit breaker: forcing answer due to proof status",
			slog.String("session_id", deps.Session.ID),
			slog.String("suggested_tool", selection.Tool),
			slog.String("reason", cbReason),
		)
		span.SetAttributes(
			attribute.String("circuit_breaker", "proof_disproven"),
			attribute.String("blocked_tool", selection.Tool),
			attribute.String("cb_reason", cbReason),
		)

		// CRS-04: Learn from circuit breaker activation
		p.learnFromFailure(ctx, deps, crs.FailureEvent{
			SessionID:   deps.Session.ID,
			FailureType: crs.FailureTypeCircuitBreaker,
			Tool:        selection.Tool,
			Source:      crs.SignalSourceHard,
		})

		// CRS-06: Emit EventCircuitBreaker to Coordinator
		p.emitCoordinatorEvent(ctx, deps, integration.EventCircuitBreaker, nil, nil, cbReason)

		// Force "answer" to synthesize a response from gathered information
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.8,
			Reasoning:  fmt.Sprintf("Circuit breaker: %s. Synthesizing answer from gathered information.", cbReason),
			Duration:   selection.Duration,
		}
	}

	// CRS-05: UCB1-enhanced tool selection
	// This replaces the CRS-04 clause check with full UCB1 scoring that includes:
	// - Clause blocking via unit propagation
	// - Proof number penalties
	// - Exploration bonuses for less-used tools
	// - Cache for repeated state lookups
	if selection.Tool != "answer" && deps.Session != nil {
		ucb1Ctx := getUCB1Context(deps.Session.ID)
		availableTools := getAvailableToolNames(toolDefs)

		ucb1Selection, modified := p.selectToolWithUCB1(ctx, deps, ucb1Ctx, selection, availableTools)

		if modified {
			span.SetAttributes(
				attribute.String("ucb1_original_tool", selection.Tool),
				attribute.String("ucb1_selected_tool", ucb1Selection.Tool),
				attribute.Float64("ucb1_score", ucb1Selection.Confidence),
				attribute.Bool("ucb1_modified", true),
			)
		} else {
			span.SetAttributes(
				attribute.String("ucb1_selected_tool", ucb1Selection.Tool),
				attribute.Float64("ucb1_score", ucb1Selection.Confidence),
				attribute.Bool("ucb1_modified", false),
			)
		}

		return ucb1Selection
	}

	return selection
}

// toolDefsToSpecs converts tool.ToolDefinition slice to agent.ToolRouterSpec slice.
//
// Inputs:
//
//	defs - Tool definitions to convert.
//
// Outputs:
//
//	[]agent.ToolRouterSpec - Converted tool specs.
func toolDefsToSpecs(defs []tools.ToolDefinition) []agent.ToolRouterSpec {
	specs := make([]agent.ToolRouterSpec, len(defs))
	for i, def := range defs {
		// Extract parameter names from the Parameters map
		var params []string
		if def.Parameters != nil {
			params = make([]string, 0, len(def.Parameters))
			for name := range def.Parameters {
				params = append(params, name)
			}
		}

		specs[i] = agent.ToolRouterSpec{
			Name:        def.Name,
			Description: def.Description,
			Params:      params,
			Category:    def.Category.String(),
		}
	}
	return specs
}

// buildCodeContext creates a CodeContext from phase dependencies.
//
// Description:
//
//	Builds a rich CodeContext for history-aware routing. Includes full tool
//	history with summaries to leverage Mamba2's long-context efficiency.
//	The router can see what tools were already called and what they found,
//	enabling it to suggest the NEXT logical tool rather than repeating.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	*agent.ToolRouterCodeContext - Context for the router.
func (p *ExecutePhase) buildCodeContext(deps *Dependencies) *agent.ToolRouterCodeContext {
	ctx := &agent.ToolRouterCodeContext{}

	// Extract info from assembled context if available
	if deps.Context != nil {
		ctx.Files = len(deps.Context.CodeContext)
		ctx.Symbols = countSymbolsInContext(deps.Context)

		// Detect language from code entries
		ctx.Language = detectLanguageFromContext(deps.Context)
	}

	// Get recent tools from session history if available (legacy support)
	ctx.RecentTools = getRecentToolsFromSession(deps.Session)

	// Get recent tool errors for router feedback
	if deps.Session != nil {
		ctx.PreviousErrors = deps.Session.GetRecentToolErrors()

		// Build tool history with summaries for history-aware routing
		ctx.ToolHistory = buildToolHistoryFromSession(deps.Session)

		// Build progress summary
		ctx.Progress = buildProgressSummary(deps.Session)

		// Set step number
		if deps.EventEmitter != nil {
			ctx.StepNumber = deps.EventEmitter.CurrentStep()
		}
	}

	return ctx
}

// maxToolHistoryEntries limits the number of tool history entries passed to the router.
// This keeps context manageable while still providing sufficient history for
// the router to make informed decisions about the next tool.
const maxToolHistoryEntries = 10

// maxRepeatedToolCalls is the circuit breaker threshold for repeated tool suggestions.
// If the router suggests a tool that has already been called this many times,
// we force selection of "answer" to synthesize from gathered information.
// This prevents infinite loops where the model ignores tool history.
// Fixed in cb_30a after trace_logs_18 showed 5+ repeated find_entry_points calls.
//
// NOTE: This must match crs.DefaultCircuitBreakerThreshold for consistent behavior.
const maxRepeatedToolCalls = crs.DefaultCircuitBreakerThreshold

// countToolCalls counts how many times a specific tool appears in the history.
//
// Inputs:
//
//	history - Tool history entries.
//	toolName - The tool name to count.
//
// Outputs:
//
//	int - Number of times the tool was called.
func countToolCalls(history []agent.ToolHistoryEntry, toolName string) int {
	count := 0
	for _, entry := range history {
		if entry.Tool == toolName {
			count++
		}
	}
	return count
}

// categorizeToolError maps error messages to ErrorCategory for CDCL learning.
//
// Description:
//
//	Analyzes error messages to categorize them for clause generation.
//	This enables more specific clauses that block tool+error combinations.
//
// Inputs:
//
//	errMsg - The error message from tool execution.
//
// Outputs:
//
//	crs.ErrorCategory - The error category.
func categorizeToolError(errMsg string) crs.ErrorCategory {
	errLower := strings.ToLower(errMsg)

	switch {
	case strings.Contains(errLower, "not found") ||
		strings.Contains(errLower, "no such file") ||
		strings.Contains(errLower, "does not exist") ||
		strings.Contains(errLower, "enoent"): // CR-8: Unix errno
		return crs.ErrorCategoryToolNotFound

	case strings.Contains(errLower, "invalid param") ||
		strings.Contains(errLower, "invalid argument") ||
		strings.Contains(errLower, "missing required") ||
		strings.Contains(errLower, "einval"): // CR-8: Unix errno
		return crs.ErrorCategoryInvalidParams

	case strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "timed out") ||
		strings.Contains(errLower, "deadline") ||
		strings.Contains(errLower, "i/o timeout") || // CR-8: Go net timeout
		strings.Contains(errLower, "context deadline"): // CR-8: Context timeout
		return crs.ErrorCategoryTimeout

	case strings.Contains(errLower, "rate limit") ||
		strings.Contains(errLower, "too many requests") ||
		strings.Contains(errLower, "429"): // CR-8: HTTP status
		return crs.ErrorCategoryRateLimited

	case strings.Contains(errLower, "permission") ||
		strings.Contains(errLower, "access denied") ||
		strings.Contains(errLower, "forbidden") ||
		strings.Contains(errLower, "eperm") || // CR-8: Unix errno
		strings.Contains(errLower, "eacces"): // CR-8: Unix errno
		return crs.ErrorCategoryPermission

	case strings.Contains(errLower, "network") ||
		strings.Contains(errLower, "connection") ||
		strings.Contains(errLower, "eof") || // CR-8: Unexpected EOF
		strings.Contains(errLower, "broken pipe") || // CR-8: Unix
		strings.Contains(errLower, "reset by peer"): // CR-8: TCP reset
		return crs.ErrorCategoryNetwork

	default:
		return crs.ErrorCategoryInternal
	}
}

// buildToolHistoryFromSession extracts tool history with summaries from session.
//
// Description:
//
//	Iterates through the session's trace steps and builds a history of
//	tool calls with brief summaries of what each tool found. This enables
//	history-aware routing where the router can see what was already tried.
//
// Inputs:
//
//	s - The session to extract history from.
//
// Outputs:
//
//	[]agent.ToolHistoryEntry - Tool history with summaries.
func buildToolHistoryFromSession(s *agent.Session) []agent.ToolHistoryEntry {
	if s == nil {
		return nil
	}

	traceSteps := s.GetTraceSteps()
	if len(traceSteps) == 0 {
		return nil
	}

	var history []agent.ToolHistoryEntry
	stepNum := 0

	for _, step := range traceSteps {
		// Include both tool_call and tool_call_forced actions.
		// CB-31d fix: tool_call_forced was not being counted, so circuit breaker
		// never detected repeated forced tool calls from the router.
		if step.Action != "tool_call" && step.Action != "tool_call_forced" {
			continue
		}

		stepNum++
		entry := agent.ToolHistoryEntry{
			Tool:       step.Tool,
			Success:    step.Error == "",
			StepNumber: stepNum,
		}

		// Build summary based on tool type and results
		entry.Summary = buildToolSummary(step)

		history = append(history, entry)
	}

	// Limit to last N entries to keep context manageable
	if len(history) > maxToolHistoryEntries {
		history = history[len(history)-maxToolHistoryEntries:]
	}

	return history
}

// buildToolSummary creates a brief summary of what a tool call found.
//
// Inputs:
//
//	step - The trace step for the tool call.
//
// Outputs:
//
//	string - Brief summary of the result.
func buildToolSummary(step crs.TraceStep) string {
	if step.Error != "" {
		return "FAILED: " + truncateString(step.Error, 50)
	}

	// Extract summary from metadata if available
	if summary, ok := step.Metadata["summary"]; ok && summary != "" {
		return truncateString(summary, 100)
	}

	// Build summary based on symbols found
	if len(step.SymbolsFound) > 0 {
		return fmt.Sprintf("Found %d symbols", len(step.SymbolsFound))
	}

	// Default to a generic success message with target
	if step.Target != "" {
		return "Processed " + truncateString(step.Target, 50)
	}

	return "Completed successfully"
}

// buildProgressSummary creates a summary of current progress.
//
// Inputs:
//
//	s - The session to summarize.
//
// Outputs:
//
//	string - Progress summary.
func buildProgressSummary(s *agent.Session) string {
	if s == nil {
		return ""
	}

	traceSteps := s.GetTraceSteps()
	if len(traceSteps) == 0 {
		return "No tools called yet"
	}

	// Count tools by category
	toolCounts := make(map[string]int)
	toolOrder := make([]string, 0) // Track insertion order for deterministic output
	totalSymbols := 0

	for _, step := range traceSteps {
		if step.Action == "tool_call" && step.Error == "" {
			if toolCounts[step.Tool] == 0 {
				toolOrder = append(toolOrder, step.Tool)
			}
			toolCounts[step.Tool]++
			totalSymbols += len(step.SymbolsFound)
		}
	}

	// Build summary in deterministic order
	var parts []string
	for _, tool := range toolOrder {
		parts = append(parts, fmt.Sprintf("%s(%d)", tool, toolCounts[tool]))
	}

	summary := fmt.Sprintf("Tools used: %s", strings.Join(parts, ", "))
	if totalSymbols > 0 {
		summary += fmt.Sprintf("; %d symbols found", totalSymbols)
	}

	return summary
}

// truncateString truncates a string to maxLen with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// countSymbolsInContext counts unique symbols referenced in the context.
func countSymbolsInContext(ctx *agent.AssembledContext) int {
	if ctx == nil {
		return 0
	}
	// Count code entries as a proxy for symbols
	return len(ctx.CodeContext)
}

// detectLanguageFromContext attempts to detect the primary language from context.
func detectLanguageFromContext(ctx *agent.AssembledContext) string {
	if ctx == nil || len(ctx.CodeContext) == 0 {
		return ""
	}

	// Simple heuristic: look at file extensions in the code context
	goCount, pyCount := 0, 0
	for _, entry := range ctx.CodeContext {
		if strings.HasSuffix(entry.FilePath, ".go") {
			goCount++
		} else if strings.HasSuffix(entry.FilePath, ".py") {
			pyCount++
		}
	}

	if goCount > pyCount {
		return "go"
	} else if pyCount > goCount {
		return "python"
	}
	return ""
}

// getRecentToolsFromSession extracts recent tool names from session history.
func getRecentToolsFromSession(s *agent.Session) []string {
	if s == nil {
		return nil
	}

	history := s.GetHistory()
	if len(history) == 0 {
		return nil
	}

	// Get last 5 unique tools
	seen := make(map[string]bool)
	var recent []string
	for i := len(history) - 1; i >= 0 && len(recent) < 5; i-- {
		entry := history[i]
		if entry.Type == "tool_call" && entry.ToolName != "" && !seen[entry.ToolName] {
			seen[entry.ToolName] = true
			recent = append(recent, entry.ToolName)
		}
	}
	return recent
}

// truncateQuery truncates a query string for logging.
func truncateQuery(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// emitToolRouting emits a routing event and records a trace step.
func (p *ExecutePhase) emitToolRouting(deps *Dependencies, selection *agent.ToolRouterSelection) {
	if selection == nil {
		return
	}

	// Emit event if emitter is available
	if deps.EventEmitter != nil {
		deps.EventEmitter.Emit(events.TypeToolForcing, &events.ToolForcingData{
			Query:         deps.Query,
			SuggestedTool: selection.Tool,
			RetryCount:    0,
			MaxRetries:    0,
			StepNumber:    0,
			Reason:        fmt.Sprintf("router_selection (confidence: %.2f, reasoning: %s)", selection.Confidence, selection.Reasoning),
		})
	}

	// Record trace step for routing decision
	if deps.Session != nil {
		traceStep := crs.TraceStep{
			Action:   "tool_routing",
			Target:   selection.Tool,
			Duration: selection.Duration,
			Metadata: map[string]string{
				"confidence": fmt.Sprintf("%.2f", selection.Confidence),
				"reasoning":  selection.Reasoning,
				"query":      truncateQuery(deps.Query, 200),
			},
		}
		deps.Session.RecordTraceStep(traceStep)
	}
}

// callLLM sends a request to the LLM and emits events.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	request - The LLM request.
//
// Outputs:
//
//	*llm.Response - The LLM response.
//	error - Non-nil if the request fails.
func (p *ExecutePhase) callLLM(ctx context.Context, deps *Dependencies, request *llm.Request) (*llm.Response, error) {
	// Emit LLM request event
	p.emitLLMRequest(deps, request)

	// Call LLM
	response, err := deps.LLMClient.Complete(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// Emit LLM response event
	p.emitLLMResponse(deps, response)

	return response, nil
}

// handleLLMError handles LLM request errors.
//
// Description:
//
//	Handles different LLM error types with appropriate recovery strategies.
//	For EmptyResponseError (context overflow), synthesizes a graceful response
//	from gathered tool results instead of failing. Fixed in cb_30a.
//
// Inputs:
//
//	deps - Phase dependencies.
//	err - The LLM error.
//
// Outputs:
//
//	agent.AgentState - ERROR for unrecoverable, COMPLETE if graceful recovery.
//	error - The original error or nil if recovered.
func (p *ExecutePhase) handleLLMError(deps *Dependencies, err error) (agent.AgentState, error) {
	// Check for EmptyResponseError - often caused by context overflow.
	// Instead of failing, synthesize a response from gathered tool results.
	// This provides a graceful degradation when the model is overwhelmed.
	// Fixed in cb_30a after trace_logs_18 showed 23 messages causing empty response.
	var emptyErr *llm.EmptyResponseError
	if errors.As(err, &emptyErr) {
		slog.Warn("Attempting graceful recovery from empty response",
			slog.String("session_id", deps.Session.ID),
			slog.Int("message_count", emptyErr.MessageCount),
			slog.Duration("duration", emptyErr.Duration),
		)

		// Build a summary response from tool results we already have
		summary := p.synthesizeFromToolResults(deps)
		if summary != "" {
			slog.Info("Graceful recovery: synthesized response from tool results",
				slog.String("session_id", deps.Session.ID),
				slog.Int("summary_len", len(summary)),
			)

			// Add synthesized response to conversation history as assistant message
			if deps.Context != nil {
				deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
					Role:    "assistant",
					Content: summary,
				})
				deps.Session.SetCurrentContext(deps.Context)
			}

			p.emitError(deps, fmt.Errorf("recovered from empty response: synthesized from %d tool results", len(deps.Context.ToolResults)), false)
			return agent.StateComplete, nil
		}

		// No tool results to synthesize from - fall through to error
		slog.Warn("Graceful recovery failed: no tool results to synthesize from",
			slog.String("session_id", deps.Session.ID),
		)
	}

	p.emitError(deps, err, false)
	return agent.StateError, err
}

// synthesizeFromToolResults builds a summary response from gathered tool results.
//
// Description:
//
//	When the LLM returns an empty response (often due to context overflow),
//	this function creates a useful response from the tool results already
//	collected. This provides graceful degradation instead of failing.
//
// Inputs:
//
//	deps - Phase dependencies containing tool results.
//
// Outputs:
//
//	string - Synthesized summary, empty if nothing to synthesize.
func (p *ExecutePhase) synthesizeFromToolResults(deps *Dependencies) string {
	if deps.Context == nil || len(deps.Context.ToolResults) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Based on the codebase analysis:\n\n")

	// Summarize tool results
	hasContent := false
	for i, result := range deps.Context.ToolResults {
		if result.Success && result.Output != "" {
			// Truncate very long outputs
			output := result.Output
			if len(output) > 500 {
				output = output[:500] + "..."
			}
			// Use index-based identifier since ToolResult doesn't have ToolName
			sb.WriteString(fmt.Sprintf("**Tool Result %d** (ID: %s):\n%s\n\n", i+1, truncateString(result.InvocationID, 20), output))
			hasContent = true
		}
	}

	if !hasContent {
		return ""
	}

	sb.WriteString("\n*Note: This summary was generated from tool outputs due to context limitations.*")
	return sb.String()
}

// handleCompletion handles LLM responses with no tool calls.
//
// Description:
//
//	Validates the response against grounding rules (anti-hallucination).
//	If critical violations are detected and retries are available, builds
//	a correction prompt and returns EXECUTE to retry. Implements circuit
//	breaker pattern to prevent infinite retry loops.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	response - The LLM response.
//	stepStart - When the step started.
//	stepNumber - The step number.
//
// Outputs:
//
//	agent.AgentState - COMPLETE if grounded, EXECUTE if retry needed.
//	error - Non-nil only for unrecoverable errors.
func (p *ExecutePhase) handleCompletion(ctx context.Context, deps *Dependencies, response *llm.Response, stepStart time.Time, stepNumber int) (agent.AgentState, error) {
	slog.Info("Handling completion",
		slog.String("session_id", deps.Session.ID),
		slog.Int("output_tokens", response.OutputTokens),
		slog.Int("response_len", len(response.Content)),
	)

	// Update session metrics with token usage
	if response.OutputTokens > 0 {
		deps.Session.IncrementMetric(agent.MetricTokens, response.OutputTokens)
	}
	deps.Session.IncrementMetric(agent.MetricLLMCalls, 1)

	// Validate response for prohibited patterns (hybrid stack layer 4)
	if p.responseValidator != nil {
		// Get the tool_choice that was used for this request
		var toolChoice *llm.ToolChoice
		if p.toolChoiceSelector != nil && deps.Query != "" {
			toolNames := p.getToolNames(deps)
			selection := p.toolChoiceSelector.SelectToolChoice(ctx, deps.Query, toolNames)
			if selection.IsAnalytical {
				toolChoice = selection.ToolChoice
			}
		}

		validation := p.responseValidator.Validate(response, toolChoice)
		if !validation.Valid {
			forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

			slog.Warn("Response validation failed",
				slog.String("session_id", deps.Session.ID),
				slog.String("reason", validation.Reason),
				slog.String("pattern", validation.MatchedPattern),
				slog.Bool("retryable", validation.Retryable),
				slog.Int("retry_count", forcingRetries),
			)

			// Check if we should retry with stronger tool_choice
			if validation.Retryable && p.responseValidator.ShouldRetry(validation, forcingRetries) {
				return p.retryWithStrongerToolChoice(ctx, deps, response, validation, stepNumber)
			}
		}
	}

	// Validate response quality (hedging language, citations) for analytical queries
	if p.qualityValidator != nil && response.Content != "" {
		// Determine if this is an analytical query
		isAnalytical := false
		if p.toolChoiceSelector != nil && deps.Query != "" {
			toolNames := p.getToolNames(deps)
			selection := p.toolChoiceSelector.SelectToolChoice(ctx, deps.Query, toolNames)
			isAnalytical = selection.IsAnalytical
		}

		qualityResult := p.qualityValidator.ValidateQuality(response.Content, isAnalytical)
		if !qualityResult.Valid {
			forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

			slog.Warn("Response quality validation failed",
				slog.String("session_id", deps.Session.ID),
				slog.String("reason", qualityResult.Reason),
				slog.String("pattern", qualityResult.MatchedPattern),
				slog.Bool("retryable", qualityResult.Retryable),
				slog.Int("retry_count", forcingRetries),
			)

			// Retry with quality correction if retryable and under limit
			if qualityResult.Retryable && forcingRetries < p.maxToolForcingRetries {
				return p.retryWithQualityCorrection(ctx, deps, response, qualityResult, stepNumber)
			}
		}
	}

	// Check if tool forcing should be applied (before grounding validation)
	if p.shouldForceToolUsage(ctx, deps, stepNumber) {
		return p.forceToolUsage(ctx, deps, response, stepNumber)
	}

	// Validate response against grounding (anti-hallucination)
	responseContent := response.Content
	var groundingResult *grounding.Result

	if deps.ResponseGrounder != nil && deps.Context != nil {
		var err error
		groundingResult, err = deps.ResponseGrounder.Validate(ctx, response.Content, deps.Context)
		if err != nil {
			slog.Warn("Grounding validation error",
				slog.String("session_id", deps.Session.ID),
				slog.String("error", err.Error()),
			)
			// Continue with unvalidated response on error
		}
	}

	// Handle grounding result
	if groundingResult != nil {
		slog.Info("Grounding validation complete",
			slog.String("session_id", deps.Session.ID),
			slog.Bool("grounded", groundingResult.Grounded),
			slog.Float64("confidence", groundingResult.Confidence),
			slog.Int("critical_count", groundingResult.CriticalCount),
			slog.Int("warning_count", groundingResult.WarningCount),
		)

		// Log violations
		for _, v := range groundingResult.Violations {
			slog.Warn("Grounding violation",
				slog.String("session_id", deps.Session.ID),
				slog.String("type", string(v.Type)),
				slog.String("severity", string(v.Severity)),
				slog.String("code", v.Code),
				slog.String("message", v.Message),
			)
		}

		// Check if we should reject and retry
		if deps.ResponseGrounder.ShouldReject(groundingResult) {
			retryCount := deps.Session.GetMetric(agent.MetricGroundingRetries)

			if retryCount < p.maxGroundingRetries {
				// Build correction prompt and retry
				correctionPrompt := p.buildCorrectionPrompt(groundingResult)

				slog.Info("Grounding rejection - requesting retry",
					slog.String("session_id", deps.Session.ID),
					slog.Int("retry_count", retryCount+1),
					slog.Int("max_retries", p.maxGroundingRetries),
				)

				// Add correction as user message to trigger re-generation
				if deps.ContextManager != nil {
					deps.ContextManager.AddMessage(deps.Context, "user", correctionPrompt)
				} else if deps.Context != nil {
					deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
						Role:    "user",
						Content: correctionPrompt,
					})
				}

				deps.Session.IncrementMetric(agent.MetricGroundingRetries, 1)

				// Return to EXECUTE to get a new response
				return agent.StateExecute, nil
			}

			// Circuit breaker triggered - log and continue with best effort
			slog.Error("Grounding circuit breaker triggered - accepting ungrounded response",
				slog.String("session_id", deps.Session.ID),
				slog.Int("retry_count", retryCount),
				slog.Int("critical_violations", groundingResult.CriticalCount),
			)

			// Add warning footnote about potential issues
			responseContent = response.Content + "\n\n---\n⚠️ **Warning**: This response may contain inaccuracies. Please verify code references."
		} else {
			// Response is grounded or only has warnings - add footnote if needed
			footnote := deps.ResponseGrounder.GenerateFootnote(groundingResult)
			if footnote != "" {
				responseContent = response.Content + footnote
			}
		}
	}

	// Add response to context conversation history
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, "assistant", responseContent)
		deps.Session.SetCurrentContext(deps.Context)
	} else if deps.Context != nil {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "assistant",
			Content: responseContent,
		})
		deps.Session.SetCurrentContext(deps.Context)
	}

	// Emit step complete
	p.emitStepComplete(deps, stepStart, stepNumber, 0)

	// Record completion trace step
	if deps.Session != nil {
		completionStep := crs.TraceStep{
			Action:   "complete",
			Target:   "response",
			Duration: time.Since(stepStart),
			Metadata: map[string]string{
				"step_number":   fmt.Sprintf("%d", stepNumber),
				"output_tokens": fmt.Sprintf("%d", response.OutputTokens),
				"response_len":  fmt.Sprintf("%d", len(responseContent)),
			},
		}
		if groundingResult != nil {
			completionStep.Metadata["grounded"] = fmt.Sprintf("%v", groundingResult.Grounded)
			completionStep.Metadata["confidence"] = fmt.Sprintf("%.2f", groundingResult.Confidence)
		}
		deps.Session.RecordTraceStep(completionStep)
	}

	// Transition to complete
	p.emitStateTransition(deps, agent.StateExecute, agent.StateComplete, "task completed")

	slog.Info("ExecutePhase completed successfully",
		slog.String("session_id", deps.Session.ID),
	)

	return agent.StateComplete, nil
}

// buildCorrectionPrompt creates a prompt to correct grounding violations.
func (p *ExecutePhase) buildCorrectionPrompt(result *grounding.Result) string {
	var issues []string
	for _, v := range result.Violations {
		if v.Severity == grounding.SeverityCritical {
			issues = append(issues, v.Message)
		}
	}

	prompt := "Your previous response had grounding issues that need correction:\n\n"
	for i, issue := range issues {
		prompt += fmt.Sprintf("%d. %s\n", i+1, issue)
	}
	prompt += "\nPlease provide a corrected response that:\n"
	prompt += "- Only discusses code that appears in the provided context\n"
	prompt += "- Uses [file:line] citations for specific code references\n"
	prompt += "- Matches the project's programming language\n"
	prompt += "- Says \"I don't see X in the context\" if something is not present\n"

	return prompt
}

// shouldForceToolUsage determines if tool usage should be forced.
//
// Description:
//
//	Checks the tool forcing policy to determine if the LLM should be
//	prompted to use tools instead of returning a text-only response.
//
// Inputs:
//
//	ctx - Context for tracing.
//	deps - Phase dependencies.
//	stepNumber - Current step number.
//
// Outputs:
//
//	bool - True if tool usage should be forced.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ExecutePhase) shouldForceToolUsage(ctx context.Context, deps *Dependencies, stepNumber int) bool {
	// Skip if no forcing policy configured
	if p.forcingPolicy == nil {
		return false
	}

	// Skip if deps is nil (shouldn't happen after validation)
	if deps == nil || deps.Session == nil {
		return false
	}

	// Get current retry count
	forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

	// Build available tools list
	var availableTools []string
	if deps.ToolRegistry != nil {
		defs := deps.ToolRegistry.GetDefinitions()
		availableTools = make([]string, len(defs))
		for i, def := range defs {
			availableTools[i] = def.Name
		}
	}

	// Build forcing request
	req := &ForcingRequest{
		Query:             deps.Query,
		StepNumber:        stepNumber,
		ForcingRetries:    forcingRetries,
		MaxRetries:        p.maxToolForcingRetries,
		MaxStepForForcing: p.maxStepForForcing,
		AvailableTools:    availableTools,
	}

	return p.forcingPolicy.ShouldForce(ctx, req)
}

// forceToolUsage injects a hint and returns to EXECUTE state for retry.
//
// Description:
//
//	When tool forcing is triggered, this method:
//	1. Builds a forcing hint with tool suggestion
//	2. Emits a tool forcing event
//	3. Adds the hint to conversation history
//	4. Increments the forcing retry metric
//	5. Returns StateExecute to retry
//
// Inputs:
//
//	ctx - Context for tracing.
//	deps - Phase dependencies.
//	response - The LLM response that triggered forcing.
//	stepNumber - Current step number.
//
// Outputs:
//
//	agent.AgentState - StateExecute to retry.
//	error - Non-nil only for unrecoverable errors.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ExecutePhase) forceToolUsage(ctx context.Context, deps *Dependencies, response *llm.Response, stepNumber int) (agent.AgentState, error) {
	forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

	slog.Info("Forcing tool usage",
		slog.String("session_id", deps.Session.ID),
		slog.String("query", deps.Query),
		slog.Int("step", stepNumber),
		slog.Int("retry", forcingRetries+1),
	)

	// Build available tools list
	var availableTools []string
	if deps.ToolRegistry != nil {
		defs := deps.ToolRegistry.GetDefinitions()
		availableTools = make([]string, len(defs))
		for i, def := range defs {
			availableTools[i] = def.Name
		}
	}

	// Build forcing request and hint
	req := &ForcingRequest{
		Query:             deps.Query,
		StepNumber:        stepNumber,
		ForcingRetries:    forcingRetries,
		MaxRetries:        p.maxToolForcingRetries,
		MaxStepForForcing: p.maxStepForForcing,
		AvailableTools:    availableTools,
	}

	hint := p.forcingPolicy.BuildHint(ctx, req)

	// Emit tool forcing event
	p.emitToolForcing(deps, req, hint, stepNumber)

	// Add hint to conversation via ContextManager (thread-safe)
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, "user", hint)
	} else if deps.Context != nil {
		// Fallback: direct append (less safe but functional)
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "user",
			Content: hint,
		})
	}

	// Increment forcing retry metric
	deps.Session.IncrementMetric(agent.MetricToolForcingRetries, 1)

	// Return to EXECUTE to retry with the hint
	return agent.StateExecute, nil
}

// retryWithStrongerToolChoice retries with escalated tool_choice after validation failure.
//
// Description:
//
//	When response validation fails (e.g., prohibited patterns detected),
//	this method escalates the tool_choice and retries. The escalation order is:
//	  auto → any → tool (specific)
//
// Inputs:
//
//	ctx - Context for tracing.
//	deps - Phase dependencies.
//	response - The failed LLM response.
//	validation - The validation result.
//	stepNumber - Current step number.
//
// Outputs:
//
//	agent.AgentState - StateExecute to retry.
//	error - Non-nil only for unrecoverable errors.
func (p *ExecutePhase) retryWithStrongerToolChoice(
	ctx context.Context,
	deps *Dependencies,
	response *llm.Response,
	validation classifier.ValidationResult,
	stepNumber int,
) (agent.AgentState, error) {
	forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

	slog.Info("Retrying with stronger tool_choice",
		slog.String("session_id", deps.Session.ID),
		slog.String("validation_reason", validation.Reason),
		slog.Int("retry", forcingRetries+1),
	)

	// Get suggested tool for retry
	var suggestedTool string
	if p.toolChoiceSelector != nil && deps.Query != "" {
		toolNames := p.getToolNames(deps)
		selection := p.toolChoiceSelector.SelectToolChoice(ctx, deps.Query, toolNames)
		suggestedTool = selection.SuggestedTool
	}

	// Get stronger tool_choice for retry
	retryToolChoice := p.responseValidator.GetRetryToolChoice(forcingRetries+1, nil, suggestedTool)

	slog.Info("Escalating tool_choice for retry",
		slog.String("session_id", deps.Session.ID),
		slog.String("new_tool_choice_type", retryToolChoice.Type),
		slog.String("new_tool_choice_name", retryToolChoice.Name),
		slog.String("suggested_tool", suggestedTool),
	)

	// Build correction message for retry
	correctionMsg := fmt.Sprintf(`Your response didn't use tools as required. Please use tools to explore the codebase before answering.

Issue: %s

Call a tool now (suggestion: %s if available), then provide your answer based on what you find.`,
		validation.Reason, suggestedTool)

	// Add correction to conversation
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, "user", correctionMsg)
	} else if deps.Context != nil {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "user",
			Content: correctionMsg,
		})
	}

	// Increment forcing retry metric
	deps.Session.IncrementMetric(agent.MetricToolForcingRetries, 1)

	// Emit tool forcing event
	p.emitToolForcing(deps, &ForcingRequest{
		Query:             deps.Query,
		StepNumber:        stepNumber,
		ForcingRetries:    forcingRetries,
		MaxRetries:        p.maxToolForcingRetries,
		MaxStepForForcing: p.maxStepForForcing,
	}, correctionMsg, stepNumber)

	// Return to EXECUTE to retry
	return agent.StateExecute, nil
}

// retryWithQualityCorrection retries after quality validation failure.
//
// Description:
//
//	When response quality validation fails (e.g., hedging language detected,
//	missing citations), this method adds a correction message and retries.
//
// Inputs:
//
//	ctx - Context for tracing.
//	deps - Phase dependencies.
//	response - The failed LLM response.
//	validation - The quality validation result.
//	stepNumber - Current step number.
//
// Outputs:
//
//	agent.AgentState - StateExecute to retry.
//	error - Non-nil only for unrecoverable errors.
func (p *ExecutePhase) retryWithQualityCorrection(
	ctx context.Context,
	deps *Dependencies,
	response *llm.Response,
	validation classifier.ValidationResult,
	stepNumber int,
) (agent.AgentState, error) {
	forcingRetries := deps.Session.GetMetric(agent.MetricToolForcingRetries)

	slog.Info("Retrying with quality correction",
		slog.String("session_id", deps.Session.ID),
		slog.String("validation_reason", validation.Reason),
		slog.Int("retry", forcingRetries+1),
	)

	// Build correction message based on the quality issue
	var correctionMsg string
	if strings.Contains(validation.Reason, "hedging") {
		correctionMsg = `Your response used hedging language ("likely", "probably", "appears to") for code facts. Please:

1. Replace hedging with specific [file.go:line] citations
2. If you're uncertain, call a tool to verify
3. If something isn't found, say "I don't see X in the context"

BAD: "The system likely uses flags for configuration"
GOOD: "Flags defined in [cmd/main.go:23-38]: -project, -api-key, -verbose"

Please revise your response with specific citations.`
	} else if strings.Contains(validation.Reason, "citation") {
		correctionMsg = `Your response lacked [file:line] citations. For analytical responses, every factual claim about code needs a citation.

Format: [file.go:42] or [file.go:42-50] for ranges

Please revise your response to include specific file and line citations for your claims.`
	} else {
		correctionMsg = fmt.Sprintf(`Your response had a quality issue: %s

Please revise with specific evidence and [file:line] citations.`, validation.Reason)
	}

	// Add correction to conversation
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, "user", correctionMsg)
	} else if deps.Context != nil {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "user",
			Content: correctionMsg,
		})
	}

	// Increment forcing retry metric
	deps.Session.IncrementMetric(agent.MetricToolForcingRetries, 1)

	// Return to EXECUTE to retry
	return agent.StateExecute, nil
}

// emitToolForcing emits a tool forcing event.
func (p *ExecutePhase) emitToolForcing(deps *Dependencies, req *ForcingRequest, hint string, stepNumber int) {
	if deps.EventEmitter == nil {
		return
	}

	// Extract suggested tool from hint (best effort)
	suggestedTool := ""
	if req != nil && len(req.AvailableTools) > 0 {
		// Get suggestion from classifier
		if p.forcingPolicy != nil {
			if dfp, ok := p.forcingPolicy.(*DefaultForcingPolicy); ok {
				suggestedTool, _ = dfp.classifier.SuggestTool(context.Background(), req.Query, req.AvailableTools)
			}
		}
	}

	deps.EventEmitter.Emit(events.TypeToolForcing, &events.ToolForcingData{
		Query:         req.Query,
		SuggestedTool: suggestedTool,
		RetryCount:    req.ForcingRetries + 1,
		MaxRetries:    req.MaxRetries,
		StepNumber:    stepNumber,
		Reason:        "analytical_query_without_tools",
	})
}

// executeToolCalls executes tool invocations with safety checks.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	invocations - Tool invocations to execute.
//
// Outputs:
//
//	[]*tools.Result - Results from tool execution.
//	bool - True if any tool was blocked by safety.
func (p *ExecutePhase) executeToolCalls(ctx context.Context, deps *Dependencies, invocations []agent.ToolInvocation) ([]*tools.Result, bool) {
	results := make([]*tools.Result, 0, len(invocations))
	blocked := false

	for i, inv := range invocations {
		// Refresh graph if dirty files exist (before tool queries stale data)
		p.maybeRefreshGraph(ctx, deps)

		// Emit tool invocation event
		p.emitToolInvocation(deps, &inv)

		// Run safety check if required
		if p.requireSafetyCheck {
			// Generate node ID for CDCL constraint extraction
			nodeID := fmt.Sprintf("tool_%s_%d", inv.Tool, i)
			safetyResult := p.isBlockedBySafety(ctx, deps, &inv, nodeID)

			if safetyResult.Blocked {
				blocked = true
				results = append(results, &tools.Result{
					Success: false,
					Error:   safetyResult.ErrorMessage,
				})
				// Record blocked trace step
				p.recordTraceStep(deps, &inv, nil, 0, safetyResult.ErrorMessage)

				// Record safety violation for CDCL learning (Issue #6)
				// Safety violations are hard signals - CDCL should learn to avoid them
				if deps.Session != nil && len(safetyResult.Constraints) > 0 {
					deps.Session.RecordSafetyViolation(
						nodeID,
						safetyResult.ErrorMessage,
						safetyResult.Constraints,
					)
				}

				// CRS-04: Learn from safety violation
				p.learnFromFailure(ctx, deps, crs.FailureEvent{
					SessionID:    deps.Session.ID,
					FailureType:  crs.FailureTypeSafety,
					Tool:         inv.Tool,
					ErrorMessage: safetyResult.ErrorMessage,
					Source:       crs.SignalSourceSafety,
				})

				// CRS-02: Mark tool path as disproven due to safety violation.
				// Safety violations are hard signals - the path cannot lead to a solution.
				p.markToolDisproven(ctx, deps, &inv, "safety_violation: "+safetyResult.ErrorMessage)
				continue
			}
		}

		// Execute the tool with timing
		toolStart := time.Now()
		result := p.executeSingleTool(ctx, deps, &inv)
		toolDuration := time.Since(toolStart)
		results = append(results, result)

		// Record trace step for this tool call
		errMsg := ""
		if !result.Success {
			errMsg = result.Error
			// Record error for router feedback
			if deps.Session != nil {
				deps.Session.RecordToolError(inv.Tool, errMsg)
			}

			// CRS-04: Learn from tool execution error
			// Determine error category from error message
			errorCategory := categorizeToolError(errMsg)
			p.learnFromFailure(ctx, deps, crs.FailureEvent{
				SessionID:     deps.Session.ID,
				FailureType:   crs.FailureTypeToolError,
				Tool:          inv.Tool,
				ErrorMessage:  errMsg,
				ErrorCategory: errorCategory,
				Source:        crs.SignalSourceHard,
			})

			// CRS-06: Emit EventToolFailed to Coordinator
			p.emitCoordinatorEvent(ctx, deps, integration.EventToolFailed, &inv, result, errMsg)
		} else {
			// CRS-06: Emit EventToolExecuted to Coordinator for successful execution
			p.emitCoordinatorEvent(ctx, deps, integration.EventToolExecuted, &inv, result, "")
		}
		p.recordTraceStep(deps, &inv, result, toolDuration, errMsg)

		// CRS-02: Update proof numbers based on tool execution outcome.
		// Proof number represents COST TO PROVE (lower = better).
		// Success decreases cost (path is viable), failure increases cost.
		p.updateProofNumber(ctx, deps, &inv, result)

		// CRS-03: Check for reasoning cycles after each step.
		// Brent's algorithm detects cycles in O(1) amortized time per step.
		stepNumber := 0
		if deps.Session != nil {
			stepNumber = deps.Session.GetMetric(agent.MetricSteps)
		}
		if cycleDetected, cycleReason := p.checkCycleAfterStep(ctx, deps, &inv, stepNumber, result.Success); cycleDetected {
			// Cycle detected - mark this as a blocked result
			slog.Warn("CRS-03: Cycle triggered circuit breaker",
				slog.String("session_id", deps.Session.ID),
				slog.String("tool", inv.Tool),
				slog.String("reason", cycleReason),
			)

			// CRS-06: Emit EventCycleDetected to Coordinator
			p.emitCoordinatorEvent(ctx, deps, integration.EventCycleDetected, &inv, nil, cycleReason)

			// Continue processing - the cycle states are already marked disproven
			// The circuit breaker will fire on the next tool selection
		}

		// Track file modifications for graph refresh
		p.trackModifiedFiles(deps, result)

		// Emit tool result event
		p.emitToolResult(deps, &inv, result)
	}

	return results, blocked
}

// recordTraceStep records a reasoning trace step for a tool execution.
//
// Inputs:
//
//	deps - Phase dependencies.
//	inv - The tool invocation.
//	result - The tool result (may be nil for blocked calls).
//	duration - How long the tool call took.
//	errMsg - Error message if the call failed.
func (p *ExecutePhase) recordTraceStep(deps *Dependencies, inv *agent.ToolInvocation, result *tools.Result, duration time.Duration, errMsg string) {
	if deps.Session == nil {
		return
	}

	// Build trace step
	step := crs.TraceStep{
		Action:   "tool_call",
		Target:   inv.Tool,
		Tool:     inv.Tool,
		Duration: duration,
		Error:    errMsg,
		Metadata: make(map[string]string),
	}

	// Add tool parameters to metadata (truncated for safety)
	if inv.Parameters != nil {
		// Extract string params
		if inv.Parameters.StringParams != nil {
			for k, v := range inv.Parameters.StringParams {
				if len(v) > 100 {
					v = v[:100] + "..."
				}
				step.Metadata[k] = v
			}
		}
		// Extract int params
		if inv.Parameters.IntParams != nil {
			for k, v := range inv.Parameters.IntParams {
				step.Metadata[k] = fmt.Sprintf("%d", v)
			}
		}
		// Extract bool params
		if inv.Parameters.BoolParams != nil {
			for k, v := range inv.Parameters.BoolParams {
				step.Metadata[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Extract symbols found from result if available
	if result != nil && result.Success {
		step.SymbolsFound = extractSymbolsFromResult(result)
	}

	deps.Session.RecordTraceStep(step)
}

// extractSymbolsFromResult extracts symbol IDs from a tool result.
func extractSymbolsFromResult(result *tools.Result) []string {
	if result == nil || result.Output == nil {
		return nil
	}

	// Try to extract symbols from common result structures
	var symbols []string

	// Check if Output is a map
	outputMap, ok := result.Output.(map[string]interface{})
	if !ok {
		return nil
	}

	// Check for Symbols field (used by many exploration tools)
	if syms, ok := outputMap["symbols"]; ok {
		if symList, ok := syms.([]interface{}); ok {
			for _, s := range symList {
				if symMap, ok := s.(map[string]interface{}); ok {
					if id, ok := symMap["id"].(string); ok {
						symbols = append(symbols, id)
					}
				}
			}
		}
	}

	// Check for EntryPoints field
	if eps, ok := outputMap["entry_points"]; ok {
		if epList, ok := eps.([]interface{}); ok {
			for _, ep := range epList {
				if epMap, ok := ep.(map[string]interface{}); ok {
					if id, ok := epMap["id"].(string); ok {
						symbols = append(symbols, id)
					}
				}
			}
		}
	}

	// Limit to first 20 symbols to avoid huge traces
	if len(symbols) > 20 {
		symbols = symbols[:20]
	}

	return symbols
}

// SafetyCheckResult holds the result of a safety check along with metadata
// for CDCL learning.
type SafetyCheckResult struct {
	// Blocked indicates if the operation should be blocked.
	Blocked bool

	// Result is the full safety check result.
	Result *safety.Result

	// ErrorMessage is the error message for learning.
	ErrorMessage string

	// Constraints are the extracted constraints for CDCL.
	Constraints []safety.SafetyConstraint
}

// isBlockedBySafety checks if a tool invocation should be blocked.
//
// Description:
//
//	Performs safety check and extracts constraints for CDCL learning.
//	Safety violations are classified as hard signals so CDCL can learn
//	to avoid patterns that trigger safety blocks.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	inv - The tool invocation.
//	nodeID - The MCTS node ID for constraint extraction.
//
// Outputs:
//
//	*SafetyCheckResult - The check result with learning metadata.
func (p *ExecutePhase) isBlockedBySafety(ctx context.Context, deps *Dependencies, inv *agent.ToolInvocation, nodeID string) *SafetyCheckResult {
	if deps.SafetyGate == nil {
		return &SafetyCheckResult{Blocked: false}
	}

	// Build proposed change from invocation
	change := p.buildProposedChange(inv)
	if change == nil {
		return &SafetyCheckResult{Blocked: false}
	}

	// Run safety check
	result, err := deps.SafetyGate.Check(ctx, []safety.ProposedChange{*change})
	if err != nil {
		// Safety check error - log but don't block
		p.emitError(deps, fmt.Errorf("safety check failed: %w", err), true)
		return &SafetyCheckResult{Blocked: false}
	}

	// Emit safety check event
	p.emitSafetyCheck(deps, result)

	blocked := deps.SafetyGate.ShouldBlock(result)
	if !blocked {
		return &SafetyCheckResult{Blocked: false, Result: result}
	}

	// Extract constraints for CDCL learning
	constraints := safety.ExtractConstraints(result, nodeID)
	errorMsg := result.ToErrorMessage()

	return &SafetyCheckResult{
		Blocked:      true,
		Result:       result,
		ErrorMessage: errorMsg,
		Constraints:  constraints,
	}
}

// buildProposedChange creates a safety change from a tool invocation.
//
// Inputs:
//
//	inv - The tool invocation.
//
// Outputs:
//
//	*safety.ProposedChange - The change, or nil if not applicable.
func (p *ExecutePhase) buildProposedChange(inv *agent.ToolInvocation) *safety.ProposedChange {
	// Map tool names to change types
	switch inv.Tool {
	case "write_file", "edit_file":
		return &safety.ProposedChange{
			Type:   "file_write",
			Target: getStringParamFromToolParams(inv.Parameters, "path"),
		}
	case "delete_file":
		return &safety.ProposedChange{
			Type:   "file_delete",
			Target: getStringParamFromToolParams(inv.Parameters, "path"),
		}
	case "run_command", "shell":
		return &safety.ProposedChange{
			Type:   "shell_command",
			Target: getStringParamFromToolParams(inv.Parameters, "command"),
		}
	default:
		return nil
	}
}

// executeSingleTool executes a single tool invocation.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	inv - The tool invocation.
//
// Outputs:
//
//	*tools.Result - The execution result.
func (p *ExecutePhase) executeSingleTool(ctx context.Context, deps *Dependencies, inv *agent.ToolInvocation) *tools.Result {
	// If no ToolExecutor, skip tool execution
	if deps.ToolExecutor == nil {
		return &tools.Result{
			Success: false,
			Error:   "tool execution not available (no ToolExecutor configured)",
		}
	}

	// Convert ToolParameters to map for internal tool execution
	toolInvocation := &tools.Invocation{
		ID:         inv.ID,
		ToolName:   inv.Tool,
		Parameters: toolParamsToMap(inv.Parameters),
	}

	result, err := deps.ToolExecutor.Execute(ctx, toolInvocation)
	if err != nil {
		return &tools.Result{
			Success: false,
			Error:   err.Error(),
		}
	}

	return result
}

// addAssistantToolCallToHistory adds an assistant message with tool calls to conversation history.
//
// Description:
//
//	When the LLM returns a response with tool calls, we must record that
//	the assistant requested those tools BEFORE adding the tool results.
//	This creates the proper message sequence:
//	  user: "query"
//	  assistant: [tool_call: find_entry_points]
//	  tool: [result]
//	  assistant: "final answer"
//
//	Without this step, tool results become orphaned - the LLM sees tool
//	results but doesn't see that it requested them, causing it to
//	re-request the same tools in an infinite loop.
//
// Inputs:
//
//	deps - Phase dependencies.
//	response - The LLM response containing tool calls.
//	invocations - Parsed tool invocations.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ExecutePhase) addAssistantToolCallToHistory(deps *Dependencies, response *llm.Response, invocations []agent.ToolInvocation) {
	if deps.Context == nil || len(invocations) == 0 {
		return
	}

	// Build a description of what tools the assistant called
	var toolCallDesc strings.Builder
	toolCallDesc.WriteString("[Tool calls: ")
	for i, inv := range invocations {
		if i > 0 {
			toolCallDesc.WriteString(", ")
		}
		toolCallDesc.WriteString(inv.Tool)
	}
	toolCallDesc.WriteString("]")

	// Add assistant message showing it requested tools
	// This ensures the conversation history shows the assistant's intent
	assistantMsg := agent.Message{
		Role:    "assistant",
		Content: toolCallDesc.String(),
	}

	// Use ContextManager if available for thread safety
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, assistantMsg.Role, assistantMsg.Content)
	} else {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, assistantMsg)
	}

	slog.Debug("Added assistant tool call to history",
		slog.String("session_id", deps.Session.ID),
		slog.Int("tool_count", len(invocations)),
		slog.String("tools", toolCallDesc.String()),
	)
}

// updateContextWithResults updates context with tool results.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	results - Tool execution results.
func (p *ExecutePhase) updateContextWithResults(ctx context.Context, deps *Dependencies, results []*tools.Result) {
	// Skip if no ContextManager (degraded mode)
	if deps.ContextManager == nil {
		return
	}

	for _, result := range results {
		if result == nil {
			continue
		}

		updated, err := deps.ContextManager.Update(ctx, deps.Context, result)
		if err != nil {
			p.emitError(deps, fmt.Errorf("context update failed: %w", err), true)
			continue
		}

		// Update deps.Context with the new context and persist to session
		deps.Context = updated
		deps.Session.SetCurrentContext(updated)
	}
}

// shouldReflect determines if reflection is needed.
//
// Inputs:
//
//	deps - Phase dependencies.
//	stepNumber - Current step number.
//
// Outputs:
//
//	bool - True if reflection should occur.
func (p *ExecutePhase) shouldReflect(deps *Dependencies, stepNumber int) bool {
	return stepNumber > 0 && stepNumber%p.reflectionThreshold == 0
}

// emitLLMRequest emits an LLM request event.
func (p *ExecutePhase) emitLLMRequest(deps *Dependencies, request *llm.Request) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeLLMRequest, &events.LLMRequestData{
		Model:     deps.LLMClient.Model(),
		TokensIn:  request.MaxTokens, // Approximation
		HasTools:  len(request.Tools) > 0,
		ToolCount: len(request.Tools),
	})
}

// emitLLMResponse emits an LLM response event.
func (p *ExecutePhase) emitLLMResponse(deps *Dependencies, response *llm.Response) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeLLMResponse, &events.LLMResponseData{
		Model:         response.Model,
		TokensOut:     response.OutputTokens,
		Duration:      response.Duration,
		StopReason:    response.StopReason,
		HasToolCalls:  response.HasToolCalls(),
		ToolCallCount: len(response.ToolCalls),
	})
}

// emitToolInvocation emits a tool invocation event.
func (p *ExecutePhase) emitToolInvocation(deps *Dependencies, inv *agent.ToolInvocation) {
	if deps.EventEmitter == nil {
		return
	}

	// Convert ToolParameters to event parameters
	deps.EventEmitter.Emit(events.TypeToolInvocation, &events.ToolInvocationData{
		ToolName:     inv.Tool,
		InvocationID: inv.ID,
		Parameters:   toolParamsToEventParams(inv.Parameters),
	})
}

// toolParamsToEventParams converts agent.ToolParameters to events.ToolInvocationParameters.
func toolParamsToEventParams(params *agent.ToolParameters) *events.ToolInvocationParameters {
	if params == nil {
		return nil
	}
	return &events.ToolInvocationParameters{
		StringParams: params.StringParams,
		IntParams:    params.IntParams,
		BoolParams:   params.BoolParams,
		RawJSON:      params.RawJSON,
	}
}

// emitToolResult emits a tool result event.
func (p *ExecutePhase) emitToolResult(deps *Dependencies, inv *agent.ToolInvocation, result *tools.Result) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeToolResult, &events.ToolResultData{
		ToolName:     inv.Tool,
		InvocationID: inv.ID,
		Success:      result.Success,
		Duration:     result.Duration,
		TokensUsed:   result.TokensUsed,
		Cached:       result.Cached,
		Error:        result.Error,
	})
}

// emitSafetyCheck emits a safety check event.
func (p *ExecutePhase) emitSafetyCheck(deps *Dependencies, result *safety.Result) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeSafetyCheck, &events.SafetyCheckData{
		ChangesChecked: result.ChecksRun,
		Passed:         result.Passed,
		CriticalCount:  result.CriticalCount,
		WarningCount:   result.WarningCount,
		Blocked:        !result.Passed,
	})
}

// emitStepComplete emits a step complete event.
func (p *ExecutePhase) emitStepComplete(deps *Dependencies, stepStart time.Time, stepNumber, toolsInvoked int) {
	if deps.EventEmitter == nil {
		return
	}

	tokensUsed := 0
	if deps.Context != nil {
		tokensUsed = deps.Context.TotalTokens
	}

	deps.EventEmitter.Emit(events.TypeStepComplete, &events.StepCompleteData{
		StepNumber:   stepNumber,
		Duration:     time.Since(stepStart),
		ToolsInvoked: toolsInvoked,
		TokensUsed:   tokensUsed,
	})
}

// emitStateTransition emits a state transition event.
func (p *ExecutePhase) emitStateTransition(deps *Dependencies, from, to agent.AgentState, reason string) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeStateTransition, &events.StateTransitionData{
		FromState: from,
		ToState:   to,
		Reason:    reason,
	})
}

// emitError emits an error event.
func (p *ExecutePhase) emitError(deps *Dependencies, err error, recoverable bool) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeError, &events.ErrorData{
		Error:       err.Error(),
		Recoverable: recoverable,
	})
}

// =============================================================================
// CRS-06: Coordinator Event Emission
// =============================================================================

// emitCoordinatorEvent emits an event to the MCTS Coordinator.
//
// Description:
//
//	If a Coordinator is configured, emits the event which triggers appropriate
//	MCTS activities (Search, Learning, Awareness, etc.). The Coordinator
//	orchestrates activities based on the event type and current session state.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing Coordinator.
//	event - The agent event to emit.
//	inv - The tool invocation (may be nil).
//	result - The tool result (may be nil).
//	errorMsg - Error message if applicable.
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) emitCoordinatorEvent(
	ctx context.Context,
	deps *Dependencies,
	event integration.AgentEvent,
	inv *agent.ToolInvocation,
	result *tools.Result,
	errorMsg string,
) {
	if deps.Coordinator == nil || deps.Session == nil {
		return
	}

	// Build event data
	data := &integration.EventData{
		SessionID: deps.Session.ID,
	}

	// Add tool information if available
	if inv != nil {
		data.Tool = inv.Tool
	}

	// Add error information if available
	if result != nil && !result.Success {
		data.Error = result.Error
	} else if errorMsg != "" {
		data.Error = errorMsg
	}

	// Get step number from session
	if deps.Session != nil {
		data.StepNumber = deps.Session.GetMetric(agent.MetricSteps)
	}

	// Handle the event - activities are executed asynchronously
	_, err := deps.Coordinator.HandleEvent(ctx, event, data)
	if err != nil {
		slog.Warn("CRS-06: Coordinator event handling failed",
			slog.String("event", string(event)),
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
	} else {
		slog.Debug("CRS-06: Coordinator event handled",
			slog.String("event", string(event)),
			slog.String("session_id", deps.Session.ID),
		)
	}
}

// getStringParamFromToolParams extracts a string parameter from ToolParameters.
//
// Inputs:
//
//	params - The tool parameters
//	key - The parameter name
//
// Outputs:
//
//	string - The parameter value, or empty string if not found
func getStringParamFromToolParams(params *agent.ToolParameters, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params.GetString(key); ok {
		return v
	}
	return ""
}

// toolParamsToMap converts ToolParameters to a map for internal tool execution.
//
// Inputs:
//
//	params - The tool parameters
//
// Outputs:
//
//	map[string]any - Parameters as a map
func toolParamsToMap(params *agent.ToolParameters) map[string]any {
	result := make(map[string]any)
	if params == nil {
		return result
	}

	for k, v := range params.StringParams {
		result[k] = v
	}
	for k, v := range params.IntParams {
		result[k] = v
	}
	for k, v := range params.BoolParams {
		result[k] = v
	}

	return result
}

// maybeRefreshGraph refreshes the graph if dirty files exist.
//
// Description:
//
//	Checks if any files have been marked dirty by previous tool executions.
//	If so, triggers an incremental refresh to update the graph with fresh
//	parse results. This ensures subsequent tool queries return up-to-date data.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//
// Thread Safety:
//
//	Safe for concurrent use. Refresh is atomic (copy-on-write).
func (p *ExecutePhase) maybeRefreshGraph(ctx context.Context, deps *Dependencies) {
	// Skip if no tracker or refresher
	if deps.DirtyTracker == nil || deps.GraphRefresher == nil {
		return
	}

	// Skip if no dirty files
	if !deps.DirtyTracker.HasDirty() {
		return
	}

	// Get dirty files (does not clear - we clear after successful refresh)
	dirtyFiles := deps.DirtyTracker.GetDirtyFiles()
	if len(dirtyFiles) == 0 {
		return
	}

	slog.Info("refreshing graph for modified files",
		slog.String("session_id", deps.Session.ID),
		slog.Int("file_count", len(dirtyFiles)),
	)

	// Perform refresh
	result, err := deps.GraphRefresher.RefreshFiles(ctx, dirtyFiles)
	if err != nil {
		slog.Warn("incremental graph refresh failed, continuing with stale graph",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		// Non-fatal: continue with stale data rather than failing
		return
	}

	// Clear the successfully refreshed files
	deps.DirtyTracker.Clear(dirtyFiles)

	slog.Info("graph refreshed",
		slog.String("session_id", deps.Session.ID),
		slog.Int("nodes_removed", result.NodesRemoved),
		slog.Int("nodes_added", result.NodesAdded),
		slog.Duration("duration", result.Duration),
	)

	// Emit graph refresh event
	p.emitGraphRefresh(deps, result, len(dirtyFiles))
}

// trackModifiedFiles marks files modified by a tool result as dirty.
//
// Description:
//
//	After a tool executes, checks if it modified any files and marks
//	them in the DirtyTracker for later refresh.
//
// Inputs:
//
//	deps - Phase dependencies.
//	result - The tool execution result.
func (p *ExecutePhase) trackModifiedFiles(deps *Dependencies, result *tools.Result) {
	// Skip if no tracker or result
	if deps.DirtyTracker == nil || result == nil {
		return
	}

	// Skip if no modified files
	if len(result.ModifiedFiles) == 0 {
		return
	}

	// Mark each modified file as dirty
	for _, path := range result.ModifiedFiles {
		deps.DirtyTracker.MarkDirty(path)
	}

	slog.Debug("tracked modified files",
		slog.String("session_id", deps.Session.ID),
		slog.Int("count", len(result.ModifiedFiles)),
	)
}

// getToolNames extracts tool names from the registry.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	[]string - List of available tool names.
func (p *ExecutePhase) getToolNames(deps *Dependencies) []string {
	if deps.ToolRegistry == nil {
		return nil
	}
	defs := deps.ToolRegistry.GetDefinitions()
	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = def.Name
	}
	return names
}

// emitGraphRefresh emits a graph refresh event.
func (p *ExecutePhase) emitGraphRefresh(deps *Dependencies, result *graph.RefreshResult, fileCount int) {
	if deps.EventEmitter == nil || result == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeContextUpdate, &events.ContextUpdateData{
		Action:          "graph_refresh",
		EntriesAffected: fileCount,
		TokensBefore:    result.NodesRemoved,
		TokensAfter:     result.NodesAdded,
	})
}

// =============================================================================
// CB-31d: Hard Tool Forcing Implementation
// =============================================================================

// extractToolParameters extracts parameters for a tool from the query and context.
//
// Description:
//
//	Uses rule-based extraction to determine tool parameters without calling
//	the Main LLM. This enables direct tool execution for router selections.
//	TR-12 Fix: Tool-specific parameter extraction logic.
//
// Inputs:
//
//	query - The user's query string.
//	toolName - The name of the tool to extract parameters for.
//	toolDefs - Available tool definitions.
//	ctx - Assembled context with current file, symbols, etc.
//
// Outputs:
//
//	map[string]interface{} - Extracted parameters.
//	error - Non-nil if parameter extraction fails.
func (p *ExecutePhase) extractToolParameters(
	query string,
	toolName string,
	toolDefs []tools.ToolDefinition,
	ctx *agent.AssembledContext,
) (map[string]interface{}, error) {
	// Find tool definition
	var toolDef *tools.ToolDefinition
	for i := range toolDefs {
		if toolDefs[i].Name == toolName {
			toolDef = &toolDefs[i]
			break
		}
	}

	if toolDef == nil {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}

	// Tool-specific parameter extraction
	switch toolName {
	case "list_packages":
		// No parameters required
		return map[string]interface{}{}, nil

	case "graph_overview":
		// Optional parameters with defaults
		params := map[string]interface{}{
			"depth":                2,
			"include_dependencies": true,
			"include_metrics":      true,
		}
		return params, nil

	case "explore_package":
		// Extract package name from query
		pkgName := extractPackageNameFromQuery(query)
		if pkgName == "" {
			return nil, errors.New("could not extract package name from query")
		}
		return map[string]interface{}{
			"package":              pkgName,
			"include_dependencies": true,
			"include_dependents":   true,
		}, nil

	case "find_entry_points":
		// Use defaults
		return map[string]interface{}{}, nil

	default:
		// For other tools, fallback to Main LLM
		return nil, fmt.Errorf("parameter extraction not implemented for tool: %s", toolName)
	}
}

// executeToolDirectlyWithFallback executes a tool directly without calling Main LLM.
//
// Description:
//
//	TR-2 Fix: Executes tool directly with full CRS recording for observability.
//	This is the core of the hard forcing mechanism that prevents Split-Brain.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	toolName - Name of the tool to execute.
//	params - Extracted parameters for the tool.
//	toolDefs - Available tool definitions.
//
// Outputs:
//
//	*PhaseResult - The execution result if successful.
//	error - Non-nil if execution fails.
func (p *ExecutePhase) executeToolDirectlyWithFallback(
	ctx context.Context,
	deps *Dependencies,
	toolName string,
	params map[string]interface{},
	toolDefs []tools.ToolDefinition,
) (*PhaseResult, error) {
	start := time.Now()

	// Get tool from registry
	tool, found := deps.ToolRegistry.Get(toolName)
	if !found {
		return nil, fmt.Errorf("tool implementation not found: %s", toolName)
	}

	// Execute the tool
	result, err := tool.Execute(ctx, params)
	duration := time.Since(start)

	// CRITICAL: Record CRS step for observability (TR-2 Fix)
	stepBuilder := crs.NewTraceStepBuilder().
		WithAction("tool_call_forced").
		WithTool(toolName).
		WithDuration(duration).
		WithMetadata("forced_by", "router").
		WithMetadata("params", fmt.Sprintf("%v", params))

	if err != nil {
		stepBuilder = stepBuilder.WithError(err.Error())
		deps.Session.RecordTraceStep(stepBuilder.Build())
		return nil, fmt.Errorf("tool execution failed: %w", err)
	}

	// Add result preview to trace
	if result != nil && result.Output != nil {
		// result.Output is interface{}, convert to string for preview
		outputStr := fmt.Sprintf("%v", result.Output)
		if len(outputStr) > 200 {
			outputStr = outputStr[:200] + "..."
		}
		stepBuilder = stepBuilder.WithMetadata("result_preview", outputStr)
	}

	deps.Session.RecordTraceStep(stepBuilder.Build())

	// Update context with tool result using ContextManager
	if deps.ContextManager != nil && deps.Context != nil && result != nil {
		_, err := deps.ContextManager.Update(ctx, deps.Context, result)
		if err != nil {
			slog.Warn("Failed to update context with hard-forced tool result",
				slog.String("tool", toolName),
				slog.String("error", err.Error()),
			)
		}
	}

	// Return success result - this will exit Execute() early
	return &PhaseResult{
		NextState: agent.StateExecute, // Stay in execute to allow router to decide next step
		Response:  fmt.Sprintf("Tool %s executed successfully (hard forced)", toolName),
	}, nil
}

// extractPackageNameFromQuery extracts a package name from the user's query.
//
// Description:
//
//	Uses regex patterns to identify package references in natural language.
//	TR-12 Fix: Rule-based extraction for explore_package tool.
//
// Inputs:
//
//	query - The user's query string.
//
// Outputs:
//
//	string - The extracted package name, or empty if not found.
func extractPackageNameFromQuery(query string) string {
	query = strings.ToLower(query)

	// Pattern 1: "about package X" or "about the X package"
	if idx := strings.Index(query, "package"); idx >= 0 {
		after := query[idx+7:]
		words := strings.Fields(after)
		if len(words) > 0 {
			pkg := strings.Trim(words[0], "?,.")
			if pkg != "" && pkg != "the" && pkg != "a" && pkg != "an" {
				return pkg
			}
			if len(words) > 1 {
				pkg = strings.Trim(words[1], "?,.")
				return pkg
			}
		}
	}

	// Pattern 2: "pkg/something" or "path/to/package"
	if strings.Contains(query, "pkg/") || strings.Contains(query, "/") {
		words := strings.Fields(query)
		for _, word := range words {
			if strings.Contains(word, "/") {
				return strings.Trim(word, "?,.")
			}
		}
	}

	return ""
}

// containsToolCallPattern detects escaped tool call patterns in LLM text response.
//
// # Description
//
// TR-5 Fix: When the circuit breaker fires and forces ToolChoiceNone(), the LLM
// may still try to output tool calls in its text response, escaping the constraint.
// This function detects patterns like:
//   - "[Tool calls: tool_name(...)]"
//   - "[Tool call: tool_name(...)]"
//   - "Calling tool: tool_name(...)"
//   - "<tool>tool_name</tool>"
//
// # Inputs
//
//   - content: The LLM's text response content.
//
// # Outputs
//
//   - bool: True if escaped tool call pattern detected, false otherwise.
//
// # Thread Safety
//
// Safe for concurrent use (read-only).
func containsToolCallPattern(content string) bool {
	if content == "" {
		return false
	}

	// Pattern 1: [Tool calls: ...]  or  [Tool call: ...]
	if strings.Contains(content, "[Tool call") {
		return true
	}

	// Pattern 2: Calling tool: ...
	if strings.Contains(content, "Calling tool:") || strings.Contains(content, "calling tool:") {
		return true
	}

	// Pattern 3: <tool>...</tool> XML-style
	if strings.Contains(content, "<tool>") && strings.Contains(content, "</tool>") {
		return true
	}

	// Pattern 4: Function call patterns: functionName(...)
	// Only trigger if we see common tool names followed by parentheses
	toolNames := []string{
		"find_symbol", "find_symbol_usages", "read_file", "read_symbol",
		"grep_codebase", "list_files", "get_symbol_graph", "find_config_usage",
		"list_packages", "graph_overview", "explore_package", "find_entry_points",
	}
	for _, toolName := range toolNames {
		if strings.Contains(content, toolName+"(") {
			return true
		}
	}

	return false
}

// truncateForLog truncates a string for logging without cutting mid-word.
//
// # Inputs
//
//   - s: String to truncate.
//   - maxLen: Maximum length.
//
// # Outputs
//
//   - string: Truncated string with "..." suffix if truncated.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Find last space before maxLen
	truncated := s[:maxLen]
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > maxLen/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}

// retryDesperationWithStrongerPrompt retries LLM call with explicit no-tools instruction.
//
// # Description
//
// TR-5 Fix: When circuit breaker fires and the LLM escapes ToolChoiceNone() by
// outputting tool calls in its text response, this function retries with an
// augmented system prompt that explicitly forbids tool call patterns.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - deps: Phase dependencies.
//   - stepStart: When this step started (for timing).
//   - stepNumber: Current step number.
//   - blockedTool: The tool that was blocked by circuit breaker.
//
// # Outputs
//
//   - agent.AgentState: Next state (always StateExecute or completion).
//   - error: Non-nil if retry fails catastrophically.
//
// # Thread Safety
//
// Not safe for concurrent use (modifies deps.Context).
func (p *ExecutePhase) retryDesperationWithStrongerPrompt(
	ctx context.Context,
	deps *Dependencies,
	stepStart time.Time,
	stepNumber int,
) (agent.AgentState, error) {
	slog.Info("Retrying with stronger anti-tool-call prompt (CB-31d TR-5)",
		slog.String("session_id", deps.Session.ID),
	)

	// Build a new request with stronger instructions
	request, _ := p.buildLLMRequest(deps) // Ignore hard forcing in desperation mode

	// TR-9 Fix: Modify request instead of rebuilding from scratch
	// Add explicit anti-tool-call instruction to system prompt
	desperationPrompt := `
CRITICAL INSTRUCTION - READ CAREFULLY:
You MUST provide a TEXT ANSWER ONLY. Tool calling has been disabled.

DO NOT output ANY of these patterns:
- [Tool calls: ...]
- [Tool call: ...]
- Calling tool: ...
- <tool>...</tool>
- Any function-call-like syntax: tool_name(...)

Instead, synthesize a helpful answer from the information gathered so far.
If you need more information, say "I don't have enough information" and explain what's missing.
`

	// Prepend to system prompt if it exists
	if len(request.Messages) > 0 && request.Messages[0].Role == "system" {
		request.Messages[0].Content = desperationPrompt + "\n\n" + request.Messages[0].Content
	} else {
		// Insert as first message
		request.Messages = append([]llm.Message{
			{Role: "system", Content: desperationPrompt},
		}, request.Messages...)
	}

	// Ensure tools are disabled
	request.ToolChoice = llm.ToolChoiceNone()
	request.Tools = nil

	// Retry the LLM call
	response, err := p.callLLM(ctx, deps, request)
	if err != nil {
		slog.Error("Desperation retry LLM call failed",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		return p.handleLLMError(deps, err)
	}

	slog.Info("Desperation retry LLM response received",
		slog.String("session_id", deps.Session.ID),
		slog.Int("output_tokens", response.OutputTokens),
		slog.Bool("has_tool_calls", response.HasToolCalls()),
		slog.Int("content_len", len(response.Content)),
	)

	// Check again - if it STILL has escaped tool calls, give up and complete anyway
	if !response.HasToolCalls() && containsToolCallPattern(response.Content) {
		slog.Error("Circuit breaker desperation: LLM STILL escaping after retry (CB-31d)",
			slog.String("session_id", deps.Session.ID),
			slog.String("response_preview", truncateForLog(response.Content, 300)),
		)
		// Strip the escaped tool call patterns and proceed with completion
		response.Content = stripToolCallPatterns(response.Content)
	}

	// Complete with the response (potentially cleaned)
	return p.handleCompletion(ctx, deps, response, stepStart, stepNumber)
}

// stripToolCallPatterns removes escaped tool call patterns from LLM response.
//
// # Description
//
// Last-resort cleanup when LLM persists in outputting tool call patterns
// despite explicit instructions. Removes common patterns like [Tool calls: ...]
// and replaces them with a note about the blocked tool.
//
// # Inputs
//
//   - content: The LLM response content with escaped patterns.
//
// # Outputs
//
//   - string: Cleaned content with patterns removed.
func stripToolCallPatterns(content string) string {
	// Pattern 1: [Tool calls: ...] or [Tool call: ...]
	content = regexp.MustCompile(`\[Tool calls?:[^\]]*\]`).ReplaceAllString(content, "[Tool call blocked by circuit breaker]")

	// Pattern 2: Calling tool: ...
	content = regexp.MustCompile(`(?i)calling tool:[^\n]*`).ReplaceAllString(content, "[Tool call blocked by circuit breaker]")

	// Pattern 3: <tool>...</tool>
	content = regexp.MustCompile(`<tool>.*?</tool>`).ReplaceAllString(content, "[Tool call blocked by circuit breaker]")

	// Pattern 4: Common tool names with parentheses
	toolNames := []string{
		"find_symbol", "find_symbol_usages", "read_file", "read_symbol",
		"grep_codebase", "list_files", "get_symbol_graph", "find_config_usage",
		"list_packages", "graph_overview", "explore_package", "find_entry_points",
	}
	for _, toolName := range toolNames {
		pattern := regexp.MustCompile(toolName + `\([^)]*\)`)
		content = pattern.ReplaceAllString(content, "[Tool call blocked by circuit breaker]")
	}

	return content
}

// -----------------------------------------------------------------------------
// CRS-02: Proof Index Integration
// -----------------------------------------------------------------------------

// updateProofNumber updates the proof number for a tool path based on execution outcome.
//
// Description:
//
//	Called after tool execution to update the CRS proof index.
//	- Success: Decrements proof number (path is easier to prove)
//	- Failure: Increments proof number (path is harder to prove)
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing session.
//	inv - The tool invocation.
//	result - The tool execution result.
func (p *ExecutePhase) updateProofNumber(ctx context.Context, deps *Dependencies, inv *agent.ToolInvocation, result *tools.Result) {
	if deps.Session == nil {
		return
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		return
	}

	// Build node ID for this tool path
	nodeID := fmt.Sprintf("session:%s:tool:%s", deps.Session.ID, inv.Tool)

	var updateType crs.ProofUpdateType
	var reason string

	if result.Success {
		// Success: decrease proof number (path is viable)
		updateType = crs.ProofUpdateTypeDecrement
		reason = "tool_success"
	} else {
		// Failure: increase proof number (path is problematic)
		updateType = crs.ProofUpdateTypeIncrement
		reason = "tool_failure: " + result.Error
	}

	err := crsInstance.UpdateProofNumber(ctx, crs.ProofUpdate{
		NodeID: nodeID,
		Type:   updateType,
		Delta:  1,
		Reason: reason,
		Source: crs.SignalSourceHard, // Tool execution is a hard signal
	})
	if err != nil {
		slog.Warn("CRS-02: failed to update proof number",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", inv.Tool),
			slog.String("error", err.Error()),
		)
	}
}

// markToolDisproven marks a tool path as disproven in the proof index.
//
// Description:
//
//	Called when a tool is blocked by safety or after repeated failures.
//	Marks the path as disproven (infinite cost to prove) and propagates
//	the disproof to parent decisions.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing session.
//	inv - The tool invocation.
//	reason - Why the tool was disproven.
func (p *ExecutePhase) markToolDisproven(ctx context.Context, deps *Dependencies, inv *agent.ToolInvocation, reason string) {
	if deps.Session == nil {
		return
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		return
	}

	// Build node ID for this tool path
	nodeID := fmt.Sprintf("session:%s:tool:%s", deps.Session.ID, inv.Tool)

	err := crsInstance.UpdateProofNumber(ctx, crs.ProofUpdate{
		NodeID: nodeID,
		Type:   crs.ProofUpdateTypeDisproven,
		Reason: reason,
		Source: crs.SignalSourceSafety, // Safety violation is a hard signal
	})
	if err != nil {
		slog.Warn("CRS-02: failed to mark tool disproven",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", inv.Tool),
			slog.String("error", err.Error()),
		)
		return
	}

	// Propagate disproof to parent decisions
	affected := crsInstance.PropagateDisproof(ctx, nodeID)
	if affected > 0 {
		slog.Debug("CRS-02: disproof propagated to parents",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", inv.Tool),
			slog.Int("affected_nodes", affected),
		)
	}
}

// checkCircuitBreakerCRS checks if the circuit breaker should fire using CRS proof status.
//
// Description:
//
//	Uses CRS.CheckCircuitBreaker to determine if a tool path is disproven
//	or has exhausted its proof number. This replaces ad-hoc counting.
//
// Inputs:
//
//	deps - Phase dependencies containing session.
//	tool - The tool name to check.
//
// Outputs:
//
//	bool - True if circuit breaker should fire.
//	string - Reason if circuit breaker fires.
func (p *ExecutePhase) checkCircuitBreakerCRS(deps *Dependencies, tool string) (bool, string) {
	if deps.Session == nil {
		return false, ""
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		// CRS not enabled - fall back to execution count
		count := crsInstance.CountToolExecutions(deps.Session.ID, tool)
		if count >= maxRepeatedToolCalls {
			return true, fmt.Sprintf("tool %s called %d times (threshold: %d)", tool, count, maxRepeatedToolCalls)
		}
		return false, ""
	}

	result := crsInstance.CheckCircuitBreaker(deps.Session.ID, tool)
	return result.ShouldFire, result.Reason
}

// -----------------------------------------------------------------------------
// CRS-03: Cycle Detection Integration
// -----------------------------------------------------------------------------

// checkCycleAfterStep checks for reasoning cycles after a tool execution step.
//
// Description:
//
//	Uses Brent's algorithm for real-time cycle detection. If a cycle is detected,
//	marks all cycle states as disproven and returns true to signal that the
//	circuit breaker should fire.
//
//	Cycle detection catches patterns that simple tool counting misses:
//	- A → B → A (2-step cycles)
//	- A → B → C → A (multi-step cycles)
//	- Complex decision patterns that form loops
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing session.
//	inv - The tool invocation that was just executed.
//	stepNumber - The current step number.
//	success - Whether the tool execution succeeded.
//
// Outputs:
//
//	bool - True if a cycle was detected.
//	string - Description of the cycle if detected.
func (p *ExecutePhase) checkCycleAfterStep(
	ctx context.Context,
	deps *Dependencies,
	inv *agent.ToolInvocation,
	stepNumber int,
	success bool,
) (bool, string) {
	if deps.Session == nil {
		return false, ""
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		return false, ""
	}

	detector := deps.Session.GetCycleDetector()
	if detector == nil {
		return false, ""
	}

	// Build step record for cycle detection
	var outcome crs.Outcome
	if success {
		outcome = crs.OutcomeSuccess
	} else {
		outcome = crs.OutcomeFailure
	}

	// I-05: Determine actor based on whether tool router is enabled
	// When router is active, it pre-selects tools before main agent executes
	actor := crs.ActorMainAgent
	if deps.Session.IsToolRouterEnabled() {
		actor = crs.ActorRouter
	}

	// I-04: Use CRS step count if available for consistency
	crsStepNumber := stepNumber
	if lastStep := crsInstance.GetLastStep(deps.Session.ID); lastStep != nil {
		crsStepNumber = lastStep.StepNumber + 1
	}

	step := crs.StepRecord{
		StepNumber: crsStepNumber,
		Timestamp:  time.Now(),
		SessionID:  deps.Session.ID,
		Actor:      actor,
		Decision:   crs.DecisionExecuteTool,
		Tool:       inv.Tool,
		Outcome:    outcome,
	}

	// Check for cycles
	result := crs.CheckCycleOnStep(ctx, crsInstance, step, detector)

	if result.Detected {
		slog.Warn("CRS-03: Reasoning cycle detected",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", inv.Tool),
			slog.Int("cycle_length", result.CycleLength),
			slog.Any("cycle", result.Cycle),
		)

		// CRS-03: Record cycle detection metric
		integration.RecordBrentCycleDetected(result.CycleLength)

		// CRS-04: Learn from cycle detection
		p.learnFromFailure(ctx, deps, crs.FailureEvent{
			SessionID:   deps.Session.ID,
			FailureType: crs.FailureTypeCycleDetected,
			FailedStep:  step,
			Tool:        inv.Tool,
			Source:      crs.SignalSourceHard,
		})

		return true, fmt.Sprintf("cycle detected (length %d): %v", result.CycleLength, result.Cycle)
	}

	return false, ""
}

// -----------------------------------------------------------------------------
// CRS-04: Learning Activity Integration
// -----------------------------------------------------------------------------

// learnFromFailure triggers CDCL learning from a failure event.
//
// Description:
//
//	When a failure occurs (tool error, cycle, circuit breaker), this method
//	creates a learned clause that prevents the same failure pattern. The
//	clause is stored in CRS and checked before future decisions.
//
//	IMPORTANT: Only learns from hard signals. Soft signals (LLM feedback)
//	are NOT used for clause learning per Rule #2.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing session and CRS.
//	failure - The failure event to learn from.
//
// Outputs:
//
//	None. Errors are logged but do not interrupt execution.
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) learnFromFailure(ctx context.Context, deps *Dependencies, failure crs.FailureEvent) {
	if deps.Session == nil {
		return
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		return
	}

	// Validate failure event
	if err := failure.Validate(); err != nil {
		slog.Warn("CRS-04: Invalid failure event",
			slog.String("error", err.Error()),
		)
		return
	}

	// Only learn from hard signals (Rule #2)
	if !failure.Source.IsHard() {
		slog.Debug("CRS-04: Skipping learning from soft signal",
			slog.String("failure_type", string(failure.FailureType)),
		)
		return
	}

	// Get step history for CDCL analysis
	steps := crsInstance.GetStepHistory(deps.Session.ID)
	failure.DecisionPath = steps

	// Generate learned clause from failure
	clause := p.generateClauseFromFailure(deps.Session.ID, failure)
	if clause == nil {
		return
	}

	// Add clause to CRS
	if err := crsInstance.AddClause(ctx, clause); err != nil {
		slog.Warn("CRS-04: Failed to add learned clause",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	slog.Info("CRS-04: Learned clause from failure",
		slog.String("session_id", deps.Session.ID),
		slog.String("failure_type", string(failure.FailureType)),
		slog.String("clause_id", clause.ID),
		slog.String("clause", clause.String()),
	)

	// Record metric
	integration.RecordClauseLearned(string(failure.FailureType))
}

// generateClauseFromFailure creates a CDCL clause from a failure event.
//
// Description:
//
//	Analyzes the failure and decision path to generate a clause that
//	prevents the same failure pattern. Different failure types produce
//	different clause structures:
//
//	- Cycle: (¬tool:A ∨ ¬prev_tool:B) - Block repeating A after B
//	- Circuit breaker: (¬tool:X ∨ ¬outcome:success) - Block repeated success tool
//	- Tool error: (¬tool:X ∨ ¬error:category) - Block tool when error occurs
//
// Inputs:
//
//	sessionID - The session ID for clause attribution.
//	failure - The failure event to analyze.
//
// Outputs:
//
//	*crs.Clause - The learned clause, or nil if no clause could be generated.
func (p *ExecutePhase) generateClauseFromFailure(sessionID string, failure crs.FailureEvent) *crs.Clause {
	var literals []crs.Literal

	switch failure.FailureType {
	case crs.FailureTypeCycleDetected:
		// Cycle: Block the tool that was repeated
		if failure.Tool != "" {
			literals = append(literals, crs.Literal{
				Variable: "tool:" + failure.Tool,
				Negated:  true,
			})
			// If we have history, add prev_tool to make clause more specific
			if len(failure.DecisionPath) > 1 {
				prevTool := failure.DecisionPath[len(failure.DecisionPath)-2].Tool
				if prevTool != "" {
					literals = append(literals, crs.Literal{
						Variable: "prev_tool:" + prevTool,
						Negated:  true,
					})
				}
			}
		}

	case crs.FailureTypeCircuitBreaker:
		// Circuit breaker: Block the tool that was called too many times
		if failure.Tool != "" {
			literals = append(literals, crs.Literal{
				Variable: "tool:" + failure.Tool,
				Negated:  true,
			})
			// Add outcome:success to make clause more specific
			// (only block repeating successful calls)
			literals = append(literals, crs.Literal{
				Variable: "outcome:success",
				Negated:  true,
			})
		}

	case crs.FailureTypeToolError:
		// Tool error: Block the tool with the specific error category
		if failure.Tool != "" && failure.ErrorCategory != "" {
			literals = append(literals, crs.Literal{
				Variable: "tool:" + failure.Tool,
				Negated:  true,
			})
			literals = append(literals, crs.Literal{
				Variable: "error:" + string(failure.ErrorCategory),
				Negated:  true,
			})
		}

	case crs.FailureTypeSafety:
		// Safety: Block the tool that triggered safety violation
		if failure.Tool != "" {
			literals = append(literals, crs.Literal{
				Variable: "tool:" + failure.Tool,
				Negated:  true,
			})
		}

	default:
		// Unknown failure type - cannot generate clause
		return nil
	}

	if len(literals) == 0 {
		return nil
	}

	// CR-3: Use UUID for collision-resistant clause IDs
	clauseID := fmt.Sprintf("clause_%s_%s_%s",
		string(failure.FailureType),
		failure.Tool,
		uuid.New().String()[:8], // Short UUID suffix for readability
	)

	return &crs.Clause{
		ID:          clauseID,
		Literals:    literals,
		Source:      crs.SignalSourceHard,
		FailureType: failure.FailureType,
		SessionID:   sessionID,
	}
}

// checkDecisionAllowed checks if a proposed tool selection violates learned clauses.
//
// Description:
//
//	Before making a tool selection decision, this method checks if the
//	proposed tool violates any learned clauses. If a violation is found,
//	the decision should be blocked and an alternative tool selected.
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	deps - Phase dependencies containing session and CRS.
//	tool - The proposed tool to check.
//
// Outputs:
//
//	bool - True if the decision is allowed.
//	string - Reason if the decision is blocked.
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) checkDecisionAllowed(ctx context.Context, deps *Dependencies, tool string) (bool, string) {
	if deps.Session == nil {
		return true, ""
	}

	crsInstance := deps.Session.GetCRS()
	if crsInstance == nil {
		return true, ""
	}

	allowed, reason := crsInstance.CheckDecisionAllowed(deps.Session.ID, tool)
	if !allowed {
		slog.InfoContext(ctx, "CRS-04: Decision blocked by learned clause",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", tool),
			slog.String("reason", reason),
		)
		integration.RecordDecisionBlocked(tool)
	}

	return allowed, reason
}

// =============================================================================
// UCB1-Enhanced Tool Selection (CRS-05)
// =============================================================================

// ucb1SelectionContext holds UCB1 selection state for a session.
type ucb1SelectionContext struct {
	scorer            *routing.UCB1Scorer
	cache             *routing.ToolSelectionCache
	forcedMoveChecker *routing.ForcedMoveChecker
	selectionCounts   *routing.SelectionCounts
	stateKeyBuilder   *routing.StateKeyBuilder
}

// initUCB1Context initializes the UCB1 selection context for a session.
//
// Description:
//
//	Creates the UCB1 scorer, cache, and forced move checker. Called once
//	per session when UCB1-enhanced selection is first used.
//
// Outputs:
//
//	*ucb1SelectionContext - The initialized context.
func initUCB1Context() *ucb1SelectionContext {
	return &ucb1SelectionContext{
		scorer:            routing.NewUCB1Scorer(),
		cache:             routing.NewToolSelectionCache(),
		forcedMoveChecker: routing.NewForcedMoveChecker(),
		selectionCounts:   routing.NewSelectionCounts(),
		stateKeyBuilder:   routing.NewStateKeyBuilder(),
	}
}

// selectToolWithUCB1 enhances router selection with UCB1 scoring.
//
// Description:
//
//	Implements the CRS-05 UCB1-enhanced tool selection flow:
//	1. Check cache for cached selection
//	2. Check for forced move (unit propagation)
//	3. Get router's tool suggestion
//	4. Score all tools using UCB1
//	5. Select best non-blocked tool
//	6. Cache result
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	ucb1Ctx - UCB1 selection context.
//	routerSelection - Initial selection from router.
//	availableTools - List of available tool names.
//
// Outputs:
//
//	*agent.ToolRouterSelection - Enhanced selection (may differ from router).
//	bool - True if selection was modified by UCB1.
func (p *ExecutePhase) selectToolWithUCB1(
	ctx context.Context,
	deps *Dependencies,
	ucb1Ctx *ucb1SelectionContext,
	routerSelection *agent.ToolRouterSelection,
	availableTools []string,
) (*agent.ToolRouterSelection, bool) {
	startTime := time.Now()
	defer func() {
		routing.RecordUCB1ScoringLatency(time.Since(startTime).Seconds())
	}()

	// Get CRS for proof index and clause checking
	var proofIndex crs.ProofIndexView
	var clauseChecker routing.ClauseChecker
	var generation int64

	if deps.Session != nil && deps.Session.HasCRS() {
		crsInstance := deps.Session.GetCRS()
		snapshot := crsInstance.Snapshot()
		proofIndex = snapshot.ProofIndex()
		clauseChecker = routing.NewClauseCheckerFromConstraintIndex(snapshot.ConstraintIndex())
		generation = crsInstance.Generation()
	}

	// Get step history for assignment building
	var steps []crs.StepRecord
	if deps.Session != nil && deps.Session.HasCRS() {
		steps = deps.Session.GetCRS().GetStepHistory(deps.Session.ID)
	}

	// Build current assignment from step history
	currentAssignment := routing.BuildAssignmentFromSteps(steps)

	// Step 1: Check cache
	cacheKey := ucb1Ctx.stateKeyBuilder.BuildKey(steps, generation)
	if tool, score, ok := ucb1Ctx.cache.Get(cacheKey, generation); ok {
		routing.RecordUCB1CacheHit()
		slog.Debug("CRS-05: UCB1 cache hit",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", tool),
			slog.Float64("score", score),
		)
		return &agent.ToolRouterSelection{
			Tool:       tool,
			Confidence: score,
			Reasoning:  "UCB1 cache hit",
			Duration:   time.Since(startTime),
		}, true
	}
	routing.RecordUCB1CacheMiss()

	// Step 2: Check for forced move (unit propagation)
	// This also gives us the viable count to detect all-blocked scenario
	forcedResult := ucb1Ctx.forcedMoveChecker.CheckForcedMove(
		availableTools,
		clauseChecker,
		currentAssignment,
	)

	if forcedResult.IsForced {
		routing.RecordUCB1ForcedMove(forcedResult.ForcedTool)
		slog.Info("CRS-05: Forced move detected",
			slog.String("session_id", deps.Session.ID),
			slog.String("forced_tool", forcedResult.ForcedTool),
			slog.Int("blocked_count", len(forcedResult.BlockedTools)),
		)

		// Cache the forced selection
		ucb1Ctx.cache.Put(cacheKey, forcedResult.ForcedTool, 1.0, generation)
		ucb1Ctx.selectionCounts.Increment(forcedResult.ForcedTool)

		return &agent.ToolRouterSelection{
			Tool:       forcedResult.ForcedTool,
			Confidence: 1.0,
			Reasoning:  fmt.Sprintf("Forced move: only viable tool (blocked %d others)", len(forcedResult.BlockedTools)),
			Duration:   time.Since(startTime),
		}, true
	}

	// Step 3: Check if all tools are blocked (reuse forcedResult to avoid duplicate work)
	// CR-4 fix: Use ViableCount from CheckForcedMove instead of calling CheckAllBlocked
	if forcedResult.ViableCount == 0 && len(availableTools) > 0 {
		routing.RecordUCB1AllBlocked()
		slog.Warn("CRS-05: All tools blocked by clauses",
			slog.String("session_id", deps.Session.ID),
			slog.Int("blocked_count", len(forcedResult.BlockedTools)),
		)
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.7,
			Reasoning:  "All tools blocked by learned clauses - synthesizing answer",
			Duration:   time.Since(startTime),
		}, true
	}

	// Step 4: Build router results for UCB1 scoring
	// Use the router's selection as primary, give other tools lower base confidence
	routerResults := make([]routing.RouterResult, 0, len(availableTools))

	for _, tool := range availableTools {
		confidence := 0.3 // Base confidence for non-selected tools
		if tool == routerSelection.Tool {
			confidence = routerSelection.Confidence
		}
		routerResults = append(routerResults, routing.RouterResult{
			Tool:       tool,
			Confidence: confidence,
		})
	}

	// Step 5: Score tools using UCB1
	scores := ucb1Ctx.scorer.ScoreTools(
		routerResults,
		proofIndex,
		ucb1Ctx.selectionCounts.AsMap(),
		clauseChecker,
		currentAssignment,
	)

	// Step 6: Select best non-blocked tool
	bestTool, bestScore := ucb1Ctx.scorer.SelectBest(scores)

	if bestTool == "" {
		// All tools blocked (shouldn't happen after allBlockedResult check, but defensive)
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.7,
			Reasoning:  "No viable tools available",
			Duration:   time.Since(startTime),
		}, true
	}

	// Record metrics for selected tool
	routing.RecordUCB1Selection(bestTool, bestScore.FinalScore, bestScore.ProofPenalty, bestScore.ExplorationBonus)

	// Record blocked tools
	for _, score := range scores {
		if score.Blocked {
			routing.RecordUCB1BlockedSelection(score.Tool, "clause_violation")
		}
	}

	// Update selection count
	ucb1Ctx.selectionCounts.Increment(bestTool)

	// Cache the selection
	ucb1Ctx.cache.Put(cacheKey, bestTool, bestScore.FinalScore, generation)

	// Check if UCB1 changed the selection
	modified := bestTool != routerSelection.Tool

	if modified {
		slog.Info("CRS-05: UCB1 modified tool selection",
			slog.String("session_id", deps.Session.ID),
			slog.String("router_suggested", routerSelection.Tool),
			slog.String("ucb1_selected", bestTool),
			slog.Float64("router_conf", routerSelection.Confidence),
			slog.Float64("ucb1_score", bestScore.FinalScore),
			slog.Float64("proof_penalty", bestScore.ProofPenalty),
			slog.Float64("exploration_bonus", bestScore.ExplorationBonus),
		)
	} else {
		slog.Debug("CRS-05: UCB1 confirmed router selection",
			slog.String("session_id", deps.Session.ID),
			slog.String("tool", bestTool),
			slog.Float64("ucb1_score", bestScore.FinalScore),
		)
	}

	return &agent.ToolRouterSelection{
		Tool:       bestTool,
		Confidence: bestScore.FinalScore,
		Reasoning:  fmt.Sprintf("UCB1: router_conf=%.2f, proof_penalty=%.2f, exploration=%.2f", bestScore.RouterConfidence, bestScore.ProofPenalty, bestScore.ExplorationBonus),
		Duration:   time.Since(startTime),
	}, modified
}

// getAvailableToolNames extracts tool names from tool definitions.
//
// Inputs:
//
//	toolDefs - Tool definitions.
//
// Outputs:
//
//	[]string - List of tool names.
func getAvailableToolNames(toolDefs []tools.ToolDefinition) []string {
	names := make([]string, len(toolDefs))
	for i, def := range toolDefs {
		names[i] = def.Name
	}
	return names
}
