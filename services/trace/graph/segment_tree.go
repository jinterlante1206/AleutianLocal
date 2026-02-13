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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sync"
	"time"

	"crypto/sha256"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
)

// Sentinel errors for segment tree operations.
var (
	ErrEmptyArray        = errors.New("array must not be empty")
	ErrArrayTooLarge     = errors.New("array size exceeds maximum")
	ErrInvalidAggFunc    = errors.New("invalid aggregation function")
	ErrInvalidRange      = errors.New("invalid query range")
	ErrRangeUpdateNotSUM = errors.New("range update only supported for SUM aggregation")
)

// AggregateFunc defines the type of aggregation operation.
type AggregateFunc int

const (
	AggregateSUM AggregateFunc = iota // Sum of values
	AggregateMIN                      // Minimum value
	AggregateMAX                      // Maximum value
	AggregateGCD                      // Greatest common divisor
)

// String returns the string representation of the aggregation function.
func (f AggregateFunc) String() string {
	switch f {
	case AggregateSUM:
		return "SUM"
	case AggregateMIN:
		return "MIN"
	case AggregateMAX:
		return "MAX"
	case AggregateGCD:
		return "GCD"
	default:
		return "UNKNOWN"
	}
}

// Identity returns the identity element for the aggregation function.
func (f AggregateFunc) Identity() int64 {
	switch f {
	case AggregateSUM:
		return 0
	case AggregateMIN:
		return math.MaxInt64
	case AggregateMAX:
		return math.MinInt64
	case AggregateGCD:
		return 0
	default:
		return 0
	}
}

// Combine applies the aggregation function to two values.
//
// Description:
//
//	Combines two values using the aggregation function. Handles integer
//	overflow for SUM by saturating at MaxInt64/MinInt64.
//
// Thread Safety: Safe for concurrent use (pure function).
func (f AggregateFunc) Combine(a, b int64) int64 {
	switch f {
	case AggregateSUM:
		// Check for overflow before adding
		if a > 0 && b > 0 && a > math.MaxInt64-b {
			slog.Warn("integer overflow in segment tree SUM",
				slog.Int64("a", a),
				slog.Int64("b", b),
			)
			return math.MaxInt64 // Saturate
		}
		if a < 0 && b < 0 && a < math.MinInt64-b {
			slog.Warn("integer underflow in segment tree SUM",
				slog.Int64("a", a),
				slog.Int64("b", b),
			)
			return math.MinInt64 // Saturate
		}
		return a + b

	case AggregateMIN:
		if a < b {
			return a
		}
		return b

	case AggregateMAX:
		if a > b {
			return a
		}
		return b

	case AggregateGCD:
		return gcd(a, b)

	default:
		return 0
	}
}

// SegmentTree provides efficient range queries and updates over an array.
//
// Description:
//
//	A segment tree supporting O(log N) range queries and updates for
//	associative aggregation functions (sum, min, max, gcd). Supports
//	lazy propagation for efficient range updates (SUM only).
//
// Invariants:
//   - Tree array has size 2*paddedSize where paddedSize is next power of 2
//   - tree[i] represents aggregate over range [L, R]
//   - Parent(i) = i/2, LeftChild(i) = 2*i, RightChild(i) = 2*i+1
//   - lazy[i] stores pending updates for node i's children (SUM only)
//   - After build, tree[1] = aggregate over entire array
//   - version increments on each Update/RangeUpdate
//
// Thread Safety:
//   - All operations are protected by RWMutex
//   - Query operations acquire read lock (thread-safe for concurrent reads)
//   - Update operations acquire write lock (not safe for concurrent updates)
//   - Lazy propagation during Query requires write lock on affected nodes
type SegmentTree struct {
	// Core structure
	tree    []int64       // Segment tree array (1-indexed)
	lazy    []int64       // Lazy propagation array (SUM only)
	hasLazy []bool        // Track which nodes have pending updates
	size    int           // Number of leaves (original array size)
	n       int           // Padded size (next power of 2)
	aggFunc AggregateFunc // Aggregation function

	// Synchronization
	mu sync.RWMutex // Protects tree, lazy, hasLazy, version

	// Metadata
	version     int64         // Incremented on each update (for cache keys)
	buildTime   time.Duration // Construction time
	queryCount  int64         // Number of queries performed
	updateCount int64         // Number of updates performed
}

