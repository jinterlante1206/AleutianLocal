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

func TestNewPlanNode(t *testing.T) {
	node := NewPlanNode("1", "Add nil check")

	if node.ID != "1" {
		t.Errorf("ID = %s, want 1", node.ID)
	}
	if node.Description != "Add nil check" {
		t.Errorf("Description = %s, want 'Add nil check'", node.Description)
	}
	if node.State() != NodeUnexplored {
		t.Errorf("State = %s, want unexplored", node.State())
	}
	if node.Visits() != 0 {
		t.Errorf("Visits = %d, want 0", node.Visits())
	}
	if node.ContentHash == "" {
		t.Error("ContentHash should not be empty")
	}
}

func TestNewPlanNode_WithOptions(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Edit file",
	}
	parent := NewPlanNode("root", "Root")

	node := NewPlanNode("1.1", "Child node",
		WithAction(action),
		WithParent(parent))

	if node.Action() != action {
		t.Error("Action not set correctly")
	}
	if node.Parent() != parent {
		t.Error("Parent not set correctly")
	}
	if node.Depth != 1 {
		t.Errorf("Depth = %d, want 1", node.Depth)
	}
}

func TestPlanNode_VisitsConcurrency(t *testing.T) {
	node := NewPlanNode("1", "Test concurrency")

	const numGoroutines = 100
	const numIncrements = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numIncrements; j++ {
				node.IncrementVisits()
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * numIncrements)
	if node.Visits() != expected {
		t.Errorf("Visits = %d, want %d", node.Visits(), expected)
	}
}

func TestPlanNode_ScoreConcurrency(t *testing.T) {
	node := NewPlanNode("1", "Test score concurrency")

	const numGoroutines = 100
	const scorePerGoroutine = 1.0

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			node.AddScore(scorePerGoroutine)
		}()
	}

	wg.Wait()

	expected := float64(numGoroutines) * scorePerGoroutine
	got := node.TotalScore()
	if got != expected {
		t.Errorf("TotalScore = %f, want %f", got, expected)
	}
}

func TestPlanNode_AvgScore(t *testing.T) {
	node := NewPlanNode("1", "Test avg score")

	// No visits = 0 avg
	if node.AvgScore() != 0 {
		t.Errorf("AvgScore with 0 visits = %f, want 0", node.AvgScore())
	}

	// Add some scores
	node.IncrementVisits()
	node.AddScore(0.8)
	if node.AvgScore() != 0.8 {
		t.Errorf("AvgScore = %f, want 0.8", node.AvgScore())
	}

	node.IncrementVisits()
	node.AddScore(0.6)
	expected := (0.8 + 0.6) / 2.0
	if node.AvgScore() != expected {
		t.Errorf("AvgScore = %f, want %f", node.AvgScore(), expected)
	}
}

func TestPlanNode_Children(t *testing.T) {
	parent := NewPlanNode("root", "Root")
	child1 := NewPlanNode("1", "Child 1")
	child2 := NewPlanNode("2", "Child 2")

	parent.AddChild(child1)
	parent.AddChild(child2)

	if parent.ChildCount() != 2 {
		t.Errorf("ChildCount = %d, want 2", parent.ChildCount())
	}

	children := parent.Children()
	if len(children) != 2 {
		t.Errorf("len(Children) = %d, want 2", len(children))
	}

	// Children should have parent set
	if child1.Parent() != parent {
		t.Error("child1.Parent() should be parent")
	}
	if child1.Depth != 1 {
		t.Errorf("child1.Depth = %d, want 1", child1.Depth)
	}

	// Remove child
	removed := parent.RemoveChild("1")
	if !removed {
		t.Error("RemoveChild should return true")
	}
	if parent.ChildCount() != 1 {
		t.Errorf("ChildCount after remove = %d, want 1", parent.ChildCount())
	}

	// Remove non-existent
	removed = parent.RemoveChild("999")
	if removed {
		t.Error("RemoveChild for non-existent should return false")
	}
}

func TestPlanNode_ChildrenConcurrency(t *testing.T) {
	parent := NewPlanNode("root", "Root")

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			child := NewPlanNode(string(rune('a'+idx)), "Child")
			parent.AddChild(child)
		}(i)
	}

	wg.Wait()

	if parent.ChildCount() != numGoroutines {
		t.Errorf("ChildCount = %d, want %d", parent.ChildCount(), numGoroutines)
	}
}

func TestPlanNode_State(t *testing.T) {
	node := NewPlanNode("1", "Test state")

	if node.State() != NodeUnexplored {
		t.Errorf("Initial state = %s, want unexplored", node.State())
	}

	node.SetState(NodeExploring)
	if node.State() != NodeExploring {
		t.Errorf("State = %s, want exploring", node.State())
	}

	node.SetState(NodeCompleted)
	if node.State() != NodeCompleted {
		t.Errorf("State = %s, want completed", node.State())
	}
}

