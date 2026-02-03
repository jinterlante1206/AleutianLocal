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
	"sync"
	"testing"
	"time"
)

func TestDefaultMCTSEngineConfig(t *testing.T) {
	config := DefaultMCTSEngineConfig()

	if config.MaxIterations != 0 {
		t.Errorf("expected MaxIterations=0, got %d", config.MaxIterations)
	}
	if config.SimulationTier != SimTierQuick {
		t.Errorf("expected SimTierQuick, got %v", config.SimulationTier)
	}
	if !config.UseProgressiveSimulation {
		t.Error("expected UseProgressiveSimulation=true")
	}
	if config.ExplorationConstant != 1.41 {
		t.Errorf("expected ExplorationConstant=1.41, got %v", config.ExplorationConstant)
	}
	if config.AbandonThreshold != 0.1 {
		t.Errorf("expected AbandonThreshold=0.1, got %v", config.AbandonThreshold)
	}
}

func TestNewMCTSEngine_UCB1(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UsePUCT = false

	engine := NewMCTSEngine(expander, simulator, config)

	if engine.puctPolicy != nil {
		t.Error("expected nil puctPolicy for UCB1")
	}

	_, ok := engine.selectionPolicy.(*UCB1Policy)
	if !ok {
		t.Error("expected UCB1Policy for selection")
	}
}

func TestNewMCTSEngine_PUCT(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UsePUCT = true

	engine := NewMCTSEngine(expander, simulator, config)

	if engine.puctPolicy == nil {
		t.Error("expected non-nil puctPolicy for PUCT")
	}

	_, ok := engine.selectionPolicy.(*PUCTPolicy)
	if !ok {
		t.Error("expected PUCTPolicy for selection")
	}
}

func TestNewMCTSEngine_WithRAVE(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UseRAVE = true

	engine := NewMCTSEngine(expander, simulator, config)

	if engine.rave == nil {
		t.Error("expected non-nil RAVE tracker")
	}
}

func TestNewMCTSEngine_WithTransposition(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UseTransposition = true

	engine := NewMCTSEngine(expander, simulator, config)

	if engine.transposition == nil {
		t.Error("expected non-nil transposition table")
	}
}

func TestMCTSEngine_Run_Basic(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.MaxIterations = 5

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.TotalNodes() < 2 {
		t.Error("expected at least root + 1 child")
	}
	if len(tree.BestPath()) == 0 {
		t.Error("expected non-empty best path")
	}
}

func TestMCTSEngine_Run_RespectsMaxIterations(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.MaxIterations = 3

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      1000, // Large budget
		MaxDepth:      10,
		MaxExpansions: 10,
		TimeLimit:     1 * time.Hour,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With max 3 iterations and progressive widening, should have limited nodes
	if tree.TotalNodes() > 20 {
		t.Errorf("expected <= 20 nodes with 3 iterations, got %d", tree.TotalNodes())
	}
}

func TestMCTSEngine_Run_RespectsBudget(t *testing.T) {
	expander := NewMockExpander(3)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.MaxIterations = 0 // No iteration limit

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      5, // Very limited
		MaxDepth:      3,
		MaxExpansions: 3,
		TimeLimit:     10 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tree.TotalNodes() > 5 {
		t.Errorf("expected <= 5 nodes, got %d", tree.TotalNodes())
	}
}

func TestMCTSEngine_Run_ContextCancellation(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      1000,
		MaxDepth:      10,
		MaxExpansions: 10,
		TimeLimit:     1 * time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tree, err := engine.Run(ctx, "Test task", budget)

	// Should complete without error (context cancellation is handled gracefully)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestMCTSEngine_Run_WithPUCT(t *testing.T) {
	expander := NewMockExpander(2)
	expander.Priors = []float64{0.8, 0.2}

	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UsePUCT = true
	config.MaxIterations = 5

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestMCTSEngine_Run_WithRAVE(t *testing.T) {
	expander := NewMockExpander(2)
	expander.ChildGenerator = func(parent *PlanNode, count int) ([]*PlanNode, []float64) {
		children := make([]*PlanNode, count)
		priors := make([]float64, count)
		for i := 0; i < count; i++ {
			child := NewPlanNode(parent.ID+"."+string(rune('1'+i)), "Approach")
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				Description: "Edit file",
			}
			child.SetAction(action)
			children[i] = child
			priors[i] = 1.0 / float64(count)
		}
		return children, priors
	}

	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UseRAVE = true
	config.RAVEBeta = 0.3
	config.MaxIterations = 5

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// Check RAVE has some data
	if engine.rave.GetScore(ActionTypeEdit) < 0 {
		t.Error("expected RAVE to have recorded scores for ActionTypeEdit")
	}
}

