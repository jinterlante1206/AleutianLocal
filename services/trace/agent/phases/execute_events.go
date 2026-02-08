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

// execute_events.go contains event emission functions extracted from execute.go
// as part of CB-30c Phase 2 decomposition.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/events"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/integration"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
)

// -----------------------------------------------------------------------------
// Standard Event Emission
// -----------------------------------------------------------------------------

// SemanticInfo holds semantic similarity information for trace step metadata.
// O1.3 Fix: Track semantic similarity in trace steps.
type SemanticInfo struct {
	Similarity float64
	Status     string // "allowed", "penalized", or "blocked"
}

// emitToolRouting emits a routing event and records a trace step.
// O1.3 Fix: Accepts optional semantic info to include in trace step metadata.
func (p *ExecutePhase) emitToolRouting(deps *Dependencies, selection *agent.ToolRouterSelection, semanticInfo ...SemanticInfo) {
	if selection == nil {
		return
	}

	// Emit event if emitter is available
	if deps.EventEmitter != nil {
		deps.EventEmitter.Emit(events.TypeToolForcing, &events.ToolForcingData{
			Query:         deps.Query,
			SuggestedTool: selection.Tool,
			RetryCount:    0,
			MaxRetries:    0,
			StepNumber:    0,
			Reason:        fmt.Sprintf("router_selection (confidence: %.2f, reasoning: %s)", selection.Confidence, selection.Reasoning),
		})
	}

	// Record trace step for routing decision
	if deps.Session != nil {
		traceStep := crs.TraceStep{
			Action:   "tool_routing",
			Target:   selection.Tool,
			Duration: selection.Duration,
			Metadata: map[string]string{
				"confidence": fmt.Sprintf("%.2f", selection.Confidence),
				"reasoning":  selection.Reasoning,
				"query":      truncateQuery(deps.Query, 200),
			},
		}

		// O1.3 Fix: Add semantic similarity to metadata if provided
		if len(semanticInfo) > 0 {
			traceStep.Metadata["semantic_similarity"] = fmt.Sprintf("%.3f", semanticInfo[0].Similarity)
			traceStep.Metadata["semantic_status"] = semanticInfo[0].Status
		}

		deps.Session.RecordTraceStep(traceStep)
	}
}

// emitToolForcing emits a tool forcing event.
func (p *ExecutePhase) emitToolForcing(deps *Dependencies, req *ForcingRequest, hint string, stepNumber int) {
	if deps.EventEmitter == nil {
		return
	}

	// Extract suggested tool from hint (best effort)
	suggestedTool := ""
	if req != nil && len(req.AvailableTools) > 0 {
		// Get suggestion from classifier
		if p.forcingPolicy != nil {
			if dfp, ok := p.forcingPolicy.(*DefaultForcingPolicy); ok {
				suggestedTool, _ = dfp.classifier.SuggestTool(context.Background(), req.Query, req.AvailableTools)
			}
		}
	}

	deps.EventEmitter.Emit(events.TypeToolForcing, &events.ToolForcingData{
		Query:         req.Query,
		SuggestedTool: suggestedTool,
		RetryCount:    req.ForcingRetries + 1,
		MaxRetries:    req.MaxRetries,
		StepNumber:    stepNumber,
		Reason:        "analytical_query_without_tools",
	})
}

// emitLLMRequest emits an LLM request event.
func (p *ExecutePhase) emitLLMRequest(deps *Dependencies, request *llm.Request) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeLLMRequest, &events.LLMRequestData{
		Model:     deps.LLMClient.Model(),
		TokensIn:  request.MaxTokens, // Approximation
		HasTools:  len(request.Tools) > 0,
		ToolCount: len(request.Tools),
	})
}

// emitLLMResponse emits an LLM response event.
func (p *ExecutePhase) emitLLMResponse(deps *Dependencies, response *llm.Response) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeLLMResponse, &events.LLMResponseData{
		Model:         response.Model,
		TokensOut:     response.OutputTokens,
		Duration:      response.Duration,
		StopReason:    response.StopReason,
		HasToolCalls:  response.HasToolCalls(),
		ToolCallCount: len(response.ToolCalls),
	})
}

// emitToolInvocation emits a tool invocation event.
func (p *ExecutePhase) emitToolInvocation(deps *Dependencies, inv *agent.ToolInvocation) {
	if deps.EventEmitter == nil {
		return
	}

	// Convert ToolParameters to event parameters
	deps.EventEmitter.Emit(events.TypeToolInvocation, &events.ToolInvocationData{
		ToolName:     inv.Tool,
		InvocationID: inv.ID,
		Parameters:   toolParamsToEventParams(inv.Parameters),
	})
}

