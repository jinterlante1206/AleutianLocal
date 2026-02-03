// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package graph provides graph traversal algorithms for code analysis.
//
// This package implements callers/callees/path queries using BFS traversal
// with support for depth limits, cycle detection, and multiple output formats.
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                      Graph Query Flow                                    │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│                                                                          │
//	│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                  │
//	│  │ Symbol      │───▶│   Resolve   │───▶│   BFS       │                  │
//	│  │ Input       │    │   Symbol    │    │  Traversal  │                  │
//	│  └─────────────┘    └─────────────┘    └─────────────┘                  │
//	│                                               │                          │
//	│                                               ▼                          │
//	│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                  │
//	│  │  Output     │◀───│  Format     │◀───│  Results    │                  │
//	│  │  (tree/json)│    │  Results    │    │  (paths)    │                  │
//	│  └─────────────┘    └─────────────┘    └─────────────┘                  │
//	│                                                                          │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Thread Safety
//
// Query operations are safe for concurrent use with the same MemoryIndex.
package graph
