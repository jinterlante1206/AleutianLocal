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
	"log/slog"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Runner
// -----------------------------------------------------------------------------

// Runner executes algorithms in goroutines and collects results via channels.
//
// Description:
//
//	Runner is the central coordinator for algorithm execution. It:
//	- Spawns goroutines for each algorithm
//	- Enforces timeouts and cancellation
//	- Collects results via channels
//	- Merges deltas into a composite
//
// Thread Safety: Safe for concurrent use.
type Runner struct {
	mu      sync.Mutex
	results chan *Result
	wg      sync.WaitGroup
	logger  *slog.Logger

	// Tracking
	started   int
	completed int
}

// NewRunner creates a new algorithm runner.
//
// Inputs:
//   - capacity: Buffer size for results channel. Default: 10.
//
// Outputs:
//   - *Runner: The new runner.
func NewRunner(capacity int) *Runner {
	if capacity <= 0 {
		capacity = 10
	}
	return &Runner{
		results: make(chan *Result, capacity),
		logger:  slog.Default().With(slog.String("component", "algorithm_runner")),
	}
}

// Run starts an algorithm in a goroutine.
//
// Description:
//
//	The algorithm runs with timeout enforcement. Results are sent to
//	the internal channel for collection.
//
// Inputs:
//   - ctx: Parent context. Algorithm runs with derived context.
//   - algo: The algorithm to run.
//   - snapshot: Immutable CRS snapshot.
//   - input: Algorithm-specific input.
//
// Thread Safety: Safe for concurrent calls.
func (r *Runner) Run(ctx context.Context, algo Algorithm, snapshot crs.Snapshot, input any) {
	r.mu.Lock()
	r.started++
	r.mu.Unlock()

	r.wg.Add(1)
	go r.runAlgorithm(ctx, algo, snapshot, input)
}

// runAlgorithm executes a single algorithm with timeout and tracing.
func (r *Runner) runAlgorithm(ctx context.Context, algo Algorithm, snapshot crs.Snapshot, input any) {
	defer r.wg.Done()

	name := algo.Name()
	startTime := time.Now()

	// Create timeout context
	timeout := algo.Timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Start span
	ctx, span := otel.Tracer("algorithms").Start(ctx, "algorithm."+name,
		trace.WithAttributes(
			attribute.String("algorithm", name),
			attribute.String("timeout", timeout.String()),
		),
	)
	defer span.End()

	result := &Result{
		Name:      name,
		StartTime: startTime.UnixMilli(),
		Metrics:   make(map[string]float64),
	}

	// Execute algorithm
	output, delta, err := algo.Process(ctx, snapshot, input)

	// Record completion
	endTime := time.Now()
	result.EndTime = endTime.UnixMilli()
	result.Duration = endTime.Sub(startTime)
	result.Output = output
	result.Delta = delta
	result.Err = err

	// Check if cancelled
	if ctx.Err() != nil {
		result.Cancelled = true
		result.Partial = algo.SupportsPartialResults()
		if err == nil {
			result.Err = ctx.Err()
		}
	}

	// Record span attributes
	span.SetAttributes(
		attribute.Int64("duration_ms", result.Duration.Milliseconds()),
		attribute.Bool("success", result.Success()),
		attribute.Bool("cancelled", result.Cancelled),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	// Log result
	if err != nil && !result.Cancelled {
		r.logger.Warn("algorithm failed",
			slog.String("algorithm", name),
			slog.Duration("duration", result.Duration),
			slog.String("error", err.Error()),
		)
	} else {
		r.logger.Debug("algorithm completed",
			slog.String("algorithm", name),
			slog.Duration("duration", result.Duration),
			slog.Bool("cancelled", result.Cancelled),
		)
	}

	// Update counters
	r.mu.Lock()
	r.completed++
	r.mu.Unlock()

	// Send result
	select {
	case r.results <- result:
	default:
		r.logger.Warn("result channel full, dropping result",
			slog.String("algorithm", name),
		)
	}
}

// Collect waits for all algorithms to complete and returns merged results.
//
// Description:
//
//	Waits for all running algorithms, then collects results and merges
//	deltas into a composite delta.
//
// Inputs:
//   - ctx: Context for cancellation.
//
// Outputs:
//   - crs.Delta: Merged delta from all successful algorithms.
//   - []*Result: All results (including failures).
//   - error: Non-nil if collection failed or context cancelled.
//
// Thread Safety: Must be called after all Run() calls.
func (r *Runner) Collect(ctx context.Context) (crs.Delta, []*Result, error) {
	// Wait for all algorithms to complete
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All algorithms completed
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	// Close results channel
	close(r.results)

	// Collect all results
	var results []*Result
	var deltas []crs.Delta

	for result := range r.results {
		results = append(results, result)
		if result.Delta != nil && result.Err == nil {
			deltas = append(deltas, result.Delta)
		}
	}

	// Merge deltas
	if len(deltas) == 0 {
		return nil, results, nil
	}

	if len(deltas) == 1 {
		return deltas[0], results, nil
	}

	composite := crs.NewCompositeDelta(deltas...)
	return composite, results, nil
}

// Stats returns execution statistics.
func (r *Runner) Stats() RunnerStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	return RunnerStats{
		Started:   r.started,
		Completed: r.completed,
		Pending:   r.started - r.completed,
	}
}

// RunnerStats contains runner execution statistics.
type RunnerStats struct {
	Started   int
	Completed int
	Pending   int
}

// -----------------------------------------------------------------------------
// Parallel Execution Helper
// -----------------------------------------------------------------------------

// RunParallel runs multiple algorithms in parallel and returns merged results.
//
// Description:
//
//	Convenience function that creates a runner, runs all algorithms,
//	and collects results.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - snapshot: Immutable CRS snapshot.
//   - executions: Pairs of (Algorithm, input).
//
// Outputs:
//   - crs.Delta: Merged delta from all successful algorithms.
//   - []*Result: All results.
//   - error: Non-nil if any critical failure.
func RunParallel(ctx context.Context, snapshot crs.Snapshot, executions ...Execution) (crs.Delta, []*Result, error) {
	runner := NewRunner(len(executions))

	for _, exec := range executions {
		runner.Run(ctx, exec.Algorithm, snapshot, exec.Input)
	}

	return runner.Collect(ctx)
}

// Execution pairs an algorithm with its input.
type Execution struct {
	Algorithm Algorithm
	Input     any
}

// NewExecution creates a new execution pair.
func NewExecution(algo Algorithm, input any) Execution {
	return Execution{
		Algorithm: algo,
		Input:     input,
	}
}
