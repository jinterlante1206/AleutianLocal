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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrOTelInitFailed is returned when OpenTelemetry initialization fails.
	ErrOTelInitFailed = errors.New("opentelemetry initialization failed")

	// ErrInvalidOTelConfig is returned when the OTel configuration is invalid.
	ErrInvalidOTelConfig = errors.New("invalid opentelemetry configuration")
)

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// OTelConfig configures the OpenTelemetry sink.
//
// Description:
//
//	OTelConfig specifies service name, instrumentation scope, and optional
//	providers for tracing and metrics.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type OTelConfig struct {
	// ServiceName is the service name for telemetry.
	// Required.
	ServiceName string

	// ServiceVersion is the service version for telemetry.
	// Optional.
	ServiceVersion string

	// TracerProvider is the tracer provider to use.
	// If nil, uses the global tracer provider.
	TracerProvider trace.TracerProvider

	// MeterProvider is the meter provider to use.
	// If nil, uses the global meter provider.
	MeterProvider metric.MeterProvider

	// TraceEnabled enables trace span creation.
	// Default: true.
	TraceEnabled bool

	// MetricsEnabled enables metric recording.
	// Default: true.
	MetricsEnabled bool
}

// DefaultOTelConfig returns a configuration with sensible defaults.
//
// Description:
//
//	Returns an OTelConfig with default service name and both tracing
//	and metrics enabled.
//
// Outputs:
//   - *OTelConfig: Configuration with defaults applied.
//
// Thread Safety: Stateless function; safe for concurrent use.
//
// Example:
//
//	config := telemetry.DefaultOTelConfig()
//	config.ServiceName = "my-service"
//	sink, err := telemetry.NewOTelSink(config)
func DefaultOTelConfig() *OTelConfig {
	return &OTelConfig{
		ServiceName:    "code-buddy-eval",
		ServiceVersion: "1.0.0",
		TraceEnabled:   true,
		MetricsEnabled: true,
	}
}

// Validate checks that the configuration is valid.
//
// Description:
//
//	Validates that required fields are set.
//
// Outputs:
//   - error: Non-nil if configuration is invalid.
//
// Thread Safety: Safe for concurrent use.
func (c *OTelConfig) Validate() error {
	if c.ServiceName == "" {
		return errors.New("service name is required")
	}
	return nil
}

// -----------------------------------------------------------------------------
// OpenTelemetry Sink
// -----------------------------------------------------------------------------

// OTelSink exports telemetry via OpenTelemetry.
//
// Description:
//
//	OTelSink creates trace spans for operations and records metrics
//	using the OpenTelemetry SDK. It integrates with the standard
//	OTel providers for flexible backend configuration.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	config := telemetry.DefaultOTelConfig()
//	config.ServiceName = "code-buddy-eval"
//
//	sink, err := telemetry.NewOTelSink(config)
//	if err != nil {
//	    return fmt.Errorf("create otel sink: %w", err)
//	}
//	defer sink.Close()
//
//	sink.RecordBenchmark(ctx, data)
type OTelSink struct {
	config *OTelConfig
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	benchmarkDuration    metric.Float64Histogram
	benchmarkIterations  metric.Int64Counter
	benchmarkLatencyMean metric.Float64Histogram
	benchmarkLatencyP99  metric.Float64Histogram
	benchmarkThroughput  metric.Float64Histogram
	benchmarkMemory      metric.Int64Histogram
	benchmarkErrors      metric.Int64Counter
	comparisonSpeedup    metric.Float64Gauge
	comparisonTotal      metric.Int64Counter
	errorsTotal          metric.Int64Counter

	mu     sync.RWMutex
	closed bool
}

