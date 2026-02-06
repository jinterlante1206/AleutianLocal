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

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

func TestSnapshot_Immutability(t *testing.T) {
	t.Run("snapshot does not change after creation", func(t *testing.T) {
		c := New(nil)
		ctx := context.Background()

		// Create initial data
		delta1 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 10, Disproof: 20},
		})
		_, _ = c.Apply(ctx, delta1)

		// Take snapshot
		snap := c.Snapshot()
		gen1 := snap.Generation()

		// Verify initial state
		proof1, ok := snap.ProofIndex().Get("node1")
		if !ok || proof1.Proof != 10 {
			t.Fatal("initial state incorrect")
		}

		// Modify CRS
		delta2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 100, Disproof: 200},
			"node2": {Proof: 30, Disproof: 40},
		})
		_, _ = c.Apply(ctx, delta2)

		// Snapshot should still show old values
		proof1After, ok := snap.ProofIndex().Get("node1")
		if !ok || proof1After.Proof != 10 {
			t.Errorf("snapshot changed: Proof = %d, want 10", proof1After.Proof)
		}

		// node2 should not exist in old snapshot
		if _, ok := snap.ProofIndex().Get("node2"); ok {
			t.Error("node2 should not exist in old snapshot")
		}

		// Generation should not change
		if snap.Generation() != gen1 {
			t.Errorf("generation changed: %d -> %d", gen1, snap.Generation())
		}
	})

	t.Run("multiple snapshots are independent", func(t *testing.T) {
		c := New(nil)
		ctx := context.Background()

		// Create data and take first snapshot
		delta1 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 1},
		})
		_, _ = c.Apply(ctx, delta1)
		snap1 := c.Snapshot()

		// Create more data and take second snapshot
		delta2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node2": {Proof: 2},
		})
		_, _ = c.Apply(ctx, delta2)
		snap2 := c.Snapshot()

		// snap1 should only have node1
		if snap1.ProofIndex().Size() != 1 {
			t.Errorf("snap1 size = %d, want 1", snap1.ProofIndex().Size())
		}

		// snap2 should have both
		if snap2.ProofIndex().Size() != 2 {
			t.Errorf("snap2 size = %d, want 2", snap2.ProofIndex().Size())
		}
	})
}

func TestProofIndexView(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
		"node1": {Proof: 10, Disproof: 20, Status: ProofStatusExpanded},
		"node2": {Proof: 30, Disproof: 40, Status: ProofStatusProven},
	})
	_, _ = c.Apply(ctx, delta)

	snap := c.Snapshot()
	pv := snap.ProofIndex()

	t.Run("Get existing", func(t *testing.T) {
		proof, ok := pv.Get("node1")
		if !ok {
			t.Fatal("node1 not found")
		}
		if proof.Proof != 10 || proof.Disproof != 20 {
			t.Errorf("proof = %v, want Proof=10, Disproof=20", proof)
		}
	})

	t.Run("Get nonexistent", func(t *testing.T) {
		_, ok := pv.Get("nonexistent")
		if ok {
			t.Error("nonexistent node should not be found")
		}
	})

	t.Run("All returns copy", func(t *testing.T) {
		all := pv.All()
		if len(all) != 2 {
			t.Errorf("All length = %d, want 2", len(all))
		}

		// Modifying returned map should not affect index
		all["node3"] = ProofNumber{Proof: 99}
		_, ok := pv.Get("node3")
		if ok {
			t.Error("modifying All() result should not affect index")
		}
	})

	t.Run("Size", func(t *testing.T) {
		if pv.Size() != 2 {
			t.Errorf("Size = %d, want 2", pv.Size())
		}
	})
}

