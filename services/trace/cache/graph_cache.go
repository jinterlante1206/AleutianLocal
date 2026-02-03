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
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
	"golang.org/x/sync/singleflight"
)

// BuildFunc is the function signature for building a graph.
type BuildFunc func(ctx context.Context, projectRoot string) (*graph.Graph, *manifest.Manifest, error)

// RefreshFunc is the function signature for incrementally updating a graph.
//
// Description:
//
//	Called during Refresh to handle the incremental update logic.
//	The function receives the current graph/manifest and should return
//	updated versions based on file system changes.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	projectRoot - Absolute path to the project root.
//	currentGraph - The current graph (will be cloned by caller).
//	currentManifest - The current manifest for change detection.
//
// Outputs:
//
//	*graph.Graph - The updated graph (may be same if no changes).
//	*manifest.Manifest - The new manifest reflecting current state.
//	error - Non-nil if refresh failed.
//
// Behavior:
//
//	The RefreshFunc should:
//	1. Scan for file changes (added/modified/deleted)
//	2. Clone the graph if changes exist
//	3. Remove deleted files from clone
//	4. Re-parse and merge modified/added files
//	5. Return the updated graph and new manifest
type RefreshFunc func(ctx context.Context, projectRoot string, currentGraph *graph.Graph, currentManifest *manifest.Manifest) (*graph.Graph, *manifest.Manifest, error)

// GraphCache provides LRU caching for code graphs with reference counting.
//
// Thread Safety:
//
//	GraphCache is safe for concurrent use. Uses RWMutex for the entry map
//	and per-entry mutexes for refresh operations.
type GraphCache struct {
	mu           sync.RWMutex
	entries      map[string]*CacheEntry
	lru          *list.List
	flight       singleflight.Group
	failedBuilds map[string]*failedBuild
	options      CacheOptions

	// Stats
	hits            int64
	misses          int64
	evictions       int64
	buildCount      int64
	refreshCount    int64
	errorCount      int64
	memoryEvictions int64 // Evictions due to memory pressure
}

// NewGraphCache creates a new GraphCache with the given options.
func NewGraphCache(opts ...CacheOption) *GraphCache {
	options := DefaultCacheOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &GraphCache{
		entries:      make(map[string]*CacheEntry),
		lru:          list.New(),
		failedBuilds: make(map[string]*failedBuild),
		options:      options,
	}
}

// Get retrieves a cached entry by project root.
//
// Returns the entry, a release function, and whether the entry was found.
// The release function MUST be called when done using the entry.
//
// If the entry is stale or expired, returns false.
func (c *GraphCache) Get(projectRoot string) (*CacheEntry, func(), bool) {
	graphID := GenerateGraphID(projectRoot)

	c.mu.RLock()
	entry, ok := c.entries[graphID]
	if !ok {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, nil, false
	}

	// Check if stale
	if entry.stale {
		c.mu.RUnlock()
		atomic.AddInt64(&c.misses, 1)
		return nil, nil, false
	}

	// Check TTL
	if c.isExpired(entry) {
		c.mu.RUnlock()
		c.removeExpired(graphID)
		atomic.AddInt64(&c.misses, 1)
		return nil, nil, false
	}

	// Acquire reference before releasing lock
	entry.Acquire()
	entry.LastAccessMilli = time.Now().UnixMilli()
	c.mu.RUnlock()

	// Update LRU (separate lock operation)
	c.updateLRU(entry)

	atomic.AddInt64(&c.hits, 1)

	release := func() {
		entry.Release()
		// Check if should be removed (stale + released)
		if entry.stale && !entry.InUse() {
			c.tryRemove(graphID)
		}
	}

	return entry, release, true
}

