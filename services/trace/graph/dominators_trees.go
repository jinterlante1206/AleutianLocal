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
	"log/slog"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// Dominator Trees (GR-16b) - Cooper-Harvey-Kennedy Algorithm
// =============================================================================

var dominatorTracer = otel.Tracer("graph.dominators")

// dominatorContextCheckInterval is how often to check for context cancellation.
const dominatorContextCheckInterval = 100

// Default configuration for dominator algorithm.
const (
	// DefaultMaxDominatorIterations caps convergence iterations.
	DefaultMaxDominatorIterations = 100
)

// DominatorTree represents the computed dominator relationships.
//
// A dominator tree captures the dominance relationship where node D dominates
// node N if every path from the entry node to N must go through D.
//
// Thread Safety: Safe for concurrent use after construction.
type DominatorTree struct {
	// Entry is the entry node ID (root of dominator tree).
	Entry string

	// ImmediateDom maps nodeID → immediate dominator nodeID.
	// The immediate dominator of a node N is the unique node D that:
	// 1. Strictly dominates N (D != N)
	// 2. Does not strictly dominate any other dominator of N
	// Entry node maps to itself.
	ImmediateDom map[string]string

	// Children maps nodeID → nodes it immediately dominates.
	// This is the inverse of ImmediateDom for efficient subtree queries.
	// Lazily computed on first access via ensureChildrenBuilt.
	Children map[string][]string

	// childrenOnce ensures Children map is built exactly once, thread-safely.
	childrenOnce sync.Once

	// Depth maps nodeID → depth in dominator tree.
	// Entry node has depth 0.
	Depth map[string]int

	// PostOrder contains nodes in reverse postorder for iteration.
	// Used internally during computation.
	PostOrder []string

	// postOrderIndex maps nodeID → index in PostOrder for O(1) lookup.
	postOrderIndex map[string]int

	// Iterations is the number of convergence iterations.
	Iterations int

	// Converged indicates whether the algorithm converged before max iterations.
	Converged bool

	// NodeCount is the total nodes in the graph.
	NodeCount int

	// EdgeCount is the total edges in the graph.
	EdgeCount int

	// ReachableCount is the number of nodes reachable from entry.
	ReachableCount int

	// ExitCount is the number of exit nodes detected (for post-dominators).
	// 0 for dominator trees, 1+ for post-dominator trees.
	ExitCount int

	// UsedVirtualExit indicates whether a virtual exit node was used
	// to handle multiple exits (for post-dominators only).
	UsedVirtualExit bool
}

// DominatorError represents an error in dominator computation.
type DominatorError struct {
	Message string
}

func (e *DominatorError) Error() string {
	return e.Message
}

