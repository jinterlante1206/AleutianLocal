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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
)

// ActionExecutor executes planned actions.
//
// Thread Safety: Implementations must be safe for concurrent use.
type ActionExecutor interface {
	ExecuteAction(ctx context.Context, action *PlannedAction) error
}

// PlanToDAGConverter converts plan trees to executable DAGs.
//
// Thread Safety: Safe for concurrent use.
type PlanToDAGConverter struct {
	executor    ActionExecutor
	projectRoot string
}

// NewPlanToDAGConverter creates a converter.
//
// Inputs:
//   - executor: The action executor for plan step execution.
//   - projectRoot: Root directory for path validation (can be empty for no validation).
//
// Outputs:
//   - *PlanToDAGConverter: The converter instance.
func NewPlanToDAGConverter(executor ActionExecutor, projectRoot string) *PlanToDAGConverter {
	return &PlanToDAGConverter{
		executor:    executor,
		projectRoot: projectRoot,
	}
}

// ToDAG converts a plan tree's best path to an executable DAG.
//
// Inputs:
//   - tree: The plan tree with BestPath populated.
//   - taskID: Unique identifier for this execution.
//
// Outputs:
//   - *dag.DAG: Executable DAG.
//   - error: Non-nil if conversion fails.
func (c *PlanToDAGConverter) ToDAG(tree *PlanTree, taskID string) (*dag.DAG, error) {
	if tree == nil {
		return nil, fmt.Errorf("nil plan tree")
	}

	bestPath := tree.BestPath()
	if len(bestPath) == 0 {
		return nil, fmt.Errorf("empty best path")
	}

	builder := dag.NewBuilder(fmt.Sprintf("plan-%s", taskID))

	// Track previous node for dependencies
	var prevNodeName string

	for i, planNode := range bestPath {
		// Skip root node (just contains task description)
		if i == 0 {
			continue
		}

		// Create DAG node for each plan step
		dagNode := c.createDAGNode(planNode, i, prevNodeName)
		builder.AddNode(dagNode)

		prevNodeName = dagNode.Name()
	}

	return builder.Build()
}

// createDAGNode creates a DAG node from a plan node.
func (c *PlanToDAGConverter) createDAGNode(planNode *PlanNode, step int, prevNodeName string) dag.Node {
	deps := []string{}
	if prevNodeName != "" {
		deps = []string{prevNodeName}
	}

	return &PlanStepNode{
		BaseNode: dag.BaseNode{
			NodeName:         fmt.Sprintf("step-%d-%s", step, planNode.ID),
			NodeDependencies: deps,
			NodeTimeout:      30 * time.Second,
			NodeRetryable:    true,
		},
		planNode:    planNode,
		executor:    c.executor,
		projectRoot: c.projectRoot,
		retryPolicy: DefaultPlanStepRetryPolicy(),
	}
}

// RetryPolicy defines retry behavior for plan steps.
type RetryPolicy struct {
	MaxRetries int
	BackoffMs  int
}

// DefaultPlanStepRetryPolicy returns the default retry policy.
func DefaultPlanStepRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 2,
		BackoffMs:  1000,
	}
}

// PlanStepNode is a DAG node that executes a plan step.
//
// Thread Safety: Safe for concurrent use.
type PlanStepNode struct {
	dag.BaseNode
	planNode    *PlanNode
	executor    ActionExecutor
	projectRoot string
	retryPolicy RetryPolicy
}

