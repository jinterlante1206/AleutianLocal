// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package datatypes

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// DirectChatRequest Validation Tests
// =============================================================================

func TestDirectChatRequest_Validate_Success(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	if err := req.Validate(); err != nil {
		t.Errorf("expected valid request, got error: %v", err)
	}
}

func TestDirectChatRequest_Validate_MissingRequestID(t *testing.T) {
	req := &DirectChatRequest{
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for missing request_id, got nil")
	}
}

func TestDirectChatRequest_Validate_InvalidRequestID(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "not-a-uuid",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for invalid request_id, got nil")
	}
}

func TestDirectChatRequest_Validate_MissingTimestamp(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for missing timestamp, got nil")
	}
}

func TestDirectChatRequest_Validate_EmptyMessages(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages:  []Message{},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for empty messages, got nil")
	}
}

func TestDirectChatRequest_Validate_TooManyMessages(t *testing.T) {
	messages := make([]Message, MaxMessagesPerRequest+1)
	for i := range messages {
		messages[i] = Message{Role: "user", Content: "Message"}
	}

	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages:  messages,
	}

	if err := req.Validate(); err == nil {
		t.Errorf("expected error for %d messages (max is %d), got nil",
			len(messages), MaxMessagesPerRequest)
	}
}

func TestDirectChatRequest_Validate_ExactlyMaxMessages(t *testing.T) {
	messages := make([]Message, MaxMessagesPerRequest)
	for i := range messages {
		messages[i] = Message{Role: "user", Content: "Message"}
	}

	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages:  messages,
	}

	if err := req.Validate(); err != nil {
		t.Errorf("expected valid request with exactly %d messages, got error: %v",
			MaxMessagesPerRequest, err)
	}
}

func TestDirectChatRequest_Validate_BudgetTokensNegative(t *testing.T) {
	req := &DirectChatRequest{
		RequestID:    "550e8400-e29b-41d4-a716-446655440000",
		Timestamp:    time.Now().UnixMilli(),
		Messages:     []Message{{Role: "user", Content: "Hello"}},
		BudgetTokens: -1,
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for negative budget_tokens, got nil")
	}
}

func TestDirectChatRequest_Validate_BudgetTokensTooHigh(t *testing.T) {
	req := &DirectChatRequest{
		RequestID:    "550e8400-e29b-41d4-a716-446655440000",
		Timestamp:    time.Now().UnixMilli(),
		Messages:     []Message{{Role: "user", Content: "Hello"}},
		BudgetTokens: MaxBudgetTokens + 1,
	}

	if err := req.Validate(); err == nil {
		t.Errorf("expected error for budget_tokens > %d, got nil", MaxBudgetTokens)
	}
}

// =============================================================================
// Message Validation Tests
// =============================================================================

func TestMessage_Validate_InvalidRole(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "invalid", Content: "Hello"},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for invalid role, got nil")
	}
}

func TestMessage_Validate_ValidRoles(t *testing.T) {
	validRoles := []string{"user", "assistant", "system"}

	for _, role := range validRoles {
		req := &DirectChatRequest{
			RequestID: "550e8400-e29b-41d4-a716-446655440000",
			Timestamp: time.Now().UnixMilli(),
			Messages: []Message{
				{Role: role, Content: "Hello"},
			},
		}

		if err := req.Validate(); err != nil {
			t.Errorf("expected valid role '%s', got error: %v", role, err)
		}
	}
}

func TestMessage_Validate_EmptyContent(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: ""},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

func TestMessage_Validate_ContentTooLarge(t *testing.T) {
	// Create content that exceeds MaxMessageContentBytes (32KB)
	largeContent := strings.Repeat("x", MaxMessageContentBytes+1)

	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: largeContent},
		},
	}

	if err := req.Validate(); err == nil {
		t.Errorf("expected error for content > %d bytes, got nil", MaxMessageContentBytes)
	}
}

func TestMessage_Validate_ContentExactlyMaxSize(t *testing.T) {
	// Create content that is exactly MaxMessageContentBytes (32KB)
	exactContent := strings.Repeat("x", MaxMessageContentBytes)

	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: exactContent},
		},
	}

	if err := req.Validate(); err != nil {
		t.Errorf("expected valid request with exactly %d bytes content, got error: %v",
			MaxMessageContentBytes, err)
	}
}

