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
	"time"

	"go.opentelemetry.io/otel/trace"
)

// MCTSEngine implements the full MCTS algorithm for plan tree exploration.
//
// The engine performs the classic MCTS loop:
//  1. SELECT: Traverse tree using UCB1/PUCT to find a leaf
//  2. EXPAND: Generate child nodes via LLM
//  3. SIMULATE: Evaluate the selected child
//  4. BACKPROPAGATE: Update scores up the tree
//
// Thread Safety: Safe for concurrent use.
type MCTSEngine struct {
	// Core components
	expander  NodeExpander
	simulator *Simulator
	tracer    *MCTSTracer

	// Selection policy
	selectionPolicy SelectionPolicy
	puctPolicy      *PUCTPolicy // For setting priors (may be nil if using UCB1)

	// Configuration
	config        MCTSEngineConfig
	pwConfig      ProgressiveWideningConfig
	rave          *RAVETracker        // Optional RAVE tracker
	transposition *TranspositionTable // Optional transposition table

	// Logging
	logger *slog.Logger
}

// MCTSEngineConfig configures the MCTS engine.
type MCTSEngineConfig struct {
	// MaxIterations is the maximum number of MCTS iterations.
	// If 0, uses budget constraints only.
	MaxIterations int

	// SimulationTier is the default simulation tier to use.
	SimulationTier SimulationTier

	// UseProgressiveSimulation enables tiered simulation promotion.
	UseProgressiveSimulation bool

	// ExplorationConstant for UCB1/PUCT (default: sqrt(2)).
	ExplorationConstant float64

	// UsePUCT enables PUCT selection instead of UCB1.
	UsePUCT bool

	// UseRAVE enables RAVE (Rapid Action Value Estimation).
	UseRAVE bool

	// RAVEBeta is the RAVE blending parameter (0-1).
	// Higher values favor RAVE estimates over MCTS.
	RAVEBeta float64

	// UseTransposition enables transposition table.
	UseTransposition bool

	// MinVisitsBeforeExpand requires this many visits before expanding.
	// Default: 1 (expand immediately)
	MinVisitsBeforeExpand int

	// AbandonThreshold is the score below which nodes are abandoned.
	// Default: 0.1
	AbandonThreshold float64
}

// DefaultMCTSEngineConfig returns sensible defaults.
func DefaultMCTSEngineConfig() MCTSEngineConfig {
	return MCTSEngineConfig{
		MaxIterations:            0, // Use budget only
		SimulationTier:           SimTierQuick,
		UseProgressiveSimulation: true,
		ExplorationConstant:      1.41,
		UsePUCT:                  false,
		UseRAVE:                  false,
		RAVEBeta:                 0.5,
		UseTransposition:         false,
		MinVisitsBeforeExpand:    1,
		AbandonThreshold:         0.1,
	}
}

