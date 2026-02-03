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

import "math"

// ConfidenceAdjustment represents a factor that adjusts confidence scores.
type ConfidenceAdjustment struct {
	// Reason explains why the adjustment is applied.
	Reason string `json:"reason"`

	// Multiplier is the factor to multiply confidence by.
	// Values < 1.0 decrease confidence, > 1.0 increase confidence.
	Multiplier float64 `json:"multiplier"`
}

// Common confidence adjustments for reuse across analyzers.
var (
	// AdjustmentInTestFile lowers confidence for symbols in test files.
	// Test files often have unusual patterns that don't represent production code.
	AdjustmentInTestFile = ConfidenceAdjustment{
		Reason:     "in test file",
		Multiplier: 0.5,
	}

	// AdjustmentHasNosecComment lowers confidence when nosec/nolint comment nearby.
	// The developer has explicitly acknowledged the pattern.
	AdjustmentHasNosecComment = ConfidenceAdjustment{
		Reason:     "nosec/nolint comment nearby",
		Multiplier: 0.2,
	}

	// AdjustmentHighComplexity increases confidence for high-complexity code.
	// Complex code is more likely to have issues.
	AdjustmentHighComplexity = ConfidenceAdjustment{
		Reason:     "high cyclomatic complexity",
		Multiplier: 1.3,
	}

	// AdjustmentLowComplexity decreases confidence for simple code.
	// Simple code is less likely to have issues.
	AdjustmentLowComplexity = ConfidenceAdjustment{
		Reason:     "low cyclomatic complexity",
		Multiplier: 0.7,
	}

	// AdjustmentManyCallers increases confidence when many callers exist.
	// More callers means higher impact if there's an issue.
	AdjustmentManyCallers = ConfidenceAdjustment{
		Reason:     "many callers (>10)",
		Multiplier: 1.2,
	}

	// AdjustmentFewCallers decreases confidence when few callers exist.
	// Fewer callers means lower impact.
	AdjustmentFewCallers = ConfidenceAdjustment{
		Reason:     "few callers (<3)",
		Multiplier: 0.8,
	}

	// AdjustmentExportedSymbol increases confidence for exported symbols.
	// Exported symbols have wider impact potential.
	AdjustmentExportedSymbol = ConfidenceAdjustment{
		Reason:     "exported symbol",
		Multiplier: 1.1,
	}

	// AdjustmentUnexportedSymbol decreases confidence for unexported symbols.
	// Unexported symbols have limited scope.
	AdjustmentUnexportedSymbol = ConfidenceAdjustment{
		Reason:     "unexported symbol",
		Multiplier: 0.9,
	}

	// AdjustmentInterfaceMethod increases confidence for interface methods.
	// Interface methods have contract implications.
	AdjustmentInterfaceMethod = ConfidenceAdjustment{
		Reason:     "interface method",
		Multiplier: 1.3,
	}

	// AdjustmentStaticAnalysisOnly reminds that this is static analysis.
	// Runtime behavior may differ.
	AdjustmentStaticAnalysisOnly = ConfidenceAdjustment{
		Reason:     "static analysis only",
		Multiplier: 0.95,
	}
)

// CalibrateConfidence computes a calibrated confidence score.
//
// Description:
//
//	Takes a base confidence score and applies a series of adjustments.
//	Each adjustment multiplies the current score. The result is clamped
//	to the range [0.0, 1.0].
//
// Inputs:
//
//	base - The starting confidence score (0.0-1.0).
//	adjustments - Zero or more adjustments to apply.
//
// Outputs:
//
//	float64 - The calibrated confidence score (0.0-1.0).
//
// Example:
//
//	confidence := CalibrateConfidence(0.8,
//	    AdjustmentExportedSymbol,
//	    AdjustmentManyCallers,
//	)
//
// Thread Safety:
//
//	This function is safe for concurrent use.
func CalibrateConfidence(base float64, adjustments ...ConfidenceAdjustment) float64 {
	score := base

	for _, adj := range adjustments {
		score *= adj.Multiplier
	}

	// Clamp to valid range
	return math.Max(0.0, math.Min(1.0, score))
}

