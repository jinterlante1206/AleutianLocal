// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
)

// =============================================================================
// Mock HTTP Client
// =============================================================================

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	// PostFunc allows customizing POST behavior per test
	PostFunc func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error)
	// GetFunc allows customizing GET behavior per test
	GetFunc func(ctx context.Context, url string) (*http.Response, error)

	// Simple response/error for basic tests
	response *http.Response
	err      error

	// Capture request details for assertions
	lastPostURL     string
	lastPostBody    string
	lastContentType string
	lastGetURL      string
}

func (m *mockHTTPClient) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	m.lastPostURL = url
	m.lastContentType = contentType
	if body != nil {
		bodyBytes, _ := io.ReadAll(body)
		m.lastPostBody = string(bodyBytes)
	}

	if m.PostFunc != nil {
		return m.PostFunc(ctx, url, contentType, body)
	}
	return m.response, m.err
}

func (m *mockHTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	m.lastGetURL = url
	if m.GetFunc != nil {
		return m.GetFunc(ctx, url)
	}
	return m.response, m.err
}

// =============================================================================
// ChatServiceResponse Tests
// =============================================================================

func TestNewChatServiceResponse(t *testing.T) {
	resp := NewChatServiceResponse("Hello", "sess-123", nil)

	if resp.Answer != "Hello" {
		t.Errorf("expected Answer 'Hello', got %q", resp.Answer)
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("expected SessionID 'sess-123', got %q", resp.SessionID)
	}
	if resp.Id == "" {
		t.Error("expected Id to be generated")
	}
	if resp.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
}

func TestNewChatServiceResponse_WithSources(t *testing.T) {
	sources := []ux.SourceInfo{
		{Source: "doc.pdf", Score: 0.95},
	}
	resp := NewChatServiceResponse("Answer", "sess", sources)

	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}
	if resp.Sources[0].Source != "doc.pdf" {
		t.Errorf("expected source 'doc.pdf', got %q", resp.Sources[0].Source)
	}
}

// =============================================================================
// RAG Chat Service Tests
// =============================================================================

func TestNewRAGChatService(t *testing.T) {
	service := NewRAGChatService(RAGChatServiceConfig{
		BaseURL:   "http://localhost:8080",
		Pipeline:  "reranking",
		SessionID: "sess-123",
	})

	if service == nil {
		t.Fatal("NewRAGChatService returned nil")
	}
	if service.GetSessionID() != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", service.GetSessionID())
	}
}

func TestRAGChatService_SendMessage_Success(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"answer":"Hello from RAG","session_id":"sess-new","sources":[{"source":"doc.pdf","score":0.9}]}`)),
		},
	}

	service := NewRAGChatServiceWithClient(mock, RAGChatServiceConfig{
		BaseURL:  "http://localhost:8080",
		Pipeline: "reranking",
	})

	resp, err := service.SendMessage(context.Background(), "What is auth?")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answer != "Hello from RAG" {
		t.Errorf("expected 'Hello from RAG', got %q", resp.Answer)
	}
	if resp.SessionID != "sess-new" {
		t.Errorf("expected session ID 'sess-new', got %q", resp.SessionID)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}

	// Verify request was made to correct URL
	if !strings.Contains(mock.lastPostURL, "/v1/chat/rag") {
		t.Errorf("expected URL to contain /v1/chat/rag, got %q", mock.lastPostURL)
	}
	if mock.lastContentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", mock.lastContentType)
	}
}

func TestRAGChatService_SendMessage_UpdatesSessionID(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"answer":"Answer","session_id":"sess-updated"}`)),
		},
	}

	service := NewRAGChatServiceWithClient(mock, RAGChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	// Initial session should be empty
	if service.GetSessionID() != "" {
		t.Errorf("expected empty initial session ID, got %q", service.GetSessionID())
	}

	_, err := service.SendMessage(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Session should be updated from response
	if service.GetSessionID() != "sess-updated" {
		t.Errorf("expected session ID 'sess-updated', got %q", service.GetSessionID())
	}
}

func TestRAGChatService_SendMessage_NetworkError(t *testing.T) {
	mock := &mockHTTPClient{
		err: errors.New("connection refused"),
	}

	service := NewRAGChatServiceWithClient(mock, RAGChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "http post") {
		t.Errorf("expected 'http post' in error, got %q", err.Error())
	}
}

func TestRAGChatService_SendMessage_ServerError(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader(`Internal Server Error`)),
		},
	}

	service := NewRAGChatServiceWithClient(mock, RAGChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected '500' in error, got %q", err.Error())
	}
}

