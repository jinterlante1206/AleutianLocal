// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import "errors"

// Sentinel errors for context assembly.
var (
	// ErrGraphNotInitialized indicates the graph is nil or not frozen.
	ErrGraphNotInitialized = errors.New("graph not initialized or not frozen")

	// ErrEmptyQuery indicates an empty or whitespace-only query.
	ErrEmptyQuery = errors.New("query must not be empty")

	// ErrQueryTooLong indicates the query exceeds the maximum length.
	ErrQueryTooLong = errors.New("query exceeds maximum length")

	// ErrInvalidBudget indicates a non-positive token budget.
	ErrInvalidBudget = errors.New("token budget must be positive")

	// ErrAssemblyTimeout indicates the assembly operation timed out.
	ErrAssemblyTimeout = errors.New("context assembly timed out")
)
