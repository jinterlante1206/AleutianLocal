// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"errors"
	"fmt"
)

// Sentinel errors for the lint package.
var (
	// ErrLinterNotInstalled indicates the linter binary was not found in PATH.
	ErrLinterNotInstalled = errors.New("linter not installed")

	// ErrLinterTimeout indicates the linter exceeded its configured timeout.
	ErrLinterTimeout = errors.New("linter timeout")

	// ErrLinterFailed indicates the linter process failed to execute.
	ErrLinterFailed = errors.New("linter execution failed")

	// ErrUnsupportedLanguage indicates no linter configuration exists for the language.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrParseOutput indicates failure to parse the linter's JSON output.
	ErrParseOutput = errors.New("failed to parse linter output")

	// ErrInvalidInput indicates invalid input to a lint function.
	ErrInvalidInput = errors.New("invalid input")
)

// LinterError wraps errors from a specific linter with context.
//
// Thread Safety: Immutable after creation.
type LinterError struct {
	// Linter is the name of the linter that failed (e.g., "golangci-lint").
	Linter string

	// Language is the language being linted (e.g., "go").
	Language string

	// Err is the underlying error.
	Err error

	// Output contains any stderr output from the linter.
	Output string
}

// Error implements the error interface.
func (e *LinterError) Error() string {
	if e.Output != "" {
		return fmt.Sprintf("%s (%s): %v: %s", e.Linter, e.Language, e.Err, e.Output)
	}
	return fmt.Sprintf("%s (%s): %v", e.Linter, e.Language, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *LinterError) Unwrap() error {
	return e.Err
}

// NewLinterError creates a new LinterError.
//
// Description:
//
//	Creates an error with context about which linter failed.
//
// Inputs:
//
//	linter - Name of the linter (e.g., "golangci-lint")
//	language - Language being linted (e.g., "go")
//	err - The underlying error
//
// Outputs:
//
//	*LinterError - The wrapped error
func NewLinterError(linter, language string, err error) *LinterError {
	return &LinterError{
		Linter:   linter,
		Language: language,
		Err:      err,
	}
}

// WithOutput adds stderr output to the error.
//
// Description:
//
//	Returns a copy of the error with the output field set.
//	Useful for capturing linter stderr for debugging.
//
// Inputs:
//
//	output - The stderr output from the linter
//
// Outputs:
//
//	*LinterError - A new error with output set
func (e *LinterError) WithOutput(output string) *LinterError {
	return &LinterError{
		Linter:   e.Linter,
		Language: e.Language,
		Err:      e.Err,
		Output:   output,
	}
}
