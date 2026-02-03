// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package index

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for index operations.
var (
	tracer = otel.Tracer("aleutian.index")
	meter  = otel.Meter("aleutian.index")
)

// Metrics for index operations.
var (
	operationLatency metric.Float64Histogram
	operationTotal   metric.Int64Counter
	indexSize        metric.Int64Gauge
	searchResults    metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		operationLatency, err = meter.Float64Histogram(
			"index_operation_duration_seconds",
			metric.WithDescription("Duration of index operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		operationTotal, err = meter.Int64Counter(
			"index_operation_total",
			metric.WithDescription("Total number of index operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		indexSize, err = meter.Int64Gauge(
			"index_size",
			metric.WithDescription("Current number of symbols in index"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		searchResults, err = meter.Int64Histogram(
			"index_search_results",
			metric.WithDescription("Number of results per search query"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startOperationSpan creates a span for an index operation.
func startOperationSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "SymbolIndex."+operation,
		trace.WithAttributes(
			attribute.String("index.operation", operation),
		),
	)
}

// setOperationSpanResult sets the result attributes on an operation span.
func setOperationSpanResult(span trace.Span, resultCount int, success bool) {
	span.SetAttributes(
		attribute.Int("index.result_count", resultCount),
		attribute.Bool("index.success", success),
	)
}

// recordOperationMetrics records metrics for an index operation.
func recordOperationMetrics(ctx context.Context, operation string, duration time.Duration, resultCount int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("success", success),
	)

	operationLatency.Record(ctx, duration.Seconds(), attrs)
	operationTotal.Add(ctx, 1, attrs)
}

// recordSearchResults records the number of search results.
func recordSearchResults(ctx context.Context, count int) {
	if err := initMetrics(); err != nil {
		return
	}
	searchResults.Record(ctx, int64(count))
}

// recordIndexSize records the current index size.
func recordIndexSize(ctx context.Context, size int) {
	if err := initMetrics(); err != nil {
		return
	}
	indexSize.Record(ctx, int64(size))
}
