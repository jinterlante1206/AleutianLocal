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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// createTestHierarchicalGraph creates a test graph with some symbols and edges.
func createTestHierarchicalGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("test-project")

	// Add some symbols
	symbols := []*ast.Symbol{
		{
			ID:        "main.go:10:main",
			Name:      "main",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "main.go",
			Package:   "main",
			StartLine: 10,
			EndLine:   15,
			Exported:  false,
		},
		{
			ID:        "main.go:20:handleRequest",
			Name:      "handleRequest",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "main.go",
			Package:   "main",
			StartLine: 20,
			EndLine:   30,
			Exported:  false,
		},
		{
			ID:        "handler/handler.go:10:Handler",
			Name:      "Handler",
			Kind:      ast.SymbolKindInterface,
			FilePath:  "handler/handler.go",
			Package:   "handler",
			StartLine: 10,
			EndLine:   15,
			Exported:  true,
		},
		{
			ID:        "handler/http.go:15:HTTPHandler",
			Name:      "HTTPHandler",
			Kind:      ast.SymbolKindStruct,
			FilePath:  "handler/http.go",
			Package:   "handler",
			StartLine: 15,
			EndLine:   20,
			Exported:  true,
		},
		{
			ID:        "handler/http.go:25:ServeHTTP",
			Name:      "ServeHTTP",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "handler/http.go",
			Package:   "handler",
			StartLine: 25,
			EndLine:   35,
			Exported:  true,
		},
		{
			ID:        "utils/utils.go:5:Helper",
			Name:      "Helper",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "utils/utils.go",
			Package:   "utils",
			StartLine: 5,
			EndLine:   10,
			Exported:  true,
		},
	}

	for _, sym := range symbols {
		if _, err := g.AddNode(sym); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}
	}

	// Add edges
	edges := []struct {
		from, to string
		edgeType EdgeType
	}{
		{"main.go:10:main", "main.go:20:handleRequest", EdgeTypeCalls},
		{"main.go:20:handleRequest", "handler/http.go:25:ServeHTTP", EdgeTypeCalls},
		{"handler/http.go:25:ServeHTTP", "utils/utils.go:5:Helper", EdgeTypeCalls},
		{"handler/http.go:15:HTTPHandler", "handler/handler.go:10:Handler", EdgeTypeImplements},
	}

	for _, e := range edges {
		if err := g.AddEdge(e.from, e.to, e.edgeType, ast.Location{}); err != nil {
			t.Fatalf("Failed to add edge: %v", err)
		}
	}

	// Freeze the graph
	g.Freeze()

	// Wrap as hierarchical
	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("Failed to wrap graph: %v", err)
	}

	return hg
}

// -----------------------------------------------------------------------------
// Constructor Tests
// -----------------------------------------------------------------------------

func TestNewCRSGraphAdapter(t *testing.T) {
	t.Run("valid graph returns adapter", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, err := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if adapter == nil {
			t.Fatal("Expected adapter, got nil")
		}
		defer adapter.Close()
	})

	t.Run("nil graph returns error", func(t *testing.T) {
		adapter, err := NewCRSGraphAdapter(nil, nil, 1, time.Now().UnixMilli(), nil)
		if err != crs.ErrGraphNotAvailable {
			t.Errorf("Expected ErrGraphNotAvailable, got: %v", err)
		}
		if adapter != nil {
			t.Error("Expected nil adapter")
		}
	})

	t.Run("custom config is used", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		config := &crs.GraphQueryConfig{
			CacheTTLMs:        60000,
			PageRankTimeoutMs: 10000,
		}
		adapter, err := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), config)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		defer adapter.Close()

		if adapter.config.CacheTTLMs != 60000 {
			t.Errorf("Expected CacheTTLMs 60000, got: %d", adapter.config.CacheTTLMs)
		}
	})
}

