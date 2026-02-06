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
	"context"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// Mock Git Client
// =============================================================================

type mockGitClient struct {
	isGitRepo          bool
	hasRebase          bool
	hasMerge           bool
	hasCherryPick      bool
	hasBisect          bool
	isDetached         bool
	currentBranch      string
	headSHA            string
	branchesCreated    []string
	branchesDeleted    []string
	stashesPushed      []string
	stashesPopped      int
	stashesDropped     []string
	resetHardCalls     []string
	cleanCalls         int
	addAllCalls        int
	commitMessages     []string
	hasStagedChanges   bool
	hasUnstagedChanges bool
	status             *GitStatus
	stashList          []StashEntry

	// Error injection
	revParseErr     error
	stashPushErr    error
	stashPopErr     error
	createBranchErr error
	deleteBranchErr error
	resetHardErr    error
	cleanErr        error
	addAllErr       error
	commitErr       error
	statusErr       error
}

func newMockGitClient() *mockGitClient {
	return &mockGitClient{
		isGitRepo:     true,
		currentBranch: "main",
		headSHA:       "abc123def456",
		status:        &GitStatus{Branch: "main", IsClean: true},
	}
}

func (m *mockGitClient) IsGitRepository(ctx context.Context) bool {
	return m.isGitRepo
}

func (m *mockGitClient) HasRebaseInProgress(ctx context.Context) bool {
	return m.hasRebase
}

func (m *mockGitClient) HasMergeInProgress(ctx context.Context) bool {
	return m.hasMerge
}

func (m *mockGitClient) IsDetachedHead(ctx context.Context) bool {
	return m.isDetached
}

func (m *mockGitClient) HasCherryPickInProgress(ctx context.Context) bool {
	return m.hasCherryPick
}

func (m *mockGitClient) HasBisectInProgress(ctx context.Context) bool {
	return m.hasBisect
}

func (m *mockGitClient) GetCurrentBranch(ctx context.Context) (string, error) {
	if m.isDetached {
		return "HEAD", nil
	}
	return m.currentBranch, nil
}

func (m *mockGitClient) RevParse(ctx context.Context, ref string) (string, error) {
	if m.revParseErr != nil {
		return "", m.revParseErr
	}
	return m.headSHA, nil
}

func (m *mockGitClient) RefExists(ctx context.Context, ref string) bool {
	return true
}

func (m *mockGitClient) StashPush(ctx context.Context, message string) error {
	if m.stashPushErr != nil {
		return m.stashPushErr
	}
	m.stashesPushed = append(m.stashesPushed, message)
	m.stashList = append([]StashEntry{{
		Index:   0,
		Ref:     "stash@{0}",
		Message: message,
	}}, m.stashList...)
	return nil
}

func (m *mockGitClient) StashPop(ctx context.Context) error {
	if m.stashPopErr != nil {
		return m.stashPopErr
	}
	m.stashesPopped++
	if len(m.stashList) > 0 {
		m.stashList = m.stashList[1:]
	}
	return nil
}

func (m *mockGitClient) StashDrop(ctx context.Context, ref string) error {
	m.stashesDropped = append(m.stashesDropped, ref)
	return nil
}

func (m *mockGitClient) StashList(ctx context.Context) ([]StashEntry, error) {
	return m.stashList, nil
}

func (m *mockGitClient) CreateBranch(ctx context.Context, name string) error {
	if m.createBranchErr != nil {
		return m.createBranchErr
	}
	m.branchesCreated = append(m.branchesCreated, name)
	return nil
}

func (m *mockGitClient) DeleteBranch(ctx context.Context, name string, force bool) error {
	if m.deleteBranchErr != nil {
		return m.deleteBranchErr
	}
	m.branchesDeleted = append(m.branchesDeleted, name)
	return nil
}

func (m *mockGitClient) Checkout(ctx context.Context, ref string) error {
	return nil
}

func (m *mockGitClient) BranchExists(ctx context.Context, name string) bool {
	for _, b := range m.branchesCreated {
		if b == name {
			return true
		}
	}
	return false
}

func (m *mockGitClient) ResetHard(ctx context.Context, ref string) error {
	if m.resetHardErr != nil {
		return m.resetHardErr
	}
	m.resetHardCalls = append(m.resetHardCalls, ref)
	return nil
}

