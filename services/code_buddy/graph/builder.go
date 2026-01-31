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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// Default builder configuration values.
const (
	// DefaultMaxMemoryMB is the default memory limit for building (512MB).
	DefaultMaxMemoryMB = 512

	// DefaultWorkerCount is the default number of parallel workers.
	// Set to 0 to use runtime.NumCPU().
	DefaultWorkerCount = 0
)

// ProgressPhase indicates which phase of building is in progress.
type ProgressPhase int

const (
	// ProgressPhaseCollecting indicates symbols are being collected as nodes.
	ProgressPhaseCollecting ProgressPhase = iota

	// ProgressPhaseExtractingEdges indicates edges are being extracted.
	ProgressPhaseExtractingEdges

	// ProgressPhaseFinalizing indicates the graph is being finalized.
	ProgressPhaseFinalizing
)

// String returns the string representation of the ProgressPhase.
func (p ProgressPhase) String() string {
	switch p {
	case ProgressPhaseCollecting:
		return "collecting"
	case ProgressPhaseExtractingEdges:
		return "extracting_edges"
	case ProgressPhaseFinalizing:
		return "finalizing"
	default:
		return "unknown"
	}
}

// BuildProgress contains progress information during a build.
type BuildProgress struct {
	// Phase is the current build phase.
	Phase ProgressPhase

	// FilesTotal is the total number of files to process.
	FilesTotal int

	// FilesProcessed is the number of files processed so far.
	FilesProcessed int

	// NodesCreated is the number of nodes created so far.
	NodesCreated int

	// EdgesCreated is the number of edges created so far.
	EdgesCreated int
}

// ProgressFunc is a callback function for build progress updates.
type ProgressFunc func(progress BuildProgress)

// BuilderOptions configures Builder behavior.
type BuilderOptions struct {
	// ProjectRoot is the absolute path to the project root directory.
	ProjectRoot string

	// MaxMemoryMB is the maximum memory usage in megabytes.
	// Build will stop with partial results if exceeded.
	// Default: 512
	MaxMemoryMB int

	// WorkerCount is the number of parallel workers for edge extraction.
	// Default: runtime.NumCPU()
	WorkerCount int

	// ProgressCallback is called periodically with build progress.
	// May be nil.
	ProgressCallback ProgressFunc

	// MaxNodes is the maximum number of nodes (passed to Graph).
	MaxNodes int

	// MaxEdges is the maximum number of edges (passed to Graph).
	MaxEdges int
}

// DefaultBuilderOptions returns sensible defaults.
func DefaultBuilderOptions() BuilderOptions {
	return BuilderOptions{
		MaxMemoryMB: DefaultMaxMemoryMB,
		WorkerCount: runtime.NumCPU(),
		MaxNodes:    DefaultMaxNodes,
		MaxEdges:    DefaultMaxEdges,
	}
}

// BuilderOption is a functional option for configuring Builder.
type BuilderOption func(*BuilderOptions)

// WithProjectRoot sets the project root path.
func WithProjectRoot(root string) BuilderOption {
	return func(o *BuilderOptions) {
		o.ProjectRoot = root
	}
}

// WithMaxMemoryMB sets the maximum memory usage in megabytes.
func WithMaxMemoryMB(mb int) BuilderOption {
	return func(o *BuilderOptions) {
		o.MaxMemoryMB = mb
	}
}

// WithWorkerCount sets the number of parallel workers.
func WithWorkerCount(n int) BuilderOption {
	return func(o *BuilderOptions) {
		o.WorkerCount = n
	}
}

// WithProgressCallback sets the progress callback function.
func WithProgressCallback(fn ProgressFunc) BuilderOption {
	return func(o *BuilderOptions) {
		o.ProgressCallback = fn
	}
}

// WithBuilderMaxNodes sets the maximum number of nodes.
func WithBuilderMaxNodes(n int) BuilderOption {
	return func(o *BuilderOptions) {
		o.MaxNodes = n
	}
}

// WithBuilderMaxEdges sets the maximum number of edges.
func WithBuilderMaxEdges(n int) BuilderOption {
	return func(o *BuilderOptions) {
		o.MaxEdges = n
	}
}

