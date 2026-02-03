// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package dag

import (
	"context"
	"sync"
	"time"
)

// Node represents a single step in the pipeline.
//
// Description:
//
//	Node is the fundamental unit of work in a DAG. Each node has a unique name,
//	declares its dependencies, and implements Execute to perform its work.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use. Execute may be called
//	concurrently with other nodes.
type Node interface {
	// Name returns the unique identifier for this node.
	//
	// Outputs:
	//   string - Unique node name (e.g., "PARSE_FILES", "BUILD_GRAPH").
	Name() string

	// Dependencies returns the names of nodes that must complete first.
	//
	// Outputs:
	//   []string - Names of dependency nodes. Empty if no dependencies.
	Dependencies() []string

	// Execute runs the node's logic.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   inputs - Map of dependency node outputs keyed by node name.
	//
	// Outputs:
	//   any - The node's output, passed to dependent nodes.
	//   error - Non-nil on failure.
	Execute(ctx context.Context, inputs map[string]any) (any, error)

	// Retryable returns true if this node can be retried on failure.
	//
	// Outputs:
	//   bool - True if retryable.
	Retryable() bool

	// Timeout returns the maximum execution time for this node.
	//
	// Outputs:
	//   time.Duration - Maximum execution time. Zero means no timeout.
	Timeout() time.Duration
}

// NodeStatus represents the execution status of a node.
type NodeStatus string

const (
	// NodeStatusPending indicates the node hasn't started.
	NodeStatusPending NodeStatus = "pending"

	// NodeStatusRunning indicates the node is executing.
	NodeStatusRunning NodeStatus = "running"

	// NodeStatusCompleted indicates successful completion.
	NodeStatusCompleted NodeStatus = "completed"

	// NodeStatusFailed indicates the node failed.
	NodeStatusFailed NodeStatus = "failed"

	// NodeStatusSkipped indicates the node was skipped.
	NodeStatusSkipped NodeStatus = "skipped"
)

// Edge represents a dependency relationship between nodes.
type Edge struct {
	// From is the dependency node name (must complete first).
	From string `json:"from"`

	// To is the dependent node name (waits for From).
	To string `json:"to"`
}

// DAG represents the complete pipeline graph.
//
// Description:
//
//	DAG holds the nodes and their dependency relationships. It must be built
//	using a Builder before execution.
//
// Thread Safety:
//
//	DAG is safe for concurrent read access after building. Do not modify
//	after calling Build().
type DAG struct {
	name     string
	nodes    map[string]Node
	edges    []Edge
	adjList  map[string][]string // node → dependencies
	terminal string              // final node name (auto-detected)
}

// Name returns the DAG's name.
func (d *DAG) Name() string {
	return d.name
}

// GetNode returns a node by name.
//
// Inputs:
//
//	name - The node name.
//
// Outputs:
//
//	Node - The node if found.
//	bool - True if found.
func (d *DAG) GetNode(name string) (Node, bool) {
	node, ok := d.nodes[name]
	return node, ok
}

// NodeCount returns the number of nodes.
func (d *DAG) NodeCount() int {
	return len(d.nodes)
}

// NodeNames returns all node names.
func (d *DAG) NodeNames() []string {
	names := make([]string, 0, len(d.nodes))
	for name := range d.nodes {
		names = append(names, name)
	}
	return names
}

// GetDependencies returns the dependency names for a node.
func (d *DAG) GetDependencies(nodeName string) []string {
	deps, ok := d.adjList[nodeName]
	if !ok {
		return nil
	}
	return deps
}

// Terminal returns the terminal (final) node name.
func (d *DAG) Terminal() string {
	return d.terminal
}

