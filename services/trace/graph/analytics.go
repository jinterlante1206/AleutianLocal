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
	"sort"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// =============================================================================
// Dominator Tree Cache
// =============================================================================

// DominatorTreeCache provides session-level caching for dominator trees.
//
// Description:
//
//	Caches a single dominator tree to avoid recomputation when the same
//	entry point is requested multiple times. The cache is automatically
//	invalidated when:
//	- A different entry point is requested
//	- The underlying graph changes (detected via BuiltAtMilli)
//
// Thread Safety:
//
//	DominatorTreeCache is safe for concurrent use via RWMutex.
type DominatorTreeCache struct {
	mu           sync.RWMutex
	entry        string         // Entry point used for computation
	tree         *DominatorTree // Cached dominator tree
	graphVersion int64          // Graph BuiltAtMilli when computed
	computedAt   int64          // Unix milliseconds when cache was populated
}

// =============================================================================
// GraphAnalytics
// =============================================================================

// GraphAnalytics provides analytical queries over the code graph.
//
// Description:
//
//	GraphAnalytics operates on a HierarchicalGraph to provide insights about
//	code structure, including:
//	- Hotspot detection (most-connected nodes)
//	- Dead code detection (unreachable symbols)
//	- Cyclic dependency detection (using Tarjan's SCC)
//	- Package coupling metrics
//
// Thread Safety:
//
//	GraphAnalytics is safe for concurrent use (read-only queries).
//
// Performance:
//
//	| Operation | Complexity |
//	|-----------|------------|
//	| HotSpots | O(V log k) for top-k |
//	| DeadCode | O(V) |
//	| CyclicDependencies | O(V + E) |
//	| PackageCoupling | O(P) where P = packages |
type GraphAnalytics struct {
	graph *HierarchicalGraph

	// domTreeCache caches the dominator tree for a given entry point.
	// Avoids recomputation when multiple tools request the same dominator tree.
	domTreeCache *DominatorTreeCache

	// postDomTreeCache caches the post-dominator tree for a given exit point.
	// Avoids recomputation when multiple tools request the same post-dominator tree.
	postDomTreeCache *DominatorTreeCache
}

// NewGraphAnalytics creates a new analytics instance for the given graph.
//
// Inputs:
//
//	graph - The hierarchical graph to analyze. Must not be nil.
//
// Outputs:
//
//	*GraphAnalytics - The analytics instance. Never nil if graph is valid.
func NewGraphAnalytics(graph *HierarchicalGraph) *GraphAnalytics {
	if graph == nil {
		return nil
	}
	return &GraphAnalytics{
		graph:            graph,
		domTreeCache:     &DominatorTreeCache{},
		postDomTreeCache: &DominatorTreeCache{},
	}
}

// =============================================================================
// Hotspot Detection
// =============================================================================

// HotspotNode represents a node with its connectivity score.
type HotspotNode struct {
	// Node is the graph node.
	Node *Node

	// Score is the connectivity score (higher = more connected).
	// Computed as: inDegree*2 + outDegree*1
	Score int

	// InDegree is the number of incoming edges.
	InDegree int

	// OutDegree is the number of outgoing edges.
	OutDegree int
}

