// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package resilience

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// DefaultSagaConfig Tests
// =============================================================================

func TestDefaultSagaConfig(t *testing.T) {
	config := DefaultSagaConfig()

	if config.StepTimeout <= 0 {
		t.Error("StepTimeout should be positive")
	}
	if config.CompensationTimeout <= 0 {
		t.Error("CompensationTimeout should be positive")
	}
	if !config.CompensateOnFail {
		t.Error("CompensateOnFail should default to true")
	}
	if config.Logger == nil {
		t.Error("Logger should not be nil")
	}
}

// =============================================================================
// NewSaga Tests
// =============================================================================

func TestNewSaga(t *testing.T) {
	tests := []struct {
		name   string
		config SagaConfig
	}{
		{
			name:   "with defaults",
			config: DefaultSagaConfig(),
		},
		{
			name: "with zero values",
			config: SagaConfig{
				StepTimeout: 0, // Should be set to default
			},
		},
		{
			name: "with custom values",
			config: SagaConfig{
				StepTimeout:         10 * time.Second,
				CompensationTimeout: 5 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saga := NewSaga(tt.config)
			if saga == nil {
				t.Fatal("NewSaga returned nil")
			}
			if saga.StepCount() != 0 {
				t.Errorf("StepCount() = %d, want 0", saga.StepCount())
			}
		})
	}
}

// =============================================================================
// AddStep Tests
// =============================================================================

func TestSaga_AddStep(t *testing.T) {
	saga := NewSaga(DefaultSagaConfig())

	saga.AddStep(SagaStep{Name: "Step1"})
	if saga.StepCount() != 1 {
		t.Errorf("StepCount() = %d, want 1", saga.StepCount())
	}

	saga.AddStep(SagaStep{Name: "Step2"})
	if saga.StepCount() != 2 {
		t.Errorf("StepCount() = %d, want 2", saga.StepCount())
	}
}

// =============================================================================
// Execute Tests - Success Path
// =============================================================================

func TestSaga_Execute_AllSuccess(t *testing.T) {
	saga := NewSaga(quietConfig())

	var executed []string
	saga.AddStep(SagaStep{
		Name: "Step1",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step1")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name: "Step2",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step2")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name: "Step3",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step3")
			return nil
		},
	})

	err := saga.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if len(executed) != 3 {
		t.Errorf("Expected 3 steps executed, got %d", len(executed))
	}

	completed := saga.CompletedSteps()
	if len(completed) != 3 {
		t.Errorf("CompletedSteps() = %d items, want 3", len(completed))
	}

	if saga.LastError() != nil {
		t.Errorf("LastError() = %v, want nil", saga.LastError())
	}
}

// =============================================================================
// Execute Tests - Failure Path
// =============================================================================

func TestSaga_Execute_FailureWithCompensation(t *testing.T) {
	saga := NewSaga(quietConfig())

	var executed []string
	var compensated []string

	saga.AddStep(SagaStep{
		Name: "Step1",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step1")
			return nil
		},
		Compensate: func(ctx context.Context) error {
			compensated = append(compensated, "Step1")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name: "Step2",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step2")
			return nil
		},
		Compensate: func(ctx context.Context) error {
			compensated = append(compensated, "Step2")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name: "Step3_Fails",
		Execute: func(ctx context.Context) error {
			executed = append(executed, "Step3_Fails")
			return errors.New("intentional failure")
		},
		Compensate: func(ctx context.Context) error {
			compensated = append(compensated, "Step3_Fails")
			return nil
		},
	})

	err := saga.Execute(context.Background())
	if err == nil {
		t.Fatal("Execute() should have failed")
	}

	// Should have executed 3 steps (Step3 fails during execution)
	if len(executed) != 3 {
		t.Errorf("Expected 3 steps executed, got %d: %v", len(executed), executed)
	}

	// Should compensate Step1 and Step2 (in reverse order), but NOT Step3
	// because Step3 failed, it shouldn't be in completed list
	if len(compensated) != 2 {
		t.Errorf("Expected 2 steps compensated, got %d: %v", len(compensated), compensated)
	}

	// Compensation should be in reverse order
	if len(compensated) >= 2 && compensated[0] != "Step2" {
		t.Errorf("First compensation should be Step2, got %s", compensated[0])
	}
	if len(compensated) >= 2 && compensated[1] != "Step1" {
		t.Errorf("Second compensation should be Step1, got %s", compensated[1])
	}

	// Error should mention the failed step
	if !strings.Contains(err.Error(), "Step3_Fails") {
		t.Errorf("Error should mention failed step, got: %v", err)
	}

	// CompletedSteps should only have steps 1 and 2
	completed := saga.CompletedSteps()
	if len(completed) != 2 {
		t.Errorf("CompletedSteps() = %v, want [Step1, Step2]", completed)
	}
}

