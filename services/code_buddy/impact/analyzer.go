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
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/analysis"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/explore"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/reason"
)

// ChangeImpactAnalyzer orchestrates multiple analysis tools to provide
// comprehensive change impact assessment.
//
// # Description
//
// ChangeImpactAnalyzer combines blast radius, breaking change, test coverage,
// side effect, and data flow analysis into a single unified report. It runs
// independent analyses in parallel for performance.
//
// # Thread Safety
//
// ChangeImpactAnalyzer is safe for concurrent use after initialization.
type ChangeImpactAnalyzer struct {
	graph    *graph.Graph
	index    *index.SymbolIndex
	breaking *reason.BreakingChangeAnalyzer
	blast    *analysis.BlastRadiusAnalyzer
	coverage *reason.TestCoverageFinder
	side     *reason.SideEffectAnalyzer
	flow     *explore.DataFlowTracer
	weights  RiskWeights
}

// NewChangeImpactAnalyzer creates a new analyzer with all sub-analyzers.
//
// # Description
//
// Creates an analyzer that orchestrates blast radius, breaking change,
// test coverage, side effect, and data flow analysis.
//
// # Inputs
//
//   - g: Code graph. Must be frozen before calling AnalyzeImpact.
//   - idx: Symbol index for lookups.
//
// # Outputs
//
//   - *ChangeImpactAnalyzer: Ready-to-use analyzer.
//
// # Example
//
//	analyzer := impact.NewChangeImpactAnalyzer(graph, index)
//	result, err := analyzer.AnalyzeImpact(ctx, "pkg.Function", "func(ctx, new) error", nil)
func NewChangeImpactAnalyzer(g *graph.Graph, idx *index.SymbolIndex) *ChangeImpactAnalyzer {
	return &ChangeImpactAnalyzer{
		graph:    g,
		index:    idx,
		breaking: reason.NewBreakingChangeAnalyzer(g, idx),
		blast:    analysis.NewBlastRadiusAnalyzer(g, idx, nil),
		coverage: reason.NewTestCoverageFinder(g, idx),
		side:     reason.NewSideEffectAnalyzer(g, idx),
		flow:     explore.NewDataFlowTracer(g, idx),
		weights:  DefaultRiskWeights(),
	}
}

// WithRiskWeights sets custom risk calculation weights.
//
// # Description
//
// Allows customizing how different factors contribute to the overall
// risk score. Returns the analyzer for method chaining.
//
// # Example
//
//	analyzer := impact.NewChangeImpactAnalyzer(g, idx).WithRiskWeights(RiskWeights{
//	    Breaking: 0.40, // Increase weight of breaking changes
//	    BlastRadius: 0.20,
//	    TestCoverage: 0.20,
//	    SideEffects: 0.10,
//	    Exported: 0.10,
//	})
func (a *ChangeImpactAnalyzer) WithRiskWeights(w RiskWeights) *ChangeImpactAnalyzer {
	a.weights = w
	return a
}

