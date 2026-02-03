// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package control

import (
	"strings"
	"sync"
	"testing"
)

func TestCategoryForType(t *testing.T) {
	tests := []struct {
		vType    ViolationType
		expected ViolationCategory
	}{
		{ViolationFileNotFound, CategoryResource},
		{ViolationPermissionDenied, CategoryResource},
		{ViolationNetworkError, CategoryResource},
		{ViolationResourceExhausted, CategoryResource},
		{ViolationIntentLoop, CategoryValidation},
		{ViolationMalformedTool, CategoryValidation},
		{ViolationInvalidCitation, CategoryValidation},
		{ViolationConstraint, CategoryValidation},
		{ViolationAmbiguous, CategorySemantic},
		{ViolationConflicting, CategorySemantic},
		{ViolationMissingContext, CategorySemantic},
		{ViolationScopeUnclear, CategorySemantic},
		{ViolationUnknown, CategoryUnknown},
		{"custom_type", CategoryUnknown},
	}

	for _, tc := range tests {
		t.Run(string(tc.vType), func(t *testing.T) {
			result := CategoryForType(tc.vType)
			if result != tc.expected {
				t.Errorf("CategoryForType(%s) = %s, want %s", tc.vType, result, tc.expected)
			}
		})
	}
}

func TestNewViolation(t *testing.T) {
	ctx := map[string]string{"path": "/config.yaml"}
	v := NewViolation(ViolationFileNotFound, "file not found", ctx)

	if v.Type != ViolationFileNotFound {
		t.Errorf("Type = %s, want %s", v.Type, ViolationFileNotFound)
	}
	if v.Category != CategoryResource {
		t.Errorf("Category = %s, want %s", v.Category, CategoryResource)
	}
	if v.Message != "file not found" {
		t.Errorf("Message = %s, want 'file not found'", v.Message)
	}
	if v.Context["path"] != "/config.yaml" {
		t.Errorf("Context[path] = %s, want '/config.yaml'", v.Context["path"])
	}
	if v.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestNewFrustrationTracker(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		f := NewFrustrationTracker(DefaultFrustrationConfig())

		if f.maxHistory != DefaultMaxHistory {
			t.Errorf("maxHistory = %d, want %d", f.maxHistory, DefaultMaxHistory)
		}
		if f.streakThreshold != DefaultStreakThreshold {
			t.Errorf("streakThreshold = %d, want %d", f.streakThreshold, DefaultStreakThreshold)
		}
	})

	t.Run("zero config uses defaults", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{})

		if f.maxHistory != DefaultMaxHistory {
			t.Errorf("maxHistory = %d, want %d", f.maxHistory, DefaultMaxHistory)
		}
	})

	t.Run("custom config", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			MaxHistory:        5,
			StreakThreshold:   2,
			CategoryThreshold: 3,
			OscillationWindow: 4,
		})

		if f.maxHistory != 5 {
			t.Errorf("maxHistory = %d, want 5", f.maxHistory)
		}
		if f.streakThreshold != 2 {
			t.Errorf("streakThreshold = %d, want 2", f.streakThreshold)
		}
	})
}

func TestFrustrationTracker_SameTypeStreak(t *testing.T) {
	t.Run("triggers after streak threshold", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold: 3,
		})

		// First two violations - not stuck
		result := f.RecordViolation(NewViolation(ViolationFileNotFound, "file 1", nil))
		if result.IsStuck {
			t.Error("should not be stuck after 1 violation")
		}

		result = f.RecordViolation(NewViolation(ViolationFileNotFound, "file 2", nil))
		if result.IsStuck {
			t.Error("should not be stuck after 2 violations")
		}

		// Third violation - should be stuck
		result = f.RecordViolation(NewViolation(ViolationFileNotFound, "file 3", nil))
		if !result.IsStuck {
			t.Error("should be stuck after 3 same-type violations")
		}
		if result.DetectionRule != "same_type" {
			t.Errorf("DetectionRule = %s, want 'same_type'", result.DetectionRule)
		}
		if result.Streak != 3 {
			t.Errorf("Streak = %d, want 3", result.Streak)
		}
		if result.ViolationType != ViolationFileNotFound {
			t.Errorf("ViolationType = %s, want %s", result.ViolationType, ViolationFileNotFound)
		}
	})

	t.Run("different types break streak", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   3,
			CategoryThreshold: 10, // High to avoid triggering
		})

		f.RecordViolation(NewViolation(ViolationFileNotFound, "file 1", nil))
		f.RecordViolation(NewViolation(ViolationFileNotFound, "file 2", nil))
		// Different type breaks the streak
		result := f.RecordViolation(NewViolation(ViolationPermissionDenied, "permission", nil))

		if result.IsStuck {
			t.Error("different type should break streak")
		}
	})
}

