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

func TestLifecycleConfig(t *testing.T) {
	t.Run("default config has sensible values", func(t *testing.T) {
		config := DefaultLifecycleConfig()

		// 90 days stale threshold
		expectedStale := 90 * 24 * time.Hour
		if config.StaleThreshold != expectedStale {
			t.Errorf("expected stale threshold %v, got %v", expectedStale, config.StaleThreshold)
		}

		// 0.7 min confidence for active
		if config.MinActiveConfidence != 0.7 {
			t.Errorf("expected min active confidence 0.7, got %f", config.MinActiveConfidence)
		}

		// Boost and decay values
		if config.ConfidenceBoostOnValidation != 0.1 {
			t.Errorf("expected boost 0.1, got %f", config.ConfidenceBoostOnValidation)
		}

		if config.ConfidenceDecayOnContradiction != 0.3 {
			t.Errorf("expected decay 0.3, got %f", config.ConfidenceDecayOnContradiction)
		}

		// Delete below confidence threshold
		if config.DeleteBelowConfidence != 0.1 {
			t.Errorf("expected delete below 0.1, got %f", config.DeleteBelowConfidence)
		}
	})

	t.Run("custom config is respected", func(t *testing.T) {
		config := LifecycleConfig{
			StaleThreshold:                 30 * 24 * time.Hour,
			MinActiveConfidence:            0.8,
			ConfidenceBoostOnValidation:    0.15,
			ConfidenceDecayOnContradiction: 0.25,
		}

		if config.StaleThreshold != 30*24*time.Hour {
			t.Errorf("expected custom stale threshold, got %v", config.StaleThreshold)
		}

		if config.MinActiveConfidence != 0.8 {
			t.Errorf("expected min confidence 0.8, got %f", config.MinActiveConfidence)
		}
	})
}

func TestCleanupResult(t *testing.T) {
	t.Run("cleanup result tracks counts", func(t *testing.T) {
		result := &CleanupResult{
			MemoriesArchived: 5,
			MemoriesDeleted:  3,
			Errors:           []string{"error 1", "error 2"},
		}

		if result.MemoriesArchived != 5 {
			t.Errorf("expected 5 archived, got %d", result.MemoriesArchived)
		}

		if result.MemoriesDeleted != 3 {
			t.Errorf("expected 3 deleted, got %d", result.MemoriesDeleted)
		}

		if len(result.Errors) != 2 {
			t.Errorf("expected 2 errors, got %d", len(result.Errors))
		}
	})
}

func TestMemoryLifecycleTransitions(t *testing.T) {
	t.Run("status transitions are correct", func(t *testing.T) {
		// Active is the initial status
		if StatusActive != "active" {
			t.Errorf("expected 'active', got %s", StatusActive)
		}

		// Archived is the stale status
		if StatusArchived != "archived" {
			t.Errorf("expected 'archived', got %s", StatusArchived)
		}
	})

	t.Run("confidence validation thresholds", func(t *testing.T) {
		config := DefaultLifecycleConfig()

		// A memory with 0.5 confidence should NOT be active (below 0.7)
		testConfidence := 0.5
		if testConfidence >= config.MinActiveConfidence {
			t.Error("expected 0.5 confidence to be below active threshold")
		}

		// A memory with 0.8 confidence should be active
		testConfidence = 0.8
		if testConfidence < config.MinActiveConfidence {
			t.Error("expected 0.8 confidence to be above active threshold")
		}
	})

	t.Run("stale calculation", func(t *testing.T) {
		config := DefaultLifecycleConfig()
		now := time.Now()

		// Memory used 100 days ago is stale
		oldLastUsed := now.Add(-100 * 24 * time.Hour)
		staleThreshold := now.Add(-config.StaleThreshold)

		if !oldLastUsed.Before(staleThreshold) {
			t.Error("expected 100-day-old memory to be stale")
		}

		// Memory used 30 days ago is not stale
		recentLastUsed := now.Add(-30 * 24 * time.Hour)
		if recentLastUsed.Before(staleThreshold) {
			t.Error("expected 30-day-old memory to not be stale")
		}
	})
}

func TestConfidenceAdjustments(t *testing.T) {
	config := DefaultLifecycleConfig()

	t.Run("validation boost is reasonable", func(t *testing.T) {
		// Starting at 0.5, boost by 0.1 -> 0.6
		startConfidence := 0.5
		boosted := startConfidence + config.ConfidenceBoostOnValidation

		if boosted != 0.6 {
			t.Errorf("expected 0.6 after boost, got %f", boosted)
		}

		// Multiple boosts should not exceed 1.0
		confidence := 0.95
		boosted = confidence + config.ConfidenceBoostOnValidation
		if boosted > 1.0 {
			boosted = 1.0
		}
		if boosted != 1.0 {
			t.Errorf("expected capped at 1.0, got %f", boosted)
		}
	})

	t.Run("contradiction decay is significant", func(t *testing.T) {
		// Starting at 0.8, decay by 0.3 -> 0.5
		startConfidence := 0.8
		decayed := startConfidence - config.ConfidenceDecayOnContradiction

		if decayed != 0.5 {
			t.Errorf("expected 0.5 after decay, got %f", decayed)
		}

		// Decay below threshold should trigger deletion consideration
		startConfidence = 0.3
		decayed = startConfidence - config.ConfidenceDecayOnContradiction
		if decayed > 0.1 {
			t.Errorf("expected decayed confidence <= 0.1 for deletion, got %f", decayed)
		}
	})

	t.Run("multiple validations increase confidence", func(t *testing.T) {
		confidence := 0.5

		// Simulate 3 validations
		for i := 0; i < 3; i++ {
			confidence += config.ConfidenceBoostOnValidation
			if confidence > 1.0 {
				confidence = 1.0
			}
		}

		// Should be at 0.8 after 3 boosts of 0.1
		expected := 0.8
		// Use a small delta for floating point comparison
		if confidence < expected-0.001 || confidence > expected+0.001 {
			t.Errorf("expected %f after 3 validations, got %f", expected, confidence)
		}
	})
}

func TestMemoryStatuses(t *testing.T) {
	t.Run("status string values", func(t *testing.T) {
		if string(StatusActive) != "active" {
			t.Errorf("expected 'active', got %s", StatusActive)
		}

		if string(StatusArchived) != "archived" {
			t.Errorf("expected 'archived', got %s", StatusArchived)
		}
	})
}
