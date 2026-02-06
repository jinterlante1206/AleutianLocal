// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

// execute_context.go contains context building and semantic analysis functions
// extracted from execute.go as part of CB-30c Phase 2 decomposition.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Context Building Functions
// -----------------------------------------------------------------------------

// buildCodeContext creates a CodeContext from phase dependencies.
//
// Description:
//
//	Builds a rich CodeContext for history-aware routing. Includes full tool
//	history with summaries to leverage Mamba2's long-context efficiency.
//	The router can see what tools were already called and what they found,
//	enabling it to suggest the NEXT logical tool rather than repeating.
//
// Inputs:
//
//	deps - Phase dependencies.
//
// Outputs:
//
//	*agent.ToolRouterCodeContext - Context for the router.
func (p *ExecutePhase) buildCodeContext(deps *Dependencies) *agent.ToolRouterCodeContext {
	ctx := &agent.ToolRouterCodeContext{}

	// Extract info from assembled context if available
	if deps.Context != nil {
		ctx.Files = len(deps.Context.CodeContext)
		ctx.Symbols = countSymbolsInContext(deps.Context)

		// Detect language from code entries
		ctx.Language = detectLanguageFromContext(deps.Context)
	}

	// Get recent tools from session history if available (legacy support)
	ctx.RecentTools = getRecentToolsFromSession(deps.Session)

	// Get recent tool errors for router feedback
	if deps.Session != nil {
		ctx.PreviousErrors = deps.Session.GetRecentToolErrors()

		// Build tool history with summaries for history-aware routing
		ctx.ToolHistory = buildToolHistoryFromSession(deps.Session)

		// Build progress summary
		ctx.Progress = buildProgressSummary(deps.Session)

		// Set step number
		if deps.EventEmitter != nil {
			ctx.StepNumber = deps.EventEmitter.CurrentStep()
		}
	}

	return ctx
}

// maxToolHistoryEntries limits the number of tool history entries passed to the router.
// This keeps context manageable while still providing sufficient history for
// the router to make informed decisions about the next tool.
const maxToolHistoryEntries = 10

// countToolCalls counts how many times a specific tool appears in the history.
//
// Inputs:
//
//	history - Tool history entries.
//	toolName - The tool name to count.
//
// Outputs:
//
//	int - Number of times the tool was called.
func countToolCalls(history []agent.ToolHistoryEntry, toolName string) int {
	count := 0
	for _, entry := range history {
		if entry.Tool == toolName {
			count++
		}
	}
	return count
}

// -----------------------------------------------------------------------------
// Semantic Repetition Detection
// -----------------------------------------------------------------------------

// semanticRepetitionThreshold is the Jaccard similarity threshold above which
// tool calls are considered semantically repetitive.
// CB-30c: Value of 0.7 means 70% of query terms must overlap.
const semanticRepetitionThreshold = 0.7

// maxSemanticHistorySteps limits how far back we look for semantic repetition.
// Only check the last N steps for performance.
const maxSemanticHistorySteps = 5

