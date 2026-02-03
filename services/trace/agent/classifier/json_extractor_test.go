// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"encoding/json"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantField string
		wantValue any
	}{
		{
			name:      "clean JSON",
			input:     `{"is_analytical":true,"tool":"find_entry_points"}`,
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "JSON with whitespace",
			input:     `   {"is_analytical":false}   `,
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: false,
		},
		{
			name:      "markdown JSON block",
			input:     "```json\n{\"is_analytical\":true}\n```",
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "generic code block",
			input:     "```\n{\"is_analytical\":true}\n```",
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "JSON with preamble",
			input:     "Here is my analysis:\n{\"is_analytical\":true}",
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "JSON with postamble",
			input:     "{\"is_analytical\":true}\nHope this helps!",
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "nested braces in string",
			input:     `{"reasoning":"something {with} braces","is_analytical":true}`,
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "escaped quotes in string",
			input:     `{"reasoning":"he said \"hello\"","is_analytical":true}`,
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   \t\n  ",
			wantErr: true,
		},
		{
			name:    "no JSON",
			input:   "This is just plain text without any JSON",
			wantErr: true,
		},
		{
			name:    "malformed JSON",
			input:   "{is_analytical: true}",
			wantErr: true,
		},
		{
			name:    "incomplete JSON",
			input:   "{\"is_analytical\":true",
			wantErr: true,
		},
		{
			name:      "multiple JSON objects - first valid taken",
			input:     `{"first":1} {"second":2}`,
			wantErr:   false,
			wantField: "first",
			wantValue: float64(1),
		},
		{
			name:      "JSON in code block with language",
			input:     "```JSON\n{\"is_analytical\":true}\n```",
			wantErr:   false,
			wantField: "is_analytical",
			wantValue: true,
		},
		{
			name:      "deeply nested object",
			input:     `{"outer":{"inner":{"is_analytical":true}}}`,
			wantErr:   false,
			wantField: "outer",
			wantValue: map[string]any{"inner": map[string]any{"is_analytical": true}},
		},
		{
			name:      "array in JSON",
			input:     `{"patterns":["a","b"],"is_analytical":true}`,
			wantErr:   false,
			wantField: "patterns",
			wantValue: []any{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var parsed map[string]any
			if err := json.Unmarshal(result, &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}

			if tt.wantField != "" {
				val, exists := parsed[tt.wantField]
				if !exists {
					t.Errorf("expected field %q not found", tt.wantField)
				}

				// Compare value types
				switch expected := tt.wantValue.(type) {
				case bool:
					if val != expected {
						t.Errorf("expected %v, got %v", expected, val)
					}
				case float64:
					if val != expected {
						t.Errorf("expected %v, got %v", expected, val)
					}
				case []any:
					gotArr, ok := val.([]any)
					if !ok {
						t.Errorf("expected array, got %T", val)
					}
					if len(gotArr) != len(expected) {
						t.Errorf("expected %d elements, got %d", len(expected), len(gotArr))
					}
				case map[string]any:
					// Just check it's a map
					if _, ok := val.(map[string]any); !ok {
						t.Errorf("expected map, got %T", val)
					}
				}
			}
		})
	}
}

func TestExtractJSONWithFallback(t *testing.T) {
	t.Run("successful extraction", func(t *testing.T) {
		input := `{"is_analytical":true,"tool":"find_entry_points"}`
		result, ok := ExtractJSONWithFallback(input)
		if !ok {
			t.Error("expected successful extraction")
		}
		if !result.IsAnalytical {
			t.Error("expected IsAnalytical=true")
		}
		if result.Tool != "find_entry_points" {
			t.Errorf("expected tool=find_entry_points, got %s", result.Tool)
		}
	})

	t.Run("failed extraction returns fallback", func(t *testing.T) {
		input := "not valid json"
		result, ok := ExtractJSONWithFallback(input)
		if ok {
			t.Error("expected failed extraction")
		}
		if result == nil {
			t.Fatal("expected non-nil fallback result")
		}
		if result.IsAnalytical {
			t.Error("fallback should be non-analytical")
		}
		if result.Reasoning == "" {
			t.Error("fallback should have reasoning")
		}
	})

	t.Run("invalid JSON structure returns fallback", func(t *testing.T) {
		input := `{"is_analytical":"not a boolean"}`
		result, ok := ExtractJSONWithFallback(input)
		if ok {
			t.Error("expected failed extraction for wrong type")
		}
		if result.IsAnalytical {
			t.Error("fallback should be non-analytical")
		}
	})
}

func TestParseClassificationResponse(t *testing.T) {
	t.Run("full response", func(t *testing.T) {
		input := `{"is_analytical":true,"tool":"find_entry_points","parameters":{"type":"test"},"search_patterns":["*_test.go"],"reasoning":"test query","confidence":0.95}`

		result, err := ParseClassificationResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.IsAnalytical {
			t.Error("expected IsAnalytical=true")
		}
		if result.Tool != "find_entry_points" {
			t.Errorf("expected tool=find_entry_points, got %s", result.Tool)
		}
		if result.Confidence != 0.95 {
			t.Errorf("expected confidence=0.95, got %f", result.Confidence)
		}
		if len(result.SearchPatterns) != 1 {
			t.Errorf("expected 1 search pattern, got %d", len(result.SearchPatterns))
		}
		if result.Reasoning != "test query" {
			t.Errorf("expected reasoning='test query', got %s", result.Reasoning)
		}
	})

	t.Run("minimal response", func(t *testing.T) {
		input := `{"is_analytical":false}`

		result, err := ParseClassificationResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.IsAnalytical {
			t.Error("expected IsAnalytical=false")
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		input := "not json"

		_, err := ParseClassificationResponse(input)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("markdown wrapped", func(t *testing.T) {
		input := "```json\n{\"is_analytical\":true,\"confidence\":0.8}\n```"

		result, err := ParseClassificationResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.IsAnalytical {
			t.Error("expected IsAnalytical=true")
		}
	})
}