// NewOTelSink creates a new OpenTelemetry telemetry sink.
//
// Description:
//
//	Creates a sink that exports telemetry via OpenTelemetry traces and metrics.
//	Uses global providers if not specified in config.
//
// Inputs:
//   - config: OpenTelemetry configuration. Must not be nil.
//
// Outputs:
//   - *OTelSink: The created sink. Never nil on success.
//   - error: Non-nil if configuration is invalid or initialization fails.
//
// Thread Safety: The returned sink is safe for concurrent use.
//
// Example:
//
//	config := telemetry.DefaultOTelConfig()
//	config.ServiceName = "my-service"
//	sink, err := telemetry.NewOTelSink(config)
//	if err != nil {
//	    return fmt.Errorf("create sink: %w", err)
//	}
//
// Limitations:
//   - Requires OpenTelemetry providers to be configured for actual export.
//   - Without providers, telemetry is discarded (no-op).
//
// Assumptions:
//   - TracerProvider and MeterProvider are properly initialized.
//   - Caller is responsible for shutting down providers.
func NewOTelSink(config *OTelConfig) (*OTelSink, error) {
	if config == nil {
		return nil, ErrInvalidOTelConfig
	}
	if err := config.Validate(); err != nil {
		return nil, errors.Join(ErrInvalidOTelConfig, err)
	}

	// Copy config to avoid mutation
	cfg := *config

	// Get providers
	tp := cfg.TracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	mp := cfg.MeterProvider
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	// Create tracer and meter
	tracer := tp.Tracer(
		"github.com/AleutianAI/AleutianFOSS/services/trace/eval/telemetry",
		trace.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	meter := mp.Meter(
		"github.com/AleutianAI/AleutianFOSS/services/trace/eval/telemetry",
		metric.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	sink := &OTelSink{
		config: &cfg,
		tracer: tracer,
		meter:  meter,
	}

	// Initialize metrics if enabled
	if cfg.MetricsEnabled {
		if err := sink.initializeMetrics(); err != nil {
			return nil, errors.Join(ErrOTelInitFailed, err)
		}
	}

	return sink, nil
}

// initializeMetrics creates all metric instruments.
func (s *OTelSink) initializeMetrics() error {
	var err error

	// Benchmark metrics
	s.benchmarkDuration, err = s.meter.Float64Histogram(
		"benchmark.duration",
		metric.WithDescription("Total benchmark duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	s.benchmarkIterations, err = s.meter.Int64Counter(
		"benchmark.iterations",
		metric.WithDescription("Total benchmark iterations"),
		metric.WithUnit("{iteration}"),
	)
	if err != nil {
		return err
	}

	s.benchmarkLatencyMean, err = s.meter.Float64Histogram(
		"benchmark.latency.mean",
		metric.WithDescription("Mean latency in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	s.benchmarkLatencyP99, err = s.meter.Float64Histogram(
		"benchmark.latency.p99",
		metric.WithDescription("P99 latency in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	s.benchmarkThroughput, err = s.meter.Float64Histogram(
		"benchmark.throughput",
		metric.WithDescription("Throughput in operations per second"),
		metric.WithUnit("{operation}/s"),
	)
	if err != nil {
		return err
	}

	s.benchmarkMemory, err = s.meter.Int64Histogram(
		"benchmark.memory",
		metric.WithDescription("Memory allocation in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return err
	}

	s.benchmarkErrors, err = s.meter.Int64Counter(
		"benchmark.errors",
		metric.WithDescription("Total benchmark errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return err
	}

	// Comparison metrics
	s.comparisonSpeedup, err = s.meter.Float64Gauge(
		"comparison.speedup",
		metric.WithDescription("Comparison speedup ratio"),
		metric.WithUnit("{ratio}"),
	)
	if err != nil {
		return err
	}

	s.comparisonTotal, err = s.meter.Int64Counter(
		"comparison.total",
		metric.WithDescription("Total comparisons performed"),
		metric.WithUnit("{comparison}"),
	)
	if err != nil {
		return err
	}

	// Error metrics
	s.errorsTotal, err = s.meter.Int64Counter(
		"errors.total",
		metric.WithDescription("Total errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// RecordBenchmark records benchmark telemetry.
//
// Description:
//
//	Creates a trace span for the benchmark operation and records
//	metrics for duration, iterations, latency, throughput, and errors.
//
// Inputs:
//   - ctx: Context for tracing. Must not be nil.
//   - data: Benchmark data to record. Must not be nil.
//
// Outputs:
//   - error: Non-nil if sink is closed or inputs are invalid.
//
// Thread Safety: Safe for concurrent use.
func (s *OTelSink) RecordBenchmark(ctx context.Context, data *BenchmarkData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSinkClosed
	}
	s.mu.RUnlock()

	name := data.Name
	if name == "" {
		name = "unknown"
	}

	// Common attributes
	attrs := []attribute.KeyValue{
		attribute.String("benchmark.name", name),
		attribute.Int("benchmark.iterations", data.Iterations),
	}

	// Add label attributes
	for k, v := range data.Labels {
		attrs = append(attrs, attribute.String("label."+k, v))
	}

	// Create span if tracing enabled
	if s.config.TraceEnabled {
		_, span := s.tracer.Start(ctx, "benchmark.record",
			trace.WithAttributes(attrs...),
			trace.WithTimestamp(data.Timestamp),
		)

		// Add latency attributes to span
		span.SetAttributes(
			attribute.Float64("latency.min_seconds", data.Latency.Min.Seconds()),
			attribute.Float64("latency.max_seconds", data.Latency.Max.Seconds()),
			attribute.Float64("latency.mean_seconds", data.Latency.Mean.Seconds()),
			attribute.Float64("latency.p99_seconds", data.Latency.P99.Seconds()),
			attribute.Float64("throughput.ops_per_second", data.Throughput.OpsPerSecond),
			attribute.Int("error_count", data.ErrorCount),
			attribute.Float64("error_rate", data.ErrorRate),
		)

		// Add memory attributes if present
		if data.Memory != nil {
			span.SetAttributes(
				attribute.Int64("memory.heap_before", int64(data.Memory.HeapAllocBefore)),
				attribute.Int64("memory.heap_after", int64(data.Memory.HeapAllocAfter)),
				attribute.Int64("memory.heap_delta", data.Memory.HeapAllocDelta),
				attribute.Int("memory.gc_pauses", int(data.Memory.GCPauses)),
			)
		}

		if data.ErrorCount > 0 {
			span.SetStatus(codes.Error, "benchmark had errors")
		}

		span.End()
	}

	// Record metrics if enabled
	if s.config.MetricsEnabled {
		attrSet := metric.WithAttributes(attrs...)

		s.benchmarkDuration.Record(ctx, data.Duration.Seconds(), attrSet)
		s.benchmarkIterations.Add(ctx, int64(data.Iterations), attrSet)
		s.benchmarkLatencyMean.Record(ctx, data.Latency.Mean.Seconds(), attrSet)
		s.benchmarkLatencyP99.Record(ctx, data.Latency.P99.Seconds(), attrSet)
		s.benchmarkThroughput.Record(ctx, data.Throughput.OpsPerSecond, attrSet)

		if data.Memory != nil {
			s.benchmarkMemory.Record(ctx, data.Memory.HeapAllocDelta, attrSet)
		}

		if data.ErrorCount > 0 {
			s.benchmarkErrors.Add(ctx, int64(data.ErrorCount), attrSet)
		}
	}

	return nil
}

// RecordComparison records comparison telemetry.
//
// Description:
//
//	Creates a trace span for the comparison operation and records
//	metrics for speedup and statistical significance.
//
// Inputs:
//   - ctx: Context for tracing. Must not be nil.
//   - data: Comparison data to record. Must not be nil.
//
// Outputs:
//   - error: Non-nil if sink is closed or inputs are invalid.
//
// Thread Safety: Safe for concurrent use.
func (s *OTelSink) RecordComparison(ctx context.Context, data *ComparisonData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSinkClosed
	}
	s.mu.RUnlock()

	winner := data.Winner
	if winner == "" {
		winner = "none"
	}

	// Common attributes
	attrs := []attribute.KeyValue{
		attribute.String("comparison.winner", winner),
		attribute.Bool("comparison.significant", data.Significant),
		attribute.Float64("comparison.speedup", data.Speedup),
		attribute.Float64("comparison.p_value", data.PValue),
		attribute.Float64("comparison.effect_size", data.EffectSize),
		attribute.String("comparison.effect_size_category", data.EffectSizeCategory),
		attribute.StringSlice("comparison.components", data.Components),
	}

	// Add label attributes
	for k, v := range data.Labels {
		attrs = append(attrs, attribute.String("label."+k, v))
	}

	// Create span if tracing enabled
	if s.config.TraceEnabled {
		_, span := s.tracer.Start(ctx, "comparison.record",
			trace.WithAttributes(attrs...),
			trace.WithTimestamp(data.Timestamp),
		)
		span.End()
	}

	// Record metrics if enabled
	if s.config.MetricsEnabled {
		attrSet := metric.WithAttributes(
			attribute.String("winner", winner),
			attribute.Bool("significant", data.Significant),
		)

		s.comparisonSpeedup.Record(ctx, data.Speedup, attrSet)
		s.comparisonTotal.Add(ctx, 1, attrSet)
	}

	return nil
}

// RecordError records error telemetry.
//
// Description:
//
//	Creates a trace span for the error event and increments
//	the error counter metric.
//
// Inputs:
//   - ctx: Context for tracing. Must not be nil.
//   - data: Error data to record. Must not be nil.
//
// Outputs:
//   - error: Non-nil if sink is closed or inputs are invalid.
//
// Thread Safety: Safe for concurrent use.
func (s *OTelSink) RecordError(ctx context.Context, data *ErrorData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrSinkClosed
	}
	s.mu.RUnlock()

	component := data.Component
	if component == "" {
		component = "unknown"
	}
	operation := data.Operation
	if operation == "" {
		operation = "unknown"
	}
	errorType := data.ErrorType
	if errorType == "" {
		errorType = "unknown"
	}

	// Common attributes
	attrs := []attribute.KeyValue{
		attribute.String("error.component", component),
		attribute.String("error.operation", operation),
		attribute.String("error.type", errorType),
		attribute.String("error.message", data.Message),
	}

	// Add label attributes
	for k, v := range data.Labels {
		attrs = append(attrs, attribute.String("label."+k, v))
	}

	// Create span if tracing enabled
	if s.config.TraceEnabled {
		_, span := s.tracer.Start(ctx, "error.record",
			trace.WithAttributes(attrs...),
			trace.WithTimestamp(data.Timestamp),
		)
		span.SetStatus(codes.Error, data.Message)
		span.End()
	}

	// Record metrics if enabled
	if s.config.MetricsEnabled {
		attrSet := metric.WithAttributes(
			attribute.String("component", component),
			attribute.String("operation", operation),
			attribute.String("error_type", errorType),
		)
		s.errorsTotal.Add(ctx, 1, attrSet)
	}

	return nil
}

// Flush forces export of any buffered telemetry.
//
// Description:
//
//	For OTel sink, this is a no-op as the SDK handles batching and export.
//	The actual flush happens via the providers' ForceFlush methods.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - error: Non-nil if sink is closed or context is nil.
//
// Thread Safety: Safe for concurrent use.
func (s *OTelSink) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrSinkClosed
	}

	// Note: Actual flushing is done via the provider's ForceFlush method
	// which should be called on the TracerProvider and MeterProvider directly.
	// This sink doesn't own the providers, so we don't flush them here.
	return nil
}

// Close releases resources.
//
// Description:
//
//	Marks the sink as closed. Does not shut down the providers as they
//	may be shared and should be managed by the caller.
//
// Outputs:
//   - error: Always nil.
//
// Thread Safety: Safe for concurrent use. Idempotent.
func (s *OTelSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Note: We don't shut down the providers here as they may be shared.
	// The caller is responsible for shutting down the providers.
	return nil
}

// -----------------------------------------------------------------------------
// Trace Context Helpers
// -----------------------------------------------------------------------------

// StartBenchmarkSpan creates a trace span for a benchmark operation.
//
// Description:
//
//	Creates a new span with benchmark attributes. The returned span
//	must be ended by the caller.
//
// Inputs:
//   - ctx: Context for tracing. Must not be nil.
//   - name: Benchmark name.
//
// Outputs:
//   - context.Context: Context with the span.
//   - trace.Span: The created span. Must be ended by caller.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	ctx, span := sink.StartBenchmarkSpan(ctx, "my_benchmark")
//	defer span.End()
//	// ... run benchmark ...
//	span.SetAttributes(attribute.Int("iterations", 1000))
func (s *OTelSink) StartBenchmarkSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		name = "unknown"
	}

	return s.tracer.Start(ctx, "benchmark."+name,
		trace.WithAttributes(
			attribute.String("benchmark.name", name),
		),
	)
}

// AddBenchmarkEvent adds an event to the current span.
//
// Description:
//
//	Adds a timestamped event to the span in the context with the given
//	name and attributes.
//
// Inputs:
//   - ctx: Context containing the span.
//   - eventName: Name of the event.
//   - attrs: Event attributes.
//
// Thread Safety: Safe for concurrent use.
func (s *OTelSink) AddBenchmarkEvent(ctx context.Context, eventName string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		span.AddEvent(eventName, trace.WithAttributes(attrs...))
	}
}

// Verify interface compliance at compile time.
var _ Sink = (*OTelSink)(nil)
