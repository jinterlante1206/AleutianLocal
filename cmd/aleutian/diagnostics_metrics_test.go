// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides tests for DiagnosticsMetrics implementations.

These tests validate:

  - NoOpDiagnosticsMetrics: In-memory recording, thread safety
  - PrometheusDiagnosticsMetrics: Metric registration, label handling
  - Factory function behavior
  - Interface compliance

# Test Strategy

NoOp tests verify in-memory counters. Prometheus tests use a test registry
to avoid conflicts with the default registry. All tests run without external
dependencies.
*/
package main

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// -----------------------------------------------------------------------------
// NoOpDiagnosticsMetrics Tests
// -----------------------------------------------------------------------------

// TestNoOpDiagnosticsMetrics_NewNoOpDiagnosticsMetrics tests constructor.
//
// # Description
//
// Verifies that the constructor creates a valid metrics recorder.
//
// # Test Steps
//
//  1. Create metrics
//  2. Verify not nil
//  3. Verify initial counters are zero
func TestNoOpDiagnosticsMetrics_NewNoOpDiagnosticsMetrics(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()
	if metrics == nil {
		t.Fatal("NewNoOpDiagnosticsMetrics returned nil")
	}

	if metrics.GetCollectionsTotal() != 0 {
		t.Errorf("Initial collectionsTotal = %d, want 0", metrics.GetCollectionsTotal())
	}
	if metrics.GetErrorsTotal() != 0 {
		t.Errorf("Initial errorsTotal = %d, want 0", metrics.GetErrorsTotal())
	}
	if metrics.GetPrunedTotal() != 0 {
		t.Errorf("Initial prunedTotal = %d, want 0", metrics.GetPrunedTotal())
	}
	if metrics.GetStoredCount() != 0 {
		t.Errorf("Initial storedCount = %d, want 0", metrics.GetStoredCount())
	}
}

// TestNoOpDiagnosticsMetrics_RecordCollection tests collection recording.
//
// # Description
//
// Verifies that RecordCollection increments the counter.
//
// # Test Steps
//
//  1. Create metrics
//  2. Record multiple collections
//  3. Verify counter incremented
func TestNoOpDiagnosticsMetrics_RecordCollection(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Record collections
	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
	metrics.RecordCollection(SeverityWarning, "machine_drift", 500, 51200)
	metrics.RecordCollection(SeverityInfo, "manual_request", 200, 25600)

	// Verify count
	if got := metrics.GetCollectionsTotal(); got != 3 {
		t.Errorf("GetCollectionsTotal() = %d, want 3", got)
	}
}

// TestNoOpDiagnosticsMetrics_RecordError tests error recording.
//
// # Description
//
// Verifies that RecordError increments the counter.
//
// # Test Steps
//
//  1. Create metrics
//  2. Record multiple errors
//  3. Verify counter incremented
func TestNoOpDiagnosticsMetrics_RecordError(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Record errors
	metrics.RecordError("storage_failure")
	metrics.RecordError("container_unreachable")

	// Verify count
	if got := metrics.GetErrorsTotal(); got != 2 {
		t.Errorf("GetErrorsTotal() = %d, want 2", got)
	}
}

// TestNoOpDiagnosticsMetrics_RecordContainerHealth tests health recording.
//
// # Description
//
// Verifies that RecordContainerHealth doesn't panic (no-op).
//
// # Test Steps
//
//  1. Create metrics
//  2. Record container health
//  3. Verify no panic
func TestNoOpDiagnosticsMetrics_RecordContainerHealth(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Should not panic
	metrics.RecordContainerHealth("aleutian-weaviate", "vectordb", "healthy")
	metrics.RecordContainerHealth("aleutian-orchestrator", "orchestrator", "unhealthy")
}

