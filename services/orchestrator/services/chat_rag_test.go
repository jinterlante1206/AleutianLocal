// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock LLM Client
// =============================================================================

// MockLLMClient implements llm.LLMClient for testing purposes.
// It allows configuring responses and tracking calls for verification.
type MockLLMClient struct {
	// ChatResponse is returned by Chat method
	ChatResponse string
	// ChatError is returned as error by Chat method
	ChatError error
	// ChatCallCount tracks how many times Chat was called
	ChatCallCount int
	// LastMessages stores the last messages passed to Chat
	LastMessages []datatypes.Message
}

// Chat implements the llm.LLMClient interface for testing.
func (m *MockLLMClient) Chat(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams) (string, error) {
	m.ChatCallCount++
	m.LastMessages = messages
	return m.ChatResponse, m.ChatError
}

// Generate implements the llm.LLMClient interface for testing.
func (m *MockLLMClient) Generate(ctx context.Context, prompt string, params llm.GenerationParams) (string, error) {
	return "", nil
}

// ChatStream implements the llm.LLMClient interface for testing.
// Emits ChatResponse as a single token event.
func (m *MockLLMClient) ChatStream(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams, callback llm.StreamCallback) error {
	m.ChatCallCount++
	m.LastMessages = messages
	if m.ChatResponse != "" {
		if err := callback(llm.StreamEvent{Type: llm.StreamEventToken, Content: m.ChatResponse}); err != nil {
			return err
		}
	}
	return m.ChatError
}

// =============================================================================
// NewChatRAGService Tests
// =============================================================================

// TestNewChatRAGService_DefaultRAGEngineURL verifies that the constructor
// uses the default RAG engine URL when the environment variable is not set.
func TestNewChatRAGService_DefaultRAGEngineURL(t *testing.T) {
	// Ensure the env var is not set
	originalURL := os.Getenv("RAG_ENGINE_URL")
	os.Unsetenv("RAG_ENGINE_URL")
	defer func() {
		if originalURL != "" {
			os.Setenv("RAG_ENGINE_URL", originalURL)
		}
	}()

	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	service := NewChatRAGService(nil, mockLLM, pe)

	require.NotNil(t, service, "service should not be nil")
	assert.Equal(t, "http://aleutian-rag-engine:8000", service.ragEngineURL,
		"should use default RAG engine URL")
}

// TestNewChatRAGService_CustomRAGEngineURL verifies that the constructor
// uses the RAG engine URL from the environment variable when set.
func TestNewChatRAGService_CustomRAGEngineURL(t *testing.T) {
	customURL := "http://custom-rag-engine:9000"

	originalURL := os.Getenv("RAG_ENGINE_URL")
	os.Setenv("RAG_ENGINE_URL", customURL)
	defer func() {
		if originalURL != "" {
			os.Setenv("RAG_ENGINE_URL", originalURL)
		} else {
			os.Unsetenv("RAG_ENGINE_URL")
		}
	}()

	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	service := NewChatRAGService(nil, mockLLM, pe)

	require.NotNil(t, service, "service should not be nil")
	assert.Equal(t, customURL, service.ragEngineURL,
		"should use custom RAG engine URL from environment")
}

// TestNewChatRAGService_StoresDependencies verifies that all dependencies
// are properly stored in the service struct.
func TestNewChatRAGService_StoresDependencies(t *testing.T) {
	mockLLM := &MockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	service := NewChatRAGService(nil, mockLLM, pe)

	assert.Nil(t, service.weaviateClient, "weaviate client should be nil as passed")
	assert.Equal(t, mockLLM, service.llmClient, "LLM client should be stored")
	assert.Equal(t, pe, service.policyEngine, "policy engine should be stored")
}

// =============================================================================
// ChatRAGService.Process Tests
// =============================================================================