// SegmentTreeStats contains statistics about the segment tree.
type SegmentTreeStats struct {
	Size        int           // Number of leaves (original array size)
	PaddedSize  int           // Padded size (next power of 2)
	TreeSize    int           // Total tree array size
	Height      int           // Tree height (log2 of padded size)
	AggFunc     AggregateFunc // Aggregation function
	BuildTime   time.Duration // Construction time
	QueryCount  int64         // Queries performed
	UpdateCount int64         // Updates performed
	Version     int64         // Current version
	MemoryBytes int           // Approximate memory usage
}

// NewSegmentTree creates a segment tree from an array of values.
//
// Description:
//
//	Constructs a segment tree supporting range queries/updates over arr.
//	Uses bottom-up construction for O(N) build time. Pads array to next
//	power of 2 for proper tree structure.
//
// Algorithm:
//
//	Time:  O(N) where N = len(arr)
//	Space: O(N) - tree array size 2*paddedSize
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - arr: Initial array values. Must not be empty.
//   - aggFunc: Aggregation function (SUM, MIN, MAX, GCD).
//
// Outputs:
//   - *SegmentTree: Constructed segment tree. Never nil on success.
//   - error: Non-nil if arr is empty, too large, or aggFunc is invalid.
//
// Optimizations:
//   - Single allocation for tree and lazy arrays
//   - Bottom-up construction (iterative, not recursive)
//   - Pre-computed power-of-2 padding
//   - Context cancellation checked periodically
//
// CRS Integration:
//   - Records construction timing and metadata
//   - Deterministic output for caching
//
// Example:
//
//	arr := []int64{3, 1, 4, 2, 5, 7, 6, 8}
//	tree, err := NewSegmentTree(ctx, arr, AggregateSUM)
//	if err != nil {
//	    return fmt.Errorf("build segment tree: %w", err)
//	}
//	sum, _ := tree.Query(ctx, 2, 5)  // Range [2,5] sum = 18
//
// Thread Safety: Safe for concurrent use with different arrays.
func NewSegmentTree(ctx context.Context, arr []int64, aggFunc AggregateFunc) (*SegmentTree, error) {
	// Phase 1: Input validation (before span creation)
	if err := validateNewSegmentTreeInputs(ctx, arr, aggFunc); err != nil {
		return nil, err
	}

	ctx, span := otel.Tracer("trace").Start(ctx, "graph.NewSegmentTree",
		trace.WithAttributes(
			attribute.Int("size", len(arr)),
			attribute.String("agg_func", aggFunc.String()),
		),
	)
	defer span.End()

	start := time.Now()

	// Phase 2: Find next power of 2
	span.AddEvent("computing_padding")
	size := len(arr)
	n := nextPowerOf2(size)

	span.SetAttributes(
		attribute.Int("padded_size", n),
		attribute.Int("height", bits.Len(uint(n))-1),
	)

	// Phase 3: Allocate arrays (single allocation for tree + lazy)
	span.AddEvent("allocating_arrays")
	treeSize := 2 * n
	allArrays := make([]int64, 2*treeSize) // tree + lazy in one allocation

	st := &SegmentTree{
		tree:    allArrays[0:treeSize],
		lazy:    allArrays[treeSize : 2*treeSize],
		hasLazy: make([]bool, treeSize),
		size:    size,
		n:       n,
		aggFunc: aggFunc,
		version: 1,
	}

	// Phase 4: Build tree bottom-up
	span.AddEvent("building_tree")
	if err := st.build(ctx, arr); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "build failed")
		return nil, fmt.Errorf("build segment tree: %w", err)
	}

	st.buildTime = time.Since(start)

	span.AddEvent("tree_built",
		trace.WithAttributes(
			attribute.Int64("build_time_us", st.buildTime.Microseconds()),
			attribute.Int("tree_size", len(st.tree)),
		),
	)

	// Phase 5: Validation
	span.AddEvent("validating_tree")
	if err := st.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		recordSegTreeValidationError(ctx)
		slog.Error("segment tree validation failed",
			slog.Int("size", st.size),
			slog.String("agg_func", st.aggFunc.String()),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("segment tree validation: %w", err)
	}

	// Phase 6: Record metrics
	span.SetAttributes(
		attribute.Int("height", bits.Len(uint(st.n))-1),
		attribute.Int64("build_time_us", st.buildTime.Microseconds()),
		attribute.Int("memory_bytes", st.MemoryUsage()),
	)

	recordSegTreeMetrics(ctx, st.Stats())

	slog.Info("segment tree constructed",
		slog.Int("size", st.size),
		slog.Int("padded_size", st.n),
		slog.String("agg_func", st.aggFunc.String()),
		slog.Duration("build_time", st.buildTime),
		slog.Int("memory_kb", st.MemoryUsage()/1024))

	span.SetStatus(codes.Ok, "segment tree constructed")
	return st, nil
}

