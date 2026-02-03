// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package search

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewWatchedLiterals(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewWatchedLiterals(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "watched_literals" {
			t.Errorf("expected name watched_literals, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &WatchedConfig{
			MaxPropagations: 500,
			Timeout:         3 * time.Second,
		}
		algo := NewWatchedLiterals(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestWatchedLiterals_Process(t *testing.T) {
	algo := NewWatchedLiterals(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("propagates unit clauses", func(t *testing.T) {
		// Clause: (A OR B) with A = false should propagate B = true
		input := &WatchedInput{
			Clauses: []CDCLClause{
				{
					ID: "clause1",
					Literals: []CDCLLiteral{
						{NodeID: "A", Positive: true},
						{NodeID: "B", Positive: true},
					},
					Source: crs.SignalSourceHard,
				},
			},
			Assignments: map[string]bool{
				"A": false, // A is false
			},
			LastAssignment: CDCLLiteral{NodeID: "A", Positive: false},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*WatchedOutput)
		if !ok {
			t.Fatal("expected *WatchedOutput")
		}

		// Should propagate B = true
		found := false
		for _, prop := range output.Propagations {
			if prop.Literal.NodeID == "B" && prop.Literal.Positive {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected B to be propagated as true")
		}
	})

	t.Run("detects conflicts", func(t *testing.T) {
		// Clause: (A OR B) with A = false and B = false should conflict
		input := &WatchedInput{
			Clauses: []CDCLClause{
				{
					ID: "clause1",
					Literals: []CDCLLiteral{
						{NodeID: "A", Positive: true},
						{NodeID: "B", Positive: true},
					},
					Source: crs.SignalSourceHard,
				},
			},
			Assignments: map[string]bool{
				"A": false,
				"B": false,
			},
			LastAssignment: CDCLLiteral{NodeID: "A", Positive: false},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WatchedOutput)

		if output.Conflict == nil {
			t.Error("expected conflict when all literals are falsified")
		}
	})

	t.Run("handles satisfied clauses", func(t *testing.T) {
		// Clause: (A OR B) with A = true is satisfied
		input := &WatchedInput{
			Clauses: []CDCLClause{
				{
					ID: "clause1",
					Literals: []CDCLLiteral{
						{NodeID: "A", Positive: true},
						{NodeID: "B", Positive: true},
					},
					Source: crs.SignalSourceHard,
				},
			},
			Assignments: map[string]bool{
				"A": true,
			},
			LastAssignment: CDCLLiteral{NodeID: "A", Positive: true},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WatchedOutput)

		// No conflict, no propagation (clause is satisfied)
		if output.Conflict != nil {
			t.Error("expected no conflict for satisfied clause")
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
		cancel() // Cancel immediately

		input := &WatchedInput{
			Clauses:        []CDCLClause{},
			Assignments:    map[string]bool{},
			LastAssignment: CDCLLiteral{NodeID: "A", Positive: true},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("handles empty clauses", func(t *testing.T) {
		input := &WatchedInput{
			Clauses:        []CDCLClause{},
			Assignments:    map[string]bool{},
			LastAssignment: CDCLLiteral{NodeID: "A", Positive: true},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*WatchedOutput)
		if output.Conflict != nil {
			t.Error("expected no conflict with empty clauses")
		}
		if len(output.Propagations) != 0 {
			t.Error("expected no propagations with empty clauses")
		}
	})
}

func TestWatchedLiterals_ChainPropagation(t *testing.T) {
	algo := NewWatchedLiterals(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	// Chain: A=false -> B=true -> C=true
	// Clause 1: (A OR B)
	// Clause 2: (NOT B OR C)
	input := &WatchedInput{
		Clauses: []CDCLClause{
			{
				ID: "clause1",
				Literals: []CDCLLiteral{
					{NodeID: "A", Positive: true},
					{NodeID: "B", Positive: true},
				},
				Source: crs.SignalSourceHard,
			},
			{
				ID: "clause2",
				Literals: []CDCLLiteral{
					{NodeID: "B", Positive: false}, // NOT B
					{NodeID: "C", Positive: true},
				},
				Source: crs.SignalSourceHard,
			},
		},
		Assignments: map[string]bool{
			"A": false,
		},
		LastAssignment: CDCLLiteral{NodeID: "A", Positive: false},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*WatchedOutput)

	// Should have propagated B = true, then C = true
	bPropagated := false
	cPropagated := false
	for _, prop := range output.Propagations {
		if prop.Literal.NodeID == "B" && prop.Literal.Positive {
			bPropagated = true
		}
		if prop.Literal.NodeID == "C" && prop.Literal.Positive {
			cPropagated = true
		}
	}

	if !bPropagated {
		t.Error("expected B to be propagated")
	}
	if !cPropagated {
		t.Error("expected C to be propagated via chain")
	}
}

func TestWatchedLiterals_Evaluable(t *testing.T) {
	algo := NewWatchedLiterals(nil)

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
		algo := &WatchedLiterals{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewWatchedLiterals(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
