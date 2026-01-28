// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"math"
	"testing"
)

func TestCalibrateConfidence(t *testing.T) {
	t.Run("no adjustments returns base", func(t *testing.T) {
		result := CalibrateConfidence(0.8)
		if result != 0.8 {
			t.Errorf("expected 0.8, got %f", result)
		}
	})

	t.Run("single adjustment multiplies correctly", func(t *testing.T) {
		result := CalibrateConfidence(0.8, ConfidenceAdjustment{
			Reason:     "test",
			Multiplier: 0.5,
		})
		if math.Abs(result-0.4) > 0.001 {
			t.Errorf("expected 0.4, got %f", result)
		}
	})

	t.Run("multiple adjustments chain correctly", func(t *testing.T) {
		result := CalibrateConfidence(1.0,
			ConfidenceAdjustment{Reason: "a", Multiplier: 0.5},
			ConfidenceAdjustment{Reason: "b", Multiplier: 0.5},
		)
		if math.Abs(result-0.25) > 0.001 {
			t.Errorf("expected 0.25, got %f", result)
		}
	})

	t.Run("clamps to maximum 1.0", func(t *testing.T) {
		result := CalibrateConfidence(0.9, ConfidenceAdjustment{
			Reason:     "increase",
			Multiplier: 2.0,
		})
		if result != 1.0 {
			t.Errorf("expected 1.0, got %f", result)
		}
	})

	t.Run("clamps to minimum 0.0", func(t *testing.T) {
		result := CalibrateConfidence(0.5, ConfidenceAdjustment{
			Reason:     "decrease",
			Multiplier: -1.0, // Invalid but should clamp
		})
		if result != 0.0 {
			t.Errorf("expected 0.0, got %f", result)
		}
	})
}

func TestConfidenceCalibration(t *testing.T) {
	t.Run("tracks adjustments", func(t *testing.T) {
		cal := NewConfidenceCalibration(0.8)
		cal.Apply(AdjustmentExportedSymbol)
		cal.Apply(AdjustmentManyCallers)

		if cal.BaseScore != 0.8 {
			t.Errorf("expected base 0.8, got %f", cal.BaseScore)
		}
		if len(cal.Adjustments) != 2 {
			t.Errorf("expected 2 adjustments, got %d", len(cal.Adjustments))
		}
	})

	t.Run("Apply chains correctly", func(t *testing.T) {
		cal := NewConfidenceCalibration(1.0)
		result := cal.Apply(ConfidenceAdjustment{Reason: "a", Multiplier: 0.9}).
			Apply(ConfidenceAdjustment{Reason: "b", Multiplier: 0.9})

		if result != cal {
			t.Error("Apply should return self for chaining")
		}
		if math.Abs(cal.FinalScore-0.81) > 0.001 {
			t.Errorf("expected 0.81, got %f", cal.FinalScore)
		}
	})

	t.Run("ApplyIf only applies when condition true", func(t *testing.T) {
		cal := NewConfidenceCalibration(1.0)
		cal.ApplyIf(true, ConfidenceAdjustment{Reason: "applied", Multiplier: 0.5})
		cal.ApplyIf(false, ConfidenceAdjustment{Reason: "not applied", Multiplier: 0.1})

		if len(cal.Adjustments) != 1 {
			t.Errorf("expected 1 adjustment, got %d", len(cal.Adjustments))
		}
		if cal.FinalScore != 0.5 {
			t.Errorf("expected 0.5, got %f", cal.FinalScore)
		}
	})
}

func TestGetConfidenceLevel(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   ConfidenceLevel
	}{
		{1.0, ConfidenceLevelVeryHigh},
		{0.95, ConfidenceLevelVeryHigh},
		{0.9, ConfidenceLevelVeryHigh},
		{0.89, ConfidenceLevelHigh},
		{0.7, ConfidenceLevelHigh},
		{0.69, ConfidenceLevelMedium},
		{0.5, ConfidenceLevelMedium},
		{0.49, ConfidenceLevelLow},
		{0.3, ConfidenceLevelLow},
		{0.29, ConfidenceLevelVeryLow},
		{0.0, ConfidenceLevelVeryLow},
	}

	for _, tt := range tests {
		result := GetConfidenceLevel(tt.confidence)
		if result != tt.expected {
			t.Errorf("GetConfidenceLevel(%f) = %s, want %s",
				tt.confidence, result, tt.expected)
		}
	}
}

func TestPredefinedAdjustments(t *testing.T) {
	// Verify predefined adjustments have reasonable values
	adjustments := []ConfidenceAdjustment{
		AdjustmentInTestFile,
		AdjustmentHasNosecComment,
		AdjustmentHighComplexity,
		AdjustmentLowComplexity,
		AdjustmentManyCallers,
		AdjustmentFewCallers,
		AdjustmentExportedSymbol,
		AdjustmentUnexportedSymbol,
		AdjustmentInterfaceMethod,
		AdjustmentStaticAnalysisOnly,
	}

	for _, adj := range adjustments {
		if adj.Reason == "" {
			t.Error("Adjustment should have a reason")
		}
		if adj.Multiplier <= 0 || adj.Multiplier > 2.0 {
			t.Errorf("Adjustment %s has unusual multiplier: %f",
				adj.Reason, adj.Multiplier)
		}
	}
}
