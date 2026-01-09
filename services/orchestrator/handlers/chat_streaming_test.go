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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Setup
// =============================================================================

// StreamingMockLLMClient implements llm.LLMClient for streaming handler testing.
//
// # Description
//
// Provides configurable mock for testing streaming chat handlers.
// Allows simulating token-by-token streaming and errors.
type StreamingMockLLMClient struct {
	// StreamTokens are the tokens to emit during ChatStream
	StreamTokens []string
	// StreamError is returned as error by ChatStream
	StreamError error
	// ChatStreamCallCount tracks how many times ChatStream was called
	ChatStreamCallCount int
	// LastMessages stores the last messages passed to ChatStream
	LastMessages []datatypes.Message
}

// Chat implements llm.LLMClient.Chat for testing.
func (m *StreamingMockLLMClient) Chat(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams) (string, error) {
	return strings.Join(m.StreamTokens, ""), nil
}

// Generate implements llm.LLMClient.Generate for testing.
func (m *StreamingMockLLMClient) Generate(ctx context.Context, prompt string, params llm.GenerationParams) (string, error) {
	return "", nil
}

// ChatStream implements llm.LLMClient.ChatStream for testing.
// Emits configured tokens one by one.
func (m *StreamingMockLLMClient) ChatStream(ctx context.Context, messages []datatypes.Message, params llm.GenerationParams, callback llm.StreamCallback) error {
	m.ChatStreamCallCount++
	m.LastMessages = messages

	for _, token := range m.StreamTokens {
		if err := callback(llm.StreamEvent{Type: llm.StreamEventToken, Content: token}); err != nil {
			return err
		}
	}

	return m.StreamError
}

// createTestStreamingChatHandler creates a StreamingChatHandler with mock dependencies.
func createTestStreamingChatHandler(t *testing.T, mockLLM *StreamingMockLLMClient) StreamingChatHandler {
	t.Helper()

	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err, "policy engine should initialize")

	return NewStreamingChatHandler(mockLLM, pe, nil, nil)
}

// =============================================================================
// NewStreamingChatHandler Tests
// =============================================================================

// TestNewStreamingChatHandler_PanicsOnNilLLMClient verifies that NewStreamingChatHandler
// panics when llmClient is nil.
func TestNewStreamingChatHandler_PanicsOnNilLLMClient(t *testing.T) {
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	assert.Panics(t, func() {
		NewStreamingChatHandler(nil, pe, nil, nil)
	}, "should panic on nil llmClient")
}

// TestNewStreamingChatHandler_PanicsOnNilPolicyEngine verifies that NewStreamingChatHandler
// panics when policyEngine is nil.
func TestNewStreamingChatHandler_PanicsOnNilPolicyEngine(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}

	assert.Panics(t, func() {
		NewStreamingChatHandler(mockLLM, nil, nil, nil)
	}, "should panic on nil policyEngine")
}

// TestNewStreamingChatHandler_Success verifies that NewStreamingChatHandler
// creates a valid handler when all dependencies are provided.
func TestNewStreamingChatHandler_Success(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	handler := NewStreamingChatHandler(mockLLM, pe, nil, nil)

	assert.NotNil(t, handler, "handler should not be nil")
}

// =============================================================================
// HandleDirectChatStream Tests
// =============================================================================

// TestHandleDirectChatStream_InvalidRequestBody verifies that the handler
// returns 400 when the request body is invalid JSON.
func TestHandleDirectChatStream_InvalidRequestBody(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "should return 400 for invalid JSON")
}

// TestHandleDirectChatStream_ValidationFailure verifies that the handler
// returns 400 when the request fails validation.
func TestHandleDirectChatStream_ValidationFailure(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	// Request with empty messages (fails validation)
	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages:  []datatypes.Message{},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "should return 400 for validation failure")
}

// TestHandleDirectChatStream_PolicyViolation verifies that the handler
// returns 403 when the user message contains sensitive data.
func TestHandleDirectChatStream_PolicyViolation(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	// Request with sensitive data (SSN)
	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "My SSN is 123-45-6789"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "should return 403 for policy violation")
}

// TestHandleDirectChatStream_Success verifies that the handler streams
// tokens correctly for a valid request.
func TestHandleDirectChatStream_Success(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{
		StreamTokens: []string{"Hello", " ", "world", "!"},
	}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "should return 200")
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"), "should set SSE content type")

	// Parse SSE events
	events := parseSSEEvents(t, w.Body.String())
	assert.True(t, len(events) >= 2, "should have at least status and done events")

	// Verify LLM was called
	assert.Equal(t, 1, mockLLM.ChatStreamCallCount, "ChatStream should be called once")
}

