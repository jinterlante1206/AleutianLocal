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
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/grounding"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/integration"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// executePhaseTracer is the OpenTelemetry tracer for the execute phase.
var executePhaseTracer = otel.Tracer("aleutian.agent.execute")

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
					// CRS-07: Added explicit prohibition of XML tool_call syntax after
					// trace_logs_30 showed GLM-4.7-flash outputting malformed XML that
					// crashed Ollama's parser.
					if circuitBreakerFired {
						synthesisPrompt := `You have already gathered information from tools. Now synthesize a complete answer.

CRITICAL - DO NOT OUTPUT ANY OF THESE PATTERNS:
- <tool_call> or </tool_call> XML tags
- <function> or </function> XML tags
- Any XML-formatted tool invocations
- Tool names by themselves without explanation

DO NOT:
- Say you need to call more tools
- Output raw tool names
- Say "I'll analyze..." without actually answering
- Use any XML syntax in your response

DO:
- Use the information already gathered from previous tool calls shown above
- Provide a direct, comprehensive answer in plain text
- If information is incomplete, state what you found and what's missing

Answer the user's question now based on the tool results shown above.`
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
		p.emitCoordinatorEvent(ctx, deps, integration.EventCircuitBreaker, nil, nil, cbReason, crs.ErrorCategoryInternal)

		// Force "answer" to synthesize a response from gathered information
		return &agent.ToolRouterSelection{
			Tool:       "answer",
			Confidence: 0.8,
			Reasoning:  fmt.Sprintf("Circuit breaker: %s. Synthesizing answer from gathered information.", cbReason),
			Duration:   selection.Duration,
		}
	}

	// CB-30c: Semantic repetition check
	// Detects when the same tool is being called with semantically similar queries.
	// This catches patterns like Grep("parseConfig") → Grep("parse_config") that
	// the simple count-based circuit breaker misses.
	if selection.Tool != "answer" && deps.Session != nil && deps.Query != "" {
		isRepetitive, similarity, similarQuery := p.checkSemanticRepetition(ctx, deps, selection.Tool, deps.Query)

		if isRepetitive {
			srReason := fmt.Sprintf("semantic repetition: query %.0f%% similar to previous '%s'",
				similarity*100, truncateQuery(similarQuery, 30))

			slog.Warn("CB-30c Semantic repetition: forcing answer",
				slog.String("session_id", deps.Session.ID),
				slog.String("tool", selection.Tool),
				slog.Float64("similarity", similarity),
				slog.String("similar_to", similarQuery),
			)

			span.SetAttributes(
				attribute.String("semantic_repetition", "detected"),
				attribute.Float64("similarity", similarity),
				attribute.String("similar_to", truncateQuery(similarQuery, 50)),
			)

			// Record metric
			grounding.RecordSemanticRepetition(selection.Tool, similarity, selection.Tool)

			// CRS-04: Learn from semantic repetition
			p.learnFromFailure(ctx, deps, crs.FailureEvent{
				SessionID:   deps.Session.ID,
				FailureType: crs.FailureTypeSemanticRepetition,
				Tool:        selection.Tool,
				Source:      crs.SignalSourceHard, // Jaccard is deterministic
			})

			// CRS-06: Emit EventSemanticRepetition to Coordinator
			p.emitCoordinatorEvent(ctx, deps, integration.EventSemanticRepetition, nil, nil, srReason, crs.ErrorCategoryInternal)

			// Force "answer" to synthesize
			return &agent.ToolRouterSelection{
				Tool:       "answer",
				Confidence: 0.8,
				Reasoning:  fmt.Sprintf("Semantic repetition: %s. Synthesizing answer from gathered information.", srReason),
				Duration:   selection.Duration,
			}
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

// maxRepeatedToolCalls is the circuit breaker threshold for repeated tool suggestions.
// If the router suggests a tool that has already been called this many times,
// we force selection of "answer" to synthesize from gathered information.
// This prevents infinite loops where the model ignores tool history.
// Fixed in cb_30a after trace_logs_18 showed 5+ repeated find_entry_points calls.
//
// NOTE: This must match crs.DefaultCircuitBreakerThreshold for consistent behavior.
const maxRepeatedToolCalls = crs.DefaultCircuitBreakerThreshold

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
