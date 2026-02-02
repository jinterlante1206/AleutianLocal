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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultGitClient implements GitClient using the git command line.
//
// # Description
//
// Executes git commands with configurable timeout and working directory.
// All operations are performed in the configured repository path.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type DefaultGitClient struct {
	repoPath string
	timeout  time.Duration
}

// NewGitClient creates a new git client for the specified repository.
//
// # Description
//
// Creates a client that executes git commands in the given directory.
//
// # Inputs
//
//   - repoPath: Absolute path to the git repository.
//   - timeout: Maximum duration for each git operation.
//
// # Outputs
//
//   - *DefaultGitClient: Ready-to-use git client.
//   - error: Non-nil if repoPath is not absolute.
func NewGitClient(repoPath string, timeout time.Duration) (*DefaultGitClient, error) {
	if !filepath.IsAbs(repoPath) {
		return nil, fmt.Errorf("repoPath must be absolute: %s", repoPath)
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &DefaultGitClient{
		repoPath: repoPath,
		timeout:  timeout,
	}, nil
}

// run executes a git command and returns stdout.
func (g *DefaultGitClient) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s: timeout after %v", args[0], g.timeout)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// runSilent executes a git command and returns only success/failure.
func (g *DefaultGitClient) runSilent(ctx context.Context, args ...string) error {
	_, err := g.run(ctx, args...)
	return err
}

// IsGitRepository checks if the path is a git repository.
//
// # Description
//
// Uses `git rev-parse --git-dir` to determine if inside a git repository.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if the path is inside a git repository.
func (g *DefaultGitClient) IsGitRepository(ctx context.Context) bool {
	err := g.runSilent(ctx, "rev-parse", "--git-dir")
	return err == nil
}

// HasRebaseInProgress checks if a rebase is in progress.
//
// # Description
//
// Checks for .git/rebase-merge or .git/rebase-apply directories.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if a rebase is in progress.
func (g *DefaultGitClient) HasRebaseInProgress(ctx context.Context) bool {
	gitDir, err := g.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	// Check for rebase directories
	rebaseMerge := filepath.Join(g.repoPath, gitDir, "rebase-merge")
	rebaseApply := filepath.Join(g.repoPath, gitDir, "rebase-apply")

	if _, err := os.Stat(rebaseMerge); err == nil {
		return true
	}
	if _, err := os.Stat(rebaseApply); err == nil {
		return true
	}
	return false
}

// HasMergeInProgress checks if a merge is in progress.
//
// # Description
//
// Checks for .git/MERGE_HEAD file.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if a merge is in progress.
func (g *DefaultGitClient) HasMergeInProgress(ctx context.Context) bool {
	gitDir, err := g.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	mergeHead := filepath.Join(g.repoPath, gitDir, "MERGE_HEAD")
	_, err = os.Stat(mergeHead)
	return err == nil
}

// GetCurrentBranch returns the current branch name.
//
// # Description
//
// Returns the current branch name, or "HEAD" if in detached HEAD state.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - string: Branch name or "HEAD".
//   - error: Non-nil if not a git repository.
func (g *DefaultGitClient) GetCurrentBranch(ctx context.Context) (string, error) {
	branch, err := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return branch, nil
}

// RevParse resolves a git ref to a commit SHA.
//
// # Description
//
// Resolves references like HEAD, branch names, or tags to full SHA.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - ref: Git reference to resolve.
//
// # Outputs
//
//   - string: Full commit SHA.
//   - error: Non-nil if ref doesn't exist.
func (g *DefaultGitClient) RevParse(ctx context.Context, ref string) (string, error) {
	sha, err := g.run(ctx, "rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("resolving ref %s: %w", ref, err)
	}
	return sha, nil
}

// RefExists checks if a git ref exists.
//
// # Description
//
// Uses `git show-ref` to check if the ref exists.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - ref: Git reference to check.
//
// # Outputs
//
//   - bool: True if the ref exists.
func (g *DefaultGitClient) RefExists(ctx context.Context, ref string) bool {
	err := g.runSilent(ctx, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// StashPush creates a new stash with the given message.
//
// # Description
//
// Stashes all tracked changes with the specified message.
// Includes untracked files.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - message: Descriptive message for the stash.
//
// # Outputs
//
//   - error: Non-nil if stash fails.
func (g *DefaultGitClient) StashPush(ctx context.Context, message string) error {
	return g.runSilent(ctx, "stash", "push", "-u", "-m", message)
}

// StashPop applies and removes the top stash.
//
// # Description
//
// Applies the most recent stash and removes it from the stash list.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - error: Non-nil if pop fails (e.g., conflicts).
func (g *DefaultGitClient) StashPop(ctx context.Context) error {
	return g.runSilent(ctx, "stash", "pop")
}

// StashDrop removes a stash entry.
//
// # Description
//
// Removes the specified stash from the stash list.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - ref: Stash reference (e.g., "stash@{0}").
//
// # Outputs
//
//   - error: Non-nil if drop fails.
func (g *DefaultGitClient) StashDrop(ctx context.Context, ref string) error {
	return g.runSilent(ctx, "stash", "drop", ref)
}

// StashList returns all stash entries.
//
// # Description
//
// Parses the stash list into structured entries.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - []StashEntry: List of stash entries.
//   - error: Non-nil if listing fails.
func (g *DefaultGitClient) StashList(ctx context.Context) ([]StashEntry, error) {
	output, err := g.run(ctx, "stash", "list")
	if err != nil {
		return nil, fmt.Errorf("listing stashes: %w", err)
	}

	if output == "" {
		return nil, nil
	}

	var entries []StashEntry
	lines := strings.Split(output, "\n")

	// Parse lines like: stash@{0}: On main: message here
	pattern := regexp.MustCompile(`^(stash@\{(\d+)\}): .*?: (.*)$`)

	for _, line := range lines {
		matches := pattern.FindStringSubmatch(line)
		if len(matches) == 4 {
			var index int
			fmt.Sscanf(matches[2], "%d", &index)
			entries = append(entries, StashEntry{
				Index:   index,
				Ref:     matches[1],
				Message: matches[3],
			})
		}
	}

	return entries, nil
}

// CreateBranch creates a new branch at the current HEAD.
//
// # Description
//
// Creates a branch without switching to it.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - name: Branch name to create.
//
// # Outputs
//
//   - error: Non-nil if branch already exists or creation fails.
func (g *DefaultGitClient) CreateBranch(ctx context.Context, name string) error {
	return g.runSilent(ctx, "branch", name)
}

// DeleteBranch deletes a branch.
//
// # Description
//
// Deletes the specified branch. Use force=true for unmerged branches.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - name: Branch name to delete.
//   - force: If true, force delete even if unmerged.
//
// # Outputs
//
//   - error: Non-nil if deletion fails.
func (g *DefaultGitClient) DeleteBranch(ctx context.Context, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return g.runSilent(ctx, "branch", flag, name)
}

// Checkout switches to the specified ref.
//
// # Description
//
// Checks out a branch, tag, or commit SHA.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - ref: Branch name, tag, or commit SHA.
//
// # Outputs
//
//   - error: Non-nil if checkout fails.
func (g *DefaultGitClient) Checkout(ctx context.Context, ref string) error {
	return g.runSilent(ctx, "checkout", ref)
}

// BranchExists checks if a branch exists.
//
// # Description
//
// Uses `git show-ref` to check for branch existence.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - name: Branch name to check.
//
// # Outputs
//
//   - bool: True if the branch exists.
func (g *DefaultGitClient) BranchExists(ctx context.Context, name string) bool {
	ref := "refs/heads/" + name
	return g.RefExists(ctx, ref)
}

// ResetHard performs a hard reset to the specified ref.
//
// # Description
//
// Resets the working tree and index to match the specified commit.
// All uncommitted changes are discarded.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - ref: Commit SHA or ref to reset to.
//
// # Outputs
//
//   - error: Non-nil if reset fails.
func (g *DefaultGitClient) ResetHard(ctx context.Context, ref string) error {
	return g.runSilent(ctx, "reset", "--hard", ref)
}

// CleanUntracked removes untracked files and directories.
//
// # Description
//
// Removes all untracked files and directories from the working tree.
// Does not remove ignored files.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - error: Non-nil if clean fails.
func (g *DefaultGitClient) CleanUntracked(ctx context.Context) error {
	return g.runSilent(ctx, "clean", "-fd")
}

// Add stages files for commit.
//
// # Description
//
// Stages the specified files for the next commit.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - paths: File paths to stage.
//
// # Outputs
//
//   - error: Non-nil if staging fails.
func (g *DefaultGitClient) Add(ctx context.Context, paths ...string) error {
	args := append([]string{"add"}, paths...)
	return g.runSilent(ctx, args...)
}

// AddAll stages all changes for commit.
//
// # Description
//
// Stages all tracked and untracked changes.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - error: Non-nil if staging fails.
func (g *DefaultGitClient) AddAll(ctx context.Context) error {
	return g.runSilent(ctx, "add", "-A")
}

// Commit creates a new commit with the staged changes.
//
// # Description
//
// Creates a commit with the specified message. Fails if nothing is staged.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - message: Commit message.
//
// # Outputs
//
//   - error: Non-nil if commit fails.
func (g *DefaultGitClient) Commit(ctx context.Context, message string) error {
	return g.runSilent(ctx, "commit", "-m", message)
}

// HasStagedChanges checks if there are staged changes.
//
// # Description
//
// Uses `git diff --cached` to check for staged changes.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if there are staged changes.
func (g *DefaultGitClient) HasStagedChanges(ctx context.Context) bool {
	output, err := g.run(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return false
	}
	return output != ""
}

// HasUnstagedChanges checks if there are unstaged changes.
//
// # Description
//
// Uses `git diff` to check for unstaged changes.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if there are unstaged changes.
func (g *DefaultGitClient) HasUnstagedChanges(ctx context.Context) bool {
	output, err := g.run(ctx, "diff", "--name-only")
	if err != nil {
		return false
	}
	return output != ""
}

// Status returns the current git status.
//
// # Description
//
// Parses `git status --porcelain` into a structured GitStatus.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - *GitStatus: Current repository status.
//   - error: Non-nil if status fails.
func (g *DefaultGitClient) Status(ctx context.Context) (*GitStatus, error) {
	branch, err := g.GetCurrentBranch(ctx)
	if err != nil {
		return nil, err
	}

	output, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("getting status: %w", err)
	}

	status := &GitStatus{
		Branch:  branch,
		IsClean: output == "",
	}

	if output == "" {
		return status, nil
	}

	// Parse porcelain output
	// XY filename
	// X = index status, Y = worktree status
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 3 {
			continue
		}

		x := line[0]
		y := line[1]
		file := strings.TrimSpace(line[3:])

		// Staged changes (index)
		if x != ' ' && x != '?' {
			status.StagedFiles = append(status.StagedFiles, file)
		}

		// Unstaged changes (worktree)
		if y != ' ' && y != '?' {
			status.ModifiedFiles = append(status.ModifiedFiles, file)
		}

		// Untracked files
		if x == '?' && y == '?' {
			status.UntrackedFiles = append(status.UntrackedFiles, file)
		}
	}

	return status, nil
}

// =============================================================================
// Worktree Operations
// =============================================================================

// CreateWorktree creates a new git worktree at the specified path.
//
// # Description
//
// Creates a detached worktree at the given path, checked out to the specified ref.
// The worktree is independent from the main working directory.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - path: Absolute path for the new worktree.
//   - ref: Git ref to checkout (branch, tag, or commit SHA).
//
// # Outputs
//
//   - error: Non-nil if worktree creation fails.
func (g *DefaultGitClient) CreateWorktree(ctx context.Context, path string, ref string) error {
	return g.runSilent(ctx, "worktree", "add", "--detach", path, ref)
}

// RemoveWorktree removes a git worktree.
//
// # Description
//
// Removes the worktree at the specified path. The worktree directory is deleted.
// Use force=true to remove even if there are uncommitted changes.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - path: Absolute path to the worktree to remove.
//   - force: If true, force removal even with uncommitted changes.
//
// # Outputs
//
//   - error: Non-nil if removal fails.
func (g *DefaultGitClient) RemoveWorktree(ctx context.Context, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	return g.runSilent(ctx, args...)
}

// WorktreeList returns all worktrees in the repository.
//
// # Description
//
// Parses the output of `git worktree list --porcelain` into structured entries.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - []WorktreeEntry: List of all worktrees including the main one.
//   - error: Non-nil if listing fails.
func (g *DefaultGitClient) WorktreeList(ctx context.Context) ([]WorktreeEntry, error) {
	output, err := g.run(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	return parseWorktreeList(output), nil
}

// IsDetachedHead checks if the repository is in detached HEAD state.
//
// # Description
//
// Returns true if HEAD is not pointing to a branch (detached HEAD state).
// This is detected when `git rev-parse --abbrev-ref HEAD` returns "HEAD".
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if in detached HEAD state.
func (g *DefaultGitClient) IsDetachedHead(ctx context.Context) bool {
	branch, err := g.GetCurrentBranch(ctx)
	if err != nil {
		return false
	}
	return branch == "HEAD"
}

// HasCherryPickInProgress checks if a cherry-pick is in progress.
//
// # Description
//
// Checks for .git/CHERRY_PICK_HEAD file which indicates an incomplete cherry-pick.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if a cherry-pick is in progress.
func (g *DefaultGitClient) HasCherryPickInProgress(ctx context.Context) bool {
	gitDir, err := g.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	cherryPickHead := filepath.Join(g.repoPath, gitDir, "CHERRY_PICK_HEAD")
	_, err = os.Stat(cherryPickHead)
	return err == nil
}

// HasBisectInProgress checks if a git bisect is in progress.
//
// # Description
//
// Checks for .git/BISECT_LOG file which indicates an active bisect session.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - bool: True if a bisect is in progress.
func (g *DefaultGitClient) HasBisectInProgress(ctx context.Context) bool {
	gitDir, err := g.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	bisectLog := filepath.Join(g.repoPath, gitDir, "BISECT_LOG")
	_, err = os.Stat(bisectLog)
	return err == nil
}

// parseWorktreeList parses git worktree list --porcelain output.
//
// Format:
//
//	worktree /path/to/main
//	HEAD abc123def456
//	branch refs/heads/main
//
//	worktree /path/to/worktree
//	HEAD def456abc123
//	detached
//	locked
func parseWorktreeList(output string) []WorktreeEntry {
	if output == "" {
		return nil
	}

	var entries []WorktreeEntry
	var current *WorktreeEntry

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if line == "" {
			if current != nil {
				entries = append(entries, *current)
				current = nil
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current = &WorktreeEntry{
				Path: strings.TrimPrefix(line, "worktree "),
			}
		} else if strings.HasPrefix(line, "HEAD ") && current != nil {
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") && current != nil {
			// Extract branch name from refs/heads/branchname
			branch := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		} else if line == "locked" && current != nil {
			current.Locked = true
		}
		// "detached" is implicit when no branch is set
	}

	// Handle last entry if output doesn't end with blank line
	if current != nil {
		entries = append(entries, *current)
	}

	return entries
}
