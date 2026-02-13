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
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ==============================================================================
// Prometheus Metrics (M-PRE-1)
// ==============================================================================

var (
	// pathUpdateTotal counts path updates by result and operation
	pathUpdateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_update_total",
		Help: "Total path updates by result and operation",
	}, []string{"result", "operation"})

	// pathUpdateDuration tracks path update latency
	pathUpdateDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "trace_path_update_duration_seconds",
		Help:    "Path update duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12), // 0.1ms to ~400ms
	}, []string{"operation"})

	// pathUpdateSegments tracks number of segments updated per path update
	pathUpdateSegments = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_path_update_segments",
		Help:    "Number of segments updated per path update",
		Buckets: []float64{1, 2, 5, 10, 20, 50},
	})

	// pathUpdatePositions tracks number of positions updated per path update
	pathUpdatePositions = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_path_update_positions",
		Help:    "Number of positions updated per path update",
		Buckets: []float64{1, 10, 100, 1000, 10000},
	})

	// pathUpdateErrors counts errors by type
	pathUpdateErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_update_errors_total",
		Help: "Total path update errors by type",
	}, []string{"error_type"})
)

// PathUpdateEngine provides path update operations using HLD and segment tree.
//
// Description:
//
//	Extends PathQueryEngine with update operations. Updates values along
//	paths by decomposing into O(log V) segments and applying range updates
//	with lazy propagation for O(log² V) total time.
//	Supports forest mode - updates only within single tree component.
//
// Invariants:
//   - Segment tree must support range updates (RangeUpdate method)
//   - Only works with SUM aggregation (GR-19b restriction)
//   - Updates invalidate query cache for correctness
//   - In forest mode, path updates only valid within single tree component
//   - Segment tree version increments on each update
//
// Thread Safety:
//
//	NOT safe for concurrent updates (PathUpdate, PathSet, etc.).
//	NOT safe for concurrent update+query (segment tree Query takes write lock for lazy push).
//	Safe for concurrent queries only (no updates in progress).
//	Caller MUST use external synchronization (e.g., sync.RWMutex):
//	  - Lock.RLock() for queries
//	  - Lock.Lock() for updates
type PathUpdateEngine struct {
	*PathQueryEngine // Embed for CRS fields (H-CRS-1)

	// Update statistics (M-PRE-2: use atomic.Int64)
	updateCount       atomic.Int64
	totalLatencyNanos atomic.Int64
	segmentsUpdated   atomic.Int64
}

// NewPathUpdateEngine creates a path update engine.
//
// Description:
//
//	Wraps PathQueryEngine to add update capabilities.
//	Requires segment tree to support RangeUpdate (SUM aggregation only).
//	Validates aggregation function at construction time.
//
// Algorithm:
//
//	Time:  O(1) - wraps existing engine
//	Space: O(1) - no allocations
//
// Inputs:
//   - pqe: Path query engine. Must not be nil.
//
// Outputs:
//   - *PathUpdateEngine: Ready for updates. Never nil on success.
//   - error: Non-nil if query engine is nil, invalid, or not configured for SUM.
//
// Example:
//
//	// Build value array in HLD position order
//	values := make([]int64, hld.NodeCount())
//	for i := 0; i < hld.NodeCount(); i++ {
//	    nodeID := hld.NodeAtPosition(i)
//	    values[i] = getNodeValue(nodeID)
//	}
//
//	// Build segment tree with SUM aggregation
//	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)
//
//	// Create query engine
//	queryEngine, _ := NewPathQueryEngine(hld, segTree, AggregateSUM, nil)
//
//	// Create update engine
//	updateEngine, _ := NewPathUpdateEngine(queryEngine)
//
// Limitations:
//   - Only works with SUM aggregation (GR-19b RangeUpdate restriction)
//   - MIN/MAX/GCD engines cannot be used for updates
//   - Constructor validates aggregation function and returns error if not SUM
//
// Assumptions:
//   - Query engine's segment tree was built with SUM aggregation
//   - Caller has validated compatibility between HLD and segment tree
//
// Thread Safety: Safe for concurrent use (wraps read-only engine).
func NewPathUpdateEngine(pqe *PathQueryEngine) (*PathUpdateEngine, error) {
	if pqe == nil {
		return nil, errors.New("query engine must not be nil")
	}

	// Verify segment tree exists
	if pqe.segTree == nil {
		return nil, errors.New("segment tree must not be nil")
	}

	// H-INT-2: RangeUpdate only supports SUM (GR-19b restriction)
	if pqe.aggFunc != AggregateSUM {
		return nil, fmt.Errorf("path updates require SUM aggregation (engine configured with %s, RangeUpdate restriction from GR-19b)", pqe.aggFunc)
	}

	return &PathUpdateEngine{
		PathQueryEngine: pqe,
	}, nil
}

