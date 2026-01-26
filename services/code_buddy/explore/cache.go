// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Default cache configuration.
const (
	// DefaultCacheSize is the default maximum number of entries per cache type.
	DefaultCacheSize = 100

	// DefaultCacheTTL is the default time-to-live for cache entries.
	DefaultCacheTTL = 5 * time.Minute
)

// ExplorationCache provides caching for expensive exploration operations.
//
// Thread Safety:
//
//	ExplorationCache is safe for concurrent use.
type ExplorationCache struct {
	mu sync.RWMutex

	// entryPoints caches EntryPointResult by options hash
	entryPoints map[string]*cachedEntryPoints

	// fileSummaries caches FileSummary by file path
	fileSummaries map[string]*cachedFileSummary

	// packageAPIs caches PackageAPI by package path
	packageAPIs map[string]*cachedPackageAPI

	// config
	maxSize int
	ttl     time.Duration

	// stats
	hits   int64
	misses int64
}

type cachedEntryPoints struct {
	result    *EntryPointResult
	createdAt time.Time
}

type cachedFileSummary struct {
	summary   *FileSummary
	createdAt time.Time
}

type cachedPackageAPI struct {
	api       *PackageAPI
	createdAt time.Time
}

// CacheConfig configures the exploration cache.
type CacheConfig struct {
	// MaxSize is the maximum number of entries per cache type.
	MaxSize int

	// TTL is the time-to-live for cache entries.
	TTL time.Duration
}

// DefaultCacheConfig returns sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize: DefaultCacheSize,
		TTL:     DefaultCacheTTL,
	}
}

// NewExplorationCache creates a new exploration cache with the given config.
//
// Example:
//
//	cache := NewExplorationCache(DefaultCacheConfig())
func NewExplorationCache(config CacheConfig) *ExplorationCache {
	if config.MaxSize <= 0 {
		config.MaxSize = DefaultCacheSize
	}
	if config.TTL <= 0 {
		config.TTL = DefaultCacheTTL
	}

	return &ExplorationCache{
		entryPoints:   make(map[string]*cachedEntryPoints),
		fileSummaries: make(map[string]*cachedFileSummary),
		packageAPIs:   make(map[string]*cachedPackageAPI),
		maxSize:       config.MaxSize,
		ttl:           config.TTL,
	}
}

// CacheStats returns cache statistics.
type CacheStats struct {
	EntryPointCount  int
	FileSummaryCount int
	PackageAPICount  int
	Hits             int64
	Misses           int64
	HitRate          float64
	MaxSize          int
	TTLSeconds       int64
}

// Stats returns current cache statistics.
func (c *ExplorationCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		EntryPointCount:  len(c.entryPoints),
		FileSummaryCount: len(c.fileSummaries),
		PackageAPICount:  len(c.packageAPIs),
		Hits:             c.hits,
		Misses:           c.misses,
		HitRate:          hitRate,
		MaxSize:          c.maxSize,
		TTLSeconds:       int64(c.ttl.Seconds()),
	}
}

// GetEntryPoints retrieves cached entry point results.
//
// Returns nil if not cached or expired.
func (c *ExplorationCache) GetEntryPoints(opts EntryPointOptions) *EntryPointResult {
	key := c.entryPointsKey(opts)

	c.mu.RLock()
	cached, ok := c.entryPoints[key]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil
	}

	// Check TTL
	if time.Since(cached.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.entryPoints, key)
		c.misses++
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()

	return cached.result
}

// SetEntryPoints caches entry point results.
func (c *ExplorationCache) SetEntryPoints(opts EntryPointOptions, result *EntryPointResult) {
	key := c.entryPointsKey(opts)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.entryPoints) >= c.maxSize {
		c.evictOldestEntryPoints()
	}

	c.entryPoints[key] = &cachedEntryPoints{
		result:    result,
		createdAt: time.Now(),
	}
}

// GetFileSummary retrieves a cached file summary.
//
// Returns nil if not cached or expired.
func (c *ExplorationCache) GetFileSummary(filePath string) *FileSummary {
	c.mu.RLock()
	cached, ok := c.fileSummaries[filePath]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil
	}

	// Check TTL
	if time.Since(cached.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.fileSummaries, filePath)
		c.misses++
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()

	return cached.summary
}

// SetFileSummary caches a file summary.
func (c *ExplorationCache) SetFileSummary(filePath string, summary *FileSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.fileSummaries) >= c.maxSize {
		c.evictOldestFileSummaries()
	}

	c.fileSummaries[filePath] = &cachedFileSummary{
		summary:   summary,
		createdAt: time.Now(),
	}
}

// GetPackageAPI retrieves a cached package API.
//
// Returns nil if not cached or expired.
func (c *ExplorationCache) GetPackageAPI(packagePath string) *PackageAPI {
	c.mu.RLock()
	cached, ok := c.packageAPIs[packagePath]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil
	}

	// Check TTL
	if time.Since(cached.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.packageAPIs, packagePath)
		c.misses++
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()

	return cached.api
}

// SetPackageAPI caches a package API.
func (c *ExplorationCache) SetPackageAPI(packagePath string, api *PackageAPI) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.packageAPIs) >= c.maxSize {
		c.evictOldestPackageAPIs()
	}

	c.packageAPIs[packagePath] = &cachedPackageAPI{
		api:       api,
		createdAt: time.Now(),
	}
}

