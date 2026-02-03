// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package scanner

import (
	"math"
	"strings"
	"sync"
)

// ScanContext provides context for confidence calculation.
//
// Description:
//
//	ScanContext contains information about the scanning environment
//	that affects confidence scoring, such as whether the code is in
//	a test file or has suppression comments.
//
// Thread Safety:
//
//	ScanContext is safe for concurrent reads.
type ScanContext struct {
	// FilePath is the path to the file being scanned.
	FilePath string

	// IsTestFile indicates if this is a test file.
	IsTestFile bool

	// HasNoSecComment indicates if there's a nosec suppression comment.
	HasNoSecComment bool

	// SuppressionNote is the reason given for suppression.
	SuppressionNote string

	// DataFlowProven indicates if trust flow analysis confirmed the issue.
	DataFlowProven bool

	// InSecurityFunction indicates if the code is in a security-sensitive function.
	InSecurityFunction bool

	// HasSanitizer indicates if a sanitizer was found in the data path.
	HasSanitizer bool
}

// ConfidenceCalculator calculates calibrated confidence scores.
//
// Description:
//
//	ConfidenceCalculator adjusts base confidence scores based on
//	context factors like data flow proof, test file presence, and
//	suppression comments. It tracks historical false positive rates
//	for continuous calibration.
//
// Thread Safety:
//
//	ConfidenceCalculator is safe for concurrent use.
type ConfidenceCalculator struct {
	// historicalFPRates tracks false positive rates by pattern ID.
	historicalFPRates map[string]float64

	// mu protects the historical rates.
	mu sync.RWMutex
}

// NewConfidenceCalculator creates a new confidence calculator.
//
// Description:
//
//	Creates a calculator with default historical FP rates based on
//	industry benchmarks for common vulnerability patterns.
//
// Outputs:
//
//	*ConfidenceCalculator - The initialized calculator.
func NewConfidenceCalculator() *ConfidenceCalculator {
	return &ConfidenceCalculator{
		historicalFPRates: defaultFPRates(),
	}
}

// Calculate computes the confidence score for an issue.
//
// Description:
//
//	Calculates confidence by starting with the pattern's base confidence
//	and applying adjustments for:
//	  - Data flow proof (+0.2)
//	  - Test file (-70%)
//	  - Suppression comment (-90%)
//	  - Security function (+0.1)
//	  - Sanitizer presence (-0.3)
//	  - Historical FP rate
//
// Inputs:
//
//	pattern - The security pattern that matched.
//	ctx - The scan context with environmental factors.
//
// Outputs:
//
//	float64 - The calculated confidence (0.0-1.0).
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (c *ConfidenceCalculator) Calculate(pattern *SecurityPattern, ctx *ScanContext) float64 {
	base := pattern.BaseConfidence
	if base == 0 {
		base = 0.7 // Default base confidence
	}

	// Boost if data flow proves exploitability
	if ctx.DataFlowProven {
		base = math.Min(base+0.2, 0.99)
	}

	// Boost if in security-sensitive function
	if ctx.InSecurityFunction {
		base = math.Min(base+0.1, 0.99)
	}

	// Reduce if sanitizer is present
	if ctx.HasSanitizer {
		base *= 0.7
	}

	// Reduce if in test file
	if ctx.IsTestFile {
		base *= 0.3
	}

	// Reduce if has suppression comment
	if ctx.HasNoSecComment {
		base *= 0.1
	}

	// Adjust for historical FP rate
	c.mu.RLock()
	fpRate, ok := c.historicalFPRates[pattern.ID]
	c.mu.RUnlock()

	if ok {
		base *= (1.0 - fpRate)
	}

	return math.Min(math.Max(base, 0.0), 1.0)
}

// UpdateFPRate updates the false positive rate for a pattern.
//
// Description:
//
//	Updates the historical FP rate when a finding is marked as
//	false positive or confirmed. Uses exponential moving average.
//
// Inputs:
//
//	patternID - The pattern ID.
//	wasFalsePositive - Whether the finding was a false positive.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (c *ConfidenceCalculator) UpdateFPRate(patternID string, wasFalsePositive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.historicalFPRates[patternID]
	if !ok {
		current = 0.0
	}

	// Exponential moving average with alpha = 0.1
	alpha := 0.1
	var newValue float64
	if wasFalsePositive {
		newValue = 1.0
	}
	c.historicalFPRates[patternID] = current*(1-alpha) + newValue*alpha
}

// GetFPRate returns the current FP rate for a pattern.
//
// Inputs:
//
//	patternID - The pattern ID.
//
// Outputs:
//
//	float64 - The FP rate (0.0-1.0).
//	bool - Whether a rate exists for this pattern.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (c *ConfidenceCalculator) GetFPRate(patternID string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rate, ok := c.historicalFPRates[patternID]
	return rate, ok
}

// IsTestFile determines if a file path indicates a test file.
//
// Description:
//
//	Checks common test file naming conventions across languages:
//	  - Go: *_test.go
//	  - Python: test_*.py, *_test.py
//	  - TypeScript/JavaScript: *.test.ts, *.spec.ts
//	  - Java: *Test.java
//	  - Also checks for test directories
//
// Inputs:
//
//	filePath - The file path to check.
//
// Outputs:
//
//	bool - True if this appears to be a test file.
func IsTestFile(filePath string) bool {
	path := strings.ToLower(filePath)

	// Go tests
	if strings.HasSuffix(path, "_test.go") {
		return true
	}

	// Python tests
	if strings.HasSuffix(path, "_test.py") || strings.Contains(path, "test_") && strings.HasSuffix(path, ".py") {
		return true
	}

	// TypeScript/JavaScript tests
	if strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".spec.ts") ||
		strings.HasSuffix(path, ".test.js") || strings.HasSuffix(path, ".spec.js") ||
		strings.HasSuffix(path, ".test.tsx") || strings.HasSuffix(path, ".spec.tsx") {
		return true
	}

	// Java tests
	if strings.HasSuffix(path, "test.java") {
		return true
	}

	// Test directories
	if strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") ||
		strings.Contains(path, "/__tests__/") || strings.Contains(path, "/testdata/") ||
		strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") ||
		strings.HasPrefix(path, "__tests__/") || strings.HasPrefix(path, "testdata/") {
		return true
	}

	return false
}

