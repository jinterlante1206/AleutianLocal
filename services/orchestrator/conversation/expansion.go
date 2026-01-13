package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Interfaces
// =============================================================================

// QueryExpander defines the interface for expanding ambiguous queries using conversation history.
//
// # Description
//
// QueryExpander detects when a user query is ambiguous (e.g., "tell me more", "he is bad")
// and rewrites it using conversation history to create explicit, searchable queries.
// This enables follow-up queries to find relevant documents even when the original
// query lacks sufficient semantic signal.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example
//
//	expander := NewLLMQueryExpander(llmClient, DefaultExpansionConfig())
//	if expander.NeedsExpansion("tell me more") {
//	    expanded, err := expander.Expand(ctx, "tell me more", history)
//	    if err != nil {
//	        // Fall back to original query
//	    }
//	    // Use expanded.Queries for multi-query search
//	}
type QueryExpander interface {
	// NeedsExpansion returns true if the query appears ambiguous and would benefit from expansion.
	//
	// # Description
	//
	// Uses heuristics to detect ambiguous queries:
	//   - Short queries (< 20 chars)
	//   - Pronoun references (he, she, it, they)
	//   - Contextual references (this, that, more, it)
	//
	// Also detects when expansion should be SKIPPED:
	//   - Commands (stop, clear, help, reset, quit, exit)
	//   - Topic switches ("switching gears", "different topic")
	//
	// # Inputs
	//
	//   - query: The user's current query.
	//
	// # Outputs
	//
	//   - bool: True if query should be expanded, false if it should be used as-is.
	//
	// # Example
	//
	//   expander.NeedsExpansion("tell me more")           // true
	//   expander.NeedsExpansion("when did he start")      // true
	//   expander.NeedsExpansion("What is the capital?")   // false (complete question)
	//   expander.NeedsExpansion("help")                   // false (command)
	//   expander.NeedsExpansion("switching gears, ...")   // false (topic switch)
	//
	// # Limitations
	//
	//   - Heuristics may have false positives/negatives.
	//   - Does not use embeddings for similarity check (see NeedsExpansionWithContext).
	//
	// # Assumptions
	//
	//   - Query is valid UTF-8 text.
	NeedsExpansion(query string) bool

	// NeedsExpansionWithContext performs advanced expansion detection with similarity check.
	//
	// # Description
	//
	// In addition to basic heuristics, checks if the current query is too similar
	// to the previous query (>0.85 similarity), which indicates a rephrasing rather
	// than a follow-up. Rephrased queries should not be expanded.
	//
	// # Inputs
	//
	//   - query: The user's current query.
	//   - prevQueryVector: Embedding of the previous query (nil if no previous query).
	//   - currentQueryVector: Embedding of the current query.
	//
	// # Outputs
	//
	//   - bool: True if query should be expanded.
	//   - bool: True if topic switch was detected (context should be dropped).
	//
	// # Example
	//
	//   needsExpand, isTopicSwitch := expander.NeedsExpansionWithContext(
	//       "tell me more",
	//       prevVector,
	//       currVector,
	//   )
	//   if isTopicSwitch {
	//       // Drop conversation context entirely
	//   }
	//
	// # Limitations
	//
	//   - Requires embedding computation for similarity check.
	//
	// # Assumptions
	//
	//   - Vectors have matching dimensions.
	NeedsExpansionWithContext(query string, prevQueryVector, currentQueryVector []float32) (needsExpansion bool, isTopicSwitch bool)

	// Expand rewrites an ambiguous query into multiple explicit search queries.
	//
	// # Description
	//
	// Uses an LLM to generate 3 query variations based on conversation history:
	//   1. SPECIFIC: A precise, narrow query focusing on the main entity
	//   2. BROAD: A wider query capturing related concepts
	//   3. CONTEXTUAL: A query emphasizing the relationship/context from history
	//
	// This multi-query approach provides redundancy against bad expansions.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - query: The ambiguous query to expand.
	//   - history: Recent conversation turns providing context.
	//
	// # Outputs
	//
	//   - *ExpandedQuery: Contains original query and 3 variations.
	//   - error: Non-nil if LLM call fails (caller should fall back to original).
	//
	// # Example
	//
	//   expanded, err := expander.Expand(ctx, "tell me more", []RelevantTurn{
	//       {Question: "What is Motown?", Answer: "Motown was founded..."},
	//   })
	//   if err != nil {
	//       // Use original query
	//   }
	//   // expanded.Queries = ["History of Motown Records...", "Motown artists...", ...]
	//
	// # Limitations
	//
	//   - LLM may hallucinate incorrect expansions.
	//   - Latency: ~300-500ms depending on model.
	//   - Empty history returns original query unchanged.
	//
	// # Assumptions
	//
	//   - LLM client is configured and available.
	//   - History contains relevant context for expansion.
	Expand(ctx context.Context, query string, history []RelevantTurn) (*ExpandedQuery, error)

	// DetectsTopicSwitch returns true if the query contains explicit topic switch phrases.
	//
	// # Description
	//
	// Checks for phrases that indicate the user is intentionally changing topics,
	// such as "switching gears", "different topic", "unrelated", etc.
	// When detected, conversation context should be dropped entirely.
	//
	// # Inputs
	//
	//   - query: The user's query to check.
	//
	// # Outputs
	//
	//   - bool: True if topic switch detected.
	//
	// # Example
	//
	//   expander.DetectsTopicSwitch("Switching gears, what about X?")  // true
	//   expander.DetectsTopicSwitch("Tell me more")                    // false
	DetectsTopicSwitch(query string) bool
}

