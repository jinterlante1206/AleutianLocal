// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package algorithms provides pure function implementations for the MCTS system.
//
// Architecture:
//
//	Algorithms are pure functions that process immutable snapshots and produce
//	deltas. They are designed for concurrent execution via goroutines.
//
//	┌─────────────────────────────────────────────────────────────────────────────┐
//	│                        ALGORITHM EXECUTION                                   │
//	├─────────────────────────────────────────────────────────────────────────────┤
//	│                                                                              │
//	│   Activity (Orchestrator)                                                    │
//	│      │                                                                       │
//	│      ▼                                                                       │
//	│   ┌─────────────────────────────────────────────────────────────────────┐   │
//	│   │                         Runner                                       │   │
//	│   │   Executes algorithms in goroutines, collects results via channels  │   │
//	│   └───────────────────────┬─────────────────────────────────────────────┘   │
//	│                           │                                                  │
//	│           ┌───────────────┼───────────────┐                                 │
//	│           ▼               ▼               ▼                                 │
//	│   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                          │
//	│   │  Algorithm  │ │  Algorithm  │ │  Algorithm  │   ← Goroutines           │
//	│   │   PN-MCTS   │ │ Transposition│ │  UnitProp   │                          │
//	│   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘                          │
//	│          │               │               │                                   │
//	│          ▼               ▼               ▼                                   │
//	│   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                          │
//	│   │   Result    │ │   Result    │ │   Result    │   ← Channels             │
//	│   │ (out,delta) │ │ (out,delta) │ │ (out,delta) │                          │
//	│   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘                          │
//	│          │               │               │                                   │
//	│          └───────────────┴───────────────┘                                  │
//	│                          │                                                   │
//	│                          ▼                                                   │
//	│                  ┌─────────────┐                                            │
//	│                  │  Composite  │   ← Merged deltas                          │
//	│                  │    Delta    │                                            │
//	│                  └─────────────┘                                            │
//	│                                                                              │
//	└─────────────────────────────────────────────────────────────────────────────┘
//
// Algorithm Contract:
//
//	Algorithms MUST:
//	1. Be pure functions - no side effects, no mutation of inputs
//	2. Check ctx.Done() at regular intervals (every 100ms max)
//	3. Report progress via ReportProgress() to avoid deadlock detection
//	4. Return partial results when cancelled (if SupportsPartialResults)
//	5. Return typed deltas describing state changes
//	6. Implement eval.Evaluable for testing and metrics
//
//	Algorithms MUST NOT:
//	1. Mutate the snapshot or input
//	2. Access global state
//	3. Perform I/O operations
//	4. Ignore cancellation for more than 100ms
//
// Hard/Soft Signal Boundary:
//
//	Algorithms respect the hard/soft signal boundary:
//	- Hard signals (compiler, tests): Can set node status to DISPROVEN
//	- Soft signals (LLM, heuristics): Cannot set DISPROVEN, only guide search
//
// Algorithm Categories:
//
//	┌─────────────────────────────────────────────────────────────────────────────┐
//	│  SEARCH      │ PN-MCTS, Transposition, UnitProp                             │
//	│  LEARNING    │ CDCL, Watched Literals                                       │
//	│  CONSTRAINTS │ TMS, AC-3, Semantic Backprop                                 │
//	│  PLANNING    │ HTN, Blackboard                                              │
//	│  GRAPH       │ Tarjan SCC, Dominators, VF2                                  │
//	│  STREAMING   │ AGM Sketch, Count-Min, HyperLogLog, MinHash, LSH, L0, WL     │
//	└─────────────────────────────────────────────────────────────────────────────┘
//
// Example Usage:
//
//	// Create a runner
//	runner := algorithms.NewRunner(10) // capacity for 10 results
//
//	// Get snapshot
//	snapshot := crs.Snapshot()
//
//	// Run algorithms in parallel
//	runner.Run(ctx, pnmcts, snapshot, &pnmcts.Input{NodeID: "root"})
//	runner.Run(ctx, zobrist, snapshot, &zobrist.Input{})
//
//	// Collect results
//	delta, results, err := runner.Collect(ctx)
//	if err != nil {
//	    return err
//	}
//
//	// Apply merged delta
//	_, err = crs.Apply(ctx, delta)
package algorithms
