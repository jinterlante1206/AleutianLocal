// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// mockGraphQuery is a mock implementation of GraphQuery for testing.
type mockGraphQuery struct {
	callers       map[string][]*ast.Symbol
	callees       map[string][]*ast.Symbol
	hasCycle      map[string]bool
	callEdgeCount int
	nodeCount     int
	edgeCount     int
	generation    int64
	closed        bool
}

func newMockGraphQuery() *mockGraphQuery {
	return &mockGraphQuery{
		callers:       make(map[string][]*ast.Symbol),
		callees:       make(map[string][]*ast.Symbol),
		hasCycle:      make(map[string]bool),
		callEdgeCount: 0,
		nodeCount:     0,
		edgeCount:     0,
		generation:    1,
	}
}

func (m *mockGraphQuery) FindSymbolByID(ctx context.Context, id string) (*ast.Symbol, bool, error) {
	return nil, false, nil
}

func (m *mockGraphQuery) FindSymbolsByName(ctx context.Context, name string) ([]*ast.Symbol, error) {
	return nil, nil
}

func (m *mockGraphQuery) FindSymbolsByKind(ctx context.Context, kind ast.SymbolKind) ([]*ast.Symbol, error) {
	return nil, nil
}

func (m *mockGraphQuery) FindSymbolsInFile(ctx context.Context, filePath string) ([]*ast.Symbol, error) {
	return nil, nil
}

func (m *mockGraphQuery) FindCallers(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	return m.callers[symbolID], nil
}

func (m *mockGraphQuery) FindCallees(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	return m.callees[symbolID], nil
}

func (m *mockGraphQuery) FindImplementations(ctx context.Context, interfaceName string) ([]*ast.Symbol, error) {
	return nil, nil
}

func (m *mockGraphQuery) FindReferences(ctx context.Context, symbolID string) ([]*ast.Symbol, error) {
	return nil, nil
}

func (m *mockGraphQuery) GetCallChain(ctx context.Context, fromID, toID string, maxDepth int) ([]string, error) {
	return nil, nil
}

func (m *mockGraphQuery) ShortestPath(ctx context.Context, fromID, toID string) ([]string, error) {
	return nil, nil
}

func (m *mockGraphQuery) Analytics() GraphAnalyticsQuery {
	return nil
}

func (m *mockGraphQuery) NodeCount() int {
	return m.nodeCount
}

func (m *mockGraphQuery) EdgeCount() int {
	return m.edgeCount
}

func (m *mockGraphQuery) Generation() int64 {
	return m.generation
}

func (m *mockGraphQuery) LastRefreshTime() int64 {
	return 0
}

func (m *mockGraphQuery) Close() error {
	m.closed = true
	return nil
}

// HasCycleFrom implements cycle detection for testing.
func (m *mockGraphQuery) HasCycleFrom(ctx context.Context, symbolID string) (bool, error) {
	return m.hasCycle[symbolID], nil
}

// CallEdgeCount implements edge count for testing.
func (m *mockGraphQuery) CallEdgeCount(ctx context.Context) (int, error) {
	return m.callEdgeCount, nil
}

// InvalidateCache implements cache invalidation for testing.
func (m *mockGraphQuery) InvalidateCache() {
	// No-op for mock
}

func TestNewGraphBackedDependencyIndex(t *testing.T) {
	t.Run("nil adapter returns error", func(t *testing.T) {
		idx, err := NewGraphBackedDependencyIndex(nil)
		if err == nil {
			t.Error("expected error for nil adapter")
		}
		if idx != nil {
			t.Error("expected nil index for nil adapter")
		}
	})

	t.Run("valid adapter returns index", func(t *testing.T) {
		mock := newMockGraphQuery()
		idx, err := NewGraphBackedDependencyIndex(mock)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if idx == nil {
			t.Error("expected non-nil index")
		}
	})
}

func TestGraphBackedDependencyIndex_DependsOn(t *testing.T) {
	t.Run("returns callee IDs", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callees["funcA"] = []*ast.Symbol{
			{ID: "funcB"},
			{ID: "funcC"},
		}

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.DependsOn("funcA")

		if len(result) != 2 {
			t.Errorf("expected 2 dependencies, got %d", len(result))
		}
		// Check IDs are present (order may vary)
		found := make(map[string]bool)
		for _, id := range result {
			found[id] = true
		}
		if !found["funcB"] || !found["funcC"] {
			t.Errorf("expected funcB and funcC, got %v", result)
		}
	})

	t.Run("returns empty for unknown node", func(t *testing.T) {
		mock := newMockGraphQuery()
		idx, _ := NewGraphBackedDependencyIndex(mock)

		result := idx.DependsOn("unknown")
		if result == nil {
			// nil is acceptable for no results
		} else if len(result) != 0 {
			t.Errorf("expected empty slice, got %v", result)
		}
	})
}

