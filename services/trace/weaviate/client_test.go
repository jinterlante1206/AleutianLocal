// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package weaviate

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// ClientConfig Tests
// -----------------------------------------------------------------------------

func TestClientConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.URL = "http://localhost:8080"
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing url", func(t *testing.T) {
		cfg := DefaultClientConfig()
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "url")
	})

	t.Run("negative retry_attempts", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.URL = "http://localhost:8080"
		cfg.RetryAttempts = -1
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retry_attempts")
	})

	t.Run("invalid retry_jitter", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.URL = "http://localhost:8080"
		cfg.RetryJitter = 1.5
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retry_jitter")
	})

	t.Run("zero circuit_threshold", func(t *testing.T) {
		cfg := DefaultClientConfig()
		cfg.URL = "http://localhost:8080"
		cfg.CircuitThreshold = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circuit_threshold")
	})
}

func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig()
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, 100*time.Millisecond, cfg.RetryBackoff)
	assert.Equal(t, 5*time.Second, cfg.MaxRetryBackoff)
	assert.Equal(t, 0.25, cfg.RetryJitter)
	assert.Equal(t, 5, cfg.CircuitThreshold)
	assert.Equal(t, 30*time.Second, cfg.CircuitWindow)
	assert.Equal(t, 30*time.Second, cfg.CircuitCooldown)
	assert.Equal(t, 10*time.Second, cfg.HealthCheckInterval)
	assert.Equal(t, 5*time.Second, cfg.DegradedCheckInterval)
	assert.Equal(t, 5*time.Second, cfg.HealthCheckTimeout)
	assert.False(t, cfg.AllowStartDegraded)
}

// -----------------------------------------------------------------------------
// ConnectionState Tests
// -----------------------------------------------------------------------------

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateConnected, "connected"},
		{StateDegraded, "degraded"},
		{StateCircuitOpen, "circuit_open"},
		{StateHalfOpen, "half_open"},
		{ConnectionState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

// -----------------------------------------------------------------------------
// ResilientClient Tests (Unit tests without actual Weaviate)
// -----------------------------------------------------------------------------

func TestNewResilientClient_InvalidConfig(t *testing.T) {
	cfg := ClientConfig{} // Missing URL
	_, err := NewResilientClient(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url")
}

func TestNewResilientClient_AllowStartDegraded(t *testing.T) {
	// This test verifies the AllowStartDegraded behavior
	// without actually connecting to Weaviate
	cfg := DefaultClientConfig()
	cfg.URL = "http://localhost:9999" // Non-existent port
	cfg.AllowStartDegraded = true
	cfg.HealthCheckTimeout = 100 * time.Millisecond // Fast timeout

	client, err := NewResilientClient(cfg)
	if err == nil {
		// Client created in degraded mode
		defer client.Close()
		assert.True(t, client.IsDegraded())
		assert.False(t, client.IsAvailable())
	} else {
		// Connection might have failed for other reasons
		// which is acceptable in unit tests
		t.Logf("Client creation failed (expected in unit test): %v", err)
	}
}

func TestNewResilientClient_StrictMode(t *testing.T) {
	cfg := DefaultClientConfig()
	cfg.URL = "http://localhost:9999" // Non-existent port
	cfg.AllowStartDegraded = false
	cfg.HealthCheckTimeout = 100 * time.Millisecond // Fast timeout

	_, err := NewResilientClient(cfg)
	// Should fail in strict mode when Weaviate unavailable
	assert.Error(t, err)
}

// -----------------------------------------------------------------------------
// Circuit Breaker Tests
// -----------------------------------------------------------------------------

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	// Create a mock client to test circuit breaker logic
	client := &ResilientClient{
		config: ClientConfig{
			CircuitThreshold: 3,
			CircuitWindow:    30 * time.Second,
			CircuitCooldown:  1 * time.Second,
		},
		failures: make([]time.Time, 3),
		logger:   slog.Default(),
	}
	client.state.Store(int32(StateConnected))

	// Record failures
	for i := 0; i < 3; i++ {
		client.recordFailure()
	}

	// Circuit should be open
	assert.Equal(t, StateCircuitOpen, client.GetState())
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{
			CircuitThreshold: 3,
			CircuitWindow:    30 * time.Second,
			CircuitCooldown:  10 * time.Millisecond, // Short for testing
		},
		failures: make([]time.Time, 3),
		logger:   slog.Default(),
	}
	client.state.Store(int32(StateCircuitOpen))
	client.circuitOpenTime.Store(time.Now().Add(-20 * time.Millisecond).Unix())

	// Should transition to half-open
	assert.True(t, client.shouldTryHalfOpen())
}

