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
	"errors"
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

// Helper function to create a test location.
func makeLocation(filePath string, line int) ast.Location {
	return ast.Location{
		FilePath:  filePath,
		StartLine: line,
		EndLine:   line,
		StartCol:  0,
		EndCol:    50,
	}
}

func TestGraphState_String(t *testing.T) {
	tests := []struct {
		state    GraphState
		expected string
	}{
		{GraphStateBuilding, "building"},
		{GraphStateReadOnly, "readonly"},
		{GraphState(99), "unknown"},
	}

	for _, tc := range tests {
		got := tc.state.String()
		if got != tc.expected {
			t.Errorf("GraphState(%d).String() = %q, expected %q", tc.state, got, tc.expected)
		}
	}
}

func TestEdgeType_String(t *testing.T) {
	tests := []struct {
		edgeType EdgeType
		expected string
	}{
		{EdgeTypeUnknown, "unknown"},
		{EdgeTypeCalls, "calls"},
		{EdgeTypeImports, "imports"},
		{EdgeTypeDefines, "defines"},
		{EdgeTypeImplements, "implements"},
		{EdgeTypeEmbeds, "embeds"},
		{EdgeTypeReferences, "references"},
		{EdgeTypeReturns, "returns"},
		{EdgeTypeReceives, "receives"},
		{EdgeTypeParameters, "parameters"},
		{EdgeType(99), "unknown"},
	}

	for _, tc := range tests {
		got := tc.edgeType.String()
		if got != tc.expected {
			t.Errorf("EdgeType(%d).String() = %q, expected %q", tc.edgeType, got, tc.expected)
		}
	}
}

func TestNewGraph(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		g := NewGraph("/path/to/project")

		if g.ProjectRoot != "/path/to/project" {
			t.Errorf("ProjectRoot = %q, expected %q", g.ProjectRoot, "/path/to/project")
		}
		if g.State() != GraphStateBuilding {
			t.Errorf("State = %v, expected Building", g.State())
		}
		if g.NodeCount() != 0 {
			t.Errorf("NodeCount = %d, expected 0", g.NodeCount())
		}
		if g.EdgeCount() != 0 {
			t.Errorf("EdgeCount = %d, expected 0", g.EdgeCount())
		}

		stats := g.Stats()
		if stats.MaxNodes != DefaultMaxNodes {
			t.Errorf("MaxNodes = %d, expected %d", stats.MaxNodes, DefaultMaxNodes)
		}
		if stats.MaxEdges != DefaultMaxEdges {
			t.Errorf("MaxEdges = %d, expected %d", stats.MaxEdges, DefaultMaxEdges)
		}
	})

	t.Run("custom options", func(t *testing.T) {
		g := NewGraph("/project", WithMaxNodes(100), WithMaxEdges(500))

		stats := g.Stats()
		if stats.MaxNodes != 100 {
			t.Errorf("MaxNodes = %d, expected 100", stats.MaxNodes)
		}
		if stats.MaxEdges != 500 {
			t.Errorf("MaxEdges = %d, expected 500", stats.MaxEdges)
		}
	})
}

