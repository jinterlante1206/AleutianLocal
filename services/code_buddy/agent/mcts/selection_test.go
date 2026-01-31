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
	"math"
	"sync"
	"testing"
)

func TestNewUCB1Policy(t *testing.T) {
	t.Run("default exploration constant", func(t *testing.T) {
		policy := NewUCB1Policy(0) // 0 should default to sqrt(2)
		if policy.ExplorationConstant != math.Sqrt(2) {
			t.Errorf("expected sqrt(2), got %v", policy.ExplorationConstant)
		}
	})

	t.Run("custom exploration constant", func(t *testing.T) {
		policy := NewUCB1Policy(2.0)
		if policy.ExplorationConstant != 2.0 {
			t.Errorf("expected 2.0, got %v", policy.ExplorationConstant)
		}
	})
}

func TestUCB1Policy_Select(t *testing.T) {
	t.Run("no children returns nil", func(t *testing.T) {
		policy := NewUCB1Policy(1.41)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()

		selected := policy.Select(parent)
		if selected != nil {
			t.Error("expected nil for node with no children")
		}
	})

	t.Run("selects unexplored first", func(t *testing.T) {
		policy := NewUCB1Policy(1.41)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()
		parent.IncrementVisits()

		// Child 1: explored
		child1 := NewPlanNode("1", "Explored child")
		child1.IncrementVisits()
		child1.AddScore(0.5)
		parent.AddChild(child1)

		// Child 2: unexplored
		child2 := NewPlanNode("2", "Unexplored child")
		parent.AddChild(child2)

		selected := policy.Select(parent)
		if selected != child2 {
			t.Error("expected unexplored child to be selected")
		}
	})

	t.Run("balances exploration and exploitation", func(t *testing.T) {
		policy := NewUCB1Policy(1.41)
		parent := NewPlanNode("root", "Root node")
		for i := 0; i < 100; i++ {
			parent.IncrementVisits()
		}

		// Child 1: high score, many visits (exploited)
		child1 := NewPlanNode("1", "Exploited child")
		for i := 0; i < 50; i++ {
			child1.IncrementVisits()
		}
		child1.AddScore(40) // avg = 0.8
		parent.AddChild(child1)

		// Child 2: lower score, few visits (should explore)
		child2 := NewPlanNode("2", "Under-explored child")
		for i := 0; i < 5; i++ {
			child2.IncrementVisits()
		}
		child2.AddScore(2.5) // avg = 0.5
		parent.AddChild(child2)

		// With UCB1, child2 should have higher score due to exploration bonus
		// UCB1(child1) = 0.8 + 1.41 * sqrt(ln(100)/50) ≈ 0.8 + 0.42 = 1.22
		// UCB1(child2) = 0.5 + 1.41 * sqrt(ln(100)/5) ≈ 0.5 + 1.35 = 1.85

		selected := policy.Select(parent)
		if selected != child2 {
			t.Error("expected under-explored child to be selected due to UCB1 bonus")
		}
	})

	t.Run("skips abandoned children", func(t *testing.T) {
		policy := NewUCB1Policy(1.41)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()

		// Abandoned child (high score but abandoned)
		abandoned := NewPlanNode("abandoned", "Abandoned child")
		abandoned.IncrementVisits()
		abandoned.AddScore(1.0)
		abandoned.SetState(NodeAbandoned)
		parent.AddChild(abandoned)

		// Normal child (lower score)
		normal := NewPlanNode("normal", "Normal child")
		normal.IncrementVisits()
		normal.AddScore(0.5)
		parent.AddChild(normal)

		selected := policy.Select(parent)
		if selected != normal {
			t.Error("expected normal child, not abandoned")
		}
	})

	t.Run("returns nil when all children abandoned", func(t *testing.T) {
		policy := NewUCB1Policy(1.41)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()

		child := NewPlanNode("child", "Abandoned child")
		child.SetState(NodeAbandoned)
		parent.AddChild(child)

		selected := policy.Select(parent)
		if selected != nil {
			t.Error("expected nil when all children abandoned")
		}
	})
}

func TestUCB1Score(t *testing.T) {
	node := NewPlanNode("test", "Test node")
	node.IncrementVisits()
	node.IncrementVisits()
	node.AddScore(1.6) // avg = 0.8

	score := UCB1Score(node, 10, 1.41)

	// UCB1 = 0.8 + 1.41 * sqrt(ln(10)/2) ≈ 0.8 + 1.51 = 2.31
	expected := 0.8 + 1.41*math.Sqrt(math.Log(10)/2)
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected ~%.2f, got %.2f", expected, score)
	}
}

func TestUCB1Score_Unvisited(t *testing.T) {
	node := NewPlanNode("test", "Unvisited node")

	score := UCB1Score(node, 10, 1.41)

	if !math.IsInf(score, 1) {
		t.Errorf("expected +Inf for unvisited node, got %v", score)
	}
}

