// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package code_buddy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/routing"
	"github.com/gin-gonic/gin"
)

// AgentHandlers contains the HTTP handlers for the Code Buddy agent.
//
// Thread Safety: AgentHandlers is safe for concurrent use.
type AgentHandlers struct {
	loop         agent.AgentLoop
	svc          *Service
	modelManager *llm.MultiModelManager
}

// NewAgentHandlers creates handlers for the Code Buddy agent.
//
// Description:
//
//	Creates HTTP handlers that wrap the AgentLoop interface.
//	The handlers provide REST endpoints for starting, continuing,
//	and aborting agent sessions.
//	Also initializes a shared MultiModelManager for tool routing.
//
// Inputs:
//
//	loop - The agent loop implementation. Must not be nil.
//	svc - The Code Buddy service for graph initialization. Must not be nil.
//
// Outputs:
//
//	*AgentHandlers - The configured handlers.
//
// Example:
//
//	loop := agent.NewDefaultAgentLoop()
//	svc := code_buddy.NewService(config)
//	handlers := code_buddy.NewAgentHandlers(loop, svc)
func NewAgentHandlers(loop agent.AgentLoop, svc *Service) *AgentHandlers {
	// Get Ollama endpoint from environment or use default
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	return &AgentHandlers{
		loop:         loop,
		svc:          svc,
		modelManager: llm.NewMultiModelManager(ollamaURL),
	}
}

// HandleAgentRun handles POST /v1/codebuddy/agent/run.
//
// Description:
//
//	Starts a new agent session with the given query. The session
//	initializes the code graph (if not already initialized), assembles
//	context, and executes the agent loop until completion or clarification.
//
// Request Body:
//
//	AgentRunRequest
//
// Response:
//
//	200 OK: AgentRunResponse (session completed or needs clarification)
//	400 Bad Request: Validation error
//	409 Conflict: Session already in progress
//	500 Internal Server Error: Processing error
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleAgentRun(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleAgentRun")

	var req AgentRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid request body", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
		return
	}

	if req.Query == "" {
		logger.Warn("Empty query")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Query is required",
			Code:  "EMPTY_QUERY",
		})
		return
	}

	logger.Info("Starting agent session",
		"project_root", req.ProjectRoot,
		"query_len", len(req.Query))

	// Create session with optional config
	var sessionConfig *agent.SessionConfig
	if req.Config != nil {
		sessionConfig = req.Config
	}

	session, err := agent.NewSession(req.ProjectRoot, sessionConfig)
	if err != nil {
		logger.Error("Failed to create session", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
			Code:  "INVALID_CONFIG",
		})
		return
	}

	// Initialize tool router if enabled
	if session.Config.ToolRouterEnabled {
		if err := h.initializeToolRouter(c.Request.Context(), session, logger); err != nil {
			// Log warning but don't fail - tool routing is optional
			logger.Warn("Failed to initialize tool router, continuing without it",
				"error", err,
				"model", session.Config.ToolRouterModel)
		}
	}

	// Run the agent loop
	result, err := h.loop.Run(c.Request.Context(), session, req.Query)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errCode := "AGENT_ERROR"

		if errors.Is(err, agent.ErrInvalidSession) {
			statusCode = http.StatusBadRequest
			errCode = "INVALID_SESSION"
		} else if errors.Is(err, agent.ErrEmptyQuery) {
			statusCode = http.StatusBadRequest
			errCode = "EMPTY_QUERY"
		} else if errors.Is(err, agent.ErrSessionInProgress) {
			statusCode = http.StatusConflict
			errCode = "SESSION_IN_PROGRESS"
		}

		logger.Error("Agent run failed", "error", err)
		c.JSON(statusCode, ErrorResponse{
			Error: err.Error(),
			Code:  errCode,
		})
		return
	}

	logger.Info("Agent session completed",
		"session_id", session.ID,
		"state", result.State,
		"steps_taken", result.StepsTaken)

	c.JSON(http.StatusOK, AgentRunResponse{
		SessionID:    session.ID,
		State:        string(result.State),
		StepsTaken:   result.StepsTaken,
		TokensUsed:   result.TokensUsed,
		Response:     result.Response,
		NeedsClarify: result.NeedsClarify,
		Error:        agentErrorToString(result.Error),
		DegradedMode: session.GetMetrics().DegradedMode,
	})
}

