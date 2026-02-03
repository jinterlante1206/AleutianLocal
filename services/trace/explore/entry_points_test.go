// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// createTestGraph creates a test graph with test symbols.
func createTestGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Add test symbols
	symbols := []*ast.Symbol{
		// Go main function
		{
			ID:       "cmd/main.go:1:main",
			Name:     "main",
			Kind:     ast.SymbolKindFunction,
			FilePath: "cmd/main.go",
			Package:  "main",
			Language: "go",
		},
		// Go HTTP handler (standard library style)
		{
			ID:        "handlers/user.go:10:ServeHTTP",
			Name:      "ServeHTTP",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			Signature: "func(w http.ResponseWriter, r *http.Request)",
			Receiver:  "UserHandler",
		},
		// Go Gin handler
		{
			ID:        "handlers/api.go:20:GetUsers",
			Name:      "GetUsers",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/api.go",
			Package:   "handlers",
			Language:  "go",
			Signature: "func(c *gin.Context)",
		},
		// Go test
		{
			ID:        "handlers/user_test.go:5:TestUserHandler",
			Name:      "TestUserHandler",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/user_test.go",
			Package:   "handlers",
			Language:  "go",
			Signature: "func(t *testing.T)",
		},
		// Python Flask handler
		{
			ID:       "app/routes.py:10:index",
			Name:     "index",
			Kind:     ast.SymbolKindFunction,
			FilePath: "app/routes.py",
			Package:  "app.routes",
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Decorators: []string{"@app.route"},
			},
		},
		// Python pytest
		{
			ID:       "tests/test_app.py:5:test_index",
			Name:     "test_index",
			Kind:     ast.SymbolKindFunction,
			FilePath: "tests/test_app.py",
			Package:  "tests",
			Language: "python",
		},
		// TypeScript NestJS handler
		{
			ID:       "src/users.controller.ts:15:getUsers",
			Name:     "getUsers",
			Kind:     ast.SymbolKindMethod,
			FilePath: "src/users.controller.ts",
			Package:  "users",
			Language: "typescript",
			Metadata: &ast.SymbolMetadata{
				Decorators: []string{"@Get"},
			},
		},
		// Regular Go function (not an entry point)
		{
			ID:        "internal/utils.go:5:helper",
			Name:      "helper",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "internal/utils.go",
			Package:   "internal",
			Language:  "go",
			Signature: "func(s string) string",
		},
		// Go gRPC implementation
		{
			ID:       "grpc/server.go:10:UserServer",
			Name:     "UserServer",
			Kind:     ast.SymbolKindStruct,
			FilePath: "grpc/server.go",
			Package:  "grpc",
			Language: "go",
			Metadata: &ast.SymbolMetadata{
				Implements: []string{"UserServiceServer"},
			},
		},
	}

	for _, sym := range symbols {
		sym.StartLine = 1
		sym.EndLine = 10
		sym.Exported = len(sym.Name) > 0 && sym.Name[0] >= 'A' && sym.Name[0] <= 'Z'

		_, err := g.AddNode(sym)
		if err != nil {
			t.Fatalf("failed to add node: %v", err)
		}

		err = idx.Add(sym)
		if err != nil {
			t.Fatalf("failed to add symbol to index: %v", err)
		}
	}

	g.Freeze()

	return g, idx
}

