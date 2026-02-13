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
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SubtreeQueryEngine provides efficient subtree queries using HLD position ranges.
//
// Description:
//
//	Leverages HLD's DFS ordering property: subtree(v) occupies contiguous
//	range [pos[v], pos[v]+size[v]) in position array. Enables O(log V)
//	subtree queries with a single segment tree range query.
//
// Invariants:
//   - pos[v] + size[v] <= nodeCount for all v
//   - All descendants of v have positions in [pos[v], pos[v]+size[v])
//   - Subtree ranges are non-overlapping (except for ancestors)
//   - Exactly one of hld or forest is non-nil
//   - In forest mode, subtrees are always within single tree (by definition)
//
// Thread Safety:
//
//	Safe for concurrent queries (read-only operations with internal locking for stats).
type SubtreeQueryEngine struct {
	hld     *HLDecomposition // Single tree mode
	forest  *HLDForest       // Forest mode (multiple trees)
	segTree *SegmentTree
	aggFunc AggregateFunc
	logger  *slog.Logger // Structured logging

	// CRS integration
	crs        crs.CRS
	sessionID  string
	stepNumber int64 // Next step number for CRS (use atomic operations)

	// Query statistics (protected by mutex for concurrent access)
	mu               sync.Mutex
	queryCount       int64
	totalLatency     time.Duration
	totalSubtreeSize int64

	// Range cache (immutable ranges, thread-safe via sync.Map)
	rangeCache sync.Map // map[string][2]int - caches [start,end] for each node
}

// SubtreeQueryStats provides query statistics.
type SubtreeQueryStats struct {
	QueryCount     int64
	TotalLatency   time.Duration
	AvgLatency     time.Duration
	AvgSubtreeSize float64
}

// NewSubtreeQueryEngine creates a subtree query engine for single-tree mode.
//
// Description:
//
//	Combines HLD and segment tree for efficient subtree aggregation queries.
//	Uses HLD's subtree size information to compute position ranges.
//
// Algorithm:
//
//	Time:  O(1) - store references and validate
//	Space: O(1) - no allocations
//
// Inputs:
//   - hld: Heavy-Light Decomposition. Must not be nil.
//   - segTree: Segment tree over node values. Must not be nil.
//   - aggFunc: Aggregation function. Must match segTree's aggregation.
//   - crsRecorder: CRS recorder for tracing (can be nil).
//   - sessionID: Session ID for CRS context (ignored if crsRecorder is nil).
//
// Outputs:
//   - *SubtreeQueryEngine: Ready for queries. Never nil on success.
//   - error: Non-nil if inputs are invalid.
//
// Example:
//
//	// Build HLD and segment tree
//	hld, _ := BuildHLD(ctx, graph, "main")
//	values := buildValueArray(hld)
//	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)
//
//	// Create subtree query engine
//	engine, _ := NewSubtreeQueryEngine(hld, segTree, AggregateSUM, nil, "")
//
// Limitations:
//   - Single tree only. For forests, use NewSubtreeQueryEngineForest.
//   - Aggregation function cannot be changed after construction.
//
// Assumptions:
//   - HLD and segment tree are already built and valid.
//   - Segment tree values correspond to HLD positions.
//
// Thread Safety: Safe for concurrent use.
func NewSubtreeQueryEngine(hld *HLDecomposition, segTree *SegmentTree, aggFunc AggregateFunc, crsRecorder crs.CRS, sessionID string) (*SubtreeQueryEngine, error) {
	// Validate inputs
	if hld == nil {
		return nil, errors.New("subtree query engine: hld must not be nil")
	}
	if segTree == nil {
		return nil, errors.New("subtree query engine: segTree must not be nil")
	}

	// Validate compatibility (C-PRE-2 partial)
	if hld.NodeCount() != segTree.size {
		return nil, fmt.Errorf("subtree query engine: HLD node count %d != segment tree size %d",
			hld.NodeCount(), segTree.size)
	}

	// Validate aggregation function
	if aggFunc != segTree.aggFunc {
		return nil, fmt.Errorf("subtree query engine: agg func %s != segment tree agg func %s",
			aggFunc, segTree.aggFunc)
	}

	return &SubtreeQueryEngine{
		hld:       hld,
		forest:    nil,
		segTree:   segTree,
		aggFunc:   aggFunc,
		logger:    slog.Default(),
		crs:       crsRecorder,
		sessionID: sessionID,
	}, nil
}

