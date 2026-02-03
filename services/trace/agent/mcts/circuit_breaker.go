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
	"sync"
	"time"
)

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	// CircuitClosed is normal operation - requests pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen means too many failures - requests are rejected.
	CircuitOpen
	// CircuitHalfOpen is testing recovery - limited requests allowed.
	CircuitHalfOpen
)

// String returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the circuit breaker behavior.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening (default: 3).
	FailureThreshold int

	// SuccessThreshold is successes needed to close from half-open (default: 2).
	SuccessThreshold int

	// OpenDuration is how long to stay open before testing recovery (default: 30s).
	OpenDuration time.Duration

	// HalfOpenMax is max concurrent requests in half-open state (default: 1).
	HalfOpenMax int
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     30 * time.Second,
		HalfOpenMax:      1,
	}
}

// CircuitBreakerStats contains circuit breaker statistics.
type CircuitBreakerStats struct {
	State           string    `json:"state"`
	TotalCalls      int64     `json:"total_calls"`
	TotalFailures   int64     `json:"total_failures"`
	TotalRejections int64     `json:"total_rejections"`
	CurrentFailures int       `json:"current_failures"`
	LastStateChange time.Time `json:"last_state_change"`
}

// CircuitBreaker provides circuit breaker protection for LLM calls.
//
// The circuit breaker pattern prevents cascading failures by temporarily
// blocking requests after repeated failures. It has three states:
//
//   - Closed: Normal operation, requests pass through.
//   - Open: After FailureThreshold failures, requests are rejected.
//   - Half-Open: After OpenDuration, limited requests test recovery.
//
// Thread Safety: Safe for concurrent use.
type CircuitBreaker struct {
	config CircuitBreakerConfig

	mu              sync.RWMutex
	state           CircuitState
	failures        int
	successes       int
	lastStateChange time.Time
	halfOpenActive  int

	// Metrics
	totalCalls      int64
	totalFailures   int64
	totalRejections int64
}

// NewCircuitBreaker creates a new circuit breaker.
//
// Inputs:
//   - config: Circuit breaker configuration.
//
// Outputs:
//   - *CircuitBreaker: Ready to use circuit breaker.
//
// Thread Safety: The returned circuit breaker is safe for concurrent use.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config:          config,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Allow checks if a request should be allowed.
//
// Outputs:
//   - bool: True if the request should proceed.
//   - func(): Release function to call when request completes (may be nil).
//
// Usage:
//
//	allowed, release := cb.Allow()
//	if !allowed {
//	    return ErrCircuitOpen
//	}
//	if release != nil {
//	    defer release()
//	}
//	// ... make request ...
func (cb *CircuitBreaker) Allow() (bool, func()) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalCalls++

	switch cb.state {
	case CircuitClosed:
		return true, nil

	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastStateChange) > cb.config.OpenDuration {
			cb.transitionTo(CircuitHalfOpen)
			return cb.tryHalfOpen()
		}
		cb.totalRejections++
		return false, nil

	case CircuitHalfOpen:
		return cb.tryHalfOpen()
	}

	return false, nil
}

// tryHalfOpen attempts to allow a request in half-open state.
// Must be called with lock held.
func (cb *CircuitBreaker) tryHalfOpen() (bool, func()) {
	if cb.halfOpenActive >= cb.config.HalfOpenMax {
		cb.totalRejections++
		return false, nil
	}

	cb.halfOpenActive++
	return true, func() {
		cb.mu.Lock()
		cb.halfOpenActive--
		cb.mu.Unlock()
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(CircuitClosed)
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures++
	cb.failures++
	cb.successes = 0

	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(CircuitOpen)
		}
	case CircuitHalfOpen:
		cb.transitionTo(CircuitOpen)
	}
}

// transitionTo changes state. Must be called with lock held.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	cb.state = newState
	cb.lastStateChange = time.Now()
	cb.failures = 0
	cb.successes = 0
}

// Execute wraps a function with circuit breaker protection.
//
// Inputs:
//   - ctx: Context for the operation.
//   - fn: The function to execute.
//
// Outputs:
//   - error: ErrCircuitOpen if rejected, or the error from fn.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	allowed, release := cb.Allow()
	if !allowed {
		return ErrCircuitOpen
	}
	if release != nil {
		defer release()
	}

	err := fn()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// Stats returns circuit breaker statistics.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:           cb.state.String(),
		TotalCalls:      cb.totalCalls,
		TotalFailures:   cb.totalFailures,
		TotalRejections: cb.totalRejections,
		CurrentFailures: cb.failures,
		LastStateChange: cb.lastStateChange,
	}
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenActive = 0
	cb.lastStateChange = time.Now()
}
