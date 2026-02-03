// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import "sort"

// ViolationTypeToPriority maps a violation type to its processing priority.
//
// Description:
//
//	Returns the priority for a given violation type. Lower values indicate
//	higher priority (processed first). New regression-related violations
//	have explicit priorities; existing types default to PriorityOther.
//
// Inputs:
//   - vt: The violation type to get priority for.
//
// Outputs:
//   - ViolationPriority: The priority level for ordering.
//
// Thread Safety: Safe for concurrent use (pure function).
func ViolationTypeToPriority(vt ViolationType) ViolationPriority {
	switch vt {
	case ViolationSemanticDrift:
		return PrioritySemanticDrift
	case ViolationPhantomFile:
		return PriorityPhantomFile
	case ViolationPhantomSymbol:
		return PriorityPhantomSymbol
	case ViolationStructuralClaim:
		return PriorityStructuralClaim
	case ViolationAttributeHallucination:
		return PriorityAttributeHallucination
	case ViolationLineNumberFabrication:
		return PriorityLineNumberFabrication
	case ViolationRelationshipHallucination:
		return PriorityRelationshipHallucination
	case ViolationBehavioralHallucination:
		return PriorityBehavioralHallucination
	case ViolationQuantitativeHallucination:
		return PriorityQuantitativeHallucination
	case ViolationLanguageConfusion:
		return PriorityLanguageConfusion
	case ViolationGenericPattern:
		return PriorityGenericPattern
	case ViolationFabricatedCode:
		return PriorityFabricatedCode
	case ViolationAPIHallucination:
		return PriorityAPIHallucination
	case ViolationTemporalHallucination:
		return PriorityTemporalHallucination
	case ViolationCrossContextConfusion:
		return PriorityCrossContextConfusion
	case ViolationConfidenceFabrication:
		return PriorityConfidenceFabrication
	default:
		return PriorityOther
	}
}

// Priority returns the processing priority for this violation.
//
// Description:
//
//	Convenience method that calls ViolationTypeToPriority with
//	the violation's type.
//
// Outputs:
//   - ViolationPriority: The priority level for ordering.
//
// Thread Safety: Safe for concurrent use.
func (v *Violation) Priority() ViolationPriority {
	return ViolationTypeToPriority(v.Type)
}

// SortViolationsByPriority returns violations sorted by priority.
//
// Description:
//
//	Sorts violations so that higher-priority violations (lower numeric value)
//	come first. Uses stable sort to preserve order within the same priority.
//	This ensures deterministic ordering for consistent behavior.
//
// Inputs:
//   - violations: Slice of violations to sort.
//
// Outputs:
//   - []Violation: New slice with violations sorted by priority.
//
// Thread Safety: Safe for concurrent use (creates new slice).
func SortViolationsByPriority(violations []Violation) []Violation {
	if len(violations) == 0 {
		return violations
	}

	// Create a copy to avoid mutating the input
	result := make([]Violation, len(violations))
	copy(result, violations)

	// Stable sort preserves order within same priority
	sort.SliceStable(result, func(i, j int) bool {
		pi := result[i].Priority()
		pj := result[j].Priority()

		if pi != pj {
			return pi < pj
		}

		// Within same priority, sort by location offset for determinism
		return result[i].LocationOffset < result[j].LocationOffset
	})

	return result
}

