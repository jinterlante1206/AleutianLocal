package conversation

import (
	"os"
	"strconv"
)

// RelevantTurn represents a conversation turn retrieved from memory search.
//
// # Description
//
// RelevantTurn contains the question and answer from a past conversation turn,
// along with metadata for ordering and relevance scoring. This struct is
// serialized to JSON when passing history to the Python RAG engine.
//
// # JSON Serialization
//
// The struct uses json tags that match the Python RelevantHistoryItem model:
//
//	{
//	    "question": "What is Chrysler?",
//	    "answer": "Chrysler is an American automotive company...",
//	    "turn_number": 5,
//	    "similarity_score": 0.87
//	}
//
// # Thread Safety
//
// RelevantTurn is a value type and safe for concurrent read access.
// Modifications should be done on copies.
type RelevantTurn struct {
	// Question is the user's original query for this turn.
	Question string `json:"question"`

	// Answer is the AI's response for this turn.
	Answer string `json:"answer"`

	// TurnNumber is the sequential turn number within the session.
	// Used for ordering and deduplication in hybrid retrieval.
	// A value of 0 indicates the turn number is unknown (legacy data).
	TurnNumber int `json:"turn_number"`

	// SimilarityScore is the cosine similarity score from vector search.
	// Range: 0.0 to 1.0, where 1.0 is identical.
	// Set to 0.0 for turns retrieved by recency (not semantic search).
	SimilarityScore float64 `json:"similarity_score"`
}

// SearchConfig holds configuration for conversation memory search.
//
// # Description
//
// SearchConfig allows customization of the hybrid retrieval parameters.
// Default values are provided by DefaultSearchConfig().
//
// # Example
//
//	config := DefaultSearchConfig()
//	config.SemanticTopK = 5  // Increase semantic results
//	searcher := NewWeaviateConversationSearcher(client, embedder, config)
type SearchConfig struct {
	// RecentTurns is the number of recent turns to always include.
	// These provide immediate context regardless of semantic relevance.
	// Default: 2
	RecentTurns int

	// SemanticTopK is the maximum number of semantically relevant turns to retrieve.
	// Default: 3
	SemanticTopK int

	// MaxTurnAge is how far back (in turns) to search for semantic matches.
	// Turns older than (currentTurn - MaxTurnAge) are excluded from semantic search.
	// Default: 20
	MaxTurnAge int

	// MaxTotalTurns is the maximum number of turns to return after merging.
	// Default: 5
	MaxTotalTurns int

	// MaxEmbedLength is the maximum characters to embed for search queries.
	// Longer text is truncated. Default: 2000
	MaxEmbedLength int

	// MemoryVersionTag is the version_tag value that identifies chat memory chunks.
	// Default: "chat_memory"
	MemoryVersionTag string
}

// DefaultSearchConfig returns the default search configuration.
//
// # Description
//
// Returns a SearchConfig with sensible defaults for hybrid retrieval.
// Values can be overridden via environment variables:
//   - CONV_SEARCH_RECENT_TURNS (default: 2)
//   - CONV_SEARCH_SEMANTIC_TOPK (default: 3)
//   - CONV_SEARCH_MAX_TURN_AGE (default: 20)
//   - CONV_SEARCH_MAX_TOTAL_TURNS (default: 5)
//   - CONV_SEARCH_MAX_EMBED_LENGTH (default: 2000)
//   - CONV_SEARCH_MEMORY_VERSION_TAG (default: "chat_memory")
//
// # Outputs
//
//   - SearchConfig: Configuration with default or env-configured values.
//
// # Example
//
//	config := DefaultSearchConfig()
//	// config.RecentTurns == 2 (or CONV_SEARCH_RECENT_TURNS if set)
func DefaultSearchConfig() SearchConfig {
	return SearchConfig{
		RecentTurns:      getEnvInt("CONV_SEARCH_RECENT_TURNS", 2),
		SemanticTopK:     getEnvInt("CONV_SEARCH_SEMANTIC_TOPK", 3),
		MaxTurnAge:       getEnvInt("CONV_SEARCH_MAX_TURN_AGE", 20),
		MaxTotalTurns:    getEnvInt("CONV_SEARCH_MAX_TOTAL_TURNS", 5),
		MaxEmbedLength:   getEnvInt("CONV_SEARCH_MAX_EMBED_LENGTH", 2000),
		MemoryVersionTag: getEnvString("CONV_SEARCH_MEMORY_VERSION_TAG", "chat_memory"),
	}
}

// getEnvInt returns an environment variable as int, or defaultVal if not set/invalid.
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// getEnvString returns an environment variable as string, or defaultVal if not set.
func getEnvString(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// SearchResult contains the full result of a hybrid context search.
//
// # Description
//
// SearchResult provides detailed information about the search operation,
// including which retrieval methods succeeded and any errors encountered.
// This is useful for observability and debugging.
//
// # Example
//
//	result := searcher.SearchWithDetails(ctx, sessionID, query, turnNum)
//	if result.SemanticError != nil {
//	    log.Warn("Semantic search failed, using recent-only", "error", result.SemanticError)
//	}
//	// result.Turns contains the merged results
type SearchResult struct {
	// Turns is the final merged and deduplicated list of relevant turns.
	Turns []RelevantTurn

	// RecentCount is the number of turns retrieved by recency.
	RecentCount int

	// SemanticCount is the number of turns retrieved by semantic search.
	SemanticCount int

	// RecentError is set if recent turn retrieval failed.
	RecentError error

	// SemanticError is set if semantic search failed.
	// The search may still return results from recent-only retrieval.
	SemanticError error
}
