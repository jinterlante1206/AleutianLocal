// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Query Type Constants
// -----------------------------------------------------------------------------

// QueryType classifies the type of graph query performed.
type QueryType string

const (
	// QueryTypeCallers finds functions that call a given symbol.
	QueryTypeCallers QueryType = "callers"

	// QueryTypeCallees finds functions called by a given symbol.
	QueryTypeCallees QueryType = "callees"

	// QueryTypePath finds the shortest path between two symbols.
	QueryTypePath QueryType = "path"

	// QueryTypeHotspots finds the most connected symbols.
	QueryTypeHotspots QueryType = "hotspots"

	// QueryTypeDeadCode finds unreachable code.
	QueryTypeDeadCode QueryType = "dead_code"

	// QueryTypeCycles detects cyclic dependencies.
	QueryTypeCycles QueryType = "cycles"

	// QueryTypeReferences finds all references to a symbol.
	QueryTypeReferences QueryType = "references"

	// QueryTypeSymbol finds a symbol by name.
	QueryTypeSymbol QueryType = "symbol"

	// QueryTypeImplementations finds implementations of an interface.
	QueryTypeImplementations QueryType = "implementations"

	// QueryTypeCallChain finds the call chain between symbols.
	QueryTypeCallChain QueryType = "call_chain"
)

// -----------------------------------------------------------------------------
// Graph Context Limits
// -----------------------------------------------------------------------------

const (
	// MaxFilesPerContext limits the number of files tracked per event.
	MaxFilesPerContext = 100

	// MaxSymbolsPerContext limits the number of symbols tracked per event.
	MaxSymbolsPerContext = 100

	// DefaultSliceCapacity is the pre-allocation size for slices.
	DefaultSliceCapacity = 10
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNegativeResultCount is returned when ResultCount is negative.
	ErrNegativeResultCount = errors.New("result_count must be non-negative")

	// ErrNegativeNodeCount is returned when NodeCount is negative.
	ErrNegativeNodeCount = errors.New("node_count must be non-negative")

	// ErrNegativeEdgeCount is returned when EdgeCount is negative.
	ErrNegativeEdgeCount = errors.New("edge_count must be non-negative")

	// ErrNegativeGraphGeneration is returned when GraphGeneration is negative.
	ErrNegativeGraphGeneration = errors.New("graph_generation must be non-negative")
)

// -----------------------------------------------------------------------------
// Graph Context
// -----------------------------------------------------------------------------

// GraphContext provides structured graph information for events.
//
// Description:
//
//	Contains file, symbol, and graph state information associated with
//	an agent event. This enables activities to make graph-informed decisions.
//
// Thread Safety: NOT safe for concurrent modification.
// Acquire from pool, populate, use, then release back to pool.
type GraphContext struct {
	// FilesRead contains paths of files that were read during the operation.
	// Paths are relative to project root. Max 100 entries.
	FilesRead []string `json:"files_read,omitempty"`

	// FilesModified contains paths of files that were modified.
	// Paths are relative to project root. Max 100 entries.
	FilesModified []string `json:"files_modified,omitempty"`

	// FilesCreated contains paths of files that were created.
	// Paths are relative to project root. Max 100 entries.
	FilesCreated []string `json:"files_created,omitempty"`

	// SymbolsQueried contains IDs of symbols that were looked up.
	// Max 100 entries.
	SymbolsQueried []string `json:"symbols_queried,omitempty"`

	// SymbolsFound contains IDs of symbols that were returned in results.
	// Max 100 entries.
	SymbolsFound []string `json:"symbols_found,omitempty"`

	// SymbolsModified contains IDs of symbols affected by file changes.
	// Max 100 entries.
	SymbolsModified []string `json:"symbols_modified,omitempty"`

	// NodeCount is the current graph node count.
	NodeCount int `json:"node_count"`

	// EdgeCount is the current graph edge count.
	EdgeCount int `json:"edge_count"`

	// GraphGeneration is the graph generation for staleness detection.
	// Coordinates with GR-28 adapter generation.
	GraphGeneration int64 `json:"graph_generation"`

	// RefreshTime is the last graph refresh time (Unix milliseconds UTC).
	// Uses int64 per CLAUDE.md timestamp standard.
	RefreshTime int64 `json:"refresh_time,omitempty"`

	// QueryType is the type of graph query performed.
	// Use QueryType constants, not free-form strings.
	QueryType QueryType `json:"query_type,omitempty"`

	// QueryTarget is what was being searched for.
	QueryTarget string `json:"query_target,omitempty"`

	// ResultCount is the number of results returned.
	ResultCount int `json:"result_count,omitempty"`
}

