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

// execute_retry.go contains retry, circuit breaker, and cycle detection functions
// extracted from execute.go as part of CB-30c Phase 2 decomposition.

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/integration"
)

// -----------------------------------------------------------------------------
// Retry Functions
// -----------------------------------------------------------------------------

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

// -----------------------------------------------------------------------------
// Tool Call Pattern Detection
// -----------------------------------------------------------------------------

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
// Circuit Breaker
// -----------------------------------------------------------------------------

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
		Timestamp:  time.Now().UnixMilli(),
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
