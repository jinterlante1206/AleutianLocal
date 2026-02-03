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
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Bridge Tests
// -----------------------------------------------------------------------------

func TestNewBridge(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		crsInstance := crs.New(nil)
		bridge := NewBridge(crsInstance, nil)
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &BridgeConfig{
			MaxRetries: 5,
			RetryDelay: 200 * time.Millisecond,
		}
		bridge := NewBridge(crs.New(nil), config)
		if bridge.config.MaxRetries != 5 {
			t.Errorf("expected 5 retries, got %d", bridge.config.MaxRetries)
		}
	})
}

func TestBridge_Snapshot(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("returns valid snapshot", func(t *testing.T) {
		snapshot := bridge.Snapshot()
		if snapshot == nil {
			t.Fatal("expected non-nil snapshot")
		}
		if snapshot.Generation() < 0 {
			t.Error("expected non-negative generation")
		}
	})
}

func TestBridge_Generation(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("returns current generation", func(t *testing.T) {
		gen := bridge.Generation()
		if gen < 0 {
			t.Error("expected non-negative generation")
		}
	})
}

func TestBridge_Close(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("close succeeds", func(t *testing.T) {
		err := bridge.Close()
		if err != nil {
			t.Errorf("close failed: %v", err)
		}
	})

	t.Run("double close is safe", func(t *testing.T) {
		err := bridge.Close()
		if err != nil {
			t.Errorf("double close failed: %v", err)
		}
	})
}

func TestBridge_Stats(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("returns stats", func(t *testing.T) {
		stats := bridge.Stats()
		if stats.ActivitiesRun != 0 {
			t.Errorf("expected 0 activities, got %d", stats.ActivitiesRun)
		}
	})
}

func TestBridge_HealthCheck(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	t.Run("passes when open", func(t *testing.T) {
		err := bridge.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails when closed", func(t *testing.T) {
		bridge.Close()
		err := bridge.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for closed bridge")
		}
	})
}

func TestBridge_Evaluable(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)

	t.Run("has properties", func(t *testing.T) {
		props := bridge.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := bridge.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// Bridge TraceRecorder Tests (CB-29-2a)
// -----------------------------------------------------------------------------

func TestNewBridge_WithTraceRecorder(t *testing.T) {
	crsInstance := crs.New(nil)
	recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())

	bridge := NewBridge(crsInstance, nil, WithTraceRecorder(recorder))

	if bridge.TraceRecorder() == nil {
		t.Fatal("expected non-nil trace recorder")
	}
	if bridge.TraceRecorder() != recorder {
		t.Error("expected same recorder instance")
	}
}

func TestNewBridge_WithoutTraceRecorder(t *testing.T) {
	crsInstance := crs.New(nil)

	bridge := NewBridge(crsInstance, nil)

	if bridge.TraceRecorder() != nil {
		t.Error("expected nil trace recorder")
	}
}

func TestNewBridge_WithNilTraceRecorder(t *testing.T) {
	crsInstance := crs.New(nil)

	bridge := NewBridge(crsInstance, nil, WithTraceRecorder(nil))

	if bridge.TraceRecorder() != nil {
		t.Error("expected nil trace recorder")
	}
}

func TestBridge_SetTraceRecorder(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)
	recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())

	// Initially nil
	if bridge.TraceRecorder() != nil {
		t.Error("expected nil trace recorder initially")
	}

	// Set recorder
	bridge.SetTraceRecorder(recorder)

	if bridge.TraceRecorder() == nil {
		t.Fatal("expected non-nil trace recorder after set")
	}
	if bridge.TraceRecorder() != recorder {
		t.Error("expected same recorder instance")
	}

	// Clear recorder
	bridge.SetTraceRecorder(nil)

	if bridge.TraceRecorder() != nil {
		t.Error("expected nil trace recorder after clear")
	}
}

func TestBridge_SetTraceRecorder_Concurrent(t *testing.T) {
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
			bridge.SetTraceRecorder(recorder)
		}()
		go func() {
			defer wg.Done()
			_ = bridge.TraceRecorder()
		}()
	}
	wg.Wait()

	// Test passes if no race condition detected
}

func TestBridgeConfig_EnableTraceRecording_Default(t *testing.T) {
	config := DefaultBridgeConfig()

	if !config.EnableTraceRecording {
		t.Error("expected EnableTraceRecording to default to true")
	}
}

