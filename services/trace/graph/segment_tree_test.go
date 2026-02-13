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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create test array
func makeTestArray(size int) []int64 {
	arr := make([]int64, size)
	for i := range arr {
		arr[i] = int64(i + 1)
	}
	return arr
}

// Test construction with valid inputs
func TestNewSegmentTree_ValidInput(t *testing.T) {
	arr := []int64{3, 1, 4, 2, 5, 7, 6, 8}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)
	require.NotNil(t, tree)

	assert.Equal(t, 8, tree.size)
	assert.Equal(t, 8, tree.n) // 8 is already power of 2
	assert.Equal(t, AggregateSUM, tree.aggFunc)
}

// Test single element array
func TestNewSegmentTree_SingleElement(t *testing.T) {
	arr := []int64{42}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Query entire range
	sum, err := tree.Query(context.Background(), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(42), sum)

	// Update
	err = tree.Update(context.Background(), 0, 100)
	require.NoError(t, err)

	sum, err = tree.Query(context.Background(), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(100), sum)
}

// Test power-of-2 sizes
func TestNewSegmentTree_PowerOf2Sizes(t *testing.T) {
	sizes := []int{1, 2, 4, 8, 16, 32, 64, 128, 256}
	for _, size := range sizes {
		arr := makeTestArray(size)
		tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
		require.NoError(t, err, "size=%d", size)

		// Verify padded size equals actual size (no padding needed)
		assert.Equal(t, size, tree.n, "size=%d", size)

		// Query entire range
		sum, err := tree.Query(context.Background(), 0, size-1)
		require.NoError(t, err)

		expectedSum := int64(size * (size + 1) / 2)
		assert.Equal(t, expectedSum, sum, "size=%d", size)
	}
}

// Test non-power-of-2 sizes (CRITICAL: tests padding fix)
func TestNewSegmentTree_NonPowerOf2Sizes(t *testing.T) {
	sizes := []int{3, 5, 7, 9, 10, 13, 17, 100, 1000}
	for _, size := range sizes {
		arr := makeTestArray(size)
		tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
		require.NoError(t, err, "size=%d", size)

		// Verify padded size is next power of 2
		expectedN := nextPowerOf2(size)
		assert.Equal(t, expectedN, tree.n, "size=%d", size)

		// Query entire range
		sum, err := tree.Query(context.Background(), 0, size-1)
		require.NoError(t, err, "size=%d", size)

		expectedSum := int64(size * (size + 1) / 2)
		assert.Equal(t, expectedSum, sum, "size=%d", size)
	}
}

// Test all aggregation functions
func TestNewSegmentTree_AllAggFunctions(t *testing.T) {
	arr := []int64{3, 1, 4, 2, 5}

	tests := []struct {
		name     string
		aggFunc  AggregateFunc
		expected int64
	}{
		{"SUM", AggregateSUM, 15},
		{"MIN", AggregateMIN, 1},
		{"MAX", AggregateMAX, 5},
		{"GCD", AggregateGCD, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := NewSegmentTree(context.Background(), arr, tt.aggFunc)
			require.NoError(t, err)

			result, err := tree.Query(context.Background(), 0, 4)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test invalid inputs
func TestNewSegmentTree_InvalidInputs(t *testing.T) {
	arr := []int64{1, 2, 3}

	t.Run("nil context", func(t *testing.T) {
		_, err := NewSegmentTree(nil, arr, AggregateSUM)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ctx must not be nil")
	})

	t.Run("empty array", func(t *testing.T) {
		_, err := NewSegmentTree(context.Background(), []int64{}, AggregateSUM)
		assert.ErrorIs(t, err, ErrEmptyArray)
	})

	t.Run("invalid agg func", func(t *testing.T) {
		_, err := NewSegmentTree(context.Background(), arr, AggregateFunc(999))
		assert.ErrorIs(t, err, ErrInvalidAggFunc)
	})

	t.Run("array too large", func(t *testing.T) {
		largeSize := (1 << 30) + 1 // > MaxInt32/4
		_, err := NewSegmentTree(context.Background(), make([]int64, largeSize), AggregateSUM)
		assert.ErrorIs(t, err, ErrArrayTooLarge)
	})
}

// Test query entire range
func TestSegmentTree_Query_EntireRange(t *testing.T) {
	arr := makeTestArray(8)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	sum, err := tree.Query(context.Background(), 0, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(36), sum) // 1+2+3+4+5+6+7+8
}

// Test query single element
func TestSegmentTree_Query_SingleElement(t *testing.T) {
	arr := makeTestArray(8)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	for i := 0; i < 8; i++ {
		val, err := tree.Query(context.Background(), i, i)
		require.NoError(t, err)
		assert.Equal(t, int64(i+1), val, "index=%d", i)
	}
}

// Test query sub-ranges
func TestSegmentTree_Query_SubRanges(t *testing.T) {
	arr := []int64{3, 1, 4, 2, 5, 7, 6, 8}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	tests := []struct {
		left     int
		right    int
		expected int64
	}{
		{0, 3, 10}, // 3+1+4+2
		{2, 5, 18}, // 4+2+5+7
		{4, 7, 26}, // 5+7+6+8
		{1, 3, 7},  // 1+4+2
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("[%d,%d]", tt.left, tt.right), func(t *testing.T) {
			sum, err := tree.Query(context.Background(), tt.left, tt.right)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, sum)
		})
	}
}

// Test query with invalid ranges
func TestSegmentTree_Query_InvalidRanges(t *testing.T) {
	arr := makeTestArray(5)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	tests := []struct {
		name  string
		left  int
		right int
	}{
		{"left < 0", -1, 3},
		{"right >= size", 0, 5},
		{"left > right", 3, 1},
		{"both out of bounds", -5, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tree.Query(context.Background(), tt.left, tt.right)
			assert.ErrorIs(t, err, ErrInvalidRange)
		})
	}
}