// HotSpots returns the top N most-connected nodes in the graph.
//
// Description:
//
//	Finds nodes with the highest connectivity score, where:
//	score = inDegree * 2 + outDegree * 1
//
//	Incoming edges are weighted higher because being called frequently
//	indicates higher impact on the codebase.
//
// Inputs:
//
//	top - Maximum number of hotspots to return. Must be > 0.
//
// Outputs:
//
//	[]HotspotNode - Top N nodes sorted by score descending.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) HotSpots(top int) []HotspotNode {
	if top <= 0 {
		return []HotspotNode{}
	}

	// Collect all nodes with scores
	hotspots := make([]HotspotNode, 0, a.graph.NodeCount())
	for _, node := range a.graph.Nodes() {
		if node.Symbol == nil {
			continue
		}

		// Skip external/placeholder nodes
		if node.Symbol.Kind == ast.SymbolKindExternal {
			continue
		}

		hs := HotspotNode{
			Node:      node,
			InDegree:  len(node.Incoming),
			OutDegree: len(node.Outgoing),
		}
		hs.Score = hs.InDegree*2 + hs.OutDegree

		hotspots = append(hotspots, hs)
	}

	// Sort by score descending
	sort.Slice(hotspots, func(i, j int) bool {
		if hotspots[i].Score != hotspots[j].Score {
			return hotspots[i].Score > hotspots[j].Score
		}
		// Tie-breaker: sort by ID for stability
		return hotspots[i].Node.ID < hotspots[j].Node.ID
	})

	// Return top N
	if top > len(hotspots) {
		top = len(hotspots)
	}
	return hotspots[:top]
}

// HotSpotsWithCRS returns hotspots with a TraceStep for CRS recording.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) HotSpotsWithCRS(ctx context.Context, top int) ([]HotspotNode, crs.TraceStep) {
	start := time.Now()

	// CR-5 FIX: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_hotspots").
			WithTarget("project").
			WithTool("HotSpots").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return []HotspotNode{}, step
	}

	hotspots := a.HotSpots(top)

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_hotspots").
		WithTarget("project").
		WithTool("HotSpots").
		WithDuration(time.Since(start)).
		WithMetadata("requested", itoa(top)).
		WithMetadata("returned", itoa(len(hotspots))).
		WithMetadata("total_nodes", itoa(a.graph.NodeCount())).
		Build()

	return hotspots, step
}

// =============================================================================
// Dead Code Detection
// =============================================================================

// DeadCodeNode represents a potentially unused symbol.
type DeadCodeNode struct {
	// Node is the graph node.
	Node *Node

	// Reason explains why this was flagged as dead code.
	Reason string
}

// DeadCode finds symbols with no incoming edges (potential unused code).
//
// Description:
//
//	Returns nodes that have no callers/references, excluding:
//	- Entry points (main, init, Test*, Benchmark*)
//	- Exported symbols in main package
//	- External/placeholder nodes
//	- Interface methods (implementations satisfy interfaces)
//
// Outputs:
//
//	[]DeadCodeNode - Potentially unused symbols with reasons.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DeadCode() []DeadCodeNode {
	result := make([]DeadCodeNode, 0)

	for _, node := range a.graph.Nodes() {
		if node.Symbol == nil {
			continue
		}

		sym := node.Symbol

		// Skip external/placeholder nodes
		if sym.Kind == ast.SymbolKindExternal {
			continue
		}

		// Skip nodes with incoming edges (they're used)
		if len(node.Incoming) > 0 {
			continue
		}

		// Skip entry points
		if isEntryPoint(sym) {
			continue
		}

		// Skip interface methods
		if sym.Kind == ast.SymbolKindMethod {
			// Methods might implement interfaces - harder to detect
			// Be conservative and skip all methods
			continue
		}

		// This node has no incoming edges and isn't an entry point
		reason := "no callers or references"
		if sym.Exported {
			reason = "exported but not referenced internally"
		}

		result = append(result, DeadCodeNode{
			Node:   node,
			Reason: reason,
		})
	}

	// Sort by file path and name for consistent output
	sort.Slice(result, func(i, j int) bool {
		ni, nj := result[i].Node.Symbol, result[j].Node.Symbol
		if ni.FilePath != nj.FilePath {
			return ni.FilePath < nj.FilePath
		}
		return ni.Name < nj.Name
	})

	return result
}

// DeadCodeWithCRS returns dead code analysis with a TraceStep.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DeadCodeWithCRS(ctx context.Context) ([]DeadCodeNode, crs.TraceStep) {
	start := time.Now()

	// CR-5 FIX: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_dead_code").
			WithTarget("project").
			WithTool("DeadCode").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return []DeadCodeNode{}, step
	}

	deadCode := a.DeadCode()

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_dead_code").
		WithTarget("project").
		WithTool("DeadCode").
		WithDuration(time.Since(start)).
		WithMetadata("dead_code_count", itoa(len(deadCode))).
		WithMetadata("total_nodes", itoa(a.graph.NodeCount())).
		Build()

	return deadCode, step
}

