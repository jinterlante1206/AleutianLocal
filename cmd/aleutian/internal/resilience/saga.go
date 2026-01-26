// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package resilience

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// =============================================================================
// Saga Executor Interface
// =============================================================================

// SagaExecutor defines the interface for saga-based transaction execution.
//
// # Description
//
// SagaExecutor manages multi-step operations that require automatic rollback
// on failure. When any step fails, previously completed steps are compensated
// (rolled back) in reverse order.
//
// # Thread Safety
//
// Implementations must be safe for use from a single goroutine.
// Concurrent execution of the same saga is not supported.
//
// # Example
//
//	var executor SagaExecutor = NewSaga(DefaultSagaConfig())
//	executor.AddStep(SagaStep{...})
//	if err := executor.Execute(ctx); err != nil {
//	    log.Printf("Saga failed: %v", err)
//	}
//
// # Limitations
//
//   - Implementations may vary in persistence and recovery capabilities
//   - Not designed for concurrent execution
//
// # Assumptions
//
//   - Execute is called from a single goroutine
//   - Steps are added before Execute is called
type SagaExecutor interface {
	// AddStep adds a step to the saga.
	AddStep(step SagaStep)

	// Execute runs all steps. If any fails, compensates completed steps.
	Execute(ctx context.Context) error

	// Reset clears all steps and state for reuse.
	Reset()

	// CompletedSteps returns names of successfully completed steps.
	CompletedSteps() []string

	// LastError returns the error that caused the saga to fail.
	LastError() error
}

// =============================================================================
// Saga Step
// =============================================================================

// SagaStep represents one step in a saga with its rollback action.
//
// # Description
//
// Each step consists of an Execute function and an optional Compensate function.
// The Execute function performs the forward action; the Compensate function
// undoes it if a later step fails.
//
// # Example
//
//	step := SagaStep{
//	    Name: "Create Network",
//	    Execute: func(ctx context.Context) error {
//	        return docker.CreateNetwork(ctx, "mynet")
//	    },
//	    Compensate: func(ctx context.Context) error {
//	        return docker.RemoveNetwork(ctx, "mynet")
//	    },
//	}
//
// # Limitations
//
//   - Compensate should be idempotent (safe to call multiple times)
//   - Compensate should not fail on "already doesn't exist" scenarios
//
// # Assumptions
//
//   - Execute respects context cancellation
//   - Compensate can be nil if no cleanup is needed
type SagaStep struct {
	// Name identifies the step for logging and debugging.
	Name string

	// Execute performs the forward action.
	Execute func(ctx context.Context) error

	// Compensate undoes the Execute action. May be nil if no cleanup needed.
	Compensate func(ctx context.Context) error

	// Timeout overrides the default step timeout. Zero uses saga default.
	Timeout time.Duration
}

// =============================================================================
// Saga Configuration
// =============================================================================

// SagaConfig configures saga behavior.
//
// # Description
//
// Controls timeouts, logging, and compensation behavior. All fields have
// sensible defaults that can be overridden.
//
// # Example
//
//	config := SagaConfig{
//	    StepTimeout:      30 * time.Second,
//	    CompensateOnFail: true,
//	    Logger:           slog.Default(),
//	}
//
// # Limitations
//
//   - Logger must be thread-safe if saga callbacks access shared state
//
// # Assumptions
//
//   - Callbacks are non-blocking or have reasonable timeouts
type SagaConfig struct {
	// StepTimeout is the default timeout for each step.
	// Default: 60 seconds
	StepTimeout time.Duration

	// CompensationTimeout is the timeout for each compensation.
	// Default: 30 seconds
	CompensationTimeout time.Duration

	// CompensateOnFail determines whether to run compensation on failure.
	// Default: true
	CompensateOnFail bool

	// Logger is used for step execution and compensation events.
	// Default: slog.Default()
	Logger *slog.Logger

	// OnStepStart is called before each step executes.
	OnStepStart func(step SagaStep)

	// OnStepComplete is called after each step completes successfully.
	OnStepComplete func(step SagaStep, duration time.Duration)

	// OnStepFail is called when a step fails.
	OnStepFail func(step SagaStep, err error)

	// OnCompensate is called when compensation runs.
	OnCompensate func(step SagaStep, err error)
}

