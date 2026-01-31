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
	"math"
)

// NodeExpander generates child nodes for a given parent node.
//
// In MCTS for code planning, expansion typically involves:
//  1. Sending the current state to an LLM
//  2. Generating 2-3 alternative approaches
//  3. Creating PlanNode instances for each alternative
//
// Thread Safety: Implementations must be safe for concurrent use.
type NodeExpander interface {
	// Expand generates child nodes for the given parent.
	//
	// Inputs:
	//   - ctx: Context for cancellation and timeout.
	//   - parent: The node to expand.
	//   - budget: Budget to check before expansion.
	//
	// Outputs:
	//   - []*PlanNode: Generated child nodes with descriptions and optional actions.
	//   - []float64: Prior probabilities for each child (for PUCT). May be nil.
	//   - error: Non-nil on failure.
	Expand(ctx context.Context, parent *PlanNode, budget *TreeBudget) ([]*PlanNode, []float64, error)
}

// ExpansionConfig configures expansion behavior.
type ExpansionConfig struct {
	// MaxChildren is the maximum number of children to generate per expansion.
	// Default: 3
	MaxChildren int

	// MinChildren is the minimum number of children to generate.
	// If LLM returns fewer, the expansion is considered failed.
	// Default: 1
	MinChildren int

	// GeneratePriors controls whether to generate PUCT priors.
	// If true, expander should return confidence scores for each child.
	// Default: true
	GeneratePriors bool

	// IncludeActions controls whether to generate PlannedActions for children.
	// If false, only descriptions are generated (cheaper but less precise).
	// Default: true
	IncludeActions bool
}

// DefaultExpansionConfig returns sensible defaults.
func DefaultExpansionConfig() ExpansionConfig {
	return ExpansionConfig{
		MaxChildren:    3,
		MinChildren:    1,
		GeneratePriors: true,
		IncludeActions: true,
	}
}

// ProgressiveWideningConfig configures progressive widening behavior.
//
// Progressive widening limits the branching factor but allows it to grow
// with the number of visits to a node:
//
//	maxChildren = k * visits^alpha
//
// This prevents explosive branching early while allowing more exploration
// of promising paths.
type ProgressiveWideningConfig struct {
	// Enabled controls whether progressive widening is active.
	Enabled bool

	// Alpha is the exponent for the widening formula (typically 0.5).
	// Higher values allow faster branching growth.
	Alpha float64

	// K is the scaling constant (typically 1.0).
	// Higher values allow more initial children.
	K float64

	// MinChildren is the minimum children allowed regardless of formula.
	// Default: 1
	MinChildren int

	// MaxChildren is the hard cap on children regardless of formula.
	// Default: 10
	MaxChildren int
}

// DefaultProgressiveWideningConfig returns sensible defaults.
func DefaultProgressiveWideningConfig() ProgressiveWideningConfig {
	return ProgressiveWideningConfig{
		Enabled:     true,
		Alpha:       0.5,
		K:           1.0,
		MinChildren: 1,
		MaxChildren: 10,
	}
}

// ShouldExpand determines whether a node should be expanded.
//
// Inputs:
//   - node: The node to check.
//   - config: Progressive widening configuration.
//
// Outputs:
//   - bool: True if expansion is allowed.
//   - int: Maximum number of new children allowed.
func ShouldExpand(node *PlanNode, config ProgressiveWideningConfig) (bool, int) {
	currentChildren := node.ChildCount()

	if !config.Enabled {
		// Without progressive widening, always allow expansion up to hard cap
		if currentChildren >= config.MaxChildren {
			return false, 0
		}
		return true, config.MaxChildren - currentChildren
	}

	visits := float64(node.Visits())
	if visits < 1 {
		visits = 1
	}

	// Progressive widening formula: maxChildren = k * visits^alpha
	maxAllowed := int(config.K * math.Pow(visits, config.Alpha))

	// Apply bounds
	if maxAllowed < config.MinChildren {
		maxAllowed = config.MinChildren
	}
	if maxAllowed > config.MaxChildren {
		maxAllowed = config.MaxChildren
	}

	if currentChildren >= maxAllowed {
		return false, 0
	}

	return true, maxAllowed - currentChildren
}

// ExpansionResult contains the output of an expansion operation.
type ExpansionResult struct {
	// Children are the generated child nodes.
	Children []*PlanNode

	// Priors are the PUCT prior probabilities for each child.
	// Same length as Children, or nil if not generated.
	Priors []float64

	// TokensUsed is the number of LLM tokens consumed.
	TokensUsed int

	// CostUSD is the cost of the expansion operation.
	CostUSD float64
}

// MockExpander is a test implementation of NodeExpander.
//
// Thread Safety: Safe for concurrent use.
type MockExpander struct {
	// ChildrenPerExpansion is how many children to generate.
	ChildrenPerExpansion int

	// Priors to return (if nil, uniform priors are generated).
	Priors []float64

	// Error to return (if set).
	Err error

	// ChildGenerator allows custom child generation.
	// If nil, default children are generated.
	ChildGenerator func(parent *PlanNode, count int) ([]*PlanNode, []float64)
}

