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
	"os"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Baseline Tests
// -----------------------------------------------------------------------------

func TestMemoryBaseline(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		baseline := &BaselineData{
			Component: "test",
			Version:   "1.0",
			Latency: LatencyBaseline{
				P50: 10 * time.Millisecond,
				P99: 100 * time.Millisecond,
			},
		}

		err := store.Set(ctx, "test", baseline)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		retrieved, err := store.Get(ctx, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if retrieved.Component != "test" {
			t.Errorf("expected component 'test', got '%s'", retrieved.Component)
		}
		if retrieved.Latency.P50 != 10*time.Millisecond {
			t.Errorf("expected P50 10ms, got %v", retrieved.Latency.P50)
		}
	})

	t.Run("not found", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		_, err := store.Get(ctx, "nonexistent")
		if err != ErrBaselineNotFound {
			t.Errorf("expected ErrBaselineNotFound, got %v", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		store.Set(ctx, "comp1", &BaselineData{Component: "comp1"})
		store.Set(ctx, "comp2", &BaselineData{Component: "comp2"})

		names, err := store.List(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(names) != 2 {
			t.Errorf("expected 2 names, got %d", len(names))
		}
	})

	t.Run("delete", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		store.Set(ctx, "test", &BaselineData{Component: "test"})

		err := store.Delete(ctx, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = store.Get(ctx, "test")
		if err != ErrBaselineNotFound {
			t.Errorf("expected ErrBaselineNotFound after delete")
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		err := store.Delete(ctx, "nonexistent")
		if err != ErrBaselineNotFound {
			t.Errorf("expected ErrBaselineNotFound, got %v", err)
		}
	})
}

func TestFileBaseline(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		dir := t.TempDir()
		store, err := NewFileBaseline(dir)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		ctx := context.Background()
		baseline := &BaselineData{
			Component: "test",
			Version:   "1.0",
			Latency: LatencyBaseline{
				P50: 10 * time.Millisecond,
			},
		}

		err = store.Set(ctx, "test", baseline)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		retrieved, err := store.Get(ctx, "test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if retrieved.Component != "test" {
			t.Errorf("expected component 'test', got '%s'", retrieved.Component)
		}
	})

	t.Run("file persisted", func(t *testing.T) {
		dir := t.TempDir()
		store, _ := NewFileBaseline(dir)
		ctx := context.Background()

		store.Set(ctx, "test", &BaselineData{Component: "test"})

		// Check file exists
		files, _ := os.ReadDir(dir)
		found := false
		for _, f := range files {
			if f.Name() == "test.json" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected test.json file to exist")
		}
	})
}

func TestBaselineBuilder(t *testing.T) {
	builder := NewBaselineBuilder("test_component", "1.0")

	baseline := builder.
		WithLatency(10*time.Millisecond, 50*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond, 5*time.Millisecond).
		WithThroughput(1000, 1024*1024).
		WithMemory(1024, 10, 10*1024*1024).
		WithErrorRate(0.01).
		WithSampleCount(1000).
		WithMetadata("branch", "main").
		Build()

	if baseline.Component != "test_component" {
		t.Errorf("expected component 'test_component', got '%s'", baseline.Component)
	}
	if baseline.Latency.P50 != 10*time.Millisecond {
		t.Errorf("expected P50 10ms, got %v", baseline.Latency.P50)
	}
	if baseline.Throughput.OpsPerSecond != 1000 {
		t.Errorf("expected throughput 1000, got %f", baseline.Throughput.OpsPerSecond)
	}
	if baseline.Memory.AllocBytesPerOp != 1024 {
		t.Errorf("expected memory 1024, got %d", baseline.Memory.AllocBytesPerOp)
	}
	if baseline.Error.Rate != 0.01 {
		t.Errorf("expected error rate 0.01, got %f", baseline.Error.Rate)
	}
	if baseline.SampleCount != 1000 {
		t.Errorf("expected sample count 1000, got %d", baseline.SampleCount)
	}
	if baseline.Metadata["branch"] != "main" {
		t.Errorf("expected metadata branch=main")
	}
}

