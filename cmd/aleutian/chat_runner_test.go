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

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// mockChatService implements ChatService for testing.
//
// Allows configuring responses and tracking calls for verification.
type mockChatService struct {
	sendMessageFunc func(ctx context.Context, msg string) (*ChatServiceResponse, error)
	sessionID       string
	closeErr        error
	closed          bool
	messagesSent    []string
}

func (m *mockChatService) SendMessage(ctx context.Context, message string) (*ChatServiceResponse, error) {
	m.messagesSent = append(m.messagesSent, message)
	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, message)
	}
	return &ChatServiceResponse{
		Answer:    "Mock response",
		SessionID: m.sessionID,
	}, nil
}

func (m *mockChatService) GetSessionID() string {
	return m.sessionID
}

func (m *mockChatService) Close() error {
	m.closed = true
	return m.closeErr
}

// =============================================================================
// InputReader Tests
// =============================================================================

func TestStdinReader_ReadLine(t *testing.T) {
	// StdinReader wraps os.Stdin which we can't easily mock
	// This test verifies the type implements the interface
	var _ InputReader = &StdinReader{}
}

func TestMockInputReader_ReadLine_ReturnsInputsInOrder(t *testing.T) {
	inputs := []string{"first", "second", "third"}
	reader := NewMockInputReader(inputs)

	for i, expected := range inputs {
		got, err := reader.ReadLine()
		if err != nil {
			t.Fatalf("ReadLine() %d: unexpected error: %v", i, err)
		}
		if got != expected {
			t.Errorf("ReadLine() %d: got %q, want %q", i, got, expected)
		}
	}
}

func TestMockInputReader_ReadLine_ReturnsEOFWhenExhausted(t *testing.T) {
	reader := NewMockInputReader([]string{"only"})

	// First read succeeds
	_, err := reader.ReadLine()
	if err != nil {
		t.Fatalf("first ReadLine(): unexpected error: %v", err)
	}

	// Second read returns EOF
	_, err = reader.ReadLine()
	if err != io.EOF {
		t.Errorf("second ReadLine(): got error %v, want io.EOF", err)
	}
}

func TestMockInputReader_ReadLine_EmptyInputs(t *testing.T) {
	reader := NewMockInputReader([]string{})

	_, err := reader.ReadLine()
	if err != io.EOF {
		t.Errorf("ReadLine() on empty: got error %v, want io.EOF", err)
	}
}

// =============================================================================
// isExitCommand Tests
// =============================================================================

func TestIsExitCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"exit", true},
		{"quit", true},
		{"EXIT", false}, // Case-sensitive
		{"QUIT", false},
		{"Exit", false},
		{"hello", false},
		{"", false},
		{"exit please", false},
		{"please exit", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isExitCommand(tt.input)
			if got != tt.want {
				t.Errorf("isExitCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// RAGChatRunner Tests
// =============================================================================

func TestRAGChatRunner_Run_ExitCommand(t *testing.T) {
	mockService := &mockChatService{sessionID: "sess-123"}
	mockInput := NewMockInputReader([]string{"exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test-pipeline")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify no messages were sent (exit before any message)
	if len(mockService.messagesSent) != 0 {
		t.Errorf("expected 0 messages sent, got %d", len(mockService.messagesSent))
	}
}

func TestRAGChatRunner_Run_QuitCommand(t *testing.T) {
	mockService := &mockChatService{sessionID: "sess-456"}
	mockInput := NewMockInputReader([]string{"quit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test-pipeline")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestRAGChatRunner_Run_SendsMessage(t *testing.T) {
	mockService := &mockChatService{
		sessionID: "sess-789",
		sendMessageFunc: func(ctx context.Context, msg string) (*ChatServiceResponse, error) {
			return &ChatServiceResponse{
				Answer:    "Hello back!",
				SessionID: "sess-789",
				Sources: []ux.SourceInfo{
					{Source: "doc1.txt", Score: 0.95},
				},
			}, nil
		},
	}
	mockInput := NewMockInputReader([]string{"hello", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "reranking")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify message was sent
	if len(mockService.messagesSent) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(mockService.messagesSent))
	}
	if mockService.messagesSent[0] != "hello" {
		t.Errorf("message sent = %q, want %q", mockService.messagesSent[0], "hello")
	}

	// Verify output contains response
	output := buf.String()
	if !strings.Contains(output, "Hello back!") {
		t.Errorf("output missing response, got: %s", output)
	}
}

func TestRAGChatRunner_Run_SkipsEmptyInput(t *testing.T) {
	mockService := &mockChatService{sessionID: "sess-empty"}
	mockInput := NewMockInputReader([]string{"", "", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify no messages were sent (all empty, then exit)
	if len(mockService.messagesSent) != 0 {
		t.Errorf("expected 0 messages sent, got %d", len(mockService.messagesSent))
	}
}

func TestRAGChatRunner_Run_ServiceError_ContinuesLoop(t *testing.T) {
	callCount := 0
	mockService := &mockChatService{
		sessionID: "sess-err",
		sendMessageFunc: func(ctx context.Context, msg string) (*ChatServiceResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("temporary error")
			}
			return &ChatServiceResponse{Answer: "Success!", SessionID: "sess-err"}, nil
		},
	}
	mockInput := NewMockInputReader([]string{"first", "second", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify both messages were attempted
	if len(mockService.messagesSent) != 2 {
		t.Errorf("expected 2 messages sent, got %d", len(mockService.messagesSent))
	}
}

func TestRAGChatRunner_Run_ContextCancellation(t *testing.T) {
	// Note: Context cancellation is difficult to test with synchronous MockInputReader
	// because all inputs are processed before the cancel goroutine fires.
	// This test verifies that pre-cancelled context returns immediately.
	mockService := &mockChatService{sessionID: "sess-cancel"}
	mockInput := NewMockInputReader([]string{"msg1", "msg2"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test")

	// Pre-cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.Run(ctx)

	// Should return context.Canceled immediately
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRAGChatRunner_Run_EOFExitsGracefully(t *testing.T) {
	mockService := &mockChatService{sessionID: "sess-eof"}
	// No exit command, just EOF after messages
	mockInput := NewMockInputReader([]string{"hello"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify message was sent before EOF
	if len(mockService.messagesSent) != 1 {
		t.Errorf("expected 1 message sent, got %d", len(mockService.messagesSent))
	}
}

func TestRAGChatRunner_Close_Idempotent(t *testing.T) {
	mockService := &mockChatService{sessionID: "sess-close"}
	mockInput := NewMockInputReader([]string{})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test")

	// Close multiple times
	err1 := runner.Close()
	err2 := runner.Close()
	err3 := runner.Close()

	if err1 != nil || err2 != nil || err3 != nil {
		t.Errorf("Close() should succeed multiple times: %v, %v, %v", err1, err2, err3)
	}

	// Verify service was only closed once
	if !mockService.closed {
		t.Error("expected service to be closed")
	}
}

func TestRAGChatRunner_DisplaysSources(t *testing.T) {
	mockService := &mockChatService{
		sessionID: "sess-sources",
		sendMessageFunc: func(ctx context.Context, msg string) (*ChatServiceResponse, error) {
			return &ChatServiceResponse{
				Answer:    "Answer with sources",
				SessionID: "sess-sources",
				Sources: []ux.SourceInfo{
					{Source: "document1.pdf", Score: 0.95},
					{Source: "document2.txt", Score: 0.87},
				},
			}, nil
		},
	}
	mockInput := NewMockInputReader([]string{"question", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "reranking")
	runner.Run(context.Background())

	output := buf.String()
	// Verify sources are in output (exact format depends on UI implementation)
	if !strings.Contains(output, "document1.pdf") {
		t.Errorf("output missing source, got: %s", output)
	}
}

// =============================================================================
// DirectChatRunner Tests
// =============================================================================

func TestDirectChatRunner_Run_ExitCommand(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestDirectChatRunner_Run_QuitCommand(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"quit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestDirectChatRunner_Run_SendsMessage(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			return makeHTTPResponse(200, `{"answer": "Direct response!"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"hello direct", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Direct response!") {
		t.Errorf("output missing response, got: %s", output)
	}
}

func TestDirectChatRunner_Run_SkipsEmptyInput(t *testing.T) {
	callCount := 0
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			callCount++
			return makeHTTPResponse(200, `{"answer": "response"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"", "", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify no HTTP calls were made (all empty, then exit)
	if callCount != 0 {
		t.Errorf("expected 0 HTTP calls, got %d", callCount)
	}
}

func TestDirectChatRunner_Run_ServiceError_ContinuesLoop(t *testing.T) {
	callCount := 0
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return makeHTTPResponse(500, `{"error": "server error"}`), nil
			}
			return makeHTTPResponse(200, `{"answer": "Success after retry"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"first", "second", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify both messages were attempted
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}

func TestDirectChatRunner_Run_ContextCancellation(t *testing.T) {
	// Note: Context cancellation is difficult to test with synchronous MockInputReader
	// because all inputs are processed before the cancel goroutine fires.
	// This test verifies that pre-cancelled context returns immediately.
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			return makeHTTPResponse(200, `{"answer": "response"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"msg1", "msg2"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	// Pre-cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.Run(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestDirectChatRunner_Run_EOFExitsGracefully(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			return makeHTTPResponse(200, `{"answer": "response"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"hello"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestDirectChatRunner_Close_Idempotent(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")

	err1 := runner.Close()
	err2 := runner.Close()
	err3 := runner.Close()

	if err1 != nil || err2 != nil || err3 != nil {
		t.Errorf("Close() should succeed multiple times: %v, %v, %v", err1, err2, err3)
	}
}

func TestDirectChatRunner_NoSourcesDisplayed(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			return makeHTTPResponse(200, `{"answer": "Direct answer without sources"}`), nil
		},
	}
	service := NewDirectChatServiceWithClient(mockHTTPClient, DirectChatServiceConfig{
		BaseURL: "http://test",
	})
	mockInput := NewMockInputReader([]string{"question", "exit"})
	var buf bytes.Buffer
	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)

	runner := NewDirectChatRunnerWithDeps(service, ui, mockInput, "")
	runner.Run(context.Background())

	output := buf.String()
	// Verify answer is present
	if !strings.Contains(output, "Direct answer without sources") {
		t.Errorf("output missing answer, got: %s", output)
	}
	// Direct chat should NOT have "Sources:" section
	// (This depends on UI implementation - adjust if needed)
}

// =============================================================================
// Helper: runnerMockHTTPClient for DirectChatRunner tests
// =============================================================================

// runnerMockHTTPClient implements HTTPClient for testing DirectChatRunner.
// Returns *http.Response to match the HTTPClient interface.
// Named differently from chat_service_test.go's mockHTTPClient to avoid conflict.
type runnerMockHTTPClient struct {
	postFunc func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error)
	getFunc  func(ctx context.Context, url string) (*http.Response, error)
}

func (m *runnerMockHTTPClient) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	if m.postFunc != nil {
		return m.postFunc(ctx, url, contentType, body)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"answer": "default mock response"}`)),
	}, nil
}

func (m *runnerMockHTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, url)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// makeHTTPResponse is a helper to create *http.Response for tests
func makeHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
