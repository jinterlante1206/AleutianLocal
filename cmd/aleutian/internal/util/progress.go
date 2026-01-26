// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package util

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// =============================================================================
// Progress Indicator Interface
// =============================================================================

// ProgressIndicator defines the interface for progress feedback.
//
// # Description
//
// ProgressIndicator provides visual feedback during long-running operations
// to prevent users from thinking the application has frozen.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Example
//
//	var indicator ProgressIndicator = NewSpinner(DefaultSpinnerConfig())
//	indicator.Start()
//	defer indicator.Stop()
//
// # Limitations
//
//   - Implementations may vary in display capabilities
//   - Some implementations may not work without TTY
//
// # Assumptions
//
//   - Start() can be called before Stop()
//   - SetMessage() can be called at any time
type ProgressIndicator interface {
	// Start begins the progress indication.
	Start()

	// Stop halts the progress indication.
	Stop()

	// SetMessage updates the displayed message.
	SetMessage(message string)

	// IsRunning returns whether the indicator is active.
	IsRunning() bool
}

// =============================================================================
// Spinner Configuration
// =============================================================================

// SpinnerConfig configures spinner behavior.
//
// # Description
//
// Controls the spinner's appearance, speed, and output destination.
// All fields have sensible defaults that can be overridden.
//
// # Example
//
//	config := SpinnerConfig{
//	    Message:  "Loading...",
//	    Interval: 100 * time.Millisecond,
//	    Writer:   os.Stderr,
//	}
//
// # Limitations
//
//   - Custom Frames must contain at least one element
//   - Writer must support ANSI escape codes for full functionality
type SpinnerConfig struct {
	// Message is the text displayed next to the spinner.
	Message string

	// Interval is the time between frame updates.
	// Default: 100ms
	Interval time.Duration

	// Frames are the animation characters.
	// Default: Braille dots (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏)
	Frames []string

	// Writer is where output is written.
	// Default: os.Stderr
	Writer io.Writer

	// HideCursor hides the terminal cursor while spinning.
	// Default: true
	HideCursor bool

	// ClearOnStop clears the spinner line when stopped.
	// Default: true
	ClearOnStop bool

	// SuccessMessage shown when StopSuccess is called.
	SuccessMessage string

	// FailureMessage shown when StopFailure is called.
	FailureMessage string
}

// =============================================================================
// Constructor Functions
// =============================================================================

// DefaultSpinnerConfig returns sensible defaults.
//
// # Description
//
// Returns a configuration with Braille dot animation, 100ms interval,
// writing to stderr. Suitable for most CLI use cases.
//
// # Inputs
//
//   - None
//
// # Outputs
//
//   - SpinnerConfig: Configuration with default values
//
// # Example
//
//	config := DefaultSpinnerConfig()
//	config.Message = "Custom message..."
//	spinner := NewSpinner(config)
//
// # Limitations
//
//   - Braille characters may not display on all terminals
//
// # Assumptions
//
//   - os.Stderr is available for writing
func DefaultSpinnerConfig() SpinnerConfig {
	return SpinnerConfig{
		Message:     "Working...",
		Interval:    100 * time.Millisecond,
		Frames:      []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		Writer:      os.Stderr,
		HideCursor:  true,
		ClearOnStop: true,
	}
}

// =============================================================================
// Spinner Struct
// =============================================================================

// Spinner provides animated progress feedback for CLI operations.
//
// # Description
//
// Spinner displays an animated character sequence with a message to
// indicate that a long-running operation is in progress. This prevents
// users from thinking the application has frozen.
//
// # Use Cases
//
//   - Waiting for containers to start
//   - Downloading files
//   - Health check waits
//   - Any operation > 1 second
//
// # Thread Safety
//
// Spinner is safe for concurrent use. Start/Stop can be called from
// different goroutines.
//
// # Example
//
//	spinner := NewSpinner(SpinnerConfig{Message: "Starting services..."})
//	spinner.Start()
//	defer spinner.Stop()
//
//	// ... long operation
//
//	spinner.SetMessage("Waiting for health checks...")
//	// ... more work
//
// # Limitations
//
//   - Requires TTY-compatible terminal for proper display
//   - ANSI escape codes may not work on all terminals
//   - Concurrent writes to same Writer may cause garbled output
//
// # Assumptions
//
//   - Terminal supports ANSI escape codes
//   - Only one spinner writes to Writer at a time
type Spinner struct {
	config  SpinnerConfig
	frame   int
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
}

