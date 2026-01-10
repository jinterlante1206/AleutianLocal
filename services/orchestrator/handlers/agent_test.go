// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// Tests for agent step handler

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// HandleAgentStep Tests
// =============================================================================

func TestHandleAgentStep_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader([]byte("{invalid json")))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "Invalid")
}

func TestHandleAgentStep_PolicyViolationInQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create request with SSN pattern (will be detected as PII)
	reqBody := datatypes.AgentStepRequest{
		Query:   "The SSN is 123-45-6789 and we need to process it",
		History: []datatypes.AgentMessage{},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"].(string), "Policy Violation")
}

func TestHandleAgentStep_PolicyViolationInHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create request with clean query but sensitive data in history (tool output)
	reqBody := datatypes.AgentStepRequest{
		Query: "What's in that file?",
		History: []datatypes.AgentMessage{
			{
				Role:    "tool",
				Content: "File contains: SSN: 123-45-6789, Credit Card: 4111-1111-1111-1111",
			},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"].(string), "Policy Violation")
	assert.NotNil(t, response["findings"])
}

func TestHandleAgentStep_CleanQueryPasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create a mock server for the Python RAG engine
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decision": "respond",
			"content":  "This is the agent response",
		})
	}))
	defer mockServer.Close()

	// Set the RAG_ENGINE_URL environment variable
	t.Setenv("RAG_ENGINE_URL", mockServer.URL)

	// Create request with clean query
	reqBody := datatypes.AgentStepRequest{
		Query: "What is the weather today?",
		History: []datatypes.AgentMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAgentStep_RAGEngineUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Set a non-existent RAG engine URL
	t.Setenv("RAG_ENGINE_URL", "http://localhost:12345")

	reqBody := datatypes.AgentStepRequest{
		Query:   "What is the weather today?",
		History: []datatypes.AgentMessage{},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Should return 502 Bad Gateway when RAG engine is unavailable
	assert.Equal(t, http.StatusBadGateway, w.Code)

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Contains(t, response["error"], "unavailable")
}

func TestHandleAgentStep_RAGEngineError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create a mock server that returns an error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Internal processing error"}`))
	}))
	defer mockServer.Close()

	t.Setenv("RAG_ENGINE_URL", mockServer.URL)

	reqBody := datatypes.AgentStepRequest{
		Query:   "What is the weather today?",
		History: []datatypes.AgentMessage{},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAgentStep_AssistantRoleNotScanned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create a mock server for the Python RAG engine
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"decision": "respond",
		})
	}))
	defer mockServer.Close()

	t.Setenv("RAG_ENGINE_URL", mockServer.URL)

	// Create request where assistant role contains sensitive data
	// (assistant role should NOT be scanned - only tool and user roles)
	reqBody := datatypes.AgentStepRequest{
		Query: "What is the weather today?",
		History: []datatypes.AgentMessage{
			{
				Role:    "assistant",
				Content: "Here is the SSN: 123-45-6789", // Should NOT be scanned
			},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Should pass because assistant role is not scanned
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAgentStep_UserRoleInHistoryScanned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create request where user role in history contains sensitive data
	reqBody := datatypes.AgentStepRequest{
		Query: "What is the weather today?",
		History: []datatypes.AgentMessage{
			{
				Role:    "user",
				Content: "My SSN is 123-45-6789", // SHOULD be scanned
			},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Should fail because user role in history IS scanned
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAgentStep_EmptyHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pe, _ := policy_engine.NewPolicyEngine()

	// Create a mock server for the Python RAG engine
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request body
		var reqBody datatypes.AgentStepRequest
		json.NewDecoder(r.Body).Decode(&reqBody)

		assert.Equal(t, "Hello world", reqBody.Query)
		assert.Empty(t, reqBody.History)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"decision": "respond"})
	}))
	defer mockServer.Close()

	t.Setenv("RAG_ENGINE_URL", mockServer.URL)

	reqBody := datatypes.AgentStepRequest{
		Query:   "Hello world",
		History: []datatypes.AgentMessage{}, // Empty history
	}

	jsonBody, _ := json.Marshal(reqBody)

	router := gin.New()
	router.POST("/v1/agent/step", HandleAgentStep(pe))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agent/step", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
