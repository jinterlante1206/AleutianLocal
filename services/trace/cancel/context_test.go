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

func TestSessionContext_Lifecycle(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	// Create session
	session, err := ctrl.NewSession(context.Background(), SessionConfig{
		ID: "test-session",
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Verify initial state
	if session.ID() != "test-session" {
		t.Errorf("ID() = %v, want 'test-session'", session.ID())
	}
	if session.Level() != LevelSession {
		t.Errorf("Level() = %v, want %v", session.Level(), LevelSession)
	}
	if session.State() != StateRunning {
		t.Errorf("State() = %v, want %v", session.State(), StateRunning)
	}

	// Cancel session
	reason := CancelReason{
		Type:    CancelUser,
		Message: "Test cancellation",
	}
	session.Cancel(reason)

	// Verify cancellation
	select {
	case <-session.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Session was not cancelled in time")
	}

	if session.State() != StateCancelling && session.State() != StateCancelled {
		t.Errorf("State() = %v, want Cancelling or Cancelled", session.State())
	}
}

func TestActivityContext_Lifecycle(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, err := ctrl.NewSession(context.Background(), SessionConfig{ID: "test-session"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Create activity
	activity := session.NewActivity("search")

	// Verify
	if activity.ID() != "test-session/search" {
		t.Errorf("ID() = %v, want 'test-session/search'", activity.ID())
	}
	if activity.Level() != LevelActivity {
		t.Errorf("Level() = %v, want %v", activity.Level(), LevelActivity)
	}
	if activity.Name() != "search" {
		t.Errorf("Name() = %v, want 'search'", activity.Name())
	}
	if activity.Session() != session {
		t.Error("Session() did not return parent session")
	}

	// Cancel activity
	activity.Cancel(CancelReason{Type: CancelUser})

	select {
	case <-activity.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Activity was not cancelled in time")
	}
}

func TestAlgorithmContext_Lifecycle(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, err := ctrl.NewSession(context.Background(), SessionConfig{ID: "test-session"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	activity := session.NewActivity("search")
	algorithm := activity.NewAlgorithm("pnmcts", 5*time.Second)

	// Verify
	if algorithm.ID() != "test-session/search/pnmcts" {
		t.Errorf("ID() = %v, want 'test-session/search/pnmcts'", algorithm.ID())
	}
	if algorithm.Level() != LevelAlgorithm {
		t.Errorf("Level() = %v, want %v", algorithm.Level(), LevelAlgorithm)
	}
	if algorithm.Name() != "pnmcts" {
		t.Errorf("Name() = %v, want 'pnmcts'", algorithm.Name())
	}
	if algorithm.Activity() != activity {
		t.Error("Activity() did not return parent activity")
	}
	if algorithm.Timeout() != 5*time.Second {
		t.Errorf("Timeout() = %v, want 5s", algorithm.Timeout())
	}

	// Cancel algorithm
	algorithm.Cancel(CancelReason{Type: CancelUser})

	select {
	case <-algorithm.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Algorithm was not cancelled in time")
	}
}

func TestHierarchicalCancellation_SessionCancelsAll(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity1 := session.NewActivity("activity1")
	activity2 := session.NewActivity("activity2")
	algo1 := activity1.NewAlgorithm("algo1", 5*time.Second)
	algo2 := activity1.NewAlgorithm("algo2", 5*time.Second)
	algo3 := activity2.NewAlgorithm("algo3", 5*time.Second)

	// Cancel session - should cancel all children
	session.Cancel(CancelReason{Type: CancelUser, Message: "Parent cancel"})

	// Verify all are cancelled
	contexts := []Cancellable{session, activity1, activity2, algo1, algo2, algo3}
	for _, ctx := range contexts {
		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Context %s was not cancelled", ctx.ID())
		}
	}
}

func TestHierarchicalCancellation_ActivityCancelsChildren(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity1 := session.NewActivity("activity1")
	activity2 := session.NewActivity("activity2")
	algo1 := activity1.NewAlgorithm("algo1", 5*time.Second)
	algo2 := activity1.NewAlgorithm("algo2", 5*time.Second)
	algo3 := activity2.NewAlgorithm("algo3", 5*time.Second)

	// Cancel activity1 - should cancel its children but not activity2 or session
	activity1.Cancel(CancelReason{Type: CancelUser})

	// Verify activity1 and its algorithms are cancelled
	for _, ctx := range []Cancellable{activity1, algo1, algo2} {
		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Context %s should be cancelled", ctx.ID())
		}
	}

	// Verify session and activity2 are NOT cancelled
	for _, ctx := range []Cancellable{session, activity2, algo3} {
		select {
		case <-ctx.Done():
			t.Errorf("Context %s should NOT be cancelled", ctx.ID())
		default:
			// Expected - not cancelled
		}
	}
}

func TestAlgorithmCancellation_DoesNotAffectSiblings(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo1 := activity.NewAlgorithm("algo1", 5*time.Second)
	algo2 := activity.NewAlgorithm("algo2", 5*time.Second)

	// Cancel algo1 only
	algo1.Cancel(CancelReason{Type: CancelUser})

	// Verify algo1 is cancelled
	select {
	case <-algo1.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("algo1 should be cancelled")
	}

	// Verify algo2, activity, session are NOT cancelled
	for _, ctx := range []Cancellable{algo2, activity, session} {
		select {
		case <-ctx.Done():
			t.Errorf("Context %s should NOT be cancelled", ctx.ID())
		default:
			// Expected
		}
	}
}

func TestProgressReporting(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Get initial progress time
	initialProgress := algo.LastProgress()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Report progress
	algo.ReportProgress()

	// Verify progress time updated
	newProgress := algo.LastProgress()
	if newProgress <= initialProgress {
		t.Error("Progress time should have been updated")
	}
}

func TestReportProgress_Helper(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Use the helper function
	ctx := algo.Context()
	initialProgress := algo.LastProgress()

	time.Sleep(10 * time.Millisecond)

	ReportProgress(ctx)

	newProgress := algo.LastProgress()
	if newProgress <= initialProgress {
		t.Error("ReportProgress helper should update progress time")
	}
}

func TestPartialResultCollection(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Set partial result collector
	expectedResult := "partial-data"
	algo.SetPartialCollector(func() (any, error) {
		return expectedResult, nil
	})

	// Collect partial result
	result, err := algo.collectPartialResult()
	if err != nil {
		t.Fatalf("collectPartialResult failed: %v", err)
	}
	if result != expectedResult {
		t.Errorf("collectPartialResult() = %v, want %v", result, expectedResult)
	}

	// Verify it's accessible
	if algo.PartialResult() != expectedResult {
		t.Errorf("PartialResult() = %v, want %v", algo.PartialResult(), expectedResult)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")

	// Create many algorithms concurrently
	var wg sync.WaitGroup
	algorithms := make([]*AlgorithmContext, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "algo-" + formatInt(idx)
			algorithms[idx] = activity.NewAlgorithm(name, time.Second)
		}(i)
	}

	wg.Wait()

	// Verify all algorithms were created
	for i, algo := range algorithms {
		if algo == nil {
			t.Errorf("Algorithm %d was not created", i)
		}
	}

	// Cancel half concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			algorithms[idx].Cancel(CancelReason{Type: CancelUser})
		}(i)
	}

	wg.Wait()

	// Report progress on the other half concurrently
	for i := 50; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				algorithms[idx].ReportProgress()
			}
		}(i)
	}

	wg.Wait()
}

