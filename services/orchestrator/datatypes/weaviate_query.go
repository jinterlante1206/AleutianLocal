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
	"encoding/json"
	"fmt"

	"github.com/weaviate/weaviate/entities/models"
)

// =============================================================================
// Generic GraphQL Response Parser
// =============================================================================

// ParseGraphQLResponse parses a Weaviate GraphQL response into the target type.
//
// # Description
//
// This generic function encapsulates the marshal/unmarshal pattern required to
// convert Weaviate's dynamic response (map[string]models.JSONObject) into a
// strongly-typed Go struct. The target type T must have json tags matching
// the expected response shape.
//
// # Type Parameters
//
//   - T: The target struct type with json tags matching the response shape.
//
// # Inputs
//
//   - resp: The GraphQL response from Weaviate client's Do() method.
//
// # Outputs
//
//   - *T: Pointer to the parsed struct.
//   - error: Non-nil if response is nil or parsing fails.
//
// # Example
//
//	type SessionResponse struct {
//	    Get struct {
//	        Session []struct {
//	            SessionID string `json:"session_id"`
//	        } `json:"Session"`
//	    } `json:"Get"`
//	}
//
//	resp, err := client.GraphQL().Get().WithClassName("Session").Do(ctx)
//	if err != nil { ... }
//
//	parsed, err := ParseGraphQLResponse[SessionResponse](resp)
//	if err != nil { ... }
//
//	for _, s := range parsed.Get.Session {
//	    fmt.Println(s.SessionID)
//	}
//
// # Limitations
//
//   - Requires the target type to exactly match the expected response structure.
//   - Type mismatches will result in zero values, not errors.
//
// # Assumptions
//
//   - The response Data field is JSON-marshalable.
//   - The target type T has correct json tags.
func ParseGraphQLResponse[T any](resp *models.GraphQLResponse) (*T, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil GraphQL response")
	}

	respBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL response data: %w", err)
	}

	var result T
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into target type: %w", err)
	}

	return &result, nil
}

// =============================================================================
// Common Weaviate Response Types
// =============================================================================

// SessionQueryResponse represents the response from querying the Session class.
//
// # Fields
//
//   - Get.Session: Array of session objects with their Weaviate UUIDs.
type SessionQueryResponse struct {
	Get struct {
		Session []SessionResult `json:"Session"`
	} `json:"Get"`
}

// SessionResult represents a single session from a query.
type SessionResult struct {
	SessionID  string `json:"session_id"`
	Summary    string `json:"summary"`
	Timestamp  int64  `json:"timestamp"`
	Additional struct {
		ID string `json:"id"`
	} `json:"_additional"`
}

// ConversationQueryResponse represents the response from querying the Conversation class.
//
// # Fields
//
//   - Get.Conversation: Array of conversation turns.
type ConversationQueryResponse struct {
	Get struct {
		Conversation []ConversationResult `json:"Conversation"`
	} `json:"Get"`
}

// ConversationResult represents a single conversation turn from a query.
type ConversationResult struct {
	SessionID  string `json:"session_id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`
	Timestamp  int64  `json:"timestamp"`
	TurnNumber *int   `json:"turn_number"`
}

// DocumentQueryResponse represents the response from querying the Document class.
//
// # Fields
//
//   - Get.Document: Array of document objects.
type DocumentQueryResponse struct {
	Get struct {
		Document []DocumentResult `json:"Document"`
	} `json:"Get"`
}

// DocumentResult represents a single document from a query.
type DocumentResult struct {
	Content       string `json:"content"`
	Source        string `json:"source"`
	ParentSource  string `json:"parent_source"`
	DataSpace     string `json:"data_space"`
	VersionTag    string `json:"version_tag"`
	VersionNumber *int   `json:"version_number"`
	IsCurrent     *bool  `json:"is_current"`
	TurnNumber    *int   `json:"turn_number"`
	IngestedAt    int64  `json:"ingested_at"`
	Additional    struct {
		ID        string   `json:"id"`
		Distance  *float32 `json:"distance"`
		Certainty *float32 `json:"certainty"`
	} `json:"_additional"`
}

// =============================================================================
// ToMap Methods for Property Structs (defined in rag.go)
// =============================================================================

// ToMap converts SessionProperties to map[string]interface{} for Weaviate.
//
// # Description
//
// Converts the typed SessionProperties struct to the map format required by
// Weaviate's WithProperties() method.
//
// # Outputs
//
//   - map[string]interface{}: Property map ready for Weaviate client.
//
// # Example
//
//	props := SessionProperties{SessionId: "sess_123", Summary: "...", Timestamp: now}
//	client.Data().Creator().WithProperties(props.ToMap()).Do(ctx)
func (p *SessionProperties) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"session_id": p.SessionId,
		"summary":    p.Summary,
		"timestamp":  p.Timestamp,
	}
}

// ToMap converts ConversationProperties to map[string]interface{} for Weaviate.
//
// # Description
//
// Converts the typed ConversationProperties struct to the map format required by
// Weaviate's WithProperties() method.
//
// # Outputs
//
//   - map[string]interface{}: Property map ready for Weaviate client.
//
// # Example
//
//	props := ConversationProperties{SessionId: "sess_123", Question: "...", Answer: "..."}
//	client.Data().Creator().WithProperties(props.ToMap()).Do(ctx)
func (p *ConversationProperties) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"session_id": p.SessionId,
		"question":   p.Question,
		"answer":     p.Answer,
		"timestamp":  p.Timestamp,
	}
}

// DocumentProperties represents the properties for creating a Document object.
type DocumentProperties struct {
	Content       string `json:"content"`
	Source        string `json:"source"`
	ParentSource  string `json:"parent_source"`
	DataSpace     string `json:"data_space"`
	VersionTag    string `json:"version_tag"`
	VersionNumber int    `json:"version_number"`
	IsCurrent     bool   `json:"is_current"`
	TurnNumber    int    `json:"turn_number"`
	IngestedAt    int64  `json:"ingested_at"`
}

// ToMap converts DocumentProperties to map[string]interface{} for Weaviate.
func (p *DocumentProperties) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"content":        p.Content,
		"source":         p.Source,
		"parent_source":  p.ParentSource,
		"data_space":     p.DataSpace,
		"version_tag":    p.VersionTag,
		"version_number": p.VersionNumber,
		"is_current":     p.IsCurrent,
		"turn_number":    p.TurnNumber,
		"ingested_at":    p.IngestedAt,
	}
}

// WithBeacon adds an inSession beacon reference to the properties map.
//
// # Description
//
// Creates the cross-reference format Weaviate requires for linking objects.
// Note: The "localhost" in the beacon URI is part of Weaviate's standard
// cross-reference format and is NOT an actual host - it's a protocol identifier.
// See: https://weaviate.io/developers/weaviate/manage-data/cross-references
//
// # Inputs
//
//   - props: The property map to add the beacon to.
//   - sessionUUID: The Weaviate UUID of the target Session object.
//
// # Example
//
//	props := docProps.ToMap()
//	WithBeacon(props, sessionUUID)
//
// BeaconRef represents a Weaviate cross-reference beacon.
type BeaconRef struct {
	Beacon string `json:"beacon"`
}

func WithBeacon(props map[string]interface{}, sessionUUID string) {
	// "weaviate://localhost/" is the standard beacon URI scheme - localhost is NOT a real host
	// Reference properties in Weaviate must be arrays of beacon objects
	beacon := BeaconRef{
		Beacon: fmt.Sprintf("weaviate://localhost/Session/%s", sessionUUID),
	}
	props["inSession"] = []BeaconRef{beacon}
}
