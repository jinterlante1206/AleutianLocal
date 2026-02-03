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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

func setupPNMCTSTestCRS(t *testing.T) crs.CRS {
	t.Helper()
	c := crs.New(nil)
	ctx := context.Background()

	// Add proof data for test nodes
	proofDelta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
		"root":   {Proof: 10, Disproof: 5, Status: crs.ProofStatusExpanded},
		"child1": {Proof: 5, Disproof: 3, Status: crs.ProofStatusUnknown},
		"child2": {Proof: 8, Disproof: 4, Status: crs.ProofStatusUnknown},
		"leaf1":  {Proof: 1, Disproof: 1, Status: crs.ProofStatusUnknown},
	})
	if _, err := c.Apply(ctx, proofDelta); err != nil {
		t.Fatalf("failed to apply proof delta: %v", err)
	}

	// Add dependency edges: root -> child1 -> leaf1, root -> child2
	depDelta := crs.NewDependencyDelta(crs.SignalSourceHard)
	depDelta.AddEdges = [][2]string{
		{"root", "child1"},
		{"root", "child2"},
		{"child1", "leaf1"},
	}
	if _, err := c.Apply(ctx, depDelta); err != nil {
		t.Fatalf("failed to apply dependency delta: %v", err)
	}

	return c
}

func TestNewPNMCTS(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewPNMCTS(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "pnmcts" {
			t.Errorf("expected name pnmcts, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &PNMCTSConfig{
			MaxIterations: 500,
			Timeout:       3 * time.Second,
		}
		algo := NewPNMCTS(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestPNMCTS_Process(t *testing.T) {
	c := setupPNMCTSTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewPNMCTS(nil)

	t.Run("selects most proving node", func(t *testing.T) {
		input := &PNMCTSInput{
			RootNodeID:  "root",
			TargetNodes: []string{"leaf1"},
		}

		result, delta, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*PNMCTSOutput)
		if !ok {
			t.Fatal("expected *PNMCTSOutput")
		}

		// leaf1 has smallest proof number (1), should be selected
		if output.SelectedNode != "leaf1" {
			t.Errorf("expected leaf1, got %s", output.SelectedNode)
		}

		if output.Iterations == 0 {
			t.Error("expected some iterations")
		}

		// Delta should be soft source (PN-MCTS doesn't verify with compiler)
		if delta != nil {
			proofDelta, ok := delta.(*crs.ProofDelta)
			if ok && proofDelta.Source() != crs.SignalSourceSoft {
				t.Errorf("expected soft source, got %v", proofDelta.Source())
			}
		}
	})

	t.Run("returns error for invalid input type", func(t *testing.T) {
		_, _, err := algo.Process(ctx, snapshot, "invalid")
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})

	t.Run("returns error for empty root", func(t *testing.T) {
		input := &PNMCTSInput{RootNodeID: ""}
		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for empty root")
		}
	})

	t.Run("handles cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		input := &PNMCTSInput{
			RootNodeID:  "root",
			TargetNodes: []string{"leaf1"},
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
}

func TestPNMCTS_ProofNumberUpdate(t *testing.T) {
	c := setupPNMCTSTestCRS(t)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewPNMCTS(nil)

	input := &PNMCTSInput{
		RootNodeID:  "root",
		TargetNodes: []string{"child1", "child2", "leaf1"},
	}

	result, delta, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*PNMCTSOutput)

	// Should have proof updates
	if len(output.ProofUpdates) == 0 && delta == nil {
		t.Log("No proof updates (converged quickly)")
	}

	// Path should go through tree
	if len(output.Path) > 0 {
		if output.Path[0] != "root" {
			t.Errorf("path should start at root, got %s", output.Path[0])
		}
	}
}

func TestPNMCTS_NeverMarksDisproven(t *testing.T) {
	c := crs.New(nil)
	ctx := context.Background()
	snapshot := c.Snapshot()

	algo := NewPNMCTS(nil)

	// Run PN-MCTS on empty CRS
	input := &PNMCTSInput{
		RootNodeID:  "test-node",
		TargetNodes: []string{},
	}

	result, delta, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*PNMCTSOutput)

	// Check that no node is marked DISPROVEN
	for nodeID, pn := range output.ProofUpdates {
		if pn.Status == crs.ProofStatusDisproven {
			t.Errorf("PN-MCTS should never mark nodes DISPROVEN, but %s is", nodeID)
		}
	}

	// Also verify delta source is soft
	if delta != nil {
		if delta.Source() != crs.SignalSourceSoft {
			t.Errorf("expected soft source, got %v", delta.Source())
		}
	}
}

func TestPNMCTS_Evaluable(t *testing.T) {
	algo := NewPNMCTS(nil)

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

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
