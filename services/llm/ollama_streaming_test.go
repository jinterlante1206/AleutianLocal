// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// =============================================================================
// Mock Server Helpers
// =============================================================================

// newMockOllamaServer creates a test server that returns streaming NDJSON.
//
// # Description
//
// Creates an httptest.Server that responds to /api/chat with streaming
// NDJSON responses. The response is controlled by the provided handler.
//
// # Inputs
//
//   - handler: Function to generate response for each request.
//
// # Outputs
//
//   - *httptest.Server: Test server. Caller must call Close().
//
// # Examples
//
//	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
//	    w.Write([]byte(`{"message":{"content":"Hi"},"done":false}`))
//	    w.Write([]byte("\n"))
//	    w.Write([]byte(`{"done":true}`))
//	})
//	defer server.Close()
//
// # Limitations
//
//   - Only handles /api/chat endpoint
//
// # Assumptions
//
//   - Handler writes valid NDJSON
func newMockOllamaServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// newTestOllamaClient creates an OllamaClient pointing to a test server.
//
// # Description
//
// Creates an OllamaClient configured to use the given test server URL.
// Used for testing without a real Ollama server.
//
// # Inputs
//
//   - baseURL: Test server URL.
//   - model: Model name to use.
//
// # Outputs
//
//   - *OllamaClient: Configured client.
//
// # Examples
//
//	client := newTestOllamaClient(server.URL, "test-model")
//
// # Limitations
//
//   - Bypasses environment variable configuration
//
// # Assumptions
//
//   - baseURL is accessible
func newTestOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		model:      model,
	}
}

// =============================================================================
// StreamProcessor Tests
// =============================================================================

// TestDefaultStreamProcessor_ProcessChunk_ContentToken tests basic content token processing.
//
// # Description
//
// Verifies that DefaultStreamProcessor correctly processes content tokens
// and emits StreamEventToken events.
func TestDefaultStreamProcessor_ProcessChunk_ContentToken(t *testing.T) {
	t.Parallel()

	cfg := DefaultStreamConfig()
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Message: datatypes.Message{
			Role:    "assistant",
			Content: "Hello",
		},
		Done: false,
	}

	var receivedEvent StreamEvent
	callback := func(event StreamEvent) error {
		receivedEvent = event
		return nil
	}

	done, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}
	if done {
		t.Error("ProcessChunk returned done=true for non-final chunk")
	}
	if receivedEvent.Type != StreamEventToken {
		t.Errorf("Expected StreamEventToken, got %v", receivedEvent.Type)
	}
	if receivedEvent.Content != "Hello" {
		t.Errorf("Expected content 'Hello', got '%s'", receivedEvent.Content)
	}
	if processor.GetTokenCount() != 1 {
		t.Errorf("Expected token count 1, got %d", processor.GetTokenCount())
	}
	if processor.GetResponseLength() != 5 {
		t.Errorf("Expected response length 5, got %d", processor.GetResponseLength())
	}
}

// TestDefaultStreamProcessor_ProcessChunk_ThinkingToken tests thinking token processing.
//
// # Description
//
// Verifies that DefaultStreamProcessor correctly processes thinking tokens
// and emits StreamEventThinking events when not redacted.
func TestDefaultStreamProcessor_ProcessChunk_ThinkingToken(t *testing.T) {
	t.Parallel()

	cfg := StreamConfig{
		RedactThinking:    false,
		MaxThinkingLength: 0,
		MaxResponseLength: 0,
	}
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Thinking: "Let me think about this...",
		Done:     false,
	}

	var receivedEvent StreamEvent
	callback := func(event StreamEvent) error {
		receivedEvent = event
		return nil
	}

	done, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}
	if done {
		t.Error("ProcessChunk returned done=true for non-final chunk")
	}
	if receivedEvent.Type != StreamEventThinking {
		t.Errorf("Expected StreamEventThinking, got %v", receivedEvent.Type)
	}
	if receivedEvent.Content != "Let me think about this..." {
		t.Errorf("Expected thinking content, got '%s'", receivedEvent.Content)
	}
}

