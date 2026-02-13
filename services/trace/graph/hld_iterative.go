// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
)

// dfsFrame represents a stack frame for iterative DFS traversal.
type dfsFrame struct {
	v         int  // Current node
	p         int  // Parent node
	d         int  // Depth from root
	childIdx  int  // Index of current child being processed
	returning bool // True if returning from child
}

// dfsSubtreeSizeIterative computes subtree sizes using iterative DFS.
//
// Description:
//
//	Iterative version of dfsSubtreeSize that avoids recursion stack.
//	Uses post-order traversal: visit children first, then parent.
//	Prevents stack overflow for graphs with depth >10K.
//
// Algorithm:
//
//	Two-stack approach:
//	1. First stack pushes nodes in pre-order
//	2. Second stack pops in post-order to compute sizes
//	Time:  O(V) where V = number of nodes
//	Space: O(V) for explicit stacks
//
// Inputs:
//   - ctx: Context for cancellation checking
//   - root: Root node index
//   - adj: Adjacency list
//
// Outputs:
//   - error: Non-nil if context is cancelled
//
// Modifies: hld.parent, hld.depth, hld.subSize arrays
//
// Thread Safety: NOT safe for concurrent use.
func (hld *HLDecomposition) dfsSubtreeSizeIterative(ctx context.Context, root int, adj [][]int) error {
	// Phase 1: Pre-order traversal to set parent and depth
	type preFrame struct {
		v int
		p int
		d int
	}

	preStack := []preFrame{{v: root, p: -1, d: 0}}
	postOrder := []int{} // Store nodes in reverse post-order

	for len(preStack) > 0 {
		// Check context periodically
		if len(preStack)%100 == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("subtree size calculation: %w", ctx.Err())
			default:
			}
		}

		frame := preStack[len(preStack)-1]
		preStack = preStack[:len(preStack)-1]

		// Set parent and depth
		hld.parent[frame.v] = frame.p
		hld.depth[frame.v] = frame.d
		hld.subSize[frame.v] = 1 // Initialize

		// Add to post-order list
		postOrder = append(postOrder, frame.v)

		// Push children to pre-order stack
		for _, child := range adj[frame.v] {
			if child != frame.p {
				preStack = append(preStack, preFrame{
					v: child,
					p: frame.v,
					d: frame.d + 1,
				})
			}
		}
	}

	// Phase 2: Post-order traversal to compute subtree sizes
	// Process in reverse order (leaf to root)
	for i := len(postOrder) - 1; i >= 0; i-- {
		// Check context periodically
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("subtree size accumulation: %w", ctx.Err())
			default:
			}
		}

		v := postOrder[i]

		// Accumulate child subtree sizes
		for _, child := range adj[v] {
			if hld.parent[child] == v {
				hld.subSize[v] += hld.subSize[child]
			}
		}
	}

	return nil
}

