// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// -----------------------------------------------------------------------------
// Metrics (GR-32)
// -----------------------------------------------------------------------------

var (
	depIndexQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crs_dependency_index_query_duration_seconds",
		Help:    "Dependency index query duration in seconds",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
	}, []string{"operation"})

	depIndexCacheInvalidations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crs_dependency_index_cache_invalidations_total",
		Help: "Total number of cache invalidations",
	})

	depIndexCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crs_dependency_index_cache_hits_total",
		Help: "Total number of cache hits",
	})

	depIndexCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crs_dependency_index_cache_misses_total",
		Help: "Total number of cache misses",
	})

	depIndexSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crs_dependency_index_edge_count",
		Help: "Current number of dependency (call) edges in the index",
	})

	// GR-32 Code Review Fix: Error counter for operational visibility
	depIndexQueryErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crs_dependency_index_query_errors_total",
		Help: "Total number of query errors by operation",
	}, []string{"operation"})
)

// -----------------------------------------------------------------------------
// Tracer
// -----------------------------------------------------------------------------

var graphDepIndexTracer = otel.Tracer("crs.graph_dependency_index")

// -----------------------------------------------------------------------------
// GraphBackedDependencyIndex (GR-32)
// -----------------------------------------------------------------------------

// GraphBackedDependencyIndex provides dependency queries backed by the actual graph.
//
// Description:
//
//	This replaces the standalone dependencyGraph that stored duplicate edge data.
//	Instead of maintaining its own forward/reverse maps, it delegates all queries
//	to the GraphQuery interface (typically CRSGraphAdapter from GR-28), which
//	wraps the actual HierarchicalGraph.
//
//	The index implements DependencyIndexView interface for use in CRS snapshots.
//
// Architecture:
//
//	┌──────────────────────────────┐
//	│ GraphBackedDependencyIndex   │
//	│  └── adapter GraphQuery ─────┼──► CRSGraphAdapter ──► HierarchicalGraph
//	└──────────────────────────────┘
//
// Limitations:
//   - Does NOT provide point-in-time snapshot semantics. Queries see live graph state.
//   - Interface methods cannot accept context.Context (existing interface constraint).
//   - Uses internal timeout (5 seconds) to prevent unbounded queries.
//
// Thread Safety: Safe for concurrent read access.
type GraphBackedDependencyIndex struct {
	adapter GraphQuery
	logger  *slog.Logger

	// Cache for Size() - invalidation-based, not TTL
	mu             sync.RWMutex
	sizeCache      int
	sizeCacheValid bool
	cacheGen       uint64 // GR-32 Code Review Fix: Generation counter for race detection

	// singleflight prevents thundering herd on Size() cache miss
	sizeGroup singleflight.Group

	// Internal timeout for queries (interface doesn't accept context)
	queryTimeout time.Duration
}

// defaultQueryTimeout is the timeout for queries when interface doesn't allow context.
const defaultQueryTimeout = 5 * time.Second

// NewGraphBackedDependencyIndex creates a dependency index backed by the graph.
//
// Description:
//
//	Creates an index that delegates dependency queries to the GraphQuery
//	interface (typically CRSGraphAdapter from GR-28). This eliminates data
//	duplication between CRS and the code graph.
//
// Inputs:
//   - adapter: The GraphQuery implementation. Must not be nil.
//
// Outputs:
//   - *GraphBackedDependencyIndex: The new index.
//   - error: Non-nil if adapter is nil.
//
// Example:
//
//	adapter, _ := graph.NewCRSGraphAdapter(g, idx, gen, refresh, nil)
//	depIndex, err := crs.NewGraphBackedDependencyIndex(adapter)
//	if err != nil {
//	    return fmt.Errorf("create dependency index: %w", err)
//	}
//
// Thread Safety: The returned index is safe for concurrent read access.
// The caller must ensure the adapter outlives this index.
func NewGraphBackedDependencyIndex(adapter GraphQuery) (*GraphBackedDependencyIndex, error) {
	if adapter == nil {
		return nil, errors.New("adapter must not be nil")
	}

	return &GraphBackedDependencyIndex{
		adapter:      adapter,
		logger:       slog.Default().With(slog.String("component", "graph_backed_dep_index")),
		queryTimeout: defaultQueryTimeout,
	}, nil
}

// -----------------------------------------------------------------------------
// DependencyIndexView Interface Implementation
// -----------------------------------------------------------------------------