// Builder constructs code graphs from parsed AST results.
//
// The builder is stateless and can be reused across multiple builds.
// Each Build() call creates a new graph.
//
// Thread Safety:
//
//	Builder is safe for concurrent use. Each Build() call operates
//	independently with its own internal state.
type Builder struct {
	options BuilderOptions
}

// NewBuilder creates a new Builder with the given options.
//
// Example:
//
//	builder := NewBuilder(
//	    WithProjectRoot("/path/to/project"),
//	    WithMaxMemoryMB(1024),
//	)
func NewBuilder(opts ...BuilderOption) *Builder {
	options := DefaultBuilderOptions()
	for _, opt := range opts {
		opt(&options)
	}

	if options.WorkerCount <= 0 {
		options.WorkerCount = runtime.NumCPU()
	}

	return &Builder{
		options: options,
	}
}

// buildState holds mutable state during a single build operation.
type buildState struct {
	graph         *Graph
	result        *BuildResult
	symbolsByID   map[string]*ast.Symbol
	symbolsByName map[string][]*ast.Symbol
	fileImports   map[string][]ast.Import // filePath -> imports
	placeholders  map[string]*Node        // external ID -> placeholder node
	mu            sync.Mutex              // protects placeholders
	startTime     time.Time
}

// Build constructs a graph from the given parse results.
//
// Description:
//
//	Processes all parse results, creating nodes for symbols and edges
//	for their relationships. The build is resilient to individual file
//	failures - partial results are returned even on errors.
//
// Inputs:
//
//	ctx - Context for cancellation. Build checks context periodically.
//	results - Parse results from AST parsing. Nil entries are skipped with error.
//
// Outputs:
//
//	*BuildResult - Contains the graph, any errors, and build statistics.
//	error - Non-nil only for fatal errors (context cancelled returns partial result).
//
// Build Phases:
//
//  1. COLLECT: Validate and add all symbols as nodes
//  2. EXTRACT EDGES: Create edges for imports, calls, implements, etc.
//  3. FINALIZE: Freeze graph and compute statistics
func (b *Builder) Build(ctx context.Context, results []*ast.ParseResult) (*BuildResult, error) {
	// Start tracing span
	ctx, span := startBuildSpan(ctx, len(results))
	defer span.End()

	state := &buildState{
		graph: NewGraph(b.options.ProjectRoot,
			WithMaxNodes(b.options.MaxNodes),
			WithMaxEdges(b.options.MaxEdges),
		),
		result: &BuildResult{
			FileErrors: make([]FileError, 0),
			EdgeErrors: make([]EdgeError, 0),
		},
		symbolsByID:   make(map[string]*ast.Symbol),
		symbolsByName: make(map[string][]*ast.Symbol),
		fileImports:   make(map[string][]ast.Import),
		placeholders:  make(map[string]*Node),
		startTime:     time.Now(),
	}
	state.result.Graph = state.graph

	// Phase 1: Collect symbols as nodes
	if err := b.collectPhase(ctx, state, results); err != nil {
		state.result.Incomplete = true
		state.result.Stats.DurationMilli = time.Since(state.startTime).Milliseconds()
		setBuildSpanResult(span, state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, true)
		recordBuildMetrics(ctx, time.Since(state.startTime), state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, false)
		return state.result, nil
	}

	// Phase 2: Extract edges
	if err := b.extractEdgesPhase(ctx, state, results); err != nil {
		state.result.Incomplete = true
		state.result.Stats.DurationMilli = time.Since(state.startTime).Milliseconds()
		setBuildSpanResult(span, state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, true)
		recordBuildMetrics(ctx, time.Since(state.startTime), state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, false)
		return state.result, nil
	}

	// Phase 3: Finalize
	state.graph.Freeze()
	state.result.Stats.DurationMilli = time.Since(state.startTime).Milliseconds()

	b.reportProgress(state, ProgressPhaseFinalizing, len(results), len(results))

	// Record success metrics
	setBuildSpanResult(span, state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, false)
	recordBuildMetrics(ctx, time.Since(state.startTime), state.result.Stats.NodesCreated, state.result.Stats.EdgesCreated, true)

	return state.result, nil
}

