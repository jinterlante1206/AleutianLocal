// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package weaviate provides a resilient Weaviate client with circuit breaker,
// retry with backoff, and graceful degradation.
//
// Features:
//   - Circuit breaker to prevent thundering herd
//   - Exponential backoff with jitter for retries
//   - Health checking with adaptive intervals
//   - Graceful degradation when Weaviate is unavailable
//   - OpenTelemetry tracing integration
package weaviate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrWeaviateUnavailable is returned when Weaviate is not reachable.
	ErrWeaviateUnavailable = errors.New("weaviate is not available")

	// ErrCircuitOpen is returned when the circuit breaker is open.
	ErrCircuitOpen = errors.New("circuit breaker is open, weaviate requests blocked")

	// ErrConnectionTimeout is returned when connection times out.
	ErrConnectionTimeout = errors.New("weaviate connection timeout")

	// ErrClientClosed is returned when operations are called on a closed client.
	ErrClientClosed = errors.New("weaviate client is closed")
)

// -----------------------------------------------------------------------------
// Connection State
// -----------------------------------------------------------------------------

// ConnectionState represents the current state of the Weaviate connection.
type ConnectionState int32

const (
	// StateConnected indicates normal operation.
	StateConnected ConnectionState = iota
	// StateDegraded indicates Weaviate is unavailable but client is functional.
	StateDegraded
	// StateCircuitOpen indicates circuit breaker is open, requests blocked.
	StateCircuitOpen
	// StateHalfOpen indicates circuit breaker is testing with single request.
	StateHalfOpen
)

// String returns the string representation of ConnectionState.
func (s ConnectionState) String() string {
	switch s {
	case StateConnected:
		return "connected"
	case StateDegraded:
		return "degraded"
	case StateCircuitOpen:
		return "circuit_open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// -----------------------------------------------------------------------------
// Client Configuration
// -----------------------------------------------------------------------------

// ClientConfig configures the resilient Weaviate client.
type ClientConfig struct {
	// URL is the Weaviate server URL (e.g., "http://localhost:8080").
	URL string

	// RetryAttempts is the number of retry attempts for failed requests.
	// Default: 3
	RetryAttempts int

	// RetryBackoff is the initial backoff duration between retries.
	// Default: 100ms
	RetryBackoff time.Duration

	// MaxRetryBackoff caps the exponential backoff.
	// Default: 5s
	MaxRetryBackoff time.Duration

	// RetryJitter adds randomness to backoff (0.0-1.0).
	// Default: 0.25 (±25%)
	RetryJitter float64

	// CircuitThreshold is the number of failures before opening circuit.
	// Default: 5
	CircuitThreshold int

	// CircuitWindow is the sliding window for counting failures.
	// Default: 30s
	CircuitWindow time.Duration

	// CircuitCooldown is how long circuit stays open before half-opening.
	// Default: 30s
	CircuitCooldown time.Duration

	// HealthCheckInterval is how often to check health when connected.
	// Default: 10s
	HealthCheckInterval time.Duration

	// DegradedCheckInterval is how often to check health when degraded.
	// Default: 5s
	DegradedCheckInterval time.Duration

	// HealthCheckTimeout prevents health checks from blocking.
	// Default: 5s
	HealthCheckTimeout time.Duration

	// AllowStartDegraded allows starting even if Weaviate is unavailable.
	// Default: false
	AllowStartDegraded bool

	// Logger for client operations.
	// Default: slog.Default()
	Logger *slog.Logger
}

// DefaultClientConfig returns sensible defaults for production use.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		RetryAttempts:         3,
		RetryBackoff:          100 * time.Millisecond,
		MaxRetryBackoff:       5 * time.Second,
		RetryJitter:           0.25,
		CircuitThreshold:      5,
		CircuitWindow:         30 * time.Second,
		CircuitCooldown:       30 * time.Second,
		HealthCheckInterval:   10 * time.Second,
		DegradedCheckInterval: 5 * time.Second,
		HealthCheckTimeout:    5 * time.Second,
		AllowStartDegraded:    false,
		Logger:                slog.Default(),
	}
}

// Validate checks if the configuration is valid.
func (c *ClientConfig) Validate() error {
	if c.URL == "" {
		return errors.New("url must not be empty")
	}
	if c.RetryAttempts < 0 {
		return errors.New("retry_attempts must be non-negative")
	}
	if c.RetryBackoff < 0 {
		return errors.New("retry_backoff must be non-negative")
	}
	if c.RetryJitter < 0 || c.RetryJitter > 1 {
		return errors.New("retry_jitter must be between 0 and 1")
	}
	if c.CircuitThreshold < 1 {
		return errors.New("circuit_threshold must be at least 1")
	}
	if c.CircuitWindow <= 0 {
		return errors.New("circuit_window must be positive")
	}
	if c.HealthCheckTimeout <= 0 {
		return errors.New("health_check_timeout must be positive")
	}
	return nil
}

