// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package planning

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewBlackboard(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewBlackboard(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "blackboard" {
			t.Errorf("expected name blackboard, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &BlackboardConfig{
			MaxIterations:    100,
			MaxContributions: 200,
			Timeout:          10 * time.Second,
		}
		algo := NewBlackboard(config)
		if algo.Timeout() != 10*time.Second {
			t.Errorf("expected timeout 10s, got %v", algo.Timeout())
		}
	})
}

func TestBlackboard_Process(t *testing.T) {
	algo := NewBlackboard(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("activates knowledge source on trigger", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"raw.input": {
					Level:      "raw",
					Key:        "input",
					Value:      "test data",
					Confidence: 1.0,
				},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID:   "analyzer",
					Name: "Data Analyzer",
					Triggers: []BlackboardCondition{
						{Level: "raw", Key: "input", Operator: "exists", MinConfidence: 0.5},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "analyzed", Key: "result", ValueTemplate: "analyzed", Confidence: 0.9},
					},
					Priority: 1,
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "analyzed", Key: "result", Operator: "exists", MinConfidence: 0.5},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*BlackboardOutput)
		if !ok {
			t.Fatal("expected *BlackboardOutput")
		}

		if !output.GoalReached {
			t.Errorf("expected goal to be reached, stopped: %s", output.StoppedReason)
		}

		if len(output.Contributions) != 1 {
			t.Errorf("expected 1 contribution, got %d", len(output.Contributions))
		}

		// Verify contribution
		if output.Contributions[0].SourceID != "analyzer" {
			t.Errorf("expected contribution from analyzer")
		}

		// Verify final state
		if _, ok := output.FinalState["analyzed.result"]; !ok {
			t.Error("expected analyzed.result in final state")
		}
	})

	t.Run("respects priority ordering", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"raw.data": {Level: "raw", Key: "data", Value: "x", Confidence: 1.0},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID:   "low_priority",
					Name: "Low Priority",
					Triggers: []BlackboardCondition{
						{Level: "raw", Key: "data", Operator: "exists"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "low", ValueTemplate: "low", Confidence: 1.0},
					},
					Priority: 1,
				},
				{
					ID:   "high_priority",
					Name: "High Priority",
					Triggers: []BlackboardCondition{
						{Level: "raw", Key: "data", Operator: "exists"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "high", ValueTemplate: "high", Confidence: 1.0},
					},
					Priority: 10,
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "result", Key: "high", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)

		// First contribution should be from high priority source
		if len(output.Contributions) == 0 {
			t.Fatal("expected at least one contribution")
		}

		if output.Contributions[0].SourceID != "high_priority" {
			t.Errorf("expected first contribution from high_priority, got %s", output.Contributions[0].SourceID)
		}
	})

	t.Run("stops when no triggered sources", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{},
			KnowledgeSources: []KnowledgeSource{
				{
					ID:   "ks1",
					Name: "Source 1",
					Triggers: []BlackboardCondition{
						{Level: "nonexistent", Key: "data", Operator: "exists"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "out", ValueTemplate: "x", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "result", Key: "out", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)

		if output.GoalReached {
			t.Error("expected goal not reached")
		}

		if output.StoppedReason != "no triggered sources" {
			t.Errorf("expected 'no triggered sources', got %s", output.StoppedReason)
		}
	})

	t.Run("handles multiple knowledge sources in chain", func(t *testing.T) {
		// Use not_exists conditions to ensure each stage only runs once
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"raw.input": {Level: "raw", Key: "input", Value: "data", Confidence: 1.0},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID:   "stage1",
					Name: "Stage 1",
					Triggers: []BlackboardCondition{
						{Level: "raw", Key: "input", Operator: "exists"},
						{Level: "stage1", Key: "output", Operator: "not_exists"}, // Only run if not already done
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "stage1", Key: "output", ValueTemplate: "processed1", Confidence: 1.0},
					},
					Priority: 2, // Higher priority so stage1 runs first
				},
				{
					ID:   "stage2",
					Name: "Stage 2",
					Triggers: []BlackboardCondition{
						{Level: "stage1", Key: "output", Operator: "exists"},
						{Level: "stage2", Key: "output", Operator: "not_exists"}, // Only run if not already done
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "stage2", Key: "output", ValueTemplate: "processed2", Confidence: 1.0},
					},
					Priority: 1,
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "stage2", Key: "output", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)

		if !output.GoalReached {
			t.Errorf("expected goal reached, stopped: %s", output.StoppedReason)
		}

		// Should have 2 contributions (one from each stage)
		if len(output.Contributions) != 2 {
			t.Errorf("expected 2 contributions, got %d", len(output.Contributions))
		}

		// Verify both stages contributed
		if output.ActivationsPerSource["stage1"] != 1 {
			t.Error("expected stage1 to activate once")
		}
		if output.ActivationsPerSource["stage2"] != 1 {
			t.Error("expected stage2 to activate once")
		}
	})

	t.Run("returns error for invalid input type", func(t *testing.T) {
		_, _, err := algo.Process(ctx, snapshot, "invalid")
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})

	t.Run("handles cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		input := &BlackboardInput{
			InitialData:      map[string]BlackboardEntry{},
			KnowledgeSources: []KnowledgeSource{},
			GoalConditions:   []BlackboardCondition{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("respects max iterations limit", func(t *testing.T) {
		algo := NewBlackboard(&BlackboardConfig{
			MaxIterations:    3,
			MaxContributions: 100,
			Timeout:          5 * time.Second,
		})

		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"trigger.data": {Level: "trigger", Key: "data", Value: "x", Confidence: 1.0},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID:   "looper",
					Name: "Looper",
					Triggers: []BlackboardCondition{
						{Level: "trigger", Key: "data", Operator: "exists"},
					},
					Actions: []BlackboardAction{
						{Type: "update", Level: "counter", Key: "count", ValueTemplate: "incremented", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "never", Key: "reached", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)

		if output.StoppedReason != "max iterations reached" {
			t.Errorf("expected 'max iterations reached', got %s", output.StoppedReason)
		}

		if output.Iterations != 3 {
			t.Errorf("expected 3 iterations, got %d", output.Iterations)
		}
	})
}

func TestBlackboard_ConditionOperators(t *testing.T) {
	algo := NewBlackboard(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("equals operator", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"status.value": {Level: "status", Key: "value", Value: "ready", Confidence: 1.0},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID: "checker",
					Triggers: []BlackboardCondition{
						{Level: "status", Key: "value", Operator: "equals", Value: "ready"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "matched", ValueTemplate: "yes", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "result", Key: "matched", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)
		if !output.GoalReached {
			t.Errorf("expected goal reached with equals operator")
		}
	})

	t.Run("contains operator", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"message.text": {Level: "message", Key: "text", Value: "hello world", Confidence: 1.0},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID: "finder",
					Triggers: []BlackboardCondition{
						{Level: "message", Key: "text", Operator: "contains", Value: "world"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "found", ValueTemplate: "yes", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "result", Key: "found", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)
		if !output.GoalReached {
			t.Errorf("expected goal reached with contains operator")
		}
	})

	t.Run("not_exists operator", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{}, // Empty
			KnowledgeSources: []KnowledgeSource{
				{
					ID: "initializer",
					Triggers: []BlackboardCondition{
						{Level: "initialized", Key: "flag", Operator: "not_exists"},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "initialized", Key: "flag", ValueTemplate: "true", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "initialized", Key: "flag", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)
		if !output.GoalReached {
			t.Errorf("expected goal reached with not_exists trigger")
		}
	})

	t.Run("min confidence filter", func(t *testing.T) {
		input := &BlackboardInput{
			InitialData: map[string]BlackboardEntry{
				"data.low_conf": {Level: "data", Key: "low_conf", Value: "x", Confidence: 0.3},
			},
			KnowledgeSources: []KnowledgeSource{
				{
					ID: "high_conf_only",
					Triggers: []BlackboardCondition{
						{Level: "data", Key: "low_conf", Operator: "exists", MinConfidence: 0.8},
					},
					Actions: []BlackboardAction{
						{Type: "add", Level: "result", Key: "processed", ValueTemplate: "x", Confidence: 1.0},
					},
				},
			},
			GoalConditions: []BlackboardCondition{
				{Level: "result", Key: "processed", Operator: "exists"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*BlackboardOutput)

		// Should NOT trigger because confidence too low
		if output.GoalReached {
			t.Error("expected goal not reached due to low confidence")
		}
	})
}

func TestBlackboard_Properties(t *testing.T) {
	algo := NewBlackboard(nil)

	t.Run("contributions_from_valid_sources property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "contributions_from_valid_sources" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("contributions_from_valid_sources property not found")
		}

		// Should pass for valid contributions
		input := &BlackboardInput{
			KnowledgeSources: []KnowledgeSource{
				{ID: "ks1"},
				{ID: "ks2"},
			},
		}
		output := &BlackboardOutput{
			Contributions: []BlackboardContribution{
				{SourceID: "ks1"},
				{SourceID: "ks2"},
			},
		}
		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail for unknown source
		outputBad := &BlackboardOutput{
			Contributions: []BlackboardContribution{
				{SourceID: "unknown_source"},
			},
		}
		if err := prop(input, outputBad); err == nil {
			t.Error("expected error for contribution from unknown source")
		}
	})

	t.Run("confidence_in_range property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "confidence_in_range" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("confidence_in_range property not found")
		}

		// Should pass for valid confidence
		output := &BlackboardOutput{
			FinalState: map[string]BlackboardEntry{
				"a": {Confidence: 0.5},
				"b": {Confidence: 1.0},
				"c": {Confidence: 0.0},
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail for invalid confidence
		outputBad := &BlackboardOutput{
			FinalState: map[string]BlackboardEntry{
				"a": {Confidence: 1.5}, // Invalid!
			},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for confidence > 1")
		}
	})
}

func TestBlackboard_Evaluable(t *testing.T) {
	algo := NewBlackboard(nil)

	t.Run("has properties", func(t *testing.T) {
		props := algo.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}
	})

	t.Run("has metrics", func(t *testing.T) {
		metrics := algo.Metrics()
		if len(metrics) == 0 {
			t.Error("expected metrics")
		}
	})

	t.Run("health check passes", func(t *testing.T) {
		err := algo.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("health check failed: %v", err)
		}
	})

	t.Run("health check fails with nil config", func(t *testing.T) {
		algo := &Blackboard{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max iterations", func(t *testing.T) {
		algo := NewBlackboard(&BlackboardConfig{MaxIterations: 0})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero max iterations")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewBlackboard(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
