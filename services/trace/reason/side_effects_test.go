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

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

func setupSideEffectTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Handler that does file I/O
	fileHandler := &ast.Symbol{
		ID:        "handlers/file.go:10:WriteFile",
		Name:      "WriteFile",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/file.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "handlers",
		Signature: "func(path string, data []byte) error",
		Language:  "go",
		Exported:  true,
	}

	// os.WriteFile (stdlib function with side effect)
	osWriteFile := &ast.Symbol{
		ID:        "os:WriteFile",
		Name:      "WriteFile",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "os/file.go",
		StartLine: 1,
		EndLine:   10,
		Package:   "os",
		Signature: "func(name string, data []byte, perm FileMode) error",
		Language:  "go",
		Exported:  true,
	}

	// Handler that calls HTTP
	httpHandler := &ast.Symbol{
		ID:        "handlers/api.go:20:FetchData",
		Name:      "FetchData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/api.go",
		StartLine: 20,
		EndLine:   40,
		Package:   "handlers",
		Signature: "func(url string) ([]byte, error)",
		Language:  "go",
		Exported:  true,
	}

	// http.Get (stdlib function with side effect)
	httpGet := &ast.Symbol{
		ID:        "net/http:Get",
		Name:      "Get",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "net/http/client.go",
		StartLine: 1,
		EndLine:   10,
		Package:   "net/http",
		Signature: "func(url string) (*Response, error)",
		Language:  "go",
		Exported:  true,
	}

	// Pure function (no side effects)
	pureFunc := &ast.Symbol{
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

	// Function that calls another function with side effects (transitive)
	orchestrator := &ast.Symbol{
		ID:        "handlers/orchestrator.go:10:ProcessAll",
		Name:      "ProcessAll",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/orchestrator.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(ctx context.Context) error",
		Language:  "go",
		Exported:  true,
	}

	// Database handler
	dbHandler := &ast.Symbol{
		ID:        "handlers/db.go:10:SaveUser",
		Name:      "SaveUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/db.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "handlers",
		Signature: "func(user *User) error",
		Language:  "go",
		Exported:  true,
	}

	// sql.Exec
	sqlExec := &ast.Symbol{
		ID:        "database/sql:Exec",
		Name:      "Exec",
		Kind:      ast.SymbolKindMethod,
		FilePath:  "database/sql/sql.go",
		StartLine: 1,
		EndLine:   10,
		Package:   "database/sql",
		Signature: "func(query string, args ...interface{}) (Result, error)",
		Language:  "go",
		Exported:  true,
	}

	// Add to index
	for _, sym := range []*ast.Symbol{fileHandler, osWriteFile, httpHandler, httpGet, pureFunc, orchestrator, dbHandler, sqlExec} {
		g.AddNode(sym)
		idx.Add(sym)
	}

	// Add edges
	g.AddEdge(fileHandler.ID, osWriteFile.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  fileHandler.FilePath,
		StartLine: 15,
	})

	g.AddEdge(httpHandler.ID, httpGet.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  httpHandler.FilePath,
		StartLine: 25,
	})

	g.AddEdge(dbHandler.ID, sqlExec.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  dbHandler.FilePath,
		StartLine: 18,
	})

	// orchestrator calls fileHandler (transitive effect)
	g.AddEdge(orchestrator.ID, fileHandler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  orchestrator.FilePath,
		StartLine: 15,
	})

	// orchestrator calls httpHandler (transitive effect)
	g.AddEdge(orchestrator.ID, httpHandler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  orchestrator.FilePath,
		StartLine: 20,
	})

	g.Freeze()
	return g, idx
}

func TestSideEffectAnalyzer_FindSideEffects(t *testing.T) {
	g, idx := setupSideEffectTestGraph()
	analyzer := NewSideEffectAnalyzer(g, idx)
	ctx := context.Background()

	t.Run("detects file IO side effects", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "handlers/file.go:10:WriteFile")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.IsPure {
			t.Error("expected function to not be pure")
		}

		if analysis.DirectEffects == 0 {
			t.Error("expected direct side effects")
		}

		// Check for file IO effect
		found := false
		for _, effect := range analysis.SideEffects {
			if effect.Type == SideEffectTypeFileIO {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected file IO side effect")
		}
	})

	t.Run("detects network side effects", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "handlers/api.go:20:FetchData")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.IsPure {
			t.Error("expected function to not be pure")
		}

		// Check for network effect
		found := false
		for _, effect := range analysis.SideEffects {
			if effect.Type == SideEffectTypeNetwork {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected network side effect")
		}
	})

	t.Run("detects database side effects", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "handlers/db.go:10:SaveUser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.IsPure {
			t.Error("expected function to not be pure")
		}

		// Check for database effect
		found := false
		for _, effect := range analysis.SideEffects {
			if effect.Type == SideEffectTypeDatabase {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected database side effect")
		}
	})

	t.Run("pure function has no side effects", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "utils/math.go:10:Add")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !analysis.IsPure {
			t.Error("expected function to be pure")
		}

		if len(analysis.SideEffects) != 0 {
			t.Errorf("expected no side effects, got %d", len(analysis.SideEffects))
		}
	})

	t.Run("detects transitive side effects", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "handlers/orchestrator.go:10:ProcessAll")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.IsPure {
			t.Error("expected function to not be pure")
		}

		if analysis.TransitiveEffects == 0 {
			t.Error("expected transitive side effects")
		}

		// Check that transitive effects have call chains
		for _, effect := range analysis.SideEffects {
			if effect.Transitive && len(effect.CallChain) == 0 {
				t.Error("transitive effect should have call chain")
			}
		}
	})

	t.Run("has confidence score", func(t *testing.T) {
		analysis, err := analyzer.FindSideEffects(ctx, "handlers/file.go:10:WriteFile")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.Confidence <= 0 || analysis.Confidence > 1 {
			t.Errorf("confidence should be between 0 and 1, got %f", analysis.Confidence)
		}
	})
}