// dfsDecomposeIterative performs iterative path decomposition.
//
// Description:
//
//	Iterative version of dfsDecompose that avoids recursion stack.
//	Visits heavy children first, then light children (starting new paths).
//
// Algorithm:
//
//	Time:  O(V) where V = number of nodes
//	Space: O(V) for explicit stack
//
// Inputs:
//   - ctx: Context for cancellation checking
//   - root: Root node index
//   - head: Head of current heavy path
//   - pos: Pointer to position counter
//   - adj: Adjacency list
//
// Outputs:
//   - error: Non-nil if context is cancelled
//
// Modifies: hld.head, hld.pos, hld.nodeAtPos arrays
//
// Thread Safety: NOT safe for concurrent use.
func (hld *HLDecomposition) dfsDecomposeIterative(ctx context.Context, root, head int, pos *int, adj [][]int) error {
	type decomposeFrame struct {
		v            int  // Current node
		h            int  // Head of heavy path
		visitedHeavy bool // Whether heavy child has been visited
	}

	stack := []decomposeFrame{{v: root, h: head, visitedHeavy: false}}

	for len(stack) > 0 {
		// Check for context cancellation periodically
		if len(stack)%100 == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("path decomposition: %w", ctx.Err())
			default:
			}
		}

		frame := &stack[len(stack)-1]

		if !frame.visitedHeavy {
			// First visit: assign head and position
			hld.head[frame.v] = frame.h
			hld.pos[frame.v] = *pos
			hld.nodeAtPos[*pos] = frame.v
			*pos++

			// Mark heavy child as visited
			frame.visitedHeavy = true

			// Push heavy child first (extends current path)
			if hld.heavy[frame.v] != -1 {
				stack = append(stack, decomposeFrame{
					v:            hld.heavy[frame.v],
					h:            frame.h, // Same head
					visitedHeavy: false,
				})
				continue
			}
		}

		// Heavy child visited (or doesn't exist), now visit light children
		// Capture node values before popping
		v := frame.v
		heavyChild := hld.heavy[v]

		// Pop current frame
		stack = stack[:len(stack)-1]

		// Push light children in reverse order (so they're processed in correct order)
		for i := len(adj[v]) - 1; i >= 0; i-- {
			child := adj[v][i]
			if hld.parent[child] == v && child != heavyChild {
				stack = append(stack, decomposeFrame{
					v:            child,
					h:            child, // Light child starts new path
					visitedHeavy: false,
				})
			}
		}
	}

	return nil
}

// buildHLDInternal is an internal builder that optionally skips global tree validation.
//
// Used by BuildHLDForest to build HLD for individual trees in a forest.
func buildHLDInternal(ctx context.Context, g *Graph, root string, skipTreeValidation bool) (*HLDecomposition, error) {
	// Phase 1: Input Validation
	if err := validateBuildInputs(ctx, g, root); err != nil {
		return nil, err
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.BuildHLD",
		trace.WithAttributes(
			attribute.String("root", root),
			attribute.Bool("skip_tree_validation", skipTreeValidation),
		),
	)
	defer span.End()

	start := time.Now()

	// Phase 2: Tree Structure Validation (optional)
	if !skipTreeValidation {
		span.AddEvent("validating_tree")
		if err := g.IsTree(ctx, root); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid tree")
			return nil, fmt.Errorf("%w: %v", ErrInvalidTree, err)
		}
	}

	// Phase 3: Initialize HLD structure
	span.AddEvent("initializing_hld")
	var hld *HLDecomposition
	var err error

	if skipTreeValidation {
		// Forest mode: collect only nodes reachable from root
		span.AddEvent("collecting_component_nodes")
		visited := make(map[string]bool)
		component := []string{}
		if err := collectComponent(ctx, g, root, visited, &component); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "component collection failed")
			return nil, fmt.Errorf("collect component: %w", err)
		}
		span.SetAttributes(attribute.Int("component_size", len(component)))

		hld, err = initializeHLDForComponent(component, root)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "initialization failed")
			return nil, fmt.Errorf("initialize HLD for component: %w", err)
		}
	} else {
		// Single tree mode: use all graph nodes
		hld, err = initializeHLD(g, root)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "initialization failed")
			return nil, fmt.Errorf("initialize HLD: %w", err)
		}
	}

	span.SetAttributes(attribute.Int("node_count", hld.nodeCount))

	// Phase 4: Build adjacency list
	span.AddEvent("building_adjacency_list")
	adj := hld.buildAdjacencyList(g)

	// Phase 5: DFS - compute subtree sizes
	span.AddEvent("computing_subtree_sizes")
	if err := hld.dfsSubtreeSize(ctx, hld.root, -1, 0, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "subtree size computation failed")
		return nil, fmt.Errorf("compute subtree sizes: %w", err)
	}

	// Phase 6: Select heavy children
	span.AddEvent("selecting_heavy_children")
	hld.selectHeavyChildren(adj)

	// Phase 7: DFS - decompose paths
	span.AddEvent("decomposing_paths")
	pos := 0
	if err := hld.dfsDecompose(ctx, hld.root, hld.root, &pos, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "path decomposition failed")
		return nil, fmt.Errorf("decompose paths: %w", err)
	}

	// Phase 8: Validation
	span.AddEvent("validating_hld")
	if err := hld.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		recordHLDValidationError(ctx)
		slog.Error("HLD validation failed",
			slog.String("root", root),
			slog.Int("node_count", hld.nodeCount),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	// Phase 9: Compute statistics
	hld.buildTime = time.Since(start)
	stats := hld.Stats()

	// Phase 10: Record final metrics
	span.SetAttributes(
		attribute.Int("heavy_path_count", stats.HeavyPathCount),
		attribute.Int("light_edge_count", stats.LightEdgeCount),
		attribute.Int64("construction_time_ms", stats.ConstructionTime.Milliseconds()),
		attribute.Int("max_path_length", stats.MaxPathLength),
	)

	recordHLDMetrics(ctx, stats)

	span.SetStatus(codes.Ok, "HLD constructed successfully")
	slog.Info("HLD construction complete",
		slog.String("root", root),
		slog.Int("node_count", hld.nodeCount),
		slog.Int("heavy_paths", stats.HeavyPathCount),
		slog.Int("light_edges", stats.LightEdgeCount),
		slog.Duration("duration", hld.buildTime))

	return hld, nil
}

