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

func TestNewVF2(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewVF2(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "vf2" {
			t.Errorf("expected name vf2, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &VF2Config{
			MaxMatches: 10,
			Timeout:    3 * time.Second,
		}
		algo := NewVF2(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestVF2_Process(t *testing.T) {
	algo := NewVF2(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("empty pattern matches", func(t *testing.T) {
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{},
				Edges: map[string][]string{},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{"A": {"B"}},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic for empty pattern")
		}
	})

	t.Run("pattern larger than target", func(t *testing.T) {
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{},
			},
			Target: VF2Graph{
				Nodes: []string{"X", "Y"},
				Edges: map[string][]string{},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if output.IsIsomorphic {
			t.Error("expected not isomorphic when pattern > target")
		}
	})

	t.Run("single node match", func(t *testing.T) {
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P"},
				Edges: map[string][]string{},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		if output.MatchCount != 2 {
			t.Errorf("expected 2 matches, got %d", output.MatchCount)
		}
	})

	t.Run("simple edge match", func(t *testing.T) {
		// Pattern: P1 -> P2
		// Target: A -> B, C -> D
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2"},
				Edges: map[string][]string{"P1": {"P2"}},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C", "D"},
				Edges: map[string][]string{
					"A": {"B"},
					"C": {"D"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		if output.MatchCount != 2 {
			t.Errorf("expected 2 matches, got %d", output.MatchCount)
		}

		// Verify one of the matches
		foundAB := false
		foundCD := false
		for _, match := range output.Matches {
			if match["P1"] == "A" && match["P2"] == "B" {
				foundAB = true
			}
			if match["P1"] == "C" && match["P2"] == "D" {
				foundCD = true
			}
		}
		if !foundAB || !foundCD {
			t.Errorf("expected matches A->B and C->D, got %v", output.Matches)
		}
	})

	t.Run("triangle pattern", func(t *testing.T) {
		// Pattern: triangle P1 -> P2 -> P3 -> P1
		// Target: contains triangle A -> B -> C -> A and more
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2", "P3"},
				Edges: map[string][]string{
					"P1": {"P2"},
					"P2": {"P3"},
					"P3": {"P1"},
				},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C", "D"},
				Edges: map[string][]string{
					"A": {"B"},
					"B": {"C"},
					"C": {"A"},
					"D": {"A"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		// Should find 3 rotations of the triangle
		if output.MatchCount != 3 {
			t.Errorf("expected 3 matches (rotations), got %d", output.MatchCount)
		}
	})

	t.Run("no match due to structure", func(t *testing.T) {
		// Pattern: chain P1 -> P2 -> P3
		// Target: only has edges A -> B (chain of 2, not 3)
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2", "P3"},
				Edges: map[string][]string{
					"P1": {"P2"},
					"P2": {"P3"},
				},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{
					"A": {"B"},
					// No edge from B, so no chain of 3
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if output.IsIsomorphic {
			t.Error("expected not isomorphic")
		}
	})

	t.Run("label matching", func(t *testing.T) {
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes:      []string{"P1", "P2"},
				Edges:      map[string][]string{"P1": {"P2"}},
				NodeLabels: map[string]string{"P1": "func", "P2": "call"},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C", "D"},
				Edges: map[string][]string{
					"A": {"B"},
					"C": {"D"},
				},
				NodeLabels: map[string]string{
					"A": "func", "B": "call",
					"C": "var", "D": "call",
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		// Only A->B should match (C has wrong label)
		if output.MatchCount != 1 {
			t.Errorf("expected 1 match, got %d", output.MatchCount)
		}
	})

	t.Run("custom node matcher", func(t *testing.T) {
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2"},
				Edges: map[string][]string{"P1": {"P2"}},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C", "D"},
				Edges: map[string][]string{
					"A": {"B"},
					"C": {"D"},
				},
			},
			NodeMatcher: func(pNode, tNode string, pLabels, tLabels map[string]string) bool {
				// Only allow matching P1 to A
				if pNode == "P1" {
					return tNode == "A"
				}
				return true
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		// Only A->B should match due to custom matcher
		if output.MatchCount != 1 {
			t.Errorf("expected 1 match, got %d", output.MatchCount)
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

		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1"},
				Edges: map[string][]string{},
			},
			Target: VF2Graph{
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

	t.Run("respects max matches limit", func(t *testing.T) {
		algo := NewVF2(&VF2Config{
			MaxMatches:    1,
			MaxIterations: 100000,
			Timeout:       5 * time.Second,
		})

		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P"},
				Edges: map[string][]string{},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C"},
				Edges: map[string][]string{},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if output.MatchCount != 1 {
			t.Errorf("expected 1 match (limited), got %d", output.MatchCount)
		}
	})
}

func TestVF2_ComplexPatterns(t *testing.T) {
	algo := NewVF2(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("diamond pattern", func(t *testing.T) {
		// Pattern: diamond shape
		//     P1
		//    / \
		//   P2  P3
		//    \ /
		//     P4
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2", "P3", "P4"},
				Edges: map[string][]string{
					"P1": {"P2", "P3"},
					"P2": {"P4"},
					"P3": {"P4"},
				},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B", "C", "D", "E"},
				Edges: map[string][]string{
					"A": {"B", "C"},
					"B": {"D"},
					"C": {"D"},
					"E": {"A"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		// Should find matches with P2/P3 swappable
		if output.MatchCount < 1 {
			t.Errorf("expected at least 1 match, got %d", output.MatchCount)
		}
	})

	t.Run("star pattern", func(t *testing.T) {
		// Pattern: star with center and 3 leaves
		// Center -> L1, L2, L3
		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"Center", "L1", "L2", "L3"},
				Edges: map[string][]string{
					"Center": {"L1", "L2", "L3"},
				},
			},
			Target: VF2Graph{
				Nodes: []string{"Hub", "A", "B", "C", "D"},
				Edges: map[string][]string{
					"Hub": {"A", "B", "C", "D"},
				},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*VF2Output)
		if !output.IsIsomorphic {
			t.Error("expected isomorphic")
		}
		// Should find multiple matches (permutations of leaves)
		// 4 choose 3 * 3! = 4 * 6 = 24 matches
		if output.MatchCount != 24 {
			t.Errorf("expected 24 matches, got %d", output.MatchCount)
		}
	})
}

func TestVF2_Properties(t *testing.T) {
	algo := NewVF2(nil)

	t.Run("mapping_is_bijection property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "mapping_is_bijection" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("mapping_is_bijection property not found")
		}

		// Should pass: unique targets
		output := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A", "P2": "B"},
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: duplicate targets
		outputBad := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A", "P2": "A"}, // Both map to A!
			},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for duplicate targets")
		}
	})

	t.Run("mapping_preserves_edges property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "mapping_preserves_edges" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("mapping_preserves_edges property not found")
		}

		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2"},
				Edges: map[string][]string{"P1": {"P2"}},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{"A": {"B"}},
			},
		}

		// Should pass: edge preserved
		output := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A", "P2": "B"},
			},
		}
		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: edge not preserved
		inputBad := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2"},
				Edges: map[string][]string{"P1": {"P2"}},
			},
			Target: VF2Graph{
				Nodes: []string{"A", "B"},
				Edges: map[string][]string{}, // No edges!
			},
		}
		outputBad := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A", "P2": "B"},
			},
		}
		if err := prop(inputBad, outputBad); err == nil {
			t.Error("expected error for missing edge")
		}
	})

	t.Run("complete_pattern_coverage property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "complete_pattern_coverage" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("complete_pattern_coverage property not found")
		}

		input := &VF2Input{
			Pattern: VF2Graph{
				Nodes: []string{"P1", "P2"},
			},
		}

		// Should pass: all pattern nodes covered
		output := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A", "P2": "B"},
			},
		}
		if err := prop(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Should fail: incomplete coverage
		outputBad := &VF2Output{
			Matches: []map[string]string{
				{"P1": "A"}, // Missing P2!
			},
		}
		if err := prop(input, outputBad); err == nil {
			t.Error("expected error for incomplete coverage")
		}
	})
}

func TestVF2_Evaluable(t *testing.T) {
	algo := NewVF2(nil)

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
		algo := &VF2{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid max iterations", func(t *testing.T) {
		algo := NewVF2(&VF2Config{MaxIterations: 0})
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
