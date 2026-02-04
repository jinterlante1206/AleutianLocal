// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package transaction provides atomic file operations with git-based rollback.
//
// # Description
//
// This package implements transactional file operations using git checkpoints.
// When a transaction begins, the current git state is captured. If the transaction
// fails or is explicitly rolled back, all changes are reverted to the checkpoint.
// If the transaction succeeds, changes can be committed as a single unit.
//
// # Thread Safety
//
// TransactionManager is safe for concurrent use from multiple goroutines.
// Only one transaction may be active at a time per manager instance.
//
// # Nested Transactions
//
// Nested transactions are NOT supported. Attempting to begin a transaction
// while one is already active will return ErrTransactionActive.
package transaction

import (
	"context"
	"errors"
	"time"
)

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrTransactionActive is returned when attempting to begin a transaction
	// while one is already in progress.
	ErrTransactionActive = errors.New("transaction already active")

	// ErrNoTransaction is returned when attempting to commit or rollback
	// when no transaction is active.
	ErrNoTransaction = errors.New("no active transaction")

	// ErrNotGitRepository is returned when the working directory is not
	// a git repository.
	ErrNotGitRepository = errors.New("not a git repository")

	// ErrRebaseInProgress is returned when a git rebase is in progress,
	// which would interfere with transaction operations.
	ErrRebaseInProgress = errors.New("git rebase in progress")

	// ErrMergeInProgress is returned when a git merge is in progress,
	// which would interfere with transaction operations.
	ErrMergeInProgress = errors.New("git merge in progress")

	// ErrDetachedHead is returned when HEAD is not attached to a branch.
	// Agent operations may not work correctly in detached HEAD state.
	ErrDetachedHead = errors.New("repository is in detached HEAD state")

	// ErrCherryPickInProgress is returned when a cherry-pick operation is incomplete.
	// Complete or abort the cherry-pick before starting agent operations.
	ErrCherryPickInProgress = errors.New("cherry-pick in progress")

	// ErrBisectInProgress is returned when a git bisect is in progress.
	// Complete or reset the bisect before starting agent operations.
	ErrBisectInProgress = errors.New("git bisect in progress")

	// ErrDirtyWorkingTree is returned when uncommitted changes are present.
	// Commit, stash, or discard changes before starting agent operations.
	ErrDirtyWorkingTree = errors.New("uncommitted changes in working tree")

	// ErrTransactionExpired is returned when a transaction has exceeded
	// its TTL and was auto-rolled back.
	ErrTransactionExpired = errors.New("transaction expired")

	// ErrRollbackFailed is returned when rollback fails, leaving the
	// repository in an inconsistent state requiring manual intervention.
	ErrRollbackFailed = errors.New("rollback failed: manual intervention required")

	// ErrCheckpointNotFound is returned when the checkpoint reference
	// cannot be found (may have been garbage collected or corrupted).
	ErrCheckpointNotFound = errors.New("checkpoint not found")

	// ErrMaxFilesExceeded is returned when trying to track more files
	// than the configured maximum.
	ErrMaxFilesExceeded = errors.New("maximum tracked files exceeded")
)

// =============================================================================
// Transaction Status
// =============================================================================

// Status represents the current state of a transaction.
type Status string

const (
	// StatusIdle indicates no active transaction.
	StatusIdle Status = "idle"

	// StatusActive indicates a transaction is in progress.
	StatusActive Status = "active"

	// StatusCommitting indicates a commit is in progress.
	StatusCommitting Status = "committing"

	// StatusRollingBack indicates a rollback is in progress.
	StatusRollingBack Status = "rolling_back"

	// StatusCommitted indicates the transaction was successfully committed.
	StatusCommitted Status = "committed"

	// StatusRolledBack indicates the transaction was rolled back.
	StatusRolledBack Status = "rolled_back"

	// StatusFailed indicates the transaction failed (rollback also failed).
	StatusFailed Status = "failed"
)

// String returns the string representation of the status.
func (s Status) String() string {
	return string(s)
}

// IsTerminal returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	return s == StatusCommitted || s == StatusRolledBack || s == StatusFailed
}

// =============================================================================
// Checkpoint Strategy
// =============================================================================

// Strategy defines how checkpoints are created and managed.
type Strategy string

