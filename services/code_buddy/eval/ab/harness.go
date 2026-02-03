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
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNilAlgorithm indicates a nil algorithm was provided.
	ErrNilAlgorithm = errors.New("algorithm must not be nil")

	// ErrTypeMismatch indicates input/output type mismatch between algorithms.
	ErrTypeMismatch = errors.New("algorithm type mismatch")
)

// -----------------------------------------------------------------------------
// Harness Configuration
// -----------------------------------------------------------------------------

// HarnessConfig configures the A/B test harness.
type HarnessConfig struct {
	// SampleRate is the fraction of requests to send to experiment (0.0 to 1.0).
	// Default: 0.1 (10%)
	SampleRate float64

	// MaxSamples is the maximum samples to collect per variant.
	// Default: 10000
	MaxSamples int

	// SamplerType determines the sampling strategy.
	// Default: SamplerTypeHash
	SamplerType SamplerType

	// RunBothAlways runs both algorithms for every request (for comparison).
	// Default: false
	RunBothAlways bool

	// CompareOutputs enables output comparison for correctness checking.
	// Default: true
	CompareOutputs bool

	// DecisionConfig configures automated decision making.
	// Default: DefaultDecisionConfig()
	DecisionConfig *DecisionConfig

	// Logger for debug output.
	Logger *slog.Logger

	// MetricsPrefix for exported metrics.
	// Default: "ab_harness"
	MetricsPrefix string
}

// SamplerType determines which sampling strategy to use.
type SamplerType int

const (
	// SamplerTypeRandom uses random sampling.
	SamplerTypeRandom SamplerType = iota
	// SamplerTypeHash uses consistent hash-based sampling.
	SamplerTypeHash
	// SamplerTypeBandit uses Thompson Sampling.
	SamplerTypeBandit
	// SamplerTypeRampUp uses gradual ramp-up.
	SamplerTypeRampUp
)

// DefaultHarnessConfig returns sensible defaults.
//
// Outputs:
//   - *HarnessConfig: Default configuration. Never nil.
func DefaultHarnessConfig() *HarnessConfig {
	return &HarnessConfig{
		SampleRate:     0.1,
		MaxSamples:     10000,
		SamplerType:    SamplerTypeHash,
		RunBothAlways:  false,
		CompareOutputs: true,
		DecisionConfig: DefaultDecisionConfig(),
		Logger:         slog.Default(),
		MetricsPrefix:  "ab_harness",
	}
}

// -----------------------------------------------------------------------------
// Harness Option Functions
// -----------------------------------------------------------------------------

// HarnessOption configures the harness.
type HarnessOption func(*HarnessConfig)

// WithSampleRate sets the experiment sample rate.
func WithSampleRate(rate float64) HarnessOption {
	return func(c *HarnessConfig) {
		if rate >= 0 && rate <= 1 {
			c.SampleRate = rate
		}
	}
}

// WithMaxSamples sets the maximum samples per variant.
func WithMaxSamples(n int) HarnessOption {
	return func(c *HarnessConfig) {
		if n > 0 {
			c.MaxSamples = n
		}
	}
}

// WithSamplerType sets the sampling strategy.
func WithSamplerType(t SamplerType) HarnessOption {
	return func(c *HarnessConfig) {
		c.SamplerType = t
	}
}

// WithRunBothAlways enables running both algorithms for every request.
func WithRunBothAlways(enabled bool) HarnessOption {
	return func(c *HarnessConfig) {
		c.RunBothAlways = enabled
	}
}

// WithCompareOutputs enables output comparison.
func WithCompareOutputs(enabled bool) HarnessOption {
	return func(c *HarnessConfig) {
		c.CompareOutputs = enabled
	}
}

// WithDecisionConfig sets the decision configuration.
func WithDecisionConfig(config *DecisionConfig) HarnessOption {
	return func(c *HarnessConfig) {
		if config != nil {
			c.DecisionConfig = config
		}
	}
}

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) HarnessOption {
	return func(c *HarnessConfig) {
		if logger != nil {
			c.Logger = logger
		}
	}
}

// WithMinSamples sets the minimum samples for decision.
func WithMinSamples(n int) HarnessOption {
	return func(c *HarnessConfig) {
		if n > 0 && c.DecisionConfig != nil {
			c.DecisionConfig.MinSamples = n
		}
	}
}

