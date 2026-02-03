// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package risk provides aggregated risk assessment for code changes.
//
// The risk package combines multiple signals (impact analysis, policy
// violations, and complexity metrics) into a single risk level that
// can be used for CI/CD gating and code review prioritization.
//
// # Architecture
//
// The risk assessment follows a three-signal model:
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                    Risk Assessment Pipeline                     │
//	├─────────────────────────────────────────────────────────────────┤
//	│                                                                  │
//	│  Git Changes (Diff/Staged/Commit/Branch)                        │
//	│         │                                                        │
//	│         ▼                                                        │
//	│  ┌──────────────┬──────────────┬──────────────┐                │
//	│  │   Impact     │   Policy     │  Complexity  │                │
//	│  │   Signal     │   Signal     │   Signal     │                │
//	│  │  (weight 0.5)│  (weight 0.3)│  (weight 0.2)│                │
//	│  └──────────────┴──────────────┴──────────────┘                │
//	│         │              │              │                          │
//	│         └──────────────┼──────────────┘                          │
//	│                        ▼                                         │
//	│               ┌──────────────┐                                  │
//	│               │  Aggregator  │                                  │
//	│               └──────────────┘                                  │
//	│                        │                                         │
//	│                        ▼                                         │
//	│               Risk Level (LOW/MEDIUM/HIGH/CRITICAL)             │
//	│                                                                  │
//	└─────────────────────────────────────────────────────────────────┘
//
// # Signals
//
// Impact Signal:
//   - Blast radius (affected symbols)
//   - Security-sensitive paths
//   - Public API exposure
//   - Database/IO operations
//
// Policy Signal:
//   - Secret detection violations
//   - PII exposure
//   - Credential leaks
//
// Complexity Signal:
//   - Lines added/removed
//   - Files changed
//   - Cyclomatic complexity delta
//
// # Thread Safety
//
// All exported types in this package are safe for concurrent use.
// Signal collection runs in parallel using goroutines with proper
// synchronization.
//
// # Algorithm Versioning
//
// The aggregation algorithm is versioned to ensure reproducibility.
// When making changes that affect risk calculations, increment the
// RiskAlgorithmVersion constant.
package risk
