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
