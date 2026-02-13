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
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper: Generate node ID.
func nodeID(i int) string {
	return fmt.Sprintf("node%d", i)
}

// TestBuildHLDIterative_BasicTree tests iterative HLD on simple tree.
func TestBuildHLDIterative_BasicTree(t *testing.T) {
	g := NewGraph("/test")

	// Create tree: main -> foo -> bar
	//                   -> baz
	symbols := []*ast.Symbol{
		{ID: "main", Kind: ast.SymbolKindFunction, Name: "main"},
		{ID: "foo", Kind: ast.SymbolKindFunction, Name: "foo"},
		{ID: "bar", Kind: ast.SymbolKindFunction, Name: "bar"},
		{ID: "baz", Kind: ast.SymbolKindFunction, Name: "baz"},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
	}

	g.AddEdge("main", "foo", EdgeTypeCalls, ast.Location{})
	g.AddEdge("main", "baz", EdgeTypeCalls, ast.Location{})
	g.AddEdge("foo", "bar", EdgeTypeCalls, ast.Location{})
	g.Freeze()

	hld, err := BuildHLDIterative(context.Background(), g, "main")
	require.NoError(t, err)
	require.NotNil(t, hld)

	assert.Equal(t, 4, hld.NodeCount())

	// Verify stats
	stats := hld.Stats()
	assert.Greater(t, stats.HeavyPathCount, 0)
	assert.Greater(t, stats.ConstructionTime, int64(0))
}

