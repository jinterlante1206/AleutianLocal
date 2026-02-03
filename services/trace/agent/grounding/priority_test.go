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
	"testing"
)

func TestViolationTypeToPriority(t *testing.T) {
	tests := []struct {
		name     string
		vType    ViolationType
		expected ViolationPriority
	}{
		{
			name:     "phantom file is highest priority",
			vType:    ViolationPhantomFile,
			expected: PriorityPhantomFile,
		},
		{
			name:     "structural claim is second priority",
			vType:    ViolationStructuralClaim,
			expected: PriorityStructuralClaim,
		},
		{
			name:     "language confusion is third priority",
			vType:    ViolationLanguageConfusion,
			expected: PriorityLanguageConfusion,
		},
		{
			name:     "generic pattern is fourth priority",
			vType:    ViolationGenericPattern,
			expected: PriorityGenericPattern,
		},
		{
			name:     "existing type maps to other",
			vType:    ViolationFileNotFound,
			expected: PriorityOther,
		},
		{
			name:     "wrong language maps to other",
			vType:    ViolationWrongLanguage,
			expected: PriorityOther,
		},
		{
			name:     "citation invalid maps to other",
			vType:    ViolationCitationInvalid,
			expected: PriorityOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ViolationTypeToPriority(tt.vType)
			if got != tt.expected {
				t.Errorf("ViolationTypeToPriority(%v) = %v, want %v", tt.vType, got, tt.expected)
			}
		})
	}
}

func TestViolation_Priority(t *testing.T) {
	v := Violation{
		Type:     ViolationPhantomFile,
		Severity: SeverityCritical,
		Message:  "test",
	}

	if got := v.Priority(); got != PriorityPhantomFile {
		t.Errorf("Violation.Priority() = %v, want %v", got, PriorityPhantomFile)
	}
}

func TestViolationPriority_Ordering(t *testing.T) {
	// Verify that the priority constants are ordered correctly
	// Lower value = higher priority
	if PriorityPhantomFile >= PriorityStructuralClaim {
		t.Error("PhantomFile should have higher priority (lower value) than StructuralClaim")
	}
	if PriorityStructuralClaim >= PriorityLanguageConfusion {
		t.Error("StructuralClaim should have higher priority than LanguageConfusion")
	}
	if PriorityLanguageConfusion >= PriorityGenericPattern {
		t.Error("LanguageConfusion should have higher priority than GenericPattern")
	}
	if PriorityGenericPattern >= PriorityOther {
		t.Error("GenericPattern should have higher priority than Other")
	}
}

func TestSortViolationsByPriority(t *testing.T) {
	t.Run("empty slice returns empty", func(t *testing.T) {
		result := SortViolationsByPriority(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}

		result = SortViolationsByPriority([]Violation{})
		if len(result) != 0 {
			t.Errorf("expected empty, got %d items", len(result))
		}
	})

	t.Run("sorts by priority ascending", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationGenericPattern, Message: "generic"},
			{Type: ViolationPhantomFile, Message: "phantom"},
			{Type: ViolationLanguageConfusion, Message: "language"},
			{Type: ViolationStructuralClaim, Message: "structural"},
		}

		result := SortViolationsByPriority(violations)

		// Verify order: phantom (1) -> structural (2) -> language (3) -> generic (4)
		expectedOrder := []ViolationType{
			ViolationPhantomFile,
			ViolationStructuralClaim,
			ViolationLanguageConfusion,
			ViolationGenericPattern,
		}

		for i, expected := range expectedOrder {
			if result[i].Type != expected {
				t.Errorf("position %d: got %v, want %v", i, result[i].Type, expected)
			}
		}
	})

	t.Run("stable sort preserves order within same priority", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationFileNotFound, Message: "first", LocationOffset: 10},
			{Type: ViolationFileNotFound, Message: "second", LocationOffset: 20},
			{Type: ViolationFileNotFound, Message: "third", LocationOffset: 30},
		}

		result := SortViolationsByPriority(violations)

		// All have same priority (Other), should preserve location offset order
		if result[0].Message != "first" || result[1].Message != "second" || result[2].Message != "third" {
			t.Errorf("stable sort not preserved: got %s, %s, %s",
				result[0].Message, result[1].Message, result[2].Message)
		}
	})

	t.Run("does not mutate input", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationGenericPattern, Message: "generic"},
			{Type: ViolationPhantomFile, Message: "phantom"},
		}

		original := violations[0].Type
		_ = SortViolationsByPriority(violations)

		if violations[0].Type != original {
			t.Error("input slice was mutated")
		}
	})
}

