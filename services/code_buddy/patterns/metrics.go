// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for pattern detection operations.
var (
	tracer = otel.Tracer("aleutian.patterns")
	meter  = otel.Meter("aleutian.patterns")
)

// Metrics for pattern detection operations.
var (
	detectLatency  metric.Float64Histogram
	detectTotal    metric.Int64Counter
	patternsFound  metric.Int64Histogram
	patternsByType metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		detectLatency, err = meter.Float64Histogram(
			"patterns_detect_duration_seconds",
			metric.WithDescription("Duration of pattern detection operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		detectTotal, err = meter.Int64Counter(
			"patterns_detect_total",
			metric.WithDescription("Total number of pattern detection operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		patternsFound, err = meter.Int64Histogram(
			"patterns_found",
			metric.WithDescription("Number of patterns found per detection"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		patternsByType, err = meter.Int64Counter(
			"patterns_by_type_total",
			metric.WithDescription("Total patterns found by type"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startDetectSpan creates a span for a pattern detection operation.
func startDetectSpan(ctx context.Context, scope string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "PatternDetector.DetectPatterns",
		trace.WithAttributes(
			attribute.String("patterns.scope", scope),
		),
	)
}

// setDetectSpanResult sets the result attributes on a detection span.
func setDetectSpanResult(span trace.Span, patternCount int, success bool) {
	span.SetAttributes(
		attribute.Int("patterns.count", patternCount),
		attribute.Bool("patterns.success", success),
	)
}

// recordDetectMetrics records metrics for a pattern detection operation.
func recordDetectMetrics(ctx context.Context, duration time.Duration, patternCount int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.Bool("success", success),
	)

	detectLatency.Record(ctx, duration.Seconds(), attrs)
	detectTotal.Add(ctx, 1, attrs)
	patternsFound.Record(ctx, int64(patternCount))
}

// recordPatternByType records a pattern found by type.
func recordPatternByType(ctx context.Context, patternType string) {
	if err := initMetrics(); err != nil {
		return
	}
	patternsByType.Add(ctx, 1, metric.WithAttributes(
		attribute.String("pattern_type", patternType),
	))
}
