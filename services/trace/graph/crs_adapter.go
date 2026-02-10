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
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// -----------------------------------------------------------------------------
// Tracer
// -----------------------------------------------------------------------------

var crsAdapterTracer = otel.Tracer("graph.crs_adapter")

// -----------------------------------------------------------------------------
// CRSGraphAdapter (GR-28)
// -----------------------------------------------------------------------------

// CRSGraphAdapter provides read-only access to the code graph from CRS activities.
//
// Description:
//
//	CRSGraphAdapter wraps the HierarchicalGraph, GraphAnalytics, and SymbolIndex
//	to provide a clean interface for CRS activities to query the code graph.
//	It handles:
//	  - Lifecycle management (closed state detection)
//	  - Generation tracking for staleness detection
//	  - Analytics caching with TTL and singleflight deduplication
//	  - O(1) symbol lookups via SymbolIndex
//	  - OTel tracing for all queries
//
// Thread Safety: All methods are safe for concurrent use.
type CRSGraphAdapter struct {
	// graph is the underlying hierarchical graph.
	graph *HierarchicalGraph

	// analytics provides analytics queries over the graph.
	analytics *GraphAnalytics

	// symbolIndex provides O(1) symbol lookups by ID, name, kind, and file.
	// May be nil if not provided.
	symbolIndex *index.SymbolIndex

	// generation is the graph generation when this adapter was created.
	generation int64

	// lastRefreshTime is when the graph was last refreshed (Unix milliseconds UTC).
	lastRefreshTime int64

	// closed indicates whether the adapter has been closed.
	closed atomic.Bool

	// config contains adapter configuration.
	config *crs.GraphQueryConfig

	// analyticsCache caches expensive analytics results.
	analyticsCache *crsAnalyticsCache

	// singleflight groups prevent thundering herd on cache misses.
	pageRankGroup    singleflight.Group
	communitiesGroup singleflight.Group

	// Query singleflight groups (GR-10)
	callersGroup singleflight.Group
	calleesGroup singleflight.Group
	pathsGroup   singleflight.Group
}

// Query cache configuration.
const (
	// DefaultQueryCacheSize is the default LRU cache size for query results.
	DefaultQueryCacheSize = 1000
)

// crsAnalyticsCache stores cached analytics results.
type crsAnalyticsCache struct {
	mu sync.RWMutex

	// PageRank cache
	pageRank          map[string]float64
	pageRankTimestamp int64

	// Communities cache
	communities          []crs.GraphCommunity
	communitiesTimestamp int64

	// Call edge count cache (GR-32)
	callEdgeCount      int
	callEdgeCountValid bool

	// Query result caches (GR-10)
	callersCache *LRUCache[string, *QueryResult]
	calleesCache *LRUCache[string, *QueryResult]
	pathsCache   *LRUCache[string, *PathResult]
}

// NewCRSGraphAdapter creates a new adapter for querying the graph from CRS.
//
// Description:
//
//	Creates an adapter that wraps the graph, analytics, and optional symbol index
//	for CRS use. The adapter tracks the graph generation for staleness detection.
//
// Inputs:
//   - g: The hierarchical graph to wrap. Must not be nil.
//   - idx: Optional symbol index for O(1) lookups. May be nil.
//   - generation: The graph generation number.
//   - refreshTime: When the graph was last refreshed (Unix milliseconds UTC).
//   - config: Optional configuration. Uses defaults if nil.
//
// Outputs:
//   - *CRSGraphAdapter: The adapter. Never nil if g is not nil.
//   - error: Non-nil if g is nil.
//
// Example:
//
//	adapter, err := NewCRSGraphAdapter(graph, symbolIndex, gen, refreshTime, nil)
//	if err != nil {
//	    return fmt.Errorf("creating graph adapter: %w", err)
//	}
//	defer adapter.Close()
//
// Limitations:
//   - Adapter does not own the graph or index lifecycle
//   - Graph mutations after adapter creation may cause stale results
//
// Thread Safety: The returned adapter is safe for concurrent use.
func NewCRSGraphAdapter(g *HierarchicalGraph, idx *index.SymbolIndex, generation int64, refreshTime int64, config *crs.GraphQueryConfig) (*CRSGraphAdapter, error) {
	if g == nil {
		return nil, crs.ErrGraphNotAvailable
	}

	if config == nil {
		config = crs.DefaultGraphQueryConfig()
	}

	return &CRSGraphAdapter{
		graph:           g,
		analytics:       NewGraphAnalytics(g),
		symbolIndex:     idx,
		generation:      generation,
		lastRefreshTime: refreshTime,
		config:          config,
		analyticsCache: &crsAnalyticsCache{
			pageRank:     make(map[string]float64),
			communities:  nil,
			callersCache: NewLRUCache[string, *QueryResult](DefaultQueryCacheSize),
			calleesCache: NewLRUCache[string, *QueryResult](DefaultQueryCacheSize),
			pathsCache:   NewLRUCache[string, *PathResult](DefaultQueryCacheSize),
		},
	}, nil
}

// InvalidateCache clears all cached analytics and query results.
//
// Description:
//
//	Call this when the underlying graph has been refreshed to ensure
//	subsequent queries return fresh data. This is typically called by
//	the graph refresh event handler.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) InvalidateCache() {
	cache := a.analyticsCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.pageRank = make(map[string]float64)
	cache.pageRankTimestamp = 0
	cache.communities = nil
	cache.communitiesTimestamp = 0
	cache.callEdgeCount = 0          // GR-32
	cache.callEdgeCountValid = false // GR-32

	// GR-10: Clear query caches
	if cache.callersCache != nil {
		cache.callersCache.Purge()
	}
	if cache.calleesCache != nil {
		cache.calleesCache.Purge()
	}
	if cache.pathsCache != nil {
		cache.pathsCache.Purge()
	}
}

