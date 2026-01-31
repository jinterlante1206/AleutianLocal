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
	"fmt"
	"strings"
)

// TreeSubState represents states within tree mode planning.
// Used to track progress through MCTS phases within the broader CB-11 agent loop.
type TreeSubState string

const (
	// TreeSubStateNone indicates not in tree mode.
	TreeSubStateNone TreeSubState = ""

	// TreeSubStateExpand indicates the expansion phase (generating child nodes).
	TreeSubStateExpand TreeSubState = "expanding"

	// TreeSubStateSelect indicates the selection phase (UCB1 traversal).
	TreeSubStateSelect TreeSubState = "selecting"

	// TreeSubStateSimulate indicates the simulation phase (evaluating actions).
	TreeSubStateSimulate TreeSubState = "simulating"

	// TreeSubStateBackprop indicates the backpropagation phase (updating scores).
	TreeSubStateBackprop TreeSubState = "backpropagating"

	// TreeSubStateExtracting indicates extracting the best path.
	TreeSubStateExtracting TreeSubState = "extracting_best"
)

// IsActive returns true if in an active tree mode state.
func (s TreeSubState) IsActive() bool {
	return s != TreeSubStateNone
}

// String returns the string representation.
func (s TreeSubState) String() string {
	if s == TreeSubStateNone {
		return "none"
	}
	return string(s)
}

// PlanPhaseConfig extends plan phase configuration for tree mode.
//
// This configuration determines when the agent should use tree-based
// MCTS planning versus simple linear planning.
type PlanPhaseConfig struct {
	// Tree mode triggers
	ComplexityThreshold float64 // Complexity score to trigger tree mode (default: 0.7)
	AlwaysUseTreeMode   bool    // Force tree mode for all planning
	EnableTreeMode      bool    // Allow tree mode (default: true)

	// Fallback behavior
	FallbackOnTreeFailure bool // Use linear plan if tree fails (default: true)
	MaxTreeRetries        int  // Retries before fallback (default: 1)
}

// DefaultPlanPhaseConfig returns sensible defaults.
//
// Outputs:
//   - PlanPhaseConfig: Configuration with default values.
func DefaultPlanPhaseConfig() PlanPhaseConfig {
	return PlanPhaseConfig{
		ComplexityThreshold:   0.7,
		AlwaysUseTreeMode:     false,
		EnableTreeMode:        true,
		FallbackOnTreeFailure: true,
		MaxTreeRetries:        1,
	}
}

// SessionMetrics contains metrics from a previous session that may influence
// tree mode decision.
type SessionMetrics struct {
	// PreviousPlanFailed indicates the last plan attempt failed.
	PreviousPlanFailed bool

	// EstimatedBlastRadius is the number of files/symbols affected.
	EstimatedBlastRadius int

	// PreviousIterations is how many planning iterations were needed.
	PreviousIterations int
}

// TreeModeDecision contains the decision about whether to use tree mode.
type TreeModeDecision struct {
	// UseTreeMode is true if tree-based MCTS should be used.
	UseTreeMode bool

	// Reason explains why this decision was made.
	Reason string

	// Complexity is the estimated task complexity (0-1).
	Complexity float64

	// Triggers lists which conditions activated tree mode.
	Triggers []string
}

// ShouldUseTreeMode determines if tree mode should be activated.
//
// The decision is based on:
// - Configuration settings (EnableTreeMode, AlwaysUseTreeMode)
// - Task complexity estimation from the description
// - Session history (previous failures, blast radius)
//
// Inputs:
//   - task: The user's task description.
//   - metrics: Session metrics (may be nil).
//   - config: Plan phase configuration.
//
// Outputs:
//   - TreeModeDecision: Decision with reasoning.
//
// Thread Safety: Safe for concurrent use (pure function).
func ShouldUseTreeMode(task string, metrics *SessionMetrics, config PlanPhaseConfig) TreeModeDecision {
	decision := TreeModeDecision{
		Triggers: make([]string, 0),
	}

	// Check if tree mode is disabled
	if !config.EnableTreeMode {
		decision.Reason = "tree mode disabled in config"
		return decision
	}

	// Check if always enabled
	if config.AlwaysUseTreeMode {
		decision.UseTreeMode = true
		decision.Reason = "always_explore enabled"
		decision.Triggers = append(decision.Triggers, "config:always")
		return decision
	}

	// Estimate task complexity
	complexity := estimateTaskComplexity(task)
	decision.Complexity = complexity

	// Check complexity threshold
	if complexity >= config.ComplexityThreshold {
		decision.UseTreeMode = true
		decision.Triggers = append(decision.Triggers, "complexity:high")
	}

	// Check session metrics if available
	if metrics != nil {
		// Previous plan failed
		if metrics.PreviousPlanFailed {
			decision.UseTreeMode = true
			decision.Triggers = append(decision.Triggers, "previous:failed")
		}

		// High blast radius (affecting many files/symbols)
		if metrics.EstimatedBlastRadius > 10 {
			decision.UseTreeMode = true
			decision.Triggers = append(decision.Triggers, "blast_radius:high")
		}
	}

	// Set reason based on decision
	if decision.UseTreeMode {
		decision.Reason = fmt.Sprintf("triggered by: %v", decision.Triggers)
	} else {
		decision.Reason = "no triggers activated, using linear mode"
	}

	return decision
}

// complexityIndicator defines a keyword and its complexity contribution.
type complexityIndicator struct {
	keyword string
	delta   float64
}

// multiFileIndicators suggest changes across multiple files.
var multiFileIndicators = []complexityIndicator{
	{"multiple files", 0.2},
	{"across", 0.15},
	{"refactor", 0.25},
	{"rename", 0.15},
	{"all occurrences", 0.2},
}

// archIndicators suggest architectural changes.
var archIndicators = []complexityIndicator{
	{"architecture", 0.35},
	{"design", 0.25},
	{"restructure", 0.3},
	{"migrate", 0.35},
	{"rewrite", 0.3},
	{"overhaul", 0.35},
}

// simpleIndicators suggest simple tasks (negative contribution).
var simpleIndicators = []complexityIndicator{
	{"typo", -0.3},
	{"comment", -0.25},
	{"format", -0.25},
	{"lint", -0.25},
	{"whitespace", -0.3},
	{"rename variable", -0.15},
}

// estimateTaskComplexity estimates task complexity from the description.
//
// Complexity Scoring:
//   - Base score: 0.0
//   - Multi-file indicators: +0.15 to +0.25 each
//   - Architectural indicators: +0.25 to +0.35 each
//   - Simple task indicators: -0.25 to -0.30 each
//   - Final score clamped to [0, 1]
//
// Inputs:
//   - task: The task description to analyze.
//
// Outputs:
//   - float64: Complexity score in range [0, 1].
//
// Thread Safety: Safe for concurrent use (pure function).
func estimateTaskComplexity(task string) float64 {
	score := 0.0
	taskLower := strings.ToLower(task)

	// Check multi-file indicators
	for _, indicator := range multiFileIndicators {
		if strings.Contains(taskLower, indicator.keyword) {
			score += indicator.delta
		}
	}

	// Check architectural indicators
	for _, indicator := range archIndicators {
		if strings.Contains(taskLower, indicator.keyword) {
			score += indicator.delta
		}
	}

	// Check simple task indicators (negative)
	for _, indicator := range simpleIndicators {
		if strings.Contains(taskLower, indicator.keyword) {
			score += indicator.delta
		}
	}

	// Clamp to [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}
