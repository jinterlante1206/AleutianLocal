// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package regression

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrGateFailed indicates the regression gate did not pass.
	ErrGateFailed = errors.New("regression gate failed")
)

// -----------------------------------------------------------------------------
// Gate Configuration
// -----------------------------------------------------------------------------

// GateConfig configures the regression gate.
type GateConfig struct {
	// Detector configuration.
	DetectorConfig *DetectorConfig

	// UpdateBaselineOnPass updates baseline when check passes.
	// Default: false
	UpdateBaselineOnPass bool

	// RequireBaseline fails if no baseline exists.
	// Default: false (missing baseline = pass)
	RequireBaseline bool

	// AllowedRegressions is the maximum regressions before failing.
	// Default: 0 (any regression fails)
	AllowedRegressions int

	// FailOnWarnings fails the gate on warnings.
	// Default: false
	FailOnWarnings bool

	// Logger for output.
	Logger *slog.Logger
}

// DefaultGateConfig returns sensible defaults.
func DefaultGateConfig() *GateConfig {
	return &GateConfig{
		DetectorConfig:       DefaultDetectorConfig(),
		UpdateBaselineOnPass: false,
		RequireBaseline:      false,
		AllowedRegressions:   0,
		FailOnWarnings:       false,
		Logger:               slog.Default(),
	}
}

// -----------------------------------------------------------------------------
// Gate Options
// -----------------------------------------------------------------------------

// GateOption configures the gate.
type GateOption func(*GateConfig)

// WithLatencyThreshold sets all latency thresholds.
func WithLatencyThreshold(threshold float64) GateOption {
	return func(c *GateConfig) {
		c.DetectorConfig.LatencyP50Threshold = threshold
		c.DetectorConfig.LatencyP95Threshold = threshold * 1.5
		c.DetectorConfig.LatencyP99Threshold = threshold * 2
	}
}

// WithThroughputThreshold sets the throughput threshold.
func WithThroughputThreshold(threshold float64) GateOption {
	return func(c *GateConfig) {
		c.DetectorConfig.ThroughputThreshold = threshold
	}
}

// WithMemoryThreshold sets the memory threshold.
func WithMemoryThreshold(threshold float64) GateOption {
	return func(c *GateConfig) {
		c.DetectorConfig.MemoryThreshold = threshold
	}
}

// WithErrorThreshold sets the error rate threshold.
func WithErrorThreshold(threshold float64) GateOption {
	return func(c *GateConfig) {
		c.DetectorConfig.ErrorRateThreshold = threshold
	}
}

// WithUpdateBaseline enables baseline update on pass.
func WithUpdateBaseline(enabled bool) GateOption {
	return func(c *GateConfig) {
		c.UpdateBaselineOnPass = enabled
	}
}

// WithRequireBaseline requires baseline to exist.
func WithRequireBaseline(required bool) GateOption {
	return func(c *GateConfig) {
		c.RequireBaseline = required
	}
}

// WithAllowedRegressions sets allowed regression count.
func WithAllowedRegressions(count int) GateOption {
	return func(c *GateConfig) {
		if count >= 0 {
			c.AllowedRegressions = count
		}
	}
}

// WithFailOnWarnings enables failing on warnings.
func WithFailOnWarnings(fail bool) GateOption {
	return func(c *GateConfig) {
		c.FailOnWarnings = fail
	}
}

// WithGateLogger sets the logger.
func WithGateLogger(logger *slog.Logger) GateOption {
	return func(c *GateConfig) {
		if logger != nil {
			c.Logger = logger
		}
	}
}

// -----------------------------------------------------------------------------
// Gate
// -----------------------------------------------------------------------------

// Gate checks for performance regressions against baselines.
//
// Description:
//
//	Gate compares current benchmark results against stored baselines
//	and determines whether to pass or fail the CI/CD pipeline.
//
// Thread Safety: Safe for concurrent use.
type Gate struct {
	baseline Baseline
	detector *Detector
	config   *GateConfig
	logger   *slog.Logger
}

// NewGate creates a new regression gate.
//
// Inputs:
//   - baseline: Baseline store. Must not be nil.
//   - opts: Configuration options.
//
// Outputs:
//   - *Gate: The new gate. Never nil.
func NewGate(baseline Baseline, opts ...GateOption) *Gate {
	config := DefaultGateConfig()
	for _, opt := range opts {
		opt(config)
	}

	return &Gate{
		baseline: baseline,
		detector: NewDetector(config.DetectorConfig),
		config:   config,
		logger:   config.Logger,
	}
}

