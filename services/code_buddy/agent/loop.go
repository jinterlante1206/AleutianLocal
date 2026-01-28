// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentLoop defines the interface for running agent sessions.
//
// The agent loop orchestrates the state machine, phases, tools, and LLM
// interactions to process user queries about codebases.
type AgentLoop interface {
	// Run starts a new session and processes a query.
	//
	// Description:
	//   Creates a new session, initializes the graph, assembles context,
	//   and executes the agent loop until completion or clarification needed.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   session - The session to run (must be in IDLE state).
	//   query - The user's query.
	//
	// Outputs:
	//   *RunResult - The execution result.
	//   error - Non-nil if an unrecoverable error occurred.
	//
	// Thread Safety: This method is safe for concurrent use with different sessions.
	Run(ctx context.Context, session *Session, query string) (*RunResult, error)

	// Continue resumes a session from CLARIFY state.
	//
	// Description:
	//   Processes the user's clarification response and continues execution.
	//   Only valid when session is in CLARIFY state.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   sessionID - The session ID to continue.
	//   clarification - The user's clarification response.
	//
	// Outputs:
	//   *RunResult - The execution result.
	//   error - Non-nil if session not found or not in CLARIFY state.
	//
	// Thread Safety: This method is safe for concurrent use.
	Continue(ctx context.Context, sessionID string, clarification string) (*RunResult, error)

	// Abort terminates a running session.
	//
	// Description:
	//   Stops a session that is currently executing. Does not affect
	//   sessions that are already in terminal states.
	//
	// Inputs:
	//   ctx - Context for the abort operation.
	//   sessionID - The session ID to abort.
	//
	// Outputs:
	//   error - Non-nil if session not found.
	//
	// Thread Safety: This method is safe for concurrent use.
	Abort(ctx context.Context, sessionID string) error

	// GetState returns the current state of a session.
	//
	// Inputs:
	//   sessionID - The session ID to query.
	//
	// Outputs:
	//   *SessionState - The session state.
	//   error - Non-nil if session not found.
	//
	// Thread Safety: This method is safe for concurrent use.
	GetState(sessionID string) (*SessionState, error)
}

// LoopDependencies contains dependencies for the agent loop.
type LoopDependencies struct {
	// StateMachine handles state transitions.
	StateMachine *StateMachine

	// PhaseRegistry maps states to phase implementations.
	PhaseRegistry PhaseRegistry

	// SessionStore persists session state.
	SessionStore SessionStore

	// EventHandler processes agent events.
	EventHandler func(event any)
}

// PhaseRegistry provides access to phase implementations.
type PhaseRegistry interface {
	// GetPhase returns the phase implementation for a state.
	GetPhase(state AgentState) (PhaseExecutor, bool)
}

// PhaseExecutor executes a phase and returns the next state.
type PhaseExecutor interface {
	// Execute runs the phase.
	Execute(ctx context.Context, deps any) (AgentState, error)

	// Name returns the phase name.
	Name() string
}

// SessionStore manages session persistence.
type SessionStore interface {
	// Get retrieves a session by ID.
	Get(id string) (*Session, bool)

	// Put stores a session.
	Put(session *Session)

	// Delete removes a session.
	Delete(id string)

	// List returns all active session IDs.
	List() []string
}

// DefaultAgentLoop implements the AgentLoop interface.
//
// Thread Safety: DefaultAgentLoop is safe for concurrent use.
type DefaultAgentLoop struct {
	mu sync.RWMutex

	// sessions stores active sessions.
	sessions SessionStore

	// stateMachine handles state transitions.
	stateMachine *StateMachine

	// phaseRegistry provides phase implementations.
	phaseRegistry PhaseRegistry

	// phaseDeps contains dependencies passed to phases.
	phaseDeps any

	// maxConcurrent limits concurrent sessions (0 = unlimited).
	maxConcurrent int

	// activeSessions tracks currently running sessions.
	activeSessions int
}

// DefaultLoopOption configures a DefaultAgentLoop.
type DefaultLoopOption func(*DefaultAgentLoop)

// WithMaxConcurrentSessions limits concurrent sessions.
//
// Inputs:
//
//	max - Maximum concurrent sessions (0 = unlimited).
//
// Outputs:
//
//	DefaultLoopOption - The configuration function.
func WithMaxConcurrentSessions(max int) DefaultLoopOption {
	return func(l *DefaultAgentLoop) {
		l.maxConcurrent = max
	}
}

