// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package regression

import (
	"fmt"
	"math"
	"time"
)

// -----------------------------------------------------------------------------
// Regression Types
// -----------------------------------------------------------------------------

// RegressionType identifies the type of regression.
type RegressionType int

const (
	// RegressionNone indicates no regression.
	RegressionNone RegressionType = iota

	// RegressionLatencyP50 indicates P50 latency regression.
	RegressionLatencyP50

	// RegressionLatencyP95 indicates P95 latency regression.
	RegressionLatencyP95

	// RegressionLatencyP99 indicates P99 latency regression.
	RegressionLatencyP99

	// RegressionThroughput indicates throughput regression.
	RegressionThroughput

	// RegressionMemory indicates memory usage regression.
	RegressionMemory

	// RegressionErrorRate indicates error rate regression.
	RegressionErrorRate
)

// String returns the string representation.
func (r RegressionType) String() string {
	switch r {
	case RegressionNone:
		return "none"
	case RegressionLatencyP50:
		return "latency_p50"
	case RegressionLatencyP95:
		return "latency_p95"
	case RegressionLatencyP99:
		return "latency_p99"
	case RegressionThroughput:
		return "throughput"
	case RegressionMemory:
		return "memory"
	case RegressionErrorRate:
		return "error_rate"
	default:
		return "unknown"
	}
}

// Severity indicates how severe a regression is.
type Severity int

const (
	// SeverityNone indicates no issue.
	SeverityNone Severity = iota

	// SeverityWarning indicates a warning-level regression.
	SeverityWarning

	// SeverityError indicates an error-level regression.
	SeverityError

	// SeverityCritical indicates a critical regression.
	SeverityCritical
)

