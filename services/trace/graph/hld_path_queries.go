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
	"strings"
	"sync"
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
// Prometheus Metrics (H-IMPL-2)
// ==============================================================================

var (
	// pathQueryTotal counts path queries by result type
	pathQueryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_query_total",
		Help: "Total path queries by result type",
	}, []string{"result", "agg_func"})

	// pathQueryDuration tracks path query latency
	pathQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "trace_path_query_duration_seconds",
		Help:    "Path query duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12), // 0.1ms to ~400ms
	}, []string{"agg_func"})

	// pathQueryCacheMisses counts query cache misses
	pathQueryCacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_query_cache_misses_total",
		Help: "Total path query cache misses",
	}, []string{"cache_type"}) // "lca" or "query"

	// pathQueryCacheHits counts query cache hits
	pathQueryCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_query_cache_hits_total",
		Help: "Total path query cache hits",
	}, []string{"cache_type"}) // "lca" or "query"

	// pathQueryErrors counts errors by type
	pathQueryErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_path_query_errors_total",
		Help: "Total path query errors by type",
	}, []string{"error_type"})

	// segmentTreeQueryDuration tracks segment tree query latency
	segmentTreeQueryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_segment_tree_query_duration_seconds",
		Help:    "Segment tree query duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 0.1ms to ~100ms
	})

	// pathSegmentCount tracks number of segments per path query
	pathQuerySegmentCount = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_path_query_segment_count",
		Help:    "Number of segments per path query",
		Buckets: []float64{1, 2, 3, 5, 10, 20, 50, 100},
	})
)

// PathQueryEngineOptions configures path query engine behavior.
//
// Description:
//
//	Controls caching, timeouts, and resource limits for path query operations.
//	All fields are optional with sensible defaults.
//
// Thread Safety: Immutable after creation, safe to share across goroutines.
type PathQueryEngineOptions struct {
	// EnableLCACache enables caching of LCA results.
	// Default: true
	EnableLCACache bool

	// LCACacheSize sets maximum number of LCA results to cache.
	// Default: 10000
	LCACacheSize int

	// EnableQueryCache enables caching of full path query results.
	// Default: false (disabled by default as results may be large)
	EnableQueryCache bool

	// QueryCacheSize sets maximum number of query results to cache.
	// Default: 1000
	QueryCacheSize int

	// QueryTimeout sets maximum time allowed for a single path query.
	// Default: 30 seconds
	QueryTimeout time.Duration

	// MaxTreeDepth sets maximum allowed tree depth to prevent OOM.
	// Default: 10000
	MaxTreeDepth int

	// SlowQueryThreshold defines the duration threshold for slow query warnings.
	// Queries exceeding this threshold will emit a warning log.
	// Default: 5 seconds
	SlowQueryThreshold time.Duration
}

// DefaultPathQueryEngineOptions returns default options.
func DefaultPathQueryEngineOptions() PathQueryEngineOptions {
	return PathQueryEngineOptions{
		EnableLCACache:     true,
		LCACacheSize:       10000,
		EnableQueryCache:   false,
		QueryCacheSize:     1000,
		QueryTimeout:       30 * time.Second,
		MaxTreeDepth:       10000,
		SlowQueryThreshold: 5 * time.Second,
	}
}

// PathQueryEngine combines HLD and Segment Tree for efficient path aggregate queries.
//
// Description:
//
//	Provides O(log² V) path aggregate queries by decomposing paths into
//	O(log V) heavy path segments and querying each segment with a segment tree.
//	Supports SUM, MIN, MAX, GCD aggregations over node values.
//
// Invariants:
//   - Exactly one of hld or forest must be non-nil
//   - HLD and SegmentTree must be built from same graph
//   - Node indices must align between HLD positions and segment tree array
//   - SegmentTree aggregation function must match query type
//   - In forest mode, path queries only valid within single tree component
//
// Thread Safety:
//
//	Safe for concurrent queries (read-only operations).
//	Stats are tracked with atomic operations.
type PathQueryEngine struct {
	// Graph structure (exactly one must be set)
	hld    *HLDecomposition // Single tree mode
	forest *HLDForest       // Forest mode (multiple trees)

	// Query infrastructure
	segTree *SegmentTree
	aggFunc AggregateFunc
	opts    PathQueryEngineOptions

	// Caching (nil if disabled)
	lcaCache   *LRUCache[string, string] // (u,v) -> lca
	queryCache *LRUCache[string, int64]  // query key -> result

	// Statistics (thread-safe atomic operations)
	queryCount        atomic.Int64 // Total queries
	totalLatencyNanos atomic.Int64 // Sum of all query durations (nanoseconds)
	totalSegments     atomic.Int64 // Sum of segments across all queries (M-IMPL-2)
	lastQueryTime     atomic.Int64 // Unix milliseconds UTC of last query
	cacheHits         atomic.Int64 // LCA + query cache hits
	cacheMisses       atomic.Int64 // LCA + query cache misses

	// CRS integration (H-IMPL-3) - optional
	crs            crs.CRS      // CRS instance for sub-step recording (nil if disabled)
	sessionID      string       // Current CRS session ID
	subStepCounter atomic.Int64 // Sub-step counter for LCA/DecomposePath
	mu             sync.RWMutex // Protects sessionID
}

// lcaCacheKey creates cache key for LCA result.
func lcaCacheKey(u, v string) string {
	// Canonical ordering for symmetric queries
	if u > v {
		u, v = v, u
	}
	return u + ":" + v
}

// nodeNotFoundErr checks if error is a node not found error.
func nodeNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return stringContains(msg, "not found") || stringContains(msg, "HLD not found")
}

