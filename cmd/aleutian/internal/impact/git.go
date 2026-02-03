// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package impact

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitClient handles git operations for change detection.
//
// # Thread Safety
//
// GitClient is safe for concurrent use.
type GitClient struct {
	workDir string
}

// NewGitClient creates a new GitClient for the given working directory.
//
// # Inputs
//
//   - workDir: Working directory (project root). Must not be empty.
//
// # Outputs
//
//   - *GitClient: The git client instance.
func NewGitClient(workDir string) *GitClient {
	return &GitClient{workDir: workDir}
}

// IsGitRepo checks if the working directory is a git repository.
//
// # Outputs
//
//   - bool: True if the directory is a git repository.
func (g *GitClient) IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = g.workDir
	return cmd.Run() == nil
}

// GetChangedFiles returns the list of changed files based on the mode.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - cfg: Configuration specifying change mode.
//
// # Outputs
//
//   - []ChangedFile: List of changed files.
//   - error: Non-nil if git operation fails.
func (g *GitClient) GetChangedFiles(ctx context.Context, cfg Config) ([]ChangedFile, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	switch cfg.Mode {
	case ChangeModeFiles:
		return g.filesFromList(cfg.Files)
	case ChangeModeDiff:
		return g.runGitDiff(ctx, []string{"diff", "--name-status"})
	case ChangeModeStaged:
		return g.runGitDiff(ctx, []string{"diff", "--cached", "--name-status"})
	case ChangeModeCommit:
		if cfg.CommitHash == "" {
			return nil, fmt.Errorf("commit hash required for commit mode")
		}
		return g.runGitDiff(ctx, []string{"show", "--name-status", "--format=", cfg.CommitHash})
	case ChangeModeBranch:
		if cfg.BaseBranch == "" {
			return nil, fmt.Errorf("base branch required for branch mode")
		}
		// Verify branch exists
		if err := g.verifyBranch(ctx, cfg.BaseBranch); err != nil {
			return nil, err
		}
		return g.runGitDiff(ctx, []string{"diff", "--name-status", cfg.BaseBranch + "...HEAD"})
	default:
		return nil, fmt.Errorf("unknown change mode: %s", cfg.Mode)
	}
}

// filesFromList creates ChangedFile entries from an explicit file list.
func (g *GitClient) filesFromList(files []string) ([]ChangedFile, error) {
	result := make([]ChangedFile, 0, len(files))
	for _, f := range files {
		result = append(result, ChangedFile{
			Path:       f,
			ChangeType: ChangeModified, // Assume modified for explicit files
		})
	}
	return result, nil
}

// runGitDiff executes a git diff command and parses the output.
func (g *GitClient) runGitDiff(ctx context.Context, args []string) ([]ChangedFile, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}

	return g.parseNameStatus(stdout.String())
}

// parseNameStatus parses git diff --name-status output.
// Format: M\tpath/to/file.go
//
//	R\told/path.go\tnew/path.go
func (g *GitClient) parseNameStatus(output string) ([]ChangedFile, error) {
	var result []ChangedFile

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		path := parts[1]

		cf := ChangedFile{
			Path: filepath.ToSlash(path),
		}

		// Parse status (first character for rename scores like R100)
		switch {
		case strings.HasPrefix(status, "A"):
			cf.ChangeType = ChangeAdded
		case strings.HasPrefix(status, "M"):
			cf.ChangeType = ChangeModified
		case strings.HasPrefix(status, "D"):
			cf.ChangeType = ChangeDeleted
		case strings.HasPrefix(status, "R"):
			cf.ChangeType = ChangeRenamed
			if len(parts) >= 3 {
				cf.OldPath = filepath.ToSlash(parts[1])
				cf.Path = filepath.ToSlash(parts[2])
			}
		case strings.HasPrefix(status, "C"):
			cf.ChangeType = ChangeCopied
			if len(parts) >= 3 {
				cf.OldPath = filepath.ToSlash(parts[1])
				cf.Path = filepath.ToSlash(parts[2])
			}
		default:
			cf.ChangeType = ChangeModified
		}

		result = append(result, cf)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parsing git output: %w", err)
	}

	return result, nil
}

// verifyBranch checks if a branch exists.
func (g *GitClient) verifyBranch(ctx context.Context, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branch)
	cmd.Dir = g.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("branch %q not found: %w: %s", branch, err, stderr.String())
	}
	return nil
}

// GetMergeBase returns the merge base between HEAD and a branch.
func (g *GitClient) GetMergeBase(ctx context.Context, branch string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", branch, "HEAD")
	cmd.Dir = g.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("getting merge base: %w: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}
