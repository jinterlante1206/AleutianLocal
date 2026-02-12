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
	"sort"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// SESE Region Detection (GR-16h)
// =============================================================================

var seseTracer = otel.Tracer("graph.sese")

// seseContextCheckInterval is how often to check for context cancellation.
const seseContextCheckInterval = 100

// SESERegion represents a single-entry single-exit region.
//
// A SESE region has exactly one entry and one exit node where:
//   - All paths into the region go through entry
//   - All paths out of the region go through exit
//   - Entry dominates all nodes in the region
//   - Exit post-dominates all nodes in the region
//
// Thread Safety: Safe for concurrent use after construction.
type SESERegion struct {
	// Entry is the single entry node ID.
	Entry string

	// Exit is the single exit node ID.
	Exit string

	// Nodes contains all node IDs in the region (including entry/exit).
	Nodes []string

	// Size is the node count (len(Nodes)).
	Size int

	// Parent is the enclosing region (nil if top-level).
	Parent *SESERegion

	// Children are nested regions within this region.
	Children []*SESERegion

	// Depth is the nesting depth (0 = top-level).
	Depth int
}

// SESEResult contains all detected SESE regions.
//
// Thread Safety: Safe for concurrent use after construction.
type SESEResult struct {
	// Regions contains all SESE regions found.
	Regions []*SESERegion

	// TopLevel contains regions with no parent (depth 0).
	TopLevel []*SESERegion

	// RegionOf maps node ID → innermost containing region.
	RegionOf map[string]*SESERegion

	// MaxDepth is the maximum nesting depth found.
	MaxDepth int

	// Extractable contains regions suitable for extraction.
	// Populated by FilterExtractable().
	Extractable []*SESERegion

	// NodeCount is the total nodes analyzed.
	NodeCount int

	// RegionCount is the total regions found.
	RegionCount int
}

// SESEError represents an error in SESE region detection.
type SESEError struct {
	Message string
}

func (e *SESEError) Error() string {
	return e.Message
}

// FilterExtractable returns regions suitable for extraction based on size.
//
// Description:
//
//	Filters regions by node count. Regions that are too small may not be
//	worth extracting, while regions that are too large should be split further.
//
// Inputs:
//
//   - minSize: Minimum nodes in region (inclusive). Use 1 to include all.
//   - maxSize: Maximum nodes in region (inclusive). Use a large value for no limit.
//
// Outputs:
//
//   - []*SESERegion: Regions within the size range. Never nil.
//
// Example:
//
//	// Find regions with 3-50 nodes (good candidates for extraction)
//	extractable := result.FilterExtractable(3, 50)
//
// Thread Safety: Safe for concurrent use.
func (r *SESEResult) FilterExtractable(minSize, maxSize int) []*SESERegion {
	if r == nil || len(r.Regions) == 0 {
		return []*SESERegion{}
	}

	extractable := make([]*SESERegion, 0)
	for _, region := range r.Regions {
		if region.Size >= minSize && region.Size <= maxSize {
			extractable = append(extractable, region)
		}
	}
	return extractable
}

