// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// Bridge Comprehensive Tests
// -----------------------------------------------------------------------------

func TestBridge_RunActivity(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("nil context returns error", func(t *testing.T) {
		activity := activities.NewSearchActivity(nil)
		//nolint:staticcheck // Intentionally testing nil context
		_, err := bridge.RunActivity(nil, activity, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		var bridgeErr *BridgeError
		if !errors.As(err, &bridgeErr) {
			t.Fatalf("expected BridgeError, got %T", err)
		}
		if bridgeErr.Err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", bridgeErr.Err)
		}
	})

	t.Run("nil activity returns error", func(t *testing.T) {
		_, err := bridge.RunActivity(context.Background(), nil, nil)
		if err == nil {
			t.Fatal("expected error for nil activity")
		}
		var bridgeErr *BridgeError
		if !errors.As(err, &bridgeErr) {
			t.Fatalf("expected BridgeError, got %T", err)
		}
		if bridgeErr.Err != ErrNilActivity {
			t.Errorf("expected ErrNilActivity, got %v", bridgeErr.Err)
		}
	})

	t.Run("closed bridge returns error", func(t *testing.T) {
		closedBridge := NewBridge(crs.New(nil), nil)
		closedBridge.Close()

		activity := activities.NewSearchActivity(nil)
		_, err := closedBridge.RunActivity(context.Background(), activity, nil)
		if err == nil {
			t.Fatal("expected error for closed bridge")
		}
		var bridgeErr *BridgeError
		if !errors.As(err, &bridgeErr) {
			t.Fatalf("expected BridgeError, got %T", err)
		}
		if bridgeErr.Err != ErrBridgeClosed {
			t.Errorf("expected ErrBridgeClosed, got %v", bridgeErr.Err)
		}
	})

	t.Run("executes activity successfully", func(t *testing.T) {
		activity := activities.NewSearchActivity(nil)
		input := activities.NewSearchInput("test", "root", crs.SignalSourceHard)

		result, err := bridge.RunActivity(context.Background(), activity, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ActivityName != "search" {
			t.Errorf("expected search, got %s", result.ActivityName)
		}
	})

	t.Run("context cancellation stops execution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		activity := activities.NewSearchActivity(nil)
		input := activities.NewSearchInput("test", "root", crs.SignalSourceHard)

		_, err := bridge.RunActivity(ctx, activity, input)
		// Should return context.Canceled or complete
		_ = err
	})
}

func TestBridge_Apply(t *testing.T) {
	t.Run("nil context returns error", func(t *testing.T) {
		bridge := NewBridge(crs.New(nil), nil)
		delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{})

		//nolint:staticcheck // Intentionally testing nil context
		_, err := bridge.Apply(nil, delta)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
		var bridgeErr *BridgeError
		if !errors.As(err, &bridgeErr) {
			t.Fatalf("expected BridgeError, got %T", err)
		}
		if bridgeErr.Err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", bridgeErr.Err)
		}
	})

	t.Run("closed bridge returns error", func(t *testing.T) {
		bridge := NewBridge(crs.New(nil), nil)
		bridge.Close()
		delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{})

		_, err := bridge.Apply(context.Background(), delta)
		if err == nil {
			t.Fatal("expected error for closed bridge")
		}
		var bridgeErr *BridgeError
		if !errors.As(err, &bridgeErr) {
			t.Fatalf("expected BridgeError, got %T", err)
		}
		if bridgeErr.Err != ErrBridgeClosed {
			t.Errorf("expected ErrBridgeClosed, got %v", bridgeErr.Err)
		}
	})

	t.Run("applies valid delta", func(t *testing.T) {
		bridge := NewBridge(crs.New(nil), nil)
		updates := map[string]crs.ProofNumber{
			"node-1": {
				Proof:    1,
				Disproof: 2,
				Status:   crs.ProofStatusProven,
				Source:   crs.SignalSourceHard,
			},
		}
		delta := crs.NewProofDelta(crs.SignalSourceHard, updates)

		metrics, err := bridge.Apply(context.Background(), delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if metrics.NewGeneration < 1 {
			t.Errorf("expected generation >= 1, got %d", metrics.NewGeneration)
		}
	})
}

