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

// Package-level compiled regexes for relationship claim extraction.
var (
	// callClaimPattern matches "A calls/invokes/triggers B" patterns.
	// Group 1: caller, Group 2: verb, Group 3: callee
	// Allows both PascalCase and camelCase function names.
	callClaimPattern = regexp.MustCompile(
		`(?i)\b([a-zA-Z][a-zA-Z0-9_]*)\s+(calls?|invokes?|triggers?|executes?)\s+([a-zA-Z][a-zA-Z0-9_]*)\b`,
	)

	// callClaimReversePattern matches "B is called by A" patterns.
	// Group 1: callee, Group 2: caller
	// Allows both PascalCase and camelCase function names.
	callClaimReversePattern = regexp.MustCompile(
		`(?i)\b([a-zA-Z][a-zA-Z0-9_]*)\s+is\s+(?:called|invoked|triggered|executed)\s+by\s+([a-zA-Z][a-zA-Z0-9_]*)\b`,
	)

	// importClaimPattern matches "X imports/uses Y" patterns.
	// Group 1: importer (file/package), Group 2: verb, Group 3: imported package
	importClaimPattern = regexp.MustCompile(
		`(?i)\b([a-zA-Z][a-zA-Z0-9_./]*)\s+(imports?|uses?|depends\s+on|requires?)\s+["']?([a-zA-Z][a-zA-Z0-9_./\-]*)["']?\b`,
	)

	// importClaimReversePattern matches "Y is imported by X" patterns.
	// Group 1: imported package, Group 2: importer
	importClaimReversePattern = regexp.MustCompile(
		`(?i)\b["']?([a-zA-Z][a-zA-Z0-9_./\-]*)["']?\s+is\s+(?:imported|used|required)\s+by\s+([a-zA-Z][a-zA-Z0-9_./]*)\b`,
	)

	// commonEnglishWords are words that should not be treated as function/file names.
	// These words commonly appear before verbs like "calls" or "imports" in prose.
	commonEnglishWords = map[string]bool{
		"also": true, "then": true, "that": true, "this": true, "which": true,
		"who": true, "what": true, "when": true, "where": true, "why": true,
		"how": true, "and": true, "but": true, "or": true, "not": true,
		"the": true, "a": true, "an": true, "it": true, "is": true,
		"be": true, "to": true, "of": true, "in": true, "for": true,
		"on": true, "with": true, "as": true, "at": true, "by": true,
		"from": true, "if": true, "so": true, "too": true, "no": true,
		"yes": true, "code": true, "file": true, "function": true,
		"method": true, "class": true, "module": true, "package": true,
		"subsequently": true, "first": true, "next": true, "finally": true,
		"now": true, "later": true, "before": true, "after": true,
	}
)

// RelationshipKind categorizes types of relationships.
type RelationshipKind int

const (
	// ImportRelation is an import/dependency relationship.
	ImportRelation RelationshipKind = iota

	// CallRelation is a function call relationship.
	CallRelation
)

// RelationshipClaim represents a parsed relationship claim from response text.
type RelationshipClaim struct {
	// Subject is the entity making the relationship (caller, importer).
	Subject string

	// Object is the entity being related to (callee, imported package).
	Object string

	// Kind is the type of relationship.
	Kind RelationshipKind

	// Position is the character offset in the response.
	Position int

	// Raw is the original matched text.
	Raw string
}

// RelationshipChecker validates relationship claims in responses.
//
// This checker detects fabricated relationships including:
// - Function calls that don't exist ("A calls B" when A doesn't call B)
// - Imports that don't exist ("X imports Y" when X doesn't import Y)
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type RelationshipChecker struct {
	config *RelationshipCheckerConfig
}

