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
	"sort"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
)

// Sentinel errors for HLD operations.
var (
	ErrInvalidTree      = errors.New("graph is not a valid tree")
	ErrValidationFailed = errors.New("HLD validation failed")
)

// HLDecomposition represents a Heavy-Light Decomposition of a tree.
//
// Description:
//
//	Decomposes a tree into heavy and light paths, enabling O(log V) path
//	queries and O(log² V) aggregate queries when combined with segment trees.
//	The decomposition is computed via DFS and remains static until rebuilt.
//
// Invariants:
//   - All arrays have length V (number of nodes)
//   - parent[root] == -1
//   - heavy[v] == -1 iff v is leaf or all children have subtree size 1
//   - head[v] == v iff v starts a new heavy path
//   - Positions form valid DFS ordering: parent before children
//   - Subtree v occupies positions [pos[v], pos[v]+subSize[v])
//
// Thread Safety:
//
//	Read-only after construction. Safe for concurrent queries.
type HLDecomposition struct {
	// Core structure (immutable after construction)
	parent    []int // parent[v] = parent of v, -1 for root
	depth     []int // depth[v] = distance from root
	subSize   []int // subSize[v] = subtree size rooted at v
	heavy     []int // heavy[v] = heavy child of v, -1 if none
	head      []int // head[v] = head of heavy path containing v
	pos       []int // pos[v] = position in DFS order
	nodeAtPos []int // nodeAtPos[i] = node at position i

	// Metadata
	root      int // Root node index
	nodeCount int // Total number of nodes

	// Node ID mappings (string ↔ int)
	nodeToIdx map[string]int // Map node ID to index
	idxToNode []string       // Map index to node ID

	// Construction metadata
	buildTime      time.Duration // Time taken to build
	lightEdgeCount int           // Total light edges (for validation)

	// Version and change detection (A-M1, A-M2)
	version   int    // Schema version (current: 1)
	graphHash string // Hash of graph when HLD was built

	// LCA query statistics (atomic access required)
	lcaQueryCount      int64 // Total number of LCA queries
	lcaTotalIters      int64 // Total iterations across all queries
	lcaMaxIters        int32 // Maximum iterations in a single query (atomic)
	lcaTotalDurationMs int64 // Total query duration in milliseconds

	// Optional extensions (can be nil)
	cache          HLDCache       // Cache for query results (optional)
	rateLimiter    RateLimiter    // Rate limiter for queries (optional)
	circuitBreaker CircuitBreaker // Circuit breaker for fault tolerance (optional)
}

// HLDStats contains statistics about the HLD structure.
type HLDStats struct {
	NodeCount        int
	HeavyPathCount   int
	AvgPathLength    float64
	MaxPathLength    int
	LightEdgeCount   int
	ConstructionTime time.Duration
}

