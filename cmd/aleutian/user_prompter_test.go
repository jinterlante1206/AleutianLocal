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
Package main contains unit tests for UserPrompter.

# Testing Strategy

These tests verify:
  - InteractivePrompter correctly handles various user inputs
  - NonInteractivePrompter behaves correctly for --yes and --non-interactive
  - MockPrompter works correctly for test doubles
  - Error handling for edge cases
*/
package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// InteractivePrompter Tests
// -----------------------------------------------------------------------------

// TestInteractivePrompter_Confirm_Yes verifies yes responses.
func TestInteractivePrompter_Confirm_Yes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase y", "y\n", true},
		{"uppercase Y", "Y\n", true},
		{"lowercase yes", "yes\n", true},
		{"uppercase YES", "YES\n", true},
		{"mixed Yes", "Yes\n", true},
		{"with spaces", "  y  \n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			prompter := NewInteractivePrompterWithIO(reader, writer)

			ctx := context.Background()
			got, err := prompter.Confirm(ctx, "Continue?")
			if err != nil {
				t.Fatalf("Confirm() unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Confirm() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestInteractivePrompter_Confirm_No verifies no responses.
func TestInteractivePrompter_Confirm_No(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase n", "n\n", false},
		{"uppercase N", "N\n", false},
		{"lowercase no", "no\n", false},
		{"uppercase NO", "NO\n", false},
		{"empty input", "\n", false},
		{"random text", "maybe\n", false},
		{"number", "1\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			prompter := NewInteractivePrompterWithIO(reader, writer)

			ctx := context.Background()
			got, err := prompter.Confirm(ctx, "Continue?")
			if err != nil {
				t.Fatalf("Confirm() unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Confirm() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestInteractivePrompter_Confirm_Prompt verifies prompt is displayed.
func TestInteractivePrompter_Confirm_Prompt(t *testing.T) {
	reader := strings.NewReader("y\n")
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx := context.Background()
	_, _ = prompter.Confirm(ctx, "Delete all data?")

	output := writer.String()
	if !strings.Contains(output, "Delete all data?") {
		t.Errorf("prompt not displayed in output: %q", output)
	}
	if !strings.Contains(output, "[y/N]") {
		t.Errorf("hint not displayed in output: %q", output)
	}
}

// TestInteractivePrompter_Confirm_EOF verifies EOF handling.
func TestInteractivePrompter_Confirm_EOF(t *testing.T) {
	reader := strings.NewReader("") // EOF
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx := context.Background()
	got, err := prompter.Confirm(ctx, "Continue?")
	if err != nil {
		t.Fatalf("Confirm() unexpected error: %v", err)
	}
	if got != false {
		t.Errorf("Confirm() = %v, want false on EOF", got)
	}
}

// TestInteractivePrompter_Confirm_ContextCancelled verifies context handling.
func TestInteractivePrompter_Confirm_ContextCancelled(t *testing.T) {
	reader := strings.NewReader("y\n")
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	_, err := prompter.Confirm(ctx, "Continue?")
	if err == nil {
		t.Fatal("Confirm() expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Confirm() error = %v, want context.Canceled", err)
	}
}

// TestInteractivePrompter_Select_ValidChoice verifies valid selections.
func TestInteractivePrompter_Select_ValidChoice(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		options  []string
		expected int
	}{
		{"first option", "1\n", []string{"A", "B", "C"}, 0},
		{"second option", "2\n", []string{"A", "B", "C"}, 1},
		{"last option", "3\n", []string{"A", "B", "C"}, 2},
		{"with spaces", "  2  \n", []string{"A", "B"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			prompter := NewInteractivePrompterWithIO(reader, writer)

			ctx := context.Background()
			got, err := prompter.Select(ctx, "Choose:", tt.options)
			if err != nil {
				t.Fatalf("Select() unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Select() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestInteractivePrompter_Select_InvalidChoice verifies error for invalid selection.
func TestInteractivePrompter_Select_InvalidChoice(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		options []string
	}{
		{"zero", "0\n", []string{"A", "B"}},
		{"too high", "5\n", []string{"A", "B"}},
		{"negative", "-1\n", []string{"A", "B"}},
		{"text", "abc\n", []string{"A", "B"}},
		{"empty", "\n", []string{"A", "B"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			prompter := NewInteractivePrompterWithIO(reader, writer)

			ctx := context.Background()
			_, err := prompter.Select(ctx, "Choose:", tt.options)
			if err == nil {
				t.Fatal("Select() expected error for invalid choice")
			}
			if !errors.Is(err, ErrInvalidSelection) {
				t.Errorf("Select() error = %v, want ErrInvalidSelection", err)
			}
		})
	}
}

// TestInteractivePrompter_Select_DisplaysOptions verifies options are displayed.
func TestInteractivePrompter_Select_DisplaysOptions(t *testing.T) {
	reader := strings.NewReader("1\n")
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx := context.Background()
	options := []string{"Fix automatically", "Skip", "Abort"}
	_, _ = prompter.Select(ctx, "Choose action:", options)

	output := writer.String()
	if !strings.Contains(output, "Choose action:") {
		t.Errorf("prompt not displayed: %q", output)
	}
	if !strings.Contains(output, "1. Fix automatically") {
		t.Errorf("option 1 not displayed: %q", output)
	}
	if !strings.Contains(output, "2. Skip") {
		t.Errorf("option 2 not displayed: %q", output)
	}
	if !strings.Contains(output, "3. Abort") {
		t.Errorf("option 3 not displayed: %q", output)
	}
}

// TestInteractivePrompter_Select_EmptyOptions verifies error for no options.
func TestInteractivePrompter_Select_EmptyOptions(t *testing.T) {
	reader := strings.NewReader("1\n")
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx := context.Background()
	_, err := prompter.Select(ctx, "Choose:", []string{})
	if err == nil {
		t.Fatal("Select() expected error for empty options")
	}
}

// TestInteractivePrompter_Select_ContextCancelled verifies context handling.
func TestInteractivePrompter_Select_ContextCancelled(t *testing.T) {
	reader := strings.NewReader("1\n")
	writer := &bytes.Buffer{}
	prompter := NewInteractivePrompterWithIO(reader, writer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := prompter.Select(ctx, "Choose:", []string{"A", "B"})
	if err == nil {
		t.Fatal("Select() expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Select() error = %v, want context.Canceled", err)
	}
}

// TestInteractivePrompter_IsInteractive verifies it returns true.
func TestInteractivePrompter_IsInteractive(t *testing.T) {
	prompter := NewInteractivePrompter()
	if !prompter.IsInteractive() {
		t.Error("IsInteractive() = false, want true")
	}
}

// -----------------------------------------------------------------------------
// NonInteractivePrompter Tests
// -----------------------------------------------------------------------------

// TestNonInteractivePrompter_Confirm_Rejects verifies prompt rejection.
func TestNonInteractivePrompter_Confirm_Rejects(t *testing.T) {
	prompter := NewNonInteractivePrompter()

	ctx := context.Background()
	_, err := prompter.Confirm(ctx, "Continue?")
	if err == nil {
		t.Fatal("Confirm() expected error in non-interactive mode")
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Errorf("Confirm() error = %v, want ErrNonInteractive", err)
	}
}

// TestNonInteractivePrompter_Select_Rejects verifies prompt rejection.
func TestNonInteractivePrompter_Select_Rejects(t *testing.T) {
	prompter := NewNonInteractivePrompter()

	ctx := context.Background()
	_, err := prompter.Select(ctx, "Choose:", []string{"A", "B"})
	if err == nil {
		t.Fatal("Select() expected error in non-interactive mode")
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Errorf("Select() error = %v, want ErrNonInteractive", err)
	}
}

// TestNonInteractivePrompter_IsInteractive verifies it returns false.
func TestNonInteractivePrompter_IsInteractive(t *testing.T) {
	prompter := NewNonInteractivePrompter()
	if prompter.IsInteractive() {
		t.Error("IsInteractive() = true, want false")
	}
}

// TestAutoApprovePrompter_Confirm_Approves verifies auto-approval.
func TestAutoApprovePrompter_Confirm_Approves(t *testing.T) {
	prompter := NewAutoApprovePrompter()

	ctx := context.Background()
	got, err := prompter.Confirm(ctx, "Delete everything?")
	if err != nil {
		t.Fatalf("Confirm() unexpected error: %v", err)
	}
	if !got {
		t.Error("Confirm() = false, want true for auto-approve")
	}
}

// TestAutoApprovePrompter_Select_SelectsFirst verifies first option selection.
func TestAutoApprovePrompter_Select_SelectsFirst(t *testing.T) {
	prompter := NewAutoApprovePrompter()

	ctx := context.Background()
	got, err := prompter.Select(ctx, "Choose:", []string{"First", "Second", "Third"})
	if err != nil {
		t.Fatalf("Select() unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("Select() = %d, want 0 for auto-approve", got)
	}
}

// TestAutoApprovePrompter_Select_EmptyOptions verifies error handling.
func TestAutoApprovePrompter_Select_EmptyOptions(t *testing.T) {
	prompter := NewAutoApprovePrompter()

	ctx := context.Background()
	_, err := prompter.Select(ctx, "Choose:", []string{})
	if err == nil {
		t.Fatal("Select() expected error for empty options")
	}
}

// TestAutoApprovePrompter_IsInteractive verifies it returns false.
func TestAutoApprovePrompter_IsInteractive(t *testing.T) {
	prompter := NewAutoApprovePrompter()
	if prompter.IsInteractive() {
		t.Error("IsInteractive() = true, want false for auto-approve")
	}
}

// -----------------------------------------------------------------------------
// MockPrompter Tests
// -----------------------------------------------------------------------------

// TestMockPrompter_Confirm verifies mock Confirm behavior.
func TestMockPrompter_Confirm(t *testing.T) {
	mock := &MockPrompter{
		ConfirmFunc: func(ctx context.Context, prompt string) (bool, error) {
			if prompt == "Delete data?" {
				return true, nil
			}
			return false, nil
		},
	}

	ctx := context.Background()

	// Test matching prompt
	got, err := mock.Confirm(ctx, "Delete data?")
	if err != nil || !got {
		t.Errorf("Confirm() = (%v, %v), want (true, nil)", got, err)
	}

	// Test non-matching prompt
	got, err = mock.Confirm(ctx, "Other prompt")
	if err != nil || got {
		t.Errorf("Confirm() = (%v, %v), want (false, nil)", got, err)
	}

	// Verify calls recorded
	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mock.Calls))
	}
	if mock.Calls[0].Method != "Confirm" || mock.Calls[0].Prompt != "Delete data?" {
		t.Errorf("call[0] = %+v, unexpected", mock.Calls[0])
	}
}

// TestMockPrompter_Select verifies mock Select behavior.
func TestMockPrompter_Select(t *testing.T) {
	mock := &MockPrompter{
		SelectFunc: func(ctx context.Context, prompt string, options []string) (int, error) {
			// Always select second option
			return 1, nil
		},
	}

	ctx := context.Background()
	options := []string{"A", "B", "C"}
	got, err := mock.Select(ctx, "Choose:", options)
	if err != nil {
		t.Fatalf("Select() unexpected error: %v", err)
	}
	if got != 1 {
		t.Errorf("Select() = %d, want 1", got)
	}

	// Verify call recorded with options
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].Method != "Select" {
		t.Errorf("call method = %q, want Select", mock.Calls[0].Method)
	}
	if len(mock.Calls[0].Options) != 3 {
		t.Errorf("call options = %v, want 3 options", mock.Calls[0].Options)
	}
}

// TestMockPrompter_IsInteractive verifies default and custom behavior.
func TestMockPrompter_IsInteractive(t *testing.T) {
	// Default returns true
	mock := &MockPrompter{}
	if !mock.IsInteractive() {
		t.Error("IsInteractive() default = false, want true")
	}

	// Custom function
	mock.IsInteractiveFunc = func() bool { return false }
	if mock.IsInteractive() {
		t.Error("IsInteractive() custom = true, want false")
	}
}

// TestMockPrompter_Reset verifies call history reset.
func TestMockPrompter_Reset(t *testing.T) {
	mock := &MockPrompter{
		ConfirmFunc: func(ctx context.Context, prompt string) (bool, error) {
			return true, nil
		},
	}

	ctx := context.Background()
	_, _ = mock.Confirm(ctx, "test1")
	_, _ = mock.Confirm(ctx, "test2")

	if len(mock.Calls) != 2 {
		t.Fatalf("expected 2 calls before reset, got %d", len(mock.Calls))
	}

	mock.Reset()

	if len(mock.Calls) != 0 {
		t.Errorf("expected 0 calls after reset, got %d", len(mock.Calls))
	}
}

// TestMockPrompter_NilFunc_Panics verifies panic on unconfigured mock.
func TestMockPrompter_NilFunc_Panics(t *testing.T) {
	mock := &MockPrompter{} // No functions set

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when ConfirmFunc is nil")
		}
	}()

	ctx := context.Background()
	_, _ = mock.Confirm(ctx, "test")
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestUserPrompter_InterfaceCompliance verifies interface implementations.
func TestUserPrompter_InterfaceCompliance(t *testing.T) {
	// These will fail to compile if interfaces aren't implemented correctly
	var _ UserPrompter = (*InteractivePrompter)(nil)
	var _ UserPrompter = (*NonInteractivePrompter)(nil)
	var _ UserPrompter = (*MockPrompter)(nil)
}

// -----------------------------------------------------------------------------
// Error Type Tests
// -----------------------------------------------------------------------------

// TestErrors verifies error variables are properly defined.
func TestErrors(t *testing.T) {
	if ErrNonInteractive == nil {
		t.Error("ErrNonInteractive should not be nil")
	}
	if ErrCancelled == nil {
		t.Error("ErrCancelled should not be nil")
	}
	if ErrInvalidSelection == nil {
		t.Error("ErrInvalidSelection should not be nil")
	}
}
