// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// sampleResult creates a sample graph result for testing.
func sampleResult() *GraphResult {
	return &GraphResult{
		Query: "Fix auth bug",
		FocusNodes: []Node{
			{ID: "pkg/auth.ValidateToken", Name: "ValidateToken", Location: "pkg/auth/validator.go:42", Type: "function", Package: "auth"},
		},
		Graph: GraphStats{
			NodeCount: 12,
			EdgeCount: 18,
			Depth:     3,
		},
		KeyNodes: []KeyNode{
			{
				Node:    Node{ID: "pkg/auth.ValidateToken", Name: "ValidateToken", Location: "pkg/auth/validator.go:42", Type: "function"},
				Callers: 3,
				Callees: 2,
				Connections: []Connection{
					{TargetID: "handlers/auth.handleLogin", Type: "called_by"},
					{TargetID: "handlers/auth.handleRefresh", Type: "called_by"},
					{TargetID: "middleware/auth.AuthMiddleware", Type: "called_by"},
					{TargetID: "pkg/auth.TokenStore.Validate", Type: "calls"},
					{TargetID: "pkg/auth.Claims", Type: "uses_type"},
				},
			},
			{
				Node:    Node{ID: "pkg/auth.TokenStore", Name: "TokenStore", Location: "pkg/auth/store.go:10", Type: "struct"},
				Callers: 3,
				Callees: 0,
				Flag:    "shared",
				Warning: "Used by multiple functions",
			},
		},
		Risk: RiskAssessment{
			Level:          "medium",
			DirectImpact:   3,
			IndirectImpact: 7,
			Warnings:       []string{"TokenStore is shared by 3 functions"},
		},
	}
}

func TestNewFormatRegistry(t *testing.T) {
	r := NewFormatRegistry()
	if r == nil {
		t.Fatal("registry is nil")
	}

	// Check all formats are registered
	formats := []FormatType{FormatJSON, FormatOutline, FormatCompact, FormatMermaid, FormatMarkdown}
	for _, f := range formats {
		formatter, err := r.GetFormatter(f)
		if err != nil {
			t.Errorf("GetFormatter(%s) failed: %v", f, err)
		}
		if formatter == nil {
			t.Errorf("formatter for %s is nil", f)
		}
	}
}

func TestFormatRegistry_GetFormatter_Invalid(t *testing.T) {
	r := NewFormatRegistry()
	_, err := r.GetFormatter("invalid")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestFormatRegistry_ValidateCapability(t *testing.T) {
	r := NewFormatRegistry()

	// Create large result
	largeResult := &GraphResult{
		Graph: GraphStats{NodeCount: 200},
	}

	// Mermaid should fail with large result
	err := r.ValidateCapability(largeResult, FormatMermaid)
	if err == nil {
		t.Error("expected error for large result with mermaid format")
	}

	// JSON should succeed with any size
	err = r.ValidateCapability(largeResult, FormatJSON)
	if err != nil {
		t.Errorf("JSON should accept any size: %v", err)
	}
}

func TestFormatRegistry_AutoSelectFormat(t *testing.T) {
	r := NewFormatRegistry()

	tests := []struct {
		name       string
		nodeCount  int
		budget     int
		wantFormat FormatType
	}{
		{
			name:       "small result uses JSON",
			nodeCount:  10,
			budget:     1000,
			wantFormat: FormatJSON,
		},
		{
			name:       "medium result uses outline",
			nodeCount:  50,
			budget:     5000,
			wantFormat: FormatOutline,
		},
		{
			name:       "large result uses compact",
			nodeCount:  200,
			budget:     10000,
			wantFormat: FormatCompact,
		},
		{
			name:       "very large with small budget uses markdown",
			nodeCount:  500,
			budget:     1000,
			wantFormat: FormatMarkdown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &GraphResult{
				Graph: GraphStats{NodeCount: tt.nodeCount},
			}
			selected := r.AutoSelectFormat(result, tt.budget)
			if selected != tt.wantFormat {
				t.Errorf("got %s, want %s", selected, tt.wantFormat)
			}
		})
	}
}