func (m *mockGitClient) CleanUntracked(ctx context.Context) error {
	if m.cleanErr != nil {
		return m.cleanErr
	}
	m.cleanCalls++
	return nil
}

func (m *mockGitClient) Add(ctx context.Context, paths ...string) error {
	return nil
}

func (m *mockGitClient) AddAll(ctx context.Context) error {
	if m.addAllErr != nil {
		return m.addAllErr
	}
	m.addAllCalls++
	return nil
}

func (m *mockGitClient) Commit(ctx context.Context, message string) error {
	if m.commitErr != nil {
		return m.commitErr
	}
	m.commitMessages = append(m.commitMessages, message)
	return nil
}

func (m *mockGitClient) HasStagedChanges(ctx context.Context) bool {
	return m.hasStagedChanges
}

func (m *mockGitClient) HasUnstagedChanges(ctx context.Context) bool {
	return m.hasUnstagedChanges
}

func (m *mockGitClient) Status(ctx context.Context) (*GitStatus, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return m.status, nil
}

func (m *mockGitClient) CreateWorktree(ctx context.Context, path string, ref string) error {
	return nil
}

func (m *mockGitClient) RemoveWorktree(ctx context.Context, path string, force bool) error {
	return nil
}

func (m *mockGitClient) WorktreeList(ctx context.Context) ([]WorktreeEntry, error) {
	return nil, nil
}

// =============================================================================
// Manager Tests
// =============================================================================

func TestNewManager(t *testing.T) {
	t.Run("requires RepoPath", func(t *testing.T) {
		config := DefaultConfig()
		_, err := NewManagerWithGit(config, newMockGitClient())
		if err == nil {
			t.Error("expected error for empty RepoPath")
		}
	})

	t.Run("applies defaults", func(t *testing.T) {
		config := Config{RepoPath: "/tmp/test"}
		git := newMockGitClient()
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if m.config.Strategy != StrategyBranch {
			t.Errorf("expected default strategy Branch, got %s", m.config.Strategy)
		}
		if m.config.TransactionTTL != 30*time.Minute {
			t.Errorf("expected default TTL 30m, got %v", m.config.TransactionTTL)
		}
		if m.config.MaxTrackedFiles != 10000 {
			t.Errorf("expected default MaxTrackedFiles 10000, got %d", m.config.MaxTrackedFiles)
		}
	})
}

