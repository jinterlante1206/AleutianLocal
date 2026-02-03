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
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety/trust_flow"
)

// CrossingDetector detects data flows that cross trust zone boundaries.
//
// Description:
//
//	CrossingDetector identifies where data moves from less trusted to more
//	trusted zones, and checks whether proper validation exists at the boundary.
//	This is the core of Aleutian's trust boundary analysis.
//
// Thread Safety:
//
//	CrossingDetector is safe for concurrent use after initialization.
type CrossingDetector struct {
	zoneDetector *ZoneDetector
	requirements *CrossingRequirements
	sanitizers   *trust_flow.SanitizerRegistry
}

// NewCrossingDetector creates a new CrossingDetector with default configuration.
func NewCrossingDetector() *CrossingDetector {
	return &CrossingDetector{
		zoneDetector: NewZoneDetector(),
		requirements: DefaultCrossingRequirements(),
		sanitizers:   trust_flow.NewSanitizerRegistry(),
	}
}

// NewCrossingDetectorWithConfig creates a CrossingDetector with custom config.
//
// Description:
//
//	Creates a detector with custom patterns, requirements, and sanitizer
//	registry for specialized analysis.
//
// Inputs:
//
//	patterns - Zone detection patterns. If nil, uses defaults.
//	requirements - Crossing requirements. If nil, uses defaults.
//	sanitizers - Sanitizer registry. If nil, uses defaults.
//
// Outputs:
//
//	*CrossingDetector - The configured detector.
func NewCrossingDetectorWithConfig(
	patterns *ZonePatterns,
	requirements *CrossingRequirements,
	sanitizers *trust_flow.SanitizerRegistry,
) *CrossingDetector {
	zd := NewZoneDetector()
	if patterns != nil {
		zd = NewZoneDetectorWithPatterns(patterns)
	}

	reqs := DefaultCrossingRequirements()
	if requirements != nil {
		reqs = requirements
	}

	sans := trust_flow.NewSanitizerRegistry()
	if sanitizers != nil {
		sans = sanitizers
	}

	return &CrossingDetector{
		zoneDetector: zd,
		requirements: reqs,
		sanitizers:   sans,
	}
}

// DetectCrossings finds all boundary crossings in the given scope.
//
// Description:
//
//	Analyzes the call graph to find edges that cross from one trust zone
//	to another. For each crossing, determines if proper validation exists.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	g - The code graph. Must be frozen.
//	zones - The detected trust zones from ZoneDetector.
//	scope - Package or path prefix to limit analysis. Empty for all.
//
// Outputs:
//
//	[]safety.BoundaryCrossing - All detected crossings.
//	error - Non-nil on context cancellation or graph not ready.
//
// Performance:
//
//	Processes nodes in parallel batches for large graphs.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (d *CrossingDetector) DetectCrossings(
	ctx context.Context,
	g *graph.Graph,
	zones []safety.TrustZone,
	scope string,
) ([]safety.BoundaryCrossing, error) {
	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}
	if !g.IsFrozen() {
		return nil, safety.ErrGraphNotReady
	}

	// Build zone lookup map for fast access
	zoneMap := make(map[string]*safety.TrustZone)
	for i := range zones {
		zoneMap[zones[i].ID] = &zones[i]
	}

	// Collect nodes from iterator into slice for batching
	var allNodes []*graph.Node
	for _, node := range g.Nodes() {
		allNodes = append(allNodes, node)
	}

	// Process nodes in parallel batches
	var crossings []safety.BoundaryCrossing
	var mu sync.Mutex
	var wg sync.WaitGroup

	batchSize := 100
	for i := 0; i < len(allNodes); i += batchSize {
		// Check context
		if err := ctx.Err(); err != nil {
			return crossings, nil // Return partial results
		}

		end := i + batchSize
		if end > len(allNodes) {
			end = len(allNodes)
		}

		wg.Add(1)
		go func(batch []*graph.Node) {
			defer wg.Done()

			localCrossings := d.detectCrossingsInBatch(g, batch, zones, scope)

			if len(localCrossings) > 0 {
				mu.Lock()
				crossings = append(crossings, localCrossings...)
				mu.Unlock()
			}
		}(allNodes[i:end])
	}
	wg.Wait()

	return crossings, nil
}

// detectCrossingsInBatch finds crossings in a batch of nodes.
func (d *CrossingDetector) detectCrossingsInBatch(
	g *graph.Graph,
	batch []*graph.Node,
	zones []safety.TrustZone,
	scope string,
) []safety.BoundaryCrossing {
	var crossings []safety.BoundaryCrossing

	for _, node := range batch {
		if node.Symbol == nil {
			continue
		}

		// Check scope filter
		if scope != "" {
			if !hasPrefix(node.Symbol.FilePath, scope) &&
				!hasPrefix(node.Symbol.Package, scope) {
				continue
			}
		}

		// Classify this node's zone
		fromLevel, fromZoneName := d.zoneDetector.classifyNode(node)
		fromZoneID := GenerateZoneID(fromLevel, fromZoneName)
		fromZone := d.zoneDetector.FindZoneForNode(node, zones)

		// Check each outgoing edge for zone crossings
		for _, edge := range node.Outgoing {
			if edge.Type != graph.EdgeTypeCalls {
				continue
			}

			toNode, exists := g.GetNode(edge.ToID)
			if !exists || toNode.Symbol == nil {
				continue
			}

			// Classify target node's zone
			toLevel, toZoneName := d.zoneDetector.classifyNode(toNode)
			toZoneID := GenerateZoneID(toLevel, toZoneName)
			toZone := d.zoneDetector.FindZoneForNode(toNode, zones)

			// Check if this is a boundary crossing
			if fromZoneID == toZoneID {
				continue // Same zone, no crossing
			}

			// Only report crossings from less trusted to more trusted
			if !d.requirements.RequiresValidation(fromLevel, toLevel) {
				continue
			}

			// Build the crossing
			crossing := safety.BoundaryCrossing{
				ID:         GenerateCrossingID(fromZoneID, toZoneID, node.ID),
				From:       fromZone,
				To:         toZone,
				CrossingAt: fmt.Sprintf("%s:%d", node.Symbol.FilePath, node.Symbol.StartLine),
				DataPath:   []string{node.ID, toNode.ID},
			}

			// Check if validation exists along this path
			hasVal, valFn := d.checkValidationExists(g, node, toNode)
			crossing.HasValidation = hasVal
			crossing.ValidationFn = valFn

			crossings = append(crossings, crossing)
		}
	}

	return crossings
}

