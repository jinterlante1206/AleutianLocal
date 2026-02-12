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
// Articulation Points / Cut Vertices (GR-16a)
// =============================================================================

var articulationTracer = otel.Tracer("graph.articulation")

// articulationContextCheckInterval is how often to check for context cancellation.
const articulationContextCheckInterval = 1000

// ArticulationResult contains the articulation point analysis.
type ArticulationResult struct {
	// Points contains node IDs that are articulation points.
	// An articulation point is a node whose removal increases the number
	// of connected components in the graph.
	Points []string

	// Bridges contains edges whose removal disconnects the graph.
	// Each bridge is represented as [fromID, toID].
	Bridges [][2]string

	// Components is the number of connected components in the graph.
	// For a fully connected graph, this equals 1. For disconnected graphs,
	// this equals the number of separate subgraphs.
	Components int

	// NodeCount is the total nodes analyzed.
	NodeCount int

	// EdgeCount is the total edges analyzed.
	EdgeCount int
}

// Phase constants for iterative DFS in Tarjan's algorithm.
const (
	phaseInit         = 0 // Initialize node: set discovery/low-link times, mark visited
	phaseProcessEdges = 1 // Iterate through neighbors, push unvisited to stack
	phasePostChild    = 2 // Return from child: update low-link, check articulation condition
	phaseFinalize     = 3 // Complete node processing: check root articulation, pop from stack
)

// articulationFrame represents a stack frame for iterative DFS.
// Using iterative DFS avoids stack overflow on deep graphs.
// The phase field controls which step of the algorithm executes next.
type articulationFrame struct {
	nodeID     string
	parentID   string
	edgeIndex  int    // Current index into neighbors
	phase      int    // One of phaseInit, phaseProcessEdges, phasePostChild, phaseFinalize
	childID    string // ID of child we just returned from
	childCount int    // Number of DFS tree children (for root check)
}

