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
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrInsufficientSamples indicates not enough samples for analysis.
	ErrInsufficientSamples = errors.New("insufficient samples for statistical analysis")

	// ErrZeroVariance indicates a sample set has zero variance.
	ErrZeroVariance = errors.New("sample set has zero variance")
)

// -----------------------------------------------------------------------------
// Sample Collection
// -----------------------------------------------------------------------------

// SampleCollector accumulates samples for statistical analysis.
//
// Description:
//
//	SampleCollector provides thread-safe collection of duration samples
//	with support for rolling windows and summary statistics.
//
// Thread Safety: Safe for concurrent use.
type SampleCollector struct {
	mu          sync.RWMutex
	samples     []time.Duration
	maxSamples  int
	windowStart time.Time
	windowSize  time.Duration
}

// NewSampleCollector creates a new sample collector.
//
// Inputs:
//   - maxSamples: Maximum samples to retain. Zero means unlimited.
//   - windowSize: Time window for samples. Zero means no time limit.
//
// Outputs:
//   - *SampleCollector: The new collector. Never nil.
func NewSampleCollector(maxSamples int, windowSize time.Duration) *SampleCollector {
	return &SampleCollector{
		samples:     make([]time.Duration, 0, 1024),
		maxSamples:  maxSamples,
		windowStart: time.Now(),
		windowSize:  windowSize,
	}
}

// Add records a new sample.
//
// Thread Safety: Safe for concurrent use.
func (c *SampleCollector) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict old samples if window is set
	if c.windowSize > 0 {
		now := time.Now()
		if now.Sub(c.windowStart) > c.windowSize {
			// Shift to new window
			c.samples = c.samples[:0]
			c.windowStart = now
		}
	}

	// Evict oldest if at capacity
	if c.maxSamples > 0 && len(c.samples) >= c.maxSamples {
		c.samples = c.samples[1:]
	}

	c.samples = append(c.samples, d)
}

// Samples returns a copy of collected samples.
//
// Thread Safety: Safe for concurrent use.
func (c *SampleCollector) Samples() []time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]time.Duration, len(c.samples))
	copy(result, c.samples)
	return result
}

// Count returns the number of samples.
//
// Thread Safety: Safe for concurrent use.
func (c *SampleCollector) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.samples)
}

// Reset clears all samples.
//
// Thread Safety: Safe for concurrent use.
func (c *SampleCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples = c.samples[:0]
	c.windowStart = time.Now()
}

// -----------------------------------------------------------------------------
// Statistical Analysis
// -----------------------------------------------------------------------------

// TTestResult holds the results of a t-test.
type TTestResult struct {
	// TStatistic is the computed t-statistic.
	TStatistic float64

	// PValue is the two-tailed p-value.
	PValue float64

	// DegreesOfFreedom is the Welch-Satterthwaite df.
	DegreesOfFreedom float64

	// Significant is true if PValue < significance level.
	Significant bool

	// SignificanceLevel is the alpha used (e.g., 0.05).
	SignificanceLevel float64
}

// WelchTTest performs Welch's t-test for two sample sets.
//
// Description:
//
//	Welch's t-test is used when the two samples may have unequal variances.
//	It does not assume equal population variances, making it more robust
//	than Student's t-test.
//
// Inputs:
//   - samples1: First sample set. Must have at least 2 samples.
//   - samples2: Second sample set. Must have at least 2 samples.
//   - alpha: Significance level (e.g., 0.05 for 95% confidence).
//
// Outputs:
//   - *TTestResult: Test results with t-statistic, p-value, and significance.
//   - error: Non-nil if samples are insufficient.
//
// Thread Safety: This function is stateless and safe for concurrent use.
func WelchTTest(samples1, samples2 []time.Duration, alpha float64) (*TTestResult, error) {
	if len(samples1) < 2 || len(samples2) < 2 {
		return nil, ErrInsufficientSamples
	}

	mean1 := mean(samples1)
	mean2 := mean(samples2)

	var1 := variance(samples1, mean1)
	var2 := variance(samples2, mean2)

	n1 := float64(len(samples1))
	n2 := float64(len(samples2))

	// Standard error
	se := math.Sqrt(var1/n1 + var2/n2)
	if se == 0 {
		return nil, ErrZeroVariance
	}

	// t-statistic
	tStat := (mean1 - mean2) / se

	// Degrees of freedom (Welch-Satterthwaite equation)
	num := math.Pow(var1/n1+var2/n2, 2)
	denom := math.Pow(var1/n1, 2)/(n1-1) + math.Pow(var2/n2, 2)/(n2-1)
	if denom == 0 {
		return nil, ErrZeroVariance
	}
	df := num / denom

	// Calculate p-value
	pValue := tDistributionPValue(math.Abs(tStat), df)

	return &TTestResult{
		TStatistic:        tStat,
		PValue:            pValue,
		DegreesOfFreedom:  df,
		Significant:       pValue < alpha,
		SignificanceLevel: alpha,
	}, nil
}

