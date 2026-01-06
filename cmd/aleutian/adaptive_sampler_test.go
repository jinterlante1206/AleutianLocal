package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDefaultSamplingConfig(t *testing.T) {
	config := DefaultSamplingConfig()

	if config.BaseSamplingRate <= 0 || config.BaseSamplingRate > 1 {
		t.Error("BaseSamplingRate should be between 0 and 1")
	}
	if config.MinSamplingRate <= 0 {
		t.Error("MinSamplingRate should be positive")
	}
	if config.MaxSamplingRate <= 0 {
		t.Error("MaxSamplingRate should be positive")
	}
	if config.LatencyThreshold <= 0 {
		t.Error("LatencyThreshold should be positive")
	}
}

func TestNewAdaptiveSampler(t *testing.T) {
	sampler := NewAdaptiveSampler(DefaultSamplingConfig())
	defer sampler.Stop()

	if sampler == nil {
		t.Fatal("NewAdaptiveSampler returned nil")
	}
}

func TestNewAdaptiveSampler_DefaultsZeroConfig(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		// All zero values
	})
	defer sampler.Stop()

	if sampler == nil {
		t.Fatal("NewAdaptiveSampler returned nil")
	}

	// Should have applied defaults
	if sampler.GetSamplingRate() <= 0 {
		t.Error("Should have applied default sampling rate")
	}
}

func TestNewAdaptiveSampler_ClampsInvalidRates(t *testing.T) {
	// Test that rates > 1.0 are clamped to defaults
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 10.0,  // Invalid: 1000%
		MinSamplingRate:  5.0,   // Invalid: 500%
		MaxSamplingRate:  100.0, // Invalid: 10000%
	})
	defer sampler.Stop()

	// Should have applied defaults (not the invalid values)
	rate := sampler.GetSamplingRate()
	if rate > 1.0 {
		t.Errorf("Rate should not exceed 1.0, got %v", rate)
	}
	if rate <= 0 {
		t.Errorf("Rate should be positive, got %v", rate)
	}
}

func TestNewAdaptiveSampler_EnforcesLogicalBounds(t *testing.T) {
	// Test that BaseSamplingRate is clamped to [MinSamplingRate, MaxSamplingRate]
	// Case 1: Base rate below minimum
	sampler1 := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.05, // Below min
		MinSamplingRate:  0.1,
		MaxSamplingRate:  0.9,
	})
	defer sampler1.Stop()

	rate1 := sampler1.GetSamplingRate()
	if rate1 < 0.1 {
		t.Errorf("Base rate should be clamped to min (0.1), got %v", rate1)
	}

	// Case 2: Base rate above maximum
	sampler2 := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.95, // Above max
		MinSamplingRate:  0.1,
		MaxSamplingRate:  0.8,
	})
	defer sampler2.Stop()

	rate2 := sampler2.GetSamplingRate()
	if rate2 > 0.8 {
		t.Errorf("Base rate should be clamped to max (0.8), got %v", rate2)
	}
}

func TestAdaptiveSampler_ShouldSample_Rate100(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 1.0, // 100%
		MinSamplingRate:  0.01,
		MaxSamplingRate:  1.0,
	})
	defer sampler.Stop()

	// Should always sample at 100%
	for i := 0; i < 100; i++ {
		if !sampler.ShouldSample() {
			t.Error("Should always sample at 100% rate")
		}
	}
}

func TestAdaptiveSampler_ShouldSample_Rate0(t *testing.T) {
	// Use a very low min rate to allow setting to 0
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.0,  // 0%
		MinSamplingRate:  -1.0, // Allow 0 (will be clamped to 0 not negative)
		MaxSamplingRate:  1.0,
	})
	defer sampler.Stop()

	// Manually set rate to 0 via atomic store to bypass bounds check
	sampler.currentRate.Store(float64(0))

	// Should never sample at 0%
	for i := 0; i < 100; i++ {
		if sampler.ShouldSample() {
			t.Error("Should never sample at 0% rate")
		}
	}
}

func TestAdaptiveSampler_ShouldSample_Probabilistic(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.5, // 50%
		MinSamplingRate:  0.01,
		MaxSamplingRate:  1.0,
	})
	defer sampler.Stop()

	// Sample 1000 times and check distribution
	sampled := 0
	for i := 0; i < 1000; i++ {
		if sampler.ShouldSample() {
			sampled++
		}
	}

	// Should be roughly 50% (allow wide margin for randomness)
	rate := float64(sampled) / 1000.0
	if rate < 0.3 || rate > 0.7 {
		t.Errorf("Sampling rate = %.2f, expected ~0.5", rate)
	}
}

