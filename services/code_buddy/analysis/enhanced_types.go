// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import "time"

// EnhancedBlastRadius extends BlastRadius with additional analysis dimensions.
//
// # Description
//
// Combines the core CB-17 blast radius result with enhanced analysis from
// CB-17b enrichers: security path detection, historical churn, ownership,
// change impact classification, and confidence scoring.
//
// # JSON Serialization
//
// All fields use omitempty where appropriate to minimize response size.
// Optional enricher fields are nil when the enricher didn't run or failed.
//
// # Thread Safety
//
// Not safe for concurrent modification. Create a new instance for each
// analysis result.
type EnhancedBlastRadius struct {
	// BlastRadius embeds the core CB-17 result.
	// This includes: Target, RiskLevel, DirectCallers, IndirectCallers,
	// Implementers, SharedDeps, FilesAffected, TestFiles, Summary, Recommendation.
	BlastRadius

	// SecurityPath contains security analysis results (single primary path).
	// Nil if security analysis didn't run or found nothing.
	SecurityPath *SecurityPath `json:"security_path,omitempty"`

	// SecurityPaths contains all detected security paths.
	// Used by CB-17c for multiple path detection.
	SecurityPaths []SecurityPath `json:"security_paths,omitempty"`

	// ChurnScore contains historical churn analysis (single primary).
	// Nil if git is unavailable or analysis failed.
	ChurnScore *ChurnScore `json:"churn_score,omitempty"`

	// ChurnScores contains all churn scores for affected files.
	// Used by CB-17c for comprehensive churn analysis.
	ChurnScores []ChurnScore `json:"churn_scores,omitempty"`

	// Ownership contains code ownership information.
	// Nil if CODEOWNERS is missing or analysis failed.
	Ownership *Ownership `json:"ownership,omitempty"`

	// ChangeImpacts describes potential impacts of different change types.
	// Empty if classification couldn't be performed.
	ChangeImpacts []ChangeImpact `json:"change_impacts"`

	// Confidence indicates analysis reliability.
	// Always populated (defaults to HIGH if no issues detected).
	Confidence *ConfidenceScore `json:"confidence,omitempty"`

	// EnricherResults tracks which enrichers ran successfully.
	// Useful for debugging partial results.
	EnricherResults []EnricherResult `json:"enricher_results,omitempty"`

	// AnalyzedAt is when this analysis was performed.
	AnalyzedAt time.Time `json:"analyzed_at"`

	// GraphGeneration is the graph version used for analysis.
	// Used for cache invalidation.
	GraphGeneration uint64 `json:"graph_generation"`

	// TransitiveCount is the total number of transitive callers.
	TransitiveCount int `json:"transitive_count,omitempty"`

	// --- CB-17c Sub-Ticket B: Dead Code & Coverage ---

	// DeadCode contains dead code analysis for this symbol.
	DeadCode *DeadCodeInfo `json:"dead_code,omitempty"`

	// Coverage contains test coverage information for this symbol.
	Coverage *CoverageInfo `json:"coverage,omitempty"`

	// UntestedCallers are callers that lack test coverage.
	UntestedCallers []Caller `json:"untested_callers,omitempty"`

	// CoverageRisk indicates the coverage risk level.
	// One of: "HIGH_UNTESTED", "PARTIAL", "COVERED".
	CoverageRisk string `json:"coverage_risk,omitempty"`

	// --- CB-17c Sub-Ticket C: Database & API Dependencies ---

	// SchemaDependencies are database schema dependencies.
	SchemaDependencies []SchemaDependency `json:"schema_dependencies,omitempty"`

	// APIDependencies are external API dependencies.
	APIDependencies []APIDependency `json:"api_dependencies,omitempty"`

	// ConfigDependencies are configuration dependencies.
	ConfigDependencies []ConfigDependency `json:"config_dependencies,omitempty"`

	// --- CB-17c Sub-Ticket G: Historical & Predictive ---

	// RollbackRisk assesses the difficulty of rolling back changes.
	RollbackRisk *RollbackRisk `json:"rollback_risk,omitempty"`

	// PredictiveRisk contains predictive risk scoring based on history.
	PredictiveRisk *PredictiveRisk `json:"predictive_risk,omitempty"`
}

