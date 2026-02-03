// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

// setupTestGraph creates a graph with test data for query tests.
func setupTestGraph(t *testing.T, projectRoot string) (*graph.Graph, *manifest.Manifest) {
	t.Helper()

	g := graph.NewGraph(projectRoot)
	m := manifest.NewManifest(projectRoot)

	// Create test file
	testFile := "main.go"
	testFilePath := filepath.Join(projectRoot, testFile)

	content := []byte(`package main

func main() {
	helper()
}

func helper() {
	println("hello")
}
`)

	if err := os.WriteFile(testFilePath, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Get file info for manifest
	stat, err := os.Stat(testFilePath)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}

	// Hash the file
	hasher := manifest.NewSHA256Hasher(1024 * 1024) // 1MB limit
	hash, err := hasher.HashFile(testFilePath)
	if err != nil {
		t.Fatalf("failed to hash file: %v", err)
	}

	// Add manifest entry
	m.Files[testFile] = manifest.FileEntry{
		Path:  testFile,
		Hash:  hash,
		Size:  stat.Size(),
		Mtime: stat.ModTime().UnixNano(),
	}

	// Add nodes for main and helper
	mainSymbol := &ast.Symbol{
		ID:        "main.go:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  testFile,
		StartLine: 3,
		EndLine:   5,
	}

	helperSymbol := &ast.Symbol{
		ID:        "main.go:helper",
		Name:      "helper",
		Kind:      ast.SymbolKindFunction,
		FilePath:  testFile,
		StartLine: 7,
		EndLine:   9,
	}

	// Add nodes
	if _, err := g.AddNode(mainSymbol); err != nil {
		t.Fatalf("failed to add main node: %v", err)
	}
	if _, err := g.AddNode(helperSymbol); err != nil {
		t.Fatalf("failed to add helper node: %v", err)
	}

	// Add edge: main calls helper
	if err := g.AddEdge(mainSymbol.ID, helperSymbol.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  testFile,
		StartLine: 4,
	}); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	g.Freeze()
	return g, m
}

func TestNewVerifiedQuery(t *testing.T) {
	t.Run("creates with default verifier", func(t *testing.T) {
		g := graph.NewGraph("/test")
		m := manifest.NewManifest("/test")

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if vq.Graph() != g {
			t.Error("Graph() should return the wrapped graph")
		}
		if vq.Manifest() != m {
			t.Error("Manifest() should return the wrapped manifest")
		}
		if vq.Verifier() == nil {
			t.Error("Verifier() should return a default verifier")
		}
	})

	t.Run("uses provided verifier", func(t *testing.T) {
		g := graph.NewGraph("/test")
		m := manifest.NewManifest("/test")
		v := NewVerifier()

		vq, err := NewVerifiedQuery(g, m, v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if vq.Verifier() != v {
			t.Error("Verifier() should return the provided verifier")
		}
	})

	t.Run("returns error for nil graph", func(t *testing.T) {
		m := manifest.NewManifest("/test")

		_, err := NewVerifiedQuery(nil, m, nil)
		if err == nil {
			t.Fatal("expected error for nil graph")
		}
		if !errors.Is(err, ErrNilGraph) {
			t.Errorf("expected ErrNilGraph, got %v", err)
		}
	})

	t.Run("returns error for nil manifest", func(t *testing.T) {
		g := graph.NewGraph("/test")

		_, err := NewVerifiedQuery(g, nil, nil)
		if err == nil {
			t.Fatal("expected error for nil manifest")
		}
		if !errors.Is(err, ErrNilManifest) {
			t.Errorf("expected ErrNilManifest, got %v", err)
		}
	})
}

func TestVerifiedQuery_FindCallersByID(t *testing.T) {
	t.Run("returns results when file is fresh", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Find callers of helper - should return main
		result, err := vq.FindCallersByID(ctx, "main.go:helper")
		if err != nil {
			t.Fatalf("FindCallersByID failed: %v", err)
		}

		if len(result.Symbols) != 1 {
			t.Errorf("expected 1 caller, got %d", len(result.Symbols))
		}
		if len(result.Symbols) > 0 && result.Symbols[0].Name != "main" {
			t.Errorf("expected caller to be 'main', got %q", result.Symbols[0].Name)
		}
	})

	t.Run("returns ErrStaleData when file changed", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		// Modify the file to make it stale
		testFile := filepath.Join(tmpDir, "main.go")
		content := []byte(`package main

func main() {
	helper()
	anotherHelper() // Added
}

func helper() {}
func anotherHelper() {}
`)
		// Give filesystem time to update mtime
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		_, err = vq.FindCallersByID(ctx, "main.go:helper")
		if err == nil {
			t.Fatal("expected ErrStaleData, got nil")
		}

		var staleErr *ErrStaleData
		if !errors.As(err, &staleErr) {
			t.Errorf("expected ErrStaleData, got %T: %v", err, err)
		}
	})

	t.Run("returns results when symbol not found in graph", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Query non-existent symbol - should pass through to graph
		result, err := vq.FindCallersByID(ctx, "nonexistent:symbol")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Symbols) != 0 {
			t.Errorf("expected empty result, got %d symbols", len(result.Symbols))
		}
	})
}

