// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package eval

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrNotFound is returned when a component is not found in the registry.
	ErrNotFound = errors.New("component not found")

	// ErrAlreadyRegistered is returned when attempting to register a duplicate.
	ErrAlreadyRegistered = errors.New("component already registered")

	// ErrNilComponent is returned when attempting to register nil.
	ErrNilComponent = errors.New("component must not be nil")

	// ErrInvalidProperty is returned when a property is malformed.
	ErrInvalidProperty = errors.New("invalid property definition")

	// ErrPropertyFailed is returned when a property check fails.
	ErrPropertyFailed = errors.New("property check failed")

	// ErrHealthCheckFailed is returned when a health check fails.
	ErrHealthCheckFailed = errors.New("health check failed")

	// ErrSoftSignalViolation is returned when soft signals are used for hard decisions.
	ErrSoftSignalViolation = errors.New("soft signal used for state mutation")
)

// -----------------------------------------------------------------------------
// Signal Sources (Hard/Soft Boundary)
// -----------------------------------------------------------------------------

// SignalSource identifies where a piece of information came from.
// This is critical for the hard/soft signal boundary.
type SignalSource int

const (
	// SourceUnknown is the zero value, indicating unset source.
	SourceUnknown SignalSource = iota

	// Hard signals - can update reasoning state
	// SourceCompiler indicates the signal came from compiler output.
	SourceCompiler
	// SourceTest indicates the signal came from test execution.
	SourceTest
	// SourceTypeCheck indicates the signal came from type checker.
	SourceTypeCheck
	// SourceLinter indicates the signal came from linter output.
	SourceLinter
	// SourceSyntax indicates the signal came from syntax analysis.
	SourceSyntax

	// Soft signals - guidance only, cannot update state
	// SourceLLM indicates the signal came from LLM output.
	SourceLLM
	// SourceHeuristic indicates the signal came from heuristic estimation.
	SourceHeuristic
	// SourceSimilarity indicates the signal came from similarity matching.
	SourceSimilarity
	// SourceEstimate indicates the signal came from statistical estimation.
	SourceEstimate
)

// String returns the string representation of a SignalSource.
func (s SignalSource) String() string {
	switch s {
	case SourceUnknown:
		return "unknown"
	case SourceCompiler:
		return "compiler"
	case SourceTest:
		return "test"
	case SourceTypeCheck:
		return "type_check"
	case SourceLinter:
		return "linter"
	case SourceSyntax:
		return "syntax"
	case SourceLLM:
		return "llm"
	case SourceHeuristic:
		return "heuristic"
	case SourceSimilarity:
		return "similarity"
	case SourceEstimate:
		return "estimate"
	default:
		return fmt.Sprintf("signal_source(%d)", s)
	}
}

// IsHard returns true if this is a hard signal that can update state.
func (s SignalSource) IsHard() bool {
	switch s {
	case SourceCompiler, SourceTest, SourceTypeCheck, SourceLinter, SourceSyntax:
		return true
	default:
		return false
	}
}

// IsSoft returns true if this is a soft signal (guidance only).
func (s SignalSource) IsSoft() bool {
	return !s.IsHard() && s != SourceUnknown
}

// -----------------------------------------------------------------------------
// Core Interfaces
// -----------------------------------------------------------------------------

// Evaluable is the interface that all testable/benchmarkable components implement.
// This is the foundation of the evaluation framework.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Evaluable interface {
	// Name returns a unique identifier for metrics and logging.
	// The name should be stable across versions and suitable for use
	// in metric labels (lowercase, underscore-separated).
	//
	// Example: "cdcl", "pn_mcts", "tms"
	Name() string

	// Properties returns the correctness properties this component guarantees.
	// These are used by the property-based testing framework.
	// An empty slice indicates no properties to verify.
	Properties() []Property

	// Metrics returns the metrics this component exposes.
	// These are used by the benchmark and monitoring systems.
	// An empty slice indicates no custom metrics.
	Metrics() []MetricDefinition

	// HealthCheck verifies the component is functioning correctly.
	// Called during chaos testing recovery verification.
	// Returns nil if healthy, error with details otherwise.
	//
	// Inputs:
	//   - ctx: Context for cancellation. Must not be nil.
	//
	// Outputs:
	//   - error: nil if healthy, descriptive error otherwise.
	HealthCheck(ctx context.Context) error
}

// -----------------------------------------------------------------------------
// Property Definition
// -----------------------------------------------------------------------------

