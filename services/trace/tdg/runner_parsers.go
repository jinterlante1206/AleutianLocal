// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"regexp"
	"strings"
	"sync"
)

// =============================================================================
// TEST OUTPUT PARSERS
// =============================================================================

// TestOutputParser parses test runner output to extract results.
//
// Inputs:
//
//	output - Raw stdout/stderr from test execution
//
// Outputs:
//
//	passed - True if all tests passed
//	failedTests - Names of failed tests
type TestOutputParser func(output []byte) (passed bool, failedTests []string)

// parserRegistry maps languages to their output parsers.
// Protected by parserMu for concurrent access.
var (
	parserRegistry = map[string]TestOutputParser{
		"go":         parseGoTestOutput,
		"python":     parsePytestOutput,
		"typescript": parseJestOutput,
		"javascript": parseJestOutput,
	}
	parserMu sync.RWMutex
)

// GetTestOutputParser returns the parser for a language.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	TestOutputParser - The parser function, or nil if not found
//
// Thread Safety: Safe for concurrent use.
func GetTestOutputParser(language string) TestOutputParser {
	parserMu.RLock()
	defer parserMu.RUnlock()
	return parserRegistry[language]
}

// RegisterTestOutputParser registers a custom parser for a language.
//
// Inputs:
//
//	language - The language identifier
//	parser - The parser function
//
// Thread Safety: Safe for concurrent use.
func RegisterTestOutputParser(language string, parser TestOutputParser) {
	parserMu.Lock()
	defer parserMu.Unlock()
	parserRegistry[language] = parser
}

// =============================================================================
// GO TEST OUTPUT PARSER
// =============================================================================

// Go test output patterns
var (
	goPassPattern    = regexp.MustCompile(`^--- PASS: (\S+)`)
	goFailPattern    = regexp.MustCompile(`^--- FAIL: (\S+)`)
	goOKPattern      = regexp.MustCompile(`^ok\s+`)
	goFAILPattern    = regexp.MustCompile(`^FAIL\s+`)
	goPanicPattern   = regexp.MustCompile(`^panic:`)
	goSkipPattern    = regexp.MustCompile(`^--- SKIP: (\S+)`)
	goRunPattern     = regexp.MustCompile(`^=== RUN\s+(\S+)`)
	goSummaryPattern = regexp.MustCompile(`^(PASS|FAIL)$`)
)

// parseGoTestOutput parses `go test -v` output.
//
// Description:
//
//	Extracts test results from Go test output. Looks for:
//	  - "--- PASS: TestName" for passed tests
//	  - "--- FAIL: TestName" for failed tests
//	  - "panic:" for panics (treated as failure)
//	  - "FAIL" or "ok" summary lines
//
// Inputs:
//
//	output - Raw stdout/stderr from go test
//
// Outputs:
//
//	passed - True if all tests passed (no FAIL lines, no panic)
//	failedTests - Names of failed tests
func parseGoTestOutput(output []byte) (passed bool, failedTests []string) {
	lines := strings.Split(string(output), "\n")
	failedTests = make([]string, 0)
	hasFailure := false
	hasPanic := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for panic
		if goPanicPattern.MatchString(line) {
			hasPanic = true
			hasFailure = true
		}

		// Check for failed test
		if matches := goFailPattern.FindStringSubmatch(line); len(matches) > 1 {
			failedTests = append(failedTests, matches[1])
			hasFailure = true
		}

		// Check for package FAIL
		if goFAILPattern.MatchString(line) {
			hasFailure = true
		}

		// Check for summary FAIL
		if goSummaryPattern.MatchString(line) && line == "FAIL" {
			hasFailure = true
		}
	}

	passed = !hasFailure && !hasPanic
	return passed, failedTests
}

// =============================================================================
// PYTEST OUTPUT PARSER
// =============================================================================

// Pytest output patterns
var (
	pytestPassedPattern   = regexp.MustCompile(`(\d+) passed`)
	pytestFailedPattern   = regexp.MustCompile(`(\d+) failed`)
	pytestErrorPattern    = regexp.MustCompile(`(\d+) error`)
	pytestFailNamePattern = regexp.MustCompile(`^FAILED\s+(\S+)`)
	pytestShortPattern    = regexp.MustCompile(`^(PASSED|FAILED|ERROR)\s+(\S+)`)
)

