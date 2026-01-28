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

func setupBreakingTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a handler function
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

	// Create callers
	caller1 := &ast.Symbol{
		ID:        "main.go:50:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "main.go",
		StartLine: 50,
		Package:   "main",
		Language:  "go",
	}

	caller2 := &ast.Symbol{
		ID:        "server.go:100:StartServer",
		Name:      "StartServer",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "server.go",
		StartLine: 100,
		Package:   "main",
		Language:  "go",
	}

	// Add to graph
	g.AddNode(handler)
	g.AddNode(caller1)
	g.AddNode(caller2)

	// Add call edges
	g.AddEdge(caller1.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  caller1.FilePath,
		StartLine: caller1.StartLine,
	})
	g.AddEdge(caller2.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  caller2.FilePath,
		StartLine: caller2.StartLine,
	})

	g.Freeze()

	// Add to index
	idx.Add(handler)
	idx.Add(caller1)
	idx.Add(caller2)

	return g, idx
}

func TestBreakingChangeAnalyzer_AnalyzeBreaking(t *testing.T) {
	g, idx := setupBreakingTestGraph()
	analyzer := NewBreakingChangeAnalyzer(g, idx)
	ctx := context.Background()

	t.Run("detects added required parameter", func(t *testing.T) {
		analysis, err := analyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, req *UserRequest, opts Options) (*UserResponse, error)",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !analysis.IsBreaking {
			t.Error("expected breaking change for added parameter")
		}
		if analysis.CallersAffected != 2 {
			t.Errorf("expected 2 affected callers, got %d", analysis.CallersAffected)
		}
		if len(analysis.BreakingChanges) == 0 {
			t.Error("expected breaking changes to be listed")
		}
	})

	t.Run("no breaking change for identical signature", func(t *testing.T) {
		analysis, err := analyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, req *UserRequest) (*UserResponse, error)",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.IsBreaking {
			t.Error("expected no breaking change for identical signature")
		}
	})

	t.Run("detects return type change", func(t *testing.T) {
		analysis, err := analyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, req *UserRequest) error",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !analysis.IsBreaking {
			t.Error("expected breaking change for return type change")
		}
		hasReturnChange := false
		for _, change := range analysis.BreakingChanges {
			if change.Type == BreakingChangeReturn {
				hasReturnChange = true
				break
			}
		}
		if !hasReturnChange {
			t.Error("expected return type breaking change")
		}
	})

	t.Run("has confidence score", func(t *testing.T) {
		analysis, err := analyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context) error",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if analysis.Confidence <= 0 || analysis.Confidence > 1 {
			t.Errorf("confidence should be between 0 and 1, got %f", analysis.Confidence)
		}
	})
}

func TestBreakingChangeAnalyzer_Errors(t *testing.T) {
	g, idx := setupBreakingTestGraph()
	analyzer := NewBreakingChangeAnalyzer(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := analyzer.AnalyzeBreaking(nil, "id", "sig")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target ID", func(t *testing.T) {
		_, err := analyzer.AnalyzeBreaking(ctx, "", "func()")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty proposed signature", func(t *testing.T) {
		_, err := analyzer.AnalyzeBreaking(ctx, "some.id", "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("symbol not found", func(t *testing.T) {
		_, err := analyzer.AnalyzeBreaking(ctx, "nonexistent.id", "func()")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := analyzer.AnalyzeBreaking(cancelCtx, "id", "sig")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("unfrozen graph", func(t *testing.T) {
		unfrozenGraph := graph.NewGraph("/test")
		unfrozenAnalyzer := NewBreakingChangeAnalyzer(unfrozenGraph, idx)

		_, err := unfrozenAnalyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func()")
		if err != ErrGraphNotReady {
			t.Errorf("expected ErrGraphNotReady, got %v", err)
		}
	})
}

func TestBreakingChangeAnalyzer_BatchAnalysis(t *testing.T) {
	g, idx := setupBreakingTestGraph()
	analyzer := NewBreakingChangeAnalyzer(g, idx)
	ctx := context.Background()

	changes := map[string]string{
		"handlers/user.go:10:HandleUser": "func(ctx context.Context) error",
		"nonexistent.symbol":             "func()",
	}

	results, err := analyzer.AnalyzeBreakingBatch(ctx, changes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check that existing symbol was analyzed
	if result, ok := results["handlers/user.go:10:HandleUser"]; ok {
		if !result.IsBreaking {
			t.Error("expected breaking change")
		}
	} else {
		t.Error("missing result for existing symbol")
	}

	// Check that nonexistent symbol has error in limitations
	if result, ok := results["nonexistent.symbol"]; ok {
		if len(result.Limitations) == 0 {
			t.Error("expected limitation for failed analysis")
		}
	} else {
		t.Error("missing result for nonexistent symbol")
	}
}

func TestBreakingChangeAnalyzer_VisibilityChanges(t *testing.T) {
	g, idx := setupBreakingTestGraph()

	// Add an unexported function
	unexported := &ast.Symbol{
		ID:        "internal/helper.go:5:helper",
		Name:      "helper",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "internal/helper.go",
		StartLine: 5,
		Package:   "internal",
		Signature: "func(x int) int",
		Language:  "go",
		Exported:  false,
	}
	idx.Add(unexported)

	analyzer := NewBreakingChangeAnalyzer(g, idx)
	ctx := context.Background()

	t.Run("detects exported to unexported change", func(t *testing.T) {
		analysis, err := analyzer.AnalyzeBreaking(ctx,
			"handlers/user.go:10:HandleUser",
			"func handleUser(ctx context.Context, req *UserRequest) (*UserResponse, error)",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		hasVisibilityChange := false
		for _, change := range analysis.BreakingChanges {
			if change.Type == BreakingChangeVisibility {
				hasVisibilityChange = true
				break
			}
		}
		if !hasVisibilityChange {
			t.Error("expected visibility breaking change")
		}
	})
}

func TestExtractFunctionName(t *testing.T) {
	tests := []struct {
		sig      string
		expected string
	}{
		{"func Foo()", "Foo"},
		{"func Bar(x int) error", "Bar"},
		{"func (s *S) Method() error", "Method"},
		{"func (r Receiver) M()", "M"},
		{"not a func", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractFunctionName(tt.sig)
		if result != tt.expected {
			t.Errorf("extractFunctionName(%q) = %q, want %q",
				tt.sig, result, tt.expected)
		}
	}
}

func TestIsGoExported(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Foo", true},
		{"foo", false},
		{"A", true},
		{"a", false},
		{"FOO", true},
		{"_foo", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isGoExported(tt.name)
		if result != tt.expected {
			t.Errorf("isGoExported(%q) = %v, want %v",
				tt.name, result, tt.expected)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"foo_test.go", true},
		{"foo.go", false},
		{"test_foo.py", false},
		{"foo_test.py", true},
		{"foo.test.ts", true},
		{"foo.spec.ts", true},
		{"foo.ts", false},
		{"foo.test.js", true},
		{"foo.spec.js", true},
	}

	for _, tt := range tests {
		result := isTestFile(tt.path)
		if result != tt.expected {
			t.Errorf("isTestFile(%q) = %v, want %v",
				tt.path, result, tt.expected)
		}
	}
}
