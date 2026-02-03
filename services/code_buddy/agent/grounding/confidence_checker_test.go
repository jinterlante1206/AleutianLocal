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
	"testing"
)

func TestConfidenceChecker_Name(t *testing.T) {
	checker := NewConfidenceChecker(nil)
	if checker.Name() != "confidence_checker" {
		t.Errorf("expected 'confidence_checker', got '%s'", checker.Name())
	}
}

func TestConfidenceChecker_Disabled(t *testing.T) {
	config := &ConfidenceCheckerConfig{
		Enabled: false,
	}
	checker := NewConfidenceChecker(config)

	input := &CheckInput{
		Response: "All inputs are always validated without exception.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestConfidenceChecker_NilInput(t *testing.T) {
	checker := NewConfidenceChecker(nil)
	violations := checker.Check(context.Background(), nil)
	if violations != nil {
		t.Errorf("expected nil for nil input, got %v", violations)
	}
}

func TestConfidenceChecker_EmptyResponse(t *testing.T) {
	checker := NewConfidenceChecker(nil)
	input := &CheckInput{
		Response: "",
	}
	violations := checker.Check(context.Background(), input)
	if violations != nil {
		t.Errorf("expected nil for empty response, got %v", violations)
	}
}

func TestConfidenceChecker_NoAbsoluteLanguage(t *testing.T) {
	checker := NewConfidenceChecker(nil)
	input := &CheckInput{
		Response: "The function processes inputs and returns results. It handles errors gracefully.",
	}
	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for response without absolutes, got %d", len(violations))
	}
}

func TestConfidenceChecker_AbsoluteClaimNoEvidence(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "All inputs are validated before processing.",
		ToolResults: []ToolResult{}, // No tool results
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for absolute claim without evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication && v.Code == "CONFIDENCE_ABSENT" {
			found = true
			if v.Severity != SeverityCritical {
				t.Errorf("expected severity %s, got %s", SeverityCritical, v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected CONFIDENCE_ABSENT violation, got %v", violations)
	}
}

func TestConfidenceChecker_AbsoluteClaimWithStrongEvidence(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response: "All inputs are validated before processing.",
		ToolResults: []ToolResult{
			{
				InvocationID: "search-1",
				Output:       "Found input validation in main.go, handler.go, and service.go. All input parameters are checked.",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when evidence supports the claim, got %d: %v", len(violations), violations)
	}
}

func TestConfidenceChecker_AbsoluteClaimPartialEvidence(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response: "All inputs are validated before processing.",
		ToolResults: []ToolResult{
			{
				InvocationID: "search-1",
				Output:       "Found input validation... showing first 10 results... and 45 more",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for absolute claim with partial evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication && v.Code == "CONFIDENCE_PARTIAL" {
			found = true
			if v.Severity != SeverityHigh {
				t.Errorf("expected severity %s, got %s", SeverityHigh, v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected CONFIDENCE_PARTIAL violation, got %v", violations)
	}
}

func TestConfidenceChecker_NegativeClaimNoEvidence(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "There is no error logging in this codebase.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for negative claim without evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confidence fabrication violation for 'there is no', got %v", violations)
	}
}

func TestConfidenceChecker_ExhaustiveClaimNoEvidence(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "The only way to connect to the database is through the Pool function.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for exhaustive claim without evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confidence fabrication violation for 'the only', got %v", violations)
	}
}

func TestConfidenceChecker_HedgedAbsoluteSkipped(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "It appears that all inputs are validated before processing.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for hedged absolute, got %d: %v", len(violations), violations)
	}
}

func TestConfidenceChecker_HedgedScopeSkipped(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "Based on what I found, all inputs are validated before processing.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scope-hedged absolute, got %d: %v", len(violations), violations)
	}
}

func TestConfidenceChecker_HedgedMightSkipped(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "There might never be a case where this fails.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for 'might' hedged absolute, got %d: %v", len(violations), violations)
	}
}

func TestConfidenceChecker_TautologySkipped(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "All .go files have .go extension.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for tautology, got %d: %v", len(violations), violations)
	}
}

func TestConfidenceChecker_CodeBlockSkipped(t *testing.T) {
	config := &ConfidenceCheckerConfig{
		Enabled:        true,
		SkipCodeBlocks: true,
	}
	checker := NewConfidenceChecker(config)

	input := &CheckInput{
		Response: "Here is the code:\n```\n// This function always returns true\nfunc alwaysTrue() bool { return true }\n```\nThe code is simple.",
		ToolResults: []ToolResult{
			{Output: "function definition found"},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should not flag "always" inside code block
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication &&
			(containsIgnoreCase(v.Evidence, "always returns true") || containsIgnoreCase(v.Evidence, "alwaysTrue")) {
			t.Errorf("should not flag absolute language inside code blocks, got: %v", v)
		}
	}
}

func TestConfidenceChecker_InlineCodeSkipped(t *testing.T) {
	config := &ConfidenceCheckerConfig{
		Enabled:        true,
		SkipCodeBlocks: true,
	}
	checker := NewConfidenceChecker(config)

	input := &CheckInput{
		Response:    "The `alwaysRetry` function handles retries.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	// "always" is inside inline code, should be stripped
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication && containsIgnoreCase(v.Evidence, "alwaysRetry") {
			t.Errorf("should not flag absolute language inside inline code, got: %v", v)
		}
	}
}

func TestConfidenceChecker_StrongAbsolute(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "This is definitely the correct approach and guaranteed to work.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for strong absolute without evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confidence fabrication violation for 'definitely/guaranteed', got %v", violations)
	}
}