func TestConstraintIndexView(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	delta := NewConstraintDelta(SignalSourceHard)
	delta.Add = []Constraint{
		{ID: "c1", Type: ConstraintTypeMutualExclusion, Nodes: []string{"n1", "n2"}},
		{ID: "c2", Type: ConstraintTypeImplication, Nodes: []string{"n2", "n3"}},
		{ID: "c3", Type: ConstraintTypeMutualExclusion, Nodes: []string{"n3", "n4"}},
	}
	_, _ = c.Apply(ctx, delta)

	snap := c.Snapshot()
	cv := snap.ConstraintIndex()

	t.Run("Get", func(t *testing.T) {
		constraint, ok := cv.Get("c1")
		if !ok {
			t.Fatal("c1 not found")
		}
		if constraint.Type != ConstraintTypeMutualExclusion {
			t.Errorf("Type = %v, want MutualExclusion", constraint.Type)
		}
	})

	t.Run("FindByType", func(t *testing.T) {
		exclusions := cv.FindByType(ConstraintTypeMutualExclusion)
		if len(exclusions) != 2 {
			t.Errorf("found %d mutual exclusions, want 2", len(exclusions))
		}
	})

	t.Run("FindByNode", func(t *testing.T) {
		n2Constraints := cv.FindByNode("n2")
		if len(n2Constraints) != 2 {
			t.Errorf("found %d constraints for n2, want 2", len(n2Constraints))
		}
	})

	t.Run("Size", func(t *testing.T) {
		if cv.Size() != 3 {
			t.Errorf("Size = %d, want 3", cv.Size())
		}
	})
}

func TestSimilarityIndexView(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	delta := NewSimilarityDelta(SignalSourceHard)
	delta.Updates[[2]string{"a", "b"}] = 0.1
	delta.Updates[[2]string{"a", "c"}] = 0.5
	delta.Updates[[2]string{"a", "d"}] = 0.3
	_, _ = c.Apply(ctx, delta)

	snap := c.Snapshot()
	sv := snap.SimilarityIndex()

	t.Run("Distance forward", func(t *testing.T) {
		dist, ok := sv.Distance("a", "b")
		if !ok {
			t.Fatal("distance not found")
		}
		if dist != 0.1 {
			t.Errorf("distance = %f, want 0.1", dist)
		}
	})

	t.Run("Distance reverse", func(t *testing.T) {
		dist, ok := sv.Distance("b", "a")
		if !ok {
			t.Fatal("reverse distance not found")
		}
		if dist != 0.1 {
			t.Errorf("distance = %f, want 0.1", dist)
		}
	})

	t.Run("Distance nonexistent", func(t *testing.T) {
		_, ok := sv.Distance("x", "y")
		if ok {
			t.Error("nonexistent distance should not be found")
		}
	})

	t.Run("NearestNeighbors", func(t *testing.T) {
		neighbors := sv.NearestNeighbors("a", 2)
		if len(neighbors) != 2 {
			t.Fatalf("found %d neighbors, want 2", len(neighbors))
		}

		// Should be sorted by distance
		if neighbors[0].Distance > neighbors[1].Distance {
			t.Error("neighbors not sorted by distance")
		}

		// First should be b (0.1)
		if neighbors[0].NodeID != "b" || neighbors[0].Distance != 0.1 {
			t.Errorf("first neighbor = %v, want b with distance 0.1", neighbors[0])
		}
	})

	t.Run("Size", func(t *testing.T) {
		if sv.Size() != 3 {
			t.Errorf("Size = %d, want 3", sv.Size())
		}
	})
}

func TestDependencyIndexView(t *testing.T) {
	// GR-32: Test with graph-backed dependency index
	// Set up mock graph with call relationships:
	//   a -> b, a -> c, b -> d
	// Uses mockGraphQuery from graph_dependency_index_test.go
	mock := newMockGraphQuery()
	mock.callees["a"] = []*ast.Symbol{{ID: "b"}, {ID: "c"}}
	mock.callees["b"] = []*ast.Symbol{{ID: "d"}}
	mock.callers["b"] = []*ast.Symbol{{ID: "a"}}
	mock.callers["c"] = []*ast.Symbol{{ID: "a"}}
	mock.callers["d"] = []*ast.Symbol{{ID: "b"}}
	mock.hasCycle["a"] = false
	mock.callEdgeCount = 3

	c := New(nil)
	c.SetGraphProvider(mock)

	snap := c.Snapshot()
	dv := snap.DependencyIndex()

	t.Run("DependsOn", func(t *testing.T) {
		deps := dv.DependsOn("a")
		if len(deps) != 2 {
			t.Errorf("a depends on %d nodes, want 2", len(deps))
		}
	})

	t.Run("DependedBy", func(t *testing.T) {
		deps := dv.DependedBy("b")
		if len(deps) != 1 {
			t.Errorf("b depended by %d nodes, want 1", len(deps))
		}
		if len(deps) > 0 && deps[0] != "a" {
			t.Errorf("b depended by %s, want a", deps[0])
		}
	})

	t.Run("HasCycle false", func(t *testing.T) {
		if dv.HasCycle("a") {
			t.Error("no cycle should be detected")
		}
	})

	t.Run("Size", func(t *testing.T) {
		if dv.Size() != 3 {
			t.Errorf("Size = %d, want 3", dv.Size())
		}
	})
}

