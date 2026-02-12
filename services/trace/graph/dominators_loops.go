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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// Natural Loop Detection (GR-16f)
// =============================================================================

var loopDetectionTracer = otel.Tracer("graph.loop_detection")

// loopContextCheckInterval is how often to check for context cancellation.
const loopContextCheckInterval = 100

// Constants for loop detection limits.
const (
	// maxNestingDepthWarning triggers a warning if loop nesting exceeds this depth.
	maxNestingDepthWarning = 10

	// maxLoopBodyWarning triggers a warning if a loop body exceeds this size.
	maxLoopBodyWarning = 1000
)

// Loop represents a natural loop in the graph.
//
// A natural loop is defined by a back edge A→B where B dominates A.
// The header B is the single entry point to the loop.
//
// Thread Safety: Safe for concurrent use after construction.
type Loop struct {
	// Header is the loop header node ID (the dominator).
	// The header is the single entry point to the loop.
	Header string

	// BackEdges contains edges that form this loop.
	// Each entry is [fromID, toID] where toID is the header.
	BackEdges [][2]string

	// Body contains all node IDs in the loop body (including header).
	// Computed via reverse BFS from back edge sources.
	Body []string

	// Size is the number of nodes in the loop body.
	Size int

	// Depth is the nesting depth (0 = top-level loop).
	Depth int

	// Parent is the enclosing loop (nil if top-level).
	Parent *Loop

	// Children are nested loops within this loop.
	Children []*Loop
}

// LoopNest represents the complete loop nesting structure.
//
// Thread Safety: Safe for concurrent use after construction.
type LoopNest struct {
	// Loops contains all detected loops.
	Loops []*Loop

	// TopLevel contains loops with no parent.
	TopLevel []*Loop

	// LoopOf maps node ID → innermost containing loop.
	LoopOf map[string]*Loop

	// MaxDepth is the maximum nesting depth.
	MaxDepth int

	// BackEdgeCount is the total number of back edges found.
	BackEdgeCount int

	// DomTree is the dominator tree used for detection.
	DomTree *DominatorTree
}

// LoopDetectionError represents an error in loop detection.
type LoopDetectionError struct {
	Message string
}

func (e *LoopDetectionError) Error() string {
	return e.Message
}

// IsInLoop checks if a node is inside any loop.
//
// Description:
//
//	Returns true if the specified node is part of any detected loop body.
//	This includes loop headers and all nodes within loop bodies.
//
// Inputs:
//
//   - nodeID: The node to check. Must be a valid node ID.
//
// Outputs:
//
//   - bool: True if the node is in any loop, false otherwise.
//
// Limitations:
//
//   - Returns false if receiver is nil or nodeID is not in any loop.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(1) map lookup.
func (ln *LoopNest) IsInLoop(nodeID string) bool {
	if ln == nil || ln.LoopOf == nil {
		return false
	}
	_, ok := ln.LoopOf[nodeID]
	return ok
}

// GetInnermostLoop returns the innermost loop containing a node.
//
// Description:
//
//	Returns the most deeply nested loop that contains the specified node.
//	For nested loops, this returns the inner loop, not the outer one.
//
// Inputs:
//
//   - nodeID: The node to query. Must be a valid node ID.
//
// Outputs:
//
//   - *Loop: The innermost containing loop, or nil if not in any loop.
//
// Limitations:
//
//   - Returns nil if receiver is nil or nodeID is not in any loop.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(1) map lookup.
func (ln *LoopNest) GetInnermostLoop(nodeID string) *Loop {
	if ln == nil || ln.LoopOf == nil {
		return nil
	}
	return ln.LoopOf[nodeID]
}

// GetLoopByHeader returns the loop with the specified header.
//
// Description:
//
//	Finds the loop whose header matches the specified node ID.
//	Each loop has exactly one header.
//
// Inputs:
//
//   - headerID: The header node ID to search for.
//
// Outputs:
//
//   - *Loop: The loop with this header, or nil if no such loop exists.
//
// Limitations:
//
//   - Returns nil if receiver is nil or no loop has this header.
//   - Linear search through loops.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(loops) linear search.
func (ln *LoopNest) GetLoopByHeader(headerID string) *Loop {
	if ln == nil || ln.Loops == nil {
		return nil
	}
	for _, loop := range ln.Loops {
		if loop.Header == headerID {
			return loop
		}
	}
	return nil
}

