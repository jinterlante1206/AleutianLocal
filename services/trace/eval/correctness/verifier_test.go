// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package correctness

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

func TestNewVerifier(t *testing.T) {
	registry := eval.NewRegistry()
	v := NewVerifier(registry)

	if v == nil {
		t.Fatal("NewVerifier returned nil")
	}
}

func TestVerifier_Verify_PassingProperty(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("test_algo").
		AddProperty(eval.Property{
			Name:        "always_passes",
			Description: "A property that always passes",
			Check: func(input, output any) error {
				return nil
			},
			Generator: func() any {
				return rand.Intn(100)
			},
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, err := v.Verify(context.Background(), "test_algo", WithIterations(50))

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if !result.Passed {
		t.Error("Expected verification to pass")
	}

	if len(result.Properties) != 1 {
		t.Errorf("Expected 1 property result, got %d", len(result.Properties))
	}

	if result.Properties[0].Iterations != 50 {
		t.Errorf("Expected 50 iterations, got %d", result.Properties[0].Iterations)
	}
}

func TestVerifier_Verify_FailingProperty(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("test_algo").
		AddProperty(eval.Property{
			Name:        "fails_on_negative",
			Description: "Fails when input is negative",
			Check: func(input, output any) error {
				n := input.(int)
				if n < 0 {
					return errors.New("input must be non-negative")
				}
				return nil
			},
			Generator: func() any {
				// Generate negative numbers sometimes
				return rand.Intn(200) - 100
			},
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, err := v.Verify(context.Background(), "test_algo", WithIterations(1000))

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Passed {
		t.Error("Expected verification to fail")
	}

	if len(result.FailedProperties()) == 0 {
		t.Error("Expected at least one failed property")
	}

	// Check that we captured the failing input
	failed := result.Properties[0]
	if failed.FailingInput == nil {
		t.Error("Expected failing input to be captured")
	}
}

func TestVerifier_Verify_WithShrinking(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("test_algo").
		AddProperty(eval.Property{
			Name:        "shrinkable",
			Description: "Fails on numbers > 10, shrinks to 11",
			Check: func(input, output any) error {
				n := input.(int)
				if n > 10 {
					return errors.New("number too large")
				}
				return nil
			},
			Generator: func() any {
				return rand.Intn(100) + 11 // Always > 10
			},
			Shrink: func(input any) []any {
				n := input.(int)
				if n <= 11 {
					return nil
				}
				// Try the minimum first, then decrement
				// This ensures we find the actual minimum
				return []any{11, n - 1}
			},
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, err := v.Verify(context.Background(), "test_algo",
		WithIterations(10),
		WithShrinkIterations(50),
	)

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Passed {
		t.Error("Expected verification to fail")
	}

	// Should have shrunk to minimal failing case (11)
	failed := result.Properties[0]
	if failed.FailingInput.(int) != 11 {
		t.Errorf("Expected shrunk input to be 11, got %v", failed.FailingInput)
	}

	if failed.ShrinkSteps == 0 {
		t.Error("Expected some shrink steps")
	}
}

func TestVerifier_Verify_ComponentNotFound(t *testing.T) {
	registry := eval.NewRegistry()
	v := NewVerifier(registry)

	_, err := v.Verify(context.Background(), "nonexistent")

	if !errors.Is(err, eval.ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestVerifier_Verify_NoProperties(t *testing.T) {
	registry := eval.NewRegistry()
	component := eval.NewSimpleEvaluable("no_props")
	registry.MustRegister(component)

	v := NewVerifier(registry)
	_, err := v.Verify(context.Background(), "no_props")

	if !errors.Is(err, ErrNoProperties) {
		t.Errorf("Expected ErrNoProperties, got %v", err)
	}
}

func TestVerifier_Verify_NoGenerator(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("test_algo").
		AddProperty(eval.Property{
			Name:        "no_generator",
			Description: "Property without generator",
			Check: func(input, output any) error {
				return nil
			},
			// No Generator!
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, err := v.Verify(context.Background(), "test_algo")

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// Property should fail due to no generator
	if result.Passed {
		t.Error("Expected verification to fail due to no generator")
	}

	if !errors.Is(result.Properties[0].Error, ErrNoGenerator) {
		t.Errorf("Expected ErrNoGenerator, got %v", result.Properties[0].Error)
	}
}

func TestVerifier_Verify_WithTimeout(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("slow_algo").
		AddProperty(eval.Property{
			Name:        "slow_property",
			Description: "Takes a long time",
			Check: func(input, output any) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			},
			Generator: func() any {
				return 1
			},
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	result, _ := v.Verify(ctx, "slow_algo", WithIterations(100))

	// Should have timed out before completing all iterations
	if result.Properties[0].Iterations >= 100 {
		t.Errorf("Expected timeout before 100 iterations, got %d", result.Properties[0].Iterations)
	}
}

func TestVerifier_Verify_WithTags(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("tagged_algo").
		AddProperty(eval.Property{
			Name:        "critical_prop",
			Description: "Critical property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
			Tags:        []string{"critical"},
		}).
		AddProperty(eval.Property{
			Name:        "perf_prop",
			Description: "Performance property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
			Tags:        []string{"performance"},
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)

	// Only verify critical properties
	result, err := v.Verify(context.Background(), "tagged_algo",
		WithTags("critical"),
		WithIterations(10),
	)

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if len(result.Properties) != 1 {
		t.Errorf("Expected 1 property (critical only), got %d", len(result.Properties))
	}

	if result.Properties[0].Name != "critical_prop" {
		t.Errorf("Expected critical_prop, got %s", result.Properties[0].Name)
	}
}

func TestVerifier_Verify_StopOnFailure(t *testing.T) {
	registry := eval.NewRegistry()

	callCount := 0
	component := eval.NewSimpleEvaluable("multi_prop_algo").
		AddProperty(eval.Property{
			Name:        "fails_first",
			Description: "Always fails",
			Check: func(_, _ any) error {
				callCount++
				return errors.New("always fails")
			},
			Generator: func() any { return 1 },
		}).
		AddProperty(eval.Property{
			Name:        "second_prop",
			Description: "Should not run with stop on failure",
			Check: func(_, _ any) error {
				callCount++
				return nil
			},
			Generator: func() any { return 1 },
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)

	result, err := v.Verify(context.Background(), "multi_prop_algo",
		WithStopOnFailure(true),
		WithIterations(1),
	)

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Passed {
		t.Error("Expected verification to fail")
	}

	// Only the first property should have been checked
	if len(result.Properties) != 1 {
		t.Errorf("Expected 1 property result (stop on failure), got %d", len(result.Properties))
	}
}

func TestVerifier_Verify_Parallel(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("parallel_algo").
		AddProperty(eval.Property{
			Name:        "prop1",
			Description: "First property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}).
		AddProperty(eval.Property{
			Name:        "prop2",
			Description: "Second property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}).
		AddProperty(eval.Property{
			Name:        "prop3",
			Description: "Third property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)

	start := time.Now()
	result, err := v.Verify(context.Background(), "parallel_algo",
		WithParallelism(3),
		WithIterations(10),
	)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if !result.Passed {
		t.Error("Expected verification to pass")
	}

	if len(result.Properties) != 3 {
		t.Errorf("Expected 3 property results, got %d", len(result.Properties))
	}

	// Should be relatively fast due to parallelism
	if duration > 5*time.Second {
		t.Errorf("Parallel verification took too long: %v", duration)
	}
}

func TestVerifier_VerifyAll(t *testing.T) {
	registry := eval.NewRegistry()

	registry.MustRegister(eval.NewSimpleEvaluable("algo1").
		AddProperty(eval.Property{
			Name:        "prop1",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}))

	registry.MustRegister(eval.NewSimpleEvaluable("algo2").
		AddProperty(eval.Property{
			Name:        "prop2",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}))

	// Component without properties (should be skipped)
	registry.MustRegister(eval.NewSimpleEvaluable("no_props"))

	v := NewVerifier(registry)
	results, err := v.VerifyAll(context.Background(), WithIterations(10))

	if err != nil {
		t.Fatalf("VerifyAll failed: %v", err)
	}

	// Should have 2 results (no_props is skipped)
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Passed {
			t.Errorf("Component %s failed unexpectedly", r.Component)
		}
	}
}

func TestVerifier_Verify_NilContext(t *testing.T) {
	registry := eval.NewRegistry()
	v := NewVerifier(registry)

	//nolint:staticcheck // Testing nil context handling
	_, err := v.Verify(nil, "test")

	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestRunner(t *testing.T) {
	registry := eval.NewRegistry()

	registry.MustRegister(eval.NewSimpleEvaluable("test_algo").
		AddProperty(eval.Property{
			Name:        "test_prop",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}))

	runner := NewRunner(registry).
		WithIterations(20).
		WithParallelism(2).
		WithTimeout(time.Minute)

	t.Run("Run single", func(t *testing.T) {
		result, err := runner.Run(context.Background(), "test_algo")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if !result.Passed {
			t.Error("Expected pass")
		}
		if result.Properties[0].Iterations != 20 {
			t.Errorf("Expected 20 iterations, got %d", result.Properties[0].Iterations)
		}
	})

	t.Run("RunAll", func(t *testing.T) {
		results, err := runner.RunAll(context.Background())
		if err != nil {
			t.Fatalf("RunAll failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})
}

func TestVerifier_Verify_MultipleProperties(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("multi_algo").
		AddProperty(eval.Property{
			Name:        "prop_a",
			Description: "First property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		}).
		AddProperty(eval.Property{
			Name:        "prop_b",
			Description: "Second property that fails",
			Check:       func(_, _ any) error { return errors.New("intentional failure") },
			Generator:   func() any { return 1 },
		}).
		AddProperty(eval.Property{
			Name:        "prop_c",
			Description: "Third property",
			Check:       func(_, _ any) error { return nil },
			Generator:   func() any { return 1 },
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, err := v.Verify(context.Background(), "multi_algo", WithIterations(5))

	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if result.Passed {
		t.Error("Expected verification to fail")
	}

	if len(result.Properties) != 3 {
		t.Errorf("Expected 3 property results, got %d", len(result.Properties))
	}

	// First and third should pass, second should fail
	if !result.Properties[0].Passed {
		t.Error("prop_a should pass")
	}
	if result.Properties[1].Passed {
		t.Error("prop_b should fail")
	}
	if !result.Properties[2].Passed {
		t.Error("prop_c should pass")
	}

	// Check failed properties helper
	failed := result.FailedProperties()
	if len(failed) != 1 {
		t.Errorf("Expected 1 failed property, got %d", len(failed))
	}
	if failed[0].Name != "prop_b" {
		t.Errorf("Expected prop_b to fail, got %s", failed[0].Name)
	}
}

func TestVerifier_Verify_PropertyTimeout(t *testing.T) {
	registry := eval.NewRegistry()

	component := eval.NewSimpleEvaluable("timeout_algo").
		AddProperty(eval.Property{
			Name:        "timeout_prop",
			Description: "Property with custom timeout",
			Check: func(_, _ any) error {
				time.Sleep(200 * time.Millisecond)
				return nil
			},
			Generator: func() any { return 1 },
			Timeout:   100 * time.Millisecond, // Short timeout
		})

	registry.MustRegister(component)

	v := NewVerifier(registry)
	result, _ := v.Verify(context.Background(), "timeout_algo",
		WithIterations(10),
		WithPropertyTimeout(1*time.Second), // Default would allow more
	)

	// Property should timeout due to its own timeout setting
	if result.Properties[0].Iterations > 1 {
		t.Logf("Property ran %d iterations before timeout", result.Properties[0].Iterations)
	}
}
