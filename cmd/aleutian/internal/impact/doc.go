// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package impact provides change impact analysis for code changes.
//
// # Overview
//
// The impact package analyzes code changes to determine their blast radius
// and risk level. It integrates with git to detect changes and uses the
// code graph to compute affected symbols.
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────────┐
//	│                       Impact Analysis Flow                           │
//	├─────────────────────────────────────────────────────────────────────┤
//	│                                                                      │
//	│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐        │
//	│  │  Git Change  │────▶│   Symbol     │────▶│    Blast     │        │
//	│  │  Detection   │     │   Mapping    │     │    Radius    │        │
//	│  └──────────────┘     └──────────────┘     └──────────────┘        │
//	│                                                   │                 │
//	│                                                   ▼                 │
//	│                                           ┌──────────────┐          │
//	│                                           │     Risk     │          │
//	│                                           │  Assessment  │          │
//	│                                           └──────────────┘          │
//	│                                                   │                 │
//	│                                                   ▼                 │
//	│                                           ┌──────────────┐          │
//	│                                           │    Impact    │          │
//	│                                           │    Report    │          │
//	│                                           └──────────────┘          │
//	└─────────────────────────────────────────────────────────────────────┘
//
// # Usage
//
//	analyzer := impact.NewAnalyzer(index)
//	result, err := analyzer.Analyze(ctx, cfg)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Risk: %s, Affected: %d symbols\n", result.RiskLevel, result.TotalAffected)
//
// # Thread Safety
//
// Analyzer is safe for concurrent use. Multiple analyses can run in parallel.
package impact
