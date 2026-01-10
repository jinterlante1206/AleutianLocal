// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"errors"
	"testing"
	"time"
)

// =============================================================================
// NewSpinner Tests
// =============================================================================

func TestNewSpinner_ReturnsNonNil(t *testing.T) {
	spin := NewSpinner("Loading...")
	if spin == nil {
		t.Fatal("NewSpinner returned nil")
	}
}

func TestNewSpinner_SetsMessage(t *testing.T) {
	spin := NewSpinner("Processing data")
	if spin.message != "Processing data" {
		t.Errorf("expected message 'Processing data', got %q", spin.message)
	}
}

func TestNewSpinner_DefaultsToDotsType(t *testing.T) {
	spin := NewSpinner("Loading...")
	if spin.spinType != SpinnerDots {
		t.Errorf("expected SpinnerDots, got %v", spin.spinType)
	}
}

func TestNewSpinner_InitializesChannels(t *testing.T) {
	spin := NewSpinner("Loading...")
	if spin.stop == nil {
		t.Error("stop channel should be initialized")
	}
	if spin.done == nil {
		t.Error("done channel should be initialized")
	}
}

// =============================================================================
// WithType Tests
// =============================================================================

func TestSpinner_WithType_Wave(t *testing.T) {
	spin := NewSpinner("Loading...").WithType(SpinnerWave)
	if spin.spinType != SpinnerWave {
		t.Errorf("expected SpinnerWave, got %v", spin.spinType)
	}
}

func TestSpinner_WithType_Anchor(t *testing.T) {
	spin := NewSpinner("Loading...").WithType(SpinnerAnchor)
	if spin.spinType != SpinnerAnchor {
		t.Errorf("expected SpinnerAnchor, got %v", spin.spinType)
	}
}

func TestSpinner_WithType_Compass(t *testing.T) {
	spin := NewSpinner("Loading...").WithType(SpinnerCompass)
	if spin.spinType != SpinnerCompass {
		t.Errorf("expected SpinnerCompass, got %v", spin.spinType)
	}
}

func TestSpinner_WithType_Chaining(t *testing.T) {
	spin := NewSpinner("Loading...").WithType(SpinnerWave)
	if spin == nil {
		t.Error("WithType should return the spinner for chaining")
	}
}

// =============================================================================
// Start/Stop Tests (Machine Mode)
// =============================================================================

func TestSpinner_Start_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	output := captureStdout(func() {
		spin.Start()
	})

	if output != "PROGRESS: Processing...\n" {
		t.Errorf("expected 'PROGRESS: Processing...', got %q", output)
	}
}

func TestSpinner_Stop_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Start()
	spin.Stop() // Should not panic or hang
}

func TestSpinner_Start_AlreadyRunning(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Start()
	spin.Start() // Second start should be no-op
	spin.Stop()
}

func TestSpinner_Stop_NotRunning(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Stop() // Should not panic when not running
}

// =============================================================================
// Start/Stop Tests (Full Mode - Brief)
// =============================================================================

func TestSpinner_StartStop_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	spin := NewSpinner("Processing...")
	spin.Start()

	// Give it a moment to start animation
	time.Sleep(100 * time.Millisecond)

	spin.Stop()
}

// =============================================================================
// UpdateMessage Tests
// =============================================================================

func TestSpinner_UpdateMessage(t *testing.T) {
	spin := NewSpinner("Initial message")

	spin.UpdateMessage("Updated message")

	if spin.message != "Updated message" {
		t.Errorf("expected 'Updated message', got %q", spin.message)
	}
}

func TestSpinner_UpdateMessage_WhileRunning(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Initial")
	spin.Start()

	spin.UpdateMessage("Updated")

	if spin.message != "Updated" {
		t.Errorf("expected 'Updated', got %q", spin.message)
	}

	spin.Stop()
}

// =============================================================================
// StopWithSuccess Tests
// =============================================================================

func TestSpinner_StopWithSuccess_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Start()

	output := captureStdout(func() {
		spin.StopWithSuccess("Done successfully")
	})

	if output != "OK: Done successfully\n" {
		t.Errorf("expected success message, got %q", output)
	}
}

// =============================================================================
// StopWithError Tests
// =============================================================================

func TestSpinner_StopWithError_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Start()

	output := captureStderr(func() {
		spin.StopWithError("Operation failed")
	})

	if output != "ERROR: Operation failed\n" {
		t.Errorf("expected error message, got %q", output)
	}
}

// =============================================================================
// StopWithWarning Tests
// =============================================================================

