// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// SSE Stream Reader Tests
// =============================================================================

func TestNewSSEStreamReader(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())
	if reader == nil {
		t.Fatal("NewSSEStreamReader() returned nil")
	}
}

// -----------------------------------------------------------------------------
// Read Tests - Basic Functionality
// -----------------------------------------------------------------------------

func TestSSEStreamReader_Read_TokenEvents(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"Hello"}
data: {"type":"token","content":" world"}
data: {"type":"done","session_id":"sess-123"}
`)

	var tokens []string
	var sessionID string

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventToken:
			tokens = append(tokens, event.Content)
		case StreamEventDone:
			sessionID = event.SessionID
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0] != "Hello" || tokens[1] != " world" {
		t.Errorf("unexpected tokens: %v", tokens)
	}
	if sessionID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", sessionID)
	}
}

func TestSSEStreamReader_Read_AllEventTypes(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"status","message":"Searching..."}
data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.9}]}
data: {"type":"thinking","content":"Let me think..."}
data: {"type":"token","content":"The answer is 42"}
data: {"type":"done","session_id":"sess-xyz"}
`)

	events := make([]StreamEvent, 0)

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	// Verify event types
	expectedTypes := []StreamEventType{
		StreamEventStatus,
		StreamEventSources,
		StreamEventThinking,
		StreamEventToken,
		StreamEventDone,
	}
	for i, expected := range expectedTypes {
		if events[i].Type != expected {
			t.Errorf("event %d: expected Type %v, got %v", i, expected, events[i].Type)
		}
	}
}

func TestSSEStreamReader_Read_EventIndexing(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"a"}
data: {"type":"token","content":"b"}
data: {"type":"token","content":"c"}
data: {"type":"done"}
`)

	indices := make([]int, 0)

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		indices = append(indices, event.Index)
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indices) != 4 {
		t.Fatalf("expected 4 events, got %d", len(indices))
	}
	for i, idx := range indices {
		if idx != i {
			t.Errorf("event %d: expected Index %d, got %d", i, i, idx)
		}
	}
}

// -----------------------------------------------------------------------------
// Read Tests - Error Handling
// -----------------------------------------------------------------------------

func TestSSEStreamReader_Read_ErrorEvent(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"partial"}
data: {"type":"error","error":"Server overloaded"}
data: {"type":"token","content":"should not see this"}
`)

	var receivedError string
	tokenCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		switch event.Type {
		case StreamEventToken:
			tokenCount++
		case StreamEventError:
			receivedError = event.Error
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenCount != 1 {
		t.Errorf("expected 1 token, got %d", tokenCount)
	}
	if receivedError != "Server overloaded" {
		t.Errorf("expected error 'Server overloaded', got %q", receivedError)
	}
}

func TestSSEStreamReader_Read_CallbackError(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"a"}
data: {"type":"token","content":"b"}
data: {"type":"token","content":"c"}
`)

	callbackErr := errors.New("callback stopped")
	tokenCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		tokenCount++
		if tokenCount == 2 {
			return callbackErr
		}
		return nil
	})

	if err != callbackErr {
		t.Errorf("expected callback error, got %v", err)
	}
	if tokenCount != 2 {
		t.Errorf("expected 2 tokens before error, got %d", tokenCount)
	}
}

func TestSSEStreamReader_Read_ContextCancellation(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	// Create a slow reader that we can cancel
	stream := strings.NewReader(`data: {"type":"token","content":"a"}
data: {"type":"token","content":"b"}
data: {"type":"token","content":"c"}
`)

	ctx, cancel := context.WithCancel(context.Background())
	tokenCount := 0

	err := reader.Read(ctx, stream, func(event StreamEvent) error {
		tokenCount++
		if tokenCount == 1 {
			cancel() // Cancel after first token
		}
		return nil
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSSEStreamReader_Read_InvalidJSON(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"ok"}
data: {invalid json}
`)

	tokenCount := 0
	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		tokenCount++
		return nil
	})

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if tokenCount != 1 {
		t.Errorf("expected 1 token before error, got %d", tokenCount)
	}
}

// -----------------------------------------------------------------------------
// Read Tests - Edge Cases
// -----------------------------------------------------------------------------

func TestSSEStreamReader_Read_EmptyStream(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader("")
	eventCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		eventCount++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventCount != 0 {
		t.Errorf("expected 0 events, got %d", eventCount)
	}
}

func TestSSEStreamReader_Read_EmptyLinesSkipped(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`
data: {"type":"token","content":"a"}

data: {"type":"token","content":"b"}

data: {"type":"done"}

`)

	eventCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		eventCount++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventCount != 3 {
		t.Errorf("expected 3 events, got %d", eventCount)
	}
}