// LLMClient defines the interface for making LLM generation calls.
//
// # Description
//
// LLMClient abstracts the LLM backend (Ollama, OpenAI, etc.) for query expansion
// and context summarization. This allows mocking in tests and swapping backends.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
// GenerateFunc is a function type for LLM text generation.
//
// # Description
//
// Using a function type instead of an interface allows callers to pass
// a simple closure, eliminating the need for adapter structs when the
// underlying LLM client has a different signature.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - prompt: The prompt to send to the LLM.
//   - maxTokens: Maximum tokens in the response.
//
// # Outputs
//
//   - string: The generated text.
//   - error: Non-nil if generation fails.
//
// # Example
//
//	generateFunc := func(ctx context.Context, prompt string, maxTokens int) (string, error) {
//	    params := llm.GenerationParams{MaxTokens: &maxTokens}
//	    return ollamaClient.Generate(ctx, prompt, params)
//	}
//	expander := NewLLMQueryExpander(generateFunc, config)
type GenerateFunc func(ctx context.Context, prompt string, maxTokens int) (string, error)

// =============================================================================
// Types
// =============================================================================

// ExpandedQuery contains the result of query expansion.
//
// # Description
//
// ExpandedQuery holds both the original query and the expanded variations.
// The Queries slice contains 3 variations for multi-query search:
//   - Index 0: SPECIFIC query
//   - Index 1: BROAD query
//   - Index 2: CONTEXTUAL query
//
// # JSON Serialization
//
//	{
//	    "original": "tell me more",
//	    "queries": ["History of Motown...", "Motown artists...", "Berry Gordy..."],
//	    "expanded": true
//	}
type ExpandedQuery struct {
	// Original is the user's original query before expansion.
	Original string `json:"original"`

	// Queries contains the expanded query variations.
	// Length is 3 for successful expansion, 1 (original only) on failure.
	Queries []string `json:"queries"`

	// Expanded indicates whether expansion was performed.
	// False if expansion was skipped or failed.
	Expanded bool `json:"expanded"`
}

// ExpansionConfig holds configuration for query expansion.
//
// # Description
//
// ExpansionConfig allows customization of the expansion behavior.
// Default values are provided by DefaultExpansionConfig().
//
// # Example
//
//	config := DefaultExpansionConfig()
//	config.MaxTokens = 200  // Allow longer expansions
//	expander := NewLLMQueryExpander(client, config)
type ExpansionConfig struct {
	// Enabled controls whether expansion is active.
	// If false, NeedsExpansion always returns false.
	// Default: true (can be set via QUERY_EXPANSION_ENABLED)
	Enabled bool

	// Model is the LLM model to use for expansion.
	// If empty, defaults to OLLAMA_MODEL environment variable.
	// Default: "" (uses OLLAMA_MODEL)
	Model string

	// MaxTokens is the maximum tokens for the expansion response.
	// Default: 150
	MaxTokens int

	// TimeoutMs is the timeout in milliseconds for expansion LLM calls.
	// Default: 1500
	TimeoutMs int

	// Variations is the number of query variations to generate.
	// Default: 3
	Variations int

	// SimilarityThreshold is the cosine similarity above which
	// queries are considered too similar (rephrasing, not follow-up).
	// Default: 0.85
	SimilarityThreshold float64

	// MinQueryLength is the minimum query length to trigger expansion heuristics.
	// Queries shorter than this are considered potentially ambiguous.
	// Default: 20
	MinQueryLength int
}

