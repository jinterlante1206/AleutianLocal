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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

func TestNewAC3(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		algo := NewAC3(nil)
		if algo == nil {
			t.Fatal("expected non-nil algorithm")
		}
		if algo.Name() != "ac3" {
			t.Errorf("expected name ac3, got %s", algo.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		config := &AC3Config{
			MaxRevisions: 500,
			Timeout:      5 * time.Second,
		}
		algo := NewAC3(config)
		if algo.Timeout() != 5*time.Second {
			t.Errorf("expected timeout 5s, got %v", algo.Timeout())
		}
	})
}

func TestAC3_Process(t *testing.T) {
	algo := NewAC3(nil)
	ctx := context.Background()
	snapshot := crs.New(nil).Snapshot()

	t.Run("reduces domains with not-equal constraint", func(t *testing.T) {
		// X and Y must be different, X = {a, b}, Y = {a}
		// After AC-3: X = {b} (a is removed because Y only has a)
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"a", "b"}},
				"Y": {NodeID: "Y", Domain: []string{"a"}},
			},
			Constraints: []AC3Constraint{
				{ID: "c1", X: "X", Y: "Y", Type: AC3ConstraintNotEqual},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output, ok := result.(*AC3Output)
		if !ok {
			t.Fatal("expected *AC3Output")
		}

		// X should only have "b"
		xDomain := output.ReducedDomains["X"].Domain
		if len(xDomain) != 1 || xDomain[0] != "b" {
			t.Errorf("expected X domain = [b], got %v", xDomain)
		}

		if !output.Consistent {
			t.Error("expected consistent result")
		}
	})

	t.Run("detects empty domain (inconsistent)", func(t *testing.T) {
		// X and Y must be different, but both only have "a"
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"a"}},
				"Y": {NodeID: "Y", Domain: []string{"a"}},
			},
			Constraints: []AC3Constraint{
				{ID: "c1", X: "X", Y: "Y", Type: AC3ConstraintNotEqual},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AC3Output)

		if output.Consistent {
			t.Error("expected inconsistent result (empty domain)")
		}
		if len(output.EmptyDomains) == 0 {
			t.Error("expected at least one empty domain")
		}
	})

	t.Run("handles equal constraint", func(t *testing.T) {
		// X and Y must be equal, X = {a, b, c}, Y = {b, c}
		// After AC-3: X = {b, c}
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"a", "b", "c"}},
				"Y": {NodeID: "Y", Domain: []string{"b", "c"}},
			},
			Constraints: []AC3Constraint{
				{ID: "c1", X: "X", Y: "Y", Type: AC3ConstraintEqual},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AC3Output)

		// X should have "b" and "c" removed "a"
		xDomain := output.ReducedDomains["X"].Domain
		hasA := false
		for _, v := range xDomain {
			if v == "a" {
				hasA = true
			}
		}
		if hasA {
			t.Error("expected 'a' to be removed from X domain")
		}
	})

	t.Run("handles implies constraint", func(t *testing.T) {
		// X=true implies Y=true
		// X = {true, false}, Y = {false}
		// X cannot be true, so X = {false}
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"true", "false"}},
				"Y": {NodeID: "Y", Domain: []string{"false"}},
			},
			Constraints: []AC3Constraint{
				{ID: "c1", X: "X", Y: "Y", Type: AC3ConstraintImplies},
			},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AC3Output)

		// X should only have "false"
		xDomain := output.ReducedDomains["X"].Domain
		if len(xDomain) != 1 || xDomain[0] != "false" {
			t.Errorf("expected X domain = [false], got %v", xDomain)
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

		input := &AC3Input{
			Variables:   map[string]AC3Variable{},
			Constraints: []AC3Constraint{},
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
		input := &AC3Input{
			Variables:   map[string]AC3Variable{},
			Constraints: []AC3Constraint{},
		}

		result, _, err := algo.Process(ctx, snapshot, input)
		if err != nil {
			t.Fatalf("Process failed: %v", err)
		}

		output := result.(*AC3Output)
		if !output.Consistent {
			t.Error("expected consistent result with empty input")
		}
	})
}

func TestAC3_DomainSubsetProperty(t *testing.T) {
	algo := NewAC3(nil)

	props := algo.Properties()
	var subsetProp func(input, output any) error
	for _, p := range props {
		if p.Name == "domain_subset" {
			subsetProp = p.Check
			break
		}
	}

	if subsetProp == nil {
		t.Fatal("domain_subset property not found")
	}

	t.Run("passes for valid reduction", func(t *testing.T) {
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"a", "b", "c"}},
			},
		}
		output := &AC3Output{
			ReducedDomains: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"b", "c"}}, // Subset of original
			},
		}

		if err := subsetProp(input, output); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("fails for invalid reduction", func(t *testing.T) {
		input := &AC3Input{
			Variables: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"a", "b"}},
			},
		}
		output := &AC3Output{
			ReducedDomains: map[string]AC3Variable{
				"X": {NodeID: "X", Domain: []string{"c"}}, // "c" not in original!
			},
		}

		if err := subsetProp(input, output); err == nil {
			t.Error("expected error for invalid subset")
		}
	})
}

func TestAC3_Evaluable(t *testing.T) {
	algo := NewAC3(nil)

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
		algo := &AC3{config: nil}
		err := algo.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected health check to fail with nil config")
		}
	})

	t.Run("supports partial results", func(t *testing.T) {
		algo := NewAC3(nil)
		if !algo.SupportsPartialResults() {
			t.Error("expected SupportsPartialResults to be true")
		}
	})
}