// collectPhase validates parse results and adds symbols as nodes.
func (b *Builder) collectPhase(ctx context.Context, state *buildState, results []*ast.ParseResult) error {
	for i, r := range results {
		// Check context
		if err := ctx.Err(); err != nil {
			return err
		}

		// Validate parse result
		if err := b.validateParseResult(r); err != nil {
			filePath := ""
			if r != nil {
				filePath = r.FilePath
			} else {
				filePath = fmt.Sprintf("result[%d]", i)
			}
			state.result.FileErrors = append(state.result.FileErrors, FileError{
				FilePath: filePath,
				Err:      err,
			})
			state.result.Stats.FilesFailed++
			continue
		}

		// Store imports for edge extraction
		state.fileImports[r.FilePath] = r.Imports

		// Add symbols as nodes
		for _, sym := range r.Symbols {
			if sym == nil {
				continue
			}

			// Add to graph
			_, err := state.graph.AddNode(sym)
			if err != nil {
				state.result.FileErrors = append(state.result.FileErrors, FileError{
					FilePath: r.FilePath,
					Err:      fmt.Errorf("add node %s: %w", sym.ID, err),
				})
				continue
			}

			// Index for resolution
			state.symbolsByID[sym.ID] = sym
			state.symbolsByName[sym.Name] = append(state.symbolsByName[sym.Name], sym)
			state.result.Stats.NodesCreated++

			// Recursively add children
			b.addChildSymbols(state, sym.Children)
		}

		state.result.Stats.FilesProcessed++
		b.reportProgress(state, ProgressPhaseCollecting, len(results), i+1)
	}

	return nil
}

// addChildSymbols recursively adds child symbols to the graph.
func (b *Builder) addChildSymbols(state *buildState, children []*ast.Symbol) {
	for _, child := range children {
		if child == nil {
			continue
		}

		_, err := state.graph.AddNode(child)
		if err != nil {
			// Log but don't fail - child nodes are optional
			continue
		}

		state.symbolsByID[child.ID] = child
		state.symbolsByName[child.Name] = append(state.symbolsByName[child.Name], child)
		state.result.Stats.NodesCreated++

		// Recurse
		b.addChildSymbols(state, child.Children)
	}
}

// extractEdgesPhase creates edges for symbol relationships.
func (b *Builder) extractEdgesPhase(ctx context.Context, state *buildState, results []*ast.ParseResult) error {
	for i, r := range results {
		// Check context
		if err := ctx.Err(); err != nil {
			return err
		}

		if r == nil {
			continue
		}

		// Extract edges for this file
		b.extractFileEdges(state, r)

		b.reportProgress(state, ProgressPhaseExtractingEdges, len(results), i+1)
	}

	return nil
}

// extractFileEdges extracts all edge types from a single file's parse result.
func (b *Builder) extractFileEdges(state *buildState, r *ast.ParseResult) {
	// Extract import edges
	b.extractImportEdges(state, r)

	// Extract edges from symbols
	for _, sym := range r.Symbols {
		if sym == nil {
			continue
		}

		b.extractSymbolEdges(state, sym, r)

		// Process children
		b.extractChildEdges(state, sym.Children, r)
	}
}

// extractChildEdges recursively extracts edges from child symbols.
func (b *Builder) extractChildEdges(state *buildState, children []*ast.Symbol, r *ast.ParseResult) {
	for _, child := range children {
		if child == nil {
			continue
		}
		b.extractSymbolEdges(state, child, r)
		b.extractChildEdges(state, child.Children, r)
	}
}

// extractImportEdges creates IMPORTS edges from import statements.
func (b *Builder) extractImportEdges(state *buildState, r *ast.ParseResult) {
	// Find file symbol or create virtual one
	fileSymbolID := fmt.Sprintf("%s:1:file", r.FilePath)

	for _, imp := range r.Imports {
		// Create placeholder for imported package
		pkgID := b.getOrCreatePlaceholder(state, imp.Path, imp.Path)

		// Create edge from file to import
		err := state.graph.AddEdge(fileSymbolID, pkgID, EdgeTypeImports, imp.Location)
		if err != nil {
			// File symbol might not exist - try to create from first symbol
			if len(r.Symbols) > 0 && r.Symbols[0] != nil {
				firstSymID := r.Symbols[0].ID
				err = state.graph.AddEdge(firstSymID, pkgID, EdgeTypeImports, imp.Location)
			}
			if err != nil {
				state.result.EdgeErrors = append(state.result.EdgeErrors, EdgeError{
					FromID:   fileSymbolID,
					ToID:     pkgID,
					EdgeType: EdgeTypeImports,
					Err:      err,
				})
				continue
			}
		}
		state.result.Stats.EdgesCreated++
	}
}

