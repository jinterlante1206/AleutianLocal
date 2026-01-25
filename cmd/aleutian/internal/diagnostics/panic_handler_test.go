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
Package diagnostics_test provides tests for DefaultPanicRecoveryHandler.

These tests validate:

  - Panic capture and diagnostics collection
  - Stack trace capture
  - Output formatting
  - Re-panic behavior
  - Thread safety

# Test Strategy

Tests use a mock collector to avoid real filesystem operations. The
SetRePanic(false) method prevents tests from actually crashing. Output
is captured to verify formatting.
*/
package diagnostics

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Mock Collector for Testing
// -----------------------------------------------------------------------------

// mockPanicCollector is a test collector that records calls.
//
// # Description
//
// Mock implementation of DiagnosticsCollector for panic handler testing.
//
// # Thread Safety
//
// Safe for concurrent use.
type mockPanicCollector struct {
	mu          sync.Mutex
	collectOpts []CollectOptions
	result      *DiagnosticsResult
	err         error
	delay       time.Duration
}

// newMockPanicCollector creates a mock collector.
//
// # Outputs
//
//   - *mockPanicCollector: Ready mock
func newMockPanicCollector() *mockPanicCollector {
	return &mockPanicCollector{
		result: &DiagnosticsResult{
			Location:    "/tmp/test-diag.json",
			TraceID:     "mock-trace-id-12345",
			SpanID:      "mock-span-id",
			TimestampMs: time.Now().UnixMilli(),
			DurationMs:  100,
			Format:      ".json",
			SizeBytes:   1024,
		},
	}
}

// Collect records the call and returns configured result.
func (m *mockPanicCollector) Collect(ctx context.Context, opts CollectOptions) (*DiagnosticsResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.collectOpts = append(m.collectOpts, opts)

	// Simulate delay if configured
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return m.result, m.err
}

// GetLastResult returns nil for testing.
func (m *mockPanicCollector) GetLastResult() *DiagnosticsResult {
	return nil
}

// SetStorage is a no-op for testing.
func (m *mockPanicCollector) SetStorage(storage DiagnosticsStorage) {}

// SetFormatter is a no-op for testing.
func (m *mockPanicCollector) SetFormatter(formatter DiagnosticsFormatter) {}

// GetCollectCalls returns recorded collect calls.
func (m *mockPanicCollector) GetCollectCalls() []CollectOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.collectOpts
}

// SetError configures an error to return from Collect.
func (m *mockPanicCollector) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// SetDelay configures a delay for Collect.
func (m *mockPanicCollector) SetDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delay = delay
}

// -----------------------------------------------------------------------------
// Constructor Tests
// -----------------------------------------------------------------------------

// TestDefaultPanicRecoveryHandler_NewDefaultPanicRecoveryHandler tests constructor.
//
// # Description
//
// Verifies that the constructor creates a valid handler.
//
// # Test Steps
//
//  1. Create mock collector
//  2. Create handler
//  3. Verify not nil
func TestDefaultPanicRecoveryHandler_NewDefaultPanicRecoveryHandler(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)

	if handler == nil {
		t.Fatal("NewDefaultPanicRecoveryHandler returned nil")
	}

	if handler.collector != collector {
		t.Error("Collector not set")
	}

	if handler.output == nil {
		t.Error("Output not set")
	}

	if !handler.rePanic {
		t.Error("rePanic should default to true")
	}

	if handler.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", handler.timeout)
	}
}

// -----------------------------------------------------------------------------
// Wrap Tests
// -----------------------------------------------------------------------------

// TestDefaultPanicRecoveryHandler_Wrap_CapturesPanic tests panic capture.
//
// # Description
//
// Verifies that Wrap() captures panics and collects diagnostics.
//
// # Test Steps
//
//  1. Create handler with mock collector
//  2. Disable re-panic for testing
//  3. Trigger panic in wrapped function
//  4. Verify diagnostics collected
func TestDefaultPanicRecoveryHandler_Wrap_CapturesPanic(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("test panic")
	}()

	// Verify collector was called
	calls := collector.GetCollectCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 collect call, got %d", len(calls))
	}

	// Verify collect options
	opts := calls[0]
	if opts.Reason != "panic_recovery" {
		t.Errorf("Reason = %q, want panic_recovery", opts.Reason)
	}
	if opts.Severity != SeverityCritical {
		t.Errorf("Severity = %v, want SeverityCritical", opts.Severity)
	}
	if !strings.Contains(opts.Details, "test panic") {
		t.Error("Details should contain panic message")
	}
	if !strings.Contains(opts.Details, "Stack Trace") {
		t.Error("Details should contain stack trace")
	}
}