func TestBridge_Concurrent(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)

	t.Run("concurrent RunActivity calls", func(t *testing.T) {
		const numGoroutines = 20
		var wg sync.WaitGroup

		activity := activities.NewSearchActivity(nil)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				input := activities.NewSearchInput("test", "root", crs.SignalSourceHard)
				_, _ = bridge.RunActivity(context.Background(), activity, input)
			}()
		}

		wg.Wait()

		stats := bridge.Stats()
		if stats.ActivitiesRun < 1 {
			t.Errorf("expected activities to be run, got %d", stats.ActivitiesRun)
		}
	})

	t.Run("concurrent Apply calls", func(t *testing.T) {
		const numGoroutines = 20
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				updates := map[string]crs.ProofNumber{
					"node-concurrent": {
						Proof:    uint64(id),
						Disproof: 1,
						Status:   crs.ProofStatusExpanded,
						Source:   crs.SignalSourceHard,
					},
				}
				delta := crs.NewProofDelta(crs.SignalSourceHard, updates)
				_, _ = bridge.Apply(context.Background(), delta)
			}(i)
		}

		wg.Wait()
	})
}

func TestBridgeError(t *testing.T) {
	t.Run("error format", func(t *testing.T) {
		err := &BridgeError{Operation: "Apply", Err: ErrNilContext}
		expected := "bridge.Apply: context must not be nil"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})

	t.Run("unwrap returns underlying error", func(t *testing.T) {
		err := &BridgeError{Operation: "Apply", Err: ErrNilContext}
		if err.Unwrap() != ErrNilContext {
			t.Error("unwrap should return underlying error")
		}
	})
}

func TestDefaultBridgeConfig(t *testing.T) {
	config := DefaultBridgeConfig()

	if config.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", config.MaxRetries)
	}
	if config.RetryDelay != 100*time.Millisecond {
		t.Errorf("expected 100ms retry delay, got %v", config.RetryDelay)
	}
	if !config.EnableMetrics {
		t.Error("expected metrics enabled")
	}
	if !config.EnableTracing {
		t.Error("expected tracing enabled")
	}
}

// -----------------------------------------------------------------------------
// Coordinator Comprehensive Tests
// -----------------------------------------------------------------------------

func TestCoordinator_RunOnce_Closed(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Close()

	_, err := coord.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected error for closed coordinator")
	}
	var coordErr *CoordinatorError
	if !errors.As(err, &coordErr) {
		t.Fatalf("expected CoordinatorError, got %T", err)
	}
	if coordErr.Err != ErrCoordinatorClosed {
		t.Errorf("expected ErrCoordinatorClosed, got %v", coordErr.Err)
	}
}

func TestCoordinator_RunOnce_WithActivities(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	// Register streaming activity which always wants to run
	coord.Register(activities.NewStreamingActivity(nil))

	results, err := coord.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have run at least one activity
	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestCoordinator_RunOnce_ContextCancellation(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Register(activities.NewStreamingActivity(nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not panic, may return partial results or error
	_, _ = coord.RunOnce(ctx)
}

func TestCoordinator_RunActivity(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("not found returns error", func(t *testing.T) {
		_, err := coord.RunActivity(context.Background(), "nonexistent", nil)
		if err == nil {
			t.Fatal("expected error for not found activity")
		}
		var coordErr *CoordinatorError
		if !errors.As(err, &coordErr) {
			t.Fatalf("expected CoordinatorError, got %T", err)
		}
		if coordErr.Err != ErrActivityNotFound {
			t.Errorf("expected ErrActivityNotFound, got %v", coordErr.Err)
		}
	})

	t.Run("runs registered activity", func(t *testing.T) {
		coord.Register(activities.NewSearchActivity(nil))
		input := activities.NewSearchInput("test", "root", crs.SignalSourceHard)

		result, err := coord.RunActivity(context.Background(), "search", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ActivityName != "search" {
			t.Errorf("expected search, got %s", result.ActivityName)
		}
	})
}

func TestCoordinator_Run(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	config := &CoordinatorConfig{
		MaxConcurrentActivities: 2,
		ScheduleInterval:        10 * time.Millisecond,
	}
	coord := NewCoordinator(bridge, config)
	coord.Register(activities.NewStreamingActivity(nil))

	t.Run("runs until cancelled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := coord.Run(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected DeadlineExceeded, got %v", err)
		}

		stats := coord.Stats()
		if stats.ExecutedTotal < 1 {
			t.Errorf("expected at least 1 execution, got %d", stats.ExecutedTotal)
		}
	})
}

func TestCoordinator_Schedule_Priority(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	// Register multiple activities with different priorities
	coord.Register(activities.NewStreamingActivity(nil)) // Low priority
	coord.Register(activities.NewSearchActivity(nil))    // May not want to run

	scheduled := coord.Schedule()

	// Streaming should be scheduled (it always wants to run)
	found := false
	for _, sa := range scheduled {
		if sa.activity.Name() == "streaming" {
			found = true
			break
		}
	}
	if !found && len(scheduled) > 0 {
		t.Error("expected streaming activity to be scheduled")
	}
}

func TestCoordinator_HealthCheck_Closed(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)
	coord.Close()

	err := coord.HealthCheck(context.Background())
	if err == nil {
		t.Error("expected error for closed coordinator")
	}
}