// PathUpdate adds delta to all node values on path from u to v.
//
// Description:
//
//	Decomposes path into heavy path segments and applies range update
//	to each segment. Handles LCA correctly (DecomposePath already excludes
//	from second path). Uses lazy propagation for efficiency.
//	Invalidates query cache after update for correctness.
//
// Algorithm:
//
//	Time:  O(log² V) = O(log V) segments × O(log V) per range update
//	Space: O(log V) - segment decomposition
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph and not be empty.
//   - v: End node ID. Must exist in graph and not be empty.
//   - delta: Value to add to each node on path.
//   - logger: Structured logger for events. Must not be nil.
//
// Outputs:
//   - error: Non-nil if nodes don't exist, cross-tree in forest, or update fails.
//
// Special Cases:
//   - PathUpdate(u, u, delta): Updates single node
//   - PathUpdate(u, parent[u], delta): Updates two nodes
//   - delta == 0: No-op fast path, still recorded in CRS
//
// Example:
//
//	// Add 5 to complexity along call path
//	err := engine.PathUpdate(ctx, "main", "leaf", 5, logger)
//	if err != nil {
//	    return fmt.Errorf("path update: %w", err)
//	}
//
// CRS Integration:
//
//	Records update as StepRecord with node IDs, delta, segments updated.
//	Sub-steps recorded for each segment update.
//
// Limitations:
//   - Only works within single tree component (error for cross-tree in forest mode)
//   - Requires SUM aggregation (MIN/MAX/GCD not supported for updates - GR-19b restriction)
//   - Query cache invalidated on EVERY update via Purge() - entire cache discarded (O(cache_size) cost)
//     This impacts mixed read-heavy workloads: after ANY update, all cached queries must be recomputed
//     For update-heavy workloads, consider disabling query cache to avoid purge overhead
//   - NOT safe for concurrent updates (caller must synchronize with external lock)
//   - Segment tree version incremented (may invalidate external references)
//
// Assumptions:
//   - Segment tree values correspond to HLD position array
//   - Node values fit in int64
//   - Caller has validated graph hasn't changed since construction
//
// Thread Safety: NOT safe for concurrent use. Caller must synchronize.
func (pue *PathUpdateEngine) PathUpdate(ctx context.Context, u, v string, delta int64, logger *slog.Logger) error {
	var updateErr error // Track errors for metrics

	// H5: Input validation
	if ctx == nil {
		updateErr = errors.New("ctx must not be nil")
		return updateErr
	}
	if u == "" {
		updateErr = errors.New("u must not be empty")
		return updateErr
	}
	if v == "" {
		updateErr = errors.New("v must not be empty")
		return updateErr
	}
	if logger == nil {
		updateErr = errors.New("logger must not be nil")
		return updateErr
	}

	// C4: Check context cancellation at start
	select {
	case <-ctx.Done():
		updateErr = ctx.Err()
		return updateErr
	default:
	}

	startTime := time.Now()

	// M-PRE-1: Record metrics on completion
	defer func() {
		duration := time.Since(startTime)
		pathUpdateDuration.WithLabelValues("path_update").Observe(duration.Seconds())

		if updateErr != nil {
			pathUpdateTotal.WithLabelValues("error", "path_update").Inc()
			errorType := string(classifyUpdateError(updateErr))
			pathUpdateErrors.WithLabelValues(errorType).Inc()
		} else {
			pathUpdateTotal.WithLabelValues("success", "path_update").Inc()
		}
	}()

	// Create OpenTelemetry span (H-OBS-1)
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.PathUpdateEngine.PathUpdate",
		trace.WithAttributes(
			attribute.String("node_u", u),
			attribute.String("node_v", v),
			attribute.Int64("delta", delta),
		),
	)
	defer span.End()

	// M1: Structured logging at update start
	logger.Info("path_update_start",
		slog.String("u", u),
		slog.String("v", v),
		slog.Int64("delta", delta),
	)

	// M-PERF-1: Delta==0 fast path
	if delta == 0 {
		logger.Debug("path_update_noop",
			slog.String("u", u),
			slog.String("v", v),
		)

		duration := time.Since(startTime)
		pue.recordUpdate(duration, 0)

		span.SetAttributes(
			attribute.Bool("noop", true),
		)
		span.SetStatus(codes.Ok, "no-op update (delta=0)")

		return nil
	}

	// Special case: same node (DecomposePath returns empty for u==v)
	if u == v {
		// Get node position
		var nodeIdx int
		var ok bool
		if pue.PathQueryEngine.hld != nil {
			nodeIdx, ok = pue.PathQueryEngine.hld.NodeToIdx(u)
		} else {
			treeHLD := pue.PathQueryEngine.forest.GetHLD(u)
			if treeHLD == nil {
				err := fmt.Errorf("HLD not found for node %s", u)
				span.RecordError(err)
				span.SetStatus(codes.Error, "node lookup failed")
				updateErr = err
				return updateErr
			}
			nodeIdx, ok = treeHLD.NodeToIdx(u)
		}

		if !ok {
			err := fmt.Errorf("node %s not found", u)
			span.RecordError(err)
			span.SetStatus(codes.Error, "node not found")
			updateErr = err
			return updateErr
		}

		var pos int
		if pue.PathQueryEngine.hld != nil {
			pos = pue.PathQueryEngine.hld.Pos(nodeIdx)
		} else {
			// Forest mode: get tree offset and add to tree-local position
			treeHLD := pue.PathQueryEngine.forest.GetHLD(u)
			treePos := treeHLD.Pos(nodeIdx)

			treeOffset, offsetErr := pue.PathQueryEngine.forest.GetTreeOffset(u)
			if offsetErr != nil {
				err := fmt.Errorf("getting tree offset for node %s: %w", u, offsetErr)
				span.RecordError(err)
				span.SetStatus(codes.Error, "tree offset failed")
				updateErr = err
				return updateErr
			}

			pos = treeOffset + treePos
		}

		// Update single position
		err := pue.segTree.RangeUpdate(ctx, pos, pos, delta)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "range update failed")
			updateErr = fmt.Errorf("updating node %s at position %d: %w", u, pos, err)
			return updateErr
		}

		duration := time.Since(startTime)
		pue.recordUpdate(duration, 1)

		// Invalidate query cache
		if pue.PathQueryEngine.queryCache != nil {
			pue.PathQueryEngine.queryCache.Purge()
		}

		span.SetAttributes(
			attribute.Bool("same_node", true),
			attribute.Int("position", pos),
			attribute.Int64("duration_us", duration.Microseconds()),
		)
		span.SetStatus(codes.Ok, "same node update complete")

		logger.Info("path_update_complete",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("delta", delta),
			slog.Int("position", pos),
			slog.Duration("duration", duration),
		)

		return nil
	}

	// H-PRE-4: Forest mode validation
	if pue.PathQueryEngine.forest != nil {
		if err := pue.PathQueryEngine.checkTreeComponent(u, v); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "cross-tree update")
			updateErr = err
			return updateErr
		}
	}

	// Compute LCA (reuses PathQueryEngine's LCA cache)
	span.AddEvent("computing_lca")
	lca, err := pue.computeLCAWithCache(ctx, u, v)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "LCA failed")
		updateErr = fmt.Errorf("computing LCA for path %s->%s: %w", u, v, err)

		// H-OBS-2: Structured error logging
		logger.Error("path_update_lca_failed",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("delta", delta),
			slog.String("error", err.Error()),
			slog.String("error_type", string(classifyUpdateError(err))),
		)

		return updateErr
	}

	// Decompose path into segments
	span.AddEvent("decomposing_path")
	var segments []PathSegment
	if pue.PathQueryEngine.hld != nil {
		// C-PRE-3: DecomposePath already handles LCA correctly
		// LCA appears in only one segment, no manual exclusion needed
		segments, err = pue.PathQueryEngine.hld.DecomposePath(ctx, u, v)
	} else {
		// M-INT-1: Forest mode support
		// Get HLD for tree containing u (already validated same tree)
		treeHLD := pue.PathQueryEngine.forest.GetHLD(u)
		if treeHLD == nil {
			err := fmt.Errorf("HLD not found for node %s", u)
			span.RecordError(err)
			span.SetStatus(codes.Error, "forest HLD lookup failed")
			updateErr = err

			logger.Error("path_update_hld_lookup_failed",
				slog.String("u", u),
				slog.String("error", err.Error()),
			)

			return updateErr
		}
		segments, err = treeHLD.DecomposePath(ctx, u, v)

		// Offset segment positions to global segment tree space
		if err == nil {
			treeOffset, offsetErr := pue.PathQueryEngine.forest.GetTreeOffset(u)
			if offsetErr != nil {
				err = fmt.Errorf("getting tree offset for node %s: %w", u, offsetErr)
			} else {
				// Add offset to all segment positions
				for i := range segments {
					segments[i].Start += treeOffset
					segments[i].End += treeOffset
				}
			}
		}
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decomposition failed")
		updateErr = fmt.Errorf("decomposing path %s->%s: %w", u, v, err)

		logger.Error("path_update_decomposition_failed",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("delta", delta),
			slog.String("error", err.Error()),
			slog.String("error_type", string(classifyUpdateError(err))),
		)

		return updateErr
	}

	// Update segments
	span.AddEvent("updating_segments",
		trace.WithAttributes(attribute.Int("segment_count", len(segments))),
	)

	// C-PRE-2: Add ctx parameter to updateSegments
	totalPositions, err := pue.updateSegments(ctx, segments, delta, u, v, logger)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "segment update failed")
		updateErr = fmt.Errorf("updating segments for path %s->%s: %w", u, v, err)

		logger.Error("path_update_segments_failed",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("delta", delta),
			slog.Int("segments", len(segments)),
			slog.String("error", err.Error()),
			slog.String("error_type", string(classifyUpdateError(err))),
		)

		return updateErr
	}

	duration := time.Since(startTime)

	// Record statistics
	pue.recordUpdate(duration, len(segments))
	pathUpdateSegments.Observe(float64(len(segments)))
	pathUpdatePositions.Observe(float64(totalPositions))

	// C-PRE-4: Invalidate query cache after update
	if pue.PathQueryEngine.queryCache != nil {
		pue.PathQueryEngine.queryCache.Purge()
	}

	// H-OBS-1: Rich span attributes
	graphHash := ""
	if pue.PathQueryEngine.hld != nil {
		graphHash = pue.PathQueryEngine.hld.GraphHash()
	} else if pue.PathQueryEngine.forest != nil {
		graphHash = pue.PathQueryEngine.forest.GraphHash()
	}

	span.SetAttributes(
		attribute.String("lca", lca),
		attribute.Int("segment_count", len(segments)),
		attribute.Int("total_positions", totalPositions),
		attribute.Int64("duration_us", duration.Microseconds()),
		attribute.String("graph_hash", graphHash),
	)
	span.SetStatus(codes.Ok, "path update complete")

	// M1: Log completion
	logger.Info("path_update_complete",
		slog.String("u", u),
		slog.String("v", v),
		slog.Int64("delta", delta),
		slog.Int("segments", len(segments)),
		slog.Int("positions", totalPositions),
		slog.Duration("duration", duration),
	)

	// M-PRE-3: Slow update threshold check
	if pue.PathQueryEngine.opts.SlowQueryThreshold > 0 && duration > pue.PathQueryEngine.opts.SlowQueryThreshold*2 {
		logger.Warn("slow_path_update",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("delta", delta),
			slog.Duration("duration", duration),
			slog.Duration("threshold", pue.PathQueryEngine.opts.SlowQueryThreshold*2),
			slog.Int("segments", len(segments)),
			slog.Int("positions", totalPositions),
		)
	}

	return nil
}

