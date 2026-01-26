// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

// SecretScanner scans code for hardcoded secrets.
type SecretScanner struct {
	patterns    []compiledSecretPattern
	minEntropy  float64
	allowlistRe []*regexp.Regexp
}

// compiledSecretPattern is a secret pattern with compiled regex.
type compiledSecretPattern struct {
	SecretPattern
	regex *regexp.Regexp
}

// NewSecretScanner creates a new secret scanner.
func NewSecretScanner(config ValidatorConfig) (*SecretScanner, error) {
	s := &SecretScanner{
		minEntropy: config.MinSecretEntropy,
	}

	// Compile secret patterns
	for _, p := range SecretPatterns() {
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return nil, err
		}
		s.patterns = append(s.patterns, compiledSecretPattern{
			SecretPattern: p,
			regex:         re,
		})
	}

	// Compile allowlist patterns
	for _, pattern := range config.AllowlistPaths {
		// Convert glob to regex
		rePattern := globToRegex(pattern)
		re, err := regexp.Compile(rePattern)
		if err != nil {
			continue // Skip invalid patterns
		}
		s.allowlistRe = append(s.allowlistRe, re)
	}

	return s, nil
}

// Scan scans content for hardcoded secrets.
//
// Description:
//
//	Checks content against known secret patterns using regex matching
//	combined with entropy analysis to reduce false positives. Skips
//	files matching the allowlist (test files, fixtures).
//
// Inputs:
//
//	content - Source code content
//	filePath - File path (for allowlist checking)
//
// Outputs:
//
//	[]ValidationWarning - Detected secrets
func (s *SecretScanner) Scan(content []byte, filePath string) []ValidationWarning {
	// Check if file is in allowlist
	if s.isAllowlisted(filePath) {
		return nil
	}

	var warnings []ValidationWarning
	lines := strings.Split(string(content), "\n")

	for lineNum, line := range lines {
		// Skip obvious comments and empty lines
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}

		for _, pattern := range s.patterns {
			matches := pattern.regex.FindAllStringIndex(line, -1)
			for _, match := range matches {
				matchedStr := line[match[0]:match[1]]

				// Check entropy if pattern requires it
				minEntropy := pattern.MinEntropy
				if minEntropy == 0 {
					minEntropy = s.minEntropy
				}

				// Extract the likely secret value
				secretValue := extractSecretValue(matchedStr)
				if minEntropy > 0 && calculateEntropy(secretValue) < minEntropy {
					continue // Skip low-entropy matches
				}

				warnings = append(warnings, ValidationWarning{
					Type:       WarnTypeSecret,
					Pattern:    pattern.Name,
					File:       filePath,
					Line:       lineNum + 1,
					Severity:   pattern.Severity,
					Message:    pattern.Message,
					Suggestion: "Use environment variables or a secret manager instead of hardcoding secrets.",
					Blocking:   true, // Secrets always block
				})
			}
		}
	}

	return warnings
}

// isAllowlisted checks if a file path matches the allowlist.
func (s *SecretScanner) isAllowlisted(filePath string) bool {
	// Check against compiled allowlist patterns
	for _, re := range s.allowlistRe {
		if re.MatchString(filePath) {
			return true
		}
	}

	// Additional heuristic checks
	lower := strings.ToLower(filePath)
	if strings.Contains(lower, "/test") ||
		strings.Contains(lower, "test_") ||
		strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.Contains(lower, "fixture") ||
		strings.Contains(lower, "mock") ||
		strings.Contains(lower, "example") ||
		strings.Contains(lower, "__tests__") {
		return true
	}

	return false
}

// calculateEntropy calculates Shannon entropy of a string.
// Higher entropy indicates more randomness (more likely to be a real secret).
func calculateEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	// Count character frequencies
	freq := make(map[rune]int)
	for _, r := range s {
		freq[r]++
	}

	// Calculate entropy
	var entropy float64
	length := float64(len(s))
	for _, count := range freq {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}

	return entropy
}

// extractSecretValue extracts the likely secret value from a match.
// Handles formats like: key="value", key: value, key = 'value'
func extractSecretValue(match string) string {
	// Try to extract value after common separators
	for _, sep := range []string{"=", ":", " "} {
		if idx := strings.Index(match, sep); idx > 0 {
			value := strings.TrimSpace(match[idx+1:])
			// Remove quotes
			value = strings.Trim(value, `"'`)
			return value
		}
	}
	return match
}

// isCommentLine checks if a line is a comment.
func isCommentLine(line string) bool {
	return strings.HasPrefix(line, "//") ||
		strings.HasPrefix(line, "#") ||
		strings.HasPrefix(line, "/*") ||
		strings.HasPrefix(line, "*") ||
		strings.HasPrefix(line, "'''") ||
		strings.HasPrefix(line, `"""`)
}

// globToRegex converts a glob pattern to a regex pattern.
func globToRegex(glob string) string {
	// Escape special regex chars except * and ?
	special := []string{".", "+", "^", "$", "(", ")", "[", "]", "{", "}", "|", "\\"}
	result := glob
	for _, s := range special {
		result = strings.ReplaceAll(result, s, "\\"+s)
	}

	// Convert glob wildcards to regex
	result = strings.ReplaceAll(result, "**", ".*")
	result = strings.ReplaceAll(result, "*", "[^/]*")
	result = strings.ReplaceAll(result, "?", ".")

	return "^" + result + "$"
}

// ScanPath returns whether a path should be scanned.
// This is a quick pre-check before content scanning.
func (s *SecretScanner) ScanPath(filePath string) bool {
	// Skip binary files
	ext := filepath.Ext(filePath)
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".png": true, ".jpg": true, ".gif": true, ".ico": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true,
		".pdf": true, ".doc": true, ".xls": true,
	}
	if binaryExts[ext] {
		return false
	}

	return !s.isAllowlisted(filePath)
}