// applyDefaults fills in zero values with defaults.
func (c *ClientConfig) applyDefaults() {
	defaults := DefaultClientConfig()
	if c.RetryAttempts == 0 {
		c.RetryAttempts = defaults.RetryAttempts
	}
	if c.RetryBackoff == 0 {
		c.RetryBackoff = defaults.RetryBackoff
	}
	if c.MaxRetryBackoff == 0 {
		c.MaxRetryBackoff = defaults.MaxRetryBackoff
	}
	if c.RetryJitter == 0 {
		c.RetryJitter = defaults.RetryJitter
	}
	if c.CircuitThreshold == 0 {
		c.CircuitThreshold = defaults.CircuitThreshold
	}
	if c.CircuitWindow == 0 {
		c.CircuitWindow = defaults.CircuitWindow
	}
	if c.CircuitCooldown == 0 {
		c.CircuitCooldown = defaults.CircuitCooldown
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = defaults.HealthCheckInterval
	}
	if c.DegradedCheckInterval == 0 {
		c.DegradedCheckInterval = defaults.DegradedCheckInterval
	}
	if c.HealthCheckTimeout == 0 {
		c.HealthCheckTimeout = defaults.HealthCheckTimeout
	}
	if c.Logger == nil {
		c.Logger = defaults.Logger
	}
}

// -----------------------------------------------------------------------------
// Resilient Client
// -----------------------------------------------------------------------------

// ResilientClient wraps the Weaviate client with resilience features.
//
// Thread Safety: Safe for concurrent use from multiple goroutines.
type ResilientClient struct {
	client *weaviate.Client
	config ClientConfig
	logger *slog.Logger

	// State
	state           atomic.Int32
	circuitOpenTime atomic.Int64 // Unix timestamp when circuit opened
	closed          atomic.Bool

	// Circuit breaker - sliding window
	failures   []time.Time // Ring buffer of failure timestamps
	failureIdx int
	failureMu  sync.Mutex

	// Half-open state - only one test request allowed
	halfOpenTest atomic.Bool

	// Lifecycle
	healthCtx    context.Context
	healthCancel context.CancelFunc
	healthWg     sync.WaitGroup

	// Degradation handlers
	handlers   []DegradationHandler
	handlersMu sync.RWMutex
}

// NewResilientClient creates a new resilient Weaviate client.
//
// Inputs:
//
//	config - Client configuration. URL is required.
//
// Outputs:
//
//	*ResilientClient - Ready-to-use client.
//	error - Non-nil if configuration invalid or connection fails (and AllowStartDegraded=false).
//
// Thread Safety: Safe for concurrent use.
func NewResilientClient(config ClientConfig) (*ResilientClient, error) {
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create underlying Weaviate client
	cfg := weaviate.Config{
		Host:   config.URL,
		Scheme: "http",
	}

	// Parse URL to extract scheme if present
	if len(config.URL) > 8 && config.URL[:8] == "https://" {
		cfg.Scheme = "https"
		cfg.Host = config.URL[8:]
	} else if len(config.URL) > 7 && config.URL[:7] == "http://" {
		cfg.Host = config.URL[7:]
	}

	client, err := weaviate.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create weaviate client: %w", err)
	}

	healthCtx, healthCancel := context.WithCancel(context.Background())

	rc := &ResilientClient{
		client:       client,
		config:       config,
		logger:       config.Logger.With(slog.String("component", "weaviate_client")),
		failures:     make([]time.Time, config.CircuitThreshold),
		healthCtx:    healthCtx,
		healthCancel: healthCancel,
	}
	rc.state.Store(int32(StateDegraded)) // Start degraded until proven healthy

	// Attempt initial connection
	if err := rc.checkHealth(context.Background()); err != nil {
		if config.AllowStartDegraded {
			rc.logger.Warn("Weaviate unavailable at startup, starting in degraded mode",
				slog.String("url", config.URL),
				slog.String("error", err.Error()))
			rc.healthWg.Add(1)
			go rc.runHealthChecker()
			return rc, nil
		}
		healthCancel()
		return nil, fmt.Errorf("weaviate not available: %w", err)
	}

	rc.transitionState(StateConnected)
	rc.healthWg.Add(1)
	go rc.runHealthChecker()

	rc.logger.Info("Weaviate client initialized",
		slog.String("url", config.URL),
		slog.String("state", rc.GetState().String()))

	return rc, nil
}

