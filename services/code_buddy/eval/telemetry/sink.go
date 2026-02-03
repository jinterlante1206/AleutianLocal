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
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNilContext is returned when a nil context is provided.
	ErrNilContext = errors.New("context must not be nil")

	// ErrNilData is returned when nil data is provided to a recording method.
	ErrNilData = errors.New("data must not be nil")

	// ErrSinkClosed is returned when attempting to use a closed sink.
	ErrSinkClosed = errors.New("sink has been closed")

	// ErrNoSinks is returned when creating a composite sink with no children.
	ErrNoSinks = errors.New("at least one sink is required")

	// ErrFlushTimeout is returned when flush exceeds the timeout.
	ErrFlushTimeout = errors.New("flush operation timed out")
)

// -----------------------------------------------------------------------------
// Interface
// -----------------------------------------------------------------------------

// Sink defines the interface for telemetry data collection.
//
// Description:
//
//	Sink is the primary abstraction for recording evaluation telemetry.
//	Implementations handle the specific export format (Prometheus, OTel, etc.).
//
// Thread Safety: All implementations must be safe for concurrent use.
//
// Example:
//
//	sink := telemetry.NewPrometheusSink(config)
//	defer sink.Close()
//
//	if err := sink.RecordBenchmark(ctx, result); err != nil {
//	    log.Printf("telemetry error: %v", err)
//	}
type Sink interface {
	// RecordBenchmark records a single benchmark result.
	//
	// Description:
	//   Records latency, throughput, memory, and error metrics from a benchmark run.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - data: Benchmark data to record. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if recording fails or sink is closed.
	//
	// Thread Safety: Safe for concurrent use.
	RecordBenchmark(ctx context.Context, data *BenchmarkData) error

	// RecordComparison records a benchmark comparison result.
	//
	// Description:
	//   Records comparison metrics including speedup, statistical significance,
	//   and effect size.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - data: Comparison data to record. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if recording fails or sink is closed.
	//
	// Thread Safety: Safe for concurrent use.
	RecordComparison(ctx context.Context, data *ComparisonData) error

	// RecordError records an error event.
	//
	// Description:
	//   Records error occurrences for alerting and debugging.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - data: Error data to record. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if recording fails or sink is closed.
	//
	// Thread Safety: Safe for concurrent use.
	RecordError(ctx context.Context, data *ErrorData) error

	// Flush ensures all buffered data is exported.
	//
	// Description:
	//   Forces export of any buffered telemetry data. Called automatically
	//   on Close(), but can be called explicitly for immediate export.
	//
	// Inputs:
	//   - ctx: Context for cancellation and timeout. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if flush fails or times out.
	//
	// Thread Safety: Safe for concurrent use.
	Flush(ctx context.Context) error

	// Close releases resources and flushes pending data.
	//
	// Description:
	//   Gracefully shuts down the sink, flushing any buffered data.
	//   After Close(), all recording methods return ErrSinkClosed.
	//
	// Outputs:
	//   - error: Non-nil if shutdown fails.
	//
	// Thread Safety: Safe for concurrent use. Idempotent.
	Close() error
}

// -----------------------------------------------------------------------------
// Data Types
// -----------------------------------------------------------------------------

// BenchmarkData contains data for a benchmark result recording.
//
// Description:
//
//	BenchmarkData captures all metrics from a single benchmark run,
//	including latency statistics, throughput, memory usage, and metadata.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type BenchmarkData struct {
	// Name is the benchmark/component identifier.
	Name string

	// Timestamp is when the benchmark was recorded.
	Timestamp time.Time

	// Duration is the total benchmark duration.
	Duration time.Duration

	// Iterations is the number of iterations executed.
	Iterations int

	// Latency contains latency statistics.
	Latency LatencyData

	// Throughput contains throughput metrics.
	Throughput ThroughputData

	// Memory contains memory statistics (optional).
	Memory *MemoryData

	// Labels are additional key-value pairs for filtering.
	Labels map[string]string

	// ErrorCount is the number of errors during the benchmark.
	ErrorCount int

	// ErrorRate is the proportion of iterations that failed (0.0-1.0).
	ErrorRate float64
}

// LatencyData contains latency statistics.
//
// Description:
//
//	LatencyData captures the full distribution of latency measurements
//	including min, max, mean, standard deviation, and percentiles.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type LatencyData struct {
	// Min is the minimum observed latency.
	Min time.Duration

	// Max is the maximum observed latency.
	Max time.Duration

	// Mean is the arithmetic mean latency.
	Mean time.Duration

	// Median is the 50th percentile latency.
	Median time.Duration

	// StdDev is the standard deviation.
	StdDev time.Duration

	// P50 is the 50th percentile.
	P50 time.Duration

	// P90 is the 90th percentile.
	P90 time.Duration

	// P95 is the 95th percentile.
	P95 time.Duration

	// P99 is the 99th percentile.
	P99 time.Duration

	// P999 is the 99.9th percentile.
	P999 time.Duration
}