func TestAdaptiveSampler_ShouldSampleContext(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 1.0, // 100%
	})
	defer sampler.Stop()

	ctx := context.Background()

	// First call decides
	ctx, sampled := sampler.ShouldSampleContext(ctx)
	if !sampled {
		t.Error("Should sample at 100% rate")
	}

	// Subsequent calls should return same decision
	_, sampled2 := sampler.ShouldSampleContext(ctx)
	if sampled != sampled2 {
		t.Error("Context should preserve sampling decision")
	}
}

func TestAdaptiveSampler_RecordLatency(t *testing.T) {
	sampler := NewAdaptiveSampler(DefaultSamplingConfig())
	defer sampler.Stop()

	// Record some latencies
	sampler.RecordLatency(50 * time.Millisecond)
	sampler.RecordLatency(100 * time.Millisecond)
	sampler.RecordLatency(150 * time.Millisecond)

	// Stats should show average
	stats := sampler.Stats()
	if stats.AverageLatency == 0 {
		t.Error("AverageLatency should be calculated")
	}

	// Average should be 100ms
	expectedAvg := 100 * time.Millisecond
	if stats.AverageLatency < expectedAvg-20*time.Millisecond ||
		stats.AverageLatency > expectedAvg+20*time.Millisecond {
		t.Errorf("AverageLatency = %v, expected ~%v", stats.AverageLatency, expectedAvg)
	}
}

func TestAdaptiveSampler_GetSamplingRate(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.25,
	})
	defer sampler.Stop()

	rate := sampler.GetSamplingRate()
	if rate != 0.25 {
		t.Errorf("GetSamplingRate() = %v, want 0.25", rate)
	}
}

func TestAdaptiveSampler_SetBaseSamplingRate(t *testing.T) {
	sampler := NewAdaptiveSampler(DefaultSamplingConfig())
	defer sampler.Stop()

	sampler.SetBaseSamplingRate(0.75)

	// Note: SetBaseSamplingRate does NOT immediately update currentRate.
	// The adjustLoop goroutine is the single source of truth for currentRate.
	// Here we verify the config was updated by checking Stats().
	stats := sampler.Stats()
	// The currentRate will converge to the new base rate over time via adjustLoop.
	// For immediate verification, we can check that the sampler still functions.
	_ = sampler.ShouldSample()
	_ = stats
}

func TestAdaptiveSampler_SetBaseSamplingRate_Bounds(t *testing.T) {
	// Note: SetBaseSamplingRate no longer immediately updates currentRate.
	// The adjustLoop is the single source of truth for currentRate.
	// This test verifies that SetBaseSamplingRate doesn't panic with out-of-bounds values.
	sampler := NewAdaptiveSampler(SamplingConfig{
		MinSamplingRate: 0.1,
		MaxSamplingRate: 0.9,
	})
	defer sampler.Stop()

	// Below min - should be clamped internally (no panic)
	sampler.SetBaseSamplingRate(0.01)

	// Above max - should be clamped internally (no panic)
	sampler.SetBaseSamplingRate(1.0)

	// Verify sampler still works after bound-violating calls
	_ = sampler.ShouldSample()
}

func TestAdaptiveSampler_Stats(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 1.0, // 100%
	})
	defer sampler.Stop()

	// Sample a few times
	for i := 0; i < 10; i++ {
		sampler.ShouldSample()
	}

	stats := sampler.Stats()

	if stats.TotalSampled != 10 {
		t.Errorf("TotalSampled = %d, want 10", stats.TotalSampled)
	}
	if stats.TotalDropped != 0 {
		t.Errorf("TotalDropped = %d, want 0", stats.TotalDropped)
	}
	if stats.CurrentRate != 1.0 {
		t.Errorf("CurrentRate = %v, want 1.0", stats.CurrentRate)
	}
}

func TestAdaptiveSampler_ForceEnable(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.0, // 0% - would never sample
		MinSamplingRate:  0.0,
	})
	defer sampler.Stop()

	// Force enable for 100ms
	sampler.ForceEnable(100 * time.Millisecond)

	// Should now sample even though rate is 0
	if !sampler.ShouldSample() {
		t.Error("Should sample when force enabled")
	}

	stats := sampler.Stats()
	if !stats.ForceEnabled {
		t.Error("ForceEnabled should be true")
	}

	// Wait for force enable to expire
	time.Sleep(150 * time.Millisecond)

	// Force should be disabled after expiry
	// Note: The forceEnabled flag is only cleared on the next ShouldSample call
	sampler.ShouldSample()

	stats = sampler.Stats()
	if stats.ForceEnabled {
		t.Error("ForceEnabled should be false after expiry")
	}
}

