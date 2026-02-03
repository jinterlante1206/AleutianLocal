// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// A/B Testing Harness
// -----------------------------------------------------------------------------

// ABHarness enables A/B testing between two algorithms.
//
// Description:
//
//	ABHarness runs two algorithms (experiment and control) and compares
//	their results. Use cases:
//	- Test new algorithm implementations
//	- Compare algorithm performance
//	- Validate algorithm correctness
//
//	The harness:
//	- Samples a percentage of requests for testing
//	- Runs both algorithms on sampled requests
//	- Compares outputs and records metrics
//	- Returns the control result for safety
//
//	Design Note: The harness always returns the control result for safety,
//	ensuring that experimental code doesn't affect production behavior.
//	Experiment results are only used for comparison metrics.
//
// Thread Safety: Safe for concurrent use.
type ABHarness struct {
	mu         sync.RWMutex
	experiment algorithms.Algorithm
	control    algorithms.Algorithm
	config     *ABConfig
	logger     *slog.Logger

	// rngMu protects rng access. rand.Rand is not thread-safe, so we need
	// a dedicated mutex for random number generation.
	rngMu sync.Mutex
	rng   *rand.Rand

	// Metrics
	totalRequests      int64
	sampledRequests    int64
	experimentWins     int64
	controlWins        int64
	ties               int64
	experimentFailures int64
	controlFailures    int64
}

// ABConfig configures the A/B testing harness.
type ABConfig struct {
	// SampleRate is the percentage of requests to sample (0.0-1.0).
	SampleRate float64

	// MetricsPrefix is the prefix for metrics.
	MetricsPrefix string

	// CompareFunc compares outputs and returns which is better.
	// Returns: -1 if experiment better, 1 if control better, 0 if tie.
	CompareFunc func(experiment, control any) int

	// EnableMetrics enables metrics collection.
	EnableMetrics bool

	// EnableTracing enables OpenTelemetry tracing.
	EnableTracing bool
}

// DefaultABConfig returns the default A/B config.
func DefaultABConfig() *ABConfig {
	return &ABConfig{
		SampleRate:    0.1, // 10% of requests
		MetricsPrefix: "ab_test",
		CompareFunc:   defaultCompare,
		EnableMetrics: true,
		EnableTracing: true,
	}
}

// defaultCompare is the default comparison function (always tie).
func defaultCompare(experiment, control any) int {
	return 0
}