// isEntryPoint checks if a symbol is an entry point.
func isEntryPoint(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	name := sym.Name

	// Main and init are always entry points
	if name == "main" || name == "init" {
		return true
	}

	// Test and benchmark functions
	if len(name) > 4 && (name[:4] == "Test" || name[:4] == "Fuzz") {
		return true
	}
	if len(name) > 9 && name[:9] == "Benchmark" {
		return true
	}

	// Example functions (for documentation)
	if len(name) > 7 && name[:7] == "Example" {
		return true
	}

	// HTTP handlers (common patterns)
	if sym.Kind == ast.SymbolKindMethod && name == "ServeHTTP" {
		return true
	}

	return false
}

// =============================================================================
// Cyclic Dependency Detection (Tarjan's SCC)
// =============================================================================

// CyclicDependency represents a cycle in the dependency graph.
type CyclicDependency struct {
	// Nodes contains the node IDs in the cycle (in cycle order).
	Nodes []string

	// Packages contains the unique packages involved in the cycle.
	Packages []string

	// Length is the number of nodes in the cycle.
	Length int
}

// CyclicDependencies finds all cycles in the graph using Tarjan's SCC algorithm.
//
// Description:
//
//	Uses Tarjan's strongly connected components algorithm to find cycles.
//	Only returns components with more than one node (actual cycles).
//
//	Time complexity: O(V + E)
//	Space complexity: O(V)
//
//	Implementation uses an explicit call stack to avoid stack overflow on
//	deep graphs (CR-2 fix).
//
// Outputs:
//
//	[]CyclicDependency - All cycles found, sorted by length descending.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) CyclicDependencies() []CyclicDependency {
	// Tarjan's SCC state
	index := 0
	nodeIndex := make(map[string]int)
	nodeLowLink := make(map[string]int)
	onStack := make(map[string]bool)
	sccStack := make([]string, 0)
	sccs := make([][]string, 0)

	// callFrame represents a stack frame in the iterative Tarjan's algorithm.
	// This replaces the recursive call stack to avoid stack overflow on deep graphs.
	type callFrame struct {
		nodeID    string
		edgeIndex int    // Current index into Outgoing edges
		phase     int    // 0=init, 1=process edges, 2=post-child
		childID   string // ID of child we just returned from (for phase 2)
	}

	// strongConnectIterative performs Tarjan's algorithm iteratively.
	strongConnectIterative := func(startNodeID string) {
		callStack := []callFrame{{nodeID: startNodeID, edgeIndex: 0, phase: 0}}

		for len(callStack) > 0 {
			frame := &callStack[len(callStack)-1]

			switch frame.phase {
			case 0:
				// Initialize node (equivalent to function entry)
				nodeIndex[frame.nodeID] = index
				nodeLowLink[frame.nodeID] = index
				index++
				sccStack = append(sccStack, frame.nodeID)
				onStack[frame.nodeID] = true
				frame.phase = 1

			case 1:
				// Process outgoing edges
				node, ok := a.graph.GetNode(frame.nodeID)
				if !ok {
					frame.phase = 3 // Skip to finalization
					continue
				}

				// Find next unprocessed edge
				for frame.edgeIndex < len(node.Outgoing) {
					edge := node.Outgoing[frame.edgeIndex]
					frame.edgeIndex++

					if _, visited := nodeIndex[edge.ToID]; !visited {
						// Push new frame for unvisited child
						frame.phase = 2
						frame.childID = edge.ToID
						callStack = append(callStack, callFrame{
							nodeID:    edge.ToID,
							edgeIndex: 0,
							phase:     0,
						})
						goto continueLoop
					} else if onStack[edge.ToID] {
						// Back edge to node on stack
						if nodeIndex[edge.ToID] < nodeLowLink[frame.nodeID] {
							nodeLowLink[frame.nodeID] = nodeIndex[edge.ToID]
						}
					}
				}
				// All edges processed, move to finalization
				frame.phase = 3

			case 2:
				// Post-child processing (after returning from recursive call)
				if nodeLowLink[frame.childID] < nodeLowLink[frame.nodeID] {
					nodeLowLink[frame.nodeID] = nodeLowLink[frame.childID]
				}
				frame.phase = 1 // Continue processing edges

			case 3:
				// Finalization: check if this is SCC root
				if nodeLowLink[frame.nodeID] == nodeIndex[frame.nodeID] {
					scc := make([]string, 0)
					for {
						w := sccStack[len(sccStack)-1]
						sccStack = sccStack[:len(sccStack)-1]
						onStack[w] = false
						scc = append(scc, w)
						if w == frame.nodeID {
							break
						}
					}
					if len(scc) > 1 {
						// Only record cycles (components with >1 node)
						sccs = append(sccs, scc)
					}
				}
				// Pop this frame
				callStack = callStack[:len(callStack)-1]
			}
		continueLoop:
		}
	}

	// Run Tarjan's on all nodes
	for _, node := range a.graph.Nodes() {
		if _, visited := nodeIndex[node.ID]; !visited {
			strongConnectIterative(node.ID)
		}
	}

	// Convert to CyclicDependency results
	result := make([]CyclicDependency, 0, len(sccs))
	for _, scc := range sccs {
		pkgSet := make(map[string]bool)
		for _, nodeID := range scc {
			if node, ok := a.graph.GetNode(nodeID); ok {
				pkg := getNodePackage(node)
				if pkg != "" {
					pkgSet[pkg] = true
				}
			}
		}

		packages := make([]string, 0, len(pkgSet))
		for pkg := range pkgSet {
			packages = append(packages, pkg)
		}
		sort.Strings(packages)

		result = append(result, CyclicDependency{
			Nodes:    scc,
			Packages: packages,
			Length:   len(scc),
		})
	}

	// Sort by length descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Length > result[j].Length
	})

	return result
}

