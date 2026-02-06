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
	"errors"
	"math"
	"sort"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNoSamples indicates that no samples were collected.
	ErrNoSamples = errors.New("no samples collected")

	// ErrInvalidConfig indicates an invalid benchmark configuration.
	ErrInvalidConfig = errors.New("invalid benchmark configuration")

	// ErrBenchmarkFailed indicates that the benchmark failed to run.
	ErrBenchmarkFailed = errors.New("benchmark failed")
)

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// Config holds benchmark configuration.
//
// Description:
//
//	Config controls all aspects of a benchmark run including iteration counts,
//	timeouts, memory collection, and outlier handling. Use DefaultConfig() to
//	get sensible defaults, then override specific fields as needed.
//
// Thread Safety: Safe for concurrent read access after initialization.
type Config struct {
	// Iterations is the number of benchmark iterations to run.
	// Default: 1000
	Iterations int

	// Warmup is the number of warmup iterations before measurement.
	// Default: 100
	Warmup int

	// Cooldown is the duration to wait between warmup and measurement.
	// Default: 100ms
	Cooldown time.Duration

	// Timeout is the maximum time for the entire benchmark.
	// Default: 5 minutes
	Timeout time.Duration

	// IterationTimeout is the maximum time for a single iteration.
	// Default: 10 seconds
	IterationTimeout time.Duration

	// CollectMemory enables memory statistics collection.
	// Default: true
	CollectMemory bool

	// RemoveOutliers removes statistical outliers from results.
	// Default: true
	RemoveOutliers bool

	// OutlierThreshold is the IQR multiplier for outlier detection.
	// Default: 1.5
	OutlierThreshold float64

	// Parallelism is the number of concurrent benchmark goroutines.
	// Default: 1 (sequential)
	Parallelism int

	// InputGenerator generates input for each iteration.
	// If nil, uses the component's default generator.
	InputGenerator func() any
}

// DefaultConfig returns a configuration with default values.
//
// Description:
//
//	Creates a new Config with sensible defaults for most benchmarking
//	scenarios. Values can be overridden after creation.
//
// Outputs:
//   - *Config: Configuration with default values. Never nil.
//
// Example:
//
//	config := DefaultConfig()
//	config.Iterations = 10000  // Override defaults
//	config.Parallelism = 4
func DefaultConfig() *Config {
	return &Config{
		Iterations:       1000,
		Warmup:           100,
		Cooldown:         100 * time.Millisecond,
		Timeout:          5 * time.Minute,
		IterationTimeout: 10 * time.Second,
		CollectMemory:    true,
		RemoveOutliers:   true,
		OutlierThreshold: 1.5,
		Parallelism:      1,
	}
}

