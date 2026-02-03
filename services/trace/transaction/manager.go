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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager provides atomic file operations with git-based rollback.
//
// # Description
//
// TransactionManager wraps file operations in git checkpoints.
// On failure, all changes can be rolled back to the pre-transaction state.
// On success, changes can be committed as a single atomic unit.
//
// # Thread Safety
//
// All public methods are safe for concurrent use.
// Only one transaction may be active at a time.
//
// # Nested Transactions
//
// Nested transactions are NOT supported. Calling Begin() while a
// transaction is active returns ErrTransactionActive.
type Manager struct {
	config            Config
	git               GitClient
	activeTransaction *Transaction
	preflightGuard    *PreFlightGuard
	preflightStashRef string // Stash ref from preflight auto-stash
	mu                sync.Mutex
	logger            *slog.Logger
	tracer            *Tracer
}

// NewManager creates a new transaction manager.
//
// # Description
//
// Creates a manager with the specified configuration. If CleanupOnInit
// is true, recovers or cleans up any stale transactions from previous
// crashed sessions.
//
// # Inputs
//
//   - config: Manager configuration. Use DefaultConfig() for defaults.
//
// # Outputs
//
//   - *Manager: Ready-to-use transaction manager.
//   - error: Non-nil if setup fails.
//
// # Example
//
//	config := transaction.DefaultConfig()
//	config.RepoPath = "/path/to/repo"
//	manager, err := transaction.NewManager(config)
//	if err != nil {
//	    return err
//	}
//	defer manager.Close()
func NewManager(config Config) (*Manager, error) {
	if config.RepoPath == "" {
		return nil, fmt.Errorf("RepoPath is required")
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(config.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("resolving repo path: %w", err)
	}
	config.RepoPath = absPath

	// Apply defaults
	if config.Strategy == "" {
		config.Strategy = StrategyBranch
	}
	if config.TransactionTTL == 0 {
		config.TransactionTTL = 30 * time.Minute
	}
	if config.GitTimeout == 0 {
		config.GitTimeout = 30 * time.Second
	}
	if config.MaxTrackedFiles == 0 {
		config.MaxTrackedFiles = 10000
	}
	if config.StateDir == "" {
		config.StateDir = filepath.Join(config.RepoPath, ".aleutian", "transactions")
	}

	// Create git client
	git, err := NewGitClient(config.RepoPath, config.GitTimeout)
	if err != nil {
		return nil, fmt.Errorf("creating git client: %w", err)
	}

	// Create state directory
	if err := os.MkdirAll(config.StateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	logger := slog.Default().With("component", "transaction.Manager")

	// Initialize observability
	SetMetricsEnabled(config.MetricsEnabled)
	tracer := NewTracer(logger, config.TracingEnabled)

	// Initialize preflight guard
	preflightGuard := NewPreFlightGuard(git, config.PreFlight, logger)

	m := &Manager{
		config:         config,
		git:            git,
		preflightGuard: preflightGuard,
		logger:         logger,
		tracer:         tracer,
	}

	// Cleanup stale transactions on init
	if config.CleanupOnInit {
		if err := m.cleanupStale(context.Background()); err != nil {
			m.logger.Warn("failed to cleanup stale transactions",
				"error", err)
		}
	}

	return m, nil
}

// NewManagerWithGit creates a manager with a custom git client (for testing).
//
// # Description
//
// Allows injection of a mock GitClient for testing.
//
// # Inputs
//
//   - config: Manager configuration.
//   - git: Custom git client implementation.
//
// # Outputs
//
//   - *Manager: Ready-to-use transaction manager.
//   - error: Non-nil if setup fails.
func NewManagerWithGit(config Config, git GitClient) (*Manager, error) {
	if config.RepoPath == "" {
		return nil, fmt.Errorf("RepoPath is required")
	}

	// Apply defaults
	if config.Strategy == "" {
		config.Strategy = StrategyBranch
	}
	if config.TransactionTTL == 0 {
		config.TransactionTTL = 30 * time.Minute
	}
	if config.GitTimeout == 0 {
		config.GitTimeout = 30 * time.Second
	}
	if config.MaxTrackedFiles == 0 {
		config.MaxTrackedFiles = 10000
	}
	if config.StateDir == "" {
		config.StateDir = filepath.Join(config.RepoPath, ".aleutian", "transactions")
	}

	// Create state directory
	if err := os.MkdirAll(config.StateDir, 0755); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	logger := slog.Default().With("component", "transaction.Manager")

	// Initialize observability
	SetMetricsEnabled(config.MetricsEnabled)
	tracer := NewTracer(logger, config.TracingEnabled)

	// Initialize preflight guard
	preflightGuard := NewPreFlightGuard(git, config.PreFlight, logger)

	return &Manager{
		config:         config,
		git:            git,
		preflightGuard: preflightGuard,
		logger:         logger,
		tracer:         tracer,
	}, nil
}

// Begin starts a new transaction with a checkpoint.
//
// # Description
//
// Creates a checkpoint of the current git state. All subsequent file
// modifications can be rolled back to this point. Only one transaction
// may be active at a time.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - sessionID: Identifier for the agent session (for logging/debugging).
//
// # Outputs
//
//   - *Transaction: The active transaction.
//   - error: ErrTransactionActive if a transaction is already in progress,
//     ErrNotGitRepository if not a git repo, or other errors on failure.
//
// # Example
//
//	tx, err := manager.Begin(ctx, "session-123")
//	if err != nil {
//	    return err
//	}
//	// ... make changes ...
//	if failed {
//	    manager.Rollback(ctx, "operation failed")
//	} else {
//	    manager.Commit(ctx, "completed task")
//	}
func (m *Manager) Begin(ctx context.Context, sessionID string) (tx *Transaction, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start tracing span
	ctx, span := m.tracer.StartBegin(ctx, sessionID, m.config.Strategy)
	defer func() { m.tracer.EndBegin(span, tx, err) }()

	// Use logger with trace context
	logger := LoggerWithTrace(ctx, m.logger)

	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Begin: %v", r)
			logger.Error("panic in Begin",
				"panic", r,
				"session_id", sessionID)
		}
	}()

	// Record metrics on exit
	defer func() {
		recordBegin(ctx, m.config.Strategy, err == nil)
		if err == nil {
			incActive(ctx)
		}
	}()

	// Check for existing transaction
	if m.activeTransaction != nil {
		return nil, ErrTransactionActive
	}

	// Pre-flight checks (replaces basic health checks)
	preflightResult, preflightErr := m.preflightGuard.Check(ctx)
	if preflightErr != nil {
		return nil, fmt.Errorf("preflight check: %w", preflightErr)
	}
	if !preflightResult.Passed {
		logger.Warn("preflight check failed",
			"errors", len(preflightResult.Errors),
			"first_error", preflightResult.Errors[0].Code)
		return nil, preflightResult.FirstError()
	}

	// Store stash ref if auto-stash was used
	m.preflightStashRef = preflightResult.StashRef

	// Log any warnings
	for _, warning := range preflightResult.Warnings {
		logger.Warn("preflight warning",
			"code", warning.Code,
			"message", warning.Message)
	}

	// Get current state
	currentBranch, err := m.git.GetCurrentBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting current branch: %w", err)
	}

	checkpointRef, err := m.git.RevParse(ctx, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("getting HEAD: %w", err)
	}

	// Create transaction
	now := time.Now()
	tx = &Transaction{
		ID:             uuid.New().String(),
		SessionID:      sessionID,
		StartedAt:      now,
		ExpiresAt:      now.Add(m.config.TransactionTTL),
		CheckpointRef:  checkpointRef,
		OriginalBranch: currentBranch,
		ModifiedFiles:  make(map[string]struct{}),
		Status:         StatusActive,
		Strategy:       m.config.Strategy,
	}

	// Create checkpoint based on strategy (with git operation tracing)
	switch m.config.Strategy {
	case StrategyStash:
		if err := m.createStashCheckpointWithTrace(ctx, tx); err != nil {
			return nil, fmt.Errorf("creating stash checkpoint: %w", err)
		}
	case StrategyBranch:
		if err := m.createBranchCheckpointWithTrace(ctx, tx); err != nil {
			return nil, fmt.Errorf("creating branch checkpoint: %w", err)
		}
	case StrategyWorktree:
		if err := m.createWorktreeCheckpointWithTrace(ctx, tx); err != nil {
			return nil, fmt.Errorf("creating worktree checkpoint: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown strategy: %s", m.config.Strategy)
	}

	// Persist transaction state for crash recovery
	if err := m.persistTransaction(tx); err != nil {
		logger.Warn("failed to persist transaction state",
			"tx_id", tx.ID,
			"error", err)
	}

	m.activeTransaction = tx
	logger.Info("transaction started",
		"tx_id", tx.ID,
		"session_id", sessionID,
		"strategy", m.config.Strategy,
		"checkpoint", checkpointRef,
		"expires_at", tx.ExpiresAt.Format(time.RFC3339))

	return tx, nil
}

