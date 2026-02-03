// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package phases

import (
	"context"
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/classifier"
)

// mockClassifier is a test classifier with configurable behavior.
type mockClassifier struct {
	isAnalytical  bool
	suggestedTool string
	hasSuggestion bool
}

func (m *mockClassifier) IsAnalytical(_ context.Context, _ string) bool {
	return m.isAnalytical
}

func (m *mockClassifier) SuggestTool(_ context.Context, _ string, available []string) (string, bool) {
	if !m.hasSuggestion {
		return "", false
	}
	// Return suggested tool if it's in available list
	for _, t := range available {
		if t == m.suggestedTool {
			return m.suggestedTool, true
		}
	}
	// Fall back to first available
	if len(available) > 0 {
		return available[0], true
	}
	return "", false
}

func (m *mockClassifier) SuggestToolWithHint(_ context.Context, _ string, available []string) (*classifier.ToolSuggestion, bool) {
	tool, ok := m.SuggestTool(nil, "", available)
	if !ok {
		return nil, false
	}
	return &classifier.ToolSuggestion{
		ToolName:       tool,
		SearchHint:     "Use " + tool + " to explore",
		SearchPatterns: []string{"*_test.go"},
	}, true
}

var _ classifier.QueryClassifier = (*mockClassifier)(nil)

