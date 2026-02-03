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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ManagerOption is a functional option for configuring ManifestManager.
type ManagerOption func(*ManifestManager)

// ManifestManager provides file manifest creation and comparison.
//
// Thread Safety: ManifestManager is safe for concurrent use.
type ManifestManager struct {
	hasher         Hasher
	matcher        *GlobMatcher
	maxFileSize    int64
	followSymlinks bool
	maxRetries     int
}

// NewManifestManager creates a new ManifestManager with the given options.
//
// Default configuration:
//   - maxFileSize: 100MB
//   - followSymlinks: false
//   - maxRetries: 3
//   - includes: DefaultIncludes
//   - excludes: DefaultExcludes
func NewManifestManager(opts ...ManagerOption) *ManifestManager {
	m := &ManifestManager{
		maxFileSize:    DefaultMaxFileSize,
		followSymlinks: false,
		maxRetries:     DefaultMaxRetries,
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Create hasher if not set
	if m.hasher == nil {
		m.hasher = NewSHA256Hasher(m.maxFileSize)
	}

	// Create matcher if not set
	if m.matcher == nil {
		m.matcher = NewGlobMatcher(DefaultIncludes, DefaultExcludes)
	}

	return m
}

// WithIncludes sets the include glob patterns.
func WithIncludes(patterns ...string) ManagerOption {
	return func(m *ManifestManager) {
		if m.matcher == nil {
			m.matcher = NewGlobMatcher(patterns, DefaultExcludes)
		} else {
			m.matcher = NewGlobMatcher(patterns, m.matcher.excludes)
		}
	}
}

// WithExcludes sets the exclude glob patterns.
func WithExcludes(patterns ...string) ManagerOption {
	return func(m *ManifestManager) {
		if m.matcher == nil {
			m.matcher = NewGlobMatcher(DefaultIncludes, patterns)
		} else {
			m.matcher = NewGlobMatcher(m.matcher.includes, patterns)
		}
	}
}

// WithMaxFileSize sets the maximum file size for hashing.
func WithMaxFileSize(bytes int64) ManagerOption {
	return func(m *ManifestManager) {
		m.maxFileSize = bytes
	}
}

// WithFollowSymlinks enables or disables following symlinks.
func WithFollowSymlinks(follow bool) ManagerOption {
	return func(m *ManifestManager) {
		m.followSymlinks = follow
	}
}

// WithHasher sets a custom hasher implementation.
func WithHasher(h Hasher) ManagerOption {
	return func(m *ManifestManager) {
		m.hasher = h
	}
}

// WithMaxRetries sets the maximum retry count for atomic hashing.
func WithMaxRetries(n int) ManagerOption {
	return func(m *ManifestManager) {
		m.maxRetries = n
	}
}

// inodeKey uniquely identifies a file for cycle detection.
type inodeKey struct {
	dev uint64
	ino uint64
}

// getInodeKey extracts the inode key from file info.
func getInodeKey(info os.FileInfo) inodeKey {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return inodeKey{
			dev: uint64(stat.Dev),
			ino: stat.Ino,
		}
	}
	return inodeKey{}
}

// validatePath ensures a path is within the project root.
//
// Returns ErrPathTraversal if the path escapes the root.
func validatePath(projectRoot, path string) error {
	// Handle both relative and absolute paths
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Clean(filepath.Join(projectRoot, path))
	}

	// Get relative path from project root
	rel, err := filepath.Rel(projectRoot, absPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathTraversal, err)
	}

	// Check for escape
	if strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("%w: %s escapes root", ErrPathTraversal, path)
	}

	return nil
}

// Scan walks a project directory and creates a manifest of all matching files.
//
// Description:
//
//	Recursively walks the directory, applying include/exclude patterns,
//	and computes hashes for matching files. Non-fatal errors (permission
//	denied, large files) are recorded in the manifest's Errors field.
//
// Inputs:
//
//	ctx - Context for cancellation. If cancelled, returns partial manifest.
//	root - Absolute path to the project root directory.
//
// Outputs:
//
//	*Manifest - The scan result. Never nil.
//	error - Non-nil if root is invalid or cannot be accessed.
//
// Behavior:
//
//   - Symlinks are NOT followed unless WithFollowSymlinks(true)
//   - Files larger than maxFileSize are skipped (added to Errors)
//   - Permission errors are recorded but don't stop scanning
//   - Context cancellation sets Incomplete=true and returns partial result
func (m *ManifestManager) Scan(ctx context.Context, root string) (*Manifest, error) {
	// Validate root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: not a directory", ErrInvalidRoot)
	}

	manifest := NewManifest(absRoot)
	visited := make(map[inodeKey]bool)

	err = m.scanDir(ctx, absRoot, absRoot, manifest, visited)
	if err != nil {
		// Check if it was a cancellation
		if ctx.Err() != nil {
			manifest.Incomplete = true
			return manifest, nil
		}
		return manifest, err
	}

	manifest.UpdatedAtMilli = time.Now().UnixMilli()
	return manifest, nil
}

