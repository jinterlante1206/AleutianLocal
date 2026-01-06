// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

// =============================================================================
// DefaultLogSanitizer TESTS
// =============================================================================

func TestDefaultLogSanitizer_Sanitize_Email(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple email",
			input:    "User john@example.com logged in",
			expected: "User [EMAIL_REDACTED] logged in",
		},
		{
			name:     "email with plus",
			input:    "Contact user+tag@domain.org",
			expected: "Contact [EMAIL_REDACTED]",
		},
		{
			name:     "multiple emails",
			input:    "From: a@b.com To: c@d.org",
			expected: "From: [EMAIL_REDACTED] To: [EMAIL_REDACTED]",
		},
		{
			name:     "email in URL",
			input:    "mailto:test@example.com?subject=Hi",
			expected: "mailto:[EMAIL_REDACTED]?subject=Hi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_IPv4(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple IP",
			input:    "Connection from 192.168.1.100",
			expected: "Connection from [IP_REDACTED]",
		},
		{
			name:     "localhost",
			input:    "Listening on 127.0.0.1:8080",
			expected: "Listening on [IP_REDACTED]:8080",
		},
		{
			name:     "multiple IPs",
			input:    "Route: 10.0.0.1 -> 10.0.0.2",
			expected: "Route: [IP_REDACTED] -> [IP_REDACTED]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_APIKey(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "api_key format",
			input:    "api_key=sk_live_1234567890abcdef",
			contains: "[KEY_REDACTED]",
		},
		{
			name:     "apikey format",
			input:    "apikey=abcdef1234567890abcd",
			contains: "[KEY_REDACTED]",
		},
		{
			name:     "secret_key format",
			input:    "secret_key='mysupersecretkey123'",
			contains: "[KEY_REDACTED]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if !strings.Contains(result, tc.contains) {
				t.Errorf("Expected result to contain %q, got %q", tc.contains, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_Bearer(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[TOKEN_REDACTED]") {
		t.Errorf("Expected Bearer token to be redacted, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_CreditCard(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "spaces",
			input:    "Card: 4111 1111 1111 1111",
			expected: "Card: [CC_REDACTED]",
		},
		{
			name:     "dashes",
			input:    "Card: 5500-0000-0000-0004",
			expected: "Card: [CC_REDACTED]",
		},
		{
			name:     "no separator",
			input:    "Card: 4000000000000002",
			expected: "Card: [CC_REDACTED]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_SSN(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "SSN: 123-45-6789"
	expected := "SSN: [SSN_REDACTED]"

	result := sanitizer.Sanitize(input)
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestDefaultLogSanitizer_Sanitize_AWSKey(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AKIA key",
			input:    "AWS Key: AKIAIOSFODNN7EXAMPLE",
			expected: "AWS Key: [AWS_KEY_REDACTED]",
		},
		{
			name:     "ASIA key",
			input:    "Key: ASIAXXXXXXXXXYYYYYYY",
			expected: "Key: [AWS_KEY_REDACTED]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_HexSecret(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "Hash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[HEX_REDACTED]") {
		t.Errorf("Expected hex secret to be redacted, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_JWT(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[JWT_REDACTED]") {
		t.Errorf("Expected JWT to be redacted, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_UserPaths(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "macOS path",
			input:    "File: /Users/johndoe/Documents/secret.txt",
			expected: "File: /Users/[USER]/Documents/secret.txt",
		},
		{
			name:     "Linux path",
			input:    "File: /home/alice/config.json",
			expected: "File: /home/[USER]/config.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestDefaultLogSanitizer_Sanitize_PrivateKey(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDZ
-----END PRIVATE KEY-----`

	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[PRIVATE_KEY_REDACTED]") {
		t.Errorf("Expected private key to be redacted, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_URLPassword(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "Database: postgres://admin:secretpass@localhost:5432/db"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[PASSWORD_REDACTED]") {
		t.Errorf("Expected URL password to be redacted, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_EmptyInput(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	result := sanitizer.Sanitize("")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_NoSensitiveData(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "INFO: Server started successfully on port 8080"
	result := sanitizer.Sanitize(input)

	if result != input {
		t.Errorf("Expected unchanged input, got %q", result)
	}
}

func TestDefaultLogSanitizer_Sanitize_MultipleSensitiveTypes(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	input := "User john@example.com from 192.168.1.1 with SSN 123-45-6789"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "[EMAIL_REDACTED]") {
		t.Error("Expected email to be redacted")
	}
	if !strings.Contains(result, "[IP_REDACTED]") {
		t.Error("Expected IP to be redacted")
	}
	if !strings.Contains(result, "[SSN_REDACTED]") {
		t.Error("Expected SSN to be redacted")
	}
}

func TestDefaultLogSanitizer_AddPattern(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(nil)

	initialCount := sanitizer.GetPatternCount()
	if initialCount != 0 {
		t.Errorf("Expected 0 patterns, got %d", initialCount)
	}

	sanitizer.AddPattern("custom",
		regexp.MustCompile(`CUSTOM-\d{8}`),
		"[CUSTOM_REDACTED]")

	if sanitizer.GetPatternCount() != 1 {
		t.Errorf("Expected 1 pattern, got %d", sanitizer.GetPatternCount())
	}

	input := "ID: CUSTOM-12345678"
	expected := "ID: [CUSTOM_REDACTED]"
	result := sanitizer.Sanitize(input)

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestDefaultLogSanitizer_GetPatternCount(t *testing.T) {
	patterns := DefaultSanitizationPatterns()
	sanitizer := NewDefaultLogSanitizer(patterns)

	count := sanitizer.GetPatternCount()
	if count != len(patterns) {
		t.Errorf("Expected %d patterns, got %d", len(patterns), count)
	}
}

func TestDefaultLogSanitizer_GetStats(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	// Initial stats
	stats := sanitizer.GetStats()
	if stats.TotalCalls != 0 {
		t.Errorf("Expected 0 calls, got %d", stats.TotalCalls)
	}
	if stats.TotalRedactions != 0 {
		t.Errorf("Expected 0 redactions, got %d", stats.TotalRedactions)
	}

	// Perform sanitizations
	sanitizer.Sanitize("user@example.com and 192.168.1.1")
	sanitizer.Sanitize("another@email.org")

	stats = sanitizer.GetStats()
	if stats.TotalCalls != 2 {
		t.Errorf("Expected 2 calls, got %d", stats.TotalCalls)
	}
	if stats.TotalRedactions < 3 {
		t.Errorf("Expected at least 3 redactions, got %d", stats.TotalRedactions)
	}
	if stats.ByPattern["email"] < 2 {
		t.Errorf("Expected at least 2 email redactions, got %d", stats.ByPattern["email"])
	}
	if stats.ID == "" {
		t.Error("Expected stats to have an ID")
	}
	if stats.CreatedAt.IsZero() {
		t.Error("Expected stats to have a CreatedAt timestamp")
	}
}

func TestDefaultLogSanitizer_ThreadSafety(t *testing.T) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent sanitization
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sanitizer.Sanitize("user@example.com from 10.0.0.1")
		}(i)
	}

	// Concurrent pattern addition
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sanitizer.AddPattern("custom",
				regexp.MustCompile(`TEST-\d+`),
				"[REDACTED]")
		}(i)
	}

	// Concurrent stats access
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sanitizer.GetStats()
			_ = sanitizer.GetPatternCount()
		}()
	}

	wg.Wait()

	stats := sanitizer.GetStats()
	if stats.TotalCalls != int64(iterations) {
		t.Errorf("Expected %d calls, got %d", iterations, stats.TotalCalls)
	}
}

func TestDefaultSanitizationPatterns_AllPatternsCompile(t *testing.T) {
	patterns := DefaultSanitizationPatterns()

	for _, p := range patterns {
		if p.Pattern == nil {
			t.Errorf("Pattern %q has nil regex", p.Name)
		}
		if p.Name == "" {
			t.Error("Pattern has empty name")
		}
		if p.Replacement == "" {
			t.Errorf("Pattern %q has empty replacement", p.Name)
		}
		if p.ID == "" {
			t.Errorf("Pattern %q has empty ID", p.Name)
		}
		if p.Version == "" {
			t.Errorf("Pattern %q has empty version", p.Name)
		}
		if p.CreatedAt.IsZero() {
			t.Errorf("Pattern %q has zero CreatedAt", p.Name)
		}
	}
}

func TestDefaultSanitizationPatterns_Coverage(t *testing.T) {
	patterns := DefaultSanitizationPatterns()

	expectedPatterns := []string{
		"email", "ipv4", "ipv6", "api_key", "bearer",
		"credit_card", "ssn", "aws_key", "hex_secret", "jwt",
		"user_path_mac", "home_path_linux", "private_key", "url_password", "phone_us",
	}

	patternNames := make(map[string]bool)
	for _, p := range patterns {
		patternNames[p.Name] = true
	}

	for _, expected := range expectedPatterns {
		if !patternNames[expected] {
			t.Errorf("Missing expected pattern: %q", expected)
		}
	}
}

// =============================================================================
// MockLogSanitizer TESTS
// =============================================================================

func TestMockLogSanitizer_DefaultBehavior(t *testing.T) {
	mock := &MockLogSanitizer{}

	input := "sensitive data"
	result := mock.Sanitize(input)

	// Default behavior is passthrough
	if result != input {
		t.Errorf("Expected %q, got %q", input, result)
	}

	// Verify call was recorded
	if len(mock.SanitizeCalls) != 1 {
		t.Errorf("Expected 1 call recorded, got %d", len(mock.SanitizeCalls))
	}
	if mock.SanitizeCalls[0] != input {
		t.Errorf("Expected recorded call %q, got %q", input, mock.SanitizeCalls[0])
	}
}

func TestMockLogSanitizer_CustomFunction(t *testing.T) {
	mock := &MockLogSanitizer{
		SanitizeFunc: func(logs string) string {
			return "[ALL_REDACTED]"
		},
	}

	result := mock.Sanitize("anything")
	if result != "[ALL_REDACTED]" {
		t.Errorf("Expected [ALL_REDACTED], got %q", result)
	}
}

func TestMockLogSanitizer_AddPattern(t *testing.T) {
	mock := &MockLogSanitizer{}

	mock.AddPattern("test", regexp.MustCompile(`test`), "[REDACTED]")

	if len(mock.AddedPatterns) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(mock.AddedPatterns))
	}
	if mock.AddedPatterns[0].Name != "test" {
		t.Errorf("Expected pattern name 'test', got %q", mock.AddedPatterns[0].Name)
	}
}

func TestMockLogSanitizer_GetStats(t *testing.T) {
	mock := &MockLogSanitizer{
		StatsToReturn: &SanitizationStats{
			TotalCalls:      42,
			TotalRedactions: 100,
		},
	}

	stats := mock.GetStats()
	if stats.TotalCalls != 42 {
		t.Errorf("Expected 42 calls, got %d", stats.TotalCalls)
	}
}

func TestMockLogSanitizer_GetStats_Default(t *testing.T) {
	mock := &MockLogSanitizer{}

	stats := mock.GetStats()
	if stats == nil {
		t.Error("Expected non-nil stats")
	}
	if stats.ID == "" {
		t.Error("Expected stats to have an ID")
	}
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

func BenchmarkDefaultLogSanitizer_Sanitize_Simple(b *testing.B) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
	input := "User john@example.com logged in from 192.168.1.1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.Sanitize(input)
	}
}

func BenchmarkDefaultLogSanitizer_Sanitize_Complex(b *testing.B) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
	input := `2026-01-05 10:30:00 INFO User john@example.com logged in from 192.168.1.1
	SSN: 123-45-6789, Card: 4111 1111 1111 1111
	API-KEY: sk_live_abcdefghijklmnop
	JWT: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sig
	Path: /Users/johndoe/Documents/secret.txt`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.Sanitize(input)
	}
}

func BenchmarkDefaultLogSanitizer_Sanitize_NoMatches(b *testing.B) {
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
	input := "INFO: Server started successfully. No sensitive data here."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.Sanitize(input)
	}
}