// Dominators computes the dominator tree using Cooper-Harvey-Kennedy algorithm.
//
// Description:
//
//	Uses the iterative data-flow approach from "A Simple, Fast Dominance Algorithm"
//	by Keith D. Cooper, Timothy J. Harvey, and Ken Kennedy (2001).
//	This algorithm converges in O(E) time for typical reducible graphs
//	and O(V²) worst case for non-reducible graphs.
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil. Checked every iteration.
//   - entry: The entry node ID. Must exist in graph and not be empty.
//
// Outputs:
//
//   - *DominatorTree: The computed dominator tree. Never nil.
//   - error: Non-nil if entry not found or context cancelled.
//
// Example:
//
//	dt, err := analytics.Dominators(ctx, "main.go:10:main")
//	if err != nil {
//	    log.Fatalf("failed to compute dominators: %v", err)
//	}
//	for node, idom := range dt.ImmediateDom {
//	    log.Printf("%s is immediately dominated by %s", node, idom)
//	}
//
// Limitations:
//
//   - Only computes dominators for nodes reachable from entry
//   - Unreachable nodes have no entry in ImmediateDom
//   - Memory usage: O(V) for all data structures
//
// Assumptions:
//
//   - Graph is frozen (guaranteed by HierarchicalGraph construction)
//   - Entry node exists in the graph
//   - Directed edges represent call relationships
//
// Thread Safety: Safe for concurrent use (read-only on graph).
//
// Complexity: O(E) typical, O(V²) worst case.
func (a *GraphAnalytics) Dominators(ctx context.Context, entry string) (*DominatorTree, error) {
	// Initialize result with empty maps
	result := &DominatorTree{
		Entry:          entry,
		ImmediateDom:   make(map[string]string),
		Depth:          make(map[string]int),
		PostOrder:      make([]string, 0),
		postOrderIndex: make(map[string]int),
	}

	// Nil graph check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: nil analytics or graph, returning empty result")
		return result, nil
	}

	// Empty entry check
	if entry == "" {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: empty entry node ID")
		return result, &DominatorError{Message: "entry node ID must not be empty"}
	}

	// Track start time for duration logging
	startTime := time.Now()

	// Start OTel span
	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	ctx, span := dominatorTracer.Start(ctx, "GraphAnalytics.Dominators",
		trace.WithAttributes(
			attribute.String("entry", entry),
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
		),
	)
	defer span.End()

	result.NodeCount = nodeCount
	result.EdgeCount = edgeCount

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		return result, ctx.Err()
	}

	// Empty graph check
	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: empty graph, returning empty result")
		return result, nil
	}

	// Verify entry node exists
	if _, ok := a.graph.GetNode(entry); !ok {
		span.AddEvent("entry_not_found")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: entry node not found", slog.String("entry", entry))
		return result, &DominatorError{Message: "entry node not found: " + entry}
	}

	// Compute reverse postorder via DFS
	postOrder, postOrderIndex := a.computeReversePostorder(ctx, entry)
	if len(postOrder) == 0 {
		span.AddEvent("no_reachable_nodes")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: no nodes reachable from entry")
		return result, nil
	}

	result.PostOrder = postOrder
	result.postOrderIndex = postOrderIndex
	result.ReachableCount = len(postOrder)

	span.AddEvent("postorder_complete", trace.WithAttributes(
		attribute.Int("reachable_nodes", len(postOrder)),
	))

	// Pre-allocate maps
	result.ImmediateDom = make(map[string]string, len(postOrder))
	result.Depth = make(map[string]int, len(postOrder))

	// Initialize: idom[entry] = entry
	result.ImmediateDom[entry] = entry

	// Build predecessors map for efficient lookup
	predecessors := a.buildPredecessors(postOrder)

	// Iterative fixed-point algorithm
	changed := true
	iterations := 0
	maxIterations := DefaultMaxDominatorIterations

	for changed && iterations < maxIterations {
		// Check context each iteration
		if ctx != nil && ctx.Err() != nil {
			span.AddEvent("context_cancelled", trace.WithAttributes(
				attribute.Int("iteration", iterations),
			))
			result.Iterations = iterations
			return result, ctx.Err()
		}

		changed = false
		iterations++

		// Process nodes in reverse postorder (except entry)
		for i := len(postOrder) - 1; i >= 0; i-- {
			nodeID := postOrder[i]
			if nodeID == entry {
				continue
			}

			preds := predecessors[nodeID]
			if len(preds) == 0 {
				continue // Unreachable from entry
			}

			// Find first processed predecessor
			var newIdom string
			for _, pred := range preds {
				if _, ok := result.ImmediateDom[pred]; ok {
					newIdom = pred
					break
				}
			}

			if newIdom == "" {
				continue // No processed predecessor yet
			}

			// Intersect with other processed predecessors
			for _, pred := range preds {
				if pred == newIdom {
					continue
				}
				if _, ok := result.ImmediateDom[pred]; ok {
					newIdom = a.intersect(result, pred, newIdom)
				}
			}

			// Check if changed
			if oldIdom, ok := result.ImmediateDom[nodeID]; !ok || oldIdom != newIdom {
				result.ImmediateDom[nodeID] = newIdom
				changed = true
			}
		}

		// Add span event for iteration tracking (helps debug convergence issues)
		if iterations <= 10 || !changed {
			// Log first 10 iterations and final iteration
			span.AddEvent("iteration_complete", trace.WithAttributes(
				attribute.Int("iteration", iterations),
				attribute.Bool("changed", changed),
			))
		}

		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: iteration complete",
			slog.Int("iteration", iterations),
			slog.Bool("changed", changed),
		)
	}

	result.Iterations = iterations
	result.Converged = !changed

	if !result.Converged {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("dominators: did not converge",
			slog.Int("iterations", iterations),
			slog.Int("max_iterations", maxIterations),
		)
	}

	// Compute depths
	a.computeDominatorDepths(result)

	// Verify idom reference integrity (all idom values must exist as keys)
	if invalidRefs := validateIdomReferences(result); len(invalidRefs) > 0 {
		span.AddEvent("idom_reference_warnings", trace.WithAttributes(
			attribute.Int("invalid_count", len(invalidRefs)),
		))
		telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("dominators: invalid idom references detected",
			slog.Int("invalid_count", len(invalidRefs)),
		)
	}

	span.AddEvent("algorithm_complete", trace.WithAttributes(
		attribute.Int("iterations", iterations),
		attribute.Bool("converged", result.Converged),
		attribute.Int("reachable_nodes", len(result.ImmediateDom)),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("dominators: analysis complete",
		slog.String("entry", entry),
		slog.Int("iterations", iterations),
		slog.Bool("converged", result.Converged),
		slog.Int("reachable_nodes", len(result.ImmediateDom)),
		slog.Duration("duration", time.Since(startTime)),
	)

	return result, nil
}

// validateIdomReferences checks that all ImmediateDom values exist as keys.
//
// Description:
//
//	Verifies the integrity of the dominator tree by ensuring every
//	ImmediateDom value (except self-references for entry) points to
//	a node that exists in the tree.
//
// Inputs:
//
//   - dt: The dominator tree to validate. Must not be nil.
//
// Outputs:
//
//   - []string: List of nodes with invalid idom references (empty if valid).
//
// Thread Safety: Safe for concurrent use (read-only on dt).
func validateIdomReferences(dt *DominatorTree) []string {
	if dt == nil || len(dt.ImmediateDom) == 0 {
		return nil
	}

	invalidRefs := make([]string, 0)

	for node, idom := range dt.ImmediateDom {
		// Self-reference is valid for entry node
		if node == idom {
			continue
		}

		// Check that idom exists as a key
		if _, exists := dt.ImmediateDom[idom]; !exists {
			invalidRefs = append(invalidRefs, node)
		}
	}

	return invalidRefs
}

// computeReversePostorder computes reverse postorder via iterative DFS.
//
// Returns the nodes in reverse postorder and a map from nodeID to index.
// Only includes nodes reachable from the entry node.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) computeReversePostorder(ctx context.Context, entry string) ([]string, map[string]int) {
	visited := make(map[string]bool)
	postOrder := make([]string, 0, a.graph.NodeCount())

	// Iterative DFS using explicit stack
	type frame struct {
		nodeID    string
		childIdx  int
		processed bool
	}

	stack := []frame{{nodeID: entry}}
	iterations := 0

	for len(stack) > 0 {
		iterations++
		if iterations%dominatorContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				// Build partial index map from what we have so far
				indexMap := make(map[string]int, len(postOrder))
				for i, nodeID := range postOrder {
					indexMap[nodeID] = i
				}
				return postOrder, indexMap
			}
		}

		current := &stack[len(stack)-1]

		if !current.processed {
			if visited[current.nodeID] {
				stack = stack[:len(stack)-1]
				continue
			}
			visited[current.nodeID] = true
			current.processed = true
			current.childIdx = 0
		}

		// Get children (successors)
		node, ok := a.graph.GetNode(current.nodeID)
		if !ok {
			stack = stack[:len(stack)-1]
			continue
		}

		// Process next unvisited child
		foundUnvisited := false
		for current.childIdx < len(node.Outgoing) {
			childID := node.Outgoing[current.childIdx].ToID
			current.childIdx++

			if !visited[childID] {
				stack = append(stack, frame{nodeID: childID})
				foundUnvisited = true
				break
			}
		}

		if !foundUnvisited {
			// All children processed, add to postorder
			postOrder = append(postOrder, current.nodeID)
			stack = stack[:len(stack)-1]
		}
	}

	// Build index map for O(1) lookup
	indexMap := make(map[string]int, len(postOrder))
	for i, nodeID := range postOrder {
		indexMap[nodeID] = i
	}

	return postOrder, indexMap
}

