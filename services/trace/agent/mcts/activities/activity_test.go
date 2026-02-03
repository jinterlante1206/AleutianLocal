// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

func TestPriority(t *testing.T) {
	t.Run("priority string representation", func(t *testing.T) {
		tests := []struct {
			priority Priority
			expected string
		}{
			{PriorityLow, "low"},
			{PriorityNormal, "normal"},
			{PriorityHigh, "high"},
			{PriorityCritical, "critical"},
			{Priority(99), "Priority(99)"},
		}

		for _, tc := range tests {
			if got := tc.priority.String(); got != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, got)
			}
		}
	})
}

func TestActivityError(t *testing.T) {
	t.Run("error message format", func(t *testing.T) {
		err := NewActivityError("search", "Execute", ErrNilInput)
		expected := "search.Execute: input must not be nil"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})

	t.Run("unwrap returns underlying error", func(t *testing.T) {
		err := NewActivityError("search", "Execute", ErrNilInput)
		if err.Unwrap() != ErrNilInput {
			t.Error("unwrap should return underlying error")
		}
	})
}

func TestBaseInput(t *testing.T) {
	t.Run("creates with correct values", func(t *testing.T) {
		input := NewBaseInput("req-123", crs.SignalSourceHard)
		if input.RequestID != "req-123" {
			t.Errorf("expected req-123, got %s", input.RequestID)
		}
		if input.Source() != crs.SignalSourceHard {
			t.Error("expected hard signal source")
		}
		if input.Type() != "base" {
			t.Errorf("expected base, got %s", input.Type())
		}
	})
}

func TestActivityResult(t *testing.T) {
	t.Run("success count", func(t *testing.T) {
		// Would need mock algorithm results
		result := ActivityResult{
			AlgorithmResults: nil,
		}
		if result.SuccessCount() != 0 {
			t.Error("expected 0 success count for empty results")
		}
	})

	t.Run("failure count", func(t *testing.T) {
		result := ActivityResult{
			AlgorithmResults: nil,
		}
		if result.FailureCount() != 0 {
			t.Error("expected 0 failure count for empty results")
		}
	})
}

func TestBaseActivity(t *testing.T) {
	t.Run("creates with correct values", func(t *testing.T) {
		activity := NewBaseActivity("test", 5*time.Second)
		if activity.Name() != "test" {
			t.Errorf("expected test, got %s", activity.Name())
		}
		if activity.Timeout() != 5*time.Second {
			t.Errorf("expected 5s timeout, got %v", activity.Timeout())
		}
	})

	t.Run("algorithms returns empty slice", func(t *testing.T) {
		activity := NewBaseActivity("test", 5*time.Second)
		if len(activity.Algorithms()) != 0 {
			t.Error("expected empty algorithms slice")
		}
	})
}

func TestActivityConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultActivityConfig()
		if config.Timeout != 30*time.Second {
			t.Errorf("expected 30s timeout, got %v", config.Timeout)
		}
		if !config.EnableMetrics {
			t.Error("expected metrics enabled")
		}
		if !config.EnableTracing {
			t.Error("expected tracing enabled")
		}
		if config.MaxConcurrentAlgorithms != 10 {
			t.Errorf("expected 10 max concurrent, got %d", config.MaxConcurrentAlgorithms)
		}
	})

	t.Run("validate valid config", func(t *testing.T) {
		config := DefaultActivityConfig()
		if err := config.Validate(); err != nil {
			t.Errorf("expected valid config, got %v", err)
		}
	})

	t.Run("validate invalid timeout", func(t *testing.T) {
		config := &ActivityConfig{Timeout: -1}
		if err := config.Validate(); err != ErrInvalidConfig {
			t.Errorf("expected ErrInvalidConfig, got %v", err)
		}
	})

	t.Run("validate invalid max concurrent", func(t *testing.T) {
		config := &ActivityConfig{MaxConcurrentAlgorithms: -1}
		if err := config.Validate(); err != ErrInvalidConfig {
			t.Errorf("expected ErrInvalidConfig, got %v", err)
		}
	})
}
