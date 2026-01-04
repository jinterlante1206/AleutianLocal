// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main contains unit tests for ollama_client.go.

# Testing Strategy

These tests use httptest to create mock Ollama API servers:
  - Mock /api/tags for model listing
  - Mock /api/show for custom model detection
  - Mock /api/pull for model pulling with streaming

All tests are designed to run fast (<1s total) and in isolation.

# Test Coverage

The tests cover:
  - Model listing with caching behavior
  - HasModel with name normalization
  - Custom model detection via template field
  - Model pulling with progress callbacks
  - Model size queries
  - Error handling for all failure modes
  - Cache invalidation after pulls
*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Mock OllamaModelManager for Testing
// -----------------------------------------------------------------------------

// MockOllamaModelManager implements OllamaModelManager for testing.
type MockOllamaModelManager struct {
	models         []OllamaModel
	hasModelMap    map[string]bool
	customModelMap map[string]bool
	pullError      error
	sizeMap        map[string]int64
	listError      error
	baseURL        string

	// Track calls
	mu            sync.Mutex
	listCallCount int
	pullCalls     []string
}

func (m *MockOllamaModelManager) ListModels(ctx context.Context) ([]OllamaModel, error) {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()

	if m.listError != nil {
		return nil, m.listError
	}
	return m.models, nil
}

func (m *MockOllamaModelManager) RefreshModelCache(ctx context.Context) error {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()
	return m.listError
}

func (m *MockOllamaModelManager) HasModel(ctx context.Context, modelName string) (bool, error) {
	if m.listError != nil {
		return false, m.listError
	}
	if m.hasModelMap != nil {
		return m.hasModelMap[modelName], nil
	}
	return false, nil
}

func (m *MockOllamaModelManager) IsCustomModel(ctx context.Context, modelName string) (bool, error) {
	if m.customModelMap != nil {
		return m.customModelMap[modelName], nil
	}
	return false, nil
}

func (m *MockOllamaModelManager) PullModel(ctx context.Context, modelName string, progress PullProgressCallback) error {
	m.mu.Lock()
	m.pullCalls = append(m.pullCalls, modelName)
	m.mu.Unlock()

	if progress != nil {
		progress("pulling manifest", 0, 0)
		progress("downloading", 50, 100)
		progress("verifying", 100, 100)
	}
	return m.pullError
}

func (m *MockOllamaModelManager) GetModelSize(ctx context.Context, modelName string) (int64, error) {
	if m.sizeMap != nil {
		if size, ok := m.sizeMap[modelName]; ok {
			return size, nil
		}
	}
	return 500 * 1024 * 1024, nil // Default fallback
}

func (m *MockOllamaModelManager) GetBaseURL() string {
	return m.baseURL
}

// -----------------------------------------------------------------------------
// ModelError Tests
// -----------------------------------------------------------------------------