func TestSideEffectAnalyzer_Errors(t *testing.T) {
	g, idx := setupSideEffectTestGraph()
	analyzer := NewSideEffectAnalyzer(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := analyzer.FindSideEffects(nil, "handlers/file.go:10:WriteFile")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target ID", func(t *testing.T) {
		_, err := analyzer.FindSideEffects(ctx, "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("non-existent symbol", func(t *testing.T) {
		_, err := analyzer.FindSideEffects(ctx, "nonexistent")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := analyzer.FindSideEffects(cancelCtx, "handlers/file.go:10:WriteFile")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestSideEffectAnalyzer_FindSideEffectsBatch(t *testing.T) {
	g, idx := setupSideEffectTestGraph()
	analyzer := NewSideEffectAnalyzer(g, idx)
	ctx := context.Background()

	targets := []string{
		"handlers/file.go:10:WriteFile",
		"handlers/api.go:20:FetchData",
		"utils/math.go:10:Add",
	}

	results, err := analyzer.FindSideEffectsBatch(ctx, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Check that file handler is not pure
	if fileResult, ok := results["handlers/file.go:10:WriteFile"]; ok {
		if fileResult.IsPure {
			t.Error("file handler should not be pure")
		}
	} else {
		t.Error("missing result for file handler")
	}

	// Check that pure function is pure
	if pureResult, ok := results["utils/math.go:10:Add"]; ok {
		if !pureResult.IsPure {
			t.Error("pure function should be pure")
		}
	} else {
		t.Error("missing result for pure function")
	}
}

func TestSideEffectPatterns(t *testing.T) {
	t.Run("Go patterns exist", func(t *testing.T) {
		patterns := GetPatternsForLanguage("go")
		if patterns == nil {
			t.Fatal("expected Go patterns")
		}

		if len(patterns.FileIO) == 0 {
			t.Error("expected file IO patterns")
		}
		if len(patterns.Network) == 0 {
			t.Error("expected network patterns")
		}
		if len(patterns.Database) == 0 {
			t.Error("expected database patterns")
		}
	})

	t.Run("Python patterns exist", func(t *testing.T) {
		patterns := GetPatternsForLanguage("python")
		if patterns == nil {
			t.Fatal("expected Python patterns")
		}

		if len(patterns.FileIO) == 0 {
			t.Error("expected file IO patterns")
		}
		if len(patterns.Network) == 0 {
			t.Error("expected network patterns")
		}
	})

	t.Run("TypeScript patterns exist", func(t *testing.T) {
		patterns := GetPatternsForLanguage("typescript")
		if patterns == nil {
			t.Fatal("expected TypeScript patterns")
		}

		if len(patterns.FileIO) == 0 {
			t.Error("expected file IO patterns")
		}
		if len(patterns.Network) == 0 {
			t.Error("expected network patterns")
		}
	})

	t.Run("unsupported language returns nil", func(t *testing.T) {
		patterns := GetPatternsForLanguage("rust")
		if patterns != nil {
			t.Error("expected nil for unsupported language")
		}
	})

	t.Run("GetAllPatterns returns all patterns", func(t *testing.T) {
		patterns := GetPatternsForLanguage("go")
		all := patterns.GetAllPatterns()

		if len(all) == 0 {
			t.Error("expected patterns")
		}

		// Should include patterns from multiple categories
		categories := make(map[SideEffectType]bool)
		for _, p := range all {
			categories[p.EffectType] = true
		}

		if len(categories) < 3 {
			t.Error("expected patterns from multiple categories")
		}
	})
}

func TestSummarySideEffects(t *testing.T) {
	analysis := &SideEffectAnalysis{
		SideEffects: []SideEffect{
			{Type: SideEffectTypeFileIO, Operation: "WriteFile"},
			{Type: SideEffectTypeFileIO, Operation: "ReadFile"},
			{Type: SideEffectTypeNetwork, Operation: "Get"},
			{Type: SideEffectTypeDatabase, Operation: "Exec"},
		},
	}

	summary := SummarySideEffects(analysis)

	if len(summary[SideEffectTypeFileIO]) != 2 {
		t.Errorf("expected 2 file IO effects, got %d", len(summary[SideEffectTypeFileIO]))
	}

	if len(summary[SideEffectTypeNetwork]) != 1 {
		t.Errorf("expected 1 network effect, got %d", len(summary[SideEffectTypeNetwork]))
	}

	if len(summary[SideEffectTypeDatabase]) != 1 {
		t.Errorf("expected 1 database effect, got %d", len(summary[SideEffectTypeDatabase]))
	}
}
