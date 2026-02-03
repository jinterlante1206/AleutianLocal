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

import "github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"

// RiskAlgorithmVersion is the version of the risk scoring algorithm.
// Increment when making changes that affect risk calculations.
const RiskAlgorithmVersion = "1.0"

// APIVersion is the JSON output API version.
const APIVersion = "1.0"

// Exit codes for impact analysis.
const (
	ExitSuccess   = 0 // Risk at or below threshold
	ExitRiskFound = 1 // Risk above threshold
	ExitError     = 2 // Error (no index, git error, etc.)
)

// Default configuration values.
const (
	DefaultMaxDepth    = 10
	DefaultMaxFiles    = 500
	DefaultThreshold   = RiskHigh
	DefaultMaxAffected = 1000
)

// RiskLevel represents the severity of change risk.
type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

// ParseRiskLevel parses a string to RiskLevel.
func ParseRiskLevel(s string) RiskLevel {
	switch s {
	case "low", "LOW":
		return RiskLow
	case "medium", "MEDIUM":
		return RiskMedium
	case "high", "HIGH":
		return RiskHigh
	case "critical", "CRITICAL":
		return RiskCritical
	default:
		return RiskHigh
	}
}

// Exceeds returns true if this risk level exceeds the threshold.
func (r RiskLevel) Exceeds(threshold RiskLevel) bool {
	levels := map[RiskLevel]int{
		RiskLow:      0,
		RiskMedium:   1,
		RiskHigh:     2,
		RiskCritical: 3,
	}
	return levels[r] > levels[threshold]
}

// ChangeMode specifies how to detect changes.
type ChangeMode string

const (
	ChangeModeFiles  ChangeMode = "files"  // Explicit file list
	ChangeModeDiff   ChangeMode = "diff"   // git diff (uncommitted)
	ChangeModeStaged ChangeMode = "staged" // git diff --cached
	ChangeModeCommit ChangeMode = "commit" // Specific commit
	ChangeModeBranch ChangeMode = "branch" // Since branch point
)

// ChangeType describes how a file was changed.
type ChangeType string

const (
	ChangeAdded    ChangeType = "A"
	ChangeModified ChangeType = "M"
	ChangeDeleted  ChangeType = "D"
	ChangeRenamed  ChangeType = "R"
	ChangeCopied   ChangeType = "C"
)

// Config holds configuration for impact analysis.
//
// # Fields
//
//   - Mode: How to detect changes (diff, staged, commit, branch, files).
//   - CommitHash: Commit to analyze (for commit mode).
//   - BaseBranch: Base branch for comparison (for branch mode).
//   - Files: Explicit file list (for files mode).
//   - Threshold: Risk threshold for exit code.
//   - MaxDepth: Maximum transitive depth.
//   - MaxFiles: Maximum files to analyze.
//   - IncludeTests: Include test files in analysis.
//   - ExcludePatterns: Patterns to exclude from analysis.
type Config struct {
	Mode            ChangeMode
	CommitHash      string
	BaseBranch      string
	Files           []string
	Threshold       RiskLevel
	MaxDepth        int
	MaxFiles        int
	MaxAffected     int
	IncludeTests    bool
	ExcludePatterns []string
	Quiet           bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Mode:         ChangeModeDiff,
		Threshold:    DefaultThreshold,
		MaxDepth:     DefaultMaxDepth,
		MaxFiles:     DefaultMaxFiles,
		MaxAffected:  DefaultMaxAffected,
		IncludeTests: false,
	}
}

// ChangedFile represents a file that was changed.
type ChangedFile struct {
	Path       string     `json:"path"`
	ChangeType ChangeType `json:"change_type"`
	OldPath    string     `json:"old_path,omitempty"` // For renames
}

// ChangedSymbol represents a symbol affected by changes.
type ChangedSymbol struct {
	Symbol     initializer.Symbol `json:"symbol"`
	ChangeType ChangeType         `json:"change_type"`
	FilePath   string             `json:"file_path"`
}

// AffectedSymbol represents a symbol in the blast radius.
type AffectedSymbol struct {
	Symbol   initializer.Symbol `json:"symbol"`
	Depth    int                `json:"depth"`     // Distance from changed symbol
	SourceID string             `json:"source_id"` // Which changed symbol caused this
}

// RiskFactors holds factors used for risk calculation.
type RiskFactors struct {
	DirectCallers     int      `json:"direct_callers"`
	TransitiveCallers int      `json:"transitive_callers"`
	IsSecurityPath    bool     `json:"is_security_path"`
	IsPublicAPI       bool     `json:"is_public_api"`
	HasDBOperations   bool     `json:"has_db_operations"`
	HasIOOperations   bool     `json:"has_io_operations"`
	AffectedPackages  int      `json:"affected_packages"`
	AffectedTests     int      `json:"affected_tests"`
	Reasons           []string `json:"reasons"`
}

// Result holds the impact analysis result.
type Result struct {
	APIVersion           string           `json:"api_version"`
	RiskAlgorithmVersion string           `json:"risk_algorithm_version"`
	RiskLevel            RiskLevel        `json:"risk_level"`
	RiskFactors          RiskFactors      `json:"risk_factors"`
	ChangedFiles         []ChangedFile    `json:"changed_files"`
	ChangedSymbols       []ChangedSymbol  `json:"changed_symbols"`
	AffectedSymbols      []AffectedSymbol `json:"affected_symbols"`
	AffectedTests        []string         `json:"affected_tests"`
	AffectedPackages     []string         `json:"affected_packages"`
	DirectCount          int              `json:"direct_count"`
	TransitiveCount      int              `json:"transitive_count"`
	TotalAffected        int              `json:"total_affected"`
	Truncated            bool             `json:"truncated"`
	Warnings             []string         `json:"warnings,omitempty"`
	DurationMs           int64            `json:"duration_ms"`
}

// NewResult creates a new Result with default values.
func NewResult() *Result {
	return &Result{
		APIVersion:           APIVersion,
		RiskAlgorithmVersion: RiskAlgorithmVersion,
		RiskLevel:            RiskLow,
		ChangedFiles:         make([]ChangedFile, 0),
		ChangedSymbols:       make([]ChangedSymbol, 0),
		AffectedSymbols:      make([]AffectedSymbol, 0),
		AffectedTests:        make([]string, 0),
		AffectedPackages:     make([]string, 0),
		Warnings:             make([]string, 0),
	}
}

// Summary returns a human-readable summary of the impact.
func (r *Result) Summary() string {
	if len(r.ChangedSymbols) == 0 {
		return "No code changes detected."
	}
	return ""
}