// NewSubtreeQueryEngineForest creates a subtree query engine for forest mode.
//
// Description:
//
//	Like NewSubtreeQueryEngine but supports multiple disconnected trees.
//	Automatically routes queries to the correct tree component.
//
// Algorithm:
//
//	Time:  O(1) - store references and validate
//	Space: O(1) - no allocations
//
// Inputs:
//   - forest: HLD forest. Must not be nil.
//   - segTree: Segment tree over ALL forest nodes. Must not be nil.
//   - aggFunc: Aggregation function. Must match segTree's aggregation.
//   - crsRecorder: CRS recorder for tracing (can be nil).
//   - sessionID: Session ID for CRS context (ignored if crsRecorder is nil).
//
// Outputs:
//   - *SubtreeQueryEngine: Ready for queries. Never nil on success.
//   - error: Non-nil if inputs are invalid.
//
// Example:
//
//	forest, _ := BuildHLDForest(ctx, graph, true)
//	values := buildForestValueArray(forest)
//	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)
//	engine, _ := NewSubtreeQueryEngineForest(forest, segTree, AggregateSUM, nil, "")
//
// Limitations:
//   - Segment tree must span entire forest (size = forest.TotalNodes()).
//
// Assumptions:
//   - Forest and segment tree are already built and valid.
//   - Segment tree values correspond to forest global positions.
//
// Thread Safety: Safe for concurrent use.
func NewSubtreeQueryEngineForest(forest *HLDForest, segTree *SegmentTree, aggFunc AggregateFunc, crsRecorder crs.CRS, sessionID string) (*SubtreeQueryEngine, error) {
	// Validate inputs (H-PRE-3)
	if forest == nil {
		return nil, errors.New("subtree query engine: forest must not be nil")
	}
	if segTree == nil {
		return nil, errors.New("subtree query engine: segTree must not be nil")
	}

	// Validate forest compatibility (M-PRE-10)
	if forest.TotalNodes() != segTree.size {
		return nil, fmt.Errorf("subtree query engine: forest node count %d != segment tree size %d",
			forest.TotalNodes(), segTree.size)
	}

	// Validate aggregation function
	if aggFunc != segTree.aggFunc {
		return nil, fmt.Errorf("subtree query engine: agg func %s != segment tree agg func %s",
			aggFunc, segTree.aggFunc)
	}

	return &SubtreeQueryEngine{
		hld:       nil,
		forest:    forest,
		segTree:   segTree,
		aggFunc:   aggFunc,
		logger:    slog.Default(),
		crs:       crsRecorder,
		sessionID: sessionID,
	}, nil
}

