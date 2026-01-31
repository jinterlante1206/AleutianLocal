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
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RetryConfig
		wantErr bool
	}{
		{
			name:    "default config is valid",
			config:  DefaultRetryConfig(),
			wantErr: false,
		},
		{
			name:    "zero max attempts is invalid",
			config:  RetryConfig{MaxAttempts: 0, InitialBackoff: time.Second, MaxBackoff: time.Second, BackoffFactor: 2.0},
			wantErr: true,
		},
		{
			name:    "negative initial backoff is invalid",
			config:  RetryConfig{MaxAttempts: 3, InitialBackoff: -time.Second, MaxBackoff: time.Second, BackoffFactor: 2.0},
			wantErr: true,
		},
		{
			name:    "max backoff less than initial is invalid",
			config:  RetryConfig{MaxAttempts: 3, InitialBackoff: 10 * time.Second, MaxBackoff: time.Second, BackoffFactor: 2.0},
			wantErr: true,
		},
		{
			name:    "backoff factor less than 1 is invalid",
			config:  RetryConfig{MaxAttempts: 3, InitialBackoff: time.Second, MaxBackoff: time.Second, BackoffFactor: 0.5},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}

	var attempts int32
	result, err := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		atomic.AddInt32(&attempts, 1)
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", result.Attempts)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("function called %d times, want 1", attempts)
	}
}

func TestRetry_SuccessOnSecondAttempt(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}

	var attempts int32
	result, err := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			return ErrLLMRateLimited // Retryable
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", result.Attempts)
	}
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}

	var attempts int32
	retryableErr := ErrLLMRateLimited

	result, err := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		atomic.AddInt32(&attempts, 1)
		return retryableErr
	})

	if !errors.Is(err, retryableErr) {
		t.Fatalf("expected %v, got %v", retryableErr, err)
	}
	if result.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", result.Attempts)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("function called %d times, want 3", attempts)
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}

	var attempts int32
	nonRetryableErr := errors.New("non-retryable error")

	result, err := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		atomic.AddInt32(&attempts, 1)
		return nonRetryableErr
	})

	if !errors.Is(err, nonRetryableErr) {
		t.Fatalf("expected %v, got %v", nonRetryableErr, err)
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (no retry for non-retryable)", result.Attempts)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := RetryConfig{
		MaxAttempts:    5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     time.Second,
		BackoffFactor:  2.0,
	}

	var attempts int32
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		atomic.AddInt32(&attempts, 1)
		return ErrLLMRateLimited
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if result.Attempts > 3 {
		t.Errorf("too many attempts: %d", result.Attempts)
	}
}

func TestRetry_ExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    4,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
		JitterFactor:   0, // No jitter for predictable timing
	}

	start := time.Now()
	result, _ := Retry(ctx, config, func(ctx context.Context, attempt int) error {
		return ErrLLMRateLimited
	})
	duration := time.Since(start)

	// Expected: 10ms + 20ms + 40ms = 70ms (3 waits between 4 attempts)
	// Allow some tolerance
	expectedMin := 60 * time.Millisecond
	expectedMax := 100 * time.Millisecond

	if duration < expectedMin || duration > expectedMax {
		t.Errorf("Duration = %v, expected between %v and %v", duration, expectedMin, expectedMax)
	}

	if result.Attempts != 4 {
		t.Errorf("Attempts = %d, want 4", result.Attempts)
	}
}

func TestRetryWithCircuitBreaker_CircuitOpen(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    1,
		ResetTimeout:        time.Hour,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	})

	// Trip the circuit
	cb.RecordFailure()

	var attempts int32
	result, err := RetryWithCircuitBreaker(ctx, cb, config, func(ctx context.Context, attempt int) error {
		atomic.AddInt32(&attempts, 1)
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if atomic.LoadInt32(&attempts) != 0 {
		t.Errorf("function should not be called when circuit is open")
	}
	if result.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0", result.Attempts)
	}
}

func TestRetryWithCircuitBreaker_RecordsSuccess(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	// Record a failure first
	cb.RecordFailure()

	_, err := RetryWithCircuitBreaker(ctx, cb, config, func(ctx context.Context, attempt int) error {
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have recorded success
	stats := cb.Stats()
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("failures should be reset after success")
	}
}

func TestRetryWithCircuitBreaker_RecordsFailure(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		BackoffFactor:  2.0,
	}
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold:    10, // High threshold so it doesn't open
		ResetTimeout:        time.Hour,
		HalfOpenMaxRequests: 1,
		SuccessThreshold:    1,
	})

	_, _ = RetryWithCircuitBreaker(ctx, cb, config, func(ctx context.Context, attempt int) error {
		return ErrLLMRateLimited
	})

	// Should have recorded 3 failures (one per attempt)
	stats := cb.Stats()
	if stats.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", stats.ConsecutiveFailures)
	}
}

func TestCalculateBackoff_NoJitter(t *testing.T) {
	base := 100 * time.Millisecond
	result := calculateBackoff(base, 0)

	if result != base {
		t.Errorf("calculateBackoff with no jitter = %v, want %v", result, base)
	}
}

func TestCalculateBackoff_WithJitter(t *testing.T) {
	base := 100 * time.Millisecond
	jitter := 0.2

	// Run multiple times to check range
	for i := 0; i < 100; i++ {
		result := calculateBackoff(base, jitter)

		minExpected := time.Duration(float64(base) * (1 - jitter))
		maxExpected := time.Duration(float64(base) * (1 + jitter))

		if result < minExpected || result > maxExpected {
			t.Errorf("calculateBackoff = %v, expected in range [%v, %v]", result, minExpected, maxExpected)
		}
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current  time.Duration
		factor   float64
		max      time.Duration
		expected time.Duration
	}{
		{time.Second, 2.0, time.Minute, 2 * time.Second},
		{30 * time.Second, 2.0, time.Minute, time.Minute}, // Capped at max
		{time.Second, 3.0, time.Minute, 3 * time.Second},
	}

	for _, tt := range tests {
		result := nextBackoff(tt.current, tt.factor, tt.max)
		if result != tt.expected {
			t.Errorf("nextBackoff(%v, %v, %v) = %v, want %v",
				tt.current, tt.factor, tt.max, result, tt.expected)
		}
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", config.MaxAttempts)
	}
	if config.InitialBackoff != time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", config.InitialBackoff)
	}
	if config.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", config.MaxBackoff)
	}
	if config.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %f, want 2.0", config.BackoffFactor)
	}
	if config.JitterFactor != 0.2 {
		t.Errorf("JitterFactor = %f, want 0.2", config.JitterFactor)
	}
}

func TestRetryResult_Fields(t *testing.T) {
	result := RetryResult{
		Attempts:      3,
		TotalDuration: 5 * time.Second,
		LastError:     errors.New("test error"),
	}

	if result.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", result.Attempts)
	}
	if result.TotalDuration != 5*time.Second {
		t.Errorf("TotalDuration = %v, want 5s", result.TotalDuration)
	}
	if result.LastError == nil {
		t.Error("expected LastError to be set")
	}
}