func TestGraph_AddNode(t *testing.T) {
	t.Run("add node success", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")

		node, err := g.AddNode(sym)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if node.ID != sym.ID {
			t.Errorf("node.ID = %q, expected %q", node.ID, sym.ID)
		}
		if node.Symbol != sym {
			t.Error("node.Symbol should be the same pointer")
		}
		if len(node.Outgoing) != 0 {
			t.Errorf("Outgoing should be empty, got %d", len(node.Outgoing))
		}
		if len(node.Incoming) != 0 {
			t.Errorf("Incoming should be empty, got %d", len(node.Incoming))
		}
		if g.NodeCount() != 1 {
			t.Errorf("NodeCount = %d, expected 1", g.NodeCount())
		}
	})

	t.Run("add node nil symbol returns error", func(t *testing.T) {
		g := NewGraph("/project")

		_, err := g.AddNode(nil)
		if err == nil {
			t.Fatal("expected error for nil symbol")
		}
		if !errors.Is(err, ErrInvalidNode) {
			t.Errorf("expected ErrInvalidNode, got %v", err)
		}
	})

	t.Run("add duplicate node returns error", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")
		sym2 := makeSymbol("main.go:1:main", "other", ast.SymbolKindVariable, "main.go")

		if _, err := g.AddNode(sym1); err != nil {
			t.Fatalf("first add failed: %v", err)
		}

		_, err := g.AddNode(sym2)
		if err == nil {
			t.Fatal("expected error for duplicate node")
		}
		if !errors.Is(err, ErrDuplicateNode) {
			t.Errorf("expected ErrDuplicateNode, got %v", err)
		}
		if g.NodeCount() != 1 {
			t.Errorf("NodeCount = %d, expected 1 (original only)", g.NodeCount())
		}
	})

	t.Run("add node when frozen returns error", func(t *testing.T) {
		g := NewGraph("/project")
		g.Freeze()

		sym := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")
		_, err := g.AddNode(sym)
		if err == nil {
			t.Fatal("expected error when frozen")
		}
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("expected ErrGraphFrozen, got %v", err)
		}
	})

	t.Run("add node at capacity returns error", func(t *testing.T) {
		g := NewGraph("/project", WithMaxNodes(2))

		sym1 := makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:c", "c", ast.SymbolKindFunction, "c.go")

		if _, err := g.AddNode(sym1); err != nil {
			t.Fatalf("add 1 failed: %v", err)
		}
		if _, err := g.AddNode(sym2); err != nil {
			t.Fatalf("add 2 failed: %v", err)
		}

		_, err := g.AddNode(sym3)
		if err == nil {
			t.Fatal("expected error at capacity")
		}
		if !errors.Is(err, ErrMaxNodesExceeded) {
			t.Errorf("expected ErrMaxNodesExceeded, got %v", err)
		}
	})
}

func TestGraph_GetNode(t *testing.T) {
	g := NewGraph("/project")
	sym := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")
	g.AddNode(sym)

	t.Run("get existing node", func(t *testing.T) {
		node, ok := g.GetNode("main.go:1:main")
		if !ok {
			t.Fatal("expected to find node")
		}
		if node.Symbol != sym {
			t.Error("wrong symbol")
		}
	})

	t.Run("get non-existent node", func(t *testing.T) {
		_, ok := g.GetNode("does-not-exist")
		if ok {
			t.Error("expected not to find node")
		}
	})
}

