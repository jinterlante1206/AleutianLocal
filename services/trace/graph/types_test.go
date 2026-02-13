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

// =============================================================================
// GR-06/07/08: Secondary Index Tests
// =============================================================================

func TestGraph_GetNodesByName(t *testing.T) {
	t.Run("returns nodes with matching name", func(t *testing.T) {
		g := NewGraph("/project")

		// Add multiple symbols with same name in different packages
		sym1 := makeSymbol("pkg1/a.go:1:Setup", "Setup", ast.SymbolKindFunction, "pkg1/a.go")
		sym2 := makeSymbol("pkg2/a.go:1:Setup", "Setup", ast.SymbolKindFunction, "pkg2/a.go")
		sym3 := makeSymbol("pkg1/a.go:10:Other", "Other", ast.SymbolKindFunction, "pkg1/a.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		// Query by name
		nodes := g.GetNodesByName("Setup")
		if len(nodes) != 2 {
			t.Errorf("len(nodes) = %d, expected 2", len(nodes))
		}

		// Verify both Setup nodes are returned
		ids := make(map[string]bool)
		for _, n := range nodes {
			ids[n.ID] = true
		}
		if !ids[sym1.ID] || !ids[sym2.ID] {
			t.Error("expected both Setup nodes")
		}
	})

	t.Run("returns empty for non-existent name", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		nodes := g.GetNodesByName("NonExistent")
		if len(nodes) != 0 {
			t.Errorf("len(nodes) = %d, expected 0", len(nodes))
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		nodes1 := g.GetNodesByName("funcA")
		nodes2 := g.GetNodesByName("funcA")

		// Slices should be different (defensive copy)
		nodes1[0] = nil
		if nodes2[0] == nil {
			t.Error("modifying returned slice should not affect subsequent calls")
		}
	})

	t.Run("handles empty name symbols", func(t *testing.T) {
		g := NewGraph("/project")
		sym := &ast.Symbol{
			ID:       "a.go:1:anon",
			Name:     "", // Empty name
			Kind:     ast.SymbolKindFunction,
			FilePath: "a.go",
		}
		g.AddNode(sym)

		// Empty name should not be indexed
		nodes := g.GetNodesByName("")
		if len(nodes) != 0 {
			t.Errorf("empty name should not be indexed, got %d nodes", len(nodes))
		}
	})
}

func TestGraph_GetNodesByKind(t *testing.T) {
	t.Run("returns nodes of matching kind", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("a.go:10:funcB", "funcB", ast.SymbolKindFunction, "a.go")
		sym3 := makeSymbol("a.go:20:TypeC", "TypeC", ast.SymbolKindStruct, "a.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		functions := g.GetNodesByKind(ast.SymbolKindFunction)
		if len(functions) != 2 {
			t.Errorf("len(functions) = %d, expected 2", len(functions))
		}

		structs := g.GetNodesByKind(ast.SymbolKindStruct)
		if len(structs) != 1 {
			t.Errorf("len(structs) = %d, expected 1", len(structs))
		}
	})

	t.Run("returns empty for non-existent kind", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		nodes := g.GetNodesByKind(ast.SymbolKindInterface)
		if len(nodes) != 0 {
			t.Errorf("len(nodes) = %d, expected 0", len(nodes))
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		nodes1 := g.GetNodesByKind(ast.SymbolKindFunction)
		nodes2 := g.GetNodesByKind(ast.SymbolKindFunction)

		nodes1[0] = nil
		if nodes2[0] == nil {
			t.Error("modifying returned slice should not affect subsequent calls")
		}
	})
}

func TestGraph_GetEdgesByType(t *testing.T) {
	t.Run("returns edges of matching type", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:TypeC", "TypeC", ast.SymbolKindStruct, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 20)) // Second call site
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeReferences, makeLocation("a.go", 15))

		callEdges := g.GetEdgesByType(EdgeTypeCalls)
		if len(callEdges) != 2 {
			t.Errorf("len(callEdges) = %d, expected 2", len(callEdges))
		}

		refEdges := g.GetEdgesByType(EdgeTypeReferences)
		if len(refEdges) != 1 {
			t.Errorf("len(refEdges) = %d, expected 1", len(refEdges))
		}
	})

	t.Run("returns empty for non-existent type", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		edges := g.GetEdgesByType(EdgeTypeImports)
		if len(edges) != 0 {
			t.Errorf("len(edges) = %d, expected 0", len(edges))
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		edges1 := g.GetEdgesByType(EdgeTypeCalls)
		edges2 := g.GetEdgesByType(EdgeTypeCalls)

		edges1[0] = nil
		if edges2[0] == nil {
			t.Error("modifying returned slice should not affect subsequent calls")
		}
	})

	t.Run("handles invalid edge type", func(t *testing.T) {
		g := NewGraph("/project")

		edges := g.GetEdgesByType(EdgeType(-1))
		if len(edges) != 0 {
			t.Errorf("invalid edge type should return empty, got %d", len(edges))
		}

		edges = g.GetEdgesByType(EdgeType(999))
		if len(edges) != 0 {
			t.Errorf("out of range edge type should return empty, got %d", len(edges))
		}
	})
}