// HandleAgentContinue handles POST /v1/codebuddy/agent/continue.
//
// Description:
//
//	Continues an existing agent session that is waiting for clarification.
//	The session must be in the CLARIFY state to accept continuation.
//
// Request Body:
//
//	AgentContinueRequest
//
// Response:
//
//	200 OK: AgentRunResponse
//	400 Bad Request: Session not in CLARIFY state
//	404 Not Found: Session not found
//	500 Internal Server Error: Processing error
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleAgentContinue(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleAgentContinue")

	var req AgentContinueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid request body", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
		return
	}

	if req.SessionID == "" {
		logger.Warn("Missing session_id")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "session_id is required",
			Code:  "MISSING_PARAMETER",
		})
		return
	}

	logger.Info("Continuing agent session",
		"session_id", req.SessionID,
		"clarification_len", len(req.Clarification))

	result, err := h.loop.Continue(c.Request.Context(), req.SessionID, req.Clarification)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errCode := "AGENT_ERROR"

		if errors.Is(err, agent.ErrSessionNotFound) {
			statusCode = http.StatusNotFound
			errCode = "SESSION_NOT_FOUND"
		} else if errors.Is(err, agent.ErrNotInClarifyState) {
			statusCode = http.StatusBadRequest
			errCode = "NOT_IN_CLARIFY_STATE"
		} else if errors.Is(err, agent.ErrSessionInProgress) {
			statusCode = http.StatusConflict
			errCode = "SESSION_IN_PROGRESS"
		}

		logger.Error("Agent continue failed", "error", err)
		c.JSON(statusCode, ErrorResponse{
			Error: err.Error(),
			Code:  errCode,
		})
		return
	}

	// Get state info including degraded mode
	state, _ := h.loop.GetState(req.SessionID)
	degradedMode := false
	if state != nil {
		degradedMode = state.DegradedMode
	}

	logger.Info("Agent session continued",
		"session_id", req.SessionID,
		"state", result.State,
		"steps_taken", result.StepsTaken)

	c.JSON(http.StatusOK, AgentRunResponse{
		SessionID:    req.SessionID,
		State:        string(result.State),
		StepsTaken:   result.StepsTaken,
		TokensUsed:   result.TokensUsed,
		Response:     result.Response,
		NeedsClarify: result.NeedsClarify,
		Error:        agentErrorToString(result.Error),
		DegradedMode: degradedMode,
	})
}

// HandleAgentAbort handles POST /v1/codebuddy/agent/abort.
//
// Description:
//
//	Aborts an active agent session. The session will transition
//	to the ERROR state and any in-progress operations will be cancelled.
//
// Request Body:
//
//	AgentAbortRequest
//
// Response:
//
//	200 OK: Success message
//	404 Not Found: Session not found
//	500 Internal Server Error: Processing error
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleAgentAbort(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleAgentAbort")

	var req AgentAbortRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid request body", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Invalid request body",
			Code:  "INVALID_REQUEST",
		})
		return
	}

	if req.SessionID == "" {
		logger.Warn("Missing session_id")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "session_id is required",
			Code:  "MISSING_PARAMETER",
		})
		return
	}

	logger.Info("Aborting agent session", "session_id", req.SessionID)

	err := h.loop.Abort(c.Request.Context(), req.SessionID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errCode := "AGENT_ERROR"

		if errors.Is(err, agent.ErrSessionNotFound) {
			statusCode = http.StatusNotFound
			errCode = "SESSION_NOT_FOUND"
		}

		logger.Error("Agent abort failed", "error", err)
		c.JSON(statusCode, ErrorResponse{
			Error: err.Error(),
			Code:  errCode,
		})
		return
	}

	logger.Info("Agent session aborted", "session_id", req.SessionID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Session aborted successfully",
		"session_id": req.SessionID,
	})
}