func TestGraph_AddEdge(t *testing.T) {
	t.Run("add edge success", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)

		loc := makeLocation("a.go", 10)
		err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if g.EdgeCount() != 1 {
			t.Errorf("EdgeCount = %d, expected 1", g.EdgeCount())
		}

		// Check outgoing edge on source node
		node1, _ := g.GetNode(sym1.ID)
		if len(node1.Outgoing) != 1 {
			t.Errorf("source Outgoing = %d, expected 1", len(node1.Outgoing))
		}
		if node1.Outgoing[0].ToID != sym2.ID {
			t.Errorf("edge.ToID = %q, expected %q", node1.Outgoing[0].ToID, sym2.ID)
		}
		if node1.Outgoing[0].Type != EdgeTypeCalls {
			t.Errorf("edge.Type = %v, expected Calls", node1.Outgoing[0].Type)
		}

		// Check incoming edge on target node
		node2, _ := g.GetNode(sym2.ID)
		if len(node2.Incoming) != 1 {
			t.Errorf("target Incoming = %d, expected 1", len(node2.Incoming))
		}
		if node2.Incoming[0].FromID != sym1.ID {
			t.Errorf("edge.FromID = %q, expected %q", node2.Incoming[0].FromID, sym1.ID)
		}
	})

	t.Run("add edge with non-existent source returns error", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym)

		loc := makeLocation("a.go", 10)
		err := g.AddEdge("does-not-exist", sym.ID, EdgeTypeCalls, loc)
		if err == nil {
			t.Fatal("expected error for non-existent source")
		}
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("add edge with non-existent target returns error", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		loc := makeLocation("a.go", 10)
		err := g.AddEdge(sym.ID, "does-not-exist", EdgeTypeCalls, loc)
		if err == nil {
			t.Fatal("expected error for non-existent target")
		}
		if !errors.Is(err, ErrNodeNotFound) {
			t.Errorf("expected ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("add edge when frozen returns error", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.Freeze()

		loc := makeLocation("a.go", 10)
		err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc)
		if err == nil {
			t.Fatal("expected error when frozen")
		}
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("expected ErrGraphFrozen, got %v", err)
		}
	})

	t.Run("add edge at capacity returns error", func(t *testing.T) {
		g := NewGraph("/project", WithMaxEdges(1))
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)

		loc1 := makeLocation("a.go", 10)
		if err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc1); err != nil {
			t.Fatalf("first edge failed: %v", err)
		}

		loc2 := makeLocation("a.go", 20)
		err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc2)
		if err == nil {
			t.Fatal("expected error at capacity")
		}
		if !errors.Is(err, ErrMaxEdgesExceeded) {
			t.Errorf("expected ErrMaxEdgesExceeded, got %v", err)
		}
	})

	t.Run("add duplicate edge succeeds (different locations)", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)

		loc1 := makeLocation("a.go", 10)
		loc2 := makeLocation("a.go", 20)

		if err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc1); err != nil {
			t.Fatalf("first edge failed: %v", err)
		}
		if err := g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, loc2); err != nil {
			t.Fatalf("second edge failed: %v", err)
		}

		if g.EdgeCount() != 2 {
			t.Errorf("EdgeCount = %d, expected 2", g.EdgeCount())
		}

		node1, _ := g.GetNode(sym1.ID)
		if len(node1.Outgoing) != 2 {
			t.Errorf("source Outgoing = %d, expected 2", len(node1.Outgoing))
		}

		node2, _ := g.GetNode(sym2.ID)
		if len(node2.Incoming) != 2 {
			t.Errorf("target Incoming = %d, expected 2", len(node2.Incoming))
		}
	})
}

func TestGraph_Freeze(t *testing.T) {
	t.Run("freeze sets state and timestamp", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("main.go:1:main", "main", ast.SymbolKindFunction, "main.go")
		g.AddNode(sym)

		if g.IsFrozen() {
			t.Error("should not be frozen before Freeze()")
		}
		if g.BuiltAtMilli != 0 {
			t.Errorf("BuiltAtMilli should be 0 before freeze, got %d", g.BuiltAtMilli)
		}

		beforeFreeze := time.Now().UnixMilli()
		g.Freeze()
		afterFreeze := time.Now().UnixMilli()

		if !g.IsFrozen() {
			t.Error("should be frozen after Freeze()")
		}
		if g.State() != GraphStateReadOnly {
			t.Errorf("State = %v, expected ReadOnly", g.State())
		}
		if g.BuiltAtMilli < beforeFreeze || g.BuiltAtMilli > afterFreeze {
			t.Errorf("BuiltAtMilli = %d, expected between %d and %d",
				g.BuiltAtMilli, beforeFreeze, afterFreeze)
		}
	})

	t.Run("freeze prevents modifications", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym1)
		g.Freeze()

		// Try to add node
		sym2 := makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go")
		_, err := g.AddNode(sym2)
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("AddNode should return ErrGraphFrozen, got %v", err)
		}

		// Try to add edge
		err = g.AddEdge(sym1.ID, sym1.ID, EdgeTypeCalls, makeLocation("a.go", 1))
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("AddEdge should return ErrGraphFrozen, got %v", err)
		}
	})
}

