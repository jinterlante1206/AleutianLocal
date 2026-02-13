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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TEST HELPERS
// ============================================================================

// buildTestSubtreeEngine creates a test engine with known tree structure.
//
// Tree structure:
//
//	 1[val=5, size=9]
//	    /│\
//	   / │ \
//	2[10,5] 3[2,1] 4[8,2]
//	/│               \
//
// 5[3,3] 6[7,1]     7[4,1]
// /│
// 8[1,1] 9[6,1]
//
// DFS order: 1, 2, 5, 8, 9, 6, 3, 4, 7
// Positions: 0, 1, 2, 3, 4, 5, 6, 7, 8
func buildTestSubtreeEngine(t *testing.T, aggFunc AggregateFunc) *SubtreeQueryEngine {
	g := NewGraph("/test")

	// Add nodes with values
	nodes := map[string]int64{
		"1": 5, "2": 10, "3": 2, "4": 8, "5": 3,
		"6": 7, "7": 4, "8": 1, "9": 6,
	}

	for id := range nodes {
		g.AddNode(&ast.Symbol{
			ID:   id,
			Kind: ast.SymbolKindFunction,
			Name: fmt.Sprintf("func%s", id),
		})
	}

	// Build tree edges
	edges := [][2]string{
		{"1", "2"}, {"1", "3"}, {"1", "4"},
		{"2", "5"}, {"2", "6"},
		{"5", "8"}, {"5", "9"},
		{"4", "7"},
	}

	for _, edge := range edges {
		g.AddEdge(edge[0], edge[1], EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()

	// Build HLD
	hld, err := BuildHLD(context.Background(), g, "1")
	require.NoError(t, err)

	// Build value array from HLD positions
	values := make([]int64, hld.NodeCount())
	for nodeID, val := range nodes {
		idx, ok := hld.NodeToIdx(nodeID)
		require.True(t, ok)
		pos := hld.Pos(idx)
		values[pos] = val
	}

	// Build segment tree
	segTree, err := NewSegmentTree(context.Background(), values, aggFunc)
	require.NoError(t, err)

	// Create engine
	engine, err := NewSubtreeQueryEngine(hld, segTree, aggFunc, nil, "")
	require.NoError(t, err)

	return engine
}

// buildTwoTreeForestEngine creates engine for forest with 2 disconnected trees.
//
// Tree 0: A[10,3] -> B[20,2] -> C[30,1]
// Tree 1: X[100,3] -> Y[200,2] -> Z[300,1]
func buildTwoTreeForestEngine(t *testing.T, aggFunc AggregateFunc) *SubtreeQueryEngine {
	g := NewGraph("/test")

	// Tree 0
	g.AddNode(&ast.Symbol{ID: "A", Kind: ast.SymbolKindFunction, Name: "A"})
	g.AddNode(&ast.Symbol{ID: "B", Kind: ast.SymbolKindFunction, Name: "B"})
	g.AddNode(&ast.Symbol{ID: "C", Kind: ast.SymbolKindFunction, Name: "C"})
	g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{})
	g.AddEdge("B", "C", EdgeTypeCalls, ast.Location{})

	// Tree 1
	g.AddNode(&ast.Symbol{ID: "X", Kind: ast.SymbolKindFunction, Name: "X"})
	g.AddNode(&ast.Symbol{ID: "Y", Kind: ast.SymbolKindFunction, Name: "Y"})
	g.AddNode(&ast.Symbol{ID: "Z", Kind: ast.SymbolKindFunction, Name: "Z"})
	g.AddEdge("X", "Y", EdgeTypeCalls, ast.Location{})
	g.AddEdge("Y", "Z", EdgeTypeCalls, ast.Location{})

	g.Freeze()

	// Build forest
	forest, err := BuildHLDForest(context.Background(), g, true)
	require.NoError(t, err)

	// Build value array for entire forest
	allValues := map[string]int64{
		"A": 10, "B": 20, "C": 30,
		"X": 100, "Y": 200, "Z": 300,
	}

	values := make([]int64, forest.TotalNodes())
	position := 0

	for treeIdx := 0; treeIdx < forest.TreeCount(); treeIdx++ {
		hld := forest.GetHLDByIndex(treeIdx)
		for i := 0; i < hld.NodeCount(); i++ {
			nodeIdx := hld.NodeAtPos(i)
			nodeID, err := hld.IdxToNode(nodeIdx)
			require.NoError(t, err)
			values[position] = allValues[nodeID]
			position++
		}
	}

	// Build segment tree over entire forest
	segTree, err := NewSegmentTree(context.Background(), values, aggFunc)
	require.NoError(t, err)

	// Create forest engine
	engine, err := NewSubtreeQueryEngineForest(forest, segTree, aggFunc, nil, "")
	require.NoError(t, err)

	return engine
}

// ============================================================================
// CONSTRUCTOR TESTS
// ============================================================================

func TestNewSubtreeQueryEngine_ValidInputs(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	require.NotNil(t, engine)
	assert.NotNil(t, engine.hld)
	assert.Nil(t, engine.forest)
	assert.NotNil(t, engine.segTree)
	assert.Equal(t, AggregateSUM, engine.aggFunc)
}

func TestNewSubtreeQueryEngine_InvalidInputs(t *testing.T) {
	ctx := context.Background()
	hld, _ := BuildHLD(ctx, buildSimpleGraph(), "main")
	values := make([]int64, hld.NodeCount())
	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)

	t.Run("nil_hld", func(t *testing.T) {
		_, err := NewSubtreeQueryEngine(nil, segTree, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hld must not be nil")
	})

	t.Run("nil_segTree", func(t *testing.T) {
		_, err := NewSubtreeQueryEngine(hld, nil, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "segTree must not be nil")
	})

	t.Run("size_mismatch", func(t *testing.T) {
		wrongValues := make([]int64, hld.NodeCount()+5)
		wrongSegTree, _ := NewSegmentTree(ctx, wrongValues, AggregateSUM)

		_, err := NewSubtreeQueryEngine(hld, wrongSegTree, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "node count")
		assert.Contains(t, err.Error(), "segment tree size")
	})

	t.Run("agg_func_mismatch", func(t *testing.T) {
		_, err := NewSubtreeQueryEngine(hld, segTree, AggregateMAX, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "agg func")
	})
}

func TestNewSubtreeQueryEngineForest_ValidInputs(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	require.NotNil(t, engine)
	assert.Nil(t, engine.hld)
	assert.NotNil(t, engine.forest)
	assert.Equal(t, 2, engine.forest.TreeCount())
}

func TestNewSubtreeQueryEngineForest_InvalidInputs(t *testing.T) {
	ctx := context.Background()
	g := NewGraph("/test")
	g.AddNode(&ast.Symbol{ID: "A", Kind: ast.SymbolKindFunction, Name: "A"})
	g.Freeze()
	forest, _ := BuildHLDForest(ctx, g, true)
	values := make([]int64, forest.TotalNodes())
	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)

	t.Run("nil_forest", func(t *testing.T) {
		_, err := NewSubtreeQueryEngineForest(nil, segTree, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "forest must not be nil")
	})

	t.Run("nil_segTree", func(t *testing.T) {
		_, err := NewSubtreeQueryEngineForest(forest, nil, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "segTree must not be nil")
	})

	t.Run("forest_size_mismatch", func(t *testing.T) {
		wrongValues := make([]int64, forest.TotalNodes()+3)
		wrongSegTree, _ := NewSegmentTree(ctx, wrongValues, AggregateSUM)

		_, err := NewSubtreeQueryEngineForest(forest, wrongSegTree, AggregateSUM, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "forest node count")
		assert.Contains(t, err.Error(), "segment tree size")
	})
}

