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

	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/impact"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// BlastRadiusNode analyzes the impact of changing a symbol.
//
// Description:
//
//	Uses the impact.ChangeImpactAnalyzer to determine what would be
//	affected if a symbol is changed. Includes callers, test coverage,
//	side effects, and risk assessment.
//
// Inputs (from map[string]any):
//
//	"graph" (*graph.Graph): The code graph. Required.
//	"index" (*index.SymbolIndex): Symbol index for lookups. Required.
//	"target_id" (string): Symbol ID to analyze. Required.
//	"proposed_change" (string): New signature for breaking change analysis. Optional.
//
// Outputs:
//
//	*BlastRadiusOutput containing:
//	  - Impact: The change impact analysis result
//	  - RiskLevel: Overall risk level
//	  - Duration: Analysis time
//
// Thread Safety:
//
//	Safe for concurrent use.
type BlastRadiusNode struct {
	dag.BaseNode
}

// BlastRadiusOutput contains the result of blast radius analysis.
type BlastRadiusOutput struct {
	// Impact contains the full change impact analysis.
	Impact *impact.ChangeImpact

	// RiskLevel is the overall risk level (low, medium, high, critical).
	RiskLevel string

	// RiskScore is the numeric risk score (0.0 - 1.0).
	RiskScore float64

	// DirectCallers is the count of direct callers.
	DirectCallers int

	// TotalImpact is the total number of affected symbols.
	TotalImpact int

	// Duration is the analysis time.
	Duration time.Duration
}

// NewBlastRadiusNode creates a new blast radius node.
//
// Inputs:
//
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*BlastRadiusNode - The configured node.
func NewBlastRadiusNode(deps []string) *BlastRadiusNode {
	return &BlastRadiusNode{
		BaseNode: dag.BaseNode{
			NodeName:         "BLAST_RADIUS",
			NodeDependencies: deps,
			NodeTimeout:      1 * time.Minute,
			NodeRetryable:    false,
		},
	}
}

// Execute analyzes the blast radius of changing a symbol.
//
// Description:
//
//	Creates a ChangeImpactAnalyzer and runs full impact analysis.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "graph", "index", "target_id", and optionally "proposed_change".
//
// Outputs:
//
//	*BlastRadiusOutput - The analysis result.
//	error - Non-nil if required inputs are missing or analysis fails.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *BlastRadiusNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	// Extract inputs
	g, idx, targetID, proposedChange, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	if !g.IsFrozen() {
		return nil, ErrGraphNotFrozen
	}

	start := time.Now()

	// Create analyzer
	analyzer := impact.NewChangeImpactAnalyzer(g, idx)

	// Analyze impact
	result, err := analyzer.AnalyzeImpact(ctx, targetID, proposedChange, nil)
	if err != nil {
		return nil, fmt.Errorf("analyze impact: %w", err)
	}

	return &BlastRadiusOutput{
		Impact:        result,
		RiskLevel:     string(result.RiskLevel),
		RiskScore:     result.RiskScore,
		DirectCallers: result.DirectCallers,
		TotalImpact:   result.TotalImpact,
		Duration:      time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *BlastRadiusNode) extractInputs(inputs map[string]any) (*graph.Graph, *index.SymbolIndex, string, string, error) {
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
			return nil, nil, "", "", fmt.Errorf("%w: graph", ErrMissingInput)
		}
	}

	g, ok := graphRaw.(*graph.Graph)
	if !ok {
		return nil, nil, "", "", fmt.Errorf("%w: graph must be *graph.Graph", ErrInvalidInputType)
	}

	// Extract index
	indexRaw, ok := inputs["index"]
	if !ok {
		return nil, nil, "", "", fmt.Errorf("%w: index", ErrMissingInput)
	}

	idx, ok := indexRaw.(*index.SymbolIndex)
	if !ok {
		return nil, nil, "", "", fmt.Errorf("%w: index must be *index.SymbolIndex", ErrInvalidInputType)
	}

	// Extract target_id
	targetIDRaw, ok := inputs["target_id"]
	if !ok {
		return nil, nil, "", "", fmt.Errorf("%w: target_id", ErrMissingInput)
	}

	targetID, ok := targetIDRaw.(string)
	if !ok {
		return nil, nil, "", "", fmt.Errorf("%w: target_id must be string", ErrInvalidInputType)
	}

	// Extract optional proposed_change
	proposedChange := ""
	if changeRaw, ok := inputs["proposed_change"]; ok {
		if s, ok := changeRaw.(string); ok {
			proposedChange = s
		}
	}

	return g, idx, targetID, proposedChange, nil
}