// DefaultExpansionConfig returns the default expansion configuration.
//
// # Description
//
// Returns an ExpansionConfig with sensible defaults for multi-query expansion.
// Values can be overridden via environment variables:
//   - QUERY_EXPANSION_ENABLED (default: "true")
//   - QUERY_EXPANSION_MODEL (default: "" → uses OLLAMA_MODEL)
//   - QUERY_EXPANSION_MAX_TOKENS (default: 150)
//   - QUERY_EXPANSION_TIMEOUT_MS (default: 1500)
//   - QUERY_EXPANSION_VARIATIONS (default: 3)
//   - EXPANSION_SIMILARITY_THRESHOLD (default: 0.85)
//
// # Outputs
//
//   - ExpansionConfig: Configuration with default or env-configured values.
//
// # Example
//
//	config := DefaultExpansionConfig()
//	// config.Enabled == true (or QUERY_EXPANSION_ENABLED if set)
func DefaultExpansionConfig() ExpansionConfig {
	return ExpansionConfig{
		Enabled:             getEnvBool("QUERY_EXPANSION_ENABLED", true),
		Model:               getEnvString("QUERY_EXPANSION_MODEL", ""),
		MaxTokens:           getEnvInt("QUERY_EXPANSION_MAX_TOKENS", 150),
		TimeoutMs:           getEnvInt("QUERY_EXPANSION_TIMEOUT_MS", 1500),
		Variations:          getEnvInt("QUERY_EXPANSION_VARIATIONS", 3),
		SimilarityThreshold: getEnvFloat("EXPANSION_SIMILARITY_THRESHOLD", 0.85),
		MinQueryLength:      20,
	}
}

// getEnvBool returns an environment variable as bool, or defaultVal if not set/invalid.
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

// getEnvFloat returns an environment variable as float64, or defaultVal if not set/invalid.
func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}

// =============================================================================
// Implementation
// =============================================================================

// LLMQueryExpander implements QueryExpander using an LLM for query rewriting.
//
// # Description
//
// LLMQueryExpander uses configurable heuristics to detect ambiguous queries
// and an LLM to generate multiple search query variations. This enables
// "tell me more" and similar follow-up queries to find relevant documents.
//
// # Thread Safety
//
// LLMQueryExpander is safe for concurrent use.
//
// # Example
//
//	expander := NewLLMQueryExpander(llmClient, DefaultExpansionConfig())
//	expanded, err := expander.Expand(ctx, "tell me more", history)
type LLMQueryExpander struct {
	generate GenerateFunc
	config   ExpansionConfig
}

// NewLLMQueryExpander creates a new LLMQueryExpander with the given generate function and config.
//
// # Description
//
// Creates an expander that uses the provided generate function for query rewriting.
// Using a function instead of an interface allows callers to pass a simple closure.
//
// # Inputs
//
//   - client: The LLM client for generation calls.
//   - config: Configuration for expansion behavior.
//
// # Outputs
//
//   - *LLMQueryExpander: The configured expander.
//
// # Example
//
//	expander := NewLLMQueryExpander(ollamaClient, DefaultExpansionConfig())
//
// # Limitations
//
//   - Client must not be nil.
//
// # Assumptions
//
//   - LLM client is properly configured and ready.
func NewLLMQueryExpander(generate GenerateFunc, config ExpansionConfig) *LLMQueryExpander {
	return &LLMQueryExpander{
		generate: generate,
		config:   config,
	}
}

// pronounPatterns contains words that suggest a follow-up query.
var pronounPatterns = []string{
	"he", "she", "it", "they", "them", "his", "her", "its", "their",
	"this", "that", "these", "those",
	"more", "again", "also", "else",
}

// commandStopList contains queries that should never be expanded.
var commandStopList = []string{
	"stop", "clear", "help", "reset", "quit", "exit",
	"cancel", "undo", "back", "menu", "start over",
}

// topicSwitchPhrases contains phrases indicating intentional topic change.
var topicSwitchPhrases = []string{
	"switching gears", "different topic", "unrelated", "change of subject",
	"new question", "forget that", "moving on", "something else",
	"anyway", "by the way", "on another note", "separate question",
}

