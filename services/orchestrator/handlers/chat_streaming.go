// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// =============================================================================
// STREAMING CHAT MODULE - PIPELINE FEATURE PARITY
// =============================================================================
//
// This module implements SSE streaming for multiple RAG pipelines. When adding
// new pipelines (REFRAG, GraphRAG, etc.), ensure feature parity with the
// reference implementation (HandleChatRAGStream).
//
// # Feature Parity Matrix
//
// | Feature                    | Direct | RAG (reranking) | Verified | REFRAG | GraphRAG |
// |----------------------------|--------|-----------------|----------|--------|----------|
// | SSE streaming              | ✅     | ✅              | ✅       | TODO   | TODO     |
// | Session ID in done event   | ✅     | ✅              | ✅       | TODO   | TODO     |
// | Turn persistence           | ❌ (1) | ✅              | ✅       | TODO   | TODO     |
// | Session history loading    | ❌ (1) | ✅              | ❌ (2)   | TODO   | TODO     |
// | History in LLM prompt      | ❌ (1) | ✅              | ❌ (2)   | TODO   | TODO     |
// | PII audit (outbound)       | ✅     | ✅              | ✅       | TODO   | TODO     |
// | PII audit (RAG context)    | ❌     | ✅              | ❌ (3)   | TODO   | TODO     |
// | PII audit (answer)         | ❌     | ✅              | ✅       | TODO   | TODO     |
// | Heartbeat keepalive        | ✅     | ✅              | ❌ (4)   | TODO   | TODO     |
// | Hash chain integrity       | ❌     | ✅              | ✅ (5)   | TODO   | TODO     |
// | Debate log persistence     | N/A    | N/A             | ❌ (6)   | N/A    | N/A      |
//
// # Notes
//
// (1) Direct chat intentionally doesn't persist turns - no session context.
// (2) Verified pipeline proxies to Python which builds prompts internally.
//     To support --resume with conversation context:
//     a) Go must load history from Weaviate (loadSessionHistory)
//     b) Go must pass history to Python in request body
//     c) Python must include history in Optimist/Skeptic/Refiner prompts
// (3) Verified does RAG context audit in Python, not Go.
// (4) Verified streams from Python which handles its own timeouts.
// (5) Verified uses simple SHA256 of final answer (not incremental token hashing).
// (6) Skeptic/Optimist debate details are logged in Python but not persisted
//     to Weaviate. Future: expose via `session verify --full` command.
//
// # Adding a New Pipeline
//
// When implementing a new pipeline handler (e.g., HandleREFRAGStream):
//
// 1. Copy the structure from HandleChatRAGStream (reference implementation)
// 2. Implement all ✅ features from the matrix above
// 3. Add session history loading (Step 2.5 in HandleChatRAGStream)
// 4. Add turn persistence (Step 10.5-10.6 in HandleChatRAGStream)
// 5. Update the feature matrix in this comment
// 6. Add tests for the new handler
//
// # Related Files
//
// - services/rag_engine/pipelines/*.py - Python pipeline implementations
// - services/orchestrator/services/chat_rag.go - RAG service client
// - cmd/aleutian/commands/chat.go - CLI chat command
//
// =============================================================================

package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/extensions"
	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/conversation"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/middleware"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/observability"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/services"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// heartbeatInterval is the interval for sending keepalive pings.
	// Set to 15s to stay well under typical LB timeouts (60s for ALB/Nginx).
	heartbeatInterval = 15 * time.Second

	// maxHistoryTurns limits the number of conversation turns loaded for session resume.
	// This prevents context window overflow for long conversations.
	maxHistoryTurns = 20
)

// =============================================================================
// Streaming Callback Types
// =============================================================================

// StreamCallback is called for each token or event during streaming.
//
// # Description
//
// StreamCallback receives tokens as they are generated by the LLM.
// The callback should write each token to the SSE stream.
// Return an error to abort streaming (e.g., on client disconnect).
//
// # Inputs
//
//   - event: Stream event containing token content, thinking, or error.
//
// # Outputs
//
//   - error: Non-nil to abort streaming.
//
// # Examples
//
//	callback := func(event llm.StreamEvent) error {
//	    return sseWriter.WriteToken(event.Content)
//	}
//
// # Limitations
//
//   - Must be safe to call from any goroutine.
//
// # Assumptions
//
//   - Called in token order.
type StreamCallback = llm.StreamCallback

// =============================================================================
// Session History Types
// =============================================================================

// ConversationTurn represents a single Q&A exchange in a session.
//
// # Description
//
// A ConversationTurn captures one user question and the assistant's response.
// Used for loading session history to provide context to the LLM when
// resuming a conversation.
//
// # Fields
//
//   - Question: The user's input message
//   - Answer: The assistant's response
//   - Timestamp: When the turn occurred (Unix milliseconds, for ordering)
//
// # Examples
//
//	turn := ConversationTurn{
//	    Question:  "What is OAuth?",
//	    Answer:    "OAuth is an authorization framework...",
//	    Timestamp: 1704067200000,
//	}
//
// # Limitations
//
//   - Does not include sources or metadata from RAG responses
//
// # Assumptions
//
//   - Timestamp is in Unix milliseconds (matches Weaviate schema)
type ConversationTurn struct {
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Timestamp int64  `json:"timestamp"`
}

// WeaviateConversationResponse represents the typed GraphQL response from Weaviate.
//
// # Description
//
// This struct provides compile-time type safety for parsing Weaviate's
// GraphQL response format when querying the Conversation class.
//
// # Fields
//
//   - Get: Contains the query results
//   - Get.Conversation: Array of conversation turns
//
// # Limitations
//
//   - Specific to Conversation class queries
//
// # Assumptions
//
//   - Response follows standard Weaviate GraphQL format
type WeaviateConversationResponse struct {
	Get struct {
		Conversation []ConversationTurn `json:"Conversation"`
	} `json:"Get"`
}

// =============================================================================
// Interface Definition
// =============================================================================

// StreamingChatHandler defines the contract for handling streaming chat HTTP requests.
//
// # Description
//
// StreamingChatHandler abstracts streaming chat endpoint handling, enabling different
// implementations and facilitating testing via mocks. The interface provides
// Server-Sent Events (SSE) streaming endpoints.
//
// # Security Model
//
// - Outbound (user → system): Blocked if contains sensitive data (policy engine)
// - Inbound (system → user): Allowed, logged for audit via hash chain
// - RAG context: Logged if contains PII (future: granite-guardian integration)
//
// # Thread Safety
//
// Implementations must be safe for concurrent use by multiple goroutines.
// HTTP handlers are called concurrently by the Gin framework.
//
// # Limitations
//
//   - Requires LLM client that supports streaming (ChatStream method)
//   - Client must support SSE (EventSource or similar)
//
// # Assumptions
//
//   - All dependencies are properly initialized before handler use
//   - Gin context is valid and not nil
//   - LLM client implements ChatStream method
type StreamingChatHandler interface {
	// HandleDirectChatStream processes direct LLM chat requests with SSE streaming.
	//
	// # Description
	//
	// Handles POST /v1/chat/direct/stream requests. Streams tokens as they
	// are generated by the LLM via Server-Sent Events.
	//
	// # Inputs
	//
	//   - c: Gin context containing the HTTP request.
	//
	// # Outputs
	//
	// SSE stream with events:
	//   - status: Processing status updates
	//   - token: Generated tokens
	//   - thinking: Extended thinking content (if enabled)
	//   - done: Stream completion with session ID
	//   - error: Error events (if failure occurs)
	//
	// # Limitations
	//
	//   - No sources event (direct chat has no RAG)
	//
	// # Assumptions
	//
	//   - Client supports SSE
	HandleDirectChatStream(c *gin.Context)

	// HandleChatRAGStream processes conversational RAG requests with SSE streaming.
	//
	// # Description
	//
	// Handles POST /v1/chat/rag/stream requests. Retrieves context from
	// vector database, then streams LLM response via Server-Sent Events.
	//
	// # Inputs
	//
	//   - c: Gin context containing the HTTP request.
	//
	// # Outputs
	//
	// SSE stream with events:
	//   - status: Processing status updates
	//   - sources: Retrieved documents with scores
	//   - token: Generated tokens
	//   - done: Stream completion with session ID
	//   - error: Error events (if failure occurs)
	//
	// # Limitations
	//
	//   - Requires Weaviate client for RAG retrieval
	//
	// # Assumptions
	//
	//   - Client supports SSE
	//   - RAG service is available
	HandleChatRAGStream(c *gin.Context)

	// HandleVerifiedRAGStream processes verified RAG requests with progress streaming.
	//
	// # Description
	//
	// Handles POST /v1/chat/rag/verified/stream requests. Runs the skeptic/optimist
	// debate and streams progress events in real-time via SSE. This enables users
	// to see the verification process as it happens.
	//
	// # Inputs
	//
	//   - c: Gin context containing the HTTP request.
	//
	// # Outputs
	//
	// SSE stream with events:
	//   - progress: Debate progress updates (retrieval, draft, skeptic audit, etc.)
	//   - answer: Final verified answer with sources
	//   - done: Stream completion
	//   - error: Error events (if failure occurs)
	//
	// # Verbosity Levels
	//
	// The CLI controls verbosity via the "X-Verbosity" header:
	//   - 0: Silent (no progress events forwarded)
	//   - 1: Summary (message only)
	//   - 2: Detailed (message + details + OTel trace link)
	//
	// # Limitations
	//
	//   - Requires Python RAG engine's /rag/verified/stream endpoint
	//   - Client must support SSE
	//
	// # Assumptions
	//
	//   - RAG engine is available and running
	//   - Client supports SSE
	HandleVerifiedRAGStream(c *gin.Context)
}

// =============================================================================
// Struct Definition
// =============================================================================

// streamingChatHandler implements StreamingChatHandler for production use.
//
// # Description
//
// streamingChatHandler coordinates between HTTP layer and streaming business logic.
// It performs HTTP-related tasks and delegates LLM streaming to injected services:
//   - Request parsing and validation
//   - SSE header configuration
//   - Stream event emission
//   - Error handling and cleanup
//
// # Fields
//
//   - llmClient: LLM client with streaming support (must implement ChatStream)
//   - policyEngine: Policy engine for sensitive data scanning
//   - ragService: Service for RAG chat processing (may be nil)
//   - tracer: OpenTelemetry tracer for distributed tracing
//
// # Thread Safety
//
// Thread-safe. All fields are read-only after construction.
// No shared mutable state between requests.
//
// # Limitations
//
//   - Requires LLM client that supports ChatStream method
//   - RAG streaming requires ragService to be non-nil
//
// # Assumptions
//
//   - Dependencies are non-nil and properly configured
//   - LLM client supports streaming
type streamingChatHandler struct {
	llmClient          llm.LLMClient
	policyEngine       *policy_engine.PolicyEngine
	ragService         *services.ChatRAGService
	weaviateClient     *weaviate.Client
	tracer             trace.Tracer
	queryExpander      conversation.QueryExpander
	contextualEmbedder conversation.ContextBuilder
	opts               extensions.ServiceOptions
}

// =============================================================================
// Constructor
// =============================================================================

// NewStreamingChatHandler creates a StreamingChatHandler with the provided dependencies.
//
// # Description
//
// Creates a fully configured streamingChatHandler for production use.
// All dependencies must be properly initialized before calling.
// Panics if llmClient or policyEngine is nil (programming errors).
//
// # Inputs
//
//   - llmClient: LLM client with streaming support. Must not be nil.
//     Must implement ChatStream method for streaming to work.
//   - policyEngine: Policy scanner. Must not be nil.
//   - ragService: RAG chat service. May be nil if RAG is not used.
//   - weaviateClient: Weaviate client for session history. May be nil if
//     session resume is not needed.
//   - opts: Extension options for enterprise features (auth, audit, filter).
//
// # Outputs
//
//   - StreamingChatHandler: Ready for use with Gin router
//
// # Examples
//
//	handler := handlers.NewStreamingChatHandler(llmClient, policyEngine, ragService, weaviateClient, opts)
//	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)
//	router.POST("/v1/chat/rag/stream", handler.HandleChatRAGStream)
//
// # Limitations
//
//   - Panics on nil llmClient or policyEngine
//   - Session resume requires weaviateClient to be non-nil
//
// # Assumptions
//
//   - llmClient and policyEngine are non-nil and ready for use
//   - llmClient supports ChatStream method
func NewStreamingChatHandler(
	llmClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine,
	ragService *services.ChatRAGService,
	weaviateClient *weaviate.Client,
	opts extensions.ServiceOptions,
) StreamingChatHandler {
	if llmClient == nil {
		panic("NewStreamingChatHandler: llmClient must not be nil")
	}
	if policyEngine == nil {
		panic("NewStreamingChatHandler: policyEngine must not be nil")
	}

	// Create generate function for conversation components (P8)
	// Using a closure eliminates the need for adapter structs
	generateFunc := func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		temp := float32(0.2) // Low temperature for deterministic expansion/summarization
		params := llm.GenerationParams{
			Temperature: &temp,
			MaxTokens:   &maxTokens,
		}
		return llmClient.Generate(ctx, prompt, params)
	}

	// Initialize query expander (P8 - conversational context enhancement)
	var queryExpander conversation.QueryExpander
	expansionConfig := conversation.DefaultExpansionConfig()
	if expansionConfig.Enabled {
		queryExpander = conversation.NewLLMQueryExpander(generateFunc, expansionConfig)
		slog.Info("Query expansion enabled",
			"timeout_ms", expansionConfig.TimeoutMs,
			"max_tokens", expansionConfig.MaxTokens,
		)
	}

	// Initialize contextual embedder (P8 - embedding with history context)
	var contextualEmbedder conversation.ContextBuilder
	contextConfig := conversation.DefaultContextConfig()
	if contextConfig.Enabled {
		contextualEmbedder = conversation.NewContextualEmbedder(generateFunc, contextConfig)
		slog.Info("Contextual embedding enabled",
			"summarization", contextConfig.SummarizationEnabled,
			"max_chars", contextConfig.MaxChars,
		)
	}

	return &streamingChatHandler{
		llmClient:          llmClient,
		policyEngine:       policyEngine,
		ragService:         ragService,
		weaviateClient:     weaviateClient,
		tracer:             otel.Tracer("aleutian.orchestrator.handlers.chat_streaming"),
		queryExpander:      queryExpander,
		contextualEmbedder: contextualEmbedder,
		opts:               opts,
	}
}

