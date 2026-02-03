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

// Package-level precompiled regex patterns for attribute claim extraction.
var (
	// Return type patterns
	// Matches: "Parse returns error", "The Parse function returns (*Result, error)"
	returnTypePattern = regexp.MustCompile(`(?i)\b([A-Z]\w*)\s+(?:function\s+)?returns?\s+(\([^)]+\)|[\w*\[\]]+)`)

	// Parameter patterns
	// Matches: "Process takes 3 arguments", "The Process function takes 2 parameters"
	paramCountPattern = regexp.MustCompile(`(?i)\b([A-Z]\w*)\s+(?:function\s+)?takes?\s+(\d+)\s+(?:arguments?|parameters?|params?)`)

	// Field patterns
	// Matches: "Config has 5 fields", "The Config struct has 3 fields"
	fieldCountPattern = regexp.MustCompile(`(?i)\b([A-Z]\w*)\s+(?:struct\s+)?has\s+(\d+)\s+fields?`)
	// Matches: "Config has fields: Name, Value", "Config contains fields Name and Value"
	fieldNamesPattern = regexp.MustCompile(`(?i)\b([A-Z]\w*)\s+(?:struct\s+)?(?:has|contains?)\s+fields?\s*:?\s*([^.]+)`)

	// Method patterns
	// Matches: "Reader defines 3 methods", "The Reader interface has 2 methods"
	methodCountPattern = regexp.MustCompile(`(?i)\b([A-Z]\w*)\s+(?:interface\s+)?(?:defines?|has)\s+(\d+)\s+methods?`)
)

// AttributeClaimKind categorizes the type of attribute claim.
type AttributeClaimKind int

const (
	// ClaimReturnType is a claim about function return type.
	ClaimReturnType AttributeClaimKind = iota
	// ClaimParamCount is a claim about parameter count.
	ClaimParamCount
	// ClaimParamTypes is a claim about parameter types.
	ClaimParamTypes
	// ClaimFieldCount is a claim about struct field count.
	ClaimFieldCount
	// ClaimFieldNames is a claim about specific field names.
	ClaimFieldNames
	// ClaimMethodCount is a claim about interface method count.
	ClaimMethodCount
)

// AttributeClaim represents an extracted attribute claim from the response.
type AttributeClaim struct {
	Kind       AttributeClaimKind
	SymbolName string
	Value      string   // The claimed value (e.g., "3" for count, "error" for type)
	Values     []string // For list-based claims (e.g., field names)
	RawText    string   // Original text containing the claim
	Position   int      // Position in response
}

// AttributeChecker validates attribute claims about code symbols.
//
// This checker detects when the model makes incorrect claims about
// real code elements: wrong return types, wrong parameter counts,
// wrong field names, etc.
//
// Thread Safety: Safe for concurrent use after construction.
type AttributeChecker struct {
	config *AttributeCheckerConfig
}

// NewAttributeChecker creates a new attribute checker.
//
// Inputs:
//
//	config - Configuration for the checker. If nil, defaults are used.
//
// Outputs:
//
//	*AttributeChecker - The configured checker.
func NewAttributeChecker(config *AttributeCheckerConfig) *AttributeChecker {
	if config == nil {
		config = DefaultAttributeCheckerConfig()
	}
	return &AttributeChecker{config: config}
}

// Name returns the checker name for logging and metrics.
func (c *AttributeChecker) Name() string {
	return "attribute_checker"
}

// Check runs the attribute hallucination check.
//
// Description:
//
//	Extracts attribute claims from the response and validates them
//	against symbol details in the evidence index.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	input - The input data for checking.
//
// Outputs:
//
//	[]Violation - Any violations found.
//
// Thread Safety: Safe for concurrent use.
func (c *AttributeChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Need symbol details to validate against
	if input.EvidenceIndex == nil || len(input.EvidenceIndex.SymbolDetails) == 0 {
		return nil
	}

	var violations []Violation

	// Extract and validate claims
	claims := c.extractClaims(input.Response)

	// Limit claims to check
	if len(claims) > c.config.MaxClaimsToCheck {
		claims = claims[:c.config.MaxClaimsToCheck]
	}

	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		// Look up symbol in evidence index
		symbolInfos, found := input.EvidenceIndex.SymbolDetails[claim.SymbolName]
		if !found {
			// Symbol not in evidence, skip validation
			continue
		}

		// Validate the claim against all symbol locations
		if v := c.validateClaim(ctx, claim, symbolInfos); v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// extractClaims extracts attribute claims from the response text.
func (c *AttributeChecker) extractClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Extract return type claims
	claims = append(claims, c.extractReturnTypeClaims(response)...)

	// Extract parameter count claims
	claims = append(claims, c.extractParamCountClaims(response)...)

	// Extract field count claims
	claims = append(claims, c.extractFieldCountClaims(response)...)

	// Extract field name claims
	if !c.config.IgnorePartialClaims {
		claims = append(claims, c.extractFieldNameClaims(response)...)
	}

	// Extract method count claims
	claims = append(claims, c.extractMethodCountClaims(response)...)

	return claims
}