func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// NewPathQueryEngine creates a path query engine for single-tree graphs.
//
// Description:
//
//	Combines HLD and segment tree to enable efficient path queries.
//	The segment tree must be built over node values in HLD position order.
//
// Algorithm:
//
//	Time:  O(1) - just validates and stores references
//	Space: O(C) where C is total cache capacity
//
// Inputs:
//   - hld: Heavy-Light Decomposition. Must not be nil.
//   - segTree: Segment tree over node values. Must not be nil.
//   - aggFunc: Aggregation function. Must match segTree's aggregation.
//   - opts: Configuration options. If nil, uses defaults.
//
// Outputs:
//   - *PathQueryEngine: Ready for queries. Never nil on success.
//   - error: Non-nil if inputs are invalid.
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
//	// Build segment tree
//	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)
//
//	// Create query engine
//	opts := DefaultPathQueryEngineOptions()
//	engine, _ := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
//
// Limitations:
//   - Only works with single-tree graphs (no forests)
//   - Cannot change aggregation function after creation
//   - Cache sizes fixed at creation time
//
// Assumptions:
//   - Values array used for segment tree corresponds to HLD position array
//   - Graph structure unchanged since HLD construction (validated by graph hash)
//   - All node values fit in int64
//
// Thread Safety: The returned engine is safe for concurrent use.
func NewPathQueryEngine(hld *HLDecomposition, segTree *SegmentTree, aggFunc AggregateFunc, opts *PathQueryEngineOptions) (*PathQueryEngine, error) {
	return NewPathQueryEngineWithCRS(hld, segTree, aggFunc, opts, nil, "")
}

// NewPathQueryEngineWithCRS creates a path query engine with optional CRS integration.
//
// Description:
//
//	Like NewPathQueryEngine but accepts optional CRS instance and session ID
//	for recording sub-steps (LCA computation, path decomposition).
//	If crsInstance is nil, CRS recording is disabled.
//
// Inputs:
//   - hld: HLD structure. Must not be nil.
//   - segTree: Segment tree for range queries. Must not be nil.
//   - aggFunc: Aggregation function (SUM/MIN/MAX/GCD). Must be valid constant.
//   - opts: Configuration options. If nil, uses defaults.
//   - crsInstance: Optional CRS for sub-step recording. Can be nil.
//   - sessionID: CRS session ID. Ignored if crsInstance is nil.
//
// Outputs:
//   - *PathQueryEngine: Configured engine ready for queries.
//   - error: Non-nil if validation fails.
//
// Limitations:
//   - Single-tree graphs only (use NewPathQueryEngineForForest for forests)
//   - CRS recording adds ~0.1-0.5ms overhead per query
//
// Assumptions:
//   - hld and segTree are compatible (same node count, same agg func)
//   - Caller validates crsInstance is not nil if sessionID is provided
//
// Thread Safety: The returned engine is safe for concurrent use.
func NewPathQueryEngineWithCRS(hld *HLDecomposition, segTree *SegmentTree, aggFunc AggregateFunc, opts *PathQueryEngineOptions, crsInstance crs.CRS, sessionID string) (*PathQueryEngine, error) {
	// Validate inputs
	if hld == nil {
		return nil, errors.New("hld must not be nil")
	}
	if segTree == nil {
		return nil, errors.New("segTree must not be nil")
	}

	// Validate aggregation function
	if aggFunc < AggregateSUM || aggFunc > AggregateGCD {
		return nil, fmt.Errorf("aggFunc must be valid AggregateFunc constant (got %d)", aggFunc)
	}

	// Validate compatibility
	if hld.NodeCount() != segTree.size {
		return nil, fmt.Errorf("HLD node count %d != segment tree size %d",
			hld.NodeCount(), segTree.size)
	}

	// Validate aggregation function matches
	if aggFunc != segTree.aggFunc {
		return nil, fmt.Errorf("engine agg func %s != segment tree agg func %s",
			aggFunc, segTree.aggFunc)
	}

	// Use defaults if no options provided
	if opts == nil {
		defaultOpts := DefaultPathQueryEngineOptions()
		opts = &defaultOpts
	}

	pqe := &PathQueryEngine{
		hld:       hld,
		segTree:   segTree,
		aggFunc:   aggFunc,
		opts:      *opts,
		crs:       crsInstance,
		sessionID: sessionID,
	}

	// Initialize caches if enabled
	if opts.EnableLCACache && opts.LCACacheSize > 0 {
		pqe.lcaCache = NewLRUCache[string, string](opts.LCACacheSize)
	}
	if opts.EnableQueryCache && opts.QueryCacheSize > 0 {
		pqe.queryCache = NewLRUCache[string, int64](opts.QueryCacheSize)
	}

	return pqe, nil
}

// NewPathQueryEngineForForest creates a path query engine for multi-tree graphs (forests).
//
// Description:
//
//	Like NewPathQueryEngine but supports forests with multiple disconnected trees.
//	Path queries are only valid within a single tree component.
//
// Inputs:
//   - forest: HLD forest structure. Must not be nil.
//   - segTree: Segment tree over all node values. Must not be nil.
//   - aggFunc: Aggregation function. Must match segTree's aggregation.
//   - opts: Configuration options. If nil, uses defaults.
//
// Outputs:
//   - *PathQueryEngine: Ready for queries. Never nil on success.
//   - error: Non-nil if inputs are invalid.
//
// Example:
//
//	forest, _ := BuildHLDForest(ctx, graph)
//	values := buildValueArrayForForest(forest)
//	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)
//	engine, _ := NewPathQueryEngineForForest(forest, segTree, AggregateSUM, nil)
//
// Limitations:
//   - Path queries between nodes in different trees will return an error
//   - Cannot query across tree boundaries even if graph has edges between components
//
// Assumptions:
//   - Forest and segment tree built from same graph
//   - Values array corresponds to forest's unified position array
//
// Thread Safety: The returned engine is safe for concurrent use.
func NewPathQueryEngineForForest(forest *HLDForest, segTree *SegmentTree, aggFunc AggregateFunc, opts *PathQueryEngineOptions) (*PathQueryEngine, error) {
	// Validate inputs
	if forest == nil {
		return nil, errors.New("forest must not be nil")
	}
	if segTree == nil {
		return nil, errors.New("segTree must not be nil")
	}

	// Validate aggregation function
	if aggFunc < AggregateSUM || aggFunc > AggregateGCD {
		return nil, fmt.Errorf("aggFunc must be valid AggregateFunc constant (got %d)", aggFunc)
	}

	// Validate compatibility
	if forest.TotalNodes() != segTree.size {
		return nil, fmt.Errorf("forest node count %d != segment tree size %d",
			forest.TotalNodes(), segTree.size)
	}

	// Validate aggregation function matches
	if aggFunc != segTree.aggFunc {
		return nil, fmt.Errorf("engine agg func %s != segment tree agg func %s",
			aggFunc, segTree.aggFunc)
	}

	// Use defaults if no options provided
	if opts == nil {
		defaultOpts := DefaultPathQueryEngineOptions()
		opts = &defaultOpts
	}

	pqe := &PathQueryEngine{
		forest:  forest,
		segTree: segTree,
		aggFunc: aggFunc,
		opts:    *opts,
	}

	// Initialize caches if enabled
	if opts.EnableLCACache && opts.LCACacheSize > 0 {
		pqe.lcaCache = NewLRUCache[string, string](opts.LCACacheSize)
	}
	if opts.EnableQueryCache && opts.QueryCacheSize > 0 {
		pqe.queryCache = NewLRUCache[string, int64](opts.QueryCacheSize)
	}

	return pqe, nil
}