// HasSuppressionComment checks if content has a security suppression comment.
//
// Description:
//
//	Looks for common suppression comment patterns:
//	  - nosec
//	  - nolint:gosec
//	  - NOSONAR
//	  - @SuppressWarnings
//	  - security-ignore
//
// Inputs:
//
//	content - The code content around the issue.
//	lineStart - Start of the line containing the issue.
//	lineEnd - End of the line containing the issue.
//
// Outputs:
//
//	bool - True if suppression comment found.
//	string - The suppression note if found.
func HasSuppressionComment(content string, lineStart, lineEnd int) (bool, string) {
	if lineStart < 0 {
		lineStart = 0
	}
	if lineEnd > len(content) {
		lineEnd = len(content)
	}

	// Get the line and the previous line (for comment above)
	start := lineStart
	for start > 0 && content[start-1] != '\n' {
		start--
	}
	prevLineStart := start
	if prevLineStart > 0 {
		prevLineStart--
		for prevLineStart > 0 && content[prevLineStart-1] != '\n' {
			prevLineStart--
		}
	}

	end := lineEnd
	for end < len(content) && content[end] != '\n' {
		end++
	}

	// Check both current line and previous line
	searchArea := content[prevLineStart:end]

	suppressionPatterns := []string{
		"nosec",
		"nolint:gosec",
		"NOSONAR",
		"@SuppressWarnings",
		"security-ignore",
		"#nosec",
		"// nosec",
		"/* nosec",
		"# nosec",
	}

	for _, pattern := range suppressionPatterns {
		if idx := strings.Index(strings.ToLower(searchArea), strings.ToLower(pattern)); idx >= 0 {
			// Try to extract the note after the pattern
			afterPattern := searchArea[idx+len(pattern):]
			note := ""
			if len(afterPattern) > 0 {
				// Skip leading whitespace
				afterPattern = strings.TrimLeft(afterPattern, " \t")
				if len(afterPattern) > 0 {
					// Look for a reason in parentheses or after colon
					if afterPattern[0] == ':' {
						afterColon := strings.TrimLeft(afterPattern[1:], " \t")
						endIdx := strings.IndexAny(afterColon, ")\n")
						if endIdx > 0 {
							note = strings.TrimSpace(afterColon[:endIdx])
						} else if len(afterColon) > 0 {
							note = strings.TrimSpace(afterColon)
						}
					} else if afterPattern[0] == '(' {
						endIdx := strings.Index(afterPattern, ")")
						if endIdx > 1 {
							note = strings.TrimSpace(afterPattern[1:endIdx])
						}
					}
				}
			}
			return true, note
		}
	}

	return false, ""
}

// IsSecurityFunction checks if a function name suggests security sensitivity.
//
// Description:
//
//	Checks if the function name contains security-related keywords
//	that indicate the code should be more carefully scrutinized.
//
// Inputs:
//
//	funcName - The function name to check.
//
// Outputs:
//
//	bool - True if the function appears security-sensitive.
func IsSecurityFunction(funcName string) bool {
	name := strings.ToLower(funcName)

	securityKeywords := []string{
		// Authentication
		"auth", "login", "logout", "signin", "signout", "authenticate",
		"verify", "validate", "check",
		// Authorization
		"authorize", "permission", "allowed", "access", "role", "acl",
		// Crypto
		"encrypt", "decrypt", "hash", "sign", "token",
		// Sensitive operations
		"password", "secret", "credential", "key",
		"sanitize", "escape", "filter",
		"admin", "privileged", "sudo",
	}

	for _, keyword := range securityKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}

	return false
}

// defaultFPRates returns industry-benchmark FP rates for patterns.
func defaultFPRates() map[string]float64 {
	return map[string]float64{
		// Injection patterns have lower FP rates due to specific patterns
		"SEC-020": 0.15, // SQL injection
		"SEC-021": 0.10, // Command injection
		"SEC-022": 0.25, // XSS (higher FP rate)
		"SEC-023": 0.10, // Code injection

		// Crypto patterns have moderate FP rates
		"SEC-010": 0.30, // Weak crypto (often used for checksums)
		"SEC-011": 0.15, // Weak hash for passwords
		"SEC-012": 0.20, // Hardcoded credentials

		// Access control has higher FP rates
		"SEC-001": 0.35, // IDOR
		"SEC-002": 0.40, // Missing access control

		// Auth patterns
		"SEC-040": 0.30, // Improper auth
		"SEC-041": 0.40, // Session fixation

		// Other patterns
		"SEC-030": 0.25, // Error info leak
		"SEC-031": 0.20, // Insecure cookie
		"SEC-050": 0.15, // Insecure deserialization
		"SEC-060": 0.35, // Sensitive data in logs
		"SEC-070": 0.25, // SSRF
		"SEC-080": 0.20, // Path traversal
		"SEC-081": 0.40, // TOCTOU race
		"SEC-082": 0.20, // Prototype pollution
		"SEC-083": 0.25, // JWT algorithm confusion
		"SEC-084": 0.15, // Template injection
	}
}
