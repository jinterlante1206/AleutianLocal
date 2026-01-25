// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package llm provides interfaces and implementations for LLM backends.
//
// This package defines the LLMClient interface for interacting with
// language models (Anthropic Claude, OpenAI, local models, etc.) and
// provides streaming support for real-time token generation.
//
// # Architecture
//
// The package follows the interface-first pattern:
//   - LLMClient interface defines the contract
//   - AnthropicClient implements for Claude models
//   - Additional implementations can be added for other backends
//
// # Streaming
//
// Streaming is implemented via callback pattern. The ChatStream method
// calls a callback for each token as it's generated, enabling real-time
// display in CLI and SSE endpoints.
//
// # Thread Safety
//
// All implementations must be safe for concurrent use.
package llm

import (
	"context"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
)

// =============================================================================
// Generation Parameters
// =============================================================================

// GenerationParams holds parameters for LLM generation.
//
// # Description
//
// Contains all configurable parameters for text generation including
// temperature, sampling parameters, and tool definitions. These parameters
// control the LLM's output behavior.
//
// # Fields
//
//   - Temperature: Sampling temperature (0.0-1.0). Lower = more deterministic.
//     nil uses the model's default.
//   - TopK: Sample from top K tokens. nil uses model default.
//   - TopP: Nucleus sampling threshold. nil uses model default.
//   - MaxTokens: Maximum tokens to generate. nil uses model default.
//   - Stop: Stop sequences to halt generation. Empty means no custom stops.
//   - ToolDefinitions: Tool schemas for function calling (Claude tools format).
//   - EnableThinking: Enable Claude extended thinking mode. Only works with
//     Claude models that support extended thinking.
//   - BudgetTokens: Token budget for thinking (max 65536). Only used when
//     EnableThinking is true.
//
// # Examples
//
//	// Default parameters
//	params := GenerationParams{}
//
//	// Custom temperature
//	temp := float32(0.7)
//	params := GenerationParams{Temperature: &temp}
//
//	// Extended thinking
//	params := GenerationParams{EnableThinking: true, BudgetTokens: 4096}
//
// # Limitations
//
//   - Not all parameters are supported by all backends
//   - EnableThinking only works with Claude models
//
// # Assumptions
//
//   - nil values mean "use model default"
type GenerationParams struct {
	Temperature     *float32      `json:"temperature"`
	TopK            *int          `json:"top_k"`
	TopP            *float32      `json:"top_p"`
	MaxTokens       *int          `json:"max_tokens"`
	Stop            []string      `json:"stop"`
	ToolDefinitions []interface{} `json:"tools,omitempty"`
	EnableThinking  bool          `json:"thinking,omitempty"`
	BudgetTokens    int           `json:"budget_tokens,omitempty"`
}

// =============================================================================
// Streaming Types
// =============================================================================

// StreamEventType represents the type of streaming event.
//
// # Description
//
// StreamEventType categorizes streaming events to allow handlers to
// process different event types appropriately. Token events contain
// generated text, thinking events contain reasoning, and error events
// signal problems during generation.
type StreamEventType string

const (
	// StreamEventToken indicates a content token event.
	// The Content field contains the generated text fragment.
	StreamEventToken StreamEventType = "token"

	// StreamEventThinking indicates a thinking/reasoning token event.
	// The Content field contains Claude's reasoning text.
	// Only emitted when EnableThinking is true.
	StreamEventThinking StreamEventType = "thinking"

	// StreamEventError indicates an error occurred during streaming.
	// The Error field contains the error message.
	// Streaming typically stops after an error event.
	StreamEventError StreamEventType = "error"
)

// StreamEvent represents a single event during LLM streaming.
//
// # Description
//
// StreamEvent is emitted by ChatStream for each token or event during
// generation. The Type field indicates what kind of event this is and
// which fields are populated.
//
// # Fields
//
//   - Type: Event type (token, thinking, error).
//   - Content: Token content. Populated for token and thinking events.
//   - Error: Error message. Populated for error events.
//
// # Examples
//
//	// Token event
//	StreamEvent{Type: StreamEventToken, Content: "Hello"}
//
//	// Thinking event
//	StreamEvent{Type: StreamEventThinking, Content: "Let me analyze..."}
//
//	// Error event
//	StreamEvent{Type: StreamEventError, Error: "Connection reset"}
//
// # Limitations
//
//   - Only one of Content or Error is populated per event
//
// # Assumptions
//
//   - Events are delivered in generation order
type StreamEvent struct {
	Type    StreamEventType
	Content string
	Error   string
}

// StreamCallback is called for each event during streaming.
//
// # Description
//
// StreamCallback receives events as they are generated by the LLM.
// Return an error to abort streaming (e.g., on client disconnect).
// The callback should process events quickly to avoid backpressure.
//
// # Inputs
//
//   - event: The streaming event (token, thinking, or error).
//
// # Outputs
//
//   - error: Non-nil to abort streaming. The ChatStream method will
//     return this error after cleanup.
//
// # Examples
//
//	callback := func(event StreamEvent) error {
//	    switch event.Type {
//	    case StreamEventToken:
//	        fmt.Print(event.Content)
//	    case StreamEventThinking:
//	        fmt.Printf("[thinking] %s", event.Content)
//	    case StreamEventError:
//	        return fmt.Errorf("stream error: %s", event.Error)
//	    }
//	    return nil
//	}
//
// # Limitations
//
//   - Must handle events quickly to avoid backpressure
//   - Errors abort the stream immediately
//
// # Assumptions
//
//   - Called in event order (tokens arrive in generation order)
//   - Called from a single goroutine (no concurrent calls)
type StreamCallback func(event StreamEvent) error

