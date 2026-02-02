// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for grounding operations.
var (
	tracer = otel.Tracer("aleutian.grounding")
	meter  = otel.Meter("aleutian.grounding")
)

// Metrics for grounding operations.
var (
	// Check metrics
	checksTotal      metric.Int64Counter
	checkDuration    metric.Float64Histogram
	violationsTotal  metric.Int64Counter
	repromptsTotal   metric.Int64Counter
	rejectionsTotal  metric.Int64Counter
	warningFootnotes metric.Int64Counter

	// Multi-sample metrics
	consensusRateHistogram metric.Float64Histogram
	samplesAnalyzed        metric.Int64Counter

	// Circuit breaker metrics
	circuitBreakerState metric.Int64UpDownCounter
	retriesExhausted    metric.Int64Counter

	// Confidence metrics
	confidenceHistogram metric.Float64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		// Check metrics
		checksTotal, err = meter.Int64Counter(
			"grounding_checks_total",
			metric.WithDescription("Total grounding checks by checker and outcome"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		checkDuration, err = meter.Float64Histogram(
			"grounding_check_duration_seconds",
			metric.WithDescription("Grounding check duration by checker"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		violationsTotal, err = meter.Int64Counter(
			"grounding_violations_total",
			metric.WithDescription("Total violations by type, severity, and code"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		repromptsTotal, err = meter.Int64Counter(
			"grounding_reprompts_total",
			metric.WithDescription("Total re-prompt attempts after rejection"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		rejectionsTotal, err = meter.Int64Counter(
			"grounding_rejections_total",
			metric.WithDescription("Total rejected responses by reason"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		warningFootnotes, err = meter.Int64Counter(
			"grounding_warning_footnotes_total",
			metric.WithDescription("Total warning footnotes added to responses"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Multi-sample metrics
		consensusRateHistogram, err = meter.Float64Histogram(
			"grounding_consensus_rate",
			metric.WithDescription("Consensus rate distribution for multi-sample verification"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		samplesAnalyzed, err = meter.Int64Counter(
			"grounding_samples_analyzed_total",
			metric.WithDescription("Total samples analyzed in multi-sample verification"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Circuit breaker metrics
		circuitBreakerState, err = meter.Int64UpDownCounter(
			"grounding_circuit_breaker_state",
			metric.WithDescription("Current circuit breaker state (0=closed, 1=half-open, 2=open)"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		retriesExhausted, err = meter.Int64Counter(
			"grounding_retries_exhausted_total",
			metric.WithDescription("Times hallucination retry limit was exhausted"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Confidence metrics
		confidenceHistogram, err = meter.Float64Histogram(
			"grounding_confidence",
			metric.WithDescription("Response confidence score distribution"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// RecordCheck records metrics for a single checker execution.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - checker: Name of the checker.
//   - violationCount: Number of violations found.
//   - duration: Time taken for the check.
//
// Thread Safety: Safe for concurrent use.
func RecordCheck(ctx context.Context, checker string, violationCount int, duration time.Duration) {
	if err := initMetrics(); err != nil {
		return
	}

	outcome := "pass"
	if violationCount > 0 {
		outcome = "violations_found"
	}

	attrs := metric.WithAttributes(
		attribute.String("checker", checker),
		attribute.String("outcome", outcome),
	)

	checksTotal.Add(ctx, 1, attrs)
	checkDuration.Record(ctx, duration.Seconds(), attrs)
}

// RecordViolation records a single violation.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - v: The violation to record.
//
// Thread Safety: Safe for concurrent use.
func RecordViolation(ctx context.Context, v Violation) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("type", string(v.Type)),
		attribute.String("severity", string(v.Severity)),
		attribute.String("code", v.Code),
	)

	violationsTotal.Add(ctx, 1, attrs)
}

// RecordReprompt records a re-prompt attempt.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - attempt: Current attempt number.
//   - reason: Reason for the re-prompt.
//
// Thread Safety: Safe for concurrent use.
func RecordReprompt(ctx context.Context, attempt int, reason string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.Int("attempt", attempt),
		attribute.String("reason", reason),
	)

	repromptsTotal.Add(ctx, 1, attrs)
}

// RecordRejection records a rejected response.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - reason: Why the response was rejected.
//
// Thread Safety: Safe for concurrent use.
func RecordRejection(ctx context.Context, reason string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("reason", reason))
	rejectionsTotal.Add(ctx, 1, attrs)
}

// RecordWarningFootnote records that a warning footnote was added.
//
// Thread Safety: Safe for concurrent use.
func RecordWarningFootnote(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	warningFootnotes.Add(ctx, 1)
}

// RecordConsensusResult records multi-sample consensus metrics.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - result: The consensus result to record.
//
// Thread Safety: Safe for concurrent use.
func RecordConsensusResult(ctx context.Context, result *ConsensusResult) {
	if err := initMetrics(); err != nil {
		return
	}

	if result == nil {
		return
	}

	consensusRateHistogram.Record(ctx, result.ConsensusRate)
	samplesAnalyzed.Add(ctx, int64(result.TotalSamples))
}

// CircuitBreakerStateValue represents circuit breaker states as integers.
type CircuitBreakerStateValue int

const (
	CircuitBreakerClosed   CircuitBreakerStateValue = 0
	CircuitBreakerHalfOpen CircuitBreakerStateValue = 1
	CircuitBreakerOpen     CircuitBreakerStateValue = 2
)

// RecordCircuitBreakerState records the circuit breaker state change.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - state: New circuit breaker state.
//
// Thread Safety: Safe for concurrent use.
func RecordCircuitBreakerState(ctx context.Context, state CircuitBreakerStateValue) {
	if err := initMetrics(); err != nil {
		return
	}
	circuitBreakerState.Add(ctx, int64(state))
}

// RecordRetriesExhausted records when hallucination retries are exhausted.
//
// Thread Safety: Safe for concurrent use.
func RecordRetriesExhausted(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	retriesExhausted.Add(ctx, 1)
}

// RecordConfidence records the response confidence score.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - confidence: Confidence score (0.0-1.0).
//
// Thread Safety: Safe for concurrent use.
func RecordConfidence(ctx context.Context, confidence float64) {
	if err := initMetrics(); err != nil {
		return
	}
	confidenceHistogram.Record(ctx, confidence)
}

// ValidationStats contains statistics for a complete validation run.
type ValidationStats struct {
	ChecksRun       int
	ViolationsFound int
	CriticalCount   int
	WarningCount    int
	Grounded        bool
	Confidence      float64
	Duration        time.Duration
}

// RecordValidation records aggregate metrics for a validation run.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - stats: Validation statistics.
//
// Thread Safety: Safe for concurrent use.
func RecordValidation(ctx context.Context, stats ValidationStats) {
	if err := initMetrics(); err != nil {
		return
	}

	outcome := "grounded"
	if !stats.Grounded {
		outcome = "ungrounded"
	}

	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	checkDuration.Record(ctx, stats.Duration.Seconds(), attrs)
	confidenceHistogram.Record(ctx, stats.Confidence)
}

// StartGroundingSpan creates a span for grounding validation.
//
// Inputs:
//   - ctx: Parent context.
//   - operation: Operation name.
//   - responseLen: Length of response being validated.
//
// Outputs:
//   - context.Context: Context with span.
//   - trace.Span: The created span.
//
// Thread Safety: Safe for concurrent use.
func StartGroundingSpan(ctx context.Context, operation string, responseLen int) (context.Context, trace.Span) {
	return tracer.Start(ctx, operation,
		trace.WithAttributes(
			attribute.Int("grounding.response_length", responseLen),
		),
	)
}

// SetGroundingSpanResult sets result attributes on a grounding span.
//
// Inputs:
//   - span: The span to update.
//   - result: The validation result.
//
// Thread Safety: Safe for concurrent use.
func SetGroundingSpanResult(span trace.Span, result *Result) {
	if result == nil {
		return
	}

	span.SetAttributes(
		attribute.Bool("grounding.grounded", result.Grounded),
		attribute.Float64("grounding.confidence", result.Confidence),
		attribute.Int("grounding.checks_run", result.ChecksRun),
		attribute.Int("grounding.violations", len(result.Violations)),
		attribute.Int("grounding.critical_count", result.CriticalCount),
		attribute.Int("grounding.warning_count", result.WarningCount),
		attribute.Int64("grounding.duration_ms", result.CheckDuration.Milliseconds()),
	)
}

// AddCheckerEvent adds an event to the span for checker execution.
//
// Inputs:
//   - span: The span to add the event to.
//   - checker: Name of the checker.
//   - violationCount: Number of violations found.
//   - duration: Time taken for the check.
//
// Thread Safety: Safe for concurrent use.
func AddCheckerEvent(span trace.Span, checker string, violationCount int, duration time.Duration) {
	span.AddEvent("checker_executed", trace.WithAttributes(
		attribute.String("checker", checker),
		attribute.Int("violations", violationCount),
		attribute.Int64("duration_ms", duration.Milliseconds()),
	))
}

// AddViolationEvent adds an event to the span for a violation.
//
// Inputs:
//   - span: The span to add the event to.
//   - v: The violation.
//
// Thread Safety: Safe for concurrent use.
func AddViolationEvent(span trace.Span, v Violation) {
	span.AddEvent("violation_detected", trace.WithAttributes(
		attribute.String("type", string(v.Type)),
		attribute.String("severity", string(v.Severity)),
		attribute.String("code", v.Code),
		attribute.String("message", truncateForAttribute(v.Message, 200)),
	))
}

// AddConsensusEvent adds an event to the span for multi-sample consensus.
//
// Inputs:
//   - span: The span to add the event to.
//   - result: The consensus result.
//
// Thread Safety: Safe for concurrent use.
func AddConsensusEvent(span trace.Span, result *ConsensusResult) {
	if result == nil {
		return
	}

	span.AddEvent("consensus_analyzed", trace.WithAttributes(
		attribute.Int("total_samples", result.TotalSamples),
		attribute.Int("consistent_claims", len(result.ConsistentClaims)),
		attribute.Int("inconsistent_claims", len(result.InconsistentClaims)),
		attribute.Float64("consensus_rate", result.ConsensusRate),
	))
}

// truncateForAttribute truncates a string for use in span attributes.
func truncateForAttribute(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