// ============================================================================
// SUBTREE QUERY TESTS
// ============================================================================

func TestSubtreeQuery_LeafNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Node 8 is a leaf with value 1
	sum, err := engine.SubtreeQuery(ctx, "8")
	require.NoError(t, err)
	assert.Equal(t, int64(1), sum, "leaf subtree should be just the node value")

	// Node 9 is a leaf with value 6
	sum, err = engine.SubtreeQuery(ctx, "9")
	require.NoError(t, err)
	assert.Equal(t, int64(6), sum)
}

func TestSubtreeQuery_Root(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Root subtree is entire tree: 5+10+3+1+6+7+2+8+4 = 46
	sum, err := engine.SubtreeQuery(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, int64(46), sum, "root subtree should be sum of all nodes")
}

func TestSubtreeQuery_InternalNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Node 2 has subtree {2,5,8,9,6}: 10+3+1+6+7 = 27
	sum, err := engine.SubtreeQuery(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, int64(27), sum)

	// Node 5 has subtree {5,8,9}: 3+1+6 = 10
	sum, err = engine.SubtreeQuery(ctx, "5")
	require.NoError(t, err)
	assert.Equal(t, int64(10), sum)

	// Node 4 has subtree {4,7}: 8+4 = 12
	sum, err = engine.SubtreeQuery(ctx, "4")
	require.NoError(t, err)
	assert.Equal(t, int64(12), sum)
}

