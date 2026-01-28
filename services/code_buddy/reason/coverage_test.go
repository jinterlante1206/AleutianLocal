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

func setupCoverageTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a handler function (production code)
	handler := &ast.Symbol{
		ID:        "handlers/user.go:10:HandleUser",
		Name:      "HandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(ctx context.Context, req *UserRequest) (*UserResponse, error)",
		Language:  "go",
		Exported:  true,
	}

	// Helper function called by handler
	helper := &ast.Symbol{
		ID:        "handlers/user.go:50:validateUser",
		Name:      "validateUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user.go",
		StartLine: 50,
		EndLine:   70,
		Package:   "handlers",
		Signature: "func(user *User) error",
		Language:  "go",
		Exported:  false,
	}

	// Uncovered function
	uncovered := &ast.Symbol{
		ID:        "handlers/admin.go:20:AdminHandler",
		Name:      "AdminHandler",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/admin.go",
		StartLine: 20,
		EndLine:   40,
		Package:   "handlers",
		Signature: "func() error",
		Language:  "go",
		Exported:  true,
	}

	// Direct test for handler
	testDirect := &ast.Symbol{
		ID:        "handlers/user_test.go:10:TestHandleUser",
		Name:      "TestHandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user_test.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(t *testing.T)",
		Language:  "go",
	}

	// Integration test that indirectly covers helper
	testIndirect := &ast.Symbol{
		ID:        "handlers/integration_test.go:20:TestIntegration",
		Name:      "TestIntegration",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/integration_test.go",
		StartLine: 20,
		EndLine:   50,
		Package:   "handlers",
		Signature: "func(t *testing.T)",
		Language:  "go",
	}

	// Add to graph
	g.AddNode(handler)
	g.AddNode(helper)
	g.AddNode(uncovered)
	g.AddNode(testDirect)
	g.AddNode(testIndirect)

	// Call edges:
	// testDirect -> handler (direct call)
	g.AddEdge(testDirect.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  testDirect.FilePath,
		StartLine: 15,
	})

	// handler -> helper (production call)
	g.AddEdge(handler.ID, helper.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  handler.FilePath,
		StartLine: 20,
	})

	// testIndirect -> handler -> helper (indirect coverage)
	g.AddEdge(testIndirect.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  testIndirect.FilePath,
		StartLine: 25,
	})

	g.Freeze()

	// Add to index
	idx.Add(handler)
	idx.Add(helper)
	idx.Add(uncovered)
	idx.Add(testDirect)
	idx.Add(testIndirect)

	return g, idx
}

func TestTestCoverageFinder_FindTestCoverage(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	t.Run("finds direct test coverage", func(t *testing.T) {
		coverage, err := finder.FindTestCoverage(ctx, "handlers/user.go:10:HandleUser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !coverage.IsCovered {
			t.Error("expected symbol to be covered")
		}
		if len(coverage.DirectTests) == 0 {
			t.Error("expected direct tests")
		}

		foundDirectTest := false
		for _, test := range coverage.DirectTests {
			if test.TestName == "TestHandleUser" {
				foundDirectTest = true
				if test.CallDepth != 1 {
					t.Errorf("expected call depth 1, got %d", test.CallDepth)
				}
			}
		}
		if !foundDirectTest {
			t.Error("expected TestHandleUser in direct tests")
		}
	})

	t.Run("finds indirect test coverage", func(t *testing.T) {
		coverage, err := finder.FindTestCoverage(ctx, "handlers/user.go:50:validateUser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !coverage.IsCovered {
			t.Error("expected symbol to be covered (indirectly)")
		}

		// validateUser is called by handler, which is called by tests
		// So it should be indirect coverage at depth 2
		if len(coverage.IndirectTests) == 0 && len(coverage.DirectTests) == 0 {
			t.Error("expected some test coverage")
		}

		if coverage.CoverageDepth < 2 {
			t.Errorf("expected coverage depth >= 2, got %d", coverage.CoverageDepth)
		}
	})

	t.Run("reports uncovered symbol", func(t *testing.T) {
		coverage, err := finder.FindTestCoverage(ctx, "handlers/admin.go:20:AdminHandler")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if coverage.IsCovered {
			t.Error("expected symbol to be uncovered")
		}
		if len(coverage.DirectTests) > 0 {
			t.Error("expected no direct tests")
		}
		if len(coverage.IndirectTests) > 0 {
			t.Error("expected no indirect tests")
		}
		if coverage.CoverageDepth != -1 {
			t.Errorf("expected coverage depth -1, got %d", coverage.CoverageDepth)
		}
	})

	t.Run("has confidence score", func(t *testing.T) {
		coverage, err := finder.FindTestCoverage(ctx, "handlers/user.go:10:HandleUser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if coverage.Confidence <= 0 || coverage.Confidence > 1 {
			t.Errorf("confidence should be between 0 and 1, got %f", coverage.Confidence)
		}
	})
}