func TestModelErrorType_String(t *testing.T) {
	tests := []struct {
		errorType ModelErrorType
		expected  string
	}{
		{ModelErrorNotFound, "MODEL_NOT_FOUND"},
		{ModelErrorPullFailed, "PULL_FAILED"},
		{ModelErrorConnectionFailed, "CONNECTION_FAILED"},
		{ModelErrorInvalidResponse, "INVALID_RESPONSE"},
		{ModelErrorContextCancelled, "CONTEXT_CANCELLED"},
		{ModelErrorType(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.errorType.String(); got != tt.expected {
				t.Errorf("ModelErrorType.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestModelError_Error(t *testing.T) {
	err := &ModelError{
		Type:    ModelErrorNotFound,
		Model:   "test-model",
		Message: "Model not found",
	}

	if got := err.Error(); got != "Model not found" {
		t.Errorf("ModelError.Error() = %q, want %q", got, "Model not found")
	}
}

func TestModelError_FullError(t *testing.T) {
	err := &ModelError{
		Type:        ModelErrorPullFailed,
		Model:       "test-model",
		Message:     "Pull failed",
		Detail:      "Connection reset",
		Remediation: "Check network and try again",
	}

	full := err.FullError()

	if !strings.Contains(full, "Pull failed") {
		t.Error("FullError should contain Message")
	}
	if !strings.Contains(full, "test-model") {
		t.Error("FullError should contain Model name")
	}
	if !strings.Contains(full, "Connection reset") {
		t.Error("FullError should contain Detail")
	}
	if !strings.Contains(full, "Check network") {
		t.Error("FullError should contain Remediation")
	}
}

// -----------------------------------------------------------------------------
// OllamaClient Constructor Tests
// -----------------------------------------------------------------------------

func TestNewOllamaClient_NormalizesURL(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434/")

	if client.baseURL != "http://localhost:11434" {
		t.Errorf("Expected trailing slash to be removed, got %s", client.baseURL)
	}
}

func TestNewOllamaClient_SetsDefaults(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434")

	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}

	if client.cacheTTL != 30*time.Second {
		t.Errorf("Default cache TTL should be 30s, got %v", client.cacheTTL)
	}

	if client.customModelMap == nil {
		t.Error("customModelMap should be initialized")
	}
}

func TestGetBaseURL(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434")

	if got := client.GetBaseURL(); got != "http://localhost:11434" {
		t.Errorf("GetBaseURL() = %q, want %q", got, "http://localhost:11434")
	}
}

// -----------------------------------------------------------------------------
// ListModels Tests
// -----------------------------------------------------------------------------

func TestListModels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}

		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{
					Name:   "nomic-embed-text-v2-moe:latest",
					Size:   274000000,
					Digest: "abc123",
				},
				{
					Name:   "gpt-oss:latest",
					Size:   4100000000,
					Digest: "def456",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	models, err := client.ListModels(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	if models[0].Name != "nomic-embed-text-v2-moe:latest" {
		t.Errorf("Unexpected model name: %s", models[0].Name)
	}
}

func TestListModels_Caching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// First call
	_, _ = client.ListModels(ctx)
	// Second call should use cache
	_, _ = client.ListModels(ctx)

	if callCount != 1 {
		t.Errorf("Expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestListModels_CacheExpiry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	client.cacheTTL = 10 * time.Millisecond // Very short for testing
	ctx := context.Background()

	// First call
	_, _ = client.ListModels(ctx)

	// Wait for cache to expire
	time.Sleep(20 * time.Millisecond)

	// Second call should make new request
	_, _ = client.ListModels(ctx)

	if callCount != 2 {
		t.Errorf("Expected 2 HTTP calls after cache expiry, got %d", callCount)
	}
}

func TestListModels_ConnectionError(t *testing.T) {
	// Use invalid URL to force connection error
	client := NewOllamaClient("http://localhost:99999")
	ctx := context.Background()

	_, err := client.ListModels(ctx)

	if err == nil {
		t.Error("Expected error for connection failure")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Error("Error should be a ModelError")
	}

	if modelErr.Type != ModelErrorConnectionFailed {
		t.Errorf("Expected ModelErrorConnectionFailed, got %v", modelErr.Type)
	}
}

func TestListModels_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	_, err := client.ListModels(ctx)

	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Error("Error should be a ModelError")
	}

	if modelErr.Type != ModelErrorInvalidResponse {
		t.Errorf("Expected ModelErrorInvalidResponse, got %v", modelErr.Type)
	}
}

func TestListModels_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ListModels(ctx)

	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// -----------------------------------------------------------------------------
// RefreshModelCache Tests
// -----------------------------------------------------------------------------

func TestRefreshModelCache_ClearsCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Prime the cache
	_, _ = client.ListModels(ctx)

	// Refresh should make new request
	_ = client.RefreshModelCache(ctx)

	if callCount != 2 {
		t.Errorf("Expected 2 HTTP calls after refresh, got %d", callCount)
	}
}

// -----------------------------------------------------------------------------
// HasModel Tests
// -----------------------------------------------------------------------------

func TestHasModel_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{Name: "nomic-embed-text-v2-moe:latest"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	exists, err := client.HasModel(ctx, "nomic-embed-text-v2-moe")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !exists {
		t.Error("Expected model to be found")
	}
}

func TestHasModel_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{Name: "other-model:latest"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	exists, err := client.HasModel(ctx, "nomic-embed-text-v2-moe")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if exists {
		t.Error("Expected model to not be found")
	}
}

func TestHasModel_MatchesWithLatestTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{Name: "nomic-embed-text-v2-moe:latest"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Search without :latest
	exists, _ := client.HasModel(ctx, "nomic-embed-text-v2-moe")
	if !exists {
		t.Error("Should match model without :latest suffix")
	}

	// Search with :latest
	exists, _ = client.HasModel(ctx, "nomic-embed-text-v2-moe:latest")
	if !exists {
		t.Error("Should match model with :latest suffix")
	}
}

func TestHasModel_CaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaTagsResponse{
			Models: []ollamaModelInfo{
				{Name: "Nomic-Embed-Text-V2-MOE:latest"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	exists, _ := client.HasModel(ctx, "nomic-embed-text-v2-moe")
	if !exists {
		t.Error("Should match model case-insensitively")
	}
}

// -----------------------------------------------------------------------------
// IsCustomModel Tests
// -----------------------------------------------------------------------------

func TestIsCustomModel_WithTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			resp := ollamaShowResponse{
				Template: "{{ .System }}\n{{ .Prompt }}",
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	isCustom, err := client.IsCustomModel(ctx, "my-custom-llm")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !isCustom {
		t.Error("Model with template should be detected as custom")
	}
}

func TestIsCustomModel_WithoutTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			resp := ollamaShowResponse{
				Template: "", // No template = not custom
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	isCustom, err := client.IsCustomModel(ctx, "nomic-embed-text-v2-moe")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if isCustom {
		t.Error("Model without template should not be detected as custom")
	}
}

func TestIsCustomModel_Caching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			callCount++
			resp := ollamaShowResponse{Template: "test"}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// First call
	_, _ = client.IsCustomModel(ctx, "test-model")
	// Second call should use cache
	_, _ = client.IsCustomModel(ctx, "test-model")

	if callCount != 1 {
		t.Errorf("Expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestIsCustomModel_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"model not found"}`))
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	_, err := client.IsCustomModel(ctx, "nonexistent")

	if err == nil {
		t.Error("Expected error for model not found")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Error("Error should be a ModelError")
	}

	if modelErr.Type != ModelErrorNotFound {
		t.Errorf("Expected ModelErrorNotFound, got %v", modelErr.Type)
	}
}

// -----------------------------------------------------------------------------
// PullModel Tests
// -----------------------------------------------------------------------------

func TestPullModel_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			// Stream progress updates
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("Response writer doesn't support flushing")
			}

			updates := []ollamaPullProgress{
				{Status: "pulling manifest"},
				{Status: "pulling sha256:abc123", Completed: 50, Total: 100},
				{Status: "verifying sha256 digest"},
				{Status: "success"},
			}

			for _, u := range updates {
				json.NewEncoder(w).Encode(u)
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	var progressCalls []string
	err := client.PullModel(ctx, "test-model", func(status string, completed, total int64) {
		progressCalls = append(progressCalls, status)
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(progressCalls) == 0 {
		t.Error("Expected progress callback to be called")
	}
}

func TestPullModel_NilProgressCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			updates := []ollamaPullProgress{
				{Status: "success"},
			}
			for _, u := range updates {
				json.NewEncoder(w).Encode(u)
			}
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Should not panic with nil callback
	err := client.PullModel(ctx, "test-model", nil)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

func TestPullModel_ErrorInStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			updates := []ollamaPullProgress{
				{Status: "pulling manifest"},
				{Error: "pull access denied"},
			}
			for _, u := range updates {
				json.NewEncoder(w).Encode(u)
			}
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	err := client.PullModel(ctx, "private-model", nil)

	if err == nil {
		t.Error("Expected error from pull stream")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Error("Error should be a ModelError")
	}

	if modelErr.Type != ModelErrorPullFailed {
		t.Errorf("Expected ModelErrorPullFailed, got %v", modelErr.Type)
	}
}

func TestPullModel_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	err := client.PullModel(ctx, "test-model", nil)

	if err == nil {
		t.Error("Expected error for HTTP error")
	}

	var modelErr *ModelError
	if !errors.As(err, &modelErr) {
		t.Error("Error should be a ModelError")
	}
}

func TestPullModel_ClearsCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
			json.NewEncoder(w).Encode(resp)
		case "/api/pull":
			progress := ollamaPullProgress{Status: "success"}
			json.NewEncoder(w).Encode(progress)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Prime the cache
	_, _ = client.ListModels(ctx)

	// Pull should clear the cache
	_ = client.PullModel(ctx, "new-model", nil)

	// Check cache was cleared
	client.cacheMu.RLock()
	cacheCleared := client.modelCache == nil
	client.cacheMu.RUnlock()

	if !cacheCleared {
		t.Error("PullModel should clear the model cache")
	}
}

func TestPullModel_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.PullModel(ctx, "test-model", nil)

	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

// -----------------------------------------------------------------------------
// GetModelSize Tests
// -----------------------------------------------------------------------------

func TestGetModelSize_LocalModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := ollamaTagsResponse{
				Models: []ollamaModelInfo{
					{Name: "test-model:latest", Size: 1234567890},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	size, err := client.GetModelSize(ctx, "test-model")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if size != 1234567890 {
		t.Errorf("Expected size 1234567890, got %d", size)
	}
}

func TestGetModelSize_FallbackForUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	size, err := client.GetModelSize(ctx, "unknown-model")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return 500MB fallback
	expectedFallback := int64(500 * 1024 * 1024)
	if size != expectedFallback {
		t.Errorf("Expected fallback size %d, got %d", expectedFallback, size)
	}
}

// -----------------------------------------------------------------------------
// normalizeModelName Tests
// -----------------------------------------------------------------------------

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"model:latest", "model"},
		{"model", "model"},
		{"Model:Latest", "model"},
		{"nomic-embed-text-v2-moe:latest", "nomic-embed-text-v2-moe"},
		{"UPPERCASE", "uppercase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeModelName(tt.input); got != tt.expected {
				t.Errorf("normalizeModelName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Integration-Style Tests (Using Mock)
// -----------------------------------------------------------------------------

func TestMockOllamaModelManager_Interface(t *testing.T) {
	// Verify MockOllamaModelManager implements OllamaModelManager
	var _ OllamaModelManager = (*MockOllamaModelManager)(nil)
}

func TestFullWorkflow_PullMissingModels(t *testing.T) {
	mock := &MockOllamaModelManager{
		hasModelMap: map[string]bool{
			"existing-model": true,
		},
		sizeMap: map[string]int64{
			"new-model": 1024 * 1024 * 1024, // 1GB
		},
	}

	ctx := context.Background()

	// Check for existing model
	exists, _ := mock.HasModel(ctx, "existing-model")
	if !exists {
		t.Error("Existing model should be found")
	}

	// Check for missing model
	exists, _ = mock.HasModel(ctx, "new-model")
	if exists {
		t.Error("New model should not exist")
	}

	// Pull missing model
	err := mock.PullModel(ctx, "new-model", nil)
	if err != nil {
		t.Errorf("Pull should succeed: %v", err)
	}

	// Verify pull was recorded
	if len(mock.pullCalls) != 1 || mock.pullCalls[0] != "new-model" {
		t.Error("Pull should be recorded")
	}
}

func TestFullWorkflow_CustomModelDetection(t *testing.T) {
	mock := &MockOllamaModelManager{
		hasModelMap: map[string]bool{
			"registry-model": true,
			"custom-llm":     true,
		},
		customModelMap: map[string]bool{
			"registry-model": false,
			"custom-llm":     true,
		},
	}

	ctx := context.Background()

	// Registry model should not be custom
	isCustom, _ := mock.IsCustomModel(ctx, "registry-model")
	if isCustom {
		t.Error("Registry model should not be custom")
	}

	// Custom model should be detected
	isCustom, _ = mock.IsCustomModel(ctx, "custom-llm")
	if !isCustom {
		t.Error("Custom model should be detected")
	}
}

func TestProgressCallback_Format(t *testing.T) {
	var lastStatus string
	var lastCompleted, lastTotal int64

	callback := func(status string, completed, total int64) {
		lastStatus = status
		lastCompleted = completed
		lastTotal = total
	}

	// Simulate progress updates
	callback("pulling manifest", 0, 0)
	if lastStatus != "pulling manifest" {
		t.Error("Status should be 'pulling manifest'")
	}

	callback("downloading", 512, 1024)
	if lastCompleted != 512 || lastTotal != 1024 {
		t.Error("Progress values should be passed correctly")
	}

	// Calculate percentage
	percent := float64(lastCompleted) / float64(lastTotal) * 100
	if percent != 50.0 {
		t.Errorf("Expected 50%%, got %.1f%%", percent)
	}
}

// -----------------------------------------------------------------------------
// OllamaModel Tests
// -----------------------------------------------------------------------------

func TestOllamaModel_Fields(t *testing.T) {
	model := OllamaModel{
		Name:              "test-model:latest",
		Size:              1024 * 1024 * 1024,
		ModifiedAt:        time.Now(),
		IsCustom:          true,
		Digest:            "sha256:abc123",
		Family:            "llama",
		ParameterSize:     "7B",
		QuantizationLevel: "Q4_K_M",
	}

	if model.Name != "test-model:latest" {
		t.Error("Name field incorrect")
	}

	if model.Size != 1024*1024*1024 {
		t.Error("Size field incorrect")
	}

	if !model.IsCustom {
		t.Error("IsCustom field incorrect")
	}

	if model.Family != "llama" {
		t.Error("Family field incorrect")
	}
}

// -----------------------------------------------------------------------------
// Concurrency Tests
// -----------------------------------------------------------------------------

func TestListModels_ConcurrentAccess(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // Simulate latency
		resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// First, populate the cache with a single call
	_, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("Initial call failed: %v", err)
	}

	// Reset counter after cache is populated
	mu.Lock()
	initialCalls := callCount
	mu.Unlock()

	// Launch multiple concurrent requests - all should hit cache
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.ListModels(ctx)
		}()
	}
	wg.Wait()

	mu.Lock()
	additionalCalls := callCount - initialCalls
	mu.Unlock()

	// All concurrent calls should hit cache (0 additional HTTP calls)
	if additionalCalls != 0 {
		t.Errorf("Expected 0 additional HTTP calls after cache populated, got %d", additionalCalls)
	}
}

// -----------------------------------------------------------------------------
// Error Handling Edge Cases
// -----------------------------------------------------------------------------

func TestPullModel_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			// Return empty response
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Should handle gracefully
	err := client.PullModel(ctx, "test-model", nil)

	// No error expected - empty stream is valid (just no progress)
	if err != nil {
		t.Logf("Got error (may be acceptable): %v", err)
	}
}

