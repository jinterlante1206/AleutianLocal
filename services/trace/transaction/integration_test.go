// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

//go:build integration

package transaction

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_BranchStrategy_CommitRollback tests the branch strategy with real git.
func TestIntegration_BranchStrategy_CommitRollback(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	t.Run("commit creates real commit", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyBranch

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		tx, err := manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Create a new file
		testFile := filepath.Join(repo, "test.txt")
		if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		manager.RecordModification("test.txt")

		// Commit
		result, err := manager.Commit(ctx, "Test commit")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		// Verify commit exists
		if result.CommitSHA == "" {
			t.Error("expected commit SHA, got empty")
		}
		if result.CommitSHA == tx.CheckpointRef {
			t.Error("commit SHA should be different from checkpoint")
		}

		// Verify file still exists
		if _, err := os.Stat(testFile); err != nil {
			t.Errorf("test file should still exist after commit: %v", err)
		}
	})

	t.Run("rollback restores state", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyBranch

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		_, err = manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Create a new file
		testFile := filepath.Join(repo, "test.txt")
		if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		manager.RecordModification("test.txt")

		// Rollback
		_, err = manager.Rollback(ctx, "test rollback")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		// Verify file is gone
		if _, err := os.Stat(testFile); !os.IsNotExist(err) {
			t.Error("test file should not exist after rollback")
		}
	})
}

// TestIntegration_StashStrategy_CommitRollback tests the stash strategy with real git.
func TestIntegration_StashStrategy_CommitRollback(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	t.Run("commit creates real commit", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyStash

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		tx, err := manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Create a new file
		testFile := filepath.Join(repo, "test.txt")
		if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		manager.RecordModification("test.txt")

		// Commit
		result, err := manager.Commit(ctx, "Test commit")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		// Verify commit exists
		if result.CommitSHA == "" {
			t.Error("expected commit SHA, got empty")
		}
		if result.CommitSHA == tx.CheckpointRef {
			t.Error("commit SHA should be different from checkpoint")
		}
	})

	t.Run("rollback restores state", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyStash

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		_, err = manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Create a new file
		testFile := filepath.Join(repo, "test.txt")
		if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		manager.RecordModification("test.txt")

		// Rollback
		_, err = manager.Rollback(ctx, "test rollback")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		// Verify file is gone
		if _, err := os.Stat(testFile); !os.IsNotExist(err) {
			t.Error("test file should not exist after rollback")
		}
	})
}

// TestIntegration_WorktreeStrategy_CommitRollback tests the worktree strategy with real git.
func TestIntegration_WorktreeStrategy_CommitRollback(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	if !gitWorktreeAvailable() {
		t.Skip("git worktree not available (requires git 2.5+)")
	}
	t.Parallel()

	t.Run("begin creates worktree", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyWorktree

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		tx, err := manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Verify worktree was created
		if tx.WorktreePath == "" {
			t.Error("expected worktree path, got empty")
		}
		if _, err := os.Stat(tx.WorktreePath); err != nil {
			t.Errorf("worktree directory should exist: %v", err)
		}

		// Cleanup
		manager.Rollback(ctx, "cleanup")
	})

	t.Run("commit creates real commit", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyWorktree

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		tx, err := manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Create a new file in the worktree
		testFile := filepath.Join(tx.WorktreePath, "test.txt")
		if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		manager.RecordModification("test.txt")

		// Commit
		result, err := manager.Commit(ctx, "Test commit")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		// Verify commit exists
		if result.CommitSHA == "" {
			t.Error("expected commit SHA, got empty")
		}
		if result.CommitSHA == tx.CheckpointRef {
			t.Error("commit SHA should be different from checkpoint")
		}
	})

	t.Run("rollback removes worktree", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyWorktree

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		tx, err := manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		worktreePath := tx.WorktreePath

		// Rollback
		_, err = manager.Rollback(ctx, "test rollback")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		// Verify worktree is gone
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Error("worktree directory should not exist after rollback")
		}
	})
}