// Reset clears the GraphContext for reuse via pool.
//
// Description:
//
//	Resets all fields to zero values while preserving slice capacity.
//	Called by ReleaseGraphContext before returning to pool.
//
// Thread Safety: NOT safe for concurrent use. Only call when you have
// exclusive ownership of the GraphContext (before pool release).
func (gc *GraphContext) Reset() {
	// Clear slices but preserve capacity
	gc.FilesRead = gc.FilesRead[:0]
	gc.FilesModified = gc.FilesModified[:0]
	gc.FilesCreated = gc.FilesCreated[:0]
	gc.SymbolsQueried = gc.SymbolsQueried[:0]
	gc.SymbolsFound = gc.SymbolsFound[:0]
	gc.SymbolsModified = gc.SymbolsModified[:0]

	// Clear numeric fields
	gc.NodeCount = 0
	gc.EdgeCount = 0
	gc.GraphGeneration = 0
	gc.RefreshTime = 0
	gc.ResultCount = 0

	// Clear string fields (O-1 fix: explicitly clear to release memory)
	gc.QueryType = ""
	gc.QueryTarget = ""
}

// Validate checks the GraphContext for invalid values.
//
// Description:
//
//	Validates all numeric fields are non-negative. Call before using
//	a GraphContext that may have been populated from untrusted sources.
//
// Outputs:
//   - error: Non-nil if validation fails. Returns the first error found.
//
// Thread Safety: Safe for concurrent use (read-only operation).
func (gc *GraphContext) Validate() error {
	if gc.ResultCount < 0 {
		return ErrNegativeResultCount
	}
	if gc.NodeCount < 0 {
		return ErrNegativeNodeCount
	}
	if gc.EdgeCount < 0 {
		return ErrNegativeEdgeCount
	}
	// R-3 fix: Validate GraphGeneration
	if gc.GraphGeneration < 0 {
		return ErrNegativeGraphGeneration
	}
	return nil
}

// -----------------------------------------------------------------------------
// Graph Context Pool
// -----------------------------------------------------------------------------

// graphContextPool provides reusable GraphContext instances.
var graphContextPool = sync.Pool{
	New: func() any {
		return &GraphContext{
			FilesRead:       make([]string, 0, DefaultSliceCapacity),
			FilesModified:   make([]string, 0, DefaultSliceCapacity),
			FilesCreated:    make([]string, 0, DefaultSliceCapacity),
			SymbolsQueried:  make([]string, 0, DefaultSliceCapacity),
			SymbolsFound:    make([]string, 0, DefaultSliceCapacity),
			SymbolsModified: make([]string, 0, DefaultSliceCapacity),
		}
	},
}

// AcquireGraphContext gets a GraphContext from the pool.
//
// Description:
//
//	Returns a reset GraphContext ready for use. The caller should
//	call ReleaseGraphContext when done to return it to the pool.
//
// Outputs:
//
//	*GraphContext - A reset, reusable GraphContext instance.
//
// Thread Safety: Safe for concurrent use.
func AcquireGraphContext() *GraphContext {
	return graphContextPool.Get().(*GraphContext)
}

// ReleaseGraphContext returns a GraphContext to the pool.
//
// Description:
//
//	Resets the GraphContext and returns it to the pool for reuse.
//	Safe to call with nil.
//
// Inputs:
//
//	gc - The GraphContext to release. May be nil.
//
// Thread Safety: Safe for concurrent use.
func ReleaseGraphContext(gc *GraphContext) {
	if gc == nil {
		return
	}
	gc.Reset()
	graphContextPool.Put(gc)
}

// -----------------------------------------------------------------------------
// Graph Query Interface
// -----------------------------------------------------------------------------

// GraphStateProvider provides graph state information.
//
// Description:
//
//	Interface for components that can provide graph state (node count,
//	edge count, generation). This allows the builder to work with
//	different graph implementations.
type GraphStateProvider interface {
	// NodeCount returns the number of nodes in the graph.
	NodeCount() int

	// EdgeCount returns the number of edges in the graph.
	EdgeCount() int

	// Generation returns the graph generation for staleness detection.
	Generation() int64
}

