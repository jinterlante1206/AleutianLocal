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

func TestNewHTN(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewHTN(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "htn" {
			t.Errorf("expected name htn, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &HTNConfig{
			MaxDepth:      10,
			MaxPlanLength: 50,
			Timeout:       3 * time.Second,
		}
		algo := NewHTN(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestHTN_Process(t *testing.T) {
	algo := NewHTN(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("decomposes compound task to primitives", func(t *testing.T) {
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "goal1", Name: "build_and_test", IsPrimitive: false},
			},
			Methods: []HTNMethod{
				{
					ID:            "m1",
					TaskName:      "build_and_test",
					Preconditions: []HTNPrecondition{},
					Subtasks: []HTNTask{
						{ID: "sub1", Name: "compile", IsPrimitive: true},
						{ID: "sub2", Name: "run_tests", IsPrimitive: true},
					},
					Priority: 1,
				},
			},
			InitialState: map[string]bool{},
			Source:       crs.SignalSourceHard,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*HTNOutput)
		if !ok {
			t.Fatal("expected *HTNOutput")
		}

		if !output.Success {
			t.Errorf("expected success, got failure: %s", output.FailureReason)
		}

		if len(output.Plan) != 2 {
			t.Errorf("expected 2 primitives in plan, got %d", len(output.Plan))
		}

		// Verify order
		if len(output.Plan) >= 2 {
			if output.Plan[0].Name != "compile" {
				t.Errorf("expected first task to be 'compile', got %s", output.Plan[0].Name)
			}
			if output.Plan[1].Name != "run_tests" {
				t.Errorf("expected second task to be 'run_tests', got %s", output.Plan[1].Name)
			}
		}
	})

	t.Run("handles nested decomposition", func(t *testing.T) {
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "goal1", Name: "deploy", IsPrimitive: false},
			},
			Methods: []HTNMethod{
				{
					ID:       "m1",
					TaskName: "deploy",
					Subtasks: []HTNTask{
						{ID: "sub1", Name: "build", IsPrimitive: false},
						{ID: "sub2", Name: "upload", IsPrimitive: true},
					},
				},
				{
					ID:       "m2",
					TaskName: "build",
					Subtasks: []HTNTask{
						{ID: "sub3", Name: "compile", IsPrimitive: true},
						{ID: "sub4", Name: "package", IsPrimitive: true},
					},
				},
			},
			InitialState: map[string]bool{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HTNOutput)

		if !output.Success {
			t.Errorf("expected success, got failure: %s", output.FailureReason)
		}

		// Plan should have: compile, package, upload (in order)
		if len(output.Plan) != 3 {
			t.Errorf("expected 3 primitives, got %d", len(output.Plan))
		}

		if output.DepthReached < 1 {
			t.Errorf("expected depth >= 1, got %d", output.DepthReached)
		}
	})

	t.Run("respects preconditions", func(t *testing.T) {
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "goal1", Name: "task", IsPrimitive: false},
			},
			Methods: []HTNMethod{
				{
					ID:       "m1",
					TaskName: "task",
					Preconditions: []HTNPrecondition{
						{Predicate: "ready", Value: true},
					},
					Subtasks: []HTNTask{
						{ID: "sub1", Name: "action1", IsPrimitive: true},
					},
					Priority: 2,
				},
				{
					ID:       "m2",
					TaskName: "task",
					Preconditions: []HTNPrecondition{
						{Predicate: "fallback", Value: true},
					},
					Subtasks: []HTNTask{
						{ID: "sub2", Name: "action2", IsPrimitive: true},
					},
					Priority: 1,
				},
			},
			InitialState: map[string]bool{
				"fallback": true, // Only fallback condition is true
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HTNOutput)

		if !output.Success {
			t.Errorf("expected success, got failure: %s", output.FailureReason)
		}

		// Should use m2 (fallback) since m1's precondition is not met
		if len(output.Plan) != 1 || output.Plan[0].Name != "action2" {
			t.Errorf("expected action2 from fallback method")
		}
	})

	t.Run("fails when no applicable method", func(t *testing.T) {
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "goal1", Name: "task", IsPrimitive: false},
			},
			Methods: []HTNMethod{
				{
					ID:       "m1",
					TaskName: "task",
					Preconditions: []HTNPrecondition{
						{Predicate: "impossible", Value: true},
					},
					Subtasks: []HTNTask{
						{ID: "sub1", Name: "action", IsPrimitive: true},
					},
				},
			},
			InitialState: map[string]bool{
				"impossible": false,
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HTNOutput)

		if output.Success {
			t.Error("expected failure when no applicable method")
		}

		if output.FailureReason == "" {
			t.Error("expected failure reason")
		}
	})

	t.Run("handles primitive-only input", func(t *testing.T) {
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "p1", Name: "action1", IsPrimitive: true},
				{ID: "p2", Name: "action2", IsPrimitive: true},
			},
			Methods:      []HTNMethod{},
			InitialState: map[string]bool{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HTNOutput)

		if !output.Success {
			t.Errorf("expected success for primitive-only input")
		}

		if len(output.Plan) != 2 {
			t.Errorf("expected 2 primitives, got %d", len(output.Plan))
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

		input := &HTNInput{
			Tasks:        []HTNTask{},
			Methods:      []HTNMethod{},
			InitialState: map[string]bool{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("respects max depth limit", func(t *testing.T) {
		algo := NewHTN(&HTNConfig{
			MaxDepth:      2,
			MaxPlanLength: 100,
			Timeout:       5 * time.Second,
		})

		// Create a chain that would exceed depth 2
		input := &HTNInput{
			Tasks: []HTNTask{
				{ID: "t1", Name: "level0", IsPrimitive: false},
			},
			Methods: []HTNMethod{
				{
					ID:       "m1",
					TaskName: "level0",
					Subtasks: []HTNTask{
						{ID: "t2", Name: "level1", IsPrimitive: false},
					},
				},
				{
					ID:       "m2",
					TaskName: "level1",
					Subtasks: []HTNTask{
						{ID: "t3", Name: "level2", IsPrimitive: false},
					},
				},
				{
					ID:       "m3",
					TaskName: "level2",
					Subtasks: []HTNTask{
						{ID: "t4", Name: "level3", IsPrimitive: false}, // Would be depth 3
					},
				},
			},
			InitialState: map[string]bool{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HTNOutput)

		if output.Success {
			t.Error("expected failure due to depth limit")
		}

		if output.FailureReason != "max depth exceeded" {
			t.Errorf("expected 'max depth exceeded', got %s", output.FailureReason)
		}
	})
}

func TestHTN_MethodPriority(t *testing.T) {
	algo := NewHTN(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := &HTNInput{
		Tasks: []HTNTask{
			{ID: "goal1", Name: "task", IsPrimitive: false},
		},
		Methods: []HTNMethod{
			{
				ID:       "low_priority",
				TaskName: "task",
				Subtasks: []HTNTask{
					{ID: "sub1", Name: "low_action", IsPrimitive: true},
				},
				Priority: 1,
			},
			{
				ID:       "high_priority",
				TaskName: "task",
				Subtasks: []HTNTask{
					{ID: "sub2", Name: "high_action", IsPrimitive: true},
				},
				Priority: 10,
			},
		},
		InitialState: map[string]bool{},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*HTNOutput)

	if !output.Success {
		t.Errorf("expected success, got failure: %s", output.FailureReason)
	}

	// Should prefer high priority method
	if len(output.Plan) != 1 || output.Plan[0].Name != "high_action" {
		t.Errorf("expected high_action from high priority method")
	}
}

func TestHTN_Properties(t *testing.T) {
	algo := NewHTN(nil)

	t.Run("plan_only_primitives property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "plan_only_primitives" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("plan_only_primitives property not found")
		}

		// Should pass for primitive-only plan
		output := &HTNOutput{
			Success: true,
			Plan: []HTNTask{
				{ID: "p1", IsPrimitive: true},
				{ID: "p2", IsPrimitive: true},
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail for non-primitive in plan
		outputBad := &HTNOutput{
			Success: true,
			Plan: []HTNTask{
				{ID: "p1", IsPrimitive: false}, // Not primitive!
			},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for non-primitive in plan")
		}
	})
}

func TestHTN_Evaluable(t *testing.T) {
	algo := NewHTN(nil)

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
		algo := &HTN{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max depth", func(t *testing.T) {
		algo := NewHTN(&HTNConfig{MaxDepth: 0})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero max depth")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewHTN(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