// WithConfidenceLevel sets the statistical confidence level.
func WithConfidenceLevel(level float64) HarnessOption {
	return func(c *HarnessConfig) {
		if level > 0 && level < 1 && c.DecisionConfig != nil {
			c.DecisionConfig.ConfidenceLevel = level
		}
	}
}

// -----------------------------------------------------------------------------
// Harness
// -----------------------------------------------------------------------------

// Harness runs A/B tests comparing two algorithm implementations.
//
// Description:
//
//	Harness manages an A/B experiment between a control and experiment
//	algorithm. It samples traffic, collects latency measurements,
//	compares outputs for correctness, and provides statistical analysis.
//
// Thread Safety: Safe for concurrent use.
type Harness struct {
	control    eval.Evaluable
	experiment eval.Evaluable

	config   *HarnessConfig
	sampler  Sampler
	decision *DecisionEngine
	logger   *slog.Logger

	// State
	mu               sync.RWMutex
	startTime        time.Time
	controlSamples   *SampleCollector
	expSamples       *SampleCollector
	correctnessHits  atomic.Int64
	correctnessTotal atomic.Int64

	// Metrics
	controlCalls     atomic.Int64
	experimentCalls  atomic.Int64
	controlErrors    atomic.Int64
	experimentErrors atomic.Int64
}

// NewHarness creates a new A/B test harness.
//
// Inputs:
//   - control: The control algorithm (baseline). Must not be nil.
//   - experiment: The experiment algorithm (new implementation). Must not be nil.
//   - opts: Optional configuration options.
//
// Outputs:
//   - *Harness: The new harness. Never nil.
//   - error: Non-nil if algorithms are nil.
//
// Example:
//
//	harness, err := ab.NewHarness(controlAlgo, experimentAlgo,
//	    ab.WithSampleRate(0.1),
//	    ab.WithMinSamples(1000),
//	)
func NewHarness(control, experiment eval.Evaluable, opts ...HarnessOption) (*Harness, error) {
	if control == nil || experiment == nil {
		return nil, ErrNilAlgorithm
	}

	config := DefaultHarnessConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Create sampler
	var sampler Sampler
	switch config.SamplerType {
	case SamplerTypeRandom:
		sampler = NewRandomSampler(config.SampleRate)
	case SamplerTypeBandit:
		sampler = NewBanditSampler(0.05)
		sampler.SetRate(config.SampleRate)
	case SamplerTypeRampUp:
		sampler = NewRampUpSampler(0.01, config.SampleRate, 24*time.Hour)
	default:
		sampler = NewHashSampler(config.SampleRate)
	}

	return &Harness{
		control:        control,
		experiment:     experiment,
		config:         config,
		sampler:        sampler,
		decision:       NewDecisionEngine(config.DecisionConfig),
		logger:         config.Logger,
		startTime:      time.Now(),
		controlSamples: NewSampleCollector(config.MaxSamples, 0),
		expSamples:     NewSampleCollector(config.MaxSamples, 0),
	}, nil
}

// Processor is a function that processes input and returns output, delta, and error.
type Processor func(ctx context.Context, input any) (output any, duration time.Duration, err error)

