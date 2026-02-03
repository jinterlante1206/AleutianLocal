// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// =============================================================================
// Mock Cache Invalidator
// =============================================================================

type mockCacheInvalidator struct {
	invalidateAllCalled   bool
	invalidateFilesCalled bool
	filesInvalidated      []string
	waitCalled            bool
}

func (m *mockCacheInvalidator) InvalidateAll() error {
	m.invalidateAllCalled = true
	return nil
}

func (m *mockCacheInvalidator) InvalidateFiles(paths []string) error {
	m.invalidateFilesCalled = true
	m.filesInvalidated = paths
	return nil
}

func (m *mockCacheInvalidator) WaitForRebuilds() error {
	m.waitCalled = true
	return nil
}

// =============================================================================
// Command Classification Tests
// =============================================================================

func TestClassifyCommand(t *testing.T) {
	ctx := context.Background()
	workDir := "."

	tests := []struct {
		name     string
		args     []string
		expected InvalidationType
	}{
		// Full invalidation commands
		{"checkout branch", []string{"checkout", "main"}, InvalidationFull},
		{"switch branch", []string{"switch", "feature"}, InvalidationFull},
		{"merge", []string{"merge", "feature"}, InvalidationFull},
		{"rebase", []string{"rebase", "main"}, InvalidationFull},
		{"pull", []string{"pull"}, InvalidationFull},
		{"reset hard", []string{"reset", "--hard"}, InvalidationFull},
		{"stash pop", []string{"stash", "pop"}, InvalidationFull},
		{"stash apply", []string{"stash", "apply"}, InvalidationFull},
		{"cherry-pick", []string{"cherry-pick", "abc123"}, InvalidationFull},
		{"revert", []string{"revert", "abc123"}, InvalidationFull},

		// Targeted invalidation commands
		{"add file", []string{"add", "file.go"}, InvalidationTargeted},
		{"restore file", []string{"restore", "file.go"}, InvalidationTargeted},
		{"rm file", []string{"rm", "file.go"}, InvalidationTargeted},
		{"checkout file", []string{"checkout", "--", "file.go"}, InvalidationTargeted},
		{"reset soft", []string{"reset", "HEAD~1"}, InvalidationTargeted},

		// No invalidation commands
		{"status", []string{"status"}, InvalidationNone},
		{"diff", []string{"diff"}, InvalidationNone},
		{"log", []string{"log"}, InvalidationNone},
		{"show", []string{"show", "HEAD"}, InvalidationNone},
		{"branch list", []string{"branch"}, InvalidationNone},
		{"remote", []string{"remote", "-v"}, InvalidationNone},
		{"fetch", []string{"fetch"}, InvalidationNone},
		{"stash list", []string{"stash", "list"}, InvalidationNone},
		{"stash show", []string{"stash", "show"}, InvalidationNone},

		// Edge cases
		{"empty args", []string{}, InvalidationNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyCommand(ctx, tt.args, workDir)
			if result != tt.expected {
				t.Errorf("classifyCommand(%v) = %v, want %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestInvalidationType_String(t *testing.T) {
	tests := []struct {
		invType  InvalidationType
		expected string
	}{
		{InvalidationNone, "none"},
		{InvalidationTargeted, "targeted"},
		{InvalidationFull, "full"},
		{InvalidationType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.invType.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Executor Tests
// =============================================================================

func TestGitAwareExecutor_Execute(t *testing.T) {
	// Skip if git not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a temporary git repo
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	mock := &mockCacheInvalidator{}
	config := DefaultExecutorConfig()
	config.WorkDir = tmpDir
	config.Cache = mock

	executor, err := NewGitAwareExecutor(config)
	if err != nil {
		t.Fatalf("NewGitAwareExecutor failed: %v", err)
	}

	t.Run("status triggers no invalidation", func(t *testing.T) {
		mock.invalidateAllCalled = false
		mock.invalidateFilesCalled = false

		result, err := executor.Execute(context.Background(), "status")
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result.InvalidationType != InvalidationNone {
			t.Errorf("Expected InvalidationNone, got %v", result.InvalidationType)
		}
		if mock.invalidateAllCalled || mock.invalidateFilesCalled {
			t.Error("Cache should not be invalidated for git status")
		}
	})

	t.Run("add triggers targeted invalidation", func(t *testing.T) {
		mock.invalidateAllCalled = false
		mock.invalidateFilesCalled = false

		// Create a file to add
		testFile := filepath.Join(tmpDir, "test_add.go")
		if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		result, err := executor.Execute(context.Background(), "add", "test_add.go")
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result.InvalidationType != InvalidationTargeted {
			t.Errorf("Expected InvalidationTargeted, got %v", result.InvalidationType)
		}
		if !mock.waitCalled {
			t.Error("WaitForRebuilds should be called before invalidation")
		}
	})

	t.Run("failed command does not invalidate", func(t *testing.T) {
		mock.invalidateAllCalled = false
		mock.invalidateFilesCalled = false

		// Try to checkout non-existent branch
		result, err := executor.Execute(context.Background(), "checkout", "nonexistent-branch-xyz")
		if err != nil {
			t.Fatalf("Execute should not return error for failed git command: %v", err)
		}

		if result.ExitCode == 0 {
			t.Error("Expected non-zero exit code for failed command")
		}
		if mock.invalidateAllCalled {
			t.Error("Cache should not be invalidated when command fails")
		}
	})
}

func TestGitAwareExecutor_FindGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	config := DefaultExecutorConfig()
	config.WorkDir = tmpDir

	executor, err := NewGitAwareExecutor(config)
	if err != nil {
		t.Fatalf("NewGitAwareExecutor failed: %v", err)
	}

	gitDir, err := executor.FindGitDir(context.Background())
	if err != nil {
		t.Fatalf("FindGitDir failed: %v", err)
	}

	expectedGitDir := filepath.Join(tmpDir, ".git")
	if gitDir != expectedGitDir {
		t.Errorf("FindGitDir = %v, want %v", gitDir, expectedGitDir)
	}
}

func TestGitAwareExecutor_GetCurrentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	config := DefaultExecutorConfig()
	config.WorkDir = tmpDir

	executor, err := NewGitAwareExecutor(config)
	if err != nil {
		t.Fatalf("NewGitAwareExecutor failed: %v", err)
	}

	branch, err := executor.GetCurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}

	// Should be on main or master depending on git version
	if branch != "main" && branch != "master" {
		t.Errorf("Expected main or master, got %v", branch)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func TestGetFileArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{"files only", []string{"file1.go", "file2.go"}, []string{"file1.go", "file2.go"}},
		{"with flags", []string{"-f", "file.go", "--force"}, []string{"file.go"}},
		{"with double dash", []string{"--", "file.go"}, []string{"file.go"}},
		{"empty", []string{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFileArgs(tt.args)
			if len(result) != len(tt.expected) {
				t.Errorf("getFileArgs(%v) = %v, want %v", tt.args, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getFileArgs(%v)[%d] = %v, want %v", tt.args, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestHasFlag(t *testing.T) {
	args := []string{"checkout", "--hard", "-f", "main"}

	if !hasFlag(args, "--hard") {
		t.Error("Expected to find --hard")
	}
	if !hasFlag(args, "-f") {
		t.Error("Expected to find -f")
	}
	if hasFlag(args, "--soft") {
		t.Error("Did not expect to find --soft")
	}
}

func TestHasDoubleDash(t *testing.T) {
	if !hasDoubleDash([]string{"checkout", "--", "file.go"}) {
		t.Error("Expected to find --")
	}
	if hasDoubleDash([]string{"checkout", "main"}) {
		t.Error("Did not expect to find --")
	}
}

// =============================================================================
// Test Setup Helpers
// =============================================================================

func setupGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.Run()

	// Create initial commit
	readmeFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("Failed to create README: %v", err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}
