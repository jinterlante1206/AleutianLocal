package datatypes

import (
	"context"
	"encoding/json"
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

// This struct is used to parse the UUID from a Weaviate query
type sessionQueryResponse struct {
	Get struct {
		Session []struct {
			Additional struct {
				ID string `json:"id"`
			} `json:"_additional"`
		} `json:"Session"`
	} `json:"Get"`
}

// findOrCreateSessionUUID finds a session by its session_id and returns its Weaviate UUID
// If it doesn't exist, it creates one and returns the new UUID
func findOrCreateSessionUUID(ctx context.Context, client *weaviate.Client,
	sessionID string) (string, error) {

	ctx, span := convTracer.Start(ctx, "findOrCreateSessionUUID")
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

	// 2. Parse the response
	var queryResp sessionQueryResponse
	respBytes, _ := json.Marshal(resp.Data)
	if err := json.Unmarshal(respBytes, &queryResp); err != nil {
		return "", fmt.Errorf("error parsing session query response: %w", err)
	}

	if len(queryResp.Get.Session) > 0 {
		uuid := queryResp.Get.Session[0].Additional.ID
		slog.Info("Found existing session", "sessionId", sessionID, "weaviateUUID", uuid)
		return uuid, nil
	}

	// 3. Not found, so create it
	slog.Info("No existing session found, creating a new one with pending summary...", "sessionId", sessionID)
	properties := map[string]interface{}{
		"session_id": sessionID,
		"summary":    "(Summary pending...)", // Placeholder
		"timestamp":  time.Now().UnixMilli(),
	}

	// --- FIX 2: Correctly access the ID from result.Object.ID ---
	result, err := client.Data().Creator().
		WithClassName("Session").
		WithProperties(properties).
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
	sessionUUID, err := findOrCreateSessionUUID(parentCtx, client, c.SessionId)
	if err != nil {
		slog.Error(
			"Failed to find or create parent session, saving conversation without graph link",
			"sessionId", c.SessionId,
			"error", err)
	}

	properties := map[string]interface{}{
		"session_id": c.SessionId,
		"question":   c.Question,
		"answer":     c.Answer,
		"timestamp":  time.Now().UnixMilli(),
	}

	// Create the "beacon" (the graph link)
	// This is the format Weaviate requires for a cross-reference
	beacon := map[string]interface{}{
		"beacon": fmt.Sprintf("weaviate://localhost/Session/%s", sessionUUID),
	}

	// Add the beacon to the 'inSession' property
	if err == nil { // Only add the link if we successfully got the UUID
		properties["inSession"] = []map[string]interface{}{beacon}
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
