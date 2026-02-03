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
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		c := New(nil)
		if c == nil {
			t.Fatal("New returned nil")
		}
		if c.Generation() != 0 {
			t.Errorf("initial generation = %d, want 0", c.Generation())
		}
	})

	t.Run("custom config", func(t *testing.T) {
		config := &Config{
			SnapshotEpochLimit: 500,
			EnableMetrics:      false,
		}
		c := New(config)
		if c == nil {
			t.Fatal("New returned nil")
		}
	})
}

func TestCRS_Snapshot(t *testing.T) {
	t.Run("returns non-nil snapshot", func(t *testing.T) {
		c := New(nil)
		snap := c.Snapshot()
		if snap == nil {
			t.Fatal("Snapshot returned nil")
		}
	})

	t.Run("snapshot has correct generation", func(t *testing.T) {
		c := New(nil)
		snap := c.Snapshot()
		if snap.Generation() != 0 {
			t.Errorf("generation = %d, want 0", snap.Generation())
		}
	})

	t.Run("snapshot has creation time", func(t *testing.T) {
		before := time.Now()
		c := New(nil)
		snap := c.Snapshot()
		after := time.Now()

		if snap.CreatedAt().Before(before) || snap.CreatedAt().After(after) {
			t.Errorf("creation time %v not between %v and %v", snap.CreatedAt(), before, after)
		}
	})

	t.Run("all indexes accessible", func(t *testing.T) {
		c := New(nil)
		snap := c.Snapshot()

		if snap.ProofIndex() == nil {
			t.Error("ProofIndex is nil")
		}
		if snap.ConstraintIndex() == nil {
			t.Error("ConstraintIndex is nil")
		}
		if snap.SimilarityIndex() == nil {
			t.Error("SimilarityIndex is nil")
		}
		if snap.DependencyIndex() == nil {
			t.Error("DependencyIndex is nil")
		}
		if snap.HistoryIndex() == nil {
			t.Error("HistoryIndex is nil")
		}
		if snap.StreamingIndex() == nil {
			t.Error("StreamingIndex is nil")
		}
	})
}

func TestCRS_Apply(t *testing.T) {
	t.Run("nil context returns error", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceHard, nil)
		_, err := c.Apply(nil, delta) //nolint:staticcheck
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("error = %v, want %v", err, ErrNilContext)
		}
	})

	t.Run("nil delta returns error", func(t *testing.T) {
		c := New(nil)
		_, err := c.Apply(context.Background(), nil)
		if !errors.Is(err, ErrNilDelta) {
			t.Errorf("error = %v, want %v", err, ErrNilDelta)
		}
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		c := New(nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		delta := NewProofDelta(SignalSourceHard, nil)
		_, err := c.Apply(ctx, delta)
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})

	t.Run("increments generation on success", func(t *testing.T) {
		c := New(nil)
		before := c.Generation()

		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 1, Disproof: 2, Status: ProofStatusExpanded},
		})
		metrics, err := c.Apply(context.Background(), delta)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		after := c.Generation()
		if after != before+1 {
			t.Errorf("generation = %d, want %d", after, before+1)
		}
		if metrics.OldGeneration != before {
			t.Errorf("OldGeneration = %d, want %d", metrics.OldGeneration, before)
		}
		if metrics.NewGeneration != after {
			t.Errorf("NewGeneration = %d, want %d", metrics.NewGeneration, after)
		}
	})

	t.Run("proof delta updates proof index", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 10, Disproof: 20, Status: ProofStatusExpanded},
		})
		_, err := c.Apply(context.Background(), delta)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		snap := c.Snapshot()
		proof, ok := snap.ProofIndex().Get("node1")
		if !ok {
			t.Fatal("node1 not found in proof index")
		}
		if proof.Proof != 10 || proof.Disproof != 20 {
			t.Errorf("proof = %v, want Proof=10, Disproof=20", proof)
		}
	})

	t.Run("returns metrics", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 1, Disproof: 1},
			"node2": {Proof: 2, Disproof: 2},
		})
		metrics, err := c.Apply(context.Background(), delta)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		if metrics.DeltaType != DeltaTypeProof {
			t.Errorf("DeltaType = %v, want %v", metrics.DeltaType, DeltaTypeProof)
		}
		if metrics.EntriesModified != 2 {
			t.Errorf("EntriesModified = %d, want 2", metrics.EntriesModified)
		}
		if len(metrics.IndexesUpdated) == 0 {
			t.Error("IndexesUpdated is empty")
		}
		if metrics.ApplyDuration <= 0 {
			t.Error("ApplyDuration should be positive")
		}
	})
}