// NeedsExpansion returns true if the query appears ambiguous and would benefit from expansion.
//
// # Description
//
// Uses heuristics to detect ambiguous queries that would benefit from
// context-aware expansion. Returns false for commands, topic switches,
// and queries that appear complete.
//
// # Inputs
//
//   - query: The user's current query.
//
// # Outputs
//
//   - bool: True if query should be expanded.
//
// # Example
//
//   expander.NeedsExpansion("tell me more")  // true
//   expander.NeedsExpansion("help")          // false
//
// # Limitations
//
//   - Heuristics may have false positives/negatives.
//
// # Assumptions
//
//   - Query is valid UTF-8 text.
func (e *LLMQueryExpander) NeedsExpansion(query string) bool {
	if !e.config.Enabled {
		return false
	}

	lower := strings.ToLower(strings.TrimSpace(query))

	// Never expand commands
	if e.isCommand(lower) {
		return false
	}

	// Don't expand topic switches (context will be dropped anyway)
	if e.DetectsTopicSwitch(query) {
		return false
	}

	// Short queries are likely ambiguous
	if len(lower) < e.config.MinQueryLength {
		return true
	}

	// Check for pronouns and contextual references
	if e.containsPronouns(lower) {
		return true
	}

	return false
}

// NeedsExpansionWithContext performs advanced expansion detection with similarity check.
//
// # Description
//
// Extends NeedsExpansion with a similarity check to avoid expanding
// queries that are just rephrased versions of the previous query.
//
// # Inputs
//
//   - query: The user's current query.
//   - prevQueryVector: Embedding of the previous query (nil if none).
//   - currentQueryVector: Embedding of the current query.
//
// # Outputs
//
//   - needsExpansion: True if query should be expanded.
//   - isTopicSwitch: True if topic switch detected.
//
// # Example
//
//   needsExpand, isSwitch := expander.NeedsExpansionWithContext(query, prev, curr)
//
// # Limitations
//
//   - Requires pre-computed embeddings.
//
// # Assumptions
//
//   - Vectors have matching dimensions if both non-nil.
func (e *LLMQueryExpander) NeedsExpansionWithContext(query string, prevQueryVector, currentQueryVector []float32) (needsExpansion bool, isTopicSwitch bool) {
	// Check for topic switch first
	if e.DetectsTopicSwitch(query) {
		return false, true
	}

	// Basic heuristics check
	if !e.NeedsExpansion(query) {
		return false, false
	}

	// If we have both vectors, check similarity
	if prevQueryVector != nil && currentQueryVector != nil {
		similarity := cosineSimilarity(prevQueryVector, currentQueryVector)
		if similarity > e.config.SimilarityThreshold {
			// Too similar to previous query - this is a rephrasing, not a follow-up
			return false, false
		}
	}

	return true, false
}