func TestGraph_IndexConsistency_RemoveFile(t *testing.T) {
	t.Run("indexes updated after RemoveFile", func(t *testing.T) {
		g := NewGraph("/project")

		// Add nodes from two files with same name
		symA := makeSymbol("a.go:1:Setup", "Setup", ast.SymbolKindFunction, "a.go")
		symB := makeSymbol("b.go:1:Setup", "Setup", ast.SymbolKindFunction, "b.go")
		symC := makeSymbol("a.go:10:Helper", "Helper", ast.SymbolKindFunction, "a.go")

		g.AddNode(symA)
		g.AddNode(symB)
		g.AddNode(symC)

		// Add edges
		g.AddEdge(symA.ID, symC.ID, EdgeTypeCalls, makeLocation("a.go", 5))
		g.AddEdge(symB.ID, symA.ID, EdgeTypeCalls, makeLocation("b.go", 5))

		// Remove file a.go
		removed, err := g.RemoveFile("a.go")
		if err != nil {
			t.Fatalf("RemoveFile failed: %v", err)
		}
		if removed != 2 {
			t.Errorf("removed = %d, expected 2", removed)
		}

		// Check nodesByName - "Setup" should only have 1 node now
		setupNodes := g.GetNodesByName("Setup")
		if len(setupNodes) != 1 {
			t.Errorf("Setup nodes = %d, expected 1", len(setupNodes))
		}
		if setupNodes[0].ID != symB.ID {
			t.Errorf("remaining Setup should be from b.go")
		}

		// Check nodesByName - "Helper" should be empty
		helperNodes := g.GetNodesByName("Helper")
		if len(helperNodes) != 0 {
			t.Errorf("Helper nodes = %d, expected 0", len(helperNodes))
		}

		// Check nodesByKind - should only have 1 function
		funcNodes := g.GetNodesByKind(ast.SymbolKindFunction)
		if len(funcNodes) != 1 {
			t.Errorf("function nodes = %d, expected 1", len(funcNodes))
		}

		// Check edgesByType - all call edges should be removed
		callEdges := g.GetEdgesByType(EdgeTypeCalls)
		if len(callEdges) != 0 {
			t.Errorf("call edges = %d, expected 0", len(callEdges))
		}
	})
}

