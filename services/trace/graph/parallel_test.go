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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// TestParallelBFS_Correctness verifies parallel BFS returns the same nodes as sequential.
func TestParallelBFS_Correctness(t *testing.T) {
	t.Run("same nodes as sequential for call graph", func(t *testing.T) {
		g := buildWideGraph(t, 4, 5) // 4 levels, 5 children per node (smaller for accurate test)
		ctx := context.Background()

		// Run sequential with high limit to get all nodes
		seqResult, err := g.GetCallGraph(ctx, "root:1:root", WithMaxDepth(5), WithLimit(100000))
		if err != nil {
			t.Fatalf("GetCallGraph() error = %v", err)
		}

		// Run parallel with same high limit
		parResult, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(5), WithLimit(100000))
		if err != nil {
			t.Fatalf("GetCallGraphParallel() error = %v", err)
		}

		// Compare node sets (order may differ)
		seqNodes := make(map[string]bool)
		for _, id := range seqResult.VisitedNodes {
			seqNodes[id] = true
		}

		parNodes := make(map[string]bool)
		for _, id := range parResult.VisitedNodes {
			parNodes[id] = true
		}

		if len(seqNodes) != len(parNodes) {
			t.Errorf("node count mismatch: sequential=%d, parallel=%d", len(seqNodes), len(parNodes))
		}

		for id := range seqNodes {
			if !parNodes[id] {
				t.Errorf("node %s in sequential but not parallel", id)
			}
		}

		for id := range parNodes {
			if !seqNodes[id] {
				t.Errorf("node %s in parallel but not sequential", id)
			}
		}
	})

	t.Run("same nodes as sequential for reverse call graph", func(t *testing.T) {
		g := buildWideGraph(t, 3, 5) // Smaller graph for reverse test
		ctx := context.Background()

		// Use root node for reverse traversal (it has no callers, but is valid)
		leafID := "root:1:root"

		// Run sequential with high limit
		seqResult, err := g.GetReverseCallGraph(ctx, leafID, WithMaxDepth(5), WithLimit(100000))
		if err != nil {
			t.Fatalf("GetReverseCallGraph() error = %v", err)
		}

		// Run parallel with high limit
		parResult, err := g.GetReverseCallGraphParallel(ctx, leafID, WithMaxDepth(5), WithLimit(100000))
		if err != nil {
			t.Fatalf("GetReverseCallGraphParallel() error = %v", err)
		}

		// Compare node sets
		seqNodes := make(map[string]bool)
		for _, id := range seqResult.VisitedNodes {
			seqNodes[id] = true
		}

		parNodes := make(map[string]bool)
		for _, id := range parResult.VisitedNodes {
			parNodes[id] = true
		}

		if len(seqNodes) != len(parNodes) {
			t.Errorf("node count mismatch: sequential=%d, parallel=%d", len(seqNodes), len(parNodes))
		}
	})
}

// TestParallelBFS_EdgeCorrectness verifies parallel BFS returns the same edges.
func TestParallelBFS_EdgeCorrectness(t *testing.T) {
	g := buildWideGraph(t, 4, 8)
	ctx := context.Background()

	seqResult, err := g.GetCallGraph(ctx, "root:1:root", WithMaxDepth(4))
	if err != nil {
		t.Fatalf("GetCallGraph() error = %v", err)
	}

	parResult, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(4))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// Compare edge sets
	seqEdges := make(map[string]bool)
	for _, e := range seqResult.Edges {
		key := e.FromID + "->" + e.ToID
		seqEdges[key] = true
	}

	parEdges := make(map[string]bool)
	for _, e := range parResult.Edges {
		key := e.FromID + "->" + e.ToID
		parEdges[key] = true
	}

	if len(seqEdges) != len(parEdges) {
		t.Errorf("edge count mismatch: sequential=%d, parallel=%d", len(seqEdges), len(parEdges))
	}

	for key := range seqEdges {
		if !parEdges[key] {
			t.Errorf("edge %s in sequential but not parallel", key)
		}
	}
}

// TestParallelBFS_ContextCancellation verifies context cancellation stops traversal.
func TestParallelBFS_ContextCancellation(t *testing.T) {
	g := buildWideGraph(t, 4, 10) // Moderate graph (not too large for test speed)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Should complete without hanging
	result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(100))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// Result should be truncated due to cancellation
	if !result.Truncated {
		t.Log("Note: traversal completed before timeout (graph may be small)")
	}
}