// SubtreeQuery computes aggregate over all nodes in subtree of v.
//
// Description:
//
//	Uses HLD's contiguous subtree property to answer query with a single
//	segment tree range query. Subtree(v) = [pos[v], pos[v]+size[v]).
//
// Algorithm:
//
//	Time:  O(log V) - single segment tree query
//	Space: O(1) - no allocations
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - int64: Aggregate value over all nodes in subtree(v).
//   - error: Non-nil if node doesn't exist or query fails.
//
// Special Cases:
//   - SubtreeQuery(leaf) = value[leaf] (single node, uses fast path)
//   - SubtreeQuery(root) = aggregate over entire tree
//
// Example:
//
//	// Total complexity in package and all subpackages
//	totalComplexity, err := engine.SubtreeQuery(ctx, "pkg/parser")
//	if err != nil {
//		return fmt.Errorf("subtree query: %w", err)
//	}
//	fmt.Printf("Total complexity: %d\n", totalComplexity)
//
// CRS Integration:
//
//	Records query as TraceStep with node ID, subtree size, result.
//	Only records if CRS recorder is non-nil (H-PRE-6).
//
// Limitations:
//   - Requires valid HLD and segment tree.
//   - Forest mode requires GetTreeOffset method.
//
// Assumptions:
//   - Node v exists in the graph.
//   - HLD positions are valid and consistent.
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeQuery(ctx context.Context, v string) (int64, error) {
	// Input validation
	if ctx == nil {
		return 0, errors.New("subtree query: ctx must not be nil")
	}

	// Check context cancellation early
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	// Start OTel span (L-POST-1: add mode attribute)
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SubtreeQueryEngine.SubtreeQuery",
		trace.WithAttributes(
			attribute.String("node", v),
			attribute.String("agg_func", sqe.aggFunc.String()),
			attribute.String("mode", sqe.mode()),
		),
	)
	defer span.End()

	start := time.Now()

	// Get subtree range (with caching - H-PRE-4)
	rangeStart, rangeEnd, err := sqe.getSubtreeRange(ctx, v, span)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get subtree range")
		return 0, fmt.Errorf("subtree query for node %q: %w", v, err)
	}

	subtreeSize := rangeEnd - rangeStart + 1

	// Fast path for single-node subtrees (M-PRE-4)
	var result int64
	if subtreeSize == 1 {
		span.AddEvent("fast_path_single_node")
		result, err = sqe.segTree.GetValue(ctx, rangeStart)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "get value failed")
			return 0, fmt.Errorf("subtree query for node %q: get value: %w", v, err)
		}
	} else {
		// Standard path: segment tree query
		span.AddEvent("querying_segment_tree",
			trace.WithAttributes(
				attribute.Int("start", rangeStart),
				attribute.Int("end", rangeEnd),
				attribute.Int("size", subtreeSize),
			),
		)

		result, err = sqe.segTree.Query(ctx, rangeStart, rangeEnd)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "segment tree query failed")
			return 0, fmt.Errorf("subtree query for node %q: segment tree query [%d,%d]: %w",
				v, rangeStart, rangeEnd, err)
		}
	}

	duration := time.Since(start)

	// Update stats with locking (H-PRE-2)
	sqe.mu.Lock()
	sqe.queryCount++
	sqe.totalLatency += duration
	sqe.totalSubtreeSize += int64(subtreeSize)
	sqe.mu.Unlock()

	// Record metrics (M-PRE-6)
	subtreeQueryDuration.WithLabelValues(sqe.aggFunc.String()).Observe(duration.Seconds())
	subtreeQuerySize.Observe(float64(subtreeSize))

	// Log for large subtrees (M-PRE-5)
	if subtreeSize > 10000 {
		sqe.logger.Warn("large subtree query",
			slog.String("node", v),
			slog.Int("size", subtreeSize),
			slog.Duration("duration", duration),
		)
	}

	// CRS recording (H-PRE-5, H-PRE-6, M-PRE-7, C-POST-1)
	if sqe.crs != nil && sqe.sessionID != "" {
		step := crs.StepRecord{
			SessionID:     sqe.sessionID,
			StepNumber:    int(atomic.AddInt64(&sqe.stepNumber, 1)),
			Timestamp:     time.Now().UnixMilli(),
			Actor:         crs.ActorSystem,
			Decision:      crs.DecisionExecuteTool,
			Tool:          "SubtreeQuery",
			ToolParams:    &crs.ToolParams{Target: v, Query: sqe.aggFunc.String()},
			Outcome:       crs.OutcomeSuccess,
			DurationMs:    duration.Milliseconds(),
			ResultSummary: fmt.Sprintf("SubtreeQuery(%s,%s)=%d (size=%d)", v, sqe.aggFunc.String(), result, subtreeSize),
			Propagate:     false,
			Terminal:      false,
		}

		if err := sqe.crs.RecordStep(ctx, step); err != nil {
			sqe.logger.Warn("failed to record CRS step",
				slog.String("error", err.Error()),
				slog.String("tool", "SubtreeQuery"),
			)
		}
	}

	span.SetAttributes(
		attribute.Int("subtree_size", subtreeSize),
		attribute.Int64("result", result),
		attribute.Int64("duration_us", duration.Microseconds()),
	)
	span.SetStatus(codes.Ok, "subtree query complete")

	return result, nil
}

// getSubtreeRange returns the position range for subtree(v) with caching.
//
// This is an internal helper that implements range caching (H-PRE-4)
// and forest mode position offset (C-PRE-1).
func (sqe *SubtreeQueryEngine) getSubtreeRange(ctx context.Context, v string, span trace.Span) (start, end int, err error) {
	// Check cache first (H-PRE-4)
	if cached, ok := sqe.rangeCache.Load(v); ok {
		r := cached.([2]int)
		span.AddEvent("range_cache_hit")
		sqe.logger.Debug("subtree range cache hit",
			slog.String("node", v),
			slog.Int("start", r[0]),
			slog.Int("end", r[1]),
		)
		return r[0], r[1], nil
	}

	// Compute range based on mode
	if sqe.hld != nil {
		// Single tree mode
		vIdx, ok := sqe.hld.NodeToIdx(v)
		if !ok {
			return 0, 0, fmt.Errorf("node %q does not exist", v)
		}

		pos := sqe.hld.Pos(vIdx)
		size := sqe.hld.SubtreeSize(vIdx)
		start = pos
		end = pos + size - 1

	} else if sqe.forest != nil {
		// Forest mode (C-PRE-1: must offset positions)
		treeHLD := sqe.forest.GetHLD(v)
		if treeHLD == nil {
			return 0, 0, fmt.Errorf("node %q does not exist in forest", v)
		}

		vIdx, ok := treeHLD.NodeToIdx(v)
		if !ok {
			return 0, 0, fmt.Errorf("node %q index not found", v)
		}

		// Get tree-local positions
		treeLocalPos := treeHLD.Pos(vIdx)
		size := treeHLD.SubtreeSize(vIdx)

		// Get tree offset to map to global segment tree positions (C-PRE-1)
		treeOffset, err := sqe.forest.GetTreeOffset(v)
		if err != nil {
			return 0, 0, fmt.Errorf("getting tree offset for node %q: %w", v, err)
		}

		// Apply offset
		start = treeOffset + treeLocalPos
		end = treeOffset + treeLocalPos + size - 1

	} else {
		return 0, 0, errors.New("engine has neither hld nor forest (invalid state)")
	}

	// Validate bounds (C-PRE-2)
	if start < 0 || end >= sqe.segTree.size {
		sqe.logger.Error("subtree range out of bounds",
			slog.String("node", v),
			slog.Int("start", start),
			slog.Int("end", end),
			slog.Int("tree_size", sqe.segTree.size),
		)
		return 0, 0, fmt.Errorf("subtree range [%d,%d] out of bounds [0,%d)",
			start, end, sqe.segTree.size)
	}

	// Cache the result (H-PRE-4)
	sqe.rangeCache.Store(v, [2]int{start, end})

	span.AddEvent("computed_range",
		trace.WithAttributes(
			attribute.Int("start", start),
			attribute.Int("end", end),
			attribute.Int("size", end-start+1),
		),
	)

	// Debug logging (M-POST-1)
	sqe.logger.Debug("computed subtree range",
		slog.String("node", v),
		slog.Int("start", start),
		slog.Int("end", end),
		slog.Int("size", end-start+1),
		slog.Bool("cached", false),
	)

	return start, end, nil
}

