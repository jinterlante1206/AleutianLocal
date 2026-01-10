// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Terminal Stream Renderer Tests
// =============================================================================

func TestNewTerminalStreamRenderer(t *testing.T) {
	renderer := NewTerminalStreamRenderer(nil, PersonalityMachine)
	if renderer == nil {
		t.Fatal("NewTerminalStreamRenderer() returned nil")
	}

	result := renderer.Result()
	if result.Id == "" {
		t.Error("expected Id to be set")
	}
	if result.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
}

// -----------------------------------------------------------------------------
// OnToken Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnToken_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnToken(ctx, "Hello")
	renderer.OnToken(ctx, " world")
	renderer.OnDone(ctx, "sess-123")

	output := buf.String()
	if !strings.Contains(output, "ANSWER: Hello world") {
		t.Errorf("expected ANSWER in output, got %q", output)
	}
	if !strings.Contains(output, "SESSION: sess-123") {
		t.Errorf("expected SESSION in output, got %q", output)
	}
	if !strings.Contains(output, "DONE") {
		t.Errorf("expected DONE in output, got %q", output)
	}
}

func TestTerminalStreamRenderer_OnToken_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMinimal)
	ctx := context.Background()

	renderer.OnToken(ctx, "Hi")
	renderer.OnDone(ctx, "")

	output := buf.String()
	// In minimal mode, tokens are streamed directly
	if !strings.Contains(output, "Hi") {
		t.Errorf("expected streamed token, got %q", output)
	}
}

func TestTerminalStreamRenderer_OnToken_SetsFirstTokenAt(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	result1 := renderer.Result()
	if result1.FirstTokenAt != 0 {
		t.Error("expected FirstTokenAt to be 0 before first token")
	}

	renderer.OnToken(ctx, "test")

	result2 := renderer.Result()
	if result2.FirstTokenAt == 0 {
		t.Error("expected FirstTokenAt to be set after first token")
	}
}

// -----------------------------------------------------------------------------
// OnStatus Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnStatus_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnStatus(ctx, "Searching...")
	renderer.OnDone(ctx, "")

	output := buf.String()
	if !strings.Contains(output, "STATUS: Searching...") {
		t.Errorf("expected STATUS in output, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// OnThinking Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnThinking_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnThinking(ctx, "Let me think...")
	renderer.OnToken(ctx, "Answer")
	renderer.OnDone(ctx, "")

	output := buf.String()
	if !strings.Contains(output, "THINKING: Let me think...") {
		t.Errorf("expected THINKING in output, got %q", output)
	}

	result := renderer.Result()
	if result.ThinkingTokens != 1 {
		t.Errorf("expected ThinkingTokens 1, got %d", result.ThinkingTokens)
	}
}

// -----------------------------------------------------------------------------
// OnSources Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnSources_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	sources := []SourceInfo{
		{Source: "doc.pdf", Score: 0.95},
		{Source: "notes.md", Distance: 0.1},
	}
	renderer.OnSources(ctx, sources)
	renderer.OnDone(ctx, "")

	output := buf.String()
	if !strings.Contains(output, "SOURCE: doc.pdf score=0.9500") {
		t.Errorf("expected SOURCE with score in output, got %q", output)
	}
	if !strings.Contains(output, "SOURCE: notes.md distance=0.1000") {
		t.Errorf("expected SOURCE with distance in output, got %q", output)
	}
}

func TestTerminalStreamRenderer_OnSources_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMinimal)
	ctx := context.Background()

	sources := []SourceInfo{{Source: "test.pdf"}}
	renderer.OnSources(ctx, sources)

	output := buf.String()
	if !strings.Contains(output, "Sources:") {
		t.Errorf("expected Sources header, got %q", output)
	}
	if !strings.Contains(output, "1. test.pdf") {
		t.Errorf("expected numbered source, got %q", output)
	}
}