func TestManager_Begin(t *testing.T) {
	t.Run("creates transaction with branch strategy", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyBranch,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		if tx == nil {
			t.Fatal("expected transaction, got nil")
		}
		if tx.SessionID != "test-session" {
			t.Errorf("expected session ID 'test-session', got '%s'", tx.SessionID)
		}
		if tx.Status != StatusActive {
			t.Errorf("expected status Active, got %s", tx.Status)
		}
		if tx.CheckpointRef != git.headSHA {
			t.Errorf("expected checkpoint ref '%s', got '%s'", git.headSHA, tx.CheckpointRef)
		}
		if tx.OriginalBranch != "main" {
			t.Errorf("expected original branch 'main', got '%s'", tx.OriginalBranch)
		}
		if len(git.branchesCreated) != 1 {
			t.Errorf("expected 1 branch created, got %d", len(git.branchesCreated))
		}
	})

	t.Run("creates transaction with stash strategy", func(t *testing.T) {
		git := newMockGitClient()
		// Use clean status for this test since we're testing transaction creation
		// The preflight check blocks dirty working trees by default
		git.status = &GitStatus{Branch: "main", IsClean: true}
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyStash,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// With clean status, no stash is needed
		if tx.StashRef != "" {
			t.Errorf("expected empty stash ref for clean repo, got '%s'", tx.StashRef)
		}
		// The stash checkpoint only pushes if there are changes
		if len(git.stashesPushed) != 0 {
			t.Errorf("expected 0 stash pushed for clean repo, got %d", len(git.stashesPushed))
		}
	})

	t.Run("fails if transaction already active", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "session-1")
		if err != nil {
			t.Fatalf("first Begin failed: %v", err)
		}

		_, err = m.Begin(ctx, "session-2")
		if !errors.Is(err, ErrTransactionActive) {
			t.Errorf("expected ErrTransactionActive, got %v", err)
		}
	})

	t.Run("allows non-git repository with warning", func(t *testing.T) {
		// The new preflight behavior allows non-git repos with a warning
		// (file rollback won't be available, but operations can proceed)
		git := newMockGitClient()
		git.isGitRepo = false
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Errorf("expected Begin to succeed with warning, got error: %v", err)
		}
		if tx == nil {
			t.Error("expected transaction to be created")
		}
	})

	t.Run("fails if rebase in progress", func(t *testing.T) {
		git := newMockGitClient()
		git.hasRebase = true
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for rebase in progress")
		}
		// Check it's a PreFlightError with the correct code
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "REBASE_IN_PROGRESS" {
			t.Errorf("expected code REBASE_IN_PROGRESS, got %s", pfErr.Code)
		}
	})

	t.Run("fails if merge in progress", func(t *testing.T) {
		git := newMockGitClient()
		git.hasMerge = true
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for merge in progress")
		}
		// Check it's a PreFlightError with the correct code
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "MERGE_IN_PROGRESS" {
			t.Errorf("expected code MERGE_IN_PROGRESS, got %s", pfErr.Code)
		}
	})

	t.Run("fails if cherry-pick in progress", func(t *testing.T) {
		git := newMockGitClient()
		git.hasCherryPick = true
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for cherry-pick in progress")
		}
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "CHERRY_PICK_IN_PROGRESS" {
			t.Errorf("expected code CHERRY_PICK_IN_PROGRESS, got %s", pfErr.Code)
		}
	})

	t.Run("fails if bisect in progress", func(t *testing.T) {
		git := newMockGitClient()
		git.hasBisect = true
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for bisect in progress")
		}
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "BISECT_IN_PROGRESS" {
			t.Errorf("expected code BISECT_IN_PROGRESS, got %s", pfErr.Code)
		}
	})

	t.Run("fails if detached HEAD", func(t *testing.T) {
		git := newMockGitClient()
		git.isDetached = true
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for detached HEAD")
		}
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "DETACHED_HEAD" {
			t.Errorf("expected code DETACHED_HEAD, got %s", pfErr.Code)
		}
	})

	t.Run("allows detached HEAD when configured", func(t *testing.T) {
		git := newMockGitClient()
		git.isDetached = true
		config := Config{
			RepoPath:  "/tmp/test",
			PreFlight: PreFlightConfig{AllowDetached: true},
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Errorf("expected Begin to succeed with AllowDetached, got error: %v", err)
		}
		if tx == nil {
			t.Error("expected transaction to be created")
		}
	})

	t.Run("fails if dirty working tree", func(t *testing.T) {
		git := newMockGitClient()
		git.status = &GitStatus{
			Branch:        "main",
			IsClean:       false,
			ModifiedFiles: []string{"file.go"},
		}
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err == nil {
			t.Error("expected error for dirty working tree")
		}
		var pfErr *PreFlightError
		if !errors.As(err, &pfErr) {
			t.Fatalf("expected PreFlightError, got %T: %v", err, err)
		}
		if pfErr.Code != "DIRTY_WORKING_TREE" {
			t.Errorf("expected code DIRTY_WORKING_TREE, got %s", pfErr.Code)
		}
	})

	t.Run("allows dirty working tree with Force", func(t *testing.T) {
		git := newMockGitClient()
		git.status = &GitStatus{
			Branch:        "main",
			IsClean:       false,
			ModifiedFiles: []string{"file.go"},
		}
		config := Config{
			RepoPath:  "/tmp/test",
			PreFlight: PreFlightConfig{Force: true},
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Errorf("expected Begin to succeed with Force, got error: %v", err)
		}
		if tx == nil {
			t.Error("expected transaction to be created")
		}
	})
}

