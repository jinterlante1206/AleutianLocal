// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ux

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
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
// HeaderWithConfig Tests (TTL and DataSpace)
// -----------------------------------------------------------------------------

func TestChatUI_HeaderWithConfig_TTL_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "verified",
		SessionID: "sess-123",
		TTL:       "5m",
	})

	output := buf.String()
	if !strings.Contains(output, "ttl=5m") {
		t.Errorf("expected ttl=5m in output, got %q", output)
	}
	if !strings.Contains(output, "pipeline=verified") {
		t.Errorf("expected pipeline=verified, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpace_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "reranking",
		DataSpace: "wheat",
	})

	output := buf.String()
	if !strings.Contains(output, "dataspace=wheat") {
		t.Errorf("expected dataspace=wheat in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_TTLAndDataSpace_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "verified",
		SessionID: "sess-456",
		TTL:       "24h",
		DataSpace: "production",
	})

	output := buf.String()
	if !strings.Contains(output, "ttl=24h") {
		t.Errorf("expected ttl=24h in output, got %q", output)
	}
	if !strings.Contains(output, "dataspace=production") {
		t.Errorf("expected dataspace=production in output, got %q", output)
	}
	if !strings.Contains(output, "session=sess-456") {
		t.Errorf("expected session=sess-456 in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_TTL_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:     ChatModeRAG,
		Pipeline: "graph",
		TTL:      "7d",
	})

	output := buf.String()
	if !strings.Contains(output, "TTL: 7d") {
		t.Errorf("expected TTL: 7d in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpace_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "standard",
		DataSpace: "work",
	})

	output := buf.String()
	if !strings.Contains(output, "Dataspace: work") {
		t.Errorf("expected Dataspace: work in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_TTL_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "verified",
		SessionID: "sess-full",
		TTL:       "1h",
	})

	output := buf.String()
	if !strings.Contains(output, "TTL:") {
		t.Errorf("expected TTL: in output, got %q", output)
	}
	if !strings.Contains(output, "1h") {
		t.Errorf("expected 1h in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpace_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "reranking",
		DataSpace: "personal",
	})

	output := buf.String()
	if !strings.Contains(output, "Dataspace:") {
		t.Errorf("expected Dataspace: in output, got %q", output)
	}
	if !strings.Contains(output, "personal") {
		t.Errorf("expected personal in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_BothTTLAndDataSpace_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "verified",
		SessionID: "sess-both",
		TTL:       "30m",
		DataSpace: "research",
	})

	output := buf.String()
	// Should show both TTL and DataSpace
	if !strings.Contains(output, "TTL:") {
		t.Errorf("expected TTL: in output, got %q", output)
	}
	if !strings.Contains(output, "30m") {
		t.Errorf("expected 30m in output, got %q", output)
	}
	if !strings.Contains(output, "Dataspace:") {
		t.Errorf("expected Dataspace: in output, got %q", output)
	}
	if !strings.Contains(output, "research") {
		t.Errorf("expected research in output, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_NoTTLOrDataSpace(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "standard",
		SessionID: "sess-none",
	})

	output := buf.String()
	// Should NOT contain TTL or Dataspace when not provided
	if strings.Contains(output, "TTL:") {
		t.Errorf("expected no TTL line when empty, got %q", output)
	}
	if strings.Contains(output, "Dataspace:") {
		t.Errorf("expected no Dataspace line when empty, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DirectMode_IgnoresTTLAndDataSpace(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeDirect,
		SessionID: "sess-direct",
		TTL:       "5m",      // Should be ignored for direct mode
		DataSpace: "ignored", // Should be ignored for direct mode
	})

	output := buf.String()
	// Direct mode header should not include TTL or dataspace
	if !strings.Contains(output, "mode=direct") {
		t.Errorf("expected mode=direct, got %q", output)
	}
	// These should NOT appear in direct mode output
	if strings.Contains(output, "ttl=") {
		t.Errorf("unexpected ttl in direct mode, got %q", output)
	}
	if strings.Contains(output, "dataspace=") {
		t.Errorf("unexpected dataspace in direct mode, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpaceStats_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "reranking",
		DataSpace: "wheat",
		DataSpaceStats: &DataSpaceStats{
			DocumentCount: 142,
			LastUpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
		},
	})

	output := buf.String()
	if !strings.Contains(output, "dataspace=wheat") {
		t.Errorf("expected dataspace=wheat, got %q", output)
	}
	if !strings.Contains(output, "chunks=142") {
		t.Errorf("expected chunks=142, got %q", output)
	}
	if !strings.Contains(output, "last_updated=") {
		t.Errorf("expected last_updated=, got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpaceStats_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "reranking",
		DataSpace: "wheat",
		DataSpaceStats: &DataSpaceStats{
			DocumentCount: 142,
			LastUpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
		},
	})

	output := buf.String()
	if !strings.Contains(output, "Dataspace: wheat (142 chunks)") {
		t.Errorf("expected 'Dataspace: wheat (142 chunks)', got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpaceStats_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:      ChatModeRAG,
		Pipeline:  "reranking",
		DataSpace: "wheat",
		DataSpaceStats: &DataSpaceStats{
			DocumentCount: 142,
			LastUpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
		},
	})

	output := buf.String()
	// Should contain chunk count and relative time
	if !strings.Contains(output, "142 chunks") {
		t.Errorf("expected '142 chunks', got %q", output)
	}
	if !strings.Contains(output, "updated") {
		t.Errorf("expected 'updated' (relative time), got %q", output)
	}
}

