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
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("aleutian.orchestrator.handlers")

func ListSessions(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracer.Start(c.Request.Context(), "ListSessions.handler")
		defer span.End()
		slog.Info("Received request to list sessions")
		fields := []graphql.Field{
			{Name: "session_id"},
			{Name: "summary"},
			{Name: "timestamp"},
		}
		result, err := client.GraphQL().Get().
			WithClassName("Session").
			WithFields(fields...).
			Do(ctx)
		if err != nil {
			slog.Error("failed to query Weaviate for sessions", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": "failed to query Weaviate for sessions"})
			return
		}
		c.JSON(http.StatusOK, result.Data)
	}
}

func DeleteSessions(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracer.Start(c.Request.Context(), "DeleteSessions.handler")
		defer span.End()
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
			Do(ctx)
		if err != nil {
			slog.Error("failed to delete objects from the Weaviate DB", "error", err)
			span.RecordError(err)
		}
		// 2. Delete the main Session object itself
		_, err = client.Batch().ObjectsBatchDeleter().
			WithClassName("Session").
			WithOutput("minimal").
			WithWhere(whereFilter).
			Do(ctx)
		if err != nil {
			slog.Error("failed to delete session object from the Weaviate DB", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fully delete session"})
			return
		}

		slog.Info("successfully deleted objects from the Weaviate DB", "response", &response.Output)
		slog.Info("Successfully deleted all data for session", "sessionId", session)
		c.JSON(http.StatusOK, gin.H{"status": "success", "deleted_session_id": session})
	}
}

// GetSessionHistory retrieves all chat turns for a specific session
func GetSessionHistory(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracer.Start(c.Request.Context(), "GetSessionHistory.handler")
		defer span.End()
		sessionId := c.Param("sessionId")
		if sessionId == "" {
			c.JSON(http.StatusBadRequest,
				gin.H{"error": "a sessionId is required to resume a session"})
			return
		}
		slog.Info("Received a request for session history", "sessionId", sessionId)
		// Define the fields to retrieve from the VectorDB Conversation class
		fields := []graphql.Field{
			{Name: "question"},
			{Name: "answer"},
			{Name: "timestamp"},
		}
		// Create a filter to find objects by session_id (defined in the weaviate schema)
		whereFilter := filters.Where().
			WithPath([]string{"session_id"}).
			WithOperator(filters.Equal).
			WithValueString(sessionId)
		// Create a sorter to get them in chronological order
		sortBy := graphql.Sort{
			Path:  []string{"timestamp"},
			Order: graphql.Asc,
		}
		// Execute the query
		result, err := client.GraphQL().Get().
			WithClassName("Conversation").
			WithWhere(whereFilter).
			WithSort(sortBy).
			WithFields(fields...).
			Do(ctx)
		if err != nil {
			slog.Error("failed to query Weaviate for session history", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query history"})
			return
		}
		// Return the raw data (result.Data["Get"]["Conversation"]
		c.JSON(http.StatusOK, result.Data)
	}
}

// GetSessionDocuments pulls the non-global/session scoped documents
func GetSessionDocuments(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracer.Start(c.Request.Context(), "GetSessionDocuments.handler")
		defer span.End()
		sessionId := c.Param("sessionId")
		if sessionId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
			return
		}
		slog.Info("Received a request for session documents", "sessionId", sessionId)

		whereFilter := filters.Where().
			WithPath([]string{"inSession", "Session", "session_id"}).
			WithOperator(filters.Equal).
			WithValueString(sessionId)

		resp, err := client.GraphQL().Aggregate().
			WithClassName("Document").
			WithWhere(whereFilter).
			WithGroupBy("parent_source").
			WithFields(
				graphql.Field{Name: "meta", Fields: []graphql.Field{
					{Name: "count"},
				}}).
			Do(ctx)
		if err != nil {
			slog.Error("Failed to aggregate session documents", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query session documents"})
			return
		}

		// --- Minimal parser for this specific aggregation ---
		var parsedResp struct {
			Aggregate struct {
				Document []struct {
					GroupedBy struct {
						Value string `json:"value"`
					} `json:"groupedBy"`
					Meta struct {
						Count float64 `json:"count"`
					} `json:"meta"`
				} `json:"Document"`
			} `json:"Aggregate"`
		}

		rawRespData, _ := json.Marshal(resp.Data)
		if err := json.Unmarshal(rawRespData, &parsedResp); err != nil {
			slog.Error("Failed to unmarshal session documents response", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse response"})
			return
		}

		// Build the final list
		docList := make([]map[string]interface{}, 0)
		for _, group := range parsedResp.Aggregate.Document {
			docList = append(docList, map[string]interface{}{
				"parent_source": group.GroupedBy.Value,
				"chunk_count":   int(group.Meta.Count),
			})
		}

		slog.Info("Successfully fetched session document list", "count", len(docList))
		c.JSON(http.StatusOK, gin.H{"documents": docList})

	}
}
