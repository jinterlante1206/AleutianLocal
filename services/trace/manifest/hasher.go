// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Default configuration values.
const (
	// DefaultMaxFileSize is the default maximum file size for hashing (100MB).
	DefaultMaxFileSize = 100 * 1024 * 1024

	// DefaultMaxRetries is the default number of retries for atomic hashing.
	DefaultMaxRetries = 3
)

// Hasher defines the interface for file hashing operations.
type Hasher interface {
	// HashFile computes SHA256 of file contents.
	//
	// Returns lowercase hex string (64 chars) on success.
	// Returns error if file cannot be read or exceeds size limit.
	HashFile(path string) (string, error)

	// HashFileAtomic computes hash with TOCTOU protection.
	//
	// Verifies that mtime is unchanged after hashing. If the file changes
	// during hashing, the operation is retried up to maxRetries times.
	//
	// Returns a complete FileEntry on success, including hash, mtime, and size.
	// Returns ErrFileUnstable if the file keeps changing after all retries.
	HashFileAtomic(path string, maxRetries int) (FileEntry, error)
}

// SHA256Hasher implements Hasher using SHA256.
type SHA256Hasher struct {
	// maxFileSize is the maximum file size in bytes (0 = no limit).
	maxFileSize int64
}

// NewSHA256Hasher creates a new SHA256Hasher with the given size limit.
//
// If maxFileSize is 0, no size limit is enforced.
// If maxFileSize is negative, it defaults to DefaultMaxFileSize.
func NewSHA256Hasher(maxFileSize int64) *SHA256Hasher {
	if maxFileSize < 0 {
		maxFileSize = DefaultMaxFileSize
	}
	return &SHA256Hasher{
		maxFileSize: maxFileSize,
	}
}

// HashFile computes SHA256 of file contents.
//
// Description:
//
//	Opens the file, reads its contents, and computes the SHA256 hash.
//	Uses streaming to avoid loading the entire file into memory.
//
// Inputs:
//
//	path - Absolute or relative path to the file.
//
// Outputs:
//
//	string - Lowercase hexadecimal hash (64 characters).
//	error - Non-nil if file cannot be read or exceeds size limit.
//
// Errors:
//
//	ErrFileTooLarge - File size exceeds maxFileSize limit.
//	os errors - File doesn't exist, permission denied, etc.
func (h *SHA256Hasher) HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Check size if limit set
	if h.maxFileSize > 0 {
		stat, err := f.Stat()
		if err != nil {
			return "", err
		}
		if stat.Size() > h.maxFileSize {
			return "", fmt.Errorf("%w: %d bytes > %d limit",
				ErrFileTooLarge, stat.Size(), h.maxFileSize)
		}
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// HashFileAtomic computes hash with TOCTOU race detection.
//
// Description:
//
//	Computes the file hash while verifying that the file hasn't changed
//	during the operation. Uses stat-before and stat-after to detect
//	modifications. Retries if the file changes during hashing.
//
// Inputs:
//
//	path - Absolute or relative path to the file.
//	maxRetries - Maximum number of retry attempts (0 means no retries).
//
// Outputs:
//
//	FileEntry - Complete entry with path, hash, mtime, and size.
//	error - Non-nil if hashing fails after all retries.
//
// Errors:
//
//	ErrFileUnstable - File changed during hashing after all retries.
//	ErrFileTooLarge - File size exceeds maxFileSize limit.
//	os errors - File doesn't exist, permission denied, etc.
//
// Algorithm:
//
//  1. Lstat file to get initial mtime and size
//  2. Compute hash (streaming)
//  3. Lstat file again to get final mtime and size
//  4. If mtime and size unchanged, return FileEntry
//  5. If changed, retry (up to maxRetries)
//  6. If still changing after retries, return ErrFileUnstable
func (h *SHA256Hasher) HashFileAtomic(path string, maxRetries int) (FileEntry, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Stat before
		stat1, err := os.Lstat(path)
		if err != nil {
			return FileEntry{}, err
		}

		// Size check
		if h.maxFileSize > 0 && stat1.Size() > h.maxFileSize {
			return FileEntry{}, fmt.Errorf("%w: %d bytes", ErrFileTooLarge, stat1.Size())
		}

		// Hash
		hash, err := h.HashFile(path)
		if err != nil {
			return FileEntry{}, err
		}

		// Stat after
		stat2, err := os.Lstat(path)
		if err != nil {
			return FileEntry{}, err
		}

		// Check mtime and size unchanged
		if stat1.ModTime().Equal(stat2.ModTime()) && stat1.Size() == stat2.Size() {
			return FileEntry{
				Path:  path,
				Hash:  hash,
				Mtime: stat2.ModTime().UnixNano(),
				Size:  stat2.Size(),
			}, nil
		}

		// File changed during hashing, retry
	}

	return FileEntry{}, fmt.Errorf("%w: changed %d times during hashing",
		ErrFileUnstable, maxRetries+1)
}