const (
	// StrategyStash uses git stash to save the current state.
	// Pros: Simple, no branch pollution.
	// Cons: Can conflict on pop, limited metadata.
	// Best for: Small changes, quick operations.
	StrategyStash Strategy = "stash"

	// StrategyBranch creates a temporary branch for the transaction.
	// Pros: Clean history, easy to inspect agent's work.
	// Cons: More git operations, potential merge conflicts.
	// Best for: Complex changes, long-running transactions.
	StrategyBranch Strategy = "branch"

	// StrategyWorktree creates an isolated git worktree.
	// Pros: Complete isolation, can't corrupt original.
	// Cons: More disk space, path management complexity.
	// Best for: Sandboxed operations, parallel transactions.
	StrategyWorktree Strategy = "worktree"
)

// String returns the string representation of the strategy.
func (s Strategy) String() string {
	return string(s)
}

// =============================================================================
// Transaction
// =============================================================================

// Transaction represents an active transactional checkpoint.
//
// # Description
//
// A Transaction captures the state at Begin() and allows either
// committing changes or rolling back to the captured state.
//
// # Thread Safety
//
// Transaction objects should only be accessed through the TransactionManager.
type Transaction struct {
	// ID is the unique identifier for this transaction.
	ID string `json:"id"`

	// SessionID links the transaction to an agent session.
	SessionID string `json:"session_id"`

	// StartedAt is when the transaction began.
	StartedAt time.Time `json:"started_at"`

	// ExpiresAt is when the transaction will auto-rollback if not completed.
	ExpiresAt time.Time `json:"expires_at"`

	// CheckpointRef is the git ref (commit SHA) to restore on rollback.
	CheckpointRef string `json:"checkpoint_ref"`

	// OriginalBranch is the branch we started on.
	OriginalBranch string `json:"original_branch"`

	// WorkBranch is the temporary branch name (for branch strategy).
	WorkBranch string `json:"work_branch,omitempty"`

	// WorktreePath is the worktree directory (for worktree strategy).
	WorktreePath string `json:"worktree_path,omitempty"`

	// StashRef is the stash reference (for stash strategy).
	StashRef string `json:"stash_ref,omitempty"`

	// ModifiedFiles tracks files changed during the transaction.
	// Stored as a map for O(1) deduplication.
	ModifiedFiles map[string]struct{} `json:"modified_files"`

	// Status is the current transaction state.
	Status Status `json:"status"`

	// Strategy used for this transaction.
	Strategy Strategy `json:"strategy"`

	// RollbackReason is set if the transaction was rolled back.
	RollbackReason string `json:"rollback_reason,omitempty"`

	// Error is set if the transaction failed.
	Error string `json:"error,omitempty"`
}

// IsExpired returns true if the transaction has exceeded its TTL.
func (t *Transaction) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// Duration returns how long the transaction has been active.
func (t *Transaction) Duration() time.Duration {
	return time.Since(t.StartedAt)
}

// FileCount returns the number of files modified in this transaction.
func (t *Transaction) FileCount() int {
	return len(t.ModifiedFiles)
}

// Files returns the list of modified files as a slice.
func (t *Transaction) Files() []string {
	files := make([]string, 0, len(t.ModifiedFiles))
	for f := range t.ModifiedFiles {
		files = append(files, f)
	}
	return files
}

// =============================================================================
// Manager Configuration
// =============================================================================

// Config configures the TransactionManager behavior.
type Config struct {
	// RepoPath is the git repository root directory.
	// Must be an absolute path.
	RepoPath string

	// Strategy is the checkpoint strategy to use.
	// Default: StrategyWorktree (prevents LSP/IDE conflicts)
	// Note: Use StrategyBranch if worktrees not supported or disk space is a concern.
	Strategy Strategy

	// TransactionTTL is how long a transaction can be active before
	// auto-rollback. Zero means no timeout.
	// Default: 30 minutes
	TransactionTTL time.Duration

	// GitTimeout is the maximum time for a single git operation.
	// Default: 30 seconds
	GitTimeout time.Duration

	// MaxTrackedFiles is the maximum number of files to track per transaction.
	// Prevents memory exhaustion on large operations.
	// Default: 10000
	MaxTrackedFiles int

	// StateDir is where transaction state is persisted for crash recovery.
	// Default: {RepoPath}/.aleutian/transactions
	StateDir string

	// CleanupOnInit removes stale transactions on manager creation.
	// Default: true
	CleanupOnInit bool

	// TracingEnabled controls whether OpenTelemetry spans are emitted.
	// When false, uses noop tracer for zero overhead.
	// Default: true
	TracingEnabled bool

	// MetricsEnabled controls whether Prometheus metrics are recorded.
	// Default: true
	MetricsEnabled bool

	// PreFlight configures pre-flight repository state validation.
	// These checks run before Begin() to prevent data loss.
	PreFlight PreFlightConfig
}