// HandleAgentState handles GET /v1/codebuddy/agent/:id.
//
// Description:
//
//	Retrieves the current state of an agent session including
//	metrics, history, and any pending clarification prompts.
//
// Path Parameters:
//
//	id: Session ID (required)
//
// Response:
//
//	200 OK: AgentStateResponse
//	404 Not Found: Session not found
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleAgentState(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleAgentState")

	sessionID := c.Param("id")
	if sessionID == "" {
		logger.Warn("Missing session id")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "session id is required",
			Code:  "MISSING_PARAMETER",
		})
		return
	}

	logger.Info("Getting agent session state", "session_id", sessionID)

	state, err := h.loop.GetState(sessionID)
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: err.Error(),
				Code:  "SESSION_NOT_FOUND",
			})
			return
		}

		logger.Error("Get session state failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
			Code:  "GET_STATE_FAILED",
		})
		return
	}

	logger.Info("Got agent session state",
		"session_id", sessionID,
		"state", state.State)

	c.JSON(http.StatusOK, AgentStateResponse{
		SessionID:    state.ID,
		ProjectRoot:  state.ProjectRoot,
		GraphID:      state.GraphID,
		State:        string(state.State),
		StepCount:    state.StepCount,
		TokensUsed:   state.TokensUsed,
		CreatedAt:    state.CreatedAt.Unix(),
		LastActiveAt: state.LastActiveAt.Unix(),
		DegradedMode: state.DegradedMode,
	})
}

// HandleGetReasoningTrace handles GET /v1/codebuddy/agent/:id/reasoning.
//
// Description:
//
//	Retrieves the step-by-step reasoning trace for a session.
//	The trace shows what actions were taken, what was found,
//	and how the CRS was updated during reasoning.
//
// Path Parameters:
//
//	id: Session ID (required)
//
// Response:
//
//	200 OK: ReasoningTraceResponse
//	404 Not Found: Session not found
//	204 No Content: Session exists but trace recording not enabled
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleGetReasoningTrace(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleGetReasoningTrace")

	sessionID := c.Param("id")
	if sessionID == "" {
		logger.Warn("Missing session id")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "session id is required",
			Code:  "MISSING_PARAMETER",
		})
		return
	}

	logger.Info("Getting reasoning trace", "session_id", sessionID)

	session, err := h.loop.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: err.Error(),
				Code:  "SESSION_NOT_FOUND",
			})
			return
		}

		logger.Error("Get session failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
			Code:  "GET_SESSION_FAILED",
		})
		return
	}

	trace := session.GetReasoningTrace()
	if trace == nil {
		// Trace recording not enabled for this session
		c.Status(http.StatusNoContent)
		return
	}

	// Convert to API response
	response := convertReasoningTrace(trace, session.GetReasoningSummary())

	logger.Info("Got reasoning trace",
		"session_id", sessionID,
		"total_steps", response.TotalSteps)

	c.JSON(http.StatusOK, response)
}

// HandleGetCRSExport handles GET /v1/codebuddy/agent/:id/crs.
//
// Description:
//
//	Retrieves the full CRS (Code Reasoning State) export for a session.
//	This includes all six indexes and summary metrics for debugging
//	and analysis of the reasoning process.
//
// Path Parameters:
//
//	id: Session ID (required)
//
// Response:
//
//	200 OK: CRSExportResponse
//	404 Not Found: Session not found
//	204 No Content: Session exists but CRS not enabled
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) HandleGetCRSExport(c *gin.Context) {
	requestID := getOrCreateRequestID(c)
	logger := slog.With("request_id", requestID, "handler", "HandleGetCRSExport")

	sessionID := c.Param("id")
	if sessionID == "" {
		logger.Warn("Missing session id")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "session id is required",
			Code:  "MISSING_PARAMETER",
		})
		return
	}

	logger.Info("Getting CRS export", "session_id", sessionID)

	session, err := h.loop.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: err.Error(),
				Code:  "SESSION_NOT_FOUND",
			})
			return
		}

		logger.Error("Get session failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: err.Error(),
			Code:  "GET_SESSION_FAILED",
		})
		return
	}

	export := session.GetCRSExport()
	if export == nil {
		// CRS not enabled for this session
		c.Status(http.StatusNoContent)
		return
	}

	// Convert to API response
	response := convertCRSExport(export)

	logger.Info("Got CRS export",
		"session_id", sessionID,
		"generation", response.Generation)

	c.JSON(http.StatusOK, response)
}