// SubtreeSum computes sum of all values in subtree.
//
// Description:
//
//	Convenience wrapper for SubtreeQuery with SUM aggregation.
//	Requires engine to be configured with AggregateSUM.
//
// Algorithm:
//
//	Time:  O(log V) - delegates to SubtreeQuery
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - int64: Sum of all values in subtree(v).
//   - error: Non-nil if node doesn't exist or query fails.
//
// Example:
//
//	totalLOC, err := engine.SubtreeSum(ctx, "main")
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeSum(ctx context.Context, v string) (int64, error) {
	return sqe.SubtreeQuery(ctx, v)
}

// SubtreeMin computes minimum value in subtree.
//
// Description:
//
//	Convenience wrapper for SubtreeQuery with MIN aggregation.
//	Requires engine to be configured with AggregateMIN.
//
// Algorithm:
//
//	Time:  O(log V) - delegates to SubtreeQuery
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - int64: Minimum value in subtree(v).
//   - error: Non-nil if node doesn't exist or query fails.
//
// Example:
//
//	minMemory, err := engine.SubtreeMin(ctx, "allocator")
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeMin(ctx context.Context, v string) (int64, error) {
	return sqe.SubtreeQuery(ctx, v)
}

// SubtreeMax computes maximum value in subtree.
//
// Description:
//
//	Convenience wrapper for SubtreeQuery with MAX aggregation.
//	Requires engine to be configured with AggregateMAX.
//
// Algorithm:
//
//	Time:  O(log V) - delegates to SubtreeQuery
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - int64: Maximum value in subtree(v).
//   - error: Non-nil if node doesn't exist or query fails.
//
// Example:
//
//	maxLatency, err := engine.SubtreeMax(ctx, "handler")
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeMax(ctx context.Context, v string) (int64, error) {
	return sqe.SubtreeQuery(ctx, v)
}

// SubtreeGCD computes GCD of all values in subtree.
//
// Description:
//
//	Convenience wrapper for SubtreeQuery with GCD aggregation.
//	Requires engine to be configured with AggregateGCD.
//
// Algorithm:
//
//	Time:  O(log V) - delegates to SubtreeQuery
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - int64: GCD of all values in subtree(v).
//   - error: Non-nil if node doesn't exist or query fails.
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeGCD(ctx context.Context, v string) (int64, error) {
	return sqe.SubtreeQuery(ctx, v)
}

// SubtreeRange returns the position range occupied by subtree(v).
//
// Description:
//
//	Returns [start, end] (inclusive) position range for subtree.
//	Useful for debugging and visualization.
//
// Algorithm:
//
//	Time:  O(1) - cached array lookups
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - start: First position in subtree (pos[v])
//   - end: Last position in subtree (pos[v] + size[v] - 1)
//   - error: Non-nil if node doesn't exist
//
// Example:
//
//	start, end, _ := engine.SubtreeRange(ctx, "parser")
//	fmt.Printf("Subtree occupies positions [%d, %d]\n", start, end)
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) SubtreeRange(ctx context.Context, v string) (start, end int, err error) {
	if ctx == nil {
		return 0, 0, errors.New("subtree range: ctx must not be nil")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	default:
	}

	// Use internal helper (with caching)
	_, span := otel.Tracer("trace").Start(ctx, "graph.SubtreeQueryEngine.SubtreeRange")
	defer span.End()

	start, end, err = sqe.getSubtreeRange(ctx, v, span)
	if err != nil {
		return 0, 0, fmt.Errorf("subtree range for node %q: %w", v, err)
	}

	return start, end, nil
}

