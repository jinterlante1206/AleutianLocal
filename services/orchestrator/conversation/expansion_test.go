package conversation

import (
	"context"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// Test Helpers
// =============================================================================

// mockGenerator creates a GenerateFunc with call tracking for tests.
// Returns the function and pointers to track callCount and lastPrompt.
func mockGenerator(response string, err error) (GenerateFunc, *int, *string) {
	callCount := 0
	lastPrompt := ""
	fn := func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		callCount++
		lastPrompt = prompt
		return response, err
	}
	return fn, &callCount, &lastPrompt
}

// mockGeneratorWithFunc creates a GenerateFunc that delegates to a custom function.
func mockGeneratorWithFunc(fn func(ctx context.Context, prompt string, maxTokens int) (string, error)) (GenerateFunc, *int, *string) {
	callCount := 0
	lastPrompt := ""
	wrapper := func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		callCount++
		lastPrompt = prompt
		return fn(ctx, prompt, maxTokens)
	}
	return wrapper, &callCount, &lastPrompt
}

// =============================================================================
// NeedsExpansion Tests
// =============================================================================

func TestNeedsExpansion_ShortQuery(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"tell me more", true},
		{"more", true},
		{"why", true},
		{"yes", true},
		{"and?", true},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.NeedsExpansion(tt.query); got != tt.expected {
				t.Errorf("NeedsExpansion(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestNeedsExpansion_Pronouns(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"when did he start the company", true},
		{"what did she accomplish", true},
		{"tell me about it", true},
		{"what happened to them after that", true},
		{"is this related to the previous topic", true},
		{"can you explain that more", true},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.NeedsExpansion(tt.query); got != tt.expected {
				t.Errorf("NeedsExpansion(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestNeedsExpansion_CompleteQuery(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"What is the capital of France?", false},
		{"Explain quantum entanglement in simple terms", false},
		{"How do neural networks learn from data?", false},
		{"What are the main causes of climate change?", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.NeedsExpansion(tt.query); got != tt.expected {
				t.Errorf("NeedsExpansion(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestNeedsExpansion_StopList(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"stop", false},
		{"clear", false},
		{"help", false},
		{"reset", false},
		{"quit", false},
		{"exit", false},
		{"help me understand this", false}, // starts with command
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.NeedsExpansion(tt.query); got != tt.expected {
				t.Errorf("NeedsExpansion(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestNeedsExpansion_TopicSwitch(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"Switching gears, tell me about quantum physics", false},
		{"Different topic - what about the economy?", false},
		{"Unrelated, but can you help with cooking?", false},
		{"Moving on, let's discuss something else", false},
		{"By the way, what time is it?", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.NeedsExpansion(tt.query); got != tt.expected {
				t.Errorf("NeedsExpansion(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

func TestNeedsExpansion_Disabled(t *testing.T) {
	config := DefaultExpansionConfig()
	config.Enabled = false
	expander := NewLLMQueryExpander(nil, config)

	// Even queries that would normally need expansion should return false
	if expander.NeedsExpansion("tell me more") {
		t.Error("NeedsExpansion should return false when disabled")
	}
}

// =============================================================================
// DetectsTopicSwitch Tests
// =============================================================================

func TestDetectsTopicSwitch(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	tests := []struct {
		query    string
		expected bool
	}{
		{"Switching gears, what about X?", true},
		{"Different topic now", true},
		{"This is unrelated but important", true},
		{"Moving on to something else", true},
		{"By the way, can you help?", true},
		{"On another note, what is Y?", true},
		{"Tell me more about Motown", false},
		{"What else can you tell me?", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := expander.DetectsTopicSwitch(tt.query); got != tt.expected {
				t.Errorf("DetectsTopicSwitch(%q) = %v, want %v", tt.query, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// NeedsExpansionWithContext Tests
// =============================================================================

func TestNeedsExpansionWithContext_SimilarToPrevious(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	// Create two similar vectors (>0.85 similarity)
	prevVector := []float32{1.0, 0.0, 0.0}
	currVector := []float32{0.99, 0.1, 0.0} // Very similar to prev

	needsExpand, isSwitch := expander.NeedsExpansionWithContext("what?", prevVector, currVector)

	if needsExpand {
		t.Error("Should not need expansion when query is too similar to previous")
	}
	if isSwitch {
		t.Error("Should not be a topic switch")
	}
}

func TestNeedsExpansionWithContext_TopicSwitch(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	needsExpand, isSwitch := expander.NeedsExpansionWithContext(
		"Switching gears, tell me about physics",
		nil, nil,
	)

	if needsExpand {
		t.Error("Should not need expansion on topic switch")
	}
	if !isSwitch {
		t.Error("Should detect topic switch")
	}
}

func TestNeedsExpansionWithContext_NilVectors(t *testing.T) {
	expander := NewLLMQueryExpander(nil, DefaultExpansionConfig())

	// With nil vectors, should fall back to basic heuristics
	needsExpand, isSwitch := expander.NeedsExpansionWithContext("tell me more", nil, nil)

	if !needsExpand {
		t.Error("Should need expansion for short ambiguous query")
	}
	if isSwitch {
		t.Error("Should not be a topic switch")
	}
}

// =============================================================================
// Expand Tests
// =============================================================================

func TestExpand_TellMeMore(t *testing.T) {
	generateFunc, callCount, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		return `{"queries": ["History of Motown Records founding", "Motown artists and influence", "Berry Gordy Detroit music"]}`, nil
	})

	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	history := []RelevantTurn{
		{Question: "What is Motown?", Answer: "Motown was a record label founded by Berry Gordy in Detroit..."},
	}

	expanded, err := expander.Expand(context.Background(), "tell me more", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if !expanded.Expanded {
		t.Error("Expected Expanded to be true")
	}

	if expanded.Original != "tell me more" {
		t.Errorf("Original = %q, want %q", expanded.Original, "tell me more")
	}

	if len(expanded.Queries) != 3 {
		t.Errorf("Expected 3 queries, got %d", len(expanded.Queries))
	}

	if *callCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", *callCount)
	}
}

func TestExpand_NoHistory(t *testing.T) {
	generateFunc, callCount, _ := mockGenerator("", nil)
	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	expanded, err := expander.Expand(context.Background(), "tell me more", nil)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if expanded.Expanded {
		t.Error("Expected Expanded to be false with no history")
	}

	if len(expanded.Queries) != 1 || expanded.Queries[0] != "tell me more" {
		t.Error("Expected single query matching original")
	}

	if *callCount != 0 {
		t.Error("Should not call LLM with no history")
	}
}

func TestExpand_LLMError(t *testing.T) {
	generateFunc, _, _ := mockGenerator("", errors.New("LLM unavailable"))

	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	_, err := expander.Expand(context.Background(), "tell me more", history)
	if err == nil {
		t.Error("Expected error when LLM fails")
	}
}

func TestExpand_Timeout(t *testing.T) {
	generateFunc, _, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		// Simulate slow LLM
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return `{"queries": ["query1", "query2", "query3"]}`, nil
		}
	})

	config := DefaultExpansionConfig()
	config.TimeoutMs = 100 // 100ms timeout
	expander := NewLLMQueryExpander(generateFunc, config)

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	_, err := expander.Expand(context.Background(), "tell me more", history)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestExpand_InvalidJSON(t *testing.T) {
	generateFunc, _, _ := mockGenerator("This is not valid JSON at all", nil)

	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	expanded, err := expander.Expand(context.Background(), "tell me more", history)

	// Should return error but also a fallback ExpandedQuery
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	if expanded.Expanded {
		t.Error("Expanded should be false on parse failure")
	}

	if len(expanded.Queries) != 1 || expanded.Queries[0] != "tell me more" {
		t.Error("Should fall back to original query")
	}
}

func TestExpand_JSONWithExtraText(t *testing.T) {
	generateFunc, _, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		// LLM sometimes adds extra text before/after JSON
		return `Here are the queries:
{"queries": ["query about history", "query about people", "query about context"]}
I hope these help!`, nil
	})

	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	history := []RelevantTurn{
		{Question: "What is X?", Answer: "X is..."},
	}

	expanded, err := expander.Expand(context.Background(), "tell me more", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if !expanded.Expanded {
		t.Error("Should successfully parse JSON embedded in text")
	}

	if len(expanded.Queries) != 3 {
		t.Errorf("Expected 3 queries, got %d", len(expanded.Queries))
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float64
		delta    float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
			delta:    0.001,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "different lengths",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
			delta:    0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if got < tt.expected-tt.delta || got > tt.expected+tt.delta {
				t.Errorf("cosineSimilarity() = %v, want %v (±%v)", got, tt.expected, tt.delta)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"this is a longer string", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := truncateString(tt.input, tt.maxLen); got != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Integration-style Tests (still mocked, but testing full flow)
// =============================================================================

func TestExpand_PronounResolution(t *testing.T) {
	generateFunc, _, _ := mockGeneratorWithFunc(func(ctx context.Context, prompt string, maxTokens int) (string, error) {
		// Verify the prompt contains the history about Elon Musk
		if !contains(prompt, "Elon Musk") {
			t.Error("Prompt should contain history about Elon Musk")
		}
		return `{"queries": ["When did Elon Musk start SpaceX", "Elon Musk SpaceX founding year", "SpaceX company history Musk"]}`, nil
	})

	expander := NewLLMQueryExpander(generateFunc, DefaultExpansionConfig())

	history := []RelevantTurn{
		{Question: "Who founded Tesla?", Answer: "Tesla was founded by Elon Musk along with other co-founders..."},
	}

	expanded, err := expander.Expand(context.Background(), "when did he start SpaceX", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	if !expanded.Expanded {
		t.Error("Expected expansion for pronoun query")
	}

	// Check that at least one query mentions Elon Musk
	foundMusk := false
	for _, q := range expanded.Queries {
		if contains(q, "Musk") || contains(q, "Elon") {
			foundMusk = true
			break
		}
	}
	if !foundMusk {
		t.Error("Expected at least one query to resolve 'he' to 'Elon Musk'")
	}
}

// contains checks if s contains substr (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && (s[0:len(substr)] == substr ||
			contains(s[1:], substr)))
}