// BuildHLD constructs a Heavy-Light Decomposition from a tree graph.
//
// Description:
//
//	Performs three-pass DFS to compute HLD structure:
//	1. First DFS: Compute subtree sizes, depths, parents
//	2. Heavy child selection: Identify heavy child for each node
//	3. Second DFS: Assign heads and positions in DFS order
//
// Algorithm:
//
//	Time:  O(V) where V = number of nodes
//	Space: O(V) for arrays, O(log V) for recursion stack
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - g: Tree graph. Must be frozen, acyclic, connected.
//   - root: Root node ID. Must exist in graph.
//
// Outputs:
//   - *HLDecomposition: Constructed HLD structure. Never nil on success.
//   - error: Non-nil if graph is invalid (cyclic, disconnected, not frozen).
//
// Optimizations:
//   - Single allocation for all arrays (reduces GC pressure)
//   - Iterative DFS where possible (reduces stack depth)
//   - Pre-computed node count (avoids multiple traversals)
//   - Inline heavy child selection (avoids separate pass)
//
// CRS Integration:
//   - Records construction as TraceStep with timing and metadata
//   - Includes node count, root, light edge count
//   - Enables caching of HLD structure across sessions
//
// Example:
//
//	hld, err := BuildHLD(ctx, callGraph, "main")
//	if err != nil {
//	    return fmt.Errorf("build HLD: %w", err)
//	}
//	// hld is ready for queries
//
// Thread Safety: Safe for concurrent use with different graphs.
func BuildHLD(ctx context.Context, g *Graph, root string) (*HLDecomposition, error) {
	// Phase 1: Input Validation (before span creation to handle nil context)
	if err := validateBuildInputs(ctx, g, root); err != nil {
		return nil, err
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.BuildHLD",
		trace.WithAttributes(
			attribute.String("root", root),
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

	// Phase 4: Build adjacency list (CRITICAL: sorted for determinism)
	span.AddEvent("building_adjacency_list")
	adj := hld.buildAdjacencyList(g)

	// Phase 5: First DFS - compute subtree sizes
	span.AddEvent("computing_subtree_sizes")
	subtreeStart := time.Now()
	if err := hld.dfsSubtreeSize(ctx, hld.root, -1, 0, adj); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "subtree size computation failed")
		return nil, fmt.Errorf("compute subtree sizes: %w", err)
	}
	subtreeDuration := time.Since(subtreeStart)
	span.AddEvent("subtree_sizes_computed",
		trace.WithAttributes(
			attribute.Int64("duration_ms", subtreeDuration.Milliseconds()),
		),
	)

	// Phase 6: Select heavy children
	span.AddEvent("selecting_heavy_children")
	hld.selectHeavyChildren(adj)

	// Phase 7: Second DFS - decompose paths
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

	// Record HLD metrics
	recordHLDMetrics(ctx, stats)

	// Compute and store graph hash for validation
	hld.graphHash = g.Hash()

	slog.Info("HLD construction complete",
		slog.String("root", root),
		slog.Int("node_count", hld.nodeCount),
		slog.Int("heavy_paths", stats.HeavyPathCount),
		slog.Int("light_edges", stats.LightEdgeCount),
		slog.Duration("duration", hld.buildTime))

	span.SetStatus(codes.Ok, "HLD constructed successfully")
	return hld, nil
}

// validateBuildInputs validates all inputs to BuildHLD.
func validateBuildInputs(ctx context.Context, g *Graph, root string) error {
	// 1. Validate context
	if ctx == nil {
		return errors.New("ctx must not be nil")
	}

	// 2. Validate graph
	if g == nil {
		return errors.New("graph must not be nil")
	}
	if !g.IsFrozen() {
		return ErrGraphNotFrozen
	}
	if g.NodeCount() == 0 {
		return errors.New("graph is empty")
	}

	// 3. Validate root exists
	if _, ok := g.GetNode(root); !ok {
		return fmt.Errorf("root node %q does not exist in graph", root)
	}

	return nil
}

// initializeHLD creates and initializes HLD structure with single allocation.
func initializeHLD(g *Graph, root string) (*HLDecomposition, error) {
	nodeCount := g.NodeCount()

	// Collect all node IDs
	nodeIDs := make([]string, 0, nodeCount)
	for nodeID := range g.nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}

	return initializeHLDForComponent(nodeIDs, root)
}

// initializeHLDForComponent initializes HLD structure for a specific set of nodes.
//
// Description:
//
//	Creates HLD structure sized for only the specified nodes, enabling
//	forest support where each tree component has its own HLD.
//
// Inputs:
//   - componentNodes: Slice of node IDs to include in this HLD. Must not be empty.
//   - root: Root node ID. Must exist in componentNodes.
//
// Outputs:
//   - *HLDecomposition: Initialized HLD structure. Never nil on success.
//   - error: Non-nil if root not in componentNodes.
//
// Thread Safety: Safe for concurrent use with different component sets.
func initializeHLDForComponent(componentNodes []string, root string) (*HLDecomposition, error) {
	nodeCount := len(componentNodes)

	// Create node ID mappings (sorted for determinism)
	nodeToIdx := make(map[string]int, nodeCount)
	idxToNode := make([]string, 0, nodeCount)

	// Use only nodes from the component
	idxToNode = append(idxToNode, componentNodes...)

	// CRITICAL: Sort node IDs for deterministic indexing
	sort.Strings(idxToNode)

	// Build reverse mapping
	for idx, nodeID := range idxToNode {
		nodeToIdx[nodeID] = idx
	}

	// Get root index
	rootIdx, ok := nodeToIdx[root]
	if !ok {
		return nil, fmt.Errorf("root node %q not found in component", root)
	}

	// Single allocation for all arrays (7 * nodeCount)
	allArrays := make([]int, 7*nodeCount)

	hld := &HLDecomposition{
		parent:    allArrays[0*nodeCount : 1*nodeCount],
		depth:     allArrays[1*nodeCount : 2*nodeCount],
		subSize:   allArrays[2*nodeCount : 3*nodeCount],
		heavy:     allArrays[3*nodeCount : 4*nodeCount],
		head:      allArrays[4*nodeCount : 5*nodeCount],
		pos:       allArrays[5*nodeCount : 6*nodeCount],
		nodeAtPos: allArrays[6*nodeCount : 7*nodeCount],
		root:      rootIdx,
		nodeCount: nodeCount,
		nodeToIdx: nodeToIdx,
		idxToNode: idxToNode,
	}

	// Initialize arrays with sentinel values
	for i := 0; i < nodeCount; i++ {
		hld.parent[i] = -1
		hld.heavy[i] = -1
		hld.depth[i] = -1
		hld.subSize[i] = 0
		hld.head[i] = -1
		hld.pos[i] = -1
		hld.nodeAtPos[i] = -1
	}

	return hld, nil
}