// TestDefaultStreamProcessor_ProcessChunk_ThinkingRedacted tests thinking redaction.
//
// # Description
//
// Verifies that thinking tokens are not emitted when RedactThinking is true.
func TestDefaultStreamProcessor_ProcessChunk_ThinkingRedacted(t *testing.T) {
	t.Parallel()

	cfg := StreamConfig{
		RedactThinking: true,
	}
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Thinking: "Secret thinking...",
		Done:     false,
	}

	callbackCalled := false
	callback := func(event StreamEvent) error {
		callbackCalled = true
		return nil
	}

	done, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}
	if done {
		t.Error("ProcessChunk returned done=true for non-final chunk")
	}
	if callbackCalled {
		t.Error("Callback should not be called when thinking is redacted")
	}
}

// TestDefaultStreamProcessor_ProcessChunk_ChunkError tests error handling in chunks.
//
// # Description
//
// Verifies that ProcessChunk correctly handles error fields in chunks
// and emits StreamEventError events.
func TestDefaultStreamProcessor_ProcessChunk_ChunkError(t *testing.T) {
	t.Parallel()

	cfg := DefaultStreamConfig()
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Error: "model not found",
		Done:  false,
	}

	var receivedEvent StreamEvent
	callback := func(event StreamEvent) error {
		receivedEvent = event
		return nil
	}

	done, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err == nil {
		t.Fatal("ProcessChunk should return error for chunk with error field")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("Error should contain 'model not found', got: %v", err)
	}
	if !done {
		t.Error("ProcessChunk should return done=true for error chunks")
	}
	if receivedEvent.Type != StreamEventError {
		t.Errorf("Expected StreamEventError, got %v", receivedEvent.Type)
	}
	if receivedEvent.Error != "model not found" {
		t.Errorf("Expected error 'model not found', got '%s'", receivedEvent.Error)
	}
}

// TestDefaultStreamProcessor_ProcessChunk_DoneFlag tests done flag handling.
//
// # Description
//
// Verifies that ProcessChunk correctly returns done=true when chunk.Done is true.
func TestDefaultStreamProcessor_ProcessChunk_DoneFlag(t *testing.T) {
	t.Parallel()

	cfg := DefaultStreamConfig()
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Done:       true,
		DoneReason: "stop",
	}

	callback := func(event StreamEvent) error {
		return nil
	}

	done, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}
	if !done {
		t.Error("ProcessChunk should return done=true when chunk.Done is true")
	}
}

// TestDefaultStreamProcessor_ProcessChunk_ResponseLengthLimit tests response truncation.
//
// # Description
//
// Verifies that content is truncated when MaxResponseLength is exceeded.
func TestDefaultStreamProcessor_ProcessChunk_ResponseLengthLimit(t *testing.T) {
	t.Parallel()

	cfg := StreamConfig{
		MaxResponseLength: 10,
	}
	processor := NewDefaultStreamProcessor(cfg, nil)

	// First chunk: "Hello" (5 chars)
	chunk1 := &ollamaStreamChunk{
		Message: datatypes.Message{Content: "Hello"},
		Done:    false,
	}

	var events []StreamEvent
	callback := func(event StreamEvent) error {
		events = append(events, event)
		return nil
	}

	_, err := processor.ProcessChunk(context.Background(), chunk1, callback)
	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}

	// Second chunk: " World!" (7 chars, would exceed limit of 10)
	chunk2 := &ollamaStreamChunk{
		Message: datatypes.Message{Content: " World!"},
		Done:    false,
	}

	_, err = processor.ProcessChunk(context.Background(), chunk2, callback)
	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}

	// Should have received two events, second truncated
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if events[0].Content != "Hello" {
		t.Errorf("First event should be 'Hello', got '%s'", events[0].Content)
	}
	// Second should be truncated to fit within 10 total chars
	if events[1].Content != " Worl" {
		t.Errorf("Second event should be ' Worl' (truncated), got '%s'", events[1].Content)
	}
	if processor.GetResponseLength() != 10 {
		t.Errorf("Response length should be 10, got %d", processor.GetResponseLength())
	}
}