func TestGraph_IndexConsistency_Clone(t *testing.T) {
	t.Run("indexes cloned correctly", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:Setup", "Setup", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:Setup", "Setup", ast.SymbolKindFunction, "b.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.Freeze()

		clone := g.Clone()

		// Check nodesByName in clone
		setupNodes := clone.GetNodesByName("Setup")
		if len(setupNodes) != 2 {
			t.Errorf("clone Setup nodes = %d, expected 2", len(setupNodes))
		}

		// Verify cloned nodes are different pointers
		origNodes := g.GetNodesByName("Setup")
		for i, cn := range setupNodes {
			if cn == origNodes[i] {
				t.Error("cloned nodes should be different pointers")
			}
		}

		// Check nodesByKind in clone
		funcNodes := clone.GetNodesByKind(ast.SymbolKindFunction)
		if len(funcNodes) != 2 {
			t.Errorf("clone function nodes = %d, expected 2", len(funcNodes))
		}

		// Check edgesByType in clone
		callEdges := clone.GetEdgesByType(EdgeTypeCalls)
		if len(callEdges) != 1 {
			t.Errorf("clone call edges = %d, expected 1", len(callEdges))
		}

		// Verify cloned edges are different pointers
		origEdges := g.GetEdgesByType(EdgeTypeCalls)
		if callEdges[0] == origEdges[0] {
			t.Error("cloned edges should be different pointers")
		}

		// Modify clone and verify original is unchanged
		sym3 := makeSymbol("c.go:1:NewFunc", "NewFunc", ast.SymbolKindFunction, "c.go")
		clone.AddNode(sym3)

		origFuncNodes := g.GetNodesByKind(ast.SymbolKindFunction)
		if len(origFuncNodes) != 2 {
			t.Errorf("original should still have 2 functions, got %d", len(origFuncNodes))
		}
	})
}

func TestGraph_IndexConsistency_MergeParseResult(t *testing.T) {
	t.Run("indexes updated after MergeParseResult", func(t *testing.T) {
		g := NewGraph("/project")

		// Add initial node
		existing := makeSymbol("a.go:1:Setup", "Setup", ast.SymbolKindFunction, "a.go")
		g.AddNode(existing)

		// Merge new symbols
		result := &ast.ParseResult{
			FilePath: "b.go",
			Language: "go",
			Symbols: []*ast.Symbol{
				makeSymbol("b.go:1:Setup", "Setup", ast.SymbolKindFunction, "b.go"),
				makeSymbol("b.go:10:Helper", "Helper", ast.SymbolKindMethod, "b.go"),
			},
		}

		added, err := g.MergeParseResult(result)
		if err != nil {
			t.Fatalf("MergeParseResult failed: %v", err)
		}
		if added != 2 {
			t.Errorf("added = %d, expected 2", added)
		}

		// Check nodesByName
		setupNodes := g.GetNodesByName("Setup")
		if len(setupNodes) != 2 {
			t.Errorf("Setup nodes = %d, expected 2", len(setupNodes))
		}

		helperNodes := g.GetNodesByName("Helper")
		if len(helperNodes) != 1 {
			t.Errorf("Helper nodes = %d, expected 1", len(helperNodes))
		}

		// Check nodesByKind
		funcNodes := g.GetNodesByKind(ast.SymbolKindFunction)
		if len(funcNodes) != 2 {
			t.Errorf("function nodes = %d, expected 2", len(funcNodes))
		}

		methodNodes := g.GetNodesByKind(ast.SymbolKindMethod)
		if len(methodNodes) != 1 {
			t.Errorf("method nodes = %d, expected 1", len(methodNodes))
		}
	})
}

func TestGraph_Stats_UsesIndexes(t *testing.T) {
	t.Run("Stats uses indexes for counts", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:TypeC", "TypeC", ast.SymbolKindStruct, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeReferences, makeLocation("a.go", 15))

		stats := g.Stats()

		// Verify NodesByKind
		if stats.NodesByKind[ast.SymbolKindFunction] != 2 {
			t.Errorf("NodesByKind[Function] = %d, expected 2", stats.NodesByKind[ast.SymbolKindFunction])
		}
		if stats.NodesByKind[ast.SymbolKindStruct] != 1 {
			t.Errorf("NodesByKind[Struct] = %d, expected 1", stats.NodesByKind[ast.SymbolKindStruct])
		}

		// Verify EdgesByType
		if stats.EdgesByType[EdgeTypeCalls] != 1 {
			t.Errorf("EdgesByType[Calls] = %d, expected 1", stats.EdgesByType[EdgeTypeCalls])
		}
		if stats.EdgesByType[EdgeTypeReferences] != 1 {
			t.Errorf("EdgesByType[References] = %d, expected 1", stats.EdgesByType[EdgeTypeReferences])
		}
	})
}