// -----------------------------------------------------------------------------
// Node Query Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_FindSymbolByID(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds existing symbol", func(t *testing.T) {
		sym, found, err := adapter.FindSymbolByID(ctx, "main.go:10:main")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Error("Expected to find symbol")
		}
		if sym.Name != "main" {
			t.Errorf("Expected name 'main', got: %s", sym.Name)
		}
	})

	t.Run("returns false for non-existent symbol", func(t *testing.T) {
		_, found, err := adapter.FindSymbolByID(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Error("Expected not to find symbol")
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		closedAdapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		closedAdapter.Close()

		_, _, err := closedAdapter.FindSymbolByID(ctx, "main.go:10:main")
		if err != crs.ErrGraphQueryClosed {
			t.Errorf("Expected ErrGraphQueryClosed, got: %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, _, err := adapter.FindSymbolByID(canceledCtx, "main.go:10:main")
		if err == nil {
			t.Error("Expected error for canceled context")
		}
	})
}

func TestCRSAdapter_FindSymbolsByName(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds symbols by name", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsByName(ctx, "Helper")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 1 {
			t.Errorf("Expected 1 symbol, got: %d", len(symbols))
		}
		if symbols[0].Name != "Helper" {
			t.Errorf("Expected name 'Helper', got: %s", symbols[0].Name)
		}
	})

	t.Run("returns empty for non-existent name", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsByName(ctx, "NonExistent")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 0 {
			t.Errorf("Expected 0 symbols, got: %d", len(symbols))
		}
	})
}

func TestCRSAdapter_FindSymbolsByKind(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds functions", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsByKind(ctx, ast.SymbolKindFunction)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 3 { // main, handleRequest, Helper
			t.Errorf("Expected 3 functions, got: %d", len(symbols))
		}
	})

	t.Run("finds interfaces", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsByKind(ctx, ast.SymbolKindInterface)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 1 {
			t.Errorf("Expected 1 interface, got: %d", len(symbols))
		}
		if symbols[0].Name != "Handler" {
			t.Errorf("Expected 'Handler', got: %s", symbols[0].Name)
		}
	})
}

func TestCRSAdapter_FindSymbolsInFile(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds symbols in file", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsInFile(ctx, "handler/http.go")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 2 { // HTTPHandler, ServeHTTP
			t.Errorf("Expected 2 symbols, got: %d", len(symbols))
		}
	})

	t.Run("returns empty for non-existent file", func(t *testing.T) {
		symbols, err := adapter.FindSymbolsInFile(ctx, "nonexistent.go")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(symbols) != 0 {
			t.Errorf("Expected 0 symbols, got: %d", len(symbols))
		}
	})
}

// -----------------------------------------------------------------------------
// Edge Query Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_FindCallers(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds callers", func(t *testing.T) {
		callers, err := adapter.FindCallers(ctx, "main.go:20:handleRequest")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(callers) != 1 {
			t.Errorf("Expected 1 caller, got: %d", len(callers))
		}
		if callers[0].Name != "main" {
			t.Errorf("Expected caller 'main', got: %s", callers[0].Name)
		}
	})

	t.Run("returns empty for no callers", func(t *testing.T) {
		callers, err := adapter.FindCallers(ctx, "main.go:10:main")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(callers) != 0 {
			t.Errorf("Expected 0 callers, got: %d", len(callers))
		}
	})
}

func TestCRSAdapter_FindCallees(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds callees", func(t *testing.T) {
		callees, err := adapter.FindCallees(ctx, "main.go:10:main")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(callees) != 1 {
			t.Errorf("Expected 1 callee, got: %d", len(callees))
		}
		if callees[0].Name != "handleRequest" {
			t.Errorf("Expected callee 'handleRequest', got: %s", callees[0].Name)
		}
	})
}

func TestCRSAdapter_FindImplementations(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds implementations", func(t *testing.T) {
		impls, err := adapter.FindImplementations(ctx, "Handler")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(impls) != 1 {
			t.Errorf("Expected 1 implementation, got: %d", len(impls))
		}
		if impls[0].Name != "HTTPHandler" {
			t.Errorf("Expected 'HTTPHandler', got: %s", impls[0].Name)
		}
	})
}