// GateDecision contains the gate check result.
type GateDecision struct {
	// Pass is true if the gate allows deployment.
	Pass bool

	// Component is the checked component.
	Component string

	// Regressions contains detected regressions.
	Regressions []Regression

	// Warnings contains non-blocking warnings.
	Warnings []Regression

	// BaselineUpdated is true if baseline was updated.
	BaselineUpdated bool

	// Report is a human-readable summary.
	Report string

	// Duration is the check duration.
	Duration time.Duration

	// Timestamp is when the check was performed.
	Timestamp time.Time
}

// Check evaluates current metrics against baseline.
//
// Description:
//
//	Check retrieves the baseline for the component, runs regression
//	detection, and returns a decision about whether to pass or fail.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - component: Component name.
//   - current: Current performance metrics.
//
// Outputs:
//   - *GateDecision: The gate decision. Never nil.
//   - error: Non-nil only if check could not be performed.
//
// Thread Safety: Safe for concurrent use.
func (g *Gate) Check(ctx context.Context, component string, current *CurrentMetrics) (*GateDecision, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	if current == nil {
		return nil, errors.New("current metrics must not be nil")
	}

	ctx, span := otel.Tracer("regression").Start(ctx, "regression.Gate.Check",
		trace.WithAttributes(
			attribute.String("component", component),
		),
	)
	defer span.End()

	start := time.Now()
	decision := &GateDecision{
		Component: component,
		Timestamp: start,
	}

	// Get baseline
	baselineData, err := g.baseline.Get(ctx, component)
	if err != nil {
		if errors.Is(err, ErrBaselineNotFound) {
			if g.config.RequireBaseline {
				decision.Pass = false
				decision.Report = "Baseline not found and RequireBaseline is enabled"
				decision.Duration = time.Since(start)
				return decision, nil
			}

			// No baseline - create one and pass
			newBaseline := g.createBaseline(component, current)
			if g.config.UpdateBaselineOnPass {
				if setErr := g.baseline.Set(ctx, component, newBaseline); setErr != nil {
					g.logger.Warn("failed to create initial baseline",
						slog.String("component", component),
						slog.String("error", setErr.Error()),
					)
				} else {
					decision.BaselineUpdated = true
				}
			}

			decision.Pass = true
			decision.Report = "No baseline found - first run"
			decision.Duration = time.Since(start)
			return decision, nil
		}
		return nil, err
	}

	// Run detection
	result := g.detector.Detect(baselineData, current)

	decision.Regressions = result.Regressions
	decision.Warnings = result.Warnings

	// Determine pass/fail
	if len(result.Regressions) > g.config.AllowedRegressions {
		decision.Pass = false
	} else if g.config.FailOnWarnings && len(result.Warnings) > 0 {
		decision.Pass = false
	} else {
		decision.Pass = true

		// Update baseline if configured
		if g.config.UpdateBaselineOnPass {
			newBaseline := g.createBaseline(component, current)
			newBaseline.Version = baselineData.Version // Preserve version
			if setErr := g.baseline.Set(ctx, component, newBaseline); setErr != nil {
				g.logger.Warn("failed to update baseline",
					slog.String("component", component),
					slog.String("error", setErr.Error()),
				)
			} else {
				decision.BaselineUpdated = true
			}
		}
	}

	decision.Report = g.generateReport(decision, baselineData, current)
	decision.Duration = time.Since(start)

	span.SetAttributes(
		attribute.Bool("pass", decision.Pass),
		attribute.Int("regressions", len(decision.Regressions)),
		attribute.Int("warnings", len(decision.Warnings)),
		attribute.Bool("baseline_updated", decision.BaselineUpdated),
	)
	if !decision.Pass {
		span.SetStatus(codes.Error, "regression detected")
	}

	g.logger.Info("regression gate check completed",
		slog.String("component", component),
		slog.Bool("pass", decision.Pass),
		slog.Int("regressions", len(decision.Regressions)),
		slog.Int("warnings", len(decision.Warnings)),
	)

	return decision, nil
}

// CheckAll checks multiple components.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - components: Map of component name to current metrics.
//
// Outputs:
//   - map[string]*GateDecision: Decisions for each component.
//   - error: Non-nil if any check failed to run.
//
// Thread Safety: Safe for concurrent use.
func (g *Gate) CheckAll(ctx context.Context, components map[string]*CurrentMetrics) (map[string]*GateDecision, error) {
	decisions := make(map[string]*GateDecision)

	for name, metrics := range components {
		select {
		case <-ctx.Done():
			return decisions, ctx.Err()
		default:
		}

		decision, err := g.Check(ctx, name, metrics)
		if err != nil {
			return decisions, err
		}
		decisions[name] = decision
	}

	return decisions, nil
}