// buildHLDIterativeInternal is an internal iterative builder that optionally skips global tree validation.
//
// Used by BuildHLDForest to build HLD for individual trees in a forest.
func buildHLDIterativeInternal(ctx context.Context, g *Graph, root string, skipTreeValidation bool) (*HLDecomposition, error) {
	// Phase 1: Input Validation
	if err := validateBuildInputs(ctx, g, root); err != nil {
		return nil, err
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.BuildHLDIterative",
		trace.WithAttributes(
			attribute.String("root", root),
			attribute.Bool("iterative", true),
			attribute.Bool("skip_tree_validation", skipTreeValidation),
		),
	)
	defer span.End()

	start := time.Now()

	// Phase 2: Tree Structure Validation (optional)
	if !skipTreeValidation {
		span.AddEvent("validating_tree")
		if err := g.IsTree(ctx, root); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid tree")
			return nil, fmt.Errorf("%w: %v", ErrInvalidTree, err)
		}
	}

	// Phase 3: Initialize HLD structure
	span.AddEvent("initializing_hld")
	var hld *HLDecomposition
	var err error

	if skipTreeValidation {
		// Forest mode: collect only nodes reachable from root
		span.AddEvent("collecting_component_nodes")
		visited := make(map[string]bool)
		component := []string{}
		if err := collectComponent(ctx, g, root, visited, &component); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "component collection failed")
			return nil, fmt.Errorf("collect component: %w", err)
		}
		span.SetAttributes(attribute.Int("component_size", len(component)))

		hld, err = initializeHLDForComponent(component, root)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "initialization failed")
			return nil, fmt.Errorf("initialize HLD for component: %w", err)
		}
	} else {
		// Single tree mode: use all graph nodes
		hld, err = initializeHLD(g, root)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "initialization failed")
			return nil, fmt.Errorf("initialize HLD: %w", err)
		}
	}

	span.SetAttributes(attribute.Int("node_count", hld.nodeCount))

	// Phase 4: Build adjacency list
	span.AddEvent("building_adjacency_list")
	adj := hld.buildAdjacencyList(g)

	// Phase 5: Iterative DFS - compute subtree sizes
	span.AddEvent("computing_subtree_sizes_iterative")
	if err := hld.dfsSubtreeSizeIterative(ctx, hld.root, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "subtree size computation failed")
		return nil, fmt.Errorf("compute subtree sizes: %w", err)
	}

	// Phase 6: Select heavy children
	span.AddEvent("selecting_heavy_children")
	hld.selectHeavyChildren(adj)

	// Phase 7: Iterative DFS - decompose paths
	span.AddEvent("decomposing_paths_iterative")
	pos := 0
	if err := hld.dfsDecomposeIterative(ctx, hld.root, hld.root, &pos, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "path decomposition failed")
		return nil, fmt.Errorf("decompose paths: %w", err)
	}

	// Phase 8: Validation
	span.AddEvent("validating_hld")
	if err := hld.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		recordHLDValidationError(ctx)
		slog.Error("HLD validation failed",
			slog.String("root", root),
			slog.Int("node_count", hld.nodeCount),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	// Phase 9: Compute statistics
	hld.buildTime = time.Since(start)
	stats := hld.Stats()

	// Phase 10: Record final metrics
	span.SetAttributes(
		attribute.Int("heavy_path_count", stats.HeavyPathCount),
		attribute.Int("light_edge_count", stats.LightEdgeCount),
		attribute.Int64("construction_time_ms", stats.ConstructionTime.Milliseconds()),
		attribute.Int("max_path_length", stats.MaxPathLength),
	)

	recordHLDMetrics(ctx, stats)

	// Compute and store graph hash for validation
	hld.graphHash = g.Hash()

	span.SetStatus(codes.Ok, "HLD constructed successfully (iterative)")
	slog.Info("HLD construction complete (iterative)",
		slog.String("root", root),
		slog.Int("node_count", hld.nodeCount),
		slog.Int("heavy_paths", stats.HeavyPathCount),
		slog.Int("light_edges", stats.LightEdgeCount),
		slog.Duration("duration", hld.buildTime))

	return hld, nil
}