func TestCRS_Apply_HardSoftBoundary(t *testing.T) {
	t.Run("hard signal can mark disproven", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Status: ProofStatusDisproven, Source: SignalSourceHard},
		})
		_, err := c.Apply(context.Background(), delta)
		if err != nil {
			t.Errorf("hard signal should be able to mark disproven: %v", err)
		}
	})

	t.Run("soft signal cannot mark disproven", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Status: ProofStatusDisproven, Source: SignalSourceSoft},
		})
		_, err := c.Apply(context.Background(), delta)
		if err == nil {
			t.Error("soft signal should not be able to mark disproven")
		}
		if !errors.Is(err, ErrDeltaValidation) {
			t.Errorf("error = %v, want ErrDeltaValidation", err)
		}
	})

	t.Run("soft signal can mark proven", func(t *testing.T) {
		c := New(nil)
		delta := NewProofDelta(SignalSourceSoft, map[string]ProofNumber{
			"node1": {Status: ProofStatusProven, Source: SignalSourceSoft},
		})
		_, err := c.Apply(context.Background(), delta)
		if err != nil {
			t.Errorf("soft signal should be able to mark proven: %v", err)
		}
	})
}

func TestCRS_Checkpoint(t *testing.T) {
	t.Run("creates checkpoint", func(t *testing.T) {
		c := New(nil)

		// Add some data
		delta := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 10, Disproof: 20},
		})
		_, _ = c.Apply(context.Background(), delta)

		cp, err := c.Checkpoint(context.Background())
		if err != nil {
			t.Fatalf("Checkpoint failed: %v", err)
		}

		if cp.ID == "" {
			t.Error("checkpoint ID is empty")
		}
		if cp.Generation != 1 {
			t.Errorf("checkpoint generation = %d, want 1", cp.Generation)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := New(nil)
		_, err := c.Checkpoint(nil) //nolint:staticcheck
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("error = %v, want %v", err, ErrNilContext)
		}
	})
}

func TestCRS_Restore(t *testing.T) {
	t.Run("restores state", func(t *testing.T) {
		c := New(nil)
		ctx := context.Background()

		// Add initial data
		delta1 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node1": {Proof: 10, Disproof: 20},
		})
		_, _ = c.Apply(ctx, delta1)

		// Create checkpoint
		cp, _ := c.Checkpoint(ctx)

		// Add more data
		delta2 := NewProofDelta(SignalSourceHard, map[string]ProofNumber{
			"node2": {Proof: 30, Disproof: 40},
		})
		_, _ = c.Apply(ctx, delta2)

		// Verify node2 exists
		snap := c.Snapshot()
		if _, ok := snap.ProofIndex().Get("node2"); !ok {
			t.Fatal("node2 should exist before restore")
		}

		// Restore
		err := c.Restore(ctx, cp)
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}

		// Verify state is restored
		snap = c.Snapshot()
		if _, ok := snap.ProofIndex().Get("node2"); ok {
			t.Error("node2 should not exist after restore")
		}
		if _, ok := snap.ProofIndex().Get("node1"); !ok {
			t.Error("node1 should still exist after restore")
		}
		if c.Generation() != cp.Generation {
			t.Errorf("generation = %d, want %d", c.Generation(), cp.Generation)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := New(nil)
		cp, _ := c.Checkpoint(context.Background())
		err := c.Restore(nil, cp) //nolint:staticcheck
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("error = %v, want %v", err, ErrNilContext)
		}
	})
}

func TestCRS_Concurrent(t *testing.T) {
	t.Run("concurrent snapshots", func(t *testing.T) {
		c := New(nil)
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				snap := c.Snapshot()
				if snap == nil {
					t.Error("snapshot is nil")
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent applies", func(t *testing.T) {
		c := New(nil)
		ctx := context.Background()
		var wg sync.WaitGroup
		errCh := make(chan error, 100)

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				delta := NewHistoryDelta(SignalSourceHard, []HistoryEntry{
					{ID: "entry-" + string(rune(n)), NodeID: "node1", Action: "test"},
				})
				_, err := c.Apply(ctx, delta)
				if err != nil {
					errCh <- err
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("concurrent apply failed: %v", err)
		}

		// Verify generation increased correctly
		if c.Generation() != 100 {
			t.Errorf("generation = %d, want 100", c.Generation())
		}
	})

	t.Run("concurrent reads and writes", func(t *testing.T) {
		c := New(nil)
		ctx := context.Background()
		var wg sync.WaitGroup

		// Writers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				delta := NewStreamingDelta(SignalSourceHard)
				delta.Increments["item"] = 1
				_, _ = c.Apply(ctx, delta)
			}(i)
		}

		// Readers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				snap := c.Snapshot()
				_ = snap.StreamingIndex().Estimate("item")
			}()
		}

		wg.Wait()
	})
}

func TestCRS_HealthCheck(t *testing.T) {
	t.Run("healthy state", func(t *testing.T) {
		c := New(nil)
		err := c.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("HealthCheck failed: %v", err)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := New(nil)
		err := c.HealthCheck(nil) //nolint:staticcheck
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("error = %v, want %v", err, ErrNilContext)
		}
	})
}

func TestCRS_Evaluable(t *testing.T) {
	t.Run("implements Evaluable", func(t *testing.T) {
		c := New(nil)

		if c.Name() != "crs" {
			t.Errorf("Name = %s, want crs", c.Name())
		}

		props := c.Properties()
		if len(props) == 0 {
			t.Error("Properties should not be empty")
		}

		metrics := c.Metrics()
		if len(metrics) == 0 {
			t.Error("Metrics should not be empty")
		}
	})
}
