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
	"sync"
	"testing"
	"time"
)

func TestDeltaHistoryWorker_Record(t *testing.T) {
	t.Run("records delta successfully", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Proof: 5, Status: ProofStatusExpanded},
		})

		w.Record(delta, 1, "test_source", "session1", nil)

		// Give worker time to process
		time.Sleep(10 * time.Millisecond)

		ctx := context.Background()
		size, err := w.Size(ctx)
		if err != nil {
			t.Fatalf("Size() error: %v", err)
		}
		if size != 1 {
			t.Errorf("Size() = %d, want 1", size)
		}
	})

	t.Run("nil delta is ignored", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		w.Record(nil, 1, "test_source", "session1", nil)

		time.Sleep(10 * time.Millisecond)

		ctx := context.Background()
		size, err := w.Size(ctx)
		if err != nil {
			t.Fatalf("Size() error: %v", err)
		}
		if size != 0 {
			t.Errorf("Size() = %d, want 0", size)
		}
	})

	t.Run("records multiple deltas in order", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		for i := 1; i <= 5; i++ {
			delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Proof: uint64(i), Status: ProofStatusExpanded},
			})
			w.Record(delta, int64(i), "test_source", "session1", nil)
		}

		time.Sleep(50 * time.Millisecond)

		records, err := w.All(ctx)
		if err != nil {
			t.Fatalf("All() error: %v", err)
		}
		if len(records) != 5 {
			t.Errorf("len(records) = %d, want 5", len(records))
		}

		// Verify order
		for i, r := range records {
			expectedGen := int64(i + 1)
			if r.Generation != expectedGen {
				t.Errorf("records[%d].Generation = %d, want %d", i, r.Generation, expectedGen)
			}
		}
	})
}

func TestDeltaHistoryWorker_GetRange(t *testing.T) {
	t.Run("returns deltas in range", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		// Record 10 deltas
		for i := 1; i <= 10; i++ {
			delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Proof: uint64(i)},
			})
			w.Record(delta, int64(i), "test", "sess", nil)
		}

		time.Sleep(50 * time.Millisecond)

		// Get range (exclusive start, inclusive end)
		records, err := w.GetRange(ctx, 3, 7)
		if err != nil {
			t.Fatalf("GetRange() error: %v", err)
		}

		// Should get generations 4, 5, 6, 7
		if len(records) != 4 {
			t.Errorf("len(records) = %d, want 4", len(records))
		}

		expectedGens := []int64{4, 5, 6, 7}
		for i, r := range records {
			if r.Generation != expectedGens[i] {
				t.Errorf("records[%d].Generation = %d, want %d", i, r.Generation, expectedGens[i])
			}
		}
	})

	t.Run("returns empty for non-existent range", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node1": {Proof: 1}})
		w.Record(delta, 1, "test", "sess", nil)

		time.Sleep(10 * time.Millisecond)

		records, err := w.GetRange(ctx, 100, 200)
		if err != nil {
			t.Fatalf("GetRange() error: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("len(records) = %d, want 0", len(records))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := w.GetRange(ctx, 0, 10)
		if err == nil {
			t.Error("expected context cancelled error")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		_, err := w.GetRange(nil, 0, 10)
		if err != ErrNilContext {
			t.Errorf("GetRange(nil, ...) error = %v, want ErrNilContext", err)
		}
	})
}

func TestDeltaHistoryWorker_GetByNode(t *testing.T) {
	t.Run("returns deltas affecting node", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		// Record deltas affecting different nodes
		delta1 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeA": {Proof: 1}})
		delta2 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeB": {Proof: 2}})
		delta3 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeA": {Proof: 3}})
		delta4 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeA": {Proof: 4}, "nodeB": {Proof: 5}})

		w.Record(delta1, 1, "test", "sess", nil)
		w.Record(delta2, 2, "test", "sess", nil)
		w.Record(delta3, 3, "test", "sess", nil)
		w.Record(delta4, 4, "test", "sess", nil)

		time.Sleep(50 * time.Millisecond)

		// Query for nodeA
		records, err := w.GetByNode(ctx, "nodeA")
		if err != nil {
			t.Fatalf("GetByNode() error: %v", err)
		}

		// Should get 3 records (generations 1, 3, 4)
		if len(records) != 3 {
			t.Errorf("len(records) = %d, want 3", len(records))
		}

		expectedGens := []int64{1, 3, 4}
		for i, r := range records {
			if r.Generation != expectedGens[i] {
				t.Errorf("records[%d].Generation = %d, want %d", i, r.Generation, expectedGens[i])
			}
		}
	})

	t.Run("returns empty for unknown node", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node1": {Proof: 1}})
		w.Record(delta, 1, "test", "sess", nil)

		time.Sleep(10 * time.Millisecond)

		records, err := w.GetByNode(ctx, "unknown_node")
		if err != nil {
			t.Fatalf("GetByNode() error: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("len(records) = %d, want 0", len(records))
		}
	})
}