// WithSessionStore sets the session store.
//
// Inputs:
//
//	store - The session store implementation.
//
// Outputs:
//
//	DefaultLoopOption - The configuration function.
func WithSessionStore(store SessionStore) DefaultLoopOption {
	return func(l *DefaultAgentLoop) {
		l.sessions = store
	}
}

// WithPhaseRegistry sets the phase registry.
//
// Inputs:
//
//	registry - The phase registry implementation.
//
// Outputs:
//
//	DefaultLoopOption - The configuration function.
func WithPhaseRegistry(registry PhaseRegistry) DefaultLoopOption {
	return func(l *DefaultAgentLoop) {
		l.phaseRegistry = registry
	}
}

// WithPhaseDependencies sets dependencies passed to phases.
//
// Inputs:
//
//	deps - Dependencies for phase execution.
//
// Outputs:
//
//	DefaultLoopOption - The configuration function.
func WithPhaseDependencies(deps any) DefaultLoopOption {
	return func(l *DefaultAgentLoop) {
		l.phaseDeps = deps
	}
}

// NewDefaultAgentLoop creates a new agent loop.
//
// Description:
//
//	Creates an agent loop with the specified options. If no session store
//	is provided, uses an in-memory store. If no phase registry is provided,
//	phases must be registered separately.
//
// Inputs:
//
//	opts - Configuration options.
//
// Outputs:
//
//	*DefaultAgentLoop - The configured agent loop.
func NewDefaultAgentLoop(opts ...DefaultLoopOption) *DefaultAgentLoop {
	l := &DefaultAgentLoop{
		sessions:     NewInMemorySessionStore(),
		stateMachine: DefaultStateMachine,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Run implements AgentLoop.
//
// Description:
//
//	Executes the agent loop for a new query. The session must be in IDLE state.
//	The loop runs until reaching a terminal state (COMPLETE, ERROR) or
//	CLARIFY state (which pauses for user input).
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	session - The session to run.
//	query - The user's query.
//
// Outputs:
//
//	*RunResult - The execution result.
//	error - Non-nil if an unrecoverable error occurred.
//
// Thread Safety: This method is safe for concurrent use with different sessions.
func (l *DefaultAgentLoop) Run(ctx context.Context, session *Session, query string) (*RunResult, error) {
	if err := l.validateRunInput(session, query); err != nil {
		return nil, err
	}

	// Try to acquire the session
	if !session.TryAcquire() {
		return nil, ErrSessionInProgress
	}
	defer session.Release()

	// Check concurrent session limit
	if err := l.acquireSlot(); err != nil {
		return nil, err
	}
	defer l.releaseSlot()

	// Store the session
	l.sessions.Put(session)

	// Store the query
	session.LastQuery = query

	// Transition to INIT
	if err := l.transition(session, StateInit, "query received"); err != nil {
		return nil, err
	}

	// Run the main loop
	return l.runLoop(ctx, session)
}

// Continue implements AgentLoop.
//
// Description:
//
//	Resumes execution after user provides clarification. The session must be
//	in CLARIFY state. The clarification is added to the context and execution
//	continues.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	sessionID - The session ID to continue.
//	clarification - The user's clarification response.
//
// Outputs:
//
//	*RunResult - The execution result.
//	error - Non-nil if session not found or invalid state.
//
// Thread Safety: This method is safe for concurrent use.
func (l *DefaultAgentLoop) Continue(ctx context.Context, sessionID string, clarification string) (*RunResult, error) {
	session, ok := l.sessions.Get(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}

	if session.GetState() != StateClarify {
		return nil, ErrNotInClarifyState
	}

	if !session.TryAcquire() {
		return nil, ErrSessionInProgress
	}
	defer session.Release()

	// Check concurrent session limit
	if err := l.acquireSlot(); err != nil {
		return nil, err
	}
	defer l.releaseSlot()

	// Add clarification to context (this would be handled by the clarify phase)
	// For now, we just store it in the session
	session.AddHistoryEntry(HistoryEntry{
		Type:  "clarification",
		Input: clarification,
		Query: clarification,
	})

	// Run the main loop (it will transition from CLARIFY to PLAN)
	return l.runLoop(ctx, session)
}

// Abort implements AgentLoop.
//
// Description:
//
//	Terminates a running session by transitioning it to ERROR state.
//	If the session is already in a terminal state, this is a no-op.
//
// Inputs:
//
//	ctx - Context for the abort operation.
//	sessionID - The session ID to abort.
//
// Outputs:
//
//	error - Non-nil if session not found.
//
// Thread Safety: This method is safe for concurrent use.
func (l *DefaultAgentLoop) Abort(ctx context.Context, sessionID string) error {
	session, ok := l.sessions.Get(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	// If already terminated, nothing to do
	if session.IsTerminated() {
		return nil
	}

	// Force transition to ERROR
	session.SetState(StateError)
	session.AddHistoryEntry(HistoryEntry{
		Type:  "abort",
		Error: "session aborted by user",
	})

	return nil
}

// GetState implements AgentLoop.
//
// Description:
//
//	Returns the current state of a session without modifying it.
//
// Inputs:
//
//	sessionID - The session ID to query.
//
// Outputs:
//
//	*SessionState - The session state.
//	error - Non-nil if session not found.
//
// Thread Safety: This method is safe for concurrent use.
func (l *DefaultAgentLoop) GetState(sessionID string) (*SessionState, error) {
	session, ok := l.sessions.Get(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}

	return session.ToSessionState(), nil
}

// validateRunInput validates inputs for Run.
func (l *DefaultAgentLoop) validateRunInput(session *Session, query string) error {
	if session == nil {
		return ErrInvalidSession
	}
	if query == "" {
		return ErrEmptyQuery
	}
	if session.GetState() != StateIdle {
		return ErrInvalidTransition
	}
	return nil
}

// acquireSlot attempts to acquire a concurrent session slot.
func (l *DefaultAgentLoop) acquireSlot() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.maxConcurrent > 0 && l.activeSessions >= l.maxConcurrent {
		return fmt.Errorf("maximum concurrent sessions reached (%d)", l.maxConcurrent)
	}

	l.activeSessions++
	return nil
}

// releaseSlot releases a concurrent session slot.
func (l *DefaultAgentLoop) releaseSlot() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.activeSessions--
}

// transition attempts a state transition.
func (l *DefaultAgentLoop) transition(session *Session, newState AgentState, reason string) error {
	currentState := session.GetState()

	if err := l.stateMachine.Transition(session, newState); err != nil {
		return err
	}

	// Record the transition
	session.AddHistoryEntry(HistoryEntry{
		Type:  "state_transition",
		Input: fmt.Sprintf("%s -> %s: %s", currentState, newState, reason),
	})

	return nil
}

// runLoop executes the main agent loop.
func (l *DefaultAgentLoop) runLoop(ctx context.Context, session *Session) (*RunResult, error) {
	startTime := time.Now()

	for {
		// Check context cancellation
		if err := ctx.Err(); err != nil {
			session.SetState(StateError)
			return l.buildErrorResult(session, ErrCanceled, startTime), nil
		}

		// Check timeout
		if session.Config.TotalTimeout > 0 && time.Since(startTime) > session.Config.TotalTimeout {
			session.SetState(StateError)
			return l.buildErrorResult(session, ErrTimeout, startTime), nil
		}

		currentState := session.GetState()

		// Check for terminal states
		if currentState.IsTerminal() {
			return l.buildResult(session, startTime), nil
		}

		// Check for CLARIFY state (pause for user input)
		if currentState == StateClarify {
			return l.buildClarifyResult(session, startTime), nil
		}

		// Execute the current phase
		nextState, err := l.executePhase(ctx, session)
		if err != nil {
			// Check if it's awaiting clarification (not a real error)
			if err == ErrAwaitingClarification {
				return l.buildClarifyResult(session, startTime), nil
			}

			session.SetState(StateError)
			return l.buildErrorResult(session, err, startTime), nil
		}

		// Transition to the next state
		if nextState != currentState {
			if err := l.transition(session, nextState, "phase completed"); err != nil {
				session.SetState(StateError)
				return l.buildErrorResult(session, err, startTime), nil
			}
		}

		session.IncrementMetric(MetricSteps, 1)
	}
}

// executePhase runs the phase for the current state.
func (l *DefaultAgentLoop) executePhase(ctx context.Context, session *Session) (AgentState, error) {
	currentState := session.GetState()

	if l.phaseRegistry == nil {
		// No phases registered - default behavior based on state
		return l.defaultPhaseExecution(session)
	}

	phase, ok := l.phaseRegistry.GetPhase(currentState)
	if !ok {
		return l.defaultPhaseExecution(session)
	}

	// Execute the phase with dependencies
	return phase.Execute(ctx, l.phaseDeps)
}

// defaultPhaseExecution provides default state transitions when no phase is registered.
func (l *DefaultAgentLoop) defaultPhaseExecution(session *Session) (AgentState, error) {
	switch session.GetState() {
	case StateInit:
		return StatePlan, nil
	case StatePlan:
		return StateExecute, nil
	case StateExecute:
		return StateComplete, nil
	case StateReflect:
		return StateExecute, nil
	case StateClarify:
		return StateClarify, ErrAwaitingClarification
	case StateDegraded:
		return StatePlan, nil
	default:
		return StateError, fmt.Errorf("no phase registered for state %s", session.GetState())
	}
}

// buildResult creates a RunResult for a completed session.
func (l *DefaultAgentLoop) buildResult(session *Session, startTime time.Time) *RunResult {
	result := &RunResult{
		State:      session.GetState(),
		TokensUsed: session.Metrics.TotalTokens,
		StepsTaken: session.Metrics.TotalSteps,
		ToolsUsed:  l.collectToolInvocations(session),
	}

	// Add response if complete
	if session.GetState() == StateComplete {
		result.Response = l.getLastAssistantMessage(session)
	}

	return result
}

// buildClarifyResult creates a RunResult for a session needing clarification.
func (l *DefaultAgentLoop) buildClarifyResult(session *Session, startTime time.Time) *RunResult {
	return &RunResult{
		State:      StateClarify,
		TokensUsed: session.Metrics.TotalTokens,
		StepsTaken: session.Metrics.TotalSteps,
		ToolsUsed:  l.collectToolInvocations(session),
		NeedsClarify: &ClarifyRequest{
			Question: session.GetClarificationPrompt(),
			Context:  "Additional information needed to proceed",
		},
	}
}

// buildErrorResult creates a RunResult for an error.
func (l *DefaultAgentLoop) buildErrorResult(session *Session, err error, startTime time.Time) *RunResult {
	return &RunResult{
		State:      StateError,
		TokensUsed: session.Metrics.TotalTokens,
		StepsTaken: session.Metrics.TotalSteps,
		ToolsUsed:  l.collectToolInvocations(session),
		Error: &AgentError{
			Code:        "EXECUTION_ERROR",
			Message:     err.Error(),
			Recoverable: false,
		},
	}
}

// collectToolInvocations gathers all tool invocations from history.
func (l *DefaultAgentLoop) collectToolInvocations(session *Session) []ToolInvocation {
	var invocations []ToolInvocation

	for i, entry := range session.GetHistory() {
		if entry.Type == "tool_call" && entry.ToolName != "" {
			invocations = append(invocations, ToolInvocation{
				ID:         fmt.Sprintf("inv_%d", i),
				Tool:       entry.ToolName,
				StepNumber: entry.Step,
			})
		}
	}

	return invocations
}

// getLastAssistantMessage returns the last assistant response.
func (l *DefaultAgentLoop) getLastAssistantMessage(session *Session) string {
	ctx := session.GetCurrentContext()
	if ctx == nil {
		return ""
	}

	for i := len(ctx.ConversationHistory) - 1; i >= 0; i-- {
		if ctx.ConversationHistory[i].Role == "assistant" {
			return ctx.ConversationHistory[i].Content
		}
	}

	return ""
}

// InMemorySessionStore is a simple in-memory session store.
//
// Thread Safety: InMemorySessionStore is safe for concurrent use.
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewInMemorySessionStore creates a new in-memory session store.
//
// Outputs:
//
//	*InMemorySessionStore - The new store.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*Session),
	}
}

// Get implements SessionStore.
func (s *InMemorySessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

// Put implements SessionStore.
func (s *InMemorySessionStore) Put(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

// Delete implements SessionStore.
func (s *InMemorySessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// List implements SessionStore.
func (s *InMemorySessionStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}