// Commit finalizes the transaction and persists changes.
//
// # Description
//
// Completes the transaction, making all changes permanent. The checkpoint
// is removed. After commit, no rollback is possible.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - message: Commit message describing the changes.
//
// # Outputs
//
//   - *Result: Information about the completed transaction.
//   - error: ErrNoTransaction if no transaction is active, or other errors.
func (m *Manager) Commit(ctx context.Context, message string) (result *Result, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction == nil {
		return nil, ErrNoTransaction
	}

	tx := m.activeTransaction

	// Start tracing span
	ctx, span := m.tracer.StartCommit(ctx, tx, message)
	defer func() { m.tracer.EndCommit(span, result, err) }()

	// Use logger with trace context
	logger := LoggerWithTrace(ctx, m.logger)

	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Commit: %v", r)
			logger.Error("panic in Commit", "panic", r)
		}
	}()

	// Record metrics on exit
	defer func() {
		if err == nil && result != nil {
			recordCommit(ctx, result.Duration, result.FilesModified, true)
		} else {
			recordCommit(ctx, tx.Duration(), tx.FileCount(), false)
		}
		decActive(ctx)
	}()

	// Record state transition
	m.tracer.RecordStateTransition(ctx, tx.ID, tx.Status, StatusCommitting, time.Since(tx.StartedAt))
	tx.Status = StatusCommitting

	// Check for expiration
	if tx.IsExpired() {
		logger.Warn("transaction expired, rolling back",
			"tx_id", tx.ID,
			"started_at", tx.StartedAt.Format(time.RFC3339))
		m.tracer.RecordExpiration(ctx, tx.ID)
		recordExpired(ctx)
		// Use background context for rollback to ensure it completes
		_, _ = m.rollbackInternal(context.Background(), tx, "transaction expired")
		m.activeTransaction = nil
		return nil, ErrTransactionExpired
	}

	logger.Info("committing transaction",
		"tx_id", tx.ID,
		"files_modified", tx.FileCount(),
		"message", message)

	// Commit based on strategy (with git operation tracing)
	var commitSHA string
	switch tx.Strategy {
	case StrategyStash:
		commitSHA, err = m.commitStashWithTrace(ctx, tx, message)
	case StrategyBranch:
		commitSHA, err = m.commitBranchWithTrace(ctx, tx, message)
	case StrategyWorktree:
		commitSHA, err = m.commitWorktreeWithTrace(ctx, tx, message)
	}

	if err != nil {
		m.tracer.RecordStateTransition(ctx, tx.ID, StatusCommitting, StatusFailed, 0)
		tx.Status = StatusFailed
		tx.Error = err.Error()
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	m.tracer.RecordStateTransition(ctx, tx.ID, StatusCommitting, StatusCommitted, 0)
	tx.Status = StatusCommitted
	result = &Result{
		TransactionID: tx.ID,
		Status:        StatusCommitted,
		Duration:      tx.Duration(),
		FilesModified: tx.FileCount(),
		CommitSHA:     commitSHA,
	}

	// Cleanup persisted state
	_ = m.removePersistedTransaction(tx.ID)

	// Restore auto-stashed changes if any
	if m.preflightStashRef != "" {
		if cleanupErr := m.preflightGuard.Cleanup(ctx, m.preflightStashRef); cleanupErr != nil {
			logger.Warn("failed to restore auto-stashed changes (may have conflicts)",
				"error", cleanupErr)
		}
		m.preflightStashRef = ""
	}

	m.activeTransaction = nil
	logger.Info("transaction committed",
		"tx_id", tx.ID,
		"duration", result.Duration,
		"files_modified", result.FilesModified,
		"commit_sha", commitSHA)

	return result, nil
}

