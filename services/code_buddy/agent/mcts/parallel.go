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
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ParallelMCTSEngine runs multiple MCTS iterations concurrently using virtual loss.
//
// Virtual Loss:
// When a worker selects a path, it applies a "virtual loss" to discourage other
// workers from selecting the same path. This ensures better exploration across
// workers. After simulation, the virtual loss is removed and the real score
// is backpropagated.
//
// Thread Safety: Safe for concurrent use.
type ParallelMCTSEngine struct {
	// Core MCTS engine (used for single-threaded operations)
	engine *MCTSEngine

	// Configuration
	config ParallelMCTSConfig

	// Virtual loss tracking
	virtualLossValue float64

	// Logging
	logger *slog.Logger
}

// ParallelMCTSConfig configures parallel MCTS execution.
type ParallelMCTSConfig struct {
	// NumWorkers is the number of concurrent workers.
	// Default: 4
	NumWorkers int

	// VirtualLossValue is the penalty applied during selection.
	// Higher values = more exploration across workers.
	// Default: 1.0
	VirtualLossValue float64

	// BatchSize is how many iterations each worker performs before syncing.
	// Default: 1
	BatchSize int

	// ChannelBufferSize is the size of the work channel buffer.
	// Default: NumWorkers * 2
	ChannelBufferSize int
}

// DefaultParallelMCTSConfig returns sensible defaults.
func DefaultParallelMCTSConfig() ParallelMCTSConfig {
	return ParallelMCTSConfig{
		NumWorkers:        4,
		VirtualLossValue:  1.0,
		BatchSize:         1,
		ChannelBufferSize: 8,
	}
}

// NewParallelMCTSEngine creates a parallel MCTS engine.
//
// Inputs:
//   - engine: The underlying MCTS engine.
//   - config: Parallel configuration.
//
// Outputs:
//   - *ParallelMCTSEngine: Ready to use parallel engine.
func NewParallelMCTSEngine(
	engine *MCTSEngine,
	config ParallelMCTSConfig,
) *ParallelMCTSEngine {
	if config.ChannelBufferSize == 0 {
		config.ChannelBufferSize = config.NumWorkers * 2
	}

	return &ParallelMCTSEngine{
		engine:           engine,
		config:           config,
		virtualLossValue: config.VirtualLossValue,
		logger:           slog.Default(),
	}
}

// WithParallelLogger sets the logger.
func (p *ParallelMCTSEngine) WithParallelLogger(logger *slog.Logger) *ParallelMCTSEngine {
	p.logger = logger
	return p
}

// Run executes parallel MCTS.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - task: The task description.
//   - budget: Resource budget for exploration.
//
// Outputs:
//   - *PlanTree: The explored tree with best path set.
//   - error: Non-nil on failure.
func (p *ParallelMCTSEngine) Run(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	// Create tree
	tree := NewPlanTree(task, budget)
	tree.Root().SetState(NodeExploring)
	tree.Root().IncrementVisits()

	// Initial expansion of root (single-threaded)
	if err := p.engine.expandNode(ctx, tree, tree.Root(), budget); err != nil {
		return tree, fmt.Errorf("initial expansion: %w", err)
	}

	// Calculate target iterations
	targetIterations := p.engine.config.MaxIterations
	if targetIterations == 0 {
		// Use budget-based estimation
		remaining := budget.Remaining()
		targetIterations = remaining.Nodes * 2 // Heuristic
		if targetIterations > 10000 {
			targetIterations = 10000
		}
	}

	// Run parallel workers
	var completedIterations atomic.Int64
	var wg sync.WaitGroup

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start workers
	for i := 0; i < p.config.NumWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			p.worker(workerCtx, tree, budget, workerID, &completedIterations, targetIterations)
		}(i)
	}

	// Wait for completion or cancellation
	wg.Wait()

	// Extract best path
	tree.SetBestPath(tree.ExtractBestPath())

	p.logger.Info("Parallel MCTS complete",
		slog.Int("workers", p.config.NumWorkers),
		slog.Int64("iterations", completedIterations.Load()),
		slog.Int64("nodes", tree.TotalNodes()),
		slog.Float64("best_score", tree.BestScore()))

	return tree, nil
}

