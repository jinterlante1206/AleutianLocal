// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package conversation

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("aleutian.orchestrator.conversation")

// WeaviateConversationSearcher implements ConversationSearcher using Weaviate.
//
// # Description
//
// WeaviateConversationSearcher provides hybrid retrieval of conversation history
// by combining recent turns from the Conversation class with semantic search
// over memory chunks in the Document class.
//
// # Thread Safety
//
// WeaviateConversationSearcher is safe for concurrent use. The underlying Weaviate
// client handles connection pooling.
//
// # Example
//
//	embedder := NewDatatypesEmbedder()
//	searcher := NewWeaviateConversationSearcher(client, embedder, DefaultSearchConfig())
//	history, err := searcher.GetHybridContext(ctx, "sess_123", "tell me more", 25)
type WeaviateConversationSearcher struct {
	client   *weaviate.Client
	embedder EmbeddingProvider
	config   SearchConfig
}

// NewWeaviateConversationSearcher creates a new conversation searcher.
//
// # Description
//
// Creates a WeaviateConversationSearcher with the given Weaviate client,
// embedding provider, and search configuration. Config values are validated
// and corrected if necessary.
//
// # Inputs
//
//   - client: Weaviate client for database access.
//   - embedder: Provider for computing text embeddings.
//   - config: Search configuration (use DefaultSearchConfig() for defaults).
//
// # Outputs
//
//   - *WeaviateConversationSearcher: Ready to use searcher instance.
//
// # Example
//
//	embedder := NewDatatypesEmbedder()
//	config := DefaultSearchConfig()
//	config.SemanticTopK = 5  // Increase semantic results
//	searcher := NewWeaviateConversationSearcher(client, embedder, config)
//
// # Assumptions
//
//   - Client is connected and authenticated to Weaviate.
//   - Embedder is configured and accessible.
func NewWeaviateConversationSearcher(client *weaviate.Client, embedder EmbeddingProvider, config SearchConfig) *WeaviateConversationSearcher {
	// Validate and correct config values
	validatedConfig := validateSearchConfig(config)

	return &WeaviateConversationSearcher{
		client:   client,
		embedder: embedder,
		config:   validatedConfig,
	}
}

