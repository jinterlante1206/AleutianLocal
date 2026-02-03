// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package telemetry

import "errors"

// Sentinel errors for the telemetry package.
var (
	// ErrNilContext is returned when a nil context is passed.
	ErrNilContext = errors.New("context must not be nil")

	// ErrUnknownExporter is returned when an unknown exporter type is specified.
	ErrUnknownExporter = errors.New("unknown exporter type")

	// ErrAlreadyInitialized is returned if Init is called more than once.
	ErrAlreadyInitialized = errors.New("telemetry already initialized")
)