// BuildHLDIterative constructs HLD using iterative DFS (no recursion).
//
// Description:
//
//	Identical to BuildHLD but uses iterative DFS to avoid stack overflow
//	for deep trees (>10K depth). Recommended for production use with
//	large or unknown graph structures.
//
// Algorithm:
//
//	Time:  O(V) where V = number of nodes
//	Space: O(V) for arrays + O(V) for explicit DFS stack
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - g: Tree graph. Must be frozen, acyclic, connected.
//   - root: Root node ID. Must exist in graph.
//
// Outputs:
//   - *HLDecomposition: Constructed HLD structure. Never nil on success.
//   - error: Non-nil if graph is invalid or operation is cancelled.
//
// Differences from BuildHLD:
//   - Uses iterative DFS (explicit stack) instead of recursive DFS
//   - No stack overflow risk for deep trees
//   - Slightly more memory overhead (explicit stack)
//
// Example:
//
//	// For large or deep graphs
//	hld, err := BuildHLDIterative(ctx, callGraph, "main")
//	if err != nil {
//	    return fmt.Errorf("build HLD: %w", err)
//	}
//
// Thread Safety: Safe for concurrent use with different graphs.
func BuildHLDIterative(ctx context.Context, g *Graph, root string) (*HLDecomposition, error) {
	// Phase 1: Input Validation
	if err := validateBuildInputs(ctx, g, root); err != nil {
		return nil, err
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.BuildHLDIterative",
		trace.WithAttributes(
			attribute.String("root", root),
			attribute.Bool("iterative", true),
		),
	)
	defer span.End()

	start := time.Now()

	span.AddEvent("validating_inputs")

	// Phase 2: Tree Structure Validation
	span.AddEvent("validating_tree")
	if err := g.IsTree(ctx, root); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid tree")
		return nil, fmt.Errorf("%w: %v", ErrInvalidTree, err)
	}

	// Phase 3: Initialize HLD structure
	span.AddEvent("initializing_hld")
	hld, err := initializeHLD(g, root)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "initialization failed")
		return nil, fmt.Errorf("initialize HLD: %w", err)
	}

	span.SetAttributes(attribute.Int("node_count", hld.nodeCount))

	// Phase 4: Build adjacency list
	span.AddEvent("building_adjacency_list")
	adj := hld.buildAdjacencyList(g)

	// Phase 5: Iterative DFS - compute subtree sizes
	span.AddEvent("computing_subtree_sizes_iterative")
	if err := hld.dfsSubtreeSizeIterative(ctx, hld.root, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "subtree size computation failed")
		return nil, fmt.Errorf("compute subtree sizes: %w", err)
	}

	// Phase 6: Select heavy children
	span.AddEvent("selecting_heavy_children")
	hld.selectHeavyChildren(adj)

	// Phase 7: Iterative DFS - decompose paths
	span.AddEvent("decomposing_paths_iterative")
	pos := 0
	if err := hld.dfsDecomposeIterative(ctx, hld.root, hld.root, &pos, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "path decomposition failed")
		return nil, fmt.Errorf("decompose paths: %w", err)
	}

	// Phase 8: Validation
	span.AddEvent("validating_hld")
	if err := hld.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		recordHLDValidationError(ctx)
		slog.Error("HLD validation failed",
			slog.String("root", root),
			slog.Int("node_count", hld.nodeCount),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	// Phase 9: Compute statistics
	hld.buildTime = time.Since(start)
	stats := hld.Stats()

	// Phase 10: Record final metrics
	span.SetAttributes(
		attribute.Int("heavy_path_count", stats.HeavyPathCount),
		attribute.Int("light_edge_count", stats.LightEdgeCount),
		attribute.Int64("construction_time_ms", stats.ConstructionTime.Milliseconds()),
		attribute.Int("max_path_length", stats.MaxPathLength),
	)

	recordHLDMetrics(ctx, stats)

	// Compute and store graph hash for validation
	hld.graphHash = g.Hash()

	span.SetStatus(codes.Ok, "HLD constructed successfully (iterative)")
	slog.Info("HLD construction complete (iterative)",
		slog.String("root", root),
		slog.Int("node_count", hld.nodeCount),
		slog.Int("heavy_paths", stats.HeavyPathCount),
		slog.Int("light_edges", stats.LightEdgeCount),
		slog.Duration("duration", hld.buildTime))

	return hld, nil
}

