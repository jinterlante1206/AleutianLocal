package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

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

// SpinnerConfig configures spinner behavior.
//
// # Description
//
// Controls the spinner's appearance, speed, and output destination.
//
// # Example
//
//	config := SpinnerConfig{
//	    Message:  "Loading...",
//	    Interval: 100 * time.Millisecond,
//	    Writer:   os.Stderr,
//	}
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

// DefaultSpinnerConfig returns sensible defaults.
//
// # Description
//
// Returns a configuration with Braille dot animation, 100ms interval,
// writing to stderr.
//
// # Outputs
//
//   - SpinnerConfig: Configuration with default values
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
type Spinner struct {
	config  SpinnerConfig
	frame   int
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
}

// NewSpinner creates a new spinner with the given configuration.
//
// # Description
//
// Creates a spinner ready to be started. The spinner will not display
// anything until Start() is called.
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

// Start begins the spinner animation.
//
// # Description
//
// Starts the background goroutine that animates the spinner.
// Safe to call multiple times (subsequent calls are no-ops).
//
// # Example
//
//	spinner.Start()
//	defer spinner.Stop()
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
		fmt.Fprint(s.config.Writer, "\033[?25l")
	}

	go s.spin()
}

// Stop halts the spinner animation.
//
// # Description
//
// Stops the spinner and optionally clears the line. Blocks until
// the spinner goroutine has fully stopped.
//
// # Example
//
//	spinner.Stop()
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
		fmt.Fprint(s.config.Writer, "\033[?25h")
	}
}

// StopSuccess stops and displays a success message.
//
// # Description
//
// Stops the spinner and displays a success indicator with the
// configured or provided message.
//
// # Inputs
//
//   - message: Optional message (uses SuccessMessage if empty)
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
	fmt.Fprintf(s.config.Writer, "\r✓ %s\n", message)

	if s.config.HideCursor {
		fmt.Fprint(s.config.Writer, "\033[?25h")
	}
}

// StopFailure stops and displays a failure message.
//
// # Description
//
// Stops the spinner and displays a failure indicator with the
// configured or provided message.
//
// # Inputs
//
//   - message: Optional message (uses FailureMessage if empty)
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
	fmt.Fprintf(s.config.Writer, "\r✗ %s\n", message)

	if s.config.HideCursor {
		fmt.Fprint(s.config.Writer, "\033[?25h")
	}
}

// SetMessage updates the displayed message.
//
// # Description
//
// Changes the message shown next to the spinner. Safe to call
// while the spinner is running.
//
// # Inputs
//
//   - message: New message to display
//
// # Example
//
//	spinner.SetMessage("Downloading... 50%")
func (s *Spinner) SetMessage(message string) {
	s.mu.Lock()
	s.config.Message = message
	s.mu.Unlock()
}

// IsRunning returns whether the spinner is active.
//
// # Outputs
//
//   - bool: true if spinner is running
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

	fmt.Fprintf(s.config.Writer, "\r%s %s", frame, message)
}

// clearLine clears the current line.
func (s *Spinner) clearLine() {
	fmt.Fprint(s.config.Writer, "\r\033[K")
}

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
func SpinWhileContext(ctx context.Context, message string, fn func() error) error {
	spinner := NewSpinner(SpinnerConfig{Message: message})
	spinner.Start()

	// Run fn in goroutine
	done := make(chan error, 1)
	go func() {
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

// Compile-time interface check
var _ ProgressIndicator = (*Spinner)(nil)
