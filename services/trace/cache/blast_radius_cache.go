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
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/analysis"
	"golang.org/x/sync/singleflight"
)

// BlastRadiusCache provides LRU caching for blast radius analysis results.
//
// # Description
//
// Caches EnhancedBlastRadius results to avoid redundant analysis. Uses
// graph generation as cache key to automatically invalidate when the
// graph changes.
//
// # Cache Key Format
//
// Keys are computed as: SHA256(projectRoot + ":" + symbolID + ":" + graphGeneration)
// This ensures cache entries are invalidated when:
//   - The target symbol changes
//   - The graph is rebuilt
//
// # Thread Safety
//
// Safe for concurrent use. Uses sync.RWMutex for entry map and
// singleflight.Group for analysis deduplication.
type BlastRadiusCache struct {
	mu      sync.RWMutex
	entries map[string]*brCacheEntry
	lru     *list.List
	flight  singleflight.Group
	options BRCacheOptions

	// Stats
	hits       int64
	misses     int64
	evictions  int64
	computes   int64
	errorCount int64
}

// brCacheEntry represents a cached blast radius result.
type brCacheEntry struct {
	// Key is the cache key.
	Key string

	// Result is the cached analysis result.
	Result *analysis.EnhancedBlastRadius

	// SymbolID is the target symbol.
	SymbolID string

	// GraphGeneration is the graph version when computed.
	GraphGeneration uint64

	// ComputedAtMilli is when the result was computed.
	ComputedAtMilli int64

	// LastAccessMilli is when the entry was last accessed.
	LastAccessMilli int64

	// lruElement is the position in the LRU list.
	lruElement *list.Element
}

// BRCacheOptions configures BlastRadiusCache.
type BRCacheOptions struct {
	// MaxEntries is the maximum number of cached results.
	// Default: 1000
	MaxEntries int

	// MaxAge is the TTL for cached entries.
	// Default: 5 minutes
	MaxAge time.Duration

	// ComputeTimeout is the maximum time for a single analysis.
	// Default: 500ms
	ComputeTimeout time.Duration
}

// DefaultBRCacheOptions returns sensible defaults.
func DefaultBRCacheOptions() BRCacheOptions {
	return BRCacheOptions{
		MaxEntries:     1000,
		MaxAge:         5 * time.Minute,
		ComputeTimeout: 500 * time.Millisecond,
	}
}

// BRCacheOption is a functional option for configuring BlastRadiusCache.
type BRCacheOption func(*BRCacheOptions)

// WithBRMaxEntries sets the maximum number of cached entries.
func WithBRMaxEntries(n int) BRCacheOption {
	return func(o *BRCacheOptions) {
		if n > 0 {
			o.MaxEntries = n
		}
	}
}

// WithBRMaxAge sets the TTL for cached entries.
func WithBRMaxAge(d time.Duration) BRCacheOption {
	return func(o *BRCacheOptions) {
		if d > 0 {
			o.MaxAge = d
		}
	}
}

// WithBRComputeTimeout sets the analysis timeout.
func WithBRComputeTimeout(d time.Duration) BRCacheOption {
	return func(o *BRCacheOptions) {
		if d > 0 {
			o.ComputeTimeout = d
		}
	}
}

// NewBlastRadiusCache creates a new BlastRadiusCache.
func NewBlastRadiusCache(opts ...BRCacheOption) *BlastRadiusCache {
	options := DefaultBRCacheOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &BlastRadiusCache{
		entries: make(map[string]*brCacheEntry),
		lru:     list.New(),
		options: options,
	}
}

// AnalyzeFunc is the function signature for computing blast radius.
type AnalyzeFunc func(ctx context.Context, symbolID string) (*analysis.EnhancedBlastRadius, error)