func TestBridgeConfig_EnableTraceRecording_Custom(t *testing.T) {
	config := &BridgeConfig{
		EnableTraceRecording: false,
	}
	crsInstance := crs.New(nil)
	bridge := NewBridge(crsInstance, config)

	if bridge.config.EnableTraceRecording {
		t.Error("expected EnableTraceRecording to be false")
	}
}

// -----------------------------------------------------------------------------
// Coordinator Tests
// -----------------------------------------------------------------------------

func TestNewCoordinator(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)

	t.Run("creates with default config", func(t *testing.T) {
		coord := NewCoordinator(bridge, nil)
		if coord == nil {
			t.Fatal("expected non-nil coordinator")
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &CoordinatorConfig{
			MaxConcurrentActivities: 8,
			ScheduleInterval:        50 * time.Millisecond,
		}
		coord := NewCoordinator(bridge, config)
		if coord.config.MaxConcurrentActivities != 8 {
			t.Errorf("expected 8 concurrent, got %d", coord.config.MaxConcurrentActivities)
		}
	})
}

func TestCoordinator_Register(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("registers activity", func(t *testing.T) {
		activity := activities.NewSearchActivity(nil)
		coord.Register(activity)

		got, ok := coord.Get("search")
		if !ok {
			t.Fatal("expected to find registered activity")
		}
		if got.Name() != "search" {
			t.Errorf("expected search, got %s", got.Name())
		}
	})
}

func TestCoordinator_Unregister(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("unregisters activity", func(t *testing.T) {
		activity := activities.NewSearchActivity(nil)
		coord.Register(activity)
		coord.Unregister("search")

		_, ok := coord.Get("search")
		if ok {
			t.Error("expected activity to be unregistered")
		}
	})
}

func TestCoordinator_All(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("returns all activities", func(t *testing.T) {
		coord.Register(activities.NewSearchActivity(nil))
		coord.Register(activities.NewLearningActivity(nil))

		all := coord.All()
		if len(all) != 2 {
			t.Errorf("expected 2 activities, got %d", len(all))
		}
	})
}

func TestCoordinator_Schedule(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("returns empty for no activities", func(t *testing.T) {
		scheduled := coord.Schedule()
		if len(scheduled) != 0 {
			t.Errorf("expected 0 scheduled, got %d", len(scheduled))
		}
	})

	t.Run("schedules activities that want to run", func(t *testing.T) {
		// Register streaming which always wants to run
		coord.Register(activities.NewStreamingActivity(nil))

		scheduled := coord.Schedule()
		if len(scheduled) != 1 {
			t.Errorf("expected 1 scheduled, got %d", len(scheduled))
		}
	})
}

