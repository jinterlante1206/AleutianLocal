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
	"path/filepath"
	"regexp"
	"strings"
)

// Package-level compiled regexes for file reference extraction (compiled once).
var (
	// filePathPattern matches file paths with common code extensions.
	// Matches: path/to/file.go, ./relative/file.py, file.ts, etc.
	filePathPattern = regexp.MustCompile(
		`(?:^|[\s\"\'\` + "`" + `\(\[\{])` +
			`((?:[a-zA-Z_][a-zA-Z0-9_\-./\\]*)?` +
			`[a-zA-Z_][a-zA-Z0-9_\-]*\.` +
			`(?:go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt|scala|md|yaml|yml|json|toml|sh))` +
			`(?:$|[\s\"\'\` + "`" + `\)\]\}:,])`,
	)

	// backtickPathPattern matches file paths in backticks.
	// Matches: `path/to/file.go`, `file.py`
	backtickPathPattern = regexp.MustCompile(
		"`" + `([a-zA-Z_][a-zA-Z0-9_\-./\\]*\.` +
			`(?:go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt|scala|md|yaml|yml|json|toml|sh))` +
			"`",
	)

	// codeBlockPathPattern matches file paths mentioned in code blocks or inline code.
	// This is more permissive for paths inside code.
	codeBlockPathPattern = regexp.MustCompile(
		`(?:in|at|from|file|path)\s+` +
			`([a-zA-Z_][a-zA-Z0-9_\-./\\]*\.` +
			`(?:go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb|php|swift|kt|scala|md|yaml|yml|json|toml|sh))`,
	)
)

// PhantomFileChecker detects references to files that don't exist.
//
// This checker identifies when the LLM references files that are not present
// in the codebase. Phantom file references are a strong signal of hallucination
// because the model is inventing file paths that don't exist.
//
// This complements CitationChecker: CitationChecker validates citations that
// ARE in context, while PhantomFileChecker catches references to files that
// don't exist at all.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type PhantomFileChecker struct {
	config *PhantomCheckerConfig
}

// NewPhantomFileChecker creates a new phantom file checker.
//
// Description:
//
//	Creates a checker that detects references to non-existent files.
//	Uses CheckInput.KnownFiles to validate file existence.
//
// Inputs:
//   - config: Configuration for the checker (nil uses defaults).
//
// Outputs:
//   - *PhantomFileChecker: The configured checker.
//
// Thread Safety: Safe for concurrent use.
func NewPhantomFileChecker(config *PhantomCheckerConfig) *PhantomFileChecker {
	if config == nil {
		config = DefaultPhantomCheckerConfig()
	}

	return &PhantomFileChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *PhantomFileChecker) Name() string {
	return "phantom_file_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts file references from the response and validates they exist
//	in KnownFiles. Non-existent file references are flagged as
//	ViolationPhantomFile with CRITICAL severity.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - input: The check input containing response and KnownFiles.
//
// Outputs:
//   - []Violation: Any violations found.
//
// Thread Safety: Safe for concurrent use.
func (c *PhantomFileChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	// Need KnownFiles to validate against
	if input.KnownFiles == nil || len(input.KnownFiles) == 0 {
		return nil
	}

	var violations []Violation

	// Limit response size for performance
	response := input.Response
	if len(response) > 15000 {
		response = response[:15000]
	}

	// Extract file references from response
	refs := c.extractFileReferences(response)

	// Early exit if no file references found
	if len(refs) == 0 {
		return nil
	}

	// Limit number of references to check
	maxRefs := c.config.MaxRefsToCheck
	if maxRefs > 0 && len(refs) > maxRefs {
		refs = refs[:maxRefs]
	}

	// Check each reference against KnownFiles
	for _, ref := range refs {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		if !c.fileExists(ref, input.KnownFiles) {
			violations = append(violations, Violation{
				Type:     ViolationPhantomFile,
				Severity: SeverityCritical,
				Code:     "PHANTOM_FILE",
				Message:  fmt.Sprintf("Reference to non-existent file: %s", ref),
				Evidence: ref,
				Expected: "File should exist in the project",
				Suggestion: "Verify the file path is correct. Use file exploration tools " +
					"to discover actual files before referencing them.",
			})
		}
	}

	return violations
}

// extractFileReferences extracts file path references from the response.
//
// Description:
//
//	Uses multiple regex patterns to find file references in different
//	contexts (prose, backticks, code blocks). Deduplicates results.
//
// Inputs:
//   - response: The LLM response text.
//
// Outputs:
//   - []string: Unique file references found.
func (c *PhantomFileChecker) extractFileReferences(response string) []string {
	seen := make(map[string]bool)
	var refs []string

	addRef := func(ref string) {
		// Normalize the reference
		ref = strings.TrimSpace(ref)
		ref = strings.TrimPrefix(ref, "./")
		ref = filepath.ToSlash(ref)

		// Skip empty or very long paths (likely false positives)
		if ref == "" || len(ref) > 200 {
			return
		}

		// Skip if already seen
		if seen[ref] {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}

	// Extract from general pattern
	matches := filePathPattern.FindAllStringSubmatch(response, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			addRef(match[1])
		}
	}

	// Extract from backticks
	matches = backtickPathPattern.FindAllStringSubmatch(response, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			addRef(match[1])
		}
	}

	// Extract from "in file X" patterns
	matches = codeBlockPathPattern.FindAllStringSubmatch(response, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			addRef(match[1])
		}
	}

	return refs
}

// fileExists checks if a file reference exists in KnownFiles.
//
// Description:
//
//	Checks multiple variations of the path: as-is, normalized,
//	and basename-only. This handles cases where the model might
//	use different path formats than what's in KnownFiles.
//
// Inputs:
//   - ref: The file reference from the response.
//   - knownFiles: Map of known files in the project.
//
// Outputs:
//   - bool: True if the file exists.
func (c *PhantomFileChecker) fileExists(ref string, knownFiles map[string]bool) bool {
	// Check as-is
	if knownFiles[ref] {
		return true
	}

	// Check normalized
	normalized := normalizePath(ref)
	if knownFiles[normalized] {
		return true
	}

	// Check basename only (for simple references like "main.go")
	basename := filepath.Base(ref)
	if knownFiles[basename] {
		return true
	}

	// Check if any known file ends with this path (handles partial paths)
	for known := range knownFiles {
		if strings.HasSuffix(known, "/"+ref) || strings.HasSuffix(known, "/"+normalized) {
			return true
		}
	}

	return false
}
