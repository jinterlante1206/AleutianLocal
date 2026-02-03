// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"sync"
	"time"
)

// CacheConfig configures the summary cache behavior.
type CacheConfig struct {
	// StaleReadEnabled allows returning stale summaries on failure.
	StaleReadEnabled bool `json:"stale_read_enabled"`

	// StaleTTL is how long to keep stale entries readable.
	StaleTTL time.Duration `json:"stale_ttl"`

	// MaxEntries is the maximum number of entries to cache.
	// 0 means unlimited.
	MaxEntries int `json:"max_entries"`

	// FreshTTL is how long summaries are considered fresh.
	FreshTTL time.Duration `json:"fresh_ttl"`
}

// DefaultCacheConfig returns sensible defaults for the cache.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		StaleReadEnabled: true,
		StaleTTL:         24 * time.Hour,
		MaxEntries:       10000,
		FreshTTL:         1 * time.Hour,
	}
}

// SummaryCache provides in-memory caching for summaries.
//
// This implementation uses an in-memory map. For production,
// this should be backed by Weaviate for vector similarity search.
//
// Thread Safety: Safe for concurrent use.
type SummaryCache struct {
	config CacheConfig

	entries map[string]*cacheEntry
	mu      sync.RWMutex

	// Stats
	hits      int64
	misses    int64
	staleHits int64
	evictions int64
}

type cacheEntry struct {
	summary    *Summary
	createdAt  time.Time
	accessedAt time.Time
	stale      bool
}

// NewSummaryCache creates a new summary cache.
//
// Inputs:
//   - config: Cache configuration.
//
// Outputs:
//   - *SummaryCache: A new cache instance.
func NewSummaryCache(config CacheConfig) *SummaryCache {
	return &SummaryCache{
		config:  config,
		entries: make(map[string]*cacheEntry),
	}
}

// Get retrieves a fresh summary from the cache.
//
// Inputs:
//   - id: The summary ID.
//
// Outputs:
//   - *Summary: The summary if found and fresh.
//   - bool: True if found.
//
// Returns false if the summary is not found or is stale.
func (c *SummaryCache) Get(id string) (*Summary, bool) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	if !ok {
		c.mu.RUnlock()
		c.recordMiss()
		return nil, false
	}

	// Check if fresh while holding lock
	stale := entry.stale
	summary := entry.summary
	fresh := summary.IsFresh(c.config.FreshTTL)
	c.mu.RUnlock()

	if stale || !fresh {
		c.recordMiss()
		return nil, false
	}

	c.recordHit()
	c.touchEntry(id)
	return summary, true
}

// GetStale retrieves a summary, allowing stale entries.
//
// Inputs:
//   - id: The summary ID.
//
// Outputs:
//   - *Summary: The summary if found (even if stale).
//   - bool: True if found.
//   - bool: True if the entry is stale.
//
// Use this for graceful degradation when fresh data is unavailable.
func (c *SummaryCache) GetStale(id string) (*Summary, bool, bool) {
	if !c.config.StaleReadEnabled {
		s, ok := c.Get(id)
		return s, ok, false
	}

	c.mu.RLock()
	entry, ok := c.entries[id]
	if !ok {
		c.mu.RUnlock()
		c.recordMiss()
		return nil, false, false
	}

	// Extract fields while holding lock
	createdAt := entry.createdAt
	stale := entry.stale
	summary := entry.summary
	fresh := summary.IsFresh(c.config.FreshTTL)
	c.mu.RUnlock()

	// Check if within stale TTL
	if time.Since(createdAt) > c.config.StaleTTL {
		c.recordMiss()
		return nil, false, false
	}

	isStale := stale || !fresh
	if isStale {
		c.recordStaleHit()
	} else {
		c.recordHit()
	}

	c.touchEntry(id)
	return summary, true, isStale
}

