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
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/tmc/langchaingo/textsplitter"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

var (
	CHUNK_SIZE        = 1000
	CHUNK_OVERLAP     = int(float64(CHUNK_SIZE) * 0.10) // Chunk_overlap is 10% of the CHUNK_SIZE
	defaultSeparators = []string{"\n\n", "\n", " ", ""}
	pythonSeparators  = []string{"\nclass ", "\ndef ", "\n\t", "\n", " "}
	cStyleSeparators  = []string{
		"\nfunction ", "\nclass ", "\ninterface ",
		"\npublic ", "\nprivate ", "\nprotected ",
		"\nfunc", "\ntype",
		"\n\n", "\n", " ", "",
	}

	markdownSeparators = []string{
		"\n# ", "\n## ", "\n### ", "\n#### ", "\n##### ", "\n###### ",
		"\n\n", "\n", " ", "",
	}
)

type IngestDocumentRequest struct {
	Content    string `json:"content"`
	Source     string `json:"source"`
	DataSpace  string `json:"data_space"`
	VersionTag string `json:"version_tag"`
}

type BatchEmbeddingRequest struct {
	Texts []string `json:"texts"`
}

type BatchEmbeddingResponse struct {
	Id        string      `json:"id"`
	Timestamp int64       `json:"timestamp"`
	Vectors   [][]float32 `json:"vectors"`
	Model     string      `json:"model"`
	Dim       int         `json:"dim"`
}

// CreateDocument receives data from the CLI and adds it to Weaviate
func CreateDocument(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req IngestDocumentRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// --- GET TOKENIZER CONFIG FROM ORCHESTRATOR'S ENV VARS ---
		embeddingServiceBaseURL := os.Getenv("EMBEDDING_SERVICE_URL")
		if embeddingServiceBaseURL == "" {
			slog.Error("EMBEDDING_SERVICE_URL not set for orchestrator")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Embedding service not configured"})
			return
		}
		batchEmbeddingURL := strings.TrimSuffix(embeddingServiceBaseURL, "/embed") + "/batch_embed"
		slog.Info("Ingestion request received", "source", req.Source)

		// --- GET SPLITTER ---
		splitter := getSplitterForFile(req.Source)

		// --- SPLIT TEXT ---
		chunks, err := splitter.SplitText(req.Content)
		if err != nil {
			slog.Error("Failed to split text", "source", req.Source, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to split content"})
			return
		}
		if len(chunks) == 0 {
			slog.Warn("No chunks produced after splitting", "source", req.Source)
			c.JSON(http.StatusOK, gin.H{"status": "skipped", "source": req.Source, "chunks_created": 0, "message": "No content to ingest."})
			return
		}
		slog.Info("Split document into chunks", "source", req.Source, "chunk_count", len(chunks))

		vectors, err := callBatchEmbed(batchEmbeddingURL, chunks)
		if err != nil {
			slog.Error("Failed to get batch embeddings", "source", req.Source, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(vectors) != len(chunks) {
			slog.Error("Mismatch between chunk count and vector count", "chunks", len(chunks), "vectors", len(vectors))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Embedding service returned mismatched vector count"})
			return
		}
		slog.Info("Successfully generated batch embeddings", "source", req.Source, "vector_count", len(vectors))

		// --- Batch Weaviate Import in one request ---
		batcher := client.Batch().ObjectsBatcher()
		objects := make([]*models.Object, len(chunks))

		// Get embeddings
		for i, chunk := range chunks {
			chunkSource := fmt.Sprintf("%s_part_%d", req.Source, i+1)
			hash := sha256.Sum256([]byte(chunk))
			docUUID, _ := uuid.FromBytes(hash[:16])
			docId := docUUID.String()

			objects[i] = &models.Object{
				Class:  "Document",
				ID:     strfmt.UUID(docId),
				Vector: vectors[i],
				Properties: map[string]interface{}{
					"content":       chunk,
					"source":        chunkSource,
					"parent_source": req.Source,
					"data_space":    req.DataSpace,
					"version_tag":   req.VersionTag,
					"ingested_at":   time.Now().UnixMilli(),
				},
			}
		}

		batcher.WithObjects(objects...)

		resp, err := batcher.Do(c.Request.Context())
		if err != nil {
			slog.Error("Failed to perform batch import to Weaviate", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save objects to Weaviate"})
			return
		}

		chunksCreated := 0
		hasErrors := false
		if resp != nil {
			for _, item := range resp {
				if item.Result != nil && item.Result.Status != nil && *item.Result.Status == "SUCCESS" {
					chunksCreated++
				} else {
					hasErrors = true
					if item.Result != nil && item.Result.Errors != nil && len(item.Result.Errors.Error) > 0 {
						for _, errItem := range item.Result.Errors.Error {
							slog.Warn("Error in Weaviate batch item", "source", req.Source, "error", errItem.Message)
						}
					} else {
						var status string
						if item.Result != nil && item.Result.Status != nil {
							status = *item.Result.Status
						} else {
							status = "UNKNOWN"
						}
						slog.Warn("Failed Weaviate batch item, no error provided", "source", req.Source, "status", status)
					}
				}
			}
		}

		if hasErrors {
			slog.Warn("Errors encountered during Weaviate batch import", "source", req.Source, "successful_chunks", chunksCreated)
		}

		slog.Info("Successfully processed document", "source", req.Source, "chunks_processed",
			chunksCreated)
		c.JSON(http.StatusCreated, gin.H{
			"status":           "success",
			"source":           req.Source,
			"chunks_processed": chunksCreated,
		})
	}
}

func callBatchEmbed(batchEmbedURL string, chunks []string) ([][]float32, error) {
	reqBody := BatchEmbeddingRequest{Texts: chunks}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch embed request: %w", err)
	}

	// Use a client with a longer timeout for batch processing
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(batchEmbedURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to call /batch_embed endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read /batch_embed response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/batch_embed returned status %d: %s", resp.StatusCode, string(body))
	}

	var batchResp BatchEmbeddingResponse
	if err = json.Unmarshal(body, &batchResp); err != nil {
		return nil, fmt.Errorf("failed to decode batch embed response: %w", err)
	}

	return batchResp.Vectors, nil
}

func getSplitterForFile(filename string) textsplitter.TextSplitter {
	ext := filepath.Ext(filename)
	switch ext {
	case ".md":
		return textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(CHUNK_SIZE),
			textsplitter.WithChunkOverlap(CHUNK_OVERLAP),
			textsplitter.WithSeparators(markdownSeparators),
		)

	case ".py":
		return textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(CHUNK_SIZE),
			textsplitter.WithChunkOverlap(CHUNK_OVERLAP),
			textsplitter.WithSeparators(pythonSeparators),
		)

	case ".js", ".ts", ".java", ".c", ".cpp", ".h", ".hpp", ".rs", ".go":
		return textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(CHUNK_SIZE),
			textsplitter.WithChunkOverlap(CHUNK_OVERLAP),
			textsplitter.WithSeparators(cStyleSeparators),
		)

	default:
		return textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(CHUNK_SIZE),
			textsplitter.WithChunkOverlap(CHUNK_OVERLAP),
			textsplitter.WithSeparators(defaultSeparators),
		)
	}
}