// Rollback discards all changes and restores to checkpoint.
//
// # Description
//
// Reverts all changes made since Begin() was called. The repository
// is restored to the exact state it was in when the transaction started.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation. Note: rollback uses a
//     background context internally to ensure completion even if ctx is cancelled.
//   - reason: Human-readable reason for the rollback (for logging).
//
// # Outputs
//
//   - *Result: Information about the rolled-back transaction.
//   - error: ErrNoTransaction if no transaction is active, ErrRollbackFailed
//     if rollback itself fails.
func (m *Manager) Rollback(ctx context.Context, reason string) (result *Result, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction == nil {
		return nil, ErrNoTransaction
	}

	tx := m.activeTransaction

	// Start tracing span (use original context for parent link)
	ctx, span := m.tracer.StartRollback(ctx, tx, reason)
	defer func() { m.tracer.EndRollback(span, result, err) }()

	// Record metrics on exit
	defer func() {
		if result != nil {
			recordRollback(ctx, result.Duration, result.FilesModified, reason)
		}
		decActive(ctx)
	}()

	// Use background context for rollback git operations to ensure they complete
	// even if the original context is cancelled
	bgCtx := context.Background()

	result, err = m.rollbackInternal(bgCtx, tx, reason)
	if err != nil {
		return nil, err
	}

	m.activeTransaction = nil
	return result, nil
}

// rollbackInternal performs the actual rollback (must be called with lock held).
func (m *Manager) rollbackInternal(ctx context.Context, tx *Transaction, reason string) (*Result, error) {
	// Use logger with trace context if available
	logger := LoggerWithTrace(ctx, m.logger)

	// Panic recovery for critical rollback
	defer func() {
		if r := recover(); r != nil {
			logger.Error("CRITICAL: panic during rollback",
				"panic", r,
				"tx_id", tx.ID)
		}
	}()

	prevStatus := tx.Status
	tx.Status = StatusRollingBack
	tx.RollbackReason = reason

	// Record state transition (if tracer is available and we have a valid span context)
	m.tracer.RecordStateTransition(ctx, tx.ID, prevStatus, StatusRollingBack, time.Since(tx.StartedAt))

	logger.Warn("rolling back transaction",
		"tx_id", tx.ID,
		"reason", reason,
		"files_modified", tx.FileCount())

	var err error
	switch tx.Strategy {
	case StrategyStash:
		err = m.rollbackStashWithTrace(ctx, tx)
	case StrategyBranch:
		err = m.rollbackBranchWithTrace(ctx, tx)
	case StrategyWorktree:
		err = m.rollbackWorktreeWithTrace(ctx, tx)
	}

	if err != nil {
		m.tracer.RecordStateTransition(ctx, tx.ID, StatusRollingBack, StatusFailed, 0)
		tx.Status = StatusFailed
		tx.Error = err.Error()
		logger.Error("CRITICAL: rollback failed",
			"tx_id", tx.ID,
			"error", err)
		return nil, fmt.Errorf("%w: %v", ErrRollbackFailed, err)
	}

	m.tracer.RecordStateTransition(ctx, tx.ID, StatusRollingBack, StatusRolledBack, 0)
	tx.Status = StatusRolledBack

	result := &Result{
		TransactionID:  tx.ID,
		Status:         StatusRolledBack,
		Duration:       tx.Duration(),
		FilesModified:  tx.FileCount(),
		RollbackReason: reason,
	}

	// Cleanup persisted state
	_ = m.removePersistedTransaction(tx.ID)

	// Restore auto-stashed changes if any
	if m.preflightStashRef != "" {
		if cleanupErr := m.preflightGuard.Cleanup(ctx, m.preflightStashRef); cleanupErr != nil {
			logger.Warn("failed to restore auto-stashed changes (may have conflicts)",
				"error", cleanupErr)
		}
		m.preflightStashRef = ""
	}

	logger.Info("transaction rolled back",
		"tx_id", tx.ID,
		"reason", reason,
		"duration", result.Duration)

	return result, nil
}

// RecordModification tracks a file change for auditing.
//
// # Description
//
// Records that a file was modified during this transaction. This is
// used for logging and debugging - the actual rollback uses git state.
//
// # Inputs
//
//   - filePath: Path to the modified file.
//
// # Outputs
//
//   - error: ErrMaxFilesExceeded if limit reached, nil otherwise.
func (m *Manager) RecordModification(filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction == nil {
		return nil // Silently ignore if no transaction
	}

	tx := m.activeTransaction

	// Check limit
	if len(tx.ModifiedFiles) >= m.config.MaxTrackedFiles {
		return ErrMaxFilesExceeded
	}

	tx.ModifiedFiles[filePath] = struct{}{}
	return nil
}

// RecordModifications tracks multiple file changes.
//
// # Description
//
// Batch version of RecordModification for efficiency.
//
// # Inputs
//
//   - paths: Paths to modified files.
//
// # Outputs
//
//   - error: ErrMaxFilesExceeded if limit would be exceeded.
func (m *Manager) RecordModifications(paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction == nil {
		return nil
	}

	tx := m.activeTransaction

	// Check limit
	if len(tx.ModifiedFiles)+len(paths) > m.config.MaxTrackedFiles {
		return ErrMaxFilesExceeded
	}

	for _, path := range paths {
		tx.ModifiedFiles[path] = struct{}{}
	}
	return nil
}

// Active returns the currently active transaction, or nil if none.
//
// # Description
//
// Returns a copy of the active transaction for inspection.
// The returned Transaction should not be modified.
//
// # Outputs
//
//   - *Transaction: Copy of active transaction, or nil.
func (m *Manager) Active() *Transaction {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction == nil {
		return nil
	}

	// Return a copy
	tx := *m.activeTransaction
	tx.ModifiedFiles = make(map[string]struct{}, len(m.activeTransaction.ModifiedFiles))
	for k, v := range m.activeTransaction.ModifiedFiles {
		tx.ModifiedFiles[k] = v
	}
	return &tx
}

