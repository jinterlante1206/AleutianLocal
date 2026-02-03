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

// BehavioralClaimCategory categorizes types of behavioral claims.
type BehavioralClaimCategory int

const (
	// ClaimErrorHandling is a claim about error handling behavior.
	ClaimErrorHandling BehavioralClaimCategory = iota

	// ClaimValidation is a claim about input validation behavior.
	ClaimValidation

	// ClaimSecurity is a claim about security behavior.
	ClaimSecurity
)

// String returns the string representation of a BehavioralClaimCategory.
func (c BehavioralClaimCategory) String() string {
	switch c {
	case ClaimErrorHandling:
		return "error_handling"
	case ClaimValidation:
		return "validation"
	case ClaimSecurity:
		return "security"
	default:
		return "unknown"
	}
}

// BehavioralClaim represents a parsed behavioral claim from response text.
type BehavioralClaim struct {
	// Subject is the function/component the claim is about.
	Subject string

	// Behavior is the claimed behavior.
	Behavior string

	// Category is the type of behavioral claim.
	Category BehavioralClaimCategory

	// Position is the character offset in the response.
	Position int

	// Raw is the original matched text.
	Raw string
}

// Package-level compiled regexes for behavioral claim extraction.
var (
	// Error handling claim patterns.
	// Match: "FunctionName logs/returns/handles errors"
	// Match: "FunctionName handles errors gracefully"
	errorHandlingClaimPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+(?:` +
			`logs?\s+(?:the\s+)?errors?|` +
			`returns?\s+(?:the\s+)?errors?\s+to|` +
			`handles?\s+(?:the\s+)?errors?\s+gracefully|` +
			`handles?\s+(?:all\s+)?errors?|` +
			`propagates?\s+errors?|` +
			`wraps?\s+(?:the\s+)?errors?` +
			`)\b`,
	)

	// Validation claim patterns.
	// Match: "FunctionName validates/sanitizes input"
	validationClaimPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+(?:` +
			`validates?\s+(?:the\s+)?(?:input|data|parameters?|user\s+input)|` +
			`sanitizes?\s+(?:the\s+)?(?:input|data|user\s+input)|` +
			`checks?\s+(?:the\s+)?(?:input|parameters?)|` +
			`verifies?\s+(?:the\s+)?(?:input|data)` +
			`)\b`,
	)

	// Security claim patterns.
	// Match: "FunctionName encrypts/authenticates/authorizes"
	securityClaimPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+(?:` +
			`encrypts?\s+(?:the\s+)?(?:data|password|credentials?)?|` +
			`authenticates?\s+(?:the\s+)?(?:user|request|client)?|` +
			`authorizes?\s+(?:the\s+)?(?:access|user|request)?|` +
			`hashes?\s+(?:the\s+)?(?:password|credentials?)?` +
			`)\b`,
	)

	// Evidence patterns for error handling - if any match, claim is supported.
	errorHandlingEvidencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)log\.(Error|Warn|Info)\s*\(`),
		regexp.MustCompile(`return\s+.*err\b`),
		regexp.MustCompile(`errors\.(Wrap|New|Errorf)\s*\(`),
		regexp.MustCompile(`fmt\.Errorf\s*\(`),
		regexp.MustCompile(`if\s+err\s*!=\s*nil\s*\{`),
		regexp.MustCompile(`logger\.(Error|Warn)\s*\(`),
		regexp.MustCompile(`slog\.(Error|Warn)\s*\(`),
	}

	// Counter-evidence patterns for error handling - contradicts claims.
	errorHandlingCounterPatterns = []*regexp.Regexp{
		regexp.MustCompile(`_\s*=\s*err\b`),            // _ = err - ignoring error
		regexp.MustCompile(`_\s*=\s*\w+\.\w+\(`),       // _ = method() - ignoring return
		regexp.MustCompile(`catch\s*\{\s*\}`),          // empty catch block
		regexp.MustCompile(`except:\s*pass`),           // Python bare except pass
		regexp.MustCompile(`\.catch\s*\(\s*\(\)\s*=>`), // JS empty catch
		regexp.MustCompile(`(?i)//\s*TODO.*error`),     // TODO comment about errors
		regexp.MustCompile(`(?i)//\s*FIXME.*error`),    // FIXME comment about errors
	}

	// Evidence patterns for validation - if any match, claim is supported.
	validationEvidencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`if\s+len\s*\(`),                // length check
		regexp.MustCompile(`if\s+\w+\s*==\s*""`),           // empty string check
		regexp.MustCompile(`if\s+\w+\s*==\s*nil`),          // nil check
		regexp.MustCompile(`regexp?\.(Match|MustCompile)`), // regex validation
		regexp.MustCompile(`validator\.(Validate|Struct)`), // validator library
		regexp.MustCompile(`\.Validate\s*\(`),              // generic Validate call
		regexp.MustCompile(`strings\.TrimSpace`),           // sanitization
		regexp.MustCompile(`html\.EscapeString`),           // HTML sanitization
		regexp.MustCompile(`url\.QueryEscape`),             // URL sanitization
	}

	// Counter-evidence patterns for validation - contradicts claims.
	validationCounterPatterns = []*regexp.Regexp{
		// These patterns suggest direct use without validation
		// We look for these in specific contexts (commented out - too broad)
	}

	// Evidence patterns for security - if any match, claim is supported.
	securityEvidencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`crypto\.\w+`),      // crypto package
		regexp.MustCompile(`bcrypt\.\w+`),      // bcrypt usage
		regexp.MustCompile(`sha256\.\w+`),      // SHA256 hashing
		regexp.MustCompile(`aes\.\w+`),         // AES encryption
		regexp.MustCompile(`jwt\.\w+`),         // JWT tokens
		regexp.MustCompile(`oauth\w*\.\w+`),    // OAuth
		regexp.MustCompile(`\.Hash\s*\(`),      // Hash method
		regexp.MustCompile(`\.Encrypt\s*\(`),   // Encrypt method
		regexp.MustCompile(`password.*hash`),   // password hashing
		regexp.MustCompile(`auth.*middleware`), // auth middleware
	}

	// Counter-evidence patterns for security - contradicts claims.
	securityCounterPatterns = []*regexp.Regexp{
		regexp.MustCompile(`password\s*=\s*["\']`),   // hardcoded password
		regexp.MustCompile(`secret\s*=\s*["\']`),     // hardcoded secret
		regexp.MustCompile(`plaintext`),              // plaintext mention
		regexp.MustCompile(`(?i)//\s*TODO.*encrypt`), // TODO about encryption
		regexp.MustCompile(`(?i)//\s*FIXME.*auth`),   // FIXME about auth
	}

	// Common words to filter out as subjects.
	// Note: Patterns use (?i) case-insensitive mode, so these are matched case-insensitively.
	// Keys should be in the exact case they appear when captured (typically first-letter uppercase
	// for sentence starts, but the lookup needs to handle both).
	behavioralCommonWords = map[string]bool{
		// Articles and pronouns
		"The": true, "A": true, "An": true, "It": true, "Its": true,
		"This": true, "That": true, "These": true, "Those": true,
		// Conjunctions and prepositions (case-insensitive patterns may capture these)
		"And": true, "Or": true, "But": true, "If": true, "When": true,
		"Then": true, "Also": true, "Which": true, "Where": true,
		// Quantifiers
		"Each": true, "Every": true, "All": true, "Any": true, "Some": true,
		"No": true, "None": true, "Both": true, "Either": true,
		// Code-related generic terms
		"Function": true, "Method": true, "Handler": true, "Code": true,
		"Module": true, "Package": true, "Class": true, "Object": true,
		"Service": true, "Component": true, "System": true,
	}
)