// -----------------------------------------------------------------------------
// Graph Context Builder
// -----------------------------------------------------------------------------

// GraphContextBuilder constructs GraphContext instances using a fluent API.
//
// Description:
//
//	Provides a convenient way to build GraphContext with validation
//	and automatic limit enforcement.
//
// Example:
//
//	gc, err := NewGraphContextBuilder().
//	    WithFilesRead("main.go", "util.go").
//	    WithQuery(QueryTypeCallers, "MyFunc", 5).
//	    Build()
//
// Thread Safety: NOT safe for concurrent use.
type GraphContextBuilder struct {
	ctx *GraphContext
}

// NewGraphContextBuilder creates a new GraphContextBuilder.
//
// Outputs:
//
//	*GraphContextBuilder - A new builder instance.
func NewGraphContextBuilder() *GraphContextBuilder {
	return &GraphContextBuilder{
		ctx: AcquireGraphContext(),
	}
}

// NewGraphContextBuilderWithContext creates a builder using an existing context.
//
// Description:
//
//	Useful when you want to extend an existing GraphContext.
//	If gc is nil, acquires a new context from the pool.
//
// Inputs:
//
//	gc - The existing GraphContext to build upon. May be nil.
//
// Outputs:
//
//	*GraphContextBuilder - A new builder wrapping the provided context.
func NewGraphContextBuilderWithContext(gc *GraphContext) *GraphContextBuilder {
	// R-2 fix: Handle nil context safely
	if gc == nil {
		gc = AcquireGraphContext()
	}
	return &GraphContextBuilder{
		ctx: gc,
	}
}

// WithFilesRead adds files that were read.
//
// Inputs:
//
//	files - File paths to add (relative to project root).
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithFilesRead(files ...string) *GraphContextBuilder {
	b.ctx.FilesRead = append(b.ctx.FilesRead, files...)
	return b
}

// WithFilesModified adds files that were modified.
//
// Inputs:
//
//	files - File paths to add (relative to project root).
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithFilesModified(files ...string) *GraphContextBuilder {
	b.ctx.FilesModified = append(b.ctx.FilesModified, files...)
	return b
}

// WithFilesCreated adds files that were created.
//
// Inputs:
//
//	files - File paths to add (relative to project root).
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithFilesCreated(files ...string) *GraphContextBuilder {
	b.ctx.FilesCreated = append(b.ctx.FilesCreated, files...)
	return b
}

// WithSymbolsQueried adds symbol IDs that were looked up.
//
// Inputs:
//
//	ids - Symbol IDs that were queried.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithSymbolsQueried(ids ...string) *GraphContextBuilder {
	b.ctx.SymbolsQueried = append(b.ctx.SymbolsQueried, ids...)
	return b
}

// WithSymbolsFound adds symbol IDs that were returned in results.
//
// Inputs:
//
//	ids - Symbol IDs that were found.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithSymbolsFound(ids ...string) *GraphContextBuilder {
	b.ctx.SymbolsFound = append(b.ctx.SymbolsFound, ids...)
	return b
}

// WithSymbolsModified adds symbol IDs affected by file changes.
//
// Inputs:
//
//	ids - Symbol IDs that were modified.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithSymbolsModified(ids ...string) *GraphContextBuilder {
	b.ctx.SymbolsModified = append(b.ctx.SymbolsModified, ids...)
	return b
}

// WithGraphState adds graph state from a provider.
//
// Description:
//
//	Sets node count, edge count, and generation from a GraphStateProvider.
//	Safe to call with nil provider.
//
// Inputs:
//
//	provider - The graph state provider. May be nil.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithGraphState(provider GraphStateProvider) *GraphContextBuilder {
	if provider != nil {
		b.ctx.NodeCount = provider.NodeCount()
		b.ctx.EdgeCount = provider.EdgeCount()
		b.ctx.GraphGeneration = provider.Generation()
	}
	return b
}

// WithGraphCounts sets node and edge counts directly.
//
// Inputs:
//
//	nodeCount - Number of nodes in the graph.
//	edgeCount - Number of edges in the graph.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithGraphCounts(nodeCount, edgeCount int) *GraphContextBuilder {
	b.ctx.NodeCount = nodeCount
	b.ctx.EdgeCount = edgeCount
	return b
}

// WithGraphGeneration sets the graph generation.
//
// Inputs:
//
//	generation - The graph generation number.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithGraphGeneration(generation int64) *GraphContextBuilder {
	b.ctx.GraphGeneration = generation
	return b
}

