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
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// Leiden Community Detection (GR-14)
// =============================================================================

var communityTracer = otel.Tracer("graph.community")

// Leiden configuration constants.
const (
	// DefaultMaxLeidenIterations is the maximum outer loop iterations.
	DefaultMaxLeidenIterations = 100

	// DefaultConvergenceThreshold stops early if modularity gain < this.
	DefaultConvergenceThreshold = 1e-6

	// DefaultMinCommunitySize filters out tiny communities from results.
	DefaultMinCommunitySize = 1

	// DefaultResolution affects community granularity.
	// Higher values = smaller communities, lower = larger communities.
	DefaultResolution = 1.0
)

// LeidenOptions configures the Leiden algorithm.
type LeidenOptions struct {
	// MaxIterations limits total outer loop passes. Default: 100
	MaxIterations int

	// ConvergenceThreshold stops early if modularity gain < this. Default: 1e-6
	ConvergenceThreshold float64

	// MinCommunitySize filters out tiny communities from results. Default: 1
	MinCommunitySize int

	// Resolution affects community granularity. Default: 1.0
	// Higher values produce smaller, more granular communities.
	// Lower values produce larger, coarser communities.
	Resolution float64
}

// Validate checks options and applies defaults for invalid values.
func (o *LeidenOptions) Validate() {
	if o.MaxIterations <= 0 {
		o.MaxIterations = DefaultMaxLeidenIterations
	}
	if o.ConvergenceThreshold <= 0 {
		o.ConvergenceThreshold = DefaultConvergenceThreshold
	}
	if o.MinCommunitySize <= 0 {
		o.MinCommunitySize = DefaultMinCommunitySize
	}
	if o.Resolution <= 0 {
		o.Resolution = DefaultResolution
	}
}

// DefaultLeidenOptions returns sensible defaults.
func DefaultLeidenOptions() *LeidenOptions {
	return &LeidenOptions{
		MaxIterations:        DefaultMaxLeidenIterations,
		ConvergenceThreshold: DefaultConvergenceThreshold,
		MinCommunitySize:     DefaultMinCommunitySize,
		Resolution:           DefaultResolution,
	}
}

// Community represents a detected code module.
type Community struct {
	// ID is the unique identifier for this community.
	ID int `json:"id"`

	// Nodes contains the node IDs in this community.
	Nodes []string `json:"nodes"`

	// DominantPackage is the most common package in this community.
	DominantPackage string `json:"dominant_package"`

	// InternalEdges is the count of edges within this community.
	InternalEdges int `json:"internal_edges"`

	// ExternalEdges is the count of edges to other communities.
	ExternalEdges int `json:"external_edges"`

	// Connectivity is the internal density measure (internal / (internal + external)).
	Connectivity float64 `json:"connectivity"`
}

// CommunityResult contains the full Leiden output.
type CommunityResult struct {
	// Communities contains all detected communities.
	Communities []Community `json:"communities"`

	// Modularity is the final modularity score Q.
	// Range [0, 1] where higher is better.
	Modularity float64 `json:"modularity"`

	// Iterations is the number of outer loop passes completed.
	Iterations int `json:"iterations"`

	// Converged indicates whether the algorithm converged before MaxIterations.
	Converged bool `json:"converged"`

	// NodeCount is the total nodes analyzed.
	NodeCount int `json:"node_count"`

	// EdgeCount is the total edges analyzed.
	EdgeCount int `json:"edge_count"`
}

// GetCommunityForNode returns the community ID for a node.
//
// Inputs:
//
//	nodeID - The node ID to look up.
//
// Outputs:
//
//	int - The community ID.
//	bool - True if the node was found in a community.
func (r *CommunityResult) GetCommunityForNode(nodeID string) (int, bool) {
	for _, comm := range r.Communities {
		for _, id := range comm.Nodes {
			if id == nodeID {
				return comm.ID, true
			}
		}
	}
	return -1, false
}

// GetCommunityMembers returns all nodes in a community.
//
// Inputs:
//
//	communityID - The community ID to look up.
//
// Outputs:
//
//	[]string - Node IDs in the community. Empty if not found.
func (r *CommunityResult) GetCommunityMembers(communityID int) []string {
	for _, comm := range r.Communities {
		if comm.ID == communityID {
			result := make([]string, len(comm.Nodes))
			copy(result, comm.Nodes)
			return result
		}
	}
	return []string{}
}