// SubtreeNodes returns all node IDs in subtree(v).
//
// Description:
//
//	Materializes the subtree as a list of node IDs.
//	Useful for debugging but slower than aggregation queries.
//
// Algorithm:
//
//	Time:  O(size[v]) - collect all nodes in subtree
//	Space: O(size[v]) - store node IDs
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//
// Outputs:
//   - []string: Node IDs in DFS order (same as position order)
//   - error: Non-nil if node doesn't exist
//
// Example:
//
//	nodes, err := engine.SubtreeNodes(ctx, "package")
//	fmt.Printf("Package contains %d functions\n", len(nodes))
//
// Limitations:
//
//	Slower than SubtreeQuery - use only when you need the actual node list.
//
// Thread Safety: Safe for concurrent use (allocates new slice).
func (sqe *SubtreeQueryEngine) SubtreeNodes(ctx context.Context, v string) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("subtree nodes: ctx must not be nil")
	}

	// Get subtree range
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SubtreeQueryEngine.SubtreeNodes")
	defer span.End()

	start, end, err := sqe.getSubtreeRange(ctx, v, span)
	if err != nil {
		return nil, fmt.Errorf("subtree nodes for node %q: %w", v, err)
	}

	subtreeSize := end - start + 1

	// Validate size before allocation (M-PRE-3)
	if subtreeSize <= 0 {
		return nil, fmt.Errorf("subtree nodes for node %q: invalid subtree size %d", v, subtreeSize)
	}

	// Pre-allocate exact size
	nodes := make([]string, subtreeSize)

	// Collect nodes based on mode
	if sqe.hld != nil {
		// Single tree mode
		for i := 0; i < subtreeSize; i++ {
			pos := start + i
			nodeIdx := sqe.hld.NodeAtPos(pos)
			nodeID, err := sqe.hld.IdxToNode(nodeIdx)
			if err != nil {
				return nil, fmt.Errorf("subtree nodes for node %q: getting node at pos %d: %w", v, pos, err)
			}
			nodes[i] = nodeID
		}
	} else if sqe.forest != nil {
		// Forest mode - need to find which tree this node is in
		treeHLD := sqe.forest.GetHLD(v)
		if treeHLD == nil {
			return nil, fmt.Errorf("subtree nodes for node %q: node not in forest", v)
		}

		// Get tree offset
		treeOffset, err := sqe.forest.GetTreeOffset(v)
		if err != nil {
			return nil, fmt.Errorf("subtree nodes for node %q: getting tree offset: %w", v, err)
		}

		// Collect nodes (positions are global, so we need tree-local positions)
		for i := 0; i < subtreeSize; i++ {
			globalPos := start + i
			treeLocalPos := globalPos - treeOffset
			nodeIdx := treeHLD.NodeAtPos(treeLocalPos)
			nodeID, err := treeHLD.IdxToNode(nodeIdx)
			if err != nil {
				return nil, fmt.Errorf("subtree nodes for node %q: getting node at pos %d: %w", v, globalPos, err)
			}
			nodes[i] = nodeID
		}
	} else {
		return nil, errors.New("subtree nodes: engine has neither hld nor forest (invalid state)")
	}

	return nodes, nil
}

