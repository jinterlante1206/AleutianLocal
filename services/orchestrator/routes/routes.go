// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/handlers"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/services"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

// SetupRoutes configures all HTTP routes for the orchestrator service.
//
// # Description
//
// Registers all API endpoints with the Gin router. Some endpoints require
// a Weaviate client for vector database operations; these are only registered
// when client is not nil.
//
// # Inputs
//
//   - router: Gin engine instance
//   - client: Weaviate client (may be nil if vector DB not available)
//   - globalLLMClient: LLM client for chat operations
//   - policyEngine: Policy engine for sensitive data scanning
//
// # Endpoints
//
//   - GET /health: Health check
//   - POST /v1/chat/direct: Direct LLM chat (always available)
//   - POST /v1/chat/rag: Conversational RAG (requires Weaviate)
//   - And more (see route registration below)
func SetupRoutes(router *gin.Engine, client *weaviate.Client, globalLLMClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine) {

	router.GET("/health", handlers.HealthCheck)
	router.StaticFS("/ui", http.Dir("/app/ui"))

	// Add a friendly redirect from /chat to the actual HTML file
	router.GET("/chat", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui/chat.html")
	})

	// Create ChatHandler with optional RAG service
	// RAG service is only available when vector DB (Weaviate) is configured
	var chatRAGService *services.ChatRAGService
	if client != nil {
		chatRAGService = services.NewChatRAGService(client, globalLLMClient, policyEngine)
	}
	chatHandler := handlers.NewChatHandler(globalLLMClient, policyEngine, chatRAGService)

	// API version 1 group
	v1 := router.Group("/v1")
	{
		// Chat endpoints using the new ChatHandler interface
		v1.POST("/chat/direct", chatHandler.HandleDirectChat)

		v1.POST("/timeseries/forecast", handlers.HandleTimeSeriesForecast())
		v1.POST("/data/fetch", handlers.HandleDataFetch())
		v1.POST("/trading/signal", handlers.HandleTradingSignal())
		v1.POST("/models/pull", handlers.HandleModelPull())
		v1.POST("/agent/step", handlers.HandleAgentStep(policyEngine))

		// Vector DB-dependent routes (requires Weaviate client)
		if client != nil {
			v1.GET("/chat/ws", handlers.HandleChatWebSocket(client, globalLLMClient, policyEngine))
			v1.POST("/chat/rag", chatHandler.HandleChatRAG) // Conversational RAG (default for CLI)
			v1.POST("/documents", handlers.CreateDocument(client))
			v1.GET("/documents", handlers.ListDocuments(client))
			v1.DELETE("/document", handlers.DeleteBySource(client))
			v1.POST("/rag", handlers.HandleRAGRequest(client, globalLLMClient)) // Single-shot RAG (aleutian ask)

			// Session administration routes
			sessions := v1.Group("/sessions")
			{
				sessions.GET("", handlers.ListSessions(client))
				sessions.GET("/:sessionId/history", handlers.GetSessionHistory(client))
				sessions.GET("/:sessionId/documents", handlers.GetSessionDocuments(client))
				sessions.DELETE("/:sessionId", handlers.DeleteSessions(client))
			}

			// Weaviate administration routes
			weaviateAdmin := v1.Group("/weaviate")
			{
				weaviateAdmin.POST("/backups", handlers.HandleBackup(client))
				weaviateAdmin.GET("/summary", handlers.GetSummary(client))
				weaviateAdmin.DELETE("/data", handlers.DeleteAll(client))
			}
		}
	}
}