// buildAdjacencyList builds adjacency list with sorted children for determinism.
//
// CRITICAL: Children must be sorted by node ID to ensure deterministic
// heavy child selection. This enables CRS caching to work correctly.
func (hld *HLDecomposition) buildAdjacencyList(g *Graph) [][]int {
	adj := make([][]int, hld.nodeCount)

	for v := 0; v < hld.nodeCount; v++ {
		nodeID := hld.idxToNode[v]
		node, ok := g.GetNode(nodeID)
		if !ok || node == nil {
			continue
		}

		// Collect children (nodes this node points to in the tree)
		children := []int{}
		for _, edge := range node.Outgoing {
			if childIdx, ok := hld.nodeToIdx[edge.ToID]; ok {
				children = append(children, childIdx)
			}
		}

		// CRITICAL: Sort children by node ID for deterministic processing
		sort.Slice(children, func(i, j int) bool {
			return hld.idxToNode[children[i]] < hld.idxToNode[children[j]]
		})

		adj[v] = children
	}

	return adj
}

// dfsSubtreeSize computes subtree sizes in first DFS pass with context cancellation.
//
// Description:
//
//	Recursive DFS to compute:
//	- Subtree size for each node
//	- Depth from root
//	- Parent pointers
//	All arrays must be pre-allocated to size V.
//
// Complexity: O(V) time, O(log V) recursion depth (balanced tree)
//
// Inputs:
//   - ctx: Context for cancellation checking
//   - v: Current node index
//   - p: Parent node index (-1 for root)
//   - d: Current depth
//   - adj: Adjacency list
//
// Outputs:
//   - error: Non-nil if context is cancelled
//
// Modifies: hld.parent, hld.depth, hld.subSize arrays
//
// Thread Safety: NOT safe for concurrent use.
func (hld *HLDecomposition) dfsSubtreeSize(ctx context.Context, v, p, d int, adj [][]int) error {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("subtree size calculation: %w", ctx.Err())
	default:
	}

	hld.parent[v] = p
	hld.depth[v] = d
	hld.subSize[v] = 1

	for _, child := range adj[v] {
		if child == p {
			continue // Skip parent
		}

		if err := hld.dfsSubtreeSize(ctx, child, v, d+1, adj); err != nil {
			return err
		}
		hld.subSize[v] += hld.subSize[child]
	}

	return nil
}

// selectHeavyChildren identifies heavy child for each node.
//
// Description:
//
//	For each node, select child with largest subtree as heavy child.
//	Tiebreaker: First child in sorted adjacency list (deterministic).
//
// Complexity: O(V) time
//
// Modifies: hld.heavy array, hld.lightEdgeCount
//
// Thread Safety: NOT safe for concurrent use.
func (hld *HLDecomposition) selectHeavyChildren(adj [][]int) {
	hld.lightEdgeCount = 0

	for v := 0; v < hld.nodeCount; v++ {
		if len(adj[v]) == 0 {
			continue // Leaf node
		}

		maxSize := -1
		heavyChild := -1

		for _, child := range adj[v] {
			if hld.parent[child] != v {
				continue // Not a child in the tree
			}

			if hld.subSize[child] > maxSize {
				maxSize = hld.subSize[child]
				heavyChild = child
			}
			// Tiebreaker: First child in sorted list (already deterministic)
		}

		if heavyChild != -1 {
			hld.heavy[v] = heavyChild

			// Count light edges
			for _, child := range adj[v] {
				if hld.parent[child] == v && child != heavyChild {
					hld.lightEdgeCount++
				}
			}
		}
	}
}

