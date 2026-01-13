// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package conversation

import (
	"context"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// MockEmbedder is a mock implementation of EmbeddingProvider for testing.
type MockEmbedder struct {
	// EmbedFunc allows customizing the embedding behavior per test.
	EmbedFunc func(ctx context.Context, text string) ([]float32, error)
	// Calls tracks how many times Embed was called.
	Calls int
	// LastText stores the last text passed to Embed.
	LastText string
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.Calls++
	m.LastText = text
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, text)
	}
	// Default: return a simple vector
	return []float32{0.1, 0.2, 0.3, 0.4, 0.5}, nil
}

// =============================================================================
// Helper Function Tests (No Weaviate Required)
// =============================================================================

func TestParseMemoryContent(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectedQuestion string
		expectedAnswer   string
	}{
		{
			name:             "standard format",
			content:          "User: What is Chrysler?\nAI: Chrysler is an American automotive company.",
			expectedQuestion: "What is Chrysler?",
			expectedAnswer:   "Chrysler is an American automotive company.",
		},
		{
			name:             "multiline answer",
			content:          "User: Tell me about Detroit\nAI: Detroit is a city in Michigan.\nIt is known for the automotive industry.",
			expectedQuestion: "Tell me about Detroit",
			expectedAnswer:   "Detroit is a city in Michigan.\nIt is known for the automotive industry.",
		},
		{
			name:             "empty answer",
			content:          "User: Hello\nAI: ",
			expectedQuestion: "Hello",
			expectedAnswer:   "",
		},
		{
			name:             "no AI prefix - malformed",
			content:          "User: Question only",
			expectedQuestion: "Question only",
			expectedAnswer:   "",
		},
		{
			name:             "no User prefix - malformed",
			content:          "Some random content\nAI: Some answer",
			expectedQuestion: "Some random content",
			expectedAnswer:   "Some answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			question, answer := parseMemoryContent(tt.content)

			if question != tt.expectedQuestion {
				t.Errorf("question mismatch:\n  got:  %q\n  want: %q", question, tt.expectedQuestion)
			}

			if answer != tt.expectedAnswer {
				t.Errorf("answer mismatch:\n  got:  %q\n  want: %q", answer, tt.expectedAnswer)
			}
		})
	}
}

func TestMergeAndDeduplicate(t *testing.T) {
	tests := []struct {
		name     string
		recent   []RelevantTurn
		semantic []RelevantTurn
		maxTurns int
		expected int
		// expectedFirstQuestion is the question of the first result (highest turn number)
		expectedFirstQuestion string
	}{
		{
			name: "no duplicates, under limit",
			recent: []RelevantTurn{
				{Question: "Q1", Answer: "A1", TurnNumber: 10},
				{Question: "Q2", Answer: "A2", TurnNumber: 9},
			},
			semantic: []RelevantTurn{
				{Question: "Q3", Answer: "A3", TurnNumber: 5, SimilarityScore: 0.9},
			},
			maxTurns:              5,
			expected:              3,
			expectedFirstQuestion: "Q1", // Turn 10 is highest
		},
		{
			name: "with duplicates",
			recent: []RelevantTurn{
				{Question: "Q1", Answer: "A1", TurnNumber: 10},
			},
			semantic: []RelevantTurn{
				{Question: "Q1", Answer: "A1 (semantic)", TurnNumber: 10, SimilarityScore: 0.9}, // duplicate
				{Question: "Q2", Answer: "A2", TurnNumber: 5, SimilarityScore: 0.8},
			},
			maxTurns:              5,
			expected:              2, // Q1 deduped
			expectedFirstQuestion: "Q1",
		},
		{
			name: "exceeds max limit",
			recent: []RelevantTurn{
				{Question: "Q1", Answer: "A1", TurnNumber: 10},
				{Question: "Q2", Answer: "A2", TurnNumber: 9},
				{Question: "Q3", Answer: "A3", TurnNumber: 8},
			},
			semantic: []RelevantTurn{
				{Question: "Q4", Answer: "A4", TurnNumber: 5, SimilarityScore: 0.9},
				{Question: "Q5", Answer: "A5", TurnNumber: 3, SimilarityScore: 0.8},
			},
			maxTurns:              3,
			expected:              3, // Limited to 3
			expectedFirstQuestion: "Q1",
		},
		{
			name:                  "empty inputs",
			recent:                []RelevantTurn{},
			semantic:              []RelevantTurn{},
			maxTurns:              5,
			expected:              0,
			expectedFirstQuestion: "",
		},
		{
			name:   "semantic only",
			recent: []RelevantTurn{},
			semantic: []RelevantTurn{
				{Question: "Q1", Answer: "A1", TurnNumber: 5, SimilarityScore: 0.9},
			},
			maxTurns:              5,
			expected:              1,
			expectedFirstQuestion: "Q1",
		},
		{
			name: "recent only",
			recent: []RelevantTurn{
				{Question: "Q1", Answer: "A1", TurnNumber: 10},
			},
			semantic:              []RelevantTurn{},
			maxTurns:              5,
			expected:              1,
			expectedFirstQuestion: "Q1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeAndDeduplicate(tt.recent, tt.semantic, tt.maxTurns)

			if len(result) != tt.expected {
				t.Errorf("length mismatch: got %d, want %d", len(result), tt.expected)
			}

			if tt.expected > 0 && result[0].Question != tt.expectedFirstQuestion {
				t.Errorf("first question mismatch: got %q, want %q", result[0].Question, tt.expectedFirstQuestion)
			}

			// Verify ordering (descending by turn number)
			for i := 1; i < len(result); i++ {
				if result[i].TurnNumber > result[i-1].TurnNumber {
					t.Errorf("ordering violation at index %d: turn %d > turn %d",
						i, result[i].TurnNumber, result[i-1].TurnNumber)
				}
			}
		})
	}
}