// IsLoopHeader returns true if the node is a loop header.
//
// Description:
//
//	Checks if the specified node is the header (entry point) of any loop.
//
// Inputs:
//
//   - nodeID: The node to check. Must be a valid node ID.
//
// Outputs:
//
//   - bool: True if the node is a loop header, false otherwise.
//
// Limitations:
//
//   - Returns false if receiver is nil.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(loops) linear search.
func (ln *LoopNest) IsLoopHeader(nodeID string) bool {
	return ln.GetLoopByHeader(nodeID) != nil
}

// DetectLoops identifies natural loops in the call graph.
//
// Description:
//
//	Uses the dominator tree to find back edges (edges A→B where B dominates A),
//	then constructs natural loops and their nesting hierarchy. Each back edge
//	defines a natural loop with the target as the header.
//
//	The algorithm:
//	1. Find all back edges using the dominator tree
//	2. For each header, compute the loop body via reverse BFS
//	3. Build the loop nesting hierarchy
//	4. Assign depths and parent/child relationships
//
// Inputs:
//
//   - ctx: Context for cancellation. Checked between loop body computations.
//   - domTree: Pre-computed dominator tree. Must not be nil.
//
// Outputs:
//
//   - *LoopNest: Complete loop nesting structure. Never nil.
//   - error: Non-nil if domTree is nil or context cancelled.
//
// Example:
//
//	domTree, _ := analytics.Dominators(ctx, "main")
//	loops, err := analytics.DetectLoops(ctx, domTree)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, loop := range loops.Loops {
//	    log.Printf("Loop with header %s has %d nodes", loop.Header, loop.Size)
//	}
//
// Limitations:
//
//   - Only detects natural loops (single entry point)
//   - Irreducible loops are not fully characterized (logged as warning)
//   - Requires dominator tree to be precomputed
//
// Assumptions:
//
//   - domTree is valid and consistent
//   - Graph is frozen
//
// Thread Safety: Safe for concurrent use (read-only on graph and domTree).
//
// Complexity: O(V + E) after dominator tree.
func (a *GraphAnalytics) DetectLoops(
	ctx context.Context,
	domTree *DominatorTree,
) (*LoopNest, error) {
	// Initialize result with empty structures
	result := &LoopNest{
		Loops:    make([]*Loop, 0),
		TopLevel: make([]*Loop, 0),
		LoopOf:   make(map[string]*Loop),
	}

	// Nil dominator tree check
	if domTree == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: nil dominator tree")
		return result, &LoopDetectionError{Message: "dominator tree must not be nil"}
	}

	// Validate dominator tree has valid entry
	if domTree.Entry == "" {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: dominator tree has empty entry")
		return result, &LoopDetectionError{Message: "dominator tree has empty entry node"}
	}

	result.DomTree = domTree

	// Empty dominator tree check
	if len(domTree.ImmediateDom) == 0 {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: empty dominator tree, returning empty result")
		return result, nil
	}

	// Start timing
	startTime := time.Now()

	// Start OTel span
	ctx, span := loopDetectionTracer.Start(ctx, "GraphAnalytics.DetectLoops",
		trace.WithAttributes(
			attribute.String("entry", domTree.Entry),
			attribute.Int("node_count", len(domTree.ImmediateDom)),
		),
	)
	defer span.End()

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: context cancelled before start")
		return result, ctx.Err()
	}

	// Nil graph check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: nil analytics or graph")
		return result, nil
	}

	// Step 1: Find all back edges
	span.AddEvent("finding_back_edges")
	backEdges := a.findBackEdges(ctx, domTree)
	result.BackEdgeCount = len(backEdges)

	span.AddEvent("back_edges_found", trace.WithAttributes(
		attribute.Int("back_edge_count", len(backEdges)),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: back edges found",
		slog.Int("count", len(backEdges)),
	)

	if len(backEdges) == 0 {
		// No loops in this graph
		span.AddEvent("no_loops")
		return result, nil
	}

	// Step 2: Group back edges by header
	headerToBackEdges := make(map[string][][2]string)
	for _, edge := range backEdges {
		header := edge[1] // Target of back edge is the header
		headerToBackEdges[header] = append(headerToBackEdges[header], edge)
	}

	// Step 3: Compute loop body for each header
	span.AddEvent("computing_loop_bodies", trace.WithAttributes(
		attribute.Int("header_count", len(headerToBackEdges)),
	))

	loops := make([]*Loop, 0, len(headerToBackEdges))
	iterationsProcessed := 0

	for header, edges := range headerToBackEdges {
		iterationsProcessed++

		// Check context periodically
		if iterationsProcessed%loopContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				span.AddEvent("context_cancelled", trace.WithAttributes(
					attribute.Int("loops_processed", len(loops)),
				))
				telemetry.LoggerWithTrace(ctx, slog.Default()).Info("detect_loops: context cancelled",
					slog.Int("loops_processed", len(loops)),
				)
				// Return partial results
				result.Loops = loops
				return result, ctx.Err()
			}
		}

		// Compute loop body from all back edge sources
		body := a.computeLoopBody(header, edges)

		loop := &Loop{
			Header:    header,
			BackEdges: edges,
			Body:      body,
			Size:      len(body),
			Children:  make([]*Loop, 0),
		}

		loops = append(loops, loop)

		// Warn on large loop bodies
		if len(body) > maxLoopBodyWarning {
			telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("detect_loops: large loop body detected",
				slog.String("header", header),
				slog.Int("body_size", len(body)),
			)
		}
	}

	result.Loops = loops

	// Step 4: Build nesting hierarchy
	span.AddEvent("building_nesting_hierarchy")
	a.buildLoopNestingHierarchy(ctx, result)

	// Step 5: Build LoopOf map (node → innermost loop)
	a.buildLoopOfMap(result)

	// Compute statistics
	maxDepth := 0
	for _, loop := range result.Loops {
		if loop.Depth > maxDepth {
			maxDepth = loop.Depth
		}
	}
	result.MaxDepth = maxDepth

	// Warn on deep nesting
	if maxDepth > maxNestingDepthWarning {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("detect_loops: deeply nested loops detected",
			slog.Int("max_depth", maxDepth),
		)
	}

	span.AddEvent("loop_detection_complete", trace.WithAttributes(
		attribute.Int("loops_found", len(result.Loops)),
		attribute.Int("top_level", len(result.TopLevel)),
		attribute.Int("max_depth", result.MaxDepth),
		attribute.Int("back_edges", result.BackEdgeCount),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("detect_loops: detection complete",
		slog.Int("loops", len(result.Loops)),
		slog.Int("top_level", len(result.TopLevel)),
		slog.Int("max_depth", result.MaxDepth),
		slog.Duration("duration", time.Since(startTime)),
	)

	return result, nil
}

// findBackEdges finds all back edges in the graph using the dominator tree.
//
// A back edge is an edge A→B where B dominates A.
// This indicates a loop with B as the header.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) findBackEdges(ctx context.Context, domTree *DominatorTree) [][2]string {
	backEdges := make([][2]string, 0)
	iterationsProcessed := 0

	for _, node := range a.graph.Nodes() {
		iterationsProcessed++

		// Check context periodically
		if iterationsProcessed%loopContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				return backEdges // Return partial result
			}
		}

		nodeID := node.ID

		// Skip nodes not in dominator tree
		if _, inTree := domTree.ImmediateDom[nodeID]; !inTree && nodeID != domTree.Entry {
			continue
		}

		for _, edge := range node.Outgoing {
			toID := edge.ToID

			// Skip edges to nodes not in dominator tree
			if _, inTree := domTree.ImmediateDom[toID]; !inTree && toID != domTree.Entry {
				continue
			}

			// Back edge: target dominates source
			if domTree.Dominates(toID, nodeID) {
				backEdges = append(backEdges, [2]string{nodeID, toID})
			}
		}
	}

	return backEdges
}

