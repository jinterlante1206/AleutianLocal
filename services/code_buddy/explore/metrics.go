// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for explore operations.
var (
	tracer = otel.Tracer("aleutian.explore")
	meter  = otel.Meter("aleutian.explore")
)

// Metrics for explore operations.
var (
	traceLatency metric.Float64Histogram
	traceTotal   metric.Int64Counter
	nodesVisited metric.Int64Histogram
	sinksFound   metric.Int64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		traceLatency, err = meter.Float64Histogram(
			"explore_trace_duration_seconds",
			metric.WithDescription("Duration of data flow trace operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		traceTotal, err = meter.Int64Counter(
			"explore_trace_total",
			metric.WithDescription("Total number of data flow traces"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		nodesVisited, err = meter.Int64Histogram(
			"explore_nodes_visited",
			metric.WithDescription("Number of nodes visited per trace"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		sinksFound, err = meter.Int64Histogram(
			"explore_sinks_found",
			metric.WithDescription("Number of sinks found per trace"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// startTraceSpan creates a span for a data flow trace operation.
func startTraceSpan(ctx context.Context, operation, symbolID string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "DataFlowTracer."+operation,
		trace.WithAttributes(
			attribute.String("explore.operation", operation),
			attribute.String("explore.symbol_id", symbolID),
		),
	)
}

// setTraceSpanResult sets the result attributes on a trace span.
func setTraceSpanResult(span trace.Span, nodesVisited, sinksFound int, success bool) {
	span.SetAttributes(
		attribute.Int("explore.nodes_visited", nodesVisited),
		attribute.Int("explore.sinks_found", sinksFound),
		attribute.Bool("explore.success", success),
	)
}

// recordTraceMetrics records metrics for a data flow trace operation.
func recordTraceMetrics(ctx context.Context, operation string, duration time.Duration, nodes, sinks int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("success", success),
	)

	traceLatency.Record(ctx, duration.Seconds(), attrs)
	traceTotal.Add(ctx, 1, attrs)
	nodesVisited.Record(ctx, int64(nodes))
	sinksFound.Record(ctx, int64(sinks))
}
