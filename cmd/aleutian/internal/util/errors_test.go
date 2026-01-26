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
	"errors"
	"fmt"
	"testing"
)

// =============================================================================
// CommandError.Error() Tests
// =============================================================================

// TestCommandError_Error_WithStderr verifies error message includes stderr.
//
// # Description
//
// When stderr is present, the error message should include it.
func TestCommandError_Error_WithStderr(t *testing.T) {
	err := &CommandError{
		Command:  "podman-compose up",
		ExitCode: 1,
		Stderr:   "disk full",
		Wrapped:  nil,
	}

	got := err.Error()
	want := "podman-compose up (exit 1): disk full"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestCommandError_Error_WithWrapped verifies error message includes wrapped error.
//
// # Description
//
// When no stderr but wrapped error exists, message should include wrapped error.
func TestCommandError_Error_WithWrapped(t *testing.T) {
	wrapped := errors.New("connection refused")
	err := &CommandError{
		Command:  "podman-compose up",
		ExitCode: 1,
		Stderr:   "",
		Wrapped:  wrapped,
	}

	got := err.Error()
	want := "podman-compose up (exit 1): connection refused"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestCommandError_Error_MinimalInfo verifies error message with just command and exit code.
//
// # Description
//
// When neither stderr nor wrapped error exists, message should be minimal.
func TestCommandError_Error_MinimalInfo(t *testing.T) {
	err := &CommandError{
		Command:  "podman-compose up",
		ExitCode: 127,
		Stderr:   "",
		Wrapped:  nil,
	}

	got := err.Error()
	want := "podman-compose up (exit 127)"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestCommandError_Error_StderrPriority verifies stderr takes priority over wrapped.
//
// # Description
//
// When both stderr and wrapped error exist, stderr should be shown.
func TestCommandError_Error_StderrPriority(t *testing.T) {
	wrapped := errors.New("should not appear")
	err := &CommandError{
		Command:  "cmd",
		ExitCode: 1,
		Stderr:   "stderr message",
		Wrapped:  wrapped,
	}

	got := err.Error()
	if got != "cmd (exit 1): stderr message" {
		t.Errorf("Error() = %q, stderr should take priority", got)
	}
}

// =============================================================================
// CommandError.Unwrap() Tests
// =============================================================================

// TestCommandError_Unwrap verifies Unwrap returns wrapped error.
//
// # Description
//
// Unwrap should return the wrapped error for use with errors.Is/As.
func TestCommandError_Unwrap(t *testing.T) {
	wrapped := errors.New("original error")
	err := &CommandError{
		Command:  "cmd",
		ExitCode: 1,
		Wrapped:  wrapped,
	}

	got := err.Unwrap()
	if got != wrapped {
		t.Errorf("Unwrap() = %v, want %v", got, wrapped)
	}
}

// TestCommandError_Unwrap_Nil verifies Unwrap returns nil when no wrapped error.
func TestCommandError_Unwrap_Nil(t *testing.T) {
	err := &CommandError{
		Command:  "cmd",
		ExitCode: 1,
		Wrapped:  nil,
	}

	if got := err.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

// TestCommandError_ErrorsIs verifies errors.Is works through the chain.
func TestCommandError_ErrorsIs(t *testing.T) {
	sentinel := errors.New("sentinel error")
	err := &CommandError{
		Command:  "cmd",
		ExitCode: 1,
		Wrapped:  sentinel,
	}

	if !errors.Is(err, sentinel) {
		t.Error("errors.Is should find wrapped sentinel error")
	}
}

// TestCommandError_ErrorsAs verifies errors.As works to extract CommandError.
func TestCommandError_ErrorsAs(t *testing.T) {
	cmdErr := &CommandError{
		Command:  "test-cmd",
		ExitCode: 42,
		Stderr:   "test stderr",
	}

	// Wrap in fmt.Errorf to create a chain
	wrapped := fmt.Errorf("wrapped: %w", cmdErr)

	var extracted *CommandError
	if !errors.As(wrapped, &extracted) {
		t.Fatal("errors.As should find CommandError in chain")
	}

	if extracted.Command != "test-cmd" {
		t.Errorf("extracted.Command = %q, want %q", extracted.Command, "test-cmd")
	}
	if extracted.ExitCode != 42 {
		t.Errorf("extracted.ExitCode = %d, want %d", extracted.ExitCode, 42)
	}
}

// =============================================================================
// CommandError.HasStderr() Tests
// =============================================================================

// TestCommandError_HasStderr verifies correct boolean return.
func TestCommandError_HasStderr(t *testing.T) {
	tests := []struct {
		name           string
		stderr         string
		useConstructor bool // Use NewCommandError for trimming behavior
		want           bool
	}{
		{"empty stderr", "", false, false},
		{"with content", "error output", false, true},
		{"whitespace only via constructor", "   ", true, false}, // Constructor trims to empty
		{"whitespace only direct", "   ", false, true},          // Direct assignment keeps whitespace
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err *CommandError
			if tt.useConstructor {
				err = NewCommandError("cmd", 0, tt.stderr, nil)
			} else {
				err = &CommandError{Stderr: tt.stderr}
			}
			if got := err.HasStderr(); got != tt.want {
				t.Errorf("HasStderr() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// NewCommandError Tests
// =============================================================================

// TestNewCommandError_CreatesCorrectly verifies constructor sets all fields.
func TestNewCommandError_CreatesCorrectly(t *testing.T) {
	wrapped := errors.New("underlying")
	err := NewCommandError("podman-compose up", 1, "disk full", wrapped)

	if err.Command != "podman-compose up" {
		t.Errorf("Command = %q, want %q", err.Command, "podman-compose up")
	}
	if err.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want %d", err.ExitCode, 1)
	}
	if err.Stderr != "disk full" {
		t.Errorf("Stderr = %q, want %q", err.Stderr, "disk full")
	}
	if err.Wrapped != wrapped {
		t.Errorf("Wrapped = %v, want %v", err.Wrapped, wrapped)
	}
}

// TestNewCommandError_TrimsStderr verifies stderr whitespace is trimmed.
func TestNewCommandError_TrimsStderr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"leading space", "  error", "error"},
		{"trailing space", "error  ", "error"},
		{"both sides", "  error  ", "error"},
		{"newlines", "\nerror\n", "error"},
		{"tabs", "\terror\t", "error"},
		{"complex", "  \n\t  error message  \t\n  ", "error message"},
		{"empty becomes empty", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewCommandError("cmd", 1, tt.input, nil)
			if err.Stderr != tt.want {
				t.Errorf("Stderr = %q, want %q", err.Stderr, tt.want)
			}
		})
	}
}

// TestNewCommandError_NilWrapped verifies nil wrapped error is handled.
func TestNewCommandError_NilWrapped(t *testing.T) {
	err := NewCommandError("cmd", 0, "", nil)

	if err.Wrapped != nil {
		t.Errorf("Wrapped = %v, want nil", err.Wrapped)
	}
	if err.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", err.Unwrap())
	}
}

// TestNewCommandError_NegativeExitCode verifies negative exit codes work.
func TestNewCommandError_NegativeExitCode(t *testing.T) {
	err := NewCommandError("cmd", -1, "", nil)

	if err.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", err.ExitCode)
	}

	got := err.Error()
	want := "cmd (exit -1)"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// =============================================================================
// WrapCommandError Tests
// =============================================================================

// TestWrapCommandError_NilError verifies nil input returns nil.
func TestWrapCommandError_NilError(t *testing.T) {
	got := WrapCommandError(nil, "cmd", 1, "stderr")

	if got != nil {
		t.Errorf("WrapCommandError(nil, ...) = %v, want nil", got)
	}
}

// TestWrapCommandError_AlreadyCommandError verifies no double-wrapping.
func TestWrapCommandError_AlreadyCommandError(t *testing.T) {
	original := &CommandError{
		Command:  "original-cmd",
		ExitCode: 42,
		Stderr:   "original stderr",
	}

	got := WrapCommandError(original, "new-cmd", 99, "new stderr")

	// Should return the same pointer
	if got != original {
		t.Error("WrapCommandError should return original CommandError, not wrap it")
	}
	if got.Command != "original-cmd" {
		t.Errorf("Command = %q, want %q", got.Command, "original-cmd")
	}
	if got.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", got.ExitCode)
	}
}

// TestWrapCommandError_WrapsStandardError verifies standard errors are wrapped.
func TestWrapCommandError_WrapsStandardError(t *testing.T) {
	original := errors.New("standard error")

	got := WrapCommandError(original, "cmd", 1, "stderr")

	if got == nil {
		t.Fatal("WrapCommandError returned nil for non-nil error")
	}
	if got.Command != "cmd" {
		t.Errorf("Command = %q, want %q", got.Command, "cmd")
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", got.ExitCode)
	}
	if got.Stderr != "stderr" {
		t.Errorf("Stderr = %q, want %q", got.Stderr, "stderr")
	}
	if got.Wrapped != original {
		t.Errorf("Wrapped = %v, want %v", got.Wrapped, original)
	}
}

// =============================================================================
// ExtractStderr Tests
// =============================================================================

// TestExtractStderr_DirectCommandError verifies extraction from direct CommandError.
func TestExtractStderr_DirectCommandError(t *testing.T) {
	err := &CommandError{
		Command: "cmd",
		Stderr:  "the stderr content",
	}

	got := ExtractStderr(err)
	if got != "the stderr content" {
		t.Errorf("ExtractStderr() = %q, want %q", got, "the stderr content")
	}
}

// TestExtractStderr_WrappedCommandError verifies extraction through error chain.
func TestExtractStderr_WrappedCommandError(t *testing.T) {
	cmdErr := &CommandError{
		Command: "cmd",
		Stderr:  "deep stderr",
	}
	wrapped := fmt.Errorf("level 1: %w", cmdErr)
	doubleWrapped := fmt.Errorf("level 0: %w", wrapped)

	got := ExtractStderr(doubleWrapped)
	if got != "deep stderr" {
		t.Errorf("ExtractStderr() = %q, want %q", got, "deep stderr")
	}
}

// TestExtractStderr_NoCommandError verifies empty string for non-CommandError.
func TestExtractStderr_NoCommandError(t *testing.T) {
	err := errors.New("standard error")

	got := ExtractStderr(err)
	if got != "" {
		t.Errorf("ExtractStderr() = %q, want empty string", got)
	}
}

// TestExtractStderr_NilError verifies empty string for nil.
func TestExtractStderr_NilError(t *testing.T) {
	got := ExtractStderr(nil)
	if got != "" {
		t.Errorf("ExtractStderr(nil) = %q, want empty string", got)
	}
}

// TestExtractStderr_EmptyStderr verifies empty stderr not returned.
func TestExtractStderr_EmptyStderr(t *testing.T) {
	err := &CommandError{
		Command: "cmd",
		Stderr:  "",
	}

	got := ExtractStderr(err)
	if got != "" {
		t.Errorf("ExtractStderr() = %q, want empty for empty stderr", got)
	}
}

// TestExtractStderr_FirstStderrWins verifies first stderr in chain is returned.
func TestExtractStderr_FirstStderrWins(t *testing.T) {
	inner := &CommandError{
		Command: "inner",
		Stderr:  "inner stderr",
	}
	outer := &CommandError{
		Command: "outer",
		Stderr:  "outer stderr",
		Wrapped: inner,
	}

	got := ExtractStderr(outer)
	// Should get outer's stderr first
	if got != "outer stderr" {
		t.Errorf("ExtractStderr() = %q, want %q (first in chain)", got, "outer stderr")
	}
}

// =============================================================================
// Interface Satisfaction Tests
// =============================================================================

// TestCommandError_ImplementsError verifies error interface.
func TestCommandError_ImplementsError(t *testing.T) {
	var _ error = (*CommandError)(nil)

	// Also verify through assignment
	var err error = &CommandError{Command: "test", ExitCode: 0}
	if err == nil {
		t.Error("CommandError should not be nil when assigned")
	}
}

// TestCommandError_ImplementsUnwrapper verifies unwrap interface.
func TestCommandError_ImplementsUnwrapper(t *testing.T) {
	// Interface check (anonymous interface from errors package)
	var err interface{ Unwrap() error } = &CommandError{}
	if err.Unwrap() != nil {
		t.Error("Empty CommandError should unwrap to nil")
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestCommandError_EmptyCommand verifies behavior with empty command.
func TestCommandError_EmptyCommand(t *testing.T) {
	err := NewCommandError("", 1, "stderr", nil)

	got := err.Error()
	want := " (exit 1): stderr"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestCommandError_LargeExitCode verifies behavior with large exit codes.
func TestCommandError_LargeExitCode(t *testing.T) {
	err := NewCommandError("cmd", 255, "", nil)

	got := err.Error()
	want := "cmd (exit 255)"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestCommandError_MultilineStderr verifies multiline stderr is preserved.
func TestCommandError_MultilineStderr(t *testing.T) {
	stderr := "line1\nline2\nline3"
	err := NewCommandError("cmd", 1, stderr, nil)

	if err.Stderr != stderr {
		t.Errorf("Stderr = %q, want %q", err.Stderr, stderr)
	}
}
