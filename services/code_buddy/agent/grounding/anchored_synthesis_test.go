// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

func TestDefaultAnchoredSynthesisConfig(t *testing.T) {
	config := DefaultAnchoredSynthesisConfig()

	if !config.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if config.MaxEvidenceTokens != 8000 {
		t.Errorf("expected MaxEvidenceTokens=8000, got %d", config.MaxEvidenceTokens)
	}
	if config.MaxResultLength != 2000 {
		t.Errorf("expected MaxResultLength=2000, got %d", config.MaxResultLength)
	}
	if config.MinResultsToInclude != 3 {
		t.Errorf("expected MinResultsToInclude=3, got %d", config.MinResultsToInclude)
	}
	if config.RecencyWeight != 0.5 {
		t.Errorf("expected RecencyWeight=0.5, got %f", config.RecencyWeight)
	}
	if config.RelevanceWeight != 0.5 {
		t.Errorf("expected RelevanceWeight=0.5, got %f", config.RelevanceWeight)
	}
	if !config.IncludeNegativeExamples {
		t.Error("expected IncludeNegativeExamples to be true by default")
	}
	if !config.EnforceLanguage {
		t.Error("expected EnforceLanguage to be true by default")
	}
}

func TestNewAnchoredSynthesisBuilder_NilConfig(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)
	if builder == nil {
		t.Fatal("expected non-nil builder")
	}
	if builder.config == nil {
		t.Error("expected non-nil config when nil passed")
	}
}

func TestScoreEvidence_RecencyScoring(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	toolResults := []agent.ToolResult{
		{InvocationID: "1", Output: "oldest result"},
		{InvocationID: "2", Output: "middle result"},
		{InvocationID: "3", Output: "newest result"},
	}

	scored := builder.ScoreEvidence(toolResults, "")

	// Verify recency scores increase with index
	// After sorting by total score (which includes recency), newest should be first
	if len(scored) != 3 {
		t.Fatalf("expected 3 scored results, got %d", len(scored))
	}

	// Find the original indices
	var recencyScores [3]float64
	for _, s := range scored {
		recencyScores[s.Index] = s.RecencyScore
	}

	// Index 0 (oldest) should have lowest recency
	if recencyScores[0] >= recencyScores[1] {
		t.Errorf("oldest result should have lower recency than middle: %f >= %f", recencyScores[0], recencyScores[1])
	}
	if recencyScores[1] >= recencyScores[2] {
		t.Errorf("middle result should have lower recency than newest: %f >= %f", recencyScores[1], recencyScores[2])
	}

	// Newest (index 2) should have recency close to 0.5 (RecencyWeight)
	if recencyScores[2] < 0.4 || recencyScores[2] > 0.5 {
		t.Errorf("newest result recency should be ~0.5, got %f", recencyScores[2])
	}

	// Oldest (index 0) should have recency close to 0.0
	if recencyScores[0] > 0.1 {
		t.Errorf("oldest result recency should be ~0.0, got %f", recencyScores[0])
	}
}

func TestScoreEvidence_SingleResult(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	toolResults := []agent.ToolResult{
		{InvocationID: "1", Output: "only result"},
	}

	scored := builder.ScoreEvidence(toolResults, "")

	if len(scored) != 1 {
		t.Fatalf("expected 1 scored result, got %d", len(scored))
	}

	// Single result should get full recency weight
	if scored[0].RecencyScore != 0.5 {
		t.Errorf("single result should have recency=0.5, got %f", scored[0].RecencyScore)
	}
}

func TestScoreEvidence_RelevanceScoring(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	toolResults := []agent.ToolResult{
		{InvocationID: "1", Output: "This file contains no relevant content at all."},
		{InvocationID: "2", Output: "The configuration file is at config/settings.go with database options."},
		{InvocationID: "3", Output: "error: file not found"},
	}

	scored := builder.ScoreEvidence(toolResults, "What configuration options are available?")

	// Find results by ID
	var relevanceByID = make(map[string]float64)
	for _, s := range scored {
		relevanceByID[s.InvocationID] = s.RelevanceScore
	}

	// Result 2 has "configuration" and file path - should have highest relevance
	if relevanceByID["2"] <= relevanceByID["1"] {
		t.Errorf("result with keywords and file path should have higher relevance: 2=%f, 1=%f",
			relevanceByID["2"], relevanceByID["1"])
	}

	// Result 3 is an error - should have low relevance
	if relevanceByID["3"] >= relevanceByID["2"] {
		t.Errorf("error result should have lower relevance: 3=%f, 2=%f",
			relevanceByID["3"], relevanceByID["2"])
	}
}

