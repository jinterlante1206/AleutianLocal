// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"context"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// SideEffectAnalyzer detects side effects in functions.
//
// Description:
//
//	SideEffectAnalyzer helps understand what external effects a function has,
//	including file I/O, network calls, database operations, and global state
//	mutations. This is crucial for understanding the impact of changes.
//
// Thread Safety:
//
//	SideEffectAnalyzer is safe for concurrent use.
type SideEffectAnalyzer struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewSideEffectAnalyzer creates a new SideEffectAnalyzer.
//
// Description:
//
//	Creates an analyzer that can detect side effects in functions using
//	the code graph and symbol index.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*SideEffectAnalyzer - The configured analyzer.
func NewSideEffectAnalyzer(g *graph.Graph, idx *index.SymbolIndex) *SideEffectAnalyzer {
	return &SideEffectAnalyzer{
		graph: g,
		index: idx,
	}
}

// SideEffect describes a single side effect detected in a function.
type SideEffect struct {
	// Type categorizes the side effect (file_io, network, database, etc.).
	Type SideEffectType `json:"type"`

	// Operation describes the specific operation (e.g., "Write", "Query").
	Operation string `json:"operation"`

	// Location describes where the side effect occurs.
	Location string `json:"location"`

	// Description explains what the side effect does.
	Description string `json:"description"`

	// Reversible indicates if the effect can be undone.
	Reversible bool `json:"reversible"`

	// Idempotent indicates if repeating the call is safe.
	Idempotent bool `json:"idempotent"`

	// Transitive indicates if this effect comes from a called function.
	Transitive bool `json:"transitive"`

	// SourceFunctionID is the function that directly has the side effect.
	// For transitive effects, this is the callee that has the effect.
	SourceFunctionID string `json:"source_function_id,omitempty"`

	// CallChain shows the path from target to the side effect source.
	CallChain []string `json:"call_chain,omitempty"`
}

// SideEffectAnalysis is the result of side effect analysis.
type SideEffectAnalysis struct {
	// TargetID is the symbol being analyzed.
	TargetID string `json:"target_id"`

	// IsPure indicates if the function has no side effects.
	IsPure bool `json:"is_pure"`

	// SideEffects lists all detected side effects.
	SideEffects []SideEffect `json:"side_effects"`

	// DirectEffects is the count of direct side effects.
	DirectEffects int `json:"direct_effects"`

	// TransitiveEffects is the count of transitive side effects.
	TransitiveEffects int `json:"transitive_effects"`

	// Confidence is how confident we are in the analysis (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// Limitations lists what we couldn't analyze.
	Limitations []string `json:"limitations"`
}

// FindSideEffects analyzes a function for side effects.
//
// Description:
//
//	Detects side effects in the target function including file I/O,
//	network calls, database operations, and global state mutations.
//	Also tracks transitive side effects through called functions.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	targetID - The symbol ID to analyze.
//
// Outputs:
//
//	*SideEffectAnalysis - Analysis results.
//	error - Non-nil if the analysis fails.
//
// Example:
//
//	analysis, err := analyzer.FindSideEffects(ctx, "pkg/handlers.SaveUser")
//	if !analysis.IsPure {
//	    fmt.Printf("Found %d side effects\n", len(analysis.SideEffects))
//	}
//
// Limitations:
//
//   - Static analysis only, cannot detect runtime-determined side effects
//   - May not detect effects through reflection or interfaces
//   - Transitive tracking limited to prevent performance issues
func (a *SideEffectAnalyzer) FindSideEffects(
	ctx context.Context,
	targetID string,
) (*SideEffectAnalysis, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if targetID == "" {
		return nil, ErrInvalidInput
	}
	if a.graph != nil && !a.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	// Find the target symbol
	symbol, found := a.index.GetByID(targetID)
	if !found || symbol == nil {
		return nil, ErrSymbolNotFound
	}

	result := &SideEffectAnalysis{
		TargetID:    targetID,
		SideEffects: make([]SideEffect, 0),
		Limitations: make([]string, 0),
	}

	// Get patterns for the language
	patterns := GetPatternsForLanguage(symbol.Language)
	if patterns == nil {
		result.Limitations = append(result.Limitations,
			"Unsupported language: "+symbol.Language)
		result.IsPure = false // Assume not pure if we can't analyze
		result.Confidence = 0.3
		return result, nil
	}

	// Find direct side effects (calls to side-effecting functions)
	directEffects := a.findDirectEffects(symbol, patterns)
	result.SideEffects = append(result.SideEffects, directEffects...)
	result.DirectEffects = len(directEffects)

	// Find transitive side effects (through called functions)
	if a.graph != nil {
		transitiveEffects := a.findTransitiveEffects(ctx, targetID, patterns,
			make(map[string]bool), []string{targetID}, 0)
		result.SideEffects = append(result.SideEffects, transitiveEffects...)
		result.TransitiveEffects = len(transitiveEffects)
	} else {
		result.Limitations = append(result.Limitations,
			"Graph not available - cannot track transitive effects")
	}

	// Determine purity
	result.IsPure = len(result.SideEffects) == 0

	// Calculate confidence
	result.Confidence = a.calculateConfidence(result, symbol)

	return result, nil
}

