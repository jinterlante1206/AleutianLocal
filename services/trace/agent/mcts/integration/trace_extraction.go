// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"sort"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/activities"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// Trace Extraction
// -----------------------------------------------------------------------------

// maxCompositeDeltaDepth limits recursion depth when flattening CompositeDelta.
// Prevents stack overflow on malformed deeply nested deltas.
const maxCompositeDeltaDepth = 100

// ExtractTraceStep creates a TraceStep from activity execution results.
//
// Description:
//
//	Converts the result of activity execution into a TraceStep suitable
//	for recording. Extracts relevant data from the ActivityResult and
//	Delta, handling nil/empty cases gracefully.
//
// Inputs:
//
//	result - The activity execution result. May be nil.
//	delta - The delta produced by the activity. May be nil.
//	input - The activity input for target extraction. May be nil.
//	startTime - When the activity started.
//
// Outputs:
//
//	crs.TraceStep - The extracted trace step. Never nil-equivalent.
//
// Thread Safety: Safe for concurrent use (stateless pure function).
func ExtractTraceStep(
	result *activities.ActivityResult,
	delta crs.Delta,
	input activities.ActivityInput,
	startTime time.Time,
) crs.TraceStep {
	step := crs.TraceStep{
		Timestamp: startTime,
	}

	// Extract from result
	if result != nil {
		step.Action = result.ActivityName
		step.Duration = result.Duration
		step.SymbolsFound = extractSymbolsFound(result.AlgorithmResults)

		// Record errors from failed algorithms
		if !result.Success && !result.PartialSuccess {
			step.Error = "activity failed"
		}
	}

	// Extract target from input
	step.Target = extractTarget(input)

	// Extract from delta
	if delta != nil {
		step.ProofUpdates = extractProofUpdates(delta, 0)
		step.ConstraintsAdded = extractConstraints(delta, 0)
		step.DependenciesFound = extractDependencies(delta, 0)
	} else {
		// Return empty slices, not nil (for JSON marshaling)
		step.ProofUpdates = []crs.ProofUpdate{}
		step.ConstraintsAdded = []crs.ConstraintUpdate{}
		step.DependenciesFound = []crs.DependencyEdge{}
	}

	return step
}

// extractProofUpdates extracts proof changes from a delta.
//
// Description:
//
//	Recursively processes the delta (including CompositeDelta) to extract
//	all proof number updates. Results are deterministically ordered by node ID.
//
// Inputs:
//
//	delta - The delta to extract from. May be nil.
//	depth - Current recursion depth for stack overflow protection.
//
// Outputs:
//
//	[]crs.ProofUpdate - Extracted proof updates, sorted by NodeID.
func extractProofUpdates(delta crs.Delta, depth int) []crs.ProofUpdate {
	if delta == nil || depth > maxCompositeDeltaDepth {
		return []crs.ProofUpdate{}
	}

	var updates []crs.ProofUpdate

	switch d := delta.(type) {
	case *crs.ProofDelta:
		if len(d.Updates) == 0 {
			return []crs.ProofUpdate{}
		}

		// Pre-allocate with capacity hint
		updates = make([]crs.ProofUpdate, 0, len(d.Updates))

		// Collect keys for deterministic ordering
		nodeIDs := make([]string, 0, len(d.Updates))
		for nodeID := range d.Updates {
			nodeIDs = append(nodeIDs, nodeID)
		}
		sort.Strings(nodeIDs)

		// Extract updates in deterministic order
		for _, nodeID := range nodeIDs {
			proof := d.Updates[nodeID]
			updates = append(updates, crs.ProofUpdate{
				NodeID: nodeID,
				Status: proof.Status.String(),
				Source: proof.Source.String(),
			})
		}

	case *crs.CompositeDelta:
		// Flatten nested deltas
		for _, nested := range d.Deltas {
			nested := extractProofUpdates(nested, depth+1)
			updates = append(updates, nested...)
		}

	default:
		// Unknown delta type - return empty slice
		return []crs.ProofUpdate{}
	}

	// Ensure we never return nil
	if updates == nil {
		return []crs.ProofUpdate{}
	}
	return updates
}

