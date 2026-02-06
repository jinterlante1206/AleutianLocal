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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PlanTree represents the root structure of an MCTS plan tree.
//
// Thread Safety: Safe for concurrent use.
type PlanTree struct {
	// Task description (immutable after creation)
	Task      string `json:"task"`
	CreatedAt int64  `json:"created_at"` // Unix milliseconds UTC

	// Root node
	root *PlanNode

	// Statistics (atomic)
	totalNodes int64

	// Best path (protected by mu)
	mu       sync.RWMutex
	bestPath []*PlanNode

	// Budget tracking
	budget *TreeBudget
}

// NewPlanTree creates a new plan tree for the given task.
//
// Inputs:
//   - task: The task description
//   - budget: Resource budget for tree exploration
//
// Outputs:
//   - *PlanTree: The created tree with a root node
//
// Thread Safety: The returned tree is safe for concurrent use.
func NewPlanTree(task string, budget *TreeBudget) *PlanTree {
	root := NewPlanNode("root", task)
	root.SetState(NodeExploring)

	return &PlanTree{
		Task:       task,
		CreatedAt:  time.Now().UnixMilli(),
		root:       root,
		totalNodes: 1,
		budget:     budget,
	}
}

// Root returns the root node of the tree.
func (t *PlanTree) Root() *PlanNode {
	return t.root
}

// TotalNodes returns the total number of nodes in the tree.
func (t *PlanTree) TotalNodes() int64 {
	return atomic.LoadInt64(&t.totalNodes)
}

// IncrementNodeCount atomically increments the node count.
func (t *PlanTree) IncrementNodeCount() int64 {
	return atomic.AddInt64(&t.totalNodes, 1)
}

// Budget returns the tree budget.
func (t *PlanTree) Budget() *TreeBudget {
	return t.budget
}

// BestPath returns a copy of the best path.
func (t *PlanTree) BestPath() []*PlanNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	path := make([]*PlanNode, len(t.bestPath))
	copy(path, t.bestPath)
	return path
}

// SetBestPath updates the best path.
func (t *PlanTree) SetBestPath(path []*PlanNode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bestPath = make([]*PlanNode, len(path))
	copy(t.bestPath, path)
}

// BestScore returns the average score of the best leaf node.
// Returns 0 if no best path exists.
func (t *PlanTree) BestScore() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.bestPath) == 0 {
		return 0
	}
	return t.bestPath[len(t.bestPath)-1].AvgScore()
}

// FindNode finds a node by ID using BFS.
func (t *PlanTree) FindNode(id string) *PlanNode {
	if t.root == nil {
		return nil
	}
	if t.root.ID == id {
		return t.root
	}

	queue := []*PlanNode{t.root}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		for _, child := range node.Children() {
			if child.ID == id {
				return child
			}
			queue = append(queue, child)
		}
	}
	return nil
}

// MaxDepth returns the maximum depth of the tree.
func (t *PlanTree) MaxDepth() int {
	if t.root == nil {
		return 0
	}
	return t.maxDepthRecursive(t.root)
}

func (t *PlanTree) maxDepthRecursive(node *PlanNode) int {
	maxChildDepth := node.Depth
	for _, child := range node.Children() {
		childDepth := t.maxDepthRecursive(child)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}
	return maxChildDepth
}

// CountByState returns a map of node counts by state.
func (t *PlanTree) CountByState() map[NodeState]int {
	counts := make(map[NodeState]int)
	if t.root == nil {
		return counts
	}

	t.traverseAll(func(n *PlanNode) {
		counts[n.State()]++
	})
	return counts
}

// traverseAll visits all nodes in BFS order.
func (t *PlanTree) traverseAll(fn func(*PlanNode)) {
	if t.root == nil {
		return
	}

	queue := []*PlanNode{t.root}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		fn(node)
		queue = append(queue, node.Children()...)
	}
}

// Prune removes low-scoring branches to free memory.
// Keeps the top N children by average score at each level.
//
// Inputs:
//   - keepTopN: Number of top-scoring children to keep per node
//   - minVisits: Minimum visits required to be considered for pruning
//
// Outputs:
//   - int: Number of nodes pruned
//
// Thread Safety: Safe for concurrent use, but may conflict with ongoing MCTS.
func (t *PlanTree) Prune(keepTopN int, minVisits int64) int {
	if t.root == nil || keepTopN <= 0 {
		return 0
	}

	pruned := 0
	t.pruneRecursive(t.root, keepTopN, minVisits, &pruned)
	atomic.AddInt64(&t.totalNodes, int64(-pruned))
	return pruned
}

