// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"errors"
)

// ============================================================================
// Configuration
// ============================================================================

// Config holds configuration for validation tools.
type Config struct {
	// WorkingDir is the project root directory for resolving relative paths.
	WorkingDir string

	// MaxFileSize is the maximum file size to validate (bytes).
	// Default: 10MB.
	MaxFileSize int64

	// Timeout is the default timeout for validation operations (seconds).
	// Default: 30.
	Timeout int
}

// NewConfig creates a new Config with the given working directory.
func NewConfig(workingDir string) *Config {
	return &Config{
		WorkingDir:  workingDir,
		MaxFileSize: 10 * 1024 * 1024, // 10MB
		Timeout:     30,
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		WorkingDir:  ".",
		MaxFileSize: 10 * 1024 * 1024,
		Timeout:     30,
	}
}

// ============================================================================
// Error Definitions
// ============================================================================

var (
	// ErrFileNotFound is returned when a file cannot be found.
	ErrFileNotFound = errors.New("file not found")

	// ErrFileTooLarge is returned when a file exceeds the size limit.
	ErrFileTooLarge = errors.New("file exceeds maximum size limit")

	// ErrUnsupportedLanguage is returned for unsupported languages.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrInvalidInput is returned for invalid input parameters.
	ErrInvalidInput = errors.New("invalid input")

	// ErrValidationTimeout is returned when validation times out.
	ErrValidationTimeout = errors.New("validation timed out")
)

// ============================================================================
// Syntax Validation Types (CB-56a)
// ============================================================================

// SyntaxInput defines input parameters for syntax validation.
type SyntaxInput struct {
	// FilePath is the path to the file to validate.
	// Can be absolute or relative to working directory.
	FilePath string `json:"file_path,omitempty"`

	// Content is the code content to validate.
	// If provided, takes precedence over file_path.
	Content string `json:"content,omitempty"`

	// Language overrides language detection.
	// Supported: go, python, javascript, typescript, rust, bash.
	Language string `json:"language,omitempty"`
}

// Validate checks if the input parameters are valid.
func (i *SyntaxInput) Validate() error {
	if i.FilePath == "" && i.Content == "" {
		return errors.New("either file_path or content must be provided")
	}
	return nil
}

// SyntaxOutput contains the result of syntax validation.
type SyntaxOutput struct {
	// Valid indicates if the syntax is correct.
	Valid bool `json:"valid"`

	// Language is the detected or specified language.
	Language string `json:"language"`

	// Errors contains all syntax errors found.
	Errors []SyntaxError `json:"errors,omitempty"`

	// Warnings contains non-fatal syntax warnings.
	Warnings []SyntaxWarning `json:"warnings,omitempty"`

	// ParseTime is how long parsing took (milliseconds).
	ParseTime int64 `json:"parse_time_ms"`
}

// SyntaxError represents a syntax error with location information.
type SyntaxError struct {
	// Line is the 1-indexed line number.
	Line int `json:"line"`

	// Column is the 0-indexed column number.
	Column int `json:"column"`

	// Message describes the error.
	Message string `json:"message"`

	// ErrorType categorizes the error (syntax, missing, unexpected).
	ErrorType string `json:"error_type"`

	// Context is the code snippet around the error.
	Context string `json:"context,omitempty"`

	// Suggestion provides a fix recommendation.
	Suggestion string `json:"suggestion,omitempty"`
}

// SyntaxWarning represents a non-fatal syntax warning.
type SyntaxWarning struct {
	// Line is the 1-indexed line number.
	Line int `json:"line"`

	// Column is the 0-indexed column number.
	Column int `json:"column"`

	// Message describes the warning.
	Message string `json:"message"`

	// WarningType categorizes the warning.
	WarningType string `json:"warning_type"`
}

// ============================================================================
// Test Execution Types (CB-56b)
// ============================================================================

// TestScope defines the scope of test execution.
type TestScope string

const (
	// TestScopeFile runs tests in a single file.
	TestScopeFile TestScope = "file"

	// TestScopePackage runs tests in a package.
	TestScopePackage TestScope = "package"

	// TestScopeAffected runs tests affected by recent changes.
	// Uses call graph to find tests covering modified code.
	TestScopeAffected TestScope = "affected"

	// TestScopeAll runs the full test suite.
	TestScopeAll TestScope = "all"
)

// TestInput defines input parameters for test execution.
type TestInput struct {
	// Scope determines which tests to run.
	Scope TestScope `json:"scope"`

	// Target is the path, pattern, or package to test.
	// Interpretation depends on scope.
	Target string `json:"target"`

	// Timeout is the maximum time for test execution (seconds).
	Timeout int `json:"timeout,omitempty"`

	// Verbose enables verbose test output.
	Verbose bool `json:"verbose,omitempty"`

	// Coverage enables coverage collection.
	Coverage bool `json:"coverage,omitempty"`
}

