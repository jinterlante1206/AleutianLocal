// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"strings"
	"sync"
	"unicode"
)

// =============================================================================
// Semantic Tool Call Tracking (GR-38 Issue 17)
// =============================================================================

// MaxToolCallHistorySize is the maximum number of calls to retain in history.
// Older calls are evicted when this limit is reached.
// R1.1 Fix: Prevent unbounded memory growth.
const MaxToolCallHistorySize = 100

// noiseWords is the set of common words that don't add semantic value.
// R1.3 Fix: Moved to package level to avoid allocation on every call.
var noiseWords = map[string]bool{
	"the": true, "a": true, "an": true,
	"in": true, "on": true, "at": true,
	"to": true, "for": true, "of": true,
	"is": true, "are": true, "was": true,
	"it": true, "be": true,
}

// ToolCallSignature represents a unique tool call with semantic signature.
//
// Description:
//
//	Captures the tool name and extracted query terms for semantic comparison.
//	Two calls are considered semantically equivalent if they have the same tool
//	and high Jaccard similarity between their query terms.
//
// Thread Safety: Immutable after creation.
type ToolCallSignature struct {
	// Tool is the tool name (e.g., "Grep", "find_callers").
	Tool string

	// QueryTerms are extracted terms from the query parameters.
	// Used for Jaccard similarity comparison.
	QueryTerms map[string]bool

	// RawQuery is the original query string before term extraction.
	// Stored for exact duplicate detection and debugging.
	RawQuery string

	// StepNumber is when this call was made (1-indexed).
	StepNumber int

	// Success indicates whether the tool call succeeded.
	Success bool
}

// ToolCallHistory tracks tool calls with semantic signatures.
//
// Description:
//
//	Maintains a history of tool calls for semantic duplicate detection.
//	Provides methods to check if a proposed call is similar to prior calls.
//	R1.1 Fix: History is limited to MaxToolCallHistorySize entries.
//
// Thread Safety: Safe for concurrent use.
type ToolCallHistory struct {
	mu      sync.RWMutex
	calls   []ToolCallSignature
	maxSize int
}

// NewToolCallHistory creates a new empty history with default max size.
//
// Outputs:
//
//	*ToolCallHistory - The history instance.
//
// Thread Safety: The returned instance is safe for concurrent use.
func NewToolCallHistory() *ToolCallHistory {
	return NewToolCallHistoryWithSize(MaxToolCallHistorySize)
}

// NewToolCallHistoryWithSize creates a new empty history with specified max size.
//
// Inputs:
//
//	maxSize - Maximum number of entries to retain. If <= 0, uses default.
//
// Outputs:
//
//	*ToolCallHistory - The history instance.
//
// Thread Safety: The returned instance is safe for concurrent use.
func NewToolCallHistoryWithSize(maxSize int) *ToolCallHistory {
	if maxSize <= 0 {
		maxSize = MaxToolCallHistorySize
	}
	return &ToolCallHistory{
		calls:   make([]ToolCallSignature, 0, min(maxSize, 100)),
		maxSize: maxSize,
	}
}

// Add records a tool call signature in the history.
//
// Description:
//
//	Appends the signature to history. If history exceeds maxSize,
//	older entries are evicted (sliding window).
//
// Inputs:
//
//	sig - The tool call signature to record.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) Add(sig ToolCallSignature) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.calls = append(h.calls, sig)

	// R1.1 Fix: Evict oldest entries if over limit
	if len(h.calls) > h.maxSize {
		// Keep the most recent maxSize entries
		excess := len(h.calls) - h.maxSize
		h.calls = h.calls[excess:]
	}
}

// GetCallsForTool returns all calls for a specific tool.
//
// Inputs:
//
//	tool - The tool name to filter by.
//
// Outputs:
//
//	[]ToolCallSignature - Calls matching the tool name.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) GetCallsForTool(tool string) []ToolCallSignature {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []ToolCallSignature
	for _, call := range h.calls {
		if call.Tool == tool {
			result = append(result, call)
		}
	}
	return result
}

// GetSimilarity finds the most similar prior call for a tool+query combination.
//
// Description:
//
//	Compares the proposed query terms against all prior calls of the same tool.
//	Returns the maximum similarity found and a copy of the most similar call.
//	I1.1 Fix: Returns copy instead of pointer to avoid data races.
//
// Inputs:
//
//	tool - The tool name to check.
//	queryTerms - Extracted terms from the proposed call's parameters.
//
// Outputs:
//
//	maxSimilarity - Highest Jaccard similarity found (0.0-1.0).
//	mostSimilar - Copy of the most similar prior call, or nil if none found.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) GetSimilarity(tool string, queryTerms map[string]bool) (float64, *ToolCallSignature) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var maxSimilarity float64
	var mostSimilarIdx int = -1

	for i := range h.calls {
		if h.calls[i].Tool != tool {
			continue
		}

		similarity := JaccardSimilarity(queryTerms, h.calls[i].QueryTerms)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
			mostSimilarIdx = i
		}
	}

	// I1.1 Fix: Return a copy of the signature, not a pointer into the slice
	if mostSimilarIdx >= 0 {
		callCopy := h.calls[mostSimilarIdx]
		return maxSimilarity, &callCopy
	}

	return maxSimilarity, nil
}

