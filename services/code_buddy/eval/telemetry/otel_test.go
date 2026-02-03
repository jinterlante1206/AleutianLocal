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
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// -----------------------------------------------------------------------------
// Configuration Tests
// -----------------------------------------------------------------------------

func TestDefaultOTelConfig(t *testing.T) {
	config := DefaultOTelConfig()

	if config.ServiceName != "code-buddy-eval" {
		t.Errorf("ServiceName = %s, want code-buddy-eval", config.ServiceName)
	}
	if config.ServiceVersion != "1.0.0" {
		t.Errorf("ServiceVersion = %s, want 1.0.0", config.ServiceVersion)
	}
	if !config.TraceEnabled {
		t.Error("TraceEnabled should be true by default")
	}
	if !config.MetricsEnabled {
		t.Error("MetricsEnabled should be true by default")
	}
}

func TestOTelConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultOTelConfig()
		if err := config.Validate(); err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("empty service name", func(t *testing.T) {
		config := DefaultOTelConfig()
		config.ServiceName = ""
		if err := config.Validate(); err == nil {
			t.Error("Validate() should fail for empty service name")
		}
	})
}

// -----------------------------------------------------------------------------
// NewOTelSink Tests
// -----------------------------------------------------------------------------

func TestNewOTelSink(t *testing.T) {
	t.Run("creates with valid config", func(t *testing.T) {
		config := DefaultOTelConfig()
		// Use SDK providers for testing
		tp := trace.NewTracerProvider()
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, err := NewOTelSink(config)
		if err != nil {
			t.Fatalf("NewOTelSink failed: %v", err)
		}
		if sink == nil {
			t.Fatal("Expected non-nil sink")
		}
		sink.Close()
	})

	t.Run("creates with defaults", func(t *testing.T) {
		config := DefaultOTelConfig()
		// Don't set providers - will use global

		sink, err := NewOTelSink(config)
		if err != nil {
			t.Fatalf("NewOTelSink failed: %v", err)
		}
		if sink == nil {
			t.Fatal("Expected non-nil sink")
		}
		sink.Close()
	})

	t.Run("rejects nil config", func(t *testing.T) {
		_, err := NewOTelSink(nil)
		if !errors.Is(err, ErrInvalidOTelConfig) {
			t.Errorf("Expected ErrInvalidOTelConfig, got %v", err)
		}
	})

	t.Run("rejects invalid config", func(t *testing.T) {
		config := &OTelConfig{
			ServiceName: "", // Invalid
		}
		_, err := NewOTelSink(config)
		if !errors.Is(err, ErrInvalidOTelConfig) {
			t.Errorf("Expected ErrInvalidOTelConfig, got %v", err)
		}
	})

	t.Run("creates with tracing disabled", func(t *testing.T) {
		config := DefaultOTelConfig()
		config.TraceEnabled = false

		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, err := NewOTelSink(config)
		if err != nil {
			t.Fatalf("NewOTelSink failed: %v", err)
		}
		sink.Close()
	})

	t.Run("creates with metrics disabled", func(t *testing.T) {
		config := DefaultOTelConfig()
		config.MetricsEnabled = false

		tp := trace.NewTracerProvider()
		defer tp.Shutdown(context.Background())
		config.TracerProvider = tp

		sink, err := NewOTelSink(config)
		if err != nil {
			t.Fatalf("NewOTelSink failed: %v", err)
		}
		sink.Close()
	})
}

// -----------------------------------------------------------------------------
// RecordBenchmark Tests
// -----------------------------------------------------------------------------

