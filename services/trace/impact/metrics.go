// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package impact

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for impact analysis operations.
var (
	tracer = otel.Tracer("aleutian.impact")
	meter  = otel.Meter("aleutian.impact")
)

// Metrics for impact analysis operations.
var (
	analysisLatency metric.Float64Histogram
	analysisTotal   metric.Int64Counter
	riskScores      metric.Float64Histogram
	affectedCallers metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		analysisLatency, err = meter.Float64Histogram(
			"impact_analysis_duration_seconds",
			metric.WithDescription("Duration of impact analysis operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		analysisTotal, err = meter.Int64Counter(
			"impact_analysis_total",
			metric.WithDescription("Total number of impact analyses"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		riskScores, err = meter.Float64Histogram(
			"impact_risk_score",
			metric.WithDescription("Distribution of risk scores"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		affectedCallers, err = meter.Int64Histogram(
			"impact_affected_callers",
			metric.WithDescription("Number of callers affected by changes"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startAnalysisSpan creates a span for an impact analysis operation.
func startAnalysisSpan(ctx context.Context, targetID string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "ChangeImpactAnalyzer.AnalyzeImpact",
		trace.WithAttributes(
			attribute.String("impact.target_id", targetID),
		),
	)
}

// setAnalysisSpanResult sets the result attributes on an analysis span.
func setAnalysisSpanResult(span trace.Span, riskLevel string, riskScore float64, directCallers, totalImpact int, success bool) {
	span.SetAttributes(
		attribute.String("impact.risk_level", riskLevel),
		attribute.Float64("impact.risk_score", riskScore),
		attribute.Int("impact.direct_callers", directCallers),
		attribute.Int("impact.total_impact", totalImpact),
		attribute.Bool("impact.success", success),
	)
}

// recordAnalysisMetrics records metrics for an impact analysis.
func recordAnalysisMetrics(ctx context.Context, duration time.Duration, riskLevel string, riskScore float64, directCallers, totalImpact int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("risk_level", riskLevel),
		attribute.Bool("success", success),
	)

	analysisLatency.Record(ctx, duration.Seconds(), attrs)
	analysisTotal.Add(ctx, 1, attrs)
	riskScores.Record(ctx, riskScore)
	affectedCallers.Record(ctx, int64(directCallers+totalImpact))
}
