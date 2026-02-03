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
	"errors"
	"fmt"
)

// Sentinel errors for the dag package.
var (
	// ErrNilContext is returned when a nil context is passed.
	ErrNilContext = errors.New("context must not be nil")

	// ErrNilNode is returned when a nil node is provided.
	ErrNilNode = errors.New("node must not be nil")

	// ErrDuplicateNode is returned when adding a node with an existing name.
	ErrDuplicateNode = errors.New("node with this name already exists")

	// ErrNodeNotFound is returned when a referenced node doesn't exist.
	ErrNodeNotFound = errors.New("node not found")

	// ErrCycleDetected is returned when the DAG contains a cycle.
	ErrCycleDetected = errors.New("cycle detected in DAG")

	// ErrNoProgress is returned when no nodes can make progress (deadlock).
	ErrNoProgress = errors.New("no progress possible: deadlock or missing dependency")

	// ErrNodeTimeout is returned when a node exceeds its timeout.
	ErrNodeTimeout = errors.New("node execution timed out")

	// ErrNodeFailed is returned when a node fails during execution.
	ErrNodeFailed = errors.New("node execution failed")

	// ErrDAGNotBuilt is returned when trying to execute an unbuilt DAG.
	ErrDAGNotBuilt = errors.New("DAG has not been built")

	// ErrAlreadyRunning is returned when trying to run an already running executor.
	ErrAlreadyRunning = errors.New("executor is already running")

	// ErrCheckpointCorrupt is returned when a checkpoint fails verification.
	ErrCheckpointCorrupt = errors.New("checkpoint data is corrupt")

	// ErrCheckpointVersionMismatch is returned when checkpoint version doesn't match.
	ErrCheckpointVersionMismatch = errors.New("checkpoint version mismatch")

	// ErrInvalidInput is returned when input validation fails.
	ErrInvalidInput = errors.New("invalid input")
)

// NodeError wraps an error with the node that caused it.
type NodeError struct {
	NodeName string
	Err      error
}

// Error returns the error message.
func (e *NodeError) Error() string {
	return fmt.Sprintf("node %q: %v", e.NodeName, e.Err)
}

// Unwrap returns the underlying error.
func (e *NodeError) Unwrap() error {
	return e.Err
}

// NewNodeError creates a NodeError.
func NewNodeError(nodeName string, err error) *NodeError {
	return &NodeError{
		NodeName: nodeName,
		Err:      err,
	}
}

// CycleError provides details about a detected cycle.
type CycleError struct {
	Path []string
}

// Error returns the cycle description.
func (e *CycleError) Error() string {
	return fmt.Sprintf("cycle detected: %v", e.Path)
}

// NewCycleError creates a CycleError.
func NewCycleError(path []string) *CycleError {
	return &CycleError{Path: path}
}