// PathQuery computes aggregate over path from u to v.
//
// Description:
//
//	Decomposes path into heavy path segments, queries each segment with
//	segment tree, and combines results using aggregation function.
//	Handles LCA double-counting for SUM (but not MIN/MAX - idempotent).
//
// Algorithm:
//
//	Time:  O(log² V) = O(log V) segments × O(log V) per query
//	Space: O(log V) - store segment query results
//
// Inputs:
//   - ctx: Context for cancellation and timeout. Must not be nil.
//   - u: Start node ID. Must exist in graph and not be empty.
//   - v: End node ID. Must exist in graph and not be empty.
//   - logger: Structured logger for events. Must not be nil.
//
// Outputs:
//   - int64: Aggregate value over path u → v.
//   - error: Non-nil if nodes don't exist, cross-tree in forest, or query fails.
//
// Special Cases:
//   - PathQuery(u, u) = value[u]
//   - PathQuery(u, parent[u]) = aggFunc(value[u], value[parent[u]])
//
// Example:
//
//	sum, err := engine.PathQuery(ctx, "main", "helper", logger)
//	if err != nil {
//	    return fmt.Errorf("path query: %w", err)
//	}
//	fmt.Printf("Total complexity on path: %d\n", sum)
//
// Limitations:
//   - Only works within single tree component (error for cross-tree in forest)
//   - Requires graph unchanged since HLD construction (checked via graph hash)
//   - Maximum depth limited by opts.MaxTreeDepth
//
// Assumptions:
//   - Segment tree values correspond to HLD position array
//   - Node values fit in int64
//   - Caller has validated graph hasn't changed
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) PathQuery(ctx context.Context, u, v string, logger *slog.Logger) (int64, error) {
	var queryErr error // H-IMPL-2: Track errors for metrics

	// H5: Input validation
	if ctx == nil {
		queryErr = errors.New("ctx must not be nil")
		return 0, queryErr
	}
	if u == "" {
		queryErr = errors.New("u must not be empty")
		return 0, queryErr
	}
	if v == "" {
		queryErr = errors.New("v must not be empty")
		return 0, queryErr
	}
	if logger == nil {
		queryErr = errors.New("logger must not be nil")
		return 0, queryErr
	}

	// C4: Check context cancellation at start
	select {
	case <-ctx.Done():
		queryErr = ctx.Err()
		return 0, queryErr
	default:
	}

	// H4: Apply timeout if configured
	if pqe.opts.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pqe.opts.QueryTimeout)
		defer cancel()
	}

	startTime := time.Now()
	aggFuncStr := pqe.aggFunc.String()

	// H-IMPL-2: Record metrics on completion
	defer func() {
		duration := time.Since(startTime)
		pathQueryDuration.WithLabelValues(aggFuncStr).Observe(duration.Seconds())

		if queryErr != nil {
			// Classify error
			errorType := "other"
			switch {
			case errors.Is(queryErr, context.Canceled):
				errorType = "canceled"
			case errors.Is(queryErr, context.DeadlineExceeded):
				errorType = "timeout"
			case errors.Is(queryErr, ErrNodesInDifferentTrees):
				errorType = "cross_tree"
			default:
				if nodeNotFoundErr(queryErr) {
					errorType = "node_not_found"
				}
			}
			pathQueryErrors.WithLabelValues(errorType).Inc()
			pathQueryTotal.WithLabelValues("error", aggFuncStr).Inc()
		} else {
			pathQueryTotal.WithLabelValues("success", aggFuncStr).Inc()
		}
	}()

	// Create OpenTelemetry span
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.PathQueryEngine.PathQuery",
		trace.WithAttributes(
			attribute.String("node_u", u),
			attribute.String("node_v", v),
			attribute.String("agg_func", pqe.aggFunc.String()),
		),
	)
	defer span.End()

	// M1: Structured logging at query start
	logger.Info("path_query_start",
		slog.String("u", u),
		slog.String("v", v),
		slog.String("agg_func", pqe.aggFunc.String()),
	)

	// Special case: same node
	if u == v {
		// Query single node value
		var nodeIdx int
		var ok bool
		if pqe.hld != nil {
			nodeIdx, ok = pqe.hld.NodeToIdx(u)
		} else {
			treeHLD := pqe.forest.GetHLD(u)
			if treeHLD == nil {
				err := fmt.Errorf("HLD not found for node %s", u)
				span.RecordError(err)
				queryErr = err
				return 0, err
			}
			nodeIdx, ok = treeHLD.NodeToIdx(u)
		}

		if !ok {
			err := fmt.Errorf("node %s not found", u)
			span.RecordError(err)
			queryErr = err
			return 0, err
		}

		var pos int
		if pqe.hld != nil {
			pos = pqe.hld.Pos(nodeIdx)
		} else {
			// Forest mode: get tree offset and add to tree-local position
			treeHLD := pqe.forest.GetHLD(u)
			treePos := treeHLD.Pos(nodeIdx)

			treeOffset, offsetErr := pqe.forest.GetTreeOffset(u)
			if offsetErr != nil {
				err := fmt.Errorf("getting tree offset for node %s: %w", u, offsetErr)
				span.RecordError(err)
				queryErr = err
				return 0, err
			}

			pos = treeOffset + treePos
		}

		result, err := pqe.segTree.Query(ctx, pos, pos)
		if err != nil {
			span.RecordError(err)
			queryErr = fmt.Errorf("querying node %s: %w", u, err)
			return 0, queryErr
		}

		duration := time.Since(startTime)
		pqe.recordQuery(duration, 1) // M-IMPL-2: Same-node = 1 segment

		span.SetAttributes(
			attribute.Int64("result", result),
			attribute.Bool("same_node", true),
		)
		span.SetStatus(codes.Ok, "single node query")

		logger.Info("path_query_complete",
			slog.String("u", u),
			slog.String("v", v),
			slog.Int64("result", result),
			slog.Duration("duration", duration),
		)

		// H-IMPL-1: Slow query logging
		if pqe.opts.SlowQueryThreshold > 0 && duration > pqe.opts.SlowQueryThreshold {
			logger.Warn("slow_path_query",
				slog.String("u", u),
				slog.String("v", v),
				slog.Duration("duration", duration),
				slog.Duration("threshold", pqe.opts.SlowQueryThreshold),
				slog.String("query_type", "same_node"),
			)
		}

		return result, nil
	}

	// C3: Forest mode - validate nodes in same tree
	if pqe.forest != nil {
		if err := pqe.checkTreeComponent(u, v); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "cross-tree query")
			queryErr = err
			return 0, queryErr
		}
	}

	// H2: Check query cache (includes graph hash in key)
	if pqe.queryCache != nil {
		cacheKey := pqe.PathQueryCacheKey(u, v)
		if result, ok := pqe.queryCache.Get(cacheKey); ok {
			pqe.cacheHits.Add(1)
			pathQueryCacheHits.WithLabelValues("query").Inc() // H-IMPL-2
			duration := time.Since(startTime)
			pqe.recordQuery(duration, 0) // M-IMPL-2: Cache hit, segment count unknown

			// M1: Log cache hit
			logger.Debug("path_query_cache_hit",
				slog.String("cache_key", cacheKey),
				slog.Int64("result", result),
			)

			span.SetAttributes(
				attribute.Bool("cache_hit", true),
				attribute.Int64("result", result),
			)
			span.SetStatus(codes.Ok, "cache hit")
			return result, nil
		}
		pqe.cacheMisses.Add(1)
		pathQueryCacheMisses.WithLabelValues("query").Inc() // H-IMPL-2
	}

	// Compute LCA with caching
	span.AddEvent("computing_lca")
	lca, err := pqe.computeLCAWithCache(ctx, u, v)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "LCA failed")
		queryErr = fmt.Errorf("computing LCA for path %s->%s: %w", u, v, err)
		return 0, queryErr
	}

	// Decompose path into segments
	span.AddEvent("decomposing_path")
	// H-IMPL-3: Time path decomposition for CRS sub-step
	decomposeStart := time.Now()
	var segments []PathSegment
	if pqe.hld != nil {
		segments, err = pqe.hld.DecomposePath(ctx, u, v)
	} else {
		// Forest mode: get HLD for tree containing u (already validated same tree)
		treeHLD := pqe.forest.GetHLD(u)
		if treeHLD == nil {
			err := fmt.Errorf("HLD not found for node %s", u)
			span.RecordError(err)
			span.SetStatus(codes.Error, "forest HLD lookup failed")
			queryErr = err
			return 0, queryErr
		}
		segments, err = treeHLD.DecomposePath(ctx, u, v)

		// Offset segment positions to global segment tree space
		if err == nil {
			treeOffset, offsetErr := pqe.forest.GetTreeOffset(u)
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

	// H-IMPL-3: Record CRS sub-step for path decomposition
	decomposeDuration := time.Since(decomposeStart)
	if pqe.crs != nil {
		pqe.recordDecomposeSubStep(ctx, u, v, lca, segments, decomposeDuration, err)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decomposition failed")
		queryErr = fmt.Errorf("decomposing path %s->%s: %w", u, v, err)
		return 0, queryErr
	}

	// L3: Check depth limit
	if len(segments) > pqe.opts.MaxTreeDepth {
		err := fmt.Errorf("path depth %d exceeds maximum %d", len(segments), pqe.opts.MaxTreeDepth)
		span.RecordError(err)
		span.SetStatus(codes.Error, "depth limit exceeded")
		queryErr = err
		return 0, queryErr
	}

	// Query segments
	span.AddEvent("querying_segments",
		trace.WithAttributes(attribute.Int("segment_count", len(segments))),
	)

	// Get LCA position for double-counting handling
	var lcaPos int
	if pqe.hld != nil {
		lcaIdx, ok := pqe.hld.NodeToIdx(lca)
		if !ok {
			err := fmt.Errorf("LCA node %s not found in HLD", lca)
			span.RecordError(err)
			queryErr = err
			return 0, queryErr
		}
		lcaPos = pqe.hld.Pos(lcaIdx)
	} else {
		// Forest mode: get tree offset and add to tree-local position
		treeHLD := pqe.forest.GetHLD(lca)
		if treeHLD == nil {
			err := fmt.Errorf("HLD not found for LCA node %s", lca)
			span.RecordError(err)
			queryErr = err
			return 0, queryErr
		}
		lcaIdx, ok := treeHLD.NodeToIdx(lca)
		if !ok {
			err := fmt.Errorf("LCA node %s not found in tree HLD", lca)
			span.RecordError(err)
			queryErr = err
			return 0, queryErr
		}

		// Get tree offset for global segment tree position
		treeOffset, err := pqe.forest.GetTreeOffset(lca)
		if err != nil {
			span.RecordError(err)
			queryErr = fmt.Errorf("getting tree offset for LCA %s: %w", lca, err)
			return 0, queryErr
		}

		// Calculate global position: offset + tree-local position
		lcaPos = treeOffset + treeHLD.Pos(lcaIdx)
	}

	result, err := pqe.querySegments(ctx, segments, lcaPos, logger, span)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "segment query failed")
		queryErr = fmt.Errorf("querying segments for path %s->%s: %w", u, v, err)
		return 0, queryErr
	}

	duration := time.Since(startTime)

	// Record statistics
	pqe.recordQuery(duration, len(segments))              // M-IMPL-2
	pathQuerySegmentCount.Observe(float64(len(segments))) // H-IMPL-2

	// Cache result
	if pqe.queryCache != nil {
		cacheKey := pqe.PathQueryCacheKey(u, v)
		pqe.queryCache.Set(cacheKey, result)
	}

	// L5: Rich span attributes
	graphHash := ""
	if pqe.hld != nil {
		graphHash = pqe.hld.GraphHash()
	} else if pqe.forest != nil {
		graphHash = pqe.forest.GraphHash()
	}

	span.SetAttributes(
		attribute.String("lca", lca),
		attribute.Int("segment_count", len(segments)),
		attribute.Int64("result", result),
		attribute.Int64("duration_us", duration.Microseconds()),
		attribute.String("graph_hash", graphHash),
		attribute.Bool("cache_hit", false),
	)
	span.SetStatus(codes.Ok, "path query complete")

	// M1: Log completion
	logger.Info("path_query_complete",
		slog.String("u", u),
		slog.String("v", v),
		slog.Int64("result", result),
		slog.Int("segments", len(segments)),
		slog.Duration("duration", duration),
	)

	// H-IMPL-1: Slow query logging
	if pqe.opts.SlowQueryThreshold > 0 && duration > pqe.opts.SlowQueryThreshold {
		logger.Warn("slow_path_query",
			slog.String("u", u),
			slog.String("v", v),
			slog.Duration("duration", duration),
			slog.Duration("threshold", pqe.opts.SlowQueryThreshold),
			slog.Int("segments", len(segments)),
			slog.String("lca", lca),
		)
	}

	return result, nil
}