func TestGraph_Stats(t *testing.T) {
	g := NewGraph("/project", WithMaxNodes(100), WithMaxEdges(500))

	sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
	sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
	sym3 := makeSymbol("c.go:1:TypeC", "TypeC", ast.SymbolKindStruct, "c.go")

	g.AddNode(sym1)
	g.AddNode(sym2)
	g.AddNode(sym3)

	g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
	g.AddEdge(sym1.ID, sym3.ID, EdgeTypeReferences, makeLocation("a.go", 15))
	g.AddEdge(sym2.ID, sym3.ID, EdgeTypeReturns, makeLocation("b.go", 5))

	stats := g.Stats()

	if stats.NodeCount != 3 {
		t.Errorf("NodeCount = %d, expected 3", stats.NodeCount)
	}
	if stats.EdgeCount != 3 {
		t.Errorf("EdgeCount = %d, expected 3", stats.EdgeCount)
	}
	if stats.MaxNodes != 100 {
		t.Errorf("MaxNodes = %d, expected 100", stats.MaxNodes)
	}
	if stats.MaxEdges != 500 {
		t.Errorf("MaxEdges = %d, expected 500", stats.MaxEdges)
	}
	if stats.State != GraphStateBuilding {
		t.Errorf("State = %v, expected Building", stats.State)
	}
	if stats.BuiltAtMilli != 0 {
		t.Errorf("BuiltAtMilli = %d, expected 0 (not frozen)", stats.BuiltAtMilli)
	}

	// Check edge counts by type
	if stats.EdgesByType[EdgeTypeCalls] != 1 {
		t.Errorf("EdgesByType[Calls] = %d, expected 1", stats.EdgesByType[EdgeTypeCalls])
	}
	if stats.EdgesByType[EdgeTypeReferences] != 1 {
		t.Errorf("EdgesByType[References] = %d, expected 1", stats.EdgesByType[EdgeTypeReferences])
	}
	if stats.EdgesByType[EdgeTypeReturns] != 1 {
		t.Errorf("EdgesByType[Returns] = %d, expected 1", stats.EdgesByType[EdgeTypeReturns])
	}
}

func TestGraph_Nodes(t *testing.T) {
	g := NewGraph("/project")
	sym1 := makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go")
	sym2 := makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go")
	g.AddNode(sym1)
	g.AddNode(sym2)

	count := 0
	ids := make(map[string]bool)
	for id, node := range g.Nodes() {
		count++
		ids[id] = true
		if node.ID != id {
			t.Errorf("node.ID = %q, key = %q, should match", node.ID, id)
		}
	}

	if count != 2 {
		t.Errorf("iterated %d nodes, expected 2", count)
	}
	if !ids[sym1.ID] || !ids[sym2.ID] {
		t.Error("not all node IDs found in iteration")
	}
}

func TestGraph_Edges(t *testing.T) {
	g := NewGraph("/project")
	sym1 := makeSymbol("a.go:1:a", "a", ast.SymbolKindFunction, "a.go")
	sym2 := makeSymbol("b.go:1:b", "b", ast.SymbolKindFunction, "b.go")
	g.AddNode(sym1)
	g.AddNode(sym2)
	g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

	edges := g.Edges()
	if len(edges) != 1 {
		t.Errorf("len(Edges) = %d, expected 1", len(edges))
	}
	if edges[0].FromID != sym1.ID {
		t.Errorf("edge.FromID = %q, expected %q", edges[0].FromID, sym1.ID)
	}
}

func TestOutgoingIncomingEdges(t *testing.T) {
	// Build a small graph:
	// A -> B -> C
	// A -> C
	g := NewGraph("/project")

	symA := makeSymbol("a.go:1:A", "A", ast.SymbolKindFunction, "a.go")
	symB := makeSymbol("b.go:1:B", "B", ast.SymbolKindFunction, "b.go")
	symC := makeSymbol("c.go:1:C", "C", ast.SymbolKindFunction, "c.go")

	g.AddNode(symA)
	g.AddNode(symB)
	g.AddNode(symC)

	g.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, makeLocation("a.go", 10))
	g.AddEdge(symB.ID, symC.ID, EdgeTypeCalls, makeLocation("b.go", 10))
	g.AddEdge(symA.ID, symC.ID, EdgeTypeCalls, makeLocation("a.go", 20))

	nodeA, _ := g.GetNode(symA.ID)
	nodeB, _ := g.GetNode(symB.ID)
	nodeC, _ := g.GetNode(symC.ID)

	// A: 2 outgoing, 0 incoming
	if len(nodeA.Outgoing) != 2 {
		t.Errorf("A.Outgoing = %d, expected 2", len(nodeA.Outgoing))
	}
	if len(nodeA.Incoming) != 0 {
		t.Errorf("A.Incoming = %d, expected 0", len(nodeA.Incoming))
	}

	// B: 1 outgoing, 1 incoming
	if len(nodeB.Outgoing) != 1 {
		t.Errorf("B.Outgoing = %d, expected 1", len(nodeB.Outgoing))
	}
	if len(nodeB.Incoming) != 1 {
		t.Errorf("B.Incoming = %d, expected 1", len(nodeB.Incoming))
	}

	// C: 0 outgoing, 2 incoming
	if len(nodeC.Outgoing) != 0 {
		t.Errorf("C.Outgoing = %d, expected 0", len(nodeC.Outgoing))
	}
	if len(nodeC.Incoming) != 2 {
		t.Errorf("C.Incoming = %d, expected 2", len(nodeC.Incoming))
	}
}