// DependsOn returns all nodes that nodeID depends on (callees).
//
// Description:
//
//	Returns the IDs of all symbols that the given node calls. Delegates to
//	GraphQuery.FindCallees() and extracts symbol IDs.
//
// Inputs:
//   - nodeID: The symbol ID to find dependencies for.
//
// Outputs:
//   - []string: Symbol IDs of callees. Empty slice if none or on error.
//
// Limitations:
//   - Cannot accept context.Context due to interface constraint.
//   - Uses internal 5-second timeout.
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) DependsOn(nodeID string) []string {
	// GR-32 Code Review Fix: Validate input
	if nodeID == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		depIndexQueryDuration.WithLabelValues("depends_on").Observe(time.Since(start).Seconds())
	}()

	// Create context with timeout since interface doesn't accept one
	ctx, cancel := context.WithTimeout(context.Background(), d.queryTimeout)
	defer cancel()

	ctx, span := graphDepIndexTracer.Start(ctx, "crs.GraphBackedDependencyIndex.DependsOn",
		trace.WithAttributes(
			attribute.String("node_id", nodeID),
		),
	)
	defer span.End()

	symbols, err := d.adapter.FindCallees(ctx, nodeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "find callees failed")
		// GR-32 Code Review Fix: Log at Warn level for operational visibility
		depIndexQueryErrors.WithLabelValues("depends_on").Inc()
		d.logger.Warn("DependsOn failed",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()),
		)
		return nil
	}

	result := make([]string, len(symbols))
	for i, sym := range symbols {
		result[i] = sym.ID
	}

	span.SetAttributes(attribute.Int("callee_count", len(result)))
	return result
}

// DependedBy returns all nodes that depend on nodeID (callers).
//
// Description:
//
//	Returns the IDs of all symbols that call the given node. Delegates to
//	GraphQuery.FindCallers() and extracts symbol IDs.
//
// Inputs:
//   - nodeID: The symbol ID to find dependents for.
//
// Outputs:
//   - []string: Symbol IDs of callers. Empty slice if none or on error.
//
// Limitations:
//   - Cannot accept context.Context due to interface constraint.
//   - Uses internal 5-second timeout.
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) DependedBy(nodeID string) []string {
	// GR-32 Code Review Fix: Validate input
	if nodeID == "" {
		return nil
	}

	start := time.Now()
	defer func() {
		depIndexQueryDuration.WithLabelValues("depended_by").Observe(time.Since(start).Seconds())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), d.queryTimeout)
	defer cancel()

	ctx, span := graphDepIndexTracer.Start(ctx, "crs.GraphBackedDependencyIndex.DependedBy",
		trace.WithAttributes(
			attribute.String("node_id", nodeID),
		),
	)
	defer span.End()

	symbols, err := d.adapter.FindCallers(ctx, nodeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "find callers failed")
		// GR-32 Code Review Fix: Log at Warn level for operational visibility
		depIndexQueryErrors.WithLabelValues("depended_by").Inc()
		d.logger.Warn("DependedBy failed",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()),
		)
		return nil
	}

	result := make([]string, len(symbols))
	for i, sym := range symbols {
		result[i] = sym.ID
	}

	span.SetAttributes(attribute.Int("caller_count", len(result)))
	return result
}

// HasCycle returns true if there's a cycle involving nodeID.
//
// Description:
//
//	Checks if following call edges from nodeID eventually leads back to itself.
//	Delegates to CRSGraphAdapter.HasCycleFrom() if supported.
//
// Inputs:
//   - nodeID: The symbol ID to check for cycles from.
//
// Outputs:
//   - bool: True if cycle exists, false otherwise or on error.
//
// Limitations:
//   - Cannot accept context.Context due to interface constraint.
//   - Uses internal 5-second timeout.
//   - Returns false on error (safe default).
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) HasCycle(nodeID string) bool {
	// GR-32 Code Review Fix: Validate input
	if nodeID == "" {
		return false
	}

	start := time.Now()
	defer func() {
		depIndexQueryDuration.WithLabelValues("has_cycle").Observe(time.Since(start).Seconds())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), d.queryTimeout)
	defer cancel()

	ctx, span := graphDepIndexTracer.Start(ctx, "crs.GraphBackedDependencyIndex.HasCycle",
		trace.WithAttributes(
			attribute.String("node_id", nodeID),
		),
	)
	defer span.End()

	// Type assert to access HasCycleFrom - it's on CRSGraphAdapter, not GraphQuery interface
	// If adapter doesn't support HasCycleFrom, return false (safe default)
	type cycleChecker interface {
		HasCycleFrom(ctx context.Context, symbolID string) (bool, error)
	}

	checker, ok := d.adapter.(cycleChecker)
	if !ok {
		span.SetAttributes(attribute.Bool("cycle_check_supported", false))
		d.logger.Debug("HasCycle not supported by adapter",
			slog.String("node_id", nodeID),
		)
		return false
	}

	hasCycle, err := checker.HasCycleFrom(ctx, nodeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "cycle check failed")
		// GR-32 Code Review Fix: Log at Warn level for operational visibility
		depIndexQueryErrors.WithLabelValues("has_cycle").Inc()
		d.logger.Warn("HasCycle failed",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()),
		)
		return false
	}

	span.SetAttributes(attribute.Bool("has_cycle", hasCycle))
	return hasCycle
}

