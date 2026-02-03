// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package index

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// Helper function to create a valid test symbol.
func makeSymbol(id, name string, kind ast.SymbolKind, filePath string) *ast.Symbol {
	return &ast.Symbol{
		ID:            id,
		Name:          name,
		Kind:          kind,
		FilePath:      filePath,
		StartLine:     1,
		EndLine:       10,
		StartCol:      0,
		EndCol:        50,
		Language:      "go",
		ParsedAtMilli: time.Now().UnixMilli(),
	}
}

// Test data for consistent testing.
var testSymbols = []*ast.Symbol{
	makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go"),
	makeSymbol("handler.go:10:HandleAgent", "HandleAgent", ast.SymbolKindFunction, "handler.go"),
	makeSymbol("handler.go:20:HandleChat", "HandleChat", ast.SymbolKindFunction, "handler.go"),
	makeSymbol("types.go:5:Request", "Request", ast.SymbolKindStruct, "types.go"),
}

func TestNewSymbolIndex(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		idx := NewSymbolIndex()
		stats := idx.Stats()

		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0 symbols, got %d", stats.TotalSymbols)
		}
		if stats.MaxSymbols != DefaultMaxSymbols {
			t.Errorf("expected max %d, got %d", DefaultMaxSymbols, stats.MaxSymbols)
		}
	})

	t.Run("custom max symbols", func(t *testing.T) {
		idx := NewSymbolIndex(WithMaxSymbols(100))
		stats := idx.Stats()

		if stats.MaxSymbols != 100 {
			t.Errorf("expected max 100, got %d", stats.MaxSymbols)
		}
	})
}

func TestSymbolIndex_Add(t *testing.T) {
	t.Run("add single symbol success", func(t *testing.T) {
		idx := NewSymbolIndex()
		sym := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")

		err := idx.Add(sym)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify retrievable by all indexes
		if got, ok := idx.GetByID("main.go:1:main"); !ok || got != sym {
			t.Error("GetByID failed")
		}

		byName := idx.GetByName("main")
		if len(byName) != 1 || byName[0] != sym {
			t.Error("GetByName failed")
		}

		byFile := idx.GetByFile("main.go")
		if len(byFile) != 1 || byFile[0] != sym {
			t.Error("GetByFile failed")
		}

		byKind := idx.GetByKind(ast.SymbolKindFunction)
		if len(byKind) != 1 || byKind[0] != sym {
			t.Error("GetByKind failed")
		}
	})

	t.Run("add nil symbol returns error", func(t *testing.T) {
		idx := NewSymbolIndex()

		err := idx.Add(nil)
		if err == nil {
			t.Fatal("expected error for nil symbol")
		}
		if !errors.Is(err, ErrInvalidSymbol) {
			t.Errorf("expected ErrInvalidSymbol, got %v", err)
		}
	})

	t.Run("add invalid symbol returns error", func(t *testing.T) {
		idx := NewSymbolIndex()
		sym := &ast.Symbol{} // Empty symbol fails validation

		err := idx.Add(sym)
		if err == nil {
			t.Fatal("expected error for invalid symbol")
		}
		if !errors.Is(err, ErrInvalidSymbol) {
			t.Errorf("expected ErrInvalidSymbol, got %v", err)
		}
	})

	t.Run("add duplicate ID returns error", func(t *testing.T) {
		idx := NewSymbolIndex()
		sym1 := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")
		sym2 := makeSymbol("main.go:1:main", "other", ast.SymbolKindVariable, "main.go")

		if err := idx.Add(sym1); err != nil {
			t.Fatalf("first add failed: %v", err)
		}

		err := idx.Add(sym2)
		if err == nil {
			t.Fatal("expected error for duplicate ID")
		}
		if !errors.Is(err, ErrDuplicateSymbol) {
			t.Errorf("expected ErrDuplicateSymbol, got %v", err)
		}

		// Verify original symbol still in index
		if got, ok := idx.GetByID("main.go:1:main"); !ok || got != sym1 {
			t.Error("original symbol should still be in index")
		}
	})

	t.Run("add at max capacity returns error", func(t *testing.T) {
		idx := NewSymbolIndex(WithMaxSymbols(2))

		sym1 := makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:c", "c", ast.SymbolKindFunction, "c.go")

		if err := idx.Add(sym1); err != nil {
			t.Fatalf("add 1 failed: %v", err)
		}
		if err := idx.Add(sym2); err != nil {
			t.Fatalf("add 2 failed: %v", err)
		}

		err := idx.Add(sym3)
		if err == nil {
			t.Fatal("expected error at max capacity")
		}
		if !errors.Is(err, ErrMaxSymbolsExceeded) {
			t.Errorf("expected ErrMaxSymbolsExceeded, got %v", err)
		}
	})
}

