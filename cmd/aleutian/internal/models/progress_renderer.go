package models

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// ProgressRenderer Interface
// =============================================================================

// ProgressRenderer displays progress for long-running operations.
//
// # Description
//
// This interface abstracts progress display, enabling different implementations
// for TTY (interactive), non-TTY (CI), and headless (API) environments.
// All model download progress flows through this interface.
//
// # Security
//
//   - Implementations MUST NOT log model names to external services
//   - Progress data may contain network timing information (potential fingerprint)
//   - All output is sanitized to prevent terminal escape sequence injection
//
// # Thread Safety
//
// Implementations must be safe for concurrent Render calls from multiple
// goroutines (parallel downloads).
type ProgressRenderer interface {
	// Render displays progress for a named operation.
	//
	// # Description
	//
	// Updates the progress display for the given operation. May be called
	// many times per second during active downloads. Implementations should
	// handle rate limiting internally if needed.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - operation: Operation identifier (e.g., "pulling nomic-embed-text-v2-moe")
	//   - status: Current status text (e.g., "downloading layer 3/5")
	//   - completed: Bytes/units completed
	//   - total: Total bytes/units (0 if unknown)
	//
	// # Examples
	//
	//   renderer.Render(ctx, "model:gpt-oss", "pulling manifest", 0, 0)
	//   renderer.Render(ctx, "model:gpt-oss", "downloading", 1024*1024, 4*1024*1024*1024)
	//
	// # Limitations
	//
	//   - Completed/total may both be 0 for indeterminate progress
	//   - Operation names are not guaranteed unique across concurrent operations
	Render(ctx context.Context, operation, status string, completed, total int64)

	// Complete marks an operation as finished.
	//
	// # Description
	//
	// Called when an operation completes (success or failure).
	// Implementations should clear/finalize the progress display.
	//
	// # Inputs
	//
	//   - ctx: Context
	//   - operation: Operation identifier (same as passed to Render)
	//   - success: True if operation succeeded
	//   - message: Final status message
	Complete(ctx context.Context, operation string, success bool, message string)

	// SetOutput configures the output destination.
	//
	// # Description
	//
	// Allows runtime reconfiguration of where progress is written.
	// Passing nil should disable output (silent mode).
	//
	// # Inputs
	//
	//   - w: Writer for progress output (nil = silent)
	SetOutput(w io.Writer)

	// IsTTY returns true if output supports terminal features.
	//
	// # Description
	//
	// Used to determine if ANSI escape codes, carriage returns,
	// and other terminal features can be used.
	//
	// # Outputs
	//
	//   - bool: True if output supports TTY features
	IsTTY() bool
}

// =============================================================================
// Supporting Types
// =============================================================================

// rateSample records bytes completed at a point in time for rate calculation.
type rateSample struct {
	Time      time.Time
	Completed int64
}

// operationState tracks progress state for a single operation.
type operationState struct {
	StartTime     time.Time
	LastUpdate    time.Time
	LastCompleted int64
	Completed     int64
	Total         int64
	Status        string

	// Rolling window for rate calculation
	RateSamples   []rateSample
	RateWindowSec int
}

// MockRenderCall records a Render call for test verification.
type MockRenderCall struct {
	Operation string
	Status    string
	Completed int64
	Total     int64
}

// MockCompleteCall records a Complete call for test verification.
type MockCompleteCall struct {
	Operation string
	Success   bool
	Message   string
}

// =============================================================================
// DefaultProgressRenderer Struct
// =============================================================================

// DefaultProgressRenderer displays progress with a visual progress bar.
//
// # Description
//
// This implementation is designed for interactive TTY terminals. It uses
// carriage returns to update progress in place and displays a visual
// progress bar with percentage, transfer rate, and ETA.
//
// Use this when the terminal supports ANSI escape codes and carriage returns.
// For CI/non-interactive environments, use LineProgressRenderer instead.
//
// # Thread Safety
//
// Safe for concurrent use. Uses mutex to protect state.
//
// # Rate Limiting
//
// Updates are rate-limited to 10 per second to prevent terminal flicker.
type DefaultProgressRenderer struct {
	mu                sync.Mutex
	output            io.Writer
	operations        map[string]*operationState
	lastRender        time.Time
	minUpdateInterval time.Duration
	rateWindowSec     int
}

// =============================================================================
// LineProgressRenderer Struct
// =============================================================================

// LineProgressRenderer displays progress as individual log lines.
//
// # Description
//
// This implementation is designed for non-interactive environments like CI
// pipelines. It emits progress as separate lines without terminal escape codes.
//
// # Thread Safety
//
// Safe for concurrent use. Uses mutex to protect state.
//
// # Rate Limiting
//
// Updates are rate-limited to 1 per 5 seconds to prevent log flooding.
type LineProgressRenderer struct {
	mu                sync.Mutex
	output            io.Writer
	operations        map[string]*operationState
	lastRender        map[string]time.Time
	minUpdateInterval time.Duration
	rateWindowSec     int
}

// =============================================================================
// SilentProgressRenderer Struct
// =============================================================================