// -----------------------------------------------------------------------------
// Node Queries
// -----------------------------------------------------------------------------

// FindSymbolByID returns a symbol by its unique ID.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - id: The unique symbol ID.
//
// Outputs:
//   - *ast.Symbol: The symbol, or nil if not found.
//   - bool: True if symbol was found.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindSymbolByID(ctx context.Context, id string) (*ast.Symbol, bool, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindSymbolByID",
		trace.WithAttributes(
			attribute.String("symbol_id", id),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, false, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, false, err
	}

	node, ok := a.graph.GetNode(id)
	if !ok || node.Symbol == nil {
		span.SetAttributes(attribute.Bool("found", false))
		return nil, false, nil
	}

	span.SetAttributes(attribute.Bool("found", true))
	return node.Symbol, true, nil
}

// FindSymbolsByName returns all symbols with the given name.
//
// Description:
//
//	GR-06: Uses nodesByName secondary index for O(1) lookup instead of
//	O(V) scan. Multiple symbols can share a name (e.g., "Setup" in
//	different packages).
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - name: The symbol name to search for.
//
// Outputs:
//   - []*ast.Symbol: Matching symbols. Empty slice if none found.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindSymbolsByName(ctx context.Context, name string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindSymbolsByName",
		trace.WithAttributes(
			attribute.String("name", name),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, err
	}

	// GR-06: Use nodesByName secondary index for O(1) lookup instead of O(V) scan.
	// GetNodesByName returns a defensive copy, so we can safely iterate.
	nodes := a.graph.GetNodesByName(name)
	results := make([]*ast.Symbol, 0, len(nodes))
	for _, node := range nodes {
		if node.Symbol != nil {
			results = append(results, node.Symbol)
		}
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// FindSymbolsByKind returns all symbols of the given kind.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - kind: The symbol kind to filter by.
//
// Outputs:
//   - []*ast.Symbol: Matching symbols. Empty slice if none found.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindSymbolsByKind(ctx context.Context, kind ast.SymbolKind) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindSymbolsByKind",
		trace.WithAttributes(
			attribute.String("kind", kind.String()),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, err
	}

	nodes := a.graph.GetNodesByKind(kind)
	results := make([]*ast.Symbol, 0, len(nodes))
	for _, node := range nodes {
		if node.Symbol != nil {
			results = append(results, node.Symbol)
		}
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// FindSymbolsInFile returns all symbols in the given file.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - filePath: The file path to search in.
//
// Outputs:
//   - []*ast.Symbol: Symbols in the file. Empty slice if none found.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindSymbolsInFile(ctx context.Context, filePath string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindSymbolsInFile",
		trace.WithAttributes(
			attribute.String("file_path", filePath),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, err
	}

	nodes := a.graph.GetNodesInFile(filePath)
	results := make([]*ast.Symbol, 0, len(nodes))
	for _, node := range nodes {
		if node.Symbol != nil {
			results = append(results, node.Symbol)
		}
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// -----------------------------------------------------------------------------
// Edge Queries
// -----------------------------------------------------------------------------

// FindCallers returns symbols that call the given symbol.
//
// Description:
//
//	Finds all symbols that have outgoing call edges to the target symbol.
//	GR-10: Uses LRU cache to avoid recomputation for repeated queries.
//	If SymbolIndex is available, uses O(1) lookup; otherwise falls back to
//	graph traversal.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: The symbol to find callers for.
//
// Outputs:
//   - []*ast.Symbol: Caller symbols. Empty slice if none found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Limitations:
//   - GR-10 Review (I-1): Results may be cached. Callers MUST NOT mutate
//     the returned slice or its elements, as this would corrupt the cache.
//   - Truncated results (when limit is reached) are not cached.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindCallers(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindCallers",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// GR-10: Check cache first
	cacheKey := symbolID
	if cached, ok := a.analyticsCache.callersCache.Get(cacheKey); ok {
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("count", len(cached.Symbols)),
			attribute.Bool("truncated", cached.Truncated),
		)
		// CRITICAL: Return defensive copy to prevent cache corruption
		symbolsCopy := make([]*ast.Symbol, len(cached.Symbols))
		copy(symbolsCopy, cached.Symbols)
		return symbolsCopy, nil
	}

	// Use singleflight to prevent thundering herd on cache miss
	resultI, err, _ := a.callersGroup.Do(cacheKey, func() (any, error) {
		// Double-check cache inside singleflight
		if cached, ok := a.analyticsCache.callersCache.Get(cacheKey); ok {
			return cached, nil
		}

		result, err := a.graph.Graph.FindCallersByID(ctx, symbolID)
		if err != nil {
			return nil, err
		}

		// Only cache non-truncated results
		if !result.Truncated {
			a.analyticsCache.callersCache.Set(cacheKey, result)
		}

		return result, nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("finding callers for %s: %w", symbolID, err)
	}

	// Use comma-ok idiom for safer type assertion
	result, ok := resultI.(*QueryResult)
	if !ok {
		err := fmt.Errorf("unexpected type from singleflight group 'callersGroup': got %T", resultI)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Int("count", len(result.Symbols)),
		attribute.Bool("truncated", result.Truncated),
	)
	return result.Symbols, nil
}

// FindCallees returns symbols that the given symbol calls.
//
// Description:
//
//	Finds all symbols that are called by the source symbol via outgoing call edges.
//	GR-10: Uses LRU cache to avoid recomputation for repeated queries.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: The symbol to find callees for.
//
// Outputs:
//   - []*ast.Symbol: Callee symbols. Empty slice if none found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Limitations:
//   - GR-10 Review (I-1): Results may be cached. Callers MUST NOT mutate
//     the returned slice or its elements, as this would corrupt the cache.
//   - Truncated results (when limit is reached) are not cached.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindCallees(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindCallees",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// GR-10: Check cache first
	cacheKey := symbolID
	if cached, ok := a.analyticsCache.calleesCache.Get(cacheKey); ok {
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("count", len(cached.Symbols)),
			attribute.Bool("truncated", cached.Truncated),
		)
		// CRITICAL: Return defensive copy to prevent cache corruption
		symbolsCopy := make([]*ast.Symbol, len(cached.Symbols))
		copy(symbolsCopy, cached.Symbols)
		return symbolsCopy, nil
	}

	// Use singleflight to prevent thundering herd on cache miss
	resultI, err, _ := a.calleesGroup.Do(cacheKey, func() (any, error) {
		// Double-check cache inside singleflight
		if cached, ok := a.analyticsCache.calleesCache.Get(cacheKey); ok {
			return cached, nil
		}

		result, err := a.graph.Graph.FindCalleesByID(ctx, symbolID)
		if err != nil {
			return nil, err
		}

		// Only cache non-truncated results
		if !result.Truncated {
			a.analyticsCache.calleesCache.Set(cacheKey, result)
		}

		return result, nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("finding callees for %s: %w", symbolID, err)
	}

	// Use comma-ok idiom for safer type assertion
	result, ok := resultI.(*QueryResult)
	if !ok {
		err := fmt.Errorf("unexpected type from singleflight group 'calleesGroup': got %T", resultI)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Int("count", len(result.Symbols)),
		attribute.Bool("truncated", result.Truncated),
	)
	return result.Symbols, nil
}

// FindImplementations returns types that implement the given interface.
//
// Description:
//
//	Finds all types that implement the specified interface by name.
//	Searches across all matching interface definitions.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - interfaceName: The interface name to find implementations for.
//
// Outputs:
//   - []*ast.Symbol: Implementing types. Empty slice if none found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindImplementations(ctx context.Context, interfaceName string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindImplementations",
		trace.WithAttributes(
			attribute.String("interface_name", interfaceName),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	results, err := a.graph.Graph.FindImplementationsByName(ctx, interfaceName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("finding implementations of %s: %w", interfaceName, err)
	}

	// Collect all implementations from all matching interfaces
	var allSymbols []*ast.Symbol
	for _, result := range results {
		allSymbols = append(allSymbols, result.Symbols...)
	}

	span.SetAttributes(attribute.Int("count", len(allSymbols)))
	return allSymbols, nil
}

// FindReferences returns symbols that reference the given symbol.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: The symbol to find references for.
//
// Outputs:
//   - []*ast.Symbol: Referencing symbols. Empty slice if none found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) FindReferences(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.FindReferences",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, err
	}

	// Get the node and find all incoming edges
	node, ok := a.graph.GetNode(symbolID)
	if !ok {
		span.SetAttributes(attribute.Int("count", 0))
		return []*ast.Symbol{}, nil
	}

	var results []*ast.Symbol
	for _, edge := range node.Incoming {
		refNode, ok := a.graph.GetNode(edge.FromID)
		if ok && refNode.Symbol != nil {
			results = append(results, refNode.Symbol)
		}
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// GetEdgesByFile returns all edges with Location in the given file.
//
// Description:
//
//	GR-09: Uses edgesByFile secondary index for O(1) lookup instead of
//	O(E) scan. Useful for file-scoped dependency analysis.
//	Note: Returns edges by their Location.FilePath (where the edge is
//	expressed in code), not by source or target node file.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - filePath: The file path to find edges for.
//
// Outputs:
//   - []*Edge: Edges with Location in the file. Empty slice if none found.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) GetEdgesByFile(ctx context.Context, filePath string) ([]*Edge, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.GetEdgesByFile",
		trace.WithAttributes(
			attribute.String("file_path", filePath),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, err
	}

	// GR-09: Use edgesByFile secondary index for O(1) lookup.
	// GetEdgesByFile returns a defensive copy, so we can safely return it.
	edges := a.graph.GetEdgesByFile(filePath)

	span.SetAttributes(attribute.Int("count", len(edges)))
	return edges, nil
}