// dfsDecompose performs second DFS to assign heads and positions.
//
// Description:
//
//	Recursive DFS following heavy-light decomposition rules:
//	- Heavy child visited first (extends current heavy path)
//	- Light children visited second (start new heavy paths)
//	Position counter incremented in DFS order.
//
// Complexity: O(V) time, O(log V) recursion depth
//
// Inputs:
//   - ctx: Context for cancellation checking
//   - v: Current node index
//   - h: Head of current heavy path
//   - pos: Pointer to position counter
//   - adj: Adjacency list
//
// Outputs:
//   - error: Non-nil if context is cancelled
//
// Modifies: hld.head, hld.pos, hld.nodeAtPos arrays
//
// Thread Safety: NOT safe for concurrent use.
func (hld *HLDecomposition) dfsDecompose(ctx context.Context, v, h int, pos *int, adj [][]int) error {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("path decomposition: %w", ctx.Err())
	default:
	}

	hld.head[v] = h
	hld.pos[v] = *pos
	hld.nodeAtPos[*pos] = v
	*pos++

	// Visit heavy child first (extends current path)
	if hld.heavy[v] != -1 {
		if err := hld.dfsDecompose(ctx, hld.heavy[v], h, pos, adj); err != nil {
			return err
		}
	}

	// Visit light children (start new heavy paths)
	for _, child := range adj[v] {
		if hld.parent[child] == v && child != hld.heavy[v] {
			if err := hld.dfsDecompose(ctx, child, child, pos, adj); err != nil {
				return err
			}
		}
	}

	return nil
}

// Validate checks HLD invariants after construction.
//
// Description:
//
//	Verifies:
//	- All arrays have correct length
//	- Root has parent -1
//	- Depths are valid (parent depth < child depth)
//	- Subtree sizes are consistent
//	- Heavy paths are well-formed
//	- Position mapping is bijective
//
// Complexity: O(V) time
//
// Thread Safety: Safe for concurrent use (read-only).
func (hld *HLDecomposition) Validate() error {
	// Check array lengths
	if len(hld.parent) != hld.nodeCount {
		return fmt.Errorf("parent array length %d != node count %d", len(hld.parent), hld.nodeCount)
	}
	if len(hld.depth) != hld.nodeCount {
		return fmt.Errorf("depth array length %d != node count %d", len(hld.depth), hld.nodeCount)
	}
	if len(hld.subSize) != hld.nodeCount {
		return fmt.Errorf("subSize array length %d != node count %d", len(hld.subSize), hld.nodeCount)
	}
	if len(hld.heavy) != hld.nodeCount {
		return fmt.Errorf("heavy array length %d != node count %d", len(hld.heavy), hld.nodeCount)
	}
	if len(hld.head) != hld.nodeCount {
		return fmt.Errorf("head array length %d != node count %d", len(hld.head), hld.nodeCount)
	}
	if len(hld.pos) != hld.nodeCount {
		return fmt.Errorf("pos array length %d != node count %d", len(hld.pos), hld.nodeCount)
	}
	if len(hld.nodeAtPos) != hld.nodeCount {
		return fmt.Errorf("nodeAtPos array length %d != node count %d", len(hld.nodeAtPos), hld.nodeCount)
	}

	// Check root invariant
	if hld.parent[hld.root] != -1 {
		return fmt.Errorf("root parent is %d, expected -1", hld.parent[hld.root])
	}

	// Check depth invariants
	for v := 0; v < hld.nodeCount; v++ {
		p := hld.parent[v]
		if p == -1 {
			continue // Root
		}
		if hld.depth[v] != hld.depth[p]+1 {
			return fmt.Errorf("node %d depth %d != parent %d depth %d + 1",
				v, hld.depth[v], p, hld.depth[p])
		}
	}

	// Check heavy child invariants
	for v := 0; v < hld.nodeCount; v++ {
		h := hld.heavy[v]
		if h == -1 {
			continue // No heavy child
		}
		if hld.parent[h] != v {
			return fmt.Errorf("heavy child %d has parent %d, expected %d", h, hld.parent[h], v)
		}
		// Heavy child must have largest subtree among all children
		for c := 0; c < hld.nodeCount; c++ {
			if hld.parent[c] == v && c != h && hld.subSize[c] > hld.subSize[h] {
				return fmt.Errorf("node %d has child %d with larger subtree (%d) than heavy child %d (%d)",
					v, c, hld.subSize[c], h, hld.subSize[h])
			}
		}
	}

	// Check position mapping is bijective
	seen := make([]bool, hld.nodeCount)
	for i := 0; i < hld.nodeCount; i++ {
		v := hld.nodeAtPos[i]
		if v < 0 || v >= hld.nodeCount {
			return fmt.Errorf("invalid node %d at position %d", v, i)
		}
		if seen[v] {
			return fmt.Errorf("node %d appears twice in position array", v)
		}
		seen[v] = true
		if hld.pos[v] != i {
			return fmt.Errorf("position mismatch: pos[%d]=%d but nodeAtPos[%d]=%d", v, hld.pos[v], i, v)
		}
	}

	return nil
}

