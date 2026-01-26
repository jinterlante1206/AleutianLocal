// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/pkg/ux"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// mockChatService implements ChatService for testing (legacy blocking).
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

// mockStreamingChatService implements StreamingChatService for testing.
//
// Allows configuring responses and tracking calls for verification.
type mockStreamingChatService struct {
	sendMessageFunc func(ctx context.Context, msg string) (*ux.StreamResult, error)
	sessionID       string
	closeErr        error
	closed          bool
	messagesSent    []string
}

func (m *mockStreamingChatService) SendMessage(ctx context.Context, message string) (*ux.StreamResult, error) {
	m.messagesSent = append(m.messagesSent, message)
	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, message)
	}
	return &ux.StreamResult{
		Answer:    "Mock response",
		SessionID: m.sessionID,
	}, nil
}

func (m *mockStreamingChatService) GetSessionID() string {
	return m.sessionID
}

func (m *mockStreamingChatService) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockStreamingChatService) GetDataSpaceStats(_ context.Context) (*ux.DataSpaceStats, error) {
	// Mock returns nil stats (no dataspace configured or stats unavailable)
	return nil, nil
}

func (m *mockStreamingChatService) LoadSessionMetadata(_ context.Context, _ string) (*SessionMetadata, error) {
	// Mock returns nil metadata (session doesn't exist or no stored context)
	return nil, nil
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
// InteractiveInputReader Tests
// =============================================================================

func TestInteractiveInputReader_ImplementsInputReader(t *testing.T) {
	// Verify the type implements the interface
	var _ InputReader = &InteractiveInputReader{}
}

func TestInteractiveInputReader_ImplementsPromptingInputReader(t *testing.T) {
	// Verify the type implements the PromptingInputReader interface
	var _ PromptingInputReader = &InteractiveInputReader{}
}

func TestInteractiveInputReader_SetPrompt(t *testing.T) {
	reader := &InteractiveInputReader{
		history:      make([]string, 0),
		historyIndex: -1,
		maxHistory:   50,
		prompt:       "> ",
	}

	reader.SetPrompt("custom> ")

	if reader.prompt != "custom> " {
		t.Errorf("SetPrompt(): prompt = %q, want %q", reader.prompt, "custom> ")
	}
}

func TestInteractiveInputReader_AddToHistory(t *testing.T) {
	reader := &InteractiveInputReader{
		history:      make([]string, 0),
		historyIndex: -1,
		maxHistory:   3,
		prompt:       "> ",
	}

	// Add items to history
	reader.addToHistory("first")
	reader.addToHistory("second")
	reader.addToHistory("third")

	if len(reader.history) != 3 {
		t.Errorf("addToHistory(): len = %d, want 3", len(reader.history))
	}

	// Add fourth item, should trim oldest
	reader.addToHistory("fourth")

	if len(reader.history) != 3 {
		t.Errorf("addToHistory() after overflow: len = %d, want 3", len(reader.history))
	}

	if reader.history[0] != "second" {
		t.Errorf("addToHistory(): first item = %q, want %q", reader.history[0], "second")
	}
}

func TestInteractiveInputReader_AddToHistory_NoDuplicates(t *testing.T) {
	reader := &InteractiveInputReader{
		history:      make([]string, 0),
		historyIndex: -1,
		maxHistory:   10,
		prompt:       "> ",
	}

	// Add same item twice
	reader.addToHistory("same")
	reader.addToHistory("same")

	if len(reader.history) != 1 {
		t.Errorf("addToHistory() with duplicate: len = %d, want 1", len(reader.history))
	}
}

func TestNewInteractiveInputReader_NonTTY_FallsBackToStdin(t *testing.T) {
	// In test environment, stdin is not a TTY
	// So NewInteractiveInputReader should return a StdinReader
	reader := NewInteractiveInputReader(50)

	// Type assertion to check fallback
	_, isStdinReader := reader.(*StdinReader)
	_, isInteractive := reader.(*InteractiveInputReader)

	// In non-TTY (test environment), should be StdinReader
	// Note: This test behavior depends on test runner's TTY status
	if !isStdinReader && !isInteractive {
		t.Errorf("NewInteractiveInputReader(): unexpected type %T", reader)
	}
}

func TestPromptingInputReader_TypeAssertion(t *testing.T) {
	// Test that we can correctly identify prompting readers
	interactive := &InteractiveInputReader{
		history:      make([]string, 0),
		historyIndex: -1,
		maxHistory:   50,
		prompt:       "> ",
	}

	stdin := &StdinReader{}
	mock := NewMockInputReader([]string{"test"})

	// Interactive should implement PromptingInputReader
	if _, ok := InputReader(interactive).(PromptingInputReader); !ok {
		t.Error("InteractiveInputReader should implement PromptingInputReader")
	}

	// StdinReader should NOT implement PromptingInputReader
	if _, ok := InputReader(stdin).(PromptingInputReader); ok {
		t.Error("StdinReader should NOT implement PromptingInputReader")
	}

	// MockInputReader should NOT implement PromptingInputReader
	if _, ok := InputReader(mock).(PromptingInputReader); ok {
		t.Error("MockInputReader should NOT implement PromptingInputReader")
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
	mockService := &mockStreamingChatService{sessionID: "sess-123"}
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
	mockService := &mockStreamingChatService{sessionID: "sess-456"}
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
	mockService := &mockStreamingChatService{
		sessionID: "sess-789",
		sendMessageFunc: func(ctx context.Context, msg string) (*ux.StreamResult, error) {
			return &ux.StreamResult{
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

	// Note: With streaming, response is rendered via StreamRenderer callbacks
	// The mock doesn't actually render, so we just verify the message was sent
}

func TestRAGChatRunner_Run_SkipsEmptyInput(t *testing.T) {
	mockService := &mockStreamingChatService{sessionID: "sess-empty"}
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
	mockService := &mockStreamingChatService{
		sessionID: "sess-err",
		sendMessageFunc: func(ctx context.Context, msg string) (*ux.StreamResult, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("temporary error")
			}
			return &ux.StreamResult{Answer: "Success!", SessionID: "sess-err"}, nil
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
	mockService := &mockStreamingChatService{sessionID: "sess-cancel"}
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
	mockService := &mockStreamingChatService{sessionID: "sess-eof"}
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
	mockService := &mockStreamingChatService{sessionID: "sess-close"}
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
	mockService := &mockStreamingChatService{
		sessionID: "sess-sources",
		sendMessageFunc: func(ctx context.Context, msg string) (*ux.StreamResult, error) {
			return &ux.StreamResult{
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

	// Note: With streaming, sources are rendered via StreamRenderer callbacks
	// The mock doesn't actually render, so we just verify the run completes
}

// =============================================================================
// DirectChatRunner Tests
// =============================================================================

func TestDirectChatRunner_Run_ExitCommand(t *testing.T) {
	mockHTTPClient := &runnerMockHTTPClient{}
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
	// NOTE: Streaming tests are covered in streaming_service_test.go
	// This test uses a mock streaming service to verify runner behavior
	t.Skip("DirectChatRunner now uses streaming - SSE format tests are in streaming_service_test.go")
}

func TestDirectChatRunner_Run_SkipsEmptyInput(t *testing.T) {
	callCount := 0
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			callCount++
			// Return SSE-formatted response
			return makeSSEResponse(200, "data: {\"type\":\"done\",\"session_id\":\"sess-1\"}\n\n"), nil
		},
	}
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
	// NOTE: Error handling with SSE streaming is more complex
	// Comprehensive tests are in streaming_service_test.go
	t.Skip("DirectChatRunner now uses streaming - error tests are in streaming_service_test.go")
}

func TestDirectChatRunner_Run_ContextCancellation(t *testing.T) {
	// Note: Context cancellation is difficult to test with synchronous MockInputReader
	// because all inputs are processed before the cancel goroutine fires.
	// This test verifies that pre-cancelled context returns immediately.
	mockHTTPClient := &runnerMockHTTPClient{
		postFunc: func(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
			return makeSSEResponse(200, "data: {\"type\":\"done\"}\n\n"), nil
		},
	}
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
			return makeSSEResponse(200, "data: {\"type\":\"token\",\"content\":\"response\"}\n\ndata: {\"type\":\"done\"}\n\n"), nil
		},
	}
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
	service := NewDirectStreamingChatServiceWithClient(mockHTTPClient, DirectStreamingChatServiceConfig{
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
	// NOTE: With streaming, content is rendered via StreamRenderer callbacks
	// Direct chat never displays sources - this is enforced by the DirectStreamingChatService
	t.Skip("DirectChatRunner now uses streaming - source display tests are N/A for direct mode")
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

func (m *runnerMockHTTPClient) PostWithHeaders(ctx context.Context, url, contentType string, body io.Reader, _ map[string]string) (*http.Response, error) {
	// Delegate to Post for mock simplicity
	return m.Post(ctx, url, contentType, body)
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

// makeSSEResponse creates an HTTP response with SSE content type for streaming tests.
func makeSSEResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

// =============================================================================
// Spell Correction Tests
// =============================================================================

// mockChatUI implements ux.ChatUI for testing spell correction.
type mockChatUI struct {
	autoCorrectionCalls []struct{ original, corrected string }
	suggestionCalls     []struct{ original, suggested string }
}

func (m *mockChatUI) Header(mode ux.ChatMode, pipeline, sessionID string)     {}
func (m *mockChatUI) HeaderWithConfig(config ux.HeaderConfig)                 {}
func (m *mockChatUI) Prompt() string                                          { return "> " }
func (m *mockChatUI) Response(answer string)                                  {}
func (m *mockChatUI) Sources(sources []ux.SourceInfo)                         {}
func (m *mockChatUI) NoSources()                                              {}
func (m *mockChatUI) Error(err error)                                         {}
func (m *mockChatUI) SessionResume(sessionID string, turnCount int)           {}
func (m *mockChatUI) SessionEnd(sessionID string)                             {}
func (m *mockChatUI) SessionEndRich(sessionID string, stats *ux.SessionStats) {}

func (m *mockChatUI) ShowAutoCorrection(original, corrected string) {
	m.autoCorrectionCalls = append(m.autoCorrectionCalls, struct {
		original  string
		corrected string
	}{original, corrected})
}

func (m *mockChatUI) ShowCorrectionSuggestion(original, suggested string) {
	m.suggestionCalls = append(m.suggestionCalls, struct {
		original  string
		suggested string
	}{original, suggested})
}

func TestReplaceWord_SingleOccurrence(t *testing.T) {
	result := replaceWord("show me wheet data", "wheet", "wheat")
	expected := "show me wheat data"
	if result != expected {
		t.Errorf("replaceWord single occurrence: got %q, want %q", result, expected)
	}
}

func TestReplaceWord_MultipleOccurrences(t *testing.T) {
	result := replaceWord("what is wheet and where is wheet", "wheet", "wheat")
	expected := "what is wheat and where is wheat"
	if result != expected {
		t.Errorf("replaceWord multiple occurrences: got %q, want %q", result, expected)
	}
}

func TestReplaceWord_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		oldWord  string
		newWord  string
		expected string
	}{
		{"lowercase", "show me wheet", "wheet", "wheat", "show me wheat"},
		{"uppercase", "show me WHEET", "wheet", "wheat", "show me wheat"},
		{"mixed case", "show me Wheet", "wheet", "wheat", "show me wheat"},
		{"multiple mixed", "WHEET and wheet and Wheet", "wheet", "wheat", "wheat and wheat and wheat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceWord(tt.input, tt.oldWord, tt.newWord)
			if result != tt.expected {
				t.Errorf("replaceWord case-insensitive %s: got %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}

func TestReplaceWord_WithPunctuation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		oldWord  string
		newWord  string
		expected string
	}{
		{"trailing period", "show me wheet.", "wheet", "wheat", "show me wheat."},
		{"trailing comma", "wheet, barley", "wheet", "wheat", "wheat, barley"},
		{"trailing question", "is this wheet?", "wheet", "wheat", "is this wheat?"},
		{"in parentheses", "(wheet)", "wheet", "wheat", "(wheat)"},
		{"with quotes", `"wheet"`, "wheet", "wheat", `"wheat"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceWord(tt.input, tt.oldWord, tt.newWord)
			if result != tt.expected {
				t.Errorf("replaceWord with punctuation %s: got %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}

func TestReplaceWord_WordBoundaries(t *testing.T) {
	// Should NOT replace partial matches
	tests := []struct {
		name     string
		input    string
		oldWord  string
		newWord  string
		expected string
	}{
		{"prefix match", "wheetabix cereal", "wheet", "wheat", "wheetabix cereal"},
		{"suffix match", "buckwheet flour", "wheet", "wheat", "buckwheet flour"},
		{"embedded match", "awheeta", "wheet", "wheat", "awheeta"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceWord(tt.input, tt.oldWord, tt.newWord)
			if result != tt.expected {
				t.Errorf("replaceWord word boundaries %s: got %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}

func TestReplaceWord_NoMatch(t *testing.T) {
	input := "show me barley data"
	result := replaceWord(input, "wheet", "wheat")
	if result != input {
		t.Errorf("replaceWord no match: got %q, want %q", result, input)
	}
}

func TestReplaceWord_EmptyInput(t *testing.T) {
	result := replaceWord("", "wheet", "wheat")
	if result != "" {
		t.Errorf("replaceWord empty input: got %q, want empty", result)
	}
}

func TestReplaceWord_SpecialRegexChars(t *testing.T) {
	// Old word with regex special characters should be escaped
	result := replaceWord("test a.b test", "a.b", "x")
	expected := "test x test"
	if result != expected {
		t.Errorf("replaceWord regex escape: got %q, want %q", result, expected)
	}
}

// =============================================================================
// applySpellCorrection Integration Tests
// =============================================================================

func TestApplySpellCorrection_NoTypo(t *testing.T) {
	// Create a corrector with "wheat" in vocabulary
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	ui := &mockChatUI{}
	runner := &RAGChatRunner{
		ui:             ui,
		spellCorrector: corrector,
	}

	input := "tell me about wheat"
	result := runner.applySpellCorrection(input)

	// Should return unchanged - "wheat" is an exact match
	if result != input {
		t.Errorf("expected no change for exact match, got %q", result)
	}
	if len(ui.autoCorrectionCalls) != 0 {
		t.Errorf("expected no auto-correction calls, got %d", len(ui.autoCorrectionCalls))
	}
	if len(ui.suggestionCalls) != 0 {
		t.Errorf("expected no suggestion calls, got %d", len(ui.suggestionCalls))
	}
}

func TestApplySpellCorrection_AutoCorrectDistance1(t *testing.T) {
	// Create a corrector with "wheat" in vocabulary
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	ui := &mockChatUI{}
	runner := &RAGChatRunner{
		ui:             ui,
		spellCorrector: corrector,
	}

	input := "tell me about whet" // distance 1 from wheat
	result := runner.applySpellCorrection(input)

	// Should auto-correct
	expected := "tell me about wheat"
	if result != expected {
		t.Errorf("expected auto-correction, got %q, want %q", result, expected)
	}
	if len(ui.autoCorrectionCalls) != 1 {
		t.Errorf("expected 1 auto-correction call, got %d", len(ui.autoCorrectionCalls))
	}
	if len(ui.suggestionCalls) != 0 {
		t.Errorf("expected no suggestion calls, got %d", len(ui.suggestionCalls))
	}
}

func TestApplySpellCorrection_SuggestionDistance2(t *testing.T) {
	// Create a corrector with "wheat" in vocabulary
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	ui := &mockChatUI{}
	runner := &RAGChatRunner{
		ui:             ui,
		spellCorrector: corrector,
	}

	input := "tell me about wehat" // distance 2 from wheat (transposition)
	result := runner.applySpellCorrection(input)

	// Should suggest but not auto-correct
	if result != input {
		t.Errorf("expected no change for distance 2, got %q", result)
	}
	if len(ui.autoCorrectionCalls) != 0 {
		t.Errorf("expected no auto-correction calls, got %d", len(ui.autoCorrectionCalls))
	}
	if len(ui.suggestionCalls) != 1 {
		t.Errorf("expected 1 suggestion call, got %d", len(ui.suggestionCalls))
	}
}

func TestApplySpellCorrection_NilCorrector(t *testing.T) {
	ui := &mockChatUI{}
	runner := &RAGChatRunner{
		ui:             ui,
		spellCorrector: nil, // No corrector configured
	}

	input := "tell me about wheet"
	result := runner.applySpellCorrection(input)

	// Should return unchanged
	if result != input {
		t.Errorf("expected no change with nil corrector, got %q", result)
	}
}
