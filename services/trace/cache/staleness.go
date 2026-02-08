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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer for staleness operations - uses consistent naming with graph cache.
var stalenessTracer = otel.Tracer("aleutian.cache.graph.staleness")

// Prometheus metrics for staleness operations (A4).
var (
	stalenessChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "graph_cache_staleness_checks_total",
		Help: "Total staleness checks by reason",
	}, []string{"reason"})

	stalenessHashDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "graph_cache_staleness_hash_duration_seconds",
		Help:    "Time spent computing source hash",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	})

	stalenessHashFileCount = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "graph_cache_staleness_hash_file_count",
		Help:    "Number of files included in source hash",
		Buckets: []float64{10, 50, 100, 500, 1000, 5000, 10000, 50000},
	})

	stalenessHashCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "graph_cache_staleness_hash_cache_hits_total",
		Help: "Number of hash cache hits",
	})

	stalenessHashCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "graph_cache_staleness_hash_cache_misses_total",
		Help: "Number of hash cache misses",
	})
)

// StalenessReason indicates why a cache entry is stale.
type StalenessReason string

const (
	// StalenessNone indicates the cache is valid.
	StalenessNone StalenessReason = ""

	// StalenessVersionMismatch indicates the builder version changed.
	StalenessVersionMismatch StalenessReason = "builder_version_mismatch"

	// StalenessSourceChanged indicates source files changed.
	StalenessSourceChanged StalenessReason = "source_files_changed"

	// StalenessHashError indicates an error computing source hash.
	StalenessHashError StalenessReason = "hash_computation_error"
)

// DefaultSourceExtensions are the file extensions checked for staleness.
// Can be overridden per-call using ComputeSourceHashWithExtensions.
var DefaultSourceExtensions = map[string]bool{
	".go":    true,
	".py":    true,
	".ts":    true,
	".tsx":   true,
	".js":    true,
	".jsx":   true,
	".java":  true,
	".kt":    true, // M1: Added Kotlin
	".rs":    true,
	".c":     true,
	".cpp":   true,
	".h":     true,
	".hpp":   true,
	".rb":    true, // M1: Added Ruby
	".swift": true, // M1: Added Swift
}

// DefaultSkipDirectories are directories skipped during hash computation.
// M2: Extended list of commonly generated/vendored directories.
var DefaultSkipDirectories = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"__pycache__":  true,
	"target":       true,
	// M2: Additional directories
	".idea":    true,
	".vscode":  true,
	"build":    true,
	"dist":     true,
	"bin":      true,
	".next":    true,
	"coverage": true,
	".cache":   true,
	"tmp":      true,
	".tox":     true,
	"eggs":     true,
	".eggs":    true,
}

// sourceHashCache caches computed source hashes with TTL.
type sourceHashCache struct {
	mu            sync.RWMutex
	hashes        map[string]cachedHash
	ttl           time.Duration
	maxFiles      int   // Maximum files to scan before giving up
	maxCacheSize  int   // A3: Maximum entries before cleanup
	lastCleanup   int64 // Unix millis of last cleanup
	cleanupPeriod time.Duration
}

type cachedHash struct {
	hash       string
	fileCount  int
	computedAt int64 // Unix millis
}

// globalHashCache is the default hash cache instance.
// C2: Can be replaced for testing via SetHashCache.
var globalHashCache = newSourceHashCache()

// newSourceHashCache creates a new source hash cache with defaults.
func newSourceHashCache() *sourceHashCache {
	return &sourceHashCache{
		hashes:        make(map[string]cachedHash),
		ttl:           DefaultSourceHashTTL,
		maxFiles:      100000,
		maxCacheSize:  1000, // A3: Limit cache size
		cleanupPeriod: 5 * time.Minute,
	}
}

// SetHashCache replaces the global hash cache (for testing). C2 fix.
// Returns a cleanup function that restores the original cache.
func SetHashCache(cache *sourceHashCache) func() {
	old := globalHashCache
	globalHashCache = cache
	return func() {
		globalHashCache = old
	}
}