// =============================================================================
// Handler Methods
// =============================================================================

// HandleDirectChatStream processes direct LLM chat requests with SSE streaming.
//
// # Description
//
// Handles POST /v1/chat/direct/stream requests. The flow is:
//  1. Parse and validate request body
//  2. Scan last user message for policy violations (outbound protection)
//  3. Set SSE headers and create writer
//  4. Emit status event
//  5. Stream tokens from LLM via ChatStream
//  6. Emit done event with session info
//
// # Security
//
// - Outbound (user → LLM): Scanned and blocked if contains sensitive data
// - Inbound (LLM → user): Allowed, logged via hash chain for async audit
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
//
// # Outputs
//
// SSE Events:
//   - event: status, data: {"type":"status","message":"Generating response..."}
//   - event: token, data: {"type":"token","content":"Hello"}
//   - event: thinking, data: {"type":"thinking","content":"Let me think..."}
//   - event: done, data: {"type":"done","session_id":"..."}
//   - event: error, data: {"type":"error","error":"..."}
//
// HTTP Status (before streaming starts):
//   - 400 Bad Request: Invalid request body or validation failure
//   - 403 Forbidden: Policy violation detected (sensitive data in outbound message)
//   - 500 Internal Server Error: SSE setup failure
//
// # Examples
//
// Request:
//
//	POST /v1/chat/direct/stream
//	Accept: text/event-stream
//	{
//	    "request_id": "550e8400-e29b-41d4-a716-446655440000",
//	    "timestamp": 1735817400000,
//	    "messages": [{"role": "user", "content": "Hello"}]
//	}
//
// Response (SSE stream):
//
//	event: status
//	data: {"type":"status","message":"Generating response...","id":"...","created_at":...}
//
//	event: token
//	data: {"type":"token","content":"Hello","id":"...","created_at":...}
//
//	event: done
//	data: {"type":"done","session_id":"...","id":"...","created_at":...}
//
// # Limitations
//
//   - Only scans last user message for policy (not full history)
//   - Errors during streaming are sent as events, not HTTP errors
//
// # Assumptions
//
//   - Request body is valid JSON
//   - LLM client supports ChatStream method
//   - Client supports SSE
//
// # Security References
//
//   - SEC-003: Message size limits enforced via validation
//   - SEC-005: Internal errors not exposed to client
func (h *streamingChatHandler) HandleDirectChatStream(c *gin.Context) {
	startTime := time.Now()
	endpoint := observability.EndpointDirectStream

	ctx, span := h.tracer.Start(c.Request.Context(), "HandleDirectChatStream")
	defer span.End()

	// Track active stream (for metrics)
	if m := observability.DefaultMetrics; m != nil {
		m.StreamStarted(endpoint)
		defer m.StreamEnded(endpoint)
	}

	success := false
	defer func() {
		// Record final metrics
		if m := observability.DefaultMetrics; m != nil {
			duration := time.Since(startTime).Seconds()
			m.RecordRequest(endpoint, success)
			m.RecordStreamDuration(endpoint, duration, success)
		}
	}()

	// Step 0: Get authenticated user from context (FOSS-003)
	// Auth middleware has already validated the token and stored AuthInfo
	authInfo := middleware.GetAuthInfo(c)
	userID := "anonymous"
	if authInfo != nil {
		userID = authInfo.UserID
	}
	span.SetAttributes(attribute.String("user.id", userID))

	// Step 0.5: Read raw body for enterprise request capture (FOSS-008)
	rawBody, bodyErr := io.ReadAll(c.Request.Body)
	if bodyErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Step 1: Parse request body
	var req datatypes.DirectChatRequest
	if err := c.BindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		slog.Error("Failed to parse streaming chat request", "error", err)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeValidation)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Add request attributes to span
	span.SetAttributes(
		attribute.String("request.id", req.RequestID),
		attribute.Int("request.message_count", len(req.Messages)),
		attribute.Bool("request.thinking_enabled", req.EnableThinking),
	)

	// Step 2: Validate request
	if err := req.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		slog.Error("Streaming request validation failed",
			"error", err,
			"requestId", req.RequestID,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeValidation)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: validation failed"})
		return
	}

	// Step 2.5: Authorization check (FOSS-004)
	// Enterprise can restrict who can send streaming chat messages
	if err := h.opts.AuthzProvider.Authorize(ctx, extensions.AuthzRequest{
		User:         authInfo,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "direct/stream",
	}); err != nil {
		span.SetStatus(codes.Error, "authorization denied")
		// Log unauthorized attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "authz.denied",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "direct/stream",
			Outcome:      "denied",
			Metadata: map[string]any{
				"request_id": req.RequestID,
				"reason":     err.Error(),
			},
		})
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Step 2.6: Capture request for enterprise audit (FOSS-008)
	auditID, _ := h.opts.RequestAuditor.CaptureRequest(ctx, &extensions.AuditableRequest{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Headers:   extractHeaders(c),
		Body:      rawBody,
		UserID:    userID,
		Timestamp: startTime,
	})

	// Step 3: Scan last user message for policy violations (OUTBOUND protection)
	// This prevents users from sending sensitive data OUT to the LLM
	lastIdx := len(req.Messages) - 1
	if lastIdx >= 0 {
		lastMsg := req.Messages[lastIdx]
		if lastMsg.Role == "user" {
			// Step 3a: Policy engine scan
			findings := h.policyEngine.ScanFileContent(lastMsg.Content)
			if len(findings) > 0 {
				span.SetAttributes(attribute.Int("policy.findings_count", len(findings)))
				slog.Warn("Blocked streaming chat: user attempting to send sensitive data",
					"findings_count", len(findings),
					"requestId", req.RequestID,
				)
				if m := observability.DefaultMetrics; m != nil {
					m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
				}
				c.JSON(http.StatusForbidden, gin.H{
					"error":    "Policy Violation: Message contains sensitive data.",
					"findings": findings,
				})
				return
			}

			// Step 3b: Apply message filter (FOSS-006)
			// Enterprise can implement custom filtering (PII redaction, etc.)
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
					ResourceType: "chat",
					ResourceID:   "direct/stream",
					Outcome:      "blocked",
					Metadata: map[string]any{
						"request_id": req.RequestID,
						"reason":     filterResult.BlockReason,
					},
				})
				if m := observability.DefaultMetrics; m != nil {
					m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
				}
				c.JSON(http.StatusForbidden, gin.H{
					"error":  "Message blocked by content filter",
					"reason": filterResult.BlockReason,
				})
				return
			}

			// Use filtered content (may have PII redacted)
			req.Messages[lastIdx].Content = filterResult.Filtered
		}
	}

	// Step 4: Set SSE headers and create writer
	SetSSEHeaders(c.Writer)
	sseWriter, err := NewSSEWriter(c.Writer)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "SSE setup failed")
		slog.Error("Failed to create SSE writer",
			"error", err,
			"requestId", req.RequestID,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeInternal)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	// Step 5: Emit status event
	if err := sseWriter.WriteStatus("Generating response..."); err != nil {
		span.RecordError(err)
		slog.Error("Failed to write status event",
			"error", err,
			"requestId", req.RequestID,
		)
		return
	}

	// Step 6: Start heartbeat goroutine to prevent connection timeouts
	heartbeatDone := make(chan struct{})
	go h.runHeartbeat(ctx, sseWriter, endpoint, heartbeatDone)

	// Step 7: Stream tokens from LLM
	// Inbound content (LLM → user) is allowed and logged via hash chain
	// Note: Direct chat doesn't persist turns (no session context).
	// Use HandleChatRAGStream for session-based persistence.
	params := llm.GenerationParams{
		EnableThinking:  req.EnableThinking,
		BudgetTokens:    req.BudgetTokens,
		ToolDefinitions: req.Tools,
	}

	// Step 7.5: Create accumulator for enterprise response capture (FOSS-008)
	accumulator, accErr := NewSecureTokenAccumulator()
	if accErr != nil {
		slog.Debug("failed to create token accumulator for capture", "error", accErr)
	}
	defer func() {
		if accumulator != nil {
			accumulator.Destroy()
		}
	}()

	var tokenCount int32
	firstTokenTime := time.Time{}
	streamErr := h.streamFromLLMWithMetrics(ctx, req.RequestID, req.Messages, params, sseWriter, endpoint, &tokenCount, &firstTokenTime, accumulator)

	// Stop heartbeat
	close(heartbeatDone)

	if streamErr != nil {
		span.RecordError(streamErr)
		span.SetStatus(codes.Error, "LLM streaming failed")
		span.SetAttributes(attribute.Int("stream.token_count", int(tokenCount)))
		slog.Error("LLM streaming failed",
			"error", streamErr,
			"requestId", req.RequestID,
			"tokenCount", tokenCount,
		)

		// Log failed streaming attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "chat.stream",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "direct/stream",
			Outcome:      "failed",
			Metadata: map[string]any{
				"request_id":  req.RequestID,
				"error":       streamErr.Error(),
				"token_count": fmt.Sprintf("%d", tokenCount),
			},
		})

		// Categorize error for metrics
		if errors.Is(streamErr, context.Canceled) {
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodeClientDisconnect)
				m.RecordClientDisconnect(endpoint)
			}
		} else {
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodeLLMError)
			}
		}
		// Error already sent via SSE
		return
	}

	// Record time to first token
	if !firstTokenTime.IsZero() {
		ttft := firstTokenTime.Sub(startTime).Seconds()
		span.SetAttributes(attribute.Float64("stream.time_to_first_token_seconds", ttft))
		if m := observability.DefaultMetrics; m != nil {
			m.RecordTimeToFirstToken(endpoint, ttft)
		}
	}

	span.SetAttributes(attribute.Int("stream.token_count", int(tokenCount)))

	// Step 8: Emit done event
	if err := sseWriter.WriteDone(req.RequestID); err != nil {
		span.RecordError(err)
		slog.Error("Failed to write done event",
			"error", err,
			"requestId", req.RequestID,
		)
		return
	}

	// Step 8.5: Capture response for enterprise audit (FOSS-008)
	if accumulator != nil {
		answer, _, _ := accumulator.Finalize()
		_ = h.opts.RequestAuditor.CaptureResponse(ctx, auditID, &extensions.AuditableResponse{
			StatusCode: http.StatusOK,
			Headers:    extensions.HTTPHeaders{"Content-Type": "text/event-stream"},
			Body:       []byte(answer),
			Timestamp:  time.Now().UTC(),
		})
	}

	// Step 9: Log successful streaming (FOSS-005)
	processingTime := time.Since(startTime).Milliseconds()
	_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
		EventType:    "chat.stream",
		Timestamp:    time.Now().UTC(),
		UserID:       userID,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "direct/stream",
		Outcome:      "success",
		Metadata: map[string]any{
			"request_id":       req.RequestID,
			"token_count":      fmt.Sprintf("%d", tokenCount),
			"processing_ms":    fmt.Sprintf("%d", processingTime),
			"thinking_enabled": fmt.Sprintf("%t", req.EnableThinking),
		},
	})

	success = true
	span.SetStatus(codes.Ok, "stream completed successfully")
}