func TestGraph_Clone(t *testing.T) {
	t.Run("clone creates independent copy", func(t *testing.T) {
		// Build original graph
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.Freeze()

		// Clone
		clone := g.Clone()

		// Verify clone has same data
		if clone.ProjectRoot != g.ProjectRoot {
			t.Errorf("clone.ProjectRoot = %q, expected %q", clone.ProjectRoot, g.ProjectRoot)
		}
		if clone.NodeCount() != g.NodeCount() {
			t.Errorf("clone.NodeCount = %d, expected %d", clone.NodeCount(), g.NodeCount())
		}
		if clone.EdgeCount() != g.EdgeCount() {
			t.Errorf("clone.EdgeCount = %d, expected %d", clone.EdgeCount(), g.EdgeCount())
		}
		if clone.BuiltAtMilli != g.BuiltAtMilli {
			t.Errorf("clone.BuiltAtMilli = %d, expected %d", clone.BuiltAtMilli, g.BuiltAtMilli)
		}

		// Clone should be in building state
		if clone.State() != GraphStateBuilding {
			t.Errorf("clone.State = %v, expected Building", clone.State())
		}

		// Verify nodes are deep copied
		origNode, _ := g.GetNode(sym1.ID)
		cloneNode, _ := clone.GetNode(sym1.ID)
		if origNode == cloneNode {
			t.Error("nodes should be different pointers")
		}

		// Verify symbol pointers are shared (immutable)
		if origNode.Symbol != cloneNode.Symbol {
			t.Error("symbols should be same pointer (shared)")
		}

		// Verify edges are deep copied
		if len(cloneNode.Outgoing) != 1 {
			t.Fatalf("clone node should have 1 outgoing edge")
		}
		if origNode.Outgoing[0] == cloneNode.Outgoing[0] {
			t.Error("edges should be different pointers")
		}
	})

	t.Run("modifying clone does not affect original", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym1)
		g.Freeze()

		clone := g.Clone()

		// Add new node to clone
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		_, err := clone.AddNode(sym2)
		if err != nil {
			t.Fatalf("add to clone failed: %v", err)
		}

		// Original should be unchanged
		if g.NodeCount() != 1 {
			t.Errorf("original NodeCount = %d, expected 1", g.NodeCount())
		}
		if clone.NodeCount() != 2 {
			t.Errorf("clone NodeCount = %d, expected 2", clone.NodeCount())
		}
	})

	t.Run("clone of empty graph", func(t *testing.T) {
		g := NewGraph("/project")
		clone := g.Clone()

		if clone.NodeCount() != 0 {
			t.Errorf("clone.NodeCount = %d, expected 0", clone.NodeCount())
		}
		if clone.EdgeCount() != 0 {
			t.Errorf("clone.EdgeCount = %d, expected 0", clone.EdgeCount())
		}
	})
}

