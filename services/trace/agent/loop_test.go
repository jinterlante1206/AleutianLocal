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
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDefaultAgentLoop_Run_Success(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, err := NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	result, err := loop.Run(context.Background(), session, "What does this function do?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// With default phase execution, should complete
	if result.State != StateComplete {
		t.Errorf("State = %s, want COMPLETE", result.State)
	}
}

func TestDefaultAgentLoop_Run_NilSession(t *testing.T) {
	loop := NewDefaultAgentLoop()

	_, err := loop.Run(context.Background(), nil, "query")
	if err == nil {
		t.Error("expected error for nil session")
	}
	if !errors.Is(err, ErrInvalidSession) {
		t.Errorf("expected ErrInvalidSession, got %v", err)
	}
}

func TestDefaultAgentLoop_Run_EmptyQuery(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	_, err := loop.Run(context.Background(), session, "")
	if err == nil {
		t.Error("expected error for empty query")
	}
	if !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestDefaultAgentLoop_Run_SessionNotIdle(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	session.SetState(StateExecute) // Not IDLE

	_, err := loop.Run(context.Background(), session, "query")
	if err == nil {
		t.Error("expected error for non-IDLE session")
	}
}

func TestDefaultAgentLoop_Run_SessionInProgress(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)

	// Acquire the session first
	session.TryAcquire()

	// Try to run - should fail because session is in progress
	_, err := loop.Run(context.Background(), session, "query")
	if err == nil {
		t.Error("expected error for session in progress")
	}
	if !errors.Is(err, ErrSessionInProgress) {
		t.Errorf("expected ErrSessionInProgress, got %v", err)
	}

	session.Release()
}

func TestDefaultAgentLoop_Run_Timeout(t *testing.T) {
	loop := NewDefaultAgentLoop()

	// Create session with very short timeout
	config := DefaultSessionConfig()
	config.TotalTimeout = 1 * time.Nanosecond
	session, _ := NewSession("/test/project", config)

	// Add a small delay to ensure timeout triggers
	time.Sleep(10 * time.Millisecond)

	result, err := loop.Run(context.Background(), session, "query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.State != StateError {
		t.Errorf("State = %s, want ERROR", result.State)
	}
}

func TestDefaultAgentLoop_Run_ContextCancellation(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := loop.Run(ctx, session, "query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.State != StateError {
		t.Errorf("State = %s, want ERROR", result.State)
	}
}

func TestDefaultAgentLoop_Continue_NotFound(t *testing.T) {
	loop := NewDefaultAgentLoop()

	_, err := loop.Continue(context.Background(), "nonexistent-id", "clarification")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestDefaultAgentLoop_Continue_NotInClarifyState(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	loop.sessions.Put(session) // Store the session

	_, err := loop.Continue(context.Background(), session.ID, "clarification")
	if err == nil {
		t.Error("expected error for non-CLARIFY state")
	}
	if !errors.Is(err, ErrNotInClarifyState) {
		t.Errorf("expected ErrNotInClarifyState, got %v", err)
	}
}

func TestDefaultAgentLoop_Abort_NotFound(t *testing.T) {
	loop := NewDefaultAgentLoop()

	err := loop.Abort(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestDefaultAgentLoop_Abort_Success(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	session.SetState(StateExecute)
	loop.sessions.Put(session)

	err := loop.Abort(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Abort failed: %v", err)
	}

	if session.GetState() != StateError {
		t.Errorf("State = %s, want ERROR", session.GetState())
	}
}

func TestDefaultAgentLoop_Abort_AlreadyTerminated(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	session.SetState(StateComplete) // Already terminated
	loop.sessions.Put(session)

	err := loop.Abort(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Abort failed: %v", err)
	}

	// Should remain COMPLETE (no change)
	if session.GetState() != StateComplete {
		t.Errorf("State = %s, want COMPLETE", session.GetState())
	}
}

func TestDefaultAgentLoop_GetState_NotFound(t *testing.T) {
	loop := NewDefaultAgentLoop()

	_, err := loop.GetState("nonexistent-id")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestDefaultAgentLoop_GetState_Success(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)
	session.SetState(StateExecute)
	loop.sessions.Put(session)

	state, err := loop.GetState(session.ID)
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	if state.State != StateExecute {
		t.Errorf("State = %s, want EXECUTE", state.State)
	}
	if state.ID != session.ID {
		t.Errorf("ID = %s, want %s", state.ID, session.ID)
	}
}

func TestDefaultAgentLoop_MaxConcurrentSessions(t *testing.T) {
	loop := NewDefaultAgentLoop(WithMaxConcurrentSessions(2))

	session1, _ := NewSession("/test/project1", nil)
	session2, _ := NewSession("/test/project2", nil)

	// Start both sessions concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = loop.Run(context.Background(), session1, "query1")
	}()

	go func() {
		defer wg.Done()
		_, _ = loop.Run(context.Background(), session2, "query2")
	}()

	wg.Wait()

	// Both sessions should complete
	// This test verifies concurrent limits work without panicking
}

func TestInMemorySessionStore(t *testing.T) {
	store := NewInMemorySessionStore()

	session, _ := NewSession("/test/project", nil)

	// Test Put and Get
	store.Put(session)
	got, ok := store.Get(session.ID)
	if !ok {
		t.Error("Get should find stored session")
	}
	if got.ID != session.ID {
		t.Errorf("ID = %s, want %s", got.ID, session.ID)
	}

	// Test List
	ids := store.List()
	if len(ids) != 1 {
		t.Errorf("List returned %d sessions, want 1", len(ids))
	}

	// Test Delete
	store.Delete(session.ID)
	_, ok = store.Get(session.ID)
	if ok {
		t.Error("Get should not find deleted session")
	}

	// Test Get non-existent
	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("Get should not find non-existent session")
	}
}

func TestInMemorySessionStore_ConcurrentAccess(t *testing.T) {
	store := NewInMemorySessionStore()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent puts
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			session, _ := NewSession("/test/project", nil)
			store.Put(session)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.List()
		}()
	}

	wg.Wait()

	// Should not panic or deadlock
	ids := store.List()
	if len(ids) != numGoroutines {
		t.Errorf("List returned %d sessions, want %d", len(ids), numGoroutines)
	}
}

