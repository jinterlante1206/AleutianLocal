// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// =============================================================================
// Tool Selection Cache (CRS-05)
// =============================================================================

// ToolSelectionCache caches tool selections for identical states.
//
// Description:
//
//	Provides lightweight state caching for tool selection decisions.
//	Cache entries are invalidated when:
//	  - TTL expires
//	  - CRS generation changes (clauses were added/removed)
//	  - Cache is explicitly cleared
//
//	This is a simplified transposition table optimized for tool selection.
//	Unlike the full MCTS transposition table, this cache:
//	  - Uses order-preserving state keys (tool sequence matters)
//	  - Invalidates on CRS changes (clause learning)
//	  - Has shorter TTL (session-scoped)
//
// Thread Safety: Safe for concurrent use.
type ToolSelectionCache struct {
	mu     sync.RWMutex
	cache  map[string]*cachedSelection
	ttl    time.Duration
	maxLen int
	logger *slog.Logger

	// Metrics - use atomic for lock-free updates
	hits          atomic.Int64
	misses        atomic.Int64
	invalidations atomic.Int64
}

// cachedSelection stores a cached tool selection.
type cachedSelection struct {
	tool       string
	score      float64
	cachedAt   time.Time
	generation int64 // CRS generation when cached
}

// ToolSelectionCacheConfig configures the cache.
type ToolSelectionCacheConfig struct {
	// TTL is the time-to-live for cache entries.
	// Default: 5 minutes.
	TTL time.Duration

	// MaxLen is the maximum number of entries.
	// Default: 1000.
	MaxLen int

	// Logger for debug output. If nil, uses default logger.
	Logger *slog.Logger
}

// DefaultToolSelectionCacheConfig returns the default cache configuration.
//
// Outputs:
//
//	*ToolSelectionCacheConfig - Default configuration.
func DefaultToolSelectionCacheConfig() *ToolSelectionCacheConfig {
	return &ToolSelectionCacheConfig{
		TTL:    5 * time.Minute,
		MaxLen: 1000,
		Logger: nil,
	}
}

// NewToolSelectionCache creates a new tool selection cache with default config.
//
// Outputs:
//
//	*ToolSelectionCache - The cache instance.
func NewToolSelectionCache() *ToolSelectionCache {
	return NewToolSelectionCacheWithConfig(DefaultToolSelectionCacheConfig())
}

// NewToolSelectionCacheWithConfig creates a new cache with the given config.
//
// Inputs:
//
//	config - The configuration. If nil, uses default.
//
// Outputs:
//
//	*ToolSelectionCache - The cache instance.
func NewToolSelectionCacheWithConfig(config *ToolSelectionCacheConfig) *ToolSelectionCache {
	if config == nil {
		config = DefaultToolSelectionCacheConfig()
	}

	ttl := config.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	maxLen := config.MaxLen
	if maxLen <= 0 {
		maxLen = 1000
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &ToolSelectionCache{
		cache:  make(map[string]*cachedSelection),
		ttl:    ttl,
		maxLen: maxLen,
		logger: logger,
	}
}

// Get retrieves a cached selection if valid.
//
// Description:
//
//	Returns the cached tool selection if:
//	  - Key exists in cache
//	  - Entry hasn't expired (TTL)
//	  - CRS generation matches (clauses haven't changed)
//
// Inputs:
//
//	key - The state key (from StateKeyBuilder).
//	currentGen - The current CRS generation.
//
// Outputs:
//
//	string - The cached tool name.
//	float64 - The cached score.
//	bool - True if cache hit, false otherwise.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) Get(key string, currentGen int64) (string, float64, bool) {
	c.mu.RLock()
	entry, exists := c.cache[key]
	if !exists {
		c.mu.RUnlock()
		c.misses.Add(1)
		return "", 0, false
	}

	// Copy values while holding lock
	tool := entry.tool
	score := entry.score
	cachedAt := entry.cachedAt
	generation := entry.generation
	c.mu.RUnlock()

	// Check TTL
	if time.Since(cachedAt) > c.ttl {
		c.misses.Add(1)
		c.invalidations.Add(1)
		return "", 0, false
	}

	// Check generation (invalidate if CRS changed)
	if generation != currentGen {
		c.misses.Add(1)
		c.invalidations.Add(1)
		return "", 0, false
	}

	c.hits.Add(1)
	return tool, score, true
}

// Put stores a tool selection in the cache.
//
// Description:
//
//	Stores the selection keyed by state. If the cache is full,
//	evicts the oldest entry.
//
// Inputs:
//
//	key - The state key.
//	tool - The selected tool.
//	score - The selection score.
//	generation - The CRS generation.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) Put(key, tool string, score float64, generation int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.cache) >= c.maxLen {
		c.evictOldest()
	}

	c.cache[key] = &cachedSelection{
		tool:       tool,
		score:      score,
		cachedAt:   time.Now(),
		generation: generation,
	}

	c.logger.Debug("UCB1: cached tool selection",
		slog.String("key", truncateKey(key, 50)),
		slog.String("tool", tool),
		slog.Float64("score", score),
		slog.Int64("generation", generation),
	)
}