// AnalyzeImpact performs comprehensive change impact analysis.
//
// # Description
//
// Orchestrates multiple analysis tools to provide a unified view of what
// would happen if the target symbol is changed. Runs independent analyses
// in parallel for performance. The proposed change is optional - if not
// provided, breaking change analysis is skipped.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - targetID: Symbol ID to analyze (e.g., "pkg/auth.go:10:ValidateToken").
//   - proposedChange: Optional new signature for breaking change analysis.
//   - opts: Analysis options (nil uses defaults).
//
// # Outputs
//
//   - *ChangeImpact: Unified analysis results.
//   - error: Non-nil if validation fails or all analyses fail.
//
// # Performance
//
// Target latency: < 500ms for typical codebase.
// Independent analyses run in parallel.
//
// # Example
//
//	result, err := analyzer.AnalyzeImpact(ctx,
//	    "handlers/user.go:10:HandleUser",
//	    "func(ctx context.Context, req *Request) (*Response, error)",
//	    nil,
//	)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Risk: %s (%.2f)\n", result.RiskLevel, result.RiskScore)
//	for _, action := range result.SuggestedActions {
//	    fmt.Printf("- %s\n", action)
//	}
//
// # Limitations
//
//   - Breaking change analysis requires proposedChange to be provided.
//   - Data flow analysis is function-level, not variable-level.
//   - Side effect detection uses pattern matching, may miss dynamic calls.
func (a *ChangeImpactAnalyzer) AnalyzeImpact(
	ctx context.Context,
	targetID string,
	proposedChange string,
	opts *AnalyzeOptions,
) (*ChangeImpact, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if targetID == "" {
		return nil, fmt.Errorf("%w: targetID is empty", ErrInvalidInput)
	}
	if a.graph != nil && !a.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	// Get target symbol
	symbol, found := a.index.GetByID(targetID)
	if !found || symbol == nil {
		return nil, ErrSymbolNotFound
	}

	options := DefaultAnalyzeOptions()
	if opts != nil {
		options = *opts
	}

	// Skip breaking change analysis if no proposed change
	if proposedChange == "" {
		options.IncludeBreaking = false
	}

	result := &ChangeImpact{
		TargetID:       targetID,
		TargetName:     symbol.Name,
		ProposedChange: proposedChange,
		Limitations:    make([]string, 0),
		Warnings:       make([]string, 0),
	}

	// Run analyses in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var analysisErrors []error

	// Blast radius analysis
	if options.IncludeBlastRadius {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runBlastRadiusAnalysis(ctx, targetID, options, result, &mu); err != nil {
				mu.Lock()
				analysisErrors = append(analysisErrors, fmt.Errorf("blast radius: %w", err))
				result.Limitations = append(result.Limitations, "Blast radius analysis failed")
				mu.Unlock()
			}
		}()
	}

	// Breaking change analysis
	if options.IncludeBreaking && proposedChange != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runBreakingAnalysis(ctx, targetID, proposedChange, result, &mu); err != nil {
				mu.Lock()
				analysisErrors = append(analysisErrors, fmt.Errorf("breaking changes: %w", err))
				result.Limitations = append(result.Limitations, "Breaking change analysis failed")
				mu.Unlock()
			}
		}()
	}

	// Test coverage analysis
	if options.IncludeCoverage {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runCoverageAnalysis(ctx, targetID, result, &mu); err != nil {
				mu.Lock()
				analysisErrors = append(analysisErrors, fmt.Errorf("coverage: %w", err))
				result.Limitations = append(result.Limitations, "Test coverage analysis failed")
				mu.Unlock()
			}
		}()
	}

	// Side effect analysis
	if options.IncludeSideEffects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runSideEffectAnalysis(ctx, targetID, options, result, &mu); err != nil {
				mu.Lock()
				analysisErrors = append(analysisErrors, fmt.Errorf("side effects: %w", err))
				result.Limitations = append(result.Limitations, "Side effect analysis failed")
				mu.Unlock()
			}
		}()
	}

	// Data flow analysis
	if options.IncludeDataFlow {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runDataFlowAnalysis(ctx, targetID, result, &mu); err != nil {
				mu.Lock()
				analysisErrors = append(analysisErrors, fmt.Errorf("data flow: %w", err))
				result.Limitations = append(result.Limitations, "Data flow analysis failed")
				mu.Unlock()
			}
		}()
	}

	// Wait for all analyses to complete
	wg.Wait()

	// Check if context was canceled
	if ctx.Err() != nil {
		return nil, ErrContextCanceled
	}

	// Calculate overall risk score and level
	a.calculateRiskScore(symbol, result)

	// Generate suggested actions
	a.generateSuggestedActions(symbol, result)

	// If all analyses failed, return error
	if len(analysisErrors) > 0 && len(result.Limitations) >= 5 {
		return result, ErrAnalysisFailed
	}

	return result, nil
}

