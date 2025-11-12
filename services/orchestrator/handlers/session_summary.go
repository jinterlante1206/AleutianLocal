package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jinterlante1206/AleutianLocal/services/llm"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
)

var (
	SUMMARY_TITLE_MAX_TOKENS  = 50
	SUMMARY_TITLE_TEMPERATURE = 0.2
)

// This helper struct is for parsing the UUID from a Weaviate query
type sessionQueryResponse struct {
	Get struct {
		Session []struct {
			Additional struct {
				ID string `json:"id"`
			} `json:"_additional"`
		} `json:"Session"`
	} `json:"Get"`
}

func SummarizeAndSaveSession(llmClient llm.LLMClient, client *weaviate.Client, sessionId, question, answer string) {
	slog.Info("Generating and saving summary for existing session", "sessionId", sessionId)

	// 1. Construct the meta-prompt for summarization (same as before)
	summaryPrompt := fmt.Sprintf("Generate a very short title (8 words max) for this conversation:\nUser: %s\nAI: %s\nTitle:", question, answer)

	// 2. Call the LLM (same as before)
	temp := float32(SUMMARY_TITLE_TEMPERATURE)
	maxTokens := SUMMARY_TITLE_MAX_TOKENS
	summaryParams := llm.GenerationParams{
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		Stop:        []string{"\n", "User:", "AI:"},
	}

	summaryString, err := llmClient.Generate(context.Background(), summaryPrompt, summaryParams)
	summaryString = strings.TrimSpace(summaryString)

	// 3. Fallback logic (same as before)
	if err != nil || summaryString == "" {
		if err != nil {
			slog.Error("Failed to generate session summary via LLMClient", "sessionId", sessionId, "error", err)
		} else {
			slog.Warn("LLM generated an empty summary, using fallback.", "sessionId", sessionId)
		}
		summaryString = fmt.Sprintf("Chat: %s", question)
		if len(summaryString) > 100 {
			summaryString = summaryString[:100] + "..."
		}
	} else {
		slog.Info("Successfully generated session summary", "sessionId", sessionId, "summary", summaryString)
	}

	// 4. Find the Weaviate UUID for the session_id
	// This session should have been created by Conversation.Save
	where := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionId)

	fields := []graphql.Field{
		{Name: "_additional", Fields: []graphql.Field{{Name: "id"}}},
	}

	resp, err := client.GraphQL().Get().
		WithClassName("Session").
		WithWhere(where).
		WithFields(fields...).
		WithLimit(1).
		Do(context.Background())

	if err != nil {
		slog.Error("Failed to query for session to update summary", "sessionId", sessionId, "error", err)
		return
	}

	var queryResp sessionQueryResponse
	respBytes, _ := json.Marshal(resp.Data)
	if err := json.Unmarshal(respBytes, &queryResp); err != nil {
		slog.Error("Failed to parse session query response for summary update", "sessionId", sessionId, "error", err)
		return
	}

	if len(queryResp.Get.Session) == 0 {
		slog.Error("CRITICAL: Session object was not created by Conversation.Save, cannot update summary.", "sessionId", sessionId)
		return
	}

	sessionUUID := queryResp.Get.Session[0].Additional.ID

	// 5. Update the existing Session object with the new summary
	err = client.Data().Updater().
		WithClassName("Session").
		WithID(sessionUUID).
		WithMerge().
		WithProperties(map[string]interface{}{
			"summary": summaryString,
		}).
		Do(context.Background())

	if err != nil {
		slog.Error("Failed to update session with new summary", "sessionId", sessionId, "weaviateUUID", sessionUUID, "error", err)
	} else {
		slog.Info("Successfully updated session with summary", "sessionId", sessionId, "weaviateUUID", sessionUUID)
	}
}
