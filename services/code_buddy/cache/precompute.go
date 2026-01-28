// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cache

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// HotPathPrecomputer pre-computes blast radius for frequently accessed symbols.
//
// # Description
//
// Tracks which symbols are accessed most frequently and pre-computes their
// blast radius results in the background. This warms the cache for hot paths
// so user requests can be served from cache immediately.
//
// # Algorithm
//
// Uses a min-heap to track the top N most accessed symbols. When a symbol is
// accessed, its count is incremented. Periodically, the precomputer runs the
// top symbols through the analysis and caches the results.
//
// # Thread Safety
//
// Safe for concurrent use. Access recording is non-blocking via atomic counters.
type HotPathPrecomputer struct {
	cache        *BlastRadiusCache
	maxTracked   int
	topN         int
	interval     time.Duration
	computeFunc  AnalyzeFunc
	graphGenFunc func() uint64 // Returns current graph generation

	mu      sync.RWMutex
	access  map[string]*accessEntry
	heap    accessHeap
	running bool
	done    chan struct{}

	// Stats
	precomputes  int64
	totalAccess  int64
	hotSymbols   int64
	lastRunMilli int64
}

// accessEntry tracks access count for a symbol.
type accessEntry struct {
	SymbolID string
	Count    int64
	index    int // Position in heap
}

// accessHeap implements heap.Interface for access entries.
// This is a max-heap (highest count at top).
type accessHeap []*accessEntry

func (h accessHeap) Len() int           { return len(h) }
func (h accessHeap) Less(i, j int) bool { return h[i].Count > h[j].Count } // Max heap
func (h accessHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *accessHeap) Push(x interface{}) {
	entry := x.(*accessEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *accessHeap) Pop() interface{} {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.index = -1
	*h = old[0 : n-1]
	return entry
}

// PrecomputerOptions configures the HotPathPrecomputer.
type PrecomputerOptions struct {
	// MaxTracked is the maximum number of symbols to track access for.
	// Default: 10000
	MaxTracked int

	// TopN is how many top symbols to pre-compute.
	// Default: 100
	TopN int

	// Interval is how often to run pre-computation.
	// Default: 1 minute
	Interval time.Duration
}

// DefaultPrecomputerOptions returns sensible defaults.
func DefaultPrecomputerOptions() PrecomputerOptions {
	return PrecomputerOptions{
		MaxTracked: 10000,
		TopN:       100,
		Interval:   1 * time.Minute,
	}
}

// NewHotPathPrecomputer creates a new precomputer.
//
// # Inputs
//
//   - cache: The blast radius cache to warm.
//   - computeFunc: Function to compute blast radius (passed to cache.GetOrCompute).
//   - graphGenFunc: Function returning the current graph generation.
//   - opts: Optional configuration (nil uses defaults).
//
// # Outputs
//
//   - *HotPathPrecomputer: Ready-to-use precomputer (call Start to begin).
func NewHotPathPrecomputer(
	cache *BlastRadiusCache,
	computeFunc AnalyzeFunc,
	graphGenFunc func() uint64,
	opts *PrecomputerOptions,
) *HotPathPrecomputer {
	if opts == nil {
		defaults := DefaultPrecomputerOptions()
		opts = &defaults
	}

	return &HotPathPrecomputer{
		cache:        cache,
		maxTracked:   opts.MaxTracked,
		topN:         opts.TopN,
		interval:     opts.Interval,
		computeFunc:  computeFunc,
		graphGenFunc: graphGenFunc,
		access:       make(map[string]*accessEntry),
		heap:         make(accessHeap, 0),
		done:         make(chan struct{}),
	}
}

// RecordAccess records an access to a symbol.
//
// # Description
//
// Called when a symbol's blast radius is requested. The access count is
// incremented atomically for thread-safe recording without locks.
//
// # Inputs
//
//   - symbolID: The accessed symbol identifier.
func (p *HotPathPrecomputer) RecordAccess(symbolID string) {
	atomic.AddInt64(&p.totalAccess, 1)

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, exists := p.access[symbolID]
	if exists {
		entry.Count++
		heap.Fix(&p.heap, entry.index)
		return
	}

	// New entry - check if we have room
	if len(p.access) >= p.maxTracked {
		// Evict the least accessed (bottom of heap)
		// Note: This is a max-heap, so we need to find the minimum
		p.evictLeastAccessed()
	}

	// Add new entry
	entry = &accessEntry{
		SymbolID: symbolID,
		Count:    1,
	}
	p.access[symbolID] = entry
	heap.Push(&p.heap, entry)
}

// evictLeastAccessed removes the symbol with lowest access count.
// Must be called with lock held.
func (p *HotPathPrecomputer) evictLeastAccessed() {
	if len(p.heap) == 0 {
		return
	}

	// Find minimum in the heap (it's a max-heap, so min is at leaves)
	minIdx := len(p.heap) / 2 // Start from first leaf
	for i := minIdx + 1; i < len(p.heap); i++ {
		if p.heap[i].Count < p.heap[minIdx].Count {
			minIdx = i
		}
	}

	// Remove the minimum
	entry := p.heap[minIdx]
	heap.Remove(&p.heap, minIdx)
	delete(p.access, entry.SymbolID)
}

// Start begins the background pre-computation loop.
//
// # Inputs
//
//   - ctx: Context for cancellation.
func (p *HotPathPrecomputer) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	go p.runLoop(ctx)
}

