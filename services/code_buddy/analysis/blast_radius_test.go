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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// =============================================================================
// Test Helpers
// =============================================================================

// createTestSymbol creates a symbol with the given parameters.
func createTestSymbol(id, name, filePath string, startLine int, kind ast.SymbolKind) *ast.Symbol {
	return &ast.Symbol{
		ID:        id,
		Name:      name,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   startLine + 10,
		Kind:      kind,
		Language:  "go",
	}
}

// setupTestGraph creates a Graph and SymbolIndex populated with the given symbols.
func setupTestGraph(symbols []*ast.Symbol, edges [][3]string) (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Add symbols to both graph and index
	for _, sym := range symbols {
		g.AddNode(sym)
		idx.Add(sym)
	}

	// Add edges (caller, target, edgeType)
	for _, edge := range edges {
		callerID := edge[0]
		targetID := edge[1]
		edgeType := edge[2]

		var et graph.EdgeType
		switch edgeType {
		case "calls":
			et = graph.EdgeTypeCalls
		case "implements":
			et = graph.EdgeTypeImplements
		default:
			et = graph.EdgeTypeCalls
		}

		g.AddEdge(callerID, targetID, et, ast.Location{
			FilePath:  "test.go",
			StartLine: 1,
			EndLine:   1,
		})
	}

	g.Freeze()
	return g, idx
}

// =============================================================================
// Test BlastRadiusAnalyzer
// =============================================================================