// TestDefaultStreamProcessor_ProcessChunk_ThinkingLengthLimit tests thinking truncation.
//
// # Description
//
// Verifies that thinking content is truncated when MaxThinkingLength is exceeded.
func TestDefaultStreamProcessor_ProcessChunk_ThinkingLengthLimit(t *testing.T) {
	t.Parallel()

	cfg := StreamConfig{
		RedactThinking:    false,
		MaxThinkingLength: 10,
	}
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Thinking: "This is a very long thinking content",
		Done:     false,
	}

	var receivedEvent StreamEvent
	callback := func(event StreamEvent) error {
		receivedEvent = event
		return nil
	}

	_, err := processor.ProcessChunk(context.Background(), chunk, callback)
	if err != nil {
		t.Fatalf("ProcessChunk returned error: %v", err)
	}

	if len(receivedEvent.Content) != 10 {
		t.Errorf("Thinking content should be truncated to 10 chars, got %d", len(receivedEvent.Content))
	}
	if receivedEvent.Content != "This is a " {
		t.Errorf("Expected 'This is a ', got '%s'", receivedEvent.Content)
	}
}

// TestDefaultStreamProcessor_ProcessChunk_CallbackError tests callback error handling.
//
// # Description
//
// Verifies that callback errors are properly propagated.
func TestDefaultStreamProcessor_ProcessChunk_CallbackError(t *testing.T) {
	t.Parallel()

	cfg := DefaultStreamConfig()
	processor := NewDefaultStreamProcessor(cfg, nil)

	chunk := &ollamaStreamChunk{
		Message: datatypes.Message{Content: "Hello"},
		Done:    false,
	}

	expectedErr := errors.New("callback failed")
	callback := func(event StreamEvent) error {
		return expectedErr
	}

	_, err := processor.ProcessChunk(context.Background(), chunk, callback)

	if err == nil {
		t.Fatal("ProcessChunk should return error when callback fails")
	}
	if !strings.Contains(err.Error(), "callback") {
		t.Errorf("Error should mention callback, got: %v", err)
	}
}

// =============================================================================
// ChatStream Integration Tests (with Mock Server)
// =============================================================================

// TestChatStream_BasicSuccess tests successful streaming.
//
// # Description
//
// Verifies end-to-end streaming with a mock server returning
// multiple content chunks followed by a done chunk.
func TestChatStream_BasicSuccess(t *testing.T) {
	t.Parallel()

	// Setup mock server
	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/api/chat" {
			t.Errorf("Expected path /api/chat, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/x-ndjson" {
			t.Errorf("Expected Accept: application/x-ndjson, got %s", r.Header.Get("Accept"))
		}

		// Write streaming response
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hello"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":" there"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"!"},"done":false}`)
		fmt.Fprintln(w, `{"done":true,"done_reason":"stop"}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	messages := []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}

	var response strings.Builder
	callback := func(event StreamEvent) error {
		if event.Type == StreamEventToken {
			response.WriteString(event.Content)
		}
		return nil
	}

	err := client.ChatStream(context.Background(), messages, GenerationParams{}, callback)

	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if response.String() != "Hello there!" {
		t.Errorf("Expected 'Hello there!', got '%s'", response.String())
	}
}

// TestChatStream_WithThinking tests streaming with thinking tokens.
//
// # Description
//
// Verifies that thinking tokens are streamed when present and not redacted.
func TestChatStream_WithThinking(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"thinking":"Let me think...","done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"The answer is 42"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "gpt-oss")

	var thinkingContent string
	var responseContent string

	callback := func(event StreamEvent) error {
		switch event.Type {
		case StreamEventThinking:
			thinkingContent += event.Content
		case StreamEventToken:
			responseContent += event.Content
		}
		return nil
	}

	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "What is the meaning of life?"},
	}, GenerationParams{}, callback)

	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if thinkingContent != "Let me think..." {
		t.Errorf("Expected thinking 'Let me think...', got '%s'", thinkingContent)
	}
	if responseContent != "The answer is 42" {
		t.Errorf("Expected response 'The answer is 42', got '%s'", responseContent)
	}
}

