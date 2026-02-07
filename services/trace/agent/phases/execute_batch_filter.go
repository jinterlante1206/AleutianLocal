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

// execute_batch_filter.go implements GR-39a: Router-assisted semantic
// deduplication for parallel tool call batches.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// BatchFilterer Interface
// -----------------------------------------------------------------------------

// BatchFilterer is an interface for components that can filter tool call batches.
//
// Description:
//
//	Components implementing this interface can classify/filter tool calls
//	using a fast model. The primary implementation is the router model
//	(granite4:micro-h) which is optimized for fast classification.
//
// Thread Safety: Implementations must be safe for concurrent use.
type BatchFilterer interface {
	// FilterBatch evaluates a batch of tool calls and returns filter decisions.
	//
	// Inputs:
	//   ctx - Context for cancellation/timeout.
	//   prompt - The filter prompt containing tools and similarity scores.
	//
	// Outputs:
	//   string - Response with KEEP/SKIP decisions.
	//   error - Non-nil if the filter call fails.
	FilterBatch(ctx context.Context, prompt string) (string, error)
}

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

// batchFilterMinSize is the minimum batch size to trigger router filtering.
// Batches of 1-2 tools are executed directly without filtering overhead.
const batchFilterMinSize = 3

// batchFilterTimeout is the maximum time to wait for router response.
const batchFilterTimeout = 2 * time.Second

// maxHistoryStepsInPrompt limits history items shown in filter prompt.
// Prevents prompt from growing too large.
const maxHistoryStepsInPrompt = 10

// batchFilterPromptEstimatedSize is the estimated prompt size for Grow().
const batchFilterPromptEstimatedSize = 2048

// batchFilterSimilarityThreshold is the minimum similarity to show in prompt.
const batchFilterSimilarityThreshold = 0.3

// -----------------------------------------------------------------------------
// Compiled Patterns (R-3 fix: compile once at package level)
// -----------------------------------------------------------------------------

// filterResponsePattern matches "N:KEEP" or "N:SKIP" in router responses.
var filterResponsePattern = regexp.MustCompile(`(\d+)\s*:\s*(KEEP|SKIP)`)

// -----------------------------------------------------------------------------
// Prometheus Metrics (L-2 fix: actually use the defined metrics)
// -----------------------------------------------------------------------------

var (
	batchFilterTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "trace_batch_filter_total",
		Help: "Total batch filter operations by result",
	}, []string{"result"}) // "filtered", "passthrough", "error", "timeout"

	batchFilterSkipped = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_batch_filter_skipped",
		Help:    "Number of tool calls skipped per batch",
		Buckets: []float64{0, 1, 2, 3, 5, 10},
	})

	batchFilterDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "trace_batch_filter_duration_seconds",
		Help:    "Time spent in batch filtering",
		Buckets: []float64{0.05, 0.1, 0.2, 0.5, 1.0, 2.0},
	})

	batchFilterRouterCalls = promauto.NewCounter(prometheus.CounterOpts{
		Name: "trace_batch_filter_router_calls_total",
		Help: "Total router calls for batch filtering",
	})
)

// -----------------------------------------------------------------------------
// Main Filter Function
// -----------------------------------------------------------------------------

