// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// =============================================================================
// Prometheus Metrics for Tool Routing
// =============================================================================

var (
	// routingLatency measures the time taken for tool routing decisions.
	// Labels: model (the router model used), status (success, error, low_confidence)
	routingLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "latency_seconds",
		Help:      "Tool routing decision latency in seconds",
		Buckets:   []float64{0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2, 0.3, 0.5, 1.0},
	}, []string{"model", "status"})

	// routingConfidence tracks the distribution of confidence scores.
	// Labels: model
	routingConfidence = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "confidence",
		Help:      "Distribution of routing confidence scores",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0},
	}, []string{"model"})

	// routingSelections counts tool selections by the router.
	// Labels: model, tool (selected tool name)
	routingSelections = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "selections_total",
		Help:      "Total tool selections by router",
	}, []string{"model", "tool"})

	// routingFallbacks counts when routing falls back to main LLM.
	// Labels: model, reason (error, low_confidence, disabled)
	routingFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "fallbacks_total",
		Help:      "Total routing fallbacks to main LLM",
	}, []string{"model", "reason"})

	// routingErrors counts routing errors by type.
	// Labels: model, error_type (timeout, parse_error, model_unavailable)
	routingErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "errors_total",
		Help:      "Total routing errors by type",
	}, []string{"model", "error_type"})

	// modelWarmupDuration measures time to warm up router models.
	// Labels: model
	modelWarmupDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "warmup_duration_seconds",
		Help:      "Time to warm up router model",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60},
	}, []string{"model"})

	// modelWarmupStatus tracks warmup success/failure.
	// Labels: model, status (success, error)
	modelWarmupStatus = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "code_buddy",
		Subsystem: "routing",
		Name:      "warmup_total",
		Help:      "Model warmup attempts by status",
	}, []string{"model", "status"})
)

// =============================================================================
// Metrics Recording Functions
// =============================================================================

// RecordRoutingLatency records the latency of a routing decision.
//
// Inputs:
//
//	model - The router model name.
//	status - "success", "error", or "low_confidence".
//	durationSec - Duration in seconds.
func RecordRoutingLatency(model, status string, durationSec float64) {
	routingLatency.WithLabelValues(model, status).Observe(durationSec)
}

// RecordRoutingConfidence records a confidence score.
//
// Inputs:
//
//	model - The router model name.
//	confidence - The confidence score (0.0-1.0).
func RecordRoutingConfidence(model string, confidence float64) {
	routingConfidence.WithLabelValues(model).Observe(confidence)
}

// RecordRoutingSelection records a successful tool selection.
//
// Inputs:
//
//	model - The router model name.
//	tool - The selected tool name.
func RecordRoutingSelection(model, tool string) {
	routingSelections.WithLabelValues(model, tool).Inc()
}

// RecordRoutingFallback records a fallback to main LLM.
//
// Inputs:
//
//	model - The router model name (empty if router disabled).
//	reason - "error", "low_confidence", or "disabled".
func RecordRoutingFallback(model, reason string) {
	routingFallbacks.WithLabelValues(model, reason).Inc()
}

// RecordRoutingError records a routing error.
//
// Inputs:
//
//	model - The router model name.
//	errorType - Error type (e.g., "timeout", "parse_error").
func RecordRoutingError(model, errorType string) {
	routingErrors.WithLabelValues(model, errorType).Inc()
}

// RecordModelWarmup records model warmup metrics.
//
// Inputs:
//
//	model - The model name.
//	durationSec - Warmup duration in seconds.
//	success - Whether warmup succeeded.
func RecordModelWarmup(model string, durationSec float64, success bool) {
	modelWarmupDuration.WithLabelValues(model).Observe(durationSec)
	status := "success"
	if !success {
		status = "error"
	}
	modelWarmupStatus.WithLabelValues(model, status).Inc()
}
