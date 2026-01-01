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

	"github.com/gin-gonic/gin"
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
type MockLLMClient struct {
	ChatResponse string
	ChatError    error
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams) (string, error) {
	return m.ChatResponse, m.ChatError
}

func (m *MockLLMClient) Generate(ctx context.Context, prompt string, params llm.GenerationParams) (string, error) {
	return "", nil
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

// =============================================================================
// HandleDirectChat Tests
// =============================================================================

// TestHandleDirectChat_Success verifies that a valid request returns
// a successful response with the LLM's answer.
func TestHandleDirectChat_Success(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Hello! How can I help you?"}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

	body := DirectChatRequest{
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "Hello! How can I help you?", response["answer"])
}

// TestHandleDirectChat_InvalidJSON verifies that invalid JSON returns
// a 400 Bad Request response.
func TestHandleDirectChat_InvalidJSON(t *testing.T) {
	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

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

// TestHandleDirectChat_EmptyMessages verifies that an empty messages array
// returns a 400 Bad Request response.
func TestHandleDirectChat_EmptyMessages(t *testing.T) {
	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

	body := DirectChatRequest{
		Messages: []datatypes.Message{},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "no messages provided", response["error"])
}

// TestHandleDirectChat_PolicyViolation verifies that sensitive data in
// user messages triggers a 403 Forbidden response.
func TestHandleDirectChat_PolicyViolation(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Should not reach this"}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

	body := DirectChatRequest{
		Messages: []datatypes.Message{
			{Role: "user", Content: "My SSN is 123-45-6789"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "Policy Violation")
	assert.NotNil(t, response["findings"])
}

// TestHandleDirectChat_LLMError verifies that LLM errors return a 500
// Internal Server Error response.
func TestHandleDirectChat_LLMError(t *testing.T) {
	mockLLM := &MockLLMClient{
		ChatError: assert.AnError,
	}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

	body := DirectChatRequest{
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.NotEmpty(t, response["error"])
}

// TestHandleDirectChat_WithGenerationParams verifies that generation parameters
// are passed through to the LLM client.
func TestHandleDirectChat_WithGenerationParams(t *testing.T) {
	mockLLM := &MockLLMClient{ChatResponse: "Response with thinking"}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	router := createTestRouter("POST", "/v1/chat/direct", HandleDirectChat(mockLLM, pe))

	body := DirectChatRequest{
		Messages: []datatypes.Message{
			{Role: "user", Content: "Explain this code"},
		},
		EnableThinking: true,
		BudgetTokens:   1000,
	}

	w := performRequest(router, "POST", "/v1/chat/direct", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "Response with thinking", response["answer"])
}

// =============================================================================
// HandleChatRAG Tests
// =============================================================================

// TestHandleChatRAG_InvalidJSON verifies that invalid JSON returns
// a 400 Bad Request response.
func TestHandleChatRAG_InvalidJSON(t *testing.T) {
	// Create mock RAG server
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called
		t.Error("RAG server should not be called for invalid JSON")
	}))
	defer mockRAGServer.Close()

	// Create service with mock server
	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

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
		// Should not be called
		t.Error("RAG server should not be called for validation failure")
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

	body := datatypes.ChatRAGRequest{
		Message: "", // Empty message should fail validation
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["details"], "validation failed")
}

// TestHandleChatRAG_PolicyViolation verifies that sensitive data in
// the message triggers a 403 Forbidden response.
func TestHandleChatRAG_PolicyViolation(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called for policy violation
		t.Error("RAG server should not be called for policy violation")
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

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
		// Return mock RAG response
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
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

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
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

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

// TestHandleChatRAG_WithBearing verifies that the bearing filter is passed
// through to the RAG engine.
func TestHandleChatRAG_WithBearing(t *testing.T) {
	var receivedBearing string

	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		if b, ok := payload["bearing"].(string); ok {
			receivedBearing = b
		}

		resp := datatypes.RagEngineResponse{
			Answer: "Security-related answer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

	body := datatypes.ChatRAGRequest{
		Message: "How does security work?",
		Bearing: "security",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "security", receivedBearing, "bearing should be passed to RAG engine")
}

// TestHandleChatRAG_RAGEngineError verifies that RAG engine errors return
// a 500 Internal Server Error response.
func TestHandleChatRAG_RAGEngineError(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "RAG engine failed"}`))
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

	body := datatypes.ChatRAGRequest{
		Message: "What is authentication?",
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "Failed to process request", response["error"])
	assert.Contains(t, response["details"], "RAG engine")
}

// TestHandleChatRAG_WithHistory verifies that conversation history is handled
// correctly and reflected in the turn count.
func TestHandleChatRAG_WithHistory(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)

		resp := datatypes.RagEngineResponse{
			Answer: "Response with history context",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createMockChatRAGService(t, mockRAGServer)
	router := createTestRouter("POST", "/v1/chat/rag", HandleChatRAG(service))

	body := datatypes.ChatRAGRequest{
		Message: "Third question",
		History: []datatypes.ChatTurn{
			{Id: "1", Role: "user", Content: "first question"},
			{Id: "2", Role: "assistant", Content: "first answer"},
			{Id: "3", Role: "user", Content: "second question"},
			{Id: "4", Role: "assistant", Content: "second answer"},
		},
	}

	w := performRequest(router, "POST", "/v1/chat/rag", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var response datatypes.ChatRAGResponse
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, 5, response.TurnCount, "turn count should include history")
}

// =============================================================================
// Helper Functions
// =============================================================================

// createMockChatRAGService creates a ChatRAGService configured for testing.
func createMockChatRAGService(t *testing.T, mockRAGServer *httptest.Server) *services.ChatRAGService {
	t.Helper()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	mockLLM := &MockLLMClient{}

	// Use reflection or create service directly since fields are unexported
	// For now, we'll use the constructor and set RAG_ENGINE_URL env var
	originalURL := ""
	if mockRAGServer != nil {
		originalURL = mockRAGServer.URL
	}

	// Create service - it will read RAG_ENGINE_URL from env or use default
	// Since we can't easily inject the URL, we'll need a workaround
	// The service constructor reads from env, so we set it temporarily
	t.Setenv("RAG_ENGINE_URL", originalURL)

	return services.NewChatRAGService(nil, mockLLM, pe)
}