// NewMockExpander creates a mock expander for testing.
func NewMockExpander(childrenPerExpansion int) *MockExpander {
	return &MockExpander{
		ChildrenPerExpansion: childrenPerExpansion,
	}
}

// Expand implements NodeExpander for testing.
func (m *MockExpander) Expand(ctx context.Context, parent *PlanNode, budget *TreeBudget) ([]*PlanNode, []float64, error) {
	if m.Err != nil {
		return nil, nil, m.Err
	}

	if m.ChildGenerator != nil {
		children, priors := m.ChildGenerator(parent, m.ChildrenPerExpansion)
		return children, priors, nil
	}

	// Generate default children
	children := make([]*PlanNode, m.ChildrenPerExpansion)
	priors := make([]float64, m.ChildrenPerExpansion)
	uniformPrior := 1.0 / float64(m.ChildrenPerExpansion)

	for i := 0; i < m.ChildrenPerExpansion; i++ {
		childID := fmt.Sprintf("%s.%d", parent.ID, i+1)
		children[i] = NewPlanNode(childID, fmt.Sprintf("Approach %d for %s", i+1, parent.Description))

		if m.Priors != nil && i < len(m.Priors) {
			priors[i] = m.Priors[i]
		} else {
			priors[i] = uniformPrior
		}
	}

	return children, priors, nil
}

// ContextExpander extracts expansion context from the tree.
//
// This provides information needed by an LLM to generate meaningful expansions.
type ExpansionContext struct {
	// Task is the original task description.
	Task string

	// PathFromRoot describes the decisions made to reach this node.
	PathFromRoot []string

	// CurrentDepth is how deep in the tree we are.
	CurrentDepth int

	// SiblingDescriptions are the descriptions of sibling nodes.
	// Helps the LLM generate diverse alternatives.
	SiblingDescriptions []string

	// ParentAction is the action of the parent node (if any).
	ParentAction *PlannedAction

	// BudgetRemaining shows how much budget is left.
	BudgetRemaining BudgetRemaining
}

// BuildExpansionContext creates context for expansion.
//
// Inputs:
//   - tree: The plan tree.
//   - node: The node being expanded.
//   - budget: The current budget.
//
// Outputs:
//   - *ExpansionContext: Context for the expansion.
func BuildExpansionContext(tree *PlanTree, node *PlanNode, budget *TreeBudget) *ExpansionContext {
	// Build path descriptions
	path := node.PathFromRoot()
	pathDescriptions := make([]string, len(path))
	for i, p := range path {
		pathDescriptions[i] = p.Description
	}

	// Get sibling descriptions
	var siblingDescs []string
	parent := node.Parent()
	if parent != nil {
		for _, sibling := range parent.Children() {
			if sibling.ID != node.ID {
				siblingDescs = append(siblingDescs, sibling.Description)
			}
		}
	}

	return &ExpansionContext{
		Task:                tree.Task,
		PathFromRoot:        pathDescriptions,
		CurrentDepth:        node.Depth,
		SiblingDescriptions: siblingDescs,
		ParentAction:        node.Action(),
		BudgetRemaining:     budget.Remaining(),
	}
}

// ExpandAndIntegrate expands a node and integrates children into the tree.
//
// This is a convenience function that:
//  1. Checks if expansion is allowed (progressive widening, budget)
//  2. Calls the expander
//  3. Adds children to the parent
//  4. Updates tree statistics
//  5. Records budget usage
//
// Inputs:
//   - ctx: Context for cancellation.
//   - tree: The plan tree.
//   - node: The node to expand.
//   - expander: The node expander to use.
//   - budget: The budget to check and update.
//   - pwConfig: Progressive widening configuration.
//   - puctPolicy: Optional PUCT policy to set priors (can be nil).
//
// Outputs:
//   - []*PlanNode: The generated children (already added to tree).
//   - error: Non-nil on failure.
func ExpandAndIntegrate(
	ctx context.Context,
	tree *PlanTree,
	node *PlanNode,
	expander NodeExpander,
	budget *TreeBudget,
	pwConfig ProgressiveWideningConfig,
	puctPolicy *PUCTPolicy,
) ([]*PlanNode, error) {
	// Check budget
	if budget.Exhausted() {
		return nil, ErrBudgetExhausted
	}

	// Check progressive widening
	shouldExpand, maxNew := ShouldExpand(node, pwConfig)
	if !shouldExpand {
		return nil, nil // Not an error, just can't expand more
	}

	// Check depth limit
	if err := budget.CheckDepth(node.Depth + 1); err != nil {
		return nil, err
	}

	// Call expander
	children, priors, err := expander.Expand(ctx, node, budget)
	if err != nil {
		return nil, fmt.Errorf("expand: %w", err)
	}

	// Limit to maxNew children
	if len(children) > maxNew {
		children = children[:maxNew]
		if priors != nil {
			priors = priors[:maxNew]
		}
	}

	// Integrate children
	for i, child := range children {
		node.AddChild(child)
		tree.IncrementNodeCount()
		budget.RecordNodeExplored()

		// Set PUCT priors if available
		if puctPolicy != nil && priors != nil && i < len(priors) {
			puctPolicy.SetPrior(child.ID, priors[i])
		}
	}

	return children, nil
}