// filterBatchWithRouter uses the router model to filter redundant tool calls.
//
// Description:
//
//	Given a batch of tool calls from the main LLM, this function:
//	1. Computes Jaccard similarity scores against history and batch siblings
//	2. Sends the batch + scores to the router for filtering
//	3. Returns only the tool calls the router marked as KEEP
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	deps - Phase dependencies. Router and Session may be nil.
//	invocations - Tool calls to filter. May be empty.
//
// Outputs:
//
//	[]agent.ToolInvocation - Filtered tool calls. Never nil.
//	error - Non-nil only on fatal errors. On timeout/parse failures,
//	        returns original batch with nil error (graceful degradation).
//
// Example:
//
//	filtered, err := p.filterBatchWithRouter(ctx, deps, invocations)
//	if err != nil {
//	    return nil, fmt.Errorf("batch filter: %w", err)
//	}
//	// Use filtered batch
//
// Limitations:
//
//   - Adds 150-200ms latency for router call
//   - Only filters batches of 3+ tools
//   - Router decisions may not always be optimal
//
// Assumptions:
//
//   - Router model (granite4:micro-h) is fast enough for 2s timeout
//   - Jaccard similarity is sufficient for semantic comparison
//   - Caller will execute all returned invocations
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) filterBatchWithRouter(
	ctx context.Context,
	deps *Dependencies,
	invocations []agent.ToolInvocation,
) ([]agent.ToolInvocation, error) {
	// A-1 fix: Validate context
	if ctx == nil {
		return invocations, errors.New("ctx must not be nil")
	}

	// Start span with session_id (L-3 fix)
	sessionID := ""
	if deps != nil && deps.Session != nil {
		sessionID = deps.Session.ID
	}

	ctx, span := executePhaseTracer.Start(ctx, "ExecutePhase.filterBatchWithRouter",
		trace.WithAttributes(
			attribute.Int("batch_size", len(invocations)),
			attribute.String("session_id", sessionID),
		),
	)
	defer span.End()

	// Get context-aware logger (L-1 fix)
	logger := loggerWithTrace(ctx)

	// Skip small batches - no filtering needed
	if len(invocations) < batchFilterMinSize {
		batchFilterTotal.WithLabelValues("passthrough").Inc()
		span.SetAttributes(attribute.String("skip_reason", "batch_too_small"))
		return invocations, nil
	}

	// Get batch filterer from session's router (if available)
	var filterer BatchFilterer
	if deps != nil && deps.Session != nil {
		router := deps.Session.GetToolRouter()
		if bf, ok := router.(BatchFilterer); ok {
			filterer = bf
		}
	}

	// Skip if no filterer available
	if filterer == nil {
		batchFilterTotal.WithLabelValues("passthrough").Inc()
		span.SetAttributes(attribute.String("skip_reason", "no_filterer"))
		return invocations, nil
	}

	// Get history for similarity comparison
	var history []crs.TraceStep
	if deps.Session != nil {
		history = deps.Session.GetTraceSteps()
	}

	// Build prompt with Jaccard scores (O-1 fix: pre-sized builder)
	prompt := p.buildBatchFilterPrompt(deps.Query, invocations, history)

	// Call router with timeout
	filterCtx, cancel := context.WithTimeout(ctx, batchFilterTimeout)
	defer cancel()

	start := time.Now()
	batchFilterRouterCalls.Inc()

	response, err := filterer.FilterBatch(filterCtx, prompt)
	duration := time.Since(start)

	// Record duration metric
	batchFilterDuration.Observe(duration.Seconds())

	span.SetAttributes(
		attribute.Float64("router_duration_ms", float64(duration.Milliseconds())),
	)

	// R-2 fix: Graceful degradation on timeout/error
	if err != nil {
		span.SetAttributes(attribute.Bool("router_error", true))
		span.RecordError(err)

		if errors.Is(err, context.DeadlineExceeded) {
			batchFilterTotal.WithLabelValues("timeout").Inc()
			logger.Warn("GR-39a: Router timeout, using original batch",
				slog.Duration("timeout", batchFilterTimeout),
				slog.Int("batch_size", len(invocations)),
			)
			// Return original batch on timeout - graceful degradation
			return invocations, nil
		}

		batchFilterTotal.WithLabelValues("error").Inc()
		logger.Warn("GR-39a: Router call failed, using original batch",
			slog.String("error", err.Error()),
			slog.Int("batch_size", len(invocations)),
		)
		// Return original batch on error - graceful degradation
		return invocations, nil
	}

	// Parse response to get keep/skip decisions
	filtered := p.parseFilterResponse(ctx, response, invocations)

	// Record metrics
	skipped := len(invocations) - len(filtered)
	batchFilterTotal.WithLabelValues("filtered").Inc()
	batchFilterSkipped.Observe(float64(skipped))

	span.SetAttributes(
		attribute.Int("kept", len(filtered)),
		attribute.Int("skipped", skipped),
	)

	// Record in trace (R-1 fix: check Session before accessing)
	if deps.Session != nil {
		deps.Session.RecordTraceStep(crs.TraceStep{
			Action:   "batch_filter",
			Tool:     "router",
			Duration: duration,
			Metadata: map[string]string{
				"original_count": fmt.Sprintf("%d", len(invocations)),
				"filtered_count": fmt.Sprintf("%d", len(filtered)),
				"skipped_count":  fmt.Sprintf("%d", skipped),
			},
		})

		// C-2 fix: Learn from skipped tools for CDCL
		if skipped > 0 {
			p.learnFromBatchFilter(ctx, deps, invocations, filtered)
		}

		// Log with session context (R-1 fix: inside nil check)
		logger.Info("GR-39a: Router filtered batch",
			slog.String("session_id", deps.Session.ID),
			slog.Int("original", len(invocations)),
			slog.Int("filtered", len(filtered)),
			slog.Int("skipped", skipped),
			slog.Duration("duration", duration),
		)
	}

	return filtered, nil
}

