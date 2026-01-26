// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package sampling

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Adaptive Sampler Interface
// =============================================================================

// AdaptiveSampler defines the interface for load-adaptive sampling.
//
// # Description
//
// AdaptiveSampler prevents the "Observer Effect" where 100% logging/tracing
// causes performance degradation. It dynamically adjusts sampling rate based
// on system load.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example
//
//	var sampler AdaptiveSampler = NewAdaptiveSampler(DefaultSamplingConfig())
//	if sampler.ShouldSample() {
//	    trace.Start()
//	    defer trace.End()
//	}
//
// # Limitations
//
//   - Sampling is probabilistic; important events may be missed
//   - Rate adjustment has some latency
//
// # Assumptions
//
//   - Caller records latencies for adaptive behavior
//   - Missing some samples is acceptable
type AdaptiveSampler interface {
	// ShouldSample returns true if this item should be sampled.
	ShouldSample() bool

	// ShouldSampleContext returns true and records the decision in context.
	ShouldSampleContext(ctx context.Context) (context.Context, bool)

	// RecordLatency records a latency measurement for adaptive adjustment.
	RecordLatency(latency time.Duration)

	// GetSamplingRate returns the current sampling rate (0.0-1.0).
	GetSamplingRate() float64

	// SetBaseSamplingRate sets the base sampling rate.
	SetBaseSamplingRate(rate float64)

	// Stats returns current sampler statistics.
	Stats() SamplerStats

	// ForceEnable temporarily enables 100% sampling (for debugging).
	ForceEnable(duration time.Duration)
}

// =============================================================================
// Sampler Statistics
// =============================================================================

// SamplerStats contains sampling statistics.
//
// # Description
//
// Provides visibility into sampling behavior for monitoring.
type SamplerStats struct {
	// TotalSampled is the count of items that were sampled.
	TotalSampled int64

	// TotalDropped is the count of items that were not sampled.
	TotalDropped int64

	// CurrentRate is the current sampling rate.
	CurrentRate float64

	// AverageLatency is the rolling average latency.
	AverageLatency time.Duration

	// IsThrottled indicates if sampling is reduced due to load.
	IsThrottled bool

	// ThrottleReason explains why sampling is reduced.
	ThrottleReason string

	// ForceEnabled indicates if force-enable is active.
	ForceEnabled bool
}

// =============================================================================
// Sampling Configuration
// =============================================================================

// Config configures the adaptive sampler.
//
// # Description
//
// Defines the parameters for adaptive sampling behavior.
//
// # Example
//
//	config := Config{
//	    BaseSamplingRate: 0.1,      // 10% base rate
//	    MinSamplingRate:  0.01,     // Never go below 1%
//	    MaxSamplingRate:  1.0,      // Can go up to 100%
//	    LatencyThreshold: 100 * time.Millisecond,
//	}
//
// # Limitations
//
//   - All rate values must be in range [0.0, 1.0]
//
// # Assumptions
//
//   - MinSamplingRate <= BaseSamplingRate <= MaxSamplingRate
type Config struct {
	// BaseSamplingRate is the default sampling rate (0.0-1.0).
	// Default: 0.1 (10%)
	BaseSamplingRate float64

	// MinSamplingRate is the minimum rate even under high load.
	// Default: 0.01 (1%)
	MinSamplingRate float64

	// MaxSamplingRate is the maximum rate under low load.
	// Default: 1.0 (100%)
	MaxSamplingRate float64

	// LatencyThreshold triggers throttling when exceeded.
	// Default: 100ms
	LatencyThreshold time.Duration

	// LatencyWindow is the window for averaging latency.
	// Default: 1 minute
	LatencyWindow time.Duration

	// AdjustmentInterval is how often to recalculate rate.
	// Default: 10 seconds
	AdjustmentInterval time.Duration

	// LatencyBufferSize is the number of latency samples to store in the ring buffer.
	// A larger buffer provides more stable averages but uses more memory.
	// Default: 1024
	LatencyBufferSize int
}

// =============================================================================
// Constructor Functions
// =============================================================================

// DefaultConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with 10% base sampling rate that adapts
// based on system latency.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - Config: Default configuration
//
// # Example
//
//	config := DefaultConfig()
//	config.BaseSamplingRate = 0.2  // Override to 20%
//	sampler := NewAdaptiveSampler(config)
//
// # Limitations
//
//   - Defaults may not be optimal for all workloads
//
// # Assumptions
//
//   - Caller will tune for their specific use case
func DefaultConfig() Config {
	return Config{
		BaseSamplingRate:   0.1,
		MinSamplingRate:    0.01,
		MaxSamplingRate:    1.0,
		LatencyThreshold:   100 * time.Millisecond,
		LatencyWindow:      time.Minute,
		AdjustmentInterval: 10 * time.Second,
		LatencyBufferSize:  1024,
	}
}

// =============================================================================
// Default Adaptive Sampler Struct
// =============================================================================

// DefaultAdaptiveSampler implements AdaptiveSampler.
//
// # Description
//
// Dynamically adjusts sampling rate based on system latency to prevent
// observability overhead from causing performance issues. When latency
// increases, sampling rate decreases automatically.
//
// # Use Cases
//
//   - Trace sampling during high load
//   - Log sampling for high-volume paths
//   - Metric collection rate adjustment
//
// # Thread Safety
//
// DefaultAdaptiveSampler is safe for concurrent use.
//
// # Algorithm
//
// The sampler uses a simple proportional adjustment:
//   - If avg latency > threshold: rate = rate * (threshold / latency)
//   - If avg latency < threshold/2: rate = min(rate * 1.1, max)
//
// # Example
//
//	sampler := NewAdaptiveSampler(DefaultConfig())
//	defer sampler.Stop()
//
//	// In request handler:
//	if sampler.ShouldSample() {
//	    trace.Start()
//	    defer trace.End()
//	}
//
//	// Record latency after request:
//	sampler.RecordLatency(time.Since(start))
//
// # Limitations
//
//   - Uses simple random sampling (not reservoir)
//   - Latency window uses ring buffer (fixed size)
//
// # Assumptions
//
//   - Caller calls Stop() on shutdown
//   - Latency recordings represent actual system load
type DefaultAdaptiveSampler struct {
	config   Config
	configMu sync.RWMutex // Protects config field access

	// Current state
	currentRate    atomic.Value // float64
	totalSampled   atomic.Int64
	totalDropped   atomic.Int64
	isThrottled    atomic.Bool
	throttleReason atomic.Value // string

	// Latency tracking
	latencies    []time.Duration
	latencyIndex int
	latencyMu    sync.Mutex

	// Force enable - protected by forceMu to ensure atomic updates of both fields.
	// Using a mutex instead of separate atomics prevents a TOCTOU race where
	// a new ForceEnable call could be immediately disabled by a concurrent
	// expiry check that read the old (expired) forceUntil value.
	forceMu      sync.RWMutex
	forceEnabled bool
	forceUntil   time.Time

	// Adjustment goroutine
	stopCh   chan struct{}
	stopOnce sync.Once // Ensures Stop() is idempotent
	wg       sync.WaitGroup
}

// Compile-time interface check
var _ AdaptiveSampler = (*DefaultAdaptiveSampler)(nil)

