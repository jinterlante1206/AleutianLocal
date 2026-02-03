// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package constraints

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestNewTMS(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewTMS(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "tms" {
			t.Errorf("expected name tms, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &TMSConfig{
			MaxIterations: 500,
			Timeout:       5 * time.Second,
		}
		algo := NewTMS(config)
		if algo.Timeout() != 5*time.Second {
			t.Errorf("expected timeout 5s, got %v", algo.Timeout())
		}
	})
}

func TestTMS_Process(t *testing.T) {
	algo := NewTMS(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("propagates belief changes", func(t *testing.T) {
		input := &TMSInput{
			Beliefs: map[string]TMSBelief{
				"A": {NodeID: "A", Status: TMSStatusOut},
				"B": {NodeID: "B", Status: TMSStatusOut},
			},
			Justifications: map[string][]TMSJustification{
				"B": {
					{
						ID:      "j1",
						InList:  []string{"A"}, // B is IN if A is IN
						OutList: []string{},
						Source:  crs.SignalSourceHard,
					},
				},
			},
			Changes: []TMSChange{
				{NodeID: "A", NewStatus: TMSStatusIn, Source: crs.SignalSourceHard},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*TMSOutput)
		if !ok {
			t.Fatal("expected *TMSOutput")
		}

		// B should now be IN because A is IN
		found := false
		for _, b := range output.UpdatedBeliefs {
			if b.NodeID == "B" && b.Status == TMSStatusIn {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected B to be updated to IN")
		}
	})

	t.Run("handles OUT list in justifications", func(t *testing.T) {
		// B is IN only if A is OUT
		input := &TMSInput{
			Beliefs: map[string]TMSBelief{
				"A": {NodeID: "A", Status: TMSStatusOut},
				"B": {NodeID: "B", Status: TMSStatusOut},
			},
			Justifications: map[string][]TMSJustification{
				"B": {
					{
						ID:      "j1",
						InList:  []string{},
						OutList: []string{"A"}, // B is IN if A is OUT
						Source:  crs.SignalSourceHard,
					},
				},
			},
			Changes: []TMSChange{
				{NodeID: "B", NewStatus: TMSStatusIn, Source: crs.SignalSourceHard},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TMSOutput)

		// B should be IN because A is OUT
		found := false
		for _, b := range output.UpdatedBeliefs {
			if b.NodeID == "B" && b.Status == TMSStatusIn {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected B to be IN when A is OUT")
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

		input := &TMSInput{
			Beliefs:        map[string]TMSBelief{},
			Justifications: map[string][]TMSJustification{},
			Changes:        []TMSChange{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if result == nil {
			t.Error("expected partial result")
		}
	})

	t.Run("handles empty input", func(t *testing.T) {
		input := &TMSInput{
			Beliefs:        map[string]TMSBelief{},
			Justifications: map[string][]TMSJustification{},
			Changes:        []TMSChange{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*TMSOutput)
		if len(output.UpdatedBeliefs) != 0 {
			t.Error("expected no updated beliefs with empty input")
		}
	})
}

func TestTMS_ChainPropagation(t *testing.T) {
	algo := NewTMS(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	// Chain: A -> B -> C
	input := &TMSInput{
		Beliefs: map[string]TMSBelief{
			"A": {NodeID: "A", Status: TMSStatusOut},
			"B": {NodeID: "B", Status: TMSStatusOut},
			"C": {NodeID: "C", Status: TMSStatusOut},
		},
		Justifications: map[string][]TMSJustification{
			"B": {
				{ID: "j1", InList: []string{"A"}, Source: crs.SignalSourceHard},
			},
			"C": {
				{ID: "j2", InList: []string{"B"}, Source: crs.SignalSourceHard},
			},
		},
		Changes: []TMSChange{
			{NodeID: "A", NewStatus: TMSStatusIn, Source: crs.SignalSourceHard},
		},
	}

	result, _, err := algo.Process(ctx, snapshot, input)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	output := result.(*TMSOutput)

	// Both B and C should be IN
	beliefStatus := make(map[string]TMSStatus)
	for _, b := range output.UpdatedBeliefs {
		beliefStatus[b.NodeID] = b.Status
	}

	if beliefStatus["B"] != TMSStatusIn {
		t.Error("expected B to be IN")
	}
	if beliefStatus["C"] != TMSStatusIn {
		t.Error("expected C to be IN via chain propagation")
	}
}

func TestTMS_Evaluable(t *testing.T) {
	algo := NewTMS(nil)

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
		algo := &TMS{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewTMS(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