// SilentProgressRenderer suppresses all progress output.
//
// # Description
//
// This implementation is used for --quiet mode. It tracks state internally
// but produces no output during progress. Only the final Complete call
// produces a single summary line.
//
// # Thread Safety
//
// Safe for concurrent use. Uses mutex to protect state.
type SilentProgressRenderer struct {
	mu         sync.Mutex
	output     io.Writer
	operations map[string]*operationState
}

// =============================================================================
// MockProgressRenderer Struct
// =============================================================================

// MockProgressRenderer is a test double for ProgressRenderer.
//
// # Description
//
// Captures all calls for test verification. Can be configured with
// custom behavior via function fields.
//
// # Thread Safety
//
// Safe for concurrent use.
type MockProgressRenderer struct {
	mu sync.Mutex

	// Function fields for behavior customization
	RenderFunc   func(ctx context.Context, operation, status string, completed, total int64)
	CompleteFunc func(ctx context.Context, operation string, success bool, message string)

	// Call tracking
	RenderCalls   []MockRenderCall
	CompleteCalls []MockCompleteCall

	// Configuration
	TTYValue bool
	Output   io.Writer
}

// =============================================================================
// Compile-time Interface Checks
// =============================================================================

var (
	_ ProgressRenderer = (*DefaultProgressRenderer)(nil)
	_ ProgressRenderer = (*LineProgressRenderer)(nil)
	_ ProgressRenderer = (*SilentProgressRenderer)(nil)
	_ ProgressRenderer = (*MockProgressRenderer)(nil)
)

// =============================================================================
// operationState Methods
// =============================================================================

// calculateRate returns bytes per second using rolling window average.
//
// # Description
//
// Calculates transfer rate using samples from the rolling window.
// Removes stale samples older than the window duration.
//
// # Inputs
//
// None (uses internal state).
//
// # Outputs
//
//   - float64: Bytes per second, or 0 if insufficient data
//
// # Examples
//
//	rate := op.calculateRate()
//	if rate > 0 {
//	    fmt.Printf("Transfer rate: %.1f MB/s", rate/1024/1024)
//	}
//
// # Limitations
//
//   - Requires at least 2 samples in the window
//   - Returns 0 if elapsed time is 0
//
// # Assumptions
//
//   - RateSamples contains chronologically ordered samples
//   - RateWindowSec is positive
func (o *operationState) calculateRate() float64 {
	if len(o.RateSamples) < 2 {
		return 0
	}

	cutoff := time.Now().Add(-time.Duration(o.RateWindowSec) * time.Second)
	validSamples := make([]rateSample, 0, len(o.RateSamples))
	for _, s := range o.RateSamples {
		if s.Time.After(cutoff) {
			validSamples = append(validSamples, s)
		}
	}
	o.RateSamples = validSamples

	if len(validSamples) < 2 {
		return 0
	}

	first := validSamples[0]
	last := validSamples[len(validSamples)-1]
	elapsed := last.Time.Sub(first.Time).Seconds()
	if elapsed <= 0 {
		return 0
	}

	return float64(last.Completed-first.Completed) / elapsed
}

// calculateETA returns estimated time remaining.
//
// # Description
//
// Calculates ETA based on current rate and remaining bytes.
//
// # Inputs
//
// None (uses internal state).
//
// # Outputs
//
//   - time.Duration: Estimated time remaining, or 0 if cannot calculate
//
// # Examples
//
//	eta := op.calculateETA()
//	if eta > 0 {
//	    fmt.Printf("ETA: %s", eta)
//	}
//
// # Limitations
//
//   - Returns 0 if total is unknown (<=0)
//   - Returns 0 if rate is 0 or negative
//   - Returns 0 if already complete
//
// # Assumptions
//
//   - Total represents actual total size
//   - Rate is reasonably stable
func (o *operationState) calculateETA() time.Duration {
	if o.Total <= 0 || o.Completed >= o.Total {
		return 0
	}

	rate := o.calculateRate()
	if rate <= 0 {
		return 0
	}

	remaining := o.Total - o.Completed
	seconds := float64(remaining) / rate
	return time.Duration(seconds * float64(time.Second))
}

// =============================================================================
// DefaultProgressRenderer Constructor
// =============================================================================

// NewDefaultProgressRenderer creates a new TTY progress renderer.
//
// # Description
//
// Creates a renderer optimized for interactive terminals with progress bars.
// Assumes the output supports ANSI escape codes and carriage returns.
// Use LineProgressRenderer for CI/non-interactive environments.
//
// # Inputs
//
//   - w: Writer for progress output (typically os.Stdout)
//
// # Outputs
//
//   - *DefaultProgressRenderer: Configured renderer
//
// # Examples
//
//	renderer := NewDefaultProgressRenderer(os.Stdout)
//	renderer.Render(ctx, "download", "starting", 0, 100)
//
// # Limitations
//
//   - Assumes terminal supports ANSI escape codes
//   - Use LineProgressRenderer for environments without ANSI support
//
// # Assumptions
//
//   - Writer is valid and writable
//   - Terminal supports carriage return (\r) for in-place updates
func NewDefaultProgressRenderer(w io.Writer) *DefaultProgressRenderer {
	return &DefaultProgressRenderer{
		output:            w,
		operations:        make(map[string]*operationState),
		minUpdateInterval: 100 * time.Millisecond,
		rateWindowSec:     5,
	}
}

