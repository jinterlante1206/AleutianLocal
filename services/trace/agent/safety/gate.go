// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package safety provides safety gate functionality for the agent loop.
//
// The safety gate validates proposed changes before they are executed,
// checking for potential issues like dangerous patterns, sensitive file
// modifications, or security vulnerabilities.
//
// Thread Safety:
//
//	All types in this package are designed for concurrent use.
package safety

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Severity indicates the severity of a safety issue.
type Severity string

const (
	// SeverityInfo is for informational messages.
	SeverityInfo Severity = "info"

	// SeverityWarning is for warnings that should be reviewed.
	SeverityWarning Severity = "warning"

	// SeverityCritical is for critical issues that should block execution.
	SeverityCritical Severity = "critical"
)

// ChangeMetadata contains additional typed information about a proposed change.
type ChangeMetadata struct {
	// Reason explains why this change is being made.
	Reason string `json:"reason,omitempty"`

	// ToolName is the tool that initiated this change.
	ToolName string `json:"tool_name,omitempty"`

	// InvocationID links to the tool invocation.
	InvocationID string `json:"invocation_id,omitempty"`

	// OldContent is the previous content (for modifications).
	OldContent string `json:"old_content,omitempty"`

	// LineRange specifies the affected lines (for partial edits).
	LineRange *LineRange `json:"line_range,omitempty"`

	// Permissions are the requested file permissions (for creates).
	Permissions string `json:"permissions,omitempty"`
}

// LineRange specifies a range of lines in a file.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ProposedChange represents a change the agent wants to make.
type ProposedChange struct {
	// Type is the kind of change (e.g., "file_write", "file_delete", "shell_command").
	Type string `json:"type"`

	// Target is the target of the change (e.g., file path, command).
	Target string `json:"target"`

	// Content is the proposed content (for writes).
	Content string `json:"content,omitempty"`

	// Metadata contains additional typed information about the change.
	Metadata *ChangeMetadata `json:"metadata,omitempty"`
}

// Issue represents a safety issue found during checking.
type Issue struct {
	// Severity indicates how serious the issue is.
	Severity Severity `json:"severity"`

	// Code is a machine-readable issue code.
	Code string `json:"code"`

	// Message is a human-readable description.
	Message string `json:"message"`

	// Change is the change that triggered this issue.
	Change *ProposedChange `json:"change,omitempty"`

	// Suggestion is an optional suggested fix.
	Suggestion string `json:"suggestion,omitempty"`
}

// Result contains the result of a safety check.
type Result struct {
	// Passed is true if no blocking issues were found.
	Passed bool `json:"passed"`

	// Issues contains all issues found during checking.
	Issues []Issue `json:"issues,omitempty"`

	// CriticalCount is the number of critical issues.
	CriticalCount int `json:"critical_count"`

	// WarningCount is the number of warnings.
	WarningCount int `json:"warning_count"`

	// ChecksRun is the number of safety checks that were executed.
	ChecksRun int `json:"checks_run"`
}

// HasCritical returns true if there are critical issues.
func (r *Result) HasCritical() bool {
	return r.CriticalCount > 0
}

// HasWarnings returns true if there are warnings.
func (r *Result) HasWarnings() bool {
	return r.WarningCount > 0
}

// GateConfig configures the safety gate behavior.
type GateConfig struct {
	// Enabled determines if safety checks are enabled.
	Enabled bool

	// BlockOnCritical determines if critical issues block execution.
	BlockOnCritical bool

	// BlockOnWarning determines if warnings also block execution.
	BlockOnWarning bool

	// AllowedPaths are paths that are always allowed to be modified.
	AllowedPaths []string

	// BlockedPaths are paths that are never allowed to be modified.
	BlockedPaths []string

	// AllowedCommands are shell commands that are always allowed.
	AllowedCommands []string

	// BlockedCommands are shell commands that are never allowed.
	BlockedCommands []string

	// MaxFileSize is the maximum allowed file size for writes.
	MaxFileSize int64
}