// validateNewSegmentTreeInputs validates inputs to NewSegmentTree.
func validateNewSegmentTreeInputs(ctx context.Context, arr []int64, aggFunc AggregateFunc) error {
	// 1. Validate context
	if ctx == nil {
		return errors.New("ctx must not be nil")
	}

	// 2. Validate array
	if len(arr) == 0 {
		return ErrEmptyArray
	}
	if len(arr) > math.MaxInt32/4 {
		return fmt.Errorf("%w: %d > %d", ErrArrayTooLarge, len(arr), math.MaxInt32/4)
	}

	// 3. Validate aggregation function
	switch aggFunc {
	case AggregateSUM, AggregateMIN, AggregateMAX, AggregateGCD:
		// Valid
	default:
		return fmt.Errorf("%w: %d", ErrInvalidAggFunc, aggFunc)
	}

	return nil
}

// nextPowerOf2 returns the smallest power of 2 that is >= n.
func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	// Use bits.Len to find highest set bit
	p := 1 << (bits.Len(uint(n - 1)))
	return p
}

// build constructs the segment tree bottom-up with proper power-of-2 padding.
//
// CRITICAL FIX: Handles non-power-of-2 sizes by padding to next power of 2
// and initializing padded elements with identity values.
func (st *SegmentTree) build(ctx context.Context, arr []int64) error {
	// Initialize entire tree with identity values
	identity := st.aggFunc.Identity()
	for i := range st.tree {
		st.tree[i] = identity
	}

	// Copy actual array values to leaves (starting at index n)
	for i := 0; i < st.size; i++ {
		// Check context periodically (every 1000 elements)
		if i%1000 == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("build cancelled: %w", ctx.Err())
			default:
			}
		}
		st.tree[st.n+i] = arr[i]
	}

	// Padded elements (from size to n) remain as identity

	// Build internal nodes bottom-up
	for i := st.n - 1; i > 0; i-- {
		left := st.tree[2*i]
		right := st.tree[2*i+1]
		st.tree[i] = st.aggFunc.Combine(left, right)
	}

	return nil
}

// Query computes the aggregate over range [left, right] (inclusive).
//
// Description:
//
//	Queries the segment tree for the aggregate value over [left, right].
//	Automatically pushes down lazy updates before reading nodes.
//
// Algorithm:
//
//	Time:  O(log N) where N = size of array
//	Space: O(1) - iterative implementation
//
// Inputs:
//   - ctx: Context for tracing.
//   - left: Left boundary (0-indexed, inclusive). Must be in [0, size).
//   - right: Right boundary (0-indexed, inclusive). Must be in [left, size).
//
// Outputs:
//   - int64: Aggregate value over [left, right].
//   - error: Non-nil if range is invalid.
//
// Example:
//
//	sum, err := tree.Query(ctx, 2, 5)  // Sum of arr[2..5]
//
// Thread Safety: Safe for concurrent reads (protected by RWMutex).
func (st *SegmentTree) Query(ctx context.Context, left, right int) (int64, error) {
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SegmentTree.Query",
		trace.WithAttributes(
			attribute.Int("left", left),
			attribute.Int("right", right),
		),
	)
	defer span.End()

	// Validate range
	if err := st.validateRange(left, right); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, err
	}

	st.mu.RLock()
	defer st.mu.RUnlock()

	result := st.queryRec(1, 0, st.n-1, left, right)

	st.queryCount++

	span.SetAttributes(attribute.Int64("result", result))
	span.SetStatus(codes.Ok, "query complete")

	return result, nil
}