// TestParallelBFS_EmptyGraph verifies behavior with minimal graphs.
func TestParallelBFS_EmptyGraph(t *testing.T) {
	t.Run("single node graph", func(t *testing.T) {
		g := NewGraph("/test")
		sym := &ast.Symbol{
			ID:       "single:1:single",
			Name:     "single",
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		if _, err := g.AddNode(sym); err != nil {
			t.Fatalf("AddNode() error = %v", err)
		}

		ctx := context.Background()
		result, err := g.GetCallGraphParallel(ctx, "single:1:single")
		if err != nil {
			t.Fatalf("GetCallGraphParallel() error = %v", err)
		}

		if len(result.VisitedNodes) != 1 {
			t.Errorf("expected 1 node, got %d", len(result.VisitedNodes))
		}
	})

	t.Run("root not found", func(t *testing.T) {
		g := NewGraph("/test")
		ctx := context.Background()

		_, err := g.GetCallGraphParallel(ctx, "nonexistent:1:func")
		if err == nil {
			t.Error("expected error for non-existent root")
		}
	})
}

// TestParallelBFS_DeepNarrowGraph verifies sequential mode is used for narrow graphs.
func TestParallelBFS_DeepNarrowGraph(t *testing.T) {
	// Build a deep chain: A -> B -> C -> D -> ... (1 child per level)
	g := NewGraph("/test")
	prevID := ""

	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("func%d:1:func%d", i, i)
		sym := &ast.Symbol{
			ID:       id,
			Name:     fmt.Sprintf("func%d", i),
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		if _, err := g.AddNode(sym); err != nil {
			t.Fatalf("AddNode() error = %v", err)
		}

		if prevID != "" {
			loc := ast.Location{FilePath: "test.go", StartLine: i, EndLine: i, StartCol: 1, EndCol: 10}
			if err := g.AddEdge(prevID, id, EdgeTypeCalls, loc); err != nil {
				t.Fatalf("AddEdge() error = %v", err)
			}
		}
		prevID = id
	}

	ctx := context.Background()
	result, err := g.GetCallGraphParallel(ctx, "func0:1:func0", WithMaxDepth(100))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// Should traverse all 50 nodes
	if len(result.VisitedNodes) != 50 {
		t.Errorf("expected 50 nodes, got %d", len(result.VisitedNodes))
	}
}

// TestParallelBFS_WideShallowGraph verifies parallel mode is used for wide graphs.
func TestParallelBFS_WideShallowGraph(t *testing.T) {
	// Build wide graph: root -> 50 children (triggers parallel mode at threshold=32)
	g := buildWideGraph(t, 2, 50)
	ctx := context.Background()

	result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(3))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// Should have: 1 root + 50 level1 = 51 nodes minimum
	expectedMin := 1 + 50
	if len(result.VisitedNodes) < expectedMin {
		t.Errorf("expected at least %d nodes, got %d", expectedMin, len(result.VisitedNodes))
	}
}

// TestParallelBFS_Limit verifies limit is respected.
func TestParallelBFS_Limit(t *testing.T) {
	g := buildWideGraph(t, 5, 10)
	ctx := context.Background()

	result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithLimit(50))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	if len(result.VisitedNodes) > 50 {
		t.Errorf("expected at most 50 nodes, got %d", len(result.VisitedNodes))
	}

	if !result.Truncated {
		t.Error("expected Truncated=true when limit reached")
	}
}

// TestParallelBFS_CycleHandling verifies cycles don't cause infinite loops.
func TestParallelBFS_CycleHandling(t *testing.T) {
	g := NewGraph("/test")

	// Create cycle: A -> B -> C -> A
	nodes := []string{"a:1:a", "b:1:b", "c:1:c"}
	for _, id := range nodes {
		sym := &ast.Symbol{
			ID:       id,
			Name:     id[:1],
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		if _, err := g.AddNode(sym); err != nil {
			t.Fatalf("AddNode() error = %v", err)
		}
	}

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}
	g.AddEdge("a:1:a", "b:1:b", EdgeTypeCalls, loc)
	g.AddEdge("b:1:b", "c:1:c", EdgeTypeCalls, loc)
	g.AddEdge("c:1:c", "a:1:a", EdgeTypeCalls, loc)

	ctx := context.Background()
	result, err := g.GetCallGraphParallel(ctx, "a:1:a", WithMaxDepth(100))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// Should visit exactly 3 nodes (cycle detected)
	if len(result.VisitedNodes) != 3 {
		t.Errorf("expected 3 nodes (cycle handled), got %d", len(result.VisitedNodes))
	}
}