func TestChatUI_HeaderWithConfig_DataSpaceStatsNil_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.HeaderWithConfig(HeaderConfig{
		Mode:           ChatModeRAG,
		Pipeline:       "reranking",
		DataSpace:      "wheat",
		DataSpaceStats: nil, // No stats available
	})

	output := buf.String()
	// Should still show dataspace but without stats
	if !strings.Contains(output, "wheat") {
		t.Errorf("expected 'wheat' dataspace, got %q", output)
	}
	// Should NOT contain chunk count
	if strings.Contains(output, "chunks") {
		t.Errorf("unexpected 'chunks' when stats are nil, got %q", output)
	}
}

// -----------------------------------------------------------------------------
// formatRelativeTime Tests
// -----------------------------------------------------------------------------

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		unixMs   int64
		expected string
	}{
		{"zero", 0, "unknown"},
		{"just now", now.Add(-30 * time.Second).UnixMilli(), "just now"},
		{"1 min ago", now.Add(-1 * time.Minute).UnixMilli(), "1 min ago"},
		{"5 mins ago", now.Add(-5 * time.Minute).UnixMilli(), "5 mins ago"},
		{"1h ago", now.Add(-1 * time.Hour).UnixMilli(), "1h ago"},
		{"2h ago", now.Add(-2 * time.Hour).UnixMilli(), "2h ago"},
		{"1 day ago", now.Add(-24 * time.Hour).UnixMilli(), "1 day ago"},
		{"3 days ago", now.Add(-3 * 24 * time.Hour).UnixMilli(), "3 days ago"},
		{"1 week ago", now.Add(-7 * 24 * time.Hour).UnixMilli(), "1 week ago"},
		{"2 weeks ago", now.Add(-14 * 24 * time.Hour).UnixMilli(), "2 weeks ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRelativeTime(tt.unixMs)
			if result != tt.expected {
				t.Errorf("formatRelativeTime(%d) = %q, want %q", tt.unixMs, result, tt.expected)
			}
		})
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
// SessionEndRich Tests
// =============================================================================

func TestChatUI_SessionEndRich_MachineMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMachine)

	stats := &SessionStats{
		MessageCount: 5,
		TotalTokens:  1234,
		Duration:     2 * time.Minute,
	}
	ui.SessionEndRich("sess-123", stats)

	output := buf.String()
	if !strings.Contains(output, "CHAT_END:") {
		t.Errorf("expected CHAT_END prefix, got %q", output)
	}
	if !strings.Contains(output, "session=sess-123") {
		t.Errorf("expected session ID, got %q", output)
	}
	if !strings.Contains(output, "messages=5") {
		t.Errorf("expected message count, got %q", output)
	}
	if !strings.Contains(output, "tokens=1234") {
		t.Errorf("expected token count, got %q", output)
	}
}

