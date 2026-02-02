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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/llm"
)

// ReflectPhase handles progress evaluation and decision making.
//
// This phase is responsible for:
//   - Evaluating progress toward the goal
//   - Deciding whether to continue, complete, or request clarification
//   - Summarizing recent actions for context management
//
// Thread Safety: ReflectPhase is safe for concurrent use.
type ReflectPhase struct {
	// maxSteps is the maximum steps before forcing completion.
	maxSteps int

	// maxTokens is the maximum tokens before forcing completion.
	maxTokens int
}

// ReflectPhaseOption configures a ReflectPhase.
type ReflectPhaseOption func(*ReflectPhase)

// WithMaxSteps sets the maximum allowed steps.
//
// Inputs:
//
//	steps - Maximum step count.
//
// Outputs:
//
//	ReflectPhaseOption - The configuration function.
func WithMaxSteps(steps int) ReflectPhaseOption {
	return func(p *ReflectPhase) {
		p.maxSteps = steps
	}
}

// WithMaxTotalTokens sets the maximum total tokens.
//
// Inputs:
//
//	tokens - Maximum token count.
//
// Outputs:
//
//	ReflectPhaseOption - The configuration function.
func WithMaxTotalTokens(tokens int) ReflectPhaseOption {
	return func(p *ReflectPhase) {
		p.maxTokens = tokens
	}
}

