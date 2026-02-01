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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// Search Activity Tests
// -----------------------------------------------------------------------------

func TestNewSearchActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewSearchActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "search" {
			t.Errorf("expected search, got %s", activity.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &SearchConfig{
			ActivityConfig: &ActivityConfig{
				Timeout: 10 * time.Second,
			},
		}
		activity := NewSearchActivity(config)
		if activity.Timeout() != 10*time.Second {
			t.Errorf("expected 10s timeout, got %v", activity.Timeout())
		}
	})

	t.Run("has three algorithms", func(t *testing.T) {
		activity := NewSearchActivity(nil)
		if len(activity.Algorithms()) != 3 {
			t.Errorf("expected 3 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestSearchActivity_Execute(t *testing.T) {
	activity := NewSearchActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns error for nil input", func(t *testing.T) {
		_, _, err := activity.Execute(ctx, snapshot, nil)
		if err == nil {
			t.Error("expected error for nil input")
		}
	})

	t.Run("returns error for wrong input type", func(t *testing.T) {
		_, _, err := activity.Execute(ctx, snapshot, &MemoryInput{})
		if err == nil {
			t.Error("expected error for wrong input type")
		}
	})
}

func TestSearchActivity_ShouldRun(t *testing.T) {
	activity := NewSearchActivity(nil)
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

func TestSearchActivity_Evaluable(t *testing.T) {
	activity := NewSearchActivity(nil)

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

	t.Run("health check passes", func(t *testing.T) {
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Learning Activity Tests
// -----------------------------------------------------------------------------

func TestNewLearningActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewLearningActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "learning" {
			t.Errorf("expected learning, got %s", activity.Name())
		}
	})

	t.Run("has two algorithms", func(t *testing.T) {
		activity := NewLearningActivity(nil)
		if len(activity.Algorithms()) != 2 {
			t.Errorf("expected 2 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestLearningActivity_Execute(t *testing.T) {
	activity := NewLearningActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns error for nil input", func(t *testing.T) {
		_, _, err := activity.Execute(ctx, snapshot, nil)
		if err == nil {
			t.Error("expected error for nil input")
		}
	})
}

func TestLearningActivity_ShouldRun(t *testing.T) {
	activity := NewLearningActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns false for no disproven nodes", func(t *testing.T) {
		shouldRun, _ := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("expected shouldRun to be false")
		}
	})
}

// -----------------------------------------------------------------------------
// Constraint Activity Tests
// -----------------------------------------------------------------------------

func TestNewConstraintActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewConstraintActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "constraint" {
			t.Errorf("expected constraint, got %s", activity.Name())
		}
	})

	t.Run("has three algorithms", func(t *testing.T) {
		activity := NewConstraintActivity(nil)
		if len(activity.Algorithms()) != 3 {
			t.Errorf("expected 3 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestConstraintActivity_Evaluable(t *testing.T) {
	activity := NewConstraintActivity(nil)

	t.Run("has properties", func(t *testing.T) {
		props := activity.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("health check passes", func(t *testing.T) {
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Planning Activity Tests
// -----------------------------------------------------------------------------

func TestNewPlanningActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewPlanningActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "planning" {
			t.Errorf("expected planning, got %s", activity.Name())
		}
	})

	t.Run("has two algorithms", func(t *testing.T) {
		activity := NewPlanningActivity(nil)
		if len(activity.Algorithms()) != 2 {
			t.Errorf("expected 2 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestPlanningActivity_ShouldRun(t *testing.T) {
	activity := NewPlanningActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns false for empty history", func(t *testing.T) {
		shouldRun, _ := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("expected shouldRun to be false")
		}
	})
}

// -----------------------------------------------------------------------------
// Awareness Activity Tests
// -----------------------------------------------------------------------------

func TestNewAwarenessActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewAwarenessActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "awareness" {
			t.Errorf("expected awareness, got %s", activity.Name())
		}
	})

	t.Run("has three algorithms", func(t *testing.T) {
		activity := NewAwarenessActivity(nil)
		if len(activity.Algorithms()) != 3 {
			t.Errorf("expected 3 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestAwarenessInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewAwarenessInput("req-123", crs.SignalSourceHard)
		if input.Type() != "awareness" {
			t.Errorf("expected awareness, got %s", input.Type())
		}
	})
}

// -----------------------------------------------------------------------------
// Similarity Activity Tests
// -----------------------------------------------------------------------------

func TestNewSimilarityActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewSimilarityActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "similarity" {
			t.Errorf("expected similarity, got %s", activity.Name())
		}
	})

	t.Run("has four algorithms", func(t *testing.T) {
		activity := NewSimilarityActivity(nil)
		if len(activity.Algorithms()) != 4 {
			t.Errorf("expected 4 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestSimilarityInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewSimilarityInput("req-123", crs.SignalSourceHard)
		if input.Type() != "similarity" {
			t.Errorf("expected similarity, got %s", input.Type())
		}
	})
}

// -----------------------------------------------------------------------------
// Streaming Activity Tests
// -----------------------------------------------------------------------------

func TestNewStreamingActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewStreamingActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "streaming" {
			t.Errorf("expected streaming, got %s", activity.Name())
		}
	})

	t.Run("has three algorithms", func(t *testing.T) {
		activity := NewStreamingActivity(nil)
		if len(activity.Algorithms()) != 3 {
			t.Errorf("expected 3 algorithms, got %d", len(activity.Algorithms()))
		}
	})
}

func TestStreamingActivity_ShouldRun(t *testing.T) {
	activity := NewStreamingActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("streaming always wants to run", func(t *testing.T) {
		shouldRun, priority := activity.ShouldRun(snapshot)
		if !shouldRun {
			t.Error("streaming should always want to run")
		}
		if priority != PriorityLow {
			t.Errorf("expected low priority, got %v", priority)
		}
	})
}

// -----------------------------------------------------------------------------
// Memory Activity Tests
// -----------------------------------------------------------------------------

func TestNewMemoryActivity(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		activity := NewMemoryActivity(nil)
		if activity == nil {
			t.Fatal("expected non-nil activity")
		}
		if activity.Name() != "memory" {
			t.Errorf("expected memory, got %s", activity.Name())
		}
	})
}

