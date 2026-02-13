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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==============================================================================
// Test Fixtures
// ==============================================================================

// buildTestUpdateEngine creates a PathUpdateEngine with test tree.
//
// Returns:
//   - *PathUpdateEngine: Ready for update operations
//   - *Graph: Underlying graph (frozen)
//   - map[string]int64: Initial node values
func buildTestUpdateEngine(t *testing.T) (*PathUpdateEngine, *Graph, map[string]int64) {
	ctx := context.Background()
	g, initialValues := buildTestTree()
	g.Freeze()

	// Build HLD
	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	// Build value array in HLD position order
	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = initialValues[nodeID]
	}

	// Build segment tree with SUM aggregation (required for updates)
	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(t, err)

	// Create path query engine
	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(t, err)

	// Create update engine
	updateEngine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(t, err)

	return updateEngine, g, initialValues
}

// verifyNodeValue checks that a node has the expected value after updates.
func verifyNodeValue(t *testing.T, engine *PathUpdateEngine, nodeID string, expected int64) {
	ctx := context.Background()
	result, err := engine.PathSum(ctx, nodeID, nodeID, testLogger)
	require.NoError(t, err)
	assert.Equal(t, expected, result, "Node %s should have value %d", nodeID, expected)
}

// ==============================================================================
// Construction Tests
// ==============================================================================

func TestNewPathUpdateEngine_ValidInputs(t *testing.T) {
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
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(t, err)

	updateEngine, err := NewPathUpdateEngine(queryEngine)

	require.NoError(t, err)
	assert.NotNil(t, updateEngine)
	assert.NotNil(t, updateEngine.PathQueryEngine)
}

func TestNewPathUpdateEngine_InvalidInputs(t *testing.T) {
	ctx := context.Background()
	g, _ := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	values := make([]int64, hld.NodeCount())
	opts := DefaultPathQueryEngineOptions()

	t.Run("nil query engine", func(t *testing.T) {
		_, err := NewPathUpdateEngine(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query engine must not be nil")
	})

	t.Run("wrong aggregation", func(t *testing.T) {
		// Create query engine with MIN (updates require SUM)
		minSegTree, err := NewSegmentTree(ctx, values, AggregateMIN)
		require.NoError(t, err)

		minQueryEngine, err := NewPathQueryEngine(hld, minSegTree, AggregateMIN, &opts)
		require.NoError(t, err)

		_, err = NewPathUpdateEngine(minQueryEngine)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SUM aggregation")
		assert.Contains(t, err.Error(), "MIN")
	})

	t.Run("MAX aggregation rejected", func(t *testing.T) {
		maxSegTree, err := NewSegmentTree(ctx, values, AggregateMAX)
		require.NoError(t, err)

		maxQueryEngine, err := NewPathQueryEngine(hld, maxSegTree, AggregateMAX, &opts)
		require.NoError(t, err)

		_, err = NewPathUpdateEngine(maxQueryEngine)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SUM aggregation")
	})

	t.Run("GCD aggregation rejected", func(t *testing.T) {
		gcdSegTree, err := NewSegmentTree(ctx, values, AggregateGCD)
		require.NoError(t, err)

		gcdQueryEngine, err := NewPathQueryEngine(hld, gcdSegTree, AggregateGCD, &opts)
		require.NoError(t, err)

		_, err = NewPathUpdateEngine(gcdQueryEngine)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SUM aggregation")
	})
}

// ==============================================================================
// PathUpdate Tests
// ==============================================================================

func TestPathUpdate_SameNode(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	nodeID := "5"
	delta := int64(10)
	initialValue := initialValues[nodeID]

	err := engine.PathUpdate(ctx, nodeID, nodeID, delta, testLogger)
	require.NoError(t, err)

	// Verify value updated
	verifyNodeValue(t, engine, nodeID, initialValue+delta)
}

func TestPathUpdate_ParentChild(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Update path from parent to child
	parent, child := "1", "2"
	delta := int64(5)

	initialParent := initialValues[parent]
	initialChild := initialValues[child]

	err := engine.PathUpdate(ctx, parent, child, delta, testLogger)
	require.NoError(t, err)

	// Both nodes should be incremented
	verifyNodeValue(t, engine, parent, initialParent+delta)
	verifyNodeValue(t, engine, child, initialChild+delta)

	// Verify sum on path
	sum, err := engine.PathSum(ctx, parent, child, testLogger)
	require.NoError(t, err)
	expectedSum := (initialParent + delta) + (initialChild + delta)
	assert.Equal(t, expectedSum, sum)
}

func TestPathUpdate_MultipleHeavyPaths(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Path from leaf to leaf across multiple heavy paths
	// Path: 8 -> 5 -> 2 -> 1 -> 4 -> 7
	u, v := "8", "7"
	delta := int64(3)

	// Initial sum
	initialSum := initialValues["8"] + initialValues["5"] + initialValues["2"] +
		initialValues["1"] + initialValues["4"] + initialValues["7"]

	err := engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Query sum after update
	sum, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)

	// Each node incremented by delta
	pathLength := int64(6) // 6 nodes on path
	expectedSum := initialSum + delta*pathLength
	assert.Equal(t, expectedSum, sum)
}