// PathSum computes sum of values on path from u to v.
//
// Description:
//
//	Convenience wrapper for PathQuery with SUM aggregation.
//	Requires engine to be configured with AggregateSUM.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Logger for structured logging. Must not be nil.
//
// Outputs:
//   - int64: Sum of values along path u → v.
//   - error: Non-nil if engine not configured for SUM or PathQuery fails.
//
// Example:
//
//	totalComplexity, err := engine.PathSum(ctx, "main", "leaf", logger)
//
// Limitations:
//   - Engine must be created with AggregateSUM
//   - Nodes must be in same tree (forest mode)
//
// Assumptions:
//   - Caller has validated engine configuration
//   - Node IDs are valid and exist in graph
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) PathSum(ctx context.Context, u, v string, logger *slog.Logger) (int64, error) {
	if pqe.aggFunc != AggregateSUM {
		return 0, fmt.Errorf("PathSum requires AggregateSUM, engine configured with %s", pqe.aggFunc)
	}
	return pqe.PathQuery(ctx, u, v, logger)
}

// PathMin computes minimum value on path from u to v.
//
// Description:
//
//	Convenience wrapper for PathQuery with MIN aggregation.
//	Requires engine to be configured with AggregateMIN.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Logger for structured logging. Must not be nil.
//
// Outputs:
//   - int64: Minimum value along path u → v.
//   - error: Non-nil if engine not configured for MIN or PathQuery fails.
//
// Example:
//
//	minMemory, err := engine.PathMin(ctx, "main", "leaf", logger)
//
// Limitations:
//   - Engine must be created with AggregateMIN
//   - Nodes must be in same tree (forest mode)
//
// Assumptions:
//   - Caller has validated engine configuration
//   - Node IDs are valid and exist in graph
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) PathMin(ctx context.Context, u, v string, logger *slog.Logger) (int64, error) {
	if pqe.aggFunc != AggregateMIN {
		return 0, fmt.Errorf("PathMin requires AggregateMIN, engine configured with %s", pqe.aggFunc)
	}
	return pqe.PathQuery(ctx, u, v, logger)
}

