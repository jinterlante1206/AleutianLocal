package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// BuildContextualQuery Tests
// =============================================================================

func TestBuildContextualQuery_NoHistory(t *testing.T) {
	embedder := NewContextualEmbedder(nil, DefaultContextConfig())

	result := embedder.BuildContextualQuery(context.Background(), "tell me more", nil)

	// Should return query repeated twice
	if result != "tell me more | tell me more" {
		t.Errorf("Expected 'tell me more | tell me more', got %q", result)
	}
}

func TestBuildContextualQuery_WithHistory_Truncation(t *testing.T) {
	config := DefaultContextConfig()
	config.SummarizationEnabled = false // Force truncation mode
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "What is Motown?", Answer: "Motown was a record label founded in Detroit."},
	}

	result := embedder.BuildContextualQuery(context.Background(), "tell me more", history)

	// Should contain query twice and context
	if !strings.Contains(result, "tell me more | tell me more") {
		t.Error("Result should contain query repeated twice")
	}
	if !strings.Contains(result, "Context:") {
		t.Error("Result should contain 'Context:'")
	}
	if !strings.Contains(result, "Motown") {
		t.Error("Result should contain history content")
	}
}

func TestBuildContextualQuery_WithHistory_Summarization(t *testing.T) {
	generateFunc, callCount, _ := mockGenerator("User is asking about Motown Records history", nil)

	config := DefaultContextConfig()
	config.SummarizationEnabled = true
	embedder := NewContextualEmbedder(generateFunc, config)

	history := []RelevantTurn{
		{Question: "What is Motown?", Answer: "Motown was a record label..."},
	}

	result := embedder.BuildContextualQuery(context.Background(), "tell me more", history)

	if !strings.Contains(result, "tell me more | tell me more") {
		t.Error("Result should contain query repeated twice")
	}
	if !strings.Contains(result, "Motown Records history") {
		t.Error("Result should contain summarized context")
	}
	if *callCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", *callCount)
	}
}

func TestBuildContextualQuery_Disabled(t *testing.T) {
	config := DefaultContextConfig()
	config.Enabled = false
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	result := embedder.BuildContextualQuery(context.Background(), "tell me more", history)

	// Should return query unchanged when disabled
	if result != "tell me more" {
		t.Errorf("Expected unchanged query, got %q", result)
	}
}

func TestBuildContextualQuery_LimitsHistory(t *testing.T) {
	config := DefaultContextConfig()
	config.SummarizationEnabled = false
	config.MaxTurns = 2
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "Q1", Answer: "A1"},
		{Question: "Q2", Answer: "A2"},
		{Question: "Q3", Answer: "A3"},
		{Question: "Q4", Answer: "A4"},
	}

	result := embedder.BuildContextualQuery(context.Background(), "query", history)

	// Should only include first 2 turns
	if strings.Contains(result, "Q3") || strings.Contains(result, "Q4") {
		t.Error("Should only include MaxTurns history items")
	}
	if !strings.Contains(result, "Q1") || !strings.Contains(result, "Q2") {
		t.Error("Should include first MaxTurns history items")
	}
}

func TestBuildContextualQuery_EnforcesMaxChars(t *testing.T) {
	config := DefaultContextConfig()
	config.SummarizationEnabled = false
	config.MaxChars = 100 // Very small limit
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "This is a very long question about many topics", Answer: "This is an extremely long answer that goes on and on about various subjects in great detail"},
	}

	result := embedder.BuildContextualQuery(context.Background(), "short query", history)

	if len(result) > config.MaxChars {
		t.Errorf("Result length %d exceeds MaxChars %d", len(result), config.MaxChars)
	}
}

func TestBuildContextualQuery_FallbackOnSummarizationError(t *testing.T) {
	generateFunc, _, _ := mockGenerator("", errors.New("LLM unavailable"))

	config := DefaultContextConfig()
	config.SummarizationEnabled = true
	embedder := NewContextualEmbedder(generateFunc, config)

	history := []RelevantTurn{
		{Question: "What is Motown?", Answer: "Motown was founded in Detroit."},
	}

	result := embedder.BuildContextualQuery(context.Background(), "tell me more", history)

	// Should still produce output using truncation fallback
	if !strings.Contains(result, "tell me more") {
		t.Error("Should contain query")
	}
	if !strings.Contains(result, "Motown") {
		t.Error("Should contain truncated history as fallback")
	}
}

// =============================================================================
// SummarizeContext Tests
// =============================================================================

