// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state Closed, got %v", cb.State())
	}

	if !cb.Allow() {
		t.Error("expected Allow() to return true in closed state")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    3,
		ResetTimeout:        10 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	}
	cb := NewCircuitBreaker(config)

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		if cb.State() != CircuitClosed {
			t.Fatalf("expected Closed state before threshold, got %v at iteration %d", cb.State(), i)
		}
		cb.RecordFailure()
	}

	// Should now be open
	if cb.State() != CircuitOpen {
		t.Errorf("expected Open state after threshold, got %v", cb.State())
	}

	// Allow should return false
	if cb.Allow() {
		t.Error("expected Allow() to return false in open state")
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    3,
		ResetTimeout:        10 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	}
	cb := NewCircuitBreaker(config)

	// Record 2 failures
	cb.RecordFailure()
	cb.RecordFailure()

	// Then a success
	cb.RecordSuccess()

	// 2 more failures shouldn't open it (counter was reset)
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed state (counter should have reset), got %v", cb.State())
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open state, got %v", cb.State())
	}

	// Wait for reset timeout
	time.Sleep(20 * time.Millisecond)

	// Allow should transition to half-open
	if !cb.Allow() {
		t.Error("expected Allow() to return true after timeout")
	}

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected HalfOpen state, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenClosesOnSuccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    2,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(20 * time.Millisecond)
	cb.Allow()

	// Record successes
	cb.RecordSuccess()
	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected still HalfOpen after 1 success, got %v", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed after 2 successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenOpensOnFailure(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    2,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(20 * time.Millisecond)
	cb.Allow()

	// Record failure in half-open
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected Open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenLimitsRequests(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    3,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(20 * time.Millisecond)

	// First 2 requests should be allowed
	if !cb.Allow() {
		t.Error("expected first request allowed in half-open")
	}
	if !cb.Allow() {
		t.Error("expected second request allowed in half-open")
	}

	// Third request should be rejected
	if cb.Allow() {
		t.Error("expected third request rejected in half-open")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        10 * time.Second,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	}
	cb := NewCircuitBreaker(config)

	// Open the circuit
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected Open state, got %v", cb.State())
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected Closed state after reset, got %v", cb.State())
	}

	if !cb.Allow() {
		t.Error("expected Allow() to return true after reset")
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	stats := cb.Stats()

	if stats.State != CircuitClosed {
		t.Errorf("expected Closed state, got %v", stats.State)
	}

	// Failures were reset by success
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("expected 0 consecutive failures, got %d", stats.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold:    100,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 10,
		SuccessThreshold:    5,
	}
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	iterations := 1000

	// Concurrent allows
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cb.Allow()
		}
	}()

	// Concurrent successes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/2; i++ {
			cb.RecordSuccess()
		}
	}()

	// Concurrent failures
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/2; i++ {
			cb.RecordFailure()
		}
	}()

	// Concurrent state reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = cb.State()
			_ = cb.Stats()
		}
	}()

	wg.Wait()

	// Should not panic - that's the main test
	// State is undefined due to concurrent modifications
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}
