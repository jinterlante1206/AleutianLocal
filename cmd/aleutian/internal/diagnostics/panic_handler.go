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
Package diagnostics provides PanicRecoveryHandler for crash diagnostics.

This file implements the "Black Box Recorder" pattern - capturing system state
exactly when a crash occurs, when it's most valuable for debugging.

# Open Core Architecture

This follows the Open Core model:

  - FOSS (DefaultPanicRecoveryHandler): Local crash files in ~/.aleutian/diagnostics/
  - Enterprise (SentryPanicRecoveryHandler): PII redaction + Sentry integration

The interface is public; the implementation dictates the value.

# Why Panic Recovery?

When Aleutian crashes:

  - Users lose context of what went wrong
  - Support tickets contain incomplete information
  - Reproducing issues is difficult

The panic handler captures:

  - Stack trace with goroutine information
  - System state at crash time
  - Container status that may have caused the crash
  - Trace ID for correlation with other telemetry

# Privacy Considerations

The panic handler MUST respect Privacy/PII policy:

  - Does NOT dump memory containing user prompts
  - Does NOT capture sensitive environment variables
  - All output passes through the sanitization pipeline

# Usage

	func main() {
	    collector, _ := NewDefaultDiagnosticsCollector("0.4.0")
	    panicHandler := NewDefaultPanicRecoveryHandler(collector)
	    defer panicHandler.Wrap()()

	    // Normal execution...
	}
*/
package diagnostics

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// DefaultPanicRecoveryHandler Implementation
// -----------------------------------------------------------------------------

// DefaultPanicRecoveryHandler captures diagnostics when the application panics.
//
// # Description
//
// This is the FOSS-tier implementation that captures crash diagnostics
// and saves them to the local filesystem. The trace ID and location are
// printed to stderr for user reference.
//
// # Enterprise Alternative
//
// SentryPanicRecoveryHandler (Enterprise) provides:
//   - Automatic PII redaction before capture
//   - Push to Sentry for centralized crash tracking
//   - Team notification integration
//
// # Capabilities
//
//   - Captures panic value and stack trace
//   - Collects system diagnostics at crash time
//   - Prints trace ID for support tickets
//   - Re-panics to preserve normal crash behavior
//
// # Thread Safety
//
// DefaultPanicRecoveryHandler is safe for concurrent use.
type DefaultPanicRecoveryHandler struct {
	// collector gathers system diagnostics on panic.
	collector DiagnosticsCollector

	// lastResult stores the result of the last panic capture.
	lastResult *DiagnosticsResult

	// mu protects lastResult.
	mu sync.RWMutex

	// output is where panic messages are written (default: os.Stderr).
	output io.Writer

	// rePanic controls whether to re-panic after capture (default: true).
	rePanic bool

	// timeout for diagnostic collection during panic.
	timeout time.Duration
}

// NewDefaultPanicRecoveryHandler creates a panic handler with the given collector.
//
// # Description
//
// Creates a FOSS-tier panic handler that captures diagnostics on crash.
// The collector must be fully initialized before panics can be captured.
//
// # Inputs
//
//   - collector: DiagnosticsCollector for capturing crash state
//
// # Outputs
//
//   - *DefaultPanicRecoveryHandler: Ready-to-use panic handler
//
// # Examples
//
//	collector, _ := NewDefaultDiagnosticsCollector("0.4.0")
//	panicHandler := NewDefaultPanicRecoveryHandler(collector)
//	defer panicHandler.Wrap()()
//
// # Limitations
//
//   - Collector must be initialized before any panics
//   - Cannot capture panics in the collector itself
//
// # Assumptions
//
//   - Collector is properly initialized
//   - Storage backend is accessible
func NewDefaultPanicRecoveryHandler(collector DiagnosticsCollector) *DefaultPanicRecoveryHandler {
	return &DefaultPanicRecoveryHandler{
		collector: collector,
		output:    os.Stderr,
		rePanic:   true,
		timeout:   30 * time.Second,
	}
}

// Wrap returns a function suitable for defer that captures panics.
//
// # Description
//
// Returns a closure that should be deferred at the start of main() or
// critical goroutines. On panic, it captures diagnostics before the
// process terminates.
//
// # Outputs
//
//   - func(): Closure to defer; call with () to execute
//
// # Examples
//
//	func main() {
//	    panicHandler := NewDefaultPanicRecoveryHandler(collector)
//	    defer panicHandler.Wrap()()
//
//	    // Normal execution...
//	    panic("something went wrong")
//	    // Diagnostics captured, trace ID printed, then re-panics
//	}
//
// # Behavior on Panic
//
//  1. Recovers the panic value
//  2. Captures stack trace
//  3. Collects diagnostics with SeverityCritical
//  4. Prints trace ID and location to stderr
//  5. Re-panics with original value (process still terminates)
//
// # Limitations
//
//   - Cannot capture diagnostics if defer is not set up
//   - Storage failures are logged but don't prevent panic propagation
//   - Memory exhaustion panics may not have resources to collect
//
// # Assumptions
//
//   - Collector is properly initialized before panic
//   - Storage backend is accessible (or fails gracefully)
func (h *DefaultPanicRecoveryHandler) Wrap() func() {
	return func() {
		if r := recover(); r != nil {
			h.handlePanic(r)
		}
	}
}

