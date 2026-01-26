// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package datatypes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type embeddingServiceRequest struct {
	Texts []string `json:"texts"`
}

type embeddingServiceResponse struct {
	Vectors   [][]float32 `json:"vectors"`
	Model     string      `json:"model"`
	Dim       int         `json:"dim"`
	Timestamp int64       `json:"timestamp"`
	Id        string      `json:"id"`
}

// ollamaEmbedRequest is the request format for Ollama's /api/embed endpoint.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse is the response format from Ollama's /api/embed endpoint.
type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

type EmbeddingRequest struct {
	Text string `json:"text"`
}

type EmbeddingResponse struct {
	Id        string    `json:"id"`
	Timestamp int       `json:"timestamp"`
	Text      string    `json:"text"`
	Vector    []float32 `json:"vector"`
	Dim       int       `json:"dim"`
}

type CodeSnippetProperties struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
	Language string `json:"language"`
}

type WeaviateObject struct {
	Class      string                `json:"class"`
	Properties CodeSnippetProperties `json:"properties"`
	Vector     []float32             `json:"vector"`
}

type WeaviateConversationMemoryObject struct {
	Class      string                 `json:"class"`
	Properties ConversationProperties `json:"properties"`
	Vector     []float32              `json:"vector"`
}

type ConversationProperties struct {
	SessionId string `json:"session_id"`
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Timestamp int64  `json:"timestamp"`
}

type WeaviateSessionObject struct {
	Class      string            `json:"class"`
	Properties SessionProperties `json:"properties"`
}

type SessionProperties struct {
	SessionId     string `json:"session_id"`
	Summary       string `json:"summary"`
	Timestamp     int64  `json:"timestamp"`
	TTLExpiresAt  int64  `json:"ttl_expires_at"`
	TTLDurationMs int64  `json:"ttl_duration_ms"`
	DataSpace     string `json:"data_space"`
	Pipeline      string `json:"pipeline"`
}

type RAGRequest struct {
	Query      string `json:"query"`
	SessionId  string `json:"session_id"`
	Pipeline   string `json:"pipeline"`
	NoRag      bool   `json:"no_rag"`
	DataSpace  string `json:"data_space,omitempty"`  // Data space to filter queries by (e.g., "work", "personal")
	SessionTTL string `json:"session_ttl,omitempty"` // Session TTL (e.g., "24h", "7d"). Resets on each message.
}

// =============================================================================
// Verified Pipeline Configuration Types
// =============================================================================

// TemperatureOverrides allows per-request temperature configuration for the
// verified pipeline's debate roles.
//
// # Description
//
// The verified pipeline uses three LLM roles with different temperature needs:
//   - Optimist: Generates initial draft (higher temp = more creative)
//   - Skeptic: Audits for hallucinations (lower temp = more consistent)
//   - Refiner: Rewrites to remove hallucinations (balanced temp)
//
// All fields are optional. Omitted fields use the pipeline's configured defaults
// (from environment variables or config).
//
// # Fields
//
//   - Optimist: Temperature for draft generation (0.0-2.0, default: 0.6)
//   - Skeptic: Temperature for skeptic audits (0.0-2.0, default: 0.2)
//   - Refiner: Temperature for refinement (0.0-2.0, default: 0.4)
//
// # Examples
//
//	// More creative drafts, stricter verification
//	overrides := &TemperatureOverrides{Optimist: ptr(0.8), Skeptic: ptr(0.1)}
//
//	// Conservative mode (all low temperatures)
//	overrides := &TemperatureOverrides{Optimist: ptr(0.3), Skeptic: ptr(0.1), Refiner: ptr(0.2)}
type TemperatureOverrides struct {
	Optimist *float64 `json:"optimist,omitempty"`
	Skeptic  *float64 `json:"skeptic,omitempty"`
	Refiner  *float64 `json:"refiner,omitempty"`
}

// VerifiedPipelineConfig contains configuration options specific to the
// verified (skeptic/optimist) RAG pipeline.
//
// # Description
//
// This struct extends the standard RAG request with verified-pipeline-specific
// options like temperature overrides and strictness mode.
//
// # Fields
//
//   - TemperatureOverrides: Per-role temperature settings (optional)
//   - Strictness: "strict" (cite every fact) or "balanced" (allow synthesis)
//
// # Examples
//
//	config := &VerifiedPipelineConfig{
//	    TemperatureOverrides: &TemperatureOverrides{Skeptic: ptr(0.1)},
//	    Strictness: "strict",
//	}
type VerifiedPipelineConfig struct {
	TemperatureOverrides *TemperatureOverrides `json:"temperature_overrides,omitempty"`
	Strictness           string                `json:"strictness,omitempty"` // "strict" or "balanced"
}