// checkValidationExists determines if validation exists between two nodes.
//
// Description:
//
//	Checks if there's a sanitizer/validator function call between the
//	source node and the target node in the call path.
//
// Inputs:
//
//	g - The code graph.
//	from - The source node (lower trust).
//	to - The target node (higher trust).
//
// Outputs:
//
//	bool - True if validation was found.
//	string - The validation function name, empty if none found.
func (d *CrossingDetector) checkValidationExists(
	g *graph.Graph,
	from *graph.Node,
	to *graph.Node,
) (bool, string) {
	// Check if 'to' is itself a validator/sanitizer
	if to.Symbol != nil {
		if pat, isSanitizer := d.sanitizers.MatchSanitizer(to.Symbol); isSanitizer {
			return true, fmt.Sprintf("%s (sanitizer: %s)", to.Symbol.Name, pat.Description)
		}
	}

	// Check if 'from' calls any validators before calling 'to'
	// This is a simplified check - we look at from's other outgoing calls
	for _, edge := range from.Outgoing {
		if edge.ToID == to.ID {
			continue // Skip the target edge
		}

		intermediate, exists := g.GetNode(edge.ToID)
		if !exists || intermediate.Symbol == nil {
			continue
		}

		// Check if this intermediate is a validator/sanitizer
		if pat, isSanitizer := d.sanitizers.MatchSanitizer(intermediate.Symbol); isSanitizer {
			return true, fmt.Sprintf("%s (sanitizer: %s)", intermediate.Symbol.Name, pat.Description)
		}
	}

	// Check common validation patterns in function name
	if from.Symbol != nil {
		if d.hasValidationInName(from.Symbol.Name) {
			return true, from.Symbol.Name
		}
	}

	return false, ""
}

// hasValidationInName checks if a function name suggests validation.
func (d *CrossingDetector) hasValidationInName(name string) bool {
	// Match boundary patterns
	if _, matched := d.zoneDetector.patterns.MatchFunction(name); matched {
		return true
	}
	return false
}

// FindViolations identifies crossings that lack required validation.
//
// Description:
//
//	Analyzes crossings to find boundary violations where data flows from
//	a less trusted zone to a more trusted zone without proper validation.
//
// Inputs:
//
//	crossings - The detected boundary crossings.
//
// Outputs:
//
//	[]safety.BoundaryViolation - Violations where validation is missing.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (d *CrossingDetector) FindViolations(
	crossings []safety.BoundaryCrossing,
) []safety.BoundaryViolation {
	var violations []safety.BoundaryViolation

	for i := range crossings {
		crossing := &crossings[i]

		// Skip if validation exists
		if crossing.HasValidation {
			continue
		}

		// Skip if zones are nil (shouldn't happen but defensive)
		if crossing.From == nil || crossing.To == nil {
			continue
		}

		fromLevel := crossing.From.Level
		toLevel := crossing.To.Level

		// Get requirements for this crossing type
		reqs := d.requirements.GetRequirements(fromLevel, toLevel)
		if len(reqs) == 0 {
			continue // No requirements defined
		}

		// Create violation
		violation := safety.BoundaryViolation{
			Crossing:    crossing,
			Severity:    d.requirements.GetSeverity(fromLevel, toLevel),
			MissingStep: formatRequirements(reqs),
			CWE:         d.requirements.GetCWE(fromLevel, toLevel),
			Remediation: d.generateRemediation(fromLevel, toLevel, reqs),
		}

		violations = append(violations, violation)
	}

	return violations
}

// generateRemediation creates remediation advice for a violation.
func (d *CrossingDetector) generateRemediation(
	from, to safety.TrustLevel,
	requirements []string,
) string {
	switch {
	case from == safety.TrustExternal && to == safety.TrustPrivileged:
		return "Add authentication and authorization middleware before this call. " +
			"Validate all input parameters. Consider rate limiting."

	case from == safety.TrustExternal && to == safety.TrustInternal:
		return "Add input validation before passing data to internal functions. " +
			"Consider using a validation library with strict type checking."

	case from == safety.TrustValidation && to == safety.TrustPrivileged:
		return "Add authorization check to verify the caller has permission " +
			"to access privileged functionality."

	case from == safety.TrustInternal && to == safety.TrustPrivileged:
		return "Add an authorization guard to prevent privilege escalation."

	default:
		return "Add appropriate validation before crossing this trust boundary."
	}
}

// formatRequirements formats requirements into a human-readable string.
func formatRequirements(reqs []string) string {
	if len(reqs) == 0 {
		return "validation required"
	}
	if len(reqs) == 1 {
		return reqs[0]
	}
	return reqs[0] + fmt.Sprintf(" (+%d more)", len(reqs)-1)
}

// hasPrefix is a string prefix check helper.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
