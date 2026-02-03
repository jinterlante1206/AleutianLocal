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
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// createTestGraph creates a test graph with basic relationships.
func createTestGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	// Create symbols
	target := &ast.Symbol{
		ID:        "pkg/service.go:10:ProcessData",
		Name:      "ProcessData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service.go",
		StartLine: 10,
		EndLine:   25,
		Exported:  true,
		Language:  "go",
		Signature: "func ProcessData(ctx context.Context, data []byte) error",
	}

	caller1 := &ast.Symbol{
		ID:        "pkg/handler.go:20:HandleRequest",
		Name:      "HandleRequest",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handler.go",
		StartLine: 20,
		EndLine:   35,
		Exported:  true,
		Language:  "go",
		Signature: "func HandleRequest(w http.ResponseWriter, r *http.Request)",
	}

	caller2 := &ast.Symbol{
		ID:        "pkg/worker.go:15:ProcessJob",
		Name:      "ProcessJob",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/worker.go",
		StartLine: 15,
		EndLine:   30,
		Exported:  true,
		Language:  "go",
		Signature: "func ProcessJob(job *Job) error",
	}

	testFunc := &ast.Symbol{
		ID:        "pkg/service_test.go:10:TestProcessData",
		Name:      "TestProcessData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service_test.go",
		StartLine: 10,
		EndLine:   25,
		Exported:  true,
		Language:  "go",
		Signature: "func TestProcessData(t *testing.T)",
	}

	indirectCaller := &ast.Symbol{
		ID:        "cmd/main.go:10:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "cmd/main.go",
		StartLine: 10,
		EndLine:   30,
		Exported:  false,
		Language:  "go",
		Signature: "func main()",
	}

	// Create graph and index
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Add symbols to index
	idx.Add(target)
	idx.Add(caller1)
	idx.Add(caller2)
	idx.Add(testFunc)
	idx.Add(indirectCaller)

	// Add nodes to graph
	g.AddNode(target)
	g.AddNode(caller1)
	g.AddNode(caller2)
	g.AddNode(testFunc)
	g.AddNode(indirectCaller)

	// Add edges (caller -> target = "calls")
	loc := ast.Location{FilePath: "pkg/handler.go", StartLine: 25}
	g.AddEdge(caller1.ID, target.ID, graph.EdgeTypeCalls, loc)

	loc2 := ast.Location{FilePath: "pkg/worker.go", StartLine: 20}
	g.AddEdge(caller2.ID, target.ID, graph.EdgeTypeCalls, loc2)

	loc3 := ast.Location{FilePath: "pkg/service_test.go", StartLine: 15}
	g.AddEdge(testFunc.ID, target.ID, graph.EdgeTypeCalls, loc3)

	loc4 := ast.Location{FilePath: "cmd/main.go", StartLine: 15}
	g.AddEdge(indirectCaller.ID, caller1.ID, graph.EdgeTypeCalls, loc4)

	// Freeze graph
	g.Freeze()

	return g, idx
}