// Property defines a correctness invariant for testing.
// Properties should be independent and composable.
//
// Example:
//
//	Property{
//	    Name: "no_soft_signal_clauses",
//	    Description: "CDCL only learns from compiler/test failures",
//	    Check: func(input, output any) error { ... },
//	    Generator: func() any { ... },
//	}
type Property struct {
	// Name is a unique identifier for this property.
	// Should be lowercase with underscores (e.g., "proof_propagation_correct").
	Name string

	// Description explains what this property verifies.
	// Should be a complete sentence.
	Description string

	// Check verifies the property holds for the given input/output pair.
	// Returns nil if the property holds, error with details otherwise.
	//
	// Inputs:
	//   - input: The input that was provided to the component.
	//   - output: The output that was produced.
	//
	// Outputs:
	//   - error: nil if property holds, descriptive error otherwise.
	Check func(input any, output any) error

	// Generator produces random valid inputs for property testing.
	// Should return diverse inputs covering edge cases.
	// If nil, the property can only be checked with explicit inputs.
	Generator func() any

	// Shrink attempts to reduce a failing input to a minimal case.
	// If nil, no shrinking is performed.
	// This helps debugging by finding the smallest failing input.
	Shrink func(input any) []any

	// Tags categorize this property for selective testing.
	// Examples: "critical", "performance", "boundary"
	Tags []string

	// Timeout is the maximum time for a single property check.
	// Zero means use the default timeout.
	Timeout time.Duration
}

// Validate checks that the property is well-formed.
//
// Outputs:
//   - error: nil if valid, descriptive error otherwise.
func (p *Property) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidProperty)
	}
	if p.Description == "" {
		return fmt.Errorf("%w: description is required for %s", ErrInvalidProperty, p.Name)
	}
	if p.Check == nil {
		return fmt.Errorf("%w: check function is required for %s", ErrInvalidProperty, p.Name)
	}
	return nil
}

// HasGenerator returns true if this property has an input generator.
func (p *Property) HasGenerator() bool {
	return p.Generator != nil
}

// HasShrink returns true if this property supports input shrinking.
func (p *Property) HasShrink() bool {
	return p.Shrink != nil
}

