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
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
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