// ConfidenceInterval represents a statistical confidence interval.
type ConfidenceInterval struct {
	// Lower is the lower bound.
	Lower float64

	// Upper is the upper bound.
	Upper float64

	// Level is the confidence level (e.g., 0.95).
	Level float64

	// Center is the point estimate (mean).
	Center float64
}

// Contains returns true if the interval contains the value.
func (ci *ConfidenceInterval) Contains(v float64) bool {
	return v >= ci.Lower && v <= ci.Upper
}

// Width returns the interval width.
func (ci *ConfidenceInterval) Width() float64 {
	return ci.Upper - ci.Lower
}

// CalculateCI calculates a confidence interval for the mean difference.
//
// Description:
//
//	Calculates a confidence interval for the difference between two means
//	using Welch's method (for unequal variances).
//
// Inputs:
//   - samples1: First sample set. Must have at least 2 samples.
//   - samples2: Second sample set. Must have at least 2 samples.
//   - level: Confidence level (e.g., 0.95 for 95% CI).
//
// Outputs:
//   - *ConfidenceInterval: The confidence interval for mean1 - mean2.
//   - error: Non-nil if samples are insufficient.
//
// Thread Safety: This function is stateless and safe for concurrent use.
func CalculateCI(samples1, samples2 []time.Duration, level float64) (*ConfidenceInterval, error) {
	if len(samples1) < 2 || len(samples2) < 2 {
		return nil, ErrInsufficientSamples
	}

	mean1 := mean(samples1)
	mean2 := mean(samples2)
	meanDiff := mean1 - mean2

	var1 := variance(samples1, mean1)
	var2 := variance(samples2, mean2)

	n1 := float64(len(samples1))
	n2 := float64(len(samples2))

	// Standard error of the difference
	se := math.Sqrt(var1/n1 + var2/n2)
	if se == 0 {
		// No variance, return point estimate
		return &ConfidenceInterval{
			Lower:  meanDiff,
			Upper:  meanDiff,
			Level:  level,
			Center: meanDiff,
		}, nil
	}

	// Degrees of freedom (Welch-Satterthwaite)
	num := math.Pow(var1/n1+var2/n2, 2)
	denom := math.Pow(var1/n1, 2)/(n1-1) + math.Pow(var2/n2, 2)/(n2-1)
	df := num / denom

	// Get t critical value
	tCrit := tCriticalValue(int(math.Round(df)), level)

	margin := tCrit * se
	return &ConfidenceInterval{
		Lower:  meanDiff - margin,
		Upper:  meanDiff + margin,
		Level:  level,
		Center: meanDiff,
	}, nil
}

// EffectSize calculates Cohen's d effect size.
//
// Description:
//
//	Cohen's d measures the standardized difference between two means.
//	Uses the pooled standard deviation for the denominator.
//
// Inputs:
//   - samples1: First sample set. Must not be empty.
//   - samples2: Second sample set. Must not be empty.
//
// Outputs:
//   - float64: Cohen's d value. Positive means samples1 > samples2.
//   - error: Non-nil if samples are empty or have zero pooled variance.
//
// Thread Safety: This function is stateless and safe for concurrent use.
func EffectSize(samples1, samples2 []time.Duration) (float64, error) {
	if len(samples1) == 0 || len(samples2) == 0 {
		return 0, ErrInsufficientSamples
	}

	mean1 := mean(samples1)
	mean2 := mean(samples2)

	var1 := variance(samples1, mean1)
	var2 := variance(samples2, mean2)

	n1 := float64(len(samples1))
	n2 := float64(len(samples2))

	// Pooled standard deviation
	pooledVar := ((n1-1)*var1 + (n2-1)*var2) / (n1 + n2 - 2)
	pooledStdDev := math.Sqrt(pooledVar)

	if pooledStdDev == 0 {
		return 0, ErrZeroVariance
	}

	return (mean1 - mean2) / pooledStdDev, nil
}

// EffectCategory categorizes effect sizes using Cohen's conventions.
type EffectCategory int

