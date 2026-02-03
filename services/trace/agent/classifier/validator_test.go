// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
)

func TestResponseValidator_ProhibitedPatterns(t *testing.T) {
	validator := NewResponseValidator()

	prohibitedResponses := []string{
		"I'm ready to help you analyze this codebase!",
		"I am ready to help with your question.",
		"I'd be happy to help you explore the code.",
		"What would you like me to investigate?",
		"How can I assist you today?",
		"Let me know if you have any questions.",
		"I can help you with analyzing this project.",
		"I'm here to help! What do you need?",
		"Would you like me to find the tests?",
		"Do you want me to trace the data flow?",
		"Hello! I'm an AI assistant...",
		// Lazy Agent patterns (asking for clarification instead of investigating)
		"Could you please specify what aspect of this codebase you'd like me to analyze?",
		"Could you specify which component you're interested in?",
		"Please let me know what specific analysis you're looking for.",
		"Are you looking for potential improvements or issues?",
		"Are you interested in a specific component?",
		"Do you need help understanding the data flow?",
		"What specific analysis you're looking for?",
	}

	for _, resp := range prohibitedResponses {
		t.Run(resp[:min(30, len(resp))], func(t *testing.T) {
			response := &llm.Response{Content: resp}
			result := validator.Validate(response, nil)
			if result.Valid {
				t.Errorf("Should reject prohibited pattern: %q", resp)
			}
			if !result.Retryable {
				t.Error("Prohibited pattern failures should be retryable")
			}
		})
	}
}

func TestResponseValidator_ValidResponses(t *testing.T) {
	validator := NewResponseValidator()

	validResponses := []struct {
		name    string
		content string
		tools   []llm.ToolCall
	}{
		{
			name:    "tool call with analysis",
			content: "Based on the tool results, the main entry point is in cmd/main.go:15",
			tools: []llm.ToolCall{
				{ID: "1", Name: "find_entry_points", Arguments: "{}"},
			},
		},
		{
			name:    "tool call only",
			content: "",
			tools: []llm.ToolCall{
				{ID: "1", Name: "trace_data_flow", Arguments: "{}"},
			},
		},
		{
			name:    "analytical response without tools",
			content: "The project has 3 packages: api, service, and model.",
			tools:   nil,
		},
		{
			name:    "code reference",
			content: "The authentication is handled in [auth/handler.go:42]",
			tools:   nil,
		},
	}

	for _, tt := range validResponses {
		t.Run(tt.name, func(t *testing.T) {
			response := &llm.Response{
				Content:   tt.content,
				ToolCalls: tt.tools,
			}
			result := validator.Validate(response, nil)
			if !result.Valid {
				t.Errorf("Should accept valid response: %s (reason: %s)", tt.name, result.Reason)
			}
		})
	}
}

func TestResponseValidator_ToolChoiceEnforcement(t *testing.T) {
	validator := NewResponseValidator()

	t.Run("tool_choice any requires tools", func(t *testing.T) {
		response := &llm.Response{
			Content:   "Here's my analysis without calling any tools.",
			ToolCalls: nil,
		}
		result := validator.Validate(response, llm.ToolChoiceAny())
		if result.Valid {
			t.Error("Should reject response without tools when tool_choice is 'any'")
		}
	})

	t.Run("tool_choice any accepts tools", func(t *testing.T) {
		response := &llm.Response{
			Content:   "Analysis based on tool results.",
			ToolCalls: []llm.ToolCall{{ID: "1", Name: "find_entry_points", Arguments: "{}"}},
		}
		result := validator.Validate(response, llm.ToolChoiceAny())
		if !result.Valid {
			t.Errorf("Should accept response with tools when tool_choice is 'any' (reason: %s)", result.Reason)
		}
	})

	t.Run("tool_choice tool requires specific tool", func(t *testing.T) {
		response := &llm.Response{
			Content:   "Analysis",
			ToolCalls: []llm.ToolCall{{ID: "1", Name: "trace_data_flow", Arguments: "{}"}},
		}
		result := validator.Validate(response, llm.ToolChoiceRequired("find_entry_points"))
		if result.Valid {
			t.Error("Should reject response with wrong tool")
		}
	})

	t.Run("tool_choice tool accepts correct tool", func(t *testing.T) {
		response := &llm.Response{
			Content:   "Analysis",
			ToolCalls: []llm.ToolCall{{ID: "1", Name: "find_entry_points", Arguments: "{}"}},
		}
		result := validator.Validate(response, llm.ToolChoiceRequired("find_entry_points"))
		if !result.Valid {
			t.Errorf("Should accept response with correct tool (reason: %s)", result.Reason)
		}
	})

	t.Run("tool_choice auto allows no tools", func(t *testing.T) {
		response := &llm.Response{
			Content:   "The code looks well structured.",
			ToolCalls: nil,
		}
		result := validator.Validate(response, llm.ToolChoiceAuto())
		if !result.Valid {
			t.Error("Should accept response without tools when tool_choice is 'auto'")
		}
	})
}