// NewReflectPhase creates a new reflection phase.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*ReflectPhase - The configured phase.
func NewReflectPhase(opts ...ReflectPhaseOption) *ReflectPhase {
	p := &ReflectPhase{
		maxSteps:  50,
		maxTokens: 100000,
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
//	string - "reflect"
func (p *ReflectPhase) Name() string {
	return "reflect"
}

// Execute implements Phase.
//
// Description:
//
//	Evaluates the current progress and decides on the next action:
//	- EXECUTE: Continue with more steps
//	- COMPLETE: Task is done
//	- CLARIFY: Need user input
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	deps - Phase dependencies.
//
// Outputs:
//
//	agent.AgentState - Next state (EXECUTE, COMPLETE, or CLARIFY).
//	error - Non-nil only for unrecoverable errors.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ReflectPhase) Execute(ctx context.Context, deps *Dependencies) (agent.AgentState, error) {
	if err := p.validateDependencies(deps); err != nil {
		return agent.StateError, err
	}

	// Gather reflection input
	input := p.gatherReflectionInput(deps)

	// Check hard limits first
	if p.exceedsLimits(input) {
		return p.handleLimitExceeded(deps, input)
	}

	// Perform reflection analysis
	output := p.analyzeProgress(deps, input)

	// Emit reflection event
	p.emitReflection(deps, input, output)

	// Handle the decision
	return p.handleDecision(deps, output)
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
func (p *ReflectPhase) validateDependencies(deps *Dependencies) error {
	if deps == nil {
		return fmt.Errorf("dependencies are nil")
	}
	if deps.Session == nil {
		return fmt.Errorf("session is nil")
	}
	if deps.Context == nil {
		return fmt.Errorf("context is nil")
	}
	return nil
}

// gatherReflectionInput collects data for reflection analysis.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	*ReflectionInput - The gathered input data.
func (p *ReflectPhase) gatherReflectionInput(deps *Dependencies) *ReflectionInput {
	metrics := deps.Session.GetMetrics()

	input := &ReflectionInput{
		StepsCompleted: metrics.TotalSteps,
		TokensUsed:     deps.Context.TotalTokens,
		ToolsInvoked:   metrics.ToolCalls,
		RecentResults:  p.getRecentResults(deps),
	}

	// Get last response from conversation history
	if len(deps.Context.ConversationHistory) > 0 {
		lastMsg := deps.Context.ConversationHistory[len(deps.Context.ConversationHistory)-1]
		if lastMsg.Role == "assistant" {
			input.LastResponse = lastMsg.Content
		}
	}

	return input
}

// getRecentResults returns the most recent tool results.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	[]agent.ToolResult - Recent tool results (max 5).
func (p *ReflectPhase) getRecentResults(deps *Dependencies) []agent.ToolResult {
	results := deps.Context.ToolResults
	if len(results) > 5 {
		return results[len(results)-5:]
	}
	return results
}

// exceedsLimits checks if hard limits have been exceeded.
//
// Inputs:
//
//	input - The reflection input.
//
// Outputs:
//
//	bool - True if limits are exceeded.
func (p *ReflectPhase) exceedsLimits(input *ReflectionInput) bool {
	if input.StepsCompleted >= p.maxSteps {
		return true
	}
	if input.TokensUsed >= p.maxTokens {
		return true
	}
	return false
}

// handleLimitExceeded handles the case when limits are exceeded.
//
// Inputs:
//
//	deps - Phase dependencies.
//	input - The reflection input.
//
// Outputs:
//
//	agent.AgentState - COMPLETE.
//	error - Always nil.
func (p *ReflectPhase) handleLimitExceeded(deps *Dependencies, input *ReflectionInput) (agent.AgentState, error) {
	var reason string
	if input.StepsCompleted >= p.maxSteps {
		reason = "maximum steps reached"
	} else {
		reason = "maximum tokens reached"
	}

	// Synthesize a final response before completing
	p.synthesizeResponse(context.Background(), deps, reason)

	p.emitReflection(deps, input, &ReflectionOutput{
		Decision: DecisionComplete,
		Reason:   reason,
	})

	p.emitStateTransition(deps, agent.StateReflect, agent.StateComplete, reason)

	return agent.StateComplete, nil
}

// analyzeProgress evaluates progress and determines next action.
//
// Description:
//
//	Uses heuristics to decide whether to continue execution,
//	mark as complete, or request user clarification.
//	In practice, this could use the LLM for more sophisticated analysis.
//
// Inputs:
//
//	deps - Phase dependencies.
//	input - The reflection input data.
//
// Outputs:
//
//	*ReflectionOutput - The decision and reasoning.
//
// CLARIFY Policy:
//
//	CLARIFY should only be triggered when the agent is genuinely stuck after
//	multiple failed attempts. It should NOT be used for:
//	  - Broad questions (agent should explore the codebase)
//	  - "Which file/function?" questions (agent should search)
//	  - Requests that can be answered by exploration
//
//	CLARIFY IS appropriate when:
//	  - Multiple tool calls have failed repeatedly
//	  - The agent cannot make progress despite trying
//	  - There's a genuine blocker (missing permissions, invalid paths, etc.)
func (p *ReflectPhase) analyzeProgress(deps *Dependencies, input *ReflectionInput) *ReflectionOutput {
	// Check if recent results indicate completion
	if p.looksComplete(input) {
		return &ReflectionOutput{
			Decision: DecisionComplete,
			Reason:   "task appears complete based on recent results",
		}
	}

	// Check if we're stuck (repeated failures indicate a genuine blocker)
	// Note: This is the ONLY path to CLARIFY from Reflect - we don't go to
	// CLARIFY just because a question is broad or vague. The agent should
	// explore the codebase and provide comprehensive answers.
	if p.looksStuck(input) {
		return &ReflectionOutput{
			Decision:            DecisionClarify,
			Reason:              "multiple recent failures suggest clarification needed",
			ClarificationPrompt: "I've encountered some difficulties. Could you provide more details about what you're trying to accomplish?",
		}
	}

	// Default: continue execution
	return &ReflectionOutput{
		Decision: DecisionContinue,
		Reason:   "progress is being made, continuing execution",
	}
}

// looksComplete determines if the task appears complete.
//
// Inputs:
//
//	input - The reflection input.
//
// Outputs:
//
//	bool - True if the task looks complete.
func (p *ReflectPhase) looksComplete(input *ReflectionInput) bool {
	// If the last response mentions completion
	if input.LastResponse != "" {
		completionPhrases := []string{
			"complete",
			"finished",
			"done",
			"accomplished",
			"implemented",
		}
		for _, phrase := range completionPhrases {
			if containsIgnoreCase(input.LastResponse, phrase) {
				return true
			}
		}
	}

	// If all recent results are successful and there's substantial progress
	if len(input.RecentResults) >= 3 {
		allSuccessful := true
		for _, r := range input.RecentResults {
			if !r.Success {
				allSuccessful = false
				break
			}
		}
		if allSuccessful {
			return true
		}
	}

	return false
}

// looksStuck determines if the agent appears to be stuck.
//
// Inputs:
//
//	input - The reflection input.
//
// Outputs:
//
//	bool - True if the agent looks stuck.
func (p *ReflectPhase) looksStuck(input *ReflectionInput) bool {
	if len(input.RecentResults) < 3 {
		return false
	}

	// Count recent failures
	failures := 0
	for _, r := range input.RecentResults {
		if !r.Success {
			failures++
		}
	}

	// More than 60% failures indicates being stuck
	return float64(failures)/float64(len(input.RecentResults)) > 0.6
}

// handleDecision transitions to the appropriate state based on decision.
//
// Inputs:
//
//	deps - Phase dependencies.
//	output - The reflection decision.
//
// Outputs:
//
//	agent.AgentState - The next state.
//	error - Always nil.
func (p *ReflectPhase) handleDecision(deps *Dependencies, output *ReflectionOutput) (agent.AgentState, error) {
	var nextState agent.AgentState

	switch output.Decision {
	case DecisionContinue:
		nextState = agent.StateExecute
	case DecisionComplete:
		// Synthesize a final response before completing
		p.synthesizeResponse(context.Background(), deps, output.Reason)
		nextState = agent.StateComplete
	case DecisionClarify:
		nextState = agent.StateClarify
	default:
		nextState = agent.StateExecute
	}

	p.emitStateTransition(deps, agent.StateReflect, nextState, output.Reason)

	return nextState, nil
}

// emitReflection emits a reflection event.
//
// Inputs:
//
//	deps - Phase dependencies.
//	input - The reflection input.
//	output - The reflection output.
func (p *ReflectPhase) emitReflection(deps *Dependencies, input *ReflectionInput, output *ReflectionOutput) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeReflection, &events.ReflectionData{
		StepsCompleted: input.StepsCompleted,
		TokensUsed:     input.TokensUsed,
		Decision:       string(output.Decision),
		Reason:         output.Reason,
	})
}