func TestBlastRadiusAnalyzer_Analyze(t *testing.T) {
	t.Run("basic analysis", func(t *testing.T) {
		// Setup: target function with 3 callers
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/target.go:10:targetFunc", "targetFunc", "pkg/target.go", 10, ast.SymbolKindFunction),
			createTestSymbol("pkg/caller.go:20:caller1", "caller1", "pkg/caller.go", 20, ast.SymbolKindFunction),
			createTestSymbol("pkg/caller.go:30:caller2", "caller2", "pkg/caller.go", 30, ast.SymbolKindFunction),
			createTestSymbol("pkg/other.go:40:caller3", "caller3", "pkg/other.go", 40, ast.SymbolKindFunction),
		}

		edges := [][3]string{
			{"pkg/caller.go:20:caller1", "pkg/target.go:10:targetFunc", "calls"},
			{"pkg/caller.go:30:caller2", "pkg/target.go:10:targetFunc", "calls"},
			{"pkg/other.go:40:caller3", "pkg/target.go:10:targetFunc", "calls"},
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		result, err := analyzer.Analyze(context.Background(), "pkg/target.go:10:targetFunc", nil)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if len(result.DirectCallers) != 3 {
			t.Errorf("Expected 3 direct callers, got %d", len(result.DirectCallers))
		}

		if result.RiskLevel != RiskLow {
			t.Errorf("Expected RiskLow, got %v", result.RiskLevel)
		}
	})

	t.Run("medium risk", func(t *testing.T) {
		// Setup: target with 5 callers (>= 4 = medium)
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/t.go:10:target", "target", "pkg/t.go", 10, ast.SymbolKindFunction),
		}
		edges := [][3]string{}

		for i := 0; i < 5; i++ {
			id := fmt.Sprintf("pkg/c.go:%d:caller%d", i*10, i)
			symbols = append(symbols, createTestSymbol(id, fmt.Sprintf("caller%d", i), "pkg/c.go", i*10, ast.SymbolKindFunction))
			edges = append(edges, [3]string{id, "pkg/t.go:10:target", "calls"})
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		result, err := analyzer.Analyze(context.Background(), "pkg/t.go:10:target", nil)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if result.RiskLevel != RiskMedium {
			t.Errorf("Expected RiskMedium, got %v", result.RiskLevel)
		}
	})

	t.Run("high risk", func(t *testing.T) {
		// Setup: target with 12 callers (>= 10 = high)
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/t.go:10:target", "target", "pkg/t.go", 10, ast.SymbolKindFunction),
		}
		edges := [][3]string{}

		for i := 0; i < 12; i++ {
			id := fmt.Sprintf("pkg/c.go:%d:caller%d", i*10, i)
			symbols = append(symbols, createTestSymbol(id, fmt.Sprintf("caller%d", i), "pkg/c.go", i*10, ast.SymbolKindFunction))
			edges = append(edges, [3]string{id, "pkg/t.go:10:target", "calls"})
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		result, err := analyzer.Analyze(context.Background(), "pkg/t.go:10:target", nil)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if result.RiskLevel != RiskHigh {
			t.Errorf("Expected RiskHigh, got %v", result.RiskLevel)
		}
	})

	t.Run("critical risk for interface", func(t *testing.T) {
		// Setup: interface with implementers
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/iface.go:5:MyInterface", "MyInterface", "pkg/iface.go", 5, ast.SymbolKindInterface),
			createTestSymbol("pkg/impl1.go:10:Impl1", "Impl1", "pkg/impl1.go", 10, ast.SymbolKindStruct),
			createTestSymbol("pkg/impl2.go:10:Impl2", "Impl2", "pkg/impl2.go", 10, ast.SymbolKindStruct),
		}

		edges := [][3]string{
			{"pkg/impl1.go:10:Impl1", "pkg/iface.go:5:MyInterface", "implements"},
			{"pkg/impl2.go:10:Impl2", "pkg/iface.go:5:MyInterface", "implements"},
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		result, err := analyzer.Analyze(context.Background(), "pkg/iface.go:5:MyInterface", nil)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if result.RiskLevel != RiskCritical {
			t.Errorf("Expected RiskCritical for interface, got %v", result.RiskLevel)
		}

		if len(result.Implementers) != 2 {
			t.Errorf("Expected 2 implementers, got %d", len(result.Implementers))
		}
	})

	t.Run("indirect callers", func(t *testing.T) {
		// Setup: A -> B -> C (C is target, B is direct, A is indirect)
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/c.go:10:C", "C", "pkg/c.go", 10, ast.SymbolKindFunction),
			createTestSymbol("pkg/b.go:10:B", "B", "pkg/b.go", 10, ast.SymbolKindFunction),
			createTestSymbol("pkg/a.go:10:A", "A", "pkg/a.go", 10, ast.SymbolKindFunction),
		}

		edges := [][3]string{
			{"pkg/b.go:10:B", "pkg/c.go:10:C", "calls"},
			{"pkg/a.go:10:A", "pkg/b.go:10:B", "calls"},
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		opts := DefaultAnalyzeOptions()
		opts.MaxHops = 2
		result, err := analyzer.Analyze(context.Background(), "pkg/c.go:10:C", &opts)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if len(result.DirectCallers) != 1 {
			t.Errorf("Expected 1 direct caller, got %d", len(result.DirectCallers))
		}
		if len(result.DirectCallers) > 0 && result.DirectCallers[0].Name != "B" {
			t.Errorf("Expected direct caller 'B', got %s", result.DirectCallers[0].Name)
		}

		if len(result.IndirectCallers) != 1 {
			t.Errorf("Expected 1 indirect caller, got %d", len(result.IndirectCallers))
		}
		if len(result.IndirectCallers) > 0 {
			if result.IndirectCallers[0].Name != "A" {
				t.Errorf("Expected indirect caller 'A', got %s", result.IndirectCallers[0].Name)
			}
			if result.IndirectCallers[0].Hops != 2 {
				t.Errorf("Expected indirect caller at 2 hops, got %d", result.IndirectCallers[0].Hops)
			}
		}
	})

	t.Run("truncation on limit", func(t *testing.T) {
		// Setup: target with 200 callers
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/t.go:10:target", "target", "pkg/t.go", 10, ast.SymbolKindFunction),
		}
		edges := [][3]string{}

		for i := 0; i < 200; i++ {
			id := fmt.Sprintf("pkg/c.go:%d:caller%d", i, i)
			symbols = append(symbols, createTestSymbol(id, fmt.Sprintf("caller%d", i), "pkg/c.go", i, ast.SymbolKindFunction))
			edges = append(edges, [3]string{id, "pkg/t.go:10:target", "calls"})
		}

		g, idx := setupTestGraph(symbols, edges)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		opts := DefaultAnalyzeOptions()
		opts.MaxDirectCallers = 50
		result, err := analyzer.Analyze(context.Background(), "pkg/t.go:10:target", &opts)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if len(result.DirectCallers) != 50 {
			t.Errorf("Expected 50 direct callers (limit), got %d", len(result.DirectCallers))
		}

		if !result.Truncated {
			t.Error("Expected Truncated to be true")
		}
	})

	t.Run("timeout handling", func(t *testing.T) {
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/t.go:10:target", "target", "pkg/t.go", 10, ast.SymbolKindFunction),
		}

		g, idx := setupTestGraph(symbols, nil)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		// Use very short timeout
		opts := DefaultAnalyzeOptions()
		opts.Timeout = 1 * time.Nanosecond

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		result, err := analyzer.Analyze(ctx, "pkg/t.go:10:target", &opts)
		if err != nil {
			t.Fatalf("Analyze should not error on timeout: %v", err)
		}

		// Result should still be returned (possibly truncated)
		if result == nil {
			t.Error("Expected non-nil result even on timeout")
		}
	})

	t.Run("no callers returns low risk", func(t *testing.T) {
		symbols := []*ast.Symbol{
			createTestSymbol("pkg/t.go:10:orphan", "orphan", "pkg/t.go", 10, ast.SymbolKindFunction),
		}

		g, idx := setupTestGraph(symbols, nil)

		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)

		result, err := analyzer.Analyze(context.Background(), "pkg/t.go:10:orphan", nil)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}

		if result.RiskLevel != RiskLow {
			t.Errorf("Expected RiskLow for orphan function, got %v", result.RiskLevel)
		}

		if len(result.DirectCallers) != 0 {
			t.Errorf("Expected 0 direct callers, got %d", len(result.DirectCallers))
		}
	})
}