// ArticulationPoints finds cut vertices using Tarjan's algorithm.
//
// Description:
//
//	Uses iterative DFS (to avoid stack overflow on deep graphs) to find
//	nodes whose removal would disconnect the graph when treated as undirected.
//	Also identifies bridges (critical edges).
//
//	The algorithm treats the directed call graph as undirected, which means
//	both incoming and outgoing edges are considered for connectivity analysis.
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil. Checked every 1000 nodes.
//
// Outputs:
//
//   - *ArticulationResult: Analysis results. Never nil on success.
//     Contains Points (cut vertices), Bridges (critical edges), and component count.
//   - error: Non-nil only on context cancellation. Partial results still returned.
//
// Example:
//
//	result, err := analytics.ArticulationPoints(ctx)
//	if err != nil {
//	    // Context was cancelled - result may be partial
//	    log.Printf("cancelled after processing %d nodes", result.NodeCount)
//	}
//	for _, point := range result.Points {
//	    log.Printf("Single point of failure: %s", point)
//	}
//
// Limitations:
//
//   - Treats directed graph as undirected for connectivity
//   - Does not identify direction-sensitive articulation points
//   - Memory usage scales with O(V) for auxiliary data structures
//
// Assumptions:
//
//   - Graph is frozen (guaranteed by HierarchicalGraph construction)
//   - Node IDs are valid and non-empty strings
//   - Self-loops are skipped (do not affect connectivity)
//
// Thread Safety: Safe for concurrent use (read-only on graph).
//
// Complexity: O(V + E) time, O(V) space.
func (a *GraphAnalytics) ArticulationPoints(ctx context.Context) (*ArticulationResult, error) {
	// Initialize result with empty slices (never return nil slices)
	result := &ArticulationResult{
		Points:  make([]string, 0),
		Bridges: make([][2]string, 0),
	}

	// Nil graph check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("articulation_points: nil analytics or graph, returning empty result")
		return result, nil
	}

	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	// Start OTel span
	ctx, span := articulationTracer.Start(ctx, "GraphAnalytics.ArticulationPoints",
		trace.WithAttributes(
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
		),
	)
	defer span.End()

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("articulation_points: context cancelled before start")
		return result, ctx.Err()
	}

	// Empty graph check
	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("articulation_points: empty graph, returning empty result")
		return result, nil
	}

	result.NodeCount = nodeCount
	result.EdgeCount = edgeCount

	// Pre-allocate based on heuristics
	// Typically < 5% of nodes are articulation points
	estimatedPoints := nodeCount / 20
	if estimatedPoints < 10 {
		estimatedPoints = 10
	}
	result.Points = make([]string, 0, estimatedPoints)
	result.Bridges = make([][2]string, 0, estimatedPoints/2)

	// Build adjacency list treating graph as undirected
	neighbors := a.buildUndirectedNeighbors()

	// Tarjan's algorithm state
	discovery := make(map[string]int, nodeCount)
	lowLink := make(map[string]int, nodeCount)
	visited := make(map[string]bool, nodeCount)
	parent := make(map[string]string, nodeCount)
	isArticulation := make(map[string]bool, nodeCount)

	timer := 0
	iterationsProcessed := 0
	components := 0

	// Get all node IDs
	allNodes := a.graph.Nodes()

	// Process each unvisited node (handles disconnected graphs)
	for _, startNode := range allNodes {
		if visited[startNode.ID] {
			continue
		}

		// Run iterative DFS from this start node
		err := a.tarjanIterative(
			ctx,
			startNode.ID,
			neighbors,
			discovery,
			lowLink,
			visited,
			parent,
			isArticulation,
			&timer,
			&iterationsProcessed,
			result,
		)
		if err != nil {
			span.AddEvent("context_cancelled", trace.WithAttributes(
				attribute.Int("nodes_processed", iterationsProcessed),
			))
			telemetry.LoggerWithTrace(ctx, slog.Default()).Info("articulation_points: context cancelled",
				slog.Int("nodes_processed", iterationsProcessed),
				slog.Int("total_nodes", nodeCount),
			)
			return result, err
		}

		components++
	}

	// Convert articulation points map to slice
	for nodeID, isAP := range isArticulation {
		if isAP {
			result.Points = append(result.Points, nodeID)
		}
	}

	result.Components = components

	span.AddEvent("algorithm_complete", trace.WithAttributes(
		attribute.Int("articulation_points", len(result.Points)),
		attribute.Int("bridges", len(result.Bridges)),
		attribute.Int("components", components),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("articulation_points: analysis complete",
		slog.Int("points", len(result.Points)),
		slog.Int("bridges", len(result.Bridges)),
		slog.Int("components", components),
	)

	return result, nil
}

// buildUndirectedNeighbors creates an adjacency list treating directed edges as undirected.
//
// For each node, collects all nodes reachable via outgoing OR incoming edges.
// Self-loops are excluded. Parallel edges are deduplicated to avoid redundant processing.
//
// Thread Safety: Safe for concurrent use (read-only on graph).
func (a *GraphAnalytics) buildUndirectedNeighbors() map[string][]string {
	start := time.Now()
	neighbors := make(map[string][]string)

	for _, node := range a.graph.Nodes() {
		nodeID := node.ID
		// Use a set to deduplicate neighbors (handles parallel edges)
		seen := make(map[string]bool, len(node.Outgoing)+len(node.Incoming))

		// Add outgoing neighbors
		for _, edge := range node.Outgoing {
			// Skip self-loops
			if edge.ToID == nodeID {
				continue
			}
			if !seen[edge.ToID] {
				seen[edge.ToID] = true
			}
		}

		// Add incoming neighbors (treating as undirected)
		for _, edge := range node.Incoming {
			// Skip self-loops
			if edge.FromID == nodeID {
				continue
			}
			if !seen[edge.FromID] {
				seen[edge.FromID] = true
			}
		}

		// Convert set to slice
		neighbors[nodeID] = make([]string, 0, len(seen))
		for neighborID := range seen {
			neighbors[nodeID] = append(neighbors[nodeID], neighborID)
		}
	}

	slog.Debug("articulation_points: built undirected neighbors",
		slog.Int("node_count", len(neighbors)),
		slog.Duration("duration", time.Since(start)),
	)

	return neighbors
}

// tarjanIterative performs Tarjan's articulation point algorithm using an explicit stack.
//
// This iterative approach avoids stack overflow on deep graphs that would occur
// with recursive DFS. The algorithm uses phase-based state machine per stack frame
// to simulate recursive call/return semantics.
//
// Parameters are passed by reference to accumulate state across the connected component.
// The function processes one connected component starting from startNodeID.
//
// Returns error only on context cancellation; nil otherwise.
//
// Thread Safety: Not safe for concurrent use. Called from ArticulationPoints which is thread-safe.
func (a *GraphAnalytics) tarjanIterative(
	ctx context.Context,
	startNodeID string,
	neighbors map[string][]string,
	discovery map[string]int,
	lowLink map[string]int,
	visited map[string]bool,
	parent map[string]string,
	isArticulation map[string]bool,
	timer *int,
	iterationsProcessed *int,
	result *ArticulationResult,
) error {
	stack := make([]articulationFrame, 0, 100)
	stack = append(stack, articulationFrame{
		nodeID:   startNodeID,
		parentID: "",
		phase:    phaseInit,
	})

	for len(stack) > 0 {
		// Batch context check
		*iterationsProcessed++
		if *iterationsProcessed%articulationContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				return ctx.Err()
			}
		}

		frame := &stack[len(stack)-1]

		switch frame.phase {
		case phaseInit:
			// Initialize node
			visited[frame.nodeID] = true
			discovery[frame.nodeID] = *timer
			lowLink[frame.nodeID] = *timer
			*timer++
			parent[frame.nodeID] = frame.parentID
			frame.childCount = 0
			frame.edgeIndex = 0
			frame.phase = phaseProcessEdges

		case phaseProcessEdges:
			// Process neighbors
			nodeNeighbors := neighbors[frame.nodeID]
			for frame.edgeIndex < len(nodeNeighbors) {
				neighborID := nodeNeighbors[frame.edgeIndex]
				frame.edgeIndex++

				// Skip the parent edge (avoid going back)
				if neighborID == frame.parentID {
					continue
				}

				if !visited[neighborID] {
					// Tree edge - recurse
					frame.phase = phasePostChild
					frame.childID = neighborID
					frame.childCount++
					stack = append(stack, articulationFrame{
						nodeID:   neighborID,
						parentID: frame.nodeID,
						phase:    phaseInit,
					})
					goto continueLoop
				} else {
					// Back edge - update low-link
					if discovery[neighborID] < lowLink[frame.nodeID] {
						lowLink[frame.nodeID] = discovery[neighborID]
					}
				}
			}
			// All neighbors processed
			frame.phase = phaseFinalize

		case phasePostChild:
			// Post-child processing
			// Update low-link from child
			if lowLink[frame.childID] < lowLink[frame.nodeID] {
				lowLink[frame.nodeID] = lowLink[frame.childID]
			}

			// Check articulation point condition for non-root
			if frame.parentID != "" && lowLink[frame.childID] >= discovery[frame.nodeID] {
				isArticulation[frame.nodeID] = true
			}

			// Check for bridge
			if lowLink[frame.childID] > discovery[frame.nodeID] {
				result.Bridges = append(result.Bridges, [2]string{frame.nodeID, frame.childID})
			}

			frame.phase = phaseProcessEdges // Continue processing neighbors

		case phaseFinalize:
			// Finalization
			// Root is articulation point if it has 2+ children
			if frame.parentID == "" && frame.childCount >= 2 {
				isArticulation[frame.nodeID] = true
			}

			// Pop frame
			stack = stack[:len(stack)-1]
		}
	continueLoop:
	}

	return nil
}

