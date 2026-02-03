// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ast

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for AST parsing.
var (
	tracer = otel.Tracer("aleutian.ast")
	meter  = otel.Meter("aleutian.ast")
)

// Metrics for AST parsing operations.
var (
	parseLatency     metric.Float64Histogram
	parseTotal       metric.Int64Counter
	symbolsExtracted metric.Int64Histogram
	parseErrors      metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		parseLatency, err = meter.Float64Histogram(
			"ast_parse_duration_seconds",
			metric.WithDescription("Duration of AST parsing operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		parseTotal, err = meter.Int64Counter(
			"ast_parse_total",
			metric.WithDescription("Total number of parse operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		symbolsExtracted, err = meter.Int64Histogram(
			"ast_symbols_extracted",
			metric.WithDescription("Number of symbols extracted per parse"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		parseErrors, err = meter.Int64Counter(
			"ast_parse_errors_total",
			metric.WithDescription("Total number of parse errors"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// recordParseMetrics records metrics for a parse operation.
//
// Parameters:
//   - ctx: Context for metric recording
//   - language: Language being parsed (e.g., "go", "python")
//   - duration: How long the parse took
//   - symbolCount: Number of symbols extracted
//   - success: Whether the parse succeeded
func recordParseMetrics(ctx context.Context, language string, duration time.Duration, symbolCount int, success bool) {
	if err := initMetrics(); err != nil {
		return // Silently skip if metrics init failed
	}

	attrs := metric.WithAttributes(
		attribute.String("language", language),
		attribute.Bool("success", success),
	)

	parseLatency.Record(ctx, duration.Seconds(), attrs)
	parseTotal.Add(ctx, 1, attrs)

	if success {
		symbolsExtracted.Record(ctx, int64(symbolCount),
			metric.WithAttributes(attribute.String("language", language)),
		)
	} else {
		parseErrors.Add(ctx, 1,
			metric.WithAttributes(attribute.String("language", language)),
		)
	}
}

// startParseSpan creates a span for a parse operation.
//
// Parameters:
//   - ctx: Parent context
//   - language: Language being parsed
//   - filePath: Path to the file being parsed
//   - contentSize: Size of the content in bytes
//
// Returns:
//   - ctx: Context with span
//   - span: The created span (caller must call span.End())
func startParseSpan(ctx context.Context, language, filePath string, contentSize int) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Parser.Parse",
		trace.WithAttributes(
			attribute.String("ast.language", language),
			attribute.String("ast.file", filePath),
			attribute.Int("ast.content_size", contentSize),
		),
	)
}

// setParseSpanResult sets the result attributes on a parse span.
func setParseSpanResult(span trace.Span, symbolCount int, errorCount int) {
	span.SetAttributes(
		attribute.Int("ast.symbol_count", symbolCount),
		attribute.Int("ast.error_count", errorCount),
	)
}