func TestGraph_RemoveFile(t *testing.T) {
	t.Run("remove file removes all nodes from that file", func(t *testing.T) {
		g := NewGraph("/project")

		// Add nodes from two files
		symA1 := makeSymbol("a.go:1:funcA1", "funcA1", ast.SymbolKindFunction, "a.go")
		symA2 := makeSymbol("a.go:10:funcA2", "funcA2", ast.SymbolKindFunction, "a.go")
		symB1 := makeSymbol("b.go:1:funcB1", "funcB1", ast.SymbolKindFunction, "b.go")

		g.AddNode(symA1)
		g.AddNode(symA2)
		g.AddNode(symB1)

		// Add edges
		g.AddEdge(symA1.ID, symA2.ID, EdgeTypeCalls, makeLocation("a.go", 5))
		g.AddEdge(symA1.ID, symB1.ID, EdgeTypeCalls, makeLocation("a.go", 6))
		g.AddEdge(symB1.ID, symA1.ID, EdgeTypeCalls, makeLocation("b.go", 5))

		// Remove file a.go
		removed, err := g.RemoveFile("a.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if removed != 2 {
			t.Errorf("removed = %d, expected 2", removed)
		}
		if g.NodeCount() != 1 {
			t.Errorf("NodeCount = %d, expected 1", g.NodeCount())
		}

		// Verify a.go nodes are gone
		if _, ok := g.GetNode(symA1.ID); ok {
			t.Error("symA1 should be removed")
		}
		if _, ok := g.GetNode(symA2.ID); ok {
			t.Error("symA2 should be removed")
		}

		// Verify b.go node still exists
		nodeB, ok := g.GetNode(symB1.ID)
		if !ok {
			t.Fatal("symB1 should still exist")
		}

		// Edges involving a.go should be removed
		if len(nodeB.Outgoing) != 0 {
			t.Errorf("nodeB.Outgoing = %d, expected 0 (edge to a.go removed)", len(nodeB.Outgoing))
		}
		if len(nodeB.Incoming) != 0 {
			t.Errorf("nodeB.Incoming = %d, expected 0 (edge from a.go removed)", len(nodeB.Incoming))
		}

		// Total edges should be 0
		if g.EdgeCount() != 0 {
			t.Errorf("EdgeCount = %d, expected 0", g.EdgeCount())
		}
	})

	t.Run("remove non-existent file returns 0", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		removed, err := g.RemoveFile("does-not-exist.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != 0 {
			t.Errorf("removed = %d, expected 0", removed)
		}
		if g.NodeCount() != 1 {
			t.Errorf("NodeCount = %d, expected 1", g.NodeCount())
		}
	})

	t.Run("remove file when frozen returns error", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)
		g.Freeze()

		_, err := g.RemoveFile("a.go")
		if err == nil {
			t.Fatal("expected error when frozen")
		}
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("expected ErrGraphFrozen, got %v", err)
		}
	})
}

