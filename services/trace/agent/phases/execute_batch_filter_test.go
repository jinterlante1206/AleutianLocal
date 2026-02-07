// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// -----------------------------------------------------------------------------
// Mock Types
// -----------------------------------------------------------------------------

// mockBatchFilterer implements BatchFilterer for testing.
type mockBatchFilterer struct {
	response string
	err      error
	delay    time.Duration
}

func (m *mockBatchFilterer) FilterBatch(ctx context.Context, prompt string) (string, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return m.response, m.err
}

// mockToolRouter implements agent.ToolRouter with BatchFilterer for testing.
type mockToolRouter struct {
	filterer *mockBatchFilterer
}

func (m *mockToolRouter) SelectTool(ctx context.Context, query string, availableTools []agent.ToolRouterSpec, codeContext *agent.ToolRouterCodeContext) (*agent.ToolRouterSelection, error) {
	return nil, nil
}

func (m *mockToolRouter) Model() string {
	return "mock-model"
}

func (m *mockToolRouter) FilterBatch(ctx context.Context, prompt string) (string, error) {
	if m.filterer != nil {
		return m.filterer.FilterBatch(ctx, prompt)
	}
	return "", nil
}

// testSessionWrapper wraps an agent.Session for testing.
// In unit tests, we use nil sessions with the filter logic handling gracefully.

// -----------------------------------------------------------------------------
// Test: filterBatchWithRouter
// -----------------------------------------------------------------------------

// makeToolInvocation creates a ToolInvocation with string params for testing.
func makeToolInvocation(tool string, params map[string]string) agent.ToolInvocation {
	return agent.ToolInvocation{
		Tool: tool,
		Parameters: &agent.ToolParameters{
			StringParams: params,
		},
	}
}

func TestFilterBatchWithRouter_SmallBatch(t *testing.T) {
	t.Run("batch of 1 passes through", func(t *testing.T) {
		p := &ExecutePhase{}
		ctx := context.Background()
		deps := &Dependencies{} // No session, so no filterer - should passthrough
		invocations := []agent.ToolInvocation{
			makeToolInvocation("find_callees", map[string]string{"function_name": "main"}),
		}

		result, err := p.filterBatchWithRouter(ctx, deps, invocations)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 invocation, got %d", len(result))
		}
	})

	t.Run("batch of 2 passes through", func(t *testing.T) {
		p := &ExecutePhase{}
		ctx := context.Background()
		deps := &Dependencies{} // No session
		invocations := []agent.ToolInvocation{
			makeToolInvocation("find_callees", map[string]string{"function_name": "main"}),
			makeToolInvocation("find_callees", map[string]string{"function_name": "init"}),
		}

		result, err := p.filterBatchWithRouter(ctx, deps, invocations)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 invocations (passthrough), got %d", len(result))
		}
	})
}

func TestFilterBatchWithRouter_NoSession(t *testing.T) {
	p := &ExecutePhase{}
	ctx := context.Background()
	deps := &Dependencies{} // No session configured
	invocations := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "a"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "b"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "c"}),
	}

	result, err := p.filterBatchWithRouter(ctx, deps, invocations)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 invocations (passthrough), got %d", len(result))
	}
}

func TestFilterBatchWithRouter_NilContext(t *testing.T) {
	p := &ExecutePhase{}
	deps := &Dependencies{}
	invocations := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "a"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "b"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "c"}),
	}

	result, err := p.filterBatchWithRouter(nil, deps, invocations)

	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if len(result) != 3 {
		t.Errorf("expected original batch on error, got %d", len(result))
	}
}

func TestFilterBatchWithRouter_NilDeps(t *testing.T) {
	p := &ExecutePhase{}
	ctx := context.Background()
	invocations := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "a"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "b"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "c"}),
	}

	result, err := p.filterBatchWithRouter(ctx, nil, invocations)

	// Should gracefully degrade - return original batch
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected original 3 invocations, got %d", len(result))
	}
}

// -----------------------------------------------------------------------------
// Test: parseFilterResponse
// -----------------------------------------------------------------------------

func TestParseFilterResponse_StandardFormat(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{
			name:     "all keep",
			response: "1:KEEP 2:KEEP 3:KEEP",
			want:     3,
		},
		{
			name:     "all skip keeps first",
			response: "1:SKIP 2:SKIP 3:SKIP",
			want:     1, // Safety: keep at least first
		},
		{
			name:     "mixed",
			response: "1:KEEP 2:SKIP 3:KEEP",
			want:     2,
		},
		{
			name:     "with newlines",
			response: "1:KEEP\n2:SKIP\n3:KEEP",
			want:     2,
		},
		{
			name:     "lowercase",
			response: "1:keep 2:skip 3:keep",
			want:     2,
		},
		{
			name:     "extra spaces",
			response: "1 : KEEP   2 : SKIP   3 : KEEP",
			want:     2,
		},
	}

	p := &ExecutePhase{}
	ctx := context.Background()
	invocations := []agent.ToolInvocation{
		{Tool: "a"},
		{Tool: "b"},
		{Tool: "c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseFilterResponse(ctx, tt.response, invocations)
			if len(result) != tt.want {
				t.Errorf("parseFilterResponse(%q) = %d invocations, want %d", tt.response, len(result), tt.want)
			}
		})
	}
}

