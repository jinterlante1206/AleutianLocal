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

// Package-level compiled regexes for claim extraction (compiled once).
var (
	// filePatternsCompiled detects file path references.
	filePatternsCompiled = []*regexp.Regexp{
		// File paths with extensions: "in main.go", "the file.py file"
		regexp.MustCompile(`\b([\w/.-]+\.(go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|cc))\b`),
	}

	// symbolPatternsCompiled detects symbol references.
	symbolPatternsCompiled = []*regexp.Regexp{
		// "the X function" - requires "the" prefix to avoid false matches
		regexp.MustCompile(`(?i)the\s+([A-Z]\w*)\s+function`),
		// "`X` function" or "X() function" - code-like references
		regexp.MustCompile("(?i)`([A-Za-z_]\\w*)`\\s+function"),
		regexp.MustCompile(`(?i)([A-Z]\w*)\(\)\s+function`),
		// "type X", "struct X", "class X", "interface X" - definitions
		regexp.MustCompile(`(?i)type\s+([A-Z]\w*)`),
		regexp.MustCompile(`(?i)struct\s+([A-Z]\w*)`),
		regexp.MustCompile(`(?i)class\s+([A-Z]\w*)`),
		regexp.MustCompile(`(?i)interface\s+([A-Z]\w*)`),
		// "the X method" - requires "the" prefix
		regexp.MustCompile(`(?i)the\s+([A-Z]\w*)\s+method`),
		// "`X` method" - code-like references
		regexp.MustCompile("(?i)`([A-Za-z_]\\w*)`\\s+method"),
	}

	// frameworkPatternCompiled detects framework/library mentions.
	frameworkPatternCompiled = regexp.MustCompile(`(?i)\b(flask|django|fastapi|pyramid|tornado|express|koa|nest|gin|echo|fiber|chi|gorilla|beego|spring|micronaut|quarkus|rails|sinatra|laravel|symfony)\b`)

	// commonWordsSet is a package-level set of words to filter from symbol claims.
	// Kept as package-level to avoid allocating a new map on every call.
	commonWordsSet = map[string]bool{
		// Articles and pronouns
		"the": true, "a": true, "an": true, "this": true, "that": true,
		// Common verbs
		"get": true, "set": true, "run": true, "do": true, "make": true,
		"is": true, "are": true, "was": true, "be": true, "have": true,
		// Common adjectives
		"new": true, "old": true, "main": true, "test": true, "default": true,
		// Common nouns
		"data": true, "value": true, "error": true, "result": true, "input": true,
		"output": true, "file": true, "string": true, "int": true, "bool": true,
		// Code-related keywords
		"function": true, "method": true, "type": true, "struct": true, "class": true,
		"interface": true, "package": true, "module": true, "import": true,
		// Too short or generic
		"i": true, "j": true, "k": true, "x": true, "y": true, "n": true,
		"id": true, "db": true, "io": true, "ok": true, "err": true,
		// Common Go types/packages that are too generic
		"context": true, "http": true, "fmt": true, "log": true,
	}
)

// GroundingCheckerConfig configures the grounding checker.
type GroundingCheckerConfig struct {
	// CheckFiles enables file reference validation.
	CheckFiles bool

	// CheckSymbols enables symbol reference validation.
	CheckSymbols bool

	// CheckFrameworks enables framework reference validation.
	CheckFrameworks bool

	// SymbolSeverity is the severity for ungrounded symbols.
	// Defaults to warning since symbol names can be generic.
	SymbolSeverity Severity

	// FileSeverity is the severity for ungrounded file references.
	// Defaults to critical since files should definitely exist.
	FileSeverity Severity

	// FrameworkSeverity is the severity for ungrounded framework mentions.
	// Defaults to critical since this strongly indicates hallucination.
	FrameworkSeverity Severity
}

// DefaultGroundingCheckerConfig returns a config with sensible defaults.
func DefaultGroundingCheckerConfig() *GroundingCheckerConfig {
	return &GroundingCheckerConfig{
		CheckFiles:        true,
		CheckSymbols:      true,
		CheckFrameworks:   true,
		SymbolSeverity:    SeverityWarning,
		FileSeverity:      SeverityCritical,
		FrameworkSeverity: SeverityCritical,
	}
}

