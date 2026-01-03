// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"testing"
	"time"
)

// =============================================================================
// StreamEventType Tests
// =============================================================================

func TestStreamEventType_String(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		want      string
	}{
		{StreamEventStatus, "status"},
		{StreamEventToken, "token"},
		{StreamEventThinking, "thinking"},
		{StreamEventSources, "sources"},
		{StreamEventDone, "done"},
		{StreamEventError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.want {
				t.Errorf("StreamEventType.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamEventType_IsTerminal(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		want      bool
	}{
		{StreamEventStatus, false},
		{StreamEventToken, false},
		{StreamEventThinking, false},
		{StreamEventSources, false},
		{StreamEventDone, true},
		{StreamEventError, true},
	}

	for _, tt := range tests {
		t.Run(tt.eventType.String(), func(t *testing.T) {
			if got := tt.eventType.IsTerminal(); got != tt.want {
				t.Errorf("StreamEventType.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// StreamEvent Constructor Tests
// =============================================================================

func TestNewTokenEvent(t *testing.T) {
	content := "Hello world"
	event := NewTokenEvent(content)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventToken {
		t.Errorf("expected Type %v, got %v", StreamEventToken, event.Type)
	}
	if event.Content != content {
		t.Errorf("expected Content %q, got %q", content, event.Content)
	}
}

func TestNewThinkingEvent(t *testing.T) {
	content := "Let me think..."
	event := NewThinkingEvent(content)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventThinking {
		t.Errorf("expected Type %v, got %v", StreamEventThinking, event.Type)
	}
	if event.Content != content {
		t.Errorf("expected Content %q, got %q", content, event.Content)
	}
}

func TestNewStatusEvent(t *testing.T) {
	message := "Searching..."
	event := NewStatusEvent(message)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventStatus {
		t.Errorf("expected Type %v, got %v", StreamEventStatus, event.Type)
	}
	if event.Message != message {
		t.Errorf("expected Message %q, got %q", message, event.Message)
	}
}

func TestNewSourcesEvent(t *testing.T) {
	sources := []SourceInfo{
		{Source: "doc1.pdf", Score: 0.95},
		{Source: "doc2.pdf", Score: 0.87},
	}
	event := NewSourcesEvent(sources)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventSources {
		t.Errorf("expected Type %v, got %v", StreamEventSources, event.Type)
	}
	if len(event.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(event.Sources))
	}
}

func TestNewDoneEvent(t *testing.T) {
	sessionID := "sess-abc123"
	event := NewDoneEvent(sessionID)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventDone {
		t.Errorf("expected Type %v, got %v", StreamEventDone, event.Type)
	}
	if event.SessionID != sessionID {
		t.Errorf("expected SessionID %q, got %q", sessionID, event.SessionID)
	}
	if !event.IsTerminal() {
		t.Error("expected done event to be terminal")
	}
}

func TestNewErrorEvent(t *testing.T) {
	errMsg := "Server overloaded"
	event := NewErrorEvent(errMsg)

	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventError {
		t.Errorf("expected Type %v, got %v", StreamEventError, event.Type)
	}
	if event.Error != errMsg {
		t.Errorf("expected Error %q, got %q", errMsg, event.Error)
	}
	if !event.IsTerminal() {
		t.Error("expected error event to be terminal")
	}
}

// =============================================================================
// StreamEvent Method Tests
// =============================================================================

func TestStreamEvent_CreatedAtTime(t *testing.T) {
	now := time.Now()
	event := NewTokenEvent("test")

	createdAt := event.CreatedAtTime()
	diff := createdAt.Sub(now)

	// Should be within 1 second of now
	if diff < -time.Second || diff > time.Second {
		t.Errorf("CreatedAtTime() = %v, expected within 1s of %v", createdAt, now)
	}
}

func TestStreamEvent_IsTerminal(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		want  bool
	}{
		{"token", NewTokenEvent("hi"), false},
		{"thinking", NewThinkingEvent("hmm"), false},
		{"status", NewStatusEvent("working"), false},
		{"sources", NewSourcesEvent(nil), false},
		{"done", NewDoneEvent("sess"), true},
		{"error", NewErrorEvent("oops"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsTerminal(); got != tt.want {
				t.Errorf("StreamEvent.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// StreamResult Tests
// =============================================================================

func TestNewStreamResult(t *testing.T) {
	result := NewStreamResult()

	if result.Id == "" {
		t.Error("expected Id to be set")
	}
	if result.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
}

func TestNewStreamResultWithRequestID(t *testing.T) {
	requestID := "req-xyz789"
	result := NewStreamResultWithRequestID(requestID)

	if result.Id == "" {
		t.Error("expected Id to be set")
	}
	if result.RequestID != requestID {
		t.Errorf("expected RequestID %q, got %q", requestID, result.RequestID)
	}
}

func TestStreamResult_HasError(t *testing.T) {
	tests := []struct {
		name   string
		result StreamResult
		want   bool
	}{
		{"no error", StreamResult{Answer: "hello"}, false},
		{"with error", StreamResult{Error: "failed"}, true},
		{"empty error", StreamResult{Error: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasError(); got != tt.want {
				t.Errorf("StreamResult.HasError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamResult_Duration(t *testing.T) {
	result := StreamResult{
		CreatedAt:   1000,
		CompletedAt: 3500,
	}

	duration := result.Duration()
	expected := 2500 * time.Millisecond

	if duration != expected {
		t.Errorf("Duration() = %v, want %v", duration, expected)
	}
}

func TestStreamResult_Duration_ZeroValues(t *testing.T) {
	tests := []struct {
		name   string
		result StreamResult
	}{
		{"zero created", StreamResult{CreatedAt: 0, CompletedAt: 1000}},
		{"zero completed", StreamResult{CreatedAt: 1000, CompletedAt: 0}},
		{"both zero", StreamResult{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Duration(); got != 0 {
				t.Errorf("Duration() = %v, want 0", got)
			}
		})
	}
}

func TestStreamResult_TimeToFirstToken(t *testing.T) {
	result := StreamResult{
		CreatedAt:    1000,
		FirstTokenAt: 1800,
	}

	ttft := result.TimeToFirstToken()
	expected := 800 * time.Millisecond

	if ttft != expected {
		t.Errorf("TimeToFirstToken() = %v, want %v", ttft, expected)
	}
}

func TestStreamResult_TimeToFirstToken_ZeroValues(t *testing.T) {
	tests := []struct {
		name   string
		result StreamResult
	}{
		{"zero first token", StreamResult{CreatedAt: 1000, FirstTokenAt: 0}},
		{"zero created", StreamResult{CreatedAt: 0, FirstTokenAt: 1000}},
		{"both zero", StreamResult{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.TimeToFirstToken(); got != 0 {
				t.Errorf("TimeToFirstToken() = %v, want 0", got)
			}
		})
	}
}

func TestStreamResult_TokensPerSecond(t *testing.T) {
	result := StreamResult{
		CreatedAt:   1000,
		CompletedAt: 3000, // 2 seconds duration (3000 - 1000)
		TotalTokens: 100,
	}

	tps := result.TokensPerSecond()
	expected := 50.0 // 100 tokens / 2 seconds

	if tps != expected {
		t.Errorf("TokensPerSecond() = %v, want %v", tps, expected)
	}
}

func TestStreamResult_TokensPerSecond_ZeroValues(t *testing.T) {
	tests := []struct {
		name   string
		result StreamResult
	}{
		{"zero tokens", StreamResult{CreatedAt: 0, CompletedAt: 1000, TotalTokens: 0}},
		{"zero duration", StreamResult{CreatedAt: 1000, CompletedAt: 1000, TotalTokens: 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.TokensPerSecond(); got != 0 {
				t.Errorf("TokensPerSecond() = %v, want 0", got)
			}
		})
	}
}

func TestStreamResult_TimeConversions(t *testing.T) {
	now := time.Now()
	nowMs := now.UnixMilli()

	result := StreamResult{
		CreatedAt:    nowMs,
		CompletedAt:  nowMs + 1000,
		FirstTokenAt: nowMs + 500,
	}

	// Check CreatedAtTime
	if diff := result.CreatedAtTime().Sub(now); diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("CreatedAtTime() diff = %v, expected < 1ms", diff)
	}

	// Check CompletedAtTime
	expectedCompleted := now.Add(1000 * time.Millisecond)
	if diff := result.CompletedAtTime().Sub(expectedCompleted); diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("CompletedAtTime() diff = %v, expected < 1ms", diff)
	}

	// Check FirstTokenAtTime
	expectedFirst := now.Add(500 * time.Millisecond)
	if diff := result.FirstTokenAtTime().Sub(expectedFirst); diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("FirstTokenAtTime() diff = %v, expected < 1ms", diff)
	}
}

func TestStreamResult_FirstTokenAtTime_Zero(t *testing.T) {
	result := StreamResult{FirstTokenAt: 0}

	if !result.FirstTokenAtTime().IsZero() {
		t.Error("expected zero time when FirstTokenAt is 0")
	}
}

// =============================================================================
// Event ID Uniqueness Tests
// =============================================================================

func TestEventIDs_AreUnique(t *testing.T) {
	ids := make(map[string]bool)
	count := 100

	for i := 0; i < count; i++ {
		event := NewTokenEvent("test")
		if ids[event.Id] {
			t.Errorf("duplicate Id found: %s", event.Id)
		}
		ids[event.Id] = true
	}
}

func TestResultIDs_AreUnique(t *testing.T) {
	ids := make(map[string]bool)
	count := 100

	for i := 0; i < count; i++ {
		result := NewStreamResult()
		if ids[result.Id] {
			t.Errorf("duplicate Id found: %s", result.Id)
		}
		ids[result.Id] = true
	}
}
