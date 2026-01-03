// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package observability provides metrics and instrumentation for the orchestrator.
//
// # Description
//
// This package implements Prometheus metrics for monitoring streaming chat
// operations. Metrics include:
//   - Request counters (by endpoint, status, error type)
//   - Token usage (input/output tokens by model)
//   - Latency histograms (time to first token, total duration)
//   - Active stream gauges
//
// # Integration
//
// Metrics are exposed via /metrics endpoint. Use with Prometheus + Grafana
// for dashboards and alerting.
//
// # Thread Safety
//
// All metric operations are thread-safe via Prometheus's internal locking.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// =============================================================================
// Metric Definitions
// =============================================================================

// Namespace for all metrics
const metricsNamespace = "aleutian"

// Subsystem for streaming metrics
const streamingSubsystem = "streaming"

// StreamingMetrics holds all Prometheus metrics for streaming chat operations.
//
// # Description
//
// Provides counters, histograms, and gauges for monitoring streaming performance
// and resource usage. Initialize once at startup via NewStreamingMetrics().
//
// # Fields
//
//   - RequestsTotal: Counter of streaming requests by endpoint and status
//   - TokensTotal: Counter of tokens processed (input/output by model)
//   - TimeToFirstTokenSeconds: Histogram of time to first token
//   - StreamDurationSeconds: Histogram of total stream duration
//   - ActiveStreams: Gauge of currently active streams
//   - ErrorsTotal: Counter of errors by type and endpoint
//
// # Thread Safety
//
// All operations are thread-safe.
type StreamingMetrics struct {
	// RequestsTotal counts streaming requests by endpoint and status.
	// Labels: endpoint (direct_stream, rag_stream), status (success, error)
	RequestsTotal *prometheus.CounterVec

	// TokensTotal counts tokens processed by direction and model.
	// Labels: direction (input, output), model (claude-3-5-sonnet, etc.)
	TokensTotal *prometheus.CounterVec

	// TimeToFirstTokenSeconds measures latency to first token.
	// Labels: endpoint (direct_stream, rag_stream)
	TimeToFirstTokenSeconds *prometheus.HistogramVec

	// StreamDurationSeconds measures total stream duration.
	// Labels: endpoint (direct_stream, rag_stream), status (success, error)
	StreamDurationSeconds *prometheus.HistogramVec

	// ActiveStreams tracks currently active streaming connections.
	// Labels: endpoint (direct_stream, rag_stream)
	ActiveStreams *prometheus.GaugeVec

	// ErrorsTotal counts errors by type and endpoint.
	// Labels: endpoint, error_code (policy_violation, llm_error, timeout, etc.)
	ErrorsTotal *prometheus.CounterVec

	// KeepAlivesTotal counts keepalive pings sent.
	// Labels: endpoint
	KeepAlivesTotal *prometheus.CounterVec

	// ClientDisconnectsTotal counts client disconnections during streaming.
	// Labels: endpoint
	ClientDisconnectsTotal *prometheus.CounterVec
}

// DefaultMetrics is the singleton instance of StreamingMetrics.
// Initialized by InitMetrics().
var DefaultMetrics *StreamingMetrics

// InitMetrics initializes the default metrics instance.
//
// # Description
//
// Creates and registers all Prometheus metrics. Should be called once
// at application startup, after Prometheus registry is available.
//
// # Outputs
//
//   - *StreamingMetrics: The initialized metrics instance.
//
// # Examples
//
//	func main() {
//	    observability.InitMetrics()
//	    // ... start server ...
//	}
//
// # Limitations
//
//   - Panics if called twice (duplicate registration).
//
// # Assumptions
//
//   - Prometheus default registry is available.
func InitMetrics() *StreamingMetrics {
	DefaultMetrics = &StreamingMetrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "requests_total",
				Help:      "Total number of streaming requests by endpoint and status",
			},
			[]string{"endpoint", "status"},
		),

		TokensTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "tokens_total",
				Help:      "Total tokens processed by direction and model",
			},
			[]string{"direction", "model"},
		),

		TimeToFirstTokenSeconds: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "time_to_first_token_seconds",
				Help:      "Time from request to first token in seconds",
				Buckets:   []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
			},
			[]string{"endpoint"},
		),

		StreamDurationSeconds: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "stream_duration_seconds",
				Help:      "Total stream duration in seconds",
				Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"endpoint", "status"},
		),

		ActiveStreams: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "active_streams",
				Help:      "Number of currently active streaming connections",
			},
			[]string{"endpoint"},
		),

		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "errors_total",
				Help:      "Total streaming errors by type and endpoint",
			},
			[]string{"endpoint", "error_code"},
		),

		KeepAlivesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "keepalives_total",
				Help:      "Total keepalive pings sent",
			},
			[]string{"endpoint"},
		),

		ClientDisconnectsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: streamingSubsystem,
				Name:      "client_disconnects_total",
				Help:      "Total client disconnections during streaming",
			},
			[]string{"endpoint"},
		),
	}

	return DefaultMetrics
}

