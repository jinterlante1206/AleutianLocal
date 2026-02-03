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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// NodeState represents the lifecycle state of a plan node.
type NodeState string

const (
	NodeUnexplored NodeState = "unexplored"
	NodeExploring  NodeState = "exploring"
	NodeCompleted  NodeState = "completed"
	NodeAbandoned  NodeState = "abandoned"
)

// String returns the string representation of the node state.
func (s NodeState) String() string {
	return string(s)
}

// IsTerminal returns true if this state is terminal (completed or abandoned).
func (s NodeState) IsTerminal() bool {
	return s == NodeCompleted || s == NodeAbandoned
}

// PlanNode represents a node in the MCTS plan tree.
//
// Thread Safety: Safe for concurrent use. Uses atomic operations for visit/score
// updates and mutex for structural modifications (children).
type PlanNode struct {
	// Immutable after creation
	ID          string    `json:"id"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Depth       int       `json:"depth"`
	ContentHash string    `json:"content_hash"` // SHA256 of description+action

	// Parent pointer (not serialized to avoid cycles)
	parent *PlanNode

	// Action associated with this node (may be nil for root)
	action *PlannedAction

	// MCTS statistics (atomic operations for thread safety)
	visits     int64   // Atomic
	totalScore float64 // Protected by mu

	// Children (protected by mu)
	mu       sync.RWMutex
	children []*PlanNode

	// State (protected by mu)
	state NodeState

	// Simulation results (protected by mu)
	simulated bool
	simResult *SimulationResult
}

// SimulationResult holds the outcome of simulating a node.
type SimulationResult struct {
	Score         float64            `json:"score"`
	Signals       map[string]float64 `json:"signals"`
	Errors        []string           `json:"errors,omitempty"`
	Warnings      []string           `json:"warnings,omitempty"`
	Duration      time.Duration      `json:"duration"`
	Tier          string             `json:"tier"`            // quick, standard, full
	PromoteToNext bool               `json:"promote_to_next"` // Should run next tier?
}

// PlanNodeOption configures a PlanNode during creation.
type PlanNodeOption func(*PlanNode)

// WithAction sets the planned action for a node.
func WithAction(action *PlannedAction) PlanNodeOption {
	return func(n *PlanNode) {
		n.action = action
	}
}

// WithParent sets the parent node.
func WithParent(parent *PlanNode) PlanNodeOption {
	return func(n *PlanNode) {
		n.parent = parent
		if parent != nil {
			n.Depth = parent.Depth + 1
		}
	}
}

// NewPlanNode creates a new plan node with the given ID and description.
//
// Inputs:
//   - id: Unique identifier for this node (e.g., "1", "1.1", "1.1.2")
//   - description: Human-readable description of this plan step
//   - opts: Optional configuration functions
//
// Outputs:
//   - *PlanNode: The created node, never nil
//
// Thread Safety: The returned node is safe for concurrent use.
func NewPlanNode(id, description string, opts ...PlanNodeOption) *PlanNode {
	n := &PlanNode{
		ID:          id,
		Description: description,
		CreatedAt:   time.Now(),
		state:       NodeUnexplored,
		children:    make([]*PlanNode, 0),
	}

	for _, opt := range opts {
		opt(n)
	}

	// Compute content hash
	n.ContentHash = n.computeContentHash()

	return n
}

// computeContentHash generates a SHA256 hash of the node's content.
func (n *PlanNode) computeContentHash() string {
	h := sha256.New()
	h.Write([]byte(n.ID))
	h.Write([]byte(n.Description))
	if n.action != nil {
		h.Write([]byte(n.action.Type))
		h.Write([]byte(n.action.FilePath))
		h.Write([]byte(n.action.CodeDiff))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Visits returns the visit count atomically.
func (n *PlanNode) Visits() int64 {
	return atomic.LoadInt64(&n.visits)
}

// IncrementVisits atomically increments the visit count and returns the new value.
func (n *PlanNode) IncrementVisits() int64 {
	return atomic.AddInt64(&n.visits, 1)
}

// TotalScore returns the total score (requires lock).
func (n *PlanNode) TotalScore() float64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.totalScore
}

// AddScore adds to the total score atomically.
func (n *PlanNode) AddScore(score float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.totalScore += score
}

// AvgScore returns the average score (total/visits).
// Returns 0 if no visits.
func (n *PlanNode) AvgScore() float64 {
	visits := n.Visits()
	if visits == 0 {
		return 0
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.totalScore / float64(visits)
}

// State returns the current node state.
func (n *PlanNode) State() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// SetState updates the node state.
func (n *PlanNode) SetState(state NodeState) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.state = state
}

// Parent returns the parent node (may be nil for root).
func (n *PlanNode) Parent() *PlanNode {
	return n.parent
}

// Action returns the planned action (may be nil).
func (n *PlanNode) Action() *PlannedAction {
	return n.action
}

// SetAction sets the planned action.
func (n *PlanNode) SetAction(action *PlannedAction) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.action = action
	n.ContentHash = n.computeContentHash()
}

// Children returns a copy of the children slice.
func (n *PlanNode) Children() []*PlanNode {
	n.mu.RLock()
	defer n.mu.RUnlock()
	children := make([]*PlanNode, len(n.children))
	copy(children, n.children)
	return children
}

// ChildCount returns the number of children.
func (n *PlanNode) ChildCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.children)
}

// AddChild adds a child node and sets its parent pointer.
func (n *PlanNode) AddChild(child *PlanNode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	child.parent = n
	child.Depth = n.Depth + 1
	n.children = append(n.children, child)
}

// RemoveChild removes a child node by ID.
func (n *PlanNode) RemoveChild(childID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, child := range n.children {
		if child.ID == childID {
			n.children = append(n.children[:i], n.children[i+1:]...)
			child.parent = nil
			return true
		}
	}
	return false
}

// IsRoot returns true if this node has no parent.
func (n *PlanNode) IsRoot() bool {
	return n.parent == nil
}

// IsLeaf returns true if this node has no children.
func (n *PlanNode) IsLeaf() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.children) == 0
}

// NeedsExpansion returns true if this node should be expanded.
// A node needs expansion if it's unexplored and has no children.
func (n *PlanNode) NeedsExpansion() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state == NodeUnexplored && len(n.children) == 0
}

// IsSimulated returns whether this node has been simulated.
func (n *PlanNode) IsSimulated() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.simulated
}

// SimulationResult returns the simulation result (may be nil).
func (n *PlanNode) SimulationResult() *SimulationResult {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.simResult
}

// SetSimulationResult sets the simulation result.
func (n *PlanNode) SetSimulationResult(result *SimulationResult) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.simulated = true
	n.simResult = result
}

// PathFromRoot returns the path from root to this node.
func (n *PlanNode) PathFromRoot() []*PlanNode {
	// Build path in reverse (leaf to root), then reverse
	var path []*PlanNode
	current := n
	for current != nil {
		path = append(path, current)
		current = current.parent
	}
	// Reverse to get root-to-leaf order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// PathIDs returns the IDs of nodes from root to this node.
func (n *PlanNode) PathIDs() []string {
	path := n.PathFromRoot()
	ids := make([]string, len(path))
	for i, node := range path {
		ids[i] = node.ID
	}
	return ids
}

// String returns a human-readable representation of the node.
func (n *PlanNode) String() string {
	return fmt.Sprintf("PlanNode{id=%s, state=%s, visits=%d, avg_score=%.2f, children=%d}",
		n.ID, n.State(), n.Visits(), n.AvgScore(), n.ChildCount())
}

// MarshalJSON implements json.Marshaler with custom handling.
func (n *PlanNode) MarshalJSON() ([]byte, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	type nodeJSON struct {
		ID          string            `json:"id"`
		Description string            `json:"description"`
		Depth       int               `json:"depth"`
		ContentHash string            `json:"content_hash"`
		State       NodeState         `json:"state"`
		Visits      int64             `json:"visits"`
		TotalScore  float64           `json:"total_score"`
		AvgScore    float64           `json:"avg_score"`
		Action      *PlannedAction    `json:"action,omitempty"`
		Children    []*PlanNode       `json:"children,omitempty"`
		Simulated   bool              `json:"simulated"`
		SimResult   *SimulationResult `json:"sim_result,omitempty"`
		CreatedAt   time.Time         `json:"created_at"`
	}

	return json.Marshal(&nodeJSON{
		ID:          n.ID,
		Description: n.Description,
		Depth:       n.Depth,
		ContentHash: n.ContentHash,
		State:       n.state,
		Visits:      atomic.LoadInt64(&n.visits),
		TotalScore:  n.totalScore,
		AvgScore:    n.totalScore / float64(max(1, atomic.LoadInt64(&n.visits))),
		Action:      n.action,
		Children:    n.children,
		Simulated:   n.simulated,
		SimResult:   n.simResult,
		CreatedAt:   n.CreatedAt,
	})
}
