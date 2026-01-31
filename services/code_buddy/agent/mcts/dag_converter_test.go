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
	"time"
)

func TestNewPlanToDAGConverter(t *testing.T) {
	executor := &NoopActionExecutor{}
	converter := NewPlanToDAGConverter(executor, "/project")

	if converter == nil {
		t.Fatal("NewPlanToDAGConverter returned nil")
	}
	if converter.executor == nil {
		t.Error("executor should not be nil")
	}
	if converter.projectRoot != "/project" {
		t.Errorf("projectRoot = %s, want /project", converter.projectRoot)
	}
}

func TestPlanToDAGConverter_ToDAG_NilTree(t *testing.T) {
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	_, err := converter.ToDAG(nil, "test")
	if err == nil {
		t.Error("expected error for nil tree")
	}
}

func TestPlanToDAGConverter_ToDAG_EmptyBestPath(t *testing.T) {
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("test task", budget)
	// Don't set best path

	_, err := converter.ToDAG(tree, "test")
	if err == nil {
		t.Error("expected error for empty best path")
	}
}

func TestPlanToDAGConverter_ToDAG_Success(t *testing.T) {
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("test task", budget)

	// Add some children
	child1 := NewPlanNode("child-1", "First step")
	child1.SetAction(&PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Edit file",
	})
	tree.Root().AddChild(child1)
	tree.IncrementNodeCount()

	child2 := NewPlanNode("child-2", "Second step")
	child1.AddChild(child2)
	tree.IncrementNodeCount()

	// Set best path
	tree.SetBestPath([]*PlanNode{tree.Root(), child1, child2})

	dag, err := converter.ToDAG(tree, "test-task")
	if err != nil {
		t.Fatalf("ToDAG() error = %v", err)
	}

	if dag == nil {
		t.Fatal("DAG should not be nil")
	}

	// Should have 2 nodes (excluding root)
	if dag.NodeCount() != 2 {
		t.Errorf("NodeCount() = %d, want 2", dag.NodeCount())
	}
}

func TestPlanToDAGConverter_ToDAG_SingleStep(t *testing.T) {
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("single step task", budget)

	child := NewPlanNode("only-child", "Only step")
	tree.Root().AddChild(child)
	tree.IncrementNodeCount()
	tree.SetBestPath([]*PlanNode{tree.Root(), child})

	dag, err := converter.ToDAG(tree, "single")
	if err != nil {
		t.Fatalf("ToDAG() error = %v", err)
	}

	if dag.NodeCount() != 1 {
		t.Errorf("NodeCount() = %d, want 1", dag.NodeCount())
	}
}

func TestDefaultPlanStepRetryPolicy(t *testing.T) {
	policy := DefaultPlanStepRetryPolicy()

	if policy.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", policy.MaxRetries)
	}
	if policy.BackoffMs != 1000 {
		t.Errorf("BackoffMs = %d, want 1000", policy.BackoffMs)
	}
}

func TestPlanStepNode_Execute_NoAction(t *testing.T) {
	node := &PlanStepNode{
		planNode: NewPlanNode("test", "Test description"),
		executor: &NoopActionExecutor{},
	}

	result, err := node.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}

	if skipped, ok := resultMap["skipped"].(bool); !ok || !skipped {
		t.Error("should be marked as skipped")
	}
}

func TestPlanStepNode_Execute_WithAction(t *testing.T) {
	tmpDir := t.TempDir()

	planNode := NewPlanNode("test", "Test description")
	planNode.SetAction(&PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Edit test file",
	})

	node := &PlanStepNode{
		planNode:    planNode,
		executor:    &NoopActionExecutor{},
		projectRoot: tmpDir,
		retryPolicy: DefaultPlanStepRetryPolicy(),
	}

	result, err := node.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}

	if resultMap["action_type"] != "edit" {
		t.Errorf("action_type = %v, want edit", resultMap["action_type"])
	}
}