// IsActive returns true if a transaction is currently active.
func (m *Manager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeTransaction != nil
}

// Close cleans up the manager.
//
// # Description
//
// If a transaction is active, it is rolled back. Auto-stashed changes are
// restored. Resources are released.
//
// # Outputs
//
//   - error: Non-nil if rollback fails.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeTransaction != nil {
		m.logger.Warn("closing manager with active transaction, rolling back",
			"tx_id", m.activeTransaction.ID)
		_, err := m.rollbackInternal(context.Background(), m.activeTransaction, "manager closed")
		m.activeTransaction = nil
		return err
	}

	// Restore auto-stashed changes if any (edge case: manager closed without Begin completing)
	if m.preflightStashRef != "" {
		if cleanupErr := m.preflightGuard.Cleanup(context.Background(), m.preflightStashRef); cleanupErr != nil {
			m.logger.Warn("failed to restore auto-stashed changes on close",
				"error", cleanupErr)
		}
		m.preflightStashRef = ""
	}

	return nil
}

// =============================================================================
// Health Checks
// =============================================================================

// healthCheck verifies the repository is in a good state for transactions.
func (m *Manager) healthCheck(ctx context.Context) error {
	// Check if git repository
	if !m.git.IsGitRepository(ctx) {
		return ErrNotGitRepository
	}

	// Check for rebase in progress
	if m.git.HasRebaseInProgress(ctx) {
		return ErrRebaseInProgress
	}

	// Check for merge in progress
	if m.git.HasMergeInProgress(ctx) {
		return ErrMergeInProgress
	}

	return nil
}

// =============================================================================
// Stash Strategy
// =============================================================================

// createStashCheckpoint creates a checkpoint using git stash.
func (m *Manager) createStashCheckpoint(ctx context.Context, tx *Transaction) error {
	// Check if there are changes to stash
	status, err := m.git.Status(ctx)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	if status.IsClean {
		// No changes to stash, just record the checkpoint ref
		tx.StashRef = ""
		return nil
	}

	// Stash current changes
	message := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
	if err := m.git.StashPush(ctx, message); err != nil {
		return fmt.Errorf("stashing changes: %w", err)
	}

	tx.StashRef = "stash@{0}"

	m.logger.Debug("created stash checkpoint",
		"tx_id", tx.ID,
		"stash_ref", tx.StashRef)

	return nil
}

// commitStash commits changes when using stash strategy.
func (m *Manager) commitStash(ctx context.Context, tx *Transaction, message string) (string, error) {
	// Drop the stash checkpoint (we don't need it anymore)
	if tx.StashRef != "" {
		// Find and drop our stash
		stashes, err := m.git.StashList(ctx)
		if err != nil {
			m.logger.Warn("failed to list stashes", "error", err)
		} else {
			stashMessage := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
			for _, stash := range stashes {
				if stash.Message == stashMessage {
					_ = m.git.StashDrop(ctx, stash.Ref)
					break
				}
			}
		}
	}

	// Stage all changes
	if err := m.git.AddAll(ctx); err != nil {
		return "", fmt.Errorf("staging changes: %w", err)
	}

	// Check if there are changes to commit
	if !m.git.HasStagedChanges(ctx) {
		m.logger.Info("no changes to commit")
		return tx.CheckpointRef, nil
	}

	// Commit
	if err := m.git.Commit(ctx, message); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	// Get the new commit SHA
	commitSHA, err := m.git.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	return commitSHA, nil
}

// rollbackStash rolls back to checkpoint using stash strategy.
func (m *Manager) rollbackStash(ctx context.Context, tx *Transaction) error {
	// Reset to checkpoint
	if err := m.git.ResetHard(ctx, tx.CheckpointRef); err != nil {
		return fmt.Errorf("resetting to checkpoint: %w", err)
	}

	// Clean untracked files
	if err := m.git.CleanUntracked(ctx); err != nil {
		m.logger.Warn("failed to clean untracked files", "error", err)
	}

	// Restore stashed changes if any
	if tx.StashRef != "" {
		// Find and pop our stash
		stashes, err := m.git.StashList(ctx)
		if err != nil {
			m.logger.Warn("failed to list stashes", "error", err)
		} else {
			stashMessage := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
			for _, stash := range stashes {
				if stash.Message == stashMessage {
					if err := m.git.StashPop(ctx); err != nil {
						m.logger.Warn("failed to pop stash (may have conflicts)",
							"error", err)
					}
					break
				}
			}
		}
	}

	return nil
}

// =============================================================================
// Branch Strategy
// =============================================================================

// createBranchCheckpoint creates a checkpoint using a temporary branch.
// It also preserves any pre-existing uncommitted changes (user's WIP) to prevent data loss.
func (m *Manager) createBranchCheckpoint(ctx context.Context, tx *Transaction) error {
	// Generate branch name
	tx.WorkBranch = fmt.Sprintf("agent-work-%s", tx.ID[:8])

	// CRITICAL: Preserve user's work-in-progress before doing anything.
	// If the user has uncommitted changes and we later rollback with reset --hard,
	// those changes would be lost forever. Stash them first.
	status, err := m.git.Status(ctx)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	if !status.IsClean {
		// User has uncommitted changes - stash them for safekeeping
		stashMessage := fmt.Sprintf("agent-user-wip-%s", tx.ID)
		if err := m.git.StashPush(ctx, stashMessage); err != nil {
			return fmt.Errorf("stashing user WIP: %w", err)
		}
		tx.StashRef = "stash@{0}" // Mark that we have a user stash to restore

		m.logger.Debug("stashed user WIP for preservation",
			"tx_id", tx.ID,
			"stash_message", stashMessage)
	}

	// Create the backup branch at current HEAD (don't switch to it)
	// This preserves the checkpoint without changing our working state
	if err := m.git.CreateBranch(ctx, tx.WorkBranch); err != nil {
		return fmt.Errorf("creating work branch: %w", err)
	}

	m.logger.Debug("created branch checkpoint",
		"tx_id", tx.ID,
		"work_branch", tx.WorkBranch,
		"checkpoint", tx.CheckpointRef)

	return nil
}

