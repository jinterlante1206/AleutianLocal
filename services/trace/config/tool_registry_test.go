// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package config

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// TestGetToolRoutingRegistry_Singleton tests that the registry is loaded once.
func TestGetToolRoutingRegistry_Singleton(t *testing.T) {
	// Reset to ensure clean state
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()

	// First call should load the registry
	reg1, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}
	if reg1 == nil {
		t.Fatal("GetToolRoutingRegistry returned nil")
	}

	// Second call should return the same instance
	reg2, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry second call failed: %v", err)
	}
	if reg1 != reg2 {
		t.Error("GetToolRoutingRegistry should return same instance (singleton)")
	}
}

// TestGetToolRoutingRegistry_NilContext tests that nil context returns error.
func TestGetToolRoutingRegistry_NilContext(t *testing.T) {
	// Reset to ensure clean state
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	_, err := GetToolRoutingRegistry(nil)
	if err == nil {
		t.Error("GetToolRoutingRegistry(nil) should return error")
	}
}

// TestToolRoutingRegistry_GetEntry tests looking up tool entries.
func TestToolRoutingRegistry_GetEntry(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	// Should find find_callers
	entry, ok := reg.GetEntry("find_callers")
	if !ok {
		t.Error("GetEntry('find_callers') should return true")
	}
	if entry == nil {
		t.Fatal("GetEntry('find_callers') returned nil entry")
	}
	if entry.Name != "find_callers" {
		t.Errorf("GetEntry returned wrong name: %s", entry.Name)
	}
	if len(entry.Keywords) == 0 {
		t.Error("find_callers should have keywords")
	}

	// Should not find nonexistent tool
	_, ok = reg.GetEntry("nonexistent_tool")
	if ok {
		t.Error("GetEntry('nonexistent_tool') should return false")
	}
}

// TestToolRoutingRegistry_FindToolsByKeyword tests keyword matching.
func TestToolRoutingRegistry_FindToolsByKeyword(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	tests := []struct {
		name           string
		query          string
		wantTool       string
		wantInResults  bool
		wantMatchCount int
	}{
		{
			name:          "callers query matches find_callers",
			query:         "find all callers of this function",
			wantTool:      "find_callers",
			wantInResults: true,
		},
		{
			name:          "callees query matches find_callees",
			query:         "find all callees of this function",
			wantTool:      "find_callees",
			wantInResults: true,
		},
		{
			name:          "search query matches Grep",
			query:         "search for pattern in files",
			wantTool:      "Grep",
			wantInResults: true,
		},
		{
			name:          "who calls query matches find_callers",
			query:         "who calls parseConfig",
			wantTool:      "find_callers",
			wantInResults: true,
		},
		{
			name:          "no match for gibberish",
			query:         "xyzzy foo bar",
			wantInResults: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := reg.FindToolsByKeyword(tt.query)

			if !tt.wantInResults {
				// Expect no matches or low match count
				return
			}

			// Check if expected tool is in results
			found := false
			for _, m := range matches {
				if m.ToolName == tt.wantTool {
					found = true
					if m.MatchCount == 0 {
						t.Errorf("MatchCount should be > 0 for %s", tt.wantTool)
					}
					break
				}
			}
			if !found {
				t.Errorf("Expected to find %s in matches for query %q", tt.wantTool, tt.query)
				t.Logf("Got matches: %v", matches)
			}
		})
	}
}

// TestToolRoutingRegistry_GetWhenToUse tests WhenToUse retrieval.
func TestToolRoutingRegistry_GetWhenToUse(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	// Should find find_callers
	whenToUse, ok := reg.GetWhenToUse("find_callers")
	if !ok {
		t.Error("GetWhenToUse('find_callers') should return true")
	}
	if len(whenToUse.Keywords) == 0 {
		t.Error("find_callers should have keywords in WhenToUse")
	}
	if whenToUse.UseWhen == "" {
		t.Error("find_callers should have UseWhen in WhenToUse")
	}

	// Should not find nonexistent tool
	_, ok = reg.GetWhenToUse("nonexistent_tool")
	if ok {
		t.Error("GetWhenToUse('nonexistent_tool') should return false")
	}
}

// TestToolRoutingRegistry_ToolCount tests tool counting.
func TestToolRoutingRegistry_ToolCount(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	count := reg.ToolCount()
	if count < 30 {
		t.Errorf("Expected at least 30 tools in registry, got %d", count)
	}
}

// TestToolRoutingRegistry_KeywordCount tests keyword counting.
func TestToolRoutingRegistry_KeywordCount(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	count := reg.KeywordCount()
	if count < 50 {
		t.Errorf("Expected at least 50 keywords in index, got %d", count)
	}
}

// TestToolRoutingRegistry_LoadedAt tests timestamp.
func TestToolRoutingRegistry_LoadedAt(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	loadedAt := reg.LoadedAt()
	if loadedAt == 0 {
		t.Error("LoadedAt should be non-zero")
	}
}

// TestParseToolRegistryYAML_InvalidYAML tests error handling for invalid YAML.
func TestParseToolRegistryYAML_InvalidYAML(t *testing.T) {
	ctx := context.Background()

	invalidYAML := []byte("this is not valid yaml: [")
	_, err := parseToolRegistryYAML(ctx, invalidYAML)
	if err == nil {
		t.Error("parseToolRegistryYAML should fail for invalid YAML")
	}
}

// TestParseToolRegistryYAML_EmptyToolName tests validation for empty tool name.
func TestParseToolRegistryYAML_EmptyToolName(t *testing.T) {
	ctx := context.Background()

	yamlWithEmptyName := []byte(`
tools:
  - name: ""
    keywords:
      - test
    use_when: "test"
`)
	_, err := parseToolRegistryYAML(ctx, yamlWithEmptyName)
	if err == nil {
		t.Error("parseToolRegistryYAML should fail for empty tool name")
	}
}