// Benchmarks for index operations
func BenchmarkGetNodesByName_WithIndex(b *testing.B) {
	g := NewGraph("/project")

	// Add 1000 nodes with 100 unique names
	for i := 0; i < 1000; i++ {
		name := "func" + string(rune('A'+i%100))
		id := "file.go:" + string(rune('0'+i)) + ":" + name
		sym := makeSymbol(id, name, ast.SymbolKindFunction, "file.go")
		g.AddNode(sym)
	}
	g.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.GetNodesByName("funcA")
	}
}

func BenchmarkGetNodesByKind_WithIndex(b *testing.B) {
	g := NewGraph("/project")

	// Add 1000 nodes with mixed kinds
	for i := 0; i < 1000; i++ {
		kind := ast.SymbolKindFunction
		if i%3 == 0 {
			kind = ast.SymbolKindStruct
		} else if i%3 == 1 {
			kind = ast.SymbolKindMethod
		}
		id := "file.go:" + string(rune('0'+i)) + ":sym"
		sym := makeSymbol(id, "sym", kind, "file.go")
		g.AddNode(sym)
	}
	g.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.GetNodesByKind(ast.SymbolKindFunction)
	}
}

func BenchmarkGetEdgesByType_WithIndex(b *testing.B) {
	g := NewGraph("/project")

	// Add nodes
	nodes := make([]*ast.Symbol, 100)
	for i := 0; i < 100; i++ {
		id := "file.go:" + string(rune('0'+i)) + ":func"
		nodes[i] = makeSymbol(id, "func", ast.SymbolKindFunction, "file.go")
		g.AddNode(nodes[i])
	}

	// Add 1000 edges with mixed types
	for i := 0; i < 1000; i++ {
		edgeType := EdgeTypeCalls
		if i%3 == 0 {
			edgeType = EdgeTypeImports
		} else if i%3 == 1 {
			edgeType = EdgeTypeReferences
		}
		from := nodes[i%100]
		to := nodes[(i+1)%100]
		g.AddEdge(from.ID, to.ID, edgeType, makeLocation("file.go", i))
	}
	g.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.GetEdgesByType(EdgeTypeCalls)
	}
}

// =============================================================================
// GR-06/07/08: Count Methods Tests
// =============================================================================

func TestGraph_GetEdgeCountByType(t *testing.T) {
	t.Run("returns correct count", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:TypeC", "TypeC", ast.SymbolKindStruct, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 20)) // Second call
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeReferences, makeLocation("a.go", 15))

		if count := g.GetEdgeCountByType(EdgeTypeCalls); count != 2 {
			t.Errorf("GetEdgeCountByType(Calls) = %d, expected 2", count)
		}
		if count := g.GetEdgeCountByType(EdgeTypeReferences); count != 1 {
			t.Errorf("GetEdgeCountByType(References) = %d, expected 1", count)
		}
		if count := g.GetEdgeCountByType(EdgeTypeImports); count != 0 {
			t.Errorf("GetEdgeCountByType(Imports) = %d, expected 0", count)
		}
	})

	t.Run("handles invalid type", func(t *testing.T) {
		g := NewGraph("/project")

		if count := g.GetEdgeCountByType(EdgeType(-1)); count != 0 {
			t.Errorf("invalid type should return 0, got %d", count)
		}
		if count := g.GetEdgeCountByType(EdgeType(999)); count != 0 {
			t.Errorf("out of range type should return 0, got %d", count)
		}
	})
}

