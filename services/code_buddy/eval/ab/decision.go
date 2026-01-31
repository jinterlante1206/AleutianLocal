// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ab

import (
	"fmt"
	"time"
)

// -----------------------------------------------------------------------------
// Recommendation Types
// -----------------------------------------------------------------------------

// Recommendation indicates the suggested action based on experiment results.
type Recommendation int

const (
	// NeedMoreData indicates insufficient samples for a decision.
	NeedMoreData Recommendation = iota

	// KeepControl indicates control variant should be kept.
	KeepControl

	// SwitchToExperiment indicates experiment variant is better.
	SwitchToExperiment

	// NoDifference indicates no statistically significant difference.
	NoDifference
)

// String returns the string representation.
func (r Recommendation) String() string {
	switch r {
	case NeedMoreData:
		return "need_more_data"
	case KeepControl:
		return "keep_control"
	case SwitchToExperiment:
		return "switch_to_experiment"
	case NoDifference:
		return "no_difference"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------------
// Decision Configuration
// -----------------------------------------------------------------------------

// DecisionConfig configures the decision engine.
type DecisionConfig struct {
	// MinSamples is the minimum samples per variant before making a decision.
	// Default: 100
	MinSamples int

	// ConfidenceLevel is the statistical confidence required (e.g., 0.95).
	// Default: 0.95
	ConfidenceLevel float64

	// MinEffectSize is the minimum Cohen's d to consider practically significant.
	// Default: 0.2 (small effect)
	MinEffectSize float64

	// MaxPValue is the maximum p-value for statistical significance.
	// Default: 0.05
	MaxPValue float64

	// MinPower is the minimum statistical power required.
	// Default: 0.8
	MinPower float64

	// ImprovementThreshold is the minimum relative improvement to switch.
	// E.g., 0.05 means experiment must be at least 5% better.
	// Default: 0.0 (any improvement)
	ImprovementThreshold float64

	// MaxDuration is the maximum experiment duration before forcing a decision.
	// Default: 7 days
	MaxDuration time.Duration

	// RequireCorrectness requires correctness match rate >= threshold.
	// Default: 0.99 (99% correctness match)
	RequireCorrectness float64
}

// DefaultDecisionConfig returns sensible defaults.
//
// Outputs:
//   - *DecisionConfig: Default configuration. Never nil.
func DefaultDecisionConfig() *DecisionConfig {
	return &DecisionConfig{
		MinSamples:           100,
		ConfidenceLevel:      0.95,
		MinEffectSize:        0.2,
		MaxPValue:            0.05,
		MinPower:             0.8,
		ImprovementThreshold: 0.0,
		MaxDuration:          7 * 24 * time.Hour,
		RequireCorrectness:   0.99,
	}
}

// -----------------------------------------------------------------------------
// Decision Engine
// -----------------------------------------------------------------------------

// DecisionEngine evaluates A/B test results and makes recommendations.
//
// Description:
//
//	DecisionEngine analyzes collected samples and statistical results
//	to determine whether to keep the control, switch to experiment,
//	or continue collecting data.
//
// Thread Safety: Safe for concurrent use (stateless).
type DecisionEngine struct {
	config *DecisionConfig
}

// NewDecisionEngine creates a new decision engine.
//
// Inputs:
//   - config: Decision configuration. If nil, uses defaults.
//
// Outputs:
//   - *DecisionEngine: The new engine. Never nil.
func NewDecisionEngine(config *DecisionConfig) *DecisionEngine {
	if config == nil {
		config = DefaultDecisionConfig()
	}
	return &DecisionEngine{config: config}
}

// Decision holds the complete decision with supporting evidence.
type Decision struct {
	// Recommendation is the suggested action.
	Recommendation Recommendation

	// Confidence is the statistical confidence in the recommendation.
	Confidence float64

	// Reason explains the recommendation in human-readable form.
	Reason string

	// Evidence contains the supporting statistical evidence.
	Evidence *Evidence

	// Timestamp is when the decision was made.
	Timestamp time.Time
}

// Evidence contains the statistical support for a decision.
type Evidence struct {
	// ControlSamples is the number of control observations.
	ControlSamples int

	// ExperimentSamples is the number of experiment observations.
	ExperimentSamples int

	// ControlMean is the mean latency for control (nanoseconds).
	ControlMean float64

	// ExperimentMean is the mean latency for experiment (nanoseconds).
	ExperimentMean float64

	// MeanDifference is experiment - control (negative = experiment faster).
	MeanDifference float64

	// RelativeImprovement is the percentage improvement (negative = experiment better).
	RelativeImprovement float64

	// TTest is the t-test result.
	TTest *TTestResult

	// EffectSize is Cohen's d.
	EffectSize float64

	// EffectCategory is the effect size interpretation.
	EffectCategory EffectCategory

	// ConfidenceInterval is the CI for the mean difference.
	ConfidenceInterval *ConfidenceInterval

	// Power is the statistical power.
	Power float64

	// CorrectnessMatch is the rate at which outputs matched (0 to 1).
	CorrectnessMatch float64

	// ExperimentDuration is how long the experiment has been running.
	ExperimentDuration time.Duration
}

// DecisionInput contains the data needed to make a decision.
type DecisionInput struct {
	// ControlSamples are the latency samples from control.
	ControlSamples []time.Duration

	// ExperimentSamples are the latency samples from experiment.
	ExperimentSamples []time.Duration

	// CorrectnessMatches is the count of matching outputs.
	CorrectnessMatches int

	// TotalComparisons is the total number of correctness comparisons.
	TotalComparisons int

	// ExperimentStartTime is when the experiment started.
	ExperimentStartTime time.Time
}

// Evaluate analyzes the input and returns a decision.
//
// Inputs:
//   - input: The collected experiment data. Must not be nil.
//
// Outputs:
//   - *Decision: The recommendation with evidence. Never nil.
//
// Thread Safety: Safe for concurrent use.
func (e *DecisionEngine) Evaluate(input *DecisionInput) *Decision {
	decision := &Decision{
		Timestamp: time.Now(),
		Evidence:  &Evidence{},
	}

	if input == nil {
		decision.Recommendation = NeedMoreData
		decision.Reason = "No input data provided"
		return decision
	}

	evidence := decision.Evidence
	evidence.ControlSamples = len(input.ControlSamples)
	evidence.ExperimentSamples = len(input.ExperimentSamples)
	evidence.ExperimentDuration = time.Since(input.ExperimentStartTime)

	// Check minimum samples
	if evidence.ControlSamples < e.config.MinSamples ||
		evidence.ExperimentSamples < e.config.MinSamples {
		decision.Recommendation = NeedMoreData
		decision.Reason = fmt.Sprintf(
			"Insufficient samples: control=%d, experiment=%d (need %d each)",
			evidence.ControlSamples, evidence.ExperimentSamples, e.config.MinSamples,
		)
		return decision
	}

	// Calculate correctness match rate
	if input.TotalComparisons > 0 {
		evidence.CorrectnessMatch = float64(input.CorrectnessMatches) / float64(input.TotalComparisons)
	}

	// Check correctness requirement
	if evidence.CorrectnessMatch < e.config.RequireCorrectness {
		decision.Recommendation = KeepControl
		decision.Confidence = 1.0
		decision.Reason = fmt.Sprintf(
			"Experiment correctness too low: %.2f%% (need %.2f%%)",
			evidence.CorrectnessMatch*100, e.config.RequireCorrectness*100,
		)
		return decision
	}

	// Calculate means
	evidence.ControlMean = mean(input.ControlSamples)
	evidence.ExperimentMean = mean(input.ExperimentSamples)
	evidence.MeanDifference = evidence.ExperimentMean - evidence.ControlMean

	if evidence.ControlMean > 0 {
		evidence.RelativeImprovement = evidence.MeanDifference / evidence.ControlMean
	}

	// Perform t-test
	tTest, err := WelchTTest(input.ControlSamples, input.ExperimentSamples, e.config.MaxPValue)
	if err != nil {
		decision.Recommendation = NeedMoreData
		decision.Reason = fmt.Sprintf("Statistical test failed: %v", err)
		return decision
	}
	evidence.TTest = tTest

	// Calculate effect size
	effectSize, _ := EffectSize(input.ControlSamples, input.ExperimentSamples)
	evidence.EffectSize = effectSize
	evidence.EffectCategory = CategorizeEffect(effectSize)

	// Calculate confidence interval
	ci, _ := CalculateCI(input.ControlSamples, input.ExperimentSamples, e.config.ConfidenceLevel)
	evidence.ConfidenceInterval = ci

	// Calculate power
	evidence.Power = CalculatePower(
		evidence.ControlSamples, evidence.ExperimentSamples,
		evidence.EffectSize, e.config.MaxPValue,
	)

	// Check if we've exceeded max duration
	if evidence.ExperimentDuration > e.config.MaxDuration {
		return e.makeTimeoutDecision(decision, evidence)
	}

	// Check power
	if evidence.Power < e.config.MinPower {
		// Calculate required samples for desired power
		required := RequiredSampleSize(e.config.MinEffectSize, e.config.MaxPValue, e.config.MinPower)
		decision.Recommendation = NeedMoreData
		decision.Confidence = evidence.Power
		decision.Reason = fmt.Sprintf(
			"Insufficient power: %.2f (need %.2f). Estimated samples needed: %d per group",
			evidence.Power, e.config.MinPower, required,
		)
		return decision
	}

	// Make decision based on statistical significance and effect size
	return e.makeStatisticalDecision(decision, evidence)
}

// makeStatisticalDecision uses statistical evidence to decide.
func (e *DecisionEngine) makeStatisticalDecision(decision *Decision, evidence *Evidence) *Decision {
	tTest := evidence.TTest

	// Not statistically significant
	if !tTest.Significant {
		decision.Recommendation = NoDifference
		decision.Confidence = 1 - tTest.PValue
		decision.Reason = fmt.Sprintf(
			"No significant difference (p=%.4f > %.4f). Effect size: %.3f (%s)",
			tTest.PValue, e.config.MaxPValue, evidence.EffectSize, evidence.EffectCategory,
		)
		return decision
	}

	// Statistically significant - check direction and magnitude
	absEffect := evidence.EffectSize
	if absEffect < 0 {
		absEffect = -absEffect
	}

	// Check minimum effect size
	if absEffect < e.config.MinEffectSize {
		decision.Recommendation = NoDifference
		decision.Confidence = 1 - tTest.PValue
		decision.Reason = fmt.Sprintf(
			"Effect size too small: %.3f < %.3f (statistically significant but not practically)",
			absEffect, e.config.MinEffectSize,
		)
		return decision
	}

	// Positive effect size means control > experiment (control is slower/worse for latency).
	// So positive d means experiment is faster/better.
	if evidence.EffectSize > 0 {
		// Experiment is better (faster) - check improvement threshold
		improvement := -evidence.RelativeImprovement // RelativeImprovement is (exp-ctrl)/ctrl, negative when exp is faster
		if improvement < e.config.ImprovementThreshold {
			decision.Recommendation = NoDifference
			decision.Confidence = 1 - tTest.PValue
			decision.Reason = fmt.Sprintf(
				"Improvement %.2f%% below threshold %.2f%%",
				improvement*100, e.config.ImprovementThreshold*100,
			)
			return decision
		}

		decision.Recommendation = SwitchToExperiment
		decision.Confidence = 1 - tTest.PValue
		decision.Reason = fmt.Sprintf(
			"Experiment is %.2f%% faster (p=%.4f, d=%.3f %s)",
			improvement*100, tTest.PValue, evidence.EffectSize, evidence.EffectCategory,
		)
		return decision
	}

	// Negative effect size means control < experiment (experiment is slower/worse for latency)
	degradation := evidence.RelativeImprovement * 100 // Positive when experiment is slower
	decision.Recommendation = KeepControl
	decision.Confidence = 1 - tTest.PValue
	decision.Reason = fmt.Sprintf(
		"Experiment is %.2f%% slower (p=%.4f, d=%.3f %s). Keeping control.",
		degradation, tTest.PValue, evidence.EffectSize, evidence.EffectCategory,
	)
	return decision
}

// makeTimeoutDecision handles experiment timeout.
func (e *DecisionEngine) makeTimeoutDecision(decision *Decision, evidence *Evidence) *Decision {
	// Make best decision with available data
	if evidence.TTest == nil {
		decision.Recommendation = KeepControl
		decision.Confidence = 0.5
		decision.Reason = "Experiment timed out with insufficient data. Defaulting to control."
		return decision
	}

	if evidence.TTest.Significant && evidence.EffectSize > 0 {
		// Experiment appears better (control is slower, so d > 0)
		decision.Recommendation = SwitchToExperiment
		decision.Confidence = 1 - evidence.TTest.PValue
		decision.Reason = fmt.Sprintf(
			"Experiment timed out. Evidence suggests experiment is better (p=%.4f, d=%.3f)",
			evidence.TTest.PValue, evidence.EffectSize,
		)
		return decision
	}

	decision.Recommendation = KeepControl
	decision.Confidence = 0.5
	if evidence.TTest.Significant {
		decision.Confidence = 1 - evidence.TTest.PValue
	}
	decision.Reason = fmt.Sprintf(
		"Experiment timed out after %v. No clear winner, defaulting to control.",
		evidence.ExperimentDuration.Round(time.Hour),
	)
	return decision
}

// -----------------------------------------------------------------------------
// Summary Report
// -----------------------------------------------------------------------------

// Summary generates a human-readable summary of the experiment.
func (d *Decision) Summary() string {
	if d.Evidence == nil {
		return fmt.Sprintf("Recommendation: %s\nReason: %s", d.Recommendation, d.Reason)
	}

	e := d.Evidence
	return fmt.Sprintf(`A/B Test Summary
================
Recommendation: %s
Confidence: %.2f%%
Reason: %s

Samples:
  Control: %d
  Experiment: %d

Performance:
  Control Mean: %.2fms
  Experiment Mean: %.2fms
  Difference: %.2f%% %s

Statistics:
  p-value: %.4f
  Effect Size: %.3f (%s)
  Power: %.2f%%

Correctness: %.2f%%
Duration: %v`,
		d.Recommendation,
		d.Confidence*100,
		d.Reason,
		e.ControlSamples,
		e.ExperimentSamples,
		e.ControlMean/float64(time.Millisecond),
		e.ExperimentMean/float64(time.Millisecond),
		e.RelativeImprovement*100*-1, // Positive = improvement
		directionString(e.MeanDifference),
		e.TTest.PValue,
		e.EffectSize,
		e.EffectCategory,
		e.Power*100,
		e.CorrectnessMatch*100,
		e.ExperimentDuration.Round(time.Minute),
	)
}

func directionString(diff float64) string {
	if diff < 0 {
		return "(experiment faster)"
	}
	if diff > 0 {
		return "(control faster)"
	}
	return "(equal)"
}
