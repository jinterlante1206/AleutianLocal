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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// sanitizeActivityName Tests
// -----------------------------------------------------------------------------

func TestSanitizeActivityName(t *testing.T) {
	t.Run("known activity names pass through", func(t *testing.T) {
		knownNames := []string{"search", "awareness", "constraint", "learning", "memory", "planning", "similarity", "streaming"}
		for _, name := range knownNames {
			result := sanitizeActivityName(name)
			if result != name {
				t.Errorf("expected %q, got %q", name, result)
			}
		}
	})

	t.Run("empty name returns unknown", func(t *testing.T) {
		result := sanitizeActivityName("")
		if result != "unknown" {
			t.Errorf("expected 'unknown', got %q", result)
		}
	})

	t.Run("unknown name returns unknown", func(t *testing.T) {
		result := sanitizeActivityName("invalid_activity_name")
		if result != "unknown" {
			t.Errorf("expected 'unknown', got %q", result)
		}
	})

	t.Run("case sensitive - uppercase returns unknown", func(t *testing.T) {
		result := sanitizeActivityName("SEARCH")
		if result != "unknown" {
			t.Errorf("expected 'unknown', got %q", result)
		}
	})
}

func TestRegisterActivityName(t *testing.T) {
	t.Run("registered name becomes known", func(t *testing.T) {
		// Register a new name
		RegisterActivityName("test_activity")

		// Verify it's now known
		result := sanitizeActivityName("test_activity")
		if result != "test_activity" {
			t.Errorf("expected 'test_activity', got %q", result)
		}

		// Clean up by removing from map (in real usage, names would only be added)
		delete(knownActivities, "test_activity")
	})
}

// -----------------------------------------------------------------------------
// RecordActivityMetrics Tests
// -----------------------------------------------------------------------------

func TestRecordActivityMetrics(t *testing.T) {
	t.Run("success status", func(t *testing.T) {
		// This test verifies the function doesn't panic
		// In a production environment, we'd verify Prometheus metrics
		RecordActivityMetrics("search", true, false, 1.5)
	})

	t.Run("failure status", func(t *testing.T) {
		RecordActivityMetrics("search", false, false, 0.5)
	})

	t.Run("partial status", func(t *testing.T) {
		RecordActivityMetrics("search", false, true, 2.0)
	})

	t.Run("unknown activity sanitized", func(t *testing.T) {
		// Should not panic, unknown names are sanitized
		RecordActivityMetrics("invalid_name", true, false, 1.0)
	})

	t.Run("empty activity name sanitized", func(t *testing.T) {
		RecordActivityMetrics("", true, false, 1.0)
	})

	t.Run("zero duration", func(t *testing.T) {
		RecordActivityMetrics("search", true, false, 0)
	})

	t.Run("negative duration handled", func(t *testing.T) {
		// Prometheus histograms accept negative values (though unusual)
		RecordActivityMetrics("search", true, false, -1.0)
	})
}

// -----------------------------------------------------------------------------
// RecordStepMetrics Tests
// -----------------------------------------------------------------------------

func TestRecordStepMetrics(t *testing.T) {
	t.Run("nil step returns immediately", func(t *testing.T) {
		// Should not panic
		RecordStepMetrics("search", nil)
	})

	t.Run("empty step", func(t *testing.T) {
		step := &crs.TraceStep{}
		RecordStepMetrics("search", step)
	})

	t.Run("step with proof updates", func(t *testing.T) {
		step := &crs.TraceStep{
			ProofUpdates: []crs.ProofUpdate{
				{NodeID: "node1", Status: "proven"},
				{NodeID: "node2", Status: "disproven"},
				{NodeID: "node3", Status: "expanded"},
				{NodeID: "node4", Status: "unknown"},
			},
		}
		RecordStepMetrics("search", step)
	})

	t.Run("step with empty status defaults to unknown", func(t *testing.T) {
		step := &crs.TraceStep{
			ProofUpdates: []crs.ProofUpdate{
				{NodeID: "node1", Status: ""},
			},
		}
		RecordStepMetrics("search", step)
	})

	t.Run("step with constraints", func(t *testing.T) {
		step := &crs.TraceStep{
			ConstraintsAdded: []crs.ConstraintUpdate{
				{ID: "c1", Type: "mutual_exclusion", Nodes: []string{"a", "b"}},
				{ID: "c2", Type: "implication", Nodes: []string{"c", "d"}},
				{ID: "c3", Type: "ordering", Nodes: []string{"e", "f"}},
				{ID: "c4", Type: "resource", Nodes: []string{"g", "h"}},
			},
		}
		RecordStepMetrics("constraint", step)
	})

	t.Run("step with empty constraint type defaults to unknown", func(t *testing.T) {
		step := &crs.TraceStep{
			ConstraintsAdded: []crs.ConstraintUpdate{
				{ID: "c1", Type: "", Nodes: []string{"a", "b"}},
			},
		}
		RecordStepMetrics("constraint", step)
	})

	t.Run("step with dependencies", func(t *testing.T) {
		step := &crs.TraceStep{
			DependenciesFound: []crs.DependencyEdge{
				{From: "a", To: "b"},
				{From: "c", To: "d"},
			},
		}
		RecordStepMetrics("search", step)
	})

	t.Run("step with symbols", func(t *testing.T) {
		step := &crs.TraceStep{
			SymbolsFound: []string{"func1", "func2", "type1"},
		}
		RecordStepMetrics("search", step)
	})

	t.Run("step with all data", func(t *testing.T) {
		step := &crs.TraceStep{
			ProofUpdates: []crs.ProofUpdate{
				{NodeID: "node1", Status: "proven"},
			},
			ConstraintsAdded: []crs.ConstraintUpdate{
				{ID: "c1", Type: "mutual_exclusion", Nodes: []string{"a", "b"}},
			},
			DependenciesFound: []crs.DependencyEdge{
				{From: "a", To: "b"},
			},
			SymbolsFound: []string{"func1"},
		}
		RecordStepMetrics("search", step)
	})

	t.Run("unknown activity name sanitized", func(t *testing.T) {
		step := &crs.TraceStep{
			ProofUpdates: []crs.ProofUpdate{
				{NodeID: "node1", Status: "proven"},
			},
		}
		RecordStepMetrics("invalid_activity", step)
	})
}

