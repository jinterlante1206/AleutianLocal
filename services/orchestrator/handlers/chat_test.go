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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/services"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Setup
// =============================================================================

func init() {
	// Set Gin to test mode to reduce noise in test output
	gin.SetMode(gin.TestMode)
}

// MockLLMClient implements llm.LLMClient for handler testing.
//
// # Description
//
// Provides a configurable mock for testing chat handlers without
// making real LLM API calls.
//
// # Fields
//
//   - ChatResponse: Response to return from Chat()
//   - ChatError: Error to return from Chat()
type MockLLMClient struct {
	ChatResponse string
	ChatError    error
}

// Chat implements llm.LLMClient.Chat for testing.
func (m *MockLLMClient) Chat(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams) (string, error) {
	return m.ChatResponse, m.ChatError
}

// Generate implements llm.LLMClient.Generate for testing.
func (m *MockLLMClient) Generate(ctx context.Context, prompt string, params llm.GenerationParams) (string, error) {
	return "", nil
}

// ChatStream implements llm.LLMClient.ChatStream for testing.
// Returns an error indicating streaming is not implemented in mock.
func (m *MockLLMClient) ChatStream(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams, callback llm.StreamCallback) error {
	// For testing, emit the ChatResponse as a single token if set
	if m.ChatResponse != "" {
		if err := callback(llm.StreamEvent{Type: llm.StreamEventToken, Content: m.ChatResponse}); err != nil {
			return err
		}
	}
	return m.ChatError
}

// createTestChatHandler creates a ChatHandler with mock dependencies for testing.
//
// # Inputs
//
//   - t: Test instance for error reporting
//   - mockLLM: Mock LLM client
//   - ragService: Optional RAG service (may be nil)
//
// # Outputs
//
//   - ChatHandler: Configured handler for testing
func createTestChatHandler(t *testing.T, mockLLM *MockLLMClient, ragService *services.ChatRAGService) ChatHandler {
	t.Helper()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	return NewChatHandler(mockLLM, pe, ragService)
}

// createTestRouter creates a Gin router with the specified handler for testing.
func createTestRouter(method, path string, handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	switch method {
	case "POST":
		router.POST(path, handler)
	case "GET":
		router.GET(path, handler)
	}
	return router
}

// performRequest executes an HTTP request against the test router.
func performRequest(router *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(jsonBytes)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// newValidDirectChatRequest creates a valid DirectChatRequest for testing.
//
// # Description
//
// Helper function to create a properly populated request with all required fields.
func newValidDirectChatRequest(content string) datatypes.DirectChatRequest {
	return datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: content},
		},
	}
}

// =============================================================================
// NewChatHandler Tests
// =============================================================================

// TestNewChatHandler_PanicsOnNilLLMClient verifies that NewChatHandler panics
// when llmClient is nil.
func TestNewChatHandler_PanicsOnNilLLMClient(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	assert.Panics(t, func() {
		NewChatHandler(nil, pe, nil)
	}, "should panic on nil llmClient")
}

// TestNewChatHandler_PanicsOnNilPolicyEngine verifies that NewChatHandler panics
// when policyEngine is nil.
func TestNewChatHandler_PanicsOnNilPolicyEngine(t *testing.T) {
	mockLLM := &MockLLMClient{}

	assert.Panics(t, func() {
		NewChatHandler(mockLLM, nil, nil)
	}, "should panic on nil policyEngine")
}

// TestNewChatHandler_AcceptsNilRAGService verifies that NewChatHandler accepts
// nil ragService (optional dependency).
func TestNewChatHandler_AcceptsNilRAGService(t *testing.T) {
	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		NewChatHandler(mockLLM, pe, nil)
	}, "should accept nil ragService")
}

// =============================================================================
// HandleDirectChat Tests
// =============================================================================