func TestGraph_GetNodeCountByName(t *testing.T) {
	t.Run("returns correct count", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("pkg1/a.go:1:Setup", "Setup", ast.SymbolKindFunction, "pkg1/a.go")
		sym2 := makeSymbol("pkg2/a.go:1:Setup", "Setup", ast.SymbolKindFunction, "pkg2/a.go")
		sym3 := makeSymbol("pkg1/a.go:10:Other", "Other", ast.SymbolKindFunction, "pkg1/a.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		if count := g.GetNodeCountByName("Setup"); count != 2 {
			t.Errorf("GetNodeCountByName(Setup) = %d, expected 2", count)
		}
		if count := g.GetNodeCountByName("Other"); count != 1 {
			t.Errorf("GetNodeCountByName(Other) = %d, expected 1", count)
		}
		if count := g.GetNodeCountByName("NonExistent"); count != 0 {
			t.Errorf("GetNodeCountByName(NonExistent) = %d, expected 0", count)
		}
	})
}

func TestGraph_GetNodeCountByKind(t *testing.T) {
	t.Run("returns correct count", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("a.go:10:funcB", "funcB", ast.SymbolKindFunction, "a.go")
		sym3 := makeSymbol("a.go:20:TypeC", "TypeC", ast.SymbolKindStruct, "a.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		if count := g.GetNodeCountByKind(ast.SymbolKindFunction); count != 2 {
			t.Errorf("GetNodeCountByKind(Function) = %d, expected 2", count)
		}
		if count := g.GetNodeCountByKind(ast.SymbolKindStruct); count != 1 {
			t.Errorf("GetNodeCountByKind(Struct) = %d, expected 1", count)
		}
		if count := g.GetNodeCountByKind(ast.SymbolKindInterface); count != 0 {
			t.Errorf("GetNodeCountByKind(Interface) = %d, expected 0", count)
		}
	})
}

// =============================================================================
// GR-09: edgesByFile Index Tests
// =============================================================================

func TestGraph_GetEdgesByFile(t *testing.T) {
	t.Run("returns edges in matching file", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		// Add edges from different files
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeCalls, makeLocation("a.go", 20))
		g.AddEdge(sym2.ID, sym3.ID, EdgeTypeCalls, makeLocation("b.go", 10))

		// Query edges by file
		aEdges := g.GetEdgesByFile("a.go")
		if len(aEdges) != 2 {
			t.Errorf("GetEdgesByFile(a.go) = %d, expected 2", len(aEdges))
		}

		bEdges := g.GetEdgesByFile("b.go")
		if len(bEdges) != 1 {
			t.Errorf("GetEdgesByFile(b.go) = %d, expected 1", len(bEdges))
		}

		cEdges := g.GetEdgesByFile("c.go")
		if len(cEdges) != 0 {
			t.Errorf("GetEdgesByFile(c.go) = %d, expected 0 (no edges originate from c.go)", len(cEdges))
		}
	})

	t.Run("returns empty for non-existent file", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		edges := g.GetEdgesByFile("nonexistent.go")
		if len(edges) != 0 {
			t.Errorf("GetEdgesByFile(nonexistent.go) = %d, expected 0", len(edges))
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		edges1 := g.GetEdgesByFile("a.go")
		edges2 := g.GetEdgesByFile("a.go")

		edges1[0] = nil
		if edges2[0] == nil {
			t.Error("modifying returned slice should not affect subsequent calls")
		}
	})
}

func TestGraph_GetEdgeCountByFile(t *testing.T) {
	t.Run("returns correct count", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeCalls, makeLocation("a.go", 20))
		g.AddEdge(sym2.ID, sym3.ID, EdgeTypeCalls, makeLocation("b.go", 10))

		if count := g.GetEdgeCountByFile("a.go"); count != 2 {
			t.Errorf("GetEdgeCountByFile(a.go) = %d, expected 2", count)
		}
		if count := g.GetEdgeCountByFile("b.go"); count != 1 {
			t.Errorf("GetEdgeCountByFile(b.go) = %d, expected 1", count)
		}
		if count := g.GetEdgeCountByFile("c.go"); count != 0 {
			t.Errorf("GetEdgeCountByFile(c.go) = %d, expected 0", count)
		}
	})
}