// Compile-time interface check
var _ ProgressIndicator = (*Spinner)(nil)

// NewSpinner creates a new spinner with the given configuration.
//
// # Description
//
// Creates a spinner ready to be started. The spinner will not display
// anything until Start() is called. Zero values in config are replaced
// with sensible defaults.
//
// # Inputs
//
//   - config: Configuration for spinner behavior
//
// # Outputs
//
//   - *Spinner: New spinner (not yet started)
//
// # Example
//
//	spinner := NewSpinner(SpinnerConfig{
//	    Message: "Downloading model...",
//	    Frames:  []string{"|", "/", "-", "\\"},
//	})
//
// # Limitations
//
//   - Does not validate Writer supports ANSI codes
//
// # Assumptions
//
//   - Caller will call Start() when ready
func NewSpinner(config SpinnerConfig) *Spinner {
	if config.Interval <= 0 {
		config.Interval = 100 * time.Millisecond
	}
	if len(config.Frames) == 0 {
		config.Frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	if config.Writer == nil {
		config.Writer = os.Stderr
	}

	return &Spinner{
		config: config,
	}
}

// =============================================================================
// Spinner Methods
// =============================================================================

// Start begins the spinner animation.
//
// # Description
//
// Starts the background goroutine that animates the spinner.
// Safe to call multiple times (subsequent calls are no-ops).
// Hides the cursor if HideCursor is enabled.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - None
//
// # Example
//
//	spinner.Start()
//	defer spinner.Stop()
//
// # Limitations
//
//   - Creates a goroutine that must be cleaned up with Stop()
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	// Hide cursor if configured
	if s.config.HideCursor {
		if _, err := fmt.Fprint(s.config.Writer, "\033[?25l"); err != nil {
			slog.Warn("failed to hide cursor", "error", err)
		}
	}

	go s.spin()
}

// Stop halts the spinner animation.
//
// # Description
//
// Stops the spinner and optionally clears the line. Blocks until
// the spinner goroutine has fully stopped. Safe to call multiple
// times (subsequent calls are no-ops). Restores cursor if it was hidden.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - None
//
// # Example
//
//	spinner.Stop()
//
// # Limitations
//
//   - Blocks until background goroutine exits
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	// Clear line if configured
	if s.config.ClearOnStop {
		s.clearLine()
	}

	// Show cursor if it was hidden
	if s.config.HideCursor {
		if _, err := fmt.Fprint(s.config.Writer, "\033[?25h"); err != nil {
			slog.Warn("failed to show cursor", "error", err)
		}
	}
}

// StopSuccess stops and displays a success message.
//
// # Description
//
// Stops the spinner and displays a success indicator with the
// configured or provided message. Shows a checkmark (✓) prefix.
//
// # Inputs
//
//   - message: Optional message (uses SuccessMessage if empty)
//
// # Outputs
//
//   - None
//
// # Example
//
//	spinner.StopSuccess("Upload complete!")
//
// # Limitations
//
//   - Requires terminal to display ✓ character
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) StopSuccess(message string) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	s.clearLine()

	if message == "" {
		message = s.config.SuccessMessage
	}
	if message == "" {
		message = "Done"
	}
	if _, err := fmt.Fprintf(s.config.Writer, "\r✓ %s\n", message); err != nil {
		slog.Warn("failed to write success message", "error", err)
	}

	if s.config.HideCursor {
		if _, err := fmt.Fprint(s.config.Writer, "\033[?25h"); err != nil {
			slog.Warn("failed to show cursor", "error", err)
		}
	}
}

// StopFailure stops and displays a failure message.
//
// # Description
//
// Stops the spinner and displays a failure indicator with the
// configured or provided message. Shows an X mark (✗) prefix.
//
// # Inputs
//
//   - message: Optional message (uses FailureMessage if empty)
//
// # Outputs
//
//   - None
//
// # Example
//
//	spinner.StopFailure("Connection timed out")
//
// # Limitations
//
//   - Requires terminal to display ✗ character
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) StopFailure(message string) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	s.clearLine()

	if message == "" {
		message = s.config.FailureMessage
	}
	if message == "" {
		message = "Failed"
	}
	if _, err := fmt.Fprintf(s.config.Writer, "\r✗ %s\n", message); err != nil {
		slog.Warn("failed to write failure message", "error", err)
	}

	if s.config.HideCursor {
		if _, err := fmt.Fprint(s.config.Writer, "\033[?25h"); err != nil {
			slog.Warn("failed to show cursor", "error", err)
		}
	}
}

