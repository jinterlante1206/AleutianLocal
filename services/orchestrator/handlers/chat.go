// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/extensions"
	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/middleware"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/services"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// Interface Definition
// =============================================================================

// ChatHandler defines the contract for handling chat HTTP requests.
//
// # Description
//
// ChatHandler abstracts chat endpoint handling, enabling different
// implementations and facilitating testing via mocks. The interface
// follows the single-method-per-operation pattern for clarity.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use by multiple goroutines.
// HTTP handlers are called concurrently by the Gin framework.
//
// # Limitations
//
//   - Streaming endpoints not included (separate interface)
//
// # Assumptions
//
//   - All dependencies are properly initialized before handler use
//   - Gin context is valid and not nil
type ChatHandler interface {
	// HandleDirectChat processes direct LLM chat requests (no RAG).
	HandleDirectChat(c *gin.Context)

	// HandleChatRAG processes conversational RAG requests.
	HandleChatRAG(c *gin.Context)
}

// =============================================================================
// Struct Definition
// =============================================================================

// chatHandler implements ChatHandler for production use.
//
// # Description
//
// chatHandler coordinates between HTTP layer and business logic.
// It performs only HTTP-related tasks:
//   - Request parsing and validation
//   - Error mapping to HTTP status codes
//   - Response serialization
//
// All business logic is delegated to injected services.
//
// # Fields
//
//   - llmClient: LLM client for direct chat generation
//   - policyEngine: Policy engine for sensitive data scanning
//   - ragService: Service for RAG chat processing
//   - tracer: OpenTelemetry tracer for distributed tracing
//   - opts: Extension options for enterprise features (auth, audit, filter)
//
// # Thread Safety
//
// Thread-safe. All fields are read-only after construction.
// No shared mutable state between requests.
//
// # Limitations
//
//   - Does not support streaming (will be added separately)
//
// # Assumptions
//
//   - Dependencies are non-nil and properly configured
type chatHandler struct {
	llmClient    llm.LLMClient
	policyEngine *policy_engine.PolicyEngine
	ragService   *services.ChatRAGService
	tracer       trace.Tracer
	opts         extensions.ServiceOptions
}

// =============================================================================
// Constructor
// =============================================================================

// NewChatHandler creates a ChatHandler with the provided dependencies.
//
// # Description
//
// Creates a fully configured chatHandler for production use.
// All dependencies must be properly initialized before calling.
// Panics if llmClient or policyEngine is nil (programming errors).
//
// # Inputs
//
//   - llmClient: LLM client for direct chat. Must not be nil.
//   - policyEngine: Policy scanner. Must not be nil.
//   - ragService: RAG chat service. May be nil if RAG is not used.
//   - opts: Extension options for enterprise features (auth, audit, filter).
//
// # Outputs
//
//   - ChatHandler: Ready for use with Gin router
//
// # Examples
//
//	handler := handlers.NewChatHandler(llmClient, policyEngine, ragService, opts)
//	router.POST("/v1/chat/direct", handler.HandleDirectChat)
//	router.POST("/v1/chat/rag", handler.HandleChatRAG)
//
// # Limitations
//
//   - Panics on nil llmClient or policyEngine
//
// # Assumptions
//
//   - llmClient and policyEngine are non-nil and ready for use
func NewChatHandler(
	llmClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine,
	ragService *services.ChatRAGService,
	opts extensions.ServiceOptions,
) ChatHandler {
	if llmClient == nil {
		panic("NewChatHandler: llmClient must not be nil")
	}
	if policyEngine == nil {
		panic("NewChatHandler: policyEngine must not be nil")
	}

	return &chatHandler{
		llmClient:    llmClient,
		policyEngine: policyEngine,
		ragService:   ragService,
		tracer:       otel.Tracer("aleutian.orchestrator.handlers.chat"),
		opts:         opts,
	}
}

// =============================================================================
// Handler Methods
// =============================================================================

