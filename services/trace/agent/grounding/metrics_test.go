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
	"time"
)

func TestRecordCheck(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordCheck(ctx, "citation_checker", 0, 10*time.Millisecond)
	RecordCheck(ctx, "language_checker", 2, 50*time.Millisecond)
	RecordCheck(ctx, "grounding_checker", 1, 100*time.Millisecond)
}

func TestRecordViolation(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordViolation(ctx, Violation{
		Type:     ViolationCitationInvalid,
		Severity: SeverityWarning,
		Code:     "CITATION_NOT_IN_CONTEXT",
	})
	RecordViolation(ctx, Violation{
		Type:     ViolationWrongLanguage,
		Severity: SeverityCritical,
		Code:     "WRONG_LANGUAGE_PYTHON",
	})
	RecordViolation(ctx, Violation{
		Type:     ViolationUngrounded,
		Severity: SeverityCritical,
		Code:     "FILE_NOT_IN_CONTEXT",
	})
}

func TestRecordReprompt(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordReprompt(ctx, 1, "critical_violation")
	RecordReprompt(ctx, 2, "wrong_language")
	RecordReprompt(ctx, 3, "ungrounded_claim")
}

func TestRecordRejection(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordRejection(ctx, "critical_violation")
	RecordRejection(ctx, "max_violations_exceeded")
	RecordRejection(ctx, "low_confidence")
}

func TestRecordWarningFootnote(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordWarningFootnote(ctx)
	RecordWarningFootnote(ctx)
}

func TestRecordConsensusResult(t *testing.T) {
	ctx := context.Background()

	// Should not panic with nil
	RecordConsensusResult(ctx, nil)

	// Should not panic with valid result
	RecordConsensusResult(ctx, &ConsensusResult{
		TotalSamples:  3,
		ConsensusRate: 0.85,
		ConsistentClaims: []NormalizedClaim{
			{Original: "test claim"},
		},
		InconsistentClaims: []NormalizedClaim{
			{Original: "inconsistent claim"},
		},
	})
}

func TestRecordCircuitBreakerState(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordCircuitBreakerState(ctx, CircuitBreakerClosed)
	RecordCircuitBreakerState(ctx, CircuitBreakerHalfOpen)
	RecordCircuitBreakerState(ctx, CircuitBreakerOpen)
}

func TestRecordRetriesExhausted(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordRetriesExhausted(ctx)
}

func TestRecordConfidence(t *testing.T) {
	ctx := context.Background()

	// Should not panic
	RecordConfidence(ctx, 0.0)
	RecordConfidence(ctx, 0.5)
	RecordConfidence(ctx, 1.0)
}

func TestRecordValidation(t *testing.T) {
	ctx := context.Background()

	// Should not panic - grounded
	RecordValidation(ctx, ValidationStats{
		ChecksRun:       5,
		ViolationsFound: 0,
		CriticalCount:   0,
		WarningCount:    0,
		Grounded:        true,
		Confidence:      0.95,
		Duration:        150 * time.Millisecond,
	})

	// Should not panic - ungrounded
	RecordValidation(ctx, ValidationStats{
		ChecksRun:       5,
		ViolationsFound: 3,
		CriticalCount:   1,
		WarningCount:    2,
		Grounded:        false,
		Confidence:      0.3,
		Duration:        200 * time.Millisecond,
	})
}

func TestStartGroundingSpan(t *testing.T) {
	ctx := context.Background()

	newCtx, span := StartGroundingSpan(ctx, "grounding.Validate", 1000)
	defer span.End()

	if newCtx == nil {
		t.Error("context should not be nil")
	}
	if span == nil {
		t.Error("span should not be nil")
	}
}

func TestSetGroundingSpanResult(t *testing.T) {
	ctx := context.Background()

	_, span := StartGroundingSpan(ctx, "test_operation", 500)
	defer span.End()

	// Should not panic with nil
	SetGroundingSpanResult(span, nil)

	// Should not panic with valid result
	SetGroundingSpanResult(span, &Result{
		Grounded:      true,
		Confidence:    0.9,
		ChecksRun:     5,
		Violations:    []Violation{{Code: "TEST"}},
		CriticalCount: 0,
		WarningCount:  1,
		CheckDuration: 100 * time.Millisecond,
	})
}

func TestAddCheckerEvent(t *testing.T) {
	ctx := context.Background()

	_, span := StartGroundingSpan(ctx, "test_operation", 500)
	defer span.End()

	// Should not panic
	AddCheckerEvent(span, "citation_checker", 0, 10*time.Millisecond)
	AddCheckerEvent(span, "language_checker", 2, 50*time.Millisecond)
}

func TestAddViolationEvent(t *testing.T) {
	ctx := context.Background()

	_, span := StartGroundingSpan(ctx, "test_operation", 500)
	defer span.End()

	// Should not panic
	AddViolationEvent(span, Violation{
		Type:     ViolationCitationInvalid,
		Severity: SeverityWarning,
		Code:     "TEST_VIOLATION",
		Message:  "This is a test violation message",
	})
}

func TestAddConsensusEvent(t *testing.T) {
	ctx := context.Background()

	_, span := StartGroundingSpan(ctx, "test_operation", 500)
	defer span.End()

	// Should not panic with nil
	AddConsensusEvent(span, nil)

	// Should not panic with valid result
	AddConsensusEvent(span, &ConsensusResult{
		TotalSamples:  3,
		ConsensusRate: 0.8,
		ConsistentClaims: []NormalizedClaim{
			{Original: "test"},
		},
		InconsistentClaims: []NormalizedClaim{
			{Original: "inconsistent"},
		},
	})
}

func TestTruncateForAttribute(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateForAttribute(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForAttribute(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestInitMetrics_Multiple(t *testing.T) {
	// Should be safe to call multiple times
	err1 := initMetrics()
	err2 := initMetrics()
	err3 := initMetrics()

	// All should return the same result (due to sync.Once)
	if err1 != err2 || err2 != err3 {
		t.Error("initMetrics should return same error on multiple calls")
	}
}