func TestDeltaHistoryWorker_GetByGeneration(t *testing.T) {
	t.Run("returns delta for specific generation", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		delta1 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node1": {Proof: 1}})
		delta2 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node2": {Proof: 2}})
		delta3 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node3": {Proof: 3}})

		w.Record(delta1, 1, "source1", "sess", nil)
		w.Record(delta2, 2, "source2", "sess", nil)
		w.Record(delta3, 3, "source3", "sess", nil)

		time.Sleep(50 * time.Millisecond)

		record, found, err := w.GetByGeneration(ctx, 2)
		if err != nil {
			t.Fatalf("GetByGeneration() error: %v", err)
		}
		if !found {
			t.Error("GetByGeneration() found = false, want true")
		}
		if record.Generation != 2 {
			t.Errorf("record.Generation = %d, want 2", record.Generation)
		}
		if record.Source != "source2" {
			t.Errorf("record.Source = %q, want %q", record.Source, "source2")
		}
	})

	t.Run("returns false for non-existent generation", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node1": {Proof: 1}})
		w.Record(delta, 1, "test", "sess", nil)

		time.Sleep(10 * time.Millisecond)

		_, found, err := w.GetByGeneration(ctx, 999)
		if err != nil {
			t.Fatalf("GetByGeneration() error: %v", err)
		}
		if found {
			t.Error("GetByGeneration() found = true, want false")
		}
	})
}

func TestDeltaHistoryWorker_Explain(t *testing.T) {
	t.Run("returns causality chain for node", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		// Build a causality chain for node1
		delta1 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Proof: 1, Status: ProofStatusUnknown},
		})
		delta2 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Proof: 3, Status: ProofStatusExpanded},
		})
		delta3 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 0, Status: ProofStatusProven},
		})

		w.Record(delta1, 1, "initialization", "sess", nil)
		w.Record(delta2, 2, "heuristic_update", "sess", nil)
		w.Record(delta3, 3, "test_success", "sess", nil)

		time.Sleep(50 * time.Millisecond)

		records, err := w.Explain(ctx, "node1")
		if err != nil {
			t.Fatalf("Explain() error: %v", err)
		}

		if len(records) != 3 {
			t.Errorf("len(records) = %d, want 3", len(records))
		}

		// Verify chronological order
		expectedSources := []string{"initialization", "heuristic_update", "test_success"}
		for i, r := range records {
			if r.Source != expectedSources[i] {
				t.Errorf("records[%d].Source = %q, want %q", i, r.Source, expectedSources[i])
			}
		}
	})
}

