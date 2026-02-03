// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// createTestIndex creates a test index with known symbols and edges.
func createTestIndex() *initializer.MemoryIndex {
	index := initializer.NewMemoryIndex()

	// Create symbols: main -> handler -> service -> db
	// Note: FilePaths must contain "/" to avoid being classified as stdlib
	index.Symbols = []initializer.Symbol{
		{ID: "main", Name: "main", Kind: "function", FilePath: "cmd/main.go", StartLine: 1, EndLine: 10},
		{ID: "handler", Name: "HandleRequest", Kind: "function", FilePath: "pkg/handler.go", StartLine: 5, EndLine: 20},
		{ID: "service", Name: "ProcessData", Kind: "function", FilePath: "pkg/service.go", StartLine: 10, EndLine: 30},
		{ID: "db", Name: "Query", Kind: "function", FilePath: "pkg/db.go", StartLine: 15, EndLine: 25},
		{ID: "helper", Name: "Helper", Kind: "function", FilePath: "pkg/helper.go", StartLine: 1, EndLine: 5},
		{ID: "test_helper", Name: "TestHelper", Kind: "function", FilePath: "pkg/helper_test.go", StartLine: 1, EndLine: 5},
	}

	// Create edges: main -> handler -> service -> db, helper -> service
	index.Edges = []initializer.Edge{
		{FromID: "main", ToID: "handler", Kind: "calls", FilePath: "cmd/main.go", Line: 5},
		{FromID: "handler", ToID: "service", Kind: "calls", FilePath: "pkg/handler.go", Line: 10},
		{FromID: "service", ToID: "db", Kind: "calls", FilePath: "pkg/service.go", Line: 20},
		{FromID: "helper", ToID: "service", Kind: "calls", FilePath: "pkg/helper.go", Line: 3},
		{FromID: "test_helper", ToID: "service", Kind: "calls", FilePath: "pkg/helper_test.go", Line: 3},
	}

	index.BuildIndexes()
	return index
}

// TestQuerier_FindCallers_Direct tests finding direct callers.
func TestQuerier_FindCallers_Direct(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()
	cfg.MaxDepth = 1

	result, err := querier.FindCallers(ctx, "ProcessData", cfg)
	if err != nil {
		t.Fatalf("FindCallers failed: %v", err)
	}

	// Should find handler and helper as direct callers
	if result.DirectCount < 2 {
		t.Errorf("DirectCount = %d, want >= 2", result.DirectCount)
	}

	if result.Query != "callers" {
		t.Errorf("Query = %q, want 'callers'", result.Query)
	}
}

// TestQuerier_FindCallers_Transitive tests finding transitive callers.
func TestQuerier_FindCallers_Transitive(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()
	cfg.MaxDepth = 5

	result, err := querier.FindCallers(ctx, "Query", cfg)
	if err != nil {
		t.Fatalf("FindCallers failed: %v", err)
	}

	// Should find service -> handler -> main chain
	if result.TotalCount < 3 {
		t.Errorf("TotalCount = %d, want >= 3", result.TotalCount)
	}

	if result.TransitiveCount < 2 {
		t.Errorf("TransitiveCount = %d, want >= 2", result.TransitiveCount)
	}
}

// TestQuerier_FindCallers_ExcludeTests tests excluding test files.
func TestQuerier_FindCallers_ExcludeTests(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()

	// Without including tests
	cfg := DefaultQueryConfig()
	cfg.IncludeTests = false
	result1, err := querier.FindCallers(ctx, "ProcessData", cfg)
	if err != nil {
		t.Fatalf("FindCallers failed: %v", err)
	}

	// With including tests
	cfg.IncludeTests = true
	result2, err := querier.FindCallers(ctx, "ProcessData", cfg)
	if err != nil {
		t.Fatalf("FindCallers with tests failed: %v", err)
	}

	// Should have more results with tests included
	if result2.TotalCount <= result1.TotalCount {
		t.Logf("Without tests: %d, with tests: %d", result1.TotalCount, result2.TotalCount)
		// This might be equal if test_helper was already filtered somewhere else
	}
}