// ToMap converts TemperatureOverrides to a map for JSON serialization.
// Only includes non-nil values.
func (t *TemperatureOverrides) ToMap() map[string]float64 {
	if t == nil {
		return nil
	}
	result := make(map[string]float64)
	if t.Optimist != nil {
		result["optimist"] = *t.Optimist
	}
	if t.Skeptic != nil {
		result["skeptic"] = *t.Skeptic
	}
	if t.Refiner != nil {
		result["refiner"] = *t.Refiner
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// =============================================================================
// Retrieval-Only Types (for streaming integration)
// =============================================================================

// RetrievalRequest is the request sent to the Python RAG engine's
// retrieval-only endpoint. It returns document content without LLM generation.
//
// # Description
//
// This request type supports the streaming integration where:
// 1. Python RAG retrieves and reranks documents
// 2. Go orchestrator receives raw document content
// 3. Go orchestrator streams LLM response with full document context
//
// This fixes the "two-LLM" problem where Python generated an answer,
// then Go used that answer as "context" for a second LLM that would
// say "no documents found" even when sources were displayed.
//
// # Fields
//
//   - Query: The user's query to retrieve documents for.
//   - Pipeline: The RAG pipeline to use ("reranking" or "verified").
//   - SessionId: Optional session ID for session-scoped document filtering.
//   - StrictMode: If true, only return documents above relevance threshold.
//   - MaxChunks: Maximum number of document chunks to return (default: 5).
//
// # Supported Pipelines
//
//   - "reranking": Cross-encoder reranking for better relevance ordering.
//   - "verified": Skeptic/optimist debate for hallucination prevention.
type RetrievalRequest struct {
	Query      string `json:"query"`
	Pipeline   string `json:"pipeline,omitempty"`
	SessionId  string `json:"session_id,omitempty"`
	StrictMode bool   `json:"strict_mode"`
	MaxChunks  int    `json:"max_chunks,omitempty"`
	DataSpace  string `json:"data_space,omitempty"` // Data space to filter queries by
}

// RetrievalChunk represents a single document chunk from retrieval-only mode.
//
// # Description
//
// Each chunk contains the document content, source identifier, and optional
// relevance score from the reranking model.
//
// # Fields
//
//   - Content: The actual document text.
//   - Source: Document name, path, or URL identifying the source.
//   - RerankScore: Relevance score from reranking (higher = more relevant).
type RetrievalChunk struct {
	Content     string   `json:"content"`
	Source      string   `json:"source"`
	RerankScore *float64 `json:"rerank_score,omitempty"`
}

// RetrievalResponse is the response from the Python RAG engine's
// retrieval-only endpoint.
//
// # Description
//
// Contains the retrieved document chunks, pre-formatted context text
// for direct use in LLM prompts, and a flag indicating whether
// relevant documents were found.
//
// # Fields
//
//   - Chunks: List of retrieved document chunks with content and metadata.
//   - ContextText: Formatted string with "[Document N: source]" headers
//     ready for LLM context. This enables clear citation in responses.
//   - HasRelevantDocs: Boolean indicating if documents above the relevance
//     threshold were found. Used to determine if "no documents" message
//     should be shown.
//
// # Context Format Example
//
//	[Document 1: detroit_history.md]
//	Detroit was founded in 1701 by French colonists...
//
//	[Document 2: detroit_economy.md]
//	The city's economy was historically based on automotive manufacturing...
type RetrievalResponse struct {
	Chunks          []RetrievalChunk `json:"chunks"`
	ContextText     string           `json:"context_text"`
	HasRelevantDocs bool             `json:"has_relevant_docs"`
}

// NewRetrievalRequest creates a new RetrievalRequest with default values.
//
// # Description
//
// Creates a RetrievalRequest with sensible defaults:
//   - Pipeline: "reranking" (cross-encoder reranking)
//   - StrictMode: true (only return relevant documents)
//   - MaxChunks: 5 (matches top_k_final in reranking pipeline)
//
// # Inputs
//
//   - query: The user's query to retrieve documents for.
//   - sessionId: Optional session ID (pass empty string for no session).
//
// # Outputs
//
//   - *RetrievalRequest: Ready to be sent to the RAG engine.
//
// # Examples
//
//	req := NewRetrievalRequest("What is Detroit known for?", "sess_abc123")
//	req := NewRetrievalRequest("authentication", "")  // No session
//	req := NewRetrievalRequest("query", "").WithPipeline("verified")
func NewRetrievalRequest(query, sessionId string) *RetrievalRequest {
	return &RetrievalRequest{
		Query:      query,
		Pipeline:   "reranking",
		SessionId:  sessionId,
		StrictMode: true,
		MaxChunks:  5,
	}
}

// WithPipeline sets the RAG pipeline and returns the request for chaining.
//
// # Description
//
// Sets the retrieval pipeline to use. Supported pipelines:
//   - "reranking": Cross-encoder reranking for better relevance ordering.
//   - "verified": Skeptic/optimist debate for hallucination prevention.
//
// # Inputs
//
//   - pipeline: The pipeline name ("reranking" or "verified").
//
// # Outputs
//
//   - *RetrievalRequest: The modified request for method chaining.
//
// # Examples
//
//	req := NewRetrievalRequest("query", "sess_123").WithPipeline("verified")
func (r *RetrievalRequest) WithPipeline(pipeline string) *RetrievalRequest {
	r.Pipeline = pipeline
	return r
}

// WithStrictMode sets the strict mode flag and returns the request for chaining.
func (r *RetrievalRequest) WithStrictMode(strict bool) *RetrievalRequest {
	r.StrictMode = strict
	return r
}

// WithMaxChunks sets the maximum chunks and returns the request for chaining.
func (r *RetrievalRequest) WithMaxChunks(max int) *RetrievalRequest {
	r.MaxChunks = max
	return r
}

// WithDataSpace sets the data space filter and returns the request for chaining.
//
// # Description
//
// Sets the data space for query isolation. Documents are filtered to only
// include those from the specified data space (e.g., "work" or "personal").
// If not set or empty, searches across ALL data spaces (no isolation).
//
// # Inputs
//
//   - dataSpace: The data space name to filter by.
//
// # Outputs
//
//   - *RetrievalRequest: The modified request for method chaining.
//
// # Examples
//
//	req := NewRetrievalRequest("query", "sess_123").WithDataSpace("work")
func (r *RetrievalRequest) WithDataSpace(dataSpace string) *RetrievalRequest {
	r.DataSpace = dataSpace
	return r
}

// SourceInfo represents a retrieved document source from RAG retrieval.
//
// # Description
//
// SourceInfo captures metadata about a document retrieved during RAG
// processing. Each source has a unique ID and timestamp for database
// storage and audit trails.
//
// # Fields
//
//   - Id: Unique identifier for this source record (UUID v4).
//   - CreatedAt: Unix timestamp in milliseconds when source was retrieved.
//   - Source: Document name, path, or URL identifying the source.
//   - Distance: Vector distance (lower = more similar). Used by some pipelines.
//   - Score: Relevance score (higher = more relevant). Used by reranking pipelines.
//   - Hash: SHA-256 hash of source content at retrieval time. Used for
//     tamper detection and audit trails. Part of the hash chain.
//
// # Notes
//
// Distance and Score are mutually exclusive - only one will be set
// depending on the RAG pipeline used.
type SourceInfo struct {
	Id            string  `json:"id,omitempty"`
	CreatedAt     int64   `json:"created_at,omitempty"`
	Source        string  `json:"source"`
	Distance      float64 `json:"distance,omitempty"`
	Score         float64 `json:"score,omitempty"`
	Hash          string  `json:"hash,omitempty"`
	VersionNumber *int    `json:"version_number,omitempty"` // Document version (1, 2, 3...)
	IsCurrent     *bool   `json:"is_current,omitempty"`     // True if this is the latest version
	IngestedAt    int64   `json:"ingested_at,omitempty"`    // Unix ms timestamp when document was ingested
}

type RAGResponse struct {
	Answer    string       `json:"answer"`
	SessionId string       `json:"session_id"`
	Sources   []SourceInfo `json:"sources,omitempty"`
}

// =============================================================================
// Recency Bias - Time-Decay Scoring
// =============================================================================

// IMPORTANT: Recency Bias vs Document Versioning
//
// Recency bias is for TIME-SENSITIVE CONTENT where newer information is inherently
// more relevant (news, changelogs, daily reports). It is NOT for document versioning.
//
// Document versioning is handled separately:
//   - Old versions are marked is_current=false on re-ingestion
//   - RAG queries filter by is_current=true by default (see base.py)
//   - Use --keep-versions to delete old versions entirely
//
// Using recency bias on versioned documents will incorrectly penalize old-but-relevant
// content (e.g., a "Company Mission Statement" from 2020 would be deprioritized).

// RecencyDecayPreset defines decay rates for recency bias presets.
//
// # Presets
//
//   - none: No decay (λ=0). Default, backward compatible.
//   - gentle: λ=0.01. 50% at ~69 days. General knowledge bases.
//   - moderate: λ=0.05. 50% at ~14 days. News, changelogs.
//   - aggressive: λ=0.1. 50% at ~7 days. Fast-changing data.
var RecencyDecayPreset = map[string]float64{
	"none":       0.0,
	"gentle":     0.01,
	"moderate":   0.05,
	"aggressive": 0.1,
}

// GetRecencyDecayRate returns the decay rate for a given preset.
//
// # Description
//
// Looks up the decay rate (λ) for the given preset name. If the preset
// is not recognized, returns 0.0 (no decay) for backward compatibility.
//
// # Inputs
//
//   - preset: One of "none", "gentle", "moderate", "aggressive"
//
// # Outputs
//
//   - float64: The decay rate λ for exponential decay formula
func GetRecencyDecayRate(preset string) float64 {
	if rate, ok := RecencyDecayPreset[preset]; ok {
		return rate
	}
	return 0.0 // Unknown preset = no decay
}

// ApplyRecencyDecay applies time-based decay to source scores.
//
// # Description
//
// Applies exponential decay to document scores based on age:
//
//	final_score = semantic_score * exp(-λ * age_in_days)
//
// Where λ (lambda) is the decay rate. Higher λ = faster decay.
// After decay, sources are re-sorted by score (highest first).
//
// # When To Use
//
// Use recency decay ONLY for time-sensitive content where newer = more relevant:
//   - News articles and current events
//   - Changelogs and release notes
//   - Daily/weekly reports
//   - Time-series data
//
// Do NOT use for document versioning (handled by is_current filter) or static
// knowledge bases where old content is equally valid.
//
// # Inputs
//
//   - sources: Slice of SourceInfo to apply decay to.
//   - decayRate: The decay rate λ. Use GetRecencyDecayRate() for presets.
//
// # Outputs
//
//   - []SourceInfo: Sources with adjusted scores, sorted by score descending.
//
// # Notes
//
//   - If decayRate is 0, returns sources unchanged.
//   - Uses Score field if set, otherwise Distance (inverted for decay).
//   - IngestedAt must be set for decay to work; 0 = no decay applied.
func ApplyRecencyDecay(sources []SourceInfo, decayRate float64) []SourceInfo {
	if decayRate == 0 || len(sources) == 0 {
		return sources
	}

	now := time.Now().UnixMilli()
	result := make([]SourceInfo, len(sources))
	copy(result, sources)

	for i := range result {
		if result[i].IngestedAt == 0 {
			continue // No timestamp, skip decay
		}

		// Calculate age in days
		ageMs := now - result[i].IngestedAt
		ageDays := float64(ageMs) / (24 * 60 * 60 * 1000)
		if ageDays < 0 {
			ageDays = 0 // Future timestamps treated as brand new
		}

		// Calculate decay factor: exp(-λ * age)
		decayFactor := math.Exp(-decayRate * ageDays)

		// Apply decay to score
		if result[i].Score > 0 {
			result[i].Score *= decayFactor
		} else if result[i].Distance > 0 {
			// Distance is inverse of similarity, so we increase it
			// Higher distance = less relevant after decay
			result[i].Distance /= decayFactor
		}
	}

	// Re-sort by score descending (or distance ascending)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score > 0 && result[j].Score > 0 {
			return result[i].Score > result[j].Score
		}
		if result[i].Distance > 0 && result[j].Distance > 0 {
			return result[i].Distance < result[j].Distance
		}
		return result[i].Score > result[j].Score
	})

	slog.Debug("Applied recency decay to sources",
		"decay_rate", decayRate,
		"source_count", len(result),
	)

	return result
}