func TestSubtreeSum_AllAggregationFunctions(t *testing.T) {
	tests := []struct {
		aggFunc  AggregateFunc
		node     string
		expected int64
		name     string
	}{
		{AggregateSUM, "2", 27, "sum_subtree_2"},
		{AggregateMAX, "2", 10, "max_subtree_2"}, // max(10,3,1,6,7) = 10
		{AggregateMIN, "2", 1, "min_subtree_2"},  // min(10,3,1,6,7) = 1
		{AggregateGCD, "5", 1, "gcd_subtree_5"},  // gcd(3,1,6) = 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := buildTestSubtreeEngine(t, tt.aggFunc)
			result, err := engine.SubtreeQuery(context.Background(), tt.node)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubtreeSum_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	sum, err := engine.SubtreeSum(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, int64(27), sum)
}

func TestSubtreeMin_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateMIN)
	ctx := context.Background()

	min, err := engine.SubtreeMin(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), min)
}

func TestSubtreeMax_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateMAX)
	ctx := context.Background()

	max, err := engine.SubtreeMax(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, int64(10), max)
}

func TestSubtreeGCD_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateGCD)
	ctx := context.Background()

	gcd, err := engine.SubtreeGCD(ctx, "5")
	require.NoError(t, err)
	assert.Equal(t, int64(1), gcd)
}

// ============================================================================
// SUBTREE RANGE TESTS
// ============================================================================

func TestSubtreeRange_LeafNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	start, end, err := engine.SubtreeRange(ctx, "8")
	require.NoError(t, err)
	assert.Equal(t, end-start+1, 1, "leaf range should have size 1")
}

func TestSubtreeRange_Boundaries(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Root should have range [0, 8] (9 nodes total)
	start, end, err := engine.SubtreeRange(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 0, start, "root should start at position 0")
	assert.Equal(t, 8, end, "root should end at last position")
	assert.Equal(t, 9, end-start+1, "root range should cover all 9 nodes")
}

func TestSubtreeRange_Contiguous(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Node 2's subtree should be contiguous
	start, end, err := engine.SubtreeRange(ctx, "2")
	require.NoError(t, err)

	// Get subtree size and verify range matches
	nodes, _ := engine.SubtreeNodes(ctx, "2")
	assert.Equal(t, len(nodes), end-start+1, "range size should match subtree node count")
}

// ============================================================================
// SUBTREE NODES TESTS
// ============================================================================

func TestSubtreeNodes_LeafNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	nodes, err := engine.SubtreeNodes(ctx, "8")
	require.NoError(t, err)
	assert.Equal(t, 1, len(nodes))
	assert.Equal(t, "8", nodes[0])
}

func TestSubtreeNodes_Root(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	nodes, err := engine.SubtreeNodes(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 9, len(nodes), "root should contain all 9 nodes")
	assert.Contains(t, nodes, "1")
	assert.Contains(t, nodes, "2")
	assert.Contains(t, nodes, "9")
}

func TestSubtreeNodes_Ordering(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Subtree nodes should be in DFS order (same as position order)
	nodes, err := engine.SubtreeNodes(ctx, "2")
	require.NoError(t, err)

	// First node should be the root of subtree
	assert.Equal(t, "2", nodes[0], "first node should be subtree root")

	// All nodes should be in the subtree
	expected := map[string]bool{"2": true, "5": true, "8": true, "9": true, "6": true}
	for _, node := range nodes {
		assert.True(t, expected[node], "node %s should be in subtree", node)
	}
	assert.Equal(t, len(expected), len(nodes))
}