// initializeToolRouter sets up the micro-LLM tool router for a session.
//
// Description:
//
//	Creates a Granite4Router with the session's configuration and wraps it
//	with a RouterAdapter to implement the agent.ToolRouter interface.
//	Blocks until the router model is warmed to ensure tool selection works.
//
// Inputs:
//
//	ctx - Context for the request (used for logging, not warmup).
//	session - The session to configure.
//	logger - Logger for diagnostics.
//
// Outputs:
//
//	error - Non-nil if router creation or warmup fails.
//
// Thread Safety: This method is safe for concurrent use.
func (h *AgentHandlers) initializeToolRouter(ctx context.Context, session *agent.Session, logger *slog.Logger) error {
	// Build router config from session config
	routerConfig := routing.RouterConfig{
		Model:               session.Config.ToolRouterModel,
		OllamaEndpoint:      h.getOllamaEndpoint(),
		Timeout:             session.Config.ToolRouterTimeout,
		ConfidenceThreshold: session.Config.ToolRouterConfidence,
		Temperature:         0.1, // Low temperature for consistent routing
		MaxTokens:           256,
		KeepAlive:           "24h", // Keep model loaded (24 hours)
	}

	// Create the Granite4 router
	router, err := routing.NewGranite4Router(h.modelManager, routerConfig)
	if err != nil {
		return err
	}

	// Wrap with adapter to implement agent.ToolRouter interface
	adapter := routing.NewRouterAdapter(router)

	// Set router on session
	session.SetToolRouter(adapter)

	logger.Info("Tool router initialized",
		"model", routerConfig.Model,
		"timeout", routerConfig.Timeout,
		"confidence_threshold", routerConfig.ConfidenceThreshold)

	// Warm the router model SYNCHRONOUSLY with background context.
	// We use context.Background() with timeout instead of request context
	// because the request context may be cancelled when HTTP response is sent,
	// but we need the model to stay loaded for subsequent tool routing calls.
	warmupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger.Info("Warming router model (blocking)", "model", routerConfig.Model)
	if warmErr := router.WarmRouter(warmupCtx); warmErr != nil {
		logger.Error("Failed to warm router model",
			"error", warmErr,
			"model", routerConfig.Model)
		return warmErr
	}
	logger.Info("Router model warmed successfully", "model", routerConfig.Model)

	return nil
}

// getOllamaEndpoint returns the Ollama endpoint from environment or default.
func (h *AgentHandlers) getOllamaEndpoint() string {
	endpoint := os.Getenv("OLLAMA_URL")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return endpoint
}

// agentErrorToString converts an AgentError to a string, returning empty string for nil.
//
// Inputs:
//
//	err - The agent error to convert.
//
// Outputs:
//
//	string - The error message or empty string.
func agentErrorToString(err *agent.AgentError) string {
	if err == nil {
		return ""
	}
	return err.Message
}

