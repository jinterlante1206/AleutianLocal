// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package git

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for git operations.
var (
	tracer = otel.Tracer("aleutian.git")
	meter  = otel.Meter("aleutian.git")
)

// Metrics for git operations.
var (
	executeLatency    metric.Float64Histogram
	executeTotal      metric.Int64Counter
	invalidationTotal metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		executeLatency, err = meter.Float64Histogram(
			"git_execute_duration_seconds",
			metric.WithDescription("Duration of git command execution"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		executeTotal, err = meter.Int64Counter(
			"git_execute_total",
			metric.WithDescription("Total number of git commands executed"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		invalidationTotal, err = meter.Int64Counter(
			"git_invalidation_total",
			metric.WithDescription("Total number of cache invalidations triggered by git commands"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startExecuteSpan creates a span for a git command execution.
func startExecuteSpan(ctx context.Context, command string, workDir string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "GitAwareExecutor.Execute",
		trace.WithAttributes(
			attribute.String("git.command", command),
			attribute.String("git.work_dir", workDir),
		),
	)
}

// setExecuteSpanResult sets the result attributes on an execution span.
func setExecuteSpanResult(span trace.Span, exitCode int, invalidationType string) {
	span.SetAttributes(
		attribute.Int("git.exit_code", exitCode),
		attribute.String("git.invalidation_type", invalidationType),
	)
}

// recordExecuteMetrics records metrics for a git command execution.
func recordExecuteMetrics(ctx context.Context, command string, duration time.Duration, exitCode int, invalidationType string) {
	if err := initMetrics(); err != nil {
		return
	}

	success := exitCode == 0
	attrs := metric.WithAttributes(
		attribute.String("command", command),
		attribute.Bool("success", success),
	)

	executeLatency.Record(ctx, duration.Seconds(), attrs)
	executeTotal.Add(ctx, 1, attrs)

	if success && invalidationType != "none" {
		invalidationTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("type", invalidationType),
		))
	}
}
