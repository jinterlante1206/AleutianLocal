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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

// memoryChunkTimeout is the maximum time allowed for SaveMemoryChunk operations.
// This prevents goroutine accumulation if embedding or Weaviate is slow.
const memoryChunkTimeout = 60 * time.Second

// SaveMemoryChunk runs in a goroutine to save a chat turn as a searchable Document.
//
// # Description
//
// Saves a conversation turn to the Document class with the version_tag "chat_memory"
// for semantic search during follow-up queries. The turn is embedded and stored
// with a vector for similarity search.
//
// When sessionCtx.TTL is provided, the session's TTL is reset on each message (the
// "reset on activity" behavior for ephemeral sessions). The dataspace and pipeline
// are stored with the session for resume functionality.
//
// # Inputs
//
//   - client: Weaviate client for database access.
//   - sessionId: The session ID to associate with this memory chunk.
//   - question: The user's question for this turn.
//   - answer: The AI's response for this turn.
//   - turnNumber: The sequential turn number within the session (1-indexed).
//   - sessionCtx: Session context with dataspace, pipeline, and TTL.
//
// # Thread Safety
//
// This function is safe to call from a goroutine. It does not block the caller.
// Operations are bounded by memoryChunkTimeout (60s) to prevent goroutine leaks.
//
// # Example
//
//	ctx := datatypes.SessionContext{DataSpace: "work", Pipeline: "verified", TTL: "24h"}
//	go SaveMemoryChunk(client, "sess_123", "What is Chrysler?", "Chrysler is...", 5, ctx)
func SaveMemoryChunk(client *weaviate.Client, sessionId, question, answer string, turnNumber int, sessionCtx datatypes.SessionContext) {
	// Create a bounded context to prevent goroutine accumulation
	ctx, cancel := context.WithTimeout(context.Background(), memoryChunkTimeout)
	defer cancel()

	slog.Info("Saving chat turn to Document class for RAG memory",
		"sessionId", sessionId,
		"turnNumber", turnNumber,
		"sessionTTL", sessionCtx.TTL,
		"dataSpace", sessionCtx.DataSpace,
		"pipeline", sessionCtx.Pipeline)

	// Use TTL-aware session management when TTL is provided (also stores dataspace/pipeline)
	var sessionUUID string
	var err error
	if sessionCtx.TTL != "" || sessionCtx.DataSpace != "" || sessionCtx.Pipeline != "" {
		sessionUUID, err = datatypes.FindOrCreateSessionWithTTL(ctx, client, sessionId, sessionCtx)
	} else {
		sessionUUID, err = datatypes.FindOrCreateSessionUUID(ctx, client, sessionId)
	}
	if err != nil {
		slog.Error("Failed to find parent session for memory chunk, aborting save.",
			"sessionId", sessionId, "error", err)
		return
	}

	// 1. Format the content just like your RAG pipeline would expect.
	content := fmt.Sprintf("User: %s\nAI: %s", question, answer)

	// 2. Get the embedding for this Q&A content (with context for timeout).
	var embResp datatypes.EmbeddingResponse
	if err := embResp.GetWithContext(ctx, content); err != nil {
		slog.Error("Failed to get embedding for chat memory",
			"sessionId", sessionId, "error", err)
		return
	}

	// 3. Define the properties using typed struct
	parentSource := fmt.Sprintf("session_memory_%s", sessionId)
	source := fmt.Sprintf("%s_turn_%d", parentSource, turnNumber)

	props := datatypes.DocumentProperties{
		Content:      content,
		Source:       source,
		ParentSource: parentSource,
		DataSpace:    sessionId,
		VersionTag:   "chat_memory",
		TurnNumber:   turnNumber,
		IngestedAt:   time.Now().UnixMilli(),
	}
	properties := props.ToMap()
	datatypes.WithBeacon(properties, sessionUUID)

	// 4. Save to Weaviate *with* the vector.
	_, err = client.Data().Creator().
		WithClassName("Document").
		WithProperties(properties).
		WithVector(embResp.Vector).
		Do(ctx)

	if err != nil {
		slog.Error("Failed to save chat memory chunk to Weaviate",
			"sessionId", sessionId, "error", err)
		return
	}

	slog.Info("Successfully saved chat memory chunk",
		"sessionId", sessionId,
		"turnNumber", turnNumber,
		"source", source)
}

// SaveMemoryChunkWithSummary saves the memory chunk and generates a session summary on first turn.
//
// # Description
//
// Wraps SaveMemoryChunk and additionally calls SummarizeAndSaveSession when turnNumber is 1.
// This ensures the session exists before attempting to update its summary, avoiding race conditions.
//
// # Inputs
//
//   - llmClient: LLM client for summary generation.
//   - client: Weaviate client for database access.
//   - sessionId: The session ID to associate with this memory chunk.
//   - question: The user's question for this turn.
//   - answer: The AI's response for this turn.
//   - turnNumber: The sequential turn number within the session (1-indexed).
//   - sessionCtx: Session context with dataspace, pipeline, and TTL.
//
// # Thread Safety
//
// This function is safe to call from a goroutine.
func SaveMemoryChunkWithSummary(llmClient llm.LLMClient, client *weaviate.Client, sessionId, question, answer string, turnNumber int, sessionCtx datatypes.SessionContext) {
	// First, save the memory chunk (this creates the Session if needed)
	SaveMemoryChunk(client, sessionId, question, answer, turnNumber, sessionCtx)

	// Then generate summary on first turn (session now guaranteed to exist)
	if turnNumber == 1 {
		SummarizeAndSaveSession(llmClient, client, sessionId, question, answer)
	}
}