// =============================================================================
// Saga Result Types
// =============================================================================

// SagaResult contains the outcome of a saga execution.
//
// # Description
//
// Provides detailed information about saga execution, including which
// steps succeeded, which failed, and any compensation errors.
type SagaResult struct {
	// Success indicates if all steps completed successfully.
	Success bool

	// CompletedSteps lists names of steps that executed successfully.
	CompletedSteps []string

	// FailedStep is the name of the step that failed (empty if success).
	FailedStep string

	// Error is the error from the failed step.
	Error error

	// CompensationErrors lists any errors from compensation actions.
	CompensationErrors []CompensationError

	// Duration is the total execution time.
	Duration time.Duration
}

// CompensationError records a failure during compensation.
type CompensationError struct {
	// StepName is the step being compensated.
	StepName string

	// Error is what went wrong during compensation.
	Error error
}

// =============================================================================
// Constructor Functions
// =============================================================================

// DefaultSagaConfig returns sensible defaults.
//
// # Description
//
// Returns a configuration with reasonable timeouts and logging enabled.
// Suitable for most use cases.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - SagaConfig: Configuration with default values
//
// # Example
//
//	config := DefaultSagaConfig()
//	config.StepTimeout = 30 * time.Second
//	saga := NewSaga(config)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - slog package is available
func DefaultSagaConfig() SagaConfig {
	return SagaConfig{
		StepTimeout:         60 * time.Second,
		CompensationTimeout: 30 * time.Second,
		CompensateOnFail:    true,
		Logger:              slog.Default(),
	}
}

// =============================================================================
// Saga Struct
// =============================================================================

// Saga implements SagaExecutor for multi-step operations with rollback.
//
// # Description
//
// Saga is the core implementation of the Saga pattern for managing
// multi-step operations that must be atomic. If any step fails, all
// previously completed steps are compensated in reverse order.
//
// # Use Cases
//
//   - Starting a container stack (create network, start services)
//   - Database migrations (apply changes, update schema)
//   - File operations (create directories, write files)
//
// # How It Works
//
//  1. Steps are added via AddStep in order of execution
//  2. Execute runs each step sequentially
//  3. On failure, completed steps are compensated in reverse order
//  4. Compensation errors are logged but don't stop other compensations
//
// # Thread Safety
//
// Saga is safe for concurrent use from multiple goroutines. All public
// methods are protected by a mutex. However, calls to Execute() on the
// same instance are serialized - concurrent Execute() calls will block
// until the preceding execution completes. A single Saga instance does
// not support parallel execution of its steps.
//
// # Example
//
//	saga := NewSaga(DefaultSagaConfig())
//
//	saga.AddStep(SagaStep{
//	    Name: "Create Network",
//	    Execute: func(ctx context.Context) error {
//	        return compose.CreateNetwork(ctx)
//	    },
//	    Compensate: func(ctx context.Context) error {
//	        return compose.RemoveNetwork(ctx)
//	    },
//	})
//
//	if err := saga.Execute(ctx); err != nil {
//	    log.Printf("Stack start failed: %v", err)
//	}
//
// # Limitations
//
//   - Steps execute sequentially (no parallel execution)
//   - Compensation may fail, leaving partial state
//   - No persistence - saga state is lost on process crash
//
// # Assumptions
//
//   - Compensate functions are idempotent
//   - Context cancellation should be respected
//   - Steps have reasonable timeouts
type Saga struct {
	config    SagaConfig
	steps     []SagaStep
	completed []SagaStep
	lastError error
	mu        sync.Mutex
}

// Compile-time interface satisfaction check
var _ SagaExecutor = (*Saga)(nil)