func TestDefaultForcingPolicy_ShouldForce(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		isAnalytical      bool
		stepNumber        int
		maxStepForForcing int
		forcingRetries    int
		maxRetries        int
		expected          bool
	}{
		{
			name:              "analytical query on first step",
			isAnalytical:      true,
			stepNumber:        1,
			maxStepForForcing: 2,
			forcingRetries:    0,
			maxRetries:        2,
			expected:          true,
		},
		{
			name:              "analytical query on max step",
			isAnalytical:      true,
			stepNumber:        2,
			maxStepForForcing: 2,
			forcingRetries:    0,
			maxRetries:        2,
			expected:          true,
		},
		{
			name:              "analytical query past max step",
			isAnalytical:      true,
			stepNumber:        3,
			maxStepForForcing: 2,
			forcingRetries:    0,
			maxRetries:        2,
			expected:          false, // Step threshold exceeded
		},
		{
			name:              "non-analytical query",
			isAnalytical:      false,
			stepNumber:        1,
			maxStepForForcing: 2,
			forcingRetries:    0,
			maxRetries:        2,
			expected:          false, // Not analytical
		},
		{
			name:              "circuit breaker triggered",
			isAnalytical:      true,
			stepNumber:        1,
			maxStepForForcing: 2,
			forcingRetries:    2,
			maxRetries:        2,
			expected:          false, // Circuit breaker
		},
		{
			name:              "last retry before circuit breaker",
			isAnalytical:      true,
			stepNumber:        1,
			maxStepForForcing: 2,
			forcingRetries:    1,
			maxRetries:        2,
			expected:          true, // One more try allowed
		},
		{
			name:              "step zero",
			isAnalytical:      true,
			stepNumber:        0,
			maxStepForForcing: 2,
			forcingRetries:    0,
			maxRetries:        2,
			expected:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := NewDefaultForcingPolicyWithClassifier(&mockClassifier{
				isAnalytical: tt.isAnalytical,
			})

			req := &ForcingRequest{
				Query:             "test query",
				StepNumber:        tt.stepNumber,
				MaxStepForForcing: tt.maxStepForForcing,
				ForcingRetries:    tt.forcingRetries,
				MaxRetries:        tt.maxRetries,
			}

			got := policy.ShouldForce(ctx, req)
			if got != tt.expected {
				t.Errorf("ShouldForce() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultForcingPolicy_ShouldForce_NilHandling(t *testing.T) {
	policy := NewDefaultForcingPolicy()

	t.Run("nil context", func(t *testing.T) {
		req := &ForcingRequest{
			Query:             "What tests exist?",
			StepNumber:        1,
			MaxStepForForcing: 2,
			MaxRetries:        2,
		}
		// Should not panic
		got := policy.ShouldForce(nil, req)
		if !got {
			t.Error("Expected true for analytical query even with nil context")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		// Should not panic, should return false
		got := policy.ShouldForce(context.Background(), nil)
		if got {
			t.Error("Expected false for nil request")
		}
	})
}

func TestDefaultForcingPolicy_BuildHint(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		suggestedTool  string
		hasSuggestion  bool
		availableTools []string
		wantContains   []string
	}{
		{
			name:           "with suggested tool",
			suggestedTool:  "find_entry_points",
			hasSuggestion:  true,
			availableTools: []string{"find_entry_points", "trace_data_flow"},
			wantContains:   []string{"find_entry_points", "MUST use tools"},
		},
		{
			name:           "without suggestion",
			suggestedTool:  "",
			hasSuggestion:  false,
			availableTools: []string{"some_tool"},
			wantContains:   []string{"MUST use tools"},
		},
		{
			name:           "empty available tools",
			suggestedTool:  "find_entry_points",
			hasSuggestion:  true,
			availableTools: []string{},
			wantContains:   []string{"MUST use tools"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := NewDefaultForcingPolicyWithClassifier(&mockClassifier{
				suggestedTool: tt.suggestedTool,
				hasSuggestion: tt.hasSuggestion,
			})

			req := &ForcingRequest{
				Query:          "What tests exist?",
				AvailableTools: tt.availableTools,
			}

			got := policy.BuildHint(ctx, req)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("BuildHint() = %q, should contain %q", got, want)
				}
			}
		})
	}
}

func TestDefaultForcingPolicy_BuildHint_NilHandling(t *testing.T) {
	policy := NewDefaultForcingPolicy()

	t.Run("nil context", func(t *testing.T) {
		req := &ForcingRequest{
			Query:          "What tests exist?",
			AvailableTools: []string{"find_entry_points"},
		}
		// Should not panic
		got := policy.BuildHint(nil, req)
		if got == "" {
			t.Error("Expected non-empty hint even with nil context")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		// Should not panic, should return generic hint
		got := policy.BuildHint(context.Background(), nil)
		if got == "" {
			t.Error("Expected non-empty hint for nil request")
		}
	})
}

func TestNewDefaultForcingPolicy(t *testing.T) {
	policy := NewDefaultForcingPolicy()
	if policy == nil {
		t.Fatal("NewDefaultForcingPolicy() returned nil")
	}
	if policy.classifier == nil {
		t.Error("Policy has nil classifier")
	}
}

func TestNewDefaultForcingPolicyWithClassifier(t *testing.T) {
	t.Run("with valid classifier", func(t *testing.T) {
		mock := &mockClassifier{isAnalytical: true}
		policy := NewDefaultForcingPolicyWithClassifier(mock)
		if policy.classifier != mock {
			t.Error("Policy doesn't have the provided classifier")
		}
	})

	t.Run("with nil classifier", func(t *testing.T) {
		policy := NewDefaultForcingPolicyWithClassifier(nil)
		if policy.classifier == nil {
			t.Error("Policy should have default classifier when nil provided")
		}
	})
}

func TestForcingRequest_Fields(t *testing.T) {
	req := &ForcingRequest{
		Query:             "What tests exist?",
		StepNumber:        5,
		ForcingRetries:    2,
		MaxRetries:        3,
		MaxStepForForcing: 10,
		AvailableTools:    []string{"tool1", "tool2"},
	}

	if req.Query != "What tests exist?" {
		t.Error("Query field not set correctly")
	}
	if req.StepNumber != 5 {
		t.Error("StepNumber field not set correctly")
	}
	if req.ForcingRetries != 2 {
		t.Error("ForcingRetries field not set correctly")
	}
	if req.MaxRetries != 3 {
		t.Error("MaxRetries field not set correctly")
	}
	if req.MaxStepForForcing != 10 {
		t.Error("MaxStepForForcing field not set correctly")
	}
	if len(req.AvailableTools) != 2 {
		t.Error("AvailableTools field not set correctly")
	}
}

func TestToolForcingPolicy_Interface(t *testing.T) {
	// Verify DefaultForcingPolicy implements ToolForcingPolicy
	var _ ToolForcingPolicy = (*DefaultForcingPolicy)(nil)
	var _ ToolForcingPolicy = NewDefaultForcingPolicy()
}

func BenchmarkDefaultForcingPolicy_ShouldForce(b *testing.B) {
	policy := NewDefaultForcingPolicy()
	ctx := context.Background()
	req := &ForcingRequest{
		Query:             "What tests exist in the authentication module?",
		StepNumber:        1,
		MaxStepForForcing: 2,
		ForcingRetries:    0,
		MaxRetries:        2,
		AvailableTools:    []string{"find_entry_points", "trace_data_flow"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.ShouldForce(ctx, req)
	}
}

func BenchmarkDefaultForcingPolicy_BuildHint(b *testing.B) {
	policy := NewDefaultForcingPolicy()
	ctx := context.Background()
	req := &ForcingRequest{
		Query:          "What tests exist in the authentication module?",
		AvailableTools: []string{"find_entry_points", "trace_data_flow", "trace_error_flow"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.BuildHint(ctx, req)
	}
}
