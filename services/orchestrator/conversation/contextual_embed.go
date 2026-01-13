package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Interfaces
// =============================================================================

// ContextBuilder defines the interface for building contextual queries for embedding.
//
// # Description
//
// ContextBuilder creates enriched query text by combining the current query
// with relevant conversation history. This helps embedding models produce
// vectors that capture conversational context, improving retrieval for
// follow-up queries like "tell me more".
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Example
//
//	builder := NewContextualEmbedder(llmClient, DefaultContextConfig())
//	enriched := builder.BuildContextualQuery(ctx, "tell me more", history)
//	// enriched = "tell me more | tell me more | Context: User is asking about Motown"
type ContextBuilder interface {
	// BuildContextualQuery creates enriched text for embedding by combining query with history.
	//
	// # Description
	//
	// Combines the query with summarized conversation history to create
	// text that produces more contextually relevant embeddings. The query
	// is repeated twice to boost its weight relative to history context.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation (used if summarization is enabled).
	//   - query: The current user query.
	//   - history: Recent conversation turns for context.
	//
	// # Outputs
	//
	//   - string: The enriched query text for embedding.
	//
	// # Example
	//
	//   enriched := builder.BuildContextualQuery(ctx, "tell me more", history)
	//   // "tell me more | tell me more | Context: Discussing Motown Records"
	//
	// # Limitations
	//
	//   - If summarization fails, falls back to truncated raw history.
	//   - Total output length is limited by MaxChars config.
	//
	// # Assumptions
	//
	//   - History is ordered newest first.
	BuildContextualQuery(ctx context.Context, query string, history []RelevantTurn) string

	// SummarizeContext creates a focused summary of conversation history.
	//
	// # Description
	//
	// Uses an LLM to distill conversation history into a concise, focused
	// summary. This produces cleaner context than raw history truncation,
	// improving embedding quality.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - history: Conversation turns to summarize.
	//
	// # Outputs
	//
	//   - string: A focused summary of the conversation.
	//   - error: Non-nil if LLM call fails (caller should use fallback).
	//
	// # Example
	//
	//   summary, err := builder.SummarizeContext(ctx, history)
	//   // summary = "User is asking about Berry Gordy and Motown Records"
	//
	// # Limitations
	//
	//   - Latency: ~200ms depending on model.
	//   - May lose nuance in complex multi-topic conversations.
	//
	// # Assumptions
	//
	//   - LLM client is configured and available.
	SummarizeContext(ctx context.Context, history []RelevantTurn) (string, error)
}

// =============================================================================
// Types
// =============================================================================

// ContextConfig holds configuration for contextual embedding.
//
// # Description
//
// ContextConfig allows customization of how history is processed
// and combined with queries for embedding.
//
// # Example
//
//	config := DefaultContextConfig()
//	config.MaxChars = 1000  // Increase context size
//	builder := NewContextualEmbedder(llmClient, config)
type ContextConfig struct {
	// Enabled controls whether contextual embedding is active.
	// If false, BuildContextualQuery returns query unchanged.
	// Default: true (can be set via CONTEXTUAL_EMBED_ENABLED)
	Enabled bool

	// SummarizationEnabled controls whether LLM summarization is used.
	// If false, raw history truncation is used instead.
	// Default: true (can be set via CONTEXT_SUMMARIZATION_ENABLED)
	SummarizationEnabled bool

	// SummarizationModel is the LLM model for summarization.
	// If empty, defaults to OLLAMA_MODEL environment variable.
	// Default: "" (uses OLLAMA_MODEL)
	SummarizationModel string

	// SummarizationMaxTokens is the max tokens for summary response.
	// Default: 50
	SummarizationMaxTokens int

	// SummarizationTimeoutMs is the timeout for summarization LLM calls.
	// Default: 1000
	SummarizationTimeoutMs int

	// MaxChars is the maximum total characters for enriched query.
	// Default: 500
	MaxChars int

	// MaxTurns is the maximum number of history turns to include.
	// Default: 2
	MaxTurns int

	// AnswerLimit is the maximum characters per answer in raw truncation.
	// Default: 150
	AnswerLimit int
}