// HLDForest represents multiple HLD decompositions for a forest (disconnected graph).
//
// Description:
//
//	A forest is a collection of disconnected trees. This structure maintains
//	separate HLD decompositions for each tree in the forest.
//
// Thread Safety:
//
//	Read-only after construction. Safe for concurrent queries.
type HLDForest struct {
	decompositions []*HLDecomposition // One HLD per tree
	roots          []string           // Root node IDs for each tree
	nodeToTree     map[string]int     // Maps node ID to tree index
}

// BuildHLDForest constructs HLD for each tree in a forest.
//
// Description:
//
//	Detects all connected components in the graph and builds separate
//	HLD decompositions for each tree. Useful for graphs that may have
//	multiple disconnected subgraphs.
//
// Algorithm:
//
//	Time:  O(V + E) where V = nodes, E = edges
//	Space: O(V) for each tree
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - g: Graph (may be forest). Must be frozen, acyclic.
//   - useIterative: If true, uses iterative DFS (recommended for deep trees).
//
// Outputs:
//   - *HLDForest: Forest structure with HLD for each tree. Never nil on success.
//   - error: Non-nil if graph contains cycles or operation is cancelled.
//
// Example:
//
//	forest, err := BuildHLDForest(ctx, graph, true)
//	if err != nil {
//	    return fmt.Errorf("build forest: %w", err)
//	}
//	fmt.Printf("Forest has %d trees\n", forest.TreeCount())
//
// Thread Safety: Safe for concurrent use with different graphs.
func BuildHLDForest(ctx context.Context, g *Graph, useIterative bool) (*HLDForest, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if g == nil {
		return nil, fmt.Errorf("graph must not be nil")
	}
	if !g.IsFrozen() {
		return nil, fmt.Errorf("graph must be frozen")
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.BuildHLDForest",
		trace.WithAttributes(
			attribute.Bool("iterative", useIterative),
		),
	)
	defer span.End()

	// Phase 1: Find all connected components (roots)
	span.AddEvent("finding_connected_components")
	roots, err := findForestRoots(ctx, g)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to find roots")
		return nil, fmt.Errorf("find forest roots: %w", err)
	}

	span.SetAttributes(attribute.Int("tree_count", len(roots)))

	if len(roots) == 0 {
		return nil, fmt.Errorf("empty graph")
	}

	// Phase 2: Build HLD for each tree
	forest := &HLDForest{
		decompositions: make([]*HLDecomposition, 0, len(roots)),
		roots:          make([]string, 0, len(roots)),
		nodeToTree:     make(map[string]int),
	}

	for i, root := range roots {
		span.AddEvent(fmt.Sprintf("building_tree_%d", i),
			trace.WithAttributes(
				attribute.String("root", root),
			),
		)

		var hld *HLDecomposition
		// Use internal builder that skips global tree validation
		// (validation would fail for forests since entire graph is disconnected)
		if useIterative {
			hld, err = buildHLDIterativeInternal(ctx, g, root, true)
		} else {
			hld, err = buildHLDInternal(ctx, g, root, true)
		}

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("failed to build tree %d", i))
			return nil, fmt.Errorf("build HLD for tree %d (root=%s): %w", i, root, err)
		}

		forest.decompositions = append(forest.decompositions, hld)
		forest.roots = append(forest.roots, root)

		// Map all nodes in this tree to tree index
		for nodeID := range hld.nodeToIdx {
			forest.nodeToTree[nodeID] = i
		}
	}

	span.SetStatus(codes.Ok, "forest HLD constructed successfully")
	slog.Info("HLD forest construction complete",
		slog.Int("tree_count", len(roots)),
		slog.Bool("iterative", useIterative))

	return forest, nil
}