func TestChatUI_SessionEndRich_MinimalMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	stats := &SessionStats{
		MessageCount: 3,
		TotalTokens:  500,
		Duration:     30 * time.Second,
	}
	ui.SessionEndRich("sess-456", stats)

	output := buf.String()
	if !strings.Contains(output, "Session: sess-456") {
		t.Errorf("expected session ID, got %q", output)
	}
	if !strings.Contains(output, "Messages: 3") {
		t.Errorf("expected message count, got %q", output)
	}
	if !strings.Contains(output, "Tokens: 500") {
		t.Errorf("expected token count, got %q", output)
	}
	if !strings.Contains(output, "Goodbye") {
		t.Errorf("expected goodbye, got %q", output)
	}
}

func TestChatUI_SessionEndRich_NilStatsFallback(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityMinimal)

	ui.SessionEndRich("sess-789", nil)

	output := buf.String()
	// Should fall back to simple SessionEnd behavior
	if !strings.Contains(output, "Goodbye") {
		t.Errorf("expected goodbye message, got %q", output)
	}
}

func TestSessionStats_Fields(t *testing.T) {
	stats := SessionStats{
		MessageCount:         10,
		TotalTokens:          5000,
		ThinkingTokens:       500,
		SourcesUsed:          3,
		Duration:             5 * time.Minute,
		FirstResponseLatency: 200 * time.Millisecond,
		AverageResponseTime:  2 * time.Second,
	}

	if stats.MessageCount != 10 {
		t.Errorf("expected MessageCount 10, got %d", stats.MessageCount)
	}
	if stats.TotalTokens != 5000 {
		t.Errorf("expected TotalTokens 5000, got %d", stats.TotalTokens)
	}
	if stats.ThinkingTokens != 500 {
		t.Errorf("expected ThinkingTokens 500, got %d", stats.ThinkingTokens)
	}
	if stats.SourcesUsed != 3 {
		t.Errorf("expected SourcesUsed 3, got %d", stats.SourcesUsed)
	}
	if stats.Duration != 5*time.Minute {
		t.Errorf("expected Duration 5m, got %v", stats.Duration)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Millisecond, "500ms"},
		{5 * time.Second, "5.0s"},
		{90 * time.Second, "1m 30s"},
		{2 * time.Minute, "2m"},
		{65 * time.Minute, "1h 5m"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.input)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.input, result, tt.expected)
		}
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

// =============================================================================
// SessionEndRich Full Mode Tests
// =============================================================================

func TestChatUI_SessionEndRich_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	stats := &SessionStats{
		MessageCount:         8,
		TotalTokens:          2500,
		ThinkingTokens:       200,
		SourcesUsed:          5,
		Duration:             3 * time.Minute,
		FirstResponseLatency: 150 * time.Millisecond,
		AverageResponseTime:  1500 * time.Millisecond,
	}
	ui.SessionEndRich("sess-full-test", stats)

	output := buf.String()
	// Full mode should include rich formatting
	if !strings.Contains(output, "Session Summary") || !strings.Contains(output, "Goodbye") || !strings.Contains(output, "sess-full-test") {
		t.Errorf("expected session summary in full mode, got %q", output)
	}
}

func TestChatUI_SessionEndRich_FullMode_ZeroStats(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	stats := &SessionStats{
		MessageCount: 0,
		TotalTokens:  0,
		Duration:     0,
	}
	ui.SessionEndRich("sess-zero", stats)

	output := buf.String()
	// Should still render even with zero stats
	if !strings.Contains(output, "sess-zero") {
		t.Errorf("expected session ID, got %q", output)
	}
}

// =============================================================================
// Header Tests for Full Mode
// =============================================================================

func TestChatUI_Header_RAG_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.Header(ChatModeRAG, "reranking", "sess-full")

	output := buf.String()
	// Full mode should have styled output
	if len(output) == 0 {
		t.Error("expected non-empty output in full mode")
	}
}