func TestNewChangeImpactAnalyzer(t *testing.T) {
	g, idx := createTestGraph(t)

	analyzer := NewChangeImpactAnalyzer(g, idx)

	if analyzer == nil {
		t.Fatal("expected analyzer to be non-nil")
	}
	if analyzer.graph != g {
		t.Error("expected graph to be set")
	}
	if analyzer.index != idx {
		t.Error("expected index to be set")
	}
	if analyzer.breaking == nil {
		t.Error("expected breaking analyzer to be set")
	}
	if analyzer.blast == nil {
		t.Error("expected blast analyzer to be set")
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_NilContext(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)

	_, err := analyzer.AnalyzeImpact(nil, "pkg/service.go:10:ProcessData", "", nil)

	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_EmptyTargetID(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)
	ctx := context.Background()

	_, err := analyzer.AnalyzeImpact(ctx, "", "", nil)

	if err == nil {
		t.Error("expected error for empty targetID")
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_SymbolNotFound(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)
	ctx := context.Background()

	_, err := analyzer.AnalyzeImpact(ctx, "nonexistent:symbol", "", nil)

	if err != ErrSymbolNotFound {
		t.Errorf("expected ErrSymbolNotFound, got %v", err)
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_BasicAnalysis(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)
	ctx := context.Background()

	result, err := analyzer.AnalyzeImpact(ctx, "pkg/service.go:10:ProcessData", "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result to be non-nil")
	}

	// Check basic fields
	if result.TargetID != "pkg/service.go:10:ProcessData" {
		t.Errorf("expected target ID to be set, got %s", result.TargetID)
	}
	if result.TargetName != "ProcessData" {
		t.Errorf("expected target name 'ProcessData', got %s", result.TargetName)
	}

	// Check blast radius was analyzed
	if result.DirectCallers < 2 {
		t.Errorf("expected at least 2 direct callers, got %d", result.DirectCallers)
	}

	// Check risk assessment exists
	if result.RiskLevel == "" {
		t.Error("expected risk level to be set")
	}
	if result.RiskScore < 0 || result.RiskScore > 1 {
		t.Errorf("expected risk score between 0 and 1, got %f", result.RiskScore)
	}

	// Check suggested actions
	if len(result.SuggestedActions) == 0 {
		t.Error("expected at least one suggested action")
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_WithBreakingChange(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)
	ctx := context.Background()

	// Propose a breaking change (add parameter)
	newSig := "func ProcessData(ctx context.Context, data []byte, opts Options) error"
	result, err := analyzer.AnalyzeImpact(ctx, "pkg/service.go:10:ProcessData", newSig, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The analysis should have attempted breaking change detection
	// (Whether it finds breaking changes depends on the implementation)
	if result.ProposedChange != newSig {
		t.Errorf("expected proposed change to be set")
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_QuickOptions(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)
	ctx := context.Background()

	opts := QuickAnalyzeOptions()
	result, err := analyzer.AnalyzeImpact(ctx, "pkg/service.go:10:ProcessData", "", &opts)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Quick options should still produce a result
	if result == nil {
		t.Fatal("expected result to be non-nil")
	}

	// Quick options disable file lists
	if len(result.FilesAffected) != 0 {
		t.Errorf("expected no files affected with quick options, got %d", len(result.FilesAffected))
	}
}

func TestChangeImpactAnalyzer_AnalyzeImpact_ContextCanceled(t *testing.T) {
	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := analyzer.AnalyzeImpact(ctx, "pkg/service.go:10:ProcessData", "", nil)

	if err != ErrContextCanceled {
		t.Errorf("expected ErrContextCanceled, got %v", err)
	}
}

func TestChangeImpactAnalyzer_WithRiskWeights(t *testing.T) {
	g, idx := createTestGraph(t)

	customWeights := RiskWeights{
		Breaking:     0.50, // Increase breaking weight
		BlastRadius:  0.20,
		TestCoverage: 0.15,
		SideEffects:  0.10,
		Exported:     0.05,
	}

	analyzer := NewChangeImpactAnalyzer(g, idx).WithRiskWeights(customWeights)

	if analyzer.weights.Breaking != 0.50 {
		t.Errorf("expected breaking weight 0.50, got %f", analyzer.weights.Breaking)
	}
}

func TestRiskScore_Calculation(t *testing.T) {
	tests := []struct {
		name           string
		isBreaking     bool
		directCallers  int
		coverage       CoverageLevel
		hasSideEffects bool
		exported       bool
		expectedRange  [2]float64 // min, max
		expectedLevel  RiskLevel
	}{
		{
			name:           "low risk - no callers, good coverage",
			isBreaking:     false,
			directCallers:  0,
			coverage:       CoverageGood,
			hasSideEffects: false,
			exported:       false,
			expectedRange:  [2]float64{0.0, 0.3},
			expectedLevel:  RiskLow,
		},
		{
			name:           "high risk - breaking, many callers",
			isBreaking:     true,
			directCallers:  15,
			coverage:       CoverageNone,
			hasSideEffects: true,
			exported:       true,
			expectedRange:  [2]float64{0.7, 1.0},
			expectedLevel:  RiskCritical,
		},
		{
			name:           "medium risk - moderate callers, partial coverage, side effects",
			isBreaking:     false,
			directCallers:  8,
			coverage:       CoveragePartial,
			hasSideEffects: true,
			exported:       true,
			expectedRange:  [2]float64{0.3, 0.49},
			expectedLevel:  RiskMedium,
		},
	}

	g, idx := createTestGraph(t)
	analyzer := NewChangeImpactAnalyzer(g, idx)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ChangeImpact{
				IsBreaking:      tt.isBreaking,
				DirectCallers:   tt.directCallers,
				CoverageLevel:   tt.coverage,
				HasSideEffects:  tt.hasSideEffects,
				SideEffectCount: 1,
			}

			var symbol *ast.Symbol
			if tt.exported {
				symbol = &ast.Symbol{Exported: true}
			} else {
				symbol = &ast.Symbol{Exported: false}
			}

			analyzer.calculateRiskScore(symbol, result)

			if result.RiskScore < tt.expectedRange[0] || result.RiskScore > tt.expectedRange[1] {
				t.Errorf("expected risk score in range [%f, %f], got %f",
					tt.expectedRange[0], tt.expectedRange[1], result.RiskScore)
			}

			if result.RiskLevel != tt.expectedLevel {
				t.Errorf("expected risk level %s, got %s", tt.expectedLevel, result.RiskLevel)
			}
		})
	}
}

func TestDefaultAnalyzeOptions(t *testing.T) {
	opts := DefaultAnalyzeOptions()

	if !opts.IncludeBreaking {
		t.Error("expected IncludeBreaking to be true by default")
	}
	if !opts.IncludeBlastRadius {
		t.Error("expected IncludeBlastRadius to be true by default")
	}
	if !opts.IncludeCoverage {
		t.Error("expected IncludeCoverage to be true by default")
	}
	if !opts.IncludeSideEffects {
		t.Error("expected IncludeSideEffects to be true by default")
	}
	if opts.MaxCallers <= 0 {
		t.Error("expected MaxCallers to be positive")
	}
}

func TestQuickAnalyzeOptions(t *testing.T) {
	opts := QuickAnalyzeOptions()

	if opts.IncludeBreaking {
		t.Error("expected IncludeBreaking to be false for quick options")
	}
	if !opts.IncludeBlastRadius {
		t.Error("expected IncludeBlastRadius to be true for quick options")
	}
	if opts.IncludeSideEffects {
		t.Error("expected IncludeSideEffects to be false for quick options")
	}
}

func TestCoverageLevelConstants(t *testing.T) {
	if CoverageGood != "good" {
		t.Errorf("expected CoverageGood to be 'good', got %s", CoverageGood)
	}
	if CoveragePartial != "partial" {
		t.Errorf("expected CoveragePartial to be 'partial', got %s", CoveragePartial)
	}
	if CoverageNone != "none" {
		t.Errorf("expected CoverageNone to be 'none', got %s", CoverageNone)
	}
}

func TestRiskLevelConstants(t *testing.T) {
	if RiskCritical != "CRITICAL" {
		t.Errorf("expected RiskCritical to be 'CRITICAL', got %s", RiskCritical)
	}
	if RiskHigh != "HIGH" {
		t.Errorf("expected RiskHigh to be 'HIGH', got %s", RiskHigh)
	}
	if RiskMedium != "MEDIUM" {
		t.Errorf("expected RiskMedium to be 'MEDIUM', got %s", RiskMedium)
	}
	if RiskLow != "LOW" {
		t.Errorf("expected RiskLow to be 'LOW', got %s", RiskLow)
	}
}