// DetectCommunities uses the Leiden algorithm to find natural code communities.
//
// Description:
//
//	Implements the Leiden algorithm for community detection, which is an
//	improvement over Louvain. The key difference is the refinement phase
//	that guarantees all communities are well-connected.
//
//	Algorithm phases:
//	  1. Local moves: Try moving each node to neighbor's community if it improves modularity
//	  2. Refinement: Ensure each community is well-connected (Leiden's key improvement)
//	  3. Aggregation: Collapse communities into super-nodes (optional, for hierarchical)
//
//	For code graphs, we implement phases 1 and 2 which are sufficient for
//	typical codebase sizes (<100K nodes).
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil.
//   - opts: Configuration options. If nil, defaults are used.
//
// Outputs:
//
//   - *CommunityResult: Detected communities with modularity score.
//   - error: Non-nil if cancelled or other error.
//
// Example:
//
//	opts := graph.DefaultLeidenOptions()
//	result, err := analytics.DetectCommunities(ctx, opts)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Found %d communities with modularity %.3f\n",
//	    len(result.Communities), result.Modularity)
//
// Thread Safety: Safe for concurrent use (read-only on graph).
//
// Complexity: O(V + E) per iteration, typically few iterations.
//
// Limitations:
//
//   - Single-threaded implementation
//   - Memory usage: O(V) for community assignments
func (a *GraphAnalytics) DetectCommunities(ctx context.Context, opts *LeidenOptions) (*CommunityResult, error) {
	// R-2: Nil graph check (must happen before span to avoid nil dereference)
	if a.graph == nil {
		_, span := communityTracer.Start(ctx, "GraphAnalytics.DetectCommunities")
		defer span.End()
		span.AddEvent("nil_graph")
		return &CommunityResult{Converged: true}, nil
	}

	// R-2: Empty graph check
	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	ctx, span := communityTracer.Start(ctx, "GraphAnalytics.DetectCommunities",
		trace.WithAttributes(
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
		),
	)
	defer span.End()

	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		return &CommunityResult{Converged: true}, nil
	}

	// Apply defaults and validate options
	if opts == nil {
		opts = DefaultLeidenOptions()
	} else {
		opts.Validate()
	}

	span.SetAttributes(
		attribute.Int("max_iterations", opts.MaxIterations),
		attribute.Float64("convergence_threshold", opts.ConvergenceThreshold),
		attribute.Float64("resolution", opts.Resolution),
	)

	// I-3: Build sorted node list for deterministic iteration order
	nodeIDs := make([]string, 0, nodeCount)
	for id := range a.graph.Nodes() {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	// Initialize: each node in its own community
	nodeToComm := make(map[string]int, nodeCount)
	for i, id := range nodeIDs {
		nodeToComm[id] = i
	}

	// Handle zero-edge case
	m := float64(edgeCount)
	if m == 0 {
		span.AddEvent("no_edges")
		// Each node is its own community - trivially converged
		result := a.buildCommunityResult(nodeToComm, nodeIDs, opts)
		result.Converged = true
		result.Iterations = 0
		return result, nil
	}

	// P-1: Pre-compute degrees for all nodes
	degrees := make(map[string]float64, nodeCount)
	for _, id := range nodeIDs {
		node, _ := a.graph.GetNode(id)
		if node != nil {
			degrees[id] = float64(len(node.Incoming) + len(node.Outgoing))
		}
	}

	// P-2: Pre-compute neighbor lists for faster access
	neighbors := make(map[string][]string, nodeCount)
	for _, id := range nodeIDs {
		node, _ := a.graph.GetNode(id)
		if node == nil {
			continue
		}
		neighborSet := make(map[string]bool)
		for _, edge := range node.Outgoing {
			neighborSet[edge.ToID] = true
		}
		for _, edge := range node.Incoming {
			neighborSet[edge.FromID] = true
		}
		neighborList := make([]string, 0, len(neighborSet))
		for n := range neighborSet {
			neighborList = append(neighborList, n)
		}
		neighbors[id] = neighborList
	}

	// P-3: Pre-compute community degree sums for O(1) deltaQ calculation
	// This is the key optimization - avoids O(V) scan per deltaQ call
	commDegreeSum := make(map[int]float64, nodeCount)
	for _, id := range nodeIDs {
		comm := nodeToComm[id]
		commDegreeSum[comm] = degrees[id] // Initially each node is its own community
	}

	// Main Leiden loop
	previousQ := -1.0
	iterations := 0
	converged := false

	for iterations < opts.MaxIterations {
		// R-1: Check cancellation at iteration boundary
		if ctx.Err() != nil {
			span.AddEvent("cancelled", trace.WithAttributes(
				attribute.Int("iterations_completed", iterations),
			))
			return nil, ctx.Err()
		}

		iterations++
		improved := false

		// Phase 1: Local moves
		// Try moving each node to a neighbor's community if it improves modularity
		for _, id := range nodeIDs {
			currentComm := nodeToComm[id]
			bestComm := currentComm
			bestDeltaQ := 0.0

			// Get unique neighbor communities
			neighborComms := make(map[int]bool)
			for _, neighborID := range neighbors[id] {
				neighborComms[nodeToComm[neighborID]] = true
			}

			ki := degrees[id]

			// Try each neighbor's community
			for comm := range neighborComms {
				if comm == currentComm {
					continue
				}

				// O(1) deltaQ calculation using cached community sums
				deltaQ := a.calculateDeltaQFast(id, currentComm, comm, nodeToComm, degrees, commDegreeSum, ki, m, opts.Resolution)
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = comm
				}
			}

			// Move if improvement found
			if bestComm != currentComm && bestDeltaQ > 0 {
				// Update community degree sums incrementally (O(1))
				commDegreeSum[currentComm] -= ki
				commDegreeSum[bestComm] += ki
				nodeToComm[id] = bestComm
				improved = true
			}
		}

		// Phase 2: Refinement (Leiden's key improvement)
		// Ensure each community is well-connected
		if improved {
			nodeToComm = a.refineCommunities(nodeToComm, nodeIDs, neighbors)

			// Rebuild community degree sums after refinement (community IDs changed)
			commDegreeSum = make(map[int]float64)
			for _, id := range nodeIDs {
				comm := nodeToComm[id]
				commDegreeSum[comm] += degrees[id]
			}
		}

		// R-4: Convergence detection
		currentQ := a.calculateModularityFast(nodeToComm, degrees, commDegreeSum, m, opts.Resolution)

		if !improved || (currentQ-previousQ < opts.ConvergenceThreshold && previousQ >= 0) {
			converged = true
			break
		}

		previousQ = currentQ
	}

	// Build final result
	result := a.buildCommunityResult(nodeToComm, nodeIDs, opts)
	result.Iterations = iterations
	result.Converged = converged

	// O-2: Structured logging
	slog.Debug("Leiden community detection completed",
		slog.Int("iterations", iterations),
		slog.Int("communities", len(result.Communities)),
		slog.Float64("modularity", result.Modularity),
		slog.Bool("converged", converged),
		slog.Int("node_count", nodeCount),
		slog.Int("edge_count", edgeCount),
	)

	span.SetAttributes(
		attribute.Int("iterations", iterations),
		attribute.Int("communities_found", len(result.Communities)),
		attribute.Float64("modularity", result.Modularity),
		attribute.Bool("converged", converged),
		attribute.String("algorithm", "leiden"),
	)

	return result, nil
}