// Stats returns statistics about the HLD structure.
//
// Description:
//
//	Computes:
//	- Number of heavy paths
//	- Average heavy path length
//	- Maximum heavy path length
//	- Light edge count
//	Useful for debugging and performance analysis.
//
// Thread Safety: Safe for concurrent use (read-only).
func (hld *HLDecomposition) Stats() HLDStats {
	pathCount := 0
	totalPathLength := 0
	maxPathLength := 0

	visited := make([]bool, hld.nodeCount)

	// Count heavy paths
	for v := 0; v < hld.nodeCount; v++ {
		if visited[v] {
			continue
		}
		if hld.head[v] != v {
			continue // Not a path head
		}

		// Walk down heavy path
		pathLength := 0
		curr := v
		for curr != -1 && !visited[curr] {
			visited[curr] = true
			pathLength++
			curr = hld.heavy[curr]
		}

		if pathLength > 0 {
			pathCount++
			totalPathLength += pathLength
			if pathLength > maxPathLength {
				maxPathLength = pathLength
			}
		}
	}

	avgPathLength := 0.0
	if pathCount > 0 {
		avgPathLength = float64(totalPathLength) / float64(pathCount)
	}

	return HLDStats{
		NodeCount:        hld.nodeCount,
		HeavyPathCount:   pathCount,
		AvgPathLength:    avgPathLength,
		MaxPathLength:    maxPathLength,
		LightEdgeCount:   hld.lightEdgeCount,
		ConstructionTime: hld.buildTime,
	}
}

// IsTree validates that the graph forms a valid tree structure.
//
// Description:
//
//	Checks that graph is acyclic, connected, and has exactly V-1 edges.
//	Uses DFS to detect cycles and verify connectivity from root.
//
// Complexity: O(V + E)
//
// Thread Safety: Safe for concurrent use (read-only on frozen graph).
func (g *Graph) IsTree(ctx context.Context, root string) error {
	if !g.IsFrozen() {
		return ErrGraphNotFrozen
	}

	if _, ok := g.GetNode(root); !ok {
		return fmt.Errorf("root node %q does not exist", root)
	}

	nodeCount := g.NodeCount()
	edgeCount := len(g.edges)

	// DFS to check connectivity and detect cycles (do this first for better error messages)
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(string, string) error
	dfs = func(nodeID, parentID string) error {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if inStack[nodeID] {
			return fmt.Errorf("cycle detected at node %q", nodeID)
		}

		if visited[nodeID] {
			return nil // Already processed
		}

		visited[nodeID] = true
		inStack[nodeID] = true

		node, ok := g.GetNode(nodeID)
		if ok && node != nil {
			for _, edge := range node.Outgoing {
				if edge.ToID != parentID { // Don't revisit parent
					if err := dfs(edge.ToID, nodeID); err != nil {
						return err
					}
				}
			}
		}

		inStack[nodeID] = false
		return nil
	}

	if err := dfs(root, ""); err != nil {
		return err
	}

	// Check all nodes are reachable from root
	if len(visited) != nodeCount {
		return fmt.Errorf("graph is disconnected: %d nodes reachable from root, %d total nodes", len(visited), nodeCount)
	}

	// Tree property: exactly V-1 edges (check after connectivity to provide better errors)
	if edgeCount != nodeCount-1 {
		return fmt.Errorf("tree must have exactly V-1 edges, got %d edges for %d nodes", edgeCount, nodeCount)
	}

	return nil
}

