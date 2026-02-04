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
	"path/filepath"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/validate"
)

// HybridSecurityScanner combines AST-based and regex-based security scanning.
//
// # Description
//
// For known languages (Go, Python, JavaScript, TypeScript), uses tree-sitter
// AST analysis which automatically excludes patterns in comments and strings.
// For unknown languages or when AST parsing fails, falls back to regex-based
// BasicSecurityScanner.
//
// This addresses the "Regex Castle" problem where security patterns in
// comments trigger false positives (e.g., "// TODO: Remove exec.Command").
//
// # Thread Safety
//
// Safe for concurrent use. The scanner maintains no mutable state after
// construction. Both underlying scanners are also thread-safe.
type HybridSecurityScanner struct {
	astScanner   *validate.ASTScanner
	regexScanner *BasicSecurityScanner
	excludeTests bool
	metrics      *HybridScannerMetrics
}

// HybridScannerMetrics tracks scanner usage for observability.
type HybridScannerMetrics struct {
	mu            sync.Mutex
	ASTScans      int64
	RegexScans    int64
	ASTFallbacks  int64
	TestsExcluded int64
}

// HybridScannerOption configures the HybridSecurityScanner.
type HybridScannerOption func(*HybridSecurityScanner)

// WithExcludeTests configures the scanner to skip test files.
//
// Test files are detected by patterns:
//   - *_test.go (Go)
//   - test_*.py, *_test.py (Python)
//   - *.test.js, *.test.ts, *.spec.js, *.spec.ts (JavaScript/TypeScript)
func WithExcludeTests(exclude bool) HybridScannerOption {
	return func(s *HybridSecurityScanner) {
		s.excludeTests = exclude
	}
}

// WithCustomRegexPatterns provides custom regex patterns for fallback scanning.
func WithCustomRegexPatterns(patterns []SecurityPattern) HybridScannerOption {
	return func(s *HybridSecurityScanner) {
		s.regexScanner = NewBasicSecurityScannerWithPatterns(patterns)
	}
}

