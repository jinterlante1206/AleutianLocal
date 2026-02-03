// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ab

import (
	"math"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Sample Collector Tests
// -----------------------------------------------------------------------------

func TestSampleCollector_Add(t *testing.T) {
	t.Run("basic add", func(t *testing.T) {
		collector := NewSampleCollector(100, 0)

		collector.Add(10 * time.Millisecond)
		collector.Add(20 * time.Millisecond)
		collector.Add(30 * time.Millisecond)

		samples := collector.Samples()
		if len(samples) != 3 {
			t.Errorf("expected 3 samples, got %d", len(samples))
		}
		if samples[0] != 10*time.Millisecond {
			t.Errorf("expected first sample to be 10ms, got %v", samples[0])
		}
	})

	t.Run("max samples enforced", func(t *testing.T) {
		collector := NewSampleCollector(3, 0)

		for i := 0; i < 5; i++ {
			collector.Add(time.Duration(i+1) * time.Millisecond)
		}

		samples := collector.Samples()
		if len(samples) != 3 {
			t.Errorf("expected 3 samples (max), got %d", len(samples))
		}
		// Should have evicted oldest (1ms, 2ms)
		if samples[0] != 3*time.Millisecond {
			t.Errorf("expected first sample to be 3ms (oldest kept), got %v", samples[0])
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		collector := NewSampleCollector(1000, 0)

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				collector.Add(time.Duration(i) * time.Microsecond)
			}(i)
		}
		wg.Wait()

		if collector.Count() != 100 {
			t.Errorf("expected 100 samples, got %d", collector.Count())
		}
	})
}

func TestSampleCollector_Reset(t *testing.T) {
	collector := NewSampleCollector(100, 0)
	collector.Add(10 * time.Millisecond)
	collector.Add(20 * time.Millisecond)

	collector.Reset()

	if collector.Count() != 0 {
		t.Errorf("expected 0 samples after reset, got %d", collector.Count())
	}
}

// -----------------------------------------------------------------------------
// Welch's t-test Tests
// -----------------------------------------------------------------------------

