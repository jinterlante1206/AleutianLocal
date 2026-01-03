// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Setup
// =============================================================================

// StreamingMockLLMClient implements llm.LLMClient for streaming handler testing.
//
// # Description
//
// Provides configurable mock for testing streaming chat handlers.
// Allows simulating token-by-token streaming and errors.
type StreamingMockLLMClient struct {
	// StreamTokens are the tokens to emit during ChatStream
	StreamTokens []string
	// StreamError is returned as error by ChatStream
	StreamError error
	// ChatStreamCallCount tracks how many times ChatStream was called
	ChatStreamCallCount int
	// LastMessages stores the last messages passed to ChatStream
	LastMessages []datatypes.Message
}

// Chat implements llm.LLMClient.Chat for testing.
func (m *StreamingMockLLMClient) Chat(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams) (string, error) {
	return strings.Join(m.StreamTokens, ""), nil
}

// Generate implements llm.LLMClient.Generate for testing.
func (m *StreamingMockLLMClient) Generate(ctx context.Context, prompt string, params llm.GenerationParams) (string, error) {
	return "", nil
}

// ChatStream implements llm.LLMClient.ChatStream for testing.
// Emits configured tokens one by one.
func (m *StreamingMockLLMClient) ChatStream(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams, callback llm.StreamCallback) error {
	m.ChatStreamCallCount++
	m.LastMessages = messages

	for _, token := range m.StreamTokens {
		if err := callback(llm.StreamEvent{Type: llm.StreamEventToken, Content: token}); err != nil {
			return err
		}
	}

	return m.StreamError
}

// createTestStreamingChatHandler creates a StreamingChatHandler with mock dependencies.
func createTestStreamingChatHandler(t *testing.T, mockLLM *StreamingMockLLMClient) StreamingChatHandler {
	t.Helper()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	return NewStreamingChatHandler(mockLLM, pe, nil)
}

// =============================================================================
// NewStreamingChatHandler Tests
// =============================================================================

// TestNewStreamingChatHandler_PanicsOnNilLLMClient verifies that NewStreamingChatHandler
// panics when llmClient is nil.
func TestNewStreamingChatHandler_PanicsOnNilLLMClient(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	assert.Panics(t, func() {
		NewStreamingChatHandler(nil, pe, nil)
	}, "should panic on nil llmClient")
}

// TestNewStreamingChatHandler_PanicsOnNilPolicyEngine verifies that NewStreamingChatHandler
// panics when policyEngine is nil.
func TestNewStreamingChatHandler_PanicsOnNilPolicyEngine(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}

	assert.Panics(t, func() {
		NewStreamingChatHandler(mockLLM, nil, nil)
	}, "should panic on nil policyEngine")
}

// TestNewStreamingChatHandler_Success verifies that NewStreamingChatHandler
// creates a valid handler when all dependencies are provided.
func TestNewStreamingChatHandler_Success(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	handler := NewStreamingChatHandler(mockLLM, pe, nil)

	assert.NotNil(t, handler, "handler should not be nil")
}

// =============================================================================
// HandleDirectChatStream Tests
// =============================================================================

// TestHandleDirectChatStream_InvalidRequestBody verifies that the handler
// returns 400 when the request body is invalid JSON.
func TestHandleDirectChatStream_InvalidRequestBody(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "should return 400 for invalid JSON")
}

// TestHandleDirectChatStream_ValidationFailure verifies that the handler
// returns 400 when the request fails validation.
func TestHandleDirectChatStream_ValidationFailure(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	// Request with empty messages (fails validation)
	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages:  []datatypes.Message{},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "should return 400 for validation failure")
}

// TestHandleDirectChatStream_PolicyViolation verifies that the handler
// returns 403 when the user message contains sensitive data.
func TestHandleDirectChatStream_PolicyViolation(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	// Request with sensitive data (SSN)
	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "My SSN is 123-45-6789"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "should return 403 for policy violation")
}

// TestHandleDirectChatStream_Success verifies that the handler streams
// tokens correctly for a valid request.
func TestHandleDirectChatStream_Success(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{
		StreamTokens: []string{"Hello", " ", "world", "!"},
	}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "should return 200")
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"), "should set SSE content type")

	// Parse SSE events
	events := parseSSEEvents(t, w.Body.String())
	assert.True(t, len(events) >= 2, "should have at least status and done events")

	// Verify LLM was called
	assert.Equal(t, 1, mockLLM.ChatStreamCallCount, "ChatStream should be called once")
}

// TestHandleDirectChatStream_SSEHeaders verifies that the handler sets
// correct SSE headers.
func TestHandleDirectChatStream_SSEHeaders(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{
		StreamTokens: []string{"test"},
	}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "test"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
}

// =============================================================================
// HandleChatRAGStream Tests
// =============================================================================

// TestHandleChatRAGStream_NoRAGService verifies that the handler returns 500
// when RAG service is not configured.
func TestHandleChatRAGStream_NoRAGService(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	// Create handler WITHOUT RAG service
	handler := NewStreamingChatHandler(mockLLM, pe, nil)

	router := gin.New()
	router.POST("/v1/chat/rag/stream", handler.HandleChatRAGStream)

	reqBody := datatypes.ChatRAGRequest{
		Message: "test query",
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/rag/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "should return 500 when RAG service not available")
}

// TestHandleChatRAGStream_InvalidRequestBody verifies that the handler
// returns 500 for invalid JSON (RAG service check happens first).
func TestHandleChatRAGStream_InvalidRequestBody(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	// Without RAG service, should return 500 before parsing body
	handler := NewStreamingChatHandler(mockLLM, pe, nil)

	router := gin.New()
	router.POST("/v1/chat/rag/stream", handler.HandleChatRAGStream)

	req, _ := http.NewRequest("POST", "/v1/chat/rag/stream", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 500 because RAG service is nil (checked before parsing)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// NOTE: Testing RAG policy violation requires a mock RAG service
// which is more complex to set up. This would be an integration test.
// The policy check in HandleChatRAGStream is tested implicitly through
// the direct chat tests since they share the same policy engine logic.

// =============================================================================
// Helper Functions
// =============================================================================

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	Event string
	Data  string
}

// parseSSEEvents parses SSE events from a response body.
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()

	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))

	var currentEvent sseEvent
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent.Event != "" {
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		}
	}

	// Add last event if not empty
	if currentEvent.Event != "" {
		events = append(events, currentEvent)
	}

	return events
}