// SecurityPath describes security-sensitive code paths.
//
// # Description
//
// Identifies whether the target symbol is in a security-sensitive path
// such as authentication, authorization, cryptography, or PII handling.
// When a symbol is in a security path, changes require extra scrutiny.
//
// # Path Types
//
//   - AUTH: Authentication (login, logout, token validation)
//   - AUTHZ: Authorization (permissions, roles, access control)
//   - PII: Personal identifiable information handling
//   - CRYPTO: Cryptographic operations
//   - SECRETS: Secret/credential handling
//
// # Thread Safety
//
// Read-only after construction.
type SecurityPath struct {
	// IsSecuritySensitive indicates the symbol is in a security path.
	IsSecuritySensitive bool `json:"is_security_sensitive"`

	// PathType identifies the type of security path.
	// One of: "AUTH", "AUTHZ", "PII", "CRYPTO", "SECRETS".
	// Empty string if not security sensitive.
	PathType string `json:"path_type,omitempty"`

	// Reason explains why this was classified as security-sensitive.
	// Human-readable explanation for agents.
	Reason string `json:"reason,omitempty"`

	// RequiresReview indicates whether changes need security review.
	RequiresReview bool `json:"requires_review"`

	// MatchedPatterns lists which patterns triggered detection.
	// Useful for debugging false positives.
	MatchedPatterns []string `json:"matched_patterns,omitempty"`

	// CallChainSecurity indicates the symbol is called BY security code.
	// A function may not look security-sensitive itself but is in the
	// security call chain.
	CallChainSecurity bool `json:"call_chain_security,omitempty"`
}

// ChurnScore describes historical code churn.
//
// # Description
//
// Analyzes git history to determine how often this code changes.
// High churn code is more likely to have bugs and may need extra
// attention during changes.
//
// # Churn Levels
//
//   - LOW: < 3 changes in 30 days (stable code)
//   - MODERATE: 3-9 changes in 30 days (active development)
//   - HIGH: 10+ changes in 30 days (hot code, potential instability)
//
// # Thread Safety
//
// Read-only after construction.
type ChurnScore struct {
	// ChangesLast30Days is the number of commits affecting this file
	// in the last 30 days.
	ChangesLast30Days int `json:"changes_last_30_days"`

	// ChangesLast90Days is the number of commits affecting this file
	// in the last 90 days.
	ChangesLast90Days int `json:"changes_last_90_days"`

	// BugReportsLinked is the count of commits with "fix", "bug", or
	// issue references (e.g., "#123") in the last 90 days.
	BugReportsLinked int `json:"bug_reports_linked"`

	// LastModified is when the file was last changed.
	LastModified time.Time `json:"last_modified"`

	// ChurnLevel categorizes the churn rate.
	// One of: "LOW", "MODERATE", "HIGH".
	ChurnLevel string `json:"churn_level"`

	// Contributors lists unique authors who modified this file recently.
	Contributors []string `json:"contributors,omitempty"`
}

// Ownership describes code ownership from CODEOWNERS.
//
// # Description
//
// Parses CODEOWNERS file to determine who owns the code and should
// review changes. Also identifies secondary owners from affected files.
//
// # Thread Safety
//
// Read-only after construction.
type Ownership struct {
	// PrimaryOwner is the team/person from CODEOWNERS matching the target file.
	// Format: "@team-name" or "@username" or "email@example.com".
	// Empty if no CODEOWNERS match.
	PrimaryOwner string `json:"primary_owner,omitempty"`

	// SecondaryOwners are owners of files in the blast radius.
	// These teams should be notified of changes.
	SecondaryOwners []string `json:"secondary_owners,omitempty"`

	// ReviewerHint suggests who should review based on recent activity.
	// May be a specific person who recently worked on this code.
	ReviewerHint string `json:"reviewer_hint,omitempty"`

	// OwnershipSource indicates where ownership was determined from.
	// One of: "CODEOWNERS", "git_blame", "fallback".
	OwnershipSource string `json:"ownership_source,omitempty"`
}

