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

	// Anchored synthesis metrics
	synthesisPromptsWithEvidence metric.Int64Counter
	evidenceRelevanceHistogram   metric.Float64Histogram

	// Structural claim metrics (CB-28d-5b)
	structuralClaimsNoCitation metric.Int64Counter

	// Post-synthesis metrics (CB-28d-5e)
	postSynthesisViolations metric.Int64Counter
	feedbackLoopsTriggered  metric.Int64Counter

	// Phantom symbol metrics (CB-28d-5g)
	phantomSymbolsTotal metric.Int64Counter

	// Semantic drift metrics (CB-28d-5h)
	semanticDriftTotal          metric.Int64Counter
	semanticDriftScoreHistogram metric.Float64Histogram

	// Attribute hallucination metrics (CB-28d-5i)
	attributeHallucinationsTotal metric.Int64Counter

	// Line number fabrication metrics (CB-28d-5j)
	lineFabricationsTotal metric.Int64Counter

	// Relationship hallucination metrics (CB-28d-5k)
	relationshipHallucinationsTotal metric.Int64Counter

	// Behavioral hallucination metrics (CB-28d-5l)
	behavioralHallucinationsTotal metric.Int64Counter

	// Quantitative hallucination metrics (CB-28d-5m)
	quantitativeHallucinationsTotal metric.Int64Counter

	// Fabricated code snippet metrics (CB-28d-5n)
	fabricatedCodeTotal metric.Int64Counter

	// API/library hallucination metrics (CB-28d-5o)
	apiHallucinationsTotal metric.Int64Counter

	// Temporal hallucination metrics (CB-28d-5p)
	temporalHallucinationsTotal metric.Int64Counter

	// Cross-context confusion metrics (CB-28d-5q)
	crossContextConfusionsTotal metric.Int64Counter

	// Confidence fabrication metrics (CB-28d-5r)
	confidenceFabricationsTotal metric.Int64Counter

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

		// Anchored synthesis metrics
		synthesisPromptsWithEvidence, err = meter.Int64Counter(
			"grounding_synthesis_prompts_with_evidence_total",
			metric.WithDescription("Total synthesis prompts built with tool evidence"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		evidenceRelevanceHistogram, err = meter.Float64Histogram(
			"grounding_synthesis_evidence_relevance_score",
			metric.WithDescription("Distribution of evidence relevance scores"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Structural claim metrics (CB-28d-5b)
		structuralClaimsNoCitation, err = meter.Int64Counter(
			"grounding_structural_claims_no_citation_total",
			metric.WithDescription("Structural claims detected without tool evidence"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Post-synthesis metrics (CB-28d-5e)
		postSynthesisViolations, err = meter.Int64Counter(
			"grounding_post_synthesis_violations_total",
			metric.WithDescription("Violations detected during post-synthesis verification"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		feedbackLoopsTriggered, err = meter.Int64Counter(
			"grounding_feedback_loops_triggered_total",
			metric.WithDescription("Feedback loops triggered after retry exhaustion"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Phantom symbol metrics (CB-28d-5g)
		phantomSymbolsTotal, err = meter.Int64Counter(
			"grounding_phantom_symbols_total",
			metric.WithDescription("Phantom symbol references detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Semantic drift metrics (CB-28d-5h)
		semanticDriftTotal, err = meter.Int64Counter(
			"grounding_semantic_drift_total",
			metric.WithDescription("Semantic drift violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		semanticDriftScoreHistogram, err = meter.Float64Histogram(
			"grounding_semantic_drift_score",
			metric.WithDescription("Distribution of semantic drift scores"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Attribute hallucination metrics (CB-28d-5i)
		attributeHallucinationsTotal, err = meter.Int64Counter(
			"grounding_attribute_hallucinations_total",
			metric.WithDescription("Attribute hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Line number fabrication metrics (CB-28d-5j)
		lineFabricationsTotal, err = meter.Int64Counter(
			"grounding_line_fabrications_total",
			metric.WithDescription("Line number fabrication violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Relationship hallucination metrics (CB-28d-5k)
		relationshipHallucinationsTotal, err = meter.Int64Counter(
			"grounding_relationship_hallucinations_total",
			metric.WithDescription("Relationship hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Behavioral hallucination metrics (CB-28d-5l)
		behavioralHallucinationsTotal, err = meter.Int64Counter(
			"grounding_behavioral_hallucinations_total",
			metric.WithDescription("Behavioral hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Quantitative hallucination metrics (CB-28d-5m)
		quantitativeHallucinationsTotal, err = meter.Int64Counter(
			"grounding_quantitative_hallucinations_total",
			metric.WithDescription("Quantitative hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Fabricated code snippet metrics (CB-28d-5n)
		fabricatedCodeTotal, err = meter.Int64Counter(
			"grounding_fabricated_code_total",
			metric.WithDescription("Fabricated code snippet violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// API/library hallucination metrics (CB-28d-5o)
		apiHallucinationsTotal, err = meter.Int64Counter(
			"grounding_api_hallucinations_total",
			metric.WithDescription("API/library hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Temporal hallucination metrics (CB-28d-5p)
		temporalHallucinationsTotal, err = meter.Int64Counter(
			"grounding_temporal_hallucinations_total",
			metric.WithDescription("Temporal hallucination violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Cross-context confusion metrics (CB-28d-5q)
		crossContextConfusionsTotal, err = meter.Int64Counter(
			"grounding_cross_context_confusions_total",
			metric.WithDescription("Cross-context confusion violations detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// Confidence fabrication metrics (CB-28d-5r)
		confidenceFabricationsTotal, err = meter.Int64Counter(
			"grounding_confidence_fabrications_total",
			metric.WithDescription("Confidence fabrication violations detected"),
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

// lastCircuitBreakerState tracks the previous state for delta calculation.
var lastCircuitBreakerState CircuitBreakerStateValue

// RecordCircuitBreakerState records the circuit breaker state change.
//
// Uses delta approach: subtracts old state value and adds new state value
// so the metric reflects current state, not cumulative.
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
	// Subtract old state, add new state to maintain current value
	oldState := lastCircuitBreakerState
	lastCircuitBreakerState = state
	circuitBreakerState.Add(ctx, int64(state)-int64(oldState))
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

// RecordSynthesisPromptWithEvidence records that a synthesis prompt was built with evidence.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - evidenceCount: Number of evidence items included.
//   - projectLang: The detected project language.
//
// Thread Safety: Safe for concurrent use.
func RecordSynthesisPromptWithEvidence(ctx context.Context, evidenceCount int, projectLang string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.Int("evidence_count", evidenceCount),
		attribute.String("project_language", projectLang),
	)

	synthesisPromptsWithEvidence.Add(ctx, 1, attrs)
}

// RecordEvidenceRelevanceScore records a single evidence relevance score.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - score: The relevance score (0.0-1.0).
//
// Thread Safety: Safe for concurrent use.
func RecordEvidenceRelevanceScore(ctx context.Context, score float64) {
	if err := initMetrics(); err != nil {
		return
	}
	evidenceRelevanceHistogram.Record(ctx, score)
}

// RecordStructuralClaimNoCitation records a structural claim without tool evidence.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of structural claim (e.g., "directory", "file_tree").
//
// Thread Safety: Safe for concurrent use.
func RecordStructuralClaimNoCitation(ctx context.Context, claimType string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
	)

	structuralClaimsNoCitation.Add(ctx, 1, attrs)
}

// RecordPostSynthesisViolation records a violation found during post-synthesis verification.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - violationType: Type of violation.
//   - severity: Violation severity.
//   - retryCount: Current retry attempt.
//
// Thread Safety: Safe for concurrent use.
func RecordPostSynthesisViolation(ctx context.Context, violationType ViolationType, severity Severity, retryCount int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("violation_type", string(violationType)),
		attribute.String("severity", string(severity)),
		attribute.Int("retry_count", retryCount),
	)

	postSynthesisViolations.Add(ctx, 1, attrs)
}

// RecordFeedbackLoopTriggered records when a feedback loop is triggered after retry exhaustion.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - questionCount: Number of feedback questions generated.
//
// Thread Safety: Safe for concurrent use.
func RecordFeedbackLoopTriggered(ctx context.Context, questionCount int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.Int("question_count", questionCount),
	)

	feedbackLoopsTriggered.Add(ctx, 1, attrs)
}

// RecordPhantomSymbol records a phantom symbol reference detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - symbolKind: Kind of symbol (function, type, etc.).
//   - hasFileContext: Whether the symbol had file association.
//
// Thread Safety: Safe for concurrent use.
func RecordPhantomSymbol(ctx context.Context, symbolKind string, hasFileContext bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("symbol_kind", symbolKind),
		attribute.Bool("has_file_context", hasFileContext),
	)

	phantomSymbolsTotal.Add(ctx, 1, attrs)
}

// RecordSemanticDrift records a semantic drift detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - driftScore: The calculated drift score (0.0-1.0).
//   - questionType: The detected question type (list, how, where, etc.).
//   - severity: The violation severity.
//
// Thread Safety: Safe for concurrent use.
func RecordSemanticDrift(ctx context.Context, driftScore float64, questionType string, severity Severity) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("question_type", questionType),
		attribute.String("severity", string(severity)),
	)

	semanticDriftTotal.Add(ctx, 1, attrs)
	semanticDriftScoreHistogram.Record(ctx, driftScore)
}

// RecordAttributeHallucination records an attribute hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of attribute claim (return_type, parameter_count, field_count, etc.).
//   - symbolKind: Kind of symbol (function, struct, interface).
//
// Thread Safety: Safe for concurrent use.
func RecordAttributeHallucination(ctx context.Context, claimType string, symbolKind string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
		attribute.String("symbol_kind", symbolKind),
	)

	attributeHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordLineFabrication records a line number fabrication detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - fabricationType: Type of fabrication (beyond_file_length, symbol_mismatch, invalid_range).
//   - filePath: The file path involved in the fabrication.
//
// Thread Safety: Safe for concurrent use.
func RecordLineFabrication(ctx context.Context, fabricationType string, filePath string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("fabrication_type", fabricationType),
		attribute.String("file_path", filePath),
	)

	lineFabricationsTotal.Add(ctx, 1, attrs)
}

// RecordRelationshipHallucination records a relationship hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - relationshipKind: Kind of relationship (import, call, implements).
//   - subject: The subject of the relationship claim.
//   - object: The object of the relationship claim.
//
// Thread Safety: Safe for concurrent use.
func RecordRelationshipHallucination(ctx context.Context, relationshipKind string, subject string, object string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("relationship_kind", relationshipKind),
		attribute.String("subject", subject),
		attribute.String("object", object),
	)

	relationshipHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordBehavioralHallucination records a behavioral hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - category: Category of behavioral claim (error_handling, validation, security).
//   - subject: The function/component the claim is about.
//   - claimedBehavior: The behavior that was claimed.
//
// Thread Safety: Safe for concurrent use.
func RecordBehavioralHallucination(ctx context.Context, category string, subject string, claimedBehavior string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("category", category),
		attribute.String("subject", subject),
		attribute.String("claimed_behavior", claimedBehavior),
	)

	behavioralHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordQuantitativeHallucination records a quantitative hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of quantitative claim (file_count, line_count, symbol_count).
//   - claimed: The claimed number.
//   - actual: The actual number from evidence.
//
// Thread Safety: Safe for concurrent use.
func RecordQuantitativeHallucination(ctx context.Context, claimType string, claimed int, actual int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
		attribute.Int("claimed", claimed),
		attribute.Int("actual", actual),
	)

	quantitativeHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordFabricatedCode records a fabricated code snippet detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - classification: Classification result (verbatim, modified, fabricated).
//   - similarity: The similarity score (0.0-1.0).
//   - snippetLen: Length of the code snippet in characters.
//
// Thread Safety: Safe for concurrent use.
func RecordFabricatedCode(ctx context.Context, classification string, similarity float64, snippetLen int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("classification", classification),
		attribute.Float64("similarity", similarity),
		attribute.Int("snippet_length", snippetLen),
	)

	fabricatedCodeTotal.Add(ctx, 1, attrs)
}

// RecordAPIHallucination records an API/library hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of claim (library_missing, library_confusion, api_not_found).
//   - claimedLibrary: The library that was claimed.
//   - actualLibrary: The library actually found in evidence (if confusion detected).
//
// Thread Safety: Safe for concurrent use.
func RecordAPIHallucination(ctx context.Context, claimType string, claimedLibrary string, actualLibrary string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
		attribute.String("claimed_library", claimedLibrary),
		attribute.String("actual_library", actualLibrary),
	)

	apiHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordTemporalHallucination records a temporal hallucination detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of temporal claim (recency, historical, version, reason).
//   - hasGitEvidence: Whether git evidence was available.
//
// Thread Safety: Safe for concurrent use.
func RecordTemporalHallucination(ctx context.Context, claimType string, hasGitEvidence bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
		attribute.Bool("has_git_evidence", hasGitEvidence),
	)

	temporalHallucinationsTotal.Add(ctx, 1, attrs)
}

// RecordCrossContextConfusion records a cross-context confusion detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - confusionType: Type of confusion (attribute_confusion, location_mismatch, ambiguous_reference).
//   - symbol: The symbol name that was confused.
//   - claimedLocation: The location claimed in the response.
//   - actualLocation: The actual location in evidence (if applicable).
//
// Thread Safety: Safe for concurrent use.
func RecordCrossContextConfusion(ctx context.Context, confusionType string, symbol string, claimedLocation string, actualLocation string) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("confusion_type", confusionType),
		attribute.String("symbol", symbol),
		attribute.String("claimed_location", claimedLocation),
		attribute.String("actual_location", actualLocation),
	)

	crossContextConfusionsTotal.Add(ctx, 1, attrs)
}

// RecordConfidenceFabrication records a confidence fabrication detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - claimType: Type of claim (absolute, negative_absolute, universal, exhaustive).
//   - evidenceStrength: Strength of evidence (absent, partial, strong).
//   - severity: The violation severity.
//
// Thread Safety: Safe for concurrent use.
func RecordConfidenceFabrication(ctx context.Context, claimType string, evidenceStrength string, severity Severity) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("claim_type", claimType),
		attribute.String("evidence_strength", evidenceStrength),
		attribute.String("severity", string(severity)),
	)

	confidenceFabricationsTotal.Add(ctx, 1, attrs)
}