// NewTestHashCache creates a hash cache for testing. C2 fix.
func NewTestHashCache() *sourceHashCache {
	return newSourceHashCache()
}

// ComputeSourceHash computes a hash of source file metadata.
//
// Description:
//
//	Walks the project directory and computes a SHA256 hash of
//	(relative_path, mtime, size) for all source files. Files are
//	sorted alphabetically for deterministic hashing (H2 fix).
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	root - Absolute path to project root. Must exist.
//
// Outputs:
//
//	string - Hex-encoded SHA256 hash (64 chars).
//	int - Number of files included in hash.
//	error - Non-nil if walk failed or was cancelled.
//
// Example:
//
//	hash, count, err := ComputeSourceHash(ctx, "/path/to/project")
//	if err != nil {
//	    return fmt.Errorf("compute hash: %w", err)
//	}
//	fmt.Printf("Hash: %s (%d files)\n", hash[:16], count)
//
// Thread Safety: Safe for concurrent use.
//
// Complexity: O(N log N) where N = number of files (due to sorting).
//
// Limitations:
//
//   - Skips symlinks to avoid infinite loops
//   - Returns error if >100K files (likely wrong directory)
//   - Uses mtime which may not detect content-preserving copies
//
// Assumptions:
//
//   - ctx is not nil
//   - root is a valid directory path
//   - Filesystem mtime has sufficient granularity (may fail on some network filesystems)
func ComputeSourceHash(ctx context.Context, root string) (string, int, error) {
	return ComputeSourceHashWithExtensions(ctx, root, nil)
}

// ComputeSourceHashWithExtensions computes hash with custom extensions. M1 fix.
//
// If extensions is nil, uses DefaultSourceExtensions.
func ComputeSourceHashWithExtensions(ctx context.Context, root string, extensions map[string]bool) (string, int, error) {
	ctx, span := stalenessTracer.Start(ctx, "ComputeSourceHash",
		trace.WithAttributes(
			attribute.String("root", root),
		),
	)
	defer span.End()

	if extensions == nil {
		extensions = DefaultSourceExtensions
	}

	// Check cache first
	if cached, ok := globalHashCache.get(root); ok {
		span.SetAttributes(
			attribute.Bool("cache_hit", true),
			attribute.Int("file_count", cached.fileCount),
		)
		stalenessHashCacheHits.Inc()
		return cached.hash, cached.fileCount, nil
	}
	stalenessHashCacheMisses.Inc()

	startTime := time.Now()

	// H2 Fix: Collect files first, then sort for deterministic order
	type fileInfo struct {
		relPath string
		mtime   int64
		size    int64
	}
	var files []fileInfo
	var permissionErrors []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Check cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			// H1 Fix: Track permission errors for better diagnostics
			permissionErrors = append(permissionErrors, path)
			if len(permissionErrors) <= 3 {
				slog.Debug("GR-42: skipping inaccessible path",
					slog.String("path", path),
					slog.String("error", err.Error()),
				)
			}
			return nil
		}

		// Skip directories
		if d.IsDir() {
			name := d.Name()
			if DefaultSkipDirectories[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks (avoid infinite loops)
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Check extension
		ext := filepath.Ext(path)
		if !extensions[ext] {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			// Track but continue
			permissionErrors = append(permissionErrors, path)
			return nil
		}

		// Check file count limit
		if len(files) >= globalHashCache.maxFiles {
			return fmt.Errorf("too many source files (>%d), aborting hash computation", globalHashCache.maxFiles)
		}

		// Use relative path for consistent hashing across machines
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			relPath = path
		}

		files = append(files, fileInfo{
			relPath: relPath,
			mtime:   info.ModTime().UnixNano(),
			size:    info.Size(),
		})
		return nil
	})

	if err != nil {
		span.RecordError(err)
		return "", 0, fmt.Errorf("walking source files in %s: %w", root, err)
	}

	// H1 Fix: Log summary of permission errors
	if len(permissionErrors) > 3 {
		slog.Warn("GR-42: multiple inaccessible paths during hash",
			slog.String("root", root),
			slog.Int("count", len(permissionErrors)),
			slog.String("first", permissionErrors[0]),
		)
	}

	// H2 Fix: Sort files for deterministic order
	sort.Slice(files, func(i, j int) bool {
		return files[i].relPath < files[j].relPath
	})

	// Compute hash
	hasher := sha256.New()
	for _, f := range files {
		fmt.Fprintf(hasher, "%s:%d:%d\n", f.relPath, f.mtime, f.size)
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	duration := time.Since(startTime)
	fileCount := len(files)

	span.SetAttributes(
		attribute.Bool("cache_hit", false),
		attribute.Int("file_count", fileCount),
		attribute.Int64("duration_ms", duration.Milliseconds()),
		attribute.Int("permission_errors", len(permissionErrors)),
	)

	// A4: Record metrics
	stalenessHashDuration.Observe(duration.Seconds())
	stalenessHashFileCount.Observe(float64(fileCount))

	// M4 Fix: Only log at Debug level, reduce noise
	if duration > 100*time.Millisecond {
		slog.Debug("GR-42: source hash computation slow",
			slog.String("root", root),
			slog.Int("file_count", fileCount),
			slog.Duration("duration", duration),
		)
	}

	// Cache the result
	globalHashCache.set(root, hash, fileCount)

	return hash, fileCount, nil
}