func TestTerminalStreamRenderer_OnSources_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnSources(ctx, []SourceInfo{})
	renderer.OnDone(ctx, "")

	output := buf.String()
	if strings.Contains(output, "SOURCE:") {
		t.Errorf("expected no SOURCE output for empty sources, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// OnError Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnError_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnError(ctx, errors.New("connection failed"))

	output := buf.String()
	if !strings.Contains(output, "ERROR: connection failed") {
		t.Errorf("expected ERROR in output, got %q", output)
	}

	result := renderer.Result()
	if result.Error != "connection failed" {
		t.Errorf("expected Error 'connection failed', got %q", result.Error)
	}
}

// -----------------------------------------------------------------------------
// OnDone Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_OnDone_SetsCompletedAt(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	result1 := renderer.Result()
	if result1.CompletedAt != 0 {
		t.Error("expected CompletedAt to be 0 before OnDone")
	}

	renderer.OnDone(ctx, "sess-xyz")

	result2 := renderer.Result()
	if result2.CompletedAt == 0 {
		t.Error("expected CompletedAt to be set after OnDone")
	}
	if result2.SessionID != "sess-xyz" {
		t.Errorf("expected SessionID 'sess-xyz', got %q", result2.SessionID)
	}
}

// -----------------------------------------------------------------------------
// Finalize Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_Finalize_Idempotent(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnToken(ctx, "test")

	// Call Finalize multiple times
	renderer.Finalize()
	renderer.Finalize()
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "test" {
		t.Errorf("expected Answer 'test', got %q", result.Answer)
	}
}

func TestTerminalStreamRenderer_Finalize_IgnoresSubsequentCalls(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnToken(ctx, "first")
	renderer.Finalize()

	// These should be ignored
	renderer.OnToken(ctx, "second")
	renderer.OnDone(ctx, "sess")

	result := renderer.Result()
	if result.Answer != "first" {
		t.Errorf("expected Answer 'first', got %q", result.Answer)
	}
}

// -----------------------------------------------------------------------------
// Result Tests
// -----------------------------------------------------------------------------

