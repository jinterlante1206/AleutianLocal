// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package datatypes provides data structures for the orchestrator service.
//
// This file contains request and response types for direct chat endpoints
// (non-RAG LLM chat). For RAG chat types, see rag.go.
package datatypes

import (
	"time"

	"github.com/go-playground/validator/v10"
)

// =============================================================================
// Constants for Security Compliance
// =============================================================================

const (
	// MaxMessageContentBytes is the maximum size of a single message content.
	// Per SEC-003: Unbounded message input mitigation.
	MaxMessageContentBytes = 32 * 1024 // 32KB

	// MaxMessagesPerRequest is the maximum number of messages in a request.
	// Per SEC-004: Unbounded message history mitigation.
	MaxMessagesPerRequest = 100

	// MaxBudgetTokens is the maximum token budget for thinking mode.
	MaxBudgetTokens = 65536
)

// =============================================================================
// Shared Validator Instance
// =============================================================================

// chatValidate is the validator instance for chat datatypes.
// Initialized in init() with custom validators.
var chatValidate *validator.Validate

func init() {
	chatValidate = validator.New()

	// Register custom validator for message content size (SEC-003)
	_ = chatValidate.RegisterValidation("maxbytes", validateMaxBytes)
}

// validateMaxBytes validates that a string field does not exceed MaxMessageContentBytes.
//
// # Description
//
// Custom validator to enforce SEC-003 message size limits. Checks byte length
// (not rune count) to prevent memory exhaustion attacks with large payloads.
//
// # Inputs
//
//   - fl: Validator field level containing the string to validate
//
// # Outputs
//
//   - bool: true if content <= 32KB, false otherwise
//
// # Security References
//
//   - SEC-003: Unbounded message input (security_architecture_review.md)
func validateMaxBytes(fl validator.FieldLevel) bool {
	content := fl.Field().String()
	return len(content) <= MaxMessageContentBytes
}

// =============================================================================
// Direct Chat Request Types
// =============================================================================

// DirectChatRequest represents a direct LLM chat request body.
//
// # Description
//
// DirectChatRequest contains the messages and optional parameters for direct
// LLM chat (without RAG retrieval). This is used for the POST /v1/chat/direct
// endpoint. Every request includes a unique ID and timestamp for audit trails
// and database storage.
//
// # Fields
//
//   - RequestID: Required. Unique identifier for this request (UUID v4).
//     Used for tracing, audit logging, and database correlation.
//   - Timestamp: Required. Unix timestamp in milliseconds (UTC) when request was created.
//     Used for audit trails, ordering, and retention policy enforcement.
//   - Messages: Required. Conversation history with 1-100 messages.
//     Each message must have a Role ("user", "assistant", "system") and Content.
//     Content is limited to 32KB per message (SEC-003 compliance).
//   - EnableThinking: Optional. Enable Claude extended thinking mode.
//     When true, Claude will show its reasoning process.
//   - BudgetTokens: Optional. Token budget for thinking mode (0-65536).
//     Only used when EnableThinking is true. Default: 2048 if not specified.
//   - Tools: Optional. Tool definitions for function calling.
//     Allows the LLM to call defined functions.
//
// # Validation
//
// Uses go-playground/validator:
//   - RequestID: required, must be valid UUID v4
//   - Timestamp: required, must be > 0
//   - Messages: required, 1-100 elements, each element validated
//   - Messages[].Content: max 32768 bytes (32KB) per SEC-003
//   - BudgetTokens: must be 0-65536
//
// # Examples
//
//	// Simple chat
//	req := DirectChatRequest{
//	    RequestID: "550e8400-e29b-41d4-a716-446655440000",
//	    Timestamp: time.Now().UnixMilli(),
//	    Messages: []Message{
//	        {Role: "user", Content: "Hello"},
//	    },
//	}
//
// # Limitations
//
//   - No streaming support in this request type (use stream endpoint)
//   - Tools feature requires compatible LLM backend
//   - Message content limited to 32KB (larger payloads rejected)
//   - Maximum 100 messages per request (history truncation may be needed)
//
// # Assumptions
//
//   - At least one message is provided
//   - Messages are in chronological order
//   - RequestID is generated client-side (UUID v4)
//   - Timestamp is Unix UTC timestamp in milliseconds
//
// # Security References
//
//   - SEC-003: Message size limits (security_architecture_review.md)
//   - SEC-005: Error message sanitization (security_architecture_review.md)
type DirectChatRequest struct {
	RequestID      string        `json:"request_id" validate:"required,uuid4"`
	Timestamp      int64         `json:"timestamp" validate:"required,gt=0"`
	Messages       []Message     `json:"messages" validate:"required,min=1,max=100,dive"`
	EnableThinking bool          `json:"enable_thinking"`
	BudgetTokens   int           `json:"budget_tokens" validate:"gte=0,lte=65536"`
	Tools          []interface{} `json:"tools,omitempty"`
}