// convertReasoningTrace converts crs.ReasoningTrace to ReasoningTraceResponse.
//
// Inputs:
//
//	trace - The CRS reasoning trace.
//	summary - The reasoning summary (optional).
//
// Outputs:
//
//	*ReasoningTraceResponse - The API response.
func convertReasoningTrace(trace *crs.ReasoningTrace, summary *agent.ReasoningSummary) *ReasoningTraceResponse {
	if trace == nil {
		return nil
	}

	response := &ReasoningTraceResponse{
		SessionID:  trace.SessionID,
		TotalSteps: trace.TotalSteps,
		Duration:   trace.Duration,
		Trace:      make([]ReasoningStep, 0, len(trace.Trace)),
	}

	if !trace.StartTime.IsZero() {
		response.StartTime = trace.StartTime.Format("2006-01-02T15:04:05Z07:00")
	}
	if !trace.EndTime.IsZero() {
		response.EndTime = trace.EndTime.Format("2006-01-02T15:04:05Z07:00")
	}

	for _, step := range trace.Trace {
		respStep := ReasoningStep{
			Step:         step.Step,
			Action:       step.Action,
			Target:       step.Target,
			Tool:         step.Tool,
			DurationMs:   step.Duration.Milliseconds(),
			SymbolsFound: step.SymbolsFound,
			Error:        step.Error,
			Metadata:     step.Metadata,
		}

		if !step.Timestamp.IsZero() {
			respStep.Timestamp = step.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		}

		for _, update := range step.ProofUpdates {
			respStep.ProofUpdates = append(respStep.ProofUpdates, ProofUpdateResponse{
				NodeID: update.NodeID,
				Status: update.Status,
				Reason: update.Reason,
				Source: update.Source,
			})
		}

		response.Trace = append(response.Trace, respStep)
	}

	if summary != nil {
		response.Summary = &ReasoningSummaryResponse{
			NodesExplored:      summary.NodesExplored,
			NodesProven:        summary.NodesProven,
			NodesDisproven:     summary.NodesDisproven,
			NodesUnknown:       summary.NodesUnknown,
			ConstraintsApplied: summary.ConstraintsApplied,
			ExplorationDepth:   summary.ExplorationDepth,
			ConfidenceScore:    summary.ConfidenceScore,
		}
	}

	return response
}

// convertCRSExport converts crs.CRSExport to CRSExportResponse.
//
// Inputs:
//
//	export - The CRS export.
//
// Outputs:
//
//	*CRSExportResponse - The API response.
func convertCRSExport(export *crs.CRSExport) *CRSExportResponse {
	if export == nil {
		return nil
	}

	response := &CRSExportResponse{
		SessionID:  export.SessionID,
		Generation: export.Generation,
		Indexes:    CRSIndexesResponse{},
		Summary: ReasoningSummaryResponse{
			NodesExplored:      export.Summary.NodesExplored,
			NodesProven:        export.Summary.NodesProven,
			NodesDisproven:     export.Summary.NodesDisproven,
			NodesUnknown:       export.Summary.NodesUnknown,
			ConstraintsApplied: export.Summary.ConstraintsApplied,
			ExplorationDepth:   export.Summary.ExplorationDepth,
			ConfidenceScore:    export.Summary.ConfidenceScore,
		},
	}

	if !export.Timestamp.IsZero() {
		response.Timestamp = export.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	}

	// Convert proof index entries
	for _, entry := range export.Indexes.Proof.Entries {
		response.Indexes.Proof = append(response.Indexes.Proof, ProofEntryResponse{
			NodeID: entry.NodeID,
			Status: entry.Status,
			// Evidence is derived from proof/disproof numbers
			Evidence: nil,
		})
	}

	// Convert constraint index entries
	for _, entry := range export.Indexes.Constraint.Constraints {
		response.Indexes.Constraints = append(response.Indexes.Constraints, ConstraintEntryResponse{
			ID:       entry.ID,
			Type:     entry.Type,
			Nodes:    entry.Nodes,
			Strength: 1.0, // Not stored in source, default to 1.0
		})
	}

	// Set aggregate counts for performance-deferred indexes
	response.Indexes.SimilarityCount = export.Indexes.Similarity.PairCount
	response.Indexes.DependencyCount = export.Indexes.Dependency.EdgeCount
	response.Indexes.StreamingCardinality = export.Indexes.Streaming.Cardinality
	response.Indexes.StreamingBytes = export.Indexes.Streaming.ApproximateBytes

	// Convert history index recent entries
	for _, entry := range export.Indexes.History.RecentEntries {
		histEntry := HistoryEntryResponse{
			NodeID:     entry.NodeID,
			VisitCount: 1, // Each entry represents one visit
		}
		if !entry.Timestamp.IsZero() {
			histEntry.LastVisitedAt = entry.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		}
		response.Indexes.History = append(response.Indexes.History, histEntry)
	}

	return response
}
