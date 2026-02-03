// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import "errors"

// Sentinel errors for the patterns package.
var (
	// ErrInvalidInput indicates invalid input parameters.
	ErrInvalidInput = errors.New("invalid input")

	// ErrGraphNotReady indicates the graph is not frozen.
	ErrGraphNotReady = errors.New("graph not ready")

	// ErrContextCanceled indicates the context was canceled.
	ErrContextCanceled = errors.New("context canceled")

	// ErrPatternNotFound indicates no patterns were detected.
	ErrPatternNotFound = errors.New("pattern not found")

	// ErrUnsupportedLanguage indicates the language isn't supported.
	ErrUnsupportedLanguage = errors.New("unsupported language")
)
