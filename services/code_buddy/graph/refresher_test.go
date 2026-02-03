// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// mockParser implements ast.Parser for testing.
type mockParser struct {
	language   string
	extensions []string
	parseFunc  func(ctx context.Context, content []byte, filePath string) (*ast.ParseResult, error)
}

func (m *mockParser) Language() string                       { return m.language }
func (m *mockParser) Extensions() []string                   { return m.extensions }
func (m *mockParser) SupportedSymbolKinds() []ast.SymbolKind { return nil }

func (m *mockParser) Parse(ctx context.Context, content []byte, filePath string) (*ast.ParseResult, error) {
	if m.parseFunc != nil {
		return m.parseFunc(ctx, content, filePath)
	}
	// Default: create a simple parse result with one symbol
	return &ast.ParseResult{
		FilePath: filePath,
		Language: m.language,
		Symbols: []*ast.Symbol{
			{
				ID:        filePath + ":TestFunc",
				Name:      "TestFunc",
				Kind:      ast.SymbolKindFunction,
				FilePath:  filePath,
				StartLine: 1,
				EndLine:   5,
			},
		},
	}, nil
}

func TestRefreshConfig_Defaults(t *testing.T) {
	config := DefaultRefreshConfig()

	if config.MaxFilesToRefresh != 50 {
		t.Errorf("MaxFilesToRefresh = %d, want 50", config.MaxFilesToRefresh)
	}
	if !config.ParallelParsing {
		t.Error("ParallelParsing should be true by default")
	}
	if config.ParallelWorkers != 4 {
		t.Errorf("ParallelWorkers = %d, want 4", config.ParallelWorkers)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", config.Timeout)
	}
}

func TestGraphHolder(t *testing.T) {
	t.Run("new holder with nil graph", func(t *testing.T) {
		holder := NewGraphHolder(nil)
		if holder.Get() != nil {
			t.Error("Expected nil graph")
		}
	})

	t.Run("set and get graph", func(t *testing.T) {
		holder := NewGraphHolder(nil)
		graph := NewGraph("/test")

		holder.Set(graph)

		if holder.Get() != graph {
			t.Error("Expected to get the same graph")
		}
	})

	t.Run("get pointer", func(t *testing.T) {
		graph := NewGraph("/test")
		holder := NewGraphHolder(graph)

		ptr := holder.GetPtr()
		if *ptr != graph {
			t.Error("Pointer should point to the graph")
		}
	})
}

func TestRefresher_RefreshFiles_Empty(t *testing.T) {
	graph := NewGraph("/test")
	holder := NewGraphHolder(graph)
	registry := ast.NewParserRegistry()

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherLogger(NullLogger()),
	)

	result, err := refresher.RefreshFiles(context.Background(), []string{})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.FilesRefreshed != 0 {
		t.Errorf("FilesRefreshed = %d, want 0", result.FilesRefreshed)
	}
}

func TestRefresher_RefreshFiles_TooManyFiles(t *testing.T) {
	graph := NewGraph("/test")
	holder := NewGraphHolder(graph)
	registry := ast.NewParserRegistry()

	config := DefaultRefreshConfig()
	config.MaxFilesToRefresh = 2

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherConfig(config),
		WithRefresherLogger(NullLogger()),
	)

	// Try to refresh 3 files (exceeds limit of 2)
	paths := []string{"a.go", "b.go", "c.go"}
	_, err := refresher.RefreshFiles(context.Background(), paths)

	if err == nil {
		t.Fatal("Expected error for too many files")
	}
}

func TestRefresher_RefreshFiles_NilGraph(t *testing.T) {
	var graph *Graph = nil
	holder := NewGraphHolder(graph)
	registry := ast.NewParserRegistry()

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherLogger(NullLogger()),
	)

	_, err := refresher.RefreshFiles(context.Background(), []string{"foo.go"})

	if err == nil {
		t.Fatal("Expected error for nil graph")
	}
}

