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
)

// Package-level compiled regexes for line citation parsing (compiled once).
var (
	// lineCitationBracketed matches [file.go:42] or [file.go:42-50] citations.
	// Group 1: file path, Group 2: start line, Group 3: end line (optional)
	lineCitationBracketed = regexp.MustCompile(
		`\[([^\[\]:]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|md|yaml|yml|json)):(\d+)(?:-(\d+))?\]`,
	)

	// lineCitationUnbracketed matches file.go:42 or file.go:42-50 without brackets.
	// Must not be preceded by [ or ( to avoid double-matching bracketed/parenthesized citations.
	// Group 1: file path, Group 2: start line, Group 3: end line (optional)
	lineCitationUnbracketed = regexp.MustCompile(
		`(?:^|[^\[(])\b([a-zA-Z0-9_\-./]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|md|yaml|yml|json)):(\d+)(?:-(\d+))?`,
	)

	// lineCitationProse matches "at line 42 of file.go" or "lines 42-50 in file.go".
	// Group 1: start line, Group 2: end line (optional), Group 3: file path
	lineCitationProse = regexp.MustCompile(
		`(?i)(?:at\s+)?lines?\s+(\d+)(?:\s*[-–]\s*(\d+))?\s+(?:of|in)\s+([a-zA-Z0-9_\-./]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp))`,
	)

	// lineCitationParenthesized matches (file.go:42) or (file.go:42-50).
	// Group 1: file path, Group 2: start line, Group 3: end line (optional)
	lineCitationParenthesized = regexp.MustCompile(
		`\(([a-zA-Z0-9_\-./]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|md|yaml|yml|json)):(\d+)(?:-(\d+))?\)`,
	)
)

// LineCitation represents a parsed line citation from response text.
type LineCitation struct {
	// Raw is the original matched text.
	Raw string

	// FilePath is the file path (may be basename only).
	FilePath string

	// StartLine is the cited starting line (1-indexed).
	StartLine int

	// EndLine is the cited ending line (same as StartLine for single-line citations).
	EndLine int

	// Position is the character offset in the response where this was found.
	Position int

	// SymbolName is the associated symbol name, if any.
	SymbolName string
}

// LineNumberChecker validates line number citations in responses.
//
// This checker detects fabricated line numbers including:
// - Lines beyond file length
// - Symbol location mismatches (claim says line 42, symbol is at 127)
// - Invalid ranges (end < start)
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type LineNumberChecker struct {
	config *LineNumberCheckerConfig
}

// NewLineNumberChecker creates a new line number checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*LineNumberChecker - The configured checker.
func NewLineNumberChecker(config *LineNumberCheckerConfig) *LineNumberChecker {
	if config == nil {
		config = DefaultLineNumberCheckerConfig()
	}
	return &LineNumberChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *LineNumberChecker) Name() string {
	return "line_number_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts line citations from the response and validates them against
//	the EvidenceIndex. Detects fabricated line numbers including:
//	- Lines beyond file length
//	- Symbol location mismatches
//	- Invalid line ranges
//
// Thread Safety: Safe for concurrent use.
func (c *LineNumberChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Need evidence index with FileLines to validate
	if input.EvidenceIndex == nil {
		return nil
	}

	var violations []Violation

	// Extract all line citations from response
	citations := c.extractLineCitations(input.Response)

	// Limit citations to check
	if c.config.MaxCitationsToCheck > 0 && len(citations) > c.config.MaxCitationsToCheck {
		citations = citations[:c.config.MaxCitationsToCheck]
	}

	// Validate each citation
	for _, cit := range citations {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		v := c.validateCitation(ctx, cit, input.EvidenceIndex)
		if v != nil {
			violations = append(violations, *v)
		}
	}

	return violations
}

// extractLineCitations extracts all line citations from response text.
func (c *LineNumberChecker) extractLineCitations(response string) []LineCitation {
	var citations []LineCitation
	seen := make(map[string]bool) // Dedup by position+raw

	// Extract bracketed citations [file.go:42]
	matches := lineCitationBracketed.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		cit := c.parseBracketedMatch(response, match)
		if cit != nil {
			key := fmt.Sprintf("%d:%s", cit.Position, cit.Raw)
			if !seen[key] {
				citations = append(citations, *cit)
				seen[key] = true
			}
		}
	}

	// Extract parenthesized citations (file.go:42)
	matches = lineCitationParenthesized.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		cit := c.parseParenthesizedMatch(response, match)
		if cit != nil {
			key := fmt.Sprintf("%d:%s", cit.Position, cit.Raw)
			if !seen[key] {
				citations = append(citations, *cit)
				seen[key] = true
			}
		}
	}

	// Extract unbracketed citations file.go:42
	matches = lineCitationUnbracketed.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		cit := c.parseUnbracketedMatch(response, match)
		if cit != nil {
			key := fmt.Sprintf("%d:%s", cit.Position, cit.Raw)
			if !seen[key] {
				citations = append(citations, *cit)
				seen[key] = true
			}
		}
	}

	// Extract prose citations "at line 42 of file.go"
	matches = lineCitationProse.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		cit := c.parseProseMatch(response, match)
		if cit != nil {
			key := fmt.Sprintf("%d:%s", cit.Position, cit.Raw)
			if !seen[key] {
				citations = append(citations, *cit)
				seen[key] = true
			}
		}
	}

	return citations
}

