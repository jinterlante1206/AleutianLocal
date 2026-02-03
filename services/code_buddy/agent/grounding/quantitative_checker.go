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
	"strconv"
	"strings"
)

// QuantitativeClaimType categorizes types of quantitative claims.
type QuantitativeClaimType int

const (
	// ClaimFileCount is a claim about the number of files.
	ClaimFileCount QuantitativeClaimType = iota

	// ClaimLineCount is a claim about line counts in files.
	ClaimLineCount

	// ClaimSymbolCount is a claim about the number of symbols.
	ClaimSymbolCount
)

// String returns the string representation of a QuantitativeClaimType.
func (c QuantitativeClaimType) String() string {
	switch c {
	case ClaimFileCount:
		return "file_count"
	case ClaimLineCount:
		return "line_count"
	case ClaimSymbolCount:
		return "symbol_count"
	default:
		return "unknown"
	}
}

// QuantitativeClaim represents a parsed quantitative claim from response text.
type QuantitativeClaim struct {
	// Type is the kind of quantitative claim.
	Type QuantitativeClaimType

	// Number is the claimed numeric value.
	Number int

	// Subject is what's being counted (e.g., "test files", "lines", "functions").
	Subject string

	// Context provides additional context (e.g., file name for line counts).
	Context string

	// IsApproximate indicates if the claim used "about", "around", etc.
	IsApproximate bool

	// Position is the character offset in the response.
	Position int

	// Raw is the original matched text.
	Raw string
}

// Package-level number word mappings.
var (
	// numberWords maps English number words to their integer values.
	numberWords = map[string]int{
		"zero":      0,
		"one":       1,
		"two":       2,
		"three":     3,
		"four":      4,
		"five":      5,
		"six":       6,
		"seven":     7,
		"eight":     8,
		"nine":      9,
		"ten":       10,
		"eleven":    11,
		"twelve":    12,
		"dozen":     12,
		"thirteen":  13,
		"fourteen":  14,
		"fifteen":   15,
		"sixteen":   16,
		"seventeen": 17,
		"eighteen":  18,
		"nineteen":  19,
		"twenty":    20,
		"thirty":    30,
		"forty":     40,
		"fifty":     50,
		"sixty":     60,
		"seventy":   70,
		"eighty":    80,
		"ninety":    90,
		"hundred":   100,
	}

	// vagueQuantities are words that indicate imprecise quantities (skip validation).
	vagueQuantities = map[string]bool{
		"several":  true,
		"many":     true,
		"few":      true,
		"some":     true,
		"numerous": true,
		"multiple": true,
		"various":  true,
		"a few":    true,
		"a lot":    true,
	}
)