// GroundingChecker validates that claims in responses are grounded in evidence.
//
// This is Layer 4 of the anti-hallucination defense system. It extracts
// claims about files, symbols, and frameworks from the LLM response and
// verifies each claim against the evidence index.
//
// Thread Safety: Safe for concurrent use after construction.
type GroundingChecker struct {
	config *GroundingCheckerConfig
}

// NewGroundingChecker creates a new grounding checker.
//
// Inputs:
//
//	config - Configuration for the checker. If nil, defaults are used.
//
// Outputs:
//
//	*GroundingChecker - The configured checker.
func NewGroundingChecker(config *GroundingCheckerConfig) *GroundingChecker {
	if config == nil {
		config = DefaultGroundingCheckerConfig()
	}

	return &GroundingChecker{
		config: config,
	}
}

// Name returns the checker name for logging and metrics.
func (c *GroundingChecker) Name() string {
	return "grounding_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts claims from the response and validates each against the
//	evidence index. Returns violations for claims that are not grounded.
//
// Thread Safety: Safe for concurrent use.
func (c *GroundingChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if input == nil || input.Response == "" {
		return nil
	}

	var violations []Violation

	// Use provided evidence index or build from context
	evidence := input.EvidenceIndex
	if evidence == nil {
		evidence = NewEvidenceIndex()
	}

	// Extract and validate claims
	claims := c.extractClaims(input.Response)

	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		violation := c.validateClaim(claim, evidence, input)
		if violation != nil {
			violations = append(violations, *violation)
		}
	}

	return violations
}

// extractClaims extracts all claims from the response text.
func (c *GroundingChecker) extractClaims(response string) []Claim {
	var claims []Claim
	seen := make(map[string]bool) // Deduplicate claims

	// Extract file claims
	if c.config.CheckFiles {
		for _, pattern := range filePatternsCompiled {
			matches := pattern.FindAllStringSubmatchIndex(response, -1)
			for _, match := range matches {
				if len(match) >= 4 {
					filePath := response[match[2]:match[3]]
					key := "file:" + strings.ToLower(filePath)
					if !seen[key] {
						seen[key] = true
						claims = append(claims, Claim{
							Type:     ClaimFile,
							Value:    filePath,
							RawText:  response[match[0]:match[1]],
							Position: match[0],
						})
					}
				}
			}
		}
	}

	// Extract symbol claims
	if c.config.CheckSymbols {
		for _, pattern := range symbolPatternsCompiled {
			matches := pattern.FindAllStringSubmatchIndex(response, -1)
			for _, match := range matches {
				if len(match) >= 4 {
					symbol := response[match[2]:match[3]]
					// Skip common words that aren't symbols
					if isCommonWord(symbol) {
						continue
					}
					key := "symbol:" + strings.ToLower(symbol)
					if !seen[key] {
						seen[key] = true
						claims = append(claims, Claim{
							Type:     ClaimSymbol,
							Value:    symbol,
							RawText:  response[match[0]:match[1]],
							Position: match[0],
						})
					}
				}
			}
		}
	}

	// Extract framework claims
	if c.config.CheckFrameworks {
		matches := frameworkPatternCompiled.FindAllStringSubmatchIndex(response, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				framework := strings.ToLower(response[match[2]:match[3]])
				key := "framework:" + framework
				if !seen[key] {
					seen[key] = true
					claims = append(claims, Claim{
						Type:     ClaimFramework,
						Value:    framework,
						RawText:  response[match[0]:match[1]],
						Position: match[0],
					})
				}
			}
		}
	}

	return claims
}

// validateClaim checks if a claim is grounded in evidence.
func (c *GroundingChecker) validateClaim(claim Claim, evidence *EvidenceIndex, input *CheckInput) *Violation {
	switch claim.Type {
	case ClaimFile:
		return c.validateFileClaim(claim, evidence, input)
	case ClaimSymbol:
		return c.validateSymbolClaim(claim, evidence)
	case ClaimFramework:
		return c.validateFrameworkClaim(claim, evidence)
	default:
		return nil
	}
}