func TestScoreEvidence_CombinedScoring(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	toolResults := []agent.ToolResult{
		{InvocationID: "1", Output: "func main() { fmt.Println(\"config\") }"},           // Oldest, has keyword + code
		{InvocationID: "2", Output: "random text without any useful content"},            // Middle, no relevance
		{InvocationID: "3", Output: "The config is defined in cmd/main.go with options"}, // Newest, has keyword + path
	}

	scored := builder.ScoreEvidence(toolResults, "Where is the config defined?")

	// Results should be sorted by total score
	// Result 3 should likely be first (high recency + high relevance)
	// Result 1 might be second (low recency + high relevance)
	// Result 2 should be last (medium recency + low relevance)

	if len(scored) != 3 {
		t.Fatalf("expected 3 scored results, got %d", len(scored))
	}

	// Verify sorted in descending order
	for i := 1; i < len(scored); i++ {
		if scored[i].TotalScore > scored[i-1].TotalScore {
			t.Errorf("results not sorted by total score: %f > %f at position %d",
				scored[i].TotalScore, scored[i-1].TotalScore, i)
		}
	}

	// Result 3 should be first or have highest total score
	found := false
	for _, s := range scored[:2] { // Check top 2
		if s.InvocationID == "3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("newest result with keywords should be in top 2")
	}
}

func TestScoreEvidence_Empty(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	scored := builder.ScoreEvidence(nil, "question")
	if scored != nil {
		t.Errorf("expected nil for empty results, got %v", scored)
	}

	scored = builder.ScoreEvidence([]agent.ToolResult{}, "question")
	if scored != nil {
		t.Errorf("expected nil for empty slice, got %v", scored)
	}
}

func TestSelectTopEvidence_ContextBudget(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(&AnchoredSynthesisConfig{
		MaxEvidenceTokens:   100, // Very small budget for testing
		MaxResultLength:     200,
		MinResultsToInclude: 1,
		RecencyWeight:       0.5,
		RelevanceWeight:     0.5,
	})

	// Create scored evidence with known sizes
	scored := []ScoredEvidence{
		{Index: 0, InvocationID: "1", Output: strings.Repeat("a", 400), TotalScore: 1.0, EstimatedTokens: 100},
		{Index: 1, InvocationID: "2", Output: strings.Repeat("b", 400), TotalScore: 0.9, EstimatedTokens: 100},
		{Index: 2, InvocationID: "3", Output: strings.Repeat("c", 400), TotalScore: 0.8, EstimatedTokens: 100},
	}

	selected := builder.SelectTopEvidence(scored, 100)

	// Should include at least 1 (MinResultsToInclude) but not all 3
	if len(selected) < 1 {
		t.Error("should include at least MinResultsToInclude results")
	}

	// First selected should be highest scored
	if selected[0].InvocationID != "1" {
		t.Errorf("first selected should be highest scored, got %s", selected[0].InvocationID)
	}
}

func TestSelectTopEvidence_MinResultsGuaranteed(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(&AnchoredSynthesisConfig{
		MaxEvidenceTokens:   1, // Extremely small budget
		MaxResultLength:     200,
		MinResultsToInclude: 2, // But require at least 2
		RecencyWeight:       0.5,
		RelevanceWeight:     0.5,
	})

	scored := []ScoredEvidence{
		{Index: 0, InvocationID: "1", Output: strings.Repeat("a", 400), TotalScore: 1.0, EstimatedTokens: 100},
		{Index: 1, InvocationID: "2", Output: strings.Repeat("b", 400), TotalScore: 0.9, EstimatedTokens: 100},
	}

	selected := builder.SelectTopEvidence(scored, 1)

	// Should include minimum even if over budget
	if len(selected) < 2 {
		t.Errorf("should include at least MinResultsToInclude (2), got %d", len(selected))
	}
}

