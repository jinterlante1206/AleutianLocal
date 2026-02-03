// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"context"
	"testing"
)

func TestToolChoiceSelector_AnalyticalQueries(t *testing.T) {
	classifier := NewRegexClassifier()
	selector := NewToolChoiceSelector(classifier, nil)
	ctx := context.Background()

	availableTools := []string{
		"find_entry_points",
		"trace_data_flow",
		"trace_error_flow",
		"find_config_usage",
	}

	tests := []struct {
		name               string
		query              string
		expectAnalytical   bool
		expectToolRequired bool
		expectSpecificTool string
	}{
		{
			name:               "test query",
			query:              "What tests exist in this project?",
			expectAnalytical:   true,
			expectToolRequired: true,
			expectSpecificTool: "find_entry_points",
		},
		{
			name:               "data flow query",
			query:              "Trace the data flow through this function",
			expectAnalytical:   true,
			expectToolRequired: true,
			expectSpecificTool: "trace_data_flow",
		},
		{
			name:               "error handling query",
			query:              "How are errors handled in this code?",
			expectAnalytical:   true,
			expectToolRequired: true,
			expectSpecificTool: "trace_error_flow",
		},
		{
			name:               "config query",
			query:              "What configuration options are available?",
			expectAnalytical:   true,
			expectToolRequired: true,
			expectSpecificTool: "find_config_usage",
		},
		{
			name:               "greeting - not analytical",
			query:              "Hello",
			expectAnalytical:   false,
			expectToolRequired: false,
			expectSpecificTool: "",
		},
		{
			name:               "general question - not analytical",
			query:              "What is the capital of France?",
			expectAnalytical:   false,
			expectToolRequired: false,
			expectSpecificTool: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selector.SelectToolChoice(ctx, tt.query, availableTools)

			if result.IsAnalytical != tt.expectAnalytical {
				t.Errorf("IsAnalytical = %v, want %v", result.IsAnalytical, tt.expectAnalytical)
			}

			if tt.expectToolRequired {
				if result.ToolChoice == nil {
					t.Error("Expected ToolChoice to be set")
				} else if result.ToolChoice.Type != "tool" && result.ToolChoice.Type != "any" {
					t.Errorf("Expected ToolChoice.Type 'tool' or 'any', got %q", result.ToolChoice.Type)
				}
			}

			if tt.expectSpecificTool != "" {
				if result.SuggestedTool != tt.expectSpecificTool {
					t.Errorf("SuggestedTool = %q, want %q", result.SuggestedTool, tt.expectSpecificTool)
				}
			}
		})
	}
}

func TestToolChoiceSelector_NoToolsAvailable(t *testing.T) {
	classifier := NewRegexClassifier()
	selector := NewToolChoiceSelector(classifier, nil)
	ctx := context.Background()

	result := selector.SelectToolChoice(ctx, "What tests exist?", nil)

	if result.ToolChoice.Type != "auto" {
		t.Errorf("Expected 'auto' when no tools available, got %q", result.ToolChoice.Type)
	}
}

func TestToolChoiceSelector_CustomThresholds(t *testing.T) {
	classifier := NewRegexClassifier()

	// Very high threshold - should rarely force specific tools
	config := &SelectorConfig{
		ForceThreshold:   0.99,
		RequireThreshold: 0.98,
	}
	selector := NewToolChoiceSelector(classifier, config)
	ctx := context.Background()

	result := selector.SelectToolChoice(ctx, "What tests exist?",
		[]string{"find_entry_points"})

	// With very high thresholds, even good matches should default to auto or any
	if result.ToolChoice.Type == "tool" {
		t.Error("With high thresholds, should not force specific tool")
	}
}

func TestToolChoiceSelector_NilContext(t *testing.T) {
	classifier := NewRegexClassifier()
	selector := NewToolChoiceSelector(classifier, nil)

	// Should not panic with nil context
	result := selector.SelectToolChoice(nil, "What tests exist?",
		[]string{"find_entry_points"})

	if result.ToolChoice == nil {
		t.Error("ToolChoice should not be nil")
	}
}