// findDirectEffects finds side effects directly in the target function.
func (a *SideEffectAnalyzer) findDirectEffects(
	symbol *ast.Symbol,
	patterns *SideEffectPatterns,
) []SideEffect {
	effects := make([]SideEffect, 0)

	if a.graph == nil {
		return effects
	}

	node, found := a.graph.GetNode(symbol.ID)
	if !found || node == nil {
		return effects
	}

	// Check each outgoing call
	for _, edge := range node.Outgoing {
		if edge.Type != graph.EdgeTypeCalls {
			continue
		}

		calledNode, calledFound := a.graph.GetNode(edge.ToID)
		if !calledFound || calledNode == nil || calledNode.Symbol == nil {
			continue
		}

		calledSymbol := calledNode.Symbol

		// Check if the called function matches any pattern
		for _, pattern := range patterns.GetAllPatterns() {
			if a.matchesPattern(calledSymbol, pattern, symbol.Language) {
				effect := SideEffect{
					Type:        pattern.EffectType,
					Operation:   calledSymbol.Name,
					Location:    formatLocation(edge.Location.FilePath, edge.Location.StartLine),
					Description: pattern.Description,
					Reversible:  pattern.Reversible,
					Idempotent:  pattern.Idempotent,
					Transitive:  false,
				}
				effects = append(effects, effect)
				break
			}
		}
	}

	return effects
}

// findTransitiveEffects finds side effects through called functions.
func (a *SideEffectAnalyzer) findTransitiveEffects(
	ctx context.Context,
	targetID string,
	patterns *SideEffectPatterns,
	visited map[string]bool,
	callChain []string,
	depth int,
) []SideEffect {
	effects := make([]SideEffect, 0)

	// Limit depth to prevent performance issues
	const maxDepth = 5
	if depth >= maxDepth {
		return effects
	}

	// Check for cancellation
	if ctx.Err() != nil {
		return effects
	}

	if visited[targetID] {
		return effects
	}
	visited[targetID] = true

	node, found := a.graph.GetNode(targetID)
	if !found || node == nil {
		return effects
	}

	// Check each outgoing call
	for _, edge := range node.Outgoing {
		if edge.Type != graph.EdgeTypeCalls {
			continue
		}

		calleeID := edge.ToID
		if visited[calleeID] {
			continue
		}

		calledNode, calledFound := a.graph.GetNode(calleeID)
		if !calledFound || calledNode == nil || calledNode.Symbol == nil {
			continue
		}

		calledSymbol := calledNode.Symbol
		newChain := append(append([]string{}, callChain...), calleeID)

		// Check if the callee has direct side effects
		for _, pattern := range patterns.GetAllPatterns() {
			if a.matchesPattern(calledSymbol, pattern, calledSymbol.Language) {
				// Check if this isn't in the first level (those are direct effects)
				if depth > 0 {
					effect := SideEffect{
						Type:             pattern.EffectType,
						Operation:        calledSymbol.Name,
						Location:         formatLocation(calledSymbol.FilePath, calledSymbol.StartLine),
						Description:      pattern.Description + " (transitive)",
						Reversible:       pattern.Reversible,
						Idempotent:       pattern.Idempotent,
						Transitive:       true,
						SourceFunctionID: calleeID,
						CallChain:        newChain,
					}
					effects = append(effects, effect)
				}
				break
			}
		}

		// Recursively check callees
		transitiveEffects := a.findTransitiveEffects(ctx, calleeID, patterns,
			visited, newChain, depth+1)
		effects = append(effects, transitiveEffects...)
	}

	return effects
}

// matchesPattern checks if a symbol matches a side effect pattern.
func (a *SideEffectAnalyzer) matchesPattern(
	symbol *ast.Symbol,
	pattern FunctionPattern,
	language string,
) bool {
	// Check function name
	nameMatches := false
	for _, fn := range pattern.Functions {
		if symbol.Name == fn {
			nameMatches = true
			break
		}
	}

	if !nameMatches {
		return false
	}

	// Check package/module
	switch language {
	case "go":
		if pattern.Package != "" && symbol.Package != pattern.Package {
			// Also check for partial package match (e.g., "net/http" contains "http")
			if !strings.HasSuffix(symbol.Package, "/"+pattern.Package) &&
				!strings.HasSuffix(symbol.Package, pattern.Package) {
				return false
			}
		}
	case "python":
		if pattern.Module != "" && symbol.Package != pattern.Module {
			return false
		}
	case "typescript", "javascript":
		// For JS/TS, module matching is more flexible
		if pattern.Module != "" && symbol.Package != pattern.Module {
			// Check import path
			if !strings.Contains(symbol.FilePath, pattern.Module) &&
				symbol.Package != pattern.Module {
				return false
			}
		}
	}

	return true
}

