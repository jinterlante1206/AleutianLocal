// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ux

import (
	"os"
	"testing"
)

// =============================================================================
// GetPersonality / SetPersonality Tests
// =============================================================================

func TestSetPersonality_AndGet(t *testing.T) {
	// Save original personality
	orig := GetPersonality()
	defer SetPersonality(orig)

	// Set a custom personality
	custom := Personality{
		Level:        PersonalityMinimal,
		Theme:        "custom",
		ShowTips:     false,
		NauticalMode: false,
	}
	SetPersonality(custom)

	// Verify it was set
	retrieved := GetPersonality()
	if retrieved.Level != PersonalityMinimal {
		t.Errorf("expected level %v, got %v", PersonalityMinimal, retrieved.Level)
	}
	if retrieved.Theme != "custom" {
		t.Errorf("expected theme 'custom', got %q", retrieved.Theme)
	}
	if retrieved.ShowTips != false {
		t.Errorf("expected ShowTips false, got %v", retrieved.ShowTips)
	}
	if retrieved.NauticalMode != false {
		t.Errorf("expected NauticalMode false, got %v", retrieved.NauticalMode)
	}
}

// =============================================================================
// SetPersonalityLevel Tests
// =============================================================================

func TestSetPersonalityLevel_Full(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	if GetPersonality().Level != PersonalityFull {
		t.Errorf("expected PersonalityFull, got %v", GetPersonality().Level)
	}
}

func TestSetPersonalityLevel_Standard(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityStandard)

	if GetPersonality().Level != PersonalityStandard {
		t.Errorf("expected PersonalityStandard, got %v", GetPersonality().Level)
	}
}

func TestSetPersonalityLevel_Minimal(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	if GetPersonality().Level != PersonalityMinimal {
		t.Errorf("expected PersonalityMinimal, got %v", GetPersonality().Level)
	}
}

func TestSetPersonalityLevel_Machine(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	if GetPersonality().Level != PersonalityMachine {
		t.Errorf("expected PersonalityMachine, got %v", GetPersonality().Level)
	}
}

// =============================================================================
// ParsePersonalityLevel Tests
// =============================================================================

func TestParsePersonalityLevel_Full(t *testing.T) {
	inputs := []string{"full", "Full", "FULL", "f"}
	for _, input := range inputs {
		result := ParsePersonalityLevel(input)
		if result != PersonalityFull {
			t.Errorf("ParsePersonalityLevel(%q) = %v, want PersonalityFull", input, result)
		}
	}
}

func TestParsePersonalityLevel_Standard(t *testing.T) {
	inputs := []string{"standard", "Standard", "STANDARD", "std", "s"}
	for _, input := range inputs {
		result := ParsePersonalityLevel(input)
		if result != PersonalityStandard {
			t.Errorf("ParsePersonalityLevel(%q) = %v, want PersonalityStandard", input, result)
		}
	}
}

func TestParsePersonalityLevel_Minimal(t *testing.T) {
	inputs := []string{"minimal", "Minimal", "MINIMAL", "min", "m"}
	for _, input := range inputs {
		result := ParsePersonalityLevel(input)
		if result != PersonalityMinimal {
			t.Errorf("ParsePersonalityLevel(%q) = %v, want PersonalityMinimal", input, result)
		}
	}
}

func TestParsePersonalityLevel_Machine(t *testing.T) {
	inputs := []string{"machine", "Machine", "MACHINE", "quiet", "q"}
	for _, input := range inputs {
		result := ParsePersonalityLevel(input)
		if result != PersonalityMachine {
			t.Errorf("ParsePersonalityLevel(%q) = %v, want PersonalityMachine", input, result)
		}
	}
}

func TestParsePersonalityLevel_Default(t *testing.T) {
	// Unknown inputs should default to standard
	inputs := []string{"unknown", "invalid", "", "xyz", "12345"}
	for _, input := range inputs {
		result := ParsePersonalityLevel(input)
		if result != PersonalityStandard {
			t.Errorf("ParsePersonalityLevel(%q) = %v, want PersonalityStandard (default)", input, result)
		}
	}
}

// =============================================================================
// InitPersonality Tests
// =============================================================================

func TestInitPersonality_WithEnvVar(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)
	defer os.Unsetenv("ALEUTIAN_PERSONALITY")

	os.Setenv("ALEUTIAN_PERSONALITY", "minimal")
	InitPersonality()

	if GetPersonality().Level != PersonalityMinimal {
		t.Errorf("expected PersonalityMinimal from env, got %v", GetPersonality().Level)
	}
}

