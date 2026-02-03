// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package transaction

import (
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []WorktreeEntry
	}{
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name: "single worktree with branch",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main
`,
			expected: []WorktreeEntry{
				{
					Path:   "/path/to/main",
					HEAD:   "abc123def456",
					Branch: "main",
					Locked: false,
				},
			},
		},
		{
			name: "multiple worktrees",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main

worktree /path/to/feature
HEAD def456abc123
branch refs/heads/feature-branch
`,
			expected: []WorktreeEntry{
				{
					Path:   "/path/to/main",
					HEAD:   "abc123def456",
					Branch: "main",
					Locked: false,
				},
				{
					Path:   "/path/to/feature",
					HEAD:   "def456abc123",
					Branch: "feature-branch",
					Locked: false,
				},
			},
		},
		{
			name: "detached worktree",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main

worktree /path/to/detached
HEAD def456abc123
detached
`,
			expected: []WorktreeEntry{
				{
					Path:   "/path/to/main",
					HEAD:   "abc123def456",
					Branch: "main",
					Locked: false,
				},
				{
					Path:   "/path/to/detached",
					HEAD:   "def456abc123",
					Branch: "",
					Locked: false,
				},
			},
		},
		{
			name: "locked worktree",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main

worktree /path/to/locked
HEAD def456abc123
detached
locked
`,
			expected: []WorktreeEntry{
				{
					Path:   "/path/to/main",
					HEAD:   "abc123def456",
					Branch: "main",
					Locked: false,
				},
				{
					Path:   "/path/to/locked",
					HEAD:   "def456abc123",
					Branch: "",
					Locked: true,
				},
			},
		},
		{
			name: "worktree without trailing newline",
			input: `worktree /path/to/main
HEAD abc123def456
branch refs/heads/main`,
			expected: []WorktreeEntry{
				{
					Path:   "/path/to/main",
					HEAD:   "abc123def456",
					Branch: "main",
					Locked: false,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseWorktreeList(tc.input)

			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d entries, got %d", len(tc.expected), len(result))
			}

			for i, expected := range tc.expected {
				actual := result[i]
				if actual.Path != expected.Path {
					t.Errorf("entry %d: expected Path=%q, got %q", i, expected.Path, actual.Path)
				}
				if actual.HEAD != expected.HEAD {
					t.Errorf("entry %d: expected HEAD=%q, got %q", i, expected.HEAD, actual.HEAD)
				}
				if actual.Branch != expected.Branch {
					t.Errorf("entry %d: expected Branch=%q, got %q", i, expected.Branch, actual.Branch)
				}
				if actual.Locked != expected.Locked {
					t.Errorf("entry %d: expected Locked=%v, got %v", i, expected.Locked, actual.Locked)
				}
			}
		})
	}
}

func TestNewGitClient(t *testing.T) {
	t.Run("requires absolute path", func(t *testing.T) {
		_, err := NewGitClient("relative/path", 0)
		if err == nil {
			t.Error("expected error for relative path")
		}
	})

	t.Run("creates client with absolute path", func(t *testing.T) {
		client, err := NewGitClient("/absolute/path", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("sets default timeout", func(t *testing.T) {
		client, err := NewGitClient("/absolute/path", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Default timeout is 30 seconds
		if client.timeout.Seconds() != 30 {
			t.Errorf("expected default timeout of 30s, got %v", client.timeout)
		}
	})
}