func TestRiskLevel(t *testing.T) {
	tests := []struct {
		directCount int
		expected    RiskLevel
	}{
		{0, RiskLow},
		{3, RiskLow},
		{4, RiskMedium},
		{9, RiskMedium},
		{10, RiskHigh},
		{19, RiskHigh},
		{20, RiskCritical},
		{100, RiskCritical},
	}

	config := DefaultRiskConfig()
	analyzer := &BlastRadiusAnalyzer{riskConfig: config}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_callers", tt.directCount), func(t *testing.T) {
			result := &BlastRadius{
				DirectCallers: make([]Caller, tt.directCount),
			}
			risk := analyzer.calculateRiskLevel(result)
			if risk != tt.expected {
				t.Errorf("For %d callers, expected %v, got %v", tt.directCount, tt.expected, risk)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultAnalyzeOptions()

	if opts.MaxDirectCallers != 100 {
		t.Errorf("Expected MaxDirectCallers 100, got %d", opts.MaxDirectCallers)
	}
	if opts.MaxIndirectCallers != 500 {
		t.Errorf("Expected MaxIndirectCallers 500, got %d", opts.MaxIndirectCallers)
	}
	if opts.MaxHops != 3 {
		t.Errorf("Expected MaxHops 3, got %d", opts.MaxHops)
	}
	if opts.Timeout != 500*time.Millisecond {
		t.Errorf("Expected Timeout 500ms, got %v", opts.Timeout)
	}
}

func TestDefaultRiskConfig(t *testing.T) {
	config := DefaultRiskConfig()

	if config.CriticalThreshold != 20 {
		t.Errorf("Expected CriticalThreshold 20, got %d", config.CriticalThreshold)
	}
	if config.HighThreshold != 10 {
		t.Errorf("Expected HighThreshold 10, got %d", config.HighThreshold)
	}
	if config.MediumThreshold != 4 {
		t.Errorf("Expected MediumThreshold 4, got %d", config.MediumThreshold)
	}
}

func TestRiskLevelString(t *testing.T) {
	tests := []struct {
		level    RiskLevel
		expected string
	}{
		{RiskLow, "LOW"},
		{RiskMedium, "MEDIUM"},
		{RiskHigh, "HIGH"},
		{RiskCritical, "CRITICAL"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.level) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.level)
			}
		})
	}
}