// TreeCount returns the number of trees in the forest.
func (f *HLDForest) TreeCount() int {
	return len(f.decompositions)
}

// GetHLD returns the HLD decomposition for the tree containing the given node.
//
// Returns nil if node is not found in any tree.
func (f *HLDForest) GetHLD(nodeID string) *HLDecomposition {
	treeIdx, ok := f.nodeToTree[nodeID]
	if !ok {
		return nil
	}
	return f.decompositions[treeIdx]
}

// GetHLDByIndex returns the HLD decomposition for the tree at the given index.
//
// Returns nil if index is out of bounds.
func (f *HLDForest) GetHLDByIndex(index int) *HLDecomposition {
	if index < 0 || index >= len(f.decompositions) {
		return nil
	}
	return f.decompositions[index]
}

// GetRoot returns the root node ID for the tree at the given index.
//
// Returns empty string if index is out of bounds.
func (f *HLDForest) GetRoot(index int) string {
	if index < 0 || index >= len(f.roots) {
		return ""
	}
	return f.roots[index]
}

// GetTreeID returns the tree index for a given node.
//
// Description:
//
//	Returns the index of the tree containing the specified node.
//	Used for validating that two nodes are in the same tree component.
//
// Inputs:
//   - nodeID: Node to look up. Must exist in forest.
//
// Outputs:
//   - int: Tree index (0-based).
//   - error: Non-nil if node not found.
//
// Thread Safety: Safe for concurrent use (read-only).
func (f *HLDForest) GetTreeID(nodeID string) (int, error) {
	treeIdx, ok := f.nodeToTree[nodeID]
	if !ok {
		return -1, fmt.Errorf("node %s not found in forest", nodeID)
	}
	return treeIdx, nil
}