// NewAdaptiveSampler creates a new adaptive sampler.
//
// # Description
//
// Creates a sampler that automatically adjusts rate based on latency.
// Starts a background goroutine for periodic adjustment. Caller must
// call Stop() to clean up the background goroutine.
//
// # Inputs
//
//   - config: Configuration for sampling behavior
//
// # Outputs
//
//   - *DefaultAdaptiveSampler: New sampler
//
// # Example
//
//	sampler := NewAdaptiveSampler(Config{
//	    BaseSamplingRate: 0.2,
//	    LatencyThreshold: 50 * time.Millisecond,
//	})
//	defer sampler.Stop()
//
// # Limitations
//
//   - Starts a background goroutine that must be stopped
//
// # Assumptions
//
//   - Caller will call Stop() on shutdown
func NewAdaptiveSampler(config Config) *DefaultAdaptiveSampler {
	// Apply defaults and validate bounds [0.0, 1.0]
	if config.BaseSamplingRate <= 0 || config.BaseSamplingRate > 1.0 {
		config.BaseSamplingRate = 0.1
	}
	if config.MinSamplingRate <= 0 || config.MinSamplingRate > 1.0 {
		config.MinSamplingRate = 0.01
	}
	if config.MaxSamplingRate <= 0 || config.MaxSamplingRate > 1.0 {
		config.MaxSamplingRate = 1.0
	}
	if config.LatencyThreshold <= 0 {
		config.LatencyThreshold = 100 * time.Millisecond
	}
	if config.LatencyWindow <= 0 {
		config.LatencyWindow = time.Minute
	}
	if config.AdjustmentInterval <= 0 {
		config.AdjustmentInterval = 10 * time.Second
	}

	// Ensure logical consistency: MinSamplingRate <= BaseSamplingRate <= MaxSamplingRate
	// This prevents configurations where the base rate is unreachable.
	if config.BaseSamplingRate < config.MinSamplingRate {
		config.BaseSamplingRate = config.MinSamplingRate
	}
	if config.BaseSamplingRate > config.MaxSamplingRate {
		config.BaseSamplingRate = config.MaxSamplingRate
	}

	// Validate latency buffer size
	if config.LatencyBufferSize <= 0 {
		config.LatencyBufferSize = 1024
	}
	bufferSize := config.LatencyBufferSize

	s := &DefaultAdaptiveSampler{
		config:    config,
		latencies: make([]time.Duration, bufferSize),
		stopCh:    make(chan struct{}),
	}

	s.currentRate.Store(config.BaseSamplingRate)
	s.throttleReason.Store("")
	// forceEnabled and forceUntil are zero-valued (false and time.Time{})

	// Start adjustment goroutine
	s.wg.Add(1)
	go s.adjustLoop()

	return s
}

// =============================================================================
// DefaultAdaptiveSampler Methods
// =============================================================================

// ShouldSample returns true if this item should be sampled.
//
// # Description
//
// Uses the current sampling rate to make a probabilistic decision.
// Thread-safe and fast (lock-free for the common path).
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - bool: True if item should be sampled
//
// # Example
//
//	if sampler.ShouldSample() {
//	    // Record trace, log, etc.
//	}
//
// # Limitations
//
//   - Decision is probabilistic; results vary between calls
//
// # Assumptions
//
//   - Receiver is not nil
func (s *DefaultAdaptiveSampler) ShouldSample() bool {
	// Check force-enable state under read lock
	s.forceMu.RLock()
	enabled := s.forceEnabled
	until := s.forceUntil
	s.forceMu.RUnlock()

	if enabled {
		if time.Now().Before(until) {
			s.totalSampled.Add(1)
			return true
		}

		// Time expired, try to disable with write lock.
		// Must re-check condition after acquiring lock in case another
		// goroutine just called ForceEnable with a new duration.
		s.forceMu.Lock()
		if s.forceEnabled && time.Now().After(s.forceUntil) {
			s.forceEnabled = false
		}
		s.forceMu.Unlock()
	}

	rate := s.currentRate.Load().(float64)

	// Fast path for 0 or 100%
	if rate <= 0 {
		s.totalDropped.Add(1)
		return false
	}
	if rate >= 1 {
		s.totalSampled.Add(1)
		return true
	}

	// Probabilistic sampling using math/rand/v2 (concurrent-safe)
	if rand.Float64() < rate {
		s.totalSampled.Add(1)
		return true
	}

	s.totalDropped.Add(1)
	return false
}

