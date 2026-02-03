// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for the LLM classifier.
var (
	// classifierCallsTotal counts total classification calls by result type.
	classifierCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "classifier_calls_total",
		Help: "Total classification calls by result and fallback status",
	}, []string{"result", "fallback"})

	// classifierLatency measures classification latency.
	classifierLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "classifier_latency_seconds",
		Help:    "Classification latency in seconds",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"cached"})

	// classifierCacheHitsTotal counts cache hits.
	classifierCacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "classifier_cache_hits_total",
		Help: "Total cache hits",
	})

	// classifierCacheMissesTotal counts cache misses.
	classifierCacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "classifier_cache_misses_total",
		Help: "Total cache misses",
	})

	// classifierFallbackTotal counts fallback uses by reason.
	classifierFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "classifier_fallback_total",
		Help: "Total fallbacks to regex classifier by reason",
	}, []string{"reason"})

	// classifierValidationWarningsTotal counts validation warnings.
	classifierValidationWarningsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "classifier_validation_warnings_total",
		Help: "Total validation warnings by type",
	}, []string{"type"})

	// classifierRetryTotal counts retry attempts.
	classifierRetryTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "classifier_retry_total",
		Help: "Total retry attempts",
	})
)

// recordClassification records metrics for a classification call.
func recordClassification(result *ClassificationResult, cached bool) {
	if result == nil {
		return
	}

	// Record latency
	cachedStr := "false"
	if cached {
		cachedStr = "true"
		classifierCacheHitsTotal.Inc()
	}
	classifierLatency.WithLabelValues(cachedStr).Observe(result.Duration.Seconds())

	// Record result
	resultStr := "non_analytical"
	if result.IsAnalytical {
		resultStr = "analytical"
	}
	fallbackStr := "false"
	if result.FallbackUsed {
		fallbackStr = "true"
	}
	classifierCallsTotal.WithLabelValues(resultStr, fallbackStr).Inc()

	// Record validation warnings
	for range result.ValidationWarnings {
		classifierValidationWarningsTotal.WithLabelValues("parameter").Inc()
	}
}

// recordFallback records a fallback metric.
func recordFallback(reason string) {
	// Normalize reason for cardinality control
	switch {
	case reason == "":
		reason = "unknown"
	case len(reason) > 30:
		reason = reason[:30]
	}
	classifierFallbackTotal.WithLabelValues(reason).Inc()
}

// recordCacheMiss records a cache miss.
func recordCacheMiss() {
	classifierCacheMissesTotal.Inc()
}

// recordRetry records a retry attempt.
func recordRetry() {
	classifierRetryTotal.Inc()
}