// DefaultGateConfig returns sensible defaults.
func DefaultGateConfig() GateConfig {
	return GateConfig{
		Enabled:         true,
		BlockOnCritical: true,
		BlockOnWarning:  false,
		BlockedPaths: []string{
			".git",
			".env",
			"credentials",
			"secrets",
			"private",
		},
		BlockedCommands: []string{
			"rm -rf",
			"format",
			"mkfs",
			"dd if=",
			"> /dev/",
			"chmod 777",
		},
		MaxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

// Gate is the safety gate interface.
//
// Implementations validate proposed changes before execution.
type Gate interface {
	// Check validates the proposed changes.
	//
	// Inputs:
	//   ctx - Context for cancellation.
	//   changes - The changes to validate.
	//
	// Outputs:
	//   *Result - The check result.
	//   error - Non-nil if the check itself fails.
	Check(ctx context.Context, changes []ProposedChange) (*Result, error)

	// ShouldBlock determines if the result should block execution.
	//
	// Inputs:
	//   result - The check result.
	//
	// Outputs:
	//   bool - True if execution should be blocked.
	ShouldBlock(result *Result) bool

	// GenerateWarnings creates human-readable warnings from issues.
	//
	// Inputs:
	//   result - The check result.
	//
	// Outputs:
	//   []string - Warning messages.
	GenerateWarnings(result *Result) []string
}

// Checker is a single safety check.
type Checker interface {
	// Name returns the checker name.
	Name() string

	// Check runs the safety check.
	Check(ctx context.Context, change *ProposedChange) []Issue
}

// DefaultGate implements the Gate interface with configurable checks.
//
// Thread Safety: DefaultGate is safe for concurrent use.
type DefaultGate struct {
	mu       sync.RWMutex
	config   GateConfig
	checkers []Checker
}

// NewDefaultGate creates a new safety gate with the provided config.
//
// Description:
//
//	Creates a DefaultGate instance with the specified configuration.
//	Registers default checkers: PathChecker, CommandChecker, and FileSizeChecker.
//	If config is nil, uses DefaultGateConfig().
//
// Inputs:
//
//	config - Gate configuration. If nil, uses defaults.
//
// Outputs:
//
//	*DefaultGate - The configured gate with default checkers registered.
//
// Example:
//
//	gate := NewDefaultGate(&GateConfig{
//	    Enabled:         true,
//	    BlockOnCritical: true,
//	    BlockedPaths:    []string{".git", ".env"},
//	})
func NewDefaultGate(config *GateConfig) *DefaultGate {
	cfg := DefaultGateConfig()
	if config != nil {
		cfg = *config
	}

	gate := &DefaultGate{
		config:   cfg,
		checkers: make([]Checker, 0),
	}

	// Register default checkers
	gate.RegisterChecker(&PathChecker{config: cfg})
	gate.RegisterChecker(&CommandChecker{config: cfg})
	gate.RegisterChecker(&FileSizeChecker{config: cfg})

	return gate
}

// RegisterChecker adds a checker to the gate.
func (g *DefaultGate) RegisterChecker(checker Checker) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkers = append(g.checkers, checker)
}

// Check implements Gate.
func (g *DefaultGate) Check(ctx context.Context, changes []ProposedChange) (*Result, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.config.Enabled {
		return &Result{Passed: true}, nil
	}

	result := &Result{
		Passed: true,
		Issues: make([]Issue, 0),
	}

	for i := range changes {
		// SG-001: Use index to avoid loop variable capture issues
		change := &changes[i]
		for _, checker := range g.checkers {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			issues := checker.Check(ctx, change)
			result.ChecksRun++

			for j := range issues {
				// SG-001: Set Change at creation, avoid mutation after append
				issues[j].Change = change
				result.Issues = append(result.Issues, issues[j])

				switch issues[j].Severity {
				case SeverityCritical:
					result.CriticalCount++
				case SeverityWarning:
					result.WarningCount++
				}
			}
		}
	}

	// Determine if passed based on config
	if g.config.BlockOnCritical && result.CriticalCount > 0 {
		result.Passed = false
	}
	if g.config.BlockOnWarning && result.WarningCount > 0 {
		result.Passed = false
	}

	return result, nil
}

// ShouldBlock implements Gate.
func (g *DefaultGate) ShouldBlock(result *Result) bool {
	if result == nil {
		return false
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.config.BlockOnCritical && result.CriticalCount > 0 {
		return true
	}
	if g.config.BlockOnWarning && result.WarningCount > 0 {
		return true
	}

	return false
}

// GenerateWarnings implements Gate.
func (g *DefaultGate) GenerateWarnings(result *Result) []string {
	if result == nil || len(result.Issues) == 0 {
		return nil
	}

	warnings := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		prefix := ""
		switch issue.Severity {
		case SeverityCritical:
			prefix = "[CRITICAL] "
		case SeverityWarning:
			prefix = "[WARNING] "
		case SeverityInfo:
			prefix = "[INFO] "
		}

		msg := fmt.Sprintf("%s%s", prefix, issue.Message)
		if issue.Suggestion != "" {
			msg += fmt.Sprintf(" Suggestion: %s", issue.Suggestion)
		}
		warnings = append(warnings, msg)
	}

	return warnings
}

// PathChecker validates file operations against blocked path patterns.
//
// Description:
//
//	Checks if proposed file operations (writes, deletes) target paths
//	that match blocked patterns defined in the configuration.
//	Returns critical issues for any blocked path matches.
//
// Thread Safety:
//
//	PathChecker is safe for concurrent use as it only reads config.
type PathChecker struct {
	config GateConfig
}

// Name implements Checker.
func (c *PathChecker) Name() string {
	return "path_checker"
}

// Check implements Checker.
func (c *PathChecker) Check(ctx context.Context, change *ProposedChange) []Issue {
	if change.Type != "file_write" && change.Type != "file_delete" {
		return nil
	}

	var issues []Issue

	// Check blocked paths
	for _, blocked := range c.config.BlockedPaths {
		if containsPath(change.Target, blocked) {
			issues = append(issues, Issue{
				Severity:   SeverityCritical,
				Code:       "BLOCKED_PATH",
				Message:    fmt.Sprintf("Operation on blocked path: %s contains %s", change.Target, blocked),
				Suggestion: "Choose a different target path or modify the safety configuration.",
			})
		}
	}

	return issues
}

// CommandChecker validates shell commands against blocked patterns.
//
// Description:
//
//	Checks if proposed shell commands contain blocked patterns
//	defined in the configuration (e.g., "rm -rf", "chmod 777").
//	Returns critical issues for any blocked command matches.
//
// Thread Safety:
//
//	CommandChecker is safe for concurrent use as it only reads config.
type CommandChecker struct {
	config GateConfig
}

// Name implements Checker.
func (c *CommandChecker) Name() string {
	return "command_checker"
}

// Check implements Checker.
func (c *CommandChecker) Check(ctx context.Context, change *ProposedChange) []Issue {
	if change.Type != "shell_command" {
		return nil
	}

	var issues []Issue

	// Check blocked commands
	for _, blocked := range c.config.BlockedCommands {
		if containsCommand(change.Target, blocked) {
			issues = append(issues, Issue{
				Severity:   SeverityCritical,
				Code:       "BLOCKED_COMMAND",
				Message:    fmt.Sprintf("Blocked command pattern detected: %s", blocked),
				Suggestion: "Use a safer alternative command.",
			})
		}
	}

	return issues
}

// FileSizeChecker validates file write sizes against configured limits.
//
// Description:
//
//	Checks if proposed file write content exceeds the maximum file size
//	limit defined in the configuration. Returns a warning for oversized files.
//
// Thread Safety:
//
//	FileSizeChecker is safe for concurrent use as it only reads config.
type FileSizeChecker struct {
	config GateConfig
}

// Name implements Checker.
func (c *FileSizeChecker) Name() string {
	return "file_size_checker"
}

// Check implements Checker.
func (c *FileSizeChecker) Check(ctx context.Context, change *ProposedChange) []Issue {
	if change.Type != "file_write" {
		return nil
	}

	var issues []Issue

	if c.config.MaxFileSize > 0 && int64(len(change.Content)) > c.config.MaxFileSize {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Code:     "FILE_TOO_LARGE",
			Message: fmt.Sprintf("File size (%d bytes) exceeds maximum (%d bytes)",
				len(change.Content), c.config.MaxFileSize),
			Suggestion: "Consider splitting the file or increasing the size limit.",
		})
	}

	return issues
}

