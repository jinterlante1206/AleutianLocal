// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package regression provides CI/CD regression detection gates.
//
// # Architecture
//
// The regression package implements automated regression detection by comparing
// current performance against established baselines:
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                        REGRESSION FRAMEWORK                              │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│                                                                          │
//	│   Benchmark Results ──► Detector ──► Gate ──► Decision                  │
//	│                            │                     │                       │
//	│                            │                     ├──► PASS: Deploy       │
//	│                            │                     ├──► WARN: Review       │
//	│                            ▼                     └──► FAIL: Block        │
//	│                       ┌─────────┐                                        │
//	│                       │Baseline │                                        │
//	│                       │  Store  │                                        │
//	│                       └─────────┘                                        │
//	│                                                                          │
//	│   Thresholds:                                                            │
//	│   • Latency: P50 +5%, P99 +10%                                          │
//	│   • Throughput: -5%                                                      │
//	│   • Memory: +10%                                                         │
//	│   • Errors: +1%                                                          │
//	│                                                                          │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Components
//
//   - Baseline: Stores historical performance data
//   - Detector: Compares current vs baseline with statistical tests
//   - Gate: Makes pass/warn/fail decisions for CI/CD
//   - Alert: Notifies stakeholders of regressions
//
// # Usage
//
// Basic regression gate:
//
//	baseline := regression.NewFileBaseline("./baselines")
//	gate := regression.NewGate(baseline,
//	    regression.WithLatencyThreshold(0.10),  // 10% increase allowed
//	    regression.WithThroughputThreshold(0.05),  // 5% decrease allowed
//	)
//
//	// Check benchmark results
//	decision, err := gate.Check(ctx, benchResults)
//	if !decision.Pass {
//	    log.Fatalf("Regression detected: %s", decision.Report)
//	}
//
// # CI/CD Integration
//
// The gate is designed for CI/CD pipelines:
//
//	# GitHub Actions example
//	- name: Run benchmarks
//	  run: go test -bench=. -benchmem ./...
//
//	- name: Check regression
//	  run: regression-gate check --baseline ./baselines
//
// # Thread Safety
//
// All types in this package are safe for concurrent use unless otherwise noted.
package regression