// GetEdgeCountByFile returns the count of edges with Location in the given file.
//
// Description:
//
//	GR-09: Uses secondary index for O(1) count without copying edges.
//	More efficient than len(GetEdgesByFile()) for just getting counts.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - filePath: The file path to count edges for.
//
// Outputs:
//   - int: Number of edges with Location in the file.
//   - error: Non-nil on context cancellation or adapter closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) GetEdgeCountByFile(ctx context.Context, filePath string) (int, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.GetEdgeCountByFile",
		trace.WithAttributes(
			attribute.String("file_path", filePath),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return 0, err
	}

	// GR-09: Use secondary index for O(1) count.
	count := a.graph.GetEdgeCountByFile(filePath)

	span.SetAttributes(attribute.Int("count", count))
	return count, nil
}

// -----------------------------------------------------------------------------
// Cycle Detection (GR-32)
// -----------------------------------------------------------------------------

// HasCycleFrom checks if there's a cycle in the call graph starting from the given symbol.
//
// Description:
//
//	Uses depth-first search with a recursion stack to detect cycles in the
//	call graph. Only follows EdgeTypeCalls edges.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: The starting symbol ID to check for cycles from.
//
// Outputs:
//   - bool: True if a cycle is detected, false otherwise.
//   - error: Non-nil on context cancellation or if adapter is closed.
//
// Example:
//
//	hasCycle, err := adapter.HasCycleFrom(ctx, "pkg.Function")
//	if err != nil {
//	    return fmt.Errorf("cycle check: %w", err)
//	}
//	if hasCycle {
//	    log.Warn("recursive call detected")
//	}
//
// Limitations:
//   - Only detects cycles through direct CALLS edges
//   - May miss cycles through function pointers or interfaces
//   - Performance: O(V + E) where V=nodes reachable, E=edges traversed
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) HasCycleFrom(ctx context.Context, symbolID string) (bool, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.HasCycleFrom",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return false, err
	}

	// GR-32 Code Review Fix: Pre-allocate maps with node count estimate
	nodeCount := a.graph.NodeCount()
	visited := make(map[string]bool, nodeCount)
	recStack := make(map[string]bool, nodeCount)

	hasCycle, err := a.hasCycleUtil(ctx, symbolID, visited, recStack, 0)
	if err != nil {
		span.RecordError(err)
		return false, err
	}

	span.SetAttributes(attribute.Bool("has_cycle", hasCycle))
	return hasCycle, nil
}