// NewSaga creates a new saga with the given configuration.
//
// # Description
//
// Creates an empty saga ready to receive steps. The saga will use
// the provided configuration for timeouts and callbacks. Zero values
// in config are replaced with sensible defaults.
//
// # Inputs
//
//   - config: Configuration for saga behavior
//
// # Outputs
//
//   - *Saga: New empty saga
//
// # Example
//
//	saga := NewSaga(SagaConfig{
//	    StepTimeout: 30 * time.Second,
//	    Logger:      slog.Default(),
//	})
//
// # Limitations
//
//   - Does not validate callback functions
//
// # Assumptions
//
//   - Caller will add steps before executing
func NewSaga(config SagaConfig) *Saga {
	if config.StepTimeout <= 0 {
		config.StepTimeout = 60 * time.Second
	}
	if config.CompensationTimeout <= 0 {
		config.CompensationTimeout = 30 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &Saga{
		config:    config,
		steps:     make([]SagaStep, 0),
		completed: make([]SagaStep, 0),
	}
}

// =============================================================================
// Saga Methods
// =============================================================================

// AddStep adds a step to the saga.
//
// # Description
//
// Steps are executed in the order they are added. Each step should
// have a corresponding Compensate function that can undo its effects.
// Thread-safe for concurrent step additions.
//
// # Inputs
//
//   - step: Step to add to the saga
//
// # Outputs
//
//   - None
//
// # Example
//
//	saga.AddStep(SagaStep{
//	    Name:       "Create Volume",
//	    Execute:    createVolume,
//	    Compensate: deleteVolume,
//	})
//
// # Limitations
//
//   - Does not validate step fields
//
// # Assumptions
//
//   - Step has a non-empty Name for debugging
//   - Execute function is not nil
func (s *Saga) AddStep(step SagaStep) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

// Execute runs all steps. If any fails, compensates completed steps.
//
// # Description
//
// Executes steps sequentially. Each step runs with a timeout derived
// from its Timeout field or the saga's StepTimeout default.
//
// If a step fails:
//  1. The error is recorded
//  2. All completed steps are compensated in reverse order
//  3. The original error is returned
//
// # Inputs
//
//   - ctx: Context for cancellation and timeouts
//
// # Outputs
//
//   - error: nil if all steps succeed, otherwise the first failure
//
// # Error Conditions
//
//   - Context cancelled before completion
//   - Step execution failed
//   - Step timed out
//
// # Example
//
//	err := saga.Execute(ctx)
//	if err != nil {
//	    log.Printf("Operation failed and rolled back: %v", err)
//	}
//
// # Limitations
//
//   - Compensation errors are logged but not returned
//   - Cannot recover from process crash mid-execution
//
// # Assumptions
//
//   - ctx is not nil
//   - Steps respect context cancellation
func (s *Saga) Execute(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.completed = make([]SagaStep, 0, len(s.steps))
	s.lastError = nil

	for _, step := range s.steps {
		// Check context before each step
		if ctx.Err() != nil {
			s.lastError = fmt.Errorf("saga cancelled: %w", ctx.Err())
			s.compensate(ctx)
			return s.lastError
		}

		// Determine step timeout
		timeout := step.Timeout
		if timeout <= 0 {
			timeout = s.config.StepTimeout
		}

		// Execute step with timeout
		if err := s.executeStep(ctx, step, timeout); err != nil {
			s.lastError = fmt.Errorf("saga failed at step %q: %w", step.Name, err)

			if s.config.OnStepFail != nil {
				s.config.OnStepFail(step, err)
			}

			// Compensate completed steps
			if s.config.CompensateOnFail {
				s.compensate(ctx)
			}

			return s.lastError
		}

		s.completed = append(s.completed, step)
	}

	return nil
}

// executeStep runs a single step with timeout.
//
// # Description
//
// Executes a single step within the specified timeout. Calls the
// appropriate callbacks before and after execution.
//
// # Inputs
//
//   - ctx: Parent context
//   - step: Step to execute
//   - timeout: Maximum duration for step execution
//
// # Outputs
//
//   - error: nil on success, error on failure or timeout
//
// # Limitations
//
//   - Step function runs in a goroutine and may leak if it ignores context
//
// # Assumptions
//
//   - Step respects context cancellation
func (s *Saga) executeStep(ctx context.Context, step SagaStep, timeout time.Duration) error {
	if s.config.OnStepStart != nil {
		s.config.OnStepStart(step)
	}

	s.config.Logger.Info("Executing step", "step", step.Name)
	start := time.Now()

	// Create timeout context
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute in goroutine to handle timeout
	done := make(chan error, 1)
	go func() {
		done <- step.Execute(stepCtx)
	}()

	select {
	case err := <-done:
		duration := time.Since(start)
		if err != nil {
			s.config.Logger.Error("Step failed", "step", step.Name, "duration", duration, "error", err)
			return err
		}
		s.config.Logger.Info("Step completed", "step", step.Name, "duration", duration)
		if s.config.OnStepComplete != nil {
			s.config.OnStepComplete(step, duration)
		}
		return nil

	case <-stepCtx.Done():
		return fmt.Errorf("step timed out after %v", timeout)
	}
}

// compensate runs compensation for completed steps in reverse order.
//
// # Description
//
// Compensates all completed steps in reverse order of execution.
// Compensation errors are logged but do not stop other compensations.
// Uses a fresh context independent of the parent to ensure cleanup completes.
//
// # Inputs
//
//   - ctx: Original context (used for logging only)
//
// # Outputs
//
//   - None
//
// # Limitations
//
//   - Does not return compensation errors
//   - May leave partial state if compensation fails
//
// # Assumptions
//
//   - Compensate functions are idempotent
//   - Compensate functions have reasonable timeouts
func (s *Saga) compensate(ctx context.Context) {
	if len(s.completed) == 0 {
		return
	}

	s.config.Logger.Info("Compensating completed steps", "count", len(s.completed))

	// Create a context for compensation that won't be cancelled
	// even if parent is cancelled (we want to complete cleanup)
	compensateCtx, cancel := context.WithTimeout(context.Background(),
		s.config.CompensationTimeout*time.Duration(len(s.completed)))
	defer cancel()

	// Compensate in reverse order
	for i := len(s.completed) - 1; i >= 0; i-- {
		step := s.completed[i]
		if step.Compensate == nil {
			s.config.Logger.Debug("No compensation defined", "step", step.Name)
			continue
		}

		s.config.Logger.Info("Compensating step", "step", step.Name)

		stepCtx, stepCancel := context.WithTimeout(compensateCtx, s.config.CompensationTimeout)

		err := step.Compensate(stepCtx)
		stepCancel()

		if err != nil {
			s.config.Logger.Warn("Compensation failed", "step", step.Name, "error", err)
			if s.config.OnCompensate != nil {
				s.config.OnCompensate(step, err)
			}
		} else {
			s.config.Logger.Info("Compensated step", "step", step.Name)
			if s.config.OnCompensate != nil {
				s.config.OnCompensate(step, nil)
			}
		}
	}
}

// Reset clears all steps and state for reuse.
//
// # Description
//
// Resets the saga to its initial empty state. Use this to reuse
// a saga instance for a new operation. Clears steps, completed steps,
// and last error.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - None
//
// # Example
//
//	saga.Reset()
//	saga.AddStep(newStep)
//	saga.Execute(ctx)
//
// # Limitations
//
//   - Does not reset config or callbacks
//
// # Assumptions
//
//   - Saga is not currently executing
func (s *Saga) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = make([]SagaStep, 0)
	s.completed = make([]SagaStep, 0)
	s.lastError = nil
}

