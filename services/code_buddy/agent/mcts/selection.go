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
)

// SelectionPolicy determines how nodes are selected during MCTS traversal.
//
// Thread Safety: Implementations must be safe for concurrent use.
type SelectionPolicy interface {
	// Select returns the best child to explore based on the policy.
	// Returns nil if node has no children or all children are abandoned.
	//
	// Inputs:
	//   - parent: The parent node whose children to select from.
	//
	// Outputs:
	//   - *PlanNode: The selected child, or nil if none available.
	Select(parent *PlanNode) *PlanNode
}

// UCB1Policy implements the Upper Confidence Bound 1 selection policy.
//
// UCB1 balances exploitation (high average score) with exploration
// (low visit count) using the formula:
//
//	UCB1(node) = avgScore + C * sqrt(ln(parentVisits) / nodeVisits)
//
// Where C is the exploration constant (typically sqrt(2) ≈ 1.41).
//
// Thread Safety: Safe for concurrent use.
type UCB1Policy struct {
	// ExplorationConstant (C) controls exploration vs exploitation.
	// Higher values favor exploration of less-visited nodes.
	// Typical values: 1.41 (sqrt(2)), 2.0 (more exploration)
	ExplorationConstant float64
}

// NewUCB1Policy creates a UCB1 selection policy.
//
// Inputs:
//   - explorationConstant: The C parameter (use 1.41 for standard UCB1).
//
// Outputs:
//   - *UCB1Policy: Ready to use selection policy.
func NewUCB1Policy(explorationConstant float64) *UCB1Policy {
	if explorationConstant <= 0 {
		explorationConstant = math.Sqrt(2) // Default to sqrt(2)
	}
	return &UCB1Policy{
		ExplorationConstant: explorationConstant,
	}
}

// Select implements SelectionPolicy using UCB1.
func (p *UCB1Policy) Select(parent *PlanNode) *PlanNode {
	children := parent.Children()
	if len(children) == 0 {
		return nil
	}

	parentVisits := float64(parent.Visits())
	if parentVisits < 1 {
		parentVisits = 1 // Avoid log(0)
	}

	var best *PlanNode
	bestScore := math.Inf(-1)

	for _, child := range children {
		// Skip abandoned nodes
		if child.State() == NodeAbandoned {
			continue
		}

		childVisits := float64(child.Visits())

		// Prioritize unexplored nodes (infinite UCB1 score)
		if childVisits == 0 {
			return child
		}

		// UCB1 formula
		ucb1 := child.AvgScore() + p.ExplorationConstant*math.Sqrt(math.Log(parentVisits)/childVisits)

		if ucb1 > bestScore {
			bestScore = ucb1
			best = child
		}
	}

	return best
}

// UCB1Score calculates the UCB1 score for a node.
//
// Inputs:
//   - node: The node to score.
//   - parentVisits: Visit count of the parent.
//   - C: Exploration constant.
//
// Outputs:
//   - float64: The UCB1 score (infinity for unvisited nodes).
func UCB1Score(node *PlanNode, parentVisits float64, C float64) float64 {
	nodeVisits := float64(node.Visits())
	if nodeVisits == 0 {
		return math.Inf(1)
	}
	if parentVisits < 1 {
		parentVisits = 1
	}
	return node.AvgScore() + C*math.Sqrt(math.Log(parentVisits)/nodeVisits)
}

// PUCTPolicy implements the Predictor + Upper Confidence Tree policy.
//
// PUCT is the selection policy used by AlphaGo/AlphaZero. It incorporates
// a prior probability for each action (from a policy network or LLM):
//
//	PUCT(node) = avgScore + C * prior * sqrt(parentVisits) / (1 + nodeVisits)
//
// The prior allows the policy to favor actions that are estimated to be
// good even before they've been explored.
//
// Thread Safety: Safe for concurrent use.
type PUCTPolicy struct {
	// ExplorationConstant (C_puct) controls exploration.
	// AlphaGo uses values around 1.0-2.0.
	ExplorationConstant float64

	// priors maps node IDs to their prior probabilities [0, 1].
	// Nodes without priors default to 1/numSiblings.
	priors   map[string]float64
	priorsMu sync.RWMutex
}

// NewPUCTPolicy creates a PUCT selection policy.
//
// Inputs:
//   - explorationConstant: The C_puct parameter (typically 1.0-2.0).
//
// Outputs:
//   - *PUCTPolicy: Ready to use selection policy.
func NewPUCTPolicy(explorationConstant float64) *PUCTPolicy {
	if explorationConstant <= 0 {
		explorationConstant = 1.5 // Default
	}
	return &PUCTPolicy{
		ExplorationConstant: explorationConstant,
		priors:              make(map[string]float64),
	}
}

// SetPrior sets the prior probability for a node.
//
// Inputs:
//   - nodeID: The node's ID.
//   - prior: Prior probability [0, 1]. Higher means more likely to be selected.
func (p *PUCTPolicy) SetPrior(nodeID string, prior float64) {
	// Clamp to [0, 1]
	if prior < 0 {
		prior = 0
	}
	if prior > 1 {
		prior = 1
	}

	p.priorsMu.Lock()
	defer p.priorsMu.Unlock()
	p.priors[nodeID] = prior
}

