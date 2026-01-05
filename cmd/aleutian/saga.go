package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

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

// SagaConfig configures saga behavior.
//
// # Description
//
// Controls timeouts, logging, and compensation behavior.
//
// # Example
//
//	config := SagaConfig{
//	    StepTimeout:      30 * time.Second,
//	    CompensateOnFail: true,
//	    Logger:           log.Printf,
//	}
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

	// Logger is called for step execution and compensation events.
	// Default: log.Printf
	Logger func(format string, args ...interface{})

	// OnStepStart is called before each step executes.
	OnStepStart func(step SagaStep)

	// OnStepComplete is called after each step completes successfully.
	OnStepComplete func(step SagaStep, duration time.Duration)

	// OnStepFail is called when a step fails.
	OnStepFail func(step SagaStep, err error)

	// OnCompensate is called when compensation runs.
	OnCompensate func(step SagaStep, err error)
}

// DefaultSagaConfig returns sensible defaults.
//
// # Description
//
// Returns a configuration with reasonable timeouts and logging enabled.
//
// # Outputs
//
//   - SagaConfig: Configuration with default values
func DefaultSagaConfig() SagaConfig {
	return SagaConfig{
		StepTimeout:         60 * time.Second,
		CompensationTimeout: 30 * time.Second,
		CompensateOnFail:    true,
		Logger:              log.Printf,
	}
}

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
// Saga is NOT safe for concurrent use. Each saga instance should be
// used from a single goroutine.
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
//	saga.AddStep(SagaStep{
//	    Name: "Start Weaviate",
//	    Execute: func(ctx context.Context) error {
//	        return compose.Up(ctx, UpOptions{Services: []string{"weaviate"}})
//	    },
//	    Compensate: func(ctx context.Context) error {
//	        return compose.Down(ctx, DownOptions{Services: []string{"weaviate"}})
//	    },
//	})
//
//	if err := saga.Execute(ctx); err != nil {
//	    // All completed steps have been rolled back
//	    log.Printf("Stack start failed: %v", err)
//	}
type Saga struct {
	config    SagaConfig
	steps     []SagaStep
	completed []SagaStep
	lastError error
	mu        sync.Mutex
}

// NewSaga creates a new saga with the given configuration.
//
// # Description
//
// Creates an empty saga ready to receive steps. The saga will use
// the provided configuration for timeouts and callbacks.
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
//	    Logger:      log.Printf,
//	})
func NewSaga(config SagaConfig) *Saga {
	if config.StepTimeout <= 0 {
		config.StepTimeout = 60 * time.Second
	}
	if config.CompensationTimeout <= 0 {
		config.CompensationTimeout = 30 * time.Second
	}
	if config.Logger == nil {
		config.Logger = log.Printf
	}

	return &Saga{
		config:    config,
		steps:     make([]SagaStep, 0),
		completed: make([]SagaStep, 0),
	}
}

// AddStep adds a step to the saga.
//
// # Description
//
// Steps are executed in the order they are added. Each step should
// have a corresponding Compensate function that can undo its effects.
//
// # Inputs
//
//   - step: Step to add to the saga
//
// # Example
//
//	saga.AddStep(SagaStep{
//	    Name:       "Create Volume",
//	    Execute:    createVolume,
//	    Compensate: deleteVolume,
//	})
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
//	    // Saga failed and rolled back
//	    log.Printf("Operation failed: %v", err)
//	}
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
func (s *Saga) executeStep(ctx context.Context, step SagaStep, timeout time.Duration) error {
	if s.config.OnStepStart != nil {
		s.config.OnStepStart(step)
	}

	s.config.Logger("Executing step: %s", step.Name)
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
			s.config.Logger("Step %s failed after %v: %v", step.Name, duration, err)
			return err
		}
		s.config.Logger("Step %s completed in %v", step.Name, duration)
		if s.config.OnStepComplete != nil {
			s.config.OnStepComplete(step, duration)
		}
		return nil

	case <-stepCtx.Done():
		return fmt.Errorf("step timed out after %v", timeout)
	}
}

// compensate runs compensation for completed steps in reverse order.
func (s *Saga) compensate(ctx context.Context) {
	if len(s.completed) == 0 {
		return
	}

	s.config.Logger("Compensating %d completed steps...", len(s.completed))

	// Create a context for compensation that won't be cancelled
	// even if parent is cancelled (we want to complete cleanup)
	compensateCtx, cancel := context.WithTimeout(context.Background(),
		s.config.CompensationTimeout*time.Duration(len(s.completed)))
	defer cancel()

	// Compensate in reverse order
	for i := len(s.completed) - 1; i >= 0; i-- {
		step := s.completed[i]
		if step.Compensate == nil {
			s.config.Logger("No compensation defined for step: %s", step.Name)
			continue
		}

		s.config.Logger("Compensating step: %s", step.Name)

		stepCtx, stepCancel := context.WithTimeout(compensateCtx, s.config.CompensationTimeout)

		err := step.Compensate(stepCtx)
		stepCancel()

		if err != nil {
			s.config.Logger("WARNING: Compensation failed for %s: %v", step.Name, err)
			if s.config.OnCompensate != nil {
				s.config.OnCompensate(step, err)
			}
		} else {
			s.config.Logger("Compensated step: %s", step.Name)
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
// a saga instance for a new operation.
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
// # Outputs
//
//   - []string: Names of completed steps in execution order
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
//
// # Outputs
//
//   - error: The failure error, or nil
func (s *Saga) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// StepCount returns the total number of steps in the saga.
func (s *Saga) StepCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.steps)
}

// Compile-time interface satisfaction check
var _ SagaExecutor = (*Saga)(nil)
