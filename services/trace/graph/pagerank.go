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
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// PageRank Algorithm (GR-12)
// =============================================================================

var pageRankTracer = otel.Tracer("graph.pagerank")

// PageRank configuration constants.
const (
	// DefaultDampingFactor is the probability of following a link (vs random jump).
	// Standard value from the original PageRank paper.
	DefaultDampingFactor = 0.85

	// DefaultMaxIterations is the maximum iterations before stopping.
	DefaultMaxIterations = 100

	// DefaultConvergence is the threshold for convergence detection.
	// Power iteration stops when max score change < this value.
	DefaultConvergence = 1e-6

	// SmallGraphThreshold is the node count below which we skip convergence checks.
	SmallGraphThreshold = 10
)

// PageRankOptions configures the PageRank algorithm.
type PageRankOptions struct {
	// DampingFactor is the probability of following a link (vs random jump).
	// Must be in [0, 1]. Default: 0.85
	DampingFactor float64

	// MaxIterations is the maximum iterations before stopping.
	// Must be > 0. Default: 100
	MaxIterations int

	// Convergence is the threshold for convergence detection.
	// Must be > 0. Default: 1e-6
	Convergence float64
}

// Validate checks options and applies defaults for invalid values.
func (o *PageRankOptions) Validate() {
	if o.DampingFactor < 0 || o.DampingFactor > 1 {
		o.DampingFactor = DefaultDampingFactor
	}
	if o.MaxIterations <= 0 {
		o.MaxIterations = DefaultMaxIterations
	}
	if o.Convergence <= 0 {
		o.Convergence = DefaultConvergence
	}
}

// DefaultPageRankOptions returns sensible defaults.
func DefaultPageRankOptions() *PageRankOptions {
	return &PageRankOptions{
		DampingFactor: DefaultDampingFactor,
		MaxIterations: DefaultMaxIterations,
		Convergence:   DefaultConvergence,
	}
}

// PageRankResult contains the output of PageRank computation.
type PageRankResult struct {
	// Scores maps nodeID to PageRank score.
	// Scores sum to approximately 1.0.
	Scores map[string]float64

	// Iterations is the actual number of iterations performed.
	Iterations int

	// Converged indicates whether the algorithm converged before MaxIterations.
	Converged bool

	// MaxDiff is the final maximum score difference (useful for debugging).
	MaxDiff float64
}

// PageRankNode represents a node with its PageRank score and rank.
type PageRankNode struct {
	// Node is the graph node.
	Node *Node

	// Score is the PageRank score.
	Score float64

	// Rank is the position in the ranking (1-indexed).
	Rank int

	// DegreeScore is the simple degree-based score for comparison.
	// Computed as: inDegree*2 + outDegree
	DegreeScore int
}

