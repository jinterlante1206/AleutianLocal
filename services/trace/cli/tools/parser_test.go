// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"encoding/json"
	"testing"
)

func TestParser_ParseXML(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantCalls     int
		wantName      string
		wantRemaining string
	}{
		{
			name: "simple tool call",
			input: `Let me read that file.
<tool_call>
<name>read_file</name>
<params>{"path": "/foo/bar.go"}</params>
</tool_call>`,
			wantCalls:     1,
			wantName:      "read_file",
			wantRemaining: "Let me read that file.",
		},
		{
			name: "multiple tool calls",
			input: `First I'll search, then read.
<tool_call>
<name>glob</name>
<params>{"pattern": "*.go"}</params>
</tool_call>
And then:
<tool_call>
<name>read_file</name>
<params>{"path": "main.go"}</params>
</tool_call>`,
			wantCalls:     2,
			wantName:      "glob",
			wantRemaining: "First I'll search, then read.\n\nAnd then:",
		},
		{
			name:          "no tool calls",
			input:         "Just some text without any tool calls.",
			wantCalls:     0,
			wantRemaining: "Just some text without any tool calls.",
		},
		{
			name: "empty params",
			input: `<tool_call>
<name>list_files</name>
<params>{}</params>
</tool_call>`,
			wantCalls: 1,
			wantName:  "list_files",
		},
		{
			name:      "empty input",
			input:     "",
			wantCalls: 0,
		},
		{
			name: "tool call with whitespace",
			input: `<tool_call>
  <name>  read_file  </name>
  <params>  {"path": "foo.go"}  </params>
</tool_call>`,
			wantCalls: 1,
			wantName:  "read_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(FormatXML)
			calls, remaining, err := p.Parse(tt.input)

			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}

			if len(calls) != tt.wantCalls {
				t.Errorf("Parse() got %d calls, want %d", len(calls), tt.wantCalls)
				return
			}

			if tt.wantCalls > 0 && calls[0].Name != tt.wantName {
				t.Errorf("Parse() first call name = %q, want %q", calls[0].Name, tt.wantName)
			}

			if tt.wantRemaining != "" && remaining != tt.wantRemaining {
				t.Errorf("Parse() remaining = %q, want %q", remaining, tt.wantRemaining)
			}
		})
	}
}

func TestParser_ParseJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCalls int
		wantName  string
	}{
		{
			name:      "simple JSON tool call",
			input:     `Here's the tool call: {"tool": "read_file", "params": {"path": "/foo/bar.go"}}`,
			wantCalls: 1,
			wantName:  "read_file",
		},
		{
			name:      "multiple JSON tool calls",
			input:     `{"tool": "glob", "params": {"pattern": "*.go"}} and {"tool": "read_file", "params": {"path": "main.go"}}`,
			wantCalls: 2,
			wantName:  "glob",
		},
		{
			name:      "no JSON tool calls",
			input:     `Just some JSON that doesn't match: {"foo": "bar"}`,
			wantCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(FormatJSON)
			calls, _, err := p.Parse(tt.input)

			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}

			if len(calls) != tt.wantCalls {
				t.Errorf("Parse() got %d calls, want %d", len(calls), tt.wantCalls)
				return
			}

			if tt.wantCalls > 0 && calls[0].Name != tt.wantName {
				t.Errorf("Parse() first call name = %q, want %q", calls[0].Name, tt.wantName)
			}
		})
	}
}

func TestParser_ParseAnthropicXML(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCalls  int
		wantName   string
		wantParams map[string]any
	}{
		{
			name: "simple Anthropic XML",
			input: `Let me search for that.
<function_calls>
<invoke name="find_files">
<parameter name="pattern">*.go</parameter>
<parameter name="recursive">true</parameter>
</invoke>
</function_calls>`,
			wantCalls: 1,
			wantName:  "find_files",
			wantParams: map[string]any{
				"pattern":   "*.go",
				"recursive": true,
			},
		},
		{
			name: "multiple invokes in one block",
			input: `<function_calls>
<invoke name="glob">
<parameter name="pattern">*.go</parameter>
</invoke>
<invoke name="read_file">
<parameter name="path">main.go</parameter>
</invoke>
</function_calls>`,
			wantCalls: 2,
			wantName:  "glob",
		},
		{
			name: "numeric parameter",
			input: `<function_calls>
<invoke name="search">
<parameter name="limit">10</parameter>
</invoke>
</function_calls>`,
			wantCalls: 1,
			wantName:  "search",
			wantParams: map[string]any{
				"limit": float64(10),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(FormatAnthropicXML)
			calls, _, err := p.Parse(tt.input)

			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}

			if len(calls) != tt.wantCalls {
				t.Errorf("Parse() got %d calls, want %d", len(calls), tt.wantCalls)
				return
			}

			if tt.wantCalls > 0 {
				if calls[0].Name != tt.wantName {
					t.Errorf("Parse() first call name = %q, want %q", calls[0].Name, tt.wantName)
				}

				if tt.wantParams != nil {
					params, err := calls[0].ParamsMap()
					if err != nil {
						t.Errorf("ParamsMap() error = %v", err)
						return
					}
					for k, want := range tt.wantParams {
						got, ok := params[k]
						if !ok {
							t.Errorf("Parse() missing param %q", k)
							continue
						}
						if got != want {
							t.Errorf("Parse() param %q = %v, want %v", k, got, want)
						}
					}
				}
			}
		})
	}
}

