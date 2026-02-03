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
	"path/filepath"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// BlastRadiusAnalyzer calculates the impact of changing code.
//
// # Description
//
// Analyzes a target symbol to determine what would be affected if it
// were modified. Provides risk assessment, caller analysis, and
// actionable recommendations.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type BlastRadiusAnalyzer struct {
	graph      *graph.Graph
	index      *index.SymbolIndex
	riskConfig RiskConfig
}

// NewBlastRadiusAnalyzer creates an analyzer with the given graph and index.
//
// # Description
//
// Creates an analyzer that will use the provided graph for relationship
// queries and the index for symbol lookups.
//
// # Inputs
//
//   - g: Code graph with call relationships.
//   - idx: Symbol index for lookups.
//   - riskConfig: Optional risk thresholds (nil uses defaults).
//
// # Outputs
//
//   - *BlastRadiusAnalyzer: Ready-to-use analyzer.
func NewBlastRadiusAnalyzer(g *graph.Graph, idx *index.SymbolIndex, riskConfig *RiskConfig) *BlastRadiusAnalyzer {
	config := DefaultRiskConfig()
	if riskConfig != nil {
		config = *riskConfig
	}

	return &BlastRadiusAnalyzer{
		graph:      g,
		index:      idx,
		riskConfig: config,
	}
}

// Analyze calculates the blast radius for a target symbol.
//
// # Description
//
// Performs comprehensive analysis of what would be affected if the target
// symbol were modified. Respects timeout and limits specified in options.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - targetID: Symbol ID to analyze (e.g., "pkg/auth.go:10:ValidateToken").
//   - opts: Analysis options (nil uses defaults).
//
// # Outputs
//
//   - *BlastRadius: Analysis results.
//   - error: Non-nil on failure.
//
// # Example
//
//	analyzer := analysis.NewBlastRadiusAnalyzer(graph, index, nil)
//	result, err := analyzer.Analyze(ctx, "pkg/auth.go:10:ValidateToken", nil)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Risk: %s\n", result.RiskLevel)
//	fmt.Printf("Direct callers: %d\n", len(result.DirectCallers))
func (a *BlastRadiusAnalyzer) Analyze(ctx context.Context, targetID string, opts *AnalyzeOptions) (*BlastRadius, error) {
	options := DefaultAnalyzeOptions()
	if opts != nil {
		options = *opts
	}

	// Create timeout context if specified
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	// Initialize result
	result := &BlastRadius{
		Target:          targetID,
		DirectCallers:   make([]Caller, 0),
		IndirectCallers: make([]Caller, 0),
		Implementers:    make([]Implementer, 0),
		SharedDeps:      make([]SharedDep, 0),
		FilesAffected:   make([]string, 0),
		TestFiles:       make([]string, 0),
	}

	// Find direct callers
	directCallers, truncated := a.findDirectCallers(ctx, targetID, options.MaxDirectCallers)
	result.DirectCallers = directCallers
	if truncated {
		result.Truncated = true
		result.TruncatedReason = fmt.Sprintf("direct callers exceeded limit (%d)", options.MaxDirectCallers)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		result.Truncated = true
		result.TruncatedReason = "timeout"
		a.finalizeResult(result, options)
		return result, nil
	default:
	}

	// Find indirect callers
	indirectCallers, truncated := a.findIndirectCallers(ctx, directCallers, options.MaxHops, options.MaxIndirectCallers)
	result.IndirectCallers = indirectCallers
	if truncated && !result.Truncated {
		result.Truncated = true
		result.TruncatedReason = fmt.Sprintf("indirect callers exceeded limit (%d)", options.MaxIndirectCallers)
	}

	// Find interface implementers (if target is interface or method)
	result.Implementers = a.findImplementers(ctx, targetID)

	// Find shared dependencies
	result.SharedDeps = a.findSharedDeps(ctx, targetID, directCallers)

	// Finalize result (risk level, files, tests, summary)
	a.finalizeResult(result, options)

	return result, nil
}

// findDirectCallers finds all functions that directly call the target.
func (a *BlastRadiusAnalyzer) findDirectCallers(ctx context.Context, targetID string, limit int) ([]Caller, bool) {
	callers := make([]Caller, 0)
	truncated := false

	// Query the graph for callers using FindCallersByID
	queryResult, err := a.graph.FindCallersByID(ctx, targetID, graph.WithLimit(limit))
	if err != nil {
		return callers, false
	}

	if queryResult.Truncated {
		truncated = true
	}

	for _, symbol := range queryResult.Symbols {
		if symbol == nil {
			continue
		}

		callers = append(callers, Caller{
			ID:       symbol.ID,
			Name:     symbol.Name,
			FilePath: symbol.FilePath,
			Line:     symbol.StartLine,
			Hops:     1,
		})
	}

	return callers, truncated
}