func TestNewPUCTPolicy(t *testing.T) {
	t.Run("default exploration constant", func(t *testing.T) {
		policy := NewPUCTPolicy(0)
		if policy.ExplorationConstant != 1.5 {
			t.Errorf("expected 1.5, got %v", policy.ExplorationConstant)
		}
	})

	t.Run("custom exploration constant", func(t *testing.T) {
		policy := NewPUCTPolicy(2.0)
		if policy.ExplorationConstant != 2.0 {
			t.Errorf("expected 2.0, got %v", policy.ExplorationConstant)
		}
	})
}

func TestPUCTPolicy_SetPrior(t *testing.T) {
	policy := NewPUCTPolicy(1.5)

	policy.SetPrior("node1", 0.8)
	policy.SetPrior("node2", -0.5) // Should clamp to 0
	policy.SetPrior("node3", 1.5)  // Should clamp to 1

	if prior := policy.GetPrior("node1", 3); prior != 0.8 {
		t.Errorf("expected 0.8, got %v", prior)
	}
	if prior := policy.GetPrior("node2", 3); prior != 0 {
		t.Errorf("expected 0 (clamped), got %v", prior)
	}
	if prior := policy.GetPrior("node3", 3); prior != 1 {
		t.Errorf("expected 1 (clamped), got %v", prior)
	}
}

func TestPUCTPolicy_SetPriors(t *testing.T) {
	policy := NewPUCTPolicy(1.5)

	priors := map[string]float64{
		"node1": 0.7,
		"node2": 0.2,
		"node3": 0.1,
	}
	policy.SetPriors(priors)

	if prior := policy.GetPrior("node1", 3); prior != 0.7 {
		t.Errorf("expected 0.7, got %v", prior)
	}
}

func TestPUCTPolicy_GetPrior_Default(t *testing.T) {
	policy := NewPUCTPolicy(1.5)

	// Node with no set prior should get uniform distribution
	prior := policy.GetPrior("unknown", 4)
	if prior != 0.25 {
		t.Errorf("expected 0.25 (1/4), got %v", prior)
	}
}

func TestPUCTPolicy_Select(t *testing.T) {
	t.Run("no children returns nil", func(t *testing.T) {
		policy := NewPUCTPolicy(1.5)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()

		selected := policy.Select(parent)
		if selected != nil {
			t.Error("expected nil for node with no children")
		}
	})

	t.Run("respects priors for unexplored nodes", func(t *testing.T) {
		policy := NewPUCTPolicy(1.5)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()
		parent.IncrementVisits()

		// Two unexplored children with different priors
		child1 := NewPlanNode("1", "Low prior")
		parent.AddChild(child1)
		policy.SetPrior("1", 0.2)

		child2 := NewPlanNode("2", "High prior")
		parent.AddChild(child2)
		policy.SetPrior("2", 0.8)

		selected := policy.Select(parent)
		if selected != child2 {
			t.Error("expected high-prior child to be selected")
		}
	})

	t.Run("balances prior and visits", func(t *testing.T) {
		policy := NewPUCTPolicy(1.5)
		parent := NewPlanNode("root", "Root node")
		for i := 0; i < 100; i++ {
			parent.IncrementVisits()
		}

		// Child 1: high score but low prior
		child1 := NewPlanNode("1", "High score, low prior")
		for i := 0; i < 50; i++ {
			child1.IncrementVisits()
		}
		child1.AddScore(45) // avg = 0.9
		parent.AddChild(child1)
		policy.SetPrior("1", 0.1)

		// Child 2: lower score but high prior and fewer visits
		child2 := NewPlanNode("2", "Lower score, high prior")
		for i := 0; i < 5; i++ {
			child2.IncrementVisits()
		}
		child2.AddScore(3) // avg = 0.6
		parent.AddChild(child2)
		policy.SetPrior("2", 0.9)

		// PUCT should favor child2 due to high prior and low visits
		selected := policy.Select(parent)
		if selected != child2 {
			t.Error("expected high-prior, low-visit child to be selected")
		}
	})

	t.Run("skips abandoned children", func(t *testing.T) {
		policy := NewPUCTPolicy(1.5)
		parent := NewPlanNode("root", "Root node")
		parent.IncrementVisits()

		abandoned := NewPlanNode("abandoned", "Abandoned")
		abandoned.SetState(NodeAbandoned)
		parent.AddChild(abandoned)
		policy.SetPrior("abandoned", 0.99)

		normal := NewPlanNode("normal", "Normal")
		parent.AddChild(normal)
		policy.SetPrior("normal", 0.01)

		selected := policy.Select(parent)
		if selected != normal {
			t.Error("expected normal child despite lower prior")
		}
	})
}