// ShouldSampleContext returns sampling decision and annotated context.
//
// # Description
//
// Same as ShouldSample but also stores the decision in context for
// consistent sampling of related operations. If a decision was already
// made (stored in context), it returns that decision without double-counting.
//
// # Inputs
//
//   - ctx: Request context
//
// # Outputs
//
//   - context.Context: Context with sampling decision
//   - bool: True if item should be sampled
//
// # Example
//
//	ctx, sampled := sampler.ShouldSampleContext(ctx)
//	if sampled {
//	    // All operations with this ctx will consistently sample
//	}
//
// # Limitations
//
//   - Context must be propagated for consistent sampling
//
// # Assumptions
//
//   - ctx is not nil
func (s *DefaultAdaptiveSampler) ShouldSampleContext(ctx context.Context) (context.Context, bool) {
	// Check if already decided - DO NOT increment counters here,
	// as they were already incremented on the initial call.
	if sampled, ok := ctx.Value(samplingContextKey{}).(bool); ok {
		return ctx, sampled
	}

	// First decision - ShouldSample handles counter increments
	sampled := s.ShouldSample()
	return context.WithValue(ctx, samplingContextKey{}, sampled), sampled
}

// samplingContextKey is used to store sampling decision in context.
type samplingContextKey struct{}

// RecordLatency records a latency measurement.
//
// # Description
//
// Records latency for use in adaptive rate adjustment. Uses a ring
// buffer to maintain a rolling window of measurements.
//
// # Performance Note
//
// The latencyMu mutex serializes all latency recordings. On systems with
// very high request rates and many cores, this could become a contention
// point. If profiling reveals this as a bottleneck, consider using a
// buffered channel with a single consumer goroutine, or sharded mutexes.
// For most use cases (< 100k req/s), the current implementation is adequate.
//
// # Inputs
//
//   - latency: The measured latency
//
// # Outputs
//
//   - None
//
// # Example
//
//	start := time.Now()
//	doWork()
//	sampler.RecordLatency(time.Since(start))
//
// # Limitations
//
//   - High-frequency recording may cause mutex contention
//
// # Assumptions
//
//   - Latency values are representative of system load
func (s *DefaultAdaptiveSampler) RecordLatency(latency time.Duration) {
	s.latencyMu.Lock()
	s.latencies[s.latencyIndex] = latency
	s.latencyIndex = (s.latencyIndex + 1) % len(s.latencies)
	s.latencyMu.Unlock()
}

// GetSamplingRate returns the current sampling rate.
//
// # Description
//
// Returns the current effective sampling rate (0.0-1.0).
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - float64: Current sampling rate
//
// # Example
//
//	rate := sampler.GetSamplingRate()
//	log.Printf("Current sampling rate: %.1f%%", rate*100)
//
// # Limitations
//
//   - Value may change immediately after return
//
// # Assumptions
//
//   - Receiver is not nil
func (s *DefaultAdaptiveSampler) GetSamplingRate() float64 {
	return s.currentRate.Load().(float64)
}

// SetBaseSamplingRate sets the base sampling rate.
//
// # Description
//
// Updates the base rate. The actual rate may still be adjusted
// based on load. Thread-safe: uses mutex to protect config access.
//
// Note: This does NOT immediately update currentRate. The background
// adjustLoop is the single source of truth for currentRate and will
// pick up the new BaseSamplingRate on its next tick. This prevents
// a race condition where adjustRate could overwrite the user's setting
// with a value calculated from stale data.
//
// # Inputs
//
//   - rate: New base rate (0.0-1.0)
//
// # Outputs
//
//   - None
//
// # Example
//
//	sampler.SetBaseSamplingRate(0.5)  // Set to 50%
//
// # Limitations
//
//   - Rate change takes effect on next adjustment cycle
//
// # Assumptions
//
//   - Rate is in valid range [0.0, 1.0]
func (s *DefaultAdaptiveSampler) SetBaseSamplingRate(rate float64) {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	if rate < s.config.MinSamplingRate {
		rate = s.config.MinSamplingRate
	}
	if rate > s.config.MaxSamplingRate {
		rate = s.config.MaxSamplingRate
	}
	s.config.BaseSamplingRate = rate
	// Note: Do NOT call s.currentRate.Store(rate) here.
	// The adjustLoop goroutine is the single source of truth for currentRate.
}

