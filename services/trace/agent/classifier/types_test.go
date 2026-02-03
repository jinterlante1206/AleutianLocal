// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"testing"
	"time"
)

func TestClassifierConfig_Validate_AllFields(t *testing.T) {
	t.Run("all valid", func(t *testing.T) {
		config := ClassifierConfig{
			Temperature:         0.5,
			MaxTokens:           100,
			Timeout:             5 * time.Second,
			MaxRetries:          2,
			RetryBackoff:        100 * time.Millisecond,
			CacheTTL:            10 * time.Minute,
			CacheMaxSize:        1000,
			ConfidenceThreshold: 0.7,
			FallbackToRegex:     true,
			MaxConcurrent:       10,
		}
		if err := config.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("temperature too high", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.Temperature = 1.5
		if err := config.Validate(); err == nil {
			t.Error("expected error for temperature > 1.0")
		}
	})

	t.Run("temperature negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.Temperature = -0.5
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative temperature")
		}
	})

	t.Run("max tokens zero", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.MaxTokens = 0
		if err := config.Validate(); err == nil {
			t.Error("expected error for zero max tokens")
		}
	})

	t.Run("max tokens negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.MaxTokens = -100
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative max tokens")
		}
	})

	t.Run("timeout zero", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.Timeout = 0
		if err := config.Validate(); err == nil {
			t.Error("expected error for zero timeout")
		}
	})

	t.Run("max retries negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.MaxRetries = -1
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative max retries")
		}
	})

	t.Run("retry backoff negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.RetryBackoff = -1 * time.Millisecond
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative retry backoff")
		}
	})

	t.Run("cache size zero when TTL set", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.CacheTTL = 10 * time.Minute
		config.CacheMaxSize = 0
		if err := config.Validate(); err == nil {
			t.Error("expected error when cache TTL set but size is zero")
		}
	})

	t.Run("cache size positive with no TTL is valid", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.CacheTTL = 0
		config.CacheMaxSize = 0
		if err := config.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("confidence threshold too high", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.ConfidenceThreshold = 1.5
		if err := config.Validate(); err == nil {
			t.Error("expected error for confidence > 1.0")
		}
	})

	t.Run("confidence threshold negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.ConfidenceThreshold = -0.5
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative confidence")
		}
	})

	t.Run("max concurrent negative", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.MaxConcurrent = -1
		if err := config.Validate(); err == nil {
			t.Error("expected error for negative max concurrent")
		}
	})

	t.Run("multiple errors reported", func(t *testing.T) {
		config := ClassifierConfig{
			Temperature:         2.0, // Invalid
			MaxTokens:           -1,  // Invalid
			Timeout:             0,   // Invalid
			ConfidenceThreshold: 2.0, // Invalid
			CacheTTL:            time.Hour,
			CacheMaxSize:        0, // Invalid with TTL
		}
		err := config.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		// Should mention multiple issues
		errStr := err.Error()
		if len(errStr) < 50 {
			t.Error("expected multiple errors to be reported")
		}
	})
}

func TestDefaultClassifierConfig(t *testing.T) {
	config := DefaultClassifierConfig()

	if config.Temperature != 0.0 {
		t.Errorf("expected Temperature=0.0, got %f", config.Temperature)
	}
	if config.MaxTokens != 256 {
		t.Errorf("expected MaxTokens=256, got %d", config.MaxTokens)
	}
	if config.Timeout != 5*time.Second {
		t.Errorf("expected Timeout=5s, got %v", config.Timeout)
	}
	if config.MaxRetries != 2 {
		t.Errorf("expected MaxRetries=2, got %d", config.MaxRetries)
	}
	if config.RetryBackoff != 100*time.Millisecond {
		t.Errorf("expected RetryBackoff=100ms, got %v", config.RetryBackoff)
	}
	if config.CacheTTL != 10*time.Minute {
		t.Errorf("expected CacheTTL=10m, got %v", config.CacheTTL)
	}
	if config.CacheMaxSize != 1000 {
		t.Errorf("expected CacheMaxSize=1000, got %d", config.CacheMaxSize)
	}
	if config.ConfidenceThreshold != 0.7 {
		t.Errorf("expected ConfidenceThreshold=0.7, got %f", config.ConfidenceThreshold)
	}
	if !config.FallbackToRegex {
		t.Error("expected FallbackToRegex=true")
	}
	if config.MaxConcurrent != 10 {
		t.Errorf("expected MaxConcurrent=10, got %d", config.MaxConcurrent)
	}

	// Default config should be valid
	if err := config.Validate(); err != nil {
		t.Errorf("default config should be valid: %v", err)
	}
}

func TestClassificationResult_ToToolSuggestion_AllCases(t *testing.T) {
	t.Run("analytical with params", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: true,
			Tool:         "find_entry_points",
			Parameters: map[string]any{
				"type":  "test",
				"limit": 10,
			},
			SearchPatterns: []string{"*_test.go", "func Test"},
		}

		suggestion := result.ToToolSuggestion()
		if suggestion == nil {
			t.Fatal("expected non-nil suggestion")
		}
		if suggestion.ToolName != "find_entry_points" {
			t.Errorf("expected find_entry_points, got %s", suggestion.ToolName)
		}
		if suggestion.SearchHint == "" {
			t.Error("expected non-empty search hint")
		}
		if len(suggestion.SearchPatterns) != 2 {
			t.Errorf("expected 2 search patterns, got %d", len(suggestion.SearchPatterns))
		}
	})

	t.Run("analytical no params", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical:   true,
			Tool:           "trace_data_flow",
			SearchPatterns: []string{"pattern1"},
		}

		suggestion := result.ToToolSuggestion()
		if suggestion == nil {
			t.Fatal("expected non-nil suggestion")
		}
		if suggestion.SearchHint == "" {
			t.Error("expected generic search hint")
		}
	})

	t.Run("non-analytical", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: false,
			Tool:         "some_tool", // Should be ignored
		}

		suggestion := result.ToToolSuggestion()
		if suggestion != nil {
			t.Error("expected nil for non-analytical")
		}
	})

	t.Run("analytical no tool", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: true,
			Tool:         "",
		}

		suggestion := result.ToToolSuggestion()
		if suggestion != nil {
			t.Error("expected nil when no tool suggested")
		}
	})
}

func TestClassificationResult_Fields(t *testing.T) {
	result := &ClassificationResult{
		IsAnalytical:       true,
		Tool:               "find_entry_points",
		Parameters:         map[string]any{"type": "test"},
		SearchPatterns:     []string{"*_test.go"},
		Reasoning:          "test query detected",
		Confidence:         0.95,
		Cached:             true,
		Duration:           100 * time.Millisecond,
		FallbackUsed:       false,
		ValidationWarnings: []string{"fuzzy matched tool name"},
	}

	if !result.IsAnalytical {
		t.Error("expected IsAnalytical=true")
	}
	if result.Tool != "find_entry_points" {
		t.Errorf("expected tool=find_entry_points, got %s", result.Tool)
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected confidence=0.95, got %f", result.Confidence)
	}
	if !result.Cached {
		t.Error("expected Cached=true")
	}
	if result.Duration != 100*time.Millisecond {
		t.Errorf("expected duration=100ms, got %v", result.Duration)
	}
	if len(result.ValidationWarnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.ValidationWarnings))
	}
}