// Validate checks that the configuration is valid.
//
// Description:
//
//	Validates all configuration fields to ensure they have acceptable
//	values. Must be called before using the configuration.
//
// Outputs:
//   - error: Non-nil if configuration is invalid, with message indicating
//     which field failed validation.
//
// Example:
//
//	config := DefaultConfig()
//	config.Iterations = -1  // Invalid
//	if err := config.Validate(); err != nil {
//	    log.Fatal(err)  // "iterations must be positive"
//	}
func (c *Config) Validate() error {
	if c.Iterations <= 0 {
		return errors.New("iterations must be positive")
	}
	if c.Warmup < 0 {
		return errors.New("warmup must be non-negative")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if c.IterationTimeout <= 0 {
		return errors.New("iteration timeout must be positive")
	}
	if c.OutlierThreshold <= 0 {
		return errors.New("outlier threshold must be positive")
	}
	if c.Parallelism <= 0 {
		return errors.New("parallelism must be positive")
	}
	return nil
}

// -----------------------------------------------------------------------------
// Results
// -----------------------------------------------------------------------------

// Result holds the results of a benchmark run.
//
// Description:
//
//	Result contains comprehensive statistics from a benchmark execution
//	including latency percentiles, throughput, memory usage, and error rates.
//	The raw samples are preserved for custom analysis.
//
// Thread Safety: Safe for concurrent read access after creation.
type Result struct {
	// Name is the name of the benchmarked component.
	Name string

	// Iterations is the number of iterations run.
	Iterations int

	// TotalDuration is the total time for all iterations.
	TotalDuration time.Duration

	// Latency holds latency statistics.
	Latency LatencyStats

	// Memory holds memory statistics (if collected).
	Memory *MemoryStats

	// Throughput holds throughput statistics.
	Throughput ThroughputStats

	// Errors is the number of iterations that resulted in errors.
	Errors int

	// ErrorRate is Errors / Iterations.
	ErrorRate float64

	// Timestamp is when the benchmark was run (Unix milliseconds UTC).
	Timestamp int64

	// Config is the configuration used for this benchmark.
	Config *Config

	// RawSamples holds the raw latency samples (before outlier removal).
	RawSamples []time.Duration

	// Samples holds the latency samples used for statistics.
	Samples []time.Duration
}

// LatencyStats holds latency percentile statistics.
//
// Description:
//
//	LatencyStats provides comprehensive latency analysis including
//	min/max, mean/median, standard deviation, and percentiles.
//	All percentiles are calculated using linear interpolation.
//
// Thread Safety: Safe for concurrent read access after creation.
type LatencyStats struct {
	// Min is the minimum latency observed.
	Min time.Duration

	// Max is the maximum latency observed.
	Max time.Duration

	// Mean is the arithmetic mean of latencies.
	Mean time.Duration

	// Median is the 50th percentile (P50).
	Median time.Duration

	// StdDev is the standard deviation.
	StdDev time.Duration

	// Variance is the variance (StdDev^2).
	Variance float64

	// P50 is the 50th percentile.
	P50 time.Duration

	// P90 is the 90th percentile.
	P90 time.Duration

	// P95 is the 95th percentile.
	P95 time.Duration

	// P99 is the 99th percentile.
	P99 time.Duration

	// P999 is the 99.9th percentile.
	P999 time.Duration
}

// MemoryStats holds memory usage statistics.
//
// Description:
//
//	MemoryStats captures heap allocation changes and GC behavior
//	during the benchmark run. Useful for identifying memory leaks
//	and understanding allocation patterns.
//
// Thread Safety: Safe for concurrent read access after creation.
type MemoryStats struct {
	// AllocBytes is the total bytes allocated per iteration (mean).
	AllocBytes uint64

	// AllocObjects is the total objects allocated per iteration (mean).
	AllocObjects uint64

	// HeapAllocBefore is the heap allocation before the benchmark.
	HeapAllocBefore uint64

	// HeapAllocAfter is the heap allocation after the benchmark.
	HeapAllocAfter uint64

	// HeapAllocDelta is the change in heap allocation.
	HeapAllocDelta int64

	// GCPauses is the number of GC pauses during the benchmark.
	GCPauses uint32

	// GCPauseTotal is the total GC pause time.
	GCPauseTotal time.Duration
}

// ThroughputStats holds throughput statistics.
//
// Description:
//
//	ThroughputStats measures the rate of operations, optionally
//	including byte and item throughput if the benchmark provides
//	this information.
//
// Thread Safety: Safe for concurrent read access after creation.
type ThroughputStats struct {
	// OpsPerSecond is the number of operations per second.
	OpsPerSecond float64

	// BytesPerSecond is the throughput in bytes per second (if applicable).
	BytesPerSecond float64

	// ItemsPerSecond is the throughput in items per second (if applicable).
	ItemsPerSecond float64
}

// -----------------------------------------------------------------------------
// Comparison
// -----------------------------------------------------------------------------

// ComparisonResult holds the results of comparing two or more benchmarks.
//
// Description:
//
//	ComparisonResult provides statistical comparison between multiple
//	components, determining if performance differences are significant.
//	Uses Welch's t-test for significance and Cohen's d for effect size.
//
// Thread Safety: Safe for concurrent read access after creation.
type ComparisonResult struct {
	// Results holds the individual benchmark results keyed by name.
	Results map[string]*Result

	// Winner is the name of the fastest component (if statistically significant).
	// Empty if no clear winner.
	Winner string

	// Speedup is the ratio of slowest to fastest (e.g., 2.0 means 2x faster).
	Speedup float64

	// Significant indicates whether the difference is statistically significant.
	Significant bool

	// PValue is the p-value from the statistical test.
	PValue float64

	// ConfidenceLevel is the confidence level used (e.g., 0.95).
	ConfidenceLevel float64

	// EffectSize is Cohen's d effect size.
	EffectSize float64

	// EffectSizeCategory categorizes the effect size.
	EffectSizeCategory EffectSizeCategory

	// Ranking is the components ranked from fastest to slowest.
	Ranking []string
}

// EffectSizeCategory categorizes effect sizes using Cohen's conventions.
//
// Description:
//
//	Effect sizes help interpret the practical significance of differences.
//	Cohen's d thresholds: negligible (<0.2), small (0.2-0.5), medium (0.5-0.8),
//	large (≥0.8).
type EffectSizeCategory int

const (
	// EffectNegligible indicates Cohen's d < 0.2
	EffectNegligible EffectSizeCategory = iota
	// EffectSmall indicates Cohen's d between 0.2 and 0.5
	EffectSmall
	// EffectMedium indicates Cohen's d between 0.5 and 0.8
	EffectMedium
	// EffectLarge indicates Cohen's d >= 0.8
	EffectLarge
)

// String returns the string representation of the effect size category.
//
// Outputs:
//   - string: Human-readable category name.
func (e EffectSizeCategory) String() string {
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

// CategorizeEffectSize returns the category for a given Cohen's d value.
//
// Description:
//
//	Categorizes the effect size using Cohen's conventional thresholds.
//	Uses the absolute value of d, so direction doesn't affect category.
//
// Inputs:
//   - d: Cohen's d value (can be positive or negative).
//
// Outputs:
//   - EffectSizeCategory: The corresponding category.
//
// Example:
//
//	cat := CategorizeEffectSize(0.3)  // EffectSmall
//	cat := CategorizeEffectSize(-0.9) // EffectLarge (uses absolute value)
func CategorizeEffectSize(d float64) EffectSizeCategory {
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
// Statistics Functions
// -----------------------------------------------------------------------------

// CalculateLatencyStats computes latency statistics from samples.
//
// Description:
//
//	Computes comprehensive statistics including min, max, mean, median,
//	standard deviation, variance, and percentiles (P50, P90, P95, P99, P999).
//	Percentiles are calculated using linear interpolation.
//
// Inputs:
//   - samples: Duration samples. Must not be empty.
//
// Outputs:
//   - LatencyStats: Computed statistics with all fields populated.
//   - error: ErrNoSamples if samples is empty.
//
// Thread Safety: This function is stateless and safe for concurrent use.
//
// Example:
//
//	samples := []time.Duration{10*time.Millisecond, 20*time.Millisecond, 30*time.Millisecond}
//	stats, err := CalculateLatencyStats(samples)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("P99: %v\n", stats.P99)
//
// Assumptions:
//   - All sample values are non-negative durations.
func CalculateLatencyStats(samples []time.Duration) (LatencyStats, error) {
	if len(samples) == 0 {
		return LatencyStats{}, ErrNoSamples
	}

	// Sort samples for percentile calculation
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	stats := LatencyStats{
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		Median: percentile(sorted, 0.5),
		P50:    percentile(sorted, 0.5),
		P90:    percentile(sorted, 0.9),
		P95:    percentile(sorted, 0.95),
		P99:    percentile(sorted, 0.99),
		P999:   percentile(sorted, 0.999),
	}

	// Calculate mean
	var sum time.Duration
	for _, s := range samples {
		sum += s
	}
	stats.Mean = sum / time.Duration(len(samples))

	// Calculate variance and standard deviation
	var sumSquaredDiff float64
	meanFloat := float64(stats.Mean)
	for _, s := range samples {
		diff := float64(s) - meanFloat
		sumSquaredDiff += diff * diff
	}
	stats.Variance = sumSquaredDiff / float64(len(samples))
	stats.StdDev = time.Duration(math.Sqrt(stats.Variance))

	return stats, nil
}

// percentile calculates the p-th percentile of sorted samples using linear interpolation.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	// Linear interpolation
	index := p * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	fraction := index - float64(lower)
	return time.Duration(float64(sorted[lower])*(1-fraction) + float64(sorted[upper])*fraction)
}

// RemoveOutliers removes outliers using the IQR method.
//
// Description:
//
//	Uses the Interquartile Range (IQR) method to identify and remove outliers.
//	Values outside [Q1 - threshold*IQR, Q3 + threshold*IQR] are removed.
//	If removing outliers would remove more than half the samples, returns
//	the original samples unchanged.
//
// Inputs:
//   - samples: Duration samples. Small samples (< 4) are returned unchanged.
//   - threshold: IQR multiplier (typically 1.5 for mild outliers, 3.0 for extreme).
//
// Outputs:
//   - []time.Duration: Samples with outliers removed.
//
// Thread Safety: This function is stateless and safe for concurrent use.
//
// Example:
//
//	samples := []time.Duration{10*time.Millisecond, 11*time.Millisecond, 1000*time.Millisecond}
//	filtered := RemoveOutliers(samples, 1.5)
//	// 1000ms outlier removed
func RemoveOutliers(samples []time.Duration, threshold float64) []time.Duration {
	if len(samples) < 4 {
		return samples
	}

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	q1 := percentile(sorted, 0.25)
	q3 := percentile(sorted, 0.75)
	iqr := q3 - q1

	lowerBound := q1 - time.Duration(threshold*float64(iqr))
	upperBound := q3 + time.Duration(threshold*float64(iqr))

	var filtered []time.Duration
	for _, s := range samples {
		if s >= lowerBound && s <= upperBound {
			filtered = append(filtered, s)
		}
	}

	// Don't remove too many samples
	if len(filtered) < len(samples)/2 {
		return samples
	}

	return filtered
}

// CalculateCohensD calculates Cohen's d effect size between two sample sets.
//
// Description:
//
//	Cohen's d measures the standardized difference between two means.
//	Uses the pooled standard deviation for the denominator.
//	Positive d indicates samples1 > samples2, negative indicates samples1 < samples2.
//
// Inputs:
//   - samples1: First sample set. Must not be empty.
//   - samples2: Second sample set. Must not be empty.
//
// Outputs:
//   - float64: Cohen's d value. Returns 0 if either sample is empty or
//     if pooled standard deviation is 0.
//
// Thread Safety: This function is stateless and safe for concurrent use.
//
// Example:
//
//	fast := []time.Duration{10*time.Millisecond, 11*time.Millisecond}
//	slow := []time.Duration{100*time.Millisecond, 101*time.Millisecond}
//	d := CalculateCohensD(fast, slow)  // Large negative d (fast is faster)
func CalculateCohensD(samples1, samples2 []time.Duration) float64 {
	if len(samples1) == 0 || len(samples2) == 0 {
		return 0
	}

	mean1 := calculateMean(samples1)
	mean2 := calculateMean(samples2)

	var1 := calculateVariance(samples1, mean1)
	var2 := calculateVariance(samples2, mean2)

	// Pooled standard deviation
	n1 := float64(len(samples1))
	n2 := float64(len(samples2))
	pooledVar := ((n1-1)*var1 + (n2-1)*var2) / (n1 + n2 - 2)
	pooledStdDev := math.Sqrt(pooledVar)

	if pooledStdDev == 0 {
		return 0
	}

	return (mean1 - mean2) / pooledStdDev
}

// calculateMean calculates the arithmetic mean of samples.
func calculateMean(samples []time.Duration) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s)
	}
	return sum / float64(len(samples))
}