// TestParseToolRegistryYAML_TooManyKeywords tests validation for keyword limit.
func TestParseToolRegistryYAML_TooManyKeywords(t *testing.T) {
	ctx := context.Background()

	// Generate YAML with more than MaxKeywordsPerTool keywords
	keywords := ""
	for i := 0; i < MaxKeywordsPerTool+10; i++ {
		keywords += "      - keyword" + string(rune('0'+i%10)) + "\n"
	}

	yamlWithTooManyKeywords := []byte(`
tools:
  - name: "test_tool"
` + keywords + `    use_when: "test"
`)
	_, err := parseToolRegistryYAML(ctx, yamlWithTooManyKeywords)
	if err == nil {
		t.Error("parseToolRegistryYAML should fail for too many keywords")
	}
}

// TestMetricRecording tests that metrics are recorded.
func TestMetricRecording(t *testing.T) {
	// Just test that the functions don't panic
	RecordRoutingDecision("find_callers", "keywords")
	RecordFallbackBlocked()
}

// BenchmarkFindToolsByKeyword benchmarks keyword matching performance.
func BenchmarkFindToolsByKeyword(b *testing.B) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		b.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	query := "find all callers of the parseConfig function"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.FindToolsByKeyword(query)
	}
}

// TestPopulateToolDefinitionsWhenToUse tests populating WhenToUse on tool defs.
func TestPopulateToolDefinitionsWhenToUse(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()

	// Create some mock tool definitions
	defs := []tools.ToolDefinition{
		{Name: "find_callers"},
		{Name: "find_callees"},
		{Name: "Grep"},
		{Name: "nonexistent_tool"},
	}

	count, err := PopulateToolDefinitionsWhenToUse(ctx, defs)
	if err != nil {
		t.Fatalf("PopulateToolDefinitionsWhenToUse failed: %v", err)
	}

	// Should populate at least 3 (find_callers, find_callees, Grep)
	if count < 3 {
		t.Errorf("Expected at least 3 tools populated, got %d", count)
	}

	// Check that find_callers has keywords populated
	for _, def := range defs {
		if def.Name == "find_callers" {
			if len(def.WhenToUse.Keywords) == 0 {
				t.Error("find_callers should have keywords after population")
			}
			if def.WhenToUse.UseWhen == "" {
				t.Error("find_callers should have UseWhen after population")
			}
		}
	}
}

// TestPopulateToolDefinitionsWhenToUse_NilContext tests nil context handling.
func TestPopulateToolDefinitionsWhenToUse_NilContext(t *testing.T) {
	_, err := PopulateToolDefinitionsWhenToUse(nil, []tools.ToolDefinition{})
	if err == nil {
		t.Error("PopulateToolDefinitionsWhenToUse(nil, ...) should return error")
	}
}

// TestFindToolsByKeyword_MultiWordKeywords tests multi-word keyword matching.
func TestFindToolsByKeyword_MultiWordKeywords(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	tests := []struct {
		name     string
		query    string
		wantTool string
	}{
		{
			name:     "who calls matches find_callers",
			query:    "who calls this function",
			wantTool: "find_callers",
		},
		{
			name:     "call chain matches get_call_chain",
			query:    "show me the call chain for this function",
			wantTool: "get_call_chain",
		},
		{
			name:     "entry points matches find_entry_points",
			query:    "find entry points in the codebase",
			wantTool: "find_entry_points",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := reg.FindToolsByKeyword(tt.query)
			found := false
			for _, m := range matches {
				if m.ToolName == tt.wantTool {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected %s in matches for %q", tt.wantTool, tt.query)
			}
		})
	}
}

// TestToolRoutingEntry_InsteadOf tests tool substitution.
func TestToolRoutingEntry_InsteadOf(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	// find_callers should have InsteadOf entries
	entry, ok := reg.GetEntry("find_callers")
	if !ok {
		t.Fatal("find_callers not found in registry")
	}

	if len(entry.InsteadOf) == 0 {
		t.Error("find_callers should have InsteadOf entries")
	}

	// Check that Grep is in the InsteadOf list
	hasGrep := false
	for _, sub := range entry.InsteadOf {
		if sub.Tool == "Grep" {
			hasGrep = true
			if sub.When == "" {
				t.Error("InsteadOf.When should not be empty")
			}
		}
	}
	if !hasGrep {
		t.Error("find_callers should prefer over Grep")
	}
}

// TestToolRoutingRegistry_AllToolsHaveKeywords tests that all tools have keywords.
func TestToolRoutingRegistry_AllToolsHaveKeywords(t *testing.T) {
	ResetToolRoutingRegistry()
	defer ResetToolRoutingRegistry()

	ctx := context.Background()
	reg, err := GetToolRoutingRegistry(ctx)
	if err != nil {
		t.Fatalf("GetToolRoutingRegistry failed: %v", err)
	}

	// Check that key tools have keywords
	criticalTools := []string{
		"find_callers",
		"find_callees",
		"find_implementations",
		"Grep",
		"Read",
		"Write",
	}

	for _, toolName := range criticalTools {
		entry, ok := reg.GetEntry(toolName)
		if !ok {
			t.Errorf("Critical tool %s not found in registry", toolName)
			continue
		}
		if len(entry.Keywords) == 0 {
			t.Errorf("Critical tool %s has no keywords", toolName)
		}
		if entry.UseWhen == "" {
			t.Errorf("Critical tool %s has no UseWhen", toolName)
		}
	}
}