// ThroughputData contains throughput metrics.
//
// Description:
//
//	ThroughputData captures the rate of operations.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type ThroughputData struct {
	// OpsPerSecond is the operations per second.
	OpsPerSecond float64
}

// MemoryData contains memory statistics.
//
// Description:
//
//	MemoryData captures heap allocation and GC metrics.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type MemoryData struct {
	// HeapAllocBefore is heap allocation before the benchmark.
	HeapAllocBefore uint64

	// HeapAllocAfter is heap allocation after the benchmark.
	HeapAllocAfter uint64

	// HeapAllocDelta is the change in heap allocation.
	HeapAllocDelta int64

	// GCPauses is the number of GC pauses during the benchmark.
	GCPauses uint32

	// GCPauseTotal is the total time spent in GC pauses.
	GCPauseTotal time.Duration
}

// ComparisonData contains data for a benchmark comparison recording.
//
// Description:
//
//	ComparisonData captures the results of comparing two or more
//	benchmark runs, including statistical analysis.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type ComparisonData struct {
	// Timestamp is when the comparison was recorded.
	Timestamp time.Time

	// Components lists the compared component names.
	Components []string

	// Winner is the name of the best-performing component (empty if no winner).
	Winner string

	// Speedup is the performance ratio (winner vs runner-up).
	Speedup float64

	// Significant indicates if the difference is statistically significant.
	Significant bool

	// PValue is the statistical p-value.
	PValue float64

	// ConfidenceLevel is the confidence level used (e.g., 0.95).
	ConfidenceLevel float64

	// EffectSize is Cohen's d effect size.
	EffectSize float64

	// EffectSizeCategory is the effect size interpretation.
	EffectSizeCategory string

	// Labels are additional key-value pairs for filtering.
	Labels map[string]string
}

// ErrorData contains data for an error recording.
//
// Description:
//
//	ErrorData captures error events for alerting and debugging.
//
// Thread Safety: Immutable after creation; safe for concurrent read access.
type ErrorData struct {
	// Timestamp is when the error occurred.
	Timestamp time.Time

	// Component is the component that produced the error.
	Component string

	// Operation is the operation that failed.
	Operation string

	// ErrorType categorizes the error (e.g., "timeout", "validation", "internal").
	ErrorType string

	// Message is the error message (should not contain PII).
	Message string

	// Labels are additional key-value pairs for filtering.
	Labels map[string]string
}

// -----------------------------------------------------------------------------
// Composite Sink
// -----------------------------------------------------------------------------

// CompositeSink multiplexes telemetry to multiple sinks.
//
// Description:
//
//	CompositeSink allows sending telemetry data to multiple backends
//	simultaneously (e.g., Prometheus and OpenTelemetry).
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	composite := telemetry.NewCompositeSink(promSink, otelSink)
//	defer composite.Close()
//
//	// Records to both Prometheus and OpenTelemetry
//	composite.RecordBenchmark(ctx, data)
type CompositeSink struct {
	sinks  []Sink
	mu     sync.RWMutex
	closed bool
}

// NewCompositeSink creates a new composite sink.
//
// Description:
//
//	Creates a sink that forwards all telemetry to multiple child sinks.
//	Errors from individual sinks are collected and returned as a combined error.
//
// Inputs:
//   - sinks: Child sinks to forward to. At least one required.
//
// Outputs:
//   - *CompositeSink: The created composite sink. Never nil on success.
//   - error: ErrNoSinks if no sinks provided.
//
// Thread Safety: The returned sink is safe for concurrent use.
//
// Example:
//
//	composite, err := telemetry.NewCompositeSink(promSink, otelSink)
//	if err != nil {
//	    return fmt.Errorf("create composite sink: %w", err)
//	}
//
// Limitations:
//   - All child sinks receive all data; no filtering per sink.
//   - Errors from multiple sinks are joined; individual failures don't stop others.
//
// Assumptions:
//   - Child sinks are properly initialized.
//   - Child sinks remain valid for the lifetime of the composite.
func NewCompositeSink(sinks ...Sink) (*CompositeSink, error) {
	if len(sinks) == 0 {
		return nil, ErrNoSinks
	}

	// Filter out nil sinks
	validSinks := make([]Sink, 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			validSinks = append(validSinks, s)
		}
	}

	if len(validSinks) == 0 {
		return nil, ErrNoSinks
	}

	return &CompositeSink{
		sinks: validSinks,
	}, nil
}

