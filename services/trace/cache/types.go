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
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

// Default configuration values.
const (
	// DefaultMaxEntries is the default maximum number of cached graphs.
	DefaultMaxEntries = 5

	// DefaultMaxAge is the default TTL for cached entries.
	DefaultMaxAge = 30 * time.Minute

	// DefaultErrorCacheTTL is how long build errors are cached.
	DefaultErrorCacheTTL = 5 * time.Second
)

// CacheEntry represents a cached graph with its metadata.
//
// Thread Safety:
//
//	CacheEntry is safe for concurrent reads. The mu mutex must be
//	held for write operations like Refresh.
type CacheEntry struct {
	// GraphID is the unique identifier for this cache entry.
	// Format: full SHA256 of project root (64 hex chars).
	GraphID string

	// ProjectRoot is the absolute path to the project.
	ProjectRoot string

	// Graph is the cached code graph.
	Graph *graph.Graph

	// Manifest contains the file hashes at build time.
	// Used for change detection.
	Manifest *manifest.Manifest

	// BuiltAtMilli is when the graph was built.
	BuiltAtMilli int64

	// LastAccessMilli is when the entry was last accessed.
	LastAccessMilli int64

	// refCount tracks active references to this entry.
	refCount int32

	// stale is true if the entry has been invalidated.
	stale bool

	// mu protects write operations on this entry.
	mu sync.Mutex

	// lruElement is the position in the LRU list.
	lruElement *list.Element
}

// Acquire increments the reference count.
//
// Must be paired with a call to Release when done using the entry.
func (e *CacheEntry) Acquire() {
	atomic.AddInt32(&e.refCount, 1)
}

// Release decrements the reference count.
func (e *CacheEntry) Release() {
	atomic.AddInt32(&e.refCount, -1)
}

// InUse returns true if the entry has active references.
func (e *CacheEntry) InUse() bool {
	return atomic.LoadInt32(&e.refCount) > 0
}

// RefCount returns the current reference count.
func (e *CacheEntry) RefCount() int32 {
	return atomic.LoadInt32(&e.refCount)
}

// IsStale returns true if the entry has been marked as stale.
func (e *CacheEntry) IsStale() bool {
	return e.stale
}

// EstimatedMemoryBytes returns an approximate memory usage for this entry.
//
// Description:
//
//	Estimates memory usage based on graph node/edge counts and manifest
//	file counts. Uses conservative estimates per element.
//
// Inputs:
//
//	None. Reads from entry state.
//
// Outputs:
//
//	int64 - Estimated memory usage in bytes.
//
// Memory Estimation:
//
//   - Per node: ~500 bytes (Node struct + Symbol pointer + slices)
//   - Per edge: ~100 bytes (Edge struct + Location)
//   - Per manifest file: ~200 bytes (FileEntry with path/hash)
//   - Base overhead: ~1KB
//
// Limitations:
//
//	Estimates are heuristic. Actual memory may differ due to:
//	- Go allocator overhead and alignment
//	- Varying string lengths in Symbol/FileEntry
//	- Runtime memory fragmentation
//
// Assumptions:
//
//	Graph and Manifest are not nil, or method handles nil gracefully.
//
// Thread Safety:
//
//	Safe for concurrent reads. Does not modify entry state.
func (e *CacheEntry) EstimatedMemoryBytes() int64 {
	const (
		baseOverhead = 1024 // 1KB base
		bytesPerNode = 500  // Node + Symbol reference + edge slices
		bytesPerEdge = 100  // Edge struct
		bytesPerFile = 200  // FileEntry in manifest
	)

	var bytes int64 = baseOverhead

	if e.Graph != nil {
		bytes += int64(e.Graph.NodeCount()) * bytesPerNode
		bytes += int64(e.Graph.EdgeCount()) * bytesPerEdge
	}

	if e.Manifest != nil {
		bytes += int64(len(e.Manifest.Files)) * bytesPerFile
	}

	return bytes
}

// CacheStats contains statistics about the cache.
type CacheStats struct {
	// EntryCount is the number of entries in the cache.
	EntryCount int

	// Hits is the number of cache hits.
	Hits int64

	// Misses is the number of cache misses.
	Misses int64

	// Evictions is the number of entries evicted.
	Evictions int64

	// MemoryEvictions is the number of entries evicted due to memory pressure.
	MemoryEvictions int64

	// BuildCount is the number of graphs built.
	BuildCount int64

	// RefreshCount is the number of incremental updates.
	RefreshCount int64

	// ErrorCount is the number of build errors.
	ErrorCount int64

	// MaxEntries is the configured maximum entries.
	MaxEntries int

	// MaxAge is the configured TTL.
	MaxAge time.Duration

	// MaxMemoryMB is the configured memory limit (0 = unlimited).
	MaxMemoryMB int

	// EstimatedMemoryMB is the current estimated memory usage.
	EstimatedMemoryMB int
}

// HitRate returns the cache hit rate as a percentage.
func (s CacheStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}

// CacheOptions configures GraphCache behavior.
type CacheOptions struct {
	// MaxEntries is the maximum number of cached graphs.
	MaxEntries int

	// MaxAge is the TTL for cached entries.
	MaxAge time.Duration

	// MaxMemoryMB is the soft memory limit (0 = unlimited).
	MaxMemoryMB int

	// ErrorCacheTTL is how long build errors are cached.
	ErrorCacheTTL time.Duration
}

// DefaultCacheOptions returns sensible defaults.
func DefaultCacheOptions() CacheOptions {
	return CacheOptions{
		MaxEntries:    DefaultMaxEntries,
		MaxAge:        DefaultMaxAge,
		ErrorCacheTTL: DefaultErrorCacheTTL,
	}
}

// CacheOption is a functional option for configuring GraphCache.
type CacheOption func(*CacheOptions)

// WithMaxEntries sets the maximum number of cached entries.
func WithMaxEntries(n int) CacheOption {
	return func(o *CacheOptions) {
		if n > 0 {
			o.MaxEntries = n
		}
	}
}

// WithMaxAge sets the TTL for cached entries.
func WithMaxAge(d time.Duration) CacheOption {
	return func(o *CacheOptions) {
		if d > 0 {
			o.MaxAge = d
		}
	}
}

// WithMaxMemoryMB sets the soft memory limit.
func WithMaxMemoryMB(mb int) CacheOption {
	return func(o *CacheOptions) {
		if mb >= 0 {
			o.MaxMemoryMB = mb
		}
	}
}

// WithErrorCacheTTL sets how long build errors are cached.
func WithErrorCacheTTL(d time.Duration) CacheOption {
	return func(o *CacheOptions) {
		if d > 0 {
			o.ErrorCacheTTL = d
		}
	}
}

// GenerateGraphID creates a stable ID for a project root.
//
// Uses full SHA256 (64 hex chars) to eliminate collision risk.
func GenerateGraphID(projectRoot string) string {
	h := sha256.Sum256([]byte(projectRoot))
	return hex.EncodeToString(h[:])
}

// failedBuild represents a cached build error.
type failedBuild struct {
	err      error
	failedAt int64 // Unix milliseconds UTC
	retryAt  int64 // Unix milliseconds UTC
}
