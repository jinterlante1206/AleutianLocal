// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package crs

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestDefaultTraceConfig(t *testing.T) {
	config := DefaultTraceConfig()

	if config.MaxSteps != 1000 {
		t.Errorf("MaxSteps = %d, want 1000", config.MaxSteps)
	}
	if !config.RecordSymbols {
		t.Error("RecordSymbols should be true by default")
	}
	if !config.RecordMetadata {
		t.Error("RecordMetadata should be true by default")
	}
}

func TestNewTraceRecorder(t *testing.T) {
	t.Run("uses default MaxSteps when zero", func(t *testing.T) {
		recorder := NewTraceRecorder(TraceConfig{MaxSteps: 0})
		if recorder.config.MaxSteps != 1000 {
			t.Errorf("MaxSteps = %d, want 1000", recorder.config.MaxSteps)
		}
	})

	t.Run("respects custom MaxSteps", func(t *testing.T) {
		recorder := NewTraceRecorder(TraceConfig{MaxSteps: 50})
		if recorder.config.MaxSteps != 50 {
			t.Errorf("MaxSteps = %d, want 50", recorder.config.MaxSteps)
		}
	})
}

func TestTraceRecorder_RecordStep(t *testing.T) {
	t.Run("assigns step numbers", func(t *testing.T) {
		recorder := NewTraceRecorder(DefaultTraceConfig())

		recorder.RecordStep(TraceStep{Action: "first"})
		recorder.RecordStep(TraceStep{Action: "second"})
		recorder.RecordStep(TraceStep{Action: "third"})

		steps := recorder.GetSteps()

		if len(steps) != 3 {
			t.Fatalf("Step count = %d, want 3", len(steps))
		}
		if steps[0].Step != 1 {
			t.Errorf("First step number = %d, want 1", steps[0].Step)
		}
		if steps[1].Step != 2 {
			t.Errorf("Second step number = %d, want 2", steps[1].Step)
		}
		if steps[2].Step != 3 {
			t.Errorf("Third step number = %d, want 3", steps[2].Step)
		}
	})

	t.Run("sets timestamp if not provided", func(t *testing.T) {
		recorder := NewTraceRecorder(DefaultTraceConfig())

		before := time.Now()
		recorder.RecordStep(TraceStep{Action: "test"})
		after := time.Now()

		steps := recorder.GetSteps()
		if steps[0].Timestamp.Before(before) || steps[0].Timestamp.After(after) {
			t.Error("Timestamp should be set to current time")
		}
	})

	t.Run("preserves provided timestamp", func(t *testing.T) {
		recorder := NewTraceRecorder(DefaultTraceConfig())

		customTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		recorder.RecordStep(TraceStep{Action: "test", Timestamp: customTime})

		steps := recorder.GetSteps()
		if !steps[0].Timestamp.Equal(customTime) {
			t.Error("Custom timestamp should be preserved")
		}
	})

	t.Run("evicts oldest when at capacity", func(t *testing.T) {
		recorder := NewTraceRecorder(TraceConfig{MaxSteps: 3})

		recorder.RecordStep(TraceStep{Action: "first"})
		recorder.RecordStep(TraceStep{Action: "second"})
		recorder.RecordStep(TraceStep{Action: "third"})
		recorder.RecordStep(TraceStep{Action: "fourth"})

		steps := recorder.GetSteps()

		if len(steps) != 3 {
			t.Fatalf("Step count = %d, want 3", len(steps))
		}
		// First step should have been evicted
		if steps[0].Action != "second" {
			t.Errorf("First remaining step = %q, want %q", steps[0].Action, "second")
		}
		// Step numbers are monotonically increasing and never reset
		// "second" was recorded as step 2, so it remains step 2
		if steps[0].Step != 2 {
			t.Errorf("Second step number = %d, want 2", steps[0].Step)
		}
		// "third" was recorded as step 3
		if steps[1].Step != 3 {
			t.Errorf("Third step number = %d, want 3", steps[1].Step)
		}
		// "fourth" was recorded as step 4 (monotonically increasing)
		if steps[2].Step != 4 {
			t.Errorf("Fourth step number = %d, want 4", steps[2].Step)
		}
	})

	t.Run("respects RecordSymbols config", func(t *testing.T) {
		recorder := NewTraceRecorder(TraceConfig{
			MaxSteps:      100,
			RecordSymbols: false,
		})

		recorder.RecordStep(TraceStep{
			Action:       "test",
			SymbolsFound: []string{"sym1", "sym2"},
		})

		steps := recorder.GetSteps()
		if steps[0].SymbolsFound != nil {
			t.Error("SymbolsFound should be nil when RecordSymbols is false")
		}
	})

	t.Run("respects RecordMetadata config", func(t *testing.T) {
		recorder := NewTraceRecorder(TraceConfig{
			MaxSteps:       100,
			RecordMetadata: false,
		})

		recorder.RecordStep(TraceStep{
			Action:   "test",
			Metadata: map[string]string{"key": "value"},
		})

		steps := recorder.GetSteps()
		if steps[0].Metadata != nil {
			t.Error("Metadata should be nil when RecordMetadata is false")
		}
	})
}