// extractSymbolEdges extracts edges for a single symbol.
func (b *Builder) extractSymbolEdges(state *buildState, sym *ast.Symbol, r *ast.ParseResult) {
	switch sym.Kind {
	case ast.SymbolKindMethod:
		// Method -> Receiver type (RECEIVES edge)
		if sym.Receiver != "" {
			b.extractReceiverEdge(state, sym)
		}
		fallthrough // Methods can also have calls, returns, etc.

	case ast.SymbolKindFunction:
		// Extract call edges would require parsing function body
		// For now, we extract what we can from metadata
		b.extractReturnTypeEdges(state, sym)
		b.extractParameterEdges(state, sym)

	case ast.SymbolKindStruct, ast.SymbolKindClass:
		// Extract implements edges if metadata available
		b.extractImplementsEdges(state, sym)
		// Extract embeds edges from fields
		b.extractEmbedsEdges(state, sym)

	case ast.SymbolKindInterface:
		// Interfaces define contracts - no outgoing edges typically
		break
	}
}

// extractReceiverEdge creates a RECEIVES edge from method to receiver type.
func (b *Builder) extractReceiverEdge(state *buildState, sym *ast.Symbol) {
	// Find receiver type symbol
	receiverName := strings.TrimPrefix(sym.Receiver, "*")
	targets := b.resolveSymbolByName(state, receiverName, sym.FilePath)

	if len(targets) == 0 {
		// Create placeholder
		targetID := b.getOrCreatePlaceholder(state, sym.Package, receiverName)
		targets = []string{targetID}
	}

	for _, targetID := range targets {
		err := state.graph.AddEdge(sym.ID, targetID, EdgeTypeReceives, sym.Location())
		if err != nil {
			state.result.EdgeErrors = append(state.result.EdgeErrors, EdgeError{
				FromID:   sym.ID,
				ToID:     targetID,
				EdgeType: EdgeTypeReceives,
				Err:      err,
			})
			continue
		}
		state.result.Stats.EdgesCreated++
	}

	if len(targets) > 1 {
		state.result.Stats.AmbiguousResolves++
	}
}

// extractReturnTypeEdges creates RETURNS edges from function to return types.
func (b *Builder) extractReturnTypeEdges(state *buildState, sym *ast.Symbol) {
	if sym.Metadata == nil || sym.Metadata.ReturnType == "" {
		return
	}

	// Parse return type (simplified - just use the type name)
	returnType := extractTypeName(sym.Metadata.ReturnType)
	if returnType == "" {
		return
	}

	targets := b.resolveSymbolByName(state, returnType, sym.FilePath)
	if len(targets) == 0 {
		targetID := b.getOrCreatePlaceholder(state, "", returnType)
		targets = []string{targetID}
	}

	for _, targetID := range targets {
		err := state.graph.AddEdge(sym.ID, targetID, EdgeTypeReturns, sym.Location())
		if err != nil {
			state.result.EdgeErrors = append(state.result.EdgeErrors, EdgeError{
				FromID:   sym.ID,
				ToID:     targetID,
				EdgeType: EdgeTypeReturns,
				Err:      err,
			})
			continue
		}
		state.result.Stats.EdgesCreated++
	}

	if len(targets) > 1 {
		state.result.Stats.AmbiguousResolves++
	}
}

// extractParameterEdges creates PARAMETERS edges from function to parameter types.
func (b *Builder) extractParameterEdges(state *buildState, sym *ast.Symbol) {
	// Extract parameter types from signature (simplified)
	// Full implementation would parse the signature properly
	if sym.Signature == "" {
		return
	}

	// For now, skip complex signature parsing
	// This would be enhanced in a future iteration
}

