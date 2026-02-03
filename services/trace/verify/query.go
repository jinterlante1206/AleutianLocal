// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"context"
	"errors"
	"fmt"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

// ErrNilGraph is returned when a nil graph is passed to NewVerifiedQuery.
var ErrNilGraph = errors.New("graph must not be nil")

// ErrNilManifest is returned when a nil manifest is passed to NewVerifiedQuery.
var ErrNilManifest = errors.New("manifest must not be nil")

// VerifiedQuery wraps a Graph with automatic pre-query verification.
//
// Description:
//
//	VerifiedQuery provides the same query interface as Graph, but
//	verifies that source files haven't changed before returning results.
//	This ensures queries never return stale data.
//
// Thread Safety:
//
//	VerifiedQuery is safe for concurrent use.
type VerifiedQuery struct {
	g        *graph.Graph
	m        *manifest.Manifest
	verifier *Verifier
}

// NewVerifiedQuery creates a new VerifiedQuery wrapper.
//
// Description:
//
//	Wraps a graph and manifest with automatic verification.
//	All query methods will verify relevant files before execution.
//
// Inputs:
//
//	g - The graph to query. Must not be nil.
//	m - The manifest for verification. Must not be nil.
//	v - The verifier. If nil, uses a default verifier.
//
// Outputs:
//
//	*VerifiedQuery - The wrapped query interface. Never nil on success.
//	error - ErrNilGraph or ErrNilManifest if inputs are invalid.
//
// Example:
//
//	vq, err := NewVerifiedQuery(g, m, nil)
//	if err != nil {
//	    return fmt.Errorf("creating verified query: %w", err)
//	}
//	result, err := vq.FindCallersByID(ctx, "main.go:main")
//
// Limitations:
//
//	Does not clone the graph or manifest. Caller must ensure
//	these are not modified during the lifetime of VerifiedQuery.
//
// Assumptions:
//
//	Graph and manifest are consistent (manifest was built from graph's project).
//
// Thread Safety:
//
//	The returned VerifiedQuery is safe for concurrent use.
func NewVerifiedQuery(g *graph.Graph, m *manifest.Manifest, v *Verifier) (*VerifiedQuery, error) {
	if g == nil {
		return nil, ErrNilGraph
	}
	if m == nil {
		return nil, ErrNilManifest
	}
	if v == nil {
		v = NewVerifier()
	}
	return &VerifiedQuery{
		g:        g,
		m:        m,
		verifier: v,
	}, nil
}

// Graph returns the underlying graph.
func (vq *VerifiedQuery) Graph() *graph.Graph {
	return vq.g
}

// Manifest returns the underlying manifest.
func (vq *VerifiedQuery) Manifest() *manifest.Manifest {
	return vq.m
}

// Verifier returns the underlying verifier.
func (vq *VerifiedQuery) Verifier() *Verifier {
	return vq.verifier
}

// verifyFileForSymbol verifies the file containing the given symbol ID.
//
// Description:
//
//	Looks up the symbol in the graph and verifies its source file.
//	If the symbol is not found, returns nil (lets the query handle missing symbols).
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	symbolID - ID of the symbol whose file should be verified.
//
// Outputs:
//
//	error - ErrStaleData if file changed, nil if fresh or symbol not found.
//
// Assumptions:
//
//	Graph and manifest are consistent.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (vq *VerifiedQuery) verifyFileForSymbol(ctx context.Context, symbolID string) error {
	node, ok := vq.g.GetNode(symbolID)
	if !ok {
		return nil // Node not found, let query handle this
	}

	if node.Symbol == nil || node.Symbol.FilePath == "" {
		return nil // No file to verify
	}

	return vq.verifyFile(ctx, node.Symbol.FilePath)
}

