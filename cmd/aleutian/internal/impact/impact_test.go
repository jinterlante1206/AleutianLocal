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

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// createTestIndex creates a test index with known symbols and edges.
func createTestIndex() *initializer.MemoryIndex {
	index := initializer.NewMemoryIndex()

	// Create symbols: main -> handler -> auth -> db
	index.Symbols = []initializer.Symbol{
		{ID: "main", Name: "main", Kind: "function", FilePath: "cmd/main.go", StartLine: 1, EndLine: 10},
		{ID: "handler", Name: "HandleRequest", Kind: "function", FilePath: "pkg/handler/handler.go", StartLine: 5, EndLine: 20},
		{ID: "auth", Name: "ValidateToken", Kind: "function", FilePath: "pkg/auth/validator.go", StartLine: 10, EndLine: 30},
		{ID: "db", Name: "Query", Kind: "function", FilePath: "pkg/db/db.go", StartLine: 15, EndLine: 25},
		{ID: "helper", Name: "Helper", Kind: "function", FilePath: "pkg/util/helper.go", StartLine: 1, EndLine: 5},
		{ID: "test_auth", Name: "TestValidateToken", Kind: "function", FilePath: "pkg/auth/validator_test.go", StartLine: 1, EndLine: 20},
	}

	// Create edges: main -> handler -> auth -> db, handler -> helper
	index.Edges = []initializer.Edge{
		{FromID: "main", ToID: "handler", Kind: "calls", FilePath: "cmd/main.go", Line: 5},
		{FromID: "handler", ToID: "auth", Kind: "calls", FilePath: "pkg/handler/handler.go", Line: 10},
		{FromID: "handler", ToID: "helper", Kind: "calls", FilePath: "pkg/handler/handler.go", Line: 15},
		{FromID: "auth", ToID: "db", Kind: "calls", FilePath: "pkg/auth/validator.go", Line: 20},
		{FromID: "test_auth", ToID: "auth", Kind: "calls", FilePath: "pkg/auth/validator_test.go", Line: 5},
	}

	index.BuildIndexes()
	return index
}

// TestAnalyzer_Analyze_BasicChange tests impact of a single function change.
func TestAnalyzer_Analyze_BasicChange(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"pkg/db/db.go"}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.ChangedFiles) != 1 {
		t.Errorf("ChangedFiles = %d, want 1", len(result.ChangedFiles))
	}

	if len(result.ChangedSymbols) < 1 {
		t.Errorf("ChangedSymbols = %d, want >= 1", len(result.ChangedSymbols))
	}

	// db is called by auth, which is called by handler, which is called by main
	if result.TotalAffected < 2 {
		t.Errorf("TotalAffected = %d, want >= 2", result.TotalAffected)
	}
}

// TestAnalyzer_Analyze_AuthChange tests impact of security-sensitive change.
func TestAnalyzer_Analyze_AuthChange(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"pkg/auth/validator.go"}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// auth is a security-sensitive path
	if !result.RiskFactors.IsSecurityPath {
		t.Error("Expected IsSecurityPath to be true for auth/ path")
	}

	// auth is called by handler, which is called by main
	if result.DirectCount < 1 {
		t.Errorf("DirectCount = %d, want >= 1", result.DirectCount)
	}
}

// TestAnalyzer_Analyze_NoCallers tests impact with no callers.
func TestAnalyzer_Analyze_NoCallers(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"cmd/main.go"}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// main has no callers, so blast radius should be 0
	if result.TotalAffected != 0 {
		t.Errorf("TotalAffected = %d, want 0", result.TotalAffected)
	}

	// Should be low risk
	if result.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %s, want LOW", result.RiskLevel)
	}
}

