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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
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
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

type RAGRequest struct {
	Query     string `json:"query"`
	SessionId string `json:"session_id"`
	Pipeline  string `json:"pipeline"`
	NoRag     bool   `json:"no_rag"`
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
	Id        string  `json:"id,omitempty"`
	CreatedAt int64   `json:"created_at,omitempty"`
	Source    string  `json:"source"`
	Distance  float64 `json:"distance,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Hash      string  `json:"hash,omitempty"`
}

type RAGResponse struct {
	Answer    string       `json:"answer"`
	SessionId string       `json:"session_id"`
	Sources   []SourceInfo `json:"sources,omitempty"`
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

	// Use the correct request struct: {"texts": ["..."]}
	embReq := embeddingServiceRequest{Texts: []string{text}}
	reqBody, err := json.Marshal(embReq)
	if err != nil {
		return fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	// This part is unchanged
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

	// Use the correct response struct to parse: {"vectors": [[...]]}
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
	e.Timestamp = int(time.Now().Unix()) // Use current time
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
		body, _ := io.ReadAll(resp.Body)
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
type ChatRAGRequest struct {
	Id          string     `json:"id,omitempty"`           // Request ID (server-generated for tracing)
	CreatedAt   int64      `json:"created_at,omitempty"`   // Unix timestamp when request was created
	Message     string     `json:"message"`                // Current user message
	SessionId   string     `json:"session_id,omitempty"`   // Optional: resume session
	Pipeline    string     `json:"pipeline,omitempty"`     // RAG pipeline (default: reranking)
	Bearing     string     `json:"bearing,omitempty"`      // Topic filter for retrieval
	Stream      bool       `json:"stream,omitempty"`       // Enable SSE streaming
	History     []ChatTurn `json:"history,omitempty"`      // Previous turns (if not using session)
	ContentHash string     `json:"content_hash,omitempty"` // SHA-256 hash of message for integrity
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
		}
		if !validPipelines[r.Pipeline] {
			return fmt.Errorf("invalid pipeline '%s': must be one of standard, reranking, raptor, graph, rig, semantic", r.Pipeline)
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
