// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package cache provides ephemeral graph caching with LRU eviction.
//
// The cache package implements an in-memory cache for code graphs with:
//   - Reference counting for safe eviction
//   - Copy-on-write incremental updates
//   - Singleflight deduplication of concurrent builds
//   - LRU eviction with configurable limits
//
// # Design Principles
//
// Graphs are ephemeral and always rebuildable from source code files.
// The cache is a performance optimization, not a source of truth.
//
// # Thread Safety
//
// GraphCache is safe for concurrent use. Individual CacheEntry structs
// are safe for concurrent reads but require the entry's mutex for writes.
package cache

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for cache operations.
var (
	// ErrCacheEntryInUse is returned when attempting to evict an entry
	// that has active references.
	ErrCacheEntryInUse = errors.New("cache entry in use")

	// ErrEntryNotFound is returned when the requested entry doesn't exist.
	ErrEntryNotFound = errors.New("cache entry not found")

	// ErrCacheStale is returned when an entry has been marked as stale.
	ErrCacheStale = errors.New("cache entry is stale")
)

// ErrBuildFailed wraps a build error with timing information.
//
// When a build fails, the error is cached to prevent retry storms.
// This error type includes when the failure occurred and when
// a retry is allowed.
type ErrBuildFailed struct {
	// Err is the underlying build error.
	Err error

	// FailedAt is when the build failed.
	FailedAt time.Time

	// RetryAt is when a retry is allowed.
	RetryAt time.Time
}

// Error implements the error interface.
func (e *ErrBuildFailed) Error() string {
	return fmt.Sprintf("build failed at %v (retry after %v): %v",
		e.FailedAt.Format(time.RFC3339), e.RetryAt.Format(time.RFC3339), e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ErrBuildFailed) Unwrap() error {
	return e.Err
}

// CanRetry returns true if the retry time has passed.
func (e *ErrBuildFailed) CanRetry() bool {
	return time.Now().After(e.RetryAt)
}