// CheckStaleness determines if a cache entry is stale.
//
// Description:
//
//	Checks if the cached graph is stale by comparing:
//	1. Builder version (must match current GraphBuilderVersion)
//	2. Source hash (must match current source files)
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	entry - The cache entry to check. Must not be nil.
//
// Outputs:
//
//	StalenessReason - The reason for staleness, or StalenessNone if valid.
//	error - Non-nil only if hash computation failed. Note: when error is non-nil,
//	        reason is always StalenessHashError.
//
// Example:
//
//	reason, err := CheckStaleness(ctx, entry)
//	if err != nil {
//	    // Hash computation failed - treat as stale but log error
//	    slog.Warn("staleness check failed", slog.String("error", err.Error()))
//	}
//	if reason != StalenessNone {
//	    // Entry is stale, needs rebuild
//	}
//
// Thread Safety: Safe for concurrent use.
//
// Assumptions:
//
//   - ctx is not nil
//   - entry is not nil and has valid ProjectRoot
func CheckStaleness(ctx context.Context, entry *CacheEntry) (StalenessReason, error) {
	// C1 Fix: Nil check for entry
	if entry == nil {
		return StalenessHashError, errors.New("cache entry must not be nil")
	}

	ctx, span := stalenessTracer.Start(ctx, "CheckStaleness",
		trace.WithAttributes(
			attribute.String("graph_id", entry.GraphID),
			attribute.String("project_root", entry.ProjectRoot),
			attribute.String("cached_version", entry.BuilderVersion),
			attribute.String("current_version", GraphBuilderVersion),
		),
	)
	defer span.End()

	// Check 1: Builder version (fast, no I/O)
	if entry.BuilderVersion != GraphBuilderVersion {
		slog.Info("GR-42: cache stale due to builder version mismatch",
			slog.String("graph_id", entry.GraphID),
			slog.String("cached_version", entry.BuilderVersion),
			slog.String("current_version", GraphBuilderVersion),
		)
		span.SetAttributes(attribute.String("staleness_reason", string(StalenessVersionMismatch)))
		stalenessChecksTotal.WithLabelValues(string(StalenessVersionMismatch)).Inc()
		return StalenessVersionMismatch, nil
	}

	// Check 2: Source hash (requires filesystem walk)
	currentHash, fileCount, err := ComputeSourceHash(ctx, entry.ProjectRoot)
	if err != nil {
		// M6 Fix: Preserve and return the actual error
		slog.Warn("GR-42: failed to compute source hash",
			slog.String("graph_id", entry.GraphID),
			slog.String("project_root", entry.ProjectRoot),
			slog.String("error", err.Error()),
		)
		span.RecordError(err)
		span.SetAttributes(attribute.String("staleness_reason", string(StalenessHashError)))
		stalenessChecksTotal.WithLabelValues(string(StalenessHashError)).Inc()
		return StalenessHashError, fmt.Errorf("computing source hash for %s: %w", entry.ProjectRoot, err)
	}

	span.SetAttributes(attribute.Int("current_file_count", fileCount))

	// M3 Fix: Handle empty source hash gracefully
	// If cached hash is empty (hash failed at build time), always rebuild
	if entry.SourceHash == "" {
		slog.Info("GR-42: cache stale due to missing source hash",
			slog.String("graph_id", entry.GraphID),
		)
		span.SetAttributes(attribute.String("staleness_reason", string(StalenessSourceChanged)))
		stalenessChecksTotal.WithLabelValues(string(StalenessSourceChanged)).Inc()
		return StalenessSourceChanged, nil
	}

	if entry.SourceHash != currentHash {
		slog.Info("GR-42: cache stale due to source file changes",
			slog.String("graph_id", entry.GraphID),
			slog.String("cached_hash", truncateHash(entry.SourceHash)),
			slog.String("current_hash", truncateHash(currentHash)),
			slog.Int("file_count", fileCount),
		)
		span.SetAttributes(attribute.String("staleness_reason", string(StalenessSourceChanged)))
		stalenessChecksTotal.WithLabelValues(string(StalenessSourceChanged)).Inc()
		return StalenessSourceChanged, nil
	}

	span.SetAttributes(attribute.String("staleness_reason", string(StalenessNone)))
	stalenessChecksTotal.WithLabelValues(string(StalenessNone)).Inc()
	return StalenessNone, nil
}