func TestTerminalStreamRenderer_Result_Metrics(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnStatus(ctx, "starting")
	renderer.OnSources(ctx, []SourceInfo{{Source: "doc.pdf"}})
	renderer.OnThinking(ctx, "hmm")
	renderer.OnToken(ctx, "a")
	renderer.OnToken(ctx, "b")
	renderer.OnToken(ctx, "c")
	renderer.OnDone(ctx, "sess")

	result := renderer.Result()
	if result.TotalTokens != 3 {
		t.Errorf("expected TotalTokens 3, got %d", result.TotalTokens)
	}
	if result.ThinkingTokens != 1 {
		t.Errorf("expected ThinkingTokens 1, got %d", result.ThinkingTokens)
	}
	if result.TotalEvents != 7 {
		t.Errorf("expected TotalEvents 7, got %d", result.TotalEvents)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}

// =============================================================================
// Buffer Stream Renderer Tests
// =============================================================================

func TestNewBufferStreamRenderer(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	if renderer == nil {
		t.Fatal("NewBufferStreamRenderer() returned nil")
	}

	result := renderer.Result()
	if result.Id == "" {
		t.Error("expected Id to be set")
	}
}

func TestBufferStreamRenderer_CapturesAllEventTypes(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	bufRenderer := renderer.(*bufferStreamRenderer)
	ctx := context.Background()

	renderer.OnStatus(ctx, "starting")
	renderer.OnSources(ctx, []SourceInfo{{Source: "doc.pdf"}})
	renderer.OnThinking(ctx, "thinking...")
	renderer.OnToken(ctx, "answer")
	renderer.OnDone(ctx, "sess-123")

	events := bufRenderer.Events()
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

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

func TestBufferStreamRenderer_Result(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	ctx := context.Background()

	renderer.OnThinking(ctx, "let me think")
	renderer.OnToken(ctx, "hello ")
	renderer.OnToken(ctx, "world")
	renderer.OnSources(ctx, []SourceInfo{{Source: "doc.pdf", Score: 0.9}})
	renderer.OnDone(ctx, "sess-abc")
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "hello world" {
		t.Errorf("expected Answer 'hello world', got %q", result.Answer)
	}
	if result.Thinking != "let me think" {
		t.Errorf("expected Thinking 'let me think', got %q", result.Thinking)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("expected SessionID 'sess-abc', got %q", result.SessionID)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}

func TestBufferStreamRenderer_OnError(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	ctx := context.Background()

	renderer.OnToken(ctx, "partial")
	renderer.OnError(ctx, errors.New("stream failed"))
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "partial" {
		t.Errorf("expected Answer 'partial', got %q", result.Answer)
	}
	if result.Error != "stream failed" {
		t.Errorf("expected Error 'stream failed', got %q", result.Error)
	}
	if result.CompletedAt == 0 {
		t.Error("expected CompletedAt to be set")
	}
}

func TestBufferStreamRenderer_Finalize_Idempotent(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	ctx := context.Background()

	renderer.OnToken(ctx, "test")

	renderer.Finalize()
	renderer.Finalize()
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "test" {
		t.Errorf("expected Answer 'test', got %q", result.Answer)
	}
}

func TestBufferStreamRenderer_Events_ReturnsCopy(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	bufRenderer := renderer.(*bufferStreamRenderer)
	ctx := context.Background()

	renderer.OnToken(ctx, "test")

	events1 := bufRenderer.Events()
	events2 := bufRenderer.Events()

	// Modify events1
	events1[0].Content = "modified"

	// events2 should be unaffected
	if events2[0].Content != "test" {
		t.Error("Events() should return a copy, not a reference")
	}
}

// =============================================================================
// Concurrent Safety Tests
// =============================================================================

func TestTerminalStreamRenderer_ConcurrentSafety(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				renderer.OnToken(ctx, "x")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	renderer.Finalize()
	result := renderer.Result()
	if result.TotalTokens != 1000 {
		t.Errorf("expected TotalTokens 1000, got %d", result.TotalTokens)
	}
}

func TestBufferStreamRenderer_ConcurrentSafety(t *testing.T) {
	renderer := NewBufferStreamRenderer()
	ctx := context.Background()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				renderer.OnToken(ctx, "x")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	renderer.Finalize()
	result := renderer.Result()
	if result.TotalTokens != 1000 {
		t.Errorf("expected TotalTokens 1000, got %d", result.TotalTokens)
	}

	bufRenderer := renderer.(*bufferStreamRenderer)
	events := bufRenderer.Events()
	if len(events) != 1000 {
		t.Errorf("expected 1000 events, got %d", len(events))
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestTerminalStreamRenderer_FullFlow_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	// Simulate a typical RAG streaming response
	renderer.OnStatus(ctx, "Searching knowledge base...")
	renderer.OnSources(ctx, []SourceInfo{
		{Source: "architecture.pdf", Score: 0.95},
		{Source: "api-docs.md", Score: 0.87},
	})
	renderer.OnStatus(ctx, "Generating response...")
	renderer.OnThinking(ctx, "Let me analyze the relevant documents...")
	renderer.OnToken(ctx, "Based on ")
	renderer.OnToken(ctx, "the documentation, ")
	renderer.OnToken(ctx, "the answer is 42.")
	renderer.OnDone(ctx, "sess-test-123")
	renderer.Finalize()

	output := buf.String()

	// Verify all expected output
	expectedParts := []string{
		"STATUS: Searching knowledge base...",
		"SOURCE: architecture.pdf score=0.9500",
		"SOURCE: api-docs.md score=0.8700",
		"STATUS: Generating response...",
		"THINKING: Let me analyze the relevant documents...",
		"ANSWER: Based on the documentation, the answer is 42.",
		"SESSION: sess-test-123",
		"DONE",
	}

	for _, expected := range expectedParts {
		if !strings.Contains(output, expected) {
			t.Errorf("expected output to contain %q, got:\n%s", expected, output)
		}
	}

	// Verify result
	result := renderer.Result()
	if result.Answer != "Based on the documentation, the answer is 42." {
		t.Errorf("unexpected Answer: %q", result.Answer)
	}
	if result.Thinking != "Let me analyze the relevant documents..." {
		t.Errorf("unexpected Thinking: %q", result.Thinking)
	}
	if result.TotalTokens != 3 {
		t.Errorf("expected TotalTokens 3, got %d", result.TotalTokens)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
}

// =============================================================================
// Full Personality Mode Tests (Terminal Renderer)
// =============================================================================

func TestTerminalStreamRenderer_OnStatus_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	// Status should start a spinner
	renderer.OnStatus(ctx, "Searching...")
	renderer.OnStatus(ctx, "Processing...")
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if result.TotalEvents < 2 {
		t.Errorf("expected at least 2 events, got %d", result.TotalEvents)
	}
}

func TestTerminalStreamRenderer_OnToken_FullMode_StopsSpinner(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	// Start a spinner with status
	renderer.OnStatus(ctx, "Thinking...")
	// First token should stop the spinner
	renderer.OnToken(ctx, "Hello")
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	output := buf.String()
	if !strings.Contains(output, "Hello") {
		t.Errorf("expected token in output, got %q", output)
	}
}

func TestTerminalStreamRenderer_OnThinking_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	renderer.OnThinking(ctx, "Let me think about this...")
	renderer.OnToken(ctx, "The answer is 42.")
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if result.Thinking != "Let me think about this..." {
		t.Errorf("unexpected Thinking: %q", result.Thinking)
	}
}

func TestTerminalStreamRenderer_OnThinking_StopsSpinner(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	// Start a spinner
	renderer.OnStatus(ctx, "Processing...")
	// Thinking should stop the spinner
	renderer.OnThinking(ctx, "Analyzing...")
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if result.ThinkingTokens != 1 {
		t.Errorf("expected ThinkingTokens 1, got %d", result.ThinkingTokens)
	}
}

func TestTerminalStreamRenderer_OnSources_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	sources := []SourceInfo{
		{Source: "document.pdf", Score: 0.92},
		{Source: "notes.md", Distance: 0.15},
	}
	renderer.OnSources(ctx, sources)
	renderer.OnToken(ctx, "Based on the sources...")
	renderer.OnDone(ctx, "sess-full")
	renderer.Finalize()

	result := renderer.Result()
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
}

func TestTerminalStreamRenderer_OnSources_StopsSpinner(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	// Start a spinner
	renderer.OnStatus(ctx, "Searching...")
	// Sources should stop the spinner
	renderer.OnSources(ctx, []SourceInfo{{Source: "doc.pdf"}})
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}

func TestTerminalStreamRenderer_OnDone_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	renderer.OnToken(ctx, "Answer")
	renderer.OnDone(ctx, "sess-full-test")
	renderer.Finalize()

	result := renderer.Result()
	if result.SessionID != "sess-full-test" {
		t.Errorf("expected SessionID 'sess-full-test', got %q", result.SessionID)
	}
}

func TestTerminalStreamRenderer_OnError_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	renderer.OnStatus(ctx, "Working...")
	renderer.OnError(ctx, errors.New("something went wrong"))
	renderer.Finalize()

	result := renderer.Result()
	if result.Error != "something went wrong" {
		t.Errorf("expected Error 'something went wrong', got %q", result.Error)
	}
}