// HandleChatRAGStream processes conversational RAG requests with SSE streaming.
//
// # Description
//
// Handles POST /v1/chat/rag/stream requests. The flow is:
//  1. Parse request body
//  2. Scan user message for policy violations (outbound protection)
//  3. Set SSE headers and create writer
//  4. Emit status event for retrieval
//  5. Retrieve context from RAG service
//  6. Scan and log if retrieved context contains PII (audit trail)
//  7. Emit sources event
//  8. Emit status event for generation
//  9. Stream tokens from LLM via ChatStream
//  10. Emit done event with session ID
//
// # Security
//
//   - Outbound (user → system): Scanned and blocked if contains sensitive data
//   - RAG context (DB → LLM): Scanned and LOGGED if contains PII (audit trail)
//     Future: granite-guardian integration for user acknowledgment flow
//   - Inbound (LLM → user): Allowed, logged via hash chain for async audit
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
//
// # Outputs
//
// SSE Events:
//   - event: status, data: {"type":"status","message":"Searching knowledge base..."}
//   - event: sources, data: {"type":"sources","sources":[...]}
//   - event: status, data: {"type":"status","message":"Generating response..."}
//   - event: token, data: {"type":"token","content":"..."}
//   - event: done, data: {"type":"done","session_id":"..."}
//   - event: error, data: {"type":"error","error":"..."}
//
// HTTP Status (before streaming starts):
//   - 400 Bad Request: Invalid request body
//   - 403 Forbidden: Policy violation detected (sensitive data in outbound message)
//   - 500 Internal Server Error: RAG service not available or SSE setup failure
//
// # Examples
//
// Request:
//
//	POST /v1/chat/rag/stream
//	Accept: text/event-stream
//	{"message": "What is OAuth?", "pipeline": "reranking"}
//
// Response (SSE stream):
//
//	event: status
//	data: {"type":"status","message":"Searching knowledge base..."}
//
//	event: sources
//	data: {"type":"sources","sources":[{"source":"oauth.md","score":0.95}]}
//
//	event: status
//	data: {"type":"status","message":"Generating response..."}
//
//	event: token
//	data: {"type":"token","content":"OAuth"}
//
//	event: done
//	data: {"type":"done","session_id":"sess-abc123"}
//
// # Limitations
//
//   - Requires ragService to be non-nil
//   - Errors during streaming are sent as events, not HTTP errors
//
// # Assumptions
//
//   - RAG service and Weaviate are available
//   - LLM client supports ChatStream method
//   - Client supports SSE
//
// # Security References
//
//   - SEC-005: Internal errors not exposed to client
func (h *streamingChatHandler) HandleChatRAGStream(c *gin.Context) {
	startTime := time.Now()
	endpoint := observability.EndpointRAGStream

	ctx, span := h.tracer.Start(c.Request.Context(), "HandleChatRAGStream")
	defer span.End()

	// Track active stream (for metrics)
	if m := observability.DefaultMetrics; m != nil {
		m.StreamStarted(endpoint)
		defer m.StreamEnded(endpoint)
	}

	success := false
	defer func() {
		// Record final metrics
		if m := observability.DefaultMetrics; m != nil {
			duration := time.Since(startTime).Seconds()
			m.RecordRequest(endpoint, success)
			m.RecordStreamDuration(endpoint, duration, success)
		}
	}()

	// Step 0: Get authenticated user from context (FOSS-003)
	// Auth middleware has already validated the token and stored AuthInfo
	authInfo := middleware.GetAuthInfo(c)
	userID := "anonymous"
	if authInfo != nil {
		userID = authInfo.UserID
	}
	span.SetAttributes(attribute.String("user.id", userID))

	// Check if RAG service is available
	if h.ragService == nil {
		slog.Error("RAG service not configured for streaming")
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeInternal)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "RAG service not available"})
		return
	}

	// Step 0.5: Read raw body for enterprise request capture (FOSS-008)
	rawBody, bodyErr := io.ReadAll(c.Request.Body)
	if bodyErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Step 1: Parse request body
	var req datatypes.ChatRAGRequest
	if err := c.BindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		slog.Error("Failed to parse streaming RAG request", "error", err)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeValidation)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Add request attributes to span
	span.SetAttributes(
		attribute.String("request.id", req.Id),
		attribute.String("request.pipeline", req.Pipeline),
		attribute.String("request.session_id", req.SessionId),
	)

	// Step 1.5: Authorization check (FOSS-004)
	// Enterprise can restrict who can send streaming RAG messages
	if err := h.opts.AuthzProvider.Authorize(ctx, extensions.AuthzRequest{
		User:         authInfo,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "rag/stream",
	}); err != nil {
		span.SetStatus(codes.Error, "authorization denied")
		// Log unauthorized attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "authz.denied",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "rag/stream",
			Outcome:      "denied",
			Metadata: map[string]any{
				"request_id": req.Id,
				"reason":     err.Error(),
			},
		})
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Step 1.6: Capture request for enterprise audit (FOSS-008)
	auditID, _ := h.opts.RequestAuditor.CaptureRequest(ctx, &extensions.AuditableRequest{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Headers:   extractHeaders(c),
		Body:      rawBody,
		UserID:    userID,
		Timestamp: startTime,
	})

	// Step 2: Scan user message for policy violations (OUTBOUND protection)
	// This prevents users from sending sensitive data OUT
	// Step 2a: Policy engine scan
	findings := h.policyEngine.ScanFileContent(req.Message)
	if len(findings) > 0 {
		span.SetAttributes(attribute.Int("policy.findings_count", len(findings)))
		slog.Warn("Blocked streaming RAG: user attempting to send sensitive data",
			"findings_count", len(findings),
			"requestId", req.Id,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
		}
		c.JSON(http.StatusForbidden, gin.H{
			"error":    "Policy Violation: Message contains sensitive data.",
			"findings": findings,
		})
		return
	}

	// Step 2b: Apply message filter (FOSS-006)
	// Enterprise can implement custom filtering (PII redaction, etc.)
	if req.Message != "" {
		filterResult, filterErr := h.opts.MessageFilter.FilterInput(ctx, req.Message)
		if filterErr != nil {
			slog.Error("Message filter failed", "error", filterErr, "requestId", req.Id)
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
				ResourceType: "chat",
				ResourceID:   "rag/stream",
				Outcome:      "blocked",
				Metadata: map[string]any{
					"request_id": req.Id,
					"reason":     filterResult.BlockReason,
				},
			})
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
			}
			c.JSON(http.StatusForbidden, gin.H{
				"error":  "Message blocked by content filter",
				"reason": filterResult.BlockReason,
			})
			return
		}

		// Use filtered content (may have PII redacted)
		req.Message = filterResult.Filtered
	}

	// Step 2.5: Load session history for session resume
	// This enables conversation continuity when resuming a session
	var sessionHistory []ConversationTurn
	if req.SessionId != "" {
		var historyErr error
		sessionHistory, historyErr = h.loadSessionHistory(ctx, req.SessionId)
		if historyErr != nil {
			// Log warning but continue - don't fail the request
			slog.Warn("failed to load session history, continuing without history",
				"session_id", req.SessionId,
				"error", historyErr,
			)
		}
		span.SetAttributes(attribute.Int("session.history_turns", len(sessionHistory)))
	}

	// Step 3: Set SSE headers and create writer
	SetSSEHeaders(c.Writer)
	sseWriter, err := NewSSEWriter(c.Writer)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "SSE setup failed")
		slog.Error("Failed to create SSE writer",
			"error", err,
			"requestId", req.Id,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeInternal)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	// Step 4: Emit status event for retrieval
	if err := sseWriter.WriteStatus("Searching knowledge base..."); err != nil {
		span.RecordError(err)
		slog.Error("Failed to write retrieval status event",
			"error", err,
			"requestId", req.Id,
		)
		return
	}

	// Step 5: Start heartbeat for RAG retrieval (can be slow)
	heartbeatDone := make(chan struct{})
	go h.runHeartbeat(ctx, sseWriter, endpoint, heartbeatDone)

	// Step 6: Retrieve context from RAG service
	ragCtx, sources, err := h.retrieveRAGContext(ctx, &req)
	if err != nil {
		close(heartbeatDone)
		span.RecordError(err)
		span.SetStatus(codes.Error, "RAG retrieval failed")
		slog.Error("RAG retrieval failed",
			"error", err,
			"requestId", req.Id,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeRAGError)
		}
		_ = sseWriter.WriteError("Failed to retrieve context")
		return
	}

	span.SetAttributes(attribute.Int("rag.sources_count", len(sources)))

	// Step 7: Scan retrieved context for PII (AUDIT logging, not blocking)
	// This logs when database content contains sensitive data being sent to LLM
	// Future: integrate granite-guardian for user acknowledgment flow
	h.auditRAGContextForPII(req.Id, ragCtx, sources)

	// Step 7.5: Apply recency bias if configured
	if req.RecencyBias != "" && req.RecencyBias != "none" {
		decayRate := datatypes.GetRecencyDecayRate(req.RecencyBias)
		if decayRate > 0 {
			sources = datatypes.ApplyRecencyDecay(sources, decayRate)
			slog.Debug("Applied recency bias to sources",
				"requestId", req.Id,
				"recency_bias", req.RecencyBias,
				"decay_rate", decayRate,
			)
		}
	}

	// Step 8: Emit sources event
	if err := sseWriter.WriteSources(sources); err != nil {
		close(heartbeatDone)
		span.RecordError(err)
		slog.Error("Failed to write sources event",
			"error", err,
			"requestId", req.Id,
		)
		return
	}

	// Step 9: Emit status event for generation
	if err := sseWriter.WriteStatus("Generating response..."); err != nil {
		close(heartbeatDone)
		span.RecordError(err)
		slog.Error("Failed to write generation status event",
			"error", err,
			"requestId", req.Id,
		)
		return
	}

	// Step 10: Build messages with RAG context and session history, then stream
	// Inbound content (LLM → user) is allowed and logged via hash chain
	messages := h.buildRAGMessagesWithHistory(ragCtx, req.Message, sessionHistory, req.StrictMode)
	params := llm.GenerationParams{}

	// Step 10.5: Create secure token accumulator for turn persistence
	// Tokens are accumulated in mlocked memory with incremental hashing
	accumulator, accErr := NewSecureTokenAccumulator()
	if accErr != nil {
		// Log warning but continue without persistence
		slog.Warn("failed to create token accumulator, turn will not be persisted",
			"requestId", req.Id,
			"error", accErr,
		)
	}
	defer func() {
		if accumulator != nil {
			accumulator.Destroy()
		}
	}()

	var tokenCount int32
	firstTokenTime := time.Time{}
	streamErr := h.streamFromLLMWithMetrics(ctx, req.Id, messages, params, sseWriter, endpoint, &tokenCount, &firstTokenTime, accumulator)

	// Stop heartbeat
	close(heartbeatDone)

	if streamErr != nil {
		span.RecordError(streamErr)
		span.SetStatus(codes.Error, "LLM streaming failed")
		span.SetAttributes(attribute.Int("stream.token_count", int(tokenCount)))
		slog.Error("RAG LLM streaming failed",
			"error", streamErr,
			"requestId", req.Id,
			"tokenCount", tokenCount,
		)

		// Log failed streaming attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "chat.stream",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "rag/stream",
			Outcome:      "failed",
			Metadata: map[string]any{
				"request_id":  req.Id,
				"session_id":  req.SessionId,
				"error":       streamErr.Error(),
				"token_count": fmt.Sprintf("%d", tokenCount),
			},
		})

		// Categorize error for metrics
		if errors.Is(streamErr, context.Canceled) {
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodeClientDisconnect)
				m.RecordClientDisconnect(endpoint)
			}
		} else {
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodeLLMError)
			}
		}
		// Error already sent via SSE
		return
	}

	// Record time to first token
	if !firstTokenTime.IsZero() {
		ttft := firstTokenTime.Sub(startTime).Seconds()
		span.SetAttributes(attribute.Float64("stream.time_to_first_token_seconds", ttft))
		if m := observability.DefaultMetrics; m != nil {
			m.RecordTimeToFirstToken(endpoint, ttft)
		}
	}

	span.SetAttributes(attribute.Int("stream.token_count", int(tokenCount)))

	// Step 10.6: Persist conversation turn with hash chain
	// This enables session verify to show turn counts and verify integrity
	sessionID := req.SessionId
	if sessionID == "" {
		sessionID = req.Id
	}

	if accumulator != nil {
		// Finalize accumulator to get answer and hash
		answer, turnHash, finalizeErr := accumulator.Finalize()

		// Step 10.6.1: Capture response for enterprise audit (FOSS-008)
		_ = h.opts.RequestAuditor.CaptureResponse(ctx, auditID, &extensions.AuditableResponse{
			StatusCode: http.StatusOK,
			Headers:    extensions.HTTPHeaders{"Content-Type": "text/event-stream"},
			Body:       []byte(answer),
			Timestamp:  time.Now().UTC(),
		})

		if finalizeErr != nil {
			slog.Warn("failed to finalize accumulator, turn will not be persisted",
				"requestId", req.Id,
				"sessionId", sessionID,
				"error", finalizeErr,
			)
		} else if answer != "" {
			// Audit answer for PII before persistence
			shouldBlock, piiFindings := h.auditAnswerForPII(sessionID, answer)
			span.SetAttributes(attribute.Int("pii.findings_count", len(piiFindings)))

			if shouldBlock {
				slog.Warn("turn persistence blocked due to PII in answer",
					"requestId", req.Id,
					"sessionId", sessionID,
					"findingsCount", len(piiFindings),
				)
			} else {
				// Get current turn count to determine next turn number
				currentTurnCount, countErr := h.getTurnCount(ctx, sessionID)
				if countErr != nil {
					slog.Warn("failed to get turn count, using turn number 1",
						"requestId", req.Id,
						"sessionId", sessionID,
						"error", countErr,
					)
					currentTurnCount = 0
				}
				turnNumber := currentTurnCount + 1

				// Persist the conversation turn
				persistErr := h.persistConversationTurn(
					ctx,
					sessionID,
					turnNumber,
					req.Message, // The user's question
					answer,
					turnHash,
				)
				if persistErr != nil {
					slog.Error("failed to persist conversation turn",
						"requestId", req.Id,
						"sessionId", sessionID,
						"turnNumber", turnNumber,
						"error", persistErr,
					)
				} else {
					span.SetAttributes(
						attribute.Int("turn.number", turnNumber),
						attribute.String("turn.hash", turnHash[:16]+"..."), // Truncate for logging
					)
					slog.Info("conversation turn persisted successfully",
						"requestId", req.Id,
						"sessionId", sessionID,
						"turnNumber", turnNumber,
					)

					// Save to semantic memory for future context retrieval
					// Also stores session context (dataspace, pipeline, TTL) for resume functionality
					// If this is the first turn, also generate a session summary
					sessionCtx := datatypes.SessionContext{
						DataSpace: req.DataSpace,
						Pipeline:  req.Pipeline,
						TTL:       req.SessionTTL,
					}
					go SaveMemoryChunkWithSummary(h.llmClient, h.weaviateClient, sessionID, req.Message, answer, turnNumber, sessionCtx)
				}
			}
		}
	}

	// Step 11: Emit done event with session ID
	if err := sseWriter.WriteDone(sessionID); err != nil {
		span.RecordError(err)
		slog.Error("Failed to write done event",
			"error", err,
			"requestId", req.Id,
		)
		return
	}

	// Step 12: Log successful streaming (FOSS-005)
	processingTime := time.Since(startTime).Milliseconds()
	_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
		EventType:    "chat.stream",
		Timestamp:    time.Now().UTC(),
		UserID:       userID,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "rag/stream",
		Outcome:      "success",
		Metadata: map[string]any{
			"request_id":    req.Id,
			"session_id":    sessionID,
			"token_count":   fmt.Sprintf("%d", tokenCount),
			"processing_ms": fmt.Sprintf("%d", processingTime),
			"pipeline":      req.Pipeline,
		},
	})

	success = true
	span.SetStatus(codes.Ok, "stream completed successfully")
}

