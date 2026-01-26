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

import "time"

// ErrorType represents the type of validation error.
type ErrorType string

const (
	ErrorTypeSizeLimit  ErrorType = "SIZE_LIMIT"
	ErrorTypeDiffParse  ErrorType = "DIFF_PARSE"
	ErrorTypeSyntax     ErrorType = "SYNTAX"
	ErrorTypePermission ErrorType = "PERMISSION"
	ErrorTypeInternal   ErrorType = "INTERNAL"
)

// WarnType represents the type of validation warning.
type WarnType string

const (
	WarnTypeDangerousPattern WarnType = "DANGEROUS_PATTERN"
	WarnTypeSecret           WarnType = "SECRET"
	WarnTypeSSRF             WarnType = "SSRF"
	WarnTypeSQLInjection     WarnType = "SQL_INJECTION"
	WarnTypeTemplateInject   WarnType = "TEMPLATE_INJECTION"
	WarnTypePrototypePollute WarnType = "PROTOTYPE_POLLUTION"
	WarnTypeDeserialization  WarnType = "DESERIALIZATION"
	WarnTypePathTraversal    WarnType = "PATH_TRAVERSAL"
)

// Severity represents the severity level of a warning.
type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
)

// ValidationResult contains the result of patch validation.
type ValidationResult struct {
	// Valid indicates whether the patch passed validation.
	Valid bool `json:"valid"`

	// Errors contains blocking validation errors.
	Errors []ValidationError `json:"errors,omitempty"`

	// Warnings contains non-blocking warnings.
	Warnings []ValidationWarning `json:"warnings,omitempty"`

	// Permissions contains file permission issues.
	Permissions []PermissionIssue `json:"permissions,omitempty"`

	// Stats contains patch statistics.
	Stats PatchStats `json:"stats"`

	// PatternVersion is the version of the pattern database used.
	PatternVersion string `json:"pattern_version"`

	// ValidatedAt is when validation occurred.
	ValidatedAt time.Time `json:"validated_at"`
}

// ValidationError represents a blocking validation error.
type ValidationError struct {
	// Type is the error type.
	Type ErrorType `json:"type"`

	// Message is a human-readable error message.
	Message string `json:"message"`

	// File is the file where the error occurred.
	File string `json:"file,omitempty"`

	// Line is the line number where the error occurred.
	Line int `json:"line,omitempty"`
}

// ValidationWarning represents a non-blocking warning.
type ValidationWarning struct {
	// Type is the warning type.
	Type WarnType `json:"type"`

	// Pattern is the dangerous pattern that was matched.
	Pattern string `json:"pattern"`

	// File is the file where the pattern was found.
	File string `json:"file"`

	// Line is the line number where the pattern was found.
	Line int `json:"line"`

	// Severity is the severity level.
	Severity Severity `json:"severity"`

	// Message is a human-readable warning message.
	Message string `json:"message"`

	// Suggestion is a suggestion for fixing the issue.
	Suggestion string `json:"suggestion,omitempty"`

	// Blocking indicates if this warning should block the patch.
	Blocking bool `json:"blocking"`
}

// PermissionIssue represents a file permission problem.
type PermissionIssue struct {
	// File is the file path.
	File string `json:"file"`

	// Issue describes the permission problem.
	Issue string `json:"issue"`
}

// PatchStats contains statistics about the patch.
type PatchStats struct {
	// LinesAdded is the number of lines added.
	LinesAdded int `json:"lines_added"`

	// LinesRemoved is the number of lines removed.
	LinesRemoved int `json:"lines_removed"`

	// FilesAffected is the number of files affected.
	FilesAffected int `json:"files_affected"`
}

// ValidatorConfig configures the patch validator.
type ValidatorConfig struct {
	// MaxLines is the maximum number of lines allowed in a patch.
	MaxLines int

	// BlockDangerous determines if dangerous patterns block validation.
	BlockDangerous bool

	// WarnOnly makes all patterns warnings instead of blockers.
	WarnOnly bool

	// MinPatternVersion is the minimum required pattern version.
	MinPatternVersion string

	// AllowlistPaths are path patterns to skip for secret scanning.
	AllowlistPaths []string

	// MinSecretEntropy is the minimum entropy for secret detection.
	MinSecretEntropy float64
}

// DefaultValidatorConfig returns the default configuration.
func DefaultValidatorConfig() ValidatorConfig {
	return ValidatorConfig{
		MaxLines:          500,
		BlockDangerous:    true,
		WarnOnly:          false,
		MinPatternVersion: "",
		AllowlistPaths: []string{
			"*_test.go",
			"test_*.py",
			"*.test.js",
			"*.test.ts",
			"**/testdata/**",
			"**/fixtures/**",
			"**/__tests__/**",
		},
		MinSecretEntropy: 3.5,
	}
}

// DangerousPattern defines a pattern to detect in code.
type DangerousPattern struct {
	// Name is the pattern identifier.
	Name string

	// Language is the language this pattern applies to ("" for all).
	Language string

	// NodeType is the AST node type to match (for AST-based detection).
	NodeType string

	// FuncNames are function names that trigger this pattern.
	FuncNames []string

	// Severity is the severity level.
	Severity Severity

	// Message describes the issue.
	Message string

	// Suggestion describes how to fix the issue.
	Suggestion string

	// Blocking indicates if this pattern should block patches.
	Blocking bool

	// WarnType is the warning type category.
	WarnType WarnType
}

// SecretPattern defines a pattern for detecting secrets.
type SecretPattern struct {
	// Name is the pattern identifier.
	Name string

	// Pattern is the regex pattern.
	Pattern string

	// MinEntropy overrides the default minimum entropy.
	MinEntropy float64

	// Keywords are context keywords that must appear nearby.
	Keywords []string

	// Severity is the severity level.
	Severity Severity

	// Message describes the issue.
	Message string
}