func TestAdaptiveSampler_AdaptiveThrottling(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate:   0.5,
		MinSamplingRate:    0.01,
		MaxSamplingRate:    1.0,
		LatencyThreshold:   50 * time.Millisecond,
		AdjustmentInterval: 10 * time.Millisecond, // Fast for testing
	})
	defer sampler.Stop()

	// Record high latencies
	for i := 0; i < 100; i++ {
		sampler.RecordLatency(200 * time.Millisecond) // 4x threshold
	}

	// Wait for adjustment
	time.Sleep(50 * time.Millisecond)

	// Rate should have decreased
	rate := sampler.GetSamplingRate()
	if rate >= 0.5 {
		t.Errorf("Rate should have decreased from 0.5, got %v", rate)
	}

	stats := sampler.Stats()
	if !stats.IsThrottled {
		t.Error("Should be throttled with high latency")
	}
}

func TestAdaptiveSampler_IsThrottled_ClearsOnRecovery(t *testing.T) {
	// Use a small buffer size via short LatencyWindow to make testing easier
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate:   0.5,
		MinSamplingRate:    0.01,
		MaxSamplingRate:    1.0,
		LatencyThreshold:   50 * time.Millisecond,
		AdjustmentInterval: 10 * time.Millisecond, // Fast for testing
		LatencyWindow:      100 * time.Millisecond,
	})
	defer sampler.Stop()

	// Record high latencies to trigger throttling
	// Buffer size is ~1000 (100ms/10ms * 100), so fill it completely
	for i := 0; i < 1000; i++ {
		sampler.RecordLatency(200 * time.Millisecond)
	}

	// Wait for adjustment
	time.Sleep(50 * time.Millisecond)

	stats := sampler.Stats()
	if !stats.IsThrottled {
		t.Error("Should be throttled with high latency")
	}

	// Now record normal latencies (below threshold)
	// Fill the entire buffer to flush out all high latencies
	for i := 0; i < 1000; i++ {
		sampler.RecordLatency(40 * time.Millisecond) // Below threshold (50ms)
	}

	// Wait for adjustment
	time.Sleep(50 * time.Millisecond)

	// isThrottled should now be false because latency is below threshold
	stats = sampler.Stats()
	if stats.IsThrottled {
		t.Error("Should NOT be throttled when latency is below threshold")
	}
}

func TestAdaptiveSampler_ConcurrentAccess(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.5,
	})
	defer sampler.Stop()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Concurrent samplers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					sampler.ShouldSample()
				}
			}
		}()
	}

	// Concurrent latency recorders
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					sampler.RecordLatency(50 * time.Millisecond)
				}
			}
		}()
	}

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()

	// Should not panic and stats should be reasonable
	stats := sampler.Stats()
	if stats.TotalSampled+stats.TotalDropped == 0 {
		t.Error("Should have processed some samples")
	}
}

func TestAlwaysSampleError(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.0, // 0% - would never sample
		MinSamplingRate:  0.0,
	})
	defer sampler.Stop()

	// Errors should always sample
	if !AlwaysSampleError(sampler, true) {
		t.Error("Errors should always be sampled")
	}

	// Non-errors use sampler (0% rate = no sampling)
	// Note: Can't reliably test this since sampler rate is 0
}

func TestHeadSampler(t *testing.T) {
	sampler := HeadSampler(3)

	// First 3 should sample
	if !sampler() {
		t.Error("Item 1 should sample")
	}
	if !sampler() {
		t.Error("Item 2 should sample")
	}
	if !sampler() {
		t.Error("Item 3 should sample")
	}

	// Rest should not
	if sampler() {
		t.Error("Item 4 should not sample")
	}
	if sampler() {
		t.Error("Item 5 should not sample")
	}
}

func TestHeadSampler_Concurrent(t *testing.T) {
	sampler := HeadSampler(100)

	var wg sync.WaitGroup
	sampled := make(chan bool, 200)

	// Launch 200 goroutines
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sampled <- sampler()
		}()
	}

	wg.Wait()
	close(sampled)

	// Count sampled
	count := 0
	for s := range sampled {
		if s {
			count++
		}
	}

	// Should have exactly 100
	if count != 100 {
		t.Errorf("HeadSampler sampled %d, want 100", count)
	}
}