func TestPathUpdate_LongPath(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Longest path in tree: 9 -> 5 -> 2 -> 1
	u, v := "9", "1"
	delta := int64(7)

	// Initial sum
	initialSum := initialValues["9"] + initialValues["5"] + initialValues["2"] + initialValues["1"]

	err := engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Query sum after update
	sum, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)

	// 4 nodes on path
	pathLength := int64(4)
	expectedSum := initialSum + delta*pathLength
	assert.Equal(t, expectedSum, sum)
}

func TestPathUpdate_ZeroDelta(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "1", "2"
	delta := int64(0)

	// Update with zero delta (no-op)
	err := engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Verify values unchanged
	verifyNodeValue(t, engine, u, initialValues[u])
	verifyNodeValue(t, engine, v, initialValues[v])

	// Verify stats tracked even for no-op
	stats := engine.Stats()
	assert.Equal(t, int64(1), stats.UpdateCount)
}

func TestPathUpdate_NegativeDelta(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Use a simple single-node path to verify negative delta works
	u, v := "5", "5"
	delta := int64(-2)

	initialValue := initialValues[u]

	err := engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Node should be decremented
	verifyNodeValue(t, engine, u, initialValue+delta)
}

// ==============================================================================
// LCA Handling Tests (CRITICAL)
// ==============================================================================

func TestPathUpdate_LCANotDoubleUpdated(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Find two nodes with common ancestor
	// Path: 8 -> 5 -> 2 -> 6
	// LCA is node 2
	u, v := "8", "6"
	lca, err := engine.PathQueryEngine.hld.LCA(ctx, u, v)
	require.NoError(t, err)
	assert.Equal(t, "2", lca, "LCA of 8 and 6 should be 2")

	delta := int64(5)
	initialLCA := initialValues[lca]

	err = engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Query LCA value - should be updated EXACTLY ONCE
	lcaValue, err := engine.PathSum(ctx, lca, lca, testLogger)
	require.NoError(t, err)

	assert.Equal(t, initialLCA+delta, lcaValue,
		"LCA %s should be updated exactly once, not twice", lca)

	// Verify full path sum
	// Nodes on path: 8, 5, 2, 6 (4 nodes)
	initialPathSum := initialValues["8"] + initialValues["5"] + initialValues["2"] + initialValues["6"]
	expectedPathSum := initialPathSum + delta*4

	sum, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)
	assert.Equal(t, expectedPathSum, sum, "Path sum should reflect 4 nodes updated once each")
}

func TestPathUpdate_SymmetricPaths(t *testing.T) {
	engine1, _, initialValues := buildTestUpdateEngine(t)
	engine2, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "8", "7"
	delta := int64(4)

	// Update u->v
	err := engine1.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Update v->u (reversed)
	err = engine2.PathUpdate(ctx, v, u, delta, testLogger)
	require.NoError(t, err)

	// Both should produce same result
	sum1, err := engine1.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)

	sum2, err := engine2.PathSum(ctx, v, u, testLogger)
	require.NoError(t, err)

	assert.Equal(t, sum1, sum2, "PathUpdate(u,v) and PathUpdate(v,u) should produce identical results")

	// Verify expected value
	pathNodes := []string{"8", "5", "2", "1", "4", "7"}
	expectedSum := int64(0)
	for _, node := range pathNodes {
		expectedSum += initialValues[node] + delta
	}
	assert.Equal(t, expectedSum, sum1)
}

