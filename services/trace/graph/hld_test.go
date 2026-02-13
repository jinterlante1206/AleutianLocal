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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a simple tree graph for testing
func createSimpleTree(t *testing.T) *Graph {
	g := NewGraph("/test")

	// Create nodes
	nodes := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	for _, id := range nodes {
		sym := &ast.Symbol{
			ID:   id,
			Name: id,
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(sym)
	}

	// Create tree edges:
	//           1
	//          /|\
	//         2 3 4
	//        /|    \
	//       5 6     7
	//      /|
	//     8 9
	edges := [][2]string{
		{"1", "2"}, {"1", "3"}, {"1", "4"},
		{"2", "5"}, {"2", "6"},
		{"4", "7"},
		{"5", "8"}, {"5", "9"},
	}

	for _, edge := range edges {
		g.AddEdge(edge[0], edge[1], EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()
	return g
}

// Helper function to create a single-node tree
func createSingleNodeTree(t *testing.T) *Graph {
	g := NewGraph("/test")
	sym := &ast.Symbol{
		ID:   "main",
		Name: "main",
		Kind: ast.SymbolKindFunction,
	}
	g.AddNode(sym)
	g.Freeze()
	return g
}

// Helper function to create a linear chain tree
func createLinearChain(t *testing.T, n int) *Graph {
	g := NewGraph("/test")

	// Create n nodes in a chain: 0 → 1 → 2 → ... → n-1
	for i := 0; i < n; i++ {
		sym := &ast.Symbol{
			ID:   fmt.Sprintf("node%d", i),
			Name: fmt.Sprintf("node%d", i),
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(sym)
	}

	// Create edges
	for i := 0; i < n-1; i++ {
		g.AddEdge(fmt.Sprintf("node%d", i), fmt.Sprintf("node%d", i+1), EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()
	return g
}

// Helper function to create a star graph (center with many leaves)
func createStarGraph(t *testing.T, center string, numLeaves int) *Graph {
	g := NewGraph("/test")

	// Create center node
	centerSym := &ast.Symbol{
		ID:   center,
		Name: center,
		Kind: ast.SymbolKindFunction,
	}
	g.AddNode(centerSym)

	// Create leaf nodes
	for i := 0; i < numLeaves; i++ {
		leafID := fmt.Sprintf("leaf%d", i)
		leafSym := &ast.Symbol{
			ID:   leafID,
			Name: leafID,
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(leafSym)

		// Connect center to leaf
		g.AddEdge(center, leafID, EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()
	return g
}

// Helper function to create a balanced binary tree
func createBalancedBinaryTree(t *testing.T, depth int) *Graph {
	g := NewGraph("/test")

	// Helper to create tree recursively
	var createSubtree func(id int, currentDepth int)
	createSubtree = func(id int, currentDepth int) {
		nodeID := fmt.Sprintf("node%d", id)
		sym := &ast.Symbol{
			ID:   nodeID,
			Name: nodeID,
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(sym)

		if currentDepth < depth {
			leftID := 2*id + 1
			rightID := 2*id + 2

			createSubtree(leftID, currentDepth+1)
			createSubtree(rightID, currentDepth+1)

			g.AddEdge(nodeID, fmt.Sprintf("node%d", leftID), EdgeTypeCalls, ast.Location{})
			g.AddEdge(nodeID, fmt.Sprintf("node%d", rightID), EdgeTypeCalls, ast.Location{})
		}
	}

	createSubtree(0, 0)
	g.Freeze()
	return g
}

// Helper function to create a graph with cycles (not a tree)
func createCyclicGraph(t *testing.T) *Graph {
	g := NewGraph("/test")

	nodes := []string{"A", "B", "C"}
	for _, id := range nodes {
		sym := &ast.Symbol{
			ID:   id,
			Name: id,
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(sym)
	}

	// Create cycle: A → B → C → A
	g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{})
	g.AddEdge("B", "C", EdgeTypeCalls, ast.Location{})
	g.AddEdge("C", "A", EdgeTypeCalls, ast.Location{})

	g.Freeze()
	return g
}

// Helper function to create a disconnected graph (forest)
func createDisconnectedGraph(t *testing.T) *Graph {
	g := NewGraph("/test")

	// Tree 1: A → B
	nodes := []string{"A", "B", "C", "D"}
	for _, id := range nodes {
		sym := &ast.Symbol{
			ID:   id,
			Name: id,
			Kind: ast.SymbolKindFunction,
		}
		g.AddNode(sym)
	}

	g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{})
	// Tree 2: C → D (disconnected from A-B)
	g.AddEdge("C", "D", EdgeTypeCalls, ast.Location{})

	g.Freeze()
	return g
}

func TestBuildHLD_BasicTree(t *testing.T) {
	g := createSimpleTree(t)

	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)
	require.NotNil(t, hld)

	// Check basic properties
	assert.Equal(t, 9, hld.NodeCount())

	// Root node "1" should have parent -1
	rootIdx, ok := hld.NodeToIdx("1")
	require.True(t, ok)
	assert.Equal(t, -1, hld.Parent(rootIdx))
	assert.Equal(t, 0, hld.Depth(rootIdx))
	assert.Equal(t, 9, hld.SubtreeSize(rootIdx))

	// Node "2" should have largest subtree (5 nodes), so it's heavy child of "1"
	node2Idx, ok := hld.NodeToIdx("2")
	require.True(t, ok)
	assert.Equal(t, node2Idx, hld.HeavyChild(rootIdx))

	// Validate HLD structure
	assert.NoError(t, hld.Validate())

	// Check statistics
	stats := hld.Stats()
	assert.Greater(t, stats.HeavyPathCount, 0)
	assert.Greater(t, stats.LightEdgeCount, 0)
	assert.Greater(t, stats.MaxPathLength, 0)
}

func TestBuildHLD_SingleNode(t *testing.T) {
	g := createSingleNodeTree(t)

	hld, err := BuildHLD(context.Background(), g, "main")
	require.NoError(t, err)
	require.NotNil(t, hld)

	// Check properties
	assert.Equal(t, 1, hld.NodeCount())

	mainIdx, ok := hld.NodeToIdx("main")
	require.True(t, ok)

	assert.Equal(t, -1, hld.Parent(mainIdx))
	assert.Equal(t, 0, hld.Depth(mainIdx))
	assert.Equal(t, 1, hld.SubtreeSize(mainIdx))
	assert.Equal(t, -1, hld.HeavyChild(mainIdx))

	// Validate
	assert.NoError(t, hld.Validate())

	// Check stats
	stats := hld.Stats()
	assert.Equal(t, 1, stats.NodeCount)
	assert.Equal(t, 1, stats.HeavyPathCount)
	assert.Equal(t, 0, stats.LightEdgeCount)
}

func TestBuildHLD_LinearChain(t *testing.T) {
	g := createLinearChain(t, 10)

	hld, err := BuildHLD(context.Background(), g, "node0")
	require.NoError(t, err)
	require.NotNil(t, hld)

	// All nodes should be on same heavy path (head = root)
	rootIdx, _ := hld.NodeToIdx("node0")
	for i := 0; i < 10; i++ {
		nodeIdx, ok := hld.NodeToIdx(fmt.Sprintf("node%d", i))
		require.True(t, ok)
		assert.Equal(t, rootIdx, hld.Head(nodeIdx), "node%d should have head = root", i)
	}

	// All edges should be heavy (0 light edges)
	stats := hld.Stats()
	assert.Equal(t, 0, stats.LightEdgeCount, "linear chain should have 0 light edges")
	assert.Equal(t, 1, stats.HeavyPathCount, "linear chain should have 1 heavy path")
	assert.Equal(t, 10, stats.MaxPathLength, "path length should be 10")

	assert.NoError(t, hld.Validate())
}

func TestBuildHLD_StarGraph(t *testing.T) {
	g := createStarGraph(t, "center", 10)

	hld, err := BuildHLD(context.Background(), g, "center")
	require.NoError(t, err)
	require.NotNil(t, hld)

	// Center should have subtree size 11 (itself + 10 leaves)
	centerIdx, ok := hld.NodeToIdx("center")
	require.True(t, ok)
	assert.Equal(t, 11, hld.SubtreeSize(centerIdx))

	// Center should have a heavy child (first leaf lexicographically)
	heavyChild := hld.HeavyChild(centerIdx)
	require.NotEqual(t, -1, heavyChild)

	// Heavy child should be "leaf0" (first lexicographically)
	heavyChildID, err := hld.IdxToNode(heavyChild)
	require.NoError(t, err)
	assert.Equal(t, "leaf0", heavyChildID)

	// All other edges should be light (9 light edges)
	stats := hld.Stats()
	assert.Equal(t, 9, stats.LightEdgeCount)

	assert.NoError(t, hld.Validate())
}

func TestBuildHLD_BalancedBinaryTree(t *testing.T) {
	g := createBalancedBinaryTree(t, 3) // Depth 3 = 15 nodes

	hld, err := BuildHLD(context.Background(), g, "node0")
	require.NoError(t, err)
	require.NotNil(t, hld)

	// Check node count
	expectedNodes := (1 << 4) - 1 // 2^4 - 1 = 15
	assert.Equal(t, expectedNodes, hld.NodeCount())

	// Validate structure
	assert.NoError(t, hld.Validate())

	// Check stats
	stats := hld.Stats()
	assert.Greater(t, stats.HeavyPathCount, 1)
	assert.Greater(t, stats.LightEdgeCount, 0)
	assert.LessOrEqual(t, stats.MaxPathLength, 4) // Depth 3 = height 4
}

func TestBuildHLD_InvalidInputs(t *testing.T) {
	g := createSimpleTree(t)

	t.Run("nil context", func(t *testing.T) {
		_, err := BuildHLD(nil, g, "1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("nil graph", func(t *testing.T) {
		_, err := BuildHLD(context.Background(), nil, "1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "graph must not be nil")
	})

	t.Run("unfrozen graph", func(t *testing.T) {
		unfrozen := NewGraph("/test")
		sym := &ast.Symbol{ID: "A", Name: "A", Kind: ast.SymbolKindFunction}
		unfrozen.AddNode(sym)
		// Don't freeze

		_, err := BuildHLD(context.Background(), unfrozen, "A")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrGraphNotFrozen)
	})

	t.Run("empty graph", func(t *testing.T) {
		empty := NewGraph("/test")
		empty.Freeze()

		_, err := BuildHLD(context.Background(), empty, "A")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "graph is empty")
	})

	t.Run("root not in graph", func(t *testing.T) {
		_, err := BuildHLD(context.Background(), g, "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("graph with cycle", func(t *testing.T) {
		cyclic := createCyclicGraph(t)
		_, err := BuildHLD(context.Background(), cyclic, "A")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTree)
	})

	t.Run("disconnected graph", func(t *testing.T) {
		disconnected := createDisconnectedGraph(t)
		_, err := BuildHLD(context.Background(), disconnected, "A")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTree)
		assert.Contains(t, err.Error(), "disconnected")
	})
}

func TestBuildHLD_ContextCancellation(t *testing.T) {
	g := createLinearChain(t, 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := BuildHLD(ctx, g, "node0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestHLDValidate_ValidStructure(t *testing.T) {
	g := createSimpleTree(t)
	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	// Validate should pass
	assert.NoError(t, hld.Validate())
}

func TestHLD_Determinism(t *testing.T) {
	// Build HLD twice with same graph, verify identical structure
	g := createSimpleTree(t)

	hld1, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	hld2, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	// Check that all arrays are identical
	assert.Equal(t, hld1.nodeCount, hld2.nodeCount)

	for i := 0; i < hld1.nodeCount; i++ {
		assert.Equal(t, hld1.parent[i], hld2.parent[i], "parent mismatch at %d", i)
		assert.Equal(t, hld1.depth[i], hld2.depth[i], "depth mismatch at %d", i)
		assert.Equal(t, hld1.subSize[i], hld2.subSize[i], "subSize mismatch at %d", i)
		assert.Equal(t, hld1.heavy[i], hld2.heavy[i], "heavy mismatch at %d", i)
		assert.Equal(t, hld1.head[i], hld2.head[i], "head mismatch at %d", i)
		assert.Equal(t, hld1.pos[i], hld2.pos[i], "pos mismatch at %d", i)
		assert.Equal(t, hld1.nodeAtPos[i], hld2.nodeAtPos[i], "nodeAtPos mismatch at %d", i)
	}

	// Check cache keys are identical
	assert.Equal(t, hld1.CacheKey(), hld2.CacheKey())
}

func TestHLD_CacheKey(t *testing.T) {
	g1 := createSimpleTree(t)
	hld1, err := BuildHLD(context.Background(), g1, "1")
	require.NoError(t, err)

	// Same graph → same cache key
	hld2, err := BuildHLD(context.Background(), g1, "1")
	require.NoError(t, err)
	assert.Equal(t, hld1.CacheKey(), hld2.CacheKey())

	// Different graph → different cache key
	g2 := createLinearChain(t, 5)
	hld3, err := BuildHLD(context.Background(), g2, "node0")
	require.NoError(t, err)
	assert.NotEqual(t, hld1.CacheKey(), hld3.CacheKey())
}

func TestHLD_Stats(t *testing.T) {
	g := createSimpleTree(t)
	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	stats := hld.Stats()

	assert.Equal(t, 9, stats.NodeCount)
	assert.Greater(t, stats.HeavyPathCount, 0)
	assert.Greater(t, stats.LightEdgeCount, 0)
	assert.Greater(t, stats.MaxPathLength, 0)
	assert.Greater(t, stats.AvgPathLength, float64(0))
	assert.Greater(t, stats.ConstructionTime, time.Duration(0))
}

func TestHLD_AccessorMethods(t *testing.T) {
	g := createSimpleTree(t)
	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	// Test NodeToIdx / IdxToNode round-trip
	t.Run("node mapping round-trip", func(t *testing.T) {
		for i := 0; i < hld.NodeCount(); i++ {
			nodeID, err := hld.IdxToNode(i)
			require.NoError(t, err)

			idx, ok := hld.NodeToIdx(nodeID)
			require.True(t, ok)
			assert.Equal(t, i, idx)
		}
	})

	// Test bound checking
	t.Run("bounds checking", func(t *testing.T) {
		assert.Equal(t, -1, hld.Parent(-1))
		assert.Equal(t, -1, hld.Parent(hld.NodeCount()))
		assert.Equal(t, -1, hld.Depth(-1))
		assert.Equal(t, 0, hld.SubtreeSize(-1))
		assert.Equal(t, -1, hld.HeavyChild(-1))
		assert.Equal(t, -1, hld.Head(-1))
		assert.Equal(t, -1, hld.Pos(-1))
		assert.Equal(t, -1, hld.NodeAtPos(-1))

		_, err := hld.IdxToNode(-1)
		assert.Error(t, err)
		_, err = hld.IdxToNode(hld.NodeCount())
		assert.Error(t, err)
	})
}

func TestHLD_SubtreeSizeTies(t *testing.T) {
	// Create tree where root has two children with equal subtree sizes
	g := NewGraph("/test")

	// Root → childA (subtree size 2: childA, leafA)
	// Root → childB (subtree size 2: childB, leafB)
	nodes := []string{"root", "childA", "childB", "leafA", "leafB"}
	for _, id := range nodes {
		sym := &ast.Symbol{ID: id, Name: id, Kind: ast.SymbolKindFunction}
		g.AddNode(sym)
	}

	g.AddEdge("root", "childA", EdgeTypeCalls, ast.Location{})
	g.AddEdge("root", "childB", EdgeTypeCalls, ast.Location{})
	g.AddEdge("childA", "leafA", EdgeTypeCalls, ast.Location{})
	g.AddEdge("childB", "leafB", EdgeTypeCalls, ast.Location{})
	g.Freeze()

	hld, err := BuildHLD(context.Background(), g, "root")
	require.NoError(t, err)

	rootIdx, _ := hld.NodeToIdx("root")
	childAIdx, _ := hld.NodeToIdx("childA")

	// childA should be selected as heavy child (lexicographic tiebreaker: A < B)
	assert.Equal(t, childAIdx, hld.HeavyChild(rootIdx),
		"childA should be selected as heavy child (lexicographic tiebreaker)")
}

func TestIsTree_ValidTree(t *testing.T) {
	g := createSimpleTree(t)

	err := g.IsTree(context.Background(), "1")
	assert.NoError(t, err)
}

func TestIsTree_CyclicGraph(t *testing.T) {
	g := createCyclicGraph(t)

	err := g.IsTree(context.Background(), "A")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestIsTree_DisconnectedGraph(t *testing.T) {
	g := createDisconnectedGraph(t)

	err := g.IsTree(context.Background(), "A")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disconnected")
}

func TestIsTree_WrongEdgeCount(t *testing.T) {
	g := NewGraph("/test")

	// Create 3 nodes with 3 edges (should be 2 for tree)
	nodes := []string{"A", "B", "C"}
	for _, id := range nodes {
		sym := &ast.Symbol{ID: id, Name: id, Kind: ast.SymbolKindFunction}
		g.AddNode(sym)
	}

	g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{})
	g.AddEdge("A", "C", EdgeTypeCalls, ast.Location{})
	g.AddEdge("B", "C", EdgeTypeCalls, ast.Location{}) // Extra edge
	g.Freeze()

	err := g.IsTree(context.Background(), "A")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exactly V-1 edges")
}

func TestHLD_SerializeMetadata(t *testing.T) {
	g := createSimpleTree(t)
	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	metadata := hld.SerializeMetadata()

	// Check required fields
	assert.NotEmpty(t, metadata["node_count"])
	assert.NotEmpty(t, metadata["root"])
	assert.NotEmpty(t, metadata["light_edge_count"])
	assert.NotEmpty(t, metadata["heavy_path_count"])
	assert.NotEmpty(t, metadata["max_path_length"])
	assert.NotEmpty(t, metadata["construction_time_ms"])

	// Verify values
	assert.Equal(t, "9", metadata["node_count"])
	assert.Equal(t, "1", metadata["root"])
}

// Benchmark tests
func BenchmarkBuildHLD_N100(b *testing.B) {
	g := createBalancedBinaryTree(&testing.T{}, 6) // ~127 nodes
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildHLD(ctx, g, "node0")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildHLD_N1000(b *testing.B) {
	g := createLinearChain(&testing.T{}, 1000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildHLD(ctx, g, "node0")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildHLD_LinearChain_N1000(b *testing.B) {
	g := createLinearChain(&testing.T{}, 1000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildHLD(ctx, g, "node0")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildHLD_BalancedTree_N1000(b *testing.B) {
	g := createBalancedBinaryTree(&testing.T{}, 9) // ~1023 nodes
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildHLD(ctx, g, "node0")
		if err != nil {
			b.Fatal(err)
		}
	}
}