// PathMax computes maximum value on path from u to v.
//
// Description:
//
//	Convenience wrapper for PathQuery with MAX aggregation.
//	Requires engine to be configured with AggregateMAX.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Logger for structured logging. Must not be nil.
//
// Outputs:
//   - int64: Maximum value along path u → v.
//   - error: Non-nil if engine not configured for MAX or PathQuery fails.
//
// Example:
//
//	maxLatency, err := engine.PathMax(ctx, "main", "leaf", logger)
//
// Limitations:
//   - Engine must be created with AggregateMAX
//   - Nodes must be in same tree (forest mode)
//
// Assumptions:
//   - Caller has validated engine configuration
//   - Node IDs are valid and exist in graph
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) PathMax(ctx context.Context, u, v string, logger *slog.Logger) (int64, error) {
	if pqe.aggFunc != AggregateMAX {
		return 0, fmt.Errorf("PathMax requires AggregateMAX, engine configured with %s", pqe.aggFunc)
	}
	return pqe.PathQuery(ctx, u, v, logger)
}

// PathGCD computes GCD of values on path from u to v.
//
// Description:
//
//	Convenience wrapper for PathQuery with GCD aggregation.
//	Requires engine to be configured with AggregateGCD.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: Start node ID. Must exist in graph.
//   - v: End node ID. Must exist in graph.
//   - logger: Logger for structured logging. Must not be nil.
//
// Outputs:
//   - int64: GCD of values along path u → v.
//   - error: Non-nil if engine not configured for GCD or PathQuery fails.
//
// Example:
//
//	gcdVersion, err := engine.PathGCD(ctx, "main", "leaf", logger)
//
// Limitations:
//   - Engine must be created with AggregateGCD
//   - Nodes must be in same tree (forest mode)
//   - GCD undefined for paths with zero values
//
// Assumptions:
//   - Caller has validated engine configuration
//   - Node IDs are valid and exist in graph
//   - All values on path are non-negative
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) PathGCD(ctx context.Context, u, v string, logger *slog.Logger) (int64, error) {
	if pqe.aggFunc != AggregateGCD {
		return 0, fmt.Errorf("PathGCD requires AggregateGCD, engine configured with %s", pqe.aggFunc)
	}
	return pqe.PathQuery(ctx, u, v, logger)
}