// verifyFile verifies a single file against its manifest entry.
//
// Description:
//
//	Checks if a file has changed since the manifest was created.
//	Uses mtime-first optimization with hash fallback.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	filePath - Relative path to the file to verify.
//
// Outputs:
//
//	error - ErrStaleData if file changed, nil if fresh or not in manifest.
//
// Assumptions:
//
//	filePath is relative to the graph's project root.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (vq *VerifiedQuery) verifyFile(ctx context.Context, filePath string) error {
	entry, ok := vq.m.Files[filePath]
	if !ok {
		return nil // File not in manifest, skip
	}

	result, err := vq.verifier.FastVerify(ctx, vq.g.ProjectRoot, filePath, entry)
	if err != nil {
		return fmt.Errorf("verifying file %s: %w", filePath, err)
	}

	if result.HasChanges() {
		return &ErrStaleData{
			StaleFiles:   result.StaleFiles,
			DeletedFiles: result.DeletedFiles,
		}
	}

	return nil
}

// verifyFiles verifies multiple files against their manifest entries.
//
// Description:
//
//	Checks if any of the specified files have changed. Uses parallel
//	verification for performance. Only verifies files that exist in
//	the manifest.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	filePaths - Relative paths to files to verify.
//
// Outputs:
//
//	error - ErrStaleData if any file changed, nil if all fresh.
//
// Assumptions:
//
//	All filePaths are relative to the graph's project root.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (vq *VerifiedQuery) verifyFiles(ctx context.Context, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	// Build entries map for files to verify
	entries := make(map[string]manifest.FileEntry)
	for _, path := range filePaths {
		if entry, ok := vq.m.Files[path]; ok {
			entries[path] = entry
		}
	}

	if len(entries) == 0 {
		return nil
	}

	result, err := vq.verifier.VerifyFiles(ctx, vq.g.ProjectRoot, entries)
	if err != nil {
		return fmt.Errorf("verifying %d files: %w", len(entries), err)
	}

	if result.HasChanges() {
		return &ErrStaleData{
			StaleFiles:   result.StaleFiles,
			DeletedFiles: result.DeletedFiles,
		}
	}

	return nil
}

// FindCallersByID returns all symbols that call the given function/method.
//
// Description:
//
//	Verifies the target symbol's file before finding callers.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	symbolID - ID of the function/method to find callers for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	*graph.QueryResult - Symbols that call the target.
//	error - ErrStaleData if file changed, or other error.
//
// Limitations:
//
//	Only verifies the target symbol's file, not caller files.
//	Callers from stale files may still be returned.
//
// Assumptions:
//
//	symbolID format is "filepath:name" or similar graph-specific format.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (vq *VerifiedQuery) FindCallersByID(ctx context.Context, symbolID string, opts ...graph.QueryOption) (*graph.QueryResult, error) {
	if err := vq.verifyFileForSymbol(ctx, symbolID); err != nil {
		return nil, err
	}
	return vq.g.FindCallersByID(ctx, symbolID, opts...)
}

// FindCalleesByID returns all symbols called by the given function/method.
//
// Description:
//
//	Verifies the target symbol's file before finding callees.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - ID of the function/method to find callees for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	*graph.QueryResult - Symbols called by the target.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindCalleesByID(ctx context.Context, symbolID string, opts ...graph.QueryOption) (*graph.QueryResult, error) {
	if err := vq.verifyFileForSymbol(ctx, symbolID); err != nil {
		return nil, err
	}
	return vq.g.FindCalleesByID(ctx, symbolID, opts...)
}

// FindImplementationsByID returns all implementations of the given interface.
//
// Description:
//
//	Verifies the interface's file before finding implementations.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	interfaceID - ID of the interface to find implementations for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	*graph.QueryResult - Types that implement the interface.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindImplementationsByID(ctx context.Context, interfaceID string, opts ...graph.QueryOption) (*graph.QueryResult, error) {
	if err := vq.verifyFileForSymbol(ctx, interfaceID); err != nil {
		return nil, err
	}
	return vq.g.FindImplementationsByID(ctx, interfaceID, opts...)
}