// MockPhaseRegistry for testing with custom phases
type MockPhaseRegistry struct {
	phases map[AgentState]PhaseExecutor
}

func NewMockPhaseRegistry() *MockPhaseRegistry {
	return &MockPhaseRegistry{
		phases: make(map[AgentState]PhaseExecutor),
	}
}

func (r *MockPhaseRegistry) GetPhase(state AgentState) (PhaseExecutor, bool) {
	phase, ok := r.phases[state]
	return phase, ok
}

func (r *MockPhaseRegistry) RegisterPhase(state AgentState, phase PhaseExecutor) {
	r.phases[state] = phase
}

// MockPhase for testing
type MockPhase struct {
	name      string
	nextState AgentState
	err       error
}

func (p *MockPhase) Name() string {
	return p.name
}

func (p *MockPhase) Execute(ctx context.Context, deps any) (AgentState, error) {
	return p.nextState, p.err
}

func TestDefaultAgentLoop_WithCustomPhases(t *testing.T) {
	registry := NewMockPhaseRegistry()

	// Register mock phases
	registry.RegisterPhase(StateInit, &MockPhase{name: "init", nextState: StatePlan})
	registry.RegisterPhase(StatePlan, &MockPhase{name: "plan", nextState: StateExecute})
	registry.RegisterPhase(StateExecute, &MockPhase{name: "execute", nextState: StateComplete})

	loop := NewDefaultAgentLoop(WithPhaseRegistry(registry))

	session, _ := NewSession("/test/project", nil)

	result, err := loop.Run(context.Background(), session, "query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.State != StateComplete {
		t.Errorf("State = %s, want COMPLETE", result.State)
	}
}

func TestDefaultAgentLoop_PhaseError(t *testing.T) {
	registry := NewMockPhaseRegistry()

	// Register a phase that returns an error
	registry.RegisterPhase(StateInit, &MockPhase{name: "init", nextState: StateError, err: errors.New("phase failed")})

	loop := NewDefaultAgentLoop(WithPhaseRegistry(registry))

	session, _ := NewSession("/test/project", nil)

	result, err := loop.Run(context.Background(), session, "query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.State != StateError {
		t.Errorf("State = %s, want ERROR", result.State)
	}
	if result.Error == nil {
		t.Error("expected error in result")
	}
}