// DeduplicateCascadeViolations removes lower-priority violations subsumed by higher ones.
//
// Description:
//
//	When a higher-priority violation covers the same evidence as a lower-priority
//	one, the lower-priority violation is often a symptom of the higher one.
//	For example:
//	  - PhantomFile("config/app.py") + LanguageConfusion("Flask") →
//	    Keep only PhantomFile (language confusion is a consequence)
//
//	This function removes such redundant violations while preserving violations
//	with different evidence.
//
// Inputs:
//   - violations: Slice of violations (may be unsorted).
//
// Outputs:
//   - []Violation: Deduplicated violations, sorted by priority.
//
// Thread Safety: Safe for concurrent use (creates new slice).
func DeduplicateCascadeViolations(violations []Violation) []Violation {
	if len(violations) == 0 {
		return violations
	}

	// First, sort by priority
	sorted := SortViolationsByPriority(violations)

	// Track evidence seen at each priority level
	// key: normalized evidence string, value: priority that claimed it
	seen := make(map[string]ViolationPriority)
	result := make([]Violation, 0, len(violations))

	for _, v := range sorted {
		// Normalize evidence for comparison
		key := normalizeEvidence(v.Evidence)
		if key == "" {
			// No evidence to deduplicate against - always keep
			result = append(result, v)
			continue
		}

		currentPriority := v.Priority()

		if existingPriority, ok := seen[key]; ok {
			// Evidence already claimed by another violation
			if existingPriority < currentPriority {
				// Higher priority violation already covers this evidence - skip
				continue
			}
			// Same or lower priority - keep both (rare case, could be different aspects)
		}

		seen[key] = currentPriority
		result = append(result, v)
	}

	return result
}

// normalizeEvidence normalizes evidence strings for comparison.
//
// Description:
//
//	Extracts the core identifying information from evidence for deduplication.
//	For file paths, extracts the path. For other evidence, uses as-is.
//
// Inputs:
//   - evidence: The evidence string to normalize.
//
// Outputs:
//   - string: Normalized evidence for comparison.
func normalizeEvidence(evidence string) string {
	// Trim whitespace
	evidence = trimSpace(evidence)

	// If it looks like a file path, use it directly
	// File paths are the primary deduplication key
	if len(evidence) > 0 {
		return evidence
	}

	return ""
}

// trimSpace removes leading and trailing whitespace.
func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

// CountViolationsByPriority counts violations at each priority level.
//
// Description:
//
//	Returns a map from priority to count of violations at that priority.
//	Useful for metrics and deciding on retry strategies.
//
// Inputs:
//   - violations: Slice of violations to count.
//
// Outputs:
//   - map[ViolationPriority]int: Count of violations per priority.
//
// Thread Safety: Safe for concurrent use.
func CountViolationsByPriority(violations []Violation) map[ViolationPriority]int {
	counts := make(map[ViolationPriority]int)
	for _, v := range violations {
		counts[v.Priority()]++
	}
	return counts
}

// HasHighPriorityViolations returns true if any violations are P0, P1, or P2.
//
// Description:
//
//	Quick check for critical violations that should trigger special handling
//	(e.g., immediate retry or feedback loop).
//
// Inputs:
//   - violations: Slice of violations to check.
//
// Outputs:
//   - bool: True if any high-priority violations exist (P0-P2).
//
// Thread Safety: Safe for concurrent use.
func HasHighPriorityViolations(violations []Violation) bool {
	for _, v := range violations {
		p := v.Priority()
		// P0 = SemanticDrift, P1 = PhantomFile, P2 = StructuralClaim/PhantomSymbol/Attribute/Relationship/Behavioral
		if p <= PriorityStructuralClaim {
			return true
		}
	}
	return false
}

// FilterViolationsByPriority returns violations at or above a priority threshold.
//
// Description:
//
//	Filters to include only violations with priority <= threshold.
//	Lower priority numbers are higher priority, so threshold=2 includes
//	P1 (PhantomFile) and P2 (StructuralClaim).
//
// Inputs:
//   - violations: Slice of violations to filter.
//   - threshold: Maximum priority value to include (lower = higher priority).
//
// Outputs:
//   - []Violation: Filtered violations.
//
// Thread Safety: Safe for concurrent use.
func FilterViolationsByPriority(violations []Violation, threshold ViolationPriority) []Violation {
	result := make([]Violation, 0)
	for _, v := range violations {
		if v.Priority() <= threshold {
			result = append(result, v)
		}
	}
	return result
}