// =============================================================================
// Interface Definition
// =============================================================================

// LLMClient defines the standard interface for any LLM backend.
//
// # Description
//
// LLMClient abstracts LLM interactions, enabling different backends
// (Anthropic Claude, OpenAI, local models) to be used interchangeably.
// The interface provides both blocking and streaming methods.
//
// # Methods
//
//   - Generate: Single-prompt completion (legacy, prefer Chat)
//   - Chat: Blocking conversation with full response
//   - ChatStream: Streaming conversation with token-by-token callbacks
//
// # Thread Safety
//
// Implementations must be safe for concurrent use. Multiple goroutines
// may call methods simultaneously.
//
// # Limitations
//
//   - Not all backends support all features (e.g., extended thinking)
//   - Streaming requires backend support
//
// # Assumptions
//
//   - Backend is configured and authenticated before use
//   - Context cancellation is respected
type LLMClient interface {
	// Generate produces text from a single prompt.
	//
	// # Description
	//
	// Sends a prompt to the LLM and returns the generated text.
	// This is a simple completion API without conversation context.
	// Prefer Chat for conversational interactions.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout. When cancelled,
	//     the method returns with context.Canceled error.
	//   - prompt: Text prompt to complete. Must not be empty.
	//   - params: Generation parameters. Use empty struct for defaults.
	//
	// # Outputs
	//
	//   - string: Generated text response.
	//   - error: Non-nil on network failure, API error, or cancellation.
	//
	// # Examples
	//
	//	response, err := client.Generate(ctx, "Explain OAuth in one sentence", GenerationParams{})
	//
	// # Limitations
	//
	//   - No conversation context (stateless)
	//   - Some backends may not support this method
	//
	// # Assumptions
	//
	//   - Prompt is within model's context window
	Generate(ctx context.Context, prompt string, params GenerationParams) (string, error)

	// Chat conducts a conversation with message history.
	//
	// # Description
	//
	// Sends a conversation (system, user, assistant messages) to the LLM
	// and returns the assistant's response. This is a blocking call that
	// waits for the complete response.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - messages: Conversation history. Must have at least one message.
	//     Messages should alternate user/assistant with optional system.
	//   - params: Generation parameters.
	//
	// # Outputs
	//
	//   - string: Assistant's complete response.
	//   - error: Non-nil on failure.
	//
	// # Examples
	//
	//	messages := []datatypes.Message{
	//	    {Role: "system", Content: "You are helpful."},
	//	    {Role: "user", Content: "What is 2+2?"},
	//	}
	//	response, err := client.Chat(ctx, messages, GenerationParams{})
	//
	// # Limitations
	//
	//   - Blocks until complete response received
	//   - No partial results on timeout
	//
	// # Assumptions
	//
	//   - Messages are well-formed with valid roles
	//   - Total tokens (input + output) within context window
	Chat(ctx context.Context, messages []datatypes.Message, params GenerationParams) (string, error)

	// ChatStream conducts a conversation with streaming response.
	//
	// # Description
	//
	// Like Chat, but streams the response token-by-token via callback.
	// Enables real-time display of generation progress. The callback
	// is called for each token as it's generated.
	//
	// If an error occurs during streaming, the callback is called with
	// a StreamEventError event before the method returns. This allows
	// callers to see partial results before the error.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout. Cancellation stops
	//     streaming and returns context.Canceled.
	//   - messages: Conversation history.
	//   - params: Generation parameters.
	//   - callback: Called for each streaming event. Return error to abort.
	//
	// # Outputs
	//
	//   - error: Non-nil on failure or if callback returns error.
	//     If streaming error occurs, callback receives error event first.
	//
	// # Examples
	//
	//	var fullResponse strings.Builder
	//	err := client.ChatStream(ctx, messages, params, func(e StreamEvent) error {
	//	    switch e.Type {
	//	    case StreamEventToken:
	//	        fullResponse.WriteString(e.Content)
	//	        fmt.Print(e.Content)  // Real-time display
	//	    case StreamEventThinking:
	//	        fmt.Printf("[thinking] %s", e.Content)
	//	    case StreamEventError:
	//	        fmt.Printf("[error] %s", e.Error)
	//	    }
	//	    return nil
	//	})
	//
	// # Limitations
	//
	//   - Callback errors abort the stream
	//   - No automatic retry on transient failures
	//
	// # Assumptions
	//
	//   - Backend supports streaming API
	//   - Callback handles events quickly
	ChatStream(ctx context.Context, messages []datatypes.Message, params GenerationParams, callback StreamCallback) error
}