func TestManager_Commit(t *testing.T) {
	t.Run("commits with branch strategy", func(t *testing.T) {
		git := newMockGitClient()
		git.hasStagedChanges = true
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyBranch,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		result, err := m.Commit(ctx, "test commit message")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		if result.Status != StatusCommitted {
			t.Errorf("expected status Committed, got %s", result.Status)
		}
		if len(git.commitMessages) != 1 {
			t.Errorf("expected 1 commit, got %d", len(git.commitMessages))
		}
		if git.commitMessages[0] != "test commit message" {
			t.Errorf("expected commit message 'test commit message', got '%s'", git.commitMessages[0])
		}
		if len(git.branchesDeleted) != 1 {
			t.Errorf("expected work branch deleted")
		}
	})

	t.Run("commits with stash strategy", func(t *testing.T) {
		git := newMockGitClient()
		git.hasStagedChanges = true
		git.status = &GitStatus{Branch: "main", IsClean: false}
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyStash,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		result, err := m.Commit(ctx, "test commit")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		if result.Status != StatusCommitted {
			t.Errorf("expected status Committed, got %s", result.Status)
		}
	})

	t.Run("fails if no transaction active", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Commit(ctx, "test commit")
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("expected ErrNoTransaction, got %v", err)
		}
	})

	t.Run("handles no changes gracefully", func(t *testing.T) {
		git := newMockGitClient()
		git.hasStagedChanges = false
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		result, err := m.Commit(ctx, "test commit")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		if result.Status != StatusCommitted {
			t.Errorf("expected status Committed, got %s", result.Status)
		}
		// Should not create a commit if nothing changed
		if len(git.commitMessages) != 0 {
			t.Errorf("expected 0 commits for no changes, got %d", len(git.commitMessages))
		}
	})

	t.Run("rolls back expired transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{
			RepoPath:       "/tmp/test",
			TransactionTTL: 1 * time.Millisecond, // Very short TTL
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		_, err = m.Commit(ctx, "test commit")
		if !errors.Is(err, ErrTransactionExpired) {
			t.Errorf("expected ErrTransactionExpired, got %v", err)
		}

		// Transaction should have been rolled back
		if m.IsActive() {
			t.Error("expected no active transaction after expiration")
		}
	})
}

func TestManager_Rollback(t *testing.T) {
	t.Run("rolls back with branch strategy", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyBranch,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		tx, err := m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		result, err := m.Rollback(ctx, "test failure")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		if result.Status != StatusRolledBack {
			t.Errorf("expected status RolledBack, got %s", result.Status)
		}
		if result.RollbackReason != "test failure" {
			t.Errorf("expected reason 'test failure', got '%s'", result.RollbackReason)
		}
		if len(git.resetHardCalls) != 1 {
			t.Errorf("expected 1 reset hard call, got %d", len(git.resetHardCalls))
		}
		if git.resetHardCalls[0] != tx.CheckpointRef {
			t.Errorf("expected reset to checkpoint '%s', got '%s'", tx.CheckpointRef, git.resetHardCalls[0])
		}
		if git.cleanCalls != 1 {
			t.Errorf("expected 1 clean call, got %d", git.cleanCalls)
		}
	})

	t.Run("rolls back with stash strategy", func(t *testing.T) {
		git := newMockGitClient()
		git.status = &GitStatus{Branch: "main", IsClean: false}
		config := Config{
			RepoPath: "/tmp/test",
			Strategy: StrategyStash,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		result, err := m.Rollback(ctx, "test failure")
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		if result.Status != StatusRolledBack {
			t.Errorf("expected status RolledBack, got %s", result.Status)
		}
		if git.stashesPopped != 1 {
			t.Errorf("expected 1 stash pop, got %d", git.stashesPopped)
		}
	})

	t.Run("fails if no transaction active", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Rollback(ctx, "test failure")
		if !errors.Is(err, ErrNoTransaction) {
			t.Errorf("expected ErrNoTransaction, got %v", err)
		}
	})

	t.Run("returns error if reset fails", func(t *testing.T) {
		git := newMockGitClient()
		git.resetHardErr = errors.New("reset failed")
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		_, err = m.Rollback(ctx, "test failure")
		if !errors.Is(err, ErrRollbackFailed) {
			t.Errorf("expected ErrRollbackFailed, got %v", err)
		}
	})
}

