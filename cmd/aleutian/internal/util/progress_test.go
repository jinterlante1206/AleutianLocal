// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package util

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// DefaultSpinnerConfig Tests
// =============================================================================

// TestDefaultSpinnerConfig verifies default values.
func TestDefaultSpinnerConfig(t *testing.T) {
	config := DefaultSpinnerConfig()

	if config.Message == "" {
		t.Error("Message should have default value")
	}
	if config.Interval <= 0 {
		t.Error("Interval should be positive")
	}
	if len(config.Frames) == 0 {
		t.Error("Frames should have default values")
	}
	if config.Writer == nil {
		t.Error("Writer should not be nil")
	}
}

// =============================================================================
// NewSpinner Tests
// =============================================================================

// TestNewSpinner verifies spinner creation.
func TestNewSpinner(t *testing.T) {
	tests := []struct {
		name   string
		config SpinnerConfig
	}{
		{
			name:   "with defaults",
			config: DefaultSpinnerConfig(),
		},
		{
			name: "with zero values",
			config: SpinnerConfig{
				Interval: 0, // Should be set to default
			},
		},
		{
			name: "with custom values",
			config: SpinnerConfig{
				Message:  "Loading...",
				Interval: 50 * time.Millisecond,
				Frames:   []string{"|", "/", "-", "\\"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spinner := NewSpinner(tt.config)
			if spinner == nil {
				t.Fatal("NewSpinner returned nil")
			}
			if spinner.IsRunning() {
				t.Error("New spinner should not be running")
			}
		})
	}
}

// =============================================================================
// Start/Stop Tests
// =============================================================================

// TestSpinner_StartStop verifies basic lifecycle.
func TestSpinner_StartStop(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:     "Test",
		Interval:    10 * time.Millisecond,
		Writer:      buf,
		HideCursor:  false,
		ClearOnStop: true,
	})

	if spinner.IsRunning() {
		t.Error("Spinner should not be running initially")
	}

	spinner.Start()

	if !spinner.IsRunning() {
		t.Error("Spinner should be running after Start()")
	}

	// Let it run a few frames
	time.Sleep(50 * time.Millisecond)

	spinner.Stop()

	if spinner.IsRunning() {
		t.Error("Spinner should not be running after Stop()")
	}

	// Buffer should have had output
	if buf.Len() == 0 {
		t.Error("Spinner should have written output")
	}
}

// TestSpinner_DoubleStart verifies idempotent start.
func TestSpinner_DoubleStart(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Interval:   10 * time.Millisecond,
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	spinner.Start() // Should be no-op

	if !spinner.IsRunning() {
		t.Error("Spinner should be running")
	}

	spinner.Stop()
}

// TestSpinner_DoubleStop verifies safe double stop.
func TestSpinner_DoubleStop(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Interval:   10 * time.Millisecond,
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)
	spinner.Stop()
	spinner.Stop() // Should be safe

	if spinner.IsRunning() {
		t.Error("Spinner should not be running after Stop()")
	}
}

// =============================================================================
// SetMessage Tests
// =============================================================================

// TestSpinner_SetMessage verifies message update.
func TestSpinner_SetMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Initial",
		Interval:   10 * time.Millisecond,
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)

	spinner.SetMessage("Updated")
	time.Sleep(30 * time.Millisecond)

	spinner.Stop()

	output := buf.String()
	if !strings.Contains(output, "Initial") {
		t.Error("Output should contain initial message")
	}
	if !strings.Contains(output, "Updated") {
		t.Error("Output should contain updated message")
	}
}

// =============================================================================
// StopSuccess Tests
// =============================================================================

// TestSpinner_StopSuccess verifies success output.
func TestSpinner_StopSuccess(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Interval:   10 * time.Millisecond,
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)
	spinner.StopSuccess("Completed successfully")

	output := buf.String()
	if !strings.Contains(output, "✓") {
		t.Error("Success output should contain checkmark")
	}
	if !strings.Contains(output, "Completed successfully") {
		t.Error("Success output should contain message")
	}
}

// TestSpinner_StopSuccess_DefaultMessage verifies default success message.
func TestSpinner_StopSuccess_DefaultMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:        "Test",
		Interval:       10 * time.Millisecond,
		Writer:         buf,
		HideCursor:     false,
		SuccessMessage: "All done!",
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)
	spinner.StopSuccess("") // Empty message should use default

	output := buf.String()
	if !strings.Contains(output, "All done!") {
		t.Error("Should use configured SuccessMessage")
	}
}

