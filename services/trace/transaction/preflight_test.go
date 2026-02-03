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
	"testing"
)

// mockGitClientForPreflight is a mock that implements the GitClient interface for preflight tests.
type mockGitClientForPreflight struct {
	isGitRepo       bool
	hasRebase       bool
	hasMerge        bool
	hasCherryPick   bool
	hasBisect       bool
	isDetached      bool
	status          *GitStatus
	statusErr       error
	stashPushCalled bool
	stashPushErr    error
	stashPopCalled  bool
	stashPopErr     error
}

func (m *mockGitClientForPreflight) IsGitRepository(ctx context.Context) bool {
	return m.isGitRepo
}

func (m *mockGitClientForPreflight) HasRebaseInProgress(ctx context.Context) bool {
	return m.hasRebase
}

func (m *mockGitClientForPreflight) HasMergeInProgress(ctx context.Context) bool {
	return m.hasMerge
}

func (m *mockGitClientForPreflight) IsDetachedHead(ctx context.Context) bool {
	return m.isDetached
}

func (m *mockGitClientForPreflight) HasCherryPickInProgress(ctx context.Context) bool {
	return m.hasCherryPick
}

func (m *mockGitClientForPreflight) HasBisectInProgress(ctx context.Context) bool {
	return m.hasBisect
}

func (m *mockGitClientForPreflight) GetCurrentBranch(ctx context.Context) (string, error) {
	if m.isDetached {
		return "HEAD", nil
	}
	return "main", nil
}

func (m *mockGitClientForPreflight) RevParse(ctx context.Context, ref string) (string, error) {
	return "abc123", nil
}

func (m *mockGitClientForPreflight) RefExists(ctx context.Context, ref string) bool {
	return true
}

func (m *mockGitClientForPreflight) StashPush(ctx context.Context, message string) error {
	m.stashPushCalled = true
	return m.stashPushErr
}

func (m *mockGitClientForPreflight) StashPop(ctx context.Context) error {
	m.stashPopCalled = true
	return m.stashPopErr
}

func (m *mockGitClientForPreflight) StashDrop(ctx context.Context, ref string) error {
	return nil
}

func (m *mockGitClientForPreflight) StashList(ctx context.Context) ([]StashEntry, error) {
	return nil, nil
}

func (m *mockGitClientForPreflight) CreateBranch(ctx context.Context, name string) error {
	return nil
}

func (m *mockGitClientForPreflight) DeleteBranch(ctx context.Context, name string, force bool) error {
	return nil
}

func (m *mockGitClientForPreflight) Checkout(ctx context.Context, ref string) error {
	return nil
}

func (m *mockGitClientForPreflight) BranchExists(ctx context.Context, name string) bool {
	return true
}

func (m *mockGitClientForPreflight) ResetHard(ctx context.Context, ref string) error {
	return nil
}

func (m *mockGitClientForPreflight) CleanUntracked(ctx context.Context) error {
	return nil
}

func (m *mockGitClientForPreflight) Add(ctx context.Context, paths ...string) error {
	return nil
}

func (m *mockGitClientForPreflight) AddAll(ctx context.Context) error {
	return nil
}

func (m *mockGitClientForPreflight) Commit(ctx context.Context, message string) error {
	return nil
}

func (m *mockGitClientForPreflight) HasStagedChanges(ctx context.Context) bool {
	return false
}

func (m *mockGitClientForPreflight) HasUnstagedChanges(ctx context.Context) bool {
	return false
}

func (m *mockGitClientForPreflight) Status(ctx context.Context) (*GitStatus, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	if m.status != nil {
		return m.status, nil
	}
	return &GitStatus{Branch: "main", IsClean: true}, nil
}

func (m *mockGitClientForPreflight) CreateWorktree(ctx context.Context, path string, ref string) error {
	return nil
}

func (m *mockGitClientForPreflight) RemoveWorktree(ctx context.Context, path string, force bool) error {
	return nil
}

func (m *mockGitClientForPreflight) WorktreeList(ctx context.Context) ([]WorktreeEntry, error) {
	return nil, nil
}

// =============================================================================
// Tests
// =============================================================================

func TestPreFlightGuard_Check_CleanRepo(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status:    &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass, got errors: %v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(result.Errors))
	}
}

func TestPreFlightGuard_Check_NotGitRepo(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: false,
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass (with warning), got errors: %v", result.Errors)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Code != "NOT_GIT_REPO" {
		t.Errorf("expected NOT_GIT_REPO warning, got %s", result.Warnings[0].Code)
	}
}

