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
	"log/slog"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// Session Cleanup Hooks
// =============================================================================

// SessionCleanupFunc is a function called when a session is closed.
// It receives the session ID to clean up.
type SessionCleanupFunc func(sessionID string)

// sessionCleanupHooks stores registered cleanup functions.
// Key is a unique name for the hook, value is the cleanup function.
var sessionCleanupHooks = struct {
	mu    sync.RWMutex
	hooks map[string]SessionCleanupFunc
}{
	hooks: make(map[string]SessionCleanupFunc),
}

// RegisterSessionCleanupHook registers a cleanup function to be called
// when sessions are closed.
//
// Description:
//
//	Phases and other components can register cleanup functions that will be
//	called when CloseSession is invoked. This allows components to clean up
//	session-specific state without creating import cycles.
//
// Inputs:
//
//	name - Unique name for this hook (used for debugging and unregistration).
//	fn - The cleanup function to call.
//
// Thread Safety: Safe for concurrent use.
func RegisterSessionCleanupHook(name string, fn SessionCleanupFunc) {
	sessionCleanupHooks.mu.Lock()
	defer sessionCleanupHooks.mu.Unlock()
	sessionCleanupHooks.hooks[name] = fn
	slog.Debug("Registered session cleanup hook", slog.String("name", name))
}

// UnregisterSessionCleanupHook removes a cleanup hook.
//
// Inputs:
//
//	name - The name of the hook to remove.
//
// Thread Safety: Safe for concurrent use.
func UnregisterSessionCleanupHook(name string) {
	sessionCleanupHooks.mu.Lock()
	defer sessionCleanupHooks.mu.Unlock()
	delete(sessionCleanupHooks.hooks, name)
}

// runSessionCleanupHooks calls all registered cleanup hooks for a session.
func runSessionCleanupHooks(sessionID string) {
	sessionCleanupHooks.mu.RLock()
	defer sessionCleanupHooks.mu.RUnlock()

	for name, fn := range sessionCleanupHooks.hooks {
		slog.Debug("Running session cleanup hook",
			slog.String("hook", name),
			slog.String("session_id", sessionID),
		)
		fn(sessionID)
	}
}

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

	// GetSession returns the full session object.
	//
	// Description:
	//   Returns the complete Session for operations that need access
	//   to CRS, trace recorder, or other internal state not exposed
	//   through SessionState.
	//
	// Inputs:
	//   sessionID - The session ID to retrieve.
	//
	// Outputs:
	//   *Session - The full session object.
	//   error - Non-nil if session not found.
	//
	// Thread Safety: This method is safe for concurrent use.
	GetSession(sessionID string) (*Session, error)

	// CloseSession permanently closes a session and releases all resources.
	//
	// Description:
	//   Closes a session that will no longer be used. This cleans up all
	//   session-specific state including UCB1 contexts, caches, and removes
	//   the session from the session store. Unlike Abort, this is for
	//   intentional cleanup when a session is definitively finished.
	//
	//   Call this when:
	//   - User explicitly ends the conversation
	//   - Session timeout for inactive sessions
	//   - Application shutdown cleanup
	//
	// Inputs:
	//   sessionID - The session ID to close.
	//
	// Outputs:
	//   error - Non-nil if session not found.
	//
	// Thread Safety: This method is safe for concurrent use.
	CloseSession(sessionID string) error
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

