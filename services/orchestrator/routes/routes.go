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
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

func SetupRoutes(router *gin.Engine, client *weaviate.Client, globalLLMClient llm.LLMClient,
	policyEngine *policy_engine.PolicyEngine) {

	router.GET("/health", handlers.HealthCheck)
	router.StaticFS("/ui", http.Dir("/app/ui"))

	// Add a friendly redirect from /chat to the actual HTML file
	router.GET("/chat", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui/chat.html")
	})

	// API version 1 group
	v1 := router.Group("/v1")
	{
		v1.POST("/documents", handlers.CreateDocument(client))
		v1.GET("/documents", handlers.ListDocuments(client))
		v1.DELETE("/document", handlers.DeleteBySource(client))
		v1.POST("/rag", handlers.HandleRAGRequest(client, globalLLMClient))
		v1.POST("/chat/direct", handlers.HandleDirectChat(globalLLMClient, policyEngine))
		v1.GET("/chat/ws", handlers.HandleChatWebSocket(client, globalLLMClient, policyEngine))
		v1.POST("/timeseries/forecast", handlers.HandleTimeSeriesForecast())
		v1.POST("/data/fetch", handlers.HandleDataFetch())
		v1.POST("/models/pull", handlers.HandleModelPull())
		v1.POST("/agent/trace", handlers.HandleAgentTrace(policyEngine))
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