// HandleVerifiedRAGStream processes verified RAG requests with progress streaming.
//
// # Description
//
// Handles POST /v1/chat/rag/verified/stream requests. This method:
//  1. Parses and validates the request
//  2. Calls Python's /rag/verified/stream endpoint
//  3. Reads SSE events from Python
//  4. Applies verbosity filtering based on X-Verbosity header
//  5. Forwards filtered events to the client
//
// # Security
//
// - Outbound (user → system): Scanned and blocked if contains sensitive data
// - Inbound (system → user): Allowed, logged via hash chain for async audit
//
// # Inputs
//
//   - c: Gin context containing the HTTP request
//
// Request Body (datatypes.ChatRAGRequest):
//   - message: Required. User's query.
//   - session_id: Optional. Existing session to continue.
//   - temperature_overrides: Optional. Per-role temperature overrides.
//   - strictness: Optional. "strict" or "balanced".
//
// Request Headers:
//   - X-Verbosity: Optional. Controls output detail (0, 1, or 2). Default: 2.
//
// # Outputs
//
// SSE Events:
//   - event: progress, data: {"event_type":"...", "message":"...", ...}
//   - event: answer, data: {"answer":"...", "sources":[...], "is_verified":...}
//   - event: done, data: {}
//   - event: error, data: {"error":"..."}
//
// HTTP Status (before streaming starts):
//   - 400 Bad Request: Invalid request body
//   - 403 Forbidden: Policy violation detected
//   - 500 Internal Server Error: SSE setup failure
//   - 503 Service Unavailable: RAG engine not reachable
//
// # Examples
//
// Request:
//
//	POST /v1/chat/rag/verified/stream
//	Accept: text/event-stream
//	X-Verbosity: 2
//	{"message": "What is Detroit known for?"}
//
// Response (SSE stream):
//
//	event: progress
//	data: {"event_type":"retrieval_start","message":"Retrieving documents..."}
//
//	event: progress
//	data: {"event_type":"skeptic_audit_complete","message":"✓ All claims verified!"}
//
//	event: answer
//	data: {"answer":"Detroit is known for...","sources":[...],"is_verified":true}
//
//	event: done
//	data: {}
//
// # Limitations
//
//   - Requires Python RAG engine to be running
//   - Verbosity filtering happens at this layer, not in Python
//
// # Assumptions
//
//   - Python /rag/verified/stream endpoint is available
//   - Client supports SSE
func (h *streamingChatHandler) HandleVerifiedRAGStream(c *gin.Context) {
	startTime := time.Now()
	endpoint := observability.EndpointRAGStream // Reuse existing metric endpoint

	ctx, span := h.tracer.Start(c.Request.Context(), "HandleVerifiedRAGStream")
	defer span.End()

	// Track active stream (for metrics)
	if m := observability.DefaultMetrics; m != nil {
		m.StreamStarted(endpoint)
		defer m.StreamEnded(endpoint)
	}

	success := false
	defer func() {
		if m := observability.DefaultMetrics; m != nil {
			duration := time.Since(startTime).Seconds()
			m.RecordRequest(endpoint, success)
			m.RecordStreamDuration(endpoint, duration, success)
		}
	}()

	// Step 0: Get authenticated user from context (FOSS-003)
	// Auth middleware has already validated the token and stored AuthInfo
	authInfo := middleware.GetAuthInfo(c)
	userID := "anonymous"
	if authInfo != nil {
		userID = authInfo.UserID
	}
	span.SetAttributes(attribute.String("user.id", userID))

	// Step 0.5: Read raw body for enterprise request capture (FOSS-008)
	rawBody, bodyErr := io.ReadAll(c.Request.Body)
	if bodyErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Step 1: Parse request body
	var req datatypes.ChatRAGRequest
	if err := c.BindJSON(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		slog.Error("Failed to parse verified streaming request", "error", err)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeValidation)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Ensure pipeline is set to "verified"
	req.Pipeline = "verified"

	// Add request attributes to span
	span.SetAttributes(
		attribute.String("request.id", req.Id),
		attribute.String("request.pipeline", req.Pipeline),
		attribute.String("request.session_id", req.SessionId),
	)

	// Step 1.5: Authorization check (FOSS-004)
	// Enterprise can restrict who can send verified streaming messages
	if err := h.opts.AuthzProvider.Authorize(ctx, extensions.AuthzRequest{
		User:         authInfo,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "rag/verified/stream",
	}); err != nil {
		span.SetStatus(codes.Error, "authorization denied")
		// Log unauthorized attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "authz.denied",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "rag/verified/stream",
			Outcome:      "denied",
			Metadata: map[string]any{
				"request_id": req.Id,
				"reason":     err.Error(),
			},
		})
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	// Step 1.6: Capture request for enterprise audit (FOSS-008)
	auditID, _ := h.opts.RequestAuditor.CaptureRequest(ctx, &extensions.AuditableRequest{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Headers:   extractHeaders(c),
		Body:      rawBody,
		UserID:    userID,
		Timestamp: startTime,
	})

	// Step 2: Scan user message for policy violations (OUTBOUND protection)
	// Step 2a: Policy engine scan
	findings := h.policyEngine.ScanFileContent(req.Message)
	if len(findings) > 0 {
		span.SetAttributes(attribute.Int("policy.findings_count", len(findings)))
		slog.Warn("Blocked verified streaming: user attempting to send sensitive data",
			"findings_count", len(findings),
			"requestId", req.Id,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
		}
		c.JSON(http.StatusForbidden, gin.H{
			"error":    "Policy Violation: Message contains sensitive data.",
			"findings": findings,
		})
		return
	}

	// Step 2b: Apply message filter (FOSS-006)
	// Enterprise can implement custom filtering (PII redaction, etc.)
	if req.Message != "" {
		filterResult, filterErr := h.opts.MessageFilter.FilterInput(ctx, req.Message)
		if filterErr != nil {
			slog.Error("Message filter failed", "error", filterErr, "requestId", req.Id)
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
				ResourceType: "chat",
				ResourceID:   "rag/verified/stream",
				Outcome:      "blocked",
				Metadata: map[string]any{
					"request_id": req.Id,
					"reason":     filterResult.BlockReason,
				},
			})
			if m := observability.DefaultMetrics; m != nil {
				m.RecordError(endpoint, observability.ErrorCodePolicyViolation)
			}
			c.JSON(http.StatusForbidden, gin.H{
				"error":  "Message blocked by content filter",
				"reason": filterResult.BlockReason,
			})
			return
		}

		// Use filtered content (may have PII redacted)
		req.Message = filterResult.Filtered
	}

	// Step 3: Get verbosity level from header (default: 2 for development)
	verbosity := h.getVerbosityLevel(c)
	span.SetAttributes(attribute.Int("verbosity", verbosity))

	// Step 4: Set SSE headers and create writer
	SetSSEHeaders(c.Writer)
	sseWriter, err := NewSSEWriter(c.Writer)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "SSE setup failed")
		slog.Error("Failed to create SSE writer",
			"error", err,
			"requestId", req.Id,
		)
		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeInternal)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	// Step 5: Determine session ID (from request or generate from Id)
	sessionID := req.SessionId
	if sessionID == "" {
		sessionID = req.Id
	}

	// Step 5.5: Get turn count ONCE to avoid race condition
	// This value is used for both history search and persistence
	currentTurnCount, countErr := h.getTurnCount(ctx, sessionID)
	if countErr != nil {
		slog.Warn("failed to get turn count, proceeding with turn 1",
			"requestId", req.Id,
			"sessionId", sessionID,
			"error", countErr,
		)
		currentTurnCount = 0
	}
	// Pre-compute next turn number to use consistently throughout
	nextTurnNumber := currentTurnCount + 1

	// Step 5.6: Search for relevant conversation history (P7)
	var relevantHistory []conversation.RelevantTurn
	if currentTurnCount > 0 {
		// Create conversation searcher
		embedder := conversation.NewDatatypesEmbedder()
		searcher := conversation.NewWeaviateConversationSearcher(h.weaviateClient, embedder, conversation.DefaultSearchConfig())

		// Search for relevant history
		history, searchErr := searcher.GetHybridContext(ctx, sessionID, req.Message, currentTurnCount)
		if searchErr != nil {
			slog.Warn("failed to search conversation history, proceeding without history",
				"requestId", req.Id,
				"sessionId", sessionID,
				"error", searchErr,
			)
		} else {
			relevantHistory = history
			span.SetAttributes(attribute.Int("conversation.history_count", len(relevantHistory)))
			slog.Info("retrieved relevant conversation history",
				"requestId", req.Id,
				"sessionId", sessionID,
				"historyCount", len(relevantHistory),
			)
		}
	}

	// Step 5.7: Query Expansion (P8 - conversational context enhancement)
	// Expand ambiguous queries like "tell me more" using conversation history
	var expandedQuery *conversation.ExpandedQuery
	if h.queryExpander != nil && len(relevantHistory) > 0 {
		if h.queryExpander.NeedsExpansion(req.Message) {
			expanded, expandErr := h.queryExpander.Expand(ctx, req.Message, relevantHistory)
			if expandErr != nil {
				slog.Warn("query expansion failed, using original query",
					"requestId", req.Id,
					"query", req.Message,
					"error", expandErr,
				)
				// Fall back to original query
				expandedQuery = &conversation.ExpandedQuery{
					Original: req.Message,
					Queries:  []string{req.Message},
					Expanded: false,
				}
			} else {
				expandedQuery = expanded
				span.SetAttributes(
					attribute.Bool("query.expanded", expanded.Expanded),
					attribute.Int("query.variation_count", len(expanded.Queries)),
				)
				slog.Info("query expanded",
					"requestId", req.Id,
					"original", req.Message,
					"expanded", expanded.Queries,
				)
			}
		} else if h.queryExpander.DetectsTopicSwitch(req.Message) {
			// Topic switch detected - clear history context
			slog.Info("topic switch detected, clearing history context",
				"requestId", req.Id,
				"query", req.Message,
			)
			relevantHistory = nil
			span.SetAttributes(attribute.Bool("query.topic_switch", true))
		}
	}

	// Step 5.8: Build contextual query for embedding (P8)
	// This would be used if we re-embed the query with context, but currently
	// we pass the context to Python for use in prompts/reranking
	var contextualQuery string
	if h.contextualEmbedder != nil && len(relevantHistory) > 0 {
		queryForContext := req.Message
		if expandedQuery != nil && len(expandedQuery.Queries) > 0 {
			queryForContext = expandedQuery.Queries[0] // Use primary expanded query
		}
		contextualQuery = h.contextualEmbedder.BuildContextualQuery(ctx, queryForContext, relevantHistory)
		span.SetAttributes(attribute.Int("query.contextual_length", len(contextualQuery)))
	}

	// Step 6: Call Python's verified streaming endpoint
	result, err := h.streamFromVerifiedPipeline(ctx, &req, sseWriter, verbosity, sessionID, relevantHistory, expandedQuery, contextualQuery, span)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Verified streaming failed")
		slog.Error("Verified streaming failed",
			"error", err,
			"requestId", req.Id,
		)

		// Log failed streaming attempt (FOSS-005)
		_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
			EventType:    "chat.stream",
			Timestamp:    time.Now().UTC(),
			UserID:       userID,
			Action:       "send",
			ResourceType: "chat",
			ResourceID:   "rag/verified/stream",
			Outcome:      "failed",
			Metadata: map[string]any{
				"request_id": req.Id,
				"session_id": sessionID,
				"error":      err.Error(),
			},
		})

		if m := observability.DefaultMetrics; m != nil {
			m.RecordError(endpoint, observability.ErrorCodeRAGError)
		}
		// Error already sent via SSE in streamFromVerifiedPipeline
		return
	}

	// Step 7: Persist conversation turn with hash chain
	// This enables session verify to show turn counts and verify integrity
	// NOTE: Use a detached context for persistence because the HTTP request
	// context may be canceled after the stream completes (client disconnects).
	if result != nil && result.Answer != "" {
		// Step 7.0: Capture response for enterprise audit (FOSS-008)
		_ = h.opts.RequestAuditor.CaptureResponse(ctx, auditID, &extensions.AuditableResponse{
			StatusCode: http.StatusOK,
			Headers:    extensions.HTTPHeaders{"Content-Type": "text/event-stream"},
			Body:       []byte(result.Answer),
			Timestamp:  time.Now().UTC(),
		})

		// Create a detached context with timeout for persistence
		// This ensures persistence completes even if client disconnects
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer persistCancel()

		// Compute hash of the answer for integrity verification
		answerHash := fmt.Sprintf("%x", sha256.Sum256([]byte(result.Answer)))

		// Audit answer for PII before persistence
		shouldBlock, piiFindings := h.auditAnswerForPII(sessionID, result.Answer)
		span.SetAttributes(attribute.Int("pii.findings_count", len(piiFindings)))

		if shouldBlock {
			slog.Warn("turn persistence blocked due to PII in answer",
				"requestId", req.Id,
				"sessionId", sessionID,
				"findingsCount", len(piiFindings),
			)
		} else {
			// Use pre-computed nextTurnNumber from Step 5.5 to avoid race condition
			// Note: Concurrent requests may still get same turn number - for strict ordering,
			// database-level locking or atomic increment would be needed.

			// Persist the conversation turn
			persistErr := h.persistConversationTurn(
				persistCtx,
				sessionID,
				nextTurnNumber,
				req.Message, // The user's question
				result.Answer,
				answerHash,
			)
			if persistErr != nil {
				slog.Error("failed to persist conversation turn",
					"requestId", req.Id,
					"sessionId", sessionID,
					"turnNumber", nextTurnNumber,
					"error", persistErr,
				)
			} else {
				span.SetAttributes(
					attribute.Int("turn.number", nextTurnNumber),
					attribute.String("turn.hash", answerHash[:16]+"..."), // Truncate for logging
					attribute.Bool("turn.is_verified", result.IsVerified),
				)
				slog.Info("verified conversation turn persisted successfully",
					"requestId", req.Id,
					"sessionId", sessionID,
					"turnNumber", nextTurnNumber,
					"isVerified", result.IsVerified,
				)

				// Save to semantic memory for future context retrieval (P7)
				// Pass session context to store dataspace/pipeline for resume functionality
				// If first turn, also generate session summary
				sessionCtx := datatypes.SessionContext{
					DataSpace: req.DataSpace,
					Pipeline:  req.Pipeline,
					TTL:       req.SessionTTL,
				}
				go SaveMemoryChunkWithSummary(h.llmClient, h.weaviateClient, sessionID, req.Message, result.Answer, nextTurnNumber, sessionCtx)
			}
		}
	}

	// Step 8: Log successful streaming (FOSS-005)
	processingTime := time.Since(startTime).Milliseconds()
	_ = h.opts.AuditLogger.Log(ctx, extensions.AuditEvent{
		EventType:    "chat.stream",
		Timestamp:    time.Now().UTC(),
		UserID:       userID,
		Action:       "send",
		ResourceType: "chat",
		ResourceID:   "rag/verified/stream",
		Outcome:      "success",
		Metadata: map[string]any{
			"request_id":    req.Id,
			"session_id":    sessionID,
			"processing_ms": fmt.Sprintf("%d", processingTime),
			"pipeline":      "verified",
			"is_verified":   fmt.Sprintf("%t", result != nil && result.IsVerified),
		},
	})

	success = true
	span.SetStatus(codes.Ok, "verified stream completed successfully")
}

