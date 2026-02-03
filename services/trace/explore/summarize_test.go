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

// createSummarizeTestGraph creates a test graph with symbols for summarization testing.
func createSummarizeTestGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Add symbols for handlers/user.go
	userHandlerSymbols := []*ast.Symbol{
		{
			ID:        "handlers/user.go:1:fmt",
			Name:      "fmt",
			Kind:      ast.SymbolKindImport,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 1,
			EndLine:   1,
		},
		{
			ID:        "handlers/user.go:2:http",
			Name:      "http",
			Kind:      ast.SymbolKindImport,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 2,
			EndLine:   2,
		},
		{
			ID:        "handlers/user.go:10:UserHandler",
			Name:      "UserHandler",
			Kind:      ast.SymbolKindStruct,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 10,
			EndLine:   20,
			Exported:  true,
			Children: []*ast.Symbol{
				{
					ID:        "handlers/user.go:11:db",
					Name:      "db",
					Kind:      ast.SymbolKindField,
					FilePath:  "handlers/user.go",
					Package:   "handlers",
					Language:  "go",
					StartLine: 11,
					EndLine:   11,
				},
				{
					ID:        "handlers/user.go:12:cache",
					Name:      "cache",
					Kind:      ast.SymbolKindField,
					FilePath:  "handlers/user.go",
					Package:   "handlers",
					Language:  "go",
					StartLine: 12,
					EndLine:   12,
				},
			},
		},
		{
			ID:        "handlers/user.go:25:GetUser",
			Name:      "GetUser",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 25,
			EndLine:   35,
			Receiver:  "UserHandler",
			Signature: "func(w http.ResponseWriter, r *http.Request)",
			Exported:  true,
		},
		{
			ID:        "handlers/user.go:40:CreateUser",
			Name:      "CreateUser",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 40,
			EndLine:   60,
			Receiver:  "UserHandler",
			Signature: "func(w http.ResponseWriter, r *http.Request)",
			Exported:  true,
		},
		{
			ID:        "handlers/user.go:65:parseUserID",
			Name:      "parseUserID",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/user.go",
			Package:   "handlers",
			Language:  "go",
			StartLine: 65,
			EndLine:   70,
			Signature: "func(r *http.Request) (int, error)",
			Exported:  false,
		},
	}

	// Add symbols for internal/types.go
	typesSymbols := []*ast.Symbol{
		{
			ID:        "internal/types.go:5:User",
			Name:      "User",
			Kind:      ast.SymbolKindStruct,
			FilePath:  "internal/types.go",
			Package:   "internal",
			Language:  "go",
			StartLine: 5,
			EndLine:   15,
			Exported:  true,
		},
		{
			ID:        "internal/types.go:20:UserService",
			Name:      "UserService",
			Kind:      ast.SymbolKindInterface,
			FilePath:  "internal/types.go",
			Package:   "internal",
			Language:  "go",
			StartLine: 20,
			EndLine:   30,
			Exported:  true,
		},
	}

	// Add all symbols
	allSymbols := append(userHandlerSymbols, typesSymbols...)
	for _, sym := range allSymbols {
		_, err := g.AddNode(sym)
		if err != nil {
			t.Fatalf("failed to add node: %v", err)
		}
		err = idx.Add(sym)
		if err != nil {
			t.Fatalf("failed to add symbol: %v", err)
		}
	}

	g.Freeze()
	return g, idx
}