func TestEntryPointFinder_FindEntryPoints(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)

	t.Run("find all entry points", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.IncludeTests = true
		opts.Limit = 100

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.EntryPoints) == 0 {
			t.Error("expected to find entry points")
		}

		// Check that we found main
		foundMain := false
		for _, ep := range result.EntryPoints {
			if ep.Type == EntryPointMain && ep.Name == "main" {
				foundMain = true
				break
			}
		}
		if !foundMain {
			t.Error("expected to find main entry point")
		}
	})

	t.Run("find only main entry points", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Type = EntryPointMain

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range result.EntryPoints {
			if ep.Type != EntryPointMain {
				t.Errorf("expected only main entry points, got %s", ep.Type)
			}
		}
	})

	t.Run("find handlers", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Type = EntryPointHandler
		opts.Limit = 100

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.EntryPoints) == 0 {
			t.Error("expected to find handlers")
		}

		for _, ep := range result.EntryPoints {
			if ep.Type != EntryPointHandler {
				t.Errorf("expected only handler entry points, got %s", ep.Type)
			}
		}
	})

	t.Run("find tests when included", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Type = EntryPointTest
		opts.IncludeTests = true

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.EntryPoints) == 0 {
			t.Error("expected to find tests")
		}
	})

	t.Run("exclude tests by default", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.IncludeTests = false

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range result.EntryPoints {
			if ep.Type == EntryPointTest {
				t.Error("expected tests to be excluded")
			}
		}
	})

	t.Run("filter by package", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Package = "handlers"
		opts.IncludeTests = true

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range result.EntryPoints {
			if ep.Package != "handlers" {
				t.Errorf("expected package handlers, got %s", ep.Package)
			}
		}
	})

	t.Run("filter by language", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Language = "python"
		opts.IncludeTests = true

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.EntryPoints) == 0 {
			t.Error("expected to find python entry points")
		}
	})

	t.Run("respect limit", func(t *testing.T) {
		ctx := context.Background()
		opts := DefaultEntryPointOptions()
		opts.Limit = 2
		opts.IncludeTests = true

		result, err := finder.FindEntryPoints(ctx, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.EntryPoints) > 2 {
			t.Errorf("expected at most 2 entry points, got %d", len(result.EntryPoints))
		}

		if !result.Truncated && len(result.EntryPoints) == 2 {
			// May or may not be truncated depending on total count
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		opts := DefaultEntryPointOptions()
		_, err := finder.FindEntryPoints(ctx, opts)

		// Should return ErrContextCanceled when context is already cancelled
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		opts := DefaultEntryPointOptions()
		_, err := finder.FindEntryPoints(nil, opts)

		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestEntryPointFinder_ConvenienceMethods(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)
	ctx := context.Background()

	t.Run("FindMainEntryPoints", func(t *testing.T) {
		eps, err := finder.FindMainEntryPoints(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range eps {
			if ep.Type != EntryPointMain {
				t.Errorf("expected main type, got %s", ep.Type)
			}
		}
	})

	t.Run("FindHandlers", func(t *testing.T) {
		eps, err := finder.FindHandlers(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range eps {
			if ep.Type != EntryPointHandler {
				t.Errorf("expected handler type, got %s", ep.Type)
			}
		}
	})

	t.Run("FindTests", func(t *testing.T) {
		eps, err := finder.FindTests(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range eps {
			if ep.Type != EntryPointTest {
				t.Errorf("expected test type, got %s", ep.Type)
			}
		}
	})

	t.Run("FindByFramework", func(t *testing.T) {
		eps, err := finder.FindByFramework(ctx, "gin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range eps {
			if ep.Framework != "gin" {
				t.Errorf("expected gin framework, got %s", ep.Framework)
			}
		}
	})

	t.Run("FindByPackage", func(t *testing.T) {
		eps, err := finder.FindByPackage(ctx, "handlers")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, ep := range eps {
			if ep.Package != "handlers" {
				t.Errorf("expected handlers package, got %s", ep.Package)
			}
		}
	})

	t.Run("DetectFrameworks", func(t *testing.T) {
		frameworks, err := finder.DetectFrameworks(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should detect at least stdlib
		if len(frameworks) == 0 {
			t.Error("expected to detect some frameworks")
		}
	})
}

func TestEntryPointFinder_WithUnfrozenGraph(t *testing.T) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Don't freeze the graph
	finder := NewEntryPointFinder(g, idx)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()

	_, err := finder.FindEntryPoints(ctx, opts)
	if err != ErrGraphNotReady {
		t.Errorf("expected ErrGraphNotReady, got %v", err)
	}
}

func TestEntryPointFinder_WithCustomRegistry(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)

	// Create custom registry with only main patterns
	customRegistry := &EntryPointRegistry{
		patterns: make(map[string]*EntryPointPatterns),
	}
	customRegistry.RegisterPatterns("go", &EntryPointPatterns{
		Language: "go",
		Main:     []PatternMatcher{{Name: "main", Package: "main"}},
		// No handler, test, etc. patterns
	})

	customFinder := finder.WithRegistry(customRegistry)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()
	opts.IncludeTests = true

	result, err := customFinder.FindEntryPoints(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only find main, not handlers
	for _, ep := range result.EntryPoints {
		if ep.Type != EntryPointMain {
			t.Errorf("expected only main with custom registry, got %s", ep.Type)
		}
	}
}

func TestEntryPointResult_Sorting(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()
	opts.IncludeTests = true
	opts.Limit = 100

	result, err := finder.FindEntryPoints(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify sorting by file path then line
	for i := 1; i < len(result.EntryPoints); i++ {
		prev := result.EntryPoints[i-1]
		curr := result.EntryPoints[i]

		if prev.FilePath > curr.FilePath {
			t.Errorf("entry points not sorted by file path: %s > %s", prev.FilePath, curr.FilePath)
		}
		if prev.FilePath == curr.FilePath && prev.Line > curr.Line {
			t.Errorf("entry points not sorted by line: %d > %d", prev.Line, curr.Line)
		}
	}
}

func BenchmarkEntryPointFinder_FindEntryPoints(b *testing.B) {
	// Create a larger test graph
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Add many symbols
	for i := 0; i < 10000; i++ {
		sym := &ast.Symbol{
			ID:        ast.GenerateID("file.go", i, "func"),
			Name:      "func",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "file.go",
			Package:   "main",
			Language:  "go",
			StartLine: i,
			EndLine:   i + 10,
		}
		g.AddNode(sym)
		idx.Add(sym)
	}

	// Add some entry points
	for i := 0; i < 100; i++ {
		sym := &ast.Symbol{
			ID:        ast.GenerateID("main.go", i, "main"),
			Name:      "main",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "main.go",
			Package:   "main",
			Language:  "go",
			StartLine: i,
			EndLine:   i + 10,
		}
		g.AddNode(sym)
		idx.Add(sym)
	}

	g.Freeze()
	finder := NewEntryPointFinder(g, idx)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		finder.FindEntryPoints(ctx, opts)
	}
}

func TestEntryPointFinder_PerformanceTarget(t *testing.T) {
	g, idx := createTestGraph(t)
	finder := NewEntryPointFinder(g, idx)

	ctx := context.Background()
	opts := DefaultEntryPointOptions()
	opts.IncludeTests = true

	// Target: < 100ms
	start := time.Now()
	_, err := finder.FindEntryPoints(ctx, opts)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if duration > 100*time.Millisecond {
		t.Errorf("FindEntryPoints took %v, expected < 100ms", duration)
	}
}