// =============================================================================
// DefaultProgressRenderer Methods
// =============================================================================

// Render implements ProgressRenderer.
//
// # Description
//
// Updates the progress display for the given operation. Handles rate limiting
// and delegates to TTY or non-TTY rendering based on terminal detection.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - operation: Operation identifier
//   - status: Current status text
//   - completed: Bytes completed
//   - total: Total bytes (0 if unknown)
//
// # Outputs
//
// None (writes to configured output).
//
// # Examples
//
//	renderer.Render(ctx, "pulling model", "downloading", 1024, 4096)
//
// # Limitations
//
//   - Rate limited to 10 updates/second
//   - Silently returns if context is cancelled
//   - Silently returns if output is nil
//
// # Assumptions
//
//   - Completed <= Total when Total > 0
//   - Operation names are reasonably short (<100 chars)
func (r *DefaultProgressRenderer) Render(ctx context.Context, operation, status string, completed, total int64) {
	if ctx.Err() != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.output == nil {
		return
	}

	now := time.Now()

	op := r.getOrCreateOperation(operation, now)
	r.updateOperationState(op, status, completed, total, now)

	if r.shouldSkipRender(now) {
		return
	}

	r.lastRender = now
	op.LastUpdate = now
	op.LastCompleted = completed

	r.renderTTYProgress(operation, op)
}

// getOrCreateOperation returns existing operation state or creates new one.
//
// # Description
//
// Retrieves operation state from the map, creating it if not exists.
//
// # Inputs
//
//   - operation: Operation identifier
//   - now: Current time for initialization
//
// # Outputs
//
//   - *operationState: Operation state (never nil)
//
// # Examples
//
//	op := r.getOrCreateOperation("download", time.Now())
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - r.operations is initialized
func (r *DefaultProgressRenderer) getOrCreateOperation(operation string, now time.Time) *operationState {
	op, exists := r.operations[operation]
	if !exists {
		op = &operationState{
			StartTime:     now,
			RateWindowSec: r.rateWindowSec,
			RateSamples:   make([]rateSample, 0, 100),
		}
		r.operations[operation] = op
	}
	return op
}

// updateOperationState updates the operation with new progress data.
//
// # Description
//
// Updates operation state fields and adds a rate sample.
//
// # Inputs
//
//   - op: Operation state to update
//   - status: Current status text
//   - completed: Bytes completed
//   - total: Total bytes
//   - now: Current time
//
// # Outputs
//
// None (modifies op in place).
//
// # Examples
//
//	r.updateOperationState(op, "downloading", 1024, 4096, time.Now())
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - op is not nil
func (r *DefaultProgressRenderer) updateOperationState(op *operationState, status string, completed, total int64, now time.Time) {
	op.Completed = completed
	op.Total = total
	op.Status = status
	op.RateSamples = append(op.RateSamples, rateSample{Time: now, Completed: completed})
}

// shouldSkipRender returns true if render should be skipped due to rate limiting.
//
// # Description
//
// Checks if enough time has passed since last render.
//
// # Inputs
//
//   - now: Current time
//
// # Outputs
//
//   - bool: True if render should be skipped
//
// # Examples
//
//	if r.shouldSkipRender(time.Now()) {
//	    return
//	}
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - minUpdateInterval is positive
func (r *DefaultProgressRenderer) shouldSkipRender(now time.Time) bool {
	return now.Sub(r.lastRender) < r.minUpdateInterval
}

// renderTTYProgress renders progress for TTY terminals with progress bar.
//
// # Description
//
// Outputs a single-line progress display with progress bar, percentage,
// transfer rate, and ETA. Uses carriage return for in-place updates.
//
// # Inputs
//
//   - operation: Operation identifier
//   - op: Operation state
//
// # Outputs
//
// None (writes to r.output).
//
// # Examples
//
//	r.renderTTYProgress("download", op)
//	// Output: "  ⏳ download [████████░░░░░░░░░░░░] 40.0% (1.6 GB / 4.0 GB) 52.3 MB/s ETA: 2m 30s"
//
// # Limitations
//
//   - Caller must hold mutex
//   - Assumes terminal width >= 80 chars
//
// # Assumptions
//
//   - r.output is writable
//   - Terminal supports ANSI escape codes
func (r *DefaultProgressRenderer) renderTTYProgress(operation string, op *operationState) {
	pct := r.calculatePercentage(op)
	rate := op.calculateRate()
	eta := op.calculateETA()
	bar := r.buildProgressBar(op, 20)

	var line string
	if op.Total > 0 {
		line = fmt.Sprintf("\r  ⏳ %s [%s] %.1f%% (%s / %s) %s %s   ",
			sanitizeForTerminal(truncateString(operation, 30)),
			bar,
			pct,
			formatBytes(op.Completed),
			formatBytes(op.Total),
			formatRate(rate),
			formatETA(eta),
		)
	} else {
		line = fmt.Sprintf("\r  ⏳ %s: %s   ",
			sanitizeForTerminal(truncateString(operation, 30)),
			sanitizeForTerminal(op.Status),
		)
	}

	fmt.Fprint(r.output, line)
}

