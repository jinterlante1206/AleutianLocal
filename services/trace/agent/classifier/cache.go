// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// ClassificationCache caches classification results with LRU eviction.
//
// Description:
//
//	Provides a thread-safe LRU cache for classification results with TTL
//	expiration. Cache keys are computed from query + toolsHash to ensure
//	results are invalidated when available tools change.
//
// Thread Safety: This type is safe for concurrent use.
type ClassificationCache struct {
	mu      sync.RWMutex
	entries map[string]*list.Element
	lru     *list.List
	ttl     time.Duration
	maxSize int

	// Metrics
	hits   atomic.Int64
	misses atomic.Int64
}

// cacheEntry stores a cached classification result.
type cacheEntry struct {
	key       string
	result    *ClassificationResult
	expiresAt time.Time
}

// NewClassificationCache creates a cache with TTL and max size.
//
// Description:
//
//	Creates a new LRU cache for classification results. The cache uses
//	TTL-based expiration and LRU eviction when at capacity.
//
// Inputs:
//
//	ttl - How long cached results are valid. Must be > 0.
//	maxSize - Maximum number of entries before LRU eviction. Must be > 0.
//
// Outputs:
//
//	*ClassificationCache - Ready-to-use cache.
//
// Example:
//
//	cache := NewClassificationCache(10*time.Minute, 1000)
//	cache.Set("what tests exist?", toolsHash, result)
//	if cached, ok := cache.Get("what tests exist?", toolsHash); ok {
//	    // Use cached result
//	}
//
// Thread Safety: The returned cache is safe for concurrent use.
func NewClassificationCache(ttl time.Duration, maxSize int) *ClassificationCache {
	return &ClassificationCache{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// Get retrieves a cached result if valid (not expired).
//
// Description:
//
//	Looks up a cached classification result by query and toolsHash.
//	Returns nil if the entry doesn't exist, has expired, or the cache
//	is empty.
//
// Inputs:
//
//	query - The original query string.
//	toolsHash - Hash of available tool definitions for cache key.
//
// Outputs:
//
//	*ClassificationResult - The cached result with Cached=true, or nil.
//	bool - True if a valid cached result was found.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Get(query, toolsHash string) (*ClassificationResult, bool) {
	key := c.computeKey(query, toolsHash)

	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.entries[key]
	if !exists {
		c.misses.Add(1)
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		// Expired - remove lazily
		c.removeElement(elem)
		c.misses.Add(1)
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(elem)

	c.hits.Add(1)

	// Deep copy to prevent mutation of cached entry
	return deepCopyResult(entry.result), true
}

// Set stores a classification result, evicting LRU if at capacity.
//
// Description:
//
//	Stores or updates a classification result in the cache. If the cache
//	is at capacity, the least recently used entry is evicted first.
//
// Inputs:
//
//	query - The original query string.
//	toolsHash - Hash of available tool definitions for cache key.
//	result - The classification result to cache. Must not be nil.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Set(query, toolsHash string, result *ClassificationResult) {
	if result == nil {
		return
	}

	key := c.computeKey(query, toolsHash)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Deep copy the result to prevent mutation of cached entry
	resultCopy := deepCopyResult(result)
	resultCopy.Cached = false // Don't cache the Cached flag

	// Update existing entry
	if elem, exists := c.entries[key]; exists {
		entry := elem.Value.(*cacheEntry)
		entry.result = resultCopy
		entry.expiresAt = time.Now().Add(c.ttl)
		c.lru.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	for c.lru.Len() >= c.maxSize {
		c.evictOldest()
	}

	// Add new entry
	entry := &cacheEntry{
		key:       key,
		result:    resultCopy,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.lru.PushFront(entry)
	c.entries[key] = elem
}

// deepCopyResult creates a deep copy of a ClassificationResult.
// This ensures cached entries are not affected by mutations to returned results.
func deepCopyResult(src *ClassificationResult) *ClassificationResult {
	if src == nil {
		return nil
	}

	dst := &ClassificationResult{
		IsAnalytical: src.IsAnalytical,
		Tool:         src.Tool,
		Reasoning:    src.Reasoning,
		Confidence:   src.Confidence,
		Cached:       true,
		Duration:     src.Duration,
		FallbackUsed: src.FallbackUsed,
	}

	// Deep copy Parameters map
	if src.Parameters != nil {
		dst.Parameters = make(map[string]any, len(src.Parameters))
		for k, v := range src.Parameters {
			dst.Parameters[k] = v // Values are primitives from JSON
		}
	}

	// Deep copy SearchPatterns slice
	if src.SearchPatterns != nil {
		dst.SearchPatterns = make([]string, len(src.SearchPatterns))
		copy(dst.SearchPatterns, src.SearchPatterns)
	}

	// Deep copy ValidationWarnings slice
	if src.ValidationWarnings != nil {
		dst.ValidationWarnings = make([]string, len(src.ValidationWarnings))
		copy(dst.ValidationWarnings, src.ValidationWarnings)
	}

	return dst
}

// Delete removes a specific entry from the cache.
//
// Description:
//
//	Removes the cached result for the given query and toolsHash if it exists.
//
// Inputs:
//
//	query - The original query string.
//	toolsHash - Hash of available tool definitions.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Delete(query, toolsHash string) {
	key := c.computeKey(query, toolsHash)

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.entries[key]; exists {
		c.removeElement(elem)
	}
}

// Clear removes all entries from the cache.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*list.Element)
	c.lru = list.New()
}

// Size returns the current number of entries in the cache.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lru.Len()
}

// HitRate returns the cache hit rate (0.0-1.0).
//
// Description:
//
//	Calculates the ratio of cache hits to total lookups.
//	Returns 0 if no lookups have been performed.
//
// Outputs:
//
//	float64 - Hit rate between 0.0 and 1.0.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) HitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// Hits returns the total number of cache hits.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Hits() int64 {
	return c.hits.Load()
}

// Misses returns the total number of cache misses.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) Misses() int64 {
	return c.misses.Load()
}

// ResetMetrics resets the hit/miss counters to zero.
//
// Thread Safety: This method is safe for concurrent use.
func (c *ClassificationCache) ResetMetrics() {
	c.hits.Store(0)
	c.misses.Store(0)
}

// computeKey creates a cache key from query + tool definitions hash.
//
// Description:
//
//	Computes a SHA-256 hash of the query concatenated with the tools hash.
//	This ensures cache invalidation when available tools change.
func (c *ClassificationCache) computeKey(query, toolsHash string) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(toolsHash))
	return hex.EncodeToString(h.Sum(nil))
}

// evictOldest removes the least recently used entry.
// Must be called with lock held.
func (c *ClassificationCache) evictOldest() {
	elem := c.lru.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement removes an element from both map and list.
// Must be called with lock held.
func (c *ClassificationCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.entries, entry.key)
	c.lru.Remove(elem)
}

// ComputeToolsHash creates a stable hash of tool definitions.
//
// Description:
//
//	Computes a SHA-256 hash of tool names, sorted alphabetically, to create
//	a stable hash that changes when available tools change. This enables
//	cache invalidation when the tool set changes.
//
// Inputs:
//
//	toolNames - List of tool names.
//
// Outputs:
//
//	string - Hex-encoded hash of the tool names.
//
// Thread Safety: This function is safe for concurrent use.
func ComputeToolsHash(toolNames []string) string {
	h := sha256.New()
	for _, name := range toolNames {
		h.Write([]byte(name))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