// validateSearchConfig validates and corrects search configuration values.
// Logs warnings for invalid values and applies sensible defaults.
func validateSearchConfig(config SearchConfig) SearchConfig {
	defaults := DefaultSearchConfig()

	if config.RecentTurns < 0 {
		slog.Warn("Invalid RecentTurns config, using default",
			"provided", config.RecentTurns, "default", defaults.RecentTurns)
		config.RecentTurns = defaults.RecentTurns
	}

	if config.SemanticTopK < 0 {
		slog.Warn("Invalid SemanticTopK config, using default",
			"provided", config.SemanticTopK, "default", defaults.SemanticTopK)
		config.SemanticTopK = defaults.SemanticTopK
	}

	if config.MaxTurnAge < 1 {
		slog.Warn("Invalid MaxTurnAge config, using default",
			"provided", config.MaxTurnAge, "default", defaults.MaxTurnAge)
		config.MaxTurnAge = defaults.MaxTurnAge
	}

	if config.MaxTotalTurns < 1 {
		slog.Warn("Invalid MaxTotalTurns config, using default",
			"provided", config.MaxTotalTurns, "default", defaults.MaxTotalTurns)
		config.MaxTotalTurns = defaults.MaxTotalTurns
	}

	if config.MaxEmbedLength < 1 {
		slog.Warn("Invalid MaxEmbedLength config, using default",
			"provided", config.MaxEmbedLength, "default", defaults.MaxEmbedLength)
		config.MaxEmbedLength = defaults.MaxEmbedLength
	}

	if config.MemoryVersionTag == "" {
		slog.Warn("Empty MemoryVersionTag config, using default",
			"default", defaults.MemoryVersionTag)
		config.MemoryVersionTag = defaults.MemoryVersionTag
	}

	return config
}

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
//   - []RelevantTurn: Relevant past turns, ordered by similarity score (highest first).
//   - error: Non-nil if search fails (embedding error, Weaviate error).
//
// # Example
//
//	turns, err := searcher.SearchRelevant(ctx, "sess_123", "tell me more about Chrysler", 25, 3)
//
// # Limitations
//
//   - Short queries like "tell me more" have weak semantic signal.
//   - Returns empty slice if no relevant history found.
//   - Only searches turns with turn_number > (currentTurnNumber - maxTurnAge).
//
// # Assumptions
//
//   - Session has at least one prior turn stored in Document class.
//   - Embedding dimensions match stored vectors.
//   - Document class has version_tag, data_space, and turn_number properties indexed.
func (s *WeaviateConversationSearcher) SearchRelevant(ctx context.Context, sessionID string, query string, currentTurnNumber int, topK int) ([]RelevantTurn, error) {
	ctx, span := tracer.Start(ctx, "SearchRelevant")
	defer span.End()

	slog.Debug("Searching for relevant conversation history",
		"sessionID", sessionID,
		"currentTurn", currentTurnNumber,
		"topK", topK)

	// 1. Truncate query if needed
	truncatedQuery := query
	if len(query) > s.config.MaxEmbedLength {
		truncatedQuery = query[:s.config.MaxEmbedLength]
		slog.Debug("Truncated query for embedding", "originalLen", len(query), "truncatedLen", len(truncatedQuery))
	}

	// 2. Get embedding for the query
	vector, err := s.embedder.Embed(ctx, truncatedQuery)
	if err != nil {
		slog.Error("Failed to embed query for conversation search", "error", err)
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// 3. Build the filter
	// Filter: data_space == sessionID AND version_tag == "chat_memory" AND turn_number > minTurnNumber
	minTurnNumber := currentTurnNumber - s.config.MaxTurnAge
	if minTurnNumber < 0 {
		minTurnNumber = 0
	}

	dataSpaceFilter := filters.Where().
		WithPath([]string{"data_space"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	versionTagFilter := filters.Where().
		WithPath([]string{"version_tag"}).
		WithOperator(filters.Equal).
		WithValueString(s.config.MemoryVersionTag)

	turnNumberFilter := filters.Where().
		WithPath([]string{"turn_number"}).
		WithOperator(filters.GreaterThan).
		WithValueInt(int64(minTurnNumber))

	// Combine filters with AND
	combinedFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{dataSpaceFilter, versionTagFilter, turnNumberFilter})

	// 4. Build the NearVector search
	nearVector := s.client.GraphQL().NearVectorArgBuilder().
		WithVector(vector)

	// 5. Define fields to retrieve
	// Note: We request certainty (always [0,1]) instead of distance which varies by metric
	fields := []graphql.Field{
		{Name: "content"},
		{Name: "source"},
		{Name: "turn_number"},
		{Name: "_additional", Fields: []graphql.Field{
			{Name: "certainty"},
		}},
	}

	// 6. Execute the search
	result, err := s.client.GraphQL().Get().
		WithClassName("Document").
		WithFields(fields...).
		WithWhere(combinedFilter).
		WithNearVector(nearVector).
		WithLimit(topK).
		Do(ctx)

	if err != nil {
		slog.Error("Failed to search Document class for conversation memory", "error", err)
		return nil, fmt.Errorf("weaviate search failed: %w", err)
	}

	// 7. Parse the results using typed parser
	parsed, err := datatypes.ParseGraphQLResponse[datatypes.DocumentQueryResponse](result)
	if err != nil {
		slog.Error("Failed to parse search results", "error", err)
		return nil, fmt.Errorf("failed to parse results: %w", err)
	}

	turns := parseDocumentSearchResults(parsed)
	slog.Debug("Found relevant conversation turns", "count", len(turns))
	return turns, nil
}

// GetRecent retrieves the N most recent turns from Conversation class.
//
// # Description
//
// Loads recent turns by timestamp ordering without semantic search.
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
//   - []RelevantTurn: Recent turns, newest first (highest timestamp first).
//   - error: Non-nil if load fails.
//
// # Example
//
//	recent, err := searcher.GetRecent(ctx, "sess_123", 2)
//
// # Limitations
//
//   - Returns fewer than N if session has fewer turns.
//   - Does not include similarity scores (set to 0.0).
//
// # Assumptions
//
//   - Session exists in Weaviate Conversation class.
//   - timestamp field is properly indexed for ordering.
func (s *WeaviateConversationSearcher) GetRecent(ctx context.Context, sessionID string, n int) ([]RelevantTurn, error) {
	ctx, span := tracer.Start(ctx, "GetRecent")
	defer span.End()

	slog.Debug("Retrieving recent conversation turns", "sessionID", sessionID, "count", n)

	// 1. Build the filter
	whereFilter := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	// 2. Sort by timestamp descending (newest first)
	sortBy := graphql.Sort{
		Path:  []string{"timestamp"},
		Order: graphql.Desc,
	}

	// 3. Define fields to retrieve
	fields := []graphql.Field{
		{Name: "question"},
		{Name: "answer"},
		{Name: "timestamp"},
		{Name: "turn_number"},
	}

	// 4. Execute the query
	result, err := s.client.GraphQL().Get().
		WithClassName("Conversation").
		WithFields(fields...).
		WithWhere(whereFilter).
		WithSort(sortBy).
		WithLimit(n).
		Do(ctx)

	if err != nil {
		slog.Error("Failed to retrieve recent conversations", "error", err)
		return nil, fmt.Errorf("weaviate query failed: %w", err)
	}

	// 5. Parse the results using typed parser
	parsed, err := datatypes.ParseGraphQLResponse[datatypes.ConversationQueryResponse](result)
	if err != nil {
		slog.Error("Failed to parse conversation results", "error", err)
		return nil, fmt.Errorf("failed to parse results: %w", err)
	}

	turns := parseConversationResults(parsed)
	slog.Debug("Retrieved recent conversation turns", "count", len(turns))
	return turns, nil
}

// GetHybridContext retrieves conversation context using hybrid retrieval.
//
// # Description
//
// Combines GetRecent (last 2 turns) and SearchRelevant (top 3 by similarity)
// to provide robust context retrieval. Results are merged, deduplicated by
// question content, and limited to a maximum of 5 turns.
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
//   - []RelevantTurn: Merged context, with recent turns first, then semantic results.
//   - error: Non-nil if both retrieval methods fail.
//
// # Example
//
//	context, err := searcher.GetHybridContext(ctx, "sess_123", "tell me more", 25)
//
// # Limitations
//
//   - If semantic search fails, falls back to recent-only (graceful degradation).
//   - Maximum of MaxTotalTurns returned regardless of input parameters.
//
// # Assumptions
//
//   - At least one of GetRecent or SearchRelevant will succeed.
//   - Session has been initialized and has at least one turn.
func (s *WeaviateConversationSearcher) GetHybridContext(ctx context.Context, sessionID string, query string, currentTurnNumber int) ([]RelevantTurn, error) {
	ctx, span := tracer.Start(ctx, "GetHybridContext")
	defer span.End()

	slog.Info("Getting hybrid conversation context", "sessionID", sessionID, "currentTurn", currentTurnNumber)

	var recentTurns []RelevantTurn
	var semanticTurns []RelevantTurn
	var recentErr, semanticErr error

	// 1. Get recent turns (always attempted)
	recentTurns, recentErr = s.GetRecent(ctx, sessionID, s.config.RecentTurns)
	if recentErr != nil {
		slog.Warn("Failed to get recent turns, continuing with semantic-only", "error", recentErr)
	}

	// 2. Get semantically relevant turns
	semanticTurns, semanticErr = s.SearchRelevant(ctx, sessionID, query, currentTurnNumber, s.config.SemanticTopK)
	if semanticErr != nil {
		slog.Warn("Failed to search semantic memory, continuing with recent-only", "error", semanticErr)
	}

	// 3. Check if both failed
	if recentErr != nil && semanticErr != nil {
		return nil, fmt.Errorf("both recent and semantic retrieval failed: recent=[%v], semantic=[%v]", recentErr, semanticErr)
	}

	// 4. Merge and deduplicate
	merged := mergeAndDeduplicate(recentTurns, semanticTurns, s.config.MaxTotalTurns)

	slog.Info("Hybrid context retrieved",
		"recentCount", len(recentTurns),
		"semanticCount", len(semanticTurns),
		"mergedCount", len(merged))

	return merged, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// parseDocumentSearchResults converts DocumentQueryResponse to RelevantTurn slices.
//
// # Description
//
// Extracts question/answer from content field (format: "User: Q\nAI: A")
// and uses certainty as similarity score.
//
// # Inputs
//
//   - resp: Typed response from ParseGraphQLResponse[DocumentQueryResponse].
//
// # Outputs
//
//   - []RelevantTurn: Parsed turns with similarity scores.
//
// # Assumptions
//
//   - Content format is "User: <question>\nAI: <answer>".
//   - Certainty is used directly as similarity (always [0, 1]).
func parseDocumentSearchResults(resp *datatypes.DocumentQueryResponse) []RelevantTurn {
	if resp == nil {
		return []RelevantTurn{}
	}

	turns := make([]RelevantTurn, 0, len(resp.Get.Document))
	for _, doc := range resp.Get.Document {
		// Parse question and answer from content
		question, answer := parseMemoryContent(doc.Content)

		// Use certainty directly as similarity score (always [0, 1])
		var similarity float64
		if doc.Additional.Certainty != nil {
			similarity = float64(*doc.Additional.Certainty)
		}

		turnNum := 0
		if doc.TurnNumber != nil {
			turnNum = *doc.TurnNumber
		}

		turns = append(turns, RelevantTurn{
			Question:        question,
			Answer:          answer,
			TurnNumber:      turnNum,
			SimilarityScore: similarity,
		})
	}

	return turns
}

// parseConversationResults converts ConversationQueryResponse to RelevantTurn slices.
//
// # Description
//
// Extracts turn data from Conversation class query results. Uses actual turn_number
// from the database for accurate deduplication with Document-based semantic results.
//
// # Inputs
//
//   - resp: Typed response from ParseGraphQLResponse[ConversationQueryResponse].
//
// # Outputs
//
//   - []RelevantTurn: Parsed turns without similarity scores (set to 0.0).
//
// # Assumptions
//
//   - Results are ordered by timestamp descending.
//   - TurnNumber uses actual value from database (0 if not set).
func parseConversationResults(resp *datatypes.ConversationQueryResponse) []RelevantTurn {
	if resp == nil {
		return []RelevantTurn{}
	}

	turns := make([]RelevantTurn, 0, len(resp.Get.Conversation))
	for _, conv := range resp.Get.Conversation {
		turnNum := 0
		if conv.TurnNumber != nil {
			turnNum = *conv.TurnNumber
		}

		turns = append(turns, RelevantTurn{
			Question:        conv.Question,
			Answer:          conv.Answer,
			TurnNumber:      turnNum,
			SimilarityScore: 0.0, // No similarity for recency-based retrieval
		})
	}

	return turns
}

// parseMemoryContent extracts question and answer from memory chunk content.
//
// # Description
//
// Parses the combined format "User: <question>\nAI: <answer>" back into
// separate question and answer strings.
//
// # Inputs
//
//   - content: The raw content string from Document class.
//
// # Outputs
//
//   - question: The extracted user question.
//   - answer: The extracted AI answer.
//
// # Limitations
//
//   - Assumes exact format "User: Q\nAI: A".
//   - Returns content as-is if format doesn't match.
func parseMemoryContent(content string) (question, answer string) {
	// Format is "User: <question>\nAI: <answer>"
	const userPrefix = "User: "
	const aiPrefix = "\nAI: "

	// Find the AI prefix position
	aiStart := len(content)
	for i := 0; i <= len(content)-len(aiPrefix); i++ {
		if content[i:i+len(aiPrefix)] == aiPrefix {
			aiStart = i
			break
		}
	}

	// Extract question (after "User: " up to "\nAI: ")
	if len(content) > len(userPrefix) && content[:len(userPrefix)] == userPrefix {
		question = content[len(userPrefix):aiStart]
	} else {
		question = content[:aiStart]
	}

	// Extract answer (after "\nAI: " to end)
	if aiStart+len(aiPrefix) < len(content) {
		answer = content[aiStart+len(aiPrefix):]
	}

	return question, answer
}

// mergeAndDeduplicate combines recent and semantic turns, removing duplicates.
//
// # Description
//
// Merges two slices of turns, deduplicates by normalized question content,
// and limits the result to maxTurns. Recent turns are prioritized and appear first.
// Question normalization lowercases and strips punctuation for better matching.
//
// # Inputs
//
//   - recent: Turns from recency-based retrieval.
//   - semantic: Turns from semantic search.
//   - maxTurns: Maximum number of turns to return.
//
// # Outputs
//
//   - []RelevantTurn: Merged and deduplicated turns, limited to maxTurns.
//
// # Algorithm
//
//  1. Add all recent turns to result, tracking seen questions (normalized).
//  2. Add semantic turns that have unique questions.
//  3. Sort by turn number descending (newest first).
//  4. Limit to maxTurns.
func mergeAndDeduplicate(recent, semantic []RelevantTurn, maxTurns int) []RelevantTurn {
	seen := make(map[string]bool)
	result := make([]RelevantTurn, 0, len(recent)+len(semantic))

	// Add recent turns first (they take priority)
	for _, turn := range recent {
		normalized := normalizeQuestion(turn.Question)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, turn)
		}
	}

	// Add semantic turns that aren't duplicates
	for _, turn := range semantic {
		normalized := normalizeQuestion(turn.Question)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, turn)
		}
	}

	// Sort by turn number descending (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].TurnNumber > result[j].TurnNumber
	})

	// Limit to maxTurns
	if len(result) > maxTurns {
		result = result[:maxTurns]
	}

	return result
}