func TestPlanNode_SimulationResult(t *testing.T) {
	node := NewPlanNode("1", "Test simulation")

	if node.IsSimulated() {
		t.Error("IsSimulated should be false initially")
	}

	result := &SimulationResult{
		Score: 0.85,
		Signals: map[string]float64{
			"syntax": 1.0,
			"lint":   0.7,
		},
	}

	node.SetSimulationResult(result)

	if !node.IsSimulated() {
		t.Error("IsSimulated should be true after setting result")
	}

	got := node.SimulationResult()
	if got.Score != result.Score {
		t.Errorf("SimulationResult.Score = %f, want %f", got.Score, result.Score)
	}
}

func TestPlanNode_PathFromRoot(t *testing.T) {
	root := NewPlanNode("root", "Root")
	child1 := NewPlanNode("1", "Child 1")
	child2 := NewPlanNode("1.1", "Child 1.1")

	root.AddChild(child1)
	child1.AddChild(child2)

	path := child2.PathFromRoot()
	if len(path) != 3 {
		t.Errorf("len(PathFromRoot) = %d, want 3", len(path))
	}
	if path[0].ID != "root" {
		t.Error("First in path should be root")
	}
	if path[1].ID != "1" {
		t.Error("Second in path should be child1")
	}
	if path[2].ID != "1.1" {
		t.Error("Third in path should be child2")
	}

	ids := child2.PathIDs()
	if len(ids) != 3 {
		t.Errorf("len(PathIDs) = %d, want 3", len(ids))
	}
}

func TestPlanNode_IsRootIsLeaf(t *testing.T) {
	root := NewPlanNode("root", "Root")
	child := NewPlanNode("1", "Child")

	if !root.IsRoot() {
		t.Error("root.IsRoot() should be true")
	}
	if !root.IsLeaf() {
		t.Error("root.IsLeaf() should be true (no children)")
	}

	root.AddChild(child)

	if !root.IsRoot() {
		t.Error("root.IsRoot() should still be true")
	}
	if root.IsLeaf() {
		t.Error("root.IsLeaf() should be false (has children)")
	}
	if child.IsRoot() {
		t.Error("child.IsRoot() should be false")
	}
	if !child.IsLeaf() {
		t.Error("child.IsLeaf() should be true")
	}
}

func TestPlanNode_NeedsExpansion(t *testing.T) {
	node := NewPlanNode("1", "Test")

	// Initially needs expansion
	if !node.NeedsExpansion() {
		t.Error("NeedsExpansion should be true initially")
	}

	// After adding children, doesn't need expansion
	node.AddChild(NewPlanNode("1.1", "Child"))
	if node.NeedsExpansion() {
		t.Error("NeedsExpansion should be false after adding children")
	}

	// New node with state change
	node2 := NewPlanNode("2", "Test 2")
	node2.SetState(NodeExploring)
	if node2.NeedsExpansion() {
		t.Error("NeedsExpansion should be false when not unexplored")
	}
}

func TestPlanNode_String(t *testing.T) {
	node := NewPlanNode("1", "Test node")
	node.IncrementVisits()
	node.AddScore(0.75)

	str := node.String()
	if str == "" {
		t.Error("String() should not be empty")
	}
	// Should contain key info
	if !strings.Contains(str, "1") {
		t.Error("String() should contain ID")
	}
}

func TestPlanNode_MarshalJSON(t *testing.T) {
	node := NewPlanNode("1", "Test JSON")
	node.IncrementVisits()
	node.AddScore(0.8)
	node.SetState(NodeCompleted)

	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Edit file",
	}
	node.SetAction(action)

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	// Unmarshal to verify structure
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["id"] != "1" {
		t.Errorf("JSON id = %v, want 1", result["id"])
	}
	if result["state"] != "completed" {
		t.Errorf("JSON state = %v, want completed", result["state"])
	}
	if result["visits"].(float64) != 1 {
		t.Errorf("JSON visits = %v, want 1", result["visits"])
	}
}

func TestNodeState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    NodeState
		terminal bool
	}{
		{NodeUnexplored, false},
		{NodeExploring, false},
		{NodeCompleted, true},
		{NodeAbandoned, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if tt.state.IsTerminal() != tt.terminal {
				t.Errorf("%s.IsTerminal() = %v, want %v", tt.state, tt.state.IsTerminal(), tt.terminal)
			}
		})
	}
}

func TestNodeState_String(t *testing.T) {
	tests := []struct {
		state NodeState
		want  string
	}{
		{NodeUnexplored, "unexplored"},
		{NodeExploring, "exploring"},
		{NodeCompleted, "completed"},
		{NodeAbandoned, "abandoned"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if tt.state.String() != tt.want {
				t.Errorf("%s.String() = %s, want %s", tt.state, tt.state.String(), tt.want)
			}
		})
	}
}

func TestPlanNode_ContentHashChanges(t *testing.T) {
	node := NewPlanNode("1", "Original")
	originalHash := node.ContentHash

	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Edit",
		CodeDiff:    "some code",
	}
	node.SetAction(action)

	if node.ContentHash == originalHash {
		t.Error("ContentHash should change when action is set")
	}
}