// TestIntegration_CrashRecovery tests that transactions survive process restart
// and that the recovery mechanism detects and cleans up stale transactions.
func TestIntegration_CrashRecovery(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	repo := setupTestRepo(t)
	config := DefaultConfig()
	config.RepoPath = repo
	config.StateDir = filepath.Join(repo, ".aleutian", "state")
	config.Strategy = StrategyBranch

	ctx := context.Background()

	// First manager - simulates original process
	manager1, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Begin transaction
	tx, err := manager1.Begin(ctx, "test-session")
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	txID := tx.ID

	// Create a file
	testFile := filepath.Join(repo, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	manager1.RecordModification("test.txt")

	// Verify state file exists while transaction is active
	stateFile := filepath.Join(config.StateDir, txID+".json")
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file should exist during active transaction: %v", err)
	}

	// Verify the modified file exists
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("test file should exist: %v", err)
	}

	// Simulate crash by NOT calling Close() - in a real crash, cleanup wouldn't happen.
	// The manager goes out of scope without proper cleanup, leaving state file intact.
	manager1 = nil

	// Verify state file persists after "crash" (manager abandoned without cleanup)
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file should persist after crash (no cleanup): %v", err)
	}

	// Second manager - simulates new process after restart
	// The manager's startup logic detects and cleans up stale transactions.
	manager2, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create second manager: %v", err)
	}
	defer manager2.Close()

	// After recovery, the state file should be cleaned up (stale transaction rolled back)
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Error("state file should be cleaned up after recovery (stale transaction rolled back)")
	}

	// After rollback, the test file should be gone (changes reverted)
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("test file should be reverted after stale transaction rollback")
	}

	// Verify the new manager can start a fresh transaction
	_, err = manager2.Begin(ctx, "new-session")
	if err != nil {
		t.Fatalf("should be able to begin new transaction after recovery: %v", err)
	}
}

// TestIntegration_StrategySwitch tests switching between strategies.
func TestIntegration_StrategySwitch(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	strategies := []Strategy{StrategyBranch, StrategyStash}
	if gitWorktreeAvailable() {
		strategies = append(strategies, StrategyWorktree)
	}

	for _, strategy := range strategies {
		strategy := strategy // capture range variable
		t.Run(string(strategy), func(t *testing.T) {
			t.Parallel()

			repo := setupTestRepo(t)
			config := DefaultConfig()
			config.RepoPath = repo
			config.StateDir = filepath.Join(repo, ".aleutian", "state")
			config.Strategy = strategy

			manager, err := NewManager(config)
			if err != nil {
				t.Fatalf("failed to create manager with strategy %s: %v", strategy, err)
			}
			defer manager.Close()

			ctx := context.Background()

			// Begin/Commit cycle
			_, err = manager.Begin(ctx, "test-session")
			if err != nil {
				t.Fatalf("Begin failed with strategy %s: %v", strategy, err)
			}

			_, err = manager.Commit(ctx, "Test commit")
			if err != nil {
				t.Fatalf("Commit failed with strategy %s: %v", strategy, err)
			}
		})
	}
}

