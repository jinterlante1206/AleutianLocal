// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package algorithms

import (
	"context"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Algorithm Interface
// -----------------------------------------------------------------------------

// Algorithm is a pure function that processes CRS state.
//
// Description:
//
//	Algorithms are the core computational units of the MCTS system. They:
//	- Read from immutable snapshots
//	- Produce typed deltas describing state changes
//	- Run in goroutines with timeout enforcement
//	- Support cancellation and partial results
//
// CANCELLATION CONTRACT:
//   - Algorithms MUST check ctx.Done() at regular intervals (every 100ms)
//   - Algorithms MUST call ReportProgress() to avoid deadlock detection
//   - Algorithms SHOULD return partial results when cancelled
//   - Algorithms MUST NOT ignore cancellation for more than 100ms
//
// Thread Safety: Must be safe for concurrent execution.
type Algorithm interface {
	eval.Evaluable

	// Process executes the algorithm's core logic.
	//
	// Description:
	//
	//   This is the pure function core. It MUST NOT mutate snapshot or input.
	//   It MUST respect context cancellation and SHOULD return partial results
	//   when cancelled.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - snapshot: Immutable view of CRS. Must not be nil.
	//   - input: Algorithm-specific input. Type determined by InputType().
	//
	// Outputs:
	//   - any: Algorithm-specific output. Type determined by OutputType().
	//   - crs.Delta: State changes to apply. May be nil if no changes.
	//   - error: Non-nil on failure. May include partial results.
	Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error)

	// Timeout returns the maximum execution time.
	//
	// Description:
	//
	//   After this duration, the algorithm will be auto-cancelled by the runner.
	//   Algorithms should choose conservative timeouts.
	//
	// Outputs:
	//   - time.Duration: Maximum allowed execution time.
	Timeout() time.Duration

	// InputType returns the expected input type.
	//
	// Description:
	//
	//   Used for validation before calling Process().
	//
	// Outputs:
	//   - reflect.Type: The expected input type.
	InputType() reflect.Type

	// OutputType returns the output type.
	//
	// Description:
	//
	//   Used for validation and type-safe result handling.
	//
	// Outputs:
	//   - reflect.Type: The output type.
	OutputType() reflect.Type

	// ProgressInterval returns how often progress should be reported.
	//
	// Description:
	//
	//   If no progress is reported for 3x this interval, deadlock is assumed
	//   and the algorithm is cancelled.
	//
	// Outputs:
	//   - time.Duration: Progress reporting interval. Default: 1 second.
	ProgressInterval() time.Duration

	// SupportsPartialResults returns whether partial results are meaningful.
	//
	// Description:
	//
	//   If true, the algorithm can return useful partial results when cancelled.
	//   This is used by the runner to decide whether to wait for results.
	//
	// Outputs:
	//   - bool: True if partial results are supported.
	SupportsPartialResults() bool
}

// -----------------------------------------------------------------------------
// Algorithm Result
// -----------------------------------------------------------------------------

// Result wraps the output of an algorithm execution.
//
// Description:
//
//	Result is returned by the Runner and contains the algorithm's output,
//	delta, timing information, and any errors.
//
// Thread Safety: Immutable after creation.
type Result struct {
	// Name is the algorithm name (from Algorithm.Name()).
	Name string

	// Output is the algorithm-specific output.
	Output any

	// Delta is the state changes to apply.
	Delta crs.Delta

	// Err is non-nil if the algorithm failed.
	Err error

	// Duration is how long the algorithm ran.
	Duration time.Duration

	// StartTime is when the algorithm started.
	StartTime time.Time

	// EndTime is when the algorithm finished.
	EndTime time.Time

	// Cancelled is true if the algorithm was cancelled.
	Cancelled bool

	// Partial is true if the result is a partial result.
	Partial bool

	// Metrics contains algorithm-specific metrics.
	Metrics map[string]float64
}

// Success returns true if the algorithm succeeded without error.
func (r *Result) Success() bool {
	return r.Err == nil && !r.Cancelled
}

// -----------------------------------------------------------------------------
// Algorithm Config
// -----------------------------------------------------------------------------

// Config provides common configuration for algorithms.
type Config struct {
	// Timeout overrides the algorithm's default timeout.
	Timeout time.Duration

	// ProgressInterval overrides the default progress interval.
	ProgressInterval time.Duration

	// MaxIterations limits the number of iterations.
	MaxIterations int

	// EnableMetrics enables detailed metrics collection.
	EnableMetrics bool

	// EnableTracing enables OpenTelemetry tracing.
	EnableTracing bool
}

// DefaultConfig returns the default algorithm configuration.
func DefaultConfig() *Config {
	return &Config{
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
		MaxIterations:    1000,
		EnableMetrics:    true,
		EnableTracing:    true,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Timeout < 0 {
		return ErrInvalidConfig
	}
	if c.ProgressInterval < 0 {
		return ErrInvalidConfig
	}
	if c.MaxIterations < 0 {
		return ErrInvalidConfig
	}
	return nil
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

// Common algorithm errors.
var (
	// ErrNilContext is returned when context is nil.
	ErrNilContext = crs.ErrNilContext

	// ErrNilSnapshot is returned when snapshot is nil.
	ErrNilSnapshot = crs.ErrIndexNotFound

	// ErrNilInput is returned when input is nil.
	ErrNilInput = crs.ErrNilDelta

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = crs.ErrDeltaValidation

	// ErrTimeout is returned when the algorithm times out.
	ErrTimeout = context.DeadlineExceeded

	// ErrCancelled is returned when the algorithm is cancelled.
	ErrCancelled = context.Canceled
)

// AlgorithmError wraps an error with algorithm context.
type AlgorithmError struct {
	Algorithm string
	Operation string
	Err       error
}

func (e *AlgorithmError) Error() string {
	return e.Algorithm + "." + e.Operation + ": " + e.Err.Error()
}

func (e *AlgorithmError) Unwrap() error {
	return e.Err
}

// NewAlgorithmError creates a new algorithm error.
func NewAlgorithmError(algorithm, operation string, err error) *AlgorithmError {
	return &AlgorithmError{
		Algorithm: algorithm,
		Operation: operation,
		Err:       err,
	}
}