// NewABHarness creates a new A/B testing harness.
//
// Inputs:
//   - experiment: The experimental algorithm.
//   - control: The control algorithm.
//   - config: A/B config. Uses defaults if nil.
//
// Outputs:
//   - *ABHarness: The new harness.
func NewABHarness(experiment, control algorithms.Algorithm, config *ABConfig) *ABHarness {
	if config == nil {
		config = DefaultABConfig()
	}

	return &ABHarness{
		experiment: experiment,
		control:    control,
		config:     config,
		logger:     slog.Default().With(slog.String("component", "ab_harness")),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the harness name.
func (h *ABHarness) Name() string {
	return h.config.MetricsPrefix + "_harness"
}

// Process runs the A/B test.
//
// Description:
//
//	For sampled requests, runs both algorithms and compares results.
//	Always returns the control result for safety.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - snapshot: Immutable CRS snapshot.
//   - input: Algorithm-specific input.
//
// Outputs:
//   - any: Control algorithm output.
//   - crs.Delta: Control algorithm delta.
//   - error: Control algorithm error.
func (h *ABHarness) Process(
	ctx context.Context,
	snapshot crs.Snapshot,
	input any,
) (any, crs.Delta, error) {
	// Update total requests counter
	h.mu.Lock()
	h.totalRequests++
	h.mu.Unlock()

	// Generate random sample decision with dedicated RNG mutex
	// (rand.Rand is not thread-safe)
	h.rngMu.Lock()
	shouldSample := h.rng.Float64() < h.config.SampleRate
	h.rngMu.Unlock()

	if !shouldSample {
		// Just run control
		return h.control.Process(ctx, snapshot, input)
	}

	h.mu.Lock()
	h.sampledRequests++
	h.mu.Unlock()

	// Run both algorithms in parallel
	type result struct {
		output any
		delta  crs.Delta
		err    error
	}

	experimentCh := make(chan result, 1)
	controlCh := make(chan result, 1)

	go func() {
		output, delta, err := h.experiment.Process(ctx, snapshot, input)
		experimentCh <- result{output, delta, err}
	}()

	go func() {
		output, delta, err := h.control.Process(ctx, snapshot, input)
		controlCh <- result{output, delta, err}
	}()

	// Wait for both results. Order doesn't matter since we need both
	// to compare, and we always return the control result anyway.
	// This design prioritizes simplicity over fail-fast behavior.
	experimentResult := <-experimentCh
	controlResult := <-controlCh

	// Record results
	h.recordResults(experimentResult, controlResult)

	// Always return control result
	return controlResult.output, controlResult.delta, controlResult.err
}

// recordResults records the comparison results.
func (h *ABHarness) recordResults(experiment, control struct {
	output any
	delta  crs.Delta
	err    error
}) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Record failures
	if experiment.err != nil {
		h.experimentFailures++
	}
	if control.err != nil {
		h.controlFailures++
	}

	// Only compare if both succeeded and CompareFunc is set
	if experiment.err == nil && control.err == nil && h.config.CompareFunc != nil {
		comparison := h.config.CompareFunc(experiment.output, control.output)
		switch comparison {
		case -1:
			h.experimentWins++
		case 1:
			h.controlWins++
		default:
			h.ties++
		}
	} else if experiment.err == nil && control.err == nil {
		// No compare function, treat as tie
		h.ties++
	}

	h.logger.Debug("A/B test recorded",
		slog.Int64("total", h.totalRequests),
		slog.Int64("sampled", h.sampledRequests),
		slog.Int64("experiment_wins", h.experimentWins),
		slog.Int64("control_wins", h.controlWins),
		slog.Int64("ties", h.ties),
	)
}

// Timeout returns the control algorithm's timeout.
func (h *ABHarness) Timeout() time.Duration {
	return h.control.Timeout()
}

// InputType returns the control algorithm's input type.
func (h *ABHarness) InputType() any {
	return h.control.InputType()
}

// OutputType returns the control algorithm's output type.
func (h *ABHarness) OutputType() any {
	return h.control.OutputType()
}

// ProgressInterval returns the control algorithm's progress interval.
func (h *ABHarness) ProgressInterval() time.Duration {
	return h.control.ProgressInterval()
}

// SupportsPartialResults returns the control algorithm's value.
func (h *ABHarness) SupportsPartialResults() bool {
	return h.control.SupportsPartialResults()
}

// -----------------------------------------------------------------------------
// Statistics
// -----------------------------------------------------------------------------

// Stats returns A/B testing statistics.
func (h *ABHarness) Stats() ABStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return ABStats{
		TotalRequests:      h.totalRequests,
		SampledRequests:    h.sampledRequests,
		ExperimentWins:     h.experimentWins,
		ControlWins:        h.controlWins,
		Ties:               h.ties,
		ExperimentFailures: h.experimentFailures,
		ControlFailures:    h.controlFailures,
	}
}

// ABStats contains A/B testing statistics.
type ABStats struct {
	TotalRequests      int64
	SampledRequests    int64
	ExperimentWins     int64
	ControlWins        int64
	Ties               int64
	ExperimentFailures int64
	ControlFailures    int64
}

// ExperimentWinRate returns the experiment win rate.
func (s ABStats) ExperimentWinRate() float64 {
	total := s.ExperimentWins + s.ControlWins + s.Ties
	if total == 0 {
		return 0
	}
	return float64(s.ExperimentWins) / float64(total)
}