func (t *PlanTree) pruneRecursive(node *PlanNode, keepTopN int, minVisits int64, pruned *int) {
	children := node.Children()
	if len(children) <= keepTopN {
		// Recurse into children
		for _, child := range children {
			t.pruneRecursive(child, keepTopN, minVisits, pruned)
		}
		return
	}

	// Sort children by average score (descending) using O(n log n) sort
	type scoredChild struct {
		node  *PlanNode
		score float64
	}
	scored := make([]scoredChild, len(children))
	for i, child := range children {
		scored[i] = scoredChild{node: child, score: child.AvgScore()}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score // Descending order
	})

	// Remove low-scoring children that meet the minVisits threshold
	for i := keepTopN; i < len(scored); i++ {
		child := scored[i].node
		if child.Visits() >= minVisits {
			count := t.countNodes(child)
			if node.RemoveChild(child.ID) {
				*pruned += count
			}
		}
	}

	// Recurse into remaining children
	for _, child := range node.Children() {
		t.pruneRecursive(child, keepTopN, minVisits, pruned)
	}
}

func (t *PlanTree) countNodes(node *PlanNode) int {
	count := 1
	for _, child := range node.Children() {
		count += t.countNodes(child)
	}
	return count
}

// ExtractBestPath finds the best path from root to leaf.
// Uses highest average score at each level.
func (t *PlanTree) ExtractBestPath() []*PlanNode {
	if t.root == nil {
		return nil
	}

	path := []*PlanNode{t.root}
	node := t.root

	for {
		children := node.Children()
		if len(children) == 0 {
			break
		}

		// Find best child by average score
		best := children[0]
		for _, child := range children[1:] {
			// Skip abandoned nodes
			if child.State() == NodeAbandoned {
				continue
			}
			if child.AvgScore() > best.AvgScore() {
				best = child
			}
		}

		// Stop if best child is abandoned
		if best.State() == NodeAbandoned {
			break
		}

		path = append(path, best)
		node = best
	}

	return path
}

// Format returns a formatted string representation of the tree.
func (t *PlanTree) Format() string {
	if t.root == nil {
		return "Empty tree"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task: %s\n", t.Task))
	sb.WriteString(fmt.Sprintf("Nodes: %d, Max Depth: %d\n", t.TotalNodes(), t.MaxDepth()))
	sb.WriteString(fmt.Sprintf("Best Score: %.2f\n", t.BestScore()))
	sb.WriteString("\n")

	t.formatNode(&sb, t.root, "", true)
	return sb.String()
}

func (t *PlanTree) formatNode(sb *strings.Builder, node *PlanNode, prefix string, isLast bool) {
	// Determine the branch character
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	// Format state indicator
	stateIcon := " "
	switch node.State() {
	case NodeCompleted:
		stateIcon = "✓"
	case NodeAbandoned:
		stateIcon = "✗"
	case NodeExploring:
		stateIcon = "→"
	}

	// Check if this node is in best path
	bestPathIcon := ""
	bestPath := t.BestPath()
	for _, bp := range bestPath {
		if bp.ID == node.ID {
			bestPathIcon = " ★"
			break
		}
	}

	sb.WriteString(fmt.Sprintf("%s%s[%s] %s (score: %.2f, visits: %d) %s%s\n",
		prefix, branch, node.ID, truncate(node.Description, 40),
		node.AvgScore(), node.Visits(), stateIcon, bestPathIcon))

	// Update prefix for children
	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}

	// Format children
	children := node.Children()
	for i, child := range children {
		t.formatNode(sb, child, childPrefix, i == len(children)-1)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// MarshalJSON implements json.Marshaler.
func (t *PlanTree) MarshalJSON() ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	type treeJSON struct {
		Task       string      `json:"task"`
		CreatedAt  time.Time   `json:"created_at"`
		TotalNodes int64       `json:"total_nodes"`
		MaxDepth   int         `json:"max_depth"`
		BestScore  float64     `json:"best_score"`
		Root       *PlanNode   `json:"root"`
		BestPath   []*PlanNode `json:"best_path,omitempty"`
	}

	return json.Marshal(&treeJSON{
		Task:       t.Task,
		CreatedAt:  time.UnixMilli(t.CreatedAt),
		TotalNodes: t.TotalNodes(),
		MaxDepth:   t.MaxDepth(),
		BestScore:  t.BestScore(),
		Root:       t.root,
		BestPath:   t.bestPath,
	})
}
