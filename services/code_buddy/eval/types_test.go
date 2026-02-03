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
	"testing"
	"time"
)

func TestSignalSource_String(t *testing.T) {
	tests := []struct {
		source   SignalSource
		expected string
	}{
		{SourceUnknown, "unknown"},
		{SourceCompiler, "compiler"},
		{SourceTest, "test"},
		{SourceTypeCheck, "type_check"},
		{SourceLinter, "linter"},
		{SourceSyntax, "syntax"},
		{SourceLLM, "llm"},
		{SourceHeuristic, "heuristic"},
		{SourceSimilarity, "similarity"},
		{SourceEstimate, "estimate"},
		{SignalSource(99), "signal_source(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.source.String(); got != tt.expected {
				t.Errorf("SignalSource.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSignalSource_IsHard(t *testing.T) {
	hardSources := []SignalSource{
		SourceCompiler,
		SourceTest,
		SourceTypeCheck,
		SourceLinter,
		SourceSyntax,
	}

	softSources := []SignalSource{
		SourceLLM,
		SourceHeuristic,
		SourceSimilarity,
		SourceEstimate,
	}

	for _, s := range hardSources {
		t.Run(s.String()+"_is_hard", func(t *testing.T) {
			if !s.IsHard() {
				t.Errorf("%v should be a hard signal", s)
			}
			if s.IsSoft() {
				t.Errorf("%v should not be a soft signal", s)
			}
		})
	}

	for _, s := range softSources {
		t.Run(s.String()+"_is_soft", func(t *testing.T) {
			if s.IsHard() {
				t.Errorf("%v should not be a hard signal", s)
			}
			if !s.IsSoft() {
				t.Errorf("%v should be a soft signal", s)
			}
		})
	}

	// Unknown is neither hard nor soft
	t.Run("unknown_is_neither", func(t *testing.T) {
		if SourceUnknown.IsHard() {
			t.Error("SourceUnknown should not be hard")
		}
		if SourceUnknown.IsSoft() {
			t.Error("SourceUnknown should not be soft")
		}
	})
}

func TestProperty_Validate(t *testing.T) {
	validCheck := func(input, output any) error { return nil }
	validGenerator := func() any { return nil }

	tests := []struct {
		name      string
		property  Property
		wantError bool
	}{
		{
			name: "valid property with generator",
			property: Property{
				Name:        "test_property",
				Description: "A test property",
				Check:       validCheck,
				Generator:   validGenerator,
			},
			wantError: false,
		},
		{
			name: "valid property without generator",
			property: Property{
				Name:        "test_property",
				Description: "A test property",
				Check:       validCheck,
			},
			wantError: false,
		},
		{
			name: "missing name",
			property: Property{
				Description: "A test property",
				Check:       validCheck,
			},
			wantError: true,
		},
		{
			name: "missing description",
			property: Property{
				Name:  "test_property",
				Check: validCheck,
			},
			wantError: true,
		},
		{
			name: "missing check",
			property: Property{
				Name:        "test_property",
				Description: "A test property",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.property.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Property.Validate() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && err != nil {
				if !errors.Is(err, ErrInvalidProperty) {
					t.Errorf("Expected ErrInvalidProperty, got %v", err)
				}
			}
		})
	}
}

func TestProperty_HasGenerator(t *testing.T) {
	t.Run("with generator", func(t *testing.T) {
		p := Property{
			Name:        "test",
			Description: "test",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return nil },
		}
		if !p.HasGenerator() {
			t.Error("Expected HasGenerator() to return true")
		}
	})

	t.Run("without generator", func(t *testing.T) {
		p := Property{
			Name:        "test",
			Description: "test",
			Check:       func(_, _ any) error { return nil },
		}
		if p.HasGenerator() {
			t.Error("Expected HasGenerator() to return false")
		}
	})
}

func TestProperty_HasShrink(t *testing.T) {
	t.Run("with shrink", func(t *testing.T) {
		p := Property{
			Name:        "test",
			Description: "test",
			Check:       func(_, _ any) error { return nil },
			Shrink:      func(input any) []any { return nil },
		}
		if !p.HasShrink() {
			t.Error("Expected HasShrink() to return true")
		}
	})

	t.Run("without shrink", func(t *testing.T) {
		p := Property{
			Name:        "test",
			Description: "test",
			Check:       func(_, _ any) error { return nil },
		}
		if p.HasShrink() {
			t.Error("Expected HasShrink() to return false")
		}
	})
}

func TestProperty_HasTag(t *testing.T) {
	p := Property{
		Name:        "test",
		Description: "test",
		Check:       func(_, _ any) error { return nil },
		Tags:        []string{"critical", "performance"},
	}

	if !p.HasTag("critical") {
		t.Error("Expected HasTag('critical') to return true")
	}
	if !p.HasTag("performance") {
		t.Error("Expected HasTag('performance') to return true")
	}
	if p.HasTag("nonexistent") {
		t.Error("Expected HasTag('nonexistent') to return false")
	}
}

func TestMetricType_String(t *testing.T) {
	tests := []struct {
		mt       MetricType
		expected string
	}{
		{MetricCounter, "counter"},
		{MetricGauge, "gauge"},
		{MetricHistogram, "histogram"},
		{MetricSummary, "summary"},
		{MetricType(99), "metric_type(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.mt.String(); got != tt.expected {
				t.Errorf("MetricType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMetricDefinition_Validate(t *testing.T) {
	tests := []struct {
		name      string
		metric    MetricDefinition
		wantError bool
	}{
		{
			name: "valid counter",
			metric: MetricDefinition{
				Name:        "test_counter",
				Type:        MetricCounter,
				Description: "A test counter",
			},
			wantError: false,
		},
		{
			name: "valid histogram with buckets",
			metric: MetricDefinition{
				Name:        "test_histogram",
				Type:        MetricHistogram,
				Description: "A test histogram",
				Buckets:     []float64{0.1, 0.5, 1.0},
			},
			wantError: false,
		},
		{
			name: "histogram without buckets",
			metric: MetricDefinition{
				Name:        "test_histogram",
				Type:        MetricHistogram,
				Description: "A test histogram",
			},
			wantError: true,
		},
		{
			name: "missing name",
			metric: MetricDefinition{
				Type:        MetricCounter,
				Description: "A test counter",
			},
			wantError: true,
		},
		{
			name: "missing description",
			metric: MetricDefinition{
				Name: "test_counter",
				Type: MetricCounter,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.metric.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("MetricDefinition.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestVerifyResult_FailedProperties(t *testing.T) {
	result := VerifyResult{
		Component: "test",
		Properties: []PropertyResult{
			{Name: "prop1", Passed: true},
			{Name: "prop2", Passed: false, Error: errors.New("failed")},
			{Name: "prop3", Passed: true},
			{Name: "prop4", Passed: false, Error: errors.New("also failed")},
		},
		Passed: false,
	}

	failed := result.FailedProperties()
	if len(failed) != 2 {
		t.Errorf("Expected 2 failed properties, got %d", len(failed))
	}

	if failed[0].Name != "prop2" || failed[1].Name != "prop4" {
		t.Errorf("Unexpected failed properties: %v", failed)
	}
}

func TestCoverageInfo_Percentage(t *testing.T) {
	tests := []struct {
		name     string
		coverage CoverageInfo
		expected float64
	}{
		{
			name:     "50% coverage",
			coverage: CoverageInfo{Statements: 50, TotalStatements: 100},
			expected: 50.0,
		},
		{
			name:     "100% coverage",
			coverage: CoverageInfo{Statements: 100, TotalStatements: 100},
			expected: 100.0,
		},
		{
			name:     "0% coverage",
			coverage: CoverageInfo{Statements: 0, TotalStatements: 100},
			expected: 0.0,
		},
		{
			name:     "no statements",
			coverage: CoverageInfo{Statements: 0, TotalStatements: 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.coverage.Percentage()
			if got != tt.expected {
				t.Errorf("CoverageInfo.Percentage() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{HealthUnknown, "unknown"},
		{HealthHealthy, "healthy"},
		{HealthDegraded, "degraded"},
		{HealthUnhealthy, "unhealthy"},
		{HealthStatus(99), "health_status(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("HealthStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSimpleEvaluable(t *testing.T) {
	t.Run("basic creation", func(t *testing.T) {
		e := NewSimpleEvaluable("test_component")

		if e.Name() != "test_component" {
			t.Errorf("Name() = %v, want 'test_component'", e.Name())
		}
		if len(e.Properties()) != 0 {
			t.Errorf("Properties() should be empty, got %d", len(e.Properties()))
		}
		if len(e.Metrics()) != 0 {
			t.Errorf("Metrics() should be empty, got %d", len(e.Metrics()))
		}
	})

	t.Run("add property", func(t *testing.T) {
		e := NewSimpleEvaluable("test")
		e.AddProperty(Property{
			Name:        "prop1",
			Description: "Test property",
			Check:       func(_, _ any) error { return nil },
		})

		if len(e.Properties()) != 1 {
			t.Errorf("Expected 1 property, got %d", len(e.Properties()))
		}
		if e.Properties()[0].Name != "prop1" {
			t.Errorf("Property name = %v, want 'prop1'", e.Properties()[0].Name)
		}
	})

	t.Run("add metric", func(t *testing.T) {
		e := NewSimpleEvaluable("test")
		e.AddMetric(MetricDefinition{
			Name:        "metric1",
			Type:        MetricCounter,
			Description: "Test metric",
		})

		if len(e.Metrics()) != 1 {
			t.Errorf("Expected 1 metric, got %d", len(e.Metrics()))
		}
		if e.Metrics()[0].Name != "metric1" {
			t.Errorf("Metric name = %v, want 'metric1'", e.Metrics()[0].Name)
		}
	})

	t.Run("health check nil", func(t *testing.T) {
		e := NewSimpleEvaluable("test")
		err := e.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("HealthCheck() = %v, want nil", err)
		}
	})

	t.Run("health check custom", func(t *testing.T) {
		e := NewSimpleEvaluable("test")
		expectedErr := errors.New("unhealthy")
		e.SetHealthCheck(func(ctx context.Context) error {
			return expectedErr
		})

		err := e.HealthCheck(context.Background())
		if err != expectedErr {
			t.Errorf("HealthCheck() = %v, want %v", err, expectedErr)
		}
	})

	t.Run("fluent interface", func(t *testing.T) {
		e := NewSimpleEvaluable("test").
			AddProperty(Property{
				Name:        "prop1",
				Description: "Test",
				Check:       func(_, _ any) error { return nil },
			}).
			AddMetric(MetricDefinition{
				Name:        "metric1",
				Type:        MetricGauge,
				Description: "Test",
			}).
			SetHealthCheck(func(ctx context.Context) error { return nil })

		if len(e.Properties()) != 1 {
			t.Error("Fluent AddProperty failed")
		}
		if len(e.Metrics()) != 1 {
			t.Error("Fluent AddMetric failed")
		}
	})
}

func TestSimpleEvaluable_ImplementsEvaluable(t *testing.T) {
	// Compile-time check that SimpleEvaluable implements Evaluable
	var _ Evaluable = (*SimpleEvaluable)(nil)
}

func TestPropertyResult_WithTimeout(t *testing.T) {
	p := Property{
		Name:        "test_property",
		Description: "Test property with timeout",
		Check:       func(_, _ any) error { return nil },
		Timeout:     5 * time.Second,
	}

	if p.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", p.Timeout)
	}
}

func TestHealthResult_Fields(t *testing.T) {
	now := time.Now()
	result := HealthResult{
		Component: "test_component",
		Status:    HealthHealthy,
		Message:   "All systems operational",
		Duration:  100 * time.Millisecond,
		Timestamp: now,
		Details: map[string]any{
			"memory_usage": "100MB",
			"connections":  42,
		},
	}

	if result.Component != "test_component" {
		t.Errorf("Component = %v, want 'test_component'", result.Component)
	}
	if result.Status != HealthHealthy {
		t.Errorf("Status = %v, want HealthHealthy", result.Status)
	}
	if result.Message != "All systems operational" {
		t.Errorf("Message = %v, want 'All systems operational'", result.Message)
	}
	if result.Duration != 100*time.Millisecond {
		t.Errorf("Duration = %v, want 100ms", result.Duration)
	}
	if !result.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", result.Timestamp, now)
	}
	if result.Details["memory_usage"] != "100MB" {
		t.Error("Details not set correctly")
	}
}
