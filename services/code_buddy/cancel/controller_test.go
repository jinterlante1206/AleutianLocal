// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cancel

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewController(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		ctrl, err := NewController(ControllerConfig{}, nil)
		if err != nil {
			t.Fatalf("NewController failed: %v", err)
		}
		defer ctrl.Close()

		if ctrl == nil {
			t.Fatal("Controller should not be nil")
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		_, err := NewController(ControllerConfig{
			DeadlockMultiplier: 1, // Invalid - must be >= 2
		}, nil)
		if err == nil {
			t.Fatal("NewController should fail with invalid config")
		}
	})
}

func TestController_NewSession(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	t.Run("valid session", func(t *testing.T) {
		session, err := ctrl.NewSession(context.Background(), SessionConfig{
			ID: "test-session",
		})
		if err != nil {
			t.Fatalf("NewSession failed: %v", err)
		}
		if session == nil {
			t.Fatal("Session should not be nil")
		}
		if session.ID() != "test-session" {
			t.Errorf("Session ID = %v, want 'test-session'", session.ID())
		}
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := ctrl.NewSession(nil, SessionConfig{ID: "test"})
		if err != ErrNilContext {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		_, err := ctrl.NewSession(context.Background(), SessionConfig{})
		if err == nil {
			t.Fatal("Expected error for empty ID")
		}
	})

	t.Run("closed controller", func(t *testing.T) {
		closedCtrl, _ := NewController(ControllerConfig{}, nil)
		closedCtrl.Close()

		_, err := closedCtrl.NewSession(context.Background(), SessionConfig{ID: "test"})
		if err != ErrControllerClosed {
			t.Errorf("Expected ErrControllerClosed, got %v", err)
		}
	})
}

func TestController_Cancel(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	t.Run("cancel by full ID", func(t *testing.T) {
		err := ctrl.Cancel("test/activity/algo", CancelReason{Type: CancelUser})
		if err != nil {
			t.Fatalf("Cancel failed: %v", err)
		}

		select {
		case <-algo.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("Algorithm should be cancelled")
		}
	})

	t.Run("cancel by algorithm name", func(t *testing.T) {
		algo2 := activity.NewAlgorithm("algo2", 5*time.Second)

		err := ctrl.Cancel("algo2", CancelReason{Type: CancelUser})
		if err != nil {
			t.Fatalf("Cancel failed: %v", err)
		}

		select {
		case <-algo2.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("Algorithm should be cancelled")
		}
	})

	t.Run("cancel nonexistent", func(t *testing.T) {
		err := ctrl.Cancel("nonexistent", CancelReason{Type: CancelUser})
		if err == nil {
			t.Fatal("Expected error for nonexistent context")
		}
	})
}

func TestController_CancelAll(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create multiple sessions with activities and algorithms
	session1, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "session1"})
	session2, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "session2"})
	activity1 := session1.NewActivity("activity1")
	activity2 := session2.NewActivity("activity2")
	algo1 := activity1.NewAlgorithm("algo1", 5*time.Second)
	algo2 := activity2.NewAlgorithm("algo2", 5*time.Second)

	// Cancel all
	ctrl.CancelAll(CancelReason{Type: CancelShutdown, Message: "Test shutdown"})

	// Verify all are cancelled
	contexts := []*AlgorithmContext{algo1, algo2}
	for _, ctx := range contexts {
		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Context %s should be cancelled", ctx.ID())
		}
	}
}

func TestController_Status(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create hierarchy
	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Get status
	status := ctrl.Status()

	if status.TotalActive != 3 { // session + activity + algo
		t.Errorf("TotalActive = %d, want 3", status.TotalActive)
	}
	if len(status.Sessions) != 1 {
		t.Errorf("Sessions count = %d, want 1", len(status.Sessions))
	}

	// Cancel algorithm and mark as terminal
	algo.Cancel(CancelReason{Type: CancelUser})
	<-algo.Done()
	algo.markCancelled() // Move to terminal state

	// Status should reflect cancellation
	status = ctrl.Status()
	if status.TotalActive != 2 { // session + activity (algo is terminal)
		t.Errorf("TotalActive = %d, want 2", status.TotalActive)
	}
}

func TestController_Shutdown(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      100 * time.Millisecond,
		ForceKillTimeout: 200 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	// Create some contexts
	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	activity.NewAlgorithm("algo", 5*time.Second)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := ctrl.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if !result.Success {
		t.Error("Shutdown should be successful")
	}
	if result.Duration == 0 {
		t.Error("Shutdown duration should be > 0")
	}
}