func TestPlanStepNode_Execute_WithRetry(t *testing.T) {
	tmpDir := t.TempDir()
	failCount := 0
	executor := &mockFailingExecutor{
		failUntil: 2, // Fail first 2 attempts
		count:     &failCount,
	}

	planNode := NewPlanNode("test", "Test description")
	planNode.SetAction(&PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Edit test file",
	})

	node := &PlanStepNode{
		planNode:    planNode,
		executor:    executor,
		projectRoot: tmpDir,
		retryPolicy: RetryPolicy{
			MaxRetries: 3,
			BackoffMs:  10, // Fast for testing
		},
	}

	result, err := node.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result should be a map")
	}

	attempts := resultMap["attempts"].(int)
	if attempts != 3 { // Should succeed on 3rd attempt
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestPlanStepNode_Execute_AllRetriesFail(t *testing.T) {
	executor := &mockFailingExecutor{
		failUntil: 100, // Always fail
		count:     new(int),
	}

	planNode := NewPlanNode("test", "Test description")
	planNode.SetAction(&PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Edit test file",
	})

	node := &PlanStepNode{
		planNode: planNode,
		executor: executor,
		retryPolicy: RetryPolicy{
			MaxRetries: 2,
			BackoffMs:  10,
		},
	}

	_, err := node.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error after all retries failed")
	}
}

func TestPlanStepNode_Execute_ContextCancellation(t *testing.T) {
	executor := &mockFailingExecutor{
		failUntil: 100, // Always fail
		count:     new(int),
	}

	planNode := NewPlanNode("test", "Test description")
	planNode.SetAction(&PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Edit test file",
	})

	node := &PlanStepNode{
		planNode: planNode,
		executor: executor,
		retryPolicy: RetryPolicy{
			MaxRetries: 5,
			BackoffMs:  1000, // Long backoff
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := node.Execute(ctx, nil)
	if err == nil {
		t.Error("expected context error")
	}
}

func TestPlanStepNode_PlanNode(t *testing.T) {
	planNode := NewPlanNode("test", "Test")
	stepNode := &PlanStepNode{planNode: planNode}

	if stepNode.PlanNode() != planNode {
		t.Error("PlanNode() should return the plan node")
	}
}

func TestNewPlanTreeDAGNode(t *testing.T) {
	runner := &NoopMCTSRunner{}
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	node := NewPlanTreeDAGNode("test-plan", "Test task", runner, budget, converter)

	if node == nil {
		t.Fatal("NewPlanTreeDAGNode returned nil")
	}
	if node.Name() != "test-plan" {
		t.Errorf("Name() = %s, want test-plan", node.Name())
	}
	if node.Timeout() != 5*time.Minute {
		t.Errorf("Timeout() = %v, want 5m", node.Timeout())
	}
	if node.Retryable() {
		t.Error("should not be retryable")
	}
}

func TestPlanTreeDAGNode_Execute(t *testing.T) {
	runner := &NoopMCTSRunner{}
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	node := NewPlanTreeDAGNode("test-plan", "Test task", runner, budget, converter)

	result, err := node.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	planResult, ok := result.(PlanTreeResult)
	if !ok {
		t.Fatal("result should be PlanTreeResult")
	}

	if planResult.Tree == nil {
		t.Error("Tree should not be nil")
	}
	if planResult.DAG == nil {
		t.Error("DAG should not be nil")
	}
	if planResult.StepsCount < 0 {
		t.Error("StepsCount should be >= 0")
	}
}

func TestPlanTreeDAGNode_Execute_MCTSFailure(t *testing.T) {
	runner := &NoopMCTSRunner{Err: errors.New("MCTS failed")}
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	converter := NewPlanToDAGConverter(&NoopActionExecutor{}, "")

	node := NewPlanTreeDAGNode("test-plan", "Test task", runner, budget, converter)

	_, err := node.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error from MCTS failure")
	}
}

func TestNoopActionExecutor(t *testing.T) {
	executor := &NoopActionExecutor{}
	err := executor.ExecuteAction(context.Background(), &PlannedAction{})
	if err != nil {
		t.Errorf("ExecuteAction() error = %v", err)
	}
}

// mockFailingExecutor fails until a certain number of attempts.
type mockFailingExecutor struct {
	failUntil int
	count     *int
}

func (m *mockFailingExecutor) ExecuteAction(_ context.Context, _ *PlannedAction) error {
	*m.count++
	if *m.count <= m.failUntil {
		return errors.New("simulated failure")
	}
	return nil
}
