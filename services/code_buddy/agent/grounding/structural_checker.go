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
	"strings"
)

// Package-level compiled regexes for structural claim detection (compiled once).
var (
	// Unicode tree markers: ├── └── │
	unicodeTreeMarkers = regexp.MustCompile(`[├└│]──|[├└]─`)

	// ASCII tree markers: +-- |-- `--
	asciiTreeMarkers = regexp.MustCompile(`[\+\|` + "`]" + `--`)

	// Structural phrases indicating directory/file enumeration.
	structuralPhrases = regexp.MustCompile(
		`(?i)(directory structure|project structure|folder structure|` +
			`project layout|file structure|codebase structure|` +
			`the project contains|files and directories|` +
			`organized as follows|structured as follows)`,
	)

	// Path list patterns: paths ending in / or file extensions in lists.
	pathListPattern = regexp.MustCompile(`(?m)^[\s\-\*]*[a-zA-Z_][a-zA-Z0-9_\-./]*(/|\.go|\.py|\.js|\.ts|\.java|\.rs)\s*$`)

	// Extract paths from tree structures.
	// Matches: ├── filename.go, └── dirname/, +-- file.py, etc.
	treePathExtractor = regexp.MustCompile(
		`(?:[├└│\|\+` + "`" + `][\s─\-]*)+\s*([a-zA-Z_][a-zA-Z0-9_\-.]*)(/?)`,
	)

	// Tool commands that provide structural evidence.
	structuralToolPatterns = regexp.MustCompile(
		`(?i)(ls\s+|find\s+|tree\s+|` +
			`\$ ls|output of ls|from ls|` +
			`\$ find|output of find|from find|` +
			`\$ tree|output of tree|from tree|` +
			`glob\s+result|search\s+result|file\s+list)`,
	)
)

// StructuralClaimChecker detects structural claims without supporting evidence.
//
// This checker identifies when the LLM describes directory structures or file
// lists without having tool evidence (ls, find, tree output) to support those
// claims. Unsupported structural claims often indicate the model is fabricating
// a "generic project" structure rather than describing the actual codebase.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type StructuralClaimChecker struct {
	config *StructuralClaimCheckerConfig
}

// NewStructuralClaimChecker creates a new structural claim checker.
//
// Description:
//
//	Creates a checker that detects structural claims (directory trees, file lists)
//	and validates they are backed by tool evidence.
//
// Inputs:
//   - config: Configuration for the checker (nil uses defaults).
//
// Outputs:
//   - *StructuralClaimChecker: The configured checker.
//
// Thread Safety: Safe for concurrent use.
func NewStructuralClaimChecker(config *StructuralClaimCheckerConfig) *StructuralClaimChecker {
	if config == nil {
		config = DefaultStructuralClaimCheckerConfig()
	}

	return &StructuralClaimChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *StructuralClaimChecker) Name() string {
	return "structural_claim_checker"
}

// Check implements Checker.
//
// Description:
//
//	Scans the response for structural claims (directory trees, file lists)
//	and verifies they are backed by tool evidence. Structural claims without
//	supporting evidence are flagged as ViolationStructuralClaim.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - input: The check input containing response and evidence.
//
// Outputs:
//   - []Violation: Any violations found.
//
// Thread Safety: Safe for concurrent use.
func (c *StructuralClaimChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	var violations []Violation

	// Limit response size for performance
	response := input.Response
	if len(response) > 15000 {
		response = response[:15000]
	}

	// Step 1: Check if response contains structural claims
	if !c.hasStructuralClaim(response) {
		return nil
	}

	select {
	case <-ctx.Done():
		return violations
	default:
	}

	// Step 2: Check if we have structural evidence
	if c.hasStructuralEvidence(input) {
		return nil
	}

	// Step 3: Extract claimed paths for the violation evidence
	claimedPaths := c.extractClaimedPaths(response)

	// Step 4: Create violation
	evidence := "Directory tree or file list in response"
	if len(claimedPaths) > 0 {
		// Show first few claimed paths as evidence
		maxShow := 5
		if len(claimedPaths) < maxShow {
			maxShow = len(claimedPaths)
		}
		evidence = "Claimed paths: " + strings.Join(claimedPaths[:maxShow], ", ")
		if len(claimedPaths) > maxShow {
			evidence += "..."
		}
	}

	violations = append(violations, Violation{
		Type:     ViolationStructuralClaim,
		Severity: SeverityHigh,
		Code:     "STRUCTURAL_CLAIM_NO_EVIDENCE",
		Message:  "Response contains structural claims (directory tree/file list) without tool evidence",
		Evidence: evidence,
		Expected: "Structural claims should be backed by ls/find/tree output or file search results",
		Suggestion: "Use file exploration tools before describing project structure. " +
			"Cite the tool output that shows the actual directory layout.",
	})

	return violations
}

