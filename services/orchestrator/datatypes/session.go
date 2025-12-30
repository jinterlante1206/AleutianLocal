// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package datatypes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
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