// isBehavioralCommonWord checks if a word is a common English word that should not be
// treated as a function name. The check is case-insensitive.
func isBehavioralCommonWord(word string) bool {
	// Normalize to title case for lookup (first letter uppercase, rest lowercase)
	// This matches how words appear in the behavioralCommonWords map
	if len(word) == 0 {
		return false
	}
	normalized := strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	return behavioralCommonWords[normalized]
}

// BehavioralChecker validates behavioral claims in responses.
//
// This checker detects fabricated behavioral claims including:
// - Error handling claims with counter-evidence (swallowed errors)
// - Validation claims with counter-evidence (no validation visible)
// - Security claims with counter-evidence (plaintext storage)
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type BehavioralChecker struct {
	config *BehavioralCheckerConfig
}

// NewBehavioralChecker creates a new behavioral checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*BehavioralChecker - The configured checker.
func NewBehavioralChecker(config *BehavioralCheckerConfig) *BehavioralChecker {
	if config == nil {
		config = DefaultBehavioralCheckerConfig()
	}
	return &BehavioralChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *BehavioralChecker) Name() string {
	return "behavioral_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts behavioral claims from the response and validates them against
//	the EvidenceIndex. Detects fabricated behavioral claims by looking for
//	counter-evidence patterns in the code.
//
// Thread Safety: Safe for concurrent use.
func (c *BehavioralChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Need evidence index to validate
	if input.EvidenceIndex == nil {
		return nil
	}

	var violations []Violation

	// Extract all behavioral claims from response
	claims := c.extractBehavioralClaims(input.Response)

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

		// Skip if category is disabled
		if !c.isCategoryEnabled(claim.Category) {
			continue
		}

		// Skip if subject is not in evidence (can't verify)
		if !c.isSubjectInEvidence(claim.Subject, input.EvidenceIndex) {
			continue
		}

		// Validate the claim against evidence
		v := c.validateBehavioralClaim(ctx, claim, input.EvidenceIndex)
		if v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// isCategoryEnabled checks if a claim category is enabled in config.
func (c *BehavioralChecker) isCategoryEnabled(category BehavioralClaimCategory) bool {
	switch category {
	case ClaimErrorHandling:
		return c.config.CheckErrorHandling
	case ClaimValidation:
		return c.config.CheckValidation
	case ClaimSecurity:
		return c.config.CheckSecurity
	default:
		return false
	}
}

// isSubjectInEvidence checks if the claimed subject exists in evidence.
func (c *BehavioralChecker) isSubjectInEvidence(subject string, idx *EvidenceIndex) bool {
	// Check Symbols map
	if idx.Symbols[subject] {
		return true
	}

	// Check SymbolDetails map
	if _, ok := idx.SymbolDetails[subject]; ok {
		return true
	}

	// Check if subject appears in RawContent (function definition)
	if idx.RawContent != "" {
		// Look for function definition pattern
		pattern := `func\s+(?:\([^)]*\)\s+)?` + regexp.QuoteMeta(subject) + `\s*\(`
		if matched, _ := regexp.MatchString(pattern, idx.RawContent); matched {
			return true
		}
	}

	return false
}

// extractBehavioralClaims extracts all behavioral claims from response text.
func (c *BehavioralChecker) extractBehavioralClaims(response string) []BehavioralClaim {
	var claims []BehavioralClaim
	seen := make(map[string]bool) // Dedup by subject+category

	// Extract error handling claims
	if c.config.CheckErrorHandling {
		matches := errorHandlingClaimPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}
			subject := response[match[2]:match[3]]
			raw := response[match[0]:match[1]]

			// Skip common words (case-insensitive check)
			if isBehavioralCommonWord(subject) {
				continue
			}

			key := fmt.Sprintf("error:%s", subject)
			if !seen[key] {
				claims = append(claims, BehavioralClaim{
					Subject:  subject,
					Behavior: "handles errors",
					Category: ClaimErrorHandling,
					Position: match[0],
					Raw:      raw,
				})
				seen[key] = true
			}
		}
	}

	// Extract validation claims
	if c.config.CheckValidation {
		matches := validationClaimPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}
			subject := response[match[2]:match[3]]
			raw := response[match[0]:match[1]]

			// Skip common words (case-insensitive check)
			if isBehavioralCommonWord(subject) {
				continue
			}

			key := fmt.Sprintf("validation:%s", subject)
			if !seen[key] {
				claims = append(claims, BehavioralClaim{
					Subject:  subject,
					Behavior: "validates input",
					Category: ClaimValidation,
					Position: match[0],
					Raw:      raw,
				})
				seen[key] = true
			}
		}
	}

	// Extract security claims
	if c.config.CheckSecurity {
		matches := securityClaimPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}
			subject := response[match[2]:match[3]]
			raw := response[match[0]:match[1]]

			// Skip common words (case-insensitive check)
			if isBehavioralCommonWord(subject) {
				continue
			}

			key := fmt.Sprintf("security:%s", subject)
			if !seen[key] {
				claims = append(claims, BehavioralClaim{
					Subject:  subject,
					Behavior: "secures data",
					Category: ClaimSecurity,
					Position: match[0],
					Raw:      raw,
				})
				seen[key] = true
			}
		}
	}

	return claims
}