// extractReturnTypeClaims extracts claims about function return types.
func (c *AttributeChecker) extractReturnTypeClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Pattern: "X returns Y" or "X returns (A, B)"
	matches := returnTypePattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) >= 6 {
			symbolName := response[match[2]:match[3]]
			returnType := response[match[4]:match[5]]

			if len(symbolName) < c.config.MinSymbolLength {
				continue
			}

			claims = append(claims, AttributeClaim{
				Kind:       ClaimReturnType,
				SymbolName: symbolName,
				Value:      strings.TrimSpace(returnType),
				RawText:    response[match[0]:match[1]],
				Position:   match[0],
			})
		}
	}

	return claims
}

// extractParamCountClaims extracts claims about parameter counts.
func (c *AttributeChecker) extractParamCountClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Pattern: "X takes N arguments"
	matches := paramCountPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) >= 6 {
			symbolName := response[match[2]:match[3]]
			countStr := response[match[4]:match[5]]

			if len(symbolName) < c.config.MinSymbolLength {
				continue
			}

			claims = append(claims, AttributeClaim{
				Kind:       ClaimParamCount,
				SymbolName: symbolName,
				Value:      countStr,
				RawText:    response[match[0]:match[1]],
				Position:   match[0],
			})
		}
	}

	return claims
}

// extractFieldCountClaims extracts claims about struct field counts.
func (c *AttributeChecker) extractFieldCountClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Pattern: "X has N fields"
	matches := fieldCountPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) >= 6 {
			symbolName := response[match[2]:match[3]]
			countStr := response[match[4]:match[5]]

			if len(symbolName) < c.config.MinSymbolLength {
				continue
			}

			claims = append(claims, AttributeClaim{
				Kind:       ClaimFieldCount,
				SymbolName: symbolName,
				Value:      countStr,
				RawText:    response[match[0]:match[1]],
				Position:   match[0],
			})
		}
	}

	return claims
}

// extractFieldNameClaims extracts claims about specific field names.
func (c *AttributeChecker) extractFieldNameClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Pattern: "X has/contains fields: A, B, C" or "X has fields A and B"
	matches := fieldNamesPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) >= 6 {
			symbolName := response[match[2]:match[3]]
			fieldsPart := response[match[4]:match[5]]

			if len(symbolName) < c.config.MinSymbolLength {
				continue
			}

			// Parse field names from the list
			fieldNames := parseFieldNameList(fieldsPart)
			if len(fieldNames) == 0 {
				continue
			}

			claims = append(claims, AttributeClaim{
				Kind:       ClaimFieldNames,
				SymbolName: symbolName,
				Values:     fieldNames,
				RawText:    response[match[0]:match[1]],
				Position:   match[0],
			})
		}
	}

	return claims
}

// extractMethodCountClaims extracts claims about interface method counts.
func (c *AttributeChecker) extractMethodCountClaims(response string) []AttributeClaim {
	var claims []AttributeClaim

	// Pattern: "X defines/has N methods"
	matches := methodCountPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) >= 6 {
			symbolName := response[match[2]:match[3]]
			countStr := response[match[4]:match[5]]

			if len(symbolName) < c.config.MinSymbolLength {
				continue
			}

			claims = append(claims, AttributeClaim{
				Kind:       ClaimMethodCount,
				SymbolName: symbolName,
				Value:      countStr,
				RawText:    response[match[0]:match[1]],
				Position:   match[0],
			})
		}
	}

	return claims
}