func TestController_Close(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      50 * time.Millisecond,
		ForceKillTimeout: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	// Create contexts
	ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})

	// Close should not error
	err = ctrl.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Close is idempotent
	err = ctrl.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestController_GetContext(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	t.Run("get session", func(t *testing.T) {
		ctx, ok := ctrl.GetContext("test")
		if !ok {
			t.Fatal("Session not found")
		}
		if ctx.ID() != "test" {
			t.Errorf("ID = %v, want 'test'", ctx.ID())
		}
	})

	t.Run("get activity", func(t *testing.T) {
		ctx, ok := ctrl.GetContext("test/activity")
		if !ok {
			t.Fatal("Activity not found")
		}
		if ctx.ID() != "test/activity" {
			t.Errorf("ID = %v, want 'test/activity'", ctx.ID())
		}
	})

	t.Run("get algorithm", func(t *testing.T) {
		ctx, ok := ctrl.GetContext("test/activity/algo")
		if !ok {
			t.Fatal("Algorithm not found")
		}
		if ctx.ID() != algo.ID() {
			t.Errorf("ID = %v, want %v", ctx.ID(), algo.ID())
		}
	})

	t.Run("get nonexistent", func(t *testing.T) {
		_, ok := ctrl.GetContext("nonexistent")
		if ok {
			t.Fatal("Should not find nonexistent context")
		}
	})
}

func TestController_GetSession(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})

	t.Run("get existing", func(t *testing.T) {
		session, ok := ctrl.GetSession("test")
		if !ok {
			t.Fatal("Session not found")
		}
		if session.ID() != "test" {
			t.Errorf("ID = %v, want 'test'", session.ID())
		}
	})

	t.Run("get nonexistent", func(t *testing.T) {
		_, ok := ctrl.GetSession("nonexistent")
		if ok {
			t.Fatal("Should not find nonexistent session")
		}
	})
}

func TestController_ConcurrentOperations(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	var wg sync.WaitGroup
	sessions := make([]*SessionContext, 50)

	// Create sessions concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "session-" + formatInt(idx)
			session, err := ctrl.NewSession(context.Background(), SessionConfig{ID: id})
			if err != nil {
				t.Errorf("NewSession failed: %v", err)
				return
			}
			sessions[idx] = session
		}(i)
	}

	wg.Wait()

	// Create activities concurrently
	for _, session := range sessions {
		if session == nil {
			continue
		}
		wg.Add(1)
		go func(s *SessionContext) {
			defer wg.Done()
			s.NewActivity("activity")
		}(session)
	}

	wg.Wait()

	// Get status concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctrl.Status()
		}()
	}

	// Cancel some sessions concurrently
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if sessions[idx] != nil {
				ctrl.Cancel(sessions[idx].ID(), CancelReason{Type: CancelUser})
			}
		}(i)
	}

	wg.Wait()
}

func TestController_MultipleSessions(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create multiple sessions
	session1, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "session1"})
	session2, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "session2"})
	session3, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "session3"})

	// Verify all exist
	status := ctrl.Status()
	if len(status.Sessions) != 3 {
		t.Errorf("Sessions count = %d, want 3", len(status.Sessions))
	}

	// Cancel one session
	ctrl.Cancel("session2", CancelReason{Type: CancelUser})

	// Verify others are unaffected
	if session1.State() != StateRunning {
		t.Error("session1 should still be running")
	}
	if session3.State() != StateRunning {
		t.Error("session3 should still be running")
	}

	select {
	case <-session2.Done():
		// Expected
	default:
		t.Error("session2 should be cancelled")
	}
}

func TestController_ResourceLimitedSession(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 50 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session with resource limits
	session, err := ctrl.NewSession(context.Background(), SessionConfig{
		ID: "limited-session",
		ResourceLimits: ResourceLimits{
			MaxGoroutines: 1000000, // Very high limit - won't trigger
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Session should be running
	if session.State() != StateRunning {
		t.Errorf("State = %v, want Running", session.State())
	}

	// Give resource monitor time to start
	time.Sleep(100 * time.Millisecond)

	// Session should still be running (limits not exceeded)
	if session.State() != StateRunning {
		t.Errorf("State = %v, want Running (limits not exceeded)", session.State())
	}
}
