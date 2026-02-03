// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package control

import (
	"log/slog"
	"strings"
	"testing"
)

func TestBufferExtractor_ValidFinalResponse(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "This is a valid final response that is longer than 50 characters and contains useful information.",
		ToolResults:   []ToolResultInput{},
		Query:         "Test query",
	}

	result := extractor.Extract(state)

	if result.IsFallback {
		t.Error("Should not be fallback when valid final response exists")
	}
	if result.Response != state.FinalResponse {
		t.Error("Should return the final response unchanged")
	}
}

func TestBufferExtractor_ShortFinalResponse(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "Too short", // Less than 50 chars
		ToolResults: []ToolResultInput{
			{ToolName: "read_file", Content: "File content here", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if !result.IsFallback {
		t.Error("Should be fallback when final response is too short")
	}
}

func TestBufferExtractor_NoToolResults(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults:   []ToolResultInput{},
		Query:         "Test query",
	}

	result := extractor.Extract(state)

	if !result.IsFallback {
		t.Error("Should be fallback when no tool results")
	}
	if !strings.Contains(result.Response, "wasn't able to gather") {
		t.Error("Should contain no-information message")
	}
}

func TestBufferExtractor_SuccessfulToolResults(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "read_file", Content: "File: main.go\nfunc main() {}", Success: true},
			{ToolName: "search", Content: "Found 3 matches in services/api/handler.go", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if !result.IsFallback {
		t.Error("Should be fallback")
	}
	if result.SourceCount != 2 {
		t.Errorf("SourceCount = %d, want 2", result.SourceCount)
	}
	if !strings.Contains(result.Response, "Based on my exploration") {
		t.Error("Should contain fallback response header")
	}
	if !strings.Contains(result.Response, "Key findings") {
		t.Error("Should contain key findings section")
	}
}

func TestBufferExtractor_FilterFailedResults(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "read_file", Content: "Success content", Success: true},
			{ToolName: "search", Content: "Error: file not found", Success: false},
			{ToolName: "analyze", Content: "", Success: true}, // Empty content
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	// Should only count the successful result with content
	if result.SourceCount != 1 {
		t.Errorf("SourceCount = %d, want 1 (only successful with content)", result.SourceCount)
	}
}

func TestBufferExtractor_ExtractFilePath(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "read_file", Content: "File: services/api/handler.go\nContent here", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if !strings.Contains(result.Response, "services/api/handler.go") {
		t.Error("Should extract and display file path")
	}
	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", result.FileCount)
	}
}

func TestBufferExtractor_MaxFindings(t *testing.T) {
	config := DefaultExtractionConfig()
	config.MaxFindings = 2
	extractor := NewBufferExtractor(config, slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "tool1", Content: "Finding 1", Success: true},
			{ToolName: "tool2", Content: "Finding 2", Success: true},
			{ToolName: "tool3", Content: "Finding 3", Success: true},
			{ToolName: "tool4", Content: "Finding 4", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	// Should include disclaimer about findings
	if !strings.Contains(result.Response, "Key findings") {
		t.Error("Should have key findings section")
	}
}

func TestBufferExtractor_TruncateSummary(t *testing.T) {
	config := DefaultExtractionConfig()
	config.MaxSummaryLength = 50
	extractor := NewBufferExtractor(config, slog.Default())

	longContent := strings.Repeat("This is a very long content that should be truncated. ", 10)

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "read_file", Content: longContent, Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if !strings.Contains(result.Response, "...") {
		t.Error("Long content should be truncated with ellipsis")
	}
}

func TestBufferExtractor_Disclaimer(t *testing.T) {
	config := DefaultExtractionConfig()
	config.IncludeDisclaimer = true
	extractor := NewBufferExtractor(config, slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "tool", Content: "Some content", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if !strings.Contains(result.Response, "Note:") {
		t.Error("Should include disclaimer when configured")
	}
}

func TestBufferExtractor_NoDisclaimer(t *testing.T) {
	config := DefaultExtractionConfig()
	config.IncludeDisclaimer = false
	extractor := NewBufferExtractor(config, slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{ToolName: "tool", Content: "Some content", Success: true},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	if strings.Contains(result.Response, "Note:") {
		t.Error("Should not include disclaimer when disabled")
	}
}

func TestBufferExtractor_HasUsableContent(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	tests := []struct {
		name  string
		state AgentStateInput
		want  bool
	}{
		{
			name: "valid final response",
			state: AgentStateInput{
				FinalResponse: strings.Repeat("a", 60),
				ToolResults:   []ToolResultInput{},
			},
			want: true,
		},
		{
			name: "successful tool results",
			state: AgentStateInput{
				FinalResponse: "",
				ToolResults: []ToolResultInput{
					{Success: true, Content: "content"},
				},
			},
			want: true,
		},
		{
			name: "no usable content",
			state: AgentStateInput{
				FinalResponse: "short",
				ToolResults:   []ToolResultInput{},
			},
			want: false,
		},
		{
			name: "only failed results",
			state: AgentStateInput{
				FinalResponse: "",
				ToolResults: []ToolResultInput{
					{Success: false, Content: "error"},
				},
			},
			want: false,
		},
		{
			name: "empty content in successful result",
			state: AgentStateInput{
				FinalResponse: "",
				ToolResults: []ToolResultInput{
					{Success: true, Content: ""},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractor.HasUsableContent(tc.state)
			if got != tc.want {
				t.Errorf("HasUsableContent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultExtractionConfig(t *testing.T) {
	config := DefaultExtractionConfig()

	if config.MinFinalResponseLength != 50 {
		t.Errorf("MinFinalResponseLength = %d, want 50", config.MinFinalResponseLength)
	}
	if config.MaxSummaryLength != 500 {
		t.Errorf("MaxSummaryLength = %d, want 500", config.MaxSummaryLength)
	}
	if config.MaxFindings != 10 {
		t.Errorf("MaxFindings = %d, want 10", config.MaxFindings)
	}
	if !config.IncludeDisclaimer {
		t.Error("IncludeDisclaimer should be true by default")
	}
}

func TestBufferExtractor_FilePathPatterns(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	tests := []struct {
		content  string
		expected string
	}{
		{
			content:  "File: main.go",
			expected: "main.go",
		},
		{
			content:  "reading file: services/api/handler.go",
			expected: "services/api/handler.go",
		},
		{
			content:  "Found in path/to/file.py",
			expected: "path/to/file.py",
		},
		{
			content:  "Found in path/to/file.js",
			expected: "path/to/file.js",
		},
		{
			content:  "Found in path/to/file.ts",
			expected: "path/to/file.ts",
		},
	}

	for _, tc := range tests {
		state := AgentStateInput{
			FinalResponse: "",
			ToolResults: []ToolResultInput{
				{ToolName: "read", Content: tc.content, Success: true},
			},
			Query: "Test query",
		}

		result := extractor.Extract(state)

		if !strings.Contains(result.Response, tc.expected) {
			t.Errorf("Content %q: expected file path %q in response", tc.content, tc.expected)
		}
	}
}

func TestBufferExtractor_SkipHeaderLines(t *testing.T) {
	extractor := NewBufferExtractor(DefaultExtractionConfig(), slog.Default())

	state := AgentStateInput{
		FinalResponse: "",
		ToolResults: []ToolResultInput{
			{
				ToolName: "read",
				Content:  "---\n===\nmain.go\nActual content here",
				Success:  true,
			},
		},
		Query: "Test query",
	}

	result := extractor.Extract(state)

	// Should skip the header lines and get to actual content
	if strings.Contains(result.Response, "---") {
		t.Error("Should skip header lines")
	}
}
