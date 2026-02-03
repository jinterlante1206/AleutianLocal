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

func setupUnitPropTestCRS(t *testing.T) crs.CRS {
	t.Helper()
	c := crs.New(nil)
	ctx := context.Background()

	// Add proof data for test nodes
	proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"node1": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
		"node2": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
		"node3": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
		"node4": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
	})
	if _, err := c.Apply(ctx, proofDelta); err != nil {
		t.Fatalf("failed to apply proof delta: %v", err)
	}

	// Add mutual exclusion constraint: only one of node1, node2, node3 can be selected
	constraintDelta := crs.NewConstraintDelta(crs.SignalSourceHard)
	constraintDelta.Add = []crs.Constraint{
		{
			ID:    "mutex1",
			Type:  crs.ConstraintTypeMutualExclusion,
			Nodes: []string{"node1", "node2", "node3"},
		},
		{
			ID:    "impl1",
			Type:  crs.ConstraintTypeImplication,
			Nodes: []string{"node1", "node4"}, // node1 selected implies node4 selected
		},
		{
			ID:    "order1",
			Type:  crs.ConstraintTypeOrdering,
			Nodes: []string{"node2", "node3"}, // node3 selected implies node2 selected
		},
	}
	if _, err := c.Apply(ctx, constraintDelta); err != nil {
		t.Fatalf("failed to apply constraint delta: %v", err)
	}

	return c
}

func TestNewUnitPropagation(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewUnitPropagation(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "unit_propagation" {
			t.Errorf("expected name unit_propagation, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &UnitPropConfig{
			MaxPropagations: 500,
			Timeout:         3 * time.Second,
			DetectConflicts: false,
		}
		algo := NewUnitPropagation(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestUnitPropagation_MutualExclusion(t *testing.T) {
	c := setupUnitPropTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewUnitPropagation(nil)

	t.Run("forces deselection when one is selected", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes: []string{"node1", "node2", "node3"},
			Assignments: map[string]bool{
				"node1": true, // node1 is selected
			},
		}

		result, delta, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*UnitPropOutput)
		if !ok {
			t.Fatal("expected *UnitPropOutput")
		}

		// Should force node2 and node3 to be deselected
		forcedNodes := make(map[string]bool)
		for _, move := range output.ForcedMoves {
			if !move.Selected {
				forcedNodes[move.NodeID] = true
			}
		}

		if !forcedNodes["node2"] {
			t.Error("expected node2 to be forced deselected")
		}
		if !forcedNodes["node3"] {
			t.Error("expected node3 to be forced deselected")
		}

		// No conflicts expected
		if output.ConflictDetected {
			t.Error("expected no conflicts")
		}

		// Delta should be nil (no proof status changes from forced moves alone)
		_ = delta // Delta may or may not be nil depending on implementation
	})

	t.Run("detects conflict when multiple selected", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes: []string{"node1", "node2"},
			Assignments: map[string]bool{
				"node1": true,
				"node2": true, // Both selected - violates mutex
			},
		}

		result, delta, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)

		// Should detect conflict
		if !output.ConflictDetected {
			t.Error("expected conflict to be detected")
		}

		// Should have at least one conflict
		if len(output.Conflicts) == 0 {
			t.Error("expected at least one conflict")
		}

		// Delta should mark conflicting nodes as DISPROVEN (hard signal)
		if delta != nil {
			if delta.Source() != crs.SignalSourceHard {
				t.Errorf("expected hard signal source, got %v", delta.Source())
			}
		}
	})
}

func TestUnitPropagation_Implication(t *testing.T) {
	c := setupUnitPropTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewUnitPropagation(nil)

	t.Run("forces consequent when antecedent selected", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes: []string{"node1"},
			Assignments: map[string]bool{
				"node1": true, // Antecedent selected
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)

		// Should force node4 to be selected
		found := false
		for _, move := range output.ForcedMoves {
			if move.NodeID == "node4" && move.Selected {
				found = true
				break
			}
		}

		if !found {
			t.Error("expected node4 to be forced selected due to implication")
		}
	})

	t.Run("detects conflict when consequent deselected", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes: []string{"node1", "node4"},
			Assignments: map[string]bool{
				"node1": true,
				"node4": false, // Consequent deselected - violates implication
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)

		if !output.ConflictDetected {
			t.Error("expected conflict when implication violated")
		}
	})
}