// checkSemanticRepetition checks if the proposed tool call is semantically similar
// to recent tool calls with the same tool name.
//
// Description:
//
//	Uses Jaccard similarity on query/pattern parameters to detect when
//	the agent is calling the same tool with slightly different but semantically
//	equivalent queries. This prevents loops like:
//	- Grep("parseConfig") → Grep("parse_config") → Grep("ParseConfig")
//
//	CB-30c: Uses TraceSteps (which have Metadata with actual params) instead of
//	CRS StepRecords (which don't populate ToolParams.Query).
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	deps - Phase dependencies containing session.
//	tool - The tool name being proposed.
//	query - The query string from the current tool call (extracted from params).
//
// Outputs:
//
//	bool - True if this is a semantic repetition.
//	float64 - The maximum similarity score found.
//	string - The query from history that was most similar.
//
// Thread Safety: Safe for concurrent use (reads from session trace).
func (p *ExecutePhase) checkSemanticRepetition(
	ctx context.Context,
	deps *Dependencies,
	tool string,
	query string,
) (bool, float64, string) {
	ctx, span := executePhaseTracer.Start(ctx, "ExecutePhase.checkSemanticRepetition",
		trace.WithAttributes(
			attribute.String("tool", tool),
			attribute.String("query_preview", truncateQuery(query, 50)),
		),
	)
	defer span.End()

	if deps.Session == nil || query == "" {
		return false, 0, ""
	}

	// Get trace steps from session (these have Metadata with actual params)
	steps := deps.Session.GetTraceSteps()
	if len(steps) == 0 {
		return false, 0, ""
	}

	// Check last N steps for same tool with similar query
	maxSimilarity := 0.0
	similarQuery := ""
	startIdx := len(steps) - maxSemanticHistorySteps
	if startIdx < 0 {
		startIdx = 0
	}

	currentTerms := extractQueryTerms(query)
	if len(currentTerms) == 0 {
		return false, 0, ""
	}

	// Query param names to check in Metadata
	queryParamNames := []string{"pattern", "query", "search", "symbol", "name", "path", "target"}

	for i := len(steps) - 1; i >= startIdx; i-- {
		step := steps[i]

		// Only compare same tool (step.Tool contains the tool name)
		if step.Tool != tool {
			continue
		}

		// Extract query from Metadata
		prevQuery := ""
		for _, paramName := range queryParamNames {
			if val, ok := step.Metadata[paramName]; ok && val != "" {
				prevQuery = val
				break
			}
		}

		if prevQuery == "" {
			continue
		}

		prevTerms := extractQueryTerms(prevQuery)
		if len(prevTerms) == 0 {
			continue
		}

		// Calculate Jaccard similarity
		similarity := jaccardSimilarity(currentTerms, prevTerms)

		if similarity > maxSimilarity {
			maxSimilarity = similarity
			similarQuery = prevQuery
		}
	}

	span.SetAttributes(
		attribute.Float64("max_similarity", maxSimilarity),
		attribute.Float64("threshold", semanticRepetitionThreshold),
		attribute.Bool("is_repetitive", maxSimilarity >= semanticRepetitionThreshold),
	)

	if maxSimilarity >= semanticRepetitionThreshold {
		span.AddEvent("semantic_repetition_detected",
			trace.WithAttributes(
				attribute.String("similar_to_query", similarQuery),
			),
		)
		return true, maxSimilarity, similarQuery
	}

	return false, maxSimilarity, ""
}

// extractQueryTerms extracts terms from a query string for Jaccard comparison.
//
// Description:
//
//	Tokenizes the query into lowercase terms, normalizing for comparison.
//	Handles common delimiters like spaces, underscores, and camelCase.
//
// Inputs:
//
//	query - The query string to tokenize.
//
// Outputs:
//
//	map[string]bool - Set of unique lowercase terms.
func extractQueryTerms(query string) map[string]bool {
	terms := make(map[string]bool)

	// Split on common delimiters
	query = strings.ToLower(query)
	query = strings.ReplaceAll(query, "_", " ")
	query = strings.ReplaceAll(query, "-", " ")
	query = strings.ReplaceAll(query, ".", " ")
	query = strings.ReplaceAll(query, "/", " ")

	// Split camelCase: "parseConfig" → "parse config"
	var expanded strings.Builder
	for i, r := range query {
		if i > 0 && r >= 'A' && r <= 'Z' {
			expanded.WriteRune(' ')
		}
		expanded.WriteRune(r)
	}
	query = expanded.String()

	// Extract words
	words := strings.Fields(query)
	for _, word := range words {
		if len(word) >= 2 { // Skip single chars
			terms[word] = true
		}
	}

	return terms
}

// jaccardSimilarity calculates the Jaccard similarity between two term sets.
//
// Description:
//
//	Jaccard = |intersection| / |union|
//	Returns 0.0 if either set is empty, 1.0 if identical.
//
// Inputs:
//
//	a, b - Term sets to compare.
//
// Outputs:
//
//	float64 - Similarity score in range [0.0, 1.0].
func jaccardSimilarity(a, b map[string]bool) float64 {
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

// extractToolQuery extracts the query/pattern parameter from a tool invocation.
//
// Description:
//
//	Different tools use different parameter names for their "query" concept.
//	This function extracts the relevant parameter for semantic comparison.
//
// Inputs:
//
//	inv - The tool invocation to extract from.
//
// Outputs:
//
//	string - The query/pattern string, or empty if not found.
func extractToolQuery(inv *agent.ToolInvocation) string {
	if inv == nil || inv.Parameters == nil {
		return ""
	}

	// Common query parameter names across tools
	queryParamNames := []string{"pattern", "query", "search", "symbol", "name", "path", "target"}

	// Check StringParams first
	if inv.Parameters.StringParams != nil {
		for _, name := range queryParamNames {
			if val, ok := inv.Parameters.StringParams[name]; ok && val != "" {
				return val
			}
		}
	}

	// Fallback: try to parse from RawJSON
	if len(inv.Parameters.RawJSON) > 0 {
		var rawParams map[string]interface{}
		if err := json.Unmarshal(inv.Parameters.RawJSON, &rawParams); err == nil {
			for _, name := range queryParamNames {
				if val, ok := rawParams[name]; ok {
					if strVal, isStr := val.(string); isStr && strVal != "" {
						return strVal
					}
				}
			}
		}
	}

	return ""
}

// -----------------------------------------------------------------------------
// Token Estimation
// -----------------------------------------------------------------------------

// estimateToolResultTokens estimates token count for tool output.
//
// Description:
//
//	Uses tiered estimation based on content type, as per CB-30c review:
//	- JSON is denser (~3.5 chars/token)
//	- Code is sparser (~5 chars/token)
//	- Default prose (~4 chars/token)
//
// Inputs:
//
//	result - The tool output as a string.
//
// Outputs:
//
//	int - Estimated token count.
func estimateToolResultTokens(result string) int {
	if len(result) == 0 {
		return 0
	}

	trimmed := strings.TrimSpace(result)

	// JSON is denser (~3.5 chars/token)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return len(result) * 10 / 35
	}

	// Code is sparser (~5 chars/token due to whitespace and indentation)
	if strings.Contains(result, "func ") || strings.Contains(result, "def ") ||
		strings.Contains(result, "class ") || strings.Contains(result, "package ") {
		return len(result) / 5
	}

	// Default prose (~4 chars/token)
	return len(result) / 4
}