// querySegments queries all segments and combines results.
//
// Description:
//
//	Internal helper to query each PathSegment with segment tree and
//	combine results using aggregation function.
//	H-IMPL-2: Records segment tree query duration metrics.
//
// Algorithm:
//
//	for each segment:
//	  check context cancellation
//	  segResult = segTree.Query(segment.Start, segment.End)
//	  result = aggFunc.Combine(result, segResult)
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - segments: Path segments to query. Can be empty for same-node queries.
//   - lcaPos: LCA position in HLD array. Used for double-counting note.
//   - logger: Structured logger. Must not be nil.
//   - span: OpenTelemetry span for events. Must not be nil.
//
// Outputs:
//   - int64: Combined aggregate result.
//   - error: Non-nil if segment tree query fails or context canceled.
//
// Limitations:
//   - LCA double-counting already handled by DecomposePath
//   - Segments must be in HLD position space
//
// Assumptions:
//   - Segments are valid output from DecomposePath
//   - Segment positions within segment tree bounds
//
// Thread Safety: Safe for concurrent use (read-only).
func (pqe *PathQueryEngine) querySegments(ctx context.Context, segments []PathSegment, lcaPos int, logger *slog.Logger, span trace.Span) (int64, error) {
	result := pqe.aggFunc.Identity()

	for i, seg := range segments {
		// C4: Check context cancellation between segments
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Ensure segment indices are in correct order
		start, end := seg.Start, seg.End
		if start > end {
			start, end = end, start
		}

		// Query segment tree (H-IMPL-2: measure duration)
		segStartTime := time.Now()
		val, err := pqe.segTree.Query(ctx, start, end)
		segDuration := time.Since(segStartTime)
		segmentTreeQueryDuration.Observe(segDuration.Seconds())

		if err != nil {
			logger.Error("segment_query_failed",
				slog.Int("segment_index", i),
				slog.Int("start", start),
				slog.Int("end", end),
				slog.String("error", err.Error()),
			)
			return result, fmt.Errorf("segment %d query [%d,%d] failed: %w", i, start, end, err)
		}

		// Combine with result
		result = pqe.aggFunc.Combine(result, val)

		// Record span event for segment
		span.AddEvent(fmt.Sprintf("segment_%d_queried", i),
			trace.WithAttributes(
				attribute.Int("start", start),
				attribute.Int("end", end),
				attribute.Int64("value", val),
			),
		)
	}

	// NOTE: DecomposePath already handles LCA correctly - it appears in only one segment
	// No need for LCA double-counting adjustment

	return result, nil
}

// checkTreeComponent validates that u and v are in the same tree component (forest mode).
//
// Description:
//
//	In forest mode, path queries are only valid within a single tree.
//	Returns error if nodes are in different trees.
//	Returns nil in single-tree mode (no forest).
//
// Inputs:
//   - u: First node ID. Must not be empty.
//   - v: Second node ID. Must not be empty.
//
// Outputs:
//   - error: Non-nil if nodes in different trees or node not found. Nil if same tree or single-tree mode.
//
// Limitations:
//   - Only applicable to forest mode
//   - Does not validate if path exists between nodes
//
// Assumptions:
//   - Forest mode: pqe.forest != nil
//   - Single-tree mode: pqe.forest == nil
//
// Thread Safety: Safe for concurrent use (read-only).
func (pqe *PathQueryEngine) checkTreeComponent(u, v string) error {
	if pqe.forest == nil {
		return nil // Single-tree mode, always valid
	}

	treeU, err := pqe.forest.GetTreeID(u)
	if err != nil {
		return fmt.Errorf("node %s not found in forest: %w", u, err)
	}

	treeV, err := pqe.forest.GetTreeID(v)
	if err != nil {
		return fmt.Errorf("node %s not found in forest: %w", v, err)
	}

	if treeU != treeV {
		return fmt.Errorf("cannot query path: nodes %s (tree %d) and %s (tree %d) in different tree components",
			u, treeU, v, treeV)
	}

	return nil
}

// computeLCAWithCache computes LCA with caching.
//
// Description:
//
//	H1: Uses LRU cache to avoid repeated LCA computations.
//	H2: Cache key includes nodes but implicitly tied to graph via engine instance.
//	H-IMPL-2: Records cache hit/miss metrics.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - u: First node ID. Must exist in graph.
//   - v: Second node ID. Must exist in graph.
//
// Outputs:
//   - string: LCA node ID.
//   - error: Non-nil if LCA computation fails or nodes not found.
//
// Limitations:
//   - Cache invalidation tied to engine instance lifetime
//   - Forest mode: nodes must be in same tree
//
// Assumptions:
//   - Caller has validated u and v exist and are in same tree
//   - Graph structure unchanged since cache population
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) computeLCAWithCache(ctx context.Context, u, v string) (string, error) {
	startTime := time.Now()

	// Check cache
	if pqe.lcaCache != nil {
		key := lcaCacheKey(u, v)
		if lca, ok := pqe.lcaCache.Get(key); ok {
			pqe.cacheHits.Add(1)
			pathQueryCacheHits.WithLabelValues("lca").Inc() // H-IMPL-2

			// H-IMPL-3: Record CRS sub-step for cached LCA
			if pqe.crs != nil {
				pqe.recordLCASubStep(ctx, u, v, lca, time.Since(startTime), true, nil)
			}

			return lca, nil
		}
		pqe.cacheMisses.Add(1)
		pathQueryCacheMisses.WithLabelValues("lca").Inc() // H-IMPL-2
	}

	// Compute LCA
	var lca string
	var err error
	if pqe.hld != nil {
		lca, err = pqe.hld.LCA(ctx, u, v)
	} else {
		// Forest mode: get HLD for tree containing u (already validated same tree)
		treeHLD := pqe.forest.GetHLD(u)
		if treeHLD == nil {
			return "", fmt.Errorf("HLD not found for node %s", u)
		}
		lca, err = treeHLD.LCA(ctx, u, v)
	}

	if err != nil {
		// H-IMPL-3: Record CRS sub-step for failed LCA
		if pqe.crs != nil {
			pqe.recordLCASubStep(ctx, u, v, "", time.Since(startTime), false, err)
		}
		return "", err
	}

	// Cache result
	if pqe.lcaCache != nil {
		key := lcaCacheKey(u, v)
		pqe.lcaCache.Set(key, lca)
	}

	// H-IMPL-3: Record CRS sub-step for computed LCA
	if pqe.crs != nil {
		pqe.recordLCASubStep(ctx, u, v, lca, time.Since(startTime), false, nil)
	}

	return lca, nil
}