// DependenciesFactory creates Dependencies for a session.
//
// Description:
//
//	DependenciesFactory is called by the agent loop to create Dependencies
//	for each phase execution. This allows per-session configuration of
//	dependencies while reusing shared components like LLM clients.
type DependenciesFactory interface {
	// Create builds Dependencies for the given session and query.
	//
	// Inputs:
	//   session - The current session.
	//   query - The user's query.
	//
	// Outputs:
	//   any - The dependencies (typically *phases.Dependencies).
	//   error - Non-nil if creation failed.
	Create(session *Session, query string) (any, error)
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

	// phaseDeps contains static dependencies passed to phases.
	// Used when depsFactory is not set.
	phaseDeps any

	// depsFactory creates per-session dependencies.
	// Takes precedence over phaseDeps when set.
	depsFactory DependenciesFactory

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

// WithDependenciesFactory sets a factory for creating per-session dependencies.
//
// Description:
//
//	When set, the factory is called to create Dependencies for each
//	phase execution. This is preferred over WithPhaseDependencies
//	for production use as it allows per-session configuration.
//
// Inputs:
//
//	factory - The dependencies factory.
//
// Outputs:
//
//	DefaultLoopOption - The configuration function.
func WithDependenciesFactory(factory DependenciesFactory) DefaultLoopOption {
	return func(l *DefaultAgentLoop) {
		l.depsFactory = factory
	}
}

// WithPhaseDependencies sets static dependencies passed to phases.
//
// Description:
//
//	Sets static dependencies that are passed to all phases. Use
//	WithDependenciesFactory for per-session dependency creation.
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
		slog.Error("Agent loop validation failed", slog.String("error", err.Error()))
		return nil, err
	}

	slog.Info("Agent loop starting",
		slog.String("session_id", session.ID),
		slog.String("project_root", session.ProjectRoot),
		slog.Int("query_len", len(query)),
	)

	// Try to acquire the session
	if !session.TryAcquire() {
		slog.Warn("Session already in progress", slog.String("session_id", session.ID))
		return nil, ErrSessionInProgress
	}
	defer session.Release()

	// Check concurrent session limit
	if err := l.acquireSlot(); err != nil {
		slog.Warn("Concurrent session limit reached", slog.String("error", err.Error()))
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
//	Resumes execution after user provides clarification or follow-up question.
//	Accepts sessions in CLARIFY state (waiting for clarification) or COMPLETE
//	state (for multi-turn follow-up questions).
//
//	For CLARIFY state: Provides the requested clarification and continues.
//	For COMPLETE state: Treats input as a follow-up question with conversation
//	history preserved, enabling multi-turn conversations.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	sessionID - The session ID to continue.
//	clarification - The user's clarification or follow-up question.
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

	currentState := session.GetState()

	// Accept both CLARIFY (awaiting clarification) and COMPLETE (follow-up question)
	if currentState != StateClarify && currentState != StateComplete {
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

	// Handle based on current state
	if currentState == StateComplete {
		// Follow-up question on completed session
		slog.Info("Continuing completed session with follow-up",
			slog.String("session_id", session.ID),
			slog.Int("follow_up_len", len(clarification)),
		)

		// Update the query for the new turn
		session.LastQuery = clarification

		// Add follow-up to history
		session.AddHistoryEntry(HistoryEntry{
			Type:  "follow_up",
			Input: clarification,
			Query: clarification,
		})

		// Add to conversation context if available
		sessionCtx := session.GetCurrentContext()
		if sessionCtx != nil {
			sessionCtx.ConversationHistory = append(sessionCtx.ConversationHistory, Message{
				Role:    "user",
				Content: clarification,
			})
			session.SetCurrentContext(sessionCtx)
		}

		// Transition back to PLAN to process the follow-up (via state machine)
		if err := l.transition(session, StatePlan, "multi-turn follow-up"); err != nil {
			return nil, err
		}
	} else {
		// Clarification for ambiguous query
		slog.Info("Continuing with clarification",
			slog.String("session_id", session.ID),
			slog.Int("clarification_len", len(clarification)),
		)

		session.AddHistoryEntry(HistoryEntry{
			Type:  "clarification",
			Input: clarification,
			Query: clarification,
		})

		// Update the query with the clarified version
		session.LastQuery = clarification
	}

	// Run the main loop
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

// CloseSession implements AgentLoop.
//
// Description:
//
//	Permanently closes a session and releases all resources. This removes
//	the session from the session store and runs all registered cleanup hooks.
//	Unlike Abort, this is for intentional cleanup when a session is
//	definitively finished (user ends conversation, timeout, or shutdown).
//
// Inputs:
//
//	sessionID - The session ID to close.
//
// Outputs:
//
//	error - Non-nil if session not found.
//
// Thread Safety: This method is safe for concurrent use.
func (l *DefaultAgentLoop) CloseSession(sessionID string) error {
	session, ok := l.sessions.Get(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	slog.Info("Closing session",
		slog.String("session_id", sessionID),
		slog.String("final_state", string(session.GetState())),
	)

	// Run all registered cleanup hooks
	runSessionCleanupHooks(sessionID)

	// Remove from session store
	l.sessions.Delete(sessionID)

	session.AddHistoryEntry(HistoryEntry{
		Type:  "session_closed",
		Input: "session explicitly closed",
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

// GetSession implements AgentLoop.
//
// Description:
//
//	Returns the full session object for operations that need access
//	to CRS, trace recorder, or other internal state.
//
// Inputs:
//
//	sessionID - The session ID to retrieve.
//
// Outputs:
//
//	*Session - The full session object.
//	error - Non-nil if session not found.
//
// Thread Safety: This method is safe for concurrent use.
func (l *DefaultAgentLoop) GetSession(sessionID string) (*Session, error) {
	session, ok := l.sessions.Get(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}

	return session, nil
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

	slog.Info("State transition",
		slog.String("session_id", session.ID),
		slog.String("from", string(currentState)),
		slog.String("to", string(newState)),
		slog.String("reason", reason),
	)

	if err := l.stateMachine.Transition(session, newState); err != nil {
		slog.Error("State transition failed",
			slog.String("session_id", session.ID),
			slog.String("error", err.Error()),
		)
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
			// LP-001: Record audit trail before setting error state
			session.AddHistoryEntry(HistoryEntry{
				Type:  "context_cancelled",
				Input: err.Error(),
				Error: ErrCanceled.Error(),
			})
			if transErr := l.transition(session, StateError, "context cancelled"); transErr != nil {
				slog.Warn("Failed to transition to error state", slog.String("error", transErr.Error()))
			}
			return l.buildErrorResult(session, ErrCanceled, startTime), nil
		}

		// Check timeout
		if session.Config.TotalTimeout > 0 && time.Since(startTime) > session.Config.TotalTimeout {
			// LP-001: Record audit trail before setting error state
			session.AddHistoryEntry(HistoryEntry{
				Type:  "timeout",
				Input: fmt.Sprintf("exceeded %v", session.Config.TotalTimeout),
				Error: ErrTimeout.Error(),
			})
			if transErr := l.transition(session, StateError, "timeout exceeded"); transErr != nil {
				slog.Warn("Failed to transition to error state", slog.String("error", transErr.Error()))
			}
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

			// LP-002: Use transition() to validate state change and record history
			session.AddHistoryEntry(HistoryEntry{
				Type:  "phase_error",
				Input: fmt.Sprintf("phase %s failed", currentState),
				Error: err.Error(),
			})
			if transErr := l.transition(session, StateError, fmt.Sprintf("phase error: %v", err)); transErr != nil {
				slog.Warn("Failed to transition to error state", slog.String("error", transErr.Error()))
				// Fallback: force state if transition fails (edge case)
				session.SetState(StateError)
			}
			return l.buildErrorResult(session, err, startTime), nil
		}

		// Transition to the next state
		if nextState != currentState {
			if err := l.transition(session, nextState, "phase completed"); err != nil {
				// LP-002: Record history for transition failure
				session.AddHistoryEntry(HistoryEntry{
					Type:  "transition_error",
					Input: fmt.Sprintf("%s -> %s", currentState, nextState),
					Error: err.Error(),
				})
				if transErr := l.transition(session, StateError, fmt.Sprintf("transition error: %v", err)); transErr != nil {
					slog.Warn("Failed to transition to error state", slog.String("error", transErr.Error()))
					session.SetState(StateError)
				}
				return l.buildErrorResult(session, err, startTime), nil
			}
		}

		session.IncrementMetric(MetricSteps, 1)
	}
}

// executePhase runs the phase for the current state.
//
// Description:
//
//	Looks up the phase implementation for the current state and executes it.
//	Falls back to default phase execution if no phase is registered.
//	Uses depsFactory for per-session dependency creation if available,
//	otherwise falls back to static phaseDeps.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	session - The session being executed.
//
// Outputs:
//
//	AgentState - The next state to transition to.
//	error - Non-nil if phase execution failed.
func (l *DefaultAgentLoop) executePhase(ctx context.Context, session *Session) (AgentState, error) {
	currentState := session.GetState()

	slog.Info("Executing phase",
		slog.String("session_id", session.ID),
		slog.String("state", string(currentState)),
	)

	if l.phaseRegistry == nil {
		slog.Debug("No phase registry, using default execution")
		return l.defaultPhaseExecution(session)
	}

	phase, ok := l.phaseRegistry.GetPhase(currentState)
	if !ok || phase == nil {
		slog.Debug("No phase registered for state, using default execution",
			slog.String("state", string(currentState)),
		)
		return l.defaultPhaseExecution(session)
	}

	slog.Info("Running phase implementation",
		slog.String("phase", phase.Name()),
		slog.String("session_id", session.ID),
	)

	// Get dependencies for phase execution
	var deps any
	var err error

	if l.depsFactory != nil {
		// Use factory for per-session dependencies
		deps, err = l.depsFactory.Create(session, session.LastQuery)
		if err != nil {
			slog.Error("Failed to create dependencies",
				slog.String("session_id", session.ID),
				slog.String("error", err.Error()),
			)
			return StateError, fmt.Errorf("failed to create dependencies: %w", err)
		}
	} else {
		// Fall back to static dependencies
		deps = l.phaseDeps
	}

	// Execute the phase with dependencies
	nextState, err := phase.Execute(ctx, deps)
	if err != nil {
		slog.Error("Phase execution failed",
			slog.String("phase", phase.Name()),
			slog.String("session_id", session.ID),
			slog.String("error", err.Error()),
		)
	} else {
		slog.Info("Phase execution completed",
			slog.String("phase", phase.Name()),
			slog.String("session_id", session.ID),
			slog.String("next_state", string(nextState)),
		)
	}

	return nextState, err
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

// getLastAssistantMessage returns the last non-empty assistant response.
//
// Description:
//
//	Walks backward through conversation history to find the most recent
//	assistant message with actual content. Empty assistant messages (which
//	can occur from context overflow during LLM calls) are skipped.
//
// Inputs:
//
//	session - The session to search.
//
// Outputs:
//
//	string - The last non-empty assistant response, or empty string if none.
func (l *DefaultAgentLoop) getLastAssistantMessage(session *Session) string {
	ctx := session.GetCurrentContext()
	if ctx == nil {
		return ""
	}

	for i := len(ctx.ConversationHistory) - 1; i >= 0; i-- {
		msg := ctx.ConversationHistory[i]
		// Skip empty assistant messages (can occur from context overflow)
		if msg.Role == "assistant" && msg.Content != "" {
			return msg.Content
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
//
// Description:
//
//	Returns all session IDs sorted alphabetically for deterministic ordering.
//
// Outputs:
//
//	[]string - All session IDs, sorted alphabetically.
//
// Thread Safety: This method is safe for concurrent use.
func (s *InMemorySessionStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