// PageRank computes PageRank scores for all nodes in the graph.
//
// Description:
//
//	Uses power iteration to compute the PageRank score of each node,
//	which represents its importance based on the importance of nodes
//	linking to it (transitive importance).
//
//	The algorithm handles sink nodes (nodes with no outgoing edges) by
//	redistributing their PageRank evenly across all nodes, preventing
//	rank "leakage" from the graph.
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil.
//   - opts: Configuration options. If nil, defaults are used.
//
// Outputs:
//
//   - *PageRankResult: Scores for all nodes, iteration count, convergence status.
//     Returns empty result if graph is nil or empty.
//
// Example:
//
//	opts := graph.DefaultPageRankOptions()
//	result := analytics.PageRank(ctx, opts)
//	if result.Converged {
//	    fmt.Printf("Converged in %d iterations\n", result.Iterations)
//	}
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(k × E) where k = iterations to converge (~20 typical).
//
// Limitations:
//
//   - Single-threaded implementation
//   - Memory usage: 2 × V for score maps
func (a *GraphAnalytics) PageRank(ctx context.Context, opts *PageRankOptions) *PageRankResult {
	ctx, span := pageRankTracer.Start(ctx, "GraphAnalytics.PageRank",
		trace.WithAttributes(
			attribute.Int("node_count", a.graph.NodeCount()),
			attribute.Int("edge_count", a.graph.EdgeCount()),
		),
	)
	defer span.End()

	// R2: Nil graph check
	if a.graph == nil {
		span.AddEvent("nil_graph")
		return &PageRankResult{
			Scores:    make(map[string]float64),
			Converged: true,
		}
	}

	// R3: Empty graph check
	N := float64(a.graph.NodeCount())
	if N == 0 {
		span.AddEvent("empty_graph")
		return &PageRankResult{
			Scores:    make(map[string]float64),
			Converged: true,
		}
	}

	// Apply defaults and validate options
	if opts == nil {
		opts = DefaultPageRankOptions()
	} else {
		opts.Validate()
	}

	span.SetAttributes(
		attribute.Float64("damping_factor", opts.DampingFactor),
		attribute.Int("max_iterations", opts.MaxIterations),
		attribute.Float64("convergence_threshold", opts.Convergence),
	)

	nodes := a.graph.Nodes()
	d := opts.DampingFactor

	// P1: Pre-allocate two maps and swap instead of reallocating
	scores := make(map[string]float64, int(N))
	newScores := make(map[string]float64, int(N))

	// Initialize scores uniformly
	initial := 1.0 / N
	for id := range nodes {
		scores[id] = initial
	}

	// I1: Identify sink nodes (no outgoing edges) for special handling
	sinkNodes := make([]string, 0)
	outDegree := make(map[string]int, int(N)) // P2: Cache outDegree
	for id, node := range nodes {
		deg := len(node.Outgoing)
		outDegree[id] = deg
		if deg == 0 {
			sinkNodes = append(sinkNodes, id)
		}
	}

	span.SetAttributes(attribute.Int("sink_node_count", len(sinkNodes)))

	// Power iteration
	var iterations int
	var converged bool
	var maxDiff float64

	for iter := 0; iter < opts.MaxIterations; iter++ {
		// R1: Check context cancellation
		if ctx.Err() != nil {
			span.AddEvent("cancelled", trace.WithAttributes(
				attribute.Int("iterations_completed", iter),
			))
			return &PageRankResult{
				Scores:     scores,
				Iterations: iter,
				Converged:  false,
				MaxDiff:    maxDiff,
			}
		}

		maxDiff = 0.0

		// I1: Calculate sink contribution (redistribute evenly)
		sinkContribution := 0.0
		for _, sinkID := range sinkNodes {
			sinkContribution += scores[sinkID]
		}
		sinkContribution = d * sinkContribution / N

		// Compute new scores
		for id, node := range nodes {
			// Base score (random jump) + sink redistribution
			newScore := (1-d)/N + sinkContribution

			// Contribution from incoming edges
			for _, edge := range node.Incoming {
				fromOutDegree := outDegree[edge.FromID]
				if fromOutDegree > 0 {
					newScore += d * scores[edge.FromID] / float64(fromOutDegree)
				}
			}

			newScores[id] = newScore

			// Track convergence
			diff := math.Abs(newScore - scores[id])
			if diff > maxDiff {
				maxDiff = diff
			}
		}

		// P1: Swap maps instead of reallocating
		scores, newScores = newScores, scores

		iterations = iter + 1

		// P3: Skip convergence check for small graphs (always converge fast)
		if int(N) < SmallGraphThreshold || maxDiff < opts.Convergence {
			converged = true
			break
		}
	}

	// O2: Log convergence info
	slog.Debug("PageRank completed",
		slog.Int("iterations", iterations),
		slog.Bool("converged", converged),
		slog.Float64("max_diff", maxDiff),
		slog.Int("node_count", int(N)),
	)

	span.SetAttributes(
		attribute.Int("iterations", iterations),
		attribute.Bool("converged", converged),
		attribute.Float64("max_diff", maxDiff),
	)

	return &PageRankResult{
		Scores:     scores,
		Iterations: iterations,
		Converged:  converged,
		MaxDiff:    maxDiff,
	}
}

