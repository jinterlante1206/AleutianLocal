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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/tools"
)

// ExecutePhase handles the main tool execution loop.
//
// This phase is responsible for:
//   - Sending requests to the LLM with current context
//   - Parsing and executing tool calls from LLM responses
//   - Running safety checks on proposed changes
//   - Updating context with tool results
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
	p := &ExecutePhase{
		maxTokens:           4096,
		reflectionThreshold: 10,
		requireSafetyCheck:  true,
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
	slog.Info("ExecutePhase starting",
		slog.String("session_id", deps.Session.ID),
		slog.String("query", deps.Query),
	)

	if err := p.validateDependencies(deps); err != nil {
		slog.Error("ExecutePhase validation failed", slog.String("error", err.Error()))
		return agent.StateError, err
	}

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
		return p.handleCompletion(deps, response, stepStart, stepNumber)
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
	if deps.ToolRegistry != nil {
		toolDefs = deps.ToolRegistry.GetDefinitions()
	}

	return llm.BuildRequest(deps.Context, toolDefs, p.maxTokens)
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
// Inputs:
//
//	deps - Phase dependencies.
//	response - The LLM response.
//	stepStart - When the step started.
//	stepNumber - The step number.
//
// Outputs:
//
//	agent.AgentState - COMPLETE.
//	error - Always nil.
func (p *ExecutePhase) handleCompletion(deps *Dependencies, response *llm.Response, stepStart time.Time, stepNumber int) (agent.AgentState, error) {
	slog.Info("Handling completion",
		slog.String("session_id", deps.Session.ID),
		slog.Int("output_tokens", response.OutputTokens),
		slog.Int("response_len", len(response.Content)),
	)

	// Update session metrics with token usage
	if response.OutputTokens > 0 {
		deps.Session.IncrementMetric(agent.MetricTokens, response.OutputTokens)
		slog.Info("Updated token metrics",
			slog.String("session_id", deps.Session.ID),
			slog.Int("tokens_added", response.OutputTokens),
		)
	}
	deps.Session.IncrementMetric(agent.MetricLLMCalls, 1)

	// Add response to context conversation history
	if deps.ContextManager != nil {
		deps.ContextManager.AddMessage(deps.Context, "assistant", response.Content)
		slog.Debug("Added response via ContextManager")
	} else if deps.Context != nil {
		// In degraded mode without ContextManager, add directly to conversation history
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "assistant",
			Content: response.Content,
		})
		// Persist updated context to session
		deps.Session.SetCurrentContext(deps.Context)
		slog.Info("Stored response in session context (degraded mode)",
			slog.String("session_id", deps.Session.ID),
			slog.Int("history_len", len(deps.Context.ConversationHistory)),
		)
	} else {
		slog.Warn("Cannot store response - no context available",
			slog.String("session_id", deps.Session.ID),
		)
	}

	// Emit step complete
	p.emitStepComplete(deps, stepStart, stepNumber, 0)

	// Transition to complete
	p.emitStateTransition(deps, agent.StateExecute, agent.StateComplete, "task completed")

	slog.Info("ExecutePhase completed successfully",
		slog.String("session_id", deps.Session.ID),
	)

	return agent.StateComplete, nil
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
				continue
			}
		}

		// Execute the tool
		result := p.executeSingleTool(ctx, deps, &inv)
		results = append(results, result)

		// Emit tool result event
		p.emitToolResult(deps, &inv, result)
	}

	return results, blocked
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

		_, err := deps.ContextManager.Update(ctx, deps.Context, result)
		if err != nil {
			p.emitError(deps, fmt.Errorf("context update failed: %w", err), true)
		}
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