func TestSaga_Execute_CompensationDisabled(t *testing.T) {
	config := quietConfig()
	config.CompensateOnFail = false
	saga := NewSaga(config)

	var compensated []string

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
		Compensate: func(ctx context.Context) error {
			compensated = append(compensated, "Step1")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name:    "Step2_Fails",
		Execute: func(ctx context.Context) error { return errors.New("fail") },
	})

	err := saga.Execute(context.Background())
	if err == nil {
		t.Fatal("Execute() should have failed")
	}

	// Compensation should not run when disabled
	if len(compensated) != 0 {
		t.Errorf("Expected no compensation, got %d: %v", len(compensated), compensated)
	}
}

func TestSaga_Execute_NilCompensation(t *testing.T) {
	saga := NewSaga(quietConfig())

	saga.AddStep(SagaStep{
		Name:       "Step1_NoCompensate",
		Execute:    func(ctx context.Context) error { return nil },
		Compensate: nil, // No compensation defined
	})
	saga.AddStep(SagaStep{
		Name:    "Step2_Fails",
		Execute: func(ctx context.Context) error { return errors.New("fail") },
	})

	// Should not panic when compensation is nil
	err := saga.Execute(context.Background())
	if err == nil {
		t.Fatal("Execute() should have failed")
	}
}

func TestSaga_Execute_CompensationError(t *testing.T) {
	saga := NewSaga(quietConfig())

	var compensationAttempted bool

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
		Compensate: func(ctx context.Context) error {
			compensationAttempted = true
			return errors.New("compensation failed")
		},
	})
	saga.AddStep(SagaStep{
		Name:    "Step2_Fails",
		Execute: func(ctx context.Context) error { return errors.New("fail") },
	})

	err := saga.Execute(context.Background())
	if err == nil {
		t.Fatal("Execute() should have failed")
	}

	// Compensation should have been attempted
	if !compensationAttempted {
		t.Error("Compensation should have been attempted")
	}

	// Original error should be returned (not compensation error)
	if !strings.Contains(err.Error(), "Step2_Fails") {
		t.Errorf("Error should be from Step2_Fails, got: %v", err)
	}
}

// =============================================================================
// Execute Tests - Context Cancellation
// =============================================================================

func TestSaga_Execute_ContextCancelledBeforeStart(t *testing.T) {
	saga := NewSaga(quietConfig())

	var compensated []string

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
		Compensate: func(ctx context.Context) error {
			compensated = append(compensated, "Step1")
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name: "Step2_NeverRuns",
		Execute: func(ctx context.Context) error {
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before execution

	err := saga.Execute(ctx)
	if err == nil {
		t.Fatal("Execute() should have failed due to cancellation")
	}

	if !strings.Contains(err.Error(), "cancel") {
		t.Errorf("Error should mention cancellation, got: %v", err)
	}
}

func TestSaga_Execute_ContextCancelledDuringStep(t *testing.T) {
	saga := NewSaga(quietConfig())

	saga.AddStep(SagaStep{
		Name: "Step1_RespectsContext",
		Execute: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after step starts
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := saga.Execute(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Execute() should have failed")
	}

	// Should complete quickly (not wait for 10s timeout)
	if elapsed > time.Second {
		t.Errorf("Should have cancelled quickly, took %v", elapsed)
	}
}

// =============================================================================
// Execute Tests - Timeouts
// =============================================================================

func TestSaga_Execute_StepTimeout(t *testing.T) {
	config := quietConfig()
	config.StepTimeout = 100 * time.Millisecond
	saga := NewSaga(config)

	saga.AddStep(SagaStep{
		Name: "Step1_TimesOut",
		Execute: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	})

	start := time.Now()
	err := saga.Execute(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Execute() should have timed out")
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("Should have timed out quickly, took %v", elapsed)
	}

	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Error should mention timeout, got: %v", err)
	}
}

func TestSaga_Execute_CustomStepTimeout(t *testing.T) {
	config := quietConfig()
	config.StepTimeout = 5 * time.Second // Long default
	saga := NewSaga(config)

	saga.AddStep(SagaStep{
		Name:    "Step1_CustomTimeout",
		Timeout: 100 * time.Millisecond, // Short override
		Execute: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	})

	start := time.Now()
	err := saga.Execute(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Execute() should have timed out")
	}

	// Should use custom timeout, not default
	if elapsed > 500*time.Millisecond {
		t.Errorf("Should have timed out at custom timeout, took %v", elapsed)
	}
}

// =============================================================================
// Reset Tests
// =============================================================================

func TestSaga_Reset(t *testing.T) {
	saga := NewSaga(quietConfig())

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
	})

	_ = saga.Execute(context.Background())

	if saga.StepCount() != 1 {
		t.Errorf("Before reset: StepCount() = %d, want 1", saga.StepCount())
	}
	if len(saga.CompletedSteps()) != 1 {
		t.Errorf("Before reset: CompletedSteps() = %d, want 1", len(saga.CompletedSteps()))
	}

	saga.Reset()

	if saga.StepCount() != 0 {
		t.Errorf("After reset: StepCount() = %d, want 0", saga.StepCount())
	}
	if len(saga.CompletedSteps()) != 0 {
		t.Errorf("After reset: CompletedSteps() = %d, want 0", len(saga.CompletedSteps()))
	}
	if saga.LastError() != nil {
		t.Errorf("After reset: LastError() = %v, want nil", saga.LastError())
	}
}

// =============================================================================
// Callback Tests
// =============================================================================

func TestSaga_Callbacks(t *testing.T) {
	var started []string
	var completed []string
	var failed []string

	config := quietConfig()
	config.OnStepStart = func(step SagaStep) {
		started = append(started, step.Name)
	}
	config.OnStepComplete = func(step SagaStep, duration time.Duration) {
		completed = append(completed, step.Name)
	}
	config.OnStepFail = func(step SagaStep, err error) {
		failed = append(failed, step.Name)
	}

	saga := NewSaga(config)

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
	})
	saga.AddStep(SagaStep{
		Name:    "Step2_Fails",
		Execute: func(ctx context.Context) error { return errors.New("fail") },
	})

	_ = saga.Execute(context.Background())

	if len(started) != 2 {
		t.Errorf("OnStepStart called %d times, want 2", len(started))
	}
	if len(completed) != 1 {
		t.Errorf("OnStepComplete called %d times, want 1", len(completed))
	}
	if len(failed) != 1 {
		t.Errorf("OnStepFail called %d times, want 1", len(failed))
	}
	if len(failed) > 0 && failed[0] != "Step2_Fails" {
		t.Errorf("OnStepFail called with %s, want Step2_Fails", failed[0])
	}
}