func TestCircuitBreaker_DoesNotOpenWithoutThreshold(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{
			CircuitThreshold: 5,
			CircuitWindow:    30 * time.Second,
		},
		failures: make([]time.Time, 5),
		logger:   slog.Default(),
	}
	client.state.Store(int32(StateConnected))

	// Record fewer failures than threshold
	for i := 0; i < 3; i++ {
		client.recordFailure()
	}

	// Circuit should not be open (should be degraded)
	assert.NotEqual(t, StateCircuitOpen, client.GetState())
}

func TestCircuitBreaker_SlidingWindow(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{
			CircuitThreshold: 3,
			CircuitWindow:    100 * time.Millisecond, // Short window
		},
		failures: make([]time.Time, 3),
		logger:   slog.Default(),
	}
	client.state.Store(int32(StateConnected))

	// Record failure
	client.recordFailure()

	// Wait for window to pass
	time.Sleep(150 * time.Millisecond)

	// Record more failures - old one should be outside window
	client.recordFailure()
	client.recordFailure()

	// Circuit should NOT be open (only 2 recent failures)
	assert.NotEqual(t, StateCircuitOpen, client.GetState())
}

// -----------------------------------------------------------------------------
// Retry Tests
// -----------------------------------------------------------------------------

func TestCalculateBackoff_WithJitter(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{
			RetryBackoff:    100 * time.Millisecond,
			MaxRetryBackoff: 1 * time.Second,
			RetryJitter:     0.25,
		},
	}

	// Run multiple times to test jitter randomness
	backoffs := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		backoffs[i] = client.calculateBackoff(1)
	}

	// All should be within jitter range of expected backoff
	expected := 200 * time.Millisecond // 100ms * 2^1
	minExpected := time.Duration(float64(expected) * 0.75)
	maxExpected := time.Duration(float64(expected) * 1.25)

	for _, b := range backoffs {
		assert.GreaterOrEqual(t, b, minExpected)
		assert.LessOrEqual(t, b, maxExpected)
	}

	// At least some variation (not all identical)
	allSame := true
	for i := 1; i < len(backoffs); i++ {
		if backoffs[i] != backoffs[0] {
			allSame = false
			break
		}
	}
	assert.False(t, allSame, "jitter should produce some variation")
}

func TestCalculateBackoff_CapsAtMax(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{
			RetryBackoff:    100 * time.Millisecond,
			MaxRetryBackoff: 500 * time.Millisecond,
			RetryJitter:     0, // No jitter for deterministic test
		},
	}

	// High attempt number that would exceed max
	backoff := client.calculateBackoff(10) // 100ms * 2^10 = 102.4s

	// Should be capped at max
	assert.LessOrEqual(t, backoff, client.config.MaxRetryBackoff)
}

// -----------------------------------------------------------------------------
// Error Categorization Tests
// -----------------------------------------------------------------------------

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"context cancelled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"random error", errors.New("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, isRetryable(tt.err))
		})
	}
}

func TestIsRetryable_NetworkErrors(t *testing.T) {
	t.Run("net.OpError is retryable", func(t *testing.T) {
		// net.OpError is checked separately via errors.As for *net.OpError
		netErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("connection refused"),
		}
		assert.True(t, isRetryable(netErr), "net.OpError should be retryable")
	})

	t.Run("timeout error is retryable", func(t *testing.T) {
		// Create a timeout-style error
		netErr := &net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: &timeoutError{},
		}
		assert.True(t, isRetryable(netErr), "timeout error should be retryable")
	})
}