// IsExactDuplicate checks if a call is an exact duplicate of a prior call.
//
// Description:
//
//	An exact duplicate is same tool + same raw query (case-insensitive).
//	P1.3 Fix: Uses EqualFold directly without redundant ToLower.
//	I1.1 Fix: Returns copy instead of pointer to avoid data races.
//
// Inputs:
//
//	tool - The tool name.
//	rawQuery - The raw query string.
//
// Outputs:
//
//	bool - True if an exact duplicate exists.
//	*ToolCallSignature - Copy of the duplicate call, or nil if none.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) IsExactDuplicate(tool, rawQuery string) (bool, *ToolCallSignature) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// P1.3 Fix: TrimSpace once, use EqualFold for case-insensitive comparison
	trimmedQuery := strings.TrimSpace(rawQuery)

	for i := range h.calls {
		if h.calls[i].Tool != tool {
			continue
		}

		if strings.EqualFold(strings.TrimSpace(h.calls[i].RawQuery), trimmedQuery) {
			// I1.1 Fix: Return a copy
			callCopy := h.calls[i]
			return true, &callCopy
		}
	}

	return false, nil
}

// Len returns the number of recorded calls.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.calls)
}

// Clear removes all recorded calls.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = h.calls[:0]
}

// Trim reduces the history to the specified size, keeping the most recent entries.
//
// Description:
//
//	R1.2 Fix: Allows external callers to trim history if needed.
//
// Inputs:
//
//	size - Maximum number of entries to keep. If <= 0, clears all entries.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) Trim(size int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if size <= 0 {
		h.calls = h.calls[:0]
		return
	}

	if len(h.calls) > size {
		excess := len(h.calls) - size
		h.calls = h.calls[excess:]
	}
}

// MaxSize returns the maximum history size.
//
// Thread Safety: Safe for concurrent use.
func (h *ToolCallHistory) MaxSize() int {
	return h.maxSize
}

// =============================================================================
// Semantic Similarity Functions
// =============================================================================

// ExtractQueryTerms extracts terms from a query string for Jaccard comparison.
//
// Description:
//
//	Tokenizes the query into lowercase terms, normalizing for comparison.
//	Handles common delimiters like spaces, underscores, and camelCase.
//	R1.4 Fix: Uses unicode.IsUpper for proper Unicode handling.
//
// Inputs:
//
//	query - The query string to tokenize.
//
// Outputs:
//
//	map[string]bool - Set of unique lowercase terms.
//
// Thread Safety: Safe for concurrent use (no shared state).
func ExtractQueryTerms(query string) map[string]bool {
	terms := make(map[string]bool)

	if query == "" {
		return terms
	}

	// Split camelCase FIRST (before lowercase): "parseConfig" -> "parse Config"
	// R1.4 Fix: Use unicode.IsUpper for proper Unicode handling
	var expanded strings.Builder
	var prevWasUpper bool
	for i, r := range query {
		isUpper := unicode.IsUpper(r)
		if i > 0 && isUpper && !prevWasUpper {
			expanded.WriteRune(' ')
		}
		expanded.WriteRune(r)
		prevWasUpper = isUpper
	}
	query = expanded.String()

	// Normalize to lowercase
	query = strings.ToLower(query)

	// Replace common delimiters with spaces
	query = strings.ReplaceAll(query, "_", " ")
	query = strings.ReplaceAll(query, "-", " ")
	query = strings.ReplaceAll(query, ".", " ")
	query = strings.ReplaceAll(query, "/", " ")
	query = strings.ReplaceAll(query, "\\", " ")
	query = strings.ReplaceAll(query, ":", " ")

	// Extract words
	words := strings.Fields(query)
	for _, word := range words {
		// Skip single characters and common noise words
		// R1.3 Fix: Use package-level noiseWords map
		if len(word) >= 2 && !noiseWords[word] {
			terms[word] = true
		}
	}

	return terms
}

