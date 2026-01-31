// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"context"
	"strings"
	"testing"
)

func TestAutoFix_NilContext(t *testing.T) {
	runner := NewLintRunner()

	_, err := runner.AutoFix(nil, "test.go") //nolint:staticcheck
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestAutoFix_EmptyFilePath(t *testing.T) {
	runner := NewLintRunner()

	ctx := context.Background()
	_, err := runner.AutoFix(ctx, "")
	if err == nil {
		t.Error("Expected error for empty file path")
	}
	if !strings.Contains(err.Error(), "filePath must not be empty") {
		t.Errorf("Expected 'filePath must not be empty' error, got: %v", err)
	}
}

func TestAutoFix_UnsupportedLanguage(t *testing.T) {
	runner := NewLintRunner()

	ctx := context.Background()
	_, err := runner.AutoFix(ctx, "test.unknown")
	if err == nil {
		t.Error("Expected error for unsupported language")
	}
}

func TestAutoFixWithLanguage_NoFixArgs(t *testing.T) {
	runner := NewLintRunner()

	// Create a config with no fix args
	runner.Configs().Register(&LinterConfig{
		Language:   "nofixlang",
		Command:    "somecommand",
		Args:       []string{"run"},
		Extensions: []string{".nofix"},
		FixArgs:    nil, // No fix args
	})

	ctx := context.Background()
	_, err := runner.AutoFixWithLanguage(ctx, "test.nofix", "nofixlang")
	if err == nil {
		t.Error("Expected error for linter without fix support")
	}
	if !strings.Contains(err.Error(), "does not support auto-fix") {
		t.Errorf("Expected 'does not support auto-fix' error, got: %v", err)
	}
}

func TestAutoFixContent_NilContext(t *testing.T) {
	runner := NewLintRunner()

	_, _, err := runner.AutoFixContent(nil, []byte("test"), "go") //nolint:staticcheck
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestAutoFixContent_EmptyContent(t *testing.T) {
	runner := NewLintRunner()
	runner.DetectAvailableLinters()

	ctx := context.Background()
	content, result, err := runner.AutoFixContent(ctx, []byte{}, "go")

	if err != nil {
		t.Fatalf("AutoFixContent: %v", err)
	}

	if len(content) != 0 {
		t.Error("Expected empty content back")
	}

	if result == nil {
		t.Error("Expected non-nil result")
	}

	if !result.Valid {
		t.Error("Empty content should be valid")
	}
}

func TestAutoFixContent_UnsupportedLanguage(t *testing.T) {
	runner := NewLintRunner()

	ctx := context.Background()
	_, _, err := runner.AutoFixContent(ctx, []byte("test"), "unknown")
	if err == nil {
		t.Error("Expected error for unsupported language")
	}
}

func TestAutoFixContent_LinterUnavailable(t *testing.T) {
	runner := NewLintRunner()
	// Don't call DetectAvailableLinters - linters are unavailable

	ctx := context.Background()
	content, result, err := runner.AutoFixContent(ctx, []byte("package main"), "go")

	if err != nil {
		t.Fatalf("AutoFixContent: %v", err)
	}

	// Should return original content when linter unavailable
	if string(content) != "package main" {
		t.Error("Expected original content back")
	}

	if result == nil {
		t.Error("Expected non-nil result")
	}

	if result.LinterAvailable {
		t.Error("Expected LinterAvailable to be false")
	}

	if !result.Valid {
		t.Error("Expected Valid to be true when linter unavailable")
	}
}

// =============================================================================
// FEEDBACK TESTS
// =============================================================================

func TestFormatFeedback_NilResult(t *testing.T) {
	feedback := FormatFeedback(nil)

	if feedback == nil {
		t.Fatal("Expected non-nil feedback")
	}

	if feedback.Rejected {
		t.Error("Nil result should not be rejected")
	}

	if feedback.Reason == "" {
		t.Error("Expected reason to be set")
	}
}

func TestFormatFeedback_LinterUnavailable(t *testing.T) {
	result := &LintResult{
		Valid:           true,
		LinterAvailable: false,
		Linter:          "golangci-lint",
	}

	feedback := FormatFeedback(result)

	if feedback.Rejected {
		t.Error("Should not be rejected when linter unavailable")
	}

	if !strings.Contains(feedback.Reason, "not available") {
		t.Errorf("Expected 'not available' in reason, got: %s", feedback.Reason)
	}

	if !strings.Contains(feedback.Action, "golangci-lint") {
		t.Errorf("Expected linter name in action, got: %s", feedback.Action)
	}
}

func TestFormatFeedback_Valid(t *testing.T) {
	result := &LintResult{
		Valid:           true,
		LinterAvailable: true,
		Errors:          []LintIssue{},
		Warnings:        []LintIssue{},
	}

	feedback := FormatFeedback(result)

	if feedback.Rejected {
		t.Error("Valid result should not be rejected")
	}

	if !strings.Contains(feedback.Reason, "No blocking issues") {
		t.Errorf("Expected 'No blocking issues' in reason, got: %s", feedback.Reason)
	}
}

func TestFormatFeedback_ValidWithWarnings(t *testing.T) {
	result := &LintResult{
		Valid:           true,
		LinterAvailable: true,
		Errors:          []LintIssue{},
		Warnings: []LintIssue{
			{Rule: "unused", Message: "unused variable"},
			{Rule: "ineffassign", Message: "ineffective assignment"},
		},
	}

	feedback := FormatFeedback(result)

	if feedback.Rejected {
		t.Error("Valid result should not be rejected")
	}

	if !strings.Contains(feedback.Action, "2 warnings") {
		t.Errorf("Expected warning count in action, got: %s", feedback.Action)
	}

	if len(feedback.Issues) != 2 {
		t.Errorf("Expected 2 issues, got %d", len(feedback.Issues))
	}
}

func TestFormatFeedback_Rejected(t *testing.T) {
	result := &LintResult{
		Valid:           false,
		LinterAvailable: true,
		Errors: []LintIssue{
			{Rule: "errcheck", Message: "error not checked", Line: 42},
		},
		Warnings: []LintIssue{},
	}

	feedback := FormatFeedback(result)

	if !feedback.Rejected {
		t.Error("Invalid result should be rejected")
	}

	if !strings.Contains(feedback.Reason, "1 blocking errors") {
		t.Errorf("Expected error count in reason, got: %s", feedback.Reason)
	}

	if !strings.Contains(feedback.Action, "regenerate") {
		t.Errorf("Expected 'regenerate' in action, got: %s", feedback.Action)
	}

	if len(feedback.Issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(feedback.Issues))
	}

	if feedback.Issues[0].Rule != "errcheck" {
		t.Errorf("Expected rule 'errcheck', got %s", feedback.Issues[0].Rule)
	}
}

func TestFormatFeedback_WithAutoFix(t *testing.T) {
	result := &LintResult{
		Valid:           false,
		LinterAvailable: true,
		Errors: []LintIssue{
			{
				Rule:       "errcheck",
				Message:    "error not checked",
				Line:       42,
				CanAutoFix: true,
				Suggestion: "Add error check",
			},
		},
	}

	feedback := FormatFeedback(result)

	if feedback.AutoFixable != 1 {
		t.Errorf("Expected 1 auto-fixable, got %d", feedback.AutoFixable)
	}

	if feedback.Issues[0].Fix != "Add error check" {
		t.Errorf("Expected fix suggestion, got: %s", feedback.Issues[0].Fix)
	}
}

func TestFormatFeedback_WithReplacement(t *testing.T) {
	result := &LintResult{
		Valid:           false,
		LinterAvailable: true,
		Errors: []LintIssue{
			{
				Rule:        "errcheck",
				Message:     "error not checked",
				Line:        42,
				CanAutoFix:  false,
				Replacement: "newCode()",
			},
		},
	}

	feedback := FormatFeedback(result)

	if !strings.Contains(feedback.Issues[0].Fix, "newCode()") {
		t.Errorf("Expected replacement in fix, got: %s", feedback.Issues[0].Fix)
	}
}

func TestFormatFeedback_WarningsLimitedToFive(t *testing.T) {
	warnings := make([]LintIssue, 10)
	for i := 0; i < 10; i++ {
		warnings[i] = LintIssue{Rule: "unused", Message: "warning"}
	}

	result := &LintResult{
		Valid:           true,
		LinterAvailable: true,
		Errors:          []LintIssue{},
		Warnings:        warnings,
	}

	feedback := FormatFeedback(result)

	// Should only include first 5 warnings
	if len(feedback.Issues) != 5 {
		t.Errorf("Expected 5 issues (limited warnings), got %d", len(feedback.Issues))
	}
}

func TestLintFeedback_String_Nil(t *testing.T) {
	var feedback *LintFeedback
	if feedback.String() != "" {
		t.Error("Nil feedback should return empty string")
	}
}

func TestLintFeedback_String_Rejected(t *testing.T) {
	feedback := &LintFeedback{
		Rejected: true,
		Reason:   "Found 2 blocking errors",
		Issues: []FeedbackIssue{
			{Rule: "errcheck", Line: 42, Message: "error not checked", Fix: "Add error check"},
			{Rule: "typecheck", Line: 50, Message: "type mismatch"},
		},
		AutoFixable: 1,
		Action:      "Please fix the errors",
	}

	s := feedback.String()

	if !strings.Contains(s, "REJECTED") {
		t.Error("Expected REJECTED in output")
	}
	if !strings.Contains(s, "Found 2 blocking errors") {
		t.Error("Expected reason in output")
	}
	if !strings.Contains(s, "errcheck") {
		t.Error("Expected rule in output")
	}
	if !strings.Contains(s, "Line 42") {
		t.Error("Expected line number in output")
	}
	if !strings.Contains(s, "Add error check") {
		t.Error("Expected fix in output")
	}
	if !strings.Contains(s, "Auto-fixable: 1") {
		t.Error("Expected auto-fixable count in output")
	}
	if !strings.Contains(s, "Please fix the errors") {
		t.Error("Expected action in output")
	}
}

func TestLintFeedback_String_Passed(t *testing.T) {
	feedback := &LintFeedback{
		Rejected: false,
		Reason:   "No blocking issues found",
	}

	s := feedback.String()

	if !strings.Contains(s, "PASSED") {
		t.Error("Expected PASSED in output")
	}
}

// =============================================================================
// PARSER REGISTRY TESTS
// =============================================================================

func TestGetParser(t *testing.T) {
	tests := []struct {
		language string
		wantNil  bool
	}{
		{"go", false},
		{"python", false},
		{"typescript", false},
		{"javascript", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		parser := GetParser(tt.language)
		isNil := parser == nil
		if isNil != tt.wantNil {
			t.Errorf("GetParser(%q) nil = %v, want %v", tt.language, isNil, tt.wantNil)
		}
	}
}

func TestRegisterParser(t *testing.T) {
	// Register a custom parser
	customParser := func(data []byte) ([]LintIssue, error) {
		return []LintIssue{{Rule: "custom"}}, nil
	}

	RegisterParser("customlang", customParser)

	parser := GetParser("customlang")
	if parser == nil {
		t.Fatal("Expected custom parser to be registered")
	}

	issues, err := parser([]byte{})
	if err != nil {
		t.Fatalf("Parser error: %v", err)
	}

	if len(issues) != 1 || issues[0].Rule != "custom" {
		t.Error("Custom parser not working as expected")
	}

	// Clean up
	delete(parserRegistry, "customlang")
}
