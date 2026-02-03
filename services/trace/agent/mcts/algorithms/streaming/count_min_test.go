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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewCountMin(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewCountMin(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "count_min" {
			t.Errorf("expected name count_min, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &CountMinConfig{
			Width:   512,
			Depth:   3,
			Timeout: 3 * time.Second,
		}
		algo := NewCountMin(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestCountMin_Process(t *testing.T) {
	algo := NewCountMin(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("add single item", func(t *testing.T) {
		input := &CountMinInput{
			Operation: "add",
			Items:     []CountMinItem{{Key: "foo", Count: 5}},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CountMinOutput)
		if output.ItemsProcessed != 1 {
			t.Errorf("expected 1 item processed, got %d", output.ItemsProcessed)
		}
		if output.TotalCount != 5 {
			t.Errorf("expected total count 5, got %d", output.TotalCount)
		}
	})

	t.Run("add multiple items", func(t *testing.T) {
		input := &CountMinInput{
			Operation: "add",
			Items: []CountMinItem{
				{Key: "foo", Count: 3},
				{Key: "bar", Count: 7},
				{Key: "foo", Count: 2}, // Duplicate key
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CountMinOutput)
		if output.ItemsProcessed != 3 {
			t.Errorf("expected 3 items processed, got %d", output.ItemsProcessed)
		}
		if output.TotalCount != 12 {
			t.Errorf("expected total count 12, got %d", output.TotalCount)
		}
	})

	t.Run("query frequencies", func(t *testing.T) {
		// First add items
		addInput := &CountMinInput{
			Operation: "add",
			Items: []CountMinItem{
				{Key: "foo", Count: 10},
				{Key: "bar", Count: 5},
			},
		}

		addResult, _, _ := algo.Process(ctx, snapshot, addInput)
		sketch := addResult.(*CountMinOutput).Sketch

		// Now query
		queryInput := &CountMinInput{
			Operation: "query",
			Queries:   []string{"foo", "bar", "baz"},
			Sketch:    sketch,
		}

		result, _, err := algo.Process(ctx, snapshot, queryInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CountMinOutput)
		if output.Frequencies["foo"] < 10 {
			t.Errorf("expected foo frequency >= 10, got %d", output.Frequencies["foo"])
		}
		if output.Frequencies["bar"] < 5 {
			t.Errorf("expected bar frequency >= 5, got %d", output.Frequencies["bar"])
		}
	})

	t.Run("query without sketch returns error", func(t *testing.T) {
		input := &CountMinInput{
			Operation: "query",
			Queries:   []string{"foo"},
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for query without sketch")
		}
	})

	t.Run("merge sketches", func(t *testing.T) {
		// Create first sketch
		input1 := &CountMinInput{
			Operation: "add",
			Items:     []CountMinItem{{Key: "foo", Count: 10}},
		}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		sketch1 := result1.(*CountMinOutput).Sketch

		// Merge with same dimensions
		mergeInput := &CountMinInput{
			Operation: "merge",
			Sketch:    sketch1,
		}

		result, _, err := algo.Process(ctx, snapshot, mergeInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CountMinOutput)
		if output.TotalCount != sketch1.Total {
			t.Errorf("expected total count %d, got %d", sketch1.Total, output.TotalCount)
		}
	})

	t.Run("returns error for invalid input type", func(t *testing.T) {
		_, _, err := algo.Process(ctx, snapshot, "invalid")
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})

	t.Run("returns error for unknown operation", func(t *testing.T) {
		input := &CountMinInput{
			Operation: "unknown",
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for unknown operation")
		}
	})

	t.Run("handles cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		input := &CountMinInput{
			Operation: "add",
			Items:     []CountMinItem{{Key: "foo", Count: 1}},
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

func TestCountMin_Properties(t *testing.T) {
	algo := NewCountMin(nil)

	t.Run("total_preserved property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "total_preserved" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("total_preserved property not found")
		}

		// Valid output
		output := &CountMinOutput{
			Sketch:     &CountMinSketch{Width: 10, Depth: 3, Table: make([][]int64, 3)},
			TotalCount: 100,
		}
		for i := range output.Sketch.Table {
			output.Sketch.Table[i] = make([]int64, 10)
		}

		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: negative count
		outputBad := &CountMinOutput{
			Sketch:     &CountMinSketch{Width: 10, Depth: 3},
			TotalCount: -1,
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for negative count")
		}
	})

	t.Run("sketch_dimensions_valid property", func(t *testing.T) {
		props := algo.Properties()
		var prop func(input, output any) error
		for _, p := range props {
			if p.Name == "sketch_dimensions_valid" {
				prop = p.Check
				break
			}
		}

		if prop == nil {
			t.Fatal("sketch_dimensions_valid property not found")
		}

		// Valid output
		output := &CountMinOutput{
			Sketch: &CountMinSketch{
				Width: 10,
				Depth: 3,
				Table: [][]int64{
					make([]int64, 10),
					make([]int64, 10),
					make([]int64, 10),
				},
			},
		}
		if err := prop(nil, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Invalid: wrong depth
		outputBad := &CountMinOutput{
			Sketch: &CountMinSketch{
				Width: 10,
				Depth: 3,
				Table: [][]int64{make([]int64, 10)}, // Only 1 row
			},
		}
		if err := prop(nil, outputBad); err == nil {
			t.Error("expected error for wrong depth")
		}
	})
}

func TestCountMin_Evaluable(t *testing.T) {
	algo := NewCountMin(nil)

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
		algo := &CountMin{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid width", func(t *testing.T) {
		algo := NewCountMin(&CountMinConfig{Width: 0, Depth: 3})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with zero width")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