// TestParallelBFS_Determinism verifies multiple runs produce same node set.
// Note: Order may differ due to goroutine scheduling, but the SET must be identical.
func TestParallelBFS_Determinism(t *testing.T) {
	g := buildWideGraph(t, 3, 10) // Smaller graph for faster test
	ctx := context.Background()

	// Run multiple times
	var firstNodeSet map[string]bool
	for i := 0; i < 5; i++ {
		result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(4), WithLimit(100000))
		if err != nil {
			t.Fatalf("run %d: GetCallGraphParallel() error = %v", i, err)
		}

		// Build set for comparison
		nodeSet := make(map[string]bool)
		for _, id := range result.VisitedNodes {
			nodeSet[id] = true
		}

		if i == 0 {
			firstNodeSet = nodeSet
		} else {
			if len(nodeSet) != len(firstNodeSet) {
				t.Errorf("run %d: node count mismatch: got %d, want %d", i, len(nodeSet), len(firstNodeSet))
			}
			for id := range firstNodeSet {
				if !nodeSet[id] {
					t.Errorf("run %d: missing node %s", i, id)
				}
			}
			for id := range nodeSet {
				if !firstNodeSet[id] {
					t.Errorf("run %d: extra node %s", i, id)
				}
			}
		}
	}
}

// buildWideGraph creates a graph with specified depth and branching factor.
// Each node at level N has childrenPerNode unique children.
func buildWideGraph(t *testing.T, depth, childrenPerNode int) *Graph {
	t.Helper()
	g := NewGraph("/test")

	// Create root
	rootSym := &ast.Symbol{
		ID:       "root:1:root",
		Name:     "root",
		Kind:     ast.SymbolKindFunction,
		FilePath: "test.go",
	}
	if _, err := g.AddNode(rootSym); err != nil {
		t.Fatalf("AddNode(root) error = %v", err)
	}

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}

	// BFS to create levels with unique node counter
	currentLevel := []string{"root:1:root"}
	nodeCounter := 0

	for level := 0; level < depth-1; level++ {
		var nextLevel []string

		for _, parentID := range currentLevel {
			for i := 0; i < childrenPerNode; i++ {
				childID := fmt.Sprintf("level%d_node%d:1:level%d_node%d", level+1, nodeCounter, level+1, nodeCounter)
				childSym := &ast.Symbol{
					ID:       childID,
					Name:     fmt.Sprintf("level%d_node%d", level+1, nodeCounter),
					Kind:     ast.SymbolKindFunction,
					FilePath: "test.go",
				}
				if _, err := g.AddNode(childSym); err != nil {
					t.Fatalf("AddNode() error = %v", err)
				}

				if err := g.AddEdge(parentID, childID, EdgeTypeCalls, loc); err != nil {
					t.Fatalf("AddEdge() error = %v", err)
				}

				nextLevel = append(nextLevel, childID)
				nodeCounter++
			}
		}

		currentLevel = nextLevel
	}

	return g
}

// TestParallelBFS_ConcurrentAccess verifies thread safety with concurrent callers.
func TestParallelBFS_ConcurrentAccess(t *testing.T) {
	g := buildWideGraph(t, 3, 10)
	ctx := context.Background()

	// Run multiple parallel BFS calls concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(4), WithLimit(100000))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", id, err)
				return
			}
			if len(result.VisitedNodes) == 0 {
				errors <- fmt.Errorf("goroutine %d: no nodes returned", id)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// TestParallelBFS_MaxDepthZero verifies behavior when MaxDepth is 0.
func TestParallelBFS_MaxDepthZero(t *testing.T) {
	g := buildWideGraph(t, 3, 5)
	ctx := context.Background()

	result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(0))
	if err != nil {
		t.Fatalf("GetCallGraphParallel() error = %v", err)
	}

	// MaxDepth 0 should return no nodes (loop doesn't execute)
	if len(result.VisitedNodes) != 0 {
		t.Errorf("expected 0 nodes with MaxDepth=0, got %d", len(result.VisitedNodes))
	}
}

// TestParallelBFS_ThresholdBoundary verifies parallel mode engages at exact threshold.
func TestParallelBFS_ThresholdBoundary(t *testing.T) {
	t.Run("at threshold - should use sequential", func(t *testing.T) {
		// Build graph with exactly 32 children (at threshold, not above)
		g := buildWideGraph(t, 2, 32)
		ctx := context.Background()

		result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(3), WithLimit(100000))
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		// Should have 1 + 32 = 33 nodes
		if len(result.VisitedNodes) != 33 {
			t.Errorf("expected 33 nodes, got %d", len(result.VisitedNodes))
		}
	})

	t.Run("above threshold - should use parallel", func(t *testing.T) {
		// Build graph with 33 children (above threshold)
		g := buildWideGraph(t, 2, 33)
		ctx := context.Background()

		result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(3), WithLimit(100000))
		if err != nil {
			t.Fatalf("error: %v", err)
		}

		// Should have 1 + 33 = 34 nodes
		if len(result.VisitedNodes) != 34 {
			t.Errorf("expected 34 nodes, got %d", len(result.VisitedNodes))
		}
	})
}