// worker runs MCTS iterations for a single worker.
func (p *ParallelMCTSEngine) worker(
	ctx context.Context,
	tree *PlanTree,
	budget *TreeBudget,
	workerID int,
	completedIterations *atomic.Int64,
	targetIterations int,
) {
	for {
		// Check stopping conditions
		if ctx.Err() != nil {
			return
		}
		if budget.Exhausted() {
			return
		}
		if targetIterations > 0 && int(completedIterations.Load()) >= targetIterations {
			return
		}

		// Run one iteration with virtual loss
		err := p.runIterationWithVirtualLoss(ctx, tree, budget, workerID)
		if err != nil {
			p.logger.Debug("worker iteration failed",
				slog.Int("worker", workerID),
				slog.String("error", err.Error()))
		}

		completedIterations.Add(1)
	}
}

// runIterationWithVirtualLoss performs one MCTS iteration with virtual loss.
func (p *ParallelMCTSEngine) runIterationWithVirtualLoss(
	ctx context.Context,
	tree *PlanTree,
	budget *TreeBudget,
	workerID int,
) error {
	// 1. SELECT with virtual loss
	leaf, path, release := SelectWithVirtualLoss(tree, p.engine.selectionPolicy, p.virtualLossValue)
	if leaf == nil {
		return fmt.Errorf("selection returned nil leaf")
	}

	// Ensure we release virtual loss when done
	defer release()

	// Check transposition table
	if p.engine.transposition != nil {
		if existing := p.engine.transposition.Lookup(leaf.ContentHash); existing != nil {
			// Reuse existing node's value
			p.backpropagateParallel(path, existing.AvgScore())
			return nil
		}
	}

	// 2. EXPAND: If leaf needs expansion
	if leaf.NeedsExpansion() && leaf.Visits() >= int64(p.engine.config.MinVisitsBeforeExpand) {
		if err := p.engine.expandNode(ctx, tree, leaf, budget); err != nil {
			// Expansion failed, continue with simulation of current node
		} else if leaf.ChildCount() > 0 {
			// Select a child for simulation
			child := p.engine.selectionPolicy.Select(leaf)
			if child != nil {
				// Apply virtual loss to new child manually
				child.IncrementVisits()
				child.AddScore(-p.virtualLossValue)
				path = append(path, child)
				leaf = child
			}
		}
	}

	// 3. SIMULATE
	score, err := p.engine.simulate(ctx, leaf)
	if err != nil {
		return fmt.Errorf("simulation: %w", err)
	}

	// Check for abandonment
	if score < p.engine.config.AbandonThreshold && leaf.Visits() > 2 {
		leaf.SetState(NodeAbandoned)
	}

	// 4. BACKPROPAGATE (with virtual loss already applied, so we compensate)
	p.backpropagateParallel(path, score)

	// Update RAVE if enabled
	if p.engine.rave != nil && leaf.Action() != nil {
		p.engine.rave.Update(leaf.Action().Type, score)
	}

	// Store in transposition table
	if p.engine.transposition != nil {
		p.engine.transposition.Store(leaf.ContentHash, leaf)
	}

	return nil
}

// backpropagateParallel updates scores from leaf to root.
// This compensates for the virtual loss that was already applied.
func (p *ParallelMCTSEngine) backpropagateParallel(path []*PlanNode, score float64) {
	for _, node := range path {
		// Note: Virtual loss will be removed by the release function
		// Here we just add the real score contribution
		node.AddScore(score)
		// IncrementVisits was already done during selection with virtual loss
	}
}

// ParallelMCTSStats contains statistics about parallel execution.
type ParallelMCTSStats struct {
	TotalIterations   int64
	IterationsPerSec  float64
	AverageWorkerTime time.Duration
	WorkerStats       []WorkerStats
}

// WorkerStats contains per-worker statistics.
type WorkerStats struct {
	WorkerID   int
	Iterations int64
	TotalTime  time.Duration
}

// RunMCTS implements the MCTSRunner interface.
func (p *ParallelMCTSEngine) RunMCTS(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	return p.Run(ctx, task, budget)
}

// LeafParallelMCTSEngine uses leaf parallelization instead of root parallelization.
//
// In leaf parallelization, multiple simulations are run in parallel on the same leaf node.
// This can be more efficient when simulations are expensive but tree traversal is cheap.
//
// Thread Safety: Safe for concurrent use.
type LeafParallelMCTSEngine struct {
	engine *MCTSEngine
	config LeafParallelConfig
	logger *slog.Logger
}