func TestTerminalStreamRenderer_OnError_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMinimal)
	ctx := context.Background()

	renderer.OnError(ctx, errors.New("error message"))
	renderer.Finalize()

	output := buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("expected error message in output, got %q", output)
	}
}

func TestTerminalStreamRenderer_FullFlow_FullMode(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	// Simulate a typical RAG streaming response in full mode
	renderer.OnStatus(ctx, "Searching knowledge base...")
	renderer.OnSources(ctx, []SourceInfo{
		{Source: "architecture.pdf", Score: 0.95},
	})
	renderer.OnStatus(ctx, "Generating response...")
	renderer.OnThinking(ctx, "Let me analyze...")
	renderer.OnToken(ctx, "The answer ")
	renderer.OnToken(ctx, "is 42.")
	renderer.OnDone(ctx, "sess-test")
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "The answer is 42." {
		t.Errorf("unexpected Answer: %q", result.Answer)
	}
	if result.Thinking != "Let me analyze..." {
		t.Errorf("unexpected Thinking: %q", result.Thinking)
	}
}

// =============================================================================
// Edge Cases and Error Handling
// =============================================================================

func TestTerminalStreamRenderer_OnDone_StopsSpinner(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	renderer.OnStatus(ctx, "Working...")
	// OnDone should clean up the spinner
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if result.CompletedAt == 0 {
		t.Error("expected CompletedAt to be set")
	}
}