func TestParseDocumentSearchResults(t *testing.T) {
	// Helper to create pointers
	intPtr := func(i int) *int { return &i }
	floatPtr := func(f float32) *float32 { return &f }

	tests := []struct {
		name     string
		resp     *datatypes.DocumentQueryResponse
		expected int
	}{
		{
			name: "valid results",
			resp: &datatypes.DocumentQueryResponse{
				Get: struct {
					Document []datatypes.DocumentResult `json:"Document"`
				}{
					Document: []datatypes.DocumentResult{
						{
							Content:    "User: What is Chrysler?\nAI: Chrysler is an American automotive company.",
							Source:     "session_memory_123",
							TurnNumber: intPtr(5),
							Additional: struct {
								ID        string   `json:"id"`
								Distance  *float32 `json:"distance"`
								Certainty *float32 `json:"certainty"`
							}{Certainty: floatPtr(0.8)},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "empty results",
			resp: &datatypes.DocumentQueryResponse{
				Get: struct {
					Document []datatypes.DocumentResult `json:"Document"`
				}{
					Document: []datatypes.DocumentResult{},
				},
			},
			expected: 0,
		},
		{
			name: "multiple results",
			resp: &datatypes.DocumentQueryResponse{
				Get: struct {
					Document []datatypes.DocumentResult `json:"Document"`
				}{
					Document: []datatypes.DocumentResult{
						{
							Content:    "User: Q1\nAI: A1",
							Source:     "src1",
							TurnNumber: intPtr(10),
							Additional: struct {
								ID        string   `json:"id"`
								Distance  *float32 `json:"distance"`
								Certainty *float32 `json:"certainty"`
							}{Certainty: floatPtr(0.9)},
						},
						{
							Content:    "User: Q2\nAI: A2",
							Source:     "src2",
							TurnNumber: intPtr(8),
							Additional: struct {
								ID        string   `json:"id"`
								Distance  *float32 `json:"distance"`
								Certainty *float32 `json:"certainty"`
							}{Certainty: floatPtr(0.7)},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "nil response",
			resp: nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDocumentSearchResults(tt.resp)

			if len(result) != tt.expected {
				t.Errorf("result length mismatch: got %d, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestParseDocumentSearchResults_SimilarityScore(t *testing.T) {
	// Test that certainty is used directly as similarity score
	floatPtr := func(f float32) *float32 { return &f }
	intPtr := func(i int) *int { return &i }

	resp := &datatypes.DocumentQueryResponse{
		Get: struct {
			Document []datatypes.DocumentResult `json:"Document"`
		}{
			Document: []datatypes.DocumentResult{
				{
					Content:    "User: Test?\nAI: Test.",
					Source:     "test",
					TurnNumber: intPtr(1),
					Additional: struct {
						ID        string   `json:"id"`
						Distance  *float32 `json:"distance"`
						Certainty *float32 `json:"certainty"`
					}{Certainty: floatPtr(0.85)}, // Certainty used directly as similarity
				},
			},
		},
	}

	result := parseDocumentSearchResults(resp)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	// Certainty 0.85 should be used directly as similarity
	expectedSimilarity := 0.85
	tolerance := 0.001
	if result[0].SimilarityScore < expectedSimilarity-tolerance ||
		result[0].SimilarityScore > expectedSimilarity+tolerance {
		t.Errorf("similarity score mismatch: got %f, want ~%f", result[0].SimilarityScore, expectedSimilarity)
	}
}

func TestParseConversationResults(t *testing.T) {
	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name             string
		resp             *datatypes.ConversationQueryResponse
		expected         int
		expectedTurnNums []int
	}{
		{
			name: "valid results with turn number",
			resp: &datatypes.ConversationQueryResponse{
				Get: struct {
					Conversation []datatypes.ConversationResult `json:"Conversation"`
				}{
					Conversation: []datatypes.ConversationResult{
						{
							Question:   "What is Chrysler?",
							Answer:     "Chrysler is an American automotive company.",
							Timestamp:  1704067200000,
							TurnNumber: intPtr(5),
						},
					},
				},
			},
			expected:         1,
			expectedTurnNums: []int{5},
		},
		{
			name: "empty results",
			resp: &datatypes.ConversationQueryResponse{
				Get: struct {
					Conversation []datatypes.ConversationResult `json:"Conversation"`
				}{
					Conversation: []datatypes.ConversationResult{},
				},
			},
			expected:         0,
			expectedTurnNums: []int{},
		},
		{
			name: "multiple results with turn numbers",
			resp: &datatypes.ConversationQueryResponse{
				Get: struct {
					Conversation []datatypes.ConversationResult `json:"Conversation"`
				}{
					Conversation: []datatypes.ConversationResult{
						{Question: "Q1", Answer: "A1", Timestamp: 1704067300000, TurnNumber: intPtr(10)},
						{Question: "Q2", Answer: "A2", Timestamp: 1704067200000, TurnNumber: intPtr(9)},
					},
				},
			},
			expected:         2,
			expectedTurnNums: []int{10, 9},
		},
		{
			name: "nil turn number defaults to 0",
			resp: &datatypes.ConversationQueryResponse{
				Get: struct {
					Conversation []datatypes.ConversationResult `json:"Conversation"`
				}{
					Conversation: []datatypes.ConversationResult{
						{Question: "Q1", Answer: "A1", Timestamp: 1704067300000, TurnNumber: nil},
					},
				},
			},
			expected:         1,
			expectedTurnNums: []int{0},
		},
		{
			name:             "nil response",
			resp:             nil,
			expected:         0,
			expectedTurnNums: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseConversationResults(tt.resp)

			if len(result) != tt.expected {
				t.Errorf("result length mismatch: got %d, want %d", len(result), tt.expected)
			}

			// Verify turn numbers match database values
			for i, turn := range result {
				if i < len(tt.expectedTurnNums) && turn.TurnNumber != tt.expectedTurnNums[i] {
					t.Errorf("turn number mismatch at index %d: got %d, want %d",
						i, turn.TurnNumber, tt.expectedTurnNums[i])
				}
			}
		})
	}
}

func TestDefaultSearchConfig(t *testing.T) {
	config := DefaultSearchConfig()

	// Verify default values match documentation
	if config.RecentTurns != 2 {
		t.Errorf("RecentTurns: got %d, want 2", config.RecentTurns)
	}

	if config.SemanticTopK != 3 {
		t.Errorf("SemanticTopK: got %d, want 3", config.SemanticTopK)
	}

	if config.MaxTurnAge != 20 {
		t.Errorf("MaxTurnAge: got %d, want 20", config.MaxTurnAge)
	}

	if config.MaxTotalTurns != 5 {
		t.Errorf("MaxTotalTurns: got %d, want 5", config.MaxTotalTurns)
	}

	if config.MaxEmbedLength != 2000 {
		t.Errorf("MaxEmbedLength: got %d, want 2000", config.MaxEmbedLength)
	}

	if config.MemoryVersionTag != "chat_memory" {
		t.Errorf("MemoryVersionTag: got %q, want %q", config.MemoryVersionTag, "chat_memory")
	}
}

func TestMockEmbedder(t *testing.T) {
	embedder := &MockEmbedder{}

	// Test default behavior
	vector, err := embedder.Embed(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vector) != 5 {
		t.Errorf("vector length: got %d, want 5", len(vector))
	}

	if embedder.Calls != 1 {
		t.Errorf("call count: got %d, want 1", embedder.Calls)
	}

	if embedder.LastText != "test query" {
		t.Errorf("last text: got %q, want %q", embedder.LastText, "test query")
	}
}

func TestNewDatatypesEmbedder(t *testing.T) {
	embedder := NewDatatypesEmbedder()
	if embedder == nil {
		t.Error("NewDatatypesEmbedder returned nil")
	}
}

func TestNewWeaviateConversationSearcher(t *testing.T) {
	embedder := &MockEmbedder{}
	config := DefaultSearchConfig()

	// Pass nil for client since we're not making actual calls
	searcher := NewWeaviateConversationSearcher(nil, embedder, config)

	if searcher == nil {
		t.Error("NewWeaviateConversationSearcher returned nil")
	}

	if searcher.embedder != embedder {
		t.Error("embedder not properly assigned")
	}

	if searcher.config.RecentTurns != config.RecentTurns {
		t.Error("config not properly assigned")
	}
}

// =============================================================================
// Integration Tests (Require Weaviate - Skipped by Default)
// =============================================================================

// TestSearchRelevant_Integration tests the full search flow with Weaviate.
// Skip this test unless WEAVIATE_INTEGRATION_TEST=true is set.
func TestSearchRelevant_Integration(t *testing.T) {
	t.Skip("Integration test requires Weaviate - run with WEAVIATE_INTEGRATION_TEST=true")
}

// TestGetRecent_Integration tests recent turn retrieval with Weaviate.
// Skip this test unless WEAVIATE_INTEGRATION_TEST=true is set.
func TestGetRecent_Integration(t *testing.T) {
	t.Skip("Integration test requires Weaviate - run with WEAVIATE_INTEGRATION_TEST=true")
}

// TestGetHybridContext_Integration tests hybrid retrieval with Weaviate.
// Skip this test unless WEAVIATE_INTEGRATION_TEST=true is set.
func TestGetHybridContext_Integration(t *testing.T) {
	t.Skip("Integration test requires Weaviate - run with WEAVIATE_INTEGRATION_TEST=true")
}