// commitBranch commits changes when using branch strategy.
// After committing, it restores any pre-existing user changes that were stashed.
func (m *Manager) commitBranch(ctx context.Context, tx *Transaction, message string) (string, error) {
	// Delete the backup branch (we don't need it anymore)
	if tx.WorkBranch != "" && m.git.BranchExists(ctx, tx.WorkBranch) {
		if err := m.git.DeleteBranch(ctx, tx.WorkBranch, true); err != nil {
			m.logger.Warn("failed to delete work branch", "error", err)
		}
	}

	// Stage all changes
	if err := m.git.AddAll(ctx); err != nil {
		return "", fmt.Errorf("staging changes: %w", err)
	}

	// Check if there are changes to commit
	if !m.git.HasStagedChanges(ctx) {
		m.logger.Info("no changes to commit")
		// Even if no agent changes, restore user's WIP
		m.restoreUserStash(ctx, tx)
		return tx.CheckpointRef, nil
	}

	// Commit
	if err := m.git.Commit(ctx, message); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	// Get the new commit SHA
	commitSHA, err := m.git.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	// Restore user's work-in-progress that was stashed at Begin time.
	// The user's uncommitted changes should not be lost just because
	// the agent committed its own changes.
	m.restoreUserStash(ctx, tx)

	return commitSHA, nil
}

// rollbackBranch rolls back to checkpoint using branch strategy.
// It restores any pre-existing user changes that were stashed at Begin time.
func (m *Manager) rollbackBranch(ctx context.Context, tx *Transaction) error {
	// Reset to checkpoint
	if err := m.git.ResetHard(ctx, tx.CheckpointRef); err != nil {
		return fmt.Errorf("resetting to checkpoint: %w", err)
	}

	// Clean untracked files
	if err := m.git.CleanUntracked(ctx); err != nil {
		m.logger.Warn("failed to clean untracked files", "error", err)
	}

	// Delete the work branch
	if tx.WorkBranch != "" && m.git.BranchExists(ctx, tx.WorkBranch) {
		if err := m.git.DeleteBranch(ctx, tx.WorkBranch, true); err != nil {
			m.logger.Warn("failed to delete work branch", "error", err)
		}
	}

	// CRITICAL: Restore user's work-in-progress that was stashed at Begin time.
	// This ensures the user doesn't lose their uncommitted changes after a rollback.
	m.restoreUserStash(ctx, tx)

	return nil
}

// restoreUserStash restores the user's work-in-progress that was stashed at Begin time.
// This is a helper used by both commitBranch and rollbackBranch.
func (m *Manager) restoreUserStash(ctx context.Context, tx *Transaction) {
	if tx.StashRef == "" {
		return
	}

	stashMessage := fmt.Sprintf("agent-user-wip-%s", tx.ID)
	stashes, err := m.git.StashList(ctx)
	if err != nil {
		m.logger.Warn("failed to list stashes for user WIP restore", "error", err)
		return
	}

	for _, stash := range stashes {
		if stash.Message == stashMessage {
			if err := m.git.StashPop(ctx); err != nil {
				m.logger.Warn("failed to restore user WIP (may have conflicts)",
					"error", err)
			} else {
				m.logger.Debug("restored user WIP", "tx_id", tx.ID)
			}
			break
		}
	}
}

// =============================================================================
// Worktree Strategy
// =============================================================================

// createWorktreeCheckpoint creates an isolated worktree for the transaction.
//
// # Description
//
// Creates a detached worktree at .aleutian/worktrees/{tx.ID}. This provides
// complete isolation from the main working directory.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - tx: Transaction to initialize (WorktreePath will be set).
//
// # Outputs
//
//   - error: Non-nil if worktree creation fails.
func (m *Manager) createWorktreeCheckpoint(ctx context.Context, tx *Transaction) error {
	// Generate worktree path
	worktreePath := filepath.Join(m.config.RepoPath, ".aleutian", "worktrees", tx.ID)

	// Ensure parent directory exists
	parentDir := filepath.Dir(worktreePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("creating worktree parent directory: %w", err)
	}

	// Clean up any existing worktree at this path (crash recovery)
	if _, err := os.Stat(worktreePath); err == nil {
		m.logger.Warn("cleaning up existing worktree from previous crash",
			"path", worktreePath)
		if err := m.git.RemoveWorktree(ctx, worktreePath, true); err != nil {
			// If git removal fails, try direct filesystem removal
			if err := os.RemoveAll(worktreePath); err != nil {
				return fmt.Errorf("cleaning up existing worktree: %w", err)
			}
		}
	}

	// Get current HEAD as checkpoint
	headSHA, err := m.git.RevParse(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("getting HEAD SHA: %w", err)
	}
	tx.CheckpointRef = headSHA

	// Create worktree at HEAD (detached)
	if err := m.git.CreateWorktree(ctx, worktreePath, headSHA); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	tx.WorktreePath = worktreePath
	return nil
}

// commitWorktree commits changes from the worktree to the main branch.
//
// # Description
//
// Stages and commits all changes in the worktree, then cherry-picks the commit
// to the main branch. The worktree is removed after successful commit.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - tx: Transaction with worktree path.
//   - message: Commit message.
//
// # Outputs
//
//   - string: Commit SHA on success.
//   - error: Non-nil if commit fails.
func (m *Manager) commitWorktree(ctx context.Context, tx *Transaction, message string) (string, error) {
	if tx.WorktreePath == "" {
		return "", fmt.Errorf("no worktree path set")
	}

	// Create a git client for the worktree
	worktreeGit, err := NewGitClient(tx.WorktreePath, m.config.GitTimeout)
	if err != nil {
		return "", fmt.Errorf("creating worktree git client: %w", err)
	}

	// Check for changes in worktree
	status, err := worktreeGit.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("getting worktree status: %w", err)
	}

	if status.IsClean {
		// No changes - clean up worktree and return original checkpoint
		m.logger.Info("no changes in worktree")
		if err := m.git.RemoveWorktree(ctx, tx.WorktreePath, false); err != nil {
			m.logger.Warn("failed to remove worktree", "error", err)
		}
		return tx.CheckpointRef, nil
	}

	// Stage all changes in worktree
	if err := worktreeGit.AddAll(ctx); err != nil {
		return "", fmt.Errorf("staging changes in worktree: %w", err)
	}

	// Commit in worktree
	if err := worktreeGit.Commit(ctx, message); err != nil {
		return "", fmt.Errorf("committing in worktree: %w", err)
	}

	// Get the new commit SHA
	commitSHA, err := worktreeGit.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting worktree commit SHA: %w", err)
	}

	// Cherry-pick the commit to the main working directory
	if err := m.git.Checkout(ctx, commitSHA); err != nil {
		return "", fmt.Errorf("checking out worktree commit: %w", err)
	}

	// Remove the worktree
	if err := m.git.RemoveWorktree(ctx, tx.WorktreePath, false); err != nil {
		m.logger.Warn("failed to remove worktree after commit", "error", err)
	}

	return commitSHA, nil
}