// Size returns the number of dependency edges (call edges).
//
// Description:
//
//	Returns the total number of CALLS edges in the graph. Uses invalidation-based
//	caching with singleflight to prevent thundering herd.
//
// Outputs:
//   - int: Number of call edges. Returns 0 on error.
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) Size() int {
	start := time.Now()
	defer func() {
		depIndexQueryDuration.WithLabelValues("size").Observe(time.Since(start).Seconds())
	}()

	// Check cache first
	d.mu.RLock()
	if d.sizeCacheValid {
		size := d.sizeCache
		d.mu.RUnlock()
		depIndexCacheHits.Inc()
		return size
	}
	// GR-32 Code Review Fix: Capture generation before singleflight to detect
	// concurrent invalidation
	genBefore := d.cacheGen
	d.mu.RUnlock()
	depIndexCacheMisses.Inc()

	// Use singleflight to prevent thundering herd
	result, _, _ := d.sizeGroup.Do("size", func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), d.queryTimeout)
		defer cancel()

		ctx, span := graphDepIndexTracer.Start(ctx, "crs.GraphBackedDependencyIndex.Size.Compute")
		defer span.End()

		// Type assert to access CallEdgeCount
		type edgeCounter interface {
			CallEdgeCount(ctx context.Context) (int, error)
		}

		counter, ok := d.adapter.(edgeCounter)
		if !ok {
			// Fallback: use EdgeCount from the interface (counts all edge types)
			span.SetAttributes(attribute.Bool("fallback_count", true))
			d.logger.Debug("CallEdgeCount not supported, using EdgeCount fallback")
			count := d.adapter.EdgeCount()
			return count, nil
		}

		count, err := counter.CallEdgeCount(ctx)
		if err != nil {
			span.RecordError(err)
			// GR-32 Code Review Fix: Log at Warn level for operational visibility
			depIndexQueryErrors.WithLabelValues("size").Inc()
			d.logger.Warn("CallEdgeCount failed",
				slog.String("error", err.Error()),
			)
			return 0, err
		}

		// GR-32 Code Review Fix: Only update cache if generation hasn't changed
		// (no concurrent invalidation occurred during singleflight)
		d.mu.Lock()
		if d.cacheGen == genBefore {
			d.sizeCache = count
			d.sizeCacheValid = true
			// Update metric
			depIndexSize.Set(float64(count))
		}
		d.mu.Unlock()

		span.SetAttributes(attribute.Int("edge_count", count))
		return count, nil
	})

	if size, ok := result.(int); ok {
		return size
	}
	return 0
}

// -----------------------------------------------------------------------------
// Cache Management
// -----------------------------------------------------------------------------

// InvalidateCache marks the cache as stale.
//
// Description:
//
//	Called when the underlying graph is refreshed. This forces Size() to
//	recompute on next call. Also invalidates the adapter's cache if supported.
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) InvalidateCache() {
	d.mu.Lock()
	d.sizeCacheValid = false
	d.cacheGen++ // GR-32 Code Review Fix: Increment generation to detect race with singleflight
	d.mu.Unlock()

	depIndexCacheInvalidations.Inc()

	d.logger.Debug("dependency index cache invalidated")

	// Also invalidate adapter cache if supported
	type cacheInvalidator interface {
		InvalidateCache()
	}
	if invalidator, ok := d.adapter.(cacheInvalidator); ok {
		invalidator.InvalidateCache()
	}
}

// Generation returns the adapter's generation for staleness detection.
//
// Thread Safety: Safe for concurrent use.
func (d *GraphBackedDependencyIndex) Generation() int64 {
	return d.adapter.Generation()
}

// -----------------------------------------------------------------------------
// Compile-time Interface Check
// -----------------------------------------------------------------------------

var _ DependencyIndexView = (*GraphBackedDependencyIndex)(nil)
