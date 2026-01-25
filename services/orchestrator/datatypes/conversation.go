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

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/ttl"
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

// FindOrCreateSessionWithTTL finds or creates a session with optional TTL.
//
// # Description
//
// Like FindOrCreateSessionUUID, but also sets TTL on new sessions and resets
// TTL on existing sessions (for the "resets on each message" behavior).
//
// # Inputs
//
//   - ctx: Context for cancellation and tracing.
//   - client: Weaviate client.
//   - sessionID: Session identifier (e.g., "sess_abc123").
//   - sessionTTL: TTL duration string (e.g., "24h", "7d"). Empty = no TTL.
//
// # Outputs
//
//   - string: Weaviate UUID of the session.
//   - error: Non-nil if operation fails.
func FindOrCreateSessionWithTTL(ctx context.Context, client *weaviate.Client,
	sessionID, sessionTTL string) (string, error) {

	ctx, span := convTracer.Start(ctx, "FindOrCreateSessionWithTTL")
	defer span.End()

	// Parse TTL if provided
	var ttlExpiresAt int64
	var ttlDurationMs int64
	if sessionTTL != "" {
		result, err := ttl.ParseTTLDuration(sessionTTL)
		if err != nil {
			return "", fmt.Errorf("invalid session TTL '%s': %w", sessionTTL, err)
		}
		ttlExpiresAt = result.ExpiresAt
		ttlDurationMs = result.Duration.Milliseconds()
		slog.Info("Session TTL configured",
			"session_id", sessionID,
			"ttl_input", sessionTTL,
			"ttl_description", result.Description,
			"expires_at", time.UnixMilli(ttlExpiresAt).Format(time.RFC3339),
		)
	}

	// 1. Try to find the existing session
	where := filters.Where().
		WithPath([]string{"session_id"}).
		WithOperator(filters.Equal).
		WithValueString(sessionID)

	fields := []graphql.Field{
		{Name: "_additional", Fields: []graphql.Field{{Name: "id"}}},
		{Name: "ttl_expires_at"},
		{Name: "ttl_duration_ms"},
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

	queryResp, err := ParseGraphQLResponse[SessionQueryResponse](resp)
	if err != nil {
		return "", fmt.Errorf("error parsing session query response: %w", err)
	}

	if len(queryResp.Get.Session) > 0 {
		uuid := queryResp.Get.Session[0].Additional.ID
		slog.Info("Found existing session", "sessionId", sessionID, "weaviateUUID", uuid)

		// Reset TTL on existing session if TTL is configured
		if ttlExpiresAt > 0 {
			if err := ResetSessionTTL(ctx, client, uuid, ttlExpiresAt, ttlDurationMs); err != nil {
				slog.Warn("Failed to reset session TTL", "session_id", sessionID, "error", err)
				// Don't fail the request, just log the warning
			}
		}
		return uuid, nil
	}

	// 2. Not found, so create it with TTL
	slog.Info("No existing session found, creating a new one...",
		"sessionId", sessionID,
		"has_ttl", ttlExpiresAt > 0,
	)
	props := SessionProperties{
		SessionId:     sessionID,
		Summary:       "(Summary pending...)",
		Timestamp:     time.Now().UnixMilli(),
		TTLExpiresAt:  ttlExpiresAt,
		TTLDurationMs: ttlDurationMs,
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

	slog.Info("Successfully created new session",
		"sessionId", sessionID,
		"weaviateUUID", result.Object.ID,
		"ttl_expires_at", ttlExpiresAt,
	)
	return result.Object.ID.String(), nil
}

// ResetSessionTTL updates the TTL expiration time on an existing session.
//
// # Description
//
// Called on each message to reset the TTL countdown. The session will expire
// ttlDurationMs milliseconds from now.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - client: Weaviate client.
//   - weaviateUUID: The Weaviate object UUID of the session.
//   - ttlExpiresAt: New expiration timestamp (Unix milliseconds).
//   - ttlDurationMs: Original TTL duration in milliseconds.
//
// # Outputs
//
//   - error: Non-nil if update fails.
func ResetSessionTTL(ctx context.Context, client *weaviate.Client,
	weaviateUUID string, ttlExpiresAt, ttlDurationMs int64) error {

	ctx, span := convTracer.Start(ctx, "ResetSessionTTL")
	defer span.End()

	err := client.Data().Updater().
		WithClassName("Session").
		WithID(weaviateUUID).
		WithProperties(map[string]interface{}{
			"ttl_expires_at":  ttlExpiresAt,
			"ttl_duration_ms": ttlDurationMs,
		}).
		Do(ctx)

	if err != nil {
		return fmt.Errorf("failed to reset session TTL: %w", err)
	}

	slog.Debug("Reset session TTL",
		"weaviate_uuid", weaviateUUID,
		"new_expires_at", time.UnixMilli(ttlExpiresAt).Format(time.RFC3339),
	)
	return nil
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