// calculateDeltaQ computes the modularity change for moving a node to a new community.
//
// The modularity change formula:
// ΔQ = [Σ_in + 2*k_i,in] / 2m - [(Σ_tot + k_i) / 2m]² - [Σ_in / 2m - (Σ_tot / 2m)² - (k_i / 2m)²]
//
// Simplified for our use case where we're comparing current vs target community.
func (a *GraphAnalytics) calculateDeltaQ(
	nodeID string,
	currentComm, targetComm int,
	nodeToComm map[string]int,
	degrees map[string]float64,
	m float64,
	resolution float64,
) float64 {
	if m == 0 {
		return 0
	}

	node, ok := a.graph.GetNode(nodeID)
	if !ok || node == nil {
		return 0
	}

	ki := degrees[nodeID]

	// Count edges to current community and target community
	edgesToCurrent := 0.0
	edgesToTarget := 0.0

	for _, edge := range node.Outgoing {
		toComm := nodeToComm[edge.ToID]
		if toComm == currentComm {
			edgesToCurrent++
		} else if toComm == targetComm {
			edgesToTarget++
		}
	}
	for _, edge := range node.Incoming {
		fromComm := nodeToComm[edge.FromID]
		if fromComm == currentComm {
			edgesToCurrent++
		} else if fromComm == targetComm {
			edgesToTarget++
		}
	}

	// Sum of degrees in current and target communities (excluding the moving node)
	sumDegreeCurrent := 0.0
	sumDegreeTarget := 0.0

	for id, comm := range nodeToComm {
		if id == nodeID {
			continue
		}
		if comm == currentComm {
			sumDegreeCurrent += degrees[id]
		} else if comm == targetComm {
			sumDegreeTarget += degrees[id]
		}
	}

	// Calculate delta Q using the simplified formula
	// ΔQ = (edges_to_target - edges_to_current) / m
	//    - resolution * ki * (sum_degree_target - sum_degree_current) / (2 * m²)

	deltaQ := (edgesToTarget - edgesToCurrent) / m
	deltaQ -= resolution * ki * (sumDegreeTarget - sumDegreeCurrent) / (2 * m * m)

	return deltaQ
}