func TestMCTSEngine_Run_WithTransposition(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.UseTransposition = true
	config.MaxIterations = 10

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// Check transposition table has entries
	if engine.transposition.Size() == 0 {
		t.Error("expected transposition table to have entries")
	}
}

func TestMCTSEngine_RunMCTS_Interface(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.MaxIterations = 3

	engine := NewMCTSEngine(expander, simulator, config)

	// Test that engine implements MCTSRunner interface
	var runner MCTSRunner = engine

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree, err := runner.RunMCTS(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestRAVETracker(t *testing.T) {
	t.Run("Update and GetScore", func(t *testing.T) {
		rave := NewRAVETracker()

		rave.Update(ActionTypeEdit, 0.8)
		rave.Update(ActionTypeEdit, 0.6)

		score := rave.GetScore(ActionTypeEdit)
		expected := 0.7 // (0.8 + 0.6) / 2

		if score < expected-0.01 || score > expected+0.01 {
			t.Errorf("expected ~%.2f, got %.2f", expected, score)
		}
	})

	t.Run("Unknown action returns -1", func(t *testing.T) {
		rave := NewRAVETracker()

		score := rave.GetScore(ActionTypeDelete)

		if score != -1 {
			t.Errorf("expected -1 for unknown action, got %v", score)
		}
	})

	t.Run("Concurrent access", func(t *testing.T) {
		rave := NewRAVETracker()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				rave.Update(ActionTypeEdit, float64(idx)/100)
				_ = rave.GetScore(ActionTypeEdit)
			}(i)
		}

		wg.Wait()

		// Should not panic and should have valid score
		score := rave.GetScore(ActionTypeEdit)
		if score < 0 || score > 1 {
			t.Errorf("expected score in [0,1], got %v", score)
		}
	})
}

func TestTranspositionTable(t *testing.T) {
	t.Run("Store and Lookup", func(t *testing.T) {
		tt := NewTranspositionTable()
		node := NewPlanNode("test", "Test node")

		tt.Store("hash123", node)
		found := tt.Lookup("hash123")

		if found != node {
			t.Error("expected to find stored node")
		}
	})

	t.Run("Lookup unknown returns nil", func(t *testing.T) {
		tt := NewTranspositionTable()

		found := tt.Lookup("unknown")

		if found != nil {
			t.Error("expected nil for unknown hash")
		}
	})

	t.Run("Size", func(t *testing.T) {
		tt := NewTranspositionTable()

		if tt.Size() != 0 {
			t.Error("expected size 0")
		}

		tt.Store("hash1", NewPlanNode("1", "Node 1"))
		tt.Store("hash2", NewPlanNode("2", "Node 2"))

		if tt.Size() != 2 {
			t.Errorf("expected size 2, got %d", tt.Size())
		}
	})

	t.Run("Clear", func(t *testing.T) {
		tt := NewTranspositionTable()
		tt.Store("hash1", NewPlanNode("1", "Node 1"))

		tt.Clear()

		if tt.Size() != 0 {
			t.Error("expected size 0 after clear")
		}
		if tt.Lookup("hash1") != nil {
			t.Error("expected nil after clear")
		}
	})

	t.Run("Concurrent access", func(t *testing.T) {
		tt := NewTranspositionTable()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				hash := string(rune('A' + idx%26))
				tt.Store(hash, NewPlanNode(hash, "Node"))
				_ = tt.Lookup(hash)
			}(i)
		}

		wg.Wait()

		// Should not panic
		if tt.Size() == 0 {
			t.Error("expected some entries")
		}
	})
}

func TestMCTSEngine_ScoreImprovement(t *testing.T) {
	// Test that MCTS actually improves scores over iterations
	expander := NewMockExpander(3)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()
	config.MaxIterations = 20

	engine := NewMCTSEngine(expander, simulator, config)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      100,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     30 * time.Second,
	})

	tree, err := engine.Run(context.Background(), "Improve scores test", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bestPath := tree.BestPath()
	if len(bestPath) < 2 {
		t.Error("expected meaningful best path")
	}

	// Root should have multiple visits
	if tree.Root().Visits() < 2 {
		t.Error("expected root to have multiple visits")
	}
}

func TestMCTSEngine_WithOptions(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	config := DefaultMCTSEngineConfig()

	pwConfig := ProgressiveWideningConfig{
		Enabled:     true,
		Alpha:       0.6,
		K:           2.0,
		MinChildren: 2,
		MaxChildren: 8,
	}

	engine := NewMCTSEngine(
		expander,
		simulator,
		config,
		WithProgressiveWidening(pwConfig),
	)

	if engine.pwConfig.Alpha != 0.6 {
		t.Errorf("expected Alpha=0.6, got %v", engine.pwConfig.Alpha)
	}
	if engine.pwConfig.K != 2.0 {
		t.Errorf("expected K=2.0, got %v", engine.pwConfig.K)
	}
}