// buildPredecessors builds a map from nodeID to its predecessors (callers).
//
// Only includes predecessors that are in the reachable set (postOrder).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildPredecessors(postOrder []string) map[string][]string {
	reachable := make(map[string]bool, len(postOrder))
	for _, nodeID := range postOrder {
		reachable[nodeID] = true
	}

	predecessors := make(map[string][]string, len(postOrder))

	for _, nodeID := range postOrder {
		node, ok := a.graph.GetNode(nodeID)
		if !ok {
			continue
		}

		// Predecessors are nodes with incoming edges
		for _, edge := range node.Incoming {
			fromID := edge.FromID
			if reachable[fromID] {
				predecessors[nodeID] = append(predecessors[nodeID], fromID)
			}
		}
	}

	return predecessors
}

// intersect finds the lowest common dominator using postorder indices.
//
// This is the core of the Cooper-Harvey-Kennedy algorithm.
// Uses the property that dominators with higher postorder indices
// are closer to the entry node.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) intersect(dt *DominatorTree, b1, b2 string) string {
	finger1 := b1
	finger2 := b2

	for finger1 != finger2 {
		for dt.postOrderIndex[finger1] < dt.postOrderIndex[finger2] {
			finger1 = dt.ImmediateDom[finger1]
		}
		for dt.postOrderIndex[finger2] < dt.postOrderIndex[finger1] {
			finger2 = dt.ImmediateDom[finger2]
		}
	}

	return finger1
}

// computeDominatorDepths computes the depth of each node in the dominator tree.
//
// Entry node has depth 0. Each node's depth is idom's depth + 1.
// For post-dominator trees with multiple exits, nodes with self-pointing idom
// (real exits after virtual exit filtering) are assigned depth 0 explicitly.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) computeDominatorDepths(dt *DominatorTree) {
	// Entry has depth 0
	dt.Depth[dt.Entry] = 0

	// Process in reverse postorder to ensure idom is processed first
	for i := len(dt.PostOrder) - 1; i >= 0; i-- {
		nodeID := dt.PostOrder[i]
		if nodeID == dt.Entry {
			continue
		}

		idom, ok := dt.ImmediateDom[nodeID]
		if !ok {
			continue
		}

		// Handle self-pointing idom explicitly (for post-dominator trees with
		// multiple exits where real exits have idom = self after virtual exit filtering)
		if idom == nodeID {
			dt.Depth[nodeID] = 0 // Root of subtree
			continue
		}

		// Depth is idom's depth + 1
		if idomDepth, ok := dt.Depth[idom]; ok {
			dt.Depth[nodeID] = idomDepth + 1
		}
	}
}

// Dominates returns true if a dominates b.
//
// A node dominates itself (reflexive).
// Returns false if either node is not in the dominator tree.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(depth) where depth is the depth of b in the dominator tree.
func (dt *DominatorTree) Dominates(a, b string) bool {
	// Every node dominates itself
	if a == b {
		return true
	}

	// Check if both nodes are in the tree
	if _, ok := dt.ImmediateDom[b]; !ok {
		return false
	}
	if _, ok := dt.ImmediateDom[a]; !ok && a != dt.Entry {
		return false
	}

	// Walk up from b to entry, looking for a
	current := b
	for current != dt.Entry {
		idom, ok := dt.ImmediateDom[current]
		if !ok {
			return false
		}
		if idom == a {
			return true
		}
		current = idom
	}

	// Only entry dominates all reachable nodes
	return a == dt.Entry
}

// DominatorsOf returns all dominators of a node (path from node to entry).
//
// Returns empty slice if node is not in the dominator tree.
// The result includes the node itself and ends with the entry node.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(depth) where depth is the depth of node.
func (dt *DominatorTree) DominatorsOf(node string) []string {
	if _, ok := dt.ImmediateDom[node]; !ok && node != dt.Entry {
		return []string{}
	}

	result := []string{node}
	current := node

	// Guard against infinite loops in case of data corruption.
	// Max iterations is bounded by the number of nodes in the tree.
	maxIterations := len(dt.ImmediateDom) + 1

	for i := 0; current != dt.Entry && i < maxIterations; i++ {
		idom, ok := dt.ImmediateDom[current]
		if !ok {
			break
		}
		if idom == current {
			// Entry node points to itself
			break
		}
		result = append(result, idom)
		current = idom
	}

	return result
}

// DominatedBy returns all nodes dominated by a node (subtree rooted at node).
//
// Builds the children map lazily on first call.
// Returns empty slice if node is not in the dominator tree.
//
// Thread Safety: Safe for concurrent use. Children map is built atomically.
//
// Complexity: O(subtree size) for traversal.
func (dt *DominatorTree) DominatedBy(node string) []string {
	// Ensure children map is built
	dt.ensureChildrenBuilt()

	if _, ok := dt.ImmediateDom[node]; !ok && node != dt.Entry {
		return []string{}
	}

	// BFS to collect subtree
	result := []string{node}
	queue := []string{node}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if children, ok := dt.Children[current]; ok {
			for _, child := range children {
				result = append(result, child)
				queue = append(queue, child)
			}
		}
	}

	return result
}

// ensureChildrenBuilt builds the Children map lazily using sync.Once.
//
// Thread Safety: Safe for concurrent use. Uses sync.Once to ensure
// Children map is built exactly once even under concurrent access.
func (dt *DominatorTree) ensureChildrenBuilt() {
	dt.childrenOnce.Do(func() {
		dt.Children = make(map[string][]string, len(dt.ImmediateDom))

		for node, idom := range dt.ImmediateDom {
			if node == idom {
				continue // Entry points to itself
			}
			dt.Children[idom] = append(dt.Children[idom], node)
		}
	})
}