// NewRelationshipChecker creates a new relationship checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*RelationshipChecker - The configured checker.
func NewRelationshipChecker(config *RelationshipCheckerConfig) *RelationshipChecker {
	if config == nil {
		config = DefaultRelationshipCheckerConfig()
	}
	return &RelationshipChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *RelationshipChecker) Name() string {
	return "relationship_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts relationship claims from the response and validates them against
//	the EvidenceIndex. Detects fabricated relationships including:
//	- Function calls that don't exist in the call graph
//	- Imports that don't exist in the import list
//
// Thread Safety: Safe for concurrent use.
func (c *RelationshipChecker) Check(ctx context.Context, input *CheckInput) []Violation {
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

	// Extract all relationship claims from response
	claims := c.extractRelationshipClaims(input.Response)

	// Limit claims to check
	if c.config.MaxRelationshipsToCheck > 0 && len(claims) > c.config.MaxRelationshipsToCheck {
		claims = claims[:c.config.MaxRelationshipsToCheck]
	}

	// Validate each claim
	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		var v *Violation
		switch claim.Kind {
		case ImportRelation:
			if c.config.ValidateImports {
				v = c.validateImportClaim(ctx, claim, input.EvidenceIndex)
			}
		case CallRelation:
			if c.config.ValidateCalls {
				v = c.validateCallClaim(ctx, claim, input.EvidenceIndex)
			}
		}

		if v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// isCommonEnglishWord returns true if the word is a common English word
// that should not be treated as a function or file name.
//
// The check only applies to all-lowercase words, since uppercase/mixed-case
// identifiers like "A", "DB", or "ProcessData" are likely valid symbols.
func isCommonEnglishWord(word string) bool {
	// Only filter all-lowercase words (e.g., "also", "then")
	// Uppercase or mixed-case words are likely valid identifiers
	if word != strings.ToLower(word) {
		return false
	}
	return commonEnglishWords[word]
}

// extractRelationshipClaims extracts all relationship claims from response text.
func (c *RelationshipChecker) extractRelationshipClaims(response string) []RelationshipClaim {
	var claims []RelationshipClaim
	seen := make(map[string]bool) // Dedup by subject+object+kind

	// Extract call claims: "A calls B"
	matches := callClaimPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 8 {
			continue
		}
		caller := response[match[2]:match[3]]
		callee := response[match[6]:match[7]]
		raw := response[match[0]:match[1]]

		// Skip if caller or callee is a common English word
		if isCommonEnglishWord(caller) || isCommonEnglishWord(callee) {
			continue
		}

		key := fmt.Sprintf("call:%s->%s", caller, callee)
		if !seen[key] {
			claims = append(claims, RelationshipClaim{
				Subject:  caller,
				Object:   callee,
				Kind:     CallRelation,
				Position: match[0],
				Raw:      raw,
			})
			seen[key] = true
		}
	}

	// Extract reverse call claims: "B is called by A"
	matches = callClaimReversePattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		callee := response[match[2]:match[3]]
		caller := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		// Skip if caller or callee is a common English word
		if isCommonEnglishWord(caller) || isCommonEnglishWord(callee) {
			continue
		}

		key := fmt.Sprintf("call:%s->%s", caller, callee)
		if !seen[key] {
			claims = append(claims, RelationshipClaim{
				Subject:  caller,
				Object:   callee,
				Kind:     CallRelation,
				Position: match[0],
				Raw:      raw,
			})
			seen[key] = true
		}
	}

	// Extract import claims: "X imports Y"
	matches = importClaimPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 8 {
			continue
		}
		importer := response[match[2]:match[3]]
		imported := response[match[6]:match[7]]
		raw := response[match[0]:match[1]]

		// Skip if importer is a common English word (imported packages can be short like "os")
		if isCommonEnglishWord(importer) {
			continue
		}

		key := fmt.Sprintf("import:%s->%s", importer, imported)
		if !seen[key] {
			claims = append(claims, RelationshipClaim{
				Subject:  importer,
				Object:   imported,
				Kind:     ImportRelation,
				Position: match[0],
				Raw:      raw,
			})
			seen[key] = true
		}
	}

	// Extract reverse import claims: "Y is imported by X"
	matches = importClaimReversePattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		imported := response[match[2]:match[3]]
		importer := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		// Skip if importer is a common English word
		if isCommonEnglishWord(importer) {
			continue
		}

		key := fmt.Sprintf("import:%s->%s", importer, imported)
		if !seen[key] {
			claims = append(claims, RelationshipClaim{
				Subject:  importer,
				Object:   imported,
				Kind:     ImportRelation,
				Position: match[0],
				Raw:      raw,
			})
			seen[key] = true
		}
	}

	return claims
}