func TestContextStatus(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Get status
	status := session.Status()

	if status.ID != "test" {
		t.Errorf("Status.ID = %v, want 'test'", status.ID)
	}
	if status.Level != LevelSession {
		t.Errorf("Status.Level = %v, want %v", status.Level, LevelSession)
	}
	if status.State != StateRunning {
		t.Errorf("Status.State = %v, want %v", status.State, StateRunning)
	}
	if len(status.Children) != 1 {
		t.Errorf("Status.Children count = %d, want 1", len(status.Children))
	}

	// Check activity status
	activityStatus := status.Children[0]
	if activityStatus.ID != "test/activity" {
		t.Errorf("Activity Status.ID = %v, want 'test/activity'", activityStatus.ID)
	}
	if len(activityStatus.Children) != 1 {
		t.Errorf("Activity Status.Children count = %d, want 1", len(activityStatus.Children))
	}

	// Check algorithm status
	algoStatus := activityStatus.Children[0]
	if algoStatus.ID != "test/activity/algo" {
		t.Errorf("Algorithm Status.ID = %v, want 'test/activity/algo'", algoStatus.ID)
	}

	// Cancel and check status
	algo.Cancel(CancelReason{Type: CancelUser, Message: "test"})
	<-algo.Done()

	algoStatus = algo.Status()
	if algoStatus.CancelReason == nil {
		t.Error("CancelReason should be set after cancellation")
	}
	if algoStatus.CancelReason.Type != CancelUser {
		t.Errorf("CancelReason.Type = %v, want %v", algoStatus.CancelReason.Type, CancelUser)
	}
}

func TestGetContextID(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Test GetContextID helper
	id := GetContextID(algo.Context())
	if id != "test/activity/algo" {
		t.Errorf("GetContextID() = %v, want 'test/activity/algo'", id)
	}

	// Test with plain context
	id = GetContextID(context.Background())
	if id != "" {
		t.Errorf("GetContextID(background) = %v, want ''", id)
	}
}

func TestAlgorithmTimeout(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{
		DefaultTimeout: 50 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 50*time.Millisecond)

	// Wait for timeout
	select {
	case <-algo.Done():
		// Expected - context.WithTimeout should have fired
	case <-time.After(200 * time.Millisecond):
		t.Error("Algorithm should have timed out")
	}
}

func TestMarkDone(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})
	activity := session.NewActivity("activity")
	algo := activity.NewAlgorithm("algo", 5*time.Second)

	// Mark as done
	algo.MarkDone()

	if algo.State() != StateDone {
		t.Errorf("State() = %v, want %v", algo.State(), StateDone)
	}
}

func TestDoubleCancellation(t *testing.T) {
	ctrl, err := NewController(ControllerConfig{}, nil)
	if err != nil {
		t.Fatalf("NewController failed: %v", err)
	}
	defer ctrl.Close()

	session, _ := ctrl.NewSession(context.Background(), SessionConfig{ID: "test"})

	// Cancel twice - should not panic
	session.Cancel(CancelReason{Type: CancelUser, Message: "First"})
	session.Cancel(CancelReason{Type: CancelTimeout, Message: "Second"})

	// Verify first reason is kept
	status := session.Status()
	if status.CancelReason.Message != "First" {
		t.Errorf("CancelReason.Message = %v, want 'First'", status.CancelReason.Message)
	}
}
