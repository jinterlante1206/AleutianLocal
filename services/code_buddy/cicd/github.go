// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cicd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/analysis"
)

// PRAnalysisResult contains the blast radius analysis for a pull request.
type PRAnalysisResult struct {
	// ChangedSymbols are the symbols directly modified in the PR.
	ChangedSymbols []SymbolAnalysis `json:"changed_symbols"`

	// TotalCallers is the total number of affected callers.
	TotalCallers int `json:"total_callers"`

	// RiskLevel is the overall risk level of the PR.
	RiskLevel RiskLevel `json:"risk_level"`

	// SecurityPaths indicates if security-sensitive code is affected.
	SecurityPaths []string `json:"security_paths,omitempty"`

	// RequiredActions are actions that must be taken before merge.
	RequiredActions []RequiredAction `json:"required_actions,omitempty"`

	// Recommendations are suggested actions.
	Recommendations []string `json:"recommendations,omitempty"`

	// Blocked indicates if the PR should be blocked.
	Blocked bool `json:"blocked"`

	// BlockReason explains why the PR is blocked.
	BlockReason string `json:"block_reason,omitempty"`
}

// SymbolAnalysis contains analysis for a single changed symbol.
type SymbolAnalysis struct {
	// SymbolID is the unique identifier.
	SymbolID string `json:"symbol_id"`

	// Name is the symbol name.
	Name string `json:"name"`

	// FilePath is the file containing the symbol.
	FilePath string `json:"file_path"`

	// CallerCount is the number of direct callers.
	CallerCount int `json:"caller_count"`

	// TransitiveCount is the number of transitive callers.
	TransitiveCount int `json:"transitive_count"`

	// RiskLevel is the risk level for this symbol.
	RiskLevel RiskLevel `json:"risk_level"`

	// SecurityPaths are security paths through this symbol.
	SecurityPaths []string `json:"security_paths,omitempty"`

	// Action is the recommended action.
	Action ActionType `json:"action"`
}

// RiskLevel represents the risk level of a change.
type RiskLevel string

const (
	RiskCritical RiskLevel = "CRITICAL"
	RiskHigh     RiskLevel = "HIGH"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskLow      RiskLevel = "LOW"
)

// ActionType represents the action to take for a symbol.
type ActionType string

const (
	ActionBlock  ActionType = "BLOCK"
	ActionReview ActionType = "REVIEW"
	ActionOK     ActionType = "OK"
)

// RequiredAction represents an action that must be taken.
type RequiredAction struct {
	// Description of the action.
	Description string `json:"description"`

	// Assignee is who should take the action (if known).
	Assignee string `json:"assignee,omitempty"`

	// Reason explains why this action is required.
	Reason string `json:"reason"`
}

// ThresholdConfig configures block/warn thresholds.
type ThresholdConfig struct {
	// BlockCallerThreshold blocks if any symbol has more than this many callers.
	// Default: 50
	BlockCallerThreshold int `json:"block_caller_threshold"`

	// WarnCallerThreshold warns if any symbol has more than this many callers.
	// Default: 20
	WarnCallerThreshold int `json:"warn_caller_threshold"`

	// BlockSecurityPaths blocks if security-sensitive paths are affected.
	// Default: true
	BlockSecurityPaths bool `json:"block_security_paths"`

	// RequireOwnerApproval requires CODEOWNERS approval for affected files.
	// Default: true
	RequireOwnerApproval bool `json:"require_owner_approval"`

	// MaxTransitiveDepth limits transitive analysis depth.
	// Default: 5
	MaxTransitiveDepth int `json:"max_transitive_depth"`
}

// DefaultThresholdConfig returns sensible defaults.
func DefaultThresholdConfig() ThresholdConfig {
	return ThresholdConfig{
		BlockCallerThreshold: 50,
		WarnCallerThreshold:  20,
		BlockSecurityPaths:   true,
		RequireOwnerApproval: true,
		MaxTransitiveDepth:   5,
	}
}

// GitHubIntegration provides GitHub Actions integration for blast radius.
type GitHubIntegration struct {
	thresholds ThresholdConfig
}

// NewGitHubIntegration creates a new GitHub integration.
func NewGitHubIntegration(thresholds *ThresholdConfig) *GitHubIntegration {
	if thresholds == nil {
		defaults := DefaultThresholdConfig()
		thresholds = &defaults
	}
	return &GitHubIntegration{
		thresholds: *thresholds,
	}
}

