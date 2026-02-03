// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewTarjanSCC(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewTarjanSCC(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "tarjan_scc" {
			t.Errorf("expected name tarjan_scc, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &TarjanConfig{
			MaxNodes: 500,
			Timeout:  3 * time.Second,
		}
		algo := NewTarjanSCC(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestTarjanSCC_Process(t *testing.T) {
	algo := NewTarjanSCC(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("single node no edges", func(t *testing.T) {
		input := &TarjanInput{
			Nodes: []string{"A"},
			Edges: map[string][]string{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 1 {
			t.Errorf("expected 1 SCC, got %d", len(output.SCCs))
		}
		if output.Cyclic {
			t.Error("expected non-cyclic")
		}
		if output.LargestSCCSize != 1 {
			t.Errorf("expected largest SCC size 1, got %d", output.LargestSCCSize)
		}
	})

	t.Run("simple cycle", func(t *testing.T) {
		// A -> B -> C -> A
		input := &TarjanInput{
			Nodes: []string{"A", "B", "C"},
			Edges: map[string][]string{
				"A": {"B"},
				"B": {"C"},
				"C": {"A"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 1 {
			t.Errorf("expected 1 SCC, got %d", len(output.SCCs))
		}
		if !output.Cyclic {
			t.Error("expected cyclic")
		}
		if output.LargestSCCSize != 3 {
			t.Errorf("expected largest SCC size 3, got %d", output.LargestSCCSize)
		}
	})

	t.Run("two separate SCCs", func(t *testing.T) {
		// SCC1: A -> B -> A
		// SCC2: C -> D -> C
		// A -> C (cross edge)
		input := &TarjanInput{
			Nodes: []string{"A", "B", "C", "D"},
			Edges: map[string][]string{
				"A": {"B", "C"},
				"B": {"A"},
				"C": {"D"},
				"D": {"C"},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 2 {
			t.Errorf("expected 2 SCCs, got %d", len(output.SCCs))
		}
		if !output.Cyclic {
			t.Error("expected cyclic")
		}
	})

	t.Run("DAG no cycles", func(t *testing.T) {
		// A -> B -> C
		// A -> C
		input := &TarjanInput{
			Nodes: []string{"A", "B", "C"},
			Edges: map[string][]string{
				"A": {"B", "C"},
				"B": {"C"},
				"C": {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 3 {
			t.Errorf("expected 3 SCCs (each node), got %d", len(output.SCCs))
		}
		if output.Cyclic {
			t.Error("expected non-cyclic")
		}
		if output.LargestSCCSize != 1 {
			t.Errorf("expected largest SCC size 1, got %d", output.LargestSCCSize)
		}
	})

	t.Run("complex graph with multiple SCCs", func(t *testing.T) {
		// SCC1: {1, 2, 3}
		// SCC2: {4, 5}
		// SCC3: {6}
		// 1 -> 2 -> 3 -> 1
		// 3 -> 4
		// 4 -> 5 -> 4
		// 5 -> 6
		input := &TarjanInput{
			Nodes: []string{"1", "2", "3", "4", "5", "6"},
			Edges: map[string][]string{
				"1": {"2"},
				"2": {"3"},
				"3": {"1", "4"},
				"4": {"5"},
				"5": {"4", "6"},
				"6": {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 3 {
			t.Errorf("expected 3 SCCs, got %d", len(output.SCCs))
		}
		if !output.Cyclic {
			t.Error("expected cyclic")
		}
		if output.NodesProcessed != 6 {
			t.Errorf("expected 6 nodes processed, got %d", output.NodesProcessed)
		}
	})

	t.Run("empty graph", func(t *testing.T) {
		input := &TarjanInput{
			Nodes: []string{},
			Edges: map[string][]string{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TarjanOutput)
		if len(output.SCCs) != 0 {
			t.Errorf("expected 0 SCCs, got %d", len(output.SCCs))
		}
		if output.Cyclic {
			t.Error("expected non-cyclic")
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

		input := &TarjanInput{
			Nodes: []string{"A", "B"},
			Edges: map[string][]string{"A": {"B"}},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("respects max nodes limit", func(t *testing.T) {
		algo := NewTarjanSCC(&TarjanConfig{
			MaxNodes: 2,
			Timeout:  5 * time.Second,
		})

		input := &TarjanInput{
			Nodes: []string{"A", "B", "C"},
			Edges: map[string][]string{},
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for too many nodes")
		}
	})
}

func TestTarjanSCC_NodeToSCC(t *testing.T) {
	algo := NewTarjanSCC(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := &TarjanInput{
		Nodes: []string{"A", "B", "C", "D"},
		Edges: map[string][]string{
			"A": {"B"},
			"B": {"A"},
			"C": {"D"},
			"D": {"C"},
		},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*TarjanOutput)

	// Verify NodeToSCC mapping
	if len(output.NodeToSCC) != 4 {
		t.Errorf("expected 4 entries in NodeToSCC, got %d", len(output.NodeToSCC))
	}

	// A and B should be in same SCC
	if output.NodeToSCC["A"] != output.NodeToSCC["B"] {
		t.Error("expected A and B in same SCC")
	}

	// C and D should be in same SCC
	if output.NodeToSCC["C"] != output.NodeToSCC["D"] {
		t.Error("expected C and D in same SCC")
	}

	// A and C should be in different SCCs
	if output.NodeToSCC["A"] == output.NodeToSCC["C"] {
		t.Error("expected A and C in different SCCs")
	}
}

func TestTarjanSCC_Properties(t *testing.T) {
	algo := NewTarjanSCC(nil)

	t.Run("sccs_partition_nodes property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "sccs_partition_nodes" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("sccs_partition_nodes property not found")
		}

		// Should pass for valid output
		output := &TarjanOutput{
			SCCs:           [][]string{{"A", "B"}, {"C"}},
			NodesProcessed: 3,
		}
		if err := prop(&TarjanInput{}, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail for inconsistent counts
		outputBad := &TarjanOutput{
			SCCs:           [][]string{{"A", "B"}},
			NodesProcessed: 5, // Mismatch!
		}
		if err := prop(&TarjanInput{}, outputBad); err == nil {
			t.Error("expected error for inconsistent counts")
		}
	})

	t.Run("cyclic_implies_large_scc property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "cyclic_implies_large_scc" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("cyclic_implies_large_scc property not found")
		}

		// Should pass: cyclic=true with SCC size > 1
		output := &TarjanOutput{
			SCCs:   [][]string{{"A", "B"}},
			Cyclic: true,
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: cyclic=true but no large SCC
		outputBad := &TarjanOutput{
			SCCs:   [][]string{{"A"}, {"B"}},
			Cyclic: true, // Wrong!
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for inconsistent cyclic flag")
		}
	})
}

func TestTarjanSCC_Evaluable(t *testing.T) {
	algo := NewTarjanSCC(nil)

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
		algo := &TarjanSCC{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max nodes", func(t *testing.T) {
		algo := NewTarjanSCC(&TarjanConfig{MaxNodes: 0})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero max nodes")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
