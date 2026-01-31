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
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ServiceName != "aleutian" {
		t.Errorf("ServiceName = %q, want %q", cfg.ServiceName, "aleutian")
	}
	if cfg.TraceExporter != "otlp" {
		t.Errorf("TraceExporter = %q, want %q", cfg.TraceExporter, "otlp")
	}
	if cfg.MetricExporter != "prometheus" {
		t.Errorf("MetricExporter = %q, want %q", cfg.MetricExporter, "prometheus")
	}
	if cfg.OTLPEndpoint != "localhost:4317" {
		t.Errorf("OTLPEndpoint = %q, want %q", cfg.OTLPEndpoint, "localhost:4317")
	}
}

func TestInit_NilContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"

	_, err := Init(nil, cfg)
	if err != ErrNilContext {
		t.Errorf("Init(nil, cfg) error = %v, want %v", err, ErrNilContext)
	}
}

func TestInit_NoopExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if shutdown == nil {
		t.Fatal("shutdown function is nil")
	}

	// Verify shutdown works
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

func TestInit_StdoutExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Verify tracer is configured
	tracer := otel.Tracer("test")
	if tracer == nil {
		t.Error("tracer is nil")
	}
}

func TestInit_UnknownExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "unknown_exporter"

	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("Init() with unknown exporter should fail")
	}
	if !strings.Contains(err.Error(), "unknown exporter type") {
		t.Errorf("error = %v, want to contain 'unknown exporter type'", err)
	}
}

func TestLoggerWithTrace_NoSpan(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// No span in context
	result := LoggerWithTrace(context.Background(), logger)

	// Should return original logger (no trace fields added)
	result.Info("test message")
	output := buf.String()

	if strings.Contains(output, "trace_id") {
		t.Errorf("output should not contain trace_id when no span: %s", output)
	}
}

func TestLoggerWithTrace_NilContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	result := LoggerWithTrace(nil, logger)

	// Should return original logger
	result.Info("test message")
	output := buf.String()

	if !strings.Contains(output, "test message") {
		t.Errorf("output should contain message: %s", output)
	}
}

func TestLoggerWithTrace_NilLogger(t *testing.T) {
	result := LoggerWithTrace(context.Background(), nil)

	// Should return slog.Default() instead of panicking
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestLoggerWithTrace_WithSpan(t *testing.T) {
	// Set up a tracer that creates valid span contexts
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create a mock span context for testing
	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	result := LoggerWithTrace(ctx, logger)
	result.Info("test message")

	output := buf.String()

	if !strings.Contains(output, "trace_id") {
		t.Errorf("output should contain trace_id: %s", output)
	}
	if !strings.Contains(output, "span_id") {
		t.Errorf("output should contain span_id: %s", output)
	}
	if !strings.Contains(output, traceID.String()) {
		t.Errorf("output should contain actual trace ID: %s", output)
	}
}

func TestLoggerWithNode(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	result := LoggerWithNode(context.Background(), logger, "PARSE_FILES")
	result.Info("test message")

	output := buf.String()

	if !strings.Contains(output, `"node":"PARSE_FILES"`) {
		t.Errorf("output should contain node field: %s", output)
	}
}

func TestLoggerWithSession(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	result := LoggerWithSession(context.Background(), logger, "abc123")
	result.Info("test message")

	output := buf.String()

	if !strings.Contains(output, `"session_id":"abc123"`) {
		t.Errorf("output should contain session_id field: %s", output)
	}
}

func TestGetEnvOr(t *testing.T) {
	t.Run("returns fallback when env not set", func(t *testing.T) {
		result := getEnvOr("TELEMETRY_TEST_NONEXISTENT_VAR_12345", "fallback")
		if result != "fallback" {
			t.Errorf("getEnvOr() = %q, want %q", result, "fallback")
		}
	})

	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TELEMETRY_TEST_VAR", "custom_value")
		result := getEnvOr("TELEMETRY_TEST_VAR", "fallback")
		if result != "custom_value" {
			t.Errorf("getEnvOr() = %q, want %q", result, "custom_value")
		}
	})
}

func TestInit_PrometheusExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Verify meter is configured
	meter := otel.Meter("test")
	if meter == nil {
		t.Error("meter is nil")
	}

	// Create a test counter to ensure metrics work
	counter, err := meter.Int64Counter("test_counter")
	if err != nil {
		t.Fatalf("creating counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	// Verify MetricsHandler is available
	handler := MetricsHandler()
	if handler == nil {
		t.Fatal("MetricsHandler() returned nil")
	}
}

func TestMetricsHandler_ReturnsPrometheusFormat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "prometheus"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create and increment a counter
	meter := otel.Meter("test_metrics")
	counter, err := meter.Int64Counter("telemetry_test_requests_total")
	if err != nil {
		t.Fatalf("creating counter: %v", err)
	}
	counter.Add(context.Background(), 42)

	handler := MetricsHandler()
	if handler == nil {
		t.Fatal("MetricsHandler() returned nil")
	}

	// Make a request to the metrics handler
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	// Verify it's Prometheus format (should contain # HELP or # TYPE)
	output := string(body)
	if !strings.Contains(output, "# HELP") && !strings.Contains(output, "# TYPE") {
		t.Errorf("output should be Prometheus format: %s", output[:min(200, len(output))])
	}
}

func TestMetricsHandler_NilBeforeInit(t *testing.T) {
	// Reset the handler to simulate fresh state
	prometheusHandlerMu.Lock()
	oldHandler := prometheusHandler
	prometheusHandler = nil
	prometheusHandlerMu.Unlock()

	defer func() {
		prometheusHandlerMu.Lock()
		prometheusHandler = oldHandler
		prometheusHandlerMu.Unlock()
	}()

	handler := MetricsHandler()
	if handler != nil {
		t.Error("MetricsHandler() should return nil before Prometheus init")
	}
}

func TestInit_StdoutMetricExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "stdout"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Verify meter is configured
	meter := otel.Meter("test")
	if meter == nil {
		t.Error("meter is nil")
	}
}

func TestInit_UnknownMetricExporter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "unknown_metric_exporter"

	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("Init() with unknown metric exporter should fail")
	}
	if !strings.Contains(err.Error(), "unknown exporter type") {
		t.Errorf("error = %v, want to contain 'unknown exporter type'", err)
	}
}
