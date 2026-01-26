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
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/gin-gonic/gin"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

type BackupRequest struct {
	Id     string `json:"id"`
	Action string `json:"action"` // "create", "list", or "restore" for now
}

func HandleBackup(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req BackupRequest
		if err := c.BindJSON(&req); err != nil {
			slog.Error("failed to parse the backup request to json", "error", err)
			c.JSON(http.StatusBadRequest,
				gin.H{"error": "failed to parse the backup request to json"})
			return
		}
		backend := "filesystem"
		slog.Info("Received a Weaviate backup request", "action", req.Action, "id", req.Id)

		switch req.Action {
		case "create":
			resp, err := client.Backup().Creator().
				WithBackend(backend).
				WithBackupID(req.Id).
				WithWaitForCompletion(true).
				Do(context.Background())
			if err != nil {
				slog.Error("backup operation failed", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			slog.Info("Backup operation successful", "id", req.Id, "status", resp.Status)
			c.JSON(http.StatusOK, resp)
		case "restore":
			resp, err := client.Backup().Restorer().
				WithBackend(backend).
				WithBackupID(req.Id).
				WithWaitForCompletion(true).
				Do(context.Background())
			if err != nil {
				slog.Error("restore operation failed", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			slog.Info("restore operation successful", "id", req.Id, "status", resp.Status)
			c.JSON(http.StatusOK, resp)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action"})
			return
		}

	}
}

func GetSummary(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Info("Received request for Weaviate summary")
		schema, err := client.Schema().Getter().Do(context.Background())
		if err != nil {
			slog.Error("Failed to get the weaviate schema", "error", err)
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": "failed to get the weaviate schema"})
			return
		}
		c.JSON(http.StatusOK, schema)
	}
}

func DeleteAll(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Warn("Received a request to DELETE ALL DATA from your vector DB")
		if err := client.Schema().AllDeleter().Do(context.Background()); err != nil {
			slog.Error("failed to delete all of the schemas and data from Weaviate")
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": "failed to wipe the Weaviate instance clean"})
		}
		slog.Info("All data and schemas have been deleted from your Weaviate instance. " +
			"It's been wiped clean.")
		slog.Info("Rebuilding default schemas...")
		datatypes.EnsureWeaviateSchema(client)
		slog.Info("Default schemas rebuilt successfully.")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Weaviate was wiped clean"})
	}
}

func DeleteBySource(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		sourceName := c.Query("source")
		if sourceName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "source name was empty"})
			return
		}
		deletingUser := "APIUser"
		slog.Warn("Received a request to DELETE all chunks for document", "source", sourceName)
		whereFilter := filters.Where().
			WithPath([]string{"parent_source"}).
			WithOperator(filters.Equal).
			WithValueString(sourceName)

		// setup the deletion audit trail
		fields := []graphql.Field{
			{Name: "source"},
			{Name: "ingested_at"},
			{Name: "version_tag"},
			{Name: "_additional { id }"},
		}
		getResp, err := client.GraphQL().Get().
			WithClassName("Document").
			WithWhere(whereFilter).
			WithFields(fields...).
			Do(ctx)

		if err != nil {
			slog.Error("failed to query objects for deletion audit", "source", sourceName, "error", err)
		}

		chunksToDelete := parseChunksFromResult(getResp)

		// Use the Batch Deleter to delete all matching objects
		response, err := client.Batch().ObjectsBatchDeleter().
			WithClassName("Document").
			WithOutput("minimal").
			WithWhere(whereFilter).
			Do(ctx)
		if err != nil {
			slog.Error("failed to delete objects by source", "source", sourceName)
			slog.Error("DELETION AUDIT",
				"action", "DELETE_BY_SOURCE",
				"status", "FAILURE",
				"user", deletingUser,
				"timestamp_utc", time.Now().UTC().Format(time.RFC3339),
				"deleted_parent_source", sourceName,
				"error", err.Error(),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Report the results
		var successful int64
		if response != nil && response.Results != nil {
			successful = response.Results.Successful
		}
		slog.Info("DELETION AUDIT",
			"action", "DELETE_BY_SOURCE",
			"status", "SUCCESS",
			"user", deletingUser,
			"timestamp_utc", time.Now().UTC().Format(time.RFC3339),
			"deleted_parent_source", sourceName,
			"chunks_deleted_count", successful,
			"chunks_deleted_details", chunksToDelete,
		)
		slog.Info("Successfully deleted objects by source", "source", sourceName)
		c.JSON(http.StatusOK, gin.H{
			"status":         "success",
			"source_deleted": sourceName,
			"chunks_deleted": successful,
		})
	}
}