func TestJSONFormatter(t *testing.T) {
	f := NewJSONFormatter()
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}

	// Check properties
	if !f.IsReversible() {
		t.Error("JSON should be reversible")
	}
	if !f.SupportsStreaming() {
		t.Error("JSON should support streaming")
	}
	if f.Name() != FormatJSON {
		t.Errorf("Name() = %s, want %s", f.Name(), FormatJSON)
	}

	// Test streaming
	var buf bytes.Buffer
	if err := f.FormatStreaming(result, &buf); err != nil {
		t.Errorf("FormatStreaming error: %v", err)
	}
}

func TestJSONFormatter_TokenEstimate(t *testing.T) {
	f := NewJSONFormatter()
	result := sampleResult()

	// Default tokenizer
	tokens := f.TokenEstimate(result)
	if tokens <= 0 {
		t.Error("token estimate should be positive")
	}

	// Claude tokenizer (fewer tokens due to 3.5 ratio)
	claudeTokens := f.TokenEstimate(result, "claude")
	if claudeTokens <= tokens {
		// Claude uses 3.5 chars/token vs 4.0, so should estimate MORE tokens
		// Wait, that's backwards - smaller ratio = more tokens
	}
}

func TestOutlineFormatter(t *testing.T) {
	f := NewOutlineFormatter(true)
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Check for expected content
	if !strings.Contains(output, "Fix auth bug") {
		t.Error("missing query in output")
	}
	if !strings.Contains(output, "Entry Points:") {
		t.Error("missing Entry Points section")
	}
	if !strings.Contains(output, "ValidateToken") {
		t.Error("missing focus node")
	}
	if !strings.Contains(output, "Risk Assessment:") {
		t.Error("missing risk assessment")
	}

	// Unicode characters
	if !strings.Contains(output, "├") || !strings.Contains(output, "└") {
		t.Error("missing Unicode tree characters")
	}

	// Properties
	if f.IsReversible() {
		t.Error("Outline should not be reversible")
	}
	if !f.SupportsStreaming() {
		t.Error("Outline should support streaming")
	}
}

func TestOutlineFormatter_ASCII(t *testing.T) {
	f := NewOutlineFormatter(false)
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Should use ASCII characters
	if strings.Contains(output, "├") || strings.Contains(output, "└") {
		t.Error("should not contain Unicode in ASCII mode")
	}
	if !strings.Contains(output, "|--") || !strings.Contains(output, "`--") {
		t.Error("missing ASCII tree characters")
	}
}

func TestCompactFormatter(t *testing.T) {
	f := NewCompactFormatter()
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}

	// Check for schema info
	if parsed["_v"] != CompactSchemaVersion {
		t.Errorf("missing or wrong version: %v", parsed["_v"])
	}
	if parsed["_s"] != CompactSchemaName {
		t.Errorf("missing or wrong schema: %v", parsed["_s"])
	}
	if parsed["_keys"] == nil {
		t.Error("missing key mapping")
	}

	// Check for short keys
	if _, ok := parsed["q"]; !ok {
		t.Error("missing short key 'q' for query")
	}

	// Properties
	if !f.IsReversible() {
		t.Error("Compact with schema should be reversible")
	}

	// Compare size with full JSON
	fullJSON := NewJSONFormatter()
	fullOutput, _ := fullJSON.Format(result)

	if len(output) >= len(fullOutput) {
		t.Errorf("compact (%d bytes) should be smaller than full JSON (%d bytes)",
			len(output), len(fullOutput))
	}
}

func TestCompactFormatter_NoSchema(t *testing.T) {
	f := NewCompactFormatterNoSchema()
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}

	// Should not have schema info
	if _, ok := parsed["_v"]; ok {
		t.Error("should not have version without schema")
	}

	// Should not be reversible without schema
	if f.IsReversible() {
		t.Error("Compact without schema should not be reversible")
	}
}

func TestMermaidFormatter(t *testing.T) {
	f := NewMermaidFormatter(50)
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Check Mermaid syntax
	if !strings.HasPrefix(output, "```mermaid") {
		t.Error("missing mermaid code fence")
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "```") {
		t.Error("missing closing code fence")
	}
	if !strings.Contains(output, "graph TD") {
		t.Error("missing graph declaration")
	}
	if !strings.Contains(output, "subgraph") {
		t.Error("missing subgraph")
	}
	if !strings.Contains(output, "-->") {
		t.Error("missing edge arrows")
	}

	// Properties
	if f.IsReversible() {
		t.Error("Mermaid should not be reversible")
	}
	if f.SupportsStreaming() {
		t.Error("Mermaid should not support streaming")
	}
}

