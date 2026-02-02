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
	"strconv"
	"strings"
)

// CitationChecker validates [file:line] citations in responses.
//
// This checker extracts citations from the LLM response and validates:
// - File exists in the project
// - File was shown in context
// - Line number is within valid range
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type CitationChecker struct {
	config          *CitationCheckerConfig
	citationPattern *regexp.Regexp
	claimPatterns   []*regexp.Regexp
}

// NewCitationChecker creates a new citation checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*CitationChecker - The configured checker.
func NewCitationChecker(config *CitationCheckerConfig) *CitationChecker {
	if config == nil {
		config = DefaultCitationCheckerConfig()
	}

	return &CitationChecker{
		config: config,
		// Matches [file.go:45] or [file.go:45-50]
		citationPattern: regexp.MustCompile(
			`\[([^\[\]:]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|md|yaml|yml|json)):(\d+)(?:-(\d+))?\]`,
		),
		claimPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)the\s+(\w+)\s+function`),
			regexp.MustCompile(`(?i)function\s+(\w+)`),
			regexp.MustCompile(`(?i)in\s+(\w+\.go)`),
			regexp.MustCompile(`(?i)the\s+file\s+(\w+\.\w+)`),
			regexp.MustCompile(`(?i)this\s+(code|file|function|method)`),
			regexp.MustCompile(`(?i)the\s+(main|init|setup|handle\w*)\s+function`),
		},
	}
}

// Name implements Checker.
func (c *CitationChecker) Name() string {
	return "citation_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts and validates all [file:line] citations in the response.
//	Also detects if the response makes claims without citations.
//
// Thread Safety: Safe for concurrent use.
func (c *CitationChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	var violations []Violation

	// Extract citations from response
	citations := c.extractCitations(input.Response)

	// Validate each citation
	for _, cit := range citations {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		// Level 1: Does file exist in project?
		if c.config.ValidateFileExists && input.KnownFiles != nil {
			normalizedPath := normalizePath(cit.FilePath)
			basename := filepath.Base(cit.FilePath)

			fileExists := input.KnownFiles[normalizedPath] ||
				input.KnownFiles[cit.FilePath] ||
				input.KnownFiles[basename]

			if !fileExists {
				violations = append(violations, Violation{
					Type:     ViolationCitationInvalid,
					Severity: SeverityCritical,
					Code:     "CITATION_FILE_NOT_FOUND",
					Message:  fmt.Sprintf("Cited file does not exist: %s", cit.FilePath),
					Evidence: cit.Raw,
				})
				continue
			}
		}

		// Level 2: Was file shown in context?
		if c.config.ValidateInContext {
			if !c.fileInContext(cit.FilePath, input) {
				violations = append(violations, Violation{
					Type:     ViolationCitationInvalid,
					Severity: SeverityWarning,
					Code:     "CITATION_NOT_IN_CONTEXT",
					Message:  fmt.Sprintf("Cited file was not in context: %s", cit.FilePath),
					Evidence: cit.Raw,
				})
				continue
			}

			// Level 3: Is line number valid?
			if c.config.ValidateLineRange {
				fileContent := c.getFileContent(cit.FilePath, input)
				if fileContent != "" {
					lineCount := strings.Count(fileContent, "\n") + 1
					if cit.StartLine > lineCount || cit.StartLine < 1 {
						violations = append(violations, Violation{
							Type:     ViolationCitationInvalid,
							Severity: SeverityCritical,
							Code:     "CITATION_LINE_OUT_OF_RANGE",
							Message: fmt.Sprintf("Line %d out of range (file has %d lines)",
								cit.StartLine, lineCount),
							Evidence: cit.Raw,
						})
					}
				}
			}
		}
	}

	// Check for MISSING citations (claims without references)
	if c.config.RequireCitations && len(citations) == 0 {
		if c.containsCodeClaims(input.Response) {
			violations = append(violations, Violation{
				Type:     ViolationNoCitations,
				Severity: SeverityWarning,
				Code:     "NO_CITATIONS",
				Message:  "Response makes code claims without [file:line] citations",
			})
		}
	}

	return violations
}

// extractCitations parses all citations from the response.
func (c *CitationChecker) extractCitations(response string) []Citation {
	var citations []Citation

	matches := c.citationPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 8 {
			continue
		}

		raw := response[match[0]:match[1]]
		filePath := response[match[2]:match[3]]
		startLineStr := response[match[6]:match[7]]

		startLine, err := strconv.Atoi(startLineStr)
		if err != nil {
			continue
		}

		endLine := startLine
		if match[8] != -1 && match[9] != -1 {
			endLineStr := response[match[8]:match[9]]
			if parsed, err := strconv.Atoi(endLineStr); err == nil {
				endLine = parsed
			}
		}

		citations = append(citations, Citation{
			Raw:       raw,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Position:  match[0],
		})
	}

	return citations
}

// fileInContext checks if a file was shown in context.
func (c *CitationChecker) fileInContext(filePath string, input *CheckInput) bool {
	normalizedPath := normalizePath(filePath)
	basename := filepath.Base(filePath)

	// Check evidence index first
	if input.EvidenceIndex != nil {
		if input.EvidenceIndex.Files[normalizedPath] ||
			input.EvidenceIndex.Files[filePath] ||
			input.EvidenceIndex.FileBasenames[basename] {
			return true
		}
	}

	// Check code context
	for _, entry := range input.CodeContext {
		entryBase := filepath.Base(entry.FilePath)
		if entry.FilePath == filePath ||
			entry.FilePath == normalizedPath ||
			entryBase == basename {
			return true
		}
	}

	// Check tool results for file reads
	for _, result := range input.ToolResults {
		if strings.Contains(result.Output, filePath) ||
			strings.Contains(result.Output, basename) {
			return true
		}
	}

	return false
}

// getFileContent retrieves file content from context.
func (c *CitationChecker) getFileContent(filePath string, input *CheckInput) string {
	normalizedPath := normalizePath(filePath)
	basename := filepath.Base(filePath)

	// Check evidence index first
	if input.EvidenceIndex != nil {
		if content, ok := input.EvidenceIndex.FileContents[normalizedPath]; ok {
			return content
		}
		if content, ok := input.EvidenceIndex.FileContents[filePath]; ok {
			return content
		}
	}

	// Check code context
	for _, entry := range input.CodeContext {
		entryBase := filepath.Base(entry.FilePath)
		if entry.FilePath == filePath ||
			entry.FilePath == normalizedPath ||
			entryBase == basename {
			return entry.Content
		}
	}

	return ""
}

// containsCodeClaims detects if response makes claims about code.
func (c *CitationChecker) containsCodeClaims(response string) bool {
	for _, pattern := range c.claimPatterns {
		if pattern.MatchString(response) {
			return true
		}
	}
	return false
}

// normalizePath normalizes a file path for comparison.
func normalizePath(path string) string {
	// Remove leading ./ or /
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	// Normalize separators
	path = filepath.Clean(path)
	return path
}
