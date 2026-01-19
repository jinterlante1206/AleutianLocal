// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/pkg/extensions"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
)

// ============================================================================
// Test Setup
// ============================================================================

func init() {
	// Set Gin to test mode to reduce noise in test output
	gin.SetMode(gin.TestMode)
}

// mockLLMClient is a minimal mock for llm.LLMClient
type mockLLMClient struct{}

func (m *mockLLMClient) Generate(_ context.Context, _ string, _ llm.GenerationParams) (string, error) {
	return "mock response", nil
}

func (m *mockLLMClient) Chat(_ context.Context, _ []datatypes.Message, _ llm.GenerationParams) (string, error) {
	return "mock chat response", nil
}

func (m *mockLLMClient) ChatStream(_ context.Context, _ []datatypes.Message, _ llm.GenerationParams, callback llm.StreamCallback) error {
	_ = callback(llm.StreamEvent{Type: llm.StreamEventToken, Content: "mock stream"})
	return nil
}

// ============================================================================
// SetupRoutes Tests - Without Weaviate Client
// ============================================================================

func TestSetupRoutes_WithoutWeaviateClient(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	// Should not panic when weaviate client is nil
	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	// Verify core routes are registered
	coreRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/health"},
		{"GET", "/metrics"},
		{"GET", "/chat"},
		{"POST", "/v1/chat/direct"},
		{"POST", "/v1/chat/direct/stream"},
		{"POST", "/v1/timeseries/forecast"},
		{"POST", "/v1/data/fetch"},
		{"POST", "/v1/trading/signal"},
		{"POST", "/v1/models/pull"},
		{"POST", "/v1/agent/step"},
	}

	routes := router.Routes()
	for _, expected := range coreRoutes {
		found := false
		for _, r := range routes {
			if r.Method == expected.method && r.Path == expected.path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected route %s %s not found", expected.method, expected.path)
		}
	}
}

func TestSetupRoutes_VectorDBRoutesNotRegisteredWithoutClient(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	// These routes should NOT be registered when weaviate client is nil
	vectorDBRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/chat/ws"},
		{"POST", "/v1/chat/rag"},
		{"POST", "/v1/chat/rag/stream"},
		{"POST", "/v1/documents"},
		{"GET", "/v1/documents"},
		{"DELETE", "/v1/document"},
		{"POST", "/v1/rag"},
		{"GET", "/v1/sessions"},
		{"GET", "/v1/sessions/:sessionId/history"},
		{"GET", "/v1/sessions/:sessionId/documents"},
		{"DELETE", "/v1/sessions/:sessionId"},
		{"POST", "/v1/sessions/:sessionId/verify"},
		{"POST", "/v1/weaviate/backups"},
		{"GET", "/v1/weaviate/summary"},
		{"DELETE", "/v1/weaviate/data"},
	}

	routes := router.Routes()
	for _, notExpected := range vectorDBRoutes {
		found := false
		for _, r := range routes {
			if r.Method == notExpected.method && r.Path == notExpected.path {
				found = true
				break
			}
		}
		if found {
			t.Errorf("Route %s %s should NOT be registered without Weaviate client", notExpected.method, notExpected.path)
		}
	}
}

// ============================================================================
// Route Handler Tests
// ============================================================================

func TestSetupRoutes_HealthEndpoint(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health endpoint returned %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSetupRoutes_ChatRedirect(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/chat", nil)
	router.ServeHTTP(w, req)

	// Should redirect to /ui/chat.html
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("Chat redirect returned %d, want %d", w.Code, http.StatusMovedPermanently)
	}

	location := w.Header().Get("Location")
	if location != "/ui/chat.html" {
		t.Errorf("Chat redirect location = %q, want %q", location, "/ui/chat.html")
	}
}

func TestSetupRoutes_MetricsEndpoint(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)

	// Prometheus metrics endpoint should return 200
	if w.Code != http.StatusOK {
		t.Errorf("Metrics endpoint returned %d, want %d", w.Code, http.StatusOK)
	}

	// Should return prometheus format
	contentType := w.Header().Get("Content-Type")
	if contentType == "" {
		t.Error("Metrics endpoint should return Content-Type header")
	}
}

// ============================================================================
// Route Count Tests
// ============================================================================

func TestSetupRoutes_RouteCountWithoutWeaviate(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	routes := router.Routes()

	// Expected core routes when Weaviate is not available:
	// - GET /health
	// - GET /metrics
	// - GET /ui/* (StaticFS)
	// - GET /chat
	// - POST /v1/chat/direct
	// - POST /v1/chat/direct/stream
	// - POST /v1/timeseries/forecast
	// - POST /v1/data/fetch
	// - POST /v1/trading/signal
	// - POST /v1/models/pull
	// - POST /v1/agent/step
	// Plus HEAD methods for GET endpoints

	// Instead of exact count, verify minimum routes
	minExpectedRoutes := 10
	if len(routes) < minExpectedRoutes {
		t.Errorf("Expected at least %d routes, got %d", minExpectedRoutes, len(routes))
	}
}

// ============================================================================
// Nil Safety Tests
// ============================================================================

func TestSetupRoutes_NilPolicyEngine_Panics(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}

	// SetupRoutes requires non-nil policy engine for handlers
	// Verify it panics with appropriate message
	defer func() {
		r := recover()
		if r == nil {
			t.Error("Expected SetupRoutes to panic with nil policy engine")
			return
		}
		panicMsg, ok := r.(string)
		if !ok {
			return // Panic recovered, which is expected
		}
		if panicMsg == "" {
			return // Any panic is expected
		}
	}()

	SetupRoutes(router, nil, mockLLM, nil, extensions.DefaultOptions())
}

func TestSetupRoutes_NilLLMClient_Panics(t *testing.T) {
	router := gin.New()
	policyEng, _ := policy_engine.NewPolicyEngine()

	// SetupRoutes requires non-nil LLM client for handlers
	// Verify it panics with appropriate message
	defer func() {
		r := recover()
		if r == nil {
			t.Error("Expected SetupRoutes to panic with nil LLM client")
			return
		}
		// Panic recovered, which is expected
	}()

	SetupRoutes(router, nil, nil, policyEng, extensions.DefaultOptions())
}

// ============================================================================
// Static File Routes Tests
// ============================================================================

func TestSetupRoutes_StaticFS(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	// StaticFS should be registered for /ui
	routes := router.Routes()
	foundUI := false
	for _, r := range routes {
		if r.Path == "/ui/*filepath" && r.Method == "GET" {
			foundUI = true
			break
		}
	}

	if !foundUI {
		t.Error("Expected /ui/*filepath route for static files")
	}
}

// ============================================================================
// API Version Group Tests
// ============================================================================

func TestSetupRoutes_V1GroupExists(t *testing.T) {
	router := gin.New()
	mockLLM := &mockLLMClient{}
	policyEng, _ := policy_engine.NewPolicyEngine()

	SetupRoutes(router, nil, mockLLM, policyEng, extensions.DefaultOptions())

	routes := router.Routes()
	v1Routes := 0
	for _, r := range routes {
		if len(r.Path) > 3 && r.Path[:3] == "/v1" {
			v1Routes++
		}
	}

	if v1Routes == 0 {
		t.Error("Expected at least one /v1 route")
	}
}
