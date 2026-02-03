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
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

func TestOutputSanitizer_StripToolCall(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "strip tool_call tags",
			content: "Here is the answer. <tool_call>{\"name\": \"read_file\"}</tool_call> More text.",
			want:    "Here is the answer.  More text.",
		},
		{
			name:    "strip execute tags",
			content: "Let me check. <execute><command>ls -la</command></execute> Done.",
			want:    "Let me check.  Done.",
		},
		{
			name:    "strip think tags",
			content: "The answer is 42. <think>Let me verify this...</think> Yes, 42.",
			want:    "The answer is 42.  Yes, 42.",
		},
		{
			name:    "strip thought tags",
			content: "Starting analysis. <thought>Processing data...</thought> Complete.",
			want:    "Starting analysis.  Complete.",
		},
		{
			name:    "strip reasoning tags",
			content: "Result: <reasoning>Step 1, Step 2...</reasoning> Final answer.",
			want:    "Result:  Final answer.",
		},
		{
			name:    "strip reflection tags",
			content: "Check: <reflection>Was this correct?</reflection> Yes.",
			want:    "Check:  Yes.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tc.content)
			// Normalize whitespace for comparison
			got := strings.TrimSpace(result.Content)
			want := strings.TrimSpace(tc.want)
			if got != want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.content, got, want)
			}
			if !result.Stripped {
				t.Error("Expected Stripped = true")
			}
		})
	}
}

func TestOutputSanitizer_PreserveCodeBlocks(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	content := "Here's code:\n```go\n<tool_call>This is inside code block</tool_call>\n```\nOutside: <think>strip me</think>"

	result := sanitizer.Sanitize(content)

	// Code block content should be preserved
	if !strings.Contains(result.Content, "<tool_call>This is inside code block</tool_call>") {
		t.Error("Code block content should be preserved")
	}

	// Outside content should be stripped
	if strings.Contains(result.Content, "<think>strip me</think>") {
		t.Error("Think tags outside code block should be stripped")
	}
}

func TestOutputSanitizer_PreserveInlineCode(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	content := "Use `<tool_call>` syntax. <think>internal</think> Done."

	result := sanitizer.Sanitize(content)

	// Inline code should be preserved
	if !strings.Contains(result.Content, "`<tool_call>`") {
		t.Error("Inline code should be preserved")
	}

	// Think tags outside should be stripped
	if strings.Contains(result.Content, "<think>internal</think>") {
		t.Error("Think tags outside inline code should be stripped")
	}
}

func TestOutputSanitizer_NoXMLEarlyExit(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	content := "This is plain text with no XML tags at all."

	result := sanitizer.Sanitize(content)

	if result.Content != content {
		t.Errorf("Plain text should pass through unchanged, got %q", result.Content)
	}
	if result.Stripped {
		t.Error("Plain text should not have Stripped = true")
	}
}

func TestOutputSanitizer_ModelAware_Claude(t *testing.T) {
	config := DefaultSanitizeConfig()
	config.Model = agent.ModelClaude
	sanitizer := NewOutputSanitizer(config)

	// Claude uses function_calls natively, should preserve for Claude
	// Test that think tags are still stripped for Claude
	content := "Answer here. " + "<" + "think>internal<" + "/think> Done."

	result := sanitizer.Sanitize(content)

	// Think tags should still be stripped for Claude
	if strings.Contains(result.Content, "internal") {
		t.Error("Think tags should be stripped even for Claude")
	}
}

func TestOutputSanitizer_ModelAware_Generic(t *testing.T) {
	config := DefaultSanitizeConfig()
	config.Model = agent.ModelGeneric
	sanitizer := NewOutputSanitizer(config)

	// Generic model should strip function_calls
	content := "Text " + "<" + "function_calls>test<" + "/function_calls> more."

	result := sanitizer.Sanitize(content)

	if strings.Contains(result.Content, "function_calls") {
		t.Error("function_calls should be stripped for generic model")
	}
}

func TestOutputSanitizer_MultipleTagsStripped(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	content := "Start " + "<" + "think>thought1<" + "/think> middle " + "<" + "reasoning>reason<" + "/reasoning> end."

	result := sanitizer.Sanitize(content)

	if strings.Contains(result.Content, "thought1") {
		t.Error("First think tag content should be stripped")
	}
	if strings.Contains(result.Content, "reason") {
		t.Error("Reasoning tag content should be stripped")
	}
	if result.StrippedCount < 1 {
		t.Error("StrippedCount should reflect multiple tags stripped")
	}
}

func TestOutputSanitizer_WhitespaceCleanup(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	// Content with tags that will be stripped, leaving multiple newlines
	content := "Line1\n\n\n" + "<" + "think>internal<" + "/think>\n\nLine2"

	result := sanitizer.Sanitize(content)

	// Should collapse multiple newlines to at most two after stripping
	if strings.Contains(result.Content, "\n\n\n") {
		t.Error("Should collapse multiple newlines")
	}
}

func TestOutputSanitizer_SanitizeString(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	content := "Text " + "<" + "think>internal<" + "/think> more."

	result := sanitizer.SanitizeString(content)

	if strings.Contains(result, "internal") {
		t.Error("SanitizeString should strip tags")
	}
}

func TestOutputSanitizer_ContainsLeakedMarkup(t *testing.T) {
	config := DefaultSanitizeConfig()
	sanitizer := NewOutputSanitizer(config)

	tests := []struct {
		content string
		want    bool
	}{
		{"Plain text", false},
		{"Text " + "<" + "think>internal<" + "/think>", true},
		{"Text " + "<" + "tool_call>call<" + "/tool_call>", true},
		{"Text with `code`", false},
	}

	for _, tc := range tests {
		got := sanitizer.ContainsLeakedMarkup(tc.content)
		if got != tc.want {
			t.Errorf("ContainsLeakedMarkup(%q) = %v, want %v", tc.content, got, tc.want)
		}
	}
}

func TestOutputSanitizer_GetModel(t *testing.T) {
	tests := []agent.ModelType{agent.ModelClaude, agent.ModelGLM4, agent.ModelGPT4, agent.ModelGeneric}

	for _, model := range tests {
		config := DefaultSanitizeConfig()
		config.Model = model
		sanitizer := NewOutputSanitizer(config)

		if sanitizer.GetModel() != model {
			t.Errorf("GetModel() = %s, want %s", sanitizer.GetModel(), model)
		}
	}
}

func TestQuickSanitize(t *testing.T) {
	content := "Text " + "<" + "think>internal<" + "/think> more."

	result := QuickSanitize(content)

	if strings.Contains(result, "internal") {
		t.Error("QuickSanitize should strip tags")
	}
}

func TestQuickSanitizeForModel(t *testing.T) {
	content := "Text " + "<" + "think>internal<" + "/think> more."

	result := QuickSanitizeForModel(content, agent.ModelClaude)

	if strings.Contains(result, "internal") {
		t.Error("QuickSanitizeForModel should strip tags")
	}
}

func TestDefaultSanitizeConfig(t *testing.T) {
	config := DefaultSanitizeConfig()

	if config.Model != agent.ModelGeneric {
		t.Errorf("Default Model = %s, want generic", config.Model)
	}
	if !config.PreserveCodeBlocks {
		t.Error("PreserveCodeBlocks should be true by default")
	}
	if !config.PreserveInlineCode {
		t.Error("PreserveInlineCode should be true by default")
	}
	if !config.StripThinkTags {
		t.Error("StripThinkTags should be true by default")
	}
	if !config.StripReasoningTags {
		t.Error("StripReasoningTags should be true by default")
	}
}