// Validate checks that HLD and SegmentTree are compatible.
//
// Description:
//
//	Verifies:
//	- Exactly one of hld/forest is non-nil (L-PRE-2)
//	- HLD/Forest and SegmentTree have same node count
//	- All subtree ranges are valid (pos[v] + size[v] <= nodeCount)
//
// Algorithm:
//
//	Time:  O(V) - must check all nodes
//	Space: O(1)
//
// Outputs:
//   - error: Non-nil if validation fails
//
// Thread Safety: Safe for concurrent use (read-only).
func (sqe *SubtreeQueryEngine) Validate() error {
	// Check exactly one mode active (L-PRE-2)
	if (sqe.hld == nil) == (sqe.forest == nil) {
		return errors.New("subtree query engine validation: exactly one of hld or forest must be non-nil")
	}

	var nodeCount int

	if sqe.hld != nil {
		// Validate HLD
		if err := sqe.hld.Validate(); err != nil {
			return fmt.Errorf("subtree query engine validation: HLD invalid: %w", err)
		}
		nodeCount = sqe.hld.NodeCount()

		// Verify all subtree ranges are valid
		for v := 0; v < nodeCount; v++ {
			pos := sqe.hld.Pos(v)
			size := sqe.hld.SubtreeSize(v)
			end := pos + size

			if end > nodeCount {
				return fmt.Errorf("subtree query engine validation: node %d: subtree range [%d,%d) exceeds node count %d",
					v, pos, end, nodeCount)
			}

			if pos < 0 || pos >= nodeCount {
				return fmt.Errorf("subtree query engine validation: node %d: invalid position %d", v, pos)
			}

			if size < 1 {
				return fmt.Errorf("subtree query engine validation: node %d: invalid subtree size %d", v, size)
			}
		}
	} else {
		// Validate forest
		if err := sqe.forest.Validate(); err != nil {
			return fmt.Errorf("subtree query engine validation: forest invalid: %w", err)
		}
		nodeCount = sqe.forest.TotalNodes()
	}

	// Validate segment tree
	if err := sqe.segTree.Validate(); err != nil {
		return fmt.Errorf("subtree query engine validation: segment tree invalid: %w", err)
	}

	// Check size match
	if nodeCount != sqe.segTree.size {
		return fmt.Errorf("subtree query engine validation: node count %d != segment tree size %d",
			nodeCount, sqe.segTree.size)
	}

	return nil
}

// Stats returns query statistics.
//
// Description:
//
//	Provides query count, latency metrics, and average subtree size.
//	Thread-safe via mutex locking.
//
// Algorithm:
//
//	Time:  O(1)
//	Space: O(1)
//
// Outputs:
//
//	SubtreeQueryStats with current statistics
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) Stats() SubtreeQueryStats {
	sqe.mu.Lock()
	defer sqe.mu.Unlock()

	avgLatency := time.Duration(0)
	avgSubtreeSize := float64(0)

	if sqe.queryCount > 0 {
		avgLatency = sqe.totalLatency / time.Duration(sqe.queryCount)
		avgSubtreeSize = float64(sqe.totalSubtreeSize) / float64(sqe.queryCount)
	}

	return SubtreeQueryStats{
		QueryCount:     sqe.queryCount,
		TotalLatency:   sqe.totalLatency,
		AvgLatency:     avgLatency,
		AvgSubtreeSize: avgSubtreeSize,
	}
}

// mode returns "single" or "forest" for logging/observability.
func (sqe *SubtreeQueryEngine) mode() string {
	if sqe.hld != nil {
		return "single"
	}
	return "forest"
}

// ClearCache clears the subtree range cache.
//
// Description:
//
//	Clears all cached subtree ranges. Useful for long-lived engines
//	that have queried many unique nodes and want to free memory.
//
// Algorithm:
//
//	Time:  O(n) where n is number of cached entries
//	Space: O(1)
//
// Limitations:
//
//	Cache entries are only 16 bytes each, so this is rarely needed.
//	Clearing the cache will cause next queries to recompute ranges.
//
// Thread Safety: Safe for concurrent use.
func (sqe *SubtreeQueryEngine) ClearCache() {
	sqe.rangeCache.Range(func(key, value interface{}) bool {
		sqe.rangeCache.Delete(key)
		return true
	})
}

// ============================================================================
// SUBTREE UPDATE ENGINE
// ============================================================================

// SubtreeUpdateEngine extends SubtreeQueryEngine with update operations.
//
// Description:
//
//	Provides O(log V) subtree updates using single range update.
//	All nodes in subtree(v) updated with one segment tree RangeUpdate.
//
// Thread Safety:
//
//	NOT safe for concurrent updates. Caller must synchronize writes.
//	Safe for concurrent reads (queries).
type SubtreeUpdateEngine struct {
	*SubtreeQueryEngine // Embed query engine

	// Update statistics (protected by mutex)
	mu                 sync.Mutex
	updateCount        int64
	totalUpdateLatency time.Duration
	nodesUpdated       int64
}

// SubtreeUpdateStats provides update statistics.
type SubtreeUpdateStats struct {
	UpdateCount        int64
	TotalUpdateLatency time.Duration
	AvgUpdateLatency   time.Duration
	NodesUpdated       int64
}

