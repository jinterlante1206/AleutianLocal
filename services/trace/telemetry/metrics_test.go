// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func TestNewMetrics(t *testing.T) {
	// Initialize telemetry with prometheus exporter
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	// Verify all metrics are created
	if metrics.HTTPRequestsTotal == nil {
		t.Error("HTTPRequestsTotal is nil")
	}
	if metrics.HTTPRequestDuration == nil {
		t.Error("HTTPRequestDuration is nil")
	}
	if metrics.HTTPActiveRequests == nil {
		t.Error("HTTPActiveRequests is nil")
	}
	if metrics.GraphBuildsTotal == nil {
		t.Error("GraphBuildsTotal is nil")
	}
	if metrics.GraphBuildDuration == nil {
		t.Error("GraphBuildDuration is nil")
	}
	if metrics.GraphQueriesTotal == nil {
		t.Error("GraphQueriesTotal is nil")
	}
	if metrics.GraphQueryDuration == nil {
		t.Error("GraphQueryDuration is nil")
	}
	if metrics.WeaviateRequestsTotal == nil {
		t.Error("WeaviateRequestsTotal is nil")
	}
	if metrics.WeaviateRequestDuration == nil {
		t.Error("WeaviateRequestDuration is nil")
	}
	if metrics.MemoryRetrievalsTotal == nil {
		t.Error("MemoryRetrievalsTotal is nil")
	}
	if metrics.MemoryStoresTotal == nil {
		t.Error("MemoryStoresTotal is nil")
	}
	if metrics.MCTSIterationsTotal == nil {
		t.Error("MCTSIterationsTotal is nil")
	}
	if metrics.ErrorsTotal == nil {
		t.Error("ErrorsTotal is nil")
	}
}

func TestMetrics_RecordHTTPMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_http_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	ctx := context.Background()
	attrs := metric.WithAttributes(
		attribute.String("method", "GET"),
		attribute.String("path", "/api/query"),
		attribute.Int("status", 200),
	)

	// Should not panic
	metrics.HTTPRequestsTotal.Add(ctx, 1, attrs)
	metrics.HTTPRequestDuration.Record(ctx, 0.123, attrs)
	metrics.HTTPActiveRequests.Add(ctx, 1)
	metrics.HTTPActiveRequests.Add(ctx, -1)
}

func TestMetrics_RecordGraphMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_graph_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	ctx := context.Background()

	// Graph builds
	metrics.GraphBuildsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", "success"),
	))
	metrics.GraphBuildDuration.Record(ctx, 5.5)

	// Graph queries
	metrics.GraphQueriesTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("query_type", "find_callers"),
		attribute.String("status", "success"),
	))
	metrics.GraphQueryDuration.Record(ctx, 0.012, metric.WithAttributes(
		attribute.String("query_type", "find_callers"),
	))

	// Symbols
	metrics.GraphSymbolsTotal.Add(ctx, 150, metric.WithAttributes(
		attribute.String("language", "go"),
	))
}

func TestMetrics_RecordWeaviateMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_weaviate_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	ctx := context.Background()

	metrics.WeaviateRequestsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", "search"),
		attribute.String("status", "success"),
	))
	metrics.WeaviateRequestDuration.Record(ctx, 0.045, metric.WithAttributes(
		attribute.String("operation", "search"),
	))
}

func TestMetrics_RegisterWeaviateCircuitState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_circuit_state")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	// Register circuit state callback
	currentState := int64(0) // closed
	reg, err := metrics.RegisterWeaviateCircuitState(meter, func() int64 {
		return currentState
	})
	if err != nil {
		t.Fatalf("RegisterWeaviateCircuitState() error = %v", err)
	}
	defer reg.Unregister()

	// Verify gauge was created
	if metrics.WeaviateCircuitState == nil {
		t.Error("WeaviateCircuitState is nil after registration")
	}
}

func TestMetrics_RecordMCTSMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_mcts_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	ctx := context.Background()

	metrics.MCTSIterationsTotal.Add(ctx, 100)
	metrics.MCTSIterationDuration.Record(ctx, 0.005)
	metrics.MCTSNodesExplored.Add(ctx, 500)
}

func TestMetrics_RecordErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_errors")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	ctx := context.Background()

	metrics.ErrorsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", "validation"),
		attribute.String("component", "parser"),
	))
}