func TestGraphBackedDependencyIndex_DependedBy(t *testing.T) {
	t.Run("returns caller IDs", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callers["helper"] = []*ast.Symbol{
			{ID: "main"},
			{ID: "process"},
		}

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.DependedBy("helper")

		if len(result) != 2 {
			t.Errorf("expected 2 dependents, got %d", len(result))
		}
		found := make(map[string]bool)
		for _, id := range result {
			found[id] = true
		}
		if !found["main"] || !found["process"] {
			t.Errorf("expected main and process, got %v", result)
		}
	})
}

func TestGraphBackedDependencyIndex_HasCycle(t *testing.T) {
	t.Run("returns true when cycle exists", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.hasCycle["funcA"] = true

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.HasCycle("funcA")

		if !result {
			t.Error("expected cycle to be detected")
		}
	})

	t.Run("returns false when no cycle", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.hasCycle["funcA"] = false

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.HasCycle("funcA")

		if result {
			t.Error("expected no cycle")
		}
	})
}

func TestGraphBackedDependencyIndex_Size(t *testing.T) {
	t.Run("returns call edge count", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callEdgeCount = 42

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.Size()

		if result != 42 {
			t.Errorf("expected 42, got %d", result)
		}
	})

	t.Run("caches result", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callEdgeCount = 10

		idx, _ := NewGraphBackedDependencyIndex(mock)

		// First call
		result1 := idx.Size()
		if result1 != 10 {
			t.Errorf("expected 10, got %d", result1)
		}

		// Change the mock's count
		mock.callEdgeCount = 20

		// Second call should return cached value
		result2 := idx.Size()
		if result2 != 10 {
			t.Errorf("expected cached 10, got %d", result2)
		}
	})

	t.Run("invalidate cache refreshes", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callEdgeCount = 10

		idx, _ := NewGraphBackedDependencyIndex(mock)

		// First call
		_ = idx.Size()

		// Change the mock's count
		mock.callEdgeCount = 20

		// Invalidate and re-query
		idx.InvalidateCache()
		result := idx.Size()

		if result != 20 {
			t.Errorf("expected 20 after invalidation, got %d", result)
		}
	})
}

func TestGraphBackedDependencyIndex_Generation(t *testing.T) {
	t.Run("returns adapter generation", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.generation = 42

		idx, _ := NewGraphBackedDependencyIndex(mock)
		result := idx.Generation()

		if result != 42 {
			t.Errorf("expected 42, got %d", result)
		}
	})
}

func TestGraphBackedDependencyIndex_InterfaceCompliance(t *testing.T) {
	// This test verifies that GraphBackedDependencyIndex implements DependencyIndexView
	mock := newMockGraphQuery()
	idx, _ := NewGraphBackedDependencyIndex(mock)

	var _ DependencyIndexView = idx
}

// GR-32 Code Review Fix: Test concurrent Size()/InvalidateCache() operations
func TestGraphBackedDependencyIndex_ConcurrentSizeAndInvalidate(t *testing.T) {
	t.Run("concurrent size and invalidate does not race", func(t *testing.T) {
		mock := newMockGraphQuery()
		mock.callEdgeCount = 100

		idx, _ := NewGraphBackedDependencyIndex(mock)

		// Run many concurrent operations
		iterations := 100

		// Use sync.WaitGroup for proper synchronization
		var wg sync.WaitGroup
		wg.Add(3)

		// Channel to collect errors from goroutines (safe alternative to t.Errorf in goroutines)
		errCh := make(chan string, iterations*2)

		// Goroutine 1: Repeatedly call Size()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				size := idx.Size()
				// Size should always be 100 - mock doesn't return errors
				// and cache invalidation doesn't change the underlying value
				if size != 100 {
					errCh <- fmt.Sprintf("goroutine 1: unexpected size: %d (iteration %d)", size, i)
				}
			}
		}()

		// Goroutine 2: Repeatedly call InvalidateCache()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				idx.InvalidateCache()
			}
		}()

		// Goroutine 3: Also call Size()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				size := idx.Size()
				if size != 100 {
					errCh <- fmt.Sprintf("goroutine 3: unexpected size: %d (iteration %d)", size, i)
				}
			}
		}()

		// Wait for all goroutines to complete
		wg.Wait()
		close(errCh)

		// Report any errors collected from goroutines
		for errMsg := range errCh {
			t.Error(errMsg)
		}
	})
}

// GR-32 Code Review Fix: Test empty nodeID validation
func TestGraphBackedDependencyIndex_EmptyNodeIDValidation(t *testing.T) {
	mock := newMockGraphQuery()
	idx, _ := NewGraphBackedDependencyIndex(mock)

	t.Run("DependsOn returns nil for empty nodeID", func(t *testing.T) {
		result := idx.DependsOn("")
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("DependedBy returns nil for empty nodeID", func(t *testing.T) {
		result := idx.DependedBy("")
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("HasCycle returns false for empty nodeID", func(t *testing.T) {
		result := idx.HasCycle("")
		if result {
			t.Error("expected false for empty nodeID")
		}
	})
}
