// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ast

import (
	"errors"
	"fmt"
)

// Sentinel errors for common parse failure conditions.
//
// These errors can be checked using errors.Is() to determine the
// category of failure without inspecting error messages.
var (
	// ErrUnsupportedLanguage indicates that no parser is available for the
	// requested language or file extension.
	//
	// Example:
	//   parser, ok := registry.GetByExtension(".xyz")
	//   if !ok {
	//       return fmt.Errorf("file type .xyz: %w", ErrUnsupportedLanguage)
	//   }
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrParseFailed indicates that parsing failed completely and no
	// useful result could be produced.
	//
	// This is different from partial parse failures, which are reported
	// in ParseResult.Errors while still returning extracted symbols.
	//
	// Common causes:
	//   - Invalid UTF-8 encoding
	//   - Corrupted file content
	//   - Parser internal error
	ErrParseFailed = errors.New("parse failed")

	// ErrInvalidContent indicates that the provided content is invalid
	// and cannot be processed.
	//
	// Common causes:
	//   - Nil content slice
	//   - Non-UTF-8 encoding
	//   - Binary file content
	ErrInvalidContent = errors.New("invalid content")

	// ErrContextCanceled indicates that parsing was canceled via context.
	//
	// This wraps context.Canceled but provides a parse-specific error
	// that can be distinguished from other context cancellations.
	ErrContextCanceled = errors.New("parse canceled")

	// ErrTimeout indicates that parsing exceeded the allowed time limit.
	//
	// Large files or complex syntax may trigger this error.
	// Consider increasing the context timeout or splitting the file.
	ErrTimeout = errors.New("parse timeout")
)

// ParseError provides detailed information about a parse failure.
//
// ParseError wraps an underlying error with additional context about
// where the error occurred in the source file. It implements the
// error interface and can be unwrapped to access the underlying cause.
//
// Example:
//
//	result, err := parser.Parse(ctx, content, "main.go")
//	if err != nil {
//	    var parseErr *ParseError
//	    if errors.As(err, &parseErr) {
//	        fmt.Printf("Error at %s:%d:%d: %s\n",
//	            parseErr.FilePath, parseErr.Line, parseErr.Column, parseErr.Message)
//	    }
//	}
type ParseError struct {
	// FilePath is the path to the file where the error occurred.
	FilePath string

	// Line is the 1-indexed line number where the error occurred.
	// May be 0 if the error is not associated with a specific line.
	Line int

	// Column is the 0-indexed column where the error occurred.
	// May be 0 if the error is not associated with a specific column.
	Column int

	// Message describes the error in human-readable form.
	Message string

	// Cause is the underlying error that triggered this parse error.
	// May be nil if this is a primary error.
	Cause error
}

// Error returns a formatted error message including file location.
//
// Format depends on available location information:
//   - With line and column: "file.go:10:5: unexpected token"
//   - With line only:       "file.go:10: unexpected token"
//   - Without location:     "file.go: unexpected token"
func (e *ParseError) Error() string {
	if e.Line > 0 && e.Column > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.FilePath, e.Line, e.Column, e.Message)
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.FilePath, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.FilePath, e.Message)
}

// Unwrap returns the underlying cause error.
//
// This enables use with errors.Is() and errors.As() to check
// or extract the underlying error.
func (e *ParseError) Unwrap() error {
	return e.Cause
}

// NewParseError creates a new ParseError with the given details.
//
// Parameters:
//   - filePath: Path to the file where the error occurred.
//   - line: 1-indexed line number (0 if unknown).
//   - column: 0-indexed column number (0 if unknown).
//   - message: Human-readable error description.
//
// Returns:
//
//	A new ParseError instance.
func NewParseError(filePath string, line, column int, message string) *ParseError {
	return &ParseError{
		FilePath: filePath,
		Line:     line,
		Column:   column,
		Message:  message,
	}
}

// NewParseErrorWithCause creates a new ParseError wrapping an underlying error.
//
// Parameters:
//   - filePath: Path to the file where the error occurred.
//   - line: 1-indexed line number (0 if unknown).
//   - column: 0-indexed column number (0 if unknown).
//   - message: Human-readable error description.
//   - cause: The underlying error that triggered this failure.
//
// Returns:
//
//	A new ParseError instance wrapping the cause.
func NewParseErrorWithCause(filePath string, line, column int, message string, cause error) *ParseError {
	return &ParseError{
		FilePath: filePath,
		Line:     line,
		Column:   column,
		Message:  message,
		Cause:    cause,
	}
}

// WrapParseError wraps an error with file context.
//
// If the error is already a ParseError, it returns it unchanged.
// Otherwise, it creates a new ParseError wrapping the original error.
//
// Parameters:
//   - err: The error to wrap.
//   - filePath: Path to the file where the error occurred.
//
// Returns:
//
//	A ParseError wrapping the original error, or nil if err is nil.
func WrapParseError(err error, filePath string) error {
	if err == nil {
		return nil
	}

	// Don't double-wrap ParseErrors
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return err
	}

	return &ParseError{
		FilePath: filePath,
		Message:  err.Error(),
		Cause:    err,
	}
}

// IsParseError checks if an error is or wraps a ParseError.
//
// Parameters:
//   - err: The error to check.
//
// Returns:
//
//	True if the error is or contains a ParseError.
func IsParseError(err error) bool {
	var parseErr *ParseError
	return errors.As(err, &parseErr)
}

// IsUnsupportedLanguage checks if an error indicates an unsupported language.
//
// Parameters:
//   - err: The error to check.
//
// Returns:
//
//	True if the error is or wraps ErrUnsupportedLanguage.
func IsUnsupportedLanguage(err error) bool {
	return errors.Is(err, ErrUnsupportedLanguage)
}

// IsParseFailed checks if an error indicates a complete parse failure.
//
// Parameters:
//   - err: The error to check.
//
// Returns:
//
//	True if the error is or wraps ErrParseFailed.
func IsParseFailed(err error) bool {
	return errors.Is(err, ErrParseFailed)
}