// HistoryTurn represents a single question-answer pair in conversation history.
//
// # Description
//
// HistoryTurn captures a complete exchange between user and assistant.
// Used for loading previous conversation context when resuming sessions.
// Each turn has a unique ID and timestamp for database storage.
//
// # Fields
//
//   - Id: Unique identifier for this turn (UUID v4).
//   - CreatedAt: Unix timestamp in milliseconds when turn was created.
//   - Question: The user's input message.
//   - Answer: The assistant's response.
//   - Hash: SHA-256 hash of Question+Answer. Used for tamper detection.
//     Formula: SHA256(Question || Answer || CreatedAt || PrevHash)
type HistoryTurn struct {
	Id        string `json:"id,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Hash      string `json:"hash,omitempty"`
}

type RagEngineResponse struct {
	Answer  string       `json:"answer"`
	Sources []SourceInfo `json:"sources,omitempty"`
}

// Message represents a single message in a conversation.
//
// # Description
//
// Message is the fundamental unit of conversation in both direct chat and RAG
// chat endpoints. Each message has a role (who said it) and content (what they said).
// Optional ID and timestamp fields enable database storage and audit trails.
//
// # Fields
//
//   - MessageID: Optional. Unique identifier for this message (UUID v4).
//     Used for database correlation and message-level operations.
//   - Timestamp: Optional. Unix timestamp in milliseconds (UTC) when message was created.
//     Used for ordering, audit trails, and retention policies.
//   - Role: Required. The role of the message sender.
//     Must be one of: "user", "assistant", "system".
//   - Content: Required. The message text content.
//     Limited to 32KB per SEC-003 compliance.
//
// # Validation
//
//   - Role: required, must be "user", "assistant", or "system"
//   - Content: required, max 32KB (validated via maxbytes custom validator in chat.go)
//   - MessageID: optional, but if provided must be valid UUID v4
//   - Timestamp: optional, but if provided must be > 0
//
// # Security References
//
//   - SEC-003: Message size limits (security_architecture_review.md)
type Message struct {
	MessageID string `json:"message_id,omitempty" validate:"omitempty,uuid4"`
	Timestamp int64  `json:"timestamp,omitempty" validate:"omitempty,gt=0"`
	Role      string `json:"role" validate:"required,oneof=user assistant system"`
	Content   string `json:"content" validate:"required,maxbytes"`
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func (e *EmbeddingResponse) Get(text string) error {
	embeddingServiceURL := os.Getenv("EMBEDDING_SERVICE_URL")
	if embeddingServiceURL == "" {
		return fmt.Errorf("EMBEDDING_SERVICE_URL not set")
	}

	// Detect if we're using Ollama based on URL pattern
	isOllama := strings.Contains(embeddingServiceURL, "/api/embed")

	var reqBody []byte
	var err error

	if isOllama {
		// Ollama format: {"model": "...", "input": "..."}
		embeddingModel := os.Getenv("EMBEDDING_MODEL")
		if embeddingModel == "" {
			embeddingModel = "nomic-embed-text-v2-moe" // Default
		}
		embReq := ollamaEmbedRequest{Model: embeddingModel, Input: text}
		reqBody, err = json.Marshal(embReq)
	} else {
		// Legacy format: {"texts": ["..."]}
		embReq := embeddingServiceRequest{Texts: []string{text}}
		reqBody, err = json.Marshal(embReq)
	}
	if err != nil {
		return fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, embeddingServiceURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to setup a new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make the request to the embedding service: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("Failed to close out the body on func close")
		}
	}(resp.Body)

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("embedding service returned non-200 status: %s, %d", string(bodyBytes), resp.StatusCode)
	}

	if isOllama {
		// Parse Ollama response: {"model": "...", "embeddings": [[...]]}
		var ollamaResp ollamaEmbedResponse
		if err := json.Unmarshal(bodyBytes, &ollamaResp); err != nil {
			return fmt.Errorf("failed to parse Ollama embedding response: %w", err)
		}
		if len(ollamaResp.Embeddings) == 0 || len(ollamaResp.Embeddings[0]) == 0 {
			return fmt.Errorf("Ollama returned no embeddings")
		}
		e.Vector = ollamaResp.Embeddings[0]
		e.Dim = len(e.Vector)
		e.Text = text
		e.Timestamp = int(time.Now().Unix())
		return nil
	}

	// Parse legacy response: {"vectors": [[...]]}
	var serviceResp embeddingServiceResponse
	if err := json.Unmarshal(bodyBytes, &serviceResp); err != nil {
		slog.Warn("Failed to parse embedding service response as batch, trying single", "error", err)
		if err := json.Unmarshal(bodyBytes, &e); err != nil {
			return fmt.Errorf("failed to parse response from embedding service in any format: %w", err)
		}
		return nil
	}

	// Check that we got at least one vector back
	if len(serviceResp.Vectors) == 0 || len(serviceResp.Vectors[0]) == 0 {
		return fmt.Errorf("embedding service returned no vectors")
	}

	e.Vector = serviceResp.Vectors[0]
	e.Dim = len(e.Vector)
	e.Text = text
	e.Timestamp = int(time.Now().Unix())
	e.Id = serviceResp.Id

	return nil
}

// GetWithContext fetches embeddings with context support for cancellation and timeout.
//
// # Description
//
// This is a context-aware version of Get() that respects cancellation signals.
// Use this when the embedding call is part of an HTTP request handler or other
// cancelable operation.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - text: The text to embed.
//
// # Outputs
//
//   - error: Non-nil if context is canceled, timed out, or embedding fails.
//
// # Example
//
//	var emb EmbeddingResponse
//	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
//	defer cancel()
//	if err := emb.GetWithContext(ctx, "What is AI?"); err != nil { ... }
func (e *EmbeddingResponse) GetWithContext(ctx context.Context, text string) error {
	embeddingServiceURL := os.Getenv("EMBEDDING_SERVICE_URL")
	if embeddingServiceURL == "" {
		return fmt.Errorf("EMBEDDING_SERVICE_URL not set")
	}

	// Detect if we're using Ollama based on URL pattern
	isOllama := strings.Contains(embeddingServiceURL, "/api/embed")

	var reqBody []byte
	var err error

	if isOllama {
		// Ollama format: {"model": "...", "input": "..."}
		embeddingModel := os.Getenv("EMBEDDING_MODEL")
		if embeddingModel == "" {
			embeddingModel = "nomic-embed-text-v2-moe" // Default
		}
		embReq := ollamaEmbedRequest{Model: embeddingModel, Input: text}
		reqBody, err = json.Marshal(embReq)
	} else {
		// Legacy format: {"texts": ["..."]}
		embReq := embeddingServiceRequest{Texts: []string{text}}
		reqBody, err = json.Marshal(embReq)
	}
	if err != nil {
		return fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingServiceURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to setup a new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")

	resp, err := httpClient.Do(req)
	if err != nil {
		// Check if context was canceled
		if ctx.Err() != nil {
			return fmt.Errorf("embedding request canceled: %w", ctx.Err())
		}
		return fmt.Errorf("failed to make the request to the embedding service: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("Failed to close out the body on func close")
		}
	}(resp.Body)

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("embedding service returned non-200 status: %s, %d", string(bodyBytes), resp.StatusCode)
	}

	if isOllama {
		// Parse Ollama response: {"model": "...", "embeddings": [[...]]}
		var ollamaResp ollamaEmbedResponse
		if err := json.Unmarshal(bodyBytes, &ollamaResp); err != nil {
			return fmt.Errorf("failed to parse Ollama embedding response: %w", err)
		}
		if len(ollamaResp.Embeddings) == 0 || len(ollamaResp.Embeddings[0]) == 0 {
			return fmt.Errorf("Ollama returned no embeddings")
		}
		e.Vector = ollamaResp.Embeddings[0]
		e.Dim = len(e.Vector)
		e.Text = text
		e.Timestamp = int(time.Now().Unix())
		return nil
	}

	// Parse legacy response: {"vectors": [[...]]}
	var serviceResp embeddingServiceResponse
	if err := json.Unmarshal(bodyBytes, &serviceResp); err != nil {
		slog.Warn("Failed to parse embedding service response as batch, trying single", "error", err)
		if err := json.Unmarshal(bodyBytes, &e); err != nil {
			return fmt.Errorf("failed to parse response from embedding service in any format: %w", err)
		}
		return nil
	}

	// Check that we got at least one vector back
	if len(serviceResp.Vectors) == 0 || len(serviceResp.Vectors[0]) == 0 {
		return fmt.Errorf("embedding service returned no vectors")
	}

	e.Vector = serviceResp.Vectors[0]
	e.Dim = len(e.Vector)
	e.Text = text
	e.Timestamp = int(time.Now().Unix())
	e.Id = serviceResp.Id

	return nil
}

type WeaviateSchemas struct {
	Schemas []struct {
		Class       string `json:"class"`
		Description string `json:"description"`
		Vectorizer  string `json:"vectorizer"`
		Properties  []struct {
			Name        string   `json:"name"`
			DataType    []string `json:"dataType"`
			Description string   `json:"description"`
		} `json:"properties"`
	} `json:"schemas"`
}

func (w *WeaviateSchemas) InitializeSchemas() {
	for _, schema := range w.Schemas {
		schemaToString, err := json.Marshal(schema)
		if err != nil {
			slog.Error("failed to convert the schema back to a string", "error", err)
		}
		resp, err := http.Post(fmt.Sprintf("%s/schema", os.Getenv("WEAVIATE_SERVICE_URL")),
			"application/json", strings.NewReader(string(schemaToString)))
		if err != nil {
			log.Fatalf("FATAL: Could not send a schema to Weaviate: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Warn("Failed to read Weaviate schema response", "class", schema.Class, "error", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			slog.Warn(
				"Weaviate returned a non-200 status while creating a schema", "class", schema.Class, "status_code", resp.StatusCode, "response", string(body))
		} else {
			slog.Info("Successfully created or verified schema", "class", schema.Class)
		}
	}
}

// =============================================================================
// Conversational RAG Types (for /v1/chat/rag endpoint)
// =============================================================================

// ChatRAGRequest is used for conversational RAG with multi-turn support.
//
// # Hash Chain
//
// ContentHash contains SHA-256 hash of the message content for integrity
// verification. The server computes this on receipt and includes it in
// audit logs.
//
// # Verified Pipeline Configuration
//
// When Pipeline is "verified", the TemperatureOverrides field can be used
// to customize the temperature for each debate role (optimist, skeptic, refiner).
// The Strictness field controls how strictly the optimist must cite sources.
type ChatRAGRequest struct {
	Id                   string                `json:"id,omitempty"`                    // Request ID (server-generated for tracing)
	CreatedAt            int64                 `json:"created_at,omitempty"`            // Unix timestamp when request was created
	Message              string                `json:"message"`                         // Current user message
	SessionId            string                `json:"session_id,omitempty"`            // Optional: resume session
	Pipeline             string                `json:"pipeline,omitempty"`              // RAG pipeline (default: reranking)
	Bearing              string                `json:"bearing,omitempty"`               // Topic filter for retrieval
	Stream               bool                  `json:"stream,omitempty"`                // Enable SSE streaming
	History              []ChatTurn            `json:"history,omitempty"`               // Previous turns (if not using session)
	ContentHash          string                `json:"content_hash,omitempty"`          // SHA-256 hash of message for integrity
	StrictMode           bool                  `json:"strict_mode,omitempty"`           // Strict RAG: only answer from docs
	TemperatureOverrides *TemperatureOverrides `json:"temperature_overrides,omitempty"` // Verified pipeline: per-role temps
	Strictness           string                `json:"strictness,omitempty"`            // Verified pipeline: "strict" or "balanced"
	DataSpace            string                `json:"data_space,omitempty"`            // Data space to filter queries by (e.g., "work", "personal")
	VersionTag           string                `json:"version_tag,omitempty"`           // Specific version to query (e.g., "v1"); empty = current
	SessionTTL           string                `json:"session_ttl,omitempty"`           // Session TTL (e.g., "24h", "7d"). Resets on each message.
	RecencyBias          string                `json:"recency_bias,omitempty"`          // Recency bias preset: none, gentle, moderate, aggressive
}

// ChatTurn represents a single turn in a conversation
type ChatTurn struct {
	Id        string       `json:"id"`                // Turn ID (uuid)
	CreatedAt int64        `json:"created_at"`        // Unix timestamp when turn was created
	Role      string       `json:"role"`              // "user" or "assistant"
	Content   string       `json:"content"`           // Message content
	Sources   []SourceInfo `json:"sources,omitempty"` // Sources used (assistant only)
}

// Harbor represents a saved conversation bookmark
type Harbor struct {
	Id        string `json:"id"`         // Harbor ID (uuid)
	CreatedAt int64  `json:"created_at"` // Unix timestamp when harbor was created
	Name      string `json:"name"`       // User-defined name
	TurnIndex int    `json:"turn_index"` // Index of the turn this bookmark points to
}

// ChatRAGResponse is the non-streaming response.
//
// # Hash Chain
//
// ContentHash is SHA-256 hash of the answer content.
// ChainHash is the final hash if this response was generated via streaming
// (accumulated from all StreamEvent hashes).
type ChatRAGResponse struct {
	Id          string       `json:"id"`                     // Response ID (for logging/tracing)
	CreatedAt   int64        `json:"created_at"`             // Unix timestamp when response was generated
	Answer      string       `json:"answer"`                 // LLM response text
	SessionId   string       `json:"session_id"`             // Session this belongs to
	Sources     []SourceInfo `json:"sources,omitempty"`      // Retrieved sources
	TurnCount   int          `json:"turn_count"`             // Number of turns in session
	ContentHash string       `json:"content_hash,omitempty"` // SHA-256 hash of answer
	ChainHash   string       `json:"chain_hash,omitempty"`   // Final hash from streaming chain
}

// StreamEvent represents a single SSE event for streaming responses.
//
// # Hash Chain
//
// Each event contains Hash and PrevHash fields for tamper-evident logging.
// Formula: Hash_N = SHA256(Content_N || CreatedAt_N || Hash_{N-1})
//
// This enables detection of:
//   - Missing events (gap in chain)
//   - Modified events (hash mismatch)
//   - Reordered events (prev_hash mismatch)
type StreamEvent struct {
	Id        string       `json:"id"`                   // Event ID (for ordering/deduplication)
	CreatedAt int64        `json:"created_at"`           // Unix timestamp (milliseconds)
	Type      string       `json:"type"`                 // "status", "token", "sources", "done", "error"
	Message   string       `json:"message,omitempty"`    // For status events
	Content   string       `json:"content,omitempty"`    // For token events
	Sources   []SourceInfo `json:"sources,omitempty"`    // For sources event
	SessionId string       `json:"session_id,omitempty"` // For done event
	Error     string       `json:"error,omitempty"`      // For error event
	Hash      string       `json:"hash,omitempty"`       // SHA-256 hash of this event
	PrevHash  string       `json:"prev_hash,omitempty"`  // Hash of previous event in chain
}

// =============================================================================
// ChatRAGRequest Methods
// =============================================================================

// Validate performs validation on a ChatRAGRequest to ensure all required fields
// are present and all provided values are within acceptable bounds.
//
// Validation rules:
//   - Message: Required, cannot be empty. This is the user's input text.
//   - Pipeline: Optional, but if provided must be one of the supported pipelines:
//     "standard", "reranking", "raptor", "graph", "rig", "semantic".
//   - SessionId: Optional, will be generated if not provided (see EnsureSessionId).
//   - Bearing: Optional, used for topic filtering during retrieval.
//   - Stream: Optional, defaults to false.
//   - History: Optional, used to provide conversation context without server-side sessions.
//
// Returns nil if validation passes, or an error describing the first validation
// failure encountered. Callers should check the error before proceeding with
// request processing.
//
// Example:
//
//	req := &ChatRAGRequest{Message: "What is authentication?"}
//	if err := req.Validate(); err != nil {
//	    return fmt.Errorf("invalid request: %w", err)
//	}
func (r *ChatRAGRequest) Validate() error {
	if r.Message == "" {
		return fmt.Errorf("message is required")
	}

	// Pipeline validation: if specified, must be a known pipeline type
	if r.Pipeline != "" {
		validPipelines := map[string]bool{
			"standard":  true,
			"reranking": true,
			"raptor":    true,
			"graph":     true,
			"rig":       true,
			"semantic":  true,
			"verified":  true, // Skeptic/optimist debate for hallucination prevention
		}
		if !validPipelines[r.Pipeline] {
			return fmt.Errorf("invalid pipeline '%s': must be one of standard, reranking, raptor, graph, rig, semantic, verified", r.Pipeline)
		}
	}

	return nil
}

// EnsureDefaults populates default values for optional fields that were not
// provided by the client. This method should be called after binding the JSON
// request but before validation.
//
// Fields populated:
//   - Id: Generated UUID if empty. Used for request tracing and logging.
//   - CreatedAt: Current Unix timestamp if zero. Records when request was received.
//   - Pipeline: Defaults to "reranking" if empty. This is the recommended pipeline
//     for most use cases as it provides good accuracy with reasonable latency.
//
// This method modifies the receiver in place and is idempotent - calling it
// multiple times will not change already-set values.
//
// Example:
//
//	req := &ChatRAGRequest{Message: "Hello"}
//	req.EnsureDefaults()
//	// req.Id is now set to a UUID
//	// req.CreatedAt is now set to current timestamp
//	// req.Pipeline is now "reranking"
func (r *ChatRAGRequest) EnsureDefaults() {
	if r.Id == "" {
		r.Id = generateUUID()
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().UnixMilli()
	}
	if r.Pipeline == "" {
		r.Pipeline = "reranking"
	}
}

// EnsureSessionId generates a new session ID if one was not provided in the
// request, and returns the session ID (whether newly generated or existing).
//
// Session IDs are UUIDs that identify a conversation across multiple requests.
// They are used to:
//   - Store and retrieve conversation history from Weaviate
//   - Group related turns together for context management
//   - Enable session resume functionality in the CLI
//
// This method modifies the receiver's SessionId field in place if it was empty.
//
// Returns the session ID string, which is guaranteed to be non-empty after
// this method returns.
//
// Example:
//
//	req := &ChatRAGRequest{Message: "Hello"}
//	sessionId := req.EnsureSessionId()
//	// sessionId is now a valid UUID
//	// req.SessionId is also set to the same value
func (r *ChatRAGRequest) EnsureSessionId() string {
	if r.SessionId == "" {
		r.SessionId = generateUUID()
	}
	return r.SessionId
}

// =============================================================================
// ChatRAGResponse Constructor
// =============================================================================

// NewChatRAGResponse creates a new ChatRAGResponse with auto-generated ID and
// timestamp. This constructor ensures that all responses have consistent
// identification for logging, tracing, and potential database storage.
//
// Parameters:
//   - answer: The LLM-generated response text to the user's query. This is the
//     main content that will be displayed to the user.
//   - sessionId: The session ID this response belongs to. Should match the
//     session ID from the corresponding ChatRAGRequest.
//   - sources: Slice of SourceInfo structs representing the retrieved documents
//     that were used to generate the answer. May be nil or empty if no sources
//     were used (e.g., for greetings or when RAG retrieval found nothing).
//   - turnCount: The total number of conversation turns including this one.
//     Used by the CLI to display conversation length and manage context.
//
// Returns a pointer to a new ChatRAGResponse with Id and CreatedAt automatically
// populated.
//
// Example:
//
//	sources := []SourceInfo{{Source: "auth.go", Score: 0.95}}
//	resp := NewChatRAGResponse(
//	    "Authentication uses JWT tokens...",
//	    "sess_abc123",
//	    sources,
//	    5,
//	)
func NewChatRAGResponse(answer, sessionId string, sources []SourceInfo, turnCount int) *ChatRAGResponse {
	return &ChatRAGResponse{
		Id:        generateUUID(),
		CreatedAt: time.Now().UnixMilli(),
		Answer:    answer,
		SessionId: sessionId,
		Sources:   sources,
		TurnCount: turnCount,
	}
}

// =============================================================================
// SourceInfo Constructor
// =============================================================================

// NewSourceInfo creates a new SourceInfo with auto-generated ID and timestamp.
//
// # Description
//
// Creates a SourceInfo for a retrieved document source. The ID and timestamp
// are automatically populated for database storage and tracing.
//
// # Inputs
//
//   - source: Document name, path, or URL identifying the source.
//
// # Outputs
//
//   - *SourceInfo: Ready for use with Score or Distance set separately.
//
// # Examples
//
//	src := NewSourceInfo("auth.go").WithScore(0.95)
//	src := NewSourceInfo("api.md").WithDistance(0.123)
func NewSourceInfo(source string) *SourceInfo {
	return &SourceInfo{
		Id:        generateUUID(),
		CreatedAt: time.Now().UnixMilli(),
		Source:    source,
	}
}

// WithScore sets the relevance score on a SourceInfo and returns it for chaining.
func (s *SourceInfo) WithScore(score float64) *SourceInfo {
	s.Score = score
	return s
}

// WithDistance sets the vector distance on a SourceInfo and returns it for chaining.
func (s *SourceInfo) WithDistance(distance float64) *SourceInfo {
	s.Distance = distance
	return s
}

// =============================================================================
// HistoryTurn Constructor
// =============================================================================

// NewHistoryTurn creates a new HistoryTurn with auto-generated ID and timestamp.
//
// # Description
//
// Creates a HistoryTurn capturing a question-answer exchange. The ID and
// timestamp are automatically populated for database storage.
//
// # Inputs
//
//   - question: The user's input message.
//   - answer: The assistant's response.
//
// # Outputs
//
//   - *HistoryTurn: Ready for storage or transmission.
//
// # Examples
//
//	turn := NewHistoryTurn("What is OAuth?", "OAuth is an authorization framework...")
func NewHistoryTurn(question, answer string) *HistoryTurn {
	return &HistoryTurn{
		Id:        generateUUID(),
		CreatedAt: time.Now().UnixMilli(),
		Question:  question,
		Answer:    answer,
	}
}

// =============================================================================
// StreamEvent Constructor and Builder Methods
// =============================================================================

// NewStreamEvent creates a new StreamEvent of the specified type with
// auto-generated ID and timestamp. Use the builder methods (WithMessage,
// WithContent, etc.) to populate type-specific fields.
//
// Supported event types:
//   - "status": Progress updates (e.g., "Searching knowledge base...")
//   - "token": Individual tokens for streaming LLM output
//   - "sources": Retrieved source documents after RAG retrieval
//   - "done": Signals end of stream, includes final session ID
//   - "error": Error occurred during processing
//
// Example:
//
//	event := NewStreamEvent("status").WithMessage("Searching knowledge base...")
//	event := NewStreamEvent("token").WithContent("The")
//	event := NewStreamEvent("error").WithError("Connection timeout")
func NewStreamEvent(eventType string) *StreamEvent {
	return &StreamEvent{
		Id:        generateUUID(),
		CreatedAt: time.Now().UnixMilli(),
		Type:      eventType,
	}
}

// WithMessage sets the Message field on a StreamEvent and returns the event
// for method chaining. This is typically used with "status" type events to
// communicate progress to the client.
//
// Example:
//
//	event := NewStreamEvent("status").WithMessage("Found 8 relevant chunks")
func (e *StreamEvent) WithMessage(msg string) *StreamEvent {
	e.Message = msg
	return e
}

// WithContent sets the Content field on a StreamEvent and returns the event
// for method chaining. This is used with "token" type events to stream
// individual tokens from the LLM response.
//
// Example:
//
//	event := NewStreamEvent("token").WithContent("authentication")
func (e *StreamEvent) WithContent(content string) *StreamEvent {
	e.Content = content
	return e
}

// WithSources sets the Sources field on a StreamEvent and returns the event
// for method chaining. This is used with "sources" type events to send the
// retrieved document sources to the client after RAG retrieval completes.
//
// Example:
//
//	sources := []SourceInfo{{Source: "auth.go", Score: 0.95}}
//	event := NewStreamEvent("sources").WithSources(sources)
func (e *StreamEvent) WithSources(sources []SourceInfo) *StreamEvent {
	e.Sources = sources
	return e
}

// WithSessionId sets the SessionId field on a StreamEvent and returns the
// event for method chaining. This is typically used with "done" type events
// to communicate the final session ID to the client for potential resume.
//
// Example:
//
//	event := NewStreamEvent("done").WithSessionId("sess_abc123")
func (e *StreamEvent) WithSessionId(sessionId string) *StreamEvent {
	e.SessionId = sessionId
	return e
}

// WithError sets the Error field on a StreamEvent and returns the event for
// method chaining. This is used with "error" type events to communicate
// error details to the client.
//
// Example:
//
//	event := NewStreamEvent("error").WithError("RAG engine unavailable")
func (e *StreamEvent) WithError(err string) *StreamEvent {
	e.Error = err
	return e
}

// =============================================================================
// Verified Pipeline Progress Streaming Types (P6)
// =============================================================================

// ProgressEventType defines the discrete stages of the skeptic/optimist debate.
//
// # Description
//
// These event types are emitted during the verified RAG pipeline execution
// and streamed to clients for real-time feedback. Each type corresponds to
// a specific stage of the debate pattern.
//
// # Values
//
//   - RetrievalStart: Document retrieval has begun
//   - RetrievalComplete: Documents retrieved, includes count and sources
//   - DraftStart: Optimist is generating initial answer
//   - DraftComplete: Initial draft ready for skeptic review
//   - SkepticAuditStart: Skeptic is analyzing the draft
//   - SkepticAuditComplete: Skeptic has rendered verdict
//   - RefinementStart: Refiner is correcting hallucinations
//   - RefinementComplete: Refined answer ready for re-audit
//   - VerificationComplete: Final answer verified, debate concluded
//   - Error: An error occurred during pipeline execution
//
// # Examples
//
//	if event.EventType == ProgressEventTypeSkepticAuditComplete {
//	    if event.AuditDetails != nil && !event.AuditDetails.IsVerified {
//	        fmt.Printf("Found %d hallucinations\n", len(event.AuditDetails.Hallucinations))
//	    }
//	}
type ProgressEventType string

const (
	ProgressEventTypeRetrievalStart       ProgressEventType = "retrieval_start"
	ProgressEventTypeRetrievalComplete    ProgressEventType = "retrieval_complete"
	ProgressEventTypeDraftStart           ProgressEventType = "draft_start"
	ProgressEventTypeDraftComplete        ProgressEventType = "draft_complete"
	ProgressEventTypeSkepticAuditStart    ProgressEventType = "skeptic_audit_start"
	ProgressEventTypeSkepticAuditComplete ProgressEventType = "skeptic_audit_complete"
	ProgressEventTypeRefinementStart      ProgressEventType = "refinement_start"
	ProgressEventTypeRefinementComplete   ProgressEventType = "refinement_complete"
	ProgressEventTypeVerificationComplete ProgressEventType = "verification_complete"
	ProgressEventTypeError                ProgressEventType = "error"
)

// SkepticAuditDetails contains detailed skeptic audit results.
//
// # Description
//
// This struct is populated during SKEPTIC_AUDIT_COMPLETE events at verbosity
// level 2. It provides granular information about what the skeptic found,
// including specific hallucinations and missing evidence.
//
// # Fields
//
//   - IsVerified: True if all claims are supported by evidence.
//   - Reasoning: The skeptic's explanation of the verdict.
//   - Hallucinations: List of specific unsupported claims.
//   - MissingEvidence: List of facts needing evidence.
//   - SourcesCited: Indices of sources referenced (0-based).
//
// # Examples
//
//	if !details.IsVerified {
//	    for _, h := range details.Hallucinations {
//	        fmt.Printf("  Unsupported: %s\n", h)
//	    }
//	}
type SkepticAuditDetails struct {
	IsVerified      bool     `json:"is_verified"`
	Reasoning       string   `json:"reasoning"`
	Hallucinations  []string `json:"hallucinations,omitempty"`
	MissingEvidence []string `json:"missing_evidence,omitempty"`
	SourcesCited    []int    `json:"sources_cited,omitempty"`
}

// RetrievalDetails contains document retrieval information.
//
// # Description
//
// This struct is populated during RETRIEVAL_COMPLETE events at verbosity
// level 2. It provides information about the documents retrieved for the
// query before the skeptic/optimist debate begins.
//
// # Fields
//
//   - DocumentCount: Number of documents retrieved.
//   - Sources: List of source identifiers (filenames, URLs, etc.).
//   - HasRelevantDocs: Whether relevant documents were found.
//
// # Examples
//
//	if details.HasRelevantDocs {
//	    fmt.Printf("Found %d documents: %v\n", details.DocumentCount, details.Sources)
//	}
type RetrievalDetails struct {
	DocumentCount   int      `json:"document_count"`
	Sources         []string `json:"sources,omitempty"`
	HasRelevantDocs bool     `json:"has_relevant_docs"`
}

// ProgressEvent represents a single progress update during verified pipeline execution.
//
// # Description
//
// Progress events are streamed via SSE to provide real-time feedback during
// the skeptic/optimist debate. Each event has a type, message, and optional
// detailed information depending on the event type and verbosity level.
//
// # Fields
//
//   - EventType: The type of progress event (from ProgressEventType enum).
//   - Message: Human-readable summary message (always present).
//   - Timestamp: ISO 8601 timestamp when the event occurred.
//   - Attempt: Current verification attempt (1-indexed, max 3).
//   - TraceID: OpenTelemetry trace ID for debugging (verbosity >= 2).
//   - RetrievalDetails: Details about retrieval (RETRIEVAL_COMPLETE only).
//   - AuditDetails: Details about skeptic audit (SKEPTIC_AUDIT_COMPLETE only).
//   - ErrorMessage: Error description (ERROR event type only).
//
// # Verbosity Levels
//
// The CLI uses verbosity to control what is displayed:
//   - Level 0: Silent (no progress shown)
//   - Level 1: Summary (shows Message only)
//   - Level 2: Detailed (shows Message + details + trace link)
//
// # Examples
//
//	// Verbosity 1 display
//	fmt.Printf("[%s] %s\n", event.EventType, event.Message)
//
//	// Verbosity 2 display with OTel link
//	if event.TraceID != "" {
//	    fmt.Printf("  View trace: http://localhost:16686/trace/%s\n", event.TraceID)
//	}
type ProgressEvent struct {
	EventType        ProgressEventType    `json:"event_type"`
	Message          string               `json:"message"`
	Timestamp        string               `json:"timestamp,omitempty"`
	Attempt          int                  `json:"attempt,omitempty"`
	TraceID          string               `json:"trace_id,omitempty"`
	RetrievalDetails *RetrievalDetails    `json:"retrieval_details,omitempty"`
	AuditDetails     *SkepticAuditDetails `json:"audit_details,omitempty"`
	ErrorMessage     string               `json:"error_message,omitempty"`
}

// VerifiedStreamAnswer is the final answer event from verified streaming.
//
// # Description
//
// This struct is sent in the "answer" SSE event after the verification
// process completes. It contains the final verified (or unverified) answer
// and the sources used.
//
// # Fields
//
//   - Answer: The final answer text (may include verification warning).
//   - Sources: List of sources used to generate the answer.
//   - IsVerified: True if the answer passed skeptic verification.
//
// # Examples
//
//	if !answer.IsVerified {
//	    fmt.Println("Warning: Some claims could not be verified")
//	}
type VerifiedStreamAnswer struct {
	Answer     string           `json:"answer"`
	Sources    []map[string]any `json:"sources,omitempty"`
	IsVerified bool             `json:"is_verified"`
}

// VerifiedStreamError represents an error during verified streaming.
//
// # Description
//
// This struct is sent in the "error" SSE event if the pipeline fails.
// The stream will close after this event.
//
// # Fields
//
//   - Error: Description of what went wrong.
type VerifiedStreamError struct {
	Error string `json:"error"`
}

// =============================================================================
// Helper Functions
// =============================================================================

// generateUUID creates a new UUID-like string for use as identifiers.
//
// This function generates a unique identifier based on the current timestamp
// with nanosecond precision, formatted to resemble a UUID. While not a true
// RFC 4122 UUID, it provides sufficient uniqueness for request/response
// identification within a single system.
//
// The format is: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
//
// Note: For production systems requiring cryptographically secure or globally
// unique identifiers, consider using github.com/google/uuid instead. This
// implementation avoids the import to prevent potential circular dependencies
// in the datatypes package.
//
// Returns a string in UUID format that is unique within the current process.
func generateUUID() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		now&0xFFFFFFFF,
		(now>>32)&0xFFFF,
		(now>>48)&0xFFFF|0x4000, // Version 4
		(now>>32)&0x3FFF|0x8000, // Variant
		now&0xFFFFFFFFFFFF)
}
