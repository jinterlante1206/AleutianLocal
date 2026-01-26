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

import "testing"

func TestGlobMatcher_Match(t *testing.T) {
	tests := []struct {
		name     string
		includes []string
		excludes []string
		path     string
		want     bool
	}{
		// Basic includes
		{
			name:     "no patterns includes all",
			includes: nil,
			excludes: nil,
			path:     "src/main.go",
			want:     true,
		},
		{
			name:     "simple include matches",
			includes: []string{"*.go"},
			excludes: nil,
			path:     "main.go",
			want:     true,
		},
		{
			name:     "simple include rejects non-match",
			includes: []string{"*.go"},
			excludes: nil,
			path:     "main.py",
			want:     false,
		},

		// Recursive patterns
		{
			name:     "** matches deeply nested",
			includes: []string{"**/*.go"},
			excludes: nil,
			path:     "a/b/c/main.go",
			want:     true,
		},
		{
			name:     "** matches at root",
			includes: []string{"**/*.go"},
			excludes: nil,
			path:     "main.go",
			want:     true,
		},

		// Excludes
		{
			name:     "exclude takes precedence",
			includes: []string{"**/*.go"},
			excludes: []string{"vendor/**"},
			path:     "vendor/dep/file.go",
			want:     false,
		},
		{
			name:     "non-matching exclude allows",
			includes: []string{"**/*.go"},
			excludes: []string{"vendor/**"},
			path:     "src/main.go",
			want:     true,
		},
		{
			name:     "node_modules excluded",
			includes: []string{"**/*.js"},
			excludes: []string{"node_modules/**"},
			path:     "node_modules/pkg/index.js",
			want:     false,
		},

		// Complex patterns
		{
			name:     "prefix pattern matches",
			includes: []string{"src/**/*.go"},
			excludes: nil,
			path:     "src/handlers/user.go",
			want:     true,
		},
		{
			name:     "prefix pattern rejects outside",
			includes: []string{"src/**/*.go"},
			excludes: nil,
			path:     "pkg/handlers/user.go",
			want:     false,
		},

		// Test file patterns
		{
			name:     "test files excluded",
			includes: []string{"**/*.go"},
			excludes: []string{"**/*_test.go"},
			path:     "main_test.go",
			want:     false,
		},
		{
			name:     "non-test files included",
			includes: []string{"**/*.go"},
			excludes: []string{"**/*_test.go"},
			path:     "main.go",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := NewGlobMatcher(tt.includes, tt.excludes)
			got := matcher.Match(tt.path)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Simple wildcards
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"*.go", "dir/main.go", true}, // matches filename main.go

		// Double star
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c/main.go", true},
		{"vendor/**", "vendor/pkg/file.go", true},
		{"**/test/**", "a/test/b/c.go", true},

		// Prefix matching
		{"src/**", "src/main.go", true},
		{"src/**", "pkg/main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.path)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestDefaultPatterns(t *testing.T) {
	t.Run("DefaultIncludes is not empty", func(t *testing.T) {
		if len(DefaultIncludes) == 0 {
			t.Error("DefaultIncludes is empty")
		}
	})

	t.Run("DefaultExcludes is not empty", func(t *testing.T) {
		if len(DefaultExcludes) == 0 {
			t.Error("DefaultExcludes is empty")
		}
	})

	t.Run("defaults cover common patterns", func(t *testing.T) {
		matcher := NewGlobMatcher(DefaultIncludes, DefaultExcludes)

		// Should include
		shouldInclude := []string{
			"main.go",
			"src/handlers/user.go",
			"app/page.tsx",
			"styles/main.css",
		}
		for _, path := range shouldInclude {
			if !matcher.Match(path) {
				t.Errorf("expected %q to be included", path)
			}
		}

		// Should exclude
		shouldExclude := []string{
			"vendor/pkg/file.go",
			"node_modules/react/index.js",
			".git/config",
		}
		for _, path := range shouldExclude {
			if matcher.Match(path) {
				t.Errorf("expected %q to be excluded", path)
			}
		}
	})
}