// Execute implements dag.Node.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - inputs: Map of dependency node outputs.
//
// Outputs:
//   - any: Execution result map.
//   - error: Non-nil on failure.
func (n *PlanStepNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	action := n.planNode.Action()
	if action == nil {
		// No action to execute, just pass through
		return map[string]any{
			"node_id":     n.planNode.ID,
			"description": n.planNode.Description,
			"skipped":     true,
		}, nil
	}

	// Validate action before execution
	if err := action.Validate(n.projectRoot, DefaultActionValidationConfig()); err != nil {
		return nil, fmt.Errorf("action validation failed: %w", err)
	}

	// Execute with retry
	var lastErr error
	for attempt := 0; attempt <= n.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			// Backoff before retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(n.retryPolicy.BackoffMs*attempt) * time.Millisecond):
			}
		}

		err := n.executor.ExecuteAction(ctx, action)
		if err == nil {
			return map[string]any{
				"node_id":     n.planNode.ID,
				"description": n.planNode.Description,
				"action_type": string(action.Type),
				"file_path":   action.FilePath,
				"attempts":    attempt + 1,
			}, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("action failed after %d attempts: %w", n.retryPolicy.MaxRetries+1, lastErr)
}

// PlanNode returns the underlying plan node.
func (n *PlanStepNode) PlanNode() *PlanNode {
	return n.planNode
}

// PlanTreeDAGNode wraps MCTS exploration as a DAG node.
// This allows tree planning to be part of a larger DAG workflow.
//
// Thread Safety: Safe for concurrent use.
type PlanTreeDAGNode struct {
	dag.BaseNode
	task      string
	runMCTS   MCTSRunner
	budget    *TreeBudget
	converter *PlanToDAGConverter
}

// MCTSRunner runs MCTS exploration.
type MCTSRunner interface {
	RunMCTS(ctx context.Context, task string, budget *TreeBudget) (*PlanTree, error)
}

// NewPlanTreeDAGNode creates a DAG node that runs MCTS planning.
//
// Inputs:
//   - id: Unique node identifier.
//   - task: The task description for MCTS.
//   - runner: The MCTS runner.
//   - budget: Budget for MCTS exploration.
//   - converter: Converter to create executable DAG from result.
//
// Outputs:
//   - *PlanTreeDAGNode: The DAG node.
func NewPlanTreeDAGNode(
	id string,
	task string,
	runner MCTSRunner,
	budget *TreeBudget,
	converter *PlanToDAGConverter,
) *PlanTreeDAGNode {
	return &PlanTreeDAGNode{
		BaseNode: dag.BaseNode{
			NodeName:         id,
			NodeDependencies: []string{},
			NodeTimeout:      5 * time.Minute, // MCTS can take time
			NodeRetryable:    false,           // Don't retry MCTS
		},
		task:      task,
		runMCTS:   runner,
		budget:    budget,
		converter: converter,
	}
}

// Execute runs MCTS and returns the resulting plan.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - inputs: Map of dependency node outputs.
//
// Outputs:
//   - any: PlanTreeResult containing the tree and DAG.
//   - error: Non-nil on failure.
func (n *PlanTreeDAGNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	// Run MCTS
	tree, err := n.runMCTS.RunMCTS(ctx, n.task, n.budget)
	if err != nil {
		return nil, fmt.Errorf("MCTS planning failed: %w", err)
	}

	// Convert to executable DAG
	execDAG, err := n.converter.ToDAG(tree, n.NodeName)
	if err != nil {
		return nil, fmt.Errorf("DAG conversion failed: %w", err)
	}

	bestPath := tree.BestPath()
	bestScore := 0.0
	if len(bestPath) > 0 {
		bestScore = bestPath[len(bestPath)-1].AvgScore()
	}

	return PlanTreeResult{
		Tree:       tree,
		DAG:        execDAG,
		BestScore:  bestScore,
		StepsCount: len(bestPath) - 1, // Exclude root
		Budget:     n.budget,
	}, nil
}

// PlanTreeResult contains the output of MCTS planning.
type PlanTreeResult struct {
	Tree       *PlanTree   `json:"tree"`
	DAG        *dag.DAG    `json:"dag"`
	BestScore  float64     `json:"best_score"`
	StepsCount int         `json:"steps_count"`
	Budget     *TreeBudget `json:"budget_usage"`
}

// NoopActionExecutor is a no-op executor for testing.
type NoopActionExecutor struct{}

// ExecuteAction does nothing and returns nil.
func (n *NoopActionExecutor) ExecuteAction(_ context.Context, _ *PlannedAction) error {
	return nil
}