func TestSelectTopEvidence_Empty(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	selected := builder.SelectTopEvidence(nil, 1000)
	if selected != nil {
		t.Errorf("expected nil for empty input, got %v", selected)
	}
}

func TestBuildAnchoredSynthesisPrompt_Basic(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	assembledCtx := &agent.AssembledContext{
		ToolResults: []agent.ToolResult{
			{InvocationID: "1", Output: "Found main.go with config parsing"},
		},
		ConversationHistory: []agent.Message{
			{Role: "user", Content: "What config options exist?"},
		},
	}

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), assembledCtx, "What config options exist?", "go")

	// Check for key components
	if !strings.Contains(prompt, "PROJECT LANGUAGE: GO") {
		t.Error("prompt should contain language header")
	}
	if !strings.Contains(prompt, "TOOL EVIDENCE") {
		t.Error("prompt should contain evidence section")
	}
	if !strings.Contains(prompt, "INSTRUCTIONS") {
		t.Error("prompt should contain instructions")
	}
	if !strings.Contains(prompt, "DO NOT") {
		t.Error("prompt should contain negative examples")
	}
	if !strings.Contains(prompt, "main.go") {
		t.Error("prompt should contain the evidence content")
	}
}

func TestBuildAnchoredSynthesisPrompt_NegativeExamples_Go(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), nil, "question", "go")

	// Should include Go-specific negative examples
	if !strings.Contains(prompt, "Python patterns") {
		t.Error("Go project should warn against Python patterns")
	}
	if !strings.Contains(prompt, "JavaScript patterns") {
		t.Error("Go project should warn against JavaScript patterns")
	}
}

func TestBuildAnchoredSynthesisPrompt_NegativeExamples_Python(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), nil, "question", "python")

	// Should include Python-specific negative examples
	if !strings.Contains(prompt, "Go patterns") {
		t.Error("Python project should warn against Go patterns")
	}
}

func TestBuildAnchoredSynthesisPrompt_LanguageEnforcement(t *testing.T) {
	t.Run("with language", func(t *testing.T) {
		builder := NewAnchoredSynthesisBuilder(nil)
		prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), nil, "q", "rust")

		if !strings.Contains(prompt, "PROJECT LANGUAGE: RUST") {
			t.Error("prompt should contain uppercase language header")
		}
	})

	t.Run("without language", func(t *testing.T) {
		builder := NewAnchoredSynthesisBuilder(nil)
		prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), nil, "q", "")

		if strings.Contains(prompt, "PROJECT LANGUAGE:") {
			t.Error("prompt should not contain language header when language is empty")
		}
	})
}

func TestBuildAnchoredSynthesisPrompt_Disabled(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(&AnchoredSynthesisConfig{
		Enabled: false,
	})

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), nil, "q", "go")

	// Should return basic prompt
	if !strings.Contains(prompt, "Based on the tools you used") {
		t.Error("disabled builder should return basic prompt")
	}
	if strings.Contains(prompt, "DO NOT") {
		t.Error("disabled builder should not include negative examples")
	}
}

func TestBuildAnchoredSynthesisPrompt_NoEvidence(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	assembledCtx := &agent.AssembledContext{
		ToolResults: []agent.ToolResult{}, // Empty
	}

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), assembledCtx, "question", "go")

	if !strings.Contains(prompt, "No tool evidence available") {
		t.Error("prompt should note when no evidence is available")
	}
}