// TotalNodes returns the total number of nodes across all trees in the forest.
//
// Description:
//
//	Sums NodeCount() across all HLD decompositions.
//	Used for validating segment tree size compatibility.
//
// Thread Safety: Safe for concurrent use (read-only).
func (f *HLDForest) TotalNodes() int {
	total := 0
	for _, hld := range f.decompositions {
		total += hld.NodeCount()
	}
	return total
}

// GetTreeOffset returns the segment tree position offset for a node's tree.
//
// Description:
//
//	Calculates the cumulative position offset for the tree containing nodeID.
//	In a forest, segment tree positions are laid out sequentially:
//	  - Tree 0: positions [0, tree0.NodeCount())
//	  - Tree 1: positions [tree0.NodeCount(), tree0.NodeCount() + tree1.NodeCount())
//	  - etc.
//
//	To get global segment tree position from tree-local HLD position:
//	  globalPos = GetTreeOffset(nodeID) + treeHLD.Pos(nodeIdx)
//
// Algorithm:
//
//	Time:  O(T) where T = number of trees (typically small)
//	Space: O(1)
//
// Inputs:
//   - nodeID: Node to get tree offset for. Must exist in forest.
//
// Outputs:
//   - int: Position offset for this node's tree (0 for tree 0).
//   - error: Non-nil if node not found in forest.
//
// Example:
//
//	// Forest with 2 trees: tree0 has 3 nodes, tree1 has 4 nodes
//	// Segment tree positions: [0,1,2 | 3,4,5,6]
//	offset, _ := forest.GetTreeOffset("nodeInTree1") // Returns 3
//	treeHLD := forest.GetHLD("nodeInTree1")
//	nodeIdx, _ := treeHLD.NodeToIdx("nodeInTree1")
//	treePos := treeHLD.Pos(nodeIdx)  // e.g., 0 (local position)
//	globalPos := offset + treePos     // 3 + 0 = 3 (global position)
//
// Thread Safety: Safe for concurrent use (read-only).
func (f *HLDForest) GetTreeOffset(nodeID string) (int, error) {
	if f == nil {
		return 0, errors.New("forest is nil")
	}

	// Get tree index for this node
	treeIdx, err := f.GetTreeID(nodeID)
	if err != nil {
		return 0, err
	}

	// Calculate cumulative offset (sum of all previous tree sizes)
	offset := 0
	for i := 0; i < treeIdx; i++ {
		offset += f.decompositions[i].NodeCount()
	}

	return offset, nil
}