// hasCycleUtil is the recursive DFS helper for cycle detection.
// GR-32 Code Review Fix: Added depth parameter to prevent stack overflow.
func (a *CRSGraphAdapter) hasCycleUtil(ctx context.Context, nodeID string, visited, recStack map[string]bool, depth int) (bool, error) {
	// Check cancellation at each recursion level
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	// GR-32 Code Review Fix: Enforce depth limit to prevent stack overflow
	if depth >= maxCycleDetectionDepth {
		// Log warning when depth limit is reached - cycle detection is incomplete
		_, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.hasCycleUtil.DepthLimitReached",
			trace.WithAttributes(
				attribute.String("node_id", nodeID),
				attribute.Int("depth", depth),
				attribute.Int("max_depth", maxCycleDetectionDepth),
			),
		)
		span.AddEvent("depth_limit_reached", trace.WithAttributes(
			attribute.String("warning", "cycle detection incomplete due to depth limit"),
		))
		span.End()
		return false, nil
	}

	visited[nodeID] = true
	recStack[nodeID] = true

	// Get callees (outgoing CALLS edges)
	callees, err := a.FindCallees(ctx, nodeID)
	if err != nil {
		return false, err
	}

	for _, callee := range callees {
		if !visited[callee.ID] {
			if hasCycle, err := a.hasCycleUtil(ctx, callee.ID, visited, recStack, depth+1); err != nil || hasCycle {
				return hasCycle, err
			}
		} else if recStack[callee.ID] {
			// Found a back edge - cycle detected
			return true, nil
		}
	}

	recStack[nodeID] = false
	return false, nil
}

// CallEdgeCount returns the number of CALLS edges in the graph.
//
// Description:
//
//	Counts edges of type EdgeTypeCalls. This is used by the dependency
//	index Size() method. Results are cached and invalidated when
//	InvalidateCache() is called.
//
// Inputs:
//   - ctx: Context for cancellation.
//
// Outputs:
//   - int: Number of call edges.
//   - error: Non-nil on context cancellation or if adapter is closed.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) CallEdgeCount(ctx context.Context) (int, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.CallEdgeCount")
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, err
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return 0, err
	}

	// Use cached value if available and valid
	cache := a.analyticsCache
	cache.mu.RLock()
	if cache.callEdgeCountValid {
		count := cache.callEdgeCount
		cache.mu.RUnlock()
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("edge_count", count),
		)
		return count, nil
	}
	cache.mu.RUnlock()

	// GR-08: Use edgesByType secondary index for O(1) lookup instead of O(V+E) scan.
	// This leverages the new GetEdgeCountByType method added in GR-08.
	count := a.graph.GetEdgeCountByType(EdgeTypeCalls)

	// Cache result
	cache.mu.Lock()
	cache.callEdgeCount = count
	cache.callEdgeCountValid = true
	cache.mu.Unlock()

	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Int("edge_count", count),
	)
	return count, nil
}

// -----------------------------------------------------------------------------
// Path Queries
// -----------------------------------------------------------------------------

// maxCallChainDepth is the maximum allowed depth for call chain traversal.
const maxCallChainDepth = 100

// maxCycleDetectionDepth is the maximum DFS depth for cycle detection.
// Prevents stack overflow on very deep call graphs.
const maxCycleDetectionDepth = 1000

// GetCallChain returns the call chain from source to target.
//
// Description:
//
//	Finds the path from source to target following call edges. Uses BFS
//	for path finding to ensure shortest path.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - fromID: The source symbol ID.
//   - toID: The target symbol ID.
//   - maxDepth: Maximum traversal depth. Clamped to [1, 100].
//
// Outputs:
//   - []string: Symbol IDs in the call chain. Empty if no path found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Example:
//
//	path, err := adapter.GetCallChain(ctx, "main.go:10:main", "util.go:20:helper", 10)
//	if err != nil {
//	    return fmt.Errorf("getting call chain: %w", err)
//	}
//
// Limitations:
//   - Only follows call edges, not other edge types
//   - maxDepth is clamped to prevent runaway traversal
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) GetCallChain(ctx context.Context, fromID, toID string, maxDepth int) ([]string, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.GetCallChain",
		trace.WithAttributes(
			attribute.String("from_id", fromID),
			attribute.String("to_id", toID),
			attribute.Int("max_depth", maxDepth),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Validate and clamp maxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	if maxDepth > maxCallChainDepth {
		maxDepth = maxCallChainDepth
	}

	// Directly use BFS to find path (avoids redundant GetCallGraph traversal)
	path, err := a.reconstructPath(ctx, fromID, toID, maxDepth)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("reconstructing path from %s to %s: %w", fromID, toID, err)
	}

	span.SetAttributes(attribute.Int("path_length", len(path)))
	return path, nil
}

