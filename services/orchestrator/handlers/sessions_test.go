// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"encoding/json"
	"testing"
	"time"
)

// =============================================================================
// Session Context Persistence Tests (chat_ux_05)
// =============================================================================

// TestSessionMetadata_JSONMarshaling tests that SessionMetadata serializes correctly.
func TestSessionMetadata_JSONMarshaling(t *testing.T) {
	t.Run("full metadata serializes all fields", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID:     "sess-abc-123",
			DataSpace:     "wheat",
			Pipeline:      "verified",
			TTLDurationMs: 300000,
			TTLExpiresAt:  time.Now().UnixMilli() + 300000,
			Timestamp:     time.Now().UnixMilli(),
			Summary:       "Test session",
		}

		jsonBytes, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var decoded SessionMetadata
		if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if decoded.SessionID != meta.SessionID {
			t.Errorf("SessionID mismatch: got %q, want %q", decoded.SessionID, meta.SessionID)
		}
		if decoded.DataSpace != meta.DataSpace {
			t.Errorf("DataSpace mismatch: got %q, want %q", decoded.DataSpace, meta.DataSpace)
		}
		if decoded.Pipeline != meta.Pipeline {
			t.Errorf("Pipeline mismatch: got %q, want %q", decoded.Pipeline, meta.Pipeline)
		}
		if decoded.TTLDurationMs != meta.TTLDurationMs {
			t.Errorf("TTLDurationMs mismatch: got %d, want %d", decoded.TTLDurationMs, meta.TTLDurationMs)
		}
	})

	t.Run("empty optional fields omit correctly", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID: "sess-123",
			// DataSpace, Pipeline, TTL all empty
		}

		jsonBytes, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		jsonStr := string(jsonBytes)

		// Check that omitempty fields are not present when empty
		if containsSubstring(jsonStr, `"data_space":""`) {
			t.Error("Empty data_space should be omitted")
		}
		if containsSubstring(jsonStr, `"pipeline":""`) {
			t.Error("Empty pipeline should be omitted")
		}
	})

	t.Run("context fields present when set", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID: "sess-123",
			DataSpace: "work",
			Pipeline:  "reranking",
		}

		jsonBytes, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		jsonStr := string(jsonBytes)

		if !containsSubstring(jsonStr, `"data_space":"work"`) {
			t.Errorf("Expected data_space:work in JSON, got: %s", jsonStr)
		}
		if !containsSubstring(jsonStr, `"pipeline":"reranking"`) {
			t.Errorf("Expected pipeline:reranking in JSON, got: %s", jsonStr)
		}
	})
}

// TestSessionMetadata_TTLExpiredCheck tests TTL expiration logic.
func TestSessionMetadata_TTLExpiredCheck(t *testing.T) {
	t.Run("session with future expiry is not expired", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID:    "sess-123",
			TTLExpiresAt: time.Now().Add(1 * time.Hour).UnixMilli(),
		}

		if isSessionExpired(meta) {
			t.Error("Session with future expiry should not be expired")
		}
	})

	t.Run("session with past expiry is expired", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID:    "sess-123",
			TTLExpiresAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
		}

		if !isSessionExpired(meta) {
			t.Error("Session with past expiry should be expired")
		}
	})

	t.Run("session with zero TTL never expires", func(t *testing.T) {
		meta := SessionMetadata{
			SessionID:    "sess-123",
			TTLExpiresAt: 0, // No TTL
		}

		if isSessionExpired(meta) {
			t.Error("Session with zero TTL should never expire")
		}
	})
}

// TestSessionContextFields tests that all context fields are recognized.
func TestSessionContextFields(t *testing.T) {
	t.Run("all context fields deserialize from JSON", func(t *testing.T) {
		jsonStr := `{
			"session_id": "sess-test-001",
			"data_space": "wheat",
			"pipeline": "verified",
			"ttl_duration_ms": 300000,
			"ttl_expires_at": 1737900000000,
			"timestamp": 1737800000000,
			"summary": "Testing wheat documents"
		}`

		var meta SessionMetadata
		if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if meta.SessionID != "sess-test-001" {
			t.Errorf("SessionID: got %q, want %q", meta.SessionID, "sess-test-001")
		}
		if meta.DataSpace != "wheat" {
			t.Errorf("DataSpace: got %q, want %q", meta.DataSpace, "wheat")
		}
		if meta.Pipeline != "verified" {
			t.Errorf("Pipeline: got %q, want %q", meta.Pipeline, "verified")
		}
		if meta.TTLDurationMs != 300000 {
			t.Errorf("TTLDurationMs: got %d, want %d", meta.TTLDurationMs, 300000)
		}
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// isSessionExpired checks if a session has passed its TTL expiration.
// Returns false if TTLExpiresAt is 0 (no TTL configured).
func isSessionExpired(meta SessionMetadata) bool {
	if meta.TTLExpiresAt == 0 {
		return false // No TTL = never expires
	}
	return time.Now().UnixMilli() > meta.TTLExpiresAt
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
