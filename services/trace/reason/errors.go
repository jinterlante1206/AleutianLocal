// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import "errors"

// Common errors for the reason package.
var (
	// ErrInvalidInput is returned when input parameters are invalid.
	ErrInvalidInput = errors.New("invalid input")

	// ErrSymbolNotFound is returned when a target symbol cannot be found.
	ErrSymbolNotFound = errors.New("symbol not found")

	// ErrContextCanceled is returned when the context is canceled.
	ErrContextCanceled = errors.New("context canceled")

	// ErrGraphNotReady is returned when the graph is not frozen.
	ErrGraphNotReady = errors.New("graph not ready: must be frozen")

	// ErrUnsupportedLanguage is returned when the language is not supported.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrParseFailure is returned when signature parsing fails.
	ErrParseFailure = errors.New("failed to parse signature")

	// ErrTypeNotFound is returned when a type cannot be found.
	ErrTypeNotFound = errors.New("type not found")
)
