// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package crs provides the Code Reasoning State (CRS) - the central mutable
// state container for the Aleutian Hybrid MCTS system.
//
// # Architecture Overview
//
// CRS sits between the application layer (activities) and the algorithm layer,
// providing immutable snapshots for reading and delta-based mutations for writing.
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                         APPLICATION LAYER                                │
//	│                    (Activities: Search, Constraint, etc.)               │
//	└────────────────────────────────┬────────────────────────────────────────┘
//	                                 │
//	                                 │ 1. Snapshot() - get immutable view
//	                                 │ 2. Apply(delta) - atomic mutation
//	                                 ▼
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                         CRS (Code Reasoning State)                       │
//	│                                                                          │
//	│  ┌────────────────────────────────────────────────────────────────────┐ │
//	│  │                       Snapshot (Immutable)                         │ │
//	│  │  ┌─────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐        │ │
//	│  │  │  Proof  │ │ Constraint │ │ Similarity │ │ Dependency │        │ │
//	│  │  │  Index  │ │   Index    │ │   Index    │ │   Index    │        │ │
//	│  │  └─────────┘ └────────────┘ └────────────┘ └────────────┘        │ │
//	│  │  ┌─────────┐ ┌────────────┐                                       │ │
//	│  │  │ History │ │ Streaming  │                                       │ │
//	│  │  │  Index  │ │   Index    │                                       │ │
//	│  │  └─────────┘ └────────────┘                                       │ │
//	│  └────────────────────────────────────────────────────────────────────┘ │
//	│                                                                          │
//	│  Key Operations:                                                         │
//	│  • Snapshot() → CRSSnapshot (copy-on-write, immutable)                  │
//	│  • Apply(delta) → validates, updates indexes, increments generation     │
//	│  • Query() → cross-index query API                                      │
//	│  • Generation() → current state version                                 │
//	└────────────────────────────────┬────────────────────────────────────────┘
//	                                 │
//	                                 │ Algorithms produce deltas
//	                                 ▼
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                         ALGORITHM LAYER                                  │
//	│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐          │
//	│  │ PN-MCTS │ │  CDCL   │ │   TMS   │ │   HTN   │ │ MinHash │  ...     │
//	│  └─────────┘ └─────────┘ └─────────┘ └─────────┘ └─────────┘          │
//	│                                                                          │
//	│  Pure Functions: Process(ctx, snapshot, input) → (output, delta, error) │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Core Concepts
//
// ## Snapshot
//
// A snapshot is an immutable view of CRS at a point in time. Algorithms read
// from snapshots, never from CRS directly. This ensures:
//
//   - Thread safety: multiple algorithms can read the same snapshot concurrently
//   - Consistency: all reads within an algorithm see the same state
//   - Performance: copy-on-write semantics make snapshots cheap to create
//
// ## Delta
//
// A delta represents a change to CRS state. Algorithms produce deltas as output,
// which activities then merge and apply to CRS. Deltas support:
//
//   - Validation: deltas are validated before application
//   - Merging: multiple deltas can be combined
//   - Conflict detection: overlapping changes are detected
//   - Atomicity: either all changes apply or none do
//
// ## Generation
//
// Every Apply() increments the generation counter. This enables:
//
//   - Cache invalidation: detect when cached data is stale
//   - Ordering: determine which state is newer
//   - Debugging: trace state evolution
//
// # Thread Safety
//
// CRS uses a single RWMutex for thread safety:
//
//   - Snapshot() acquires read lock briefly, returns immutable snapshot
//   - Apply() acquires write lock, validates and applies delta
//   - Multiple readers can hold snapshots concurrently
//   - Writer blocks until all readers release
//
// All CRS methods accept context.Context and respect cancellation.
//
// # Observability
//
// CRS implements the eval.Evaluable interface, exposing:
//
//   - Properties: snapshot_immutability, delta_idempotence, generation_monotonic
//   - Metrics: crs_snapshot_duration, crs_apply_duration, crs_generation
//   - Health checks: index connectivity, memory bounds
//
// # Hard/Soft Signal Boundary
//
// CRS enforces the hard/soft signal boundary from CB-28C:
//
//   - Hard signals (compiler errors, test results): can mark nodes DISPROVEN
//   - Soft signals (LLM feedback): cannot mark nodes DISPROVEN
//
// Delta validation rejects any delta that violates this boundary.
//
// # Usage Example
//
//	// Create CRS
//	crs := crs.New(crs.DefaultConfig())
//
//	// Get snapshot for algorithm
//	snapshot := crs.Snapshot()
//
//	// Algorithm processes snapshot
//	output, delta, err := algorithm.Process(ctx, snapshot, input)
//	if err != nil {
//	    return err
//	}
//
//	// Apply delta to CRS
//	metrics, err := crs.Apply(ctx, delta)
//	if err != nil {
//	    return fmt.Errorf("apply delta: %w", err)
//	}
//
//	// Check new generation
//	fmt.Printf("New generation: %d\n", crs.Generation())
package crs
