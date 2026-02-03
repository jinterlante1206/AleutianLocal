// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import "errors"

// Sentinel errors for node operations.
var (
	// ErrMissingInput is returned when a required input is not provided.
	ErrMissingInput = errors.New("missing required input")

	// ErrInvalidInputType is returned when input has wrong type.
	ErrInvalidInputType = errors.New("invalid input type")

	// ErrNoFilesToProcess is returned when no files match the criteria.
	ErrNoFilesToProcess = errors.New("no files to process")

	// ErrParserNotFound is returned when no parser exists for a file type.
	ErrParserNotFound = errors.New("parser not found for file type")

	// ErrBuildFailed is returned when graph building fails.
	ErrBuildFailed = errors.New("graph build failed")

	// ErrLSPUnavailable is returned when LSP server cannot be spawned.
	ErrLSPUnavailable = errors.New("LSP server unavailable")

	// ErrLintFailed is returned when linting fails.
	ErrLintFailed = errors.New("lint failed")

	// ErrGateBlocked is returned when a gate condition blocks execution.
	ErrGateBlocked = errors.New("gate condition not met")

	// ErrNilDependency is returned when a required dependency is nil.
	ErrNilDependency = errors.New("required dependency is nil")

	// ErrGraphNotFrozen is returned when a graph operation requires frozen graph.
	ErrGraphNotFrozen = errors.New("graph must be frozen")

	// ErrCacheNotReady is returned when cache is not initialized.
	ErrCacheNotReady = errors.New("cache not ready")
)
