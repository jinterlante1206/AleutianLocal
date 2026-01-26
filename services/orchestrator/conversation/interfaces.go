// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package conversation provides semantic memory capabilities for conversation history.
//
// # Description
//
// This package implements hybrid retrieval of conversation history, combining
// recency-based retrieval from the Conversation class with semantic search
// over memory chunks in the Document class. This enables follow-up queries
// like "tell me more" to work by retrieving contextually relevant past turns.
//
// # Architecture
//
// The package uses a dual-class approach:
//   - Conversation class: Structured history with turn_number ordering
//   - Document class: Semantic memory with vector embeddings (version_tag="chat_memory")
//
// # Thread Safety
//
// All implementations are safe for concurrent use.
package conversation

import (
	"context"
)

// ConversationSearcher defines the interface for retrieving relevant conversation history.
//
// # Description
//
// ConversationSearcher provides methods to search conversation history using both
// recency-based and semantic search strategies. Implementations combine results
// from both approaches to provide robust context retrieval.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example
//
//	searcher := NewWeaviateConversationSearcher(client, embedder)
//	history, err := searcher.GetHybridContext(ctx, "sess_123", "tell me more", 25)
//	if err != nil {
//	    // Handle error - consider graceful degradation
//	}
//	// history contains merged recent + semantically relevant turns
type ConversationSearcher interface {
	// SearchRelevant retrieves semantically similar past turns from Document class.
	//
	// # Description
	//
	// Embeds the query and searches the Document class for memory chunks
	// (version_tag="chat_memory") that are semantically similar. Results are
	// filtered by session (data_space) and recency (turn_number > currentTurn - maxTurnAge).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - sessionID: The session to search within (maps to data_space field).
	//   - query: The current user query to find relevant history for.
	//   - currentTurnNumber: The current turn number for recency filtering.
	//   - topK: Maximum number of relevant turns to return.
	//
	// # Outputs
	//
	//   - []RelevantTurn: Relevant past turns, ordered by similarity score.
	//   - error: Non-nil if search fails (embedding error, Weaviate error).
	//
	// # Example
	//
	//   turns, err := searcher.SearchRelevant(ctx, "sess_123", "tell me more about Chrysler", 25, 3)
	//   // Returns up to 3 turns about Chrysler from the last 20 turns
	//
	// # Limitations
	//
	//   - Short queries like "tell me more" have weak semantic signal.
	//   - Returns empty slice if no relevant history found.
	//   - Only searches turns with turn_number > (currentTurnNumber - 20).
	//
	// # Assumptions
	//
	//   - Session has at least one prior turn stored in Document class.
	//   - Embedding dimensions match stored vectors.
	//   - Document class has version_tag, data_space, and turn_number properties indexed.
	SearchRelevant(ctx context.Context, sessionID string, query string, currentTurnNumber int, topK int) ([]RelevantTurn, error)

	// GetRecent retrieves the N most recent turns from Conversation class.
	//
	// # Description
	//
	// Loads recent turns by turn_number ordering without semantic search.
	// Used to ensure immediate context is always available regardless of
	// semantic relevance.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - sessionID: The session to load from.
	//   - n: Number of recent turns to retrieve.
	//
	// # Outputs
	//
	//   - []RelevantTurn: Recent turns, newest first (highest turn_number first).
	//   - error: Non-nil if load fails.
	//
	// # Example
	//
	//   recent, err := searcher.GetRecent(ctx, "sess_123", 2)
	//   // Returns last 2 turns for immediate context
	//
	// # Limitations
	//
	//   - Returns fewer than N if session has fewer turns.
	//   - Does not include similarity scores (set to 0.0).
	//
	// # Assumptions
	//
	//   - Session exists in Weaviate Conversation class.
	//   - turn_number field is properly indexed for ordering.
	GetRecent(ctx context.Context, sessionID string, n int) ([]RelevantTurn, error)

	// GetHybridContext retrieves conversation context using hybrid retrieval.
	//
	// # Description
	//
	// Combines GetRecent (last 2 turns) and SearchRelevant (top 3 by similarity)
	// to provide robust context retrieval. Results are merged, deduplicated by
	// turn_number, and limited to a maximum of 5 turns.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - sessionID: The session to search within.
	//   - query: The current user query.
	//   - currentTurnNumber: The current turn number for recency filtering.
	//
	// # Outputs
	//
	//   - []RelevantTurn: Merged context, ordered by turn_number descending.
	//   - error: Non-nil if both retrieval methods fail.
	//
	// # Example
	//
	//   context, err := searcher.GetHybridContext(ctx, "sess_123", "tell me more", 25)
	//   // Returns up to 5 turns: recent + semantically relevant, deduplicated
	//
	// # Limitations
	//
	//   - If semantic search fails, falls back to recent-only (graceful degradation).
	//   - Maximum of 5 turns returned regardless of input parameters.
	//
	// # Assumptions
	//
	//   - At least one of GetRecent or SearchRelevant will succeed.
	//   - Session has been initialized and has at least one turn.
	GetHybridContext(ctx context.Context, sessionID string, query string, currentTurnNumber int) ([]RelevantTurn, error)
}

// EmbeddingProvider defines the interface for computing text embeddings.
//
// # Description
//
// EmbeddingProvider wraps calls to the embedding model (Ollama, OpenAI, etc.)
// to convert text into vector representations for semantic search. This
// abstraction allows for easy mocking in tests and swapping embedding backends.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example
//
//	embedder := NewOllamaEmbedder(embeddingURL, modelName)
//	vector, err := embedder.Embed(ctx, "What is Chrysler?")
//	if err != nil {
//	    // Handle embedding failure
//	}
//	// vector is []float32 with dimension matching the model
type EmbeddingProvider interface {
	// Embed computes a vector embedding for the given text.
	//
	// # Description
	//
	// Calls the configured embedding service to convert text into a dense
	// vector representation suitable for semantic similarity search.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - text: The text to embed. May be truncated if exceeding model limits.
	//
	// # Outputs
	//
	//   - []float32: The embedding vector with dimension matching the model.
	//   - error: Non-nil if embedding fails (network error, model error).
	//
	// # Example
	//
	//   vector, err := embedder.Embed(ctx, "Tell me about automotive history")
	//   // vector is []float32{0.023, -0.156, 0.089, ...}
	//
	// # Limitations
	//
	//   - Text may be truncated if exceeding model's max input tokens.
	//   - Network latency depends on embedding service location.
	//   - Vector dimension is fixed per model (e.g., 768 for nomic-embed-text).
	//
	// # Assumptions
	//
	//   - Embedding service is available and responding.
	//   - Text is valid UTF-8.
	Embed(ctx context.Context, text string) ([]float32, error)
}