func TestFrustrationTracker_CategorySaturation(t *testing.T) {
	t.Run("triggers after category threshold", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10, // High to not trigger same_type
			CategoryThreshold: 3,
		})

		// Different resource types but same category
		f.RecordViolation(NewViolation(ViolationFileNotFound, "file", nil))
		f.RecordViolation(NewViolation(ViolationPermissionDenied, "permission", nil))
		result := f.RecordViolation(NewViolation(ViolationNetworkError, "network", nil))

		if !result.IsStuck {
			t.Error("should be stuck after 3 same-category violations")
		}
		if result.DetectionRule != "category_saturation" {
			t.Errorf("DetectionRule = %s, want 'category_saturation'", result.DetectionRule)
		}
		if result.ViolationCategory != CategoryResource {
			t.Errorf("ViolationCategory = %s, want %s", result.ViolationCategory, CategoryResource)
		}
	})

	t.Run("different categories break saturation", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10,
			CategoryThreshold: 3,
		})

		f.RecordViolation(NewViolation(ViolationFileNotFound, "file", nil))
		f.RecordViolation(NewViolation(ViolationPermissionDenied, "permission", nil))
		// Different category breaks saturation
		result := f.RecordViolation(NewViolation(ViolationIntentLoop, "intent", nil))

		if result.IsStuck {
			t.Error("different category should break saturation")
		}
	})
}

func TestFrustrationTracker_Oscillation(t *testing.T) {
	t.Run("triggers on A-B-A-B pattern", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10,
			CategoryThreshold: 10,
			OscillationWindow: 6,
		})

		// Create A-B-A-B-A-B pattern
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A1", nil))
		f.RecordViolation(NewViolation(ViolationMalformedTool, "B1", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A2", nil))
		f.RecordViolation(NewViolation(ViolationMalformedTool, "B2", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A3", nil))
		result := f.RecordViolation(NewViolation(ViolationMalformedTool, "B3", nil))

		if !result.IsStuck {
			t.Error("should detect oscillation pattern")
		}
		if result.DetectionRule != "oscillation" {
			t.Errorf("DetectionRule = %s, want 'oscillation'", result.DetectionRule)
		}
	})

	t.Run("no oscillation with same types", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10,
			CategoryThreshold: 10,
			OscillationWindow: 4,
		})

		// All same type - not oscillation
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A1", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A2", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A3", nil))
		result := f.RecordViolation(NewViolation(ViolationIntentLoop, "A4", nil))

		// Should not trigger oscillation (might trigger same_type though)
		if result.IsStuck && result.DetectionRule == "oscillation" {
			t.Error("same types should not trigger oscillation")
		}
	})

	t.Run("broken pattern not detected", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10,
			CategoryThreshold: 10,
			OscillationWindow: 6,
		})

		// A-B-A-C-A-B - pattern broken
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A1", nil))
		f.RecordViolation(NewViolation(ViolationMalformedTool, "B1", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A2", nil))
		f.RecordViolation(NewViolation(ViolationFileNotFound, "C", nil)) // Breaks pattern
		f.RecordViolation(NewViolation(ViolationIntentLoop, "A3", nil))
		result := f.RecordViolation(NewViolation(ViolationMalformedTool, "B2", nil))

		if result.IsStuck && result.DetectionRule == "oscillation" {
			t.Error("broken pattern should not trigger oscillation")
		}
	})
}