// ==============================================================================
// Consistency Tests
// ==============================================================================

func TestPathUpdate_ThenQueryConsistency(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "1", "9"
	delta := int64(10)

	// Initial query
	initialSum, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)

	// Update
	err = engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Query again
	newSum, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)

	// Calculate expected increase
	// Path: 1 -> 2 -> 5 -> 9 (4 nodes)
	pathNodes := []string{"1", "2", "5", "9"}
	pathLength := int64(len(pathNodes))

	// Verify initial sum
	expectedInitialSum := int64(0)
	for _, node := range pathNodes {
		expectedInitialSum += initialValues[node]
	}
	assert.Equal(t, expectedInitialSum, initialSum, "Initial sum should match sum of node values")

	// Verify new sum
	expectedNewSum := initialSum + delta*pathLength
	assert.Equal(t, expectedNewSum, newSum,
		"Sum should increase by delta × path_length (%d × %d = %d)",
		delta, pathLength, delta*pathLength)
}

func TestPathUpdate_MultipleUpdates(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	nodeID := "5"
	initialValue := initialValues[nodeID]

	// Apply multiple updates
	deltas := []int64{5, -2, 10, -3}
	expectedValue := initialValue
	for _, delta := range deltas {
		err := engine.PathUpdate(ctx, nodeID, nodeID, delta, testLogger)
		require.NoError(t, err)
		expectedValue += delta
	}

	// Verify final value
	verifyNodeValue(t, engine, nodeID, expectedValue)

	// Verify stats
	stats := engine.Stats()
	assert.Equal(t, int64(len(deltas)), stats.UpdateCount)
}

func TestPathUpdate_UpdateQueryUpdate(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Use simple single-node path to test interleaved update-query-update
	nodeID := "3"
	initialValue := initialValues[nodeID]

	// First update
	delta1 := int64(5)
	err := engine.PathUpdate(ctx, nodeID, nodeID, delta1, testLogger)
	require.NoError(t, err)

	// Query intermediate state
	value1, err := engine.PathSum(ctx, nodeID, nodeID, testLogger)
	require.NoError(t, err)
	expectedValue1 := initialValue + delta1
	assert.Equal(t, expectedValue1, value1, "First query should reflect first update")

	// Second update
	delta2 := int64(3)
	err = engine.PathUpdate(ctx, nodeID, nodeID, delta2, testLogger)
	require.NoError(t, err)

	// Query final state
	value2, err := engine.PathSum(ctx, nodeID, nodeID, testLogger)
	require.NoError(t, err)
	expectedValue2 := expectedValue1 + delta2
	assert.Equal(t, expectedValue2, value2, "Second query should reflect both updates")
}

// ==============================================================================
// PathSet Tests
// ==============================================================================

func TestPathSet_BasicPath(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "2", "5"
	targetValue := int64(100)

	err := engine.PathSet(ctx, u, v, targetValue, testLogger)
	require.NoError(t, err)

	// Verify all nodes on path have target value
	verifyNodeValue(t, engine, u, targetValue)
	verifyNodeValue(t, engine, v, targetValue)
}

func TestPathSet_ThenQuery(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Use single node to test PathSet
	nodeID := "3"
	targetValue := int64(50)

	err := engine.PathSet(ctx, nodeID, nodeID, targetValue, testLogger)
	require.NoError(t, err)

	// Query value
	value, err := engine.PathSum(ctx, nodeID, nodeID, testLogger)
	require.NoError(t, err)

	assert.Equal(t, targetValue, value)
}

// ==============================================================================
// Increment/Decrement Tests
// ==============================================================================

func TestPathIncrement(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "4", "7"

	err := engine.PathIncrement(ctx, u, v, testLogger)
	require.NoError(t, err)

	// Verify values incremented by 1
	verifyNodeValue(t, engine, u, initialValues[u]+1)
	verifyNodeValue(t, engine, v, initialValues[v]+1)
}

