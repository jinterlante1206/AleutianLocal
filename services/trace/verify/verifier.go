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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

// Verifier performs hash-verified operations on code graphs.
//
// Thread Safety:
//
//	Verifier is safe for concurrent use.
type Verifier struct {
	manifestManager *manifest.ManifestManager
	cache           *VerificationCache
	mtimeResolution time.Duration
	parallelLimit   int
	rebuildCallback RebuildCallback
}

// VerifierOption is a functional option for configuring Verifier.
type VerifierOption func(*Verifier)

// WithMtimeResolution sets the minimum mtime granularity to trust.
//
// Description:
//
//	Some filesystems (FAT32, NFS) have low mtime resolution (1-2 seconds).
//	Files modified within this window will always be hash-verified even
//	if mtime appears unchanged.
//
// Inputs:
//
//	d - The resolution duration. Default is 2 seconds.
func WithMtimeResolution(d time.Duration) VerifierOption {
	return func(v *Verifier) {
		if d > 0 {
			v.mtimeResolution = d
		}
	}
}

// WithVerificationCache sets a custom verification cache.
func WithVerificationCache(c *VerificationCache) VerifierOption {
	return func(v *Verifier) {
		v.cache = c
	}
}

// WithParallelLimit sets the maximum concurrent file verifications.
func WithParallelLimit(limit int) VerifierOption {
	return func(v *Verifier) {
		if limit > 0 {
			v.parallelLimit = limit
		}
	}
}

// WithRebuildCallback sets the callback for rebuild progress updates.
func WithRebuildCallback(fn RebuildCallback) VerifierOption {
	return func(v *Verifier) {
		v.rebuildCallback = fn
	}
}

// WithManifestManager sets a custom manifest manager.
func WithManifestManager(m *manifest.ManifestManager) VerifierOption {
	return func(v *Verifier) {
		v.manifestManager = m
	}
}

// NewVerifier creates a new Verifier.
//
// Description:
//
//	Creates a verifier for detecting stale files in code graphs.
//	Uses mtime-first optimization for fast checks, falling back
//	to hash verification when needed.
//
// Inputs:
//
//	opts - Optional configuration.
//
// Outputs:
//
//	*Verifier - The new verifier instance.
//
// Thread Safety:
//
//	The returned verifier is safe for concurrent use.
func NewVerifier(opts ...VerifierOption) *Verifier {
	v := &Verifier{
		manifestManager: manifest.NewManifestManager(),
		cache:           NewVerificationCache(),
		mtimeResolution: DefaultMtimeResolution,
		parallelLimit:   DefaultParallelLimit,
	}

	for _, opt := range opts {
		opt(v)
	}

	return v
}