// CyclicDependenciesWithCRS returns cycles with a TraceStep.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) CyclicDependenciesWithCRS(ctx context.Context) ([]CyclicDependency, crs.TraceStep) {
	start := time.Now()

	// CR-5 FIX: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_cycles").
			WithTarget("project").
			WithTool("CyclicDependencies").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return []CyclicDependency{}, step
	}

	cycles := a.CyclicDependencies()

	totalNodes := 0
	for _, c := range cycles {
		totalNodes += c.Length
	}

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_cycles").
		WithTarget("project").
		WithTool("CyclicDependencies").
		WithDuration(time.Since(start)).
		WithMetadata("cycles_found", itoa(len(cycles))).
		WithMetadata("nodes_in_cycles", itoa(totalNodes)).
		WithMetadata("graph_nodes", itoa(a.graph.NodeCount())).
		WithMetadata("graph_edges", itoa(a.graph.EdgeCount())).
		Build()

	return cycles, step
}

// =============================================================================
// Package Coupling Metrics
// =============================================================================

// PackageCoupling computes coupling metrics for all packages.
//
// Description:
//
//	Computes Robert C. Martin's coupling metrics for each package:
//
//	- Afferent Coupling (Ca): packages that depend ON this package
//	- Efferent Coupling (Ce): packages this package depends ON
//	- Instability (I): Ce / (Ca + Ce), range [0, 1]
//	  - I=0: maximally stable (many dependents, no dependencies)
//	  - I=1: maximally unstable (no dependents, all dependencies)
//	- Abstractness (A): abstract types / total types
//
// Outputs:
//
//	map[string]CouplingMetrics - Metrics keyed by package path.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PackageCoupling() map[string]CouplingMetrics {
	result := make(map[string]CouplingMetrics)

	for _, pkgInfo := range a.graph.GetPackages() {
		deps := a.graph.GetPackageDependencies(pkgInfo.Name)
		dependents := a.graph.GetPackageDependents(pkgInfo.Name)

		ca := len(dependents) // Afferent (incoming)
		ce := len(deps)       // Efferent (outgoing)

		// Compute instability
		var instability float64
		if ca+ce > 0 {
			instability = float64(ce) / float64(ca+ce)
		}

		// Count abstract vs concrete types
		abstractCount := 0
		concreteCount := 0
		for _, node := range a.graph.GetNodesInPackage(pkgInfo.Name) {
			if node.Symbol == nil {
				continue
			}
			switch node.Symbol.Kind {
			case ast.SymbolKindInterface:
				abstractCount++
			case ast.SymbolKindStruct, ast.SymbolKindClass:
				concreteCount++
			}
		}

		// Compute abstractness
		var abstractness float64
		if abstractCount+concreteCount > 0 {
			abstractness = float64(abstractCount) / float64(abstractCount+concreteCount)
		}

		result[pkgInfo.Name] = CouplingMetrics{
			Package:       pkgInfo.Name,
			Afferent:      ca,
			Efferent:      ce,
			Instability:   instability,
			AbstractTypes: abstractCount,
			ConcreteTypes: concreteCount,
			Abstractness:  abstractness,
		}
	}

	return result
}