// RecordBenchmark records a benchmark result to all child sinks.
//
// Description:
//
//	Forwards the benchmark data to all child sinks. Errors from individual
//	sinks are collected and returned as a combined error; one sink's failure
//	does not prevent others from receiving the data.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - data: Benchmark data to record. Must not be nil.
//
// Outputs:
//   - error: Combined errors from child sinks, or nil if all succeed.
//
// Thread Safety: Safe for concurrent use.
func (c *CompositeSink) RecordBenchmark(ctx context.Context, data *BenchmarkData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return ErrSinkClosed
	}
	sinks := c.sinks
	c.mu.RUnlock()

	var errs []error
	for _, sink := range sinks {
		if err := sink.RecordBenchmark(ctx, data); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// RecordComparison records a comparison result to all child sinks.
//
// Description:
//
//	Forwards the comparison data to all child sinks. Errors from individual
//	sinks are collected and returned as a combined error.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - data: Comparison data to record. Must not be nil.
//
// Outputs:
//   - error: Combined errors from child sinks, or nil if all succeed.
//
// Thread Safety: Safe for concurrent use.
func (c *CompositeSink) RecordComparison(ctx context.Context, data *ComparisonData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return ErrSinkClosed
	}
	sinks := c.sinks
	c.mu.RUnlock()

	var errs []error
	for _, sink := range sinks {
		if err := sink.RecordComparison(ctx, data); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// RecordError records an error to all child sinks.
//
// Description:
//
//	Forwards the error data to all child sinks. Errors from individual
//	sinks are collected and returned as a combined error.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - data: Error data to record. Must not be nil.
//
// Outputs:
//   - error: Combined errors from child sinks, or nil if all succeed.
//
// Thread Safety: Safe for concurrent use.
func (c *CompositeSink) RecordError(ctx context.Context, data *ErrorData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return ErrSinkClosed
	}
	sinks := c.sinks
	c.mu.RUnlock()

	var errs []error
	for _, sink := range sinks {
		if err := sink.RecordError(ctx, data); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Flush flushes all child sinks.
//
// Description:
//
//	Flushes all child sinks concurrently. Waits for all to complete
//	or context cancellation.
//
// Inputs:
//   - ctx: Context for cancellation and timeout. Must not be nil.
//
// Outputs:
//   - error: Combined errors from child sinks, or nil if all succeed.
//
// Thread Safety: Safe for concurrent use.
func (c *CompositeSink) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return ErrSinkClosed
	}
	sinks := c.sinks
	c.mu.RUnlock()

	var wg sync.WaitGroup
	errChan := make(chan error, len(sinks))

	for _, sink := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			if err := s.Flush(ctx); err != nil {
				errChan <- err
			}
		}(sink)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// Close closes all child sinks.
//
// Description:
//
//	Closes all child sinks and releases resources. Idempotent.
//
// Outputs:
//   - error: Combined errors from child sinks, or nil if all succeed.
//
// Thread Safety: Safe for concurrent use. Idempotent.
func (c *CompositeSink) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	sinks := c.sinks
	c.mu.Unlock()

	var errs []error
	for _, sink := range sinks {
		if err := sink.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// -----------------------------------------------------------------------------
// No-Op Sink
// -----------------------------------------------------------------------------

// NoOpSink is a sink that discards all data.
//
// Description:
//
//	NoOpSink is useful for testing and as a default when no telemetry
//	is configured.
//
// Thread Safety: Safe for concurrent use.
type NoOpSink struct{}

// NewNoOpSink creates a new no-op sink.
//
// Description:
//
//	Creates a sink that accepts but discards all telemetry data.
//
// Outputs:
//   - *NoOpSink: The created sink. Never nil.
//
// Thread Safety: The returned sink is safe for concurrent use.
func NewNoOpSink() *NoOpSink {
	return &NoOpSink{}
}

// RecordBenchmark discards the benchmark data.
//
// Thread Safety: Safe for concurrent use.
func (n *NoOpSink) RecordBenchmark(ctx context.Context, data *BenchmarkData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	return nil
}

// RecordComparison discards the comparison data.
//
// Thread Safety: Safe for concurrent use.
func (n *NoOpSink) RecordComparison(ctx context.Context, data *ComparisonData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	return nil
}

// RecordError discards the error data.
//
// Thread Safety: Safe for concurrent use.
func (n *NoOpSink) RecordError(ctx context.Context, data *ErrorData) error {
	if ctx == nil {
		return ErrNilContext
	}
	if data == nil {
		return ErrNilData
	}
	return nil
}

// Flush does nothing.
//
// Thread Safety: Safe for concurrent use.
func (n *NoOpSink) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return nil
}

// Close does nothing.
//
// Thread Safety: Safe for concurrent use.
func (n *NoOpSink) Close() error {
	return nil
}

// Verify interface compliance at compile time.
var (
	_ Sink = (*CompositeSink)(nil)
	_ Sink = (*NoOpSink)(nil)
)
