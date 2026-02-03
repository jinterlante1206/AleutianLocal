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
	"fmt"
	"log/slog"
	"strings"
)

// PreFlightConfig configures pre-flight check behavior.
//
// # Description
//
// Controls how the pre-flight guard validates repository state before
// allowing agent operations to proceed.
type PreFlightConfig struct {
	// Force skips the dirty working tree check (dangerous).
	// Use only when you're certain the agent won't conflict with user changes.
	Force bool

	// AutoStash automatically stashes user changes before operations
	// and restores them after. Conflicts may still occur on restore.
	AutoStash bool

	// AllowDetached permits operations in detached HEAD state.
	// Useful for CI/CD pipelines that checkout specific commits.
	AllowDetached bool
}

// PreFlightResult contains the outcomes of pre-flight validation.
//
// # Description
//
// Aggregates all errors and warnings from the pre-flight checks.
// If Passed is false, at least one error is present and the operation
// should not proceed.
type PreFlightResult struct {
	// Passed is true if all checks passed (no fatal errors).
	Passed bool

	// Errors are fatal issues that block execution.
	Errors []PreFlightError

	// Warnings are non-fatal issues the user should be aware of.
	Warnings []PreFlightWarning

	// StashRef is set if AutoStash was used, containing the stash reference.
	// The caller must call Cleanup() with this reference after operations complete.
	StashRef string
}

// FirstError returns the first error, or nil if no errors.
//
// # Description
//
// Convenience method for returning a single error from the result.
func (r *PreFlightResult) FirstError() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return &r.Errors[0]
}

// FormatErrors returns a human-readable multi-line error summary.
//
// # Description
//
// Formats all errors with their details for display to the user.
func (r *PreFlightResult) FormatErrors() string {
	if len(r.Errors) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Pre-flight check failed:\n\n")

	for i, err := range r.Errors {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s\n", err.Code, err.Message))
		for _, detail := range err.Details {
			sb.WriteString(fmt.Sprintf("    %s\n", detail))
		}
	}

	return sb.String()
}

// PreFlightError represents a fatal issue that blocks execution.
//
// # Description
//
// Contains structured error information for programmatic handling
// and human-readable messages for display.
type PreFlightError struct {
	// Code is a machine-readable error identifier.
	Code string

	// Message is a human-readable description of the error.
	Message string

	// Details contains additional information, such as affected files
	// or remediation steps.
	Details []string
}

// Error implements the error interface.
func (e *PreFlightError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// PreFlightWarning represents a non-fatal issue.
//
// # Description
//
// Warnings don't block execution but should be communicated to the user.
type PreFlightWarning struct {
	// Code is a machine-readable warning identifier.
	Code string

	// Message is a human-readable description of the warning.
	Message string
}

// PreFlightGuard performs repository state validation before agent operations.
//
// # Description
//
// Validates that the repository is in a safe state for agent operations.
// This prevents data loss from agent modifications overwriting user's
// uncommitted work.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type PreFlightGuard struct {
	git    GitClient
	config PreFlightConfig
	logger *slog.Logger
}