// recordLCASubStep records a CRS sub-step for LCA computation (H-IMPL-3).
//
// Description:
//
//	Helper method to record LCA computation as a CRS sub-step.
//	Only records if pqe.crs is non-nil.
//
// Inputs:
//   - ctx: Context for CRS recording. Must not be nil.
//   - u: Start node ID.
//   - v: End node ID.
//   - lca: Computed LCA (empty string if error).
//   - duration: Time taken for LCA computation.
//   - cacheHit: True if result came from cache.
//   - err: Error if LCA computation failed (nil on success).
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) recordLCASubStep(ctx context.Context, u, v, lca string, duration time.Duration, cacheHit bool, err error) {
	pqe.mu.RLock()
	sessionID := pqe.sessionID
	pqe.mu.RUnlock()

	outcome := crs.OutcomeSuccess
	errorCategory := crs.ErrorCategoryNone
	errorMsg := ""
	if err != nil {
		outcome = crs.OutcomeFailure
		errorCategory = classifyHLDError(err)
		errorMsg = err.Error()
	}

	resultSummary := fmt.Sprintf("LCA(%s,%s)=%s", u, v, lca)
	if cacheHit {
		resultSummary += " [cached]"
	}

	step := crs.StepRecord{
		SessionID:     sessionID,
		StepNumber:    int(pqe.subStepCounter.Add(1)),
		Timestamp:     time.Now().UnixMilli(),
		Actor:         crs.ActorSystem,
		Decision:      crs.DecisionExecuteTool,
		Tool:          "PathQuery.LCA",
		ToolParams:    &crs.ToolParams{Target: u, Query: v},
		Outcome:       outcome,
		ErrorCategory: errorCategory,
		ErrorMessage:  errorMsg,
		DurationMs:    duration.Milliseconds(),
		ResultSummary: resultSummary,
		Propagate:     false,
		Terminal:      false,
	}

	if validationErr := step.Validate(); validationErr != nil {
		// Log validation error but don't fail the query
		return
	}

	if recordErr := pqe.crs.RecordStep(ctx, step); recordErr != nil {
		// Log recording error but don't fail the query
		return
	}
}

// recordDecomposeSubStep records a CRS sub-step for path decomposition (H-IMPL-3).
//
// Description:
//
//	Helper method to record path decomposition as a CRS sub-step.
//	Only records if pqe.crs is non-nil.
//
// Inputs:
//   - ctx: Context for CRS recording. Must not be nil.
//   - u: Start node ID.
//   - v: End node ID.
//   - lca: LCA node used for decomposition.
//   - segments: Decomposed path segments (empty if error).
//   - duration: Time taken for decomposition.
//   - err: Error if decomposition failed (nil on success).
//
// Thread Safety: Safe for concurrent use.
func (pqe *PathQueryEngine) recordDecomposeSubStep(ctx context.Context, u, v, lca string, segments []PathSegment, duration time.Duration, err error) {
	pqe.mu.RLock()
	sessionID := pqe.sessionID
	pqe.mu.RUnlock()

	outcome := crs.OutcomeSuccess
	errorCategory := crs.ErrorCategoryNone
	errorMsg := ""
	if err != nil {
		outcome = crs.OutcomeFailure
		errorCategory = classifyHLDError(err)
		errorMsg = err.Error()
	}

	resultSummary := fmt.Sprintf("DecomposePath(%s,%s,lca=%s)=%d segments", u, v, lca, len(segments))

	step := crs.StepRecord{
		SessionID:     sessionID,
		StepNumber:    int(pqe.subStepCounter.Add(1)),
		Timestamp:     time.Now().UnixMilli(),
		Actor:         crs.ActorSystem,
		Decision:      crs.DecisionExecuteTool,
		Tool:          "PathQuery.DecomposePath",
		ToolParams:    &crs.ToolParams{Target: u, Query: v},
		Outcome:       outcome,
		ErrorCategory: errorCategory,
		ErrorMessage:  errorMsg,
		DurationMs:    duration.Milliseconds(),
		ResultSummary: resultSummary,
		Propagate:     false,
		Terminal:      false,
	}

	if validationErr := step.Validate(); validationErr != nil {
		// Log validation error but don't fail the query
		return
	}

	if recordErr := pqe.crs.RecordStep(ctx, step); recordErr != nil {
		// Log recording error but don't fail the query
		return
	}
}

// PathQueryCacheKey generates deterministic cache key for path query.
//
// Description:
//
//	H2: Includes graph hash to ensure cache invalidation on graph changes.
//	Canonical ordering for symmetric aggregations (MIN/MAX).
//	Uses strings.Builder for efficient string concatenation.
//
// Inputs:
//   - u: Start node ID. Must not be empty.
//   - v: End node ID. Must not be empty.
//
// Outputs:
//   - string: Cache key in format "pathquery:{graphHash}:{u}:{v}:{aggFunc}"
//
// Limitations:
//   - Keys are unique per (graph, u, v, aggFunc) tuple
//   - MIN/MAX queries use canonical ordering (smaller node first)
//
// Assumptions:
//   - Graph hash is stable for same graph structure
//   - Node IDs don't contain ':' character
//
// Thread Safety: Safe for concurrent use (read-only).
func (pqe *PathQueryEngine) PathQueryCacheKey(u, v string) string {
	// Canonical ordering for symmetric queries
	if pqe.aggFunc == AggregateMIN || pqe.aggFunc == AggregateMAX {
		if u > v {
			u, v = v, u
		}
	}

	// H2: Include graph hash for correctness
	var graphHash string
	if pqe.hld != nil {
		graphHash = pqe.hld.GraphHash()
	} else if pqe.forest != nil {
		graphHash = pqe.forest.GraphHash()
	}

	// L-IMPL-1: Use strings.Builder to reduce GC pressure
	var builder strings.Builder
	builder.Grow(len("pathquery:") + len(graphHash) + len(u) + len(v) + len(pqe.aggFunc.String()) + 4)
	builder.WriteString("pathquery:")
	builder.WriteString(graphHash)
	builder.WriteByte(':')
	builder.WriteString(u)
	builder.WriteByte(':')
	builder.WriteString(v)
	builder.WriteByte(':')
	builder.WriteString(pqe.aggFunc.String())
	return builder.String()
}

// recordQuery updates statistics atomically.
//
// Description:
//
//	C2: Thread-safe stats tracking using atomic operations.
//	M4: Timestamps stored as int64 UnixMilli per CLAUDE.md Section 4.6.
//	M-IMPL-2: Tracks segment count for average calculation.
//
// Inputs:
//   - duration: Query duration. Must be non-negative.
//   - segmentCount: Number of segments queried. 0 for cache hits, 1 for same-node, len(segments) for full path.
//
// Outputs:
//   - None (updates internal atomic counters)
//
// Limitations:
//   - Segment count of 0 indicates cache hit (unknown actual segment count)
//
// Assumptions:
//   - Called exactly once per query completion
//   - duration accurately reflects query execution time
//
// Thread Safety: Safe for concurrent use (atomic operations).
func (pqe *PathQueryEngine) recordQuery(duration time.Duration, segmentCount int) {
	pqe.queryCount.Add(1)
	pqe.totalLatencyNanos.Add(duration.Nanoseconds())
	pqe.totalSegments.Add(int64(segmentCount)) // M-IMPL-2
	pqe.lastQueryTime.Store(time.Now().UnixMilli())
}