// calculatePercentage returns completion percentage.
//
// # Description
//
// Calculates percentage from completed and total values.
//
// # Inputs
//
//   - op: Operation state
//
// # Outputs
//
//   - float64: Percentage (0-100), or 0 if total is 0
//
// # Examples
//
//	pct := r.calculatePercentage(op)
//	fmt.Printf("%.1f%%", pct)
//
// # Limitations
//
//   - Returns 0 if total is 0
//
// # Assumptions
//
//   - op is not nil
func (r *DefaultProgressRenderer) calculatePercentage(op *operationState) float64 {
	if op.Total <= 0 {
		return 0
	}
	return float64(op.Completed) / float64(op.Total) * 100
}

// buildProgressBar creates a visual progress bar string.
//
// # Description
//
// Creates a progress bar using filled and empty characters.
//
// # Inputs
//
//   - op: Operation state
//   - width: Bar width in characters
//
// # Outputs
//
//   - string: Progress bar (e.g., "████████░░░░░░░░░░░░")
//
// # Examples
//
//	bar := r.buildProgressBar(op, 20)
//
// # Limitations
//
//   - Returns empty bar if total is 0
//   - Width should be positive
//
// # Assumptions
//
//   - op is not nil
func (r *DefaultProgressRenderer) buildProgressBar(op *operationState, width int) string {
	filled := 0
	if op.Total > 0 {
		filled = int(float64(width) * float64(op.Completed) / float64(op.Total))
		if filled > width {
			filled = width
		}
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// Complete implements ProgressRenderer.
//
// # Description
//
// Marks an operation as finished and outputs final status.
//
// # Inputs
//
//   - ctx: Context (unused but required by interface)
//   - operation: Operation identifier
//   - success: Whether operation succeeded
//   - message: Final status message
//
// # Outputs
//
// None (writes to configured output).
//
// # Examples
//
//	renderer.Complete(ctx, "download", true, "completed successfully")
//
// # Limitations
//
//   - Silently returns if output is nil
//
// # Assumptions
//
//   - Operation was previously started with Render
func (r *DefaultProgressRenderer) Complete(ctx context.Context, operation string, success bool, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.output == nil {
		return
	}

	duration := r.getOperationDuration(operation)
	r.deleteOperation(operation)

	icon := r.getCompletionIcon(success)

	r.renderTTYCompletion(operation, icon, message, duration)
}

// getOperationDuration returns the duration since operation start.
//
// # Description
//
// Calculates elapsed time from operation start to now.
//
// # Inputs
//
//   - operation: Operation identifier
//
// # Outputs
//
//   - time.Duration: Elapsed time, or 0 if operation not found
//
// # Examples
//
//	duration := r.getOperationDuration("download")
//
// # Limitations
//
//   - Caller must hold mutex
//   - Returns 0 if operation not found
//
// # Assumptions
//
//   - r.operations is initialized
func (r *DefaultProgressRenderer) getOperationDuration(operation string) time.Duration {
	if op, exists := r.operations[operation]; exists {
		return time.Since(op.StartTime)
	}
	return 0
}

// deleteOperation removes operation from tracking map.
//
// # Description
//
// Removes operation state from the map to free memory.
//
// # Inputs
//
//   - operation: Operation identifier
//
// # Outputs
//
// None (modifies r.operations).
//
// # Examples
//
//	r.deleteOperation("download")
//
// # Limitations
//
//   - Caller must hold mutex
//   - No-op if operation not found
//
// # Assumptions
//
//   - r.operations is initialized
func (r *DefaultProgressRenderer) deleteOperation(operation string) {
	delete(r.operations, operation)
}

// getCompletionIcon returns the appropriate icon for success/failure.
//
// # Description
//
// Returns checkmark for success, X for failure.
//
// # Inputs
//
//   - success: Whether operation succeeded
//
// # Outputs
//
//   - string: "✓" for success, "✗" for failure
//
// # Examples
//
//	icon := r.getCompletionIcon(true) // "✓"
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (r *DefaultProgressRenderer) getCompletionIcon(success bool) string {
	if success {
		return "✓"
	}
	return "✗"
}

// renderTTYCompletion renders completion message for TTY.
//
// # Description
//
// Outputs completion with carriage return to clear progress bar.
//
// # Inputs
//
//   - operation: Operation identifier
//   - icon: Success/failure icon
//   - message: Final message
//   - duration: Operation duration
//
// # Outputs
//
// None (writes to r.output).
//
// # Examples
//
//	r.renderTTYCompletion("download", "✓", "complete", 5*time.Second)
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - r.output is writable
func (r *DefaultProgressRenderer) renderTTYCompletion(operation, icon, message string, duration time.Duration) {
	fmt.Fprintf(r.output, "\r  %s %s: %s (%s)%s\n",
		icon,
		sanitizeForTerminal(operation),
		sanitizeForTerminal(message),
		formatDuration(duration),
		strings.Repeat(" ", 20),
	)
}

// SetOutput implements ProgressRenderer.
//
// # Description
//
// Reconfigures the output destination.
//
// # Inputs
//
//   - w: New output writer (nil disables output)
//
// # Outputs
//
// None (modifies internal state).
//
// # Examples
//
//	renderer.SetOutput(os.Stderr)
//	renderer.SetOutput(nil) // disable output
//
// # Limitations
//
// None.
//
// # Assumptions
//
//   - Writer supports ANSI escape codes for TTY rendering
func (r *DefaultProgressRenderer) SetOutput(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = w
}

// IsTTY implements ProgressRenderer.
//
// # Description
//
// Returns true because DefaultProgressRenderer is designed for TTY environments.
// Use LineProgressRenderer for non-TTY environments.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: Always true for DefaultProgressRenderer
//
// # Examples
//
//	if renderer.IsTTY() {
//	    // Use ANSI colors
//	}
//
// # Limitations
//
// None.
//
// # Assumptions
//
//   - Caller has chosen this renderer because output supports TTY
func (r *DefaultProgressRenderer) IsTTY() bool {
	return true
}

// =============================================================================
// LineProgressRenderer Constructor
// =============================================================================

// NewLineProgressRenderer creates a new line-based progress renderer.
//
// # Description
//
// Creates a renderer for CI/non-TTY environments that emits progress
// as separate log lines with timestamps.
//
// # Inputs
//
//   - w: Writer for progress output
//
// # Outputs
//
//   - *LineProgressRenderer: Configured renderer
//
// # Examples
//
//	renderer := NewLineProgressRenderer(os.Stdout)
//
// # Limitations
//
//   - Rate limited to 1 update per 5 seconds per operation
//
// # Assumptions
//
//   - Writer is valid and writable
func NewLineProgressRenderer(w io.Writer) *LineProgressRenderer {
	return &LineProgressRenderer{
		output:            w,
		operations:        make(map[string]*operationState),
		lastRender:        make(map[string]time.Time),
		minUpdateInterval: 5 * time.Second,
		rateWindowSec:     5,
	}
}

// =============================================================================
// LineProgressRenderer Methods
// =============================================================================

// Render implements ProgressRenderer.
//
// # Description
//
// Outputs progress as timestamped log lines. Rate limited per operation.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - operation: Operation identifier
//   - status: Current status text
//   - completed: Bytes completed
//   - total: Total bytes (0 if unknown)
//
// # Outputs
//
// None (writes to configured output).
//
// # Examples
//
//	renderer.Render(ctx, "download", "downloading", 1024, 4096)
//
// # Limitations
//
//   - Rate limited to 1 update per 5 seconds per operation
//   - Silently returns if context is cancelled
//
// # Assumptions
//
//   - Completed <= Total when Total > 0
func (r *LineProgressRenderer) Render(ctx context.Context, operation, status string, completed, total int64) {
	if ctx.Err() != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.output == nil {
		return
	}

	now := time.Now()

	op := r.getOrCreateOperation(operation, now)
	r.updateOperationState(op, status, completed, total, now)

	if r.shouldSkipRender(operation, now) {
		return
	}
	r.lastRender[operation] = now

	r.renderLine(operation, op, now)
}

// getOrCreateOperation returns existing operation state or creates new one.
//
// # Description
//
// Retrieves operation state from the map, creating it if not exists.
//
// # Inputs
//
//   - operation: Operation identifier
//   - now: Current time for initialization
//
// # Outputs
//
//   - *operationState: Operation state (never nil)
//
// # Examples
//
//	op := r.getOrCreateOperation("download", time.Now())
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - r.operations is initialized
func (r *LineProgressRenderer) getOrCreateOperation(operation string, now time.Time) *operationState {
	op, exists := r.operations[operation]
	if !exists {
		op = &operationState{
			StartTime:     now,
			RateWindowSec: r.rateWindowSec,
			RateSamples:   make([]rateSample, 0, 100),
		}
		r.operations[operation] = op
	}
	return op
}

// updateOperationState updates the operation with new progress data.
//
// # Description
//
// Updates operation state fields and adds a rate sample.
//
// # Inputs
//
//   - op: Operation state to update
//   - status: Current status text
//   - completed: Bytes completed
//   - total: Total bytes
//   - now: Current time
//
// # Outputs
//
// None (modifies op in place).
//
// # Examples
//
//	r.updateOperationState(op, "downloading", 1024, 4096, time.Now())
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - op is not nil
func (r *LineProgressRenderer) updateOperationState(op *operationState, status string, completed, total int64, now time.Time) {
	op.Completed = completed
	op.Total = total
	op.Status = status
	op.RateSamples = append(op.RateSamples, rateSample{Time: now, Completed: completed})
}

// shouldSkipRender returns true if render should be skipped due to rate limiting.
//
// # Description
//
// Checks if enough time has passed since last render for this operation.
//
// # Inputs
//
//   - operation: Operation identifier
//   - now: Current time
//
// # Outputs
//
//   - bool: True if render should be skipped
//
// # Examples
//
//	if r.shouldSkipRender("download", time.Now()) {
//	    return
//	}
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - minUpdateInterval is positive
func (r *LineProgressRenderer) shouldSkipRender(operation string, now time.Time) bool {
	if lastRender, ok := r.lastRender[operation]; ok {
		return now.Sub(lastRender) < r.minUpdateInterval
	}
	return false
}

// renderLine outputs a single progress log line.
//
// # Description
//
// Formats and writes a timestamped progress line.
//
// # Inputs
//
//   - operation: Operation identifier
//   - op: Operation state
//   - now: Current time for timestamp
//
// # Outputs
//
// None (writes to r.output).
//
// # Examples
//
//	r.renderLine("download", op, time.Now())
//
// # Limitations
//
//   - Caller must hold mutex
//
// # Assumptions
//
//   - r.output is writable
func (r *LineProgressRenderer) renderLine(operation string, op *operationState, now time.Time) {
	pct := r.calculatePercentage(op)
	rate := op.calculateRate()
	eta := op.calculateETA()
	timestamp := now.Format(time.RFC3339)

	var line string
	if op.Total > 0 {
		line = fmt.Sprintf("%s [INFO] %s: %s %.0f%% (%s/%s) %s ETA: %s\n",
			timestamp,
			sanitizeForTerminal(operation),
			sanitizeForTerminal(op.Status),
			pct,
			formatBytes(op.Completed),
			formatBytes(op.Total),
			formatRate(rate),
			formatETA(eta),
		)
	} else {
		line = fmt.Sprintf("%s [INFO] %s: %s\n",
			timestamp,
			sanitizeForTerminal(operation),
			sanitizeForTerminal(op.Status),
		)
	}

	fmt.Fprint(r.output, line)
}

// calculatePercentage returns completion percentage.
//
// # Description
//
// Calculates percentage from completed and total values.
//
// # Inputs
//
//   - op: Operation state
//
// # Outputs
//
//   - float64: Percentage (0-100), or 0 if total is 0
//
// # Examples
//
//	pct := r.calculatePercentage(op)
//
// # Limitations
//
//   - Returns 0 if total is 0
//
// # Assumptions
//
//   - op is not nil
func (r *LineProgressRenderer) calculatePercentage(op *operationState) float64 {
	if op.Total <= 0 {
		return 0
	}
	return float64(op.Completed) / float64(op.Total) * 100
}

// Complete implements ProgressRenderer.
//
// # Description
//
// Outputs a final completion log line.
//
// # Inputs
//
//   - ctx: Context (unused)
//   - operation: Operation identifier
//   - success: Whether operation succeeded
//   - message: Final status message
//
// # Outputs
//
// None (writes to configured output).
//
// # Examples
//
//	renderer.Complete(ctx, "download", true, "completed")
//
// # Limitations
//
//   - Silently returns if output is nil
//
// # Assumptions
//
// None.
func (r *LineProgressRenderer) Complete(ctx context.Context, operation string, success bool, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.output == nil {
		return
	}

	var duration time.Duration
	if op, exists := r.operations[operation]; exists {
		duration = time.Since(op.StartTime)
		delete(r.operations, operation)
	}
	delete(r.lastRender, operation)

	timestamp := time.Now().Format(time.RFC3339)
	level := "INFO"
	if !success {
		level = "ERROR"
	}

	fmt.Fprintf(r.output, "%s [%s] %s: %s (%s)\n",
		timestamp,
		level,
		sanitizeForTerminal(operation),
		sanitizeForTerminal(message),
		formatDuration(duration),
	)
}

// SetOutput implements ProgressRenderer.
//
// # Description
//
// Reconfigures the output destination.
//
// # Inputs
//
//   - w: New output writer (nil disables output)
//
// # Outputs
//
// None (modifies internal state).
//
// # Examples
//
//	renderer.SetOutput(os.Stderr)
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (r *LineProgressRenderer) SetOutput(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = w
}

// IsTTY implements ProgressRenderer.
//
// # Description
//
// Always returns false for line-based renderer.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: Always false
//
// # Examples
//
//	isTTY := renderer.IsTTY() // false
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (r *LineProgressRenderer) IsTTY() bool {
	return false
}

// =============================================================================
// SilentProgressRenderer Constructor
// =============================================================================

// NewSilentProgressRenderer creates a new silent progress renderer.
//
// # Description
//
// Creates a renderer that suppresses progress output. Only emits
// a single line on completion.
//
// # Inputs
//
//   - w: Writer for final completion message (nil = completely silent)
//
// # Outputs
//
//   - *SilentProgressRenderer: Configured renderer
//
// # Examples
//
//	renderer := NewSilentProgressRenderer(os.Stdout)
//	renderer := NewSilentProgressRenderer(nil) // completely silent
//
// # Limitations
//
//   - No progress output during operation
//
// # Assumptions
//
// None.
func NewSilentProgressRenderer(w io.Writer) *SilentProgressRenderer {
	return &SilentProgressRenderer{
		output:     w,
		operations: make(map[string]*operationState),
	}
}

// =============================================================================
// SilentProgressRenderer Methods
// =============================================================================

// Render implements ProgressRenderer.
//
// # Description
//
// Tracks state internally but produces no output.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - operation: Operation identifier
//   - status: Current status text
//   - completed: Bytes completed
//   - total: Total bytes (0 if unknown)
//
// # Outputs
//
// None (silent).
//
// # Examples
//
//	renderer.Render(ctx, "download", "downloading", 1024, 4096) // no output
//
// # Limitations
//
//   - Produces no output
//
// # Assumptions
//
// None.
func (r *SilentProgressRenderer) Render(ctx context.Context, operation, status string, completed, total int64) {
	if ctx.Err() != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.operations[operation]; !exists {
		r.operations[operation] = &operationState{
			StartTime: time.Now(),
		}
	}
	r.operations[operation].Completed = completed
	r.operations[operation].Total = total
}

// Complete implements ProgressRenderer.
//
// # Description
//
// Outputs a single completion line if output is configured.
//
// # Inputs
//
//   - ctx: Context (unused)
//   - operation: Operation identifier
//   - success: Whether operation succeeded
//   - message: Final status message
//
// # Outputs
//
// None (writes to configured output if not nil).
//
// # Examples
//
//	renderer.Complete(ctx, "download", true, "completed")
//	// Output: "✓ download: completed (5s)"
//
// # Limitations
//
//   - Only outputs if r.output is not nil
//
// # Assumptions
//
// None.
func (r *SilentProgressRenderer) Complete(ctx context.Context, operation string, success bool, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var duration time.Duration
	if op, exists := r.operations[operation]; exists {
		duration = time.Since(op.StartTime)
		delete(r.operations, operation)
	}

	if r.output == nil {
		return
	}

	icon := "✓"
	if !success {
		icon = "✗"
	}

	fmt.Fprintf(r.output, "%s %s: %s (%s)\n",
		icon,
		sanitizeForTerminal(operation),
		sanitizeForTerminal(message),
		formatDuration(duration),
	)
}

// SetOutput implements ProgressRenderer.
//
// # Description
//
// Reconfigures the output destination for completion messages.
//
// # Inputs
//
//   - w: New output writer (nil = no output)
//
// # Outputs
//
// None (modifies internal state).
//
// # Examples
//
//	renderer.SetOutput(os.Stderr)
//	renderer.SetOutput(nil) // completely silent
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (r *SilentProgressRenderer) SetOutput(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = w
}

// IsTTY implements ProgressRenderer.
//
// # Description
//
// Always returns false for silent renderer.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: Always false
//
// # Examples
//
//	isTTY := renderer.IsTTY() // false
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (r *SilentProgressRenderer) IsTTY() bool {
	return false
}

// =============================================================================
// MockProgressRenderer Constructor
// =============================================================================

// NewMockProgressRenderer creates a new mock progress renderer.
//
// # Description
//
// Creates a mock renderer for testing that records all calls.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *MockProgressRenderer: Configured mock
//
// # Examples
//
//	mock := NewMockProgressRenderer()
//	// Use in tests
//	assert.Equal(t, 1, mock.RenderCallCount())
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func NewMockProgressRenderer() *MockProgressRenderer {
	return &MockProgressRenderer{
		RenderCalls:   make([]MockRenderCall, 0),
		CompleteCalls: make([]MockCompleteCall, 0),
	}
}

// =============================================================================
// MockProgressRenderer Methods
// =============================================================================

// Render implements ProgressRenderer.
//
// # Description
//
// Records the call and optionally invokes custom function.
//
// # Inputs
//
//   - ctx: Context
//   - operation: Operation identifier
//   - status: Status text
//   - completed: Bytes completed
//   - total: Total bytes
//
// # Outputs
//
// None (records call internally).
//
// # Examples
//
//	mock.Render(ctx, "download", "downloading", 1024, 4096)
//	assert.Equal(t, 1, mock.RenderCallCount())
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) Render(ctx context.Context, operation, status string, completed, total int64) {
	m.mu.Lock()
	m.RenderCalls = append(m.RenderCalls, MockRenderCall{
		Operation: operation,
		Status:    status,
		Completed: completed,
		Total:     total,
	})
	m.mu.Unlock()

	if m.RenderFunc != nil {
		m.RenderFunc(ctx, operation, status, completed, total)
	}
}

// Complete implements ProgressRenderer.
//
// # Description
//
// Records the call and optionally invokes custom function.
//
// # Inputs
//
//   - ctx: Context
//   - operation: Operation identifier
//   - success: Whether succeeded
//   - message: Final message
//
// # Outputs
//
// None (records call internally).
//
// # Examples
//
//	mock.Complete(ctx, "download", true, "done")
//	assert.Equal(t, 1, mock.CompleteCallCount())
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) Complete(ctx context.Context, operation string, success bool, message string) {
	m.mu.Lock()
	m.CompleteCalls = append(m.CompleteCalls, MockCompleteCall{
		Operation: operation,
		Success:   success,
		Message:   message,
	})
	m.mu.Unlock()

	if m.CompleteFunc != nil {
		m.CompleteFunc(ctx, operation, success, message)
	}
}

