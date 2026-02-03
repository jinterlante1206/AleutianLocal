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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

func TestNewDominators(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewDominators(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "dominators" {
			t.Errorf("expected name dominators, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &DominatorsConfig{
			MaxNodes: 500,
			Timeout:  3 * time.Second,
		}
		algo := NewDominators(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestDominators_Process(t *testing.T) {
	algo := NewDominators(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("single node", func(t *testing.T) {
		input := &DominatorsInput{
			Entry:      "A",
			Nodes:      []string{"A"},
			Successors: map[string][]string{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*DominatorsOutput)
		if len(output.IDom) != 0 {
			t.Errorf("expected no idom for single node, got %d", len(output.IDom))
		}
		if output.NodesReachable != 1 {
			t.Errorf("expected 1 reachable node, got %d", output.NodesReachable)
		}
	})

	t.Run("linear chain", func(t *testing.T) {
		// A -> B -> C -> D
		input := &DominatorsInput{
			Entry: "A",
			Nodes: []string{"A", "B", "C", "D"},
			Successors: map[string][]string{
				"A": {"B"},
				"B": {"C"},
				"C": {"D"},
				"D": {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*DominatorsOutput)

		// B's idom is A, C's idom is B, D's idom is C
		expected := map[string]string{
			"B": "A",
			"C": "B",
			"D": "C",
		}
		for node, expectedIdom := range expected {
			if output.IDom[node] != expectedIdom {
				t.Errorf("expected idom[%s]=%s, got %s", node, expectedIdom, output.IDom[node])
			}
		}

		if output.TreeDepth != 3 {
			t.Errorf("expected tree depth 3, got %d", output.TreeDepth)
		}
	})

	t.Run("diamond pattern", func(t *testing.T) {
		//     A
		//    / \
		//   B   C
		//    \ /
		//     D
		input := &DominatorsInput{
			Entry: "A",
			Nodes: []string{"A", "B", "C", "D"},
			Successors: map[string][]string{
				"A": {"B", "C"},
				"B": {"D"},
				"C": {"D"},
				"D": {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*DominatorsOutput)

		// B and C are dominated by A
		// D is dominated by A (not B or C, since both paths lead to D)
		if output.IDom["B"] != "A" {
			t.Errorf("expected idom[B]=A, got %s", output.IDom["B"])
		}
		if output.IDom["C"] != "A" {
			t.Errorf("expected idom[C]=A, got %s", output.IDom["C"])
		}
		if output.IDom["D"] != "A" {
			t.Errorf("expected idom[D]=A, got %s", output.IDom["D"])
		}
	})

	t.Run("complex CFG", func(t *testing.T) {
		// Entry -> A -> B -> C -> Exit
		//          |         ^
		//          |         |
		//          +-> D ----+
		input := &DominatorsInput{
			Entry: "Entry",
			Nodes: []string{"Entry", "A", "B", "C", "D", "Exit"},
			Successors: map[string][]string{
				"Entry": {"A"},
				"A":     {"B", "D"},
				"B":     {"C"},
				"C":     {"Exit"},
				"D":     {"C"},
				"Exit":  {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*DominatorsOutput)

		if output.IDom["A"] != "Entry" {
			t.Errorf("expected idom[A]=Entry, got %s", output.IDom["A"])
		}
		if output.IDom["B"] != "A" {
			t.Errorf("expected idom[B]=A, got %s", output.IDom["B"])
		}
		if output.IDom["D"] != "A" {
			t.Errorf("expected idom[D]=A, got %s", output.IDom["D"])
		}
		// C is dominated by A (the common dominator of B and D)
		if output.IDom["C"] != "A" {
			t.Errorf("expected idom[C]=A, got %s", output.IDom["C"])
		}
	})

	t.Run("unreachable nodes", func(t *testing.T) {
		// A -> B, C is unreachable
		input := &DominatorsInput{
			Entry: "A",
			Nodes: []string{"A", "B", "C"},
			Successors: map[string][]string{
				"A": {"B"},
				"B": {},
				"C": {},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*DominatorsOutput)
		if output.NodesReachable != 2 {
			t.Errorf("expected 2 reachable nodes, got %d", output.NodesReachable)
		}
		if _, exists := output.IDom["C"]; exists {
			t.Error("unreachable node C should not have idom")
		}
	})

	t.Run("returns error for missing entry", func(t *testing.T) {
		input := &DominatorsInput{
			Entry:      "",
			Nodes:      []string{"A"},
			Successors: map[string][]string{},
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for missing entry")
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

		input := &DominatorsInput{
			Entry:      "A",
			Nodes:      []string{"A", "B"},
			Successors: map[string][]string{"A": {"B"}},
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
		algo := NewDominators(&DominatorsConfig{
			MaxNodes: 2,
			Timeout:  5 * time.Second,
		})

		input := &DominatorsInput{
			Entry:      "A",
			Nodes:      []string{"A", "B", "C"},
			Successors: map[string][]string{},
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for too many nodes")
		}
	})
}

func TestDominators_DominatorTree(t *testing.T) {
	algo := NewDominators(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	input := &DominatorsInput{
		Entry: "A",
		Nodes: []string{"A", "B", "C", "D"},
		Successors: map[string][]string{
			"A": {"B"},
			"B": {"C", "D"},
			"C": {},
			"D": {},
		},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*DominatorsOutput)

	// Verify dominator tree structure
	// A dominates B, B dominates C and D
	if len(output.DominatorTree["A"]) != 1 || output.DominatorTree["A"][0] != "B" {
		t.Errorf("expected A to dominate [B], got %v", output.DominatorTree["A"])
	}

	bChildren := make(map[string]bool)
	for _, child := range output.DominatorTree["B"] {
		bChildren[child] = true
	}
	if !bChildren["C"] || !bChildren["D"] {
		t.Errorf("expected B to dominate [C, D], got %v", output.DominatorTree["B"])
	}
}

func TestDominators_DominanceFrontier(t *testing.T) {
	algo := NewDominators(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	// Diamond pattern where D is in the dominance frontier of B and C
	input := &DominatorsInput{
		Entry: "A",
		Nodes: []string{"A", "B", "C", "D"},
		Successors: map[string][]string{
			"A": {"B", "C"},
			"B": {"D"},
			"C": {"D"},
			"D": {},
		},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*DominatorsOutput)

	// D should be in the dominance frontier of B
	bFrontier := make(map[string]bool)
	for _, node := range output.DominanceFrontier["B"] {
		bFrontier[node] = true
	}
	if !bFrontier["D"] {
		t.Errorf("expected D in dominance frontier of B, got %v", output.DominanceFrontier["B"])
	}

	// D should be in the dominance frontier of C
	cFrontier := make(map[string]bool)
	for _, node := range output.DominanceFrontier["C"] {
		cFrontier[node] = true
	}
	if !cFrontier["D"] {
		t.Errorf("expected D in dominance frontier of C, got %v", output.DominanceFrontier["C"])
	}
}

func TestDominators_Properties(t *testing.T) {
	algo := NewDominators(nil)

	t.Run("entry_has_no_idom property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "entry_has_no_idom" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("entry_has_no_idom property not found")
		}

		input := &DominatorsInput{Entry: "A"}

		// Should pass: entry has no idom
		output := &DominatorsOutput{
			IDom: map[string]string{"B": "A"},
		}
		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: entry has idom
		outputBad := &DominatorsOutput{
			IDom: map[string]string{"A": "X"}, // Entry has idom!
		}
		if err := prop(input, outputBad); err == nil {
			t.Error("expected error for entry with idom")
		}
	})

	t.Run("tree_is_valid property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "tree_is_valid" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("tree_is_valid property not found")
		}

		// Should pass: tree consistent with idom
		output := &DominatorsOutput{
			IDom:          map[string]string{"B": "A", "C": "A"},
			DominatorTree: map[string][]string{"A": {"B", "C"}},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: tree inconsistent with idom
		outputBad := &DominatorsOutput{
			IDom:          map[string]string{"B": "A"},
			DominatorTree: map[string][]string{"X": {"B"}}, // B's parent should be A, not X
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for inconsistent tree")
		}
	})
}

func TestDominators_Evaluable(t *testing.T) {
	algo := NewDominators(nil)

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
		algo := &Dominators{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max nodes", func(t *testing.T) {
		algo := NewDominators(&DominatorsConfig{MaxNodes: 0})
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