// toolParamsToEventParams converts agent.ToolParameters to events.ToolInvocationParameters.
func toolParamsToEventParams(params *agent.ToolParameters) *events.ToolInvocationParameters {
	if params == nil {
		return nil
	}
	return &events.ToolInvocationParameters{
		StringParams: params.StringParams,
		IntParams:    params.IntParams,
		BoolParams:   params.BoolParams,
		RawJSON:      params.RawJSON,
	}
}

// emitToolResult emits a tool result event.
func (p *ExecutePhase) emitToolResult(deps *Dependencies, inv *agent.ToolInvocation, result *tools.Result) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeToolResult, &events.ToolResultData{
		ToolName:     inv.Tool,
		InvocationID: inv.ID,
		Success:      result.Success,
		Duration:     result.Duration,
		TokensUsed:   result.TokensUsed,
		Cached:       result.Cached,
		Error:        result.Error,
	})
}

// emitSafetyCheck emits a safety check event.
func (p *ExecutePhase) emitSafetyCheck(deps *Dependencies, result *safety.Result) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeSafetyCheck, &events.SafetyCheckData{
		ChangesChecked: result.ChecksRun,
		Passed:         result.Passed,
		CriticalCount:  result.CriticalCount,
		WarningCount:   result.WarningCount,
		Blocked:        !result.Passed,
	})
}

// emitStepComplete emits a step complete event.
func (p *ExecutePhase) emitStepComplete(deps *Dependencies, stepStart time.Time, stepNumber, toolsInvoked int) {
	if deps.EventEmitter == nil {
		return
	}

	tokensUsed := 0
	if deps.Context != nil {
		tokensUsed = deps.Context.TotalTokens
	}

	deps.EventEmitter.Emit(events.TypeStepComplete, &events.StepCompleteData{
		StepNumber:   stepNumber,
		Duration:     time.Since(stepStart),
		ToolsInvoked: toolsInvoked,
		TokensUsed:   tokensUsed,
	})
}

// emitStateTransition emits a state transition event.
func (p *ExecutePhase) emitStateTransition(deps *Dependencies, from, to agent.AgentState, reason string) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeStateTransition, &events.StateTransitionData{
		FromState: from,
		ToState:   to,
		Reason:    reason,
	})
}

// emitError emits an error event.
func (p *ExecutePhase) emitError(deps *Dependencies, err error, recoverable bool) {
	if deps.EventEmitter == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeError, &events.ErrorData{
		Error:       err.Error(),
		Recoverable: recoverable,
	})
}

// emitGraphRefresh emits a graph refresh event.
func (p *ExecutePhase) emitGraphRefresh(deps *Dependencies, result *graph.RefreshResult, fileCount int) {
	if deps.EventEmitter == nil || result == nil {
		return
	}

	deps.EventEmitter.Emit(events.TypeContextUpdate, &events.ContextUpdateData{
		Action:          "graph_refresh",
		EntriesAffected: fileCount,
		TokensBefore:    result.NodesRemoved,
		TokensAfter:     result.NodesAdded,
	})
}

// -----------------------------------------------------------------------------
// CRS-06: Coordinator Event Emission
// -----------------------------------------------------------------------------