// Client returns the underlying Weaviate client.
// Use this for direct Weaviate operations.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) Client() *weaviate.Client {
	return c.client
}

// IsAvailable returns true if Weaviate is available for requests.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) IsAvailable() bool {
	state := ConnectionState(c.state.Load())
	return state == StateConnected || state == StateHalfOpen
}

// IsDegraded returns true if operating with reduced functionality.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) IsDegraded() bool {
	state := ConnectionState(c.state.Load())
	return state == StateDegraded || state == StateCircuitOpen
}

// GetState returns the current connection state.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) GetState() ConnectionState {
	return ConnectionState(c.state.Load())
}

// RegisterHandler registers a degradation handler.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) RegisterHandler(handler DegradationHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers = append(c.handlers, handler)

	// Notify of current state if degraded
	if c.IsDegraded() {
		handler.OnDegraded("initial state: weaviate unavailable")
	}
}

// Execute runs a function with retry and circuit breaker protection.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	fn - Function to execute. Should perform Weaviate operation.
//
// Outputs:
//
//	error - Non-nil if all retries fail or circuit is open.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) Execute(ctx context.Context, fn func() error) error {
	if c.closed.Load() {
		return ErrClientClosed
	}

	ctx, span := otel.Tracer("weaviate").Start(ctx, "weaviate.Execute",
		trace.WithAttributes(
			attribute.String("state", c.GetState().String()),
		),
	)
	defer span.End()

	// Check circuit breaker
	state := c.GetState()
	switch state {
	case StateCircuitOpen:
		// Check if cooldown expired
		if c.shouldTryHalfOpen() {
			c.transitionState(StateHalfOpen)
		} else {
			span.SetStatus(codes.Error, "circuit open")
			return ErrCircuitOpen
		}
	case StateHalfOpen:
		// Only one test request allowed in half-open
		if !c.halfOpenTest.CompareAndSwap(false, true) {
			span.SetStatus(codes.Error, "circuit open (half-open busy)")
			return ErrCircuitOpen
		}
		defer c.halfOpenTest.Store(false)
	}

	// Execute with retry
	var lastErr error
	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			backoff := c.calculateBackoff(attempt)
			span.AddEvent("retry", trace.WithAttributes(
				attribute.Int("attempt", attempt),
				attribute.Int64("backoff_ms", backoff.Milliseconds()),
			))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		lastErr = fn()
		if lastErr == nil {
			c.recordSuccess()
			span.SetStatus(codes.Ok, "success")
			return nil
		}

		if !isRetryable(lastErr) {
			break
		}
	}

	c.recordFailure()
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "all retries failed")
	return WrapWeaviateError(lastErr)
}

// WaitForReady blocks until Weaviate is ready or timeout.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	timeout - Maximum time to wait.
//
// Outputs:
//
//	error - Non-nil if timeout or context cancelled.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) WaitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("weaviate not ready within %v: %w", timeout, ErrWeaviateUnavailable)
		case <-ticker.C:
			if c.checkHealth(ctx) == nil {
				return nil
			}
		}
	}
}

// Close releases resources and stops the health checker.
//
// Thread Safety: Safe for concurrent use.
func (c *ResilientClient) Close() error {
	if c.closed.Swap(true) {
		return nil // Already closed
	}

	c.logger.Info("closing weaviate client")
	c.healthCancel()
	c.healthWg.Wait()
	return nil
}

// -----------------------------------------------------------------------------
// Internal Methods
// -----------------------------------------------------------------------------

// transitionState changes state and notifies handlers.
func (c *ResilientClient) transitionState(newState ConnectionState) {
	oldState := ConnectionState(c.state.Swap(int32(newState)))
	if oldState == newState {
		return
	}

	c.logger.Info("weaviate state transition",
		slog.String("from", oldState.String()),
		slog.String("to", newState.String()))

	// Notify handlers
	c.handlersMu.RLock()
	handlers := c.handlers
	c.handlersMu.RUnlock()

	wasDegraded := oldState == StateDegraded || oldState == StateCircuitOpen
	isDegraded := newState == StateDegraded || newState == StateCircuitOpen

	if !wasDegraded && isDegraded {
		for _, h := range handlers {
			h.OnDegraded(fmt.Sprintf("state changed to %s", newState.String()))
		}
	} else if wasDegraded && !isDegraded {
		for _, h := range handlers {
			h.OnRecovered()
		}
	}
}