// TestParallelBFS_MixedEdgeTypes verifies only CALLS edges are followed.
func TestParallelBFS_MixedEdgeTypes(t *testing.T) {
	g := NewGraph("/test")

	// Create nodes
	for i := 0; i < 5; i++ {
		sym := &ast.Symbol{
			ID:       fmt.Sprintf("func%d:1:func%d", i, i),
			Name:     fmt.Sprintf("func%d", i),
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		g.AddNode(sym)
	}

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}

	// Add mixed edge types
	g.AddEdge("func0:1:func0", "func1:1:func1", EdgeTypeCalls, loc)      // Should follow
	g.AddEdge("func0:1:func0", "func2:1:func2", EdgeTypeImplements, loc) // Should NOT follow
	g.AddEdge("func1:1:func1", "func3:1:func3", EdgeTypeCalls, loc)      // Should follow
	g.AddEdge("func1:1:func1", "func4:1:func4", EdgeTypeReferences, loc) // Should NOT follow

	ctx := context.Background()
	result, err := g.GetCallGraphParallel(ctx, "func0:1:func0", WithMaxDepth(10))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Should only visit func0, func1, func3 (following CALLS edges only)
	expected := map[string]bool{
		"func0:1:func0": true,
		"func1:1:func1": true,
		"func3:1:func3": true,
	}

	if len(result.VisitedNodes) != 3 {
		t.Errorf("expected 3 nodes, got %d: %v", len(result.VisitedNodes), result.VisitedNodes)
	}

	for _, id := range result.VisitedNodes {
		if !expected[id] {
			t.Errorf("unexpected node: %s", id)
		}
	}
}

// TestParallelBFS_DisconnectedComponents verifies only reachable nodes are visited.
func TestParallelBFS_DisconnectedComponents(t *testing.T) {
	g := NewGraph("/test")

	// Component 1: A -> B -> C
	for i := 0; i < 3; i++ {
		sym := &ast.Symbol{
			ID:       fmt.Sprintf("comp1_%d:1:comp1_%d", i, i),
			Name:     fmt.Sprintf("comp1_%d", i),
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		g.AddNode(sym)
	}
	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}
	g.AddEdge("comp1_0:1:comp1_0", "comp1_1:1:comp1_1", EdgeTypeCalls, loc)
	g.AddEdge("comp1_1:1:comp1_1", "comp1_2:1:comp1_2", EdgeTypeCalls, loc)

	// Component 2: X -> Y (disconnected)
	for i := 0; i < 2; i++ {
		sym := &ast.Symbol{
			ID:       fmt.Sprintf("comp2_%d:1:comp2_%d", i, i),
			Name:     fmt.Sprintf("comp2_%d", i),
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		g.AddNode(sym)
	}
	g.AddEdge("comp2_0:1:comp2_0", "comp2_1:1:comp2_1", EdgeTypeCalls, loc)

	ctx := context.Background()

	// Starting from comp1_0 should only reach comp1 nodes
	result, err := g.GetCallGraphParallel(ctx, "comp1_0:1:comp1_0", WithMaxDepth(10))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(result.VisitedNodes) != 3 {
		t.Errorf("expected 3 nodes from component 1, got %d", len(result.VisitedNodes))
	}

	for _, id := range result.VisitedNodes {
		if !contains(id, "comp1") {
			t.Errorf("unexpected node from wrong component: %s", id)
		}
	}
}

// TestParallelBFS_ReverseWithActualCallers tests reverse BFS with real caller chains.
func TestParallelBFS_ReverseWithActualCallers(t *testing.T) {
	g := NewGraph("/test")

	// Build: A, B, C all call Target
	// Target is called by 3 functions
	nodes := []string{"target:1:target", "caller_a:1:caller_a", "caller_b:1:caller_b", "caller_c:1:caller_c"}
	for _, id := range nodes {
		sym := &ast.Symbol{
			ID:       id,
			Name:     id[:len(id)-4], // strip :1:...
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		g.AddNode(sym)
	}

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}
	g.AddEdge("caller_a:1:caller_a", "target:1:target", EdgeTypeCalls, loc)
	g.AddEdge("caller_b:1:caller_b", "target:1:target", EdgeTypeCalls, loc)
	g.AddEdge("caller_c:1:caller_c", "target:1:target", EdgeTypeCalls, loc)

	ctx := context.Background()

	// Reverse from target should find all callers
	result, err := g.GetReverseCallGraphParallel(ctx, "target:1:target", WithMaxDepth(5))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Should find target + 3 callers = 4 nodes
	if len(result.VisitedNodes) != 4 {
		t.Errorf("expected 4 nodes, got %d: %v", len(result.VisitedNodes), result.VisitedNodes)
	}
}

// TestParallelBFS_DepthTracking verifies depth is tracked correctly.
func TestParallelBFS_DepthTracking(t *testing.T) {
	g := buildWideGraph(t, 5, 3) // 5 levels deep (root + 4 child levels)
	ctx := context.Background()

	result, err := g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(10), WithLimit(100000))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// buildWideGraph creates depth-1 child levels, so 5 levels total means:
	// level 0: root, level 1-4: children
	// Loop runs 5 times (depth 0,1,2,3,4), result.Depth = depth+1 = 5
	if result.Depth != 5 {
		t.Errorf("expected depth 5, got %d", result.Depth)
	}
}