// SetMessage updates the displayed message.
//
// # Description
//
// Changes the message shown next to the spinner. Safe to call
// while the spinner is running. Thread-safe.
//
// # Inputs
//
//   - message: New message to display
//
// # Outputs
//
//   - None
//
// # Example
//
//	spinner.SetMessage("Downloading... 50%")
//
// # Limitations
//
//   - Long messages may wrap on narrow terminals
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) SetMessage(message string) {
	s.mu.Lock()
	s.config.Message = message
	s.mu.Unlock()
}

// IsRunning returns whether the spinner is active.
//
// # Description
//
// Returns whether the spinner is currently animating.
// Thread-safe snapshot; value may change immediately after return.
//
// # Inputs
//
//   - None (receiver only)
//
// # Outputs
//
//   - bool: true if spinner is running
//
// # Example
//
//	if spinner.IsRunning() {
//	    spinner.Stop()
//	}
//
// # Limitations
//
//   - Result is a point-in-time snapshot
//
// # Assumptions
//
//   - Receiver is not nil
func (s *Spinner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// spin is the main animation loop.
func (s *Spinner) spin() {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.render()
		case <-s.stopCh:
			return
		}
	}
}

// render draws the current frame.
func (s *Spinner) render() {
	s.mu.Lock()
	frame := s.config.Frames[s.frame%len(s.config.Frames)]
	message := s.config.Message
	s.frame++
	s.mu.Unlock()

	if _, err := fmt.Fprintf(s.config.Writer, "\r%s %s", frame, message); err != nil {
		slog.Warn("failed to render spinner frame", "error", err)
	}
}

// clearLine clears the current line.
func (s *Spinner) clearLine() {
	if _, err := fmt.Fprint(s.config.Writer, "\r\033[K"); err != nil {
		slog.Warn("failed to clear line", "error", err)
	}
}

// =============================================================================
// Convenience Functions
// =============================================================================

// SpinWhile runs a function with a spinner showing progress.
//
// # Description
//
// Convenience function that starts a spinner, runs the provided
// function, and stops the spinner when done. Shows success or
// failure based on the function's return value.
//
// # Inputs
//
//   - message: Message to display while running
//   - fn: Function to execute
//
// # Outputs
//
//   - error: Error from fn, or nil
//
// # Example
//
//	err := SpinWhile("Starting services...", func() error {
//	    return startAllServices()
//	})
//
// # Limitations
//
//   - Cannot update message during execution
//   - Uses default spinner configuration
//
// # Assumptions
//
//   - fn is not nil
func SpinWhile(message string, fn func() error) error {
	spinner := NewSpinner(SpinnerConfig{Message: message})
	spinner.Start()

	err := fn()

	if err != nil {
		spinner.StopFailure(err.Error())
	} else {
		spinner.StopSuccess("")
	}

	return err
}

// SpinWhileContext runs a function with a spinner, respecting context.
//
// # Description
//
// Like SpinWhile but stops the spinner if the context is cancelled.
// The function runs in a separate goroutine and may be abandoned
// if context is cancelled.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - message: Message to display
//   - fn: Function to execute
//
// # Outputs
//
//   - error: Error from fn, context error, or nil
//
// # Example
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	err := SpinWhileContext(ctx, "Waiting for health...", func() error {
//	    return waitForHealth(ctx)
//	})
//
// # Limitations
//
//   - fn continues running even if context is cancelled
//   - fn should check ctx.Done() for proper cancellation
//
// # Assumptions
//
//   - ctx and fn are not nil
func SpinWhileContext(ctx context.Context, message string, fn func() error) error {
	spinner := NewSpinner(SpinnerConfig{Message: message})
	spinner.Start()

	// Run fn in goroutine with panic recovery
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic recovered: %v", r)
			}
		}()
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil {
			spinner.StopFailure(err.Error())
		} else {
			spinner.StopSuccess("")
		}
		return err

	case <-ctx.Done():
		spinner.StopFailure("Cancelled")
		return ctx.Err()
	}
}
