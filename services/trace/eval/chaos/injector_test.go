// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package chaos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Mock Target
// -----------------------------------------------------------------------------

type mockTarget struct {
	name         string
	healthErr    error
	healthyAfter time.Time
	mu           sync.Mutex
}

func newMockTarget(name string) *mockTarget {
	return &mockTarget{name: name}
}

func (m *mockTarget) Name() string                     { return m.name }
func (m *mockTarget) Properties() []eval.Property      { return nil }
func (m *mockTarget) Metrics() []eval.MetricDefinition { return nil }

func (m *mockTarget) HealthCheck(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.healthyAfter.IsZero() && time.Now().Before(m.healthyAfter) {
		return errors.New("unhealthy")
	}
	return m.healthErr
}

func (m *mockTarget) SetUnhealthyFor(d time.Duration) {
	m.mu.Lock()
	m.healthyAfter = time.Now().Add(d)
	m.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Fault Tests
// -----------------------------------------------------------------------------

func TestLatencyFault(t *testing.T) {
	t.Run("inject and revert", func(t *testing.T) {
		fault := NewLatencyFault(10*time.Millisecond, 50*time.Millisecond)

		if fault.IsActive() {
			t.Error("expected fault to be inactive initially")
		}

		err := fault.Inject(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !fault.IsActive() {
			t.Error("expected fault to be active after inject")
		}

		err = fault.Revert(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fault.IsActive() {
			t.Error("expected fault to be inactive after revert")
		}
	})

	t.Run("double inject fails", func(t *testing.T) {
		fault := NewLatencyFault(10*time.Millisecond, 50*time.Millisecond)
		fault.Inject(context.Background())

		err := fault.Inject(context.Background())
		if err != ErrFaultActive {
			t.Errorf("expected ErrFaultActive, got %v", err)
		}

		fault.Revert(context.Background())
	})

	t.Run("double revert fails", func(t *testing.T) {
		fault := NewLatencyFault(10*time.Millisecond, 50*time.Millisecond)

		err := fault.Revert(context.Background())
		if err != ErrFaultInactive {
			t.Errorf("expected ErrFaultInactive, got %v", err)
		}
	})

	t.Run("apply adds latency", func(t *testing.T) {
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		start := time.Now()
		fault.Apply(context.Background(), nil)
		elapsed := time.Since(start)

		if elapsed < 10*time.Millisecond {
			t.Errorf("expected at least 10ms delay, got %v", elapsed)
		}
	})

	t.Run("apply without inject does nothing", func(t *testing.T) {
		fault := NewLatencyFault(100*time.Millisecond, 200*time.Millisecond)

		start := time.Now()
		fault.Apply(context.Background(), nil)
		elapsed := time.Since(start)

		if elapsed > 10*time.Millisecond {
			t.Errorf("expected no delay when inactive, got %v", elapsed)
		}
	})
}

func TestErrorFault(t *testing.T) {
	t.Run("inject and revert", func(t *testing.T) {
		fault := NewErrorFault(0.5, nil)

		if err := fault.Inject(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fault.IsActive() {
			t.Error("expected fault to be active")
		}

		if err := fault.Revert(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fault.IsActive() {
			t.Error("expected fault to be inactive")
		}
	})

	t.Run("apply returns error at rate", func(t *testing.T) {
		fault := NewErrorFault(1.0, errors.New("test error")) // 100% rate
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		err := fault.Apply(context.Background(), nil)
		if err == nil {
			t.Error("expected error at 100% rate")
		}
	})

	t.Run("apply without inject returns original", func(t *testing.T) {
		fault := NewErrorFault(1.0, errors.New("test error"))
		originalErr := errors.New("original")

		err := fault.Apply(context.Background(), originalErr)
		if err != originalErr {
			t.Errorf("expected original error, got %v", err)
		}
	})

	t.Run("stats tracking", func(t *testing.T) {
		fault := NewErrorFault(0.5, nil)
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		for i := 0; i < 100; i++ {
			fault.Apply(context.Background(), nil)
		}

		injected, total := fault.Stats()
		if total != 100 {
			t.Errorf("expected total 100, got %d", total)
		}
		// With 50% rate, should be roughly 50 injected (allow wide margin)
		if injected < 20 || injected > 80 {
			t.Errorf("expected ~50 injected with 50%% rate, got %d", injected)
		}
	})
}

func TestPanicFault(t *testing.T) {
	t.Run("inject and revert", func(t *testing.T) {
		fault := NewPanicFault(0.5, "test panic")

		if err := fault.Inject(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fault.IsActive() {
			t.Error("expected fault to be active")
		}

		if err := fault.Revert(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("apply can panic", func(t *testing.T) {
		fault := NewPanicFault(1.0, "test panic") // 100% rate
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic at 100% rate")
			}
		}()

		fault.Apply(context.Background(), nil)
	})

	t.Run("apply without inject does not panic", func(t *testing.T) {
		fault := NewPanicFault(1.0, "test panic")

		defer func() {
			if r := recover(); r != nil {
				t.Error("did not expect panic when inactive")
			}
		}()

		fault.Apply(context.Background(), nil)
	})
}

func TestTimeoutFault(t *testing.T) {
	t.Run("inject and revert", func(t *testing.T) {
		fault := NewTimeoutFault(0.5, 10*time.Millisecond)

		if err := fault.Inject(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fault.IsActive() {
			t.Error("expected fault to be active")
		}

		if err := fault.Revert(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("apply returns deadline exceeded", func(t *testing.T) {
		fault := NewTimeoutFault(1.0, 1*time.Millisecond) // 100% rate
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		err := fault.Apply(context.Background(), nil)
		if err != context.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", err)
		}
	})

	t.Run("wrap context adds timeout", func(t *testing.T) {
		fault := NewTimeoutFault(1.0, 10*time.Millisecond)
		fault.Inject(context.Background())
		defer fault.Revert(context.Background())

		ctx, cancel := fault.WrapContext(context.Background())
		defer cancel()

		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("expected context to be cancelled")
		}
	})
}

func TestCompositeFault(t *testing.T) {
	t.Run("inject all", func(t *testing.T) {
		fault1 := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)
		fault2 := NewErrorFault(0.5, nil)

		composite := NewCompositeFault("composite", fault1, fault2)

		if err := composite.Inject(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !fault1.IsActive() || !fault2.IsActive() {
			t.Error("expected all faults to be active")
		}

		if err := composite.Revert(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fault1.IsActive() || fault2.IsActive() {
			t.Error("expected all faults to be inactive")
		}
	})

	t.Run("partial inject rolls back", func(t *testing.T) {
		fault1 := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)
		fault2 := NewErrorFault(0.5, nil)
		fault2.Inject(context.Background()) // Pre-inject to make it fail

		composite := NewCompositeFault("composite", fault1, fault2)

		err := composite.Inject(context.Background())
		if err == nil {
			t.Error("expected error when sub-fault already active")
		}

		// First fault should have been rolled back
		if fault1.IsActive() {
			t.Error("expected first fault to be rolled back")
		}

		fault2.Revert(context.Background())
	})
}

// -----------------------------------------------------------------------------
// Scheduler Tests
// -----------------------------------------------------------------------------

func TestRandomScheduler(t *testing.T) {
	t.Run("respects probability", func(t *testing.T) {
		scheduler := NewRandomScheduler(0.5, 1*time.Second)
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

		injections := 0
		for i := 0; i < 1000; i++ {
			if scheduler.ShouldInject(fault) {
				injections++
			}
		}

		// With 50% probability, should be ~500 (allow 30% margin)
		if injections < 350 || injections > 650 {
			t.Errorf("expected ~500 injections with 50%% rate, got %d", injections)
		}
	})

	t.Run("should revert after max duration", func(t *testing.T) {
		scheduler := NewRandomScheduler(1.0, 100*time.Millisecond)
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

		if scheduler.ShouldRevert(fault, 50*time.Millisecond) {
			t.Error("should not revert before max duration")
		}

		if !scheduler.ShouldRevert(fault, 100*time.Millisecond) {
			t.Error("should revert after max duration")
		}
	})
}

func TestPeriodicScheduler(t *testing.T) {
	t.Run("injects at interval", func(t *testing.T) {
		scheduler := NewPeriodicScheduler(50*time.Millisecond, 20*time.Millisecond)
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

		// First injection should happen immediately
		if !scheduler.ShouldInject(fault) {
			t.Error("expected first injection")
		}

		// Second injection should not happen immediately
		if scheduler.ShouldInject(fault) {
			t.Error("should not inject before interval")
		}

		// Wait for interval
		time.Sleep(60 * time.Millisecond)

		if !scheduler.ShouldInject(fault) {
			t.Error("expected injection after interval")
		}
	})

	t.Run("should revert after duration", func(t *testing.T) {
		scheduler := NewPeriodicScheduler(100*time.Millisecond, 20*time.Millisecond)
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

		if scheduler.ShouldRevert(fault, 10*time.Millisecond) {
			t.Error("should not revert before duration")
		}

		if !scheduler.ShouldRevert(fault, 20*time.Millisecond) {
			t.Error("should revert after duration")
		}
	})
}

func TestManualScheduler(t *testing.T) {
	t.Run("enable and disable", func(t *testing.T) {
		scheduler := NewManualScheduler(1 * time.Second)
		fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

		if scheduler.ShouldInject(fault) {
			t.Error("should not inject when not enabled")
		}

		scheduler.Enable(fault.Name())

		if !scheduler.ShouldInject(fault) {
			t.Error("should inject when enabled")
		}

		scheduler.Disable(fault.Name())

		if scheduler.ShouldInject(fault) {
			t.Error("should not inject after disable")
		}
	})
}

func TestNoOpScheduler(t *testing.T) {
	scheduler := NewNoOpScheduler()
	fault := NewLatencyFault(10*time.Millisecond, 20*time.Millisecond)

	if scheduler.ShouldInject(fault) {
		t.Error("no-op should never inject")
	}

	if !scheduler.ShouldRevert(fault, 0) {
		t.Error("no-op should always revert")
	}
}

// -----------------------------------------------------------------------------
// Injector Tests
// -----------------------------------------------------------------------------

func TestInjector_Run(t *testing.T) {
	t.Run("basic run", func(t *testing.T) {
		target := newMockTarget("test")
		fault := NewErrorFault(0.5, nil)

		injector := NewInjector(
			WithFaults(fault),
			WithScheduler(NewPeriodicScheduler(10*time.Millisecond, 5*time.Millisecond)),
			WithMaxConcurrentFaults(1),
			WithHealthCheckInterval(5*time.Millisecond),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		result, err := injector.Run(ctx, target, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		if result.TargetName != "test" {
			t.Errorf("expected target name 'test', got '%s'", result.TargetName)
		}
	})

	t.Run("no faults error", func(t *testing.T) {
		target := newMockTarget("test")
		injector := NewInjector() // No faults

		_, err := injector.Run(context.Background(), target, time.Second)
		if err != ErrNoFaults {
			t.Errorf("expected ErrNoFaults, got %v", err)
		}
	})

	t.Run("nil context error", func(t *testing.T) {
		target := newMockTarget("test")
		fault := NewErrorFault(0.5, nil)
		injector := NewInjector(WithFaults(fault))

		_, err := injector.Run(nil, target, time.Second)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil target error", func(t *testing.T) {
		fault := NewErrorFault(0.5, nil)
		injector := NewInjector(WithFaults(fault))

		_, err := injector.Run(context.Background(), nil, time.Second)
		if err == nil {
			t.Error("expected error for nil target")
		}
	})

	t.Run("respects max concurrent faults", func(t *testing.T) {
		target := newMockTarget("test")
		fault1 := NewLatencyFault(1*time.Millisecond, 2*time.Millisecond)
		fault2 := NewErrorFault(0.5, nil)

		// Always inject, long duration
		scheduler := NewPeriodicScheduler(1*time.Millisecond, 1*time.Hour)

		injector := NewInjector(
			WithFaults(fault1, fault2),
			WithScheduler(scheduler),
			WithMaxConcurrentFaults(1),
			WithHealthCheckInterval(1*time.Millisecond),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		go injector.Run(ctx, target, 0)

		// Give time for injections
		time.Sleep(10 * time.Millisecond)

		active := injector.ActiveFaults()
		if len(active) > 1 {
			t.Errorf("expected max 1 active fault, got %d", len(active))
		}
	})
}

func TestInjector_Stop(t *testing.T) {
	target := newMockTarget("test")
	fault := NewErrorFault(0.5, nil)

	injector := NewInjector(
		WithFaults(fault),
		WithScheduler(NewPeriodicScheduler(100*time.Millisecond, 50*time.Millisecond)),
	)

	// Start in goroutine
	done := make(chan struct{})
	go func() {
		injector.Run(context.Background(), target, 0)
		close(done)
	}()

	// Give it time to start
	time.Sleep(10 * time.Millisecond)

	if !injector.IsRunning() {
		t.Error("expected injector to be running")
	}

	injector.Stop()

	// Should stop soon
	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("injector did not stop in time")
	}
}

func TestResult(t *testing.T) {
	t.Run("success when no failures", func(t *testing.T) {
		result := &Result{
			RecoveriesSuccess: 5,
			RecoveriesFailure: 0,
		}

		if !result.Success() {
			t.Error("expected success with no failures")
		}
	})

	t.Run("failure when recoveries fail", func(t *testing.T) {
		result := &Result{
			RecoveriesSuccess: 5,
			RecoveriesFailure: 1,
		}

		if result.Success() {
			t.Error("expected failure with failed recoveries")
		}
	})

	t.Run("failure rate", func(t *testing.T) {
		result := &Result{
			RecoveriesSuccess: 8,
			RecoveriesFailure: 2,
		}

		rate := result.FailureRate()
		if rate != 0.2 {
			t.Errorf("expected failure rate 0.2, got %f", rate)
		}
	})

	t.Run("failure rate with no recoveries", func(t *testing.T) {
		result := &Result{}

		rate := result.FailureRate()
		if rate != 0 {
			t.Errorf("expected failure rate 0, got %f", rate)
		}
	})
}

// -----------------------------------------------------------------------------
// Evaluable Implementation Tests
// -----------------------------------------------------------------------------

func TestInjector_Evaluable(t *testing.T) {
	injector := NewInjector()

	if name := injector.Name(); name != "chaos_injector" {
		t.Errorf("expected name 'chaos_injector', got '%s'", name)
	}

	props := injector.Properties()
	if len(props) == 0 {
		t.Error("expected at least one property")
	}

	metrics := injector.Metrics()
	if len(metrics) == 0 {
		t.Error("expected at least one metric")
	}

	if err := injector.HealthCheck(context.Background()); err != nil {
		t.Errorf("unexpected health check error: %v", err)
	}
}