// Get retrieves a cached blast radius result.
//
// # Inputs
//
//   - symbolID: The target symbol identifier.
//   - graphGen: The current graph generation.
//
// # Outputs
//
//   - *analysis.EnhancedBlastRadius: The cached result, or nil if not found.
//   - bool: True if the entry was found and valid.
func (c *BlastRadiusCache) Get(symbolID string, graphGen uint64) (*analysis.EnhancedBlastRadius, bool) {
	key := c.computeKey(symbolID, graphGen)

	c.mu.RLock()
	entry, ok := c.entries[key]
	if !ok {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Check TTL
	if c.isExpired(entry) {
		c.mu.RUnlock()
		c.remove(key)
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Update access time
	entry.LastAccessMilli = time.Now().UnixMilli()
	c.mu.RUnlock()

	// Update LRU
	c.updateLRU(entry)

	atomic.AddInt64(&c.hits, 1)
	return entry.Result, true
}

// GetOrCompute retrieves a cached result or computes a new one.
//
// # Description
//
// Uses singleflight to deduplicate concurrent computations for the same
// symbol. If multiple goroutines request the same symbol simultaneously,
// only one computation runs and all waiters receive the result.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - symbolID: The target symbol identifier.
//   - graphGen: The current graph generation.
//   - compute: Function to compute the blast radius if not cached.
//
// # Outputs
//
//   - *analysis.EnhancedBlastRadius: The result (cached or newly computed).
//   - error: Non-nil if computation failed.
func (c *BlastRadiusCache) GetOrCompute(
	ctx context.Context,
	symbolID string,
	graphGen uint64,
	compute AnalyzeFunc,
) (*analysis.EnhancedBlastRadius, error) {
	// Fast path: check cache
	if result, ok := c.Get(symbolID, graphGen); ok {
		return result, nil
	}

	key := c.computeKey(symbolID, graphGen)

	// Singleflight: deduplicate concurrent computations
	result, err, _ := c.flight.Do(key, func() (interface{}, error) {
		// Double-check cache (might have been populated while waiting)
		if result, ok := c.Get(symbolID, graphGen); ok {
			return result, nil
		}

		// Create timeout context
		computeCtx, cancel := context.WithTimeout(ctx, c.options.ComputeTimeout)
		defer cancel()

		// Compute
		result, err := compute(computeCtx, symbolID)
		if err != nil {
			atomic.AddInt64(&c.errorCount, 1)
			return nil, err
		}

		// Cache result
		c.put(key, symbolID, graphGen, result)
		atomic.AddInt64(&c.computes, 1)

		return result, nil
	})

	if err != nil {
		return nil, err
	}

	return result.(*analysis.EnhancedBlastRadius), nil
}

// put adds a result to the cache.
func (c *BlastRadiusCache) put(key, symbolID string, graphGen uint64, result *analysis.EnhancedBlastRadius) {
	now := time.Now().UnixMilli()
	entry := &brCacheEntry{
		Key:             key,
		Result:          result,
		SymbolID:        symbolID,
		GraphGeneration: graphGen,
		ComputedAtMilli: now,
		LastAccessMilli: now,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already cached
	if _, exists := c.entries[key]; exists {
		return
	}

	// Evict if needed
	c.evictIfNeededLocked()

	// Add to cache
	entry.lruElement = c.lru.PushFront(key)
	c.entries[key] = entry
}

// computeKey generates a cache key from symbol ID and graph generation.
func (c *BlastRadiusCache) computeKey(symbolID string, graphGen uint64) string {
	data := fmt.Sprintf("%s:%d", symbolID, graphGen)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:16]) // 32-char key (first 16 bytes)
}

// isExpired checks if an entry has exceeded its TTL.
func (c *BlastRadiusCache) isExpired(entry *brCacheEntry) bool {
	if c.options.MaxAge == 0 {
		return false
	}
	age := time.Since(time.UnixMilli(entry.ComputedAtMilli))
	return age > c.options.MaxAge
}

// updateLRU moves an entry to the front of the LRU list.
func (c *BlastRadiusCache) updateLRU(entry *brCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry.lruElement != nil {
		c.lru.MoveToFront(entry.lruElement)
	}
}

// remove removes an entry from the cache.
func (c *BlastRadiusCache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return
	}

	if entry.lruElement != nil {
		c.lru.Remove(entry.lruElement)
	}
	delete(c.entries, key)
}

// evictIfNeededLocked evicts LRU entries if at capacity (must hold lock).
func (c *BlastRadiusCache) evictIfNeededLocked() {
	for len(c.entries) >= c.options.MaxEntries {
		if !c.evictLRULocked() {
			break
		}
	}
}

// evictLRULocked evicts the least recently used entry (must hold lock).
func (c *BlastRadiusCache) evictLRULocked() bool {
	elem := c.lru.Back()
	if elem == nil {
		return false
	}

	key := elem.Value.(string)
	entry := c.entries[key]
	if entry != nil {
		c.lru.Remove(entry.lruElement)
		delete(c.entries, key)
		atomic.AddInt64(&c.evictions, 1)
		return true
	}
	return false
}

// Invalidate removes a specific entry.
func (c *BlastRadiusCache) Invalidate(symbolID string, graphGen uint64) {
	key := c.computeKey(symbolID, graphGen)
	c.remove(key)
}

// InvalidateAll removes all entries for a symbol (any generation).
func (c *BlastRadiusCache) InvalidateAll(symbolID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	toRemove := make([]string, 0)
	for key, entry := range c.entries {
		if entry.SymbolID == symbolID {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		entry := c.entries[key]
		if entry != nil && entry.lruElement != nil {
			c.lru.Remove(entry.lruElement)
		}
		delete(c.entries, key)
	}
}

// InvalidateByGeneration removes all entries for a specific graph generation.
func (c *BlastRadiusCache) InvalidateByGeneration(graphGen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	toRemove := make([]string, 0)
	for key, entry := range c.entries {
		if entry.GraphGeneration == graphGen {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		entry := c.entries[key]
		if entry != nil && entry.lruElement != nil {
			c.lru.Remove(entry.lruElement)
		}
		delete(c.entries, key)
	}
}

// Clear removes all entries from the cache.
func (c *BlastRadiusCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*brCacheEntry)
	c.lru.Init()
}

// BRCacheStats contains statistics about the cache.
type BRCacheStats struct {
	EntryCount int
	Hits       int64
	Misses     int64
	Evictions  int64
	Computes   int64
	ErrorCount int64
	MaxEntries int
	MaxAge     time.Duration
}

// HitRate returns the cache hit rate as a percentage.
func (s BRCacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}

// Stats returns current cache statistics.
func (c *BlastRadiusCache) Stats() BRCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return BRCacheStats{
		EntryCount: len(c.entries),
		Hits:       atomic.LoadInt64(&c.hits),
		Misses:     atomic.LoadInt64(&c.misses),
		Evictions:  atomic.LoadInt64(&c.evictions),
		Computes:   atomic.LoadInt64(&c.computes),
		ErrorCount: atomic.LoadInt64(&c.errorCount),
		MaxEntries: c.options.MaxEntries,
		MaxAge:     c.options.MaxAge,
	}
}

// Len returns the number of entries in the cache.
func (c *BlastRadiusCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
