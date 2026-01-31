// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"strings"
	"testing"
)

func TestTreeSubState_IsActive(t *testing.T) {
	tests := []struct {
		state    TreeSubState
		expected bool
	}{
		{TreeSubStateNone, false},
		{TreeSubStateExpand, true},
		{TreeSubStateSelect, true},
		{TreeSubStateSimulate, true},
		{TreeSubStateBackprop, true},
		{TreeSubStateExtracting, true},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			if got := tt.state.IsActive(); got != tt.expected {
				t.Errorf("IsActive() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTreeSubState_String(t *testing.T) {
	tests := []struct {
		state    TreeSubState
		expected string
	}{
		{TreeSubStateNone, "none"},
		{TreeSubStateExpand, "expanding"},
		{TreeSubStateSelect, "selecting"},
		{TreeSubStateSimulate, "simulating"},
		{TreeSubStateBackprop, "backpropagating"},
		{TreeSubStateExtracting, "extracting_best"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultPlanPhaseConfig(t *testing.T) {
	config := DefaultPlanPhaseConfig()

	if config.ComplexityThreshold != 0.7 {
		t.Errorf("ComplexityThreshold = %v, want 0.7", config.ComplexityThreshold)
	}
	if config.AlwaysUseTreeMode {
		t.Error("AlwaysUseTreeMode should be false by default")
	}
	if !config.EnableTreeMode {
		t.Error("EnableTreeMode should be true by default")
	}
	if !config.FallbackOnTreeFailure {
		t.Error("FallbackOnTreeFailure should be true by default")
	}
	if config.MaxTreeRetries != 1 {
		t.Errorf("MaxTreeRetries = %v, want 1", config.MaxTreeRetries)
	}
}

func TestShouldUseTreeMode_Disabled(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	config.EnableTreeMode = false

	decision := ShouldUseTreeMode("refactor the entire codebase", nil, config)

	if decision.UseTreeMode {
		t.Error("should not use tree mode when disabled")
	}
	if !strings.Contains(decision.Reason, "disabled") {
		t.Errorf("Reason = %v, should mention disabled", decision.Reason)
	}
}

func TestShouldUseTreeMode_AlwaysEnabled(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	config.AlwaysUseTreeMode = true

	decision := ShouldUseTreeMode("fix typo", nil, config)

	if !decision.UseTreeMode {
		t.Error("should use tree mode when always enabled")
	}
	if len(decision.Triggers) != 1 || decision.Triggers[0] != "config:always" {
		t.Errorf("Triggers = %v, want [config:always]", decision.Triggers)
	}
}

func TestShouldUseTreeMode_HighComplexity(t *testing.T) {
	config := DefaultPlanPhaseConfig()

	decision := ShouldUseTreeMode("refactor the architecture across multiple files", nil, config)

	if !decision.UseTreeMode {
		t.Error("should use tree mode for high complexity task")
	}
	if decision.Complexity < 0.7 {
		t.Errorf("Complexity = %v, expected >= 0.7", decision.Complexity)
	}

	foundComplexity := false
	for _, trigger := range decision.Triggers {
		if trigger == "complexity:high" {
			foundComplexity = true
			break
		}
	}
	if !foundComplexity {
		t.Errorf("Triggers = %v, should contain complexity:high", decision.Triggers)
	}
}

func TestShouldUseTreeMode_LowComplexity(t *testing.T) {
	config := DefaultPlanPhaseConfig()

	decision := ShouldUseTreeMode("fix typo in comment", nil, config)

	if decision.UseTreeMode {
		t.Error("should not use tree mode for low complexity task")
	}
	if decision.Complexity > 0.3 {
		t.Errorf("Complexity = %v, expected < 0.3 for simple task", decision.Complexity)
	}
}

func TestShouldUseTreeMode_PreviousPlanFailed(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	metrics := &SessionMetrics{
		PreviousPlanFailed: true,
	}

	decision := ShouldUseTreeMode("simple task", metrics, config)

	if !decision.UseTreeMode {
		t.Error("should use tree mode after previous plan failure")
	}

	foundPrevious := false
	for _, trigger := range decision.Triggers {
		if trigger == "previous:failed" {
			foundPrevious = true
			break
		}
	}
	if !foundPrevious {
		t.Errorf("Triggers = %v, should contain previous:failed", decision.Triggers)
	}
}

func TestShouldUseTreeMode_HighBlastRadius(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	metrics := &SessionMetrics{
		EstimatedBlastRadius: 15,
	}

	decision := ShouldUseTreeMode("update function signature", metrics, config)

	if !decision.UseTreeMode {
		t.Error("should use tree mode for high blast radius")
	}

	foundBlast := false
	for _, trigger := range decision.Triggers {
		if trigger == "blast_radius:high" {
			foundBlast = true
			break
		}
	}
	if !foundBlast {
		t.Errorf("Triggers = %v, should contain blast_radius:high", decision.Triggers)
	}
}

func TestShouldUseTreeMode_MultipleTriggers(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	metrics := &SessionMetrics{
		PreviousPlanFailed:   true,
		EstimatedBlastRadius: 20,
	}

	decision := ShouldUseTreeMode("refactor architecture across multiple files", metrics, config)

	if !decision.UseTreeMode {
		t.Error("should use tree mode with multiple triggers")
	}

	// Should have multiple triggers
	if len(decision.Triggers) < 2 {
		t.Errorf("expected multiple triggers, got %v", decision.Triggers)
	}
}

func TestShouldUseTreeMode_NilMetrics(t *testing.T) {
	config := DefaultPlanPhaseConfig()

	// Should not panic with nil metrics
	decision := ShouldUseTreeMode("simple task", nil, config)

	// Low complexity task without metrics should not trigger tree mode
	if decision.UseTreeMode {
		t.Error("should not use tree mode for simple task without metrics")
	}
}

func TestEstimateTaskComplexity(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		minScore float64
		maxScore float64
	}{
		{
			name:     "typo fix",
			task:     "fix typo in readme",
			minScore: 0,
			maxScore: 0.1,
		},
		{
			name:     "comment update",
			task:     "update comment",
			minScore: 0,
			maxScore: 0.1,
		},
		{
			name:     "simple refactor",
			task:     "refactor function",
			minScore: 0.2,
			maxScore: 0.4,
		},
		{
			name:     "multi-file change",
			task:     "rename variable across multiple files",
			minScore: 0.15,
			maxScore: 0.5,
		},
		{
			name:     "architecture change",
			task:     "restructure the architecture",
			minScore: 0.5,
			maxScore: 0.8,
		},
		{
			name:     "major overhaul",
			task:     "migrate and rewrite the entire architecture across all files",
			minScore: 0.8,
			maxScore: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := estimateTaskComplexity(tt.task)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("estimateTaskComplexity(%q) = %v, want between %v and %v",
					tt.task, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestEstimateTaskComplexity_ClampBounds(t *testing.T) {
	// Very simple task should clamp to 0
	score := estimateTaskComplexity("fix typo in comment format lint whitespace")
	if score != 0 {
		t.Errorf("very simple task should clamp to 0, got %v", score)
	}

	// Very complex task should clamp to 1
	score = estimateTaskComplexity("migrate architecture restructure design overhaul rewrite across multiple files refactor rename all occurrences")
	if score != 1 {
		t.Errorf("very complex task should clamp to 1, got %v", score)
	}
}

func TestEstimateTaskComplexity_CaseInsensitive(t *testing.T) {
	lower := estimateTaskComplexity("refactor")
	upper := estimateTaskComplexity("REFACTOR")
	mixed := estimateTaskComplexity("ReFaCtOr")

	if lower != upper || lower != mixed {
		t.Errorf("complexity should be case insensitive: lower=%v, upper=%v, mixed=%v",
			lower, upper, mixed)
	}
}