func TestGraph_MergeParseResult(t *testing.T) {
	t.Run("merge adds symbols as nodes", func(t *testing.T) {
		g := NewGraph("/project")

		result := &ast.ParseResult{
			FilePath: "test.go",
			Language: "go",
			Symbols: []*ast.Symbol{
				makeSymbol("test.go:1:funcA", "funcA", ast.SymbolKindFunction, "test.go"),
				makeSymbol("test.go:10:funcB", "funcB", ast.SymbolKindFunction, "test.go"),
			},
		}

		added, err := g.MergeParseResult(result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if added != 2 {
			t.Errorf("added = %d, expected 2", added)
		}
		if g.NodeCount() != 2 {
			t.Errorf("NodeCount = %d, expected 2", g.NodeCount())
		}

		// Verify nodes exist
		if _, ok := g.GetNode("test.go:1:funcA"); !ok {
			t.Error("funcA node should exist")
		}
		if _, ok := g.GetNode("test.go:10:funcB"); !ok {
			t.Error("funcB node should exist")
		}
	})

	t.Run("merge skips duplicate symbols", func(t *testing.T) {
		g := NewGraph("/project")

		// Add existing symbol
		existing := makeSymbol("test.go:1:funcA", "funcA", ast.SymbolKindFunction, "test.go")
		g.AddNode(existing)

		// Merge result with duplicate
		result := &ast.ParseResult{
			FilePath: "test.go",
			Language: "go",
			Symbols: []*ast.Symbol{
				makeSymbol("test.go:1:funcA", "funcA", ast.SymbolKindFunction, "test.go"),  // Duplicate
				makeSymbol("test.go:10:funcB", "funcB", ast.SymbolKindFunction, "test.go"), // New
			},
		}

		added, err := g.MergeParseResult(result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if added != 1 {
			t.Errorf("added = %d, expected 1 (duplicate skipped)", added)
		}
		if g.NodeCount() != 2 {
			t.Errorf("NodeCount = %d, expected 2", g.NodeCount())
		}
	})

	t.Run("merge nil result returns 0", func(t *testing.T) {
		g := NewGraph("/project")

		added, err := g.MergeParseResult(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 0 {
			t.Errorf("added = %d, expected 0", added)
		}
	})

	t.Run("merge empty result returns 0", func(t *testing.T) {
		g := NewGraph("/project")

		result := &ast.ParseResult{
			FilePath: "test.go",
			Language: "go",
			Symbols:  []*ast.Symbol{},
		}

		added, err := g.MergeParseResult(result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if added != 0 {
			t.Errorf("added = %d, expected 0", added)
		}
	})

	t.Run("merge when frozen returns error", func(t *testing.T) {
		g := NewGraph("/project")
		g.Freeze()

		result := &ast.ParseResult{
			FilePath: "test.go",
			Language: "go",
			Symbols: []*ast.Symbol{
				makeSymbol("test.go:1:funcA", "funcA", ast.SymbolKindFunction, "test.go"),
			},
		}

		_, err := g.MergeParseResult(result)
		if err == nil {
			t.Fatal("expected error when frozen")
		}
		if !errors.Is(err, ErrGraphFrozen) {
			t.Errorf("expected ErrGraphFrozen, got %v", err)
		}
	})

	t.Run("merge at capacity returns error", func(t *testing.T) {
		g := NewGraph("/project", WithMaxNodes(1))

		result := &ast.ParseResult{
			FilePath: "test.go",
			Language: "go",
			Symbols: []*ast.Symbol{
				makeSymbol("test.go:1:funcA", "funcA", ast.SymbolKindFunction, "test.go"),
				makeSymbol("test.go:10:funcB", "funcB", ast.SymbolKindFunction, "test.go"),
			},
		}

		added, err := g.MergeParseResult(result)
		if err == nil {
			t.Fatal("expected error at capacity")
		}
		if !errors.Is(err, ErrMaxNodesExceeded) {
			t.Errorf("expected ErrMaxNodesExceeded, got %v", err)
		}
		// First symbol should have been added
		if added != 1 {
			t.Errorf("added = %d, expected 1 (partial add before capacity)", added)
		}
	})
}

func TestGraph_GetNodesByFile(t *testing.T) {
	t.Run("returns nodes from file", func(t *testing.T) {
		g := NewGraph("/project")

		symA1 := makeSymbol("a.go:1:funcA1", "funcA1", ast.SymbolKindFunction, "a.go")
		symA2 := makeSymbol("a.go:10:funcA2", "funcA2", ast.SymbolKindFunction, "a.go")
		symB1 := makeSymbol("b.go:1:funcB1", "funcB1", ast.SymbolKindFunction, "b.go")

		g.AddNode(symA1)
		g.AddNode(symA2)
		g.AddNode(symB1)

		nodesA := g.GetNodesByFile("a.go")
		if len(nodesA) != 2 {
			t.Errorf("len(nodesA) = %d, expected 2", len(nodesA))
		}

		nodesB := g.GetNodesByFile("b.go")
		if len(nodesB) != 1 {
			t.Errorf("len(nodesB) = %d, expected 1", len(nodesB))
		}
	})

	t.Run("returns empty for non-existent file", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		nodes := g.GetNodesByFile("does-not-exist.go")
		if len(nodes) != 0 {
			t.Errorf("len(nodes) = %d, expected 0", len(nodes))
		}
	})
}