// DetectSESERegions finds all SESE regions in the graph.
//
// Description:
//
//	Uses dominator and post-dominator trees to identify regions with
//	single entry and single exit points. A node N forms a SESE entry
//	with its immediate post-dominator P if N dominates P.
//
//	The algorithm iterates through all nodes and checks if each node
//	can serve as a SESE entry with its immediate post-dominator as exit.
//
// Inputs:
//
//   - ctx: Context for cancellation. Checked every 100 nodes.
//   - domTree: Pre-computed dominator tree. Must not be nil.
//   - postDomTree: Pre-computed post-dominator tree. Must not be nil.
//
// Outputs:
//
//   - *SESEResult: All detected SESE regions with hierarchy. Never nil.
//   - error: Non-nil if inputs are nil or context cancelled.
//
// Example:
//
//	domTree, _ := analytics.Dominators(ctx, "main")
//	postDomTree, _ := analytics.PostDominators(ctx, "")
//	result, err := analytics.DetectSESERegions(ctx, domTree, postDomTree)
//	if err != nil {
//	    log.Fatalf("failed to detect SESE regions: %v", err)
//	}
//	for _, region := range result.Extractable {
//	    log.Printf("extractable: %s -> %s (size: %d)", region.Entry, region.Exit, region.Size)
//	}
//
// Limitations:
//
//   - Requires both dominator and post-dominator trees
//   - Only detects SESE regions for nodes in both trees
//   - Large graphs may produce many regions; use FilterExtractable()
//
// Assumptions:
//
//   - domTree and postDomTree are computed from the same graph
//   - Graph is frozen
//   - Trees are consistent and valid
//
// Thread Safety: Safe for concurrent use (read-only on trees).
//
// Complexity: O(E) after dom trees are computed.
func (a *GraphAnalytics) DetectSESERegions(
	ctx context.Context,
	domTree, postDomTree *DominatorTree,
) (*SESEResult, error) {
	// Initialize result with empty structures
	result := &SESEResult{
		Regions:     make([]*SESERegion, 0),
		TopLevel:    make([]*SESERegion, 0),
		RegionOf:    make(map[string]*SESERegion),
		Extractable: make([]*SESERegion, 0),
	}

	// Nil analytics check
	if a == nil || a.graph == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: nil analytics or graph, returning empty result")
		return result, nil
	}

	// Nil domTree check
	if domTree == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: nil dominator tree")
		return result, &SESEError{Message: "dominator tree must not be nil"}
	}

	// Nil postDomTree check
	if postDomTree == nil {
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: nil post-dominator tree")
		return result, &SESEError{Message: "post-dominator tree must not be nil"}
	}

	// Track start time
	startTime := time.Now()

	// Start OTel span
	nodeCount := a.graph.NodeCount()
	edgeCount := a.graph.EdgeCount()

	ctx, span := seseTracer.Start(ctx, "GraphAnalytics.DetectSESERegions",
		trace.WithAttributes(
			attribute.String("dom_entry", domTree.Entry),
			attribute.String("postdom_entry", postDomTree.Entry),
			attribute.Int("node_count", nodeCount),
			attribute.Int("edge_count", edgeCount),
		),
	)
	defer span.End()

	result.NodeCount = nodeCount

	// Context check
	if ctx != nil && ctx.Err() != nil {
		span.AddEvent("context_cancelled_early")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: context cancelled before start")
		return result, ctx.Err()
	}

	// Empty graph check
	if nodeCount == 0 {
		span.AddEvent("empty_graph")
		telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: empty graph, returning empty result")
		return result, nil
	}

	// Find all SESE regions
	regions := make([]*SESERegion, 0)
	iterationsProcessed := 0

	// Iterate through all nodes in the dominator tree
	for nodeID := range domTree.ImmediateDom {
		iterationsProcessed++

		// Check context periodically
		if iterationsProcessed%seseContextCheckInterval == 0 {
			if ctx != nil && ctx.Err() != nil {
				span.AddEvent("context_cancelled", trace.WithAttributes(
					attribute.Int("nodes_processed", iterationsProcessed),
				))
				telemetry.LoggerWithTrace(ctx, slog.Default()).Info("sese: context cancelled",
					slog.Int("nodes_processed", iterationsProcessed),
				)
				// Return what we have so far
				result.Regions = regions
				result.RegionCount = len(regions)
				return result, ctx.Err()
			}
		}

		// Get immediate post-dominator of this node
		ipd, hasIPD := postDomTree.ImmediateDom[nodeID]
		if !hasIPD || ipd == "" || ipd == nodeID {
			// No valid post-dominator or trivial self-reference
			continue
		}

		// Check if this forms a SESE region:
		// Node dominates its immediate post-dominator
		if domTree.Dominates(nodeID, ipd) {
			// Collect all nodes in this region
			regionNodes := a.collectSESENodes(ctx, nodeID, ipd, domTree, postDomTree)

			// Create region
			region := &SESERegion{
				Entry: nodeID,
				Exit:  ipd,
				Nodes: regionNodes,
				Size:  len(regionNodes),
			}
			regions = append(regions, region)
		}
	}

	// Build hierarchy
	a.buildSESEHierarchy(regions)

	// Collect top-level regions and compute max depth
	maxDepth := 0
	topLevel := make([]*SESERegion, 0)
	for _, region := range regions {
		if region.Parent == nil {
			topLevel = append(topLevel, region)
		}
		if region.Depth > maxDepth {
			maxDepth = region.Depth
		}
	}

	// Build RegionOf map (node -> innermost containing region)
	regionOf := a.buildRegionOfMap(regions)

	result.Regions = regions
	result.TopLevel = topLevel
	result.RegionOf = regionOf
	result.MaxDepth = maxDepth
	result.RegionCount = len(regions)

	span.AddEvent("detection_complete", trace.WithAttributes(
		attribute.Int("regions_found", len(regions)),
		attribute.Int("top_level", len(topLevel)),
		attribute.Int("max_depth", maxDepth),
	))

	telemetry.LoggerWithTrace(ctx, slog.Default()).Debug("sese: detection complete",
		slog.Int("regions", len(regions)),
		slog.Int("top_level", len(topLevel)),
		slog.Int("max_depth", maxDepth),
		slog.Duration("duration", time.Since(startTime)),
	)

	return result, nil
}