// =============================================================================
// Error Codes
// =============================================================================

// ErrorCode represents a categorized error type for metrics.
type ErrorCode string

const (
	// ErrorCodePolicyViolation indicates blocked due to policy scan.
	ErrorCodePolicyViolation ErrorCode = "policy_violation"

	// ErrorCodeValidation indicates request validation failure.
	ErrorCodeValidation ErrorCode = "validation"

	// ErrorCodeLLMError indicates LLM API failure.
	ErrorCodeLLMError ErrorCode = "llm_error"

	// ErrorCodeTimeout indicates operation timeout.
	ErrorCodeTimeout ErrorCode = "timeout"

	// ErrorCodeRAGError indicates RAG retrieval failure.
	ErrorCodeRAGError ErrorCode = "rag_error"

	// ErrorCodeInternal indicates internal server error.
	ErrorCodeInternal ErrorCode = "internal"

	// ErrorCodeClientDisconnect indicates client disconnected.
	ErrorCodeClientDisconnect ErrorCode = "client_disconnect"
)

// =============================================================================
// Endpoint Names
// =============================================================================

// Endpoint represents a streaming endpoint for metrics labeling.
type Endpoint string

const (
	// EndpointDirectStream is the direct chat streaming endpoint.
	EndpointDirectStream Endpoint = "direct_stream"

	// EndpointRAGStream is the RAG chat streaming endpoint.
	EndpointRAGStream Endpoint = "rag_stream"
)

// =============================================================================
// Helper Methods
// =============================================================================

// RecordRequest records a completed streaming request.
//
// # Inputs
//
//   - endpoint: The endpoint that handled the request.
//   - success: Whether the request completed successfully.
func (m *StreamingMetrics) RecordRequest(endpoint Endpoint, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	m.RequestsTotal.WithLabelValues(string(endpoint), status).Inc()
}

// RecordError records a streaming error.
//
// # Inputs
//
//   - endpoint: The endpoint where the error occurred.
//   - code: The error type code.
func (m *StreamingMetrics) RecordError(endpoint Endpoint, code ErrorCode) {
	m.ErrorsTotal.WithLabelValues(string(endpoint), string(code)).Inc()
}

// RecordTokens records token usage.
//
// # Inputs
//
//   - inputTokens: Number of input tokens.
//   - outputTokens: Number of output tokens.
//   - model: The model used.
func (m *StreamingMetrics) RecordTokens(inputTokens, outputTokens int, model string) {
	m.TokensTotal.WithLabelValues("input", model).Add(float64(inputTokens))
	m.TokensTotal.WithLabelValues("output", model).Add(float64(outputTokens))
}

// StreamStarted increments the active streams gauge.
//
// # Inputs
//
//   - endpoint: The endpoint handling the stream.
func (m *StreamingMetrics) StreamStarted(endpoint Endpoint) {
	m.ActiveStreams.WithLabelValues(string(endpoint)).Inc()
}

// StreamEnded decrements the active streams gauge.
//
// # Inputs
//
//   - endpoint: The endpoint that handled the stream.
func (m *StreamingMetrics) StreamEnded(endpoint Endpoint) {
	m.ActiveStreams.WithLabelValues(string(endpoint)).Dec()
}

// RecordTimeToFirstToken records the time to first token latency.
//
// # Inputs
//
//   - endpoint: The endpoint handling the stream.
//   - seconds: Time to first token in seconds.
func (m *StreamingMetrics) RecordTimeToFirstToken(endpoint Endpoint, seconds float64) {
	m.TimeToFirstTokenSeconds.WithLabelValues(string(endpoint)).Observe(seconds)
}

// RecordStreamDuration records the total stream duration.
//
// # Inputs
//
//   - endpoint: The endpoint that handled the stream.
//   - seconds: Total duration in seconds.
//   - success: Whether the stream completed successfully.
func (m *StreamingMetrics) RecordStreamDuration(endpoint Endpoint, seconds float64, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	m.StreamDurationSeconds.WithLabelValues(string(endpoint), status).Observe(seconds)
}

// RecordKeepAlive increments the keepalive counter.
//
// # Inputs
//
//   - endpoint: The endpoint that sent the keepalive.
func (m *StreamingMetrics) RecordKeepAlive(endpoint Endpoint) {
	m.KeepAlivesTotal.WithLabelValues(string(endpoint)).Inc()
}

// RecordClientDisconnect increments the client disconnect counter.
//
// # Inputs
//
//   - endpoint: The endpoint where disconnect occurred.
func (m *StreamingMetrics) RecordClientDisconnect(endpoint Endpoint) {
	m.ClientDisconnectsTotal.WithLabelValues(string(endpoint)).Inc()
}
