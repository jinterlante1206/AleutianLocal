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
	"sync"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Mock Evaluable
// -----------------------------------------------------------------------------

type mockEvaluable struct {
	name       string
	properties []eval.Property
	metrics    []eval.MetricDefinition
	healthErr  error
}

func newMockEvaluable(name string) *mockEvaluable {
	return &mockEvaluable{name: name}
}

func (m *mockEvaluable) Name() string                          { return m.name }
func (m *mockEvaluable) Properties() []eval.Property           { return m.properties }
func (m *mockEvaluable) Metrics() []eval.MetricDefinition      { return m.metrics }
func (m *mockEvaluable) HealthCheck(ctx context.Context) error { return m.healthErr }

// -----------------------------------------------------------------------------
// Harness Creation Tests
// -----------------------------------------------------------------------------

func TestNewHarness(t *testing.T) {
	t.Run("valid creation", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, err := NewHarness(control, experiment)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if harness == nil {
			t.Fatal("expected non-nil harness")
		}
	})

	t.Run("nil control", func(t *testing.T) {
		experiment := newMockEvaluable("experiment")

		_, err := NewHarness(nil, experiment)
		if err != ErrNilAlgorithm {
			t.Errorf("expected ErrNilAlgorithm, got %v", err)
		}
	})

	t.Run("nil experiment", func(t *testing.T) {
		control := newMockEvaluable("control")

		_, err := NewHarness(control, nil)
		if err != ErrNilAlgorithm {
			t.Errorf("expected ErrNilAlgorithm, got %v", err)
		}
	})

	t.Run("with options", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, err := NewHarness(control, experiment,
			WithSampleRate(0.5),
			WithMaxSamples(500),
			WithMinSamples(50),
			WithConfidenceLevel(0.99),
			WithRunBothAlways(true),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check that options were applied
		if harness.config.SampleRate != 0.5 {
			t.Errorf("expected sample rate 0.5, got %f", harness.config.SampleRate)
		}
		if harness.config.MaxSamples != 500 {
			t.Errorf("expected max samples 500, got %d", harness.config.MaxSamples)
		}
		if harness.config.DecisionConfig.MinSamples != 50 {
			t.Errorf("expected min samples 50, got %d", harness.config.DecisionConfig.MinSamples)
		}
	})

	t.Run("sampler types", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		samplerTypes := []SamplerType{
			SamplerTypeRandom,
			SamplerTypeHash,
			SamplerTypeBandit,
			SamplerTypeRampUp,
		}

		for _, st := range samplerTypes {
			harness, err := NewHarness(control, experiment, WithSamplerType(st))
			if err != nil {
				t.Fatalf("failed to create harness with sampler type %d: %v", st, err)
			}
			if harness == nil {
				t.Errorf("expected non-nil harness for sampler type %d", st)
			}
		}
	})
}

// -----------------------------------------------------------------------------
// Compare Tests
// -----------------------------------------------------------------------------

