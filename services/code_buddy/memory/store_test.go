// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package memory

import (
	"testing"
	"time"
)

func TestCodeMemory_Validate(t *testing.T) {
	t.Run("valid memory passes validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Use pointer receivers for methods",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: 0.8,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("empty content fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: 0.8,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != ErrEmptyContent {
			t.Errorf("expected ErrEmptyContent, got %v", err)
		}
	})

	t.Run("empty scope fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryTypeConvention,
			Scope:      "",
			Confidence: 0.8,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != ErrEmptyScope {
			t.Errorf("expected ErrEmptyScope, got %v", err)
		}
	})

	t.Run("invalid memory type fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryType("invalid"),
			Scope:      "*",
			Confidence: 0.8,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != ErrInvalidMemoryType {
			t.Errorf("expected ErrInvalidMemoryType, got %v", err)
		}
	})

	t.Run("invalid source fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: 0.8,
			Source:     MemorySource("invalid"),
		}

		err := memory.Validate()
		if err != ErrInvalidMemorySource {
			t.Errorf("expected ErrInvalidMemorySource, got %v", err)
		}
	})

	t.Run("confidence below 0 fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: -0.1,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != ErrInvalidConfidence {
			t.Errorf("expected ErrInvalidConfidence, got %v", err)
		}
	})

	t.Run("confidence above 1 fails validation", func(t *testing.T) {
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: 1.1,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != ErrInvalidConfidence {
			t.Errorf("expected ErrInvalidConfidence, got %v", err)
		}
	})

	t.Run("confidence at boundaries is valid", func(t *testing.T) {
		// Test 0.0
		memory := CodeMemory{
			Content:    "Some rule",
			MemoryType: MemoryTypeConvention,
			Scope:      "*",
			Confidence: 0.0,
			Source:     SourceUserFeedback,
		}

		err := memory.Validate()
		if err != nil {
			t.Errorf("expected no error for confidence 0.0, got %v", err)
		}

		// Test 1.0
		memory.Confidence = 1.0
		err = memory.Validate()
		if err != nil {
			t.Errorf("expected no error for confidence 1.0, got %v", err)
		}
	})
}

func TestMemoryTypes(t *testing.T) {
	t.Run("all memory types are valid", func(t *testing.T) {
		types := []MemoryType{
			MemoryTypeConstraint,
			MemoryTypePattern,
			MemoryTypeConvention,
			MemoryTypeBugPattern,
			MemoryTypeOptimization,
			MemoryTypeSecurity,
		}

		for _, mt := range types {
			if !ValidMemoryTypes[mt] {
				t.Errorf("expected %s to be valid", mt)
			}
		}
	})

	t.Run("invalid memory type is not valid", func(t *testing.T) {
		if ValidMemoryTypes[MemoryType("invalid")] {
			t.Error("expected invalid type to not be valid")
		}
	})
}

func TestMemorySources(t *testing.T) {
	t.Run("all memory sources are valid", func(t *testing.T) {
		sources := []MemorySource{
			SourceAgentDiscovery,
			SourceUserFeedback,
			SourceTestFailure,
			SourceCodeReview,
			SourceManual,
		}

		for _, src := range sources {
			if !ValidMemorySources[src] {
				t.Errorf("expected %s to be valid", src)
			}
		}
	})

	t.Run("invalid source is not valid", func(t *testing.T) {
		if ValidMemorySources[MemorySource("invalid")] {
			t.Error("expected invalid source to not be valid")
		}
	})
}

func TestDefaultMemoryStoreConfig(t *testing.T) {
	config := DefaultMemoryStoreConfig()

	if config.DefaultConfidence != 0.5 {
		t.Errorf("expected default confidence 0.5, got %f", config.DefaultConfidence)
	}

	if config.MaxResults != 10 {
		t.Errorf("expected max results 10, got %d", config.MaxResults)
	}
}

func TestDefaultLifecycleConfig(t *testing.T) {
	config := DefaultLifecycleConfig()

	expectedStale := 90 * 24 * time.Hour
	if config.StaleThreshold != expectedStale {
		t.Errorf("expected stale threshold %v, got %v", expectedStale, config.StaleThreshold)
	}

	if config.MinActiveConfidence != 0.7 {
		t.Errorf("expected min active confidence 0.7, got %f", config.MinActiveConfidence)
	}

	if config.ConfidenceBoostOnValidation != 0.1 {
		t.Errorf("expected confidence boost 0.1, got %f", config.ConfidenceBoostOnValidation)
	}

	if config.ConfidenceDecayOnContradiction != 0.3 {
		t.Errorf("expected confidence decay 0.3, got %f", config.ConfidenceDecayOnContradiction)
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("getString returns empty for missing key", func(t *testing.T) {
		m := map[string]interface{}{}
		result := getString(m, "missing")
		if result != "" {
			t.Errorf("expected empty string, got %s", result)
		}
	})

	t.Run("getString returns value for present key", func(t *testing.T) {
		m := map[string]interface{}{"key": "value"}
		result := getString(m, "key")
		if result != "value" {
			t.Errorf("expected 'value', got %s", result)
		}
	})

	t.Run("getString returns empty for non-string value", func(t *testing.T) {
		m := map[string]interface{}{"key": 123}
		result := getString(m, "key")
		if result != "" {
			t.Errorf("expected empty string for non-string value, got %s", result)
		}
	})

	t.Run("getFloat64 returns 0 for missing key", func(t *testing.T) {
		m := map[string]interface{}{}
		result := getFloat64(m, "missing")
		if result != 0 {
			t.Errorf("expected 0, got %f", result)
		}
	})

	t.Run("getFloat64 handles different numeric types", func(t *testing.T) {
		tests := []struct {
			value    interface{}
			expected float64
		}{
			{float64(1.5), 1.5},
			{float32(2.5), 2.5},
			{int(3), 3.0},
		}

		for _, tc := range tests {
			m := map[string]interface{}{"key": tc.value}
			result := getFloat64(m, "key")
			if result != tc.expected {
				t.Errorf("expected %f, got %f for value %v", tc.expected, result, tc.value)
			}
		}
	})

	t.Run("getInt returns 0 for missing key", func(t *testing.T) {
		m := map[string]interface{}{}
		result := getInt(m, "missing")
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("getInt handles different numeric types", func(t *testing.T) {
		tests := []struct {
			value    interface{}
			expected int
		}{
			{int(5), 5},
			{int64(10), 10},
			{float64(15.0), 15},
		}

		for _, tc := range tests {
			m := map[string]interface{}{"key": tc.value}
			result := getInt(m, "key")
			if result != tc.expected {
				t.Errorf("expected %d, got %d for value %v", tc.expected, result, tc.value)
			}
		}
	})
}