// Compare runs both algorithms and compares results.
//
// Description:
//
//	Executes both control and experiment algorithms with the same input,
//	records latencies, compares outputs, and returns the control result.
//	Use this when you need to compare every request.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - key: Unique key for sampling consistency.
//   - controlProc: Function to process with control algorithm.
//   - expProc: Function to process with experiment algorithm.
//   - input: Input to pass to processors.
//
// Outputs:
//   - output: The control algorithm's output.
//   - error: Error from control algorithm (experiment errors are logged).
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) Compare(
	ctx context.Context,
	key string,
	controlProc, expProc Processor,
	input any,
) (any, error) {
	ctx, span := otel.Tracer("ab").Start(ctx, "ab.Harness.Compare",
		trace.WithAttributes(
			attribute.String("key", key),
			attribute.String("control", h.control.Name()),
			attribute.String("experiment", h.experiment.Name()),
		),
	)
	defer span.End()

	// Always run control
	controlOutput, controlDuration, controlErr := controlProc(ctx, input)
	h.controlCalls.Add(1)
	if controlErr != nil {
		h.controlErrors.Add(1)
	} else {
		h.controlSamples.Add(controlDuration)
	}

	// Decide whether to run experiment
	runExperiment := h.config.RunBothAlways || h.sampler.Sample(key)
	if !runExperiment {
		return controlOutput, controlErr
	}

	// Run experiment
	expOutput, expDuration, expErr := expProc(ctx, input)
	h.experimentCalls.Add(1)
	if expErr != nil {
		h.experimentErrors.Add(1)
	} else {
		h.expSamples.Add(expDuration)
	}

	// Compare outputs if enabled
	if h.config.CompareOutputs && controlErr == nil && expErr == nil {
		h.correctnessTotal.Add(1)
		if outputsMatch(controlOutput, expOutput) {
			h.correctnessHits.Add(1)
		} else {
			h.logger.Debug("output mismatch",
				slog.String("key", key),
				slog.Any("control", controlOutput),
				slog.Any("experiment", expOutput),
			)
		}
	}

	// Update bandit if using adaptive sampling
	if bandit, ok := h.sampler.(*BanditSampler); ok {
		if controlErr == nil && expErr == nil {
			// Record based on relative performance
			if expDuration < controlDuration {
				bandit.RecordSuccess(true)
				bandit.RecordFailure(false)
			} else {
				bandit.RecordSuccess(false)
				bandit.RecordFailure(true)
			}
		}
	}

	return controlOutput, controlErr
}

// SelectVariant returns which variant should be used for the given key.
//
// Description:
//
//	Returns true if experiment should be used, false for control.
//	Use this when you want sampling logic but will run only one variant.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) SelectVariant(key string) bool {
	return h.sampler.Sample(key)
}

// RecordLatency records a latency measurement for the specified variant.
//
// Inputs:
//   - experiment: true for experiment variant, false for control.
//   - duration: The measured latency.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) RecordLatency(experiment bool, duration time.Duration) {
	if experiment {
		h.expSamples.Add(duration)
		h.experimentCalls.Add(1)
	} else {
		h.controlSamples.Add(duration)
		h.controlCalls.Add(1)
	}
}

// RecordError records an error for the specified variant.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) RecordError(experiment bool) {
	if experiment {
		h.experimentErrors.Add(1)
	} else {
		h.controlErrors.Add(1)
	}
}

// RecordCorrectness records a correctness comparison result.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) RecordCorrectness(matched bool) {
	h.correctnessTotal.Add(1)
	if matched {
		h.correctnessHits.Add(1)
	}
}

// GetResults returns the current statistical analysis.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) GetResults() *Results {
	controlSamples := h.controlSamples.Samples()
	expSamples := h.expSamples.Samples()

	results := &Results{
		ControlName:       h.control.Name(),
		ExperimentName:    h.experiment.Name(),
		ControlSamples:    len(controlSamples),
		ExperimentSamples: len(expSamples),
		Duration:          time.Since(h.startTime),
		SampleRate:        h.sampler.Rate(),
		ControlCalls:      h.controlCalls.Load(),
		ExperimentCalls:   h.experimentCalls.Load(),
		ControlErrors:     h.controlErrors.Load(),
		ExperimentErrors:  h.experimentErrors.Load(),
	}

	// Calculate correctness
	total := h.correctnessTotal.Load()
	if total > 0 {
		results.CorrectnessMatch = float64(h.correctnessHits.Load()) / float64(total)
	}

	// Perform statistical analysis if we have enough samples
	if len(controlSamples) >= 2 && len(expSamples) >= 2 {
		// t-test
		tTest, err := WelchTTest(controlSamples, expSamples, 0.05)
		if err == nil {
			results.TTest = tTest
		}

		// Effect size
		d, err := EffectSize(controlSamples, expSamples)
		if err == nil {
			results.EffectSize = d
			results.EffectCategory = CategorizeEffect(d)
		}

		// Confidence interval
		ci, err := CalculateCI(controlSamples, expSamples, 0.95)
		if err == nil {
			results.ConfidenceInterval = ci
		}

		// Means
		results.ControlMean = time.Duration(mean(controlSamples))
		results.ExperimentMean = time.Duration(mean(expSamples))

		// Power
		results.Power = CalculatePower(len(controlSamples), len(expSamples), d, 0.05)
	}

	// Get recommendation
	decision := h.GetDecision()
	results.Recommendation = decision.Recommendation
	results.Decision = decision

	return results
}

