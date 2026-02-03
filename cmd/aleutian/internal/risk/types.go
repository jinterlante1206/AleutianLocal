// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package risk

import "strings"

// RiskAlgorithmVersion is the version of the risk scoring algorithm.
// Increment when making changes that affect risk calculations.
const RiskAlgorithmVersion = "2.0"

// APIVersion is the JSON output API version.
const APIVersion = "1.0"

// Exit codes for risk assessment.
const (
	ExitSuccess   = 0 // Risk at or below threshold
	ExitRiskFound = 1 // Risk above threshold
	ExitError     = 2 // Error (no index, analysis failure)
)

// Default configuration values.
const (
	DefaultThreshold     = RiskHigh
	DefaultTimeout       = 60 // seconds
	DefaultSignalTimeout = 30 // seconds per signal
)

// Default weights for risk signals.
const (
	DefaultWeightImpact     = 0.5
	DefaultWeightPolicy     = 0.3
	DefaultWeightComplexity = 0.2
)

// Risk level thresholds.
const (
	ThresholdCritical = 0.8
	ThresholdHigh     = 0.6
	ThresholdMedium   = 0.3
)

// Complexity scoring thresholds.
const (
	MaxLinesForScore      = 500.0
	MaxFilesForScore      = 10.0
	MaxComplexityForScore = 20.0
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
	switch strings.ToLower(s) {
	case "low":
		return RiskLow
	case "medium":
		return RiskMedium
	case "high":
		return RiskHigh
	case "critical":
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

// Order returns the numeric order of this risk level.
func (r RiskLevel) Order() int {
	levels := map[RiskLevel]int{
		RiskLow:      0,
		RiskMedium:   1,
		RiskHigh:     2,
		RiskCritical: 3,
	}
	return levels[r]
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

// Config holds configuration for risk assessment.
//
// # Fields
//
//   - Mode: How to detect changes (diff, staged, commit, branch, files).
//   - CommitHash: Commit to analyze (for commit mode).
//   - BaseBranch: Base branch for comparison (for branch mode).
//   - Files: Explicit file list (for files mode).
//   - Threshold: Risk threshold for exit code.
//   - SkipImpact: Skip impact analysis signal.
//   - SkipPolicy: Skip policy check signal.
//   - SkipComplexity: Skip complexity analysis signal.
//   - Quiet: Suppress output.
//   - Explain: Show detailed signal breakdown.
type Config struct {
	Mode           ChangeMode
	CommitHash     string
	BaseBranch     string
	Files          []string
	Threshold      RiskLevel
	SkipImpact     bool
	SkipPolicy     bool
	SkipComplexity bool
	Quiet          bool
	Explain        bool
	Timeout        int // Total timeout in seconds
	SignalTimeout  int // Per-signal timeout in seconds
	BestEffort     bool
	Weights        Weights
	IndexPath      string
	ProjectRoot    string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Mode:          ChangeModeDiff,
		Threshold:     DefaultThreshold,
		Timeout:       DefaultTimeout,
		SignalTimeout: DefaultSignalTimeout,
		BestEffort:    false,
		Weights:       DefaultWeights(),
	}
}

// Weights holds the weights for each risk signal.
type Weights struct {
	Impact     float64 `json:"impact"`
	Policy     float64 `json:"policy"`
	Complexity float64 `json:"complexity"`
}

// DefaultWeights returns default weights for risk signals.
func DefaultWeights() Weights {
	return Weights{
		Impact:     DefaultWeightImpact,
		Policy:     DefaultWeightPolicy,
		Complexity: DefaultWeightComplexity,
	}
}

// Total returns the sum of all weights.
func (w Weights) Total() float64 {
	return w.Impact + w.Policy + w.Complexity
}

// ImpactSignal holds the impact analysis result.
type ImpactSignal struct {
	Score             float64  `json:"score"`
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

// PolicySignal holds the policy check result.
type PolicySignal struct {
	Score         float64  `json:"score"`
	TotalFound    int      `json:"total_found"`
	CriticalCount int      `json:"critical_count"`
	HighCount     int      `json:"high_count"`
	MediumCount   int      `json:"medium_count"`
	LowCount      int      `json:"low_count"`
	HasCritical   bool     `json:"has_critical"`
	Reasons       []string `json:"reasons"`
}

// ComplexitySignal holds the complexity analysis result.
type ComplexitySignal struct {
	Score           float64  `json:"score"`
	LinesAdded      int      `json:"lines_added"`
	LinesRemoved    int      `json:"lines_removed"`
	FilesChanged    int      `json:"files_changed"`
	CyclomaticDelta int      `json:"cyclomatic_delta"`
	Reasons         []string `json:"reasons"`
}

// Signals holds all collected risk signals.
type Signals struct {
	Impact     *ImpactSignal     `json:"impact,omitempty"`
	Policy     *PolicySignal     `json:"policy,omitempty"`
	Complexity *ComplexitySignal `json:"complexity,omitempty"`
}

// Contributing factor for risk report.
type Factor struct {
	Signal   string `json:"signal"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Result holds the risk assessment result.
type Result struct {
	APIVersion           string    `json:"api_version"`
	RiskAlgorithmVersion string    `json:"risk_algorithm_version"`
	RiskLevel            RiskLevel `json:"risk_level"`
	Score                float64   `json:"score"`
	Signals              Signals   `json:"signals"`
	Factors              []Factor  `json:"factors"`
	Recommendation       string    `json:"recommendation"`
	Errors               []string  `json:"errors,omitempty"`
	DurationMs           int64     `json:"duration_ms"`
}

// NewResult creates a new Result with default values.
func NewResult() *Result {
	return &Result{
		APIVersion:           APIVersion,
		RiskAlgorithmVersion: RiskAlgorithmVersion,
		RiskLevel:            RiskLow,
		Factors:              make([]Factor, 0),
		Errors:               make([]string, 0),
	}
}

// ChangedFile represents a file that was changed.
type ChangedFile struct {
	Path         string `json:"path"`
	ChangeType   string `json:"change_type"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
}

// Recommendations for each risk level.
var Recommendations = map[RiskLevel]string{
	RiskLow:      "Standard review process",
	RiskMedium:   "Standard review process; consider additional testing",
	RiskHigh:     "Thorough review recommended; consider security review",
	RiskCritical: "Requires senior engineer and security team review",
}