func TestMemoryActivity_Execute(t *testing.T) {
	activity := NewMemoryActivity(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("record creates history delta", func(t *testing.T) {
		input := NewMemoryInput("req-123", "record", crs.SignalSourceHard)
		input.NodeID = "node-1"
		input.Action = "expand"
		input.Result = "success"

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
	})

	t.Run("query does not create delta", func(t *testing.T) {
		input := NewMemoryInput("req-123", "query", crs.SignalSourceHard)
		input.QueryCount = 5

		result, delta, err := activity.Execute(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
		if delta != nil {
			t.Error("expected no delta for query operation")
		}
	})

	t.Run("replay does not create delta", func(t *testing.T) {
		input := NewMemoryInput("req-123", "replay", crs.SignalSourceHard)
		input.NodeID = "node-1"

		result, delta, err := activity.Execute(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
		if delta != nil {
			t.Error("expected no delta for replay operation")
		}
	})
}

func TestMemoryActivity_ShouldRun(t *testing.T) {
	activity := NewMemoryActivity(nil)
	snapshot := crs.New(nil).Snapshot()

	t.Run("memory is on-demand only", func(t *testing.T) {
		shouldRun, _ := activity.ShouldRun(snapshot)
		if shouldRun {
			t.Error("memory should not run automatically")
		}
	})
}

func TestMemoryActivity_HealthCheck(t *testing.T) {
	t.Run("passes with valid config", func(t *testing.T) {
		activity := NewMemoryActivity(nil)
		err := activity.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("fails with nil config", func(t *testing.T) {
		activity := &MemoryActivity{name: "memory", config: nil}
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for nil config")
		}
	})

	t.Run("fails with invalid max entries", func(t *testing.T) {
		activity := NewMemoryActivity(&MemoryConfig{
			ActivityConfig:    DefaultActivityConfig(),
			MaxHistoryEntries: 0,
		})
		err := activity.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for zero max entries")
		}
	})
}

func TestMemoryInput(t *testing.T) {
	t.Run("creates with correct type", func(t *testing.T) {
		input := NewMemoryInput("req-123", "record", crs.SignalSourceHard)
		if input.Type() != "memory" {
			t.Errorf("expected memory, got %s", input.Type())
		}
		if input.Operation != "record" {
			t.Errorf("expected record, got %s", input.Operation)
		}
	})
}