// NewHybridSecurityScanner creates a new hybrid scanner.
//
// # Inputs
//
//   - opts: Optional configuration options.
//
// # Outputs
//
//   - *HybridSecurityScanner: Ready to use hybrid scanner.
func NewHybridSecurityScanner(opts ...HybridScannerOption) *HybridSecurityScanner {
	s := &HybridSecurityScanner{
		astScanner:   validate.NewASTScanner(),
		regexScanner: NewBasicSecurityScanner(),
		excludeTests: false,
		metrics:      &HybridScannerMetrics{},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// supportedASTLanguages maps file extensions and language names to AST-supported languages.
var supportedASTLanguages = map[string]string{
	// Extensions
	".go":  "go",
	".py":  "python",
	".js":  "javascript",
	".jsx": "javascript",
	".ts":  "typescript",
	".tsx": "typescript",
	".mjs": "javascript",
	".cjs": "javascript",
	// Language names (already normalized)
	"go":         "go",
	"golang":     "go",
	"python":     "python",
	"python3":    "python",
	"javascript": "javascript",
	"js":         "javascript",
	"typescript": "typescript",
	"ts":         "typescript",
}

// ScanCode scans code for security issues using the optimal method.
//
// # Description
//
// Determines the best scanning method based on the language:
//   - For Go, Python, JavaScript, TypeScript: Uses AST-based scanning
//   - For other languages or unknown: Falls back to regex-based scanning
//   - If AST parsing fails: Falls back to regex with a warning flag
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - code: The source code to scan.
//   - language: Programming language (e.g., "go", "python"). Empty = unknown.
//   - filePath: File path for reporting and test file detection.
//
// # Outputs
//
//   - *SecurityScanResult: Scan results with score and issues.
//   - error: Non-nil on context cancellation.
//
// # Thread Safety
//
// Safe for concurrent use.
func (s *HybridSecurityScanner) ScanCode(ctx context.Context, code, language, filePath string) (*SecurityScanResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check for test file exclusion
	if s.excludeTests && isTestFile(filePath) {
		s.recordMetric(func(m *HybridScannerMetrics) { m.TestsExcluded++ })
		return &SecurityScanResult{
			Score:  1.0,
			Issues: []SecurityIssue{},
		}, nil
	}

	// Normalize language
	normalizedLang := s.normalizeLanguage(language, filePath)

	// Check if AST scanning is available for this language
	if astLang, ok := supportedASTLanguages[normalizedLang]; ok {
		result, err := s.scanWithAST(ctx, code, astLang, filePath)
		if err == nil {
			s.recordMetric(func(m *HybridScannerMetrics) { m.ASTScans++ })
			return result, nil
		}

		// AST parsing failed, fall back to regex
		s.recordMetric(func(m *HybridScannerMetrics) { m.ASTFallbacks++ })
	}

	// Fall back to regex-based scanning
	s.recordMetric(func(m *HybridScannerMetrics) { m.RegexScans++ })
	return s.regexScanner.ScanCodeWithLanguage(ctx, code, language)
}

// scanWithAST performs AST-based security scanning.
func (s *HybridSecurityScanner) scanWithAST(ctx context.Context, code, language, filePath string) (*SecurityScanResult, error) {
	warnings, err := s.astScanner.Scan(ctx, []byte(code), language, filePath)
	if err != nil {
		return nil, err
	}

	// Convert ValidationWarning to SecurityScanResult
	return s.convertASTWarnings(warnings), nil
}

// convertASTWarnings converts ASTScanner warnings to SecurityScanResult.
func (s *HybridSecurityScanner) convertASTWarnings(warnings []validate.ValidationWarning) *SecurityScanResult {
	result := &SecurityScanResult{
		Score:  1.0,
		Issues: make([]SecurityIssue, 0, len(warnings)),
	}

	for _, w := range warnings {
		issue := SecurityIssue{
			Severity: mapValidateSeverity(w.Severity),
			Message:  w.Message,
			Pattern:  w.Pattern,
		}
		result.Issues = append(result.Issues, issue)

		// Apply score deductions
		switch w.Severity {
		case validate.SeverityCritical:
			result.Score -= 0.5
		case validate.SeverityHigh:
			result.Score -= 0.3
		case validate.SeverityMedium:
			result.Score -= 0.15
		case validate.SeverityLow:
			result.Score -= 0.05
		}
	}

	// Clamp score
	if result.Score < 0 {
		result.Score = 0
	}

	return result
}

// mapValidateSeverity maps validate.Severity to string severity.
func mapValidateSeverity(sev validate.Severity) string {
	switch sev {
	case validate.SeverityCritical:
		return "critical"
	case validate.SeverityHigh:
		return "high"
	case validate.SeverityMedium:
		return "medium"
	case validate.SeverityLow:
		return "low"
	default:
		return "medium"
	}
}

// normalizeLanguage normalizes the language identifier.
func (s *HybridSecurityScanner) normalizeLanguage(language, filePath string) string {
	// If language is provided, normalize it
	if language != "" {
		normalized := strings.ToLower(strings.TrimSpace(language))
		if _, ok := supportedASTLanguages[normalized]; ok {
			return normalized
		}
	}

	// Try to detect from file extension
	if filePath != "" {
		ext := strings.ToLower(filepath.Ext(filePath))
		if _, ok := supportedASTLanguages[ext]; ok {
			return ext
		}
	}

	return language
}

// isTestFile checks if a file path matches common test file patterns.
func isTestFile(filePath string) bool {
	if filePath == "" {
		return false
	}

	base := filepath.Base(filePath)
	lower := strings.ToLower(base)

	// Go test files
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}

	// Python test files
	if strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py") {
		return true
	}
	if strings.HasSuffix(lower, "_test.py") {
		return true
	}

	// JavaScript/TypeScript test files
	if strings.HasSuffix(lower, ".test.js") || strings.HasSuffix(lower, ".test.ts") {
		return true
	}
	if strings.HasSuffix(lower, ".spec.js") || strings.HasSuffix(lower, ".spec.ts") {
		return true
	}
	if strings.HasSuffix(lower, ".test.jsx") || strings.HasSuffix(lower, ".test.tsx") {
		return true
	}
	if strings.HasSuffix(lower, ".spec.jsx") || strings.HasSuffix(lower, ".spec.tsx") {
		return true
	}

	// Directory patterns
	dir := filepath.Dir(filePath)
	if strings.Contains(dir, "testdata") || strings.Contains(dir, "__tests__") ||
		strings.Contains(dir, "fixtures") {
		return true
	}

	// Check if in a test directory (matches "test", "test/", "/test/", etc.)
	if dir == "test" || strings.HasPrefix(dir, "test/") ||
		strings.HasPrefix(dir, "test"+string(filepath.Separator)) ||
		strings.Contains(dir, "/test/") || strings.Contains(dir, "/test") {
		return true
	}

	return false
}