// ConfidenceCalibration tracks the full calculation for transparency.
type ConfidenceCalibration struct {
	// BaseScore is the starting confidence.
	BaseScore float64 `json:"base_score"`

	// Adjustments lists all adjustments applied.
	Adjustments []ConfidenceAdjustment `json:"adjustments"`

	// FinalScore is the calibrated result.
	FinalScore float64 `json:"final_score"`
}

// NewConfidenceCalibration creates a new calibration tracker.
//
// Description:
//
//	Creates a ConfidenceCalibration that tracks all adjustments applied
//	to a base score, useful for explaining how a confidence score was
//	calculated.
//
// Inputs:
//
//	base - The starting confidence score (0.0-1.0).
//
// Outputs:
//
//	*ConfidenceCalibration - A new calibration tracker.
//
// Example:
//
//	cal := NewConfidenceCalibration(0.8)
//	cal.Apply(AdjustmentExportedSymbol)
//	cal.Apply(AdjustmentManyCallers)
//	fmt.Printf("Final: %.2f\n", cal.FinalScore)
func NewConfidenceCalibration(base float64) *ConfidenceCalibration {
	return &ConfidenceCalibration{
		BaseScore:   base,
		Adjustments: make([]ConfidenceAdjustment, 0),
		FinalScore:  base,
	}
}

// Apply adds an adjustment to the calibration.
//
// Description:
//
//	Applies the given adjustment to the current score and records it
//	in the adjustments list for transparency.
//
// Inputs:
//
//	adj - The adjustment to apply.
//
// Outputs:
//
//	*ConfidenceCalibration - Returns self for chaining.
func (c *ConfidenceCalibration) Apply(adj ConfidenceAdjustment) *ConfidenceCalibration {
	c.Adjustments = append(c.Adjustments, adj)
	c.FinalScore *= adj.Multiplier
	c.FinalScore = math.Max(0.0, math.Min(1.0, c.FinalScore))
	return c
}

// ApplyIf conditionally applies an adjustment.
//
// Description:
//
//	Applies the adjustment only if the condition is true.
//	Useful for conditional adjustments based on analysis results.
//
// Inputs:
//
//	condition - Whether to apply the adjustment.
//	adj - The adjustment to apply if condition is true.
//
// Outputs:
//
//	*ConfidenceCalibration - Returns self for chaining.
func (c *ConfidenceCalibration) ApplyIf(condition bool, adj ConfidenceAdjustment) *ConfidenceCalibration {
	if condition {
		return c.Apply(adj)
	}
	return c
}

// ConfidenceLevel categorizes confidence into human-readable levels.
type ConfidenceLevel string

const (
	// ConfidenceLevelVeryHigh indicates very high confidence (>= 0.9).
	ConfidenceLevelVeryHigh ConfidenceLevel = "very_high"

	// ConfidenceLevelHigh indicates high confidence (>= 0.7).
	ConfidenceLevelHigh ConfidenceLevel = "high"

	// ConfidenceLevelMedium indicates medium confidence (>= 0.5).
	ConfidenceLevelMedium ConfidenceLevel = "medium"

	// ConfidenceLevelLow indicates low confidence (>= 0.3).
	ConfidenceLevelLow ConfidenceLevel = "low"

	// ConfidenceLevelVeryLow indicates very low confidence (< 0.3).
	ConfidenceLevelVeryLow ConfidenceLevel = "very_low"
)

// GetConfidenceLevel converts a numeric confidence to a level.
//
// Description:
//
//	Maps a confidence score (0.0-1.0) to a human-readable level.
//
// Inputs:
//
//	confidence - The confidence score (0.0-1.0).
//
// Outputs:
//
//	ConfidenceLevel - The corresponding level.
func GetConfidenceLevel(confidence float64) ConfidenceLevel {
	switch {
	case confidence >= 0.9:
		return ConfidenceLevelVeryHigh
	case confidence >= 0.7:
		return ConfidenceLevelHigh
	case confidence >= 0.5:
		return ConfidenceLevelMedium
	case confidence >= 0.3:
		return ConfidenceLevelLow
	default:
		return ConfidenceLevelVeryLow
	}
}