// SetCollector configures the diagnostics collector to use.
//
// # Description
//
// Replaces the current collector. Useful for late initialization or testing.
//
// # Inputs
//
//   - collector: DiagnosticsCollector for capturing panic state
//
// # Examples
//
//	panicHandler.SetCollector(newCollector)
//
// # Limitations
//
//   - Not thread-safe during panic
//
// # Assumptions
//
//   - Collector is properly initialized
func (h *DefaultPanicRecoveryHandler) SetCollector(collector DiagnosticsCollector) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.collector = collector
}

// GetLastPanicResult returns the result of the last panic capture.
//
// # Description
//
// Returns the diagnostic result from the most recent panic recovery.
// Useful for tests or for logging after a recovered panic.
//
// # Outputs
//
//   - *DiagnosticsResult: Last panic diagnostic, or nil if no panic captured
//
// # Examples
//
//	if result := panicHandler.GetLastPanicResult(); result != nil {
//	    fmt.Printf("Last panic trace ID: %s\n", result.TraceID)
//	}
//
// # Limitations
//
//   - Only returns the single most recent result
//   - Result is lost when process exits
//
// # Assumptions
//
//   - None
func (h *DefaultPanicRecoveryHandler) GetLastPanicResult() *DiagnosticsResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastResult
}

// SetOutput configures where panic messages are written.
//
// # Description
//
// Replaces the output writer. Useful for testing or redirecting output.
//
// # Inputs
//
//   - w: Writer for panic messages (default: os.Stderr)
//
// # Examples
//
//	var buf bytes.Buffer
//	panicHandler.SetOutput(&buf)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - Writer is valid and writable
func (h *DefaultPanicRecoveryHandler) SetOutput(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.output = w
}

// SetRePanic configures whether to re-panic after capture.
//
// # Description
//
// Controls whether the handler re-panics after capturing diagnostics.
// Useful for testing where you want to recover instead of crashing.
//
// # Inputs
//
//   - rePanic: Whether to re-panic (default: true)
//
// # Examples
//
//	panicHandler.SetRePanic(false) // For testing
//
// # Limitations
//
//   - Setting to false changes crash behavior
//
// # Assumptions
//
//   - Caller understands the implications
func (h *DefaultPanicRecoveryHandler) SetRePanic(rePanic bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rePanic = rePanic
}

// SetTimeout configures the timeout for diagnostic collection.
//
// # Description
//
// Sets the maximum time allowed for collecting diagnostics during panic.
// Prevents hanging indefinitely if collection is slow.
//
// # Inputs
//
//   - timeout: Maximum collection time (default: 30 seconds)
//
// # Examples
//
//	panicHandler.SetTimeout(10 * time.Second)
//
// # Limitations
//
//   - Too short may result in incomplete diagnostics
//
// # Assumptions
//
//   - Caller has considered collection time requirements
func (h *DefaultPanicRecoveryHandler) SetTimeout(timeout time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timeout = timeout
}

// -----------------------------------------------------------------------------
// Private Methods
// -----------------------------------------------------------------------------

// handlePanic processes a recovered panic value.
//
// # Description
//
// Core panic handling logic: captures diagnostics, prints info, re-panics.
//
// # Inputs
//
//   - panicValue: The value passed to panic()
//
// # Limitations
//
//   - Must complete before process terminates
//
// # Assumptions
//
//   - Called from recovered panic context
func (h *DefaultPanicRecoveryHandler) handlePanic(panicValue interface{}) {
	h.mu.RLock()
	collector := h.collector
	output := h.output
	rePanic := h.rePanic
	timeout := h.timeout
	h.mu.RUnlock()

	// Capture stack trace
	stackTrace := string(debug.Stack())

	// Build panic details
	details := h.buildPanicDetails(panicValue, stackTrace)

	// Attempt to collect diagnostics
	var result *DiagnosticsResult
	if collector != nil {
		result = h.collectPanicDiagnostics(collector, details, timeout)
	}

	// Store result
	h.storeResult(result)

	// Print panic information
	h.printPanicInfo(output, panicValue, result)

	// Re-panic if configured
	if rePanic {
		panic(panicValue)
	}
}