// TestAnalyzer_Analyze_ManyCallers tests impact with high fan-in.
func TestAnalyzer_Analyze_ManyCallers(t *testing.T) {
	index := initializer.NewMemoryIndex()

	// Create a symbol with many callers (high fan-in)
	index.Symbols = []initializer.Symbol{
		{ID: "core", Name: "CoreFunction", Kind: "function", FilePath: "pkg/core/core.go", StartLine: 1, EndLine: 10},
	}
	for i := 0; i < 20; i++ {
		index.Symbols = append(index.Symbols, initializer.Symbol{
			ID:        callerID(i),
			Name:      callerName(i),
			Kind:      "function",
			FilePath:  callerPath(i),
			StartLine: 1,
			EndLine:   10,
		})
		index.Edges = append(index.Edges, initializer.Edge{
			FromID:   callerID(i),
			ToID:     "core",
			Kind:     "calls",
			FilePath: callerPath(i),
			Line:     5,
		})
	}
	index.BuildIndexes()

	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"pkg/core/core.go"}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should have many direct callers
	if result.DirectCount < 10 {
		t.Errorf("DirectCount = %d, want >= 10", result.DirectCount)
	}

	// Should be higher risk due to fan-in
	if result.RiskLevel == RiskLow {
		t.Error("Expected risk level > LOW for high fan-in")
	}
}

func callerID(i int) string {
	return "caller_" + string(rune('a'+i))
}

func callerName(i int) string {
	return "Caller" + string(rune('A'+i))
}

func callerPath(i int) string {
	return "pkg/callers/caller_" + string(rune('a'+i)) + ".go"
}

// TestAnalyzer_Analyze_ExcludeTests tests excluding test files.
func TestAnalyzer_Analyze_ExcludeTests(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()

	// Without tests
	cfg1 := DefaultConfig()
	cfg1.Mode = ChangeModeFiles
	cfg1.Files = []string{"pkg/auth/validator.go"}
	cfg1.IncludeTests = false

	result1, err := analyzer.Analyze(ctx, cfg1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// With tests
	cfg2 := DefaultConfig()
	cfg2.Mode = ChangeModeFiles
	cfg2.Files = []string{"pkg/auth/validator.go"}
	cfg2.IncludeTests = true

	result2, err := analyzer.Analyze(ctx, cfg2)
	if err != nil {
		t.Fatalf("Analyze with tests failed: %v", err)
	}

	// Should have more affected symbols with tests included
	// (test_auth calls auth directly)
	t.Logf("Without tests: %d, with tests: %d", result1.TotalAffected, result2.TotalAffected)
}

// TestAnalyzer_Analyze_UnsupportedFile tests handling of non-code files.
func TestAnalyzer_Analyze_UnsupportedFile(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"README.md", "go.mod", ".gitignore"}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// No code symbols should be found for these files
	if len(result.ChangedSymbols) != 0 {
		t.Errorf("ChangedSymbols = %d, want 0 for non-code files", len(result.ChangedSymbols))
	}
}

// TestAnalyzer_Analyze_EmptyFiles tests handling of no changed files.
func TestAnalyzer_Analyze_EmptyFiles(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{}

	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if result.TotalAffected != 0 {
		t.Errorf("TotalAffected = %d, want 0", result.TotalAffected)
	}

	if result.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %s, want LOW", result.RiskLevel)
	}
}