// SetOutput implements ProgressRenderer.
//
// # Description
//
// Records the output writer.
//
// # Inputs
//
//   - w: Output writer
//
// # Outputs
//
// None.
//
// # Examples
//
//	mock.SetOutput(os.Stdout)
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) SetOutput(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Output = w
}

// IsTTY implements ProgressRenderer.
//
// # Description
//
// Returns configured TTY value.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: Configured TTYValue
//
// # Examples
//
//	mock.TTYValue = true
//	assert.True(t, mock.IsTTY())
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) IsTTY() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TTYValue
}

// Reset clears all recorded calls.
//
// # Description
//
// Resets call tracking for reuse in tests.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None (modifies internal state).
//
// # Examples
//
//	mock.Reset()
//	assert.Equal(t, 0, mock.RenderCallCount())
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RenderCalls = make([]MockRenderCall, 0)
	m.CompleteCalls = make([]MockCompleteCall, 0)
}

// RenderCallCount returns the number of Render calls.
//
// # Description
//
// Returns count of recorded Render calls for test assertions.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - int: Number of Render calls
//
// # Examples
//
//	count := mock.RenderCallCount()
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) RenderCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.RenderCalls)
}

// CompleteCallCount returns the number of Complete calls.
//
// # Description
//
// Returns count of recorded Complete calls for test assertions.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - int: Number of Complete calls
//
// # Examples
//
//	count := mock.CompleteCallCount()
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockProgressRenderer) CompleteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CompleteCalls)
}