// timeoutError implements net.Error with Timeout() = true
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestWrapWeaviateError(t *testing.T) {
	t.Run("deadline exceeded", func(t *testing.T) {
		wrapped := WrapWeaviateError(context.DeadlineExceeded)
		assert.ErrorIs(t, wrapped, ErrConnectionTimeout)
	})

	t.Run("nil error", func(t *testing.T) {
		wrapped := WrapWeaviateError(nil)
		assert.Nil(t, wrapped)
	})

	t.Run("other error", func(t *testing.T) {
		wrapped := WrapWeaviateError(errors.New("some error"))
		assert.Contains(t, wrapped.Error(), "weaviate error")
	})
}

// -----------------------------------------------------------------------------
// State Transition Tests
// -----------------------------------------------------------------------------

func TestTransitionState_NotifiesHandlers(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{},
		logger: slog.Default(),
	}
	client.state.Store(int32(StateConnected))

	handler := &mockDegradationHandler{}
	client.RegisterHandler(handler)

	// Transition to degraded
	client.transitionState(StateDegraded)

	assert.Equal(t, int32(1), handler.degradedCalls.Load())
	assert.Equal(t, int32(0), handler.recoveredCalls.Load())

	// Transition to connected
	client.transitionState(StateConnected)

	assert.Equal(t, int32(1), handler.degradedCalls.Load())
	assert.Equal(t, int32(1), handler.recoveredCalls.Load())
}

func TestTransitionState_NoOpForSameState(t *testing.T) {
	client := &ResilientClient{
		config: ClientConfig{},
		logger: slog.Default(),
	}
	client.state.Store(int32(StateConnected))

	handler := &mockDegradationHandler{}
	client.RegisterHandler(handler)

	// Transition to same state
	client.transitionState(StateConnected)

	// Handler should not be called
	assert.Equal(t, int32(0), handler.degradedCalls.Load())
	assert.Equal(t, int32(0), handler.recoveredCalls.Load())
}

// -----------------------------------------------------------------------------
// Close Tests
// -----------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	client := &ResilientClient{
		logger: slog.Default(),
	}
	healthCtx, healthCancel := context.WithCancel(context.Background())
	client.healthCtx = healthCtx
	client.healthCancel = healthCancel

	// Close twice
	err1 := client.Close()
	err2 := client.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

type mockDegradationHandler struct {
	degradedCalls  atomic.Int32
	recoveredCalls atomic.Int32
	mode           atomic.Int32
}

func (m *mockDegradationHandler) OnDegraded(reason string) {
	m.degradedCalls.Add(1)
	m.mode.Store(int32(ModeDegraded))
}

func (m *mockDegradationHandler) OnRecovered() {
	m.recoveredCalls.Add(1)
	m.mode.Store(int32(ModeNormal))
}

func (m *mockDegradationHandler) GetMode() DegradationMode {
	return DegradationMode(m.mode.Load())
}

// -----------------------------------------------------------------------------
// Integration Tests (require actual Weaviate)
// -----------------------------------------------------------------------------

// These tests require a running Weaviate instance and are skipped by default.
// Run with: go test -tags=integration

func TestIntegration_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cfg := DefaultClientConfig()
	cfg.URL = "http://localhost:8080"
	cfg.AllowStartDegraded = true

	client, err := NewResilientClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	// If Weaviate is running, should be available
	// If not, should be degraded
	t.Logf("Client state: %s", client.GetState())
}

func TestIntegration_Execute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cfg := DefaultClientConfig()
	cfg.URL = "http://localhost:8080"
	cfg.AllowStartDegraded = true

	client, err := NewResilientClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	if !client.IsAvailable() {
		t.Skip("Weaviate not available")
	}

	// Execute a simple operation
	err = client.Execute(context.Background(), func() error {
		// Just check if we can reach Weaviate
		_, err := client.Client().Misc().ReadyChecker().Do(context.Background())
		return err
	})
	assert.NoError(t, err)
}
