// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// =============================================================================
// StreamProcessor Tests
// =============================================================================

func TestNewStreamProcessorWithWriter(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	if processor == nil {
		t.Fatal("NewStreamProcessorWithWriter returned nil")
	}
}

// -----------------------------------------------------------------------------
// Process Tests - Token Events
// -----------------------------------------------------------------------------

func TestStreamProcessor_Process_TokenEvents_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	// Simulate SSE stream with token events
	stream := strings.NewReader(`data: {"type":"token","content":"Hello"}
data: {"type":"token","content":" world"}
data: {"type":"done","session_id":"sess-123"}
`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result.Answer)
	}
	if result.SessionID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", result.SessionID)
	}

	output := buf.String()
	if !strings.Contains(output, "ANSWER: Hello world") {
		t.Errorf("expected ANSWER output in machine mode, got %q", output)
	}
}

func TestStreamProcessor_Process_TokenEvents_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMinimal)

	stream := strings.NewReader(`data: {"type":"token","content":"Hi"}
data: {"type":"done"}
`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Hi" {
		t.Errorf("expected 'Hi', got %q", result.Answer)
	}

	output := buf.String()
	// In non-machine mode, tokens are streamed directly
	if !strings.Contains(output, "Hi") {
		t.Errorf("expected streamed tokens, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// Process Tests - Status Events
// -----------------------------------------------------------------------------

func TestStreamProcessor_Process_StatusEvents_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader(`data: {"type":"status","message":"Searching..."}
data: {"type":"token","content":"Found it"}
data: {"type":"done"}
`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Found it" {
		t.Errorf("expected 'Found it', got %q", result.Answer)
	}

	output := buf.String()
	if !strings.Contains(output, "STATUS: Searching...") {
		t.Errorf("expected STATUS message in machine mode, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// Process Tests - Sources Events
// -----------------------------------------------------------------------------

func TestStreamProcessor_Process_SourcesEvents(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader(`data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.95}]}
data: {"type":"token","content":"Answer text"}
data: {"type":"done"}
`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result.Sources))
	}
	if result.Sources[0].Source != "doc.pdf" {
		t.Errorf("expected source 'doc.pdf', got %q", result.Sources[0].Source)
	}
	if result.Sources[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", result.Sources[0].Score)
	}
}

// -----------------------------------------------------------------------------
// Process Tests - Error Events
// -----------------------------------------------------------------------------

func TestStreamProcessor_Process_ErrorEvent(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader(`data: {"type":"status","message":"Processing..."}
data: {"type":"error","error":"Server overloaded"}
`)

	_, err := processor.Process(stream)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Server overloaded") {
		t.Errorf("expected 'Server overloaded' in error, got %q", err.Error())
	}
}

// -----------------------------------------------------------------------------
// Process Tests - Edge Cases
// -----------------------------------------------------------------------------

func TestStreamProcessor_Process_EmptyStream(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader("")

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "" {
		t.Errorf("expected empty answer, got %q", result.Answer)
	}
}

func TestStreamProcessor_Process_StreamWithoutDone(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	// Stream ends without explicit done event
	stream := strings.NewReader(`data: {"type":"token","content":"Partial answer"}
`)

	result, err := processor.Process(stream)

	// Should not error - just return what we have
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Partial answer" {
		t.Errorf("expected 'Partial answer', got %q", result.Answer)
	}
}