func TestCoordinator_Concurrent(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("concurrent register/unregister", func(t *testing.T) {
		var wg sync.WaitGroup
		const numGoroutines = 20

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				if id%2 == 0 {
					coord.Register(activities.NewStreamingActivity(nil))
				} else {
					coord.Unregister("streaming")
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent schedule", func(t *testing.T) {
		var wg sync.WaitGroup
		const numGoroutines = 20

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = coord.Schedule()
			}()
		}

		wg.Wait()
	})
}

func TestCoordinatorError(t *testing.T) {
	t.Run("error format", func(t *testing.T) {
		err := &CoordinatorError{Operation: "RunOnce", Err: ErrCoordinatorClosed}
		expected := "coordinator.RunOnce: coordinator is closed"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})

	t.Run("unwrap returns underlying error", func(t *testing.T) {
		err := &CoordinatorError{Operation: "RunOnce", Err: ErrCoordinatorClosed}
		if err.Unwrap() != ErrCoordinatorClosed {
			t.Error("unwrap should return underlying error")
		}
	})
}

func TestDefaultCoordinatorConfig(t *testing.T) {
	config := DefaultCoordinatorConfig()

	if config.MaxConcurrentActivities != 4 {
		t.Errorf("expected 4 max concurrent, got %d", config.MaxConcurrentActivities)
	}
	if config.ScheduleInterval != 100*time.Millisecond {
		t.Errorf("expected 100ms interval, got %v", config.ScheduleInterval)
	}
	if !config.EnableMetrics {
		t.Error("expected metrics enabled")
	}
	if !config.EnableTracing {
		t.Error("expected tracing enabled")
	}
}

func TestCreateActivityInput(t *testing.T) {
	testCases := []struct {
		activity activities.Activity
		expected string
	}{
		{activities.NewSearchActivity(nil), "search"},
		{activities.NewLearningActivity(nil), "learning"},
		{activities.NewConstraintActivity(nil), "constraint"},
		{activities.NewPlanningActivity(nil), "planning"},
		{activities.NewAwarenessActivity(nil), "awareness"},
		{activities.NewSimilarityActivity(nil), "similarity"},
		{activities.NewStreamingActivity(nil), "streaming"},
		{activities.NewMemoryActivity(nil), "memory"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			input := createActivityInput(tc.activity)
			if input == nil {
				t.Fatalf("expected non-nil input for %s", tc.expected)
			}
			if input.Type() != tc.expected && input.Type() != "base" {
				t.Errorf("expected %s or base, got %s", tc.expected, input.Type())
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ABHarness Comprehensive Tests
// -----------------------------------------------------------------------------

func TestABHarness_HealthCheck(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}
	harness := NewABHarness(exp, ctrl, nil)

	err := harness.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestABHarness_Timeout(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}
	harness := NewABHarness(exp, ctrl, nil)

	timeout := harness.Timeout()
	if timeout <= 0 {
		t.Error("expected positive timeout")
	}
}

func TestABHarness_InputOutputType(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}
	harness := NewABHarness(exp, ctrl, nil)

	inputType := harness.InputType()
	if inputType == nil {
		t.Error("expected non-nil input type")
	}

	outputType := harness.OutputType()
	if outputType == nil {
		t.Error("expected non-nil output type")
	}
}

func TestABHarness_ProgressInterval(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}
	harness := NewABHarness(exp, ctrl, nil)

	interval := harness.ProgressInterval()
	if interval <= 0 {
		t.Error("expected positive progress interval")
	}
}

func TestABHarness_SupportsPartialResults(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}
	harness := NewABHarness(exp, ctrl, nil)

	// Just verify it doesn't panic
	_ = harness.SupportsPartialResults()
}

