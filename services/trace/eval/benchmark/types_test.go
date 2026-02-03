// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package benchmark

import (
	"math"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Iterations != 1000 {
		t.Errorf("Iterations = %d, want 1000", config.Iterations)
	}
	if config.Warmup != 100 {
		t.Errorf("Warmup = %d, want 100", config.Warmup)
	}
	if config.Timeout != 5*time.Minute {
		t.Errorf("Timeout = %v, want 5m", config.Timeout)
	}
	if config.CollectMemory != true {
		t.Errorf("CollectMemory = %v, want true", config.CollectMemory)
	}
	if config.RemoveOutliers != true {
		t.Errorf("RemoveOutliers = %v, want true", config.RemoveOutliers)
	}
	if config.OutlierThreshold != 1.5 {
		t.Errorf("OutlierThreshold = %v, want 1.5", config.OutlierThreshold)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "zero iterations",
			modify:  func(c *Config) { c.Iterations = 0 },
			wantErr: true,
		},
		{
			name:    "negative warmup",
			modify:  func(c *Config) { c.Warmup = -1 },
			wantErr: true,
		},
		{
			name:    "zero timeout",
			modify:  func(c *Config) { c.Timeout = 0 },
			wantErr: true,
		},
		{
			name:    "zero iteration timeout",
			modify:  func(c *Config) { c.IterationTimeout = 0 },
			wantErr: true,
		},
		{
			name:    "zero outlier threshold",
			modify:  func(c *Config) { c.OutlierThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "zero parallelism",
			modify:  func(c *Config) { c.Parallelism = 0 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.modify(config)

			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateLatencyStats(t *testing.T) {
	t.Run("empty samples", func(t *testing.T) {
		_, err := CalculateLatencyStats(nil)
		if err != ErrNoSamples {
			t.Errorf("Expected ErrNoSamples, got %v", err)
		}
	})

	t.Run("single sample", func(t *testing.T) {
		samples := []time.Duration{100 * time.Millisecond}
		stats, err := CalculateLatencyStats(samples)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if stats.Min != 100*time.Millisecond {
			t.Errorf("Min = %v, want 100ms", stats.Min)
		}
		if stats.Max != 100*time.Millisecond {
			t.Errorf("Max = %v, want 100ms", stats.Max)
		}
		if stats.Mean != 100*time.Millisecond {
			t.Errorf("Mean = %v, want 100ms", stats.Mean)
		}
	})

	t.Run("multiple samples", func(t *testing.T) {
		samples := []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			30 * time.Millisecond,
			40 * time.Millisecond,
			50 * time.Millisecond,
		}
		stats, err := CalculateLatencyStats(samples)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if stats.Min != 10*time.Millisecond {
			t.Errorf("Min = %v, want 10ms", stats.Min)
		}
		if stats.Max != 50*time.Millisecond {
			t.Errorf("Max = %v, want 50ms", stats.Max)
		}
		if stats.Mean != 30*time.Millisecond {
			t.Errorf("Mean = %v, want 30ms", stats.Mean)
		}
		if stats.Median != 30*time.Millisecond {
			t.Errorf("Median = %v, want 30ms", stats.Median)
		}
		if stats.P50 != 30*time.Millisecond {
			t.Errorf("P50 = %v, want 30ms", stats.P50)
		}
	})

	t.Run("percentile accuracy", func(t *testing.T) {
		// Generate 100 samples: 1ms, 2ms, ..., 100ms
		samples := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			samples[i] = time.Duration(i+1) * time.Millisecond
		}

		stats, err := CalculateLatencyStats(samples)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// P50 should be around 50ms
		if stats.P50 < 49*time.Millisecond || stats.P50 > 51*time.Millisecond {
			t.Errorf("P50 = %v, want ~50ms", stats.P50)
		}

		// P90 should be around 90ms
		if stats.P90 < 89*time.Millisecond || stats.P90 > 91*time.Millisecond {
			t.Errorf("P90 = %v, want ~90ms", stats.P90)
		}

		// P99 should be around 99ms
		if stats.P99 < 98*time.Millisecond || stats.P99 > 100*time.Millisecond {
			t.Errorf("P99 = %v, want ~99ms", stats.P99)
		}
	})
}

func TestRemoveOutliers(t *testing.T) {
	t.Run("small sample unchanged", func(t *testing.T) {
		samples := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
		filtered := RemoveOutliers(samples, 1.5)

		if len(filtered) != len(samples) {
			t.Errorf("Small samples should not be filtered: got %d, want %d", len(filtered), len(samples))
		}
	})

	t.Run("removes outliers", func(t *testing.T) {
		samples := []time.Duration{
			10 * time.Millisecond,
			11 * time.Millisecond,
			12 * time.Millisecond,
			13 * time.Millisecond,
			14 * time.Millisecond,
			15 * time.Millisecond,
			1000 * time.Millisecond, // Clear outlier
		}

		filtered := RemoveOutliers(samples, 1.5)

		if len(filtered) >= len(samples) {
			t.Errorf("Expected outliers to be removed: got %d, want < %d", len(filtered), len(samples))
		}

		// Check that the outlier was removed
		for _, s := range filtered {
			if s > 100*time.Millisecond {
				t.Errorf("Outlier %v should have been removed", s)
			}
		}
	})

	t.Run("preserves at least half", func(t *testing.T) {
		// All very different values - should not remove too many
		samples := []time.Duration{
			1 * time.Millisecond,
			10 * time.Millisecond,
			100 * time.Millisecond,
			1000 * time.Millisecond,
			10000 * time.Millisecond,
		}

		filtered := RemoveOutliers(samples, 1.5)

		if len(filtered) < len(samples)/2 {
			t.Errorf("Should preserve at least half: got %d, want >= %d", len(filtered), len(samples)/2)
		}
	})
}

func TestCalculateCohensD(t *testing.T) {
	t.Run("empty samples", func(t *testing.T) {
		d := CalculateCohensD(nil, []time.Duration{1 * time.Millisecond})
		if d != 0 {
			t.Errorf("Expected 0 for empty samples, got %v", d)
		}
	})

	t.Run("identical samples", func(t *testing.T) {
		samples := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
		d := CalculateCohensD(samples, samples)
		if d != 0 {
			t.Errorf("Expected 0 for identical samples, got %v", d)
		}
	})

	t.Run("different samples", func(t *testing.T) {
		samples1 := []time.Duration{10 * time.Millisecond, 11 * time.Millisecond, 12 * time.Millisecond}
		samples2 := []time.Duration{20 * time.Millisecond, 21 * time.Millisecond, 22 * time.Millisecond}

		d := CalculateCohensD(samples1, samples2)

		// Effect should be large and negative (samples1 is faster)
		if d >= 0 {
			t.Errorf("Expected negative Cohen's d (samples1 faster), got %v", d)
		}
		if math.Abs(d) < 0.8 {
			t.Errorf("Expected large effect size, got %v", d)
		}
	})
}

func TestCategorizeEffectSize(t *testing.T) {
	tests := []struct {
		d        float64
		expected EffectSizeCategory
	}{
		{0.0, EffectNegligible},
		{0.1, EffectNegligible},
		{0.19, EffectNegligible},
		{0.2, EffectSmall},
		{0.3, EffectSmall},
		{0.49, EffectSmall},
		{0.5, EffectMedium},
		{0.6, EffectMedium},
		{0.79, EffectMedium},
		{0.8, EffectLarge},
		{1.0, EffectLarge},
		{2.0, EffectLarge},
		{-0.5, EffectMedium}, // Absolute value
		{-1.0, EffectLarge},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := CategorizeEffectSize(tt.d)
			if got != tt.expected {
				t.Errorf("CategorizeEffectSize(%v) = %v, want %v", tt.d, got, tt.expected)
			}
		})
	}
}