// hasStructuralClaim detects if the response contains structural claims.
//
// Description:
//
//	Checks for tree markers (Unicode/ASCII), structural phrases, and
//	path list patterns that indicate the response is describing file
//	or directory structure.
//
// Inputs:
//   - response: The LLM response text.
//
// Outputs:
//   - bool: True if structural claims are detected.
func (c *StructuralClaimChecker) hasStructuralClaim(response string) bool {
	// Check for Unicode tree markers (├── └── │)
	if unicodeTreeMarkers.MatchString(response) {
		return true
	}

	// Check for ASCII tree markers (+-- |-- `--)
	if asciiTreeMarkers.MatchString(response) {
		return true
	}

	// Check for structural phrases
	if structuralPhrases.MatchString(response) {
		// If structural phrase found, also check for path-like content
		if pathListPattern.MatchString(response) {
			return true
		}
	}

	// Check for multiple path-like lines (indicating a file list)
	matches := pathListPattern.FindAllString(response, -1)
	if len(matches) >= 3 {
		return true
	}

	return false
}

// hasStructuralEvidence checks if tool results contain structural evidence.
//
// Description:
//
//	Checks tool results and evidence index for ls/find/tree output or
//	similar structural information that would justify structural claims.
//
// Inputs:
//   - input: The check input containing tool results and evidence.
//
// Outputs:
//   - bool: True if structural evidence is found.
func (c *StructuralClaimChecker) hasStructuralEvidence(input *CheckInput) bool {
	// Check tool results for structural commands
	for _, result := range input.ToolResults {
		if structuralToolPatterns.MatchString(result.Output) {
			return true
		}
		// Also check for directory listing patterns in output
		if c.looksLikeDirectoryListing(result.Output) {
			return true
		}
	}

	// Check if EvidenceIndex has substantial file information
	if input.EvidenceIndex != nil {
		// If we have multiple files in evidence, structural claims may be justified
		if len(input.EvidenceIndex.Files) >= 3 {
			return true
		}
	}

	// Check response itself for tool citation patterns
	if structuralToolPatterns.MatchString(input.Response) {
		return true
	}

	return false
}

// looksLikeDirectoryListing checks if output appears to be a directory listing.
//
// Description:
//
//	Heuristic check for directory listing output (multiple file paths
//	on separate lines).
//
// Inputs:
//   - output: Tool output to check.
//
// Outputs:
//   - bool: True if output looks like a directory listing.
func (c *StructuralClaimChecker) looksLikeDirectoryListing(output string) bool {
	lines := strings.Split(output, "\n")
	pathCount := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		// Check if line looks like a file path
		if strings.Contains(line, "/") || c.hasCodeFileExtension(line) {
			pathCount++
		}
	}

	// If we see multiple path-like lines, it's likely a directory listing
	return pathCount >= 3
}

// hasCodeFileExtension checks if a string ends with a common code file extension.
//
// Description:
//
//	Checks if the given string ends with a recognized source code or
//	configuration file extension. Used to identify file paths in output.
//
// Inputs:
//   - s: String to check (typically a file path or name).
//
// Outputs:
//   - bool: True if the string ends with a recognized extension.
//
// Thread Safety: Safe for concurrent use (pure function).
func (c *StructuralClaimChecker) hasCodeFileExtension(s string) bool {
	extensions := []string{
		".go", ".py", ".js", ".ts", ".jsx", ".tsx",
		".java", ".rs", ".c", ".cpp", ".h", ".hpp",
		".rb", ".php", ".swift", ".kt", ".scala",
		".md", ".yaml", ".yml", ".json", ".toml",
		".sh", ".bash", ".zsh",
	}
	for _, ext := range extensions {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}
	return false
}

// extractClaimedPaths extracts file/directory paths from structural claims.
//
// Description:
//
//	Parses tree structures and path lists to extract the paths being claimed.
//	These are used for violation evidence and could be used for further
//	validation against KnownFiles.
//
// Inputs:
//   - response: The LLM response text.
//
// Outputs:
//   - []string: Extracted paths (may include both files and directories).
func (c *StructuralClaimChecker) extractClaimedPaths(response string) []string {
	var paths []string
	seen := make(map[string]bool)

	// Extract from tree structures
	matches := treePathExtractor.FindAllStringSubmatch(response, c.config.MaxPathsToExtract)
	for _, match := range matches {
		if len(match) >= 2 {
			path := match[1]
			if len(match) >= 3 && match[2] == "/" {
				path += "/" // Preserve directory indicator
			}
			if !seen[path] && len(path) > 0 {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}

	// Extract from path list patterns
	listMatches := pathListPattern.FindAllString(response, c.config.MaxPathsToExtract-len(paths))
	for _, match := range listMatches {
		path := strings.TrimSpace(match)
		path = strings.TrimLeft(path, "-* ")
		if !seen[path] && len(path) > 0 {
			seen[path] = true
			paths = append(paths, path)
		}
	}

	return paths
}