func TestChatUI_Header_Direct_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.Header(ChatModeDirect, "", "sess-direct-full")

	output := buf.String()
	if len(output) == 0 {
		t.Error("expected non-empty output in full mode")
	}
}

// =============================================================================
// Sources Tests for Full Mode
// =============================================================================

func TestChatUI_Sources_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	sources := []SourceInfo{
		{Source: "doc1.pdf", Score: 0.95},
		{Source: "doc2.txt", Distance: 0.12},
	}
	ui.Sources(sources)

	output := buf.String()
	// Full mode should have styled output
	if len(output) == 0 {
		t.Error("expected non-empty output in full mode")
	}
}

func TestChatUI_Sources_FullMode_ManyItems(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	sources := []SourceInfo{
		{Source: "doc1.pdf", Score: 0.95},
		{Source: "doc2.txt", Score: 0.90},
		{Source: "doc3.md", Score: 0.85},
		{Source: "doc4.go", Score: 0.80},
		{Source: "doc5.py", Score: 0.75},
	}
	ui.Sources(sources)

	output := buf.String()
	if len(output) == 0 {
		t.Error("expected non-empty output for multiple sources in full mode")
	}
}

// =============================================================================
// NoSources Tests for Full Mode
// =============================================================================

func TestChatUI_NoSources_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.NoSources()

	output := buf.String()
	// Full mode may show a warning for no sources
	_ = output // Full mode might output something or nothing
}

// =============================================================================
// Error Tests for Full Mode
// =============================================================================

func TestChatUI_Error_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.Error(errors.New("test error"))

	output := buf.String()
	if !strings.Contains(output, "test error") {
		t.Errorf("expected error message, got %q", output)
	}
}

// =============================================================================
// SessionResume Tests for Full Mode
// =============================================================================

func TestChatUI_SessionResume_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.SessionResume("sess-resume", 10)

	output := buf.String()
	if len(output) == 0 {
		t.Error("expected non-empty output in full mode")
	}
}

// =============================================================================
// SessionEnd Tests for Full Mode
// =============================================================================

func TestChatUI_SessionEnd_FullMode(t *testing.T) {
	var buf bytes.Buffer
	ui := NewChatUIWithWriter(&buf, PersonalityFull)

	ui.SessionEnd("sess-end-full")

	output := buf.String()
	if len(output) == 0 {
		t.Error("expected non-empty output in full mode")
	}
}

// =============================================================================
// Global Function Tests (using default UI)
// =============================================================================

func TestGetDefaultChatUI(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ui := getDefaultChatUI()
	if ui == nil {
		t.Fatal("getDefaultChatUI returned nil")
	}

	// Calling again should return the same instance
	ui2 := getDefaultChatUI()
	if ui != ui2 {
		t.Error("getDefaultChatUI should return cached instance")
	}
}

func TestChatHeader_Global(t *testing.T) {
	// Reset global state for consistent test
	defaultChatUI = nil

	// This should not panic
	ChatHeader(ChatModeRAG, "reranking", "sess-global")
}

func TestChatSources_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	sources := []SourceInfo{{Source: "test.pdf", Score: 0.9}}
	ChatSources(sources)
}

func TestChatPrompt_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	prompt := ChatPrompt()
	if len(prompt) == 0 {
		t.Error("expected non-empty prompt")
	}
}

func TestChatResponse_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ChatResponse("test response")
}

func TestChatError_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ChatError(errors.New("test error"))
}

func TestChatSessionResume_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ChatSessionResume("sess-123", 5)
}

func TestChatSessionEnd_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ChatSessionEnd("sess-bye")
}

func TestChatNoSources_Global(t *testing.T) {
	// Reset global state
	defaultChatUI = nil

	ChatNoSources()
}

// =============================================================================
// NewChatUI Test
// =============================================================================

func TestNewChatUI(t *testing.T) {
	// This test verifies NewChatUI doesn't panic
	// It writes to os.Stdout so we can't easily capture output
	ui := NewChatUI()
	if ui == nil {
		t.Fatal("NewChatUI returned nil")
	}
}