// checkHealth performs a health check with timeout.
func (c *ResilientClient) checkHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.HealthCheckTimeout)
	defer cancel()

	_, span := otel.Tracer("weaviate").Start(ctx, "weaviate.health_check")
	defer span.End()

	isReady, err := c.client.Misc().ReadyChecker().Do(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "health check failed")
		return fmt.Errorf("health check failed: %w", err)
	}

	if !isReady {
		span.SetStatus(codes.Error, "not ready")
		return ErrWeaviateUnavailable
	}

	span.SetStatus(codes.Ok, "healthy")
	return nil
}

// runHealthChecker runs periodic health checks.
func (c *ResilientClient) runHealthChecker() {
	defer c.healthWg.Done()

	for {
		interval := c.config.HealthCheckInterval
		if c.IsDegraded() {
			interval = c.config.DegradedCheckInterval
		}

		select {
		case <-c.healthCtx.Done():
			return
		case <-time.After(interval):
			c.performHealthCheck()
		}
	}
}

// performHealthCheck runs a single health check and updates state.
func (c *ResilientClient) performHealthCheck() {
	err := c.checkHealth(c.healthCtx)
	currentState := c.GetState()

	if err == nil {
		// Healthy
		switch currentState {
		case StateDegraded, StateHalfOpen:
			c.transitionState(StateConnected)
			c.resetFailures()
		case StateCircuitOpen:
			// Don't transition directly from open to connected
			// Let half-open test succeed first
			if c.shouldTryHalfOpen() {
				c.transitionState(StateHalfOpen)
			}
		}
	} else {
		// Unhealthy
		if currentState == StateConnected {
			c.transitionState(StateDegraded)
		}
	}
}

// recordSuccess records a successful request.
func (c *ResilientClient) recordSuccess() {
	state := c.GetState()
	if state == StateHalfOpen {
		c.transitionState(StateConnected)
		c.resetFailures()
	}
}

// recordFailure records a failed request.
func (c *ResilientClient) recordFailure() {
	c.failureMu.Lock()
	defer c.failureMu.Unlock()

	now := time.Now()
	c.failures[c.failureIdx] = now
	c.failureIdx = (c.failureIdx + 1) % len(c.failures)

	// Count failures within window
	windowStart := now.Add(-c.config.CircuitWindow)
	count := 0
	for _, t := range c.failures {
		if !t.IsZero() && t.After(windowStart) {
			count++
		}
	}

	if count >= c.config.CircuitThreshold {
		if c.GetState() != StateCircuitOpen {
			c.circuitOpenTime.Store(now.Unix())
			c.transitionState(StateCircuitOpen)
			c.logger.Warn("circuit breaker opened",
				slog.Int("failures", count),
				slog.Duration("window", c.config.CircuitWindow))
		}
	} else if c.GetState() == StateConnected {
		c.transitionState(StateDegraded)
	}
}

// resetFailures clears the failure buffer.
func (c *ResilientClient) resetFailures() {
	c.failureMu.Lock()
	defer c.failureMu.Unlock()
	for i := range c.failures {
		c.failures[i] = time.Time{}
	}
	c.failureIdx = 0
}

// shouldTryHalfOpen checks if cooldown expired.
func (c *ResilientClient) shouldTryHalfOpen() bool {
	openTime := time.Unix(c.circuitOpenTime.Load(), 0)
	return time.Since(openTime) >= c.config.CircuitCooldown
}

// calculateBackoff returns backoff with jitter.
func (c *ResilientClient) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: base * 2^attempt
	backoff := c.config.RetryBackoff * time.Duration(1<<attempt)

	// Cap at max
	if backoff > c.config.MaxRetryBackoff {
		backoff = c.config.MaxRetryBackoff
	}

	// Add jitter: ±jitter%
	jitterRange := float64(backoff) * c.config.RetryJitter
	jitter := (rand.Float64()*2 - 1) * jitterRange // Random -jitter to +jitter
	backoff = time.Duration(float64(backoff) + jitter)

	if backoff < 0 {
		backoff = c.config.RetryBackoff
	}

	return backoff
}

// isRetryable determines if an error is retryable.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context cancelled - not retryable
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Timeout - retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Connection errors (OpError) - retryable (server might be starting/restarting)
	// Check this first since net.OpError implements net.Error
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Other network errors - retryable if timeout or temporary
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	// Default: not retryable (likely application error)
	return false
}

// WrapWeaviateError wraps errors with more context.
func WrapWeaviateError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrConnectionTimeout, err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", ErrConnectionTimeout, err)
	}

	return fmt.Errorf("weaviate error: %w", err)
}