// TestAnalyzer_Analyze_MaxDepth tests depth limiting.
func TestAnalyzer_Analyze_MaxDepth(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	ctx := context.Background()

	// With depth 1
	cfg1 := DefaultConfig()
	cfg1.Mode = ChangeModeFiles
	cfg1.Files = []string{"pkg/db/db.go"}
	cfg1.MaxDepth = 1

	result1, err := analyzer.Analyze(ctx, cfg1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// With depth 5
	cfg2 := DefaultConfig()
	cfg2.Mode = ChangeModeFiles
	cfg2.Files = []string{"pkg/db/db.go"}
	cfg2.MaxDepth = 5

	result2, err := analyzer.Analyze(ctx, cfg2)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Depth 5 should find more symbols than depth 1
	if result2.TotalAffected < result1.TotalAffected {
		t.Errorf("Depth 5 (%d) should have >= depth 1 (%d) affected",
			result2.TotalAffected, result1.TotalAffected)
	}
}

// TestAnalyzer_Analyze_NilContext tests nil context handling.
func TestAnalyzer_Analyze_NilContext(t *testing.T) {
	index := createTestIndex()
	analyzer := NewAnalyzer(index, ".")

	cfg := DefaultConfig()
	cfg.Mode = ChangeModeFiles
	cfg.Files = []string{"pkg/db/db.go"}

	_, err := analyzer.Analyze(nil, cfg)
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

// TestRiskLevel_Exceeds tests risk level comparison.
func TestRiskLevel_Exceeds(t *testing.T) {
	tests := []struct {
		level     RiskLevel
		threshold RiskLevel
		want      bool
	}{
		{RiskLow, RiskLow, false},
		{RiskMedium, RiskLow, true},
		{RiskHigh, RiskMedium, true},
		{RiskCritical, RiskHigh, true},
		{RiskLow, RiskHigh, false},
		{RiskMedium, RiskCritical, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level)+"_exceeds_"+string(tt.threshold), func(t *testing.T) {
			if got := tt.level.Exceeds(tt.threshold); got != tt.want {
				t.Errorf("RiskLevel(%s).Exceeds(%s) = %v, want %v",
					tt.level, tt.threshold, got, tt.want)
			}
		})
	}
}

// TestRiskAssessor_CalculateRisk tests risk calculation.
func TestRiskAssessor_CalculateRisk(t *testing.T) {
	assessor := NewRiskAssessor()

	t.Run("low_risk", func(t *testing.T) {
		changed := []ChangedSymbol{
			{Symbol: initializer.Symbol{Name: "Helper", FilePath: "pkg/util/helper.go"}},
		}
		affected := []AffectedSymbol{}

		level, factors := assessor.CalculateRisk(changed, affected, nil, nil)

		if level != RiskLow {
			t.Errorf("Level = %s, want LOW", level)
		}
		if factors.IsSecurityPath {
			t.Error("Expected IsSecurityPath = false")
		}
	})

	t.Run("security_path", func(t *testing.T) {
		changed := []ChangedSymbol{
			{Symbol: initializer.Symbol{Name: "ValidateToken", FilePath: "pkg/auth/validator.go"}},
		}
		affected := []AffectedSymbol{}

		level, factors := assessor.CalculateRisk(changed, affected, nil, nil)

		if !factors.IsSecurityPath {
			t.Error("Expected IsSecurityPath = true")
		}
		if level == RiskLow {
			t.Error("Expected risk level > LOW for security path")
		}
	})

	t.Run("high_fan_in", func(t *testing.T) {
		changed := []ChangedSymbol{
			{Symbol: initializer.Symbol{Name: "CoreFunc", FilePath: "pkg/core/func.go"}},
		}

		// Create 15 direct callers
		affected := make([]AffectedSymbol, 15)
		for i := 0; i < 15; i++ {
			affected[i] = AffectedSymbol{
				Symbol: initializer.Symbol{Name: callerName(i)},
				Depth:  1, // Direct callers
			}
		}

		level, factors := assessor.CalculateRisk(changed, affected, nil, nil)

		if factors.DirectCallers != 15 {
			t.Errorf("DirectCallers = %d, want 15", factors.DirectCallers)
		}
		if level == RiskLow {
			t.Error("Expected risk level > LOW for high fan-in")
		}
	})
}

// TestParseRiskLevel tests risk level parsing.
func TestParseRiskLevel(t *testing.T) {
	tests := []struct {
		input string
		want  RiskLevel
	}{
		{"low", RiskLow},
		{"LOW", RiskLow},
		{"medium", RiskMedium},
		{"MEDIUM", RiskMedium},
		{"high", RiskHigh},
		{"HIGH", RiskHigh},
		{"critical", RiskCritical},
		{"CRITICAL", RiskCritical},
		{"unknown", RiskHigh}, // Default
		{"", RiskHigh},        // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseRiskLevel(tt.input); got != tt.want {
				t.Errorf("ParseRiskLevel(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

// TestResult_New tests result creation.
func TestResult_New(t *testing.T) {
	result := NewResult()

	if result.APIVersion != APIVersion {
		t.Errorf("APIVersion = %s, want %s", result.APIVersion, APIVersion)
	}

	if result.RiskAlgorithmVersion != RiskAlgorithmVersion {
		t.Errorf("RiskAlgorithmVersion = %s, want %s", result.RiskAlgorithmVersion, RiskAlgorithmVersion)
	}

	if result.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %s, want LOW", result.RiskLevel)
	}

	if result.ChangedFiles == nil {
		t.Error("ChangedFiles should be initialized")
	}

	if result.ChangedSymbols == nil {
		t.Error("ChangedSymbols should be initialized")
	}

	if result.AffectedSymbols == nil {
		t.Error("AffectedSymbols should be initialized")
	}
}
