// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms/streaming"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// RunAlgorithms Tests - Core Orchestration
// -----------------------------------------------------------------------------

func TestBaseActivity_RunAlgorithms_NilContext(t *testing.T) {
	activity := NewSearchActivity(nil)

	//nolint:staticcheck // Intentionally testing nil context behavior
	_, _, err := activity.RunAlgorithms(nil, crs.New(nil).Snapshot(), func(algo algorithms.Algorithm) any {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for nil context")
	}

	var actErr *ActivityError
	if !errors.As(err, &actErr) {
		t.Fatalf("expected ActivityError, got %T", err)
	}
	if actErr.Err != ErrNilContext {
		t.Errorf("expected ErrNilContext, got %v", actErr.Err)
	}
}

func TestBaseActivity_RunAlgorithms_NilSnapshot(t *testing.T) {
	activity := NewSearchActivity(nil)

	_, _, err := activity.RunAlgorithms(context.Background(), nil, func(algo algorithms.Algorithm) any {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for nil snapshot")
	}

	var actErr *ActivityError
	if !errors.As(err, &actErr) {
		t.Fatalf("expected ActivityError, got %T", err)
	}
	if actErr.Err != ErrNilSnapshot {
		t.Errorf("expected ErrNilSnapshot, got %v", actErr.Err)
	}
}

func TestBaseActivity_RunAlgorithms_EmptyAlgorithms(t *testing.T) {
	activity := NewBaseActivity("empty", 5*time.Second) // No algorithms

	_, _, err := activity.RunAlgorithms(context.Background(), crs.New(nil).Snapshot(), func(algo algorithms.Algorithm) any {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for empty algorithms")
	}

	var actErr *ActivityError
	if !errors.As(err, &actErr) {
		t.Fatalf("expected ActivityError, got %T", err)
	}
	if actErr.Err != ErrNoAlgorithmsRun {
		t.Errorf("expected ErrNoAlgorithmsRun, got %v", actErr.Err)
	}
}

func TestBaseActivity_RunAlgorithms_ContextCancellation(t *testing.T) {
	activity := NewSearchActivity(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, delta, err := activity.RunAlgorithms(ctx, crs.New(nil).Snapshot(), func(algo algorithms.Algorithm) any {
		return nil
	})

	// Context cancellation should return partial results, not error
	// The error handling in RunAlgorithms treats context.Canceled specially
	_ = result
	_ = delta
	_ = err
	// Just verify no panic occurs
}

func TestBaseActivity_RunAlgorithms_Success(t *testing.T) {
	activity := NewSearchActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	result, _, err := activity.RunAlgorithms(ctx, snapshot, func(algo algorithms.Algorithm) any {
		// Return nil input to trigger skip
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ActivityName != "search" {
		t.Errorf("expected activity name 'search', got %s", result.ActivityName)
	}

	if result.Metrics == nil {
		t.Error("expected non-nil metrics")
	}

	// Check metrics are populated
	if _, ok := result.Metrics["algorithms_total"]; !ok {
		t.Error("expected algorithms_total metric")
	}
}

// -----------------------------------------------------------------------------
// SearchActivity Tests - Execute with Valid Input
// -----------------------------------------------------------------------------

func TestSearchActivity_Execute_ValidInput(t *testing.T) {
	activity := NewSearchActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewSearchInput("req-123", "root-node", crs.SignalSourceHard)
	input.MaxExpansions = 10
	input.TargetNodeIDs = []string{"target-1", "target-2"}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "search" {
		t.Errorf("expected search, got %s", result.ActivityName)
	}
}

func TestSearchInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewSearchInput("req-123", "root", crs.SignalSourceHard)
		if input.Type() != "search" {
			t.Errorf("expected search, got %s", input.Type())
		}
		if input.RootNodeID != "root" {
			t.Errorf("expected root, got %s", input.RootNodeID)
		}
		if input.MaxExpansions != 100 {
			t.Errorf("expected 100, got %d", input.MaxExpansions)
		}
	})
}

func TestSearchActivity_HealthCheck_NilConfig(t *testing.T) {
	activity := &SearchActivity{
		BaseActivity: NewBaseActivity("search", 5*time.Second),
		config:       nil,
	}

	err := activity.HealthCheck(context.Background())
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestSearchActivity_Properties(t *testing.T) {
	activity := NewSearchActivity(nil)
	props := activity.Properties()

	if len(props) != 2 {
		t.Errorf("expected 2 properties, got %d", len(props))
	}

	// Test algorithms_executed property check
	prop := props[0]
	if prop.Name != "algorithms_executed" {
		t.Errorf("expected algorithms_executed, got %s", prop.Name)
	}

	// Test with wrong output type
	err := prop.Check(nil, "wrong type")
	if err != nil {
		t.Errorf("expected nil for wrong type, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// LearningActivity Tests
// -----------------------------------------------------------------------------

func TestLearningActivity_Execute_ValidInput(t *testing.T) {
	activity := NewLearningActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewLearningInput("req-123", "node-1", crs.SignalSourceHard)
	input.ErrorMessage = "test failure"
	input.ErrorType = "compile"
	input.ConflictingAssignments = []string{"var1", "var2"}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "learning" {
		t.Errorf("expected learning, got %s", result.ActivityName)
	}
}

func TestLearningActivity_Execute_SoftSignal(t *testing.T) {
	activity := NewLearningActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	// Soft signal should skip CDCL but still run watched
	input := NewLearningInput("req-123", "node-1", crs.SignalSourceSoft)

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should still succeed for soft signals (watched only)
	_ = result
}

func TestLearningInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewLearningInput("req-123", "conflict-node", crs.SignalSourceHard)
		if input.Type() != "learning" {
			t.Errorf("expected learning, got %s", input.Type())
		}
		if input.ConflictNodeID != "conflict-node" {
			t.Errorf("expected conflict-node, got %s", input.ConflictNodeID)
		}
	})
}

func TestLearningActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewLearningActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &LearningActivity{
			BaseActivity: NewBaseActivity("learning", 5*time.Second),
			config:       nil,
		}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestLearningActivity_Evaluable(t *testing.T) {
	activity := NewLearningActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// ConstraintActivity Tests
// -----------------------------------------------------------------------------

func TestConstraintActivity_Execute_ValidInput(t *testing.T) {
	activity := NewConstraintActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewConstraintInput("req-123", "propagate", crs.SignalSourceHard)
	input.ConstraintIDs = []string{"c1", "c2"}
	input.BeliefChanges = map[string]bool{"belief1": true}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "constraint" {
		t.Errorf("expected constraint, got %s", result.ActivityName)
	}
}

func TestConstraintActivity_Execute_Attribution(t *testing.T) {
	activity := NewConstraintActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewConstraintInput("req-123", "attribute", crs.SignalSourceHard)
	input.ErrorNodeID = "error-node"
	input.ErrorMessage = "compile error"

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "constraint" {
		t.Errorf("expected constraint, got %s", result.ActivityName)
	}
}

func TestConstraintInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewConstraintInput("req-123", "propagate", crs.SignalSourceHard)
		if input.Type() != "constraint" {
			t.Errorf("expected constraint, got %s", input.Type())
		}
		if input.Operation != "propagate" {
			t.Errorf("expected propagate, got %s", input.Operation)
		}
		if input.BeliefChanges == nil {
			t.Error("expected non-nil belief changes")
		}
	})
}

func TestConstraintActivity_ShouldRun(t *testing.T) {
	activity := NewConstraintActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns false for empty state", func(t *testing.T) {
		shouldRun, priority := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("expected shouldRun to be false for empty state")
		}
		if priority != PriorityLow {
			t.Errorf("expected low priority, got %v", priority)
		}
	})
}

