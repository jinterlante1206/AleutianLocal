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
	"context"
	"errors"
	"testing"
)

func TestPlanningMode_String(t *testing.T) {
	tests := []struct {
		mode     PlanningMode
		expected string
	}{
		{PlanningModeLinear, "linear"},
		{PlanningModeTree, "tree"},
		{PlanningModeHybrid, "hybrid"},
		{PlanningMode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.expected {
				t.Errorf("String() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestNewPlanningOrchestrator(t *testing.T) {
	linear := &NoopLinearPlanner{}
	tree := &NoopMCTSRunner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()
	degradation := NewDegradationManager(DefaultDegradationConfig(), nil)

	orch := NewPlanningOrchestrator(linear, tree, converter, config, degradation)

	if orch == nil {
		t.Fatal("NewPlanningOrchestrator returned nil")
	}
}

func TestPlanningOrchestrator_Plan_Linear(t *testing.T) {
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.99 // Force linear mode

	orch := NewPlanningOrchestrator(linear, nil, converter, config, nil)

	result, err := orch.Plan(context.Background(), "simple task", nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if result.Mode != PlanningModeLinear {
		t.Errorf("Mode = %v, want Linear", result.Mode)
	}
	if result.LinearPlan == nil {
		t.Error("LinearPlan should not be nil")
	}
}

func TestPlanningOrchestrator_Plan_Tree(t *testing.T) {
	tree := &NoopMCTSRunner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.01 // Force tree mode (any task exceeds threshold)

	orch := NewPlanningOrchestrator(nil, tree, converter, config, nil)

	// Use a complex task that triggers tree mode
	result, err := orch.Plan(context.Background(), "refactor and restructure the architecture across multiple files and components", nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if result.Mode != PlanningModeTree {
		t.Errorf("Mode = %v, want Tree", result.Mode)
	}
	if result.Tree == nil {
		t.Error("Tree should not be nil")
	}
}

func TestPlanningOrchestrator_Plan_TreeFailureFallback(t *testing.T) {
	tree := &NoopMCTSRunner{Err: errors.New("MCTS failed")}
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.01
	config.FallbackOnTreeFailure = true

	degradation := NewDegradationManager(DefaultDegradationConfig(), nil)
	orch := NewPlanningOrchestrator(linear, tree, converter, config, degradation)

	result, err := orch.Plan(context.Background(), "refactor the entire architecture across multiple modules", nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// Should fall back to linear
	if result.Mode != PlanningModeLinear {
		t.Errorf("Mode = %v, want Linear (fallback)", result.Mode)
	}
}

func TestPlanningOrchestrator_Plan_DegradationForcesLinear(t *testing.T) {
	tree := &NoopMCTSRunner{}
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.01 // Would use tree otherwise

	degradation := NewDegradationManager(DefaultDegradationConfig(), nil)
	// Force degradation to linear fallback level
	for i := 0; i < 6; i++ {
		degradation.RecordFailure()
	}

	orch := NewPlanningOrchestrator(linear, tree, converter, config, degradation)

	result, err := orch.Plan(context.Background(), "refactor the entire architecture and restructure components", nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// Should use linear due to degradation
	if result.Mode != PlanningModeLinear {
		t.Errorf("Mode = %v, want Linear (degradation)", result.Mode)
	}
}

func TestPlanningOrchestrator_PlanWithMode_Linear(t *testing.T) {
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()

	orch := NewPlanningOrchestrator(linear, nil, converter, config, nil)

	result, err := orch.PlanWithMode(context.Background(), "test task", PlanningModeLinear)
	if err != nil {
		t.Fatalf("PlanWithMode() error = %v", err)
	}

	if result.Mode != PlanningModeLinear {
		t.Errorf("Mode = %v, want Linear", result.Mode)
	}
}

func TestPlanningOrchestrator_PlanWithMode_Tree(t *testing.T) {
	tree := &NoopMCTSRunner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()

	orch := NewPlanningOrchestrator(nil, tree, converter, config, nil)

	result, err := orch.PlanWithMode(context.Background(), "test task", PlanningModeTree)
	if err != nil {
		t.Fatalf("PlanWithMode() error = %v", err)
	}

	if result.Mode != PlanningModeTree {
		t.Errorf("Mode = %v, want Tree", result.Mode)
	}
}

func TestPlanningOrchestrator_PlanWithMode_Hybrid(t *testing.T) {
	tree := &NoopMCTSRunner{}
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()

	orch := NewPlanningOrchestrator(linear, tree, converter, config, nil)

	result, err := orch.PlanWithMode(context.Background(), "test task", PlanningModeHybrid)
	if err != nil {
		t.Fatalf("PlanWithMode() error = %v", err)
	}

	// Should use tree since it succeeds
	if result.Mode != PlanningModeTree {
		t.Errorf("Mode = %v, want Tree", result.Mode)
	}
}

func TestPlanningOrchestrator_PlanWithMode_HybridFallback(t *testing.T) {
	tree := &NoopMCTSRunner{Err: errors.New("MCTS failed")}
	linear := &NoopLinearPlanner{}
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")
	config := DefaultPlanPhaseConfig()

	orch := NewPlanningOrchestrator(linear, tree, converter, config, nil)

	result, err := orch.PlanWithMode(context.Background(), "test task", PlanningModeHybrid)
	if err != nil {
		t.Fatalf("PlanWithMode() error = %v", err)
	}

	// Should fall back to linear
	if result.Mode != PlanningModeLinear {
		t.Errorf("Mode = %v, want Linear (fallback)", result.Mode)
	}
}

func TestPlanningOrchestrator_PlanWithMode_UnknownMode(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	orch := NewPlanningOrchestrator(nil, nil, nil, config, nil)

	_, err := orch.PlanWithMode(context.Background(), "test task", PlanningMode(99))
	if err == nil {
		t.Error("expected error for unknown mode")
	}
}

func TestPlanningOrchestrator_Plan_NoLinearPlanner(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.99 // Force linear mode

	orch := NewPlanningOrchestrator(nil, nil, nil, config, nil)

	_, err := orch.Plan(context.Background(), "simple task", nil)
	if err == nil {
		t.Error("expected error when linear planner not configured")
	}
}

func TestPlanningOrchestrator_Plan_NoTreeRunner(t *testing.T) {
	config := DefaultPlanPhaseConfig()
	config.ComplexityThreshold = 0.01
	config.FallbackOnTreeFailure = false

	orch := NewPlanningOrchestrator(nil, nil, nil, config, nil)

	_, err := orch.Plan(context.Background(), "refactor and restructure the entire architecture across multiple modules", nil)
	if err == nil {
		t.Error("expected error when tree runner not configured")
	}
}

func TestLinearPlanToTree(t *testing.T) {
	plan := &LinearPlan{
		Steps: []PlanStep{
			{Description: "Step 1", Action: &PlannedAction{Type: ActionTypeEdit, FilePath: "a.go"}},
			{Description: "Step 2"},
			{Description: "Step 3", Action: &PlannedAction{Type: ActionTypeCreate, FilePath: "b.go"}},
		},
	}

	tree := linearPlanToTree(plan, "test task")

	if tree == nil {
		t.Fatal("tree should not be nil")
	}
	if tree.Task != "test task" {
		t.Errorf("Task = %s, want 'test task'", tree.Task)
	}

	// Should have root + 3 steps
	if tree.TotalNodes() != 4 {
		t.Errorf("TotalNodes() = %d, want 4", tree.TotalNodes())
	}

	bestPath := tree.BestPath()
	if len(bestPath) != 4 { // root + 3 steps
		t.Errorf("BestPath length = %d, want 4", len(bestPath))
	}
}

func TestSessionMetrics(t *testing.T) {
	metrics := &SessionMetrics{
		PreviousPlanFailed:   true,
		EstimatedBlastRadius: 10,
		PreviousIterations:   3,
	}

	if !metrics.PreviousPlanFailed {
		t.Error("PreviousPlanFailed should be true")
	}
	if metrics.EstimatedBlastRadius != 10 {
		t.Errorf("EstimatedBlastRadius = %d, want 10", metrics.EstimatedBlastRadius)
	}
}

func TestNoopLinearPlanner(t *testing.T) {
	planner := &NoopLinearPlanner{}

	plan, err := planner.Plan(context.Background(), "test task")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if len(plan.Steps) != 3 {
		t.Errorf("Steps length = %d, want 3", len(plan.Steps))
	}
}

func TestNoopLinearPlanner_WithCustomPlan(t *testing.T) {
	customPlan := &LinearPlan{
		Steps: []PlanStep{
			{Description: "Custom step"},
		},
	}

	planner := &NoopLinearPlanner{Plan_: customPlan}

	plan, err := planner.Plan(context.Background(), "test task")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Errorf("Steps length = %d, want 1", len(plan.Steps))
	}
}

func TestNoopMCTSRunner(t *testing.T) {
	runner := &NoopMCTSRunner{}
	budget := NewTreeBudget(DefaultTreeBudgetConfig())

	tree, err := runner.RunMCTS(context.Background(), "test task", budget)
	if err != nil {
		t.Fatalf("RunMCTS() error = %v", err)
	}

	if tree == nil {
		t.Fatal("tree should not be nil")
	}
	if tree.Task != "test task" {
		t.Errorf("Task = %s, want 'test task'", tree.Task)
	}
}

func TestNoopMCTSRunner_WithCustomTree(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	customTree := NewPlanTree("custom task", budget)

	runner := &NoopMCTSRunner{Tree_: customTree}

	tree, err := runner.RunMCTS(context.Background(), "different task", budget)
	if err != nil {
		t.Fatalf("RunMCTS() error = %v", err)
	}

	if tree.Task != "custom task" {
		t.Errorf("Task = %s, want 'custom task'", tree.Task)
	}
}

func TestNoopMCTSRunner_WithError(t *testing.T) {
	runner := &NoopMCTSRunner{Err: errors.New("test error")}
	budget := NewTreeBudget(DefaultTreeBudgetConfig())

	_, err := runner.RunMCTS(context.Background(), "test task", budget)
	if err == nil {
		t.Error("expected error")
	}
}
