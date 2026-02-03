// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lsp

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for LSP operations.
var (
	tracer = otel.Tracer("aleutian.lsp")
	meter  = otel.Meter("aleutian.lsp")
)

// Metrics for LSP operations.
var (
	operationLatency metric.Float64Histogram
	operationTotal   metric.Int64Counter
	serverSpawns     metric.Int64Counter
	resultCount      metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		operationLatency, err = meter.Float64Histogram(
			"lsp_operation_duration_seconds",
			metric.WithDescription("Duration of LSP operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		operationTotal, err = meter.Int64Counter(
			"lsp_operation_total",
			metric.WithDescription("Total number of LSP operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		serverSpawns, err = meter.Int64Counter(
			"lsp_server_spawns_total",
			metric.WithDescription("Total number of LSP server spawns"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		resultCount, err = meter.Int64Histogram(
			"lsp_result_count",
			metric.WithDescription("Number of results returned by LSP operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startOperationSpan creates a span for an LSP operation.
func startOperationSpan(ctx context.Context, operation, language, filePath string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Operations."+operation,
		trace.WithAttributes(
			attribute.String("lsp.operation", operation),
			attribute.String("lsp.language", language),
			attribute.String("lsp.file_path", filePath),
		),
	)
}

// setOperationSpanResult sets the result attributes on an operation span.
func setOperationSpanResult(span trace.Span, resultCnt int, success bool) {
	span.SetAttributes(
		attribute.Int("lsp.result_count", resultCnt),
		attribute.Bool("lsp.success", success),
	)
}

// recordOperationMetrics records metrics for an LSP operation.
func recordOperationMetrics(ctx context.Context, operation, language string, duration time.Duration, resultCnt int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("language", language),
		attribute.Bool("success", success),
	)

	operationLatency.Record(ctx, duration.Seconds(), attrs)
	operationTotal.Add(ctx, 1, attrs)

	if success {
		resultCount.Record(ctx, int64(resultCnt), metric.WithAttributes(
			attribute.String("operation", operation),
		))
	}
}

// recordServerSpawn records a server spawn event.
func recordServerSpawn(ctx context.Context, language string, success bool) {
	if err := initMetrics(); err != nil {
		return
	}
	serverSpawns.Add(ctx, 1, metric.WithAttributes(
		attribute.String("language", language),
		attribute.Bool("success", success),
	))
}