// evictOldest removes the oldest cache entry.
// Caller must hold write lock.
// Note: O(n) scan - acceptable for default max 1000 entries.
// For larger caches, consider min-heap or LRU linked list.
func (c *ToolSelectionCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.cache {
		if oldestKey == "" || entry.cachedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.cachedAt
		}
	}

	if oldestKey != "" {
		delete(c.cache, oldestKey)
		c.invalidations.Add(1)
	}
}

// InvalidateByGeneration removes all entries with an older generation.
//
// Description:
//
//	Called when CRS changes (clause added/removed) to invalidate stale entries.
//
// Inputs:
//
//	oldGeneration - Generation to invalidate.
//
// Outputs:
//
//	int - Number of entries invalidated.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) InvalidateByGeneration(oldGeneration int64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for key, entry := range c.cache {
		if entry.generation <= oldGeneration {
			delete(c.cache, key)
			count++
		}
	}
	c.invalidations.Add(int64(count))
	return count
}

// Clear removes all cache entries.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.invalidations.Add(int64(len(c.cache)))
	c.cache = make(map[string]*cachedSelection)
}

// Size returns the current number of cache entries.
//
// Outputs:
//
//	int - Number of entries.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Metrics returns cache performance metrics.
//
// Outputs:
//
//	CacheMetrics - Hit/miss statistics.
//
// Thread Safety: Safe for concurrent use.
func (c *ToolSelectionCache) Metrics() CacheMetrics {
	hits := c.hits.Load()
	misses := c.misses.Load()
	invalidations := c.invalidations.Load()

	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	c.mu.RLock()
	size := len(c.cache)
	c.mu.RUnlock()

	return CacheMetrics{
		Hits:          hits,
		Misses:        misses,
		Invalidations: invalidations,
		HitRate:       hitRate,
		Size:          size,
	}
}

// CacheMetrics contains cache performance statistics.
type CacheMetrics struct {
	// Hits is the number of cache hits.
	Hits int64 `json:"hits"`

	// Misses is the number of cache misses.
	Misses int64 `json:"misses"`

	// Invalidations is the number of invalidated entries.
	Invalidations int64 `json:"invalidations"`

	// HitRate is hits / (hits + misses).
	HitRate float64 `json:"hit_rate"`

	// Size is the current number of entries.
	Size int `json:"size"`
}

// =============================================================================
// State Key Builder
// =============================================================================

// StateKeyBuilder builds cache keys from tool execution history.
//
// Description:
//
//	Generates deterministic, order-preserving keys from the tool execution
//	sequence. Unlike transposition keys (which are order-independent),
//	these keys preserve the sequence because tool order matters.
//
//	Key format: "gen:<generation>|<tool1>:<outcome1>→<tool2>:<outcome2>→..."
//
// Thread Safety: StateKeyBuilder is safe for concurrent use (stateless).
type StateKeyBuilder struct{}

// NewStateKeyBuilder creates a new state key builder.
//
// Outputs:
//
//	*StateKeyBuilder - The builder instance.
func NewStateKeyBuilder() *StateKeyBuilder {
	return &StateKeyBuilder{}
}

// BuildKey generates a cache key from step history and CRS generation.
//
// Description:
//
//	Creates an order-preserving key that captures the tool execution sequence.
//	The key includes the CRS generation for automatic invalidation.
//
// Inputs:
//
//	steps - The step history from CRS.
//	generation - The current CRS generation.
//
// Outputs:
//
//	string - The cache key.
func (b *StateKeyBuilder) BuildKey(steps []crs.StepRecord, generation int64) string {
	var parts []string

	for _, step := range steps {
		if step.Decision == crs.DecisionExecuteTool && step.Tool != "" {
			parts = append(parts, fmt.Sprintf("%s:%s", step.Tool, step.Outcome))
		}
	}

	return fmt.Sprintf("gen:%d|%s", generation, strings.Join(parts, "→"))
}

// BuildKeyFromHistory generates a cache key from tool history entries.
//
// Description:
//
//	Alternative method for building keys from ToolHistoryEntry slice.
//
// Inputs:
//
//	history - The tool history entries.
//	generation - The current CRS generation.
//
// Outputs:
//
//	string - The cache key.
func (b *StateKeyBuilder) BuildKeyFromHistory(history []ToolHistoryEntry, generation int64) string {
	var parts []string

	for _, entry := range history {
		outcome := "success"
		if !entry.Success {
			outcome = "failure"
		}
		parts = append(parts, fmt.Sprintf("%s:%s", entry.Tool, outcome))
	}

	return fmt.Sprintf("gen:%d|%s", generation, strings.Join(parts, "→"))
}

// truncateKey truncates a key for logging.
func truncateKey(key string, maxLen int) string {
	if maxLen < 4 {
		// Need at least 4 characters for "x..." pattern
		if maxLen <= 0 {
			return ""
		}
		// Return first maxLen characters without ellipsis
		if len(key) <= maxLen {
			return key
		}
		return key[:maxLen]
	}
	if len(key) <= maxLen {
		return key
	}
	return key[:maxLen-3] + "..."
}