// State represents the current execution state.
//
// Description:
//
//	State tracks which nodes have completed, their outputs, and any failures.
//	It is updated by the Executor during pipeline execution.
//
// Thread Safety:
//
//	State uses internal locking and is safe for concurrent access.
type State struct {
	mu sync.RWMutex

	// SessionID is the unique identifier for this execution.
	SessionID string `json:"session_id"`

	// StartedAt is when execution began.
	StartedAt time.Time `json:"started_at"`

	// CompletedNodes tracks which nodes have finished.
	CompletedNodes map[string]bool `json:"completed_nodes"`

	// NodeOutputs stores outputs from completed nodes.
	NodeOutputs map[string]any `json:"node_outputs"`

	// NodeStatuses tracks status per node.
	NodeStatuses map[string]NodeStatus `json:"node_statuses"`

	// CurrentNodes is the set of currently executing nodes.
	CurrentNodes []string `json:"current_nodes"`

	// FailedNode is the name of the node that caused failure (if any).
	FailedNode string `json:"failed_node,omitempty"`

	// Error is the error message if failed.
	Error string `json:"error,omitempty"`
}

// NewState creates a new execution state.
func NewState(sessionID string) *State {
	return &State{
		SessionID:      sessionID,
		StartedAt:      time.Now(),
		CompletedNodes: make(map[string]bool),
		NodeOutputs:    make(map[string]any),
		NodeStatuses:   make(map[string]NodeStatus),
		CurrentNodes:   make([]string, 0),
	}
}

// IsCompleted checks if a node has completed successfully.
func (s *State) IsCompleted(nodeName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CompletedNodes[nodeName]
}

// SetCompleted marks a node as completed with its output.
func (s *State) SetCompleted(nodeName string, output any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CompletedNodes[nodeName] = true
	s.NodeOutputs[nodeName] = output
	s.NodeStatuses[nodeName] = NodeStatusCompleted
}

// GetOutput returns the output of a completed node.
func (s *State) GetOutput(nodeName string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	output, ok := s.NodeOutputs[nodeName]
	return output, ok
}

// SetFailed marks a node and the execution as failed.
func (s *State) SetFailed(nodeName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FailedNode = nodeName
	s.Error = err.Error()
	s.NodeStatuses[nodeName] = NodeStatusFailed
}

// IsFailed returns whether execution has failed.
func (s *State) IsFailed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FailedNode != ""
}

// SetStatus sets the status of a node.
func (s *State) SetStatus(nodeName string, status NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NodeStatuses[nodeName] = status
}

// GetStatus returns the status of a node.
func (s *State) GetStatus(nodeName string) NodeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status, ok := s.NodeStatuses[nodeName]
	if !ok {
		return NodeStatusPending
	}
	return status
}

// SetCurrentNodes sets the list of currently executing nodes.
func (s *State) SetCurrentNodes(nodes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentNodes = nodes
}

// IsDAGComplete checks if all nodes in the DAG have completed.
func (s *State) IsDAGComplete(dag *DAG) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, name := range dag.NodeNames() {
		if !s.CompletedNodes[name] {
			return false
		}
	}
	return true
}

// CompletedCount returns the number of completed nodes.
func (s *State) CompletedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.CompletedNodes)
}

// Checkpoint is a serializable snapshot for resume.
//
// Description:
//
//	Checkpoint captures the execution state at a point in time, enabling
//	resume after failure or restart.
type Checkpoint struct {
	// State is the execution state at checkpoint time.
	State *State `json:"state"`

	// Timestamp is when the checkpoint was created.
	Timestamp time.Time `json:"timestamp"`

	// Version is the checkpoint format version.
	Version string `json:"version"`

	// Checksum is the SHA256 of the state for integrity verification.
	Checksum string `json:"checksum"`

	// DAGName is the name of the DAG being executed.
	DAGName string `json:"dag_name"`
}

// Result represents the outcome of a DAG execution.
type Result struct {
	// Success indicates if the DAG completed successfully.
	Success bool `json:"success"`

	// SessionID is the execution session ID.
	SessionID string `json:"session_id"`

	// Duration is the total execution time.
	Duration time.Duration `json:"duration"`

	// NodesExecuted is the count of nodes that ran.
	NodesExecuted int `json:"nodes_executed"`

	// Output is the terminal node's output (if successful).
	Output any `json:"output,omitempty"`

	// Error is the error message (if failed).
	Error string `json:"error,omitempty"`

	// FailedNode is the node that caused failure.
	FailedNode string `json:"failed_node,omitempty"`

	// NodeDurations tracks execution time per node.
	NodeDurations map[string]time.Duration `json:"node_durations,omitempty"`
}
