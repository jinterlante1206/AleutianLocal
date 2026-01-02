// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// terminalChatUI Tests
// =============================================================================

func TestNewChatUIWithWriter(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	if ui == nil {
		t.Fatal("NewChatUIWithWriter returned nil")
	}
}

// -----------------------------------------------------------------------------
// Header Tests
// -----------------------------------------------------------------------------

func TestChatUI_Header_RAG_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.Header(ChatModeRAG, "reranking", "sess-123")

	output := buf.String()
	if !strings.Contains(output, "CHAT_START: mode=rag") {
		t.Errorf("expected CHAT_START: mode=rag, got %q", output)
	}
	if !strings.Contains(output, "pipeline=reranking") {
		t.Errorf("expected pipeline=reranking, got %q", output)
	}
	if !strings.Contains(output, "session=sess-123") {
		t.Errorf("expected session=sess-123, got %q", output)
	}
}

func TestChatUI_Header_Direct_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.Header(ChatModeDirect, "", "sess-456")

	output := buf.String()
	if !strings.Contains(output, "CHAT_START: mode=direct") {
		t.Errorf("expected CHAT_START: mode=direct, got %q", output)
	}
	if !strings.Contains(output, "session=sess-456") {
		t.Errorf("expected session=sess-456, got %q", output)
	}
}

func TestChatUI_Header_RAG_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.Header(ChatModeRAG, "graph", "")

	output := buf.String()
	if !strings.Contains(output, "RAG Chat") {
		t.Errorf("expected RAG Chat header, got %q", output)
	}
	if !strings.Contains(output, "pipeline: graph") {
		t.Errorf("expected pipeline: graph, got %q", output)
	}
	if !strings.Contains(output, "Type 'exit' to end.") {
		t.Errorf("expected exit instructions, got %q", output)
	}
}

func TestChatUI_Header_Direct_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.Header(ChatModeDirect, "", "")

	output := buf.String()
	if !strings.Contains(output, "Direct Chat") {
		t.Errorf("expected Direct Chat header, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// Prompt Tests
// -----------------------------------------------------------------------------

func TestChatUI_Prompt_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	prompt := ui.Prompt()

	if prompt != "> " {
		t.Errorf("expected '> ', got %q", prompt)
	}
}

func TestChatUI_Prompt_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	prompt := ui.Prompt()

	// Should contain styled prompt (not plain "> ")
	if !strings.Contains(prompt, ">") {
		t.Errorf("expected prompt to contain '>', got %q", prompt)
	}
}

// -----------------------------------------------------------------------------
// Response Tests
// -----------------------------------------------------------------------------

func TestChatUI_Response_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.Response("Hello, this is the answer.")

	output := buf.String()
	if !strings.Contains(output, "RESPONSE:") {
		t.Errorf("expected RESPONSE: prefix, got %q", output)
	}
	if !strings.Contains(output, "Hello, this is the answer.") {
		t.Errorf("expected answer text, got %q", output)
	}
}

func TestChatUI_Response_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.Response("Test answer")

	output := buf.String()
	if !strings.Contains(output, "Test answer") {
		t.Errorf("expected answer text, got %q", output)
	}
	// Should not have RESPONSE: prefix in minimal mode
	if strings.Contains(output, "RESPONSE:") {
		t.Errorf("unexpected RESPONSE: prefix in minimal mode, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// Sources Tests
// -----------------------------------------------------------------------------

func TestChatUI_Sources_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	sources := []SourceInfo{
		{Source: "doc1.pdf", Score: 0.95},
		{Source: "doc2.txt", Distance: 0.12},
		{Source: "doc3.md"},
	}
	ui.Sources(sources)

	output := buf.String()
	if !strings.Contains(output, "SOURCE: doc1.pdf score=0.9500") {
		t.Errorf("expected doc1.pdf with score, got %q", output)
	}
	if !strings.Contains(output, "SOURCE: doc2.txt distance=0.1200") {
		t.Errorf("expected doc2.txt with distance, got %q", output)
	}
	if !strings.Contains(output, "SOURCE: doc3.md\n") {
		t.Errorf("expected doc3.md without metric, got %q", output)
	}
}

func TestChatUI_Sources_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.Sources([]SourceInfo{})

	output := buf.String()
	if output != "" {
		t.Errorf("expected no output for empty sources, got %q", output)
	}
}