// rollbackWorktree discards the worktree and all changes.
//
// # Description
//
// Force-removes the worktree, discarding all uncommitted changes.
// This is the fastest rollback strategy as it doesn't need to reset the main repo.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - tx: Transaction with worktree path.
//
// # Outputs
//
//   - error: Non-nil if removal fails.
func (m *Manager) rollbackWorktree(ctx context.Context, tx *Transaction) error {
	if tx.WorktreePath == "" {
		return nil // No worktree to clean up
	}

	// Force remove worktree (discards all changes)
	if err := m.git.RemoveWorktree(ctx, tx.WorktreePath, true); err != nil {
		// If git removal fails, try direct filesystem removal
		m.logger.Warn("git worktree remove failed, trying filesystem removal",
			"path", tx.WorktreePath,
			"error", err)
		if err := os.RemoveAll(tx.WorktreePath); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}
	}

	return nil
}

// =============================================================================
// Persistence for Crash Recovery
// =============================================================================

// transactionStatePath returns the path to the transaction state file.
func (m *Manager) transactionStatePath(txID string) string {
	return filepath.Join(m.config.StateDir, txID+".json")
}

// persistTransaction saves the transaction state for crash recovery.
func (m *Manager) persistTransaction(tx *Transaction) error {
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling transaction: %w", err)
	}

	// Ensure state directory exists (may have been removed by git reset --hard)
	if err := os.MkdirAll(m.config.StateDir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	path := m.transactionStatePath(tx.ID)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing transaction state: %w", err)
	}

	return nil
}

// removePersistedTransaction removes the persisted transaction state.
func (m *Manager) removePersistedTransaction(txID string) error {
	path := m.transactionStatePath(txID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing transaction state: %w", err)
	}
	return nil
}

// cleanupStale recovers or cleans up stale transactions.
func (m *Manager) cleanupStale(ctx context.Context) error {
	entries, err := os.ReadDir(m.config.StateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading state directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(m.config.StateDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			m.logger.Warn("failed to read stale transaction", "path", path, "error", err)
			continue
		}

		var tx Transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			m.logger.Warn("failed to parse stale transaction", "path", path, "error", err)
			_ = os.Remove(path)
			continue
		}

		m.logger.Info("found stale transaction, cleaning up",
			"tx_id", tx.ID,
			"started_at", tx.StartedAt.Format(time.RFC3339),
			"strategy", tx.Strategy)

		// Try to rollback the stale transaction
		_, err = m.rollbackInternal(ctx, &tx, "stale transaction cleanup")
		if err != nil {
			m.logger.Warn("failed to rollback stale transaction", "tx_id", tx.ID, "error", err)
		}

		// Remove the state file regardless of rollback success
		_ = os.Remove(path)
	}

	return nil
}

// =============================================================================
// Traced Strategy Operations
// =============================================================================

// createStashCheckpointWithTrace wraps createStashCheckpoint with tracing.
func (m *Manager) createStashCheckpointWithTrace(ctx context.Context, tx *Transaction) error {
	// Check if there are changes to stash
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "status")
	start := time.Now()
	status, err := m.git.Status(gitCtx)
	recordGitOp(ctx, "status", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	if status.IsClean {
		// No changes to stash, just record the checkpoint ref
		tx.StashRef = ""
		return nil
	}

	// Stash current changes
	message := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "stash_push")
	start = time.Now()
	err = m.git.StashPush(gitCtx, message)
	recordGitOp(ctx, "stash_push", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("stashing changes: %w", err)
	}

	tx.StashRef = "stash@{0}"

	m.logger.Debug("created stash checkpoint",
		"tx_id", tx.ID,
		"stash_ref", tx.StashRef)

	return nil
}

// createBranchCheckpointWithTrace wraps createBranchCheckpoint with tracing.
func (m *Manager) createBranchCheckpointWithTrace(ctx context.Context, tx *Transaction) error {
	// Generate branch name
	tx.WorkBranch = fmt.Sprintf("agent-work-%s", tx.ID[:8])

	// CRITICAL: Preserve user's work-in-progress before doing anything.
	gitCtx, statusSpan := m.tracer.StartGitOp(ctx, "status")
	start := time.Now()
	status, err := m.git.Status(gitCtx)
	recordGitOp(ctx, "status", time.Since(start), err)
	m.tracer.EndGitOp(statusSpan, err)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	if !status.IsClean {
		// User has uncommitted changes - stash them for safekeeping
		stashMessage := fmt.Sprintf("agent-user-wip-%s", tx.ID)
		gitCtx, stashSpan := m.tracer.StartGitOp(ctx, "stash_push")
		start := time.Now()
		err := m.git.StashPush(gitCtx, stashMessage)
		recordGitOp(ctx, "stash_push", time.Since(start), err)
		m.tracer.EndGitOp(stashSpan, err)
		if err != nil {
			return fmt.Errorf("stashing user WIP: %w", err)
		}
		tx.StashRef = "stash@{0}"

		m.logger.Debug("stashed user WIP for preservation",
			"tx_id", tx.ID,
			"stash_message", stashMessage)
	}

	// Create the backup branch at current HEAD (don't switch to it)
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "create_branch")
	start = time.Now()
	err = m.git.CreateBranch(gitCtx, tx.WorkBranch)
	recordGitOp(ctx, "create_branch", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("creating work branch: %w", err)
	}

	m.logger.Debug("created branch checkpoint",
		"tx_id", tx.ID,
		"work_branch", tx.WorkBranch,
		"checkpoint", tx.CheckpointRef)

	return nil
}

