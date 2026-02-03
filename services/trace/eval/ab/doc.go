// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package ab provides A/B testing harness for comparing algorithm implementations.
//
// # Architecture
//
// The ab package enables controlled experiments comparing two algorithm
// implementations with statistical rigor:
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│                           A/B HARNESS                                    │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│                                                                          │
//	│   Request ──► Sampler ──► ┬─► Control Algorithm ──────┐                 │
//	│                           │                            ├──► Comparator  │
//	│                           └─► Experiment Algorithm ───┘       │         │
//	│                                                                │         │
//	│   ┌────────────────────────────────────────────────────────────┘         │
//	│   │                                                                      │
//	│   ▼                                                                      │
//	│   Statistics ──► Decision Engine ──► Recommendation                     │
//	│   • t-test                          • KEEP_CONTROL                      │
//	│   • Cohen's d                       • SWITCH_TO_EXPERIMENT              │
//	│   • Confidence intervals            • NEED_MORE_DATA                    │
//	│   • Power analysis                                                      │
//	│                                                                          │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Components
//
//   - Harness: Main coordinator that runs both algorithms and collects data
//   - Sampler: Controls which requests go to experiment vs control
//   - Statistics: Statistical analysis (Welch's t-test, confidence intervals)
//   - Decision: Automated winner selection with configurable thresholds
//
// # Usage
//
// Basic A/B test setup:
//
//	harness := ab.NewHarness(controlAlgo, experimentAlgo,
//	    ab.WithSampleRate(0.1),        // 10% of traffic to experiment
//	    ab.WithMinSamples(1000),       // Minimum samples before decision
//	    ab.WithConfidenceLevel(0.95),  // 95% confidence required
//	)
//
//	// Run comparison
//	result, err := harness.Compare(ctx, snapshot, input)
//
//	// Get statistical analysis
//	analysis := harness.GetResults()
//	if analysis.Recommendation == ab.SwitchToExperiment {
//	    // Experiment is significantly better
//	}
//
// # Statistical Methodology
//
// The package uses established statistical methods:
//
//   - Welch's t-test for comparing means (handles unequal variances)
//   - Cohen's d for effect size measurement
//   - Bootstrap confidence intervals for robustness
//   - Power analysis to determine required sample sizes
//
// # Thread Safety
//
// All types in this package are safe for concurrent use unless otherwise noted.
//
// # Integration
//
// The ab package integrates with the eval framework:
//
//   - Uses benchmark.Result for performance measurements
//   - Implements Evaluable for self-testing
//   - Exports metrics to telemetry sinks
package ab