// runBlastRadiusAnalysis runs blast radius analysis and populates results.
func (a *ChangeImpactAnalyzer) runBlastRadiusAnalysis(
	ctx context.Context,
	targetID string,
	options AnalyzeOptions,
	result *ChangeImpact,
	mu *sync.Mutex,
) error {
	blastOpts := &analysis.AnalyzeOptions{
		MaxDirectCallers:   options.MaxCallers,
		MaxIndirectCallers: options.MaxCallers * 5,
		MaxHops:            options.MaxHops,
	}

	blastResult, err := a.blast.Analyze(ctx, targetID, blastOpts)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	result.DirectCallers = len(blastResult.DirectCallers)
	result.IndirectCallers = len(blastResult.IndirectCallers)
	result.TotalImpact = result.DirectCallers + result.IndirectCallers + len(blastResult.Implementers)
	result.BlastRiskLevel = blastResult.RiskLevel

	if options.IncludeFileLists {
		result.FilesAffected = blastResult.FilesAffected
		result.TestFiles = blastResult.TestFiles
	}

	return nil
}

// runBreakingAnalysis runs breaking change analysis and populates results.
func (a *ChangeImpactAnalyzer) runBreakingAnalysis(
	ctx context.Context,
	targetID string,
	proposedChange string,
	result *ChangeImpact,
	mu *sync.Mutex,
) error {
	breakingResult, err := a.breaking.AnalyzeBreaking(ctx, targetID, proposedChange)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	result.IsBreaking = breakingResult.IsBreaking
	result.BreakingChanges = breakingResult.BreakingChanges

	return nil
}

// runCoverageAnalysis runs test coverage analysis and populates results.
func (a *ChangeImpactAnalyzer) runCoverageAnalysis(
	ctx context.Context,
	targetID string,
	result *ChangeImpact,
	mu *sync.Mutex,
) error {
	coverageResult, err := a.coverage.FindTestCoverage(ctx, targetID)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	totalTests := len(coverageResult.DirectTests) + len(coverageResult.IndirectTests)
	result.TestsCovering = totalTests

	if len(coverageResult.DirectTests) > 0 {
		result.CoverageLevel = CoverageGood
	} else if len(coverageResult.IndirectTests) > 0 {
		result.CoverageLevel = CoveragePartial
	} else {
		result.CoverageLevel = CoverageNone
	}

	return nil
}

// runSideEffectAnalysis runs side effect analysis and populates results.
func (a *ChangeImpactAnalyzer) runSideEffectAnalysis(
	ctx context.Context,
	targetID string,
	options AnalyzeOptions,
	result *ChangeImpact,
	mu *sync.Mutex,
) error {
	sideResult, err := a.side.FindSideEffects(ctx, targetID)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	result.HasSideEffects = !sideResult.IsPure
	result.SideEffectCount = len(sideResult.SideEffects)

	// Collect unique side effect types
	typeSet := make(map[string]struct{})
	for _, se := range sideResult.SideEffects {
		typeSet[string(se.Type)] = struct{}{}
	}
	for t := range typeSet {
		result.SideEffectTypes = append(result.SideEffectTypes, t)
	}

	if options.IncludeDetailedSideEffects {
		result.SideEffects = sideResult.SideEffects
	}

	return nil
}

// runDataFlowAnalysis runs data flow analysis and populates results.
func (a *ChangeImpactAnalyzer) runDataFlowAnalysis(
	ctx context.Context,
	targetID string,
	result *ChangeImpact,
	mu *sync.Mutex,
) error {
	flowResult, err := a.flow.TraceDataFlow(ctx, targetID)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	// Collect unique sink categories
	sinkSet := make(map[string]struct{})
	for _, sink := range flowResult.Sinks {
		sinkSet[sink.Category] = struct{}{}
	}
	for s := range sinkSet {
		result.DataSinks = append(result.DataSinks, s)
	}

	return nil
}