func TestConfidenceChecker_UniversalClaimNever(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "The application never crashes under load.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for 'never' claim without evidence, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Type == ViolationConfidenceFabrication {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confidence fabrication violation for 'never', got %v", violations)
	}
}

func TestConfidenceChecker_MultipleAbsoluteClaims(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	input := &CheckInput{
		Response:    "All requests are logged. Every response is cached. No errors are ignored.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	// Should catch multiple absolute claims
	if len(violations) < 2 {
		t.Errorf("expected at least 2 violations for multiple absolute claims, got %d", len(violations))
	}
}

func TestConfidenceChecker_MaxClaimsLimit(t *testing.T) {
	config := &ConfidenceCheckerConfig{
		Enabled:          true,
		MaxClaimsToCheck: 2,
	}
	checker := NewConfidenceChecker(config)

	input := &CheckInput{
		Response:    "All A. Every B. None C. Always D. Never E.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	// Should be limited to checking only 2 claims
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations with MaxClaimsToCheck=2, got %d", len(violations))
	}
}

func TestConfidenceChecker_ContextCancellation(t *testing.T) {
	checker := NewConfidenceChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response:    "All A. Every B. None C.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(ctx, input)
	// Should return early due to cancellation
	if len(violations) > 3 {
		t.Errorf("expected limited violations due to cancellation, got %d", len(violations))
	}
}

func TestConfidenceChecker_Priority(t *testing.T) {
	// Verify priority mapping exists and is correct
	priority := ViolationTypeToPriority(ViolationConfidenceFabrication)
	if priority != PriorityConfidenceFabrication {
		t.Errorf("expected priority %d, got %d", PriorityConfidenceFabrication, priority)
	}
	if priority != 3 {
		t.Errorf("expected priority 3 (medium), got %d", priority)
	}
}

func TestConfidenceChecker_CustomSeverity(t *testing.T) {
	config := &ConfidenceCheckerConfig{
		Enabled:                 true,
		AbsentEvidenceSeverity:  SeverityHigh, // Custom: not critical
		PartialEvidenceSeverity: SeverityWarning,
	}
	checker := NewConfidenceChecker(config)

	input := &CheckInput{
		Response:    "All inputs are validated.",
		ToolResults: []ToolResult{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation, got 0")
		return
	}

	for _, v := range violations {
		if v.Code == "CONFIDENCE_ABSENT" && v.Severity != SeverityHigh {
			t.Errorf("expected custom severity %s, got %s", SeverityHigh, v.Severity)
		}
	}
}

func TestEvidenceStrength_String(t *testing.T) {
	tests := []struct {
		strength EvidenceStrength
		want     string
	}{
		{EvidenceAbsent, "absent"},
		{EvidencePartial, "partial"},
		{EvidenceStrong, "strong"},
		{EvidenceStrength(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.strength.String()
			if got != tt.want {
				t.Errorf("EvidenceStrength(%d).String() = %q, want %q", tt.strength, got, tt.want)
			}
		})
	}
}

func TestExtractConfidenceKeywords(t *testing.T) {
	tests := []struct {
		sentence string
		minCount int // Minimum expected keywords
	}{
		{"All inputs are validated before processing.", 2}, // inputs, validated, processing
		{"The function always returns true.", 2},           // function, returns, true
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.sentence, func(t *testing.T) {
			keywords := extractConfidenceKeywords(tt.sentence)
			if len(keywords) < tt.minCount {
				t.Errorf("extractConfidenceKeywords(%q) returned %d keywords, want at least %d", tt.sentence, len(keywords), tt.minCount)
			}
		})
	}
}

func TestHasKeywordOverlap(t *testing.T) {
	tests := []struct {
		text     string
		keywords []string
		want     bool
	}{
		{"Found validation in main.go", []string{"validation", "main"}, true},
		{"Found nothing relevant", []string{"input", "validate"}, false},
		{"The input was validated", []string{"input", "validated"}, true},
		{"", []string{"test"}, false},
		{"test text", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := hasKeywordOverlap(tt.text, tt.keywords)
			if got != tt.want {
				t.Errorf("hasKeywordOverlap(%q, %v) = %v, want %v", tt.text, tt.keywords, got, tt.want)
			}
		})
	}
}

func TestTruncateClaim(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"this is a longer string", 10, "this is..."},
		{"", 10, ""},
		{"  spaces  ", 20, "spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateClaim(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateClaim(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// Helper function for tests
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) > 0 &&
				(s[0:len(substr)] == substr ||
					containsIgnoreCase(s[1:], substr)))
}