// getVerbosityLevel extracts verbosity from the X-Verbosity header.
//
// # Description
//
// Parses the X-Verbosity header to determine output detail level.
// Returns 2 (detailed) if header is missing or invalid.
//
// # Inputs
//
//   - c: Gin context containing the request headers.
//
// # Outputs
//
//   - int: Verbosity level (0=silent, 1=summary, 2=detailed).
//
// # Examples
//
//	verbosity := h.getVerbosityLevel(c)
//	if verbosity >= 2 {
//	    // Include detailed information
//	}
//
// # Limitations
//
//   - Defaults to 2 if parsing fails.
//
// # Assumptions
//
//   - Valid values are 0, 1, or 2.
func (h *streamingChatHandler) getVerbosityLevel(c *gin.Context) int {
	header := c.GetHeader("X-Verbosity")
	if header == "" {
		return 2 // Default to detailed for development
	}

	switch header {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	default:
		slog.Debug("Invalid X-Verbosity header, using default",
			"header", header,
		)
		return 2
	}
}

// streamFromVerifiedPipeline calls Python's /rag/verified/stream and forwards events.
//
// # Description
//
// Makes an HTTP request to the Python RAG engine's verified streaming endpoint,
// reads SSE events from the response, applies verbosity filtering, and forwards
// appropriate events to the client. Returns the captured answer for persistence.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - req: The verified RAG request.
//   - writer: SSE writer for output.
//   - verbosity: Output detail level (0=silent, 1=summary, 2=detailed).
//   - sessionID: The session ID for the conversation (for done event).
//   - relevantHistory: Relevant conversation turns from P7 semantic memory.
//   - expandedQuery: Expanded query from P8 (nil if not expanded).
//   - contextualQuery: Query with history context for embedding (empty if not used).
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - *verifiedStreamResult: Captured answer and verification status.
//   - error: Non-nil if streaming failed.
//
// # Examples
//
//	result, err := h.streamFromVerifiedPipeline(ctx, req, writer, 2, sessionID, history, expanded, contextual, span)
//
// # Limitations
//
//   - Requires RAG_ENGINE_URL environment variable.
//   - Python endpoint must be available.
//
// # Assumptions
//
//   - Python endpoint returns SSE with progress, answer, error, and done events.
func (h *streamingChatHandler) streamFromVerifiedPipeline(
	ctx context.Context,
	req *datatypes.ChatRAGRequest,
	writer SSEWriter,
	verbosity int,
	sessionID string,
	relevantHistory []conversation.RelevantTurn,
	expandedQuery *conversation.ExpandedQuery,
	contextualQuery string,
	span trace.Span,
) (*verifiedStreamResult, error) {
	// Get RAG engine URL
	ragEngineURL := os.Getenv("RAG_ENGINE_URL")
	if ragEngineURL == "" {
		ragEngineURL = "http://localhost:8081"
	}
	streamURL := ragEngineURL + "/rag/verified/stream"

	// Build request body
	requestBody := h.buildVerifiedStreamRequest(req)

	// Add relevant conversation history (P7)
	if len(relevantHistory) > 0 {
		historyList := make([]map[string]interface{}, len(relevantHistory))
		for i, turn := range relevantHistory {
			historyList[i] = map[string]interface{}{
				"question":         turn.Question,
				"answer":           turn.Answer,
				"turn_number":      turn.TurnNumber,
				"similarity_score": turn.SimilarityScore,
			}
		}
		requestBody["relevant_history"] = historyList
	}

	// Add expanded query (P8 - query expansion)
	if expandedQuery != nil && expandedQuery.Expanded {
		requestBody["expanded_query"] = map[string]interface{}{
			"original": expandedQuery.Original,
			"queries":  expandedQuery.Queries,
			"expanded": expandedQuery.Expanded,
		}
		// Use the first expanded query as the primary query for Python
		// Python will use this for document search and reranking
		if len(expandedQuery.Queries) > 0 {
			requestBody["query"] = expandedQuery.Queries[0]
			requestBody["original_query"] = expandedQuery.Original
		}
	}

	// Add contextual query for embedding (P8)
	if contextualQuery != "" {
		requestBody["contextual_query"] = contextualQuery
	}

	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		_ = writer.WriteError("Failed to prepare request")
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(reqJSON))
	if err != nil {
		_ = writer.WriteError("Failed to create request")
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Make request with longer timeout for LLM calls
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		_ = writer.WriteError("Failed to connect to RAG engine")
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_ = writer.WriteError("RAG engine returned an error")
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Process SSE stream from Python
	return h.processVerifiedSSEStream(ctx, resp.Body, writer, verbosity, sessionID, span)
}

// buildVerifiedStreamRequest constructs the request body for Python's endpoint.
//
// # Description
//
// Builds the JSON request body for the /rag/verified/stream endpoint,
// including query, session_id, strict_mode, and temperature_overrides.
//
// # Inputs
//
//   - req: The ChatRAGRequest from the client.
//
// # Outputs
//
//   - map[string]interface{}: Request body for Python endpoint.
//
// # Limitations
//
//   - Only includes non-nil temperature overrides.
//
// # Assumptions
//
//   - Python endpoint accepts the same structure as VerifiedRAGRequest.
func (h *streamingChatHandler) buildVerifiedStreamRequest(req *datatypes.ChatRAGRequest) map[string]interface{} {
	body := map[string]interface{}{
		"query":       req.Message,
		"session_id":  req.SessionId,
		"strict_mode": req.StrictMode,
	}

	// Add temperature overrides if provided
	if req.TemperatureOverrides != nil {
		tempOverrides := req.TemperatureOverrides.ToMap()
		if tempOverrides != nil {
			body["temperature_overrides"] = tempOverrides
		}
	}

	// Add data_space filter if provided
	if req.DataSpace != "" {
		body["data_space"] = req.DataSpace
	}

	// Add version_tag filter if provided (for querying specific document versions)
	if req.VersionTag != "" {
		body["version_tag"] = req.VersionTag
	}

	return body
}