// FindReferencesByID returns all locations where the given symbol is used.
//
// Description:
//
//	Verifies the symbol's file before finding references.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - ID of the symbol to find references for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	[]ast.Location - Locations where the symbol is referenced.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindReferencesByID(ctx context.Context, symbolID string, opts ...graph.QueryOption) ([]ast.Location, error) {
	if err := vq.verifyFileForSymbol(ctx, symbolID); err != nil {
		return nil, err
	}
	return vq.g.FindReferencesByID(ctx, symbolID, opts...)
}

// FindCallersByName returns callers for all symbols with the given name.
//
// Description:
//
//	Verifies all files containing symbols with the given name.
//	Returns ErrStaleData if any relevant file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	name - Symbol name to find callers for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	map[string]*graph.QueryResult - Map of symbolID to callers.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindCallersByName(ctx context.Context, name string, opts ...graph.QueryOption) (map[string]*graph.QueryResult, error) {
	// Find all symbols with this name and verify their files
	filePaths := vq.collectFilesForName(name)
	if err := vq.verifyFiles(ctx, filePaths); err != nil {
		return nil, err
	}
	return vq.g.FindCallersByName(ctx, name, opts...)
}

// FindCalleesByName returns callees for all symbols with the given name.
//
// Description:
//
//	Verifies all files containing symbols with the given name.
//	Returns ErrStaleData if any relevant file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	name - Symbol name to find callees for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	map[string]*graph.QueryResult - Map of symbolID to callees.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindCalleesByName(ctx context.Context, name string, opts ...graph.QueryOption) (map[string]*graph.QueryResult, error) {
	filePaths := vq.collectFilesForName(name)
	if err := vq.verifyFiles(ctx, filePaths); err != nil {
		return nil, err
	}
	return vq.g.FindCalleesByName(ctx, name, opts...)
}

// FindImplementationsByName returns implementations for all interfaces with the given name.
//
// Description:
//
//	Verifies all files containing interfaces with the given name.
//	Returns ErrStaleData if any relevant file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	name - Interface name to find implementations for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	map[string]*graph.QueryResult - Map of interfaceID to implementations.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) FindImplementationsByName(ctx context.Context, name string, opts ...graph.QueryOption) (map[string]*graph.QueryResult, error) {
	filePaths := vq.collectFilesForName(name)
	if err := vq.verifyFiles(ctx, filePaths); err != nil {
		return nil, err
	}
	return vq.g.FindImplementationsByName(ctx, name, opts...)
}

// FindImporters returns all files that import the given package.
//
// Description:
//
//	Verifies the manifest before finding importers.
//	This is a lightweight verification since we're looking for imports.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	packagePath - Package path to find importers for.
//	opts - Query options (Limit, Timeout).
//
// Outputs:
//
//	[]string - File paths that import the package.
//	error - ErrStaleData if verification fails, or other error.
func (vq *VerifiedQuery) FindImporters(ctx context.Context, packagePath string, opts ...graph.QueryOption) ([]string, error) {
	// For importers, verify the entire manifest
	result, err := vq.verifier.VerifyManifest(ctx, vq.g.ProjectRoot, vq.m)
	if err != nil {
		return nil, err
	}
	if result.HasChanges() {
		return nil, &ErrStaleData{
			StaleFiles:   result.StaleFiles,
			DeletedFiles: result.DeletedFiles,
		}
	}
	return vq.g.FindImporters(ctx, packagePath, opts...)
}

// GetCallGraph returns the call graph rooted at the given symbol.
//
// Description:
//
//	Verifies the root symbol's file before traversing.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - ID of the symbol to start traversal from.
//	opts - Query options (MaxDepth, Limit, Timeout).
//
// Outputs:
//
//	*graph.TraversalResult - The call graph.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) GetCallGraph(ctx context.Context, symbolID string, opts ...graph.QueryOption) (*graph.TraversalResult, error) {
	if err := vq.verifyFileForSymbol(ctx, symbolID); err != nil {
		return nil, err
	}
	return vq.g.GetCallGraph(ctx, symbolID, opts...)
}

