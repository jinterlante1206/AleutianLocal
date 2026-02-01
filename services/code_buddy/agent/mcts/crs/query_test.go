// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"testing"
	"time"
)

// setupTestCRS creates a CRS with test data for query testing.
func setupTestCRS(t *testing.T) CRS {
	t.Helper()
	c := New(nil)
	ctx := context.Background()

	// Add proof data
	proofDelta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
		"node-1": {Proof: 10, Disproof: 5, Status: ProofStatusProven, Source: SignalSourceHard},
		"node-2": {Proof: 20, Disproof: 10, Status: ProofStatusDisproven, Source: SignalSourceHard},
		"node-3": {Proof: 15, Disproof: 8, Status: ProofStatusExpanded, Source: SignalSourceSoft},
		"node-4": {Proof: 5, Disproof: 3, Status: ProofStatusUnknown, Source: SignalSourceUnknown},
		"node-5": {Proof: 25, Disproof: 12, Status: ProofStatusProven, Source: SignalSourceHard},
	})
	if _, err := c.Apply(ctx, proofDelta); err != nil {
		t.Fatalf("failed to apply proof delta: %v", err)
	}

	// Add constraint data
	constraintDelta := NewConstraintDelta(SignalSourceHard)
	constraintDelta.Add = []Constraint{
		{ID: "c1", Type: ConstraintTypeMutualExclusion, Nodes: []string{"node-1", "node-2"}},
		{ID: "c2", Type: ConstraintTypeImplication, Nodes: []string{"node-3", "node-4"}},
		{ID: "c3", Type: ConstraintTypeMutualExclusion, Nodes: []string{"node-4", "node-5"}},
	}
	if _, err := c.Apply(ctx, constraintDelta); err != nil {
		t.Fatalf("failed to apply constraint delta: %v", err)
	}

	// Add similarity data
	simDelta := NewSimilarityDelta(SignalSourceSoft)
	simDelta.Updates = map[[2]string]float64{
		{"node-1", "node-3"}: 0.2,
		{"node-1", "node-4"}: 0.8,
		{"node-2", "node-5"}: 0.3,
		{"node-3", "node-5"}: 0.1,
	}
	if _, err := c.Apply(ctx, simDelta); err != nil {
		t.Fatalf("failed to apply similarity delta: %v", err)
	}

	// Add dependency data
	depDelta := NewDependencyDelta(SignalSourceHard)
	depDelta.AddEdges = [][2]string{
		{"node-1", "node-2"},
		{"node-2", "node-3"},
		{"node-3", "node-4"},
		{"node-1", "node-5"},
	}
	if _, err := c.Apply(ctx, depDelta); err != nil {
		t.Fatalf("failed to apply dependency delta: %v", err)
	}

	// Add history data
	histDelta := NewHistoryDelta(SignalSourceHard, []HistoryEntry{
		{ID: "h1", NodeID: "node-1", Action: "select", Result: "success", Source: SignalSourceHard, Timestamp: time.Now().Add(-2 * time.Hour)},
		{ID: "h2", NodeID: "node-2", Action: "expand", Result: "failure", Source: SignalSourceSoft, Timestamp: time.Now().Add(-1 * time.Hour)},
		{ID: "h3", NodeID: "node-1", Action: "prune", Result: "success", Source: SignalSourceHard, Timestamp: time.Now()},
	})
	if _, err := c.Apply(ctx, histDelta); err != nil {
		t.Fatalf("failed to apply history delta: %v", err)
	}

	// Add streaming data
	streamDelta := NewStreamingDelta(SignalSourceSoft)
	streamDelta.Increments = map[string]uint64{
		"node-1": 100,
		"node-2": 50,
		"node-3": 75,
	}
	if _, err := c.Apply(ctx, streamDelta); err != nil {
		t.Fatalf("failed to apply streaming delta: %v", err)
	}

	return c
}

func TestQueryAPI_FindProvenNodes(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	proven := query.FindProvenNodes()

	if len(proven) != 2 {
		t.Errorf("expected 2 proven nodes, got %d", len(proven))
	}

	// Should be sorted
	expected := []string{"node-1", "node-5"}
	for i, exp := range expected {
		if i >= len(proven) || proven[i] != exp {
			t.Errorf("expected proven[%d] = %s, got %v", i, exp, proven)
		}
	}
}

func TestQueryAPI_FindDisprovenNodes(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	disproven := query.FindDisprovenNodes()

	if len(disproven) != 1 {
		t.Errorf("expected 1 disproven node, got %d", len(disproven))
	}

	if len(disproven) > 0 && disproven[0] != "node-2" {
		t.Errorf("expected node-2, got %s", disproven[0])
	}
}

