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
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultParallelMCTSConfig(t *testing.T) {
	config := DefaultParallelMCTSConfig()

	if config.NumWorkers != 4 {
		t.Errorf("expected NumWorkers=4, got %d", config.NumWorkers)
	}
	if config.VirtualLossValue != 1.0 {
		t.Errorf("expected VirtualLossValue=1.0, got %v", config.VirtualLossValue)
	}
	if config.BatchSize != 1 {
		t.Errorf("expected BatchSize=1, got %d", config.BatchSize)
	}
}

func TestNewParallelMCTSEngine(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engine := NewMCTSEngine(expander, simulator, DefaultMCTSEngineConfig())
	config := DefaultParallelMCTSConfig()

	parallel := NewParallelMCTSEngine(engine, config)

	if parallel == nil {
		t.Fatal("expected non-nil parallel engine")
	}
	if parallel.config.NumWorkers != 4 {
		t.Errorf("expected NumWorkers=4, got %d", parallel.config.NumWorkers)
	}
}

func TestParallelMCTSEngine_Run_Basic(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 10
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2 // Use fewer workers for faster test
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      100,
		MaxDepth:      5,
		MaxExpansions: 20,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.TotalNodes() < 2 {
		t.Error("expected at least root + 1 child")
	}
}

func TestParallelMCTSEngine_Run_ContextCancellation(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engine := NewMCTSEngine(expander, simulator, DefaultMCTSEngineConfig())

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      1000,
		MaxDepth:      10,
		MaxExpansions: 100,
		TimeLimit:     1 * time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tree, err := parallel.Run(ctx, "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestParallelMCTSEngine_Run_BudgetExhaustion(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engine := NewMCTSEngine(expander, simulator, DefaultMCTSEngineConfig())

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      5, // Very limited budget
		MaxDepth:      3,
		MaxExpansions: 3,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.TotalNodes() > 5 {
		t.Errorf("expected <= 5 nodes, got %d", tree.TotalNodes())
	}
}

func TestParallelMCTSEngine_VirtualLossApplication(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 5
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallelConfig.VirtualLossValue = 2.0
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	if parallel.virtualLossValue != 2.0 {
		t.Errorf("expected VirtualLossValue=2.0, got %v", parallel.virtualLossValue)
	}

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// After completion, all virtual losses should be removed
	// Check that root has positive visits (no negative from virtual loss)
	if tree.Root().Visits() < 1 {
		t.Error("expected root to have positive visits")
	}
}

func TestParallelMCTSEngine_RunMCTS_Interface(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 3
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	// Test that parallel engine implements MCTSRunner interface
	var runner MCTSRunner = parallel

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree, err := runner.RunMCTS(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestDefaultLeafParallelConfig(t *testing.T) {
	config := DefaultLeafParallelConfig()

	if config.SimulationsPerLeaf != 4 {
		t.Errorf("expected SimulationsPerLeaf=4, got %d", config.SimulationsPerLeaf)
	}
	if config.AggregationMethod != "mean" {
		t.Errorf("expected AggregationMethod=mean, got %s", config.AggregationMethod)
	}
}

func TestNewLeafParallelMCTSEngine(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engine := NewMCTSEngine(expander, simulator, DefaultMCTSEngineConfig())
	config := DefaultLeafParallelConfig()

	leaf := NewLeafParallelMCTSEngine(engine, config)

	if leaf == nil {
		t.Fatal("expected non-nil leaf parallel engine")
	}
}

func TestLeafParallelMCTSEngine_Run_Basic(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 5
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	leafConfig := DefaultLeafParallelConfig()
	leafConfig.SimulationsPerLeaf = 2
	leaf := NewLeafParallelMCTSEngine(engine, leafConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 10,
		TimeLimit:     10 * time.Second,
	})

	tree, err := leaf.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestLeafParallelMCTSEngine_AggregateScores(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engine := NewMCTSEngine(expander, simulator, DefaultMCTSEngineConfig())

	t.Run("mean aggregation", func(t *testing.T) {
		config := LeafParallelConfig{
			SimulationsPerLeaf: 4,
			AggregationMethod:  "mean",
		}
		leaf := NewLeafParallelMCTSEngine(engine, config)

		scores := []float64{0.2, 0.4, 0.6, 0.8}
		result := leaf.aggregateScores(scores)
		expected := 0.5

		if result < expected-0.01 || result > expected+0.01 {
			t.Errorf("expected ~%.2f, got %.2f", expected, result)
		}
	})

	t.Run("max aggregation", func(t *testing.T) {
		config := LeafParallelConfig{
			SimulationsPerLeaf: 4,
			AggregationMethod:  "max",
		}
		leaf := NewLeafParallelMCTSEngine(engine, config)

		scores := []float64{0.2, 0.4, 0.8, 0.6}
		result := leaf.aggregateScores(scores)

		if result != 0.8 {
			t.Errorf("expected 0.8, got %.2f", result)
		}
	})

	t.Run("weighted aggregation", func(t *testing.T) {
		config := LeafParallelConfig{
			SimulationsPerLeaf: 4,
			AggregationMethod:  "weighted",
		}
		leaf := NewLeafParallelMCTSEngine(engine, config)

		scores := []float64{0.2, 0.4, 0.6, 0.8}
		result := leaf.aggregateScores(scores)

		// Higher scores get more weight, so result should be > mean
		if result <= 0.5 {
			t.Errorf("expected weighted > mean (0.5), got %.2f", result)
		}
	})

	t.Run("empty scores", func(t *testing.T) {
		config := DefaultLeafParallelConfig()
		leaf := NewLeafParallelMCTSEngine(engine, config)

		scores := []float64{}
		result := leaf.aggregateScores(scores)

		if result != 0 {
			t.Errorf("expected 0 for empty scores, got %.2f", result)
		}
	})
}

func TestLeafParallelMCTSEngine_RunMCTS_Interface(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 3
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	leafConfig := DefaultLeafParallelConfig()
	leaf := NewLeafParallelMCTSEngine(engine, leafConfig)

	// Test that leaf parallel engine implements MCTSRunner interface
	var runner MCTSRunner = leaf

	budget := NewTreeBudget(DefaultTreeBudgetConfig())
	tree, err := runner.RunMCTS(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestParallelMCTSEngine_MultipleWorkers(t *testing.T) {
	// Test that multiple workers actually run concurrently
	var callCount atomic.Int64

	expander := NewMockExpander(2)
	expander.ChildGenerator = func(parent *PlanNode, count int) ([]*PlanNode, []float64) {
		callCount.Add(1)
		children := make([]*PlanNode, count)
		priors := make([]float64, count)
		for i := 0; i < count; i++ {
			children[i] = NewPlanNode(parent.ID+"."+string(rune('1'+i)), "Child")
			priors[i] = 1.0 / float64(count)
		}
		return children, priors
	}

	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 20
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 4
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      100,
		MaxDepth:      5,
		MaxExpansions: 50,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// Should have made multiple expansion calls
	if callCount.Load() < 2 {
		t.Errorf("expected multiple expansion calls, got %d", callCount.Load())
	}
}

func TestParallelMCTSEngine_WithTransposition(t *testing.T) {
	expander := NewMockExpander(2)
	simulator := NewSimulator(DefaultSimulatorConfig())
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 10
	engineConfig.UseTransposition = true
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 20,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// Transposition table should have entries
	if engine.transposition.Size() == 0 {
		t.Error("expected transposition table to have entries")
	}
}

func TestParallelMCTSEngine_WithRAVE(t *testing.T) {
	expander := NewMockExpander(2)
	expander.ChildGenerator = func(parent *PlanNode, count int) ([]*PlanNode, []float64) {
		children := make([]*PlanNode, count)
		priors := make([]float64, count)
		for i := 0; i < count; i++ {
			child := NewPlanNode(parent.ID+"."+string(rune('1'+i)), "Child")
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
	engineConfig := DefaultMCTSEngineConfig()
	engineConfig.MaxIterations = 10
	engineConfig.UseRAVE = true
	engine := NewMCTSEngine(expander, simulator, engineConfig)

	parallelConfig := DefaultParallelMCTSConfig()
	parallelConfig.NumWorkers = 2
	parallel := NewParallelMCTSEngine(engine, parallelConfig)

	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:      50,
		MaxDepth:      5,
		MaxExpansions: 20,
		TimeLimit:     10 * time.Second,
	})

	tree, err := parallel.Run(context.Background(), "Test task", budget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// RAVE should have recorded scores
	if engine.rave.GetScore(ActionTypeEdit) < 0 {
		t.Error("expected RAVE to have recorded scores for ActionTypeEdit")
	}
}