// processVerifiedSSEStream reads and forwards SSE events from Python.
//
// # Description
//
// Reads Server-Sent Events from the Python response, parses them,
// applies verbosity filtering, and forwards to the client writer.
// Captures the answer text for turn persistence.
//
// # SSE Event Format
//
// Events from Python:
//   - event: progress, data: {"event_type":"...", "message":"...", ...}
//   - event: answer, data: {"answer":"...", "sources":[...], "is_verified":...}
//   - event: error, data: {"error":"..."}
//   - event: done, data: {}
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - body: Response body from Python (SSE stream).
//   - writer: SSE writer for output.
//   - verbosity: Output detail level.
//   - sessionID: The session ID for the conversation (for done event).
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - *verifiedStreamResult: Captured answer and verification status.
//   - error: Non-nil if processing failed.
//
// # Limitations
//
//   - Expects well-formed SSE (event: and data: lines).
//
// # Assumptions
//
//   - Python sends events in standard SSE format.
func (h *streamingChatHandler) processVerifiedSSEStream(
	ctx context.Context,
	body io.Reader,
	writer SSEWriter,
	verbosity int,
	sessionID string,
	span trace.Span,
) (*verifiedStreamResult, error) {
	scanner := bufio.NewScanner(body)
	var currentEvent string
	var currentData string

	// Result struct to capture answer for persistence
	result := &verifiedStreamResult{}

	for scanner.Scan() {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		// Parse SSE format
		if line == "" {
			// Empty line = event complete, dispatch it
			if currentEvent != "" && currentData != "" {
				err := h.dispatchVerifiedEvent(currentEvent, currentData, writer, verbosity, sessionID, result, span)
				if err != nil {
					return nil, err
				}
			}
			currentEvent = ""
			currentData = ""
			continue
		}

		if len(line) > 6 && line[:6] == "event:" {
			currentEvent = line[6:]
			// Trim leading space if present
			if len(currentEvent) > 0 && currentEvent[0] == ' ' {
				currentEvent = currentEvent[1:]
			}
		} else if len(line) > 5 && line[:5] == "data:" {
			currentData = line[5:]
			// Trim leading space if present
			if len(currentData) > 0 && currentData[0] == ' ' {
				currentData = currentData[1:]
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan SSE stream: %w", err)
	}

	return result, nil
}

// dispatchVerifiedEvent forwards a parsed SSE event with verbosity filtering.
//
// # Description
//
// Dispatches a single SSE event based on its type and the configured
// verbosity level. Progress events are filtered; answer and done events
// are always forwarded. When an answer event is processed, the answer
// text and verification status are captured in the result struct.
//
// # Verbosity Filtering
//
//   - Level 0: No progress events, only answer and done
//   - Level 1: Progress events with message only
//   - Level 2: Progress events with full details + OTel trace link
//
// # Inputs
//
//   - eventType: SSE event type (progress, answer, error, done).
//   - data: JSON-encoded event data.
//   - writer: SSE writer for output.
//   - verbosity: Output detail level.
//   - sessionID: The session ID for the conversation (for done event).
//   - result: Pointer to result struct for capturing answer (may be nil).
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
//
// # Limitations
//
//   - OTel trace link format is hardcoded to Jaeger.
//
// # Assumptions
//
//   - Event data is valid JSON.
func (h *streamingChatHandler) dispatchVerifiedEvent(
	eventType string,
	data string,
	writer SSEWriter,
	verbosity int,
	sessionID string,
	result *verifiedStreamResult,
	span trace.Span,
) error {
	switch eventType {
	case "progress":
		return h.handleProgressEvent(data, writer, verbosity, span)
	case "answer":
		answerText, isVerified, err := h.handleAnswerEvent(data, writer, span)
		if err != nil {
			return err
		}
		// Capture answer for persistence
		if result != nil {
			result.Answer = answerText
			result.IsVerified = isVerified
		}
		return nil
	case "error":
		return h.handleErrorEvent(data, writer, span)
	case "done":
		return h.handleDoneEvent(writer, sessionID)
	default:
		slog.Debug("Unknown verified SSE event type", "type", eventType)
		return nil
	}
}

// handleProgressEvent processes a progress event with verbosity filtering.
//
// # Description
//
// Parses the progress event JSON and applies verbosity filtering:
//   - Level 0: Skip all progress events
//   - Level 1: Forward message only
//   - Level 2: Forward full event with OTel trace link
//
// # Inputs
//
//   - data: JSON-encoded ProgressEvent.
//   - writer: SSE writer for output.
//   - verbosity: Output detail level.
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
func (h *streamingChatHandler) handleProgressEvent(
	data string,
	writer SSEWriter,
	verbosity int,
	span trace.Span,
) error {
	// Skip progress events entirely at verbosity 0
	if verbosity == 0 {
		return nil
	}

	// Parse the progress event
	var event datatypes.ProgressEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		slog.Warn("Failed to parse progress event", "error", err, "data", data[:min(100, len(data))])
		return nil // Don't fail on parse errors
	}

	// Record event in span
	span.SetAttributes(
		attribute.String("progress.event_type", string(event.EventType)),
		attribute.Int("progress.attempt", event.Attempt),
	)

	if verbosity == 1 {
		// Summary mode: just write the message as a status event
		return writer.WriteStatus(event.Message)
	}

	// Verbosity 2: Full details
	// Add OTel trace link if we have a trace ID
	if event.TraceID != "" {
		jaegerURL := h.getJaegerURL()
		if jaegerURL != "" {
			traceLink := fmt.Sprintf("  View trace: %s/trace/%s", jaegerURL, event.TraceID)
			slog.Info("Verified pipeline trace available", "trace_id", event.TraceID, "trace_link", traceLink)
		}
	}

	// Forward the full event as a progress status
	// Include details based on event type
	message := event.Message
	if event.AuditDetails != nil && !event.AuditDetails.IsVerified {
		if len(event.AuditDetails.Hallucinations) > 0 {
			message += fmt.Sprintf("\n  Hallucinations: %v", event.AuditDetails.Hallucinations[:min(3, len(event.AuditDetails.Hallucinations))])
		}
	}
	if event.RetrievalDetails != nil {
		message += fmt.Sprintf("\n  Documents: %d, Sources: %v",
			event.RetrievalDetails.DocumentCount,
			event.RetrievalDetails.Sources[:min(3, len(event.RetrievalDetails.Sources))])
	}

	return writer.WriteStatus(message)
}

// handleAnswerEvent processes the final answer event.
//
// # Description
//
// Forwards the answer event to the client. Always forwarded regardless
// of verbosity level. Also returns the answer text and verification status
// for turn persistence.
//
// # Inputs
//
//   - data: JSON-encoded answer with answer, sources, and is_verified.
//   - writer: SSE writer for output.
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - string: The answer text (for persistence).
//   - bool: Whether the answer was verified.
//   - error: Non-nil if writing failed.
func (h *streamingChatHandler) handleAnswerEvent(
	data string,
	writer SSEWriter,
	span trace.Span,
) (string, bool, error) {
	var answer datatypes.VerifiedStreamAnswer
	if err := json.Unmarshal([]byte(data), &answer); err != nil {
		slog.Warn("Failed to parse answer event", "error", err)
		return "", false, writer.WriteError("Failed to parse answer")
	}

	span.SetAttributes(
		attribute.Bool("answer.is_verified", answer.IsVerified),
		attribute.Int("answer.length", len(answer.Answer)),
	)

	// Stream the answer as tokens (to maintain consistency with other streaming)
	// For now, just emit the full answer as one token
	if err := writer.WriteToken(answer.Answer); err != nil {
		return "", false, err
	}

	// Emit sources if available
	if len(answer.Sources) > 0 {
		// Convert to SourceInfo format
		sources := make([]datatypes.SourceInfo, 0, len(answer.Sources))
		for _, s := range answer.Sources {
			source := datatypes.SourceInfo{}
			if src, ok := s["source"].(string); ok {
				source.Source = src
			}
			if score, ok := s["score"].(float64); ok {
				source.Score = score
			}
			sources = append(sources, source)
		}
		if err := writer.WriteSources(sources); err != nil {
			slog.Warn("Failed to write sources", "error", err)
		}
	}

	return answer.Answer, answer.IsVerified, nil
}

// handleErrorEvent processes an error event from Python.
//
// # Description
//
// Forwards error events to the client.
//
// # Inputs
//
//   - data: JSON-encoded error with error message.
//   - writer: SSE writer for output.
//   - span: OTel span for tracing.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
func (h *streamingChatHandler) handleErrorEvent(
	data string,
	writer SSEWriter,
	span trace.Span,
) error {
	var errEvent datatypes.VerifiedStreamError
	if err := json.Unmarshal([]byte(data), &errEvent); err != nil {
		return writer.WriteError("Unknown error occurred")
	}

	span.RecordError(fmt.Errorf("%s", errEvent.Error))
	return writer.WriteError(sanitizeErrorForClient(errEvent.Error))
}

// verifiedStreamResult holds the captured data from a verified stream.
//
// # Description
//
// This struct captures the answer and metadata from a verified pipeline
// stream so it can be persisted after the stream completes.
type verifiedStreamResult struct {
	Answer     string // The final verified answer text
	IsVerified bool   // Whether the answer passed verification
}

// handleDoneEvent processes the done event.
//
// # Description
//
// Signals stream completion with the session ID. The done event is always
// forwarded and includes the session ID for client-side session management.
//
// # Inputs
//
//   - writer: SSE writer for output.
//   - sessionID: The session ID for the conversation.
//
// # Outputs
//
//   - error: Non-nil if writing failed.
func (h *streamingChatHandler) handleDoneEvent(writer SSEWriter, sessionID string) error {
	return writer.WriteDone(sessionID)
}

// getJaegerURL returns the Jaeger UI URL for trace viewing.
//
// # Description
//
// Gets the Jaeger URL from environment variable or returns default.
//
// # Outputs
//
//   - string: Jaeger UI URL (empty if not configured).
func (h *streamingChatHandler) getJaegerURL() string {
	url := os.Getenv("JAEGER_UI_URL")
	if url == "" {
		return "http://localhost:16686"
	}
	return url
}

// =============================================================================
// Helper Methods
// =============================================================================

// runHeartbeat sends periodic keepalive pings to prevent connection timeouts.
//
// # Description
//
// Runs in a separate goroutine, sending SSE comments every heartbeatInterval
// to keep the connection alive during long operations (RAG retrieval, LLM thinking).
// Stops when done channel is closed or context is cancelled.
//
// # Inputs
//
//   - ctx: Context for cancellation detection.
//   - writer: SSE writer to send keepalives.
//   - endpoint: Endpoint name for metrics.
//   - done: Channel to signal when to stop (close to stop).
//
// # Outputs
//
// None. Runs until done is closed or context is cancelled.
//
// # Examples
//
//	done := make(chan struct{})
//	go h.runHeartbeat(ctx, writer, endpoint, done)
//	// ... do work ...
//	close(done)
//
// # Limitations
//
//   - Errors writing keepalives are logged but don't stop the heartbeat.
//
// # Assumptions
//
//   - Writer is thread-safe.
func (h *streamingChatHandler) runHeartbeat(
	ctx context.Context,
	writer SSEWriter,
	endpoint observability.Endpoint,
	done <-chan struct{},
) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writer.WriteKeepAlive(); err != nil {
				slog.Debug("Failed to write keepalive", "error", err)
				return
			}
			if m := observability.DefaultMetrics; m != nil {
				m.RecordKeepAlive(endpoint)
			}
		}
	}
}

// streamFromLLMWithMetrics streams tokens with metrics tracking.
//
// # Description
//
// Enhanced version of streamFromLLM that tracks token count and time to first token.
// Also includes explicit context cancellation checks for cost control.
// Optionally accumulates tokens into a secure buffer for persistence.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//   - messages: Conversation messages.
//   - params: Generation parameters.
//   - writer: SSE writer for output.
//   - endpoint: Endpoint name for metrics.
//   - tokenCount: Pointer to atomic counter for tokens.
//   - firstTokenTime: Pointer to time of first token (set once).
//   - accumulator: Optional token accumulator for secure storage (may be nil).
//
// # Outputs
//
//   - error: Non-nil if streaming failed.
//
// # Examples
//
//	// Without accumulator
//	err := h.streamFromLLMWithMetrics(ctx, reqID, msgs, params, writer, endpoint, &count, &time, nil)
//
//	// With accumulator for persistence
//	acc, _ := NewSecureTokenAccumulator()
//	defer acc.Destroy()
//	err := h.streamFromLLMWithMetrics(ctx, reqID, msgs, params, writer, endpoint, &count, &time, acc)
//	answer, hash, _ := acc.Finalize()
//
// # Security
//
// LLM output is streamed to user and optionally accumulated in mlocked memory.
// Hash is computed incrementally as tokens arrive for integrity verification.
// This allows async review for compliance while not blocking the streaming experience.
//
// # Limitations
//
//   - Requires LLM client to implement ChatStream.
//   - Accumulator overflow causes token write failure but streaming continues.
//
// # Assumptions
//
//   - Writer is ready for events.
//   - Accumulator, if provided, is ready for writes.
func (h *streamingChatHandler) streamFromLLMWithMetrics(
	ctx context.Context,
	requestID string,
	messages []datatypes.Message,
	params llm.GenerationParams,
	writer SSEWriter,
	_ observability.Endpoint,
	tokenCount *int32,
	firstTokenTime *time.Time,
	accumulator TokenAccumulator,
) error {
	callback := func(event llm.StreamEvent) error {
		// Explicit context cancellation check (cost control)
		// Stop processing immediately if client disconnected
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch event.Type {
		case llm.StreamEventToken:
			// Track first token time
			if firstTokenTime.IsZero() {
				*firstTokenTime = time.Now()
			}
			atomic.AddInt32(tokenCount, 1)

			// Accumulate token if accumulator provided (for turn persistence)
			if accumulator != nil {
				if err := accumulator.Write(event.Content); err != nil {
					// Log but don't fail streaming - still want user to see response
					slog.Warn("failed to accumulate token for persistence",
						"requestId", requestID,
						"error", err,
						"accumulatorId", accumulator.ID(),
					)
				}
			}

			return writer.WriteToken(event.Content)

		case llm.StreamEventThinking:
			return writer.WriteThinking(event.Content)

		case llm.StreamEventError:
			// SEC-005: Sanitize error before sending to client
			sanitizedErr := sanitizeErrorForClient(event.Error)
			return writer.WriteError(sanitizedErr)
		}
		return nil
	}

	err := h.llmClient.ChatStream(ctx, messages, params, callback)
	if err != nil {
		// SEC-005: Log full error internally, send sanitized to client
		slog.Error("LLM ChatStream failed",
			"requestId", requestID,
			"error", err,
			"tokenCount", atomic.LoadInt32(tokenCount),
		)
		_ = writer.WriteError(sanitizeErrorForClient(err.Error()))
		return err
	}

	return nil
}

