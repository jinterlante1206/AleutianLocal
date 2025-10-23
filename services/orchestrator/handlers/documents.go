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
	"crypto/sha256"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
)

type CreateDocumentRequest struct {
	Content string `json:"content"`
	Source  string `json:"source"`
}

// CreateDocument receives data from the CLI and adds it to Weaviate
func CreateDocument(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateDocumentRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// -- Generate a deterministic UUID from the content hash --
		hash := sha256.Sum256([]byte(req.Content))
		// Use the first 16 bytes of the SHA-256 to create a valid UUID
		docUUID, _ := uuid.FromBytes(hash[:16])
		docId := docUUID.String()
		// Get embeddings
		var embResp datatypes.EmbeddingResponse
		if err := embResp.Get(req.Content); err != nil {
			slog.Error("error getting the embedding for document", "source", req.Source, "error", err)
			c.JSON(http.StatusInternalServerError,
				gin.H{"error": "failed to get the embeddings for the source document"})
			return
		}
		// Prepare properties
		properties := map[string]interface{}{
			"content": req.Content,
			"source":  req.Source,
		}

		_, err := client.Data().Creator().
			WithClassName("Document").
			WithID(docId).
			WithProperties(properties).
			WithVector(embResp.Vector).
			Do(context.Background())

		if err != nil {
			if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "status code: 422") {
				slog.Warn("Document likely already exists in Weaviate, skipping.", "source", req.Source, "weaviate_error", err.Error())
				c.JSON(http.StatusOK, gin.H{"status": "skipped", "message": "Document likely already exists."})
				return
			}
			slog.Error("Failed to create document object in Weaviate", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "The vectorDB returned an error"})
			return
		}

		slog.Info("Successfully created document", "source", req.Source, "id", docId)
		c.JSON(http.StatusCreated, gin.H{"status": "success", "source": req.Source})
	}
}