// TestNoOpDiagnosticsMetrics_RecordContainerMetrics tests metrics recording.
//
// # Description
//
// Verifies that RecordContainerMetrics doesn't panic (no-op).
//
// # Test Steps
//
//  1. Create metrics
//  2. Record container metrics
//  3. Verify no panic
func TestNoOpDiagnosticsMetrics_RecordContainerMetrics(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Should not panic
	metrics.RecordContainerMetrics("aleutian-rag-engine", 78.5, 4096)
	metrics.RecordContainerMetrics("aleutian-weaviate", 25.0, 2048)
}

// TestNoOpDiagnosticsMetrics_RecordPruned tests pruned recording.
//
// # Description
//
// Verifies that RecordPruned increments the counter.
//
// # Test Steps
//
//  1. Create metrics
//  2. Record pruned counts
//  3. Verify counter accumulated
func TestNoOpDiagnosticsMetrics_RecordPruned(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Record pruned
	metrics.RecordPruned(5)
	metrics.RecordPruned(10)

	// Verify count
	if got := metrics.GetPrunedTotal(); got != 15 {
		t.Errorf("GetPrunedTotal() = %d, want 15", got)
	}
}

// TestNoOpDiagnosticsMetrics_RecordStoredCount tests stored count recording.
//
// # Description
//
// Verifies that RecordStoredCount sets the gauge.
//
// # Test Steps
//
//  1. Create metrics
//  2. Set stored count
//  3. Verify value is set (not accumulated)
func TestNoOpDiagnosticsMetrics_RecordStoredCount(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	// Set count
	metrics.RecordStoredCount(42)
	if got := metrics.GetStoredCount(); got != 42 {
		t.Errorf("GetStoredCount() = %d, want 42", got)
	}

	// Set again (should replace, not add)
	metrics.RecordStoredCount(10)
	if got := metrics.GetStoredCount(); got != 10 {
		t.Errorf("GetStoredCount() after second set = %d, want 10", got)
	}
}

// TestNoOpDiagnosticsMetrics_Register tests registration.
//
// # Description
//
// Verifies that Register returns nil (no-op).
//
// # Test Steps
//
//  1. Create metrics
//  2. Call Register
//  3. Verify nil error
func TestNoOpDiagnosticsMetrics_Register(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	err := metrics.Register()
	if err != nil {
		t.Errorf("Register() = %v, want nil", err)
	}

	// Can call multiple times
	err = metrics.Register()
	if err != nil {
		t.Errorf("Second Register() = %v, want nil", err)
	}
}

// TestNoOpDiagnosticsMetrics_ThreadSafety tests concurrent access.
//
// # Description
//
// Verifies that metrics are safe for concurrent use.
//
// # Test Steps
//
//  1. Create metrics
//  2. Launch multiple goroutines
//  3. Record metrics concurrently
//  4. Verify no races (run with -race)
func TestNoOpDiagnosticsMetrics_ThreadSafety(t *testing.T) {
	metrics := NewNoOpDiagnosticsMetrics()

	var wg sync.WaitGroup

	// Launch 10 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Each goroutine records 100 times
			for j := 0; j < 100; j++ {
				metrics.RecordCollection(SeverityInfo, "concurrent_test", 100, 1024)
				metrics.RecordError("test_error")
				metrics.RecordPruned(1)
				metrics.RecordStoredCount(j)
				metrics.RecordContainerHealth("test", "test", "healthy")
				metrics.RecordContainerMetrics("test", 50.0, 1024)
			}
		}(i)
	}

	wg.Wait()

	// Verify counts
	if got := metrics.GetCollectionsTotal(); got != 1000 {
		t.Errorf("GetCollectionsTotal() = %d, want 1000", got)
	}
	if got := metrics.GetErrorsTotal(); got != 1000 {
		t.Errorf("GetErrorsTotal() = %d, want 1000", got)
	}
	if got := metrics.GetPrunedTotal(); got != 1000 {
		t.Errorf("GetPrunedTotal() = %d, want 1000", got)
	}
}

// -----------------------------------------------------------------------------
// PrometheusDiagnosticsMetrics Tests
// -----------------------------------------------------------------------------

