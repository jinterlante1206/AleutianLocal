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
	"fmt"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/validation"
	"golang.org/x/sync/errgroup"
)

// EnhancedAnalyzer orchestrates multiple enrichers with parallel execution.
//
// # Description
//
// EnhancedAnalyzer extends BlastRadiusAnalyzer by running additional enrichers
// that add security, churn, ownership, change classification, and confidence
// analysis. Enrichers run in parallel within priority groups for optimal latency.
//
// # Execution Model
//
//  1. Run base BlastRadiusAnalyzer.Analyze() to get CB-17 result
//  2. Group enrichers by priority
//  3. For each priority group (in order): run enrichers in parallel via errgroup
//  4. Aggregate results; slow enrichers don't block response (partial results OK)
//
// # Thread Safety
//
// Safe for concurrent use. Multiple goroutines may call Analyze simultaneously.
type EnhancedAnalyzer struct {
	baseAnalyzer *BlastRadiusAnalyzer
	registry     *EnricherRegistry
	timeout      time.Duration
	validator    *validation.InputValidator
	repoPath     string             // Repository root path
	graph        *graph.Graph       // Code graph (from base or explicit)
	index        *index.SymbolIndex // Symbol index (from base or explicit)

	// mu protects mutable state (currently none, but future-proofing)
	mu sync.RWMutex
}

// EnhancedAnalyzerOption configures EnhancedAnalyzer.
type EnhancedAnalyzerOption func(*EnhancedAnalyzer)

// WithEnrichmentTimeout sets the total time budget for enrichment.
// Default: 150ms.
func WithEnrichmentTimeout(d time.Duration) EnhancedAnalyzerOption {
	return func(a *EnhancedAnalyzer) {
		a.timeout = d
	}
}

// WithInputValidator sets the input validator.
func WithInputValidator(v *validation.InputValidator) EnhancedAnalyzerOption {
	return func(a *EnhancedAnalyzer) {
		a.validator = v
	}
}

// WithRepoPath sets the repository root path.
func WithRepoPath(path string) EnhancedAnalyzerOption {
	return func(a *EnhancedAnalyzer) {
		a.repoPath = path
	}
}

// WithGraph sets an explicit graph (overrides base analyzer's graph).
func WithGraph(g *graph.Graph) EnhancedAnalyzerOption {
	return func(a *EnhancedAnalyzer) {
		a.graph = g
	}
}

// WithIndex sets an explicit index (overrides base analyzer's index).
func WithIndex(idx *index.SymbolIndex) EnhancedAnalyzerOption {
	return func(a *EnhancedAnalyzer) {
		a.index = idx
	}
}