// findIndirectCallers finds callers of callers up to maxHops.
func (a *BlastRadiusAnalyzer) findIndirectCallers(ctx context.Context, directCallers []Caller, maxHops, limit int) ([]Caller, bool) {
	indirectCallers := make([]Caller, 0)
	seen := make(map[string]bool)
	truncated := false

	// Mark direct callers as seen
	for _, c := range directCallers {
		seen[c.ID] = true
	}

	// BFS through caller graph
	currentLevel := directCallers
	for hop := 2; hop <= maxHops; hop++ {
		select {
		case <-ctx.Done():
			return indirectCallers, true
		default:
		}

		nextLevel := make([]Caller, 0)

		for _, caller := range currentLevel {
			queryResult, err := a.graph.FindCallersByID(ctx, caller.ID)
			if err != nil {
				continue
			}

			for _, symbol := range queryResult.Symbols {
				if symbol == nil {
					continue
				}

				if seen[symbol.ID] {
					continue
				}
				seen[symbol.ID] = true

				if len(indirectCallers) >= limit {
					truncated = true
					break
				}

				newCaller := Caller{
					ID:       symbol.ID,
					Name:     symbol.Name,
					FilePath: symbol.FilePath,
					Line:     symbol.StartLine,
					Hops:     hop,
				}
				indirectCallers = append(indirectCallers, newCaller)
				nextLevel = append(nextLevel, newCaller)
			}

			if truncated {
				break
			}
		}

		currentLevel = nextLevel
		if len(currentLevel) == 0 || truncated {
			break
		}
	}

	return indirectCallers, truncated
}

// findImplementers finds types implementing an interface.
func (a *BlastRadiusAnalyzer) findImplementers(ctx context.Context, targetID string) []Implementer {
	implementers := make([]Implementer, 0)

	// Check if target is an interface using the index
	symbol, found := a.index.GetByID(targetID)
	if !found {
		return implementers
	}

	// Only proceed if it's an interface
	if symbol.Kind != ast.SymbolKindInterface {
		return implementers
	}

	// Query graph for implementers
	queryResult, err := a.graph.FindImplementationsByID(ctx, targetID)
	if err != nil {
		return implementers
	}

	for _, implSymbol := range queryResult.Symbols {
		select {
		case <-ctx.Done():
			return implementers
		default:
		}

		if implSymbol == nil {
			continue
		}

		implementers = append(implementers, Implementer{
			TypeID:   implSymbol.ID,
			TypeName: implSymbol.Name,
			FilePath: implSymbol.FilePath,
			Line:     implSymbol.StartLine,
		})
	}

	return implementers
}

// findSharedDeps finds dependencies shared between target and its callers.
func (a *BlastRadiusAnalyzer) findSharedDeps(ctx context.Context, targetID string, directCallers []Caller) []SharedDep {
	sharedDeps := make([]SharedDep, 0)

	// Get callees of target
	targetCallees, err := a.graph.FindCalleesByID(ctx, targetID)
	if err != nil || len(targetCallees.Symbols) == 0 {
		return sharedDeps
	}

	// Build a set of target callees for quick lookup
	targetCalleeIDs := make(map[string]bool)
	for _, sym := range targetCallees.Symbols {
		if sym != nil {
			targetCalleeIDs[sym.ID] = true
		}
	}

	// Build a map of what each caller also calls
	calleeUsers := make(map[string][]string) // callee -> list of callers that use it

	for _, caller := range directCallers {
		select {
		case <-ctx.Done():
			return sharedDeps
		default:
		}

		callerCallees, err := a.graph.FindCalleesByID(ctx, caller.ID)
		if err != nil {
			continue
		}

		for _, callee := range callerCallees.Symbols {
			if callee == nil {
				continue
			}
			// Only care about callees that target also uses
			if targetCalleeIDs[callee.ID] {
				calleeUsers[callee.ID] = append(calleeUsers[callee.ID], caller.Name)
			}
		}
	}

	// Create SharedDep for any callee used by multiple parties
	for calleeID, users := range calleeUsers {
		if len(users) == 0 {
			continue
		}

		symbol, found := a.index.GetByID(calleeID)
		if !found {
			continue
		}

		sharedDeps = append(sharedDeps, SharedDep{
			ID:        calleeID,
			Name:      symbol.Name,
			UsedBy:    users,
			UsageType: "diamond",
			Warning:   fmt.Sprintf("Used by target and %d caller(s); changes may have cascading effects", len(users)),
		})
	}

	return sharedDeps
}