func TestPreFlightGuard_Check_MergeInProgress(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		hasMerge:  true,
		status:    &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "MERGE_IN_PROGRESS" {
		t.Errorf("expected MERGE_IN_PROGRESS error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_RebaseInProgress(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		hasRebase: true,
		status:    &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "REBASE_IN_PROGRESS" {
		t.Errorf("expected REBASE_IN_PROGRESS error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_CherryPickInProgress(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo:     true,
		hasCherryPick: true,
		status:        &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "CHERRY_PICK_IN_PROGRESS" {
		t.Errorf("expected CHERRY_PICK_IN_PROGRESS error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_BisectInProgress(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		hasBisect: true,
		status:    &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "BISECT_IN_PROGRESS" {
		t.Errorf("expected BISECT_IN_PROGRESS error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_DetachedHead(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo:  true,
		isDetached: true,
		status:     &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "DETACHED_HEAD" {
		t.Errorf("expected DETACHED_HEAD error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_DetachedHead_Allowed(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo:  true,
		isDetached: true,
		status:     &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{AllowDetached: true}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass, got errors: %v", result.Errors)
	}
}

func TestPreFlightGuard_Check_DirtyWorkingTree(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status: &GitStatus{
			Branch:        "main",
			IsClean:       false,
			StagedFiles:   []string{"file1.go"},
			ModifiedFiles: []string{"file2.go"},
		},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "DIRTY_WORKING_TREE" {
		t.Errorf("expected DIRTY_WORKING_TREE error, got %s", result.Errors[0].Code)
	}
}

func TestPreFlightGuard_Check_DirtyWorkingTree_Force(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status: &GitStatus{
			Branch:        "main",
			IsClean:       false,
			ModifiedFiles: []string{"file1.go"},
		},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{Force: true}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass with Force, got errors: %v", result.Errors)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Code != "DIRTY_FORCED" {
		t.Errorf("expected DIRTY_FORCED warning, got %s", result.Warnings[0].Code)
	}
}

func TestPreFlightGuard_Check_DirtyWorkingTree_AutoStash(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status: &GitStatus{
			Branch:        "main",
			IsClean:       false,
			ModifiedFiles: []string{"file1.go"},
		},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{AutoStash: true}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass with AutoStash, got errors: %v", result.Errors)
	}
	if !mock.stashPushCalled {
		t.Error("expected StashPush to be called")
	}
	if result.StashRef != "stash@{0}" {
		t.Errorf("expected StashRef to be set, got %s", result.StashRef)
	}
}

func TestPreFlightGuard_Check_UntrackedFiles_Warning(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status: &GitStatus{
			Branch:         "main",
			IsClean:        true,
			UntrackedFiles: []string{"newfile.txt"},
		},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected check to pass, got errors: %v", result.Errors)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Code != "UNTRACKED_FILES" {
		t.Errorf("expected UNTRACKED_FILES warning, got %s", result.Warnings[0].Code)
	}
}

func TestPreFlightGuard_Check_MultipleErrors(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo:     true,
		hasMerge:      true,
		hasCherryPick: true,
		status:        &GitStatus{Branch: "main", IsClean: true},
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected check to fail")
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(result.Errors))
	}
}

func TestPreFlightGuard_Cleanup(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	err := guard.Cleanup(context.Background(), "stash@{0}")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.stashPopCalled {
		t.Error("expected StashPop to be called")
	}
}

func TestPreFlightGuard_Cleanup_EmptyStashRef(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
	}

	guard := NewPreFlightGuard(mock, PreFlightConfig{}, nil)
	err := guard.Cleanup(context.Background(), "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.stashPopCalled {
		t.Error("StashPop should not be called for empty stash ref")
	}
}

func TestPreFlightResult_FirstError(t *testing.T) {
	result := &PreFlightResult{
		Passed: false,
		Errors: []PreFlightError{
			{Code: "ERROR_1", Message: "First error"},
			{Code: "ERROR_2", Message: "Second error"},
		},
	}

	err := result.FirstError()
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "[ERROR_1] First error" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestPreFlightResult_FirstError_NoErrors(t *testing.T) {
	result := &PreFlightResult{Passed: true}
	err := result.FirstError()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestPreFlightResult_FormatErrors(t *testing.T) {
	result := &PreFlightResult{
		Passed: false,
		Errors: []PreFlightError{
			{
				Code:    "DIRTY_WORKING_TREE",
				Message: "Repository has uncommitted changes.",
				Details: []string{"modified: file1.go", "staged: file2.go"},
			},
		},
	}

	formatted := result.FormatErrors()
	if formatted == "" {
		t.Error("expected non-empty formatted output")
	}
	if !contains(formatted, "Pre-flight check failed") {
		t.Error("expected header in output")
	}
	if !contains(formatted, "DIRTY_WORKING_TREE") {
		t.Error("expected error code in output")
	}
	if !contains(formatted, "file1.go") {
		t.Error("expected details in output")
	}
}

func TestValidateConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		err := ValidateConfig(PreFlightConfig{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("force only", func(t *testing.T) {
		err := ValidateConfig(PreFlightConfig{Force: true})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("autostash only", func(t *testing.T) {
		err := ValidateConfig(PreFlightConfig{AutoStash: true})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("force and autostash", func(t *testing.T) {
		err := ValidateConfig(PreFlightConfig{Force: true, AutoStash: true})
		if err == nil {
			t.Error("expected error for conflicting config")
		}
	})
}

func TestNewPreFlightGuard_PanicOnNilGit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil git client")
		}
	}()

	NewPreFlightGuard(nil, PreFlightConfig{}, nil)
}

func TestNewPreFlightGuard_InvalidConfigHandling(t *testing.T) {
	mock := &mockGitClientForPreflight{
		isGitRepo: true,
		status: &GitStatus{
			Branch:        "main",
			IsClean:       false,
			ModifiedFiles: []string{"file.go"},
		},
	}

	// Both Force and AutoStash set - Force should take precedence
	guard := NewPreFlightGuard(mock, PreFlightConfig{Force: true, AutoStash: true}, nil)
	result, err := guard.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With invalid config corrected to Force=true, AutoStash=false,
	// the check should pass with a warning
	if !result.Passed {
		t.Errorf("expected check to pass with Force, got errors: %v", result.Errors)
	}
	// Should have DIRTY_FORCED warning, not auto-stash
	found := false
	for _, w := range result.Warnings {
		if w.Code == "DIRTY_FORCED" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DIRTY_FORCED warning when Force takes precedence")
	}
	// Should NOT have called stash
	if mock.stashPushCalled {
		t.Error("should not have called stash when Force takes precedence")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
