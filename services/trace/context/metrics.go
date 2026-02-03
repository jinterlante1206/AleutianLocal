// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for context assembly operations.
var (
	tracer = otel.Tracer("aleutian.context")
	meter  = otel.Meter("aleutian.context")
)

// Metrics for context assembly operations.
var (
	assembleLatency  metric.Float64Histogram
	assembleTotal    metric.Int64Counter
	tokensUsed       metric.Int64Histogram
	symbolsIncluded  metric.Int64Histogram
	entryPointsFound metric.Int64Histogram
	graphWalkSymbols metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		assembleLatency, err = meter.Float64Histogram(
			"context_assemble_duration_seconds",
			metric.WithDescription("Duration of context assembly operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		assembleTotal, err = meter.Int64Counter(
			"context_assemble_total",
			metric.WithDescription("Total number of context assembly operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		tokensUsed, err = meter.Int64Histogram(
			"context_tokens_used",
			metric.WithDescription("Number of tokens used in assembled context"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		symbolsIncluded, err = meter.Int64Histogram(
			"context_symbols_included",
			metric.WithDescription("Number of symbols included in assembled context"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		entryPointsFound, err = meter.Int64Histogram(
			"context_entry_points_found",
			metric.WithDescription("Number of entry points found from query"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		graphWalkSymbols, err = meter.Int64Histogram(
			"context_graph_walk_symbols",
			metric.WithDescription("Number of symbols found during graph walk"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startAssembleSpan creates a span for a context assembly operation.
func startAssembleSpan(ctx context.Context, queryLen, budget int) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Assembler.Assemble",
		trace.WithAttributes(
			attribute.Int("context.query_length", queryLen),
			attribute.Int("context.token_budget", budget),
		),
	)
}

// setAssembleSpanResult sets the result attributes on an assembly span.
func setAssembleSpanResult(span trace.Span, tokensUsed, symbolCount int, truncated bool) {
	span.SetAttributes(
		attribute.Int("context.tokens_used", tokensUsed),
		attribute.Int("context.symbols_included", symbolCount),
		attribute.Bool("context.truncated", truncated),
	)
}

// recordAssembleMetrics records metrics for a context assembly operation.
func recordAssembleMetrics(ctx context.Context, duration time.Duration, tokens, symbols int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.Bool("success", success))

	assembleLatency.Record(ctx, duration.Seconds(), attrs)
	assembleTotal.Add(ctx, 1, attrs)

	if success {
		tokensUsed.Record(ctx, int64(tokens))
		symbolsIncluded.Record(ctx, int64(symbols))
	}
}

// recordEntryPointsMetrics records the number of entry points found.
func recordEntryPointsMetrics(ctx context.Context, count int) {
	if err := initMetrics(); err != nil {
		return
	}
	entryPointsFound.Record(ctx, int64(count))
}

// recordGraphWalkMetrics records the number of symbols found during graph walk.
func recordGraphWalkMetrics(ctx context.Context, count int) {
	if err := initMetrics(); err != nil {
		return
	}
	graphWalkSymbols.Record(ctx, int64(count))
}
