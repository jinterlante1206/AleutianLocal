// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides LogSanitizer for removing PII from logs before LLM processing.

LogSanitizer is a critical security component that ensures no sensitive data
(emails, API keys, credit cards, etc.) is passed to the LLM summarizer.
All log content MUST pass through sanitization before any AI processing.

# Design Rationale

Direct log content cannot be safely included in LLM prompts because:
  - Logs may contain PII (emails, usernames, IP addresses)
  - Logs may contain secrets (API keys, tokens, passwords)
  - LLM providers (even local) should follow data minimization

By applying regex-based sanitization, we ensure defense-in-depth:
  - Redact known patterns before processing
  - Allow custom patterns for domain-specific data
  - Log what was redacted for audit purposes
*/
package main

import (
	"regexp"
	"sync"
	"time"
)

// =============================================================================
// INTERFACE DEFINITIONS
// =============================================================================

// LogSanitizer removes sensitive data before LLM processing.
//
// # Description
//
// This interface applies regex patterns to redact PII and secrets
// from logs before they are processed by the LLM summarizer.
// Implementations must be thread-safe and performant.
//
// # Security
//
// CRITICAL: All log data must pass through sanitization before
// being included in LLM prompts to prevent data leakage.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Examples
//
//	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
//	clean := sanitizer.Sanitize("User john@example.com logged in from 192.168.1.1")
//	// clean = "User [EMAIL_REDACTED] logged in from [IP_REDACTED]"
//
// # Limitations
//
//   - Regex patterns may have false positives
//   - Cannot detect context-dependent sensitive data
//   - Performance degrades with many complex patterns
//
// # Assumptions
//
//   - Patterns are pre-compiled regexes
//   - Order of pattern application may affect results
type LogSanitizer interface {
	// Sanitize removes sensitive patterns from log content.
	//
	// # Description
	//
	// Applies all registered patterns to the input, replacing matches
	// with their configured replacement strings.
	//
	// # Inputs
	//
	//   - logs: Raw log content to sanitize
	//
	// # Outputs
	//
	//   - string: Sanitized logs with PII redacted
	//
	// # Examples
	//
	//   clean := sanitizer.Sanitize("API key: sk-abc123xyz")
	//   // clean = "API key: [KEY_REDACTED]"
	//
	// # Limitations
	//
	//   - Empty input returns empty output
	//   - Very large inputs may be slow
	//
	// # Assumptions
	//
	//   - Input is valid UTF-8 text
	Sanitize(logs string) string

	// AddPattern registers a custom sanitization pattern.
	//
	// # Description
	//
	// Adds a new pattern to the sanitizer. Patterns are applied in
	// the order they were added (after default patterns).
	//
	// # Inputs
	//
	//   - name: Pattern identifier (for logging/debugging)
	//   - pattern: Compiled regex pattern to match
	//   - replacement: Replacement string (may include $1, $2 for groups)
	//
	// # Outputs
	//
	//   - None
	//
	// # Examples
	//
	//   sanitizer.AddPattern("internal_id",
	//       regexp.MustCompile(`INTERNAL-[A-Z0-9]{8}`),
	//       "[INTERNAL_ID_REDACTED]")
	//
	// # Limitations
	//
	//   - Cannot remove patterns once added
	//   - Pattern order matters for overlapping matches
	//
	// # Assumptions
	//
	//   - Pattern is already compiled and valid
	AddPattern(name string, pattern *regexp.Regexp, replacement string)

	// GetPatternCount returns the number of registered patterns.
	//
	// # Description
	//
	// Returns how many patterns are currently registered. Useful for
	// testing and diagnostics.
	//
	// # Inputs
	//
	//   - None
	//
	// # Outputs
	//
	//   - int: Number of registered patterns
	//
	// # Examples
	//
	//   count := sanitizer.GetPatternCount()
	//   fmt.Printf("Sanitizer has %d patterns\n", count)
	//
	// # Limitations
	//
	//   - None
	//
	// # Assumptions
	//
	//   - None
	GetPatternCount() int

	// GetStats returns sanitization statistics.
	//
	// # Description
	//
	// Returns statistics about sanitization operations including
	// total calls, total redactions, and per-pattern counts.
	//
	// # Inputs
	//
	//   - None
	//
	// # Outputs
	//
	//   - *SanitizationStats: Accumulated statistics
	//
	// # Examples
	//
	//   stats := sanitizer.GetStats()
	//   fmt.Printf("Total redactions: %d\n", stats.TotalRedactions)
	//
	// # Limitations
	//
	//   - Stats accumulate since creation (no reset)
	//
	// # Assumptions
	//
	//   - Stats are approximate under high concurrency
	GetStats() *SanitizationStats
}