// reconstructPath finds the path from source to target using BFS.
//
// Description:
//
//	Uses BFS to find the shortest path between two nodes following call edges.
//	Path reconstruction uses O(n) append+reverse instead of O(n²) prepend.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) reconstructPath(ctx context.Context, fromID, toID string, maxDepth int) ([]string, error) {
	if fromID == toID {
		return []string{fromID}, nil
	}

	visited := make(map[string]bool)
	parent := make(map[string]string)
	type queueItem struct {
		id    string
		depth int
	}
	queue := []queueItem{{fromID, 0}}
	visited[fromID] = true

	for len(queue) > 0 {
		// Check context periodically
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("path search cancelled: %w", err)
		}

		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		node, ok := a.graph.GetNode(item.id)
		if !ok {
			continue
		}

		for _, edge := range node.Outgoing {
			if edge.Type != EdgeTypeCalls {
				continue
			}
			if visited[edge.ToID] {
				continue
			}

			visited[edge.ToID] = true
			parent[edge.ToID] = item.id

			if edge.ToID == toID {
				// Reconstruct path using O(n) append+reverse instead of O(n²) prepend
				path := make([]string, 0, item.depth+2)
				current := toID
				for current != "" {
					path = append(path, current)
					if current == fromID {
						break
					}
					current = parent[current]
				}
				// Reverse the path
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path, nil
			}

			queue = append(queue, queueItem{edge.ToID, item.depth + 1})
		}
	}

	return []string{}, nil
}

// ShortestPath returns the shortest path between two symbols.
//
// Description:
//
//	Finds the shortest path between two symbols considering all edge types.
//	Uses BFS for unweighted shortest path.
//	GR-10: Uses LRU cache to avoid recomputation for repeated queries.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - fromID: The source symbol ID.
//   - toID: The target symbol ID.
//
// Outputs:
//   - []string: Symbol IDs in the path. Empty if no path found.
//   - error: Non-nil on graph query failure or adapter closed.
//
// Limitations:
//   - GR-10 Review (I-1): Results may be cached. Callers MUST NOT mutate
//     the returned slice, as this would corrupt the cache.
//   - Returns error if source node does not exist.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) ShortestPath(ctx context.Context, fromID, toID string) ([]string, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.CRSGraphAdapter.ShortestPath",
		trace.WithAttributes(
			attribute.String("from_id", fromID),
			attribute.String("to_id", toID),
		),
	)
	defer span.End()

	if err := a.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// GR-10: Check cache first
	cacheKey := fromID + ":" + toID
	if cached, ok := a.analyticsCache.pathsCache.Get(cacheKey); ok {
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("path_length", cached.Length),
		)
		// CRITICAL: Return defensive copy to prevent cache corruption
		pathCopy := make([]string, len(cached.Path))
		copy(pathCopy, cached.Path)
		return pathCopy, nil
	}

	// Use singleflight to prevent thundering herd on cache miss
	resultI, err, _ := a.pathsGroup.Do(cacheKey, func() (any, error) {
		// Double-check cache inside singleflight
		if cached, ok := a.analyticsCache.pathsCache.Get(cacheKey); ok {
			return cached, nil
		}

		result, err := a.graph.Graph.ShortestPath(ctx, fromID, toID)
		if err != nil {
			return nil, err
		}

		// Cache the result (paths are not truncated)
		a.analyticsCache.pathsCache.Set(cacheKey, result)

		return result, nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("finding shortest path from %s to %s: %w", fromID, toID, err)
	}

	// Use comma-ok idiom for safer type assertion
	result, ok := resultI.(*PathResult)
	if !ok {
		err := fmt.Errorf("unexpected type from singleflight group 'pathsGroup': got %T", resultI)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Int("path_length", result.Length),
	)
	// CRITICAL: Return defensive copy to prevent cache corruption
	pathCopy := make([]string, len(result.Path))
	copy(pathCopy, result.Path)
	return pathCopy, nil
}

// -----------------------------------------------------------------------------
// Analytics
// -----------------------------------------------------------------------------

// Analytics returns the analytics query interface.
//
// Outputs:
//   - crs.GraphAnalyticsQuery: The analytics interface. Never nil.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) Analytics() crs.GraphAnalyticsQuery {
	return &crsAnalyticsAdapter{
		adapter: a,
	}
}

// crsAnalyticsAdapter implements crs.GraphAnalyticsQuery.
type crsAnalyticsAdapter struct {
	adapter *CRSGraphAdapter
}

