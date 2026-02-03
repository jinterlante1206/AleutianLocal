// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import (
	"context"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// Enricher adds analysis dimensions to a blast radius result.
//
// # Description
//
// Enrichers are composable sub-analyzers that add specific dimensions to
// blast radius results. They run in parallel (grouped by priority) to
// minimize latency. Each enricher focuses on one aspect: security paths,
// code churn, ownership, etc.
//
// # Priority Groups
//
// Enrichers are grouped by priority (lower number = higher priority):
//   - Priority 1: Critical analysis (security, ownership) - must complete
//   - Priority 2: Secondary analysis (churn, change classification)
//   - Priority 3: Derived analysis (confidence) - depends on earlier results
//
// Within each priority group, enrichers run in parallel.
//
// # Implementation Requirements
//
//   - Must be idempotent (safe to retry)
//   - Must check context for cancellation frequently
//   - Should complete within 50ms for good UX
//   - Must be thread-safe for concurrent use
//   - May return partial results on timeout
//   - Must not panic (return errors instead)
//
// # Thread Safety
//
// Implementations must be safe for concurrent use. Multiple goroutines
// may call Enrich simultaneously with different targets.
type Enricher interface {
	// Name returns a unique identifier for logging and metrics.
	//
	// # Description
	//
	// The name should be a short, lowercase, underscore-separated string
	// that identifies this enricher (e.g., "security_path", "churn").
	//
	// # Outputs
	//
	//   - string: Unique identifier for this enricher.
	Name() string

	// Priority determines execution order (lower = earlier).
	//
	// # Description
	//
	// Enrichers with the same priority run in parallel using errgroup.
	// Priority groups execute sequentially.
	//
	// # Priority Guidelines
	//
	//   - 1: Critical analysis that other enrichers may depend on
	//   - 2: Independent secondary analysis
	//   - 3: Derived analysis that aggregates other results
	//
	// # Range
	//
	// Valid range: 1-10 (1 = first, 10 = last).
	//
	// # Outputs
	//
	//   - int: Priority level (1-10).
	Priority() int

	// Enrich adds analysis data to the result.
	//
	// # Description
	//
	// Performs the enricher's analysis and modifies the result in place.
	// The result is pre-populated with base BlastRadius data from CB-17.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout. Must be checked frequently.
	//     Implementations should check ctx.Err() at least every 10ms of work.
	//   - target: The symbol and context being analyzed.
	//   - result: The result to enrich. Modify appropriate fields in place.
	//
	// # Outputs
	//
	//   - error: Non-nil on failure. Partial enrichment is acceptable.
	//     Errors do not fail the overall analysis; they're logged and
	//     the enricher's contribution is marked as missing.
	//
	// # Behavior Requirements
	//
	//   - Must be idempotent (calling twice produces same result)
	//   - Must respect context cancellation (return ctx.Err())
	//   - Should complete within 50ms for good UX
	//   - May return partial results if timeout is imminent
	//   - Must not modify fields owned by other enrichers
	//
	// # Example
	//
	//   func (e *MyEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	//       // Check cancellation at start
	//       if ctx.Err() != nil {
	//           return ctx.Err()
	//       }
	//
	//       // Do analysis...
	//       data := e.analyze(target)
	//
	//       // Check cancellation after work
	//       if ctx.Err() != nil {
	//           return ctx.Err()
	//       }
	//
	//       // Enrich result
	//       result.MyData = data
	//       return nil
	//   }
	Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error
}

// EnrichmentTarget provides context for enrichment.
//
// # Description
//
// Contains all the context an enricher needs to analyze a symbol,
// including the symbol itself, the graph, the index, and the base
// blast radius result from CB-17.
//
// # Thread Safety
//
// Read-only after construction. Safe for concurrent access.
//
// # Fields
//
//   - SymbolID: The unique identifier of the target symbol.
//   - Symbol: The resolved AST symbol (nil if not found).
//   - Graph: The frozen code graph for querying relationships.
//   - Index: The symbol index for lookups.
//   - RepoPath: Absolute path to the repository root.
//   - BaseResult: The CB-17 blast radius result to enrich.
type EnrichmentTarget struct {
	// SymbolID is the unique identifier of the target symbol.
	// Format: "path/to/file.go:line:name"
	SymbolID string

	// Symbol is the resolved AST symbol. May be nil if the symbol
	// could not be found (e.g., it was deleted or is dynamically generated).
	Symbol *ast.Symbol

	// Graph is the frozen code graph. Must be read-only at this point.
	// Use graph.Query methods to find relationships.
	Graph *graph.Graph

	// Index is the symbol index for O(1) lookups by ID, name, file, or kind.
	Index *index.SymbolIndex

	// RepoPath is the absolute path to the repository root.
	// Used by enrichers that need to access files (e.g., CODEOWNERS, git).
	RepoPath string

	// BaseResult is the CB-17 blast radius result.
	// Enrichers use this to access callers, implementers, etc.
	BaseResult *BlastRadius
}