// GetOrBuild retrieves a cached entry or builds a new one.
//
// Uses singleflight to deduplicate concurrent builds for the same project.
// Build errors are cached for ErrorCacheTTL to prevent retry storms.
//
// The release function MUST be called when done using the entry.
func (c *GraphCache) GetOrBuild(ctx context.Context, projectRoot string, build BuildFunc) (*CacheEntry, func(), error) {
	graphID := GenerateGraphID(projectRoot)

	// Check cache first (fast path)
	if entry, release, ok := c.Get(projectRoot); ok {
		return entry, release, nil
	}

	// Check for cached error
	if fb := c.getCachedError(graphID); fb != nil {
		return nil, nil, &ErrBuildFailed{
			Err:      fb.err,
			FailedAt: fb.failedAt,
			RetryAt:  fb.retryAt,
		}
	}

	// Singleflight: only one build per graphID
	result, err, _ := c.flight.Do(graphID, func() (interface{}, error) {
		entry, err := c.buildAndCache(ctx, projectRoot, graphID, build)
		if err != nil {
			c.cacheError(graphID, err)
			atomic.AddInt64(&c.errorCount, 1)
			return nil, err
		}
		return entry, nil
	})

	if err != nil {
		return nil, nil, err
	}

	entry := result.(*CacheEntry)
	entry.Acquire()

	release := func() {
		entry.Release()
		if entry.stale && !entry.InUse() {
			c.tryRemove(graphID)
		}
	}

	return entry, release, nil
}