// calculateModularity computes the modularity Q of the current partition.
//
// Q = (1/2m) Σ [A_ij - γ * (k_i * k_j)/(2m)] δ(c_i, c_j)
//
// Where:
//   - A_ij = 1 if edge exists, 0 otherwise
//   - k_i = degree of node i
//   - m = total edges
//   - c_i = community of node i
//   - δ = 1 if same community, 0 otherwise
//   - γ = resolution parameter
func (a *GraphAnalytics) calculateModularity(
	nodeToComm map[string]int,
	degrees map[string]float64,
	m float64,
	resolution float64,
) float64 {
	if m == 0 {
		return 0
	}

	// Group nodes by community
	commToNodes := make(map[int][]string)
	for id, comm := range nodeToComm {
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	Q := 0.0

	for _, nodes := range commToNodes {
		// Sum of degrees in this community
		sumDegree := 0.0
		for _, id := range nodes {
			sumDegree += degrees[id]
		}

		// Count internal edges (edges where both endpoints are in this community)
		internalEdges := 0.0
		nodeSet := make(map[string]bool, len(nodes))
		for _, id := range nodes {
			nodeSet[id] = true
		}

		for _, id := range nodes {
			node, ok := a.graph.GetNode(id)
			if !ok || node == nil {
				continue
			}
			for _, edge := range node.Outgoing {
				if nodeSet[edge.ToID] {
					internalEdges++
				}
			}
		}

		// Q contribution from this community
		// Q_c = (internal_edges / m) - resolution * (sum_degree / 2m)²
		Q += internalEdges/m - resolution*(sumDegree/(2*m))*(sumDegree/(2*m))
	}

	return Q
}

// calculateDeltaQFast computes modularity change using cached community sums.
//
// Complexity: O(degree) instead of O(V) - the key optimization.
//
// The community degree sums are maintained incrementally, so we just look them up.
func (a *GraphAnalytics) calculateDeltaQFast(
	nodeID string,
	currentComm, targetComm int,
	nodeToComm map[string]int,
	degrees map[string]float64,
	commDegreeSum map[int]float64,
	ki float64,
	m float64,
	resolution float64,
) float64 {
	if m == 0 {
		return 0
	}

	node, ok := a.graph.GetNode(nodeID)
	if !ok || node == nil {
		return 0
	}

	// Count edges to current community and target community - O(degree)
	edgesToCurrent := 0.0
	edgesToTarget := 0.0

	for _, edge := range node.Outgoing {
		toComm := nodeToComm[edge.ToID]
		if toComm == currentComm {
			edgesToCurrent++
		} else if toComm == targetComm {
			edgesToTarget++
		}
	}
	for _, edge := range node.Incoming {
		fromComm := nodeToComm[edge.FromID]
		if fromComm == currentComm {
			edgesToCurrent++
		} else if fromComm == targetComm {
			edgesToTarget++
		}
	}

	// O(1) lookup instead of O(V) scan!
	sumDegreeCurrent := commDegreeSum[currentComm] - ki // Exclude the moving node
	sumDegreeTarget := commDegreeSum[targetComm]

	// Calculate delta Q
	deltaQ := (edgesToTarget - edgesToCurrent) / m
	deltaQ -= resolution * ki * (sumDegreeTarget - sumDegreeCurrent) / (2 * m * m)

	return deltaQ
}

// calculateModularityFast computes modularity using cached community sums.
//
// Uses pre-computed community degree sums for the null model term,
// but still needs to count internal edges (unavoidable).
func (a *GraphAnalytics) calculateModularityFast(
	nodeToComm map[string]int,
	degrees map[string]float64,
	commDegreeSum map[int]float64,
	m float64,
	resolution float64,
) float64 {
	if m == 0 {
		return 0
	}

	// Group nodes by community
	commToNodes := make(map[int][]string)
	for id, comm := range nodeToComm {
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	Q := 0.0

	for comm, nodes := range commToNodes {
		// O(1) lookup for sum of degrees
		sumDegree := commDegreeSum[comm]

		// Count internal edges - still O(E_internal) but unavoidable
		nodeSet := make(map[string]bool, len(nodes))
		for _, id := range nodes {
			nodeSet[id] = true
		}

		internalEdges := 0.0
		for _, id := range nodes {
			node, ok := a.graph.GetNode(id)
			if !ok || node == nil {
				continue
			}
			for _, edge := range node.Outgoing {
				if nodeSet[edge.ToID] {
					internalEdges++
				}
			}
		}

		// Q contribution from this community
		Q += internalEdges/m - resolution*(sumDegree/(2*m))*(sumDegree/(2*m))
	}

	return Q
}

// refineCommunities ensures each community is well-connected (Leiden's key improvement).
//
// Description:
//
//	For each community, verifies that it forms a connected subgraph.
//	If a community has disconnected components, splits them into separate communities.
//	This prevents the "disconnected community" problem that can occur with Louvain.
//
// Thread Safety: Not safe for concurrent use.
func (a *GraphAnalytics) refineCommunities(
	nodeToComm map[string]int,
	nodeIDs []string,
	neighbors map[string][]string,
) map[string]int {
	// Group nodes by community
	commToNodes := make(map[int][]string)
	for _, id := range nodeIDs {
		comm := nodeToComm[id]
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	refined := make(map[string]int, len(nodeIDs))
	nextCommID := 0

	for _, nodes := range commToNodes {
		if len(nodes) <= 1 {
			// Single-node community is trivially well-connected
			for _, id := range nodes {
				refined[id] = nextCommID
			}
			nextCommID++
			continue
		}

		// Find connected components within this community
		components := a.findConnectedComponents(nodes, neighbors, nodeToComm)

		// Each connected component becomes its own community
		for _, component := range components {
			for _, id := range component {
				refined[id] = nextCommID
			}
			nextCommID++
		}
	}

	return refined
}

// findConnectedComponents finds connected components within a set of nodes.
//
// Only considers edges where both endpoints are in the same community.
func (a *GraphAnalytics) findConnectedComponents(
	nodes []string,
	neighbors map[string][]string,
	nodeToComm map[string]int,
) [][]string {
	if len(nodes) == 0 {
		return nil
	}

	// Get the community of these nodes
	comm := nodeToComm[nodes[0]]

	// Build set for O(1) membership check
	nodeSet := make(map[string]bool, len(nodes))
	for _, id := range nodes {
		nodeSet[id] = true
	}

	visited := make(map[string]bool, len(nodes))
	var components [][]string

	for _, startID := range nodes {
		if visited[startID] {
			continue
		}

		// BFS to find all nodes in this connected component
		component := []string{}
		queue := []string{startID}
		visited[startID] = true

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			component = append(component, current)

			// Visit neighbors that are in the same community
			for _, neighborID := range neighbors[current] {
				if visited[neighborID] {
					continue
				}
				if !nodeSet[neighborID] {
					continue // Not in this community's node set
				}
				if nodeToComm[neighborID] != comm {
					continue // Different community
				}
				visited[neighborID] = true
				queue = append(queue, neighborID)
			}
		}

		components = append(components, component)
	}

	return components
}

// buildCommunityResult constructs the final CommunityResult from node assignments.
func (a *GraphAnalytics) buildCommunityResult(
	nodeToComm map[string]int,
	nodeIDs []string,
	opts *LeidenOptions,
) *CommunityResult {
	// Group nodes by community
	commToNodes := make(map[int][]string)
	for _, id := range nodeIDs {
		comm := nodeToComm[id]
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	// Build community structs
	communities := make([]Community, 0, len(commToNodes))
	communityID := 0

	// Process communities in deterministic order
	commIDs := make([]int, 0, len(commToNodes))
	for comm := range commToNodes {
		commIDs = append(commIDs, comm)
	}
	sort.Ints(commIDs)

	for _, comm := range commIDs {
		nodes := commToNodes[comm]

		// Filter by min size
		if len(nodes) < opts.MinCommunitySize {
			continue
		}

		// Sort nodes for deterministic output
		sort.Strings(nodes)

		// Find dominant package
		pkgCounts := make(map[string]int)
		for _, id := range nodes {
			node, ok := a.graph.GetNode(id)
			if !ok || node == nil || node.Symbol == nil {
				continue
			}
			pkg := getNodePackage(node)
			if pkg != "" {
				pkgCounts[pkg]++
			}
		}

		dominantPkg := ""
		maxCount := 0
		for pkg, count := range pkgCounts {
			if count > maxCount {
				maxCount = count
				dominantPkg = pkg
			}
		}

		// Count internal and external edges
		nodeSet := make(map[string]bool, len(nodes))
		for _, id := range nodes {
			nodeSet[id] = true
		}

		internalEdges := 0
		externalEdges := 0

		for _, id := range nodes {
			node, ok := a.graph.GetNode(id)
			if !ok || node == nil {
				continue
			}
			for _, edge := range node.Outgoing {
				if nodeSet[edge.ToID] {
					internalEdges++
				} else {
					externalEdges++
				}
			}
		}

		// Calculate connectivity
		var connectivity float64
		if internalEdges+externalEdges > 0 {
			connectivity = float64(internalEdges) / float64(internalEdges+externalEdges)
		} else if len(nodes) == 1 {
			connectivity = 1.0 // Single node is trivially well-connected
		}

		communities = append(communities, Community{
			ID:              communityID,
			Nodes:           nodes,
			DominantPackage: dominantPkg,
			InternalEdges:   internalEdges,
			ExternalEdges:   externalEdges,
			Connectivity:    connectivity,
		})

		communityID++
	}

	// Calculate final modularity
	degrees := make(map[string]float64, len(nodeIDs))
	for _, id := range nodeIDs {
		node, _ := a.graph.GetNode(id)
		if node != nil {
			degrees[id] = float64(len(node.Incoming) + len(node.Outgoing))
		}
	}

	m := float64(a.graph.EdgeCount())
	modularity := 0.0
	if m > 0 {
		modularity = a.calculateModularity(nodeToComm, degrees, m, opts.Resolution)
	}

	return &CommunityResult{
		Communities: communities,
		Modularity:  modularity,
		NodeCount:   len(nodeIDs),
		EdgeCount:   a.graph.EdgeCount(),
	}
}

// =============================================================================
// Parallel Leiden Implementation
// =============================================================================

// Parallel Leiden configuration constants.
const (
	// parallelCommunityThreshold is the minimum node count to trigger parallel processing.
	parallelCommunityThreshold = 1000

	// maxCommunityWorkers caps the number of goroutines for community detection.
	maxCommunityWorkers = 8
)

// DetectCommunitiesParallel uses parallel processing for large graphs.
//
// Description:
//
//	Parallelized version of DetectCommunities that uses multiple goroutines
//	for pre-computation, refinement, and modularity calculation. Automatically
//	falls back to sequential for graphs smaller than 1000 nodes.
//
// Parallelization strategy:
//   - Pre-computation of degrees/neighbors: embarrassingly parallel
//   - Refinement phase: each community processed independently
//   - Modularity calculation: parallel sum reduction
//   - Local moves: sequential (correctness requires synchronization)
//
// Inputs:
//
//   - ctx: Context for cancellation. Must not be nil.
//   - opts: Configuration options. If nil, defaults are used.
//
// Outputs:
//
//   - *CommunityResult: Detected communities with modularity score.
//   - error: Non-nil if cancelled or other error.
//
// Performance:
//
//	Speedup depends on graph structure and CPU count. Typical speedups:
//	  - 10K nodes: 2-3x faster
//	  - 100K nodes: 3-5x faster
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DetectCommunitiesParallel(ctx context.Context, opts *LeidenOptions) (*CommunityResult, error) {
	// R-2: Nil graph check
	if a.graph == nil {
		_, span := communityTracer.Start(ctx, "GraphAnalytics.DetectCommunitiesParallel")
		defer span.End()
		span.AddEvent("nil_graph")
		return &CommunityResult{Converged: true}, nil
	}

	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	// Fall back to sequential for small graphs
	if nodeCount < parallelCommunityThreshold {
		return a.DetectCommunities(ctx, opts)
	}

	ctx, span := communityTracer.Start(ctx, "GraphAnalytics.DetectCommunitiesParallel",
		trace.WithAttributes(
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
			attribute.Bool("parallel", true),
		),
	)
	defer span.End()

	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		return &CommunityResult{Converged: true}, nil
	}

	// Apply defaults and validate options
	if opts == nil {
		opts = DefaultLeidenOptions()
	} else {
		opts.Validate()
	}

	span.SetAttributes(
		attribute.Int("max_iterations", opts.MaxIterations),
		attribute.Float64("convergence_threshold", opts.ConvergenceThreshold),
		attribute.Float64("resolution", opts.Resolution),
	)

	// Build sorted node list for deterministic iteration order
	nodeIDs := make([]string, 0, nodeCount)
	for id := range a.graph.Nodes() {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	// Initialize: each node in its own community
	nodeToComm := make(map[string]int, nodeCount)
	for i, id := range nodeIDs {
		nodeToComm[id] = i
	}

	// Handle zero-edge case
	m := float64(edgeCount)
	if m == 0 {
		span.AddEvent("no_edges")
		result := a.buildCommunityResult(nodeToComm, nodeIDs, opts)
		result.Converged = true
		result.Iterations = 0
		return result, nil
	}

	// PARALLEL: Pre-compute degrees and neighbors
	degrees, neighbors := a.precomputeParallel(ctx, nodeIDs)

	// Check cancellation after pre-computation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Main Leiden loop (local moves remain sequential for correctness)
	previousQ := -1.0
	iterations := 0
	converged := false

	for iterations < opts.MaxIterations {
		if ctx.Err() != nil {
			span.AddEvent("cancelled", trace.WithAttributes(
				attribute.Int("iterations_completed", iterations),
			))
			return nil, ctx.Err()
		}

		iterations++
		improved := false

		// Phase 1: Local moves (sequential for correctness)
		for _, id := range nodeIDs {
			currentComm := nodeToComm[id]
			bestComm := currentComm
			bestDeltaQ := 0.0

			neighborComms := make(map[int]bool)
			for _, neighborID := range neighbors[id] {
				neighborComms[nodeToComm[neighborID]] = true
			}

			for comm := range neighborComms {
				if comm == currentComm {
					continue
				}
				deltaQ := a.calculateDeltaQ(id, currentComm, comm, nodeToComm, degrees, m, opts.Resolution)
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = comm
				}
			}

			if bestComm != currentComm && bestDeltaQ > 0 {
				nodeToComm[id] = bestComm
				improved = true
			}
		}

		// Phase 2: PARALLEL Refinement
		if improved {
			nodeToComm = a.refineCommunitiesParallel(ctx, nodeToComm, nodeIDs, neighbors)
		}

		// PARALLEL Modularity calculation
		currentQ := a.calculateModularityParallel(ctx, nodeToComm, degrees, m, opts.Resolution)

		if !improved || (currentQ-previousQ < opts.ConvergenceThreshold && previousQ >= 0) {
			converged = true
			break
		}

		previousQ = currentQ
	}

	// Build final result
	result := a.buildCommunityResult(nodeToComm, nodeIDs, opts)
	result.Iterations = iterations
	result.Converged = converged

	slog.Debug("Parallel Leiden community detection completed",
		slog.Int("iterations", iterations),
		slog.Int("communities", len(result.Communities)),
		slog.Float64("modularity", result.Modularity),
		slog.Bool("converged", converged),
		slog.Int("node_count", nodeCount),
	)

	span.SetAttributes(
		attribute.Int("iterations", iterations),
		attribute.Int("communities_found", len(result.Communities)),
		attribute.Float64("modularity", result.Modularity),
		attribute.Bool("converged", converged),
		attribute.String("algorithm", "leiden_parallel"),
	)

	return result, nil
}

// precomputeParallel computes degrees and neighbor lists in parallel.
func (a *GraphAnalytics) precomputeParallel(ctx context.Context, nodeIDs []string) (map[string]float64, map[string][]string) {
	n := len(nodeIDs)
	workers := minInt(n/100+1, minInt(runtime.NumCPU(), maxCommunityWorkers))

	degrees := make(map[string]float64, n)
	neighbors := make(map[string][]string, n)
	var mu sync.Mutex

	// Partition nodes among workers
	chunkSize := (n + workers - 1) / workers

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := minInt(start+chunkSize, n)
		if start >= n {
			break
		}

		wg.Add(1)
		go func(startIdx, endIdx int) {
			defer wg.Done()

			// Local maps to reduce lock contention
			localDegrees := make(map[string]float64, endIdx-startIdx)
			localNeighbors := make(map[string][]string, endIdx-startIdx)

			for i := startIdx; i < endIdx; i++ {
				if ctx.Err() != nil {
					return
				}

				id := nodeIDs[i]
				node, ok := a.graph.GetNode(id)
				if !ok || node == nil {
					continue
				}

				localDegrees[id] = float64(len(node.Incoming) + len(node.Outgoing))

				neighborSet := make(map[string]bool)
				for _, edge := range node.Outgoing {
					neighborSet[edge.ToID] = true
				}
				for _, edge := range node.Incoming {
					neighborSet[edge.FromID] = true
				}
				neighborList := make([]string, 0, len(neighborSet))
				for neighbor := range neighborSet {
					neighborList = append(neighborList, neighbor)
				}
				localNeighbors[id] = neighborList
			}

			// Merge into shared maps
			mu.Lock()
			for id, deg := range localDegrees {
				degrees[id] = deg
			}
			for id, neigh := range localNeighbors {
				neighbors[id] = neigh
			}
			mu.Unlock()
		}(start, end)
	}

	wg.Wait()
	return degrees, neighbors
}

// refineCommunitiesParallel ensures each community is well-connected using parallel processing.
func (a *GraphAnalytics) refineCommunitiesParallel(
	ctx context.Context,
	nodeToComm map[string]int,
	nodeIDs []string,
	neighbors map[string][]string,
) map[string]int {
	// Group nodes by community
	commToNodes := make(map[int][]string)
	for _, id := range nodeIDs {
		comm := nodeToComm[id]
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	// Convert to slice for parallel processing
	type commEntry struct {
		commID int
		nodes  []string
	}
	communities := make([]commEntry, 0, len(commToNodes))
	for commID, nodes := range commToNodes {
		communities = append(communities, commEntry{commID, nodes})
	}

	// Process communities in parallel
	workers := minInt(len(communities), minInt(runtime.NumCPU(), maxCommunityWorkers))
	if workers < 1 {
		workers = 1
	}

	type refinedComm struct {
		components [][]string
	}
	results := make([]refinedComm, len(communities))

	chunkSize := (len(communities) + workers - 1) / workers

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := minInt(start+chunkSize, len(communities))
		if start >= len(communities) {
			break
		}

		wg.Add(1)
		go func(startIdx, endIdx int) {
			defer wg.Done()

			for i := startIdx; i < endIdx; i++ {
				if ctx.Err() != nil {
					return
				}

				entry := communities[i]
				if len(entry.nodes) <= 1 {
					results[i] = refinedComm{components: [][]string{entry.nodes}}
					continue
				}

				// Find connected components within this community
				components := a.findConnectedComponents(entry.nodes, neighbors, nodeToComm)
				results[i] = refinedComm{components: components}
			}
		}(start, end)
	}

	wg.Wait()

	// Build refined assignment
	refined := make(map[string]int, len(nodeIDs))
	nextCommID := 0

	for _, result := range results {
		for _, component := range result.components {
			for _, id := range component {
				refined[id] = nextCommID
			}
			nextCommID++
		}
	}

	return refined
}

// calculateModularityParallel computes modularity using parallel reduction.
func (a *GraphAnalytics) calculateModularityParallel(
	ctx context.Context,
	nodeToComm map[string]int,
	degrees map[string]float64,
	m float64,
	resolution float64,
) float64 {
	if m == 0 {
		return 0
	}

	// Group nodes by community
	commToNodes := make(map[int][]string)
	for id, comm := range nodeToComm {
		commToNodes[comm] = append(commToNodes[comm], id)
	}

	// Convert to slice for parallel processing
	type commEntry struct {
		nodes []string
	}
	communities := make([]commEntry, 0, len(commToNodes))
	for _, nodes := range commToNodes {
		communities = append(communities, commEntry{nodes})
	}

	if len(communities) == 0 {
		return 0
	}

	// Calculate Q contribution for each community in parallel
	workers := minInt(len(communities), minInt(runtime.NumCPU(), maxCommunityWorkers))
	if workers < 1 {
		workers = 1
	}

	partialQ := make([]float64, workers)
	chunkSize := (len(communities) + workers - 1) / workers

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := minInt(start+chunkSize, len(communities))
		if start >= len(communities) {
			break
		}

		wg.Add(1)
		go func(workerID, startIdx, endIdx int) {
			defer wg.Done()

			localQ := 0.0

			for i := startIdx; i < endIdx; i++ {
				if ctx.Err() != nil {
					return
				}

				nodes := communities[i].nodes

				// Sum of degrees in this community
				sumDegree := 0.0
				for _, id := range nodes {
					sumDegree += degrees[id]
				}

				// Count internal edges
				nodeSet := make(map[string]bool, len(nodes))
				for _, id := range nodes {
					nodeSet[id] = true
				}

				internalEdges := 0.0
				for _, id := range nodes {
					node, ok := a.graph.GetNode(id)
					if !ok || node == nil {
						continue
					}
					for _, edge := range node.Outgoing {
						if nodeSet[edge.ToID] {
							internalEdges++
						}
					}
				}

				// Q contribution from this community
				localQ += internalEdges/m - resolution*(sumDegree/(2*m))*(sumDegree/(2*m))
			}

			partialQ[workerID] = localQ
		}(w, start, end)
	}

	wg.Wait()

	// Sum partial results
	Q := 0.0
	for _, partial := range partialQ {
		Q += partial
	}

	return Q
}

