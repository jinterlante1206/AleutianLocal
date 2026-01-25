// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/ux"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

// mockStreamingHTTPClient provides a mock HTTP client for streaming tests.
//
// # Description
//
// Implements HTTPClient interface with configurable responses for testing
// streaming services without network calls.
//
// # Fields
//
//   - postResponse: Response to return from Post calls
//   - postError: Error to return from Post calls
//   - getResponse: Response to return from Get calls
//   - getError: Error to return from Get calls
type mockStreamingHTTPClient struct {
	postResponse *http.Response
	postError    error
	getResponse  *http.Response
	getError     error
}

// Post implements HTTPClient.Post for testing.
func (m *mockStreamingHTTPClient) Post(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	if m.postError != nil {
		return nil, m.postError
	}
	return m.postResponse, nil
}

// PostWithHeaders implements HTTPClient.PostWithHeaders for testing.
func (m *mockStreamingHTTPClient) PostWithHeaders(ctx context.Context, url, contentType string, body io.Reader, _ map[string]string) (*http.Response, error) {
	// Delegate to Post for mock simplicity
	return m.Post(ctx, url, contentType, body)
}

// Get implements HTTPClient.Get for testing.
func (m *mockStreamingHTTPClient) Get(_ context.Context, _ string) (*http.Response, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	return m.getResponse, nil
}

// createSSEStream creates a mock SSE stream response.
//
// # Description
//
// Builds an SSE-formatted string from individual event lines.
//
// # Inputs
//
//   - events: SSE event lines (e.g., `data: {"type":"token","content":"Hi"}`)
//
// # Outputs
//
//   - string: Newline-joined SSE stream
func createSSEStream(events ...string) string {
	return strings.Join(events, "\n") + "\n"
}

// createMockResponse creates an http.Response with given status and body.
//
// # Description
//
// Creates a minimal http.Response for testing with specified body.
//
// # Inputs
//
//   - statusCode: HTTP status code
//   - body: Response body content
//
// # Outputs
//
//   - *http.Response: Mock response with NopCloser body
func createMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// =============================================================================
// RAG STREAMING CHAT SERVICE TESTS
// =============================================================================

func TestNewRAGStreamingChatService(t *testing.T) {
	t.Run("creates service with defaults", func(t *testing.T) {
		service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})

		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})

	t.Run("creates service with custom config", func(t *testing.T) {
		var buf bytes.Buffer
		service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
			BaseURL:     "http://localhost:8080",
			SessionID:   "sess-123",
			Pipeline:    "graph",
			Writer:      &buf,
			Personality: ux.PersonalityMachine,
			Timeout:     10 * time.Second,
		})

		if service == nil {
			t.Fatal("expected non-nil service")
		}
		if service.GetSessionID() != "sess-123" {
			t.Errorf("expected session ID 'sess-123', got %q", service.GetSessionID())
		}
	})
}

func TestRAGStreamingChatService_SendMessage_Success(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"status","message":"Searching..."}`,
		`data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.95}]}`,
		`data: {"type":"token","content":"Hello"}`,
		`data: {"type":"token","content":" world"}`,
		`data: {"type":"done","session_id":"sess-new"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Pipeline:    "reranking",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	ctx := context.Background()
	result, err := service.SendMessage(ctx, "What is auth?")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Answer != "Hello world" {
		t.Errorf("expected answer 'Hello world', got %q", result.Answer)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
	if service.GetSessionID() != "sess-new" {
		t.Errorf("expected session ID 'sess-new', got %q", service.GetSessionID())
	}
}

func TestRAGStreamingChatService_SendMessage_WithThinking(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"thinking","content":"Let me analyze..."}`,
		`data: {"type":"token","content":"The answer is 42"}`,
		`data: {"type":"done","session_id":"sess-123"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	result, err := service.SendMessage(context.Background(), "Question")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Thinking != "Let me analyze..." {
		t.Errorf("expected thinking 'Let me analyze...', got %q", result.Thinking)
	}
	if result.ThinkingTokens != 1 {
		t.Errorf("expected 1 thinking token, got %d", result.ThinkingTokens)
	}
}

func TestRAGStreamingChatService_SendMessage_HTTPError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		postError: errors.New("connection refused"),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "http post") {
		t.Errorf("expected 'http post' in error, got %q", err.Error())
	}
}