func TestHarness_Compare(t *testing.T) {
	t.Run("runs control always", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment, WithSampleRate(0)) // 0% experiment

		ctx := context.Background()
		controlProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "control_output", 10 * time.Millisecond, nil
		}
		expProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "experiment_output", 5 * time.Millisecond, nil
		}

		output, err := harness.Compare(ctx, "test_key", controlProc, expProc, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if output != "control_output" {
			t.Errorf("expected control output, got %v", output)
		}

		results := harness.GetResults()
		if results.ControlSamples != 1 {
			t.Errorf("expected 1 control sample, got %d", results.ControlSamples)
		}
		if results.ExperimentSamples != 0 {
			t.Errorf("expected 0 experiment samples, got %d", results.ExperimentSamples)
		}
	})

	t.Run("run both always", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment,
			WithRunBothAlways(true),
			WithCompareOutputs(true),
		)

		ctx := context.Background()
		controlProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "output", 10 * time.Millisecond, nil
		}
		expProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "output", 5 * time.Millisecond, nil
		}

		_, _ = harness.Compare(ctx, "test_key", controlProc, expProc, nil)

		results := harness.GetResults()
		if results.ControlSamples != 1 {
			t.Errorf("expected 1 control sample, got %d", results.ControlSamples)
		}
		if results.ExperimentSamples != 1 {
			t.Errorf("expected 1 experiment sample, got %d", results.ExperimentSamples)
		}
		if results.CorrectnessMatch != 1.0 {
			t.Errorf("expected 100%% correctness match, got %.2f%%", results.CorrectnessMatch*100)
		}
	})

	t.Run("correctness mismatch", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment,
			WithRunBothAlways(true),
			WithCompareOutputs(true),
		)

		ctx := context.Background()
		controlProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "control_output", 10 * time.Millisecond, nil
		}
		expProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "different_output", 5 * time.Millisecond, nil
		}

		_, _ = harness.Compare(ctx, "test_key", controlProc, expProc, nil)

		results := harness.GetResults()
		if results.CorrectnessMatch != 0.0 {
			t.Errorf("expected 0%% correctness match, got %.2f%%", results.CorrectnessMatch*100)
		}
	})

	t.Run("control error", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment, WithRunBothAlways(true))

		ctx := context.Background()
		expectedErr := errors.New("control error")
		controlProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return nil, 0, expectedErr
		}
		expProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return "output", 5 * time.Millisecond, nil
		}

		_, err := harness.Compare(ctx, "test_key", controlProc, expProc, nil)
		if err != expectedErr {
			t.Errorf("expected control error, got %v", err)
		}

		results := harness.GetResults()
		if results.ControlErrors != 1 {
			t.Errorf("expected 1 control error, got %d", results.ControlErrors)
		}
	})

	t.Run("concurrent compare", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment, WithRunBothAlways(true))

		ctx := context.Background()
		controlProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return input, 10 * time.Millisecond, nil
		}
		expProc := func(ctx context.Context, input any) (any, time.Duration, error) {
			return input, 5 * time.Millisecond, nil
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				harness.Compare(ctx, "test_key", controlProc, expProc, i)
			}(i)
		}
		wg.Wait()

		results := harness.GetResults()
		if results.ControlSamples != 100 {
			t.Errorf("expected 100 control samples, got %d", results.ControlSamples)
		}
		if results.ExperimentSamples != 100 {
			t.Errorf("expected 100 experiment samples, got %d", results.ExperimentSamples)
		}
	})
}

// -----------------------------------------------------------------------------
// Select Variant Tests
// -----------------------------------------------------------------------------

func TestHarness_SelectVariant(t *testing.T) {
	t.Run("consistent hash sampling", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment,
			WithSampleRate(0.5),
			WithSamplerType(SamplerTypeHash),
		)

		// Same key should always return same result
		key := "test_key_123"
		first := harness.SelectVariant(key)
		for i := 0; i < 100; i++ {
			if harness.SelectVariant(key) != first {
				t.Error("expected consistent hash sampling")
			}
		}
	})
}

// -----------------------------------------------------------------------------
// Record Tests
// -----------------------------------------------------------------------------

func TestHarness_RecordLatency(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	harness.RecordLatency(false, 10*time.Millisecond)
	harness.RecordLatency(false, 20*time.Millisecond)
	harness.RecordLatency(true, 5*time.Millisecond)

	results := harness.GetResults()
	if results.ControlSamples != 2 {
		t.Errorf("expected 2 control samples, got %d", results.ControlSamples)
	}
	if results.ExperimentSamples != 1 {
		t.Errorf("expected 1 experiment sample, got %d", results.ExperimentSamples)
	}
}

func TestHarness_RecordError(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	harness.RecordError(false)
	harness.RecordError(true)
	harness.RecordError(true)

	results := harness.GetResults()
	if results.ControlErrors != 1 {
		t.Errorf("expected 1 control error, got %d", results.ControlErrors)
	}
	if results.ExperimentErrors != 2 {
		t.Errorf("expected 2 experiment errors, got %d", results.ExperimentErrors)
	}
}

