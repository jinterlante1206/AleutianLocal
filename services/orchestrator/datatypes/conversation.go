package datatypes

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

type Conversation struct {
	SessionId string `json:"session_id"`
	Question  string `json:"question"`
	Answer    string `json:"answer"`
}

func (c *Conversation) Save(client *weaviate.Client) error {
	if len(strings.TrimSpace(c.Answer)) == 0 {
		return nil
	}
	slog.Info("Saving the conversation to Weaviate", "sessionId", c.SessionId)

	properties := map[string]interface{}{
		"session_id": c.SessionId,
		"question":   c.Question,
		"answer":     c.Answer,
		"timestamp":  time.Now().UnixMilli(),
	}

	_, err := client.Data().Creator().
		WithClassName("Conversation").
		WithProperties(properties).
		Do(context.Background())

	if err != nil {
		return fmt.Errorf("failed to save conversation object to Weaviate: %w", err)
	}

	slog.Info("Successfully saved conversation", "sessionId", c.SessionId)
	return nil
}