// HandleDirectChat processes direct LLM chat requests.
//
// # Description
//
// Handles POST /v1/chat/direct requests. The flow is:
//  1. Parse request body
//  2. Validate request (request_id and timestamp are required)
//  3. Scan last user message for policy violations
//  4. Call LLM client with messages and parameters
//  5. Return response with answer, response_id, and processing time
//
// # Inputs
//
//   - c: Gin context containing the HTTP request
//
// Request Body (datatypes.DirectChatRequest):
//   - request_id: Required. UUID v4 identifier for tracing.
//   - timestamp: Required. Unix timestamp in milliseconds (UTC).
//   - messages: Required. Array of message objects (1-100) with role and content.
//   - enable_thinking: Optional. Enable extended thinking mode.
//   - budget_tokens: Optional. Token budget for thinking (0-65536).
//   - tools: Optional. Tool definitions for function calling.
//
// # Outputs
//
// HTTP Responses:
//   - 200 OK: DirectChatResponse with answer, response_id, timestamp
//   - 400 Bad Request: {"error": "invalid request body"} or {"error": "validation failed: ..."}
//   - 403 Forbidden: {"error": "Policy Violation...", "findings": [...]}
//   - 500 Internal Server Error: {"error": "Failed to process request"}
//
// # Examples
//
// Request:
//
//	POST /v1/chat/direct
//	{
//	    "request_id": "550e8400-e29b-41d4-a716-446655440000",
//	    "timestamp": 1735817400000,
//	    "messages": [{"role": "user", "content": "Hello"}]
//	}
//
// Response:
//
//	{
//	    "response_id": "660f9500-f39c-52e5-b827-557766551111",
//	    "request_id": "550e8400-e29b-41d4-a716-446655440000",
//	    "timestamp": 1735817401250,
//	    "answer": "Hello! How can I help you?",
//	    "processing_time_ms": 1250
//	}
//
// # Limitations
//
//   - No streaming support (use streaming endpoint instead)
//   - Only scans last user message for policy (not full history)
//
// # Assumptions
//
//   - Request body is valid JSON
//   - LLM client is available and responding
//
// # Security References
//
//   - SEC-003: Message size limits enforced via validation
//   - SEC-005: Internal errors not exposed to client
func (h *chatHandler) HandleDirectChat(c *gin.Context) {
	ctx, span := h.tracer.Start(c.Request.Context(), "HandleDirectChat")
	defer span.End()

	startTime := time.Now()

	// Step 0: Get authenticated user from context (set by auth middleware)
	authInfo := middleware.GetAuthInfo(c)
	userID := "anonymous"
	if authInfo != nil {
		userID = authInfo.UserID
	}

	// Step 0.5: Read raw body for audit capture (FOSS-008)
	// Enterprise uses this to compute hashes and store the exact request bytes.
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		span.RecordError(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}
	// Reset body so BindJSON can read it
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Capture request for audit (FOSS-008)
	// Enterprise computes hashes/encrypts; FOSS Nop does nothing.
	auditID, _ := h.opts.RequestAuditor.CaptureRequest(ctx, &extensions.AuditableRequest{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Headers:   extractHeaders(c),
		Body:      rawBody,
		UserID:    userID,
		Timestamp: startTime,
	})

	// Step 1: Parse request body
	var req datatypes.DirectChatRequest
	if err := c.BindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		slog.Error("Failed to parse the chat request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Step 2: Validate request
	if err := req.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		slog.Error("Request validation failed",
			"error", err,
			"requestId", req.RequestID,
		)
		// SEC-005: Sanitize validation errors - do not expose internal field names
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: validation failed"})
		return
	}

	// Step 2.5: Authorization check (FOSS-004)
	// Enterprise can restrict who can send chat messages
	if err := h.opts.AuthzProvider.Authorize(ctx, extensions.AuthzRequest{
		User:         authInfo,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "direct",
	}); err != nil {
		span.SetStatus(codes.Error, "authorization denied")
		// Log unauthorized attempt
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "authz.denied",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			Outcome:      "denied",
			Metadata: map[string]any{
				"request_id": req.RequestID,
				"reason":     err.Error(),
			},
		})
		if errors.Is(err, extensions.ErrUnauthorized) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		} else {
			c.JSON(http.StatusForbidden, gin.H{"error": "authorization failed"})
		}
		return
	}

	// Step 3: Filter and scan the last user message (FOSS-006)
	// Applies PII detection/redaction before sending to LLM
	if len(req.Messages) > 0 {
		lastIdx := len(req.Messages) - 1
		lastMsg := req.Messages[lastIdx]
		if lastMsg.Role == "user" {
			// Apply message filter (FOSS-006)
			filterResult, filterErr := h.opts.MessageFilter.FilterInput(ctx, lastMsg.Content)
			if filterErr != nil {
				slog.Error("Message filter failed", "error", filterErr, "requestId", req.RequestID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "message processing failed"})
				return
			}

			// Check if message was blocked by filter
			if filterResult.WasBlocked {
				_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
					EventType:    "chat.blocked",
					Timestamp:    time.Now().UTC(),
					UserID:       userID,
					Action:       "send",
					ResourceType: "message",
					Outcome:      "blocked",
					Metadata: map[string]any{
						"request_id": req.RequestID,
						"reason":     filterResult.BlockReason,
					},
				})
				c.JSON(http.StatusBadRequest, gin.H{
					"error":  "message blocked by content filter",
					"reason": filterResult.BlockReason,
				})
				return
			}

			// Use filtered content (may have PII redacted)
			req.Messages[lastIdx].Content = filterResult.Filtered

			// Also run policy engine scan on filtered content
			findings := h.policyEngine.ScanFileContent(filterResult.Filtered)
			if len(findings) > 0 {
				slog.Warn("Blocked chat request due to policy violation",
					"findings", len(findings),
					"requestId", req.RequestID,
				)
				c.JSON(http.StatusForbidden, gin.H{
					"error":    "Policy Violation: Message contains sensitive data.",
					"findings": findings,
				})
				return
			}
		}
	}

	// Step 4: Call LLM client
	params := llm.GenerationParams{
		EnableThinking:  req.EnableThinking,
		BudgetTokens:    req.BudgetTokens,
		ToolDefinitions: req.Tools,
	}
	answer, err := h.llmClient.Chat(ctx, req.Messages, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "LLM chat failed")
		slog.Error("LLMClient.Chat failed",
			"error", err,
			"requestId", req.RequestID,
		)
		// Log failed chat attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "chat.message",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "message",
			Outcome:      "error",
			Metadata: map[string]any{
				"request_id": req.RequestID,
			},
		})
		// SEC-005: Do NOT expose internal error details to client
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	// Step 4.5: Filter output (FOSS-006)
	// Apply output filter to LLM response (e.g., remove leaked secrets)
	outputResult, outputErr := h.opts.MessageFilter.FilterOutput(ctx, answer)
	if outputErr != nil {
		slog.Error("Output filter failed", "error", outputErr, "requestId", req.RequestID)
		// Continue with original answer if filter fails
	} else {
		answer = outputResult.Filtered
	}

	// Step 5: Log successful chat (FOSS-005)
	processingTime := time.Since(startTime).Milliseconds()
	_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
		EventType:    "chat.message",
		Timestamp:    time.Now().UTC(),
		UserID:       userID,
		Action:       "send",
		ResourceType: "message",
		Outcome:      "success",
		Metadata: map[string]any{
			"request_id":         req.RequestID,
			"processing_time_ms": processingTime,
		},
	})

	// Step 6: Build and return response
	resp := datatypes.NewDirectChatResponse(req.RequestID, answer)
	resp.ProcessingTimeMs = processingTime

	// Step 6.5: Capture response for audit (FOSS-008)
	// Enterprise uses this to compute hashes and store the exact response bytes.
	respBytes, _ := json.Marshal(resp)
	_ = h.opts.RequestAuditor.CaptureResponse(ctx, auditID, &extensions.AuditableResponse{
		StatusCode: http.StatusOK,
		Headers:    extensions.HTTPHeaders{"Content-Type": "application/json"},
		Body:       respBytes,
		Timestamp:  time.Now().UTC(),
	})

	c.JSON(http.StatusOK, resp)
}