func TestHarness_RecordCorrectness(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	harness.RecordCorrectness(true)
	harness.RecordCorrectness(true)
	harness.RecordCorrectness(false)

	results := harness.GetResults()
	expectedMatch := 2.0 / 3.0
	if results.CorrectnessMatch != expectedMatch {
		t.Errorf("expected correctness %.2f, got %.2f", expectedMatch, results.CorrectnessMatch)
	}
}

// -----------------------------------------------------------------------------
// GetResults Tests
// -----------------------------------------------------------------------------

func TestHarness_GetResults(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment,
		WithMinSamples(10),
	)

	// Add samples that show experiment is faster
	for i := 0; i < 50; i++ {
		harness.RecordLatency(false, time.Duration(100+i%10)*time.Millisecond)
		harness.RecordLatency(true, time.Duration(50+i%10)*time.Millisecond)
		harness.RecordCorrectness(true)
	}

	results := harness.GetResults()

	// Check basic counts
	if results.ControlSamples != 50 {
		t.Errorf("expected 50 control samples, got %d", results.ControlSamples)
	}
	if results.ExperimentSamples != 50 {
		t.Errorf("expected 50 experiment samples, got %d", results.ExperimentSamples)
	}

	// Check statistical results exist
	if results.TTest == nil {
		t.Error("expected t-test result")
	}
	if results.ConfidenceInterval == nil {
		t.Error("expected confidence interval")
	}

	// Check direction (experiment should be faster = positive effect)
	// EffectSize = (control - experiment) / pooledStdDev
	// When control > experiment, d > 0, meaning experiment is faster/better
	if results.EffectSize <= 0 {
		t.Errorf("expected positive effect size (control slower), got %.2f", results.EffectSize)
	}

	// Check recommendation
	if results.Recommendation != SwitchToExperiment {
		t.Errorf("expected SwitchToExperiment, got %s", results.Recommendation)
	}
}

func TestHarness_GetResults_InsufficientSamples(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment,
		WithMinSamples(100),
	)

	// Add insufficient samples
	harness.RecordLatency(false, 10*time.Millisecond)
	harness.RecordLatency(true, 5*time.Millisecond)

	results := harness.GetResults()
	if results.Recommendation != NeedMoreData {
		t.Errorf("expected NeedMoreData, got %s", results.Recommendation)
	}
}

// -----------------------------------------------------------------------------
// Reset Tests
// -----------------------------------------------------------------------------

func TestHarness_Reset(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	// Add some data
	for i := 0; i < 10; i++ {
		harness.RecordLatency(false, time.Duration(i)*time.Millisecond)
		harness.RecordLatency(true, time.Duration(i)*time.Millisecond)
	}

	harness.Reset()

	results := harness.GetResults()
	if results.ControlSamples != 0 {
		t.Errorf("expected 0 control samples after reset, got %d", results.ControlSamples)
	}
	if results.ExperimentSamples != 0 {
		t.Errorf("expected 0 experiment samples after reset, got %d", results.ExperimentSamples)
	}
}

// -----------------------------------------------------------------------------
// SetSampleRate Tests
// -----------------------------------------------------------------------------

func TestHarness_SetSampleRate(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment, WithSampleRate(0.1))

	harness.SetSampleRate(0.5)

	// Rate should be updated
	if rate := harness.sampler.Rate(); rate != 0.5 {
		t.Errorf("expected rate 0.5, got %f", rate)
	}
}

// -----------------------------------------------------------------------------
// Evaluable Implementation Tests
// -----------------------------------------------------------------------------

func TestHarness_Name(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	expectedName := "ab_harness_control_vs_experiment"
	if harness.Name() != expectedName {
		t.Errorf("expected name %s, got %s", expectedName, harness.Name())
	}
}

func TestHarness_Properties(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	props := harness.Properties()
	if len(props) == 0 {
		t.Error("expected at least one property")
	}

	// Check property names
	foundSampling := false
	foundCorrectness := false
	for _, p := range props {
		if p.Name == "sampling_rate_respected" {
			foundSampling = true
		}
		if p.Name == "correctness_tracked" {
			foundCorrectness = true
		}
	}
	if !foundSampling {
		t.Error("expected sampling_rate_respected property")
	}
	if !foundCorrectness {
		t.Error("expected correctness_tracked property")
	}
}

