// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package index provides in-memory indexing for code symbols.
//
// The index package contains SymbolIndex, a concurrent-safe data structure
// for fast O(1) lookups of symbols by ID, name, file path, and kind.
//
// # Ownership Model
//
// The index stores pointers to symbols but does NOT own them:
//   - Symbols MUST NOT be mutated after being added to the index
//   - To update symbols: call RemoveByFile() then AddBatch() with new symbols
//   - The index does NOT copy symbols (for memory efficiency)
//
// # Thread Safety
//
// SymbolIndex is safe for concurrent use. Multiple goroutines can call
// any combination of methods simultaneously. Write operations (Add, AddBatch,
// RemoveByFile, Clear) use exclusive locks; read operations (GetBy*, Search,
// Stats) use shared locks.
package index

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for symbol index operations.
var (
	// ErrDuplicateSymbol is returned when adding a symbol with an ID
	// that already exists in the index.
	ErrDuplicateSymbol = errors.New("duplicate symbol ID")

	// ErrMaxSymbolsExceeded is returned when the index has reached
	// its configured maximum capacity.
	ErrMaxSymbolsExceeded = errors.New("maximum symbol count exceeded")

	// ErrInvalidSymbol is returned when a symbol fails validation.
	// The underlying error from Symbol.Validate() is wrapped.
	ErrInvalidSymbol = errors.New("invalid symbol")
)

// BatchError aggregates multiple errors from batch operations.
//
// When AddBatch encounters validation errors or duplicates, it collects
// all errors and returns them together rather than failing on the first error.
// This allows callers to see all problems at once.
//
// BatchError implements the standard errors.Unwrap() interface for Go 1.20+
// multi-error unwrapping.
type BatchError struct {
	// Errors contains all individual errors encountered during the batch operation.
	// Each error includes the index position (e.g., "symbol[0]: duplicate ID").
	Errors []error
}

// Error returns a human-readable summary of the batch errors.
//
// Format depends on error count:
//   - 1 error: returns that error's message directly
//   - 2+ errors: returns count and first error with "and N more" suffix
func (e *BatchError) Error() string {
	if len(e.Errors) == 0 {
		return "batch error with no errors" // Defensive
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d errors: %v (and %d more)",
		len(e.Errors), e.Errors[0], len(e.Errors)-1)
}

// Unwrap returns the underlying errors for use with errors.Is and errors.As.
//
// This implements the Go 1.20+ multi-error unwrapping interface.
func (e *BatchError) Unwrap() []error {
	return e.Errors
}

// ErrorList returns a formatted string with all errors, one per line.
//
// This is useful for logging or displaying the full list of problems
// to a user who needs to fix them all.
func (e *BatchError) ErrorList() string {
	if len(e.Errors) == 0 {
		return ""
	}
	var b strings.Builder
	for i, err := range e.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(err.Error())
	}
	return b.String()
}