// ChangeImpact describes the potential impact of a change type.
//
// # Description
//
// Classifies different types of changes and their impact on callers.
// Helps agents understand whether they can make internal changes safely
// or need to update all callers.
//
// # Change Types for Functions
//
//   - add_param: Adding a parameter (BREAKING)
//   - remove_param: Removing a parameter (BREAKING)
//   - change_return_type: Changing return type (BREAKING)
//   - add_return_value: Adding return value (COMPATIBLE for Go)
//   - internal_logic: Changing implementation (SAFE)
//
// # Change Types for Types
//
//   - add_field: Adding struct field (COMPATIBLE)
//   - remove_field: Removing struct field (BREAKING)
//   - change_field_type: Changing field type (BREAKING)
//   - rename_field: Renaming field (BREAKING)
//
// # Change Types for Interfaces
//
//   - add_method: Adding interface method (BREAKING for implementers)
//   - remove_method: Removing interface method (BREAKING for callers)
//   - change_signature: Changing method signature (BREAKING)
//
// # Thread Safety
//
// Read-only after construction.
type ChangeImpact struct {
	// ChangeType identifies what kind of change this describes.
	ChangeType string `json:"change_type"`

	// Impact is the severity of the change.
	// One of: "BREAKING", "COMPATIBLE", "SAFE".
	Impact string `json:"impact"`

	// AffectedSites is the count of locations that would need updates.
	// For BREAKING changes, this is the number of callers/implementers.
	AffectedSites int `json:"affected_sites"`

	// Description explains the impact in human-readable terms.
	Description string `json:"description"`

	// Example provides a code example if applicable.
	Example string `json:"example,omitempty"`
}

// Change impact severity constants.
const (
	// ImpactBreaking means callers/implementers must be updated.
	ImpactBreaking = "BREAKING"

	// ImpactCompatible means existing code continues to work.
	ImpactCompatible = "COMPATIBLE"

	// ImpactSafe means no external changes needed.
	ImpactSafe = "SAFE"
)

// ConfidenceScore indicates analysis reliability.
//
// # Description
//
// Confidence is reduced when the analysis may be incomplete due to:
//   - Reflection/dynamic dispatch (can't trace all calls)
//   - Interface with external implementers
//   - Plugin/callback patterns
//   - Analysis truncation
//   - Enricher failures
//
// # Score Interpretation
//
//   - 90-100: HIGH confidence, complete analysis
//   - 70-89: MEDIUM confidence, some uncertainty
//   - 0-69: LOW confidence, significant gaps
//
// # Thread Safety
//
// Read-only after construction.
type ConfidenceScore struct {
	// Score is a percentage (0-100) indicating confidence.
	Score int `json:"score"`

	// Level categorizes the score.
	// One of: "HIGH", "MEDIUM", "LOW".
	Level string `json:"level"`

	// UncertaintyReasons explains why confidence was reduced.
	// Empty if confidence is HIGH.
	UncertaintyReasons []string `json:"uncertainty_reasons,omitempty"`
}

// Confidence level constants.
const (
	// ConfidenceHigh means >= 90% confidence.
	ConfidenceHigh = "HIGH"

	// ConfidenceMedium means 70-89% confidence.
	ConfidenceMedium = "MEDIUM"

	// ConfidenceLow means < 70% confidence.
	ConfidenceLow = "LOW"
)

// NewConfidenceScore creates a ConfidenceScore with the appropriate level.
func NewConfidenceScore(score int, reasons []string) ConfidenceScore {
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	var level string
	switch {
	case score >= 90:
		level = ConfidenceHigh
	case score >= 70:
		level = ConfidenceMedium
	default:
		level = ConfidenceLow
	}

	return ConfidenceScore{
		Score:              score,
		Level:              level,
		UncertaintyReasons: reasons,
	}
}

// SecurityPathType constants.
const (
	// SecurityPathAuth is for authentication code.
	SecurityPathAuth = "AUTH"

	// SecurityPathAuthz is for authorization code.
	SecurityPathAuthz = "AUTHZ"

	// SecurityPathPII is for PII handling code.
	SecurityPathPII = "PII"

	// SecurityPathCrypto is for cryptographic code.
	SecurityPathCrypto = "CRYPTO"

	// SecurityPathSecrets is for secret/credential handling.
	SecurityPathSecrets = "SECRETS"
)

// ChurnLevel constants.
const (
	// ChurnLevelLow is < 3 changes in 30 days.
	ChurnLevelLow = "LOW"

	// ChurnLevelModerate is 3-9 changes in 30 days.
	ChurnLevelModerate = "MODERATE"

	// ChurnLevelHigh is 10+ changes in 30 days.
	ChurnLevelHigh = "HIGH"
)

// GetChurnLevel returns the churn level for a given change count.
func GetChurnLevel(changesLast30Days int) string {
	switch {
	case changesLast30Days >= 10:
		return ChurnLevelHigh
	case changesLast30Days >= 3:
		return ChurnLevelModerate
	default:
		return ChurnLevelLow
	}
}