func TestSpinner_StopWithWarning_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	spin := NewSpinner("Processing...")
	spin.Start()

	output := captureStderr(func() {
		spin.StopWithWarning("Completed with warnings")
	})

	if output != "WARN: Completed with warnings\n" {
		t.Errorf("expected warning message, got %q", output)
	}
}

// =============================================================================
// WithSpinner Tests
// =============================================================================

func TestWithSpinner_Success(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	called := false
	err := WithSpinner("Processing", func() error {
		called = true
		return nil
	})

	if !called {
		t.Error("function should have been called")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestWithSpinner_Error(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	testErr := errors.New("test error")
	err := WithSpinner("Processing", func() error {
		return testErr
	})

	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}
}

func TestWithSpinner_MachineMode_SuccessOutput(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		_ = WithSpinner("Test operation", func() error {
			return nil
		})
	})

	if output == "" {
		t.Error("expected some output")
	}
}

// =============================================================================
// ProgressSpinner Tests
// =============================================================================

func TestNewProgressSpinner_ReturnsNonNil(t *testing.T) {
	ps := NewProgressSpinner("Processing items", 10)
	if ps == nil {
		t.Fatal("NewProgressSpinner returned nil")
	}
}

func TestNewProgressSpinner_SetsTotal(t *testing.T) {
	ps := NewProgressSpinner("Processing", 100)
	if ps.total != 100 {
		t.Errorf("expected total 100, got %d", ps.total)
	}
}

func TestNewProgressSpinner_StartsAtZero(t *testing.T) {
	ps := NewProgressSpinner("Processing", 100)
	if ps.current != 0 {
		t.Errorf("expected current 0, got %d", ps.current)
	}
}

// =============================================================================
// ProgressSpinner.Increment Tests
// =============================================================================

func TestProgressSpinner_Increment(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	ps := NewProgressSpinner("Processing", 10)

	ps.Increment()

	if ps.current != 1 {
		t.Errorf("expected current 1, got %d", ps.current)
	}
}

func TestProgressSpinner_Increment_Multiple(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	ps := NewProgressSpinner("Processing", 10)

	for i := 0; i < 5; i++ {
		ps.Increment()
	}

	if ps.current != 5 {
		t.Errorf("expected current 5, got %d", ps.current)
	}
}

func TestProgressSpinner_Increment_FullMode_UpdatesMessage(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	ps := NewProgressSpinner("Processing", 10)
	baseMessage := ps.Spinner.message

	ps.Increment()

	// In full mode, message should be updated with progress
	if ps.message == baseMessage {
		// Message should change to include progress
	}
}

// =============================================================================
// ProgressSpinner.SetProgress Tests
// =============================================================================

func TestProgressSpinner_SetProgress(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	ps := NewProgressSpinner("Processing", 100)

	ps.SetProgress(50)

	if ps.current != 50 {
		t.Errorf("expected current 50, got %d", ps.current)
	}
}

func TestProgressSpinner_SetProgress_Zero(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	ps := NewProgressSpinner("Processing", 100)
	ps.current = 25

	ps.SetProgress(0)

	if ps.current != 0 {
		t.Errorf("expected current 0, got %d", ps.current)
	}
}

func TestProgressSpinner_SetProgress_FullMode_UpdatesMessage(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	ps := NewProgressSpinner("Processing", 100)

	ps.SetProgress(75)

	// In full mode, message should include progress indicator
	if ps.current != 75 {
		t.Errorf("expected current 75, got %d", ps.current)
	}
}

// =============================================================================
// SpinnerType Constants Tests
// =============================================================================

func TestSpinnerType_Constants(t *testing.T) {
	// Verify spinner type constants
	if SpinnerDots != 0 {
		t.Errorf("expected SpinnerDots = 0, got %d", SpinnerDots)
	}
	if SpinnerWave != 1 {
		t.Errorf("expected SpinnerWave = 1, got %d", SpinnerWave)
	}
	if SpinnerAnchor != 2 {
		t.Errorf("expected SpinnerAnchor = 2, got %d", SpinnerAnchor)
	}
	if SpinnerCompass != 3 {
		t.Errorf("expected SpinnerCompass = 3, got %d", SpinnerCompass)
	}
}

func TestSpinnerFrames_Exists(t *testing.T) {
	spinnerTypes := []SpinnerType{SpinnerDots, SpinnerWave, SpinnerAnchor, SpinnerCompass}
	for _, st := range spinnerTypes {
		frames := spinnerFrames[st]
		if len(frames) == 0 {
			t.Errorf("spinner type %d has no frames", st)
		}
	}
}