func TestMessage_Validate_InvalidMessageID(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{
				MessageID: "not-a-uuid",
				Role:      "user",
				Content:   "Hello",
			},
		},
	}

	if err := req.Validate(); err == nil {
		t.Error("expected error for invalid message_id, got nil")
	}
}

func TestMessage_Validate_ValidMessageID(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{
				MessageID: "660f9500-f39c-42e5-b827-557766551111",
				Role:      "user",
				Content:   "Hello",
			},
		},
	}

	if err := req.Validate(); err != nil {
		t.Errorf("expected valid request with message_id, got error: %v", err)
	}
}

func TestMessage_Validate_OmittedMessageID(t *testing.T) {
	req := &DirectChatRequest{
		RequestID: "550e8400-e29b-41d4-a716-446655440000",
		Timestamp: time.Now().UnixMilli(),
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	}

	if err := req.Validate(); err != nil {
		t.Errorf("expected valid request without message_id, got error: %v", err)
	}
}

// =============================================================================
// EnsureDefaults Tests
// =============================================================================

func TestDirectChatRequest_EnsureDefaults_GeneratesRequestID(t *testing.T) {
	req := &DirectChatRequest{
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	req.EnsureDefaults()

	if req.RequestID == "" {
		t.Error("expected EnsureDefaults to generate RequestID, got empty string")
	}
}

func TestDirectChatRequest_EnsureDefaults_GeneratesTimestamp(t *testing.T) {
	req := &DirectChatRequest{
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	before := time.Now().UnixMilli()
	req.EnsureDefaults()
	after := time.Now().UnixMilli()

	if req.Timestamp < before || req.Timestamp > after {
		t.Errorf("expected timestamp between %d and %d, got %d",
			before, after, req.Timestamp)
	}
}

func TestDirectChatRequest_EnsureDefaults_PreservesExistingValues(t *testing.T) {
	existingID := "550e8400-e29b-41d4-a716-446655440000"
	existingTimestamp := int64(1735817400000)

	req := &DirectChatRequest{
		RequestID: existingID,
		Timestamp: existingTimestamp,
		Messages:  []Message{{Role: "user", Content: "Hello"}},
	}

	req.EnsureDefaults()

	if req.RequestID != existingID {
		t.Errorf("expected RequestID to be preserved as %s, got %s",
			existingID, req.RequestID)
	}
	if req.Timestamp != existingTimestamp {
		t.Errorf("expected Timestamp to be preserved as %d, got %d",
			existingTimestamp, req.Timestamp)
	}
}

// =============================================================================
// NewDirectChatResponse Tests
// =============================================================================

func TestNewDirectChatResponse_SetsResponseID(t *testing.T) {
	resp := NewDirectChatResponse("req-123", "Hello!")

	if resp.ResponseID == "" {
		t.Error("expected ResponseID to be set, got empty string")
	}
}

func TestNewDirectChatResponse_EchoesRequestID(t *testing.T) {
	requestID := "550e8400-e29b-41d4-a716-446655440000"
	resp := NewDirectChatResponse(requestID, "Hello!")

	if resp.RequestID != requestID {
		t.Errorf("expected RequestID to be %s, got %s", requestID, resp.RequestID)
	}
}

func TestNewDirectChatResponse_SetsTimestamp(t *testing.T) {
	before := time.Now().UnixMilli()
	resp := NewDirectChatResponse("req-123", "Hello!")
	after := time.Now().UnixMilli()

	if resp.Timestamp < before || resp.Timestamp > after {
		t.Errorf("expected timestamp between %d and %d, got %d",
			before, after, resp.Timestamp)
	}
}

func TestNewDirectChatResponse_SetsAnswer(t *testing.T) {
	answer := "Hello! How can I help you today?"
	resp := NewDirectChatResponse("req-123", answer)

	if resp.Answer != answer {
		t.Errorf("expected Answer to be %q, got %q", answer, resp.Answer)
	}
}

// =============================================================================
// Constants Tests
// =============================================================================

func TestConstants(t *testing.T) {
	if MaxMessageContentBytes != 32*1024 {
		t.Errorf("expected MaxMessageContentBytes to be 32KB, got %d", MaxMessageContentBytes)
	}
	if MaxMessagesPerRequest != 100 {
		t.Errorf("expected MaxMessagesPerRequest to be 100, got %d", MaxMessagesPerRequest)
	}
	if MaxBudgetTokens != 65536 {
		t.Errorf("expected MaxBudgetTokens to be 65536, got %d", MaxBudgetTokens)
	}
}
