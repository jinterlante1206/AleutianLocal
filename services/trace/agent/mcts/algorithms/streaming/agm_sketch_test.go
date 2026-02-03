// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewAGMSketch(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewAGMSketch(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "agm_sketch" {
			t.Errorf("expected name agm_sketch, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &AGMConfig{
			NumLevels: 10,
			Width:     512,
			Timeout:   3 * time.Second,
		}
		algo := NewAGMSketch(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestAGMSketch_Process(t *testing.T) {
	algo := NewAGMSketch(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("add single edge", func(t *testing.T) {
		input := &AGMInput{
			Operation: "add",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.EdgesProcessed != 1 {
			t.Errorf("expected 1 edge processed, got %d", output.EdgesProcessed)
		}
		if output.Sketch == nil {
			t.Fatal("expected sketch")
		}
	})

	t.Run("add multiple edges", func(t *testing.T) {
		input := &AGMInput{
			Operation: "add",
			Edges: []AGMEdge{
				{From: "A", To: "B"},
				{From: "B", To: "C"},
				{From: "A", To: "C"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.EdgesProcessed != 3 {
			t.Errorf("expected 3 edges processed, got %d", output.EdgesProcessed)
		}
	})

	t.Run("delete edges", func(t *testing.T) {
		// First add
		addInput := &AGMInput{
			Operation: "add",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
		}
		addResult, _, _ := algo.Process(ctx, snapshot, addInput)
		sketch := addResult.(*AGMOutput).Sketch

		// Then delete
		deleteInput := &AGMInput{
			Operation: "delete",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
			Sketch:    sketch,
		}

		result, _, err := algo.Process(ctx, snapshot, deleteInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.EdgesProcessed != 1 {
			t.Errorf("expected 1 edge processed, got %d", output.EdgesProcessed)
		}
	})

	t.Run("delete requires sketch", func(t *testing.T) {
		input := &AGMInput{
			Operation: "delete",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
			Sketch:    nil,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for delete without sketch")
		}
	})

	t.Run("query connectivity", func(t *testing.T) {
		// Create a simple graph: A-B-C (linear)
		input := &AGMInput{
			Operation: "add",
			Edges: []AGMEdge{
				{From: "A", To: "B"},
				{From: "B", To: "C"},
			},
		}
		addResult, _, _ := algo.Process(ctx, snapshot, input)
		sketch := addResult.(*AGMOutput).Sketch

		queryInput := &AGMInput{
			Operation: "query",
			Sketch:    sketch,
		}

		result, _, err := algo.Process(ctx, snapshot, queryInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.Components < 0 {
			t.Errorf("expected non-negative components, got %d", output.Components)
		}
	})

	t.Run("query empty sketch", func(t *testing.T) {
		input := &AGMInput{
			Operation: "query",
			Sketch:    nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.Components != 0 {
			t.Errorf("expected 0 components, got %d", output.Components)
		}
	})

	t.Run("merge sketches", func(t *testing.T) {
		// Create first sketch
		input1 := &AGMInput{
			Operation: "add",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
		}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sketch1 := result1.(*AGMOutput).Sketch

		// Create second sketch
		input2 := &AGMInput{
			Operation: "add",
			Edges:     []AGMEdge{{From: "C", To: "D"}},
		}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		sketch2 := result2.(*AGMOutput).Sketch

		// Merge
		mergeInput := &AGMInput{
			Operation:   "merge",
			Sketch:      sketch1,
			OtherSketch: sketch2,
		}

		result, _, err := algo.Process(ctx, snapshot, mergeInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.Sketch == nil {
			t.Fatal("expected merged sketch")
		}
	})

	t.Run("merge with nil sketches", func(t *testing.T) {
		input := &AGMInput{
			Operation:   "merge",
			Sketch:      nil,
			OtherSketch: nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AGMOutput)
		if output.Sketch == nil {
			t.Fatal("expected new sketch")
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
		cancel()

		input := &AGMInput{
			Operation: "add",
			Edges:     []AGMEdge{{From: "A", To: "B"}},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})
}

func TestAGMSketch_Properties(t *testing.T) {
	algo := NewAGMSketch(nil)

	t.Run("components_non_negative property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "components_non_negative" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("components_non_negative property not found")
		}

		// Valid
		output := &AGMOutput{Components: 5}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid
		outputBad := &AGMOutput{Components: -1}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for negative components")
		}
	})

	t.Run("sketch_levels_valid property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "sketch_levels_valid" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("sketch_levels_valid property not found")
		}

		// Valid
		levels := make([][]int64, 3)
		for i := range levels {
			levels[i] = make([]int64, 10)
		}
		output := &AGMOutput{
			Sketch: &AGMState{
				Levels:    levels,
				NumLevels: 3,
				Width:     10,
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: wrong number of levels
		outputBad := &AGMOutput{
			Sketch: &AGMState{
				Levels:    make([][]int64, 1),
				NumLevels: 3,
				Width:     10,
			},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for wrong level count")
		}
	})
}

func TestAGMSketch_Evaluable(t *testing.T) {
	algo := NewAGMSketch(nil)

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
		algo := &AGMSketch{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid levels", func(t *testing.T) {
		algo := NewAGMSketch(&AGMConfig{NumLevels: 0, Width: 100})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero levels")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
