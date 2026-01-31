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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.Count() != 0 {
		t.Errorf("New registry should be empty, got count %d", r.Count())
	}
}

func TestRegistry_Register(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		r := NewRegistry()
		e := NewSimpleEvaluable("test_component")

		err := r.Register(e)
		if err != nil {
			t.Errorf("Register failed: %v", err)
		}

		if r.Count() != 1 {
			t.Errorf("Count = %d, want 1", r.Count())
		}
	})

	t.Run("nil component", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register(nil)
		if !errors.Is(err, ErrNilComponent) {
			t.Errorf("Expected ErrNilComponent, got %v", err)
		}
	})

	t.Run("duplicate registration", func(t *testing.T) {
		r := NewRegistry()
		e1 := NewSimpleEvaluable("duplicate")
		e2 := NewSimpleEvaluable("duplicate")

		if err := r.Register(e1); err != nil {
			t.Fatalf("First registration failed: %v", err)
		}

		err := r.Register(e2)
		if !errors.Is(err, ErrAlreadyRegistered) {
			t.Errorf("Expected ErrAlreadyRegistered, got %v", err)
		}
	})
}

func TestRegistry_MustRegister(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		r := NewRegistry()
		e := NewSimpleEvaluable("test")

		// Should not panic
		r.MustRegister(e)

		if r.Count() != 1 {
			t.Error("MustRegister failed to register")
		}
	})

	t.Run("panics on nil", func(t *testing.T) {
		r := NewRegistry()

		defer func() {
			if recover() == nil {
				t.Error("MustRegister(nil) should panic")
			}
		}()

		r.MustRegister(nil)
	})

	t.Run("panics on duplicate", func(t *testing.T) {
		r := NewRegistry()
		e := NewSimpleEvaluable("test")
		r.MustRegister(e)

		defer func() {
			if recover() == nil {
				t.Error("MustRegister duplicate should panic")
			}
		}()

		r.MustRegister(NewSimpleEvaluable("test"))
	})
}