// streamFromLLM streams tokens from the LLM to the SSE writer (legacy).
//
// # Description
//
// Calls the LLM's ChatStream method and writes tokens to the SSE writer
// as they arrive. Handles thinking tokens separately. All output is
// logged via hash chain for async audit review.
//
// NOTE: Consider using streamFromLLMWithMetrics for new code.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - requestID: Request identifier for logging.
//   - messages: Conversation messages.
//   - params: Generation parameters.
//   - writer: SSE writer for output.
//
// # Outputs
//
//   - error: Non-nil if streaming failed.
//
// # Security
//
// LLM output is streamed to user and logged via hash chain. This allows
// async review for compliance while not blocking the streaming experience.
//
// # Limitations
//
//   - Requires LLM client to implement ChatStream.
//
// # Assumptions
//
//   - Writer is ready for events.
func (h *streamingChatHandler) streamFromLLM(
	ctx context.Context,
	requestID string,
	messages []datatypes.Message,
	params llm.GenerationParams,
	writer SSEWriter,
) error {
	callback := func(event llm.StreamEvent) error {
		switch event.Type {
		case llm.StreamEventToken:
			return writer.WriteToken(event.Content)
		case llm.StreamEventThinking:
			return writer.WriteThinking(event.Content)
		case llm.StreamEventError:
			// SEC-005: Sanitize error before sending to client
			sanitizedErr := sanitizeErrorForClient(event.Error)
			return writer.WriteError(sanitizedErr)
		}
		return nil
	}

	err := h.llmClient.ChatStream(ctx, messages, params, callback)
	if err != nil {
		// SEC-005: Log full error internally, send sanitized to client
		slog.Error("LLM ChatStream failed",
			"requestId", requestID,
			"error", err,
		)
		_ = writer.WriteError(sanitizeErrorForClient(err.Error()))
		return err
	}

	return nil
}

// auditRAGContextForPII scans retrieved RAG context and logs PII findings.
//
// # Description
//
// Scans the RAG context string for PII using the policy engine.
// Findings are LOGGED for audit trail but do NOT block the request.
// This provides visibility into sensitive data flowing from DB to LLM.
//
// Future enhancement: Integrate granite-guardian for user acknowledgment
// flow before sending PII-containing context to LLM.
//
// # Inputs
//
//   - requestID: Request identifier for log correlation.
//   - ragContext: The retrieved context text to scan.
//   - sources: Source metadata for logging.
//
// # Outputs
//
// None. Findings are logged only.
//
// # Security
//
// This is an AUDIT function, not a blocking function. The security model
// allows users to see data from the database - we just want visibility
// into what sensitive data is being processed.
//
// # Limitations
//
//   - Does not block requests, only logs.
//   - Future: Add granite-guardian integration.
//
// # Assumptions
//
//   - Policy engine is available.
func (h *streamingChatHandler) auditRAGContextForPII(requestID string, ragContext string, sources []datatypes.SourceInfo) {
	findings := h.policyEngine.ScanFileContent(ragContext)
	if len(findings) > 0 {
		// AUDIT LOG: PII detected in RAG context being sent to LLM
		// This is logged but NOT blocked per security model
		sourceNames := make([]string, 0, len(sources))
		for _, s := range sources {
			sourceNames = append(sourceNames, s.Source)
		}
		slog.Warn("AUDIT: RAG context contains potential PII being sent to LLM",
			"requestId", requestID,
			"sources", sourceNames,
			"findings_count", len(findings),
			"findings_types", extractFindingTypes(findings),
		)
	}
}

// auditAnswerForPII scans the LLM answer for PII before persistence.
//
// # Description
//
// Scans the accumulated LLM answer for PII using the policy engine.
// The behavior depends on the PII scan mode configuration:
//   - "audit" (default): Log findings but allow persistence
//   - "block": Log findings and return true to block persistence
//
// # Inputs
//
//   - sessionID: Session ID for log correlation.
//   - answer: The complete LLM response to scan.
//
// # Outputs
//
//   - bool: True if persistence should be blocked (only in "block" mode).
//   - []policy_engine.ScanFinding: PII findings (empty if none).
//
// # Examples
//
//	shouldBlock, findings := h.auditAnswerForPII("sess-123", answer)
//	if shouldBlock {
//	    slog.Warn("Blocking persistence due to PII in answer")
//	    return
//	}
//
// # Limitations
//
//   - Scanning is best-effort; may miss some PII patterns.
//
// # Assumptions
//
//   - Policy engine is available.
//   - ALEUTIAN_PII_SCAN_MODE is "audit" or "block" (defaults to "audit").
func (h *streamingChatHandler) auditAnswerForPII(
	sessionID string,
	answer string,
) (shouldBlock bool, findings []policy_engine.ScanFinding) {
	findings = h.policyEngine.ScanFileContent(answer)

	if len(findings) == 0 {
		return false, nil
	}

	piiScanMode := h.getPIIScanMode()

	slog.Warn("AUDIT: LLM answer contains potential PII",
		"session_id", sessionID,
		"findings_count", len(findings),
		"findings_types", extractFindingTypes(findings),
		"pii_scan_mode", piiScanMode,
	)

	if piiScanMode == "block" {
		slog.Warn("BLOCKING persistence due to PII in answer",
			"session_id", sessionID,
		)
		return true, findings
	}

	return false, findings
}

// getPIIScanMode returns the configured PII scan mode.
//
// # Description
//
// Reads the ALEUTIAN_PII_SCAN_MODE environment variable to determine
// how to handle PII detected in LLM answers.
//
// # Outputs
//
//   - string: "audit" (default) or "block"
//
// # Examples
//
//	mode := h.getPIIScanMode()
//	if mode == "block" {
//	    // Don't persist answers with PII
//	}
//
// # Limitations
//
//   - Only supports "audit" and "block" modes.
//
// # Assumptions
//
//   - Environment variable may not be set (defaults to "audit").
func (h *streamingChatHandler) getPIIScanMode() string {
	mode := os.Getenv("ALEUTIAN_PII_SCAN_MODE")
	if mode == "block" {
		return "block"
	}
	return "audit"
}

// extractFindingTypes extracts the classification names from policy findings for logging.
//
// # Description
//
// Helper to extract finding classification names for structured logging without
// logging the actual sensitive content.
//
// # Inputs
//
//   - findings: Policy scan findings to extract types from.
//
// # Outputs
//
//   - []string: List of finding classification names.
func extractFindingTypes(findings []policy_engine.ScanFinding) []string {
	types := make([]string, 0, len(findings))
	for _, f := range findings {
		types = append(types, f.ClassificationName)
	}
	return types
}

// sanitizeErrorForClient removes internal details from error messages.
//
// # Description
//
// Per SEC-005, internal error details (stack traces, file paths, internal
// service names) must not be exposed to clients. This function returns
// a generic, safe error message.
//
// # Inputs
//
//   - errMsg: Raw error message (may contain internal details).
//
// # Outputs
//
//   - string: Sanitized error message safe for client display.
//
// # Security References
//
//   - SEC-005: Internal errors not exposed to client
func sanitizeErrorForClient(errMsg string) string {
	// Log the full error internally for debugging
	slog.Debug("Sanitizing error for client", "original_error", errMsg)

	// Return generic message - don't expose internals
	return "An error occurred while processing your request"
}

// retrieveRAGContext retrieves context from the RAG service.
//
// # Description
//
// Calls the RAG service to retrieve relevant documents for the query.
// Returns the context string and source information for display.
//
// NOTE: Currently the RAG service does full retrieval + LLM generation.
// For true streaming, we need a retrieval-only endpoint that returns
// the actual document content. This is a TODO for future enhancement.
//
// Current workaround: Use the RAG service's answer as context for
// streaming. This means RAG streaming currently makes two LLM calls
// (one in RAG service, one for streaming). Future optimization should
// add a retrieval-only mode to the Python RAG engine.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - req: RAG request with message and pipeline info.
//
// # Outputs
//
//   - string: Retrieved context to include in LLM prompt.
//   - []datatypes.SourceInfo: Source documents with scores.
//   - error: Non-nil if retrieval failed.
//
// # Limitations
//
//   - Depends on RAG service availability.
//   - Currently makes two LLM calls (RAG + streaming). TODO: Add retrieval-only mode.
//
// # Assumptions
//
//   - RAG service is properly configured.
func (h *streamingChatHandler) retrieveRAGContext(
	ctx context.Context,
	req *datatypes.ChatRAGRequest,
) (string, []datatypes.SourceInfo, error) {
	// TODO: Implement retrieval-only mode in Python RAG engine
	// Currently the RAG service does retrieval + LLM generation together.
	// For true streaming, we need just the retrieval results (document content).
	//
	// Workaround: Call the full RAG service and use its answer as context.
	// This is inefficient (two LLM calls) but allows streaming to work.
	resp, err := h.ragService.Process(ctx, req)
	if err != nil {
		return "", nil, err
	}

	// Use the RAG answer as context for the streaming LLM call
	// TODO: Replace with actual retrieved document content when
	// retrieval-only mode is implemented
	contextStr := resp.Answer

	return contextStr, resp.Sources, nil
}

// buildRAGMessages constructs messages with RAG context for LLM.
//
// # Description
//
// Builds the message array with system prompt containing retrieved context
// and the user's question.
//
// # Inputs
//
//   - ragContext: Retrieved document context.
//   - userMessage: User's original question.
//
// # Outputs
//
//   - []datatypes.Message: Messages ready for LLM.
//
// # Limitations
//
//   - System prompt is hardcoded.
//
// # Assumptions
//
//   - Context is already formatted.
func (h *streamingChatHandler) buildRAGMessages(ragContext, userMessage string) []datatypes.Message {
	systemPrompt := `You are a helpful assistant. Use the following context to answer the user's question.
If the context doesn't contain relevant information, say so and provide what help you can.

Context:
` + ragContext

	return []datatypes.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
}

// =============================================================================
// Session History Methods
// =============================================================================

// loadSessionHistory fetches conversation history from Weaviate for session resume.
//
// # Description
//
// When a session_id is provided, this method retrieves all previous
// conversation turns from Weaviate in chronological order. This enables
// the LLM to have context from the previous conversation when resuming.
//
// # Inputs
//
//   - ctx: Context for cancellation and tracing.
//   - sessionID: The session to load history for.
//
// # Outputs
//
//   - []ConversationTurn: Ordered history (oldest first), up to maxHistoryTurns.
//   - error: Non-nil if Weaviate query failed.
//
// # Examples
//
//	history, err := h.loadSessionHistory(ctx, "sess-abc123")
//	if err != nil {
//	    slog.Warn("failed to load history", "error", err)
//	}
//	// history contains previous Q&A pairs
//
// # Limitations
//
//   - Returns empty slice (not error) if session doesn't exist.
//   - History is limited to maxHistoryTurns most recent turns.
//   - Requires weaviateClient to be non-nil.
//
// # Assumptions
//
//   - Session ID is a valid string (validation done by caller).
//   - Weaviate Conversation class has question, answer, timestamp fields.
func (h *streamingChatHandler) loadSessionHistory(
	ctx context.Context,
	sessionID string,
) ([]ConversationTurn, error) {
	ctx, span := h.tracer.Start(ctx, "streamingChatHandler.loadSessionHistory")
	defer span.End()

	span.SetAttributes(attribute.String("session_id", sessionID))

	// Check if Weaviate client is available
	if h.weaviateClient == nil {
		slog.Debug("Weaviate client not available, skipping history load")
		return nil, nil
	}

	// Check for empty session ID
	if sessionID == "" {
		return nil, nil
	}

	slog.Debug("loading session history",
		"session_id", sessionID,
		"max_turns", maxHistoryTurns,
	)

	// Define fields to retrieve
	fields := []graphql.Field{
		{Name: "question"},
		{Name: "answer"},
		{Name: "timestamp"},
	}

	// Create filter for session_id
	whereFilter := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	// Sort by timestamp ascending (oldest first)
	sortBy := graphql.Sort{
		Path:  []string{"timestamp"},
		Order: graphql.Asc,
	}

	// Execute query with limit
	result, err := h.weaviateClient.GraphQL().Get().
		WithClassName("Conversation").
		WithWhere(whereFilter).
		WithSort(sortBy).
		WithLimit(maxHistoryTurns).
		WithFields(fields...).
		Do(ctx)

	if err != nil {
		span.RecordError(err)
		slog.Error("failed to query Weaviate for session history",
			"session_id", sessionID,
			"error", err,
		)
		return nil, fmt.Errorf("query session history: %w", err)
	}

	// Parse response using typed struct
	// Marshal to JSON and unmarshal to typed struct for compile-time safety
	jsonBytes, err := json.Marshal(result.Data)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("marshal weaviate response: %w", err)
	}

	var typedResponse WeaviateConversationResponse
	if err := json.Unmarshal(jsonBytes, &typedResponse); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("unmarshal weaviate response: %w", err)
	}

	history := h.filterValidTurns(typedResponse.Get.Conversation)

	span.SetAttributes(attribute.Int("turns_loaded", len(history)))
	slog.Info("session history loaded",
		"session_id", sessionID,
		"turns_loaded", len(history),
	)

	return history, nil
}