// Validate checks if the input parameters are valid.
func (i *TestInput) Validate() error {
	if i.Scope == "" {
		return errors.New("scope is required")
	}
	if i.Target == "" && i.Scope != TestScopeAll {
		return errors.New("target is required for non-all scopes")
	}
	return nil
}

// TestOutput contains the result of test execution.
type TestOutput struct {
	// Passed is the count of passing tests.
	Passed int `json:"passed"`

	// Failed is the count of failing tests.
	Failed int `json:"failed"`

	// Skipped is the count of skipped tests.
	Skipped int `json:"skipped"`

	// Duration is total execution time (milliseconds).
	Duration int64 `json:"duration_ms"`

	// Failures contains details of failed tests.
	Failures []TestFailure `json:"failures,omitempty"`

	// Coverage is the coverage percentage (0-100).
	Coverage float64 `json:"coverage_pct,omitempty"`

	// Output is the raw test output.
	Output string `json:"output"`
}

// TestFailure represents a single test failure.
type TestFailure struct {
	// TestName is the name of the failing test.
	TestName string `json:"test_name"`

	// Package is the package containing the test.
	Package string `json:"package"`

	// Message describes the failure.
	Message string `json:"message"`

	// File is the file where the failure occurred.
	File string `json:"file,omitempty"`

	// Line is the line number of the failure.
	Line int `json:"line,omitempty"`

	// Output is the test-specific output.
	Output string `json:"output,omitempty"`
}

// ============================================================================
// Breaking Change Types (CB-56c)
// ============================================================================

// BreakingChangeType categorizes breaking changes.
type BreakingChangeType string

const (
	// BreakingTypeSignature indicates a function signature change.
	BreakingTypeSignature BreakingChangeType = "signature"

	// BreakingTypeRemoval indicates a symbol was removed.
	BreakingTypeRemoval BreakingChangeType = "removal"

	// BreakingTypeType indicates a type change.
	BreakingTypeType BreakingChangeType = "type"

	// BreakingTypeVisibility indicates a visibility change (export -> unexport).
	BreakingTypeVisibility BreakingChangeType = "visibility"

	// BreakingTypeRename indicates a symbol rename.
	BreakingTypeRename BreakingChangeType = "rename"
)

// BreakingInput defines input for breaking change detection.
type BreakingInput struct {
	// Symbol is the symbol being changed.
	Symbol string `json:"symbol"`

	// ChangeType describes the kind of change.
	ChangeType BreakingChangeType `json:"change_type"`

	// NewValue is the new name/type/signature.
	NewValue string `json:"new_value,omitempty"`
}

// Validate checks if the input parameters are valid.
func (i *BreakingInput) Validate() error {
	if i.Symbol == "" {
		return errors.New("symbol is required")
	}
	if i.ChangeType == "" {
		return errors.New("change_type is required")
	}
	return nil
}

// BreakingOutput contains breaking change analysis results.
type BreakingOutput struct {
	// IsBreaking indicates if the change is breaking.
	IsBreaking bool `json:"is_breaking"`

	// Severity indicates the impact level.
	Severity string `json:"severity"` // low, medium, high, critical

	// AffectedCallers lists callers that would break.
	AffectedCallers []CallerInfo `json:"affected_callers,omitempty"`

	// Suggestion provides a recommendation.
	Suggestion string `json:"suggestion,omitempty"`
}

// CallerInfo describes a caller that would be affected.
type CallerInfo struct {
	// Name is the caller function/method name.
	Name string `json:"name"`

	// FilePath is where the caller is defined.
	FilePath string `json:"file_path"`

	// Line is the line number of the call.
	Line int `json:"line"`

	// Package is the caller's package.
	Package string `json:"package,omitempty"`
}

// ============================================================================
// Impact Estimation Types (CB-56d)
// ============================================================================

// ImpactInput defines input for impact estimation.
type ImpactInput struct {
	// Symbols are the symbols being changed.
	Symbols []string `json:"symbols"`

	// ChangeType describes the kind of change.
	ChangeType string `json:"change_type"`
}

// Validate checks if the input parameters are valid.
func (i *ImpactInput) Validate() error {
	if len(i.Symbols) == 0 {
		return errors.New("at least one symbol is required")
	}
	return nil
}

// ImpactOutput contains impact estimation results.
type ImpactOutput struct {
	// FilesAffected is the count of files that would change.
	FilesAffected int `json:"files_affected"`

	// LinesEstimated is the estimated lines that would change.
	LinesEstimated int `json:"lines_estimated"`

	// CallersAffected is the count of affected callers.
	CallersAffected int `json:"callers_affected"`

	// TestsToRun is the estimated number of tests to run.
	TestsToRun int `json:"tests_to_run"`

	// RiskLevel indicates the change risk.
	RiskLevel string `json:"risk_level"` // low, medium, high, critical

	// FileList is the list of affected files.
	FileList []string `json:"file_list,omitempty"`
}
