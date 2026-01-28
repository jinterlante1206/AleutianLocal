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

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/validation"
)

// OwnershipResolver resolves code ownership from CODEOWNERS file.
//
// # Description
//
// Parses GitHub/GitLab CODEOWNERS format to determine who owns the code
// and should review changes. Also aggregates owners from all affected
// files in the blast radius.
//
// # CODEOWNERS Format
//
// Standard GitHub/GitLab format:
//
//	# Comment
//	*.go @backend-team
//	/docs/ @docs-team @writers
//	src/auth/ @security-team @backend-team
//
// # Thread Safety
//
// Safe for concurrent use. Rules are loaded once at construction.
type OwnershipResolver struct {
	rules     []CodeOwnerRule
	validator *validation.InputValidator
	repoPath  string
}

// CodeOwnerRule represents a single CODEOWNERS rule.
type CodeOwnerRule struct {
	Pattern string   // Glob pattern (e.g., "*.go", "/src/auth/**")
	Owners  []string // List of owners (e.g., "@team", "user@example.com")
	Line    int      // Line number in CODEOWNERS file (for debugging)
}

// Verify interface compliance at compile time
var _ Enricher = (*OwnershipResolver)(nil)

// NewOwnershipResolver creates an ownership resolver for the repository.
//
// # Description
//
// Creates an OwnershipResolver by loading and parsing the CODEOWNERS file.
// Looks for CODEOWNERS in standard locations:
//   - CODEOWNERS
//   - .github/CODEOWNERS
//   - docs/CODEOWNERS
//
// # Inputs
//
//   - repoPath: Absolute path to the repository root.
//   - validator: Input validator. If nil, a default is created.
//
// # Outputs
//
//   - *OwnershipResolver: Ready-to-use resolver.
//   - error: Non-nil if CODEOWNERS cannot be found or parsed.
//
// # Example
//
//	resolver, err := NewOwnershipResolver("/path/to/repo", validator)
//	if err != nil {
//	    // CODEOWNERS not found - ownership will be nil
//	}
func NewOwnershipResolver(repoPath string, validator *validation.InputValidator) (*OwnershipResolver, error) {
	if validator == nil {
		validator = validation.NewInputValidator(nil)
	}

	// Find CODEOWNERS file
	codeownersPath, err := findCodeowners(repoPath)
	if err != nil {
		return nil, fmt.Errorf("CODEOWNERS not found: %w", err)
	}

	// Parse CODEOWNERS
	rules, err := parseCodeowners(codeownersPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CODEOWNERS: %w", err)
	}

	// Validate patterns don't contain path traversal
	for _, rule := range rules {
		if strings.Contains(rule.Pattern, "..") {
			return nil, fmt.Errorf("CODEOWNERS pattern contains path traversal: %s", rule.Pattern)
		}
	}

	return &OwnershipResolver{
		rules:     rules,
		validator: validator,
		repoPath:  repoPath,
	}, nil
}

