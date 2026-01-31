// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package cancel provides hierarchical cancellation for the CRS algorithm system.
//
// # Overview
//
// This package implements a first-class cancellation framework that enables graceful
// shutdown of algorithms at any level of the hierarchy: session, activity, or individual
// algorithm. It is designed to prevent hung processes and enable clean resource cleanup.
//
// # Architecture
//
// The cancellation system uses a hierarchical context tree:
//
//	SessionContext (root)
//	├── ActivityContext (Search)
//	│   ├── AlgorithmContext (PN-MCTS)
//	│   ├── AlgorithmContext (Zobrist)
//	│   └── AlgorithmContext (UnitProp)
//	├── ActivityContext (Constraint)
//	│   ├── AlgorithmContext (TMS)
//	│   └── ...
//	└── ...
//
// Cancelling a parent context automatically cancels all children, but children
// can be cancelled independently without affecting siblings or parents.
//
// # Cancellation Triggers
//
// Four types of cancellation are supported:
//
//   - User-initiated: Explicit cancel via API, Ctrl+C, or stop button
//   - Timeout: Algorithm exceeds its configured Timeout() duration
//   - Deadlock: No progress reported for 3x the ProgressInterval
//   - Resource limit: Memory or CPU threshold exceeded
//
// # Graceful Shutdown Protocol
//
// When cancellation is triggered, the following timeline applies:
//
//	T+0ms     Signal cancel (set cancellation flag)
//	T+100ms   Algorithms should detect ctx.Done()
//	T+500ms   Collect partial results from cooperative algorithms
//	T+2000ms  Force kill algorithms that haven't responded
//	T+5000ms  Generate final report and release resources
//
// # Algorithm Contract
//
// Algorithms MUST adhere to the cancellation contract:
//
//   - Check ctx.Done() at least every 100ms
//   - Call ReportProgress() to reset the deadlock timer
//   - Return partial results when cancelled (if supported)
//   - Never block indefinitely without checking cancellation
//
// Example algorithm implementation:
//
//	func (a *MyAlgorithm) Process(ctx context.Context, snapshot CRSSnapshot, input *Input) (*Output, Delta, error) {
//	    for i := 0; i < iterations; i++ {
//	        // Check for cancellation
//	        select {
//	        case <-ctx.Done():
//	            return a.partialResult(), a.partialDelta(), ctx.Err()
//	        default:
//	        }
//
//	        // Report progress to avoid deadlock detection
//	        cancel.ReportProgress(ctx)
//
//	        // Do work...
//	    }
//	    return result, delta, nil
//	}
//
// # Thread Safety
//
// All exported types in this package are safe for concurrent use.
// The CancellationController uses fine-grained locking to minimize contention.
//
// # Metrics
//
// The package exports Prometheus metrics:
//
//   - cancel_total: Counter of cancellations by type, level, and reason
//   - cancel_duration_seconds: Histogram of time from signal to completion
//   - deadlock_detected_total: Counter of deadlock detections by component
//   - resource_limit_exceeded_total: Counter of resource violations
//   - partial_results_collected: Counter of partial results saved
//
// # Usage
//
//	// Create controller
//	ctrl := cancel.NewController(cancel.ControllerConfig{
//	    DefaultTimeout:     30 * time.Second,
//	    DeadlockMultiplier: 3,
//	    GracePeriod:        500 * time.Millisecond,
//	    ForceKillTimeout:   2 * time.Second,
//	})
//
//	// Create session
//	session := ctrl.NewSession(ctx, cancel.SessionConfig{
//	    ID:              "session-123",
//	    ResourceLimits:  cancel.ResourceLimits{MaxMemoryBytes: 1 << 30},
//	})
//
//	// Create activity context
//	activityCtx := session.NewActivity("search")
//
//	// Create algorithm context
//	algoCtx := activityCtx.NewAlgorithm("pnmcts", 5*time.Second)
//
//	// Cancel at any level
//	ctrl.Cancel("pnmcts", cancel.CancelReason{Type: cancel.CancelUser, Message: "User requested"})
//	// or
//	ctrl.Cancel("search", cancel.CancelReason{Type: cancel.CancelTimeout})
//	// or
//	ctrl.CancelAll(cancel.CancelReason{Type: cancel.CancelResourceLimit})
package cancel