func TestResponseValidator_NilResponse(t *testing.T) {
	validator := NewResponseValidator()

	result := validator.Validate(nil, nil)
	if result.Valid {
		t.Error("Should reject nil response")
	}
	if !result.Retryable {
		t.Error("Nil response should be retryable")
	}
}

func TestResponseValidator_EmptyResponse(t *testing.T) {
	validator := NewResponseValidator()

	response := &llm.Response{
		Content:   "",
		ToolCalls: nil,
	}
	result := validator.Validate(response, nil)
	if result.Valid {
		t.Error("Should reject empty response")
	}
}

func TestSuggestRetryToolChoice(t *testing.T) {
	tests := []struct {
		name          string
		original      *llm.ToolChoice
		suggestedTool string
		expectedType  string
		expectedName  string
	}{
		{
			name:          "nil original → any",
			original:      nil,
			suggestedTool: "",
			expectedType:  "any",
		},
		{
			name:          "auto → any",
			original:      llm.ToolChoiceAuto(),
			suggestedTool: "",
			expectedType:  "any",
		},
		{
			name:          "any with suggested → tool",
			original:      llm.ToolChoiceAny(),
			suggestedTool: "find_entry_points",
			expectedType:  "tool",
			expectedName:  "find_entry_points",
		},
		{
			name:          "any without suggested → any",
			original:      llm.ToolChoiceAny(),
			suggestedTool: "",
			expectedType:  "any",
		},
		{
			name:          "tool → keep same",
			original:      llm.ToolChoiceRequired("trace_data_flow"),
			suggestedTool: "find_entry_points",
			expectedType:  "tool",
			expectedName:  "trace_data_flow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuggestRetryToolChoice(tt.original, tt.suggestedTool)
			if result.Type != tt.expectedType {
				t.Errorf("Type = %q, want %q", result.Type, tt.expectedType)
			}
			if tt.expectedName != "" && result.Name != tt.expectedName {
				t.Errorf("Name = %q, want %q", result.Name, tt.expectedName)
			}
		})
	}
}