func TestEffectSizeCategory_String(t *testing.T) {
	tests := []struct {
		cat      EffectSizeCategory
		expected string
	}{
		{EffectNegligible, "negligible"},
		{EffectSmall, "small"},
		{EffectMedium, "medium"},
		{EffectLarge, "large"},
		{EffectSizeCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.cat.String()
			if got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWelchTTest(t *testing.T) {
	t.Run("insufficient samples", func(t *testing.T) {
		_, p := WelchTTest([]time.Duration{1 * time.Millisecond}, []time.Duration{2 * time.Millisecond})
		if p != 1 {
			t.Errorf("Expected p=1 for insufficient samples, got %v", p)
		}
	})

	t.Run("identical samples", func(t *testing.T) {
		samples := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
		tStat, p := WelchTTest(samples, samples)

		if tStat != 0 {
			t.Errorf("Expected t=0 for identical samples, got %v", tStat)
		}
		// p-value should be 1 (no difference)
		if p != 1 {
			t.Errorf("Expected p=1 for identical samples, got %v", p)
		}
	})

	t.Run("different samples", func(t *testing.T) {
		// Create samples with some variance (not all identical)
		samples1 := make([]time.Duration, 100)
		samples2 := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			// Add some variation: 9-11ms for samples1, 19-21ms for samples2
			samples1[i] = time.Duration(9+i%3) * time.Millisecond
			samples2[i] = time.Duration(19+i%3) * time.Millisecond
		}

		tStat, p := WelchTTest(samples1, samples2)

		// t should be negative (samples1 is faster)
		if tStat >= 0 {
			t.Errorf("Expected negative t-statistic, got %v", tStat)
		}
		// p-value should be very small (highly significant)
		if p >= 0.05 {
			t.Errorf("Expected significant p-value (< 0.05), got %v", p)
		}
	})
}

func TestConfidenceInterval(t *testing.T) {
	t.Run("empty samples", func(t *testing.T) {
		lower, upper := ConfidenceInterval(nil, 0.95)
		if lower != 0 || upper != 0 {
			t.Errorf("Expected (0, 0) for empty samples, got (%v, %v)", lower, upper)
		}
	})

	t.Run("single sample", func(t *testing.T) {
		samples := []time.Duration{100 * time.Millisecond}
		lower, upper := ConfidenceInterval(samples, 0.95)
		if lower != samples[0] || upper != samples[0] {
			t.Errorf("Expected (100ms, 100ms) for single sample, got (%v, %v)", lower, upper)
		}
	})

	t.Run("multiple samples", func(t *testing.T) {
		samples := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			samples[i] = 100 * time.Millisecond
		}

		lower, upper := ConfidenceInterval(samples, 0.95)

		// With identical samples, CI should be very tight
		if lower != 100*time.Millisecond || upper != 100*time.Millisecond {
			t.Errorf("Expected tight CI around 100ms, got (%v, %v)", lower, upper)
		}
	})

	t.Run("varying samples", func(t *testing.T) {
		samples := []time.Duration{
			90 * time.Millisecond,
			95 * time.Millisecond,
			100 * time.Millisecond,
			105 * time.Millisecond,
			110 * time.Millisecond,
		}

		lower, upper := ConfidenceInterval(samples, 0.95)

		// Mean is 100ms
		if lower >= 100*time.Millisecond {
			t.Errorf("Lower bound %v should be below mean 100ms", lower)
		}
		if upper <= 100*time.Millisecond {
			t.Errorf("Upper bound %v should be above mean 100ms", upper)
		}
		if lower >= upper {
			t.Errorf("Lower bound %v should be less than upper bound %v", lower, upper)
		}
	})
}