// emitStateTransition emits a state transition event.
//
// Inputs:
//
//	deps - Phase dependencies.
//	from - The previous state.
//	to - The new state.
//	reason - The reason for the transition.
func (p *ReflectPhase) emitStateTransition(deps *Dependencies, from, to agent.AgentState, reason string) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeStateTransition, &events.StateTransitionData{
		FromState: from,
		ToState:   to,
		Reason:    reason,
	})
}

// synthesizeResponse generates a final response when completing without one.
//
// Description:
//
//	When the Reflect phase decides to complete (due to limits or decision),
//	but the agent hasn't produced a final text response yet, this method
//	asks the LLM to synthesize a summary of what was learned.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//	reason - The reason for completion.
func (p *ReflectPhase) synthesizeResponse(ctx context.Context, deps *Dependencies, reason string) {
	// Check if we already have a response
	if deps.Context != nil {
		for i := len(deps.Context.ConversationHistory) - 1; i >= 0; i-- {
			msg := deps.Context.ConversationHistory[i]
			if msg.Role == "assistant" && msg.Content != "" {
				// Already have a response, no need to synthesize
				slog.Debug("Skipping synthesis - response already exists",
					slog.String("session_id", deps.Session.ID),
				)
				return
			}
			// Stop if we hit a user message (no assistant response after it)
			if msg.Role == "user" {
				break
			}
		}
	}

	// No LLM client available, skip synthesis
	if deps.LLMClient == nil {
		slog.Warn("Cannot synthesize response - no LLM client",
			slog.String("session_id", deps.Session.ID),
		)
		return
	}

	slog.Info("Synthesizing final response",
		slog.String("session_id", deps.Session.ID),
		slog.String("reason", reason),
	)

	// Build synthesis prompt
	synthesisPrompt := "Based on the tools you used and information you gathered, please provide a concise summary answering the user's original question. Focus on the key findings and insights."

	// Add synthesis prompt to context
	if deps.Context != nil {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "user",
			Content: synthesisPrompt,
		})
	}

	// Build and send LLM request (no tools - just get a text response)
	request := llm.BuildRequest(deps.Context, nil, 4096)

	response, err := deps.LLMClient.Complete(ctx, request)
	if err != nil {
		slog.Error("Failed to synthesize response",
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Store the synthesized response
	if response.Content != "" && deps.Context != nil {
		deps.Context.ConversationHistory = append(deps.Context.ConversationHistory, agent.Message{
			Role:    "assistant",
			Content: response.Content,
		})
		// Persist to session
		deps.Session.SetCurrentContext(deps.Context)

		// Update token metrics
		if response.OutputTokens > 0 {
			deps.Session.IncrementMetric(agent.MetricTokens, response.OutputTokens)
		}

		slog.Info("Synthesized response stored",
			slog.String("session_id", deps.Session.ID),
			slog.Int("response_len", len(response.Content)),
			slog.Int("tokens", response.OutputTokens),
		)
	}
}