// finalizeResult calculates risk level, gathers files, and generates summary.
func (a *BlastRadiusAnalyzer) finalizeResult(result *BlastRadius, opts AnalyzeOptions) {
	// Calculate risk level
	result.RiskLevel = a.calculateRiskLevel(result)

	// Gather affected files
	fileSet := make(map[string]bool)
	for _, c := range result.DirectCallers {
		fileSet[c.FilePath] = true
	}
	for _, c := range result.IndirectCallers {
		fileSet[c.FilePath] = true
	}
	for _, impl := range result.Implementers {
		fileSet[impl.FilePath] = true
	}

	for file := range fileSet {
		result.FilesAffected = append(result.FilesAffected, file)
	}

	// Find test files
	result.TestFiles = a.findTestFiles(result.FilesAffected, opts.TestPatterns)

	// Generate summary
	result.Summary = a.generateSummary(result)

	// Generate recommendation
	result.Recommendation = a.generateRecommendation(result)
}

// calculateRiskLevel determines the risk level based on analysis results.
func (a *BlastRadiusAnalyzer) calculateRiskLevel(result *BlastRadius) RiskLevel {
	// Interface = critical (all implementers affected)
	if len(result.Implementers) > 0 {
		return RiskCritical
	}

	directCount := len(result.DirectCallers)

	switch {
	case directCount >= a.riskConfig.CriticalThreshold:
		return RiskCritical
	case directCount >= a.riskConfig.HighThreshold:
		return RiskHigh
	case directCount >= a.riskConfig.MediumThreshold:
		return RiskMedium
	default:
		return RiskLow
	}
}

// findTestFiles finds test files for the affected files.
func (a *BlastRadiusAnalyzer) findTestFiles(affectedFiles []string, patterns []string) []string {
	testFiles := make([]string, 0)
	seen := make(map[string]bool)

	for _, file := range affectedFiles {
		dir := filepath.Dir(file)
		base := filepath.Base(file)
		ext := filepath.Ext(base)
		nameWithoutExt := strings.TrimSuffix(base, ext)

		// Look for *_test.go pattern
		for _, pattern := range patterns {
			if strings.Contains(pattern, "*_test") {
				testFile := filepath.Join(dir, nameWithoutExt+"_test"+ext)
				if !seen[testFile] {
					seen[testFile] = true
					testFiles = append(testFiles, testFile)
				}
			}
		}
	}

	return testFiles
}

// generateSummary creates a human-readable summary.
func (a *BlastRadiusAnalyzer) generateSummary(result *BlastRadius) string {
	parts := []string{
		fmt.Sprintf("Risk Level: %s", result.RiskLevel),
		fmt.Sprintf("Direct callers: %d", len(result.DirectCallers)),
	}

	if len(result.IndirectCallers) > 0 {
		parts = append(parts, fmt.Sprintf("Indirect callers: %d", len(result.IndirectCallers)))
	}

	if len(result.Implementers) > 0 {
		parts = append(parts, fmt.Sprintf("Interface implementers: %d", len(result.Implementers)))
	}

	if len(result.SharedDeps) > 0 {
		parts = append(parts, fmt.Sprintf("Shared dependencies: %d", len(result.SharedDeps)))
	}

	parts = append(parts, fmt.Sprintf("Files affected: %d", len(result.FilesAffected)))

	if result.Truncated {
		parts = append(parts, fmt.Sprintf("(Truncated: %s)", result.TruncatedReason))
	}

	return strings.Join(parts, " | ")
}

// generateRecommendation creates actionable advice.
func (a *BlastRadiusAnalyzer) generateRecommendation(result *BlastRadius) string {
	switch result.RiskLevel {
	case RiskCritical:
		if len(result.Implementers) > 0 {
			return fmt.Sprintf("CRITICAL: This is an interface with %d implementers. All implementers must be updated together. Consider creating a new interface version instead of modifying.", len(result.Implementers))
		}
		return fmt.Sprintf("CRITICAL: %d direct callers will be affected. Consider deprecating this function and creating a new one, or ensure all callers are updated atomically.", len(result.DirectCallers))

	case RiskHigh:
		return fmt.Sprintf("HIGH RISK: %d callers need review. Update all direct callers and run tests for affected files: %s", len(result.DirectCallers), strings.Join(result.TestFiles, ", "))

	case RiskMedium:
		return fmt.Sprintf("MEDIUM RISK: Update %d direct callers and verify behavior. Run: %s", len(result.DirectCallers), strings.Join(result.TestFiles, ", "))

	case RiskLow:
		if len(result.DirectCallers) == 0 {
			return "LOW RISK: No callers found. Safe to modify, but verify the symbol is not called dynamically (reflection, string-based lookup)."
		}
		return fmt.Sprintf("LOW RISK: Only %d caller(s). Update and test.", len(result.DirectCallers))

	default:
		return "Unable to determine risk level."
	}
}