// HotSpots returns the top N most-connected symbols.
//
// Description:
//
//	Returns the k most highly-connected nodes in the graph, sorted by
//	connectivity score (in-degree + out-degree).
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - k: Number of hotspots to return. Must be > 0.
//
// Outputs:
//   - []crs.GraphHotSpot: Top k hotspots sorted by score descending.
//   - error: Non-nil on failure or invalid input.
//
// Example:
//
//	hotspots, err := analytics.HotSpots(ctx, 10)
//	if err != nil {
//	    return fmt.Errorf("getting hotspots: %w", err)
//	}
//
// Thread Safety: Safe for concurrent use.
func (ga *crsAnalyticsAdapter) HotSpots(ctx context.Context, k int) ([]crs.GraphHotSpot, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.crsAnalyticsAdapter.HotSpots",
		trace.WithAttributes(
			attribute.Int("k", k),
		),
	)
	defer span.End()

	if err := ga.adapter.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if k <= 0 {
		return []crs.GraphHotSpot{}, nil
	}

	hotspots := ga.adapter.analytics.HotSpots(k)
	results := make([]crs.GraphHotSpot, 0, len(hotspots))
	for _, hs := range hotspots {
		// Guard against nil Symbol (node may exist without symbol data)
		name := ""
		if hs.Node != nil && hs.Node.Symbol != nil {
			name = hs.Node.Symbol.Name
		}
		symbolID := ""
		if hs.Node != nil {
			symbolID = hs.Node.ID
		}

		results = append(results, crs.GraphHotSpot{
			SymbolID:  symbolID,
			Name:      name,
			Score:     hs.Score,
			InDegree:  hs.InDegree,
			OutDegree: hs.OutDegree,
		})
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// DeadCode returns symbols that are never called/referenced.
//
// Description:
//
//	Finds symbols with no incoming edges (never called/referenced).
//	Useful for identifying unused code.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - []string: Symbol IDs of dead code.
//   - error: Non-nil on failure.
//
// Thread Safety: Safe for concurrent use.
func (ga *crsAnalyticsAdapter) DeadCode(ctx context.Context) ([]string, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.crsAnalyticsAdapter.DeadCode")
	defer span.End()

	if err := ga.adapter.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	deadCode := ga.adapter.analytics.DeadCode()
	results := make([]string, 0, len(deadCode))
	for _, dc := range deadCode {
		if dc.Node != nil {
			results = append(results, dc.Node.ID)
		}
	}

	span.SetAttributes(attribute.Int("count", len(results)))
	return results, nil
}

// CyclicDependencies returns groups of symbols with cyclic dependencies.
//
// Description:
//
//	Finds strongly connected components with more than one node,
//	indicating circular dependencies.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - [][]string: Groups of symbol IDs forming cycles (deep copy).
//   - error: Non-nil on failure.
//
// Thread Safety: Safe for concurrent use.
func (ga *crsAnalyticsAdapter) CyclicDependencies(ctx context.Context) ([][]string, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.crsAnalyticsAdapter.CyclicDependencies")
	defer span.End()

	if err := ga.adapter.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	cycles := ga.adapter.analytics.CyclicDependencies()
	// Deep copy to prevent mutation
	results := make([][]string, len(cycles))
	for i, c := range cycles {
		if c.Nodes != nil {
			results[i] = make([]string, len(c.Nodes))
			copy(results[i], c.Nodes)
		}
	}

	span.SetAttributes(attribute.Int("cycle_count", len(results)))
	return results, nil
}

// PageRank returns PageRank scores for all symbols.
//
// Description:
//
//	Results are cached and recomputed only when cache is invalidated.
//	May take significant time on first call for large graphs. Uses singleflight
//	to prevent thundering herd on cache miss.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - map[string]float64: Symbol ID to PageRank score (deep copy).
//   - error: Non-nil on failure or timeout.
//
// Example:
//
//	scores, err := analytics.PageRank(ctx)
//	if err != nil {
//	    return fmt.Errorf("computing pagerank: %w", err)
//	}
//
// Limitations:
//   - First call on large graphs may take up to PageRankTimeoutMs
//   - Results are cached for CacheTTLMs milliseconds
//
// Thread Safety: Safe for concurrent use.
func (ga *crsAnalyticsAdapter) PageRank(ctx context.Context) (map[string]float64, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.crsAnalyticsAdapter.PageRank")
	defer span.End()

	if err := ga.adapter.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Check cache (fast path)
	cache := ga.adapter.analyticsCache
	cache.mu.RLock()
	now := time.Now().UnixMilli()
	if len(cache.pageRank) > 0 && (now-cache.pageRankTimestamp) < ga.adapter.config.CacheTTLMs {
		// Deep copy to prevent mutation
		result := make(map[string]float64, len(cache.pageRank))
		for k, v := range cache.pageRank {
			result[k] = v
		}
		cache.mu.RUnlock()
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("count", len(result)),
		)
		return result, nil
	}
	cache.mu.RUnlock()

	// Use singleflight to prevent thundering herd (TOCTOU fix)
	resultI, err, shared := ga.adapter.pageRankGroup.Do("pagerank", func() (any, error) {
		// Double-check cache inside singleflight (another goroutine may have populated it)
		cache.mu.RLock()
		if len(cache.pageRank) > 0 && (time.Now().UnixMilli()-cache.pageRankTimestamp) < ga.adapter.config.CacheTTLMs {
			result := make(map[string]float64, len(cache.pageRank))
			for k, v := range cache.pageRank {
				result[k] = v
			}
			cache.mu.RUnlock()
			return result, nil
		}
		cache.mu.RUnlock()

		// Compute PageRank with timeout
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(ga.adapter.config.PageRankTimeoutMs)*time.Millisecond)
		defer cancel()

		result, err := ga.computePageRank(timeoutCtx)
		if err != nil {
			return nil, fmt.Errorf("computing pagerank: %w", err)
		}

		// Update cache
		cache.mu.Lock()
		cache.pageRank = result
		cache.pageRankTimestamp = time.Now().UnixMilli()
		cache.mu.Unlock()

		return result, nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	result := resultI.(map[string]float64)

	// Deep copy result to prevent shared state mutation
	resultCopy := make(map[string]float64, len(result))
	for k, v := range result {
		resultCopy[k] = v
	}

	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Bool("shared", shared),
		attribute.Int("count", len(resultCopy)),
	)
	return resultCopy, nil
}

