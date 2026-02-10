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
	"container/list"
	"sync"
	"sync/atomic"
)

// LRUCache is a thread-safe LRU cache with generics support.
//
// Description:
//
//	Implements a fixed-size cache that evicts the least recently used
//	entries when capacity is reached. Uses container/list for O(1)
//	access and eviction.
//
// Thread Safety: All methods are safe for concurrent use.
//
// Performance:
//
//	| Operation | Complexity |
//	|-----------|------------|
//	| Get       | O(1)       |
//	| Set       | O(1)       |
//	| Delete    | O(1)       |
//	| Purge     | O(n)       |
type LRUCache[K comparable, V any] struct {
	mu       sync.RWMutex
	capacity int
	items    map[K]*list.Element
	order    *list.List // Front = most recent, Back = least recent

	// Stats (atomic for lock-free reads)
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64 // GR-10 Review (O-1): Track evictions
}

// lruEntry holds the key-value pair in the list.
type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRUCache creates a new LRU cache with the given capacity.
//
// Description:
//
//	Creates a fixed-size LRU cache. When the cache is full, the least
//	recently accessed entry is evicted to make room for new entries.
//
// Inputs:
//   - capacity: Maximum number of entries. Must be > 0.
//
// Outputs:
//   - *LRUCache[K, V]: The cache. Never nil.
//
// Example:
//
//	cache := NewLRUCache[string, *QueryResult](1000)
//	cache.Set("key", result)
//	if val, ok := cache.Get("key"); ok {
//	    // use val
//	}
//
// Thread Safety: The returned cache is safe for concurrent use.
func NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V] {
	if capacity <= 0 {
		capacity = 100 // Sensible default
	}
	return &LRUCache[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
	}
}

// Get retrieves a value from the cache.
//
// Description:
//
//	Returns the value associated with the key and moves it to the
//	front of the LRU list (most recently used).
//
// Inputs:
//   - key: The key to look up.
//
// Outputs:
//   - V: The value (zero value if not found).
//   - bool: True if the key was found.
//
// Thread Safety: Safe for concurrent use.
func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		c.hits.Add(1)
		return elem.Value.(*lruEntry[K, V]).value, true
	}

	c.misses.Add(1)
	var zero V
	return zero, false
}

// Set adds or updates a value in the cache.
//
// Description:
//
//	Adds the key-value pair to the cache. If the key exists, updates
//	the value and moves it to the front. If the cache is full, evicts
//	the least recently used entry.
//
// Inputs:
//   - key: The key to store.
//   - value: The value to associate with the key.
//
// Thread Safety: Safe for concurrent use.
func (c *LRUCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*lruEntry[K, V]).value = value
		return
	}

	// Evict if at capacity
	if c.order.Len() >= c.capacity {
		c.evictOldest()
	}

	// Add new entry at front
	entry := &lruEntry[K, V]{key: key, value: value}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
}

// Delete removes a key from the cache.
//
// Inputs:
//   - key: The key to remove.
//
// Outputs:
//   - bool: True if the key was found and removed.
//
// Thread Safety: Safe for concurrent use.
func (c *LRUCache[K, V]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
		return true
	}
	return false
}

// Purge clears all entries from the cache.
//
// Description:
//
//	Removes all entries and resets hit/miss/eviction counters.
//
// Thread Safety: Safe for concurrent use.
func (c *LRUCache[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[K]*list.Element, c.capacity)
	c.order.Init()
	c.hits.Store(0)
	c.misses.Store(0)
	c.evictions.Store(0) // GR-10 Review (O-1): Reset eviction counter
}

// Len returns the number of entries in the cache.
//
// Thread Safety: Safe for concurrent use.
func (c *LRUCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Stats returns cache hit/miss/eviction statistics.
//
// Outputs:
//   - hits: Number of cache hits since creation or last purge.
//   - misses: Number of cache misses since creation or last purge.
//
// Thread Safety: Safe for concurrent use (lock-free).
func (c *LRUCache[K, V]) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// Evictions returns the number of cache evictions since creation or last purge.
//
// Description:
//
//	GR-10 Review (O-1): Tracks evictions for monitoring cache effectiveness.
//	High eviction count relative to cache size indicates capacity may be too small.
//
// Outputs:
//   - evictions: Number of entries evicted due to capacity limits.
//
// Thread Safety: Safe for concurrent use (lock-free).
func (c *LRUCache[K, V]) Evictions() int64 {
	return c.evictions.Load()
}

// evictOldest removes the least recently used entry.
// Caller must hold the write lock.
func (c *LRUCache[K, V]) evictOldest() {
	if elem := c.order.Back(); elem != nil {
		c.removeElement(elem)
		c.evictions.Add(1) // GR-10 Review (O-1): Track evictions
	}
}

// removeElement removes an element from both the list and map.
// Caller must hold the write lock.
func (c *LRUCache[K, V]) removeElement(elem *list.Element) {
	c.order.Remove(elem)
	entry := elem.Value.(*lruEntry[K, V])
	delete(c.items, entry.key)
}
