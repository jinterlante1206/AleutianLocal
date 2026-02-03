// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package initializer

import (
	"errors"
	"fmt"
)

// Exit codes for the init command.
const (
	ExitSuccess = 0 // Operation completed successfully
	ExitFailure = 1 // Initialization failed
	ExitBadArgs = 2 // Invalid arguments
)

// Sentinel errors for initialization.
var (
	// Configuration errors
	ErrEmptyProjectRoot   = errors.New("project root must not be empty")
	ErrInvalidMaxWorkers  = errors.New("max workers must be greater than 0")
	ErrInvalidMaxFileSize = errors.New("max file size must be greater than 0")
	ErrPathNotExist       = errors.New("path does not exist")
	ErrPathNotDirectory   = errors.New("path is not a directory")
	ErrPathTraversal      = errors.New("path traversal detected")

	// Lock errors
	ErrLockAcquireFailed = errors.New("failed to acquire lock")
	ErrLockHeld          = errors.New("another init operation is in progress")

	// Storage errors
	ErrIndexCorrupted     = errors.New("index file is corrupted")
	ErrChecksumMismatch   = errors.New("checksum validation failed")
	ErrVersionMismatch    = errors.New("index format version mismatch")
	ErrAtomicSwapFailed   = errors.New("atomic directory swap failed")
	ErrDatabaseOpenFailed = errors.New("failed to open database")

	// Parsing errors
	ErrNoSupportedFiles = errors.New("no supported files found")
	ErrNoLanguages      = errors.New("no supported languages detected")
	ErrParseTimeout     = errors.New("parse operation timed out")

	// Context errors
	ErrContextCancelled = errors.New("operation was cancelled")
)

// ParseFileError represents an error parsing a specific file.
type ParseFileError struct {
	FilePath string
	Err      error
}

// Error implements the error interface.
func (e *ParseFileError) Error() string {
	return fmt.Sprintf("parsing %s: %v", e.FilePath, e.Err)
}

// Unwrap returns the underlying error.
func (e *ParseFileError) Unwrap() error {
	return e.Err
}

// BatchError holds multiple errors from batch operations.
type BatchError struct {
	Errors []error
}

// Error implements the error interface.
func (e *BatchError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d errors occurred; first: %v", len(e.Errors), e.Errors[0])
}

// Add appends an error to the batch.
func (e *BatchError) Add(err error) {
	if err != nil {
		e.Errors = append(e.Errors, err)
	}
}

// HasErrors returns true if there are any errors.
func (e *BatchError) HasErrors() bool {
	return len(e.Errors) > 0
}

// ToError returns nil if no errors, or the BatchError if there are errors.
func (e *BatchError) ToError() error {
	if !e.HasErrors() {
		return nil
	}
	return e
}

// StorageError wraps storage-related errors with context.
type StorageError struct {
	Op  string // Operation that failed
	Err error  // Underlying error
}

// Error implements the error interface.
func (e *StorageError) Error() string {
	return fmt.Sprintf("storage %s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error.
func (e *StorageError) Unwrap() error {
	return e.Err
}