func parseChunksFromResult(resp *models.GraphQLResponse) []map[string]interface{} {
	var chunks []map[string]interface{}
	if resp == nil || resp.Data == nil {
		return chunks
	}

	getMap, ok := resp.Data["Get"].(map[string]interface{})
	if !ok {
		return chunks
	}

	docList, ok := getMap["Document"].([]interface{})
	if !ok {
		return chunks
	}

	for _, item := range docList {
		if itemMap, ok := item.(map[string]interface{}); ok {
			chunkInfo := make(map[string]interface{})
			if source, ok := itemMap["source"].(string); ok {
				chunkInfo["source"] = source
			}
			if additional, ok := itemMap["_additional"].(map[string]interface{}); ok {
				if id, ok := additional["id"].(string); ok {
					chunkInfo["weaviate_id"] = id
				}
			}
			chunks = append(chunks, chunkInfo)
		}
	}
	return chunks
}

// DataspaceStats contains aggregated metrics for a dataspace.
//
// # Description
//
// DataspaceStats captures aggregate information about chunks within
// a dataspace, including count and timestamps. This is used to display
// dataspace metadata in the chat header.
//
// Note: DocumentCount actually represents chunk count, not unique source
// documents. A single uploaded file may produce many chunks for vector search.
//
// # Fields
//
//   - Name: The dataspace name (e.g., "wheat", "work", "default")
//   - DocumentCount: Number of chunks in the dataspace (not unique documents)
//   - LastUpdatedAt: Unix milliseconds of most recent document ingestion
//
// # Thread Safety
//
// This type is safe for concurrent read access.
type DataspaceStats struct {
	Name          string `json:"name"`
	DocumentCount int    `json:"document_count"`  // Actually chunk count
	LastUpdatedAt int64  `json:"last_updated_at"` // Unix ms timestamp
}

// GetDataspaceStats returns aggregated stats for a dataspace.
//
// # Description
//
// Queries Weaviate to aggregate statistics for all current documents in the
// specified dataspace. Returns document count and the most recent
// ingestion timestamp. Only counts chunks where is_current=true (ignoring
// old versions from re-ingested documents).
//
// # Inputs
//
//   - name (path param): Dataspace name to query. If "default" or empty,
//     queries all current documents without dataspace filter.
//
// # Outputs
//
//   - 200: DataspaceStats JSON response
//   - 400: Invalid dataspace name
//   - 500: Weaviate query failure
//
// # Examples
//
//	GET /v1/dataspace/wheat/stats
//	Response: {"name":"wheat","document_count":142,"last_updated_at":1737844200000}
//
// # Limitations
//
//   - Does not include content size (not stored in schema)
//   - Aggregate queries on very large dataspaces may be slow
//
// # Assumptions
//
//   - Weaviate client is properly initialized
//   - Document schema exists with data_space, is_current, and ingested_at fields
func GetDataspaceStats(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		dataspaceName := c.Param("name")
		if dataspaceName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dataspace name is required"})
			return
		}

		slog.Info("Fetching dataspace stats", "dataspace", dataspaceName)

		ctx := c.Request.Context()

		// Build the aggregate query
		// Always filter by is_current=true to only count current versions
		isCurrentFilter := filters.Where().
			WithPath([]string{"is_current"}).
			WithOperator(filters.Equal).
			WithValueBoolean(true)

		// For "default" dataspace, we query all current documents without a data_space filter
		var whereFilter *filters.WhereBuilder
		if dataspaceName != "default" {
			dataSpaceFilter := filters.Where().
				WithPath([]string{"data_space"}).
				WithOperator(filters.Equal).
				WithValueString(dataspaceName)
			whereFilter = filters.Where().
				WithOperator(filters.And).
				WithOperands([]*filters.WhereBuilder{dataSpaceFilter, isCurrentFilter})
		} else {
			whereFilter = isCurrentFilter
		}

		// Query for count using Aggregate
		fields := []graphql.Field{
			{Name: "meta", Fields: []graphql.Field{{Name: "count"}}},
		}

		builder := client.GraphQL().Aggregate().
			WithClassName("Document").
			WithFields(fields...).
			WithWhere(whereFilter)

		aggResp, err := builder.Do(ctx)
		if err != nil {
			slog.Error("failed to aggregate dataspace stats", "dataspace", dataspaceName, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query dataspace stats"})
			return
		}

		// Parse the aggregate response to get count
		docCount := parseAggregateCount(aggResp)

		// Query for max ingested_at timestamp
		var lastUpdatedAt int64
		if docCount > 0 {
			lastUpdatedAt, err = getMaxIngestedAt(ctx, client, dataspaceName)
			if err != nil {
				slog.Warn("failed to get max ingested_at", "dataspace", dataspaceName, "error", err)
				// Non-fatal, continue with 0
			}
		}

		stats := DataspaceStats{
			Name:          dataspaceName,
			DocumentCount: docCount,
			LastUpdatedAt: lastUpdatedAt,
		}

		slog.Info("Dataspace stats retrieved",
			"dataspace", dataspaceName,
			"document_count", docCount,
			"last_updated_at", lastUpdatedAt,
		)

		c.JSON(http.StatusOK, stats)
	}
}