// Test SUM aggregation
func TestSegmentTree_SUM(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	sum, err := tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(15), sum)
}

// Test MIN aggregation
func TestSegmentTree_MIN(t *testing.T) {
	arr := []int64{5, 2, 8, 1, 9, 3}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateMIN)
	require.NoError(t, err)

	min, err := tree.Query(context.Background(), 0, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(1), min)

	min, err = tree.Query(context.Background(), 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), min)
}

// Test MAX aggregation
func TestSegmentTree_MAX(t *testing.T) {
	arr := []int64{5, 2, 8, 1, 9, 3}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateMAX)
	require.NoError(t, err)

	max, err := tree.Query(context.Background(), 0, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(9), max)

	max, err = tree.Query(context.Background(), 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(8), max)
}

// Test GCD aggregation
func TestSegmentTree_GCD(t *testing.T) {
	arr := []int64{12, 18, 24, 30}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateGCD)
	require.NoError(t, err)

	gcdVal, err := tree.Query(context.Background(), 0, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(6), gcdVal)
}

// Test MIN with negative values
func TestSegmentTree_MIN_Negatives(t *testing.T) {
	arr := []int64{-10, 5, -3, 8, -1, 7, -20, 4}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateMIN)
	require.NoError(t, err)

	min, err := tree.Query(context.Background(), 0, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(-20), min)

	min, err = tree.Query(context.Background(), 1, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(-3), min)
}

// Test single element update
func TestSegmentTree_Update_SingleElement(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Update arr[2] = 10
	err = tree.Update(context.Background(), 2, 10)
	require.NoError(t, err)

	// Query to verify
	sum, err := tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(22), sum) // 1+2+10+4+5

	// Verify specific element
	val, err := tree.GetValue(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, int64(10), val)
}

// Test multiple updates
func TestSegmentTree_Update_MultipleElements(t *testing.T) {
	arr := makeTestArray(5)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Initial sum: 1+2+3+4+5 = 15
	sum, err := tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(15), sum)

	// Update multiple elements
	err = tree.Update(context.Background(), 0, 10)
	require.NoError(t, err)
	err = tree.Update(context.Background(), 2, 20)
	require.NoError(t, err)
	err = tree.Update(context.Background(), 4, 30)
	require.NoError(t, err)

	// New sum: 10+2+20+4+30 = 66
	sum, err = tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(66), sum)
}

// Test update with invalid index
func TestSegmentTree_Update_InvalidIndex(t *testing.T) {
	arr := makeTestArray(5)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	tests := []struct {
		name  string
		index int
	}{
		{"negative index", -1},
		{"index >= size", 5},
		{"large index", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tree.Update(context.Background(), tt.index, 10)
			assert.ErrorIs(t, err, ErrInvalidRange)
		})
	}
}

// Test range update entire range
func TestSegmentTree_RangeUpdate_EntireRange(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Add 10 to entire array
	err = tree.RangeUpdate(context.Background(), 0, 4, 10)
	require.NoError(t, err)

	// Each element should be +10: [11, 12, 13, 14, 15]
	sum, err := tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(65), sum) // 11+12+13+14+15
}

// Test range update sub-range
func TestSegmentTree_RangeUpdate_SubRange(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Add 10 to [1,3]
	err = tree.RangeUpdate(context.Background(), 1, 3, 10)
	require.NoError(t, err)

	// Expected: [1, 12, 13, 14, 5]
	vals := []int64{1, 12, 13, 14, 5}
	for i, expected := range vals {
		val, err := tree.GetValue(context.Background(), i)
		require.NoError(t, err)
		assert.Equal(t, expected, val, "index=%d", i)
	}

	// Query updated range
	sum, err := tree.Query(context.Background(), 1, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(39), sum) // 12+13+14
}

// Test range update with lazy propagation
func TestSegmentTree_RangeUpdate_LazyPropagation(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Multiple range updates
	err = tree.RangeUpdate(context.Background(), 0, 2, 5)
	require.NoError(t, err)

	err = tree.RangeUpdate(context.Background(), 2, 4, 3)
	require.NoError(t, err)

	// Expected: [6, 7, 11, 7, 8]
	// arr[0] = 1+5 = 6
	// arr[1] = 2+5 = 7
	// arr[2] = 3+5+3 = 11
	// arr[3] = 4+3 = 7
	// arr[4] = 5+3 = 8

	vals := []int64{6, 7, 11, 7, 8}
	for i, expected := range vals {
		val, err := tree.GetValue(context.Background(), i)
		require.NoError(t, err)
		assert.Equal(t, expected, val, "index=%d", i)
	}
}