// TestChatRAGService_Process_ValidationFailure verifies that Process returns
// an error when the request fails validation.
func TestChatRAGService_Process_ValidationFailure(t *testing.T) {
	service := createTestService(t, nil)

	// Empty message should fail validation
	req := &datatypes.ChatRAGRequest{
		Message: "",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error for invalid request")
	assert.Nil(t, resp, "response should be nil on error")
	assert.Contains(t, err.Error(), "validation failed",
		"error should mention validation failure")
}

// TestChatRAGService_Process_InvalidPipeline verifies that Process returns
// an error when an invalid pipeline is specified.
func TestChatRAGService_Process_InvalidPipeline(t *testing.T) {
	service := createTestService(t, nil)

	req := &datatypes.ChatRAGRequest{
		Message:  "test",
		Pipeline: "invalid-pipeline",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error for invalid pipeline")
	assert.Nil(t, resp, "response should be nil on error")
	assert.Contains(t, err.Error(), "invalid pipeline",
		"error should mention invalid pipeline")
}

// TestChatRAGService_Process_PolicyViolation verifies that Process returns
// a PolicyViolationError when sensitive data is detected in the message.
func TestChatRAGService_Process_PolicyViolation(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	// Create service with real policy engine
	service := &ChatRAGService{
		policyEngine: pe,
		ragEngineURL: "http://localhost:8000",
	}

	// Request with SSN (should be detected by policy engine)
	req := &datatypes.ChatRAGRequest{
		Message: "My SSN is 123-45-6789",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error for policy violation")
	assert.Nil(t, resp, "response should be nil on error")

	// Verify it's a PolicyViolationError
	assert.True(t, IsPolicyViolation(err), "error should be PolicyViolationError")

	findings := GetPolicyFindings(err)
	assert.NotEmpty(t, findings, "should have policy findings")
}

// TestChatRAGService_Process_Success verifies the successful processing path
// when the RAG engine returns a valid response.
func TestChatRAGService_Process_Success(t *testing.T) {
	// Create mock RAG engine server
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/rag/reranking")

		// Return mock response
		resp := datatypes.RagEngineResponse{
			Answer: "Authentication uses JWT tokens.",
			Sources: []datatypes.SourceInfo{
				{Source: "auth.go", Score: 0.95},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "How does authentication work?",
	}

	resp, err := service.Process(context.Background(), req)

	require.NoError(t, err, "should not return error on success")
	require.NotNil(t, resp, "response should not be nil")

	assert.NotEmpty(t, resp.Id, "response should have ID")
	assert.NotZero(t, resp.CreatedAt, "response should have timestamp")
	assert.Equal(t, "Authentication uses JWT tokens.", resp.Answer)
	assert.NotEmpty(t, resp.SessionId, "session ID should be set")
	assert.Len(t, resp.Sources, 1, "should have one source")
	assert.Equal(t, 1, resp.TurnCount, "turn count should be 1 for first message")
}

// TestChatRAGService_Process_WithExistingSession verifies that an existing
// session ID is preserved in the response.
func TestChatRAGService_Process_WithExistingSession(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datatypes.RagEngineResponse{
			Answer:  "Response for existing session",
			Sources: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	existingSessionId := "existing-session-123"
	req := &datatypes.ChatRAGRequest{
		Message:   "Follow up question",
		SessionId: existingSessionId,
	}

	resp, err := service.Process(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, existingSessionId, resp.SessionId,
		"should preserve existing session ID")
}

// TestChatRAGService_Process_WithHistory verifies that the turn count
// accounts for conversation history.
func TestChatRAGService_Process_WithHistory(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := datatypes.RagEngineResponse{
			Answer:  "Response with history",
			Sources: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "Third question",
		History: []datatypes.ChatTurn{
			{Id: "1", Role: "user", Content: "first"},
			{Id: "2", Role: "assistant", Content: "first answer"},
			{Id: "3", Role: "user", Content: "second"},
			{Id: "4", Role: "assistant", Content: "second answer"},
		},
	}

	resp, err := service.Process(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 5, resp.TurnCount, "turn count should include history + current")
}

// TestChatRAGService_Process_WithBearing verifies that the bearing filter
// is passed to the RAG engine.
func TestChatRAGService_Process_WithBearing(t *testing.T) {
	var receivedPayload map[string]interface{}

	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		resp := datatypes.RagEngineResponse{Answer: "filtered response"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "Security question",
		Bearing: "security",
	}

	_, err := service.Process(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "security", receivedPayload["bearing"],
		"bearing should be passed to RAG engine")
}

// TestChatRAGService_Process_RAGEngineError verifies that errors from the
// RAG engine are properly propagated.
func TestChatRAGService_Process_RAGEngineError(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "RAG engine failed"}`))
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "test",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error when RAG engine fails")
	assert.Nil(t, resp, "response should be nil on error")
	assert.Contains(t, err.Error(), "RAG engine",
		"error should mention RAG engine")
}

// TestChatRAGService_Process_ContextCancellation verifies that the service
// respects context cancellation.
func TestChatRAGService_Process_ContextCancellation(t *testing.T) {
	// Server that delays response
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		resp := datatypes.RagEngineResponse{Answer: "delayed response"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := &datatypes.ChatRAGRequest{
		Message: "test",
	}

	resp, err := service.Process(ctx, req)

	require.Error(t, err, "should return error when context is cancelled")
	assert.Nil(t, resp)
}

// TestChatRAGService_Process_SetsDefaults verifies that EnsureDefaults is
// called and populates request fields.
func TestChatRAGService_Process_SetsDefaults(t *testing.T) {
	var receivedPayload map[string]interface{}

	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		resp := datatypes.RagEngineResponse{Answer: "response"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "test",
		// Pipeline not set - should default to "reranking"
	}

	_, err := service.Process(context.Background(), req)

	require.NoError(t, err)

	// Verify defaults were set
	assert.NotEmpty(t, req.Id, "Id should be populated")
	assert.NotZero(t, req.CreatedAt, "CreatedAt should be populated")
	assert.Equal(t, "reranking", req.Pipeline, "Pipeline should default to reranking")

	// Verify pipeline was sent to RAG engine
	assert.Equal(t, "reranking", receivedPayload["pipeline"])
}

// =============================================================================
// ChatRAGService.ScanPolicy Tests
// =============================================================================

// TestChatRAGService_ScanPolicy_NoViolation verifies that ScanPolicy returns
// an empty slice when no sensitive data is found.
func TestChatRAGService_ScanPolicy_NoViolation(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")
	service := &ChatRAGService{policyEngine: pe}

	// Use a message that won't trigger any policy rules
	findings := service.ScanPolicy("What is the weather like today?")

	assert.Empty(t, findings, "should have no findings for clean message")
}

// TestChatRAGService_ScanPolicy_WithSSN verifies that ScanPolicy detects SSN.
func TestChatRAGService_ScanPolicy_WithSSN(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")
	service := &ChatRAGService{policyEngine: pe}

	findings := service.ScanPolicy("My SSN is 123-45-6789")

	assert.NotEmpty(t, findings, "should detect SSN")
}

// TestChatRAGService_ScanPolicy_NilPolicyEngine verifies that ScanPolicy
// handles a nil policy engine gracefully.
func TestChatRAGService_ScanPolicy_NilPolicyEngine(t *testing.T) {
	service := &ChatRAGService{policyEngine: nil}

	findings := service.ScanPolicy("123-45-6789") // SSN pattern

	assert.Nil(t, findings, "should return nil when policy engine is nil")
}

// =============================================================================
// PolicyViolationError Tests
// =============================================================================

// TestPolicyViolationError_Error verifies the error message format.
func TestPolicyViolationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		findings []policy_engine.ScanFinding
		expected string
	}{
		{
			name:     "single finding",
			findings: make([]policy_engine.ScanFinding, 1),
			expected: "policy violation: 1 findings",
		},
		{
			name:     "multiple findings",
			findings: make([]policy_engine.ScanFinding, 5),
			expected: "policy violation: 5 findings",
		},
		{
			name:     "no findings",
			findings: []policy_engine.ScanFinding{},
			expected: "policy violation: 0 findings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &PolicyViolationError{Findings: tt.findings}
			assert.Equal(t, tt.expected, err.Error())
		})
	}
}

// TestIsPolicyViolation verifies the IsPolicyViolation helper function.
func TestIsPolicyViolation(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "PolicyViolationError",
			err:      &PolicyViolationError{Findings: nil},
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPolicyViolation(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetPolicyFindings verifies the GetPolicyFindings helper function.
func TestGetPolicyFindings(t *testing.T) {
	findings := []policy_engine.ScanFinding{
		{MatchedContent: "123-45-6789", ClassificationName: "SSN"},
	}

	t.Run("returns findings from PolicyViolationError", func(t *testing.T) {
		err := &PolicyViolationError{Findings: findings}
		result := GetPolicyFindings(err)
		assert.Equal(t, findings, result)
	})

	t.Run("returns nil for generic error", func(t *testing.T) {
		err := errors.New("some error")
		result := GetPolicyFindings(err)
		assert.Nil(t, result)
	})

	t.Run("returns nil for nil error", func(t *testing.T) {
		result := GetPolicyFindings(nil)
		assert.Nil(t, result)
	})
}

// =============================================================================
// callRAGEngine Tests (via integration with Process)
// =============================================================================

// TestChatRAGService_callRAGEngine_SendsCorrectPayload verifies that the
// correct payload structure is sent to the RAG engine.
func TestChatRAGService_callRAGEngine_SendsCorrectPayload(t *testing.T) {
	var receivedPayload map[string]interface{}

	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse payload
		json.NewDecoder(r.Body).Decode(&receivedPayload)

		resp := datatypes.RagEngineResponse{Answer: "response"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message:   "test query",
		SessionId: "sess-123",
		Pipeline:  "reranking",
		Bearing:   "security",
		History: []datatypes.ChatTurn{
			{Id: "1", Role: "user", Content: "previous"},
		},
	}

	_, err := service.Process(context.Background(), req)

	require.NoError(t, err)

	// Verify payload fields
	assert.Equal(t, "test query", receivedPayload["query"])
	assert.Equal(t, "sess-123", receivedPayload["session_id"])
	assert.Equal(t, "reranking", receivedPayload["pipeline"])
	assert.Equal(t, "security", receivedPayload["bearing"])
	assert.NotNil(t, receivedPayload["history"], "history should be included")
}

// TestChatRAGService_callRAGEngine_ConstructsCorrectURL verifies that the
// correct URL is constructed based on the pipeline.
func TestChatRAGService_callRAGEngine_ConstructsCorrectURL(t *testing.T) {
	pipelines := []string{"standard", "reranking", "raptor", "graph", "rig", "semantic"}

	for _, pipeline := range pipelines {
		t.Run(pipeline, func(t *testing.T) {
			var requestPath string

			mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestPath = r.URL.Path
				resp := datatypes.RagEngineResponse{Answer: "response"}
				json.NewEncoder(w).Encode(resp)
			}))
			defer mockRAGServer.Close()

			service := createTestService(t, mockRAGServer)

			req := &datatypes.ChatRAGRequest{
				Message:  "test",
				Pipeline: pipeline,
			}

			_, err := service.Process(context.Background(), req)

			require.NoError(t, err)
			assert.Equal(t, "/rag/"+pipeline, requestPath,
				"URL path should include pipeline name")
		})
	}
}

// TestChatRAGService_callRAGEngine_HandlesInvalidJSON verifies that the
// service handles invalid JSON responses from the RAG engine.
func TestChatRAGService_callRAGEngine_HandlesInvalidJSON(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "test",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error for invalid JSON")
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "parse",
		"error should mention parsing issue")
}

// TestChatRAGService_callRAGEngine_HandlesEmptyResponse verifies that the
// service handles empty but valid JSON responses.
func TestChatRAGService_callRAGEngine_HandlesEmptyResponse(t *testing.T) {
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer mockRAGServer.Close()

	service := createTestService(t, mockRAGServer)

	req := &datatypes.ChatRAGRequest{
		Message: "test",
	}

	resp, err := service.Process(context.Background(), req)

	require.NoError(t, err, "empty JSON should be valid")
	require.NotNil(t, resp)
	assert.Empty(t, resp.Answer, "answer should be empty")
	assert.Empty(t, resp.Sources, "sources should be empty")
}

// TestChatRAGService_callRAGEngine_HandlesNetworkError verifies that the
// service handles network errors gracefully.
func TestChatRAGService_callRAGEngine_HandlesNetworkError(t *testing.T) {
	// Create a server and immediately close it to simulate network error
	mockRAGServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := mockRAGServer.URL
	mockRAGServer.Close()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	service := &ChatRAGService{
		policyEngine: pe,
		ragEngineURL: serverURL,
	}

	req := &datatypes.ChatRAGRequest{
		Message: "test",
	}

	resp, err := service.Process(context.Background(), req)

	require.Error(t, err, "should return error for network failure")
	assert.Nil(t, resp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// createTestService creates a ChatRAGService configured for testing.
// If mockServer is provided, its URL is used as the RAG engine URL.
// Otherwise, a dummy URL is used.
func createTestService(t *testing.T, mockServer *httptest.Server) *ChatRAGService {
	t.Helper()

	ragURL := "http://dummy:8000"
	if mockServer != nil {
		ragURL = mockServer.URL
	}

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	return &ChatRAGService{
		weaviateClient: nil,
		llmClient:      &MockLLMClient{},
		policyEngine:   pe,
		ragEngineURL:   ragURL,
	}
}
