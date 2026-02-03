// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultCodeExtensions are common code file extensions to track.
var DefaultCodeExtensions = map[string]bool{
	".go":    true,
	".py":    true,
	".js":    true,
	".ts":    true,
	".jsx":   true,
	".tsx":   true,
	".java":  true,
	".rs":    true,
	".c":     true,
	".cpp":   true,
	".h":     true,
	".hpp":   true,
	".rb":    true,
	".php":   true,
	".swift": true,
	".kt":    true,
	".scala": true,
	".md":    true,
	".yaml":  true,
	".yml":   true,
	".json":  true,
	".toml":  true,
	".sh":    true,
}

// FileManifest tracks files that exist in a project directory.
//
// This provides a cached view of the project's file structure for
// validating that file references in LLM responses actually exist.
// The manifest can be refreshed on-demand or checked for staleness.
//
// Thread Safety: Safe for concurrent use.
type FileManifest struct {
	mu              sync.RWMutex
	files           map[string]bool // relative paths normalized with forward slashes
	basenames       map[string]bool // just the filename portion
	extensionCounts map[string]int  // count of files per extension
	root            string
	loadedAt        time.Time
	extensions      map[string]bool
}

// NewFileManifest creates a new empty file manifest.
//
// Description:
//
//	Creates an uninitialized manifest. Call ScanDir to populate it
//	with files from a directory.
//
// Inputs:
//   - extensions: File extensions to include (nil means DefaultCodeExtensions).
//
// Outputs:
//   - *FileManifest: The empty manifest.
//
// Thread Safety: Safe for concurrent use after construction.
func NewFileManifest(extensions []string) *FileManifest {
	extMap := DefaultCodeExtensions
	if len(extensions) > 0 {
		extMap = make(map[string]bool)
		for _, ext := range extensions {
			// Ensure leading dot
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extMap[strings.ToLower(ext)] = true
		}
	}

	return &FileManifest{
		files:           make(map[string]bool),
		basenames:       make(map[string]bool),
		extensionCounts: make(map[string]int),
		extensions:      extMap,
	}
}

// ScanDir populates the manifest by scanning a directory recursively.
//
// Description:
//
//	Walks the directory tree starting at root and records all files
//	matching the configured extensions. Previous contents are cleared.
//	Skips hidden directories (starting with .) and common non-code
//	directories (node_modules, vendor, __pycache__, etc.).
//
// Inputs:
//   - ctx: Context for cancellation.
//   - root: The directory to scan (absolute path).
//
// Outputs:
//   - error: Non-nil if scanning fails.
//
// Thread Safety: Safe for concurrent use (acquires write lock).
func (m *FileManifest) ScanDir(ctx context.Context, root string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing data
	m.files = make(map[string]bool)
	m.basenames = make(map[string]bool)
	m.extensionCounts = make(map[string]int)
	m.root = root
	m.loadedAt = time.Now()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			// Skip inaccessible directories/files
			return nil
		}

		name := d.Name()

		// Skip hidden directories
		if d.IsDir() && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}

		// Skip common non-code directories
		if d.IsDir() {
			switch name {
			case "node_modules", "vendor", "__pycache__", "venv", ".venv",
				"dist", "build", "target", "bin", "obj":
				return filepath.SkipDir
			}
			return nil
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(name))
		if !m.extensions[ext] {
			return nil
		}

		// Get relative path with forward slashes
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		m.files[relPath] = true
		m.basenames[name] = true
		m.extensionCounts[ext]++

		return nil
	})

	return err
}

// Contains checks if a file path exists in the manifest.
//
// Description:
//
//	Checks the manifest for the given path. Handles path normalization
//	(leading ./, /, backslashes) and checks both full path and basename.
//
// Inputs:
//   - path: The file path to check.
//
// Outputs:
//   - bool: True if the file exists in the manifest.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) Contains(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalized := normalizePath(path)

	// Check full path
	if m.files[normalized] {
		return true
	}

	// Check basename only (for cases like "main.go" without path)
	basename := filepath.Base(path)
	if m.basenames[basename] {
		return true
	}

	return false
}

// IsStale returns true if the manifest is older than the given TTL.
//
// Description:
//
//	Checks if the manifest should be refreshed based on age.
//	An uninitialized manifest (zero LoadedAt) is always stale.
//
// Inputs:
//   - ttl: The maximum age before the manifest is considered stale.
//
// Outputs:
//   - bool: True if the manifest is stale.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) IsStale(ttl time.Duration) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.loadedAt.IsZero() {
		return true
	}

	return time.Since(m.loadedAt) > ttl
}

// FileCount returns the number of files in the manifest.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) FileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.files)
}

// Root returns the root directory of the manifest.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) Root() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.root
}

// LoadedAt returns when the manifest was last loaded.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) LoadedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadedAt
}

// ToKnownFiles returns a copy of the files map for use in CheckInput.
//
// Description:
//
//	Returns a copy of the internal files map that can be used to
//	populate CheckInput.KnownFiles for grounding checks.
//
// Outputs:
//   - map[string]bool: Copy of the files map.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) ToKnownFiles() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]bool, len(m.files))
	for k, v := range m.files {
		result[k] = v
	}
	return result
}

// ExtensionCounts returns a copy of the extension count map.
//
// Description:
//
//	Returns a map of file extensions to their count in the manifest.
//	This is useful for detecting the primary language of a project.
//	Extensions include the leading dot (e.g., ".go", ".py").
//
// Outputs:
//   - map[string]int: Copy of the extension counts map.
//
// Thread Safety: Safe for concurrent use (acquires read lock).
func (m *FileManifest) ExtensionCounts() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]int, len(m.extensionCounts))
	for k, v := range m.extensionCounts {
		result[k] = v
	}
	return result
}