func TestRAGChatService_SendMessage_InvalidJSON(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`not json`)),
		},
	}

	service := NewRAGChatServiceWithClient(mock, RAGChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("expected 'parse response' in error, got %q", err.Error())
	}
}

func TestRAGChatService_Close(t *testing.T) {
	service := NewRAGChatService(RAGChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	err := service.Close()

	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}
}

// =============================================================================
// Direct Chat Service Tests
// =============================================================================

func TestNewDirectChatService(t *testing.T) {
	service := NewDirectChatService(DirectChatServiceConfig{
		BaseURL:        "http://localhost:8080",
		SessionID:      "sess-123",
		EnableThinking: true,
		BudgetTokens:   4096,
	})

	if service == nil {
		t.Fatal("NewDirectChatService returned nil")
	}
	if service.GetSessionID() != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", service.GetSessionID())
	}
}

func TestDirectChatService_SendMessage_Success(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"answer":"Hello from direct"}`)),
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	resp, err := service.SendMessage(context.Background(), "What is auth?")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Answer != "Hello from direct" {
		t.Errorf("expected 'Hello from direct', got %q", resp.Answer)
	}

	// Direct chat should have no sources
	if len(resp.Sources) != 0 {
		t.Errorf("expected no sources for direct chat, got %d", len(resp.Sources))
	}

	// Verify request was made to correct URL
	if !strings.Contains(mock.lastPostURL, "/v1/chat/direct") {
		t.Errorf("expected URL to contain /v1/chat/direct, got %q", mock.lastPostURL)
	}
}

func TestDirectChatService_SendMessage_MaintainsHistory(t *testing.T) {
	callCount := 0
	mock := &mockHTTPClient{
		PostFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			callCount++
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"answer":"Response ` + string(rune('0'+callCount)) + `"}`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	ctx := context.Background()

	// Send first message
	resp1, _ := service.SendMessage(ctx, "First")
	if resp1.Answer != "Response 1" {
		t.Errorf("expected 'Response 1', got %q", resp1.Answer)
	}

	// Send second message - should include history
	resp2, _ := service.SendMessage(ctx, "Second")
	if resp2.Answer != "Response 2" {
		t.Errorf("expected 'Response 2', got %q", resp2.Answer)
	}

	// Check that request body contains both messages
	if !strings.Contains(mock.lastPostBody, "First") {
		t.Error("expected request body to contain 'First'")
	}
	if !strings.Contains(mock.lastPostBody, "Second") {
		t.Error("expected request body to contain 'Second'")
	}
}

func TestDirectChatService_SendMessage_RemovesMessageOnError(t *testing.T) {
	callCount := 0
	mock := &mockHTTPClient{
		PostFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			callCount++
			if callCount == 2 {
				// Fail on second call
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(strings.NewReader(`Error`)),
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"answer":"OK"}`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	ctx := context.Background()

	// First message succeeds
	service.SendMessage(ctx, "First")

	// Second message fails
	service.SendMessage(ctx, "Failing")

	// Third message - should not contain "Failing" in history
	service.SendMessage(ctx, "Third")

	// The failing message should not be in history
	if strings.Contains(mock.lastPostBody, "Failing") {
		t.Error("expected failed message to be removed from history")
	}
}

func TestDirectChatService_SendMessage_EmptyResponse(t *testing.T) {
	mock := &mockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"answer":""}`)),
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' in error, got %q", err.Error())
	}
}