// DetectsTopicSwitch returns true if the query contains topic switch phrases.
//
// # Description
//
// Checks for explicit phrases indicating the user wants to change topics.
// When detected, conversation history should be dropped.
//
// # Inputs
//
//   - query: The user's query to check.
//
// # Outputs
//
//   - bool: True if topic switch detected.
//
// # Example
//
//   expander.DetectsTopicSwitch("Switching gears, tell me about X")  // true
//
// # Limitations
//
//   - Only detects explicit phrases, not implicit topic changes.
//
// # Assumptions
//
//   - Query is valid UTF-8 text.
func (e *LLMQueryExpander) DetectsTopicSwitch(query string) bool {
	lower := strings.ToLower(query)
	for _, phrase := range topicSwitchPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// Expand rewrites an ambiguous query into multiple explicit search queries.
//
// # Description
//
// Uses the configured LLM to generate 3 query variations based on
// conversation history. Returns the original query if expansion fails.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - query: The ambiguous query to expand.
//   - history: Recent conversation turns for context.
//
// # Outputs
//
//   - *ExpandedQuery: Contains original and expanded queries.
//   - error: Non-nil if LLM call fails.
//
// # Example
//
//   expanded, err := expander.Expand(ctx, "tell me more", history)
//
// # Limitations
//
//   - LLM may hallucinate incorrect expansions.
//   - Latency: ~300-500ms.
//
// # Assumptions
//
//   - LLM client is available.
func (e *LLMQueryExpander) Expand(ctx context.Context, query string, history []RelevantTurn) (*ExpandedQuery, error) {
	// No history means nothing to expand with
	if len(history) == 0 {
		return &ExpandedQuery{
			Original: query,
			Queries:  []string{query},
			Expanded: false,
		}, nil
	}

	// Build the expansion prompt
	prompt := e.buildExpansionPrompt(query, history)

	// Apply timeout
	timeout := time.Duration(e.config.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Call LLM
	response, err := e.generate(ctx, prompt, e.config.MaxTokens)
	if err != nil {
		return nil, fmt.Errorf("expansion LLM call failed: %w", err)
	}

	// Parse the JSON response
	queries, err := e.parseExpansionResponse(response)
	if err != nil {
		// Fallback: try to extract any useful text
		return &ExpandedQuery{
			Original: query,
			Queries:  []string{query},
			Expanded: false,
		}, fmt.Errorf("failed to parse expansion response: %w", err)
	}

	return &ExpandedQuery{
		Original: query,
		Queries:  queries,
		Expanded: true,
	}, nil
}

// buildExpansionPrompt creates the prompt for multi-query expansion.
//
// # Description
//
// Constructs a prompt that instructs the LLM to generate 3 query variations
// based on the conversation history.
//
// # Inputs
//
//   - query: The query to expand.
//   - history: Conversation history for context.
//
// # Outputs
//
//   - string: The formatted prompt.
func (e *LLMQueryExpander) buildExpansionPrompt(query string, history []RelevantTurn) string {
	var historyText strings.Builder
	for _, turn := range history {
		historyText.WriteString(fmt.Sprintf("User: %s\nAssistant: %s\n\n", turn.Question, truncateString(turn.Answer, 300)))
	}

	return fmt.Sprintf(`You are a query rewriter. Given a conversation history and a follow-up query, generate THREE search query variations to maximize retrieval coverage.

Conversation History:
%s
Follow-up Query: %s

Generate exactly 3 query variations:
1. SPECIFIC: A precise, narrow query focusing on the main entity
2. BROAD: A wider query capturing related concepts
3. CONTEXTUAL: A query emphasizing the relationship/context from history

Format your response as JSON:
{"queries": ["specific query here", "broad query here", "contextual query here"]}`, historyText.String(), query)
}

// parseExpansionResponse extracts queries from the LLM's JSON response.
//
// # Description
//
// Parses the JSON response from the LLM and extracts the query variations.
//
// # Inputs
//
//   - response: The LLM's response text.
//
// # Outputs
//
//   - []string: The extracted queries.
//   - error: Non-nil if parsing fails.
func (e *LLMQueryExpander) parseExpansionResponse(response string) ([]string, error) {
	// Try to find JSON in the response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	jsonStr := response[start : end+1]

	var result struct {
		Queries []string `json:"queries"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if len(result.Queries) == 0 {
		return nil, fmt.Errorf("no queries in response")
	}

	return result.Queries, nil
}

// isCommand returns true if the query is a command that should not be expanded.
//
// # Description
//
// Checks if the query matches any command in the stop list.
//
// # Inputs
//
//   - lowerQuery: The lowercased query.
//
// # Outputs
//
//   - bool: True if query is a command.
func (e *LLMQueryExpander) isCommand(lowerQuery string) bool {
	for _, cmd := range commandStopList {
		if lowerQuery == cmd || strings.HasPrefix(lowerQuery, cmd+" ") {
			return true
		}
	}
	return false
}

// containsPronouns returns true if the query contains pronouns or contextual references.
//
// # Description
//
// Checks if the query contains words that suggest it references previous context.
//
// # Inputs
//
//   - lowerQuery: The lowercased query.
//
// # Outputs
//
//   - bool: True if pronouns/references found.
func (e *LLMQueryExpander) containsPronouns(lowerQuery string) bool {
	words := strings.Fields(lowerQuery)
	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,!?;:'\"")
		for _, pronoun := range pronounPatterns {
			if word == pronoun {
				return true
			}
		}
	}
	return false
}

// =============================================================================
// Helper Functions
// =============================================================================

// cosineSimilarity computes the cosine similarity between two vectors.
//
// # Description
//
// Returns a value between -1 and 1, where 1 means identical direction.
//
// # Inputs
//
//   - a, b: The vectors to compare.
//
// # Outputs
//
//   - float64: The cosine similarity.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// sqrt computes the square root (avoiding math import for a single function).
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
