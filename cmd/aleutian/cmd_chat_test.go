package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSendRAGRequest(t *testing.T) {
	// 1. Setup a "Fake" Orchestrator
	mockOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the URL path
		if r.URL.Path != "/v1/rag" {
			t.Errorf("Expected path /v1/rag, got %s", r.URL.Path)
		}

		// Verify the Request Body
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["query"] != "Test Question" {
			t.Errorf("Expected query 'Test Question', got %v", reqBody["query"])
		}

		// Return a Fake Response
		resp := map[string]interface{}{
			"answer":     "This is a mock answer",
			"session_id": "mock-session-123",
			"sources": []map[string]interface{}{
				{"source": "doc1.txt", "score": 0.95},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockOrchestrator.Close()

	// 2. Trick the CLI into using the Mock URL
	// We assume getOrchestratorBaseURL() looks at an env var, or we override it if logic permits.
	// Since getOrchestratorBaseURL isn't exported or easily injectable in the current code,
	// we set the Env Var that function likely checks.
	os.Setenv("ALEUTIAN_ORCHESTRATOR_URL", mockOrchestrator.URL)
	defer os.Unsetenv("ALEUTIAN_ORCHESTRATOR_URL")

	// 3. Run the Function
	// We need to verify sendRAGRequest actually uses the environment variable logic
	// If getOrchestratorBaseURL is hardcoded to localhost in your code, this test requires
	// refactoring getOrchestratorBaseURL to prioritize the Env Var.
	response, err := sendRAGRequest("Test Question", "session-1", "standard")

	// 4. Assertions
	if err != nil {
		t.Fatalf("sendRAGRequest returned error: %v", err)
	}
	if response.Answer != "This is a mock answer" {
		t.Errorf("Expected answer 'This is a mock answer', got '%s'", response.Answer)
	}
	if len(response.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(response.Sources))
	}
}