// =============================================================================
// Helper Functions
// =============================================================================

// ansiEscapeRegex matches ANSI escape sequences.
var ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// sanitizeForTerminal removes ANSI escape codes and control characters.
//
// # Description
//
// Prevents terminal escape sequence injection attacks by stripping
// all ANSI codes and control characters from output.
//
// # Inputs
//
//   - s: String to sanitize
//
// # Outputs
//
//   - string: Sanitized string safe for terminal output
//
// # Examples
//
//	safe := sanitizeForTerminal("\x1b[31mred\x1b[0m") // "red"
//
// # Limitations
//
//   - Removes all control characters except newline and tab
//
// # Assumptions
//
//   - Input is valid UTF-8
//
// # Security
//
// This function is critical for preventing terminal injection attacks.
// Model names from external sources must be sanitized before display.
func sanitizeForTerminal(s string) string {
	s = ansiEscapeRegex.ReplaceAllString(s, "")

	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= 32 {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// formatBytes formats a byte count as human-readable string.
//
// # Description
//
// Converts byte counts to human-readable format with appropriate unit.
//
// # Inputs
//
//   - bytes: Byte count to format
//
// # Outputs
//
//   - string: Formatted string (e.g., "1.5 GB")
//
// # Examples
//
//	formatBytes(1024)       // "1.0 KB"
//	formatBytes(1073741824) // "1.0 GB"
//
// # Limitations
//
//   - Uses binary units (1 KB = 1024 bytes)
//
// # Assumptions
//
//   - bytes is non-negative
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatRate formats a byte rate as human-readable string.
//
// # Description
//
// Converts bytes per second to human-readable transfer rate.
//
// # Inputs
//
//   - bytesPerSec: Transfer rate in bytes per second
//
// # Outputs
//
//   - string: Formatted rate (e.g., "52.3 MB/s")
//
// # Examples
//
//	formatRate(52428800) // "50.0 MB/s"
//	formatRate(0)        // "-- MB/s"
//
// # Limitations
//
//   - Returns "-- MB/s" for zero or negative rates
//
// # Assumptions
//
// None.
func formatRate(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "-- MB/s"
	}

	const (
		KB = 1024.0
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytesPerSec >= GB:
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/GB)
	case bytesPerSec >= MB:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/MB)
	case bytesPerSec >= KB:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// formatDuration formats a duration as human-readable string.
//
// # Description
//
// Converts duration to compact human-readable format.
//
// # Inputs
//
//   - d: Duration to format
//
// # Outputs
//
//   - string: Formatted duration (e.g., "1m 5s")
//
// # Examples
//
//	formatDuration(65*time.Second)   // "1m 5s"
//	formatDuration(3665*time.Second) // "1h 1m 5s"
//
// # Limitations
//
//   - Returns "0s" for negative durations
//
// # Assumptions
//
// None.
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// formatETA formats an ETA duration as human-readable string.
//
// # Description
//
// Converts estimated time remaining to display format.
//
// # Inputs
//
//   - eta: Estimated time remaining
//
// # Outputs
//
//   - string: Formatted ETA (e.g., "ETA: 2m 30s")
//
// # Examples
//
//	formatETA(150*time.Second) // "ETA: 2m 30s"
//	formatETA(0)               // "calculating..."
//
// # Limitations
//
//   - Returns "calculating..." for zero/negative ETAs
//
// # Assumptions
//
// None.
func formatETA(eta time.Duration) string {
	if eta <= 0 {
		return "calculating..."
	}

	return "ETA: " + formatDuration(eta)
}

// truncateString truncates a string to max length with ellipsis.
//
// # Description
//
// Truncates long strings for display, adding ellipsis if truncated.
//
// # Inputs
//
//   - s: String to truncate
//   - maxLen: Maximum length
//
// # Outputs
//
//   - string: Truncated string with ellipsis if needed
//
// # Examples
//
//	truncateString("hello world", 8) // "hello..."
//	truncateString("hello", 10)      // "hello"
//
// # Limitations
//
//   - For maxLen <= 3, no ellipsis is added
//
// # Assumptions
//
//   - maxLen is positive
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