// -----------------------------------------------------------------------------
// Error Categorization
// -----------------------------------------------------------------------------

// categorizeToolError maps error messages to ErrorCategory for CDCL learning.
//
// Description:
//
//	Analyzes error messages to categorize them for clause generation.
//	This enables more specific clauses that block tool+error combinations.
//
// Inputs:
//
//	errMsg - The error message from tool execution.
//
// Outputs:
//
//	crs.ErrorCategory - The error category.
func categorizeToolError(errMsg string) crs.ErrorCategory {
	errLower := strings.ToLower(errMsg)

	switch {
	case strings.Contains(errLower, "not found") ||
		strings.Contains(errLower, "no such file") ||
		strings.Contains(errLower, "does not exist") ||
		strings.Contains(errLower, "enoent"): // CR-8: Unix errno
		return crs.ErrorCategoryToolNotFound

	case strings.Contains(errLower, "invalid param") ||
		strings.Contains(errLower, "invalid argument") ||
		strings.Contains(errLower, "missing required") ||
		strings.Contains(errLower, "einval"): // CR-8: Unix errno
		return crs.ErrorCategoryInvalidParams

	case strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "timed out") ||
		strings.Contains(errLower, "deadline") ||
		strings.Contains(errLower, "i/o timeout") || // CR-8: Go net timeout
		strings.Contains(errLower, "context deadline"): // CR-8: Context timeout
		return crs.ErrorCategoryTimeout

	case strings.Contains(errLower, "rate limit") ||
		strings.Contains(errLower, "too many requests") ||
		strings.Contains(errLower, "429"): // CR-8: HTTP status
		return crs.ErrorCategoryRateLimited

	case strings.Contains(errLower, "permission") ||
		strings.Contains(errLower, "access denied") ||
		strings.Contains(errLower, "forbidden") ||
		strings.Contains(errLower, "eperm") || // CR-8: Unix errno
		strings.Contains(errLower, "eacces"): // CR-8: Unix errno
		return crs.ErrorCategoryPermission

	case strings.Contains(errLower, "network") ||
		strings.Contains(errLower, "connection") ||
		strings.Contains(errLower, "eof") || // CR-8: Unexpected EOF
		strings.Contains(errLower, "broken pipe") || // CR-8: Unix
		strings.Contains(errLower, "reset by peer"): // CR-8: TCP reset
		return crs.ErrorCategoryNetwork

	default:
		return crs.ErrorCategoryInternal
	}
}

// -----------------------------------------------------------------------------
// Tool History Building
// -----------------------------------------------------------------------------