// truncateHash safely truncates a hash for logging.
func truncateHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16] + "..."
	}
	return hash
}

// get retrieves a cached hash if still valid.
func (c *sourceHashCache) get(root string) (cachedHash, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.hashes[root]
	if !ok {
		return cachedHash{}, false
	}

	// H4 Fix: Use time.Since for safer TTL check (avoids overflow)
	age := time.Since(time.UnixMilli(cached.computedAt))
	if age > c.ttl {
		return cachedHash{}, false
	}

	return cached, true
}

// set stores a computed hash and triggers cleanup if needed.
func (c *sourceHashCache) set(root, hash string, fileCount int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hashes[root] = cachedHash{
		hash:       hash,
		fileCount:  fileCount,
		computedAt: time.Now().UnixMilli(),
	}

	// A3 Fix: Cleanup old entries if cache is too large
	now := time.Now().UnixMilli()
	if len(c.hashes) > c.maxCacheSize && time.Duration(now-c.lastCleanup)*time.Millisecond > c.cleanupPeriod {
		c.cleanupLocked()
		c.lastCleanup = now
	}
}

// cleanupLocked removes expired entries from the cache. Must hold write lock.
// A3 Fix: Prevents unbounded cache growth.
func (c *sourceHashCache) cleanupLocked() {
	var toDelete []string

	for root, cached := range c.hashes {
		age := time.Since(time.UnixMilli(cached.computedAt))
		if age > c.ttl {
			toDelete = append(toDelete, root)
		}
	}

	for _, root := range toDelete {
		delete(c.hashes, root)
	}

	if len(toDelete) > 0 {
		slog.Debug("GR-42: cleaned up hash cache",
			slog.Int("removed", len(toDelete)),
			slog.Int("remaining", len(c.hashes)),
		)
	}
}

// InvalidateHashCache clears the source hash cache for a project.
// Called when files are known to have changed.
func InvalidateHashCache(root string) {
	globalHashCache.mu.Lock()
	defer globalHashCache.mu.Unlock()
	delete(globalHashCache.hashes, root)
}

// ClearHashCache clears the entire source hash cache.
// Useful for testing.
func ClearHashCache() {
	globalHashCache.mu.Lock()
	defer globalHashCache.mu.Unlock()
	globalHashCache.hashes = make(map[string]cachedHash)
}

// HashCacheSize returns the current number of entries in the hash cache.
// Useful for monitoring.
func HashCacheSize() int {
	globalHashCache.mu.RLock()
	defer globalHashCache.mu.RUnlock()
	return len(globalHashCache.hashes)
}
