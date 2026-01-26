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
	"os/exec"
	"strings"
)

// fullInvalidationCommands lists git subcommands that require full cache invalidation.
// These commands can change many files atomically.
var fullInvalidationCommands = map[string]bool{
	"checkout":    true, // git checkout <branch>
	"switch":      true, // git switch <branch>
	"merge":       true, // git merge <branch>
	"rebase":      true, // git rebase <branch>
	"pull":        true, // git pull
	"reset":       true, // git reset --hard (checked separately for --hard/--merge)
	"stash":       true, // git stash pop/apply
	"cherry-pick": true, // git cherry-pick <commit>
	"revert":      true, // git revert <commit>
	"am":          true, // git am (apply patches)
}

// targetedInvalidationCommands lists git subcommands that affect specific files.
var targetedInvalidationCommands = map[string]bool{
	"add":     true, // git add <files>
	"restore": true, // git restore <files>
	"rm":      true, // git rm <files>
}

// noInvalidationCommands lists read-only git commands that don't affect cache.
var noInvalidationCommands = map[string]bool{
	"status":    true,
	"diff":      true,
	"log":       true,
	"show":      true,
	"branch":    true, // listing branches
	"remote":    true,
	"fetch":     true, // doesn't change working tree
	"tag":       true, // listing tags
	"describe":  true,
	"rev-parse": true,
	"ls-files":  true,
	"ls-tree":   true,
	"cat-file":  true,
	"blame":     true,
	"shortlog":  true,
	"reflog":    true,
	"config":    true,
	"help":      true,
	"version":   true,
}

// classifyCommand determines the invalidation type for a git command.
//
// # Description
//
// Analyzes the git subcommand and arguments to determine how much
// of the cache should be invalidated after execution.
//
// # Inputs
//
//   - ctx: Context for alias resolution (may call git config).
//   - args: Git command arguments (e.g., ["checkout", "main"]).
//   - workDir: Working directory for git commands.
//
// # Outputs
//
//   - InvalidationType: The type of invalidation needed.
//
// # Examples
//
//   - ["checkout", "main"] -> InvalidationFull
//   - ["add", "file.go"] -> InvalidationTargeted
//   - ["status"] -> InvalidationNone
func classifyCommand(ctx context.Context, args []string, workDir string) InvalidationType {
	if len(args) == 0 {
		return InvalidationNone
	}

	// Resolve alias
	subcmd := resolveAlias(ctx, args[0], workDir)

	// Check no-invalidation commands first (most common)
	if noInvalidationCommands[subcmd] {
		return InvalidationNone
	}

	// Check full invalidation commands
	if fullInvalidationCommands[subcmd] {
		// Special case: git reset without --hard or --merge is targeted
		if subcmd == "reset" && !hasFlag(args, "--hard") && !hasFlag(args, "--merge") {
			return InvalidationTargeted
		}
		// Special case: git checkout -- <files> is targeted
		if subcmd == "checkout" && hasDoubleDash(args) {
			return InvalidationTargeted
		}
		// Special case: git stash list/show is read-only
		if subcmd == "stash" && len(args) > 1 {
			stashOp := args[1]
			if stashOp == "list" || stashOp == "show" {
				return InvalidationNone
			}
		}
		return InvalidationFull
	}

	// Check targeted invalidation commands
	if targetedInvalidationCommands[subcmd] {
		return InvalidationTargeted
	}

	// Unknown command - assume targeted to be safe
	return InvalidationTargeted
}

// resolveAlias resolves a git alias to its underlying command.
//
// # Description
//
// Queries git config for alias definitions. For example, if the user
// has `alias.co = checkout`, this resolves "co" to "checkout".
//
// # Inputs
//
//   - ctx: Context for command timeout.
//   - subcmd: Potential alias (e.g., "co").
//   - workDir: Working directory for git commands.
//
// # Outputs
//
//   - string: The resolved command (or original if not an alias).
func resolveAlias(ctx context.Context, subcmd string, workDir string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "alias."+subcmd)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		// Not an alias, return original
		return subcmd
	}

	// Parse first word of alias (e.g., "checkout -b" -> "checkout")
	alias := strings.TrimSpace(string(output))
	if alias == "" {
		return subcmd
	}

	fields := strings.Fields(alias)
	if len(fields) == 0 {
		return subcmd
	}

	// Handle shell aliases (starting with !)
	if strings.HasPrefix(fields[0], "!") {
		// Shell alias - can't easily determine impact, assume full
		return "!shell"
	}

	return fields[0]
}

// extractTargetedFiles extracts file paths from a targeted git command.
//
// # Description
//
// For commands like `git add file1.go file2.go`, extracts the file paths.
// Uses git's own resolution for complex pathspecs (globs, negations).
//
// # Inputs
//
//   - ctx: Context for command timeout.
//   - args: Git command arguments.
//   - workDir: Working directory for git commands.
//
// # Outputs
//
//   - []string: List of affected file paths.
//   - error: Non-nil if git command fails.
func extractTargetedFiles(ctx context.Context, args []string, workDir string) ([]string, error) {
	if len(args) < 2 {
		return nil, nil
	}

	subcmd := args[0]

	switch subcmd {
	case "add":
		// Use git diff-index to see what would be staged
		return getAddedFiles(ctx, args[1:], workDir)
	case "restore":
		// Files being restored
		return getFileArgs(args[1:]), nil
	case "rm":
		// Files being removed
		return getFileArgs(args[1:]), nil
	case "checkout":
		// git checkout -- <files>
		if idx := findDoubleDash(args); idx != -1 && idx+1 < len(args) {
			return getFileArgs(args[idx+1:]), nil
		}
	case "reset":
		// git reset <files> (without --hard)
		return getFileArgs(args[1:]), nil
	}

	return nil, nil
}

// getAddedFiles uses git to resolve pathspecs for `git add`.
func getAddedFiles(ctx context.Context, pathspecs []string, workDir string) ([]string, error) {
	// Use git ls-files to resolve pathspecs
	cmdArgs := append([]string{"ls-files", "--modified", "--others", "--exclude-standard", "--"}, pathspecs...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		// Fallback to just the pathspecs
		return getFileArgs(pathspecs), nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// getFileArgs extracts file paths from args, filtering out flags.
func getFileArgs(args []string) []string {
	var files []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") && arg != "--" {
			files = append(files, arg)
		}
	}
	return files
}

// hasFlag checks if args contain a specific flag.
func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// hasDoubleDash checks if args contain "--".
func hasDoubleDash(args []string) bool {
	return findDoubleDash(args) != -1
}

// findDoubleDash returns the index of "--" or -1 if not found.
func findDoubleDash(args []string) int {
	for i, arg := range args {
		if arg == "--" {
			return i
		}
	}
	return -1
}