// TestDefaultPanicRecoveryHandler_Wrap_PrintsOutput tests output formatting.
//
// # Description
//
// Verifies that Wrap() prints crash report to output.
//
// # Test Steps
//
//  1. Create handler with captured output
//  2. Trigger panic
//  3. Verify output contains expected information
func TestDefaultPanicRecoveryHandler_Wrap_PrintsOutput(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("output test panic")
	}()

	output := buf.String()

	// Verify output contains expected information
	expectedStrings := []string{
		"ALEUTIAN CRASH REPORT",
		"output test panic",
		"Trace ID: mock-trace-id-12345",
		"Location: /tmp/test-diag.json",
		"github.com/AleutianAI/AleutianFOSS/issues",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Output missing %q", expected)
		}
	}
}

// TestDefaultPanicRecoveryHandler_Wrap_RePanic tests re-panic behavior.
//
// # Description
//
// Verifies that Wrap() re-panics with original value when configured.
//
// # Test Steps
//
//  1. Create handler with re-panic enabled
//  2. Trigger panic
//  3. Verify re-panic occurs with same value
func TestDefaultPanicRecoveryHandler_Wrap_RePanic(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(true)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Verify re-panic occurs
	defer func() {
		r := recover()
		if r == nil {
			t.Error("Expected re-panic")
		}
		if r != "re-panic test" {
			t.Errorf("Re-panic value = %v, want 're-panic test'", r)
		}
	}()

	func() {
		defer handler.Wrap()()
		panic("re-panic test")
	}()

	t.Error("Should not reach here")
}

// TestDefaultPanicRecoveryHandler_Wrap_NoPanic tests no-panic case.
//
// # Description
//
// Verifies that Wrap() does nothing when no panic occurs.
//
// # Test Steps
//
//  1. Create handler
//  2. Execute wrapped function that doesn't panic
//  3. Verify no diagnostics collected
func TestDefaultPanicRecoveryHandler_Wrap_NoPanic(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Execute without panic
	func() {
		defer handler.Wrap()()
		// Normal execution
	}()

	// Verify no collection
	calls := collector.GetCollectCalls()
	if len(calls) != 0 {
		t.Errorf("Expected 0 collect calls, got %d", len(calls))
	}

	// Verify no output
	if buf.Len() != 0 {
		t.Errorf("Expected no output, got %q", buf.String())
	}
}

// TestDefaultPanicRecoveryHandler_Wrap_CollectorError tests collector failure.
//
// # Description
//
// Verifies that Wrap() handles collector errors gracefully.
//
// # Test Steps
//
//  1. Create handler with failing collector
//  2. Trigger panic
//  3. Verify error is handled and output shows failure
func TestDefaultPanicRecoveryHandler_Wrap_CollectorError(t *testing.T) {
	collector := newMockPanicCollector()
	collector.SetError(context.DeadlineExceeded)
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("collector error test")
	}()

	// Verify output shows failure
	output := buf.String()
	if !strings.Contains(output, "collection failed") {
		t.Error("Output should indicate collection failed")
	}
}

// TestDefaultPanicRecoveryHandler_Wrap_NilCollector tests nil collector handling.
//
// # Description
//
// Verifies that Wrap() handles nil collector gracefully.
//
// # Test Steps
//
//  1. Create handler with nil collector
//  2. Trigger panic
//  3. Verify no crash and output shows no collector
func TestDefaultPanicRecoveryHandler_Wrap_NilCollector(t *testing.T) {
	handler := NewDefaultPanicRecoveryHandler(nil)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("nil collector test")
	}()

	// Verify output shows no collector
	output := buf.String()
	if !strings.Contains(output, "No diagnostic collector") {
		t.Error("Output should indicate no collector")
	}
}

// -----------------------------------------------------------------------------
// Configuration Tests
// -----------------------------------------------------------------------------

// TestDefaultPanicRecoveryHandler_SetCollector tests collector replacement.
//
// # Description
//
// Verifies that SetCollector replaces the collector.
//
// # Test Steps
//
//  1. Create handler with initial collector
//  2. Replace collector
//  3. Trigger panic
//  4. Verify new collector was used
func TestDefaultPanicRecoveryHandler_SetCollector(t *testing.T) {
	collector1 := newMockPanicCollector()
	collector2 := newMockPanicCollector()

	handler := NewDefaultPanicRecoveryHandler(collector1)
	handler.SetRePanic(false)
	handler.SetCollector(collector2)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("collector replacement test")
	}()

	// Verify collector2 was used
	if len(collector1.GetCollectCalls()) != 0 {
		t.Error("Old collector should not be called")
	}
	if len(collector2.GetCollectCalls()) != 1 {
		t.Error("New collector should be called")
	}
}