func TestManager_RecordModification(t *testing.T) {
	t.Run("tracks modifications", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		err = m.RecordModification("file1.go")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		err = m.RecordModification("file2.go")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Duplicate should be deduplicated
		err = m.RecordModification("file1.go")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		tx := m.Active()
		if tx.FileCount() != 2 {
			t.Errorf("expected 2 files tracked, got %d", tx.FileCount())
		}
	})

	t.Run("respects max files limit", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{
			RepoPath:        "/tmp/test",
			MaxTrackedFiles: 2,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		_ = m.RecordModification("file1.go")
		_ = m.RecordModification("file2.go")

		err = m.RecordModification("file3.go")
		if !errors.Is(err, ErrMaxFilesExceeded) {
			t.Errorf("expected ErrMaxFilesExceeded, got %v", err)
		}
	})

	t.Run("ignores when no transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should not error when no transaction
		err = m.RecordModification("file.go")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestManager_RecordModifications(t *testing.T) {
	t.Run("batch tracks modifications", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		err = m.RecordModifications([]string{"file1.go", "file2.go", "file3.go"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		tx := m.Active()
		if tx.FileCount() != 3 {
			t.Errorf("expected 3 files tracked, got %d", tx.FileCount())
		}
	})

	t.Run("respects max files limit", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{
			RepoPath:        "/tmp/test",
			MaxTrackedFiles: 2,
		}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		err = m.RecordModifications([]string{"file1.go", "file2.go", "file3.go"})
		if !errors.Is(err, ErrMaxFilesExceeded) {
			t.Errorf("expected ErrMaxFilesExceeded, got %v", err)
		}
	})
}

func TestManager_Active(t *testing.T) {
	t.Run("returns nil when no transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tx := m.Active()
		if tx != nil {
			t.Error("expected nil, got transaction")
		}
	})

	t.Run("returns copy of active transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		tx := m.Active()
		if tx == nil {
			t.Fatal("expected transaction, got nil")
		}
		if tx.SessionID != "test-session" {
			t.Errorf("expected session ID 'test-session', got '%s'", tx.SessionID)
		}

		// Verify it's a copy by modifying it
		tx.SessionID = "modified"
		tx2 := m.Active()
		if tx2.SessionID != "test-session" {
			t.Error("Active() should return a copy, not the original")
		}
	})
}

func TestManager_Close(t *testing.T) {
	t.Run("rolls back active transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		_, err = m.Begin(ctx, "test-session")
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}

		err = m.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}

		if m.IsActive() {
			t.Error("expected no active transaction after Close")
		}
		if len(git.resetHardCalls) != 1 {
			t.Error("expected rollback on Close")
		}
	})

	t.Run("succeeds with no active transaction", func(t *testing.T) {
		git := newMockGitClient()
		config := Config{RepoPath: "/tmp/test"}
		m, err := NewManagerWithGit(config, git)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = m.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})
}

// =============================================================================
// Transaction Tests
// =============================================================================

func TestTransaction_IsExpired(t *testing.T) {
	tx := &Transaction{
		StartedAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
		ExpiresAt: time.Now().Add(-30 * time.Minute).UnixMilli(),
	}

	if !tx.IsExpired() {
		t.Error("expected transaction to be expired")
	}

	tx.ExpiresAt = time.Now().Add(30 * time.Minute).UnixMilli()
	if tx.IsExpired() {
		t.Error("expected transaction not to be expired")
	}
}

func TestTransaction_Duration(t *testing.T) {
	tx := &Transaction{
		StartedAt: time.Now().Add(-5 * time.Second).UnixMilli(),
	}

	duration := tx.Duration()
	if duration < 5*time.Second || duration > 6*time.Second {
		t.Errorf("unexpected duration: %v", duration)
	}
}

func TestTransaction_Files(t *testing.T) {
	tx := &Transaction{
		ModifiedFiles: map[string]struct{}{
			"file1.go": {},
			"file2.go": {},
			"file3.go": {},
		},
	}

	files := tx.Files()
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

// =============================================================================
// Status Tests
// =============================================================================

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   Status
		terminal bool
	}{
		{StatusIdle, false},
		{StatusActive, false},
		{StatusCommitting, false},
		{StatusRollingBack, false},
		{StatusCommitted, true},
		{StatusRolledBack, true},
		{StatusFailed, true},
	}

	for _, tc := range tests {
		if tc.status.IsTerminal() != tc.terminal {
			t.Errorf("Status %s: expected IsTerminal=%v, got %v",
				tc.status, tc.terminal, tc.status.IsTerminal())
		}
	}
}
