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
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for reason operations.
var (
	tracer = otel.Tracer("aleutian.reason")
	meter  = otel.Meter("aleutian.reason")
)

// Metrics for reason operations.
var (
	analysisLatency metric.Float64Histogram
	analysisTotal   metric.Int64Counter
	breakingChanges metric.Int64Counter
	callersAffected metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		analysisLatency, err = meter.Float64Histogram(
			"reason_analysis_duration_seconds",
			metric.WithDescription("Duration of reason analysis operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		analysisTotal, err = meter.Int64Counter(
			"reason_analysis_total",
			metric.WithDescription("Total number of reason analyses"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		breakingChanges, err = meter.Int64Counter(
			"reason_breaking_changes_total",
			metric.WithDescription("Total breaking changes detected"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		callersAffected, err = meter.Int64Histogram(
			"reason_callers_affected",
			metric.WithDescription("Number of callers affected by breaking changes"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startAnalysisSpan creates a span for a reason analysis operation.
func startAnalysisSpan(ctx context.Context, operation, targetID string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "BreakingChangeAnalyzer."+operation,
		trace.WithAttributes(
			attribute.String("reason.operation", operation),
			attribute.String("reason.target_id", targetID),
		),
	)
}

// setAnalysisSpanResult sets the result attributes on an analysis span.
func setAnalysisSpanResult(span trace.Span, isBreaking bool, callersAffected int, success bool) {
	span.SetAttributes(
		attribute.Bool("reason.is_breaking", isBreaking),
		attribute.Int("reason.callers_affected", callersAffected),
		attribute.Bool("reason.success", success),
	)
}

// recordAnalysisMetrics records metrics for a reason analysis operation.
func recordAnalysisMetrics(ctx context.Context, operation string, duration time.Duration, isBreaking bool, callers int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("success", success),
	)

	analysisLatency.Record(ctx, duration.Seconds(), attrs)
	analysisTotal.Add(ctx, 1, attrs)

	if isBreaking {
		breakingChanges.Add(ctx, 1)
		callersAffected.Record(ctx, int64(callers))
	}
}
