// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package algorithms

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// mockAlgorithm is a simple test algorithm.
type mockAlgorithm struct {
	name       string
	timeout    time.Duration
	processErr error
	delay      time.Duration
	output     string
	delta      crs.Delta
}

func (m *mockAlgorithm) Name() string {
	return m.name
}

func (m *mockAlgorithm) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "partial", nil, ctx.Err()
		}
	}
	if m.processErr != nil {
		return nil, nil, m.processErr
	}
	return m.output, m.delta, nil
}

func (m *mockAlgorithm) Timeout() time.Duration {
	return m.timeout
}

func (m *mockAlgorithm) InputType() reflect.Type {
	return reflect.TypeOf("")
}

func (m *mockAlgorithm) OutputType() reflect.Type {
	return reflect.TypeOf("")
}

func (m *mockAlgorithm) ProgressInterval() time.Duration {
	return time.Second
}

func (m *mockAlgorithm) SupportsPartialResults() bool {
	return true
}

func (m *mockAlgorithm) Properties() []eval.Property {
	return nil
}

func (m *mockAlgorithm) Metrics() []eval.MetricDefinition {
	return nil
}

func (m *mockAlgorithm) HealthCheck(ctx context.Context) error {
	return nil
}

func TestNewRunner(t *testing.T) {
	t.Run("creates runner with capacity", func(t *testing.T) {
		r := NewRunner(5)
		if r == nil {
			t.Fatal("expected non-nil runner")
		}
	})

	t.Run("uses default capacity for zero", func(t *testing.T) {
		r := NewRunner(0)
		if r == nil {
			t.Fatal("expected non-nil runner")
		}
	})

	t.Run("uses default capacity for negative", func(t *testing.T) {
		r := NewRunner(-1)
		if r == nil {
			t.Fatal("expected non-nil runner")
		}
	})
}

func TestRunner_Run(t *testing.T) {
	ctx := context.Background()
	c := crs.New(nil)
	snapshot := c.Snapshot()

	t.Run("runs single algorithm", func(t *testing.T) {
		runner := NewRunner(1)
		algo := &mockAlgorithm{
			name:    "test1",
			timeout: time.Second,
			output:  "result1",
		}

		runner.Run(ctx, algo, snapshot, nil)
		delta, results, err := runner.Collect(ctx)
		if err != nil {
			t.Fatalf("collect failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Name != "test1" {
			t.Errorf("expected name test1, got %s", results[0].Name)
		}
		if results[0].Output != "result1" {
			t.Errorf("expected output result1, got %v", results[0].Output)
		}
		if delta != nil {
			t.Errorf("expected nil delta, got %v", delta)
		}
	})

	t.Run("runs multiple algorithms in parallel", func(t *testing.T) {
		runner := NewRunner(3)

		algo1 := &mockAlgorithm{name: "algo1", timeout: time.Second, output: "out1"}
		algo2 := &mockAlgorithm{name: "algo2", timeout: time.Second, output: "out2"}
		algo3 := &mockAlgorithm{name: "algo3", timeout: time.Second, output: "out3"}

		runner.Run(ctx, algo1, snapshot, nil)
		runner.Run(ctx, algo2, snapshot, nil)
		runner.Run(ctx, algo3, snapshot, nil)

		_, results, err := runner.Collect(ctx)
		if err != nil {
			t.Fatalf("collect failed: %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
	})
}

func TestRunner_Timeout(t *testing.T) {
	ctx := context.Background()
	c := crs.New(nil)
	snapshot := c.Snapshot()

	t.Run("respects algorithm timeout", func(t *testing.T) {
		runner := NewRunner(1)
		algo := &mockAlgorithm{
			name:    "slow",
			timeout: 100 * time.Millisecond,
			delay:   500 * time.Millisecond, // Longer than timeout
		}

		start := time.Now()
		runner.Run(ctx, algo, snapshot, nil)
		_, results, err := runner.Collect(ctx)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("collect failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if !results[0].Cancelled {
			t.Error("expected result to be cancelled")
		}
		// Should finish faster than the delay
		if elapsed > 300*time.Millisecond {
			t.Errorf("expected faster than 300ms, got %v", elapsed)
		}
	})
}

func TestRunner_Stats(t *testing.T) {
	ctx := context.Background()
	c := crs.New(nil)
	snapshot := c.Snapshot()

	runner := NewRunner(2)
	algo1 := &mockAlgorithm{name: "a1", timeout: time.Second}
	algo2 := &mockAlgorithm{name: "a2", timeout: time.Second}

	runner.Run(ctx, algo1, snapshot, nil)
	runner.Run(ctx, algo2, snapshot, nil)

	// Wait for completion
	runner.Collect(ctx)

	stats := runner.Stats()
	if stats.Started != 2 {
		t.Errorf("expected 2 started, got %d", stats.Started)
	}
	if stats.Completed != 2 {
		t.Errorf("expected 2 completed, got %d", stats.Completed)
	}
}

func TestRunner_DeltaMerging(t *testing.T) {
	ctx := context.Background()
	c := crs.New(nil)
	snapshot := c.Snapshot()

	t.Run("merges multiple deltas", func(t *testing.T) {
		runner := NewRunner(2)

		delta1 := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"node1": {Proof: 1, Disproof: 1},
		})
		delta2 := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"node2": {Proof: 2, Disproof: 2},
		})

		algo1 := &mockAlgorithm{name: "a1", timeout: time.Second, delta: delta1}
		algo2 := &mockAlgorithm{name: "a2", timeout: time.Second, delta: delta2}

		runner.Run(ctx, algo1, snapshot, nil)
		runner.Run(ctx, algo2, snapshot, nil)

		merged, _, err := runner.Collect(ctx)
		if err != nil {
			t.Fatalf("collect failed: %v", err)
		}

		composite, ok := merged.(*crs.CompositeDelta)
		if !ok {
			t.Fatal("expected composite delta")
		}
		if len(composite.Deltas) != 2 {
			t.Errorf("expected 2 deltas, got %d", len(composite.Deltas))
		}
	})

	t.Run("returns single delta unwrapped", func(t *testing.T) {
		runner := NewRunner(1)

		delta := crs.NewProofDelta(crs.SignalSourceHard, map[string]crs.ProofNumber{
			"node1": {Proof: 1, Disproof: 1},
		})
		algo := &mockAlgorithm{name: "a1", timeout: time.Second, delta: delta}

		runner.Run(ctx, algo, snapshot, nil)
		merged, _, err := runner.Collect(ctx)
		if err != nil {
			t.Fatalf("collect failed: %v", err)
		}

		// Should not be wrapped in composite
		if _, ok := merged.(*crs.CompositeDelta); ok {
			t.Error("expected unwrapped delta, got composite")
		}
	})
}

func TestRunParallel(t *testing.T) {
	ctx := context.Background()
	c := crs.New(nil)
	snapshot := c.Snapshot()

	algo1 := &mockAlgorithm{name: "a1", timeout: time.Second, output: "o1"}
	algo2 := &mockAlgorithm{name: "a2", timeout: time.Second, output: "o2"}

	_, results, err := RunParallel(ctx, snapshot,
		NewExecution(algo1, nil),
		NewExecution(algo2, nil),
	)

	if err != nil {
		t.Fatalf("RunParallel failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