func TestGraph_EdgesByFile_RemoveFile(t *testing.T) {
	t.Run("index updated after RemoveFile", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)

		// Edge from a.go calling b.go - location is a.go
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		// Edge from b.go calling c.go - location is b.go
		g.AddEdge(sym2.ID, sym3.ID, EdgeTypeCalls, makeLocation("b.go", 10))
		// Edge from a.go calling c.go - location is a.go
		g.AddEdge(sym1.ID, sym3.ID, EdgeTypeCalls, makeLocation("a.go", 20))

		// Before removal
		if count := g.GetEdgeCountByFile("a.go"); count != 2 {
			t.Errorf("before RemoveFile: a.go edges = %d, expected 2", count)
		}

		// Remove a.go - this removes sym1, and edges involving sym1
		removed, err := g.RemoveFile("a.go")
		if err != nil {
			t.Fatalf("RemoveFile failed: %v", err)
		}
		if removed != 1 {
			t.Errorf("removed = %d, expected 1", removed)
		}

		// After removal - edges from a.go should be removed
		if count := g.GetEdgeCountByFile("a.go"); count != 0 {
			t.Errorf("after RemoveFile: a.go edges = %d, expected 0", count)
		}

		// Edge from b.go -> c.go should still exist
		if count := g.GetEdgeCountByFile("b.go"); count != 1 {
			t.Errorf("after RemoveFile: b.go edges = %d, expected 1", count)
		}
	})
}

func TestGraph_EdgesByFile_Clone(t *testing.T) {
	t.Run("index cloned correctly", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.Freeze()

		clone := g.Clone()

		// Check index in clone
		if count := clone.GetEdgeCountByFile("a.go"); count != 1 {
			t.Errorf("clone a.go edges = %d, expected 1", count)
		}

		// Verify cloned edges are different pointers
		origEdges := g.GetEdgesByFile("a.go")
		cloneEdges := clone.GetEdgesByFile("a.go")
		if origEdges[0] == cloneEdges[0] {
			t.Error("cloned edges should be different pointers")
		}

		// Modify clone - add new edge
		sym3 := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")
		clone.AddNode(sym3)
		clone.AddEdge(sym2.ID, sym3.ID, EdgeTypeCalls, makeLocation("b.go", 10))

		// Original should be unchanged
		if count := g.GetEdgeCountByFile("b.go"); count != 0 {
			t.Errorf("original b.go edges = %d, expected 0", count)
		}
		if count := clone.GetEdgeCountByFile("b.go"); count != 1 {
			t.Errorf("clone b.go edges = %d, expected 1", count)
		}
	})
}

func BenchmarkGetEdgesByFile_WithIndex(b *testing.B) {
	g := NewGraph("/project")

	// Add nodes
	nodes := make([]*ast.Symbol, 100)
	for i := 0; i < 100; i++ {
		file := "file" + string(rune('A'+i%10)) + ".go"
		id := file + ":" + string(rune('0'+i)) + ":func"
		nodes[i] = makeSymbol(id, "func", ast.SymbolKindFunction, file)
		g.AddNode(nodes[i])
	}

	// Add 1000 edges across files
	for i := 0; i < 1000; i++ {
		from := nodes[i%100]
		to := nodes[(i+1)%100]
		file := "file" + string(rune('A'+i%10)) + ".go"
		g.AddEdge(from.ID, to.ID, EdgeTypeCalls, makeLocation(file, i))
	}
	g.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.GetEdgesByFile("fileA.go")
	}
}

