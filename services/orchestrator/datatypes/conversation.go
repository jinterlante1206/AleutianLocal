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
	"strings"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"go.opentelemetry.io/otel"
)

var convTracer = otel.Tracer("aleutian.orchestrator.datatypes")

// FindOrCreateSessionUUID finds a session by its session_id and returns its Weaviate UUID
// If it doesn't exist, it creates one and returns the new UUID
func FindOrCreateSessionUUID(ctx context.Context, client *weaviate.Client,
	sessionID string) (string, error) {

	ctx, span := convTracer.Start(ctx, "FindOrCreateSessionUUID")
	defer span.End()

	// 1. Try to find the existing session
	where := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	fields := []graphql.Field{
		{Name: "_additional", Fields: []graphql.Field{{Name: "id"}}},
	}

	resp, err := client.GraphQL().Get().
		WithClassName("Session").
		WithWhere(where).
		WithFields(fields...).
		WithLimit(1).
		Do(ctx)

	if err != nil {
		return "", fmt.Errorf("error querying for session: %w", err)
	}

	// 2. Parse the response using generic parser
	queryResp, err := ParseGraphQLResponse[SessionQueryResponse](resp)
	if err != nil {
		return "", fmt.Errorf("error parsing session query response: %w", err)
	}

	if len(queryResp.Get.Session) > 0 {
		uuid := queryResp.Get.Session[0].Additional.ID
		slog.Info("Found existing session", "sessionId", sessionID, "weaviateUUID", uuid)
		return uuid, nil
	}

	// 3. Not found, so create it using typed properties
	slog.Info("No existing session found, creating a new one with pending summary...", "sessionId", sessionID)
	props := SessionProperties{
		SessionId: sessionID,
		Summary:   "(Summary pending...)",
		Timestamp: time.Now().UnixMilli(),
	}

	result, err := client.Data().Creator().
		WithClassName("Session").
		WithProperties(props.ToMap()).
		Do(ctx)

	if err != nil {
		return "", fmt.Errorf("failed to create new session: %w", err)
	}

	if result == nil || result.Object == nil {
		return "", fmt.Errorf("weaviate created a session but returned a nil result")
	}

	slog.Info("Successfully created new session", "sessionId", sessionID, "weaviateUUID", result.Object.ID)
	return result.Object.ID.String(), nil
}

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

	parentCtx := context.Background()
	sessionUUID, err := FindOrCreateSessionUUID(parentCtx, client, c.SessionId)
	if err != nil {
		slog.Error(
			"Failed to find or create parent session, saving conversation without graph link",
			"sessionId", c.SessionId,
			"error", err)
	}

	// Use typed properties struct
	props := ConversationProperties{
		SessionId: c.SessionId,
		Question:  c.Question,
		Answer:    c.Answer,
		Timestamp: time.Now().UnixMilli(),
	}
	properties := props.ToMap()

	// Add the beacon link if we have a valid session UUID
	if err == nil {
		WithBeacon(properties, sessionUUID)
	}

	// Create the Conversation object
	creator := client.Data().Creator().
		WithClassName("Conversation").
		WithProperties(properties)

	// Weaviate v5 Creator().Do() returns (*data.ObjectWrapper, error)
	// We don't need the result object here, just the error.
	_, err = creator.Do(parentCtx)

	if err != nil {
		return fmt.Errorf("failed to save conversation object to Weaviate: %w", err)
	}

	slog.Info("Successfully saved conversation", "sessionId", c.SessionId)
	return nil
}
