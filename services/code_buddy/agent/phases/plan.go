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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/events"
)

// PlanPhase handles context assembly and execution preparation.
//
// This phase is responsible for:
//   - Assembling initial context for the user's query
//   - Detecting ambiguous queries that need clarification
//   - Preparing the context for the execution phase
//
// Thread Safety: PlanPhase is safe for concurrent use.
type PlanPhase struct {
	// initialBudget is the token budget for initial context assembly.
	initialBudget int
}

// PlanPhaseOption configures a PlanPhase.
type PlanPhaseOption func(*PlanPhase)

// WithInitialBudget sets the initial token budget.
//
// Inputs:
//
//	budget - The token budget.
//
// Outputs:
//
//	PlanPhaseOption - The configuration function.
func WithInitialBudget(budget int) PlanPhaseOption {
	return func(p *PlanPhase) {
		p.initialBudget = budget
	}
}

// NewPlanPhase creates a new planning phase.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*PlanPhase - The configured phase.
func NewPlanPhase(opts ...PlanPhaseOption) *PlanPhase {
	p := &PlanPhase{
		initialBudget: 8000, // Default budget
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
//	string - "plan"
func (p *PlanPhase) Name() string {
	return "plan"
}

// Execute implements Phase.
//
// Description:
//
//	Assembles initial context for the user's query. If the query
//	is ambiguous or context assembly fails, may transition to
//	CLARIFY state to request user input.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	deps - Phase dependencies including context manager.
//
// Outputs:
//
//	agent.AgentState - EXECUTE on success, CLARIFY if ambiguous, ERROR on failure.
//	error - Non-nil only for unrecoverable errors.
//
// Thread Safety: This method is safe for concurrent use.
func (p *PlanPhase) Execute(ctx context.Context, deps *Dependencies) (agent.AgentState, error) {
	slog.Info("PlanPhase starting",
		slog.String("session_id", deps.Session.ID),
		slog.String("query", deps.Query),
	)

	if err := p.validateDependencies(deps); err != nil {
		slog.Error("PlanPhase validation failed", slog.String("error", err.Error()))
		return agent.StateError, err
	}

	// Check if query needs clarification
	if p.isQueryAmbiguous(deps.Query) {
		slog.Info("Query is ambiguous, requesting clarification",
			slog.String("session_id", deps.Session.ID),
		)
		return p.handleAmbiguousQuery(deps)
	}

	// If ContextManager is available, assemble initial context
	if deps.ContextManager != nil {
		slog.Info("Assembling context with ContextManager",
			slog.String("session_id", deps.Session.ID),
		)
		assembledContext, err := p.assembleContext(ctx, deps)
		if err != nil {
			slog.Error("Context assembly failed",
				slog.String("session_id", deps.Session.ID),
				slog.String("error", err.Error()),
			)
			return p.handleAssemblyError(deps, err)
		}

		// Store context in dependencies for execute phase
		deps.Context = assembledContext

		// Persist context to session for cross-phase access
		deps.Session.SetCurrentContext(assembledContext)

		slog.Info("Context assembled successfully",
			slog.String("session_id", deps.Session.ID),
			slog.Int("total_tokens", assembledContext.TotalTokens),
			slog.Int("code_entries", len(assembledContext.CodeContext)),
		)

		// Emit context update event
		p.emitContextUpdate(deps, assembledContext)
	} else {
		// Create minimal context without ContextManager (degraded mode)
		slog.Info("Creating minimal context (degraded mode)",
			slog.String("session_id", deps.Session.ID),
		)
		assembledContext := &agent.AssembledContext{
			SystemPrompt: "You are a helpful code assistant. Answer questions about the codebase.",
			CodeContext:  []agent.CodeEntry{},
			TotalTokens:  0,
			// Include the user's query in conversation history
			ConversationHistory: []agent.Message{
				{
					Role:    "user",
					Content: deps.Query,
				},
			},
		}
		deps.Context = assembledContext

		// Persist context to session for cross-phase access
		deps.Session.SetCurrentContext(assembledContext)

		slog.Info("Minimal context created and stored in session",
			slog.String("session_id", deps.Session.ID),
			slog.Int("history_len", len(assembledContext.ConversationHistory)),
		)
	}

	// Emit state transition
	p.emitStateTransition(deps, agent.StatePlan, agent.StateExecute, "context ready")

	slog.Info("PlanPhase completed, transitioning to Execute",
		slog.String("session_id", deps.Session.ID),
	)

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
func (p *PlanPhase) validateDependencies(deps *Dependencies) error {
	if deps == nil {
		return fmt.Errorf("dependencies are nil")
	}
	if deps.Session == nil {
		return fmt.Errorf("session is nil")
	}
	if deps.Query == "" {
		return fmt.Errorf("query is empty")
	}
	// ContextManager is optional - we'll skip context assembly if nil
	return nil
}

// isQueryAmbiguous determines if a query needs clarification.
//
// Description:
//
//	Checks if the query is too vague, contains conflicting requirements,
//	or is otherwise unsuitable for direct execution.
//
// Inputs:
//
//	query - The user's query.
//
// Outputs:
//
//	bool - True if the query needs clarification.
func (p *PlanPhase) isQueryAmbiguous(query string) bool {
	// Simple heuristics for ambiguity detection
	// In practice, this could use the LLM for more sophisticated analysis

	// Very short queries are often ambiguous
	if len(query) < 10 {
		return true
	}

	// Check for explicitly ambiguous phrases
	ambiguousPhrases := []string{
		"something",
		"anything",
		"whatever",
		"maybe",
		"not sure",
	}

	for _, phrase := range ambiguousPhrases {
		if containsIgnoreCase(query, phrase) {
			return true
		}
	}

	return false
}

// handleAmbiguousQuery transitions to clarify state.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	agent.AgentState - CLARIFY.
//	error - Always nil.
func (p *PlanPhase) handleAmbiguousQuery(deps *Dependencies) (agent.AgentState, error) {
	p.emitStateTransition(deps, agent.StatePlan, agent.StateClarify, "query requires clarification")

	return agent.StateClarify, nil
}

// assembleContext creates the initial context for the query.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies.
//
// Outputs:
//
//	*agent.AssembledContext - The assembled context.
//	error - Non-nil if assembly fails.
func (p *PlanPhase) assembleContext(ctx context.Context, deps *Dependencies) (*agent.AssembledContext, error) {
	assembled, err := deps.ContextManager.Assemble(ctx, deps.Query, p.initialBudget)
	if err != nil {
		return nil, fmt.Errorf("assemble context: %w", err)
	}

	return assembled, nil
}

// handleAssemblyError handles context assembly errors.
//
// Inputs:
//
//	deps - Phase dependencies.
//	err - The assembly error.
//
// Outputs:
//
//	agent.AgentState - ERROR or CLARIFY depending on error type.
//	error - The original error if unrecoverable.
func (p *PlanPhase) handleAssemblyError(deps *Dependencies, err error) (agent.AgentState, error) {
	p.emitError(deps, err, false)

	// Context assembly failure is typically unrecoverable
	return agent.StateError, err
}

// emitContextUpdate emits a context update event.
//
// Inputs:
//
//	deps - Phase dependencies.
//	assembled - The assembled context.
func (p *PlanPhase) emitContextUpdate(deps *Dependencies, assembled *agent.AssembledContext) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeContextUpdate, &events.ContextUpdateData{
		Action:          "initial",
		EntriesAffected: len(assembled.CodeContext),
		TokensBefore:    0,
		TokensAfter:     assembled.TotalTokens,
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
func (p *PlanPhase) emitStateTransition(deps *Dependencies, from, to agent.AgentState, reason string) {
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
//
// Inputs:
//
//	deps - Phase dependencies.
//	err - The error that occurred.
//	recoverable - Whether the error is recoverable.
func (p *PlanPhase) emitError(deps *Dependencies, err error, recoverable bool) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeError, &events.ErrorData{
		Error:       err.Error(),
		Recoverable: recoverable,
	})
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
//
// Description:
//
//	Performs case-insensitive substring search using the standard library.
//
// Inputs:
//
//	s - The string to search in.
//	substr - The substring to search for.
//
// Outputs:
//
//	bool - True if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
