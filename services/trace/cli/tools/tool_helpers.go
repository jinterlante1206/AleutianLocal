// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"context"
	"fmt"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// =============================================================================
// Shared Helper Functions for Graph Query Tools
// =============================================================================
//
// NOTE: Some helper functions like extractNameFromNodeID, extractPackageFromNodeID,
// matchesKind, and minInt are defined in graph_query_tools.go and will be migrated
// here as part of the TOOLS-01 refactoring ticket.

// DetectEntryPoint finds a suitable entry point for dominator analysis.
//
// Description:
//
//	Searches for well-known entry point functions (main, init) using the symbol
//	index, then falls back to graph analytics to detect nodes with no incoming edges.
//	This is the canonical method for finding entry points and should be used by all
//	tools requiring dominator tree computation.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - idx: Symbol index for name-based search. May be nil (will skip index search).
//   - analytics: Graph analytics for detecting entry nodes. Must not be nil.
//
// Outputs:
//   - string: Node ID of the detected entry point.
//   - error: Non-nil if no suitable entry point found.
//
// Thread Safety: Safe for concurrent use (read-only operations).
func DetectEntryPoint(ctx context.Context, idx *index.SymbolIndex, analytics *graph.GraphAnalytics) (string, error) {
	// Try to find main or init functions first using the index
	if idx != nil {
		for _, name := range []string{"main", "Main", "init", "Init"} {
			results, err := idx.Search(ctx, name, 1)
			if err == nil && len(results) > 0 {
				return results[0].ID, nil
			}
		}
	}

	// Fall back to graph analytics to detect entry nodes (no incoming edges)
	if analytics != nil {
		entryNodes := analytics.DetectEntryNodes(ctx)
		if len(entryNodes) > 0 {
			return entryNodes[0], nil
		}
	}

	return "", fmt.Errorf("no suitable entry point found in graph")
}

// extractFileFromNodeID extracts the file path from a node ID.
//
// Node IDs follow the format: "path/to/file.go:line:name"
// This extracts the "path/to/file.go" portion.
//
// Thread Safety: Safe for concurrent use.
func extractFileFromNodeID(nodeID string) string {
	for i, c := range nodeID {
		if c == ':' {
			return nodeID[:i]
		}
	}
	return ""
}

// extractLineFromNodeID extracts the line number from a node ID.
//
// Node IDs follow the format: "path/to/file.go:line:name"
// This extracts the "line" portion as an integer.
//
// Thread Safety: Safe for concurrent use.
func extractLineFromNodeID(nodeID string) int {
	colonCount := 0
	start := 0
	for i, c := range nodeID {
		if c == ':' {
			colonCount++
			if colonCount == 1 {
				start = i + 1
			} else if colonCount == 2 {
				// Parse line number
				line := 0
				for j := start; j < i; j++ {
					c := nodeID[j]
					if c >= '0' && c <= '9' {
						line = line*10 + int(c-'0')
					} else {
						return 0
					}
				}
				return line
			}
		}
	}
	return 0
}

// extractNameFromNodeID extracts the symbol name from a node ID.
//
// Node IDs follow the format: "path/to/file.go:line:name"
// This extracts the "name" portion.
//
// Thread Safety: Safe for concurrent use.
func extractNameFromNodeID(nodeID string) string {
	colonCount := 0
	for i, c := range nodeID {
		if c == ':' {
			colonCount++
			if colonCount == 2 {
				return nodeID[i+1:]
			}
		}
	}
	return nodeID
}

// minInt returns the smaller of two integers.
//
// Thread Safety: Safe for concurrent use.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maxInt returns the larger of two integers.
//
// Thread Safety: Safe for concurrent use.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clampInt clamps a value between min and max bounds.
//
// Thread Safety: Safe for concurrent use.
func clampInt(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

// parseStringArray extracts a string array from a parameter value.
//
// Handles both []string and []interface{} (from JSON unmarshaling).
//
// Thread Safety: Safe for concurrent use.
func parseStringArray(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result, true
	default:
		return nil, false
	}
}

// parseIntParam extracts an integer from a parameter value.
//
// Handles both int and float64 (from JSON unmarshaling).
//
// Thread Safety: Safe for concurrent use.
func parseIntParam(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// parseFloatParam extracts a float64 from a parameter value.
//
// Thread Safety: Safe for concurrent use.
func parseFloatParam(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// parseBoolParam extracts a boolean from a parameter value.
//
// Thread Safety: Safe for concurrent use.
func parseBoolParam(value any) (bool, bool) {
	if b, ok := value.(bool); ok {
		return b, true
	}
	return false, false
}

// parseStringParam extracts a string from a parameter value.
//
// Thread Safety: Safe for concurrent use.
func parseStringParam(value any) (string, bool) {
	if s, ok := value.(string); ok {
		return s, true
	}
	return "", false
}