// NewSubtreeUpdateEngine creates a subtree update engine.
//
// Description:
//
//	Wraps a SubtreeQueryEngine to add update operations.
//	The query engine must already be constructed and valid.
//
// Algorithm:
//
//	Time:  O(1) - wrap reference
//	Space: O(1)
//
// Inputs:
//   - sqe: Subtree query engine. Must not be nil.
//
// Outputs:
//   - *SubtreeUpdateEngine: Ready for updates. Never nil on success.
//   - error: Non-nil if query engine is nil.
//
// Example:
//
//	queryEngine, _ := NewSubtreeQueryEngine(hld, segTree, AggregateSUM, nil, "")
//	updateEngine, _ := NewSubtreeUpdateEngine(queryEngine)
//
// Thread Safety: Safe for concurrent use (wraps read-only engine).
func NewSubtreeUpdateEngine(sqe *SubtreeQueryEngine) (*SubtreeUpdateEngine, error) {
	if sqe == nil {
		return nil, errors.New("subtree update engine: query engine must not be nil")
	}

	return &SubtreeUpdateEngine{
		SubtreeQueryEngine: sqe,
	}, nil
}

// SubtreeUpdate adds delta to all node values in subtree(v).
//
// Description:
//
//	Uses contiguous subtree range to perform single range update.
//	Much more efficient than updating each node individually.
//
// Algorithm:
//
//	Time:  O(log V) - single range update with lazy propagation
//	Space: O(1) - no allocations
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//   - delta: Value to add to each node in subtree.
//
// Outputs:
//   - error: Non-nil if node doesn't exist or update fails.
//
// Example:
//
//	// Increment complexity for entire package by 10
//	err := engine.SubtreeUpdate(ctx, "pkg/parser", 10)
//	if err != nil {
//		return fmt.Errorf("subtree update: %w", err)
//	}
//
// CRS Integration:
//
//	Records update as TraceStep with node ID, delta, subtree size.
//
// Thread Safety: NOT safe for concurrent updates.
func (sue *SubtreeUpdateEngine) SubtreeUpdate(ctx context.Context, v string, delta int64) error {
	// Input validation
	if ctx == nil {
		return errors.New("subtree update: ctx must not be nil")
	}

	// Check context cancellation (H-PRE-1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Start OTel span
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SubtreeUpdateEngine.SubtreeUpdate",
		trace.WithAttributes(
			attribute.String("node", v),
			attribute.Int64("delta", delta),
		),
	)
	defer span.End()

	start := time.Now()

	// Get subtree range
	rangeStart, rangeEnd, err := sue.getSubtreeRange(ctx, v, span)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get subtree range")
		return fmt.Errorf("subtree update for node %q: %w", v, err)
	}

	subtreeSize := rangeEnd - rangeStart + 1

	span.AddEvent("updating_segment_tree",
		trace.WithAttributes(
			attribute.Int("start", rangeStart),
			attribute.Int("end", rangeEnd),
			attribute.Int("size", subtreeSize),
			attribute.Int64("delta", delta),
		),
	)

	// Perform range update
	err = sue.segTree.RangeUpdate(ctx, rangeStart, rangeEnd, delta)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "range update failed")
		return fmt.Errorf("subtree update for node %q: range update [%d,%d]: %w",
			v, rangeStart, rangeEnd, err)
	}

	duration := time.Since(start)

	// Update stats
	sue.mu.Lock()
	sue.updateCount++
	sue.totalUpdateLatency += duration
	sue.nodesUpdated += int64(subtreeSize)
	sue.mu.Unlock()

	// Record metrics
	subtreeUpdateDuration.Observe(duration.Seconds())

	// CRS recording (H-PRE-6, M-PRE-7, C-POST-1)
	if sue.crs != nil && sue.sessionID != "" {
		step := crs.StepRecord{
			SessionID:     sue.sessionID,
			StepNumber:    int(atomic.AddInt64(&sue.stepNumber, 1)),
			Timestamp:     time.Now().UnixMilli(),
			Actor:         crs.ActorSystem,
			Decision:      crs.DecisionExecuteTool,
			Tool:          "SubtreeUpdate",
			ToolParams:    &crs.ToolParams{Target: v, Query: fmt.Sprintf("delta=%d", delta)},
			Outcome:       crs.OutcomeSuccess,
			DurationMs:    duration.Milliseconds(),
			ResultSummary: fmt.Sprintf("SubtreeUpdate(%s,delta=%d) updated %d nodes", v, delta, subtreeSize),
			Propagate:     false,
			Terminal:      false,
		}

		if err := sue.crs.RecordStep(ctx, step); err != nil {
			sue.logger.Warn("failed to record CRS step",
				slog.String("error", err.Error()),
				slog.String("tool", "SubtreeUpdate"),
			)
		}
	}

	// Large update warning (M-POST-3)
	if subtreeSize > 10000 {
		sue.logger.Warn("large subtree update",
			slog.String("node", v),
			slog.Int("size", subtreeSize),
			slog.Int64("delta", delta),
		)
	}

	span.SetAttributes(
		attribute.Int("subtree_size", subtreeSize),
		attribute.Int64("duration_us", duration.Microseconds()),
	)
	span.SetStatus(codes.Ok, "subtree update complete")

	return nil
}

