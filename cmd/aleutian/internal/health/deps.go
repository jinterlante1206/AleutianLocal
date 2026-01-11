// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package health deps.go contains temporary interface definitions for dependencies
not yet moved to their target packages.

# Temporary Nature

These interfaces are TEMPORARY stubs that will be removed when the corresponding
packages are moved in later phases:
  - LogSanitizer: Phase 7 (internal/security/)
  - MetricsStore: Phase 8 (internal/observability/)
  - DiagnosticsTracer: Phase 4 (internal/diagnostics/)

# Design Rationale

The health package depends on interfaces defined in the main package. Since
main cannot import health (circular dependency), we define minimal interface
copies here. When the actual packages are moved, these stubs will be replaced
with proper imports.

# Assumptions

  - Interface signatures match the original definitions in main package
  - No behavioral changes to interface contracts
  - Consumers will pass compatible implementations
*/
package health

import (
	"context"
	"time"
)

// =============================================================================
// PHASE 7: SECURITY INTERFACES (internal/security/)
// =============================================================================

// LogSanitizer removes sensitive data from log output.
//
// # Description
//
// Provides log sanitization to prevent PII leakage. Used by HealthIntelligence
// to sanitize container logs before analysis.
//
// # Inputs
//
//   - input: Raw log string that may contain sensitive data
//
// # Outputs
//
//   - string: Sanitized log with sensitive data redacted
//
// # Examples
//
//	sanitizer := NewDefaultLogSanitizer()
//	clean := sanitizer.Sanitize("User email: test@example.com")
//	// clean = "User email: [REDACTED]"
//
// # Limitations
//
//   - Pattern-based sanitization may miss novel PII formats
//
// # Assumptions
//
//   - Caller provides UTF-8 encoded strings
//   - Sanitization patterns are pre-configured
type LogSanitizer interface {
	// Sanitize removes sensitive data from the input string.
	//
	// # Description
	//
	// Scans input for patterns matching PII, secrets, and sensitive data,
	// replacing matches with redaction markers.
	//
	// # Inputs
	//
	//   - input: String to sanitize
	//
	// # Outputs
	//
	//   - string: Sanitized string with sensitive data replaced
	//
	// # Examples
	//
	//   clean := s.Sanitize("API key: sk-123abc")
	//   // clean = "API key: [REDACTED]"
	//
	// # Limitations
	//
	//   - May over-sanitize if patterns are too broad
	//
	// # Assumptions
	//
	//   - Input is valid UTF-8
	Sanitize(input string) string
}

// =============================================================================
// PHASE 8: OBSERVABILITY INTERFACES (internal/observability/)
// =============================================================================

// MetricsStore provides metric storage and retrieval.
//
// # Description
//
// Stores and queries time-series metrics for health analysis. Used by
// HealthIntelligence to detect trends and anomalies.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Examples
//
//	store := NewInMemoryMetricsStore()
//	points := store.Query("orchestrator", "latency_p99", start, end)
//	baseline := store.GetBaseline("orchestrator", "latency_p99", 24*time.Hour)
//
// # Limitations
//
//   - In-memory store has limited retention
//   - Query performance degrades with large datasets
//
// # Assumptions
//
//   - Metric names follow convention: service/metric_name
//   - Values are numeric (float64)
type MetricsStore interface {
	// Record stores a metric data point.
	//
	// # Description
	//
	// Adds a metric value to the store. In-memory storage has a rolling
	// window; older points are pruned automatically.
	//
	// # Inputs
	//
	//   - service: Service name (e.g., "orchestrator")
	//   - metric: Metric name (e.g., "latency_p99", "error_rate")
	//   - value: Metric value as float64
	//   - timestamp: When the metric was observed
	//
	// # Outputs
	//
	//   - None
	//
	// # Examples
	//
	//   store.Record("orchestrator", "latency_p99", 150.0, time.Now())
	//
	// # Limitations
	//
	//   - Very high write rates may cause contention
	//
	// # Assumptions
	//
	//   - Metric name is non-empty
	Record(service, metric string, value float64, timestamp time.Time)

	// GetBaseline returns baseline statistics for a metric.
	//
	// # Description
	//
	// Calculates baseline statistics (mean, p50, p99) for a metric
	// over the specified time window.
	//
	// # Inputs
	//
	//   - service: Service name (e.g., "orchestrator")
	//   - metric: Metric name (e.g., "latency_p99")
	//   - window: Time window for baseline calculation
	//
	// # Outputs
	//
	//   - *BaselineStats: Baseline statistics, or nil if insufficient data
	//
	// # Examples
	//
	//   baseline := store.GetBaseline("orchestrator", "latency_p99", 24*time.Hour)
	//   if baseline != nil {
	//       fmt.Printf("Mean: %.2f, P99: %.2f\n", baseline.Mean, baseline.P99)
	//   }
	//
	// # Limitations
	//
	//   - Returns nil if fewer than 10 data points
	//
	// # Assumptions
	//
	//   - Window duration is positive
	GetBaseline(service, metric string, window time.Duration) *BaselineStats

	// Query returns metric data points in a time range.
	//
	// # Description
	//
	// Retrieves all data points for a metric within the specified
	// time range, sorted by timestamp ascending.
	//
	// # Inputs
	//
	//   - service: Service name
	//   - metric: Metric name
	//   - start: Start of time range (inclusive)
	//   - end: End of time range (inclusive)
	//
	// # Outputs
	//
	//   - []MetricPoint: Data points in range, may be empty
	//
	// # Examples
	//
	//   points := store.Query("orchestrator", "error_rate",
	//       time.Now().Add(-5*time.Minute), time.Now())
	//   for _, p := range points {
	//       fmt.Printf("%v: %.4f\n", p.Timestamp, p.Value)
	//   }
	//
	// # Limitations
	//
	//   - Large ranges may return many points
	//
	// # Assumptions
	//
	//   - start <= end
	Query(service, metric string, start, end time.Time) []MetricPoint
}