func TestFrustrationTracker_NoFalsePositives(t *testing.T) {
	t.Run("different types no stuck", func(t *testing.T) {
		f := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   3,
			CategoryThreshold: 4,
			OscillationWindow: 6,
		})

		// All different types
		f.RecordViolation(NewViolation(ViolationFileNotFound, "v1", nil))
		f.RecordViolation(NewViolation(ViolationIntentLoop, "v2", nil))
		result := f.RecordViolation(NewViolation(ViolationAmbiguous, "v3", nil))

		if result.IsStuck {
			t.Error("different types/categories should not trigger stuck")
		}
	})

	t.Run("single violation not stuck", func(t *testing.T) {
		f := NewFrustrationTracker(DefaultFrustrationConfig())

		result := f.RecordViolation(NewViolation(ViolationFileNotFound, "v1", nil))

		if result.IsStuck {
			t.Error("single violation should not trigger stuck")
		}
	})
}

func TestFrustrationTracker_Reset(t *testing.T) {
	f := NewFrustrationTracker(FrustrationConfig{
		StreakThreshold: 2,
	})

	// Get to stuck state
	f.RecordViolation(NewViolation(ViolationFileNotFound, "v1", nil))
	f.RecordViolation(NewViolation(ViolationFileNotFound, "v2", nil))

	if !f.IsStuck() {
		t.Error("should be stuck before reset")
	}

	// Reset
	f.Reset()

	if f.IsStuck() {
		t.Error("should not be stuck after reset")
	}
	if f.ViolationCount() != 0 {
		t.Errorf("ViolationCount = %d, want 0 after reset", f.ViolationCount())
	}
}

func TestFrustrationTracker_GetRecentViolations(t *testing.T) {
	f := NewFrustrationTracker(FrustrationConfig{
		MaxHistory: 5,
	})

	// Add 3 violations
	f.RecordViolation(NewViolation(ViolationFileNotFound, "v1", nil))
	f.RecordViolation(NewViolation(ViolationFileNotFound, "v2", nil))
	f.RecordViolation(NewViolation(ViolationFileNotFound, "v3", nil))

	t.Run("get all", func(t *testing.T) {
		violations := f.GetRecentViolations(10)
		if len(violations) != 3 {
			t.Errorf("len = %d, want 3", len(violations))
		}
	})

	t.Run("get subset", func(t *testing.T) {
		violations := f.GetRecentViolations(2)
		if len(violations) != 2 {
			t.Errorf("len = %d, want 2", len(violations))
		}
		// Should be the most recent
		if violations[1].Message != "v3" {
			t.Errorf("last message = %s, want 'v3'", violations[1].Message)
		}
	})
}

func TestFrustrationTracker_MaxHistory(t *testing.T) {
	f := NewFrustrationTracker(FrustrationConfig{
		MaxHistory:      3,
		StreakThreshold: 10, // High to not trigger
	})

	// Add 5 violations
	for i := 0; i < 5; i++ {
		f.RecordViolation(NewViolation(ViolationType("v"+string(rune('0'+i))), "msg", nil))
	}

	// Should only have last 3
	if f.ViolationCount() != 3 {
		t.Errorf("ViolationCount = %d, want 3", f.ViolationCount())
	}

	violations := f.GetRecentViolations(10)
	if len(violations) != 3 {
		t.Errorf("len = %d, want 3", len(violations))
	}
}

func TestFrustrationTracker_ThreadSafety(t *testing.T) {
	f := NewFrustrationTracker(FrustrationConfig{
		StreakThreshold:   100, // High to not trigger
		CategoryThreshold: 100,
		OscillationWindow: 100,
	})

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					f.RecordViolation(NewViolation(ViolationFileNotFound, "concurrent", nil))
				}
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = f.GetRecentViolations(5)
					_ = f.IsStuck()
					_ = f.ViolationCount()
				}
			}
		}()
	}

	// Concurrent reset
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				f.Reset()
			}
		}
	}()

	// Run briefly
	close(done)
	wg.Wait()
}

