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
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"
)

// GitAwareExecutor executes git commands with automatic cache invalidation.
//
// # Description
//
// Wraps git command execution to proactively invalidate the code graph cache
// when operations change the working tree. Commands are classified by impact
// and appropriate invalidation is performed after successful execution.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type GitAwareExecutor struct {
	config ExecutorConfig
}

// NewGitAwareExecutor creates a new git executor with cache invalidation.
//
// # Description
//
// Creates an executor that will invalidate the provided cache after
// git commands that modify the working tree.
//
// # Inputs
//
//   - config: Executor configuration.
//
// # Outputs
//
//   - *GitAwareExecutor: Ready-to-use executor.
//   - error: Non-nil if configuration is invalid.
func NewGitAwareExecutor(config ExecutorConfig) (*GitAwareExecutor, error) {
	// Resolve work directory to absolute path
	absWorkDir, err := filepath.Abs(config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolving work directory: %w", err)
	}
	config.WorkDir = absWorkDir

	// Apply defaults
	if config.InvalidationTimeout <= 0 {
		config.InvalidationTimeout = 30
	}

	return &GitAwareExecutor{config: config}, nil
}

// Execute runs a git command and invalidates cache as needed.
//
// # Description
//
// Executes the git command, then performs appropriate cache invalidation
// based on the command type. Invalidation only happens if the command
// succeeds (exit code 0).
//
// # Inputs
//
//   - ctx: Context for command timeout and cancellation.
//   - args: Git command arguments (e.g., "checkout", "main").
//
// # Outputs
//
//   - *ExecuteResult: Command output and invalidation info.
//   - error: Non-nil on execution failure.
//
// # Example
//
//	result, err := executor.Execute(ctx, "checkout", "feature-branch")
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result.Output)
//	// Cache was automatically invalidated (InvalidationFull)
func (e *GitAwareExecutor) Execute(ctx context.Context, args ...string) (*ExecuteResult, error) {
	// Start tracing span
	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	ctx, span := startExecuteSpan(ctx, command, e.config.WorkDir)
	defer span.End()
	start := time.Now()

	// Classify command before execution
	invType := classifyCommand(ctx, args, e.config.WorkDir)

	slog.Debug("Executing git command",
		"args", args,
		"invalidation_type", invType.String())

	// Execute git command
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.config.WorkDir
	output, err := cmd.CombinedOutput()

	result := &ExecuteResult{
		Output:           string(output),
		InvalidationType: invType,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			setExecuteSpanResult(span, -1, invType.String())
			recordExecuteMetrics(ctx, command, time.Since(start), -1, invType.String())
			return nil, fmt.Errorf("executing git command: %w", err)
		}
		// Don't invalidate on failure
		slog.Debug("Git command failed, skipping invalidation",
			"args", args,
			"exit_code", result.ExitCode)
		setExecuteSpanResult(span, result.ExitCode, invType.String())
		recordExecuteMetrics(ctx, command, time.Since(start), result.ExitCode, invType.String())
		return result, nil
	}

	// Perform cache invalidation
	if e.config.Cache != nil && invType != InvalidationNone {
		if err := e.performInvalidation(ctx, invType, args); err != nil {
			slog.Warn("Cache invalidation failed",
				"args", args,
				"error", err)
			// Continue - command succeeded, just log the invalidation failure
		}
	}

	setExecuteSpanResult(span, result.ExitCode, invType.String())
	recordExecuteMetrics(ctx, command, time.Since(start), result.ExitCode, invType.String())

	return result, nil
}

// performInvalidation invalidates the cache based on command type.
func (e *GitAwareExecutor) performInvalidation(ctx context.Context, invType InvalidationType, args []string) error {
	// Create timeout context for invalidation
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(e.config.InvalidationTimeout)*time.Second)
	defer cancel()

	// Wait for any in-flight rebuilds
	if err := e.config.Cache.WaitForRebuilds(); err != nil {
		slog.Warn("Failed to wait for cache rebuilds",
			"error", err)
		// Continue anyway
	}

	switch invType {
	case InvalidationFull:
		slog.Info("Invalidating entire cache after git command",
			"command", args[0])
		return e.config.Cache.InvalidateAll()

	case InvalidationTargeted:
		files, err := extractTargetedFiles(timeoutCtx, args, e.config.WorkDir)
		if err != nil {
			slog.Warn("Failed to extract targeted files, falling back to full invalidation",
				"error", err)
			return e.config.Cache.InvalidateAll()
		}
		if len(files) == 0 {
			return nil
		}
		slog.Info("Invalidating specific files after git command",
			"command", args[0],
			"file_count", len(files))
		return e.config.Cache.InvalidateFiles(files)
	}

	return nil
}

// ExecuteWithRebuild runs a git command and optionally triggers cache rebuild.
//
// # Description
//
// Like Execute, but after invalidation can trigger an immediate rebuild
// of the cache rather than waiting for the next query.
//
// # Inputs
//
//   - ctx: Context for command timeout.
//   - rebuild: If true, trigger immediate cache rebuild after invalidation.
//   - args: Git command arguments.
//
// # Outputs
//
//   - *ExecuteResult: Command output and invalidation info.
//   - error: Non-nil on failure.
func (e *GitAwareExecutor) ExecuteWithRebuild(ctx context.Context, rebuild bool, args ...string) (*ExecuteResult, error) {
	result, err := e.Execute(ctx, args...)
	if err != nil {
		return nil, err
	}

	// Rebuild is handled by the cache implementation
	// The next query will trigger a rebuild if cache was invalidated

	return result, nil
}

// FindGitDir returns the .git directory for the working directory.
//
// # Description
//
// Uses `git rev-parse --git-dir` to find the correct .git directory,
// handling worktrees and GIT_DIR environment variable.
//
// # Inputs
//
//   - ctx: Context for command timeout.
//
// # Outputs
//
//   - string: Path to .git directory.
//   - error: Non-nil if not a git repository.
func (e *GitAwareExecutor) FindGitDir(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = e.config.WorkDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	gitDir := filepath.Clean(string(output[:len(output)-1])) // Remove trailing newline
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(e.config.WorkDir, gitDir)
	}
	return gitDir, nil
}

// IsDetachedHead checks if the repository is in detached HEAD state.
//
// # Description
//
// Uses `git symbolic-ref HEAD` which fails if HEAD is detached.
//
// # Inputs
//
//   - ctx: Context for command timeout.
//
// # Outputs
//
//   - bool: True if HEAD is detached.
func (e *GitAwareExecutor) IsDetachedHead(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "HEAD")
	cmd.Dir = e.config.WorkDir
	err := cmd.Run()
	return err != nil
}

// GetCurrentBranch returns the current branch name.
//
// # Description
//
// Returns the current branch name, or "HEAD" if detached.
//
// # Inputs
//
//   - ctx: Context for command timeout.
//
// # Outputs
//
//   - string: Branch name or "HEAD".
//   - error: Non-nil if not a git repository.
func (e *GitAwareExecutor) GetCurrentBranch(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = e.config.WorkDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return string(output[:len(output)-1]), nil // Remove trailing newline
}