// PageRankWithCRS computes PageRank and returns a TraceStep for CRS recording.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PageRankWithCRS(ctx context.Context, opts *PageRankOptions) (*PageRankResult, crs.TraceStep) {
	start := time.Now()

	// R1: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_pagerank").
			WithTarget("project").
			WithTool("PageRank").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &PageRankResult{Scores: make(map[string]float64)}, step
	}

	// Apply defaults if nil
	if opts == nil {
		opts = DefaultPageRankOptions()
	}

	result := a.PageRank(ctx, opts)

	// C2: Include comprehensive metadata
	step := crs.NewTraceStepBuilder().
		WithAction("analytics_pagerank").
		WithTarget("project").
		WithTool("PageRank").
		WithDuration(time.Since(start)).
		WithMetadata("iterations", itoa(result.Iterations)).
		WithMetadata("converged", btoa(result.Converged)).
		WithMetadata("node_count", itoa(a.graph.NodeCount())).
		WithMetadata("damping_factor", ftoa(opts.DampingFactor)).
		Build()

	return result, step
}

// PageRankTop returns the top-k nodes by PageRank score.
//
// Description:
//
//	Computes PageRank for all nodes and returns the top-k ranked nodes.
//	Each result includes the PageRank score and a comparison degree-based
//	score for context.
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil.
//   - k: Number of top nodes to return. Must be > 0.
//   - opts: Configuration options. If nil, defaults are used.
//
// Outputs:
//
//	[]PageRankNode: Top-k nodes sorted by PageRank score descending.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PageRankTop(ctx context.Context, k int, opts *PageRankOptions) []PageRankNode {
	ctx, span := pageRankTracer.Start(ctx, "GraphAnalytics.PageRankTop",
		trace.WithAttributes(attribute.Int("k", k)),
	)
	defer span.End()

	if k <= 0 {
		return []PageRankNode{}
	}

	result := a.PageRank(ctx, opts)

	// Build scored node list
	type scoredNode struct {
		ID    string
		Score float64
	}
	nodeList := make([]scoredNode, 0, len(result.Scores))
	for id, score := range result.Scores {
		nodeList = append(nodeList, scoredNode{ID: id, Score: score})
	}

	// I3: Sort by score descending with tie-breaking by ID
	sort.Slice(nodeList, func(i, j int) bool {
		if nodeList[i].Score != nodeList[j].Score {
			return nodeList[i].Score > nodeList[j].Score
		}
		return nodeList[i].ID < nodeList[j].ID
	})

	// Return top-k
	if k > len(nodeList) {
		k = len(nodeList)
	}

	topK := make([]PageRankNode, k)
	for i := 0; i < k; i++ {
		node, _ := a.graph.GetNode(nodeList[i].ID)
		degreeScore := 0
		if node != nil {
			degreeScore = len(node.Incoming)*2 + len(node.Outgoing)
		}

		topK[i] = PageRankNode{
			Node:        node,
			Score:       nodeList[i].Score,
			Rank:        i + 1,
			DegreeScore: degreeScore,
		}
	}

	span.SetAttributes(
		attribute.Int("returned", len(topK)),
		attribute.Bool("converged", result.Converged),
		attribute.Int("iterations", result.Iterations),
	)

	return topK
}

// PageRankTopWithCRS returns top-k nodes with a TraceStep for CRS recording.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PageRankTopWithCRS(ctx context.Context, k int, opts *PageRankOptions) ([]PageRankNode, crs.TraceStep) {
	start := time.Now()

	// R1: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_pagerank_top").
			WithTarget("project").
			WithTool("PageRankTop").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return []PageRankNode{}, step
	}

	topK := a.PageRankTop(ctx, k, opts)

	// Build metadata
	topScore := 0.0
	if len(topK) > 0 {
		topScore = topK[0].Score
	}

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_pagerank_top").
		WithTarget("project").
		WithTool("PageRankTop").
		WithDuration(time.Since(start)).
		WithMetadata("requested", itoa(k)).
		WithMetadata("returned", itoa(len(topK))).
		WithMetadata("top_score", ftoa(topScore)).
		WithMetadata("total_nodes", itoa(a.graph.NodeCount())).
		Build()

	return topK, step
}

// =============================================================================
// Helper functions
// =============================================================================

// btoa converts a bool to string.
func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ftoa converts a float64 to string with reasonable precision.
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}
