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

func TestRegexClassifier_IsAnalytical(t *testing.T) {
	classifier := NewRegexClassifier()
	ctx := context.Background()

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		// Analytical queries - should return true
		{
			name:     "what tests exist",
			query:    "What tests exist in this project?",
			expected: true,
		},
		{
			name:     "how does authentication work",
			query:    "How does the authentication system work?",
			expected: true,
		},
		{
			name:     "trace data flow",
			query:    "Can you trace the data flow from input to database?",
			expected: true,
		},
		{
			name:     "where is config",
			query:    "Where is the configuration loaded?",
			expected: true,
		},
		{
			name:     "find entry points",
			query:    "Find all entry points in the codebase",
			expected: true,
		},
		{
			name:     "security concerns",
			query:    "Are there any security concerns in this code?",
			expected: true,
		},
		{
			name:     "error handling",
			query:    "How is error handling implemented?",
			expected: true,
		},
		{
			name:     "show me handlers",
			query:    "Show me all the HTTP handlers",
			expected: true,
		},
		{
			name:     "list functions",
			query:    "List all exported functions",
			expected: true,
		},
		{
			name:     "project structure",
			query:    "What is the project structure?",
			expected: true,
		},
		{
			name:     "main function location",
			query:    "Where is the main function?",
			expected: true,
		},
		{
			name:     "logging patterns",
			query:    "What logging patterns are used?",
			expected: true,
		},

		// Non-analytical queries - should return false
		{
			name:     "simple greeting",
			query:    "Hello",
			expected: false,
		},
		{
			name:     "math question",
			query:    "What is 2 + 2?",
			expected: false,
		},
		{
			name:     "general knowledge",
			query:    "What is the capital of France?",
			expected: false,
		},
		{
			name:     "empty query",
			query:    "",
			expected: false,
		},
		{
			name:     "thanks",
			query:    "Thanks for your help!",
			expected: false,
		},
		{
			name:     "explain concept",
			query:    "Explain what a goroutine is",
			expected: false,
		},

		// Edge cases - false positives we want to avoid
		{
			name:     "callout not call - word boundary",
			query:    "Add a callout box to the UI",
			expected: false,
		},
		{
			name:     "recall not call - word boundary",
			query:    "I recall seeing that somewhere",
			expected: false,
		},
		{
			name:     "whatsoever - word boundary",
			query:    "There's no problem whatsoever",
			expected: false,
		},

		// Case insensitivity
		{
			name:     "uppercase TESTS",
			query:    "WHAT TESTS EXIST?",
			expected: true,
		},
		{
			name:     "mixed case Trace",
			query:    "TRACE the Data Flow",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifier.IsAnalytical(ctx, tt.query)
			if got != tt.expected {
				t.Errorf("IsAnalytical(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestRegexClassifier_IsAnalytical_NilContext(t *testing.T) {
	classifier := NewRegexClassifier()

	// Should not panic with nil context
	got := classifier.IsAnalytical(nil, "What tests exist?")
	if !got {
		t.Error("IsAnalytical should work with nil context")
	}
}

func TestRegexClassifier_SuggestTool(t *testing.T) {
	classifier := NewRegexClassifier()
	ctx := context.Background()

	allTools := []string{
		"find_entry_points",
		"trace_data_flow",
		"trace_error_flow",
		"find_config_usage",
		"check_security",
	}

	tests := []struct {
		name          string
		query         string
		available     []string
		expectedTool  string
		expectedFound bool
	}{
		{
			name:          "test query suggests find_entry_points",
			query:         "What tests exist?",
			available:     allTools,
			expectedTool:  "find_entry_points",
			expectedFound: true,
		},
		{
			name:          "error query suggests trace_error_flow",
			query:         "How are errors handled?",
			available:     allTools,
			expectedTool:  "trace_error_flow",
			expectedFound: true,
		},
		{
			name:          "flow query suggests trace_data_flow",
			query:         "Trace the data flow",
			available:     allTools,
			expectedTool:  "trace_data_flow",
			expectedFound: true,
		},
		{
			name:          "config query suggests find_config_usage",
			query:         "What config options are there?",
			available:     allTools,
			expectedTool:  "find_config_usage",
			expectedFound: true,
		},
		{
			name:          "security query suggests check_security",
			query:         "Are there security vulnerabilities?",
			available:     allTools,
			expectedTool:  "check_security",
			expectedFound: true,
		},
		{
			name:          "tool not available falls back",
			query:         "What tests exist?",
			available:     []string{"trace_data_flow"},
			expectedTool:  "trace_data_flow",
			expectedFound: true,
		},
		{
			name:          "no tools available",
			query:         "What tests exist?",
			available:     []string{},
			expectedTool:  "",
			expectedFound: false,
		},
		{
			name:          "nil tools available",
			query:         "What tests exist?",
			available:     nil,
			expectedTool:  "",
			expectedFound: false,
		},
		{
			name:          "generic query defaults to find_entry_points",
			query:         "Tell me about this codebase",
			available:     allTools,
			expectedTool:  "find_entry_points",
			expectedFound: true,
		},
		{
			name:          "main function suggests find_entry_points",
			query:         "Where is the main function?",
			available:     allTools,
			expectedTool:  "find_entry_points",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, found := classifier.SuggestTool(ctx, tt.query, tt.available)
			if found != tt.expectedFound {
				t.Errorf("SuggestTool(%q).found = %v, want %v", tt.query, found, tt.expectedFound)
			}
			if tool != tt.expectedTool {
				t.Errorf("SuggestTool(%q).tool = %q, want %q", tt.query, tool, tt.expectedTool)
			}
		})
	}
}

func TestRegexClassifier_SuggestTool_NilContext(t *testing.T) {
	classifier := NewRegexClassifier()

	// Should not panic with nil context
	tool, found := classifier.SuggestTool(nil, "What tests exist?", []string{"find_entry_points"})
	if !found || tool != "find_entry_points" {
		t.Errorf("SuggestTool should work with nil context, got tool=%q found=%v", tool, found)
	}
}

func TestRegexClassifier_Interface(t *testing.T) {
	// Verify RegexClassifier implements QueryClassifier
	var _ QueryClassifier = (*RegexClassifier)(nil)
	var _ QueryClassifier = NewRegexClassifier()
}

func BenchmarkRegexClassifier_IsAnalytical(b *testing.B) {
	classifier := NewRegexClassifier()
	ctx := context.Background()
	query := "How does the authentication system work with the database?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifier.IsAnalytical(ctx, query)
	}
}

func BenchmarkRegexClassifier_SuggestTool(b *testing.B) {
	classifier := NewRegexClassifier()
	ctx := context.Background()
	query := "What tests exist in the authentication module?"
	available := []string{
		"find_entry_points",
		"trace_data_flow",
		"trace_error_flow",
		"find_config_usage",
		"check_security",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifier.SuggestTool(ctx, query, available)
	}
}
