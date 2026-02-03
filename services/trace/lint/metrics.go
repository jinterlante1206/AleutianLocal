// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for lint operations.
var (
	tracer = otel.Tracer("aleutian.lint")
	meter  = otel.Meter("aleutian.lint")
)

// Metrics for lint operations.
var (
	lintLatency   metric.Float64Histogram
	lintTotal     metric.Int64Counter
	issuesFound   metric.Int64Histogram
	errorsFound   metric.Int64Counter
	warningsFound metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		lintLatency, err = meter.Float64Histogram(
			"lint_duration_seconds",
			metric.WithDescription("Duration of lint operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		lintTotal, err = meter.Int64Counter(
			"lint_total",
			metric.WithDescription("Total number of lint operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		issuesFound, err = meter.Int64Histogram(
			"lint_issues_found",
			metric.WithDescription("Number of issues found per lint operation"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		errorsFound, err = meter.Int64Counter(
			"lint_errors_found_total",
			metric.WithDescription("Total number of lint errors found"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		warningsFound, err = meter.Int64Counter(
			"lint_warnings_found_total",
			metric.WithDescription("Total number of lint warnings found"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startLintSpan creates a span for a lint operation.
func startLintSpan(ctx context.Context, language, filePath string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "LintRunner.Lint",
		trace.WithAttributes(
			attribute.String("lint.language", language),
			attribute.String("lint.file_path", filePath),
		),
	)
}

// setLintSpanResult sets the result attributes on a lint span.
func setLintSpanResult(span trace.Span, errorCount, warningCount int, linterAvailable bool) {
	span.SetAttributes(
		attribute.Int("lint.error_count", errorCount),
		attribute.Int("lint.warning_count", warningCount),
		attribute.Bool("lint.linter_available", linterAvailable),
	)
}

// recordLintMetrics records metrics for a lint operation.
func recordLintMetrics(ctx context.Context, language string, duration time.Duration, errorCount, warningCount int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("language", language),
		attribute.Bool("success", success),
	)

	lintLatency.Record(ctx, duration.Seconds(), attrs)
	lintTotal.Add(ctx, 1, attrs)

	if success {
		issuesFound.Record(ctx, int64(errorCount+warningCount), metric.WithAttributes(
			attribute.String("language", language),
		))
		errorsFound.Add(ctx, int64(errorCount), metric.WithAttributes(
			attribute.String("language", language),
		))
		warningsFound.Add(ctx, int64(warningCount), metric.WithAttributes(
			attribute.String("language", language),
		))
	}
}
