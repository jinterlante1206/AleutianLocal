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
	"fmt"
	"regexp"
	"strings"
)

// EvidenceStrength indicates how well the available evidence supports a claim.
type EvidenceStrength int

const (
	// EvidenceAbsent means no tool searched for this topic.
	EvidenceAbsent EvidenceStrength = iota

	// EvidencePartial means tool searched but results were truncated/limited.
	EvidencePartial

	// EvidenceStrong means comprehensive tool search was performed.
	EvidenceStrong
)

// String returns a string representation of EvidenceStrength.
func (e EvidenceStrength) String() string {
	switch e {
	case EvidenceAbsent:
		return "absent"
	case EvidencePartial:
		return "partial"
	case EvidenceStrong:
		return "strong"
	default:
		return "unknown"
	}
}

// AbsoluteClaim represents an extracted absolute claim from the response.
type AbsoluteClaim struct {
	// Sentence is the full sentence containing the absolute language.
	Sentence string

	// ClaimType categorizes the absolute (absolute, negative_absolute, universal, exhaustive).
	ClaimType string

	// Keywords are the extracted topic keywords from the claim.
	Keywords []string

	// Position is the character offset in the response.
	Position int

	// MatchedPattern is the pattern that matched.
	MatchedPattern string
}

// Package-level compiled regexes for claim detection.
var (
	// absolutePattern matches absolute positive language.
	absolutePattern = regexp.MustCompile(
		`(?i)\b(always|never|all|none|every|no\s+one)\b`,
	)

	// strongAbsolutePattern matches emphatic absolute language.
	strongAbsolutePattern = regexp.MustCompile(
		`(?i)\b(definitely|certainly|absolutely|guaranteed|impossible|without\s+exception)\b`,
	)

	// negativeAbsolutePattern matches claims of absence.
	negativeAbsolutePattern = regexp.MustCompile(
		`(?i)(there\s+is\s+no\s|does\s*n[o']?t\s+exist|no\s+\w+\s+exists?|never\s+\w+\s+any|doesn't\s+exist)`,
	)

	// exhaustivePattern matches exhaustive/exclusive claims.
	exhaustivePattern = regexp.MustCompile(
		`(?i)\b(the\s+only|exclusively|solely|complete\s+list|every\s+single|all\s+of\s+the)\b`,
	)

	// hedgingPattern matches hedging language that negates confidence concerns.
	hedgingPattern = regexp.MustCompile(
		`(?i)\b(appears?\s+(to|that)|seems?\s+(to|like|that)|may\b|might\b|could\b|possibly|probably|likely|unlikely)\b`,
	)

	// hedgingScopePattern matches scope-limiting phrases.
	hedgingScopePattern = regexp.MustCompile(
		`(?i)(based\s+on\s+(what\s+I\s+found|the\s+files)|from\s+(what\s+I\s+can\s+see|the\s+code)|I('m|\s+am)\s+not\s+certain|I\s+couldn't\s+confirm|unclear|I\s+didn't\s+find|but\s+there\s+may\s+be)`,
	)

	// truncationPattern matches truncation indicators in tool output.
	truncationPattern = regexp.MustCompile(
		`(?i)(showing\s+first|truncated|and\s+\d+\s+more|results\s+limited|output\s+trimmed|\(only\s+showing)`,
	)

	// tautologyPattern matches definitionally true claims.
	tautologyPattern = regexp.MustCompile(
		`(?i)(\.go\s+(files?|extension|have)|\.py\s+(files?|extension|have)|by\s+definition|\.go\s+extension)`,
	)

	// confidenceCodeBlockPattern matches markdown code blocks.
	confidenceCodeBlockPattern = regexp.MustCompile("(?s)```.*?```")

	// confidenceInlineCodePattern matches inline code.
	confidenceInlineCodePattern = regexp.MustCompile("`[^`]+`")

	// sentencePattern extracts sentences, being careful with file extensions.
	// Looks for period followed by space or end, or ! or ?.
	sentencePattern = regexp.MustCompile(`[^.!?\n]+(?:\.[a-zA-Z]{1,5})*[^.!?\n]*[.!?](?:\s|$)`)

	// quickCheckPattern is a fast check for any absolute-related keywords.
	quickCheckPattern = regexp.MustCompile(
		`(?i)(\balways\b|\bnever\b|\ball\b|\bnone\b|\bevery\b|\bdefinitely\b|\bcertainly\b|\babsolutely\b|\bguaranteed\b|\bimpossible\b|the\s+only|exclusively|no\s+\w+\s+exists?|there\s+is\s+no)`,
	)
)

// ConfidenceChecker validates that confident claims are supported by evidence.
//
// This checker detects:
// - Absolute claims ("always", "never", "all", "none") without evidence
// - Negative claims ("there is no X") without comprehensive search
// - Exhaustive claims ("the only way") without full exploration
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type ConfidenceChecker struct {
	config *ConfidenceCheckerConfig
}

