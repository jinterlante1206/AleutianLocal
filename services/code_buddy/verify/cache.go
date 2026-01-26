// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"sync"
	"time"
)

// VerificationCache caches recent verification results to avoid
// redundant checks during rapid-fire queries.
//
// Thread Safety:
//
//	VerificationCache is safe for concurrent use.
type VerificationCache struct {
	mu       sync.RWMutex
	verified map[string]verifyEntry
	ttl      time.Duration
}

// verifyEntry represents a cached verification result.
type verifyEntry struct {
	// verifiedAt is when the file was last verified.
	verifiedAt time.Time
}

// CacheOption is a functional option for configuring VerificationCache.
type CacheOption func(*VerificationCache)

// WithCacheTTL sets the TTL for cached verification results.
func WithCacheTTL(d time.Duration) CacheOption {
	return func(c *VerificationCache) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// NewVerificationCache creates a new VerificationCache.
//
// Description:
//
//	Creates a cache for storing recent verification results. Files verified
//	within the TTL period will not be re-verified, improving performance
//	for rapid successive queries.
//
// Inputs:
//
//	opts - Optional configuration. Default TTL is 500ms.
//
// Outputs:
//
//	*VerificationCache - The new cache instance.
//
// Thread Safety:
//
//	The returned cache is safe for concurrent use.
func NewVerificationCache(opts ...CacheOption) *VerificationCache {
	c := &VerificationCache{
		verified: make(map[string]verifyEntry),
		ttl:      DefaultVerificationTTL,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// NeedsVerification returns true if the file should be verified.
//
// Description:
//
//	Checks if a file's cached verification has expired. Files verified
//	within the TTL period return false (no verification needed).
//
// Inputs:
//
//	path - The file path to check.
//
// Outputs:
//
//	bool - True if verification is needed, false if recently verified.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) NeedsVerification(path string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.verified[path]
	if !exists {
		return true
	}

	// Check if TTL expired
	return time.Since(entry.verifiedAt) > c.ttl
}

// MarkVerified records that a file has been verified.
//
// Description:
//
//	Records the current time as the verification time for the file.
//	Subsequent calls to NeedsVerification will return false until
//	the TTL expires.
//
// Inputs:
//
//	path - The file path that was verified.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) MarkVerified(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.verified[path] = verifyEntry{
		verifiedAt: time.Now(),
	}
}

// MarkVerifiedBatch records multiple files as verified.
//
// Description:
//
//	Efficiently records verification for multiple files with a single
//	lock acquisition.
//
// Inputs:
//
//	paths - The file paths that were verified.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) MarkVerifiedBatch(paths []string) {
	if len(paths) == 0 {
		return
	}

	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, path := range paths {
		c.verified[path] = verifyEntry{
			verifiedAt: now,
		}
	}
}

// Invalidate removes a single file from the cache.
//
// Description:
//
//	Removes the verification record for a specific file, forcing
//	re-verification on the next check.
//
// Inputs:
//
//	path - The file path to invalidate.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.verified, path)
}

// InvalidateAll clears all cached verification results.
//
// Description:
//
//	Removes all verification records. This should be called after
//	operations that may change many files (git checkout, pull, etc.).
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.verified = make(map[string]verifyEntry)
}

// Cleanup removes expired entries from the cache.
//
// Description:
//
//	Removes entries older than the TTL. This can be called periodically
//	to prevent memory growth in long-running processes.
//
// Outputs:
//
//	int - The number of entries removed.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for path, entry := range c.verified {
		if now.Sub(entry.verifiedAt) > c.ttl {
			delete(c.verified, path)
			removed++
		}
	}

	return removed
}

// Size returns the number of entries in the cache.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (c *VerificationCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.verified)
}

// TTL returns the configured TTL duration.
func (c *VerificationCache) TTL() time.Duration {
	return c.ttl
}