// FastVerify performs optimized verification of a single file.
//
// Description:
//
//	Uses mtime-first optimization: checks modification time before hashing.
//	If mtime is unchanged and outside the resolution window, assumes file
//	is unchanged (fast path). If mtime changed or is within resolution
//	window, computes hash and compares.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	projectRoot - Absolute path to the project root.
//	path - Relative file path within the project.
//	entry - The manifest entry containing expected hash and mtime.
//
// Outputs:
//
//	VerifyResult - The verification result.
//	error - Non-nil if verification failed due to unexpected error.
//
// Behavior:
//
//   - If file deleted → Status=StatusStale, DeletedFiles=[path]
//   - If mtime unchanged AND outside resolution window → StatusFresh
//   - If mtime in future (clock skew) → always compute hash
//   - If mtime changed OR within resolution window → compute hash
//
// Thread Safety:
//
//	Safe for concurrent use.
func (v *Verifier) FastVerify(ctx context.Context, projectRoot, path string, entry manifest.FileEntry) (VerifyResult, error) {
	absPath := filepath.Join(projectRoot, path)
	now := time.Now().UnixMilli()
	result := VerifyResult{
		CheckedAt:    now,
		FilesChecked: 1,
	}

	// Check verification cache first
	if !v.cache.NeedsVerification(path) {
		result.Status = StatusFresh
		result.AllFresh = true
		result.Duration = time.Duration(time.Now().UnixMilli()-now) * time.Millisecond
		return result, nil
	}

	// Stat file
	stat, err := os.Lstat(absPath)
	if os.IsNotExist(err) {
		result.Status = StatusStale
		result.DeletedFiles = []string{path}
		result.Duration = time.Duration(time.Now().UnixMilli()-now) * time.Millisecond
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("stat %s: %w", path, err)
	}

	fileMtime := stat.ModTime()
	entryMtime := time.Unix(0, entry.Mtime)

	// Case 1: Future mtime (clock skew) - never trust, always hash
	if fileMtime.After(time.UnixMilli(now)) {
		return v.hashVerify(ctx, projectRoot, path, entry, stat, now)
	}

	// Case 2: mtime unchanged
	if fileMtime.Equal(entryMtime) && stat.Size() == entry.Size {
		// But if within resolution window, hash anyway
		timeSinceModify := (time.Duration(now-fileMtime.UnixMilli()) * time.Millisecond)
		if timeSinceModify < v.mtimeResolution {
			return v.hashVerify(ctx, projectRoot, path, entry, stat, now)
		}

		// Safe to trust mtime
		v.cache.MarkVerified(path)
		result.Status = StatusFresh
		result.AllFresh = true
		result.Duration = time.Duration(time.Now().UnixMilli()-now) * time.Millisecond
		return result, nil
	}

	// Case 3: mtime or size changed - hash to confirm
	return v.hashVerify(ctx, projectRoot, path, entry, stat, now)
}

// hashVerify performs hash-based verification using ManifestManager.
func (v *Verifier) hashVerify(ctx context.Context, projectRoot, path string, entry manifest.FileEntry, stat os.FileInfo, startTime int64) (VerifyResult, error) {
	result := VerifyResult{
		CheckedAt:    startTime,
		FilesChecked: 1,
	}

	// Use manifest manager's QuickCheck which already handles hash comparison
	changed, err := v.manifestManager.QuickCheck(ctx, projectRoot, entry)
	if err != nil {
		return result, fmt.Errorf("quickcheck %s: %w", path, err)
	}

	if changed {
		result.Status = StatusStale
		result.StaleFiles = []string{path}
	} else {
		v.cache.MarkVerified(path)
		result.Status = StatusFresh
		result.AllFresh = true
	}

	result.Duration = time.Duration(time.Now().UnixMilli()-startTime) * time.Millisecond
	return result, nil
}