func TestDeduplicateCascadeViolations(t *testing.T) {
	t.Run("empty slice returns empty", func(t *testing.T) {
		result := DeduplicateCascadeViolations(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("removes lower priority with same evidence", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationPhantomFile, Evidence: "config/app.py", Message: "file doesn't exist"},
			{Type: ViolationLanguageConfusion, Evidence: "config/app.py", Message: "Flask in Go project"},
		}

		result := DeduplicateCascadeViolations(violations)

		if len(result) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(result))
		}

		if result[0].Type != ViolationPhantomFile {
			t.Errorf("expected PhantomFile to be kept, got %v", result[0].Type)
		}
	})

	t.Run("keeps both with different evidence", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationPhantomFile, Evidence: "config/app.py", Message: "file doesn't exist"},
			{Type: ViolationLanguageConfusion, Evidence: "Flask request handling", Message: "Flask in Go project"},
		}

		result := DeduplicateCascadeViolations(violations)

		if len(result) != 2 {
			t.Fatalf("expected 2 violations, got %d", len(result))
		}
	})

	t.Run("keeps violations without evidence", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationPhantomFile, Evidence: "", Message: "no evidence"},
			{Type: ViolationLanguageConfusion, Evidence: "", Message: "also no evidence"},
		}

		result := DeduplicateCascadeViolations(violations)

		if len(result) != 2 {
			t.Fatalf("expected 2 violations (no dedup without evidence), got %d", len(result))
		}
	})

	t.Run("complex cascade scenario", func(t *testing.T) {
		// Scenario: Multiple violations about the same phantom file
		violations := []Violation{
			{Type: ViolationGenericPattern, Evidence: "config/", Message: "generic config description"},
			{Type: ViolationStructuralClaim, Evidence: "config/", Message: "fabricated config directory"},
			{Type: ViolationPhantomFile, Evidence: "config/app.py", Message: "config/app.py doesn't exist"},
			{Type: ViolationLanguageConfusion, Evidence: "config/app.py", Message: "Flask config in Go project"},
		}

		result := DeduplicateCascadeViolations(violations)

		// Expected: PhantomFile (config/app.py), StructuralClaim (config/)
		// The LanguageConfusion for config/app.py should be deduped
		// The GenericPattern for config/ should be deduped by StructuralClaim

		if len(result) != 2 {
			t.Fatalf("expected 2 violations, got %d: %+v", len(result), result)
		}

		// Verify the kept ones
		types := make(map[ViolationType]bool)
		for _, v := range result {
			types[v.Type] = true
		}

		if !types[ViolationPhantomFile] {
			t.Error("expected PhantomFile to be kept")
		}
		if !types[ViolationStructuralClaim] {
			t.Error("expected StructuralClaim to be kept")
		}
	})
}

func TestCountViolationsByPriority(t *testing.T) {
	violations := []Violation{
		{Type: ViolationPhantomFile},
		{Type: ViolationPhantomFile},
		{Type: ViolationStructuralClaim},
		{Type: ViolationFileNotFound}, // Other priority
	}

	counts := CountViolationsByPriority(violations)

	if counts[PriorityPhantomFile] != 2 {
		t.Errorf("PhantomFile count = %d, want 2", counts[PriorityPhantomFile])
	}
	if counts[PriorityStructuralClaim] != 1 {
		t.Errorf("StructuralClaim count = %d, want 1", counts[PriorityStructuralClaim])
	}
	if counts[PriorityOther] != 1 {
		t.Errorf("Other count = %d, want 1", counts[PriorityOther])
	}
}

func TestHasHighPriorityViolations(t *testing.T) {
	t.Run("returns true for phantom file", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationPhantomFile},
		}
		if !HasHighPriorityViolations(violations) {
			t.Error("expected true for PhantomFile")
		}
	})

	t.Run("returns true for structural claim", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationStructuralClaim},
		}
		if !HasHighPriorityViolations(violations) {
			t.Error("expected true for StructuralClaim")
		}
	})

	t.Run("returns false for lower priority only", func(t *testing.T) {
		violations := []Violation{
			{Type: ViolationLanguageConfusion},
			{Type: ViolationGenericPattern},
			{Type: ViolationFileNotFound},
		}
		if HasHighPriorityViolations(violations) {
			t.Error("expected false for lower priority violations")
		}
	})

	t.Run("returns false for empty", func(t *testing.T) {
		if HasHighPriorityViolations(nil) {
			t.Error("expected false for nil")
		}
		if HasHighPriorityViolations([]Violation{}) {
			t.Error("expected false for empty slice")
		}
	})
}

func TestFilterViolationsByPriority(t *testing.T) {
	violations := []Violation{
		{Type: ViolationPhantomFile, Message: "p1"},
		{Type: ViolationStructuralClaim, Message: "p2"},
		{Type: ViolationLanguageConfusion, Message: "p3"},
		{Type: ViolationGenericPattern, Message: "p4"},
		{Type: ViolationFileNotFound, Message: "p5"},
	}

	t.Run("threshold 1 returns only phantom file", func(t *testing.T) {
		result := FilterViolationsByPriority(violations, PriorityPhantomFile)
		if len(result) != 1 || result[0].Type != ViolationPhantomFile {
			t.Errorf("expected 1 PhantomFile, got %d: %+v", len(result), result)
		}
	})

	t.Run("threshold 2 returns phantom and structural", func(t *testing.T) {
		result := FilterViolationsByPriority(violations, PriorityStructuralClaim)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
	})

	t.Run("threshold 5 returns all", func(t *testing.T) {
		result := FilterViolationsByPriority(violations, PriorityOther)
		if len(result) != 5 {
			t.Errorf("expected 5, got %d", len(result))
		}
	})
}

func TestNormalizeEvidence(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  path/to/file.go  ", "path/to/file.go"},
		{"\n\tconfig/app.py\n", "config/app.py"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		got := normalizeEvidence(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeEvidence(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