// SubtreeSet sets all node values in subtree(v) to a specific value.
//
// Description:
//
//	Sets each node in subtree to value.
//	Implementation iterates through positions since different nodes
//	have different current values (cannot use single RangeUpdate).
//
// Algorithm:
//
//	Time:  O(size[v]) - must update each node individually
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - v: Root node of subtree. Must exist in graph.
//   - value: Value to set for all nodes in subtree.
//
// Outputs:
//   - error: Non-nil if node doesn't exist or update fails.
//
// Note:
//
//	This is slower than SubtreeUpdate because nodes have different
//	current values, so we cannot compute a single delta for all.
//
// Thread Safety: NOT safe for concurrent updates.
func (sue *SubtreeUpdateEngine) SubtreeSet(ctx context.Context, v string, value int64) error {
	if ctx == nil {
		return errors.New("subtree set: ctx must not be nil")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SubtreeUpdateEngine.SubtreeSet")
	defer span.End()

	start := time.Now()

	// Get subtree range
	rangeStart, rangeEnd, err := sue.getSubtreeRange(ctx, v, span)
	if err != nil {
		return fmt.Errorf("subtree set for node %q: %w", v, err)
	}

	subtreeSize := rangeEnd - rangeStart + 1

	// Update each position individually
	for pos := rangeStart; pos <= rangeEnd; pos++ {
		err := sue.segTree.Update(ctx, pos, value)
		if err != nil {
			return fmt.Errorf("subtree set for node %q: update pos %d: %w", v, pos, err)
		}
	}

	duration := time.Since(start)

	// Update stats
	sue.mu.Lock()
	sue.updateCount++
	sue.totalUpdateLatency += duration
	sue.nodesUpdated += int64(subtreeSize)
	sue.mu.Unlock()

	// CRS recording (C-POST-1)
	if sue.crs != nil && sue.sessionID != "" {
		step := crs.StepRecord{
			SessionID:     sue.sessionID,
			StepNumber:    int(atomic.AddInt64(&sue.stepNumber, 1)),
			Timestamp:     time.Now().UnixMilli(),
			Actor:         crs.ActorSystem,
			Decision:      crs.DecisionExecuteTool,
			Tool:          "SubtreeSet",
			ToolParams:    &crs.ToolParams{Target: v, Query: fmt.Sprintf("value=%d", value)},
			Outcome:       crs.OutcomeSuccess,
			DurationMs:    duration.Milliseconds(),
			ResultSummary: fmt.Sprintf("SubtreeSet(%s,value=%d) set %d nodes", v, value, subtreeSize),
			Propagate:     false,
			Terminal:      false,
		}

		if err := sue.crs.RecordStep(ctx, step); err != nil {
			sue.logger.Warn("failed to record CRS step",
				slog.String("error", err.Error()),
				slog.String("tool", "SubtreeSet"),
			)
		}
	}

	span.SetStatus(codes.Ok, "subtree set complete")
	return nil
}

// SubtreeIncrement increments all values in subtree (delta=+1).
//
// Description:
//
//	Convenience wrapper for SubtreeUpdate with delta=1.
//
// Thread Safety: NOT safe for concurrent updates.
func (sue *SubtreeUpdateEngine) SubtreeIncrement(ctx context.Context, v string) error {
	return sue.SubtreeUpdate(ctx, v, 1)
}

// SubtreeDecrement decrements all values in subtree (delta=-1).
//
// Description:
//
//	Convenience wrapper for SubtreeUpdate with delta=-1.
//
// Thread Safety: NOT safe for concurrent updates.
func (sue *SubtreeUpdateEngine) SubtreeDecrement(ctx context.Context, v string) error {
	return sue.SubtreeUpdate(ctx, v, -1)
}

// UpdateStats returns update statistics.
//
// Description:
//
//	Provides update count, latency metrics, and total nodes updated.
//
// Algorithm:
//
//	Time:  O(1)
//	Space: O(1)
//
// Outputs:
//
//	SubtreeUpdateStats with current statistics
//
// Thread Safety: Safe for concurrent use.
func (sue *SubtreeUpdateEngine) UpdateStats() SubtreeUpdateStats {
	sue.mu.Lock()
	defer sue.mu.Unlock()

	avgLatency := time.Duration(0)
	if sue.updateCount > 0 {
		avgLatency = sue.totalUpdateLatency / time.Duration(sue.updateCount)
	}

	return SubtreeUpdateStats{
		UpdateCount:        sue.updateCount,
		TotalUpdateLatency: sue.totalUpdateLatency,
		AvgUpdateLatency:   avgLatency,
		NodesUpdated:       sue.nodesUpdated,
	}
}