func TestQueryAPI_FindUnexploredNodes(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	unexplored := query.FindUnexploredNodes()

	if len(unexplored) != 2 {
		t.Errorf("expected 2 unexplored nodes, got %d", len(unexplored))
	}

	// node-3 (expanded) and node-4 (unknown)
	expected := []string{"node-3", "node-4"}
	for i, exp := range expected {
		if i >= len(unexplored) || unexplored[i] != exp {
			t.Errorf("expected unexplored[%d] = %s, got %v", i, exp, unexplored)
		}
	}
}

func TestQueryAPI_FindByProofRange(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("range includes multiple nodes", func(t *testing.T) {
		nodes := query.FindByProofRange(10, 20)
		// node-1 (10), node-3 (15), node-2 (20)
		if len(nodes) != 3 {
			t.Errorf("expected 3 nodes, got %d: %v", len(nodes), nodes)
		}
		// Should be sorted by proof number
		if len(nodes) >= 1 && nodes[0] != "node-1" {
			t.Errorf("expected first node to be node-1, got %s", nodes[0])
		}
	})

	t.Run("range excludes nodes", func(t *testing.T) {
		nodes := query.FindByProofRange(0, 4)
		if len(nodes) != 0 {
			t.Errorf("expected 0 nodes, got %d: %v", len(nodes), nodes)
		}
	})

	t.Run("single node in range", func(t *testing.T) {
		nodes := query.FindByProofRange(5, 5)
		if len(nodes) != 1 || nodes[0] != "node-4" {
			t.Errorf("expected [node-4], got %v", nodes)
		}
	})
}

func TestQueryAPI_FindConstrainedNodes(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("all constraint types", func(t *testing.T) {
		nodes := query.FindConstrainedNodes(ConstraintTypeUnknown)
		if len(nodes) != 5 {
			t.Errorf("expected 5 constrained nodes, got %d: %v", len(nodes), nodes)
		}
	})

	t.Run("mutual exclusion only", func(t *testing.T) {
		nodes := query.FindConstrainedNodes(ConstraintTypeMutualExclusion)
		// c1: node-1, node-2; c3: node-4, node-5
		if len(nodes) != 4 {
			t.Errorf("expected 4 nodes with mutual exclusion, got %d: %v", len(nodes), nodes)
		}
	})

	t.Run("implication only", func(t *testing.T) {
		nodes := query.FindConstrainedNodes(ConstraintTypeImplication)
		// c2: node-3, node-4
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes with implication, got %d: %v", len(nodes), nodes)
		}
	})
}

func TestQueryAPI_FindViolatedConstraints(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	violated := query.FindViolatedConstraints()

	// node-2 is disproven and is in constraint c1
	if len(violated) != 1 {
		t.Errorf("expected 1 violated constraint, got %d: %v", len(violated), violated)
	}

	if len(violated) > 0 && violated[0].ID != "c1" {
		t.Errorf("expected c1 to be violated, got %s", violated[0].ID)
	}
}

func TestQueryAPI_FindSimilarWithProofStatus(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("find similar proven nodes", func(t *testing.T) {
		similar := query.FindSimilarWithProofStatus("node-3", ProofStatusProven, 2)
		// node-3 is similar to node-5 (0.1) and node-1 (0.2), both proven
		if len(similar) < 1 {
			t.Errorf("expected at least 1 similar proven node, got %d", len(similar))
		}
	})

	t.Run("zero k returns nil", func(t *testing.T) {
		similar := query.FindSimilarWithProofStatus("node-1", ProofStatusProven, 0)
		if similar != nil {
			t.Errorf("expected nil for k=0, got %v", similar)
		}
	})
}

func TestQueryAPI_FindDependencyChain(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("direct path exists", func(t *testing.T) {
		chain := query.FindDependencyChain("node-1", "node-3")
		// node-1 -> node-2 -> node-3
		if len(chain) != 3 {
			t.Errorf("expected chain of 3, got %d: %v", len(chain), chain)
		}
		if len(chain) >= 3 {
			if chain[0] != "node-1" || chain[1] != "node-2" || chain[2] != "node-3" {
				t.Errorf("expected [node-1, node-2, node-3], got %v", chain)
			}
		}
	})

	t.Run("same node returns single element", func(t *testing.T) {
		chain := query.FindDependencyChain("node-1", "node-1")
		if len(chain) != 1 || chain[0] != "node-1" {
			t.Errorf("expected [node-1], got %v", chain)
		}
	})

	t.Run("no path returns nil", func(t *testing.T) {
		chain := query.FindDependencyChain("node-3", "node-1")
		// Reverse direction, no path
		if chain != nil {
			t.Errorf("expected nil for no path, got %v", chain)
		}
	})
}

