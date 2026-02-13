// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"time"
)

// ==============================================================================
// Schema Version
// ==============================================================================

const (
	// HLDSchemaVersion is the current HLD schema version (A-M1).
	// Increment when making breaking changes to serialization format.
	HLDSchemaVersion = 1
)

// ==============================================================================
// Cache Interface (DB-H1, DB-H2, DB-H3, DB-M1, DB-M2)
// ==============================================================================

// HLDCache provides caching for HLD query results.
//
// Description:
//
//	Optional interface for caching expensive query results across invocations.
//	Implementations can use in-memory caches (LRU, TTL) or persistent stores
//	(BadgerDB, Redis) depending on requirements.
//
// Thread Safety: All methods must be safe for concurrent use.
type HLDCache interface {
	// GetLCA returns cached LCA result, or (empty, false) if not found.
	GetLCA(u, v string) (lca string, found bool)

	// SetLCA stores LCA result with optional TTL.
	SetLCA(u, v, lca string, ttl time.Duration)

	// GetDistance returns cached distance, or (0, false) if not found.
	GetDistance(u, v string) (distance int, found bool)

	// SetDistance stores distance result with optional TTL.
	SetDistance(u, v string, distance int, ttl time.Duration)

	// GetPath returns cached path segments, or (nil, false) if not found.
	GetPath(u, v string) (segments []PathSegment, found bool)

	// SetPath stores path segments with optional TTL.
	SetPath(u, v string, segments []PathSegment, ttl time.Duration)

	// Invalidate removes all cached entries (called when graph changes).
	Invalidate()

	// Stats returns cache statistics.
	Stats() CacheStats
}

// CacheStats contains cache performance metrics.
type CacheStats struct {
	Hits      int64 // Total cache hits
	Misses    int64 // Total cache misses
	Evictions int64 // Total evictions
	Size      int   // Current number of cached entries
	MemoryKB  int   // Approximate memory usage in KB
}

// NoOpCache is a cache that does nothing (always misses).
// Used as default when no cache is configured.
type NoOpCache struct{}

func (NoOpCache) GetLCA(u, v string) (string, bool)                              { return "", false }
func (NoOpCache) SetLCA(u, v, lca string, ttl time.Duration)                     {}
func (NoOpCache) GetDistance(u, v string) (int, bool)                            { return 0, false }
func (NoOpCache) SetDistance(u, v string, distance int, ttl time.Duration)       {}
func (NoOpCache) GetPath(u, v string) ([]PathSegment, bool)                      { return nil, false }
func (NoOpCache) SetPath(u, v string, segments []PathSegment, ttl time.Duration) {}
func (NoOpCache) Invalidate()                                                    {}
func (NoOpCache) Stats() CacheStats                                              { return CacheStats{} }

// ==============================================================================
// Rate Limiter Interface (A-M4)
// ==============================================================================

// RateLimiter controls query rate to prevent DoS.
//
// Description:
//
//	Implements rate limiting using token bucket, leaky bucket, or
//	similar algorithms. Protects against query flooding.
//
// Thread Safety: All methods must be safe for concurrent use.
type RateLimiter interface {
	// Allow returns true if the query is allowed, false if rate limited.
	Allow() bool

	// Wait blocks until a query is allowed or context is canceled.
	Wait(ctx context.Context) error

	// Stats returns rate limiter statistics.
	Stats() RateLimiterStats
}

// RateLimiterStats contains rate limiting metrics.
type RateLimiterStats struct {
	Allowed  int64   // Total requests allowed
	Rejected int64   // Total requests rejected
	QPS      float64 // Current queries per second
}

// NoOpRateLimiter always allows all queries.
// Used as default when no rate limiting is configured.
type NoOpRateLimiter struct{}

func (NoOpRateLimiter) Allow() bool                    { return true }
func (NoOpRateLimiter) Wait(ctx context.Context) error { return nil }
func (NoOpRateLimiter) Stats() RateLimiterStats        { return RateLimiterStats{} }

// ==============================================================================
// Circuit Breaker Interface (A-M5)
// ==============================================================================

// CircuitBreaker prevents cascading failures.
//
// Description:
//
//	Implements circuit breaker pattern with three states:
//	- Closed: Normal operation, all requests pass through
//	- Open: Failing fast, no requests allowed
//	- Half-Open: Testing if service recovered
//
// Thread Safety: All methods must be safe for concurrent use.
type CircuitBreaker interface {
	// Execute runs the function if circuit is closed, returns error if open.
	Execute(fn func() error) error

	// State returns current circuit state.
	State() CircuitState

	// Stats returns circuit breaker statistics.
	Stats() CircuitBreakerStats
}

// CircuitState represents circuit breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = 0 // Normal operation
	CircuitOpen     CircuitState = 1 // Failing fast
	CircuitHalfOpen CircuitState = 2 // Testing recovery
)

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

// CircuitBreakerStats contains circuit breaker metrics.
type CircuitBreakerStats struct {
	State               CircuitState
	TotalRequests       int64
	SuccessfulRequests  int64
	FailedRequests      int64
	RejectedRequests    int64 // Rejected due to open circuit
	ConsecutiveFailures int64
	LastStateChange     int64 // Unix milliseconds UTC
}

// NoOpCircuitBreaker never opens the circuit.
// Used as default when no circuit breaker is configured.
type NoOpCircuitBreaker struct{}

func (NoOpCircuitBreaker) Execute(fn func() error) error { return fn() }
func (NoOpCircuitBreaker) State() CircuitState           { return CircuitClosed }
func (NoOpCircuitBreaker) Stats() CircuitBreakerStats {
	return CircuitBreakerStats{State: CircuitClosed}
}