// parseAggregateCount extracts the document count from an aggregate response.
func parseAggregateCount(resp *models.GraphQLResponse) int {
	if resp == nil || resp.Data == nil {
		return 0
	}

	aggMap, ok := resp.Data["Aggregate"].(map[string]interface{})
	if !ok {
		return 0
	}

	docList, ok := aggMap["Document"].([]interface{})
	if !ok || len(docList) == 0 {
		return 0
	}

	firstDoc, ok := docList[0].(map[string]interface{})
	if !ok {
		return 0
	}

	meta, ok := firstDoc["meta"].(map[string]interface{})
	if !ok {
		return 0
	}

	count, ok := meta["count"].(float64)
	if !ok {
		return 0
	}

	return int(count)
}

// getMaxIngestedAt queries for the most recent ingested_at timestamp in a dataspace.
func getMaxIngestedAt(ctx context.Context, client *weaviate.Client, dataspaceName string) (int64, error) {
	// Query for the single most recent document by ingested_at
	// Always filter by is_current=true to only count current versions
	isCurrentFilter := filters.Where().
		WithPath([]string{"is_current"}).
		WithOperator(filters.Equal).
		WithValueBoolean(true)

	var whereFilter *filters.WhereBuilder
	if dataspaceName != "default" {
		dataSpaceFilter := filters.Where().
			WithPath([]string{"data_space"}).
			WithOperator(filters.Equal).
			WithValueString(dataspaceName)
		whereFilter = filters.Where().
			WithOperator(filters.And).
			WithOperands([]*filters.WhereBuilder{dataSpaceFilter, isCurrentFilter})
	} else {
		whereFilter = isCurrentFilter
	}

	fields := []graphql.Field{
		{Name: "ingested_at"},
	}

	builder := client.GraphQL().Get().
		WithClassName("Document").
		WithFields(fields...).
		WithLimit(1).
		WithWhere(whereFilter).
		WithSort(graphql.Sort{Path: []string{"ingested_at"}, Order: "desc"})

	getResp, err := builder.Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("query max ingested_at: %w", err)
	}

	if getResp == nil || getResp.Data == nil {
		return 0, nil
	}

	getMap, ok := getResp.Data["Get"].(map[string]interface{})
	if !ok {
		return 0, nil
	}

	docList, ok := getMap["Document"].([]interface{})
	if !ok || len(docList) == 0 {
		return 0, nil
	}

	firstDoc, ok := docList[0].(map[string]interface{})
	if !ok {
		return 0, nil
	}

	ingestedAt, ok := firstDoc["ingested_at"].(float64)
	if !ok {
		return 0, nil
	}

	return int64(ingestedAt), nil
}