func TestParser_ParseFunctionCalls(t *testing.T) {
	tests := []struct {
		name      string
		input     []FunctionCallResponse
		wantCalls int
		wantName  string
		wantErr   bool
	}{
		{
			name: "single function call",
			input: []FunctionCallResponse{
				{
					ID:   "call_123",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "read_file",
						Arguments: `{"path": "/foo/bar.go"}`,
					},
				},
			},
			wantCalls: 1,
			wantName:  "read_file",
		},
		{
			name: "multiple function calls",
			input: []FunctionCallResponse{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "glob", Arguments: `{"pattern": "*.go"}`},
				},
				{
					ID:   "call_2",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "read_file", Arguments: `{"path": "main.go"}`},
				},
			},
			wantCalls: 2,
			wantName:  "glob",
		},
		{
			name:      "empty input",
			input:     []FunctionCallResponse{},
			wantCalls: 0,
		},
		{
			name: "invalid arguments JSON",
			input: []FunctionCallResponse{
				{
					ID:   "call_bad",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "test", Arguments: `{invalid json`},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			calls, err := p.ParseFunctionCalls(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFunctionCalls() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(calls) != tt.wantCalls {
				t.Errorf("ParseFunctionCalls() got %d calls, want %d", len(calls), tt.wantCalls)
				return
			}

			if tt.wantCalls > 0 && calls[0].Name != tt.wantName {
				t.Errorf("ParseFunctionCalls() first call name = %q, want %q", calls[0].Name, tt.wantName)
			}
		})
	}
}

func TestParser_MixedFormats(t *testing.T) {
	// Test that the default parser can handle multiple formats
	input := `Let me help you with that.
<tool_call>
<name>read_file</name>
<params>{"path": "main.go"}</params>
</tool_call>
Done!`

	p := NewParser() // Default parser tries all formats
	calls, remaining, err := p.Parse(input)

	if err != nil {
		t.Errorf("Parse() error = %v", err)
		return
	}

	if len(calls) != 1 {
		t.Errorf("Parse() got %d calls, want 1", len(calls))
		return
	}

	if calls[0].Name != "read_file" {
		t.Errorf("Parse() call name = %q, want %q", calls[0].Name, "read_file")
	}

	if remaining != "Let me help you with that.\n\nDone!" {
		t.Errorf("Parse() remaining = %q", remaining)
	}
}

func TestToolCall_ParamsMap(t *testing.T) {
	tests := []struct {
		name    string
		params  json.RawMessage
		want    map[string]any
		wantErr bool
	}{
		{
			name:   "valid params",
			params: json.RawMessage(`{"key": "value", "num": 42}`),
			want: map[string]any{
				"key": "value",
				"num": float64(42),
			},
		},
		{
			name:   "empty params",
			params: json.RawMessage{},
			want:   map[string]any{},
		},
		{
			name:    "invalid JSON",
			params:  json.RawMessage(`{invalid}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := ToolCall{Params: tt.params}
			got, err := tc.ParamsMap()

			if (err != nil) != tt.wantErr {
				t.Errorf("ParamsMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ParamsMap() got %d params, want %d", len(got), len(tt.want))
			}

			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("ParamsMap() [%s] = %v, want %v", k, got[k], want)
				}
			}
		})
	}
}

func TestParser_ExtractTextBetweenToolCalls(t *testing.T) {
	input := `First some text.
<tool_call>
<name>test</name>
<params>{}</params>
</tool_call>
Middle text.
<tool_call>
<name>test2</name>
<params>{}</params>
</tool_call>
Final text.`

	p := NewParser()
	text := p.ExtractTextBetweenToolCalls(input)

	if text != "First some text.\n\nMiddle text.\n\nFinal text." {
		t.Errorf("ExtractTextBetweenToolCalls() = %q", text)
	}
}

func TestParser_MalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unclosed tag",
			input: "<tool_call><name>test</name>",
		},
		{
			name:  "missing name",
			input: "<tool_call><params>{}</params></tool_call>",
		},
		{
			name:  "invalid nested tags",
			input: "<tool_call><name>test<foo>bar</foo></name></tool_call>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			calls, _, err := p.Parse(tt.input)

			// Should not error but may return no calls
			if err != nil {
				t.Errorf("Parse() error = %v, want nil (graceful handling)", err)
			}

			// Malformed input should result in no calls
			if len(calls) > 0 {
				t.Logf("Parse() found %d calls (may be partial match)", len(calls))
			}
		})
	}
}