// computePageRank computes PageRank scores for all nodes.
func (ga *crsAnalyticsAdapter) computePageRank(ctx context.Context) (map[string]float64, error) {
	const (
		dampingFactor = 0.85
		iterations    = 20
		tolerance     = 1e-6
	)

	n := ga.adapter.graph.NodeCount()
	if n == 0 {
		return map[string]float64{}, nil
	}

	// Collect nodes into a slice for iteration
	nodeList := make([]*Node, 0, n)
	for _, node := range ga.adapter.graph.Nodes() {
		nodeList = append(nodeList, node)
	}

	// Initialize scores
	scores := make(map[string]float64, n)
	initialScore := 1.0 / float64(n)
	for _, node := range nodeList {
		scores[node.ID] = initialScore
	}

	// Iterate
	for iter := 0; iter < iterations; iter++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		newScores := make(map[string]float64, n)
		for _, node := range nodeList {
			newScores[node.ID] = (1 - dampingFactor) / float64(n)
		}

		// Distribute scores
		for _, node := range nodeList {
			outDegree := len(node.Outgoing)
			if outDegree == 0 {
				// Distribute equally to all nodes (dangling node)
				contribution := scores[node.ID] * dampingFactor / float64(n)
				for _, nd := range nodeList {
					newScores[nd.ID] += contribution
				}
			} else {
				contribution := scores[node.ID] * dampingFactor / float64(outDegree)
				for _, edge := range node.Outgoing {
					newScores[edge.ToID] += contribution
				}
			}
		}

		// Check convergence
		maxDiff := 0.0
		for id, score := range newScores {
			diff := score - scores[id]
			if diff < 0 {
				diff = -diff
			}
			if diff > maxDiff {
				maxDiff = diff
			}
		}

		scores = newScores
		if maxDiff < tolerance {
			break
		}
	}

	return scores, nil
}

// Communities returns groups of related symbols.
//
// Description:
//
//	Results are cached and recomputed only when cache is invalidated.
//	Uses singleflight to prevent thundering herd on cache miss.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - []crs.GraphCommunity: Community groups (deep copy).
//   - error: Non-nil on failure.
//
// Example:
//
//	communities, err := analytics.Communities(ctx)
//	if err != nil {
//	    return fmt.Errorf("getting communities: %w", err)
//	}
//
// Limitations:
//   - Limited to 100 packages to prevent excessive processing
//   - Uses package boundaries as community heuristic
//
// Thread Safety: Safe for concurrent use.
func (ga *crsAnalyticsAdapter) Communities(ctx context.Context) ([]crs.GraphCommunity, error) {
	ctx, span := crsAdapterTracer.Start(ctx, "graph.crsAnalyticsAdapter.Communities")
	defer span.End()

	if err := ga.adapter.checkClosed(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("adapter closed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "context cancelled")
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Check cache (fast path)
	cache := ga.adapter.analyticsCache
	cache.mu.RLock()
	now := time.Now().UnixMilli()
	if cache.communities != nil && (now-cache.communitiesTimestamp) < ga.adapter.config.CacheTTLMs {
		// Deep copy to prevent mutation
		result := deepCopyCommunities(cache.communities)
		cache.mu.RUnlock()
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("count", len(result)),
		)
		return result, nil
	}
	cache.mu.RUnlock()

	// Use singleflight to prevent thundering herd (TOCTOU fix)
	resultI, err, shared := ga.adapter.communitiesGroup.Do("communities", func() (any, error) {
		// Double-check cache inside singleflight
		cache.mu.RLock()
		if cache.communities != nil && (time.Now().UnixMilli()-cache.communitiesTimestamp) < ga.adapter.config.CacheTTLMs {
			result := deepCopyCommunities(cache.communities)
			cache.mu.RUnlock()
			return result, nil
		}
		cache.mu.RUnlock()

		// Add timeout for community computation
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(ga.adapter.config.PageRankTimeoutMs)*time.Millisecond)
		defer cancel()

		// Use package-based communities as a simple heuristic
		packages := ga.adapter.graph.GetPackages()
		results := make([]crs.GraphCommunity, 0, len(packages))

		for i, pkg := range packages {
			// Check context in loop
			if err := timeoutCtx.Err(); err != nil {
				return nil, fmt.Errorf("computing communities: %w", err)
			}

			nodes := ga.adapter.graph.GetNodesInPackage(pkg.Name)
			symbolIDs := make([]string, 0, len(nodes))
			for _, node := range nodes {
				symbolIDs = append(symbolIDs, node.ID)
			}

			results = append(results, crs.GraphCommunity{
				ID:         pkg.Name,
				SymbolIDs:  symbolIDs,
				Modularity: float64(pkg.NodeCount) / float64(ga.adapter.graph.NodeCount()+1),
			})

			// Avoid processing too many packages
			if i >= 100 {
				break
			}
		}

		// Update cache
		cache.mu.Lock()
		cache.communities = results
		cache.communitiesTimestamp = time.Now().UnixMilli()
		cache.mu.Unlock()

		return results, nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	result := resultI.([]crs.GraphCommunity)

	// Deep copy result to prevent shared state mutation
	resultCopy := deepCopyCommunities(result)

	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Bool("shared", shared),
		attribute.Int("count", len(resultCopy)),
	)
	return resultCopy, nil
}

// deepCopyCommunities creates a deep copy of a communities slice.
func deepCopyCommunities(src []crs.GraphCommunity) []crs.GraphCommunity {
	if src == nil {
		return nil
	}
	result := make([]crs.GraphCommunity, len(src))
	for i, c := range src {
		result[i] = crs.GraphCommunity{
			ID:         c.ID,
			Modularity: c.Modularity,
			SymbolIDs:  make([]string, len(c.SymbolIDs)),
		}
		copy(result[i].SymbolIDs, c.SymbolIDs)
	}
	return result
}

// -----------------------------------------------------------------------------
// Metadata
// -----------------------------------------------------------------------------

// NodeCount returns the number of nodes in the graph.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) NodeCount() int {
	return a.graph.NodeCount()
}

// EdgeCount returns the number of edges in the graph.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) EdgeCount() int {
	return a.graph.EdgeCount()
}

// Generation returns the graph generation this adapter was created with.
//
// Description:
//
//	Use for staleness detection. If the current graph generation is higher
//	than this value, the adapter may return stale data.
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) Generation() int64 {
	return a.generation
}