func TestStreamProcessor_Process_NonJSONLine(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	// Mix of JSON and non-JSON lines
	stream := strings.NewReader(`data: {"type":"token","content":"Start "}
plaintext token
data: {"type":"done"}
`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-JSON line is treated as a token
	if !strings.Contains(result.Answer, "Start") {
		t.Errorf("expected answer to contain 'Start', got %q", result.Answer)
	}
}

func TestStreamProcessor_Process_EmptyLines(t *testing.T) {
	var buf bytes.Buffer
	processor := NewStreamProcessorWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader(`
data: {"type":"token","content":"Hello"}

data: {"type":"done"}

`)

	result, err := processor.Process(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Hello" {
		t.Errorf("expected 'Hello', got %q", result.Answer)
	}
}

// =============================================================================
// SimpleStreamReader Tests
// =============================================================================

func TestNewSimpleStreamReaderWithWriter(t *testing.T) {
	var buf bytes.Buffer
	reader := NewSimpleStreamReaderWithWriter(&buf, PersonalityMachine)

	if reader == nil {
		t.Fatal("NewSimpleStreamReaderWithWriter returned nil")
	}
}

func TestSimpleStreamReader_Read_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	reader := NewSimpleStreamReaderWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader("Plain text response")

	answer, err := reader.Read(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Plain text response" {
		t.Errorf("expected 'Plain text response', got %q", answer)
	}

	output := buf.String()
	if !strings.Contains(output, "ANSWER: Plain text response") {
		t.Errorf("expected ANSWER output in machine mode, got %q", output)
	}
}

func TestSimpleStreamReader_Read_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	reader := NewSimpleStreamReaderWithWriter(&buf, PersonalityMinimal)

	stream := strings.NewReader("Streaming text")

	answer, err := reader.Read(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Streaming text" {
		t.Errorf("expected 'Streaming text', got %q", answer)
	}

	output := buf.String()
	// In non-machine mode, text is streamed directly
	if !strings.Contains(output, "Streaming text") {
		t.Errorf("expected streamed text, got %q", output)
	}
}

func TestSimpleStreamReader_Read_EmptyStream(t *testing.T) {
	var buf bytes.Buffer
	reader := NewSimpleStreamReaderWithWriter(&buf, PersonalityMachine)

	stream := strings.NewReader("")

	answer, err := reader.Read(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "" {
		t.Errorf("expected empty answer, got %q", answer)
	}
}

func TestSimpleStreamReader_Read_LargeChunks(t *testing.T) {
	var buf bytes.Buffer
	reader := NewSimpleStreamReaderWithWriter(&buf, PersonalityMachine)

	// Create a response larger than the internal buffer (256 bytes)
	largeText := strings.Repeat("Lorem ipsum dolor sit amet. ", 20)
	stream := strings.NewReader(largeText)

	answer, err := reader.Read(stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != largeText {
		t.Errorf("answer length mismatch: expected %d, got %d", len(largeText), len(answer))
	}
}

// -----------------------------------------------------------------------------
// Convenience Function Tests
// -----------------------------------------------------------------------------

func TestSimpleStreamPrint(t *testing.T) {
	// This is a smoke test - the function uses os.Stdout which we can't easily capture
	// Just verify it doesn't panic with valid input
	stream := strings.NewReader("")
	_, err := SimpleStreamPrint(stream)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// StreamResult Tests
// =============================================================================

func TestStreamResult_Fields(t *testing.T) {
	result := &StreamResult{
		Answer:    "Test answer",
		SessionID: "sess-123",
		Sources: []SourceInfo{
			{Source: "doc.pdf", Score: 0.9},
		},
	}

	if result.Answer != "Test answer" {
		t.Errorf("expected Answer 'Test answer', got %q", result.Answer)
	}
	if result.SessionID != "sess-123" {
		t.Errorf("expected SessionID 'sess-123', got %q", result.SessionID)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result.Sources))
	}
}

// =============================================================================
// StreamEventType Tests
// =============================================================================

func TestStreamEventType_Values(t *testing.T) {
	if StreamEventStatus != "status" {
		t.Errorf("expected 'status', got %q", StreamEventStatus)
	}
	if StreamEventToken != "token" {
		t.Errorf("expected 'token', got %q", StreamEventToken)
	}
	if StreamEventSources != "sources" {
		t.Errorf("expected 'sources', got %q", StreamEventSources)
	}
	if StreamEventDone != "done" {
		t.Errorf("expected 'done', got %q", StreamEventDone)
	}
	if StreamEventError != "error" {
		t.Errorf("expected 'error', got %q", StreamEventError)
	}
}