// EnricherResult captures the outcome of running an enricher.
//
// # Description
//
// Used by EnhancedAnalyzer to track which enrichers succeeded, failed,
// or were skipped. Enables partial results and debugging.
type EnricherResult struct {
	// Name is the enricher's unique identifier.
	Name string

	// Success indicates whether the enricher completed without error.
	Success bool

	// Error contains the error message if Success is false.
	// Empty string if no error.
	Error string

	// DurationMs is how long the enricher took in milliseconds.
	DurationMs int64

	// Skipped indicates the enricher was skipped (e.g., due to timeout).
	Skipped bool

	// SkipReason explains why the enricher was skipped.
	SkipReason string
}

// EnricherRegistry manages a collection of enrichers.
//
// # Description
//
// Provides a way to register, unregister, and retrieve enrichers.
// Enrichers are stored in priority order for efficient iteration.
//
// # Thread Safety
//
// Safe for concurrent use after construction. Modifications during
// analysis are not supported.
type EnricherRegistry struct {
	enrichers []Enricher
}

// NewEnricherRegistry creates a new empty registry.
func NewEnricherRegistry() *EnricherRegistry {
	return &EnricherRegistry{
		enrichers: make([]Enricher, 0, 8),
	}
}

// Register adds an enricher to the registry.
//
// # Description
//
// Adds the enricher to the registry. Enrichers are stored in registration
// order; they will be sorted by priority when retrieved.
//
// # Inputs
//
//   - enricher: The enricher to register. Must not be nil.
//
// # Panics
//
// Panics if enricher is nil.
func (r *EnricherRegistry) Register(enricher Enricher) {
	if enricher == nil {
		panic("enricher must not be nil")
	}
	r.enrichers = append(r.enrichers, enricher)
}

// All returns all registered enrichers.
//
// # Description
//
// Returns a copy of the enricher slice. The caller may modify the
// returned slice without affecting the registry.
//
// # Outputs
//
//   - []Enricher: Copy of all registered enrichers.
func (r *EnricherRegistry) All() []Enricher {
	result := make([]Enricher, len(r.enrichers))
	copy(result, r.enrichers)
	return result
}

// ByPriority groups enrichers by their priority level.
//
// # Description
//
// Returns a map of priority -> enrichers. Used by EnhancedAnalyzer
// to run enrichers in parallel within each priority group.
//
// # Outputs
//
//   - map[int][]Enricher: Enrichers grouped by priority.
func (r *EnricherRegistry) ByPriority() map[int][]Enricher {
	result := make(map[int][]Enricher)
	for _, e := range r.enrichers {
		p := e.Priority()
		result[p] = append(result[p], e)
	}
	return result
}

// SortedPriorities returns priority levels in ascending order.
//
// # Description
//
// Returns the unique priority levels in ascending order. Used by
// EnhancedAnalyzer to process priority groups sequentially.
//
// # Outputs
//
//   - []int: Priority levels sorted ascending.
func (r *EnricherRegistry) SortedPriorities() []int {
	seen := make(map[int]bool)
	for _, e := range r.enrichers {
		seen[e.Priority()] = true
	}

	priorities := make([]int, 0, len(seen))
	for p := range seen {
		priorities = append(priorities, p)
	}

	// Simple insertion sort (small N)
	for i := 1; i < len(priorities); i++ {
		key := priorities[i]
		j := i - 1
		for j >= 0 && priorities[j] > key {
			priorities[j+1] = priorities[j]
			j--
		}
		priorities[j+1] = key
	}

	return priorities
}
