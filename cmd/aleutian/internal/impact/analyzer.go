// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package impact

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// Analyzer performs impact analysis on code changes.
//
// # Description
//
// Analyzer combines change detection, symbol mapping, blast radius
// calculation, and risk assessment into a single analysis pipeline.
//
// # Thread Safety
//
// Analyzer is safe for concurrent use.
type Analyzer struct {
	index    *initializer.MemoryIndex
	git      *GitClient
	assessor *RiskAssessor
}

// NewAnalyzer creates a new Analyzer.
//
// # Inputs
//
//   - index: The code index. Must not be nil.
//   - workDir: Working directory for git operations.
//
// # Outputs
//
//   - *Analyzer: The analyzer instance.
func NewAnalyzer(index *initializer.MemoryIndex, workDir string) *Analyzer {
	return &Analyzer{
		index:    index,
		git:      NewGitClient(workDir),
		assessor: NewRiskAssessor(),
	}
}

// Analyze performs impact analysis based on the configuration.
//
// # Description
//
// Performs the following steps:
// 1. Detect changed files (via git or explicit list)
// 2. Map changed files to symbols
// 3. Compute blast radius using BFS
// 4. Calculate risk level
// 5. Find affected tests
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - cfg: Analysis configuration.
//
// # Outputs
//
//   - *Result: The impact analysis result.
//   - error: Non-nil if analysis fails.
//
// # Thread Safety
//
// Safe for concurrent use.
func (a *Analyzer) Analyze(ctx context.Context, cfg Config) (*Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	start := time.Now()
	result := NewResult()

	// Step 1: Get changed files
	changedFiles, err := a.getChangedFiles(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	if len(changedFiles) == 0 {
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Check file limit
	if len(changedFiles) > cfg.MaxFiles {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Too many changed files (%d), limiting to %d", len(changedFiles), cfg.MaxFiles))
		changedFiles = changedFiles[:cfg.MaxFiles]
		result.Truncated = true
	}

	result.ChangedFiles = changedFiles

	// Step 2: Map to symbols
	changedSymbols := a.mapToSymbols(changedFiles, cfg)
	result.ChangedSymbols = changedSymbols

	if len(changedSymbols) == 0 {
		result.Warnings = append(result.Warnings, "No symbols found in changed files")
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Step 3: Compute blast radius
	affectedSymbols := a.computeBlastRadius(ctx, changedSymbols, cfg)

	// Check affected limit
	if len(affectedSymbols) > cfg.MaxAffected {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Blast radius truncated from %d to %d symbols", len(affectedSymbols), cfg.MaxAffected))
		affectedSymbols = affectedSymbols[:cfg.MaxAffected]
		result.Truncated = true
	}

	result.AffectedSymbols = affectedSymbols

	// Count direct vs transitive
	for _, sym := range affectedSymbols {
		if sym.Depth == 1 {
			result.DirectCount++
		} else {
			result.TransitiveCount++
		}
	}
	result.TotalAffected = len(affectedSymbols)

	// Step 4: Get affected packages and tests
	result.AffectedPackages = GetAffectedPackages(affectedSymbols)
	result.AffectedTests = GetAffectedTests(affectedSymbols, a.index)

	// Step 5: Calculate risk
	result.RiskLevel, result.RiskFactors = a.assessor.CalculateRisk(
		changedSymbols,
		affectedSymbols,
		result.AffectedTests,
		result.AffectedPackages,
	)

	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// getChangedFiles retrieves changed files based on configuration.
func (a *Analyzer) getChangedFiles(ctx context.Context, cfg Config) ([]ChangedFile, error) {
	// Check if we need git
	if cfg.Mode != ChangeModeFiles {
		if !a.git.IsGitRepo() {
			return nil, fmt.Errorf("not a git repository; use --files to specify files manually")
		}
	}

	return a.git.GetChangedFiles(ctx, cfg)
}

// mapToSymbols maps changed files to their symbols.
func (a *Analyzer) mapToSymbols(files []ChangedFile, cfg Config) []ChangedSymbol {
	var result []ChangedSymbol

	for _, f := range files {
		// Skip unsupported files
		if !isSupportedFile(f.Path) {
			continue
		}

		// Skip test files if not included
		if !cfg.IncludeTests && isTestFile(f.Path) {
			continue
		}

		// Skip excluded patterns
		if a.matchesExclude(f.Path, cfg.ExcludePatterns) {
			continue
		}

		// Get symbols in this file
		symbols := a.index.GetByFile(f.Path)

		// For deleted files, try to find symbols from the old path
		if len(symbols) == 0 && f.ChangeType == ChangeDeleted {
			// Deleted files won't have symbols in current index
			// We still report the file change but can't map to symbols
			continue
		}

		for _, sym := range symbols {
			result = append(result, ChangedSymbol{
				Symbol:     *sym,
				ChangeType: f.ChangeType,
				FilePath:   f.Path,
			})
		}
	}

	return result
}

// matchesExclude checks if a path matches any exclude pattern.
func (a *Analyzer) matchesExclude(path string, patterns []string) bool {
	for _, pattern := range patterns {
		// Simple prefix/suffix matching (not full glob for now)
		if matchPattern(pattern, path) {
			return true
		}
	}
	return false
}

// matchPattern performs simple pattern matching.
func matchPattern(pattern, path string) bool {
	// Handle ** patterns
	if len(pattern) > 2 && pattern[:2] == "**" {
		suffix := pattern[2:]
		if len(suffix) > 0 && suffix[0] == '/' {
			suffix = suffix[1:]
		}
		return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
	}

	// Handle * prefix
	if len(pattern) > 0 && pattern[0] == '*' {
		return len(path) >= len(pattern)-1 && path[len(path)-len(pattern)+1:] == pattern[1:]
	}

	// Exact match
	return path == pattern
}

// computeBlastRadius computes all symbols affected by the changes using BFS.
func (a *Analyzer) computeBlastRadius(ctx context.Context, changed []ChangedSymbol, cfg Config) []AffectedSymbol {
	// Use single BFS from all changed symbols (batch approach)
	type queueItem struct {
		symbolID string
		depth    int
		sourceID string
	}

	visited := make(map[string]bool)
	var result []AffectedSymbol

	// Seed queue with all changed symbols
	queue := make([]queueItem, 0, len(changed)*10)
	for _, cs := range changed {
		visited[cs.Symbol.ID] = true
		queue = append(queue, queueItem{
			symbolID: cs.Symbol.ID,
			depth:    0,
			sourceID: cs.Symbol.ID,
		})
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}

	// BFS traversal
	for len(queue) > 0 {
		// Check cancellation periodically
		select {
		case <-ctx.Done():
			return result
		default:
		}

		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		// Get callers (symbols that call this symbol)
		callers := a.index.GetCallers(item.symbolID, 0)

		for _, edge := range callers {
			if visited[edge.FromID] {
				continue
			}
			visited[edge.FromID] = true

			callerSym := a.index.GetByID(edge.FromID)
			if callerSym == nil {
				continue
			}

			// Skip test files if not included
			if !cfg.IncludeTests && isTestFile(callerSym.FilePath) {
				continue
			}

			depth := item.depth + 1
			result = append(result, AffectedSymbol{
				Symbol:   *callerSym,
				Depth:    depth,
				SourceID: item.sourceID,
			})

			// Continue BFS
			if depth < maxDepth {
				queue = append(queue, queueItem{
					symbolID: callerSym.ID,
					depth:    depth,
					sourceID: item.sourceID,
				})
			}
		}
	}

	return result
}