func TestFileSummarizer_SummarizeFile(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewFileSummarizer(g, idx)

	t.Run("summarize existing file", func(t *testing.T) {
		ctx := context.Background()
		summary, err := summarizer.SummarizeFile(ctx, "handlers/user.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if summary.FilePath != "handlers/user.go" {
			t.Errorf("expected file path handlers/user.go, got %s", summary.FilePath)
		}

		if summary.Package != "handlers" {
			t.Errorf("expected package handlers, got %s", summary.Package)
		}

		// Check imports
		if len(summary.Imports) != 2 {
			t.Errorf("expected 2 imports, got %d", len(summary.Imports))
		}

		// Check types
		if len(summary.Types) != 1 {
			t.Errorf("expected 1 type, got %d", len(summary.Types))
		}

		if len(summary.Types) > 0 {
			userHandler := summary.Types[0]
			if userHandler.Name != "UserHandler" {
				t.Errorf("expected UserHandler type, got %s", userHandler.Name)
			}
			if userHandler.Fields != 2 {
				t.Errorf("expected 2 fields, got %d", userHandler.Fields)
			}
			if len(userHandler.Methods) != 2 {
				t.Errorf("expected 2 methods, got %d", len(userHandler.Methods))
			}
		}

		// Check functions
		if len(summary.Functions) != 1 {
			t.Errorf("expected 1 function, got %d", len(summary.Functions))
		}

		// Check line count
		if summary.LineCount == 0 {
			t.Error("expected non-zero line count")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		ctx := context.Background()
		_, err := summarizer.SummarizeFile(ctx, "nonexistent.go")
		if err != ErrFileNotFound {
			t.Errorf("expected ErrFileNotFound, got %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := summarizer.SummarizeFile(nil, "handlers/user.go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := summarizer.SummarizeFile(ctx, "handlers/user.go")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestFileSummarizer_SummarizePackage(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewFileSummarizer(g, idx)

	t.Run("summarize existing package", func(t *testing.T) {
		ctx := context.Background()
		summaries, err := summarizer.SummarizePackage(ctx, "handlers")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(summaries) != 1 {
			t.Errorf("expected 1 file in handlers package, got %d", len(summaries))
		}
	})

	t.Run("package not found", func(t *testing.T) {
		ctx := context.Background()
		_, err := summarizer.SummarizePackage(ctx, "nonexistent")
		if err != ErrPackageNotFound {
			t.Errorf("expected ErrPackageNotFound, got %v", err)
		}
	})
}

func TestInferFilePurpose(t *testing.T) {
	testCases := []struct {
		filePath string
		summary  *FileSummary
		expected string
	}{
		{"handler_test.go", &FileSummary{}, "Unit tests"},
		{"test_handler.py", &FileSummary{}, "Unit tests"},
		{"main.go", &FileSummary{}, "Application entry point"},
		{"types.go", &FileSummary{}, "Type definitions"},
		{"errors.go", &FileSummary{}, "Error definitions"},
		{"utils.go", &FileSummary{}, "Utility functions"},
		{"config.go", &FileSummary{}, "Configuration management"},
		{"constants.go", &FileSummary{}, "Constant definitions"},
		{"models.go", &FileSummary{}, "Data models"},
		{"user_handler.go", &FileSummary{}, "Request handlers"},
		{"user_controller.py", &FileSummary{}, "Controllers"},
		{"user_service.go", &FileSummary{}, "Service layer"},
		{"user_repository.go", &FileSummary{}, "Data access layer"},
		{"auth_middleware.go", &FileSummary{}, "Middleware"},
		{"routes.go", &FileSummary{}, "Route definitions"},
		{"database.go", &FileSummary{}, "Database operations"},
		{"auth.go", &FileSummary{}, "Authentication/authorization"},
		{"api.go", &FileSummary{}, "API definitions"},
		{"client.go", &FileSummary{}, "Client implementation"},
		{"server.go", &FileSummary{}, "Server implementation"},
	}

	for _, tc := range testCases {
		t.Run(tc.filePath, func(t *testing.T) {
			result := inferFilePurpose(tc.filePath, tc.summary)
			if result != tc.expected {
				t.Errorf("expected purpose %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestInferFilePurpose_FromContent(t *testing.T) {
	t.Run("types only", func(t *testing.T) {
		summary := &FileSummary{
			Types:     []TypeBrief{{Name: "User"}},
			Functions: []FuncBrief{},
		}
		result := inferFilePurpose("something.go", summary)
		if result != "Type definitions" {
			t.Errorf("expected 'Type definitions', got %q", result)
		}
	})

	t.Run("functions only", func(t *testing.T) {
		summary := &FileSummary{
			Types:     []TypeBrief{},
			Functions: []FuncBrief{{Name: "DoSomething"}},
		}
		result := inferFilePurpose("something.go", summary)
		if result != "Function implementations" {
			t.Errorf("expected 'Function implementations', got %q", result)
		}
	})

	t.Run("interface heavy", func(t *testing.T) {
		summary := &FileSummary{
			Types: []TypeBrief{
				{Name: "Reader", Kind: "interface"},
				{Name: "Writer", Kind: "interface"},
			},
			Functions: []FuncBrief{},
		}
		result := inferFilePurpose("something.go", summary)
		if result != "Interface definitions" {
			t.Errorf("expected 'Interface definitions', got %q", result)
		}
	})
}

func TestPackageAPISummarizer_FindPackageAPI(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewPackageAPISummarizer(g, idx)

	t.Run("find internal package API", func(t *testing.T) {
		ctx := context.Background()
		api, err := summarizer.FindPackageAPI(ctx, "internal")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if api.Package != "internal" {
			t.Errorf("expected package internal, got %s", api.Package)
		}

		// Should have User struct and UserService interface
		if len(api.Types) != 2 {
			t.Errorf("expected 2 types, got %d", len(api.Types))
		}
	})

	t.Run("package not found", func(t *testing.T) {
		ctx := context.Background()
		_, err := summarizer.FindPackageAPI(ctx, "nonexistent")
		if err != ErrPackageNotFound {
			t.Errorf("expected ErrPackageNotFound, got %v", err)
		}
	})

	t.Run("only exported symbols", func(t *testing.T) {
		ctx := context.Background()
		api, err := summarizer.FindPackageAPI(ctx, "handlers")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// parseUserID is not exported, should not appear
		for _, f := range api.Functions {
			if f.Name == "parseUserID" {
				t.Error("unexported function should not appear in API")
			}
		}
	})
}

func TestFileSummarizer_PerformanceTarget(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewFileSummarizer(g, idx)

	ctx := context.Background()

	// Target: < 50ms
	start := time.Now()
	_, err := summarizer.SummarizeFile(ctx, "handlers/user.go")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if duration > 50*time.Millisecond {
		t.Errorf("SummarizeFile took %v, expected < 50ms", duration)
	}
}

func TestPackageAPISummarizer_PerformanceTarget(t *testing.T) {
	g, idx := createSummarizeTestGraph(t)
	summarizer := NewPackageAPISummarizer(g, idx)

	ctx := context.Background()

	// Target: < 50ms
	start := time.Now()
	_, err := summarizer.FindPackageAPI(ctx, "internal")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if duration > 50*time.Millisecond {
		t.Errorf("FindPackageAPI took %v, expected < 50ms", duration)
	}
}

func TestGetBaseName(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"handlers/user.go", "user"},
		{"user.go", "user"},
		{"internal/pkg/types.go", "types"},
		{"noextension", "noextension"},
		{"/absolute/path/file.py", "file"},
		{"windows\\path\\file.ts", "file"},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := getBaseName(tc.path)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