// MaxDepth returns the maximum depth in the dominator tree.
//
// Thread Safety: Safe for concurrent use.
func (dt *DominatorTree) MaxDepth() int {
	maxDepth := 0
	for _, d := range dt.Depth {
		if d > maxDepth {
			maxDepth = d
		}
	}
	return maxDepth
}

// =============================================================================
// Lowest Common Dominator (GR-16g)
// =============================================================================

var lcdTracer = otel.Tracer("graph.lcd")

// LowestCommonDominator finds the LCD of two nodes.
//
// Description:
//
//	Finds the deepest node in the dominator tree that dominates both
//	input nodes. Uses an O(depth) algorithm that equalizes depths and
//	walks up until the nodes meet.
//
// Inputs:
//
//   - a, b: Node IDs to find common dominator for. Can be the same node.
//
// Outputs:
//
//   - string: The LCD node ID. Returns:
//   - The node itself if a == b
//   - The entry node if either node is not in the tree
//   - Empty string if receiver is nil
//
// Example:
//
//	lcd := domTree.LowestCommonDominator("saveDB", "sendEmail")
//	// Returns "validate" (their common mandatory dependency)
//
// Limitations:
//
//   - O(depth) per query; use PrepareLCAQueries for many queries
//   - Nodes not in tree are treated as having entry as LCD
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(depth) per query.
func (dt *DominatorTree) LowestCommonDominator(a, b string) string {
	// Nil receiver check
	if dt == nil {
		return ""
	}

	// Same node optimization
	if a == b {
		// Verify node exists, otherwise return entry
		if _, ok := dt.ImmediateDom[a]; ok {
			return a
		}
		if a == dt.Entry {
			return a
		}
		return dt.Entry
	}

	// Check if nodes are in tree
	_, aInTree := dt.ImmediateDom[a]
	_, bInTree := dt.ImmediateDom[b]

	// Handle missing nodes - return entry as fallback
	if !aInTree && a != dt.Entry {
		return dt.Entry
	}
	if !bInTree && b != dt.Entry {
		return dt.Entry
	}

	// Get depths with fallback to 0
	depthA := dt.Depth[a]
	depthB := dt.Depth[b]

	// Guard against infinite loops
	maxIterations := len(dt.ImmediateDom) + 1

	// Equalize depths: move deeper node up
	for i := 0; depthA > depthB && i < maxIterations; i++ {
		idom, ok := dt.ImmediateDom[a]
		if !ok || idom == a {
			break // Reached entry or cycle
		}
		a = idom
		depthA--
	}

	for i := 0; depthB > depthA && i < maxIterations; i++ {
		idom, ok := dt.ImmediateDom[b]
		if !ok || idom == b {
			break // Reached entry or cycle
		}
		b = idom
		depthB--
	}

	// Move both up until they meet
	for i := 0; a != b && i < maxIterations; i++ {
		idomA, okA := dt.ImmediateDom[a]
		idomB, okB := dt.ImmediateDom[b]

		if !okA || idomA == a {
			return a // a is at entry
		}
		if !okB || idomB == b {
			return b // b is at entry
		}

		a = idomA
		b = idomB
	}

	return a
}

// LowestCommonDominatorMultiple finds LCD of multiple nodes.
//
// Description:
//
//	Iteratively computes LCD for a list of nodes using the property
//	that LCD is associative: LCD(a, b, c) = LCD(LCD(a, b), c).
//
// Inputs:
//
//   - nodes: Slice of node IDs. Can be empty or have duplicates.
//
// Outputs:
//
//   - string: The LCD of all nodes. Returns:
//   - Entry node if slice is empty
//   - The single node if slice has one element
//   - Empty string if receiver is nil
//
// Example:
//
//	lcd := domTree.LowestCommonDominatorMultiple([]string{"saveDB", "sendEmail", "logAction"})
//	// Returns their common mandatory dependency
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(k × depth) where k = len(nodes).
func (dt *DominatorTree) LowestCommonDominatorMultiple(nodes []string) string {
	// Nil receiver check
	if dt == nil {
		return ""
	}

	// Empty slice returns entry
	if len(nodes) == 0 {
		return dt.Entry
	}

	// Single node returns itself (after validation)
	if len(nodes) == 1 {
		if _, ok := dt.ImmediateDom[nodes[0]]; ok {
			return nodes[0]
		}
		if nodes[0] == dt.Entry {
			return nodes[0]
		}
		return dt.Entry
	}

	// Iterative LCD: LCD(a, b, c) = LCD(LCD(a, b), c)
	result := nodes[0]
	for i := 1; i < len(nodes); i++ {
		result = dt.LowestCommonDominator(result, nodes[i])
		// Early termination: if we've reached entry, we can't go higher
		if result == dt.Entry {
			return result
		}
	}

	return result
}

// LCAQueryEngine provides O(1) LCD queries after O(V log V) preprocessing.
//
// Description:
//
//	Uses binary lifting to precompute 2^k-th ancestors for each node.
//	This enables O(log depth) queries instead of O(depth), which is
//	effectively O(1) for practical tree depths.
//
// Thread Safety: Safe for concurrent use after construction.
type LCAQueryEngine struct {
	domTree     *DominatorTree
	depth       map[string]int
	parent      map[string][]string // parent[v][k] = 2^k-th ancestor of v
	maxLogDepth int
}

