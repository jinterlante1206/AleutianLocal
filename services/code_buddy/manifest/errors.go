// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package manifest provides file manifest and hash tracking for cache invalidation.
//
// This package implements a ManifestManager that scans directories, computes
// file hashes (SHA256), and detects changes between scans. It is used by the
// graph cache (CB-12) and hash verification (CB-14) subsystems.
//
// # Design Principles
//
// Security is paramount - all paths are validated to prevent directory traversal.
// Performance is optimized with mtime-first checking before computing hashes.
// Reliability is ensured through atomic hash computation with TOCTOU protection.
//
// # Thread Safety
//
// ManifestManager is safe for concurrent use. Individual Manifest structs are
// NOT safe for concurrent modification after creation.
package manifest

import (
	"errors"
	"fmt"
)

// Sentinel errors for manifest operations.
var (
	// ErrPathTraversal is returned when a path escapes the project root.
	// This is a security error that prevents access to files outside the
	// validated project boundary.
	ErrPathTraversal = errors.New("path escapes project root")

	// ErrFileTooLarge is returned when a file exceeds MaxFileSize.
	// Large files are skipped to prevent memory exhaustion during hashing.
	ErrFileTooLarge = errors.New("file too large to hash")

	// ErrFileUnstable is returned when a file changes during hashing after
	// exhausting all retry attempts. This indicates the file is being actively
	// written to and cannot be reliably hashed.
	ErrFileUnstable = errors.New("file changed during hashing")

	// ErrInvalidHash is returned when a stored hash is malformed.
	// Valid hashes are exactly 64 lowercase hexadecimal characters.
	ErrInvalidHash = errors.New("invalid hash format")

	// ErrSymlinkCycle is returned when symlink following detects a cycle.
	// This prevents infinite loops when traversing symlinked directories.
	ErrSymlinkCycle = errors.New("symlink cycle detected")

	// ErrInvalidRoot is returned when the project root path is invalid.
	ErrInvalidRoot = errors.New("invalid project root")
)

// ScanError represents a non-fatal error during scanning.
//
// When a file cannot be processed (e.g., permission denied), it is recorded
// as a ScanError and scanning continues. The Manifest's Errors field contains
// all such errors.
type ScanError struct {
	// Path is the relative path to the file that failed.
	Path string `json:"path"`

	// Err is the underlying error.
	Err error `json:"error"`
}

// Error implements the error interface.
func (e ScanError) Error() string {
	return fmt.Sprintf("scan %s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e ScanError) Unwrap() error {
	return e.Err
}

// MarshalJSON implements json.Marshaler.
// Serializes the error as its string representation.
func (e ScanError) MarshalJSON() ([]byte, error) {
	type jsonScanError struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	return []byte(fmt.Sprintf(`{"path":%q,"error":%q}`, e.Path, e.Err.Error())), nil
}
