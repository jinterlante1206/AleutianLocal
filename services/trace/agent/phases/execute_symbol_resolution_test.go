// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

import (
	"errors"
	"sync"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// Helper to create test symbol with minimal fields
func testSymbol(id, name string, kind ast.SymbolKind, line int) *ast.Symbol {
	return &ast.Symbol{
		ID:        id,
		Name:      name,
		Kind:      kind,
		FilePath:  "test.go",
		StartLine: line,
		EndLine:   line + 1,
		Language:  "go",
	}
}

// TestResolveSymbol_ExactMatch tests Strategy 1: exact symbol ID match.
func TestResolveSymbol_ExactMatch(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindStruct, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	resolvedID, confidence, strategy, err := resolveSymbol(deps, "pkg/handler.go:Handler")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected ID 'pkg/handler.go:Handler', got '%s'", resolvedID)
	}
	if confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", confidence)
	}
	if strategy != "exact" {
		t.Errorf("Expected strategy 'exact', got '%s'", strategy)
	}
}

// TestResolveSymbol_NameMatch_Single tests Strategy 2: single name match.
func TestResolveSymbol_NameMatch_Single(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindStruct, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	resolvedID, confidence, strategy, err := resolveSymbol(deps, "Handler")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected ID 'pkg/handler.go:Handler', got '%s'", resolvedID)
	}
	if confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", confidence)
	}
	if strategy != "name" {
		t.Errorf("Expected strategy 'name', got '%s'", strategy)
	}
}

// TestResolveSymbol_NameMatch_Multiple_PreferFunction tests disambigu ation: prefer function over struct.
func TestResolveSymbol_NameMatch_Multiple_PreferFunction(t *testing.T) {
	idx := index.NewSymbolIndex()

	structSymbol := testSymbol("pkg/types.go:Handler", "Handler", ast.SymbolKindStruct, 5)
	funcSymbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindFunction, 10)

	if err := idx.Add(structSymbol); err != nil {
		t.Fatalf("Failed to add struct symbol: %v", err)
	}
	if err := idx.Add(funcSymbol); err != nil {
		t.Fatalf("Failed to add function symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	resolvedID, confidence, strategy, err := resolveSymbol(deps, "Handler")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected function ID 'pkg/handler.go:Handler', got '%s'", resolvedID)
	}
	if confidence != 0.8 {
		t.Errorf("Expected confidence 0.8 (disambiguated), got %f", confidence)
	}
	if strategy != "name_disambiguated" {
		t.Errorf("Expected strategy 'name_disambiguated', got '%s'", strategy)
	}
}

// TestResolveSymbol_NoMatch tests failure case when no symbol matches.
func TestResolveSymbol_NoMatch(t *testing.T) {
	idx := index.NewSymbolIndex()
	deps := &Dependencies{SymbolIndex: idx}

	_, _, _, err := resolveSymbol(deps, "NonExistentSymbol")

	if err == nil {
		t.Fatal("Expected error for non-existent symbol, got nil")
	}
	// CB-31d: Check for typed error (M-R-1)
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Errorf("Expected ErrSymbolNotFound, got: %v", err)
	}
	if err.Error() != `symbol not found: "NonExistentSymbol"` {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

// TestResolveSymbol_NilDependencies tests error handling for nil dependencies.
func TestResolveSymbol_NilDependencies(t *testing.T) {
	_, _, _, err := resolveSymbol(nil, "Handler")

	if err == nil {
		t.Fatal("Expected error for nil dependencies, got nil")
	}
	// CB-31d: Check for typed error (M-R-1)
	if !errors.Is(err, ErrSymbolIndexNotAvailable) {
		t.Errorf("Expected ErrSymbolIndexNotAvailable, got: %v", err)
	}
}

// TestResolveSymbolCached_Hit tests cache hit path.
func TestResolveSymbolCached_Hit(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindFunction, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	var cache sync.Map
	sessionID := "test-session-123"

	// Pre-cache a resolution
	cache.Store("test-session-123:Handler", SymbolResolution{
		SymbolID:   "pkg/handler.go:Handler",
		Confidence: 0.95,
		Strategy:   "name",
	})

	resolvedID, confidence, err := resolveSymbolCached(&cache, sessionID, "Handler", deps)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected cached ID, got '%s'", resolvedID)
	}
	if confidence != 0.95 {
		t.Errorf("Expected cached confidence 0.95, got %f", confidence)
	}
}

