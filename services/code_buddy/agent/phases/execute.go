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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/grounding"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/tools"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
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
	request := p.buildLLMRequest(deps)

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

	// Check for completion (no tool calls)
	if !response.HasToolCalls() {
		slog.Info("No tool calls, completing",
			slog.String("session_id", deps.Session.ID),
		)
		return p.handleCompletion(ctx, deps, response, stepStart, stepNumber)
	}

	// Parse and execute tool calls
	invocations := llm.ParseToolCalls(response)
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
func (p *ExecutePhase) buildLLMRequest(deps *Dependencies) *llm.Request {
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

	// Try ToolRouter first for fast tool selection (micro LLM routing).
	// If the router returns a confident selection, use it. Otherwise, fall back
	// to the hybrid stack classifier.
	routerUsed := false
	if deps.Session != nil && deps.Session.IsToolRouterEnabled() && deps.Query != "" && len(toolDefs) > 0 {
		router := deps.Session.GetToolRouter()
		if router != nil {
			routerSelection := p.tryToolRouterSelection(context.Background(), deps, router, toolDefs)
			if routerSelection != nil {
				// Router returned a confident selection - use it
				request.ToolChoice = llm.ToolChoiceRequired(routerSelection.Tool)
				routerUsed = true

				slog.Info("ToolRouter selection applied",
					slog.String("session_id", deps.Session.ID),
					slog.String("selected_tool", routerSelection.Tool),
					slog.Float64("confidence", routerSelection.Confidence),
					slog.Duration("routing_duration", routerSelection.Duration),
					slog.String("reasoning", routerSelection.Reasoning),
				)

				// Emit routing event
				p.emitToolRouting(deps, routerSelection)
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

	return request
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
	selection, err := router.SelectTool(ctx, deps.Query, toolSpecs, codeContext)
	if err != nil {
		// Log but don't fail - we'll fall back to the classifier
		slog.Warn("ToolRouter selection failed, falling back to classifier",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		span.SetAttributes(attribute.String("fallback_reason", "router_error"))
		return nil
	}

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

	// Get recent tools from session history if available
	ctx.RecentTools = getRecentToolsFromSession(deps.Session)

	return ctx
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
// Inputs:
//
//	deps - Phase dependencies.
//	err - The LLM error.
//
// Outputs:
//
//	agent.AgentState - ERROR.
//	error - The original error.
func (p *ExecutePhase) handleLLMError(deps *Dependencies, err error) (agent.AgentState, error) {
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

	for _, inv := range invocations {
		// Refresh graph if dirty files exist (before tool queries stale data)
		p.maybeRefreshGraph(ctx, deps)

		// Emit tool invocation event
		p.emitToolInvocation(deps, &inv)

		// Run safety check if required
		if p.requireSafetyCheck {
			if p.isBlockedBySafety(ctx, deps, &inv) {
				blocked = true
				results = append(results, &tools.Result{
					Success: false,
					Error:   "blocked by safety check",
				})
				// Record blocked trace step
				p.recordTraceStep(deps, &inv, nil, 0, "blocked by safety check")
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
		}
		p.recordTraceStep(deps, &inv, result, toolDuration, errMsg)

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

// isBlockedBySafety checks if a tool invocation should be blocked.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	inv - The tool invocation.
//
// Outputs:
//
//	bool - True if the invocation should be blocked.
func (p *ExecutePhase) isBlockedBySafety(ctx context.Context, deps *Dependencies, inv *agent.ToolInvocation) bool {
	if deps.SafetyGate == nil {
		return false
	}

	// Build proposed change from invocation
	change := p.buildProposedChange(inv)
	if change == nil {
		return false
	}

	// Run safety check
	result, err := deps.SafetyGate.Check(ctx, []safety.ProposedChange{*change})
	if err != nil {
		// Safety check error - log but don't block
		p.emitError(deps, fmt.Errorf("safety check failed: %w", err), true)
		return false
	}

	// Emit safety check event
	p.emitSafetyCheck(deps, result)

	return deps.SafetyGate.ShouldBlock(result)
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