func TestCRSAdapter_FindReferences(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds references", func(t *testing.T) {
		refs, err := adapter.FindReferences(ctx, "utils/utils.go:5:Helper")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(refs) != 1 {
			t.Errorf("Expected 1 reference, got: %d", len(refs))
		}
		if refs[0].Name != "ServeHTTP" {
			t.Errorf("Expected 'ServeHTTP', got: %s", refs[0].Name)
		}
	})
}

// -----------------------------------------------------------------------------
// Path Query Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_GetCallChain(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds call chain", func(t *testing.T) {
		chain, err := adapter.GetCallChain(ctx, "main.go:10:main", "utils/utils.go:5:Helper", 10)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// main -> handleRequest -> ServeHTTP -> Helper
		if len(chain) != 4 {
			t.Errorf("Expected 4 nodes in chain, got: %d", len(chain))
		}
		if chain[0] != "main.go:10:main" {
			t.Errorf("Expected chain to start with main, got: %s", chain[0])
		}
		if chain[len(chain)-1] != "utils/utils.go:5:Helper" {
			t.Errorf("Expected chain to end with Helper, got: %s", chain[len(chain)-1])
		}
	})

	t.Run("returns empty for no path", func(t *testing.T) {
		chain, err := adapter.GetCallChain(ctx, "utils/utils.go:5:Helper", "main.go:10:main", 10)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(chain) != 0 {
			t.Errorf("Expected empty chain, got: %d", len(chain))
		}
	})
}

func TestCRSAdapter_ShortestPath(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()

	t.Run("finds shortest path", func(t *testing.T) {
		path, err := adapter.ShortestPath(ctx, "main.go:10:main", "utils/utils.go:5:Helper")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(path) != 4 {
			t.Errorf("Expected path of length 4, got: %d", len(path))
		}
	})

	t.Run("same node returns single element", func(t *testing.T) {
		path, err := adapter.ShortestPath(ctx, "main.go:10:main", "main.go:10:main")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(path) != 1 {
			t.Errorf("Expected path of length 1, got: %d", len(path))
		}
	})
}

// -----------------------------------------------------------------------------
// Analytics Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_Analytics_HotSpots(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()
	analytics := adapter.Analytics()

	t.Run("returns hotspots", func(t *testing.T) {
		hotspots, err := analytics.HotSpots(ctx, 3)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(hotspots) == 0 {
			t.Error("Expected at least one hotspot")
		}
		// Hotspots should be sorted by score
		for i := 1; i < len(hotspots); i++ {
			if hotspots[i].Score > hotspots[i-1].Score {
				t.Error("Hotspots should be sorted by score descending")
			}
		}
	})
}