// Stats returns current sampler statistics.
//
// # Description
//
// Returns a snapshot of current sampling statistics.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - SamplerStats: Current statistics
//
// # Example
//
//	stats := sampler.Stats()
//	log.Printf("Sampled: %d, Dropped: %d, Rate: %.1f%%",
//	    stats.TotalSampled, stats.TotalDropped, stats.CurrentRate*100)
//
// # Limitations
//
//   - Values are point-in-time snapshot
//
// # Assumptions
//
//   - Receiver is not nil
func (s *DefaultAdaptiveSampler) Stats() SamplerStats {
	s.forceMu.RLock()
	forceEnabled := s.forceEnabled
	s.forceMu.RUnlock()

	return SamplerStats{
		TotalSampled:   s.totalSampled.Load(),
		TotalDropped:   s.totalDropped.Load(),
		CurrentRate:    s.currentRate.Load().(float64),
		AverageLatency: s.calculateAverageLatency(),
		IsThrottled:    s.isThrottled.Load(),
		ThrottleReason: s.throttleReason.Load().(string),
		ForceEnabled:   forceEnabled,
	}
}

// ForceEnable temporarily enables 100% sampling.
//
// # Description
//
// Forces 100% sampling for a specified duration. Useful for
// debugging specific issues. Thread-safe: uses mutex to ensure
// both forceEnabled and forceUntil are updated atomically.
//
// # Inputs
//
//   - duration: How long to force 100% sampling
//
// # Outputs
//
//   - None
//
// # Example
//
//	sampler.ForceEnable(5 * time.Minute)  // 100% sampling for 5 minutes
//
// # Limitations
//
//   - May cause performance degradation if duration is long
//
// # Assumptions
//
//   - Duration is reasonable for the system load
func (s *DefaultAdaptiveSampler) ForceEnable(duration time.Duration) {
	s.forceMu.Lock()
	defer s.forceMu.Unlock()
	s.forceUntil = time.Now().Add(duration)
	s.forceEnabled = true
}

// Stop stops the sampler's background goroutine.
//
// # Description
//
// Stops the adjustment loop. Should be called on shutdown.
// Idempotent: safe to call multiple times without panic.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - None
//
// # Example
//
//	sampler := NewAdaptiveSampler(config)
//	defer sampler.Stop()
//
// # Limitations
//
//   - Sampler should not be used after Stop()
//
// # Assumptions
//
//   - Receiver is not nil
func (s *DefaultAdaptiveSampler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// adjustLoop periodically adjusts the sampling rate.
func (s *DefaultAdaptiveSampler) adjustLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.AdjustmentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.adjustRate()
		}
	}
}

// adjustRate calculates and applies a new sampling rate.
// Thread-safe: reads config under RLock to prevent data races with SetBaseSamplingRate.
func (s *DefaultAdaptiveSampler) adjustRate() {
	avgLatency := s.calculateAverageLatency()

	// No data yet
	if avgLatency == 0 {
		return
	}

	// Read config values under lock to prevent data race
	s.configMu.RLock()
	threshold := s.config.LatencyThreshold
	baseRate := s.config.BaseSamplingRate
	minRate := s.config.MinSamplingRate
	maxRate := s.config.MaxSamplingRate
	s.configMu.RUnlock()

	// Determine throttled state based on current latency (independent of rate).
	// This ensures isThrottled accurately reflects whether latency exceeds threshold,
	// preventing the flag from getting stuck when latency is in the middle range.
	if avgLatency > threshold {
		s.isThrottled.Store(true)
		s.throttleReason.Store("latency exceeded threshold")
	} else {
		s.isThrottled.Store(false)
		s.throttleReason.Store("")
	}

	currentRate := s.currentRate.Load().(float64)
	newRate := currentRate

	if avgLatency > threshold {
		// High latency - reduce sampling
		ratio := float64(threshold) / float64(avgLatency)
		newRate = currentRate * ratio
	} else if avgLatency < threshold/2 {
		// Low latency - increase sampling towards base rate.
		// Uses proportional recovery (25% of remaining distance) instead of
		// simple multiplier for faster adaptation to spiky workloads.
		// Example: If currentRate=0.01 and baseRate=0.1, this recovers much
		// faster than currentRate * 1.1 would.
		newRate = currentRate + (baseRate-currentRate)*0.25
	}
	// Note: When threshold/2 <= avgLatency <= threshold, rate stays unchanged.

	// Apply bounds
	if newRate < minRate {
		newRate = minRate
	}
	if newRate > maxRate {
		newRate = maxRate
	}

	s.currentRate.Store(newRate)
}

