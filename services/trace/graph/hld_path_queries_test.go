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
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test fixtures

// buildTestTree creates a simple tree for testing:
//
//	      1 [val=5]
//	     /|\
//	    / | \
//	   2  3  4
//	  [10][2][8]
//	  /|     \
//	 5 6      7
//	[3][7]   [4]
//	/|
//
// 8 9
// [1][6]
func buildTestTree() (*Graph, map[string]int64) {
	g := NewGraph("/test/project")

	// Add nodes with values
	values := map[string]int64{
		"1": 5,
		"2": 10,
		"3": 2,
		"4": 8,
		"5": 3,
		"6": 7,
		"7": 4,
		"8": 1,
		"9": 6,
	}

	for id := range values {
		sym := &ast.Symbol{
			ID:       id,
			Name:     "node_" + id,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		if err != nil {
			panic(err) // This is a test helper, panic is OK
		}
	}

	// Add edges (parent -> child)
	edges := []struct{ from, to string }{
		{"1", "2"},
		{"1", "3"},
		{"1", "4"},
		{"2", "5"},
		{"2", "6"},
		{"4", "7"},
		{"5", "8"},
		{"5", "9"},
	}

	for _, e := range edges {
		err := g.AddEdge(e.from, e.to, EdgeTypeCalls, ast.Location{})
		if err != nil {
			panic(err) // This is a test helper, panic is OK
		}
	}

	return g, values
}

// buildPathQueryEngineForTest creates PathQueryEngine with test tree.
func buildPathQueryEngineForTest(aggFunc AggregateFunc) (*PathQueryEngine, map[string]int64, error) {
	ctx := context.Background()
	g, values := buildTestTree()
	g.Freeze()

	// Build HLD
	hld, err := BuildHLDIterative(ctx, g, "1")
	if err != nil {
		return nil, nil, err
	}

	// Build value array in HLD position order
	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	// Build segment tree
	segTree, err := NewSegmentTree(ctx, valueArr, aggFunc)
	if err != nil {
		return nil, nil, err
	}

	// Create path query engine
	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, aggFunc, &opts)
	if err != nil {
		return nil, nil, err
	}

	return engine, values, nil
}

// Test logger for tests
var testLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelError, // Only show errors in tests
}))

// ==============================================================================
// Construction Tests
// ==============================================================================

func TestNewPathQueryEngine_ValidInputs(t *testing.T) {
	ctx := context.Background()
	g, _ := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	values := make([]int64, hld.NodeCount())
	for i := range values {
		values[i] = int64(i + 1)
	}

	segTree, err := NewSegmentTree(ctx, values, AggregateSUM)
	require.NoError(t, err)

	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)

	require.NoError(t, err)
	assert.NotNil(t, engine)
	assert.Equal(t, AggregateSUM, engine.aggFunc)
}

