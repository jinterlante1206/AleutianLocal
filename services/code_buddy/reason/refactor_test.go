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
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

func setupRefactorTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Long function (100+ lines)
	longFunc := &ast.Symbol{
		ID:        "handlers/process.go:10:ProcessData",
		Name:      "ProcessData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/process.go",
		StartLine: 10,
		EndLine:   150, // 140 lines
		Package:   "handlers",
		Signature: "func(ctx context.Context, data *Data) error",
		Language:  "go",
		Exported:  true,
	}

	// Function with too many parameters
	manyParams := &ast.Symbol{
		ID:        "handlers/config.go:20:Configure",
		Name:      "Configure",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/config.go",
		StartLine: 20,
		EndLine:   40,
		Package:   "handlers",
		Signature: "func(host string, port int, user string, pass string, db string, timeout int, maxConn int) error",
		Language:  "go",
		Exported:  true,
	}

	// Small healthy function
	healthyFunc := &ast.Symbol{
		ID:        "utils/math.go:10:Add",
		Name:      "Add",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "utils/math.go",
		StartLine: 10,
		EndLine:   15,
		Package:   "utils",
		Signature: "func(a, b int) int",
		Language:  "go",
		Exported:  true,
	}

	// Unclear name
	unclearName := &ast.Symbol{
		ID:        "handlers/data.go:10:Do",
		Name:      "Do",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/data.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(x interface{}) interface{}",
		Language:  "go",
		Exported:  true,
	}

	// Abbreviation name
	abbrName := &ast.Symbol{
		ID:        "handlers/proc.go:10:PrcDt",
		Name:      "PrcDt",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/proc.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "handlers",
		Signature: "func(dt []byte) error",
		Language:  "go",
		Exported:  true,
	}

	// Unused exported function (potential dead code)
	unusedFunc := &ast.Symbol{
		ID:        "handlers/legacy.go:10:OldHandler",
		Name:      "OldHandler",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/legacy.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(req *Request) error",
		Language:  "go",
		Exported:  true,
	}

	// Heavily used function (many callers)
	popularFunc := &ast.Symbol{
		ID:        "utils/validate.go:10:Validate",
		Name:      "Validate",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "utils/validate.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "utils",
		Signature: "func(data interface{}) error",
		Language:  "go",
		Exported:  true,
	}

	// Callers of popular function
	callers := make([]*ast.Symbol, 10)
	for i := 0; i < 10; i++ {
		callers[i] = &ast.Symbol{
			ID:        "handlers/caller" + intToString(i) + ".go:10:Handler" + intToString(i),
			Name:      "Handler" + intToString(i),
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/caller" + intToString(i) + ".go",
			StartLine: 10,
			EndLine:   25,
			Package:   "handlers",
			Signature: "func() error",
			Language:  "go",
			Exported:  true,
		}
	}

	// Add to graph and index
	for _, sym := range []*ast.Symbol{longFunc, manyParams, healthyFunc, unclearName, abbrName, unusedFunc, popularFunc} {
		g.AddNode(sym)
		idx.Add(sym)
	}

	for _, caller := range callers {
		g.AddNode(caller)
		idx.Add(caller)

		// Each caller calls the popular function
		g.AddEdge(caller.ID, popularFunc.ID, graph.EdgeTypeCalls, ast.Location{
			FilePath:  caller.FilePath,
			StartLine: 15,
		})
	}

	g.Freeze()
	return g, idx
}

func TestRefactorSuggester_SuggestRefactor(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	t.Run("suggests split for long function", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "handlers/process.go:10:ProcessData")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check metrics
		if suggestions.Metrics.LineCount < 100 {
			t.Errorf("expected line count > 100, got %d", suggestions.Metrics.LineCount)
		}

		// Check for split suggestion
		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeSplitFunction {
				found = true
				if s.Confidence <= 0 || s.Confidence > 1 {
					t.Errorf("invalid confidence: %f", s.Confidence)
				}
				break
			}
		}
		if !found {
			t.Error("expected split function suggestion")
		}

		// Health should be low
		if suggestions.OverallHealth > 50 {
			t.Errorf("expected low health for long function, got %d", suggestions.OverallHealth)
		}
	})

	t.Run("suggests reduce parameters for many params", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "handlers/config.go:20:Configure")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check metrics
		if suggestions.Metrics.ParameterCount < 5 {
			t.Errorf("expected parameter count > 5, got %d", suggestions.Metrics.ParameterCount)
		}

		// Check for reduce parameters suggestion
		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeReduceParameters {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected reduce parameters suggestion")
		}
	})

	t.Run("healthy function has high health score", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "utils/math.go:10:Add")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if suggestions.OverallHealth < 80 {
			t.Errorf("expected high health for simple function, got %d", suggestions.OverallHealth)
		}

		// Should have few or no suggestions
		if len(suggestions.Suggestions) > 1 {
			t.Errorf("expected few suggestions for healthy function, got %d", len(suggestions.Suggestions))
		}
	})

	t.Run("suggests rename for unclear name", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "handlers/data.go:10:Do")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeRename {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected rename suggestion for unclear name")
		}
	})

	t.Run("suggests rename for abbreviation", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "handlers/proc.go:10:PrcDt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeRename {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected rename suggestion for abbreviation")
		}
	})

	t.Run("suggests interface for popular function", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "utils/validate.go:10:Validate")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check caller count
		if suggestions.Metrics.CallerCount < 5 {
			t.Errorf("expected many callers, got %d", suggestions.Metrics.CallerCount)
		}

		// Check for interface extraction suggestion
		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeExtractInterface {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected extract interface suggestion for popular function")
		}
	})

	t.Run("suggests remove dead code for unused function", func(t *testing.T) {
		suggestions, err := suggester.SuggestRefactor(ctx, "handlers/legacy.go:10:OldHandler")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check for dead code suggestion
		found := false
		for _, s := range suggestions.Suggestions {
			if s.Type == RefactorTypeRemoveDeadCode {
				found = true
				// Should have low confidence for exported functions
				if s.Confidence > 0.6 {
					t.Errorf("expected low confidence for exported dead code, got %f", s.Confidence)
				}
				break
			}
		}
		if !found {
			t.Error("expected remove dead code suggestion")
		}
	})
}