func TestHistoryIndexView(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	now := time.Now()
	delta := NewHistoryDelta(SignalSourceHard, []HistoryEntry{
		{ID: "e1", NodeID: "node1", Action: "expand", Timestamp: now.Add(-2 * time.Hour).UnixMilli()},
		{ID: "e2", NodeID: "node2", Action: "select", Timestamp: now.Add(-1 * time.Hour).UnixMilli()},
		{ID: "e3", NodeID: "node1", Action: "backprop", Timestamp: now.UnixMilli()},
	})
	_, _ = c.Apply(ctx, delta)

	snap := c.Snapshot()
	hv := snap.HistoryIndex()

	t.Run("Trace", func(t *testing.T) {
		trace := hv.Trace("node1")
		if len(trace) != 2 {
			t.Errorf("node1 trace length = %d, want 2", len(trace))
		}
	})

	t.Run("Recent", func(t *testing.T) {
		recent := hv.Recent(2)
		if len(recent) != 2 {
			t.Errorf("recent length = %d, want 2", len(recent))
		}
	})

	t.Run("Recent more than total", func(t *testing.T) {
		recent := hv.Recent(10)
		if len(recent) != 3 {
			t.Errorf("recent length = %d, want 3 (all)", len(recent))
		}
	})

	t.Run("Size", func(t *testing.T) {
		if hv.Size() != 3 {
			t.Errorf("Size = %d, want 3", hv.Size())
		}
	})
}

func TestStreamingIndexView(t *testing.T) {
	c := New(nil)
	ctx := context.Background()

	delta := NewStreamingDelta(SignalSourceHard)
	delta.Increments["item1"] = 5
	delta.Increments["item2"] = 10
	delta.CardinalityItems = []string{"unique1", "unique2", "unique3"}
	_, _ = c.Apply(ctx, delta)

	snap := c.Snapshot()
	sv := snap.StreamingIndex()

	t.Run("Estimate", func(t *testing.T) {
		est := sv.Estimate("item1")
		if est != 5 {
			t.Errorf("item1 estimate = %d, want 5", est)
		}
	})

	t.Run("Estimate nonexistent", func(t *testing.T) {
		est := sv.Estimate("nonexistent")
		if est != 0 {
			t.Errorf("nonexistent estimate = %d, want 0", est)
		}
	})

	t.Run("Cardinality", func(t *testing.T) {
		card := sv.Cardinality()
		// 5 unique items: item1, item2, unique1, unique2, unique3
		if card != 5 {
			t.Errorf("cardinality = %d, want 5", card)
		}
	})

	t.Run("Size", func(t *testing.T) {
		size := sv.Size()
		if size <= 0 {
			t.Error("size should be positive")
		}
	})
}

func TestDependencyGraph_Cycle(t *testing.T) {
	t.Run("direct cycle", func(t *testing.T) {
		g := newDependencyGraph()
		g.addEdge("a", "b")
		g.addEdge("b", "a")

		if !g.hasCycle("a") {
			t.Error("direct cycle should be detected")
		}
	})

	t.Run("indirect cycle", func(t *testing.T) {
		g := newDependencyGraph()
		g.addEdge("a", "b")
		g.addEdge("b", "c")
		g.addEdge("c", "a")

		if !g.hasCycle("a") {
			t.Error("indirect cycle should be detected")
		}
	})

	t.Run("no cycle", func(t *testing.T) {
		g := newDependencyGraph()
		g.addEdge("a", "b")
		g.addEdge("b", "c")
		g.addEdge("a", "c")

		if g.hasCycle("a") {
			t.Error("no cycle exists")
		}
	})
}