// -----------------------------------------------------------------------------
// Prompt Building
// -----------------------------------------------------------------------------

// buildBatchFilterPrompt constructs the prompt for the router.
//
// Description:
//
//	Builds a structured prompt showing the user query, execution history,
//	and pending tool calls with their Jaccard similarity scores.
//
// Inputs:
//
//	query - The user's original query.
//	invocations - Tool calls to evaluate.
//	history - Previous execution history for similarity comparison.
//
// Outputs:
//
//	string - The complete prompt for the router model.
//
// Thread Safety: Safe for concurrent use (stateless).
func (p *ExecutePhase) buildBatchFilterPrompt(
	query string,
	invocations []agent.ToolInvocation,
	history []crs.TraceStep,
) string {
	// O-1 fix: Pre-size the string builder
	var sb strings.Builder
	sb.Grow(batchFilterPromptEstimatedSize)

	sb.WriteString("Filter these tool calls for efficiency.\n\n")
	sb.WriteString("Query: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	// Show execution history (A-2 fix: use named constant)
	sb.WriteString("Already executed:\n")
	historyCount := 0
	for _, step := range history {
		if step.Action == "tool_call" && historyCount < maxHistoryStepsInPrompt {
			toolQuery := extractHistoryToolQuery(&step)
			if toolQuery != "" {
				sb.WriteString(fmt.Sprintf("- %s(%s)\n", step.Tool, toolQuery))
				historyCount++
			}
		}
	}
	if historyCount == 0 {
		sb.WriteString("- (none)\n")
	}

	// O-2 fix: Pre-compute terms for current batch to avoid repeated extraction
	batchTerms := make([]map[string]bool, len(invocations))
	batchQueries := make([]string, len(invocations))
	for i := range invocations {
		batchQueries[i] = extractToolQuery(&invocations[i])
		batchTerms[i] = extractQueryTerms(batchQueries[i])
	}

	// Show pending calls with similarity analysis
	sb.WriteString("\nPending tool calls:\n")
	for i := range invocations {
		inv := &invocations[i]
		toolQuery := batchQueries[i]

		// Compute similarities using pre-extracted terms
		histSim, histMatch := p.computeHistorySimilarityWithTerms(
			inv.Tool, toolQuery, batchTerms[i], history)
		batchSim, batchMatch := p.computeBatchSimilarityWithTerms(
			i, inv.Tool, batchTerms[i], batchQueries[:i], batchTerms[:i], invocations[:i])

		sb.WriteString(fmt.Sprintf("%d. %s(%s)\n", i+1, inv.Tool, toolQuery))

		if histSim >= batchFilterSimilarityThreshold {
			sb.WriteString(fmt.Sprintf("   [%.0f%% similar to executed: %s]\n",
				histSim*100, histMatch))
		}
		if batchSim >= batchFilterSimilarityThreshold {
			sb.WriteString(fmt.Sprintf("   [%.0f%% similar to #%s in batch]\n",
				batchSim*100, batchMatch))
		}
	}

	sb.WriteString("\nRespond with KEEP or SKIP for each:\n")
	sb.WriteString("- KEEP: Needed to answer the query\n")
	sb.WriteString("- SKIP: Duplicate, redundant, or excessive exploration\n")
	sb.WriteString("\nFormat: 1:KEEP 2:SKIP 3:KEEP ...\n")

	return sb.String()
}

// -----------------------------------------------------------------------------
// Similarity Computation
// -----------------------------------------------------------------------------

// computeHistorySimilarityWithTerms finds max Jaccard similarity against history.
//
// Description:
//
//	Compares a tool query against the execution history to find duplicates
//	or semantically similar previous calls. Uses pre-extracted terms for
//	efficiency when processing multiple invocations.
//
// Inputs:
//
//	tool - The tool name to match.
//	query - The query string being evaluated.
//	currentTerms - Pre-extracted terms from query.
//	history - Execution history to compare against.
//
// Outputs:
//
//	float64 - Maximum similarity score found (0.0 to 1.0).
//	string - The matching query from history.
//
// Thread Safety: Safe for concurrent use (reads only).
func (p *ExecutePhase) computeHistorySimilarityWithTerms(
	tool string,
	query string,
	currentTerms map[string]bool,
	history []crs.TraceStep,
) (float64, string) {
	if query == "" || len(currentTerms) == 0 {
		return 0, ""
	}

	maxSim := 0.0
	match := ""

	for _, step := range history {
		if step.Tool != tool {
			continue
		}

		prevQuery := extractHistoryToolQuery(&step)
		if prevQuery == "" {
			continue
		}

		// Exact match fast path
		if strings.EqualFold(query, prevQuery) {
			return 1.0, prevQuery
		}

		prevTerms := extractQueryTerms(prevQuery)
		sim := jaccardSimilarity(currentTerms, prevTerms)
		if sim > maxSim {
			maxSim = sim
			match = prevQuery
		}
	}

	return maxSim, match
}

// computeBatchSimilarityWithTerms finds max similarity against earlier batch items.
//
// Description:
//
//	Compares a tool invocation against earlier items in the same batch
//	to identify redundant calls. Uses pre-extracted terms for efficiency.
//
// Inputs:
//
//	idx - Current invocation index.
//	tool - The tool name to match.
//	currentTerms - Pre-extracted terms from current query.
//	earlierQueries - Queries from earlier batch items.
//	earlierTerms - Pre-extracted terms from earlier items.
//	earlier - Earlier invocations to compare against.
//
// Outputs:
//
//	float64 - Maximum similarity score found (0.0 to 1.0).
//	string - The 1-indexed position of the matching item.
//
// Thread Safety: Safe for concurrent use (reads only).
func (p *ExecutePhase) computeBatchSimilarityWithTerms(
	idx int,
	tool string,
	currentTerms map[string]bool,
	earlierQueries []string,
	earlierTerms []map[string]bool,
	earlier []agent.ToolInvocation,
) (float64, string) {
	if len(currentTerms) == 0 {
		return 0, ""
	}

	maxSim := 0.0
	match := ""
	currentQuery := ""
	if idx < len(earlierQueries) {
		// This shouldn't happen, but defensive
		currentQuery = earlierQueries[idx]
	}

	for i := range earlier {
		if earlier[i].Tool != tool {
			continue
		}

		prevQuery := earlierQueries[i]
		if prevQuery == "" {
			continue
		}

		// Exact match fast path
		if currentQuery != "" && strings.EqualFold(currentQuery, prevQuery) {
			return 1.0, fmt.Sprintf("%d", i+1)
		}

		sim := jaccardSimilarity(currentTerms, earlierTerms[i])
		if sim > maxSim {
			maxSim = sim
			match = fmt.Sprintf("%d", i+1)
		}
	}

	return maxSim, match
}

// -----------------------------------------------------------------------------
// Response Parsing
// -----------------------------------------------------------------------------

// parseFilterResponse extracts keep/skip decisions from router response.
//
// Description:
//
//	Parses the router's response to determine which tool calls to keep.
//	Supports multiple formats:
//	- "1:KEEP 2:SKIP 3:KEEP" (standard)
//	- "KEEP SKIP KEEP" (fallback)
//	Returns original batch if parsing fails.
//
// Inputs:
//
//	ctx - Context for logging with trace.
//	response - Raw response from router.
//	invocations - Original tool invocations.
//
// Outputs:
//
//	[]agent.ToolInvocation - Filtered invocations. Never empty if input non-empty.
//
// Thread Safety: Safe for concurrent use (stateless).
func (p *ExecutePhase) parseFilterResponse(
	ctx context.Context,
	response string,
	invocations []agent.ToolInvocation,
) []agent.ToolInvocation {
	logger := loggerWithTrace(ctx)

	// R-3 fix: Use pre-compiled pattern
	matches := filterResponsePattern.FindAllStringSubmatch(strings.ToUpper(response), -1)

	if len(matches) == 0 {
		// Fallback: try to find just KEEP/SKIP in order
		words := strings.Fields(strings.ToUpper(response))
		decisions := make([]bool, len(invocations))
		idx := 0
		for _, w := range words {
			if idx >= len(invocations) {
				break
			}
			if strings.Contains(w, "KEEP") {
				decisions[idx] = true
				idx++
			} else if strings.Contains(w, "SKIP") {
				decisions[idx] = false
				idx++
			}
		}

		// If we got decisions for all, use them
		if idx == len(invocations) {
			result := make([]agent.ToolInvocation, 0, len(invocations))
			for i, keep := range decisions {
				if keep {
					result = append(result, invocations[i])
				}
			}
			if len(result) > 0 {
				return result
			}
		}

		// L-4 fix: Use Debug level for parse noise, increment metric
		logger.Debug("GR-39a: Could not parse router response, keeping all",
			slog.String("response", truncateQuery(response, 100)),
		)
		batchFilterTotal.WithLabelValues("parse_fallback").Inc()
		return invocations
	}

	// Build keep set from matches
	keepSet := make(map[int]bool)
	for _, m := range matches {
		var idx int
		fmt.Sscanf(m[1], "%d", &idx)
		if m[2] == "KEEP" {
			keepSet[idx] = true
		}
	}

	// Filter invocations
	result := make([]agent.ToolInvocation, 0, len(invocations))
	for i, inv := range invocations {
		if keepSet[i+1] { // 1-indexed in prompt
			result = append(result, inv)
		}
	}

	// Safety: if router skipped everything, keep at least the first
	if len(result) == 0 && len(invocations) > 0 {
		logger.Warn("GR-39a: Router skipped all tools, keeping first",
			slog.Int("original_count", len(invocations)),
		)
		result = append(result, invocations[0])
	}

	return result
}

// -----------------------------------------------------------------------------
// CRS Learning Integration
// -----------------------------------------------------------------------------

// learnFromBatchFilter records CDCL learning data for skipped tools.
//
// Description:
//
//	When tools are skipped by the router, this function records the
//	decision for CDCL clause learning. This helps the system learn
//	which tool call patterns are typically redundant.
//
// Inputs:
//
//	ctx - Context for tracing.
//	deps - Phase dependencies with Session.
//	original - All original invocations.
//	filtered - The filtered (kept) invocations.
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) learnFromBatchFilter(
	ctx context.Context,
	deps *Dependencies,
	original []agent.ToolInvocation,
	filtered []agent.ToolInvocation,
) {
	if deps.Session == nil {
		return
	}

	// Build set of kept tool+query combinations
	keptSet := make(map[string]bool)
	for _, inv := range filtered {
		key := inv.Tool + ":" + extractToolQuery(&inv)
		keptSet[key] = true
	}

	// Record skipped tools for learning
	for _, inv := range original {
		key := inv.Tool + ":" + extractToolQuery(&inv)
		if !keptSet[key] {
			// This tool was skipped - record for CDCL
			// Use SignalSourceSoft since router is LLM-based
			p.learnFromFailure(ctx, deps, crs.FailureEvent{
				SessionID:   deps.Session.ID,
				FailureType: crs.FailureTypeBatchFiltered,
				Tool:        inv.Tool,
				Source:      crs.SignalSourceSoft,
			})
		}
	}
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// extractHistoryToolQuery extracts the query parameter from a history step.
//
// Description:
//
//	Different tools store their query in different metadata keys.
//	This function tries common keys in order of likelihood.
//
// Inputs:
//
//	step - The trace step to extract from.
//
// Outputs:
//
//	string - The extracted query, or empty string if not found.
//
// Thread Safety: Safe for concurrent use (reads only).
func extractHistoryToolQuery(step *crs.TraceStep) string {
	if step == nil || step.Metadata == nil {
		return ""
	}

	// GR-39a: Use shared queryParamNames for consistent deduplication
	// This ensures explore_package, find_implementers, etc. are properly tracked
	for _, key := range queryParamNames {
		if v, ok := step.Metadata[key]; ok && v != "" {
			return v
		}
	}
	return ""
}

// loggerWithTrace returns a logger with trace context from ctx.
//
// Description:
//
//	Extracts trace_id and span_id from the context and adds them
//	to the logger for correlation with distributed traces.
//
// Inputs:
//
//	ctx - Context containing trace information.
//
// Outputs:
//
//	*slog.Logger - Logger with trace attributes.
//
// Thread Safety: Safe for concurrent use.
func loggerWithTrace(ctx context.Context) *slog.Logger {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return slog.Default()
	}
	return slog.Default().With(
		slog.String("trace_id", spanCtx.TraceID().String()),
		slog.String("span_id", spanCtx.SpanID().String()),
	)
}