// computeLoopBody computes the loop body for a given header and back edges.
//
// The loop body consists of all nodes that can reach a back edge source
// without going through the header. Uses reverse BFS from back edge sources.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) computeLoopBody(header string, backEdges [][2]string) []string {
	// Body always includes the header
	body := make(map[string]bool)
	body[header] = true

	// Worklist starts with all back edge sources
	worklist := make([]string, 0, len(backEdges))
	for _, edge := range backEdges {
		sourceID := edge[0]
		if sourceID != header && !body[sourceID] {
			worklist = append(worklist, sourceID)
			body[sourceID] = true
		}
	}

	// Reverse BFS: add predecessors until we reach the header
	for len(worklist) > 0 {
		node := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]

		graphNode, ok := a.graph.GetNode(node)
		if !ok {
			continue
		}

		// Add predecessors to worklist
		for _, edge := range graphNode.Incoming {
			predID := edge.FromID
			if !body[predID] {
				body[predID] = true
				worklist = append(worklist, predID)
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(body))
	for nodeID := range body {
		result = append(result, nodeID)
	}

	return result
}

// buildLoopNestingHierarchy establishes parent/child relationships between loops.
//
// A loop L1 is nested inside L2 if L1's header is in L2's body and L1 != L2.
// The nesting is proper: each loop has at most one parent.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildLoopNestingHierarchy(ctx context.Context, result *LoopNest) {
	loops := result.Loops

	// Build body sets for O(1) containment checks
	bodySets := make([]map[string]bool, len(loops))
	for i, loop := range loops {
		bodySets[i] = make(map[string]bool, len(loop.Body))
		for _, nodeID := range loop.Body {
			bodySets[i][nodeID] = true
		}
	}

	// Find parent for each loop
	// Parent is the smallest enclosing loop (by body size)
	for i, innerLoop := range loops {
		var bestParent *Loop
		bestParentSize := -1

		for j, outerLoop := range loops {
			if i == j {
				continue // Can't be own parent
			}

			// Check if inner's header is in outer's body
			if bodySets[j][innerLoop.Header] {
				// Outer contains inner - check if it's the smallest such loop
				if bestParent == nil || outerLoop.Size < bestParentSize {
					bestParent = outerLoop
					bestParentSize = outerLoop.Size
				}
			}
		}

		if bestParent != nil {
			innerLoop.Parent = bestParent
			bestParent.Children = append(bestParent.Children, innerLoop)
		}
	}

	// Collect top-level loops and compute depths
	for _, loop := range loops {
		if loop.Parent == nil {
			result.TopLevel = append(result.TopLevel, loop)
		}
	}

	// Compute depths via BFS from top-level
	for _, topLoop := range result.TopLevel {
		a.computeLoopDepths(topLoop, 0)
	}
}