func TestWelchTTest(t *testing.T) {
	t.Run("significant difference", func(t *testing.T) {
		// Create two clearly different sample sets
		fast := make([]time.Duration, 50)
		slow := make([]time.Duration, 50)
		for i := 0; i < 50; i++ {
			fast[i] = time.Duration(10+i%5) * time.Millisecond
			slow[i] = time.Duration(100+i%5) * time.Millisecond
		}

		result, err := WelchTTest(fast, slow, 0.05)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.Significant {
			t.Errorf("expected significant difference, got p=%.4f", result.PValue)
		}
		if result.TStatistic >= 0 {
			t.Errorf("expected negative t-statistic (fast < slow), got %.4f", result.TStatistic)
		}
	})

	t.Run("no significant difference", func(t *testing.T) {
		// Create two similar sample sets
		samples1 := make([]time.Duration, 30)
		samples2 := make([]time.Duration, 30)
		for i := 0; i < 30; i++ {
			samples1[i] = time.Duration(100+i%10) * time.Millisecond
			samples2[i] = time.Duration(100+i%10) * time.Millisecond
		}

		result, err := WelchTTest(samples1, samples2, 0.05)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Significant {
			t.Errorf("expected no significant difference for identical samples")
		}
	})

	t.Run("insufficient samples", func(t *testing.T) {
		samples1 := []time.Duration{10 * time.Millisecond}
		samples2 := []time.Duration{20 * time.Millisecond}

		_, err := WelchTTest(samples1, samples2, 0.05)
		if err != ErrInsufficientSamples {
			t.Errorf("expected ErrInsufficientSamples, got %v", err)
		}
	})

	t.Run("zero variance", func(t *testing.T) {
		samples1 := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
		samples2 := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}

		_, err := WelchTTest(samples1, samples2, 0.05)
		if err != ErrZeroVariance {
			t.Errorf("expected ErrZeroVariance, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Confidence Interval Tests
// -----------------------------------------------------------------------------

func TestCalculateCI(t *testing.T) {
	t.Run("basic CI", func(t *testing.T) {
		fast := make([]time.Duration, 50)
		slow := make([]time.Duration, 50)
		for i := 0; i < 50; i++ {
			fast[i] = time.Duration(10+i%5) * time.Millisecond
			slow[i] = time.Duration(100+i%5) * time.Millisecond
		}

		ci, err := CalculateCI(fast, slow, 0.95)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Difference should be negative (fast - slow)
		if ci.Center >= 0 {
			t.Errorf("expected negative center (fast < slow), got %.2f", ci.Center)
		}

		// Interval should not contain zero
		if ci.Contains(0) {
			t.Errorf("expected CI to not contain zero for significant difference")
		}

		if ci.Level != 0.95 {
			t.Errorf("expected level 0.95, got %.2f", ci.Level)
		}
	})

	t.Run("insufficient samples", func(t *testing.T) {
		samples1 := []time.Duration{10 * time.Millisecond}
		samples2 := []time.Duration{20 * time.Millisecond}

		_, err := CalculateCI(samples1, samples2, 0.95)
		if err != ErrInsufficientSamples {
			t.Errorf("expected ErrInsufficientSamples, got %v", err)
		}
	})
}

func TestConfidenceInterval_Contains(t *testing.T) {
	ci := &ConfidenceInterval{
		Lower:  -10,
		Upper:  10,
		Center: 0,
		Level:  0.95,
	}

	if !ci.Contains(0) {
		t.Error("expected CI to contain 0")
	}
	if !ci.Contains(-10) {
		t.Error("expected CI to contain lower bound")
	}
	if !ci.Contains(10) {
		t.Error("expected CI to contain upper bound")
	}
	if ci.Contains(11) {
		t.Error("expected CI to not contain 11")
	}
	if ci.Contains(-11) {
		t.Error("expected CI to not contain -11")
	}
}

func TestConfidenceInterval_Width(t *testing.T) {
	ci := &ConfidenceInterval{
		Lower: -10,
		Upper: 10,
	}

	if width := ci.Width(); width != 20 {
		t.Errorf("expected width 20, got %.2f", width)
	}
}

// -----------------------------------------------------------------------------
// Effect Size Tests
// -----------------------------------------------------------------------------

func TestEffectSize(t *testing.T) {
	t.Run("large effect", func(t *testing.T) {
		fast := make([]time.Duration, 50)
		slow := make([]time.Duration, 50)
		for i := 0; i < 50; i++ {
			fast[i] = time.Duration(10+i%5) * time.Millisecond  // 10-14ms
			slow[i] = time.Duration(100+i%5) * time.Millisecond // 100-104ms
		}

		d, err := EffectSize(fast, slow)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// d = (fast - slow) / pooledStdDev
		// Since fast < slow, d should be negative
		if d >= 0 {
			t.Errorf("expected negative effect size (fast < slow), got %.2f", d)
		}

		category := CategorizeEffect(d)
		if category != EffectLarge {
			t.Errorf("expected large effect, got %s", category)
		}
	})

	t.Run("effect size categories", func(t *testing.T) {
		tests := []struct {
			d        float64
			expected EffectCategory
		}{
			{0.1, EffectNegligible},
			{0.3, EffectSmall},
			{0.6, EffectMedium},
			{1.0, EffectLarge},
			{-0.3, EffectSmall}, // Absolute value used
			{-1.0, EffectLarge}, // Absolute value used
		}

		for _, tt := range tests {
			category := CategorizeEffect(tt.d)
			if category != tt.expected {
				t.Errorf("CategorizeEffect(%.2f): expected %s, got %s",
					tt.d, tt.expected, category)
			}
		}
	})

	t.Run("empty samples", func(t *testing.T) {
		_, err := EffectSize([]time.Duration{}, []time.Duration{10 * time.Millisecond})
		if err != ErrInsufficientSamples {
			t.Errorf("expected ErrInsufficientSamples, got %v", err)
		}
	})
}

func TestEffectCategory_String(t *testing.T) {
	tests := []struct {
		category EffectCategory
		expected string
	}{
		{EffectNegligible, "negligible"},
		{EffectSmall, "small"},
		{EffectMedium, "medium"},
		{EffectLarge, "large"},
		{EffectCategory(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.category.String(); got != tt.expected {
			t.Errorf("%d.String(): expected %s, got %s", tt.category, tt.expected, got)
		}
	}
}

// -----------------------------------------------------------------------------
// Power Analysis Tests
// -----------------------------------------------------------------------------

func TestCalculatePower(t *testing.T) {
	t.Run("high power with large sample", func(t *testing.T) {
		power := CalculatePower(100, 100, 0.5, 0.05)

		// With 100 samples per group and medium effect, should have high power
		if power < 0.8 {
			t.Errorf("expected power >= 0.8, got %.2f", power)
		}
	})

	t.Run("low power with small sample", func(t *testing.T) {
		power := CalculatePower(10, 10, 0.2, 0.05)

		// With 10 samples per group and small effect, should have low power
		if power > 0.5 {
			t.Errorf("expected power < 0.5 for small samples/effect, got %.2f", power)
		}
	})

	t.Run("insufficient samples", func(t *testing.T) {
		power := CalculatePower(1, 1, 0.5, 0.05)
		if power != 0 {
			t.Errorf("expected power 0 for n=1, got %.2f", power)
		}
	})
}

func TestRequiredSampleSize(t *testing.T) {
	t.Run("medium effect size", func(t *testing.T) {
		n := RequiredSampleSize(0.5, 0.05, 0.8)

		// For medium effect (d=0.5) at 80% power, should need ~64 per group
		if n < 50 || n > 100 {
			t.Errorf("expected sample size ~64 for medium effect, got %d", n)
		}
	})

	t.Run("small effect size needs more samples", func(t *testing.T) {
		nSmall := RequiredSampleSize(0.2, 0.05, 0.8)
		nMedium := RequiredSampleSize(0.5, 0.05, 0.8)

		if nSmall <= nMedium {
			t.Errorf("expected more samples for small effect: small=%d, medium=%d",
				nSmall, nMedium)
		}
	})

	t.Run("zero effect size", func(t *testing.T) {
		n := RequiredSampleSize(0, 0.05, 0.8)
		if n != math.MaxInt32 {
			t.Errorf("expected MaxInt32 for zero effect, got %d", n)
		}
	})
}

// -----------------------------------------------------------------------------
// Bootstrap Tests
// -----------------------------------------------------------------------------

func TestBootstrapCI(t *testing.T) {
	t.Run("bootstrap CI contains true difference", func(t *testing.T) {
		// Create samples with known difference
		fast := make([]time.Duration, 100)
		slow := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			fast[i] = time.Duration(10+i%10) * time.Millisecond
			slow[i] = time.Duration(50+i%10) * time.Millisecond
		}

		ci, err := BootstrapCI(fast, slow, 0.95, 1000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// True difference is approximately -40ms
		trueDiff := float64(-40 * time.Millisecond)

		if !ci.Contains(trueDiff) {
			t.Errorf("expected bootstrap CI [%.2f, %.2f] to contain true diff %.2f",
				ci.Lower, ci.Upper, trueDiff)
		}
	})

	t.Run("insufficient samples", func(t *testing.T) {
		_, err := BootstrapCI(
			[]time.Duration{10 * time.Millisecond},
			[]time.Duration{20 * time.Millisecond},
			0.95, 1000,
		)
		if err != ErrInsufficientSamples {
			t.Errorf("expected ErrInsufficientSamples, got %v", err)
		}
	})

	t.Run("low bootstrap count uses minimum", func(t *testing.T) {
		samples := make([]time.Duration, 10)
		for i := 0; i < 10; i++ {
			samples[i] = time.Duration(i) * time.Millisecond
		}

		// Should not error even with low bootstrap count
		_, err := BootstrapCI(samples, samples, 0.95, 10)
		if err != nil {
			t.Fatalf("unexpected error with low bootstrap count: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Helper Function Tests
// -----------------------------------------------------------------------------

func TestMean(t *testing.T) {
	samples := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}

	m := mean(samples)
	expected := float64(20 * time.Millisecond)

	if m != expected {
		t.Errorf("expected mean %.2f, got %.2f", expected, m)
	}
}

func TestMean_Empty(t *testing.T) {
	m := mean([]time.Duration{})
	if m != 0 {
		t.Errorf("expected 0 for empty samples, got %.2f", m)
	}
}

func TestVariance(t *testing.T) {
	samples := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}
	m := mean(samples)
	v := variance(samples, m)

	// Variance should be positive
	if v <= 0 {
		t.Errorf("expected positive variance, got %.2f", v)
	}
}

func TestNormalCDF(t *testing.T) {
	// Test known values
	tests := []struct {
		x        float64
		expected float64
		epsilon  float64
	}{
		{0, 0.5, 0.001},
		{1.96, 0.975, 0.01},
		{-1.96, 0.025, 0.01},
	}

	for _, tt := range tests {
		got := normalCDF(tt.x)
		if math.Abs(got-tt.expected) > tt.epsilon {
			t.Errorf("normalCDF(%.2f): expected ~%.3f, got %.3f", tt.x, tt.expected, got)
		}
	}
}

func TestZScore(t *testing.T) {
	// Test inverse of normalCDF
	tests := []struct {
		p        float64
		expected float64
		epsilon  float64
	}{
		{0.5, 0, 0.001},
		{0.975, 1.96, 0.05},
		{0.025, -1.96, 0.05},
	}

	for _, tt := range tests {
		got := zScore(tt.p)
		if math.Abs(got-tt.expected) > tt.epsilon {
			t.Errorf("zScore(%.3f): expected ~%.2f, got %.2f", tt.p, tt.expected, got)
		}
	}
}

func TestTCriticalValue(t *testing.T) {
	// Test that values decrease as df increases
	t.Run("decreasing with df", func(t *testing.T) {
		prev := tCriticalValue(1, 0.95)
		for df := 2; df <= 30; df++ {
			curr := tCriticalValue(df, 0.95)
			if curr >= prev {
				t.Errorf("t critical value should decrease: df=%d (%.3f) >= df=%d (%.3f)",
					df, curr, df-1, prev)
			}
			prev = curr
		}
	})

	t.Run("large df approaches z", func(t *testing.T) {
		tVal := tCriticalValue(100, 0.95)
		expected := 1.96 // z-score for 95%

		if math.Abs(tVal-expected) > 0.01 {
			t.Errorf("expected t(100, 0.95) ≈ 1.96, got %.3f", tVal)
		}
	})

	t.Run("handles invalid df", func(t *testing.T) {
		// Should not panic and return sensible value
		tVal := tCriticalValue(0, 0.95)
		if tVal <= 0 {
			t.Errorf("expected positive t value for df=0, got %.3f", tVal)
		}
	})
}
