// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// Aggregator aggregates risk signals into an overall risk level.
//
// # Thread Safety
//
// Aggregator is safe for concurrent use.
type Aggregator struct {
	collector *SignalCollector
	weights   Weights
}

// NewAggregator creates a new Aggregator.
//
// # Inputs
//
//   - index: The memory index for symbol lookups. May be nil for policy-only mode.
//   - projectRoot: The root directory of the project.
//
// # Outputs
//
//   - *Aggregator: The new aggregator.
func NewAggregator(index *initializer.MemoryIndex, projectRoot string) *Aggregator {
	return &Aggregator{
		collector: NewSignalCollector(index, projectRoot),
		weights:   DefaultWeights(),
	}
}

// SetWeights sets custom weights for risk signals.
func (a *Aggregator) SetWeights(w Weights) {
	a.weights = w
}

// Assess performs risk assessment on the given changes.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - cfg: Configuration for the assessment.
//
// # Outputs
//
//   - *Result: The assessment result.
//   - error: Non-nil on complete failure.
func (a *Aggregator) Assess(ctx context.Context, cfg Config) (*Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	start := time.Now()
	result := NewResult()

	// Set up total timeout
	totalTimeout := time.Duration(cfg.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	// Get changed files
	changedFiles, err := GetChangedFiles(ctx, cfg, cfg.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("get changed files: %w", err)
	}

	// No changes = low risk
	if len(changedFiles) == 0 {
		result.RiskLevel = RiskLow
		result.Score = 0
		result.Recommendation = Recommendations[RiskLow]
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Use custom weights if specified
	weights := cfg.Weights
	if weights.Total() == 0 {
		weights = a.weights
	}

	// Collect signals
	signals, errors := a.collector.CollectSignals(ctx, cfg, changedFiles)

	// Handle errors
	if len(errors) > 0 {
		for _, e := range errors {
			result.Errors = append(result.Errors, e.Error())
		}

		// If not best-effort and we have errors, fail
		if !cfg.BestEffort && len(errors) > 0 {
			// Check if we have at least one signal
			hasSignal := signals.Impact != nil || signals.Policy != nil || signals.Complexity != nil
			if !hasSignal {
				return nil, fmt.Errorf("all signals failed: %v", errors)
			}
		}
	}

	// Store signals in result
	result.Signals = *signals

	// Calculate aggregate risk
	level, score := a.calculateRisk(signals, weights)
	result.RiskLevel = level
	result.Score = score
	result.Recommendation = Recommendations[level]

	// Build contributing factors
	result.Factors = a.buildFactors(signals)

	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// calculateRisk computes the risk level and score from signals.
func (a *Aggregator) calculateRisk(signals *Signals, weights Weights) (RiskLevel, float64) {
	// Critical override: any critical policy violation = CRITICAL
	if signals.Policy != nil && signals.Policy.HasCritical {
		return RiskCritical, 1.0
	}

	score := 0.0
	totalWeight := weights.Total()

	// Fixed weights - missing signals contribute 0
	if signals.Impact != nil {
		score += weights.Impact * signals.Impact.Score
	}
	if signals.Policy != nil {
		score += weights.Policy * signals.Policy.Score
	}
	if signals.Complexity != nil {
		score += weights.Complexity * signals.Complexity.Score
	}

	// Normalize against full weight
	if totalWeight > 0 {
		score /= totalWeight
	}

	// Determine level
	var level RiskLevel
	switch {
	case score >= ThresholdCritical:
		level = RiskCritical
	case score >= ThresholdHigh:
		level = RiskHigh
	case score >= ThresholdMedium:
		level = RiskMedium
	default:
		level = RiskLow
	}

	return level, score
}

// buildFactors extracts contributing factors from signals.
func (a *Aggregator) buildFactors(signals *Signals) []Factor {
	factors := make([]Factor, 0)

	if signals.Impact != nil {
		for _, reason := range signals.Impact.Reasons {
			severity := "info"
			if signals.Impact.Score >= 0.5 {
				severity = "warning"
			}
			factors = append(factors, Factor{
				Signal:   "impact",
				Severity: severity,
				Message:  reason,
			})
		}
	}

	if signals.Policy != nil {
		for _, reason := range signals.Policy.Reasons {
			severity := "warning"
			if signals.Policy.HasCritical {
				severity = "critical"
			}
			factors = append(factors, Factor{
				Signal:   "policy",
				Severity: severity,
				Message:  reason,
			})
		}
	}

	if signals.Complexity != nil {
		for _, reason := range signals.Complexity.Reasons {
			severity := "info"
			if signals.Complexity.Score >= 0.5 {
				severity = "warning"
			}
			factors = append(factors, Factor{
				Signal:   "complexity",
				Severity: severity,
				Message:  reason,
			})
		}
	}

	return factors
}