// filterValidTurns filters conversation turns to only those with both question and answer.
//
// # Description
//
// Filters the parsed conversation turns to ensure only complete turns
// (those with both a question and an answer) are included in the history.
//
// # Inputs
//
//   - turns: Parsed conversation turns from Weaviate.
//
// # Outputs
//
//   - []ConversationTurn: Filtered turns with non-empty question and answer.
//
// # Limitations
//
//   - Drops turns missing either question or answer.
//
// # Assumptions
//
//   - Input turns are already typed from JSON unmarshaling.
func (h *streamingChatHandler) filterValidTurns(turns []ConversationTurn) []ConversationTurn {
	validTurns := make([]ConversationTurn, 0, len(turns))

	for _, turn := range turns {
		if turn.Question != "" && turn.Answer != "" {
			validTurns = append(validTurns, turn)
		}
	}

	return validTurns
}

// buildRAGMessagesWithHistory constructs LLM messages including conversation history.
//
// # Description
//
// Builds the message array with:
//  1. System prompt with RAG context (varies based on strict mode)
//  2. Previous conversation turns (alternating user/assistant)
//  3. Current user message
//
// In strict mode, the system prompt instructs the LLM to ONLY answer from
// the provided context and refuse to use general knowledge. In unrestricted
// mode, the LLM can fall back to general knowledge when documents don't help.
//
// This enables the LLM to maintain conversation continuity when resuming
// a session.
//
// # Inputs
//
//   - ragContext: Retrieved document context from RAG pipeline.
//   - userMessage: Current user question.
//   - history: Previous conversation turns (may be empty).
//   - strictMode: If true, LLM only answers from documents (no general knowledge).
//
// # Outputs
//
//   - []datatypes.Message: Messages ready for LLM, including history.
//
// # Examples
//
//	// Strict mode - only answer from documents
//	messages := h.buildRAGMessagesWithHistory(
//	    "OAuth is an authorization framework...",
//	    "How does it compare to SAML?",
//	    []ConversationTurn{{Question: "What is OAuth?", Answer: "OAuth is..."}},
//	    true, // strict mode
//	)
//
// # Limitations
//
//   - History is included in linear order (no summarization).
//
// # Assumptions
//
//   - History is already ordered chronologically.
//   - History length is within acceptable limits for context window.
func (h *streamingChatHandler) buildRAGMessagesWithHistory(
	ragContext string,
	userMessage string,
	history []ConversationTurn,
	strictMode bool,
) []datatypes.Message {
	var systemPrompt string
	if strictMode {
		// Strict RAG mode: Only answer from documents, no general knowledge
		systemPrompt = `You are a document-grounded assistant. You MUST ONLY answer based on the context provided below.

IMPORTANT RULES:
1. ONLY use information from the provided context to answer questions.
2. If the context does not contain relevant information to answer the question, respond with:
   "I don't have any documents about that topic in my knowledge base. Please try a different question or add relevant documents."
3. Do NOT use your general knowledge or training data to answer questions.
4. Do NOT speculate or make up information.
5. If you're unsure, say you don't have that information in your documents.

Context:
` + ragContext
	} else {
		// Unrestricted mode: Can use general knowledge as fallback
		systemPrompt = `You are a helpful assistant. Use the following context to answer the user's question.
If the context doesn't contain relevant information, say so and provide what help you can.

Context:
` + ragContext
	}

	// Calculate capacity: system + (history * 2) + current user
	capacity := 1 + len(history)*2 + 1
	messages := make([]datatypes.Message, 0, capacity)

	// Add system message
	messages = append(messages, datatypes.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add conversation history
	for _, turn := range history {
		messages = append(messages,
			datatypes.Message{Role: "user", Content: turn.Question},
			datatypes.Message{Role: "assistant", Content: turn.Answer},
		)
	}

	// Add current user message
	messages = append(messages, datatypes.Message{
		Role:    "user",
		Content: userMessage,
	})

	return messages
}

// =============================================================================
// Turn Persistence Methods
// =============================================================================

// persistConversationTurn saves a Q&A turn to Weaviate with hash chain integrity.
//
// # Description
//
// Persists a completed conversation turn to the Weaviate Conversation class.
// The turn includes:
//   - Session ID and turn number for ordering
//   - Question (user message) and answer (LLM response)
//   - Timestamp of when the turn was created
//   - Turn hash for integrity verification
//
// The turn UUID is generated from a hash of the content (session_id + turn_number +
// question + answer) to ensure idempotency - retrying the same turn creates the
// same UUID, preventing duplicates.
//
// # Inputs
//
//   - ctx: Context for cancellation and tracing.
//   - sessionID: UUID of the session this turn belongs to.
//   - turnNumber: Sequential turn number (1-indexed).
//   - question: The user's input message.
//   - answer: The complete LLM response.
//   - turnHash: SHA-256 hash of the answer for integrity verification.
//
// # Outputs
//
//   - error: Non-nil if persistence failed.
//
// # Examples
//
//	err := h.persistConversationTurn(ctx, "sess-123", 1, "What is OAuth?", "OAuth is...", "abc123...")
//	if err != nil {
//	    slog.Error("Failed to persist turn", "error", err)
//	    // Note: Request should still succeed - user got their answer
//	}
//
// # Limitations
//
//   - Requires weaviateClient to be non-nil.
//   - Does not block on failure - user experience takes priority.
//
// # Assumptions
//
//   - Weaviate schema has been initialized with Conversation class.
//   - Session object exists or will be created separately.
func (h *streamingChatHandler) persistConversationTurn(
	ctx context.Context,
	sessionID string,
	turnNumber int,
	question string,
	answer string,
	turnHash string,
) error {
	_, span := tracer.Start(ctx, "streamingChatHandler.persistConversationTurn")
	defer span.End()

	span.SetAttributes(
		attribute.String("session_id", sessionID),
		attribute.Int("turn_number", turnNumber),
	)

	if h.weaviateClient == nil {
		return fmt.Errorf("weaviate client not available")
	}

	turnUUID := h.generateTurnUUID(sessionID, turnNumber, question, answer)
	timestamp := time.Now().UnixMilli()

	properties := h.buildTurnProperties(sessionID, turnNumber, question, answer, turnHash, timestamp)

	err := h.createConversationObject(ctx, turnUUID, properties)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("create conversation object: %w", err)
	}

	h.logTurnPersistence(sessionID, turnNumber, turnUUID, turnHash, timestamp)
	span.SetAttributes(attribute.String("turn_uuid", turnUUID))

	return nil
}

// generateTurnUUID creates a deterministic UUID for a turn based on content.
//
// # Description
//
// Generates a UUID from a SHA-256 hash of the turn content. This ensures:
//   - Same content always produces the same UUID (idempotency)
//   - Retrying a failed persist won't create duplicates
//   - UUID is deterministically reproducible
//
// # Inputs
//
//   - sessionID: Session UUID.
//   - turnNumber: Turn number within session.
//   - question: User's question.
//   - answer: LLM's answer.
//
// # Outputs
//
//   - string: UUID string derived from content hash.
//
// # Examples
//
//	uuid := h.generateTurnUUID("sess-123", 1, "Hello", "Hi there!")
//
// # Limitations
//
//   - UUID is content-dependent; changing any field changes the UUID.
//
// # Assumptions
//
//   - Inputs are non-empty strings (empty strings will still work).
func (h *streamingChatHandler) generateTurnUUID(
	sessionID string,
	turnNumber int,
	question string,
	answer string,
) string {
	content := fmt.Sprintf("%s|%d|%s|%s", sessionID, turnNumber, question, answer)
	hash := sha256.Sum256([]byte(content))
	turnUUID, _ := uuid.FromBytes(hash[:16])
	return turnUUID.String()
}

// buildTurnProperties constructs the properties map for the Conversation object.
//
// # Description
//
// Builds a map of all properties for the Weaviate Conversation object.
//
// # Inputs
//
//   - sessionID: Session UUID.
//   - turnNumber: Turn number (1-indexed).
//   - question: User's question.
//   - answer: LLM's answer.
//   - turnHash: SHA-256 hash of answer.
//   - timestamp: Unix milliseconds timestamp.
//
// # Outputs
//
//   - map[string]interface{}: Properties for Weaviate object.
//
// # Limitations
//
//   - Does not include inSession reference (TODO: add beacon).
//
// # Assumptions
//
//   - All inputs are valid.
func (h *streamingChatHandler) buildTurnProperties(
	sessionID string,
	turnNumber int,
	question string,
	answer string,
	turnHash string,
	timestamp int64,
) map[string]interface{} {
	return map[string]interface{}{
		"session_id":  sessionID,
		"turn_number": turnNumber,
		"question":    question,
		"answer":      answer,
		"turn_hash":   turnHash,
		"timestamp":   timestamp,
	}
}

// createConversationObject creates the Conversation object in Weaviate.
//
// # Description
//
// Performs the actual Weaviate API call to create the object.
//
// # Inputs
//
//   - ctx: Context for the request.
//   - turnUUID: UUID for the new object.
//   - properties: Object properties.
//
// # Outputs
//
//   - error: Non-nil if creation failed.
//
// # Limitations
//
//   - Weaviate must be available.
//
// # Assumptions
//
//   - Schema exists with Conversation class.
func (h *streamingChatHandler) createConversationObject(
	ctx context.Context,
	turnUUID string,
	properties map[string]interface{},
) error {
	_, err := h.weaviateClient.Data().Creator().
		WithClassName("Conversation").
		WithID(turnUUID).
		WithProperties(properties).
		Do(ctx)
	return err
}

// logTurnPersistence logs successful turn persistence.
//
// # Description
//
// Logs the turn persistence with relevant details for debugging and audit.
//
// # Inputs
//
//   - sessionID: Session UUID.
//   - turnNumber: Turn number.
//   - turnUUID: Generated turn UUID.
//   - turnHash: Turn hash (truncated for logging).
//   - timestamp: Creation timestamp.
//
// # Outputs
//
// None.
//
// # Limitations
//
//   - Hash is truncated to 16 characters for logging.
//
// # Assumptions
//
//   - Called only after successful persistence.
func (h *streamingChatHandler) logTurnPersistence(
	sessionID string,
	turnNumber int,
	turnUUID string,
	turnHash string,
	timestamp int64,
) {
	hashPreview := turnHash
	if len(hashPreview) > 16 {
		hashPreview = hashPreview[:16] + "..."
	}

	slog.Info("Persisted conversation turn",
		"session_id", sessionID,
		"turn_number", turnNumber,
		"turn_uuid", turnUUID,
		"turn_hash", hashPreview,
		"timestamp", timestamp,
	)
}

// getTurnCount returns the current number of turns in a session.
//
// # Description
//
// Queries Weaviate to count existing conversation turns for a session.
// Used to determine the next turn number when persisting.
//
// # Inputs
//
//   - ctx: Context for the request.
//   - sessionID: Session to count turns for.
//
// # Outputs
//
//   - int: Number of existing turns (0 if none or error).
//   - error: Non-nil if query failed.
//
// # Examples
//
//	count, err := h.getTurnCount(ctx, "sess-123")
//	nextTurn := count + 1
//
// # Limitations
//
//   - Returns 0 on error (caller should handle).
//
// # Assumptions
//
//   - Weaviate is available.
func (h *streamingChatHandler) getTurnCount(ctx context.Context, sessionID string) (int, error) {
	if h.weaviateClient == nil {
		return 0, fmt.Errorf("weaviate client not available")
	}

	whereFilter := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	result, err := h.weaviateClient.GraphQL().Aggregate().
		WithClassName("Conversation").
		WithWhere(whereFilter).
		WithFields(graphql.Field{
			Name: "meta",
			Fields: []graphql.Field{
				{Name: "count"},
			},
		}).
		Do(ctx)

	if err != nil {
		return 0, fmt.Errorf("aggregate query failed: %w", err)
	}

	return h.parseTurnCount(result.Data)
}

// parseTurnCount extracts the count from an aggregate query result.
//
// # Description
//
// Parses the Weaviate aggregate response to extract the turn count.
//
// # Inputs
//
//   - data: Raw response data from Weaviate.
//
// # Outputs
//
//   - int: Turn count (0 if not found or parse error).
//   - error: Non-nil if parsing failed.
//
// # Limitations
//
//   - Expects specific Weaviate aggregate response structure.
//
// # Assumptions
//
//   - Response follows standard aggregate format.
func (h *streamingChatHandler) parseTurnCount(data interface{}) (int, error) {
	// Marshal and unmarshal for type safety
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return 0, fmt.Errorf("marshal aggregate response: %w", err)
	}

	var response struct {
		Aggregate struct {
			Conversation []struct {
				Meta struct {
					Count float64 `json:"count"`
				} `json:"meta"`
			} `json:"Conversation"`
		} `json:"Aggregate"`
	}

	if err := json.Unmarshal(jsonBytes, &response); err != nil {
		return 0, fmt.Errorf("unmarshal aggregate response: %w", err)
	}

	if len(response.Aggregate.Conversation) == 0 {
		return 0, nil
	}

	return int(response.Aggregate.Conversation[0].Meta.Count), nil
}

// =============================================================================
// Compile-time Interface Check
// =============================================================================

var _ StreamingChatHandler = (*streamingChatHandler)(nil)