// scanDir recursively scans a directory.
func (m *ManifestManager) scanDir(ctx context.Context, root, dir string, manifest *Manifest, visited map[inodeKey]bool) error {
	// Check cancellation
	select {
	case <-ctx.Done():
		manifest.Incomplete = true
		return ctx.Err()
	default:
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		relPath, _ := filepath.Rel(root, dir)
		manifest.Errors = append(manifest.Errors, ScanError{Path: relPath, Err: err})
		return nil // Continue scanning other directories
	}

	for _, entry := range entries {
		// Check cancellation
		select {
		case <-ctx.Done():
			manifest.Incomplete = true
			return ctx.Err()
		default:
		}

		path := filepath.Join(dir, entry.Name())
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			manifest.Errors = append(manifest.Errors, ScanError{Path: path, Err: err})
			continue
		}

		// Convert to forward slashes for consistent matching
		relPathSlash := filepath.ToSlash(relPath)

		// Get file info (Lstat to not follow symlinks)
		info, err := os.Lstat(path)
		if err != nil {
			manifest.Errors = append(manifest.Errors, ScanError{Path: relPath, Err: err})
			continue
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			if !m.followSymlinks {
				continue // Skip symlinks
			}

			// Resolve symlink target
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				manifest.Errors = append(manifest.Errors, ScanError{Path: relPath, Err: err})
				continue
			}

			// Check if target is within root
			if err := validatePath(root, target); err != nil {
				manifest.Errors = append(manifest.Errors, ScanError{
					Path: relPath,
					Err:  fmt.Errorf("symlink target outside root: %s", target),
				})
				continue
			}

			// Get info of target
			targetInfo, err := os.Stat(target)
			if err != nil {
				manifest.Errors = append(manifest.Errors, ScanError{Path: relPath, Err: err})
				continue
			}

			// Check for cycle
			key := getInodeKey(targetInfo)
			if visited[key] {
				manifest.Errors = append(manifest.Errors, ScanError{
					Path: relPath,
					Err:  fmt.Errorf("%w: %s", ErrSymlinkCycle, target),
				})
				continue
			}
			visited[key] = true

			info = targetInfo
			path = target
		}

		if info.IsDir() {
			// Always recurse into directories (filtering happens at file level)
			// Only skip explicitly excluded directories
			isExcluded := false
			for _, pattern := range m.matcher.excludes {
				if matchGlob(pattern, relPathSlash) || matchGlob(pattern, relPathSlash+"/") {
					isExcluded = true
					break
				}
			}
			if !isExcluded {
				if err := m.scanDir(ctx, root, path, manifest, visited); err != nil {
					return err
				}
			}
			continue
		}

		// Check if file matches patterns
		if !m.matcher.Match(relPathSlash) {
			continue
		}

		// Check file size
		if m.maxFileSize > 0 && info.Size() > m.maxFileSize {
			manifest.Errors = append(manifest.Errors, ScanError{
				Path: relPath,
				Err:  fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size()),
			})
			continue
		}

		// Hash file atomically
		entry, err := m.hasher.HashFileAtomic(path, m.maxRetries)
		if err != nil {
			manifest.Errors = append(manifest.Errors, ScanError{Path: relPath, Err: err})
			continue
		}

		// Store with relative path
		entry.Path = relPath
		manifest.Files[relPath] = entry
	}

	return nil
}

// Diff compares two manifests and returns the changes.
//
// Description:
//
//	Compares the Files maps of old and new manifests to identify
//	added, modified, and deleted files.
//
// Inputs:
//
//	old - The previous manifest (may be nil).
//	new - The current manifest (must not be nil).
//
// Outputs:
//
//	*Changes - The differences between old and new. Never nil.
//
// Behavior:
//
//   - If old is nil, all files in new are considered added
//   - Errors fields are not compared
//   - Comparison is based on hash, not mtime
func (m *ManifestManager) Diff(old, new *Manifest) *Changes {
	changes := &Changes{
		Added:    make([]string, 0),
		Modified: make([]string, 0),
		Deleted:  make([]string, 0),
	}

	if old == nil {
		// All files in new are added
		for path := range new.Files {
			changes.Added = append(changes.Added, path)
		}
		return changes
	}

	// Find added and modified
	for path, newEntry := range new.Files {
		oldEntry, exists := old.Files[path]
		if !exists {
			changes.Added = append(changes.Added, path)
		} else if oldEntry.Hash != newEntry.Hash {
			changes.Modified = append(changes.Modified, path)
		}
	}

	// Find deleted
	for path := range old.Files {
		if _, exists := new.Files[path]; !exists {
			changes.Deleted = append(changes.Deleted, path)
		}
	}

	return changes
}

// QuickCheck determines if a file has changed since it was last hashed.
//
// Description:
//
//	Uses mtime-first optimization: checks mtime before computing hash.
//	If mtime is unchanged, assumes file is unchanged (fast path).
//	If mtime changed, recomputes hash and compares.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	root - Absolute path to the project root directory.
//	entry - The FileEntry to check.
//
// Outputs:
//
//	changed - True if the file has changed (or was deleted).
//	err - Non-nil if an unexpected error occurred.
//
// Behavior:
//
//   - If file is deleted, returns (true, nil)
//   - If mtime unchanged, returns (false, nil) without hashing
//   - If mtime changed, hashes and compares
func (m *ManifestManager) QuickCheck(ctx context.Context, root string, entry FileEntry) (changed bool, err error) {
	// Validate path
	if err := validatePath(root, entry.Path); err != nil {
		return false, err
	}

	absPath := filepath.Join(root, entry.Path)

	// Stat file
	info, err := os.Lstat(absPath)
	if os.IsNotExist(err) {
		return true, nil // Deleted
	}
	if err != nil {
		return false, err
	}

	// Check mtime (fast path)
	currentMtime := info.ModTime().UnixNano()
	if currentMtime == entry.Mtime && info.Size() == entry.Size {
		return false, nil // Unchanged
	}

	// mtime changed, need to hash
	newEntry, err := m.hasher.HashFileAtomic(absPath, m.maxRetries)
	if err != nil {
		return false, err
	}

	return newEntry.Hash != entry.Hash, nil
}