// -----------------------------------------------------------------------------
// UpdateGenerationGauge Tests
// -----------------------------------------------------------------------------

func TestUpdateGenerationGauge(t *testing.T) {
	t.Run("positive generation", func(t *testing.T) {
		UpdateGenerationGauge(100)
	})

	t.Run("zero generation", func(t *testing.T) {
		UpdateGenerationGauge(0)
	})

	t.Run("large generation", func(t *testing.T) {
		UpdateGenerationGauge(9999999999)
	})
}

// -----------------------------------------------------------------------------
// RecordConflict Tests
// -----------------------------------------------------------------------------

func TestRecordConflict(t *testing.T) {
	t.Run("increments counter", func(t *testing.T) {
		// Should not panic
		RecordConflict()
	})

	t.Run("multiple calls", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			RecordConflict()
		}
	})
}

// -----------------------------------------------------------------------------
// RecordRecordingError Tests
// -----------------------------------------------------------------------------

func TestRecordRecordingError(t *testing.T) {
	t.Run("panic error type", func(t *testing.T) {
		RecordRecordingError("panic")
	})

	t.Run("recorder_nil error type", func(t *testing.T) {
		RecordRecordingError("recorder_nil")
	})

	t.Run("extraction_failed error type", func(t *testing.T) {
		RecordRecordingError("extraction_failed")
	})

	t.Run("empty error type", func(t *testing.T) {
		RecordRecordingError("")
	})

	t.Run("unknown error type", func(t *testing.T) {
		RecordRecordingError("some_other_error")
	})
}

// -----------------------------------------------------------------------------
// RecordRecordingDuration Tests
// -----------------------------------------------------------------------------

func TestRecordRecordingDuration(t *testing.T) {
	t.Run("short duration", func(t *testing.T) {
		start := time.Now()
		time.Sleep(1 * time.Millisecond)
		RecordRecordingDuration(start)
	})

	t.Run("zero duration", func(t *testing.T) {
		start := time.Now()
		RecordRecordingDuration(start)
	})

	t.Run("past start time", func(t *testing.T) {
		start := time.Now().Add(-1 * time.Second)
		RecordRecordingDuration(start)
	})
}

// -----------------------------------------------------------------------------
// Concurrent Access Tests
// -----------------------------------------------------------------------------

func TestMetricsConcurrentAccess(t *testing.T) {
	t.Run("concurrent RecordActivityMetrics", func(t *testing.T) {
		done := make(chan bool)
		for i := 0; i < 100; i++ {
			go func(i int) {
				RecordActivityMetrics("search", i%2 == 0, i%3 == 0, float64(i)*0.1)
				done <- true
			}(i)
		}
		for i := 0; i < 100; i++ {
			<-done
		}
	})

	t.Run("concurrent RecordStepMetrics", func(t *testing.T) {
		done := make(chan bool)
		for i := 0; i < 100; i++ {
			go func(i int) {
				step := &crs.TraceStep{
					ProofUpdates: []crs.ProofUpdate{
						{NodeID: "node", Status: "proven"},
					},
				}
				RecordStepMetrics("search", step)
				done <- true
			}(i)
		}
		for i := 0; i < 100; i++ {
			<-done
		}
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		done := make(chan bool)
		for i := 0; i < 50; i++ {
			go func() {
				RecordActivityMetrics("search", true, false, 1.0)
				done <- true
			}()
			go func() {
				RecordConflict()
				done <- true
			}()
			go func() {
				UpdateGenerationGauge(int64(i))
				done <- true
			}()
			go func() {
				RecordRecordingError("panic")
				done <- true
			}()
		}
		for i := 0; i < 200; i++ {
			<-done
		}
	})
}