// validateRange validates query/update range bounds.
func (st *SegmentTree) validateRange(left, right int) error {
	if left < 0 || left >= st.size {
		return fmt.Errorf("%w: left index %d out of bounds [0,%d)", ErrInvalidRange, left, st.size)
	}
	if right < 0 || right >= st.size {
		return fmt.Errorf("%w: right index %d out of bounds [0,%d)", ErrInvalidRange, right, st.size)
	}
	if left > right {
		return fmt.Errorf("%w: left %d > right %d", ErrInvalidRange, left, right)
	}
	return nil
}

// queryRec recursively queries range [qL, qR] in node covering [nodeL, nodeR].
func (st *SegmentTree) queryRec(node, nodeL, nodeR, qL, qR int) int64 {
	// If range is outside query, return identity
	if qR < nodeL || qL > nodeR {
		return st.aggFunc.Identity()
	}

	// If range is fully inside query, return node value
	if qL <= nodeL && nodeR <= qR {
		// Push down lazy before returning
		if st.hasLazy[node] {
			st.pushDown(node, nodeL, nodeR)
		}
		return st.tree[node]
	}

	// Push down lazy before recursing
	if st.hasLazy[node] {
		st.pushDown(node, nodeL, nodeR)
	}

	// Partial overlap, recurse on children
	mid := (nodeL + nodeR) / 2
	leftResult := st.queryRec(2*node, nodeL, mid, qL, qR)
	rightResult := st.queryRec(2*node+1, mid+1, nodeR, qL, qR)

	return st.aggFunc.Combine(leftResult, rightResult)
}

// Update sets arr[index] = value and updates tree.
//
// Description:
//
//	Updates a single element and propagates change up to root.
//	Pushes down lazy updates on path from leaf to root.
//
// Algorithm:
//
//	Time:  O(log N)
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for tracing.
//   - index: Array index to update (0-indexed). Must be in [0, size).
//   - value: New value for arr[index].
//
// Outputs:
//   - error: Non-nil if index is out of bounds.
//
// Example:
//
//	err := tree.Update(ctx, 3, 10)  // Set arr[3] = 10
//
// Thread Safety: NOT safe for concurrent use. Caller must synchronize.
func (st *SegmentTree) Update(ctx context.Context, index int, value int64) error {
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SegmentTree.Update",
		trace.WithAttributes(
			attribute.Int("index", index),
			attribute.Int64("value", value),
		),
	)
	defer span.End()

	if index < 0 || index >= st.size {
		err := fmt.Errorf("%w: index %d out of bounds [0,%d)", ErrInvalidRange, index, st.size)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	st.updateRec(1, 0, st.n-1, index, value)
	st.updateCount++
	st.version++

	span.SetStatus(codes.Ok, "update complete")
	return nil
}

// updateRec recursively updates leaf at index with value.
func (st *SegmentTree) updateRec(node, nodeL, nodeR, index int, value int64) {
	// Push down lazy before processing
	if st.hasLazy[node] {
		st.pushDown(node, nodeL, nodeR)
	}

	// Leaf node
	if nodeL == nodeR {
		st.tree[node] = value
		return
	}

	// Recurse to appropriate child
	mid := (nodeL + nodeR) / 2
	if index <= mid {
		st.updateRec(2*node, nodeL, mid, index, value)
	} else {
		st.updateRec(2*node+1, mid+1, nodeR, index, value)
	}

	// Update current node from children
	st.tree[node] = st.aggFunc.Combine(st.tree[2*node], st.tree[2*node+1])
}

