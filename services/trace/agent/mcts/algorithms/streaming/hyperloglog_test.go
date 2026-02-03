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
	"fmt"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewHyperLogLog(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewHyperLogLog(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "hyperloglog" {
			t.Errorf("expected name hyperloglog, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &HyperLogLogConfig{
			Precision: 10,
			Timeout:   3 * time.Second,
		}
		algo := NewHyperLogLog(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestHyperLogLog_Process(t *testing.T) {
	algo := NewHyperLogLog(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("add single item", func(t *testing.T) {
		input := &HyperLogLogInput{
			Operation: "add",
			Items:     []string{"foo"},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		if output.ItemsProcessed != 1 {
			t.Errorf("expected 1 item processed, got %d", output.ItemsProcessed)
		}
		if output.Cardinality == 0 {
			t.Error("expected non-zero cardinality")
		}
	})

	t.Run("add many distinct items", func(t *testing.T) {
		items := make([]string, 1000)
		for i := range items {
			items[i] = fmt.Sprintf("item_%d", i)
		}

		input := &HyperLogLogInput{
			Operation: "add",
			Items:     items,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		if output.ItemsProcessed != 1000 {
			t.Errorf("expected 1000 items processed, got %d", output.ItemsProcessed)
		}

		// HLL should estimate close to 1000 (within error bounds)
		estimate := output.Cardinality
		errorMargin := float64(1000) * output.StandardError * 3 // 3 standard deviations
		if float64(estimate) < 1000-errorMargin || float64(estimate) > 1000+errorMargin {
			t.Errorf("expected cardinality close to 1000, got %d", estimate)
		}
	})

	t.Run("add duplicates", func(t *testing.T) {
		items := make([]string, 100)
		for i := range items {
			items[i] = "same_item" // All duplicates
		}

		input := &HyperLogLogInput{
			Operation: "add",
			Items:     items,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		// Should estimate close to 1
		if output.Cardinality > 10 {
			t.Errorf("expected cardinality close to 1, got %d", output.Cardinality)
		}
	})

	t.Run("count empty HLL", func(t *testing.T) {
		input := &HyperLogLogInput{
			Operation: "count",
			HLL:       nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		if output.Cardinality != 0 {
			t.Errorf("expected cardinality 0, got %d", output.Cardinality)
		}
	})

	t.Run("merge two HLLs", func(t *testing.T) {
		// Create first HLL
		input1 := &HyperLogLogInput{
			Operation: "add",
			Items:     []string{"a", "b", "c"},
		}
		result1, _, _ := algo.Process(ctx, snapshot, input1)
		hll1 := result1.(*HyperLogLogOutput).HLL

		// Create second HLL
		input2 := &HyperLogLogInput{
			Operation: "add",
			Items:     []string{"d", "e", "f"},
		}
		result2, _, _ := algo.Process(ctx, snapshot, input2)
		hll2 := result2.(*HyperLogLogOutput).HLL

		// Merge
		mergeInput := &HyperLogLogInput{
			Operation: "merge",
			HLL:       hll1,
			OtherHLL:  hll2,
		}

		result, _, err := algo.Process(ctx, snapshot, mergeInput)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		// Should estimate close to 6
		if output.Cardinality < 4 || output.Cardinality > 10 {
			t.Errorf("expected cardinality close to 6, got %d", output.Cardinality)
		}
	})

	t.Run("merge with nil HLLs", func(t *testing.T) {
		input := &HyperLogLogInput{
			Operation: "merge",
			HLL:       nil,
			OtherHLL:  nil,
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*HyperLogLogOutput)
		if output.Cardinality != 0 {
			t.Errorf("expected cardinality 0, got %d", output.Cardinality)
		}
	})

	t.Run("merge with mismatched precision fails", func(t *testing.T) {
		hll1 := &HLLState{Registers: make([]uint8, 16), Precision: 4}
		hll2 := &HLLState{Registers: make([]uint8, 64), Precision: 6}

		input := &HyperLogLogInput{
			Operation: "merge",
			HLL:       hll1,
			OtherHLL:  hll2,
		}

		_, _, err := algo.Process(ctx, snapshot, input)
		if err == nil {
			t.Error("expected error for mismatched precision")
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

		input := &HyperLogLogInput{
			Operation: "add",
			Items:     []string{"foo"},
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

func TestHyperLogLog_Evaluable(t *testing.T) {
	algo := NewHyperLogLog(nil)

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
		algo := &HyperLogLog{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("health check fails with invalid precision", func(t *testing.T) {
		algo := NewHyperLogLog(&HyperLogLogConfig{Precision: 3})
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with precision < 4")
		}

		algo = NewHyperLogLog(&HyperLogLogConfig{Precision: 20})
		err = algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with precision > 18")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