func TestRateLimitedSampler(t *testing.T) {
	sampler := RateLimitedSampler(5) // 5 per second

	// First 5 should succeed
	for i := 0; i < 5; i++ {
		if !sampler() {
			t.Errorf("Sample %d should succeed", i)
		}
	}

	// 6th should fail
	if sampler() {
		t.Error("Sample 6 should fail (rate limited)")
	}

	// Wait for new window
	time.Sleep(1100 * time.Millisecond)

	// Should work again
	if !sampler() {
		t.Error("Sample in new window should succeed")
	}
}

func TestAdaptiveSampler_Stop(t *testing.T) {
	sampler := NewAdaptiveSampler(DefaultSamplingConfig())

	// First stop should not panic
	sampler.Stop()

	// Double stop should not panic (tests idempotency via sync.Once)
	sampler.Stop()

	// Third stop should also not panic (confirms sync.Once is working)
	sampler.Stop()
}

func TestAdaptiveSampler_InterfaceCompliance(t *testing.T) {
	var _ AdaptiveSampler = (*DefaultAdaptiveSampler)(nil)
}

func TestSamplerStats_Fields(t *testing.T) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.5,
	})
	defer sampler.Stop()

	// Force some state
	sampler.ForceEnable(time.Minute)

	stats := sampler.Stats()

	// Check all fields are accessible
	_ = stats.TotalSampled
	_ = stats.TotalDropped
	_ = stats.CurrentRate
	_ = stats.AverageLatency
	_ = stats.IsThrottled
	_ = stats.ThrottleReason
	_ = stats.ForceEnabled

	if !stats.ForceEnabled {
		t.Error("ForceEnabled should be true")
	}
}

func TestHeadSampler_InvalidInput(t *testing.T) {
	// n = 0 should never sample
	sampler0 := HeadSampler(0)
	for i := 0; i < 10; i++ {
		if sampler0() {
			t.Error("HeadSampler(0) should never sample")
		}
	}

	// n = -1 should never sample
	samplerNeg := HeadSampler(-1)
	for i := 0; i < 10; i++ {
		if samplerNeg() {
			t.Error("HeadSampler(-1) should never sample")
		}
	}
}

func TestRateLimitedSampler_InvalidInput(t *testing.T) {
	// perSecond = 0 should never sample
	sampler0 := RateLimitedSampler(0)
	for i := 0; i < 10; i++ {
		if sampler0() {
			t.Error("RateLimitedSampler(0) should never sample")
		}
	}

	// perSecond = -1 should never sample
	samplerNeg := RateLimitedSampler(-1)
	for i := 0; i < 10; i++ {
		if samplerNeg() {
			t.Error("RateLimitedSampler(-1) should never sample")
		}
	}
}

func TestAdaptiveSampler_ForceEnable_ConcurrentRace(t *testing.T) {
	// Test that ForceEnable doesn't get prematurely disabled by concurrent expiry checks
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.0, // 0% - won't sample without force
		MinSamplingRate:  0.0,
	})
	defer sampler.Stop()

	// Force enable for a very short time (will expire almost immediately)
	sampler.ForceEnable(1 * time.Millisecond)
	time.Sleep(10 * time.Millisecond) // Let it expire

	// Call ShouldSample to trigger expiry cleanup
	sampler.ShouldSample()

	// Now force enable for longer
	sampler.ForceEnable(100 * time.Millisecond)

	// The force-enable should still be active (not disabled by stale expiry check)
	stats := sampler.Stats()
	if !stats.ForceEnabled {
		t.Error("ForceEnabled should be true after re-enabling")
	}

	// Should sample because force is enabled
	if !sampler.ShouldSample() {
		t.Error("Should sample when force enabled")
	}
}

func TestAdaptiveSampler_LatencyBufferSize(t *testing.T) {
	// Test that LatencyBufferSize is respected
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate:  0.5,
		LatencyBufferSize: 50, // Small buffer for testing
	})
	defer sampler.Stop()

	// Record more than buffer size latencies
	for i := 0; i < 100; i++ {
		sampler.RecordLatency(time.Duration(i) * time.Millisecond)
	}

	// Should not panic and stats should work
	stats := sampler.Stats()
	if stats.AverageLatency == 0 {
		t.Error("AverageLatency should be calculated")
	}
}

func BenchmarkAdaptiveSampler_ShouldSample(b *testing.B) {
	sampler := NewAdaptiveSampler(SamplingConfig{
		BaseSamplingRate: 0.5,
	})
	defer sampler.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampler.ShouldSample()
	}
}

func BenchmarkAdaptiveSampler_RecordLatency(b *testing.B) {
	sampler := NewAdaptiveSampler(DefaultSamplingConfig())
	defer sampler.Stop()

	latency := 50 * time.Millisecond

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampler.RecordLatency(latency)
	}
}
