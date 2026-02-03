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

func TestNewCDCL(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewCDCL(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "cdcl" {
			t.Errorf("expected name cdcl, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &CDCLConfig{
			MaxLearned: 500,
			Timeout:    3 * time.Second,
		}
		algo := NewCDCL(config)
		if algo.Timeout() != 3*time.Second {
			t.Errorf("expected timeout 3s, got %v", algo.Timeout())
		}
	})
}

func TestCDCL_NeverLearnsFromSoftSignals(t *testing.T) {
	// CRITICAL TEST: This enforces Rule #2 - CDCL must never learn from soft signals

	algo := NewCDCL(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("ignores soft signal conflicts", func(t *testing.T) {
		input := &CDCLInput{
			Conflict: CDCLConflict{
				NodeID:           "test_node",
				Source:           crs.SignalSourceSoft, // SOFT signal - must be ignored
				ConflictingNodes: []string{"node1", "node2"},
				Reason:           "LLM suggested this is wrong",
			},
			Assignments: []CDCLAssignment{
				{NodeID: "node1", Value: true, DecisionLevel: 1, IsDecision: true},
				{NodeID: "node2", Value: true, DecisionLevel: 1, IsDecision: false},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*CDCLOutput)
		if !ok {
			t.Fatal("expected *CDCLOutput")
		}

		// CDCL must NOT learn from soft signals
		if output.LearnedClause != nil {
			t.Error("CDCL MUST NOT learn clauses from soft signals (Rule #2)")
		}
		if !output.ConflictWasSoft {
			t.Error("expected ConflictWasSoft to be true")
		}
	})

	t.Run("learns from hard signal conflicts", func(t *testing.T) {
		input := &CDCLInput{
			Conflict: CDCLConflict{
				NodeID:           "test_node",
				Source:           crs.SignalSourceHard, // HARD signal - should learn
				ConflictingNodes: []string{"node1", "node2"},
				Reason:           "Compiler error",
			},
			Assignments: []CDCLAssignment{
				{NodeID: "node1", Value: true, DecisionLevel: 1, IsDecision: true},
				{NodeID: "node2", Value: true, DecisionLevel: 1, IsDecision: false},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CDCLOutput)

		// Should learn from hard signals
		if output.LearnedClause == nil {
			t.Error("expected CDCL to learn from hard signal conflict")
		}
		if output.ConflictWasSoft {
			t.Error("expected ConflictWasSoft to be false for hard signal")
		}
		if output.LearnedClause != nil && !output.LearnedClause.Source.IsHard() {
			t.Error("learned clause must have hard signal source")
		}
	})
}

func TestCDCL_Process(t *testing.T) {
	algo := NewCDCL(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("returns error for invalid input type", func(t *testing.T) {
		_, _, err := algo.Process(ctx, snapshot, "invalid")
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})

	t.Run("handles empty conflict", func(t *testing.T) {
		input := &CDCLInput{
			Conflict: CDCLConflict{
				NodeID:           "test",
				Source:           crs.SignalSourceHard,
				ConflictingNodes: []string{},
			},
			Assignments: []CDCLAssignment{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CDCLOutput)
		if output.LearnedClause != nil && len(output.LearnedClause.Literals) > 0 {
			t.Error("expected no clause from empty conflict")
		}
	})

	t.Run("calculates backjump level", func(t *testing.T) {
		input := &CDCLInput{
			Conflict: CDCLConflict{
				NodeID:           "conflict",
				Source:           crs.SignalSourceHard,
				ConflictingNodes: []string{"node1", "node2", "node3"},
				Reason:           "Test conflict",
			},
			Assignments: []CDCLAssignment{
				{NodeID: "node1", Value: true, DecisionLevel: 1, IsDecision: true},
				{NodeID: "node2", Value: true, DecisionLevel: 2, IsDecision: true},
				{NodeID: "node3", Value: false, DecisionLevel: 3, IsDecision: true},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*CDCLOutput)
		// Backjump should be to a level less than max (3)
		if output.BackjumpLevel >= 3 {
			t.Errorf("expected backjump level < 3, got %d", output.BackjumpLevel)
		}
	})

	t.Run("handles cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		input := &CDCLInput{
			Conflict: CDCLConflict{
				NodeID:           "test",
				Source:           crs.SignalSourceHard,
				ConflictingNodes: []string{"node1"},
			},
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

func TestCDCL_Properties(t *testing.T) {
	algo := NewCDCL(nil)

	t.Run("has no_soft_signal_clauses property", func(t *testing.T) {
		props := algo.Properties()
		found := false
		for _, p := range props {
			if p.Name == "no_soft_signal_clauses" {
				found = true

				// Test the property check function
				softOutput := &CDCLOutput{
					LearnedClause: &CDCLClause{
						Source: crs.SignalSourceSoft, // VIOLATION
					},
				}
				if err := p.Check(nil, softOutput); err == nil {
					t.Error("property should fail for soft signal clause")
				}

				hardOutput := &CDCLOutput{
					LearnedClause: &CDCLClause{
						Source: crs.SignalSourceHard, // OK
					},
				}
				if err := p.Check(nil, hardOutput); err != nil {
					t.Error("property should pass for hard signal clause")
				}

				break
			}
		}
		if !found {
			t.Error("expected no_soft_signal_clauses property")
		}
	})
}

func TestCDCL_Evaluable(t *testing.T) {
	algo := NewCDCL(nil)

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
		algo := &CDCL{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewCDCL(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