func TestSymbolIndex_AddBatch(t *testing.T) {
	t.Run("add batch success", func(t *testing.T) {
		idx := NewSymbolIndex()

		err := idx.AddBatch(testSymbols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != len(testSymbols) {
			t.Errorf("expected %d symbols, got %d", len(testSymbols), stats.TotalSymbols)
		}

		// Verify all symbols are retrievable
		for _, sym := range testSymbols {
			if got, ok := idx.GetByID(sym.ID); !ok || got != sym {
				t.Errorf("symbol %s not found", sym.ID)
			}
		}
	})

	t.Run("add empty batch is noop", func(t *testing.T) {
		idx := NewSymbolIndex()

		err := idx.AddBatch(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = idx.AddBatch([]*ast.Symbol{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("add batch with invalid symbol fails atomically", func(t *testing.T) {
		idx := NewSymbolIndex()

		batch := []*ast.Symbol{
			makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go"),
			{}, // Invalid
			makeSymbol("c.go:1:c", "c", ast.SymbolKindFunction, "c.go"),
		}

		err := idx.AddBatch(batch)
		if err == nil {
			t.Fatal("expected error for invalid symbol in batch")
		}

		var batchErr *BatchError
		if !errors.As(err, &batchErr) {
			t.Fatalf("expected BatchError, got %T", err)
		}

		// Verify NO symbols were added
		stats := idx.Stats()
		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0 symbols (atomic failure), got %d", stats.TotalSymbols)
		}
	})

	t.Run("add batch with nil symbol fails atomically", func(t *testing.T) {
		idx := NewSymbolIndex()

		batch := []*ast.Symbol{
			makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go"),
			nil,
		}

		err := idx.AddBatch(batch)
		if err == nil {
			t.Fatal("expected error for nil symbol in batch")
		}

		var batchErr *BatchError
		if !errors.As(err, &batchErr) {
			t.Fatalf("expected BatchError, got %T", err)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0 symbols (atomic failure), got %d", stats.TotalSymbols)
		}
	})

	t.Run("add batch with duplicate in batch fails atomically", func(t *testing.T) {
		idx := NewSymbolIndex()

		batch := []*ast.Symbol{
			makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go"),
			makeSymbol("a.go:1:a", "other", ast.SymbolKindVariable, "a.go"), // Same ID
		}

		err := idx.AddBatch(batch)
		if err == nil {
			t.Fatal("expected error for duplicate in batch")
		}

		var batchErr *BatchError
		if !errors.As(err, &batchErr) {
			t.Fatalf("expected BatchError, got %T", err)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0 symbols (atomic failure), got %d", stats.TotalSymbols)
		}
	})

	t.Run("add batch with existing duplicate fails atomically", func(t *testing.T) {
		idx := NewSymbolIndex()

		// Add one symbol first
		existing := makeSymbol("existing.go:1:existing", "existing", ast.SymbolKindFunction, "existing.go")
		if err := idx.Add(existing); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Try to add batch containing duplicate
		batch := []*ast.Symbol{
			makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go"),
			makeSymbol("existing.go:1:existing", "different", ast.SymbolKindVariable, "existing.go"), // Duplicate
		}

		err := idx.AddBatch(batch)
		if err == nil {
			t.Fatal("expected error for duplicate with existing")
		}

		var batchErr *BatchError
		if !errors.As(err, &batchErr) {
			t.Fatalf("expected BatchError, got %T", err)
		}

		// Only original symbol should exist
		stats := idx.Stats()
		if stats.TotalSymbols != 1 {
			t.Errorf("expected 1 symbol (only original), got %d", stats.TotalSymbols)
		}
	})

	t.Run("add batch exceeding capacity fails", func(t *testing.T) {
		idx := NewSymbolIndex(WithMaxSymbols(2))

		batch := []*ast.Symbol{
			makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go"),
			makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go"),
			makeSymbol("c.go:1:c", "c", ast.SymbolKindFunction, "c.go"),
		}

		err := idx.AddBatch(batch)
		if !errors.Is(err, ErrMaxSymbolsExceeded) {
			t.Errorf("expected ErrMaxSymbolsExceeded, got %v", err)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0 symbols (atomic failure), got %d", stats.TotalSymbols)
		}
	})
}

func TestSymbolIndex_GetBy(t *testing.T) {
	idx := NewSymbolIndex()
	if err := idx.AddBatch(testSymbols); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("GetByID existing", func(t *testing.T) {
		sym, ok := idx.GetByID("handler.go:10:HandleAgent")
		if !ok {
			t.Fatal("expected to find symbol")
		}
		if sym.Name != "HandleAgent" {
			t.Errorf("wrong symbol: %s", sym.Name)
		}
	})

	t.Run("GetByID non-existent", func(t *testing.T) {
		_, ok := idx.GetByID("does-not-exist")
		if ok {
			t.Error("expected not to find symbol")
		}
	})

	t.Run("GetByName with multiple matches", func(t *testing.T) {
		// Add another symbol with same name in different file
		dup := makeSymbol("other.go:1:main", "main", ast.SymbolKindFunction, "other.go")
		if err := idx.Add(dup); err != nil {
			t.Fatalf("add failed: %v", err)
		}

		results := idx.GetByName("main")
		if len(results) != 2 {
			t.Errorf("expected 2 matches, got %d", len(results))
		}
	})

	t.Run("GetByName non-existent", func(t *testing.T) {
		results := idx.GetByName("does-not-exist")
		if results != nil {
			t.Errorf("expected nil, got %v", results)
		}
	})

	t.Run("GetByFile returns multiple symbols", func(t *testing.T) {
		results := idx.GetByFile("handler.go")
		if len(results) != 2 {
			t.Errorf("expected 2 symbols in handler.go, got %d", len(results))
		}
	})

	t.Run("GetByKind returns correct symbols", func(t *testing.T) {
		// We added 4 test symbols (3 functions + 1 struct) + 1 duplicate main = 4 functions
		functions := idx.GetByKind(ast.SymbolKindFunction)
		if len(functions) != 4 {
			t.Errorf("expected 4 functions, got %d", len(functions))
		}

		structs := idx.GetByKind(ast.SymbolKindStruct)
		if len(structs) != 1 {
			t.Errorf("expected 1 struct, got %d", len(structs))
		}
	})

	t.Run("GetBy* returns defensive copy", func(t *testing.T) {
		// Get original
		results1 := idx.GetByFile("handler.go")
		origLen := len(results1)

		// Modify returned slice
		results1[0] = nil
		results1 = append(results1, nil, nil, nil)

		// Get again - should be unchanged
		results2 := idx.GetByFile("handler.go")
		if len(results2) != origLen {
			t.Errorf("index was mutated: expected %d, got %d", origLen, len(results2))
		}
		if results2[0] == nil {
			t.Error("index was mutated: first element is nil")
		}
	})
}

func TestSymbolIndex_Search(t *testing.T) {
	idx := NewSymbolIndex()
	if err := idx.AddBatch(testSymbols); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Run("exact match", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "HandleAgent", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected results")
		}
		if results[0].Name != "HandleAgent" {
			t.Errorf("expected HandleAgent first, got %s", results[0].Name)
		}
	})

	t.Run("prefix match", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "Handle", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 Handle* matches, got %d", len(results))
		}
	})

	t.Run("substring match", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "Agent", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected substring match")
		}
	})

	t.Run("fuzzy match", func(t *testing.T) {
		// "main" vs "mian" has Levenshtein distance of 2
		results, err := idx.Search(context.Background(), "mian", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected fuzzy match for 'mian' -> 'main'")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "handleagent", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected case-insensitive match")
		}
	})

	t.Run("limit results", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "a", 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) > 2 {
			t.Errorf("expected max 2 results, got %d", len(results))
		}
	})

	t.Run("empty query returns nil", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if results != nil {
			t.Errorf("expected nil for empty query, got %v", results)
		}
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := idx.Search(ctx, "test", 10)
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("no matches returns empty slice", func(t *testing.T) {
		results, err := idx.Search(context.Background(), "xyznonexistent", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})
}

func TestSymbolIndex_RemoveByFile(t *testing.T) {
	t.Run("remove existing file", func(t *testing.T) {
		idx := NewSymbolIndex()
		if err := idx.AddBatch(testSymbols); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		initialCount := idx.Stats().TotalSymbols

		removed := idx.RemoveByFile("handler.go")
		if removed != 2 {
			t.Errorf("expected 2 removed, got %d", removed)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != initialCount-2 {
			t.Errorf("expected %d symbols, got %d", initialCount-2, stats.TotalSymbols)
		}

		// Verify symbols are gone from all indexes
		if _, ok := idx.GetByID("handler.go:10:HandleAgent"); ok {
			t.Error("symbol should be removed from byID")
		}

		byFile := idx.GetByFile("handler.go")
		if byFile != nil {
			t.Error("file should have no symbols")
		}

		// Verify other files unaffected
		if _, ok := idx.GetByID("main.go:1:main"); !ok {
			t.Error("main.go symbol should still exist")
		}
	})

	t.Run("remove non-existent file returns 0", func(t *testing.T) {
		idx := NewSymbolIndex()

		removed := idx.RemoveByFile("does-not-exist.go")
		if removed != 0 {
			t.Errorf("expected 0 removed, got %d", removed)
		}
	})

	t.Run("remove updates counters correctly", func(t *testing.T) {
		idx := NewSymbolIndex()
		if err := idx.AddBatch(testSymbols); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// handler.go has 2 functions
		initialFunctions := idx.Stats().ByKind[ast.SymbolKindFunction]

		idx.RemoveByFile("handler.go")

		stats := idx.Stats()
		expectedFunctions := initialFunctions - 2
		if stats.ByKind[ast.SymbolKindFunction] != expectedFunctions {
			t.Errorf("expected %d functions, got %d", expectedFunctions, stats.ByKind[ast.SymbolKindFunction])
		}

		// FileCount should decrease
		if stats.FileCount != 2 { // main.go and types.go remain
			t.Errorf("expected 2 files, got %d", stats.FileCount)
		}
	})
}

func TestSymbolIndex_Clear(t *testing.T) {
	idx := NewSymbolIndex()
	if err := idx.AddBatch(testSymbols); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	idx.Clear()

	stats := idx.Stats()
	if stats.TotalSymbols != 0 {
		t.Errorf("expected 0 symbols after clear, got %d", stats.TotalSymbols)
	}
	if stats.FileCount != 0 {
		t.Errorf("expected 0 files after clear, got %d", stats.FileCount)
	}
	if len(stats.ByKind) != 0 {
		t.Errorf("expected empty ByKind after clear, got %v", stats.ByKind)
	}

	// Verify can add after clear
	sym := makeSymbol("new.go:1:new", "new", ast.SymbolKindFunction, "new.go")
	if err := idx.Add(sym); err != nil {
		t.Errorf("add after clear failed: %v", err)
	}
}

func TestSymbolIndex_Stats(t *testing.T) {
	idx := NewSymbolIndex(WithMaxSymbols(500))

	t.Run("empty index", func(t *testing.T) {
		stats := idx.Stats()
		if stats.TotalSymbols != 0 {
			t.Errorf("expected 0, got %d", stats.TotalSymbols)
		}
		if stats.FileCount != 0 {
			t.Errorf("expected 0 files, got %d", stats.FileCount)
		}
		if stats.MaxSymbols != 500 {
			t.Errorf("expected max 500, got %d", stats.MaxSymbols)
		}
	})

	t.Run("after adding symbols", func(t *testing.T) {
		if err := idx.AddBatch(testSymbols); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		stats := idx.Stats()
		if stats.TotalSymbols != 4 {
			t.Errorf("expected 4, got %d", stats.TotalSymbols)
		}
		if stats.FileCount != 3 { // main.go, handler.go, types.go
			t.Errorf("expected 3 files, got %d", stats.FileCount)
		}
		if stats.ByKind[ast.SymbolKindFunction] != 3 {
			t.Errorf("expected 3 functions, got %d", stats.ByKind[ast.SymbolKindFunction])
		}
		if stats.ByKind[ast.SymbolKindStruct] != 1 {
			t.Errorf("expected 1 struct, got %d", stats.ByKind[ast.SymbolKindStruct])
		}
	})

	t.Run("stats is O(1) - returns copy of counters", func(t *testing.T) {
		stats1 := idx.Stats()
		stats1.ByKind[ast.SymbolKindFunction] = 9999

		stats2 := idx.Stats()
		if stats2.ByKind[ast.SymbolKindFunction] == 9999 {
			t.Error("stats should return independent copies")
		}
	})
}

func TestSymbolIndex_Concurrent(t *testing.T) {
	idx := NewSymbolIndex()

	// Pre-populate with some symbols
	for i := 0; i < 100; i++ {
		sym := makeSymbol(
			"file.go:"+string(rune('0'+i/10))+string(rune('0'+i%10))+":sym",
			"sym"+string(rune('0'+i/10))+string(rune('0'+i%10)),
			ast.SymbolKindFunction,
			"file.go",
		)
		// Need unique IDs
		sym.ID = "file.go:" + string(rune(i)) + ":sym" + string(rune(i))
		if err := idx.Add(sym); err != nil {
			t.Fatalf("setup failed at %d: %v", i, err)
		}
	}

	t.Run("concurrent reads", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					idx.Stats()
					idx.GetByFile("file.go")
					idx.GetByKind(ast.SymbolKindFunction)
					_, _ = idx.Search(context.Background(), "sym", 10)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("concurrent read and write", func(t *testing.T) {
		var wg sync.WaitGroup

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					idx.Stats()
					idx.GetByKind(ast.SymbolKindFunction)
				}
			}()
		}

		// Writers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					sym := makeSymbol(
						"concurrent.go:"+string(rune(workerID))+"_"+string(rune(j))+":fn",
						"concurrent_fn",
						ast.SymbolKindFunction,
						"concurrent.go",
					)
					sym.ID = "concurrent.go:" + string(rune(workerID*100+j)) + ":fn"
					_ = idx.Add(sym) // May fail with duplicate, that's OK
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestBatchError(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := &BatchError{Errors: []error{errors.New("test error")}}
		if err.Error() != "test error" {
			t.Errorf("unexpected message: %s", err.Error())
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		err := &BatchError{Errors: []error{
			errors.New("first"),
			errors.New("second"),
			errors.New("third"),
		}}
		msg := err.Error()
		if msg != "3 errors: first (and 2 more)" {
			t.Errorf("unexpected message: %s", msg)
		}
	})

	t.Run("error list", func(t *testing.T) {
		err := &BatchError{Errors: []error{
			errors.New("first"),
			errors.New("second"),
		}}
		list := err.ErrorList()
		expected := "first\nsecond"
		if list != expected {
			t.Errorf("expected %q, got %q", expected, list)
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		inner1 := errors.New("inner1")
		inner2 := errors.New("inner2")
		err := &BatchError{Errors: []error{inner1, inner2}}

		unwrapped := err.Unwrap()
		if len(unwrapped) != 2 {
			t.Errorf("expected 2 unwrapped, got %d", len(unwrapped))
		}
	})

	t.Run("errors.Is works with wrapped errors", func(t *testing.T) {
		err := &BatchError{Errors: []error{
			ErrDuplicateSymbol,
			ErrInvalidSymbol,
		}}

		if !errors.Is(err, ErrDuplicateSymbol) {
			t.Error("errors.Is should find ErrDuplicateSymbol")
		}
		if !errors.Is(err, ErrInvalidSymbol) {
			t.Error("errors.Is should find ErrInvalidSymbol")
		}
	})
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"a", "a", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"main", "mian", 2},
		{"HandleAgent", "HandleAgent", 0},
		{"handle", "Handle", 1}, // Case difference
	}

	for _, tc := range tests {
		got := levenshteinDistance(tc.a, tc.b)
		if got != tc.expected {
			t.Errorf("levenshtein(%q, %q) = %d, expected %d", tc.a, tc.b, got, tc.expected)
		}
	}
}