func TestNewPathQueryEngine_InvalidInputs(t *testing.T) {
	ctx := context.Background()
	g, _ := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	values := make([]int64, hld.NodeCount())
	segTree, err := NewSegmentTree(ctx, values, AggregateSUM)
	require.NoError(t, err)

	opts := DefaultPathQueryEngineOptions()

	t.Run("nil hld", func(t *testing.T) {
		_, err := NewPathQueryEngine(nil, segTree, AggregateSUM, &opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hld must not be nil")
	})

	t.Run("nil segTree", func(t *testing.T) {
		_, err := NewPathQueryEngine(hld, nil, AggregateSUM, &opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "segTree must not be nil")
	})

	t.Run("invalid aggFunc", func(t *testing.T) {
		_, err := NewPathQueryEngine(hld, segTree, AggregateFunc(99), &opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "aggFunc must be valid")
	})

	t.Run("size mismatch", func(t *testing.T) {
		// Create segment tree with wrong size
		wrongValues := make([]int64, hld.NodeCount()+5)
		wrongSegTree, err := NewSegmentTree(ctx, wrongValues, AggregateSUM)
		require.NoError(t, err)

		_, err = NewPathQueryEngine(hld, wrongSegTree, AggregateSUM, &opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "node count")
		assert.Contains(t, err.Error(), "segment tree size")
	})

	t.Run("agg func mismatch", func(t *testing.T) {
		// Segment tree with MIN, engine with SUM
		minSegTree, err := NewSegmentTree(ctx, values, AggregateMIN)
		require.NoError(t, err)

		_, err = NewPathQueryEngine(hld, minSegTree, AggregateSUM, &opts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "agg func")
		assert.Contains(t, err.Error(), "SUM")
		assert.Contains(t, err.Error(), "MIN")
	})
}

// ==============================================================================
// PathQuery Tests
// ==============================================================================

func TestPathQuery_SameNode(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()
	sum, err := engine.PathQuery(ctx, "5", "5", testLogger)
	require.NoError(t, err)

	expected := values["5"]
	assert.Equal(t, expected, sum)
}

func TestPathQuery_ParentChild(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// Path from parent to child
	sum, err := engine.PathQuery(ctx, "1", "2", testLogger)
	require.NoError(t, err)

	expected := values["1"] + values["2"]
	assert.Equal(t, expected, sum)

	// Path from child to parent (should be same)
	sum2, err := engine.PathQuery(ctx, "2", "1", testLogger)
	require.NoError(t, err)
	assert.Equal(t, expected, sum2)
}

func TestPathQuery_MultipleHeavyPaths(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// Path from leaf to leaf across multiple heavy paths
	// Path: 8 -> 5 -> 2 -> 1 -> 4 -> 7
	sum, err := engine.PathQuery(ctx, "8", "7", testLogger)
	require.NoError(t, err)

	expected := values["8"] + values["5"] + values["2"] + values["1"] + values["4"] + values["7"]
	assert.Equal(t, expected, sum)
}

func TestPathQuery_LongPath(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// Longest path in tree: 9 -> 5 -> 2 -> 1
	sum, err := engine.PathQuery(ctx, "9", "1", testLogger)
	require.NoError(t, err)

	expected := values["9"] + values["5"] + values["2"] + values["1"]
	assert.Equal(t, expected, sum)
}

// ==============================================================================
// SUM Tests
// ==============================================================================

func TestPathSum_BasicPath(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	sum, err := engine.PathSum(ctx, "6", "3", testLogger)
	require.NoError(t, err)

	// Path: 6 -> 2 -> 1 -> 3
	expected := values["6"] + values["2"] + values["1"] + values["3"]
	assert.Equal(t, expected, sum)
}

func TestPathSum_LCANotDoubleCounted(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// Path from one leaf to another: 8 -> 5 -> 2 -> 6
	// LCA is node 2, should be counted only once
	sum, err := engine.PathSum(ctx, "8", "6", testLogger)
	require.NoError(t, err)

	expected := values["8"] + values["5"] + values["2"] + values["6"]
	assert.Equal(t, expected, sum, "LCA should be counted exactly once")

	// Verify each node appears exactly once by computing manually
	nodeCount := make(map[string]int)
	path := []string{"8", "5", "2", "6"}
	for _, node := range path {
		nodeCount[node]++
	}

	for node, count := range nodeCount {
		assert.Equal(t, 1, count, "node %s should appear exactly once in path", node)
	}
}

func TestPathSum_WrongAggFunc(t *testing.T) {
	// Create engine with MIN
	engine, _, err := buildPathQueryEngineForTest(AggregateMIN)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to use PathSum (requires SUM)
	_, err = engine.PathSum(ctx, "1", "2", testLogger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AggregateSUM")
	assert.Contains(t, err.Error(), "MIN")
}

// ==============================================================================
// MIN/MAX Tests
// ==============================================================================

func TestPathMin_BasicPath(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateMIN)
	require.NoError(t, err)

	ctx := context.Background()

	// Path: 8 -> 5 -> 2 -> 1
	min, err := engine.PathMin(ctx, "8", "1", testLogger)
	require.NoError(t, err)

	// Min of values: 1, 3, 10, 5 = 1
	expectedMin := values["8"] // 1 is the minimum
	for _, node := range []string{"5", "2", "1"} {
		if values[node] < expectedMin {
			expectedMin = values[node]
		}
	}
	assert.Equal(t, expectedMin, min)
	assert.Equal(t, int64(1), min) // Node 8 has value 1
}

func TestPathMax_BasicPath(t *testing.T) {
	engine, values, err := buildPathQueryEngineForTest(AggregateMAX)
	require.NoError(t, err)

	ctx := context.Background()

	// Path: 9 -> 5 -> 2 -> 1
	max, err := engine.PathMax(ctx, "9", "1", testLogger)
	require.NoError(t, err)

	// Max of values: 6, 3, 10, 5 = 10
	expectedMax := values["9"]
	for _, node := range []string{"5", "2", "1"} {
		if values[node] > expectedMax {
			expectedMax = values[node]
		}
	}
	assert.Equal(t, expectedMax, max)
	assert.Equal(t, int64(10), max) // Node 2 has value 10
}

func TestPathMin_IdempotentLCA(t *testing.T) {
	// For MIN/MAX, LCA double-counting doesn't matter (idempotent)
	engine, _, err := buildPathQueryEngineForTest(AggregateMIN)
	require.NoError(t, err)

	ctx := context.Background()

	// Path: 8 -> 5 -> 2 -> 6
	// LCA is 2, min(2) = min(2, 2) = 2 (idempotent)
	min, err := engine.PathMin(ctx, "8", "6", testLogger)
	require.NoError(t, err)

	// Min of path: min(1, 3, 10, 7) = 1
	assert.Equal(t, int64(1), min)
}

// ==============================================================================
// GCD Tests
// ==============================================================================

func TestPathGCD_BasicPath(t *testing.T) {
	// Create a tree with values that have common factors
	ctx := context.Background()
	g := NewGraph("/test/project")

	values := map[string]int64{
		"1": 12, // 12 = 2^2 * 3
		"2": 18, // 18 = 2 * 3^2
		"3": 24, // 24 = 2^3 * 3
	}

	for id := range values {
		sym := &ast.Symbol{
			ID:       id,
			Name:     "node_" + id,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		require.NoError(t, err)
	}

	err := g.AddEdge("1", "2", EdgeTypeCalls, ast.Location{})
	require.NoError(t, err)
	err = g.AddEdge("2", "3", EdgeTypeCalls, ast.Location{})
	require.NoError(t, err)
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateGCD)
	require.NoError(t, err)

	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, AggregateGCD, &opts)
	require.NoError(t, err)

	// GCD of path 1 -> 2 -> 3: gcd(12, 18, 24) = 6
	gcd, err := engine.PathGCD(ctx, "1", "3", testLogger)
	require.NoError(t, err)
	assert.Equal(t, int64(6), gcd)
}