func TestInitPersonality_WithEnvVar_Machine(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)
	defer os.Unsetenv("ALEUTIAN_PERSONALITY")

	os.Setenv("ALEUTIAN_PERSONALITY", "machine")
	InitPersonality()

	if GetPersonality().Level != PersonalityMachine {
		t.Errorf("expected PersonalityMachine from env, got %v", GetPersonality().Level)
	}
}

func TestInitPersonality_NoEnvVar(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	// Ensure env var is not set
	os.Unsetenv("ALEUTIAN_PERSONALITY")

	// In tests, stdout is typically not a terminal so we'll get machine mode
	InitPersonality()

	// Just verify it doesn't panic and sets some level
	level := GetPersonality().Level
	if level != PersonalityFull && level != PersonalityMachine {
		t.Errorf("expected PersonalityFull or PersonalityMachine, got %v", level)
	}
}

// =============================================================================
// isTerminal Tests
// =============================================================================

func TestIsTerminal(t *testing.T) {
	// In test environment, stdout is typically not a terminal
	result := isTerminal()
	// We can't assert a specific value since it depends on test environment
	// but we can verify it doesn't panic
	_ = result
}

// =============================================================================
// IsInteractive Tests
// =============================================================================

func TestIsInteractive_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	result := IsInteractive()
	if result != false {
		t.Error("expected IsInteractive to be false in machine mode")
	}
}

func TestIsInteractive_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	// Result depends on whether stdout is a terminal
	// In tests, it's usually not a terminal so result is false
	result := IsInteractive()
	// Can't assert specific value since it depends on terminal state
	_ = result
}

// =============================================================================
// ShouldShowProgress Tests
// =============================================================================

func TestShouldShowProgress_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	if ShouldShowProgress() != false {
		t.Error("expected ShouldShowProgress to be false in machine mode")
	}
}

func TestShouldShowProgress_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	if ShouldShowProgress() != true {
		t.Error("expected ShouldShowProgress to be true in full mode")
	}
}

func TestShouldShowProgress_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	if ShouldShowProgress() != true {
		t.Error("expected ShouldShowProgress to be true in minimal mode")
	}
}

func TestShouldShowProgress_StandardMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityStandard)

	if ShouldShowProgress() != true {
		t.Error("expected ShouldShowProgress to be true in standard mode")
	}
}

// =============================================================================
// ShouldShowColors Tests
// =============================================================================

func TestShouldShowColors_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	if ShouldShowColors() != false {
		t.Error("expected ShouldShowColors to be false in machine mode")
	}
}

func TestShouldShowColors_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	if ShouldShowColors() != true {
		t.Error("expected ShouldShowColors to be true in full mode")
	}
}

func TestShouldShowColors_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	if ShouldShowColors() != true {
		t.Error("expected ShouldShowColors to be true in minimal mode")
	}
}

// =============================================================================
// DefaultPersonality Tests
// =============================================================================

func TestDefaultPersonality(t *testing.T) {
	def := DefaultPersonality()

	if def.Level != PersonalityFull {
		t.Errorf("expected Level PersonalityFull, got %v", def.Level)
	}
	if def.Theme != "default" {
		t.Errorf("expected Theme 'default', got %q", def.Theme)
	}
	if def.ShowTips != true {
		t.Errorf("expected ShowTips true, got %v", def.ShowTips)
	}
	if def.NauticalMode != true {
		t.Errorf("expected NauticalMode true, got %v", def.NauticalMode)
	}
}

// =============================================================================
// PersonalityLevel Constants Tests
// =============================================================================

func TestPersonalityLevel_Values(t *testing.T) {
	if PersonalityFull != "full" {
		t.Errorf("expected PersonalityFull = 'full', got %q", PersonalityFull)
	}
	if PersonalityStandard != "standard" {
		t.Errorf("expected PersonalityStandard = 'standard', got %q", PersonalityStandard)
	}
	if PersonalityMinimal != "minimal" {
		t.Errorf("expected PersonalityMinimal = 'minimal', got %q", PersonalityMinimal)
	}
	if PersonalityMachine != "machine" {
		t.Errorf("expected PersonalityMachine = 'machine', got %q", PersonalityMachine)
	}
}

// =============================================================================
// Concurrency Safety Tests
// =============================================================================

func TestPersonality_ConcurrentAccess(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	done := make(chan bool, 10)

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(level PersonalityLevel) {
			SetPersonalityLevel(level)
			done <- true
		}(PersonalityLevel([]PersonalityLevel{PersonalityFull, PersonalityStandard, PersonalityMinimal, PersonalityMachine}[i%4]))
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			_ = GetPersonality()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