// TestChatStream_ThinkingRedacted tests streaming with thinking redaction.
//
// # Description
//
// Verifies that thinking tokens are NOT emitted when RedactThinking is true.
func TestChatStream_ThinkingRedacted(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"thinking":"Secret internal reasoning...","done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Response only"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "gpt-oss")

	cfg := StreamConfig{
		RedactThinking:    true,
		MaxResponseLength: 100 * 1024,
	}

	var thinkingReceived bool
	var responseContent string

	callback := func(event StreamEvent) error {
		switch event.Type {
		case StreamEventThinking:
			thinkingReceived = true
		case StreamEventToken:
			responseContent += event.Content
		}
		return nil
	}

	err := client.ChatStreamWithConfig(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Test"},
	}, GenerationParams{}, callback, cfg)

	if err != nil {
		t.Fatalf("ChatStreamWithConfig returned error: %v", err)
	}
	if thinkingReceived {
		t.Error("Thinking tokens should not be received when RedactThinking is true")
	}
	if responseContent != "Response only" {
		t.Errorf("Expected 'Response only', got '%s'", responseContent)
	}
}

// TestChatStream_ServerError tests handling of HTTP errors.
//
// # Description
//
// Verifies that non-200 HTTP responses are handled correctly.
func TestChatStream_ServerError(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":"internal server error"}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		return nil
	})

	if err == nil {
		t.Fatal("ChatStream should return error for server error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Error should contain status code, got: %v", err)
	}
}

// TestChatStream_StreamError tests handling of error in stream.
//
// # Description
//
// Verifies that error messages within the stream are handled correctly.
func TestChatStream_StreamError(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Starting..."},"done":false}`)
		fmt.Fprintln(w, `{"error":"model crashed"}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	var errorReceived bool
	var errorMessage string

	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		if event.Type == StreamEventError {
			errorReceived = true
			errorMessage = event.Error
		}
		return nil
	})

	if err == nil {
		t.Fatal("ChatStream should return error when stream contains error")
	}
	if !errorReceived {
		t.Error("Error event should be emitted before returning")
	}
	if errorMessage != "model crashed" {
		t.Errorf("Expected error 'model crashed', got '%s'", errorMessage)
	}
}

// TestChatStream_ContextCancellation tests context cancellation handling.
//
// # Description
//
// Verifies that streaming stops when context is cancelled.
func TestChatStream_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Server that sends slowly
	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"First"},"done":false}`)

		// Simulate slow response
		time.Sleep(500 * time.Millisecond)

		fmt.Fprintln(w, `{"message":{"content":"Second"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.ChatStream(ctx, []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		return nil
	})

	if err == nil {
		t.Fatal("ChatStream should return error on context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got: %v", err)
	}
}

// TestChatStream_CallbackAbort tests callback-initiated abort.
//
// # Description
//
// Verifies that returning an error from callback stops streaming.
func TestChatStream_CallbackAbort(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"First"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"Second"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"Third"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	tokenCount := 0
	abortErr := errors.New("user abort")

	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		if event.Type == StreamEventToken {
			tokenCount++
			if tokenCount >= 2 {
				return abortErr
			}
		}
		return nil
	})

	if err == nil {
		t.Fatal("ChatStream should return error when callback aborts")
	}
	if !strings.Contains(err.Error(), "callback") {
		t.Errorf("Error should mention callback, got: %v", err)
	}
	if tokenCount != 2 {
		t.Errorf("Expected 2 tokens before abort, got %d", tokenCount)
	}
}

// TestChatStream_MalformedJSON tests handling of malformed JSON lines.
//
// # Description
//
// Verifies that malformed JSON lines are skipped with a warning.
func TestChatStream_MalformedJSON(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"First"},"done":false}`)
		fmt.Fprintln(w, `{not valid json}`)
		fmt.Fprintln(w, `{"message":{"content":"Second"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	var tokens []string
	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		if event.Type == StreamEventToken {
			tokens = append(tokens, event.Content)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("ChatStream should not fail on malformed JSON, got: %v", err)
	}
	// Should have received First and Second, skipping the malformed line
	if len(tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0] != "First" || tokens[1] != "Second" {
		t.Errorf("Expected [First, Second], got %v", tokens)
	}
}

// TestChatStream_EmptyLines tests handling of empty lines in stream.
//
// # Description
//
// Verifies that empty lines in the NDJSON stream are skipped.
func TestChatStream_EmptyLines(t *testing.T) {
	t.Parallel()

	server := newMockOllamaServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Hello"},"done":false}`)
		fmt.Fprintln(w, ``)
		fmt.Fprintln(w, ``)
		fmt.Fprintln(w, `{"message":{"content":" World"},"done":false}`)
		fmt.Fprintln(w, `{"done":true}`)
	})
	defer server.Close()

	client := newTestOllamaClient(server.URL, "test-model")

	var response strings.Builder
	err := client.ChatStream(context.Background(), []datatypes.Message{
		{Role: "user", Content: "Hi"},
	}, GenerationParams{}, func(event StreamEvent) error {
		if event.Type == StreamEventToken {
			response.WriteString(event.Content)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if response.String() != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", response.String())
	}
}

// =============================================================================
// StreamConfig Tests
// =============================================================================

// TestDefaultStreamConfig tests default configuration values.
//
// # Description
//
// Verifies that DefaultStreamConfig returns sensible defaults.
func TestDefaultStreamConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultStreamConfig()

	if cfg.RedactThinking {
		t.Error("Default RedactThinking should be false")
	}
	if cfg.MaxThinkingLength != 0 {
		t.Errorf("Default MaxThinkingLength should be 0, got %d", cfg.MaxThinkingLength)
	}
	if cfg.RateLimitPerSecond != 0 {
		t.Errorf("Default RateLimitPerSecond should be 0, got %d", cfg.RateLimitPerSecond)
	}
	if cfg.MaxResponseLength != 100*1024 {
		t.Errorf("Default MaxResponseLength should be 102400, got %d", cfg.MaxResponseLength)
	}
}