// Package-level compiled regexes for quantitative claim extraction.
var (
	// File count patterns.
	// Match: "15 files", "3 test files", "5 Go files"
	fileCountPattern = regexp.MustCompile(
		`(?i)(?:(?:there\s+(?:are|is)|contains?|has|have|with)\s+)?` +
			`((?:about|around|approximately|roughly|nearly|~)\s+)?` +
			`(\d+(?:,\d{3})*|\b(?:one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|hundred|dozen)\b)\s+` +
			`((?:\w+\s+)?files?)\b`,
	)

	// Line count patterns.
	// Match: "main.go is 200 lines", "file has 50 lines", "100 lines of code"
	lineCountPattern = regexp.MustCompile(
		`(?i)(?:([a-zA-Z0-9_./]+\.(?:go|py|js|ts|java|c|cpp|h|rs|rb))\s+(?:is|has|contains?)\s+)?` +
			`((?:about|around|approximately|roughly|nearly|~)\s+)?` +
			`(\d+(?:,\d{3})*|\b(?:one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|hundred|dozen)\b)\s+` +
			`lines?\b`,
	)

	// Symbol count patterns.
	// Match: "5 functions", "3 methods", "10 types"
	symbolCountPattern = regexp.MustCompile(
		`(?i)(?:(?:there\s+(?:are|is)|contains?|has|have|with)\s+)?` +
			`((?:about|around|approximately|roughly|nearly|~)\s+)?` +
			`(\d+(?:,\d{3})*|\b(?:one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|hundred|dozen)\b)\s+` +
			`(functions?|methods?|types?|structs?|interfaces?|variables?|constants?)\b`,
	)

	// SI suffix pattern for normalizing numbers like "1k", "2M".
	siSuffixPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([kKmMgG])$`)
)

// QuantitativeChecker validates quantitative claims in responses.
//
// This checker detects incorrect numeric claims including:
// - Wrong file counts ("15 test files" when there are 3)
// - Wrong line counts ("200 lines" when file has 52)
// - Wrong symbol counts ("10 functions" when there are 5)
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type QuantitativeChecker struct {
	config *QuantitativeCheckerConfig
}

// NewQuantitativeChecker creates a new quantitative checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*QuantitativeChecker - The configured checker.
func NewQuantitativeChecker(config *QuantitativeCheckerConfig) *QuantitativeChecker {
	if config == nil {
		config = DefaultQuantitativeCheckerConfig()
	}
	return &QuantitativeChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *QuantitativeChecker) Name() string {
	return "quantitative_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts quantitative claims from the response and validates them against
//	the EvidenceIndex. Detects incorrect numeric claims by comparing claimed
//	values against actual counts from evidence.
//
// Thread Safety: Safe for concurrent use.
func (c *QuantitativeChecker) Check(ctx context.Context, input *CheckInput) []Violation {
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

	// Extract all quantitative claims from response
	claims := c.extractQuantitativeClaims(input.Response)

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

		// Skip if claim type is disabled
		if !c.isClaimTypeEnabled(claim.Type) {
			continue
		}

		// Validate the claim against evidence
		v := c.validateQuantitativeClaim(ctx, claim, input.EvidenceIndex)
		if v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// isClaimTypeEnabled checks if a claim type is enabled in config.
func (c *QuantitativeChecker) isClaimTypeEnabled(claimType QuantitativeClaimType) bool {
	switch claimType {
	case ClaimFileCount:
		return c.config.CheckFileCounts
	case ClaimLineCount:
		return c.config.CheckLineCounts
	case ClaimSymbolCount:
		return c.config.CheckSymbolCounts
	default:
		return false
	}
}

// extractQuantitativeClaims extracts all quantitative claims from response text.
func (c *QuantitativeChecker) extractQuantitativeClaims(response string) []QuantitativeClaim {
	var claims []QuantitativeClaim
	seen := make(map[string]bool) // Dedup by raw text

	// Extract file count claims
	if c.config.CheckFileCounts {
		matches := fileCountPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			claim := c.parseFileCountMatch(response, match)
			if claim != nil {
				key := claim.Raw
				if !seen[key] {
					claims = append(claims, *claim)
					seen[key] = true
				}
			}
		}
	}

	// Extract line count claims
	if c.config.CheckLineCounts {
		matches := lineCountPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			claim := c.parseLineCountMatch(response, match)
			if claim != nil {
				key := claim.Raw
				if !seen[key] {
					claims = append(claims, *claim)
					seen[key] = true
				}
			}
		}
	}

	// Extract symbol count claims
	if c.config.CheckSymbolCounts {
		matches := symbolCountPattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			claim := c.parseSymbolCountMatch(response, match)
			if claim != nil {
				key := claim.Raw
				if !seen[key] {
					claims = append(claims, *claim)
					seen[key] = true
				}
			}
		}
	}

	return claims
}

// parseFileCountMatch parses a file count regex match.
func (c *QuantitativeChecker) parseFileCountMatch(response string, match []int) *QuantitativeClaim {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Check for approximate indicator (group 1)
	isApproximate := match[2] != -1 && match[3] != -1

	// Extract number (group 2)
	if match[4] == -1 || match[5] == -1 {
		return nil
	}
	numStr := response[match[4]:match[5]]
	num, ok := parseNumber(numStr)
	if !ok {
		return nil
	}

	// Extract subject (group 3)
	subject := ""
	if match[6] != -1 && match[7] != -1 {
		subject = strings.ToLower(response[match[6]:match[7]])
	}

	return &QuantitativeClaim{
		Type:          ClaimFileCount,
		Number:        num,
		Subject:       subject,
		IsApproximate: isApproximate,
		Position:      match[0],
		Raw:           raw,
	}
}

// parseLineCountMatch parses a line count regex match.
func (c *QuantitativeChecker) parseLineCountMatch(response string, match []int) *QuantitativeClaim {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Extract file context (group 1) - optional
	context := ""
	if match[2] != -1 && match[3] != -1 {
		context = response[match[2]:match[3]]
	}

	// Check for approximate indicator (group 2)
	isApproximate := match[4] != -1 && match[5] != -1

	// Extract number (group 3)
	if match[6] == -1 || match[7] == -1 {
		return nil
	}
	numStr := response[match[6]:match[7]]
	num, ok := parseNumber(numStr)
	if !ok {
		return nil
	}

	return &QuantitativeClaim{
		Type:          ClaimLineCount,
		Number:        num,
		Subject:       "lines",
		Context:       context,
		IsApproximate: isApproximate,
		Position:      match[0],
		Raw:           raw,
	}
}

// parseSymbolCountMatch parses a symbol count regex match.
func (c *QuantitativeChecker) parseSymbolCountMatch(response string, match []int) *QuantitativeClaim {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Check for approximate indicator (group 1)
	isApproximate := match[2] != -1 && match[3] != -1

	// Extract number (group 2)
	if match[4] == -1 || match[5] == -1 {
		return nil
	}
	numStr := response[match[4]:match[5]]
	num, ok := parseNumber(numStr)
	if !ok {
		return nil
	}

	// Extract subject (group 3)
	subject := ""
	if match[6] != -1 && match[7] != -1 {
		subject = strings.ToLower(response[match[6]:match[7]])
	}

	return &QuantitativeClaim{
		Type:          ClaimSymbolCount,
		Number:        num,
		Subject:       subject,
		IsApproximate: isApproximate,
		Position:      match[0],
		Raw:           raw,
	}
}

// parseNumber parses a number from string, handling digits, words, commas, and SI suffixes.
func parseNumber(s string) (int, bool) {
	s = strings.TrimSpace(strings.ToLower(s))

	// Check for vague quantities
	if vagueQuantities[s] {
		return 0, false // Skip validation for vague quantities
	}

	// Check for number words
	if val, ok := numberWords[s]; ok {
		return val, true
	}

	// Remove commas for comma-formatted numbers
	s = strings.ReplaceAll(s, ",", "")

	// Check for SI suffixes (1k, 2M, etc.)
	if siMatch := siSuffixPattern.FindStringSubmatch(s); len(siMatch) > 0 {
		base, err := strconv.ParseFloat(siMatch[1], 64)
		if err != nil {
			return 0, false
		}
		multiplier := 1.0
		switch strings.ToLower(siMatch[2]) {
		case "k":
			multiplier = 1000
		case "m":
			multiplier = 1000000
		case "g":
			multiplier = 1000000000
		}
		return int(base * multiplier), true
	}

	// Parse as regular integer
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return val, true
}

// validateQuantitativeClaim validates a single quantitative claim.
func (c *QuantitativeChecker) validateQuantitativeClaim(ctx context.Context, claim QuantitativeClaim, idx *EvidenceIndex) *Violation {
	var actualCount int
	var canValidate bool

	switch claim.Type {
	case ClaimFileCount:
		actualCount, canValidate = c.countFiles(claim, idx)
	case ClaimLineCount:
		actualCount, canValidate = c.countLines(claim, idx)
	case ClaimSymbolCount:
		actualCount, canValidate = c.countSymbols(claim, idx)
	}

	if !canValidate {
		// Cannot validate - no evidence available
		return nil
	}

	// Check if the claim is within tolerance
	isValid := c.isWithinTolerance(claim.Number, actualCount, claim.IsApproximate)
	if isValid {
		return nil
	}

	// Generate violation
	RecordQuantitativeHallucination(ctx, claim.Type.String(), claim.Number, actualCount)

	severity := SeverityHigh
	if claim.IsApproximate {
		severity = SeverityWarning
	}

	var message string
	if claim.Type == ClaimLineCount && claim.Context != "" {
		message = fmt.Sprintf("Claimed %s has %d lines but actual line count is %d",
			claim.Context, claim.Number, actualCount)
	} else {
		message = fmt.Sprintf("Claimed %d %s but actual count is %d",
			claim.Number, claim.Subject, actualCount)
	}

	suggestion := "Verify numeric claims against actual code or tool output"
	if claim.IsApproximate {
		diff := float64(claim.Number-actualCount) / float64(actualCount) * 100
		if diff > 0 {
			message += fmt.Sprintf(" (overcounted by %.0f%%)", diff)
		} else {
			message += fmt.Sprintf(" (undercounted by %.0f%%)", -diff)
		}
	}

	return &Violation{
		Type:           ViolationQuantitativeHallucination,
		Severity:       severity,
		Code:           fmt.Sprintf("QUANTITATIVE_%s", strings.ToUpper(claim.Type.String())),
		Message:        message,
		Evidence:       claim.Raw,
		Expected:       fmt.Sprintf("Actual count: %d", actualCount),
		LocationOffset: claim.Position,
		Suggestion:     suggestion,
	}
}

// countFiles counts files matching the claim subject.
func (c *QuantitativeChecker) countFiles(claim QuantitativeClaim, idx *EvidenceIndex) (int, bool) {
	if len(idx.Files) == 0 {
		return 0, false
	}

	subject := strings.ToLower(claim.Subject)

	// Determine filter based on subject
	var filter func(path string) bool

	switch {
	case strings.Contains(subject, "test"):
		filter = func(path string) bool {
			return strings.Contains(path, "_test.") || strings.Contains(path, "test_")
		}
	case strings.Contains(subject, "go"):
		filter = func(path string) bool {
			return strings.HasSuffix(path, ".go")
		}
	case strings.Contains(subject, "python") || strings.Contains(subject, "py"):
		filter = func(path string) bool {
			return strings.HasSuffix(path, ".py")
		}
	case strings.Contains(subject, "javascript") || strings.Contains(subject, "js"):
		filter = func(path string) bool {
			return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".ts")
		}
	default:
		// Count all files
		filter = func(path string) bool { return true }
	}

	count := 0
	for path := range idx.Files {
		if filter(path) {
			count++
		}
	}

	return count, true
}

// countLines counts lines for a specific file.
func (c *QuantitativeChecker) countLines(claim QuantitativeClaim, idx *EvidenceIndex) (int, bool) {
	if len(idx.FileLines) == 0 {
		return 0, false
	}

	// If we have a specific file context, look it up
	if claim.Context != "" {
		// Try exact match first
		if lines, ok := idx.FileLines[claim.Context]; ok {
			return lines, true
		}

		// Try matching by basename
		for path, lines := range idx.FileLines {
			if strings.HasSuffix(path, "/"+claim.Context) || path == claim.Context {
				return lines, true
			}
		}

		// No match found
		return 0, false
	}

	// No specific file context - cannot validate
	return 0, false
}

// countSymbols counts symbols matching the claim subject.
func (c *QuantitativeChecker) countSymbols(claim QuantitativeClaim, idx *EvidenceIndex) (int, bool) {
	if len(idx.SymbolDetails) == 0 && len(idx.Symbols) == 0 {
		return 0, false
	}

	subject := strings.ToLower(claim.Subject)

	// Determine symbol kind filter based on subject
	var kindFilter string
	switch {
	case strings.Contains(subject, "function"):
		kindFilter = "function"
	case strings.Contains(subject, "method"):
		kindFilter = "method"
	case strings.Contains(subject, "type") || strings.Contains(subject, "struct"):
		kindFilter = "type"
	case strings.Contains(subject, "interface"):
		kindFilter = "interface"
	case strings.Contains(subject, "variable"):
		kindFilter = "variable"
	case strings.Contains(subject, "constant"):
		kindFilter = "constant"
	default:
		// Count all symbols
		kindFilter = ""
	}

	// If we have SymbolDetails, use it for more accurate counting
	if len(idx.SymbolDetails) > 0 {
		count := 0
		for _, details := range idx.SymbolDetails {
			for _, info := range details {
				if kindFilter == "" || strings.EqualFold(info.Kind, kindFilter) {
					count++
				}
			}
		}
		return count, true
	}

	// Fall back to simple Symbols count (no kind filtering)
	if kindFilter == "" {
		return len(idx.Symbols), true
	}

	// Cannot filter by kind with simple Symbols map
	return 0, false
}

// isWithinTolerance checks if claimed value is within acceptable tolerance of actual.
func (c *QuantitativeChecker) isWithinTolerance(claimed, actual int, isApproximate bool) bool {
	if actual == 0 {
		// Special case: actual is 0, any claim > 0 is a violation
		return claimed == 0
	}

	diff := claimed - actual

	if !isApproximate {
		// Exact claim - use ExactTolerance
		return intAbs(diff) <= c.config.ExactTolerance
	}

	// Approximate claim - use asymmetric tolerance
	diffPercent := float64(diff) / float64(actual)

	if diff < 0 {
		// Undercounting: allow up to ApproximateUnderPct
		return -diffPercent <= c.config.ApproximateUnderPct
	}

	// Overcounting: allow up to ApproximateOverPct
	return diffPercent <= c.config.ApproximateOverPct
}

// intAbs returns the absolute value of an integer.
// Named intAbs to avoid conflict with abs in line_number_checker.go.
func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