// calculateConfidence computes confidence for the analysis.
func (a *SideEffectAnalyzer) calculateConfidence(
	analysis *SideEffectAnalysis,
	symbol *ast.Symbol,
) float64 {
	cal := NewConfidenceCalibration(0.8)

	// Reduce confidence for each limitation
	for range analysis.Limitations {
		cal.Apply(ConfidenceAdjustment{
			Reason:     "analysis limitation",
			Multiplier: 0.9,
		})
	}

	// Increase confidence if we found effects (shows analysis is working)
	if len(analysis.SideEffects) > 0 {
		cal.Apply(ConfidenceAdjustment{
			Reason:     "detected side effects",
			Multiplier: 1.05,
		})
	}

	// Decrease confidence for many transitive effects (harder to verify)
	if analysis.TransitiveEffects > 5 {
		cal.Apply(ConfidenceAdjustment{
			Reason:     "many transitive effects",
			Multiplier: 0.9,
		})
	}

	// Test files may have unusual patterns
	cal.ApplyIf(isTestFile(symbol.FilePath), AdjustmentInTestFile)

	// Always apply static analysis limitation
	cal.Apply(AdjustmentStaticAnalysisOnly)

	return cal.FinalScore
}

// FindSideEffectsBatch analyzes multiple functions for side effects.
//
// Description:
//
//	Analyzes multiple functions for side effects in a batch.
//	More efficient than individual calls when analyzing related functions.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	targetIDs - The symbol IDs to analyze.
//
// Outputs:
//
//	map[string]*SideEffectAnalysis - Analysis results keyed by targetID.
//	error - Non-nil if the analysis fails completely.
func (a *SideEffectAnalyzer) FindSideEffectsBatch(
	ctx context.Context,
	targetIDs []string,
) (map[string]*SideEffectAnalysis, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	results := make(map[string]*SideEffectAnalysis)

	for _, targetID := range targetIDs {
		if err := ctx.Err(); err != nil {
			return results, ErrContextCanceled
		}

		analysis, err := a.FindSideEffects(ctx, targetID)
		if err != nil {
			results[targetID] = &SideEffectAnalysis{
				TargetID:    targetID,
				Limitations: []string{"Analysis failed: " + err.Error()},
			}
			continue
		}
		results[targetID] = analysis
	}

	return results, nil
}

// GetPureFunctions returns all functions that appear to be pure.
//
// Description:
//
//	Analyzes all functions in the index and returns those with no detected
//	side effects. Useful for identifying functions safe to optimize or cache.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	[]string - IDs of functions that appear to be pure.
//	error - Non-nil if the analysis fails.
//
// Limitations:
//
//   - May include false positives (functions with undetectable side effects)
//   - May exclude false negatives (pure functions with unrecognized patterns)
func (a *SideEffectAnalyzer) GetPureFunctions(
	ctx context.Context,
) ([]string, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	pureFunctions := make([]string, 0)

	// Get all functions
	allFuncs := a.index.GetByKind(ast.SymbolKindFunction)
	allMethods := a.index.GetByKind(ast.SymbolKindMethod)
	allSymbols := append(allFuncs, allMethods...)

	for _, fn := range allSymbols {
		if ctx.Err() != nil {
			return pureFunctions, ErrContextCanceled
		}

		// Skip test functions
		if isTestFunction(fn) {
			continue
		}

		analysis, err := a.FindSideEffects(ctx, fn.ID)
		if err != nil {
			continue
		}

		if analysis.IsPure {
			pureFunctions = append(pureFunctions, fn.ID)
		}
	}

	return pureFunctions, nil
}

// SummarySideEffects returns a summary of side effects grouped by type.
//
// Description:
//
//	Groups the side effects in an analysis by their type for easier
//	understanding and reporting.
//
// Inputs:
//
//	analysis - The side effect analysis to summarize.
//
// Outputs:
//
//	map[SideEffectType][]SideEffect - Side effects grouped by type.
func SummarySideEffects(analysis *SideEffectAnalysis) map[SideEffectType][]SideEffect {
	summary := make(map[SideEffectType][]SideEffect)

	for _, effect := range analysis.SideEffects {
		summary[effect.Type] = append(summary[effect.Type], effect)
	}

	return summary
}

// Helper functions

func formatLocation(filePath string, line int) string {
	if filePath == "" {
		return ""
	}
	if line <= 0 {
		return filePath
	}
	return filePath + ":" + intToString(line)
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	if neg {
		digits = append([]byte{'-'}, digits...)
	}

	return string(digits)
}