func TestSSEStreamReader_Read_CommentsSkipped(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`: this is a comment
data: {"type":"token","content":"visible"}
: another comment
data: {"type":"done"}
`)

	eventCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		eventCount++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventCount != 2 {
		t.Errorf("expected 2 events (comments skipped), got %d", eventCount)
	}
}

func TestSSEStreamReader_Read_StreamWithoutDone(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	// Stream ends without explicit done event (EOF)
	stream := strings.NewReader(`data: {"type":"token","content":"partial"}
`)

	tokenCount := 0

	err := reader.Read(context.Background(), stream, func(event StreamEvent) error {
		tokenCount++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenCount != 1 {
		t.Errorf("expected 1 token, got %d", tokenCount)
	}
}

// -----------------------------------------------------------------------------
// ReadAll Tests
// -----------------------------------------------------------------------------

func TestSSEStreamReader_ReadAll_BasicFlow(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"status","message":"Working..."}
data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.95}]}
data: {"type":"thinking","content":"Let me analyze..."}
data: {"type":"token","content":"The answer is "}
data: {"type":"token","content":"42."}
data: {"type":"done","session_id":"sess-abc"}
`)

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Id == "" {
		t.Error("expected Id to be set")
	}
	if result.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if result.CompletedAt == 0 {
		t.Error("expected CompletedAt to be set")
	}
	if result.Answer != "The answer is 42." {
		t.Errorf("expected Answer 'The answer is 42.', got %q", result.Answer)
	}
	if result.Thinking != "Let me analyze..." {
		t.Errorf("expected Thinking 'Let me analyze...', got %q", result.Thinking)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("expected SessionID 'sess-abc', got %q", result.SessionID)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result.Sources))
	}
	if result.TotalTokens != 2 {
		t.Errorf("expected TotalTokens 2, got %d", result.TotalTokens)
	}
	if result.ThinkingTokens != 1 {
		t.Errorf("expected ThinkingTokens 1, got %d", result.ThinkingTokens)
	}
	if result.TotalEvents != 6 {
		t.Errorf("expected TotalEvents 6, got %d", result.TotalEvents)
	}
}

func TestSSEStreamReader_ReadAll_WithError(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"partial"}
data: {"type":"error","error":"Server crashed"}
`)

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "partial" {
		t.Errorf("expected Answer 'partial', got %q", result.Answer)
	}
	if result.Error != "Server crashed" {
		t.Errorf("expected Error 'Server crashed', got %q", result.Error)
	}
	if !result.HasError() {
		t.Error("expected HasError() to return true")
	}
}

func TestSSEStreamReader_ReadAll_FirstTokenTiming(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"status","message":"Thinking..."}
data: {"type":"token","content":"Hello"}
data: {"type":"done"}
`)

	before := time.Now().UnixMilli()
	result, err := reader.ReadAll(context.Background(), stream)
	after := time.Now().UnixMilli()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FirstTokenAt == 0 {
		t.Error("expected FirstTokenAt to be set")
	}
	if result.FirstTokenAt < before || result.FirstTokenAt > after {
		t.Errorf("FirstTokenAt %d outside expected range [%d, %d]",
			result.FirstTokenAt, before, after)
	}
}

func TestSSEStreamReader_ReadAll_RequestIDPropagation(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"Hi","request_id":"req-xyz"}
data: {"type":"done"}
`)

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequestID != "req-xyz" {
		t.Errorf("expected RequestID 'req-xyz', got %q", result.RequestID)
	}
}

func TestSSEStreamReader_ReadAll_DurationCalculation(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"token","content":"test"}
data: {"type":"done"}
`)

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	duration := result.Duration()
	if duration < 0 {
		t.Errorf("expected non-negative duration, got %v", duration)
	}
}

func TestSSEStreamReader_ReadAll_EmptyStream(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader("")

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "" {
		t.Errorf("expected empty Answer, got %q", result.Answer)
	}
	if result.TotalEvents != 0 {
		t.Errorf("expected TotalEvents 0, got %d", result.TotalEvents)
	}
}

func TestSSEStreamReader_ReadAll_MultipleSources(t *testing.T) {
	reader := NewSSEStreamReader(NewSSEParser())

	stream := strings.NewReader(`data: {"type":"sources","sources":[{"source":"a.pdf"}]}
data: {"type":"sources","sources":[{"source":"b.pdf"},{"source":"c.pdf"}]}
data: {"type":"done"}
`)

	result, err := reader.ReadAll(context.Background(), stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 3 {
		t.Errorf("expected 3 sources, got %d", len(result.Sources))
	}
}
