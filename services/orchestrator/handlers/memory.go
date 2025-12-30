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

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

// SaveMemoryChunk runs in a goroutine to save a chat turn as a searchable Document.
// It does the "slow" work (embedding) without blocking the main chat response.
func SaveMemoryChunk(client *weaviate.Client, sessionId, question, answer string) {
	slog.Info("Saving chat turn to Document class for RAG memory", "sessionId", sessionId)

	sessionUUID, err := datatypes.FindOrCreateSessionUUID(context.Background(), client, sessionId)
	if err != nil {
		slog.Error("Failed to find parent session for memory chunk, aborting save.", "sessionId", sessionId, "error", err)
		return
	}
	// 1. Format the content just like your RAG pipeline would expect.
	content := fmt.Sprintf("User: %s\nAI: %s", question, answer)

	// 2. Get the embedding for this Q&A content.
	// We re-use the EmbeddingResponse.Get method from rag.go
	var embResp datatypes.EmbeddingResponse
	if err := embResp.Get(content); err != nil {
		// Log the error but don't crash. This is a background task.
		slog.Error("Failed to get embedding for chat memory", "sessionId", sessionId, "error", err)
		return
	}

	// 3. Define the properties for the "Document" object.
	// We use the existing Document schema fields
	parentSource := fmt.Sprintf("session_memory_%s", sessionId)
	source := fmt.Sprintf("%s_ts_%d", parentSource, time.Now().UnixMilli())
	beacon := map[string]interface{}{
		"beacon": fmt.Sprintf("weaviate://localhost/Session/%s", sessionUUID),
	}

	properties := map[string]interface{}{
		"content":       content,
		"source":        source,
		"parent_source": parentSource,
		"data_space":    sessionId,
		"version_tag":   "chat_memory",
		"ingested_at":   time.Now().UnixMilli(),
		"inSession":     beacon,
	}

	// 4. Save to Weaviate *with* the vector.
	_, err = client.Data().Creator().
		WithClassName("Document").
		WithProperties(properties).
		WithVector(embResp.Vector).
		Do(context.Background())

	if err != nil {
		slog.Error("Failed to save chat memory chunk to Weaviate", "sessionId", sessionId, "error", err)
		return
	}

	slog.Info("Successfully saved chat memory chunk", "sessionId", sessionId, "source", source)
}