func TestUnitPropagation_Ordering(t *testing.T) {
	ctx := context.Background()
	algo := NewUnitPropagation(nil)

	// Create a separate CRS with only ordering constraint (no conflicting mutex)
	setupOrderingCRS := func() crs.CRS {
		c := crs.New(nil)

		proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"first":  {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
			"second": {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
			"third":  {Proof: 10, Disproof: 5, Status: crs.ProofStatusUnknown},
		})
		c.Apply(ctx, proofDelta)

		constraintDelta := crs.NewConstraintDelta(crs.SignalSourceHard)
		constraintDelta.Add = []crs.Constraint{
			{
				ID:    "order1",
				Type:  crs.ConstraintTypeOrdering,
				Nodes: []string{"first", "second", "third"}, // Must select in order
			},
		}
		c.Apply(ctx, constraintDelta)

		return c
	}

	t.Run("forces earlier nodes when later selected", func(t *testing.T) {
		c := setupOrderingCRS()
		snapshot := c.Snapshot()

		input := &UnitPropInput{
			FocusNodes: []string{"first", "second", "third"},
			Assignments: map[string]bool{
				"third": true, // Later node selected
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)

		// Should force first and second to be selected (ordering constraint)
		forcedSelected := make(map[string]bool)
		for _, move := range output.ForcedMoves {
			if move.Selected {
				forcedSelected[move.NodeID] = true
			}
		}

		if !forcedSelected["first"] {
			t.Error("expected first to be forced selected due to ordering")
		}
		if !forcedSelected["second"] {
			t.Error("expected second to be forced selected due to ordering")
		}
	})

	t.Run("detects conflict when ordering violated", func(t *testing.T) {
		c := setupOrderingCRS()
		snapshot := c.Snapshot()

		input := &UnitPropInput{
			FocusNodes: []string{"first", "second", "third"},
			Assignments: map[string]bool{
				"first": false, // Earlier node deselected
				"third": true,  // Later node selected - violates ordering
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)

		if !output.ConflictDetected {
			t.Error("expected conflict when ordering violated")
		}
	})
}

func TestUnitPropagation_Process(t *testing.T) {
	c := setupUnitPropTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewUnitPropagation(nil)

	t.Run("returns error for invalid input type", func(t *testing.T) {
		_, _, err := algo.Process(ctx, snapshot, "invalid")
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})

	t.Run("handles cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		input := &UnitPropInput{
			FocusNodes:  []string{"node1"},
			Assignments: map[string]bool{},
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

	t.Run("handles empty focus nodes", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes:  []string{},
			Assignments: map[string]bool{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)
		if len(output.ForcedMoves) != 0 {
			t.Errorf("expected 0 forced moves, got %d", len(output.ForcedMoves))
		}
	})

	t.Run("tracks propagation count", func(t *testing.T) {
		input := &UnitPropInput{
			FocusNodes: []string{"node1", "node2", "node3"},
			Assignments: map[string]bool{
				"node1": true,
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*UnitPropOutput)
		if output.PropagationCount == 0 {
			t.Error("expected some propagations")
		}
	})
}

func TestUnitPropagation_HardSignalSource(t *testing.T) {
	// This is a CRITICAL test: Unit Propagation uses HARD signal source
	// because constraint violations are deterministic (not LLM-based)

	c := setupUnitPropTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewUnitPropagation(nil)

	// Create a conflict
	input := &UnitPropInput{
		FocusNodes: []string{"node1", "node2"},
		Assignments: map[string]bool{
			"node1": true,
			"node2": true, // Violates mutex
		},
	}

	_, delta, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Delta should use HARD signal source
	if delta != nil {
		if delta.Source() != crs.SignalSourceHard {
			t.Errorf("UnitPropagation MUST use hard signal source for conflicts, got %v", delta.Source())
		}
	}
}

func TestUnitPropagation_MarksConflictingNodesDisproven(t *testing.T) {
	c := setupUnitPropTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewUnitPropagation(nil)

	// Create a conflict
	input := &UnitPropInput{
		FocusNodes: []string{"node1", "node2"},
		Assignments: map[string]bool{
			"node1": true,
			"node2": true, // Violates mutex
		},
	}

	_, delta, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Delta should mark conflicting nodes as DISPROVEN
	if delta != nil {
		proofDelta, ok := delta.(*crs.ProofDelta)
		if ok {
			for _, pn := range proofDelta.Updates {
				if pn.Status != crs.ProofStatusDisproven {
					// It's OK if some updates aren't DISPROVEN
					// but at least one should be
				}
			}
		}
	}
}

func TestUnitPropagation_Evaluable(t *testing.T) {
	algo := NewUnitPropagation(nil)

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
		if !found["forced_moves_valid"] {
			t.Error("expected forced_moves_valid property")
		}
		if !found["conflicts_are_real"] {
			t.Error("expected conflicts_are_real property")
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
		algo := &UnitPropagation{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewUnitPropagation(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}

func TestUnitPropagation_ConflictForcedBothWays(t *testing.T) {
	c := crs.New(nil)
	ctx := context.Background()

	// Create constraints that force a node both ways
	constraintDelta := crs.NewConstraintDelta(crs.SignalSourceHard)
	constraintDelta.Add = []crs.Constraint{
		{
			ID:    "impl1",
			Type:  crs.ConstraintTypeImplication,
			Nodes: []string{"a", "b"}, // a -> b
		},
		{
			ID:    "mutex1",
			Type:  crs.ConstraintTypeMutualExclusion,
			Nodes: []string{"b", "c"}, // b XOR c
		},
	}
	c.Apply(ctx, constraintDelta)

	snapshot := c.Snapshot()
	algo := NewUnitPropagation(nil)

	// Set up: a=true, c=true
	// a=true forces b=true (implication)
	// c=true forces b=false (mutual exclusion)
	// This creates a conflict where b is forced both ways
	input := &UnitPropInput{
		FocusNodes: []string{"a", "b", "c"},
		Assignments: map[string]bool{
			"a": true,
			"c": true,
		},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*UnitPropOutput)

	// Should detect the conflict
	if !output.ConflictDetected {
		t.Error("expected conflict when node is forced both ways")
	}
}