func TestTerminalStreamRenderer_IgnoresAfterFinalized(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityFull)
	ctx := context.Background()

	renderer.OnToken(ctx, "first")
	renderer.Finalize()

	// These should be ignored
	renderer.OnStatus(ctx, "should be ignored")
	renderer.OnThinking(ctx, "ignored thinking")
	renderer.OnSources(ctx, []SourceInfo{{Source: "ignored.pdf"}})
	renderer.OnError(ctx, errors.New("ignored error"))

	result := renderer.Result()
	if result.Answer != "first" {
		t.Errorf("expected Answer 'first', got %q", result.Answer)
	}
}

func TestTerminalStreamRenderer_OnSources_WithNoScoreOrDistance(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	sources := []SourceInfo{{Source: "plain.txt"}}
	renderer.OnSources(ctx, sources)
	renderer.OnDone(ctx, "")

	output := buf.String()
	if !strings.Contains(output, "SOURCE: plain.txt\n") {
		t.Errorf("expected SOURCE without metric, got %q", output)
	}
}

func TestTerminalStreamRenderer_NilWriter(t *testing.T) {
	// Should not panic with nil writer (uses os.Stdout internally)
	renderer := NewTerminalStreamRenderer(nil, PersonalityMachine)
	ctx := context.Background()

	renderer.OnToken(ctx, "test")
	renderer.OnDone(ctx, "")
	renderer.Finalize()

	result := renderer.Result()
	if result.Answer != "test" {
		t.Errorf("expected Answer 'test', got %q", result.Answer)
	}
}

// =============================================================================
// MachineStreamRenderer Full Workflow
// =============================================================================

func TestMachineStreamRenderer_PrintsAllBuffered(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewTerminalStreamRenderer(&buf, PersonalityMachine)
	ctx := context.Background()

	renderer.OnThinking(ctx, "thinking content")
	renderer.OnToken(ctx, "answer content")
	renderer.OnSources(ctx, []SourceInfo{{Source: "src.pdf", Score: 0.5}})
	renderer.OnDone(ctx, "sess-machine")

	output := buf.String()
	if !strings.Contains(output, "THINKING: thinking content") {
		t.Errorf("expected THINKING in output, got %q", output)
	}
	if !strings.Contains(output, "ANSWER: answer content") {
		t.Errorf("expected ANSWER in output, got %q", output)
	}
	if !strings.Contains(output, "SOURCE: src.pdf score=0.5000") {
		t.Errorf("expected SOURCE in output, got %q", output)
	}
	if !strings.Contains(output, "SESSION: sess-machine") {
		t.Errorf("expected SESSION in output, got %q", output)
	}
}

// =============================================================================
// RenderStreamToResult Tests
// =============================================================================

func TestRenderStreamToResult_BasicFlow(t *testing.T) {
	stream := strings.NewReader(`data: {"type":"status","message":"Working..."}
data: {"type":"token","content":"Hello "}
data: {"type":"token","content":"world"}
data: {"type":"done","session_id":"sess-render"}
`)

	reader := NewSSEStreamReader(NewSSEParser())
	ctx := context.Background()

	result, err := RenderStreamToResult(ctx, reader, stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Hello world" {
		t.Errorf("expected Answer 'Hello world', got %q", result.Answer)
	}
	if result.SessionID != "sess-render" {
		t.Errorf("expected SessionID 'sess-render', got %q", result.SessionID)
	}
}

func TestRenderStreamToResult_WithSources(t *testing.T) {
	stream := strings.NewReader(`data: {"type":"sources","sources":[{"source":"doc.pdf","score":0.9}]}
data: {"type":"token","content":"Answer"}
data: {"type":"done"}
`)

	reader := NewSSEStreamReader(NewSSEParser())
	ctx := context.Background()

	result, err := RenderStreamToResult(ctx, reader, stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}

func TestRenderStreamToResult_EmptyStream(t *testing.T) {
	stream := strings.NewReader("")

	reader := NewSSEStreamReader(NewSSEParser())
	ctx := context.Background()

	result, err := RenderStreamToResult(ctx, reader, stream)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "" {
		t.Errorf("expected empty Answer, got %q", result.Answer)
	}
}