// AnalyzePR analyzes a pull request's blast radius.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - blastResults: Blast radius results for each changed symbol.
//
// # Outputs
//
//   - *PRAnalysisResult: Analysis result for the PR.
//   - error: Non-nil on failure.
func (g *GitHubIntegration) AnalyzePR(ctx context.Context, blastResults []*analysis.EnhancedBlastRadius) (*PRAnalysisResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	result := &PRAnalysisResult{
		ChangedSymbols: make([]SymbolAnalysis, 0, len(blastResults)),
	}

	var totalCallers int
	var maxRisk RiskLevel = RiskLow
	securityPathsSet := make(map[string]struct{})

	for _, br := range blastResults {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		sa := g.analyzeSymbol(br)
		result.ChangedSymbols = append(result.ChangedSymbols, sa)

		totalCallers += sa.CallerCount

		// Track max risk
		if riskGreater(sa.RiskLevel, maxRisk) {
			maxRisk = sa.RiskLevel
		}

		// Collect security paths
		for _, path := range sa.SecurityPaths {
			securityPathsSet[path] = struct{}{}
		}

		// Check block conditions
		if sa.Action == ActionBlock {
			result.Blocked = true
			if result.BlockReason == "" {
				result.BlockReason = fmt.Sprintf("Symbol %s has %d callers (threshold: %d)",
					sa.Name, sa.CallerCount, g.thresholds.BlockCallerThreshold)
			}
		}
	}

	result.TotalCallers = totalCallers
	result.RiskLevel = maxRisk

	// Convert security paths to slice
	for path := range securityPathsSet {
		result.SecurityPaths = append(result.SecurityPaths, path)
	}
	sort.Strings(result.SecurityPaths)

	// Check security path blocking
	if g.thresholds.BlockSecurityPaths && len(result.SecurityPaths) > 0 {
		result.Blocked = true
		if result.BlockReason == "" {
			result.BlockReason = "Security-sensitive code paths are affected"
		}
		result.RequiredActions = append(result.RequiredActions, RequiredAction{
			Description: "Security review required",
			Reason:      fmt.Sprintf("Affects security paths: %s", strings.Join(result.SecurityPaths, ", ")),
		})
	}

	// Add recommendations
	result.Recommendations = g.generateRecommendations(result)

	return result, nil
}

// analyzeSymbol creates analysis for a single symbol.
func (g *GitHubIntegration) analyzeSymbol(br *analysis.EnhancedBlastRadius) SymbolAnalysis {
	sa := SymbolAnalysis{
		SymbolID:        br.Target,
		Name:            br.Target, // Could extract just name
		CallerCount:     len(br.DirectCallers),
		TransitiveCount: br.TransitiveCount,
		Action:          ActionOK,
	}

	// Extract file path from symbol ID
	if idx := strings.LastIndex(br.Target, ":"); idx > 0 {
		sa.FilePath = br.Target[:idx]
		sa.Name = br.Target[idx+1:]
	}

	// Determine risk level and action
	if sa.CallerCount >= g.thresholds.BlockCallerThreshold {
		sa.RiskLevel = RiskCritical
		sa.Action = ActionBlock
	} else if sa.CallerCount >= g.thresholds.WarnCallerThreshold {
		sa.RiskLevel = RiskHigh
		sa.Action = ActionReview
	} else if sa.CallerCount >= 5 {
		sa.RiskLevel = RiskMedium
		sa.Action = ActionReview
	} else {
		sa.RiskLevel = RiskLow
		sa.Action = ActionOK
	}

	// Check security paths
	for _, sp := range br.SecurityPaths {
		sa.SecurityPaths = append(sa.SecurityPaths, sp.PathType)
		// Security paths increase risk
		if sa.RiskLevel == RiskLow {
			sa.RiskLevel = RiskMedium
		}
		if g.thresholds.BlockSecurityPaths {
			sa.Action = ActionReview
		}
	}

	return sa
}

// riskGreater returns true if a > b in risk level.
func riskGreater(a, b RiskLevel) bool {
	order := map[RiskLevel]int{
		RiskLow:      0,
		RiskMedium:   1,
		RiskHigh:     2,
		RiskCritical: 3,
	}
	return order[a] > order[b]
}

// generateRecommendations creates recommendations based on analysis.
func (g *GitHubIntegration) generateRecommendations(result *PRAnalysisResult) []string {
	var recs []string

	if result.TotalCallers > 100 {
		recs = append(recs, "Consider splitting this PR into smaller changes to reduce blast radius")
	}

	if len(result.SecurityPaths) > 0 {
		recs = append(recs, "Schedule security review before merge")
	}

	criticalCount := 0
	for _, sa := range result.ChangedSymbols {
		if sa.RiskLevel == RiskCritical {
			criticalCount++
		}
	}
	if criticalCount > 3 {
		recs = append(recs, fmt.Sprintf("%d critical symbols affected - consider incremental rollout", criticalCount))
	}

	return recs
}

