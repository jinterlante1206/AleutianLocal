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
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Metrics contains pre-defined metrics for the Aleutian Trace service.
//
// Description:
//
//	Provides standard counters, histograms, and gauges for HTTP requests,
//	graph operations, Weaviate interactions, and memory operations.
//	All metrics use the "trace_" prefix for consistent naming.
//
// Thread Safety: Safe for concurrent use after creation.
type Metrics struct {
	// --- HTTP Metrics ---

	// HTTPRequestsTotal counts total HTTP requests by method, path, and status.
	HTTPRequestsTotal metric.Int64Counter

	// HTTPRequestDuration records HTTP request duration in seconds.
	HTTPRequestDuration metric.Float64Histogram

	// HTTPActiveRequests tracks currently active HTTP requests.
	HTTPActiveRequests metric.Int64UpDownCounter

	// --- Graph Metrics ---

	// GraphBuildsTotal counts total graph build operations by status.
	GraphBuildsTotal metric.Int64Counter

	// GraphBuildDuration records graph build duration in seconds.
	GraphBuildDuration metric.Float64Histogram

	// GraphQueriesTotal counts total graph queries by type and status.
	GraphQueriesTotal metric.Int64Counter

	// GraphQueryDuration records graph query duration in seconds.
	GraphQueryDuration metric.Float64Histogram

	// GraphSymbolsTotal counts total symbols indexed by language.
	GraphSymbolsTotal metric.Int64Counter

	// --- Weaviate Metrics ---

	// WeaviateRequestsTotal counts total Weaviate operations by type and status.
	WeaviateRequestsTotal metric.Int64Counter

	// WeaviateRequestDuration records Weaviate operation duration in seconds.
	WeaviateRequestDuration metric.Float64Histogram

	// WeaviateCircuitState tracks circuit breaker state (0=closed, 1=open, 2=half-open).
	WeaviateCircuitState metric.Int64ObservableGauge

	// --- Memory Metrics ---

	// MemoryRetrievalsTotal counts total memory retrieval operations by status.
	MemoryRetrievalsTotal metric.Int64Counter

	// MemoryStoresTotal counts total memory store operations by type.
	MemoryStoresTotal metric.Int64Counter

	// MemoryRetrievalDuration records memory retrieval duration in seconds.
	MemoryRetrievalDuration metric.Float64Histogram

	// --- MCTS Metrics ---

	// MCTSIterationsTotal counts total MCTS iterations.
	MCTSIterationsTotal metric.Int64Counter

	// MCTSIterationDuration records MCTS iteration duration in seconds.
	MCTSIterationDuration metric.Float64Histogram

	// MCTSNodesExplored counts total MCTS nodes explored.
	MCTSNodesExplored metric.Int64Counter

	// --- Error Metrics ---

	// ErrorsTotal counts total errors by type and component.
	ErrorsTotal metric.Int64Counter
}