// CompletedSteps returns names of successfully completed steps.
//
// # Description
//
// Returns the names of steps that executed successfully before
// any failure occurred. Useful for debugging and logging.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - []string: Names of completed steps in execution order
//
// # Example
//
//	if err := saga.Execute(ctx); err != nil {
//	    log.Printf("Completed before failure: %v", saga.CompletedSteps())
//	}
//
// # Limitations
//
//   - Returns a copy, not a reference to internal state
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Saga) CompletedSteps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, len(s.completed))
	for i, step := range s.completed {
		names[i] = step.Name
	}
	return names
}

// LastError returns the error that caused the saga to fail.
//
// # Description
//
// Returns nil if the saga has not been executed or if it succeeded.
// The error includes context about which step failed.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - error: The failure error, or nil
//
// # Example
//
//	if saga.LastError() != nil {
//	    log.Printf("Last failure: %v", saga.LastError())
//	}
//
// # Limitations
//
//   - Only stores the most recent error
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Saga) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// StepCount returns the total number of steps in the saga.
//
// # Description
//
// Returns the number of steps that have been added to the saga.
// Useful for debugging and validation.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - int: Total number of steps
//
// # Example
//
//	if saga.StepCount() == 0 {
//	    log.Println("No steps added to saga")
//	}
//
// # Limitations
//
//   - Value may be stale in concurrent scenarios
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Saga) StepCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.steps)
}