func TestParseFilterResponse_FallbackFormat(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{
			name:     "just words",
			response: "KEEP SKIP KEEP",
			want:     2,
		},
		{
			name:     "with reasoning",
			response: "I'll KEEP the first, SKIP the second, and KEEP the third",
			want:     2,
		},
	}

	p := &ExecutePhase{}
	ctx := context.Background()
	invocations := []agent.ToolInvocation{
		{Tool: "a"},
		{Tool: "b"},
		{Tool: "c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseFilterResponse(ctx, tt.response, invocations)
			if len(result) != tt.want {
				t.Errorf("parseFilterResponse(%q) = %d invocations, want %d", tt.response, len(result), tt.want)
			}
		})
	}
}

func TestParseFilterResponse_InvalidFormat(t *testing.T) {
	p := &ExecutePhase{}
	ctx := context.Background()
	invocations := []agent.ToolInvocation{
		{Tool: "a"},
		{Tool: "b"},
		{Tool: "c"},
	}

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "garbage",
			response: "xyzzy plugh",
		},
		{
			name:     "empty",
			response: "",
		},
		{
			name:     "wrong count",
			response: "KEEP SKIP", // Only 2 for 3 invocations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseFilterResponse(ctx, tt.response, invocations)
			// Should return all original on parse failure
			if len(result) != 3 {
				t.Errorf("parseFilterResponse(%q) = %d invocations, want 3 (all kept on failure)", tt.response, len(result))
			}
		})
	}
}

func TestParseFilterResponse_AllSkipped(t *testing.T) {
	p := &ExecutePhase{}
	ctx := context.Background()
	invocations := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "first"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "second"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "third"}),
	}

	result := p.parseFilterResponse(ctx, "1:SKIP 2:SKIP 3:SKIP", invocations)

	// Safety: should keep at least first
	if len(result) != 1 {
		t.Errorf("expected 1 invocation (first kept), got %d", len(result))
	}
	// First invocation should be kept
	if result[0].Tool != "find_callees" {
		t.Errorf("expected first invocation to be kept")
	}
}

// -----------------------------------------------------------------------------
// Test: computeHistorySimilarityWithTerms
// -----------------------------------------------------------------------------

func TestComputeHistorySimilarity_ExactMatch(t *testing.T) {
	p := &ExecutePhase{}
	history := []crs.TraceStep{
		{
			Tool:     "find_callees",
			Action:   "tool_call",
			Metadata: map[string]string{"function_name": "main"},
		},
	}

	sim, match := p.computeHistorySimilarity("find_callees", "main", history)

	if sim != 1.0 {
		t.Errorf("expected similarity 1.0 for exact match, got %f", sim)
	}
	if match != "main" {
		t.Errorf("expected match 'main', got %q", match)
	}
}

func TestComputeHistorySimilarity_PartialMatch(t *testing.T) {
	p := &ExecutePhase{}
	// Use underscore-separated names which extractQueryTerms handles correctly
	// (camelCase splitting happens after lowercasing, so doesn't work)
	history := []crs.TraceStep{
		{
			Tool:     "find_callees",
			Action:   "tool_call",
			Metadata: map[string]string{"function_name": "parse_config_file"},
		},
	}

	sim, match := p.computeHistorySimilarity("find_callees", "parse_config", history)

	// "parse_config_file" and "parse_config" share terms: ["parse", "config"]
	// parse_config_file: ["parse", "config", "file"]
	// parse_config: ["parse", "config"]
	// Jaccard = 2 / 3 = 0.666...
	if sim < 0.6 || sim > 0.7 {
		t.Errorf("expected similarity ~0.66 for partial match, got %f", sim)
	}
	if match != "parse_config_file" {
		t.Errorf("expected match 'parse_config_file', got %q", match)
	}
}

func TestComputeHistorySimilarity_NoMatch(t *testing.T) {
	p := &ExecutePhase{}
	history := []crs.TraceStep{
		{
			Tool:     "find_callees",
			Action:   "tool_call",
			Metadata: map[string]string{"function_name": "handleRequest"},
		},
	}

	sim, match := p.computeHistorySimilarity("find_callees", "parseConfig", history)

	// No common terms
	if sim != 0 {
		t.Errorf("expected similarity 0 for no match, got %f", sim)
	}
	if match != "" {
		t.Errorf("expected empty match, got %q", match)
	}
}

func TestComputeHistorySimilarity_DifferentTool(t *testing.T) {
	p := &ExecutePhase{}
	history := []crs.TraceStep{
		{
			Tool:     "Grep", // Different tool
			Action:   "tool_call",
			Metadata: map[string]string{"pattern": "main"},
		},
	}

	sim, match := p.computeHistorySimilarity("find_callees", "main", history)

	// Should not match different tool
	if sim != 0 {
		t.Errorf("expected similarity 0 for different tool, got %f", sim)
	}
	if match != "" {
		t.Errorf("expected empty match for different tool, got %q", match)
	}
}

