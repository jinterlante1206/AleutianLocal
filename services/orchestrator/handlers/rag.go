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
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
)

var httpClient = &http.Client{
	Timeout: time.Minute * 4,
}

func HandleRAGRequest(client *weaviate.Client, globalLLMClient llm.LLMClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request datatypes.RAGRequest // Changed from req to request for clarity
		if err := c.BindJSON(&request); err != nil {
			slog.Error("Failed to bind RAG request JSON", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		slog.Info("Received RAG request", "query", request.Query, "pipeline", request.Pipeline, "session_id", request.SessionId)

		isFirstTurn := false
		sessionId := request.SessionId // Use sessionId variable for clarity
		if sessionId == "" {
			sessionId = uuid.New().String()
			isFirstTurn = true
			slog.Info("No SessionId provided, creating a new one", "sessionId", sessionId)
		}

		// --- Proxy Request to RAG Engine ---
		ragEngineURL := os.Getenv("RAG_ENGINE_URL")
		if ragEngineURL == "" {
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
			slog.Error("Failed to marshal request body for RAG engine", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create RAG engine request"})
			return
		}

		slog.Info("Proxying RAG request", "target_url", targetURL)
		resp, err := httpClient.Post(targetURL, "application/json", bytes.NewBuffer(reqBodyBytes))
		if err != nil {
			slog.Error("Failed to call RAG engine", "url", targetURL, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to connect to the RAG engine"})
			return
		}
		defer resp.Body.Close()

		respBodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Failed to read response body from RAG engine", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read RAG engine response"})
			return
		}

		if resp.StatusCode != http.StatusOK {
			slog.Error("RAG engine returned an error", "status_code", resp.StatusCode, "response", string(respBodyBytes))
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("RAG engine failed with status %d", resp.StatusCode), "details": string(respBodyBytes)})
			return
		}

		var engineResponse datatypes.RagEngineResponse
		if err := json.Unmarshal(respBodyBytes, &engineResponse); err != nil {
			slog.Error("Failed to parse JSON response from RAG engine", "error", err, "response", string(respBodyBytes))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse RAG engine response"})
			return
		}

		finalAnswer := engineResponse.Answer
		sources := engineResponse.Sources
		slog.Info("Received answer from RAG engine", "answer_length", len(finalAnswer), "sources_count", len(sources))

		// --- Save Conversation Turn ---
		convo := datatypes.Conversation{
			SessionId: sessionId, // Use consistent sessionId variable
			Question:  request.Query,
			Answer:    finalAnswer,
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
			go SummarizeAndSaveSession(globalLLMClient, client, sessionId, request.Query, finalAnswer)
		}

		// Return the final response to the original caller
		c.JSON(http.StatusOK, datatypes.RAGResponse{
			Answer:    finalAnswer,
			SessionId: sessionId,
			Sources:   sources,
		})
	}
}
