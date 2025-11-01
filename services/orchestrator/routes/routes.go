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
	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/handlers"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

func SetupRoutes(router *gin.Engine, client *weaviate.Client, globalLLMClient llm.LLMClient) {
	router.GET("/health", handlers.HealthCheck)

	// API version 1 group
	v1 := router.Group("/v1")
	{
		v1.POST("/documents", handlers.CreateDocument(client))
		v1.POST("/rag", handlers.HandleRAGRequest(client, globalLLMClient))
		v1.POST("/chat/direct", handlers.HandleDirectChat(globalLLMClient))
		// Session administration routes
		sessions := v1.Group("/sessions")
		{
			sessions.GET("", handlers.ListSessions(client))
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
