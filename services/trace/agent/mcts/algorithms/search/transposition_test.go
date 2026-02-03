// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package search

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func setupTranspositionTestCRS(t *testing.T) crs.CRS {
	t.Helper()
	c := crs.New(nil)
	ctx := context.Background()

	// Add proof data for test nodes
	proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
		"node2": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown}, // Same as node1
		"node3": {Proof: 20, Disproof: 10, Status: crs.ProofStatusProven},
	})
	if _, err := c.Apply(ctx, proofDelta); err != nil {
		t.Fatalf("failed to apply proof delta: %v", err)
	}

	// Add dependency edges
	depDelta := crs.NewDependencyDelta(crs.SignalSourceHard)
	depDelta.AddEdges = [][2]string{
		{"node1", "child1"},
		{"node2", "child1"}, // Same dependency as node1
		{"node3", "child2"},
	}
	if _, err := c.Apply(ctx, depDelta); err != nil {
		t.Fatalf("failed to apply dependency delta: %v", err)
	}

	return c
}

func TestNewTransposition(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewTransposition(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "transposition" {
			t.Errorf("expected name transposition, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &TranspositionConfig{
			TableSize: 1 << 10,
			MaxAge:    50,
			Timeout:   3 * time.Second,
		}
		algo := NewTransposition(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestTransposition_Process(t *testing.T) {
	c := setupTranspositionTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewTransposition(nil)

	t.Run("computes hashes for nodes", func(t *testing.T) {
		input := &TranspositionInput{
			Nodes:             []string{"node1", "node2", "node3"},
			CurrentGeneration: 1,
		}

		result, delta, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*TranspositionOutput)
		if !ok {
			t.Fatal("expected *TranspositionOutput")
		}

		// All nodes should have hashes
		if len(output.Hashes) != 3 {
			t.Errorf("expected 3 hashes, got %d", len(output.Hashes))
		}

		// Verify each node has a hash
		for _, nodeID := range input.Nodes {
			if _, exists := output.Hashes[nodeID]; !exists {
				t.Errorf("missing hash for node %s", nodeID)
			}
		}

		// Transposition table is informational, no delta
		if delta != nil {
			t.Errorf("expected nil delta, got %v", delta)
		}
	})

	t.Run("detects transpositions", func(t *testing.T) {
		// Create nodes with identical state
		c := crs.New(nil)
		proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"a": {Proof: 5, Disproof: 5, Status: crs.ProofStatusUnknown},
			"b": {Proof: 5, Disproof: 5, Status: crs.ProofStatusUnknown},
		})
		c.Apply(ctx, proofDelta)
		snapshot := c.Snapshot()

		input := &TranspositionInput{
			Nodes:             []string{"a", "b"},
			CurrentGeneration: 1,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TranspositionOutput)

		// Since a and b have same proof numbers and no dependencies,
		// their hashes differ only by node ID
		// This tests the basic hashing mechanism
		if output.CacheHits+output.CacheMisses != 2 {
			t.Errorf("expected 2 total lookups, got %d", output.CacheHits+output.CacheMisses)
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

		input := &TranspositionInput{
			Nodes:             []string{"node1", "node2", "node3"},
			CurrentGeneration: 1,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}

		// Should have partial results
		if result == nil {
			t.Error("expected partial results")
		}
	})

	t.Run("handles empty node list", func(t *testing.T) {
		input := &TranspositionInput{
			Nodes:             []string{},
			CurrentGeneration: 1,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TranspositionOutput)
		if len(output.Hashes) != 0 {
			t.Errorf("expected 0 hashes, got %d", len(output.Hashes))
		}
	})
}

func TestTransposition_HashDeterminism(t *testing.T) {
	c := setupTranspositionTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewTransposition(nil)

	input := &TranspositionInput{
		Nodes:             []string{"node1"},
		CurrentGeneration: 1,
	}

	// Run multiple times
	var hashes []uint64
	for i := 0; i < 5; i++ {
		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}
		output := result.(*TranspositionOutput)
		hashes = append(hashes, output.Hashes["node1"])
	}

	// All hashes should be identical
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("hash %d differs: %d != %d", i, hashes[i], hashes[0])
		}
	}
}

func TestTransposition_Evaluable(t *testing.T) {
	algo := NewTransposition(nil)

	t.Run("has properties", func(t *testing.T) {
		props := algo.Properties()
		if len(props) == 0 {
			t.Error("expected properties")
		}

		// Check for specific properties
		found := make(map[string]bool)
		for _, p := range props {
			found[p.Name] = true
		}
		if !found["hash_determinism"] {
			t.Error("expected hash_determinism property")
		}
		if !found["transposition_correctness"] {
			t.Error("expected transposition_correctness property")
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

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}

func TestTransposition_TranspositionCorrectnessProperty(t *testing.T) {
	algo := NewTransposition(nil)

	props := algo.Properties()
	var correctnessProp func(input, output any) error
	for _, p := range props {
		if p.Name == "transposition_correctness" {
			correctnessProp = p.Check
			break
		}
	}

	if correctnessProp == nil {
		t.Fatal("transposition_correctness property not found")
	}

	t.Run("passes for valid transpositions", func(t *testing.T) {
		output := &TranspositionOutput{
			Hashes: map[string]uint64{
				"a": 12345,
				"b": 12345, // Same hash
			},
			Transpositions: map[string]string{
				"b": "a", // b is equivalent to a
			},
		}

		err := correctnessProp(nil, output)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("fails for invalid transpositions", func(t *testing.T) {
		output := &TranspositionOutput{
			Hashes: map[string]uint64{
				"a": 12345,
				"b": 67890, // Different hash
			},
			Transpositions: map[string]string{
				"b": "a", // Claims b is equivalent to a, but hashes differ!
			},
		}

		err := correctnessProp(nil, output)
		if err == nil {
			t.Error("expected error for invalid transposition")
		}
	})
}