// NewConfidenceChecker creates a new confidence fabrication checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*ConfidenceChecker - The configured checker.
func NewConfidenceChecker(config *ConfidenceCheckerConfig) *ConfidenceChecker {
	if config == nil {
		config = DefaultConfidenceCheckerConfig()
	}
	return &ConfidenceChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *ConfidenceChecker) Name() string {
	return "confidence_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts absolute/confident claims from the response and validates them
//	against the evidence strength in tool results. Claims with insufficient
//	evidence generate violations.
//
// Thread Safety: Safe for concurrent use.
func (c *ConfidenceChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Quick check: does response contain any absolute language?
	if !quickCheckPattern.MatchString(input.Response) {
		return nil
	}

	var violations []Violation

	// Remove code blocks from consideration if configured
	response := input.Response
	if c.config.SkipCodeBlocks {
		response = confidenceCodeBlockPattern.ReplaceAllString(response, " ")
		response = confidenceInlineCodePattern.ReplaceAllString(response, " ")
	}

	// Extract absolute claims
	claims := c.extractAbsoluteClaims(response)

	// Limit claims to check
	if c.config.MaxClaimsToCheck > 0 && len(claims) > c.config.MaxClaimsToCheck {
		claims = claims[:c.config.MaxClaimsToCheck]
	}

	// Validate each claim
	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		// Skip hedged claims
		if c.config.AllowHedgedAbsolutes && c.isHedged(claim.Sentence) {
			continue
		}

		// Skip tautologies
		if c.config.SkipTautologies && c.isTautology(claim.Sentence) {
			continue
		}

		// Assess evidence strength
		strength := c.assessEvidenceStrength(claim, input.ToolResults)

		// Check for mismatch
		if v := c.checkMismatch(ctx, claim, strength); v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// extractAbsoluteClaims extracts all absolute claims from the response.
func (c *ConfidenceChecker) extractAbsoluteClaims(response string) []AbsoluteClaim {
	var claims []AbsoluteClaim
	seen := make(map[string]bool) // Dedup by sentence

	// Extract sentences and check each for absolute language
	sentences := sentencePattern.FindAllStringIndex(response, -1)

	for _, idx := range sentences {
		sentence := response[idx[0]:idx[1]]
		sentenceLower := strings.ToLower(sentence)

		// Skip already seen sentences
		if seen[sentenceLower] {
			continue
		}

		// Check for different types of absolute claims
		if match := exhaustivePattern.FindString(sentence); match != "" {
			seen[sentenceLower] = true
			claims = append(claims, AbsoluteClaim{
				Sentence:       strings.TrimSpace(sentence),
				ClaimType:      "exhaustive",
				Keywords:       extractConfidenceKeywords(sentence),
				Position:       idx[0],
				MatchedPattern: match,
			})
		} else if match := negativeAbsolutePattern.FindString(sentence); match != "" {
			seen[sentenceLower] = true
			claims = append(claims, AbsoluteClaim{
				Sentence:       strings.TrimSpace(sentence),
				ClaimType:      "negative_absolute",
				Keywords:       extractConfidenceKeywords(sentence),
				Position:       idx[0],
				MatchedPattern: match,
			})
		} else if match := strongAbsolutePattern.FindString(sentence); match != "" {
			seen[sentenceLower] = true
			claims = append(claims, AbsoluteClaim{
				Sentence:       strings.TrimSpace(sentence),
				ClaimType:      "strong_absolute",
				Keywords:       extractConfidenceKeywords(sentence),
				Position:       idx[0],
				MatchedPattern: match,
			})
		} else if match := absolutePattern.FindString(sentence); match != "" {
			seen[sentenceLower] = true
			claims = append(claims, AbsoluteClaim{
				Sentence:       strings.TrimSpace(sentence),
				ClaimType:      "absolute",
				Keywords:       extractConfidenceKeywords(sentence),
				Position:       idx[0],
				MatchedPattern: match,
			})
		}
	}

	return claims
}

// isHedged returns true if the sentence contains hedging language.
func (c *ConfidenceChecker) isHedged(sentence string) bool {
	return hedgingPattern.MatchString(sentence) || hedgingScopePattern.MatchString(sentence)
}

// isTautology returns true if the claim is definitionally true.
func (c *ConfidenceChecker) isTautology(sentence string) bool {
	return tautologyPattern.MatchString(sentence)
}

// assessEvidenceStrength determines how well evidence supports the claim.
func (c *ConfidenceChecker) assessEvidenceStrength(claim AbsoluteClaim, toolResults []ToolResult) EvidenceStrength {
	if len(toolResults) == 0 {
		return EvidenceAbsent
	}

	keywords := claim.Keywords
	if len(keywords) == 0 {
		// If no keywords extracted, assume absent (conservative)
		return EvidenceAbsent
	}

	topicSearched := false
	resultsComplete := true

	for _, tr := range toolResults {
		// Check if tool output relates to the claim topic
		if hasKeywordOverlap(tr.Output, keywords) {
			topicSearched = true
			// Check for truncation
			if truncationPattern.MatchString(tr.Output) {
				resultsComplete = false
			}
		}
	}

	if !topicSearched {
		return EvidenceAbsent
	}
	if !resultsComplete {
		return EvidencePartial
	}
	return EvidenceStrong
}

// checkMismatch returns a violation if confidence exceeds evidence.
func (c *ConfidenceChecker) checkMismatch(ctx context.Context, claim AbsoluteClaim, strength EvidenceStrength) *Violation {
	// Strong evidence supports confident claims
	if strength == EvidenceStrong {
		return nil
	}

	var severity Severity
	switch strength {
	case EvidenceAbsent:
		severity = c.config.AbsentEvidenceSeverity
	case EvidencePartial:
		severity = c.config.PartialEvidenceSeverity
	default:
		return nil
	}

	// Record metric
	RecordConfidenceFabrication(ctx, claim.ClaimType, strength.String(), severity)

	var message string
	var suggestion string

	switch strength {
	case EvidenceAbsent:
		message = fmt.Sprintf("Absolute claim '%s' made without evidence to support it", truncateClaim(claim.MatchedPattern, 30))
		suggestion = "Search for relevant evidence before making absolute claims, or use hedging language like 'appears to' or 'based on what I found'"
	case EvidencePartial:
		message = fmt.Sprintf("Absolute claim '%s' made with only partial/truncated evidence", truncateClaim(claim.MatchedPattern, 30))
		suggestion = "Acknowledge that the search was limited, or perform a more comprehensive search"
	}

	return &Violation{
		Type:           ViolationConfidenceFabrication,
		Severity:       severity,
		Code:           "CONFIDENCE_" + strings.ToUpper(strength.String()),
		Message:        message,
		Evidence:       truncateClaim(claim.Sentence, 100),
		Expected:       "Evidence-supported claim or hedged language",
		Suggestion:     suggestion,
		LocationOffset: claim.Position,
	}
}

// extractConfidenceKeywords extracts meaningful keywords from a sentence.
func extractConfidenceKeywords(sentence string) []string {
	// Common words to ignore
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
		"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
		"should": true, "may": true, "might": true, "must": true, "shall": true,
		"this": true, "that": true, "these": true, "those": true, "it": true,
		"and": true, "or": true, "but": true, "if": true, "then": true, "else": true,
		"for": true, "of": true, "to": true, "from": true, "in": true, "on": true, "at": true,
		"by": true, "with": true, "as": true, "into": true, "through": true,
		"all": true, "every": true, "any": true, "no": true, "none": true, "some": true,
		"always": true, "never": true, "definitely": true, "certainly": true,
		"there": true, "here": true, "when": true, "where": true, "what": true, "which": true,
		"who": true, "how": true, "why": true,
	}

	// Extract words
	wordPattern := regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`)
	matches := wordPattern.FindAllString(sentence, -1)

	var keywords []string
	seen := make(map[string]bool)

	for _, word := range matches {
		lower := strings.ToLower(word)
		// Skip stop words and short words
		if stopWords[lower] || len(word) < 3 {
			continue
		}
		// Skip duplicates
		if seen[lower] {
			continue
		}
		seen[lower] = true
		keywords = append(keywords, lower)
	}

	return keywords
}

// hasKeywordOverlap checks if text contains any of the keywords.
// Uses both exact and stem-like matching (e.g., "input" matches "inputs").
func hasKeywordOverlap(text string, keywords []string) bool {
	textLower := strings.ToLower(text)
	matchCount := 0

	for _, kw := range keywords {
		// Exact substring match
		if strings.Contains(textLower, kw) {
			matchCount++
			continue
		}
		// Try stem-like matching: if keyword is plural, check singular
		if len(kw) > 3 && kw[len(kw)-1] == 's' {
			stem := kw[:len(kw)-1]
			if strings.Contains(textLower, stem) {
				matchCount++
				continue
			}
		}
		// Try stem-like: if keyword ends in "ed" or "ing", check root
		if len(kw) > 4 && strings.HasSuffix(kw, "ed") {
			stem := kw[:len(kw)-2]
			if strings.Contains(textLower, stem) {
				matchCount++
				continue
			}
		}
		if len(kw) > 4 && strings.HasSuffix(kw, "ing") {
			stem := kw[:len(kw)-3]
			if len(stem) > 2 && strings.Contains(textLower, stem) {
				matchCount++
				continue
			}
		}
	}

	// Require at least 1 keyword match, or 2 if we have many keywords
	if len(keywords) >= 4 {
		return matchCount >= 2
	}
	return matchCount >= 1
}

// truncateClaim truncates a claim for display.
func truncateClaim(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