// PrepareLCAQueries preprocesses for O(1) LCD queries.
//
// Description:
//
//	Uses binary lifting to enable O(log depth) LCD queries. The preprocessing
//	builds a sparse table where parent[v][k] = 2^k-th ancestor of v.
//
// Outputs:
//
//   - *LCAQueryEngine: Engine for fast LCD queries. Returns nil if
//     receiver is nil.
//
// Example:
//
//	lca := domTree.PrepareLCAQueries()
//	lcd := lca.Query("saveDB", "sendEmail")  // O(log depth) instead of O(depth)
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(V log V) time and space for preprocessing.
func (dt *DominatorTree) PrepareLCAQueries() *LCAQueryEngine {
	// Nil receiver check
	if dt == nil {
		return nil
	}

	// Find maximum depth
	maxDepth := 0
	for _, d := range dt.Depth {
		if d > maxDepth {
			maxDepth = d
		}
	}

	// Calculate log2(maxDepth) + 1
	maxLogDepth := 1
	for (1 << maxLogDepth) <= maxDepth {
		maxLogDepth++
	}

	// Edge case: empty or very shallow tree
	if maxLogDepth == 0 {
		maxLogDepth = 1
	}

	// Build binary lifting table
	parent := make(map[string][]string, len(dt.ImmediateDom))

	// Initialize level 0 (direct parents)
	for node, idom := range dt.ImmediateDom {
		parent[node] = make([]string, maxLogDepth)
		parent[node][0] = idom
	}

	// Build higher levels: parent[v][k] = parent[parent[v][k-1]][k-1]
	for k := 1; k < maxLogDepth; k++ {
		for node := range parent {
			if p := parent[node][k-1]; p != "" {
				if pp, ok := parent[p]; ok && len(pp) > k-1 {
					parent[node][k] = pp[k-1]
				}
			}
		}
	}

	return &LCAQueryEngine{
		domTree:     dt,
		depth:       dt.Depth,
		parent:      parent,
		maxLogDepth: maxLogDepth,
	}
}

// Query returns LCD in O(log depth) time after preprocessing.
//
// Description:
//
//	Uses the precomputed binary lifting table to find the LCD
//	efficiently. First lifts the deeper node to the same depth,
//	then binary searches for the LCD.
//
// Inputs:
//
//   - a, b: Node IDs to query.
//
// Outputs:
//
//   - string: The LCD node ID.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(log depth) per query.
func (lca *LCAQueryEngine) Query(a, b string) string {
	if lca == nil || lca.domTree == nil {
		return ""
	}

	// Same node optimization
	if a == b {
		if _, ok := lca.parent[a]; ok {
			return a
		}
		return lca.domTree.Entry
	}

	// Check if nodes exist
	_, aExists := lca.parent[a]
	_, bExists := lca.parent[b]

	if !aExists && a != lca.domTree.Entry {
		return lca.domTree.Entry
	}
	if !bExists && b != lca.domTree.Entry {
		return lca.domTree.Entry
	}

	// Get depths
	depthA := lca.depth[a]
	depthB := lca.depth[b]

	// Ensure a is deeper (or equal)
	if depthA < depthB {
		a, b = b, a
		depthA, depthB = depthB, depthA
	}

	// Lift a to same depth as b using binary lifting
	diff := depthA - depthB
	for k := 0; diff > 0 && k < lca.maxLogDepth; k++ {
		if diff&1 == 1 {
			if lca.parent[a] != nil && len(lca.parent[a]) > k {
				if p := lca.parent[a][k]; p != "" {
					a = p
				}
			}
		}
		diff >>= 1
	}

	// If they're the same now, that's the LCD
	if a == b {
		return a
	}

	// Binary search for LCD
	for k := lca.maxLogDepth - 1; k >= 0; k-- {
		if k >= len(lca.parent[a]) || k >= len(lca.parent[b]) {
			continue
		}
		parentA := lca.parent[a][k]
		parentB := lca.parent[b][k]

		if parentA != parentB {
			a = parentA
			b = parentB
		}
	}

	// Return parent of current position (which is the LCD)
	if lca.parent[a] != nil && len(lca.parent[a]) > 0 && lca.parent[a][0] != "" {
		return lca.parent[a][0]
	}

	return lca.domTree.Entry
}

// LCDWithCRS wraps LCD query with CRS tracing.
//
// Description:
//
//	Provides the same functionality as LowestCommonDominatorMultiple but also
//	returns a TraceStep for recording in the Code Reasoning State (CRS).
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil.
//   - domTree: Pre-computed dominator tree. Must not be nil.
//   - nodes: Slice of node IDs to find common dominator for.
//
// Outputs:
//
//   - string: The LCD of all nodes.
//   - TraceStep: Trace step for CRS recording.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) LCDWithCRS(
	ctx context.Context,
	domTree *DominatorTree,
	nodes []string,
) (string, crs.TraceStep) {
	start := time.Now()

	// Nil domTree check
	if domTree == nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_lcd").
			WithTarget("").
			WithTool("LowestCommonDominator").
			WithDuration(time.Since(start)).
			WithError("dominator tree must not be nil").
			Build()
		return "", step
	}

	// Start OTel span
	ctx, span := lcdTracer.Start(ctx, "GraphAnalytics.LCDWithCRS",
		trace.WithAttributes(
			attribute.String("entry", domTree.Entry),
			attribute.Int("nodes_queried", len(nodes)),
		),
	)
	defer span.End()

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_lcd").
			WithTarget(domTree.Entry).
			WithTool("LowestCommonDominator").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return "", step
	}

	// Compute LCD
	var result string
	if len(nodes) == 2 {
		result = domTree.LowestCommonDominator(nodes[0], nodes[1])
	} else {
		result = domTree.LowestCommonDominatorMultiple(nodes)
	}

	span.AddEvent("lcd_computed", trace.WithAttributes(
		attribute.String("result", result),
	))

	// Build TraceStep
	step := crs.NewTraceStepBuilder().
		WithAction("analytics_lcd").
		WithTarget(domTree.Entry).
		WithTool("LowestCommonDominator").
		WithDuration(time.Since(start)).
		WithMetadata("nodes_queried", itoa(len(nodes))).
		WithMetadata("lcd_result", result).
		WithMetadata("entry", domTree.Entry).
		Build()

	return result, step
}