// buildPanicDetails creates a details string for the diagnostic.
//
// # Description
//
// Formats panic value and stack trace into a details string.
//
// # Inputs
//
//   - panicValue: The value passed to panic()
//   - stackTrace: Stack trace from debug.Stack()
//
// # Outputs
//
//   - string: Formatted details string
//
// # Examples
//
//	details := h.buildPanicDetails("runtime error", "<stack>")
//
// # Limitations
//
//   - Stack trace may be truncated for very deep stacks
//
// # Assumptions
//
//   - Panic value can be formatted as string
func (h *DefaultPanicRecoveryHandler) buildPanicDetails(panicValue interface{}, stackTrace string) string {
	// Get goroutine count
	numGoroutines := runtime.NumGoroutine()

	// Format details
	details := fmt.Sprintf(
		"Panic: %v\n\nGoroutines: %d\n\nStack Trace:\n%s",
		panicValue,
		numGoroutines,
		stackTrace,
	)

	// Truncate if too long (protect against massive stack traces)
	const maxDetailsLen = 50000
	if len(details) > maxDetailsLen {
		details = details[:maxDetailsLen] + "\n... (truncated)"
	}

	return details
}

// collectPanicDiagnostics attempts to collect diagnostics during panic.
//
// # Description
//
// Collects diagnostics with a timeout to prevent hanging.
//
// # Inputs
//
//   - collector: DiagnosticsCollector to use
//   - details: Panic details string
//   - timeout: Maximum collection time
//
// # Outputs
//
//   - *DiagnosticsResult: Result, or nil if collection fails
//
// # Examples
//
//	result := h.collectPanicDiagnostics(collector, details, 30*time.Second)
//
// # Limitations
//
//   - May timeout for slow storage backends
//
// # Assumptions
//
//   - Collector is still functional during panic
func (h *DefaultPanicRecoveryHandler) collectPanicDiagnostics(
	collector DiagnosticsCollector,
	details string,
	timeout time.Duration,
) *DiagnosticsResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := collector.Collect(ctx, CollectOptions{
		Reason:               "panic_recovery",
		Details:              details,
		Severity:             SeverityCritical,
		IncludeContainerLogs: true,
		ContainerLogLines:    100,
		IncludeSystemMetrics: true,
		Tags: map[string]string{
			"trigger": "panic",
		},
	})

	if err != nil {
		// Collection failed - log but continue
		return &DiagnosticsResult{
			Error: fmt.Sprintf("diagnostic collection failed: %v", err),
		}
	}

	return result
}

// storeResult saves the panic result for later retrieval.
//
// # Description
//
// Thread-safely stores the result.
//
// # Inputs
//
//   - result: Result to store (may be nil)
//
// # Examples
//
//	h.storeResult(result)
//
// # Limitations
//
//   - Only stores the most recent result
//
// # Assumptions
//
//   - None
func (h *DefaultPanicRecoveryHandler) storeResult(result *DiagnosticsResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastResult = result
}

// printPanicInfo prints panic information to the configured output.
//
// # Description
//
// Prints a user-friendly message with trace ID and location.
//
// # Inputs
//
//   - w: Writer to print to
//   - panicValue: The original panic value
//   - result: Diagnostic result (may be nil)
//
// # Examples
//
//	h.printPanicInfo(os.Stderr, "error", result)
//
// # Limitations
//
//   - Output may be lost if stderr is redirected
//
// # Assumptions
//
//   - Writer is valid
func (h *DefaultPanicRecoveryHandler) printPanicInfo(w io.Writer, panicValue interface{}, result *DiagnosticsResult) {
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "================================================================================\n")
	fmt.Fprintf(w, "ALEUTIAN CRASH REPORT\n")
	fmt.Fprintf(w, "================================================================================\n")
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "Panic: %v\n", panicValue)
	fmt.Fprintf(w, "\n")

	if result != nil && result.Error == "" {
		fmt.Fprintf(w, "Diagnostics captured successfully.\n")
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "  Trace ID: %s\n", result.TraceID)
		fmt.Fprintf(w, "  Location: %s\n", result.Location)
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "Please include this Trace ID when reporting issues:\n")
		fmt.Fprintf(w, "  https://github.com/jinterlante1206/AleutianLocal/issues\n")
	} else if result != nil {
		fmt.Fprintf(w, "Diagnostic collection failed: %s\n", result.Error)
	} else {
		fmt.Fprintf(w, "No diagnostic collector available.\n")
	}

	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "================================================================================\n")
	fmt.Fprintf(w, "\n")
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// WrapWithPanicRecovery is a convenience function for wrapping functions.
//
// # Description
//
// Wraps a function with panic recovery using the given handler.
// Returns a function that will not panic.
//
// # Inputs
//
//   - handler: PanicRecoveryHandler to use
//   - fn: Function to wrap
//
// # Outputs
//
//   - func(): Wrapped function that won't panic
//
// # Examples
//
//	wrapped := WrapWithPanicRecovery(handler, func() {
//	    // potentially panicking code
//	})
//	wrapped() // Won't crash the program
//
// # Limitations
//
//   - Handler's rePanic setting affects behavior
//
// # Assumptions
//
//   - Handler is properly initialized
func WrapWithPanicRecovery(handler PanicRecoveryHandler, fn func()) func() {
	return func() {
		defer handler.Wrap()()
		fn()
	}
}

// Compile-time interface compliance check.
var _ PanicRecoveryHandler = (*DefaultPanicRecoveryHandler)(nil)
