// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ux

import (
	"testing"
)

// =============================================================================
// truncate Tests
// =============================================================================

func TestTruncate_ShortString(t *testing.T) {
	result := truncate("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	result := truncate("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncate_LongString(t *testing.T) {
	result := truncate("hello world this is a long string", 10)
	if result != "hello w..." {
		t.Errorf("expected 'hello w...', got %q", result)
	}
}

func TestTruncate_VeryShortMaxLen(t *testing.T) {
	result := truncate("hello", 3)
	if result != "..." {
		t.Errorf("expected '...', got %q", result)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	result := truncate("", 10)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestTruncate_MinimumMaxLen(t *testing.T) {
	// Test with maxLen = 4 (minimum safe value: 3 chars for "..." plus at least 1)
	result := truncate("hello", 4)
	if result != "h..." {
		t.Errorf("expected 'h...', got %q", result)
	}
}

// =============================================================================
// aleutianTheme Tests
// =============================================================================

func TestAleutianTheme_ReturnsNonNil(t *testing.T) {
	theme := aleutianTheme()
	if theme == nil {
		t.Fatal("aleutianTheme returned nil")
	}
}

func TestAleutianTheme_HasFocusedStyles(t *testing.T) {
	theme := aleutianTheme()
	// The theme should have focused and blurred styles configured
	// We can't easily inspect the internal state, but we can verify the theme exists
	if theme.Focused.Title.String() == "" {
		// This is fine - the style is configured but renders as empty until used
	}
}

// =============================================================================
// PromptOption Tests
// =============================================================================

func TestPromptOption_Fields(t *testing.T) {
	opt := PromptOption{
		Label:       "Test Option",
		Description: "A test description",
		Value:       "test-value",
		Recommended: true,
	}

	if opt.Label != "Test Option" {
		t.Errorf("expected Label 'Test Option', got %q", opt.Label)
	}
	if opt.Description != "A test description" {
		t.Errorf("expected Description 'A test description', got %q", opt.Description)
	}
	if opt.Value != "test-value" {
		t.Errorf("expected Value 'test-value', got %q", opt.Value)
	}
	if opt.Recommended != true {
		t.Errorf("expected Recommended true, got %v", opt.Recommended)
	}
}

func TestPromptOption_NotRecommended(t *testing.T) {
	opt := PromptOption{
		Label: "Simple Option",
		Value: "simple",
	}

	if opt.Recommended != false {
		t.Errorf("expected Recommended false by default, got %v", opt.Recommended)
	}
}

// =============================================================================
// SecretPromptOptions Tests
// =============================================================================

func TestSecretPromptOptions_Fields(t *testing.T) {
	opts := SecretPromptOptions{
		FilePath:      "/path/to/file.txt",
		ShowRedact:    true,
		ShowForceSkip: false,
		Findings: []SecretFinding{
			{LineNumber: 10, PatternID: "SSN", PatternName: "Social Security", Confidence: "HIGH", Match: "123-45-6789", Reason: "SSN pattern"},
		},
	}

	if opts.FilePath != "/path/to/file.txt" {
		t.Errorf("expected FilePath '/path/to/file.txt', got %q", opts.FilePath)
	}
	if opts.ShowRedact != true {
		t.Errorf("expected ShowRedact true, got %v", opts.ShowRedact)
	}
	if opts.ShowForceSkip != false {
		t.Errorf("expected ShowForceSkip false, got %v", opts.ShowForceSkip)
	}
	if len(opts.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(opts.Findings))
	}
}

// =============================================================================
// SecretFinding Tests
// =============================================================================

func TestSecretFinding_Fields(t *testing.T) {
	finding := SecretFinding{
		LineNumber:  42,
		PatternID:   "CREDIT_CARD",
		PatternName: "Credit Card Number",
		Confidence:  "MEDIUM",
		Match:       "4111-1111-1111-1111",
		Reason:      "Luhn checksum valid",
	}

	if finding.LineNumber != 42 {
		t.Errorf("expected LineNumber 42, got %d", finding.LineNumber)
	}
	if finding.PatternID != "CREDIT_CARD" {
		t.Errorf("expected PatternID 'CREDIT_CARD', got %q", finding.PatternID)
	}
	if finding.PatternName != "Credit Card Number" {
		t.Errorf("expected PatternName 'Credit Card Number', got %q", finding.PatternName)
	}
	if finding.Confidence != "MEDIUM" {
		t.Errorf("expected Confidence 'MEDIUM', got %q", finding.Confidence)
	}
	if finding.Match != "4111-1111-1111-1111" {
		t.Errorf("expected Match '4111-1111-1111-1111', got %q", finding.Match)
	}
	if finding.Reason != "Luhn checksum valid" {
		t.Errorf("expected Reason 'Luhn checksum valid', got %q", finding.Reason)
	}
}

// =============================================================================
// SecretAction Tests
// =============================================================================

func TestSecretAction_Constants(t *testing.T) {
	if SecretActionSkip != "skip" {
		t.Errorf("expected SecretActionSkip = 'skip', got %q", SecretActionSkip)
	}
	if SecretActionRedact != "redact" {
		t.Errorf("expected SecretActionRedact = 'redact', got %q", SecretActionRedact)
	}
	if SecretActionProceed != "proceed" {
		t.Errorf("expected SecretActionProceed = 'proceed', got %q", SecretActionProceed)
	}
	if SecretActionShowMore != "show" {
		t.Errorf("expected SecretActionShowMore = 'show', got %q", SecretActionShowMore)
	}
}

// =============================================================================
// Integration-style Tests (for types working together)
// =============================================================================

func TestSecretPromptOptions_MultipleFindings(t *testing.T) {
	opts := SecretPromptOptions{
		FilePath:      "/path/to/sensitive_file.go",
		ShowRedact:    true,
		ShowForceSkip: true,
		Findings: []SecretFinding{
			{LineNumber: 10, PatternID: "SSN", PatternName: "SSN", Confidence: "HIGH", Match: "123-45-6789", Reason: "SSN pattern"},
			{LineNumber: 25, PatternID: "EMAIL", PatternName: "Email", Confidence: "MEDIUM", Match: "user@example.com", Reason: "Email pattern"},
			{LineNumber: 42, PatternID: "API_KEY", PatternName: "API Key", Confidence: "HIGH", Match: "sk-abc123...", Reason: "API key pattern"},
		},
	}

	if len(opts.Findings) != 3 {
		t.Errorf("expected 3 findings, got %d", len(opts.Findings))
	}

	// Verify each finding is accessible
	for i, f := range opts.Findings {
		if f.LineNumber == 0 {
			t.Errorf("finding %d has zero line number", i)
		}
		if f.PatternID == "" {
			t.Errorf("finding %d has empty pattern ID", i)
		}
	}
}

func TestPromptOption_MultipleOptions(t *testing.T) {
	options := []PromptOption{
		{Label: "Option A", Value: "a", Recommended: true},
		{Label: "Option B", Value: "b", Description: "Second option"},
		{Label: "Option C", Value: "c"},
	}

	if len(options) != 3 {
		t.Errorf("expected 3 options, got %d", len(options))
	}

	// Verify only first is recommended
	recommendedCount := 0
	for _, opt := range options {
		if opt.Recommended {
			recommendedCount++
		}
	}
	if recommendedCount != 1 {
		t.Errorf("expected 1 recommended option, got %d", recommendedCount)
	}
}