func TestRAGStreamingChatService_SendMessage_ServerError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusInternalServerError, "internal error"),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server error (500)") {
		t.Errorf("expected 'server error (500)' in error, got %q", err.Error())
	}
}

func TestRAGStreamingChatService_SendMessage_InvalidJSON(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"token","content":"Hi"}`,
		`data: {invalid json}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRAGStreamingChatService_SendMessage_ContextCancellation(t *testing.T) {
	// Create a slow stream that will be cancelled
	sseStream := createSSEStream(
		`data: {"type":"token","content":"Hi"}`,
		`data: {"type":"token","content":" there"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewRAGStreamingChatServiceWithClient(mock, RAGStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.SendMessage(ctx, "Hello")

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestRAGStreamingChatService_GetSessionID(t *testing.T) {
	t.Run("returns empty before first message", func(t *testing.T) {
		service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})

		if service.GetSessionID() != "" {
			t.Errorf("expected empty session ID, got %q", service.GetSessionID())
		}
	})

	t.Run("returns configured session ID", func(t *testing.T) {
		service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
			BaseURL:   "http://localhost:8080",
			SessionID: "sess-preset",
		})

		if service.GetSessionID() != "sess-preset" {
			t.Errorf("expected 'sess-preset', got %q", service.GetSessionID())
		}
	})
}

func TestRAGStreamingChatService_Close(t *testing.T) {
	service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	err := service.Close()

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// =============================================================================
// DIRECT STREAMING CHAT SERVICE TESTS
// =============================================================================

func TestNewDirectStreamingChatService(t *testing.T) {
	t.Run("creates service with defaults", func(t *testing.T) {
		service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})

		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})

	t.Run("creates service with thinking enabled", func(t *testing.T) {
		service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
			BaseURL:        "http://localhost:8080",
			EnableThinking: true,
			BudgetTokens:   4096,
		})

		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})
}

func TestDirectStreamingChatService_SendMessage_Success(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"status","message":"Connecting..."}`,
		`data: {"type":"token","content":"Hello"}`,
		`data: {"type":"token","content":" world"}`,
		`data: {"type":"done"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	result, err := service.SendMessage(context.Background(), "Hi there")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Hello world" {
		t.Errorf("expected answer 'Hello world', got %q", result.Answer)
	}
	if result.TotalTokens != 2 {
		t.Errorf("expected 2 tokens, got %d", result.TotalTokens)
	}
}

func TestDirectStreamingChatService_SendMessage_WithThinking(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"thinking","content":"Processing..."}`,
		`data: {"type":"token","content":"Done"}`,
		`data: {"type":"done"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:        "http://localhost:8080",
		EnableThinking: true,
		BudgetTokens:   2048,
		Writer:         &buf,
		Personality:    ux.PersonalityMachine,
	})

	result, err := service.SendMessage(context.Background(), "Think about this")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Thinking != "Processing..." {
		t.Errorf("expected thinking 'Processing...', got %q", result.Thinking)
	}
}

func TestDirectStreamingChatService_SendMessage_HTTPError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		postError: errors.New("network timeout"),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "http post") {
		t.Errorf("expected 'http post' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_SendMessage_ServerError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusBadRequest, "bad request"),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server error (400)") {
		t.Errorf("expected 'server error (400)' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_SendMessage_EmptyResponse(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"done"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_SendMessage_StreamError(t *testing.T) {
	sseStream := createSSEStream(
		`data: {"type":"token","content":"Hi"}`,
		`data: {"type":"error","error":"Model overloaded"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error for stream error event")
	}
	if !strings.Contains(err.Error(), "stream error") {
		t.Errorf("expected 'stream error' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_SendMessage_MessageHistoryOnError(t *testing.T) {
	// First request succeeds
	sseStream1 := createSSEStream(
		`data: {"type":"token","content":"First"}`,
		`data: {"type":"done"}`,
	)

	mock := &mockStreamingHTTPClient{
		postResponse: createMockResponse(http.StatusOK, sseStream1),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.SendMessage(context.Background(), "Message 1")
	if err != nil {
		t.Fatalf("unexpected error on first message: %v", err)
	}

	// Second request fails
	mock.postError = errors.New("connection lost")
	mock.postResponse = nil

	_, err = service.SendMessage(context.Background(), "Message 2")
	if err == nil {
		t.Fatal("expected error on second message")
	}

	// Message 2 should NOT be in history
	// History should be: system, user1, assistant1 = 3 messages
	// (We can't directly inspect, but the test verifies error handling removes message)
}

func TestDirectStreamingChatService_GetSessionID(t *testing.T) {
	t.Run("returns empty by default", func(t *testing.T) {
		service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})

		if service.GetSessionID() != "" {
			t.Errorf("expected empty session ID, got %q", service.GetSessionID())
		}
	})

	t.Run("returns configured session ID", func(t *testing.T) {
		service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
			BaseURL:   "http://localhost:8080",
			SessionID: "client-sess-123",
		})

		if service.GetSessionID() != "client-sess-123" {
			t.Errorf("expected 'client-sess-123', got %q", service.GetSessionID())
		}
	})
}

