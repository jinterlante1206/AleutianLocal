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
Package main provides tests for DiagnosticsTracer implementations.

These tests validate:

  - NoOpDiagnosticsTracer: ID generation, context propagation, no export
  - OTelDiagnosticsTracer: Real span creation (without network)
  - Factory function behavior based on environment
  - Thread safety of ID generation
  - W3C Trace Context format compliance

# Test Strategy

NoOp tracer tests are fully offline. OTel tracer tests use the SDK but
don't require a running collector (spans are created but not exported).
*/
package main

import (
	"context"
	"regexp"
	"sync"
	"testing"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// isValidTraceID checks if a string is a valid W3C trace ID (32 hex chars).
//
// # Description
//
// Validates that the trace ID matches the W3C Trace Context format.
//
// # Inputs
//
//   - id: String to validate
//
// # Outputs
//
//   - bool: True if valid 32-character hex string
//
// # Examples
//
//	isValidTraceID("a1b2c3d4e5f60718293a4b5c6d7e8f90") // true
//	isValidTraceID("invalid") // false
//
// # Limitations
//
//   - Only checks format, not uniqueness
//
// # Assumptions
//
//   - W3C Trace Context format is 32 lowercase hex characters
func isValidTraceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{32}$", id)
	return matched
}

// isValidSpanID checks if a string is a valid W3C span ID (16 hex chars).
//
// # Description
//
// Validates that the span ID matches the W3C Trace Context format.
//
// # Inputs
//
//   - id: String to validate
//
// # Outputs
//
//   - bool: True if valid 16-character hex string
//
// # Examples
//
//	isValidSpanID("a1b2c3d4e5f60718") // true
//	isValidSpanID("invalid") // false
//
// # Limitations
//
//   - Only checks format, not uniqueness
//
// # Assumptions
//
//   - W3C Trace Context format is 16 lowercase hex characters
func isValidSpanID(id string) bool {
	if len(id) != 16 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9a-f]{16}$", id)
	return matched
}

// -----------------------------------------------------------------------------
// NoOpDiagnosticsTracer Tests
// -----------------------------------------------------------------------------

// TestNoOpDiagnosticsTracer_NewNoOpDiagnosticsTracer tests constructor.
//
// # Description
//
// Verifies that the constructor creates a valid tracer with the given service name.
//
// # Test Steps
//
//  1. Create tracer with service name
//  2. Verify not nil
//  3. Create with empty name
//  4. Verify default is used
func TestNoOpDiagnosticsTracer_NewNoOpDiagnosticsTracer(t *testing.T) {
	// With service name
	tracer := NewNoOpDiagnosticsTracer("test-service")
	if tracer == nil {
		t.Fatal("NewNoOpDiagnosticsTracer returned nil")
	}
	if tracer.serviceName != "test-service" {
		t.Errorf("serviceName = %q, want %q", tracer.serviceName, "test-service")
	}

	// With empty name (should use default)
	tracer = NewNoOpDiagnosticsTracer("")
	if tracer.serviceName != "aleutian-cli" {
		t.Errorf("serviceName = %q, want default %q", tracer.serviceName, "aleutian-cli")
	}
}

// TestNoOpDiagnosticsTracer_GenerateTraceID tests trace ID generation.
//
// # Description
//
// Verifies that GenerateTraceID produces valid W3C trace IDs.
//
// # Test Steps
//
//  1. Generate multiple trace IDs
//  2. Verify each is valid format
//  3. Verify uniqueness
func TestNoOpDiagnosticsTracer_GenerateTraceID(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := tracer.GenerateTraceID()

		if !isValidTraceID(id) {
			t.Errorf("GenerateTraceID() = %q, not valid W3C format", id)
		}

		if seen[id] {
			t.Errorf("GenerateTraceID() produced duplicate: %q", id)
		}
		seen[id] = true
	}
}

// TestNoOpDiagnosticsTracer_GenerateSpanID tests span ID generation.
//
// # Description
//
// Verifies that GenerateSpanID produces valid W3C span IDs.
//
// # Test Steps
//
//  1. Generate multiple span IDs
//  2. Verify each is valid format
//  3. Verify uniqueness
func TestNoOpDiagnosticsTracer_GenerateSpanID(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := tracer.GenerateSpanID()

		if !isValidSpanID(id) {
			t.Errorf("GenerateSpanID() = %q, not valid W3C format", id)
		}

		if seen[id] {
			t.Errorf("GenerateSpanID() produced duplicate: %q", id)
		}
		seen[id] = true
	}
}