func TestChatUI_Sources_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	sources := []SourceInfo{
		{Source: "test.pdf", Score: 0.8},
	}
	ui.Sources(sources)

	output := buf.String()
	if !strings.Contains(output, "Sources:") {
		t.Errorf("expected Sources: header, got %q", output)
	}
	if !strings.Contains(output, "1. test.pdf") {
		t.Errorf("expected numbered source, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// NoSources Tests
// -----------------------------------------------------------------------------

func TestChatUI_NoSources_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.NoSources()

	output := buf.String()
	if !strings.Contains(output, "SOURCES: none") {
		t.Errorf("expected SOURCES: none, got %q", output)
	}
}

func TestChatUI_NoSources_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.NoSources()

	output := buf.String()
	// Minimal mode doesn't print anything for no sources
	if output != "" {
		t.Errorf("expected no output in minimal mode, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// Error Tests
// -----------------------------------------------------------------------------

func TestChatUI_Error_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.Error(errors.New("connection refused"))

	output := buf.String()
	if !strings.Contains(output, "CHAT_ERROR:") {
		t.Errorf("expected CHAT_ERROR: prefix, got %q", output)
	}
	if !strings.Contains(output, "connection refused") {
		t.Errorf("expected error message, got %q", output)
	}
}

func TestChatUI_Error_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.Error(errors.New("timeout"))

	output := buf.String()
	if !strings.Contains(output, "timeout") {
		t.Errorf("expected error message, got %q", output)
	}
	if !strings.Contains(output, "Chat error") {
		t.Errorf("expected Chat error text, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// SessionResume Tests
// -----------------------------------------------------------------------------

func TestChatUI_SessionResume_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.SessionResume("sess-abc", 5)

	output := buf.String()
	if !strings.Contains(output, "SESSION_RESUME:") {
		t.Errorf("expected SESSION_RESUME: prefix, got %q", output)
	}
	if !strings.Contains(output, "session=sess-abc") {
		t.Errorf("expected session ID, got %q", output)
	}
	if !strings.Contains(output, "turns=5") {
		t.Errorf("expected turn count, got %q", output)
	}
}

func TestChatUI_SessionResume_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.SessionResume("sess-xyz", 3)

	output := buf.String()
	if !strings.Contains(output, "sess-xyz") {
		t.Errorf("expected session ID, got %q", output)
	}
	if !strings.Contains(output, "3 previous turns") {
		t.Errorf("expected turn count message, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// SessionEnd Tests
// -----------------------------------------------------------------------------

func TestChatUI_SessionEnd_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.SessionEnd("sess-end-123")

	output := buf.String()
	if !strings.Contains(output, "CHAT_END:") {
		t.Errorf("expected CHAT_END: prefix, got %q", output)
	}
	if !strings.Contains(output, "session=sess-end-123") {
		t.Errorf("expected session ID, got %q", output)
	}
}

func TestChatUI_SessionEnd_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.SessionEnd("sess-bye")

	output := buf.String()
	if !strings.Contains(output, "sess-bye") {
		t.Errorf("expected session ID, got %q", output)
	}
	if !strings.Contains(output, "Goodbye") {
		t.Errorf("expected goodbye message, got %q", output)
	}
}

func TestChatUI_SessionEnd_EmptySessionID(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.SessionEnd("")

	output := buf.String()
	if !strings.Contains(output, "Goodbye") {
		t.Errorf("expected goodbye message, got %q", output)
	}
}

// =============================================================================
// Convenience Function Tests
// =============================================================================

func TestChatMode_Values(t *testing.T) {
	if ChatModeRAG != 0 {
		t.Errorf("expected ChatModeRAG to be 0, got %d", ChatModeRAG)
	}
	if ChatModeDirect != 1 {
		t.Errorf("expected ChatModeDirect to be 1, got %d", ChatModeDirect)
	}
}

func TestSourceInfo_Fields(t *testing.T) {
	src := SourceInfo{
		Source:   "test.pdf",
		Distance: 0.5,
		Score:    0.8,
	}

	if src.Source != "test.pdf" {
		t.Errorf("expected Source to be test.pdf, got %s", src.Source)
	}
	if src.Distance != 0.5 {
		t.Errorf("expected Distance to be 0.5, got %f", src.Distance)
	}
	if src.Score != 0.8 {
		t.Errorf("expected Score to be 0.8, got %f", src.Score)
	}
}
