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
	"context"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior with exponential backoff.
type RetryConfig struct {
	// MaxAttempts is the maximum number of attempts (including initial).
	// Default: 3
	MaxAttempts int

	// InitialBackoff is the initial wait duration before first retry.
	// Default: 1s
	InitialBackoff time.Duration

	// MaxBackoff is the maximum wait duration between retries.
	// Default: 30s
	MaxBackoff time.Duration

	// BackoffFactor is the multiplier for exponential backoff.
	// Default: 2.0
	BackoffFactor float64

	// JitterFactor is the maximum jitter as a fraction of backoff (0-1).
	// Adds randomness to prevent thundering herd. Default: 0.2
	JitterFactor float64
}

// DefaultRetryConfig returns sensible defaults for retry behavior.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		JitterFactor:   0.2,
	}
}

// Validate checks if the retry configuration is valid.
func (c RetryConfig) Validate() error {
	if c.MaxAttempts < 1 {
		return ErrInvalidBudget // reuse existing error
	}
	if c.InitialBackoff <= 0 {
		return ErrInvalidBudget
	}
	if c.MaxBackoff < c.InitialBackoff {
		return ErrInvalidBudget
	}
	if c.BackoffFactor < 1.0 {
		return ErrInvalidBudget
	}
	return nil
}

// RetryResult contains the outcome of a retry operation.
type RetryResult struct {
	// Attempts is the number of attempts made.
	Attempts int

	// TotalDuration is the total time spent including waits.
	TotalDuration time.Duration

	// LastError is the error from the last attempt (nil if successful).
	LastError error
}

// RetryableFunc is a function that can be retried.
// It should return nil on success, or an error.
// Use IsRetryable to determine if the error should trigger a retry.
type RetryableFunc func(ctx context.Context, attempt int) error

// Retry executes the given function with exponential backoff retry.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - config: Retry configuration.
//   - fn: The function to execute and potentially retry.
//
// Outputs:
//   - RetryResult: Statistics about the retry operation.
//   - error: The last error if all attempts failed, nil on success.
//
// The function is retried only if it returns a retryable error
// (as determined by IsRetryable). Non-retryable errors cause
// immediate return without further attempts.
//
// Example:
//
//	result, err := Retry(ctx, DefaultRetryConfig(), func(ctx context.Context, attempt int) error {
//	    return client.Complete(ctx, prompt)
//	})
func Retry(ctx context.Context, config RetryConfig, fn RetryableFunc) (RetryResult, error) {
	start := time.Now()
	result := RetryResult{}

	backoff := config.InitialBackoff

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Check context before attempting
		if err := ctx.Err(); err != nil {
			result.LastError = err
			result.TotalDuration = time.Since(start)
			return result, err
		}

		// Execute the function
		err := fn(ctx, attempt)
		if err == nil {
			result.TotalDuration = time.Since(start)
			return result, nil
		}

		result.LastError = err

		// Check if we should retry
		if !IsRetryable(err) {
			result.TotalDuration = time.Since(start)
			return result, err
		}

		// Don't wait after the last attempt
		if attempt == config.MaxAttempts {
			break
		}

		// Calculate wait time with jitter
		waitTime := calculateBackoff(backoff, config.JitterFactor)

		// Wait or cancel
		select {
		case <-ctx.Done():
			result.LastError = ctx.Err()
			result.TotalDuration = time.Since(start)
			return result, ctx.Err()
		case <-time.After(waitTime):
		}

		// Increase backoff for next attempt
		backoff = nextBackoff(backoff, config.BackoffFactor, config.MaxBackoff)
	}

	result.TotalDuration = time.Since(start)
	return result, result.LastError
}

// calculateBackoff calculates the actual backoff with jitter.
func calculateBackoff(base time.Duration, jitterFactor float64) time.Duration {
	if jitterFactor <= 0 {
		return base
	}

	// Add random jitter: base * (1 - jitterFactor + random * 2 * jitterFactor)
	// This gives a range of [base * (1-jitter), base * (1+jitter)]
	jitter := (rand.Float64()*2 - 1) * jitterFactor
	multiplier := 1.0 + jitter

	return time.Duration(float64(base) * multiplier)
}

// nextBackoff calculates the next backoff value.
func nextBackoff(current time.Duration, factor float64, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * factor)
	if next > max {
		return max
	}
	return next
}

// RetryWithCircuitBreaker combines retry logic with circuit breaker protection.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - cb: Circuit breaker to check/update.
//   - config: Retry configuration.
//   - fn: The function to execute.
//
// Outputs:
//   - RetryResult: Statistics about the operation.
//   - error: The error if failed, nil on success.
//
// If the circuit breaker is open, returns ErrCircuitOpen immediately.
// Records success/failure to the circuit breaker after each attempt.
func RetryWithCircuitBreaker(
	ctx context.Context,
	cb *CircuitBreaker,
	config RetryConfig,
	fn RetryableFunc,
) (RetryResult, error) {
	start := time.Now()
	result := RetryResult{}

	// Check circuit breaker first
	if !cb.Allow() {
		result.TotalDuration = time.Since(start)
		result.LastError = ErrCircuitOpen
		return result, ErrCircuitOpen
	}

	backoff := config.InitialBackoff

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Check context
		if err := ctx.Err(); err != nil {
			result.LastError = err
			result.TotalDuration = time.Since(start)
			return result, err
		}

		// For retries, check circuit breaker again
		if attempt > 1 && !cb.Allow() {
			result.LastError = ErrCircuitOpen
			result.TotalDuration = time.Since(start)
			return result, ErrCircuitOpen
		}

		// Execute the function
		err := fn(ctx, attempt)
		if err == nil {
			cb.RecordSuccess()
			result.TotalDuration = time.Since(start)
			return result, nil
		}

		cb.RecordFailure()
		result.LastError = err

		// Check if we should retry
		if !IsRetryable(err) {
			result.TotalDuration = time.Since(start)
			return result, err
		}

		// Don't wait after the last attempt
		if attempt == config.MaxAttempts {
			break
		}

		// Wait with jitter
		waitTime := calculateBackoff(backoff, config.JitterFactor)
		select {
		case <-ctx.Done():
			result.LastError = ctx.Err()
			result.TotalDuration = time.Since(start)
			return result, ctx.Err()
		case <-time.After(waitTime):
		}

		backoff = nextBackoff(backoff, config.BackoffFactor, config.MaxBackoff)
	}

	result.TotalDuration = time.Since(start)
	return result, result.LastError
}