// collectSESENodes collects all nodes in the SESE region (entry, exit).
//
// A node is in the region if:
//   - entry dominates it
//   - exit post-dominates it
//   - It's reachable from entry and can reach exit
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) collectSESENodes(
	ctx context.Context,
	entry, exit string,
	domTree, postDomTree *DominatorTree,
) []string {
	if a == nil || a.graph == nil {
		return []string{entry, exit}
	}

	nodes := make([]string, 0)
	seen := make(map[string]bool)

	// BFS from entry, stop at exit
	queue := []string{entry}
	seen[entry] = true

	maxIterations := a.graph.NodeCount() + 1

	for i := 0; len(queue) > 0 && i < maxIterations; i++ {
		current := queue[0]
		queue = queue[1:]

		// Check if this node is dominated by entry and post-dominated by exit
		if !domTree.Dominates(entry, current) {
			continue
		}
		if !postDomTree.Dominates(exit, current) {
			continue
		}

		nodes = append(nodes, current)

		// Don't expand past exit
		if current == exit {
			continue
		}

		// Get successors
		node, ok := a.graph.GetNode(current)
		if !ok {
			continue
		}

		for _, edge := range node.Outgoing {
			if !seen[edge.ToID] {
				seen[edge.ToID] = true
				queue = append(queue, edge.ToID)
			}
		}
	}

	return nodes
}

// buildSESEHierarchy builds the parent/child relationships between regions.
//
// A region R1 is a parent of R2 if:
//   - R2 is contained within R1 (all nodes of R2 are nodes of R1)
//   - R1 is the smallest such containing region
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildSESEHierarchy(regions []*SESERegion) {
	if len(regions) <= 1 {
		return
	}

	// Sort regions by size (largest first)
	sort.Slice(regions, func(i, j int) bool {
		return regions[i].Size > regions[j].Size
	})

	// Build node membership sets for efficient containment check
	nodeSets := make([]map[string]bool, len(regions))
	for i, region := range regions {
		nodeSets[i] = make(map[string]bool, len(region.Nodes))
		for _, nodeID := range region.Nodes {
			nodeSets[i][nodeID] = true
		}
	}

	// For each region, find its smallest containing region (if any)
	for i := len(regions) - 1; i >= 0; i-- {
		smallerRegion := regions[i]

		// Look for smallest containing region (among regions larger than this one)
		var parent *SESERegion
		parentIdx := -1

		for j := i - 1; j >= 0; j-- {
			largerRegion := regions[j]

			// Check if larger region contains smaller region
			if a.regionContains(nodeSets[j], smallerRegion) {
				// This is a potential parent
				if parent == nil || largerRegion.Size < parent.Size {
					parent = largerRegion
					parentIdx = j
				}
			}
		}

		if parent != nil && parentIdx >= 0 {
			smallerRegion.Parent = parent
			smallerRegion.Depth = parent.Depth + 1
			parent.Children = append(parent.Children, smallerRegion)
		}
	}
}