func TestCoordinator_RunOnce(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("returns nil for no activities", func(t *testing.T) {
		results, err := coord.RunOnce(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})
}

func TestCoordinator_Close(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("close succeeds", func(t *testing.T) {
		err := coord.Close()
		if err != nil {
			t.Errorf("close failed: %v", err)
		}
	})
}

func TestCoordinator_Stats(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("returns stats", func(t *testing.T) {
		stats := coord.Stats()
		if stats.RegisteredActivities != 0 {
			t.Errorf("expected 0 registered, got %d", stats.RegisteredActivities)
		}
	})
}

func TestCoordinator_HealthCheck(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("passes when open", func(t *testing.T) {
		err := coord.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})
}

func TestCoordinator_Evaluable(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	t.Run("has properties", func(t *testing.T) {
		props := coord.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := coord.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// A/B Testing Tests
// -----------------------------------------------------------------------------

func TestNewABHarness(t *testing.T) {
	// Create mock algorithms for testing
	exp := &mockAlgorithm{name: "experiment"}
	ctrl := &mockAlgorithm{name: "control"}

	t.Run("creates with default config", func(t *testing.T) {
		harness := NewABHarness(exp, ctrl, nil)
		if harness == nil {
			t.Fatal("expected non-nil harness")
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &ABConfig{
			SampleRate:    0.5,
			MetricsPrefix: "test",
		}
		harness := NewABHarness(exp, ctrl, config)
		if harness.config.SampleRate != 0.5 {
			t.Errorf("expected 0.5 sample rate, got %f", harness.config.SampleRate)
		}
	})
}

func TestABHarness_Name(t *testing.T) {
	harness := NewABHarness(&mockAlgorithm{}, &mockAlgorithm{}, nil)

	t.Run("includes prefix", func(t *testing.T) {
		name := harness.Name()
		if name != "ab_test_harness" {
			t.Errorf("expected ab_test_harness, got %s", name)
		}
	})
}

func TestABHarness_Process(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment", output: "exp_result"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl_result"}

	t.Run("returns control result", func(t *testing.T) {
		harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 1.0})

		output, _, err := harness.Process(context.Background(), nil, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if output != "ctrl_result" {
			t.Errorf("expected ctrl_result, got %v", output)
		}
	})
}

func TestABHarness_Stats(t *testing.T) {
	harness := NewABHarness(&mockAlgorithm{}, &mockAlgorithm{}, nil)

	t.Run("initial stats are zero", func(t *testing.T) {
		stats := harness.Stats()
		if stats.TotalRequests != 0 {
			t.Errorf("expected 0 total, got %d", stats.TotalRequests)
		}
	})
}

func TestABStats(t *testing.T) {
	t.Run("win rates calculate correctly", func(t *testing.T) {
		stats := ABStats{
			ExperimentWins: 3,
			ControlWins:    2,
			Ties:           5,
		}

		if stats.ExperimentWinRate() != 0.3 {
			t.Errorf("expected 0.3, got %f", stats.ExperimentWinRate())
		}
		if stats.ControlWinRate() != 0.2 {
			t.Errorf("expected 0.2, got %f", stats.ControlWinRate())
		}
		if stats.TieRate() != 0.5 {
			t.Errorf("expected 0.5, got %f", stats.TieRate())
		}
	})

	t.Run("sample rate calculates correctly", func(t *testing.T) {
		stats := ABStats{
			TotalRequests:   100,
			SampledRequests: 10,
		}

		if stats.SampleRate() != 0.1 {
			t.Errorf("expected 0.1, got %f", stats.SampleRate())
		}
	})

	t.Run("handles zero division", func(t *testing.T) {
		stats := ABStats{}
		if stats.ExperimentWinRate() != 0 {
			t.Error("expected 0 for empty stats")
		}
		if stats.SampleRate() != 0 {
			t.Error("expected 0 for empty stats")
		}
	})
}

func TestABHarness_Evaluable(t *testing.T) {
	harness := NewABHarness(&mockAlgorithm{}, &mockAlgorithm{}, nil)

	t.Run("has properties", func(t *testing.T) {
		props := harness.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := harness.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// Mock Algorithm
// -----------------------------------------------------------------------------

type mockAlgorithm struct {
	name   string
	output any
}

func (m *mockAlgorithm) Name() string {
	return m.name
}

func (m *mockAlgorithm) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	return m.output, nil, nil
}

func (m *mockAlgorithm) Timeout() time.Duration {
	return 5 * time.Second
}

func (m *mockAlgorithm) InputType() reflect.Type {
	return reflect.TypeOf((*any)(nil)).Elem()
}

func (m *mockAlgorithm) OutputType() reflect.Type {
	return reflect.TypeOf((*any)(nil)).Elem()
}

func (m *mockAlgorithm) ProgressInterval() time.Duration {
	return time.Second
}

func (m *mockAlgorithm) SupportsPartialResults() bool {
	return true
}

func (m *mockAlgorithm) Properties() []eval.Property {
	return nil
}

func (m *mockAlgorithm) Metrics() []eval.MetricDefinition {
	return nil
}

func (m *mockAlgorithm) HealthCheck(ctx context.Context) error {
	return nil
}

// -----------------------------------------------------------------------------
// Concurrent A/B Testing Tests (CR-11-23)
// -----------------------------------------------------------------------------

func TestABHarness_ConcurrentProcess(t *testing.T) {
	exp := &mockAlgorithm{name: "experiment", output: "exp_result"}
	ctrl := &mockAlgorithm{name: "control", output: "ctrl_result"}

	harness := NewABHarness(exp, ctrl, &ABConfig{SampleRate: 1.0})

	t.Run("concurrent calls are thread-safe", func(t *testing.T) {
		const numGoroutines = 100
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := harness.Process(context.Background(), nil, nil)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}

		wg.Wait()

		stats := harness.Stats()
		if stats.TotalRequests != numGoroutines {
			t.Errorf("expected %d total requests, got %d", numGoroutines, stats.TotalRequests)
		}
	})
}

// -----------------------------------------------------------------------------
// Nil Handling Tests (CR-11-22)
// -----------------------------------------------------------------------------

func TestActivity_HandlesNilEvaluableReturns(t *testing.T) {
	bridge := NewBridge(crs.New(nil), nil)
	coord := NewCoordinator(bridge, nil)

	// Register activities that return nil from Properties/Metrics
	searchActivity := activities.NewSearchActivity(nil)
	coord.Register(searchActivity)

	t.Run("coordinator handles activities with nil properties", func(t *testing.T) {
		// This should not panic
		for _, activity := range coord.All() {
			props := activity.Properties()
			// Properties may be nil or empty, both are valid
			_ = props

			metrics := activity.Metrics()
			// Metrics may be nil or empty, both are valid
			_ = metrics
		}
	})
}

// -----------------------------------------------------------------------------
// Observability Integration Tests (CB-29-2c)
// -----------------------------------------------------------------------------

func TestBridge_RecordTraceStep_Integration(t *testing.T) {
	t.Run("records trace step after activity execution", func(t *testing.T) {
		crsInstance := crs.New(nil)
		recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
		config := &BridgeConfig{
			EnableTraceRecording: true,
			EnableMetrics:        true,
			EnableTracing:        false, // Disable OTel for this test
			MaxRetries:           0,
		}
		bridge := NewBridge(crsInstance, config, WithTraceRecorder(recorder))

		// Create a mock activity that succeeds
		activity := &mockActivity{
			name: "test_activity",
			result: activities.ActivityResult{
				ActivityName: "test_activity",
				Success:      true,
				Duration:     100 * time.Millisecond,
			},
		}

		// Register activity name for metrics
		RegisterActivityName("test_activity")

		// Run activity
		_, err := bridge.RunActivity(context.Background(), activity, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify trace step was recorded
		steps := recorder.GetSteps()
		if len(steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(steps))
		}

		step := steps[0]
		if step.Action != "test_activity" {
			t.Errorf("expected action 'test_activity', got %q", step.Action)
		}
	})

	t.Run("records trace step for failed activities", func(t *testing.T) {
		crsInstance := crs.New(nil)
		recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
		config := &BridgeConfig{
			EnableTraceRecording: true,
			EnableMetrics:        true,
			EnableTracing:        false,
			MaxRetries:           0,
		}
		bridge := NewBridge(crsInstance, config, WithTraceRecorder(recorder))

		// Create a mock activity that fails but returns a result
		activity := &mockActivity{
			name: "failing_activity",
			result: activities.ActivityResult{
				ActivityName: "failing_activity",
				Success:      false,
				Duration:     50 * time.Millisecond,
			},
		}

		RegisterActivityName("failing_activity")

		// Run activity - it should complete without error (activity failure != execution error)
		_, err := bridge.RunActivity(context.Background(), activity, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify trace step was recorded even for failed activity (DR-6/DR-15)
		steps := recorder.GetSteps()
		if len(steps) != 1 {
			t.Fatalf("expected 1 step for failed activity, got %d", len(steps))
		}
	})

	t.Run("no recording when recorder is nil", func(t *testing.T) {
		crsInstance := crs.New(nil)
		config := &BridgeConfig{
			EnableTraceRecording: true,
			EnableMetrics:        false,
			EnableTracing:        false,
			MaxRetries:           0,
		}
		// No recorder provided
		bridge := NewBridge(crsInstance, config)

		activity := &mockActivity{
			name: "test_activity",
			result: activities.ActivityResult{
				ActivityName: "test_activity",
				Success:      true,
			},
		}

		// Should not panic
		_, err := bridge.RunActivity(context.Background(), activity, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no recording when EnableTraceRecording is false", func(t *testing.T) {
		crsInstance := crs.New(nil)
		recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
		config := &BridgeConfig{
			EnableTraceRecording: false, // Disabled
			EnableMetrics:        true,
			EnableTracing:        false,
			MaxRetries:           0,
		}
		bridge := NewBridge(crsInstance, config, WithTraceRecorder(recorder))

		activity := &mockActivity{
			name: "test_activity",
			result: activities.ActivityResult{
				ActivityName: "test_activity",
				Success:      true,
			},
		}

		_, err := bridge.RunActivity(context.Background(), activity, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify no step was recorded
		steps := recorder.GetSteps()
		if len(steps) != 0 {
			t.Errorf("expected 0 steps when recording disabled, got %d", len(steps))
		}
	})
}

func TestBridge_RecordTraceStep_WithDelta(t *testing.T) {
	t.Run("records step with delta information", func(t *testing.T) {
		crsInstance := crs.New(nil)
		recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
		config := &BridgeConfig{
			EnableTraceRecording: true,
			EnableMetrics:        true,
			EnableTracing:        false,
			MaxRetries:           0,
		}
		bridge := NewBridge(crsInstance, config, WithTraceRecorder(recorder))

		// Create activity with a delta
		delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"node1": {
				Status: crs.ProofStatusProven,
				Source: crs.SignalSourceHard,
			},
		})

		activity := &mockActivityWithDelta{
			name: "delta_activity",
			result: activities.ActivityResult{
				ActivityName: "delta_activity",
				Success:      true,
				Duration:     100 * time.Millisecond,
			},
			delta: delta,
		}

		RegisterActivityName("delta_activity")

		_, err := bridge.RunActivity(context.Background(), activity, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify trace step includes delta information
		steps := recorder.GetSteps()
		if len(steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(steps))
		}

		step := steps[0]
		if len(step.ProofUpdates) != 1 {
			t.Errorf("expected 1 proof update, got %d", len(step.ProofUpdates))
		}
	})
}

func TestBridge_RecordTraceStep_ConcurrentSafety(t *testing.T) {
	t.Run("concurrent activity executions are thread-safe", func(t *testing.T) {
		crsInstance := crs.New(nil)
		recorder := crs.NewTraceRecorder(crs.DefaultTraceConfig())
		config := &BridgeConfig{
			EnableTraceRecording: true,
			EnableMetrics:        true,
			EnableTracing:        false,
			MaxRetries:           0,
		}
		bridge := NewBridge(crsInstance, config, WithTraceRecorder(recorder))

		const numGoroutines = 50
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				activity := &mockActivity{
					name: "search",
					result: activities.ActivityResult{
						ActivityName: "search",
						Success:      true,
						Duration:     time.Duration(i) * time.Millisecond,
					},
				}
				_, err := bridge.RunActivity(context.Background(), activity, nil)
				if err != nil {
					t.Errorf("unexpected error in goroutine %d: %v", i, err)
				}
			}(i)
		}

		wg.Wait()

		// Verify all steps were recorded
		steps := recorder.GetSteps()
		if len(steps) != numGoroutines {
			t.Errorf("expected %d steps, got %d", numGoroutines, len(steps))
		}
	})
}

// -----------------------------------------------------------------------------
// Mock Activity for Tests
// -----------------------------------------------------------------------------

type mockActivity struct {
	name   string
	result activities.ActivityResult
}

func (m *mockActivity) Name() string {
	return m.name
}

func (m *mockActivity) Execute(ctx context.Context, snapshot crs.Snapshot, input activities.ActivityInput) (activities.ActivityResult, crs.Delta, error) {
	return m.result, nil, nil
}

func (m *mockActivity) ShouldRun(snapshot crs.Snapshot) (bool, activities.Priority) {
	return true, activities.PriorityNormal
}

func (m *mockActivity) Algorithms() []algorithms.Algorithm {
	return nil
}

func (m *mockActivity) Timeout() time.Duration {
	return 10 * time.Second
}

func (m *mockActivity) Properties() []eval.Property {
	return nil
}

func (m *mockActivity) Metrics() []eval.MetricDefinition {
	return nil
}

func (m *mockActivity) HealthCheck(ctx context.Context) error {
	return nil
}

type mockActivityWithDelta struct {
	name   string
	result activities.ActivityResult
	delta  crs.Delta
}

func (m *mockActivityWithDelta) Name() string {
	return m.name
}

func (m *mockActivityWithDelta) Execute(ctx context.Context, snapshot crs.Snapshot, input activities.ActivityInput) (activities.ActivityResult, crs.Delta, error) {
	return m.result, m.delta, nil
}

func (m *mockActivityWithDelta) ShouldRun(snapshot crs.Snapshot) (bool, activities.Priority) {
	return true, activities.PriorityNormal
}

func (m *mockActivityWithDelta) Algorithms() []algorithms.Algorithm {
	return nil
}

func (m *mockActivityWithDelta) Timeout() time.Duration {
	return 10 * time.Second
}

func (m *mockActivityWithDelta) Properties() []eval.Property {
	return nil
}

func (m *mockActivityWithDelta) Metrics() []eval.MetricDefinition {
	return nil
}

func (m *mockActivityWithDelta) HealthCheck(ctx context.Context) error {
	return nil
}