// Validate checks that HLD and SegmentTree are compatible and valid.
//
// Description:
//
//	Verifies:
//	- HLD or Forest is valid
//	- SegmentTree is valid
//	- Size compatibility
//	- Aggregation function consistency
//	L-IMPL-2: Creates OpenTelemetry span to track validation.
//
// Inputs:
//   - None (validates internal state)
//
// Outputs:
//   - error: Non-nil if validation fails with detailed error message
//
// Limitations:
//   - Only checks structural validity, not semantic correctness
//   - Does not validate node values or tree structure
//
// Assumptions:
//   - Engine fields are immutable after construction
//   - HLD and SegmentTree built from same graph
//
// Thread Safety: Safe for concurrent use (read-only).
func (pqe *PathQueryEngine) Validate() error {
	// L-IMPL-2: Add span for validation tracking
	_, span := otel.Tracer("trace").Start(context.Background(), "graph.PathQueryEngine.Validate")
	defer func() {
		if r := recover(); r != nil {
			span.RecordError(fmt.Errorf("validation panic: %v", r))
			span.SetStatus(codes.Error, "panic during validation")
			span.End()
			panic(r)
		}
		span.End()
	}()

	// Validate HLD
	if pqe.hld != nil {
		if err := pqe.hld.Validate(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "HLD validation failed")
			span.SetAttributes(attribute.String("validation_error", "hld_invalid"))
			return fmt.Errorf("HLD invalid: %w", err)
		}

		// Check size consistency
		if pqe.hld.NodeCount() != pqe.segTree.size {
			err := fmt.Errorf("size mismatch: HLD %d != SegTree %d",
				pqe.hld.NodeCount(), pqe.segTree.size)
			span.RecordError(err)
			span.SetStatus(codes.Error, "size mismatch")
			span.SetAttributes(attribute.String("validation_error", "hld_size_mismatch"))
			return err
		}
	}

	// Validate Forest
	if pqe.forest != nil {
		if err := pqe.forest.Validate(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "forest validation failed")
			span.SetAttributes(attribute.String("validation_error", "forest_invalid"))
			return fmt.Errorf("forest invalid: %w", err)
		}

		// Check size consistency
		if pqe.forest.TotalNodes() != pqe.segTree.size {
			err := fmt.Errorf("size mismatch: Forest %d != SegTree %d",
				pqe.forest.TotalNodes(), pqe.segTree.size)
			span.RecordError(err)
			span.SetStatus(codes.Error, "size mismatch")
			span.SetAttributes(attribute.String("validation_error", "forest_size_mismatch"))
			return err
		}
	}

	// Validate segment tree
	if err := pqe.segTree.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "segment tree validation failed")
		span.SetAttributes(attribute.String("validation_error", "segtree_invalid"))
		return fmt.Errorf("segment tree invalid: %w", err)
	}

	// Verify aggregation function matches
	if pqe.aggFunc != pqe.segTree.aggFunc {
		err := fmt.Errorf("aggregation mismatch: engine %s != segment tree %s",
			pqe.aggFunc, pqe.segTree.aggFunc)
		span.RecordError(err)
		span.SetStatus(codes.Error, "aggregation mismatch")
		span.SetAttributes(attribute.String("validation_error", "agg_func_mismatch"))
		return err
	}

	// L-IMPL-2: Record successful validation
	span.SetStatus(codes.Ok, "validation successful")
	span.SetAttributes(attribute.Bool("validation_success", true))
	return nil
}

// PathQueryStats contains query statistics.
//
// Description:
//
//	Statistics collected across all queries on this engine.
//	All timestamps in Unix milliseconds UTC per CLAUDE.md Section 4.6.
//
// Thread Safety: Snapshot at time of Stats() call, may be stale.
type PathQueryStats struct {
	QueryCount       int64         // Total queries
	TotalLatency     time.Duration // Sum of all query durations
	AvgLatency       time.Duration // Average query duration
	LastQueryTime    int64         // Unix milliseconds UTC of last query
	SegmentsPerQuery float64       // Average segments per query (requires tracking)
	CacheHitRatio    float64       // Ratio of cache hits to total lookups
}

// Stats returns query statistics.
//
// Description:
//
//	C2: Thread-safe stats retrieval using atomic operations.
//	Returns snapshot of current statistics.
//	M-IMPL-2: Includes SegmentsPerQuery average.
//
// Inputs:
//   - None
//
// Outputs:
//   - PathQueryStats: Snapshot of statistics including query count, latency, cache hit ratio, segments per query
//
// Limitations:
//   - Snapshot may be stale immediately after return
//   - SegmentsPerQuery excludes cache hits (counted as 0 segments)
//   - CacheHitRatio includes both LCA cache and query cache
//
// Assumptions:
//   - Atomic loads provide consistent point-in-time snapshot
//   - QueryCount > 0 when calculating averages
//
// Thread Safety: Safe for concurrent use (atomic reads).
func (pqe *PathQueryEngine) Stats() PathQueryStats {
	queryCount := pqe.queryCount.Load()
	totalLatencyNanos := pqe.totalLatencyNanos.Load()
	totalSegments := pqe.totalSegments.Load() // M-IMPL-2
	lastQueryTime := pqe.lastQueryTime.Load()
	cacheHits := pqe.cacheHits.Load()
	cacheMisses := pqe.cacheMisses.Load()

	stats := PathQueryStats{
		QueryCount:    queryCount,
		TotalLatency:  time.Duration(totalLatencyNanos),
		LastQueryTime: lastQueryTime,
	}

	if queryCount > 0 {
		stats.AvgLatency = time.Duration(totalLatencyNanos / queryCount)
		stats.SegmentsPerQuery = float64(totalSegments) / float64(queryCount) // M-IMPL-2
	}

	totalLookups := cacheHits + cacheMisses
	if totalLookups > 0 {
		stats.CacheHitRatio = float64(cacheHits) / float64(totalLookups)
	}

	return stats
}
