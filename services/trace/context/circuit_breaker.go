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
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed allows requests through normally.
	CircuitClosed CircuitState = iota

	// CircuitOpen rejects all requests immediately.
	CircuitOpen

	// CircuitHalfOpen allows limited requests to test recovery.
	CircuitHalfOpen
)

// String returns the human-readable name for the circuit state.
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
	// FailureThreshold is the number of consecutive failures before opening.
	// Default: 5
	FailureThreshold int

	// ResetTimeout is the duration to wait before transitioning from open to half-open.
	// Default: 30s
	ResetTimeout time.Duration

	// HalfOpenMaxRequests is the max requests allowed in half-open state.
	// Default: 2
	HalfOpenMaxRequests int

	// SuccessThreshold is the number of consecutive successes in half-open to close.
	// Default: 2
	SuccessThreshold int
}

// DefaultCircuitBreakerConfig returns sensible defaults for the circuit breaker.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 2,
		SuccessThreshold:    2,
	}
}

// CircuitBreaker implements the circuit breaker pattern for fault tolerance.
//
// The circuit breaker has three states:
// - Closed: Normal operation, requests pass through
// - Open: Failure threshold exceeded, requests are rejected immediately
// - Half-Open: Testing recovery, limited requests allowed
//
// Thread Safety: Safe for concurrent use.
type CircuitBreaker struct {
	config CircuitBreakerConfig

	state                CircuitState
	consecutiveFailures  int
	consecutiveSuccesses int
	halfOpenRequests     int
	lastFailureTime      time.Time
	lastStateChange      time.Time

	mu sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
//
// Inputs:
//   - config: Configuration for thresholds and timeouts.
//
// Outputs:
//   - *CircuitBreaker: A new circuit breaker in closed state.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	now := time.Now()
	return &CircuitBreaker{
		config:          config,
		state:           CircuitClosed,
		lastStateChange: now,
	}
}

// Allow checks if a request should be allowed through.
//
// Returns true if the request is allowed, false if it should be rejected.
// In half-open state, this also tracks the number of probe requests.
//
// Outputs:
//   - bool: True if request is allowed.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if now.Sub(cb.lastFailureTime) >= cb.config.ResetTimeout {
			cb.transitionTo(CircuitHalfOpen, now)
			cb.halfOpenRequests = 1
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow limited requests in half-open
		if cb.halfOpenRequests < cb.config.HalfOpenMaxRequests {
			cb.halfOpenRequests++
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful request.
//
// In half-open state, consecutive successes may close the circuit.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitClosed:
		// Reset failure count on success
		cb.consecutiveFailures = 0

	case CircuitHalfOpen:
		cb.consecutiveSuccesses++
		cb.consecutiveFailures = 0

		// Close circuit after enough successes
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.transitionTo(CircuitClosed, now)
		}
	}
}

// RecordFailure records a failed request.
//
// Consecutive failures may open the circuit.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.lastFailureTime = now

	switch cb.state {
	case CircuitClosed:
		cb.consecutiveFailures++
		cb.consecutiveSuccesses = 0

		// Open circuit after threshold failures
		if cb.consecutiveFailures >= cb.config.FailureThreshold {
			cb.transitionTo(CircuitOpen, now)
		}

	case CircuitHalfOpen:
		// Any failure in half-open reopens the circuit
		cb.transitionTo(CircuitOpen, now)
	}
}

// State returns the current circuit state.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns current circuit breaker statistics.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:                cb.state,
		ConsecutiveFailures:  cb.consecutiveFailures,
		ConsecutiveSuccesses: cb.consecutiveSuccesses,
		LastFailureTime:      cb.lastFailureTime,
		LastStateChange:      cb.lastStateChange,
	}
}

// Reset resets the circuit breaker to closed state.
//
// This is primarily for testing or manual intervention.
//
// Thread Safety: Safe for concurrent use.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.state = CircuitClosed
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.halfOpenRequests = 0
	cb.lastStateChange = now
}

// transitionTo changes the circuit state.
// Must be called with lock held.
func (cb *CircuitBreaker) transitionTo(newState CircuitState, now time.Time) {
	cb.state = newState
	cb.lastStateChange = now
	cb.consecutiveSuccesses = 0
	cb.halfOpenRequests = 0

	if newState == CircuitClosed {
		cb.consecutiveFailures = 0
	}
}

// CircuitBreakerStats contains circuit breaker statistics.
type CircuitBreakerStats struct {
	State                CircuitState
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	LastFailureTime      time.Time
	LastStateChange      time.Time
}

// TimeSinceLastFailure returns the duration since the last failure.
func (s CircuitBreakerStats) TimeSinceLastFailure() time.Duration {
	if s.LastFailureTime.IsZero() {
		return 0
	}
	return time.Since(s.LastFailureTime)
}

// TimeSinceStateChange returns the duration since the last state change.
func (s CircuitBreakerStats) TimeSinceStateChange() time.Duration {
	return time.Since(s.LastStateChange)
}