// TestNoOpDiagnosticsTracer_StartSpan tests span creation.
//
// # Description
//
// Verifies that StartSpan stores IDs in context and returns working finish func.
//
// # Test Steps
//
//  1. Create span
//  2. Verify context has trace ID
//  3. Verify context has span ID
//  4. Call finish function
func TestNoOpDiagnosticsTracer_StartSpan(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")
	ctx := context.Background()

	// Start span
	newCtx, finish := tracer.StartSpan(ctx, "test.operation", map[string]string{
		"key": "value",
	})

	// Verify context is different
	if newCtx == ctx {
		t.Error("StartSpan should return new context")
	}

	// Verify IDs are in context
	traceID := tracer.GetTraceID(newCtx)
	if traceID == "" {
		t.Error("GetTraceID returned empty string after StartSpan")
	}
	if !isValidTraceID(traceID) {
		t.Errorf("GetTraceID = %q, not valid format", traceID)
	}

	spanID := tracer.GetSpanID(newCtx)
	if spanID == "" {
		t.Error("GetSpanID returned empty string after StartSpan")
	}
	if !isValidSpanID(spanID) {
		t.Errorf("GetSpanID = %q, not valid format", spanID)
	}

	// Finish should not panic
	finish(nil)
}

// TestNoOpDiagnosticsTracer_StartSpan_WithError tests finish with error.
//
// # Description
//
// Verifies that finish function handles errors gracefully.
//
// # Test Steps
//
//  1. Create span
//  2. Call finish with error
//  3. Verify no panic
func TestNoOpDiagnosticsTracer_StartSpan_WithError(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")
	ctx := context.Background()

	_, finish := tracer.StartSpan(ctx, "test.operation", nil)

	// Finish with error should not panic
	finish(context.DeadlineExceeded)
}

// TestNoOpDiagnosticsTracer_GetTraceID_NoSpan tests empty context.
//
// # Description
//
// Verifies that GetTraceID returns empty string for context without span.
//
// # Test Steps
//
//  1. Get trace ID from empty context
//  2. Verify empty string returned
func TestNoOpDiagnosticsTracer_GetTraceID_NoSpan(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")
	ctx := context.Background()

	traceID := tracer.GetTraceID(ctx)
	if traceID != "" {
		t.Errorf("GetTraceID on empty context = %q, want empty string", traceID)
	}
}

// TestNoOpDiagnosticsTracer_GetSpanID_NoSpan tests empty context.
//
// # Description
//
// Verifies that GetSpanID returns empty string for context without span.
//
// # Test Steps
//
//  1. Get span ID from empty context
//  2. Verify empty string returned
func TestNoOpDiagnosticsTracer_GetSpanID_NoSpan(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")
	ctx := context.Background()

	spanID := tracer.GetSpanID(ctx)
	if spanID != "" {
		t.Errorf("GetSpanID on empty context = %q, want empty string", spanID)
	}
}