// regionContains checks if the node set contains all nodes of the region.
func (a *GraphAnalytics) regionContains(nodeSet map[string]bool, region *SESERegion) bool {
	for _, nodeID := range region.Nodes {
		if !nodeSet[nodeID] {
			return false
		}
	}
	return true
}

// buildRegionOfMap maps each node to its innermost containing region.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) buildRegionOfMap(regions []*SESERegion) map[string]*SESERegion {
	regionOf := make(map[string]*SESERegion)

	// Sort regions by size (smallest first) so smaller regions overwrite larger ones
	sortedRegions := make([]*SESERegion, len(regions))
	copy(sortedRegions, regions)
	sort.Slice(sortedRegions, func(i, j int) bool {
		return sortedRegions[i].Size > sortedRegions[j].Size
	})

	// Larger regions are processed first, then overwritten by smaller ones
	for _, region := range sortedRegions {
		for _, nodeID := range region.Nodes {
			regionOf[nodeID] = region
		}
	}

	return regionOf
}

// DetectSESERegionsWithCRS wraps DetectSESERegions with CRS tracing.
//
// Description:
//
//	Provides the same functionality as DetectSESERegions but also returns
//	a TraceStep for recording in the Code Reasoning State (CRS).
//
// Inputs:
//
//   - ctx: Context for cancellation.
//   - domTree: Pre-computed dominator tree.
//   - postDomTree: Pre-computed post-dominator tree.
//
// Outputs:
//
//   - *SESEResult: All detected SESE regions.
//   - crs.TraceStep: Trace step for CRS recording.
//
// Thread Safety: Safe for concurrent use.
func (a *GraphAnalytics) DetectSESERegionsWithCRS(
	ctx context.Context,
	domTree, postDomTree *DominatorTree,
) (*SESEResult, crs.TraceStep) {
	start := time.Now()

	// Nil domTree check
	if domTree == nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_sese").
			WithTarget("graph").
			WithTool("DetectSESERegions").
			WithDuration(time.Since(start)).
			WithError("dominator tree must not be nil").
			Build()
		return &SESEResult{
			Regions:     []*SESERegion{},
			TopLevel:    []*SESERegion{},
			RegionOf:    make(map[string]*SESERegion),
			Extractable: []*SESERegion{},
		}, step
	}

	// Nil postDomTree check
	if postDomTree == nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_sese").
			WithTarget(domTree.Entry).
			WithTool("DetectSESERegions").
			WithDuration(time.Since(start)).
			WithError("post-dominator tree must not be nil").
			Build()
		return &SESEResult{
			Regions:     []*SESERegion{},
			TopLevel:    []*SESERegion{},
			RegionOf:    make(map[string]*SESERegion),
			Extractable: []*SESERegion{},
		}, step
	}

	// Early cancellation check
	if ctx != nil && ctx.Err() != nil {
		step := crs.NewTraceStepBuilder().
			WithAction("analytics_sese").
			WithTarget(domTree.Entry).
			WithTool("DetectSESERegions").
			WithDuration(time.Since(start)).
			WithError(ctx.Err().Error()).
			Build()
		return &SESEResult{
			Regions:     []*SESERegion{},
			TopLevel:    []*SESERegion{},
			RegionOf:    make(map[string]*SESERegion),
			Extractable: []*SESERegion{},
		}, step
	}

	result, err := a.DetectSESERegions(ctx, domTree, postDomTree)

	// Build TraceStep
	builder := crs.NewTraceStepBuilder().
		WithAction("analytics_sese").
		WithTarget(domTree.Entry).
		WithTool("DetectSESERegions").
		WithDuration(time.Since(start))

	if err != nil {
		builder = builder.WithError(err.Error())
	} else {
		builder = builder.
			WithMetadata("regions_found", itoa(len(result.Regions))).
			WithMetadata("top_level_count", itoa(len(result.TopLevel))).
			WithMetadata("max_depth", itoa(result.MaxDepth)).
			WithMetadata("node_count", itoa(result.NodeCount))
	}

	return result, builder.Build()
}