// DefaultContextConfig returns the default context configuration.
//
// # Description
//
// Returns a ContextConfig with sensible defaults for contextual embedding.
// Values can be overridden via environment variables:
//   - CONTEXTUAL_EMBED_ENABLED (default: "true")
//   - CONTEXT_SUMMARIZATION_ENABLED (default: "true")
//   - CONTEXT_SUMMARIZATION_MODEL (default: "" → uses OLLAMA_MODEL)
//   - CONTEXT_SUMMARIZATION_MAX_TOKENS (default: 50)
//   - CONTEXT_SUMMARIZATION_TIMEOUT_MS (default: 1000)
//   - CONTEXTUAL_EMBED_MAX_CHARS (default: 500)
//   - CONTEXTUAL_EMBED_MAX_TURNS (default: 2)
//   - CONTEXTUAL_EMBED_ANSWER_LIMIT (default: 150)
//
// # Outputs
//
//   - ContextConfig: Configuration with default or env-configured values.
//
// # Example
//
//	config := DefaultContextConfig()
//	// config.Enabled == true (or CONTEXTUAL_EMBED_ENABLED if set)
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		Enabled:                getEnvBool("CONTEXTUAL_EMBED_ENABLED", true),
		SummarizationEnabled:   getEnvBool("CONTEXT_SUMMARIZATION_ENABLED", true),
		SummarizationModel:     getEnvString("CONTEXT_SUMMARIZATION_MODEL", ""),
		SummarizationMaxTokens: getEnvInt("CONTEXT_SUMMARIZATION_MAX_TOKENS", 50),
		SummarizationTimeoutMs: getEnvInt("CONTEXT_SUMMARIZATION_TIMEOUT_MS", 1000),
		MaxChars:               getEnvInt("CONTEXTUAL_EMBED_MAX_CHARS", 500),
		MaxTurns:               getEnvInt("CONTEXTUAL_EMBED_MAX_TURNS", 2),
		AnswerLimit:            getEnvInt("CONTEXTUAL_EMBED_ANSWER_LIMIT", 150),
	}
}

// =============================================================================
// Implementation
// =============================================================================

// ContextualEmbedder implements ContextBuilder with optional LLM summarization.
//
// # Description
//
// ContextualEmbedder creates enriched query text by combining the current query
// with conversation history. It supports two modes:
//   - Summarization mode: Uses LLM to create a focused summary (higher quality)
//   - Truncation mode: Falls back to raw history truncation (faster, no LLM call)
//
// The query is repeated twice in the output to boost its weight relative to
// the history context, preventing embedding drift.
//
// # Thread Safety
//
// ContextualEmbedder is safe for concurrent use.
//
// # Example
//
//	embedder := NewContextualEmbedder(llmClient, DefaultContextConfig())
//	enriched := embedder.BuildContextualQuery(ctx, "tell me more", history)
type ContextualEmbedder struct {
	generate GenerateFunc
	config   ContextConfig
}

// NewContextualEmbedder creates a new ContextualEmbedder with the given client and config.
//
// # Description
//
// Creates an embedder that combines queries with conversation history context.
// If llmClient is nil, summarization mode is automatically disabled.
//
// # Inputs
//
//   - client: The LLM client for summarization (can be nil).
//   - config: Configuration for context building.
//
// # Outputs
//
//   - *ContextualEmbedder: The configured embedder.
//
// # Example
//
//	embedder := NewContextualEmbedder(ollamaClient, DefaultContextConfig())
//
// # Limitations
//
//   - If client is nil, SummarizationEnabled is forced to false.
//
// # Assumptions
//
//   - If client is provided, it is properly configured.
func NewContextualEmbedder(generate GenerateFunc, config ContextConfig) *ContextualEmbedder {
	// Disable summarization if no generate function
	if generate == nil {
		config.SummarizationEnabled = false
	}

	return &ContextualEmbedder{
		generate: generate,
		config:   config,
	}
}

// BuildContextualQuery creates enriched text for embedding.
//
// # Description
//
// Combines the query with conversation history to create text that
// produces more contextually relevant embeddings. The format is:
//
//	"{query} | {query} | Context: {summary or truncated history}"
//
// The query is repeated twice to boost its weight.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - query: The current user query.
//   - history: Recent conversation turns.
//
// # Outputs
//
//   - string: The enriched query text.
//
// # Example
//
//   enriched := e.BuildContextualQuery(ctx, "tell me more", history)
//
// # Limitations
//
//   - Falls back to truncation if summarization fails.
//
// # Assumptions
//
//   - History is ordered with most recent first.
func (e *ContextualEmbedder) BuildContextualQuery(ctx context.Context, query string, history []RelevantTurn) string {
	// If disabled, return query unchanged
	if !e.config.Enabled {
		return query
	}

	// If no history, return query repeated (still boost weight)
	if len(history) == 0 {
		return fmt.Sprintf("%s | %s", query, query)
	}

	// Limit history to MaxTurns
	if len(history) > e.config.MaxTurns {
		history = history[:e.config.MaxTurns]
	}

	// Get context summary or truncation
	contextStr := e.getContextString(ctx, history)

	// Build enriched query: query repeated twice for weight boost
	enriched := fmt.Sprintf("%s | %s | Context: %s", query, query, contextStr)

	// Enforce max chars
	if len(enriched) > e.config.MaxChars {
		// Truncate context to fit
		availableForContext := e.config.MaxChars - len(query) - len(query) - len(" | ") - len(" | Context: ") - 3
		if availableForContext > 0 {
			contextStr = truncateString(contextStr, availableForContext)
			enriched = fmt.Sprintf("%s | %s | Context: %s", query, query, contextStr)
		} else {
			// Query itself is too long, just return truncated query
			enriched = truncateString(query, e.config.MaxChars)
		}
	}

	return enriched
}