// NewPreFlightGuard creates a new pre-flight guard.
//
// # Description
//
// Creates a guard configured with the specified options.
//
// # Inputs
//
//   - git: Git client for repository operations. Must not be nil.
//   - config: Configuration options.
//   - logger: Logger for diagnostic output (can be nil for no logging).
//
// # Outputs
//
//   - *PreFlightGuard: Ready-to-use guard.
//
// # Panics
//
//   - Panics if git is nil.
func NewPreFlightGuard(git GitClient, config PreFlightConfig, logger *slog.Logger) *PreFlightGuard {
	if git == nil {
		panic("preflight: git client must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Validate config and log warning if invalid (don't fail - use default behavior)
	if err := ValidateConfig(config); err != nil {
		logger.Warn("invalid preflight config, using defaults",
			"error", err.Error())
		// If Force and AutoStash are both set, Force takes precedence
		if config.Force && config.AutoStash {
			config.AutoStash = false
		}
	}

	return &PreFlightGuard{
		git:    git,
		config: config,
		logger: logger.With("component", "preflight"),
	}
}

// Check performs all pre-flight validations.
//
// # Description
//
// Validates the repository state against all configured checks.
// Returns a result with Passed=true if the repository is safe for
// agent operations.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//
// # Outputs
//
//   - *PreFlightResult: Check outcomes including errors, warnings, and stash ref.
//   - error: Non-nil only if the check itself failed (not if checks didn't pass).
//
// # Example
//
//	guard := NewPreFlightGuard(git, config, logger)
//	result, err := guard.Check(ctx)
//	if err != nil {
//	    return fmt.Errorf("preflight check failed: %w", err)
//	}
//	if !result.Passed {
//	    return result.FirstError()
//	}
//	// Safe to proceed with agent operations
func (g *PreFlightGuard) Check(ctx context.Context) (*PreFlightResult, error) {
	result := &PreFlightResult{Passed: true}

	// Check 1: Is this a git repository?
	if !g.git.IsGitRepository(ctx) {
		result.Warnings = append(result.Warnings, PreFlightWarning{
			Code:    "NOT_GIT_REPO",
			Message: "Not a git repository. File rollback will not be available.",
		})
		// Not fatal - allow operations but warn
		return result, nil
	}

	// Check 2: Merge in progress (always fatal)
	if g.git.HasMergeInProgress(ctx) {
		result.Passed = false
		result.Errors = append(result.Errors, PreFlightError{
			Code:    "MERGE_IN_PROGRESS",
			Message: "A merge is in progress.",
			Details: []string{
				"Complete the merge: git commit",
				"Or abort the merge: git merge --abort",
			},
		})
	}

	// Check 3: Rebase in progress (always fatal)
	if g.git.HasRebaseInProgress(ctx) {
		result.Passed = false
		result.Errors = append(result.Errors, PreFlightError{
			Code:    "REBASE_IN_PROGRESS",
			Message: "A rebase is in progress.",
			Details: []string{
				"Continue the rebase: git rebase --continue",
				"Or abort the rebase: git rebase --abort",
			},
		})
	}

	// Check 4: Cherry-pick in progress (always fatal)
	if g.git.HasCherryPickInProgress(ctx) {
		result.Passed = false
		result.Errors = append(result.Errors, PreFlightError{
			Code:    "CHERRY_PICK_IN_PROGRESS",
			Message: "A cherry-pick is in progress.",
			Details: []string{
				"Continue the cherry-pick: git cherry-pick --continue",
				"Or abort the cherry-pick: git cherry-pick --abort",
			},
		})
	}

	// Check 5: Bisect in progress (always fatal)
	if g.git.HasBisectInProgress(ctx) {
		result.Passed = false
		result.Errors = append(result.Errors, PreFlightError{
			Code:    "BISECT_IN_PROGRESS",
			Message: "A git bisect is in progress.",
			Details: []string{
				"Complete the bisect to find the bad commit",
				"Or reset the bisect: git bisect reset",
			},
		})
	}

	// Check 6: Detached HEAD (fatal unless allowed)
	if g.git.IsDetachedHead(ctx) && !g.config.AllowDetached {
		result.Passed = false
		result.Errors = append(result.Errors, PreFlightError{
			Code:    "DETACHED_HEAD",
			Message: "Repository is in detached HEAD state.",
			Details: []string{
				"Attach to a branch: git checkout <branch>",
				"Create a new branch: git checkout -b <new-branch>",
				"Or allow detached: pass AllowDetached=true",
			},
		})
	}

	// Check 7: Dirty working tree
	status, err := g.git.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting git status: %w", err)
	}

	// Count dirty files (staged + modified, not untracked)
	dirtyCount := len(status.StagedFiles) + len(status.ModifiedFiles)

	if dirtyCount > 0 {
		if g.config.Force {
			// User explicitly chose to proceed despite dirty state
			result.Warnings = append(result.Warnings, PreFlightWarning{
				Code: "DIRTY_FORCED",
				Message: fmt.Sprintf("Proceeding with %d uncommitted changes (--force).",
					dirtyCount),
			})
			g.logger.Warn("proceeding with dirty working tree",
				"dirty_count", dirtyCount,
				"force", true)
		} else if g.config.AutoStash {
			// Auto-stash the changes
			stashRef, stashErr := g.autoStash(ctx)
			if stashErr != nil {
				result.Passed = false
				result.Errors = append(result.Errors, PreFlightError{
					Code:    "STASH_FAILED",
					Message: "Failed to auto-stash changes: " + stashErr.Error(),
				})
			} else {
				result.StashRef = stashRef
				g.logger.Info("auto-stashed user changes",
					"stash_ref", stashRef,
					"dirty_count", dirtyCount)
			}
		} else {
			// Block execution
			result.Passed = false

			details := make([]string, 0, dirtyCount+6)
			details = append(details, "Uncommitted changes:")
			for _, f := range status.StagedFiles {
				details = append(details, fmt.Sprintf("  staged:   %s", f))
			}
			for _, f := range status.ModifiedFiles {
				details = append(details, fmt.Sprintf("  modified: %s", f))
			}
			details = append(details, "")
			details = append(details, "Options:")
			details = append(details, "  1. Commit your changes: git commit -am 'WIP'")
			details = append(details, "  2. Stash your changes:  git stash")
			details = append(details, "  3. Force continue:      pass Force=true")
			details = append(details, "  4. Auto-stash:          pass AutoStash=true")

			result.Errors = append(result.Errors, PreFlightError{
				Code: "DIRTY_WORKING_TREE",
				Message: fmt.Sprintf("Repository has %d uncommitted changes.",
					dirtyCount),
				Details: details,
			})
		}
	}

	// Check 8: Untracked files (warning only)
	if len(status.UntrackedFiles) > 0 {
		result.Warnings = append(result.Warnings, PreFlightWarning{
			Code: "UNTRACKED_FILES",
			Message: fmt.Sprintf("%d untracked files present (agent won't modify these).",
				len(status.UntrackedFiles)),
		})
	}

	return result, nil
}

// stashPusher is a subset of GitClient that supports stash push.
// Used for auto-stash functionality.
type stashPusher interface {
	StashPush(ctx context.Context, message string) error
}

// stashPopper is a subset of GitClient that supports stash pop.
// Used for cleanup after auto-stash.
type stashPopper interface {
	StashPop(ctx context.Context) error
}

// autoStash stashes the current changes and returns the stash reference.
func (g *PreFlightGuard) autoStash(ctx context.Context) (string, error) {
	// Use a distinctive message for our stash
	message := "aleutian-preflight-autostash"

	// Type assert to stashPusher interface.
	// The GitClient interface includes StashPush, so this should always succeed
	// for properly implemented clients. The type assertion is defensive.
	s, ok := g.git.(stashPusher)
	if !ok {
		return "", fmt.Errorf("git client does not support stash operations")
	}

	if err := s.StashPush(ctx, message); err != nil {
		return "", fmt.Errorf("stashing changes: %w", err)
	}

	// Note: "stash@{0}" assumes this stash is now at the top.
	// In practice this is safe because Check() is synchronous and
	// we've validated the working tree state.
	return "stash@{0}", nil
}

// Cleanup restores stashed changes after agent operations complete.
//
// # Description
//
// If AutoStash was used during Check(), this method restores the
// stashed changes. Should be called regardless of whether agent
// operations succeeded or failed.
//
// # Inputs
//
//   - ctx: Context for timeout and cancellation.
//   - stashRef: The StashRef from PreFlightResult. If empty, does nothing.
//
// # Outputs
//
//   - error: Non-nil if restore failed. May indicate conflicts that
//     need manual resolution.
//
// # Example
//
//	defer func() {
//	    if err := guard.Cleanup(ctx, result.StashRef); err != nil {
//	        log.Warn("failed to restore stashed changes", "error", err)
//	    }
//	}()
func (g *PreFlightGuard) Cleanup(ctx context.Context, stashRef string) error {
	if stashRef == "" {
		return nil
	}

	// Type assert to stashPopper interface.
	// The GitClient interface includes StashPop, so this should always succeed.
	u, ok := g.git.(stashPopper)
	if !ok {
		return fmt.Errorf("git client does not support stash operations")
	}

	if err := u.StashPop(ctx); err != nil {
		return fmt.Errorf("restoring stashed changes (may have conflicts): %w", err)
	}

	g.logger.Info("restored auto-stashed changes")
	return nil
}

// ValidateConfig validates a PreFlightConfig for consistency.
//
// # Description
//
// Returns an error if the configuration is invalid or contradictory.
//
// # Inputs
//
//   - config: Configuration to validate.
//
// # Outputs
//
//   - error: Non-nil if configuration is invalid.
func ValidateConfig(config PreFlightConfig) error {
	// Force and AutoStash are mutually exclusive
	if config.Force && config.AutoStash {
		return fmt.Errorf("Force and AutoStash cannot both be true")
	}
	return nil
}