const (
	// EffectNegligible indicates |d| < 0.2
	EffectNegligible EffectCategory = iota
	// EffectSmall indicates 0.2 <= |d| < 0.5
	EffectSmall
	// EffectMedium indicates 0.5 <= |d| < 0.8
	EffectMedium
	// EffectLarge indicates |d| >= 0.8
	EffectLarge
)

// String returns the string representation.
func (e EffectCategory) String() string {
	switch e {
	case EffectNegligible:
		return "negligible"
	case EffectSmall:
		return "small"
	case EffectMedium:
		return "medium"
	case EffectLarge:
		return "large"
	default:
		return "unknown"
	}
}

// CategorizeEffect returns the category for a Cohen's d value.
func CategorizeEffect(d float64) EffectCategory {
	absD := math.Abs(d)
	switch {
	case absD < 0.2:
		return EffectNegligible
	case absD < 0.5:
		return EffectSmall
	case absD < 0.8:
		return EffectMedium
	default:
		return EffectLarge
	}
}

// -----------------------------------------------------------------------------
// Power Analysis
// -----------------------------------------------------------------------------

// PowerAnalysis holds results of statistical power calculation.
type PowerAnalysis struct {
	// Power is the probability of detecting a true effect (1 - beta).
	Power float64

	// RequiredSampleSize is the samples needed per group for desired power.
	RequiredSampleSize int

	// EffectSize is the target effect size used in calculation.
	EffectSize float64

	// Alpha is the significance level used.
	Alpha float64

	// TargetPower is the desired power level (e.g., 0.8).
	TargetPower float64
}

// CalculatePower estimates statistical power for the current sample sizes.
//
// Description:
//
//	Power is the probability of correctly rejecting the null hypothesis
//	when there is a true effect. Higher power (e.g., 0.8 or 0.9) means
//	the experiment is more likely to detect real differences.
//
// Inputs:
//   - n1: Sample size for group 1.
//   - n2: Sample size for group 2.
//   - effectSize: Expected Cohen's d effect size.
//   - alpha: Significance level (e.g., 0.05).
//
// Outputs:
//   - float64: Statistical power (0 to 1).
//
// Thread Safety: This function is stateless and safe for concurrent use.
func CalculatePower(n1, n2 int, effectSize, alpha float64) float64 {
	if n1 < 2 || n2 < 2 {
		return 0
	}

	// Harmonic mean of sample sizes for unequal groups
	nHarmonic := 2 * float64(n1) * float64(n2) / float64(n1+n2)

	// Non-centrality parameter
	ncp := effectSize * math.Sqrt(nHarmonic/2)

	// Degrees of freedom
	df := float64(n1 + n2 - 2)

	// Critical value for alpha
	tCrit := tCriticalValue(int(df), 1-alpha)

	// Power approximation using non-central t-distribution
	// Using normal approximation for simplicity
	power := 1 - normalCDF(tCrit-ncp)

	// Clamp to valid range
	if power < 0 {
		power = 0
	}
	if power > 1 {
		power = 1
	}

	return power
}

// RequiredSampleSize calculates samples needed for desired power.
//
// Description:
//
//	Determines the minimum sample size per group needed to achieve
//	a specified power level for detecting a given effect size.
//
// Inputs:
//   - effectSize: Expected Cohen's d effect size.
//   - alpha: Significance level (e.g., 0.05).
//   - power: Desired power (e.g., 0.8 for 80% power).
//
// Outputs:
//   - int: Required sample size per group.
//
// Thread Safety: This function is stateless and safe for concurrent use.
func RequiredSampleSize(effectSize, alpha, power float64) int {
	if effectSize == 0 {
		return math.MaxInt32 // Infinite samples needed for zero effect
	}

	// Using Cohen's formula for two-sample t-test
	// n = 2 * ((z_alpha + z_power) / d)^2

	zAlpha := zScore(1 - alpha/2) // Two-tailed
	zPower := zScore(power)

	n := 2 * math.Pow((zAlpha+zPower)/effectSize, 2)

	// Add 1 and ceiling for conservative estimate
	return int(math.Ceil(n)) + 1
}

// -----------------------------------------------------------------------------
// Bootstrap Methods
// -----------------------------------------------------------------------------