// normalizeQuestion normalizes a question for deduplication comparison.
// Lowercases and strips common punctuation to match variations like:
// "What is X?" vs "what is x" vs "What is X"
func normalizeQuestion(q string) string {
	// Lowercase
	q = strings.ToLower(q)

	// Strip common trailing/leading punctuation
	q = strings.TrimSpace(q)
	q = strings.TrimRight(q, "?!.,;:")
	q = strings.TrimSpace(q)

	return q
}

// =============================================================================
// DatatypesEmbedder - Wrapper for existing EmbeddingResponse.Get()
// =============================================================================

// DatatypesEmbedder wraps datatypes.EmbeddingResponse.Get() to implement EmbeddingProvider.
//
// # Description
//
// This adapter allows reuse of the existing embedding infrastructure without
// modification. It calls the EmbeddingResponse.Get() method and extracts the
// resulting vector.
//
// # Thread Safety
//
// DatatypesEmbedder is safe for concurrent use. Each call creates a new
// EmbeddingResponse instance.
//
// # Example
//
//	embedder := NewDatatypesEmbedder()
//	vector, err := embedder.Embed(ctx, "What is Chrysler?")
type DatatypesEmbedder struct{}

// NewDatatypesEmbedder creates a new embedding provider using datatypes.EmbeddingResponse.
//
// # Description
//
// Creates an adapter that wraps the existing EmbeddingResponse.Get() method
// to implement the EmbeddingProvider interface.
//
// # Outputs
//
//   - *DatatypesEmbedder: Ready to use embedder.
//
// # Example
//
//	embedder := NewDatatypesEmbedder()
//	searcher := NewWeaviateConversationSearcher(client, embedder, DefaultSearchConfig())
func NewDatatypesEmbedder() *DatatypesEmbedder {
	return &DatatypesEmbedder{}
}

// Embed computes a vector embedding for the given text.
//
// # Description
//
// Calls datatypes.EmbeddingResponse.GetWithContext() to compute the embedding using
// the configured embedding service (via EMBEDDING_SERVICE_URL env var).
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout. If canceled, returns immediately.
//   - text: The text to embed.
//
// # Outputs
//
//   - []float32: The embedding vector.
//   - error: Non-nil if embedding fails or context is canceled.
//
// # Limitations
//
//   - Requires EMBEDDING_SERVICE_URL environment variable.
//
// # Assumptions
//
//   - Embedding service is available and responding.
//   - EMBEDDING_SERVICE_URL is set.
func (e *DatatypesEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	var embResp datatypes.EmbeddingResponse
	if err := embResp.GetWithContext(ctx, text); err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	return embResp.Vector, nil
}