// buildAndCache builds a graph and adds it to the cache.
func (c *GraphCache) buildAndCache(ctx context.Context, projectRoot, graphID string, build BuildFunc) (*CacheEntry, error) {
	g, m, err := build(ctx, projectRoot)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	entry := &CacheEntry{
		GraphID:         graphID,
		ProjectRoot:     projectRoot,
		Graph:           g,
		Manifest:        m,
		BuiltAtMilli:    now,
		LastAccessMilli: now,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if entry was added while we were building
	if existing, ok := c.entries[graphID]; ok {
		return existing, nil
	}

	// Evict if needed
	c.evictIfNeeded()

	// Add to cache
	entry.lruElement = c.lru.PushFront(graphID)
	c.entries[graphID] = entry
	atomic.AddInt64(&c.buildCount, 1)

	return entry, nil
}

// Invalidate removes an entry from the cache.
//
// Returns ErrCacheEntryInUse if the entry has active references.
// Use ForceInvalidate to mark the entry as stale instead.
func (c *GraphCache) Invalidate(projectRoot string) error {
	graphID := GenerateGraphID(projectRoot)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[graphID]
	if !ok {
		return nil // Already gone
	}

	if entry.InUse() {
		return ErrCacheEntryInUse
	}

	c.removeEntryLocked(graphID, entry)
	return nil
}

// ForceInvalidate marks an entry as stale.
//
// The entry will be removed when all references are released.
// Stale entries are not returned by Get().
func (c *GraphCache) ForceInvalidate(projectRoot string) {
	graphID := GenerateGraphID(projectRoot)

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[graphID]; ok {
		entry.stale = true
	}
}

// Stats returns current cache statistics.
func (c *GraphCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Calculate estimated memory
	var totalBytes int64
	for _, entry := range c.entries {
		totalBytes += entry.EstimatedMemoryBytes()
	}

	return CacheStats{
		EntryCount:        len(c.entries),
		Hits:              atomic.LoadInt64(&c.hits),
		Misses:            atomic.LoadInt64(&c.misses),
		Evictions:         atomic.LoadInt64(&c.evictions),
		MemoryEvictions:   atomic.LoadInt64(&c.memoryEvictions),
		BuildCount:        atomic.LoadInt64(&c.buildCount),
		RefreshCount:      atomic.LoadInt64(&c.refreshCount),
		ErrorCount:        atomic.LoadInt64(&c.errorCount),
		MaxEntries:        c.options.MaxEntries,
		MaxAge:            c.options.MaxAge,
		MaxMemoryMB:       c.options.MaxMemoryMB,
		EstimatedMemoryMB: int(totalBytes / (1024 * 1024)),
	}
}

// Clear removes all entries from the cache.
//
// Entries with active references are marked as stale.
func (c *GraphCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for graphID, entry := range c.entries {
		if entry.InUse() {
			entry.stale = true
		} else {
			c.removeEntryLocked(graphID, entry)
		}
	}
}

// isExpired checks if an entry has exceeded its TTL.
func (c *GraphCache) isExpired(entry *CacheEntry) bool {
	if c.options.MaxAge == 0 {
		return false
	}
	age := time.Since(time.UnixMilli(entry.BuiltAtMilli))
	return age > c.options.MaxAge
}

// updateLRU moves an entry to the front of the LRU list.
func (c *GraphCache) updateLRU(entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry.lruElement != nil {
		c.lru.MoveToFront(entry.lruElement)
	}
}

// removeExpired removes an expired entry from the cache.
func (c *GraphCache) removeExpired(graphID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[graphID]
	if !ok {
		return
	}

	// Don't remove if in use
	if entry.InUse() {
		entry.stale = true
		return
	}

	c.removeEntryLocked(graphID, entry)
}

// tryRemove attempts to remove an entry if it's safe to do so.
func (c *GraphCache) tryRemove(graphID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[graphID]
	if !ok {
		return
	}

	if !entry.InUse() {
		c.removeEntryLocked(graphID, entry)
	}
}

// removeEntryLocked removes an entry (must hold write lock).
func (c *GraphCache) removeEntryLocked(graphID string, entry *CacheEntry) {
	if entry.lruElement != nil {
		c.lru.Remove(entry.lruElement)
	}
	delete(c.entries, graphID)
}

// evictIfNeeded evicts entries if cache is at capacity or over memory limit.
//
// Description:
//
//	Evicts LRU entries to maintain both entry count and memory limits.
//	Only evicts entries with refCount == 0. Entries currently in use
//	are protected from eviction.
//
// Inputs:
//
//	None. Operates on cache state.
//
// Outputs:
//
//	None. Modifies cache state in place.
//
// Behavior:
//
//  1. First evicts for entry count (MaxEntries)
//  2. Then evicts for memory pressure (MaxMemoryMB)
//  3. Tracks memory evictions separately from count evictions
//
// Limitations:
//
//	If all entries are in use, the cache may temporarily exceed limits.
//	Memory estimation is approximate (based on node/edge counts).
//
// Assumptions:
//
//	Caller holds the write lock on c.mu.
//
// Thread Safety:
//
//	NOT safe for concurrent use. Caller must hold write lock.
func (c *GraphCache) evictIfNeeded() {
	// Evict for entry count limit
	for len(c.entries) >= c.options.MaxEntries {
		if !c.evictLRUEntry(false) {
			break // All entries are in use
		}
	}

	// Evict for memory limit (if configured)
	if c.options.MaxMemoryMB > 0 {
		maxBytes := int64(c.options.MaxMemoryMB) * 1024 * 1024
		for c.estimatedMemoryBytesLocked() > maxBytes {
			if !c.evictLRUEntry(true) {
				break // All entries are in use
			}
		}
	}
}

// evictLRUEntry evicts the least recently used entry that is not in use.
//
// Description:
//
//	Scans the LRU list from back (oldest) to front (newest) and evicts
//	the first entry that has no active references. Increments eviction
//	counters appropriately.
//
// Inputs:
//
//	isMemoryEviction - If true, also increments the memoryEvictions counter.
//
// Outputs:
//
//	bool - True if an entry was evicted, false if all entries are in use.
//
// Limitations:
//
//	Returns false if all entries have active references, which means
//	the cache cannot be reduced.
//
// Assumptions:
//
//	Caller holds the write lock on c.mu.
//
// Thread Safety:
//
//	NOT safe for concurrent use. Caller must hold write lock.
func (c *GraphCache) evictLRUEntry(isMemoryEviction bool) bool {
	for e := c.lru.Back(); e != nil; e = e.Prev() {
		graphID := e.Value.(string)
		entry := c.entries[graphID]
		if entry != nil && !entry.InUse() {
			c.removeEntryLocked(graphID, entry)
			atomic.AddInt64(&c.evictions, 1)
			if isMemoryEviction {
				atomic.AddInt64(&c.memoryEvictions, 1)
			}
			return true
		}
	}
	return false
}

// estimatedMemoryBytesLocked returns the total estimated memory usage.
//
// Description:
//
//	Sums the estimated memory usage of all cache entries. Uses heuristic
//	estimates based on node/edge/file counts.
//
// Inputs:
//
//	None. Reads from cache state.
//
// Outputs:
//
//	int64 - Estimated total memory in bytes.
//
// Limitations:
//
//	Memory estimates are approximate. Actual memory usage may differ
//	due to Go's memory allocator overhead, pointer sizes, and alignment.
//
// Assumptions:
//
//	Caller holds at least a read lock on c.mu.
//
// Thread Safety:
//
//	NOT safe for concurrent use. Caller must hold at least read lock.
func (c *GraphCache) estimatedMemoryBytesLocked() int64 {
	var total int64
	for _, entry := range c.entries {
		total += entry.EstimatedMemoryBytes()
	}
	return total
}

// getCachedError returns a cached build error if one exists and hasn't expired.
func (c *GraphCache) getCachedError(graphID string) *failedBuild {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fb, ok := c.failedBuilds[graphID]
	if !ok {
		return nil
	}

	// Check if error has expired
	if time.Now().After(fb.retryAt) {
		// Clean up in a separate goroutine to avoid lock escalation
		go c.clearCachedError(graphID)
		return nil
	}

	return fb
}

// cacheError stores a build error.
func (c *GraphCache) cacheError(graphID string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failedBuilds[graphID] = &failedBuild{
		err:      err,
		failedAt: time.Now(),
		retryAt:  time.Now().Add(c.options.ErrorCacheTTL),
	}
}

// clearCachedError removes a cached error.
func (c *GraphCache) clearCachedError(graphID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.failedBuilds, graphID)
}