// BootstrapCI calculates a bootstrap confidence interval.
//
// Description:
//
//	Uses bootstrap resampling to construct a confidence interval
//	for the mean difference. More robust than parametric methods
//	when sample distributions are non-normal.
//
// Inputs:
//   - samples1: First sample set. Must have at least 2 samples.
//   - samples2: Second sample set. Must have at least 2 samples.
//   - level: Confidence level (e.g., 0.95).
//   - nBootstrap: Number of bootstrap iterations (recommend 10000+).
//
// Outputs:
//   - *ConfidenceInterval: Bootstrap percentile interval.
//   - error: Non-nil if samples are insufficient.
//
// Thread Safety: This function is stateless and safe for concurrent use.
func BootstrapCI(samples1, samples2 []time.Duration, level float64, nBootstrap int) (*ConfidenceInterval, error) {
	if len(samples1) < 2 || len(samples2) < 2 {
		return nil, ErrInsufficientSamples
	}
	if nBootstrap < 100 {
		nBootstrap = 100
	}

	// Use linear congruential generator for deterministic results
	seed := uint64(12345)
	lcg := func() uint64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return seed
	}

	diffs := make([]float64, nBootstrap)

	for i := 0; i < nBootstrap; i++ {
		// Resample with replacement
		boot1 := resample(samples1, lcg)
		boot2 := resample(samples2, lcg)

		mean1 := mean(boot1)
		mean2 := mean(boot2)
		diffs[i] = mean1 - mean2
	}

	// Sort differences
	sort.Float64s(diffs)

	// Percentile method
	alphaLower := (1 - level) / 2
	alphaUpper := 1 - alphaLower

	lowerIdx := int(alphaLower * float64(nBootstrap))
	upperIdx := int(alphaUpper * float64(nBootstrap))

	if lowerIdx < 0 {
		lowerIdx = 0
	}
	if upperIdx >= nBootstrap {
		upperIdx = nBootstrap - 1
	}

	return &ConfidenceInterval{
		Lower:  diffs[lowerIdx],
		Upper:  diffs[upperIdx],
		Level:  level,
		Center: mean(samples1) - mean(samples2),
	}, nil
}

// resample creates a bootstrap sample using the provided RNG.
func resample(samples []time.Duration, rng func() uint64) []time.Duration {
	n := len(samples)
	result := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		idx := int(rng() % uint64(n))
		result[i] = samples[idx]
	}
	return result
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// mean calculates the arithmetic mean.
func mean(samples []time.Duration) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s)
	}
	return sum / float64(len(samples))
}

// variance calculates population variance.
func variance(samples []time.Duration, mean float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		diff := float64(s) - mean
		sumSq += diff * diff
	}
	return sumSq / float64(len(samples))
}

// normalCDF approximates the standard normal CDF.
func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt(2)))
}

// zScore returns the z-score for a given percentile.
func zScore(p float64) float64 {
	// Approximation using rational approximation
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}

	// Use inverse error function
	// For p in (0,1): z = sqrt(2) * erfinv(2p - 1)
	return math.Sqrt(2) * math.Erfinv(2*p-1)
}

// tDistributionPValue approximates the two-tailed p-value.
func tDistributionPValue(t, df float64) float64 {
	if df <= 0 {
		return 1
	}

	// For large df, use normal approximation
	if df >= 30 {
		return 2 * (1 - normalCDF(t))
	}

	// For smaller df, use approximation
	// Adjust t-statistic to approximate t-distribution
	adjustedT := t * math.Sqrt(df/(df-2+0.001))
	pValue := 2 * (1 - normalCDF(adjustedT))

	// Clamp to valid range
	if pValue < 0 {
		pValue = 0
	}
	if pValue > 1 {
		pValue = 1
	}

	return pValue
}

// tCriticalValue returns approximate t critical value for two-tailed test.
func tCriticalValue(df int, level float64) float64 {
	// Pre-computed values for common cases
	if df >= 30 {
		switch {
		case level >= 0.99:
			return 2.576
		case level >= 0.95:
			return 1.96
		case level >= 0.90:
			return 1.645
		default:
			return 1.96
		}
	}

	// Table lookup for small df
	t95 := []float64{12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
		2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
		2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042}
	t99 := []float64{63.657, 9.925, 5.841, 4.604, 4.032, 3.707, 3.499, 3.355, 3.250, 3.169,
		3.106, 3.055, 3.012, 2.977, 2.947, 2.921, 2.898, 2.878, 2.861, 2.845,
		2.831, 2.819, 2.807, 2.797, 2.787, 2.779, 2.771, 2.763, 2.756, 2.750}

	if df < 1 {
		df = 1
	}

	switch {
	case level >= 0.99:
		return t99[df-1]
	case level >= 0.95:
		return t95[df-1]
	default:
		return t95[df-1] * 0.85 // Approximate for 90%
	}
}