// TestDefaultPanicRecoveryHandler_GetLastPanicResult tests result retrieval.
//
// # Description
//
// Verifies that GetLastPanicResult returns the captured result.
//
// # Test Steps
//
//  1. Create handler
//  2. Verify initial result is nil
//  3. Trigger panic
//  4. Verify result is stored
func TestDefaultPanicRecoveryHandler_GetLastPanicResult(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Initial result should be nil
	if handler.GetLastPanicResult() != nil {
		t.Error("Initial result should be nil")
	}

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("result test")
	}()

	// Verify result is stored
	result := handler.GetLastPanicResult()
	if result == nil {
		t.Fatal("Result should not be nil after panic")
	}
	if result.TraceID != "mock-trace-id-12345" {
		t.Errorf("TraceID = %q, want mock-trace-id-12345", result.TraceID)
	}
}

// TestDefaultPanicRecoveryHandler_SetTimeout tests timeout configuration.
//
// # Description
//
// Verifies that SetTimeout affects collection timeout.
//
// # Test Steps
//
//  1. Create handler with slow collector
//  2. Set short timeout
//  3. Trigger panic
//  4. Verify timeout error in result
func TestDefaultPanicRecoveryHandler_SetTimeout(t *testing.T) {
	collector := newMockPanicCollector()
	collector.SetDelay(5 * time.Second) // Slow collector

	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)
	handler.SetTimeout(100 * time.Millisecond) // Short timeout

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Trigger panic
	func() {
		defer handler.Wrap()()
		panic("timeout test")
	}()

	// Verify timeout error
	output := buf.String()
	if !strings.Contains(output, "collection failed") {
		t.Error("Should show collection failed due to timeout")
	}
}

// -----------------------------------------------------------------------------
// Thread Safety Tests
// -----------------------------------------------------------------------------

// TestDefaultPanicRecoveryHandler_ThreadSafety tests concurrent access.
//
// # Description
//
// Verifies that handler methods are thread-safe.
//
// # Test Steps
//
//  1. Create handler
//  2. Access methods from multiple goroutines
//  3. Verify no races (run with -race)
func TestDefaultPanicRecoveryHandler_ThreadSafety(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = handler.GetLastPanicResult()
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			for j := 0; j < 100; j++ {
				handler.SetOutput(&buf)
				handler.SetRePanic(false)
				handler.SetTimeout(time.Second)
			}
		}()
	}

	wg.Wait()
	// Success if no race conditions detected
}

// -----------------------------------------------------------------------------
// Helper Function Tests
// -----------------------------------------------------------------------------

// TestWrapWithPanicRecovery tests the wrapper helper.
//
// # Description
//
// Verifies that WrapWithPanicRecovery properly wraps functions.
//
// # Test Steps
//
//  1. Create handler
//  2. Wrap panicking function
//  3. Call wrapped function
//  4. Verify panic is handled
func TestWrapWithPanicRecovery(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)
	handler.SetRePanic(false)

	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Wrap a panicking function
	wrapped := WrapWithPanicRecovery(handler, func() {
		panic("wrapped function panic")
	})

	// Call wrapped function (should not panic)
	wrapped()

	// Verify panic was handled
	calls := collector.GetCollectCalls()
	if len(calls) != 1 {
		t.Errorf("Expected 1 collect call, got %d", len(calls))
	}
}

// TestWrapWithPanicRecovery_NoPanic tests wrapper with no panic.
//
// # Description
//
// Verifies that wrapped functions execute normally without panic.
//
// # Test Steps
//
//  1. Create handler
//  2. Wrap non-panicking function
//  3. Call wrapped function
//  4. Verify function executed
func TestWrapWithPanicRecovery_NoPanic(t *testing.T) {
	collector := newMockPanicCollector()
	handler := NewDefaultPanicRecoveryHandler(collector)

	executed := false

	// Wrap a normal function
	wrapped := WrapWithPanicRecovery(handler, func() {
		executed = true
	})

	// Call wrapped function
	wrapped()

	// Verify function executed
	if !executed {
		t.Error("Wrapped function should have executed")
	}

	// Verify no collection
	calls := collector.GetCollectCalls()
	if len(calls) != 0 {
		t.Errorf("Expected 0 collect calls, got %d", len(calls))
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestDefaultPanicRecoveryHandler_InterfaceCompliance tests interface.
//
// # Description
//
// Verifies that DefaultPanicRecoveryHandler satisfies PanicRecoveryHandler.
//
// # Test Steps
//
//  1. Assign to interface variable
//  2. Verify all methods work
func TestDefaultPanicRecoveryHandler_InterfaceCompliance(t *testing.T) {
	collector := newMockPanicCollector()
	var handler PanicRecoveryHandler = NewDefaultPanicRecoveryHandler(collector)

	// All methods should work
	handler.SetCollector(collector)
	_ = handler.GetLastPanicResult()
	_ = handler.Wrap()
}

// Compile-time interface verification.
var _ PanicRecoveryHandler = (*DefaultPanicRecoveryHandler)(nil)
var _ DiagnosticsCollector = (*mockPanicCollector)(nil)