// Set stores a summary in the cache.
//
// Inputs:
//   - summary: The summary to cache.
//
// Outputs:
//   - error: Non-nil if storage fails.
func (c *SummaryCache) Set(summary *Summary) error {
	if summary == nil {
		return ErrInvalidBudget // reuse existing error for nil input
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if c.config.MaxEntries > 0 && len(c.entries) >= c.config.MaxEntries {
		c.evictOldest()
	}

	now := time.Now()
	c.entries[summary.ID] = &cacheEntry{
		summary:    summary,
		createdAt:  now,
		accessedAt: now,
		stale:      false,
	}

	return nil
}

// SetIfUnchanged stores a summary only if the version matches.
//
// Inputs:
//   - summary: The summary to cache.
//   - expectedVersion: The expected current version.
//
// Outputs:
//   - bool: True if the update succeeded.
//   - error: Non-nil if there's a version conflict.
//
// This implements optimistic concurrency control.
func (c *SummaryCache) SetIfUnchanged(summary *Summary, expectedVersion int64) (bool, error) {
	if summary == nil {
		return false, ErrInvalidBudget
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.entries[summary.ID]
	if ok && existing.summary.Version != expectedVersion {
		return false, ErrCacheVersionConflict
	}

	// Evict if at capacity
	if c.config.MaxEntries > 0 && !ok && len(c.entries) >= c.config.MaxEntries {
		c.evictOldest()
	}

	now := time.Now()
	c.entries[summary.ID] = &cacheEntry{
		summary:    summary,
		createdAt:  now,
		accessedAt: now,
		stale:      false,
	}

	return true, nil
}

// Invalidate marks a summary as stale.
//
// Inputs:
//   - id: The summary ID to invalidate.
//
// Outputs:
//   - error: Always nil (for interface compatibility).
func (c *SummaryCache) Invalidate(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[id]; ok {
		entry.stale = true
	}

	return nil
}

// InvalidateIfStale invalidates a summary if its hash doesn't match.
//
// Inputs:
//   - id: The summary ID.
//   - currentHash: The current content hash.
//
// Outputs:
//   - bool: True if the summary was invalidated (or didn't exist).
func (c *SummaryCache) InvalidateIfStale(id, currentHash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[id]
	if !ok {
		return true // Doesn't exist, consider "invalidated"
	}

	if entry.summary.Hash != currentHash {
		entry.stale = true
		return true
	}

	return false
}

// Delete removes a summary from the cache.
//
// Inputs:
//   - id: The summary ID to delete.
func (c *SummaryCache) Delete(id string) {
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}

// ApplyBatch applies a batch of changes atomically.
//
// Inputs:
//   - batch: The batch of changes to apply.
//
// Outputs:
//   - error: Non-nil if validation fails.
//
// This is all-or-nothing: either all changes apply or none do.
func (c *SummaryCache) ApplyBatch(batch *SummaryBatch) error {
	if err := batch.Validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Delete first
	for _, id := range batch.DeleteIDs {
		delete(c.entries, id)
	}

	// Then upsert
	for i := range batch.Summaries {
		summary := &batch.Summaries[i]

		// Evict if at capacity
		if c.config.MaxEntries > 0 && len(c.entries) >= c.config.MaxEntries {
			c.evictOldest()
		}

		c.entries[summary.ID] = &cacheEntry{
			summary:    summary,
			createdAt:  now,
			accessedAt: now,
			stale:      false,
		}
	}

	return nil
}

// GetByLevel returns all summaries at a specific hierarchy level.
//
// Inputs:
//   - level: The hierarchy level to filter by.
//
// Outputs:
//   - []*Summary: Summaries at this level.
func (c *SummaryCache) GetByLevel(level int) []*Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*Summary, 0)
	for _, entry := range c.entries {
		if entry.summary.Level == level && !entry.stale {
			result = append(result, entry.summary)
		}
	}
	return result
}

// GetChildren returns child summaries for a parent.
//
// Inputs:
//   - parentID: The parent summary ID.
//
// Outputs:
//   - []*Summary: Child summaries.
func (c *SummaryCache) GetChildren(parentID string) []*Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*Summary, 0)
	for _, entry := range c.entries {
		if entry.summary.ParentID == parentID && !entry.stale {
			result = append(result, entry.summary)
		}
	}
	return result
}

// Has returns true if a summary exists (fresh or stale).
func (c *SummaryCache) Has(id string) bool {
	c.mu.RLock()
	_, ok := c.entries[id]
	c.mu.RUnlock()
	return ok
}

// Count returns the number of cached entries.
func (c *SummaryCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache.
func (c *SummaryCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()
}

// Stats returns cache statistics.
func (c *SummaryCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Entries:   len(c.entries),
		Hits:      c.hits,
		Misses:    c.misses,
		StaleHits: c.staleHits,
		Evictions: c.evictions,
	}
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Entries   int   `json:"entries"`
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
	StaleHits int64 `json:"stale_hits"`
	Evictions int64 `json:"evictions"`
}

// HitRate returns the cache hit rate (0.0-1.0).
func (s CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Helper methods for stats (without lock)
func (c *SummaryCache) recordHit() {
	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
}

func (c *SummaryCache) recordMiss() {
	c.mu.Lock()
	c.misses++
	c.mu.Unlock()
}

func (c *SummaryCache) recordStaleHit() {
	c.mu.Lock()
	c.staleHits++
	c.mu.Unlock()
}

// touchEntry updates the access time.
func (c *SummaryCache) touchEntry(id string) {
	c.mu.Lock()
	if entry, ok := c.entries[id]; ok {
		entry.accessedAt = time.Now()
	}
	c.mu.Unlock()
}

// evictOldest removes the least recently accessed entry.
// Must be called with lock held.
func (c *SummaryCache) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, entry := range c.entries {
		if oldestID == "" || entry.accessedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.accessedAt
		}
	}

	if oldestID != "" {
		delete(c.entries, oldestID)
		c.evictions++
	}
}