// recordMetric safely records a metric update.
func (s *HybridSecurityScanner) recordMetric(fn func(*HybridScannerMetrics)) {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	fn(s.metrics)
}

// Metrics returns a copy of the current scanner metrics.
//
// # Thread Safety
//
// Safe for concurrent use.
func (s *HybridSecurityScanner) Metrics() HybridScannerMetrics {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	return HybridScannerMetrics{
		ASTScans:      s.metrics.ASTScans,
		RegexScans:    s.metrics.RegexScans,
		ASTFallbacks:  s.metrics.ASTFallbacks,
		TestsExcluded: s.metrics.TestsExcluded,
	}
}

// ResetMetrics resets all scanner metrics to zero.
//
// # Thread Safety
//
// Safe for concurrent use.
func (s *HybridSecurityScanner) ResetMetrics() {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	s.metrics.ASTScans = 0
	s.metrics.RegexScans = 0
	s.metrics.ASTFallbacks = 0
	s.metrics.TestsExcluded = 0
}

// ScanMode indicates which scanning method was used.
type ScanMode string

const (
	ScanModeAST      ScanMode = "ast"
	ScanModeRegex    ScanMode = "regex"
	ScanModeSkipped  ScanMode = "skipped"
	ScanModeFallback ScanMode = "fallback"
)

// ScanCodeWithMode scans code and returns the scan mode used.
//
// # Description
//
// Same as ScanCode but also returns which scanning method was used.
// Useful for debugging and metrics collection.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - code: The source code to scan.
//   - language: Programming language.
//   - filePath: File path for reporting.
//
// # Outputs
//
//   - *SecurityScanResult: Scan results.
//   - ScanMode: Which scanning method was used.
//   - error: Non-nil on context cancellation.
func (s *HybridSecurityScanner) ScanCodeWithMode(ctx context.Context, code, language, filePath string) (*SecurityScanResult, ScanMode, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Check for test file exclusion
	if s.excludeTests && isTestFile(filePath) {
		s.recordMetric(func(m *HybridScannerMetrics) { m.TestsExcluded++ })
		return &SecurityScanResult{
			Score:  1.0,
			Issues: []SecurityIssue{},
		}, ScanModeSkipped, nil
	}

	// Normalize language
	normalizedLang := s.normalizeLanguage(language, filePath)

	// Check if AST scanning is available
	if astLang, ok := supportedASTLanguages[normalizedLang]; ok {
		result, err := s.scanWithAST(ctx, code, astLang, filePath)
		if err == nil {
			s.recordMetric(func(m *HybridScannerMetrics) { m.ASTScans++ })
			return result, ScanModeAST, nil
		}

		// AST parsing failed, fall back
		s.recordMetric(func(m *HybridScannerMetrics) { m.ASTFallbacks++ })
		result, err = s.regexScanner.ScanCodeWithLanguage(ctx, code, language)
		return result, ScanModeFallback, err
	}

	// Regex scanning
	s.recordMetric(func(m *HybridScannerMetrics) { m.RegexScans++ })
	result, err := s.regexScanner.ScanCodeWithLanguage(ctx, code, language)
	return result, ScanModeRegex, err
}