// newTestPrometheusMetrics creates metrics with a test registry.
//
// # Description
//
// Creates Prometheus metrics registered to a test registry to avoid
// conflicts with the default registry.
//
// # Outputs
//
//   - *PrometheusDiagnosticsMetrics: Metrics instance
//   - *prometheus.Registry: Test registry
//
// # Limitations
//
//   - Uses reflection to override registered flag
//
// # Assumptions
//
//   - Test registry is isolated
func newTestPrometheusMetrics(t *testing.T) (*PrometheusDiagnosticsMetrics, *prometheus.Registry) {
	t.Helper()

	metrics := NewPrometheusDiagnosticsMetrics()
	registry := prometheus.NewRegistry()

	// Register with test registry
	collectors := []prometheus.Collector{
		metrics.collectionsTotal,
		metrics.collectionDuration,
		metrics.collectionSize,
		metrics.errorsTotal,
		metrics.containerHealth,
		metrics.containerCPU,
		metrics.containerMemory,
		metrics.prunedTotal,
		metrics.storedCount,
	}

	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			t.Fatalf("Failed to register collector: %v", err)
		}
	}

	return metrics, registry
}

// TestPrometheusDiagnosticsMetrics_NewPrometheusDiagnosticsMetrics tests constructor.
//
// # Description
//
// Verifies that the constructor creates valid metrics.
//
// # Test Steps
//
//  1. Create metrics
//  2. Verify not nil
//  3. Verify collectors are initialized
func TestPrometheusDiagnosticsMetrics_NewPrometheusDiagnosticsMetrics(t *testing.T) {
	metrics := NewPrometheusDiagnosticsMetrics()
	if metrics == nil {
		t.Fatal("NewPrometheusDiagnosticsMetrics returned nil")
	}

	// Verify collectors are initialized
	if metrics.collectionsTotal == nil {
		t.Error("collectionsTotal is nil")
	}
	if metrics.collectionDuration == nil {
		t.Error("collectionDuration is nil")
	}
	if metrics.collectionSize == nil {
		t.Error("collectionSize is nil")
	}
	if metrics.errorsTotal == nil {
		t.Error("errorsTotal is nil")
	}
	if metrics.containerHealth == nil {
		t.Error("containerHealth is nil")
	}
	if metrics.containerCPU == nil {
		t.Error("containerCPU is nil")
	}
	if metrics.containerMemory == nil {
		t.Error("containerMemory is nil")
	}
	if metrics.prunedTotal == nil {
		t.Error("prunedTotal is nil")
	}
	if metrics.storedCount == nil {
		t.Error("storedCount is nil")
	}
}

// TestPrometheusDiagnosticsMetrics_RecordCollection tests collection recording.
//
// # Description
//
// Verifies that RecordCollection updates Prometheus metrics.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Record collections
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordCollection(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Record collections (should not panic)
	metrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
	metrics.RecordCollection(SeverityWarning, "machine_drift", 500, 51200)
	metrics.RecordCollection(SeverityInfo, "manual_request", 200, 25600)
}

// TestPrometheusDiagnosticsMetrics_RecordError tests error recording.
//
// # Description
//
// Verifies that RecordError updates Prometheus metrics.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Record errors
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordError(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Record errors (should not panic)
	metrics.RecordError("storage_failure")
	metrics.RecordError("container_unreachable")
	metrics.RecordError("format_error")
}

// TestPrometheusDiagnosticsMetrics_RecordContainerHealth tests health recording.
//
// # Description
//
// Verifies that RecordContainerHealth updates Prometheus gauges.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Record container health
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordContainerHealth(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Record health (should not panic)
	metrics.RecordContainerHealth("aleutian-weaviate", "vectordb", "healthy")
	metrics.RecordContainerHealth("aleutian-orchestrator", "orchestrator", "unhealthy")
	metrics.RecordContainerHealth("aleutian-rag", "rag", "unknown")
}