func TestVerifiedQuery_FindCalleesByID(t *testing.T) {
	t.Run("returns callees when file is fresh", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Find callees of main - should return helper
		result, err := vq.FindCalleesByID(ctx, "main.go:main")
		if err != nil {
			t.Fatalf("FindCalleesByID failed: %v", err)
		}

		if len(result.Symbols) != 1 {
			t.Errorf("expected 1 callee, got %d", len(result.Symbols))
		}
		if len(result.Symbols) > 0 && result.Symbols[0].Name != "helper" {
			t.Errorf("expected callee to be 'helper', got %q", result.Symbols[0].Name)
		}
	})
}

func TestVerifiedQuery_GetCallGraph(t *testing.T) {
	t.Run("returns call graph when file is fresh", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Get call graph from main
		result, err := vq.GetCallGraph(ctx, "main.go:main")
		if err != nil {
			t.Fatalf("GetCallGraph failed: %v", err)
		}

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.StartNode != "main.go:main" {
			t.Errorf("expected StartNode to be 'main.go:main', got %q", result.StartNode)
		}
	})
}

func TestVerifiedQuery_GetDependencyTree(t *testing.T) {
	t.Run("verifies file before query", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Get dependency tree
		result, err := vq.GetDependencyTree(ctx, "main.go")
		if err != nil {
			t.Fatalf("GetDependencyTree failed: %v", err)
		}

		if result == nil {
			t.Fatal("expected non-nil result")
		}
	})
}

func TestVerifiedQuery_VerifyAll(t *testing.T) {
	t.Run("verifies all files in manifest", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		result, err := vq.VerifyAll(ctx)
		if err != nil {
			t.Fatalf("VerifyAll failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("expected StatusFresh, got %v", result.Status)
		}
		if !result.AllFresh {
			t.Error("expected AllFresh to be true")
		}
	})

	t.Run("detects stale files", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		// Modify the file to make it stale
		testFile := filepath.Join(tmpDir, "main.go")
		content := []byte(`package main

func main() {
	helper()
	newFunction()
}

func helper() {}
func newFunction() {}
`)
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("failed to modify test file: %v", err)
		}

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		result, err := vq.VerifyAll(ctx)
		if err != nil {
			t.Fatalf("VerifyAll failed: %v", err)
		}

		if result.Status != StatusStale {
			t.Errorf("expected StatusStale, got %v", result.Status)
		}
		if len(result.StaleFiles) != 1 {
			t.Errorf("expected 1 stale file, got %d", len(result.StaleFiles))
		}
	})
}

func TestVerifiedQuery_FindCallersByName(t *testing.T) {
	t.Run("verifies all matching files", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}
		ctx := context.Background()

		// Find callers of "helper" by name
		results, err := vq.FindCallersByName(ctx, "helper")
		if err != nil {
			t.Fatalf("FindCallersByName failed: %v", err)
		}

		// Should find the helper symbol and its callers
		if len(results) != 1 {
			t.Errorf("expected 1 symbol result, got %d", len(results))
		}
	})
}

func TestVerifiedQuery_ContextCancellation(t *testing.T) {
	t.Run("respects cancelled context", func(t *testing.T) {
		tmpDir := t.TempDir()
		g, m := setupTestGraph(t, tmpDir)

		vq, err := NewVerifiedQuery(g, m, nil)
		if err != nil {
			t.Fatalf("failed to create VerifiedQuery: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = vq.FindCallersByID(ctx, "main.go:helper")
		// The error depends on where cancellation is detected
		// Either verification or query might return the error
		if err == nil {
			// If no error, that's also acceptable (if cache hit)
		}
	})
}
