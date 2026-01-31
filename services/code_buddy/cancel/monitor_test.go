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

func TestDeadlockDetector_DetectsDeadlock(t *testing.T) {
	// Create controller with short deadlock detection
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 10 * time.Millisecond,
		DeadlockMultiplier:    2,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session with short progress interval
	session, _ := ctrl.NewSession(context.Background(), SessionConfig{
		ID:               "test",
		ProgressInterval: 20 * time.Millisecond,
	})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Wait for detector to start
	time.Sleep(5 * time.Millisecond)

	// Don't report any progress - should trigger deadlock
	// Deadlock threshold = 2 * 20ms = 40ms
	// Check interval = 10ms
	// So after ~50ms, deadlock should be detected

	select {
	case <-algo.Done():
		// Expected - deadlock detected (algo might be cancelled by parent cascade)
		// Check that at least one context was cancelled due to deadlock
		sessionStatus := session.Status()
		activityStatus := activity.Status()
		algoStatus := algo.Status()

		// At least one should have deadlock as the reason
		deadlockFound := false
		for _, status := range []Status{sessionStatus, activityStatus, algoStatus} {
			if status.CancelReason != nil && status.CancelReason.Type == CancelDeadlock {
				deadlockFound = true
				break
			}
		}
		if !deadlockFound {
			t.Errorf("Expected at least one context to be cancelled due to deadlock, got session=%v, activity=%v, algo=%v",
				sessionStatus.CancelReason, activityStatus.CancelReason, algoStatus.CancelReason)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Deadlock should have been detected")
	}
}

func TestDeadlockDetector_NoDeadlockWithProgress(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 50 * time.Millisecond,
		DeadlockMultiplier:    3,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{
		ID:               "test",
		ProgressInterval: 100 * time.Millisecond, // Threshold = 3 * 100ms = 300ms
	})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Report progress regularly - should NOT trigger deadlock
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			algo.ReportProgress()
			time.Sleep(50 * time.Millisecond) // Much faster than 300ms threshold
		}
	}()

	select {
	case <-algo.Done():
		t.Error("Algorithm should NOT be cancelled when reporting progress")
	case <-done:
		// Expected - no deadlock
	}

	// Algorithm should still be running
	if algo.State() != StateRunning {
		t.Errorf("State = %v, want Running", algo.State())
	}
}

func TestDeadlockDetector_IgnoresTerminalContexts(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 20 * time.Millisecond,
		DeadlockMultiplier:    2,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{
		ID:               "test",
		ProgressInterval: 30 * time.Millisecond,
	})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Mark as done immediately
	algo.MarkDone()

	// Wait for potential deadlock detection
	time.Sleep(200 * time.Millisecond)

	// Should still be Done, not Cancelled
	if algo.State() != StateDone {
		t.Errorf("State = %v, want Done (deadlock detector should ignore)", algo.State())
	}
}

func TestResourceMonitor_GoroutineLimit(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session with very low goroutine limit
	session, err := ctrl.NewSession(context.Background(), SessionConfig{
		ID: "test",
		ResourceLimits: ResourceLimits{
			MaxGoroutines: 1, // Unrealistically low - will be exceeded
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Should trigger resource limit quickly
	select {
	case <-session.Done():
		// Expected - resource limit exceeded
		status := session.Status()
		if status.CancelReason == nil {
			t.Error("CancelReason should be set")
		} else if status.CancelReason.Type != CancelResourceLimit {
			t.Errorf("CancelReason.Type = %v, want %v", status.CancelReason.Type, CancelResourceLimit)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Resource limit should have been detected")
	}
}

func TestResourceMonitor_NoLimits(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session without resource limits
	session, err := ctrl.NewSession(context.Background(), SessionConfig{
		ID:             "test",
		ResourceLimits: ResourceLimits{}, // No limits
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Should still be running
	if session.State() != StateRunning {
		t.Errorf("State = %v, want Running (no limits configured)", session.State())
	}
}

func TestResourceMonitor_StopsWhenSessionDone(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		ProgressCheckInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session with high limits (won't trigger)
	session, err := ctrl.NewSession(context.Background(), SessionConfig{
		ID: "test",
		ResourceLimits: ResourceLimits{
			MaxGoroutines: 1000000,
		},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Give monitor time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel session
	session.Cancel(CancelReason{Type: CancelUser})

	// Wait for cancellation
	select {
	case <-session.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Session should be cancelled")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
		{1048576, "1.00 MiB"},
		{1073741824, "1.00 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.expected {
				t.Errorf("formatBytes(%d) = %v, want %v", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{12345, "12345"},
		{-12345, "-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatInt(tt.n)
			if got != tt.expected {
				t.Errorf("formatInt(%d) = %v, want %v", tt.n, got, tt.expected)
			}
		})
	}
}

func TestTimeoutEnforcer(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		GracePeriod: 50 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 100*time.Millisecond)

	// Create timeout enforcer
	enforcer := NewTimeoutEnforcer(ctrl)

	// Start enforcer in background
	go enforcer.EnforceTimeout(algo, 50*time.Millisecond, 50*time.Millisecond)

	// Should timeout quickly
	select {
	case <-algo.Done():
		// Expected
	case <-time.After(200 * time.Millisecond):
		t.Error("Algorithm should have timed out")
	}
}

func TestTimeoutEnforcer_ZeroTimeout(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Create timeout enforcer
	enforcer := NewTimeoutEnforcer(ctrl)

	// EnforceTimeout with 0 should return immediately
	done := make(chan struct{})
	go func() {
		enforcer.EnforceTimeout(algo, 0, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// Expected - immediate return
	case <-time.After(100 * time.Millisecond):
		t.Error("EnforceTimeout(0) should return immediately")
	}

	// Algorithm should still be running
	if algo.State() != StateRunning {
		t.Errorf("State = %v, want Running", algo.State())
	}
}

func TestTimeoutEnforcer_AlreadyCancelled(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Cancel algorithm first
	algo.Cancel(CancelReason{Type: CancelUser})
	<-algo.Done()

	// Create timeout enforcer
	enforcer := NewTimeoutEnforcer(ctrl)

	// EnforceTimeout should return immediately since algo is already done
	done := make(chan struct{})
	go func() {
		enforcer.EnforceTimeout(algo, time.Second, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// Expected - immediate return
	case <-time.After(100 * time.Millisecond):
		t.Error("EnforceTimeout should return immediately for cancelled algo")
	}
}
