// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package llm

import (
	"strings"
	"testing"
)

func TestParseReAct_BasicAction(t *testing.T) {
	input := `Thought: I need to find what tests exist in this project.
Action: find_entry_points
Action Input: {"type": "test"}`

	result := ParseReAct(input)

	if result.Thought == "" {
		t.Error("Expected thought to be parsed")
	}
	if result.Action != "find_entry_points" {
		t.Errorf("Action = %q, want %q", result.Action, "find_entry_points")
	}
	if result.ActionInput != `{"type": "test"}` {
		t.Errorf("ActionInput = %q, want %q", result.ActionInput, `{"type": "test"}`)
	}
	if result.FinalAnswer != "" {
		t.Error("FinalAnswer should be empty for action response")
	}
}

func TestParseReAct_FinalAnswer(t *testing.T) {
	input := `Thought: I now have enough information to answer the question.
Final Answer: The tests are located in the pkg/calcs/ directory. I found unit tests for calculator operations.`

	result := ParseReAct(input)

	if result.Thought == "" {
		t.Error("Expected thought to be parsed")
	}
	if result.Action != "" {
		t.Error("Action should be empty for final answer")
	}
	if result.FinalAnswer == "" {
		t.Error("Expected FinalAnswer to be parsed")
	}
	if !strings.Contains(result.FinalAnswer, "pkg/calcs/") {
		t.Error("FinalAnswer should contain the directory path")
	}
}

func TestParseReAct_VariousFormats(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedAction string
		expectedInput  string
	}{
		{
			name:           "standard format",
			input:          "Thought: I need to find tests.\nAction: find_entry_points\nAction Input: {\"type\": \"test\"}",
			expectedAction: "find_entry_points",
			expectedInput:  `{"type": "test"}`,
		},
		{
			name:           "extra whitespace",
			input:          "Thought:   I need to find tests.  \nAction:   find_entry_points  \nAction Input:  {\"type\": \"test\"}",
			expectedAction: "find_entry_points",
			expectedInput:  `{"type": "test"}`,
		},
		{
			name:           "lowercase",
			input:          "thought: thinking...\naction: trace_data_flow\naction input: {}",
			expectedAction: "trace_data_flow",
			expectedInput:  "{}",
		},
		{
			name:           "mixed case",
			input:          "THOUGHT: Thinking...\nACTION: trace_data_flow\nACTION INPUT: {}",
			expectedAction: "trace_data_flow",
			expectedInput:  "{}",
		},
		{
			name:           "with preamble text",
			input:          "Let me help you with that.\nThought: First, I'll explore.\nAction: trace_data_flow\nAction Input: {}",
			expectedAction: "trace_data_flow",
			expectedInput:  "{}",
		},
		{
			name:           "multiline JSON",
			input:          "Thought: I need details.\nAction: find_entry_points\nAction Input: {\n  \"type\": \"test\",\n  \"limit\": 10\n}",
			expectedAction: "find_entry_points",
			expectedInput:  "{\n  \"type\": \"test\",\n  \"limit\": 10\n}",
		},
		{
			name:           "no thought",
			input:          "Action: find_entry_points\nAction Input: {}",
			expectedAction: "find_entry_points",
			expectedInput:  "{}",
		},
		{
			name:           "empty input",
			input:          "",
			expectedAction: "",
			expectedInput:  "",
		},
		{
			name:           "just text - no react format",
			input:          "Here's my analysis of the code...",
			expectedAction: "",
			expectedInput:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseReAct(tt.input)
			if result.Action != tt.expectedAction {
				t.Errorf("Action = %q, want %q", result.Action, tt.expectedAction)
			}
			if result.ActionInput != tt.expectedInput {
				t.Errorf("ActionInput = %q, want %q", result.ActionInput, tt.expectedInput)
			}
		})
	}
}

func TestParseReAct_HasAction(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Action: some_tool", true},
		{"Thought: thinking\nAction: some_tool", true},
		{"Thought: thinking", false},
		{"Final Answer: done", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			result := ParseReAct(tt.input)
			if result.HasAction() != tt.expected {
				t.Errorf("HasAction() = %v, want %v", result.HasAction(), tt.expected)
			}
		})
	}
}

func TestParseReAct_HasFinalAnswer(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Final Answer: here's my answer", true},
		{"Thought: done\nFinal Answer: here's my answer", true},
		{"Action: some_tool", false},
		{"Thought: thinking", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			result := ParseReAct(tt.input)
			if result.HasFinalAnswer() != tt.expected {
				t.Errorf("HasFinalAnswer() = %v, want %v", result.HasFinalAnswer(), tt.expected)
			}
		})
	}
}

