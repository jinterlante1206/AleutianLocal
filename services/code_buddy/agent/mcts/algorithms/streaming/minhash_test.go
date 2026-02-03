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

func TestNewMinHash(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewMinHash(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "minhash" {
			t.Errorf("expected name minhash, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &MinHashConfig{
			NumHashes: 64,
			Timeout:   3 * time.Second,
		}
		algo := NewMinHash(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestMinHash_Process(t *testing.T) {
	algo := NewMinHash(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("compute signature for set", func(t *testing.T) {
		input := &MinHashInput{
			Operation: "signature",
			Set:       []string{"a", "b", "c"},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*MinHashOutput)
		if output.ItemsProcessed != 3 {
			t.Errorf("expected 3 items processed, got %d", output.ItemsProcessed)
		}
		if output.Signature == nil {
			t.Fatal("expected signature")
		}
		if len(output.Signature.Values) != output.Signature.NumHashes {
			t.Errorf("expected %d values, got %d", output.Signature.NumHashes, len(output.Signature.Values))
		}
	})

	t.Run("identical sets have similarity 1", func(t *testing.T) {
		set := []string{"a", "b", "c", "d", "e"}

		input1 := &MinHashInput{Operation: "signature", Set: set}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sig1 := result1.(*MinHashOutput).Signature

		input2 := &MinHashInput{Operation: "signature", Set: set}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		sig2 := result2.(*MinHashOutput).Signature

		simInput := &MinHashInput{
			Operation:      "similarity",
			Signature:      sig1,
			OtherSignature: sig2,
		}

		result, _, err := algo.Process(ctx, snapshot, simInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*MinHashOutput)
		if output.Similarity != 1.0 {
			t.Errorf("expected similarity 1.0, got %f", output.Similarity)
		}
	})

	t.Run("disjoint sets have low similarity", func(t *testing.T) {
		input1 := &MinHashInput{Operation: "signature", Set: []string{"a", "b", "c"}}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sig1 := result1.(*MinHashOutput).Signature

		input2 := &MinHashInput{Operation: "signature", Set: []string{"x", "y", "z"}}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		sig2 := result2.(*MinHashOutput).Signature

		simInput := &MinHashInput{
			Operation:      "similarity",
			Signature:      sig1,
			OtherSignature: sig2,
		}

		result, _, err := algo.Process(ctx, snapshot, simInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*MinHashOutput)
		// Should be very low (close to 0)
		if output.Similarity > 0.3 {
			t.Errorf("expected low similarity, got %f", output.Similarity)
		}
	})

	t.Run("overlapping sets have intermediate similarity", func(t *testing.T) {
		input1 := &MinHashInput{Operation: "signature", Set: []string{"a", "b", "c", "d"}}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sig1 := result1.(*MinHashOutput).Signature

		input2 := &MinHashInput{Operation: "signature", Set: []string{"c", "d", "e", "f"}}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		sig2 := result2.(*MinHashOutput).Signature

		simInput := &MinHashInput{
			Operation:      "similarity",
			Signature:      sig1,
			OtherSignature: sig2,
		}

		result, _, err := algo.Process(ctx, snapshot, simInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*MinHashOutput)
		// Jaccard = |{c,d}| / |{a,b,c,d,e,f}| = 2/6 = 0.333
		if output.Similarity < 0.1 || output.Similarity > 0.6 {
			t.Errorf("expected similarity around 0.33, got %f", output.Similarity)
		}
	})

	t.Run("similarity requires both signatures", func(t *testing.T) {
		input := &MinHashInput{
			Operation: "similarity",
			Signature: nil,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for missing signatures")
		}
	})

	t.Run("merge signatures", func(t *testing.T) {
		input1 := &MinHashInput{Operation: "signature", Set: []string{"a", "b"}}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sig1 := result1.(*MinHashOutput).Signature

		input2 := &MinHashInput{Operation: "signature", Set: []string{"c", "d"}}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		sig2 := result2.(*MinHashOutput).Signature

		mergeInput := &MinHashInput{
			Operation:      "merge",
			Signature:      sig1,
			OtherSignature: sig2,
		}

		result, _, err := algo.Process(ctx, snapshot, mergeInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*MinHashOutput)
		if output.Signature == nil {
			t.Fatal("expected merged signature")
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

		input := &MinHashInput{
			Operation: "signature",
			Set:       []string{"a", "b", "c"},
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

func TestMinHash_Properties(t *testing.T) {
	algo := NewMinHash(nil)

	t.Run("similarity_bounded property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "similarity_bounded" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("similarity_bounded property not found")
		}

		// Valid similarity
		output := &MinHashOutput{Similarity: 0.5}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: > 1
		outputBad := &MinHashOutput{Similarity: 1.5}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for similarity > 1")
		}

		// Invalid: < 0
		outputBad2 := &MinHashOutput{Similarity: -0.5}
		if err := prop(nil, outputBad2); err == nil {
			t.Error("expected error for similarity < 0")
		}
	})
}

func TestMinHash_Evaluable(t *testing.T) {
	algo := NewMinHash(nil)

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
		algo := &MinHash{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid num hashes", func(t *testing.T) {
		algo := NewMinHash(&MinHashConfig{NumHashes: 0})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero num hashes")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