// commitStashWithTrace wraps commitStash with tracing.
func (m *Manager) commitStashWithTrace(ctx context.Context, tx *Transaction, message string) (string, error) {
	// Drop the stash checkpoint (we don't need it anymore)
	if tx.StashRef != "" {
		gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "stash_list")
		start := time.Now()
		stashes, err := m.git.StashList(gitCtx)
		recordGitOp(ctx, "stash_list", time.Since(start), err)
		m.tracer.EndGitOp(gitSpan, err)
		if err != nil {
			m.logger.Warn("failed to list stashes", "error", err)
		} else {
			stashMessage := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
			for _, stash := range stashes {
				if stash.Message == stashMessage {
					gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "stash_drop")
					start = time.Now()
					dropErr := m.git.StashDrop(gitCtx, stash.Ref)
					recordGitOp(ctx, "stash_drop", time.Since(start), dropErr)
					m.tracer.EndGitOp(gitSpan, dropErr)
					break
				}
			}
		}
	}

	// Stage all changes
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "add_all")
	start := time.Now()
	err := m.git.AddAll(gitCtx)
	recordGitOp(ctx, "add_all", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("staging changes: %w", err)
	}

	// Check if there are changes to commit
	if !m.git.HasStagedChanges(ctx) {
		m.logger.Info("no changes to commit")
		return tx.CheckpointRef, nil
	}

	// Commit
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "commit")
	start = time.Now()
	err = m.git.Commit(gitCtx, message)
	recordGitOp(ctx, "commit", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	// Get the new commit SHA
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "rev_parse")
	start = time.Now()
	commitSHA, err := m.git.RevParse(gitCtx, "HEAD")
	recordGitOp(ctx, "rev_parse", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	return commitSHA, nil
}

// commitBranchWithTrace wraps commitBranch with tracing.
func (m *Manager) commitBranchWithTrace(ctx context.Context, tx *Transaction, message string) (string, error) {
	// Delete the backup branch (we don't need it anymore)
	if tx.WorkBranch != "" && m.git.BranchExists(ctx, tx.WorkBranch) {
		gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "delete_branch")
		start := time.Now()
		err := m.git.DeleteBranch(gitCtx, tx.WorkBranch, true)
		recordGitOp(ctx, "delete_branch", time.Since(start), err)
		m.tracer.EndGitOp(gitSpan, err)
		if err != nil {
			m.logger.Warn("failed to delete work branch", "error", err)
		}
	}

	// Stage all changes
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "add_all")
	start := time.Now()
	err := m.git.AddAll(gitCtx)
	recordGitOp(ctx, "add_all", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("staging changes: %w", err)
	}

	// Check if there are changes to commit
	if !m.git.HasStagedChanges(ctx) {
		m.logger.Info("no changes to commit")
		// Even if no agent changes, restore user's WIP
		m.restoreUserStash(ctx, tx)
		return tx.CheckpointRef, nil
	}

	// Commit
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "commit")
	start = time.Now()
	err = m.git.Commit(gitCtx, message)
	recordGitOp(ctx, "commit", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	// Get the new commit SHA
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "rev_parse")
	start = time.Now()
	commitSHA, err := m.git.RevParse(gitCtx, "HEAD")
	recordGitOp(ctx, "rev_parse", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	// Restore user's work-in-progress that was stashed at Begin time.
	m.restoreUserStash(ctx, tx)

	return commitSHA, nil
}

// rollbackStashWithTrace wraps rollbackStash with tracing.
func (m *Manager) rollbackStashWithTrace(ctx context.Context, tx *Transaction) error {
	// Reset to checkpoint
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "reset_hard")
	start := time.Now()
	err := m.git.ResetHard(gitCtx, tx.CheckpointRef)
	recordGitOp(ctx, "reset_hard", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("resetting to checkpoint: %w", err)
	}

	// Clean untracked files
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "clean_untracked")
	start = time.Now()
	cleanErr := m.git.CleanUntracked(gitCtx)
	recordGitOp(ctx, "clean_untracked", time.Since(start), cleanErr)
	m.tracer.EndGitOp(gitSpan, cleanErr)
	if cleanErr != nil {
		m.logger.Warn("failed to clean untracked files", "error", cleanErr)
	}

	// Restore stashed changes if any
	if tx.StashRef != "" {
		gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "stash_list")
		start = time.Now()
		stashes, err := m.git.StashList(gitCtx)
		recordGitOp(ctx, "stash_list", time.Since(start), err)
		m.tracer.EndGitOp(gitSpan, err)
		if err != nil {
			m.logger.Warn("failed to list stashes", "error", err)
		} else {
			stashMessage := fmt.Sprintf("agent-checkpoint-%s", tx.ID)
			for _, stash := range stashes {
				if stash.Message == stashMessage {
					gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "stash_pop")
					start = time.Now()
					popErr := m.git.StashPop(gitCtx)
					recordGitOp(ctx, "stash_pop", time.Since(start), popErr)
					m.tracer.EndGitOp(gitSpan, popErr)
					if popErr != nil {
						m.logger.Warn("failed to pop stash (may have conflicts)",
							"error", popErr)
					}
					break
				}
			}
		}
	}

	return nil
}

// rollbackBranchWithTrace wraps rollbackBranch with tracing.
func (m *Manager) rollbackBranchWithTrace(ctx context.Context, tx *Transaction) error {
	// Reset to checkpoint
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "reset_hard")
	start := time.Now()
	err := m.git.ResetHard(gitCtx, tx.CheckpointRef)
	recordGitOp(ctx, "reset_hard", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("resetting to checkpoint: %w", err)
	}

	// Clean untracked files
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "clean_untracked")
	start = time.Now()
	cleanErr := m.git.CleanUntracked(gitCtx)
	recordGitOp(ctx, "clean_untracked", time.Since(start), cleanErr)
	m.tracer.EndGitOp(gitSpan, cleanErr)
	if cleanErr != nil {
		m.logger.Warn("failed to clean untracked files", "error", cleanErr)
	}

	// Delete the work branch
	if tx.WorkBranch != "" && m.git.BranchExists(ctx, tx.WorkBranch) {
		gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "delete_branch")
		start = time.Now()
		delErr := m.git.DeleteBranch(gitCtx, tx.WorkBranch, true)
		recordGitOp(ctx, "delete_branch", time.Since(start), delErr)
		m.tracer.EndGitOp(gitSpan, delErr)
		if delErr != nil {
			m.logger.Warn("failed to delete work branch", "error", delErr)
		}
	}

	// CRITICAL: Restore user's work-in-progress that was stashed at Begin time.
	m.restoreUserStash(ctx, tx)

	return nil
}