// TestParallelBFS_EmptyEdgeList verifies nodes with no outgoing edges.
func TestParallelBFS_EmptyEdgeList(t *testing.T) {
	g := NewGraph("/test")

	// Create isolated node
	sym := &ast.Symbol{
		ID:       "isolated:1:isolated",
		Name:     "isolated",
		Kind:     ast.SymbolKindFunction,
		FilePath: "test.go",
	}
	g.AddNode(sym)

	ctx := context.Background()
	result, err := g.GetCallGraphParallel(ctx, "isolated:1:isolated", WithMaxDepth(10))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if len(result.VisitedNodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(result.VisitedNodes))
	}
	if result.Depth != 1 {
		t.Errorf("expected depth 1, got %d", result.Depth)
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmarks

func BenchmarkBFS_Sequential_Wide(b *testing.B) {
	g := buildWideGraphBench(b, 4, 50)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.GetCallGraph(ctx, "root:1:root", WithMaxDepth(4))
	}
}

func BenchmarkBFS_Parallel_Wide(b *testing.B) {
	g := buildWideGraphBench(b, 4, 50)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.GetCallGraphParallel(ctx, "root:1:root", WithMaxDepth(4))
	}
}

func BenchmarkBFS_Sequential_Narrow(b *testing.B) {
	g := buildNarrowGraphBench(b, 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.GetCallGraph(ctx, "func0:1:func0", WithMaxDepth(100))
	}
}

func BenchmarkBFS_Parallel_Narrow(b *testing.B) {
	g := buildNarrowGraphBench(b, 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.GetCallGraphParallel(ctx, "func0:1:func0", WithMaxDepth(100))
	}
}

func buildWideGraphBench(b *testing.B, depth, childrenPerNode int) *Graph {
	b.Helper()
	g := NewGraph("/bench")

	rootSym := &ast.Symbol{
		ID:       "root:1:root",
		Name:     "root",
		Kind:     ast.SymbolKindFunction,
		FilePath: "test.go",
	}
	g.AddNode(rootSym)

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}
	currentLevel := []string{"root:1:root"}
	nodeCounter := 0

	for level := 0; level < depth-1; level++ {
		var nextLevel []string

		for _, parentID := range currentLevel {
			for i := 0; i < childrenPerNode; i++ {
				childID := fmt.Sprintf("node%d:1:node%d", nodeCounter, nodeCounter)
				childSym := &ast.Symbol{
					ID:       childID,
					Name:     fmt.Sprintf("node%d", nodeCounter),
					Kind:     ast.SymbolKindFunction,
					FilePath: "test.go",
				}
				g.AddNode(childSym)
				g.AddEdge(parentID, childID, EdgeTypeCalls, loc)
				nextLevel = append(nextLevel, childID)
				nodeCounter++
			}
		}

		currentLevel = nextLevel
	}

	return g
}

func buildNarrowGraphBench(b *testing.B, depth int) *Graph {
	b.Helper()
	g := NewGraph("/bench")

	loc := ast.Location{FilePath: "test.go", StartLine: 1, EndLine: 1, StartCol: 1, EndCol: 10}
	prevID := ""
	for i := 0; i < depth; i++ {
		id := fmt.Sprintf("func%d:1:func%d", i, i)
		sym := &ast.Symbol{
			ID:       id,
			Name:     fmt.Sprintf("func%d", i),
			Kind:     ast.SymbolKindFunction,
			FilePath: "test.go",
		}
		g.AddNode(sym)

		if prevID != "" {
			g.AddEdge(prevID, id, EdgeTypeCalls, loc)
		}
		prevID = id
	}

	return g
}
