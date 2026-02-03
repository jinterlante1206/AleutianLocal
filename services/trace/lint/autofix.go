// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// AUTO-FIX
// =============================================================================

// AutoFix runs the linter in fix mode on a file.
//
// Description:
//
//	Executes the linter with --fix flag to automatically fix issues.
//	The file is modified in place. Returns the list of remaining issues
//	that could not be fixed.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	filePath - Path to the file to fix
//
// Outputs:
//
//	*LintResult - Result after fixes applied (remaining issues)
//	error - Non-nil if the linter failed
//
// Example:
//
//	result, err := runner.AutoFix(ctx, "path/to/file.go")
//	if err != nil {
//	    return err
//	}
//	if result.HasErrors() {
//	    // Some issues couldn't be fixed automatically
//	}
//
// Thread Safety: Safe for concurrent use on different files.
// NOT safe to run on the same file concurrently.
func (r *LintRunner) AutoFix(ctx context.Context, filePath string) (*LintResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}
	if filePath == "" {
		return nil, fmt.Errorf("%w: filePath must not be empty", ErrInvalidInput)
	}

	// Detect language
	language := LanguageFromPath(filePath)
	if language == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, filepath.Ext(filePath))
	}

	return r.AutoFixWithLanguage(ctx, filePath, language)
}

// AutoFixWithLanguage runs the linter in fix mode for a specific language.
//
// Description:
//
//	Like AutoFix, but with explicit language specification.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	filePath - Path to the file to fix
//	language - The language identifier
//
// Outputs:
//
//	*LintResult - Result after fixes applied (remaining issues)
//	error - Non-nil if the linter failed
//
// Thread Safety: Safe for concurrent use on different files.
func (r *LintRunner) AutoFixWithLanguage(ctx context.Context, filePath, language string) (*LintResult, error) {
	// Get config
	config := r.configs.Get(language)
	if config == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Check if fix mode is supported
	if len(config.FixArgs) == 0 {
		return nil, fmt.Errorf("linter %s does not support auto-fix", config.Command)
	}

	// Check availability
	if !r.IsAvailable(language) {
		return nil, NewLinterError(config.Command, language, ErrLinterNotInstalled)
	}

	// Resolve file path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		if r.workingDir != "" {
			absPath = filepath.Join(r.workingDir, filePath)
		} else {
			var err error
			absPath, err = filepath.Abs(filePath)
			if err != nil {
				return nil, fmt.Errorf("resolving path: %w", err)
			}
		}
	}

	// Execute linter with fix args
	if _, err := r.executeLinterFix(ctx, config, absPath); err != nil {
		return nil, err
	}

	// Re-lint to get remaining issues
	return r.LintWithLanguage(ctx, filePath, language)
}

// AutoFixContent fixes content and returns both the fixed content and edit list.
//
// Description:
//
//	Writes content to a temp file, runs the linter with --fix,
//	then reads back the fixed content. Also returns a lint result
//	with any remaining issues.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	content - The source code to fix
//	language - The language identifier
//
// Outputs:
//
//	[]byte - The fixed content
//	*LintResult - Remaining issues after fixes
//	error - Non-nil if the linter failed
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) AutoFixContent(ctx context.Context, content []byte, language string) ([]byte, *LintResult, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}
	if len(content) == 0 {
		return content, &LintResult{
			Valid:           true,
			Errors:          make([]LintIssue, 0),
			Warnings:        make([]LintIssue, 0),
			Language:        language,
			LinterAvailable: r.IsAvailable(language),
		}, nil
	}

	// Get config
	config := r.configs.Get(language)
	if config == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Check if fix mode is supported
	if len(config.FixArgs) == 0 {
		return nil, nil, fmt.Errorf("linter %s does not support auto-fix", config.Command)
	}

	// Check availability
	if !r.IsAvailable(language) {
		return content, &LintResult{
			Valid:           true,
			Errors:          make([]LintIssue, 0),
			Warnings:        make([]LintIssue, 0),
			Language:        language,
			LinterAvailable: false,
		}, nil
	}

	// Get extension for temp file
	ext := ExtensionForLanguage(language)
	if ext == "" {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "lint-fix-*"+ext)
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write content
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return nil, nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	// Run linter with fix
	if _, err := r.executeLinterFix(ctx, config, tmpPath); err != nil {
		return nil, nil, err
	}

	// Read fixed content
	fixedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading fixed file: %w", err)
	}

	// Re-lint to get remaining issues
	result, err := r.LintContent(ctx, fixedContent, language)
	if err != nil {
		return fixedContent, nil, err
	}

	return fixedContent, result, nil
}

