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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Setup
// =============================================================================

func init() {
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// Mock Session Verifier
// =============================================================================

// mockSessionVerifier implements SessionVerifier for testing.
//
// # Description
//
// Provides a configurable mock for testing the verify handler
// without requiring a real Weaviate connection.
//
// # Fields
//
//   - VerifyFunc: Function to call when VerifySession is invoked
type mockSessionVerifier struct {
	VerifyFunc func(c *gin.Context, sessionID string) (*VerifySessionResponse, error)
}

// VerifySession implements SessionVerifier for testing.
func (m *mockSessionVerifier) VerifySession(c *gin.Context, sessionID string) (*VerifySessionResponse, error) {
	if m.VerifyFunc != nil {
		return m.VerifyFunc(c, sessionID)
	}
	return &VerifySessionResponse{
		SessionID: sessionID,
		Verified:  true,
		TurnCount: 0,
	}, nil
}

// =============================================================================
// weaviateSessionVerifier Unit Tests
// =============================================================================

func TestWeaviateSessionVerifier_ComputeTurnHashes_Empty(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turns := []conversationTurn{}

	hashes := verifier.computeTurnHashes(turns)

	assert.Empty(t, hashes)
}

func TestWeaviateSessionVerifier_ComputeTurnHashes_SingleTurn(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turns := []conversationTurn{
		{Question: "What is 2+2?", Answer: "2+2 equals 4."},
	}

	hashes := verifier.computeTurnHashes(turns)

	require.Len(t, hashes, 1)
	assert.Len(t, hashes[1], 64) // SHA-256 hex is 64 chars
	assert.NotEmpty(t, hashes[1])
}

func TestWeaviateSessionVerifier_ComputeTurnHashes_MultipleTurns(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turns := []conversationTurn{
		{Question: "What is 2+2?", Answer: "4."},
		{Question: "What is 3+3?", Answer: "6."},
		{Question: "What is 4+4?", Answer: "8."},
	}

	hashes := verifier.computeTurnHashes(turns)

	require.Len(t, hashes, 3)
	// Verify 1-indexed
	assert.Contains(t, hashes, 1)
	assert.Contains(t, hashes, 2)
	assert.Contains(t, hashes, 3)
	// All hashes should be different
	assert.NotEqual(t, hashes[1], hashes[2])
	assert.NotEqual(t, hashes[2], hashes[3])
}

func TestWeaviateSessionVerifier_ComputeTurnHashes_Consistent(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turns := []conversationTurn{
		{Question: "Hello", Answer: "World"},
	}

	hash1 := verifier.computeTurnHashes(turns)
	hash2 := verifier.computeTurnHashes(turns)

	assert.Equal(t, hash1[1], hash2[1], "same input should produce same hash")
}

func TestWeaviateSessionVerifier_ComputeChainHash_Empty(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turnHashes := map[int]string{}

	chainHash := verifier.computeChainHash(turnHashes)

	assert.Empty(t, chainHash)
}

func TestWeaviateSessionVerifier_ComputeChainHash_SingleTurn(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turnHashes := map[int]string{
		1: "abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
	}

	chainHash := verifier.computeChainHash(turnHashes)

	assert.Len(t, chainHash, 64)
	assert.NotEmpty(t, chainHash)
}

func TestWeaviateSessionVerifier_ComputeChainHash_MultipleTurns(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turnHashes := map[int]string{
		1: "hash1",
		2: "hash2",
		3: "hash3",
	}

	chainHash := verifier.computeChainHash(turnHashes)

	assert.Len(t, chainHash, 64)
	// Chain hash should be SHA256 of concatenated hashes
	assert.NotEmpty(t, chainHash)
}

func TestWeaviateSessionVerifier_ComputeChainHash_Consistent(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	turnHashes := map[int]string{
		1: "hash1",
		2: "hash2",
	}

	chain1 := verifier.computeChainHash(turnHashes)
	chain2 := verifier.computeChainHash(turnHashes)

	assert.Equal(t, chain1, chain2, "same input should produce same chain hash")
}

func TestWeaviateSessionVerifier_ComputeChainHash_OrderMatters(t *testing.T) {
	verifier := &weaviateSessionVerifier{}

	// Order is determined by key (turn number), not insertion order
	hashes1 := map[int]string{1: "a", 2: "b"}
	hashes2 := map[int]string{1: "b", 2: "a"}

	chain1 := verifier.computeChainHash(hashes1)
	chain2 := verifier.computeChainHash(hashes2)

	assert.NotEqual(t, chain1, chain2, "different order should produce different chain")
}

func TestWeaviateSessionVerifier_ParseConversationResult_Empty(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	data := map[string]interface{}{
		"Get": map[string]interface{}{
			"Conversation": nil,
		},
	}

	turns, err := verifier.parseConversationResult(data)

	require.NoError(t, err)
	assert.Empty(t, turns)
}

func TestWeaviateSessionVerifier_ParseConversationResult_SingleTurn(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	data := map[string]interface{}{
		"Get": map[string]interface{}{
			"Conversation": []interface{}{
				map[string]interface{}{
					"question":  "What is Go?",
					"answer":    "Go is a programming language.",
					"timestamp": 1735657200000.0,
				},
			},
		},
	}

	turns, err := verifier.parseConversationResult(data)

	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "What is Go?", turns[0].Question)
	assert.Equal(t, "Go is a programming language.", turns[0].Answer)
}

