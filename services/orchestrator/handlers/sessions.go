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
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
)

func ListSessions(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Info("Received request to list sessions")
		fields := []graphql.Field{
			{Name: "session_id"},
			{Name: "summary"},
			{Name: "timestamp"},
		}
		result, err := client.GraphQL().Get().
			WithClassName("Session").
			WithFields(fields...).
			Do(context.Background())
		if err != nil {
			slog.Error("failed to query Weaviate for sessions", "error", err)
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": "failed to query Weaviate for sessions"})
			return
		}
		c.JSON(http.StatusOK, result.Data)
	}
}

func DeleteSessions(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := c.Param("sessionId")
		slog.Info("Received a request to delete a session", "sessionId", session)

		// 1. Delete all Conversation objects for this sessionId
		whereFilter := filters.Where().
			WithPath([]string{"session_id"}).
			WithOperator(filters.Equal).
			WithValueString(session)

		response, err := client.Batch().ObjectsBatchDeleter().
			WithClassName("Conversation").
			WithOutput("minimal").
			WithWhere(whereFilter).
			Do(context.Background())
		if err != nil {
			slog.Error("failed to delete objects from the Weaviate DB", "error", err)
		}
		// 2. Delete the main Session object itself
		_, err = client.Batch().ObjectsBatchDeleter().
			WithClassName("Session").
			WithOutput("minimal").
			WithWhere(whereFilter).
			Do(context.Background())
		if err != nil {
			slog.Error("failed to delete session object from the Weaviate DB", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fully delete session"})
			return
		}

		slog.Info("successfully deleted objects from the Weaviate DB", "response", &response.Output)
		slog.Info("Successfully deleted all data for session", "sessionId", session)
		c.JSON(http.StatusOK, gin.H{"status": "success", "deleted_session_id": session})
	}
}
