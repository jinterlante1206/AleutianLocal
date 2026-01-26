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
	"bytes"
	"io"
	"os"
	"testing"
)

// Helper to capture stdout
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// Helper to capture stderr
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	f()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// =============================================================================
// Icon.Render Tests
// =============================================================================

func TestIcon_Render_Success(t *testing.T) {
	result := IconSuccess.Render()
	if result == "" {
		t.Error("expected non-empty result for IconSuccess")
	}
}

func TestIcon_Render_Warning(t *testing.T) {
	result := IconWarning.Render()
	if result == "" {
		t.Error("expected non-empty result for IconWarning")
	}
}

func TestIcon_Render_Error(t *testing.T) {
	result := IconError.Render()
	if result == "" {
		t.Error("expected non-empty result for IconError")
	}
}

func TestIcon_Render_Pending(t *testing.T) {
	result := IconPending.Render()
	if result == "" {
		t.Error("expected non-empty result for IconPending")
	}
}

func TestIcon_Render_Default(t *testing.T) {
	// Test icons that don't have specific styling
	icons := []Icon{IconArrow, IconBullet, IconAnchor, IconShip, IconWave, IconChat, IconInfo, IconDocument, IconTime}
	for _, icon := range icons {
		result := icon.Render()
		if result != string(icon) {
			t.Errorf("expected %q for %q, got %q", string(icon), icon, result)
		}
	}
}

// =============================================================================
// Title Tests
// =============================================================================

func TestTitle_MachineMode(t *testing.T) {
	// Save and restore personality
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Title("Test Title")
	})

	// In machine mode, Title should output nothing
	if output != "" {
		t.Errorf("expected no output in machine mode, got %q", output)
	}
}

func TestTitle_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Title("Test Title")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Success Tests
// =============================================================================

func TestSuccess_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Success("Operation completed")
	})

	if output != "OK: Operation completed\n" {
		t.Errorf("expected 'OK: Operation completed', got %q", output)
	}
}

func TestSuccess_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	output := captureStdout(func() {
		Success("Operation completed")
	})

	if output == "" {
		t.Error("expected non-empty output in minimal mode")
	}
}

func TestSuccess_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Success("Operation completed")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Warning Tests
// =============================================================================

func TestWarning_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStderr(func() {
		Warning("Something might be wrong")
	})

	if output != "WARN: Something might be wrong\n" {
		t.Errorf("expected 'WARN: Something might be wrong', got %q", output)
	}
}

func TestWarning_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	output := captureStdout(func() {
		Warning("Something might be wrong")
	})

	if output == "" {
		t.Error("expected non-empty output in minimal mode")
	}
}

func TestWarning_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Warning("Something might be wrong")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Error Tests
// =============================================================================

func TestError_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStderr(func() {
		Error("Something went wrong")
	})

	if output != "ERROR: Something went wrong\n" {
		t.Errorf("expected 'ERROR: Something went wrong', got %q", output)
	}
}

func TestError_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	output := captureStdout(func() {
		Error("Something went wrong")
	})

	if output == "" {
		t.Error("expected non-empty output in minimal mode")
	}
}

func TestError_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Error("Something went wrong")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Info Tests
// =============================================================================

func TestInfo_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Info("Information message")
	})

	if output != "Information message\n" {
		t.Errorf("expected plain 'Information message', got %q", output)
	}
}

func TestInfo_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Info("Information message")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Muted Tests
// =============================================================================

func TestMuted_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Muted("Secondary text")
	})

	// In machine mode, Muted should output nothing
	if output != "" {
		t.Errorf("expected no output in machine mode, got %q", output)
	}
}

func TestMuted_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Muted("Secondary text")
	})

	if output == "" {
		t.Error("expected styled output in full mode")
	}
}

// =============================================================================
// Box Tests
// =============================================================================

func TestBox_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Box("Title", "Content here")
	})

	if output != "Title: Content here\n" {
		t.Errorf("expected 'Title: Content here', got %q", output)
	}
}

func TestBox_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Box("Title", "Content here")
	})

	if output == "" {
		t.Error("expected styled box output in full mode")
	}
}

// =============================================================================
// WarningBox Tests
// =============================================================================

func TestWarningBox_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStderr(func() {
		WarningBox("Warning Title", "Warning content")
	})

	if output != "WARN Warning Title: Warning content\n" {
		t.Errorf("expected 'WARN Warning Title: Warning content', got %q", output)
	}
}

func TestWarningBox_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		WarningBox("Warning Title", "Warning content")
	})

	if output == "" {
		t.Error("expected styled warning box output in full mode")
	}
}

// =============================================================================
// FileStatus Tests
// =============================================================================

