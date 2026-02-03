// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// Analyzer implements the safety.TrustValidationAnalyzer interface.
//
// Description:
//
//	Analyzer is the main entry point for trust boundary analysis. It
//	orchestrates zone detection, crossing analysis, and violation reporting.
//	This is Aleutian's unique security differentiator, providing formal
//	trust zone modeling that goes beyond traditional taint tracking.
//
// Thread Safety:
//
//	Analyzer is safe for concurrent use after initialization.
type Analyzer struct {
	graph            *graph.Graph
	idx              *index.SymbolIndex
	zoneDetector     *ZoneDetector
	crossingDetector *CrossingDetector
}

// AnalyzerConfig holds configuration for the Analyzer.
type AnalyzerConfig struct {
	// ShowZones includes full zone details in the output.
	ShowZones bool

	// Limits sets resource constraints.
	Limits safety.ResourceLimits
}

// DefaultAnalyzerConfig returns sensible defaults.
func DefaultAnalyzerConfig() *AnalyzerConfig {
	return &AnalyzerConfig{
		ShowZones: true,
		Limits:    safety.DefaultResourceLimits(),
	}
}

// NewAnalyzer creates a new trust boundary Analyzer.
//
// Description:
//
//	Creates an analyzer that detects trust zones, finds boundary crossings,
//	and reports violations where validation is missing.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*Analyzer - The configured analyzer.
//
// Example:
//
//	analyzer := trust.NewAnalyzer(graph, index)
//	result, err := analyzer.AnalyzeTrustBoundary(ctx, "myapp/handlers")
func NewAnalyzer(g *graph.Graph, idx *index.SymbolIndex) *Analyzer {
	return &Analyzer{
		graph:            g,
		idx:              idx,
		zoneDetector:     NewZoneDetector(),
		crossingDetector: NewCrossingDetector(),
	}
}

// NewAnalyzerWithConfig creates an Analyzer with custom configuration.
//
// Description:
//
//	Creates an analyzer with custom zone patterns and crossing requirements.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//	patterns - Zone detection patterns. If nil, uses defaults.
//	requirements - Crossing requirements. If nil, uses defaults.
//
// Outputs:
//
//	*Analyzer - The configured analyzer.
func NewAnalyzerWithConfig(
	g *graph.Graph,
	idx *index.SymbolIndex,
	patterns *ZonePatterns,
	requirements *CrossingRequirements,
) *Analyzer {
	zd := NewZoneDetector()
	if patterns != nil {
		zd = NewZoneDetectorWithPatterns(patterns)
	}

	cd := NewCrossingDetectorWithConfig(patterns, requirements, nil)

	return &Analyzer{
		graph:            g,
		idx:              idx,
		zoneDetector:     zd,
		crossingDetector: cd,
	}
}