func TestParseReAct_ToToolCall(t *testing.T) {
	t.Run("with action", func(t *testing.T) {
		result := ParseReAct("Action: find_entry_points\nAction Input: {\"type\": \"test\"}")
		call := result.ToToolCall()

		if call == nil {
			t.Fatal("ToToolCall() returned nil for action response")
		}
		if call.Name != "find_entry_points" {
			t.Errorf("Name = %q, want %q", call.Name, "find_entry_points")
		}
		if call.Arguments != `{"type": "test"}` {
			t.Errorf("Arguments = %q, want %q", call.Arguments, `{"type": "test"}`)
		}
		if !strings.HasPrefix(call.ID, "react-") {
			t.Errorf("ID should start with 'react-', got %q", call.ID)
		}
	})

	t.Run("without action", func(t *testing.T) {
		result := ParseReAct("Final Answer: done")
		call := result.ToToolCall()

		if call != nil {
			t.Error("ToToolCall() should return nil for non-action response")
		}
	})

	t.Run("with action but no input", func(t *testing.T) {
		result := ParseReAct("Action: some_tool")
		call := result.ToToolCall()

		if call == nil {
			t.Fatal("ToToolCall() returned nil")
		}
		if call.Arguments != "{}" {
			t.Errorf("Arguments should default to {}, got %q", call.Arguments)
		}
	})
}

func TestReActInstructions(t *testing.T) {
	instructions := ReActInstructions()

	if !strings.Contains(instructions, "TOOL USAGE FORMAT") {
		t.Error("Instructions should contain header")
	}
	if !strings.Contains(instructions, "Thought:") {
		t.Error("Instructions should mention Thought format")
	}
	if !strings.Contains(instructions, "Action:") {
		t.Error("Instructions should mention Action format")
	}
	if !strings.Contains(instructions, "Action Input:") {
		t.Error("Instructions should mention Action Input format")
	}
	if !strings.Contains(instructions, "Observation") {
		t.Error("Instructions should mention Observation")
	}
	if !strings.Contains(instructions, "Final Answer:") {
		t.Error("Instructions should mention Final Answer format")
	}
}

func TestFormatAsObservation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		obs := FormatAsObservation("find_entry_points", true, "Found 5 entry points")
		if obs != "Observation: Found 5 entry points" {
			t.Errorf("Unexpected observation: %q", obs)
		}
	})

	t.Run("error", func(t *testing.T) {
		obs := FormatAsObservation("find_entry_points", false, "timeout")
		if !strings.Contains(obs, "Error") {
			t.Error("Error observation should contain 'Error'")
		}
		if !strings.Contains(obs, "find_entry_points") {
			t.Error("Error observation should contain tool name")
		}
		if !strings.Contains(obs, "timeout") {
			t.Error("Error observation should contain error message")
		}
	})
}

func BenchmarkParseReAct(b *testing.B) {
	input := `Thought: I need to analyze the authentication system to understand how data flows.
Action: trace_data_flow
Action Input: {"source_id": "auth.login", "max_depth": 5}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseReAct(input)
	}
}

func TestParseToolCallsWithReAct_NativeFirst(t *testing.T) {
	response := &Response{
		Content: "Thought: I'll use find_entry_points\nAction: ignored_tool",
		ToolCalls: []ToolCall{
			{ID: "call-1", Name: "find_entry_points", Arguments: `{"type": "test"}`},
		},
	}

	invocations, usedReAct := ParseToolCallsWithReAct(response)

	if usedReAct {
		t.Error("Should use native tool calls when available, not ReAct")
	}
	if len(invocations) != 1 {
		t.Fatalf("Expected 1 invocation, got %d", len(invocations))
	}
	if invocations[0].Tool != "find_entry_points" {
		t.Error("Should parse native tool call, not ReAct")
	}
}

func TestParseToolCallsWithReAct_ReActFallback(t *testing.T) {
	response := &Response{
		Content: `Thought: I need to find the test files.
Action: find_entry_points
Action Input: {"type": "test"}`,
		ToolCalls: nil, // No native tool calls
	}

	invocations, usedReAct := ParseToolCallsWithReAct(response)

	if !usedReAct {
		t.Error("Should use ReAct fallback when no native tool calls")
	}
	if len(invocations) != 1 {
		t.Fatalf("Expected 1 invocation from ReAct, got %d", len(invocations))
	}
	if invocations[0].Tool != "find_entry_points" {
		t.Errorf("Tool = %q, want %q", invocations[0].Tool, "find_entry_points")
	}
	if !strings.HasPrefix(invocations[0].ID, "react-") {
		t.Error("ReAct-parsed invocations should have react- prefix")
	}
}

func TestParseToolCallsWithReAct_NoToolCalls(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		invocations, usedReAct := ParseToolCallsWithReAct(nil)
		if invocations != nil || usedReAct {
			t.Error("Nil response should return nil, false")
		}
	})

	t.Run("empty content no tools", func(t *testing.T) {
		response := &Response{Content: ""}
		invocations, usedReAct := ParseToolCallsWithReAct(response)
		if invocations != nil || usedReAct {
			t.Error("Empty content should return nil, false")
		}
	})

	t.Run("text without ReAct format", func(t *testing.T) {
		response := &Response{Content: "Here is my analysis of the code..."}
		invocations, usedReAct := ParseToolCallsWithReAct(response)
		if invocations != nil || usedReAct {
			t.Error("Non-ReAct text should return nil, false")
		}
	})

	t.Run("final answer only", func(t *testing.T) {
		response := &Response{Content: "Thought: Done.\nFinal Answer: The tests are in pkg/"}
		invocations, usedReAct := ParseToolCallsWithReAct(response)
		if invocations != nil || usedReAct {
			t.Error("Final answer without action should return nil, false")
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