func TestTestCoverageFinder_Errors(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := finder.FindTestCoverage(nil, "some.id")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target ID", func(t *testing.T) {
		_, err := finder.FindTestCoverage(ctx, "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("symbol not found", func(t *testing.T) {
		_, err := finder.FindTestCoverage(ctx, "nonexistent.id")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := finder.FindTestCoverage(cancelCtx, "some.id")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("unfrozen graph", func(t *testing.T) {
		unfrozenGraph := graph.NewGraph("/test")
		unfrozenFinder := NewTestCoverageFinder(unfrozenGraph, idx)

		_, err := unfrozenFinder.FindTestCoverage(ctx, "handlers/user.go:10:HandleUser")
		if err != ErrGraphNotReady {
			t.Errorf("expected ErrGraphNotReady, got %v", err)
		}
	})
}

func TestTestCoverageFinder_NilGraph(t *testing.T) {
	idx := index.NewSymbolIndex()
	handler := &ast.Symbol{
		ID:        "handlers/user.go:10:HandleUser",
		Name:      "HandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user.go",
		StartLine: 10,
		EndLine:   30,
		Language:  "go",
	}
	if err := idx.Add(handler); err != nil {
		t.Fatalf("failed to add handler: %v", err)
	}

	finder := NewTestCoverageFinder(nil, idx)
	ctx := context.Background()

	coverage, err := finder.FindTestCoverage(ctx, "handlers/user.go:10:HandleUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have limitation about no graph
	hasLimitation := false
	for _, lim := range coverage.Limitations {
		if lim == "Graph not available - cannot analyze coverage" {
			hasLimitation = true
			break
		}
	}
	if !hasLimitation {
		t.Error("expected graph limitation")
	}

	// Confidence should be low
	if coverage.Confidence >= 0.5 {
		t.Errorf("expected low confidence, got %f", coverage.Confidence)
	}
}

func TestTestCoverageFinder_FindUncoveredSymbols(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	uncovered, err := finder.FindUncoveredSymbols(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find AdminHandler as uncovered
	foundAdmin := false
	for _, id := range uncovered {
		if id == "handlers/admin.go:20:AdminHandler" {
			foundAdmin = true
			break
		}
	}
	if !foundAdmin {
		t.Error("expected AdminHandler in uncovered symbols")
	}

	// Should NOT include covered functions
	for _, id := range uncovered {
		if id == "handlers/user.go:10:HandleUser" {
			t.Error("HandleUser should be covered, not in uncovered list")
		}
	}
}

func TestTestCoverageFinder_FindTestsForFile(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	tests, err := finder.FindTestsForFile(ctx, "handlers/user.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tests) == 0 {
		t.Error("expected tests for user.go")
	}

	foundTestHandleUser := false
	for _, test := range tests {
		if test.TestName == "TestHandleUser" {
			foundTestHandleUser = true
			break
		}
	}
	if !foundTestHandleUser {
		t.Error("expected TestHandleUser in tests for file")
	}
}

func TestTestCoverageFinder_FindTestsForFile_Errors(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := finder.FindTestsForFile(nil, "file.go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty file path", func(t *testing.T) {
		_, err := finder.FindTestsForFile(ctx, "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestTestCoverageFinder_GenerateCoverageReport(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)
	ctx := context.Background()

	report, err := finder.GenerateCoverageReport(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check basic report fields
	if report.TotalSymbols == 0 {
		t.Error("expected some symbols in report")
	}
	if report.TestCount == 0 {
		t.Error("expected test count > 0")
	}
	if report.CoveragePercent < 0 || report.CoveragePercent > 100 {
		t.Errorf("coverage percent out of range: %f", report.CoveragePercent)
	}
	if report.Confidence <= 0 || report.Confidence > 1 {
		t.Errorf("confidence out of range: %f", report.Confidence)
	}

	// Should have some uncovered symbols
	if len(report.UncoveredSymbols) == 0 {
		t.Log("warning: no uncovered symbols found (may be expected)")
	}
}

func TestTestCoverageFinder_GenerateCoverageReport_Errors(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)

	t.Run("nil context", func(t *testing.T) {
		_, err := finder.GenerateCoverageReport(nil)
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("unfrozen graph", func(t *testing.T) {
		unfrozenGraph := graph.NewGraph("/test")
		unfrozenFinder := NewTestCoverageFinder(unfrozenGraph, idx)

		_, err := unfrozenFinder.GenerateCoverageReport(context.Background())
		if err != ErrGraphNotReady {
			t.Errorf("expected ErrGraphNotReady, got %v", err)
		}
	})
}

func TestFindCallPath(t *testing.T) {
	g, idx := setupCoverageTestGraph()
	finder := NewTestCoverageFinder(g, idx)

	t.Run("finds direct path", func(t *testing.T) {
		depth, path := finder.findCallPath(
			"handlers/user_test.go:10:TestHandleUser",
			"handlers/user.go:10:HandleUser",
		)

		if depth != 1 {
			t.Errorf("expected depth 1, got %d", depth)
		}
		if len(path) != 2 {
			t.Errorf("expected path length 2, got %d", len(path))
		}
	})

	t.Run("finds indirect path", func(t *testing.T) {
		depth, path := finder.findCallPath(
			"handlers/user_test.go:10:TestHandleUser",
			"handlers/user.go:50:validateUser",
		)

		if depth != 2 {
			t.Errorf("expected depth 2, got %d", depth)
		}
		if len(path) != 3 {
			t.Errorf("expected path length 3, got %d", len(path))
		}
	})

	t.Run("returns -1 for no path", func(t *testing.T) {
		depth, path := finder.findCallPath(
			"handlers/user_test.go:10:TestHandleUser",
			"handlers/admin.go:20:AdminHandler",
		)

		if depth != -1 {
			t.Errorf("expected depth -1, got %d", depth)
		}
		if path != nil {
			t.Error("expected nil path")
		}
	})

	t.Run("handles same source and target", func(t *testing.T) {
		depth, _ := finder.findCallPath(
			"handlers/user.go:10:HandleUser",
			"handlers/user.go:10:HandleUser",
		)

		// Same source and target with depth 0 should not return a path
		// (we're looking for actual calls, not identity)
		if depth != -1 {
			t.Errorf("expected depth -1 for same source/target, got %d", depth)
		}
	})
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"./handlers/user.go", "handlers/user.go"},
		{"handlers/user.go", "handlers/user.go"},
		{"handlers/../handlers/user.go", "handlers/user.go"},
		{"/absolute/path/file.go", "/absolute/path/file.go"},
	}

	for _, tt := range tests {
		result := normalizePath(tt.input)
		if result != tt.expected {
			t.Errorf("normalizePath(%q) = %q, want %q",
				tt.input, result, tt.expected)
		}
	}
}