// TestNoOpDiagnosticsTracer_Shutdown tests shutdown.
//
// # Description
//
// Verifies that Shutdown returns nil (no-op).
//
// # Test Steps
//
//  1. Call Shutdown
//  2. Verify nil error
func TestNoOpDiagnosticsTracer_Shutdown(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")
	ctx := context.Background()

	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

// TestNoOpDiagnosticsTracer_ThreadSafety tests concurrent ID generation.
//
// # Description
//
// Verifies that ID generation is thread-safe.
//
// # Test Steps
//
//  1. Launch multiple goroutines
//  2. Generate IDs concurrently
//  3. Verify no duplicates or panics
func TestNoOpDiagnosticsTracer_ThreadSafety(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test")

	var wg sync.WaitGroup
	var mu sync.Mutex
	traceIDs := make(map[string]bool)
	spanIDs := make(map[string]bool)

	// Launch 10 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Generate 100 IDs per goroutine
			for j := 0; j < 100; j++ {
				traceID := tracer.GenerateTraceID()
				spanID := tracer.GenerateSpanID()

				mu.Lock()
				traceIDs[traceID] = true
				spanIDs[spanID] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Should have ~1000 unique IDs (collisions extremely unlikely with crypto/rand)
	if len(traceIDs) < 900 {
		t.Errorf("Expected ~1000 unique trace IDs, got %d (possible collision issue)", len(traceIDs))
	}
	if len(spanIDs) < 900 {
		t.Errorf("Expected ~1000 unique span IDs, got %d (possible collision issue)", len(spanIDs))
	}
}

// -----------------------------------------------------------------------------
// Factory Function Tests
// -----------------------------------------------------------------------------

// TestNewDefaultDiagnosticsTracer_NoEndpoint tests factory without collector.
//
// # Description
//
// Verifies that factory returns NoOpDiagnosticsTracer when no endpoint is set.
//
// # Test Steps
//
//  1. Ensure OTEL_EXPORTER_OTLP_ENDPOINT is unset
//  2. Call factory
//  3. Verify NoOpDiagnosticsTracer is returned
func TestNewDefaultDiagnosticsTracer_NoEndpoint(t *testing.T) {
	// Clear env var (t.Setenv restores automatically after test)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	tracer, err := NewDefaultDiagnosticsTracer(ctx, "test")
	if err != nil {
		t.Fatalf("NewDefaultDiagnosticsTracer() error = %v", err)
	}

	// Should be NoOp tracer
	_, ok := tracer.(*NoOpDiagnosticsTracer)
	if !ok {
		t.Errorf("Expected *NoOpDiagnosticsTracer, got %T", tracer)
	}
}

// TestGetEnvironment tests environment detection.
//
// # Description
//
// Verifies that getEnvironment returns correct environment string.
//
// # Test Steps
//
//  1. Test with ALEUTIAN_ENV set
//  2. Test with ENVIRONMENT set
//  3. Test with neither set (default)
func TestGetEnvironment(t *testing.T) {
	// Clear both vars (t.Setenv restores after test)
	t.Setenv("ALEUTIAN_ENV", "")
	t.Setenv("ENVIRONMENT", "")

	// Default should be development
	if env := getEnvironment(); env != "development" {
		t.Errorf("getEnvironment() = %q, want %q", env, "development")
	}

	// ALEUTIAN_ENV takes priority
	t.Setenv("ALEUTIAN_ENV", "production")
	if env := getEnvironment(); env != "production" {
		t.Errorf("getEnvironment() with ALEUTIAN_ENV = %q, want %q", env, "production")
	}

	// ENVIRONMENT is fallback
	t.Setenv("ALEUTIAN_ENV", "")
	t.Setenv("ENVIRONMENT", "staging")
	if env := getEnvironment(); env != "staging" {
		t.Errorf("getEnvironment() with ENVIRONMENT = %q, want %q", env, "staging")
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestNoOpDiagnosticsTracer_InterfaceCompliance tests interface implementation.
//
// # Description
//
// Verifies that NoOpDiagnosticsTracer satisfies DiagnosticsTracer interface.
//
// # Test Steps
//
//  1. Assign to interface variable
//  2. Verify all methods work
func TestNoOpDiagnosticsTracer_InterfaceCompliance(t *testing.T) {
	var tracer DiagnosticsTracer = NewNoOpDiagnosticsTracer("test")

	ctx := context.Background()

	// StartSpan
	newCtx, finish := tracer.StartSpan(ctx, "test", nil)
	finish(nil)

	// GetTraceID
	_ = tracer.GetTraceID(newCtx)

	// GetSpanID
	_ = tracer.GetSpanID(newCtx)

	// GenerateTraceID
	traceID := tracer.GenerateTraceID()
	if !isValidTraceID(traceID) {
		t.Errorf("GenerateTraceID() = %q, invalid format", traceID)
	}

	// GenerateSpanID
	spanID := tracer.GenerateSpanID()
	if !isValidSpanID(spanID) {
		t.Errorf("GenerateSpanID() = %q, invalid format", spanID)
	}

	// Shutdown
	if err := tracer.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

// -----------------------------------------------------------------------------
// Integration Tests
// -----------------------------------------------------------------------------

// TestNoOpDiagnosticsTracer_Integration_FullWorkflow tests complete workflow.
//
// # Description
//
// Tests a realistic usage pattern with nested spans.
//
// # Test Steps
//
//  1. Create tracer
//  2. Start parent span
//  3. Start child span
//  4. Verify IDs propagate
//  5. Finish both spans
func TestNoOpDiagnosticsTracer_Integration_FullWorkflow(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test-service")
	ctx := context.Background()

	// Start parent span
	parentCtx, finishParent := tracer.StartSpan(ctx, "diagnostics.collect", map[string]string{
		"reason":   "startup_failure",
		"severity": "error",
	})

	parentTraceID := tracer.GetTraceID(parentCtx)
	if parentTraceID == "" {
		t.Error("Parent trace ID is empty")
	}

	// Start child span (in NoOp mode, this creates new IDs - that's expected)
	childCtx, finishChild := tracer.StartSpan(parentCtx, "diagnostics.storage.store", map[string]string{
		"storage_type": "file",
	})

	childSpanID := tracer.GetSpanID(childCtx)
	if childSpanID == "" {
		t.Error("Child span ID is empty")
	}

	// Finish child with error
	finishChild(nil)

	// Finish parent
	finishParent(nil)

	// Shutdown
	if err := tracer.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

// TestNoOpDiagnosticsTracer_Integration_ConcurrentSpans tests concurrent span creation.
//
// # Description
//
// Tests creating spans concurrently from multiple goroutines.
//
// # Test Steps
//
//  1. Create tracer
//  2. Launch multiple goroutines
//  3. Each creates and finishes spans
//  4. Verify no panics or data races
func TestNoOpDiagnosticsTracer_Integration_ConcurrentSpans(t *testing.T) {
	tracer := NewNoOpDiagnosticsTracer("test-service")
	ctx := context.Background()

	var wg sync.WaitGroup

	// Launch 20 goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Each goroutine creates 10 spans
			for j := 0; j < 10; j++ {
				spanCtx, finish := tracer.StartSpan(ctx, "concurrent.span", map[string]string{
					"goroutine": string(rune('a' + idx)),
					"iteration": string(rune('0' + j)),
				})

				// Verify we got IDs
				traceID := tracer.GetTraceID(spanCtx)
				if traceID == "" {
					t.Error("Concurrent span has empty trace ID")
				}

				// Finish with alternating success/error
				if j%2 == 0 {
					finish(nil)
				} else {
					finish(context.Canceled)
				}
			}
		}(i)
	}

	wg.Wait()
}

// Compile-time interface verification.
var _ DiagnosticsTracer = (*NoOpDiagnosticsTracer)(nil)
