// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"regexp"
)

// TemporalClaimCategory categorizes types of temporal claims.
type TemporalClaimCategory int

const (
	// ClaimCategoryRecency is for "recently", "just", "new", "latest" claims.
	ClaimCategoryRecency TemporalClaimCategory = iota

	// ClaimCategoryHistorical is for "was", "used to", "originally" claims.
	ClaimCategoryHistorical

	// ClaimCategoryVersion is for "v1.0", "version 2", "since" claims.
	ClaimCategoryVersion

	// ClaimCategoryReason is for "because", "due to", "in order to" claims.
	ClaimCategoryReason
)

// String returns the string representation of TemporalClaimCategory.
func (c TemporalClaimCategory) String() string {
	switch c {
	case ClaimCategoryRecency:
		return "recency"
	case ClaimCategoryHistorical:
		return "historical"
	case ClaimCategoryVersion:
		return "version"
	case ClaimCategoryReason:
		return "reason"
	default:
		return "unknown"
	}
}

// TemporalClaim represents a detected temporal claim in the response.
type TemporalClaim struct {
	// Category is the type of temporal claim.
	Category TemporalClaimCategory

	// Position is the character offset in the response.
	Position int

	// Raw is the matched text.
	Raw string
}

// Package-level compiled regexes for temporal claim detection.
var (
	// temporalQuickCheck is a fast check for any temporal keywords.
	// Used to short-circuit responses without temporal content.
	temporalQuickCheck = regexp.MustCompile(
		`(?i)\b(recent|new|just|latest|last|original|previous|version|v\d|since|used\s+to|was\s+changed|refactor|update|add|deprecat|earlier|formerly)`,
	)

	// recencyPattern matches recency claims.
	// "recently added", "just updated", "newly introduced", "latest version"
	recencyPattern = regexp.MustCompile(
		`(?i)\b(recently|just|newly|latest)\s+(added|updated|changed|modified|refactored|introduced|created|implemented|released)\b`,
	)

	// lastCommitPattern matches "in the last commit/version/update" patterns.
	lastCommitPattern = regexp.MustCompile(
		`(?i)\b(?:in|during)\s+(?:the\s+)?last\s+(commit|version|release|update|change)\b`,
	)

	// historicalPattern matches historical claims.
	// "was originally", "used to be", "previously", "formerly"
	historicalPattern = regexp.MustCompile(
		`(?i)\b(was\s+originally|used\s+to\s+be|used\s+to\s+have|previously|formerly|originally\s+(?:designed|implemented|created|written))\b`,
	)

	// earlierVersionPattern matches "in earlier versions" patterns.
	earlierVersionPattern = regexp.MustCompile(
		`(?i)\bin\s+(earlier|previous|older)\s+versions?\b`,
	)

	// versionPattern matches version claims.
	// "added in v1.0", "since version 2", "deprecated in 3.0"
	versionPattern = regexp.MustCompile(
		`(?i)\b(added|introduced|deprecated|removed|changed)\s+in\s+(v(?:ersion)?\s*)?(\d+(?:\.\d+)*)\b`,
	)

	// sinceVersionPattern matches "since v1.0", "as of version 2".
	sinceVersionPattern = regexp.MustCompile(
		`(?i)\b(since|as\s+of|from)\s+(v(?:ersion)?\s*)?(\d+(?:\.\d+)*)\b`,
	)

	// reasonPattern matches reason claims.
	// "was changed because", "refactored to improve", "updated due to", "change was made because"
	reasonPattern = regexp.MustCompile(
		`(?i)\b((?:was\s+)?(?:changed|updated|refactored|modified)|change\s+was\s+made|refactored|updated|changed)\s+(because|due\s+to|in\s+order\s+to|to\s+(?:improve|fix|address|resolve))\b`,
	)

	// commitReasonPattern matches commit-style reason claims.
	commitReasonPattern = regexp.MustCompile(
		`(?i)\bcommit(?:ted)?\s+(?:to\s+)?(?:fix|address|resolve|improve)\b`,
	)

	// codeBlockPattern matches markdown code blocks to skip.
	codeBlockPattern = regexp.MustCompile("(?s)```[^`]*```|`[^`]+`")

	// gitEvidencePatterns detect git command output in tool results.
	gitEvidencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`commit [0-9a-f]{40}`),     // git log commit hash
		regexp.MustCompile(`Author:\s+.+`),            // git log author
		regexp.MustCompile(`Date:\s+.+`),              // git log date
		regexp.MustCompile(`(?i)git\s+log\b`),         // git log command
		regexp.MustCompile(`(?i)git\s+show\b`),        // git show command
		regexp.MustCompile(`(?i)git\s+diff\b`),        // git diff command
		regexp.MustCompile(`(?i)git\s+blame\b`),       // git blame command
		regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}`), // ISO timestamp in git output
	}
)

// TemporalChecker validates temporal claims in responses.
//
// This checker detects:
// - Recency claims ("recently added", "just updated")
// - Historical claims ("was originally", "used to be")
// - Version claims ("added in v1.0", "since version 2")
// - Reason claims ("changed because", "refactored to improve")
//
// Without git evidence, these claims are flagged as unverifiable.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type TemporalChecker struct {
	config *TemporalCheckerConfig
}

// NewTemporalChecker creates a new temporal hallucination checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*TemporalChecker - The configured checker.
func NewTemporalChecker(config *TemporalCheckerConfig) *TemporalChecker {
	if config == nil {
		config = DefaultTemporalCheckerConfig()
	}
	return &TemporalChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *TemporalChecker) Name() string {
	return "temporal_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts temporal claims from the response and flags them as unverifiable
//	when no git evidence is available. This is not about proving claims wrong,
//	but about flagging confident assertions about code history that cannot be
//	verified from the available evidence.
//
// Thread Safety: Safe for concurrent use.
func (c *TemporalChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	response := input.Response

	// Quick check - skip if no temporal keywords in first 1000 chars
	checkLen := len(response)
	if checkLen > 1000 {
		checkLen = 1000
	}
	if !temporalQuickCheck.MatchString(response[:checkLen]) {
		return nil
	}

	// Check if we have git evidence
	hasGit := c.hasGitEvidence(input.ToolResults)

	// If we have git evidence, we could potentially validate claims,
	// but this is complex and error-prone. For now, we skip flagging
	// when git evidence is available (benefit of the doubt).
	// Future enhancement: validate specific claims against git output.
	if hasGit {
		return nil
	}

	// Optionally remove code blocks from consideration
	checkText := response
	if c.config.SkipCodeBlocks {
		checkText = codeBlockPattern.ReplaceAllString(response, "")
	}

	// Extract temporal claims
	claims := c.extractTemporalClaims(checkText)

	// Limit claims to check
	if c.config.MaxClaimsToCheck > 0 && len(claims) > c.config.MaxClaimsToCheck {
		claims = claims[:c.config.MaxClaimsToCheck]
	}

	var violations []Violation

	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		// Determine severity based on config
		severity := SeverityInfo
		if c.config.StrictMode {
			severity = SeverityWarning
		}

		// Record metric
		RecordTemporalHallucination(ctx, claim.Category.String(), false)

		violations = append(violations, Violation{
			Type:           ViolationTemporalHallucination,
			Severity:       severity,
			Code:           c.codeForCategory(claim.Category),
			Message:        c.messageForClaim(claim),
			Evidence:       claim.Raw,
			Expected:       "Claims verifiable from available evidence",
			Suggestion:     "Avoid making claims about code history without access to git history",
			LocationOffset: claim.Position,
		})
	}

	return violations
}

// hasGitEvidence checks if tool results contain git command output.
func (c *TemporalChecker) hasGitEvidence(toolResults []ToolResult) bool {
	for _, r := range toolResults {
		for _, pattern := range gitEvidencePatterns {
			if pattern.MatchString(r.Output) {
				return true
			}
		}
	}
	return false
}

// extractTemporalClaims extracts all temporal claims from the response.
func (c *TemporalChecker) extractTemporalClaims(response string) []TemporalClaim {
	var claims []TemporalClaim
	seen := make(map[int]bool) // Dedup by position

	// Extract recency claims
	if c.config.CheckRecencyClaims {
		claims = c.extractPatternClaims(response, recencyPattern, ClaimCategoryRecency, claims, seen)
		claims = c.extractPatternClaims(response, lastCommitPattern, ClaimCategoryRecency, claims, seen)
	}

	// Extract historical claims
	if c.config.CheckHistoricalClaims {
		claims = c.extractPatternClaims(response, historicalPattern, ClaimCategoryHistorical, claims, seen)
		claims = c.extractPatternClaims(response, earlierVersionPattern, ClaimCategoryHistorical, claims, seen)
	}

	// Extract version claims
	if c.config.CheckVersionClaims {
		claims = c.extractPatternClaims(response, versionPattern, ClaimCategoryVersion, claims, seen)
		claims = c.extractPatternClaims(response, sinceVersionPattern, ClaimCategoryVersion, claims, seen)
	}

	// Extract reason claims
	if c.config.CheckReasonClaims {
		claims = c.extractPatternClaims(response, reasonPattern, ClaimCategoryReason, claims, seen)
		claims = c.extractPatternClaims(response, commitReasonPattern, ClaimCategoryReason, claims, seen)
	}

	return claims
}

// extractPatternClaims extracts claims matching a pattern.
func (c *TemporalChecker) extractPatternClaims(
	response string,
	pattern *regexp.Regexp,
	category TemporalClaimCategory,
	claims []TemporalClaim,
	seen map[int]bool,
) []TemporalClaim {
	matches := pattern.FindAllStringIndex(response, -1)
	for _, match := range matches {
		pos := match[0]
		// Skip if we already have a claim at this position
		if seen[pos] {
			continue
		}
		seen[pos] = true

		raw := response[match[0]:match[1]]
		claims = append(claims, TemporalClaim{
			Category: category,
			Position: pos,
			Raw:      raw,
		})
	}
	return claims
}

// codeForCategory returns the violation code for a claim category.
func (c *TemporalChecker) codeForCategory(category TemporalClaimCategory) string {
	switch category {
	case ClaimCategoryRecency:
		return "TEMPORAL_RECENCY_UNVERIFIABLE"
	case ClaimCategoryHistorical:
		return "TEMPORAL_HISTORICAL_UNVERIFIABLE"
	case ClaimCategoryVersion:
		return "TEMPORAL_VERSION_UNVERIFIABLE"
	case ClaimCategoryReason:
		return "TEMPORAL_REASON_UNVERIFIABLE"
	default:
		return "TEMPORAL_CLAIM_UNVERIFIABLE"
	}
}

// messageForClaim returns a human-readable message for a claim.
func (c *TemporalChecker) messageForClaim(claim TemporalClaim) string {
	switch claim.Category {
	case ClaimCategoryRecency:
		return "Recency claim cannot be verified without git history"
	case ClaimCategoryHistorical:
		return "Historical claim cannot be verified without git history"
	case ClaimCategoryVersion:
		return "Version claim cannot be verified without git tags/history"
	case ClaimCategoryReason:
		return "Reason for change cannot be verified without commit messages"
	default:
		return "Temporal claim cannot be verified without git history"
	}
}

// stripCodeBlocks removes code blocks from text.
// Exported for testing.
func StripCodeBlocks(text string) string {
	return codeBlockPattern.ReplaceAllString(text, "")
}
