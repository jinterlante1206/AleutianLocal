// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	if config.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", config.FailureThreshold)
	}
	if config.SuccessThreshold != 2 {
		t.Errorf("SuccessThreshold = %d, want 2", config.SuccessThreshold)
	}
	if config.OpenDuration != 30*time.Second {
		t.Errorf("OpenDuration = %v, want 30s", config.OpenDuration)
	}
	if config.HalfOpenMax != 1 {
		t.Errorf("HalfOpenMax = %d, want 1", config.HalfOpenMax)
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker(config)

	if cb == nil {
		t.Fatal("NewCircuitBreaker returned nil")
	}
	if cb.State() != CircuitClosed {
		t.Errorf("Initial state = %v, want closed", cb.State())
	}
}

func TestCircuitBreaker_Allow_Closed(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	// Should allow in closed state
	allowed, release := cb.Allow()
	if !allowed {
		t.Error("Should allow in closed state")
	}
	if release != nil {
		t.Error("Release should be nil in closed state")
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Record failures
	for i := 0; i < 3; i++ {
		cb.Allow()
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Errorf("State = %v, want open", cb.State())
	}

	// Should reject in open state
	allowed, _ := cb.Allow()
	if allowed {
		t.Error("Should reject in open state")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("State = %v, want open", cb.State())
	}

	// Wait for open duration
	time.Sleep(20 * time.Millisecond)

	// Should transition to half-open
	allowed, release := cb.Allow()
	if !allowed {
		t.Error("Should allow first request in half-open")
	}
	if release == nil {
		t.Error("Release should not be nil in half-open")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("State = %v, want half-open", cb.State())
	}

	// Release the request
	release()
}

func TestCircuitBreaker_HalfOpenLimitsRequests(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait for open duration
	time.Sleep(20 * time.Millisecond)

	// First request should be allowed
	allowed1, release1 := cb.Allow()
	if !allowed1 {
		t.Fatal("First request should be allowed in half-open")
	}

	// Second request should be rejected (half-open limit)
	allowed2, _ := cb.Allow()
	if allowed2 {
		t.Error("Second request should be rejected in half-open")
	}

	// Release first
	release1()

	// Now another should be allowed
	allowed3, release3 := cb.Allow()
	if !allowed3 {
		t.Error("Should allow after release")
	}
	if release3 != nil {
		release3()
	}
}

func TestCircuitBreaker_ClosesAfterSuccesses(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait for open duration
	time.Sleep(20 * time.Millisecond)

	// Record successes in half-open
	for i := 0; i < 2; i++ {
		allowed, release := cb.Allow()
		if !allowed {
			t.Fatalf("Request %d should be allowed", i)
		}
		if release != nil {
			release()
		}
		cb.RecordSuccess()
	}

	if cb.State() != CircuitClosed {
		t.Errorf("State = %v, want closed", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
		HalfOpenMax:      1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait for open duration
	time.Sleep(20 * time.Millisecond)

	// Try in half-open
	allowed, release := cb.Allow()
	if !allowed {
		t.Fatal("Should allow in half-open")
	}
	if release != nil {
		release()
	}

	// Fail - should reopen
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("State = %v, want open", cb.State())
	}
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Execute returned error: %v", err)
	}
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	testErr := errors.New("test error")

	err := cb.Execute(context.Background(), func() error {
		return testErr
	})

	if err != testErr {
		t.Errorf("Execute returned %v, want %v", err, testErr)
	}

	stats := cb.Stats()
	if stats.TotalFailures != 1 {
		t.Errorf("TotalFailures = %d, want 1", stats.TotalFailures)
	}
}

func TestCircuitBreaker_Execute_CircuitOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})

	// Should return ErrCircuitOpen
	err := cb.Execute(context.Background(), func() error {
		t.Error("Function should not be called when circuit is open")
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute returned %v, want ErrCircuitOpen", err)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	// Make some calls
	cb.Allow()
	cb.RecordSuccess()
	cb.Allow()
	cb.RecordFailure()

	stats := cb.Stats()

	if stats.State != "closed" {
		t.Errorf("State = %s, want closed", stats.State)
	}
	if stats.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d, want 2", stats.TotalCalls)
	}
	if stats.TotalFailures != 1 {
		t.Errorf("TotalFailures = %d, want 1", stats.TotalFailures)
	}
	if stats.CurrentFailures != 1 {
		t.Errorf("CurrentFailures = %d, want 1", stats.CurrentFailures)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatal("Circuit should be open")
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("State = %v, want closed", cb.State())
	}

	// Should allow again
	allowed, _ := cb.Allow()
	if !allowed {
		t.Error("Should allow after reset")
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 100,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			allowed, release := cb.Allow()
			if allowed {
				if idx%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
				if release != nil {
					release()
				}
			}
		}(i)
	}

	wg.Wait()

	stats := cb.Stats()
	if stats.TotalCalls != numGoroutines {
		t.Errorf("TotalCalls = %d, want %d", stats.TotalCalls, numGoroutines)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)

	// Record some failures
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	// Success should reset failure count
	cb.Allow()
	cb.RecordSuccess()

	// Two more failures shouldn't open (only 2 consecutive)
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Errorf("State = %v, want closed (failures were reset)", cb.State())
	}

	// One more failure should open
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("State = %v, want open", cb.State())
	}
}
