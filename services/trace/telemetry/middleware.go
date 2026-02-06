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
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// statusResponseWriter wraps http.ResponseWriter to capture the status code.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// newStatusResponseWriter creates a new statusResponseWriter.
func newStatusResponseWriter(w http.ResponseWriter) *statusResponseWriter {
	return &statusResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default to 200
	}
}

// WriteHeader captures the status code before writing.
func (w *statusResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write writes data and sets status to 200 if not already set.
func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.statusCode = http.StatusOK
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility.
func (w *statusResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// TracingMiddleware creates HTTP middleware that adds distributed tracing.
//
// Description:
//
//	Wraps each HTTP request in a span with standard HTTP semantic attributes.
//	Extracts trace context from incoming headers for distributed tracing.
//	Sets span status to Error for 5xx responses.
//
// Inputs:
//
//	tracerName - Name for the tracer (e.g., "trace.http").
//
// Outputs:
//
//	Middleware function that wraps http.Handler.
//
// Example:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/api/query", handleQuery)
//	handler := telemetry.TracingMiddleware("trace.http")(mux)
//	http.ListenAndServe(":8080", handler)
//
// Thread Safety: Safe for concurrent use.
func TracingMiddleware(tracerName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(tracerName)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from incoming headers
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), &headerCarrier{r.Header})

			// Create span with HTTP semantic attributes
			spanName := r.Method + " " + r.URL.Path
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.target", r.URL.Path),
					attribute.String("http.host", r.Host),
					attribute.String("http.scheme", schemeFromRequest(r)),
					attribute.String("http.user_agent", r.UserAgent()),
					attribute.String("net.peer.ip", r.RemoteAddr),
				),
			)
			defer span.End()

			// Wrap response writer to capture status code
			sw := newStatusResponseWriter(w)

			// Pass request with traced context
			next.ServeHTTP(sw, r.WithContext(ctx))

			// Record response attributes
			span.SetAttributes(
				attribute.Int("http.status_code", sw.statusCode),
			)

			// Set span status based on HTTP status code
			if sw.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(sw.statusCode))
			} else if sw.statusCode >= 400 {
				span.SetStatus(codes.Unset, "")
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

// MetricsMiddleware creates HTTP middleware that records request metrics.
//
// Description:
//
//	Records HTTP request count, duration, and active request count.
//	Metrics include labels for method, path, and status code.
//
// Inputs:
//
//	metrics - Pre-configured Metrics instance.
//
// Outputs:
//
//	Middleware function that wraps http.Handler.
//
// Example:
//
//	metrics, _ := telemetry.NewMetrics(otel.Meter("trace"))
//	mux := http.NewServeMux()
//	handler := telemetry.MetricsMiddleware(metrics)(mux)
//	http.ListenAndServe(":8080", handler)
//
// Thread Safety: Safe for concurrent use.
func MetricsMiddleware(metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			start := time.Now()

			// Track active requests
			metrics.HTTPActiveRequests.Add(ctx, 1)
			defer metrics.HTTPActiveRequests.Add(ctx, -1)

			// Wrap response writer to capture status code
			sw := newStatusResponseWriter(w)

			// Process request
			next.ServeHTTP(sw, r)

			// Record metrics
			duration := time.Since(start).Seconds()

			attrs := metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("path", r.URL.Path),
				attribute.Int("status", sw.statusCode),
			)

			metrics.HTTPRequestsTotal.Add(ctx, 1, attrs)
			metrics.HTTPRequestDuration.Record(ctx, duration, attrs)
		})
	}
}

// CombinedMiddleware creates middleware that adds both tracing and metrics.
//
// Description:
//
//	Convenience function that combines TracingMiddleware and MetricsMiddleware.
//	Tracing is applied first (outer), then metrics (inner).
//
// Inputs:
//
//	tracerName - Name for the tracer.
//	metrics - Pre-configured Metrics instance.
//
// Outputs:
//
//	Middleware function that wraps http.Handler.
//
// Example:
//
//	metrics, _ := telemetry.NewMetrics(otel.Meter("trace"))
//	mux := http.NewServeMux()
//	handler := telemetry.CombinedMiddleware("trace.http", metrics)(mux)
//	http.ListenAndServe(":8080", handler)
//
// Thread Safety: Safe for concurrent use.
func CombinedMiddleware(tracerName string, metrics *Metrics) func(http.Handler) http.Handler {
	tracingMw := TracingMiddleware(tracerName)
	metricsMw := MetricsMiddleware(metrics)

	return func(next http.Handler) http.Handler {
		return tracingMw(metricsMw(next))
	}
}

// headerCarrier adapts http.Header to propagation.TextMapCarrier.
type headerCarrier struct {
	header http.Header
}

// Get returns the value for a key.
func (c *headerCarrier) Get(key string) string {
	return c.header.Get(key)
}

// Set sets a key-value pair.
func (c *headerCarrier) Set(key, value string) {
	c.header.Set(key, value)
}

// Keys returns all keys in the carrier.
func (c *headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.header))
	for k := range c.header {
		keys = append(keys, k)
	}
	return keys
}

// schemeFromRequest returns the scheme (http/https) from the request.
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// Check X-Forwarded-Proto header (common for reverse proxies)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