// validateFileClaim checks if a file reference is grounded.
func (c *GroundingChecker) validateFileClaim(claim Claim, evidence *EvidenceIndex, input *CheckInput) *Violation {
	filePath := claim.Value
	basename := filepath.Base(filePath)

	// Check if file was shown in context
	if evidence.Files[filePath] || evidence.Files[normalizePath(filePath)] {
		return nil // File is in evidence
	}

	// Check by basename as fallback
	if evidence.FileBasenames[basename] {
		return nil // Basename matches
	}

	// Check KnownFiles from input (project-level knowledge)
	if input.KnownFiles != nil {
		if input.KnownFiles[filePath] || input.KnownFiles[normalizePath(filePath)] {
			// File exists in project but wasn't in context
			// This is a warning, not critical - LLM might be making reasonable inference
			return &Violation{
				Type:     ViolationUngrounded,
				Severity: SeverityWarning,
				Code:     "UNGROUNDED_FILE_NOT_IN_CONTEXT",
				Message:  fmt.Sprintf("Referenced file '%s' exists in project but was not in context", filePath),
				Evidence: claim.RawText,
			}
		}
	}

	// File not found anywhere
	return &Violation{
		Type:     ViolationUngrounded,
		Severity: c.config.FileSeverity,
		Code:     "UNGROUNDED_FILE",
		Message:  fmt.Sprintf("Response references file '%s' not found in evidence", filePath),
		Evidence: claim.RawText,
		Expected: c.formatExpectedFiles(evidence),
	}
}

// validateSymbolClaim checks if a symbol reference is grounded.
func (c *GroundingChecker) validateSymbolClaim(claim Claim, evidence *EvidenceIndex) *Violation {
	symbol := claim.Value

	// Check if symbol was seen in context
	if evidence.Symbols[symbol] {
		return nil // Symbol is in evidence
	}

	// Check case-insensitive as fallback
	for s := range evidence.Symbols {
		if strings.EqualFold(s, symbol) {
			return nil // Case-insensitive match
		}
	}

	// Symbol not found - this is usually a warning since symbol names
	// might be described generically or paraphrased
	return &Violation{
		Type:     ViolationUngrounded,
		Severity: c.config.SymbolSeverity,
		Code:     "UNGROUNDED_SYMBOL",
		Message:  fmt.Sprintf("Response mentions symbol '%s' not found in context", symbol),
		Evidence: claim.RawText,
	}
}

// validateFrameworkClaim checks if a framework reference is grounded.
func (c *GroundingChecker) validateFrameworkClaim(claim Claim, evidence *EvidenceIndex) *Violation {
	framework := strings.ToLower(claim.Value)

	// Check if framework was seen in context
	if evidence.Frameworks[framework] {
		return nil // Framework is in evidence
	}

	// Check if it appears in raw content (might not be explicitly indexed)
	if evidence.RawContent != "" && strings.Contains(strings.ToLower(evidence.RawContent), framework) {
		return nil // Found in raw content
	}

	// Framework not found - this is critical as it strongly indicates hallucination
	return &Violation{
		Type:     ViolationUngrounded,
		Severity: c.config.FrameworkSeverity,
		Code:     "UNGROUNDED_FRAMEWORK",
		Message:  fmt.Sprintf("Response mentions framework '%s' not found in context", framework),
		Evidence: claim.RawText,
	}
}

// isCommonWord checks if a word is too common to be a meaningful symbol claim.
// Uses package-level commonWordsSet to avoid allocation per call.
func isCommonWord(word string) bool {
	return commonWordsSet[strings.ToLower(word)]
}

// formatExpectedFiles creates a summary of expected files for error messages.
func (c *GroundingChecker) formatExpectedFiles(evidence *EvidenceIndex) string {
	if evidence == nil {
		return "no files in context"
	}

	var files []string
	for f := range evidence.FileBasenames {
		files = append(files, f)
		if len(files) >= 5 {
			break // Limit to 5 examples
		}
	}

	if len(files) == 0 {
		return "no files in context"
	}

	if len(evidence.FileBasenames) > 5 {
		return fmt.Sprintf("files in context include: %s, ...", strings.Join(files, ", "))
	}
	return fmt.Sprintf("files in context: %s", strings.Join(files, ", "))
}
