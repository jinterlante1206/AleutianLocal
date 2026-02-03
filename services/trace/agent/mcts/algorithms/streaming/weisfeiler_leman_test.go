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

func TestNewWeisfeilerLeman(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewWeisfeilerLeman(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "weisfeiler_leman" {
			t.Errorf("expected name weisfeiler_leman, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &WLConfig{
			MaxIterations: 5,
			EarlyStop:     false,
			Timeout:       3 * time.Second,
		}
		algo := NewWeisfeilerLeman(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestWeisfeilerLeman_Process(t *testing.T) {
	algo := NewWeisfeilerLeman(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("color single node graph", func(t *testing.T) {
		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A"},
				Edges: map[string][]string{"A": {}},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if len(output.Colors) != 1 {
			t.Errorf("expected 1 color, got %d", len(output.Colors))
		}
		if output.ColorClasses != 1 {
			t.Errorf("expected 1 color class, got %d", output.ColorClasses)
		}
	})

	t.Run("color linear graph", func(t *testing.T) {
		// A -- B -- C
		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{
					"A": {"B"},
					"B": {"A", "C"},
					"C": {"B"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if len(output.Colors) != 3 {
			t.Errorf("expected 3 colors, got %d", len(output.Colors))
		}
		// A and C should have same color (symmetric), B is different
		if output.Colors["A"] != output.Colors["C"] {
			t.Error("expected A and C to have same color")
		}
		if output.Colors["A"] == output.Colors["B"] {
			t.Error("expected A and B to have different colors")
		}
	})

	t.Run("color triangle", func(t *testing.T) {
		// A -- B
		//  \  /
		//   C
		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{
					"A": {"B", "C"},
					"B": {"A", "C"},
					"C": {"A", "B"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		// All nodes should have same color (symmetric)
		if output.Colors["A"] != output.Colors["B"] || output.Colors["B"] != output.Colors["C"] {
			t.Error("expected all nodes to have same color in symmetric graph")
		}
	})

	t.Run("color with labels", func(t *testing.T) {
		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{
					"A": {"B"},
					"B": {"A"},
				},
				NodeLabels: map[string]string{
					"A": "type1",
					"B": "type2",
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		// Different labels should result in different initial colors
		if output.Colors["A"] == output.Colors["B"] {
			t.Error("expected different colors for differently labeled nodes")
		}
	})

	t.Run("compare isomorphic graphs", func(t *testing.T) {
		graph1 := &WLGraph{
			Nodes: []string{"A", "B", "C"},
			Edges: map[string][]string{
				"A": {"B"},
				"B": {"A", "C"},
				"C": {"B"},
			},
		}

		graph2 := &WLGraph{
			Nodes: []string{"X", "Y", "Z"},
			Edges: map[string][]string{
				"X": {"Y"},
				"Y": {"X", "Z"},
				"Z": {"Y"},
			},
		}

		input := &WLInput{
			Operation:  "compare",
			Graph:      graph1,
			OtherGraph: graph2,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic graphs to be detected")
		}
	})

	t.Run("compare non-isomorphic graphs", func(t *testing.T) {
		// Triangle
		graph1 := &WLGraph{
			Nodes: []string{"A", "B", "C"},
			Edges: map[string][]string{
				"A": {"B", "C"},
				"B": {"A", "C"},
				"C": {"A", "B"},
			},
		}

		// Linear
		graph2 := &WLGraph{
			Nodes: []string{"X", "Y", "Z"},
			Edges: map[string][]string{
				"X": {"Y"},
				"Y": {"X", "Z"},
				"Z": {"Y"},
			},
		}

		input := &WLInput{
			Operation:  "compare",
			Graph:      graph1,
			OtherGraph: graph2,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if output.IsIsomorphic {
			t.Error("expected non-isomorphic graphs to be detected")
		}
	})

	t.Run("compare different sized graphs", func(t *testing.T) {
		graph1 := &WLGraph{
			Nodes: []string{"A", "B"},
			Edges: map[string][]string{"A": {"B"}, "B": {"A"}},
		}

		graph2 := &WLGraph{
			Nodes: []string{"X", "Y", "Z"},
			Edges: map[string][]string{"X": {"Y"}, "Y": {"X", "Z"}, "Z": {"Y"}},
		}

		input := &WLInput{
			Operation:  "compare",
			Graph:      graph1,
			OtherGraph: graph2,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if output.IsIsomorphic {
			t.Error("expected different sized graphs to be non-isomorphic")
		}
	})

	t.Run("fingerprint graph", func(t *testing.T) {
		input := &WLInput{
			Operation: "fingerprint",
			Graph: &WLGraph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{
					"A": {"B", "C"},
					"B": {"A", "C"},
					"C": {"A", "B"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if output.Fingerprint == 0 {
			t.Error("expected non-zero fingerprint")
		}
	})

	t.Run("isomorphic graphs have same fingerprint", func(t *testing.T) {
		graph1 := &WLGraph{
			Nodes: []string{"A", "B"},
			Edges: map[string][]string{"A": {"B"}, "B": {"A"}},
		}

		graph2 := &WLGraph{
			Nodes: []string{"X", "Y"},
			Edges: map[string][]string{"X": {"Y"}, "Y": {"X"}},
		}

		input1 := &WLInput{Operation: "fingerprint", Graph: graph1}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		fp1 := result1.(*WLOutput).Fingerprint

		input2 := &WLInput{Operation: "fingerprint", Graph: graph2}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		fp2 := result2.(*WLOutput).Fingerprint

		if fp1 != fp2 {
			t.Errorf("expected same fingerprint, got %d and %d", fp1, fp2)
		}
	})

	t.Run("color empty graph", func(t *testing.T) {
		input := &WLInput{
			Operation: "color",
			Graph:     nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WLOutput)
		if output.ColorClasses != 0 {
			t.Errorf("expected 0 color classes, got %d", output.ColorClasses)
		}
	})

	t.Run("respects max nodes limit", func(t *testing.T) {
		algo := NewWeisfeilerLeman(&WLConfig{MaxNodes: 2, MaxIterations: 10})

		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{},
			},
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for too many nodes")
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

		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A"},
				Edges: map[string][]string{},
			},
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

func TestWeisfeilerLeman_Properties(t *testing.T) {
	algo := NewWeisfeilerLeman(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("colors_cover_all_nodes property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "colors_cover_all_nodes" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("colors_cover_all_nodes property not found")
		}

		// Get actual output from algorithm
		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{"A": {"B"}, "B": {"A"}},
			},
		}
		result, _, _ := algo.Process(ctx, snapshot, input)
		output := result.(*WLOutput)

		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: missing node color
		outputBad := &WLOutput{
			Colors: map[string]uint64{"A": 1}, // Missing B
		}
		if err := prop(input, outputBad); err == nil {
			t.Error("expected error for missing node color")
		}
	})

	t.Run("histogram_sums_to_node_count property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "histogram_sums_to_node_count" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("histogram_sums_to_node_count property not found")
		}

		input := &WLInput{
			Operation: "color",
			Graph: &WLGraph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{},
			},
		}

		// Valid histogram
		output := &WLOutput{
			ColorHistogram: map[uint64]int{1: 2, 2: 1}, // 2 + 1 = 3
		}
		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid histogram
		outputBad := &WLOutput{
			ColorHistogram: map[uint64]int{1: 1}, // Only 1, should be 3
		}
		if err := prop(input, outputBad); err == nil {
			t.Error("expected error for incorrect histogram sum")
		}
	})
}

func TestWeisfeilerLeman_Evaluable(t *testing.T) {
	algo := NewWeisfeilerLeman(nil)

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
		algo := &WeisfeilerLeman{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max iterations", func(t *testing.T) {
		algo := NewWeisfeilerLeman(&WLConfig{MaxIterations: 0, MaxNodes: 100})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero max iterations")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