// =============================================================================
// parseStreamChunk Tests
// =============================================================================

// TestParseStreamChunk_ValidJSON tests parsing valid JSON chunks.
//
// # Description
//
// Verifies that parseStreamChunk correctly parses valid NDJSON lines.
func TestParseStreamChunk_ValidJSON(t *testing.T) {
	t.Parallel()

	client := &OllamaClient{}

	testCases := []struct {
		name     string
		input    string
		expected ollamaStreamChunk
	}{
		{
			name:  "content only",
			input: `{"message":{"role":"assistant","content":"Hello"},"done":false}`,
			expected: ollamaStreamChunk{
				Message: datatypes.Message{Role: "assistant", Content: "Hello"},
				Done:    false,
			},
		},
		{
			name:  "thinking only",
			input: `{"thinking":"Let me think...","done":false}`,
			expected: ollamaStreamChunk{
				Thinking: "Let me think...",
				Done:     false,
			},
		},
		{
			name:  "done chunk",
			input: `{"done":true,"done_reason":"stop","total_duration":1500000000}`,
			expected: ollamaStreamChunk{
				Done:          true,
				DoneReason:    "stop",
				TotalDuration: 1500000000,
			},
		},
		{
			name:  "error chunk",
			input: `{"error":"model not found"}`,
			expected: ollamaStreamChunk{
				Error: "model not found",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chunk, err := client.parseStreamChunk([]byte(tc.input))
			if err != nil {
				t.Fatalf("parseStreamChunk returned error: %v", err)
			}
			if chunk.Message.Content != tc.expected.Message.Content {
				t.Errorf("Content mismatch: expected '%s', got '%s'",
					tc.expected.Message.Content, chunk.Message.Content)
			}
			if chunk.Thinking != tc.expected.Thinking {
				t.Errorf("Thinking mismatch: expected '%s', got '%s'",
					tc.expected.Thinking, chunk.Thinking)
			}
			if chunk.Done != tc.expected.Done {
				t.Errorf("Done mismatch: expected %v, got %v",
					tc.expected.Done, chunk.Done)
			}
			if chunk.Error != tc.expected.Error {
				t.Errorf("Error mismatch: expected '%s', got '%s'",
					tc.expected.Error, chunk.Error)
			}
		})
	}
}

// TestParseStreamChunk_InvalidJSON tests parsing invalid JSON.
//
// # Description
//
// Verifies that parseStreamChunk returns an error for invalid JSON.
func TestParseStreamChunk_InvalidJSON(t *testing.T) {
	t.Parallel()

	client := &OllamaClient{}

	invalidInputs := []string{
		`{not valid`,
		`"just a string"`,
		``,
		`{missing: quotes}`,
	}

	for _, input := range invalidInputs {
		t.Run(input, func(t *testing.T) {
			_, err := client.parseStreamChunk([]byte(input))
			if err == nil {
				t.Errorf("parseStreamChunk should return error for invalid JSON: %s", input)
			}
		})
	}
}
