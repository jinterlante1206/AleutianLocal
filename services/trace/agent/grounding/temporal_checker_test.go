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

func TestTemporalChecker_Name(t *testing.T) {
	checker := NewTemporalChecker(nil)
	if checker.Name() != "temporal_checker" {
		t.Errorf("expected 'temporal_checker', got '%s'", checker.Name())
	}
}

func TestTemporalChecker_Disabled(t *testing.T) {
	config := &TemporalCheckerConfig{
		Enabled: false,
	}
	checker := NewTemporalChecker(config)

	input := &CheckInput{
		Response: "This was recently refactored in the last commit.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestTemporalChecker_NilInput(t *testing.T) {
	checker := NewTemporalChecker(nil)
	violations := checker.Check(context.Background(), nil)
	if violations != nil {
		t.Errorf("expected nil for nil input, got %v", violations)
	}
}

func TestTemporalChecker_EmptyResponse(t *testing.T) {
	checker := NewTemporalChecker(nil)
	input := &CheckInput{
		Response: "",
	}
	violations := checker.Check(context.Background(), input)
	if violations != nil {
		t.Errorf("expected nil for empty response, got %v", violations)
	}
}

func TestTemporalChecker_NoTemporalClaims(t *testing.T) {
	checker := NewTemporalChecker(nil)

	input := &CheckInput{
		Response: "The function Parse takes a context and returns a Result. It validates the input and processes the data.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
}

func TestTemporalChecker_RecencyClaimNoEvidence(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantCode string
	}{
		{
			name:     "recently added",
			response: "This feature was recently added to improve performance.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
		{
			name:     "just updated",
			response: "The configuration was just updated to support new options.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
		{
			name:     "newly introduced",
			response: "This is a newly introduced helper function.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
		{
			name:     "latest version",
			response: "The latest released feature includes caching.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
		{
			name:     "last commit",
			response: "In the last commit, error handling was improved.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
		{
			name:     "recently refactored",
			response: "The parser was recently refactored for clarity.",
			wantCode: "TEMPORAL_RECENCY_UNVERIFIABLE",
		},
	}

	checker := NewTemporalChecker(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tt.response,
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) == 0 {
				t.Errorf("expected at least 1 violation, got 0")
				return
			}

			found := false
			for _, v := range violations {
				if v.Code == tt.wantCode {
					found = true
					if v.Type != ViolationTemporalHallucination {
						t.Errorf("expected type %s, got %s", ViolationTemporalHallucination, v.Type)
					}
					if v.Severity != SeverityInfo {
						t.Errorf("expected severity %s, got %s", SeverityInfo, v.Severity)
					}
					break
				}
			}
			if !found {
				t.Errorf("expected code %s, got codes %v", tt.wantCode, violationCodes(violations))
			}
		})
	}
}

func TestTemporalChecker_HistoricalClaimNoEvidence(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantCode string
	}{
		{
			name:     "was originally",
			response: "This function was originally designed for internal use only.",
			wantCode: "TEMPORAL_HISTORICAL_UNVERIFIABLE",
		},
		{
			name:     "used to be",
			response: "The handler used to be synchronous before the async refactor.",
			wantCode: "TEMPORAL_HISTORICAL_UNVERIFIABLE",
		},
		{
			name:     "previously",
			response: "Previously, this code lived in a different package.",
			wantCode: "TEMPORAL_HISTORICAL_UNVERIFIABLE",
		},
		{
			name:     "in earlier versions",
			response: "In earlier versions, this feature wasn't available.",
			wantCode: "TEMPORAL_HISTORICAL_UNVERIFIABLE",
		},
		{
			name:     "originally implemented",
			response: "This was originally implemented for the MVP.",
			wantCode: "TEMPORAL_HISTORICAL_UNVERIFIABLE",
		},
	}

	checker := NewTemporalChecker(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tt.response,
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) == 0 {
				t.Errorf("expected at least 1 violation, got 0")
				return
			}

			found := false
			for _, v := range violations {
				if v.Code == tt.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected code %s, got codes %v", tt.wantCode, violationCodes(violations))
			}
		})
	}
}

func TestTemporalChecker_VersionClaimNoEvidence(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantCode string
	}{
		{
			name:     "added in version",
			response: "This feature was added in version 2.0.",
			wantCode: "TEMPORAL_VERSION_UNVERIFIABLE",
		},
		{
			name:     "introduced in v1",
			response: "The API was introduced in v1.5.",
			wantCode: "TEMPORAL_VERSION_UNVERIFIABLE",
		},
		{
			name:     "deprecated in",
			response: "This method was deprecated in 3.0 in favor of the new API.",
			wantCode: "TEMPORAL_VERSION_UNVERIFIABLE",
		},
		{
			name:     "since version",
			response: "Since version 1.2, this has been the default behavior.",
			wantCode: "TEMPORAL_VERSION_UNVERIFIABLE",
		},
		{
			name:     "as of v2",
			response: "As of v2.1, the old endpoint is no longer supported.",
			wantCode: "TEMPORAL_VERSION_UNVERIFIABLE",
		},
	}

	checker := NewTemporalChecker(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tt.response,
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) == 0 {
				t.Errorf("expected at least 1 violation, got 0")
				return
			}

			found := false
			for _, v := range violations {
				if v.Code == tt.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected code %s, got codes %v", tt.wantCode, violationCodes(violations))
			}
		})
	}
}

