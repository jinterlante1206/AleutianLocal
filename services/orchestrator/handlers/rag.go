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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Ensure tracer is defined, matching chat.go
var ragTracer = otel.Tracer("aleutian.orchestrator.handlers")

// --- 1. THIS IS YOUR ORIGINAL HANDLER, NOW A SIMPLE WRAPPER ---
// This function is referenced by routes.go and is called by 'aleutian ask'
func HandleRAGRequest(client *weaviate.Client, llmClient llm.LLMClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := ragTracer.Start(c.Request.Context(), "HandleRAGRequest")
		defer span.End()

		// Use the struct from datatypes/rag.go
		var req datatypes.RAGRequest
		if err := c.BindJSON(&req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Call the new, reusable logic function
		resp, err := runRAGLogic(ctx, client, llmClient, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to run RAG logic", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// --- 2. THIS IS THE NEW REUSABLE LOGIC FUNCTION ---
// Both HandleRAGRequest and HandleOpenAICompatChat will call this.
func runRAGLogic(ctx context.Context, client *weaviate.Client, llmClient llm.LLMClient, req datatypes.RAGRequest) (datatypes.RAGResponse, error) {
	ctx, span := ragTracer.Start(ctx, "runRAGLogic")
	defer span.End()

	sessionID := req.SessionId
	if sessionID == "" {
		sessionID = uuid.New().String()
		slog.Info("New session started", "sessionId", sessionID)
	}

	span.SetAttributes(attribute.String("session.id", sessionID))

	var answer string
	var sources []datatypes.SourceInfo
	var err error

	if req.NoRag {
		// --- Logic for --no-rag flag ---
		slog.Info("Handling --no-rag request", "sessionId", sessionID)
		span.AddEvent("Handling --no-rag request")

		// Use Generate, following the pattern from session_summary.go
		params := llm.GenerationParams{} // Use defaults
		answer, err = llmClient.Generate(ctx, req.Query, params)
		if err != nil {
			span.RecordError(err)
			slog.Error("LLMClient.Generate failed for --no-rag", "error", err)
			return datatypes.RAGResponse{}, err
		}
	} else {
		// --- Logic for RAG pipeline ---
		slog.Info("Handling RAG request", "pipeline", req.Pipeline, "sessionId", sessionID)
		span.AddEvent("Handling RAG request")

		// 1. Get the base URL from the environment
		ragEngineBaseURL := os.Getenv("RAG_ENGINE_URL")
		if ragEngineBaseURL == "" {
			ragEngineBaseURL = "http://aleutian-rag-engine:8000" // Default base
			slog.Warn("RAG_ENGINE_URL not set, using default", "url", ragEngineBaseURL)
		}

		// 2. Determine the pipeline path
		pipelinePath := req.Pipeline
		if pipelinePath == "" {
			pipelinePath = "reranking" // Fallback to default
			slog.Warn("Pipeline not specified in RAGRequest, defaulting to 'reranking'")
		}

		// 3. Construct the full URL, e.g., http://aleutian-rag-engine:8000/rag/reranking
		fullRagURL := fmt.Sprintf("%s/rag/%s", strings.TrimSuffix(ragEngineBaseURL, "/"), pipelinePath)
		slog.Info("Calling RAG engine pipeline", "url", fullRagURL)
		// --- END OF FIX ---

		reqBodyBytes, _ := json.Marshal(req)

		// Make the HTTP request to the python RAG-Engine service
		httpReq, _ := http.NewRequestWithContext(ctx, "POST", fullRagURL, bytes.NewBuffer(reqBodyBytes)) // Use fullRagURL
		httpReq.Header.Set("Content-Type", "application/json")

		resp, httpErr := http.DefaultClient.Do(httpReq)
		if httpErr != nil {
			span.RecordError(httpErr)
			slog.Error("Failed to call RAG engine", "error", httpErr, "url", fullRagURL)
			return datatypes.RAGResponse{}, httpErr
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			err = fmt.Errorf("RAG engine returned status %d: %s", resp.StatusCode, string(body))
			span.RecordError(err)
			slog.Error("RAG engine error", "error", err, "url", fullRagURL)
			return datatypes.RAGResponse{}, err
		}

		var ragEngineResp datatypes.RagEngineResponse
		if err = json.NewDecoder(resp.Body).Decode(&ragEngineResp); err != nil {
			span.RecordError(err)
			slog.Error("Failed to parse response from RAG engine", "error", err)
			return datatypes.RAGResponse{}, err
		}
		answer = ragEngineResp.Answer
		sources = ragEngineResp.Sources
	}

	// Return the final response
	return datatypes.RAGResponse{
		Answer:    answer,
		SessionId: sessionID,
		Sources:   sources,
	}, nil
}