func TestQueryAPI_FindAffectedByNode(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("node with dependents", func(t *testing.T) {
		affected := query.FindAffectedByNode("node-4")
		// node-3 depends on node-4, node-2 depends on node-3, node-1 depends on node-2
		// So all of node-3, node-2, node-1 are affected
		if len(affected) != 3 {
			t.Errorf("expected 3 affected nodes, got %d: %v", len(affected), affected)
		}
	})

	t.Run("node with no dependents", func(t *testing.T) {
		affected := query.FindAffectedByNode("node-1")
		if len(affected) != 0 {
			t.Errorf("expected 0 affected nodes, got %d: %v", len(affected), affected)
		}
	})
}

func TestQueryAPI_FindHotNodes(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("returns sorted by frequency", func(t *testing.T) {
		hot := query.FindHotNodes(3)
		if len(hot) != 3 {
			t.Errorf("expected 3 hot nodes, got %d", len(hot))
		}
		if len(hot) >= 1 && hot[0].NodeID != "node-1" {
			t.Errorf("expected node-1 to be hottest (100), got %s (%d)", hot[0].NodeID, hot[0].Frequency)
		}
		if len(hot) >= 2 && hot[1].NodeID != "node-3" {
			t.Errorf("expected node-3 to be second hottest (75), got %s (%d)", hot[1].NodeID, hot[1].Frequency)
		}
	})

	t.Run("zero n returns nil", func(t *testing.T) {
		hot := query.FindHotNodes(0)
		if hot != nil {
			t.Errorf("expected nil for n=0, got %v", hot)
		}
	})
}

func TestQueryAPI_FindRecentDecisions(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("all sources", func(t *testing.T) {
		recent := query.FindRecentDecisions(10, SignalSourceUnknown)
		if len(recent) != 3 {
			t.Errorf("expected 3 decisions, got %d", len(recent))
		}
	})

	t.Run("filter by hard source", func(t *testing.T) {
		recent := query.FindRecentDecisions(10, SignalSourceHard)
		if len(recent) != 2 {
			t.Errorf("expected 2 hard decisions, got %d", len(recent))
		}
	})

	t.Run("filter by soft source", func(t *testing.T) {
		recent := query.FindRecentDecisions(10, SignalSourceSoft)
		if len(recent) != 1 {
			t.Errorf("expected 1 soft decision, got %d", len(recent))
		}
	})

	t.Run("zero n returns nil", func(t *testing.T) {
		recent := query.FindRecentDecisions(0, SignalSourceUnknown)
		if recent != nil {
			t.Errorf("expected nil for n=0, got %v", recent)
		}
	})
}

func TestQueryAPI_NodeStats(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	t.Run("node with data in all indexes", func(t *testing.T) {
		stats := query.NodeStats("node-1")
		if stats == nil {
			t.Fatal("expected stats for node-1, got nil")
		}

		if !stats.HasProof {
			t.Error("expected HasProof to be true")
		}
		if stats.ProofNumber.Status != ProofStatusProven {
			t.Errorf("expected status PROVEN, got %v", stats.ProofNumber.Status)
		}
		if stats.ConstraintCount != 1 {
			t.Errorf("expected 1 constraint, got %d", stats.ConstraintCount)
		}
		if stats.DependsOnCount != 2 {
			t.Errorf("expected 2 dependencies, got %d", stats.DependsOnCount)
		}
		if stats.HistoryEntryCount != 2 {
			t.Errorf("expected 2 history entries, got %d", stats.HistoryEntryCount)
		}
		if stats.AccessFrequency != 100 {
			t.Errorf("expected frequency 100, got %d", stats.AccessFrequency)
		}
	})

	t.Run("nonexistent node returns nil", func(t *testing.T) {
		stats := query.NodeStats("nonexistent")
		if stats != nil {
			t.Errorf("expected nil for nonexistent node, got %+v", stats)
		}
	})
}

func TestQueryAPI_ConcurrentAccess(t *testing.T) {
	c := setupTestCRS(t)
	snap := c.Snapshot()
	query := snap.Query()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = query.FindProvenNodes()
				_ = query.FindDisprovenNodes()
				_ = query.FindUnexploredNodes()
				_ = query.FindByProofRange(0, 100)
				_ = query.FindConstrainedNodes(ConstraintTypeUnknown)
				_ = query.FindDependencyChain("node-1", "node-4")
				_ = query.FindHotNodes(3)
				_ = query.NodeStats("node-1")
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