func TestTemporalChecker_ReasonClaimNoEvidence(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantCode string
	}{
		{
			name:     "was changed because",
			response: "The function was changed because of performance concerns.",
			wantCode: "TEMPORAL_REASON_UNVERIFIABLE",
		},
		{
			name:     "refactored to improve",
			response: "This module was refactored to improve maintainability.",
			wantCode: "TEMPORAL_REASON_UNVERIFIABLE",
		},
		{
			name:     "updated due to",
			response: "The API was updated due to security vulnerabilities.",
			wantCode: "TEMPORAL_REASON_UNVERIFIABLE",
		},
		{
			name:     "changed to fix",
			response: "The handler was changed to fix a race condition.",
			wantCode: "TEMPORAL_REASON_UNVERIFIABLE",
		},
	}

	checker := NewTemporalChecker(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tt.response,
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) == 0 {
				t.Errorf("expected at least 1 violation, got 0")
				return
			}

			found := false
			for _, v := range violations {
				if v.Code == tt.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected code %s, got codes %v", tt.wantCode, violationCodes(violations))
			}
		})
	}
}

func TestTemporalChecker_WithGitEvidence(t *testing.T) {
	checker := NewTemporalChecker(nil)

	tests := []struct {
		name       string
		toolOutput string
	}{
		{
			name:       "git log with commit hash",
			toolOutput: "commit a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0\nAuthor: Dev <dev@example.com>\nDate: Mon Jan 1 12:00:00 2025 -0800\n\n    Fix bug",
		},
		{
			name:       "git log command output",
			toolOutput: "$ git log -n 5\nShowing recent commits...",
		},
		{
			name:       "git show command",
			toolOutput: "$ git show HEAD\ncommit hash...",
		},
		{
			name:       "git diff command",
			toolOutput: "$ git diff main..feature\n+new line\n-old line",
		},
		{
			name:       "git blame",
			toolOutput: "$ git blame main.go\na1b2c3d4 (Author 2025-01-01 10:00:00 +0000 1) package main",
		},
		{
			name:       "ISO timestamp",
			toolOutput: "Last modified: 2025-01-15T10:30:00+00:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: "This was recently added to the codebase.",
				ToolResults: []ToolResult{
					{
						InvocationID: "test",
						Output:       tt.toolOutput,
					},
				},
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) != 0 {
				t.Errorf("expected 0 violations with git evidence, got %d", len(violations))
			}
		})
	}
}

func TestTemporalChecker_SkipCodeBlocks(t *testing.T) {
	checker := NewTemporalChecker(&TemporalCheckerConfig{
		Enabled:            true,
		CheckRecencyClaims: true,
		SkipCodeBlocks:     true,
	})

	input := &CheckInput{
		Response: "Here's an example:\n```go\n// This was recently added\nfunc New() *Service {\n    return &Service{}\n}\n```\nThe function creates a new service.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations with temporal words only in code blocks, got %d", len(violations))
	}
}

func TestTemporalChecker_DoNotSkipCodeBlocks(t *testing.T) {
	checker := NewTemporalChecker(&TemporalCheckerConfig{
		Enabled:            true,
		CheckRecencyClaims: true,
		SkipCodeBlocks:     false,
	})

	input := &CheckInput{
		Response: "The code was recently modified to improve performance.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation, got 0")
	}
}

func TestTemporalChecker_StrictMode(t *testing.T) {
	checker := NewTemporalChecker(&TemporalCheckerConfig{
		Enabled:            true,
		StrictMode:         true,
		CheckRecencyClaims: true,
	})

	input := &CheckInput{
		Response: "This was recently added.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation, got 0")
		return
	}

	if violations[0].Severity != SeverityWarning {
		t.Errorf("expected SeverityWarning in strict mode, got %s", violations[0].Severity)
	}
}

func TestTemporalChecker_MaxClaimsLimit(t *testing.T) {
	checker := NewTemporalChecker(&TemporalCheckerConfig{
		Enabled:            true,
		CheckRecencyClaims: true,
		MaxClaimsToCheck:   2,
	})

	input := &CheckInput{
		Response: "First was recently added. Second was recently modified. Third was recently changed. Fourth was recently updated.",
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations, got %d", len(violations))
	}
}

