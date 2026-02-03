// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNilContext is returned when context is nil.
	ErrNilContext = errors.New("context must not be nil")

	// ErrNilSnapshot is returned when snapshot is nil.
	ErrNilSnapshot = errors.New("snapshot must not be nil")

	// ErrNilInput is returned when input is nil.
	ErrNilInput = errors.New("input must not be nil")

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrActivityFailed is returned when activity execution fails.
	ErrActivityFailed = errors.New("activity execution failed")

	// ErrNoAlgorithmsRun is returned when no algorithms were executed.
	ErrNoAlgorithmsRun = errors.New("no algorithms were executed")
)

// ActivityError wraps an error with activity context.
type ActivityError struct {
	Activity  string
	Operation string
	Err       error
}

func (e *ActivityError) Error() string {
	return e.Activity + "." + e.Operation + ": " + e.Err.Error()
}

func (e *ActivityError) Unwrap() error {
	return e.Err
}

// NewActivityError creates a new activity error.
func NewActivityError(activity, operation string, err error) *ActivityError {
	return &ActivityError{
		Activity:  activity,
		Operation: operation,
		Err:       err,
	}
}

// -----------------------------------------------------------------------------
// Priority
// -----------------------------------------------------------------------------

// Priority represents the urgency of an activity.
type Priority int

const (
	// PriorityLow is for background activities.
	PriorityLow Priority = iota

	// PriorityNormal is the default priority.
	PriorityNormal

	// PriorityHigh is for time-sensitive activities.
	PriorityHigh

	// PriorityCritical is for activities that must run immediately.
	PriorityCritical
)

// String returns the string representation of Priority.
func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityCritical:
		return "critical"
	default:
		return fmt.Sprintf("Priority(%d)", p)
	}
}

// -----------------------------------------------------------------------------
// Activity Interface
// -----------------------------------------------------------------------------

// Activity orchestrates a group of related algorithms.
//
// Description:
//
//	Activities are the coordination layer between the MCTS engine and
//	algorithms. They decide which algorithms to run, run them in parallel,
//	and merge their results.
//
//	Activities are responsible for:
//	- Deciding when to run based on CRS state
//	- Selecting which algorithms to execute
//	- Running algorithms in parallel
//	- Merging algorithm deltas
//	- Handling algorithm failures
//
// Thread Safety: Must be safe for concurrent execution.
type Activity interface {
	eval.Evaluable

	// Name returns the activity name.
	Name() string

	// Execute runs the activity's algorithms.
	//
	// Description:
	//
	//	Executes one or more algorithms based on the input and current state.
	//	Returns a merged delta from all successful algorithms.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//   - snapshot: Immutable CRS snapshot. Must not be nil.
	//   - input: Activity-specific input.
	//
	// Outputs:
	//   - ActivityResult: The execution result.
	//   - crs.Delta: Merged delta from all algorithms.
	//   - error: Non-nil on complete failure.
	//
	// Thread Safety: Safe for concurrent calls.
	Execute(ctx context.Context, snapshot crs.Snapshot, input ActivityInput) (ActivityResult, crs.Delta, error)

	// ShouldRun decides if the activity should run.
	//
	// Description:
	//
	//	Examines the snapshot to decide if this activity is applicable.
	//	Returns the priority if it should run.
	//
	// Inputs:
	//   - snapshot: Immutable CRS snapshot.
	//
	// Outputs:
	//   - bool: True if the activity should run.
	//   - Priority: Priority if should run.
	ShouldRun(snapshot crs.Snapshot) (bool, Priority)

	// Algorithms returns the algorithms this activity orchestrates.
	Algorithms() []algorithms.Algorithm

	// Timeout returns the maximum execution time for the activity.
	Timeout() time.Duration
}

// -----------------------------------------------------------------------------
// Activity Input/Output
// -----------------------------------------------------------------------------

// ActivityInput is the base input for activities.
type ActivityInput interface {
	// Type returns the input type name.
	Type() string

	// Source returns the signal source for this request.
	Source() crs.SignalSource
}

// BaseInput provides common input functionality.
//
// Design Note: The signal field is unexported to prevent mutation after
// construction. Signal source is immutable once set via NewBaseInput to
// ensure the hard/soft signal boundary is maintained throughout processing.
type BaseInput struct {
	RequestID string
	signal    crs.SignalSource
}

// NewBaseInput creates a new base input.
func NewBaseInput(requestID string, source crs.SignalSource) BaseInput {
	return BaseInput{
		RequestID: requestID,
		signal:    source,
	}
}

// Type returns the input type name.
func (b BaseInput) Type() string {
	return "base"
}

// Source returns the signal source.
func (b BaseInput) Source() crs.SignalSource {
	return b.signal
}

// ActivityResult contains the results of activity execution.
type ActivityResult struct {
	// ActivityName is the name of the activity.
	ActivityName string

	// AlgorithmResults contains results from each algorithm.
	AlgorithmResults []*algorithms.Result

	// Duration is the total execution time.
	Duration time.Duration

	// StartTime is when execution started.
	StartTime time.Time

	// EndTime is when execution ended.
	EndTime time.Time

	// Success is true if execution succeeded.
	Success bool

	// PartialSuccess is true if some algorithms succeeded.
	PartialSuccess bool

	// Metrics contains activity-specific metrics.
	Metrics map[string]float64
}