func TestSubtreeNodes_Coverage(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Node 4's subtree: {4, 7}
	nodes, err := engine.SubtreeNodes(ctx, "4")
	require.NoError(t, err)
	assert.Equal(t, 2, len(nodes))
	assert.Contains(t, nodes, "4")
	assert.Contains(t, nodes, "7")
}

// ============================================================================
// ERROR HANDLING TESTS
// ============================================================================

func TestSubtreeQuery_InvalidInputs(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)

	t.Run("nil_context", func(t *testing.T) {
		_, err := engine.SubtreeQuery(nil, "1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("node_not_exist", func(t *testing.T) {
		_, err := engine.SubtreeQuery(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("context_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := engine.SubtreeQuery(ctx, "1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSubtreeRange_InvalidInputs(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)

	t.Run("nil_context", func(t *testing.T) {
		_, _, err := engine.SubtreeRange(nil, "1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("node_not_exist", func(t *testing.T) {
		_, _, err := engine.SubtreeRange(context.Background(), "fake")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

func TestSubtreeNodes_InvalidInputs(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)

	t.Run("nil_context", func(t *testing.T) {
		_, err := engine.SubtreeNodes(nil, "1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("node_not_exist", func(t *testing.T) {
		_, err := engine.SubtreeNodes(context.Background(), "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})
}

// ============================================================================
// VALIDATION TESTS
// ============================================================================

func TestSubtreeQueryEngine_Validate(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)

	err := engine.Validate()
	assert.NoError(t, err, "valid engine should pass validation")
}

func TestSubtreeQueryEngine_Validate_ModeConsistency(t *testing.T) {
	ctx := context.Background()
	hld, _ := BuildHLD(ctx, buildSimpleGraph(), "main")
	values := make([]int64, hld.NodeCount())
	segTree, _ := NewSegmentTree(ctx, values, AggregateSUM)

	engine := &SubtreeQueryEngine{
		hld:     hld,
		forest:  nil, // Exactly one should be non-nil
		segTree: segTree,
		aggFunc: AggregateSUM,
	}

	err := engine.Validate()
	assert.NoError(t, err)

	// Both nil is invalid
	engine.hld = nil
	err = engine.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

// ============================================================================
// STATS TESTS
// ============================================================================

func TestSubtreeQueryEngine_Stats(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	// Initial stats should be zero
	stats := engine.Stats()
	assert.Equal(t, int64(0), stats.QueryCount)
	assert.Equal(t, time.Duration(0), stats.TotalLatency)

	// Execute some queries
	_, _ = engine.SubtreeQuery(ctx, "1")
	_, _ = engine.SubtreeQuery(ctx, "2")
	_, _ = engine.SubtreeQuery(ctx, "5")

	// Stats should be updated
	stats = engine.Stats()
	assert.Equal(t, int64(3), stats.QueryCount)
	assert.Greater(t, stats.TotalLatency, time.Duration(0))
	assert.Greater(t, stats.AvgLatency, time.Duration(0))
	assert.Greater(t, stats.AvgSubtreeSize, float64(0))
}

// ============================================================================
// CONCURRENT QUERY TESTS (H-PRE-9)
// ============================================================================

func TestSubtreeQuery_ConcurrentQueries(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Run 100 concurrent queries
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := engine.SubtreeSum(ctx, "2")
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check no errors
	for err := range errors {
		assert.NoError(t, err)
	}

	// Stats should show 100 queries (with proper locking)
	stats := engine.Stats()
	assert.Equal(t, int64(100), stats.QueryCount)
}

// ============================================================================
// FOREST MODE TESTS (C-PRE-4)
// ============================================================================

func TestSubtreeQuery_ForestMode_SingleTree(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	ctx := context.Background()

	// Query subtree in first tree: A has children B,C: 10+20+30 = 60
	sum, err := engine.SubtreeSum(ctx, "A")
	require.NoError(t, err)
	assert.Equal(t, int64(60), sum, "subtree A should sum to 60")

	// Query subtree in second tree: X has children Y,Z: 100+200+300 = 600
	sum, err = engine.SubtreeSum(ctx, "X")
	require.NoError(t, err)
	assert.Equal(t, int64(600), sum, "subtree X should sum to 600")
}

func TestSubtreeQuery_ForestMode_LeafNodes(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	ctx := context.Background()

	// Leaf in first tree
	sum, err := engine.SubtreeSum(ctx, "C")
	require.NoError(t, err)
	assert.Equal(t, int64(30), sum)

	// Leaf in second tree
	sum, err = engine.SubtreeSum(ctx, "Z")
	require.NoError(t, err)
	assert.Equal(t, int64(300), sum)
}

func TestSubtreeQuery_ForestMode_PositionOffsets(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	ctx := context.Background()

	// Verify position offsets are applied correctly
	// This tests C-PRE-1 fix

	// B has subtree {B,C}: 20+30 = 50
	sum, err := engine.SubtreeSum(ctx, "B")
	require.NoError(t, err)
	assert.Equal(t, int64(50), sum, "subtree B should sum to 50")

	// Y has subtree {Y,Z}: 200+300 = 500
	sum, err = engine.SubtreeSum(ctx, "Y")
	require.NoError(t, err)
	assert.Equal(t, int64(500), sum, "subtree Y should sum to 500")
}

func TestSubtreeNodes_ForestMode(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	ctx := context.Background()

	// Get nodes in first tree
	nodes, err := engine.SubtreeNodes(ctx, "A")
	require.NoError(t, err)
	assert.Equal(t, 3, len(nodes))
	assert.Contains(t, nodes, "A")
	assert.Contains(t, nodes, "B")
	assert.Contains(t, nodes, "C")

	// Get nodes in second tree
	nodes, err = engine.SubtreeNodes(ctx, "X")
	require.NoError(t, err)
	assert.Equal(t, 3, len(nodes))
	assert.Contains(t, nodes, "X")
	assert.Contains(t, nodes, "Y")
	assert.Contains(t, nodes, "Z")
}

// ============================================================================
// SUBTREE UPDATE TESTS
// ============================================================================

func TestSubtreeUpdate_LeafNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial value of node 8
	initial, _ := engine.SubtreeSum(ctx, "8")
	assert.Equal(t, int64(1), initial)

	// Update leaf
	err = updateEngine.SubtreeUpdate(ctx, "8", 10)
	require.NoError(t, err)

	// Verify update
	updated, _ := engine.SubtreeSum(ctx, "8")
	assert.Equal(t, int64(11), updated)
}

func TestSubtreeUpdate_InternalNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial sum of node 2's subtree: 27
	initial, _ := engine.SubtreeSum(ctx, "2")
	assert.Equal(t, int64(27), initial)

	// Update entire subtree by adding 5 to each node
	// Subtree has 5 nodes, so total should increase by 5*5 = 25
	err = updateEngine.SubtreeUpdate(ctx, "2", 5)
	require.NoError(t, err)

	// Verify update
	updated, _ := engine.SubtreeSum(ctx, "2")
	assert.Equal(t, int64(52), updated, "27 + (5 nodes * 5 delta) = 52")
}

func TestSubtreeUpdate_Root(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial sum of entire tree: 46
	initial, _ := engine.SubtreeSum(ctx, "1")

	// Update entire tree
	err = updateEngine.SubtreeUpdate(ctx, "1", 10)
	require.NoError(t, err)

	// 9 nodes * 10 delta = 90 increase
	updated, _ := engine.SubtreeSum(ctx, "1")
	assert.Equal(t, initial+90, updated)
}

func TestSubtreeUpdate_ZeroDelta(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	initial, _ := engine.SubtreeSum(ctx, "2")

	// Zero delta should not change anything
	err = updateEngine.SubtreeUpdate(ctx, "2", 0)
	require.NoError(t, err)

	updated, _ := engine.SubtreeSum(ctx, "2")
	assert.Equal(t, initial, updated)
}

func TestSubtreeUpdate_NegativeDelta(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	initial, _ := engine.SubtreeSum(ctx, "5")

	// Negative delta should decrease values
	err = updateEngine.SubtreeUpdate(ctx, "5", -2)
	require.NoError(t, err)

	// Subtree has 3 nodes: decrease by 3*2 = 6
	updated, _ := engine.SubtreeSum(ctx, "5")
	assert.Equal(t, initial-6, updated)
}

func TestSubtreeUpdate_ThenQueryConsistency(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Multiple updates and queries
	node := "4"
	initial, _ := engine.SubtreeSum(ctx, node)

	// First update: +10
	_ = updateEngine.SubtreeUpdate(ctx, node, 10)
	after1, _ := engine.SubtreeSum(ctx, node)
	assert.Equal(t, initial+20, after1, "subtree has 2 nodes: 2*10=20")

	// Second update: +5
	_ = updateEngine.SubtreeUpdate(ctx, node, 5)
	after2, _ := engine.SubtreeSum(ctx, node)
	assert.Equal(t, after1+10, after2, "2*5=10 more")

	// Total change: 2*(10+5) = 30
	assert.Equal(t, initial+30, after2)
}

func TestSubtreeIncrement_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	initial, _ := engine.SubtreeSum(ctx, "6")
	err = updateEngine.SubtreeIncrement(ctx, "6")
	require.NoError(t, err)

	updated, _ := engine.SubtreeSum(ctx, "6")
	assert.Equal(t, initial+1, updated)
}

func TestSubtreeDecrement_ConvenienceWrapper(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	initial, _ := engine.SubtreeSum(ctx, "6")
	err = updateEngine.SubtreeDecrement(ctx, "6")
	require.NoError(t, err)

	updated, _ := engine.SubtreeSum(ctx, "6")
	assert.Equal(t, initial-1, updated)
}

func TestSubtreeUpdate_ContextCancellation(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = updateEngine.SubtreeUpdate(ctx, "1", 10)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSubtreeUpdate_ForestMode(t *testing.T) {
	engine := buildTwoTreeForestEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Update subtree in first tree
	initial, _ := engine.SubtreeSum(ctx, "B")
	err = updateEngine.SubtreeUpdate(ctx, "B", 5)
	require.NoError(t, err)

	// Subtree {B,C} has 2 nodes: 2*5 = 10 increase
	updated, _ := engine.SubtreeSum(ctx, "B")
	assert.Equal(t, initial+10, updated)

	// Verify other tree unaffected
	sumX, _ := engine.SubtreeSum(ctx, "X")
	assert.Equal(t, int64(600), sumX, "tree X should be unchanged")
}

// ============================================================================
// SUBTREE SET TESTS
// ============================================================================

func TestSubtreeSet_LeafNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Set leaf node to specific value
	err = updateEngine.SubtreeSet(ctx, "8", 100)
	require.NoError(t, err)

	// Verify
	value, _ := engine.SubtreeSum(ctx, "8")
	assert.Equal(t, int64(100), value)
}

func TestSubtreeSet_InternalNode(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Set all nodes in subtree to 50
	err = updateEngine.SubtreeSet(ctx, "4", 50)
	require.NoError(t, err)

	// Subtree {4,7} has 2 nodes, each now 50: sum = 100
	sum, _ := engine.SubtreeSum(ctx, "4")
	assert.Equal(t, int64(100), sum)
}

// ============================================================================
// UPDATE ENGINE CONSTRUCTOR TESTS
// ============================================================================

func TestNewSubtreeUpdateEngine_ValidInputs(t *testing.T) {
	queryEngine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(queryEngine)
	require.NoError(t, err)
	assert.NotNil(t, updateEngine)
	assert.NotNil(t, updateEngine.SubtreeQueryEngine)
}

func TestNewSubtreeUpdateEngine_NilQueryEngine(t *testing.T) {
	_, err := NewSubtreeUpdateEngine(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query engine must not be nil")
}

// ============================================================================
// UPDATE ENGINE STATS TESTS
// ============================================================================

func TestSubtreeUpdateEngine_Stats(t *testing.T) {
	engine := buildTestSubtreeEngine(t, AggregateSUM)
	updateEngine, err := NewSubtreeUpdateEngine(engine)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial stats
	stats := updateEngine.UpdateStats()
	assert.Equal(t, int64(0), stats.UpdateCount)
	assert.Equal(t, int64(0), stats.NodesUpdated)

	// Perform updates
	_ = updateEngine.SubtreeUpdate(ctx, "2", 5) // Updates 5 nodes
	_ = updateEngine.SubtreeUpdate(ctx, "8", 3) // Updates 1 node

	// Check stats
	stats = updateEngine.UpdateStats()
	assert.Equal(t, int64(2), stats.UpdateCount)
	assert.Equal(t, int64(6), stats.NodesUpdated, "5+1=6 nodes total")
	assert.Greater(t, stats.TotalUpdateLatency, time.Duration(0))
	assert.Greater(t, stats.AvgUpdateLatency, time.Duration(0))
}

// ============================================================================
// PERFORMANCE REGRESSION TESTS (M-PRE-11)
// ============================================================================

func TestSubtreeQuery_PerformanceRegression(t *testing.T) {
	// Build large tree (1000 nodes)
	g := createBalancedBinaryTree(t, 10) // Depth 10 ≈ 1024 nodes
	hld, _ := BuildHLD(context.Background(), g, "node0")

	values := make([]int64, hld.NodeCount())
	for i := range values {
		values[i] = int64(i)
	}

	segTree, _ := NewSegmentTree(context.Background(), values, AggregateSUM)
	engine, _ := NewSubtreeQueryEngine(hld, segTree, AggregateSUM, nil, "")

	// Query should complete in < 1ms (O(log V) guarantee)
	start := time.Now()
	_, err := engine.SubtreeSum(context.Background(), "node0")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, duration, time.Millisecond, "large tree query should be < 1ms")
}

// ============================================================================
// BENCHMARKS
// ============================================================================

func BenchmarkSubtreeQuery_LeafNode(b *testing.B) {
	engine := buildTestSubtreeEngine(&testing.T{}, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.SubtreeQuery(ctx, "8")
	}
}

func BenchmarkSubtreeQuery_SmallSubtree(b *testing.B) {
	engine := buildTestSubtreeEngine(&testing.T{}, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.SubtreeQuery(ctx, "5") // 3 nodes
	}
}

func BenchmarkSubtreeQuery_LargeSubtree(b *testing.B) {
	engine := buildTestSubtreeEngine(&testing.T{}, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.SubtreeQuery(ctx, "2") // 5 nodes
	}
}

func BenchmarkSubtreeQuery_Root(b *testing.B) {
	engine := buildTestSubtreeEngine(&testing.T{}, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.SubtreeQuery(ctx, "1") // All 9 nodes
	}
}

func BenchmarkSubtreeUpdate_N1000(b *testing.B) {
	g := createBalancedBinaryTree(&testing.T{}, 10)
	hld, _ := BuildHLD(context.Background(), g, "node0")
	values := make([]int64, hld.NodeCount())
	segTree, _ := NewSegmentTree(context.Background(), values, AggregateSUM)
	engine, _ := NewSubtreeQueryEngine(hld, segTree, AggregateSUM, nil, "")
	updateEngine, _ := NewSubtreeUpdateEngine(engine)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = updateEngine.SubtreeUpdate(ctx, "node0", 1)
	}
}

func BenchmarkSubtreeNodes_N1000(b *testing.B) {
	g := createBalancedBinaryTree(&testing.T{}, 10)
	hld, _ := BuildHLD(context.Background(), g, "node0")
	values := make([]int64, hld.NodeCount())
	segTree, _ := NewSegmentTree(context.Background(), values, AggregateSUM)
	engine, _ := NewSubtreeQueryEngine(hld, segTree, AggregateSUM, nil, "")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.SubtreeNodes(ctx, "node0")
	}
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func buildSimpleGraph() *Graph {
	g := NewGraph("/test")
	g.AddNode(&ast.Symbol{ID: "main", Kind: ast.SymbolKindFunction, Name: "main"})
	g.AddNode(&ast.Symbol{ID: "foo", Kind: ast.SymbolKindFunction, Name: "foo"})
	g.AddNode(&ast.Symbol{ID: "bar", Kind: ast.SymbolKindFunction, Name: "bar"})
	g.AddEdge("main", "foo", EdgeTypeCalls, ast.Location{})
	g.AddEdge("main", "bar", EdgeTypeCalls, ast.Location{})
	g.Freeze()
	return g
}