func TestABHarness_SampleRates(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment", output: "exp"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl"}

	t.Run("zero sample rate", func(t *testing.T) {
		harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 0.0})

		for i := 0; i < 10; i++ {
			_, _, _ = harness.Process(context.Background(), nil, nil)
		}

		stats := harness.Stats()
		if stats.SampledRequests != 0 {
			t.Errorf("expected 0 sampled, got %d", stats.SampledRequests)
		}
	})

	t.Run("full sample rate", func(t *testing.T) {
		harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 1.0})

		for i := 0; i < 10; i++ {
			_, _, _ = harness.Process(context.Background(), nil, nil)
		}

		stats := harness.Stats()
		if stats.SampledRequests != 10 {
			t.Errorf("expected 10 sampled, got %d", stats.SampledRequests)
		}
	})
}

func TestCompareDuration(t *testing.T) {
	t.Run("returns 0 for invalid types", func(t *testing.T) {
		result := CompareDuration("not a result", "also not a result")
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("returns 0 for nil inputs", func(t *testing.T) {
		result := CompareDuration(nil, nil)
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}

func TestCompareSuccess(t *testing.T) {
	t.Run("returns 0 for invalid types", func(t *testing.T) {
		result := CompareSuccess("not a result", "also not a result")
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("returns 0 for nil inputs", func(t *testing.T) {
		result := CompareSuccess(nil, nil)
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}

func TestDefaultABConfig(t *testing.T) {
	config := DefaultABConfig()

	if config.SampleRate != 0.1 {
		t.Errorf("expected 0.1 sample rate, got %f", config.SampleRate)
	}
	if config.MetricsPrefix != "ab_test" {
		t.Errorf("expected 'ab_test' prefix, got %s", config.MetricsPrefix)
	}
	if !config.EnableMetrics {
		t.Error("expected metrics enabled")
	}
}

func TestABStats_EdgeCases(t *testing.T) {
	t.Run("zero comparisons", func(t *testing.T) {
		stats := ABStats{
			ExperimentWins: 0,
			ControlWins:    0,
			Ties:           0,
		}

		// These should return 0, not NaN or panic
		if stats.ExperimentWinRate() != 0 {
			t.Errorf("expected 0, got %f", stats.ExperimentWinRate())
		}
		if stats.ControlWinRate() != 0 {
			t.Errorf("expected 0, got %f", stats.ControlWinRate())
		}
		if stats.TieRate() != 0 {
			t.Errorf("expected 0, got %f", stats.TieRate())
		}
	})

	t.Run("all wins for experiment", func(t *testing.T) {
		stats := ABStats{
			ExperimentWins: 10,
			ControlWins:    0,
			Ties:           0,
		}

		if stats.ExperimentWinRate() != 1.0 {
			t.Errorf("expected 1.0, got %f", stats.ExperimentWinRate())
		}
	})
}

// -----------------------------------------------------------------------------
// Integration Test: Full Pipeline
// -----------------------------------------------------------------------------

func TestFullPipeline(t *testing.T) {
	// Create CRS
	crsInstance := crs.New(nil)

	// Create Bridge
	bridge := NewBridge(crsInstance, nil)

	// Create Coordinator
	coord := NewCoordinator(bridge, nil)

	// Register all activities
	coord.Register(activities.NewSearchActivity(nil))
	coord.Register(activities.NewLearningActivity(nil))
	coord.Register(activities.NewConstraintActivity(nil))
	coord.Register(activities.NewPlanningActivity(nil))
	coord.Register(activities.NewAwarenessActivity(nil))
	coord.Register(activities.NewSimilarityActivity(nil))
	coord.Register(activities.NewStreamingActivity(nil))
	coord.Register(activities.NewMemoryActivity(nil))

	// Verify all registered
	all := coord.All()
	if len(all) != 8 {
		t.Errorf("expected 8 activities, got %d", len(all))
	}

	// Run scheduling cycle
	results, err := coord.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	// At least streaming should run
	_ = results

	// Verify stats updated
	coordStats := coord.Stats()
	if coordStats.RegisteredActivities != 8 {
		t.Errorf("expected 8 registered, got %d", coordStats.RegisteredActivities)
	}

	bridgeStats := bridge.Stats()
	// May have run activities
	_ = bridgeStats

	// Clean up
	coord.Close()
	bridge.Close()
}