// calculateVariance calculates the population variance of samples.
func calculateVariance(samples []time.Duration, mean float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSquaredDiff float64
	for _, s := range samples {
		diff := float64(s) - mean
		sumSquaredDiff += diff * diff
	}
	return sumSquaredDiff / float64(len(samples))
}

// WelchTTest performs Welch's t-test for two sample sets.
//
// Description:
//
//	Welch's t-test is used when the two samples have unequal variances
//	and/or unequal sample sizes. It does not assume equal population
//	variances, making it more robust than Student's t-test.
//
// Inputs:
//   - samples1: First sample set. Must have at least 2 samples.
//   - samples2: Second sample set. Must have at least 2 samples.
//
// Outputs:
//   - tStatistic: The t-statistic. Negative if samples1 < samples2.
//   - pValue: Approximate two-tailed p-value. Returns 1 if samples are
//     insufficient or have zero variance.
//
// Thread Safety: This function is stateless and safe for concurrent use.
//
// Example:
//
//	t, p := WelchTTest(fastSamples, slowSamples)
//	if p < 0.05 {
//	    fmt.Println("Significant difference at 95% confidence")
//	}
//
// Limitations:
//   - Uses normal approximation for p-value when df >= 30.
//   - For smaller df, uses a rough approximation. For precise p-values,
//     use a statistical library with t-distribution tables.
func WelchTTest(samples1, samples2 []time.Duration) (tStatistic float64, pValue float64) {
	if len(samples1) < 2 || len(samples2) < 2 {
		return 0, 1
	}

	mean1 := calculateMean(samples1)
	mean2 := calculateMean(samples2)

	var1 := calculateVariance(samples1, mean1)
	var2 := calculateVariance(samples2, mean2)

	n1 := float64(len(samples1))
	n2 := float64(len(samples2))

	// Standard error
	se := math.Sqrt(var1/n1 + var2/n2)
	if se == 0 {
		return 0, 1
	}

	// t-statistic
	tStatistic = (mean1 - mean2) / se

	// Degrees of freedom (Welch-Satterthwaite equation)
	num := math.Pow(var1/n1+var2/n2, 2)
	denom := math.Pow(var1/n1, 2)/(n1-1) + math.Pow(var2/n2, 2)/(n2-1)
	if denom == 0 {
		return tStatistic, 1
	}
	df := num / denom

	// Approximate p-value using normal distribution for large df
	// For more accurate results, use a t-distribution table or library
	if df >= 30 {
		// Use normal approximation
		pValue = 2 * normalCDF(-math.Abs(tStatistic))
	} else {
		// Rough approximation for smaller df
		pValue = 2 * normalCDF(-math.Abs(tStatistic)*math.Sqrt(df/(df-2)))
	}

	return tStatistic, pValue
}

