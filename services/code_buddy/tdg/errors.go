// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"errors"
	"strconv"
)

// =============================================================================
// SENTINEL ERRORS
// =============================================================================

var (
	// ErrTestTimeout indicates test execution exceeded the timeout.
	ErrTestTimeout = errors.New("test execution timeout")

	// ErrSuiteTimeout indicates the full test suite exceeded the timeout.
	ErrSuiteTimeout = errors.New("test suite timeout")

	// ErrTotalTimeout indicates the entire TDG session exceeded the timeout.
	ErrTotalTimeout = errors.New("TDG session timeout")

	// ErrMaxTestRetries indicates max test generation retries exceeded.
	// This happens when the generated test keeps passing when it should fail.
	ErrMaxTestRetries = errors.New("max test generation retries exceeded")

	// ErrMaxFixRetries indicates max fix attempts exceeded.
	// This happens when the generated fix keeps failing the test.
	ErrMaxFixRetries = errors.New("max fix attempts exceeded")

	// ErrMaxRegressionFixes indicates max regression fix attempts exceeded.
	// This happens when fixing regressions keeps breaking other tests.
	ErrMaxRegressionFixes = errors.New("max regression fix attempts exceeded")

	// ErrRegressionDetected indicates the fix caused existing tests to fail.
	ErrRegressionDetected = errors.New("fix caused regression in existing tests")

	// ErrUnsupportedLanguage indicates no test configuration for the language.
	ErrUnsupportedLanguage = errors.New("no test configuration for language")

	// ErrTestWriteFailed indicates failure to write the test file to disk.
	ErrTestWriteFailed = errors.New("failed to write test file")

	// ErrPatchApplyFailed indicates failure to apply the code patch.
	ErrPatchApplyFailed = errors.New("failed to apply patch")

	// ErrPatchRollbackFailed indicates failure to rollback a patch.
	ErrPatchRollbackFailed = errors.New("failed to rollback patch")

	// ErrTestPassedUnexpectedly indicates the reproducer test passed when
	// it should have failed, meaning it doesn't actually reproduce the bug.
	ErrTestPassedUnexpectedly = errors.New("reproducer test passed unexpectedly")

	// ErrTestFailedUnexpectedly indicates the test failed after the fix
	// was applied, meaning the fix doesn't work.
	ErrTestFailedUnexpectedly = errors.New("test failed after fix applied")

	// ErrInvalidTestCase indicates the test case is malformed or incomplete.
	ErrInvalidTestCase = errors.New("invalid test case")

	// ErrInvalidPatch indicates the patch is malformed or incomplete.
	ErrInvalidPatch = errors.New("invalid patch")

	// ErrLLMGenerationFailed indicates the LLM failed to generate content.
	ErrLLMGenerationFailed = errors.New("LLM generation failed")

	// ErrContextAssemblyFailed indicates failure to assemble code context.
	ErrContextAssemblyFailed = errors.New("context assembly failed")

	// ErrNilContext indicates a nil context.Context was passed.
	ErrNilContext = errors.New("context must not be nil")

	// ErrEmptyRequest indicates an empty or invalid request.
	ErrEmptyRequest = errors.New("request must not be empty")

	// ErrAlreadyRunning indicates the controller is already running.
	ErrAlreadyRunning = errors.New("TDG controller already running")

	// ErrNotRunning indicates the controller is not running.
	ErrNotRunning = errors.New("TDG controller not running")
)

// =============================================================================
// ERROR TYPES
// =============================================================================

// TestExecutionError provides details about a test execution failure.
type TestExecutionError struct {
	// TestName is the name of the test that failed.
	TestName string

	// FilePath is the path to the test file.
	FilePath string

	// ExitCode is the exit code from the test process.
	ExitCode int

	// Output is the captured stdout/stderr.
	Output string

	// Cause is the underlying error if any.
	Cause error
}

// Error implements the error interface.
func (e *TestExecutionError) Error() string {
	if e.Cause != nil {
		return "test execution failed: " + e.Cause.Error()
	}
	return "test execution failed: exit code " + strconv.Itoa(e.ExitCode)
}

// Unwrap returns the underlying error.
func (e *TestExecutionError) Unwrap() error {
	return e.Cause
}

// StateTransitionError indicates an invalid state transition was attempted.
type StateTransitionError struct {
	From State
	To   State
}

// Error implements the error interface.
func (e *StateTransitionError) Error() string {
	return "invalid TDG state transition: " + string(e.From) + " -> " + string(e.To)
}

// RetryExhaustedError provides details when retries are exhausted.
type RetryExhaustedError struct {
	// Phase is the TDG phase where retries were exhausted.
	Phase string

	// Attempts is the number of attempts made.
	Attempts int

	// MaxAttempts is the configured maximum attempts.
	MaxAttempts int

	// LastError is the error from the last attempt.
	LastError error
}

// Error implements the error interface.
func (e *RetryExhaustedError) Error() string {
	return e.Phase + ": max retries exhausted after " + strconv.Itoa(e.Attempts) + " attempts"
}

// Unwrap returns the last error.
func (e *RetryExhaustedError) Unwrap() error {
	return e.LastError
}