func TestRefactorSuggester_Errors(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := suggester.SuggestRefactor(nil, "utils/math.go:10:Add")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target ID", func(t *testing.T) {
		_, err := suggester.SuggestRefactor(ctx, "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("non-existent symbol", func(t *testing.T) {
		_, err := suggester.SuggestRefactor(ctx, "nonexistent")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := suggester.SuggestRefactor(cancelCtx, "utils/math.go:10:Add")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestRefactorSuggester_SuggestRefactorBatch(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	targets := []string{
		"handlers/process.go:10:ProcessData",
		"utils/math.go:10:Add",
		"handlers/config.go:20:Configure",
	}

	results, err := suggester.SuggestRefactorBatch(ctx, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Long function should have low health
	if longResult, ok := results["handlers/process.go:10:ProcessData"]; ok {
		if longResult.OverallHealth > 50 {
			t.Errorf("expected low health for long function, got %d", longResult.OverallHealth)
		}
	} else {
		t.Error("missing result for long function")
	}

	// Healthy function should have high health
	if healthyResult, ok := results["utils/math.go:10:Add"]; ok {
		if healthyResult.OverallHealth < 80 {
			t.Errorf("expected high health for healthy function, got %d", healthyResult.OverallHealth)
		}
	} else {
		t.Error("missing result for healthy function")
	}
}

func TestRefactorSuggester_CalculateMetricsOnly(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	t.Run("returns metrics without suggestions", func(t *testing.T) {
		metrics, err := suggester.CalculateMetricsOnly(ctx, "handlers/process.go:10:ProcessData")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if metrics.LineCount < 100 {
			t.Errorf("expected line count > 100, got %d", metrics.LineCount)
		}

		if !metrics.IsExported {
			t.Error("expected function to be exported")
		}
	})

	t.Run("errors on invalid input", func(t *testing.T) {
		_, err := suggester.CalculateMetricsOnly(ctx, "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestCodeMetrics_Calculations(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	t.Run("line count from symbol", func(t *testing.T) {
		metrics, _ := suggester.CalculateMetricsOnly(ctx, "handlers/process.go:10:ProcessData")
		// EndLine 150 - StartLine 10 + 1 = 141
		if metrics.LineCount != 141 {
			t.Errorf("expected 141 lines, got %d", metrics.LineCount)
		}
	})

	t.Run("parameter count from signature", func(t *testing.T) {
		metrics, _ := suggester.CalculateMetricsOnly(ctx, "handlers/config.go:20:Configure")
		if metrics.ParameterCount < 5 {
			t.Errorf("expected > 5 parameters, got %d", metrics.ParameterCount)
		}
	})

	t.Run("caller count from graph", func(t *testing.T) {
		metrics, _ := suggester.CalculateMetricsOnly(ctx, "utils/validate.go:10:Validate")
		if metrics.CallerCount != 10 {
			t.Errorf("expected 10 callers, got %d", metrics.CallerCount)
		}
	})
}

func TestSuggestion_RiskLevels(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	suggestions, err := suggester.SuggestRefactor(ctx, "handlers/process.go:10:ProcessData")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that all suggestions have risk levels
	for _, s := range suggestions.Suggestions {
		if s.RiskLevel != RiskLevelLow && s.RiskLevel != RiskLevelMedium && s.RiskLevel != RiskLevelHigh {
			t.Errorf("suggestion %s has invalid risk level: %s", s.Type, s.RiskLevel)
		}

		if s.Risk == "" {
			t.Errorf("suggestion %s has no risk description", s.Type)
		}
	}
}

func TestSuggestion_Priority(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	// Get suggestions for a function with multiple issues
	suggestions, err := suggester.SuggestRefactor(ctx, "handlers/process.go:10:ProcessData")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(suggestions.Suggestions) < 2 {
		t.Skip("need at least 2 suggestions to test priority")
	}

	// Check that suggestions are sorted by priority
	for i := 1; i < len(suggestions.Suggestions); i++ {
		if suggestions.Suggestions[i].Priority < suggestions.Suggestions[i-1].Priority {
			t.Errorf("suggestions not sorted by priority: %d before %d",
				suggestions.Suggestions[i-1].Priority, suggestions.Suggestions[i].Priority)
		}
	}
}

func TestOverallHealth_Calculation(t *testing.T) {
	g, idx := setupRefactorTestGraph()
	suggester := NewRefactorSuggester(g, idx)
	ctx := context.Background()

	tests := []struct {
		targetID    string
		minHealth   int
		maxHealth   int
		description string
	}{
		{
			"utils/math.go:10:Add",
			80, 100,
			"simple function should be healthy",
		},
		{
			"handlers/process.go:10:ProcessData",
			0, 50,
			"long function should be unhealthy",
		},
		{
			"handlers/config.go:20:Configure",
			70, 95,
			"many parameters should reduce health somewhat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			suggestions, err := suggester.SuggestRefactor(ctx, tt.targetID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if suggestions.OverallHealth < tt.minHealth || suggestions.OverallHealth > tt.maxHealth {
				t.Errorf("health %d not in expected range [%d, %d]",
					suggestions.OverallHealth, tt.minHealth, tt.maxHealth)
			}
		})
	}
}
