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
	"context"
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
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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
	Content     string `json:"content"`
	Source      string `json:"source"`
	DataSpace   string `json:"data_space"`
	VersionTag  string `json:"version_tag"`
	SessionUUID string `json:"session_id"`
}

type IngestedDocument struct {
	ParentSource string `json:"parent_source"`
	ChunkCount   int    `json:"chunk_count"`
	DataSpace    string `json:"data_space"`
	VersionTag   string `json:"version_tag"`
	IngestedAt   int64  `json:"ingested_at"`
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

type gqlAggregateResponse struct {
	Aggregate struct {
		Document []struct {
			GroupedBy struct {
				Value string `json:"value"`
			} `json:"groupedBy"`
			Meta struct {
				Count float64 `json:"count"`
			} `json:"meta"`
			DataSpace struct {
				TopOccurrences []struct {
					Value string `json:"value"`
				} `json:"topOccurrences"`
			} `json:"data_space"`
			VersionTag struct {
				TopOccurrences []struct {
					Value string `json:"value"`
				} `json:"topOccurrences"`
			} `json:"version_tag"`
			IngestedAt struct {
				Maximum float64 `json:"maximum"`
			} `json:"ingested_at"`
		} `json:"Document"`
	} `json:"Aggregate"`
}

// CreateDocument receives data from the CLI and adds it to Weaviate
// This is now a thin wrapper around RunIngestion
func CreateDocument(client *weaviate.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req IngestDocumentRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		chunksCreated, err := RunIngestion(c.Request.Context(), client, req)
		if err != nil {
			slog.Error("Ingestion failed", "source", req.Source, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		slog.Info("Successfully processed document via API", "source", req.Source, "chunks_processed", chunksCreated)
		c.JSON(http.StatusCreated, gin.H{
			"status":           "success",
			"source":           req.Source,
			"chunks_processed": chunksCreated,
		})
	}
}

// ListDocuments gets a unique list of all ingested 'parent_source' files
func ListDocuments(client *weaviate.Client) gin.HandlerFunc {
	tracer := otel.Tracer("aleutian.orchestrator.handlers")
	return func(c *gin.Context) {
		ctx, span := tracer.Start(c.Request.Context(), "ListDocuments.handler")
		defer span.End()
		slog.Info("Received request to list ingested documents")

		resp, err := client.GraphQL().Aggregate().
			WithClassName("Document").
			WithGroupBy("parent_source").
			WithFields(
				graphql.Field{
					Name: "meta",
					Fields: []graphql.Field{
						{Name: "count"},
					},
				},
				graphql.Field{
					Name: "data_space",
					Fields: []graphql.Field{
						{
							Name:   "topOccurrences(limit: 1)", // <-- Correct v5 syntax
							Fields: []graphql.Field{{Name: "value"}},
						},
					},
				},
				graphql.Field{
					Name: "version_tag",
					Fields: []graphql.Field{
						{
							Name:   "topOccurrences(limit: 1)", // <-- Correct v5 syntax
							Fields: []graphql.Field{{Name: "value"}},
						},
					},
				},
				graphql.Field{
					Name: "ingested_at",
					Fields: []graphql.Field{
						{
							Name: "maximum",
						},
					},
				},
			).
			Do(ctx)
		if err != nil {
			slog.Error("Failed to aggregate documents from Weaviate", "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query documents"})
			return
		}

		// Marshal the raw response data back to JSON
		rawRespData, err := json.Marshal(resp.Data)
		if err != nil {
			slog.Error("Failed to re-marshal weaviate response", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse response data"})
			return
		}

		// Unmarshal the JSON bytes into our strongly-typed struct
		var parsedResp gqlAggregateResponse
		if err := json.Unmarshal(rawRespData, &parsedResp); err != nil {
			slog.Error("Failed to unmarshal weaviate response into struct", "error", err, "raw_data", string(rawRespData))
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal response"})
			return
		}

		// Build the final docList from the struct
		docList := make([]IngestedDocument, 0)
		getStringValue := func(occurrences []struct {
			Value string `json:"value"`
		}) string {
			if len(occurrences) > 0 {
				return occurrences[0].Value
			}
			return ""
		}
		for _, group := range parsedResp.Aggregate.Document {
			// Filter out the RAG memory chunks from the UI list
			versionTag := getStringValue(group.VersionTag.TopOccurrences)
			if versionTag != "chat_memory" {
				docList = append(docList, IngestedDocument{
					ParentSource: group.GroupedBy.Value,
					ChunkCount:   int(group.Meta.Count),
					DataSpace:    getStringValue(group.DataSpace.TopOccurrences),
					VersionTag:   versionTag,
					IngestedAt:   int64(group.IngestedAt.Maximum),
				})
			}
		}
		slog.Info("Successfully fetched document list", "count", len(docList))
		c.JSON(http.StatusOK, gin.H{"documents": docList})
	}
}

// RunIngestion is the refactored, reusable logic for ingesting a document.
func RunIngestion(ctx context.Context, client *weaviate.Client, req IngestDocumentRequest) (int, error) {
	embeddingServiceBaseURL := os.Getenv("EMBEDDING_SERVICE_URL")
	if embeddingServiceBaseURL == "" {
		slog.Error("EMBEDDING_SERVICE_URL not set for orchestrator")
		return 0, fmt.Errorf("Embedding service not configured")
	}
	batchEmbeddingURL := strings.TrimSuffix(embeddingServiceBaseURL, "/embed") + "/batch_embed"
	slog.Info("Ingestion request received", "source", req.Source)

	// --- GET SPLITTER ---
	splitter := getSplitterForFile(req.Source)

	// --- SPLIT TEXT ---
	chunks, err := splitter.SplitText(req.Content)
	if err != nil {
		slog.Error("Failed to split text", "source", req.Source, "error", err)
		return 0, fmt.Errorf("Failed to split content: %w", err)
	}
	if len(chunks) == 0 {
		slog.Warn("No chunks produced after splitting", "source", req.Source)
		return 0, nil
	}
	slog.Info("Split document into chunks", "source", req.Source, "chunk_count", len(chunks))

	vectors, err := callBatchEmbed(batchEmbeddingURL, chunks)
	if err != nil {
		slog.Error("Failed to get batch embeddings", "source", req.Source, "error", err)
		return 0, err
	}
	if len(vectors) != len(chunks) {
		slog.Error("Mismatch between chunk count and vector count", "chunks", len(chunks), "vectors", len(vectors))
		return 0, fmt.Errorf("embedding service returned mismatched vector count")
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
		properties := map[string]interface{}{
			"content":       chunk,
			"source":        chunkSource,
			"parent_source": req.Source,
			"data_space":    req.DataSpace,
			"version_tag":   req.VersionTag,
			"ingested_at":   time.Now().UnixMilli(),
		}
		if req.SessionUUID != "" {
			beacon := map[string]interface{}{
				"beacon": fmt.Sprintf("weaviate://localhost/Session/%s", req.SessionUUID),
			}
			properties["inSession"] = []map[string]interface{}{beacon}
		}

		objects[i] = &models.Object{
			Class:      "Document",
			ID:         strfmt.UUID(docId),
			Vector:     vectors[i],
			Properties: properties,
		}

	}

	batcher.WithObjects(objects...)

	resp, err := batcher.Do(ctx)
	if err != nil {
		slog.Error("Failed to perform batch import to Weaviate", "error", err)
		return 0, fmt.Errorf("failed to save objects to Weaviate: %w", err)
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

	return chunksCreated, nil
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
