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

func TestExtractContext_WithTraceParent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	headers := http.Header{}
	headers.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	ctx := ExtractContext(context.Background(), headers)

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		t.Error("expected valid span context after extraction")
	}

	expectedTraceID := "0af7651916cd43dd8448eb211c80319c"
	if spanCtx.TraceID().String() != expectedTraceID {
		t.Errorf("trace ID = %q, want %q", spanCtx.TraceID().String(), expectedTraceID)
	}

	expectedSpanID := "b7ad6b7169203331"
	if spanCtx.SpanID().String() != expectedSpanID {
		t.Errorf("span ID = %q, want %q", spanCtx.SpanID().String(), expectedSpanID)
	}
}

func TestExtractContext_WithoutTraceParent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	headers := http.Header{}
	ctx := ExtractContext(context.Background(), headers)

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		t.Error("expected invalid span context without traceparent header")
	}
}

func TestInjectContext_AddsHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create a span to have valid context
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	headers := http.Header{}
	InjectContext(ctx, headers)

	traceparent := headers.Get("traceparent")
	if traceparent == "" {
		t.Error("expected traceparent header after injection")
	}
}

func TestRoundTrip_Propagation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create original span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "original-span")
	originalTraceID := span.SpanContext().TraceID().String()
	span.End()

	// Inject into headers
	headers := http.Header{}
	InjectContext(ctx, headers)

	// Extract into new context
	newCtx := ExtractContext(context.Background(), headers)

	// Verify trace ID is preserved
	newSpanCtx := trace.SpanContextFromContext(newCtx)
	if newSpanCtx.TraceID().String() != originalTraceID {
		t.Errorf("trace ID mismatch: got %q, want %q",
			newSpanCtx.TraceID().String(), originalTraceID)
	}
}

func TestPropagateToRequest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create a span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Create request and propagate
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req = PropagateToRequest(ctx, req)

	// Verify traceparent header was added
	traceparent := req.Header.Get("traceparent")
	if traceparent == "" {
		t.Error("expected traceparent header on request")
	}
}

func TestExtractFromRequest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	ctx := ExtractFromRequest(req)

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		t.Error("expected valid span context from request")
	}
}

func TestMapCarrier(t *testing.T) {
	t.Run("Get returns value", func(t *testing.T) {
		carrier := MapCarrier{
			"key1": "value1",
			"key2": "value2",
		}

		if got := carrier.Get("key1"); got != "value1" {
			t.Errorf("Get(key1) = %q, want %q", got, "value1")
		}
	})

	t.Run("Get returns empty for missing key", func(t *testing.T) {
		carrier := MapCarrier{}

		if got := carrier.Get("missing"); got != "" {
			t.Errorf("Get(missing) = %q, want empty string", got)
		}
	})

	t.Run("Set adds value", func(t *testing.T) {
		carrier := MapCarrier{}
		carrier.Set("key", "value")

		if got := carrier.Get("key"); got != "value" {
			t.Errorf("after Set, Get(key) = %q, want %q", got, "value")
		}
	})

	t.Run("Keys returns all keys", func(t *testing.T) {
		carrier := MapCarrier{
			"key1": "value1",
			"key2": "value2",
		}

		keys := carrier.Keys()
		if len(keys) != 2 {
			t.Errorf("Keys() returned %d keys, want 2", len(keys))
		}
	})
}

func TestExtractFromMap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	carrier := map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	}

	ctx := ExtractFromMap(context.Background(), carrier)

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		t.Error("expected valid span context from map")
	}
}

func TestInjectToMap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create a span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	t.Run("injects into existing map", func(t *testing.T) {
		carrier := map[string]string{"existing": "value"}
		result := InjectToMap(ctx, carrier)

		if result["existing"] != "value" {
			t.Error("existing key should be preserved")
		}
		if result["traceparent"] == "" {
			t.Error("traceparent should be injected")
		}
	})

	t.Run("creates new map when nil", func(t *testing.T) {
		result := InjectToMap(ctx, nil)

		if result == nil {
			t.Error("should create new map when nil")
		}
		if result["traceparent"] == "" {
			t.Error("traceparent should be injected")
		}
	})
}

func TestMapRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	// Create original span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "original-span")
	originalTraceID := span.SpanContext().TraceID().String()
	span.End()

	// Inject to map
	carrier := InjectToMap(ctx, nil)

	// Extract from map
	newCtx := ExtractFromMap(context.Background(), carrier)

	// Verify trace ID is preserved
	newSpanCtx := trace.SpanContextFromContext(newCtx)
	if newSpanCtx.TraceID().String() != originalTraceID {
		t.Errorf("trace ID mismatch in map round-trip: got %q, want %q",
			newSpanCtx.TraceID().String(), originalTraceID)
	}
}