// TestHandleDirectChat_Success verifies that a valid request returns
// a successful response with the LLM's answer.
func TestHandleDirectChat_Success(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Hello! How can I help you?"}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := newValidDirectChatRequest("Hello")
	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response datatypes.DirectChatResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.NotEmpty(t, response.ResponseID, "response should have response_id")
	assert.Equal(t, body.RequestID, response.RequestID, "response should echo request_id")
	assert.NotZero(t, response.Timestamp, "response should have timestamp")
	assert.Equal(t, "Hello! How can I help you?", response.Answer)
	// ProcessingTimeMs may be 0 if mock returns in < 1ms, just verify it's non-negative
	assert.GreaterOrEqual(t, response.ProcessingTimeMs, int64(0), "processing_time_ms should be non-negative")
}

// TestHandleDirectChat_InvalidJSON verifies that invalid JSON returns
// a 400 Bad Request response.
func TestHandleDirectChat_InvalidJSON(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	// Send invalid JSON
	req, _ := http.NewRequest("POST", "/v1/chat/direct", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "invalid request body")
}

// TestHandleDirectChat_MissingRequestID verifies that a request without
// request_id returns a 400 Bad Request response.
func TestHandleDirectChat_MissingRequestID(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := datatypes.DirectChatRequest{
		// Missing RequestID
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "validation failed")
}

// TestHandleDirectChat_MissingTimestamp verifies that a request without
// timestamp returns a 400 Bad Request response.
func TestHandleDirectChat_MissingTimestamp(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		// Missing Timestamp
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "validation failed")
}

// TestHandleDirectChat_EmptyMessages verifies that an empty messages array
// returns a 400 Bad Request response.
func TestHandleDirectChat_EmptyMessages(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages:  []datatypes.Message{},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "validation failed")
}

// TestHandleDirectChat_PolicyViolation verifies that sensitive data in
// user messages triggers a 403 Forbidden response.
func TestHandleDirectChat_PolicyViolation(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Should not reach this"}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := newValidDirectChatRequest("My SSN is 123-45-6789")
	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "Policy Violation")
	assert.NotNil(t, response["findings"])
}

// TestHandleDirectChat_LLMError verifies that LLM errors return a 500
// Internal Server Error response without leaking error details.
func TestHandleDirectChat_LLMError(t *testing.T) {
	mockLLM := &MockLLMClient{
		ChatError: assert.AnError,
	}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := newValidDirectChatRequest("Hello")
	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	// SEC-005: Error should be sanitized, no internal details
	assert.Equal(t, "Failed to process request", response["error"])
	assert.Nil(t, response["details"], "should not leak error details")
}

// TestHandleDirectChat_WithGenerationParams verifies that generation parameters
// are passed through to the LLM client.
func TestHandleDirectChat_WithGenerationParams(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Response with thinking"}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "Explain this code"},
		},
		EnableThinking: true,
		BudgetTokens:   1000,
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response datatypes.DirectChatResponse
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "Response with thinking", response.Answer)
}

// TestHandleDirectChat_InvalidRole verifies that invalid message role
// returns a 400 Bad Request response.
func TestHandleDirectChat_InvalidRole(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil)

	router := createTestRouter("POST", "/v1/chat/direct", handler.HandleDirectChat)

	body := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "invalid_role", Content: "Hello"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// HandleChatRAG Tests
// =============================================================================

// TestHandleChatRAG_NilService verifies that calling HandleChatRAG when
// ragService is nil returns a 500 Internal Server Error.
func TestHandleChatRAG_NilService(t *testing.T) {
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, nil) // nil ragService

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	body := datatypes.ChatRAGRequest{
		Message: "What is authentication?",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "RAG service not available", response["error"])
}

// TestHandleChatRAG_InvalidJSON verifies that invalid JSON returns
// a 400 Bad Request response.
func TestHandleChatRAG_InvalidJSON(t *testing.T) {
	// Create mock RAG server
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("RAG server should not be called for invalid JSON")
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	// Send invalid JSON
	req, _ := http.NewRequest("POST", "/v1/chat/rag", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "invalid request body", response["error"])
}