// GraphHash returns combined hash of all trees in the forest.
//
// Description:
//
//	Combines graph hashes from all HLD decompositions to create
//	a unique identifier for the forest structure.
//	Used for cache invalidation when graph changes.
//
// Thread Safety: Safe for concurrent use (read-only).
func (f *HLDForest) GraphHash() string {
	if f == nil || len(f.decompositions) == 0 {
		return ""
	}

	// For single tree, just return its hash
	if len(f.decompositions) == 1 {
		return f.decompositions[0].GraphHash()
	}

	// For multiple trees, combine hashes
	h := fnv.New64a()
	for _, hld := range f.decompositions {
		h.Write([]byte(hld.GraphHash()))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("forest_%016x", h.Sum64())
}

// Validate checks validity of all HLD decompositions in the forest.
//
// Description:
//
//	Validates each HLD decomposition and checks forest invariants.
//
// Thread Safety: Safe for concurrent use (read-only).
func (f *HLDForest) Validate() error {
	if f == nil {
		return errors.New("forest is nil")
	}

	if len(f.decompositions) == 0 {
		return errors.New("forest has no decompositions")
	}

	if len(f.roots) != len(f.decompositions) {
		return fmt.Errorf("roots count %d != decompositions count %d",
			len(f.roots), len(f.decompositions))
	}

	// Validate each HLD
	for i, hld := range f.decompositions {
		if err := hld.Validate(); err != nil {
			return fmt.Errorf("tree %d invalid: %w", i, err)
		}
	}

	// Validate nodeToTree map completeness
	totalNodes := f.TotalNodes()
	if len(f.nodeToTree) != totalNodes {
		return fmt.Errorf("nodeToTree map size %d != total nodes %d",
			len(f.nodeToTree), totalNodes)
	}

	return nil
}

// findForestRoots identifies root nodes for each connected component.
//
// Description:
//
//	Finds all connected components and selects a root for each.
//	For each component, selects the node with minimum incoming edges
//	from within the component (prefers actual tree roots).
//
// Algorithm:
//
//	Time:  O(V + E)
//	Space: O(V) for visited set
//
// Inputs:
//   - ctx: Context for cancellation
//   - g: Graph (frozen)
//
// Outputs:
//   - []string: List of root node IDs, one per connected component
//   - error: Non-nil if context is cancelled
//
// Thread Safety: Safe for concurrent use (read-only graph).
func findForestRoots(ctx context.Context, g *Graph) ([]string, error) {
	visited := make(map[string]bool)
	roots := []string{}

	// Iterate over all nodes
	for nodeID, node := range g.Nodes() {
		// Check context periodically
		if len(visited)%100 == 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("root finding: %w", ctx.Err())
			default:
			}
		}

		if visited[nodeID] || node == nil {
			continue
		}

		// Found a new connected component
		// Find all nodes in this component
		component := []string{}
		if err := collectComponent(ctx, g, nodeID, visited, &component); err != nil {
			return nil, err
		}

		// Select the best root for this component
		// (node with fewest incoming edges from within component)
		root := selectComponentRoot(g, component)
		roots = append(roots, root)
	}

	return roots, nil
}

// collectComponent collects all nodes in a connected component.
func collectComponent(ctx context.Context, g *Graph, start string, visited map[string]bool, component *[]string) error {
	stack := []string{start}

	for len(stack) > 0 {
		// Check context periodically
		if len(stack)%100 == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("component collection: %w", ctx.Err())
			default:
			}
		}

		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if visited[nodeID] {
			continue
		}

		visited[nodeID] = true
		*component = append(*component, nodeID)

		// Get node and its neighbors
		node, ok := g.GetNode(nodeID)
		if !ok || node == nil {
			continue
		}

		// Add all outgoing edges to stack
		for _, edge := range node.Outgoing {
			if !visited[edge.ToID] {
				stack = append(stack, edge.ToID)
			}
		}

		// Add all incoming edges to stack (for undirected behavior)
		for _, edge := range node.Incoming {
			if !visited[edge.FromID] {
				stack = append(stack, edge.FromID)
			}
		}
	}

	return nil
}

// selectComponentRoot selects the best root node for a component.
//
// Prefers nodes with no incoming edges from within the component (actual roots).
// If all nodes have incoming edges, selects the node with fewest incoming edges.
func selectComponentRoot(g *Graph, component []string) string {
	if len(component) == 0 {
		return ""
	}

	// Create set of component nodes for O(1) lookup
	componentSet := make(map[string]bool)
	for _, nodeID := range component {
		componentSet[nodeID] = true
	}

	// Find node with fewest incoming edges from within component
	bestRoot := component[0]
	minIncoming := countIncomingFromComponent(g, component[0], componentSet)

	for _, nodeID := range component[1:] {
		incoming := countIncomingFromComponent(g, nodeID, componentSet)
		if incoming < minIncoming {
			minIncoming = incoming
			bestRoot = nodeID
		}
	}

	return bestRoot
}

// countIncomingFromComponent counts incoming edges from within the component.
func countIncomingFromComponent(g *Graph, nodeID string, componentSet map[string]bool) int {
	node, ok := g.GetNode(nodeID)
	if !ok || node == nil {
		return 0
	}

	count := 0
	for _, edge := range node.Incoming {
		if componentSet[edge.FromID] {
			count++
		}
	}
	return count
}