// computeLoopDepths recursively assigns depths to loops.
func (a *GraphAnalytics) computeLoopDepths(loop *Loop, depth int) {
	loop.Depth = depth
	for _, child := range loop.Children {
		a.computeLoopDepths(child, depth+1)
	}
}

// buildLoopOfMap builds the node → innermost loop mapping.
//
// Each node maps to the innermost (most deeply nested) loop containing it.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildLoopOfMap(result *LoopNest) {
	// Process loops from deepest to shallowest so inner loops overwrite outer
	// First, sort by depth (deepest first)
	loopsByDepth := make([]*Loop, len(result.Loops))
	copy(loopsByDepth, result.Loops)

	// Simple bubble sort (typically very few loops)
	for i := 0; i < len(loopsByDepth); i++ {
		for j := i + 1; j < len(loopsByDepth); j++ {
			if loopsByDepth[i].Depth < loopsByDepth[j].Depth {
				loopsByDepth[i], loopsByDepth[j] = loopsByDepth[j], loopsByDepth[i]
			}
		}
	}

	// Build map from deepest loops first
	for _, loop := range loopsByDepth {
		for _, nodeID := range loop.Body {
			// Only set if not already set (preserves innermost)
			if _, exists := result.LoopOf[nodeID]; !exists {
				result.LoopOf[nodeID] = loop
			}
		}
	}
}

// DetectLoopsWithCRS wraps DetectLoops with CRS tracing.
//
// Description:
//
//	Provides the same functionality as DetectLoops but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DetectLoopsWithCRS(
	ctx context.Context,
	domTree *DominatorTree,
) (*LoopNest, crs.TraceStep) {
	start := time.Now()

	// Handle nil domTree case
	if domTree == nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_loops").
			WithTarget("graph").
			WithTool("DetectLoops").
			WithDuration(time.Since(start)).
			WithError("dominator tree must not be nil").
			Build()
		return &LoopNest{
			Loops:    make([]*Loop, 0),
			TopLevel: make([]*Loop, 0),
			LoopOf:   make(map[string]*Loop),
		}, step
	}

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_loops").
			WithTarget(domTree.Entry).
			WithTool("DetectLoops").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &LoopNest{
			Loops:    make([]*Loop, 0),
			TopLevel: make([]*Loop, 0),
			LoopOf:   make(map[string]*Loop),
			DomTree:  domTree,
		}, step
	}

	result, err := a.DetectLoops(ctx, domTree)

	// Build TraceStep
	builder := crs.NewTraceStepBuilder().
		WithAction("analytics_loops").
		WithTarget(domTree.Entry).
		WithTool("DetectLoops").
		WithDuration(time.Since(start))

	if err != nil {
		builder = builder.WithError(err.Error())
	} else {
		builder = builder.
			WithMetadata("loops_found", itoa(len(result.Loops))).
			WithMetadata("top_level_loops", itoa(len(result.TopLevel))).
			WithMetadata("max_depth", itoa(result.MaxDepth)).
			WithMetadata("back_edges", itoa(result.BackEdgeCount)).
			WithMetadata("entry", domTree.Entry)
	}

	return result, builder.Build()
}