func TestTraceRecorder_GetSteps(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	recorder.RecordStep(TraceStep{Action: "first"})
	recorder.RecordStep(TraceStep{Action: "second"})

	// Get steps twice and verify independence
	steps1 := recorder.GetSteps()
	steps2 := recorder.GetSteps()

	// Modify steps1
	steps1[0].Action = "modified"

	// steps2 should be unaffected
	if steps2[0].Action == "modified" {
		t.Error("GetSteps should return a copy, not the original slice")
	}
}

func TestTraceRecorder_StepCount(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	if recorder.StepCount() != 0 {
		t.Errorf("Initial step count = %d, want 0", recorder.StepCount())
	}

	recorder.RecordStep(TraceStep{Action: "first"})
	recorder.RecordStep(TraceStep{Action: "second"})

	if recorder.StepCount() != 2 {
		t.Errorf("Step count = %d, want 2", recorder.StepCount())
	}
}

func TestTraceRecorder_Clear(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	recorder.RecordStep(TraceStep{Action: "first"})
	recorder.RecordStep(TraceStep{Action: "second"})

	recorder.Clear()

	if recorder.StepCount() != 0 {
		t.Errorf("Step count after clear = %d, want 0", recorder.StepCount())
	}
}

func TestTraceRecorder_Export(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC)

	recorder.RecordStep(TraceStep{Action: "first", Timestamp: t1})
	recorder.RecordStep(TraceStep{Action: "second", Timestamp: t2})

	trace := recorder.Export("test-session")

	if trace.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", trace.SessionID, "test-session")
	}
	if trace.TotalSteps != 2 {
		t.Errorf("TotalSteps = %d, want 2", trace.TotalSteps)
	}
	if !trace.StartTime.Equal(t1) {
		t.Errorf("StartTime = %v, want %v", trace.StartTime, t1)
	}
	if !trace.EndTime.Equal(t2) {
		t.Errorf("EndTime = %v, want %v", trace.EndTime, t2)
	}
	if trace.Duration != "5m0s" {
		t.Errorf("Duration = %q, want %q", trace.Duration, "5m0s")
	}
}

func TestTraceRecorder_Export_Empty(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	trace := recorder.Export("empty-session")

	if trace.TotalSteps != 0 {
		t.Errorf("TotalSteps = %d, want 0", trace.TotalSteps)
	}
	if trace.Duration != "0s" {
		t.Errorf("Duration = %q, want %q", trace.Duration, "0s")
	}
}

func TestTraceRecorder_LastStep(t *testing.T) {
	t.Run("returns nil when empty", func(t *testing.T) {
		recorder := NewTraceRecorder(DefaultTraceConfig())
		if recorder.LastStep() != nil {
			t.Error("LastStep should return nil when empty")
		}
	})

	t.Run("returns copy of last step", func(t *testing.T) {
		recorder := NewTraceRecorder(DefaultTraceConfig())

		recorder.RecordStep(TraceStep{Action: "first"})
		recorder.RecordStep(TraceStep{Action: "second"})

		last := recorder.LastStep()
		if last == nil {
			t.Fatal("LastStep returned nil")
		}
		if last.Action != "second" {
			t.Errorf("LastStep action = %q, want %q", last.Action, "second")
		}

		// Verify it's a copy
		last.Action = "modified"
		lastAgain := recorder.LastStep()
		if lastAgain.Action == "modified" {
			t.Error("LastStep should return a copy")
		}
	})
}