func TestRefresher_RefreshFiles_WithRealFile(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "refresher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	content := `package main

func Hello() string {
	return "hello"
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Setup graph with existing node for the file
	graph := NewGraph(tmpDir)
	existingSymbol := &ast.Symbol{
		ID:        testFile + ":OldFunc",
		Name:      "OldFunc",
		Kind:      ast.SymbolKindFunction,
		FilePath:  testFile,
		StartLine: 1,
		EndLine:   3,
	}
	_, _ = graph.AddNode(existingSymbol)
	graph.Freeze()

	holder := NewGraphHolder(graph)

	// Setup parser registry with mock parser
	registry := ast.NewParserRegistry()
	registry.Register(&mockParser{
		language:   "go",
		extensions: []string{".go"},
	})

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherLogger(NullLogger()),
	)

	// Refresh
	result, err := refresher.RefreshFiles(context.Background(), []string{testFile})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.FilesRefreshed != 1 {
		t.Errorf("FilesRefreshed = %d, want 1", result.FilesRefreshed)
	}
	if result.NodesRemoved != 1 {
		t.Errorf("NodesRemoved = %d, want 1 (OldFunc)", result.NodesRemoved)
	}
	if result.NodesAdded != 1 {
		t.Errorf("NodesAdded = %d, want 1 (TestFunc from mock)", result.NodesAdded)
	}

	// Verify graph was swapped
	newGraph := holder.Get()
	if newGraph == graph {
		t.Error("Graph should have been swapped to a new instance")
	}
	if !newGraph.IsFrozen() {
		t.Error("New graph should be frozen")
	}
}

func TestRefresher_RefreshFiles_ContextCancellation(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "refresher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	graph := NewGraph(tmpDir)
	graph.Freeze()
	holder := NewGraphHolder(graph)

	registry := ast.NewParserRegistry()
	registry.Register(&mockParser{
		language:   "go",
		extensions: []string{".go"},
		parseFunc: func(ctx context.Context, content []byte, filePath string) (*ast.ParseResult, error) {
			// Simulate slow parsing
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			return &ast.ParseResult{FilePath: filePath}, nil
		},
	})

	config := DefaultRefreshConfig()
	config.ParallelParsing = false // Sequential to make cancellation deterministic

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherConfig(config),
		WithRefresherLogger(NullLogger()),
	)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = refresher.RefreshFiles(ctx, []string{testFile})

	if err == nil {
		t.Fatal("Expected error from context cancellation")
	}
}

func TestRefresher_RefreshFiles_NoParserForExtension(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "refresher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file with unsupported extension
	testFile := filepath.Join(tmpDir, "test.xyz")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	graph := NewGraph(tmpDir)
	graph.Freeze()
	holder := NewGraphHolder(graph)

	// Empty registry (no parsers)
	registry := ast.NewParserRegistry()

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherLogger(NullLogger()),
	)

	result, err := refresher.RefreshFiles(context.Background(), []string{testFile})

	// Should not fail, but record parse error
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.ParseErrors) != 1 {
		t.Errorf("Expected 1 parse error, got %d", len(result.ParseErrors))
	}
}

func TestRefresher_RefreshFiles_ParallelParsing(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "refresher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple test files
	files := []string{}
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(tmpDir, "test"+string(rune('a'+i))+".go")
		if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		files = append(files, testFile)
	}

	graph := NewGraph(tmpDir)
	graph.Freeze()
	holder := NewGraphHolder(graph)

	registry := ast.NewParserRegistry()
	registry.Register(&mockParser{
		language:   "go",
		extensions: []string{".go"},
	})

	config := DefaultRefreshConfig()
	config.ParallelParsing = true
	config.ParallelWorkers = 2

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherConfig(config),
		WithRefresherLogger(NullLogger()),
	)

	result, err := refresher.RefreshFiles(context.Background(), files)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.FilesRefreshed != 5 {
		t.Errorf("FilesRefreshed = %d, want 5", result.FilesRefreshed)
	}
	if result.NodesAdded != 5 {
		t.Errorf("NodesAdded = %d, want 5", result.NodesAdded)
	}
}

func TestRefresher_GetSetGraph(t *testing.T) {
	graph1 := NewGraph("/test1")
	holder := NewGraphHolder(graph1)
	registry := ast.NewParserRegistry()

	refresher := NewRefresher(holder.GetPtr(), registry,
		WithRefresherLogger(NullLogger()),
	)

	// Get should return initial graph
	if refresher.GetGraph() != graph1 {
		t.Error("GetGraph should return initial graph")
	}

	// Set new graph
	graph2 := NewGraph("/test2")
	refresher.SetGraph(graph2)

	if refresher.GetGraph() != graph2 {
		t.Error("GetGraph should return new graph after SetGraph")
	}
}

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"foo.go", ".go"},
		{"path/to/file.go", ".go"},
		{"file.test.go", ".go"},
		{"/abs/path/file.py", ".py"},
		{"noextension", ""},
		{"path/to/noext", ""},
		{".gitignore", ".gitignore"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := getFileExtension(tt.path)
			if result != tt.expected {
				t.Errorf("getFileExtension(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}
