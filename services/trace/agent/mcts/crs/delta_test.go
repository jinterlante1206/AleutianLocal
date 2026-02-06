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
	"errors"
	"testing"
	"time"
)

func TestProofDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewProofDelta(SignalSourceHard, nil)
		if d.Type() != DeltaTypeProof {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeProof)
		}
	})

	t.Run("source", func(t *testing.T) {
		d := NewProofDelta(SignalSourceHard, nil)
		if d.Source() != SignalSourceHard {
			t.Errorf("Source = %v, want %v", d.Source(), SignalSourceHard)
		}
	})

	t.Run("timestamp", func(t *testing.T) {
		beforeMillis := time.Now().UnixMilli()
		d := NewProofDelta(SignalSourceHard, nil)
		afterMillis := time.Now().UnixMilli()

		if d.Timestamp() < beforeMillis || d.Timestamp() > afterMillis {
			t.Errorf("Timestamp %v not between %v and %v", d.Timestamp(), beforeMillis, afterMillis)
		}
	})

	t.Run("indexes affected", func(t *testing.T) {
		d := NewProofDelta(SignalSourceHard, nil)
		affected := d.IndexesAffected()
		if len(affected) != 1 || affected[0] != "proof" {
			t.Errorf("IndexesAffected = %v, want [proof]", affected)
		}
	})

	t.Run("validate hard signal disproven", func(t *testing.T) {
		d := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Status: ProofStatusDisproven},
		})
		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err != nil {
			t.Errorf("hard signal should allow disproven: %v", err)
		}
	})

	t.Run("validate soft signal disproven fails", func(t *testing.T) {
		d := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Status: ProofStatusDisproven},
		})
		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("soft signal should not allow disproven")
		}
		if !errors.Is(err, ErrHardSoftBoundaryViolation) {
			t.Errorf("error = %v, want ErrHardSoftBoundaryViolation", err)
		}
	})

	t.Run("merge same type", func(t *testing.T) {
		d1 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Proof: 1, Disproof: 1, UpdatedAt: time.Now().Add(-time.Hour).UnixMilli()},
		})
		d2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 2, Disproof: 2, UpdatedAt: time.Now().UnixMilli()},
			"node2": {Proof: 3, Disproof: 3},
		})

		merged, err := d1.Merge(d2)
		if err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		mp, ok := merged.(*ProofDelta)
		if !ok {
			t.Fatal("merged is not ProofDelta")
		}

		// node1 should have d2's values (later timestamp)
		if mp.Updates["node1"].Proof != 2 {
			t.Errorf("node1 Proof = %d, want 2", mp.Updates["node1"].Proof)
		}

		// node2 should be present
		if _, ok := mp.Updates["node2"]; !ok {
			t.Error("node2 should be in merged delta")
		}

		// Source should be hard (upgraded)
		if !mp.Source().IsHard() {
			t.Error("merged source should be hard")
		}
	})

	t.Run("conflicts with overlapping nodes", func(t *testing.T) {
		d1 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 1},
		})
		d2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 2},
		})

		if !d1.ConflictsWith(d2) {
			t.Error("deltas with same node should conflict")
		}
	})

	t.Run("no conflict with different nodes", func(t *testing.T) {
		d1 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 1},
		})
		d2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node2": {Proof: 2},
		})

		if d1.ConflictsWith(d2) {
			t.Error("deltas with different nodes should not conflict")
		}
	})
}

func TestConstraintDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewConstraintDelta(SignalSourceHard)
		if d.Type() != DeltaTypeConstraint {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeConstraint)
		}
	})

	t.Run("validate remove nonexistent fails", func(t *testing.T) {
		d := NewConstraintDelta(SignalSourceHard)
		d.Remove = []string{"nonexistent"}

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("removing nonexistent constraint should fail")
		}
	})

	t.Run("validate duplicate add fails", func(t *testing.T) {
		d := NewConstraintDelta(SignalSourceHard)
		d.Add = []Constraint{
			{ID: "c1", Type: ConstraintTypeMutualExclusion},
			{ID: "c1", Type: ConstraintTypeImplication},
		}

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("duplicate constraint ID should fail")
		}
	})

	t.Run("merge", func(t *testing.T) {
		d1 := NewConstraintDelta(SignalSourceSoft)
		d1.Add = []Constraint{{ID: "c1"}}
		d1.Remove = []string{"r1"}

		d2 := NewConstraintDelta(SignalSourceHard)
		d2.Add = []Constraint{{ID: "c2"}}
		d2.Remove = []string{"r2", "r1"} // duplicate r1

		merged, err := d1.Merge(d2)
		if err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		mc, ok := merged.(*ConstraintDelta)
		if !ok {
			t.Fatal("merged is not ConstraintDelta")
		}

		if len(mc.Add) != 2 {
			t.Errorf("Add length = %d, want 2", len(mc.Add))
		}

		// Remove should be deduplicated
		if len(mc.Remove) != 2 {
			t.Errorf("Remove length = %d, want 2 (deduplicated)", len(mc.Remove))
		}
	})
}

func TestDependencyDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewDependencyDelta(SignalSourceHard)
		if d.Type() != DeltaTypeDependency {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeDependency)
		}
	})

	t.Run("validate self-dependency fails", func(t *testing.T) {
		d := NewDependencyDelta(SignalSourceHard)
		d.AddEdges = [][2]string{{"node1", "node1"}}

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("self-dependency should fail")
		}
	})

	t.Run("validate cycle detection", func(t *testing.T) {
		d := NewDependencyDelta(SignalSourceHard)
		d.AddEdges = [][2]string{
			{"a", "b"},
			{"b", "c"},
			{"c", "a"}, // creates cycle
		}

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("cycle should be detected")
		}
	})

	t.Run("conflicts with add/remove same edge", func(t *testing.T) {
		d1 := NewDependencyDelta(SignalSourceHard)
		d1.AddEdges = [][2]string{{"a", "b"}}

		d2 := NewDependencyDelta(SignalSourceHard)
		d2.RemoveEdges = [][2]string{{"a", "b"}}

		if !d1.ConflictsWith(d2) {
			t.Error("add and remove same edge should conflict")
		}
	})
}

func TestHistoryDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewHistoryDelta(SignalSourceHard, nil)
		if d.Type() != DeltaTypeHistory {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeHistory)
		}
	})

	t.Run("validate empty ID fails", func(t *testing.T) {
		d := NewHistoryDelta(SignalSourceHard, []HistoryEntry{
			{ID: "", NodeID: "node1"},
		})

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("empty entry ID should fail")
		}
	})

	t.Run("never conflicts", func(t *testing.T) {
		d1 := NewHistoryDelta(SignalSourceHard, []HistoryEntry{{ID: "e1"}})
		d2 := NewHistoryDelta(SignalSourceHard, []HistoryEntry{{ID: "e1"}})

		if d1.ConflictsWith(d2) {
			t.Error("history deltas should never conflict")
		}
	})

	t.Run("merge", func(t *testing.T) {
		d1 := NewHistoryDelta(SignalSourceHard, []HistoryEntry{{ID: "e1"}})
		d2 := NewHistoryDelta(SignalSourceHard, []HistoryEntry{{ID: "e2"}})

		merged, err := d1.Merge(d2)
		if err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		mh, ok := merged.(*HistoryDelta)
		if !ok {
			t.Fatal("merged is not HistoryDelta")
		}

		if len(mh.Entries) != 2 {
			t.Errorf("Entries length = %d, want 2", len(mh.Entries))
		}
	})
}

func TestStreamingDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewStreamingDelta(SignalSourceHard)
		if d.Type() != DeltaTypeStreaming {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeStreaming)
		}
	})

	t.Run("validate always succeeds", func(t *testing.T) {
		d := NewStreamingDelta(SignalSourceHard)
		d.Increments["item"] = 100

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err != nil {
			t.Errorf("streaming validation should always succeed: %v", err)
		}
	})

	t.Run("never conflicts", func(t *testing.T) {
		d1 := NewStreamingDelta(SignalSourceHard)
		d1.Increments["item"] = 1

		d2 := NewStreamingDelta(SignalSourceHard)
		d2.Increments["item"] = 2

		if d1.ConflictsWith(d2) {
			t.Error("streaming deltas should never conflict")
		}
	})

	t.Run("merge sums increments", func(t *testing.T) {
		d1 := NewStreamingDelta(SignalSourceHard)
		d1.Increments["item1"] = 5
		d1.Increments["item2"] = 3

		d2 := NewStreamingDelta(SignalSourceHard)
		d2.Increments["item1"] = 10
		d2.Increments["item3"] = 7

		merged, err := d1.Merge(d2)
		if err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		ms, ok := merged.(*StreamingDelta)
		if !ok {
			t.Fatal("merged is not StreamingDelta")
		}

		if ms.Increments["item1"] != 15 {
			t.Errorf("item1 = %d, want 15", ms.Increments["item1"])
		}
		if ms.Increments["item2"] != 3 {
			t.Errorf("item2 = %d, want 3", ms.Increments["item2"])
		}
		if ms.Increments["item3"] != 7 {
			t.Errorf("item3 = %d, want 7", ms.Increments["item3"])
		}
	})
}

func TestSimilarityDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewSimilarityDelta(SignalSourceHard)
		if d.Type() != DeltaTypeSimilarity {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeSimilarity)
		}
	})

	t.Run("validate negative distance fails", func(t *testing.T) {
		d := NewSimilarityDelta(SignalSourceHard)
		d.Updates[[2]string{"a", "b"}] = -1.0

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("negative distance should fail")
		}
	})

	t.Run("validate self-similarity fails", func(t *testing.T) {
		d := NewSimilarityDelta(SignalSourceHard)
		d.Updates[[2]string{"a", "a"}] = 0.0

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("self-similarity should fail")
		}
	})

	t.Run("conflicts with same pair", func(t *testing.T) {
		d1 := NewSimilarityDelta(SignalSourceHard)
		d1.Updates[[2]string{"a", "b"}] = 1.0

		d2 := NewSimilarityDelta(SignalSourceHard)
		d2.Updates[[2]string{"a", "b"}] = 2.0

		if !d1.ConflictsWith(d2) {
			t.Error("same pair should conflict")
		}
	})

	t.Run("conflicts with reverse pair", func(t *testing.T) {
		d1 := NewSimilarityDelta(SignalSourceHard)
		d1.Updates[[2]string{"a", "b"}] = 1.0

		d2 := NewSimilarityDelta(SignalSourceHard)
		d2.Updates[[2]string{"b", "a"}] = 2.0

		if !d1.ConflictsWith(d2) {
			t.Error("reverse pair should conflict")
		}
	})
}

func TestCompositeDelta(t *testing.T) {
	t.Run("type", func(t *testing.T) {
		d := NewCompositeDelta()
		if d.Type() != DeltaTypeComposite {
			t.Errorf("Type = %v, want %v", d.Type(), DeltaTypeComposite)
		}
	})

	t.Run("source is hard if any is hard", func(t *testing.T) {
		d := NewCompositeDelta(
			NewProofDelta(SignalSourceSoft, nil),
			NewHistoryDelta(SignalSourceHard, nil),
		)
		if !d.Source().IsHard() {
			t.Error("composite should be hard if any child is hard")
		}
	})

	t.Run("validate checks all children", func(t *testing.T) {
		d := NewCompositeDelta(
			NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Status: ProofStatusDisproven}, // This should fail
			}),
			NewHistoryDelta(SignalSourceHard, []HistoryEntry{{ID: "e1"}}),
		)

		c := New(nil)
		snap := c.Snapshot()
		err := d.Validate(snap)
		if err == nil {
			t.Error("composite should fail if any child fails")
		}
	})

	t.Run("indexes affected collects all", func(t *testing.T) {
		d := NewCompositeDelta(
			NewProofDelta(SignalSourceHard, nil),
			NewHistoryDelta(SignalSourceHard, nil),
			NewStreamingDelta(SignalSourceHard),
		)

		affected := d.IndexesAffected()
		if len(affected) != 3 {
			t.Errorf("IndexesAffected length = %d, want 3", len(affected))
		}
	})

	t.Run("merge combines all deltas", func(t *testing.T) {
		d1 := NewCompositeDelta(
			NewProofDelta(SignalSourceHard, nil),
		)
		d2 := NewCompositeDelta(
			NewHistoryDelta(SignalSourceHard, nil),
		)

		merged, err := d1.Merge(d2)
		if err != nil {
			t.Fatalf("Merge failed: %v", err)
		}

		mc, ok := merged.(*CompositeDelta)
		if !ok {
			t.Fatal("merged is not CompositeDelta")
		}

		if len(mc.Deltas) != 2 {
			t.Errorf("Deltas length = %d, want 2", len(mc.Deltas))
		}
	})
}