// -----------------------------------------------------------------------------
// Legacy Compatibility Functions
// -----------------------------------------------------------------------------

// computeHistorySimilarity is the legacy version without pre-extracted terms.
// Deprecated: Use computeHistorySimilarityWithTerms for better performance.
func (p *ExecutePhase) computeHistorySimilarity(
	tool string,
	query string,
	history []crs.TraceStep,
) (float64, string) {
	return p.computeHistorySimilarityWithTerms(tool, query, extractQueryTerms(query), history)
}

// computeBatchSimilarity is the legacy version without pre-extracted terms.
// Deprecated: Use computeBatchSimilarityWithTerms for better performance.
func (p *ExecutePhase) computeBatchSimilarity(
	inv *agent.ToolInvocation,
	earlier []agent.ToolInvocation,
) (float64, string) {
	query := extractToolQuery(inv)
	currentTerms := extractQueryTerms(query)

	// Build terms for earlier invocations
	earlierQueries := make([]string, len(earlier))
	earlierTerms := make([]map[string]bool, len(earlier))
	for i := range earlier {
		earlierQueries[i] = extractToolQuery(&earlier[i])
		earlierTerms[i] = extractQueryTerms(earlierQueries[i])
	}

	return p.computeBatchSimilarityWithTerms(
		len(earlier), inv.Tool, currentTerms, earlierQueries, earlierTerms, earlier)
}
