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
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestStartSpan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "test.tracer", "TestOperation")
	defer span.End()

	if !span.SpanContext().IsValid() {
		t.Error("expected valid span context")
	}

	// Context should have span attached
	spanFromCtx := trace.SpanFromContext(ctx)
	if spanFromCtx.SpanContext().TraceID() != span.SpanContext().TraceID() ||
		spanFromCtx.SpanContext().SpanID() != span.SpanContext().SpanID() {
		t.Error("context should contain the created span")
	}
}

func TestStartSpan_WithAttributes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	ctx, span := StartSpan(context.Background(), "test.tracer", "TestOperation",
		trace.WithAttributes(
			attribute.String("test.key", "test.value"),
		),
	)
	defer span.End()

	if !span.SpanContext().IsValid() {
		t.Error("expected valid span context")
	}

	_ = ctx // Use ctx to avoid unused variable warning
}

func TestSpanFromContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("returns span from context", func(t *testing.T) {
		ctx, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		result := SpanFromContext(ctx)
		if result.SpanContext().TraceID() != span.SpanContext().TraceID() ||
			result.SpanContext().SpanID() != span.SpanContext().SpanID() {
			t.Error("should return same span from context")
		}
	})

	t.Run("returns noop span when no span in context", func(t *testing.T) {
		result := SpanFromContext(context.Background())
		// Should not panic, should return noop span
		if result == nil {
			t.Error("should return non-nil span even without context")
		}
	})
}

func TestRecordError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("records error on span", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		testErr := errors.New("test error")
		RecordError(span, testErr)

		// Should not panic
	})

	t.Run("handles nil span", func(t *testing.T) {
		testErr := errors.New("test error")
		RecordError(nil, testErr)
		// Should not panic
	})

	t.Run("handles nil error", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		RecordError(span, nil)
		// Should not panic
	})

	t.Run("records error with attributes", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		testErr := errors.New("test error")
		RecordError(span, testErr,
			attribute.String("operation", "parse"),
			attribute.Int("line", 42),
		)
		// Should not panic
	})
}

func TestRecordErrorf(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("records formatted error", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		RecordErrorf(span, "failed to process %s: %v", "input.go", errors.New("parse error"))
		// Should not panic
	})

	t.Run("handles nil span", func(t *testing.T) {
		RecordErrorf(nil, "error: %v", errors.New("test"))
		// Should not panic
	})
}

func TestSetSpanOK(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("sets span status OK", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		SetSpanOK(span)
		// Should not panic
	})

	t.Run("handles nil span", func(t *testing.T) {
		SetSpanOK(nil)
		// Should not panic
	})
}

func TestAddSpanEvent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("adds event to span", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		AddSpanEvent(span, "cache_miss", attribute.String("key", "test_key"))
		// Should not panic
	})

	t.Run("handles nil span", func(t *testing.T) {
		AddSpanEvent(nil, "event")
		// Should not panic
	})
}

func TestSetSpanAttributes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "none"
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("sets attributes on span", func(t *testing.T) {
		_, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		SetSpanAttributes(span,
			attribute.Int("result_count", 5),
			attribute.String("query_type", "find_callers"),
		)
		// Should not panic
	})

	t.Run("handles nil span", func(t *testing.T) {
		SetSpanAttributes(nil, attribute.String("key", "value"))
		// Should not panic
	})
}

func TestTraceID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("returns trace ID from context with span", func(t *testing.T) {
		ctx, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		traceID := TraceID(ctx)
		if traceID == "" {
			t.Error("expected non-empty trace ID")
		}
		if traceID != span.SpanContext().TraceID().String() {
			t.Error("trace ID should match span's trace ID")
		}
	})

	t.Run("returns empty string without span", func(t *testing.T) {
		traceID := TraceID(context.Background())
		if traceID != "" {
			t.Errorf("expected empty trace ID, got %q", traceID)
		}
	})
}

func TestSpanID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("returns span ID from context with span", func(t *testing.T) {
		ctx, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		spanID := SpanID(ctx)
		if spanID == "" {
			t.Error("expected non-empty span ID")
		}
		if spanID != span.SpanContext().SpanID().String() {
			t.Error("span ID should match span's span ID")
		}
	})

	t.Run("returns empty string without span", func(t *testing.T) {
		spanID := SpanID(context.Background())
		if spanID != "" {
			t.Errorf("expected empty span ID, got %q", spanID)
		}
	})
}

func TestHasActiveSpan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TraceExporter = "stdout" // Need real exporter for valid spans
	cfg.MetricExporter = "none"

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer shutdown(context.Background())

	t.Run("returns true with active span", func(t *testing.T) {
		ctx, span := StartSpan(context.Background(), "test", "TestOp")
		defer span.End()

		if !HasActiveSpan(ctx) {
			t.Error("expected HasActiveSpan to return true")
		}
	})

	t.Run("returns false without span", func(t *testing.T) {
		if HasActiveSpan(context.Background()) {
			t.Error("expected HasActiveSpan to return false without span")
		}
	})
}