// ==============================================================================
// Error Cases
// ==============================================================================

func TestPathQuery_InvalidInputs(t *testing.T) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	t.Run("nil context", func(t *testing.T) {
		_, err := engine.PathQuery(nil, "1", "2", testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("empty node u", func(t *testing.T) {
		_, err := engine.PathQuery(context.Background(), "", "2", testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "u must not be empty")
	})

	t.Run("empty node v", func(t *testing.T) {
		_, err := engine.PathQuery(context.Background(), "1", "", testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "v must not be empty")
	})

	t.Run("nil logger", func(t *testing.T) {
		_, err := engine.PathQuery(context.Background(), "1", "2", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger must not be nil")
	})

	t.Run("node not exist", func(t *testing.T) {
		_, err := engine.PathQuery(context.Background(), "1", "999", testLogger)
		assert.Error(t, err)
		// Should fail in LCA computation
	})

	t.Run("context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := engine.PathQuery(ctx, "1", "2", testLogger)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond) // Ensure timeout

		_, err := engine.PathQuery(ctx, "1", "2", testLogger)
		assert.Error(t, err)
		// Should hit timeout
	})
}

// ==============================================================================
// Validation Tests
// ==============================================================================

func TestPathQueryEngine_Validate(t *testing.T) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	err = engine.Validate()
	assert.NoError(t, err)
}

// ==============================================================================
// Stats Tests
// ==============================================================================

func TestPathQueryEngine_Stats(t *testing.T) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial stats should be zero
	stats := engine.Stats()
	assert.Equal(t, int64(0), stats.QueryCount)
	assert.Equal(t, time.Duration(0), stats.TotalLatency)
	assert.Equal(t, time.Duration(0), stats.AvgLatency)
	assert.Equal(t, float64(0), stats.CacheHitRatio)

	// Run some queries
	_, err = engine.PathQuery(ctx, "1", "2", testLogger)
	require.NoError(t, err)

	_, err = engine.PathQuery(ctx, "3", "4", testLogger)
	require.NoError(t, err)

	// Stats should be updated
	stats = engine.Stats()
	assert.Equal(t, int64(2), stats.QueryCount)
	assert.Greater(t, stats.TotalLatency, time.Duration(0))
	assert.Greater(t, stats.AvgLatency, time.Duration(0))
	assert.Greater(t, stats.LastQueryTime, int64(0))
}

// ==============================================================================
// Caching Tests
// ==============================================================================

func TestPathQuery_LCACaching(t *testing.T) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(t, err)

	ctx := context.Background()

	// First query - cache miss
	_, err = engine.PathQuery(ctx, "8", "9", testLogger)
	require.NoError(t, err)

	// Get stats
	stats := engine.Stats()

	// Second query with same nodes - should hit cache (if LCA cache enabled)
	_, err = engine.PathQuery(ctx, "8", "9", testLogger)
	require.NoError(t, err)

	stats2 := engine.Stats()
	// Cache hits should have increased (if caching enabled)
	if engine.lcaCache != nil {
		assert.Greater(t, stats2.CacheHitRatio, stats.CacheHitRatio)
	}
}

