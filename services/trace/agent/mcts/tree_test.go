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
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestNewPlanTree(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Fix auth bug", budget)

	if tree.Task != "Fix auth bug" {
		t.Errorf("Task = %s, want 'Fix auth bug'", tree.Task)
	}
	if tree.Root() == nil {
		t.Error("Root should not be nil")
	}
	if tree.Root().ID != "root" {
		t.Errorf("Root ID = %s, want 'root'", tree.Root().ID)
	}
	if tree.TotalNodes() != 1 {
		t.Errorf("TotalNodes = %d, want 1", tree.TotalNodes())
	}
	if tree.Budget() != budget {
		t.Error("Budget not set correctly")
	}
}

func TestPlanTree_NodeCount(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Add some children
	child1 := NewPlanNode("1", "Child 1")
	child2 := NewPlanNode("2", "Child 2")
	tree.Root().AddChild(child1)
	tree.Root().AddChild(child2)
	tree.IncrementNodeCount()
	tree.IncrementNodeCount()

	if tree.TotalNodes() != 3 {
		t.Errorf("TotalNodes = %d, want 3", tree.TotalNodes())
	}
}

func TestPlanTree_NodeCountConcurrency(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			tree.IncrementNodeCount()
		}()
	}

	wg.Wait()

	// 1 root + 100 increments
	expected := int64(1 + numGoroutines)
	if tree.TotalNodes() != expected {
		t.Errorf("TotalNodes = %d, want %d", tree.TotalNodes(), expected)
	}
}

func TestPlanTree_BestPath(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Initially empty
	path := tree.BestPath()
	if len(path) != 0 {
		t.Errorf("Initial BestPath should be empty, got %d", len(path))
	}

	// Set best path
	child1 := NewPlanNode("1", "Best child")
	child1.IncrementVisits()
	child1.AddScore(0.9)
	tree.Root().AddChild(child1)

	tree.SetBestPath([]*PlanNode{tree.Root(), child1})

	path = tree.BestPath()
	if len(path) != 2 {
		t.Errorf("BestPath len = %d, want 2", len(path))
	}

	score := tree.BestScore()
	if score != 0.9 {
		t.Errorf("BestScore = %f, want 0.9", score)
	}
}

func TestPlanTree_FindNode(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	child1 := NewPlanNode("1", "Child 1")
	child2 := NewPlanNode("1.1", "Child 1.1")
	tree.Root().AddChild(child1)
	child1.AddChild(child2)

	// Find root
	found := tree.FindNode("root")
	if found != tree.Root() {
		t.Error("FindNode('root') should return root")
	}

	// Find child
	found = tree.FindNode("1")
	if found != child1 {
		t.Error("FindNode('1') should return child1")
	}

	// Find nested child
	found = tree.FindNode("1.1")
	if found != child2 {
		t.Error("FindNode('1.1') should return child2")
	}

	// Find non-existent
	found = tree.FindNode("999")
	if found != nil {
		t.Error("FindNode('999') should return nil")
	}
}

func TestPlanTree_MaxDepth(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Initial depth is 0
	if tree.MaxDepth() != 0 {
		t.Errorf("Initial MaxDepth = %d, want 0", tree.MaxDepth())
	}

	// Add children
	child1 := NewPlanNode("1", "Child 1")
	tree.Root().AddChild(child1)
	if tree.MaxDepth() != 1 {
		t.Errorf("MaxDepth with 1 child = %d, want 1", tree.MaxDepth())
	}

	child2 := NewPlanNode("1.1", "Child 1.1")
	child1.AddChild(child2)
	if tree.MaxDepth() != 2 {
		t.Errorf("MaxDepth with nested child = %d, want 2", tree.MaxDepth())
	}
}

func TestPlanTree_CountByState(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	child1 := NewPlanNode("1", "Child 1")
	child1.SetState(NodeCompleted)
	child2 := NewPlanNode("2", "Child 2")
	child2.SetState(NodeAbandoned)
	child3 := NewPlanNode("3", "Child 3")

	tree.Root().AddChild(child1)
	tree.Root().AddChild(child2)
	tree.Root().AddChild(child3)

	counts := tree.CountByState()

	if counts[NodeExploring] != 1 { // root
		t.Errorf("CountByState[exploring] = %d, want 1", counts[NodeExploring])
	}
	if counts[NodeCompleted] != 1 {
		t.Errorf("CountByState[completed] = %d, want 1", counts[NodeCompleted])
	}
	if counts[NodeAbandoned] != 1 {
		t.Errorf("CountByState[abandoned] = %d, want 1", counts[NodeAbandoned])
	}
	if counts[NodeUnexplored] != 1 {
		t.Errorf("CountByState[unexplored] = %d, want 1", counts[NodeUnexplored])
	}
}