// SummarizeContext creates a focused summary of conversation history.
//
// # Description
//
// Uses the configured LLM to distill conversation history into a
// concise summary. Falls back to truncation on error.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - history: Conversation turns to summarize.
//
// # Outputs
//
//   - string: The summary text.
//   - error: Non-nil if LLM call fails.
//
// # Example
//
//   summary, err := e.SummarizeContext(ctx, history)
//
// # Limitations
//
//   - Latency: ~200ms.
//
// # Assumptions
//
//   - LLM client is available.
func (e *ContextualEmbedder) SummarizeContext(ctx context.Context, history []RelevantTurn) (string, error) {
	if e.generate == nil {
		return "", fmt.Errorf("generate function not configured")
	}

	if len(history) == 0 {
		return "", nil
	}

	// Build prompt for summarization
	prompt := e.buildSummarizationPrompt(history)

	// Apply timeout
	timeout := time.Duration(e.config.SummarizationTimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Call LLM
	response, err := e.generate(ctx, prompt, e.config.SummarizationMaxTokens)
	if err != nil {
		return "", fmt.Errorf("summarization LLM call failed: %w", err)
	}

	// Clean up response
	summary := strings.TrimSpace(response)

	// Remove any prefix like "Summary:" that LLM might add
	prefixes := []string{"Summary:", "Context:", "The user", "User"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(summary, prefix) {
			summary = strings.TrimPrefix(summary, prefix)
			summary = strings.TrimSpace(summary)
		}
	}

	return summary, nil
}

// getContextString returns either a summarized or truncated context string.
//
// # Description
//
// Attempts summarization if enabled, otherwise falls back to truncation.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - history: Conversation turns.
//
// # Outputs
//
//   - string: The context string.
func (e *ContextualEmbedder) getContextString(ctx context.Context, history []RelevantTurn) string {
	if e.config.SummarizationEnabled && e.generate != nil {
		summary, err := e.SummarizeContext(ctx, history)
		if err == nil && summary != "" {
			return summary
		}
		// Fall back to truncation on error
	}

	return e.truncateHistory(history)
}

// buildSummarizationPrompt creates the prompt for context summarization.
//
// # Description
//
// Constructs a prompt that instructs the LLM to summarize the
// conversation history into a focused sentence.
//
// # Inputs
//
//   - history: Conversation turns to summarize.
//
// # Outputs
//
//   - string: The formatted prompt.
func (e *ContextualEmbedder) buildSummarizationPrompt(history []RelevantTurn) string {
	var historyText strings.Builder
	for _, turn := range history {
		historyText.WriteString(fmt.Sprintf("User: %s\nAssistant: %s\n\n",
			truncateString(turn.Question, 200),
			truncateString(turn.Answer, e.config.AnswerLimit)))
	}

	return fmt.Sprintf(`Summarize this conversation into a single focused sentence describing what the user is asking about:

Conversation:
%s
Summary (one sentence):`, historyText.String())
}

// truncateHistory creates a truncated raw history string.
//
// # Description
//
// Creates a context string by concatenating and truncating history turns.
// Used as a fallback when summarization is disabled or fails.
//
// # Inputs
//
//   - history: Conversation turns.
//
// # Outputs
//
//   - string: The truncated history string.
func (e *ContextualEmbedder) truncateHistory(history []RelevantTurn) string {
	var parts []string

	for _, turn := range history {
		q := truncateString(turn.Question, 100)
		a := truncateString(turn.Answer, e.config.AnswerLimit)
		parts = append(parts, fmt.Sprintf("Q: %s A: %s", q, a))
	}

	result := strings.Join(parts, " | ")

	// Enforce total length
	maxLen := e.config.MaxChars / 2 // Leave room for query
	if len(result) > maxLen {
		result = truncateString(result, maxLen)
	}

	return result
}