// =============================================================================
// STRUCT DEFINITIONS
// =============================================================================

// SanitizationPattern defines a PII pattern to redact.
//
// # Description
//
// Contains a compiled regex and its replacement string.
// Each pattern has a name for identification in logs and stats.
//
// # Examples
//
//	pattern := SanitizationPattern{
//	    ID:          GenerateID(),
//	    Name:        "email",
//	    Pattern:     regexp.MustCompile(`[\w.+-]+@[\w.-]+\.\w+`),
//	    Replacement: "[EMAIL_REDACTED]",
//	    Version:     "1.0.0",
//	    CreatedAt:   time.Now(),
//	}
type SanitizationPattern struct {
	// ID is a unique identifier for this pattern.
	ID string

	// Name is a human-readable pattern identifier.
	Name string

	// Pattern is the compiled regex to match.
	Pattern *regexp.Regexp

	// Replacement is the string to replace matches with.
	Replacement string

	// Version is the pattern version for tracking changes.
	Version string

	// CreatedAt is when this pattern was created.
	CreatedAt time.Time
}

// SanitizationStats contains statistics about sanitization operations.
//
// # Description
//
// Tracks how many times the sanitizer has been called and how many
// redactions were made, broken down by pattern.
type SanitizationStats struct {
	// ID is a unique identifier for this stats snapshot.
	ID string

	// TotalCalls is how many times Sanitize was called.
	TotalCalls int64

	// TotalRedactions is total number of redactions across all calls.
	TotalRedactions int64

	// ByPattern maps pattern name to redaction count.
	ByPattern map[string]int64

	// LastSanitization is when Sanitize was last called.
	LastSanitization time.Time

	// CreatedAt is when tracking started.
	CreatedAt time.Time
}

// DefaultLogSanitizer implements LogSanitizer with configurable patterns.
//
// # Description
//
// Provides regex-based log sanitization with built-in patterns for
// common PII types. Thread-safe and designed for high throughput.
//
// # Thread Safety
//
// Safe for concurrent use. Uses RWMutex for pattern access.
type DefaultLogSanitizer struct {
	patterns []SanitizationPattern
	mu       sync.RWMutex

	// Statistics
	totalCalls      int64
	totalRedactions int64
	byPattern       map[string]int64
	lastSanitize    time.Time
	createdAt       time.Time
}

