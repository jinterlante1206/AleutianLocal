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
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
)

// ClarifyPhase handles user clarification requests.
//
// This phase is responsible for:
//   - Generating clarification prompts for the user
//   - Processing user clarification responses
//   - Updating context with clarified information
//
// # When to Use CLARIFY State
//
// The CLARIFY state should ONLY be used when the agent genuinely cannot proceed
// without user input. It should NOT be used as a lazy fallback for broad questions.
//
// APPROPRIATE uses of CLARIFY:
//   - Project root not provided or invalid path
//   - User request contains contradictory requirements
//   - Multiple mutually exclusive interpretations exist (e.g., "fix the bug" when multiple bugs exist)
//   - Required information is truly missing (e.g., API keys, credentials)
//
// INAPPROPRIATE uses of CLARIFY (agent should explore instead):
//   - "Which file should I look at?" - explore the codebase
//   - "Which function do you mean?" - search for matches
//   - "What security concerns?" - analyze the code and report findings
//   - "Trace data flow" - start from entry points and explore
//
// The agent should be proactive: use tools to explore the codebase and provide
// comprehensive answers rather than asking the user to narrow the scope.
//
// Thread Safety: ClarifyPhase is safe for concurrent use.
type ClarifyPhase struct {
	mu sync.RWMutex

	// defaultPrompt is used when no specific prompt is provided.
	defaultPrompt string

	// clarificationInput stores the pending clarification (set externally).
	clarificationInput string
}

// ClarifyPhaseOption configures a ClarifyPhase.
type ClarifyPhaseOption func(*ClarifyPhase)

// WithDefaultPrompt sets the default clarification prompt.
//
// Inputs:
//
//	prompt - The default prompt.
//
// Outputs:
//
//	ClarifyPhaseOption - The configuration function.
func WithDefaultPrompt(prompt string) ClarifyPhaseOption {
	return func(p *ClarifyPhase) {
		p.defaultPrompt = prompt
	}
}

// NewClarifyPhase creates a new clarification phase.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*ClarifyPhase - The configured phase.
func NewClarifyPhase(opts ...ClarifyPhaseOption) *ClarifyPhase {
	p := &ClarifyPhase{
		defaultPrompt: "Could you please provide more details about your request?",
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
//	string - "clarify"
func (p *ClarifyPhase) Name() string {
	return "clarify"
}

// SetClarificationInput sets the user's clarification response.
//
// Description:
//
//	Called by the agent loop when the user provides clarification.
//	This should be called before Execute() when processing a clarification.
//
// Inputs:
//
//	input - The user's clarification text.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) SetClarificationInput(input string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clarificationInput = input
}

// ClearClarificationInput clears any pending clarification input.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) ClearClarificationInput() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clarificationInput = ""
}

// getClarificationInput returns the current clarification input.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) getClarificationInput() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clarificationInput
}

// Execute implements Phase.
//
// Description:
//
//	Processes the clarification state. If clarification input is available,
//	adds it to the context and transitions back to PLAN. Otherwise,
//	the phase signals that clarification is needed (returning an error
//	that indicates the session is awaiting user input).
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	deps - Phase dependencies.
//
// Outputs:
//
//	agent.AgentState - PLAN after clarification received, or CLARIFY if awaiting.
//	error - ErrAwaitingClarification if input needed, other errors on failure.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) Execute(ctx context.Context, deps *Dependencies) (agent.AgentState, error) {
	if err := p.validateDependencies(deps); err != nil {
		return agent.StateError, err
	}

	// Check if we have clarification input (thread-safe access)
	if p.getClarificationInput() == "" {
		// No input yet - return to signal we're awaiting clarification
		// The agent loop should pause and wait for user input
		return agent.StateClarify, agent.ErrAwaitingClarification
	}

	// Process the clarification
	return p.processClarification(ctx, deps)
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
func (p *ClarifyPhase) validateDependencies(deps *Dependencies) error {
	if deps == nil {
		return fmt.Errorf("dependencies are nil")
	}
	if deps.Session == nil {
		return fmt.Errorf("session is nil")
	}
	return nil
}

// processClarification integrates clarification into context.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//
// Outputs:
//
//	agent.AgentState - PLAN to re-analyze with clarification.
//	error - Non-nil if processing fails.
func (p *ClarifyPhase) processClarification(ctx context.Context, deps *Dependencies) (agent.AgentState, error) {
	// Get the clarification (thread-safe) and clear it atomically
	clarification := p.getAndClearClarification()

	// Add clarification to conversation history
	p.addClarificationToContext(deps, clarification)

	// Emit context update event
	p.emitContextUpdate(deps, clarification)

	// Transition back to PLAN to re-analyze with new information
	p.emitStateTransition(deps, agent.StateClarify, agent.StatePlan, "clarification received")

	return agent.StatePlan, nil
}

// getAndClearClarification atomically gets and clears the clarification input.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) getAndClearClarification() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	clarification := p.clarificationInput
	p.clarificationInput = ""
	return clarification
}

// addClarificationToContext adds the clarification to conversation history.
//
// Inputs:
//
//	deps - Phase dependencies.
//	clarification - The user's clarification text.
func (p *ClarifyPhase) addClarificationToContext(deps *Dependencies, clarification string) {
	if deps.Context == nil || deps.ContextManager == nil {
		return
	}

	// Add as a user message
	deps.ContextManager.AddMessage(deps.Context, "user", clarification)
	// Persist updated context to session
	deps.Session.SetCurrentContext(deps.Context)
}

// GetClarificationPrompt returns the prompt to show the user.
//
// Description:
//
//	Returns the appropriate clarification prompt based on context.
//	Should be called when the session enters CLARIFY state to get
//	the message to display to the user.
//
// Inputs:
//
//	deps - Phase dependencies (optional, used for context).
//
// Outputs:
//
//	string - The clarification prompt.
//
// Thread Safety: This method is safe for concurrent use.
func (p *ClarifyPhase) GetClarificationPrompt(deps *Dependencies) string {
	// Check if there's a specific prompt in the session
	if deps != nil && deps.Session != nil {
		prompt := deps.Session.GetClarificationPrompt()
		if prompt != "" {
			return prompt
		}
	}

	return p.defaultPrompt
}

// emitContextUpdate emits a context update event.
//
// Inputs:
//
//	deps - Phase dependencies.
//	clarification - The clarification text.
func (p *ClarifyPhase) emitContextUpdate(deps *Dependencies, clarification string) {
	if deps.EventEmitter == nil {
		return
	}

	tokensBefore := 0
	tokensAfter := 0
	if deps.Context != nil {
		tokensAfter = deps.Context.TotalTokens
		// Estimate tokens before (rough approximation)
		tokensBefore = tokensAfter - len(clarification)/4
		if tokensBefore < 0 {
			tokensBefore = 0
		}
	}

	deps.EventEmitter.Emit(events.TypeContextUpdate, &events.ContextUpdateData{
		Action:          "clarification",
		EntriesAffected: 1,
		TokensBefore:    tokensBefore,
		TokensAfter:     tokensAfter,
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
func (p *ClarifyPhase) emitStateTransition(deps *Dependencies, from, to agent.AgentState, reason string) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeStateTransition, &events.StateTransitionData{
		FromState: from,
		ToState:   to,
		Reason:    reason,
	})
}