func TestGraph_ValidateIndexes_OnFreeze(t *testing.T) {
	t.Run("freeze validates indexes", func(t *testing.T) {
		g := NewGraph("/project")

		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")

		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		// Freeze should complete without panic (validation passes)
		g.Freeze()

		// Verify graph is frozen
		if !g.IsFrozen() {
			t.Error("graph should be frozen")
		}

		// Verify indexes are consistent
		if g.GetNodeCountByName("funcA") != 1 {
			t.Error("nodesByName index should have funcA")
		}
		if g.GetNodeCountByKind(ast.SymbolKindFunction) != 2 {
			t.Error("nodesByKind index should have 2 functions")
		}
		if g.GetEdgeCountByType(EdgeTypeCalls) != 1 {
			t.Error("edgesByType index should have 1 call edge")
		}
	})
}

func BenchmarkStats_WithIndexes(b *testing.B) {
	g := NewGraph("/project")

	// Add 1000 nodes with mixed kinds
	for i := 0; i < 1000; i++ {
		kind := ast.SymbolKindFunction
		if i%3 == 0 {
			kind = ast.SymbolKindStruct
		} else if i%3 == 1 {
			kind = ast.SymbolKindMethod
		}
		id := "file.go:" + string(rune('0'+i)) + ":sym"
		sym := makeSymbol(id, "sym", kind, "file.go")
		g.AddNode(sym)
	}

	// Add edges between consecutive nodes
	for i := 0; i < 999; i++ {
		edgeType := EdgeTypeCalls
		if i%2 == 0 {
			edgeType = EdgeTypeReferences
		}
		fromID := "file.go:" + string(rune('0'+i)) + ":sym"
		toID := "file.go:" + string(rune('0'+i+1)) + ":sym"
		g.AddEdge(fromID, toID, edgeType, makeLocation("file.go", i))
	}
	g.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Stats()
	}
}

// =============================================================================
// GR-19c: Graph.Hash() Tests
// =============================================================================