// parsePytestOutput parses pytest output.
//
// Description:
//
//	Extracts test results from pytest output. Looks for:
//	  - "X passed" in summary
//	  - "X failed" in summary
//	  - "FAILED test_name" lines
//
// Inputs:
//
//	output - Raw stdout/stderr from pytest
//
// Outputs:
//
//	passed - True if no failures or errors
//	failedTests - Names of failed tests
func parsePytestOutput(output []byte) (passed bool, failedTests []string) {
	lines := strings.Split(string(output), "\n")
	failedTests = make([]string, 0)
	hasFailure := false
	hasError := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for FAILED test line
		if matches := pytestFailNamePattern.FindStringSubmatch(line); len(matches) > 1 {
			failedTests = append(failedTests, matches[1])
			hasFailure = true
		}

		// Check short format (PASSED/FAILED/ERROR test_name)
		if matches := pytestShortPattern.FindStringSubmatch(line); len(matches) > 2 {
			if matches[1] == "FAILED" || matches[1] == "ERROR" {
				failedTests = append(failedTests, matches[2])
				hasFailure = true
			}
		}

		// Check summary line for failures
		if pytestFailedPattern.MatchString(line) {
			hasFailure = true
		}

		// Check summary line for errors
		if pytestErrorPattern.MatchString(line) {
			hasError = true
		}
	}

	passed = !hasFailure && !hasError
	return passed, failedTests
}

// =============================================================================
// JEST OUTPUT PARSER
// =============================================================================

// Jest output patterns
var (
	jestPassPattern     = regexp.MustCompile(`✓|PASS`)
	jestFailPattern     = regexp.MustCompile(`✕|✗|FAIL`)
	jestFailNamePattern = regexp.MustCompile(`✕\s+(.+)`)
	jestSummaryPattern  = regexp.MustCompile(`Tests:\s+(\d+)\s+failed`)
	jestPassSummary     = regexp.MustCompile(`Tests:\s+(\d+)\s+passed,\s+(\d+)\s+total`)
)

// parseJestOutput parses Jest test output.
//
// Description:
//
//	Extracts test results from Jest output. Looks for:
//	  - "✓" or "PASS" for passed tests
//	  - "✕" or "FAIL" for failed tests
//	  - "Tests: X failed" in summary
//
// Inputs:
//
//	output - Raw stdout/stderr from jest
//
// Outputs:
//
//	passed - True if no failures
//	failedTests - Names of failed tests (if extractable)
func parseJestOutput(output []byte) (passed bool, failedTests []string) {
	lines := strings.Split(string(output), "\n")
	failedTests = make([]string, 0)
	hasFailure := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for failed test indicator
		if jestFailPattern.MatchString(line) && !strings.Contains(line, "Tests:") {
			hasFailure = true

			// Try to extract test name
			if matches := jestFailNamePattern.FindStringSubmatch(line); len(matches) > 1 {
				failedTests = append(failedTests, strings.TrimSpace(matches[1]))
			}
		}

		// Check summary for failures
		if jestSummaryPattern.MatchString(line) {
			hasFailure = true
		}
	}

	passed = !hasFailure
	return passed, failedTests
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// ExtractTestNames extracts test function names from test output.
//
// Description:
//
//	A generic helper to extract test names that match common patterns.
//	Used when language-specific parsers need additional extraction.
//
// Inputs:
//
//	output - Test output string
//	pattern - Regex pattern with capture group for test name
//
// Outputs:
//
//	[]string - Extracted test names
func ExtractTestNames(output string, pattern *regexp.Regexp) []string {
	matches := pattern.FindAllStringSubmatch(output, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			names = append(names, match[1])
		}
	}
	return names
}

// CountTestResults counts passed and failed tests from output.
//
// Description:
//
//	A generic helper to count test results using patterns.
//
// Inputs:
//
//	output - Test output string
//	passPattern - Pattern matching passed tests
//	failPattern - Pattern matching failed tests
//
// Outputs:
//
//	passed - Number of passed tests
//	failed - Number of failed tests
func CountTestResults(output string, passPattern, failPattern *regexp.Regexp) (passed, failed int) {
	passed = len(passPattern.FindAllString(output, -1))
	failed = len(failPattern.FindAllString(output, -1))
	return
}