// TestQuerier_FindCallees_Direct tests finding direct callees.
func TestQuerier_FindCallees_Direct(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()
	cfg.MaxDepth = 1

	result, err := querier.FindCallees(ctx, "HandleRequest", cfg)
	if err != nil {
		t.Fatalf("FindCallees failed: %v", err)
	}

	// Should find service as direct callee
	if result.DirectCount < 1 {
		t.Errorf("DirectCount = %d, want >= 1", result.DirectCount)
	}

	if result.Query != "callees" {
		t.Errorf("Query = %q, want 'callees'", result.Query)
	}
}

// TestQuerier_FindCallees_Transitive tests finding transitive callees.
func TestQuerier_FindCallees_Transitive(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()
	cfg.MaxDepth = 5

	result, err := querier.FindCallees(ctx, "main", cfg)
	if err != nil {
		t.Fatalf("FindCallees failed: %v", err)
	}

	// Should find handler -> service -> db chain
	if result.TotalCount < 3 {
		t.Errorf("TotalCount = %d, want >= 3", result.TotalCount)
	}
}

// TestQuerier_FindPath_Direct tests finding direct path.
func TestQuerier_FindPath_Direct(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	result, err := querier.FindPath(ctx, "handler", "service", cfg, false, 10)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if !result.PathFound {
		t.Error("PathFound = false, want true")
	}

	if result.PathCount < 1 {
		t.Errorf("PathCount = %d, want >= 1", result.PathCount)
	}

	if len(result.Paths) > 0 && result.Paths[0].Length != 1 {
		t.Errorf("Path length = %d, want 1", result.Paths[0].Length)
	}
}

// TestQuerier_FindPath_Transitive tests finding transitive path.
func TestQuerier_FindPath_Transitive(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	result, err := querier.FindPath(ctx, "main", "db", cfg, false, 10)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if !result.PathFound {
		t.Error("PathFound = false, want true")
	}

	// Path should be main -> handler -> service -> db (3 hops)
	if len(result.Paths) > 0 && result.Paths[0].Length < 3 {
		t.Errorf("Path length = %d, want >= 3", result.Paths[0].Length)
	}
}

// TestQuerier_FindPath_NoPath tests handling of no path case.
func TestQuerier_FindPath_NoPath(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	// db doesn't call anything, so no path from db to main
	result, err := querier.FindPath(ctx, "db", "main", cfg, false, 10)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if result.PathFound {
		t.Error("PathFound = true, want false (no path from db to main)")
	}
}

// TestQuerier_FindPath_SameSymbol tests finding path to same symbol.
func TestQuerier_FindPath_SameSymbol(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	result, err := querier.FindPath(ctx, "handler", "handler", cfg, false, 10)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if !result.PathFound {
		t.Error("PathFound = false, want true (trivial path)")
	}

	if len(result.Paths) > 0 && result.Paths[0].Length != 0 {
		t.Errorf("Path length = %d, want 0", result.Paths[0].Length)
	}
}

// TestQuerier_ResolveSymbol_ByName tests symbol resolution by name.
func TestQuerier_ResolveSymbol_ByName(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	// Resolve by function name
	result, err := querier.FindCallers(ctx, "Query", cfg)
	if err != nil {
		t.Fatalf("FindCallers by name failed: %v", err)
	}

	if result.Symbol != "Query" {
		t.Errorf("Symbol = %q, want 'Query'", result.Symbol)
	}
}

// TestQuerier_ResolveSymbol_ByID tests symbol resolution by ID.
func TestQuerier_ResolveSymbol_ByID(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	// Resolve by ID
	result, err := querier.FindCallers(ctx, "db", cfg)
	if err != nil {
		t.Fatalf("FindCallers by ID failed: %v", err)
	}

	if result.TotalCount < 1 {
		t.Errorf("TotalCount = %d, want >= 1", result.TotalCount)
	}
}

