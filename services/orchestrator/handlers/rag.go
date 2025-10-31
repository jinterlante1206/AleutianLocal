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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var httpClient = &http.Client{
	Timeout: time.Minute * 4,
}
var ragTracer = otel.Tracer("aleutian.orchestrator.handlers")

func HandleRAGRequest(client *weaviate.Client, globalLLMClient llm.LLMClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := ragTracer.Start(c.Request.Context(), "HandleRAGRequest")
		defer span.End()
		var request datatypes.RAGRequest // Changed from req to request for clarity
		if err := c.BindJSON(&request); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			slog.Error("Failed to bind RAG request JSON", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}
		span.SetAttributes(attribute.String("pipeline", request.Pipeline))
		span.SetAttributes(attribute.Bool("no_rag", request.NoRag))
		span.SetAttributes(attribute.String("session_id", request.SessionId))
		var answer string
		var sources []datatypes.SourceInfo
		var err error
		isFirstTurn := false
		sessionId := request.SessionId // Use sessionId variable for clarity
		if sessionId == "" {
			sessionId = uuid.New().String()
			isFirstTurn = true
			span.SetAttributes(attribute.String("session_id_new", sessionId))
			slog.Info("No SessionId provided, creating a new one", "sessionId", sessionId)
		}

		slog.Info("Received RAG request", "query", request.Query, "pipeline", request.Pipeline, "session_id", request.SessionId)
		if request.NoRag {
			slog.Info("Handling request with --no-rag flag. Skipping RAG engine.", "query",
				request.Query)
			ctx, noRagSpan := ragTracer.Start(ctx, "HandleRAGRequest.NoRag")
			// Just use the query directly as the prompt
			params := llm.GenerationParams{} // Use defaults
			answer, err = globalLLMClient.Generate(ctx, request.Query, params)
			if err != nil {
				noRagSpan.RecordError(err)
				noRagSpan.SetStatus(codes.Error, err.Error())
				noRagSpan.End()
				slog.Error("Direct LLMClient.Generate failed", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			sources = []datatypes.SourceInfo{}
			noRagSpan.End()
		} else {
			// --- Proxy Request to RAG Engine ---
			ctx, ragSpan := ragTracer.Start(ctx, "HandleRAGRequest.RAGEngine")
			ragEngineURL := os.Getenv("RAG_ENGINE_URL")
			if ragEngineURL == "" {
				ragSpan.RecordError(fmt.Errorf("RAG_ENGINE_URL not set"))
				ragSpan.End()
				slog.Error("RAG_ENGINE_URL environment variable not set")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "RAG engine endpoint not configured"})
				return
			}

			targetURL := fmt.Sprintf("%s/rag/%s", ragEngineURL, request.Pipeline)
			if request.Pipeline == "" {
				targetURL = fmt.Sprintf("%s/rag/standard", ragEngineURL)
				slog.Warn("No pipeline specified, defaulting to standard RAG")
			}

			enginePayload := map[string]string{
				"query":      request.Query,
				"session_id": sessionId,
			}
			reqBodyBytes, err := json.Marshal(enginePayload)
			if err != nil {
				ragSpan.RecordError(err)
				ragSpan.End()
				slog.Error("Failed to marshal request body for RAG engine", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create RAG engine request"})
				return
			}

			slog.Info("Proxying RAG request", "target_url", targetURL)
			httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(reqBodyBytes))
			if err != nil {
				slog.Error("Failed to create request for RAG engine", "error", err)
				ragSpan.RecordError(err)
				ragSpan.SetStatus(codes.Error, err.Error())
				ragSpan.End()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create RAG engine request"})
				return
			}
			httpReq.Header.Set("Content-Type", "application/json")

			// Use httpClient.Do() instead of httpClient.Post()
			resp, err := httpClient.Do(httpReq)
			if err != nil {
				ragSpan.RecordError(err)
				ragSpan.End()
				slog.Error("Failed to call RAG engine", "url", targetURL, "error", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to connect to the RAG engine"})
				return
			}
			defer resp.Body.Close()

			respBodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				ragSpan.RecordError(err)
				ragSpan.End()
				slog.Error("Failed to read response body from RAG engine", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read RAG engine response"})
				return
			}

			if resp.StatusCode != http.StatusOK {
				ragSpan.RecordError(fmt.Errorf("RAG engine returned status %d", resp.StatusCode))
				ragSpan.End()
				slog.Error("RAG engine returned an error", "status_code", resp.StatusCode, "response", string(respBodyBytes))
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("RAG engine failed with status %d", resp.StatusCode), "details": string(respBodyBytes)})
				return
			}

			var engineResponse datatypes.RagEngineResponse
			if err = json.Unmarshal(respBodyBytes, &engineResponse); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				slog.Error("Failed to parse JSON response from RAG engine", "error", err, "response", string(respBodyBytes))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse RAG engine response"})
				return
			}

			answer = engineResponse.Answer
			sources = engineResponse.Sources
			ragSpan.SetAttributes(attribute.Int("response.answer_length", len(answer)))
			ragSpan.SetAttributes(attribute.Int("response.sources_count", len(sources)))
			slog.Info("Received answer from RAG engine", "answer_length", len(answer), "sources_count", len(sources))
			ragSpan.End()

		}

		// --- Save Conversation Turn ---
		convo := datatypes.Conversation{
			SessionId: sessionId, // Use consistent sessionId variable
			Question:  request.Query,
			Answer:    answer,
		}
		// Run saving in a goroutine so it doesn't block the response to the user
		go func() {
			if err := convo.Save(client); err != nil {
				slog.Error("Failed to save conversation async", "session_id", sessionId, "error", err)
			}
		}()

		// --- Handle Session Summary (First Turn Only) ---
		if isFirstTurn {
			slog.Info("First turn of a new session, triggering summarization.", "sessionId", sessionId)
			// --- FIX: Pass the llmClient interface to SummarizeAndSaveSession ---
			go SummarizeAndSaveSession(globalLLMClient, client, sessionId, request.Query, answer)
		}

		// Return the final response to the original caller
		c.JSON(http.StatusOK, datatypes.RAGResponse{
			Answer:    answer,
			SessionId: sessionId,
			Sources:   sources,
		})
	}
}
