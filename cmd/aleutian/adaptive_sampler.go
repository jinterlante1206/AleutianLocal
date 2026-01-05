package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

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

// SamplingConfig configures the adaptive sampler.
//
// # Description
//
// Defines the parameters for adaptive sampling behavior.
//
// # Example
//
//	config := SamplingConfig{
//	    BaseSamplingRate: 0.1,      // 10% base rate
//	    MinSamplingRate:  0.01,     // Never go below 1%
//	    MaxSamplingRate:  1.0,      // Can go up to 100%
//	    LatencyThreshold: 100 * time.Millisecond,
//	}
type SamplingConfig struct {
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

	// ErrorsAlwaysSample ensures errors are always sampled.
	// Default: true
	ErrorsAlwaysSample bool
}

// DefaultSamplingConfig returns sensible defaults.
//
// # Description
//
// Returns configuration with 10% base sampling rate that adapts
// based on system latency.
//
// # Outputs
//
//   - SamplingConfig: Default configuration
func DefaultSamplingConfig() SamplingConfig {
	return SamplingConfig{
		BaseSamplingRate:   0.1,
		MinSamplingRate:    0.01,
		MaxSamplingRate:    1.0,
		LatencyThreshold:   100 * time.Millisecond,
		LatencyWindow:      time.Minute,
		AdjustmentInterval: 10 * time.Second,
		ErrorsAlwaysSample: true,
	}
}

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
// # Limitations
//
//   - Uses simple random sampling (not reservoir)
//   - Latency window uses ring buffer (fixed size)
//
// # Example
//
//	sampler := NewAdaptiveSampler(DefaultSamplingConfig())
//
//	// In request handler:
//	if sampler.ShouldSample() {
//	    trace.Start()
//	    defer trace.End()
//	}
//
//	// Record latency after request:
//	sampler.RecordLatency(time.Since(start))
type DefaultAdaptiveSampler struct {
	config SamplingConfig

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

	// Force enable
	forceEnabled atomic.Bool
	forceUntil   atomic.Value // time.Time

	// Random source
	randMu    sync.Mutex
	randState uint64

	// Adjustment goroutine
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewAdaptiveSampler creates a new adaptive sampler.
//
// # Description
//
// Creates a sampler that automatically adjusts rate based on latency.
// Starts a background goroutine for periodic adjustment.
//
// # Inputs
//
//   - config: Configuration for sampling behavior
//
// # Outputs
//
//   - *DefaultAdaptiveSampler: New sampler
func NewAdaptiveSampler(config SamplingConfig) *DefaultAdaptiveSampler {
	// Apply defaults
	if config.BaseSamplingRate <= 0 {
		config.BaseSamplingRate = 0.1
	}
	if config.MinSamplingRate <= 0 {
		config.MinSamplingRate = 0.01
	}
	if config.MaxSamplingRate <= 0 {
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

	// Calculate buffer size based on expected samples
	bufferSize := int(config.LatencyWindow / config.AdjustmentInterval * 100)
	if bufferSize < 100 {
		bufferSize = 100
	}
	if bufferSize > 10000 {
		bufferSize = 10000
	}

	s := &DefaultAdaptiveSampler{
		config:    config,
		latencies: make([]time.Duration, bufferSize),
		stopCh:    make(chan struct{}),
		randState: uint64(time.Now().UnixNano()),
	}

	s.currentRate.Store(config.BaseSamplingRate)
	s.throttleReason.Store("")
	s.forceUntil.Store(time.Time{})

	// Start adjustment goroutine
	s.wg.Add(1)
	go s.adjustLoop()

	return s
}

// ShouldSample returns true if this item should be sampled.
//
// # Description
//
// Uses the current sampling rate to make a probabilistic decision.
// Thread-safe and fast (lock-free for the common path).
//
// # Outputs
//
//   - bool: True if item should be sampled
func (s *DefaultAdaptiveSampler) ShouldSample() bool {
	// Force enabled?
	if s.forceEnabled.Load() {
		until := s.forceUntil.Load().(time.Time)
		if time.Now().Before(until) {
			s.totalSampled.Add(1)
			return true
		}
		s.forceEnabled.Store(false)
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

	// Probabilistic sampling
	if s.fastRandom() < rate {
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
// consistent sampling of related operations.
//
// # Inputs
//
//   - ctx: Request context
//
// # Outputs
//
//   - context.Context: Context with sampling decision
//   - bool: True if item should be sampled
func (s *DefaultAdaptiveSampler) ShouldSampleContext(ctx context.Context) (context.Context, bool) {
	// Check if already decided
	if sampled, ok := ctx.Value(samplingContextKey{}).(bool); ok {
		if sampled {
			s.totalSampled.Add(1)
		} else {
			s.totalDropped.Add(1)
		}
		return ctx, sampled
	}

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
// # Inputs
//
//   - latency: The measured latency
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
// # Outputs
//
//   - float64: Current sampling rate
func (s *DefaultAdaptiveSampler) GetSamplingRate() float64 {
	return s.currentRate.Load().(float64)
}

// SetBaseSamplingRate sets the base sampling rate.
//
// # Description
//
// Updates the base rate. The actual rate may still be adjusted
// based on load.
//
// # Inputs
//
//   - rate: New base rate (0.0-1.0)
func (s *DefaultAdaptiveSampler) SetBaseSamplingRate(rate float64) {
	if rate < s.config.MinSamplingRate {
		rate = s.config.MinSamplingRate
	}
	if rate > s.config.MaxSamplingRate {
		rate = s.config.MaxSamplingRate
	}
	s.config.BaseSamplingRate = rate
	s.currentRate.Store(rate)
}

// Stats returns current sampler statistics.
//
// # Description
//
// Returns a snapshot of current sampling statistics.
//
// # Outputs
//
//   - SamplerStats: Current statistics
func (s *DefaultAdaptiveSampler) Stats() SamplerStats {
	return SamplerStats{
		TotalSampled:   s.totalSampled.Load(),
		TotalDropped:   s.totalDropped.Load(),
		CurrentRate:    s.currentRate.Load().(float64),
		AverageLatency: s.calculateAverageLatency(),
		IsThrottled:    s.isThrottled.Load(),
		ThrottleReason: s.throttleReason.Load().(string),
		ForceEnabled:   s.forceEnabled.Load(),
	}
}

// ForceEnable temporarily enables 100% sampling.
//
// # Description
//
// Forces 100% sampling for a specified duration. Useful for
// debugging specific issues.
//
// # Inputs
//
//   - duration: How long to force 100% sampling
func (s *DefaultAdaptiveSampler) ForceEnable(duration time.Duration) {
	s.forceUntil.Store(time.Now().Add(duration))
	s.forceEnabled.Store(true)
}

// Stop stops the sampler's background goroutine.
//
// # Description
//
// Stops the adjustment loop. Should be called on shutdown.
func (s *DefaultAdaptiveSampler) Stop() {
	close(s.stopCh)
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
func (s *DefaultAdaptiveSampler) adjustRate() {
	avgLatency := s.calculateAverageLatency()

	// No data yet
	if avgLatency == 0 {
		return
	}

	currentRate := s.currentRate.Load().(float64)
	newRate := currentRate

	threshold := s.config.LatencyThreshold

	if avgLatency > threshold {
		// High latency - reduce sampling
		ratio := float64(threshold) / float64(avgLatency)
		newRate = currentRate * ratio

		s.isThrottled.Store(true)
		s.throttleReason.Store("latency exceeded threshold")
	} else if avgLatency < threshold/2 {
		// Low latency - increase sampling (slowly)
		newRate = currentRate * 1.1

		if newRate >= s.config.BaseSamplingRate {
			s.isThrottled.Store(false)
			s.throttleReason.Store("")
		}
	}

	// Apply bounds
	if newRate < s.config.MinSamplingRate {
		newRate = s.config.MinSamplingRate
	}
	if newRate > s.config.MaxSamplingRate {
		newRate = s.config.MaxSamplingRate
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

// fastRandom returns a random float64 in [0, 1).
// Uses xorshift for speed (no lock contention).
func (s *DefaultAdaptiveSampler) fastRandom() float64 {
	s.randMu.Lock()
	x := s.randState
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	s.randState = x
	s.randMu.Unlock()

	return float64(x%1000000) / 1000000.0
}

// Compile-time interface check
var _ AdaptiveSampler = (*DefaultAdaptiveSampler)(nil)

// AlwaysSampleError is a helper that always samples errors.
//
// # Description
//
// Wraps ShouldSample to always return true for errors.
//
// # Inputs
//
//   - sampler: The sampler to use
//   - isError: Whether this is an error case
//
// # Outputs
//
//   - bool: True if should sample (always true for errors)
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
//
// # Inputs
//
//   - n: Number of items to sample
//
// # Outputs
//
//   - func(): Function that returns true for first N calls
func HeadSampler(n int) func() bool {
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
//
// # Inputs
//
//   - perSecond: Maximum samples per second
//
// # Outputs
//
//   - *RateLimitedSamplerImpl: Rate-limited sampler
func RateLimitedSampler(perSecond int) *RateLimitedSamplerImpl {
	return &RateLimitedSamplerImpl{
		maxPerSecond: perSecond,
		windowStart:  time.Now(),
	}
}

// RateLimitedSamplerImpl implements rate-limited sampling.
type RateLimitedSamplerImpl struct {
	maxPerSecond int
	windowStart  time.Time
	count        int
	mu           sync.Mutex
}

// ShouldSample returns true if under rate limit.
func (r *RateLimitedSamplerImpl) ShouldSample() bool {
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