// =============================================================================
// StopFailure Tests
// =============================================================================

// TestSpinner_StopFailure verifies failure output.
func TestSpinner_StopFailure(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Interval:   10 * time.Millisecond,
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)
	spinner.StopFailure("Something went wrong")

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Error("Failure output should contain X mark")
	}
	if !strings.Contains(output, "Something went wrong") {
		t.Error("Failure output should contain message")
	}
}

// TestSpinner_StopFailure_DefaultMessage verifies default failure message.
func TestSpinner_StopFailure_DefaultMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:        "Test",
		Interval:       10 * time.Millisecond,
		Writer:         buf,
		HideCursor:     false,
		FailureMessage: "Operation failed",
	})

	spinner.Start()
	time.Sleep(30 * time.Millisecond)
	spinner.StopFailure("") // Empty message should use default

	output := buf.String()
	if !strings.Contains(output, "Operation failed") {
		t.Error("Should use configured FailureMessage")
	}
}

// =============================================================================
// Custom Frames Tests
// =============================================================================

// TestSpinner_CustomFrames verifies custom animation frames.
func TestSpinner_CustomFrames(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Interval:   10 * time.Millisecond,
		Frames:     []string{"A", "B", "C"},
		Writer:     buf,
		HideCursor: false,
	})

	spinner.Start()
	time.Sleep(50 * time.Millisecond)
	spinner.Stop()

	output := buf.String()
	// Should contain at least one of our custom frames
	hasFrame := strings.Contains(output, "A") ||
		strings.Contains(output, "B") ||
		strings.Contains(output, "C")
	if !hasFrame {
		t.Error("Output should contain custom frames")
	}
}

// =============================================================================
// SpinWhile Tests
// =============================================================================

// TestSpinWhile_Success verifies success path.
func TestSpinWhile_Success(t *testing.T) {
	var executed bool
	err := SpinWhile("Testing...", func() error {
		executed = true
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Errorf("SpinWhile returned error: %v", err)
	}
	if !executed {
		t.Error("Function should have been executed")
	}
}

// TestSpinWhile_Failure verifies failure path.
func TestSpinWhile_Failure(t *testing.T) {
	expectedErr := errors.New("test error")

	err := SpinWhile("Testing...", func() error {
		time.Sleep(50 * time.Millisecond)
		return expectedErr
	})

	if err != expectedErr {
		t.Errorf("SpinWhile error = %v, want %v", err, expectedErr)
	}
}

// =============================================================================
// SpinWhileContext Tests
// =============================================================================

// TestSpinWhileContext_Success verifies success with context.
func TestSpinWhileContext_Success(t *testing.T) {
	ctx := context.Background()

	var executed bool
	err := SpinWhileContext(ctx, "Testing...", func() error {
		executed = true
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Errorf("SpinWhileContext returned error: %v", err)
	}
	if !executed {
		t.Error("Function should have been executed")
	}
}

// TestSpinWhileContext_Cancelled verifies cancellation handling.
func TestSpinWhileContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel shortly after start
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := SpinWhileContext(ctx, "Testing...", func() error {
		time.Sleep(5 * time.Second) // Long operation
		return nil
	})

	if err != context.Canceled {
		t.Errorf("SpinWhileContext error = %v, want context.Canceled", err)
	}
}

// TestSpinWhileContext_Timeout verifies timeout handling.
func TestSpinWhileContext_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := SpinWhileContext(ctx, "Testing...", func() error {
		time.Sleep(5 * time.Second) // Longer than timeout
		return nil
	})

	if err != context.DeadlineExceeded {
		t.Errorf("SpinWhileContext error = %v, want context.DeadlineExceeded", err)
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestSpinner_InterfaceCompliance verifies interface satisfaction.
func TestSpinner_InterfaceCompliance(t *testing.T) {
	var _ ProgressIndicator = (*Spinner)(nil)
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestSpinner_StopNotRunning verifies safe stop when not running.
func TestSpinner_StopNotRunning(t *testing.T) {
	buf := &bytes.Buffer{}
	spinner := NewSpinner(SpinnerConfig{
		Message:    "Test",
		Writer:     buf,
		HideCursor: false,
	})

	// Stop without start - should not panic
	spinner.Stop()
	spinner.StopSuccess("Done")
	spinner.StopFailure("Failed")

	if spinner.IsRunning() {
		t.Error("Spinner should not be running")
	}
}
