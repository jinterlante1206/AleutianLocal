// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// Default configuration for entry point finding.
const (
	// DefaultEntryPointLimit is the default maximum number of entry points to return.
	DefaultEntryPointLimit = 100

	// contextCheckIntervalEntryPoints is how often to check context during iteration.
	contextCheckIntervalEntryPoints = 100
)

// EntryPointFinder discovers entry points in a codebase.
//
// Thread Safety:
//
//	EntryPointFinder is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type EntryPointFinder struct {
	graph    *graph.Graph
	index    *index.SymbolIndex
	registry *EntryPointRegistry
}

// NewEntryPointFinder creates a new EntryPointFinder.
//
// Description:
//
//	Creates a finder that can discover entry points using the provided
//	graph and symbol index. Uses the default pattern registry.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*EntryPointFinder - The configured finder.
//
// Example:
//
//	finder := NewEntryPointFinder(graph, index)
//	result, err := finder.FindEntryPoints(ctx, EntryPointAll, nil)
func NewEntryPointFinder(g *graph.Graph, idx *index.SymbolIndex) *EntryPointFinder {
	return &EntryPointFinder{
		graph:    g,
		index:    idx,
		registry: NewEntryPointRegistry(),
	}
}

// WithRegistry returns a new finder with a custom pattern registry.
func (f *EntryPointFinder) WithRegistry(registry *EntryPointRegistry) *EntryPointFinder {
	return &EntryPointFinder{
		graph:    f.graph,
		index:    f.index,
		registry: registry,
	}
}

// EntryPointOptions configures entry point finding.
type EntryPointOptions struct {
	// Type filters results to a specific entry point type.
	// Use EntryPointAll for all types.
	Type EntryPointType

	// Package filters results to a specific package.
	Package string

	// Language filters results to a specific language.
	Language string

	// Limit is the maximum number of results to return.
	Limit int

	// IncludeTests includes test entry points in results.
	IncludeTests bool
}

// DefaultEntryPointOptions returns sensible defaults.
func DefaultEntryPointOptions() EntryPointOptions {
	return EntryPointOptions{
		Type:         EntryPointAll,
		Limit:        DefaultEntryPointLimit,
		IncludeTests: false,
	}
}

// FindEntryPoints discovers entry points in the codebase.
//
// Description:
//
//	Scans all symbols in the index and matches them against registered
//	entry point patterns. Returns entry points sorted by file path and line.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	opts - Options for filtering and limiting results.
//
// Outputs:
//
//	*EntryPointResult - Discovered entry points.
//	error - Non-nil if the operation was canceled or failed.
//
// Errors:
//
//	ErrContextCanceled - Context was canceled.
//	ErrGraphNotReady - Graph is not frozen.
//
// Performance:
//
//	O(n) where n is the number of symbols. Target latency: < 100ms.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *EntryPointFinder) FindEntryPoints(ctx context.Context, opts EntryPointOptions) (*EntryPointResult, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	if f.graph != nil && !f.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	// Get all symbols from index
	stats := f.index.Stats()
	result := &EntryPointResult{
		EntryPoints: make([]EntryPoint, 0),
	}

	// Process each symbol kind that could be an entry point
	symbolKinds := []ast.SymbolKind{
		ast.SymbolKindFunction,
		ast.SymbolKindMethod,
		ast.SymbolKindClass,
		ast.SymbolKindStruct,
	}

	checkCounter := 0
	for _, kind := range symbolKinds {
		symbols := f.index.GetByKind(kind)
		for _, sym := range symbols {
			checkCounter++

			// Periodic context check
			if checkCounter%contextCheckIntervalEntryPoints == 0 {
				if err := ctx.Err(); err != nil {
					result.Truncated = true
					return result, nil
				}
			}

			// Apply filters
			if opts.Package != "" && sym.Package != opts.Package {
				continue
			}

			if opts.Language != "" && sym.Language != opts.Language {
				continue
			}

			// Check if symbol matches any entry point pattern
			entryPoint, matched := f.matchEntryPoint(sym, opts.Type)
			if !matched {
				continue
			}

			// Skip tests if not requested
			if entryPoint.Type == EntryPointTest && !opts.IncludeTests {
				continue
			}

			result.EntryPoints = append(result.EntryPoints, entryPoint)
			result.TotalFound++

			// Check limit
			if opts.Limit > 0 && len(result.EntryPoints) >= opts.Limit {
				result.Truncated = true
				break
			}
		}

		if result.Truncated {
			break
		}
	}

	// Sort by file path, then line number
	sort.Slice(result.EntryPoints, func(i, j int) bool {
		if result.EntryPoints[i].FilePath != result.EntryPoints[j].FilePath {
			return result.EntryPoints[i].FilePath < result.EntryPoints[j].FilePath
		}
		return result.EntryPoints[i].Line < result.EntryPoints[j].Line
	})

	// Update total count from stats if we didn't hit any matches
	if result.TotalFound == 0 {
		result.TotalFound = stats.TotalSymbols
	}

	return result, nil
}