// findCodeowners looks for CODEOWNERS in standard locations.
func findCodeowners(repoPath string) (string, error) {
	locations := []string{
		"CODEOWNERS",
		".github/CODEOWNERS",
		"docs/CODEOWNERS",
		".gitlab/CODEOWNERS",
	}

	for _, loc := range locations {
		path := filepath.Join(repoPath, loc)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("CODEOWNERS not found in standard locations")
}

// parseCodeowners parses a CODEOWNERS file.
func parseCodeowners(path string) ([]CodeOwnerRule, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rules []CodeOwnerRule
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse rule: pattern owner1 owner2 ...
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue // Invalid rule, skip
		}

		pattern := parts[0]
		owners := parts[1:]

		rules = append(rules, CodeOwnerRule{
			Pattern: pattern,
			Owners:  owners,
			Line:    lineNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

// Name returns the enricher identifier.
func (r *OwnershipResolver) Name() string {
	return "ownership"
}

// Priority returns 1 (critical analysis).
func (r *OwnershipResolver) Priority() int {
	return 1
}

// Enrich resolves ownership for the target and affected files.
//
// # Description
//
// Determines the primary owner for the target file and aggregates
// secondary owners from all files in the blast radius.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - target: The symbol to analyze.
//   - result: The result to enrich.
//
// # Outputs
//
//   - error: Non-nil on context cancellation.
func (r *OwnershipResolver) Enrich(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	ownership := &Ownership{
		OwnershipSource: "CODEOWNERS",
	}

	// Get file path from target
	filePath := ""
	if target.Symbol != nil {
		filePath = target.Symbol.FilePath
	} else {
		filePath = extractFileFromSymbolID(target.SymbolID)
	}

	if filePath != "" {
		// Resolve primary owner for target file
		owners := r.resolveOwners(filePath)
		if len(owners) > 0 {
			ownership.PrimaryOwner = owners[0]
		}
	}

	// Check context before expensive operation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Collect secondary owners from affected files
	if target.BaseResult != nil {
		secondaryOwners := make(map[string]bool)

		for _, file := range target.BaseResult.FilesAffected {
			owners := r.resolveOwners(file)
			for _, owner := range owners {
				if owner != ownership.PrimaryOwner {
					secondaryOwners[owner] = true
				}
			}

			// Check context periodically
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		// Convert map to slice
		for owner := range secondaryOwners {
			ownership.SecondaryOwners = append(ownership.SecondaryOwners, owner)
		}
	}

	// Only set if we found something
	if ownership.PrimaryOwner != "" || len(ownership.SecondaryOwners) > 0 {
		result.Ownership = ownership
	}

	return nil
}

// resolveOwners finds the owners for a given file path.
// Uses last-match-wins semantics (same as GitHub CODEOWNERS).
func (r *OwnershipResolver) resolveOwners(filePath string) []string {
	var lastMatch []string

	for _, rule := range r.rules {
		if r.matchPattern(rule.Pattern, filePath) {
			lastMatch = rule.Owners
		}
	}

	return lastMatch
}

// matchPattern checks if a CODEOWNERS pattern matches a file path.
//
// # Pattern Syntax
//
//   - `*` matches any file in the root (e.g., `*.go` matches `main.go`)
//   - `**` matches zero or more directories (e.g., `src/**` matches `src/a/b.go`)
//   - `/path/` matches exact directory
//   - No leading `/` matches anywhere in tree
func (r *OwnershipResolver) matchPattern(pattern, filePath string) bool {
	// Normalize paths
	pattern = strings.TrimSuffix(pattern, "/")
	filePath = strings.TrimPrefix(filePath, "/")

	// Handle absolute patterns (start with /)
	if strings.HasPrefix(pattern, "/") {
		pattern = strings.TrimPrefix(pattern, "/")
		return r.matchGlob(pattern, filePath)
	}

	// For relative patterns, try matching at any level
	// First try exact match
	if r.matchGlob(pattern, filePath) {
		return true
	}

	// Try matching as suffix (pattern appears in path)
	parts := strings.Split(filePath, "/")
	for i := range parts {
		subPath := strings.Join(parts[i:], "/")
		if r.matchGlob(pattern, subPath) {
			return true
		}
	}

	return false
}

// matchGlob performs simple glob matching.
func (r *OwnershipResolver) matchGlob(pattern, path string) bool {
	// Handle ** (any directory depth)
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) != 2 {
			// Multiple ** not supported, fall back to simple match
			return pattern == path
		}
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")

		hasPrefix := prefix == "" || strings.HasPrefix(path, prefix+"/") || strings.HasPrefix(path, prefix)
		hasSuffix := suffix == "" || strings.HasSuffix(path, "/"+suffix) || strings.HasSuffix(path, suffix)

		return hasPrefix && hasSuffix
	}

	// Handle * (single level wildcard)
	if strings.Contains(pattern, "*") {
		// Simple case: *.ext matches any file with that extension
		if strings.HasPrefix(pattern, "*.") {
			ext := strings.TrimPrefix(pattern, "*")
			return strings.HasSuffix(path, ext)
		}

		// Pattern like dir/*.go
		patternParts := strings.Split(pattern, "/")
		pathParts := strings.Split(path, "/")

		if len(patternParts) > len(pathParts) {
			return false
		}

		// Match from end
		offset := len(pathParts) - len(patternParts)
		for i, pp := range patternParts {
			if !r.matchWildcard(pp, pathParts[offset+i]) {
				return false
			}
		}
		return true
	}

	// Exact match
	return pattern == path || strings.HasPrefix(path, pattern+"/")
}

// matchWildcard matches a single path component with possible wildcards.
func (r *OwnershipResolver) matchWildcard(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}

	// Handle *.ext
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(name, suffix)
	}

	// Handle prefix*
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}

	// Handle prefix*suffix
	parts := strings.SplitN(pattern, "*", 2)
	return strings.HasPrefix(name, parts[0]) && strings.HasSuffix(name, parts[1])
}

// GetRules returns the parsed CODEOWNERS rules for inspection.
func (r *OwnershipResolver) GetRules() []CodeOwnerRule {
	// Return a copy to prevent mutation
	rules := make([]CodeOwnerRule, len(r.rules))
	copy(rules, r.rules)
	return rules
}
