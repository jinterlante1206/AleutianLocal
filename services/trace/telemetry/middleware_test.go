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
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestTracingMiddleware_CreatesSpan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Track if span was created
	var capturedSpanCtx trace.SpanContext

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		capturedSpanCtx = span.SpanContext()
		w.WriteHeader(http.StatusOK)
	})

	middleware := TracingMiddleware("test.http")
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if !capturedSpanCtx.IsValid() {
		t.Error("expected valid span context, got invalid")
	}
}

func TestTracingMiddleware_ExtractsTraceContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	var capturedTraceID string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		capturedTraceID = span.SpanContext().TraceID().String()
		w.WriteHeader(http.StatusOK)
	})

	middleware := TracingMiddleware("test.http")
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// Add W3C trace context header
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	// Should have extracted the trace ID from the header
	expectedTraceID := "0af7651916cd43dd8448eb211c80319c"
	if capturedTraceID != expectedTraceID {
		t.Errorf("trace ID = %q, want %q", capturedTraceID, expectedTraceID)
	}
}

func TestTracingMiddleware_CapturesStatusCode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"201 Created", http.StatusCreated},
		{"400 Bad Request", http.StatusBadRequest},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := TracingMiddleware("test.http")
			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.statusCode)
			}
		})
	}
}

func TestMetricsMiddleware_RecordsMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_middleware_metrics")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := MetricsMiddleware(metrics)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCombinedMiddleware(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	meter := otel.Meter("test_combined_middleware")
	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}

	var capturedSpanCtx trace.SpanContext

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		capturedSpanCtx = span.SpanContext()
		w.WriteHeader(http.StatusOK)
	})

	middleware := CombinedMiddleware("test.http", metrics)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if !capturedSpanCtx.IsValid() {
		t.Error("expected valid span context from combined middleware")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestStatusResponseWriter_CapturesStatus(t *testing.T) {
	tests := []struct {
		name     string
		write    func(w http.ResponseWriter)
		expected int
	}{
		{
			name: "explicit WriteHeader",
			write: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusCreated)
			},
			expected: http.StatusCreated,
		},
		{
			name: "implicit 200 from Write",
			write: func(w http.ResponseWriter) {
				w.Write([]byte("hello"))
			},
			expected: http.StatusOK,
		},
		{
			name: "WriteHeader then Write",
			write: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("error"))
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "multiple WriteHeader calls",
			write: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
				w.WriteHeader(http.StatusNotFound) // Should be ignored
			},
			expected: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			sw := newStatusResponseWriter(rec)

			tt.write(sw)

			if sw.statusCode != tt.expected {
				t.Errorf("statusCode = %d, want %d", sw.statusCode, tt.expected)
			}
		})
	}
}

func TestStatusResponseWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusResponseWriter(rec)

	unwrapped := sw.Unwrap()
	if unwrapped != rec {
		t.Error("Unwrap() should return original ResponseWriter")
	}
}

func TestSchemeFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*http.Request)
		expected string
	}{
		{
			name:     "default http",
			setup:    func(r *http.Request) {},
			expected: "http",
		},
		{
			name: "X-Forwarded-Proto https",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			},
			expected: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			tt.setup(req)

			scheme := schemeFromRequest(req)
			if scheme != tt.expected {
				t.Errorf("scheme = %q, want %q", scheme, tt.expected)
			}
		})
	}
}