func TestPlanTree_Prune(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Add 5 children with different scores
	for i := 0; i < 5; i++ {
		child := NewPlanNode(string(rune('1'+i)), "Child")
		child.IncrementVisits()
		child.IncrementVisits()
		child.AddScore(float64(i) * 0.2) // 0.0, 0.2, 0.4, 0.6, 0.8
		tree.Root().AddChild(child)
		tree.IncrementNodeCount()
	}

	// Total should be 6 (root + 5 children)
	if tree.TotalNodes() != 6 {
		t.Errorf("TotalNodes before prune = %d, want 6", tree.TotalNodes())
	}

	// Prune to keep top 2, min 2 visits
	pruned := tree.Prune(2, 2)

	if pruned != 3 {
		t.Errorf("Pruned = %d, want 3", pruned)
	}

	if tree.TotalNodes() != 3 {
		t.Errorf("TotalNodes after prune = %d, want 3", tree.TotalNodes())
	}

	// Highest scoring children should remain
	remaining := tree.Root().Children()
	if len(remaining) != 2 {
		t.Errorf("Remaining children = %d, want 2", len(remaining))
	}
}

func TestPlanTree_PruneNoOp(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Add 2 children
	tree.Root().AddChild(NewPlanNode("1", "Child 1"))
	tree.Root().AddChild(NewPlanNode("2", "Child 2"))

	// Prune to keep top 3 (more than we have)
	pruned := tree.Prune(3, 0)

	if pruned != 0 {
		t.Errorf("Pruned = %d, want 0", pruned)
	}
}

func TestPlanTree_ExtractBestPath(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// Build a tree with varying scores
	child1 := NewPlanNode("1", "Child 1")
	child1.IncrementVisits()
	child1.AddScore(0.8)
	child1.SetState(NodeCompleted)

	child2 := NewPlanNode("2", "Child 2")
	child2.IncrementVisits()
	child2.AddScore(0.5)

	tree.Root().AddChild(child1)
	tree.Root().AddChild(child2)

	// Add children to best node
	grandchild := NewPlanNode("1.1", "Grandchild")
	grandchild.IncrementVisits()
	grandchild.AddScore(0.9)
	child1.AddChild(grandchild)

	path := tree.ExtractBestPath()

	if len(path) != 3 {
		t.Errorf("ExtractBestPath len = %d, want 3", len(path))
	}
	if path[0].ID != "root" {
		t.Error("First in path should be root")
	}
	if path[1].ID != "1" {
		t.Errorf("Second in path should be '1' (highest score), got %s", path[1].ID)
	}
	if path[2].ID != "1.1" {
		t.Error("Third in path should be '1.1'")
	}
}

func TestPlanTree_ExtractBestPath_SkipsAbandoned(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	// All children abandoned
	child := NewPlanNode("1", "Abandoned child")
	child.SetState(NodeAbandoned)
	tree.Root().AddChild(child)

	path := tree.ExtractBestPath()

	// Should stop at root
	if len(path) != 1 {
		t.Errorf("ExtractBestPath len = %d, want 1 (just root)", len(path))
	}
}

func TestPlanTree_Format(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Fix auth bug", budget)

	child1 := NewPlanNode("1", "Add nil check")
	child1.IncrementVisits()
	child1.AddScore(0.85)
	child1.SetState(NodeCompleted)
	tree.Root().AddChild(child1)

	tree.SetBestPath([]*PlanNode{tree.Root(), child1})

	output := tree.Format()

	if output == "" {
		t.Error("Format should not return empty string")
	}
	// Should contain task and node info
	if !strings.Contains(output, "Fix auth bug") {
		t.Error("Format should contain task")
	}
	if !strings.Contains(output, "Add nil check") {
		t.Error("Format should contain node description")
	}
}

func TestPlanTree_MarshalJSON(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	child := NewPlanNode("1", "Child")
	tree.Root().AddChild(child)
	tree.IncrementNodeCount()
	tree.SetBestPath([]*PlanNode{tree.Root(), child})

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["task"] != "Test task" {
		t.Errorf("JSON task = %v, want 'Test task'", result["task"])
	}
	if result["total_nodes"].(float64) != 2 {
		t.Errorf("JSON total_nodes = %v, want 2", result["total_nodes"])
	}
}

func TestPlanTree_NilRoot(t *testing.T) {
	tree := &PlanTree{}

	if tree.FindNode("any") != nil {
		t.Error("FindNode on nil root should return nil")
	}
	if tree.MaxDepth() != 0 {
		t.Error("MaxDepth on nil root should return 0")
	}
	counts := tree.CountByState()
	if len(counts) != 0 {
		t.Error("CountByState on nil root should return empty map")
	}
	if tree.Prune(2, 0) != 0 {
		t.Error("Prune on nil root should return 0")
	}
	if tree.ExtractBestPath() != nil {
		t.Error("ExtractBestPath on nil root should return nil")
	}
	if tree.Format() != "Empty tree" {
		t.Error("Format on nil root should return 'Empty tree'")
	}
}