// SetPriors sets priors for multiple nodes at once.
//
// Inputs:
//   - priors: Map of nodeID → prior probability.
func (p *PUCTPolicy) SetPriors(priors map[string]float64) {
	p.priorsMu.Lock()
	defer p.priorsMu.Unlock()
	for id, prior := range priors {
		if prior < 0 {
			prior = 0
		}
		if prior > 1 {
			prior = 1
		}
		p.priors[id] = prior
	}
}

// GetPrior returns the prior for a node (default: uniform).
func (p *PUCTPolicy) GetPrior(nodeID string, numSiblings int) float64 {
	p.priorsMu.RLock()
	defer p.priorsMu.RUnlock()

	if prior, ok := p.priors[nodeID]; ok {
		return prior
	}

	// Default to uniform prior
	if numSiblings <= 0 {
		return 1.0
	}
	return 1.0 / float64(numSiblings)
}

// Select implements SelectionPolicy using PUCT.
func (p *PUCTPolicy) Select(parent *PlanNode) *PlanNode {
	children := parent.Children()
	if len(children) == 0 {
		return nil
	}

	parentVisits := float64(parent.Visits())
	if parentVisits < 1 {
		parentVisits = 1
	}
	sqrtParent := math.Sqrt(parentVisits)

	// Filter out abandoned children and count active
	activeChildren := make([]*PlanNode, 0, len(children))
	for _, child := range children {
		if child.State() != NodeAbandoned {
			activeChildren = append(activeChildren, child)
		}
	}

	if len(activeChildren) == 0 {
		return nil
	}

	var best *PlanNode
	bestScore := math.Inf(-1)

	for _, child := range activeChildren {
		childVisits := float64(child.Visits())
		prior := p.GetPrior(child.ID, len(activeChildren))

		// PUCT formula
		var puct float64
		if childVisits == 0 {
			// Unvisited nodes: use only exploration term
			puct = p.ExplorationConstant * prior * sqrtParent
		} else {
			puct = child.AvgScore() + p.ExplorationConstant*prior*sqrtParent/(1+childVisits)
		}

		if puct > bestScore {
			bestScore = puct
			best = child
		}
	}

	return best
}

// PUCTScore calculates the PUCT score for a node.
//
// Inputs:
//   - node: The node to score.
//   - parentVisits: Visit count of the parent.
//   - prior: Prior probability for this node.
//   - C: Exploration constant.
//
// Outputs:
//   - float64: The PUCT score.
func PUCTScore(node *PlanNode, parentVisits, prior, C float64) float64 {
	nodeVisits := float64(node.Visits())
	if parentVisits < 1 {
		parentVisits = 1
	}

	sqrtParent := math.Sqrt(parentVisits)

	if nodeVisits == 0 {
		return C * prior * sqrtParent
	}

	return node.AvgScore() + C*prior*sqrtParent/(1+nodeVisits)
}

// TreeTraversal traverses from root to leaf using the given selection policy.
//
// Inputs:
//   - tree: The plan tree to traverse.
//   - policy: The selection policy to use.
//
// Outputs:
//   - *PlanNode: The selected leaf node.
//   - []*PlanNode: The path from root to leaf.
//
// Thread Safety: Safe for concurrent use if policy is thread-safe.
func TreeTraversal(tree *PlanTree, policy SelectionPolicy) (*PlanNode, []*PlanNode) {
	if tree == nil || tree.Root() == nil {
		return nil, nil
	}

	path := []*PlanNode{tree.Root()}
	node := tree.Root()

	for !node.IsLeaf() {
		selected := policy.Select(node)
		if selected == nil {
			break // No selectable children
		}
		path = append(path, selected)
		node = selected
	}

	return node, path
}

// SelectWithVirtualLoss performs selection while applying virtual loss.
// This is used for parallel MCTS to discourage multiple workers from
// selecting the same path.
//
// Inputs:
//   - tree: The plan tree to traverse.
//   - policy: The selection policy to use.
//   - virtualLoss: The virtual loss to apply during traversal.
//
// Outputs:
//   - *PlanNode: The selected leaf node.
//   - []*PlanNode: The path from root to leaf.
//   - func(): Release function to call after backpropagation.
//
// Thread Safety: Safe for concurrent use.
func SelectWithVirtualLoss(tree *PlanTree, policy SelectionPolicy, virtualLoss float64) (*PlanNode, []*PlanNode, func()) {
	if tree == nil || tree.Root() == nil {
		return nil, nil, func() {}
	}

	path := []*PlanNode{tree.Root()}
	node := tree.Root()

	// Apply virtual loss as we traverse
	node.IncrementVisits()
	node.AddScore(-virtualLoss)

	for !node.IsLeaf() {
		selected := policy.Select(node)
		if selected == nil {
			break
		}

		// Apply virtual loss to selected node
		selected.IncrementVisits()
		selected.AddScore(-virtualLoss)

		path = append(path, selected)
		node = selected
	}

	// Release function removes virtual loss
	release := func() {
		for _, n := range path {
			// We added 1 visit and -virtualLoss score
			// To undo: we don't decrement visits (they're real now)
			// but we add back the virtual loss
			n.AddScore(virtualLoss)
		}
	}

	return node, path, release
}

// DefaultSelectionPolicy returns a standard UCB1 policy with sqrt(2) constant.
func DefaultSelectionPolicy() SelectionPolicy {
	return NewUCB1Policy(math.Sqrt(2))
}