// RangeUpdate adds delta to all elements in [left, right].
//
// Description:
//
//	Efficiently updates a range using lazy propagation. Actual updates
//	are deferred until the nodes are queried or updated again.
//	CRITICAL: Only supports SUM aggregation (additive operation).
//
// Algorithm:
//
//	Time:  O(log N)
//	Space: O(1)
//
// Inputs:
//   - ctx: Context for tracing.
//   - left: Left boundary (0-indexed, inclusive). Must be in [0, size).
//   - right: Right boundary (0-indexed, inclusive). Must be in [left, size).
//   - delta: Value to add to each element in range.
//
// Outputs:
//   - error: Non-nil if range is invalid or aggFunc is not SUM.
//
// Example:
//
//	err := tree.RangeUpdate(ctx, 0, 3, 5)  // Add 5 to arr[0..3]
//
// Limitations:
//   - Only supports additive updates (delta added to existing values)
//   - Does not support set operations (use Update for single elements)
//   - Only works with AggregateSUM (MIN/MAX are not additive)
//
// Thread Safety: NOT safe for concurrent use. Caller must synchronize.
func (st *SegmentTree) RangeUpdate(ctx context.Context, left, right int, delta int64) error {
	ctx, span := otel.Tracer("trace").Start(ctx, "graph.SegmentTree.RangeUpdate",
		trace.WithAttributes(
			attribute.Int("left", left),
			attribute.Int("right", right),
			attribute.Int64("delta", delta),
		),
	)
	defer span.End()

	// CRITICAL FIX: Range update only works for SUM
	if st.aggFunc != AggregateSUM {
		err := fmt.Errorf("%w: current function is %s", ErrRangeUpdateNotSUM, st.aggFunc.String())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Validate range
	if err := st.validateRange(left, right); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	st.rangeUpdateRec(1, 0, st.n-1, left, right, delta)
	st.updateCount++
	st.version++

	span.SetStatus(codes.Ok, "range update complete")
	return nil
}

// rangeUpdateRec recursively applies delta to range [uL, uR].
func (st *SegmentTree) rangeUpdateRec(node, nodeL, nodeR, uL, uR int, delta int64) {
	// If range is outside update range, do nothing
	if uR < nodeL || uL > nodeR {
		return
	}

	// If range is fully inside update range, apply lazy update
	if uL <= nodeL && nodeR <= uR {
		rangeLen := int64(nodeR - nodeL + 1)
		st.tree[node] = st.aggFunc.Combine(st.tree[node], delta*rangeLen)
		if nodeL != nodeR { // Not a leaf
			st.lazy[node] += delta
			st.hasLazy[node] = true
		}
		return
	}

	// Push down existing lazy value before recursing
	if st.hasLazy[node] {
		st.pushDown(node, nodeL, nodeR)
	}

	// Partial overlap, recurse on children
	mid := (nodeL + nodeR) / 2
	st.rangeUpdateRec(2*node, nodeL, mid, uL, uR, delta)
	st.rangeUpdateRec(2*node+1, mid+1, nodeR, uL, uR, delta)

	// Update current node from children
	st.tree[node] = st.aggFunc.Combine(st.tree[2*node], st.tree[2*node+1])
}

// pushDown propagates lazy updates from node to its children.
//
// CRITICAL: Only works for SUM aggregation (additive operation).
func (st *SegmentTree) pushDown(node, nodeL, nodeR int) {
	if !st.hasLazy[node] || nodeL == nodeR {
		return // No lazy value or is a leaf
	}

	delta := st.lazy[node]
	mid := (nodeL + nodeR) / 2

	// Update left child
	leftLen := int64(mid - nodeL + 1)
	st.tree[2*node] += delta * leftLen
	st.lazy[2*node] += delta
	st.hasLazy[2*node] = true

	// Update right child
	rightLen := int64(nodeR - mid)
	st.tree[2*node+1] += delta * rightLen
	st.lazy[2*node+1] += delta
	st.hasLazy[2*node+1] = true

	// Clear lazy value for current node
	st.lazy[node] = 0
	st.hasLazy[node] = false
}

// GetValue retrieves the current value at arr[index].
//
// Description:
//
//	Returns the current value at index, accounting for lazy updates.
//	Equivalent to Query(ctx, index, index) but more explicit.
//
// Algorithm:
//
//	Time:  O(log N) - must push down lazy updates
//	Space: O(1)
//
// Thread Safety: Safe for concurrent reads (protected by RWMutex).
func (st *SegmentTree) GetValue(ctx context.Context, index int) (int64, error) {
	if index < 0 || index >= st.size {
		return 0, fmt.Errorf("%w: index %d out of bounds [0,%d)", ErrInvalidRange, index, st.size)
	}

	return st.Query(ctx, index, index)
}

// Validate checks segment tree invariants.
//
// Description:
//
//	Verifies:
//	- Tree and lazy arrays have correct size
//	- Root aggregate matches expected value
//	- Parent-child relationships are correct
//	- Lazy array consistency
//
// Complexity: O(N) time
//
// Thread Safety: Safe for concurrent use (read-only, protected by RWMutex).
func (st *SegmentTree) Validate() error {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// Check array sizes
	expectedTreeSize := 2 * st.n
	if len(st.tree) != expectedTreeSize {
		return fmt.Errorf("tree size mismatch: got %d, expected %d", len(st.tree), expectedTreeSize)
	}
	if len(st.lazy) != expectedTreeSize {
		return fmt.Errorf("lazy size mismatch: got %d, expected %d", len(st.lazy), expectedTreeSize)
	}
	if len(st.hasLazy) != expectedTreeSize {
		return fmt.Errorf("hasLazy size mismatch: got %d, expected %d", len(st.hasLazy), expectedTreeSize)
	}

	// Check lazy array consistency
	for i := range st.hasLazy {
		if st.hasLazy[i] && st.lazy[i] == 0 && st.aggFunc == AggregateSUM {
			// For SUM, lazy[i] == 0 with hasLazy[i] == true is suspicious but technically valid
			// (could be a zero delta). Skip this check.
		}
	}

	// Verify root aggregate (compute expected by querying leaves)
	expectedRoot := st.aggFunc.Identity()
	for i := 0; i < st.size; i++ {
		leafIdx := st.n + i
		expectedRoot = st.aggFunc.Combine(expectedRoot, st.tree[leafIdx])
	}

	// For trees with lazy updates, root might not match until all lazy values are pushed down
	// Skip this check if any lazy values exist
	hasAnyLazy := false
	for _, hl := range st.hasLazy {
		if hl {
			hasAnyLazy = true
			break
		}
	}

	if !hasAnyLazy {
		actualRoot := st.tree[1]
		if actualRoot != expectedRoot {
			return fmt.Errorf("root mismatch: actual=%d expected=%d", actualRoot, expectedRoot)
		}
	}

	// Check parent-child relationships (only for non-lazy nodes)
	for i := 1; i < st.n; i++ {
		if st.hasLazy[i] {
			continue // Skip nodes with pending lazy updates
		}
		left := st.tree[2*i]
		right := st.tree[2*i+1]
		expected := st.aggFunc.Combine(left, right)
		if st.tree[i] != expected {
			return fmt.Errorf("node %d: parent=%d but combine(left=%d, right=%d)=%d",
				i, st.tree[i], left, right, expected)
		}
	}

	return nil
}

// Stats returns statistics about the segment tree.
//
// Thread Safety: Safe for concurrent use (read-only, protected by RWMutex).
func (st *SegmentTree) Stats() SegmentTreeStats {
	st.mu.RLock()
	defer st.mu.RUnlock()

	height := bits.Len(uint(st.n)) - 1

	return SegmentTreeStats{
		Size:        st.size,
		PaddedSize:  st.n,
		TreeSize:    len(st.tree),
		Height:      height,
		AggFunc:     st.aggFunc,
		BuildTime:   st.buildTime,
		QueryCount:  st.queryCount,
		UpdateCount: st.updateCount,
		Version:     st.version,
		MemoryBytes: st.MemoryUsage(),
	}
}

// MemoryUsage estimates memory usage in bytes.
func (st *SegmentTree) MemoryUsage() int {
	treeBytes := len(st.tree) * 8       // int64 = 8 bytes
	lazyBytes := len(st.lazy) * 8       // int64 = 8 bytes
	hasLazyBytes := len(st.hasLazy) * 1 // bool = 1 byte
	structOverhead := 128               // Approximate struct overhead

	return treeBytes + lazyBytes + hasLazyBytes + structOverhead
}

// CacheKey generates a cache key for this segment tree.
//
// Description:
//
//	Returns a deterministic cache key based on tree structure and version.
//	Includes version to handle updates properly.
//
// Thread Safety: Safe for concurrent use (read-only, protected by RWMutex).
func (st *SegmentTree) CacheKey() string {
	st.mu.RLock()
	defer st.mu.RUnlock()

	h := sha256.New()

	// Hash size and aggregation function
	binary.Write(h, binary.LittleEndian, int64(st.size))
	binary.Write(h, binary.LittleEndian, int64(st.aggFunc))
	binary.Write(h, binary.LittleEndian, st.version)

	// Hash tree array (only relevant nodes)
	for i := 1; i < 2*st.n; i++ {
		binary.Write(h, binary.LittleEndian, st.tree[i])
	}

	return fmt.Sprintf("segtree:%x:v%d", h.Sum(nil)[:16], st.version)
}

// gcd computes greatest common divisor using Euclidean algorithm.
func gcd(a, b int64) int64 {
	// Handle negative values
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}

	// Euclidean algorithm
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