// InvalidateFile invalidates all cache entries related to a file.
func (c *ExplorationCache) InvalidateFile(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.fileSummaries, filePath)
	// Also invalidate entry points since they may reference this file
	c.entryPoints = make(map[string]*cachedEntryPoints)
}

// InvalidatePackage invalidates all cache entries related to a package.
func (c *ExplorationCache) InvalidatePackage(packagePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.packageAPIs, packagePath)
	// Also invalidate entry points since they may reference this package
	c.entryPoints = make(map[string]*cachedEntryPoints)
}

// Clear removes all cached entries.
func (c *ExplorationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entryPoints = make(map[string]*cachedEntryPoints)
	c.fileSummaries = make(map[string]*cachedFileSummary)
	c.packageAPIs = make(map[string]*cachedPackageAPI)
}

// entryPointsKey generates a cache key for entry point options.
func (c *ExplorationCache) entryPointsKey(opts EntryPointOptions) string {
	key := fmt.Sprintf("ep:%s:%s:%s:%d:%v",
		opts.Type, opts.Package, opts.Language, opts.Limit, opts.IncludeTests)
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:8])
}

// evictOldestEntryPoints evicts the oldest entry point cache entry.
// Caller must hold the write lock.
func (c *ExplorationCache) evictOldestEntryPoints() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range c.entryPoints {
		if oldestKey == "" || cached.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.entryPoints, oldestKey)
	}
}

// evictOldestFileSummaries evicts the oldest file summary cache entry.
// Caller must hold the write lock.
func (c *ExplorationCache) evictOldestFileSummaries() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range c.fileSummaries {
		if oldestKey == "" || cached.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.fileSummaries, oldestKey)
	}
}

// evictOldestPackageAPIs evicts the oldest package API cache entry.
// Caller must hold the write lock.
func (c *ExplorationCache) evictOldestPackageAPIs() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range c.packageAPIs {
		if oldestKey == "" || cached.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.packageAPIs, oldestKey)
	}
}

// CachedEntryPointFinder wraps EntryPointFinder with caching.
type CachedEntryPointFinder struct {
	finder *EntryPointFinder
	cache  *ExplorationCache
}

// NewCachedEntryPointFinder creates a cached entry point finder.
func NewCachedEntryPointFinder(finder *EntryPointFinder, cache *ExplorationCache) *CachedEntryPointFinder {
	return &CachedEntryPointFinder{
		finder: finder,
		cache:  cache,
	}
}

// FindEntryPoints finds entry points with caching.
func (f *CachedEntryPointFinder) FindEntryPoints(ctx context.Context, opts EntryPointOptions) (*EntryPointResult, error) {
	// Check cache first
	if cached := f.cache.GetEntryPoints(opts); cached != nil {
		return cached, nil
	}

	// Execute and cache
	result, err := f.finder.FindEntryPoints(ctx, opts)
	if err != nil {
		return nil, err
	}

	f.cache.SetEntryPoints(opts, result)
	return result, nil
}

// CachedFileSummarizer wraps FileSummarizer with caching.
type CachedFileSummarizer struct {
	summarizer *FileSummarizer
	cache      *ExplorationCache
}

// NewCachedFileSummarizer creates a cached file summarizer.
func NewCachedFileSummarizer(summarizer *FileSummarizer, cache *ExplorationCache) *CachedFileSummarizer {
	return &CachedFileSummarizer{
		summarizer: summarizer,
		cache:      cache,
	}
}

// SummarizeFile summarizes a file with caching.
func (s *CachedFileSummarizer) SummarizeFile(ctx context.Context, filePath string) (*FileSummary, error) {
	// Check cache first
	if cached := s.cache.GetFileSummary(filePath); cached != nil {
		return cached, nil
	}

	// Execute and cache
	summary, err := s.summarizer.SummarizeFile(ctx, filePath)
	if err != nil {
		return nil, err
	}

	s.cache.SetFileSummary(filePath, summary)
	return summary, nil
}

// CachedPackageAPISummarizer wraps PackageAPISummarizer with caching.
type CachedPackageAPISummarizer struct {
	summarizer *PackageAPISummarizer
	cache      *ExplorationCache
}

// NewCachedPackageAPISummarizer creates a cached package API summarizer.
func NewCachedPackageAPISummarizer(summarizer *PackageAPISummarizer, cache *ExplorationCache) *CachedPackageAPISummarizer {
	return &CachedPackageAPISummarizer{
		summarizer: summarizer,
		cache:      cache,
	}
}

// FindPackageAPI finds the package API with caching.
func (s *CachedPackageAPISummarizer) FindPackageAPI(ctx context.Context, packagePath string) (*PackageAPI, error) {
	// Check cache first
	if cached := s.cache.GetPackageAPI(packagePath); cached != nil {
		return cached, nil
	}

	// Execute and cache
	api, err := s.summarizer.FindPackageAPI(ctx, packagePath)
	if err != nil {
		return nil, err
	}

	s.cache.SetPackageAPI(packagePath, api)
	return api, nil
}