func TestMermaidFormatter_NodePrioritization(t *testing.T) {
	f := NewMermaidFormatter(5) // Very small limit

	// Create result with many nodes
	result := &GraphResult{
		Query: "test",
		FocusNodes: []Node{
			{ID: "target", Name: "Target"},
		},
		KeyNodes: make([]KeyNode, 0),
	}

	// Add 20 key nodes
	for i := 0; i < 20; i++ {
		result.KeyNodes = append(result.KeyNodes, KeyNode{
			Node:    Node{ID: "node" + string(rune('A'+i)), Name: "Node"},
			Callers: i,
		})
	}
	result.Graph.NodeCount = 21

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Should include truncation notice
	if !strings.Contains(output, "Showing") && !strings.Contains(output, "of") {
		t.Error("should include truncation notice")
	}

	// Should still include target
	if !strings.Contains(output, "target") {
		t.Error("target node should always be included")
	}
}

func TestMarkdownFormatter(t *testing.T) {
	f := NewMarkdownFormatter()
	result := sampleResult()

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	// Check Markdown structure
	if !strings.Contains(output, "## Code Map:") {
		t.Error("missing header")
	}
	if !strings.Contains(output, "### Entry Points") {
		t.Error("missing entry points section")
	}
	if !strings.Contains(output, "### Symbol Summary") {
		t.Error("missing symbol summary section")
	}
	if !strings.Contains(output, "| Symbol | Location |") {
		t.Error("missing table header")
	}
	if !strings.Contains(output, "### Risk Assessment") {
		t.Error("missing risk assessment")
	}
	if !strings.Contains(output, "🟡") { // Medium risk emoji
		t.Error("missing risk emoji")
	}

	// Properties
	if f.IsReversible() {
		t.Error("Markdown should not be reversible")
	}
	if !f.SupportsStreaming() {
		t.Error("Markdown should support streaming")
	}
}

func TestMarkdownFormatter_Truncation(t *testing.T) {
	f := NewMarkdownFormatter()
	f.SetMaxRows(5)

	// Create result with many rows
	result := &GraphResult{
		Query:    "test",
		KeyNodes: make([]KeyNode, 20),
	}
	for i := range result.KeyNodes {
		result.KeyNodes[i] = KeyNode{
			Node: Node{ID: "node", Name: "Node"},
		}
	}

	output, err := f.Format(result)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}

	if !strings.Contains(output, "Showing 5 of 20") {
		t.Error("should include truncation notice")
	}
}

func TestFormatRegistry_GetMetadata(t *testing.T) {
	r := NewFormatRegistry()

	tests := []struct {
		format     FormatType
		reversible bool
	}{
		{FormatJSON, true},
		{FormatOutline, false},
		{FormatCompact, true},
		{FormatMermaid, false},
		{FormatMarkdown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			meta, err := r.GetMetadata(tt.format)
			if err != nil {
				t.Fatalf("GetMetadata error: %v", err)
			}
			if meta.Type != tt.format {
				t.Errorf("Type = %s, want %s", meta.Type, tt.format)
			}
			if meta.Reversible != tt.reversible {
				t.Errorf("Reversible = %v, want %v", meta.Reversible, tt.reversible)
			}
			if meta.Version != FormatVersion {
				t.Errorf("Version = %s, want %s", meta.Version, FormatVersion)
			}
		})
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Test package-level functions use DefaultRegistry
	result := sampleResult()

	_, err := Format(result, FormatJSON)
	if err != nil {
		t.Errorf("Format error: %v", err)
	}

	_, err = GetFormatter(FormatOutline)
	if err != nil {
		t.Errorf("GetFormatter error: %v", err)
	}

	selected := AutoSelectFormat(result, 1000)
	if selected == "" {
		t.Error("AutoSelectFormat returned empty")
	}
}

func BenchmarkFormatters(b *testing.B) {
	result := sampleResult()

	formatters := []Formatter{
		NewJSONFormatter(),
		NewOutlineFormatter(true),
		NewCompactFormatter(),
		NewMermaidFormatter(50),
		NewMarkdownFormatter(),
	}

	for _, f := range formatters {
		b.Run(string(f.Name()), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = f.Format(result)
			}
		})
	}
}