// extractImplementsEdges creates IMPLEMENTS edges from type to interfaces.
func (b *Builder) extractImplementsEdges(state *buildState, sym *ast.Symbol) {
	if sym.Metadata == nil || len(sym.Metadata.Implements) == 0 {
		return
	}

	for _, ifaceName := range sym.Metadata.Implements {
		targets := b.resolveSymbolByName(state, ifaceName, sym.FilePath)
		if len(targets) == 0 {
			targetID := b.getOrCreatePlaceholder(state, "", ifaceName)
			targets = []string{targetID}
		}

		for _, targetID := range targets {
			// Validate edge type - target should be interface
			if !b.validateEdgeType(state, sym.ID, targetID, EdgeTypeImplements) {
				continue
			}

			err := state.graph.AddEdge(sym.ID, targetID, EdgeTypeImplements, sym.Location())
			if err != nil {
				state.result.EdgeErrors = append(state.result.EdgeErrors, EdgeError{
					FromID:   sym.ID,
					ToID:     targetID,
					EdgeType: EdgeTypeImplements,
					Err:      err,
				})
				continue
			}
			state.result.Stats.EdgesCreated++
		}

		if len(targets) > 1 {
			state.result.Stats.AmbiguousResolves++
		}
	}
}

// extractEmbedsEdges creates EMBEDS edges from struct to embedded types.
func (b *Builder) extractEmbedsEdges(state *buildState, sym *ast.Symbol) {
	if sym.Metadata == nil || sym.Metadata.Extends == "" {
		return
	}

	// Extends represents embedding in Go context
	embeddedName := sym.Metadata.Extends
	targets := b.resolveSymbolByName(state, embeddedName, sym.FilePath)
	if len(targets) == 0 {
		targetID := b.getOrCreatePlaceholder(state, "", embeddedName)
		targets = []string{targetID}
	}

	for _, targetID := range targets {
		err := state.graph.AddEdge(sym.ID, targetID, EdgeTypeEmbeds, sym.Location())
		if err != nil {
			state.result.EdgeErrors = append(state.result.EdgeErrors, EdgeError{
				FromID:   sym.ID,
				ToID:     targetID,
				EdgeType: EdgeTypeEmbeds,
				Err:      err,
			})
			continue
		}
		state.result.Stats.EdgesCreated++
	}

	if len(targets) > 1 {
		state.result.Stats.AmbiguousResolves++
	}
}

// resolveSymbolByName finds symbols matching the given name.
// Prefers symbols in the same file, then same package.
func (b *Builder) resolveSymbolByName(state *buildState, name string, currentFile string) []string {
	candidates := state.symbolsByName[name]
	if len(candidates) == 0 {
		return nil
	}

	// Prefer same file
	var sameFile []string
	var samePackage []string
	var other []string

	for _, sym := range candidates {
		if sym.FilePath == currentFile {
			sameFile = append(sameFile, sym.ID)
		} else if b.samePackage(sym.FilePath, currentFile) {
			samePackage = append(samePackage, sym.ID)
		} else {
			other = append(other, sym.ID)
		}
	}

	// Return in priority order
	if len(sameFile) > 0 {
		return sameFile
	}
	if len(samePackage) > 0 {
		return samePackage
	}
	return other
}

// samePackage checks if two files are in the same package.
// This is a simple heuristic based on directory.
func (b *Builder) samePackage(file1, file2 string) bool {
	dir1 := extractDir(file1)
	dir2 := extractDir(file2)
	return dir1 == dir2
}

// extractDir extracts the directory from a file path.
func extractDir(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash < 0 {
		return ""
	}
	return path[:lastSlash]
}