func TestOTelSink_RecordBenchmark(t *testing.T) {
	t.Run("records benchmark with tracing", func(t *testing.T) {
		// Create test span exporter
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestBenchmarkData()
		err := sink.RecordBenchmark(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}

		// Verify span was created
		spans := spanRecorder.Ended()
		if len(spans) != 1 {
			t.Errorf("Expected 1 span, got %d", len(spans))
		}

		if len(spans) > 0 {
			span := spans[0]
			if span.Name() != "benchmark.record" {
				t.Errorf("Span name = %s, want benchmark.record", span.Name())
			}
		}
	})

	t.Run("records benchmark without memory", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestBenchmarkData()
		data.Memory = nil

		err := sink.RecordBenchmark(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}
	})

	t.Run("handles empty name", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestBenchmarkData()
		data.Name = ""

		err := sink.RecordBenchmark(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}
	})

	t.Run("records benchmark with labels", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestBenchmarkData()
		data.Labels = map[string]string{
			"env":     "test",
			"version": "1.0.0",
		}

		err := sink.RecordBenchmark(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordBenchmark(nil, createTestBenchmarkData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordBenchmark(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})

	t.Run("returns error after close", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		sink.Close()

		err := sink.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if !errors.Is(err, ErrSinkClosed) {
			t.Errorf("Expected ErrSinkClosed, got %v", err)
		}
	})

	t.Run("skips tracing when disabled", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp
		config.TraceEnabled = false

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordBenchmark(context.Background(), createTestBenchmarkData())
		if err != nil {
			t.Fatalf("RecordBenchmark failed: %v", err)
		}

		// Verify no span was created
		spans := spanRecorder.Ended()
		if len(spans) != 0 {
			t.Errorf("Expected 0 spans, got %d", len(spans))
		}
	})
}

// -----------------------------------------------------------------------------
// RecordComparison Tests
// -----------------------------------------------------------------------------