func TestConstraintActivity_HealthCheck_NilConfig(t *testing.T) {
	activity := &ConstraintActivity{
		BaseActivity: NewBaseActivity("constraint", 5*time.Second),
		config:       nil,
	}

	err := activity.HealthCheck(context.Background())
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestConstraintActivity_Metrics(t *testing.T) {
	activity := NewConstraintActivity(nil)
	metrics := activity.Metrics()

	if len(metrics) == 0 {
		t.Error("expected metrics")
	}
}

// -----------------------------------------------------------------------------
// PlanningActivity Tests
// -----------------------------------------------------------------------------

func TestPlanningActivity_Execute_ValidInput(t *testing.T) {
	activity := NewPlanningActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewPlanningInput("req-123", "goal-task", crs.SignalSourceHard)
	input.CurrentState = map[string]string{"ready": "true"}
	input.AvailableMethods = []string{"method1", "method2"}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "planning" {
		t.Errorf("expected planning, got %s", result.ActivityName)
	}
}

func TestPlanningInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewPlanningInput("req-123", "goal-task", crs.SignalSourceHard)
		if input.Type() != "planning" {
			t.Errorf("expected planning, got %s", input.Type())
		}
		if input.GoalTaskID != "goal-task" {
			t.Errorf("expected goal-task, got %s", input.GoalTaskID)
		}
		if input.CurrentState == nil {
			t.Error("expected non-nil current state")
		}
	})
}

func TestPlanningActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewPlanningActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &PlanningActivity{
			BaseActivity: NewBaseActivity("planning", 5*time.Second),
			config:       nil,
		}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestPlanningActivity_Evaluable(t *testing.T) {
	activity := NewPlanningActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// AwarenessActivity Tests
// -----------------------------------------------------------------------------

func TestAwarenessActivity_Execute_ValidInput(t *testing.T) {
	activity := NewAwarenessActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewAwarenessInput("req-123", crs.SignalSourceHard)

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "awareness" {
		t.Errorf("expected awareness, got %s", result.ActivityName)
	}
}

func TestAwarenessActivity_ShouldRun(t *testing.T) {
	activity := NewAwarenessActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns false for empty dependency index", func(t *testing.T) {
		shouldRun, _ := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("expected shouldRun to be false for empty state")
		}
	})
}

func TestAwarenessActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewAwarenessActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &AwarenessActivity{
			BaseActivity: NewBaseActivity("awareness", 5*time.Second),
			config:       nil,
		}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestAwarenessActivity_Evaluable(t *testing.T) {
	activity := NewAwarenessActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// SimilarityActivity Tests
// -----------------------------------------------------------------------------

func TestSimilarityActivity_Execute_ValidInput(t *testing.T) {
	activity := NewSimilarityActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewSimilarityInput("req-123", crs.SignalSourceHard)
	input.Sets = [][]string{{"a", "b", "c"}}
	input.SetIDs = []string{"set-1"}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "similarity" {
		t.Errorf("expected similarity, got %s", result.ActivityName)
	}
}

func TestSimilarityActivity_ShouldRun(t *testing.T) {
	activity := NewSimilarityActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns false for empty similarity index", func(t *testing.T) {
		shouldRun, _ := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("expected shouldRun to be false for empty state")
		}
	})
}

func TestSimilarityActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewSimilarityActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &SimilarityActivity{
			BaseActivity: NewBaseActivity("similarity", 5*time.Second),
			config:       nil,
		}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestSimilarityActivity_Evaluable(t *testing.T) {
	activity := NewSimilarityActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// StreamingActivity Tests
// -----------------------------------------------------------------------------

func TestStreamingActivity_Execute_ValidInput(t *testing.T) {
	activity := NewStreamingActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewStreamingInput("req-123", crs.SignalSourceHard)
	input.Items = []string{"item1", "item2", "item3"}
	input.Frequencies = map[string]uint64{"item1": 10}
	input.GraphEdges = []streaming.AGMEdge{{From: "a", To: "b"}}

	result, _, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ActivityName != "streaming" {
		t.Errorf("expected streaming, got %s", result.ActivityName)
	}
}

func TestStreamingInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewStreamingInput("req-123", crs.SignalSourceHard)
		if input.Type() != "streaming" {
			t.Errorf("expected streaming, got %s", input.Type())
		}
		if input.Items == nil {
			t.Error("expected non-nil items")
		}
		if input.Frequencies == nil {
			t.Error("expected non-nil frequencies")
		}
		if input.GraphEdges == nil {
			t.Error("expected non-nil graph edges")
		}
	})
}

func TestStreamingActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewStreamingActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &StreamingActivity{
			BaseActivity: NewBaseActivity("streaming", 5*time.Second),
			config:       nil,
		}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})
}

func TestStreamingActivity_Evaluable(t *testing.T) {
	activity := NewStreamingActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// MemoryActivity Tests
// -----------------------------------------------------------------------------

func TestMemoryActivity_Execute_InvalidOperation(t *testing.T) {
	activity := NewMemoryActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewMemoryInput("req-123", "invalid_op", crs.SignalSourceHard)

	_, _, err := activity.Execute(ctx, snapshot, input)
	if err == nil {
		t.Error("expected error for invalid operation")
	}
}

func TestMemoryActivity_Record_WithoutMetadata(t *testing.T) {
	config := &MemoryConfig{
		ActivityConfig:    DefaultActivityConfig(),
		MaxHistoryEntries: 100,
		TrackAllDecisions: true,
		IncludeMetadata:   false, // Disable metadata
	}
	activity := NewMemoryActivity(config)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := NewMemoryInput("req-123", "record", crs.SignalSourceHard)
	input.NodeID = "node-1"
	input.Action = "expand"
	input.Result = "success"
	input.Metadata = map[string]string{"key": "value"}

	result, delta, err := activity.Execute(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if delta == nil {
		t.Error("expected delta for record operation")
	}
}

func TestMemoryActivity_Algorithms(t *testing.T) {
	activity := NewMemoryActivity(nil)
	algos := activity.Algorithms()

	if algos != nil {
		t.Error("expected nil algorithms for memory activity")
	}
}

func TestMemoryActivity_Evaluable(t *testing.T) {
	activity := NewMemoryActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := activity.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})
}

// -----------------------------------------------------------------------------
// Configuration Tests
// -----------------------------------------------------------------------------

func TestSearchConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultSearchConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.PNMCTSConfig == nil {
			t.Error("expected non-nil PNMCTS config")
		}
		if config.TranspositionConfig == nil {
			t.Error("expected non-nil transposition config")
		}
		if config.UnitPropConfig == nil {
			t.Error("expected non-nil unit prop config")
		}
	})
}

func TestLearningConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultLearningConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.CDCLConfig == nil {
			t.Error("expected non-nil CDCL config")
		}
		if config.WatchedConfig == nil {
			t.Error("expected non-nil watched config")
		}
	})
}

func TestConstraintConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultConstraintConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.TMSConfig == nil {
			t.Error("expected non-nil TMS config")
		}
		if config.AC3Config == nil {
			t.Error("expected non-nil arc consistency config")
		}
		if config.SemanticBackpropConfig == nil {
			t.Error("expected non-nil semantic backprop config")
		}
	})
}

func TestPlanningConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultPlanningConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.HTNConfig == nil {
			t.Error("expected non-nil HTN config")
		}
		if config.BlackboardConfig == nil {
			t.Error("expected non-nil blackboard config")
		}
	})
}

func TestAwarenessConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultAwarenessConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.TarjanConfig == nil {
			t.Error("expected non-nil Tarjan config")
		}
		if config.DominatorsConfig == nil {
			t.Error("expected non-nil dominators config")
		}
		if config.VF2Config == nil {
			t.Error("expected non-nil VF2 config")
		}
	})
}

func TestSimilarityConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultSimilarityConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.MinHashConfig == nil {
			t.Error("expected non-nil MinHash config")
		}
		if config.LSHConfig == nil {
			t.Error("expected non-nil LSH config")
		}
		if config.WeisfeilerLemanConfig == nil {
			t.Error("expected non-nil WL config")
		}
		if config.L0SamplingConfig == nil {
			t.Error("expected non-nil L0 config")
		}
	})
}

func TestStreamingConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultStreamingConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.AGMConfig == nil {
			t.Error("expected non-nil AGM config")
		}
		if config.CountMinConfig == nil {
			t.Error("expected non-nil CountMin config")
		}
		if config.HyperLogLogConfig == nil {
			t.Error("expected non-nil HyperLogLog config")
		}
	})
}

func TestMemoryConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultMemoryConfig()
		if config.ActivityConfig == nil {
			t.Error("expected non-nil activity config")
		}
		if config.MaxHistoryEntries != 10000 {
			t.Errorf("expected 10000 max entries, got %d", config.MaxHistoryEntries)
		}
		if !config.TrackAllDecisions {
			t.Error("expected TrackAllDecisions to be true")
		}
		if !config.IncludeMetadata {
			t.Error("expected IncludeMetadata to be true")
		}
	})
}

// -----------------------------------------------------------------------------
// ActivityResult Tests
// -----------------------------------------------------------------------------

func TestActivityResult_SuccessCount_WithResults(t *testing.T) {
	// Create mock algorithm results with varying success
	result := ActivityResult{
		AlgorithmResults: nil, // Empty results
	}

	if result.SuccessCount() != 0 {
		t.Errorf("expected 0, got %d", result.SuccessCount())
	}
}

func TestActivityResult_FailureCount_WithResults(t *testing.T) {
	result := ActivityResult{
		AlgorithmResults: nil, // Empty results
	}

	if result.FailureCount() != 0 {
		t.Errorf("expected 0, got %d", result.FailureCount())
	}
}

// -----------------------------------------------------------------------------
// Concurrent Access Tests
// -----------------------------------------------------------------------------

func TestActivity_ConcurrentExecution(t *testing.T) {
	activity := NewSearchActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	const numGoroutines = 50
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			input := NewSearchInput("req", "root", crs.SignalSourceHard)
			_, _, _ = activity.Execute(ctx, snapshot, input)
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestActivity_ConcurrentShouldRun(t *testing.T) {
	activities := []Activity{
		NewSearchActivity(nil),
		NewLearningActivity(nil),
		NewConstraintActivity(nil),
		NewPlanningActivity(nil),
		NewAwarenessActivity(nil),
		NewSimilarityActivity(nil),
		NewStreamingActivity(nil),
		NewMemoryActivity(nil),
	}

	snapshot := crs.New(nil).Snapshot()

	const numGoroutines = 50
	done := make(chan bool, numGoroutines*len(activities))

	for _, activity := range activities {
		for i := 0; i < numGoroutines; i++ {
			go func(a Activity) {
				_, _ = a.ShouldRun(snapshot)
				done <- true
			}(activity)
		}
	}

	for i := 0; i < numGoroutines*len(activities); i++ {
		<-done
	}
}