func TestTemporalChecker_QuickCheckOptimization(t *testing.T) {
	checker := NewTemporalChecker(nil)

	// Long response without temporal keywords in first 1000 chars
	// should be skipped by quick check
	longPrefix := make([]byte, 1001)
	for i := range longPrefix {
		longPrefix[i] = 'x'
	}
	response := string(longPrefix) + " This was recently added."

	input := &CheckInput{
		Response: response,
	}

	// Should find no violations because quick check looks at first 1000 chars
	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations due to quick check optimization, got %d", len(violations))
	}
}

func TestTemporalChecker_SelectiveCategories(t *testing.T) {
	t.Run("only recency", func(t *testing.T) {
		checker := NewTemporalChecker(&TemporalCheckerConfig{
			Enabled:               true,
			CheckRecencyClaims:    true,
			CheckHistoricalClaims: false,
			CheckVersionClaims:    false,
			CheckReasonClaims:     false,
		})

		input := &CheckInput{
			Response: "This was originally designed for X. It was recently updated. Since version 2.0 it has Y.",
		}

		violations := checker.Check(context.Background(), input)
		for _, v := range violations {
			if v.Code != "TEMPORAL_RECENCY_UNVERIFIABLE" {
				t.Errorf("expected only recency violations, got %s", v.Code)
			}
		}
	})

	t.Run("only historical", func(t *testing.T) {
		checker := NewTemporalChecker(&TemporalCheckerConfig{
			Enabled:               true,
			CheckRecencyClaims:    false,
			CheckHistoricalClaims: true,
			CheckVersionClaims:    false,
			CheckReasonClaims:     false,
		})

		input := &CheckInput{
			Response: "This was originally designed for X. It was recently updated. Since version 2.0 it has Y.",
		}

		violations := checker.Check(context.Background(), input)
		for _, v := range violations {
			if v.Code != "TEMPORAL_HISTORICAL_UNVERIFIABLE" {
				t.Errorf("expected only historical violations, got %s", v.Code)
			}
		}
	})
}

func TestTemporalChecker_ContextCancellation(t *testing.T) {
	checker := NewTemporalChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: "This was recently added. It was recently updated. It was recently changed.",
	}

	// Should return early due to context cancellation
	violations := checker.Check(ctx, input)
	// May have 0 or partial violations depending on timing
	if len(violations) > 3 {
		t.Errorf("expected at most 3 violations, got %d", len(violations))
	}
}

func TestTemporalClaimCategory_String(t *testing.T) {
	tests := []struct {
		category TemporalClaimCategory
		want     string
	}{
		{ClaimCategoryRecency, "recency"},
		{ClaimCategoryHistorical, "historical"},
		{ClaimCategoryVersion, "version"},
		{ClaimCategoryReason, "reason"},
		{TemporalClaimCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.category.String(); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestTemporalChecker_Priority(t *testing.T) {
	// Verify priority mapping exists and is correct
	priority := ViolationTypeToPriority(ViolationTemporalHallucination)
	if priority != PriorityTemporalHallucination {
		t.Errorf("expected priority %d, got %d", PriorityTemporalHallucination, priority)
	}
	if priority != 4 {
		t.Errorf("expected priority 4 (low), got %d", priority)
	}
}

func TestStripCodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "fenced code block",
			input: "Before ```go\ncode here\n``` After",
			want:  "Before  After",
		},
		{
			name:  "inline code",
			input: "Use `recently` for timing",
			want:  "Use  for timing",
		},
		{
			name:  "multiple blocks",
			input: "A `b` C ```d``` E",
			want:  "A  C  E",
		},
		{
			name:  "no code blocks",
			input: "Plain text without code",
			want:  "Plain text without code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripCodeBlocks(tt.input)
			if got != tt.want {
				t.Errorf("StripCodeBlocks(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTemporalChecker_MultipleClaims(t *testing.T) {
	checker := NewTemporalChecker(nil)

	input := &CheckInput{
		Response: `The parser was recently refactored. It was originally designed for simple cases.
		Since version 2.0, it supports complex expressions. The change was made because performance was poor.`,
	}

	violations := checker.Check(context.Background(), input)

	// Should find multiple categories
	categories := make(map[string]bool)
	for _, v := range violations {
		categories[v.Code] = true
	}

	expectedCategories := []string{
		"TEMPORAL_RECENCY_UNVERIFIABLE",
		"TEMPORAL_HISTORICAL_UNVERIFIABLE",
		"TEMPORAL_VERSION_UNVERIFIABLE",
		"TEMPORAL_REASON_UNVERIFIABLE",
	}

	for _, expected := range expectedCategories {
		if !categories[expected] {
			t.Errorf("expected category %s, not found in violations", expected)
		}
	}
}

// Helper function to extract codes from violations
func violationCodes(violations []Violation) []string {
	codes := make([]string, len(violations))
	for i, v := range violations {
		codes[i] = v.Code
	}
	return codes
}