func TestDeltaHistoryWorker_RingBuffer(t *testing.T) {
	t.Run("evicts oldest when at capacity", func(t *testing.T) {
		maxRecords := 5
		w := NewDeltaHistoryWorker(maxRecords, nil)
		defer w.Close()

		ctx := context.Background()

		// Record more than max
		for i := 1; i <= 10; i++ {
			delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Proof: uint64(i)},
			})
			w.Record(delta, int64(i), "test", "sess", nil)
		}

		time.Sleep(100 * time.Millisecond)

		size, err := w.Size(ctx)
		if err != nil {
			t.Fatalf("Size() error: %v", err)
		}
		if size != maxRecords {
			t.Errorf("Size() = %d, want %d", size, maxRecords)
		}

		// Should only have generations 6-10
		records, err := w.All(ctx)
		if err != nil {
			t.Fatalf("All() error: %v", err)
		}

		for _, r := range records {
			if r.Generation < 6 {
				t.Errorf("Found evicted generation %d, should have been removed", r.Generation)
			}
		}

		// Verify oldest generation (1) is not found
		_, found, err := w.GetByGeneration(ctx, 1)
		if err != nil {
			t.Fatalf("GetByGeneration() error: %v", err)
		}
		if found {
			t.Error("Generation 1 should have been evicted")
		}
	})

	t.Run("indices updated correctly after eviction", func(t *testing.T) {
		maxRecords := 3
		w := NewDeltaHistoryWorker(maxRecords, nil)
		defer w.Close()

		ctx := context.Background()

		// Record deltas for different nodes
		w.Record(NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeA": {Proof: 1}}), 1, "test", "sess", nil)
		w.Record(NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeB": {Proof: 2}}), 2, "test", "sess", nil)
		w.Record(NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeA": {Proof: 3}}), 3, "test", "sess", nil)
		// This should evict generation 1 (nodeA)
		w.Record(NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"nodeC": {Proof: 4}}), 4, "test", "sess", nil)

		time.Sleep(50 * time.Millisecond)

		// nodeA should now only have 1 record (generation 3)
		records, err := w.GetByNode(ctx, "nodeA")
		if err != nil {
			t.Fatalf("GetByNode() error: %v", err)
		}
		if len(records) != 1 {
			t.Errorf("len(records) for nodeA = %d, want 1", len(records))
		}
		if len(records) > 0 && records[0].Generation != 3 {
			t.Errorf("records[0].Generation = %d, want 3", records[0].Generation)
		}
	})
}

func TestDeltaHistoryWorker_Concurrent(t *testing.T) {
	t.Run("handles concurrent records without corruption", func(t *testing.T) {
		// Use a larger channel buffer to reduce drops
		w := NewDeltaHistoryWorker(1000, nil)
		defer w.Close()

		ctx := context.Background()
		var wg sync.WaitGroup
		numGoroutines := 5
		recordsPerGoroutine := 20

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < recordsPerGoroutine; j++ {
					gen := int64(id*recordsPerGoroutine + j)
					delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
						"node1": {Proof: uint64(gen)},
					})
					w.Record(delta, gen, "concurrent_test", "sess", nil)
					// Small delay to reduce channel contention
					time.Sleep(time.Millisecond)
				}
			}(i)
		}

		wg.Wait()
		time.Sleep(100 * time.Millisecond)

		size, err := w.Size(ctx)
		if err != nil {
			t.Fatalf("Size() error: %v", err)
		}

		// With the delays, most records should succeed
		// We expect at least 80% success rate
		expected := numGoroutines * recordsPerGoroutine
		minExpected := expected * 80 / 100
		if size < minExpected {
			t.Errorf("Size() = %d, want at least %d (80%% of %d)", size, minExpected, expected)
		}

		// Verify no corruption - all records should be valid
		records, err := w.All(ctx)
		if err != nil {
			t.Fatalf("All() error: %v", err)
		}

		for _, r := range records {
			if r.ID == "" {
				t.Error("Found record with empty ID - data corruption")
			}
			if r.DeltaType != DeltaTypeProof {
				t.Errorf("Found record with unexpected type %v", r.DeltaType)
			}
		}
	})

	t.Run("handles concurrent queries", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		// Pre-populate
		for i := 1; i <= 50; i++ {
			delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Proof: uint64(i)},
			})
			w.Record(delta, int64(i), "test", "sess", nil)
		}

		time.Sleep(50 * time.Millisecond)

		var wg sync.WaitGroup
		numQueries := 20

		for i := 0; i < numQueries; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := w.GetRange(ctx, 10, 20)
				if err != nil {
					t.Errorf("GetRange() error: %v", err)
				}
			}()
		}

		wg.Wait()
	})
}

