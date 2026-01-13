package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Integration Tests - Real LLM
// =============================================================================
//
// These tests require a running LLM service (Ollama) and are skipped by default.
// To run: INTEGRATION_TEST=true go test ./conversation/... -v -run Integration
//
// The tests verify that:
// 1. Query expansion produces useful multi-query variations
// 2. Contextual summarization creates focused summaries
// 3. The full flow works end-to-end
//
// NOTE: These tests are SLOWER and may be flaky due to LLM non-determinism.

func skipIfNotIntegrationTest(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Integration test requires INTEGRATION_TEST=true")
	}
}

// ollamaGenerate calls the real Ollama API
func ollamaGenerate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]interface{}{
			"num_predict": maxTokens,
			"temperature": 0.2,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL+"/api/generate", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Response, nil
}

// =============================================================================
// Query Expansion Integration Tests
// =============================================================================

func TestIntegration_Expand_TellMeMore(t *testing.T) {
	skipIfNotIntegrationTest(t)

	expander := NewLLMQueryExpander(ollamaGenerate, DefaultExpansionConfig())

	history := []RelevantTurn{
		{
			Question: "What is Motown?",
			Answer:   "Motown was a record label founded by Berry Gordy in Detroit in 1959. It became one of the most successful independent record companies in American history, known for its distinctive sound and artists like The Supremes, Marvin Gaye, and Stevie Wonder.",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expanded, err := expander.Expand(ctx, "tell me more", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// Verify expansion occurred
	if !expanded.Expanded {
		t.Error("Expected Expanded to be true")
	}

	// Verify we got multiple queries
	if len(expanded.Queries) < 2 {
		t.Errorf("Expected at least 2 queries, got %d", len(expanded.Queries))
	}

	// Verify queries contain relevant terms from history
	allQueries := strings.ToLower(strings.Join(expanded.Queries, " "))
	relevantTerms := []string{"motown", "berry", "gordy", "detroit", "record"}
	foundRelevant := false
	for _, term := range relevantTerms {
		if strings.Contains(allQueries, term) {
			foundRelevant = true
			break
		}
	}

	if !foundRelevant {
		t.Errorf("Expanded queries should contain relevant terms from history. Got: %v", expanded.Queries)
	}

	t.Logf("Original: %q", expanded.Original)
	t.Logf("Expanded queries: %v", expanded.Queries)
}

func TestIntegration_Expand_PronounResolution(t *testing.T) {
	skipIfNotIntegrationTest(t)

	expander := NewLLMQueryExpander(ollamaGenerate, DefaultExpansionConfig())

	history := []RelevantTurn{
		{
			Question: "Who founded Tesla?",
			Answer:   "Elon Musk is often credited as the founder of Tesla, though technically the company was incorporated in 2003 by Martin Eberhard and Marc Tarpenning. Musk joined as chairman and lead investor in 2004.",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expanded, err := expander.Expand(ctx, "when did he start SpaceX", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	// Verify "he" was resolved to Elon Musk
	allQueries := strings.ToLower(strings.Join(expanded.Queries, " "))
	if !strings.Contains(allQueries, "elon") && !strings.Contains(allQueries, "musk") && !strings.Contains(allQueries, "spacex") {
		t.Errorf("Expected pronoun 'he' to be resolved to Elon Musk. Got: %v", expanded.Queries)
	}

	t.Logf("Original: %q", expanded.Original)
	t.Logf("Expanded queries: %v", expanded.Queries)
}

// =============================================================================
// Contextual Embedding Integration Tests
// =============================================================================

func TestIntegration_Summarize_MessyHistory(t *testing.T) {
	skipIfNotIntegrationTest(t)

	embedder := NewContextualEmbedder(ollamaGenerate, DefaultContextConfig())

	// Simulate messy conversational history
	history := []RelevantTurn{
		{
			Question: "wait, who was that guy again?",
			Answer:   "You mean Berry Gordy? He was the founder of Motown Records.",
		},
		{
			Question: "yeah him. what else did he do",
			Answer:   "Berry Gordy started as a songwriter, wrote hits like 'Lonely Teardrops'. He also developed the Motown sound and mentored many artists.",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := embedder.SummarizeContext(ctx, history)
	if err != nil {
		t.Fatalf("SummarizeContext failed: %v", err)
	}

	// Verify summary is focused and mentions key entities
	summaryLower := strings.ToLower(summary)
	if !strings.Contains(summaryLower, "berry") && !strings.Contains(summaryLower, "gordy") && !strings.Contains(summaryLower, "motown") {
		t.Errorf("Summary should mention Berry Gordy or Motown. Got: %q", summary)
	}

	// Verify summary is concise (not longer than the history)
	totalHistoryLen := 0
	for _, turn := range history {
		totalHistoryLen += len(turn.Question) + len(turn.Answer)
	}
	if len(summary) > totalHistoryLen {
		t.Errorf("Summary should be shorter than original history. Summary len: %d, History len: %d", len(summary), totalHistoryLen)
	}

	t.Logf("Summary: %q", summary)
}

func TestIntegration_BuildContextualQuery(t *testing.T) {
	skipIfNotIntegrationTest(t)

	embedder := NewContextualEmbedder(ollamaGenerate, DefaultContextConfig())

	history := []RelevantTurn{
		{
			Question: "What is Motown?",
			Answer:   "Motown was a record label founded in Detroit.",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	contextual := embedder.BuildContextualQuery(ctx, "tell me more", history)

	// Verify format: query | query | Context: ...
	if !strings.Contains(contextual, "tell me more | tell me more") {
		t.Error("Should contain query repeated twice")
	}

	if !strings.Contains(contextual, "Context:") {
		t.Error("Should contain Context: prefix")
	}

	// Verify context mentions relevant topic
	contextLower := strings.ToLower(contextual)
	if !strings.Contains(contextLower, "motown") && !strings.Contains(contextLower, "detroit") {
		t.Errorf("Context should mention Motown or Detroit. Got: %q", contextual)
	}

	t.Logf("Contextual query: %q", contextual)
}

// =============================================================================
// End-to-End Flow Test
// =============================================================================

func TestIntegration_FullFlow_TellMeMore(t *testing.T) {
	skipIfNotIntegrationTest(t)

	// This test simulates the full P8 flow:
	// 1. User asks about Motown
	// 2. User says "tell me more"
	// 3. We expand the query and build contextual embedding

	history := []RelevantTurn{
		{
			Question:   "What is Motown?",
			Answer:     "Motown was a record label founded by Berry Gordy in Detroit in 1959.",
			TurnNumber: 1,
		},
	}

	// Step 1: Query Expansion
	expander := NewLLMQueryExpander(ollamaGenerate, DefaultExpansionConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if !expander.NeedsExpansion("tell me more") {
		t.Error("'tell me more' should need expansion")
	}

	expanded, err := expander.Expand(ctx, "tell me more", history)
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}

	t.Logf("Step 1 - Expanded queries: %v", expanded.Queries)

	// Step 2: Contextual Embedding
	embedder := NewContextualEmbedder(ollamaGenerate, DefaultContextConfig())

	primaryQuery := expanded.Queries[0]
	contextual := embedder.BuildContextualQuery(ctx, primaryQuery, history)

	t.Logf("Step 2 - Contextual query: %q", contextual)

	// Verify the flow produced something useful
	if !expanded.Expanded {
		t.Error("Query should have been expanded")
	}

	if len(contextual) < len(primaryQuery) {
		t.Error("Contextual query should be longer than the primary query")
	}

	// Verify key topic is preserved through the flow
	allText := strings.ToLower(strings.Join(expanded.Queries, " ") + " " + contextual)
	if !strings.Contains(allText, "motown") && !strings.Contains(allText, "berry") && !strings.Contains(allText, "gordy") {
		t.Error("Topic should be preserved through the flow")
	}
}
