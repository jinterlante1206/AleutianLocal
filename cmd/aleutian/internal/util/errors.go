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
	"fmt"
	"strings"
)

// =============================================================================
// Command Error Type
// =============================================================================

// CommandError wraps a command execution failure with stderr context.
//
// # Description
//
// Provides rich error context for command failures, including the
// command that failed, exit code, and stderr output. Implements
// the error interface and supports unwrapping via errors.Is/As.
//
// # Thread Safety
//
// CommandError is immutable after creation and safe for concurrent reads.
//
// # Example
//
//	err := NewCommandError("podman-compose up", 1, "disk full", originalErr)
//	fmt.Println(err.Error()) // "podman-compose up (exit 1): disk full"
//
//	var cmdErr *CommandError
//	if errors.As(err, &cmdErr) {
//	    fmt.Println(cmdErr.Stderr) // "disk full"
//	}
//
// # Limitations
//
//   - Stderr is stored as a single string, not streaming
//   - Large stderr output consumes memory
type CommandError struct {
	// Command is the command that was executed.
	Command string

	// ExitCode is the process exit code (-1 if unknown).
	ExitCode int

	// Stderr contains the standard error output (trimmed).
	Stderr string

	// Wrapped is the underlying error (may be nil).
	Wrapped error
}

// =============================================================================
// CommandError Methods
// =============================================================================

// Error returns a formatted error message.
//
// # Description
//
// Returns a human-readable error message that includes the command,
// exit code, and stderr output if available. Stderr takes priority
// over wrapped error in the message format.
//
// # Inputs
//
//   - e: The CommandError receiver (must not be nil)
//
// # Outputs
//
//   - string: Formatted error message
//
// # Example
//
//	err := &CommandError{Command: "ls", ExitCode: 2, Stderr: "not found"}
//	fmt.Println(err.Error()) // "ls (exit 2): not found"
//
// # Limitations
//
//   - Format is fixed and cannot be customized
//
// # Assumptions
//
//   - Receiver is not nil
func (e *CommandError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s (exit %d): %s", e.Command, e.ExitCode, e.Stderr)
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("%s (exit %d): %v", e.Command, e.ExitCode, e.Wrapped)
	}
	return fmt.Sprintf("%s (exit %d)", e.Command, e.ExitCode)
}

// Unwrap returns the underlying error.
//
// # Description
//
// Enables errors.Is() and errors.As() to work through the error chain.
// Returns nil if there is no wrapped error.
//
// # Inputs
//
//   - e: The CommandError receiver (must not be nil)
//
// # Outputs
//
//   - error: The wrapped error, or nil
//
// # Example
//
//	original := errors.New("connection refused")
//	cmdErr := NewCommandError("docker", 1, "", original)
//	fmt.Println(errors.Is(cmdErr, original)) // true
//
// # Assumptions
//
//   - Receiver is not nil
func (e *CommandError) Unwrap() error {
	return e.Wrapped
}

// HasStderr returns true if stderr output is available.
//
// # Description
//
// Checks whether the CommandError has captured stderr content.
// Useful for conditional formatting or display.
//
// # Inputs
//
//   - e: The CommandError receiver (must not be nil)
//
// # Outputs
//
//   - bool: true if Stderr is non-empty
//
// # Example
//
//	if cmdErr.HasStderr() {
//	    fmt.Fprintf(os.Stderr, "Output: %s\n", cmdErr.Stderr)
//	}
//
// # Assumptions
//
//   - Receiver is not nil
func (e *CommandError) HasStderr() bool {
	return e.Stderr != ""
}

// Compile-time interface satisfaction checks
var _ error = (*CommandError)(nil)

// =============================================================================
// Constructor Functions
// =============================================================================

// NewCommandError creates a CommandError with full context.
//
// # Description
//
// Creates a new CommandError with command name, exit code, stderr,
// and underlying error. Stderr is trimmed of leading/trailing whitespace
// to normalize output from various command sources.
//
// # Inputs
//
//   - cmd: The command that was executed (e.g., "podman-compose up")
//   - exitCode: Process exit code (-1 if unknown)
//   - stderr: Standard error output (will be trimmed)
//   - wrapped: Underlying error (may be nil)
//
// # Outputs
//
//   - *CommandError: New error with full context
//
// # Example
//
//	if err := cmd.Run(); err != nil {
//	    return NewCommandError("podman-compose up", exitCode, stderr.String(), err)
//	}
//
// # Limitations
//
//   - Stderr is stored entirely in memory
//   - Does not validate cmd is non-empty
//
// # Assumptions
//
//   - Caller wants stderr trimmed (whitespace normalization)
func NewCommandError(cmd string, exitCode int, stderr string, wrapped error) *CommandError {
	return &CommandError{
		Command:  cmd,
		ExitCode: exitCode,
		Stderr:   strings.TrimSpace(stderr),
		Wrapped:  wrapped,
	}
}

// =============================================================================
// Utility Functions
// =============================================================================

// WrapCommandError wraps an existing error into a CommandError if it isn't already.
//
// # Description
//
// If the error is already a *CommandError, returns it as-is to prevent
// double-wrapping. Otherwise, creates a new CommandError wrapping the original.
// Returns nil if the input error is nil.
//
// # Inputs
//
//   - err: Error to wrap (may be nil)
//   - cmd: Command name for context
//   - exitCode: Exit code (-1 if unknown)
//   - stderr: Standard error output
//
// # Outputs
//
//   - *CommandError: Wrapped error, or nil if err was nil
//
// # Example
//
//	result := WrapCommandError(err, "docker build", exitCode, stderr)
//	if result != nil {
//	    return result
//	}
//
// # Limitations
//
//   - Only checks for direct *CommandError type, not wrapped
//
// # Assumptions
//
//   - Caller provides accurate command context
func WrapCommandError(err error, cmd string, exitCode int, stderr string) *CommandError {
	if err == nil {
		return nil
	}

	// Don't double-wrap
	if cmdErr, ok := err.(*CommandError); ok {
		return cmdErr
	}

	return NewCommandError(cmd, exitCode, stderr, err)
}

// ExtractStderr extracts stderr from an error chain.
//
// # Description
//
// Walks the error chain looking for a CommandError with non-empty stderr.
// Returns the first stderr found, or empty string if none exists.
// This is useful for displaying command output to users.
//
// # Inputs
//
//   - err: Error to extract stderr from (may be nil)
//
// # Outputs
//
//   - string: Stderr content, or empty string if not found
//
// # Example
//
//	stderr := ExtractStderr(err)
//	if stderr != "" {
//	    fmt.Fprintf(os.Stderr, "Command output:\n%s\n", stderr)
//	}
//
// # Limitations
//
//   - Only finds first stderr in chain (not all)
//   - Requires error to implement Unwrap() for chain walking
//
// # Assumptions
//
//   - Caller accepts empty string as "not found"
func ExtractStderr(err error) string {
	for err != nil {
		if cmdErr, ok := err.(*CommandError); ok && cmdErr.HasStderr() {
			return cmdErr.Stderr
		}
		// Try unwrapping
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = unwrapper.Unwrap()
	}
	return ""
}