func TestSaga_OnCompensateCallback(t *testing.T) {
	var compensations []string

	config := quietConfig()
	config.OnCompensate = func(step SagaStep, err error) {
		status := "success"
		if err != nil {
			status = "failed"
		}
		compensations = append(compensations, step.Name+":"+status)
	}

	saga := NewSaga(config)

	saga.AddStep(SagaStep{
		Name:    "Step1",
		Execute: func(ctx context.Context) error { return nil },
		Compensate: func(ctx context.Context) error {
			return nil
		},
	})
	saga.AddStep(SagaStep{
		Name:    "Step2",
		Execute: func(ctx context.Context) error { return nil },
		Compensate: func(ctx context.Context) error {
			return errors.New("compensation failed")
		},
	})
	saga.AddStep(SagaStep{
		Name:    "Step3_Fails",
		Execute: func(ctx context.Context) error { return errors.New("fail") },
	})

	_ = saga.Execute(context.Background())

	if len(compensations) != 2 {
		t.Errorf("OnCompensate called %d times, want 2", len(compensations))
	}
	// Step2 compensates first (reverse order) and fails
	if len(compensations) > 0 && compensations[0] != "Step2:failed" {
		t.Errorf("First compensation = %s, want Step2:failed", compensations[0])
	}
	// Step1 compensates second and succeeds
	if len(compensations) > 1 && compensations[1] != "Step1:success" {
		t.Errorf("Second compensation = %s, want Step1:success", compensations[1])
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestSaga_InterfaceCompliance(t *testing.T) {
	var _ SagaExecutor = (*Saga)(nil)
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestSaga_ConcurrentSafety(t *testing.T) {
	// While Saga is not designed for concurrent use, we should verify
	// that internal locking prevents data races on AddStep/Execute
	saga := NewSaga(quietConfig())

	var ops int64

	// Concurrent adds (not typical usage, but shouldn't crash)
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			saga.AddStep(SagaStep{
				Name: "ConcurrentStep",
				Execute: func(ctx context.Context) error {
					atomic.AddInt64(&ops, 1)
					return nil
				},
			})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if saga.StepCount() != 10 {
		t.Errorf("StepCount() = %d, want 10", saga.StepCount())
	}
}

// =============================================================================
// Empty Saga Tests
// =============================================================================

func TestSaga_Execute_EmptySaga(t *testing.T) {
	saga := NewSaga(quietConfig())

	err := saga.Execute(context.Background())
	if err != nil {
		t.Errorf("Execute() on empty saga should succeed, got: %v", err)
	}

	if len(saga.CompletedSteps()) != 0 {
		t.Errorf("CompletedSteps() should be empty, got %d", len(saga.CompletedSteps()))
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// quietConfig returns a config with no logging for cleaner test output.
func quietConfig() SagaConfig {
	return SagaConfig{
		StepTimeout:         5 * time.Second,
		CompensationTimeout: 5 * time.Second,
		CompensateOnFail:    true,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