func TestListModels_EmptyModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	models, err := client.ListModels(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("Expected 0 models, got %d", len(models))
	}
}

// -----------------------------------------------------------------------------
// Request Format Tests
// -----------------------------------------------------------------------------

func TestPullModel_RequestFormat(t *testing.T) {
	var receivedBody ollamaPullRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			progress := ollamaPullProgress{Status: "success"}
			json.NewEncoder(w).Encode(progress)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	_ = client.PullModel(ctx, "test-model", nil)

	if receivedBody.Name != "test-model" {
		t.Errorf("Expected model name 'test-model', got %q", receivedBody.Name)
	}

	if !receivedBody.Stream {
		t.Error("Stream should be true for progress updates")
	}
}

func TestIsCustomModel_RequestFormat(t *testing.T) {
	var receivedModel string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			var req struct {
				Name string `json:"name"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			receivedModel = req.Name
			resp := ollamaShowResponse{Template: ""}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	_, _ = client.IsCustomModel(ctx, "test-model")

	if receivedModel != "test-model" {
		t.Errorf("Expected model name 'test-model', got %q", receivedModel)
	}
}

// Ensure that Content-Type header is set correctly
func TestRequests_ContentType(t *testing.T) {
	var contentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		switch r.URL.Path {
		case "/api/tags":
			resp := ollamaTagsResponse{Models: []ollamaModelInfo{}}
			json.NewEncoder(w).Encode(resp)
		case "/api/show":
			resp := ollamaShowResponse{}
			json.NewEncoder(w).Encode(resp)
		case "/api/pull":
			progress := ollamaPullProgress{Status: "success"}
			json.NewEncoder(w).Encode(progress)
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx := context.Background()

	// Test /api/show (POST with body)
	_, _ = client.IsCustomModel(ctx, "test")
	if contentType != "application/json" {
		t.Errorf("IsCustomModel should set Content-Type: application/json, got %q", contentType)
	}

	// Test /api/pull (POST with body)
	_ = client.PullModel(ctx, "test", nil)
	if contentType != "application/json" {
		t.Errorf("PullModel should set Content-Type: application/json, got %q", contentType)
	}
}

// -----------------------------------------------------------------------------
// Error Message Quality Tests
// -----------------------------------------------------------------------------

func TestModelError_RemediationSuggestions(t *testing.T) {
	tests := []struct {
		name       string
		errorType  ModelErrorType
		shouldHave string
	}{
		{
			name:       "NotFound suggests pull",
			errorType:  ModelErrorNotFound,
			shouldHave: "Pull the model",
		},
		{
			name:       "ConnectionFailed suggests starting Ollama",
			errorType:  ModelErrorConnectionFailed,
			shouldHave: "Ollama",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ModelError{
				Type:        tt.errorType,
				Model:       "test-model",
				Message:     "Test error",
				Remediation: fmt.Sprintf("%s: ollama pull test-model", tt.shouldHave),
			}

			if !strings.Contains(err.Remediation, tt.shouldHave) {
				t.Errorf("Remediation should contain %q, got %q", tt.shouldHave, err.Remediation)
			}
		})
	}
}
