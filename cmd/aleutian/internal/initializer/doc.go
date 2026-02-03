// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package initializer provides local initialization for Aleutian Trace.
//
// This package implements the core logic for the `aleutian init` command,
// which creates a `.aleutian/` directory containing:
//   - index.db: Symbol index (SQLite)
//   - graph.db: Call graph (SQLite)
//   - manifest.json: File hashes for incremental updates
//   - config.yaml: Project settings
//
// # Thread Safety
//
// The Initializer is safe for concurrent use. File-level locking prevents
// multiple init operations on the same project.
//
// # Atomic Writes
//
// Index writes use the temp-directory-swap pattern to ensure the
// .aleutian/ directory is never in a partial state.
//
// # Buffered Channel Architecture
//
// File parsing uses buffered channels for work distribution:
//
//	┌─────────┐     ┌───────────────────┐     ┌───────────────────┐
//	│ Scanner │────▶│ fileChan (buffer) │────▶│ Worker Pool (N)   │
//	└─────────┘     └───────────────────┘     └───────────────────┘
//	                                                   │
//	                                                   ▼
//	┌─────────┐     ┌───────────────────┐     ┌───────────────────┐
//	│ Writer  │◀────│ resultChan (buf)  │◀────│ Parse Results     │
//	└─────────┘     └───────────────────┘     └───────────────────┘
package initializer
