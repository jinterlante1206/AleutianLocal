// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package chaos provides fault injection for resilience testing.
//
// # Architecture
//
// The chaos package enables controlled fault injection to verify system
// resilience under failure conditions:
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                           CHAOS FRAMEWORK                                │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│                                                                          │
//	│   ┌──────────────┐     ┌──────────────┐     ┌──────────────┐           │
//	│   │   Injector   │────►│  Scheduler   │────►│   Recovery   │           │
//	│   │              │     │              │     │   Verifier   │           │
//	│   └──────┬───────┘     └──────────────┘     └──────────────┘           │
//	│          │                                                               │
//	│          │   Fault Types:                                               │
//	│          │                                                               │
//	│          ├──► LatencyFault: Inject delays                               │
//	│          ├──► ErrorFault: Return errors                                 │
//	│          ├──► PanicFault: Trigger panics                                │
//	│          ├──► ResourceFault: Limit CPU/memory                           │
//	│          └──► TimeoutFault: Force timeouts                              │
//	│                                                                          │
//	│   Scheduling Strategies:                                                 │
//	│                                                                          │
//	│   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                    │
//	│   │   Random    │  │   Periodic  │  │  Scenario   │                    │
//	│   │             │  │             │  │   Based     │                    │
//	│   └─────────────┘  └─────────────┘  └─────────────┘                    │
//	│                                                                          │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Components
//
//   - Injector: Coordinates fault injection and recovery
//   - Fault: Individual fault types (latency, error, panic, resource)
//   - Scheduler: Controls when faults are injected
//   - Recovery: Verifies system recovers correctly after faults
//
// # Usage
//
// Basic chaos testing:
//
//	injector := chaos.NewInjector(
//	    chaos.WithFaults(
//	        chaos.NewLatencyFault(100*time.Millisecond, 500*time.Millisecond),
//	        chaos.NewErrorFault(0.1, errors.New("chaos error")),
//	    ),
//	    chaos.WithScheduler(chaos.RandomScheduler(0.05)),
//	)
//
//	// Run chaos test
//	result, err := injector.Run(ctx, target, 10*time.Minute)
//
// # Safety
//
// Chaos testing can cause system instability. The framework includes:
//   - Automatic fault reversion after duration
//   - Kill switch for emergency shutdown
//   - Health check verification before and after
//   - Maximum concurrent fault limits
//
// # Thread Safety
//
// All types in this package are safe for concurrent use unless otherwise noted.
package chaos
