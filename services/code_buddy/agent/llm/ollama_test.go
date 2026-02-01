// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package llm

import (
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/tools"
	"github.com/AleutianAI/AleutianFOSS/services/llm"
)

func TestConvertToolDefinitions_Empty(t *testing.T) {
	result := convertToolDefinitions(nil)
	if result != nil {
		t.Errorf("convertToolDefinitions(nil) = %v, want nil", result)
	}

	result = convertToolDefinitions([]tools.ToolDefinition{})
	if result != nil {
		t.Errorf("convertToolDefinitions([]) = %v, want nil", result)
	}
}

func TestConvertToolDefinitions_SingleTool(t *testing.T) {
	defs := []tools.ToolDefinition{
		{
			Name:        "read_file",
			Description: "Read contents of a file",
			Parameters: map[string]tools.ParamDef{
				"path": {
					Type:        tools.ParamTypeString,
					Description: "Path to the file",
					Required:    true,
				},
				"max_lines": {
					Type:        tools.ParamTypeInt,
					Description: "Maximum lines to read",
					Required:    false,
					Default:     100,
				},
			},
		},
	}

	result := convertToolDefinitions(defs)

	if len(result) != 1 {
		t.Fatalf("convertToolDefinitions() returned %d tools, want 1", len(result))
	}

	tool := result[0]
	if tool.Type != "function" {
		t.Errorf("tool.Type = %q, want 'function'", tool.Type)
	}
	if tool.Function.Name != "read_file" {
		t.Errorf("tool.Function.Name = %q, want 'read_file'", tool.Function.Name)
	}
	if tool.Function.Description != "Read contents of a file" {
		t.Errorf("tool.Function.Description = %q, want 'Read contents of a file'", tool.Function.Description)
	}
	if tool.Function.Parameters.Type != "object" {
		t.Errorf("tool.Function.Parameters.Type = %q, want 'object'", tool.Function.Parameters.Type)
	}
	if len(tool.Function.Parameters.Properties) != 2 {
		t.Errorf("tool.Function.Parameters.Properties has %d props, want 2", len(tool.Function.Parameters.Properties))
	}

	// Check path parameter
	pathProp, ok := tool.Function.Parameters.Properties["path"]
	if !ok {
		t.Fatal("missing 'path' property")
	}
	if pathProp.Type != "string" {
		t.Errorf("path.Type = %q, want 'string'", pathProp.Type)
	}
	if pathProp.Description != "Path to the file" {
		t.Errorf("path.Description = %q, want 'Path to the file'", pathProp.Description)
	}

	// Check required array
	foundPath := false
	for _, r := range tool.Function.Parameters.Required {
		if r == "path" {
			foundPath = true
		}
		if r == "max_lines" {
			t.Error("max_lines should not be in required array")
		}
	}
	if !foundPath {
		t.Error("'path' should be in required array")
	}
}

func TestConvertToolDefinitions_WithEnum(t *testing.T) {
	defs := []tools.ToolDefinition{
		{
			Name:        "format_code",
			Description: "Format source code",
			Parameters: map[string]tools.ParamDef{
				"language": {
					Type:        tools.ParamTypeString,
					Description: "Programming language",
					Required:    true,
					Enum:        []any{"go", "python", "javascript"},
				},
			},
		},
	}

	result := convertToolDefinitions(defs)

	if len(result) != 1 {
		t.Fatalf("convertToolDefinitions() returned %d tools, want 1", len(result))
	}

	langProp := result[0].Function.Parameters.Properties["language"]
	if len(langProp.Enum) != 3 {
		t.Errorf("language.Enum has %d values, want 3", len(langProp.Enum))
	}
}

func TestConvertToolDefinitions_MultipleTools(t *testing.T) {
	defs := []tools.ToolDefinition{
		{
			Name:        "tool_a",
			Description: "First tool",
			Parameters:  map[string]tools.ParamDef{},
		},
		{
			Name:        "tool_b",
			Description: "Second tool",
			Parameters:  map[string]tools.ParamDef{},
		},
		{
			Name:        "tool_c",
			Description: "Third tool",
			Parameters:  map[string]tools.ParamDef{},
		},
	}

	result := convertToolDefinitions(defs)

	if len(result) != 3 {
		t.Fatalf("convertToolDefinitions() returned %d tools, want 3", len(result))
	}

	names := make(map[string]bool)
	for _, tool := range result {
		names[tool.Function.Name] = true
	}

	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		if !names[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "empty string",
			content: "",
			want:    0,
		},
		{
			name:    "4 chars = 1 token",
			content: "test",
			want:    1,
		},
		{
			name:    "16 chars = 4 tokens",
			content: "this is a test!!", // 16 chars
			want:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.content)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestOllamaToolTypes(t *testing.T) {
	// Test that the Ollama tool types are correctly structured
	tool := llm.OllamaTool{
		Type: "function",
		Function: llm.OllamaToolFunction{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters: llm.OllamaToolParameters{
				Type: "object",
				Properties: map[string]llm.OllamaParamDef{
					"arg1": {
						Type:        "string",
						Description: "First argument",
					},
				},
				Required: []string{"arg1"},
			},
		},
	}

	if tool.Type != "function" {
		t.Errorf("tool.Type = %q, want 'function'", tool.Type)
	}
	if tool.Function.Name != "test_tool" {
		t.Errorf("tool.Function.Name = %q, want 'test_tool'", tool.Function.Name)
	}
}

func TestOllamaToolCall(t *testing.T) {
	// Test that tool call type is correctly structured
	tc := llm.OllamaToolCall{
		ID:   "call_123",
		Type: "function",
		Function: llm.OllamaFunctionCall{
			Name:      "read_file",
			Arguments: `{"path": "/test.go"}`,
		},
	}

	if tc.ID != "call_123" {
		t.Errorf("tc.ID = %q, want 'call_123'", tc.ID)
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("tc.Function.Name = %q, want 'read_file'", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"path": "/test.go"}` {
		t.Errorf("tc.Function.Arguments = %q, want '{\"path\": \"/test.go\"}'", tc.Function.Arguments)
	}
}

func TestChatWithToolsResult(t *testing.T) {
	// Test the result type
	result := llm.ChatWithToolsResult{
		Content:    "I'll read the file for you.",
		StopReason: "tool_use",
		ToolCalls: []llm.OllamaToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: llm.OllamaFunctionCall{
					Name:      "read_file",
					Arguments: `{"path": "/main.go"}`,
				},
			},
		},
	}

	if result.Content != "I'll read the file for you." {
		t.Errorf("result.Content = %q", result.Content)
	}
	if result.StopReason != "tool_use" {
		t.Errorf("result.StopReason = %q, want 'tool_use'", result.StopReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("len(result.ToolCalls) = %d, want 1", len(result.ToolCalls))
	}
}