// VerifyFiles verifies multiple files in parallel.
//
// Description:
//
//	Verifies multiple files concurrently, bounded by parallelLimit.
//	Individual file errors are collected separately from staleness results.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	projectRoot - Absolute path to the project root.
//	entries - Map of path to manifest entry for files to verify.
//
// Outputs:
//
//	*VerifyResult - Aggregated verification result.
//	error - Non-nil if context was cancelled.
//
// Behavior:
//
//   - Files are checked concurrently (bounded by GOMAXPROCS or parallelLimit)
//   - Latency is max(files), not sum(files)
//   - Individual errors add to Errors, don't stop other checks
//   - Context cancellation returns partial result
//
// Thread Safety:
//
//	Safe for concurrent use.
func (v *Verifier) VerifyFiles(ctx context.Context, projectRoot string, entries map[string]manifest.FileEntry) (*VerifyResult, error) {
	startTime := time.Now().UnixMilli()
	result := &VerifyResult{
		CheckedAt:    startTime,
		StaleFiles:   make([]string, 0),
		DeletedFiles: make([]string, 0),
		Errors:       make([]FileVerifyError, 0),
	}

	if len(entries) == 0 {
		result.Status = StatusFresh
		result.AllFresh = true
		result.Duration = time.Duration(time.Now().UnixMilli()-startTime) * time.Millisecond
		return result, nil
	}

	// Collect paths to verify (skip cached)
	var pathsToVerify []string
	for path := range entries {
		if v.cache.NeedsVerification(path) {
			pathsToVerify = append(pathsToVerify, path)
		}
	}

	// If all files were recently verified, return fresh
	if len(pathsToVerify) == 0 {
		result.Status = StatusFresh
		result.AllFresh = true
		result.FilesChecked = len(entries)
		result.Duration = time.Duration(time.Now().UnixMilli()-startTime) * time.Millisecond
		return result, nil
	}

	// Create semaphore for parallel limit
	sem := make(chan struct{}, v.parallelLimit)
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Track cancellation
	cancelled := false

	for _, path := range pathsToVerify {
		// Check context before starting new goroutine
		select {
		case <-ctx.Done():
			cancelled = true
			break
		default:
		}

		if cancelled {
			break
		}

		entry := entries[path]

		wg.Add(1)
		go func(p string, e manifest.FileEntry) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			fileResult, err := v.FastVerify(ctx, projectRoot, p, e)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.Errors = append(result.Errors, FileVerifyError{
					Path: p,
					Err:  err,
				})
				return
			}

			result.StaleFiles = append(result.StaleFiles, fileResult.StaleFiles...)
			result.DeletedFiles = append(result.DeletedFiles, fileResult.DeletedFiles...)
		}(path, entry)
	}

	wg.Wait()

	// Determine final status
	result.FilesChecked = len(entries)
	result.Duration = time.Duration(time.Now().UnixMilli()-startTime) * time.Millisecond

	if len(result.Errors) > 0 && len(result.StaleFiles) == 0 && len(result.DeletedFiles) == 0 {
		result.Status = StatusError
	} else if len(result.StaleFiles) > 0 || len(result.DeletedFiles) > 0 {
		if len(result.StaleFiles)+len(result.DeletedFiles) < len(pathsToVerify) {
			result.Status = StatusPartiallyStale
		} else {
			result.Status = StatusStale
		}
	} else {
		result.Status = StatusFresh
		result.AllFresh = true
	}

	// Mark all verified files in cache
	var freshPaths []string
	for _, path := range pathsToVerify {
		isStale := false
		for _, s := range result.StaleFiles {
			if s == path {
				isStale = true
				break
			}
		}
		for _, d := range result.DeletedFiles {
			if d == path {
				isStale = true
				break
			}
		}
		for _, e := range result.Errors {
			if e.Path == path {
				isStale = true
				break
			}
		}
		if !isStale {
			freshPaths = append(freshPaths, path)
		}
	}
	if len(freshPaths) > 0 {
		v.cache.MarkVerifiedBatch(freshPaths)
	}

	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	return result, nil
}

// VerifyManifest verifies all files in a manifest against current disk state.
//
// Description:
//
//	Compares all files in the manifest against their current state on disk.
//	Uses parallel verification for performance.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	projectRoot - Absolute path to the project root.
//	m - The manifest containing expected file hashes.
//
// Outputs:
//
//	*VerifyResult - The verification result.
//	error - Non-nil if context was cancelled.
func (v *Verifier) VerifyManifest(ctx context.Context, projectRoot string, m *manifest.Manifest) (*VerifyResult, error) {
	if m == nil {
		return &VerifyResult{
			Status:    StatusFresh,
			AllFresh:  true,
			CheckedAt: time.Now().UnixMilli(),
		}, nil
	}

	return v.VerifyFiles(ctx, projectRoot, m.Files)
}

// InvalidateCache invalidates the verification cache for all files.
//
// Description:
//
//	Clears the verification cache. Should be called after operations
//	that may change many files (git checkout, pull, etc.).
func (v *Verifier) InvalidateCache() {
	v.cache.InvalidateAll()
}

// InvalidatePath invalidates the verification cache for a single file.
//
// Description:
//
//	Removes a single file from the verification cache. Should be called
//	when a file is known to have changed.
//
// Inputs:
//
//	path - The relative file path to invalidate.
func (v *Verifier) InvalidatePath(path string) {
	v.cache.Invalidate(path)
}

// Cache returns the verification cache for direct access.
func (v *Verifier) Cache() *VerificationCache {
	return v.cache
}