// DominatorsWithCRS wraps Dominators with CRS tracing.
//
// Description:
//
//	Provides the same functionality as Dominators but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DominatorsWithCRS(ctx context.Context, entry string) (*DominatorTree, crs.TraceStep) {
	start := time.Now()

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_dominators").
			WithTarget(entry).
			WithTool("Dominators").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &DominatorTree{
			Entry:        entry,
			ImmediateDom: map[string]string{},
			Depth:        map[string]int{},
		}, step
	}

	result, err := a.Dominators(ctx, entry)

	// Build TraceStep
	builder := crs.NewTraceStepBuilder().
		WithAction("analytics_dominators").
		WithTarget(entry).
		WithTool("Dominators").
		WithDuration(time.Since(start))

	if err != nil {
		builder = builder.WithError(err.Error())
	} else {
		maxDepth := result.MaxDepth()
		builder = builder.
			WithMetadata("entry", entry).
			WithMetadata("iterations", itoa(result.Iterations)).
			WithMetadata("converged", btoa(result.Converged)).
			WithMetadata("max_depth", itoa(maxDepth)).
			WithMetadata("reachable_nodes", itoa(result.ReachableCount)).
			WithMetadata("node_count", itoa(result.NodeCount))
	}

	return result, builder.Build()
}

// =============================================================================
// Post-Dominator Trees (GR-16c)
// =============================================================================

// VirtualExitNodeID is the sentinel ID for the virtual exit node used when
// handling graphs with multiple exits. It is filtered from results.
const VirtualExitNodeID = "__virtual_exit__"

var postDominatorTracer = otel.Tracer("graph.post_dominators")