func TestRegistry_Unregister(t *testing.T) {
	t.Run("successful unregistration", func(t *testing.T) {
		r := NewRegistry()
		e := NewSimpleEvaluable("test")
		r.MustRegister(e)

		err := r.Unregister("test")
		if err != nil {
			t.Errorf("Unregister failed: %v", err)
		}

		if r.Count() != 0 {
			t.Errorf("Count = %d, want 0", r.Count())
		}
	})

	t.Run("not found", func(t *testing.T) {
		r := NewRegistry()

		err := r.Unregister("nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	e := NewSimpleEvaluable("test")
	r.MustRegister(e)

	t.Run("found", func(t *testing.T) {
		got, ok := r.Get("test")
		if !ok {
			t.Error("Get should find registered component")
		}
		if got.Name() != "test" {
			t.Errorf("Got wrong component: %s", got.Name())
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := r.Get("nonexistent")
		if ok {
			t.Error("Get should not find unregistered component")
		}
	})
}

func TestRegistry_MustGet(t *testing.T) {
	r := NewRegistry()
	e := NewSimpleEvaluable("test")
	r.MustRegister(e)

	t.Run("found", func(t *testing.T) {
		got := r.MustGet("test")
		if got.Name() != "test" {
			t.Errorf("MustGet returned wrong component: %s", got.Name())
		}
	})

	t.Run("panics on not found", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("MustGet should panic for nonexistent component")
			}
		}()

		r.MustGet("nonexistent")
	})
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(NewSimpleEvaluable("charlie"))
	r.MustRegister(NewSimpleEvaluable("alpha"))
	r.MustRegister(NewSimpleEvaluable("bravo"))

	names := r.List()

	if len(names) != 3 {
		t.Errorf("List returned %d names, want 3", len(names))
	}

	// Should be sorted
	expected := []string{"alpha", "bravo", "charlie"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("List[%d] = %s, want %s", i, name, expected[i])
		}
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	e1 := NewSimpleEvaluable("one")
	e2 := NewSimpleEvaluable("two")
	r.MustRegister(e1)
	r.MustRegister(e2)

	all := r.All()

	if len(all) != 2 {
		t.Errorf("All returned %d components, want 2", len(all))
	}

	if all["one"].Name() != "one" || all["two"].Name() != "two" {
		t.Error("All returned wrong components")
	}

	// Verify it's a copy (modifications don't affect registry)
	delete(all, "one")
	if r.Count() != 2 {
		t.Error("All should return a copy, not the original map")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(NewSimpleEvaluable("one"))
	r.MustRegister(NewSimpleEvaluable("two"))

	r.Clear()

	if r.Count() != 0 {
		t.Errorf("Count = %d after Clear, want 0", r.Count())
	}
}

func TestRegistry_AddHook(t *testing.T) {
	r := NewRegistry()

	var registered []string
	var unregistered []string

	r.AddHook(func(name string, component Evaluable, isRegistered bool) {
		if isRegistered {
			registered = append(registered, name)
		} else {
			unregistered = append(unregistered, name)
		}
	})

	e := NewSimpleEvaluable("test")
	r.Register(e)

	if len(registered) != 1 || registered[0] != "test" {
		t.Errorf("Hook not called on registration: %v", registered)
	}

	r.Unregister("test")

	if len(unregistered) != 1 || unregistered[0] != "test" {
		t.Errorf("Hook not called on unregistration: %v", unregistered)
	}
}

func TestRegistry_HealthCheckAll(t *testing.T) {
	r := NewRegistry()

	// Healthy component
	healthy := NewSimpleEvaluable("healthy").
		SetHealthCheck(func(ctx context.Context) error {
			return nil
		})

	// Unhealthy component
	unhealthy := NewSimpleEvaluable("unhealthy").
		SetHealthCheck(func(ctx context.Context) error {
			return errors.New("something wrong")
		})

	// Slow component
	slow := NewSimpleEvaluable("slow").
		SetHealthCheck(func(ctx context.Context) error {
			select {
			case <-time.After(50 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})

	r.MustRegister(healthy)
	r.MustRegister(unhealthy)
	r.MustRegister(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := r.HealthCheckAll(ctx, 10)

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Results should be sorted by name
	resultMap := make(map[string]HealthResult)
	for _, r := range results {
		resultMap[r.Component] = r
	}

	if resultMap["healthy"].Status != HealthHealthy {
		t.Errorf("Healthy component reported as %v", resultMap["healthy"].Status)
	}

	if resultMap["unhealthy"].Status != HealthUnhealthy {
		t.Errorf("Unhealthy component reported as %v", resultMap["unhealthy"].Status)
	}

	if resultMap["slow"].Status != HealthHealthy {
		t.Errorf("Slow component reported as %v", resultMap["slow"].Status)
	}
}

func TestRegistry_HealthCheckAll_ContextCancelled(t *testing.T) {
	r := NewRegistry()

	// Component that takes a long time
	slow := NewSimpleEvaluable("slow").
		SetHealthCheck(func(ctx context.Context) error {
			select {
			case <-time.After(10 * time.Second):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})

	r.MustRegister(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := r.HealthCheckAll(ctx, 10)

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Should report as unhealthy or unknown due to timeout
	if results[0].Status == HealthHealthy {
		t.Error("Timed out component should not be healthy")
	}
}

func TestRegistry_GetAllProperties(t *testing.T) {
	r := NewRegistry()

	e1 := NewSimpleEvaluable("with_props").
		AddProperty(Property{
			Name:        "prop1",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
		})

	e2 := NewSimpleEvaluable("no_props")

	r.MustRegister(e1)
	r.MustRegister(e2)

	props := r.GetAllProperties()

	if len(props) != 1 {
		t.Errorf("Expected 1 component with properties, got %d", len(props))
	}

	if _, ok := props["with_props"]; !ok {
		t.Error("with_props should have properties")
	}

	if _, ok := props["no_props"]; ok {
		t.Error("no_props should not be in result")
	}
}

func TestRegistry_GetAllMetrics(t *testing.T) {
	r := NewRegistry()

	e1 := NewSimpleEvaluable("with_metrics").
		AddMetric(MetricDefinition{
			Name:        "metric1",
			Type:        MetricCounter,
			Description: "Test",
		})

	e2 := NewSimpleEvaluable("no_metrics")

	r.MustRegister(e1)
	r.MustRegister(e2)

	metrics := r.GetAllMetrics()

	if len(metrics) != 1 {
		t.Errorf("Expected 1 component with metrics, got %d", len(metrics))
	}

	if _, ok := metrics["with_metrics"]; !ok {
		t.Error("with_metrics should have metrics")
	}

	if _, ok := metrics["no_metrics"]; ok {
		t.Error("no_metrics should not be in result")
	}
}

func TestRegistry_FindByTag(t *testing.T) {
	r := NewRegistry()

	e1 := NewSimpleEvaluable("critical_algo").
		AddProperty(Property{
			Name:        "prop1",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Tags:        []string{"critical"},
		})

	e2 := NewSimpleEvaluable("perf_algo").
		AddProperty(Property{
			Name:        "prop1",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Tags:        []string{"performance"},
		})

	e3 := NewSimpleEvaluable("both_algo").
		AddProperty(Property{
			Name:        "prop1",
			Description: "Test",
			Check:       func(_, _ any) error { return nil },
			Tags:        []string{"critical", "performance"},
		})

	r.MustRegister(e1)
	r.MustRegister(e2)
	r.MustRegister(e3)

	critical := r.FindByTag("critical")
	if len(critical) != 2 {
		t.Errorf("Expected 2 critical components, got %d", len(critical))
	}

	performance := r.FindByTag("performance")
	if len(performance) != 2 {
		t.Errorf("Expected 2 performance components, got %d", len(performance))
	}

	nonexistent := r.FindByTag("nonexistent")
	if len(nonexistent) != 0 {
		t.Errorf("Expected 0 nonexistent components, got %d", len(nonexistent))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	var wg sync.WaitGroup
	var successCount atomic.Int32

	// Concurrent registrations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			e := NewSimpleEvaluable(formatInt(id))
			if err := r.Register(e); err == nil {
				successCount.Add(1)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.List()
			r.Count()
			r.All()
		}()
	}

	wg.Wait()

	if int(successCount.Load()) != r.Count() {
		t.Errorf("Success count %d != registry count %d", successCount.Load(), r.Count())
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Save and restore default registry
	original := DefaultRegistry
	DefaultRegistry = NewRegistry()
	defer func() { DefaultRegistry = original }()

	e := NewSimpleEvaluable("default_test")

	// Test package-level functions
	if err := Register(e); err != nil {
		t.Errorf("Register failed: %v", err)
	}

	if got, ok := Get("default_test"); !ok || got.Name() != "default_test" {
		t.Error("Get failed for default registry")
	}

	names := List()
	if len(names) != 1 || names[0] != "default_test" {
		t.Errorf("List = %v, want [default_test]", names)
	}
}

func TestRegistry_HookCalledOnClear(t *testing.T) {
	r := NewRegistry()

	var unregisteredNames []string
	r.AddHook(func(name string, component Evaluable, registered bool) {
		if !registered {
			unregisteredNames = append(unregisteredNames, name)
		}
	})

	r.MustRegister(NewSimpleEvaluable("one"))
	r.MustRegister(NewSimpleEvaluable("two"))

	r.Clear()

	if len(unregisteredNames) != 2 {
		t.Errorf("Expected 2 unregister hooks, got %d", len(unregisteredNames))
	}
}

// formatInt is a helper for generating unique names
func formatInt(n int) string {
	return "component_" + string(rune('0'+n/100)) + string(rune('0'+(n%100)/10)) + string(rune('0'+n%10))
}