// Refresh performs a copy-on-write incremental update of a cached entry.
//
// Description:
//
//	Uses the provided RefreshFunc to detect and apply changes to the
//	cached graph. The update is performed atomically using copy-on-write:
//	concurrent readers see either the old or new state, never partial.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	projectRoot - Absolute path to the project root.
//	refresh - Function that performs the incremental update.
//
// Outputs:
//
//	error - Non-nil if the entry doesn't exist or refresh failed.
//
// Errors:
//
//	ErrEntryNotFound - No cached entry for this project
//	Other errors from the RefreshFunc
//
// Behavior:
//
//  1. Acquires the entry and its refresh mutex
//  2. Calls RefreshFunc with current graph/manifest
//  3. If no changes, returns immediately
//  4. Creates new entry with updated graph/manifest
//  5. Atomically swaps the entry in the cache
//  6. Marks old entry as stale
//
// Thread Safety:
//
//	Safe for concurrent use. Concurrent readers see consistent state.
//	Only one Refresh can run at a time per entry (protected by entry mutex).
func (c *GraphCache) Refresh(ctx context.Context, projectRoot string, refresh RefreshFunc) error {
	graphID := GenerateGraphID(projectRoot)

	// Get existing entry directly (not using Get() to avoid the release callback issue)
	c.mu.RLock()
	entry, ok := c.entries[graphID]
	if !ok || entry.stale || c.isExpired(entry) {
		c.mu.RUnlock()
		return ErrEntryNotFound
	}
	entry.Acquire()
	c.mu.RUnlock()

	// Acquire entry's refresh lock (prevents concurrent refreshes)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	defer entry.Release()

	// Check context
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Call refresh function
	newGraph, newManifest, err := refresh(ctx, projectRoot, entry.Graph, entry.Manifest)
	if err != nil {
		return err
	}

	// If same graph/manifest returned, no changes needed
	if newGraph == entry.Graph && newManifest == entry.Manifest {
		return nil
	}

	now := time.Now().UnixMilli()

	// Create new entry
	newEntry := &CacheEntry{
		GraphID:         entry.GraphID,
		ProjectRoot:     projectRoot,
		Graph:           newGraph,
		Manifest:        newManifest,
		BuiltAtMilli:    now,
		LastAccessMilli: now,
	}

	// Atomic swap
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if entry was replaced while we were refreshing
	currentEntry, exists := c.entries[graphID]
	if !exists || currentEntry != entry {
		// Entry was replaced/removed, our update is stale
		return nil
	}

	// Transfer LRU element to new entry
	newEntry.lruElement = entry.lruElement

	// Clear old entry's lruElement so it doesn't affect LRU list
	entry.lruElement = nil

	// Swap entry (don't mark old as stale since we're replacing it)
	c.entries[graphID] = newEntry
	atomic.AddInt64(&c.refreshCount, 1)

	return nil
}
