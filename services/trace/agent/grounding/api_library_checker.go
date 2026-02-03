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

// LibraryClaimType categorizes types of library claims.
type LibraryClaimType int

const (
	// ClaimLibraryUsage is a claim like "uses library X" or "depends on X".
	ClaimLibraryUsage LibraryClaimType = iota

	// ClaimAPICall is a claim like "calls pkg.Function()" or "uses pkg.Method".
	ClaimAPICall
)

// String returns the string representation of a LibraryClaimType.
func (c LibraryClaimType) String() string {
	switch c {
	case ClaimLibraryUsage:
		return "library_usage"
	case ClaimAPICall:
		return "api_call"
	default:
		return "unknown"
	}
}

// LibraryClaim represents a parsed library/API claim from response text.
type LibraryClaim struct {
	// Type is the kind of library claim.
	Type LibraryClaimType

	// Library is the claimed library name (e.g., "gorm", "gin", "sqlx").
	Library string

	// Method is the claimed method/function (for API calls, e.g., "Open", "Get").
	Method string

	// Position is the character offset in the response.
	Position int

	// Raw is the original matched text.
	Raw string
}

// libraryConfusions maps libraries to their commonly confused alternatives.
// If response mentions key but evidence shows a value, it's a confusion.
var libraryConfusions = map[string][]string{
	// Go ORM/database libraries
	"gorm":         {"sqlx", "database/sql", "ent", "bun", "sqlc"},
	"sqlx":         {"gorm", "database/sql", "ent", "bun", "sqlc"},
	"ent":          {"gorm", "sqlx", "database/sql", "bun"},
	"database/sql": {"gorm", "sqlx", "ent", "bun"},

	// Go HTTP frameworks
	"gin":      {"echo", "chi", "fiber", "net/http", "gorilla/mux"},
	"echo":     {"gin", "chi", "fiber", "net/http", "gorilla/mux"},
	"chi":      {"gin", "echo", "fiber", "net/http", "gorilla/mux"},
	"fiber":    {"gin", "echo", "chi", "net/http", "gorilla/mux"},
	"net/http": {"gin", "echo", "chi", "fiber", "gorilla/mux"},

	// Go logging libraries
	"logrus":   {"zap", "zerolog", "log/slog", "log"},
	"zap":      {"logrus", "zerolog", "log/slog", "log"},
	"zerolog":  {"logrus", "zap", "log/slog", "log"},
	"log/slog": {"logrus", "zap", "zerolog", "log"},

	// Go config libraries
	"viper":     {"envconfig", "godotenv", "koanf"},
	"envconfig": {"viper", "godotenv", "koanf"},

	// Python web frameworks
	"flask":   {"django", "fastapi", "bottle", "tornado"},
	"django":  {"flask", "fastapi", "pyramid"},
	"fastapi": {"flask", "django", "starlette"},

	// Python HTTP clients
	"requests": {"httpx", "urllib", "aiohttp"},
	"httpx":    {"requests", "urllib", "aiohttp"},

	// Python ORM/database
	"sqlalchemy": {"django.db", "peewee", "tortoise-orm"},
	"peewee":     {"sqlalchemy", "django.db", "tortoise-orm"},
}

// Package-level compiled regexes for library claim extraction.
var (
	// libraryUsagePattern matches "uses/imports/depends on library X" patterns.
	// Group 1: verb, Group 2: library name
	libraryUsagePattern = regexp.MustCompile(
		`(?i)\b(uses?|imports?|depends\s+on|requires?|includes?)\s+(?:the\s+)?(?:library\s+)?["']?([a-zA-Z][a-zA-Z0-9_.\-/]*)["']?\b`,
	)

	// apiCallPattern matches "pkg.Method()" or "pkg.Function" patterns.
	// Group 1: package name, Group 2: method/function name
	apiCallPattern = regexp.MustCompile(
		`\b([a-zA-Z][a-zA-Z0-9_]*)\.([A-Z][a-zA-Z0-9_]*)\s*\(`,
	)

	// explicitLibraryPattern matches "X library" or "X package" patterns.
	// Group 1: library name
	explicitLibraryPattern = regexp.MustCompile(
		`(?i)\b([a-zA-Z][a-zA-Z0-9_.\-/]*)\s+(?:library|package|framework|module)\b`,
	)
)

// APILibraryChecker validates library/API claims in responses.
//
// This checker detects:
// - Claims about libraries not in project imports
// - Library confusion (similar libraries mixed up)
// - API call patterns not found in evidence
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type APILibraryChecker struct {
	config *APILibraryCheckerConfig
}