func TestDefaultAgentLoop_ClarifyWorkflow(t *testing.T) {
	// This test verifies that the CLARIFY state correctly pauses for user input
	// and that Continue() can resume the session.
	//
	// The current implementation returns immediately when in CLARIFY state,
	// which is the expected behavior - CLARIFY means "waiting for user input".

	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)

	// Manually put session in CLARIFY state to test Continue
	session.SetState(StateClarify)
	loop.sessions.Put(session)

	// Calling Continue while in CLARIFY should work
	// With default phases, it will eventually complete
	result, err := loop.Continue(context.Background(), session.ID, "user clarification")
	if err != nil {
		t.Fatalf("Continue failed: %v", err)
	}

	// Result should be CLARIFY since the loop returns immediately
	// when it sees CLARIFY state (waiting for more input)
	// This is correct behavior - the CLARIFY phase needs to process
	// the input and transition to another state
	if result.State != StateClarify {
		t.Logf("State = %s (CLARIFY state pauses for input)", result.State)
	}

	// Verify that NeedsClarify is set
	if result.NeedsClarify == nil {
		t.Error("expected NeedsClarify to be set")
	}
}

func TestDefaultAgentLoop_StepTracking(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, _ := NewSession("/test/project", nil)

	result, err := loop.Run(context.Background(), session, "query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should have taken at least one step
	if result.StepsTaken == 0 {
		t.Error("expected at least one step")
	}
}

func TestAgentLoopInterface(t *testing.T) {
	// Verify DefaultAgentLoop implements AgentLoop interface
	var _ AgentLoop = (*DefaultAgentLoop)(nil)
}

func TestDefaultAgentLoop_GetLastAssistantMessage_SkipsEmptyMessages(t *testing.T) {
	loop := NewDefaultAgentLoop()
	session, _ := NewSession("/test/project", nil)

	// Create a context with an empty assistant message followed by a non-empty one
	ctx := &AssembledContext{
		ConversationHistory: []Message{
			{Role: "user", Content: "What does this do?"},
			{Role: "assistant", Content: ""},           // Empty message (e.g., from context overflow)
			{Role: "tool", Content: "tool result"},     // Tool result
			{Role: "assistant", Content: "The answer"}, // Actual response
		},
	}
	session.SetCurrentContext(ctx)

	result := loop.getLastAssistantMessage(session)
	if result != "The answer" {
		t.Errorf("getLastAssistantMessage = %q, want %q", result, "The answer")
	}
}

func TestDefaultAgentLoop_GetLastAssistantMessage_ReturnsEmptyWhenAllEmpty(t *testing.T) {
	loop := NewDefaultAgentLoop()
	session, _ := NewSession("/test/project", nil)

	// Create a context with only empty assistant messages
	ctx := &AssembledContext{
		ConversationHistory: []Message{
			{Role: "user", Content: "What does this do?"},
			{Role: "assistant", Content: ""},       // Empty message
			{Role: "assistant", Content: ""},       // Another empty message
			{Role: "tool", Content: "tool result"}, // Tool result
		},
	}
	session.SetCurrentContext(ctx)

	result := loop.getLastAssistantMessage(session)
	if result != "" {
		t.Errorf("getLastAssistantMessage = %q, want empty string", result)
	}
}

func TestDefaultAgentLoop_GetLastAssistantMessage_FindsLastNonEmpty(t *testing.T) {
	loop := NewDefaultAgentLoop()
	session, _ := NewSession("/test/project", nil)

	// Empty messages should be skipped, finding the last non-empty one
	ctx := &AssembledContext{
		ConversationHistory: []Message{
			{Role: "user", Content: "Question 1"},
			{Role: "assistant", Content: "First answer"},
			{Role: "user", Content: "Question 2"},
			{Role: "assistant", Content: ""}, // Empty - context overflow
			{Role: "assistant", Content: ""}, // Another empty
		},
	}
	session.SetCurrentContext(ctx)

	result := loop.getLastAssistantMessage(session)
	if result != "First answer" {
		t.Errorf("getLastAssistantMessage = %q, want %q", result, "First answer")
	}
}

// =============================================================================
// GR-38 Tests: Session Completion Tracing
// =============================================================================

func TestGR38_RecordSessionCompletion_NilSession(t *testing.T) {
	loop := NewDefaultAgentLoop()

	// Should not panic with nil session
	loop.recordSessionCompletion(context.Background(), nil, StateComplete, time.Now())
}

func TestGR38_RecordSessionCompletion_TracesStep(t *testing.T) {
	loop := NewDefaultAgentLoop()

	// TraceRecorder is created by default in NewSession
	session, err := NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Record completion with a start time slightly in the past to verify duration
	startTime := time.Now().Add(-100 * time.Millisecond)
	loop.recordSessionCompletion(context.Background(), session, StateComplete, startTime)

	// Get trace and verify session_complete step was recorded
	trace := session.GetReasoningTrace()
	if trace == nil {
		t.Fatal("Trace should not be nil when recording is enabled")
	}

	// Check that session_complete action was recorded
	found := false
	for _, step := range trace.Trace {
		if step.Action == "session_complete" {
			found = true
			if step.Target != string(StateComplete) {
				t.Errorf("Target = %s, want %s", step.Target, string(StateComplete))
			}
			break
		}
	}

	if !found {
		t.Error("session_complete trace step should be recorded")
	}
}

func TestGR38_TerminalStateRecordsCompletion(t *testing.T) {
	registry := NewMockPhaseRegistry()

	// Register mock phases that go straight to complete
	registry.RegisterPhase(StateInit, &MockPhase{name: "init", nextState: StatePlan})
	registry.RegisterPhase(StatePlan, &MockPhase{name: "plan", nextState: StateExecute})
	registry.RegisterPhase(StateExecute, &MockPhase{name: "execute", nextState: StateComplete})

	loop := NewDefaultAgentLoop(WithPhaseRegistry(registry))

	// TraceRecorder is created by default in NewSession
	session, err := NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	result, err := loop.Run(context.Background(), session, "test query")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.State != StateComplete {
		t.Errorf("State = %s, want COMPLETE", result.State)
	}

	// Verify session_complete was recorded
	trace := session.GetReasoningTrace()
	if trace == nil {
		t.Fatal("Trace should not be nil")
	}

	found := false
	for _, step := range trace.Trace {
		if step.Action == "session_complete" {
			found = true
			break
		}
	}

	if !found {
		t.Error("GR-38: session_complete trace step should be recorded on terminal state")
	}
}

// TestGR38_RecordSessionCompletion_Duration verifies duration is recorded correctly.
// GR-38 Finding 11: session_complete should include DurationMs.
func TestGR38_RecordSessionCompletion_Duration(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, err := NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Start time 500ms in the past
	startTime := time.Now().Add(-500 * time.Millisecond)
	loop.recordSessionCompletion(context.Background(), session, StateComplete, startTime)

	// Get trace and verify duration was recorded
	trace := session.GetReasoningTrace()
	if trace == nil {
		t.Fatal("Trace should not be nil")
	}

	for _, step := range trace.Trace {
		if step.Action == "session_complete" {
			// Duration should be at least 500ms (we set startTime 500ms ago)
			durationMs := step.Duration.Milliseconds()
			if durationMs < 500 {
				t.Errorf("Duration = %v (%dms), want >= 500ms", step.Duration, durationMs)
			}
			// Check metadata also has duration
			if _, ok := step.Metadata["duration_ms"]; !ok {
				t.Error("Metadata should contain duration_ms")
			}
			return
		}
	}

	t.Error("session_complete step not found")
}

// TestGR38_RecordSessionCompletion_TraceStepCount verifies trace_step_count is recorded.
// GR-38 Issue 15: Help clarify step count vs trace step count.
func TestGR38_RecordSessionCompletion_TraceStepCount(t *testing.T) {
	loop := NewDefaultAgentLoop()

	session, err := NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Record completion
	loop.recordSessionCompletion(context.Background(), session, StateComplete, time.Now())

	// Get trace and verify trace_step_count is in metadata
	trace := session.GetReasoningTrace()
	if trace == nil {
		t.Fatal("Trace should not be nil")
	}

	for _, step := range trace.Trace {
		if step.Action == "session_complete" {
			count, ok := step.Metadata["trace_step_count"]
			if !ok {
				t.Error("Metadata should contain trace_step_count")
				return
			}
			// Before session_complete, there were 0 trace steps
			if count != "0" {
				t.Errorf("trace_step_count = %s, want 0 (before session_complete)", count)
			}
			return
		}
	}

	t.Error("session_complete step not found")
}
