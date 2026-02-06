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

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrGraphQueryClosed is returned when operations are attempted on a closed adapter.
	ErrGraphQueryClosed = errors.New("graph query adapter is closed")

	// ErrGraphNotAvailable is returned when graph is not available.
	ErrGraphNotAvailable = errors.New("graph is not available")
)

// -----------------------------------------------------------------------------
// GraphQuery Interface (GR-28)
// -----------------------------------------------------------------------------

// GraphQuery provides read-only access to the code graph from CRS activities.
//
// Description:
//
//	GraphQuery is the interface that allows CRS activities to query the actual
//	code graph for structural information. This enables activities to use graph
//	algorithms (PageRank, community detection, etc.) rather than relying solely
//	on CRS's internal DependencyIndex.
//
//	The interface is read-only from the CRS perspective - the graph is owned
//	by the graph package and mutations happen via graph refresh, not CRS deltas.
//
// Thread Safety: All methods are safe for concurrent use.
type GraphQuery interface {
	// -------------------------------------------------------------------------
	// Node Queries
	// -------------------------------------------------------------------------

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
	FindSymbolByID(ctx context.Context, id string) (*ast.Symbol, bool, error)

	// FindSymbolsByName returns all symbols with the given name.
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
	FindSymbolsByName(ctx context.Context, name string) ([]*ast.Symbol, error)

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
	FindSymbolsByKind(ctx context.Context, kind ast.SymbolKind) ([]*ast.Symbol, error)

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
	FindSymbolsInFile(ctx context.Context, filePath string) ([]*ast.Symbol, error)

	// -------------------------------------------------------------------------
	// Edge Queries
	// -------------------------------------------------------------------------

	// FindCallers returns symbols that call the given symbol.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - symbolID: The symbol to find callers for.
	//
	// Outputs:
	//   - []*ast.Symbol: Caller symbols. Empty slice if none found.
	//   - error: Non-nil on graph query failure or adapter closed.
	//
	// Thread Safety: Safe for concurrent use.
	FindCallers(ctx context.Context, symbolID string) ([]*ast.Symbol, error)

	// FindCallees returns symbols that the given symbol calls.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - symbolID: The symbol to find callees for.
	//
	// Outputs:
	//   - []*ast.Symbol: Callee symbols. Empty slice if none found.
	//   - error: Non-nil on graph query failure or adapter closed.
	//
	// Thread Safety: Safe for concurrent use.
	FindCallees(ctx context.Context, symbolID string) ([]*ast.Symbol, error)

	// FindImplementations returns types that implement the given interface.
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
	FindImplementations(ctx context.Context, interfaceName string) ([]*ast.Symbol, error)

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
	FindReferences(ctx context.Context, symbolID string) ([]*ast.Symbol, error)

	// -------------------------------------------------------------------------
	// Path Queries
	// -------------------------------------------------------------------------

	// GetCallChain returns the call chain from source to target.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - fromID: The source symbol ID.
	//   - toID: The target symbol ID.
	//   - maxDepth: Maximum traversal depth.
	//
	// Outputs:
	//   - []string: Symbol IDs in the call chain. Empty if no path found.
	//   - error: Non-nil on graph query failure or adapter closed.
	//
	// Thread Safety: Safe for concurrent use.
	GetCallChain(ctx context.Context, fromID, toID string, maxDepth int) ([]string, error)

	// ShortestPath returns the shortest path between two symbols.
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
	// Thread Safety: Safe for concurrent use.
	ShortestPath(ctx context.Context, fromID, toID string) ([]string, error)

	// -------------------------------------------------------------------------
	// Analytics
	// -------------------------------------------------------------------------

	// Analytics returns the analytics query interface.
	//
	// Outputs:
	//   - GraphAnalyticsQuery: The analytics interface. Never nil.
	//
	// Thread Safety: Safe for concurrent use.
	Analytics() GraphAnalyticsQuery

	// -------------------------------------------------------------------------
	// Metadata
	// -------------------------------------------------------------------------

	// NodeCount returns the number of nodes in the graph.
	//
	// Thread Safety: Safe for concurrent use.
	NodeCount() int

	// EdgeCount returns the number of edges in the graph.
	//
	// Thread Safety: Safe for concurrent use.
	EdgeCount() int

	// Generation returns the graph generation this adapter was created with.
	//
	// Description:
	//
	//   Use for staleness detection. If the current graph generation is higher
	//   than this value, the adapter may return stale data.
	//
	// Thread Safety: Safe for concurrent use.
	Generation() int64

	// LastRefreshTime returns when the graph was last refreshed (Unix milliseconds UTC).
	//
	// Thread Safety: Safe for concurrent use.
	LastRefreshTime() int64

	// -------------------------------------------------------------------------
	// Lifecycle
	// -------------------------------------------------------------------------

	// Close releases resources held by the adapter.
	//
	// Description:
	//
	//   Must be called when the adapter is no longer needed to prevent
	//   resource leaks. After Close, all methods return ErrGraphQueryClosed.
	//
	// Thread Safety: Safe for concurrent use. Idempotent.
	Close() error
}