// containsPath checks if a path contains a blocked pattern.
//
// Description:
//
//	Checks if the given path matches a blocked pattern. Handles various
//	path formats: exact match, directory component, or path prefix/suffix.
//
// Inputs:
//
//	path - The file path to check.
//	pattern - The blocked pattern to match against.
//
// Outputs:
//
//	bool - True if the path matches the blocked pattern.
func containsPath(path, pattern string) bool {
	if len(pattern) == 0 || len(path) < len(pattern) {
		return false
	}
	return path == pattern ||
		strings.Contains(path, "/"+pattern+"/") ||
		strings.HasSuffix(path, "/"+pattern) ||
		strings.HasPrefix(path, pattern+"/")
}

// containsCommand checks if a command contains a blocked pattern.
//
// Description:
//
//	Checks if the given command string contains a blocked pattern
//	using simple substring matching.
//
// Inputs:
//
//	command - The shell command to check.
//	pattern - The blocked pattern to match against.
//
// Outputs:
//
//	bool - True if the command contains the blocked pattern.
func containsCommand(command, pattern string) bool {
	return strings.Contains(command, pattern)
}

// MockGate is a mock implementation for testing.
type MockGate struct {
	mu sync.RWMutex

	// CheckFunc overrides Check behavior.
	CheckFunc func(ctx context.Context, changes []ProposedChange) (*Result, error)

	// ShouldBlockFunc overrides ShouldBlock behavior.
	ShouldBlockFunc func(result *Result) bool

	// Calls records all Check calls.
	Calls [][]ProposedChange
}

// NewMockGate creates a new mock gate.
func NewMockGate() *MockGate {
	return &MockGate{
		Calls: make([][]ProposedChange, 0),
	}
}

// Check implements Gate.
func (m *MockGate) Check(ctx context.Context, changes []ProposedChange) (*Result, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, changes)
	m.mu.Unlock()

	if m.CheckFunc != nil {
		return m.CheckFunc(ctx, changes)
	}

	return &Result{Passed: true}, nil
}

// ShouldBlock implements Gate.
func (m *MockGate) ShouldBlock(result *Result) bool {
	if m.ShouldBlockFunc != nil {
		return m.ShouldBlockFunc(result)
	}
	return result != nil && !result.Passed
}

// GenerateWarnings implements Gate.
func (m *MockGate) GenerateWarnings(result *Result) []string {
	if result == nil || len(result.Issues) == 0 {
		return nil
	}

	warnings := make([]string, len(result.Issues))
	for i, issue := range result.Issues {
		warnings[i] = issue.Message
	}
	return warnings
}

// CallCount returns the number of Check calls.
func (m *MockGate) CallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Calls)
}

// Reset clears all recorded calls.
func (m *MockGate) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = make([][]ProposedChange, 0)
}
