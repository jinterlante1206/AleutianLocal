// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"context"
	"regexp"
	"strings"
)

// SecurityPattern defines a security anti-pattern to detect.
type SecurityPattern struct {
	Name        string
	Pattern     *regexp.Regexp
	Severity    string // critical, high, medium, low
	Description string
	Languages   []string // Empty = all languages
}

// BasicSecurityScanner provides simple pattern-based security scanning.
//
// This is a lightweight scanner for use during MCTS simulation.
// For production, integrate with CB-23 (security analysis) for deeper scanning.
//
// Thread Safety: Safe for concurrent use.
type BasicSecurityScanner struct {
	patterns []SecurityPattern
}

// NewBasicSecurityScanner creates a scanner with default patterns.
//
// Outputs:
//   - *BasicSecurityScanner: Ready to use security scanner.
func NewBasicSecurityScanner() *BasicSecurityScanner {
	return &BasicSecurityScanner{
		patterns: defaultSecurityPatterns(),
	}
}

// NewBasicSecurityScannerWithPatterns creates a scanner with custom patterns.
//
// Inputs:
//   - patterns: Custom security patterns to check.
//
// Outputs:
//   - *BasicSecurityScanner: Ready to use security scanner.
func NewBasicSecurityScannerWithPatterns(patterns []SecurityPattern) *BasicSecurityScanner {
	return &BasicSecurityScanner{
		patterns: patterns,
	}
}

func defaultSecurityPatterns() []SecurityPattern {
	return []SecurityPattern{
		// Command injection
		{
			Name:        "command_injection",
			Pattern:     regexp.MustCompile(`(?i)(exec\.Command|os\.system|subprocess\.call|subprocess\.run)\s*\([^)]*\+`),
			Severity:    "critical",
			Description: "Potential command injection via string concatenation",
		},
		// SQL injection
		{
			Name:        "sql_injection",
			Pattern:     regexp.MustCompile(`(?i)(SELECT|INSERT|UPDATE|DELETE).*\+.*['"]\s*\+`),
			Severity:    "critical",
			Description: "Potential SQL injection via string concatenation",
		},
		// Hardcoded credentials
		{
			Name:        "hardcoded_password",
			Pattern:     regexp.MustCompile(`(?i)(password|passwd|pwd|secret|api_key|apikey)\s*[=:]\s*["'][^"']{8,}["']`),
			Severity:    "high",
			Description: "Hardcoded credential or secret",
		},
		// Path traversal
		{
			Name:        "path_traversal",
			Pattern:     regexp.MustCompile(`(?i)(\.\.\/|\.\.\\|%2e%2e%2f)`),
			Severity:    "high",
			Description: "Potential path traversal",
		},
		// Insecure deserialization (Go)
		{
			Name:        "insecure_deserialize_go",
			Pattern:     regexp.MustCompile(`gob\.NewDecoder\([^)]*\)\.Decode`),
			Severity:    "medium",
			Description: "Gob deserialization of potentially untrusted data",
			Languages:   []string{"go"},
		},
		// Insecure deserialization (Python)
		{
			Name:        "insecure_deserialize_python",
			Pattern:     regexp.MustCompile(`pickle\.load|yaml\.load\([^)]*\)`),
			Severity:    "critical",
			Description: "Unsafe deserialization",
			Languages:   []string{"python"},
		},
		// Weak crypto
		{
			Name:        "weak_crypto",
			Pattern:     regexp.MustCompile(`(?i)(md5|sha1)\s*\(`),
			Severity:    "medium",
			Description: "Weak cryptographic algorithm",
		},
		// Eval usage
		{
			Name:        "eval_usage",
			Pattern:     regexp.MustCompile(`(?i)\beval\s*\(`),
			Severity:    "high",
			Description: "Dynamic code evaluation",
		},
		// Disabled SSL verification
		{
			Name:        "ssl_disabled",
			Pattern:     regexp.MustCompile(`(?i)(verify\s*[=:]\s*false|InsecureSkipVerify\s*:\s*true|CERT_NONE)`),
			Severity:    "high",
			Description: "SSL/TLS verification disabled",
		},
		// Debug mode in production
		{
			Name:        "debug_mode",
			Pattern:     regexp.MustCompile(`(?i)(DEBUG\s*[=:]\s*true|debug\s*=\s*True)`),
			Severity:    "medium",
			Description: "Debug mode may be enabled in production",
		},
		// Potential SSRF
		{
			Name:        "ssrf_potential",
			Pattern:     regexp.MustCompile(`(?i)(http\.Get|requests\.get|fetch)\([^)]*\+`),
			Severity:    "high",
			Description: "Potential SSRF via user-controlled URL",
		},
	}
}

