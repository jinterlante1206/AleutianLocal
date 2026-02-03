// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"errors"
	"fmt"
)

// Exit codes for graph commands.
const (
	ExitSuccess = 0 // Query successful (even if no results)
	ExitError   = 1 // Error (no index, invalid symbol, etc.)
	ExitBadArgs = 2 // Invalid arguments
)

// Sentinel errors for graph queries.
var (
	// Index errors
	ErrIndexNotFound = errors.New("index not found: run 'aleutian init' first")
	ErrIndexStale    = errors.New("index is stale: consider running 'aleutian init'")

	// Symbol errors
	ErrSymbolNotFound  = errors.New("symbol not found")
	ErrSymbolAmbiguous = errors.New("symbol is ambiguous")

	// Query errors
	ErrNoResults     = errors.New("no results found")
	ErrDepthExceeded = errors.New("maximum depth exceeded")
	ErrTimeout       = errors.New("query timed out")
)

// SymbolNotFoundError provides details about a missing symbol.
type SymbolNotFoundError struct {
	Input       string
	Suggestions []string
}

// Error implements the error interface.
func (e *SymbolNotFoundError) Error() string {
	if len(e.Suggestions) > 0 {
		return fmt.Sprintf("symbol %q not found; did you mean: %v", e.Input, e.Suggestions)
	}
	return fmt.Sprintf("symbol %q not found", e.Input)
}

// Unwrap returns the sentinel error.
func (e *SymbolNotFoundError) Unwrap() error {
	return ErrSymbolNotFound
}

// AmbiguousSymbolError provides details about ambiguous symbol matches.
type AmbiguousSymbolError struct {
	Input   string
	Matches []SymbolMatch
}

// SymbolMatch represents a potential symbol match.
type SymbolMatch struct {
	ID       string
	Name     string
	FilePath string
	Line     int
}

// Error implements the error interface.
func (e *AmbiguousSymbolError) Error() string {
	return fmt.Sprintf("symbol %q is ambiguous (%d matches); use --exact or provide full path",
		e.Input, len(e.Matches))
}

// Unwrap returns the sentinel error.
func (e *AmbiguousSymbolError) Unwrap() error {
	return ErrSymbolAmbiguous
}