// normalCDF approximates the cumulative distribution function for standard normal.
func normalCDF(x float64) float64 {
	// Approximation using error function
	return 0.5 * (1 + math.Erf(x/math.Sqrt(2)))
}

// ConfidenceInterval calculates the confidence interval for the mean.
//
// Description:
//
//	Calculates a symmetric confidence interval around the sample mean
//	using the standard error. For small samples (n < 30), uses t-distribution
//	critical values; for large samples, uses z-scores (normal distribution).
//
// Inputs:
//   - samples: Sample set. Must have at least 2 samples for meaningful interval.
//   - confidenceLevel: Confidence level (e.g., 0.95 for 95%). Supported: 0.90, 0.95, 0.99.
//
// Outputs:
//   - lower: Lower bound of the interval.
//   - upper: Upper bound of the interval.
//
// Thread Safety: This function is stateless and safe for concurrent use.
//
// Example:
//
//	lower, upper := ConfidenceInterval(samples, 0.95)
//	fmt.Printf("95%% CI: [%v, %v]\n", lower, upper)
func ConfidenceInterval(samples []time.Duration, confidenceLevel float64) (lower, upper time.Duration) {
	if len(samples) < 2 {
		if len(samples) == 1 {
			return samples[0], samples[0]
		}
		return 0, 0
	}

	mean := calculateMean(samples)
	variance := calculateVariance(samples, mean)
	stdErr := math.Sqrt(variance / float64(len(samples)))

	n := len(samples)
	df := n - 1

	// Get critical value based on sample size
	var criticalValue float64
	if n >= 30 {
		// Use z-scores for large samples
		switch {
		case confidenceLevel >= 0.99:
			criticalValue = 2.576
		case confidenceLevel >= 0.95:
			criticalValue = 1.96
		case confidenceLevel >= 0.90:
			criticalValue = 1.645
		default:
			criticalValue = 1.96
		}
	} else {
		// Use t-distribution critical values for small samples
		criticalValue = tCriticalValue(df, confidenceLevel)
	}

	margin := criticalValue * stdErr
	return time.Duration(mean - margin), time.Duration(mean + margin)
}

