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

func TestNewL0Sampling(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewL0Sampling(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "l0_sampling" {
			t.Errorf("expected name l0_sampling, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &L0Config{
			NumLevels:  10,
			NumSamples: 5,
			Timeout:    3 * time.Second,
		}
		algo := NewL0Sampling(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestL0Sampling_Process(t *testing.T) {
	algo := NewL0Sampling(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("update single item", func(t *testing.T) {
		input := &L0Input{
			Operation: "update",
			Updates:   []L0Update{{Key: "foo", Delta: 5}},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.UpdatesProcessed != 1 {
			t.Errorf("expected 1 update processed, got %d", output.UpdatesProcessed)
		}
		if output.State == nil {
			t.Fatal("expected state")
		}
	})

	t.Run("update multiple items", func(t *testing.T) {
		input := &L0Input{
			Operation: "update",
			Updates: []L0Update{
				{Key: "foo", Delta: 3},
				{Key: "bar", Delta: 7},
				{Key: "baz", Delta: 2},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.UpdatesProcessed != 3 {
			t.Errorf("expected 3 updates processed, got %d", output.UpdatesProcessed)
		}
	})

	t.Run("positive and negative updates", func(t *testing.T) {
		input := &L0Input{
			Operation: "update",
			Updates: []L0Update{
				{Key: "foo", Delta: 10},
				{Key: "foo", Delta: -3}, // Decrement
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.UpdatesProcessed != 2 {
			t.Errorf("expected 2 updates processed, got %d", output.UpdatesProcessed)
		}
	})

	t.Run("sample from state", func(t *testing.T) {
		// First add some items
		updateInput := &L0Input{
			Operation: "update",
			Updates: []L0Update{
				{Key: "a", Delta: 5},
				{Key: "b", Delta: 10},
				{Key: "c", Delta: 3},
			},
		}
		updateResult, _, _ := algo.Process(ctx, snapshot, updateInput)
		state := updateResult.(*L0Output).State

		// Now sample
		sampleInput := &L0Input{
			Operation: "sample",
			State:     state,
		}

		result, _, err := algo.Process(ctx, snapshot, sampleInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.NonZeroEstimate < 0 {
			t.Errorf("expected non-negative estimate, got %d", output.NonZeroEstimate)
		}
	})

	t.Run("sample empty state", func(t *testing.T) {
		input := &L0Input{
			Operation: "sample",
			State:     nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.NonZeroEstimate != 0 {
			t.Errorf("expected 0 estimate, got %d", output.NonZeroEstimate)
		}
		if len(output.Samples) != 0 {
			t.Errorf("expected 0 samples, got %d", len(output.Samples))
		}
	})

	t.Run("merge states", func(t *testing.T) {
		// Create first state
		input1 := &L0Input{
			Operation: "update",
			Updates:   []L0Update{{Key: "a", Delta: 5}},
		}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		state1 := result1.(*L0Output).State

		// Create second state
		input2 := &L0Input{
			Operation: "update",
			Updates:   []L0Update{{Key: "b", Delta: 10}},
		}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		state2 := result2.(*L0Output).State

		// Merge
		mergeInput := &L0Input{
			Operation:  "merge",
			State:      state1,
			OtherState: state2,
		}

		result, _, err := algo.Process(ctx, snapshot, mergeInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.State == nil {
			t.Fatal("expected merged state")
		}
		if output.State.TotalUpdates != 2 {
			t.Errorf("expected 2 total updates, got %d", output.State.TotalUpdates)
		}
	})

	t.Run("merge with nil states", func(t *testing.T) {
		input := &L0Input{
			Operation:  "merge",
			State:      nil,
			OtherState: nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*L0Output)
		if output.State == nil {
			t.Fatal("expected new state")
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

		input := &L0Input{
			Operation: "update",
			Updates:   []L0Update{{Key: "foo", Delta: 1}},
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

func TestL0Sampling_Properties(t *testing.T) {
	algo := NewL0Sampling(nil)

	t.Run("samples_are_non_zero property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "samples_are_non_zero" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("samples_are_non_zero property not found")
		}

		// Valid
		output := &L0Output{
			Samples: []L0Sample{{Key: "a", Value: 5}},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: zero value sample
		outputBad := &L0Output{
			Samples: []L0Sample{{Key: "a", Value: 0}},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for zero-value sample")
		}
	})

	t.Run("estimate_non_negative property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "estimate_non_negative" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("estimate_non_negative property not found")
		}

		// Valid
		output := &L0Output{NonZeroEstimate: 100}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid (negative - though int64 makes this hard to test naturally)
		outputBad := &L0Output{NonZeroEstimate: -1}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for negative estimate")
		}
	})
}

func TestL0Sampling_Evaluable(t *testing.T) {
	algo := NewL0Sampling(nil)

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
		algo := &L0Sampling{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid levels", func(t *testing.T) {
		algo := NewL0Sampling(&L0Config{NumLevels: 0, NumSamples: 10})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero levels")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