// WithRefreshTime sets the last graph refresh time.
//
// Inputs:
//
//	refreshTime - Unix milliseconds UTC of last refresh.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithRefreshTime(refreshTime int64) *GraphContextBuilder {
	b.ctx.RefreshTime = refreshTime
	return b
}

// WithRefreshTimeNow sets the refresh time to now.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithRefreshTimeNow() *GraphContextBuilder {
	b.ctx.RefreshTime = time.Now().UnixMilli()
	return b
}

// WithQuery sets query metadata.
//
// Inputs:
//
//	queryType - The type of query performed.
//	target - What was being searched for.
//	resultCount - Number of results returned.
//
// Outputs:
//
//	*GraphContextBuilder - The builder for chaining.
func (b *GraphContextBuilder) WithQuery(queryType QueryType, target string, resultCount int) *GraphContextBuilder {
	b.ctx.QueryType = queryType
	b.ctx.QueryTarget = target
	b.ctx.ResultCount = resultCount
	return b
}

// enforceLimits truncates slices to their maximum allowed sizes.
// Returns true if any truncation occurred.
//
// I-1/M-3 fix: Extracted from Build()/BuildUnsafe() to eliminate duplication.
func (b *GraphContextBuilder) enforceLimits() bool {
	truncated := false

	if len(b.ctx.FilesRead) > MaxFilesPerContext {
		b.ctx.FilesRead = b.ctx.FilesRead[:MaxFilesPerContext]
		truncated = true
	}
	if len(b.ctx.FilesModified) > MaxFilesPerContext {
		b.ctx.FilesModified = b.ctx.FilesModified[:MaxFilesPerContext]
		truncated = true
	}
	if len(b.ctx.FilesCreated) > MaxFilesPerContext {
		b.ctx.FilesCreated = b.ctx.FilesCreated[:MaxFilesPerContext]
		truncated = true
	}
	if len(b.ctx.SymbolsQueried) > MaxSymbolsPerContext {
		b.ctx.SymbolsQueried = b.ctx.SymbolsQueried[:MaxSymbolsPerContext]
		truncated = true
	}
	if len(b.ctx.SymbolsFound) > MaxSymbolsPerContext {
		b.ctx.SymbolsFound = b.ctx.SymbolsFound[:MaxSymbolsPerContext]
		truncated = true
	}
	if len(b.ctx.SymbolsModified) > MaxSymbolsPerContext {
		b.ctx.SymbolsModified = b.ctx.SymbolsModified[:MaxSymbolsPerContext]
		truncated = true
	}

	return truncated
}

// Build validates and returns the constructed GraphContext.
//
// Description:
//
//	Validates the GraphContext and enforces limits on slice sizes.
//	Returns an error if validation fails. On error, the context is
//	released back to the pool to prevent memory leaks.
//
// Outputs:
//
//	*GraphContext - The constructed context. Never nil on success.
//	error - Non-nil if validation fails.
func (b *GraphContextBuilder) Build() (*GraphContext, error) {
	// Enforce limits and log if truncation occurred (L-3/I-2 fix)
	if b.enforceLimits() {
		slog.Debug("GraphContext limits enforced, data truncated",
			slog.Int("max_files", MaxFilesPerContext),
			slog.Int("max_symbols", MaxSymbolsPerContext),
		)
	}

	// Validate
	if err := b.ctx.Validate(); err != nil {
		// R-1 fix: Release context back to pool on validation failure
		slog.Debug("GraphContext validation failed, releasing to pool",
			slog.String("error", err.Error()),
		)
		ReleaseGraphContext(b.ctx)
		b.ctx = nil
		return nil, err
	}

	return b.ctx, nil
}

// BuildUnsafe returns the GraphContext without validation.
//
// Description:
//
//	Use when you've already validated or don't need validation.
//	Still enforces size limits. Logs if truncation occurs.
//
// Outputs:
//
//	*GraphContext - The constructed context.
func (b *GraphContextBuilder) BuildUnsafe() *GraphContext {
	// Enforce limits and log if truncation occurred (L-3/I-2 fix)
	if b.enforceLimits() {
		slog.Debug("GraphContext limits enforced, data truncated",
			slog.Int("max_files", MaxFilesPerContext),
			slog.Int("max_symbols", MaxSymbolsPerContext),
		)
	}

	return b.ctx
}