// matchEntryPoint checks if a symbol matches any entry point pattern.
func (f *EntryPointFinder) matchEntryPoint(sym *ast.Symbol, filterType EntryPointType) (EntryPoint, bool) {
	patterns, ok := f.registry.GetPatterns(sym.Language)
	if !ok {
		return EntryPoint{}, false
	}

	// Check each entry point type
	entryPointTypes := []struct {
		epType   EntryPointType
		patterns []PatternMatcher
	}{
		{EntryPointMain, patterns.Main},
		{EntryPointHandler, patterns.Handler},
		{EntryPointCommand, patterns.Command},
		{EntryPointTest, patterns.Test},
		{EntryPointLambda, patterns.Lambda},
		{EntryPointGRPC, patterns.GRPC},
	}

	for _, ept := range entryPointTypes {
		// Skip if filtering by type and this isn't the right type
		if filterType != EntryPointAll && filterType != ept.epType {
			continue
		}

		for _, pattern := range ept.patterns {
			if pattern.Match(sym) {
				return EntryPoint{
					ID:         sym.ID,
					Name:       sym.Name,
					Type:       ept.epType,
					Framework:  pattern.Framework,
					FilePath:   sym.FilePath,
					Line:       sym.StartLine,
					Signature:  sym.Signature,
					DocComment: sym.DocComment,
					Package:    sym.Package,
				}, true
			}
		}
	}

	return EntryPoint{}, false
}

// FindMainEntryPoints finds all main/script entry points.
//
// Description:
//
//	Convenience method for finding main() functions and __main__ blocks.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	[]EntryPoint - Main entry points found.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) FindMainEntryPoints(ctx context.Context) ([]EntryPoint, error) {
	opts := DefaultEntryPointOptions()
	opts.Type = EntryPointMain

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.EntryPoints, nil
}

// FindHandlers finds all HTTP/REST handlers.
//
// Description:
//
//	Convenience method for finding HTTP handlers across frameworks.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	[]EntryPoint - Handler entry points found.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) FindHandlers(ctx context.Context) ([]EntryPoint, error) {
	opts := DefaultEntryPointOptions()
	opts.Type = EntryPointHandler
	opts.Limit = 1000 // Handlers can be numerous

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.EntryPoints, nil
}

// FindTests finds all test entry points.
//
// Description:
//
//	Convenience method for finding test functions.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	[]EntryPoint - Test entry points found.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) FindTests(ctx context.Context) ([]EntryPoint, error) {
	opts := DefaultEntryPointOptions()
	opts.Type = EntryPointTest
	opts.IncludeTests = true
	opts.Limit = 1000 // Tests can be numerous

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.EntryPoints, nil
}

// FindByFramework finds entry points for a specific framework.
//
// Description:
//
//	Finds all entry points associated with a framework (e.g., "gin", "fastapi").
//
// Inputs:
//
//	ctx - Context for cancellation.
//	framework - Framework name to filter by.
//
// Outputs:
//
//	[]EntryPoint - Entry points for the framework.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) FindByFramework(ctx context.Context, framework string) ([]EntryPoint, error) {
	opts := DefaultEntryPointOptions()
	opts.Type = EntryPointAll
	opts.IncludeTests = true
	opts.Limit = 0 // No limit

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Filter by framework
	filtered := make([]EntryPoint, 0)
	frameworkLower := strings.ToLower(framework)
	for _, ep := range result.EntryPoints {
		if strings.ToLower(ep.Framework) == frameworkLower {
			filtered = append(filtered, ep)
		}
	}

	return filtered, nil
}

// FindByPackage finds entry points in a specific package.
//
// Description:
//
//	Finds all entry points within a package/module.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	packagePath - Package path to filter by.
//
// Outputs:
//
//	[]EntryPoint - Entry points in the package.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) FindByPackage(ctx context.Context, packagePath string) ([]EntryPoint, error) {
	opts := DefaultEntryPointOptions()
	opts.Package = packagePath
	opts.IncludeTests = true
	opts.Limit = 0 // No limit

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.EntryPoints, nil
}

// DetectFrameworks identifies frameworks used in the codebase.
//
// Description:
//
//	Analyzes entry points to determine which frameworks are in use.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	map[string]int - Framework name to count of entry points.
//	error - Non-nil if the operation was canceled or failed.
func (f *EntryPointFinder) DetectFrameworks(ctx context.Context) (map[string]int, error) {
	opts := DefaultEntryPointOptions()
	opts.Type = EntryPointAll
	opts.IncludeTests = true
	opts.Limit = 0 // No limit

	result, err := f.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}

	frameworks := make(map[string]int)
	for _, ep := range result.EntryPoints {
		if ep.Framework != "" {
			frameworks[ep.Framework]++
		}
	}

	return frameworks, nil
}