// -----------------------------------------------------------------------------
// Test: computeBatchSimilarity
// -----------------------------------------------------------------------------

func TestComputeBatchSimilarity_ExactMatch(t *testing.T) {
	p := &ExecutePhase{}
	earlier := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "main"}),
	}
	current := makeToolInvocation("find_callees", map[string]string{"function_name": "main"})

	sim, match := p.computeBatchSimilarity(&current, earlier)

	if sim != 1.0 {
		t.Errorf("expected similarity 1.0 for exact match, got %f", sim)
	}
	if match != "1" {
		t.Errorf("expected match '1', got %q", match)
	}
}

func TestComputeBatchSimilarity_PartialMatch(t *testing.T) {
	p := &ExecutePhase{}
	// Use underscore-separated names which extractQueryTerms handles correctly
	// (camelCase splitting happens after lowercasing, so doesn't work)
	earlier := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "analyze_beacon_data"}),
	}
	current := makeToolInvocation("find_callees", map[string]string{"function_name": "analyze_daily_beacon_data"})

	sim, _ := p.computeBatchSimilarity(&current, earlier)

	// "analyze_beacon_data" and "analyze_daily_beacon_data" share terms:
	// [analyze, beacon, data] vs [analyze, daily, beacon, data]
	// Jaccard = 3 / 4 = 0.75
	if sim < 0.6 {
		t.Errorf("expected high similarity for partial match, got %f", sim)
	}
}

// -----------------------------------------------------------------------------
// Test: buildBatchFilterPrompt
// -----------------------------------------------------------------------------

func TestBuildBatchFilterPrompt(t *testing.T) {
	p := &ExecutePhase{}
	invocations := []agent.ToolInvocation{
		makeToolInvocation("find_callees", map[string]string{"function_name": "main"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "init"}),
		makeToolInvocation("find_callees", map[string]string{"function_name": "parseConfig"}),
	}
	history := []crs.TraceStep{
		{
			Tool:     "find_callees",
			Action:   "tool_call",
			Metadata: map[string]string{"function_name": "main"},
		},
	}

	prompt := p.buildBatchFilterPrompt("What does main call?", invocations, history)

	// Verify prompt structure
	if len(prompt) == 0 {
		t.Fatal("prompt should not be empty")
	}

	// Should contain query
	if !stringContains(prompt, "What does main call?") {
		t.Error("prompt should contain the query")
	}

	// Should list history
	if !stringContains(prompt, "Already executed:") {
		t.Error("prompt should contain history section")
	}

	// Should list pending calls
	if !stringContains(prompt, "1. find_callees(main)") {
		t.Error("prompt should list first invocation")
	}

	// First should show similarity to history (100% match)
	if !stringContains(prompt, "100% similar to executed: main") {
		t.Error("prompt should show similarity for duplicate")
	}

	// Should include format instructions
	if !stringContains(prompt, "Format: 1:KEEP 2:SKIP") {
		t.Error("prompt should include format instructions")
	}
}

// -----------------------------------------------------------------------------
// Test: extractHistoryToolQuery
// -----------------------------------------------------------------------------

func TestExtractHistoryToolQuery(t *testing.T) {
	tests := []struct {
		name     string
		step     *crs.TraceStep
		expected string
	}{
		{
			name:     "nil step",
			step:     nil,
			expected: "",
		},
		{
			name: "function_name key",
			step: &crs.TraceStep{
				Metadata: map[string]string{"function_name": "main"},
			},
			expected: "main",
		},
		{
			name: "pattern key",
			step: &crs.TraceStep{
				Metadata: map[string]string{"pattern": "*.go"},
			},
			expected: "*.go",
		},
		{
			name: "query key",
			step: &crs.TraceStep{
				Metadata: map[string]string{"query": "search term"},
			},
			expected: "search term",
		},
		{
			name: "priority order",
			step: &crs.TraceStep{
				Metadata: map[string]string{
					"function_name": "priority2",
					"pattern":       "priority1",
				},
			},
			expected: "priority1", // pattern takes priority (first in queryParamNames)
		},
		{
			name: "no metadata",
			step: &crs.TraceStep{
				Metadata: nil,
			},
			expected: "",
		},
		// GR-39a: Test newly added parameter names
		{
			name: "package key (GR-39a)",
			step: &crs.TraceStep{
				Metadata: map[string]string{"package": "api"},
			},
			expected: "api",
		},
		{
			name: "symbol_name key (GR-39a)",
			step: &crs.TraceStep{
				Metadata: map[string]string{"symbol_name": "ParseConfig"},
			},
			expected: "ParseConfig",
		},
		{
			name: "interface_name key (GR-39a)",
			step: &crs.TraceStep{
				Metadata: map[string]string{"interface_name": "Handler"},
			},
			expected: "Handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHistoryToolQuery(tt.step)
			if result != tt.expected {
				t.Errorf("extractHistoryToolQuery() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstringInBatchFilter(s, substr)))
}

func findSubstringInBatchFilter(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