func TestHarness_Metrics(t *testing.T) {
	control := newMockEvaluable("control")
	experiment := newMockEvaluable("experiment")

	harness, _ := NewHarness(control, experiment)

	metrics := harness.Metrics()
	if len(metrics) == 0 {
		t.Error("expected at least one metric")
	}

	// Check for latency metrics
	foundControlLatency := false
	foundExpLatency := false
	for _, m := range metrics {
		if m.Name == "ab_harness_control_latency_seconds" {
			foundControlLatency = true
			if m.Type != eval.MetricHistogram {
				t.Errorf("expected histogram type for latency metric")
			}
		}
		if m.Name == "ab_harness_experiment_latency_seconds" {
			foundExpLatency = true
		}
	}
	if !foundControlLatency {
		t.Error("expected control latency metric")
	}
	if !foundExpLatency {
		t.Error("expected experiment latency metric")
	}
}

func TestHarness_HealthCheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment)

		err := harness.HealthCheck(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unhealthy control", func(t *testing.T) {
		control := &mockEvaluable{name: "control", healthErr: errors.New("unhealthy")}
		experiment := newMockEvaluable("experiment")

		harness, _ := NewHarness(control, experiment)

		err := harness.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for unhealthy control")
		}
	})

	t.Run("unhealthy experiment", func(t *testing.T) {
		control := newMockEvaluable("control")
		experiment := &mockEvaluable{name: "experiment", healthErr: errors.New("unhealthy")}

		harness, _ := NewHarness(control, experiment)

		err := harness.HealthCheck(context.Background())
		if err == nil {
			t.Error("expected error for unhealthy experiment")
		}
	})
}

// -----------------------------------------------------------------------------
// Results Helper Tests
// -----------------------------------------------------------------------------

func TestResults_Significant(t *testing.T) {
	t.Run("significant", func(t *testing.T) {
		results := &Results{
			TTest: &TTestResult{Significant: true, PValue: 0.01},
		}
		if !results.Significant() {
			t.Error("expected significant")
		}
	})

	t.Run("not significant", func(t *testing.T) {
		results := &Results{
			TTest: &TTestResult{Significant: false, PValue: 0.10},
		}
		if results.Significant() {
			t.Error("expected not significant")
		}
	})

	t.Run("nil ttest", func(t *testing.T) {
		results := &Results{}
		if results.Significant() {
			t.Error("expected not significant with nil t-test")
		}
	})
}

func TestResults_ExperimentBetter(t *testing.T) {
	t.Run("experiment better", func(t *testing.T) {
		results := &Results{Recommendation: SwitchToExperiment}
		if !results.ExperimentBetter() {
			t.Error("expected experiment better")
		}
	})

	t.Run("control better", func(t *testing.T) {
		results := &Results{Recommendation: KeepControl}
		if results.ExperimentBetter() {
			t.Error("expected control better")
		}
	})
}

// -----------------------------------------------------------------------------
// outputsMatch Tests
// -----------------------------------------------------------------------------

func TestOutputsMatch(t *testing.T) {
	tests := []struct {
		name     string
		a        any
		b        any
		expected bool
	}{
		{"both nil", nil, nil, true},
		{"a nil", nil, "value", false},
		{"b nil", "value", nil, false},
		{"equal strings", "test", "test", true},
		{"different strings", "test1", "test2", false},
		{"equal ints", 42, 42, true},
		{"different ints", 42, 43, false},
		{"equal slices", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"different slices", []int{1, 2, 3}, []int{1, 2, 4}, false},
		{"equal maps", map[string]int{"a": 1}, map[string]int{"a": 1}, true},
		{"different maps", map[string]int{"a": 1}, map[string]int{"a": 2}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outputsMatch(tt.a, tt.b); got != tt.expected {
				t.Errorf("outputsMatch(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}
