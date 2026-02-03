// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package coordinate

import "errors"

// Sentinel errors for the coordinate package.
var (
	// ErrInvalidInput indicates invalid input parameters.
	ErrInvalidInput = errors.New("invalid input")

	// ErrSymbolNotFound indicates the target symbol was not found.
	ErrSymbolNotFound = errors.New("symbol not found")

	// ErrGraphNotReady indicates the graph is not frozen or not available.
	ErrGraphNotReady = errors.New("graph not ready")

	// ErrContextCanceled indicates the context was canceled.
	ErrContextCanceled = errors.New("context canceled")

	// ErrUnsupportedChangeType indicates the change type is not supported.
	ErrUnsupportedChangeType = errors.New("unsupported change type")

	// ErrPlanNotFound indicates a referenced plan was not found.
	ErrPlanNotFound = errors.New("plan not found")

	// ErrValidationFailed indicates the change plan failed validation.
	ErrValidationFailed = errors.New("validation failed")
)
