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

func TestDefaultExpansionConfig(t *testing.T) {
	config := DefaultExpansionConfig()

	if config.MaxChildren != 3 {
		t.Errorf("expected MaxChildren=3, got %d", config.MaxChildren)
	}
	if config.MinChildren != 1 {
		t.Errorf("expected MinChildren=1, got %d", config.MinChildren)
	}
	if !config.GeneratePriors {
		t.Error("expected GeneratePriors=true")
	}
	if !config.IncludeActions {
		t.Error("expected IncludeActions=true")
	}
}

func TestDefaultProgressiveWideningConfig(t *testing.T) {
	config := DefaultProgressiveWideningConfig()

	if !config.Enabled {
		t.Error("expected Enabled=true")
	}
	if config.Alpha != 0.5 {
		t.Errorf("expected Alpha=0.5, got %v", config.Alpha)
	}
	if config.K != 1.0 {
		t.Errorf("expected K=1.0, got %v", config.K)
	}
	if config.MinChildren != 1 {
		t.Errorf("expected MinChildren=1, got %d", config.MinChildren)
	}
	if config.MaxChildren != 10 {
		t.Errorf("expected MaxChildren=10, got %d", config.MaxChildren)
	}
}

func TestShouldExpand_Disabled(t *testing.T) {
	config := ProgressiveWideningConfig{
		Enabled:     false,
		MaxChildren: 5,
	}

	node := NewPlanNode("test", "Test node")

	shouldExpand, maxNew := ShouldExpand(node, config)

	if !shouldExpand {
		t.Error("expected shouldExpand=true when disabled")
	}
	if maxNew != 5 {
		t.Errorf("expected maxNew=5, got %d", maxNew)
	}
}

func TestShouldExpand_DisabledAtMax(t *testing.T) {
	config := ProgressiveWideningConfig{
		Enabled:     false,
		MaxChildren: 2,
	}

	node := NewPlanNode("test", "Test node")
	node.AddChild(NewPlanNode("1", "Child 1"))
	node.AddChild(NewPlanNode("2", "Child 2"))

	shouldExpand, maxNew := ShouldExpand(node, config)

	if shouldExpand {
		t.Error("expected shouldExpand=false at max children")
	}
	if maxNew != 0 {
		t.Errorf("expected maxNew=0, got %d", maxNew)
	}
}

func TestShouldExpand_ProgressiveWidening(t *testing.T) {
	config := ProgressiveWideningConfig{
		Enabled:     true,
		Alpha:       0.5,
		K:           1.0,
		MinChildren: 1,
		MaxChildren: 10,
	}

	t.Run("unvisited node allows MinChildren", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		// visits = 0, formula gives k * 0^alpha = 0, but min is 1

		shouldExpand, maxNew := ShouldExpand(node, config)

		if !shouldExpand {
			t.Error("expected shouldExpand=true")
		}
		if maxNew != 1 {
			t.Errorf("expected maxNew=1, got %d", maxNew)
		}
	})

	t.Run("visited node allows more children", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		for i := 0; i < 4; i++ {
			node.IncrementVisits()
		}
		// visits = 4, formula gives k * 4^0.5 = 2

		shouldExpand, maxNew := ShouldExpand(node, config)

		if !shouldExpand {
			t.Error("expected shouldExpand=true")
		}
		if maxNew != 2 {
			t.Errorf("expected maxNew=2, got %d", maxNew)
		}
	})

	t.Run("highly visited node allows many children", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		for i := 0; i < 100; i++ {
			node.IncrementVisits()
		}
		// visits = 100, formula gives k * 100^0.5 = 10

		shouldExpand, maxNew := ShouldExpand(node, config)

		if !shouldExpand {
			t.Error("expected shouldExpand=true")
		}
		if maxNew != 10 {
			t.Errorf("expected maxNew=10, got %d", maxNew)
		}
	})

	t.Run("respects max cap", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		for i := 0; i < 10000; i++ {
			node.IncrementVisits()
		}
		// visits = 10000, formula gives k * 10000^0.5 = 100, but capped at 10

		shouldExpand, maxNew := ShouldExpand(node, config)

		if !shouldExpand {
			t.Error("expected shouldExpand=true")
		}
		if maxNew != 10 {
			t.Errorf("expected maxNew=10 (capped), got %d", maxNew)
		}
	})

	t.Run("accounts for existing children", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		for i := 0; i < 4; i++ {
			node.IncrementVisits()
		}
		node.AddChild(NewPlanNode("1", "Existing child"))
		// visits = 4, formula gives 2, but 1 child exists, so 1 more allowed

		shouldExpand, maxNew := ShouldExpand(node, config)

		if !shouldExpand {
			t.Error("expected shouldExpand=true")
		}
		if maxNew != 1 {
			t.Errorf("expected maxNew=1, got %d", maxNew)
		}
	})

	t.Run("no expansion when at limit", func(t *testing.T) {
		node := NewPlanNode("test", "Test node")
		for i := 0; i < 4; i++ {
			node.IncrementVisits()
		}
		node.AddChild(NewPlanNode("1", "Child 1"))
		node.AddChild(NewPlanNode("2", "Child 2"))
		// visits = 4, formula gives 2, already have 2 children

		shouldExpand, maxNew := ShouldExpand(node, config)

		if shouldExpand {
			t.Error("expected shouldExpand=false")
		}
		if maxNew != 0 {
			t.Errorf("expected maxNew=0, got %d", maxNew)
		}
	})
}