// validateImportClaim validates an import claim against evidence.
func (c *RelationshipChecker) validateImportClaim(ctx context.Context, claim RelationshipClaim, idx *EvidenceIndex) *Violation {
	// If no imports data, skip validation (not a violation)
	if idx.Imports == nil || len(idx.Imports) == 0 {
		return nil
	}

	// Find the file that matches the subject
	var foundFile string
	var fileImports []ImportInfo

	for filePath, imports := range idx.Imports {
		// Match by filename (with or without extension)
		baseName := extractBaseName(filePath)
		if strings.EqualFold(baseName, claim.Subject) ||
			strings.EqualFold(filePath, claim.Subject) ||
			strings.Contains(strings.ToLower(filePath), strings.ToLower(claim.Subject)) {
			foundFile = filePath
			fileImports = imports
			break
		}
	}

	// If subject file not in evidence, skip validation
	if foundFile == "" {
		return nil
	}

	// Check if the claimed import exists
	for _, imp := range fileImports {
		// Match by alias or package name or full path
		if strings.EqualFold(imp.Alias, claim.Object) ||
			strings.EqualFold(imp.Path, claim.Object) ||
			strings.HasSuffix(strings.ToLower(imp.Path), "/"+strings.ToLower(claim.Object)) ||
			strings.EqualFold(extractPackageName(imp.Path), claim.Object) {
			return nil // Found - not a violation
		}
	}

	// Import not found - violation
	RecordRelationshipHallucination(ctx, "import", claim.Subject, claim.Object)
	return &Violation{
		Type:           ViolationRelationshipHallucination,
		Severity:       SeverityHigh,
		Code:           "IMPORT_NOT_FOUND",
		Message:        fmt.Sprintf("%s does not import %s", claim.Subject, claim.Object),
		Evidence:       claim.Raw,
		Expected:       fmt.Sprintf("Actual imports: %s", formatImports(fileImports)),
		Location:       foundFile,
		LocationOffset: claim.Position,
		Suggestion:     "Verify the import relationship against the actual code",
	}
}

// validateCallClaim validates a function call claim against evidence.
func (c *RelationshipChecker) validateCallClaim(ctx context.Context, claim RelationshipClaim, idx *EvidenceIndex) *Violation {
	// If no call graph data, skip validation (not a violation)
	if idx.CallsWithin == nil || len(idx.CallsWithin) == 0 {
		return nil
	}

	// Check if caller is known
	var callerCalls []string
	var foundCaller bool

	for funcName, calls := range idx.CallsWithin {
		if strings.EqualFold(funcName, claim.Subject) {
			foundCaller = true
			callerCalls = calls
			break
		}
	}

	// If caller not in evidence, skip validation
	if !foundCaller {
		return nil
	}

	// Check if callee is known (we only validate if both are in evidence)
	calleeKnown := idx.Symbols[claim.Object]
	if !calleeKnown {
		// Check if it's in SymbolDetails
		if _, ok := idx.SymbolDetails[claim.Object]; ok {
			calleeKnown = true
		}
	}

	// If callee not in evidence, skip validation
	if !calleeKnown {
		return nil
	}

	// Check if the call exists
	for _, call := range callerCalls {
		if strings.EqualFold(call, claim.Object) {
			return nil // Found - not a violation
		}
	}

	// Call not found - violation
	RecordRelationshipHallucination(ctx, "call", claim.Subject, claim.Object)
	return &Violation{
		Type:           ViolationRelationshipHallucination,
		Severity:       SeverityHigh,
		Code:           "CALL_NOT_FOUND",
		Message:        fmt.Sprintf("%s does not call %s", claim.Subject, claim.Object),
		Evidence:       claim.Raw,
		Expected:       fmt.Sprintf("%s calls: %s", claim.Subject, formatStringSlice(callerCalls)),
		LocationOffset: claim.Position,
		Suggestion:     "Verify the function call relationship against the actual code",
	}
}

// extractBaseName extracts the base name from a file path without extension.
func extractBaseName(filePath string) string {
	// Get the last path component
	lastSlash := strings.LastIndex(filePath, "/")
	name := filePath
	if lastSlash != -1 {
		name = filePath[lastSlash+1:]
	}

	// Remove extension
	if dotIdx := strings.LastIndex(name, "."); dotIdx != -1 {
		name = name[:dotIdx]
	}

	return name
}

// formatImports formats a list of imports for display.
func formatImports(imports []ImportInfo) string {
	if len(imports) == 0 {
		return "(none)"
	}

	var names []string
	for _, imp := range imports {
		if imp.Alias != "" && imp.Alias != extractPackageName(imp.Path) {
			names = append(names, fmt.Sprintf("%s (%s)", imp.Alias, imp.Path))
		} else {
			names = append(names, imp.Path)
		}
	}

	if len(names) > 5 {
		names = append(names[:5], "...")
	}

	return strings.Join(names, ", ")
}

// formatStringSlice formats a string slice for display.
func formatStringSlice(slice []string) string {
	if len(slice) == 0 {
		return "(none)"
	}

	if len(slice) > 5 {
		return strings.Join(slice[:5], ", ") + ", ..."
	}

	return strings.Join(slice, ", ")
}