// emitCoordinatorEvent emits an event to the MCTS Coordinator.
//
// Description:
//
//	If a Coordinator is configured, emits the event which triggers appropriate
//	MCTS activities (Search, Learning, Awareness, etc.). The Coordinator
//	orchestrates activities based on the event type and current session state.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	deps - Phase dependencies containing Coordinator.
//	event - The agent event to emit.
//	inv - The tool invocation (may be nil).
//	result - The tool result (may be nil).
//	errorMsg - Error message if applicable.
//	errorCategory - Error category for learning (use crs.ErrorCategoryNone if N/A).
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) emitCoordinatorEvent(
	ctx context.Context,
	deps *Dependencies,
	event integration.AgentEvent,
	inv *agent.ToolInvocation,
	result *tools.Result,
	errorMsg string,
	errorCategory crs.ErrorCategory,
) {
	if deps.Coordinator == nil || deps.Session == nil {
		return
	}

	// Build event data
	data := &integration.EventData{
		SessionID:     deps.Session.ID,
		ErrorCategory: errorCategory, // CR-4 fix: Include error category for Learning activity
	}

	// Add tool information if available
	if inv != nil {
		data.Tool = inv.Tool
	}

	// Add error information if available
	if result != nil && !result.Success {
		data.Error = result.Error
	} else if errorMsg != "" {
		data.Error = errorMsg
	}

	// CR-3 fix: No nil check needed - already validated at function start
	data.StepNumber = deps.Session.GetMetric(agent.MetricSteps)

	// GR-30: Build GraphContext for tool execution events
	if inv != nil && (event == integration.EventToolExecuted || event == integration.EventToolFailed) {
		data.Graph = p.buildGraphContextForTool(inv, result)
		if data.Graph != nil {
			// Use Info level so it appears in server logs (test 8 greps for "graph_context")
			slog.Info("GR-30: Event with graph_context",
				slog.String("event", string(event)),
				slog.String("tool", inv.Tool),
				slog.Int("node_count", data.Graph.NodeCount),
				slog.Int("result_count", data.Graph.ResultCount),
			)
		}
	}

	// Handle the event - activities run synchronously in priority order
	results, err := deps.Coordinator.HandleEvent(ctx, event, data)
	if err != nil {
		slog.Warn("CRS-06: Coordinator event handling failed",
			slog.String("event", string(event)),
			slog.String("session_id", deps.Session.ID),
			slog.String("error", err.Error()),
		)
	} else {
		// CR-11 fix: Log result count for observability
		slog.Debug("CRS-06: Coordinator event handled",
			slog.String("event", string(event)),
			slog.String("session_id", deps.Session.ID),
			slog.Int("activities_run", len(results)),
		)
	}
}

// -----------------------------------------------------------------------------
// GR-30: GraphContext Building
// -----------------------------------------------------------------------------

// graphToolQueryTypes maps graph tools to their QueryType constants.
var graphToolQueryTypes = map[string]integration.QueryType{
	"find_callers":      integration.QueryTypeCallers,
	"find_callees":      integration.QueryTypeCallees,
	"find_path":         integration.QueryTypePath,
	"find_hotspots":     integration.QueryTypeHotspots,
	"find_dead_code":    integration.QueryTypeDeadCode,
	"find_cycles":       integration.QueryTypeCycles,
	"find_references":   integration.QueryTypeReferences,
	"find_symbol":       integration.QueryTypeSymbol,
	"find_implementers": integration.QueryTypeImplementations,
}

// buildGraphContextForTool creates a GraphContext from tool invocation and result.
//
// Description:
//
//	Extracts relevant graph information from tool execution:
//	- File paths from Read/Write/Grep tools
//	- Query metadata from graph tools (find_callers, etc.)
//	- Result counts from tool output
//
// Inputs:
//
//	inv - The tool invocation.
//	result - The tool result (may be nil).
//
// Outputs:
//
//	*integration.GraphContext - The constructed context, or nil if not applicable.
//
// Thread Safety: Safe for concurrent use.
func (p *ExecutePhase) buildGraphContextForTool(
	inv *agent.ToolInvocation,
	result *tools.Result,
) *integration.GraphContext {
	if inv == nil {
		return nil
	}

	builder := integration.NewGraphContextBuilder()

	// Extract file paths for file-related tools
	if inv.Parameters != nil && inv.Parameters.StringParams != nil {
		if filePath, ok := inv.Parameters.StringParams["file_path"]; ok && filePath != "" {
			switch inv.Tool {
			case "Read":
				builder.WithFilesRead(filePath)
			case "Write", "Edit":
				builder.WithFilesModified(filePath)
			}
		}
		if pattern, ok := inv.Parameters.StringParams["pattern"]; ok && pattern != "" {
			if inv.Tool == "Grep" || inv.Tool == "Glob" {
				// For search tools, track the query target
				builder.WithQuery("", pattern, 0)
			}
		}
	}

	// Extract query metadata for graph tools
	if queryType, isGraphTool := graphToolQueryTypes[inv.Tool]; isGraphTool {
		target := ""
		if inv.Parameters != nil && inv.Parameters.StringParams != nil {
			// Try common parameter names for graph tool targets
			for _, key := range []string{"function_name", "symbol", "symbol_name", "target", "name"} {
				if val, ok := inv.Parameters.StringParams[key]; ok && val != "" {
					target = val
					break
				}
			}
		}

		// Note: Result count would require parsing tool output; using 0 for now
		builder.WithQuery(queryType, target, 0)
	}

	// Build and return (using BuildUnsafe since we don't need validation here)
	gc := builder.BuildUnsafe()

	// Only return if we captured something meaningful
	if len(gc.FilesRead) == 0 && len(gc.FilesModified) == 0 &&
		gc.QueryType == "" && gc.QueryTarget == "" {
		integration.ReleaseGraphContext(gc)
		return nil
	}

	return gc
}
