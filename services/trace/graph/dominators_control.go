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
// Control Dependence Graph (GR-16e)
// =============================================================================

var controlDependenceTracer = otel.Tracer("graph.control_dependence")

// ControlDependence represents the control dependence graph.
//
// A node B is control-dependent on A if:
//  1. There exists a path from A to B where A is not post-dominated by B
//  2. B post-dominates all nodes on the path after A
//
// This tells us "which conditionals control whether B executes."
//
// Thread Safety: Safe for concurrent use after construction.
type ControlDependence struct {
	// Dependencies maps node → nodes it is control-dependent on.
	// "What controls whether this node executes?"
	Dependencies map[string][]string

	// Dependents maps node → nodes that are control-dependent on it.
	// "What does this node control the execution of?"
	Dependents map[string][]string

	// EdgeCount is the total control dependence edges.
	EdgeCount int

	// NodeCount is the total nodes analyzed.
	NodeCount int

	// NodesWithDependencies is the count of nodes with non-empty Dependencies.
	NodesWithDependencies int

	// NodesWithDependents is the count of nodes with non-empty Dependents.
	NodesWithDependents int

	// MaxDependencies is the most dependencies any single node has.
	MaxDependencies int

	// MaxDependents is the most dependents any single controller has.
	MaxDependents int

	// PostDomTree is the underlying post-dominator tree used.
	PostDomTree *DominatorTree
}

// ControlDependenceError represents an error in control dependence computation.
type ControlDependenceError struct {
	Message string
}

func (e *ControlDependenceError) Error() string {
	return e.Message
}

// GetDependencies returns what controls this node.
//
// Description:
//
//	Returns the list of nodes that control whether this node executes.
//	A control dependency from A to B means "A's branch decision determines
//	whether B runs".
//
// Inputs:
//
//   - nodeID: The node to query dependencies for. Must be a valid node ID.
//
// Outputs:
//
//   - []string: List of controlling nodes, or nil if the node has no
//     dependencies or the receiver is nil.
//
// Limitations:
//
//   - Returns nil for nodes not in the post-dominator tree.
//   - The returned slice should not be modified by the caller.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(1) map lookup.
func (cd *ControlDependence) GetDependencies(nodeID string) []string {
	if cd == nil || cd.Dependencies == nil {
		return nil
	}
	return cd.Dependencies[nodeID]
}

// GetDependents returns what this node controls.
//
// Description:
//
//	Returns the list of nodes whose execution is controlled by this node.
//	If this node is a branch point, its dependents are the nodes that
//	only execute on certain branches.
//
// Inputs:
//
//   - nodeID: The node to query dependents for. Must be a valid node ID.
//
// Outputs:
//
//   - []string: List of controlled nodes, or nil if the node has no
//     dependents or the receiver is nil.
//
// Limitations:
//
//   - Returns nil for nodes that are not controllers.
//   - The returned slice should not be modified by the caller.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(1) map lookup.
func (cd *ControlDependence) GetDependents(nodeID string) []string {
	if cd == nil || cd.Dependents == nil {
		return nil
	}
	return cd.Dependents[nodeID]
}

// IsController returns true if this node controls other nodes.
//
// Description:
//
//	Checks whether this node is a control point (branch/decision point)
//	that determines whether other nodes execute.
//
// Inputs:
//
//   - nodeID: The node to check. Must be a valid node ID.
//
// Outputs:
//
//   - bool: True if this node controls at least one other node,
//     false otherwise.
//
// Limitations:
//
//   - Returns false if receiver is nil.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(1) map lookup and length check.
func (cd *ControlDependence) IsController(nodeID string) bool {
	if cd == nil || cd.Dependents == nil {
		return false
	}
	return len(cd.Dependents[nodeID]) > 0
}