func TestCRSAdapter_Analytics_DeadCode(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()
	analytics := adapter.Analytics()

	t.Run("finds dead code", func(t *testing.T) {
		deadCode, err := analytics.DeadCode(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// main is an entry point so should not be flagged
		// Helper is only called so should not be flagged
		// The test graph may have some dead code depending on structure
		_ = deadCode // Just verify it runs without error
	})
}

func TestCRSAdapter_Analytics_CyclicDependencies(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()
	analytics := adapter.Analytics()

	t.Run("returns cycles", func(t *testing.T) {
		cycles, err := analytics.CyclicDependencies(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// Our test graph doesn't have cycles
		if len(cycles) != 0 {
			t.Errorf("Expected no cycles in test graph, got: %d", len(cycles))
		}
	})
}

func TestCRSAdapter_Analytics_PageRank(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()
	analytics := adapter.Analytics()

	t.Run("computes PageRank", func(t *testing.T) {
		scores, err := analytics.PageRank(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(scores) == 0 {
			t.Error("Expected PageRank scores")
		}
		// All scores should sum to approximately 1
		sum := 0.0
		for _, score := range scores {
			sum += score
		}
		if sum < 0.99 || sum > 1.01 {
			t.Errorf("PageRank scores should sum to ~1, got: %f", sum)
		}
	})

	t.Run("caches PageRank", func(t *testing.T) {
		// First call
		scores1, _ := analytics.PageRank(ctx)
		// Second call should hit cache
		scores2, _ := analytics.PageRank(ctx)

		if len(scores1) != len(scores2) {
			t.Error("Cached result should match")
		}
	})
}

func TestCRSAdapter_Analytics_Communities(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
	defer adapter.Close()

	ctx := context.Background()
	analytics := adapter.Analytics()

	t.Run("finds communities", func(t *testing.T) {
		communities, err := analytics.Communities(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// We have 3 packages: main, handler, utils
		if len(communities) != 3 {
			t.Errorf("Expected 3 communities (packages), got: %d", len(communities))
		}
	})
}

// -----------------------------------------------------------------------------
// Metadata Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_Metadata(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	refreshTime := time.Now().UnixMilli()
	adapter, _ := NewCRSGraphAdapter(hg, nil, 42, refreshTime, nil)
	defer adapter.Close()

	t.Run("NodeCount", func(t *testing.T) {
		count := adapter.NodeCount()
		if count != 6 {
			t.Errorf("Expected 6 nodes, got: %d", count)
		}
	})

	t.Run("EdgeCount", func(t *testing.T) {
		count := adapter.EdgeCount()
		if count != 4 {
			t.Errorf("Expected 4 edges, got: %d", count)
		}
	})

	t.Run("Generation", func(t *testing.T) {
		gen := adapter.Generation()
		if gen != 42 {
			t.Errorf("Expected generation 42, got: %d", gen)
		}
	})

	t.Run("LastRefreshTime", func(t *testing.T) {
		refresh := adapter.LastRefreshTime()
		if refresh != refreshTime {
			t.Errorf("Expected refresh time %d, got: %d", refreshTime, refresh)
		}
	})
}

// -----------------------------------------------------------------------------
// Lifecycle Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_Close(t *testing.T) {
	hg := createTestHierarchicalGraph(t)
	adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)

	t.Run("Close returns nil", func(t *testing.T) {
		err := adapter.Close()
		if err != nil {
			t.Errorf("Expected nil, got: %v", err)
		}
	})

	t.Run("Close is idempotent", func(t *testing.T) {
		err := adapter.Close()
		if err != nil {
			t.Errorf("Expected nil on second close, got: %v", err)
		}
	})

	t.Run("methods return error after close", func(t *testing.T) {
		ctx := context.Background()
		_, _, err := adapter.FindSymbolByID(ctx, "test")
		if err != crs.ErrGraphQueryClosed {
			t.Errorf("Expected ErrGraphQueryClosed, got: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestCRSAdapter_InterfaceCompliance(t *testing.T) {
	t.Run("CRSGraphAdapter implements crs.GraphQuery", func(t *testing.T) {
		var _ crs.GraphQuery = (*CRSGraphAdapter)(nil)
	})

	t.Run("crsAnalyticsAdapter implements crs.GraphAnalyticsQuery", func(t *testing.T) {
		var _ crs.GraphAnalyticsQuery = (*crsAnalyticsAdapter)(nil)
	})
}

// -----------------------------------------------------------------------------
// Cycle Detection Tests (GR-32)
// -----------------------------------------------------------------------------

func TestCRSAdapter_HasCycleFrom(t *testing.T) {
	t.Run("no cycle in acyclic graph", func(t *testing.T) {
		// Create a simple acyclic graph: A -> B -> C
		g := NewGraph("test")
		symA := &ast.Symbol{ID: "a:1:A", Name: "A", Kind: ast.SymbolKindFunction, FilePath: "a.go"}
		symB := &ast.Symbol{ID: "b:1:B", Name: "B", Kind: ast.SymbolKindFunction, FilePath: "b.go"}
		symC := &ast.Symbol{ID: "c:1:C", Name: "C", Kind: ast.SymbolKindFunction, FilePath: "c.go"}

		g.AddNode(symA)
		g.AddNode(symB)
		g.AddNode(symC)
		g.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, ast.Location{})
		g.AddEdge(symB.ID, symC.ID, EdgeTypeCalls, ast.Location{})
		g.Freeze()

		hg, _ := WrapGraph(g)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()
		hasCycle, err := adapter.HasCycleFrom(ctx, symA.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hasCycle {
			t.Error("expected no cycle in acyclic graph")
		}
	})

	t.Run("detects direct cycle", func(t *testing.T) {
		// Create a cycle: A -> B -> C -> A
		g := NewGraph("test")
		symA := &ast.Symbol{ID: "a:1:A", Name: "A", Kind: ast.SymbolKindFunction, FilePath: "a.go"}
		symB := &ast.Symbol{ID: "b:1:B", Name: "B", Kind: ast.SymbolKindFunction, FilePath: "b.go"}
		symC := &ast.Symbol{ID: "c:1:C", Name: "C", Kind: ast.SymbolKindFunction, FilePath: "c.go"}

		g.AddNode(symA)
		g.AddNode(symB)
		g.AddNode(symC)
		g.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, ast.Location{})
		g.AddEdge(symB.ID, symC.ID, EdgeTypeCalls, ast.Location{})
		g.AddEdge(symC.ID, symA.ID, EdgeTypeCalls, ast.Location{})
		g.Freeze()

		hg, _ := WrapGraph(g)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()
		hasCycle, err := adapter.HasCycleFrom(ctx, symA.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !hasCycle {
			t.Error("expected cycle to be detected")
		}
	})

	t.Run("detects self-loop", func(t *testing.T) {
		// Create a self-loop: A -> A
		g := NewGraph("test")
		symA := &ast.Symbol{ID: "a:1:A", Name: "A", Kind: ast.SymbolKindFunction, FilePath: "a.go"}

		g.AddNode(symA)
		// Note: self-loops may not be allowed by the graph, so check if this works
		err := g.AddEdge(symA.ID, symA.ID, EdgeTypeCalls, ast.Location{})
		if err != nil {
			// If self-loops aren't allowed, skip this test
			t.Skip("self-loops not allowed by graph")
		}
		g.Freeze()

		hg, _ := WrapGraph(g)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()
		hasCycle, err := adapter.HasCycleFrom(ctx, symA.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !hasCycle {
			t.Error("expected self-loop cycle to be detected")
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		adapter.Close()

		ctx := context.Background()
		_, err := adapter.HasCycleFrom(ctx, "test")
		if err != crs.ErrGraphQueryClosed {
			t.Errorf("expected ErrGraphQueryClosed, got: %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := adapter.HasCycleFrom(ctx, "test")
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Call Edge Count Tests (GR-32)
// -----------------------------------------------------------------------------

func TestCRSAdapter_CallEdgeCount(t *testing.T) {
	t.Run("counts call edges correctly", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()
		count, err := adapter.CallEdgeCount(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// createTestHierarchicalGraph creates 3 CALLS edges and 1 IMPLEMENTS edge
		if count != 3 {
			t.Errorf("expected 3 call edges, got %d", count)
		}
	})

	t.Run("caches result", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()

		// First call computes
		count1, _ := adapter.CallEdgeCount(ctx)

		// Second call should be cached
		count2, _ := adapter.CallEdgeCount(ctx)

		if count1 != count2 {
			t.Errorf("expected same count, got %d and %d", count1, count2)
		}
	})

	t.Run("invalidate cache recomputes", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()

		// First call
		count1, _ := adapter.CallEdgeCount(ctx)

		// Invalidate cache
		adapter.InvalidateCache()

		// Second call should recompute
		count2, _ := adapter.CallEdgeCount(ctx)

		// Should still get the same count (graph hasn't changed)
		if count1 != count2 {
			t.Errorf("expected same count after invalidation, got %d and %d", count1, count2)
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		hg := createTestHierarchicalGraph(t)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		adapter.Close()

		ctx := context.Background()
		_, err := adapter.CallEdgeCount(ctx)
		if err != crs.ErrGraphQueryClosed {
			t.Errorf("expected ErrGraphQueryClosed, got: %v", err)
		}
	})

	t.Run("returns zero for empty graph", func(t *testing.T) {
		g := NewGraph("test")
		g.Freeze()
		hg, _ := WrapGraph(g)
		adapter, _ := NewCRSGraphAdapter(hg, nil, 1, time.Now().UnixMilli(), nil)
		defer adapter.Close()

		ctx := context.Background()
		count, err := adapter.CallEdgeCount(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0 call edges for empty graph, got %d", count)
		}
	})
}