// tCriticalValue returns the t-distribution critical value for two-tailed tests.
//
// Description:
//
//	Returns approximate t-critical values for common confidence levels
//	and degrees of freedom. Uses interpolation for non-standard df values.
//
// Inputs:
//   - df: Degrees of freedom (n-1 for sample CI).
//   - confidenceLevel: Confidence level (0.90, 0.95, or 0.99).
//
// Outputs:
//   - float64: Critical value for two-tailed test.
func tCriticalValue(df int, confidenceLevel float64) float64 {
	// Pre-computed t-critical values for common df values
	// Two-tailed, alpha = 1 - confidenceLevel
	t90 := []float64{6.314, 2.920, 2.353, 2.132, 2.015, 1.943, 1.895, 1.860, 1.833, 1.812,
		1.796, 1.782, 1.771, 1.761, 1.753, 1.746, 1.740, 1.734, 1.729, 1.725,
		1.721, 1.717, 1.714, 1.711, 1.708, 1.706, 1.703, 1.701, 1.699, 1.697}
	t95 := []float64{12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
		2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
		2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042}
	t99 := []float64{63.657, 9.925, 5.841, 4.604, 4.032, 3.707, 3.499, 3.355, 3.250, 3.169,
		3.106, 3.055, 3.012, 2.977, 2.947, 2.921, 2.898, 2.878, 2.861, 2.845,
		2.831, 2.819, 2.807, 2.797, 2.787, 2.779, 2.771, 2.763, 2.756, 2.750}

	// Select appropriate table
	var table []float64
	switch {
	case confidenceLevel >= 0.99:
		table = t99
	case confidenceLevel >= 0.95:
		table = t95
	default:
		table = t90
	}

	// Clamp df to valid range
	if df < 1 {
		df = 1
	}
	if df > 30 {
		// For df > 30, t approaches z
		switch {
		case confidenceLevel >= 0.99:
			return 2.576
		case confidenceLevel >= 0.95:
			return 1.96
		default:
			return 1.645
		}
	}

	return table[df-1]
}