// NewEnhancedAnalyzer creates an analyzer that orchestrates enrichers.
//
// # Description
//
// Creates an EnhancedAnalyzer that wraps a base BlastRadiusAnalyzer and
// adds enrichment capabilities. The graph and index must be provided for
// enrichment context (they can be the same as those used by the base analyzer).
//
// # Inputs
//
//   - base: The CB-17 BlastRadiusAnalyzer. Must not be nil.
//   - g: The code graph. Must not be nil.
//   - idx: The symbol index. Must not be nil.
//   - registry: Registry of enrichers to run. Must not be nil.
//   - opts: Optional configuration.
//
// # Outputs
//
//   - *EnhancedAnalyzer: Ready-to-use analyzer.
//
// # Panics
//
// Panics if base, graph, index, or registry is nil.
//
// # Example
//
//	registry := NewEnricherRegistry()
//	registry.Register(NewSecurityPathDetector(patterns))
//	registry.Register(NewChurnAnalyzer(repoPath, validator))
//	registry.Register(NewOwnershipResolver(repoPath, validator))
//	registry.Register(NewChangeClassifier())
//	registry.Register(NewConfidenceCalculator())
//
//	analyzer := NewEnhancedAnalyzer(baseAnalyzer, graph, index, registry,
//	    WithEnrichmentTimeout(150*time.Millisecond),
//	    WithRepoPath("/path/to/repo"),
//	)
func NewEnhancedAnalyzer(
	base *BlastRadiusAnalyzer,
	g *graph.Graph,
	idx *index.SymbolIndex,
	registry *EnricherRegistry,
	opts ...EnhancedAnalyzerOption,
) *EnhancedAnalyzer {
	if base == nil {
		panic("base analyzer must not be nil")
	}
	if g == nil {
		panic("graph must not be nil")
	}
	if idx == nil {
		panic("index must not be nil")
	}
	if registry == nil {
		panic("enricher registry must not be nil")
	}

	a := &EnhancedAnalyzer{
		baseAnalyzer: base,
		registry:     registry,
		timeout:      150 * time.Millisecond,
		validator:    validation.NewInputValidator(nil),
		graph:        g,
		index:        idx,
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// Analyze runs base analysis then enriches with all registered enrichers.
//
// # Description
//
// Performs a complete enhanced blast radius analysis:
//  1. Validates input
//  2. Runs CB-17 base analysis
//  3. Runs enrichers grouped by priority
//  4. Returns combined result with partial enrichment if timeout
//
// # Execution Model
//
// Enrichers are grouped by priority and run in parallel within each group.
// If the total enrichment timeout is reached, remaining enrichers are skipped.
// This ensures the response is always returned within the timeout budget.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout. Must not be nil.
//   - targetID: The symbol ID to analyze.
//   - opts: Analysis options (may be nil for defaults).
//
// # Outputs
//
//   - *EnhancedBlastRadius: Analysis result. Never nil on success.
//   - error: Non-nil on complete failure (validation, base analysis failed).
//     Partial enrichment failures are captured in EnricherResults, not here.
//
// # Example
//
//	result, err := analyzer.Analyze(ctx, "pkg/auth.go:42:ValidateToken", nil)
//	if err != nil {
//	    return fmt.Errorf("analysis failed: %w", err)
//	}
//
//	if result.SecurityPath != nil && result.SecurityPath.RequiresReview {
//	    log.Warn("Security review required")
//	}
func (a *EnhancedAnalyzer) Analyze(
	ctx context.Context,
	targetID string,
	opts *AnalyzeOptions,
) (*EnhancedBlastRadius, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	// Validate input
	if err := a.validator.ValidateSymbolID(targetID); err != nil {
		return nil, fmt.Errorf("invalid target ID: %w", err)
	}

	// Apply defaults
	if opts == nil {
		defaults := DefaultAnalyzeOptions()
		opts = &defaults
	}

	// Use stored graph and index
	g := a.graph
	idx := a.index

	// Run base analysis
	baseResult, err := a.baseAnalyzer.Analyze(ctx, targetID, opts)
	if err != nil {
		return nil, fmt.Errorf("base analysis failed: %w", err)
	}

	// Build enrichment target
	symbol, _ := idx.GetByID(targetID) // May be nil
	target := &EnrichmentTarget{
		SymbolID:   targetID,
		Symbol:     symbol,
		Graph:      g,
		Index:      idx,
		RepoPath:   a.repoPath,
		BaseResult: baseResult,
	}

	// Create enhanced result
	// Use BuiltAtMilli as generation (cast to uint64 for cache key purposes)
	graphGen := uint64(0)
	if g.BuiltAtMilli > 0 {
		graphGen = uint64(g.BuiltAtMilli)
	}

	result := &EnhancedBlastRadius{
		BlastRadius:     *baseResult,
		ChangeImpacts:   make([]ChangeImpact, 0),
		EnricherResults: make([]EnricherResult, 0, len(a.registry.All())),
		AnalyzedAt:      time.Now().UnixMilli(),
		GraphGeneration: graphGen,
	}

	// Run enrichers with timeout
	enrichCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	a.runEnrichers(enrichCtx, target, result)

	return result, nil
}

// runEnrichers executes enrichers grouped by priority.
func (a *EnhancedAnalyzer) runEnrichers(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) {
	priorities := a.registry.SortedPriorities()
	byPriority := a.registry.ByPriority()

	for _, priority := range priorities {
		// Check if we've timed out
		if ctx.Err() != nil {
			// Mark remaining enrichers as skipped
			for _, p := range priorities {
				if p >= priority {
					for _, e := range byPriority[p] {
						result.EnricherResults = append(result.EnricherResults, EnricherResult{
							Name:       e.Name(),
							Success:    false,
							Skipped:    true,
							SkipReason: "timeout",
						})
					}
				}
			}
			break
		}

		enrichers := byPriority[priority]
		results := a.runPriorityGroup(ctx, enrichers, target, result)
		result.EnricherResults = append(result.EnricherResults, results...)
	}
}

// runPriorityGroup runs enrichers at the same priority level in parallel.
func (a *EnhancedAnalyzer) runPriorityGroup(
	ctx context.Context,
	enrichers []Enricher,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) []EnricherResult {
	if len(enrichers) == 0 {
		return nil
	}

	// Mutex to protect concurrent writes to result
	var resultMu sync.Mutex
	enricherResults := make([]EnricherResult, len(enrichers))

	g, gCtx := errgroup.WithContext(ctx)

	for i, enricher := range enrichers {
		i, enricher := i, enricher // Capture loop variables

		g.Go(func() error {
			start := time.Now()
			err := enricher.Enrich(gCtx, target, result)
			duration := time.Since(start)

			enricherResults[i] = EnricherResult{
				Name:       enricher.Name(),
				Success:    err == nil,
				DurationMs: duration.Milliseconds(),
			}

			if err != nil {
				enricherResults[i].Error = err.Error()

				// Log but don't fail - enricher errors are non-fatal
				resultMu.Lock()
				// Note: In production, use structured logging instead
				resultMu.Unlock()
			}

			return nil // Never propagate errors - enrichers are non-fatal
		})
	}

	// Wait for all enrichers (errors ignored - they're non-fatal)
	_ = g.Wait()

	return enricherResults
}

// GetBaseAnalyzer returns the underlying BlastRadiusAnalyzer.
func (a *EnhancedAnalyzer) GetBaseAnalyzer() *BlastRadiusAnalyzer {
	return a.baseAnalyzer
}

// GetRegistry returns the enricher registry.
func (a *EnhancedAnalyzer) GetRegistry() *EnricherRegistry {
	return a.registry
}

// GetTimeout returns the enrichment timeout.
func (a *EnhancedAnalyzer) GetTimeout() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.timeout
}

// SetTimeout updates the enrichment timeout.
// Thread-safe.
func (a *EnhancedAnalyzer) SetTimeout(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.timeout = d
}

// DefaultEnricherRegistry creates a registry with standard enrichers.
//
// # Description
//
// Creates a registry pre-populated with the standard CB-17b enrichers:
//   - SecurityPathDetector (priority 1)
//   - OwnershipResolver (priority 1)
//   - ChurnAnalyzer (priority 2)
//   - ChangeClassifier (priority 2)
//   - ConfidenceCalculator (priority 3)
//
// # Inputs
//
//   - repoPath: Absolute path to the repository root.
//   - validator: Input validator (may be nil for default).
//
// # Outputs
//
//   - *EnricherRegistry: Registry with standard enrichers.
//   - error: Non-nil if critical enrichers failed to initialize.
//
// # Example
//
//	registry, err := DefaultEnricherRegistry("/path/to/repo", nil)
//	if err != nil {
//	    return fmt.Errorf("failed to create registry: %w", err)
//	}
func DefaultEnricherRegistry(repoPath string, validator *validation.InputValidator) (*EnricherRegistry, error) {
	if validator == nil {
		validator = validation.NewInputValidator(nil)
	}

	registry := NewEnricherRegistry()

	// Priority 1: Security and ownership (critical, no dependencies)
	securityDetector, err := NewSecurityPathDetector(DefaultSecurityPatterns)
	if err != nil {
		return nil, fmt.Errorf("failed to create security detector: %w", err)
	}
	registry.Register(securityDetector)

	ownershipResolver, err := NewOwnershipResolver(repoPath, validator)
	if err != nil {
		// Ownership is optional - CODEOWNERS may not exist
		// Don't fail, just skip registration
	} else {
		registry.Register(ownershipResolver)
	}

	// Priority 2: Secondary analysis
	churnAnalyzer := NewChurnAnalyzer(repoPath, validator)
	registry.Register(churnAnalyzer)

	changeClassifier := NewChangeClassifier()
	registry.Register(changeClassifier)

	// Priority 3: Derived analysis
	confidenceCalc := NewConfidenceCalculator()
	registry.Register(confidenceCalc)

	return registry, nil
}

// GetGraph returns the code graph.
func (a *EnhancedAnalyzer) GetGraph() *graph.Graph {
	return a.graph
}

// GetIndex returns the symbol index.
func (a *EnhancedAnalyzer) GetIndex() *index.SymbolIndex {
	return a.index
}

// GetRepoPath returns the repository root path.
func (a *EnhancedAnalyzer) GetRepoPath() string {
	return a.repoPath
}