func TestPathQuery_QueryCaching(t *testing.T) {
	// Create engine with query caching enabled
	opts := DefaultPathQueryEngineOptions()
	opts.EnableQueryCache = true
	opts.QueryCacheSize = 100

	ctx := context.Background()
	g, values := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(t, err)

	engine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(t, err)

	// First query - cache miss
	result1, err := engine.PathQuery(ctx, "8", "9", testLogger)
	require.NoError(t, err)

	// Second identical query - should hit cache
	result2, err := engine.PathQuery(ctx, "8", "9", testLogger)
	require.NoError(t, err)

	assert.Equal(t, result1, result2)

	// Verify cache hit ratio increased
	stats := engine.Stats()
	assert.Greater(t, stats.CacheHitRatio, float64(0))
}

// ==============================================================================
// Benchmarks
// ==============================================================================

func BenchmarkPathQuery_SameHeavyPath(b *testing.B) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(b, err)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.PathQuery(ctx, "8", "9", testLogger)
	}
}

func BenchmarkPathQuery_DifferentPaths(b *testing.B) {
	engine, _, err := buildPathQueryEngineForTest(AggregateSUM)
	require.NoError(b, err)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.PathQuery(ctx, "8", "7", testLogger)
	}
}

func BenchmarkPathSum_N1000(b *testing.B) {
	// Build a deeper tree for more realistic benchmarking
	ctx := context.Background()
	g := NewGraph("/test/project")

	// Create a chain of 1000 nodes
	values := make(map[string]int64, 1000)
	for i := 0; i < 1000; i++ {
		id := string(rune('A' + i%26))
		if i >= 26 {
			id = id + string(rune('0'+i/26))
		}
		sym := &ast.Symbol{
			ID:       id,
			Name:     "node_" + id,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		if err != nil {
			b.Fatal(err)
		}
		values[id] = int64(i + 1)

		if i > 0 {
			prevID := string(rune('A' + (i-1)%26))
			if i-1 >= 26 {
				prevID = prevID + string(rune('0'+(i-1)/26))
			}
			err = g.AddEdge(prevID, id, EdgeTypeCalls, ast.Location{})
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "A")
	require.NoError(b, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(b, err)

	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(b, err)

	// Query from root to a deep leaf
	leafID := string(rune('A' + 999%26))
	if 999 >= 26 {
		leafID = leafID + string(rune('0'+999/26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.PathSum(ctx, "A", leafID, testLogger)
	}
}

func BenchmarkPathMin_N1000(b *testing.B) {
	ctx := context.Background()
	g := NewGraph("/test/project")

	values := make(map[string]int64, 1000)
	for i := 0; i < 1000; i++ {
		id := string(rune('A' + i%26))
		if i >= 26 {
			id = id + string(rune('0'+i/26))
		}
		sym := &ast.Symbol{
			ID:       id,
			Name:     "node_" + id,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		if err != nil {
			b.Fatal(err)
		}
		values[id] = int64(i + 1)

		if i > 0 {
			prevID := string(rune('A' + (i-1)%26))
			if i-1 >= 26 {
				prevID = prevID + string(rune('0'+(i-1)/26))
			}
			err = g.AddEdge(prevID, id, EdgeTypeCalls, ast.Location{})
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "A")
	require.NoError(b, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateMIN)
	require.NoError(b, err)

	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, AggregateMIN, &opts)
	require.NoError(b, err)

	leafID := string(rune('A' + 999%26))
	if 999 >= 26 {
		leafID = leafID + string(rune('0'+999/26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.PathMin(ctx, "A", leafID, testLogger)
	}
}

func BenchmarkPathMax_N1000(b *testing.B) {
	ctx := context.Background()
	g := NewGraph("/test/project")

	values := make(map[string]int64, 1000)
	for i := 0; i < 1000; i++ {
		id := string(rune('A' + i%26))
		if i >= 26 {
			id = id + string(rune('0'+i/26))
		}
		sym := &ast.Symbol{
			ID:       id,
			Name:     "node_" + id,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		if err != nil {
			b.Fatal(err)
		}
		values[id] = int64(i + 1)

		if i > 0 {
			prevID := string(rune('A' + (i-1)%26))
			if i-1 >= 26 {
				prevID = prevID + string(rune('0'+(i-1)/26))
			}
			err = g.AddEdge(prevID, id, EdgeTypeCalls, ast.Location{})
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "A")
	require.NoError(b, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = values[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateMAX)
	require.NoError(b, err)

	opts := DefaultPathQueryEngineOptions()
	engine, err := NewPathQueryEngine(hld, segTree, AggregateMAX, &opts)
	require.NoError(b, err)

	leafID := string(rune('A' + 999%26))
	if 999 >= 26 {
		leafID = leafID + string(rune('0'+999/26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.PathMax(ctx, "A", leafID, testLogger)
	}
}