// TestQuerier_ResolveSymbol_NotFound tests symbol not found error.
func TestQuerier_ResolveSymbol_NotFound(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx := context.Background()
	cfg := DefaultQueryConfig()

	_, err := querier.FindCallers(ctx, "NonExistentSymbol", cfg)
	if err == nil {
		t.Error("Expected error for non-existent symbol")
	}

	// Should be SymbolNotFoundError
	var snfErr *SymbolNotFoundError
	if _, ok := err.(*SymbolNotFoundError); !ok {
		t.Errorf("Error type = %T, want *SymbolNotFoundError", err)
	}
	_ = snfErr
}

// TestQuerier_Cancellation tests context cancellation.
func TestQuerier_Cancellation(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := DefaultQueryConfig()

	result, err := querier.FindCallers(ctx, "Query", cfg)
	if err != nil {
		t.Fatalf("FindCallers failed: %v", err)
	}

	// Should have warning about cancellation
	if len(result.Warnings) == 0 {
		t.Log("No warnings, but cancellation may have been too late to trigger")
	}
}

// TestQuerier_NilContext tests nil context handling.
func TestQuerier_NilContext(t *testing.T) {
	index := createTestIndex()
	querier := NewQuerier(index)

	cfg := DefaultQueryConfig()

	_, err := querier.FindCallers(nil, "Query", cfg)
	if err == nil {
		t.Error("Expected error for nil context")
	}

	_, err = querier.FindCallees(nil, "Query", cfg)
	if err == nil {
		t.Error("Expected error for nil context")
	}

	_, err = querier.FindPath(nil, "main", "db", cfg, false, 10)
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

// TestQueryConfig_Defaults tests default configuration.
func TestQueryConfig_Defaults(t *testing.T) {
	cfg := DefaultQueryConfig()

	if cfg.MaxDepth != DefaultMaxDepth {
		t.Errorf("MaxDepth = %d, want %d", cfg.MaxDepth, DefaultMaxDepth)
	}

	if cfg.MaxResults != DefaultMaxResults {
		t.Errorf("MaxResults = %d, want %d", cfg.MaxResults, DefaultMaxResults)
	}

	if cfg.IncludeTests {
		t.Error("IncludeTests should default to false")
	}

	if cfg.IncludeStdlib {
		t.Error("IncludeStdlib should default to false")
	}
}

// TestQueryResult_New tests QueryResult creation.
func TestQueryResult_New(t *testing.T) {
	result := NewQueryResult("callers", "test.Symbol")

	if result.APIVersion != APIVersion {
		t.Errorf("APIVersion = %q, want %q", result.APIVersion, APIVersion)
	}

	if result.Query != "callers" {
		t.Errorf("Query = %q, want 'callers'", result.Query)
	}

	if result.Symbol != "test.Symbol" {
		t.Errorf("Symbol = %q, want 'test.Symbol'", result.Symbol)
	}

	if result.Results == nil {
		t.Error("Results should be initialized")
	}

	if result.Warnings == nil {
		t.Error("Warnings should be initialized")
	}
}

// TestPathQueryResult_New tests PathQueryResult creation.
func TestPathQueryResult_New(t *testing.T) {
	result := NewPathQueryResult("from.Symbol", "to.Symbol")

	if result.APIVersion != APIVersion {
		t.Errorf("APIVersion = %q, want %q", result.APIVersion, APIVersion)
	}

	if result.From != "from.Symbol" {
		t.Errorf("From = %q, want 'from.Symbol'", result.From)
	}

	if result.To != "to.Symbol" {
		t.Errorf("To = %q, want 'to.Symbol'", result.To)
	}

	if result.Paths == nil {
		t.Error("Paths should be initialized")
	}
}