// TestHandleDirectChatStream_SSEHeaders verifies that the handler sets
// correct SSE headers.
func TestHandleDirectChatStream_SSEHeaders(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{
		StreamTokens: []string{"test"},
	}
	handler := createTestStreamingChatHandler(t, mockLLM)

	router := gin.New()
	router.POST("/v1/chat/direct/stream", handler.HandleDirectChatStream)

	reqBody := datatypes.DirectChatRequest{
		RequestID: uuid.New().String(),
		Timestamp: time.Now().UnixMilli(),
		Messages: []datatypes.Message{
			{Role: "user", Content: "test"},
		},
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/direct/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
}

// =============================================================================
// HandleChatRAGStream Tests
// =============================================================================

// TestHandleChatRAGStream_NoRAGService verifies that the handler returns 500
// when RAG service is not configured.
func TestHandleChatRAGStream_NoRAGService(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	// Create handler WITHOUT RAG service
	handler := NewStreamingChatHandler(mockLLM, pe, nil, nil)

	router := gin.New()
	router.POST("/v1/chat/rag/stream", handler.HandleChatRAGStream)

	reqBody := datatypes.ChatRAGRequest{
		Message: "test query",
	}
	jsonBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/v1/chat/rag/stream", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "should return 500 when RAG service not available")
}

// TestHandleChatRAGStream_InvalidRequestBody verifies that the handler
// returns 500 for invalid JSON (RAG service check happens first).
func TestHandleChatRAGStream_InvalidRequestBody(t *testing.T) {
	mockLLM := &StreamingMockLLMClient{}
	pe, err := policy_engine.NewPolicyEngine()
	require.NoError(t, err)

	// Without RAG service, should return 500 before parsing body
	handler := NewStreamingChatHandler(mockLLM, pe, nil, nil)

	router := gin.New()
	router.POST("/v1/chat/rag/stream", handler.HandleChatRAGStream)

	req, _ := http.NewRequest("POST", "/v1/chat/rag/stream", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 500 because RAG service is nil (checked before parsing)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// NOTE: Testing RAG policy violation requires a mock RAG service
// which is more complex to set up. This would be an integration test.
// The policy check in HandleChatRAGStream is tested implicitly through
// the direct chat tests since they share the same policy engine logic.

// =============================================================================
// Session History Tests
// =============================================================================

// TestWeaviateConversationResponse_JSONUnmarshal verifies the typed response struct
// correctly parses Weaviate's GraphQL response format.
func TestWeaviateConversationResponse_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"Get": {
			"Conversation": [
				{"question": "What is OAuth?", "answer": "OAuth is an authorization framework.", "timestamp": 1704067200000},
				{"question": "How does it work?", "answer": "It uses tokens for access.", "timestamp": 1704067300000}
			]
		}
	}`

	var resp WeaviateConversationResponse
	err := json.Unmarshal([]byte(jsonData), &resp)

	require.NoError(t, err, "should unmarshal without error")
	assert.Len(t, resp.Get.Conversation, 2, "should have 2 conversation turns")
	assert.Equal(t, "What is OAuth?", resp.Get.Conversation[0].Question)
	assert.Equal(t, "OAuth is an authorization framework.", resp.Get.Conversation[0].Answer)
	assert.Equal(t, int64(1704067200000), resp.Get.Conversation[0].Timestamp)
}

// TestWeaviateConversationResponse_EmptyConversation verifies empty conversation handling.
func TestWeaviateConversationResponse_EmptyConversation(t *testing.T) {
	jsonData := `{"Get": {"Conversation": []}}`

	var resp WeaviateConversationResponse
	err := json.Unmarshal([]byte(jsonData), &resp)

	require.NoError(t, err)
	assert.Empty(t, resp.Get.Conversation, "should have no conversation turns")
}

// TestFilterValidTurns_AllValid verifies all valid turns are preserved.
func TestFilterValidTurns_AllValid(t *testing.T) {
	handler := &streamingChatHandler{}
	turns := []ConversationTurn{
		{Question: "Q1", Answer: "A1", Timestamp: 1000},
		{Question: "Q2", Answer: "A2", Timestamp: 2000},
	}

	result := handler.filterValidTurns(turns)

	assert.Len(t, result, 2, "should keep all valid turns")
	assert.Equal(t, "Q1", result[0].Question)
	assert.Equal(t, "Q2", result[1].Question)
}

// TestFilterValidTurns_FiltersEmptyQuestion verifies turns with empty questions are filtered.
func TestFilterValidTurns_FiltersEmptyQuestion(t *testing.T) {
	handler := &streamingChatHandler{}
	turns := []ConversationTurn{
		{Question: "", Answer: "A1", Timestamp: 1000},
		{Question: "Q2", Answer: "A2", Timestamp: 2000},
	}

	result := handler.filterValidTurns(turns)

	assert.Len(t, result, 1, "should filter out turn with empty question")
	assert.Equal(t, "Q2", result[0].Question)
}

// TestFilterValidTurns_FiltersEmptyAnswer verifies turns with empty answers are filtered.
func TestFilterValidTurns_FiltersEmptyAnswer(t *testing.T) {
	handler := &streamingChatHandler{}
	turns := []ConversationTurn{
		{Question: "Q1", Answer: "", Timestamp: 1000},
		{Question: "Q2", Answer: "A2", Timestamp: 2000},
	}

	result := handler.filterValidTurns(turns)

	assert.Len(t, result, 1, "should filter out turn with empty answer")
	assert.Equal(t, "Q2", result[0].Question)
}

// TestFilterValidTurns_EmptyInput verifies empty input returns empty output.
func TestFilterValidTurns_EmptyInput(t *testing.T) {
	handler := &streamingChatHandler{}
	turns := []ConversationTurn{}

	result := handler.filterValidTurns(turns)

	assert.Empty(t, result, "should return empty slice for empty input")
}

// TestBuildRAGMessagesWithHistory_NoHistory verifies message building without history.
func TestBuildRAGMessagesWithHistory_NoHistory(t *testing.T) {
	handler := &streamingChatHandler{}
	ragContext := "Document about OAuth."
	userMessage := "What is OAuth?"
	history := []ConversationTurn{}

	messages := handler.buildRAGMessagesWithHistory(ragContext, userMessage, history)

	assert.Len(t, messages, 2, "should have system and user messages only")
	assert.Equal(t, "system", messages[0].Role)
	assert.Contains(t, messages[0].Content, ragContext)
	assert.Equal(t, "user", messages[1].Role)
	assert.Equal(t, userMessage, messages[1].Content)
}

// TestBuildRAGMessagesWithHistory_WithHistory verifies message building with history.
func TestBuildRAGMessagesWithHistory_WithHistory(t *testing.T) {
	handler := &streamingChatHandler{}
	ragContext := "Document about OAuth."
	userMessage := "How does it compare to SAML?"
	history := []ConversationTurn{
		{Question: "What is OAuth?", Answer: "OAuth is an authorization framework."},
		{Question: "What about OIDC?", Answer: "OIDC builds on OAuth."},
	}

	messages := handler.buildRAGMessagesWithHistory(ragContext, userMessage, history)

	// Expected: system + (2 history * 2) + current user = 6 messages
	assert.Len(t, messages, 6, "should have system + history + current user")

	assert.Equal(t, "system", messages[0].Role)
	assert.Contains(t, messages[0].Content, ragContext)

	// First history turn
	assert.Equal(t, "user", messages[1].Role)
	assert.Equal(t, "What is OAuth?", messages[1].Content)
	assert.Equal(t, "assistant", messages[2].Role)
	assert.Equal(t, "OAuth is an authorization framework.", messages[2].Content)

	// Second history turn
	assert.Equal(t, "user", messages[3].Role)
	assert.Equal(t, "What about OIDC?", messages[3].Content)
	assert.Equal(t, "assistant", messages[4].Role)
	assert.Equal(t, "OIDC builds on OAuth.", messages[4].Content)

	// Current user message
	assert.Equal(t, "user", messages[5].Role)
	assert.Equal(t, userMessage, messages[5].Content)
}

// TestBuildRAGMessagesWithHistory_HistoryOrder verifies history is in correct order.
func TestBuildRAGMessagesWithHistory_HistoryOrder(t *testing.T) {
	handler := &streamingChatHandler{}
	history := []ConversationTurn{
		{Question: "First", Answer: "First answer"},
		{Question: "Second", Answer: "Second answer"},
		{Question: "Third", Answer: "Third answer"},
	}

	messages := handler.buildRAGMessagesWithHistory("ctx", "current", history)

	// Verify order: system, then history in order, then current
	assert.Equal(t, "First", messages[1].Content)
	assert.Equal(t, "Second", messages[3].Content)
	assert.Equal(t, "Third", messages[5].Content)
	assert.Equal(t, "current", messages[7].Content)
}

// TestConversationTurn_JSONRoundTrip verifies ConversationTurn serializes correctly.
func TestConversationTurn_JSONRoundTrip(t *testing.T) {
	turn := ConversationTurn{
		Question:  "What is OAuth?",
		Answer:    "OAuth is an authorization framework.",
		Timestamp: 1704067200000,
	}

	jsonBytes, err := json.Marshal(turn)
	require.NoError(t, err)

	var parsed ConversationTurn
	err = json.Unmarshal(jsonBytes, &parsed)
	require.NoError(t, err)

	assert.Equal(t, turn.Question, parsed.Question)
	assert.Equal(t, turn.Answer, parsed.Answer)
	assert.Equal(t, turn.Timestamp, parsed.Timestamp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	Event string
	Data  string
}

// parseSSEEvents parses SSE events from a response body.
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()

	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))

	var currentEvent sseEvent
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent.Event != "" {
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		}
	}

	// Add last event if not empty
	if currentEvent.Event != "" {
		events = append(events, currentEvent)
	}

	return events
}

// =============================================================================
// Tests: Turn Persistence Helper Methods
// =============================================================================

// TestGenerateTurnUUID_Deterministic verifies UUID generation is deterministic.
//
// # Description
//
// Tests that the same inputs always produce the same UUID.
func TestGenerateTurnUUID_Deterministic(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	uuid1 := h.generateTurnUUID("session-123", 1, "What is AI?", "AI is...")
	uuid2 := h.generateTurnUUID("session-123", 1, "What is AI?", "AI is...")

	assert.Equal(t, uuid1, uuid2, "Same inputs should produce same UUID")
}

// TestGenerateTurnUUID_UniqueForDifferentInputs verifies different inputs produce different UUIDs.
//
// # Description
//
// Tests that different combinations of inputs produce unique UUIDs.
func TestGenerateTurnUUID_UniqueForDifferentInputs(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	tests := []struct {
		name       string
		sessionID  string
		turnNumber int
		question   string
		answer     string
	}{
		{"base", "session-123", 1, "What is AI?", "AI is..."},
		{"different session", "session-456", 1, "What is AI?", "AI is..."},
		{"different turn", "session-123", 2, "What is AI?", "AI is..."},
		{"different question", "session-123", 1, "What is ML?", "AI is..."},
		{"different answer", "session-123", 1, "What is AI?", "ML is..."},
	}

	uuids := make(map[string]string)
	for _, tc := range tests {
		uuid := h.generateTurnUUID(tc.sessionID, tc.turnNumber, tc.question, tc.answer)
		for name, existingUUID := range uuids {
			if uuid == existingUUID && name != tc.name {
				t.Errorf("UUID collision between %q and %q", name, tc.name)
			}
		}
		uuids[tc.name] = uuid
	}
}

// TestGenerateTurnUUID_ValidFormat verifies UUID format.
//
// # Description
//
// Tests that the generated UUID is in valid UUID format.
func TestGenerateTurnUUID_ValidFormat(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	uuidStr := h.generateTurnUUID("session-123", 1, "What is AI?", "AI is...")

	_, err := uuid.Parse(uuidStr)
	assert.NoError(t, err, "Generated UUID should be valid")
}

// TestBuildTurnProperties_ContainsAllFields verifies all properties are set.
//
// # Description
//
// Tests that buildTurnProperties returns a map with all required fields.
func TestBuildTurnProperties_ContainsAllFields(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	sessionID := "session-abc"
	turnNumber := 5
	question := "What is AI?"
	answer := "AI is artificial intelligence."
	turnHash := "abc123def456"
	timestamp := int64(1704067200000) // 2024-01-01 00:00:00 UTC

	props := h.buildTurnProperties(sessionID, turnNumber, question, answer, turnHash, timestamp)

	assert.Equal(t, sessionID, props["session_id"], "session_id should match")
	assert.Equal(t, turnNumber, props["turn_number"], "turn_number should match")
	assert.Equal(t, question, props["question"], "question should match")
	assert.Equal(t, answer, props["answer"], "answer should match")
	assert.Equal(t, turnHash, props["turn_hash"], "turn_hash should match")
	assert.Equal(t, timestamp, props["timestamp"], "timestamp should match")
}

// TestBuildTurnProperties_HandlesEmptyStrings verifies empty string handling.
//
// # Description
//
// Tests that buildTurnProperties handles empty strings correctly.
func TestBuildTurnProperties_HandlesEmptyStrings(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	props := h.buildTurnProperties("", 1, "", "", "", 0)

	assert.Empty(t, props["session_id"], "session_id should be empty")
	assert.Equal(t, 1, props["turn_number"], "turn_number should be set")
	assert.Empty(t, props["question"], "question should be empty")
	assert.Empty(t, props["answer"], "answer should be empty")
	assert.Empty(t, props["turn_hash"], "turn_hash should be empty")
	assert.Equal(t, int64(0), props["timestamp"], "timestamp should be 0")
}

// TestBuildTurnProperties_HandlesUnicodeContent verifies Unicode handling.
//
// # Description
//
// Tests that buildTurnProperties preserves Unicode content.
func TestBuildTurnProperties_HandlesUnicodeContent(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	question := "こんにちは"
	answer := "世界 🌍"

	props := h.buildTurnProperties("session-1", 1, question, answer, "hash", 1234567890)

	assert.Equal(t, question, props["question"], "question should preserve Unicode")
	assert.Equal(t, answer, props["answer"], "answer should preserve Unicode")
}

// TestAuditAnswerForPII_NoFindings verifies PII-free content handling.
//
// # Description
//
// Tests that auditAnswerForPII returns false (no block) for PII-free content.
func TestAuditAnswerForPII_NoFindings(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Content without PII
	shouldBlock, findings := h.auditAnswerForPII("session-1", "Hello, this is a safe message.")

	assert.False(t, shouldBlock, "Should not block PII-free content")
	assert.Empty(t, findings, "Should have no findings")
}

// TestAuditAnswerForPII_AuditModeDefault verifies default audit mode.
//
// # Description
//
// Tests that audit mode logs but doesn't block by default.
func TestAuditAnswerForPII_AuditModeDefault(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Content with potential PII (SSN pattern)
	shouldBlock, _ := h.auditAnswerForPII("session-1", "Call me at 123-45-6789 please.")

	// In audit mode (default), should not block
	assert.False(t, shouldBlock, "Audit mode should not block")
}

// TestGetPIIScanMode_DefaultsToAudit verifies default scan mode.
//
// # Description
//
// Tests that getPIIScanMode returns "audit" when env var is not set.
func TestGetPIIScanMode_DefaultsToAudit(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Clear any existing env var
	t.Setenv("ALEUTIAN_PII_SCAN_MODE", "")

	mode := h.getPIIScanMode()
	assert.Equal(t, "audit", mode, "Default mode should be audit")
}

// TestGetPIIScanMode_RespectsEnvVar verifies env var is respected.
//
// # Description
//
// Tests that getPIIScanMode respects the ALEUTIAN_PII_SCAN_MODE env var.
func TestGetPIIScanMode_RespectsEnvVar(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	t.Setenv("ALEUTIAN_PII_SCAN_MODE", "block")
	mode := h.getPIIScanMode()
	assert.Equal(t, "block", mode, "Should respect block mode")

	t.Setenv("ALEUTIAN_PII_SCAN_MODE", "audit")
	mode = h.getPIIScanMode()
	assert.Equal(t, "audit", mode, "Should respect audit mode")
}

// TestParseTurnCount_ValidData verifies turn count parsing.
//
// # Description
//
// Tests that parseTurnCount correctly parses Weaviate aggregate response.
func TestParseTurnCount_ValidData(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Simulate Weaviate aggregate response structure
	data := map[string]interface{}{
		"Aggregate": map[string]interface{}{
			"Conversation": []interface{}{
				map[string]interface{}{
					"meta": map[string]interface{}{
						"count": float64(5),
					},
				},
			},
		},
	}

	count, err := h.parseTurnCount(data)
	assert.NoError(t, err, "Should parse valid data")
	assert.Equal(t, 5, count, "Should return correct count")
}

// TestParseTurnCount_EmptyResult verifies empty result handling.
//
// # Description
//
// Tests that parseTurnCount returns 0 for empty aggregate results.
func TestParseTurnCount_EmptyResult(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Empty aggregate response
	data := map[string]interface{}{
		"Aggregate": map[string]interface{}{
			"Conversation": []interface{}{},
		},
	}

	count, err := h.parseTurnCount(data)
	assert.NoError(t, err, "Should handle empty result")
	assert.Equal(t, 0, count, "Should return 0 for empty result")
}

// TestParseTurnCount_InvalidStructure verifies error handling.
//
// # Description
//
// Tests that parseTurnCount returns error for invalid data structure.
func TestParseTurnCount_InvalidStructure(t *testing.T) {
	handler := createTestStreamingChatHandler(t, &StreamingMockLLMClient{})
	h := handler.(*streamingChatHandler)

	// Invalid structure
	data := "not a map"

	_, err := h.parseTurnCount(data)
	assert.Error(t, err, "Should error on invalid structure")
}
