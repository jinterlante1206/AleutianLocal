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
	"errors"
	"log/slog"
	"net/http"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
	"github.com/gin-gonic/gin"
)

// AgentHandlers contains the HTTP handlers for the Code Buddy agent.
//
// Thread Safety: AgentHandlers is safe for concurrent use.
type AgentHandlers struct {
	loop agent.AgentLoop
	svc  *Service
}

// NewAgentHandlers creates handlers for the Code Buddy agent.
//
// Description:
//
//	Creates HTTP handlers that wrap the AgentLoop interface.
//	The handlers provide REST endpoints for starting, continuing,
//	and aborting agent sessions.
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
	return &AgentHandlers{
		loop: loop,
		svc:  svc,
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