// PostDominators computes the post-dominator tree using Cooper-Harvey-Kennedy algorithm.
//
// Description:
//
//	Computes post-dominators by running the dominator algorithm on the reversed
//	graph. A node A post-dominates B if every path from B to exit must go through A.
//	This is the dual of dominators: dominators answer "what must happen before X"
//	while post-dominators answer "what must happen after X".
//
//	If exit is empty, auto-detects exit nodes (nodes with no outgoing edges).
//	If multiple exits are detected, creates a virtual exit node internally.
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil. Checked every iteration.
//   - exit: The exit node ID. If empty, auto-detects exits.
//
// Outputs:
//
//   - *DominatorTree: The computed post-dominator tree. Never nil.
//     The Entry field contains the exit node (or virtual exit ID).
//   - error: Non-nil if exit not found or context cancelled.
//
// Example:
//
//	dt, err := analytics.PostDominators(ctx, "cleanup")
//	if err != nil {
//	    log.Fatalf("failed to compute post-dominators: %v", err)
//	}
//	// dt.ImmediateDom maps each node to its immediate post-dominator
//	for node, ipostdom := range dt.ImmediateDom {
//	    log.Printf("%s is immediately post-dominated by %s", node, ipostdom)
//	}
//
// Limitations:
//
//   - Only computes post-dominators for nodes that can reach exit
//   - Nodes that cannot reach exit have no entry in ImmediateDom
//   - Memory usage: O(V) for all data structures
//
// Assumptions:
//
//   - Graph is frozen (guaranteed by HierarchicalGraph construction)
//   - Exit node exists in the graph (if specified)
//   - Directed edges represent call relationships
//
// Thread Safety: Safe for concurrent use (read-only on graph).
//
// Complexity: O(E) typical, O(V²) worst case.
func (a *GraphAnalytics) PostDominators(ctx context.Context, exit string) (*DominatorTree, error) {
	// Initialize result with empty maps
	result := &DominatorTree{
		ImmediateDom:   make(map[string]string),
		Depth:          make(map[string]int),
		PostOrder:      make([]string, 0),
		postOrderIndex: make(map[string]int),
	}

	// Nil graph check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: nil analytics or graph, returning empty result")
		return result, nil
	}

	// Track start time for duration logging
	startTime := time.Now()

	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	// Start OTel span
	ctx, span := postDominatorTracer.Start(ctx, "GraphAnalytics.PostDominators",
		trace.WithAttributes(
			attribute.String("exit", exit),
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
		),
	)
	defer span.End()

	result.NodeCount = nodeCount
	result.EdgeCount = edgeCount

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		return result, ctx.Err()
	}

	// Empty graph check
	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: empty graph, returning empty result")
		return result, nil
	}

	// Auto-detect exits if not specified
	var exits []string
	var usedVirtualExit bool

	if exit == "" {
		exits = a.detectExitNodes()
		span.AddEvent("exit_detection", trace.WithAttributes(
			attribute.Int("exits_found", len(exits)),
		))
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: auto-detected exits",
			slog.Int("count", len(exits)),
		)

		if len(exits) == 0 {
			span.AddEvent("no_exits_found")
			telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: no exit nodes found (all nodes have outgoing edges)")
			return result, &DominatorError{Message: "no exit nodes found (all nodes have outgoing edges)"}
		}

		if len(exits) == 1 {
			exit = exits[0]
		} else {
			// Multiple exits - use virtual exit
			exit = VirtualExitNodeID
			usedVirtualExit = true
			span.AddEvent("virtual_exit_created", trace.WithAttributes(
				attribute.Int("real_exits", len(exits)),
			))
		}
	} else {
		// Verify specified exit exists
		if _, ok := a.graph.GetNode(exit); !ok {
			span.AddEvent("exit_not_found")
			telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: exit node not found", slog.String("exit", exit))
			return result, &DominatorError{Message: "exit node not found: " + exit}
		}
		exits = []string{exit}
	}

	result.Entry = exit
	result.ExitCount = len(exits)
	result.UsedVirtualExit = usedVirtualExit

	// Compute reverse postorder on reversed graph via DFS
	postOrder, postOrderIndex := a.computeReversePostorderReversed(ctx, exit, exits, usedVirtualExit)
	if len(postOrder) == 0 {
		span.AddEvent("no_reachable_nodes")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: no nodes reachable from exit (in reversed graph)")
		return result, nil
	}

	result.PostOrder = postOrder
	result.postOrderIndex = postOrderIndex
	result.ReachableCount = len(postOrder)

	span.AddEvent("postorder_complete", trace.WithAttributes(
		attribute.Int("reachable_nodes", len(postOrder)),
	))

	// Pre-allocate maps
	result.ImmediateDom = make(map[string]string, len(postOrder))
	result.Depth = make(map[string]int, len(postOrder))

	// Initialize: idom[exit] = exit
	result.ImmediateDom[exit] = exit

	// Build predecessors map on reversed graph (successors become predecessors)
	predecessors := a.buildReversedPredecessors(ctx, postOrder, exits, usedVirtualExit)

	// Iterative fixed-point algorithm (same as Dominators but on reversed graph)
	changed := true
	iterations := 0
	maxIterations := DefaultMaxDominatorIterations

	for changed && iterations < maxIterations {
		// Check context each iteration
		if ctx != nil && ctx.Err() != nil {
			span.AddEvent("context_cancelled", trace.WithAttributes(
				attribute.Int("iteration", iterations),
			))
			result.Iterations = iterations
			return result, ctx.Err()
		}

		changed = false
		iterations++

		// Process nodes in reverse postorder (except exit)
		for i := len(postOrder) - 1; i >= 0; i-- {
			nodeID := postOrder[i]
			if nodeID == exit {
				continue
			}

			preds := predecessors[nodeID]
			if len(preds) == 0 {
				continue
			}

			// Find first processed predecessor
			var newIdom string
			for _, pred := range preds {
				if _, ok := result.ImmediateDom[pred]; ok {
					newIdom = pred
					break
				}
			}

			if newIdom == "" {
				continue
			}

			// Intersect with other processed predecessors
			for _, pred := range preds {
				if pred == newIdom {
					continue
				}
				if _, ok := result.ImmediateDom[pred]; ok {
					newIdom = a.intersect(result, pred, newIdom)
				}
			}

			// Check if changed
			if oldIdom, ok := result.ImmediateDom[nodeID]; !ok || oldIdom != newIdom {
				result.ImmediateDom[nodeID] = newIdom
				changed = true
			}
		}

		// Add span event for iteration tracking
		if iterations <= 10 || !changed {
			span.AddEvent("iteration_complete", trace.WithAttributes(
				attribute.Int("iteration", iterations),
				attribute.Bool("changed", changed),
			))
		}

		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: iteration complete",
			slog.Int("iteration", iterations),
			slog.Bool("changed", changed),
		)
	}

	result.Iterations = iterations
	result.Converged = !changed

	if !result.Converged {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("post_dominators: did not converge",
			slog.Int("iterations", iterations),
			slog.Int("max_iterations", maxIterations),
		)
	}

	// Filter virtual exit from results
	if usedVirtualExit {
		a.filterVirtualExit(result, exits)
	}

	// Compute depths
	a.computeDominatorDepths(result)

	// Verify idom reference integrity (all idom values must exist as keys)
	// Note: For post-dominators with multiple exits, self-references are
	// expected for each real exit node after virtual exit filtering
	if invalidRefs := validateIdomReferences(result); len(invalidRefs) > 0 {
		span.AddEvent("idom_reference_warnings", trace.WithAttributes(
			attribute.Int("invalid_count", len(invalidRefs)),
		))
		telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("post_dominators: invalid idom references detected",
			slog.Int("invalid_count", len(invalidRefs)),
		)
	}

	span.AddEvent("algorithm_complete", trace.WithAttributes(
		attribute.Int("iterations", iterations),
		attribute.Bool("converged", result.Converged),
		attribute.Int("reachable_nodes", len(result.ImmediateDom)),
		attribute.Bool("used_virtual_exit", usedVirtualExit),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("post_dominators: analysis complete",
		slog.String("exit", exit),
		slog.Int("iterations", iterations),
		slog.Bool("converged", result.Converged),
		slog.Int("reachable_nodes", len(result.ImmediateDom)),
		slog.Bool("virtual_exit", usedVirtualExit),
		slog.Duration("duration", time.Since(startTime)),
	)

	return result, nil
}

// detectExitNodes finds nodes with no outgoing edges.
//
// These are natural exit points in the call graph (functions that don't call anything else).
//
// Thread Safety: Safe for concurrent use (read-only on graph).
func (a *GraphAnalytics) detectExitNodes() []string {
	exits := make([]string, 0)
	for _, node := range a.graph.Nodes() {
		// Node is an exit if it has no outgoing edges
		if len(node.Outgoing) == 0 {
			exits = append(exits, node.ID)
		}
	}
	return exits
}

// computeReversePostorderReversed computes reverse postorder on the reversed graph.
//
// In the reversed graph, outgoing edges become incoming edges.
// This is used for post-dominator computation.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) computeReversePostorderReversed(
	ctx context.Context,
	exit string,
	exits []string,
	useVirtualExit bool,
) ([]string, map[string]int) {
	visited := make(map[string]bool)
	postOrder := make([]string, 0, a.graph.NodeCount())

	// Iterative DFS using explicit stack
	type frame struct {
		nodeID    string
		childIdx  int
		processed bool
	}

	// Start from exit (or virtual exit)
	stack := []frame{{nodeID: exit}}
	iterations := 0

	for len(stack) > 0 {
		iterations++
		if iterations%dominatorContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				indexMap := make(map[string]int, len(postOrder))
				for i, nodeID := range postOrder {
					indexMap[nodeID] = i
				}
				return postOrder, indexMap
			}
		}

		current := &stack[len(stack)-1]

		if !current.processed {
			if visited[current.nodeID] {
				stack = stack[:len(stack)-1]
				continue
			}
			visited[current.nodeID] = true
			current.processed = true
			current.childIdx = 0
		}

		// Get children in reversed graph
		// For virtual exit, children are the real exits
		// For real nodes, children are nodes with outgoing edges TO this node (incoming in original)
		var children []string
		if current.nodeID == VirtualExitNodeID {
			children = exits
		} else {
			node, ok := a.graph.GetNode(current.nodeID)
			if !ok {
				stack = stack[:len(stack)-1]
				continue
			}
			// In reversed graph, predecessors (incoming edges) become successors
			children = make([]string, len(node.Incoming))
			for i, edge := range node.Incoming {
				children[i] = edge.FromID
			}
		}

		// Process next unvisited child
		foundUnvisited := false
		for current.childIdx < len(children) {
			childID := children[current.childIdx]
			current.childIdx++

			if !visited[childID] {
				stack = append(stack, frame{nodeID: childID})
				foundUnvisited = true
				break
			}
		}

		if !foundUnvisited {
			// All children processed, add to postorder
			postOrder = append(postOrder, current.nodeID)
			stack = stack[:len(stack)-1]
		}
	}

	// Build index map for O(1) lookup
	indexMap := make(map[string]int, len(postOrder))
	for i, nodeID := range postOrder {
		indexMap[nodeID] = i
	}

	return postOrder, indexMap
}