// ControlDependencyChain returns the transitive chain of control dependencies.
//
// Description:
//
//	Starting from nodeID, walks up the control dependency chain collecting
//	all controllers. Uses BFS to find all controllers at each level.
//	Stops at maxDepth or when no more dependencies exist.
//
// Inputs:
//
//   - nodeID: The starting node to find dependencies for.
//   - maxDepth: Maximum depth to traverse. Must be > 0.
//
// Outputs:
//
//   - []string: Chain of controllers in BFS order (closest first).
//     Returns nil if receiver is nil, maxDepth <= 0, or no dependencies exist.
//
// Limitations:
//
//   - Does not include nodeID itself in the output.
//   - Cycles are handled by tracking visited nodes.
//   - BFS order means all depth-1 controllers appear before depth-2, etc.
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(edges traversed) bounded by maxDepth.
func (cd *ControlDependence) ControlDependencyChain(nodeID string, maxDepth int) []string {
	if cd == nil || cd.Dependencies == nil || maxDepth <= 0 {
		return nil
	}

	chain := make([]string, 0, maxDepth)
	visited := make(map[string]bool)
	queue := []string{nodeID}
	depth := 0

	for len(queue) > 0 && depth < maxDepth {
		nextQueue := make([]string, 0)
		for _, current := range queue {
			if visited[current] {
				continue
			}
			visited[current] = true

			deps := cd.Dependencies[current]
			for _, dep := range deps {
				if !visited[dep] {
					chain = append(chain, dep)
					nextQueue = append(nextQueue, dep)
				}
			}
		}
		queue = nextQueue
		depth++
	}

	return chain
}