// calculateAverageLatency computes the average of recorded latencies.
func (s *DefaultAdaptiveSampler) calculateAverageLatency() time.Duration {
	s.latencyMu.Lock()
	defer s.latencyMu.Unlock()

	var total time.Duration
	var count int

	for _, lat := range s.latencies {
		if lat > 0 {
			total += lat
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / time.Duration(count)
}

// =============================================================================
// Helper Functions
// =============================================================================

// AlwaysSampleError is a helper that always samples errors.
//
// # Description
//
// Wraps ShouldSample to always return true for errors.
// Useful when you want to ensure all errors are captured.
//
// # Inputs
//
//   - sampler: The sampler to use
//   - isError: Whether this is an error case
//
// # Outputs
//
//   - bool: True if should sample (always true for errors)
//
// # Example
//
//	if AlwaysSampleError(sampler, err != nil) {
//	    recordTrace()
//	}
//
// # Limitations
//
//   - May increase sampling volume if errors are frequent
//
// # Assumptions
//
//   - sampler is not nil
func AlwaysSampleError(sampler AdaptiveSampler, isError bool) bool {
	if isError {
		return true
	}
	return sampler.ShouldSample()
}

// HeadSampler creates a sampler that only samples the first N items.
//
// # Description
//
// Useful for sampling the start of a batch operation.
// Thread-safe via atomic counter.
//
// # Inputs
//
//   - n: Number of items to sample (must be > 0)
//
// # Outputs
//
//   - func(): Function that returns true for first N calls
//
// # Example
//
//	sampler := HeadSampler(10)
//	for _, item := range items {
//	    if sampler() {
//	        log.Printf("Processing item: %v", item)
//	    }
//	}
//
// # Limitations
//
//   - Once N items are sampled, never samples again
//
// # Assumptions
//
//   - N is positive; if N <= 0, returns a sampler that never samples
func HeadSampler(n int) func() bool {
	if n <= 0 {
		// Return a sampler that never samples for non-positive N
		return func() bool { return false }
	}
	var count int32
	return func() bool {
		current := atomic.AddInt32(&count, 1)
		return int(current) <= n
	}
}

// RateLimitedSampler creates a sampler with per-second rate limiting.
//
// # Description
//
// Samples at most N items per second, regardless of base rate.
// Returns a function for consistency with HeadSampler API.
//
// # Inputs
//
//   - perSecond: Maximum samples per second (must be > 0)
//
// # Outputs
//
//   - func() bool: Function that returns true if under rate limit
//
// # Example
//
//	sampler := RateLimitedSampler(100)  // Max 100 per second
//	for event := range events {
//	    if sampler() {
//	        processEvent(event)
//	    }
//	}
//
// # Limitations
//
//   - Window resets every second (not sliding window)
//
// # Assumptions
//
//   - perSecond is positive; if <= 0, returns a sampler that never samples
func RateLimitedSampler(perSecond int) func() bool {
	if perSecond <= 0 {
		// Return a sampler that never samples for non-positive rates
		return func() bool { return false }
	}
	impl := &rateLimitedSamplerImpl{
		maxPerSecond: perSecond,
		windowStart:  time.Now(),
	}
	return impl.shouldSample
}

// rateLimitedSamplerImpl implements rate-limited sampling (internal).
type rateLimitedSamplerImpl struct {
	maxPerSecond int
	windowStart  time.Time
	count        int
	mu           sync.Mutex
}

// shouldSample returns true if under rate limit.
func (r *rateLimitedSamplerImpl) shouldSample() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.windowStart) >= time.Second {
		// New window
		r.windowStart = now
		r.count = 0
	}

	if r.count >= r.maxPerSecond {
		return false
	}

	r.count++
	return true
}