// validateBehavioralClaim validates a single behavioral claim.
func (c *BehavioralChecker) validateBehavioralClaim(ctx context.Context, claim BehavioralClaim, idx *EvidenceIndex) *Violation {
	// Get the code to search for patterns
	code := c.getCodeForSubject(claim.Subject, idx)
	if code == "" {
		// No code available for this subject, skip validation
		return nil
	}

	var evidencePatterns []*regexp.Regexp
	var counterPatterns []*regexp.Regexp

	switch claim.Category {
	case ClaimErrorHandling:
		evidencePatterns = errorHandlingEvidencePatterns
		counterPatterns = errorHandlingCounterPatterns
	case ClaimValidation:
		evidencePatterns = validationEvidencePatterns
		counterPatterns = validationCounterPatterns
	case ClaimSecurity:
		evidencePatterns = securityEvidencePatterns
		counterPatterns = securityCounterPatterns
	}

	// Check for supporting evidence first
	hasEvidence := false
	for _, pattern := range evidencePatterns {
		if pattern.MatchString(code) {
			hasEvidence = true
			break
		}
	}

	// If we have supporting evidence, no violation
	if hasEvidence {
		return nil
	}

	// Check for counter-evidence
	hasCounterEvidence := false
	var counterEvidenceText string
	for _, pattern := range counterPatterns {
		if match := pattern.FindString(code); match != "" {
			hasCounterEvidence = true
			counterEvidenceText = match
			break
		}
	}

	// Determine if we should flag this
	if c.config.RequireCounterEvidence && !hasCounterEvidence {
		// RequireCounterEvidence is true and we have no counter-evidence
		// Don't flag - could be false positive
		return nil
	}

	// Generate violation
	RecordBehavioralHallucination(ctx, claim.Category.String(), claim.Subject, claim.Behavior)

	severity := SeverityHigh
	var message string
	var suggestion string

	if hasCounterEvidence {
		message = fmt.Sprintf("Claim that %s %s is contradicted by code: %s",
			claim.Subject, claim.Behavior, counterEvidenceText)
		suggestion = "Verify the behavioral claim against the actual code implementation"
	} else {
		severity = SeverityWarning
		message = fmt.Sprintf("No evidence found that %s %s", claim.Subject, claim.Behavior)
		suggestion = "Add supporting code citations to verify behavioral claims"
	}

	return &Violation{
		Type:           ViolationBehavioralHallucination,
		Severity:       severity,
		Code:           fmt.Sprintf("BEHAVIORAL_%s", strings.ToUpper(claim.Category.String())),
		Message:        message,
		Evidence:       claim.Raw,
		Expected:       fmt.Sprintf("Supporting code patterns for '%s' behavior", claim.Category.String()),
		LocationOffset: claim.Position,
		Suggestion:     suggestion,
	}
}

// getCodeForSubject retrieves the code associated with a subject.
func (c *BehavioralChecker) getCodeForSubject(subject string, idx *EvidenceIndex) string {
	// First, try to find the subject in SymbolDetails to get its file
	if details, ok := idx.SymbolDetails[subject]; ok && len(details) > 0 {
		filePath := details[0].File
		if content, ok := idx.FileContents[filePath]; ok {
			return content
		}
	}

	// Fall back to searching RawContent
	return idx.RawContent
}