func TestMockExpander_Expand(t *testing.T) {
	t.Run("basic expansion", func(t *testing.T) {
		expander := NewMockExpander(3)
		parent := NewPlanNode("root", "Root node")
		budget := NewTreeBudget(DefaultTreeBudgetConfig())

		children, priors, err := expander.Expand(context.Background(), parent, budget)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(children) != 3 {
			t.Errorf("expected 3 children, got %d", len(children))
		}
		if len(priors) != 3 {
			t.Errorf("expected 3 priors, got %d", len(priors))
		}

		// Check uniform priors
		for i, p := range priors {
			expected := 1.0 / 3.0
			if p < expected-0.01 || p > expected+0.01 {
				t.Errorf("prior[%d] = %v, expected ~%v", i, p, expected)
			}
		}
	})

	t.Run("with error", func(t *testing.T) {
		expander := NewMockExpander(3)
		expander.Err = errors.New("expansion failed")
		parent := NewPlanNode("root", "Root node")
		budget := NewTreeBudget(DefaultTreeBudgetConfig())

		_, _, err := expander.Expand(context.Background(), parent, budget)

		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("with custom priors", func(t *testing.T) {
		expander := NewMockExpander(3)
		expander.Priors = []float64{0.5, 0.3, 0.2}
		parent := NewPlanNode("root", "Root node")
		budget := NewTreeBudget(DefaultTreeBudgetConfig())

		_, priors, err := expander.Expand(context.Background(), parent, budget)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if priors[0] != 0.5 || priors[1] != 0.3 || priors[2] != 0.2 {
			t.Errorf("priors don't match: %v", priors)
		}
	})

	t.Run("with custom generator", func(t *testing.T) {
		expander := NewMockExpander(2)
		expander.ChildGenerator = func(parent *PlanNode, count int) ([]*PlanNode, []float64) {
			children := []*PlanNode{
				NewPlanNode("custom-1", "Custom child 1"),
				NewPlanNode("custom-2", "Custom child 2"),
			}
			return children, []float64{0.6, 0.4}
		}
		parent := NewPlanNode("root", "Root node")
		budget := NewTreeBudget(DefaultTreeBudgetConfig())

		children, priors, err := expander.Expand(context.Background(), parent, budget)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if children[0].ID != "custom-1" {
			t.Error("expected custom child")
		}
		if priors[0] != 0.6 {
			t.Errorf("expected prior 0.6, got %v", priors[0])
		}
	})
}

func TestBuildExpansionContext(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Fix authentication bug", budget)

	// Build tree structure
	child1 := NewPlanNode("1", "Approach 1: Add validation")
	child2 := NewPlanNode("2", "Approach 2: Refactor auth")
	tree.Root().AddChild(child1)
	tree.Root().AddChild(child2)
	tree.IncrementNodeCount()
	tree.IncrementNodeCount()

	grandchild := NewPlanNode("1.1", "Step: Validate input")
	child1.AddChild(grandchild)
	tree.IncrementNodeCount()

	ctx := BuildExpansionContext(tree, grandchild, budget)

	if ctx.Task != "Fix authentication bug" {
		t.Errorf("expected task 'Fix authentication bug', got '%s'", ctx.Task)
	}

	if ctx.CurrentDepth != 2 {
		t.Errorf("expected depth 2, got %d", ctx.CurrentDepth)
	}

	if len(ctx.PathFromRoot) != 3 {
		t.Errorf("expected path length 3, got %d", len(ctx.PathFromRoot))
	}

	// Grandchild has no siblings
	if len(ctx.SiblingDescriptions) != 0 {
		t.Errorf("expected 0 siblings, got %d", len(ctx.SiblingDescriptions))
	}
}

func TestExpandAndIntegrate(t *testing.T) {
	t.Run("successful expansion", func(t *testing.T) {
		budget := NewTreeBudget(TreeBudgetConfig{
			MaxNodes:      100,
			MaxDepth:      5,
			MaxExpansions: 10,
		})
		tree := NewPlanTree("Test task", budget)
		expander := NewMockExpander(3)
		pwConfig := DefaultProgressiveWideningConfig()
		puctPolicy := NewPUCTPolicy(1.5)

		tree.Root().IncrementVisits()

		children, err := ExpandAndIntegrate(
			context.Background(),
			tree,
			tree.Root(),
			expander,
			budget,
			pwConfig,
			puctPolicy,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// With 1 visit and alpha=0.5, k=1.0, we get max 1 child
		if len(children) != 1 {
			t.Errorf("expected 1 child (progressive widening), got %d", len(children))
		}

		if tree.Root().ChildCount() != 1 {
			t.Errorf("expected 1 child attached to root, got %d", tree.Root().ChildCount())
		}

		if tree.TotalNodes() != 2 { // root + 1 child
			t.Errorf("expected 2 nodes, got %d", tree.TotalNodes())
		}
	})

	t.Run("respects budget exhaustion", func(t *testing.T) {
		budget := NewTreeBudget(TreeBudgetConfig{
			MaxNodes:      1, // Only root allowed
			MaxDepth:      5,
			MaxExpansions: 10,
		})
		budget.RecordNodeExplored() // Exhaust budget

		tree := NewPlanTree("Test task", budget)
		expander := NewMockExpander(3)
		pwConfig := DefaultProgressiveWideningConfig()

		_, err := ExpandAndIntegrate(
			context.Background(),
			tree,
			tree.Root(),
			expander,
			budget,
			pwConfig,
			nil,
		)

		if !errors.Is(err, ErrBudgetExhausted) {
			t.Errorf("expected ErrBudgetExhausted, got %v", err)
		}
	})

	t.Run("respects depth limit", func(t *testing.T) {
		budget := NewTreeBudget(TreeBudgetConfig{
			MaxNodes:      100,
			MaxDepth:      1, // Only depth 0 allowed
			MaxExpansions: 10,
		})
		tree := NewPlanTree("Test task", budget)

		// Create a child at depth 1
		child := NewPlanNode("1", "Child")
		tree.Root().AddChild(child)

		expander := NewMockExpander(3)
		pwConfig := DefaultProgressiveWideningConfig()

		_, err := ExpandAndIntegrate(
			context.Background(),
			tree,
			child, // Expanding child would create depth 2
			expander,
			budget,
			pwConfig,
			nil,
		)

		if !errors.Is(err, ErrDepthLimitExceeded) {
			t.Errorf("expected ErrDepthLimitExceeded, got %v", err)
		}
	})

	t.Run("sets PUCT priors", func(t *testing.T) {
		budget := NewTreeBudget(TreeBudgetConfig{
			MaxNodes:      100,
			MaxDepth:      5,
			MaxExpansions: 10,
		})
		tree := NewPlanTree("Test task", budget)
		expander := NewMockExpander(2)
		expander.Priors = []float64{0.7, 0.3}

		pwConfig := DefaultProgressiveWideningConfig()
		pwConfig.Enabled = false // Disable to get all children

		puctPolicy := NewPUCTPolicy(1.5)

		children, err := ExpandAndIntegrate(
			context.Background(),
			tree,
			tree.Root(),
			expander,
			budget,
			pwConfig,
			puctPolicy,
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check priors were set
		prior0 := puctPolicy.GetPrior(children[0].ID, 2)
		prior1 := puctPolicy.GetPrior(children[1].ID, 2)

		if prior0 != 0.7 {
			t.Errorf("expected prior 0.7 for child 0, got %v", prior0)
		}
		if prior1 != 0.3 {
			t.Errorf("expected prior 0.3 for child 1, got %v", prior1)
		}
	})

	t.Run("returns nil when progressive widening blocks expansion", func(t *testing.T) {
		budget := NewTreeBudget(DefaultTreeBudgetConfig())
		tree := NewPlanTree("Test task", budget)

		// Add child to root (no visits, so max 1 child allowed)
		tree.Root().AddChild(NewPlanNode("existing", "Existing child"))

		expander := NewMockExpander(3)
		pwConfig := DefaultProgressiveWideningConfig()

		children, err := ExpandAndIntegrate(
			context.Background(),
			tree,
			tree.Root(),
			expander,
			budget,
			pwConfig,
			nil,
		)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if children != nil {
			t.Error("expected nil children when expansion blocked")
		}
	})
}