// LeafParallelConfig configures leaf parallelization.
type LeafParallelConfig struct {
	// SimulationsPerLeaf is how many simulations to run per leaf in parallel.
	// Default: 4
	SimulationsPerLeaf int

	// AggregationMethod is how to combine multiple simulation results.
	// Options: "mean", "max", "weighted"
	// Default: "mean"
	AggregationMethod string
}

// DefaultLeafParallelConfig returns sensible defaults.
func DefaultLeafParallelConfig() LeafParallelConfig {
	return LeafParallelConfig{
		SimulationsPerLeaf: 4,
		AggregationMethod:  "mean",
	}
}

// NewLeafParallelMCTSEngine creates a leaf-parallel MCTS engine.
func NewLeafParallelMCTSEngine(engine *MCTSEngine, config LeafParallelConfig) *LeafParallelMCTSEngine {
	return &LeafParallelMCTSEngine{
		engine: engine,
		config: config,
		logger: slog.Default(),
	}
}

// Run executes leaf-parallel MCTS.
func (l *LeafParallelMCTSEngine) Run(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	// Create tree
	tree := NewPlanTree(task, budget)
	tree.Root().SetState(NodeExploring)
	tree.Root().IncrementVisits()

	// Initial expansion of root
	if err := l.engine.expandNode(ctx, tree, tree.Root(), budget); err != nil {
		return tree, fmt.Errorf("initial expansion: %w", err)
	}

	// Main loop
	iteration := 0
	for !budget.Exhausted() {
		if l.engine.config.MaxIterations > 0 && iteration >= l.engine.config.MaxIterations {
			break
		}
		if ctx.Err() != nil {
			break
		}

		if err := l.runIterationLeafParallel(ctx, tree, budget); err != nil {
			l.logger.Debug("iteration failed",
				slog.Int("iteration", iteration),
				slog.String("error", err.Error()))
		}

		iteration++
	}

	// Extract best path
	tree.SetBestPath(tree.ExtractBestPath())

	return tree, nil
}

// runIterationLeafParallel runs parallel simulations on a selected leaf.
func (l *LeafParallelMCTSEngine) runIterationLeafParallel(
	ctx context.Context,
	tree *PlanTree,
	budget *TreeBudget,
) error {
	// 1. SELECT
	leaf, path := TreeTraversal(tree, l.engine.selectionPolicy)
	if leaf == nil {
		return fmt.Errorf("selection returned nil leaf")
	}

	// 2. EXPAND
	if leaf.NeedsExpansion() && leaf.Visits() >= int64(l.engine.config.MinVisitsBeforeExpand) {
		if err := l.engine.expandNode(ctx, tree, leaf, budget); err != nil {
			// Continue with current leaf
		} else if leaf.ChildCount() > 0 {
			child := l.engine.selectionPolicy.Select(leaf)
			if child != nil {
				leaf = child
				path = append(path, child)
			}
		}
	}

	// 3. PARALLEL SIMULATE
	scores := make([]float64, l.config.SimulationsPerLeaf)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < l.config.SimulationsPerLeaf; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			score, err := l.engine.simulate(ctx, leaf)
			if err == nil {
				mu.Lock()
				scores[idx] = score
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	// 4. AGGREGATE scores
	score := l.aggregateScores(scores)

	// 5. BACKPROPAGATE
	for _, node := range path {
		node.IncrementVisits()
		node.AddScore(score)
	}

	return nil
}

// aggregateScores combines multiple simulation scores.
func (l *LeafParallelMCTSEngine) aggregateScores(scores []float64) float64 {
	if len(scores) == 0 {
		return 0
	}

	switch l.config.AggregationMethod {
	case "max":
		max := scores[0]
		for _, s := range scores[1:] {
			if s > max {
				max = s
			}
		}
		return max

	case "weighted":
		// Weight by score value (higher scores get more weight)
		var sum, weightSum float64
		for _, s := range scores {
			weight := s + 0.1 // Avoid zero weights
			sum += s * weight
			weightSum += weight
		}
		if weightSum == 0 {
			return 0
		}
		return sum / weightSum

	default: // "mean"
		var sum float64
		for _, s := range scores {
			sum += s
		}
		return sum / float64(len(scores))
	}
}

// RunMCTS implements the MCTSRunner interface.
func (l *LeafParallelMCTSEngine) RunMCTS(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	return l.Run(ctx, task, budget)
}