// NewMCTSEngine creates a new MCTS engine.
//
// Inputs:
//   - expander: Node expander for generating children.
//   - simulator: Simulator for evaluating nodes.
//   - config: Engine configuration.
//   - opts: Optional configuration functions.
//
// Outputs:
//   - *MCTSEngine: Ready to use engine.
func NewMCTSEngine(
	expander NodeExpander,
	simulator *Simulator,
	config MCTSEngineConfig,
	opts ...MCTSEngineOption,
) *MCTSEngine {
	e := &MCTSEngine{
		expander:  expander,
		simulator: simulator,
		config:    config,
		pwConfig:  DefaultProgressiveWideningConfig(),
		logger:    slog.Default(),
	}

	// Set up selection policy
	if config.UsePUCT {
		e.puctPolicy = NewPUCTPolicy(config.ExplorationConstant)
		e.selectionPolicy = e.puctPolicy
	} else {
		e.selectionPolicy = NewUCB1Policy(config.ExplorationConstant)
	}

	// Set up optional components
	if config.UseRAVE {
		e.rave = NewRAVETracker()
	}
	if config.UseTransposition {
		e.transposition = NewTranspositionTable()
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	return e
}

// MCTSEngineOption configures the MCTS engine.
type MCTSEngineOption func(*MCTSEngine)

// WithTracer sets the tracer for observability.
func WithMCTSTracer(tracer *MCTSTracer) MCTSEngineOption {
	return func(e *MCTSEngine) {
		e.tracer = tracer
	}
}

// WithLogger sets the logger.
func WithMCTSLogger(logger *slog.Logger) MCTSEngineOption {
	return func(e *MCTSEngine) {
		e.logger = logger
	}
}

// WithProgressiveWidening sets the progressive widening config.
func WithProgressiveWidening(config ProgressiveWideningConfig) MCTSEngineOption {
	return func(e *MCTSEngine) {
		e.pwConfig = config
	}
}

// Run executes the MCTS algorithm.
//
// This is the main entry point for MCTS exploration. It:
//  1. Creates a plan tree for the task
//  2. Runs iterations until budget exhausted or max iterations reached
//  3. Extracts and returns the best path
//
// Inputs:
//   - ctx: Context for cancellation.
//   - task: The task description.
//   - budget: Resource budget for exploration.
//
// Outputs:
//   - *PlanTree: The explored tree with best path set.
//   - error: Non-nil on failure.
func (e *MCTSEngine) Run(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	// Start tracing
	var span trace.Span
	if e.tracer != nil {
		ctx, span = e.tracer.StartMCTSRun(ctx, task, budget)
		defer func() {
			e.tracer.EndMCTSRun(span, nil, budget, nil)
		}()
	}

	// Create tree
	tree := NewPlanTree(task, budget)
	tree.Root().SetState(NodeExploring)
	tree.Root().IncrementVisits()

	// Initial expansion of root
	if err := e.expandNode(ctx, tree, tree.Root(), budget); err != nil {
		return tree, fmt.Errorf("initial expansion: %w", err)
	}

	// Main MCTS loop
	iteration := 0
	for !budget.Exhausted() {
		// Check iteration limit
		if e.config.MaxIterations > 0 && iteration >= e.config.MaxIterations {
			break
		}

		// Check context
		if ctx.Err() != nil {
			break
		}

		// Run one iteration
		if err := e.runIteration(ctx, tree, budget, iteration); err != nil {
			e.logger.Warn("iteration failed",
				slog.Int("iteration", iteration),
				slog.String("error", err.Error()))
			// Continue with next iteration
		}

		iteration++
	}

	// Extract best path
	tree.SetBestPath(tree.ExtractBestPath())

	e.logger.Info("MCTS complete",
		slog.Int("iterations", iteration),
		slog.Int64("nodes", tree.TotalNodes()),
		slog.Float64("best_score", tree.BestScore()))

	return tree, nil
}

// runIteration performs one MCTS iteration: Select → Expand → Simulate → Backpropagate.
func (e *MCTSEngine) runIteration(ctx context.Context, tree *PlanTree, budget *TreeBudget, iteration int) error {
	// TRACE: Start iteration
	var iterSpan trace.Span
	if e.tracer != nil {
		ctx, iterSpan = e.tracer.TraceIteration(ctx, iteration)
		defer func() {
			if iterSpan != nil {
				iterSpan.End()
			}
		}()
	}

	// 1. SELECT: Traverse from root to leaf
	leaf, path := TreeTraversal(tree, e.selectionPolicy)
	if leaf == nil {
		return fmt.Errorf("selection returned nil leaf")
	}

	// TRACE: Selection
	if e.tracer != nil {
		_, selSpan := e.tracer.TraceSelect(ctx, leaf)
		selSpan.End()
	}

	// Check transposition table
	if e.transposition != nil {
		if existing := e.transposition.Lookup(leaf.ContentHash); existing != nil {
			// Reuse existing node's value
			e.backpropagate(path, existing.AvgScore())
			return nil
		}
	}

	// 2. EXPAND: If leaf needs expansion and budget allows
	if leaf.NeedsExpansion() && leaf.Visits() >= int64(e.config.MinVisitsBeforeExpand) {
		if err := e.expandNode(ctx, tree, leaf, budget); err != nil {
			// Expansion failed, continue with simulation of current node
			e.logger.Debug("expansion failed",
				slog.String("node", leaf.ID),
				slog.String("error", err.Error()))
		} else if leaf.ChildCount() > 0 {
			// Select a child for simulation
			child := e.selectionPolicy.Select(leaf)
			if child != nil {
				leaf = child
				path = append(path, child)
			}
		}
	}

	// 3. SIMULATE: Evaluate the node
	score, err := e.simulate(ctx, leaf)
	if err != nil {
		return fmt.Errorf("simulation: %w", err)
	}

	// Check for abandonment
	if score < e.config.AbandonThreshold && leaf.Visits() > 2 {
		leaf.SetState(NodeAbandoned)
		if e.tracer != nil {
			e.tracer.TraceNodeAbandon(ctx, leaf, "low score")
		}
	}

	// 4. BACKPROPAGATE: Update scores up the tree
	e.backpropagate(path, score)

	// Update RAVE if enabled
	if e.rave != nil && leaf.Action() != nil {
		e.rave.Update(leaf.Action().Type, score)
	}

	// Store in transposition table
	if e.transposition != nil {
		e.transposition.Store(leaf.ContentHash, leaf)
	}

	return nil
}

// expandNode expands a node by generating children.
func (e *MCTSEngine) expandNode(ctx context.Context, tree *PlanTree, node *PlanNode, budget *TreeBudget) error {
	// TRACE: Start expansion
	var expSpan trace.Span
	if e.tracer != nil {
		ctx, expSpan = e.tracer.TraceExpand(ctx, node)
	}

	start := time.Now()

	children, err := ExpandAndIntegrate(
		ctx,
		tree,
		node,
		e.expander,
		budget,
		e.pwConfig,
		e.puctPolicy,
	)

	duration := time.Since(start)

	// TRACE: End expansion
	if e.tracer != nil {
		var priors []float64
		if e.puctPolicy != nil {
			priors = make([]float64, len(children))
			for i, child := range children {
				priors[i] = e.puctPolicy.GetPrior(child.ID, len(children))
			}
		}
		_ = priors // Used for tracing
		e.tracer.EndExpand(expSpan, children, int(duration.Milliseconds()), 0, err)
	}

	if err != nil {
		return err
	}

	// Mark node as exploring if it has children
	if len(children) > 0 {
		node.SetState(NodeExploring)
	}

	return nil
}

// simulate evaluates a node and returns its score.
func (e *MCTSEngine) simulate(ctx context.Context, node *PlanNode) (float64, error) {
	// TRACE: Start simulation
	var simSpan trace.Span
	if e.tracer != nil {
		ctx, simSpan = e.tracer.TraceSimulate(ctx, node, e.config.SimulationTier)
		defer func() {
			result := node.SimulationResult()
			e.tracer.EndSimulate(simSpan, result, nil)
		}()
	}

	var result *SimulationResult
	var err error

	if e.config.UseProgressiveSimulation {
		result, err = e.simulator.SimulateProgressive(ctx, node)
	} else {
		result, err = e.simulator.Simulate(ctx, node, e.config.SimulationTier)
	}

	if err != nil {
		return 0, err
	}

	node.SetSimulationResult(result)

	// Blend with RAVE if enabled
	score := result.Score
	if e.rave != nil && node.Action() != nil {
		raveScore := e.rave.GetScore(node.Action().Type)
		if raveScore >= 0 { // -1 means no RAVE data
			score = (1-e.config.RAVEBeta)*score + e.config.RAVEBeta*raveScore
		}
	}

	return score, nil
}

// backpropagate updates scores from leaf to root.
func (e *MCTSEngine) backpropagate(path []*PlanNode, score float64) {
	// TRACE: Start backprop
	var bpSpan trace.Span
	if e.tracer != nil && len(path) > 0 {
		_, bpSpan = e.tracer.TraceBackpropagate(context.Background(), path[len(path)-1], score)
		defer func() {
			e.tracer.EndBackpropagate(bpSpan, len(path))
		}()
	}

	for _, node := range path {
		node.IncrementVisits()
		node.AddScore(score)
	}
}

// RunMCTS implements the MCTSRunner interface.
func (e *MCTSEngine) RunMCTS(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	return e.Run(ctx, task, budget)
}

// RAVETracker tracks Rapid Action Value Estimation scores.
//
// RAVE shares value estimates between all nodes using the same action type.
// This helps bootstrap value estimates for unexplored nodes.
//
// Thread Safety: Safe for concurrent use.
type RAVETracker struct {
	scores map[ActionType]raveEntry
	mu     sync.RWMutex
}

type raveEntry struct {
	total float64
	count int
}

// NewRAVETracker creates a new RAVE tracker.
func NewRAVETracker() *RAVETracker {
	return &RAVETracker{
		scores: make(map[ActionType]raveEntry),
	}
}

// Update adds a score observation for an action type.
func (r *RAVETracker) Update(action ActionType, score float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.scores[action]
	entry.total += score
	entry.count++
	r.scores[action] = entry
}

// GetScore returns the average RAVE score for an action type.
// Returns -1 if no observations exist.
func (r *RAVETracker) GetScore(action ActionType) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.scores[action]
	if !ok || entry.count == 0 {
		return -1
	}
	return entry.total / float64(entry.count)
}

// Count returns the number of observations for an action type.
func (r *RAVETracker) Count(action ActionType) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.scores[action].count
}

// TranspositionTable stores nodes by their state hash to detect transpositions.
//
// A transposition occurs when different move sequences lead to the same state.
// By detecting these, we can reuse evaluations and avoid redundant work.
//
// Thread Safety: Safe for concurrent use.
type TranspositionTable struct {
	table map[string]*PlanNode
	mu    sync.RWMutex
}

// NewTranspositionTable creates a new transposition table.
func NewTranspositionTable() *TranspositionTable {
	return &TranspositionTable{
		table: make(map[string]*PlanNode),
	}
}

// Lookup retrieves a node by its content hash.
// Returns nil if not found.
func (t *TranspositionTable) Lookup(hash string) *PlanNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.table[hash]
}

// Store adds a node to the transposition table.
func (t *TranspositionTable) Store(hash string, node *PlanNode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.table[hash] = node
}

// Size returns the number of entries in the table.
func (t *TranspositionTable) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.table)
}

// Clear removes all entries from the table.
func (t *TranspositionTable) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.table = make(map[string]*PlanNode)
}
