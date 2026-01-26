// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package graph provides code relationship graph types and operations.
//
// The graph package contains types for representing code as a directed graph
// where nodes are symbols (functions, types, variables) and edges represent
// relationships (calls, imports, implements, etc.).
//
// # Ownership Model
//
// The graph stores pointers to symbols but does NOT own them:
//   - Symbols MUST NOT be mutated after being added via AddNode()
//   - The graph does NOT copy symbols (for memory efficiency)
//   - Same ownership contract as the index package
//
// # Thread Safety
//
// Graph is NOT safe for concurrent use during building. It is designed for:
//   - Single-writer access during build phase (AddNode, AddEdge calls)
//   - Read-only access after Freeze() is called
//
// After Freeze(), the graph can be safely read from multiple goroutines.
//
// # Lifecycle
//
// A typical graph lifecycle:
//  1. Create with NewGraph(projectRoot)
//  2. Build with AddNode() and AddEdge() calls
//  3. Call Freeze() to finalize
//  4. Query with GetNode(), traversal methods, etc.
package graph

import "errors"

// Sentinel errors for graph operations.
var (
	// ErrGraphFrozen is returned when attempting to modify a frozen graph.
	// Once Freeze() is called, the graph becomes read-only and no further
	// nodes or edges can be added.
	ErrGraphFrozen = errors.New("graph is frozen and cannot be modified")

	// ErrNodeNotFound is returned when an edge references a non-existent node.
	// Both source and target nodes must exist before an edge can be created.
	ErrNodeNotFound = errors.New("node not found")

	// ErrDuplicateNode is returned when adding a node with an ID that
	// already exists in the graph.
	ErrDuplicateNode = errors.New("duplicate node ID")

	// ErrMaxNodesExceeded is returned when the graph has reached its
	// configured maximum node capacity.
	ErrMaxNodesExceeded = errors.New("maximum node count exceeded")

	// ErrMaxEdgesExceeded is returned when the graph has reached its
	// configured maximum edge capacity.
	ErrMaxEdgesExceeded = errors.New("maximum edge count exceeded")

	// ErrInvalidNode is returned when attempting to add a nil symbol
	// or a symbol that fails validation.
	ErrInvalidNode = errors.New("invalid node")

	// ErrBuildCancelled is returned when a build operation is cancelled via context.
	ErrBuildCancelled = errors.New("build cancelled")

	// ErrMemoryLimitExceeded is returned when the builder exceeds its configured
	// memory limit during graph construction.
	ErrMemoryLimitExceeded = errors.New("memory limit exceeded")

	// ErrInvalidEdgeType is returned when an edge type is not valid for the
	// given source and target node kinds.
	ErrInvalidEdgeType = errors.New("invalid edge type for node kinds")
)
