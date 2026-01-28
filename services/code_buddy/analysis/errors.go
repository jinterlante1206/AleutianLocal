// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import "errors"

// Analysis errors.
var (
	// ErrNilContext is returned when a nil context is passed to an analysis function.
	ErrNilContext = errors.New("context must not be nil")

	// ErrSymbolNotFound is returned when a symbol cannot be found in the graph.
	ErrSymbolNotFound = errors.New("symbol not found in graph")

	// ErrGraphNotReady is returned when the graph is not in a frozen state.
	ErrGraphNotReady = errors.New("graph is not frozen")

	// ErrAnalysisTimeout is returned when analysis exceeds the time limit.
	ErrAnalysisTimeout = errors.New("analysis timeout")

	// ErrInvalidSymbolID is returned when a symbol ID is invalid.
	ErrInvalidSymbolID = errors.New("invalid symbol ID")
)