// ArticulationPointsWithCRS wraps ArticulationPoints with CRS tracing.
//
// Description:
//
//	Provides the same functionality as ArticulationPoints but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) ArticulationPointsWithCRS(ctx context.Context) (*ArticulationResult, crs.TraceStep) {
	start := time.Now()

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_articulation_points").
			WithTarget("graph").
			WithTool("ArticulationPoints").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &ArticulationResult{Points: []string{}, Bridges: [][2]string{}}, step
	}

	result, err := a.ArticulationPoints(ctx)

	// Error case
	if err != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_articulation_points").
			WithTarget("graph").
			WithTool("ArticulationPoints").
			WithDuration(time.Since(start)).
			WithError(err.Error()).
			Build()
		return result, step
	}

	// Success case
	step := crs.NewTraceStepBuilder().
		WithAction("analytics_articulation_points").
		WithTarget("graph").
		WithTool("ArticulationPoints").
		WithDuration(time.Since(start)).
		WithMetadata("points_found", itoa(len(result.Points))).
		WithMetadata("bridges_found", itoa(len(result.Bridges))).
		WithMetadata("components", itoa(result.Components)).
		WithMetadata("node_count", itoa(result.NodeCount)).
		WithMetadata("edge_count", itoa(result.EdgeCount)).
		Build()

	return result, step
}