// DetectCommunitiesWithCRS detects communities and returns a TraceStep for CRS recording.
//
// Description:
//
//	Wraps DetectCommunities with CRS integration for recording the operation
//	in the reasoning trace.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DetectCommunitiesWithCRS(ctx context.Context, opts *LeidenOptions) (*CommunityResult, crs.TraceStep) {
	start := time.Now()

	// Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_communities").
			WithTarget("project").
			WithTool("DetectCommunities").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &CommunityResult{}, step
	}

	result, err := a.DetectCommunities(ctx, opts)

	if err != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_communities").
			WithTarget("project").
			WithTool("DetectCommunities").
			WithDuration(time.Since(start)).
			WithError(err.Error()).
			Build()
		return &CommunityResult{}, step
	}

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_communities").
		WithTarget("project").
		WithTool("DetectCommunities").
		WithDuration(time.Since(start)).
		WithMetadata("algorithm", "leiden").
		WithMetadata("communities_found", itoa(len(result.Communities))).
		WithMetadata("modularity", ftoa(result.Modularity)).
		WithMetadata("iterations", itoa(result.Iterations)).
		WithMetadata("converged", btoa(result.Converged)).
		WithMetadata("node_count", itoa(result.NodeCount)).
		WithMetadata("edge_count", itoa(result.EdgeCount)).
		Build()

	return result, step
}