// extractConstraints extracts constraint additions from a delta.
//
// Description:
//
//	Recursively processes the delta to extract all added and updated
//	constraints. Results are deterministically ordered by constraint ID.
//
// Inputs:
//
//	delta - The delta to extract from. May be nil.
//	depth - Current recursion depth for stack overflow protection.
//
// Outputs:
//
//	[]crs.ConstraintUpdate - Extracted constraint updates.
func extractConstraints(delta crs.Delta, depth int) []crs.ConstraintUpdate {
	if delta == nil || depth > maxCompositeDeltaDepth {
		return []crs.ConstraintUpdate{}
	}

	var updates []crs.ConstraintUpdate

	switch d := delta.(type) {
	case *crs.ConstraintDelta:
		// Estimate capacity
		capacity := len(d.Add) + len(d.Update)
		if capacity == 0 {
			return []crs.ConstraintUpdate{}
		}
		updates = make([]crs.ConstraintUpdate, 0, capacity)

		// Extract from Add slice
		for _, c := range d.Add {
			// Copy Nodes slice to avoid sharing reference
			nodes := make([]string, len(c.Nodes))
			copy(nodes, c.Nodes)

			updates = append(updates, crs.ConstraintUpdate{
				ID:    c.ID,
				Type:  c.Type.String(),
				Nodes: nodes,
			})
		}

		// Extract from Update map (deterministic order)
		if len(d.Update) > 0 {
			updateIDs := make([]string, 0, len(d.Update))
			for id := range d.Update {
				updateIDs = append(updateIDs, id)
			}
			sort.Strings(updateIDs)

			for _, id := range updateIDs {
				c := d.Update[id]
				nodes := make([]string, len(c.Nodes))
				copy(nodes, c.Nodes)

				updates = append(updates, crs.ConstraintUpdate{
					ID:    c.ID,
					Type:  c.Type.String(),
					Nodes: nodes,
				})
			}
		}

	case *crs.CompositeDelta:
		for _, nested := range d.Deltas {
			nested := extractConstraints(nested, depth+1)
			updates = append(updates, nested...)
		}
	}

	return updates
}

// extractDependencies extracts dependency edges from a delta.
//
// Description:
//
//	Recursively processes the delta to extract all added dependency edges.
//	Results preserve the order from the delta.
//
// Inputs:
//
//	delta - The delta to extract from. May be nil.
//	depth - Current recursion depth for stack overflow protection.
//
// Outputs:
//
//	[]crs.DependencyEdge - Extracted dependency edges.
func extractDependencies(delta crs.Delta, depth int) []crs.DependencyEdge {
	if delta == nil || depth > maxCompositeDeltaDepth {
		return []crs.DependencyEdge{}
	}

	var edges []crs.DependencyEdge

	switch d := delta.(type) {
	case *crs.DependencyDelta:
		if len(d.AddEdges) == 0 {
			return []crs.DependencyEdge{}
		}

		edges = make([]crs.DependencyEdge, 0, len(d.AddEdges))
		for _, edge := range d.AddEdges {
			edges = append(edges, crs.DependencyEdge{
				From: edge[0],
				To:   edge[1],
			})
		}

	case *crs.CompositeDelta:
		for _, nested := range d.Deltas {
			nested := extractDependencies(nested, depth+1)
			edges = append(edges, nested...)
		}
	}

	return edges
}

// extractTarget extracts the target from activity input.
//
// Description:
//
//	Attempts to extract a meaningful target string from the activity input.
//	Uses type assertions to handle known input types.
//
// Inputs:
//
//	input - The activity input. May be nil.
//
// Outputs:
//
//	string - The extracted target, or empty string if not available.
func extractTarget(input activities.ActivityInput) string {
	if input == nil {
		return ""
	}

	// Check for common input types that have target-like fields
	// Use type switch for safe handling

	// Try to get target from interface if it has a Target method
	type targetGetter interface {
		Target() string
	}
	if tg, ok := input.(targetGetter); ok {
		return tg.Target()
	}

	// Try to get file path from interface if it has a FilePath method
	type filePathGetter interface {
		FilePath() string
	}
	if fpg, ok := input.(filePathGetter); ok {
		return fpg.FilePath()
	}

	// Return empty string for unknown input types
	return ""
}

// extractSymbolsFound extracts discovered symbols from algorithm results.
//
// Description:
//
//	Iterates through algorithm results to extract symbol names from
//	successful algorithm executions. Only includes results from
//	algorithms that completed successfully.
//
// Inputs:
//
//	results - Algorithm results to extract from. May be nil or empty.
//
// Outputs:
//
//	[]string - Extracted symbol names, or empty slice if none found.
func extractSymbolsFound(results []*algorithms.Result) []string {
	if len(results) == 0 {
		return []string{}
	}

	var symbols []string

	for _, result := range results {
		if result == nil || !result.Success() {
			continue
		}

		// Check if output contains symbols
		// Use type assertions for known output types
		extracted := extractSymbolsFromOutput(result.Output)
		symbols = append(symbols, extracted...)
	}

	if len(symbols) == 0 {
		return []string{}
	}

	return symbols
}

// extractSymbolsFromOutput attempts to extract symbol names from algorithm output.
//
// Description:
//
//	Uses type assertions to extract symbol information from various
//	known algorithm output types.
//
// Inputs:
//
//	output - The algorithm output. May be nil or any type.
//
// Outputs:
//
//	[]string - Extracted symbol names, or empty slice if none found.
func extractSymbolsFromOutput(output any) []string {
	if output == nil {
		return nil
	}

	// Check for common output types that contain symbols

	// Check for SymbolsProvider interface
	type symbolsProvider interface {
		Symbols() []string
	}
	if sp, ok := output.(symbolsProvider); ok {
		src := sp.Symbols()
		if len(src) == 0 {
			return nil
		}
		// Copy to avoid sharing reference
		result := make([]string, len(src))
		copy(result, src)
		return result
	}

	// Check for string slice directly
	if symbols, ok := output.([]string); ok {
		if len(symbols) == 0 {
			return nil
		}
		result := make([]string, len(symbols))
		copy(result, symbols)
		return result
	}

	return nil
}