// TestHandleChatRAG_ValidationFailure verifies that an empty message returns
// a 500 response (validation error from service).
func TestHandleChatRAG_ValidationFailure(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("RAG server should not be called for validation failure")
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	body := datatypes.ChatRAGRequest{
		Message: "", // Empty message should fail validation
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	// SEC-005: Error should be sanitized
	assert.Equal(t, "Failed to process request", response["error"])
	assert.Nil(t, response["details"], "should not leak error details")
}

// TestHandleChatRAG_PolicyViolation verifies that sensitive data in
// the message triggers a 403 Forbidden response.
func TestHandleChatRAG_PolicyViolation(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("RAG server should not be called for policy violation")
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	body := datatypes.ChatRAGRequest{
		Message: "My SSN is 123-45-6789",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "Policy Violation")
	assert.NotNil(t, response["findings"])
}

// TestHandleChatRAG_Success verifies that a valid request returns
// a successful response with the RAG answer.
func TestHandleChatRAG_Success(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datatypes.RagEngineResponse{
			Answer: "Authentication uses JWT tokens with RSA256 signing.",
			Sources: []datatypes.SourceInfo{
				{Source: "auth.go", Score: 0.95},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	body := datatypes.ChatRAGRequest{
		Message:  "How does authentication work?",
		Pipeline: "reranking",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response datatypes.ChatRAGResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.NotEmpty(t, response.Id, "response should have ID")
	assert.NotZero(t, response.CreatedAt, "response should have timestamp")
	assert.Equal(t, "Authentication uses JWT tokens with RSA256 signing.", response.Answer)
	assert.NotEmpty(t, response.SessionId, "response should have session ID")
	assert.Len(t, response.Sources, 1, "response should have sources")
	assert.Equal(t, 1, response.TurnCount, "turn count should be 1")
}

// TestHandleChatRAG_WithExistingSession verifies that an existing session ID
// is preserved in the response.
func TestHandleChatRAG_WithExistingSession(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datatypes.RagEngineResponse{
			Answer:  "Follow up answer",
			Sources: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	existingSessionId := "existing-session-123"
	body := datatypes.ChatRAGRequest{
		Message:   "Follow up question",
		SessionId: existingSessionId,
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response datatypes.ChatRAGResponse
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, existingSessionId, response.SessionId)
}

// TestHandleChatRAG_RAGEngineError verifies that RAG engine errors return
// a 500 Internal Server Error response without leaking details.
func TestHandleChatRAG_RAGEngineError(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "RAG engine failed"}`))
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	mockLLM := &MockLLMClient{}
	handler := createTestChatHandler(t, mockLLM, service)

	router := createTestRouter("POST", "/v1/chat/rag", handler.HandleChatRAG)

	body := datatypes.ChatRAGRequest{
		Message: "What is authentication?",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	// SEC-005: Error should be sanitized
	assert.Equal(t, "Failed to process request", response["error"])
	assert.Nil(t, response["details"], "should not leak error details")
}

// =============================================================================
// Helper Functions
// =============================================================================

// createMockChatRAGService creates a ChatRAGService configured for testing.
//
// # Description
//
// Sets up a ChatRAGService with a mock RAG server URL for isolated testing.
//
// # Inputs
//
//   - t: Test instance for error reporting
//   - mockRAGServer: HTTP test server simulating the RAG engine
//
// # Outputs
//
//   - *services.ChatRAGService: Configured service for testing
func createMockChatRAGService(t *testing.T, mockRAGServer *httptest.Server) *services.ChatRAGService {
	t.Helper()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	mockLLM := &MockLLMClient{}

	originalURL := ""
	if mockRAGServer != nil {
		originalURL = mockRAGServer.URL
	}

	// Set RAG_ENGINE_URL for the service to use
	t.Setenv("RAG_ENGINE_URL", originalURL)

	return services.NewChatRAGService(nil, mockLLM, pe)
}
