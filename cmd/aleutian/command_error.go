package main

import (
	"fmt"
	"strings"
)

// CommandError wraps a command execution failure with stderr context.
//
// # Description
//
// Provides rich error context for command failures, including the
// command that failed, exit code, and stderr output. Implements
// error interface and supports unwrapping.
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
type CommandError struct {
	// Command is the command that was executed.
	Command string

	// ExitCode is the process exit code (-1 if unknown).
	ExitCode int

	// Stderr contains the standard error output.
	Stderr string

	// Wrapped is the underlying error.
	Wrapped error
}

// Error returns a formatted error message.
//
// # Description
//
// Returns a human-readable error message that includes the command,
// exit code, and stderr output if available.
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
func (e *CommandError) Unwrap() error {
	return e.Wrapped
}

// HasStderr returns true if stderr output is available.
func (e *CommandError) HasStderr() bool {
	return e.Stderr != ""
}

// NewCommandError creates a CommandError with full context.
//
// # Description
//
// Creates a new CommandError with command name, exit code, stderr,
// and underlying error. Stderr is trimmed of leading/trailing whitespace.
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
func NewCommandError(cmd string, exitCode int, stderr string, wrapped error) *CommandError {
	return &CommandError{
		Command:  cmd,
		ExitCode: exitCode,
		Stderr:   strings.TrimSpace(stderr),
		Wrapped:  wrapped,
	}
}

// WrapCommandError wraps an existing error into a CommandError if it isn't already.
//
// # Description
//
// If the error is already a *CommandError, returns it as-is.
// Otherwise, creates a new CommandError wrapping the original.
//
// # Inputs
//
//   - err: Error to wrap
//   - cmd: Command name for context
//   - exitCode: Exit code (-1 if unknown)
//   - stderr: Standard error output
//
// # Outputs
//
//   - *CommandError: Wrapped error
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
// Walks the error chain looking for a CommandError with stderr.
// Returns the first stderr found, or empty string if none.
//
// # Inputs
//
//   - err: Error to extract stderr from
//
// # Outputs
//
//   - string: Stderr content or empty string
//
// # Example
//
//	stderr := ExtractStderr(err)
//	if stderr != "" {
//	    fmt.Fprintf(os.Stderr, "Command output:\n%s\n", stderr)
//	}
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