// AnalyzeTrustBoundary analyzes trust zones and boundary crossings.
//
// Description:
//
//	Performs comprehensive trust boundary analysis:
//	1. Detects trust zones based on code structure and naming conventions
//	2. Identifies boundary crossings where data flows between zones
//	3. Checks if proper validation exists at each crossing
//	4. Reports violations where validation is missing
//	5. Generates recommendations for improving security
//
// Inputs:
//
//	ctx - Context for cancellation and timeout. Must not be nil.
//	scope - The scope to analyze (package path or file pattern).
//	opts - Optional configuration (show zones, resource limits, etc.).
//
// Outputs:
//
//	*safety.TrustValidation - The analysis result with zones, crossings, violations.
//	error - Non-nil if scope not found, graph not ready, or operation canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Context is nil.
//	safety.ErrGraphNotReady - Graph is not frozen.
//	safety.ErrContextCanceled - Context was canceled.
//	safety.ErrTimeoutExceeded - Analysis timed out.
//
// Performance:
//
//	Target latency: < 500ms for 10K nodes with 30 second timeout.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (a *Analyzer) AnalyzeTrustBoundary(
	ctx context.Context,
	scope string,
	opts ...safety.BoundaryOption,
) (*safety.TrustBoundary, error) {
	start := time.Now()

	// Validate inputs
	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}
	if !a.graph.IsFrozen() {
		return nil, safety.ErrGraphNotReady
	}

	// Apply options (using defaults since boundaryConfig is unexported)
	// TODO: Add proper option extraction when safety package exports config
	config := DefaultAnalyzerConfig()
	_ = opts // Options currently unused due to unexported boundaryConfig

	// Set up context with timeout
	if config.Limits.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Limits.Timeout)
		defer cancel()
	}

	result := &safety.TrustBoundary{
		Scope:           scope,
		Zones:           nil,
		Crossings:       make([]safety.BoundaryCrossing, 0),
		Violations:      make([]safety.BoundaryViolation, 0),
		Recommendations: make([]string, 0),
		PartialFailures: make([]safety.PartialFailure, 0),
	}

	// Step 1: Detect trust zones
	zones := a.zoneDetector.DetectZones(a.graph, scope)

	// Check for context cancellation after zone detection
	if err := ctx.Err(); err != nil {
		result.PartialFailures = append(result.PartialFailures, safety.PartialFailure{
			Scope:    scope,
			Reason:   "Analysis canceled during zone detection",
			Impact:   "Crossing analysis incomplete",
			Severity: safety.SeverityMedium,
		})
		result.Duration = time.Since(start)
		return result, nil
	}

	if config.ShowZones {
		result.Zones = zones
	}

	// Step 2: Detect boundary crossings
	crossings, err := a.crossingDetector.DetectCrossings(ctx, a.graph, zones, scope)
	if err != nil {
		// Log partial failure but continue
		result.PartialFailures = append(result.PartialFailures, safety.PartialFailure{
			Scope:    scope,
			Reason:   fmt.Sprintf("Crossing detection error: %v", err),
			Impact:   "Some crossings may be missed",
			Severity: safety.SeverityLow,
		})
	}
	result.Crossings = crossings

	// Check context again
	if err := ctx.Err(); err != nil {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Step 3: Find violations (crossings without validation)
	violations := a.crossingDetector.FindViolations(crossings)
	result.Violations = violations

	// Step 4: Generate recommendations
	result.Recommendations = a.generateRecommendations(zones, crossings, violations)

	result.Duration = time.Since(start)
	return result, nil
}

// generateRecommendations creates actionable security recommendations.
func (a *Analyzer) generateRecommendations(
	zones []safety.TrustZone,
	crossings []safety.BoundaryCrossing,
	violations []safety.BoundaryViolation,
) []string {
	var recs []string

	// Count violations by severity
	criticalCount := 0
	highCount := 0
	for _, v := range violations {
		switch v.Severity {
		case safety.SeverityCritical:
			criticalCount++
		case safety.SeverityHigh:
			highCount++
		}
	}

	// Prioritize critical issues
	if criticalCount > 0 {
		recs = append(recs, fmt.Sprintf(
			"CRITICAL: %d crossing(s) from untrusted input directly to privileged code without validation. Fix these immediately.",
			criticalCount,
		))
	}

	// High severity recommendations
	if highCount > 0 {
		recs = append(recs, fmt.Sprintf(
			"HIGH: %d crossing(s) lack proper authorization checks. Add middleware or guards.",
			highCount,
		))
	}

	// Count zones by type
	untrustedZones := 0
	boundaryZones := 0
	for _, z := range zones {
		switch z.Level {
		case safety.TrustExternal:
			untrustedZones++
		case safety.TrustValidation:
			boundaryZones++
		}
	}

	// Zone structure recommendations
	if untrustedZones > 0 && boundaryZones == 0 {
		recs = append(recs, "Consider adding a dedicated validation/middleware layer between handlers and business logic.")
	}

	// General best practices
	if len(violations) > 0 {
		recs = append(recs, "Create a centralized input validation module to reuse across all entry points.")
	}

	// Entry point recommendations
	hasMultipleEntryPoints := false
	for _, z := range zones {
		if z.Level == safety.TrustExternal && len(z.EntryPoints) > 5 {
			hasMultipleEntryPoints = true
			break
		}
	}
	if hasMultipleEntryPoints {
		recs = append(recs, "Multiple entry points detected. Consider using a unified request validation middleware.")
	}

	if len(recs) == 0 {
		recs = append(recs, "No critical trust boundary violations detected. Continue monitoring as code evolves.")
	}

	return recs
}

// GetZoneDetector returns the zone detector for testing or advanced use.
func (a *Analyzer) GetZoneDetector() *ZoneDetector {
	return a.zoneDetector
}

// GetCrossingDetector returns the crossing detector for testing or advanced use.
func (a *Analyzer) GetCrossingDetector() *CrossingDetector {
	return a.crossingDetector
}