// JaccardSimilarity calculates the Jaccard similarity between two term sets.
//
// Description:
//
//	Jaccard = |intersection| / |union|
//	Returns 0.0 if either set is empty, 1.0 if identical.
//
// Inputs:
//
//	a, b - Term sets to compare. Nil maps are treated as empty.
//
// Outputs:
//
//	float64 - Similarity score in range [0.0, 1.0].
//
// Thread Safety: Safe for concurrent use (read-only on inputs).
func JaccardSimilarity(a, b map[string]bool) float64 {
	// S1.1 Fix: Handle nil maps gracefully
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	intersectionCount := 0
	for term := range a {
		if b[term] {
			intersectionCount++
		}
	}

	// |union| = |a| + |b| - |intersection|
	unionCount := len(a) + len(b) - intersectionCount

	if unionCount == 0 {
		return 0.0
	}

	return float64(intersectionCount) / float64(unionCount)
}

// ExtractQueryFromParams extracts the query/pattern parameter from tool params.
//
// Description:
//
//	Different tools use different parameter names for their "query" concept.
//	This function extracts the relevant parameter for semantic comparison.
//
// Inputs:
//
//	params - Map of parameter name to value.
//
// Outputs:
//
//	string - The query/pattern string, or empty if not found.
func ExtractQueryFromParams(params map[string]any) string {
	// Parameter names that typically contain the "query" concept
	queryParamNames := []string{
		"pattern",
		"query",
		"search",
		"symbol",
		"name",
		"path",
		"target",
		"function_name",
		"file_path",
		"type",
		"kind",
	}

	for _, paramName := range queryParamNames {
		if val, ok := params[paramName]; ok {
			if strVal, ok := val.(string); ok && strVal != "" {
				return strVal
			}
		}
	}

	return ""
}

// ExtractQueryFromMetadata extracts query from trace step metadata.
//
// Description:
//
//	Trace steps store parameters in Metadata as string values.
//	This function extracts the query for semantic comparison.
//
// Inputs:
//
//	metadata - Map of parameter name to string value.
//
// Outputs:
//
//	string - The query string, or empty if not found.
func ExtractQueryFromMetadata(metadata map[string]string) string {
	queryParamNames := []string{
		"pattern",
		"query",
		"search",
		"symbol",
		"name",
		"path",
		"target",
		"function_name",
		"file_path",
		"type",
		"kind",
	}

	for _, paramName := range queryParamNames {
		if val, ok := metadata[paramName]; ok && val != "" {
			return val
		}
	}

	return ""
}

// =============================================================================
// Semantic Similarity Thresholds
// =============================================================================

const (
	// SemanticDuplicateThreshold is the similarity threshold for blocking.
	// Calls with similarity >= this are considered semantic duplicates.
	SemanticDuplicateThreshold = 0.8

	// SemanticPenaltyThreshold is the similarity threshold for penalizing.
	// Calls with similarity >= this (but < duplicate threshold) get penalized.
	SemanticPenaltyThreshold = 0.3

	// DefaultSemanticPenaltyWeight is the default UCB1 penalty weight.
	// SemanticPenalty = similarity * weight.
	DefaultSemanticPenaltyWeight = 0.5
)

// CheckSemanticStatus evaluates a proposed call against history.
//
// Description:
//
//	Returns the semantic status of a proposed tool call:
//	- Blocked: Similarity >= 0.8 (semantic duplicate)
//	- Penalized: Similarity >= 0.3 (similar but not duplicate)
//	- Allowed: Similarity < 0.3 (sufficiently different)
//
// Inputs:
//
//	history - The call history to check against. If nil, returns "allowed".
//	tool - The proposed tool name. If empty, returns "allowed".
//	rawQuery - The raw query string.
//
// Outputs:
//
//	status - "blocked", "penalized", or "allowed".
//	similarity - The maximum similarity found.
//	similarCall - Copy of the most similar prior call, or nil.
//
// Thread Safety: Safe for concurrent use.
func CheckSemanticStatus(history *ToolCallHistory, tool, rawQuery string) (status string, similarity float64, similarCall *ToolCallSignature) {
	// S1.1 Fix: Validate inputs
	if history == nil {
		return "allowed", 0.0, nil
	}
	if tool == "" {
		return "allowed", 0.0, nil
	}

	// Check for exact duplicate first (fast path)
	if isDup, dupCall := history.IsExactDuplicate(tool, rawQuery); isDup {
		return "blocked", 1.0, dupCall
	}

	// Check semantic similarity
	queryTerms := ExtractQueryTerms(rawQuery)
	similarity, similarCall = history.GetSimilarity(tool, queryTerms)

	if similarity >= SemanticDuplicateThreshold {
		return "blocked", similarity, similarCall
	}

	if similarity >= SemanticPenaltyThreshold {
		return "penalized", similarity, similarCall
	}

	return "allowed", similarity, similarCall
}