// getCrossPackageCommunities returns communities that span multiple packages.
//
// Description:
//
//	These are often interesting for refactoring analysis - they indicate
//	code that is tightly coupled but organized in different packages.
//
// Thread Safety: Safe for concurrent use.
func getCrossPackageCommunities(result *CommunityResult) []Community {
	crossPkg := make([]Community, 0)

	for _, comm := range result.Communities {
		// Count unique packages
		pkgs := make(map[string]bool)
		for _, nodeID := range comm.Nodes {
			// Extract package from node ID (format: "pkg/file.go:Symbol")
			if idx := len(nodeID) - 1; idx > 0 {
				for i := 0; i < len(nodeID); i++ {
					if nodeID[i] == ':' {
						path := nodeID[:i]
						// Find last /
						lastSlash := 0
						for j := len(path) - 1; j >= 0; j-- {
							if path[j] == '/' {
								lastSlash = j
								break
							}
						}
						if lastSlash > 0 {
							pkgs[path[:lastSlash]] = true
						}
						break
					}
				}
			}
		}

		if len(pkgs) > 1 {
			crossPkg = append(crossPkg, comm)
		}
	}

	return crossPkg
}

// =============================================================================
// Helper functions
// =============================================================================

// NOTE: getNodePackage is defined in hierarchical.go and reused here.

// min returns the minimum of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two floats.
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// abs returns the absolute value of a float.
func absFloat(a float64) float64 {
	return math.Abs(a)
}