func TestDirectChatService_SendMessage_NetworkError(t *testing.T) {
	mock := &mockHTTPClient{
		err: errors.New("timeout"),
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.SendMessage(context.Background(), "Hello")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "http post") {
		t.Errorf("expected 'http post' in error, got %q", err.Error())
	}
}

func TestDirectChatService_LoadSessionHistory_Success(t *testing.T) {
	mock := &mockHTTPClient{
		GetFunc: func(ctx context.Context, url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"Get": {
						"Conversation": [
							{"question": "What is auth?", "answer": "Auth is..."},
							{"question": "How to implement?", "answer": "You can..."}
						]
					}
				}`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	turns, err := service.LoadSessionHistory(context.Background(), "sess-resume")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turns != 2 {
		t.Errorf("expected 2 turns, got %d", turns)
	}
	if service.GetSessionID() != "sess-resume" {
		t.Errorf("expected session ID 'sess-resume', got %q", service.GetSessionID())
	}

	// Verify request URL
	if !strings.Contains(mock.lastGetURL, "/v1/sessions/sess-resume/history") {
		t.Errorf("expected URL to contain session ID, got %q", mock.lastGetURL)
	}
}

func TestDirectChatService_LoadSessionHistory_NetworkError(t *testing.T) {
	mock := &mockHTTPClient{
		GetFunc: func(ctx context.Context, url string) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-123")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "http get") {
		t.Errorf("expected 'http get' in error, got %q", err.Error())
	}
}

func TestDirectChatService_LoadSessionHistory_ServerError(t *testing.T) {
	mock := &mockHTTPClient{
		GetFunc: func(ctx context.Context, url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: 404,
				Body:       io.NopCloser(strings.NewReader(`Not Found`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-123")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected '404' in error, got %q", err.Error())
	}
}

func TestDirectChatService_LoadSessionHistory_NoConversation(t *testing.T) {
	mock := &mockHTTPClient{
		GetFunc: func(ctx context.Context, url string) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"Get": {}}`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	_, err := service.LoadSessionHistory(context.Background(), "sess-123")

	if err == nil {
		t.Fatal("expected error for missing conversation data, got nil")
	}
	if !strings.Contains(err.Error(), "no conversation data") {
		t.Errorf("expected 'no conversation data' in error, got %q", err.Error())
	}
}

func TestDirectChatService_Close(t *testing.T) {
	service := NewDirectChatService(DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	err := service.Close()

	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}
}

// =============================================================================
// defaultHTTPClient Tests
// =============================================================================

func TestDefaultHTTPClient_Post(t *testing.T) {
	// This is a smoke test - we can't easily test the real HTTP client
	client := &defaultHTTPClient{
		client: &http.Client{},
	}

	// Just verify it doesn't panic
	_, err := client.Post(context.Background(), "http://invalid.localhost:99999", "application/json", nil)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestDefaultHTTPClient_Get(t *testing.T) {
	client := &defaultHTTPClient{
		client: &http.Client{},
	}

	// Just verify it doesn't panic
	_, err := client.Get(context.Background(), "http://invalid.localhost:99999")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// =============================================================================
// Security Tests
// =============================================================================

func TestDirectChatService_LoadSessionHistory_PathTraversal(t *testing.T) {
	mock := &mockHTTPClient{
		GetFunc: func(ctx context.Context, url string) (*http.Response, error) {
			// Verify URL is escaped
			if strings.Contains(url, "..") && !strings.Contains(url, "%2F") {
				t.Error("expected path traversal to be escaped, but found unescaped '..'")
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"Get": {"Conversation": []}}`)),
			}, nil
		},
	}

	service := NewDirectChatServiceWithClient(mock, DirectChatServiceConfig{
		BaseURL: "http://localhost:8080",
	})

	// Attempt path traversal - should be escaped
	_, _ = service.LoadSessionHistory(context.Background(), "../../../etc/passwd")

	// Verify the URL was escaped
	if strings.Contains(mock.lastGetURL, "../") {
		t.Error("expected path traversal to be escaped")
	}
	if !strings.Contains(mock.lastGetURL, "%2F") {
		t.Error("expected URL to contain escaped slashes")
	}
}