// MockLogSanitizer is a test double for LogSanitizer.
//
// # Description
//
// Allows tests to control sanitization behavior and verify calls.
type MockLogSanitizer struct {
	// SanitizeFunc overrides the Sanitize behavior.
	SanitizeFunc func(logs string) string

	// SanitizeCalls records all calls to Sanitize.
	SanitizeCalls []string

	// AddedPatterns records patterns added via AddPattern.
	AddedPatterns []SanitizationPattern

	// StatsToReturn is returned by GetStats.
	StatsToReturn *SanitizationStats

	mu sync.Mutex
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS
// =============================================================================

// NewDefaultLogSanitizer creates a sanitizer with the provided patterns.
//
// # Description
//
// Creates a new DefaultLogSanitizer initialized with the given patterns.
// Use DefaultSanitizationPatterns() for standard PII patterns.
//
// # Inputs
//
//   - patterns: Initial patterns to register
//
// # Outputs
//
//   - *DefaultLogSanitizer: Ready-to-use sanitizer
//
// # Examples
//
//	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
//
// # Limitations
//
//   - Patterns are copied, not referenced
//
// # Assumptions
//
//   - Patterns have valid compiled regexes
func NewDefaultLogSanitizer(patterns []SanitizationPattern) *DefaultLogSanitizer {
	now := time.Now()

	// Assign IDs to patterns that don't have them
	for i := range patterns {
		if patterns[i].ID == "" {
			patterns[i].ID = GenerateID()
		}
		if patterns[i].CreatedAt.IsZero() {
			patterns[i].CreatedAt = now
		}
		if patterns[i].Version == "" {
			patterns[i].Version = "1.0.0"
		}
	}

	return &DefaultLogSanitizer{
		patterns:  patterns,
		byPattern: make(map[string]int64),
		createdAt: now,
	}
}

// DefaultSanitizationPatterns returns patterns for common PII types.
//
// # Description
//
// Returns a comprehensive set of patterns covering emails, IPs,
// API keys, tokens, credit cards, SSNs, and other sensitive data.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - []SanitizationPattern: Standard PII patterns
//
// # Examples
//
//	patterns := DefaultSanitizationPatterns()
//	fmt.Printf("Loaded %d patterns\n", len(patterns))
//
// # Limitations
//
//   - Patterns may have false positives (e.g., version numbers vs IPs)
//   - US-centric patterns (SSN format, phone format)
//
// # Assumptions
//
//   - Patterns compile successfully (they are validated at init)
func DefaultSanitizationPatterns() []SanitizationPattern {
	now := time.Now()
	return []SanitizationPattern{
		// Email addresses
		{
			ID:          GenerateID(),
			Name:        "email",
			Pattern:     regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			Replacement: "[EMAIL_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// IP addresses (IPv4)
		{
			ID:          GenerateID(),
			Name:        "ipv4",
			Pattern:     regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
			Replacement: "[IP_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// IPv6 addresses (simplified pattern)
		{
			ID:          GenerateID(),
			Name:        "ipv6",
			Pattern:     regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b`),
			Replacement: "[IPV6_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// API keys (common patterns)
		{
			ID:          GenerateID(),
			Name:        "api_key",
			Pattern:     regexp.MustCompile(`(?i)(api[_\-]?key|apikey|secret[_\-]?key|auth[_\-]?token)[=:\s]["']?([a-zA-Z0-9_\-]{16,})["']?`),
			Replacement: "$1=[KEY_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Bearer tokens
		{
			ID:          GenerateID(),
			Name:        "bearer",
			Pattern:     regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-\.]+`),
			Replacement: "Bearer [TOKEN_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Credit card numbers (basic pattern)
		{
			ID:          GenerateID(),
			Name:        "credit_card",
			Pattern:     regexp.MustCompile(`\b(?:\d{4}[\s\-]?){3}\d{4}\b`),
			Replacement: "[CC_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// SSN (US format)
		{
			ID:          GenerateID(),
			Name:        "ssn",
			Pattern:     regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			Replacement: "[SSN_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// AWS access keys
		{
			ID:          GenerateID(),
			Name:        "aws_key",
			Pattern:     regexp.MustCompile(`(?i)(AKIA|ABIA|ACCA|ASIA)[A-Z0-9]{16}`),
			Replacement: "[AWS_KEY_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Generic hex secrets (32+ chars, likely tokens/hashes)
		{
			ID:          GenerateID(),
			Name:        "hex_secret",
			Pattern:     regexp.MustCompile(`\b[a-fA-F0-9]{32,}\b`),
			Replacement: "[HEX_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// JWT tokens
		{
			ID:          GenerateID(),
			Name:        "jwt",
			Pattern:     regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`),
			Replacement: "[JWT_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// File paths with usernames (macOS)
		{
			ID:          GenerateID(),
			Name:        "user_path_mac",
			Pattern:     regexp.MustCompile(`/Users/[a-zA-Z0-9_\-]+/`),
			Replacement: "/Users/[USER]/",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Home directory paths (Linux)
		{
			ID:          GenerateID(),
			Name:        "home_path_linux",
			Pattern:     regexp.MustCompile(`/home/[a-zA-Z0-9_\-]+/`),
			Replacement: "/home/[USER]/",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Private key content
		{
			ID:          GenerateID(),
			Name:        "private_key",
			Pattern:     regexp.MustCompile(`(?i)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----[\s\S]*?-----END\s+(RSA\s+)?PRIVATE\s+KEY-----`),
			Replacement: "[PRIVATE_KEY_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Password in URLs
		{
			ID:          GenerateID(),
			Name:        "url_password",
			Pattern:     regexp.MustCompile(`://[^:]+:([^@]+)@`),
			Replacement: "://[USER]:[PASSWORD_REDACTED]@",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
		// Phone numbers (US format)
		{
			ID:          GenerateID(),
			Name:        "phone_us",
			Pattern:     regexp.MustCompile(`\b(?:\+1[\s\-]?)?(?:\(?\d{3}\)?[\s\-]?)?\d{3}[\s\-]?\d{4}\b`),
			Replacement: "[PHONE_REDACTED]",
			Version:     "1.0.0",
			CreatedAt:   now,
		},
	}
}

// =============================================================================
// DefaultLogSanitizer METHODS
// =============================================================================

// Sanitize removes sensitive patterns from log content.
//
// # Description
//
// Applies all registered patterns sequentially, replacing matches
// with their configured replacement strings. Tracks statistics.
//
// # Inputs
//
//   - logs: Raw log content to sanitize
//
// # Outputs
//
//   - string: Sanitized logs with PII redacted
//
// # Examples
//
//	clean := sanitizer.Sanitize("Error for user@example.com at 10.0.0.1")
//	// clean = "Error for [EMAIL_REDACTED] at [IP_REDACTED]"
//
// # Limitations
//
//   - Patterns applied in order; earlier patterns may affect later matches
//   - Large inputs with many matches may be slow
//
// # Assumptions
//
//   - Input is valid UTF-8
func (s *DefaultLogSanitizer) Sanitize(logs string) string {
	if logs == "" {
		return logs
	}

	s.mu.Lock()
	s.totalCalls++
	s.lastSanitize = time.Now()
	patterns := make([]SanitizationPattern, len(s.patterns))
	copy(patterns, s.patterns)
	s.mu.Unlock()

	result := logs
	for _, p := range patterns {
		matches := p.Pattern.FindAllStringIndex(result, -1)
		if len(matches) > 0 {
			s.mu.Lock()
			s.totalRedactions += int64(len(matches))
			s.byPattern[p.Name] += int64(len(matches))
			s.mu.Unlock()
		}
		result = p.Pattern.ReplaceAllString(result, p.Replacement)
	}

	return result
}

// AddPattern registers a custom sanitization pattern.
//
// # Description
//
// Adds a new pattern to the end of the pattern list. Patterns are
// applied in order, so later patterns may match replacements from earlier ones.
//
// # Inputs
//
//   - name: Pattern identifier
//   - pattern: Compiled regex
//   - replacement: Replacement string
//
// # Outputs
//
//   - None
//
// # Examples
//
//	sanitizer.AddPattern("custom_id",
//	    regexp.MustCompile(`CUST-\d{8}`),
//	    "[CUSTOMER_ID_REDACTED]")
//
// # Limitations
//
//   - Cannot remove or reorder patterns
//
// # Assumptions
//
//   - Pattern is valid and compiled
func (s *DefaultLogSanitizer) AddPattern(name string, pattern *regexp.Regexp, replacement string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.patterns = append(s.patterns, SanitizationPattern{
		ID:          GenerateID(),
		Name:        name,
		Pattern:     pattern,
		Replacement: replacement,
		Version:     "1.0.0",
		CreatedAt:   time.Now(),
	})
}

// GetPatternCount returns the number of registered patterns.
//
// # Description
//
// Returns how many patterns are currently registered.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - int: Number of patterns
func (s *DefaultLogSanitizer) GetPatternCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.patterns)
}

// GetStats returns sanitization statistics.
//
// # Description
//
// Returns a snapshot of statistics including total calls,
// total redactions, and per-pattern counts.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - *SanitizationStats: Statistics snapshot
func (s *DefaultLogSanitizer) GetStats() *SanitizationStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byPattern := make(map[string]int64)
	for k, v := range s.byPattern {
		byPattern[k] = v
	}

	return &SanitizationStats{
		ID:               GenerateID(),
		TotalCalls:       s.totalCalls,
		TotalRedactions:  s.totalRedactions,
		ByPattern:        byPattern,
		LastSanitization: s.lastSanitize,
		CreatedAt:        s.createdAt,
	}
}

// =============================================================================
// MockLogSanitizer METHODS
// =============================================================================

// Sanitize calls the mock function or returns the input unchanged.
func (m *MockLogSanitizer) Sanitize(logs string) string {
	m.mu.Lock()
	m.SanitizeCalls = append(m.SanitizeCalls, logs)
	m.mu.Unlock()

	if m.SanitizeFunc != nil {
		return m.SanitizeFunc(logs)
	}
	return logs
}

// AddPattern records the pattern for verification.
func (m *MockLogSanitizer) AddPattern(name string, pattern *regexp.Regexp, replacement string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AddedPatterns = append(m.AddedPatterns, SanitizationPattern{
		ID:          GenerateID(),
		Name:        name,
		Pattern:     pattern,
		Replacement: replacement,
		CreatedAt:   time.Now(),
	})
}

// GetPatternCount returns the count of added patterns.
func (m *MockLogSanitizer) GetPatternCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.AddedPatterns)
}

// GetStats returns the configured stats or empty stats.
func (m *MockLogSanitizer) GetStats() *SanitizationStats {
	if m.StatsToReturn != nil {
		return m.StatsToReturn
	}
	return &SanitizationStats{
		ID:        GenerateID(),
		ByPattern: make(map[string]int64),
		CreatedAt: time.Now(),
	}
}