func TestNeedsToolCalls(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice *llm.ToolChoice
		expected   bool
	}{
		{"nil", nil, false},
		{"auto", llm.ToolChoiceAuto(), false},
		{"any", llm.ToolChoiceAny(), true},
		{"tool", llm.ToolChoiceRequired("test"), true},
		{"none", llm.ToolChoiceNone(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsToolCalls(tt.toolChoice); got != tt.expected {
				t.Errorf("NeedsToolCalls() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLooksLikeOfferToHelp(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{"I'm ready to help you analyze this codebase!", true},
		{"I'd be happy to help with that.", true},
		{"Would you like me to investigate?", true},
		{"Let me know if you have questions.", true},
		{"The main function is in cmd/main.go:15", false},
		{"Based on the analysis, there are 3 entry points.", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.content[:min(30, len(tt.content))], func(t *testing.T) {
			if got := LooksLikeOfferToHelp(tt.content); got != tt.expected {
				t.Errorf("LooksLikeOfferToHelp(%q) = %v, want %v", tt.content, got, tt.expected)
			}
		})
	}
}

func TestRetryableValidator(t *testing.T) {
	validator := NewRetryableValidator(3)

	t.Run("respects max retries", func(t *testing.T) {
		if validator.MaxRetries() != 3 {
			t.Errorf("MaxRetries() = %d, want 3", validator.MaxRetries())
		}
	})

	t.Run("should retry on retryable failure", func(t *testing.T) {
		result := ValidationResult{Valid: false, Retryable: true}
		if !validator.ShouldRetry(result, 1) {
			t.Error("Should retry on attempt 1")
		}
		if !validator.ShouldRetry(result, 2) {
			t.Error("Should retry on attempt 2")
		}
		if validator.ShouldRetry(result, 3) {
			t.Error("Should not retry on attempt 3 (max reached)")
		}
	})

	t.Run("should not retry valid response", func(t *testing.T) {
		result := ValidationResult{Valid: true}
		if validator.ShouldRetry(result, 1) {
			t.Error("Should not retry valid response")
		}
	})

	t.Run("should not retry non-retryable failure", func(t *testing.T) {
		result := ValidationResult{Valid: false, Retryable: false}
		if validator.ShouldRetry(result, 1) {
			t.Error("Should not retry non-retryable failure")
		}
	})

	t.Run("retry tool choice escalates", func(t *testing.T) {
		// First retry: escalate to "any"
		choice1 := validator.GetRetryToolChoice(1, llm.ToolChoiceAuto(), "")
		if choice1.Type != "any" {
			t.Errorf("First retry should use 'any', got %q", choice1.Type)
		}

		// Second retry with suggested tool: escalate to specific tool
		choice2 := validator.GetRetryToolChoice(2, llm.ToolChoiceAny(), "find_entry_points")
		if choice2.Type != "tool" || choice2.Name != "find_entry_points" {
			t.Errorf("Second retry should force tool, got type=%q name=%q", choice2.Type, choice2.Name)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Quality Validator Tests

func TestQualityValidator_HedgingLanguage(t *testing.T) {
	validator := NewQualityValidator(nil)

	hedgingResponses := []struct {
		content string
		desc    string
	}{
		{"The system likely uses flags for configuration", "likely + uses"},
		{"It appears to load config from environment variables", "appears to"},
		{"Based on the function names, this probably handles errors", "based on function names + probably"},
		{"The function might call the database directly", "might + call"},
		{"It seems to process the input in batches", "seems to"},
		{"I think this is the main entry point", "I think + this"},
		{"The code could handle multiple requests", "could + handle"},
	}

	for _, tc := range hedgingResponses {
		t.Run(tc.desc, func(t *testing.T) {
			result := validator.ValidateQuality(tc.content, true)
			if result.Valid {
				t.Errorf("Should reject hedging: %q", tc.content)
			}
			if !result.Retryable {
				t.Error("Hedging failures should be retryable at default strictness")
			}
		})
	}
}

func TestQualityValidator_AcceptsGoodResponses(t *testing.T) {
	validator := NewQualityValidator(nil)

	goodResponses := []struct {
		content string
		desc    string
	}{
		{"The main function [cmd/main.go:15] initializes the config.", "with citation"},
		{"Flags defined in [cmd/main.go:23-38]: -project, -api-key", "with range citation"},
		{"I don't see any configuration files in the context.", "explicit not found"},
		{"Not found in the provided context.", "not found variant"},
		{"The handler (api/handler.go:42) processes requests.", "parenthesis citation"},
		{"See main.go:15 for the entry point.", "bare citation"},
	}

	for _, tc := range goodResponses {
		t.Run(tc.desc, func(t *testing.T) {
			result := validator.ValidateQuality(tc.content, true)
			if !result.Valid {
				t.Errorf("Should accept good response: %q (reason: %s)", tc.content, result.Reason)
			}
		})
	}
}

func TestQualityValidator_RequiresCitationsInLongResponses(t *testing.T) {
	validator := NewQualityValidator(nil)

	// Short response without citations should pass
	shortResponse := "The code handles requests."
	result := validator.ValidateQuality(shortResponse, true)
	if !result.Valid {
		t.Error("Should accept short response without citations")
	}

	// Long response without citations should fail
	longResponse := strings.Repeat("The function handles data processing and validation. ", 20)
	result = validator.ValidateQuality(longResponse, true)
	if result.Valid {
		t.Error("Should require citations in long analytical responses")
	}

	// Long response with citations should pass
	longWithCitations := strings.Repeat("The function handles data. ", 10) + " See [main.go:42] for details."
	result = validator.ValidateQuality(longWithCitations, true)
	if !result.Valid {
		t.Errorf("Should accept long response with citations (reason: %s)", result.Reason)
	}
}

func TestQualityValidator_NonAnalyticalQueries(t *testing.T) {
	validator := NewQualityValidator(nil)

	// Hedging in non-analytical query should be accepted
	response := "The system likely uses some configuration."
	result := validator.ValidateQuality(response, false)
	if !result.Valid {
		t.Error("Should accept hedging in non-analytical queries")
	}
}

func TestQualityValidator_StrictnessLevels(t *testing.T) {
	hedgingContent := "The system likely uses flags for configuration"

	t.Run("strictness 0 - disabled", func(t *testing.T) {
		config := DefaultQualityConfig()
		config.StrictnessLevel = 0
		validator := NewQualityValidator(&config)

		result := validator.ValidateQuality(hedgingContent, true)
		if !result.Valid {
			t.Error("Should pass when validation disabled")
		}
	})

	t.Run("strictness 1 - warnings only", func(t *testing.T) {
		config := DefaultQualityConfig()
		config.StrictnessLevel = 1
		validator := NewQualityValidator(&config)

		result := validator.ValidateQuality(hedgingContent, true)
		if !result.Valid {
			t.Error("Should mark as valid with warning at strictness 1")
		}
		if !strings.Contains(result.Reason, "warning") {
			t.Error("Should include warning in reason")
		}
	})

	t.Run("strictness 2 - soft fail", func(t *testing.T) {
		config := DefaultQualityConfig()
		config.StrictnessLevel = 2
		validator := NewQualityValidator(&config)

		result := validator.ValidateQuality(hedgingContent, true)
		if result.Valid {
			t.Error("Should fail at strictness 2")
		}
		if !result.Retryable {
			t.Error("Should be retryable at strictness 2")
		}
	})

	t.Run("strictness 3 - hard fail", func(t *testing.T) {
		config := DefaultQualityConfig()
		config.StrictnessLevel = 3
		validator := NewQualityValidator(&config)

		result := validator.ValidateQuality(hedgingContent, true)
		if result.Valid {
			t.Error("Should fail at strictness 3")
		}
		if result.Retryable {
			t.Error("Should not be retryable at strictness 3")
		}
	})
}

func TestQualityValidator_MultipleCitationFormats(t *testing.T) {
	validator := NewQualityValidator(nil)

	citationFormats := []struct {
		content string
		desc    string
	}{
		{"See [main.go:42] for details", "bracket format"},
		{"See [main.go:42-50] for details", "bracket range format"},
		{"See (main.go:42) for details", "parenthesis format"},
		{"See (main.go:42-50) for details", "parenthesis range format"},
		{"At main.go:42 we see the handler", "bare format"},
		{"The file handler.py:15 contains the logic", "python file"},
		{"Check index.ts:100 for the component", "typescript file"},
		{"Defined in App.jsx:25", "jsx file"},
		{"The struct in types.rs:42", "rust file"},
	}

	for _, tc := range citationFormats {
		t.Run(tc.desc, func(t *testing.T) {
			// Make it long enough to require citations
			longContent := strings.Repeat("Context text. ", 20) + tc.content
			result := validator.ValidateQuality(longContent, true)
			if !result.Valid {
				t.Errorf("Should accept citation format: %q (reason: %s)", tc.content, result.Reason)
			}
		})
	}
}

func TestHasHedgingLanguage(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{"The system likely uses flags", true},
		{"It appears to load config", true},
		{"Based on the function names", true},
		{"The handler [main.go:42] processes requests", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.content[:min(30, len(tt.content))], func(t *testing.T) {
			hasHedging, _ := HasHedgingLanguage(tt.content)
			if hasHedging != tt.expected {
				t.Errorf("HasHedgingLanguage(%q) = %v, want %v", tt.content, hasHedging, tt.expected)
			}
		})
	}
}

func TestHasCitation(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{"[main.go:42]", true},
		{"(handler.py:15)", true},
		{"main.go:42", true},
		{"[main.go:42-50]", true},
		{"No citations here", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.content[:min(20, len(tt.content))], func(t *testing.T) {
			if got := HasCitation(tt.content); got != tt.expected {
				t.Errorf("HasCitation(%q) = %v, want %v", tt.content, got, tt.expected)
			}
		})
	}
}