func TestOTelSink_RecordComparison(t *testing.T) {
	t.Run("records comparison with tracing", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestComparisonData()
		err := sink.RecordComparison(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordComparison failed: %v", err)
		}

		// Verify span was created
		spans := spanRecorder.Ended()
		if len(spans) != 1 {
			t.Errorf("Expected 1 span, got %d", len(spans))
		}

		if len(spans) > 0 {
			span := spans[0]
			if span.Name() != "comparison.record" {
				t.Errorf("Span name = %s, want comparison.record", span.Name())
			}
		}
	})

	t.Run("handles no winner", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestComparisonData()
		data.Winner = ""
		data.Significant = false

		err := sink.RecordComparison(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordComparison failed: %v", err)
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordComparison(nil, createTestComparisonData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordComparison(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// RecordError Tests
// -----------------------------------------------------------------------------

func TestOTelSink_RecordError(t *testing.T) {
	t.Run("records error with tracing", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := createTestErrorData()
		err := sink.RecordError(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordError failed: %v", err)
		}

		// Verify span was created with error status
		spans := spanRecorder.Ended()
		if len(spans) != 1 {
			t.Errorf("Expected 1 span, got %d", len(spans))
		}

		if len(spans) > 0 {
			span := spans[0]
			if span.Name() != "error.record" {
				t.Errorf("Span name = %s, want error.record", span.Name())
			}
		}
	})

	t.Run("handles empty labels", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		data := &ErrorData{
			Timestamp: time.Now(),
			// Empty component, operation, errorType
		}

		err := sink.RecordError(context.Background(), data)
		if err != nil {
			t.Fatalf("RecordError failed: %v", err)
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordError(nil, createTestErrorData())
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("rejects nil data", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.RecordError(context.Background(), nil)
		if !errors.Is(err, ErrNilData) {
			t.Errorf("Expected ErrNilData, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Flush and Close Tests
// -----------------------------------------------------------------------------

func TestOTelSink_Flush(t *testing.T) {
	t.Run("flush succeeds", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.Flush(context.Background())
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
	})

	t.Run("rejects nil context", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		err := sink.Flush(nil)
		if !errors.Is(err, ErrNilContext) {
			t.Errorf("Expected ErrNilContext, got %v", err)
		}
	})

	t.Run("returns error after close", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		sink.Close()

		err := sink.Flush(context.Background())
		if !errors.Is(err, ErrSinkClosed) {
			t.Errorf("Expected ErrSinkClosed, got %v", err)
		}
	})
}

func TestOTelSink_Close(t *testing.T) {
	t.Run("close succeeds", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)

		err := sink.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)

		// Close multiple times
		sink.Close()
		sink.Close()
		err := sink.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Helper Method Tests
// -----------------------------------------------------------------------------

func TestOTelSink_StartBenchmarkSpan(t *testing.T) {
	t.Run("creates span", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		ctx, span := sink.StartBenchmarkSpan(context.Background(), "test_benchmark")
		span.End()

		if ctx == nil {
			t.Error("Expected non-nil context")
		}

		spans := spanRecorder.Ended()
		if len(spans) != 1 {
			t.Errorf("Expected 1 span, got %d", len(spans))
		}

		if len(spans) > 0 && spans[0].Name() != "benchmark.test_benchmark" {
			t.Errorf("Span name = %s, want benchmark.test_benchmark", spans[0].Name())
		}
	})

	t.Run("handles nil context", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		ctx, span := sink.StartBenchmarkSpan(nil, "test")
		span.End()

		if ctx == nil {
			t.Error("Expected non-nil context")
		}
	})

	t.Run("handles empty name", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		_, span := sink.StartBenchmarkSpan(context.Background(), "")
		span.End()

		spans := spanRecorder.Ended()
		if len(spans) > 0 && spans[0].Name() != "benchmark.unknown" {
			t.Errorf("Span name = %s, want benchmark.unknown", spans[0].Name())
		}
	})
}

func TestOTelSink_AddBenchmarkEvent(t *testing.T) {
	t.Run("adds event to span", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
		mp := metric.NewMeterProvider()
		defer tp.Shutdown(context.Background())
		defer mp.Shutdown(context.Background())

		config := DefaultOTelConfig()
		config.TracerProvider = tp
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		ctx, span := sink.StartBenchmarkSpan(context.Background(), "test")
		sink.AddBenchmarkEvent(ctx, "iteration_complete",
			attribute.Int("iteration", 1),
			attribute.Float64("latency_ms", 10.5),
		)
		span.End()

		spans := spanRecorder.Ended()
		if len(spans) != 1 {
			t.Fatalf("Expected 1 span, got %d", len(spans))
		}

		events := spans[0].Events()
		if len(events) != 1 {
			t.Errorf("Expected 1 event, got %d", len(events))
		}

		if len(events) > 0 && events[0].Name != "iteration_complete" {
			t.Errorf("Event name = %s, want iteration_complete", events[0].Name)
		}
	})

	t.Run("handles context without span", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		// Should not panic with context that has no span
		sink.AddBenchmarkEvent(context.Background(), "test_event")
	})
}

// -----------------------------------------------------------------------------
// Concurrent Access Tests
// -----------------------------------------------------------------------------

func TestOTelSink_Concurrent(t *testing.T) {
	t.Run("handles concurrent recording", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)
		defer sink.Close()

		var wg sync.WaitGroup
		iterations := 100

		for i := 0; i < iterations; i++ {
			wg.Add(3)

			go func() {
				defer wg.Done()
				sink.RecordBenchmark(context.Background(), createTestBenchmarkData())
			}()

			go func() {
				defer wg.Done()
				sink.RecordComparison(context.Background(), createTestComparisonData())
			}()

			go func() {
				defer wg.Done()
				sink.RecordError(context.Background(), createTestErrorData())
			}()
		}

		wg.Wait()
	})

	t.Run("handles concurrent close and record", func(t *testing.T) {
		config := DefaultOTelConfig()
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(context.Background())
		config.MeterProvider = mp

		sink, _ := NewOTelSink(config)

		var wg sync.WaitGroup

		// Start recording
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sink.RecordBenchmark(context.Background(), createTestBenchmarkData())
			}()
		}

		// Close while recording
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink.Close()
		}()

		wg.Wait()
	})
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

func TestOTelSink_InterfaceCompliance(t *testing.T) {
	var _ Sink = (*OTelSink)(nil)
}