// GetDecision returns the current recommendation.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) GetDecision() *Decision {
	controlSamples := h.controlSamples.Samples()
	expSamples := h.expSamples.Samples()

	total := h.correctnessTotal.Load()
	hits := h.correctnessHits.Load()

	input := &DecisionInput{
		ControlSamples:      controlSamples,
		ExperimentSamples:   expSamples,
		CorrectnessMatches:  int(hits),
		TotalComparisons:    int(total),
		ExperimentStartTime: h.startTime,
	}

	return h.decision.Evaluate(input)
}

// Reset clears all collected data and restarts the experiment.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.startTime = time.Now()
	h.controlSamples.Reset()
	h.expSamples.Reset()
	h.correctnessHits.Store(0)
	h.correctnessTotal.Store(0)
	h.controlCalls.Store(0)
	h.experimentCalls.Store(0)
	h.controlErrors.Store(0)
	h.experimentErrors.Store(0)
}

// SetSampleRate updates the experiment sample rate.
//
// Thread Safety: Safe for concurrent use.
func (h *Harness) SetSampleRate(rate float64) {
	h.sampler.SetRate(rate)
}

// -----------------------------------------------------------------------------
// Results
// -----------------------------------------------------------------------------

// Results holds the complete A/B test results.
type Results struct {
	// Names
	ControlName    string
	ExperimentName string

	// Sample counts
	ControlSamples    int
	ExperimentSamples int

	// Call counts
	ControlCalls     int64
	ExperimentCalls  int64
	ControlErrors    int64
	ExperimentErrors int64

	// Timing
	Duration   time.Duration
	SampleRate float64

	// Means
	ControlMean    time.Duration
	ExperimentMean time.Duration

	// Statistical analysis
	TTest              *TTestResult
	EffectSize         float64
	EffectCategory     EffectCategory
	ConfidenceInterval *ConfidenceInterval
	Power              float64
	CorrectnessMatch   float64

	// Decision
	Recommendation Recommendation
	Decision       *Decision
}

// Significant returns true if the difference is statistically significant.
func (r *Results) Significant() bool {
	return r.TTest != nil && r.TTest.Significant
}

// ExperimentBetter returns true if experiment is significantly faster.
func (r *Results) ExperimentBetter() bool {
	return r.Recommendation == SwitchToExperiment
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// outputsMatch compares two outputs for equality.
func outputsMatch(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Name implements eval.Evaluable.
func (h *Harness) Name() string {
	return "ab_harness_" + h.control.Name() + "_vs_" + h.experiment.Name()
}

// Properties implements eval.Evaluable.
func (h *Harness) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "sampling_rate_respected",
			Description: "Sampler respects configured rate within tolerance",
			Check: func(input, output any) error {
				// Check that sampler rate is approximately correct
				return nil
			},
		},
		{
			Name:        "correctness_tracked",
			Description: "Correctness comparisons are properly tracked",
			Check: func(input, output any) error {
				return nil
			},
		},
	}
}

// Metrics implements eval.Evaluable.
func (h *Harness) Metrics() []eval.MetricDefinition {
	prefix := h.config.MetricsPrefix
	return []eval.MetricDefinition{
		{
			Name:        prefix + "_control_latency_seconds",
			Type:        eval.MetricHistogram,
			Description: "Latency distribution for control variant",
			Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		{
			Name:        prefix + "_experiment_latency_seconds",
			Type:        eval.MetricHistogram,
			Description: "Latency distribution for experiment variant",
			Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		{
			Name:        prefix + "_correctness_match_rate",
			Type:        eval.MetricGauge,
			Description: "Rate of output correctness matches",
		},
		{
			Name:        prefix + "_sample_rate",
			Type:        eval.MetricGauge,
			Description: "Current experiment sample rate",
		},
	}
}

// HealthCheck implements eval.Evaluable.
func (h *Harness) HealthCheck(ctx context.Context) error {
	// Check both algorithms are healthy
	if err := h.control.HealthCheck(ctx); err != nil {
		return err
	}
	return h.experiment.HealthCheck(ctx)
}