// parseBracketedMatch parses a bracketed citation match.
func (c *LineNumberChecker) parseBracketedMatch(response string, match []int) *LineCitation {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]
	filePath := response[match[2]:match[3]]
	startLineStr := response[match[6]:match[7]]

	startLine, err := strconv.Atoi(startLineStr)
	if err != nil {
		return nil
	}

	endLine := startLine
	if match[8] != -1 && match[9] != -1 {
		if parsed, err := strconv.Atoi(response[match[8]:match[9]]); err == nil {
			endLine = parsed
		}
	}

	return &LineCitation{
		Raw:       raw,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Position:  match[0],
	}
}

// parseParenthesizedMatch parses a parenthesized citation match.
func (c *LineNumberChecker) parseParenthesizedMatch(response string, match []int) *LineCitation {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]
	filePath := response[match[2]:match[3]]
	startLineStr := response[match[6]:match[7]]

	startLine, err := strconv.Atoi(startLineStr)
	if err != nil {
		return nil
	}

	endLine := startLine
	if match[8] != -1 && match[9] != -1 {
		if parsed, err := strconv.Atoi(response[match[8]:match[9]]); err == nil {
			endLine = parsed
		}
	}

	return &LineCitation{
		Raw:       raw,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Position:  match[0],
	}
}

// parseUnbracketedMatch parses an unbracketed citation match.
func (c *LineNumberChecker) parseUnbracketedMatch(response string, match []int) *LineCitation {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]
	filePath := response[match[2]:match[3]]
	startLineStr := response[match[6]:match[7]]

	startLine, err := strconv.Atoi(startLineStr)
	if err != nil {
		return nil
	}

	endLine := startLine
	if match[8] != -1 && match[9] != -1 {
		if parsed, err := strconv.Atoi(response[match[8]:match[9]]); err == nil {
			endLine = parsed
		}
	}

	return &LineCitation{
		Raw:       raw,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Position:  match[0],
	}
}

// parseProseMatch parses a prose format citation match.
func (c *LineNumberChecker) parseProseMatch(response string, match []int) *LineCitation {
	if len(match) < 8 {
		return nil
	}

	raw := response[match[0]:match[1]]

	// Group 1: start line
	startLineStr := response[match[2]:match[3]]
	startLine, err := strconv.Atoi(startLineStr)
	if err != nil {
		return nil
	}

	// Group 2: end line (optional)
	endLine := startLine
	if match[4] != -1 && match[5] != -1 {
		if parsed, err := strconv.Atoi(response[match[4]:match[5]]); err == nil {
			endLine = parsed
		}
	}

	// Group 3: file path
	filePath := response[match[6]:match[7]]

	return &LineCitation{
		Raw:       raw,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Position:  match[0],
	}
}

