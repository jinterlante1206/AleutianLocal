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
	"time"
)

// =============================================================================
// SEVERITY
// =============================================================================

// Severity represents the severity level of a lint issue.
type Severity int

const (
	// SeverityInfo represents informational/style issues that don't block patches.
	SeverityInfo Severity = iota

	// SeverityWarning represents issues that should be noted but don't block patches.
	SeverityWarning

	// SeverityError represents issues that block patches from being applied.
	SeverityError
)

// String returns the string representation of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// SeverityFromString parses a severity string.
//
// Description:
//
//	Parses common severity strings from different linters.
//	Unknown values default to SeverityWarning.
//
// Inputs:
//
//	s - Severity string (e.g., "error", "warning", "info")
//
// Outputs:
//
//	Severity - The parsed severity level
func SeverityFromString(s string) Severity {
	switch s {
	case "error", "err", "fatal", "critical":
		return SeverityError
	case "warning", "warn":
		return SeverityWarning
	case "info", "note", "style", "hint":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// =============================================================================
// LINTER CONFIG
// =============================================================================

// LinterConfig configures how to run a specific linter.
//
// Thread Safety: Treat as immutable after creation.
type LinterConfig struct {
	// Language is the language this linter handles (e.g., "go", "python").
	Language string

	// Command is the linter executable name (e.g., "golangci-lint").
	Command string

	// Args are the arguments to pass to the linter.
	// Should include flags for JSON output.
	Args []string

	// Extensions are file extensions this linter handles (e.g., []string{".go"}).
	Extensions []string

	// Timeout is the maximum time to wait for the linter.
	Timeout time.Duration

	// Available indicates whether the linter binary was found in PATH.
	// Set by DetectAvailableLinters.
	Available bool

	// SupportsStdin indicates whether the linter can read from stdin.
	SupportsStdin bool

	// FixArgs are arguments for running the linter in fix mode.
	// Empty if the linter doesn't support auto-fix.
	FixArgs []string
}

// Clone returns a deep copy of the config.
func (c *LinterConfig) Clone() *LinterConfig {
	clone := &LinterConfig{
		Language:      c.Language,
		Command:       c.Command,
		Args:          make([]string, len(c.Args)),
		Extensions:    make([]string, len(c.Extensions)),
		Timeout:       c.Timeout,
		Available:     c.Available,
		SupportsStdin: c.SupportsStdin,
		FixArgs:       make([]string, len(c.FixArgs)),
	}
	copy(clone.Args, c.Args)
	copy(clone.Extensions, c.Extensions)
	copy(clone.FixArgs, c.FixArgs)
	return clone
}

// =============================================================================
// LINT RESULT
// =============================================================================

// LintResult contains the result of running a linter.
//
// Thread Safety: Immutable after creation by the runner.
type LintResult struct {
	// Valid is true if no blocking errors were found.
	Valid bool `json:"valid"`

	// Errors are issues with SeverityError that block the patch.
	Errors []LintIssue `json:"errors"`

	// Warnings are issues with SeverityWarning that don't block.
	Warnings []LintIssue `json:"warnings"`

	// Infos are informational issues (style, hints).
	Infos []LintIssue `json:"infos,omitempty"`

	// Duration is how long the linter took to run.
	Duration time.Duration `json:"duration"`

	// Linter is which linter produced this result.
	Linter string `json:"linter"`

	// Language is the language that was linted.
	Language string `json:"language"`

	// FilePath is the file that was linted (may be temp file for content lint).
	FilePath string `json:"file_path,omitempty"`

	// LinterAvailable indicates whether the linter was found.
	// When false, the result may be empty due to unavailable linter.
	LinterAvailable bool `json:"linter_available"`
}

// HasErrors returns true if there are any blocking errors.
func (r *LintResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings returns true if there are any warnings.
func (r *LintResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasIssues returns true if there are any issues of any severity.
func (r *LintResult) HasIssues() bool {
	return len(r.Errors) > 0 || len(r.Warnings) > 0 || len(r.Infos) > 0
}

// AllIssues returns all issues combined.
func (r *LintResult) AllIssues() []LintIssue {
	total := len(r.Errors) + len(r.Warnings) + len(r.Infos)
	issues := make([]LintIssue, 0, total)
	issues = append(issues, r.Errors...)
	issues = append(issues, r.Warnings...)
	issues = append(issues, r.Infos...)
	return issues
}

// IssueCount returns the total number of issues.
func (r *LintResult) IssueCount() int {
	return len(r.Errors) + len(r.Warnings) + len(r.Infos)
}

// AutoFixableCount returns the count of issues that can be auto-fixed.
func (r *LintResult) AutoFixableCount() int {
	count := 0
	for _, issue := range r.AllIssues() {
		if issue.CanAutoFix {
			count++
		}
	}
	return count
}

// =============================================================================
// LINT ISSUE
// =============================================================================

// LintIssue represents a single issue found by a linter.
//
// Thread Safety: Immutable after creation.
type LintIssue struct {
	// File is the path to the file containing the issue.
	File string `json:"file"`

	// Line is the 1-indexed line number where the issue occurs.
	Line int `json:"line"`

	// Column is the 1-indexed column number where the issue occurs.
	// May be 0 if the linter doesn't provide column info.
	Column int `json:"column,omitempty"`

	// EndLine is the ending line for multi-line issues.
	// May be 0 if the linter doesn't provide end position.
	EndLine int `json:"end_line,omitempty"`

	// EndColumn is the ending column for the issue.
	EndColumn int `json:"end_column,omitempty"`

	// Rule is the linter rule that triggered (e.g., "errcheck", "E501").
	Rule string `json:"rule"`

	// RuleURL is a link to documentation for the rule.
	RuleURL string `json:"rule_url,omitempty"`

	// Severity is the severity level of the issue.
	Severity Severity `json:"severity"`

	// Message is the human-readable description of the issue.
	Message string `json:"message"`

	// Suggestion is a suggested fix if available.
	Suggestion string `json:"suggestion,omitempty"`

	// CanAutoFix indicates whether this issue can be automatically fixed.
	CanAutoFix bool `json:"can_auto_fix"`

	// Replacement is the suggested replacement text for auto-fix.
	Replacement string `json:"replacement,omitempty"`

	// Linter is the name of the linter that found this issue.
	Linter string `json:"linter,omitempty"`
}

// Location returns a formatted location string (file:line:col).
func (i *LintIssue) Location() string {
	if i.Column > 0 {
		return i.File + ":" + itoa(i.Line) + ":" + itoa(i.Column)
	}
	return i.File + ":" + itoa(i.Line)
}

// itoa is a simple int to string conversion to avoid importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

// =============================================================================
// TEXT EDIT
// =============================================================================

// TextEdit represents a text change for auto-fix.
//
// Thread Safety: Immutable after creation.
type TextEdit struct {
	// StartLine is the 1-indexed starting line.
	StartLine int `json:"start_line"`

	// StartColumn is the 1-indexed starting column.
	StartColumn int `json:"start_column"`

	// EndLine is the 1-indexed ending line.
	EndLine int `json:"end_line"`

	// EndColumn is the 1-indexed ending column.
	EndColumn int `json:"end_column"`

	// NewText is the replacement text.
	NewText string `json:"new_text"`
}

// =============================================================================
// LINT OPTIONS
// =============================================================================

// LintOptions configures a single lint operation.
type LintOptions struct {
	// Timeout overrides the default timeout for this operation.
	Timeout time.Duration

	// Rules limits linting to specific rules. Empty means all rules.
	Rules []string

	// ExcludeRules excludes specific rules from linting.
	ExcludeRules []string

	// IncludeFixes includes fix suggestions when available.
	IncludeFixes bool
}

// DefaultLintOptions returns the default options.
func DefaultLintOptions() LintOptions {
	return LintOptions{
		IncludeFixes: true,
	}
}
