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
	"fmt"
	"time"
)

// Manifest represents the state of all tracked files in a project.
//
// A Manifest is created by scanning a project directory and recording
// the hash and metadata for each file. Manifests are compared using
// Diff() to detect changes.
type Manifest struct {
	// ProjectRoot is the absolute path to the project root directory.
	ProjectRoot string `json:"project_root"`

	// Files maps relative file paths to their entries.
	// Keys are relative paths from ProjectRoot.
	Files map[string]FileEntry `json:"files"`

	// Errors contains files that failed during scanning.
	// These files are not included in the Files map.
	Errors []ScanError `json:"errors,omitempty"`

	// CreatedAtMilli is the Unix timestamp in milliseconds when the
	// manifest was first created.
	CreatedAtMilli int64 `json:"created_at_milli"`

	// UpdatedAtMilli is the Unix timestamp in milliseconds when the
	// manifest was last updated.
	UpdatedAtMilli int64 `json:"updated_at_milli"`

	// Incomplete is true if the scan was cancelled before completion.
	// When true, the Files map contains only a partial result.
	Incomplete bool `json:"incomplete,omitempty"`
}

// NewManifest creates an empty manifest for the given project root.
func NewManifest(projectRoot string) *Manifest {
	now := time.Now().UnixMilli()
	return &Manifest{
		ProjectRoot:    projectRoot,
		Files:          make(map[string]FileEntry),
		Errors:         make([]ScanError, 0),
		CreatedAtMilli: now,
		UpdatedAtMilli: now,
	}
}

// FileCount returns the number of successfully scanned files.
func (m *Manifest) FileCount() int {
	return len(m.Files)
}

// ErrorCount returns the number of files that failed scanning.
func (m *Manifest) ErrorCount() int {
	return len(m.Errors)
}

// HasErrors returns true if any files failed scanning.
func (m *Manifest) HasErrors() bool {
	return len(m.Errors) > 0
}

// FileEntry represents a single file in the manifest.
type FileEntry struct {
	// Path is the relative path from project root.
	Path string `json:"path"`

	// Hash is the SHA256 hash of the file contents.
	// Format: 64 lowercase hexadecimal characters.
	Hash string `json:"hash"`

	// Mtime is the file modification time in Unix nanoseconds.
	Mtime int64 `json:"mtime"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`
}

// Validate checks that the FileEntry has valid field values.
//
// Validates:
//   - Path is non-empty
//   - Hash is exactly 64 lowercase hexadecimal characters
//
// Returns nil if valid, or an error describing the validation failure.
func (e FileEntry) Validate() error {
	if e.Path == "" {
		return fmt.Errorf("empty path")
	}

	if len(e.Hash) != 64 {
		return fmt.Errorf("%w: expected 64 chars, got %d", ErrInvalidHash, len(e.Hash))
	}

	for _, c := range e.Hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("%w: invalid character %q", ErrInvalidHash, c)
		}
	}

	return nil
}

// Changes represents the differences between two manifests.
type Changes struct {
	// Added contains relative paths of files that exist in the new
	// manifest but not in the old manifest.
	Added []string `json:"added,omitempty"`

	// Modified contains relative paths of files that exist in both
	// manifests but have different hashes.
	Modified []string `json:"modified,omitempty"`

	// Deleted contains relative paths of files that exist in the old
	// manifest but not in the new manifest.
	Deleted []string `json:"deleted,omitempty"`
}

// HasChanges returns true if there are any added, modified, or deleted files.
func (c *Changes) HasChanges() bool {
	return len(c.Added) > 0 || len(c.Modified) > 0 || len(c.Deleted) > 0
}

// Count returns the total number of changes (added + modified + deleted).
func (c *Changes) Count() int {
	return len(c.Added) + len(c.Modified) + len(c.Deleted)
}

// IsEmpty returns true if there are no changes.
func (c *Changes) IsEmpty() bool {
	return !c.HasChanges()
}