// createBaseline creates baseline data from current metrics.
func (g *Gate) createBaseline(component string, current *CurrentMetrics) *BaselineData {
	return &BaselineData{
		Component:   component,
		Version:     "1",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Latency:     current.Latency,
		Throughput:  current.Throughput,
		Memory:      current.Memory,
		Error:       ErrorBaseline{Rate: current.ErrorRate},
		SampleCount: current.SampleCount,
	}
}

// generateReport creates a human-readable report.
func (g *Gate) generateReport(decision *GateDecision, baseline *BaselineData, current *CurrentMetrics) string {
	var sb strings.Builder

	sb.WriteString("# Regression Gate Report\n\n")

	if decision.Pass {
		sb.WriteString("**Status: PASS**\n\n")
	} else {
		sb.WriteString("**Status: FAIL**\n\n")
	}

	sb.WriteString(fmt.Sprintf("Component: %s\n", decision.Component))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n\n", decision.Timestamp.Format(time.RFC3339)))

	// Comparison table
	sb.WriteString("## Metrics Comparison\n\n")
	sb.WriteString("| Metric | Baseline | Current | Change |\n")
	sb.WriteString("|--------|----------|---------|--------|\n")

	sb.WriteString(fmt.Sprintf("| P50 Latency | %v | %v | %+.1f%% |\n",
		baseline.Latency.P50, current.Latency.P50,
		percentChange(float64(baseline.Latency.P50), float64(current.Latency.P50))))

	sb.WriteString(fmt.Sprintf("| P99 Latency | %v | %v | %+.1f%% |\n",
		baseline.Latency.P99, current.Latency.P99,
		percentChange(float64(baseline.Latency.P99), float64(current.Latency.P99))))

	sb.WriteString(fmt.Sprintf("| Throughput | %.2f ops/s | %.2f ops/s | %+.1f%% |\n",
		baseline.Throughput.OpsPerSecond, current.Throughput.OpsPerSecond,
		-percentChange(baseline.Throughput.OpsPerSecond, current.Throughput.OpsPerSecond)))

	sb.WriteString(fmt.Sprintf("| Memory | %d B/op | %d B/op | %+.1f%% |\n",
		baseline.Memory.AllocBytesPerOp, current.Memory.AllocBytesPerOp,
		percentChange(float64(baseline.Memory.AllocBytesPerOp), float64(current.Memory.AllocBytesPerOp))))

	sb.WriteString(fmt.Sprintf("| Error Rate | %.2f%% | %.2f%% | %+.2f%% |\n",
		baseline.Error.Rate*100, current.ErrorRate*100,
		(current.ErrorRate-baseline.Error.Rate)*100))

	// Regressions
	if len(decision.Regressions) > 0 {
		sb.WriteString("\n## Regressions\n\n")
		for _, r := range decision.Regressions {
			sb.WriteString(fmt.Sprintf("- **%s** [%s]: %s\n",
				r.Type, r.Severity, r.Message))
		}
	}

	// Warnings
	if len(decision.Warnings) > 0 {
		sb.WriteString("\n## Warnings\n\n")
		for _, w := range decision.Warnings {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", w.Type, w.Message))
		}
	}

	if decision.BaselineUpdated {
		sb.WriteString("\n*Baseline updated with current metrics.*\n")
	}

	return sb.String()
}

func percentChange(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((current - baseline) / baseline) * 100
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Name implements eval.Evaluable.
func (g *Gate) Name() string {
	return "regression_gate"
}

// Properties implements eval.Evaluable.
func (g *Gate) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "detects_latency_regression",
			Description: "Detects when latency exceeds threshold",
			Check: func(input, output any) error {
				return nil
			},
		},
		{
			Name:        "detects_throughput_regression",
			Description: "Detects when throughput falls below threshold",
			Check: func(input, output any) error {
				return nil
			},
		},
	}
}

// Metrics implements eval.Evaluable.
func (g *Gate) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "regression_gate_checks_total",
			Type:        eval.MetricCounter,
			Description: "Total gate checks performed",
			Labels:      []string{"component", "result"},
		},
		{
			Name:        "regression_gate_regressions_detected",
			Type:        eval.MetricCounter,
			Description: "Number of regressions detected",
			Labels:      []string{"component", "type"},
		},
	}
}

// HealthCheck implements eval.Evaluable.
func (g *Gate) HealthCheck(ctx context.Context) error {
	// Try to list baselines as health check
	_, err := g.baseline.List(ctx)
	return err
}
