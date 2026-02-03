// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package indexes provides the 6 index implementations for CRS.
//
// Architecture:
//
//	CRS maintains 6 specialized indexes for different aspects of code reasoning:
//
//	┌─────────────────────────────────────────────────────────────────────────────┐
//	│                           CRS INDEXES                                        │
//	├─────────────────────────────────────────────────────────────────────────────┤
//	│                                                                              │
//	│  ┌─────────────┐  Tracks proof/disproof numbers for PN-MCTS.                │
//	│  │   PROOF     │  Each node has a proof number (cost to prove) and          │
//	│  │   INDEX     │  disproof number (cost to disprove). Used for best-first   │
//	│  └─────────────┘  tree traversal in solution search.                        │
//	│                                                                              │
//	│  ┌─────────────┐  Stores constraints on the search space.                   │
//	│  │ CONSTRAINT  │  Types: MutualExclusion, Implication, Ordering, Resource.  │
//	│  │   INDEX     │  Used by AC-3, TMS for constraint propagation.             │
//	│  └─────────────┘                                                            │
//	│                                                                              │
//	│  ┌─────────────┐  Stores similarity distances between nodes.                │
//	│  │ SIMILARITY  │  Used by MinHash, LSH for finding similar code patterns.   │
//	│  │   INDEX     │  Enables pattern transfer across similar contexts.         │
//	│  └─────────────┘                                                            │
//	│                                                                              │
//	│  ┌─────────────┐  Tracks dependencies between nodes.                        │
//	│  │ DEPENDENCY  │  Forward: what this node depends on.                       │
//	│  │   INDEX     │  Reverse: what depends on this node.                       │
//	│  └─────────────┘  Used for impact analysis and ordering.                    │
//	│                                                                              │
//	│  ┌─────────────┐  Records decision history for learning.                    │
//	│  │  HISTORY    │  Each entry: nodeID, action, result, source, timestamp.    │
//	│  │   INDEX     │  Used by CDCL for clause learning from failures.           │
//	│  └─────────────┘                                                            │
//	│                                                                              │
//	│  ┌─────────────┐  Provides streaming statistics.                            │
//	│  │ STREAMING   │  Frequency estimation (Count-Min sketch logic).            │
//	│  │   INDEX     │  Cardinality estimation (HyperLogLog logic).               │
//	│  └─────────────┘  Space-efficient approximate statistics.                   │
//	│                                                                              │
//	└─────────────────────────────────────────────────────────────────────────────┘
//
// Index Design Principles:
//
//  1. Read-Only Views: Snapshots expose read-only IndexView interfaces.
//     The underlying data is immutable within a snapshot.
//
//  2. Delta-Based Updates: All mutations happen via Delta objects that
//     are validated and applied atomically by CRS.
//
//  3. Thread Safety: Index implementations are designed for concurrent
//     reads. Mutations are serialized by CRS's write lock.
//
//  4. Evaluable: Each index implements eval.Evaluable for property-based
//     testing and metrics collection.
//
// Hard/Soft Signal Boundary:
//
//	CRITICAL: The ProofIndex enforces the hard/soft signal boundary.
//	- PROVEN status can only be set by hard signals (compiler, tests)
//	- DISPROVEN status can only be set by hard signals
//	- Soft signals (LLM) can update proof/disproof NUMBERS but not change STATUS to DISPROVEN
//
// Example Usage:
//
//	// Get a snapshot (immutable view)
//	snapshot := crs.Snapshot()
//
//	// Query the proof index
//	proofView := snapshot.ProofIndex()
//	proof, exists := proofView.Get("node-123")
//	if exists && proof.Status == crs.ProofStatusProven {
//	    // Node is proven, can be pruned from search
//	}
//
//	// Query cross-index relationships
//	depView := snapshot.DependencyIndex()
//	dependents := depView.DependedBy("node-123")
//	for _, dep := range dependents {
//	    // These nodes depend on node-123
//	}
package indexes
