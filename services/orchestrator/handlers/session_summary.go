package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

var (
	SUMMARY_TITLE_MAX_TOKENS  = 50
	SUMMARY_TITLE_TEMPERATURE = 0.2
)

func SummarizeAndSaveSession(llmClient llm.LLMClient, client *weaviate.Client, sessionId, question, answer string) {
	slog.Info("Generating a summary for a new session", "sessionId", sessionId)
	// 1. Construct the meta-prompt for summarization
	summaryPrompt := fmt.Sprintf("Generate a very short title (8 words max) for this conversation:\nUser: %s\nAI: %s\nTitle:", question, answer)
	// 2. Call the LLM with the summary prompt
	temp := float32(SUMMARY_TITLE_TEMPERATURE)
	maxTokens := SUMMARY_TITLE_MAX_TOKENS
	summaryParams := llm.GenerationParams{
		Temperature: &temp,
		TopK:        nil,
		TopP:        nil,
		MaxTokens:   &maxTokens,
		Stop:        []string{"\n", "User:", "AI:"},
	}

	summaryString, err := llmClient.Generate(context.Background(), summaryPrompt, summaryParams)
	if err != nil {
		slog.Error("Failed to generate session summary via LLMClient", "sessionId", sessionId, "error", err)
		// Fallback summary
		summaryString = fmt.Sprintf("Chat: %s", question)
		if len(summaryString) > 100 {
			summaryString = summaryString[:100] + "..."
		}
	} else {
		slog.Info("Successfully generated session summary", "sessionId", sessionId, "summary", summaryString)
	}
	// Save the resulting session
	session := &datatypes.Session{
		SessionId: sessionId,
		Summary:   summaryString,
	}
	if err = session.Save(client); err != nil {
		slog.Error("failed to save session metadata", "sessionId", sessionId, "error", err)
	}
}