// maxMatchesPerPattern limits matches to prevent excessive processing.
// This provides defense-in-depth against pathological regex inputs.
const maxMatchesPerPattern = 100

// ScanCode scans code for security issues.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - code: The code to scan.
//
// Outputs:
//   - *SecurityScanResult: Scan results with score and issues.
//   - error: Non-nil on context cancellation.
//
// ReDoS Mitigation:
//   - Context is checked between each pattern for cancellation
//   - Match count is limited to maxMatchesPerPattern per pattern
//   - Input size should be bounded by caller (e.g., ActionValidationConfig.MaxCodeDiffBytes)
func (s *BasicSecurityScanner) ScanCode(ctx context.Context, code string) (*SecurityScanResult, error) {
	result := &SecurityScanResult{
		Score:  1.0, // Start with perfect score
		Issues: make([]SecurityIssue, 0),
	}

	for _, pattern := range s.patterns {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Limit matches per pattern to prevent excessive processing
		matches := pattern.Pattern.FindAllString(code, maxMatchesPerPattern)
		for _, match := range matches {
			issue := SecurityIssue{
				Severity: pattern.Severity,
				Message:  pattern.Description + ": " + truncateMatch(match, 50),
				Pattern:  pattern.Name,
			}
			result.Issues = append(result.Issues, issue)

			// Deduct from score based on severity
			switch pattern.Severity {
			case "critical":
				result.Score -= 0.5
			case "high":
				result.Score -= 0.3
			case "medium":
				result.Score -= 0.15
			case "low":
				result.Score -= 0.05
			}
		}
	}

	// Clamp score to [0, 1]
	if result.Score < 0 {
		result.Score = 0
	}

	return result, nil
}

// ScanCodeWithLanguage scans code with language-specific patterns.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - code: The code to scan.
//   - language: The programming language for language-specific patterns.
//
// Outputs:
//   - *SecurityScanResult: Scan results with score and issues.
//   - error: Non-nil on context cancellation.
//
// ReDoS Mitigation: Same protections as ScanCode apply.
func (s *BasicSecurityScanner) ScanCodeWithLanguage(ctx context.Context, code, language string) (*SecurityScanResult, error) {
	result := &SecurityScanResult{
		Score:  1.0,
		Issues: make([]SecurityIssue, 0),
	}

	for _, pattern := range s.patterns {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Skip language-specific patterns that don't apply
		if len(pattern.Languages) > 0 && !containsLanguage(pattern.Languages, language) {
			continue
		}

		// Limit matches per pattern to prevent excessive processing
		matches := pattern.Pattern.FindAllString(code, maxMatchesPerPattern)
		for _, match := range matches {
			issue := SecurityIssue{
				Severity: pattern.Severity,
				Message:  pattern.Description + ": " + truncateMatch(match, 50),
				Pattern:  pattern.Name,
			}
			result.Issues = append(result.Issues, issue)

			switch pattern.Severity {
			case "critical":
				result.Score -= 0.5
			case "high":
				result.Score -= 0.3
			case "medium":
				result.Score -= 0.15
			case "low":
				result.Score -= 0.05
			}
		}
	}

	if result.Score < 0 {
		result.Score = 0
	}

	return result, nil
}

func containsLanguage(languages []string, target string) bool {
	target = strings.ToLower(target)
	for _, lang := range languages {
		if strings.ToLower(lang) == target {
			return true
		}
	}
	return false
}

func truncateMatch(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Patterns returns the configured security patterns.
func (s *BasicSecurityScanner) Patterns() []SecurityPattern {
	return s.patterns
}

// AddPattern adds a custom security pattern.
func (s *BasicSecurityScanner) AddPattern(pattern SecurityPattern) {
	s.patterns = append(s.patterns, pattern)
}