// Validate validates the DirectChatRequest fields.
//
// # Description
//
// Performs validation using go-playground/validator tags and custom validators.
// This method should be called after binding the JSON request.
//
// # Outputs
//
//   - error: Non-nil if validation failed, with details about which field
//
// # Examples
//
//	if err := req.Validate(); err != nil {
//	    return fmt.Errorf("invalid request: %w", err)
//	}
func (r *DirectChatRequest) Validate() error {
	return chatValidate.Struct(r)
}

// EnsureDefaults populates default values for optional fields.
//
// # Description
//
// Generates RequestID and Timestamp if not provided by the client.
// This ensures all requests have proper identifiers for tracing and auditing.
//
// # Examples
//
//	req := &DirectChatRequest{Messages: messages}
//	req.EnsureDefaults()
//	// req.RequestID is now a UUID
//	// req.Timestamp is now a Unix timestamp
func (r *DirectChatRequest) EnsureDefaults() {
	if r.RequestID == "" {
		r.RequestID = generateUUID()
	}
	if r.Timestamp == 0 {
		r.Timestamp = time.Now().UnixMilli()
	}
}

// =============================================================================
// Direct Chat Response Types
// =============================================================================

// DirectChatResponse represents the response from a direct chat request.
//
// # Description
//
// Contains the LLM's generated response. For simple responses, only the Answer
// field is populated. Extended thinking responses may include additional metadata.
// Every response includes a unique ID and timestamp for audit trails and database
// storage.
//
// # Fields
//
//   - ResponseID: Unique identifier for this response (UUID v4).
//     Generated server-side. Used for audit logging and database correlation.
//   - RequestID: Echo of the request ID for correlation.
//     Enables request-response matching in logs and databases.
//   - Timestamp: Unix timestamp in milliseconds (UTC) when response was generated.
//     Used for audit trails, latency calculation, and retention policies.
//   - Answer: The LLM's generated response text.
//   - ThinkingContent: Optional. The reasoning process if EnableThinking was true.
//   - Usage: Optional. Token usage statistics.
//   - ProcessingTimeMs: Time taken to process the request in milliseconds.
//
// # Examples
//
//	Response JSON:
//	{
//	    "response_id": "660f9500-f39c-52e5-b827-557766551111",
//	    "request_id": "550e8400-e29b-41d4-a716-446655440000",
//	    "timestamp": 1735817400000,
//	    "answer": "Hello! How can I help you today?",
//	    "processing_time_ms": 1250
//	}
//
// # Limitations
//
//   - ThinkingContent only populated for Claude with extended thinking
//
// # Database Schema Alignment
//
//   - ResponseID: Primary key for responses table
//   - RequestID: Foreign key to requests table
//   - Timestamp: Indexed for time-range queries and retention
type DirectChatResponse struct {
	ResponseID       string      `json:"response_id"`
	RequestID        string      `json:"request_id"`
	Timestamp        int64       `json:"timestamp"`
	Answer           string      `json:"answer"`
	ThinkingContent  string      `json:"thinking,omitempty"`
	Usage            *TokenUsage `json:"usage,omitempty"`
	ProcessingTimeMs int64       `json:"processing_time_ms,omitempty"`
}

// NewDirectChatResponse creates a new DirectChatResponse with auto-generated
// ID and timestamp.
//
// # Description
//
// This constructor ensures that all responses have consistent identification
// for logging, tracing, and potential database storage.
//
// # Inputs
//
//   - requestID: The request ID to echo back for correlation
//   - answer: The LLM-generated response text
//
// # Outputs
//
//   - *DirectChatResponse: A new response with ResponseID and Timestamp set
//
// # Examples
//
//	resp := NewDirectChatResponse("req-uuid", "Hello! How can I help?")
func NewDirectChatResponse(requestID, answer string) *DirectChatResponse {
	return &DirectChatResponse{
		ResponseID: generateUUID(),
		RequestID:  requestID,
		Timestamp:  time.Now().UnixMilli(),
		Answer:     answer,
	}
}

// =============================================================================
// Token Usage Types
// =============================================================================

// TokenUsage contains token consumption statistics.
//
// # Description
//
// Tracks input and output token counts for billing and monitoring.
//
// # Fields
//
//   - InputTokens: Number of tokens in the prompt/messages
//   - OutputTokens: Number of tokens in the response
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