func TestGraph_Hash(t *testing.T) {
	t.Run("nil graph returns empty hash", func(t *testing.T) {
		var g *Graph
		hash := g.Hash()
		if hash != "" {
			t.Errorf("nil graph Hash() = %q, expected empty string", hash)
		}
	})

	t.Run("empty graph returns consistent hash", func(t *testing.T) {
		g := NewGraph("/project")
		hash1 := g.Hash()
		hash2 := g.Hash()

		if hash1 == "" {
			t.Error("empty graph should have non-empty hash")
		}
		if hash1 != hash2 {
			t.Errorf("empty graph hash not consistent: %q != %q", hash1, hash2)
		}
	})

	t.Run("same structure produces same hash", func(t *testing.T) {
		// Build graph 1
		g1 := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g1.AddNode(sym1)
		g1.AddNode(sym2)
		g1.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		// Build identical graph 2
		g2 := NewGraph("/project")
		sym1b := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2b := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g2.AddNode(sym1b)
		g2.AddNode(sym2b)
		g2.AddEdge(sym1b.ID, sym2b.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		hash1 := g1.Hash()
		hash2 := g2.Hash()

		if hash1 != hash2 {
			t.Errorf("identical graphs have different hashes: %q != %q", hash1, hash2)
		}
	})

	t.Run("hash changes when node added", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym1)

		hash1 := g.Hash()

		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym2)

		hash2 := g.Hash()

		if hash1 == hash2 {
			t.Error("hash should change when node is added")
		}
	})

	t.Run("hash changes when edge added", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		g.AddNode(sym1)
		g.AddNode(sym2)

		hash1 := g.Hash()

		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		hash2 := g.Hash()

		if hash1 == hash2 {
			t.Error("hash should change when edge is added")
		}
	})

	t.Run("hash is deterministic with multiple calls", func(t *testing.T) {
		g := NewGraph("/project")
		sym1 := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		sym2 := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		sym3 := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")
		g.AddNode(sym1)
		g.AddNode(sym2)
		g.AddNode(sym3)
		g.AddEdge(sym1.ID, sym2.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g.AddEdge(sym2.ID, sym3.ID, EdgeTypeCalls, makeLocation("b.go", 10))

		// Call Hash() multiple times - should always return same value
		hashes := make([]string, 10)
		for i := 0; i < 10; i++ {
			hashes[i] = g.Hash()
		}

		for i := 1; i < 10; i++ {
			if hashes[i] != hashes[0] {
				t.Errorf("hash call %d returned %q, expected %q", i, hashes[i], hashes[0])
			}
		}
	})

	t.Run("node insertion order does not affect hash", func(t *testing.T) {
		// Graph 1: Add nodes in order A, B, C
		g1 := NewGraph("/project")
		symA := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		symB := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		symC := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")
		g1.AddNode(symA)
		g1.AddNode(symB)
		g1.AddNode(symC)

		// Graph 2: Add nodes in order C, A, B
		g2 := NewGraph("/project")
		symAb := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		symBb := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		symCb := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")
		g2.AddNode(symCb)
		g2.AddNode(symAb)
		g2.AddNode(symBb)

		hash1 := g1.Hash()
		hash2 := g2.Hash()

		if hash1 != hash2 {
			t.Errorf("node insertion order affected hash: %q != %q", hash1, hash2)
		}
	})

	t.Run("edge insertion order does not affect hash", func(t *testing.T) {
		symA := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		symB := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")
		symC := makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go")

		// Graph 1: Add edges A->B, B->C
		g1 := NewGraph("/project")
		g1.AddNode(makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go"))
		g1.AddNode(makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go"))
		g1.AddNode(makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go"))
		g1.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, makeLocation("a.go", 10))
		g1.AddEdge(symB.ID, symC.ID, EdgeTypeCalls, makeLocation("b.go", 10))

		// Graph 2: Add edges B->C, A->B
		g2 := NewGraph("/project")
		g2.AddNode(makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go"))
		g2.AddNode(makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go"))
		g2.AddNode(makeSymbol("c.go:1:funcC", "funcC", ast.SymbolKindFunction, "c.go"))
		g2.AddEdge(symB.ID, symC.ID, EdgeTypeCalls, makeLocation("b.go", 10))
		g2.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		hash1 := g1.Hash()
		hash2 := g2.Hash()

		if hash1 != hash2 {
			t.Errorf("edge insertion order affected hash: %q != %q", hash1, hash2)
		}
	})

	t.Run("different edge types produce different hashes", func(t *testing.T) {
		symA := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		symB := makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go")

		g1 := NewGraph("/project")
		g1.AddNode(makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go"))
		g1.AddNode(makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go"))
		g1.AddEdge(symA.ID, symB.ID, EdgeTypeCalls, makeLocation("a.go", 10))

		g2 := NewGraph("/project")
		g2.AddNode(makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go"))
		g2.AddNode(makeSymbol("b.go:1:funcB", "funcB", ast.SymbolKindFunction, "b.go"))
		g2.AddEdge(symA.ID, symB.ID, EdgeTypeReferences, makeLocation("a.go", 10))

		hash1 := g1.Hash()
		hash2 := g2.Hash()

		if hash1 == hash2 {
			t.Error("different edge types should produce different hashes")
		}
	})

	t.Run("hash format is 16-character hex", func(t *testing.T) {
		g := NewGraph("/project")
		sym := makeSymbol("a.go:1:funcA", "funcA", ast.SymbolKindFunction, "a.go")
		g.AddNode(sym)

		hash := g.Hash()

		if len(hash) != 16 {
			t.Errorf("hash length = %d, expected 16", len(hash))
		}

		// Verify it's all hex characters
		for _, ch := range hash {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
				t.Errorf("hash contains non-hex character: %c in %q", ch, hash)
			}
		}
	})
}