// TestPrometheusDiagnosticsMetrics_RecordContainerMetrics tests metrics recording.
//
// # Description
//
// Verifies that RecordContainerMetrics updates Prometheus gauges.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Record container metrics
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordContainerMetrics(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Record metrics (should not panic)
	metrics.RecordContainerMetrics("aleutian-rag-engine", 78.5, 4096)
	metrics.RecordContainerMetrics("aleutian-weaviate", 25.0, 2048)
}

// TestPrometheusDiagnosticsMetrics_RecordPruned tests pruned recording.
//
// # Description
//
// Verifies that RecordPruned updates Prometheus counter.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Record pruned counts
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordPruned(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Record pruned (should not panic)
	metrics.RecordPruned(5)
	metrics.RecordPruned(10)
}

// TestPrometheusDiagnosticsMetrics_RecordStoredCount tests stored count recording.
//
// # Description
//
// Verifies that RecordStoredCount updates Prometheus gauge.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Set stored count
//  3. Verify metrics are recorded (no panic)
func TestPrometheusDiagnosticsMetrics_RecordStoredCount(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	// Set count (should not panic)
	metrics.RecordStoredCount(42)
	metrics.RecordStoredCount(10)
}

// TestPrometheusDiagnosticsMetrics_ThreadSafety tests concurrent access.
//
// # Description
//
// Verifies that Prometheus metrics are safe for concurrent use.
//
// # Test Steps
//
//  1. Create metrics with test registry
//  2. Launch multiple goroutines
//  3. Record metrics concurrently
//  4. Verify no races (run with -race)
func TestPrometheusDiagnosticsMetrics_ThreadSafety(t *testing.T) {
	metrics, _ := newTestPrometheusMetrics(t)

	var wg sync.WaitGroup

	// Launch 10 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Each goroutine records 100 times
			for j := 0; j < 100; j++ {
				metrics.RecordCollection(SeverityInfo, "concurrent_test", 100, 1024)
				metrics.RecordError("test_error")
				metrics.RecordPruned(1)
				metrics.RecordStoredCount(j)
				metrics.RecordContainerHealth("test", "test", "healthy")
				metrics.RecordContainerMetrics("test", 50.0, 1024)
			}
		}(i)
	}

	wg.Wait()
	// Success if no race conditions detected (run with -race flag)
}

// -----------------------------------------------------------------------------
// Factory Function Tests
// -----------------------------------------------------------------------------

// TestNewDefaultDiagnosticsMetrics_FOSS tests factory for FOSS mode.
//
// # Description
//
// Verifies that factory returns NoOpDiagnosticsMetrics when disabled.
//
// # Test Steps
//
//  1. Call factory with enablePrometheus=false
//  2. Verify NoOpDiagnosticsMetrics is returned
func TestNewDefaultDiagnosticsMetrics_FOSS(t *testing.T) {
	metrics := NewDefaultDiagnosticsMetrics(false)

	_, ok := metrics.(*NoOpDiagnosticsMetrics)
	if !ok {
		t.Errorf("Expected *NoOpDiagnosticsMetrics, got %T", metrics)
	}
}