func TestWeaviateSessionVerifier_ParseConversationResult_MultipleTurns(t *testing.T) {
	verifier := &weaviateSessionVerifier{}
	data := map[string]interface{}{
		"Get": map[string]interface{}{
			"Conversation": []interface{}{
				map[string]interface{}{
					"question":  "Q1",
					"answer":    "A1",
					"timestamp": 1735657200000.0,
				},
				map[string]interface{}{
					"question":  "Q2",
					"answer":    "A2",
					"timestamp": 1735657200001.0,
				},
			},
		},
	}

	turns, err := verifier.parseConversationResult(data)

	require.NoError(t, err)
	require.Len(t, turns, 2)
	assert.Equal(t, "Q1", turns[0].Question)
	assert.Equal(t, "Q2", turns[1].Question)
}

// =============================================================================
// VerifySessionResponse Tests
// =============================================================================

func TestVerifySessionResponse_JSONSerialization(t *testing.T) {
	response := &VerifySessionResponse{
		SessionID:  "sess-123",
		Verified:   true,
		TurnCount:  5,
		ChainHash:  "abc123def456",
		VerifiedAt: 1735657200000,
		TurnHashes: map[int]string{
			1: "hash1",
			2: "hash2",
		},
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	var parsed VerifySessionResponse
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, response.SessionID, parsed.SessionID)
	assert.Equal(t, response.Verified, parsed.Verified)
	assert.Equal(t, response.TurnCount, parsed.TurnCount)
	assert.Equal(t, response.ChainHash, parsed.ChainHash)
	assert.Equal(t, response.VerifiedAt, parsed.VerifiedAt)
	assert.Equal(t, response.TurnHashes[1], parsed.TurnHashes[1])
}

func TestVerifySessionResponse_WithError(t *testing.T) {
	response := &VerifySessionResponse{
		SessionID:    "sess-123",
		Verified:     false,
		TurnCount:    0,
		ErrorDetails: "session not found",
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	var parsed VerifySessionResponse
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.False(t, parsed.Verified)
	assert.Equal(t, "session not found", parsed.ErrorDetails)
}

// =============================================================================
// HTTP Handler Tests
// =============================================================================

func TestVerifySession_MissingSessionID(t *testing.T) {
	// Setup router without session ID parameter
	router := gin.New()
	router.POST("/v1/sessions/verify", func(c *gin.Context) {
		// Simulate the handler behavior when sessionId is empty
		sessionID := c.Param("sessionId")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
			return
		}
	})

	// Make request
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/verify", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error"], "sessionId")
}

// =============================================================================
// conversationTurn Tests
// =============================================================================

func TestConversationTurn_JSONSerialization(t *testing.T) {
	turn := conversationTurn{
		Question:  "What is the capital of France?",
		Answer:    "The capital of France is Paris.",
		Timestamp: 1735657200000,
	}

	data, err := json.Marshal(turn)
	require.NoError(t, err)

	var parsed conversationTurn
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, turn.Question, parsed.Question)
	assert.Equal(t, turn.Answer, parsed.Answer)
	assert.Equal(t, turn.Timestamp, parsed.Timestamp)
}