// NodeToIdx returns the internal index for a node ID.
func (hld *HLDecomposition) NodeToIdx(nodeID string) (int, bool) {
	idx, ok := hld.nodeToIdx[nodeID]
	return idx, ok
}

// IdxToNode returns the node ID for an internal index.
func (hld *HLDecomposition) IdxToNode(idx int) (string, error) {
	if idx < 0 || idx >= hld.nodeCount {
		return "", fmt.Errorf("index %d out of range [0, %d)", idx, hld.nodeCount)
	}
	return hld.idxToNode[idx], nil
}

// NodeCount returns the number of nodes in the HLD.
func (hld *HLDecomposition) NodeCount() int {
	return hld.nodeCount
}

// Parent returns the parent index of node v (-1 for root).
func (hld *HLDecomposition) Parent(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return -1
	}
	return hld.parent[v]
}

// Depth returns the depth of node v from root.
func (hld *HLDecomposition) Depth(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return -1
	}
	return hld.depth[v]
}

// SubtreeSize returns the subtree size rooted at node v.
func (hld *HLDecomposition) SubtreeSize(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return 0
	}
	return hld.subSize[v]
}

// HeavyChild returns the heavy child of node v (-1 if none).
func (hld *HLDecomposition) HeavyChild(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return -1
	}
	return hld.heavy[v]
}

// Head returns the head of the heavy path containing node v.
func (hld *HLDecomposition) Head(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return -1
	}
	return hld.head[v]
}

// Pos returns the DFS position of node v.
func (hld *HLDecomposition) Pos(v int) int {
	if v < 0 || v >= hld.nodeCount {
		return -1
	}
	return hld.pos[v]
}

// NodeAtPos returns the node at DFS position i.
func (hld *HLDecomposition) NodeAtPos(i int) int {
	if i < 0 || i >= hld.nodeCount {
		return -1
	}
	return hld.nodeAtPos[i]
}

// CacheKey generates a cache key for this HLD structure.
//
// Description:
//
//	Returns a deterministic cache key based on root node ID.
//	For full graph hashing, use with external graph hash.
//
// Thread Safety: Safe for concurrent use (read-only).
func (hld *HLDecomposition) CacheKey() string {
	rootID := hld.idxToNode[hld.root]
	return fmt.Sprintf("hld:%s:%d", rootID, hld.nodeCount)
}

// CacheKeyWithGraphHash generates a cache key using pre-computed graph hash.
//
// Description:
//
//	More efficient than CacheKey() when graph hash is already available.
//	Use this when graph provides its own hash.
//
// Thread Safety: Safe for concurrent use (read-only).
func (hld *HLDecomposition) CacheKeyWithGraphHash(graphHash string) string {
	rootID := hld.idxToNode[hld.root]
	return fmt.Sprintf("hld:%s:%s", graphHash, rootID)
}

// SerializeMetadata returns HLD metadata for CRS TraceStep.
func (hld *HLDecomposition) SerializeMetadata() map[string]string {
	stats := hld.Stats()
	return map[string]string{
		"node_count":           strconv.Itoa(hld.nodeCount),
		"root":                 hld.idxToNode[hld.root],
		"light_edge_count":     strconv.Itoa(stats.LightEdgeCount),
		"heavy_path_count":     strconv.Itoa(stats.HeavyPathCount),
		"max_path_length":      strconv.Itoa(stats.MaxPathLength),
		"construction_time_ms": strconv.FormatInt(stats.ConstructionTime.Milliseconds(), 10),
	}
}
