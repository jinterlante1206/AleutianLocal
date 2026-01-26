// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package verify provides hash-verified operations for code graphs.
//
// The verify package implements "paranoid mode" for detecting stale data
// before graph operations. It uses multiple optimization layers:
//   - mtime-first filter (99% of checks become sub-millisecond)
//   - Query-scoped verification (only verify files involved in query)
//   - Parallel verification (check multiple files concurrently)
//   - Verification TTL cache (skip re-verification within short window)
//
// # Staleness Detection
//
// Files can become stale through:
//   - Agent's own edits (graph immediately outdated)
//   - Git operations (checkout, pull, merge)
//   - External editors (user edits in VS Code)
//   - Build tools (npm install, go generate)
//
// # Thread Safety
//
// All types in this package are safe for concurrent use unless documented otherwise.
package verify

import (
	"crypto/subtle"
	"fmt"
	"time"
)

// Default configuration values.
const (
	// DefaultVerificationTTL is how long to cache verification results.
	DefaultVerificationTTL = 500 * time.Millisecond

	// DefaultMtimeResolution is the minimum mtime granularity to trust.
	// Some filesystems (FAT32, NFS) have low resolution (1-2 seconds).
	DefaultMtimeResolution = 2 * time.Second

	// DefaultParallelLimit is the maximum concurrent file verifications.
	DefaultParallelLimit = 10
)

// VerifyStatus represents the result of verification.
type VerifyStatus int

const (
	// StatusFresh indicates all checked files are unchanged.
	StatusFresh VerifyStatus = iota

	// StatusStale indicates one or more files have changed or been deleted.
	StatusStale

	// StatusPartiallyStale indicates some files are stale, some are fresh.
	StatusPartiallyStale

	// StatusError indicates verification failed due to errors.
	StatusError
)

// String returns a human-readable status description.
func (s VerifyStatus) String() string {
	switch s {
	case StatusFresh:
		return "fresh"
	case StatusStale:
		return "stale"
	case StatusPartiallyStale:
		return "partially_stale"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// RebuildStrategy determines how to handle stale files.
type RebuildStrategy int

const (
	// StrategyNone indicates no rebuild is needed.
	StrategyNone RebuildStrategy = iota

	// StrategyInlineSilent indicates 1-3 stale files, rebuild silently.
	StrategyInlineSilent

	// StrategyInlineWithStatus indicates 4-10 stale files, show status message.
	StrategyInlineWithStatus

	// StrategyBackgroundPartial indicates 11-50 stale files or >20% of files,
	// rebuild in background with progress updates.
	StrategyBackgroundPartial

	// StrategyFullRebuild indicates >50% stale files, full rebuild from scratch.
	StrategyFullRebuild
)

// String returns a human-readable strategy description.
func (s RebuildStrategy) String() string {
	switch s {
	case StrategyNone:
		return "none"
	case StrategyInlineSilent:
		return "inline_silent"
	case StrategyInlineWithStatus:
		return "inline_with_status"
	case StrategyBackgroundPartial:
		return "background_partial"
	case StrategyFullRebuild:
		return "full_rebuild"
	default:
		return "unknown"
	}
}

// VerifyResult contains the results of file verification.
type VerifyResult struct {
	// Status is the overall verification status.
	Status VerifyStatus

	// StaleFiles contains paths of files whose content has changed.
	StaleFiles []string

	// DeletedFiles contains paths of files that no longer exist.
	DeletedFiles []string

	// Errors contains files that couldn't be verified.
	Errors []FileVerifyError

	// AllFresh is true if all checked files are unchanged.
	AllFresh bool

	// CheckedAt is when the verification was performed.
	CheckedAt time.Time

	// Duration is how long the verification took.
	Duration time.Duration

	// FilesChecked is the number of files verified.
	FilesChecked int
}

// HasChanges returns true if any files are stale or deleted.
func (r *VerifyResult) HasChanges() bool {
	return len(r.StaleFiles) > 0 || len(r.DeletedFiles) > 0
}

// StaleCount returns the total number of changed/deleted files.
func (r *VerifyResult) StaleCount() int {
	return len(r.StaleFiles) + len(r.DeletedFiles)
}

// FileVerifyError represents an error verifying a specific file.
type FileVerifyError struct {
	// Path is the file that couldn't be verified.
	Path string

	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e FileVerifyError) Error() string {
	return fmt.Sprintf("verify %s: %v", e.Path, e.Err)
}

// ErrStaleData is returned when verification detects stale files.
type ErrStaleData struct {
	// StaleFiles contains paths of modified files.
	StaleFiles []string

	// DeletedFiles contains paths of deleted files.
	DeletedFiles []string
}

// Error implements the error interface.
func (e *ErrStaleData) Error() string {
	return fmt.Sprintf("stale data: %d stale, %d deleted",
		len(e.StaleFiles), len(e.DeletedFiles))
}

// HasChanges returns true if there are any stale or deleted files.
func (e *ErrStaleData) HasChanges() bool {
	return len(e.StaleFiles) > 0 || len(e.DeletedFiles) > 0
}

// ErrVerificationFailed is returned when verification encounters errors.
type ErrVerificationFailed struct {
	// Errors contains individual file verification errors.
	Errors []FileVerifyError
}

// Error implements the error interface.
func (e *ErrVerificationFailed) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("verification failed for %d files", len(e.Errors))
}

// RebuildProgress reports the status of a rebuild operation.
type RebuildProgress struct {
	// Strategy is the rebuild strategy being used.
	Strategy RebuildStrategy

	// TotalFiles is the total number of files to rebuild.
	TotalFiles int

	// Completed is the number of files successfully rebuilt.
	Completed int

	// Failed is the number of files that failed to rebuild.
	Failed int

	// StartedAt is when the rebuild started.
	StartedAt time.Time
}

// Percent returns the completion percentage.
func (p RebuildProgress) Percent() float64 {
	if p.TotalFiles == 0 {
		return 100.0
	}
	return float64(p.Completed+p.Failed) / float64(p.TotalFiles) * 100.0
}

// Elapsed returns the time since the rebuild started.
func (p RebuildProgress) Elapsed() time.Duration {
	return time.Since(p.StartedAt)
}

// RebuildCallback is called to report rebuild progress.
type RebuildCallback func(progress RebuildProgress)

// hashesEqual performs constant-time comparison of hex hash strings.
//
// Description:
//
//	Compares two hash strings using constant-time comparison to prevent
//	timing side-channel attacks. While the risk is low (attacker needs
//	local access), this provides defense-in-depth.
//
// Inputs:
//
//	a, b - Hash strings to compare. Both should be hex-encoded SHA256 hashes.
//
// Outputs:
//
//	bool - True if hashes are equal.
//
// Thread Safety:
//
//	Safe for concurrent use.
func hashesEqual(a, b string) bool {
	// Convert to bytes for constant-time compare
	aBytes := []byte(a)
	bBytes := []byte(b)

	// subtle.ConstantTimeCompare requires same length
	if len(aBytes) != len(bBytes) {
		return false
	}

	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1
}