// NewAPILibraryChecker creates a new API/library checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*APILibraryChecker - The configured checker.
func NewAPILibraryChecker(config *APILibraryCheckerConfig) *APILibraryChecker {
	if config == nil {
		config = DefaultAPILibraryCheckerConfig()
	}
	return &APILibraryChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *APILibraryChecker) Name() string {
	return "api_library_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts library claims from the response and validates them against
//	the EvidenceIndex.Imports. Detects library hallucinations and confusions.
//
// Thread Safety: Safe for concurrent use.
func (c *APILibraryChecker) Check(ctx context.Context, input *CheckInput) []Violation {
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

	// Build set of imported libraries from evidence
	importedLibraries := c.buildImportedLibraries(input.EvidenceIndex)

	// If no imports in evidence, we can't validate - skip
	if len(importedLibraries) == 0 {
		return nil
	}

	var violations []Violation

	// Extract library claims from response
	claims := c.extractLibraryClaims(input.Response)

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

		v := c.validateClaim(ctx, claim, importedLibraries, input)
		if v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// buildImportedLibraries extracts all library names from evidence imports.
func (c *APILibraryChecker) buildImportedLibraries(idx *EvidenceIndex) map[string]bool {
	libraries := make(map[string]bool)

	// From Imports map
	for _, imports := range idx.Imports {
		for _, imp := range imports {
			// Extract library name from import path
			libName := extractLibraryName(imp.Path)
			if libName != "" {
				libraries[strings.ToLower(libName)] = true
			}

			// Also add alias if present
			if imp.Alias != "" && imp.Alias != "_" && imp.Alias != "." {
				libraries[strings.ToLower(imp.Alias)] = true
			}

			// Add full path for exact matching
			libraries[strings.ToLower(imp.Path)] = true
		}
	}

	// From Frameworks map (if populated)
	for framework := range idx.Frameworks {
		libraries[strings.ToLower(framework)] = true
	}

	return libraries
}

// extractLibraryName extracts the library name from an import path.
// "github.com/gin-gonic/gin" -> "gin"
// "gorm.io/gorm" -> "gorm"
// "database/sql" -> "sql"
// "github.com/labstack/echo/v4" -> "echo"
// "example.com/pkg/v10" -> "pkg"
func extractLibraryName(importPath string) string {
	if importPath == "" {
		return ""
	}

	// Split by /
	parts := strings.Split(importPath, "/")
	if len(parts) == 0 {
		return ""
	}

	// Get last part
	lastPart := parts[len(parts)-1]

	// Handle versioned paths like "v2", "v10", "v123"
	if len(lastPart) >= 2 && lastPart[0] == 'v' && len(parts) > 1 {
		// Check if it looks like a version (v followed by digits only)
		isVersion := true
		for i := 1; i < len(lastPart); i++ {
			if lastPart[i] < '0' || lastPart[i] > '9' {
				isVersion = false
				break
			}
		}
		if isVersion {
			lastPart = parts[len(parts)-2]
		}
	}

	return lastPart
}

// extractLibraryClaims extracts all library claims from response text.
func (c *APILibraryChecker) extractLibraryClaims(response string) []LibraryClaim {
	var claims []LibraryClaim
	seen := make(map[string]bool) // Dedup by library name

	// Extract library usage claims ("uses X", "imports X")
	matches := libraryUsagePattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		claim := c.parseLibraryUsageMatch(response, match)
		if claim != nil && len(claim.Library) >= c.config.MinLibraryNameLength {
			key := strings.ToLower(claim.Library)
			if !seen[key] {
				claims = append(claims, *claim)
				seen[key] = true
			}
		}
	}

	// Extract explicit library patterns ("X library", "X framework")
	explicitMatches := explicitLibraryPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range explicitMatches {
		claim := c.parseExplicitLibraryMatch(response, match)
		if claim != nil && len(claim.Library) >= c.config.MinLibraryNameLength {
			key := strings.ToLower(claim.Library)
			if !seen[key] {
				claims = append(claims, *claim)
				seen[key] = true
			}
		}
	}

	// Extract API call claims ("pkg.Method()")
	apiMatches := apiCallPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range apiMatches {
		claim := c.parseAPICallMatch(response, match)
		if claim != nil && len(claim.Library) >= c.config.MinLibraryNameLength {
			// For API calls, key includes method to avoid dedup with library usage
			key := strings.ToLower(claim.Library + "." + claim.Method)
			if !seen[key] {
				claims = append(claims, *claim)
				seen[key] = true
			}
		}
	}

	return claims
}

// parseLibraryUsageMatch parses a library usage regex match.
func (c *APILibraryChecker) parseLibraryUsageMatch(response string, match []int) *LibraryClaim {
	if len(match) < 6 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Extract library name (group 2)
	if match[4] == -1 || match[5] == -1 {
		return nil
	}
	library := response[match[4]:match[5]]

	// Skip common false positives
	if isCommonLibraryWord(library) {
		return nil
	}

	// Skip if it looks like an API call (e.g., "gorm.Open" followed by "(")
	// This happens when libraryUsagePattern matches what should be an API call
	if looksLikeAPICall(library, response, match[5]) {
		return nil
	}

	return &LibraryClaim{
		Type:     ClaimLibraryUsage,
		Library:  library,
		Position: match[0],
		Raw:      raw,
	}
}

// looksLikeAPICall checks if a library name looks like it's actually an API call pattern.
// "gorm.Open" followed by "(" should be treated as API call, not library name.
func looksLikeAPICall(library string, response string, endPos int) bool {
	// Check if library has a dot followed by an uppercase letter (likely pkg.Method)
	for i := 0; i < len(library)-1; i++ {
		if library[i] == '.' && i+1 < len(library) && library[i+1] >= 'A' && library[i+1] <= 'Z' {
			return true
		}
	}

	// Also check if followed by "(" in the response
	if endPos < len(response) {
		remaining := response[endPos:]
		for _, c := range remaining {
			if c == ' ' || c == '\t' {
				continue
			}
			if c == '(' {
				return true
			}
			break
		}
	}

	return false
}

// parseExplicitLibraryMatch parses an explicit library pattern match.
func (c *APILibraryChecker) parseExplicitLibraryMatch(response string, match []int) *LibraryClaim {
	if len(match) < 4 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Extract library name (group 1)
	if match[2] == -1 || match[3] == -1 {
		return nil
	}
	library := response[match[2]:match[3]]

	// Skip common false positives
	if isCommonLibraryWord(library) {
		return nil
	}

	return &LibraryClaim{
		Type:     ClaimLibraryUsage,
		Library:  library,
		Position: match[0],
		Raw:      raw,
	}
}

// parseAPICallMatch parses an API call regex match.
func (c *APILibraryChecker) parseAPICallMatch(response string, match []int) *LibraryClaim {
	if len(match) < 6 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Extract package name (group 1)
	if match[2] == -1 || match[3] == -1 {
		return nil
	}
	pkg := response[match[2]:match[3]]

	// Extract method name (group 2)
	if match[4] == -1 || match[5] == -1 {
		return nil
	}
	method := response[match[4]:match[5]]

	// Skip common false positive packages
	if isCommonLibraryWord(pkg) || isBuiltinPackage(pkg) {
		return nil
	}

	return &LibraryClaim{
		Type:     ClaimAPICall,
		Library:  pkg,
		Method:   method,
		Position: match[0],
		Raw:      raw,
	}
}

// validateClaim validates a single library claim.
func (c *APILibraryChecker) validateClaim(ctx context.Context, claim LibraryClaim, importedLibraries map[string]bool, input *CheckInput) *Violation {
	libraryLower := strings.ToLower(claim.Library)

	// Check if library exists in imports
	if !c.libraryExistsInEvidence(libraryLower, importedLibraries) {
		// Library not found - check for confusion with similar libraries
		if c.config.CheckLibraryConfusion {
			confusedWith := c.findConfusedLibrary(libraryLower, importedLibraries)
			if confusedWith != "" {
				RecordAPIHallucination(ctx, "library_confusion", claim.Library, confusedWith)
				return &Violation{
					Type:     ViolationAPIHallucination,
					Severity: SeverityHigh,
					Code:     "API_LIBRARY_CONFUSION",
					Message: fmt.Sprintf(
						"Response mentions '%s' but evidence shows '%s' is used instead",
						claim.Library, confusedWith,
					),
					Evidence:   claim.Raw,
					Expected:   fmt.Sprintf("Reference to '%s' which is actually imported", confusedWith),
					Suggestion: fmt.Sprintf("Use '%s' instead of '%s' based on project imports", confusedWith, claim.Library),
				}
			}
		}

		// No confusion found - just missing library
		if c.config.CheckLibraryExists {
			RecordAPIHallucination(ctx, "library_missing", claim.Library, "")
			return &Violation{
				Type:     ViolationAPIHallucination,
				Severity: SeverityWarning,
				Code:     "API_LIBRARY_NOT_IMPORTED",
				Message: fmt.Sprintf(
					"Response mentions '%s' but this library is not in project imports",
					claim.Library,
				),
				Evidence:   claim.Raw,
				Expected:   "Reference to libraries actually imported in the project",
				Suggestion: "Verify the library name against actual imports in the codebase",
			}
		}
	}

	// For API calls, optionally check if the usage pattern exists in evidence
	if claim.Type == ClaimAPICall && c.config.CheckAPIUsageInEvidence {
		apiPattern := fmt.Sprintf("%s.%s", claim.Library, claim.Method)
		if !c.apiPatternExistsInEvidence(apiPattern, input.EvidenceIndex) {
			RecordAPIHallucination(ctx, "api_not_found", claim.Library, claim.Method)
			return &Violation{
				Type:     ViolationAPIHallucination,
				Severity: SeverityWarning,
				Code:     "API_CALL_NOT_IN_EVIDENCE",
				Message: fmt.Sprintf(
					"Response mentions '%s.%s()' but this pattern is not found in evidence",
					claim.Library, claim.Method,
				),
				Evidence:   claim.Raw,
				Expected:   "API calls that appear in the shown code",
				Suggestion: "Verify the API call against actual usage in the codebase",
			}
		}
	}

	return nil
}

// libraryExistsInEvidence checks if a library name appears in imported libraries.
func (c *APILibraryChecker) libraryExistsInEvidence(library string, importedLibraries map[string]bool) bool {
	// Direct match
	if importedLibraries[library] {
		return true
	}

	// Check if it's a suffix of any imported library path
	for imported := range importedLibraries {
		if strings.HasSuffix(imported, "/"+library) || strings.HasSuffix(imported, "."+library) {
			return true
		}
	}

	return false
}

// findConfusedLibrary checks if a claimed library has a confused alternative in evidence.
func (c *APILibraryChecker) findConfusedLibrary(claimed string, importedLibraries map[string]bool) string {
	// Get the confusion pairs for the claimed library
	confusables, ok := libraryConfusions[claimed]
	if !ok {
		return ""
	}

	// Check if any confusable library is in the imports
	for _, confusable := range confusables {
		confusableLower := strings.ToLower(confusable)
		if c.libraryExistsInEvidence(confusableLower, importedLibraries) {
			return confusable
		}
	}

	return ""
}

// apiPatternExistsInEvidence checks if an API pattern appears in evidence file contents.
//
// Returns true (don't flag) if:
// - Pattern is found in evidence
// - No evidence content exists to check against (benefit of the doubt)
//
// Returns false (flag) only if:
// - Evidence content exists AND pattern is not found
func (c *APILibraryChecker) apiPatternExistsInEvidence(pattern string, idx *EvidenceIndex) bool {
	if idx == nil {
		return true // No index to check - can't verify, don't flag
	}

	// Check if we have any content to verify against
	hasContent := len(idx.FileContents) > 0 || idx.RawContent != ""
	if !hasContent {
		return true // No content to check - can't verify, don't flag
	}

	patternLower := strings.ToLower(pattern)

	// Search in file contents
	for _, content := range idx.FileContents {
		if strings.Contains(strings.ToLower(content), patternLower) {
			return true
		}
	}

	// Search in raw content
	if idx.RawContent != "" && strings.Contains(strings.ToLower(idx.RawContent), patternLower) {
		return true
	}

	return false // Content exists but pattern not found - flag it
}

// isCommonLibraryWord returns true if the word is a common English word that's not a library.
func isCommonLibraryWord(word string) bool {
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "it": true, "is": true,
		"be": true, "to": true, "of": true, "in": true, "for": true,
		"on": true, "with": true, "as": true, "at": true, "by": true,
		"from": true, "if": true, "so": true, "this": true, "that": true,
		"code": true, "file": true, "function": true, "method": true,
		"class": true, "module": true, "package": true, "type": true,
		"var": true, "const": true, "let": true, "def": true,
		"data": true, "value": true, "result": true, "error": true,
		"api": true, "url": true, "http": true, "json": true,
	}
	return commonWords[strings.ToLower(word)]
}

// isBuiltinPackage returns true if the package is a Go builtin that shouldn't be flagged.
func isBuiltinPackage(pkg string) bool {
	builtins := map[string]bool{
		"fmt":     true,
		"log":     true,
		"os":      true,
		"io":      true,
		"strings": true,
		"strconv": true,
		"time":    true,
		"context": true,
		"errors":  true,
		"bytes":   true,
		"bufio":   true,
		"sort":    true,
		"sync":    true,
		"math":    true,
		"regexp":  true,
		"path":    true,
		"net":     true,
		"testing": true,
		"reflect": true,
		"runtime": true,
		"unsafe":  true,
	}
	return builtins[strings.ToLower(pkg)]
}