// -----------------------------------------------------------------------------
// GraphAnalyticsQuery Interface
// -----------------------------------------------------------------------------

// GraphAnalyticsQuery provides read-only access to graph analytics results.
//
// Description:
//
//	Analytics results may be cached for performance. The cache is invalidated
//	when InvalidateCache() is called on the adapter.
//
// Thread Safety: All methods are safe for concurrent use.
type GraphAnalyticsQuery interface {
	// HotSpots returns the top N most-connected symbols.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - k: Number of hotspots to return.
	//
	// Outputs:
	//   - []GraphHotSpot: Top k hotspots sorted by score descending.
	//   - error: Non-nil on failure.
	//
	// Thread Safety: Safe for concurrent use.
	HotSpots(ctx context.Context, k int) ([]GraphHotSpot, error)

	// DeadCode returns symbols that are never called/referenced.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - []string: Symbol IDs of dead code.
	//   - error: Non-nil on failure.
	//
	// Thread Safety: Safe for concurrent use.
	DeadCode(ctx context.Context) ([]string, error)

	// CyclicDependencies returns groups of symbols with cyclic dependencies.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - [][]string: Groups of symbol IDs forming cycles.
	//   - error: Non-nil on failure.
	//
	// Thread Safety: Safe for concurrent use.
	CyclicDependencies(ctx context.Context) ([][]string, error)

	// PageRank returns PageRank scores for all symbols.
	//
	// Description:
	//
	//   Results are cached and recomputed only when cache is invalidated.
	//   May take significant time on first call for large graphs.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - map[string]float64: Symbol ID to PageRank score.
	//   - error: Non-nil on failure or timeout.
	//
	// Thread Safety: Safe for concurrent use.
	PageRank(ctx context.Context) (map[string]float64, error)

	// Communities returns groups of related symbols.
	//
	// Description:
	//
	//   Results are cached and recomputed only when cache is invalidated.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - []GraphCommunity: Community groups.
	//   - error: Non-nil on failure.
	//
	// Thread Safety: Safe for concurrent use.
	Communities(ctx context.Context) ([]GraphCommunity, error)
}

// -----------------------------------------------------------------------------
// Analytics Result Types
// -----------------------------------------------------------------------------

// GraphHotSpot represents a highly-connected symbol in the graph.
type GraphHotSpot struct {
	// SymbolID is the unique symbol identifier.
	SymbolID string `json:"symbol_id"`

	// Name is the symbol name.
	Name string `json:"name"`

	// Score is the connectivity score (higher = more connected).
	Score int `json:"score"`

	// InDegree is the number of incoming edges.
	InDegree int `json:"in_degree"`

	// OutDegree is the number of outgoing edges.
	OutDegree int `json:"out_degree"`
}

// GraphCommunity represents a group of related symbols.
type GraphCommunity struct {
	// ID is the community identifier.
	ID string `json:"id"`

	// SymbolIDs are the symbols in this community.
	SymbolIDs []string `json:"symbol_ids"`

	// Modularity is the community's modularity score.
	Modularity float64 `json:"modularity"`
}

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// GraphQueryConfig configures the GraphQuery adapter.
type GraphQueryConfig struct {
	// CacheTTLMs is how long cached analytics results are valid (milliseconds).
	// Default: 300000 (5 minutes)
	CacheTTLMs int64

	// PageRankTimeoutMs is the maximum time for PageRank computation (milliseconds).
	// Default: 30000 (30 seconds)
	PageRankTimeoutMs int64
}

// DefaultGraphQueryConfig returns the default configuration.
func DefaultGraphQueryConfig() *GraphQueryConfig {
	return &GraphQueryConfig{
		CacheTTLMs:        300000, // 5 minutes
		PageRankTimeoutMs: 30000,  // 30 seconds
	}
}