// TestResolveSymbolCached_Miss tests cache miss path.
func TestResolveSymbolCached_Miss(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindFunction, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	var cache sync.Map
	sessionID := "test-session-456"

	resolvedID, confidence, err := resolveSymbolCached(&cache, sessionID, "Handler", deps)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected resolved ID, got '%s'", resolvedID)
	}
	if confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", confidence)
	}

	// Verify: Result should now be in cache
	cacheKey := "test-session-456:Handler"
	cached, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("Expected result to be cached")
	}
	cachedResult, ok := cached.(SymbolResolution)
	if !ok {
		t.Fatal("Expected SymbolResolution type in cache")
	}
	if cachedResult.SymbolID != "pkg/handler.go:Handler" {
		t.Errorf("Expected cached ID to match, got '%s'", cachedResult.SymbolID)
	}
}

// TestResolveSymbolCached_ConcurrentAccess tests thread safety of cached resolution.
func TestResolveSymbolCached_ConcurrentAccess(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindFunction, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	var cache sync.Map
	sessionID := "concurrent-session"

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := resolveSymbolCached(&cache, sessionID, "Handler", deps)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent access caused error: %v", err)
	}

	// Verify: Cache should contain exactly one entry
	count := 0
	cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	if count != 1 {
		t.Errorf("Expected 1 cached entry, got %d", count)
	}
}

// TestResolveSymbol_PreferMethod tests that methods are preferred like functions.
func TestResolveSymbol_PreferMethod(t *testing.T) {
	idx := index.NewSymbolIndex()

	typeSymbol := testSymbol("pkg/types.go:Execute", "Execute", ast.SymbolKindStruct, 5)
	methodSymbol := testSymbol("pkg/handler.go:Handler.Execute", "Execute", ast.SymbolKindMethod, 20)

	if err := idx.Add(typeSymbol); err != nil {
		t.Fatalf("Failed to add type: %v", err)
	}
	if err := idx.Add(methodSymbol); err != nil {
		t.Fatalf("Failed to add method: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	resolvedID, confidence, strategy, err := resolveSymbol(deps, "Execute")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler.Execute" {
		t.Errorf("Expected method ID, got '%s'", resolvedID)
	}
	if confidence != 0.8 {
		t.Errorf("Expected confidence 0.8 (disambiguated), got %f", confidence)
	}
	if strategy != "name_disambiguated" {
		t.Errorf("Expected strategy 'name_disambiguated', got '%s'", strategy)
	}
}

// TestResolveSymbol_EmptyName tests error handling for empty symbol name.
func TestResolveSymbol_EmptyName(t *testing.T) {
	idx := index.NewSymbolIndex()
	deps := &Dependencies{SymbolIndex: idx}

	_, _, _, err := resolveSymbol(deps, "")

	if err == nil {
		t.Fatal("Expected error for empty name, got nil")
	}
	// CB-31d: Check for typed error (M-R-1)
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Errorf("Expected ErrSymbolNotFound, got: %v", err)
	}
	if err.Error() != `symbol not found: ""` {
		t.Errorf("Expected specific error message for empty name, got: %v", err)
	}
}

// TestResolveSymbol_SpecialCharacters tests symbol resolution with special characters.
func TestResolveSymbol_SpecialCharacters(t *testing.T) {
	idx := index.NewSymbolIndex()
	// Add symbol with special characters in ID
	symbol := testSymbol("pkg/file.go:New<T>", "New<T>", ast.SymbolKindFunction, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx}

	// Test exact match with special characters
	resolvedID, confidence, strategy, err := resolveSymbol(deps, "pkg/file.go:New<T>")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resolvedID != "pkg/file.go:New<T>" {
		t.Errorf("Expected exact match with special chars, got '%s'", resolvedID)
	}
	if confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", confidence)
	}
	if strategy != "exact" {
		t.Errorf("Expected strategy 'exact', got '%s'", strategy)
	}
}

// TestResolveSymbolCached_NilSession tests caching with nil session.
func TestResolveSymbolCached_NilSession(t *testing.T) {
	idx := index.NewSymbolIndex()
	symbol := testSymbol("pkg/handler.go:Handler", "Handler", ast.SymbolKindFunction, 10)
	if err := idx.Add(symbol); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}

	deps := &Dependencies{SymbolIndex: idx, Session: nil}

	var cache sync.Map
	// Empty session ID (should still work, just cache key is ":Handler")
	resolvedID, confidence, err := resolveSymbolCached(&cache, "", "Handler", deps)
	if err != nil {
		t.Fatalf("Expected no error with nil session, got: %v", err)
	}

	if resolvedID != "pkg/handler.go:Handler" {
		t.Errorf("Expected resolved ID, got '%s'", resolvedID)
	}
	if confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", confidence)
	}

	// Verify caching still works with empty session ID
	cacheKey := ":Handler"
	cached, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("Expected result to be cached even with empty session ID")
	}
	cachedResult, ok := cached.(SymbolResolution)
	if !ok {
		t.Fatal("Expected SymbolResolution type in cache")
	}
	if cachedResult.SymbolID != "pkg/handler.go:Handler" {
		t.Errorf("Expected cached ID to match, got '%s'", cachedResult.SymbolID)
	}
}