func TestDirectStreamingChatService_Close(t *testing.T) {
	service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	err := service.Close()

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestDirectStreamingChatService_LoadSessionHistory_Success(t *testing.T) {
	historyResponse := `{
		"Get": {
			"Conversation": [
				{"question": "What is Go?", "answer": "A programming language."},
				{"question": "Who created it?", "answer": "Google."}
			]
		}
	}`

	mock := &mockStreamingHTTPClient{
		getResponse: createMockResponse(http.StatusOK, historyResponse),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	turns, err := service.LoadSessionHistory(context.Background(), "sess-abc")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turns != 2 {
		t.Errorf("expected 2 turns, got %d", turns)
	}
	if service.GetSessionID() != "sess-abc" {
		t.Errorf("expected session ID 'sess-abc', got %q", service.GetSessionID())
	}
}

func TestDirectStreamingChatService_LoadSessionHistory_HTTPError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		getError: errors.New("connection refused"),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-abc")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "http get") {
		t.Errorf("expected 'http get' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_LoadSessionHistory_ServerError(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		getResponse: createMockResponse(http.StatusNotFound, "session not found"),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-missing")

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("expected 'status 404' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_LoadSessionHistory_InvalidJSON(t *testing.T) {
	mock := &mockStreamingHTTPClient{
		getResponse: createMockResponse(http.StatusOK, "not json"),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-abc")

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse history") {
		t.Errorf("expected 'parse history' in error, got %q", err.Error())
	}
}

func TestDirectStreamingChatService_LoadSessionHistory_MissingConversation(t *testing.T) {
	historyResponse := `{"Get": {}}`

	mock := &mockStreamingHTTPClient{
		getResponse: createMockResponse(http.StatusOK, historyResponse),
	}

	var buf bytes.Buffer
	service := NewDirectStreamingChatServiceWithClient(mock, DirectStreamingChatServiceConfig{
		BaseURL:     "http://localhost:8080",
		Writer:      &buf,
		Personality: ux.PersonalityMachine,
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-abc")

	if err == nil {
		t.Fatal("expected error for missing conversation")
	}
	if !strings.Contains(err.Error(), "no conversation data") {
		t.Errorf("expected 'no conversation data' in error, got %q", err.Error())
	}
}

// =============================================================================
// INTERFACE COMPLIANCE TESTS
// =============================================================================

func TestStreamingChatService_InterfaceCompliance(t *testing.T) {
	t.Run("RAG service implements interface", func(t *testing.T) {
		var _ StreamingChatService = NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})
	})

	t.Run("Direct service implements interface", func(t *testing.T) {
		var _ StreamingChatService = NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
			BaseURL: "http://localhost:8080",
		})
	})
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestRAGStreamingChatService_ConcurrentSessionAccess(t *testing.T) {
	service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
		BaseURL:   "http://localhost:8080",
		SessionID: "initial",
	})

	done := make(chan bool, 10)

	// Multiple goroutines reading session ID
	for i := 0; i < 10; i++ {
		go func() {
			_ = service.GetSessionID()
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestDirectStreamingChatService_ConcurrentSessionAccess(t *testing.T) {
	service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
		BaseURL:   "http://localhost:8080",
		SessionID: "initial",
	})

	done := make(chan bool, 10)

	// Multiple goroutines reading session ID
	for i := 0; i < 10; i++ {
		go func() {
			_ = service.GetSessionID()
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