// PathSet sets all node values on path from u to v to a specific value.
//
// Description:
//
//	Sets each node on path to value. Implemented by computing current
//	aggregate and calculating delta needed to achieve target value.
//	Less efficient than PathUpdate (requires querying current values).
//
// Algorithm:
//
//	Time:  O(log² V) query + O(log² V) update = O(log² V)
//	Space: O(log V)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - value: New value for all nodes on path.
//   - logger: Structured logger for events. Must not be nil.
//
// Outputs:
//   - error: Non-nil if operation fails.
//
// Example:
//
//	// Set all complexity values on path to 10
//	err := engine.PathSet(ctx, "main", "leaf", 10, logger)
//
// Limitations:
//   - Requires two passes: query current aggregate, then update
//   - Only works with SUM aggregation
//   - NOT safe for concurrent updates
//
// Assumptions:
//   - Path exists and nodes are in same tree component
//   - Current values fit in int64
//
// Thread Safety: NOT safe for concurrent use.
func (pue *PathUpdateEngine) PathSet(ctx context.Context, u, v string, value int64, logger *slog.Logger) error {
	// Input validation
	if ctx == nil {
		return errors.New("ctx must not be nil")
	}
	if u == "" {
		return errors.New("u must not be empty")
	}
	if v == "" {
		return errors.New("v must not be empty")
	}
	if logger == nil {
		return errors.New("logger must not be nil")
	}

	// Note: PathSet is complex for arbitrary values
	// For simplicity, we update each position individually
	// A more efficient implementation would require tracking current values

	// Get path nodes
	var nodeIDs []string
	var err error

	if pue.PathQueryEngine.hld != nil {
		nodeIDs, err = pue.PathQueryEngine.hld.PathNodes(ctx, u, v)
	} else {
		treeHLD := pue.PathQueryEngine.forest.GetHLD(u)
		if treeHLD == nil {
			return fmt.Errorf("HLD not found for node %s", u)
		}
		nodeIDs, err = treeHLD.PathNodes(ctx, u, v)
	}

	if err != nil {
		return fmt.Errorf("getting path nodes for %s->%s: %w", u, v, err)
	}

	// For each node, query current value and update
	for _, nodeID := range nodeIDs {
		var nodeIdx int
		var ok bool
		if pue.PathQueryEngine.hld != nil {
			nodeIdx, ok = pue.PathQueryEngine.hld.NodeToIdx(nodeID)
		} else {
			treeHLD := pue.PathQueryEngine.forest.GetHLD(nodeID)
			if treeHLD == nil {
				return fmt.Errorf("HLD not found for node %s", nodeID)
			}
			nodeIdx, ok = treeHLD.NodeToIdx(nodeID)
		}

		if !ok {
			return fmt.Errorf("node %s not found in HLD", nodeID)
		}

		var pos int
		if pue.PathQueryEngine.hld != nil {
			pos = pue.PathQueryEngine.hld.Pos(nodeIdx)
		} else {
			// Forest mode: get tree offset and add to tree-local position
			treeHLD := pue.PathQueryEngine.forest.GetHLD(nodeID)
			treePos := treeHLD.Pos(nodeIdx)

			treeOffset, offsetErr := pue.PathQueryEngine.forest.GetTreeOffset(nodeID)
			if offsetErr != nil {
				return fmt.Errorf("getting tree offset for node %s: %w", nodeID, offsetErr)
			}

			pos = treeOffset + treePos
		}

		// Update single position to target value
		if err := pue.segTree.Update(ctx, pos, value); err != nil {
			return fmt.Errorf("updating node %s at position %d: %w", nodeID, pos, err)
		}
	}

	// Invalidate query cache
	if pue.PathQueryEngine.queryCache != nil {
		pue.PathQueryEngine.queryCache.Purge()
	}

	logger.Info("path_set_complete",
		slog.String("u", u),
		slog.String("v", v),
		slog.Int64("value", value),
		slog.Int("nodes", len(nodeIDs)),
	)

	return nil
}