func TestDeltaHistoryWorker_Close(t *testing.T) {
	t.Run("close is idempotent", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)

		// Multiple closes should not panic
		w.Close()
		w.Close()
		w.Close()
	})
}

func TestExtractAffectedNodes(t *testing.T) {
	tests := []struct {
		name     string
		delta    Delta
		expected []string
	}{
		{
			name: "ProofDelta",
			delta: NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
				"node1": {Proof: 1},
				"node2": {Proof: 2},
			}),
			expected: []string{"node1", "node2"},
		},
		{
			name: "ConstraintDelta with Add",
			delta: func() Delta {
				d := NewConstraintDelta(SignalSourceSoft)
				d.Add = []Constraint{
					{ID: "c1", Nodes: []string{"nodeA", "nodeB"}},
					{ID: "c2", Nodes: []string{"nodeC"}},
				}
				return d
			}(),
			expected: []string{"nodeA", "nodeB", "nodeC"},
		},
		{
			name: "SimilarityDelta",
			delta: func() Delta {
				d := NewSimilarityDelta(SignalSourceSoft)
				d.Updates = map[[2]string]float64{
					{"node1", "node2"}: 0.5,
					{"node3", "node4"}: 0.8,
				}
				return d
			}(),
			expected: []string{"node1", "node2", "node3", "node4"},
		},
		{
			name: "DependencyDelta",
			delta: func() Delta {
				d := NewDependencyDelta(SignalSourceSoft)
				d.AddEdges = [][2]string{{"from1", "to1"}}
				d.RemoveEdges = [][2]string{{"from2", "to2"}}
				return d
			}(),
			expected: []string{"from1", "to1", "from2", "to2"},
		},
		{
			name:     "nil delta",
			delta:    nil,
			expected: []string{},
		},
		{
			name: "CompositeDelta",
			delta: func() Delta {
				d1 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node1": {Proof: 1}})
				d2 := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{"node2": {Proof: 2}})
				return NewCompositeDelta(d1, d2)
			}(),
			expected: []string{"node1", "node2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAffectedNodes(tt.delta)

			// Convert to set for comparison (order doesn't matter)
			resultSet := make(map[string]bool)
			for _, n := range result {
				resultSet[n] = true
			}

			expectedSet := make(map[string]bool)
			for _, n := range tt.expected {
				expectedSet[n] = true
			}

			if len(resultSet) != len(expectedSet) {
				t.Errorf("extractAffectedNodes() returned %d nodes, want %d", len(resultSet), len(expectedSet))
			}

			for n := range expectedSet {
				if !resultSet[n] {
					t.Errorf("extractAffectedNodes() missing expected node %q", n)
				}
			}
		})
	}
}

func TestDeltaRecord_Metadata(t *testing.T) {
	t.Run("preserves metadata", func(t *testing.T) {
		w := NewDeltaHistoryWorker(100, nil)
		defer w.Close()

		ctx := context.Background()

		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Proof: 1},
		})

		metadata := map[string]string{
			"activity": "awareness",
			"reason":   "heuristic_update",
		}

		w.Record(delta, 1, "test_source", "session1", metadata)

		time.Sleep(10 * time.Millisecond)

		records, err := w.All(ctx)
		if err != nil {
			t.Fatalf("All() error: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("len(records) = %d, want 1", len(records))
		}

		r := records[0]
		if r.Metadata["activity"] != "awareness" {
			t.Errorf("Metadata[activity] = %q, want %q", r.Metadata["activity"], "awareness")
		}
		if r.Metadata["reason"] != "heuristic_update" {
			t.Errorf("Metadata[reason] = %q, want %q", r.Metadata["reason"], "heuristic_update")
		}
	})
}