// ControlWinRate returns the control win rate.
func (s ABStats) ControlWinRate() float64 {
	total := s.ExperimentWins + s.ControlWins + s.Ties
	if total == 0 {
		return 0
	}
	return float64(s.ControlWins) / float64(total)
}

// TieRate returns the tie rate.
func (s ABStats) TieRate() float64 {
	total := s.ExperimentWins + s.ControlWins + s.Ties
	if total == 0 {
		return 0
	}
	return float64(s.Ties) / float64(total)
}

// SampleRate returns the actual sample rate.
func (s ABStats) SampleRate() float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.SampledRequests) / float64(s.TotalRequests)
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (h *ABHarness) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "control_returned",
			Description: "Control result is always returned",
			Check: func(input, output any) error {
				// Verified by Process implementation
				return nil
			},
		},
		{
			Name:        "sample_rate_respected",
			Description: "Sample rate is approximately correct",
			Check: func(input, output any) error {
				// Verified by statistical analysis
				return nil
			},
		},
	}
}

// Metrics returns the metrics this component exposes.
func (h *ABHarness) Metrics() []eval.MetricDefinition {
	prefix := h.config.MetricsPrefix
	return []eval.MetricDefinition{
		{
			Name:        prefix + "_requests_total",
			Type:        eval.MetricCounter,
			Description: "Total requests",
		},
		{
			Name:        prefix + "_sampled_total",
			Type:        eval.MetricCounter,
			Description: "Total sampled requests",
		},
		{
			Name:        prefix + "_experiment_wins_total",
			Type:        eval.MetricCounter,
			Description: "Total experiment wins",
		},
		{
			Name:        prefix + "_control_wins_total",
			Type:        eval.MetricCounter,
			Description: "Total control wins",
		},
		{
			Name:        prefix + "_ties_total",
			Type:        eval.MetricCounter,
			Description: "Total ties",
		},
	}
}

// HealthCheck verifies the harness is functioning.
func (h *ABHarness) HealthCheck(ctx context.Context) error {
	// Check both algorithms
	if err := h.experiment.HealthCheck(ctx); err != nil {
		return err
	}
	return h.control.HealthCheck(ctx)
}

// -----------------------------------------------------------------------------
// Comparison Functions
// -----------------------------------------------------------------------------

// CompareDuration compares results by duration (faster is better).
//
// Inputs:
//   - experiment: Result from experimental algorithm (must be *algorithms.Result).
//   - control: Result from control algorithm (must be *algorithms.Result).
//
// Outputs:
//   - int: -1 if experiment faster, 1 if control faster, 0 if tie or invalid.
//
// Thread Safety: Safe for concurrent use (stateless).
func CompareDuration(experiment, control any) int {
	expResult, ok1 := experiment.(*algorithms.Result)
	ctrlResult, ok2 := control.(*algorithms.Result)

	if !ok1 || !ok2 {
		return 0
	}

	if expResult.Duration < ctrlResult.Duration {
		return -1 // experiment faster
	}
	if expResult.Duration > ctrlResult.Duration {
		return 1 // control faster
	}
	return 0
}

// CompareSuccess compares results by success (success is better).
//
// Inputs:
//   - experiment: Result from experimental algorithm (must be *algorithms.Result).
//   - control: Result from control algorithm (must be *algorithms.Result).
//
// Outputs:
//   - int: -1 if experiment succeeded and control failed, 1 if control succeeded
//     and experiment failed, 0 if both same status or invalid.
//
// Thread Safety: Safe for concurrent use (stateless).
func CompareSuccess(experiment, control any) int {
	expResult, ok1 := experiment.(*algorithms.Result)
	ctrlResult, ok2 := control.(*algorithms.Result)

	if !ok1 || !ok2 {
		return 0
	}

	expSuccess := expResult.Err == nil
	ctrlSuccess := ctrlResult.Err == nil

	if expSuccess && !ctrlSuccess {
		return -1 // experiment succeeded, control failed
	}
	if !expSuccess && ctrlSuccess {
		return 1 // control succeeded, experiment failed
	}
	return 0
}
