// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for graph operations.
var (
	tracer = otel.Tracer("aleutian.graph")
	meter  = otel.Meter("aleutian.graph")
)

// Metrics for graph building operations.
var (
	buildLatency metric.Float64Histogram
	buildTotal   metric.Int64Counter
	nodesCreated metric.Int64Histogram
	edgesCreated metric.Int64Histogram
	queryLatency metric.Float64Histogram

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		buildLatency, err = meter.Float64Histogram(
			"graph_build_duration_seconds",
			metric.WithDescription("Duration of graph build operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		buildTotal, err = meter.Int64Counter(
			"graph_build_total",
			metric.WithDescription("Total number of graph build operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		nodesCreated, err = meter.Int64Histogram(
			"graph_nodes_created",
			metric.WithDescription("Number of nodes created per build"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		edgesCreated, err = meter.Int64Histogram(
			"graph_edges_created",
			metric.WithDescription("Number of edges created per build"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		queryLatency, err = meter.Float64Histogram(
			"graph_query_duration_seconds",
			metric.WithDescription("Duration of graph query operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// recordBuildMetrics records metrics for a build operation.
func recordBuildMetrics(ctx context.Context, duration time.Duration, nodeCount, edgeCount int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.Bool("success", success))

	buildLatency.Record(ctx, duration.Seconds(), attrs)
	buildTotal.Add(ctx, 1, attrs)

	if success {
		nodesCreated.Record(ctx, int64(nodeCount))
		edgesCreated.Record(ctx, int64(edgeCount))
	}
}

// recordQueryMetrics records metrics for a query operation.
func recordQueryMetrics(ctx context.Context, queryType string, duration time.Duration, resultCount int) {
	if err := initMetrics(); err != nil {
		return
	}

	queryLatency.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("query_type", queryType)),
	)
}

// startBuildSpan creates a span for a build operation.
func startBuildSpan(ctx context.Context, fileCount int) (context.Context, trace.Span) {
	return tracer.Start(ctx, "GraphBuilder.Build",
		trace.WithAttributes(
			attribute.Int("graph.file_count", fileCount),
		),
	)
}

// setBuildSpanResult sets the result attributes on a build span.
func setBuildSpanResult(span trace.Span, nodeCount, edgeCount int, incomplete bool) {
	span.SetAttributes(
		attribute.Int("graph.node_count", nodeCount),
		attribute.Int("graph.edge_count", edgeCount),
		attribute.Bool("graph.incomplete", incomplete),
	)
}

// startQuerySpan creates a span for a query operation.
func startQuerySpan(ctx context.Context, queryType, symbolID string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Graph."+queryType,
		trace.WithAttributes(
			attribute.String("graph.query_type", queryType),
			attribute.String("graph.symbol_id", symbolID),
		),
	)
}