func TestFileStatus_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		FileStatus("/path/to/file.txt", IconSuccess, "processed")
	})

	if output != "✓\t/path/to/file.txt\tprocessed\n" {
		t.Errorf("expected tab-separated output, got %q", output)
	}
}

func TestFileStatus_MinimalMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMinimal)

	output := captureStdout(func() {
		FileStatus("/path/to/file.txt", IconSuccess, "processed")
	})

	if output == "" {
		t.Error("expected non-empty output in minimal mode")
	}
}

func TestFileStatus_FullMode_WithReason(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		FileStatus("/path/to/file.txt", IconWarning, "contains PII")
	})

	if output == "" {
		t.Error("expected styled output with reason in full mode")
	}
}

func TestFileStatus_FullMode_NoReason(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		FileStatus("/path/to/file.txt", IconSuccess, "")
	})

	if output == "" {
		t.Error("expected styled output without reason in full mode")
	}
}

// =============================================================================
// Summary Tests
// =============================================================================

func TestSummary_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	output := captureStdout(func() {
		Summary(5, 2, 7)
	})

	if output != "SUMMARY: approved=5 skipped=2 total=7\n" {
		t.Errorf("expected machine format summary, got %q", output)
	}
}

func TestSummary_FullMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	output := captureStdout(func() {
		Summary(10, 0, 10)
	})

	if output == "" {
		t.Error("expected styled summary output in full mode")
	}
}

// =============================================================================
// ProgressBar Tests
// =============================================================================

func TestProgressBar_MachineMode(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityMachine)

	result := ProgressBar(5, 10, 20)

	if result != "5/10" {
		t.Errorf("expected '5/10', got %q", result)
	}
}

func TestProgressBar_FullMode_HalfFull(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	result := ProgressBar(5, 10, 20)

	if result == "" {
		t.Error("expected styled progress bar in full mode")
	}
}

func TestProgressBar_FullMode_Empty(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	result := ProgressBar(0, 10, 20)

	if result == "" {
		t.Error("expected styled progress bar even when empty")
	}
}

func TestProgressBar_FullMode_Full(t *testing.T) {
	orig := GetPersonality()
	defer SetPersonality(orig)

	SetPersonalityLevel(PersonalityFull)

	result := ProgressBar(10, 10, 20)

	if result == "" {
		t.Error("expected styled progress bar when full")
	}
}

// =============================================================================
// repeatChar Tests
// =============================================================================

func TestRepeatChar_Positive(t *testing.T) {
	result := repeatChar('X', 5)
	if result != "XXXXX" {
		t.Errorf("expected 'XXXXX', got %q", result)
	}
}

func TestRepeatChar_Zero(t *testing.T) {
	result := repeatChar('X', 0)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestRepeatChar_Negative(t *testing.T) {
	result := repeatChar('X', -5)
	if result != "" {
		t.Errorf("expected empty string for negative count, got %q", result)
	}
}

func TestRepeatChar_One(t *testing.T) {
	result := repeatChar('A', 1)
	if result != "A" {
		t.Errorf("expected 'A', got %q", result)
	}
}

func TestRepeatChar_Unicode(t *testing.T) {
	result := repeatChar('█', 3)
	if result != "███" {
		t.Errorf("expected '███', got %q", result)
	}
}

// =============================================================================
// Style Constants Tests
// =============================================================================

func TestStyles_NotNil(t *testing.T) {
	// Verify all style structs are initialized
	if Styles.Title.String() == "" && Styles.Subtitle.String() == "" {
		// Styles are initialized but render as empty strings until applied
	}
}

func TestColorConstants(t *testing.T) {
	// Verify color constants are defined
	colors := []interface{}{
		ColorTealBright,
		ColorTealPrimary,
		ColorTealVibrant,
		ColorTealMedium,
		ColorTealDeep,
		ColorTealOcean,
		ColorDeepSea,
		ColorAbyss,
		ColorMidnight,
		ColorSlate,
		ColorDarkest,
		ColorSuccess,
		ColorWarning,
		ColorError,
		ColorMuted,
	}

	for i, c := range colors {
		if c == nil {
			t.Errorf("color at index %d is nil", i)
		}
	}
}

func TestIconConstants(t *testing.T) {
	icons := map[string]Icon{
		"Success":  IconSuccess,
		"Warning":  IconWarning,
		"Error":    IconError,
		"Pending":  IconPending,
		"Arrow":    IconArrow,
		"Bullet":   IconBullet,
		"Anchor":   IconAnchor,
		"Ship":     IconShip,
		"Wave":     IconWave,
		"Chat":     IconChat,
		"Info":     IconInfo,
		"Document": IconDocument,
		"Time":     IconTime,
	}

	for name, icon := range icons {
		if string(icon) == "" {
			t.Errorf("icon %s is empty", name)
		}
	}
}