// String returns the string representation.
func (s Severity) String() string {
	switch s {
	case SeverityNone:
		return "none"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------------
// Detection Result
// -----------------------------------------------------------------------------

// Regression describes a detected regression.
type Regression struct {
	// Type identifies the regression type.
	Type RegressionType

	// Severity is the regression severity.
	Severity Severity

	// Component is the affected component.
	Component string

	// BaselineValue is the baseline metric value.
	BaselineValue float64

	// CurrentValue is the current metric value.
	CurrentValue float64

	// Change is the relative change (positive = regression).
	Change float64

	// Threshold is the threshold that was exceeded.
	Threshold float64

	// Message is a human-readable description.
	Message string
}

// DetectionResult holds the results of regression detection.
type DetectionResult struct {
	// Component is the component analyzed.
	Component string

	// Baseline is the baseline data used.
	Baseline *BaselineData

	// Regressions contains all detected regressions.
	Regressions []Regression

	// Warnings contains non-blocking issues.
	Warnings []Regression

	// Pass is true if no blocking regressions were found.
	Pass bool

	// MaxSeverity is the highest severity found.
	MaxSeverity Severity

	// AnalyzedAt is when detection was performed.
	AnalyzedAt time.Time
}

// HasRegressions returns true if any regressions were detected.
func (r *DetectionResult) HasRegressions() bool {
	return len(r.Regressions) > 0
}

// HasWarnings returns true if any warnings were detected.
func (r *DetectionResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// -----------------------------------------------------------------------------
// Detector
// -----------------------------------------------------------------------------

// DetectorConfig configures regression detection.
type DetectorConfig struct {
	// LatencyP50Threshold is the allowed P50 increase ratio (e.g., 0.05 = 5%).
	LatencyP50Threshold float64

	// LatencyP95Threshold is the allowed P95 increase ratio.
	LatencyP95Threshold float64

	// LatencyP99Threshold is the allowed P99 increase ratio.
	LatencyP99Threshold float64

	// ThroughputThreshold is the allowed throughput decrease ratio.
	ThroughputThreshold float64

	// MemoryThreshold is the allowed memory increase ratio.
	MemoryThreshold float64

	// ErrorRateThreshold is the allowed error rate increase (absolute).
	ErrorRateThreshold float64

	// WarnThresholdRatio is the ratio of threshold at which to warn.
	// E.g., 0.8 means warn at 80% of threshold.
	WarnThresholdRatio float64

	// MinSamples is the minimum samples required for valid comparison.
	MinSamples int
}

// DefaultDetectorConfig returns sensible defaults.
func DefaultDetectorConfig() *DetectorConfig {
	return &DetectorConfig{
		LatencyP50Threshold: 0.05, // 5% increase allowed
		LatencyP95Threshold: 0.10, // 10% increase allowed
		LatencyP99Threshold: 0.15, // 15% increase allowed
		ThroughputThreshold: 0.05, // 5% decrease allowed
		MemoryThreshold:     0.10, // 10% increase allowed
		ErrorRateThreshold:  0.01, // 1% absolute increase allowed
		WarnThresholdRatio:  0.80, // Warn at 80% of threshold
		MinSamples:          30,
	}
}

// Detector compares current metrics against baselines.
//
// Thread Safety: Safe for concurrent use (stateless).
type Detector struct {
	config *DetectorConfig
}

// NewDetector creates a new regression detector.
//
// Inputs:
//   - config: Detection configuration. If nil, uses defaults.
//
// Outputs:
//   - *Detector: The new detector. Never nil.
func NewDetector(config *DetectorConfig) *Detector {
	if config == nil {
		config = DefaultDetectorConfig()
	}
	return &Detector{config: config}
}

// CurrentMetrics holds current performance measurements.
type CurrentMetrics struct {
	// Latency holds current latency metrics.
	Latency LatencyBaseline

	// Throughput holds current throughput metrics.
	Throughput ThroughputBaseline

	// Memory holds current memory metrics.
	Memory MemoryBaseline

	// ErrorRate is the current error rate.
	ErrorRate float64

	// SampleCount is the number of samples.
	SampleCount int
}

// Detect compares current metrics against a baseline.
//
// Inputs:
//   - baseline: The baseline to compare against.
//   - current: Current performance metrics.
//
// Outputs:
//   - *DetectionResult: Detection results with any regressions found.
//
// Thread Safety: Safe for concurrent use.
func (d *Detector) Detect(baseline *BaselineData, current *CurrentMetrics) *DetectionResult {
	result := &DetectionResult{
		Component:   baseline.Component,
		Baseline:    baseline,
		Regressions: make([]Regression, 0),
		Warnings:    make([]Regression, 0),
		Pass:        true,
		MaxSeverity: SeverityNone,
		AnalyzedAt:  time.Now(),
	}

	// Check sample count
	if current.SampleCount < d.config.MinSamples {
		result.Warnings = append(result.Warnings, Regression{
			Type:     RegressionNone,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("Insufficient samples: %d < %d", current.SampleCount, d.config.MinSamples),
		})
	}

	// Check latency P50
	d.checkLatency(result, RegressionLatencyP50,
		baseline.Latency.P50, current.Latency.P50,
		d.config.LatencyP50Threshold)

	// Check latency P95
	d.checkLatency(result, RegressionLatencyP95,
		baseline.Latency.P95, current.Latency.P95,
		d.config.LatencyP95Threshold)

	// Check latency P99
	d.checkLatency(result, RegressionLatencyP99,
		baseline.Latency.P99, current.Latency.P99,
		d.config.LatencyP99Threshold)

	// Check throughput
	d.checkThroughput(result,
		baseline.Throughput.OpsPerSecond, current.Throughput.OpsPerSecond,
		d.config.ThroughputThreshold)

	// Check memory
	d.checkMemory(result,
		baseline.Memory.AllocBytesPerOp, current.Memory.AllocBytesPerOp,
		d.config.MemoryThreshold)

	// Check error rate
	d.checkErrorRate(result,
		baseline.Error.Rate, current.ErrorRate,
		d.config.ErrorRateThreshold)

	return result
}

// checkLatency checks for latency regression.
func (d *Detector) checkLatency(result *DetectionResult, regType RegressionType,
	baseline, current time.Duration, threshold float64) {

	if baseline == 0 {
		return
	}

	change := float64(current-baseline) / float64(baseline)

	reg := Regression{
		Type:          regType,
		Component:     result.Component,
		BaselineValue: float64(baseline),
		CurrentValue:  float64(current),
		Change:        change,
		Threshold:     threshold,
	}

	if change > threshold {
		reg.Severity = SeverityError
		reg.Message = fmt.Sprintf("%s increased by %.1f%% (threshold: %.1f%%)",
			regType, change*100, threshold*100)
		result.Regressions = append(result.Regressions, reg)
		result.Pass = false
		if reg.Severity > result.MaxSeverity {
			result.MaxSeverity = reg.Severity
		}
	} else if change > threshold*d.config.WarnThresholdRatio {
		reg.Severity = SeverityWarning
		reg.Message = fmt.Sprintf("%s increased by %.1f%% (approaching threshold: %.1f%%)",
			regType, change*100, threshold*100)
		result.Warnings = append(result.Warnings, reg)
		if reg.Severity > result.MaxSeverity && result.MaxSeverity < SeverityError {
			result.MaxSeverity = reg.Severity
		}
	}
}

// checkThroughput checks for throughput regression.
func (d *Detector) checkThroughput(result *DetectionResult,
	baseline, current float64, threshold float64) {

	if baseline == 0 {
		return
	}

	// For throughput, regression is when current < baseline
	change := (baseline - current) / baseline

	reg := Regression{
		Type:          RegressionThroughput,
		Component:     result.Component,
		BaselineValue: baseline,
		CurrentValue:  current,
		Change:        change,
		Threshold:     threshold,
	}

	if change > threshold {
		reg.Severity = SeverityError
		reg.Message = fmt.Sprintf("Throughput decreased by %.1f%% (threshold: %.1f%%)",
			change*100, threshold*100)
		result.Regressions = append(result.Regressions, reg)
		result.Pass = false
		if reg.Severity > result.MaxSeverity {
			result.MaxSeverity = reg.Severity
		}
	} else if change > threshold*d.config.WarnThresholdRatio {
		reg.Severity = SeverityWarning
		reg.Message = fmt.Sprintf("Throughput decreased by %.1f%% (approaching threshold: %.1f%%)",
			change*100, threshold*100)
		result.Warnings = append(result.Warnings, reg)
	}
}

// checkMemory checks for memory regression.
func (d *Detector) checkMemory(result *DetectionResult,
	baseline, current uint64, threshold float64) {

	if baseline == 0 {
		return
	}

	change := float64(current-baseline) / float64(baseline)

	reg := Regression{
		Type:          RegressionMemory,
		Component:     result.Component,
		BaselineValue: float64(baseline),
		CurrentValue:  float64(current),
		Change:        change,
		Threshold:     threshold,
	}

	if change > threshold {
		reg.Severity = SeverityError
		reg.Message = fmt.Sprintf("Memory increased by %.1f%% (threshold: %.1f%%)",
			change*100, threshold*100)
		result.Regressions = append(result.Regressions, reg)
		result.Pass = false
		if reg.Severity > result.MaxSeverity {
			result.MaxSeverity = reg.Severity
		}
	} else if change > threshold*d.config.WarnThresholdRatio {
		reg.Severity = SeverityWarning
		reg.Message = fmt.Sprintf("Memory increased by %.1f%% (approaching threshold: %.1f%%)",
			change*100, threshold*100)
		result.Warnings = append(result.Warnings, reg)
	}
}

// checkErrorRate checks for error rate regression.
func (d *Detector) checkErrorRate(result *DetectionResult,
	baseline, current float64, threshold float64) {

	// For error rate, use absolute change
	change := current - baseline

	reg := Regression{
		Type:          RegressionErrorRate,
		Component:     result.Component,
		BaselineValue: baseline,
		CurrentValue:  current,
		Change:        change,
		Threshold:     threshold,
	}

	if change > threshold {
		reg.Severity = SeverityCritical
		reg.Message = fmt.Sprintf("Error rate increased by %.2f%% (threshold: %.2f%%)",
			change*100, threshold*100)
		result.Regressions = append(result.Regressions, reg)
		result.Pass = false
		result.MaxSeverity = SeverityCritical
	} else if change > threshold*d.config.WarnThresholdRatio {
		reg.Severity = SeverityWarning
		reg.Message = fmt.Sprintf("Error rate increased by %.2f%% (approaching threshold: %.2f%%)",
			change*100, threshold*100)
		result.Warnings = append(result.Warnings, reg)
	}
}

// -----------------------------------------------------------------------------
// Statistical Detector
// -----------------------------------------------------------------------------

// StatisticalDetector uses statistical tests for more robust detection.
type StatisticalDetector struct {
	*Detector
	pValueThreshold float64
}

// NewStatisticalDetector creates a detector with statistical significance testing.
//
// Inputs:
//   - config: Detection configuration.
//   - pValueThreshold: P-value threshold for significance (e.g., 0.05).
//
// Outputs:
//   - *StatisticalDetector: The new detector. Never nil.
func NewStatisticalDetector(config *DetectorConfig, pValueThreshold float64) *StatisticalDetector {
	return &StatisticalDetector{
		Detector:        NewDetector(config),
		pValueThreshold: pValueThreshold,
	}
}

// DetectWithSamples detects regressions using raw samples for statistical testing.
//
// Inputs:
//   - baseline: The baseline to compare against.
//   - current: Current performance metrics.
//   - baselineSamples: Raw latency samples from baseline period.
//   - currentSamples: Raw latency samples from current period.
//
// Outputs:
//   - *DetectionResult: Detection results with statistical confidence.
//
// Thread Safety: Safe for concurrent use.
func (d *StatisticalDetector) DetectWithSamples(
	baseline *BaselineData,
	current *CurrentMetrics,
	baselineSamples, currentSamples []time.Duration,
) *DetectionResult {
	// First do basic detection
	result := d.Detect(baseline, current)

	// If we have samples, perform t-test
	if len(baselineSamples) >= 2 && len(currentSamples) >= 2 {
		// Check if difference is statistically significant
		tStat, pValue := welchTTest(baselineSamples, currentSamples)

		if pValue > d.pValueThreshold {
			// Not statistically significant - remove regressions
			originalRegressions := result.Regressions
			result.Regressions = nil
			result.Pass = true
			result.MaxSeverity = SeverityNone

			// Add warning about statistical insignificance
			if len(originalRegressions) > 0 {
				result.Warnings = append(result.Warnings, Regression{
					Type:     RegressionNone,
					Severity: SeverityWarning,
					Message: fmt.Sprintf(
						"Performance change not statistically significant (p=%.4f, t=%.4f)",
						pValue, tStat),
				})
			}
		}
	}

	return result
}

// welchTTest performs Welch's t-test.
func welchTTest(samples1, samples2 []time.Duration) (tStatistic, pValue float64) {
	if len(samples1) < 2 || len(samples2) < 2 {
		return 0, 1
	}

	mean1 := durationMean(samples1)
	mean2 := durationMean(samples2)

	var1 := durationVariance(samples1, mean1)
	var2 := durationVariance(samples2, mean2)

	n1 := float64(len(samples1))
	n2 := float64(len(samples2))

	se := math.Sqrt(var1/n1 + var2/n2)
	if se == 0 {
		return 0, 1
	}

	tStatistic = (mean1 - mean2) / se

	// Degrees of freedom (Welch-Satterthwaite)
	num := math.Pow(var1/n1+var2/n2, 2)
	denom := math.Pow(var1/n1, 2)/(n1-1) + math.Pow(var2/n2, 2)/(n2-1)
	if denom == 0 {
		return tStatistic, 1
	}
	df := num / denom

	// Approximate p-value
	if df >= 30 {
		pValue = 2 * normalCDF(-math.Abs(tStatistic))
	} else {
		pValue = 2 * normalCDF(-math.Abs(tStatistic)*math.Sqrt(df/(df-2+0.001)))
	}

	return tStatistic, pValue
}

func durationMean(samples []time.Duration) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s)
	}
	return sum / float64(len(samples))
}

func durationVariance(samples []time.Duration, mean float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		diff := float64(s) - mean
		sumSq += diff * diff
	}
	return sumSq / float64(len(samples))
}

func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt(2)))
}