func TestPUCTScore(t *testing.T) {
	node := NewPlanNode("test", "Test node")
	node.IncrementVisits()
	node.IncrementVisits()
	node.AddScore(1.2) // avg = 0.6

	score := PUCTScore(node, 10, 0.8, 1.5)

	// PUCT = 0.6 + 1.5 * 0.8 * sqrt(10) / (1 + 2)
	// = 0.6 + 1.2 * 3.16 / 3 = 0.6 + 1.26 = 1.86
	expected := 0.6 + 1.5*0.8*math.Sqrt(10)/(1+2)
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected ~%.2f, got %.2f", expected, score)
	}
}

func TestPUCTScore_Unvisited(t *testing.T) {
	node := NewPlanNode("test", "Unvisited node")

	score := PUCTScore(node, 10, 0.8, 1.5)

	// For unvisited: PUCT = C * prior * sqrt(parentVisits)
	expected := 1.5 * 0.8 * math.Sqrt(10)
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected ~%.2f, got %.2f", expected, score)
	}
}

func TestTreeTraversal(t *testing.T) {
	t.Run("nil tree returns nil", func(t *testing.T) {
		node, path := TreeTraversal(nil, DefaultSelectionPolicy())
		if node != nil || path != nil {
			t.Error("expected nil for nil tree")
		}
	})

	t.Run("single node tree returns root", func(t *testing.T) {
		budget := NewTreeBudget(DefaultTreeBudgetConfig())
		tree := NewPlanTree("Test task", budget)

		leaf, path := TreeTraversal(tree, DefaultSelectionPolicy())

		if leaf != tree.Root() {
			t.Error("expected root as leaf")
		}
		if len(path) != 1 || path[0] != tree.Root() {
			t.Error("expected path with just root")
		}
	})

	t.Run("traverses to leaf", func(t *testing.T) {
		budget := NewTreeBudget(DefaultTreeBudgetConfig())
		tree := NewPlanTree("Test task", budget)

		// Build a small tree
		child1 := NewPlanNode("1", "Child 1")
		child1.IncrementVisits()
		child1.AddScore(0.5)
		tree.Root().AddChild(child1)
		tree.IncrementNodeCount()

		grandchild := NewPlanNode("1.1", "Grandchild")
		child1.AddChild(grandchild)
		tree.IncrementNodeCount()

		tree.Root().IncrementVisits()
		tree.Root().IncrementVisits()

		leaf, path := TreeTraversal(tree, DefaultSelectionPolicy())

		if leaf != grandchild {
			t.Error("expected grandchild as leaf")
		}
		if len(path) != 3 {
			t.Errorf("expected path length 3, got %d", len(path))
		}
	})
}

func TestSelectWithVirtualLoss(t *testing.T) {
	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree := NewPlanTree("Test task", budget)

	child := NewPlanNode("1", "Child")
	tree.Root().AddChild(child)
	tree.IncrementNodeCount()

	initialRootVisits := tree.Root().Visits()
	initialChildVisits := child.Visits()

	leaf, path, release := SelectWithVirtualLoss(tree, DefaultSelectionPolicy(), 0.5)

	// After traversal with virtual loss
	if tree.Root().Visits() != initialRootVisits+1 {
		t.Error("expected root visits to increase by 1")
	}
	if child.Visits() != initialChildVisits+1 {
		t.Error("expected child visits to increase by 1")
	}

	if leaf != child {
		t.Error("expected child as leaf")
	}
	if len(path) != 2 {
		t.Error("expected path length 2")
	}

	// Release should restore scores (but not visits)
	release()

	// Visits should remain incremented
	if tree.Root().Visits() != initialRootVisits+1 {
		t.Error("visits should remain after release")
	}
}

func TestSelectionPolicy_ThreadSafety(t *testing.T) {
	policy := NewPUCTPolicy(1.5)
	parent := NewPlanNode("root", "Root")
	parent.IncrementVisits()

	// Add children
	for i := 0; i < 5; i++ {
		child := NewPlanNode(string(rune('A'+i)), "Child")
		child.IncrementVisits()
		child.AddScore(float64(i) * 0.1)
		parent.AddChild(child)
	}

	var wg sync.WaitGroup

	// Concurrent selections
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = policy.Select(parent)
		}()
	}

	// Concurrent prior updates
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := string(rune('A' + idx%5))
			policy.SetPrior(nodeID, float64(idx%100)/100)
		}(i)
	}

	wg.Wait() // Should not deadlock or panic
}

func TestDefaultSelectionPolicy(t *testing.T) {
	policy := DefaultSelectionPolicy()

	ucb1, ok := policy.(*UCB1Policy)
	if !ok {
		t.Fatal("expected UCB1Policy")
	}

	if ucb1.ExplorationConstant != math.Sqrt(2) {
		t.Errorf("expected sqrt(2), got %v", ucb1.ExplorationConstant)
	}
}