// MetricPoint represents a single metric data point.
//
// # Description
//
// A timestamped numeric value for a metric. Used in time-series
// analysis for trend detection.
//
// # Examples
//
//	point := MetricPoint{
//	    Timestamp: time.Now(),
//	    Value:     150.5,
//	}
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Value is a valid float64 (not NaN or Inf)
type MetricPoint struct {
	// Timestamp is when the metric was recorded.
	Timestamp time.Time

	// Value is the metric value.
	Value float64
}

// BaselineStats contains baseline statistics for a metric.
//
// # Description
//
// Statistical summary of a metric over a time window. Used to
// compare current values against historical norms.
//
// # Examples
//
//	stats := BaselineStats{Mean: 100.0, P50: 95.0, P99: 250.0}
//	if current > stats.P99 * 1.5 {
//	    // Significant deviation from baseline
//	}
//
// # Limitations
//
//   - Simple percentiles; no standard deviation
//
// # Assumptions
//
//   - Values are non-negative
type BaselineStats struct {
	// Mean is the arithmetic mean of values.
	Mean float64

	// P50 is the 50th percentile (median).
	P50 float64

	// P99 is the 99th percentile.
	P99 float64
}

// =============================================================================
// PHASE 4: DIAGNOSTICS INTERFACES (internal/diagnostics/)
// =============================================================================

// DiagnosticsTracer provides span creation for distributed tracing.
//
// # Description
//
// Abstracts OpenTelemetry span creation for health analysis tracing.
// Enables the "Support Ticket Revolution" - users report trace IDs,
// support views the full trace in Jaeger.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Examples
//
//	tracer := NewDefaultDiagnosticsTracer(ctx, "aleutian-health")
//	ctx, finish := tracer.StartSpan(ctx, "health.analysis", map[string]string{
//	    "analysis.type": "periodic",
//	})
//	defer finish(nil)
//
// # Limitations
//
//   - NoOp tracer generates IDs but doesn't export
//
// # Assumptions
//
//   - OTel collector is configured for actual export
type DiagnosticsTracer interface {
	// StartSpan creates a new span for tracing.
	//
	// # Description
	//
	// Creates a child span with the given name and attributes.
	// Returns a context with the span and a finish function.
	//
	// # Inputs
	//
	//   - ctx: Parent context (may contain existing trace)
	//   - name: Span name (e.g., "health.analysis")
	//   - attrs: Key-value attributes for the span
	//
	// # Outputs
	//
	//   - context.Context: Context with span for propagation
	//   - func(error): Call to end span (pass nil for success)
	//
	// # Examples
	//
	//   ctx, finish := tracer.StartSpan(ctx, "check.http", map[string]string{
	//       "service.name": "orchestrator",
	//   })
	//   defer finish(nil)
	//
	// # Limitations
	//
	//   - Span must be finished to be exported
	//
	// # Assumptions
	//
	//   - Caller finishes the span
	StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error))

	// GetTraceID returns the trace ID from the context.
	//
	// # Description
	//
	// Extracts the W3C trace ID (32-character hex) from the context.
	// Returns empty string if no span in context.
	//
	// # Inputs
	//
	//   - ctx: Context with span
	//
	// # Outputs
	//
	//   - string: 32-character hex trace ID, or empty string
	//
	// # Examples
	//
	//   traceID := tracer.GetTraceID(ctx)
	//   if traceID != "" {
	//       report.TraceID = traceID
	//   }
	//
	// # Limitations
	//
	//   - Returns empty if context has no span
	//
	// # Assumptions
	//
	//   - Context was created by StartSpan
	GetTraceID(ctx context.Context) string
}

// noOpTraceIDKey is the context key for trace IDs in NoOp mode.
//
// # Description
//
// Used by NoOpDiagnosticsTracer to store generated trace IDs
// in context without actual OTel integration.
type noOpTraceIDKey struct{}