// buildReversedPredecessors builds a map from nodeID to its predecessors in the reversed graph.
//
// In the reversed graph, predecessors of a node are nodes it has outgoing edges to.
// For the virtual exit, its predecessors are the real exit nodes.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildReversedPredecessors(
	ctx context.Context,
	postOrder []string,
	exits []string,
	useVirtualExit bool,
) map[string][]string {
	reachable := make(map[string]bool, len(postOrder))
	for _, nodeID := range postOrder {
		reachable[nodeID] = true
	}

	// Build set of real exits for quick lookup (deferred until needed)
	var exitSet map[string]bool
	if useVirtualExit {
		exitSet = make(map[string]bool, len(exits))
		for _, e := range exits {
			exitSet[e] = true
		}
	}

	predecessors := make(map[string][]string, len(postOrder))

	for i, nodeID := range postOrder {
		// Context cancellation check every 1000 nodes
		if i > 0 && i%1000 == 0 {
			if ctx != nil && ctx.Err() != nil {
				return predecessors // Return partial result
			}
		}

		if nodeID == VirtualExitNodeID {
			// Virtual exit has no predecessors (it's the root)
			continue
		}

		node, ok := a.graph.GetNode(nodeID)
		if !ok {
			continue
		}

		// In reversed graph, predecessors are nodes we have outgoing edges to
		// (they come "before" us in the reversed flow)
		for _, edge := range node.Outgoing {
			toID := edge.ToID
			if reachable[toID] {
				predecessors[nodeID] = append(predecessors[nodeID], toID)
			}
		}

		// If this is a real exit and we're using virtual exit, add virtual as predecessor
		if useVirtualExit && exitSet[nodeID] {
			predecessors[nodeID] = append(predecessors[nodeID], VirtualExitNodeID)
		}
	}

	return predecessors
}

// filterVirtualExit removes the virtual exit node from the result and adjusts
// relationships to point to real exits instead.
//
// Thread Safety: Not safe for concurrent use. Called only from PostDominators.
func (a *GraphAnalytics) filterVirtualExit(dt *DominatorTree, realExits []string) {
	// Remove virtual exit from ImmediateDom
	delete(dt.ImmediateDom, VirtualExitNodeID)

	// For nodes whose idom is virtual exit, they have no real immediate post-dominator
	// (they're at the "top" of their respective exit paths)
	// We'll update them to point to themselves (like entry in dominator tree)
	for node, idom := range dt.ImmediateDom {
		if idom == VirtualExitNodeID {
			// For real exits, they post-dominate themselves
			dt.ImmediateDom[node] = node
		}
	}

	// Remove virtual exit from postorder
	newPostOrder := make([]string, 0, len(dt.PostOrder))
	for _, nodeID := range dt.PostOrder {
		if nodeID != VirtualExitNodeID {
			newPostOrder = append(newPostOrder, nodeID)
		}
	}
	dt.PostOrder = newPostOrder

	// Rebuild postorder index
	dt.postOrderIndex = make(map[string]int, len(dt.PostOrder))
	for i, nodeID := range dt.PostOrder {
		dt.postOrderIndex[nodeID] = i
	}

	// Update entry to be one of the real exits (first one, arbitrarily chosen).
	// Note: For multi-exit post-dominator trees, Entry represents the first detected exit.
	// All real exits are at depth 0 in the tree since they all have idom = self.
	if len(realExits) > 0 {
		dt.Entry = realExits[0]
	}
}

// PostDominatorsWithCRS wraps PostDominators with CRS tracing.
//
// Description:
//
//	Provides the same functionality as PostDominators but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PostDominatorsWithCRS(ctx context.Context, exit string) (*DominatorTree, crs.TraceStep) {
	start := time.Now()

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_post_dominators").
			WithTarget(exit).
			WithTool("PostDominators").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &DominatorTree{
			Entry:        exit,
			ImmediateDom: map[string]string{},
			Depth:        map[string]int{},
		}, step
	}

	result, err := a.PostDominators(ctx, exit)

	// Build TraceStep
	builder := crs.NewTraceStepBuilder().
		WithAction("analytics_post_dominators").
		WithTarget(exit).
		WithTool("PostDominators").
		WithDuration(time.Since(start))

	if err != nil {
		builder = builder.WithError(err.Error())
	} else {
		maxDepth := result.MaxDepth()
		exitUsed := result.Entry
		if exit == "" {
			exitUsed = result.Entry + " (auto-detected)"
		}
		builder = builder.
			WithMetadata("exit", exitUsed).
			WithMetadata("iterations", itoa(result.Iterations)).
			WithMetadata("converged", btoa(result.Converged)).
			WithMetadata("max_depth", itoa(maxDepth)).
			WithMetadata("reachable_nodes", itoa(result.ReachableCount)).
			WithMetadata("node_count", itoa(result.NodeCount)).
			WithMetadata("exit_count", itoa(result.ExitCount)).
			WithMetadata("virtual_exit", btoa(result.UsedVirtualExit))
	}

	return result, builder.Build()
}