// DefaultConfig returns a Config with sensible defaults.
//
// # Note on Strategy Default
//
// The default is StrategyWorktree to prevent IDE/LSP conflicts. When the agent
// uses branch switching, the user's gopls/LSP sees thousands of file changes,
// causing cache invalidation and IDE freezes. Worktrees isolate agent work to
// a separate directory, leaving the user's workspace untouched.
//
// Use StrategyBranch if:
//   - Git worktrees are not supported (older git versions)
//   - Disk space is constrained (worktrees require ~2x repo size)
//   - User explicitly requests branch-based isolation
func DefaultConfig() Config {
	return Config{
		Strategy:        StrategyWorktree,
		TransactionTTL:  30 * time.Minute,
		GitTimeout:      30 * time.Second,
		MaxTrackedFiles: 10000,
		CleanupOnInit:   true,
		TracingEnabled:  true,
		MetricsEnabled:  true,
	}
}

// =============================================================================
// Git Client Interface
// =============================================================================

// GitClient abstracts git operations for testing.
//
// # Description
//
// Implementations must be safe for concurrent use.
type GitClient interface {
	// IsGitRepository checks if the path is a git repository.
	IsGitRepository(ctx context.Context) bool

	// HasRebaseInProgress checks if a rebase is in progress.
	HasRebaseInProgress(ctx context.Context) bool

	// HasMergeInProgress checks if a merge is in progress.
	HasMergeInProgress(ctx context.Context) bool

	// IsDetachedHead checks if HEAD is detached (not on a branch).
	IsDetachedHead(ctx context.Context) bool

	// HasCherryPickInProgress checks if a cherry-pick is in progress.
	HasCherryPickInProgress(ctx context.Context) bool

	// HasBisectInProgress checks if a git bisect is in progress.
	HasBisectInProgress(ctx context.Context) bool

	// GetCurrentBranch returns the current branch name, or "HEAD" if detached.
	GetCurrentBranch(ctx context.Context) (string, error)

	// RevParse resolves a git ref to a commit SHA.
	RevParse(ctx context.Context, ref string) (string, error)

	// RefExists checks if a git ref exists.
	RefExists(ctx context.Context, ref string) bool

	// Stash operations
	StashPush(ctx context.Context, message string) error
	StashPop(ctx context.Context) error
	StashDrop(ctx context.Context, ref string) error
	StashList(ctx context.Context) ([]StashEntry, error)

	// Branch operations
	CreateBranch(ctx context.Context, name string) error
	DeleteBranch(ctx context.Context, name string, force bool) error
	Checkout(ctx context.Context, ref string) error
	BranchExists(ctx context.Context, name string) bool

	// Reset operations
	ResetHard(ctx context.Context, ref string) error
	CleanUntracked(ctx context.Context) error

	// Commit operations
	Add(ctx context.Context, paths ...string) error
	AddAll(ctx context.Context) error
	Commit(ctx context.Context, message string) error
	HasStagedChanges(ctx context.Context) bool
	HasUnstagedChanges(ctx context.Context) bool

	// Status
	Status(ctx context.Context) (*GitStatus, error)

	// Worktree operations
	CreateWorktree(ctx context.Context, path string, ref string) error
	RemoveWorktree(ctx context.Context, path string, force bool) error
	WorktreeList(ctx context.Context) ([]WorktreeEntry, error)
}

// WorktreeEntry represents a git worktree.
type WorktreeEntry struct {
	// Path is the absolute filesystem path to the worktree.
	Path string

	// HEAD is the current commit SHA of the worktree.
	HEAD string

	// Branch is the branch name, or empty string if detached HEAD.
	Branch string

	// Locked indicates if the worktree is locked.
	Locked bool
}

// StashEntry represents a git stash entry.
type StashEntry struct {
	Index   int
	Ref     string
	Message string
}

// GitStatus represents the current git working tree status.
type GitStatus struct {
	// Branch is the current branch name.
	Branch string

	// IsClean is true if there are no uncommitted changes.
	IsClean bool

	// StagedFiles are files staged for commit.
	StagedFiles []string

	// ModifiedFiles are files with unstaged changes.
	ModifiedFiles []string

	// UntrackedFiles are untracked files.
	UntrackedFiles []string
}

// =============================================================================
// Transaction Result
// =============================================================================

// Result contains information about a completed transaction.
type Result struct {
	// TransactionID is the completed transaction's ID.
	TransactionID string

	// Status is the final status.
	Status Status

	// Duration is how long the transaction was active.
	Duration time.Duration

	// FilesModified is the count of files changed.
	FilesModified int

	// CommitSHA is the new commit SHA (if committed).
	CommitSHA string

	// RollbackReason is why the transaction was rolled back (if applicable).
	RollbackReason string
}