func TestTraceRecorder_ConcurrentAccess(t *testing.T) {
	recorder := NewTraceRecorder(TraceConfig{MaxSteps: 1000})

	var wg sync.WaitGroup
	writers := 10
	stepsPerWriter := 100

	// Concurrent writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < stepsPerWriter; j++ {
				recorder.RecordStep(TraceStep{
					Action: "write",
					Target: "target",
				})
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = recorder.GetSteps()
				_ = recorder.StepCount()
				_ = recorder.LastStep()
			}
		}()
	}

	wg.Wait()

	// Verify all steps were recorded (within capacity)
	count := recorder.StepCount()
	expectedMax := writers * stepsPerWriter
	if count < 1000 && count != expectedMax {
		t.Errorf("Step count = %d, expected up to %d", count, expectedMax)
	}
}

func TestTraceStepBuilder(t *testing.T) {
	step := NewTraceStepBuilder().
		WithAction("explore").
		WithTarget("main.go").
		WithTool("code_graph_search").
		WithDuration(100*time.Millisecond).
		WithSymbolsFound([]string{"main", "init"}).
		WithProofUpdate("main.go:main", "proven", "test passed", "hard").
		WithConstraint("c1", "mutual_exclusion", []string{"a", "b"}).
		WithDependency("a", "b").
		WithError("").
		WithMetadata("context", "testing").
		Build()

	if step.Action != "explore" {
		t.Errorf("Action = %q, want %q", step.Action, "explore")
	}
	if step.Target != "main.go" {
		t.Errorf("Target = %q, want %q", step.Target, "main.go")
	}
	if step.Tool != "code_graph_search" {
		t.Errorf("Tool = %q, want %q", step.Tool, "code_graph_search")
	}
	if step.Duration != 100*time.Millisecond {
		t.Errorf("Duration = %v, want %v", step.Duration, 100*time.Millisecond)
	}
	if len(step.SymbolsFound) != 2 {
		t.Errorf("SymbolsFound count = %d, want 2", len(step.SymbolsFound))
	}
	if len(step.ProofUpdates) != 1 {
		t.Errorf("ProofUpdates count = %d, want 1", len(step.ProofUpdates))
	}
	if step.ProofUpdates[0].Status != "proven" {
		t.Errorf("ProofUpdate status = %q, want %q", step.ProofUpdates[0].Status, "proven")
	}
	if len(step.ConstraintsAdded) != 1 {
		t.Errorf("ConstraintsAdded count = %d, want 1", len(step.ConstraintsAdded))
	}
	if len(step.DependenciesFound) != 1 {
		t.Errorf("DependenciesFound count = %d, want 1", len(step.DependenciesFound))
	}
	if step.Metadata["context"] != "testing" {
		t.Errorf("Metadata[context] = %q, want %q", step.Metadata["context"], "testing")
	}
}

func TestReasoningTrace_JSONSerializable(t *testing.T) {
	recorder := NewTraceRecorder(DefaultTraceConfig())

	now := time.Now()
	recorder.RecordStep(TraceStep{
		Action:       "explore",
		Target:       "auth.go",
		Timestamp:    now,
		SymbolsFound: []string{"ValidateToken"},
		ProofUpdates: []ProofUpdate{
			{NodeID: "auth.go:ValidateToken", Status: "proven", Source: "hard"},
		},
		Metadata: map[string]string{"key": "value"},
	})

	trace := recorder.Export("json-test")

	// Verify serialization works
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("Failed to marshal trace: %v", err)
	}

	// Verify deserialization works
	var parsed ReasoningTrace
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal trace: %v", err)
	}

	// Verify round-trip preserved data
	if parsed.SessionID != trace.SessionID {
		t.Error("SessionID mismatch after round-trip")
	}
	if len(parsed.Trace) != len(trace.Trace) {
		t.Error("Trace length mismatch after round-trip")
	}
	if parsed.Trace[0].Action != "explore" {
		t.Error("Action mismatch after round-trip")
	}
	if len(parsed.Trace[0].SymbolsFound) != 1 {
		t.Error("SymbolsFound mismatch after round-trip")
	}
	if parsed.Trace[0].Metadata["key"] != "value" {
		t.Error("Metadata mismatch after round-trip")
	}
}