// PackageCouplingWithCRS returns coupling metrics with a TraceStep.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) PackageCouplingWithCRS(ctx context.Context) (map[string]CouplingMetrics, crs.TraceStep) {
	start := time.Now()

	// CR-5 FIX: Check context cancellation
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_coupling").
			WithTarget("project").
			WithTool("PackageCoupling").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return map[string]CouplingMetrics{}, step
	}

	metrics := a.PackageCoupling()

	// Find most unstable and most stable
	mostUnstable := ""
	mostStable := ""
	highestI := 0.0
	lowestI := 2.0 // Start above max

	for pkg, m := range metrics {
		if m.Instability > highestI {
			highestI = m.Instability
			mostUnstable = pkg
		}
		if m.Instability < lowestI {
			lowestI = m.Instability
			mostStable = pkg
		}
	}

	step := crs.NewTraceStepBuilder().
		WithAction("analytics_coupling").
		WithTarget("project").
		WithTool("PackageCoupling").
		WithDuration(time.Since(start)).
		WithMetadata("packages_analyzed", itoa(len(metrics))).
		WithMetadata("most_unstable_pkg", mostUnstable).
		WithMetadata("most_stable_pkg", mostStable).
		Build()

	return metrics, step
}

// GetCouplingForPackage returns coupling metrics for a specific package.
//
// Inputs:
//
//	pkg - The package path to analyze.
//
// Outputs:
//
//	*CouplingMetrics - Metrics for the package, or nil if not found.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) GetCouplingForPackage(pkg string) *CouplingMetrics {
	deps := a.graph.GetPackageDependencies(pkg)
	dependents := a.graph.GetPackageDependents(pkg)

	ca := len(dependents)
	ce := len(deps)

	var instability float64
	if ca+ce > 0 {
		instability = float64(ce) / float64(ca+ce)
	}

	abstractCount := 0
	concreteCount := 0
	for _, node := range a.graph.GetNodesInPackage(pkg) {
		if node.Symbol == nil {
			continue
		}
		switch node.Symbol.Kind {
		case ast.SymbolKindInterface:
			abstractCount++
		case ast.SymbolKindStruct, ast.SymbolKindClass:
			concreteCount++
		}
	}

	var abstractness float64
	if abstractCount+concreteCount > 0 {
		abstractness = float64(abstractCount) / float64(abstractCount+concreteCount)
	}

	return &CouplingMetrics{
		Package:       pkg,
		Afferent:      ca,
		Efferent:      ce,
		Instability:   instability,
		AbstractTypes: abstractCount,
		ConcreteTypes: concreteCount,
		Abstractness:  abstractness,
	}
}