// validateClaim validates a single attribute claim against symbol info.
// If ANY symbol location matches the claim, no violation is returned.
func (c *AttributeChecker) validateClaim(ctx context.Context, claim AttributeClaim, symbolInfos []SymbolInfo) *Violation {
	var lastViolation *Violation
	var lastSymKind string
	foundMatchingKind := false

	// Check all symbol locations - if any matches, no violation
	for _, sym := range symbolInfos {
		switch claim.Kind {
		case ClaimReturnType:
			if sym.Kind != "function" && sym.Kind != "method" {
				continue
			}
			foundMatchingKind = true
			if v := c.validateReturnType(claim, sym); v != nil {
				lastViolation = v
				lastSymKind = sym.Kind
			} else {
				// Found a match - no violation
				return nil
			}

		case ClaimParamCount:
			if sym.Kind != "function" && sym.Kind != "method" {
				continue
			}
			foundMatchingKind = true
			if v := c.validateParamCount(claim, sym); v != nil {
				lastViolation = v
				lastSymKind = sym.Kind
			} else {
				return nil
			}

		case ClaimFieldCount:
			if sym.Kind != "struct" {
				continue
			}
			foundMatchingKind = true
			if v := c.validateFieldCount(claim, sym); v != nil {
				lastViolation = v
				lastSymKind = sym.Kind
			} else {
				return nil
			}

		case ClaimFieldNames:
			if sym.Kind != "struct" {
				continue
			}
			foundMatchingKind = true
			if v := c.validateFieldNames(claim, sym); v != nil {
				lastViolation = v
				lastSymKind = sym.Kind
			} else {
				return nil
			}

		case ClaimMethodCount:
			if sym.Kind != "interface" {
				continue
			}
			foundMatchingKind = true
			if v := c.validateMethodCount(claim, sym); v != nil {
				lastViolation = v
				lastSymKind = sym.Kind
			} else {
				return nil
			}
		}
	}

	// If we found symbols of matching kind but none matched the claim, return violation
	if foundMatchingKind && lastViolation != nil {
		claimType := claimKindToString(claim.Kind)
		RecordAttributeHallucination(ctx, claimType, lastSymKind)
		return lastViolation
	}

	// No matching symbol kind found, skip validation
	return nil
}

// claimKindToString converts a claim kind to a string for metrics.
func claimKindToString(kind AttributeClaimKind) string {
	switch kind {
	case ClaimReturnType:
		return "return_type"
	case ClaimParamCount:
		return "parameter_count"
	case ClaimParamTypes:
		return "parameter_types"
	case ClaimFieldCount:
		return "field_count"
	case ClaimFieldNames:
		return "field_names"
	case ClaimMethodCount:
		return "method_count"
	default:
		return "unknown"
	}
}

// validateReturnType validates a return type claim.
func (c *AttributeChecker) validateReturnType(claim AttributeClaim, sym SymbolInfo) *Violation {
	if len(sym.ReturnTypes) == 0 {
		// No return type info available, skip
		return nil
	}

	claimedReturns := parseReturnTypes(claim.Value)
	actualReturns := sym.ReturnTypes

	// Compare return types (order matters for Go)
	if !compareReturnTypes(claimedReturns, actualReturns) {
		return &Violation{
			Type:     ViolationAttributeHallucination,
			Severity: SeverityHigh,
			Code:     "ATTR_WRONG_RETURN_TYPE",
			Message: fmt.Sprintf("%s does not return %s",
				claim.SymbolName, claim.Value),
			Evidence: claim.RawText,
			Expected: formatReturnTypes(actualReturns),
			Location: fmt.Sprintf("%s:%d", sym.File, sym.Line),
			Suggestion: fmt.Sprintf("The actual return type is %s",
				formatReturnTypes(actualReturns)),
		}
	}

	return nil
}

// validateParamCount validates a parameter count claim.
func (c *AttributeChecker) validateParamCount(claim AttributeClaim, sym SymbolInfo) *Violation {
	claimedCount, err := strconv.Atoi(claim.Value)
	if err != nil {
		return nil // Can't parse, skip
	}

	actualCount := len(sym.Parameters)

	if claimedCount != actualCount {
		return &Violation{
			Type:     ViolationAttributeHallucination,
			Severity: SeverityHigh,
			Code:     "ATTR_WRONG_PARAM_COUNT",
			Message: fmt.Sprintf("%s does not take %d arguments",
				claim.SymbolName, claimedCount),
			Evidence: claim.RawText,
			Expected: fmt.Sprintf("%d parameters", actualCount),
			Location: fmt.Sprintf("%s:%d", sym.File, sym.Line),
			Suggestion: fmt.Sprintf("The function takes %d parameters",
				actualCount),
		}
	}

	return nil
}

// validateFieldCount validates a field count claim.
func (c *AttributeChecker) validateFieldCount(claim AttributeClaim, sym SymbolInfo) *Violation {
	claimedCount, err := strconv.Atoi(claim.Value)
	if err != nil {
		return nil // Can't parse, skip
	}

	actualCount := len(sym.Fields)

	if claimedCount != actualCount {
		return &Violation{
			Type:     ViolationAttributeHallucination,
			Severity: SeverityHigh,
			Code:     "ATTR_WRONG_FIELD_COUNT",
			Message: fmt.Sprintf("%s does not have %d fields",
				claim.SymbolName, claimedCount),
			Evidence: claim.RawText,
			Expected: fmt.Sprintf("%d fields", actualCount),
			Location: fmt.Sprintf("%s:%d", sym.File, sym.Line),
			Suggestion: fmt.Sprintf("The struct has %d fields: %s",
				actualCount, strings.Join(sym.Fields, ", ")),
		}
	}

	return nil
}

