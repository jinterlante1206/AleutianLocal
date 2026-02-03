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
	"path/filepath"
	"strings"
)

// Default glob patterns for common source files.
var (
	// DefaultIncludes specifies patterns for common source file types.
	DefaultIncludes = []string{
		"**/*.go",
		"**/*.py",
		"**/*.ts",
		"**/*.tsx",
		"**/*.js",
		"**/*.jsx",
		"**/*.html",
		"**/*.css",
	}

	// DefaultExcludes specifies patterns for commonly excluded directories and files.
	DefaultExcludes = []string{
		"vendor/**",
		"node_modules/**",
		".git/**",
		"**/testdata/**",
		"**/*_test.go",
		"**/*.test.ts",
		"**/*.spec.ts",
	}
)

// GlobMatcher provides file path matching against include/exclude patterns.
//
// Patterns use glob syntax with ** for recursive matching:
//   - * matches any sequence of non-separator characters
//   - ** matches any sequence of characters including separators
//   - ? matches any single non-separator character
//   - [abc] matches one of the characters in brackets
//
// Thread Safety: GlobMatcher is safe for concurrent use after creation.
type GlobMatcher struct {
	includes []string
	excludes []string
}

// NewGlobMatcher creates a matcher with the given include and exclude patterns.
//
// If includes is empty, all files are included by default.
// If excludes is empty, no files are excluded.
func NewGlobMatcher(includes, excludes []string) *GlobMatcher {
	return &GlobMatcher{
		includes: includes,
		excludes: excludes,
	}
}

// Match returns true if the path should be included.
//
// A path is included if:
//  1. It matches at least one include pattern (or includes is empty), AND
//  2. It does not match any exclude pattern
//
// The path should use forward slashes as separators for consistency.
func (m *GlobMatcher) Match(path string) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)

	// Check excludes first - if excluded, always reject
	for _, pattern := range m.excludes {
		if matchGlob(pattern, path) {
			return false
		}
	}

	// If no includes specified, include everything not excluded
	if len(m.includes) == 0 {
		return true
	}

	// Must match at least one include pattern
	for _, pattern := range m.includes {
		if matchGlob(pattern, path) {
			return true
		}
	}

	return false
}

// matchGlob matches a path against a glob pattern.
//
// Supports:
//   - * matches any non-separator characters
//   - ** matches any characters including separators (recursive)
//   - ? matches single character
//   - [abc] character class
func matchGlob(pattern, path string) bool {
	// Handle ** recursive matching
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, path)
	}

	// Simple glob matching
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	// Try matching against just the filename
	matched, _ = filepath.Match(pattern, filepath.Base(path))
	return matched
}

// matchDoublestar handles ** recursive patterns.
func matchDoublestar(pattern, path string) bool {
	// Split pattern by **
	parts := strings.Split(pattern, "**")
	if len(parts) == 1 {
		// No ** found, use regular matching
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// For "prefix/**/suffix" patterns
	if len(parts) == 2 {
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")

		// Check prefix if non-empty
		if prefix != "" {
			if !strings.HasPrefix(path, prefix+"/") && path != prefix {
				return false
			}
			// Remove prefix from path
			path = strings.TrimPrefix(path, prefix+"/")
		}

		// Check suffix if non-empty
		if suffix != "" {
			// Suffix can match anywhere in the remaining path
			return matchSuffix(suffix, path)
		}

		return true
	}

	// Complex patterns with multiple ** - try simple approach
	// Match if the non-** parts appear in order
	pathIdx := 0
	for i, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}

		// Find this part in remaining path
		idx := strings.Index(path[pathIdx:], part)
		if idx == -1 {
			return false
		}

		// First part must be at start if pattern doesn't start with **
		if i == 0 && !strings.HasPrefix(pattern, "**") && idx != 0 {
			return false
		}

		pathIdx += idx + len(part)
	}

	// Last part must be at end if pattern doesn't end with **
	if !strings.HasSuffix(pattern, "**") && pathIdx != len(path) {
		return false
	}

	return true
}

// matchSuffix checks if path ends with or contains the suffix pattern.
func matchSuffix(suffix, path string) bool {
	// If suffix contains wildcards, need more complex matching
	if strings.ContainsAny(suffix, "*?[") {
		// Check if any suffix of path matches
		parts := strings.Split(path, "/")
		for i := range parts {
			subpath := strings.Join(parts[i:], "/")
			matched, _ := filepath.Match(suffix, subpath)
			if matched {
				return true
			}
		}
		return false
	}

	// Simple string suffix check
	return strings.HasSuffix(path, suffix) || strings.Contains(path, suffix+"/") || path == suffix
}
