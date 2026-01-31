// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/patterns"
)

// PatternScanNode detects design patterns in code.
//
// Description:
//
//	Uses the patterns.PatternDetector to find design patterns in the
//	code graph. Can detect singleton, factory, builder, and other
//	common patterns.
//
// Inputs (from map[string]any):
//
//	"graph" (*graph.Graph): The code graph to scan. Required.
//	"index" (*index.SymbolIndex): Symbol index for lookups. Required.
//	"scope" (string): Package/file prefix to scan. Optional, default "" (all).
//	"patterns" ([]string): Specific patterns to detect. Optional.
//
// Outputs:
//
//	*PatternScanOutput containing:
//	  - Patterns: Detected design patterns
//	  - Summary: Pattern detection summary
//	  - Duration: Scan time
//
// Thread Safety:
//
//	Safe for concurrent use.
type PatternScanNode struct {
	dag.BaseNode
	minConfidence       float64
	includeNonIdiomatic bool
}

// PatternScanOutput contains the result of pattern detection.
type PatternScanOutput struct {
	// Patterns contains detected design patterns.
	Patterns []patterns.DetectedPattern

	// Summary is a human-readable summary.
	Summary string

	// PatternCounts maps pattern types to counts.
	PatternCounts map[string]int

	// IdiomaticCount is the number of idiomatic patterns.
	IdiomaticCount int

	// Duration is the scan time.
	Duration time.Duration
}

// NewPatternScanNode creates a new pattern scan node.
//
// Inputs:
//
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*PatternScanNode - The configured node.
func NewPatternScanNode(deps []string) *PatternScanNode {
	return &PatternScanNode{
		BaseNode: dag.BaseNode{
			NodeName:         "PATTERN_SCAN",
			NodeDependencies: deps,
			NodeTimeout:      2 * time.Minute,
			NodeRetryable:    false,
		},
		minConfidence:       0.0,
		includeNonIdiomatic: true,
	}
}

// WithMinConfidence sets the minimum confidence threshold.
func (n *PatternScanNode) WithMinConfidence(conf float64) *PatternScanNode {
	n.minConfidence = conf
	return n
}

// WithIdiomaticOnly excludes non-idiomatic patterns.
func (n *PatternScanNode) WithIdiomaticOnly(only bool) *PatternScanNode {
	n.includeNonIdiomatic = !only
	return n
}

// Execute scans for design patterns in the code graph.
//
// Description:
//
//	Creates a PatternDetector and scans the graph for patterns.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "graph", "index", and optionally "scope".
//
// Outputs:
//
//	*PatternScanOutput - The scan result.
//	error - Non-nil if required inputs are missing.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *PatternScanNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	// Extract inputs
	g, idx, scope, patternTypes, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	if !g.IsFrozen() {
		return nil, ErrGraphNotFrozen
	}

	start := time.Now()

	// Create detector
	detector := patterns.NewPatternDetector(g, idx)

	// Configure detection options
	opts := &patterns.DetectionOptions{
		MinConfidence:       n.minConfidence,
		IncludeNonIdiomatic: n.includeNonIdiomatic,
	}

	// Add pattern type filter if specified
	if len(patternTypes) > 0 {
		opts.Patterns = make([]patterns.PatternType, len(patternTypes))
		for i, pt := range patternTypes {
			opts.Patterns[i] = patterns.PatternType(pt)
		}
	}

	// Detect patterns
	detected, err := detector.DetectPatterns(ctx, scope, opts)
	if err != nil {
		return nil, fmt.Errorf("detect patterns: %w", err)
	}

	// Calculate stats
	patternCounts := make(map[string]int)
	idiomaticCount := 0
	for _, p := range detected {
		patternCounts[string(p.Type)]++
		if p.Idiomatic {
			idiomaticCount++
		}
	}

	return &PatternScanOutput{
		Patterns:       detected,
		Summary:        detector.Summary(detected),
		PatternCounts:  patternCounts,
		IdiomaticCount: idiomaticCount,
		Duration:       time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *PatternScanNode) extractInputs(inputs map[string]any) (*graph.Graph, *index.SymbolIndex, string, []string, error) {
	// Extract graph
	graphRaw, ok := inputs["graph"]
	if !ok {
		// Try to get from BUILD_GRAPH output
		if buildOutput, ok := inputs["BUILD_GRAPH"]; ok {
			if output, ok := buildOutput.(*BuildGraphOutput); ok && output.Graph != nil {
				graphRaw = output.Graph
			}
		}
		if graphRaw == nil {
			return nil, nil, "", nil, fmt.Errorf("%w: graph", ErrMissingInput)
		}
	}

	g, ok := graphRaw.(*graph.Graph)
	if !ok {
		return nil, nil, "", nil, fmt.Errorf("%w: graph must be *graph.Graph", ErrInvalidInputType)
	}

	// Extract index
	indexRaw, ok := inputs["index"]
	if !ok {
		return nil, nil, "", nil, fmt.Errorf("%w: index", ErrMissingInput)
	}

	idx, ok := indexRaw.(*index.SymbolIndex)
	if !ok {
		return nil, nil, "", nil, fmt.Errorf("%w: index must be *index.SymbolIndex", ErrInvalidInputType)
	}

	// Extract optional scope
	scope := ""
	if scopeRaw, ok := inputs["scope"]; ok {
		if s, ok := scopeRaw.(string); ok {
			scope = s
		}
	}

	// Extract optional pattern types
	var patternTypes []string
	if patternsRaw, ok := inputs["patterns"]; ok {
		if pts, ok := patternsRaw.([]string); ok {
			patternTypes = pts
		}
	}

	return g, idx, scope, patternTypes, nil
}