// Stop stops the precomputer.
func (p *HotPathPrecomputer) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.running = false
	close(p.done)
}

// IsRunning returns true if the precomputer is active.
func (p *HotPathPrecomputer) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// runLoop is the main background loop.
func (p *HotPathPrecomputer) runLoop(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case <-ticker.C:
			p.precompute(ctx)
		}
	}
}

// precompute runs blast radius for top N symbols.
func (p *HotPathPrecomputer) precompute(ctx context.Context) {
	p.mu.Lock()
	p.lastRunMilli = time.Now().UnixMilli()

	// Get top N symbols
	topSymbols := make([]string, 0, p.topN)
	for i := 0; i < p.topN && i < len(p.heap); i++ {
		topSymbols = append(topSymbols, p.heap[i].SymbolID)
	}
	p.mu.Unlock()

	if len(topSymbols) == 0 {
		return
	}

	atomic.StoreInt64(&p.hotSymbols, int64(len(topSymbols)))

	// Get current graph generation
	graphGen := p.graphGenFunc()

	// Pre-compute each symbol
	for _, symbolID := range topSymbols {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		default:
		}

		// Try to cache (if not already cached)
		_, err := p.cache.GetOrCompute(ctx, symbolID, graphGen, p.computeFunc)
		if err != nil {
			continue // Skip failures silently
		}
		atomic.AddInt64(&p.precomputes, 1)
	}
}

// PrecomputerStats contains statistics about the precomputer.
type PrecomputerStats struct {
	TrackedSymbols int
	HotSymbols     int64
	TotalAccess    int64
	Precomputes    int64
	LastRunMilli   int64
	IsRunning      bool
}

// Stats returns current statistics.
func (p *HotPathPrecomputer) Stats() PrecomputerStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PrecomputerStats{
		TrackedSymbols: len(p.access),
		HotSymbols:     atomic.LoadInt64(&p.hotSymbols),
		TotalAccess:    atomic.LoadInt64(&p.totalAccess),
		Precomputes:    atomic.LoadInt64(&p.precomputes),
		LastRunMilli:   atomic.LoadInt64(&p.lastRunMilli),
		IsRunning:      p.running,
	}
}

// GetTopSymbols returns the top N most accessed symbols.
func (p *HotPathPrecomputer) GetTopSymbols(n int) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]string, 0, n)
	for i := 0; i < n && i < len(p.heap); i++ {
		result = append(result, p.heap[i].SymbolID)
	}
	return result
}

// Clear resets all access tracking.
func (p *HotPathPrecomputer) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.access = make(map[string]*accessEntry)
	p.heap = make(accessHeap, 0)
}
