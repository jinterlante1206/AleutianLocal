// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package constraints

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

func TestNewSemanticBackprop(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewSemanticBackprop(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "semantic_backprop" {
			t.Errorf("expected name semantic_backprop, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &SemanticBackpropConfig{
			MaxDepth:    5,
			DecayFactor: 0.5,
			Timeout:     5 * time.Second,
		}
		algo := NewSemanticBackprop(config)
		if algo.Timeout() != 5*time.Second {
			t.Errorf("expected timeout 5s, got %v", algo.Timeout())
		}
	})
}

func TestSemanticBackprop_Process(t *testing.T) {
	algo := NewSemanticBackprop(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("attributes errors to dependencies", func(t *testing.T) {
		// Error at C, dependencies: C -> B -> A
		input := &SemanticBackpropInput{
			ErrorNodes: []ErrorNode{
				{
					NodeID:    "C",
					ErrorType: "test_failure",
					Severity:  1.0,
					Source:    crs.SignalSourceHard,
				},
			},
			Dependencies: map[string][]string{
				"C": {"B"},
				"B": {"A"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*SemanticBackpropOutput)
		if !ok {
			t.Fatal("expected *SemanticBackpropOutput")
		}

		// All nodes should have attribution
		if _, ok := output.Attributions["C"]; !ok {
			t.Error("expected C to have attribution")
		}
		if _, ok := output.Attributions["B"]; !ok {
			t.Error("expected B to have attribution")
		}
		if _, ok := output.Attributions["A"]; !ok {
			t.Error("expected A to have attribution")
		}

		// C should have highest attribution (error source)
		if output.Attributions["C"] <= output.Attributions["B"] {
			t.Error("expected C to have higher attribution than B")
		}
		if output.Attributions["B"] <= output.Attributions["A"] {
			t.Error("expected B to have higher attribution than A (decay)")
		}
	})

	t.Run("handles multiple error sources", func(t *testing.T) {
		input := &SemanticBackpropInput{
			ErrorNodes: []ErrorNode{
				{NodeID: "E1", Severity: 1.0, Source: crs.SignalSourceHard},
				{NodeID: "E2", Severity: 0.5, Source: crs.SignalSourceHard},
			},
			Dependencies: map[string][]string{
				"E1": {"shared"},
				"E2": {"shared"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*SemanticBackpropOutput)

		// Shared dependency should have attribution from both errors
		sharedAttrib, ok := output.Attributions["shared"]
		if !ok {
			t.Error("expected shared node to have attribution")
		}

		// Attribution should be greater than from a single source
		if sharedAttrib <= 0 {
			t.Error("expected positive attribution on shared node")
		}
	})

	t.Run("respects depth limit", func(t *testing.T) {
		// Create a chain longer than default max depth (10)
		deps := make(map[string][]string)
		for i := 0; i < 20; i++ {
			deps[string(rune('A'+i))] = []string{string(rune('A' + i + 1))}
		}

		input := &SemanticBackpropInput{
			ErrorNodes: []ErrorNode{
				{NodeID: "A", Severity: 1.0, Source: crs.SignalSourceHard},
			},
			Dependencies: deps,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*SemanticBackpropOutput)

		// Should not propagate beyond max depth
		if output.MaxDepthReached > 10 {
			t.Errorf("expected max depth <= 10, got %d", output.MaxDepthReached)
		}
	})

	t.Run("returns top causes", func(t *testing.T) {
		input := &SemanticBackpropInput{
			ErrorNodes: []ErrorNode{
				{NodeID: "error", Severity: 1.0, Source: crs.SignalSourceHard},
			},
			Dependencies: map[string][]string{
				"error": {"cause1", "cause2", "cause3"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*SemanticBackpropOutput)

		if len(output.TopCauses) == 0 {
			t.Error("expected top causes")
		}

		// First cause should have highest attribution
		if len(output.TopCauses) >= 2 {
			if output.TopCauses[0].Attribution < output.TopCauses[1].Attribution {
				t.Error("top causes should be sorted by attribution (descending)")
			}
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

		input := &SemanticBackpropInput{
			ErrorNodes:   []ErrorNode{},
			Dependencies: map[string][]string{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("handles empty input", func(t *testing.T) {
		input := &SemanticBackpropInput{
			ErrorNodes:   []ErrorNode{},
			Dependencies: map[string][]string{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*SemanticBackpropOutput)
		if len(output.Attributions) != 0 {
			t.Error("expected no attributions with empty input")
		}
	})
}

func TestSemanticBackprop_Properties(t *testing.T) {
	algo := NewSemanticBackprop(nil)

	t.Run("attribution_positive property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "attribution_positive" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("attribution_positive property not found")
		}

		// Should pass for positive attributions
		output := &SemanticBackpropOutput{
			Attributions: map[string]float64{
				"A": 1.0,
				"B": 0.5,
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail for negative attributions
		outputNeg := &SemanticBackpropOutput{
			Attributions: map[string]float64{
				"A": -0.5, // Negative!
			},
		}
		if err := prop(nil, outputNeg); err == nil {
			t.Error("expected error for negative attribution")
		}
	})
}

func TestSemanticBackprop_Evaluable(t *testing.T) {
	algo := NewSemanticBackprop(nil)

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
		algo := &SemanticBackprop{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid decay factor", func(t *testing.T) {
		algo := NewSemanticBackprop(&SemanticBackpropConfig{
			DecayFactor: 1.5, // Invalid: must be 0-1
		})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with invalid decay factor")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewSemanticBackprop(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