// Test range update only works for SUM (CRITICAL FIX)
func TestSegmentTree_RangeUpdate_OnlySUM(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}

	tests := []struct {
		name    string
		aggFunc AggregateFunc
	}{
		{"MIN", AggregateMIN},
		{"MAX", AggregateMAX},
		{"GCD", AggregateGCD},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := NewSegmentTree(context.Background(), arr, tt.aggFunc)
			require.NoError(t, err)

			err = tree.RangeUpdate(context.Background(), 0, 2, 5)
			assert.ErrorIs(t, err, ErrRangeUpdateNotSUM)
		})
	}
}

// Test validation
func TestSegmentTree_Validate_ValidTree(t *testing.T) {
	arr := makeTestArray(8)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Validation should pass
	assert.NoError(t, tree.Validate())
}

// Test stats
func TestSegmentTree_Stats(t *testing.T) {
	arr := makeTestArray(10)
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	stats := tree.Stats()

	assert.Equal(t, 10, stats.Size)
	assert.Equal(t, 16, stats.PaddedSize) // Next power of 2
	assert.Equal(t, 32, stats.TreeSize)   // 2 * paddedSize
	assert.Equal(t, 4, stats.Height)      // log2(16)
	assert.Equal(t, AggregateSUM, stats.AggFunc)
	assert.Greater(t, stats.BuildTime, time.Duration(0))
	assert.Greater(t, stats.MemoryBytes, 0)
}

// Test context cancellation during build
func TestNewSegmentTree_ContextCancellation(t *testing.T) {
	arr := make([]int64, 100000)
	for i := range arr {
		arr[i] = int64(i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Build should check context and fail fast
	_, err := NewSegmentTree(ctx, arr, AggregateSUM)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// Test integer overflow handling
func TestSegmentTree_IntegerOverflow(t *testing.T) {
	arr := []int64{1 << 62, 1 << 62, 1 << 62} // Large values that will overflow
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Query should saturate at MaxInt64
	sum, err := tree.Query(context.Background(), 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1<<63-1), sum) // MaxInt64
}

// Test zero values
func TestSegmentTree_ZeroValues(t *testing.T) {
	arr := []int64{0, 0, 0, 0, 0}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	sum, err := tree.Query(context.Background(), 0, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(0), sum)
}

// Test determinism
func TestSegmentTree_Determinism(t *testing.T) {
	arr := []int64{3, 1, 4, 1, 5, 9, 2, 6}

	tree1, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	tree2, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	// Both trees should produce identical results
	for i := 0; i < len(arr); i++ {
		for j := i; j < len(arr); j++ {
			sum1, err := tree1.Query(context.Background(), i, j)
			require.NoError(t, err)

			sum2, err := tree2.Query(context.Background(), i, j)
			require.NoError(t, err)

			assert.Equal(t, sum1, sum2, "range [%d,%d]", i, j)
		}
	}
}

// Test cache key changes with updates
func TestSegmentTree_CacheKey_VersionTracking(t *testing.T) {
	arr := []int64{1, 2, 3, 4, 5}
	tree, err := NewSegmentTree(context.Background(), arr, AggregateSUM)
	require.NoError(t, err)

	key1 := tree.CacheKey()

	// Update should change version and cache key
	err = tree.Update(context.Background(), 2, 10)
	require.NoError(t, err)

	key2 := tree.CacheKey()
	assert.NotEqual(t, key1, key2, "cache key should change after update")

	// Another update
	err = tree.Update(context.Background(), 3, 20)
	require.NoError(t, err)

	key3 := tree.CacheKey()
	assert.NotEqual(t, key2, key3, "cache key should change after second update")
}

// Benchmarks
func BenchmarkNewSegmentTree_N100(b *testing.B) {
	arr := makeTestArray(100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewSegmentTree(ctx, arr, AggregateSUM)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewSegmentTree_N1000(b *testing.B) {
	arr := makeTestArray(1000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewSegmentTree(ctx, arr, AggregateSUM)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewSegmentTree_N10000(b *testing.B) {
	arr := makeTestArray(10000)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewSegmentTree(ctx, arr, AggregateSUM)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQuery_N1000_SingleElement(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tree.Query(ctx, 500, 500)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQuery_N1000_HalfRange(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tree.Query(ctx, 250, 750)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQuery_N1000_EntireRange(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tree.Query(ctx, 0, 999)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdate_N1000(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := tree.Update(ctx, 500, int64(i))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRangeUpdate_N1000_SmallRange(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := tree.RangeUpdate(ctx, 480, 520, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRangeUpdate_N1000_LargeRange(b *testing.B) {
	arr := makeTestArray(1000)
	tree, _ := NewSegmentTree(context.Background(), arr, AggregateSUM)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := tree.RangeUpdate(ctx, 100, 900, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}