// NewMetrics creates a new Metrics instance with all metrics registered.
//
// Description:
//
//	Registers all pre-defined metrics with the provided meter.
//	Returns an error if any metric registration fails.
//
// Inputs:
//
//	meter - The OTel meter to use for metric registration.
//
// Outputs:
//
//	*Metrics - The metrics instance with all counters and histograms initialized.
//	error - Non-nil if metric registration fails.
//
// Example:
//
//	meter := otel.Meter("trace")
//	metrics, err := telemetry.NewMetrics(meter)
//	if err != nil {
//	    return fmt.Errorf("create metrics: %w", err)
//	}
//	metrics.HTTPRequestsTotal.Add(ctx, 1, ...)
//
// Thread Safety: Safe for concurrent use after creation.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{}
	var err error

	// --- HTTP Metrics ---
	m.HTTPRequestsTotal, err = meter.Int64Counter(
		"trace_http_requests_total",
		metric.WithDescription("Total HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http_requests_total: %w", err)
	}

	m.HTTPRequestDuration, err = meter.Float64Histogram(
		"trace_http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("create http_request_duration: %w", err)
	}

	m.HTTPActiveRequests, err = meter.Int64UpDownCounter(
		"trace_http_active_requests",
		metric.WithDescription("Currently active HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http_active_requests: %w", err)
	}

	// --- Graph Metrics ---
	m.GraphBuildsTotal, err = meter.Int64Counter(
		"trace_graph_builds_total",
		metric.WithDescription("Total graph build operations"),
		metric.WithUnit("{build}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create graph_builds_total: %w", err)
	}

	m.GraphBuildDuration, err = meter.Float64Histogram(
		"trace_graph_build_duration_seconds",
		metric.WithDescription("Graph build duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2, 5, 10, 30, 60, 120),
	)
	if err != nil {
		return nil, fmt.Errorf("create graph_build_duration: %w", err)
	}

	m.GraphQueriesTotal, err = meter.Int64Counter(
		"trace_graph_queries_total",
		metric.WithDescription("Total graph queries"),
		metric.WithUnit("{query}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create graph_queries_total: %w", err)
	}

	m.GraphQueryDuration, err = meter.Float64Histogram(
		"trace_graph_query_duration_seconds",
		metric.WithDescription("Graph query duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("create graph_query_duration: %w", err)
	}

	m.GraphSymbolsTotal, err = meter.Int64Counter(
		"trace_graph_symbols_total",
		metric.WithDescription("Total symbols indexed"),
		metric.WithUnit("{symbol}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create graph_symbols_total: %w", err)
	}

	// --- Weaviate Metrics ---
	m.WeaviateRequestsTotal, err = meter.Int64Counter(
		"trace_weaviate_requests_total",
		metric.WithDescription("Total Weaviate operations"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create weaviate_requests_total: %w", err)
	}

	m.WeaviateRequestDuration, err = meter.Float64Histogram(
		"trace_weaviate_request_duration_seconds",
		metric.WithDescription("Weaviate operation duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("create weaviate_request_duration: %w", err)
	}

	// Note: WeaviateCircuitState requires a callback registration, handled separately

	// --- Memory Metrics ---
	m.MemoryRetrievalsTotal, err = meter.Int64Counter(
		"trace_memory_retrievals_total",
		metric.WithDescription("Total memory retrieval operations"),
		metric.WithUnit("{retrieval}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create memory_retrievals_total: %w", err)
	}

	m.MemoryStoresTotal, err = meter.Int64Counter(
		"trace_memory_stores_total",
		metric.WithDescription("Total memory store operations"),
		metric.WithUnit("{store}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create memory_stores_total: %w", err)
	}

	m.MemoryRetrievalDuration, err = meter.Float64Histogram(
		"trace_memory_retrieval_duration_seconds",
		metric.WithDescription("Memory retrieval duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("create memory_retrieval_duration: %w", err)
	}

	// --- MCTS Metrics ---
	m.MCTSIterationsTotal, err = meter.Int64Counter(
		"trace_mcts_iterations_total",
		metric.WithDescription("Total MCTS iterations"),
		metric.WithUnit("{iteration}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create mcts_iterations_total: %w", err)
	}

	m.MCTSIterationDuration, err = meter.Float64Histogram(
		"trace_mcts_iteration_duration_seconds",
		metric.WithDescription("MCTS iteration duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("create mcts_iteration_duration: %w", err)
	}

	m.MCTSNodesExplored, err = meter.Int64Counter(
		"trace_mcts_nodes_explored_total",
		metric.WithDescription("Total MCTS nodes explored"),
		metric.WithUnit("{node}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create mcts_nodes_explored: %w", err)
	}

	// --- Error Metrics ---
	m.ErrorsTotal, err = meter.Int64Counter(
		"trace_errors_total",
		metric.WithDescription("Total errors by type and component"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create errors_total: %w", err)
	}

	return m, nil
}

// RegisterWeaviateCircuitState registers a callback for the Weaviate circuit state gauge.
//
// Description:
//
//	Sets up an observable gauge that reports the current circuit breaker state.
//	The callback is invoked each time metrics are scraped.
//
// Inputs:
//
//	meter - The OTel meter to use for registration.
//	stateFunc - A function that returns the current circuit state (0=closed, 1=open, 2=half-open).
//
// Outputs:
//
//	metric.Registration - Registration handle for cleanup.
//	error - Non-nil if registration fails.
func (m *Metrics) RegisterWeaviateCircuitState(meter metric.Meter, stateFunc func() int64) (metric.Registration, error) {
	var err error
	m.WeaviateCircuitState, err = meter.Int64ObservableGauge(
		"trace_weaviate_circuit_state",
		metric.WithDescription("Weaviate circuit breaker state (0=closed, 1=open, 2=half-open)"),
		metric.WithUnit("{state}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create weaviate_circuit_state: %w", err)
	}

	return meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(m.WeaviateCircuitState, stateFunc())
		return nil
	}, m.WeaviateCircuitState)
}
