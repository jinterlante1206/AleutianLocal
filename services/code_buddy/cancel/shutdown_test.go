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
	"testing"
	"time"
)

func TestShutdownPhase_String(t *testing.T) {
	tests := []struct {
		phase    ShutdownPhase
		expected string
	}{
		{PhaseSignal, "signal"},
		{PhaseCollect, "collect"},
		{PhaseForceKill, "force_kill"},
		{PhaseReport, "report"},
		{PhaseComplete, "complete"},
		{ShutdownPhase(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.phase.String(); got != tt.expected {
				t.Errorf("ShutdownPhase.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShutdownCoordinator_Execute(t *testing.T) {
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
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Set partial result collector
	algo.SetPartialCollector(func() (any, error) {
		return "partial-data", nil
	})

	// Create coordinator
	coord := NewShutdownCoordinator(ctrl, 100*time.Millisecond, 200*time.Millisecond)

	// Execute shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Error("Shutdown should be successful")
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
	if coord.Phase() != PhaseComplete {
		t.Errorf("Phase = %v, want Complete", coord.Phase())
	}
}

func TestShutdownCoordinator_DoubleExecute(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      50 * time.Millisecond,
		ForceKillTimeout: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	coord := NewShutdownCoordinator(ctrl, 50*time.Millisecond, 100*time.Millisecond)

	ctx := context.Background()

	// First execute
	result1, err := coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err != nil {
		t.Fatalf("First Execute failed: %v", err)
	}

	// Second execute should return immediately
	result2, err := coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err != nil {
		t.Fatalf("Second Execute failed: %v", err)
	}

	// First result should have duration, second should be instant
	if result1.Duration == 0 {
		t.Error("First result should have duration")
	}
	// Second result is from early return, success should be true
	if !result2.Success {
		t.Error("Second result should be successful")
	}
}

func TestShutdownCoordinator_ContextCancellation(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      time.Second, // Long grace period
		ForceKillTimeout: 2 * time.Second,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	// Create session
	ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})

	coord := NewShutdownCoordinator(ctrl, time.Second, 2*time.Second)

	// Create context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Execute should be interrupted
	_, err = coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err == nil {
		t.Error("Execute should return error when context is cancelled")
	}
}

func TestShutdownCoordinator_PartialResults(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      100 * time.Millisecond,
		ForceKillTimeout: 200 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")

	// Create multiple algorithms with partial results
	for i := 0; i < 5; i++ {
		algo := activity.NewAlgorithm("algo-"+formatInt(i), 5*time.Second)
		algo.SetPartialCollector(func() (any, error) {
			return "data", nil
		})
	}

	coord := NewShutdownCoordinator(ctrl, 100*time.Millisecond, 200*time.Millisecond)

	ctx := context.Background()
	result, err := coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should have collected partial results
	if result.PartialResultsCollected < 1 {
		t.Errorf("PartialResultsCollected = %d, want >= 1", result.PartialResultsCollected)
	}
}

func TestShutdownCoordinator_ForceKill(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      50 * time.Millisecond,
		ForceKillTimeout: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	session.NewActivity("activity")

	coord := NewShutdownCoordinator(ctrl, 50*time.Millisecond, 100*time.Millisecond)

	ctx := context.Background()
	result, err := coord.Execute(ctx, CancelReason{Type: CancelShutdown})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Shutdown should complete
	if !result.Success {
		t.Error("Shutdown should be successful")
	}
}

func TestShutdownCoordinator_WaitForCompletion(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:           50 * time.Millisecond,
		ForceKillTimeout:      100 * time.Millisecond,
		ProgressCheckInterval: 100 * time.Millisecond, // Slow down deadlock detector
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{
		ID:               "test",
		ProgressInterval: time.Second, // Prevent deadlock detection
	})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	coord := NewShutdownCoordinator(ctrl, 50*time.Millisecond, 100*time.Millisecond)

	// Cancel and mark everything as terminal
	algo.Cancel(CancelReason{Type: CancelUser})
	activity.Cancel(CancelReason{Type: CancelUser})
	session.Cancel(CancelReason{Type: CancelUser})

	// Wait for cancel to propagate
	<-algo.Done()

	// Mark as fully cancelled (terminal)
	algo.markCancelled()
	activity.markCancelled()
	session.markCancelled()

	// Wait for completion should return immediately since all are terminal
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = coord.WaitForCompletion(ctx, 10*time.Millisecond)
	if err != nil {
		t.Errorf("WaitForCompletion failed: %v", err)
	}
}

func TestShutdownCoordinator_WaitForCompletion_Timeout(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      50 * time.Millisecond,
		ForceKillTimeout: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	// Create session that won't be cancelled
	ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})

	coord := NewShutdownCoordinator(ctrl, 50*time.Millisecond, 100*time.Millisecond)

	// Wait with short timeout - should fail because contexts are still running
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = coord.WaitForCompletion(ctx, 10*time.Millisecond)
	if err == nil {
		t.Error("WaitForCompletion should timeout")
	}
}

func TestShutdownCoordinator_Phases(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod:      100 * time.Millisecond,
		ForceKillTimeout: 200 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}

	coord := NewShutdownCoordinator(ctrl, 100*time.Millisecond, 200*time.Millisecond)

	// Initial phase should be Signal
	if coord.Phase() != PhaseSignal {
		t.Errorf("Initial Phase = %v, want Signal", coord.Phase())
	}

	// Track phases during execution
	phases := make([]ShutdownPhase, 0)
	go func() {
		for {
			phases = append(phases, coord.Phase())
			time.Sleep(10 * time.Millisecond)
			if coord.Phase() == PhaseComplete {
				break
			}
		}
	}()

	// Execute shutdown
	ctx := context.Background()
	coord.Execute(ctx, CancelReason{Type: CancelShutdown})

	// Final phase should be Complete
	if coord.Phase() != PhaseComplete {
		t.Errorf("Final Phase = %v, want Complete", coord.Phase())
	}
}
