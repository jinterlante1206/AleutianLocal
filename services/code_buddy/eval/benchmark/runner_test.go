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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

func TestNewRunner(t *testing.T) {
	registry := eval.NewRegistry()
	runner := NewRunner(registry)

	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}
	if runner.registry != registry {
		t.Error("Runner registry not set correctly")
	}
}

func TestRunOptions(t *testing.T) {
	t.Run("WithIterations", func(t *testing.T) {
		config := DefaultConfig()
		WithIterations(5000)(config)
		if config.Iterations != 5000 {
			t.Errorf("Iterations = %d, want 5000", config.Iterations)
		}
	})

	t.Run("WithIterations ignores non-positive", func(t *testing.T) {
		config := DefaultConfig()
		original := config.Iterations
		WithIterations(0)(config)
		if config.Iterations != original {
			t.Errorf("Iterations = %d, want %d", config.Iterations, original)
		}
	})

	t.Run("WithWarmup", func(t *testing.T) {
		config := DefaultConfig()
		WithWarmup(500)(config)
		if config.Warmup != 500 {
			t.Errorf("Warmup = %d, want 500", config.Warmup)
		}
	})

	t.Run("WithCooldown", func(t *testing.T) {
		config := DefaultConfig()
		WithCooldown(200 * time.Millisecond)(config)
		if config.Cooldown != 200*time.Millisecond {
			t.Errorf("Cooldown = %v, want 200ms", config.Cooldown)
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		config := DefaultConfig()
		WithTimeout(10 * time.Minute)(config)
		if config.Timeout != 10*time.Minute {
			t.Errorf("Timeout = %v, want 10m", config.Timeout)
		}
	})

	t.Run("WithIterationTimeout", func(t *testing.T) {
		config := DefaultConfig()
		WithIterationTimeout(5 * time.Second)(config)
		if config.IterationTimeout != 5*time.Second {
			t.Errorf("IterationTimeout = %v, want 5s", config.IterationTimeout)
		}
	})

	t.Run("WithMemoryCollection", func(t *testing.T) {
		config := DefaultConfig()
		WithMemoryCollection(false)(config)
		if config.CollectMemory != false {
			t.Errorf("CollectMemory = %v, want false", config.CollectMemory)
		}
	})

	t.Run("WithOutlierRemoval", func(t *testing.T) {
		config := DefaultConfig()
		WithOutlierRemoval(false)(config)
		if config.RemoveOutliers != false {
			t.Errorf("RemoveOutliers = %v, want false", config.RemoveOutliers)
		}
	})

	t.Run("WithOutlierThreshold", func(t *testing.T) {
		config := DefaultConfig()
		WithOutlierThreshold(2.0)(config)
		if config.OutlierThreshold != 2.0 {
			t.Errorf("OutlierThreshold = %v, want 2.0", config.OutlierThreshold)
		}
	})

	t.Run("WithParallelism", func(t *testing.T) {
		config := DefaultConfig()
		WithParallelism(4)(config)
		if config.Parallelism != 4 {
			t.Errorf("Parallelism = %d, want 4", config.Parallelism)
		}
	})

	t.Run("WithInputGenerator", func(t *testing.T) {
		config := DefaultConfig()
		gen := func() any { return "test" }
		WithInputGenerator(gen)(config)
		if config.InputGenerator == nil {
			t.Error("InputGenerator not set")
		}
	})
}

func TestRunner_Run(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		_, err := runner.Run(nil, "test")
		if err == nil {
			t.Error("Expected error for nil context")
		}
	})

	t.Run("component not found", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		_, err := runner.Run(context.Background(), "nonexistent")
		if !errors.Is(err, eval.ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		registry := eval.NewRegistry()
		component := eval.NewSimpleEvaluable("test")
		registry.MustRegister(component)

		runner := NewRunner(registry)

		// WithIterations(0) is ignored (non-positive), so use direct config manipulation
		// by setting parallelism to 0 which will fail validation
		_, err := runner.Run(context.Background(), "test", func(c *Config) {
			c.Parallelism = 0
		})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("Expected ErrInvalidConfig, got %v", err)
		}
	})

	t.Run("successful benchmark", func(t *testing.T) {
		registry := eval.NewRegistry()
		// Create component with measurable work
		component := eval.NewSimpleEvaluable("test").
			SetHealthCheck(func(ctx context.Context) error {
				time.Sleep(100 * time.Microsecond)
				return nil
			})
		registry.MustRegister(component)

		runner := NewRunner(registry)

		result, err := runner.Run(context.Background(), "test",
			WithIterations(100),
			WithWarmup(10),
			WithCooldown(0),
			WithMemoryCollection(true),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.Name != "test" {
			t.Errorf("Name = %s, want test", result.Name)
		}
		if result.Iterations < 100 {
			t.Errorf("Iterations = %d, want >= 100", result.Iterations)
		}
		if result.Latency.Min <= 0 {
			t.Errorf("Min latency should be positive: %v", result.Latency.Min)
		}
		if result.Latency.Mean <= 0 {
			t.Errorf("Mean latency should be positive: %v", result.Latency.Mean)
		}
		if result.Throughput.OpsPerSecond <= 0 {
			t.Errorf("OpsPerSecond should be positive: %v", result.Throughput.OpsPerSecond)
		}
		if result.Memory == nil {
			t.Error("Memory stats should be collected")
		}
	})

	t.Run("parallel benchmark", func(t *testing.T) {
		registry := eval.NewRegistry()
		component := eval.NewSimpleEvaluable("test")
		registry.MustRegister(component)

		runner := NewRunner(registry)

		result, err := runner.Run(context.Background(), "test",
			WithIterations(100),
			WithWarmup(0),
			WithCooldown(0),
			WithParallelism(4),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.Iterations < 100 {
			t.Errorf("Iterations = %d, want >= 100", result.Iterations)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		registry := eval.NewRegistry()
		component := eval.NewSimpleEvaluable("slow").
			SetHealthCheck(func(ctx context.Context) error {
				select {
				case <-time.After(1 * time.Second):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		registry.MustRegister(component)

		runner := NewRunner(registry)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := runner.Run(ctx, "slow",
			WithIterations(1000),
			WithWarmup(0),
		)

		// Should either return context error or have partial results
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected context deadline error, got %v", err)
		}
	})

	t.Run("with custom input generator", func(t *testing.T) {
		registry := eval.NewRegistry()
		var inputCount int
		component := eval.NewSimpleEvaluable("test").
			SetHealthCheck(func(ctx context.Context) error {
				return nil
			})
		registry.MustRegister(component)

		runner := NewRunner(registry)

		_, err := runner.Run(context.Background(), "test",
			WithIterations(10),
			WithWarmup(5),
			WithCooldown(0),
			WithInputGenerator(func() any {
				inputCount++
				return inputCount
			}),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// 5 warmup + 10 iterations = 15 calls
		if inputCount != 15 {
			t.Errorf("Input generator called %d times, want 15", inputCount)
		}
	})
}

func TestRunner_Compare(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		_, err := runner.Compare(nil, []string{"a", "b"})
		if err == nil {
			t.Error("Expected error for nil context")
		}
	})

	t.Run("insufficient components", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		_, err := runner.Compare(context.Background(), []string{"only_one"})
		if err == nil {
			t.Error("Expected error for single component")
		}
	})

	t.Run("successful comparison", func(t *testing.T) {
		registry := eval.NewRegistry()

		// Fast component
		fast := eval.NewSimpleEvaluable("fast").
			SetHealthCheck(func(ctx context.Context) error {
				return nil
			})

		// Slow component
		slow := eval.NewSimpleEvaluable("slow").
			SetHealthCheck(func(ctx context.Context) error {
				time.Sleep(1 * time.Millisecond)
				return nil
			})

		registry.MustRegister(fast)
		registry.MustRegister(slow)

		runner := NewRunner(registry)

		comparison, err := runner.Compare(context.Background(),
			[]string{"fast", "slow"},
			WithIterations(50),
			WithWarmup(10),
			WithCooldown(0),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(comparison.Results) != 2 {
			t.Errorf("Expected 2 results, got %d", len(comparison.Results))
		}

		if len(comparison.Ranking) != 2 {
			t.Errorf("Expected ranking of 2, got %d", len(comparison.Ranking))
		}

		// Fast should be ranked first
		if comparison.Ranking[0] != "fast" {
			t.Errorf("Expected 'fast' to be ranked first, got %s", comparison.Ranking[0])
		}

		if comparison.Speedup <= 1.0 {
			t.Errorf("Speedup should be > 1.0, got %v", comparison.Speedup)
		}
	})

	t.Run("component not found", func(t *testing.T) {
		registry := eval.NewRegistry()
		component := eval.NewSimpleEvaluable("exists")
		registry.MustRegister(component)

		runner := NewRunner(registry)

		_, err := runner.Compare(context.Background(), []string{"exists", "nonexistent"})
		if !errors.Is(err, eval.ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})
}

func TestRunner_RunAll(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		_, err := runner.RunAll(nil)
		if err == nil {
			t.Error("Expected error for nil context")
		}
	})

	t.Run("empty registry", func(t *testing.T) {
		registry := eval.NewRegistry()
		runner := NewRunner(registry)

		results, err := runner.RunAll(context.Background())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("multiple components", func(t *testing.T) {
		registry := eval.NewRegistry()
		registry.MustRegister(eval.NewSimpleEvaluable("one"))
		registry.MustRegister(eval.NewSimpleEvaluable("two"))
		registry.MustRegister(eval.NewSimpleEvaluable("three"))

		runner := NewRunner(registry)

		results, err := runner.RunAll(context.Background(),
			WithIterations(10),
			WithWarmup(0),
			WithCooldown(0),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(results) != 3 {
			t.Errorf("Expected 3 results, got %d", len(results))
		}
	})

	t.Run("continues on failure", func(t *testing.T) {
		registry := eval.NewRegistry()

		good := eval.NewSimpleEvaluable("good")
		bad := eval.NewSimpleEvaluable("bad").
			SetHealthCheck(func(ctx context.Context) error {
				return errors.New("always fails")
			})
		good2 := eval.NewSimpleEvaluable("good2")

		registry.MustRegister(good)
		registry.MustRegister(bad)
		registry.MustRegister(good2)

		runner := NewRunner(registry)

		results, err := runner.RunAll(context.Background(),
			WithIterations(10),
			WithWarmup(0),
			WithCooldown(0),
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Should have results for the successful components
		// The bad component will either be skipped or have high error rate
		if len(results) < 2 {
			t.Errorf("Expected at least 2 successful results, got %d", len(results))
		}
	})
}