// FormatPRComment generates a GitHub PR comment with blast radius summary.
//
// SECURITY: All symbol names are escaped in markdown code spans to prevent
// markdown injection attacks.
//
// # Inputs
//
//   - result: The PR analysis result.
//
// # Outputs
//
//   - string: Formatted markdown comment.
func (g *GitHubIntegration) FormatPRComment(result *PRAnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("## Blast Radius Analysis\n\n")

	// Summary table
	sb.WriteString("| Risk | Symbol | Callers | Security | Action |\n")
	sb.WriteString("|------|--------|---------|----------|--------|\n")

	// Sort by risk (highest first)
	symbols := make([]SymbolAnalysis, len(result.ChangedSymbols))
	copy(symbols, result.ChangedSymbols)
	sort.Slice(symbols, func(i, j int) bool {
		return riskGreater(symbols[i].RiskLevel, symbols[j].RiskLevel)
	})

	for _, sa := range symbols {
		securityStr := "-"
		if len(sa.SecurityPaths) > 0 {
			securityStr = strings.Join(sa.SecurityPaths, ", ")
		}

		// SECURITY: Escape symbol name in code span
		escapedName := escapeMarkdown(sa.Name)

		sb.WriteString(fmt.Sprintf("| %s | `%s` | %d | %s | %s |\n",
			sa.RiskLevel, escapedName, sa.CallerCount, securityStr, sa.Action))
	}

	sb.WriteString("\n")

	// Required actions
	if len(result.RequiredActions) > 0 {
		sb.WriteString("### Required Actions\n\n")
		for _, action := range result.RequiredActions {
			// SECURITY: Escape action descriptions
			escapedDesc := escapeMarkdown(action.Description)
			sb.WriteString(fmt.Sprintf("- [ ] %s", escapedDesc))
			if action.Reason != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", escapeMarkdown(action.Reason)))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Recommendations
	if len(result.Recommendations) > 0 {
		sb.WriteString("### Recommendations\n\n")
		for _, rec := range result.Recommendations {
			sb.WriteString(fmt.Sprintf("- %s\n", escapeMarkdown(rec)))
		}
		sb.WriteString("\n")
	}

	// Block status
	if result.Blocked {
		sb.WriteString("### ⛔ Blocked\n\n")
		sb.WriteString(fmt.Sprintf("**Reason:** %s\n\n", escapeMarkdown(result.BlockReason)))
	}

	sb.WriteString("---\n")
	sb.WriteString("*Generated by [Aleutian Trace](https://aleutian.ai)*\n")

	return sb.String()
}

// escapeMarkdown escapes special markdown characters to prevent injection.
func escapeMarkdown(s string) string {
	// Replace characters that could be used for markdown injection
	replacer := strings.NewReplacer(
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"#", "\\#",
		"|", "\\|",
		"<", "&lt;",
		">", "&gt;",
		"\n", " ",
	)
	return replacer.Replace(s)
}

// FormatGitLabComment generates a GitLab MR comment.
func (g *GitHubIntegration) FormatGitLabComment(result *PRAnalysisResult) string {
	// GitLab uses similar markdown, can reuse GitHub format
	return g.FormatPRComment(result)
}

// GenerateActionOutput generates GitHub Actions output variables.
//
// # Inputs
//
//   - result: The PR analysis result.
//
// # Outputs
//
//   - map[string]string: Output variables for GitHub Actions.
func (g *GitHubIntegration) GenerateActionOutput(result *PRAnalysisResult) map[string]string {
	outputs := make(map[string]string)

	outputs["risk_level"] = string(result.RiskLevel)
	outputs["total_callers"] = fmt.Sprintf("%d", result.TotalCallers)
	outputs["blocked"] = fmt.Sprintf("%t", result.Blocked)
	outputs["changed_symbols"] = fmt.Sprintf("%d", len(result.ChangedSymbols))

	if result.BlockReason != "" {
		outputs["block_reason"] = result.BlockReason
	}

	if len(result.SecurityPaths) > 0 {
		outputs["security_paths"] = strings.Join(result.SecurityPaths, ",")
	}

	return outputs
}

// CheckResult represents the result of a CI check.
type CheckResult struct {
	// Status is the check status.
	Status CheckStatus `json:"status"`

	// Title is the check title.
	Title string `json:"title"`

	// Summary is a brief summary.
	Summary string `json:"summary"`

	// Details is the full details.
	Details string `json:"details"`
}

// CheckStatus represents the status of a check.
type CheckStatus string

const (
	CheckStatusSuccess CheckStatus = "success"
	CheckStatusFailure CheckStatus = "failure"
	CheckStatusWarning CheckStatus = "warning"
)

// CreateCheckResult creates a check result from PR analysis.
func (g *GitHubIntegration) CreateCheckResult(result *PRAnalysisResult) CheckResult {
	check := CheckResult{
		Title:   "Blast Radius Analysis",
		Details: g.FormatPRComment(result),
	}

	if result.Blocked {
		check.Status = CheckStatusFailure
		check.Summary = fmt.Sprintf("Blocked: %s", result.BlockReason)
	} else if result.RiskLevel == RiskHigh || result.RiskLevel == RiskCritical {
		check.Status = CheckStatusWarning
		check.Summary = fmt.Sprintf("%s risk: %d callers affected", result.RiskLevel, result.TotalCallers)
	} else {
		check.Status = CheckStatusSuccess
		check.Summary = fmt.Sprintf("Low risk: %d callers affected", result.TotalCallers)
	}

	return check
}