func TestHelpRequest_String(t *testing.T) {
	h := HelpRequest{
		Title:       "I Need Your Help",
		WhatITried:  []string{"Tried A", "Tried B"},
		TheProblem:  "Could not find the file",
		Suggestions: []string{"Check the path", "Provide alternative"},
		CanSkip:     true,
		SkipMessage: "I can proceed without it",
	}

	result := h.String()

	if !strings.Contains(result, "I Need Your Help") {
		t.Error("should contain title")
	}
	if !strings.Contains(result, "What I Tried") {
		t.Error("should contain What I Tried section")
	}
	if !strings.Contains(result, "Tried A") {
		t.Error("should contain attempts")
	}
	if !strings.Contains(result, "The Problem") {
		t.Error("should contain The Problem section")
	}
	if !strings.Contains(result, "How You Can Help") {
		t.Error("should contain suggestions section")
	}
	if !strings.Contains(result, "Or Skip This Step") {
		t.Error("should contain skip section")
	}
}

func TestGenerateHelpRequest(t *testing.T) {
	f := NewFrustrationTracker(FrustrationConfig{
		StreakThreshold: 2,
	})

	t.Run("file not found", func(t *testing.T) {
		f.Reset()
		f.RecordViolation(NewViolation(ViolationFileNotFound, "config.yaml not found", map[string]string{"path": "/config.yaml"}))
		result := f.RecordViolation(NewViolation(ViolationFileNotFound, "config.yml not found", map[string]string{"path": "/config.yml"}))

		if !result.IsStuck {
			t.Fatal("should be stuck")
		}

		h := result.HelpRequest
		if h.Title == "" {
			t.Error("should have title")
		}
		if len(h.WhatITried) == 0 {
			t.Error("should list what was tried")
		}
		if h.TheProblem == "" {
			t.Error("should explain problem")
		}
		if len(h.Suggestions) == 0 {
			t.Error("should have suggestions")
		}
	})

	t.Run("intent loop", func(t *testing.T) {
		f.Reset()
		f.RecordViolation(NewViolation(ViolationIntentLoop, "intent 1", nil))
		result := f.RecordViolation(NewViolation(ViolationIntentLoop, "intent 2", nil))

		if !result.IsStuck {
			t.Fatal("should be stuck")
		}

		h := result.HelpRequest
		if !strings.Contains(h.TheProblem, "misunderstanding") {
			t.Errorf("intent loop should mention misunderstanding, got: %s", h.TheProblem)
		}
	})

	t.Run("oscillation", func(t *testing.T) {
		f2 := NewFrustrationTracker(FrustrationConfig{
			StreakThreshold:   10,
			CategoryThreshold: 10,
			OscillationWindow: 4,
		})

		f2.RecordViolation(NewViolation(ViolationIntentLoop, "A", nil))
		f2.RecordViolation(NewViolation(ViolationMalformedTool, "B", nil))
		f2.RecordViolation(NewViolation(ViolationIntentLoop, "A", nil))
		result := f2.RecordViolation(NewViolation(ViolationMalformedTool, "B", nil))

		if !result.IsStuck {
			t.Fatal("should be stuck")
		}

		h := result.HelpRequest
		if !strings.Contains(h.Title, "Circles") {
			t.Errorf("oscillation should have special title, got: %s", h.Title)
		}
	})
}

func BenchmarkFrustrationTracker_RecordViolation(b *testing.B) {
	f := NewFrustrationTracker(FrustrationConfig{
		MaxHistory:        10,
		StreakThreshold:   100, // High to not trigger
		CategoryThreshold: 100,
		OscillationWindow: 100,
	})

	v := NewViolation(ViolationFileNotFound, "benchmark", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.RecordViolation(v)
		if i%10 == 0 {
			f.Reset()
		}
	}
}

func BenchmarkFrustrationTracker_IsStuck(b *testing.B) {
	f := NewFrustrationTracker(DefaultFrustrationConfig())

	// Add some violations
	for i := 0; i < 5; i++ {
		f.RecordViolation(NewViolation(ViolationType("type"+string(rune('0'+i))), "msg", nil))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = f.IsStuck()
	}
}