// TestNewDefaultDiagnosticsMetrics_Enterprise tests factory for Enterprise mode.
//
// # Description
//
// Verifies that factory returns PrometheusDiagnosticsMetrics when enabled.
//
// # Test Steps
//
//  1. Call factory with enablePrometheus=true
//  2. Verify PrometheusDiagnosticsMetrics is returned
func TestNewDefaultDiagnosticsMetrics_Enterprise(t *testing.T) {
	metrics := NewDefaultDiagnosticsMetrics(true)

	_, ok := metrics.(*PrometheusDiagnosticsMetrics)
	if !ok {
		t.Errorf("Expected *PrometheusDiagnosticsMetrics, got %T", metrics)
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestNoOpDiagnosticsMetrics_InterfaceCompliance tests interface implementation.
//
// # Description
//
// Verifies that NoOpDiagnosticsMetrics satisfies DiagnosticsMetrics interface.
//
// # Test Steps
//
//  1. Assign to interface variable
//  2. Verify all methods work
func TestNoOpDiagnosticsMetrics_InterfaceCompliance(t *testing.T) {
	var metrics DiagnosticsMetrics = NewNoOpDiagnosticsMetrics()

	// All methods should work without panic
	metrics.RecordCollection(SeverityError, "test", 100, 1024)
	metrics.RecordError("test")
	metrics.RecordContainerHealth("test", "test", "healthy")
	metrics.RecordContainerMetrics("test", 50.0, 1024)
	metrics.RecordPruned(5)
	metrics.RecordStoredCount(10)

	if err := metrics.Register(); err != nil {
		t.Errorf("Register() = %v, want nil", err)
	}
}

// TestPrometheusDiagnosticsMetrics_InterfaceCompliance tests interface implementation.
//
// # Description
//
// Verifies that PrometheusDiagnosticsMetrics satisfies DiagnosticsMetrics interface.
//
// # Test Steps
//
//  1. Assign to interface variable
//  2. Verify all methods work (with test registry)
func TestPrometheusDiagnosticsMetrics_InterfaceCompliance(t *testing.T) {
	promMetrics, _ := newTestPrometheusMetrics(t)
	var metrics DiagnosticsMetrics = promMetrics

	// All methods should work without panic
	metrics.RecordCollection(SeverityError, "test", 100, 1024)
	metrics.RecordError("test")
	metrics.RecordContainerHealth("test", "test", "healthy")
	metrics.RecordContainerMetrics("test", 50.0, 1024)
	metrics.RecordPruned(5)
	metrics.RecordStoredCount(10)
}

// -----------------------------------------------------------------------------
// Integration Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsMetrics_Integration_FullWorkflow tests complete workflow.
//
// # Description
//
// Tests a realistic workflow with both FOSS and Enterprise metrics.
//
// # Test Steps
//
//  1. Create FOSS metrics
//  2. Simulate collection cycle
//  3. Verify counts
//  4. Create Enterprise metrics
//  5. Simulate collection cycle
//  6. Verify no panics
func TestDiagnosticsMetrics_Integration_FullWorkflow(t *testing.T) {
	// FOSS workflow
	fossMetrics := NewNoOpDiagnosticsMetrics()
	fossMetrics.Register()

	// Simulate collection cycle
	fossMetrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
	fossMetrics.RecordContainerHealth("aleutian-orchestrator", "orchestrator", "unhealthy")
	fossMetrics.RecordContainerMetrics("aleutian-orchestrator", 95.0, 8192)
	fossMetrics.RecordError("container_unreachable")

	// Simulate prune
	fossMetrics.RecordPruned(15)
	fossMetrics.RecordStoredCount(42)

	// Verify FOSS counts
	if fossMetrics.GetCollectionsTotal() != 1 {
		t.Error("FOSS collections not recorded")
	}
	if fossMetrics.GetErrorsTotal() != 1 {
		t.Error("FOSS errors not recorded")
	}

	// Enterprise workflow
	promMetrics, _ := newTestPrometheusMetrics(t)

	// Simulate collection cycle
	promMetrics.RecordCollection(SeverityError, "startup_failure", 1500, 102400)
	promMetrics.RecordContainerHealth("aleutian-orchestrator", "orchestrator", "unhealthy")
	promMetrics.RecordContainerMetrics("aleutian-orchestrator", 95.0, 8192)
	promMetrics.RecordError("container_unreachable")

	// Simulate prune
	promMetrics.RecordPruned(15)
	promMetrics.RecordStoredCount(42)

	// Success if no panics
}

// Compile-time interface verification.
var _ DiagnosticsMetrics = (*NoOpDiagnosticsMetrics)(nil)
var _ DiagnosticsMetrics = (*PrometheusDiagnosticsMetrics)(nil)