// -----------------------------------------------------------------------------
// Detector Tests
// -----------------------------------------------------------------------------

func TestDetector_DetectLatencyRegression(t *testing.T) {
	config := DefaultDetectorConfig()
	config.LatencyP50Threshold = 0.10 // 10%
	detector := NewDetector(config)

	baseline := &BaselineData{
		Component: "test",
		Latency: LatencyBaseline{
			P50: 100 * time.Millisecond,
		},
	}

	t.Run("no regression", func(t *testing.T) {
		current := &CurrentMetrics{
			Latency: LatencyBaseline{
				P50: 105 * time.Millisecond, // 5% increase
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if !result.Pass {
			t.Error("expected pass with 5% increase (threshold 10%)")
		}
		if len(result.Regressions) > 0 {
			t.Errorf("expected no regressions, got %d", len(result.Regressions))
		}
	})

	t.Run("regression detected", func(t *testing.T) {
		current := &CurrentMetrics{
			Latency: LatencyBaseline{
				P50: 120 * time.Millisecond, // 20% increase
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if result.Pass {
			t.Error("expected fail with 20% increase (threshold 10%)")
		}
		if len(result.Regressions) != 1 {
			t.Errorf("expected 1 regression, got %d", len(result.Regressions))
		}
		if result.Regressions[0].Type != RegressionLatencyP50 {
			t.Errorf("expected latency P50 regression, got %s", result.Regressions[0].Type)
		}
	})

	t.Run("warning at threshold", func(t *testing.T) {
		current := &CurrentMetrics{
			Latency: LatencyBaseline{
				P50: 109 * time.Millisecond, // 9% increase (>80% of 10% threshold)
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if !result.Pass {
			t.Error("expected pass at warning level")
		}
		if len(result.Warnings) == 0 {
			t.Error("expected warning for 9% increase")
		}
	})
}

func TestDetector_DetectThroughputRegression(t *testing.T) {
	config := DefaultDetectorConfig()
	config.ThroughputThreshold = 0.05 // 5%
	detector := NewDetector(config)

	baseline := &BaselineData{
		Component: "test",
		Throughput: ThroughputBaseline{
			OpsPerSecond: 1000,
		},
	}

	t.Run("no regression", func(t *testing.T) {
		current := &CurrentMetrics{
			Throughput: ThroughputBaseline{
				OpsPerSecond: 980, // 2% decrease
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if !result.Pass {
			t.Error("expected pass with 2% decrease (threshold 5%)")
		}
	})

	t.Run("regression detected", func(t *testing.T) {
		current := &CurrentMetrics{
			Throughput: ThroughputBaseline{
				OpsPerSecond: 900, // 10% decrease
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if result.Pass {
			t.Error("expected fail with 10% decrease (threshold 5%)")
		}
		if len(result.Regressions) != 1 {
			t.Errorf("expected 1 regression, got %d", len(result.Regressions))
		}
	})
}

func TestDetector_DetectMemoryRegression(t *testing.T) {
	config := DefaultDetectorConfig()
	config.MemoryThreshold = 0.10 // 10%
	detector := NewDetector(config)

	baseline := &BaselineData{
		Component: "test",
		Memory: MemoryBaseline{
			AllocBytesPerOp: 1000,
		},
	}

	t.Run("no regression", func(t *testing.T) {
		current := &CurrentMetrics{
			Memory: MemoryBaseline{
				AllocBytesPerOp: 1050, // 5% increase
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if !result.Pass {
			t.Error("expected pass with 5% increase (threshold 10%)")
		}
	})

	t.Run("regression detected", func(t *testing.T) {
		current := &CurrentMetrics{
			Memory: MemoryBaseline{
				AllocBytesPerOp: 1200, // 20% increase
			},
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if result.Pass {
			t.Error("expected fail with 20% increase (threshold 10%)")
		}
	})
}

func TestDetector_DetectErrorRateRegression(t *testing.T) {
	config := DefaultDetectorConfig()
	config.ErrorRateThreshold = 0.01 // 1%
	detector := NewDetector(config)

	baseline := &BaselineData{
		Component: "test",
		Error:     ErrorBaseline{Rate: 0.01}, // 1% baseline
	}

	t.Run("no regression", func(t *testing.T) {
		current := &CurrentMetrics{
			ErrorRate:   0.015, // 1.5% (0.5% absolute increase)
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if !result.Pass {
			t.Error("expected pass with 0.5% absolute increase (threshold 1%)")
		}
	})

	t.Run("regression detected", func(t *testing.T) {
		current := &CurrentMetrics{
			ErrorRate:   0.03, // 3% (2% absolute increase)
			SampleCount: 100,
		}

		result := detector.Detect(baseline, current)

		if result.Pass {
			t.Error("expected fail with 2% absolute increase (threshold 1%)")
		}
		if result.MaxSeverity != SeverityCritical {
			t.Errorf("expected critical severity for error rate regression")
		}
	})
}

// -----------------------------------------------------------------------------
// Gate Tests
// -----------------------------------------------------------------------------

func TestGate_Check(t *testing.T) {
	t.Run("pass when no regression", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		baseline := &BaselineData{
			Component: "test",
			Latency:   LatencyBaseline{P50: 100 * time.Millisecond},
		}
		store.Set(ctx, "test", baseline)

		gate := NewGate(store, WithLatencyThreshold(0.10))

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 105 * time.Millisecond},
			SampleCount: 100,
		}

		decision, err := gate.Check(ctx, "test", current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !decision.Pass {
			t.Error("expected pass")
		}
	})

	t.Run("fail when regression detected", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		baseline := &BaselineData{
			Component: "test",
			Latency:   LatencyBaseline{P50: 100 * time.Millisecond},
		}
		store.Set(ctx, "test", baseline)

		gate := NewGate(store, WithLatencyThreshold(0.10))

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 150 * time.Millisecond},
			SampleCount: 100,
		}

		decision, err := gate.Check(ctx, "test", current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if decision.Pass {
			t.Error("expected fail with 50% regression")
		}
		if len(decision.Regressions) == 0 {
			t.Error("expected regressions in decision")
		}
	})

	t.Run("pass when no baseline (first run)", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		gate := NewGate(store)

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 100 * time.Millisecond},
			SampleCount: 100,
		}

		decision, err := gate.Check(ctx, "new_component", current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !decision.Pass {
			t.Error("expected pass on first run")
		}
	})

	t.Run("fail when baseline required but missing", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		gate := NewGate(store, WithRequireBaseline(true))

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 100 * time.Millisecond},
			SampleCount: 100,
		}

		decision, err := gate.Check(ctx, "new_component", current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if decision.Pass {
			t.Error("expected fail when baseline required")
		}
	})

	t.Run("update baseline on pass", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		baseline := &BaselineData{
			Component: "test",
			Latency:   LatencyBaseline{P50: 100 * time.Millisecond},
		}
		store.Set(ctx, "test", baseline)

		gate := NewGate(store, WithUpdateBaseline(true))

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 90 * time.Millisecond}, // Faster!
			SampleCount: 100,
		}

		decision, err := gate.Check(ctx, "test", current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !decision.BaselineUpdated {
			t.Error("expected baseline to be updated")
		}

		// Verify baseline was updated
		updated, _ := store.Get(ctx, "test")
		if updated.Latency.P50 != 90*time.Millisecond {
			t.Errorf("expected updated P50 90ms, got %v", updated.Latency.P50)
		}
	})

	t.Run("report generated", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()

		baseline := &BaselineData{
			Component:  "test",
			Latency:    LatencyBaseline{P50: 100 * time.Millisecond, P99: 500 * time.Millisecond},
			Throughput: ThroughputBaseline{OpsPerSecond: 1000},
			Memory:     MemoryBaseline{AllocBytesPerOp: 1024},
			Error:      ErrorBaseline{Rate: 0.01},
		}
		store.Set(ctx, "test", baseline)

		gate := NewGate(store)

		current := &CurrentMetrics{
			Latency:     LatencyBaseline{P50: 105 * time.Millisecond, P99: 520 * time.Millisecond},
			Throughput:  ThroughputBaseline{OpsPerSecond: 980},
			Memory:      MemoryBaseline{AllocBytesPerOp: 1050},
			ErrorRate:   0.012,
			SampleCount: 100,
		}

		decision, _ := gate.Check(ctx, "test", current)

		if decision.Report == "" {
			t.Error("expected non-empty report")
		}
		if len(decision.Report) < 100 {
			t.Error("report seems too short")
		}
	})

	t.Run("nil context error", func(t *testing.T) {
		store := NewMemoryBaseline()
		gate := NewGate(store)

		_, err := gate.Check(nil, "test", &CurrentMetrics{})
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil metrics error", func(t *testing.T) {
		store := NewMemoryBaseline()
		ctx := context.Background()
		gate := NewGate(store)

		_, err := gate.Check(ctx, "test", nil)
		if err == nil {
			t.Error("expected error for nil metrics")
		}
	})
}

func TestGate_CheckAll(t *testing.T) {
	store := NewMemoryBaseline()
	ctx := context.Background()

	store.Set(ctx, "comp1", &BaselineData{Component: "comp1", Latency: LatencyBaseline{P50: 100 * time.Millisecond}})
	store.Set(ctx, "comp2", &BaselineData{Component: "comp2", Latency: LatencyBaseline{P50: 200 * time.Millisecond}})

	gate := NewGate(store)

	components := map[string]*CurrentMetrics{
		"comp1": {Latency: LatencyBaseline{P50: 105 * time.Millisecond}, SampleCount: 100},
		"comp2": {Latency: LatencyBaseline{P50: 210 * time.Millisecond}, SampleCount: 100},
	}

	decisions, err := gate.CheckAll(ctx, components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(decisions))
	}

	if !decisions["comp1"].Pass {
		t.Error("expected comp1 to pass")
	}
	if !decisions["comp2"].Pass {
		t.Error("expected comp2 to pass")
	}
}

// -----------------------------------------------------------------------------
// Regression Type Tests
// -----------------------------------------------------------------------------

func TestRegressionType_String(t *testing.T) {
	tests := []struct {
		rt       RegressionType
		expected string
	}{
		{RegressionNone, "none"},
		{RegressionLatencyP50, "latency_p50"},
		{RegressionLatencyP95, "latency_p95"},
		{RegressionLatencyP99, "latency_p99"},
		{RegressionThroughput, "throughput"},
		{RegressionMemory, "memory"},
		{RegressionErrorRate, "error_rate"},
		{RegressionType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.rt.String(); got != tt.expected {
			t.Errorf("%d.String() = %s, want %s", tt.rt, got, tt.expected)
		}
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		s        Severity
		expected string
	}{
		{SeverityNone, "none"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.s.String(); got != tt.expected {
			t.Errorf("%d.String() = %s, want %s", tt.s, got, tt.expected)
		}
	}
}

// -----------------------------------------------------------------------------
// Evaluable Implementation Tests
// -----------------------------------------------------------------------------

func TestGate_Evaluable(t *testing.T) {
	store := NewMemoryBaseline()
	gate := NewGate(store)

	if name := gate.Name(); name != "regression_gate" {
		t.Errorf("expected name 'regression_gate', got '%s'", name)
	}

	props := gate.Properties()
	if len(props) == 0 {
		t.Error("expected at least one property")
	}

	metrics := gate.Metrics()
	if len(metrics) == 0 {
		t.Error("expected at least one metric")
	}

	ctx := context.Background()
	if err := gate.HealthCheck(ctx); err != nil {
		t.Errorf("unexpected health check error: %v", err)
	}
}