// LastRefreshTime returns when the graph was last refreshed (Unix milliseconds UTC).
//
// Thread Safety: Safe for concurrent use.
func (a *CRSGraphAdapter) LastRefreshTime() int64 {
	return a.lastRefreshTime
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

// Close releases resources held by the adapter.
//
// Description:
//
//	Must be called when the adapter is no longer needed to prevent
//	resource leaks. After Close, all methods return ErrGraphQueryClosed.
//
// Thread Safety: Safe for concurrent use. Idempotent.
func (a *CRSGraphAdapter) Close() error {
	a.closed.Store(true)
	return nil
}

// checkClosed returns ErrGraphQueryClosed if the adapter has been closed.
func (a *CRSGraphAdapter) checkClosed() error {
	if a.closed.Load() {
		return crs.ErrGraphQueryClosed
	}
	return nil
}

// -----------------------------------------------------------------------------
// Cache Statistics (GR-10)
// -----------------------------------------------------------------------------

// QueryCacheStats contains statistics for query caches.
//
// Description:
//
//	Provides visibility into query cache performance for monitoring and
//	debugging. All counts are since cache creation or last Purge.
//
// Thread Safety: Fields are atomic snapshots, safe for concurrent use.
type QueryCacheStats struct {
	// CallersHits is the number of callers cache hits.
	CallersHits int64 `json:"callers_hits"`
	// CallersMisses is the number of callers cache misses.
	CallersMisses int64 `json:"callers_misses"`
	// CallersEvictions is the number of callers cache evictions.
	CallersEvictions int64 `json:"callers_evictions"`
	// CallersSize is the current number of entries in the callers cache.
	CallersSize int `json:"callers_size"`

	// CalleesHits is the number of callees cache hits.
	CalleesHits int64 `json:"callees_hits"`
	// CalleesMisses is the number of callees cache misses.
	CalleesMisses int64 `json:"callees_misses"`
	// CalleesEvictions is the number of callees cache evictions.
	CalleesEvictions int64 `json:"callees_evictions"`
	// CalleesSize is the current number of entries in the callees cache.
	CalleesSize int `json:"callees_size"`

	// PathsHits is the number of paths cache hits.
	PathsHits int64 `json:"paths_hits"`
	// PathsMisses is the number of paths cache misses.
	PathsMisses int64 `json:"paths_misses"`
	// PathsEvictions is the number of paths cache evictions.
	PathsEvictions int64 `json:"paths_evictions"`
	// PathsSize is the current number of entries in the paths cache.
	PathsSize int `json:"paths_size"`

	// TotalHits is the sum of all cache hits.
	TotalHits int64 `json:"total_hits"`
	// TotalMisses is the sum of all cache misses.
	TotalMisses int64 `json:"total_misses"`
	// TotalEvictions is the sum of all cache evictions.
	TotalEvictions int64 `json:"total_evictions"`
	// HitRate is the cache hit rate (0.0 to 1.0). Zero if no queries.
	HitRate float64 `json:"hit_rate"`
}

// QueryCacheStats returns statistics for the query caches.
//
// Description:
//
//	Returns hit/miss counts and sizes for all query caches (callers,
//	callees, paths). Useful for monitoring cache effectiveness and
//	sizing decisions.
//
// Outputs:
//   - QueryCacheStats: Cache statistics snapshot.
//
// Example:
//
//	stats := adapter.QueryCacheStats()
//	if stats.HitRate < 0.5 {
//	    log.Warn("low cache hit rate", "rate", stats.HitRate)
//	}
//
// Limitations:
//   - Stats are a point-in-time snapshot; may be slightly stale
//   - Hit rate calculation uses integer counts, may lose precision
//
// Thread Safety: Safe for concurrent use. Returns atomic snapshot.
func (a *CRSGraphAdapter) QueryCacheStats() QueryCacheStats {
	cache := a.analyticsCache

	// GR-10 Review Fix (R-3): Hold read lock to prevent inconsistent reads
	// during concurrent InvalidateCache() calls
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	var stats QueryCacheStats

	// Callers cache stats
	if cache.callersCache != nil {
		stats.CallersHits, stats.CallersMisses = cache.callersCache.Stats()
		stats.CallersEvictions = cache.callersCache.Evictions()
		stats.CallersSize = cache.callersCache.Len()
	}

	// Callees cache stats
	if cache.calleesCache != nil {
		stats.CalleesHits, stats.CalleesMisses = cache.calleesCache.Stats()
		stats.CalleesEvictions = cache.calleesCache.Evictions()
		stats.CalleesSize = cache.calleesCache.Len()
	}

	// Paths cache stats
	if cache.pathsCache != nil {
		stats.PathsHits, stats.PathsMisses = cache.pathsCache.Stats()
		stats.PathsEvictions = cache.pathsCache.Evictions()
		stats.PathsSize = cache.pathsCache.Len()
	}

	// Compute totals
	stats.TotalHits = stats.CallersHits + stats.CalleesHits + stats.PathsHits
	stats.TotalMisses = stats.CallersMisses + stats.CalleesMisses + stats.PathsMisses
	stats.TotalEvictions = stats.CallersEvictions + stats.CalleesEvictions + stats.PathsEvictions

	// Compute hit rate
	total := stats.TotalHits + stats.TotalMisses
	if total > 0 {
		stats.HitRate = float64(stats.TotalHits) / float64(total)
	}

	return stats
}

// -----------------------------------------------------------------------------
// Compile-time Interface Check
// -----------------------------------------------------------------------------

var _ crs.GraphQuery = (*CRSGraphAdapter)(nil)
var _ crs.GraphAnalyticsQuery = (*crsAnalyticsAdapter)(nil)