func TestBlastRadiusAnalyzer_NewBlastRadiusAnalyzer(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	t.Run("with nil config uses defaults", func(t *testing.T) {
		analyzer := NewBlastRadiusAnalyzer(g, idx, nil)
		if analyzer == nil {
			t.Fatal("Expected non-nil analyzer")
		}
		if analyzer.riskConfig.CriticalThreshold != 20 {
			t.Errorf("Expected default critical threshold 20, got %d", analyzer.riskConfig.CriticalThreshold)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		customConfig := &RiskConfig{
			CriticalThreshold: 50,
			HighThreshold:     25,
			MediumThreshold:   10,
		}
		analyzer := NewBlastRadiusAnalyzer(g, idx, customConfig)
		if analyzer == nil {
			t.Fatal("Expected non-nil analyzer")
		}
		if analyzer.riskConfig.CriticalThreshold != 50 {
			t.Errorf("Expected custom critical threshold 50, got %d", analyzer.riskConfig.CriticalThreshold)
		}
	})
}

func TestFindTestFiles(t *testing.T) {
	analyzer := &BlastRadiusAnalyzer{}

	t.Run("finds test files for go files", func(t *testing.T) {
		affectedFiles := []string{
			"pkg/auth/handler.go",
			"pkg/user/service.go",
		}
		patterns := []string{"*_test.go"}

		testFiles := analyzer.findTestFiles(affectedFiles, patterns)

		if len(testFiles) != 2 {
			t.Errorf("Expected 2 test files, got %d", len(testFiles))
		}

		expected := map[string]bool{
			"pkg/auth/handler_test.go": true,
			"pkg/user/service_test.go": true,
		}

		for _, tf := range testFiles {
			if !expected[tf] {
				t.Errorf("Unexpected test file: %s", tf)
			}
		}
	})

	t.Run("empty patterns returns empty", func(t *testing.T) {
		affectedFiles := []string{"pkg/auth/handler.go"}
		patterns := []string{}

		testFiles := analyzer.findTestFiles(affectedFiles, patterns)

		if len(testFiles) != 0 {
			t.Errorf("Expected 0 test files, got %d", len(testFiles))
		}
	})

	t.Run("deduplicates test files", func(t *testing.T) {
		affectedFiles := []string{
			"pkg/auth/handler.go",
			"pkg/auth/handler.go", // duplicate
		}
		patterns := []string{"*_test.go"}

		testFiles := analyzer.findTestFiles(affectedFiles, patterns)

		if len(testFiles) != 1 {
			t.Errorf("Expected 1 test file (deduplicated), got %d", len(testFiles))
		}
	})
}

func TestGenerateSummary(t *testing.T) {
	analyzer := &BlastRadiusAnalyzer{}

	t.Run("basic summary", func(t *testing.T) {
		result := &BlastRadius{
			RiskLevel:     RiskMedium,
			DirectCallers: make([]Caller, 5),
			FilesAffected: []string{"file1.go", "file2.go"},
		}

		summary := analyzer.generateSummary(result)

		if summary == "" {
			t.Error("Expected non-empty summary")
		}
		if !contains(summary, "Risk Level: MEDIUM") {
			t.Errorf("Expected summary to contain risk level, got: %s", summary)
		}
		if !contains(summary, "Direct callers: 5") {
			t.Errorf("Expected summary to contain caller count, got: %s", summary)
		}
	})

	t.Run("summary with indirect callers", func(t *testing.T) {
		result := &BlastRadius{
			RiskLevel:       RiskHigh,
			DirectCallers:   make([]Caller, 10),
			IndirectCallers: make([]Caller, 25),
			FilesAffected:   []string{"file1.go"},
		}

		summary := analyzer.generateSummary(result)

		if !contains(summary, "Indirect callers: 25") {
			t.Errorf("Expected summary to contain indirect callers, got: %s", summary)
		}
	})

	t.Run("summary with truncation", func(t *testing.T) {
		result := &BlastRadius{
			RiskLevel:       RiskCritical,
			DirectCallers:   make([]Caller, 50),
			FilesAffected:   []string{"file1.go"},
			Truncated:       true,
			TruncatedReason: "limit exceeded",
		}

		summary := analyzer.generateSummary(result)

		if !contains(summary, "Truncated") {
			t.Errorf("Expected summary to indicate truncation, got: %s", summary)
		}
	})
}

func TestGenerateRecommendation(t *testing.T) {
	analyzer := &BlastRadiusAnalyzer{}

	t.Run("critical recommendation for interface", func(t *testing.T) {
		result := &BlastRadius{
			RiskLevel:    RiskCritical,
			Implementers: make([]Implementer, 5),
		}

		rec := analyzer.generateRecommendation(result)

		if !contains(rec, "CRITICAL") {
			t.Errorf("Expected critical recommendation, got: %s", rec)
		}
		if !contains(rec, "interface") {
			t.Errorf("Expected interface mention, got: %s", rec)
		}
	})

	t.Run("low risk recommendation with no callers", func(t *testing.T) {
		result := &BlastRadius{
			RiskLevel:     RiskLow,
			DirectCallers: make([]Caller, 0),
		}

		rec := analyzer.generateRecommendation(result)

		if !contains(rec, "LOW RISK") {
			t.Errorf("Expected low risk recommendation, got: %s", rec)
		}
		if !contains(rec, "No callers") {
			t.Errorf("Expected no callers mention, got: %s", rec)
		}
	})
}

// contains checks if substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