// createWorktreeCheckpointWithTrace wraps createWorktreeCheckpoint with tracing.
func (m *Manager) createWorktreeCheckpointWithTrace(ctx context.Context, tx *Transaction) error {
	// Generate worktree path
	worktreePath := filepath.Join(m.config.RepoPath, ".aleutian", "worktrees", tx.ID)

	// Ensure parent directory exists
	parentDir := filepath.Dir(worktreePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("creating worktree parent directory: %w", err)
	}

	// Clean up any existing worktree at this path (crash recovery)
	if _, err := os.Stat(worktreePath); err == nil {
		m.logger.Warn("cleaning up existing worktree from previous crash",
			"path", worktreePath)
		gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "worktree_remove")
		start := time.Now()
		removeErr := m.git.RemoveWorktree(gitCtx, worktreePath, true)
		recordGitOp(ctx, "worktree_remove", time.Since(start), removeErr)
		m.tracer.EndGitOp(gitSpan, removeErr)
		if removeErr != nil {
			// Try filesystem removal
			if err := os.RemoveAll(worktreePath); err != nil {
				return fmt.Errorf("cleaning up existing worktree: %w", err)
			}
		}
	}

	// Get current HEAD as checkpoint
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "rev_parse")
	start := time.Now()
	headSHA, err := m.git.RevParse(gitCtx, "HEAD")
	recordGitOp(ctx, "rev_parse", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("getting HEAD SHA: %w", err)
	}
	tx.CheckpointRef = headSHA

	// Create worktree at HEAD (detached)
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "worktree_add")
	start = time.Now()
	err = m.git.CreateWorktree(gitCtx, worktreePath, headSHA)
	recordGitOp(ctx, "worktree_add", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	tx.WorktreePath = worktreePath

	m.logger.Debug("created worktree checkpoint",
		"tx_id", tx.ID,
		"worktree_path", worktreePath,
		"checkpoint", tx.CheckpointRef)

	return nil
}

// commitWorktreeWithTrace wraps commitWorktree with tracing.
func (m *Manager) commitWorktreeWithTrace(ctx context.Context, tx *Transaction, message string) (string, error) {
	if tx.WorktreePath == "" {
		return "", fmt.Errorf("no worktree path set")
	}

	// Create a git client for the worktree
	worktreeGit, err := NewGitClient(tx.WorktreePath, m.config.GitTimeout)
	if err != nil {
		return "", fmt.Errorf("creating worktree git client: %w", err)
	}

	// Check for changes in worktree
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "status")
	start := time.Now()
	status, err := worktreeGit.Status(gitCtx)
	recordGitOp(ctx, "status", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("getting worktree status: %w", err)
	}

	if status.IsClean {
		// No changes - clean up worktree and return original checkpoint
		m.logger.Info("no changes in worktree")
		gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "worktree_remove")
		start = time.Now()
		removeErr := m.git.RemoveWorktree(gitCtx, tx.WorktreePath, false)
		recordGitOp(ctx, "worktree_remove", time.Since(start), removeErr)
		m.tracer.EndGitOp(gitSpan, removeErr)
		if removeErr != nil {
			m.logger.Warn("failed to remove worktree", "error", removeErr)
		}
		return tx.CheckpointRef, nil
	}

	// Stage all changes in worktree
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "add_all")
	start = time.Now()
	err = worktreeGit.AddAll(gitCtx)
	recordGitOp(ctx, "add_all", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("staging changes in worktree: %w", err)
	}

	// Commit in worktree
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "commit")
	start = time.Now()
	err = worktreeGit.Commit(gitCtx, message)
	recordGitOp(ctx, "commit", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("committing in worktree: %w", err)
	}

	// Get the new commit SHA
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "rev_parse")
	start = time.Now()
	commitSHA, err := worktreeGit.RevParse(gitCtx, "HEAD")
	recordGitOp(ctx, "rev_parse", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("getting worktree commit SHA: %w", err)
	}

	// Checkout the commit in main working directory
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "checkout")
	start = time.Now()
	err = m.git.Checkout(gitCtx, commitSHA)
	recordGitOp(ctx, "checkout", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		return "", fmt.Errorf("checking out worktree commit: %w", err)
	}

	// Remove the worktree
	gitCtx, gitSpan = m.tracer.StartGitOp(ctx, "worktree_remove")
	start = time.Now()
	removeErr := m.git.RemoveWorktree(gitCtx, tx.WorktreePath, false)
	recordGitOp(ctx, "worktree_remove", time.Since(start), removeErr)
	m.tracer.EndGitOp(gitSpan, removeErr)
	if removeErr != nil {
		m.logger.Warn("failed to remove worktree after commit", "error", removeErr)
	}

	return commitSHA, nil
}

// rollbackWorktreeWithTrace wraps rollbackWorktree with tracing.
func (m *Manager) rollbackWorktreeWithTrace(ctx context.Context, tx *Transaction) error {
	if tx.WorktreePath == "" {
		return nil // No worktree to clean up
	}

	// Force remove worktree (discards all changes)
	gitCtx, gitSpan := m.tracer.StartGitOp(ctx, "worktree_remove")
	start := time.Now()
	err := m.git.RemoveWorktree(gitCtx, tx.WorktreePath, true)
	recordGitOp(ctx, "worktree_remove", time.Since(start), err)
	m.tracer.EndGitOp(gitSpan, err)
	if err != nil {
		// If git removal fails, try direct filesystem removal
		m.logger.Warn("git worktree remove failed, trying filesystem removal",
			"path", tx.WorktreePath,
			"error", err)
		if err := os.RemoveAll(tx.WorktreePath); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}
	}

	return nil
}