// extractTypeName extracts a simple type name from a type expression.
// For example: "*User" -> "User", "[]string" -> "string", "map[string]User" -> "User"
func extractTypeName(typeExpr string) string {
	// Remove pointer prefix
	typeExpr = strings.TrimPrefix(typeExpr, "*")

	// Remove slice prefix
	typeExpr = strings.TrimPrefix(typeExpr, "[]")

	// Handle map - extract value type
	if strings.HasPrefix(typeExpr, "map[") {
		closeBracket := strings.Index(typeExpr, "]")
		if closeBracket > 0 && closeBracket < len(typeExpr)-1 {
			typeExpr = typeExpr[closeBracket+1:]
		}
	}

	// Remove channel prefix
	typeExpr = strings.TrimPrefix(typeExpr, "chan ")
	typeExpr = strings.TrimPrefix(typeExpr, "<-chan ")
	typeExpr = strings.TrimPrefix(typeExpr, "chan<- ")

	// Remove any remaining pointer
	typeExpr = strings.TrimPrefix(typeExpr, "*")

	// Extract just the type name (before any generic brackets)
	bracketIdx := strings.Index(typeExpr, "[")
	if bracketIdx > 0 {
		typeExpr = typeExpr[:bracketIdx]
	}

	// Skip built-in types
	builtins := map[string]bool{
		"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true, "complex64": true, "complex128": true,
		"bool": true, "byte": true, "rune": true, "error": true, "any": true,
	}

	if builtins[typeExpr] {
		return ""
	}

	return typeExpr
}

// getOrCreatePlaceholder returns an existing placeholder or creates a new one.
func (b *Builder) getOrCreatePlaceholder(state *buildState, pkg, name string) string {
	var id string
	if pkg != "" {
		id = fmt.Sprintf("external:%s:%s", pkg, name)
	} else {
		id = fmt.Sprintf("external::%s", name)
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if node, exists := state.placeholders[id]; exists {
		return node.ID
	}

	// Create placeholder symbol
	placeholder := &ast.Symbol{
		ID:       id,
		Name:     name,
		Kind:     ast.SymbolKindExternal,
		Package:  pkg,
		Language: "external",
	}

	node, err := state.graph.AddNode(placeholder)
	if err != nil {
		// Node might already exist (race condition) - just return the ID
		return id
	}

	state.placeholders[id] = node
	state.result.Stats.PlaceholderNodes++
	return id
}

// validateEdgeType checks if an edge type is valid for the given nodes.
func (b *Builder) validateEdgeType(state *buildState, fromID, toID string, edgeType EdgeType) bool {
	fromSym := state.symbolsByID[fromID]
	toSym := state.symbolsByID[toID]

	// If we don't have symbol info, allow the edge
	if fromSym == nil || toSym == nil {
		return true
	}

	switch edgeType {
	case EdgeTypeCalls:
		return isCallable(fromSym.Kind) && isCallable(toSym.Kind)
	case EdgeTypeImplements:
		return toSym.Kind == ast.SymbolKindInterface
	case EdgeTypeEmbeds:
		return fromSym.Kind == ast.SymbolKindStruct || fromSym.Kind == ast.SymbolKindClass
	default:
		return true
	}
}

// isCallable returns true if the symbol kind can make calls.
func isCallable(kind ast.SymbolKind) bool {
	return kind == ast.SymbolKindFunction ||
		kind == ast.SymbolKindMethod ||
		kind == ast.SymbolKindExternal
}

// validateParseResult checks if a ParseResult is valid for building.
// Note: Nil symbols are allowed and will be skipped during processing.
func (b *Builder) validateParseResult(r *ast.ParseResult) error {
	if r == nil {
		return fmt.Errorf("nil ParseResult")
	}

	if r.FilePath == "" {
		return fmt.Errorf("empty FilePath")
	}

	// Check for path traversal
	if strings.Contains(r.FilePath, "..") {
		return fmt.Errorf("FilePath contains path traversal")
	}

	// Validate non-nil symbols only
	// Nil symbols are skipped, not treated as errors
	for i, sym := range r.Symbols {
		if sym == nil {
			continue // Skip nil symbols
		}
		if err := sym.Validate(); err != nil {
			return fmt.Errorf("symbol[%d] (%s): %w", i, sym.Name, err)
		}
	}

	return nil
}

// reportProgress calls the progress callback if configured.
func (b *Builder) reportProgress(state *buildState, phase ProgressPhase, total, processed int) {
	if b.options.ProgressCallback == nil {
		return
	}

	b.options.ProgressCallback(BuildProgress{
		Phase:          phase,
		FilesTotal:     total,
		FilesProcessed: processed,
		NodesCreated:   state.result.Stats.NodesCreated,
		EdgesCreated:   state.result.Stats.EdgesCreated,
	})
}
