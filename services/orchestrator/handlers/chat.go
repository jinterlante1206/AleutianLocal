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
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/services"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var chatTracer = otel.Tracer("aleutian.orchestrator.handlers")

type DirectChatRequest struct {
	Messages       []datatypes.Message `json:"messages"`
	EnableThinking bool                `json:"enable_thinking"` // New
	BudgetTokens   int                 `json:"budget_tokens"`   // New
	Tools          []interface{}       `json:"tools"`           // New
}

func HandleDirectChat(llmClient llm.LLMClient, pe *policy_engine.PolicyEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := chatTracer.Start(c.Request.Context(), "HandleDirectChat")
		defer span.End()
		var req DirectChatRequest
		if err := c.BindJSON(&req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			slog.Error("Failed to parse the chat request", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		if len(req.Messages) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no messages provided"})
			return
		}

		// Scan the last message (the user's new input)
		// Optionally loop through all if you want to be extra safe
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" {
			findings := pe.ScanFileContent(lastMsg.Content)
			if len(findings) > 0 {
				slog.Warn("Blocked chat request due to policy violation", "findings", len(findings))
				c.JSON(http.StatusForbidden, gin.H{
					"error":    "Policy Violation: Message contains sensitive data.",
					"findings": findings,
				})
				return
			}
		}

		params := llm.GenerationParams{
			EnableThinking:  req.EnableThinking,
			BudgetTokens:    req.BudgetTokens,
			ToolDefinitions: req.Tools,
		}
		answer, err := llmClient.Chat(ctx, req.Messages, params)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			slog.Error("LLMClient.Chat failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"answer": answer})
	}
}

// HandleChatRAG returns a gin.HandlerFunc that handles conversational RAG requests.
//
// This handler is the HTTP layer for the ChatRAGService. It performs only
// HTTP-related tasks:
//   - Binding and parsing the JSON request body
//   - Calling the service layer for business logic
//   - Mapping service errors to appropriate HTTP status codes
//   - Serializing and returning the JSON response
//
// All business logic (validation, policy scanning, RAG orchestration) is
// delegated to the ChatRAGService, keeping this handler thin and focused.
//
// Request/Response:
//
//	POST /v1/chat/rag
//	Content-Type: application/json
//
//	Request Body:
//	{
//	    "message": "What is the authentication flow?",
//	    "session_id": "optional-session-id",
//	    "pipeline": "reranking",
//	    "bearing": "optional-topic-filter",
//	    "stream": false,
//	    "history": []
//	}
//
//	Response (200 OK):
//	{
//	    "id": "response-uuid",
//	    "created_at": 1704067200,
//	    "answer": "The authentication flow...",
//	    "session_id": "session-uuid",
//	    "sources": [...],
//	    "turn_count": 1
//	}
//
// Error Responses:
//   - 400 Bad Request: Invalid JSON or validation failure
//   - 403 Forbidden: Policy violation (sensitive data detected)
//   - 500 Internal Server Error: RAG engine or LLM failure
//
// Parameters:
//   - service: A configured ChatRAGService instance. The service must be
//     initialized with all required dependencies (Weaviate client, LLM client,
//     policy engine). Must not be nil.
//
// Example:
//
//	chatRAGService := services.NewChatRAGService(weaviateClient, llmClient, policyEngine)
//	router.POST("/v1/chat/rag", handlers.HandleChatRAG(chatRAGService))
func HandleChatRAG(service *services.ChatRAGService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := chatTracer.Start(c.Request.Context(), "HandleChatRAG")
		defer span.End()

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

		// Step 2: Call service layer
		resp, err := service.Process(ctx, &req)
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
			span.SetStatus(codes.Error, err.Error())
			slog.Error("Chat RAG processing failed",
				"error", err,
				"requestId", req.Id,
				"sessionId", req.SessionId,
			)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to process request",
				"details": err.Error(),
			})
			return
		}

		// Step 3: Return successful response
		c.JSON(http.StatusOK, resp)
	}
}