// GetReverseCallGraph returns the reverse call graph rooted at the given symbol.
//
// Description:
//
//	Verifies the root symbol's file before traversing.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - ID of the symbol to start traversal from.
//	opts - Query options (MaxDepth, Limit, Timeout).
//
// Outputs:
//
//	*graph.TraversalResult - The reverse call graph.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) GetReverseCallGraph(ctx context.Context, symbolID string, opts ...graph.QueryOption) (*graph.TraversalResult, error) {
	if err := vq.verifyFileForSymbol(ctx, symbolID); err != nil {
		return nil, err
	}
	return vq.g.GetReverseCallGraph(ctx, symbolID, opts...)
}

// GetDependencyTree returns the dependency tree for the given file.
//
// Description:
//
//	Verifies the file before analyzing dependencies.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - Path of the file to analyze.
//	opts - Query options (MaxDepth, Limit, Timeout).
//
// Outputs:
//
//	*graph.TraversalResult - The dependency tree.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) GetDependencyTree(ctx context.Context, filePath string, opts ...graph.QueryOption) (*graph.TraversalResult, error) {
	if err := vq.verifyFile(ctx, filePath); err != nil {
		return nil, err
	}
	return vq.g.GetDependencyTree(ctx, filePath, opts...)
}

// GetTypeHierarchy returns the type hierarchy for the given type.
//
// Description:
//
//	Verifies the type's file before analyzing hierarchy.
//	Returns ErrStaleData if the source file has changed.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	typeID - ID of the type to analyze.
//	opts - Query options (MaxDepth, Limit, Timeout).
//
// Outputs:
//
//	*graph.TraversalResult - The type hierarchy.
//	error - ErrStaleData if file changed, or other error.
func (vq *VerifiedQuery) GetTypeHierarchy(ctx context.Context, typeID string, opts ...graph.QueryOption) (*graph.TraversalResult, error) {
	if err := vq.verifyFileForSymbol(ctx, typeID); err != nil {
		return nil, err
	}
	return vq.g.GetTypeHierarchy(ctx, typeID, opts...)
}

// collectFilesForName collects all file paths containing symbols with the given name.
//
// Description:
//
//	Iterates through all nodes in the graph to find symbols matching
//	the given name, then returns the unique set of file paths.
//
// Inputs:
//
//	name - Symbol name to search for.
//
// Outputs:
//
//	[]string - Unique file paths containing matching symbols. May be empty.
//
// Limitations:
//
//	Iterates through all nodes in the graph, which may be slow for
//	large graphs. Consider adding a name index if this becomes a bottleneck.
//
// Assumptions:
//
//	Symbol names are case-sensitive.
//
// Thread Safety:
//
//	Safe for concurrent use (graph iteration is read-only).
func (vq *VerifiedQuery) collectFilesForName(name string) []string {
	seen := make(map[string]bool)
	var filePaths []string

	// Iterate through nodes to find matching names
	for _, node := range vq.g.Nodes() {
		if node.Symbol != nil && node.Symbol.Name == name && node.Symbol.FilePath != "" {
			if !seen[node.Symbol.FilePath] {
				seen[node.Symbol.FilePath] = true
				filePaths = append(filePaths, node.Symbol.FilePath)
			}
		}
	}

	return filePaths
}

// VerifyAll verifies the entire manifest.
//
// Description:
//
//	Performs a full verification of all files in the manifest.
//	Use this when you need to ensure the entire graph is fresh.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	*VerifyResult - The verification result.
//	error - Non-nil if verification failed unexpectedly.
func (vq *VerifiedQuery) VerifyAll(ctx context.Context) (*VerifyResult, error) {
	return vq.verifier.VerifyManifest(ctx, vq.g.ProjectRoot, vq.m)
}