// validateCitation validates a single line citation against evidence.
func (c *LineNumberChecker) validateCitation(ctx context.Context, cit LineCitation, idx *EvidenceIndex) *Violation {
	// Validate line numbers are positive
	if cit.StartLine <= 0 {
		RecordLineFabrication(ctx, "invalid_line_number", cit.FilePath)
		return &Violation{
			Type:           ViolationLineNumberFabrication,
			Severity:       SeverityHigh,
			Code:           "LINE_INVALID_ZERO_OR_NEGATIVE",
			Message:        fmt.Sprintf("Invalid line number %d (lines are 1-indexed)", cit.StartLine),
			Evidence:       cit.Raw,
			Location:       cit.FilePath,
			LocationOffset: cit.Position,
			Suggestion:     "Use line numbers starting from 1",
		}
	}

	// Validate range makes sense (end >= start)
	if cit.EndLine < cit.StartLine {
		RecordLineFabrication(ctx, "invalid_range", cit.FilePath)
		return &Violation{
			Type:           ViolationLineNumberFabrication,
			Severity:       SeverityHigh,
			Code:           "LINE_INVALID_RANGE",
			Message:        fmt.Sprintf("Invalid line range %d-%d (end before start)", cit.StartLine, cit.EndLine),
			Evidence:       cit.Raw,
			Location:       cit.FilePath,
			LocationOffset: cit.Position,
			Suggestion:     "Ensure end line is greater than or equal to start line",
		}
	}

	// Look up file in evidence
	normalizedPath := normalizePath(cit.FilePath)
	basename := filepath.Base(cit.FilePath)

	// Try to find the file in evidence
	var fileLineCount int
	var fileFound bool

	if count, ok := idx.FileLines[normalizedPath]; ok {
		fileLineCount = count
		fileFound = true
	} else if count, ok := idx.FileLines[cit.FilePath]; ok {
		fileLineCount = count
		fileFound = true
	} else {
		// Try matching by basename
		for path, count := range idx.FileLines {
			if filepath.Base(path) == basename {
				fileLineCount = count
				fileFound = true
				break
			}
		}
	}

	// If file not in evidence, skip validation (not a violation)
	if !fileFound {
		return nil
	}

	// Special case: empty file (0 lines) can't have any valid line citations
	if fileLineCount == 0 {
		RecordLineFabrication(ctx, "beyond_file_length", cit.FilePath)
		return &Violation{
			Type:           ViolationLineNumberFabrication,
			Severity:       SeverityHigh,
			Code:           "LINE_BEYOND_FILE_LENGTH",
			Message:        fmt.Sprintf("Line %d referenced in empty file (0 lines)", cit.StartLine),
			Evidence:       cit.Raw,
			Expected:       "File is empty - no valid line numbers",
			Location:       cit.FilePath,
			LocationOffset: cit.Position,
			Suggestion:     "The file is empty and has no lines",
		}
	}

	// Calculate tolerance based on file size
	tolerance := c.calculateTolerance(fileLineCount, cit.EndLine > cit.StartLine)

	// Check if start line is beyond file length
	if cit.StartLine > fileLineCount+tolerance {
		RecordLineFabrication(ctx, "beyond_file_length", cit.FilePath)
		return &Violation{
			Type:           ViolationLineNumberFabrication,
			Severity:       SeverityHigh,
			Code:           "LINE_BEYOND_FILE_LENGTH",
			Message:        fmt.Sprintf("Line %d exceeds file length (%d lines)", cit.StartLine, fileLineCount),
			Evidence:       cit.Raw,
			Expected:       fmt.Sprintf("Line number between 1 and %d", fileLineCount),
			Location:       cit.FilePath,
			LocationOffset: cit.Position,
			Suggestion:     fmt.Sprintf("The file only has %d lines", fileLineCount),
		}
	}

	// Check if end line is beyond file length
	if cit.EndLine > fileLineCount+tolerance {
		RecordLineFabrication(ctx, "beyond_file_length", cit.FilePath)
		return &Violation{
			Type:           ViolationLineNumberFabrication,
			Severity:       SeverityHigh,
			Code:           "LINE_RANGE_END_BEYOND_LENGTH",
			Message:        fmt.Sprintf("Line range end %d exceeds file length (%d lines)", cit.EndLine, fileLineCount),
			Evidence:       cit.Raw,
			Expected:       fmt.Sprintf("End line between %d and %d", cit.StartLine, fileLineCount),
			Location:       cit.FilePath,
			LocationOffset: cit.Position,
			Suggestion:     fmt.Sprintf("The file only has %d lines", fileLineCount),
		}
	}

	// If there's an associated symbol, validate against symbol location
	if cit.SymbolName != "" {
		v := c.validateSymbolLocation(ctx, cit, idx, tolerance)
		if v != nil {
			return v
		}
	}

	return nil
}

// calculateTolerance calculates the line tolerance based on file size and config.
func (c *LineNumberChecker) calculateTolerance(fileLines int, isRange bool) int {
	if c.config.StrictMode {
		return 0
	}

	baseTolerance := c.config.LineTolerance
	if isRange {
		baseTolerance = c.config.RangeTolerance
	}

	if !c.config.ScaleTolerance {
		return baseTolerance
	}

	// Scale tolerance based on file size
	if fileLines > 500 {
		return baseTolerance * 2
	}
	if fileLines < 100 {
		return baseTolerance / 2
	}
	return baseTolerance
}

// validateSymbolLocation validates that a cited line matches a known symbol location.
func (c *LineNumberChecker) validateSymbolLocation(ctx context.Context, cit LineCitation, idx *EvidenceIndex, tolerance int) *Violation {
	symbolInfos, ok := idx.SymbolDetails[cit.SymbolName]
	if !ok {
		// Symbol not in evidence - can't validate
		return nil
	}

	// Find symbol info for this file
	normalizedPath := normalizePath(cit.FilePath)
	basename := filepath.Base(cit.FilePath)

	for _, sym := range symbolInfos {
		symPath := normalizePath(sym.File)
		symBase := filepath.Base(sym.File)

		// Check if this symbol is in the cited file
		if symPath == normalizedPath || symPath == cit.FilePath || symBase == basename {
			// Check if cited line is within tolerance of actual location
			diff := abs(cit.StartLine - sym.Line)
			if diff <= tolerance {
				return nil // Valid
			}

			// Symbol found but line is too far off
			RecordLineFabrication(ctx, "symbol_mismatch", cit.FilePath)
			return &Violation{
				Type:           ViolationLineNumberFabrication,
				Severity:       SeverityWarning,
				Code:           "LINE_SYMBOL_MISMATCH",
				Message:        fmt.Sprintf("Symbol %s cited at line %d but actually at line %d", cit.SymbolName, cit.StartLine, sym.Line),
				Evidence:       cit.Raw,
				Expected:       fmt.Sprintf("Line %d (with tolerance ±%d)", sym.Line, tolerance),
				Location:       cit.FilePath,
				LocationOffset: cit.Position,
				Suggestion:     fmt.Sprintf("The %s symbol is defined at line %d", cit.SymbolName, sym.Line),
			}
		}
	}

	// Symbol not found in this specific file - can't validate
	return nil
}

// abs returns the absolute value of an integer.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
