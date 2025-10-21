package datatypes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/weaviate/weaviate-go-client/v4/weaviate"
)

type Session struct {
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
}

func (s *Session) Save(client *weaviate.Client) error {
	slog.Info("Saving new session metadata", "sessionId", s.SessionId, "summary", s.Summary)
	properties := map[string]interface{}{
		"session_id": s.SessionId,
		"summary":    s.Summary,
		"timestamp":  time.Now().UnixMilli(),
	}

	_, err := client.Data().Creator().
		WithClassName("Session").
		WithProperties(properties).
		Do(context.Background())

	if err != nil {
		return fmt.Errorf("failed to save Session object to Weaviate: %w", err)
	}

	slog.Info("Successfully saved session metadata", "sessionId", s.SessionId)
	return nil
}