// calculateRiskScore computes the overall risk score and level.
func (a *ChangeImpactAnalyzer) calculateRiskScore(symbol *ast.Symbol, result *ChangeImpact) {
	var score float64

	// Breaking changes factor (0.0 - 1.0)
	if result.IsBreaking {
		score += a.weights.Breaking * 1.0
	}

	// Blast radius factor (0.0 - 1.0)
	// Scale: 0 callers = 0.0, 20+ callers = 1.0
	blastFactor := float64(result.DirectCallers) / 20.0
	if blastFactor > 1.0 {
		blastFactor = 1.0
	}
	score += a.weights.BlastRadius * blastFactor

	// Test coverage factor (0.0 - 1.0)
	// Inverted: good coverage = 0.0, no coverage = 1.0
	var coverageFactor float64
	switch result.CoverageLevel {
	case CoverageGood:
		coverageFactor = 0.0
	case CoveragePartial:
		coverageFactor = 0.5
	case CoverageNone:
		coverageFactor = 1.0
	}
	score += a.weights.TestCoverage * coverageFactor

	// Side effects factor (0.0 - 1.0)
	if result.HasSideEffects {
		// Scale by number of side effects: 1 = 0.5, 3+ = 1.0
		seFactor := 0.5 + float64(result.SideEffectCount-1)*0.25
		if seFactor > 1.0 {
			seFactor = 1.0
		}
		score += a.weights.SideEffects * seFactor
	}

	// Exported factor (0.0 - 1.0)
	if symbol != nil && symbol.Exported {
		score += a.weights.Exported * 1.0
	}

	result.RiskScore = score

	// Determine risk level from score
	switch {
	case score >= 0.7:
		result.RiskLevel = RiskCritical
	case score >= 0.5:
		result.RiskLevel = RiskHigh
	case score >= 0.3:
		result.RiskLevel = RiskMedium
	default:
		result.RiskLevel = RiskLow
	}

	// Calculate confidence based on what analyses succeeded
	confidence := 1.0
	for range result.Limitations {
		confidence *= 0.9 // Reduce 10% per limitation
	}
	result.Confidence = confidence
}

// generateSuggestedActions creates actionable recommendations.
func (a *ChangeImpactAnalyzer) generateSuggestedActions(symbol *ast.Symbol, result *ChangeImpact) {
	actions := make([]string, 0)

	// Breaking changes
	if result.IsBreaking {
		actions = append(actions, "Review and update all breaking callers before applying change")
		if len(result.BreakingChanges) > 0 {
			actions = append(actions, fmt.Sprintf("Fix %d breaking change(s) in affected callers", len(result.BreakingChanges)))
		}
	}

	// Test coverage
	switch result.CoverageLevel {
	case CoverageNone:
		actions = append(actions, "Add tests before making changes - no coverage detected")
		result.Warnings = append(result.Warnings, "No test coverage found for this symbol")
	case CoveragePartial:
		actions = append(actions, "Consider adding direct tests - only indirect coverage exists")
	case CoverageGood:
		if len(result.TestFiles) > 0 {
			actions = append(actions, fmt.Sprintf("Run tests in %d file(s) after changes", len(result.TestFiles)))
		}
	}

	// Side effects
	if result.HasSideEffects {
		for _, seType := range result.SideEffectTypes {
			switch seType {
			case "database":
				actions = append(actions, "Verify database operations are idempotent or test with rollback")
			case "file_io":
				actions = append(actions, "Check file operations for proper error handling")
			case "network":
				actions = append(actions, "Consider retry logic and timeout handling for network calls")
			case "global_state":
				result.Warnings = append(result.Warnings, "Function modifies global state - check for race conditions")
			}
		}
	}

	// High blast radius
	if result.DirectCallers >= 10 {
		actions = append(actions, fmt.Sprintf("Consider deprecation strategy - %d direct callers affected", result.DirectCallers))
	}

	// Exported symbol
	if symbol != nil && symbol.Exported {
		result.Warnings = append(result.Warnings, "This is a public API symbol - changes may affect external consumers")
	}

	// If no specific actions, provide generic guidance
	if len(actions) == 0 {
		if result.RiskLevel == RiskLow {
			actions = append(actions, "Low risk change - proceed with normal review process")
		} else {
			actions = append(actions, "Review impact before proceeding")
		}
	}

	result.SuggestedActions = actions
}
