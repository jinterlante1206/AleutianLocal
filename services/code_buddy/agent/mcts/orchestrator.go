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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
)

// PlanningMode defines how planning is performed.
type PlanningMode int

const (
	// PlanningModeLinear uses CB-41 linear planning.
	PlanningModeLinear PlanningMode = iota

	// PlanningModeTree uses CB-28 MCTS planning.
	PlanningModeTree

	// PlanningModeHybrid tries tree first, falls back to linear.
	PlanningModeHybrid
)

// String returns a human-readable planning mode name.
func (m PlanningMode) String() string {
	switch m {
	case PlanningModeLinear:
		return "linear"
	case PlanningModeTree:
		return "tree"
	case PlanningModeHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// LinearPlanner is the interface for CB-41 linear planning.
type LinearPlanner interface {
	Plan(ctx context.Context, task string) (*LinearPlan, error)
}

// LinearPlan represents a CB-41 linear plan.
type LinearPlan struct {
	Steps []PlanStep `json:"steps"`
}

// PlanStep represents a single step in a linear plan.
type PlanStep struct {
	Description string         `json:"description"`
	Action      *PlannedAction `json:"action,omitempty"`
}

// PlanningOrchestrator coordinates between CB-41 linear and CB-28 tree planning.
//
// Thread Safety: Safe for concurrent use.
type PlanningOrchestrator struct {
	linearPlanner LinearPlanner
	treeRunner    MCTSRunner
	dagConverter  *PlanToDAGConverter
	config        PlanPhaseConfig
	degradation   *DegradationManager
}

// NewPlanningOrchestrator creates an orchestrator.
//
// Inputs:
//   - linear: CB-41 linear planner (can be nil if only tree mode is used).
//   - tree: MCTS runner for tree planning.
//   - converter: DAG converter.
//   - config: Plan phase configuration.
//   - degradation: Degradation manager (can be nil for no degradation handling).
//
// Outputs:
//   - *PlanningOrchestrator: The orchestrator.
func NewPlanningOrchestrator(
	linear LinearPlanner,
	tree MCTSRunner,
	converter *PlanToDAGConverter,
	config PlanPhaseConfig,
	degradation *DegradationManager,
) *PlanningOrchestrator {
	return &PlanningOrchestrator{
		linearPlanner: linear,
		treeRunner:    tree,
		dagConverter:  converter,
		config:        config,
		degradation:   degradation,
	}
}

// Plan decides which planning mode to use and executes it.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - task: The task description.
//   - metrics: Optional session metrics for complexity estimation (can be nil).
//
// Outputs:
//   - *PlanResult: The planning result.
//   - error: Non-nil on failure.
func (o *PlanningOrchestrator) Plan(ctx context.Context, task string, metrics *SessionMetrics) (*PlanResult, error) {
	// Check degradation state
	if o.degradation != nil && o.degradation.ShouldUseLinearFallback() {
		return o.planLinear(ctx, task)
	}

	// Decide mode
	decision := ShouldUseTreeMode(task, metrics, o.config)

	if decision.UseTreeMode {
		result, err := o.planTree(ctx, task)
		if err != nil {
			// Fallback to linear if configured
			if o.config.FallbackOnTreeFailure && o.linearPlanner != nil {
				if o.degradation != nil {
					o.degradation.RecordFailure()
				}
				return o.planLinear(ctx, task)
			}
			return nil, err
		}
		if o.degradation != nil {
			o.degradation.RecordSuccess()
		}
		return result, nil
	}

	return o.planLinear(ctx, task)
}

// PlanWithMode executes planning using a specific mode.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - task: The task description.
//   - mode: The planning mode to use.
//
// Outputs:
//   - *PlanResult: The planning result.
//   - error: Non-nil on failure.
func (o *PlanningOrchestrator) PlanWithMode(ctx context.Context, task string, mode PlanningMode) (*PlanResult, error) {
	switch mode {
	case PlanningModeLinear:
		return o.planLinear(ctx, task)
	case PlanningModeTree:
		return o.planTree(ctx, task)
	case PlanningModeHybrid:
		result, err := o.planTree(ctx, task)
		if err != nil && o.linearPlanner != nil {
			return o.planLinear(ctx, task)
		}
		return result, err
	default:
		return nil, fmt.Errorf("unknown planning mode: %d", mode)
	}
}

func (o *PlanningOrchestrator) planTree(ctx context.Context, task string) (*PlanResult, error) {
	if o.treeRunner == nil {
		return nil, fmt.Errorf("tree runner not configured")
	}

	var budget *TreeBudget
	if o.degradation != nil {
		budgetConfig := o.degradation.GetBudgetForLevel()
		budget = NewTreeBudget(budgetConfig)
	} else {
		budget = NewTreeBudget(DefaultTreeBudgetConfig())
	}

	tree, err := o.treeRunner.RunMCTS(ctx, task, budget)
	if err != nil {
		return nil, fmt.Errorf("tree planning: %w", err)
	}

	var execDAG *dag.DAG
	if o.dagConverter != nil {
		execDAG, err = o.dagConverter.ToDAG(tree, "plan")
		if err != nil {
			return nil, fmt.Errorf("DAG conversion: %w", err)
		}
	}

	bestPath := tree.BestPath()
	bestScore := 0.0
	if len(bestPath) > 0 {
		bestScore = bestPath[len(bestPath)-1].AvgScore()
	}

	return &PlanResult{
		Mode:      PlanningModeTree,
		Tree:      tree,
		DAG:       execDAG,
		BestScore: bestScore,
		Budget:    budget,
	}, nil
}

func (o *PlanningOrchestrator) planLinear(ctx context.Context, task string) (*PlanResult, error) {
	if o.linearPlanner == nil {
		return nil, fmt.Errorf("linear planner not configured")
	}

	plan, err := o.linearPlanner.Plan(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("linear planning: %w", err)
	}

	// Convert linear plan to minimal tree for consistency
	tree := linearPlanToTree(plan, task)

	var execDAG *dag.DAG
	if o.dagConverter != nil {
		execDAG, _ = o.dagConverter.ToDAG(tree, "plan")
	}

	return &PlanResult{
		Mode:       PlanningModeLinear,
		Tree:       tree,
		DAG:        execDAG,
		LinearPlan: plan,
	}, nil
}

// linearPlanToTree converts a linear plan to a tree structure.
func linearPlanToTree(plan *LinearPlan, task string) *PlanTree {
	budget := NewTreeBudget(TreeBudgetConfig{
		MaxNodes:     len(plan.Steps) + 1,
		MaxDepth:     len(plan.Steps) + 1,
		CostLimitUSD: 0.01, // Minimal budget for converted plans
	})

	tree := NewPlanTree(task, budget)

	current := tree.Root()
	for i, step := range plan.Steps {
		node := NewPlanNode(
			fmt.Sprintf("step-%d", i),
			step.Description,
		)
		if step.Action != nil {
			node.SetAction(step.Action)
		}
		current.AddChild(node)
		tree.IncrementNodeCount()
		current = node
	}

	// Extract and set best path
	tree.SetBestPath(tree.ExtractBestPath())

	return tree
}

// PlanResult contains the output of planning.
type PlanResult struct {
	Mode       PlanningMode `json:"mode"`
	Tree       *PlanTree    `json:"tree"`
	DAG        *dag.DAG     `json:"dag,omitempty"`
	LinearPlan *LinearPlan  `json:"linear_plan,omitempty"`
	BestScore  float64      `json:"best_score,omitempty"`
	Budget     *TreeBudget  `json:"budget_usage,omitempty"`
}

// NoopLinearPlanner is a no-op linear planner for testing.
type NoopLinearPlanner struct {
	Plan_ *LinearPlan
}

// Plan returns the configured plan or a default.
func (n *NoopLinearPlanner) Plan(_ context.Context, task string) (*LinearPlan, error) {
	if n.Plan_ != nil {
		return n.Plan_, nil
	}
	return &LinearPlan{
		Steps: []PlanStep{
			{Description: "Step 1: Analyze " + task},
			{Description: "Step 2: Implement solution"},
			{Description: "Step 3: Verify result"},
		},
	}, nil
}

// NoopMCTSRunner is a no-op MCTS runner for testing.
type NoopMCTSRunner struct {
	Tree_ *PlanTree
	Err   error
}

// RunMCTS returns the configured tree or creates a default.
func (n *NoopMCTSRunner) RunMCTS(_ context.Context, task string, budget *TreeBudget) (*PlanTree, error) {
	if n.Err != nil {
		return nil, n.Err
	}
	if n.Tree_ != nil {
		return n.Tree_, nil
	}

	tree := NewPlanTree(task, budget)
	child := NewPlanNode("approach-1", "First approach")
	child.IncrementVisits()
	child.AddScore(0.8)
	tree.Root().AddChild(child)
	tree.IncrementNodeCount()
	tree.SetBestPath([]*PlanNode{tree.Root(), child})

	return tree, nil
}