// validateFieldNames validates a field names claim.
func (c *AttributeChecker) validateFieldNames(claim AttributeClaim, sym SymbolInfo) *Violation {
	if len(sym.Fields) == 0 {
		// No field info available, skip
		return nil
	}

	// Check each claimed field exists
	actualFieldSet := make(map[string]bool)
	for _, f := range sym.Fields {
		actualFieldSet[strings.ToLower(f)] = true
	}

	var missingFields []string
	for _, claimedField := range claim.Values {
		if !actualFieldSet[strings.ToLower(claimedField)] {
			missingFields = append(missingFields, claimedField)
		}
	}

	if len(missingFields) > 0 {
		return &Violation{
			Type:     ViolationAttributeHallucination,
			Severity: SeverityHigh,
			Code:     "ATTR_WRONG_FIELD_NAMES",
			Message: fmt.Sprintf("%s does not have fields: %s",
				claim.SymbolName, strings.Join(missingFields, ", ")),
			Evidence: claim.RawText,
			Expected: fmt.Sprintf("actual fields: %s", strings.Join(sym.Fields, ", ")),
			Location: fmt.Sprintf("%s:%d", sym.File, sym.Line),
			Suggestion: fmt.Sprintf("The struct has fields: %s",
				strings.Join(sym.Fields, ", ")),
		}
	}

	return nil
}

// validateMethodCount validates a method count claim.
func (c *AttributeChecker) validateMethodCount(claim AttributeClaim, sym SymbolInfo) *Violation {
	claimedCount, err := strconv.Atoi(claim.Value)
	if err != nil {
		return nil // Can't parse, skip
	}

	actualCount := len(sym.Methods)

	if claimedCount != actualCount {
		return &Violation{
			Type:     ViolationAttributeHallucination,
			Severity: SeverityHigh,
			Code:     "ATTR_WRONG_METHOD_COUNT",
			Message: fmt.Sprintf("%s does not have %d methods",
				claim.SymbolName, claimedCount),
			Evidence: claim.RawText,
			Expected: fmt.Sprintf("%d methods", actualCount),
			Location: fmt.Sprintf("%s:%d", sym.File, sym.Line),
			Suggestion: fmt.Sprintf("The interface has %d methods: %s",
				actualCount, strings.Join(sym.Methods, ", ")),
		}
	}

	return nil
}

// parseReturnTypes parses a return type string into individual types.
// Examples: "(error)" -> ["error"], "(*Result, error)" -> ["*Result", "error"]
func parseReturnTypes(s string) []string {
	s = strings.TrimSpace(s)

	// Remove outer parens if present
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}

	// Split by comma
	parts := strings.Split(s, ",")
	var types []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			types = append(types, p)
		}
	}

	return types
}

// compareReturnTypes compares claimed return types to actual.
// Order matters for Go return types.
func compareReturnTypes(claimed, actual []string) bool {
	if len(claimed) != len(actual) {
		return false
	}

	for i := range claimed {
		if !typesMatch(claimed[i], actual[i]) {
			return false
		}
	}

	return true
}

// typesMatch compares two type strings for equivalence.
// Handles normalization like "*Result" matching "*Result".
func typesMatch(a, b string) bool {
	// Normalize
	a = normalizeType(a)
	b = normalizeType(b)

	return strings.EqualFold(a, b)
}

// normalizeType normalizes a type string for comparison.
func normalizeType(t string) string {
	t = strings.TrimSpace(t)

	// Handle generic type parameters: Result[T] -> Result
	if idx := strings.Index(t, "["); idx > 0 {
		t = t[:idx]
	}

	return t
}

// formatReturnTypes formats return types for display.
func formatReturnTypes(types []string) string {
	if len(types) == 0 {
		return "(nothing)"
	}
	if len(types) == 1 {
		return types[0]
	}
	return "(" + strings.Join(types, ", ") + ")"
}

// parseFieldNameList parses a list of field names from text.
// Handles formats like "A, B, C" or "A and B" or "A, B and C"
func parseFieldNameList(s string) []string {
	s = strings.TrimSpace(s)

	// Replace "and" with comma for uniform parsing
	s = strings.ReplaceAll(s, " and ", ", ")
	s = strings.ReplaceAll(s, " or ", ", ")

	// Split by comma
	parts := strings.Split(s, ",")
	var fields []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Extract just the identifier (first word)
		if idx := strings.IndexAny(p, " \t"); idx > 0 {
			p = p[:idx]
		}
		// Clean up any quotes or punctuation
		p = strings.Trim(p, "`'\",.")
		if p != "" && isValidFieldName(p) {
			fields = append(fields, p)
		}
	}

	return fields
}

// isValidFieldName checks if a string looks like a valid field name.
func isValidFieldName(s string) bool {
	if s == "" || len(s) < 2 {
		return false
	}
	// Must start with letter
	first := s[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return false
	}
	// Rest must be alphanumeric
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}