func TestSummarizeContext_Success(t *testing.T) {
	generateFunc, _, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		// Verify prompt structure
		if !strings.Contains(prompt, "Summarize this conversation") {
			t.Error("Prompt should contain summarization instruction")
		}
		return "User is asking about Berry Gordy and Motown", nil
	})

	config := DefaultContextConfig()
	embedder := NewContextualEmbedder(generateFunc, config)

	history := []RelevantTurn{
		{Question: "Who founded Motown?", Answer: "Berry Gordy founded Motown in 1959..."},
	}

	summary, err := embedder.SummarizeContext(context.Background(), history)
	if err != nil {
		t.Fatalf("SummarizeContext failed: %v", err)
	}

	if !strings.Contains(summary, "Berry Gordy") {
		t.Errorf("Summary should mention Berry Gordy, got %q", summary)
	}
}

func TestSummarizeContext_NoGenerateFunc(t *testing.T) {
	embedder := NewContextualEmbedder(nil, DefaultContextConfig())

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	_, err := embedder.SummarizeContext(context.Background(), history)
	if err == nil {
		t.Error("Expected error when LLM client is nil")
	}
}

func TestSummarizeContext_EmptyHistory(t *testing.T) {
	generateFunc, callCount, _ := mockGenerator("", nil)
	embedder := NewContextualEmbedder(generateFunc, DefaultContextConfig())

	summary, err := embedder.SummarizeContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("SummarizeContext failed: %v", err)
	}

	if summary != "" {
		t.Errorf("Expected empty summary, got %q", summary)
	}

	if *callCount != 0 {
		t.Error("Should not call LLM with empty history")
	}
}

func TestSummarizeContext_Timeout(t *testing.T) {
	generateFunc, _, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return "summary", nil
		}
	})

	config := DefaultContextConfig()
	config.SummarizationTimeoutMs = 100 // 100ms timeout
	embedder := NewContextualEmbedder(generateFunc, config)

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	_, err := embedder.SummarizeContext(context.Background(), history)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestSummarizeContext_CleansResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "removes Summary prefix",
			response: "Summary: User is asking about X",
			expected: "is asking about X",
		},
		{
			name:     "removes Context prefix",
			response: "Context: The conversation is about Y",
			expected: "The conversation is about Y",
		},
		{
			name:     "trims whitespace",
			response: "  User wants to know about Z  ",
			expected: "wants to know about Z",
		},
		{
			name:     "clean response unchanged",
			response: "Asking about quantum physics",
			expected: "Asking about quantum physics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generateFunc, _, _ := mockGenerator(tt.response, nil)

			embedder := NewContextualEmbedder(generateFunc, DefaultContextConfig())
			history := []RelevantTurn{{Question: "Q", Answer: "A"}}

			summary, err := embedder.SummarizeContext(context.Background(), history)
			if err != nil {
				t.Fatalf("SummarizeContext failed: %v", err)
			}

			if summary != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, summary)
			}
		})
	}
}

// =============================================================================
// truncateHistory Tests
// =============================================================================

func TestTruncateHistory(t *testing.T) {
	config := DefaultContextConfig()
	config.AnswerLimit = 50
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "Short Q1", Answer: "Short A1"},
		{Question: "Short Q2", Answer: "Short A2"},
	}

	result := embedder.truncateHistory(history)

	// Should contain both Q&A pairs
	if !strings.Contains(result, "Q: Short Q1") {
		t.Error("Should contain Q1")
	}
	if !strings.Contains(result, "A: Short A1") {
		t.Error("Should contain A1")
	}
	if !strings.Contains(result, "|") {
		t.Error("Should separate turns with |")
	}
}

func TestTruncateHistory_LongAnswers(t *testing.T) {
	config := DefaultContextConfig()
	config.AnswerLimit = 20
	embedder := NewContextualEmbedder(nil, config)

	history := []RelevantTurn{
		{Question: "Q", Answer: "This is a very long answer that exceeds the limit"},
	}

	result := embedder.truncateHistory(history)

	// Answer should be truncated
	if strings.Contains(result, "exceeds the limit") {
		t.Error("Answer should be truncated")
	}
	if !strings.Contains(result, "...") {
		t.Error("Truncated text should end with ...")
	}
}

// =============================================================================
// NewContextualEmbedder Tests
// =============================================================================

func TestNewContextualEmbedder_NilClient(t *testing.T) {
	config := DefaultContextConfig()
	config.SummarizationEnabled = true

	embedder := NewContextualEmbedder(nil, config)

	// SummarizationEnabled should be forced to false
	if embedder.config.SummarizationEnabled {
		t.Error("SummarizationEnabled should be false when client is nil")
	}
}

func TestNewContextualEmbedder_WithGenerateFunc(t *testing.T) {
	generateFunc, _, _ := mockGenerator("", nil)
	config := DefaultContextConfig()
	config.SummarizationEnabled = true

	embedder := NewContextualEmbedder(generateFunc, config)

	// SummarizationEnabled should remain true
	if !embedder.config.SummarizationEnabled {
		t.Error("SummarizationEnabled should remain true when generate function is provided")
	}
}
