// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

func TestNewLSH(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewLSH(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "lsh" {
			t.Errorf("expected name lsh, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &LSHConfig{
			NumBands:    10,
			RowsPerBand: 5,
			Timeout:     3 * time.Second,
		}
		algo := NewLSH(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestLSH_Process(t *testing.T) {
	algo := NewLSH(nil)
	minhash := NewMinHash(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	// Helper to create signature
	createSig := func(set []string) *MinHashSignature {
		input := &MinHashInput{Operation: "signature", Set: set}
		result, _, _ := minhash.Process(ctx, snapshot, input)
		return result.(*MinHashOutput).Signature
	}

	t.Run("index single item", func(t *testing.T) {
		sig := createSig([]string{"a", "b", "c"})

		input := &LSHInput{
			Operation: "index",
			ID:        "item1",
			Signature: sig,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*LSHOutput)
		if output.Index == nil {
			t.Fatal("expected index")
		}
		if output.Index.ItemCount != 1 {
			t.Errorf("expected 1 item, got %d", output.Index.ItemCount)
		}
	})

	t.Run("query similar items", func(t *testing.T) {
		// Create index with multiple items
		var idx *LSHIndex

		sets := [][]string{
			{"a", "b", "c", "d"},
			{"a", "b", "c", "e"}, // Similar to first
			{"x", "y", "z", "w"}, // Different
		}

		for i, set := range sets {
			sig := createSig(set)
			input := &LSHInput{
				Operation: "index",
				ID:        string(rune('A' + i)),
				Signature: sig,
				Index:     idx,
			}
			result, _, _ := algo.Process(ctx, snapshot, input)
			idx = result.(*LSHOutput).Index
		}

		// Query with signature similar to first two
		querySig := createSig([]string{"a", "b", "c", "f"})
		queryInput := &LSHInput{
			Operation: "query",
			Signature: querySig,
			Index:     idx,
		}

		result, _, err := algo.Process(ctx, snapshot, queryInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*LSHOutput)
		// Should find candidates (likely A and B)
		if output.CandidateCount != len(output.Candidates) {
			t.Errorf("candidate count mismatch: %d vs %d", output.CandidateCount, len(output.Candidates))
		}
	})

	t.Run("query empty index", func(t *testing.T) {
		sig := createSig([]string{"a", "b"})
		input := &LSHInput{
			Operation: "query",
			Signature: sig,
			Index:     nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*LSHOutput)
		if output.CandidateCount != 0 {
			t.Errorf("expected 0 candidates, got %d", output.CandidateCount)
		}
	})

	t.Run("get all candidates", func(t *testing.T) {
		var idx *LSHIndex

		// Add two similar items
		sig1 := createSig([]string{"a", "b", "c"})
		input1 := &LSHInput{
			Operation: "index",
			ID:        "item1",
			Signature: sig1,
			Index:     idx,
		}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		idx = result1.(*LSHOutput).Index

		sig2 := createSig([]string{"a", "b", "c"}) // Same set
		input2 := &LSHInput{
			Operation: "index",
			ID:        "item2",
			Signature: sig2,
			Index:     idx,
		}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		idx = result2.(*LSHOutput).Index

		// Get candidates
		candInput := &LSHInput{
			Operation: "candidates",
			Index:     idx,
		}

		result, _, err := algo.Process(ctx, snapshot, candInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*LSHOutput)
		// Both items should be candidates since they share buckets
		if output.CandidateCount < 2 {
			t.Errorf("expected at least 2 candidates, got %d", output.CandidateCount)
		}
	})

	t.Run("index requires ID", func(t *testing.T) {
		sig := createSig([]string{"a"})
		input := &LSHInput{
			Operation: "index",
			ID:        "",
			Signature: sig,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for missing ID")
		}
	})

	t.Run("index requires signature", func(t *testing.T) {
		input := &LSHInput{
			Operation: "index",
			ID:        "item1",
			Signature: nil,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for missing signature")
		}
	})

	t.Run("query requires signature", func(t *testing.T) {
		input := &LSHInput{
			Operation: "query",
			Signature: nil,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for missing signature")
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
		cancel()

		sig := createSig([]string{"a"})
		input := &LSHInput{
			Operation: "index",
			ID:        "item1",
			Signature: sig,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})
}

func TestLSH_Properties(t *testing.T) {
	algo := NewLSH(nil)

	t.Run("candidate_count_matches property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "candidate_count_matches" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("candidate_count_matches property not found")
		}

		// Valid
		output := &LSHOutput{
			Candidates:     []string{"a", "b"},
			CandidateCount: 2,
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid
		outputBad := &LSHOutput{
			Candidates:     []string{"a", "b"},
			CandidateCount: 5,
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for mismatched count")
		}
	})
}

func TestLSH_Evaluable(t *testing.T) {
	algo := NewLSH(nil)

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
		algo := &LSH{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid bands", func(t *testing.T) {
		algo := NewLSH(&LSHConfig{NumBands: 0, RowsPerBand: 5})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero bands")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