// HandleChatRAG processes conversational RAG requests.
//
// # Description
//
// Handles POST /v1/chat/rag requests. Delegates all business logic
// to ChatRAGService, performing only HTTP-layer operations:
//  1. Parse request body
//  2. Call service.Process()
//  3. Map errors to HTTP status codes
//  4. Return response
//
// # Inputs
//
//   - c: Gin context containing the HTTP request
//
// Request Body (datatypes.ChatRAGRequest):
//   - message: Required. User's query.
//   - session_id: Optional. Existing session to continue.
//   - pipeline: Optional. RAG pipeline name (default: "reranking").
//   - bearing: Optional. Topic filter for retrieval.
//   - stream: Optional. Enable streaming (not yet implemented).
//   - history: Optional. Previous conversation turns.
//
// # Outputs
//
// HTTP Responses:
//   - 200 OK: ChatRAGResponse with answer, sources, session_id
//   - 400 Bad Request: {"error": "invalid request body"}
//   - 403 Forbidden: {"error": "Policy Violation...", "findings": [...]}
//   - 500 Internal Server Error: {"error": "Failed to process request"}
//
// # Examples
//
// Request:
//
//	POST /v1/chat/rag
//	{"message": "What is authentication?", "pipeline": "reranking"}
//
// Response:
//
//	{
//	    "id": "resp-uuid",
//	    "created_at": 1735817400000,
//	    "answer": "Authentication is...",
//	    "sources": [...],
//	    "session_id": "sess-uuid",
//	    "turn_count": 1
//	}
//
// # Limitations
//
//   - Streaming not yet implemented (stream flag ignored)
//   - Requires ragService to be non-nil
//
// # Assumptions
//
//   - RAG service and engine are available
//
// # Security References
//
//   - SEC-005: Internal errors not exposed to client
func (h *chatHandler) HandleChatRAG(c *gin.Context) {
	ctx, span := h.tracer.Start(c.Request.Context(), "HandleChatRAG")
	defer span.End()

	startTime := time.Now()

	// Step 0: Get authenticated user from context
	authInfo := middleware.GetAuthInfo(c)
	userID := "anonymous"
	if authInfo != nil {
		userID = authInfo.UserID
	}

	// Step 0.5: Read raw body for audit capture (FOSS-008)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		span.RecordError(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Capture request for audit (FOSS-008)
	auditID, _ := h.opts.RequestAuditor.CaptureRequest(ctx, &extensions.AuditableRequest{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Headers:   extractHeaders(c),
		Body:      rawBody,
		UserID:    userID,
		Timestamp: startTime,
	})

	// Check if RAG service is available
	if h.ragService == nil {
		slog.Error("RAG service not configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "RAG service not available"})
		return
	}

	// Step 1: Parse request body
	var req datatypes.ChatRAGRequest
	if err := c.BindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		slog.Error("Failed to parse chat RAG request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	// Step 1.5: Authorization check (FOSS-004)
	if err := h.opts.AuthzProvider.Authorize(ctx, extensions.AuthzRequest{
		User:         authInfo,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "rag",
	}); err != nil {
		span.SetStatus(codes.Error, "authorization denied")
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "authz.denied",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			Outcome:      "denied",
			Metadata: map[string]any{
				"request_id": req.Id,
				"session_id": req.SessionId,
			},
		})
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	// Step 1.6: Filter input message (FOSS-006)
	if req.Message != "" {
		filterResult, filterErr := h.opts.MessageFilter.FilterInput(ctx, req.Message)
		if filterErr != nil {
			slog.Error("Message filter failed", "error", filterErr, "requestId", req.Id)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "message processing failed"})
			return
		}
		if filterResult.WasBlocked {
			_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
				EventType:    "chat.blocked",
				Timestamp:    time.Now().UTC(),
				UserID:       userID,
				Action:       "send",
				ResourceType: "message",
				Outcome:      "blocked",
				Metadata: map[string]any{
					"request_id": req.Id,
					"session_id": req.SessionId,
					"reason":     filterResult.BlockReason,
				},
			})
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "message blocked by content filter",
				"reason": filterResult.BlockReason,
			})
			return
		}
		req.Message = filterResult.Filtered
	}

	// Step 2: Call service layer
	resp, err := h.ragService.Process(ctx, &req)
	if err != nil {
		span.RecordError(err)

		// Check for policy violation (403)
		if services.IsPolicyViolation(err) {
			span.SetStatus(codes.Error, "policy violation")
			findings := services.GetPolicyFindings(err)
			slog.Warn("Blocked chat RAG request due to policy violation",
				"findings", len(findings),
				"requestId", req.Id,
			)
			c.JSON(http.StatusForbidden, gin.H{
				"error":    "Policy Violation: Message contains sensitive data.",
				"findings": findings,
			})
			return
		}

		// All other errors are internal server errors
		// SEC-005: Do NOT expose internal error details to client
		span.SetStatus(codes.Error, "processing failed")
		slog.Error("Chat RAG processing failed",
			"error", err,
			"requestId", req.Id,
			"sessionId", req.SessionId,
		)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "chat.message",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "message",
			Outcome:      "error",
			Metadata: map[string]any{
				"request_id": req.Id,
				"session_id": req.SessionId,
			},
		})
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process request",
		})
		return
	}

	// Step 2.5: Filter output (FOSS-006)
	if resp.Answer != "" {
		outputResult, outputErr := h.opts.MessageFilter.FilterOutput(ctx, resp.Answer)
		if outputErr != nil {
			slog.Error("Output filter failed", "error", outputErr, "requestId", req.Id)
		} else {
			resp.Answer = outputResult.Filtered
		}
	}

	// Step 3: Log successful chat (FOSS-005)
	processingTime := time.Since(startTime).Milliseconds()
	_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
		EventType:    "chat.message",
		Timestamp:    time.Now().UTC(),
		UserID:       userID,
		Action:       "send",
		ResourceType: "message",
		Outcome:      "success",
		Metadata: map[string]any{
			"request_id":         req.Id,
			"session_id":         resp.SessionId,
			"processing_time_ms": processingTime,
			"turn_count":         resp.TurnCount,
		},
	})

	// Step 4: Capture and return successful response
	// Capture response for audit (FOSS-008)
	respBytes, _ := json.Marshal(resp)
	_ = h.opts.RequestAuditor.CaptureResponse(ctx, auditID, &extensions.AuditableResponse{
		StatusCode: http.StatusOK,
		Headers:    extensions.HTTPHeaders{"Content-Type": "application/json"},
		Body:       respBytes,
		Timestamp:  time.Now().UTC(),
	})

	c.JSON(http.StatusOK, resp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// extractHeaders converts Gin request headers to HTTPHeaders for audit capture.
//
// # Description
//
// Extracts relevant headers from the Gin context for audit logging.
// Sensitive headers like Authorization are intentionally excluded to
// avoid storing credentials in audit logs.
//
// # Inputs
//
//   - c: Gin context with HTTP request
//
// # Outputs
//
//   - extensions.HTTPHeaders: Extracted headers safe for audit storage
func extractHeaders(c *gin.Context) extensions.HTTPHeaders {
	headers := extensions.HTTPHeaders{}

	// Extract safe headers (exclude Authorization to avoid credential logging)
	if ct := c.GetHeader("Content-Type"); ct != "" {
		headers["Content-Type"] = ct
	}
	if ua := c.GetHeader("User-Agent"); ua != "" {
		headers["User-Agent"] = ua
	}
	if xfwd := c.GetHeader("X-Forwarded-For"); xfwd != "" {
		headers["X-Forwarded-For"] = xfwd
	}
	if reqID := c.GetHeader("X-Request-ID"); reqID != "" {
		headers["X-Request-ID"] = reqID
	}

	return headers
}
