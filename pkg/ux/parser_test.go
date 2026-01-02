// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"testing"
)

// =============================================================================
// SSE Parser Tests
// =============================================================================

func TestNewSSEParser(t *testing.T) {
	parser := NewSSEParser()
	if parser == nil {
		t.Fatal("NewSSEParser() returned nil")
	}
}

// -----------------------------------------------------------------------------
// ParseLine Tests - Data Lines
// -----------------------------------------------------------------------------

func TestSSEParser_ParseLine_TokenEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"token","content":"Hello"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
	if event.Type != StreamEventToken {
		t.Errorf("expected Type %v, got %v", StreamEventToken, event.Type)
	}
	if event.Content != "Hello" {
		t.Errorf("expected Content 'Hello', got %q", event.Content)
	}
}

func TestSSEParser_ParseLine_ThinkingEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"thinking","content":"Let me analyze..."}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != StreamEventThinking {
		t.Errorf("expected Type %v, got %v", StreamEventThinking, event.Type)
	}
	if event.Content != "Let me analyze..." {
		t.Errorf("expected Content 'Let me analyze...', got %q", event.Content)
	}
}

func TestSSEParser_ParseLine_StatusEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"status","message":"Searching..."}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != StreamEventStatus {
		t.Errorf("expected Type %v, got %v", StreamEventStatus, event.Type)
	}
	if event.Message != "Searching..." {
		t.Errorf("expected Message 'Searching...', got %q", event.Message)
	}
}

func TestSSEParser_ParseLine_SourcesEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.95}]}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != StreamEventSources {
		t.Errorf("expected Type %v, got %v", StreamEventSources, event.Type)
	}
	if len(event.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(event.Sources))
	}
	if event.Sources[0].Source != "doc.pdf" {
		t.Errorf("expected source 'doc.pdf', got %q", event.Sources[0].Source)
	}
	if event.Sources[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", event.Sources[0].Score)
	}
}

func TestSSEParser_ParseLine_DoneEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"done","session_id":"sess-abc123"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != StreamEventDone {
		t.Errorf("expected Type %v, got %v", StreamEventDone, event.Type)
	}
	if event.SessionID != "sess-abc123" {
		t.Errorf("expected SessionID 'sess-abc123', got %q", event.SessionID)
	}
	if !event.IsTerminal() {
		t.Error("expected done event to be terminal")
	}
}

func TestSSEParser_ParseLine_ErrorEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"error","error":"Server overloaded"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != StreamEventError {
		t.Errorf("expected Type %v, got %v", StreamEventError, event.Type)
	}
	if event.Error != "Server overloaded" {
		t.Errorf("expected Error 'Server overloaded', got %q", event.Error)
	}
	if !event.IsTerminal() {
		t.Error("expected error event to be terminal")
	}
}

func TestSSEParser_ParseLine_WithRequestID(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"token","content":"Hi","request_id":"req-xyz"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.RequestID != "req-xyz" {
		t.Errorf("expected RequestID 'req-xyz', got %q", event.RequestID)
	}
}

// -----------------------------------------------------------------------------
// ParseLine Tests - Empty and Comment Lines
// -----------------------------------------------------------------------------

func TestSSEParser_ParseLine_EmptyLine(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine("")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected nil event for empty line")
	}
}

func TestSSEParser_ParseLine_WhitespaceOnly(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine("   \t  ")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected nil event for whitespace-only line")
	}
}

func TestSSEParser_ParseLine_Comment(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(": this is a comment")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected nil event for comment line")
	}
}

// -----------------------------------------------------------------------------
// ParseLine Tests - Raw Token Lines
// -----------------------------------------------------------------------------

func TestSSEParser_ParseLine_RawToken(t *testing.T) {
	parser := NewSSEParser()

	// Some servers send plain text tokens without JSON wrapper
	event, err := parser.ParseLine("Hello world")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Type != StreamEventToken {
		t.Errorf("expected Type %v, got %v", StreamEventToken, event.Type)
	}
	if event.Content != "Hello world" {
		t.Errorf("expected Content 'Hello world', got %q", event.Content)
	}
}

// -----------------------------------------------------------------------------
// ParseLine Tests - Edge Cases
// -----------------------------------------------------------------------------

func TestSSEParser_ParseLine_DataNoSpace(t *testing.T) {
	parser := NewSSEParser()

	// Some servers send "data:" without space
	event, err := parser.ParseLine(`data:{"type":"token","content":"Hi"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Content != "Hi" {
		t.Errorf("expected Content 'Hi', got %q", event.Content)
	}
}

func TestSSEParser_ParseLine_InvalidJSON(t *testing.T) {
	parser := NewSSEParser()

	_, err := parser.ParseLine(`data: {invalid json}`)

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSSEParser_ParseLine_MultipleSources(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseLine(`data: {"type":"sources","sources":[{"source":"a.pdf","score":0.9},{"source":"b.pdf","distance":0.1}]}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(event.Sources))
	}
	if event.Sources[1].Source != "b.pdf" {
		t.Errorf("expected second source 'b.pdf', got %q", event.Sources[1].Source)
	}
}

// -----------------------------------------------------------------------------
// ParseRawJSON Tests
// -----------------------------------------------------------------------------

func TestSSEParser_ParseRawJSON_TokenEvent(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseRawJSON([]byte(`{"type":"token","content":"Hello"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Id == "" {
		t.Error("expected Id to be set")
	}
	if event.Type != StreamEventToken {
		t.Errorf("expected Type %v, got %v", StreamEventToken, event.Type)
	}
	if event.Content != "Hello" {
		t.Errorf("expected Content 'Hello', got %q", event.Content)
	}
}

func TestSSEParser_ParseRawJSON_EmptyObject(t *testing.T) {
	parser := NewSSEParser()

	event, err := parser.ParseRawJSON([]byte(`{}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Type will be empty string, which is valid (though unusual)
	if event.Type != "" {
		t.Errorf("expected empty Type, got %v", event.Type)
	}
}

func TestSSEParser_ParseRawJSON_InvalidJSON(t *testing.T) {
	parser := NewSSEParser()

	_, err := parser.ParseRawJSON([]byte(`not json`))

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// -----------------------------------------------------------------------------
// Concurrent Safety Tests
// -----------------------------------------------------------------------------

func TestSSEParser_ConcurrentUse(t *testing.T) {
	parser := NewSSEParser()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				event, err := parser.ParseLine(`data: {"type":"token","content":"test"}`)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if event == nil {
					t.Error("expected event, got nil")
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// -----------------------------------------------------------------------------
// Event ID Uniqueness
// -----------------------------------------------------------------------------

func TestSSEParser_GeneratesUniqueIDs(t *testing.T) {
	parser := NewSSEParser()
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		event, _ := parser.ParseLine(`data: {"type":"token","content":"test"}`)
		if ids[event.Id] {
			t.Errorf("duplicate Id found: %s", event.Id)
		}
		ids[event.Id] = true
	}
}