// HasTag returns true if this property has the specified tag.
func (p *Property) HasTag(tag string) bool {
	for _, t := range p.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Metric Definition
// -----------------------------------------------------------------------------

// MetricType identifies the type of metric.
type MetricType int

const (
	// MetricCounter is a monotonically increasing value.
	MetricCounter MetricType = iota
	// MetricGauge is a value that can go up or down.
	MetricGauge
	// MetricHistogram records observations in buckets.
	MetricHistogram
	// MetricSummary records observations with quantiles.
	MetricSummary
)

// String returns the string representation of a MetricType.
func (m MetricType) String() string {
	switch m {
	case MetricCounter:
		return "counter"
	case MetricGauge:
		return "gauge"
	case MetricHistogram:
		return "histogram"
	case MetricSummary:
		return "summary"
	default:
		return fmt.Sprintf("metric_type(%d)", m)
	}
}

// MetricDefinition describes a metric exposed by a component.
//
// Example:
//
//	MetricDefinition{
//	    Name:        "cdcl_clauses_learned",
//	    Type:        MetricCounter,
//	    Description: "Total number of clauses learned",
//	    Labels:      []string{"source"},
//	}
type MetricDefinition struct {
	// Name is the metric name (should follow Prometheus conventions).
	// Example: "algorithm_process_seconds"
	Name string

	// Type is the metric type (counter, gauge, histogram, summary).
	Type MetricType

	// Description explains what this metric measures.
	Description string

	// Labels are the label names for this metric.
	// Example: []string{"algorithm", "status"}
	Labels []string

	// Buckets are the histogram bucket boundaries (for histograms only).
	// Example: []float64{0.001, 0.01, 0.1, 1.0, 10.0}
	Buckets []float64

	// Objectives are the quantile objectives (for summaries only).
	// Example: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}
	Objectives map[float64]float64
}

// Validate checks that the metric definition is well-formed.
//
// Outputs:
//   - error: nil if valid, descriptive error otherwise.
func (m *MetricDefinition) Validate() error {
	if m.Name == "" {
		return errors.New("metric name is required")
	}
	if m.Description == "" {
		return errors.New("metric description is required")
	}
	if m.Type == MetricHistogram && len(m.Buckets) == 0 {
		return errors.New("histogram metrics require buckets")
	}
	return nil
}

// -----------------------------------------------------------------------------
// Verification Results
// -----------------------------------------------------------------------------

// VerifyResult contains the results of verifying a component's properties.
type VerifyResult struct {
	// Component is the name of the component that was verified.
	Component string

	// Properties contains results for each property.
	Properties []PropertyResult

	// Duration is the total time spent verifying.
	Duration time.Duration

	// Passed is true if all properties passed.
	Passed bool

	// Iterations is the total number of test iterations run.
	Iterations int

	// Coverage tracks which code paths were exercised.
	Coverage *CoverageInfo
}

// FailedProperties returns the properties that failed.
func (r *VerifyResult) FailedProperties() []PropertyResult {
	var failed []PropertyResult
	for _, pr := range r.Properties {
		if !pr.Passed {
			failed = append(failed, pr)
		}
	}
	return failed
}

// PropertyResult contains the result of verifying a single property.
type PropertyResult struct {
	// Name is the property name.
	Name string

	// Passed is true if the property held for all inputs.
	Passed bool

	// Iterations is the number of test iterations run.
	Iterations int

	// Duration is the time spent on this property.
	Duration time.Duration

	// FailingInput is the input that caused failure (if any).
	// This is the minimal input after shrinking.
	FailingInput any

	// FailingOutput is the output that caused failure (if any).
	FailingOutput any

	// Error is the error returned by the Check function (if any).
	Error error

	// ShrinkSteps is the number of shrinking iterations performed.
	ShrinkSteps int
}

// CoverageInfo tracks code coverage during verification.
type CoverageInfo struct {
	// Statements is the number of statements covered.
	Statements int

	// TotalStatements is the total number of statements.
	TotalStatements int

	// Branches is the number of branches covered.
	Branches int

	// TotalBranches is the total number of branches.
	TotalBranches int
}

// Percentage returns the coverage percentage.
func (c *CoverageInfo) Percentage() float64 {
	if c.TotalStatements == 0 {
		return 0.0
	}
	return float64(c.Statements) / float64(c.TotalStatements) * 100.0
}

// -----------------------------------------------------------------------------
// Health Check Result
// -----------------------------------------------------------------------------

// HealthStatus represents the health state of a component.
type HealthStatus int

const (
	// HealthUnknown is the zero value.
	HealthUnknown HealthStatus = iota
	// HealthHealthy indicates the component is functioning correctly.
	HealthHealthy
	// HealthDegraded indicates reduced functionality.
	HealthDegraded
	// HealthUnhealthy indicates the component is not functioning.
	HealthUnhealthy
)

// String returns the string representation of a HealthStatus.
func (h HealthStatus) String() string {
	switch h {
	case HealthUnknown:
		return "unknown"
	case HealthHealthy:
		return "healthy"
	case HealthDegraded:
		return "degraded"
	case HealthUnhealthy:
		return "unhealthy"
	default:
		return fmt.Sprintf("health_status(%d)", h)
	}
}

// HealthResult contains the result of a health check.
type HealthResult struct {
	// Component is the name of the component.
	Component string

	// Status is the health status.
	Status HealthStatus

	// Message provides details about the health status.
	Message string

	// Duration is the time spent on the health check.
	Duration time.Duration

	// Timestamp is when the check was performed (Unix milliseconds UTC).
	Timestamp int64

	// Details contains component-specific health information.
	Details map[string]any
}

// -----------------------------------------------------------------------------
// Evaluable Wrapper
// -----------------------------------------------------------------------------

// SimpleEvaluable is a simple implementation of Evaluable for testing.
type SimpleEvaluable struct {
	name        string
	properties  []Property
	metrics     []MetricDefinition
	healthCheck func(ctx context.Context) error
}

// NewSimpleEvaluable creates a new SimpleEvaluable.
//
// Inputs:
//   - name: Unique identifier for the component. Must not be empty.
//
// Outputs:
//   - *SimpleEvaluable: The new evaluable. Never nil.
func NewSimpleEvaluable(name string) *SimpleEvaluable {
	return &SimpleEvaluable{
		name:       name,
		properties: make([]Property, 0),
		metrics:    make([]MetricDefinition, 0),
	}
}

// Name returns the component name.
func (s *SimpleEvaluable) Name() string {
	return s.name
}

// Properties returns the registered properties.
func (s *SimpleEvaluable) Properties() []Property {
	return s.properties
}

// Metrics returns the registered metrics.
func (s *SimpleEvaluable) Metrics() []MetricDefinition {
	return s.metrics
}

// HealthCheck performs the health check.
func (s *SimpleEvaluable) HealthCheck(ctx context.Context) error {
	if s.healthCheck == nil {
		return nil
	}
	return s.healthCheck(ctx)
}

// AddProperty adds a property to this evaluable.
func (s *SimpleEvaluable) AddProperty(p Property) *SimpleEvaluable {
	s.properties = append(s.properties, p)
	return s
}

// AddMetric adds a metric definition to this evaluable.
func (s *SimpleEvaluable) AddMetric(m MetricDefinition) *SimpleEvaluable {
	s.metrics = append(s.metrics, m)
	return s
}

// SetHealthCheck sets the health check function.
func (s *SimpleEvaluable) SetHealthCheck(fn func(ctx context.Context) error) *SimpleEvaluable {
	s.healthCheck = fn
	return s
}