// executeLinterFix runs the linter in fix mode.
func (r *LintRunner) executeLinterFix(ctx context.Context, config *LinterConfig, filePath string) ([]byte, error) {
	// Build command with fix args
	args := make([]string, len(config.FixArgs))
	copy(args, config.FixArgs)
	args = append(args, filePath)

	// Create command with timeout
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, config.Command, args...)

	// Set working directory
	if r.workingDir != "" {
		cmd.Dir = r.workingDir
	} else {
		cmd.Dir = filepath.Dir(filePath)
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run
	err := cmd.Run()

	// Check for timeout
	if cmdCtx.Err() == context.DeadlineExceeded {
		return nil, NewLinterError(config.Command, config.Language, ErrLinterTimeout).
			WithOutput(stderr.String())
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Some linters exit with non-zero when they fix issues - that's OK
	if err != nil && stderr.Len() > 0 && stdout.Len() == 0 {
		return nil, NewLinterError(config.Command, config.Language, ErrLinterFailed).
			WithOutput(stderr.String())
	}

	return stdout.Bytes(), nil
}

// =============================================================================
// FEEDBACK GENERATION
// =============================================================================

// LintFeedback is agent-friendly feedback about lint issues.
//
// Description:
//
//	Provides structured feedback that an LLM agent can use to
//	understand what went wrong and how to fix it.
type LintFeedback struct {
	// Rejected is true if the patch should be rejected.
	Rejected bool `json:"rejected"`

	// Reason is a human-readable summary of why.
	Reason string `json:"reason"`

	// Issues are the individual problems found.
	Issues []FeedbackIssue `json:"issues"`

	// AutoFixable is the count of issues that can be auto-fixed.
	AutoFixable int `json:"auto_fixable"`

	// Action is suggested action for the agent.
	Action string `json:"action"`
}

// FeedbackIssue is a single issue in the feedback.
type FeedbackIssue struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Fix     string `json:"fix,omitempty"`
}

// FormatFeedback creates agent-friendly feedback from a lint result.
//
// Description:
//
//	Converts a LintResult into structured feedback suitable for
//	an LLM agent to understand and act upon.
//
// Inputs:
//
//	result - The lint result to format
//
// Outputs:
//
//	*LintFeedback - Structured feedback for the agent
func FormatFeedback(result *LintResult) *LintFeedback {
	if result == nil {
		return &LintFeedback{
			Rejected: false,
			Reason:   "No lint result available",
		}
	}

	if !result.LinterAvailable {
		return &LintFeedback{
			Rejected: false,
			Reason:   "Linter not available - skipping lint check",
			Action:   "Consider installing " + result.Linter,
		}
	}

	feedback := &LintFeedback{
		Rejected:    !result.Valid,
		Issues:      make([]FeedbackIssue, 0),
		AutoFixable: result.AutoFixableCount(),
	}

	if result.Valid {
		feedback.Reason = "No blocking issues found"
		if result.HasWarnings() {
			feedback.Action = fmt.Sprintf("Consider addressing %d warnings", len(result.Warnings))
		}
	} else {
		feedback.Reason = fmt.Sprintf("Found %d blocking errors", len(result.Errors))
		feedback.Action = "Please regenerate the patch addressing these issues"
	}

	// Add error issues
	for _, issue := range result.Errors {
		fi := FeedbackIssue{
			Rule:    issue.Rule,
			Message: issue.Message,
			Line:    issue.Line,
		}
		if issue.CanAutoFix && issue.Suggestion != "" {
			fi.Fix = issue.Suggestion
		} else if issue.Replacement != "" {
			fi.Fix = fmt.Sprintf("Replace with: %s", issue.Replacement)
		}
		feedback.Issues = append(feedback.Issues, fi)
	}

	// Add warning issues (limited to first 5 to avoid overwhelming)
	for i, issue := range result.Warnings {
		if i >= 5 {
			break
		}
		fi := FeedbackIssue{
			Rule:    issue.Rule,
			Message: issue.Message,
			Line:    issue.Line,
		}
		if issue.Suggestion != "" {
			fi.Fix = issue.Suggestion
		}
		feedback.Issues = append(feedback.Issues, fi)
	}

	return feedback
}

// String returns a human-readable string representation of the feedback.
//
// Description:
//
//	Formats the feedback as a multi-line string suitable for
//	logging or displaying to users. Uses strings.Builder for
//	efficient string construction.
//
// Thread Safety: Safe for concurrent use.
func (f *LintFeedback) String() string {
	if f == nil {
		return ""
	}

	var sb strings.Builder

	// Status line
	if f.Rejected {
		sb.WriteString("REJECTED: ")
	} else {
		sb.WriteString("PASSED: ")
	}
	sb.WriteString(f.Reason)
	sb.WriteString("\n")

	// Issues
	if len(f.Issues) > 0 {
		sb.WriteString("\nIssues:\n")
		for i, issue := range f.Issues {
			sb.WriteString(fmt.Sprintf("  %d. [%s] Line %d: %s\n", i+1, issue.Rule, issue.Line, issue.Message))
			if issue.Fix != "" {
				sb.WriteString(fmt.Sprintf("     Fix: %s\n", issue.Fix))
			}
		}
	}

	// Auto-fixable count
	if f.AutoFixable > 0 {
		sb.WriteString(fmt.Sprintf("\nAuto-fixable: %d issues\n", f.AutoFixable))
	}

	// Action
	if f.Action != "" {
		sb.WriteString(fmt.Sprintf("\nAction: %s\n", f.Action))
	}

	return sb.String()
}