func TestPathDecrement(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "2", "3"

	err := engine.PathDecrement(ctx, u, v, testLogger)
	require.NoError(t, err)

	// Verify values decremented by 1
	verifyNodeValue(t, engine, u, initialValues[u]-1)
	verifyNodeValue(t, engine, v, initialValues[v]-1)
}

// ==============================================================================
// Error Cases
// ==============================================================================

func TestPathUpdate_InvalidInputs(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		err := engine.PathUpdate(nil, "1", "2", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("empty node u", func(t *testing.T) {
		err := engine.PathUpdate(ctx, "", "2", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "u must not be empty")
	})

	t.Run("empty node v", func(t *testing.T) {
		err := engine.PathUpdate(ctx, "1", "", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "v must not be empty")
	})

	t.Run("nil logger", func(t *testing.T) {
		err := engine.PathUpdate(ctx, "1", "2", 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger must not be nil")
	})

	t.Run("node not exist", func(t *testing.T) {
		err := engine.PathUpdate(ctx, "1", "999", 10, testLogger)
		assert.Error(t, err)
		// Should fail in LCA computation or path decomposition
	})

	t.Run("context canceled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		err := engine.PathUpdate(cancelCtx, "1", "2", 10, testLogger)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context timeout", func(t *testing.T) {
		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond) // Ensure timeout

		err := engine.PathUpdate(timeoutCtx, "1", "2", 10, testLogger)
		assert.Error(t, err)
		// Should hit timeout or cancellation
	})
}

func TestPathSet_InvalidInputs(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		err := engine.PathSet(nil, "1", "2", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("empty node u", func(t *testing.T) {
		err := engine.PathSet(ctx, "", "2", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "u must not be empty")
	})

	t.Run("empty node v", func(t *testing.T) {
		err := engine.PathSet(ctx, "1", "", 10, testLogger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "v must not be empty")
	})

	t.Run("nil logger", func(t *testing.T) {
		err := engine.PathSet(ctx, "1", "2", 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger must not be nil")
	})
}

// ==============================================================================
// Internal Tests
// ==============================================================================

func TestUpdateSegments_ValidSegments(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Create test segments
	segments := []PathSegment{
		{Start: 0, End: 2},
		{Start: 3, End: 5},
	}
	delta := int64(10)

	totalPositions, err := engine.updateSegments(ctx, segments, delta, "test_u", "test_v", testLogger)
	require.NoError(t, err)

	// (2-0+1) + (5-3+1) = 3 + 3 = 6 positions
	expectedPositions := 6
	assert.Equal(t, expectedPositions, totalPositions)
}

func TestUpdateSegments_OutOfBounds(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Create segment with out-of-bounds indices
	treeSize := engine.segTree.size
	segments := []PathSegment{
		{Start: 0, End: treeSize + 10}, // End beyond tree size
	}
	delta := int64(5)

	_, err := engine.updateSegments(ctx, segments, delta, "test_u", "test_v", testLogger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
}

func TestUpdateSegments_NilSegments(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	_, err := engine.updateSegments(ctx, nil, 10, "test_u", "test_v", testLogger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "segments must not be nil")
}

func TestUpdateSegments_ContextCancellation(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	segments := []PathSegment{
		{Start: 0, End: 2},
	}

	_, err := engine.updateSegments(ctx, segments, 10, "test_u", "test_v", testLogger)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ==============================================================================
// Stats Tests
// ==============================================================================

func TestPathUpdateEngine_Stats(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Initial stats should be zero
	stats := engine.Stats()
	assert.Equal(t, int64(0), stats.UpdateCount)
	assert.Equal(t, time.Duration(0), stats.TotalUpdateLatency)
	assert.Equal(t, time.Duration(0), stats.AvgUpdateLatency)
	assert.Equal(t, int64(0), stats.SegmentsUpdated)
	assert.Equal(t, float64(0), stats.AvgSegmentsPerUpdate)

	// Run some updates
	err := engine.PathUpdate(ctx, "1", "2", 5, testLogger)
	require.NoError(t, err)

	err = engine.PathUpdate(ctx, "3", "4", 10, testLogger)
	require.NoError(t, err)

	// Stats should be updated
	stats = engine.Stats()
	assert.Equal(t, int64(2), stats.UpdateCount)
	assert.Greater(t, stats.TotalUpdateLatency, time.Duration(0))
	assert.Greater(t, stats.AvgUpdateLatency, time.Duration(0))
	assert.Greater(t, stats.SegmentsUpdated, int64(0))
	assert.Greater(t, stats.AvgSegmentsPerUpdate, float64(0))
}

func TestPathUpdateEngine_StatsAfterNoOp(t *testing.T) {
	engine, _, _ := buildTestUpdateEngine(t)
	ctx := context.Background()

	// Zero delta update
	err := engine.PathUpdate(ctx, "1", "2", 0, testLogger)
	require.NoError(t, err)

	stats := engine.Stats()
	assert.Equal(t, int64(1), stats.UpdateCount)
	// No-op updates have 0 segments
	assert.Equal(t, int64(0), stats.SegmentsUpdated)
}

// ==============================================================================
// CRS Integration Tests (M-POST-1)
// ==============================================================================

func TestPathUpdate_CRSRecording(t *testing.T) {
	ctx := context.Background()
	g, initialValues := buildTestTree()
	g.Freeze()

	// Build HLD
	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	// Build value array
	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = initialValues[nodeID]
	}

	// Build segment tree
	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(t, err)

	// Create mock CRS recorder
	mockCRS := &mockCRSRecorder{
		steps: make([]crs.StepRecord, 0),
	}

	// Create path query engine WITH CRS
	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(t, err)
	queryEngine.crs = mockCRS
	queryEngine.sessionID = "test-session-123"

	// Create update engine
	updateEngine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(t, err)

	// Perform path update that requires multiple segments
	// Path: 8 -> 5 -> 2 -> 1 -> 4 -> 7 (crosses multiple heavy paths)
	u, v := "8", "7"
	delta := int64(5)

	err = updateEngine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Verify CRS sub-steps were recorded
	assert.Greater(t, len(mockCRS.steps), 0, "CRS should record sub-steps")

	// Filter for SegmentUpdate steps (PathUpdate also records LCA steps)
	segmentUpdateSteps := make([]crs.StepRecord, 0)
	for _, step := range mockCRS.steps {
		if step.Tool == "PathUpdate.SegmentUpdate" {
			segmentUpdateSteps = append(segmentUpdateSteps, step)
		}
	}

	// Verify we have segment update steps
	assert.Greater(t, len(segmentUpdateSteps), 0,
		"CRS should record at least one SegmentUpdate sub-step")

	// Verify all segment update steps have correct structure
	for i, step := range segmentUpdateSteps {
		assert.Equal(t, "test-session-123", step.SessionID,
			"Step %d should have correct session ID", i)
		assert.Equal(t, crs.ActorSystem, step.Actor,
			"Step %d should have System actor", i)
		assert.Equal(t, crs.DecisionExecuteTool, step.Decision,
			"Step %d should have ExecuteTool decision", i)
		assert.NotNil(t, step.ToolParams,
			"Step %d should have ToolParams", i)
		assert.Contains(t, step.ResultSummary, "Updated positions",
			"Step %d should have result summary", i)
	}

	// Verify all CRS step numbers are sequential
	for i := 1; i < len(mockCRS.steps); i++ {
		assert.Equal(t, mockCRS.steps[i-1].StepNumber+1, mockCRS.steps[i].StepNumber,
			"Steps should have sequential numbers")
	}
}

func TestPathUpdate_CRSRecordingOnError(t *testing.T) {
	ctx := context.Background()
	g, initialValues := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(t, err)

	valueArr := make([]int64, hld.NodeCount())
	for i := 0; i < hld.NodeCount(); i++ {
		nodeIdx := hld.NodeAtPos(i)
		nodeID, _ := hld.IdxToNode(nodeIdx)
		valueArr[i] = initialValues[nodeID]
	}

	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(t, err)

	mockCRS := &mockCRSRecorder{
		steps: make([]crs.StepRecord, 0),
	}

	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(t, err)
	queryEngine.crs = mockCRS
	queryEngine.sessionID = "test-session-error"

	updateEngine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(t, err)

	// Attempt update with non-existent node (should fail)
	err = updateEngine.PathUpdate(ctx, "1", "999", 10, testLogger)
	assert.Error(t, err, "Update with non-existent node should fail")

	// CRS recording should still work even on errors
	// (no sub-steps recorded because we fail before updateSegments)
	// This test verifies the CRS integration doesn't break on errors
}

// ==============================================================================
// Forest Mode Tests (M-POST-2)
// ==============================================================================

func TestPathUpdate_ForestMode(t *testing.T) {
	ctx := context.Background()

	// Build a forest with two separate trees
	g := NewGraph("/test/project")

	// Tree 1: nodes A -> B -> C
	tree1Nodes := []string{"A", "B", "C"}
	tree1Values := map[string]int64{"A": 10, "B": 20, "C": 30}
	for _, nodeID := range tree1Nodes {
		sym := &ast.Symbol{
			ID:       nodeID,
			Name:     "func_" + nodeID,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg1",
			FilePath: "pkg1/tree1.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		require.NoError(t, err)
	}
	require.NoError(t, g.AddEdge("A", "B", EdgeTypeCalls, ast.Location{}))
	require.NoError(t, g.AddEdge("B", "C", EdgeTypeCalls, ast.Location{}))

	// Tree 2: nodes X -> Y -> Z
	tree2Nodes := []string{"X", "Y", "Z"}
	tree2Values := map[string]int64{"X": 100, "Y": 200, "Z": 300}
	for _, nodeID := range tree2Nodes {
		sym := &ast.Symbol{
			ID:       nodeID,
			Name:     "func_" + nodeID,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg2",
			FilePath: "pkg2/tree2.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		require.NoError(t, err)
	}
	require.NoError(t, g.AddEdge("X", "Y", EdgeTypeCalls, ast.Location{}))
	require.NoError(t, g.AddEdge("Y", "Z", EdgeTypeCalls, ast.Location{}))

	g.Freeze()

	// Build forest HLD
	forest, err := BuildHLDForest(ctx, g, true)
	require.NoError(t, err)
	require.Equal(t, 2, forest.TreeCount(), "Forest should have 2 trees")

	// Build value array for forest
	// For a forest, positions are laid out sequentially: tree0 positions, then tree1, etc.
	allValues := make(map[string]int64)
	for k, v := range tree1Values {
		allValues[k] = v
	}
	for k, v := range tree2Values {
		allValues[k] = v
	}

	valueArr := make([]int64, forest.TotalNodes())
	position := 0
	for treeIdx := 0; treeIdx < forest.TreeCount(); treeIdx++ {
		hld := forest.GetHLDByIndex(treeIdx)
		require.NotNil(t, hld, "HLD for tree %d should not be nil", treeIdx)

		for i := 0; i < hld.NodeCount(); i++ {
			nodeIdx := hld.NodeAtPos(i)
			nodeID, err := hld.IdxToNode(nodeIdx)
			require.NoError(t, err, "Node index %d in tree %d should be valid", nodeIdx, treeIdx)
			valueArr[position] = allValues[nodeID]
			position++
		}
	}

	// Build segment tree
	segTree, err := NewSegmentTree(ctx, valueArr, AggregateSUM)
	require.NoError(t, err)

	// Create query engine with forest
	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngineForForest(forest, segTree, AggregateSUM, &opts)
	require.NoError(t, err)

	// Create update engine
	updateEngine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(t, err)

	t.Run("verify initial values before updates", func(t *testing.T) {
		// Verify we can query initial values correctly (BEFORE any updates)
		sumA, err := updateEngine.PathSum(ctx, "A", "A", testLogger)
		require.NoError(t, err)
		assert.Equal(t, tree1Values["A"], sumA, "Initial value of A should match")

		sumX, err := updateEngine.PathSum(ctx, "X", "X", testLogger)
		require.NoError(t, err)
		assert.Equal(t, tree2Values["X"], sumX, "Initial value of X should match")
	})

	t.Run("update within same tree succeeds", func(t *testing.T) {
		// Update path within tree 1 (A -> B -> C)
		u, v := "A", "C"
		delta := int64(5)

		// Get initial sum
		initialSum, err := updateEngine.PathSum(ctx, u, v, testLogger)
		require.NoError(t, err)
		expectedInitialSum := tree1Values["A"] + tree1Values["B"] + tree1Values["C"]
		assert.Equal(t, expectedInitialSum, initialSum,
			"Initial sum should match expected")

		// Update path
		err = updateEngine.PathUpdate(ctx, u, v, delta, testLogger)
		require.NoError(t, err, "Update within same tree should succeed")

		// Verify updated sum
		newSum, err := updateEngine.PathSum(ctx, u, v, testLogger)
		require.NoError(t, err)
		pathLength := int64(3) // A, B, C
		expectedNewSum := initialSum + delta*pathLength
		assert.Equal(t, expectedNewSum, newSum,
			"Sum should increase by delta × path_length (%d × %d = %d)",
			delta, pathLength, delta*pathLength)
	})

	t.Run("update across trees fails", func(t *testing.T) {
		// Attempt update from tree 1 to tree 2 (should fail)
		u, v := "A", "X"
		delta := int64(10)

		err := updateEngine.PathUpdate(ctx, u, v, delta, testLogger)
		assert.Error(t, err, "Update across trees should fail")
		assert.Contains(t, err.Error(), "different tree", "Error should mention cross-tree")
	})

	t.Run("update within tree 2 succeeds", func(t *testing.T) {
		// Update path within tree 2 (X -> Y)
		u, v := "X", "Y"
		delta := int64(7)

		initialSum, err := updateEngine.PathSum(ctx, u, v, testLogger)
		require.NoError(t, err)

		err = updateEngine.PathUpdate(ctx, u, v, delta, testLogger)
		require.NoError(t, err, "Update within same tree should succeed")

		newSum, err := updateEngine.PathSum(ctx, u, v, testLogger)
		require.NoError(t, err)
		pathLength := int64(2) // X, Y
		expectedNewSum := initialSum + delta*pathLength
		assert.Equal(t, expectedNewSum, newSum)
	})

	t.Run("verify trees remain isolated", func(t *testing.T) {
		// After updating tree 1 and tree 2, verify they don't affect each other

		// Tree 1: A was updated by +5 (3 nodes), then no changes
		// Node A: 10 + 5 = 15
		sumA, err := updateEngine.PathSum(ctx, "A", "A", testLogger)
		require.NoError(t, err)
		assert.Equal(t, int64(15), sumA, "Tree 1 node A should reflect only tree 1 updates")

		// Tree 2: X was updated by +7 (2 nodes: X, Y)
		// Node X: 100 + 7 = 107
		sumX, err := updateEngine.PathSum(ctx, "X", "X", testLogger)
		require.NoError(t, err)
		assert.Equal(t, int64(107), sumX, "Tree 2 node X should reflect only tree 2 updates")

		// Tree 1 node C was updated once by +5
		sumC, err := updateEngine.PathSum(ctx, "C", "C", testLogger)
		require.NoError(t, err)
		assert.Equal(t, int64(35), sumC, "Tree 1 node C: 30 + 5 = 35")

		// Tree 2 node Z was NOT updated
		sumZ, err := updateEngine.PathSum(ctx, "Z", "Z", testLogger)
		require.NoError(t, err)
		assert.Equal(t, int64(300), sumZ, "Tree 2 node Z should be unchanged")
	})
}

// ==============================================================================
// Cache Invalidation Tests
// ==============================================================================

func TestPathUpdate_InvalidatesQueryCache(t *testing.T) {
	engine, _, initialValues := buildTestUpdateEngine(t)
	ctx := context.Background()

	u, v := "1", "2"

	// Query to populate cache
	sum1, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)
	expectedSum1 := initialValues[u] + initialValues[v]
	assert.Equal(t, expectedSum1, sum1)

	// Update path
	delta := int64(10)
	err = engine.PathUpdate(ctx, u, v, delta, testLogger)
	require.NoError(t, err)

	// Query again - should return updated value, not cached old value
	sum2, err := engine.PathSum(ctx, u, v, testLogger)
	require.NoError(t, err)
	expectedSum2 := expectedSum1 + delta*2 // 2 nodes
	assert.Equal(t, expectedSum2, sum2, "Query after update should not use stale cache")
}

// ==============================================================================
// Benchmarks
// ==============================================================================

func BenchmarkPathUpdate_SameHeavyPath(b *testing.B) {
	ctx := context.Background()
	g, _ := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(b, err)

	values := make([]int64, hld.NodeCount())
	for i := range values {
		values[i] = int64(i + 1)
	}

	segTree, err := NewSegmentTree(ctx, values, AggregateSUM)
	require.NoError(b, err)

	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(b, err)

	engine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(b, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.PathUpdate(ctx, "8", "9", 1, logger)
	}
}

func BenchmarkPathUpdate_DifferentPaths(b *testing.B) {
	ctx := context.Background()
	g, _ := buildTestTree()
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, "1")
	require.NoError(b, err)

	values := make([]int64, hld.NodeCount())
	for i := range values {
		values[i] = int64(i + 1)
	}

	segTree, err := NewSegmentTree(ctx, values, AggregateSUM)
	require.NoError(b, err)

	opts := DefaultPathQueryEngineOptions()
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(b, err)

	engine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(b, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.PathUpdate(ctx, "8", "7", 1, logger)
	}
}

func BenchmarkPathUpdate_N1000(b *testing.B) {
	// Build a chain of 1000 nodes
	ctx := context.Background()
	g := NewGraph("/test/project")

	values := make(map[string]int64, 1000)
	nodeIDs := make([]string, 1000)

	for i := 0; i < 1000; i++ {
		// Generate unique node IDs
		nodeID := ""
		if i < 26 {
			nodeID = string(rune('A' + i))
		} else {
			nodeID = string(rune('A'+i%26)) + string(rune('0'+i/26))
		}
		nodeIDs[i] = nodeID

		sym := &ast.Symbol{
			ID:       nodeID,
			Name:     "node_" + nodeID,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		require.NoError(b, err)
		values[nodeID] = int64(i + 1)

		if i > 0 {
			err = g.AddEdge(nodeIDs[i-1], nodeID, EdgeTypeCalls, ast.Location{})
			require.NoError(b, err)
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, nodeIDs[0])
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
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(b, err)

	engine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(b, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Update from root to deep leaf
	rootID := nodeIDs[0]
	leafID := nodeIDs[999]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.PathUpdate(ctx, rootID, leafID, 1, logger)
	}
}

func BenchmarkPathSet_N1000(b *testing.B) {
	// Build a chain of 1000 nodes
	ctx := context.Background()
	g := NewGraph("/test/project")

	values := make(map[string]int64, 1000)
	nodeIDs := make([]string, 1000)

	for i := 0; i < 1000; i++ {
		nodeID := ""
		if i < 26 {
			nodeID = string(rune('A' + i))
		} else {
			nodeID = string(rune('A'+i%26)) + string(rune('0'+i/26))
		}
		nodeIDs[i] = nodeID

		sym := &ast.Symbol{
			ID:       nodeID,
			Name:     "node_" + nodeID,
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg/test",
			FilePath: "pkg/test/test.go",
			Exported: true,
		}
		_, err := g.AddNode(sym)
		require.NoError(b, err)
		values[nodeID] = int64(i + 1)

		if i > 0 {
			err = g.AddEdge(nodeIDs[i-1], nodeID, EdgeTypeCalls, ast.Location{})
			require.NoError(b, err)
		}
	}
	g.Freeze()

	hld, err := BuildHLDIterative(ctx, g, nodeIDs[0])
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
	queryEngine, err := NewPathQueryEngine(hld, segTree, AggregateSUM, &opts)
	require.NoError(b, err)

	engine, err := NewPathUpdateEngine(queryEngine)
	require.NoError(b, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	rootID := nodeIDs[0]
	leafID := nodeIDs[999]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.PathSet(ctx, rootID, leafID, 100, logger)
	}
}