// TestIntegration_DirtyRepoPreservation tests that pre-existing uncommitted changes
// are preserved during transaction operations. This is CRITICAL for user data safety.
// If this test fails, the agent can eat user work-in-progress.
func TestIntegration_DirtyRepoPreservation(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	t.Run("stash strategy preserves user WIP on rollback", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyStash

		// CRITICAL: Create user's work-in-progress BEFORE transaction starts
		userWIPFile := filepath.Join(repo, "user_wip.txt")
		userWIPContent := []byte("User's important uncommitted work")
		if err := os.WriteFile(userWIPFile, userWIPContent, 0644); err != nil {
			t.Fatalf("failed to create user WIP file: %v", err)
		}

		// Verify the WIP file exists before transaction
		if _, err := os.Stat(userWIPFile); err != nil {
			t.Fatalf("user WIP file should exist before transaction: %v", err)
		}

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction - this should stash user's changes
		_, err = manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// After Begin with stash strategy, user's WIP might be stashed
		// (depending on implementation - some strategies stash, some don't)

		// Agent makes its own changes
		agentFile := filepath.Join(repo, "agent_change.txt")
		if err := os.WriteFile(agentFile, []byte("Agent's work"), 0644); err != nil {
			t.Fatalf("failed to create agent file: %v", err)
		}
		manager.RecordModification("agent_change.txt")

		// Rollback - this should restore user's WIP
		_, err = manager.Rollback(ctx, "abort mission")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		// CRITICAL CHECK: User's WIP must be back
		content, err := os.ReadFile(userWIPFile)
		if err != nil {
			t.Fatalf("user WIP file should exist after rollback: %v", err)
		}
		if string(content) != string(userWIPContent) {
			t.Errorf("user WIP content corrupted: got %q, want %q", content, userWIPContent)
		}

		// Agent's changes should be gone
		if _, err := os.Stat(agentFile); !os.IsNotExist(err) {
			t.Error("agent file should not exist after rollback")
		}
	})

	t.Run("branch strategy preserves user WIP on rollback", func(t *testing.T) {
		repo := setupTestRepo(t)
		config := DefaultConfig()
		config.RepoPath = repo
		config.StateDir = filepath.Join(repo, ".aleutian", "state")
		config.Strategy = StrategyBranch

		// Create user's work-in-progress BEFORE transaction starts
		userWIPFile := filepath.Join(repo, "user_wip.txt")
		userWIPContent := []byte("User's important uncommitted work")
		if err := os.WriteFile(userWIPFile, userWIPContent, 0644); err != nil {
			t.Fatalf("failed to create user WIP file: %v", err)
		}

		manager, err := NewManager(config)
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		defer manager.Close()

		ctx := context.Background()

		// Begin transaction
		_, err = manager.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Agent makes its own changes
		agentFile := filepath.Join(repo, "agent_change.txt")
		if err := os.WriteFile(agentFile, []byte("Agent's work"), 0644); err != nil {
			t.Fatalf("failed to create agent file: %v", err)
		}
		manager.RecordModification("agent_change.txt")

		// Rollback
		_, err = manager.Rollback(ctx, "abort mission")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		// CRITICAL CHECK: User's WIP must be back
		content, err := os.ReadFile(userWIPFile)
		if err != nil {
			t.Fatalf("user WIP file should exist after rollback: %v", err)
		}
		if string(content) != string(userWIPContent) {
			t.Errorf("user WIP content corrupted: got %q, want %q", content, userWIPContent)
		}

		// Agent's changes should be gone
		if _, err := os.Stat(agentFile); !os.IsNotExist(err) {
			t.Error("agent file should not exist after rollback")
		}
	})
}

// TestIntegration_ConcurrentOperations tests that concurrent operations are handled safely.
func TestIntegration_ConcurrentOperations(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	t.Parallel()

	repo := setupTestRepo(t)
	config := DefaultConfig()
	config.RepoPath = repo
	config.StateDir = filepath.Join(repo, ".aleutian", "state")
	config.Strategy = StrategyBranch

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Begin first transaction
	_, err = manager.Begin(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	// Try to begin second transaction - should fail
	_, err = manager.Begin(ctx, "test-session-2")
	if err == nil {
		t.Error("expected error when beginning second transaction")
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

// gitAvailable checks if git is installed.
func gitAvailable() bool {
	cmd := exec.Command("git", "--version")
	return cmd.Run() == nil
}

// gitWorktreeAvailable checks if git worktree is available (requires git 2.5+).
func gitWorktreeAvailable() bool {
	// Run git worktree --help to check if the subcommand exists (works outside repos)
	cmd := exec.Command("git", "worktree", "--help")
	return cmd.Run() == nil
}

// setupTestRepo creates a temporary git repository for testing.
// This setup is designed to work on any machine, including CI/CD environments
// without global git config.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Initialize git repo
	runGit(t, dir, "init")

	// CRITICAL: Set git identity - required for commits to work.
	// CI/CD environments and fresh Docker containers won't have global config.
	runGit(t, dir, "config", "user.email", "agent@aleutian.ai")
	runGit(t, dir, "config", "user.name", "Aleutian Agent")

	// CRITICAL: Force branch name to "main" for consistency.
	// Different git versions default to "master" or "main".
	runGit(t, dir, "checkout", "-b", "main")

	// Create initial commit (required for most git operations)
	initialFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initialFile, []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "Initial commit")

	return dir
}

// runGit runs a git command in the specified directory.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}

	return string(output)
}