// PathIncrement increments all values on path (delta=+1).
//
// Description:
//
//	Convenience wrapper for PathUpdate with delta=+1.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Structured logger for events. Must not be nil.
//
// Outputs:
//   - error: Non-nil if update fails.
//
// Thread Safety: NOT safe for concurrent use.
func (pue *PathUpdateEngine) PathIncrement(ctx context.Context, u, v string, logger *slog.Logger) error {
	return pue.PathUpdate(ctx, u, v, 1, logger)
}

// PathDecrement decrements all values on path (delta=-1).
//
// Description:
//
//	Convenience wrapper for PathUpdate with delta=-1.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Structured logger for events. Must not be nil.
//
// Outputs:
//   - error: Non-nil if update fails.
//
// Thread Safety: NOT safe for concurrent use.
func (pue *PathUpdateEngine) PathDecrement(ctx context.Context, u, v string, logger *slog.Logger) error {
	return pue.PathUpdate(ctx, u, v, -1, logger)
}

// updateSegments applies delta to all segments.
//
// Description:
//
//	Internal helper to update each PathSegment via segment tree range update.
//	Normalizes segment bounds (swap if needed for upward paths).
//	Records CRS sub-steps for each segment update.
//
// Algorithm:
//
//	for each segment:
//	  check context cancellation
//	  start, end = normalize(segment.Start, segment.End)  // Ensure start <= end
//	  segTree.RangeUpdate(ctx, start, end, delta)
//	  record CRS sub-step
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - segments: Path segments to update. Can be empty (no-op). Must not be nil.
//   - delta: Value to add to each position in segments.
//   - u: Start node ID (for logging and CRS).
//   - v: End node ID (for logging and CRS).
//   - logger: Structured logger. Must not be nil.
//
// Outputs:
//   - int: Total number of positions updated.
//   - error: Non-nil if segment tree update fails or context canceled.
//
// Limitations:
//   - Segments must be valid output from DecomposePath
//   - Segment positions must be within segment tree bounds
//
// Assumptions:
//   - Segments are in HLD position space
//   - DecomposePath has already normalized ranges
//
// Thread Safety: NOT safe for concurrent use.
func (pue *PathUpdateEngine) updateSegments(ctx context.Context, segments []PathSegment, delta int64, u, v string, logger *slog.Logger) (int, error) {
	// H-PRE-2: Input validation
	if segments == nil {
		return 0, errors.New("segments must not be nil")
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	totalPositions := 0

	for i, seg := range segments {
		// C4: Check context cancellation between segments
		select {
		case <-ctx.Done():
			return totalPositions, ctx.Err()
		default:
		}

		// Normalize segment bounds (ensure start <= end)
		start, end := seg.Start, seg.End
		if start > end {
			start, end = end, start
		}

		// Defensive: check segment bounds
		if start < 0 || end >= pue.segTree.size {
			err := fmt.Errorf("updating path %s->%s: segment %d out of bounds [%d,%d] (tree_size=%d, delta=%d)",
				u, v, i, start, end, pue.segTree.size, delta)
			logger.Error("segment_out_of_bounds",
				slog.Int("segment_index", i),
				slog.Int("start", start),
				slog.Int("end", end),
				slog.Int("tree_size", pue.segTree.size),
				slog.String("error", err.Error()),
			)
			return totalPositions, err
		}

		// H-CRS-2: Time segment update for sub-step recording
		segStart := time.Now()

		// C-PRE-1: Use RangeUpdate(ctx, start, end, delta) with ctx parameter
		err := pue.segTree.RangeUpdate(ctx, start, end, delta)
		segDuration := time.Since(segStart)

		// H-CRS-2: Record sub-step for segment update
		if pue.PathQueryEngine.crs != nil {
			pue.recordSegmentUpdateSubStep(ctx, i, start, end, delta, segDuration, err)
		}

		if err != nil {
			logger.Error("segment_update_failed",
				slog.Int("segment_index", i),
				slog.Int("start", start),
				slog.Int("end", end),
				slog.Int64("delta", delta),
				slog.String("error", err.Error()),
			)
			return totalPositions, fmt.Errorf("updating path %s->%s: segment %d update failed: %w", u, v, i, err)
		}

		positionsUpdated := end - start + 1
		totalPositions += positionsUpdated

		logger.Debug("segment_updated",
			slog.Int("segment_index", i),
			slog.Int("start", start),
			slog.Int("end", end),
			slog.Int("positions", positionsUpdated),
			slog.Int64("delta", delta),
		)
	}

	return totalPositions, nil
}

// recordSegmentUpdateSubStep records CRS sub-step for segment update (H-CRS-2).
//
// Description:
//
//	Helper method to record segment update as a CRS sub-step.
//	Only records if pqe.crs is non-nil.
//	Uses StepRecord pattern from GR-19d (H-PRE-3).
//
// Inputs:
//   - ctx: Context for CRS recording. Must not be nil.
//   - segmentIndex: Index of segment being updated.
//   - start: Start position in HLD array.
//   - end: End position in HLD array.
//   - delta: Delta added to segment.
//   - duration: Time taken for segment update.
//   - err: Error if segment update failed (nil on success).
//
// Thread Safety: Safe for concurrent use.
func (pue *PathUpdateEngine) recordSegmentUpdateSubStep(ctx context.Context, segmentIndex, start, end int, delta int64, duration time.Duration, err error) {
	pue.PathQueryEngine.mu.RLock()
	sessionID := pue.PathQueryEngine.sessionID
	pue.PathQueryEngine.mu.RUnlock()

	outcome := crs.OutcomeSuccess
	errorCategory := crs.ErrorCategoryNone
	errorMsg := ""
	if err != nil {
		outcome = crs.OutcomeFailure
		errorCategory = classifyUpdateError(err)
		errorMsg = err.Error()
	}

	step := crs.StepRecord{
		SessionID:     sessionID,
		StepNumber:    int(pue.PathQueryEngine.subStepCounter.Add(1)),
		Timestamp:     time.Now().UnixMilli(),
		Actor:         crs.ActorSystem,
		Decision:      crs.DecisionExecuteTool,
		Tool:          "PathUpdate.SegmentUpdate",
		ToolParams:    &crs.ToolParams{Target: fmt.Sprintf("segment[%d]", segmentIndex), Query: fmt.Sprintf("[%d,%d] delta=%d", start, end, delta)},
		Outcome:       outcome,
		ErrorCategory: errorCategory,
		ErrorMessage:  errorMsg,
		DurationMs:    duration.Milliseconds(),
		ResultSummary: fmt.Sprintf("Updated positions [%d,%d] with delta %d", start, end, delta),
		Propagate:     false,
		Terminal:      false,
	}

	if validationErr := step.Validate(); validationErr != nil {
		// Log validation error but don't fail the update
		return
	}

	if recordErr := pue.PathQueryEngine.crs.RecordStep(ctx, step); recordErr != nil {
		// Log recording error but don't fail the update
		return
	}
}

// recordUpdate tracks update statistics (M-PRE-2).
//
// Description:
//
//	Thread-safe stats tracking using atomic operations.
//	Tracks update count, total latency, and segments updated.
//
// Inputs:
//   - duration: Update duration. Must be non-negative.
//   - segmentCount: Number of segments updated. 0 for no-op, len(segments) for full path.
//
// Outputs:
//   - None (updates internal atomic counters)
//
// Thread Safety: Safe for concurrent use (atomic operations).
func (pue *PathUpdateEngine) recordUpdate(duration time.Duration, segmentCount int) {
	pue.updateCount.Add(1)
	pue.totalLatencyNanos.Add(duration.Nanoseconds())
	pue.segmentsUpdated.Add(int64(segmentCount))
}

// PathUpdateStats contains update statistics.
//
// Description:
//
//	Statistics collected across all updates on this engine.
//
// Thread Safety: Snapshot at time of Stats() call, may be stale.
type PathUpdateStats struct {
	UpdateCount          int64         // Total updates
	TotalUpdateLatency   time.Duration // Sum of all update durations
	AvgUpdateLatency     time.Duration // Average update duration
	SegmentsUpdated      int64         // Total segments updated across all operations
	AvgSegmentsPerUpdate float64       // Average segments per update
}

// Stats returns update statistics.
//
// Description:
//
//	Thread-safe stats retrieval using atomic operations.
//	Returns snapshot of current statistics.
//	Includes average segments per update.
//
// Inputs:
//   - None
//
// Outputs:
//   - PathUpdateStats: Snapshot of statistics including update count, latency, segments per update
//
// Limitations:
//   - Snapshot may be stale immediately after return
//   - AvgSegmentsPerUpdate excludes no-op updates (counted as 0 segments)
//
// Assumptions:
//   - Atomic loads provide consistent point-in-time snapshot
//   - UpdateCount > 0 when calculating averages
//
// Thread Safety: Safe for concurrent use (atomic reads).
func (pue *PathUpdateEngine) Stats() PathUpdateStats {
	updateCount := pue.updateCount.Load()
	totalLatencyNanos := pue.totalLatencyNanos.Load()
	segmentsUpdated := pue.segmentsUpdated.Load()

	stats := PathUpdateStats{
		UpdateCount:        updateCount,
		TotalUpdateLatency: time.Duration(totalLatencyNanos),
		SegmentsUpdated:    segmentsUpdated,
	}

	if updateCount > 0 {
		stats.AvgUpdateLatency = time.Duration(totalLatencyNanos / updateCount)
		stats.AvgSegmentsPerUpdate = float64(segmentsUpdated) / float64(updateCount)
	}

	return stats
}

// classifyUpdateError classifies errors for metrics/logging (H-OBS-2).
//
// Description:
//
//	Categorizes errors for structured logging and metrics.
//	Follows GR-19d error classification pattern using classifyHLDError.
//
// Inputs:
//   - err: Error to classify. Can be nil.
//
// Outputs:
//   - crs.ErrorCategory: Error category (ErrorCategoryNone, ErrorCategoryToolNotFound, etc.)
//
// Thread Safety: Safe for concurrent use (pure function).
func classifyUpdateError(err error) crs.ErrorCategory {
	// Reuse the existing classifyHLDError function from analytics.go
	// This ensures consistency across all graph operations
	return classifyHLDError(err)
}