func TestSymbolIndex_Clone(t *testing.T) {
	t.Run("clone creates independent copy", func(t *testing.T) {
		// Create original index with symbols
		idx := NewSymbolIndex(WithMaxSymbols(1000))
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("a.go:10:TypeA", "TypeA", ast.SymbolKindStruct, "a.go")

		idx.Add(sym1)
		idx.Add(sym2)
		idx.Add(sym3)

		// Clone
		clone := idx.Clone()

		// Verify clone has same counts
		origStats := idx.Stats()
		cloneStats := clone.Stats()

		if cloneStats.TotalSymbols != origStats.TotalSymbols {
			t.Errorf("clone TotalSymbols = %d, expected %d", cloneStats.TotalSymbols, origStats.TotalSymbols)
		}
		if cloneStats.FileCount != origStats.FileCount {
			t.Errorf("clone FileCount = %d, expected %d", cloneStats.FileCount, origStats.FileCount)
		}
		if cloneStats.MaxSymbols != origStats.MaxSymbols {
			t.Errorf("clone MaxSymbols = %d, expected %d", cloneStats.MaxSymbols, origStats.MaxSymbols)
		}

		// Verify symbols are retrievable
		if got, ok := clone.GetByID("a.go:1:funcA"); !ok || got != sym1 {
			t.Error("GetByID failed on clone")
		}

		// Verify secondary indexes work
		byName := clone.GetByName("funcA")
		if len(byName) != 1 {
			t.Errorf("clone GetByName = %d, expected 1", len(byName))
		}

		byFile := clone.GetByFile("a.go")
		if len(byFile) != 2 {
			t.Errorf("clone GetByFile = %d, expected 2", len(byFile))
		}

		byKind := clone.GetByKind(ast.SymbolKindFunction)
		if len(byKind) != 2 {
			t.Errorf("clone GetByKind = %d, expected 2", len(byKind))
		}
	})

	t.Run("modifying clone does not affect original", func(t *testing.T) {
		idx := NewSymbolIndex()
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		idx.Add(sym1)

		clone := idx.Clone()

		// Add to clone
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		clone.Add(sym2)

		// Original should be unchanged
		origStats := idx.Stats()
		cloneStats := clone.Stats()

		if origStats.TotalSymbols != 1 {
			t.Errorf("original TotalSymbols = %d, expected 1", origStats.TotalSymbols)
		}
		if cloneStats.TotalSymbols != 2 {
			t.Errorf("clone TotalSymbols = %d, expected 2", cloneStats.TotalSymbols)
		}

		// Verify original doesn't have the new symbol
		if _, ok := idx.GetByID("b.go:1:funcB"); ok {
			t.Error("original should not have the new symbol")
		}
	})

	t.Run("remove from clone does not affect original", func(t *testing.T) {
		idx := NewSymbolIndex()
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("a.go:10:funcB", "funcB", ast.SymbolKindFunction, "a.go")
		idx.Add(sym1)
		idx.Add(sym2)

		clone := idx.Clone()

		// Remove from clone
		clone.RemoveByFile("a.go")

		// Original should be unchanged
		if idx.Stats().TotalSymbols != 2 {
			t.Errorf("original TotalSymbols = %d, expected 2", idx.Stats().TotalSymbols)
		}
		if clone.Stats().TotalSymbols != 0 {
			t.Errorf("clone TotalSymbols = %d, expected 0", clone.Stats().TotalSymbols)
		}
	})

	t.Run("clone of empty index", func(t *testing.T) {
		idx := NewSymbolIndex()
		clone := idx.Clone()

		if clone.Stats().TotalSymbols != 0 {
			t.Errorf("clone TotalSymbols = %d, expected 0", clone.Stats().TotalSymbols)
		}
	})

	t.Run("clone is thread-safe", func(t *testing.T) {
		idx := NewSymbolIndex()
		for i := 0; i < 100; i++ {
			sym := makeSymbol(
				"file"+string(rune('a'+i%26))+".go:1:func",
				"func"+string(rune('A'+i%26)),
				ast.SymbolKindFunction,
				"file"+string(rune('a'+i%26))+".go",
			)
			idx.Add(sym)
		}

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				clone := idx.Clone()
				// Do some reads on clone
				_ = clone.Stats()
				_ = clone.GetByKind(ast.SymbolKindFunction)
			}()
		}
		wg.Wait()
		// No panics = success
	})
}