// ComputeControlDependence builds the control dependence graph.
//
// Description:
//
//	Uses the post-dominance frontier to compute which nodes control
//	the execution of other nodes. A decision point controls nodes
//	that appear in its post-dominance frontier.
//
//	For each node n in PostDF(m), n is control-dependent on m.
//
// Inputs:
//
//   - ctx: Context for cancellation. Checked during dominance frontier computation.
//   - postDomTree: Pre-computed post-dominator tree. Must not be nil.
//
// Outputs:
//
//   - *ControlDependence: The control dependence relationships. Never nil.
//   - error: Non-nil if postDomTree is nil or context cancelled.
//
// Example:
//
//	postDomTree, _ := analytics.PostDominators(ctx, "")
//	cd, err := analytics.ComputeControlDependence(ctx, postDomTree)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// What controls the execution of handler?
//	controllers := cd.GetDependencies("handler")
//	// What does the condition control?
//	controlled := cd.GetDependents("condition")
//
// Limitations:
//
//   - Only computes dependencies for nodes in the post-dominator tree
//   - Unreachable nodes have no dependencies
//   - Memory: O(E) where E is control dependence edges
//
// Assumptions:
//
//   - postDomTree is valid and consistent
//   - Graph is frozen
//
// Thread Safety: Safe for concurrent use (read-only on graph and postDomTree).
//
// Complexity: O(E × depth), same as dominance frontier.
func (a *GraphAnalytics) ComputeControlDependence(
	ctx context.Context,
	postDomTree *DominatorTree,
) (*ControlDependence, error) {
	// Initialize result with empty structures
	result := &ControlDependence{
		Dependencies: make(map[string][]string),
		Dependents:   make(map[string][]string),
	}

	// Nil post-dominator tree check
	if postDomTree == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: nil post-dominator tree")
		return result, &ControlDependenceError{Message: "post-dominator tree must not be nil"}
	}

	// Validate post-dominator tree has valid entry (exit node)
	if postDomTree.Entry == "" {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: post-dominator tree has empty entry")
		return result, &ControlDependenceError{Message: "post-dominator tree has empty entry node"}
	}

	result.PostDomTree = postDomTree

	// Empty post-dominator tree check
	if len(postDomTree.ImmediateDom) == 0 {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: empty post-dominator tree, returning empty result")
		return result, nil
	}

	// Start timing
	startTime := time.Now()

	// Start OTel span
	ctx, span := controlDependenceTracer.Start(ctx, "GraphAnalytics.ComputeControlDependence",
		trace.WithAttributes(
			attribute.String("exit", postDomTree.Entry),
			attribute.Int("node_count", len(postDomTree.ImmediateDom)),
		),
	)
	defer span.End()

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: context cancelled before start")
		return result, ctx.Err()
	}

	// Nil graph check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: nil analytics or graph")
		return result, nil
	}

	result.NodeCount = len(postDomTree.ImmediateDom)

	// Compute post-dominance frontier
	// Note: Unlike regular dominance frontier which uses predecessors (Incoming),
	// post-dominance frontier uses successors (Outgoing) because it's computed
	// on the reversed graph.
	span.AddEvent("post_dominance_frontier_start")
	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: computing post-dominance frontier",
		slog.Int("node_count", result.NodeCount),
	)

	// Use set semantics internally to prevent duplicates
	// frontierSets[node] contains the set of nodes in PostDF(node)
	frontierSets := make(map[string]map[string]struct{}, len(postDomTree.ImmediateDom))
	iterationsProcessed := 0
	allNodes := a.graph.Nodes()

	for _, node := range allNodes {
		iterationsProcessed++

		// Check context periodically
		if iterationsProcessed%frontierContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				span.AddEvent("context_cancelled", trace.WithAttributes(
					attribute.Int("nodes_processed", iterationsProcessed),
				))
				telemetry.LoggerWithTrace(ctx, slog.Default()).Info("control_dependence: context cancelled",
					slog.Int("nodes_processed", iterationsProcessed),
				)
				return result, ctx.Err()
			}
		}

		nodeID := node.ID

		// Skip nodes not in post-dominator tree (unreachable from exits)
		ipostdomNode, inTree := postDomTree.ImmediateDom[nodeID]
		if !inTree {
			continue
		}

		// For post-dominance frontier, use successors (Outgoing edges)
		// This is the key difference from regular dominance frontier
		succs := node.Outgoing
		if len(succs) < 2 {
			// Nodes with < 2 successors can't be control flow split points
			// (single successor = no branch = no control dependence from this node)
			continue
		}

		// For each successor, walk up the post-dominator tree
		for _, succ := range succs {
			succID := succ.ToID

			// Skip self-loops
			if succID == nodeID {
				continue
			}

			// Skip successors not in post-dominator tree
			if _, succInTree := postDomTree.ImmediateDom[succID]; !succInTree {
				continue
			}

			// Walk up from successor to ipostdom(node)
			runner := succID
			chainLength := 0
			maxChainLength := len(postDomTree.ImmediateDom) + 1 // Safety limit

			for runner != ipostdomNode && runner != "" && chainLength < maxChainLength {
				// runner is control-dependent on nodeID
				// (nodeID controls whether runner executes)
				if frontierSets[nodeID] == nil {
					frontierSets[nodeID] = make(map[string]struct{})
				}
				frontierSets[nodeID][runner] = struct{}{}

				// Move up to parent in post-dominator tree
				nextRunner, ok := postDomTree.ImmediateDom[runner]
				if !ok || nextRunner == runner {
					// Reached root or cycle - stop
					break
				}
				runner = nextRunner
				chainLength++
			}

			if chainLength >= maxChainLength {
				telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("control_dependence: ipostdom chain exceeded max length",
					slog.String("node", nodeID),
					slog.String("succ", succID),
					slog.Int("chain_length", chainLength),
				)
			}
		}
	}

	// Convert sets to maps and count statistics
	totalFrontierEntries := 0
	for nodeID, frontierSet := range frontierSets {
		for controlled := range frontierSet {
			result.Dependencies[controlled] = append(result.Dependencies[controlled], nodeID)
			result.Dependents[nodeID] = append(result.Dependents[nodeID], controlled)
			result.EdgeCount++
			totalFrontierEntries++
		}
	}

	span.AddEvent("post_dominance_frontier_complete", trace.WithAttributes(
		attribute.Int("controllers", len(frontierSets)),
		attribute.Int("total_frontier_entries", totalFrontierEntries),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: post-dominance frontier computed",
		slog.Int("controllers", len(frontierSets)),
		slog.Int("frontier_entries", totalFrontierEntries),
	)

	// Compute statistics
	for nodeID, deps := range result.Dependencies {
		if len(deps) > 0 {
			result.NodesWithDependencies++
			if len(deps) > result.MaxDependencies {
				result.MaxDependencies = len(deps)
				telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: node with many dependencies",
					slog.String("node", nodeID),
					slog.Int("count", len(deps)),
				)
			}
		}
	}

	for nodeID, deps := range result.Dependents {
		if len(deps) > 0 {
			result.NodesWithDependents++
			if len(deps) > result.MaxDependents {
				result.MaxDependents = len(deps)
			}
		}

		// Warn on unusually high dependent count
		if len(deps) > 100 {
			telemetry.LoggerWithTrace(ctx, slog.Default()).Warn("control_dependence: controller with many dependents",
				slog.String("controller", nodeID),
				slog.Int("dependents", len(deps)),
			)
		}
	}

	span.AddEvent("control_dependence_complete", trace.WithAttributes(
		attribute.Int("edge_count", result.EdgeCount),
		attribute.Int("nodes_with_dependencies", result.NodesWithDependencies),
		attribute.Int("nodes_with_dependents", result.NodesWithDependents),
		attribute.Int("max_dependencies", result.MaxDependencies),
		attribute.Int("max_dependents", result.MaxDependents),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("control_dependence: computation complete",
		slog.Int("edge_count", result.EdgeCount),
		slog.Int("controllers", result.NodesWithDependents),
		slog.Int("controlled", result.NodesWithDependencies),
		slog.Duration("duration", time.Since(startTime)),
	)

	return result, nil
}

// ComputeControlDependenceWithCRS wraps ComputeControlDependence with CRS tracing.
//
// Description:
//
//	Provides the same functionality as ComputeControlDependence but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) ComputeControlDependenceWithCRS(
	ctx context.Context,
	postDomTree *DominatorTree,
) (*ControlDependence, crs.TraceStep) {
	start := time.Now()

	// Handle nil postDomTree case
	if postDomTree == nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_control_dependence").
			WithTarget("").
			WithTool("ComputeControlDependence").
			WithDuration(time.Since(start)).
			WithError("post-dominator tree must not be nil").
			Build()
		return &ControlDependence{
			Dependencies: make(map[string][]string),
			Dependents:   make(map[string][]string),
		}, step
	}

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_control_dependence").
			WithTarget(postDomTree.Entry).
			WithTool("ComputeControlDependence").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &ControlDependence{
			Dependencies: make(map[string][]string),
			Dependents:   make(map[string][]string),
			PostDomTree:  postDomTree,
		}, step
	}

	result, err := a.ComputeControlDependence(ctx, postDomTree)

	// Build TraceStep
	builder := crs.NewTraceStepBuilder().
		WithAction("analytics_control_dependence").
		WithTarget(postDomTree.Entry).
		WithTool("ComputeControlDependence").
		WithDuration(time.Since(start))

	if err != nil {
		builder = builder.WithError(err.Error())
	} else {
		builder = builder.
			WithMetadata("edge_count", itoa(result.EdgeCount)).
			WithMetadata("nodes_with_dependencies", itoa(result.NodesWithDependencies)).
			WithMetadata("nodes_with_dependents", itoa(result.NodesWithDependents)).
			WithMetadata("max_dependencies", itoa(result.MaxDependencies)).
			WithMetadata("max_dependents", itoa(result.MaxDependents)).
			WithMetadata("exit", postDomTree.Entry)
	}

	return result, builder.Build()
}