// buildToolHistoryFromSession extracts tool history with summaries from session.
//
// Description:
//
//	Iterates through the session's trace steps and builds a history of
//	tool calls with brief summaries of what each tool found. This enables
//	history-aware routing where the router can see what was already tried.
//
// Inputs:
//
//	s - The session to extract history from.
//
// Outputs:
//
//	[]agent.ToolHistoryEntry - Tool history with summaries.
func buildToolHistoryFromSession(s *agent.Session) []agent.ToolHistoryEntry {
	if s == nil {
		return nil
	}

	traceSteps := s.GetTraceSteps()
	if len(traceSteps) == 0 {
		return nil
	}

	var history []agent.ToolHistoryEntry
	stepNum := 0

	for _, step := range traceSteps {
		// Include both tool_call and tool_call_forced actions.
		// CB-31d fix: tool_call_forced was not being counted, so circuit breaker
		// never detected repeated forced tool calls from the router.
		if step.Action != "tool_call" && step.Action != "tool_call_forced" {
			continue
		}

		stepNum++
		entry := agent.ToolHistoryEntry{
			Tool:       step.Tool,
			Success:    step.Error == "",
			StepNumber: stepNum,
		}

		// Build summary based on tool type and results
		entry.Summary = buildToolSummary(step)

		history = append(history, entry)
	}

	// Limit to last N entries to keep context manageable
	if len(history) > maxToolHistoryEntries {
		history = history[len(history)-maxToolHistoryEntries:]
	}

	return history
}

// buildToolSummary creates a brief summary of what a tool call found.
//
// Inputs:
//
//	step - The trace step for the tool call.
//
// Outputs:
//
//	string - Brief summary of the result.
func buildToolSummary(step crs.TraceStep) string {
	if step.Error != "" {
		return "FAILED: " + truncateString(step.Error, 50)
	}

	// Extract summary from metadata if available
	if summary, ok := step.Metadata["summary"]; ok && summary != "" {
		return truncateString(summary, 100)
	}

	// Build summary based on symbols found
	if len(step.SymbolsFound) > 0 {
		return fmt.Sprintf("Found %d symbols", len(step.SymbolsFound))
	}

	// Default to a generic success message with target
	if step.Target != "" {
		return "Processed " + truncateString(step.Target, 50)
	}

	return "Completed successfully"
}

// buildProgressSummary creates a summary of current progress.
//
// Inputs:
//
//	s - The session to summarize.
//
// Outputs:
//
//	string - Progress summary.
func buildProgressSummary(s *agent.Session) string {
	if s == nil {
		return ""
	}

	traceSteps := s.GetTraceSteps()
	if len(traceSteps) == 0 {
		return "No tools called yet"
	}

	// Count tools by category
	toolCounts := make(map[string]int)
	toolOrder := make([]string, 0) // Track insertion order for deterministic output
	totalSymbols := 0

	for _, step := range traceSteps {
		if step.Action == "tool_call" && step.Error == "" {
			if toolCounts[step.Tool] == 0 {
				toolOrder = append(toolOrder, step.Tool)
			}
			toolCounts[step.Tool]++
			totalSymbols += len(step.SymbolsFound)
		}
	}

	// Build summary in deterministic order
	var parts []string
	for _, tool := range toolOrder {
		parts = append(parts, fmt.Sprintf("%s(%d)", tool, toolCounts[tool]))
	}

	summary := fmt.Sprintf("Tools used: %s", strings.Join(parts, ", "))
	if totalSymbols > 0 {
		summary += fmt.Sprintf("; %d symbols found", totalSymbols)
	}

	return summary
}

// -----------------------------------------------------------------------------
// Context Analysis Helpers
// -----------------------------------------------------------------------------

// countSymbolsInContext counts unique symbols referenced in the context.
func countSymbolsInContext(ctx *agent.AssembledContext) int {
	if ctx == nil {
		return 0
	}
	// Count code entries as a proxy for symbols
	return len(ctx.CodeContext)
}

// detectLanguageFromContext attempts to detect the primary language from context.
func detectLanguageFromContext(ctx *agent.AssembledContext) string {
	if ctx == nil || len(ctx.CodeContext) == 0 {
		return ""
	}

	// Simple heuristic: look at file extensions in the code context
	goCount, pyCount := 0, 0
	for _, entry := range ctx.CodeContext {
		if strings.HasSuffix(entry.FilePath, ".go") {
			goCount++
		} else if strings.HasSuffix(entry.FilePath, ".py") {
			pyCount++
		}
	}

	if goCount > pyCount {
		return "go"
	} else if pyCount > goCount {
		return "python"
	}
	return ""
}

// getRecentToolsFromSession extracts recent tool names from session history.
func getRecentToolsFromSession(s *agent.Session) []string {
	if s == nil {
		return nil
	}

	history := s.GetHistory()
	if len(history) == 0 {
		return nil
	}

	// Get last 5 unique tools
	seen := make(map[string]bool)
	var recent []string
	for i := len(history) - 1; i >= 0 && len(recent) < 5; i-- {
		entry := history[i]
		if entry.Type == "tool_call" && entry.ToolName != "" && !seen[entry.ToolName] {
			seen[entry.ToolName] = true
			recent = append(recent, entry.ToolName)
		}
	}
	return recent
}