// TestBuildHLDIterative_DeepLinearChain tests iterative DFS on deep tree.
func TestBuildHLDIterative_DeepLinearChain(t *testing.T) {
	g := NewGraph("/test")

	// Create deep linear chain: node0 -> node1 -> node2 -> ... -> node99
	depth := 100
	for i := 0; i < depth; i++ {
		sym := &ast.Symbol{
			ID:   nodeID(i),
			Kind: ast.SymbolKindFunction,
			Name: nodeID(i),
		}
		g.AddNode(sym)

		if i > 0 {
			g.AddEdge(nodeID(i-1), nodeID(i), EdgeTypeCalls, ast.Location{})
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(context.Background(), g, "node0")
	require.NoError(t, err)
	require.NotNil(t, hld)

	assert.Equal(t, depth, hld.NodeCount())

	// Verify depth increases monotonically
	for i := 0; i < depth; i++ {
		idx, ok := hld.NodeToIdx(nodeID(i))
		require.True(t, ok, "node %d should exist", i)
		assert.Equal(t, i, hld.Depth(idx), "node %d should have depth %d", i, i)
	}

	// Verify single heavy path (all heavy edges in linear chain)
	stats := hld.Stats()
	assert.Equal(t, 1, stats.HeavyPathCount, "linear chain should have 1 heavy path")
	assert.Equal(t, 0, stats.LightEdgeCount, "linear chain should have 0 light edges")
}

// TestBuildHLDIterative_VsRecursive compares iterative and recursive results.
func TestBuildHLDIterative_VsRecursive(t *testing.T) {
	g := createBalancedBinaryTree(t, 5) // 31 nodes

	hldRecursive, err := BuildHLD(context.Background(), g, "node0")
	require.NoError(t, err)

	hldIterative, err := BuildHLDIterative(context.Background(), g, "node0")
	require.NoError(t, err)

	// Both should produce identical results
	assert.Equal(t, hldRecursive.NodeCount(), hldIterative.NodeCount())

	// Compare parent arrays
	for i := 0; i < hldRecursive.NodeCount(); i++ {
		assert.Equal(t, hldRecursive.Parent(i), hldIterative.Parent(i),
			"parent mismatch at node %d", i)
		assert.Equal(t, hldRecursive.Depth(i), hldIterative.Depth(i),
			"depth mismatch at node %d", i)
		assert.Equal(t, hldRecursive.SubtreeSize(i), hldIterative.SubtreeSize(i),
			"subtree size mismatch at node %d", i)
	}

	// Compare stats
	statsRec := hldRecursive.Stats()
	statsIter := hldIterative.Stats()
	assert.Equal(t, statsRec.HeavyPathCount, statsIter.HeavyPathCount)
	assert.Equal(t, statsRec.LightEdgeCount, statsIter.LightEdgeCount)
}

// TestBuildHLDForest_SingleTree tests forest builder with single tree.
func TestBuildHLDForest_SingleTree(t *testing.T) {
	g := NewGraph("/test")

	// Create single tree: main -> foo -> bar
	symbols := []*ast.Symbol{
		{ID: "main", Kind: ast.SymbolKindFunction, Name: "main"},
		{ID: "foo", Kind: ast.SymbolKindFunction, Name: "foo"},
		{ID: "bar", Kind: ast.SymbolKindFunction, Name: "bar"},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
	}

	g.AddEdge("main", "foo", EdgeTypeCalls, ast.Location{})
	g.AddEdge("foo", "bar", EdgeTypeCalls, ast.Location{})
	g.Freeze()

	forest, err := BuildHLDForest(context.Background(), g, true)
	require.NoError(t, err)
	require.NotNil(t, forest)

	assert.Equal(t, 1, forest.TreeCount(), "should have 1 tree")
	assert.Equal(t, "main", forest.GetRoot(0))

	hld := forest.GetHLD("main")
	require.NotNil(t, hld)
	assert.Equal(t, 3, hld.NodeCount())
}

// TestBuildHLDForest_MultipleDisconnectedTrees tests actual forest.
func TestBuildHLDForest_MultipleDisconnectedTrees(t *testing.T) {
	g := NewGraph("/test")

	// Tree 1: main -> foo
	g.AddNode(&ast.Symbol{ID: "main", Kind: ast.SymbolKindFunction, Name: "main"})
	g.AddNode(&ast.Symbol{ID: "foo", Kind: ast.SymbolKindFunction, Name: "foo"})
	g.AddEdge("main", "foo", EdgeTypeCalls, ast.Location{})

	// Tree 2: alpha -> beta -> gamma
	g.AddNode(&ast.Symbol{ID: "alpha", Kind: ast.SymbolKindFunction, Name: "alpha"})
	g.AddNode(&ast.Symbol{ID: "beta", Kind: ast.SymbolKindFunction, Name: "beta"})
	g.AddNode(&ast.Symbol{ID: "gamma", Kind: ast.SymbolKindFunction, Name: "gamma"})
	g.AddEdge("alpha", "beta", EdgeTypeCalls, ast.Location{})
	g.AddEdge("beta", "gamma", EdgeTypeCalls, ast.Location{})

	// Tree 3: single node
	g.AddNode(&ast.Symbol{ID: "singleton", Kind: ast.SymbolKindFunction, Name: "singleton"})

	g.Freeze()

	forest, err := BuildHLDForest(context.Background(), g, true)
	require.NoError(t, err)
	require.NotNil(t, forest)

	assert.Equal(t, 3, forest.TreeCount(), "should have 3 trees")

	// Verify each tree
	hld1 := forest.GetHLD("main")
	require.NotNil(t, hld1)
	assert.Equal(t, 2, hld1.NodeCount())

	hld2 := forest.GetHLD("alpha")
	require.NotNil(t, hld2)
	assert.Equal(t, 3, hld2.NodeCount())

	hld3 := forest.GetHLD("singleton")
	require.NotNil(t, hld3)
	assert.Equal(t, 1, hld3.NodeCount())

	// Verify nodes map to correct trees
	assert.Equal(t, hld1, forest.GetHLD("foo"))
	assert.Equal(t, hld2, forest.GetHLD("beta"))
	assert.Equal(t, hld2, forest.GetHLD("gamma"))
}

// TestBuildHLDForest_RecursiveVsIterative compares forest builder modes.
func TestBuildHLDForest_RecursiveVsIterative(t *testing.T) {
	g := createTwoTreeForest()

	forestRec, err := BuildHLDForest(context.Background(), g, false)
	require.NoError(t, err)

	forestIter, err := BuildHLDForest(context.Background(), g, true)
	require.NoError(t, err)

	// Both should have same number of trees
	assert.Equal(t, forestRec.TreeCount(), forestIter.TreeCount())

	// Each tree should have same node count
	for i := 0; i < forestRec.TreeCount(); i++ {
		hldRec := forestRec.GetHLDByIndex(i)
		hldIter := forestIter.GetHLDByIndex(i)

		require.NotNil(t, hldRec)
		require.NotNil(t, hldIter)

		assert.Equal(t, hldRec.NodeCount(), hldIter.NodeCount(),
			"tree %d should have same node count", i)
	}
}

// TestHLDForest_GetMethods tests forest accessor methods.
func TestHLDForest_GetMethods(t *testing.T) {
	g := createTwoTreeForest()

	forest, err := BuildHLDForest(context.Background(), g, true)
	require.NoError(t, err)

	// Test GetHLDByIndex
	hld0 := forest.GetHLDByIndex(0)
	assert.NotNil(t, hld0)

	hld1 := forest.GetHLDByIndex(1)
	assert.NotNil(t, hld1)

	hldInvalid := forest.GetHLDByIndex(999)
	assert.Nil(t, hldInvalid, "invalid index should return nil")

	// Test GetRoot
	root0 := forest.GetRoot(0)
	assert.NotEmpty(t, root0)

	rootInvalid := forest.GetRoot(-1)
	assert.Empty(t, rootInvalid, "invalid index should return empty string")

	// Test GetHLD by node ID
	hld := forest.GetHLD("tree1_node0")
	assert.NotNil(t, hld)

	hldUnknown := forest.GetHLD("unknown_node")
	assert.Nil(t, hldUnknown, "unknown node should return nil")
}

// Helper: Create a two-tree forest for testing.
func createTwoTreeForest() *Graph {
	g := NewGraph("/test")

	// Tree 1: tree1_node0 -> tree1_node1 -> tree1_node2
	for i := 0; i < 3; i++ {
		g.AddNode(&ast.Symbol{
			ID:   nodeID2("tree1", i),
			Kind: ast.SymbolKindFunction,
			Name: nodeID2("tree1", i),
		})
		if i > 0 {
			g.AddEdge(nodeID2("tree1", i-1), nodeID2("tree1", i), EdgeTypeCalls, ast.Location{})
		}
	}

	// Tree 2: tree2_node0 -> tree2_node1
	for i := 0; i < 2; i++ {
		g.AddNode(&ast.Symbol{
			ID:   nodeID2("tree2", i),
			Kind: ast.SymbolKindFunction,
			Name: nodeID2("tree2", i),
		})
		if i > 0 {
			g.AddEdge(nodeID2("tree2", i-1), nodeID2("tree2", i), EdgeTypeCalls, ast.Location{})
		}
	}

	g.Freeze()
	return g
}

// Helper: Generate node ID for tree forests.
func nodeID2(tree string, i int) string {
	return fmt.Sprintf("%s_node%d", tree, i)
}

// TestHLDForest_GetTreeOffset tests tree offset calculation.
func TestHLDForest_GetTreeOffset(t *testing.T) {
	ctx := context.Background()
	g := NewGraph("/test")

	// Tree 0: A -> B -> C (3 nodes)
	g.AddNode(&ast.Symbol{ID: "A", Kind: ast.SymbolKindFunction, Name: "A"})
	g.AddNode(&ast.Symbol{ID: "B", Kind: ast.SymbolKindFunction, Name: "B"})
	g.AddNode(&ast.Symbol{ID: "C", Kind: ast.SymbolKindFunction, Name: "C"})
	g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{})
	g.AddEdge("B", "C", EdgeTypeCalls, ast.Location{})

	// Tree 1: X -> Y (2 nodes)
	g.AddNode(&ast.Symbol{ID: "X", Kind: ast.SymbolKindFunction, Name: "X"})
	g.AddNode(&ast.Symbol{ID: "Y", Kind: ast.SymbolKindFunction, Name: "Y"})
	g.AddEdge("X", "Y", EdgeTypeCalls, ast.Location{})

	// Tree 2: Z (1 node)
	g.AddNode(&ast.Symbol{ID: "Z", Kind: ast.SymbolKindFunction, Name: "Z"})

	g.Freeze()

	forest, err := BuildHLDForest(ctx, g, true)
	if err != nil {
		t.Fatalf("BuildHLDForest failed: %v", err)
	}

	// Verify offsets are consistent within each tree
	testCases := []struct {
		nodes []string // Nodes in same tree should have same offset
		name  string
	}{
		{nodes: []string{"A", "B", "C"}, name: "Tree with A,B,C"},
		{nodes: []string{"X", "Y"}, name: "Tree with X,Y"},
		{nodes: []string{"Z"}, name: "Tree with Z"},
	}

	// Track which offsets we've seen
	seenOffsets := make(map[int]bool)

	for _, tc := range testCases {
		// All nodes in same tree should have same offset
		var treeOffset int
		for i, node := range tc.nodes {
			offset, err := forest.GetTreeOffset(node)
			if err != nil {
				t.Errorf("%s: GetTreeOffset(%s) failed: %v", tc.name, node, err)
				continue
			}

			if i == 0 {
				treeOffset = offset
				seenOffsets[offset] = true
			} else {
				if offset != treeOffset {
					t.Errorf("%s: node %s has offset %d, expected %d (same as first node)",
						tc.name, node, offset, treeOffset)
				}
			}
		}
	}

	// Verify we have 3 distinct offsets
	if len(seenOffsets) != 3 {
		t.Errorf("Expected 3 distinct tree offsets, got %d", len(seenOffsets))
	}

	// Verify offsets are in valid range [0, TotalNodes)
	totalNodes := forest.TotalNodes()
	for offset := range seenOffsets {
		if offset < 0 || offset >= totalNodes {
			t.Errorf("Offset %d out of valid range [0,%d)", offset, totalNodes)
		}
	}

	// Non-existent node should return error
	_, err = forest.GetTreeOffset("NONEXISTENT")
	if err == nil {
		t.Error("GetTreeOffset(NONEXISTENT) should return error")
	}
}