// SuccessCount returns the number of successful algorithm executions.
func (r *ActivityResult) SuccessCount() int {
	count := 0
	for _, ar := range r.AlgorithmResults {
		if ar.Success() {
			count++
		}
	}
	return count
}

// FailureCount returns the number of failed algorithm executions.
func (r *ActivityResult) FailureCount() int {
	return len(r.AlgorithmResults) - r.SuccessCount()
}

// -----------------------------------------------------------------------------
// Base Activity
// -----------------------------------------------------------------------------

// BaseActivity provides common activity functionality.
type BaseActivity struct {
	name       string
	timeout    time.Duration
	algorithms []algorithms.Algorithm
}

// NewBaseActivity creates a new base activity.
func NewBaseActivity(name string, timeout time.Duration, algos ...algorithms.Algorithm) *BaseActivity {
	return &BaseActivity{
		name:       name,
		timeout:    timeout,
		algorithms: algos,
	}
}

// Name returns the activity name.
func (a *BaseActivity) Name() string {
	return a.name
}

// Timeout returns the activity timeout.
func (a *BaseActivity) Timeout() time.Duration {
	return a.timeout
}

// Algorithms returns the activity's algorithms.
func (a *BaseActivity) Algorithms() []algorithms.Algorithm {
	return a.algorithms
}

// RunAlgorithms executes all algorithms in parallel.
//
// Description:
//
//	Runs all configured algorithms in parallel using the Runner,
//	collects results, and merges deltas.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - snapshot: Immutable CRS snapshot.
//   - makeInput: Function to create algorithm-specific input.
//
// Outputs:
//   - ActivityResult: Execution result.
//   - crs.Delta: Merged delta.
//   - error: Non-nil on complete failure.
func (a *BaseActivity) RunAlgorithms(
	ctx context.Context,
	snapshot crs.Snapshot,
	makeInput func(algo algorithms.Algorithm) any,
) (ActivityResult, crs.Delta, error) {
	// Validate inputs per CLAUDE.md §5.3
	if ctx == nil {
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.name,
			Operation: "RunAlgorithms",
			Err:       ErrNilContext,
		}
	}
	if snapshot == nil {
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.name,
			Operation: "RunAlgorithms",
			Err:       ErrNilSnapshot,
		}
	}

	startTime := time.Now()

	result := ActivityResult{
		ActivityName: a.name,
		StartTime:    startTime,
		Metrics:      make(map[string]float64),
	}

	if len(a.algorithms) == 0 {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result, nil, &ActivityError{
			Activity:  a.name,
			Operation: "RunAlgorithms",
			Err:       ErrNoAlgorithmsRun,
		}
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build executions
	executions := make([]algorithms.Execution, 0, len(a.algorithms))
	for _, algo := range a.algorithms {
		input := makeInput(algo)
		executions = append(executions, algorithms.NewExecution(algo, input))
	}

	// Run in parallel
	delta, algoResults, err := algorithms.RunParallel(ctx, snapshot, executions...)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result, nil, &ActivityError{
			Activity:  a.name,
			Operation: "RunAlgorithms",
			Err:       err,
		}
	}

	result.AlgorithmResults = algoResults
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)

	// Determine success status
	successCount := result.SuccessCount()
	result.Success = successCount == len(a.algorithms)
	result.PartialSuccess = successCount > 0 && successCount < len(a.algorithms)

	// Record metrics
	result.Metrics["algorithms_total"] = float64(len(a.algorithms))
	result.Metrics["algorithms_success"] = float64(successCount)
	result.Metrics["algorithms_failed"] = float64(result.FailureCount())
	result.Metrics["duration_ms"] = float64(result.Duration.Milliseconds())

	return result, delta, nil
}

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// ActivityConfig provides common configuration for activities.
type ActivityConfig struct {
	// Timeout overrides the default activity timeout.
	Timeout time.Duration

	// EnableMetrics enables detailed metrics collection.
	EnableMetrics bool

	// EnableTracing enables OpenTelemetry tracing.
	EnableTracing bool

	// MaxConcurrentAlgorithms limits parallel algorithm execution.
	MaxConcurrentAlgorithms int
}

// DefaultActivityConfig returns the default activity configuration.
func DefaultActivityConfig() *ActivityConfig {
	return &ActivityConfig{
		Timeout:                 30 * time.Second,
		EnableMetrics:           true,
		EnableTracing:           true,
		MaxConcurrentAlgorithms: 10,
	}
}

// Validate checks if the configuration is valid.
func (c *ActivityConfig) Validate() error {
	if c.Timeout < 0 {
		return ErrInvalidConfig
	}
	if c.MaxConcurrentAlgorithms < 0 {
		return ErrInvalidConfig
	}
	return nil
}