func TestBuildAnchoredSynthesisPrompt_EvidenceTruncation(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(&AnchoredSynthesisConfig{
		Enabled:                 true,
		MaxEvidenceTokens:       8000,
		MaxResultLength:         50, // Very short
		MinResultsToInclude:     1,
		RecencyWeight:           0.5,
		RelevanceWeight:         0.5,
		IncludeNegativeExamples: true,
		EnforceLanguage:         true,
	})

	assembledCtx := &agent.AssembledContext{
		ToolResults: []agent.ToolResult{
			{InvocationID: "1", Output: strings.Repeat("x", 100)}, // Will be truncated
		},
	}

	prompt := builder.BuildAnchoredSynthesisPrompt(context.Background(), assembledCtx, "q", "go")

	if !strings.Contains(prompt, "[truncated]") {
		t.Error("long evidence should be truncated")
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		question string
		wantLen  int
		contains []string
	}{
		{
			question: "What configuration options are available?",
			wantLen:  2, // "configuration", "options" (available is filtered as common)
			contains: []string{"configuration", "options"},
		},
		{
			question: "Where is the main function?",
			wantLen:  2, // "main", "function"
			contains: []string{"main", "function"},
		},
		{
			question: "How does it work?",
			wantLen:  1, // "work"
			contains: []string{"work"},
		},
		{
			question: "a b",
			wantLen:  0, // Too short
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.question, func(t *testing.T) {
			keywords := extractKeywords(tt.question)

			if len(keywords) < tt.wantLen {
				t.Errorf("expected at least %d keywords, got %d: %v", tt.wantLen, len(keywords), keywords)
			}

			for _, want := range tt.contains {
				found := false
				for _, kw := range keywords {
					if kw == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected keyword %q in %v", want, keywords)
				}
			}
		})
	}
}

func TestContainsFilePath(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"Found config in main.go", true},
		{"The file path/to/file.py exists", true},
		{"See config.json for details", true},
		{"No file paths here", false},
		{"Just plain text", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			got := containsFilePath(tt.output)
			if got != tt.want {
				t.Errorf("containsFilePath(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestContainsCodeSnippet(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"func main() { }", true},
		{"def hello():", true},
		{"class MyClass:", true},
		{"import os", true},
		{"Just a description", false},
		{"No code here", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			got := containsCodeSnippet(tt.output)
			if got != tt.want {
				t.Errorf("containsCodeSnippet(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestExtractUserQuestion(t *testing.T) {
	t.Run("first user message", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			ConversationHistory: []agent.Message{
				{Role: "user", Content: "What is this?"},
				{Role: "assistant", Content: "Let me check."},
				{Role: "user", Content: "Thanks"},
			},
		}

		question := ExtractUserQuestion(ctx)
		if question != "What is this?" {
			t.Errorf("expected first user message, got %q", question)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		question := ExtractUserQuestion(nil)
		if question != "" {
			t.Errorf("expected empty for nil context, got %q", question)
		}
	})

	t.Run("no user messages", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			ConversationHistory: []agent.Message{
				{Role: "assistant", Content: "Hello"},
			},
		}

		question := ExtractUserQuestion(ctx)
		if question != "" {
			t.Errorf("expected empty for no user messages, got %q", question)
		}
	})

	t.Run("empty history", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			ConversationHistory: []agent.Message{},
		}

		question := ExtractUserQuestion(ctx)
		if question != "" {
			t.Errorf("expected empty for empty history, got %q", question)
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"", 0},
		{"test", 1},        // 4 chars
		{"hello world", 2}, // 11 chars -> 2 tokens
		{strings.Repeat("a", 100), 25},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := estimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestGetNegativeExamples(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	tests := []struct {
		lang     string
		mustHave string
	}{
		{"go", "Python patterns"},
		{"python", "Go patterns"},
		{"javascript", "Python patterns"},
		{"typescript", "Go patterns"},
		{"unknown", "Invent files"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			examples := builder.getNegativeExamples(tt.lang)

			found := false
			for _, ex := range examples {
				if strings.Contains(ex, tt.mustHave) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in negative examples for %s: %v", tt.mustHave, tt.lang, examples)
			}
		})
	}
}

func TestTruncateOutput(t *testing.T) {
	builder := NewAnchoredSynthesisBuilder(nil)

	t.Run("short output unchanged", func(t *testing.T) {
		output := "short"
		got := builder.truncateOutput(output, 100)
		if got != output {
			t.Errorf("short output should be unchanged: %q", got)
		}
	})

	t.Run("long output truncated", func(t *testing.T) {
		output := strings.Repeat("x", 100)
		got := builder.truncateOutput(output, 50)

		if len(got) > 70 { // 50 + "[truncated]" indicator
			t.Errorf("output should be truncated, got length %d", len(got))
		}
		if !strings.Contains(got, "[truncated]") {
			t.Error("truncated output should contain indicator")
		}
	})
}
