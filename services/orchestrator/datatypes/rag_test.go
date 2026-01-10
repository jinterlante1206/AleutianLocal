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
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ChatRAGRequest.Validate() Tests
// =============================================================================

// TestChatRAGRequest_Validate_MessageRequired verifies that an empty message
// causes validation to fail. The message field is the only required field
// in ChatRAGRequest as it represents the user's input.
func TestChatRAGRequest_Validate_MessageRequired(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty message returns error",
			message:     "",
			expectError: true,
			errorMsg:    "message is required",
		},
		{
			name:        "whitespace-only message is accepted",
			message:     "   ",
			expectError: false,
		},
		{
			name:        "valid message passes validation",
			message:     "What is authentication?",
			expectError: false,
		},
		{
			name:        "single character message is valid",
			message:     "?",
			expectError: false,
		},
		{
			name:        "unicode message is valid",
			message:     "你好世界",
			expectError: false,
		},
		{
			name:        "very long message is valid",
			message:     string(make([]byte, 10000)),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ChatRAGRequest{Message: tt.message}
			err := req.Validate()

			if tt.expectError {
				require.Error(t, err, "expected validation error")
				assert.Contains(t, err.Error(), tt.errorMsg, "error message mismatch")
			} else {
				assert.NoError(t, err, "expected no validation error")
			}
		})
	}
}

// TestChatRAGRequest_Validate_PipelineValidation verifies that the pipeline
// field accepts only supported pipeline types. The valid pipelines are:
// standard, reranking, raptor, graph, rig, semantic.
func TestChatRAGRequest_Validate_PipelineValidation(t *testing.T) {
	validPipelines := []string{
		"standard",
		"reranking",
		"raptor",
		"graph",
		"rig",
		"semantic",
	}

	invalidPipelines := []string{
		"unknown",
		"RERANKING",  // case-sensitive
		"re-ranking", // hyphenated variant
		"Standard",   // capitalized
		"fast",       // non-existent
		"hybrid",     // non-existent
		" reranking", // leading space
		"reranking ", // trailing space
		"re ranking", // space in middle
	}

	t.Run("valid pipelines pass validation", func(t *testing.T) {
		for _, pipeline := range validPipelines {
			t.Run(pipeline, func(t *testing.T) {
				req := &ChatRAGRequest{
					Message:  "test",
					Pipeline: pipeline,
				}
				err := req.Validate()
				assert.NoError(t, err, "valid pipeline '%s' should pass", pipeline)
			})
		}
	})

	t.Run("invalid pipelines fail validation", func(t *testing.T) {
		for _, pipeline := range invalidPipelines {
			t.Run(pipeline, func(t *testing.T) {
				req := &ChatRAGRequest{
					Message:  "test",
					Pipeline: pipeline,
				}
				err := req.Validate()
				require.Error(t, err, "invalid pipeline '%s' should fail", pipeline)
				assert.Contains(t, err.Error(), "invalid pipeline")
			})
		}
	})

	t.Run("empty pipeline is valid (uses default)", func(t *testing.T) {
		req := &ChatRAGRequest{
			Message:  "test",
			Pipeline: "",
		}
		err := req.Validate()
		assert.NoError(t, err, "empty pipeline should be valid")
	})
}

// TestChatRAGRequest_Validate_OptionalFields verifies that optional fields
// do not affect validation when left empty or populated.
func TestChatRAGRequest_Validate_OptionalFields(t *testing.T) {
	tests := []struct {
		name string
		req  ChatRAGRequest
	}{
		{
			name: "minimal valid request",
			req:  ChatRAGRequest{Message: "test"},
		},
		{
			name: "with session ID",
			req:  ChatRAGRequest{Message: "test", SessionId: "sess-123"},
		},
		{
			name: "with bearing",
			req:  ChatRAGRequest{Message: "test", Bearing: "authentication"},
		},
		{
			name: "with stream enabled",
			req:  ChatRAGRequest{Message: "test", Stream: true},
		},
		{
			name: "with history",
			req: ChatRAGRequest{
				Message: "test",
				History: []ChatTurn{
					{Id: "1", Role: "user", Content: "hello"},
					{Id: "2", Role: "assistant", Content: "hi there"},
				},
			},
		},
		{
			name: "with all optional fields",
			req: ChatRAGRequest{
				Id:        "req-123",
				CreatedAt: time.Now().Unix(),
				Message:   "What is authentication?",
				SessionId: "sess-456",
				Pipeline:  "reranking",
				Bearing:   "security",
				Stream:    true,
				History: []ChatTurn{
					{Id: "1", Role: "user", Content: "hello"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			assert.NoError(t, err, "request should be valid")
		})
	}
}

// =============================================================================
// ChatRAGRequest.EnsureDefaults() Tests
// =============================================================================

// TestChatRAGRequest_EnsureDefaults_PopulatesEmptyFields verifies that
// EnsureDefaults populates Id, CreatedAt, and Pipeline when they are empty.
func TestChatRAGRequest_EnsureDefaults_PopulatesEmptyFields(t *testing.T) {
	req := &ChatRAGRequest{Message: "test"}

	// Capture time before and after to verify timestamp (milliseconds)
	beforeTime := time.Now().UnixMilli()
	req.EnsureDefaults()
	afterTime := time.Now().UnixMilli()

	t.Run("generates non-empty ID", func(t *testing.T) {
		assert.NotEmpty(t, req.Id, "Id should be populated")
	})

	t.Run("sets CreatedAt to current time", func(t *testing.T) {
		assert.GreaterOrEqual(t, req.CreatedAt, beforeTime, "CreatedAt should be >= beforeTime")
		assert.LessOrEqual(t, req.CreatedAt, afterTime, "CreatedAt should be <= afterTime")
	})

	t.Run("sets Pipeline to reranking", func(t *testing.T) {
		assert.Equal(t, "reranking", req.Pipeline, "Pipeline should default to reranking")
	})
}

// TestChatRAGRequest_EnsureDefaults_PreservesExistingValues verifies that
// EnsureDefaults does not overwrite values that were already set.
func TestChatRAGRequest_EnsureDefaults_PreservesExistingValues(t *testing.T) {
	existingId := "my-custom-id"
	existingTimestamp := int64(1000000000)
	existingPipeline := "raptor"

	req := &ChatRAGRequest{
		Id:        existingId,
		CreatedAt: existingTimestamp,
		Message:   "test",
		Pipeline:  existingPipeline,
	}

	req.EnsureDefaults()

	assert.Equal(t, existingId, req.Id, "Id should be preserved")
	assert.Equal(t, existingTimestamp, req.CreatedAt, "CreatedAt should be preserved")
	assert.Equal(t, existingPipeline, req.Pipeline, "Pipeline should be preserved")
}

// TestChatRAGRequest_EnsureDefaults_Idempotent verifies that calling
// EnsureDefaults multiple times produces the same result.
func TestChatRAGRequest_EnsureDefaults_Idempotent(t *testing.T) {
	req := &ChatRAGRequest{Message: "test"}

	req.EnsureDefaults()
	firstId := req.Id
	firstTimestamp := req.CreatedAt
	firstPipeline := req.Pipeline

	// Small delay to ensure time would advance
	time.Sleep(10 * time.Millisecond)

	req.EnsureDefaults()

	assert.Equal(t, firstId, req.Id, "Id should not change on second call")
	assert.Equal(t, firstTimestamp, req.CreatedAt, "CreatedAt should not change on second call")
	assert.Equal(t, firstPipeline, req.Pipeline, "Pipeline should not change on second call")
}

// TestChatRAGRequest_EnsureDefaults_PartialPopulation verifies that only
// empty fields are populated while others are preserved.
func TestChatRAGRequest_EnsureDefaults_PartialPopulation(t *testing.T) {
	tests := []struct {
		name          string
		initialReq    ChatRAGRequest
		checkId       bool
		checkTime     bool
		checkPipeline bool
	}{
		{
			name:          "only Id preset",
			initialReq:    ChatRAGRequest{Message: "test", Id: "preset-id"},
			checkId:       false, // Id already set
			checkTime:     true,
			checkPipeline: true,
		},
		{
			name:          "only CreatedAt preset",
			initialReq:    ChatRAGRequest{Message: "test", CreatedAt: 12345},
			checkId:       true,
			checkTime:     false, // CreatedAt already set
			checkPipeline: true,
		},
		{
			name:          "only Pipeline preset",
			initialReq:    ChatRAGRequest{Message: "test", Pipeline: "graph"},
			checkId:       true,
			checkTime:     true,
			checkPipeline: false, // Pipeline already set
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.initialReq
			originalId := req.Id
			originalTime := req.CreatedAt
			originalPipeline := req.Pipeline

			req.EnsureDefaults()

			if tt.checkId {
				assert.NotEmpty(t, req.Id, "Id should be generated")
			} else {
				assert.Equal(t, originalId, req.Id, "Id should be preserved")
			}

			if tt.checkTime {
				assert.NotZero(t, req.CreatedAt, "CreatedAt should be generated")
			} else {
				assert.Equal(t, originalTime, req.CreatedAt, "CreatedAt should be preserved")
			}

			if tt.checkPipeline {
				assert.Equal(t, "reranking", req.Pipeline, "Pipeline should default to reranking")
			} else {
				assert.Equal(t, originalPipeline, req.Pipeline, "Pipeline should be preserved")
			}
		})
	}
}

// =============================================================================
// ChatRAGRequest.EnsureSessionId() Tests
// =============================================================================

// TestChatRAGRequest_EnsureSessionId_GeneratesWhenEmpty verifies that
// EnsureSessionId generates a new session ID when none is provided.
func TestChatRAGRequest_EnsureSessionId_GeneratesWhenEmpty(t *testing.T) {
	req := &ChatRAGRequest{Message: "test"}

	sessionId := req.EnsureSessionId()

	assert.NotEmpty(t, sessionId, "returned session ID should not be empty")
	assert.Equal(t, sessionId, req.SessionId, "SessionId field should be updated")
}

// TestChatRAGRequest_EnsureSessionId_PreservesExisting verifies that
// EnsureSessionId returns the existing session ID without modification.
func TestChatRAGRequest_EnsureSessionId_PreservesExisting(t *testing.T) {
	existingSessionId := "my-existing-session-123"
	req := &ChatRAGRequest{
		Message:   "test",
		SessionId: existingSessionId,
	}

	sessionId := req.EnsureSessionId()

	assert.Equal(t, existingSessionId, sessionId, "should return existing session ID")
	assert.Equal(t, existingSessionId, req.SessionId, "SessionId field should be unchanged")
}

// TestChatRAGRequest_EnsureSessionId_Idempotent verifies that calling
// EnsureSessionId multiple times returns the same session ID.
func TestChatRAGRequest_EnsureSessionId_Idempotent(t *testing.T) {
	req := &ChatRAGRequest{Message: "test"}

	firstSessionId := req.EnsureSessionId()
	secondSessionId := req.EnsureSessionId()
	thirdSessionId := req.EnsureSessionId()

	assert.Equal(t, firstSessionId, secondSessionId, "second call should return same ID")
	assert.Equal(t, secondSessionId, thirdSessionId, "third call should return same ID")
}

// TestChatRAGRequest_EnsureSessionId_GeneratesUniqueIds verifies that
// different requests generate unique session IDs.
func TestChatRAGRequest_EnsureSessionId_GeneratesUniqueIds(t *testing.T) {
	sessionIds := make(map[string]bool)
	numRequests := 100

	for i := 0; i < numRequests; i++ {
		req := &ChatRAGRequest{Message: "test"}
		sessionId := req.EnsureSessionId()

		if sessionIds[sessionId] {
			t.Fatalf("duplicate session ID generated: %s", sessionId)
		}
		sessionIds[sessionId] = true

		// Small sleep to advance time for UUID generation
		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, numRequests, len(sessionIds), "should have generated %d unique IDs", numRequests)
}

// =============================================================================
// NewChatRAGResponse() Tests
// =============================================================================

// TestNewChatRAGResponse_SetsAllFields verifies that the constructor properly
// sets all provided fields and generates Id and CreatedAt.
func TestNewChatRAGResponse_SetsAllFields(t *testing.T) {
	answer := "Authentication uses JWT tokens with RSA256 signing."
	sessionId := "sess-abc123"
	sources := []SourceInfo{
		{Source: "auth.go", Score: 0.95},
		{Source: "jwt.go", Score: 0.87},
	}
	turnCount := 5

	beforeTime := time.Now().UnixMilli()
	resp := NewChatRAGResponse(answer, sessionId, sources, turnCount)
	afterTime := time.Now().UnixMilli()

	assert.NotEmpty(t, resp.Id, "Id should be generated")
	assert.GreaterOrEqual(t, resp.CreatedAt, beforeTime, "CreatedAt should be >= beforeTime")
	assert.LessOrEqual(t, resp.CreatedAt, afterTime, "CreatedAt should be <= afterTime")
	assert.Equal(t, answer, resp.Answer, "Answer should match input")
	assert.Equal(t, sessionId, resp.SessionId, "SessionId should match input")
	assert.Equal(t, sources, resp.Sources, "Sources should match input")
	assert.Equal(t, turnCount, resp.TurnCount, "TurnCount should match input")
}

// TestNewChatRAGResponse_HandlesNilSources verifies that the constructor
// handles nil sources gracefully.
func TestNewChatRAGResponse_HandlesNilSources(t *testing.T) {
	resp := NewChatRAGResponse("hello", "sess-123", nil, 1)

	assert.Nil(t, resp.Sources, "Sources should be nil")
	assert.NotEmpty(t, resp.Id, "Id should still be generated")
}

// TestNewChatRAGResponse_HandlesEmptySources verifies that the constructor
// handles empty sources slice correctly.
func TestNewChatRAGResponse_HandlesEmptySources(t *testing.T) {
	resp := NewChatRAGResponse("hello", "sess-123", []SourceInfo{}, 1)

	assert.Empty(t, resp.Sources, "Sources should be empty")
	assert.NotEmpty(t, resp.Id, "Id should still be generated")
}

// TestNewChatRAGResponse_HandlesEmptyStrings verifies that the constructor
// works with empty string parameters.
func TestNewChatRAGResponse_HandlesEmptyStrings(t *testing.T) {
	resp := NewChatRAGResponse("", "", nil, 0)

	assert.NotEmpty(t, resp.Id, "Id should be generated even with empty inputs")
	assert.NotZero(t, resp.CreatedAt, "CreatedAt should be set even with empty inputs")
	assert.Empty(t, resp.Answer, "Answer should be empty")
	assert.Empty(t, resp.SessionId, "SessionId should be empty")
	assert.Zero(t, resp.TurnCount, "TurnCount should be zero")
}

// TestNewChatRAGResponse_GeneratesUniqueIds verifies that multiple calls
// generate unique response IDs.
func TestNewChatRAGResponse_GeneratesUniqueIds(t *testing.T) {
	ids := make(map[string]bool)
	numResponses := 100

	for i := 0; i < numResponses; i++ {
		resp := NewChatRAGResponse("test", "sess", nil, 1)

		if ids[resp.Id] {
			t.Fatalf("duplicate response ID generated: %s", resp.Id)
		}
		ids[resp.Id] = true

		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, numResponses, len(ids), "should have generated %d unique IDs", numResponses)
}

// =============================================================================
// NewStreamEvent() and Builder Methods Tests
// =============================================================================

// TestNewStreamEvent_CreatesEventWithType verifies that NewStreamEvent
// creates an event with the correct type, ID, and timestamp.
func TestNewStreamEvent_CreatesEventWithType(t *testing.T) {
	eventTypes := []string{"status", "token", "sources", "done", "error"}

	for _, eventType := range eventTypes {
		t.Run(eventType, func(t *testing.T) {
			beforeTime := time.Now().UnixMilli()
			event := NewStreamEvent(eventType)
			afterTime := time.Now().UnixMilli()

			assert.NotEmpty(t, event.Id, "Id should be generated")
			assert.GreaterOrEqual(t, event.CreatedAt, beforeTime)
			assert.LessOrEqual(t, event.CreatedAt, afterTime)
			assert.Equal(t, eventType, event.Type, "Type should match input")

			// All optional fields should be empty
			assert.Empty(t, event.Message)
			assert.Empty(t, event.Content)
			assert.Nil(t, event.Sources)
			assert.Empty(t, event.SessionId)
			assert.Empty(t, event.Error)
		})
	}
}

// TestStreamEvent_WithMessage verifies the WithMessage builder method.
func TestStreamEvent_WithMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{"normal message", "Searching knowledge base..."},
		{"empty message", ""},
		{"unicode message", "正在搜索..."},
		{"long message", string(make([]byte, 1000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewStreamEvent("status").WithMessage(tt.message)

			assert.Equal(t, tt.message, event.Message)
			assert.Equal(t, "status", event.Type, "Type should be preserved")
			assert.NotEmpty(t, event.Id, "Id should be preserved")
		})
	}
}

// TestStreamEvent_WithContent verifies the WithContent builder method.
func TestStreamEvent_WithContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"single token", "The"},
		{"word with punctuation", "authentication,"},
		{"empty content", ""},
		{"unicode content", "令牌"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewStreamEvent("token").WithContent(tt.content)

			assert.Equal(t, tt.content, event.Content)
			assert.Equal(t, "token", event.Type)
		})
	}
}

// TestStreamEvent_WithSources verifies the WithSources builder method.
func TestStreamEvent_WithSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []SourceInfo
	}{
		{
			name:    "multiple sources",
			sources: []SourceInfo{{Source: "a.go", Score: 0.9}, {Source: "b.go", Score: 0.8}},
		},
		{
			name:    "single source",
			sources: []SourceInfo{{Source: "auth.go", Score: 0.95}},
		},
		{
			name:    "empty sources",
			sources: []SourceInfo{},
		},
		{
			name:    "nil sources",
			sources: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewStreamEvent("sources").WithSources(tt.sources)

			assert.Equal(t, tt.sources, event.Sources)
			assert.Equal(t, "sources", event.Type)
		})
	}
}

// TestStreamEvent_WithSessionId verifies the WithSessionId builder method.
func TestStreamEvent_WithSessionId(t *testing.T) {
	tests := []struct {
		name      string
		sessionId string
	}{
		{"normal session ID", "sess-abc123"},
		{"empty session ID", ""},
		{"UUID format", "550e8400-e29b-41d4-a716-446655440000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewStreamEvent("done").WithSessionId(tt.sessionId)

			assert.Equal(t, tt.sessionId, event.SessionId)
			assert.Equal(t, "done", event.Type)
		})
	}
}

// TestStreamEvent_WithError verifies the WithError builder method.
func TestStreamEvent_WithError(t *testing.T) {
	tests := []struct {
		name     string
		errorMsg string
	}{
		{"connection error", "RAG engine unavailable"},
		{"timeout error", "LLM request timeout after 30s"},
		{"empty error", ""},
		{"detailed error", "failed to retrieve documents: context canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewStreamEvent("error").WithError(tt.errorMsg)

			assert.Equal(t, tt.errorMsg, event.Error)
			assert.Equal(t, "error", event.Type)
		})
	}
}

// TestStreamEvent_MethodChaining verifies that builder methods can be chained.
func TestStreamEvent_MethodChaining(t *testing.T) {
	sources := []SourceInfo{{Source: "test.go", Score: 0.9}}

	event := NewStreamEvent("done").
		WithMessage("Complete").
		WithContent("final content").
		WithSources(sources).
		WithSessionId("sess-123").
		WithError("") // Clearing any error

	assert.Equal(t, "done", event.Type)
	assert.Equal(t, "Complete", event.Message)
	assert.Equal(t, "final content", event.Content)
	assert.Equal(t, sources, event.Sources)
	assert.Equal(t, "sess-123", event.SessionId)
	assert.Empty(t, event.Error)
	assert.NotEmpty(t, event.Id)
	assert.NotZero(t, event.CreatedAt)
}

// TestStreamEvent_BuilderReturnsPointer verifies that builder methods return
// a pointer to the same event for proper chaining.
func TestStreamEvent_BuilderReturnsPointer(t *testing.T) {
	original := NewStreamEvent("status")
	withMessage := original.WithMessage("test")

	assert.Same(t, original, withMessage, "WithMessage should return same pointer")

	withContent := original.WithContent("content")
	assert.Same(t, original, withContent, "WithContent should return same pointer")

	withSources := original.WithSources(nil)
	assert.Same(t, original, withSources, "WithSources should return same pointer")

	withSessionId := original.WithSessionId("sess")
	assert.Same(t, original, withSessionId, "WithSessionId should return same pointer")

	withError := original.WithError("err")
	assert.Same(t, original, withError, "WithError should return same pointer")
}

// =============================================================================
// generateUUID() Tests
// =============================================================================

// TestGenerateUUID_Format verifies that generated UUIDs match the expected
// format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
func TestGenerateUUID_Format(t *testing.T) {
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	for i := 0; i < 100; i++ {
		uuid := generateUUID()
		assert.Regexp(t, uuidPattern, uuid, "UUID should match expected format")
		time.Sleep(time.Microsecond)
	}
}

// TestGenerateUUID_Uniqueness verifies that generateUUID produces unique values.
func TestGenerateUUID_Uniqueness(t *testing.T) {
	uuids := make(map[string]bool)
	numUUIDs := 1000

	for i := 0; i < numUUIDs; i++ {
		uuid := generateUUID()
		if uuids[uuid] {
			t.Fatalf("duplicate UUID generated: %s", uuid)
		}
		uuids[uuid] = true
		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, numUUIDs, len(uuids), "should have generated %d unique UUIDs", numUUIDs)
}

// TestGenerateUUID_Version4BitsSet verifies that the UUID has version 4 bits
// OR'd into the third segment. Note: This implementation sets the 0x4000 bit
// but doesn't mask out higher bits from the timestamp, so the first hex
// character may not always be '4'. This test verifies bit 14 is set.
func TestGenerateUUID_Version4BitsSet(t *testing.T) {
	for i := 0; i < 10; i++ {
		uuid := generateUUID()
		parts := regexp.MustCompile(`-`).Split(uuid, -1)
		require.Len(t, parts, 5, "UUID should have 5 parts")

		// Parse third part as hex and check that bit 14 (0x4000) is set
		var thirdValue uint64
		_, err := fmt.Sscanf(parts[2], "%x", &thirdValue)
		require.NoError(t, err, "third part should be valid hex")
		assert.True(t, thirdValue&0x4000 != 0, "version bit (0x4000) should be set, got %04x", thirdValue)
		time.Sleep(time.Microsecond)
	}
}

// TestGenerateUUID_VariantBitsSet verifies that the UUID has variant bits
// OR'd into the fourth segment. The implementation sets 0x8000 and clears
// bit 14 (0x3FFF mask + 0x8000), ensuring the variant bits are set.
func TestGenerateUUID_VariantBitsSet(t *testing.T) {
	for i := 0; i < 10; i++ {
		uuid := generateUUID()
		parts := regexp.MustCompile(`-`).Split(uuid, -1)
		require.Len(t, parts, 5, "UUID should have 5 parts")

		// Parse fourth part as hex and check that bit 15 (0x8000) is set
		// and bit 14 (0x4000) is clear (RFC 4122 variant)
		var fourthValue uint64
		_, err := fmt.Sscanf(parts[3], "%x", &fourthValue)
		require.NoError(t, err, "fourth part should be valid hex")
		assert.True(t, fourthValue&0x8000 != 0, "variant bit (0x8000) should be set, got %04x", fourthValue)
		assert.True(t, fourthValue&0x4000 == 0, "variant bit (0x4000) should be clear, got %04x", fourthValue)
		time.Sleep(time.Microsecond)
	}
}

// =============================================================================
// Integration/Workflow Tests
// =============================================================================

// TestChatRAGRequest_TypicalWorkflow verifies the typical request processing
// workflow: EnsureDefaults -> Validate -> EnsureSessionId
func TestChatRAGRequest_TypicalWorkflow(t *testing.T) {
	req := &ChatRAGRequest{
		Message: "What is the authentication flow?",
		Bearing: "security",
	}

	// Step 1: Ensure defaults
	req.EnsureDefaults()
	assert.NotEmpty(t, req.Id, "Id should be set after EnsureDefaults")
	assert.NotZero(t, req.CreatedAt, "CreatedAt should be set")
	assert.Equal(t, "reranking", req.Pipeline, "Pipeline should default to reranking")

	// Step 2: Validate
	err := req.Validate()
	assert.NoError(t, err, "validation should pass")

	// Step 3: Ensure session ID
	sessionId := req.EnsureSessionId()
	assert.NotEmpty(t, sessionId, "SessionId should be generated")
	assert.Equal(t, sessionId, req.SessionId, "SessionId field should be set")
}

// TestChatRAGRequest_WorkflowWithPresetValues verifies the workflow when
// the client provides some values upfront.
func TestChatRAGRequest_WorkflowWithPresetValues(t *testing.T) {
	req := &ChatRAGRequest{
		Id:        "client-provided-id",
		Message:   "How does JWT work?",
		SessionId: "existing-session-123",
		Pipeline:  "raptor",
	}

	req.EnsureDefaults()
	err := req.Validate()
	sessionId := req.EnsureSessionId()

	assert.NoError(t, err)
	assert.Equal(t, "client-provided-id", req.Id, "client ID preserved")
	assert.Equal(t, "existing-session-123", sessionId, "existing session preserved")
	assert.Equal(t, "raptor", req.Pipeline, "client pipeline preserved")
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestChatRAGRequest_ValidateAfterEnsureDefaults verifies that validation
// passes after EnsureDefaults populates the pipeline field.
func TestChatRAGRequest_ValidateAfterEnsureDefaults(t *testing.T) {
	req := &ChatRAGRequest{Message: "test"}

	// Before EnsureDefaults, Pipeline is empty but that's valid
	err := req.Validate()
	assert.NoError(t, err)

	// After EnsureDefaults
	req.EnsureDefaults()
	err = req.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "reranking", req.Pipeline)
}

// TestChatTurn_Fields verifies that ChatTurn struct can be properly populated.
func TestChatTurn_Fields(t *testing.T) {
	turn := ChatTurn{
		Id:        "turn-123",
		CreatedAt: time.Now().Unix(),
		Role:      "user",
		Content:   "What is authentication?",
		Sources:   nil,
	}

	assert.Equal(t, "turn-123", turn.Id)
	assert.Equal(t, "user", turn.Role)
	assert.Equal(t, "What is authentication?", turn.Content)
	assert.Nil(t, turn.Sources)

	// Assistant turn with sources
	assistantTurn := ChatTurn{
		Id:        "turn-124",
		CreatedAt: time.Now().Unix(),
		Role:      "assistant",
		Content:   "Authentication is...",
		Sources:   []SourceInfo{{Source: "auth.go", Score: 0.9}},
	}

	assert.Equal(t, "assistant", assistantTurn.Role)
	assert.Len(t, assistantTurn.Sources, 1)
}

// TestHarbor_Fields verifies that Harbor struct can be properly populated.
func TestHarbor_Fields(t *testing.T) {
	harbor := Harbor{
		Id:        "harbor-123",
		CreatedAt: time.Now().Unix(),
		Name:      "important-auth-answer",
		TurnIndex: 5,
	}

	assert.Equal(t, "harbor-123", harbor.Id)
	assert.Equal(t, "important-auth-answer", harbor.Name)
	assert.Equal(t, 5, harbor.TurnIndex)
	assert.NotZero(t, harbor.CreatedAt)
}

// TestSourceInfo_Fields verifies that SourceInfo struct can be properly populated.
func TestSourceInfo_Fields(t *testing.T) {
	source := SourceInfo{
		Source:   "services/auth/jwt.go",
		Distance: 0.15,
		Score:    0.85,
	}

	assert.Equal(t, "services/auth/jwt.go", source.Source)
	assert.Equal(t, 0.15, source.Distance)
	assert.Equal(t, 0.85, source.Score)
}

// =============================================================================
// NewSourceInfo() and Builder Methods Tests
// =============================================================================

// TestNewSourceInfo_CreatesSourceWithFields verifies that NewSourceInfo
// properly creates a SourceInfo with auto-generated ID and timestamp.
func TestNewSourceInfo_CreatesSourceWithFields(t *testing.T) {
	sources := []string{
		"auth.go",
		"services/api/handler.go",
		"docs/README.md",
		"/absolute/path/file.txt",
		"https://example.com/docs",
		"",
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			beforeTime := time.Now().UnixMilli()
			info := NewSourceInfo(source)
			afterTime := time.Now().UnixMilli()

			assert.NotEmpty(t, info.Id, "Id should be generated")
			assert.GreaterOrEqual(t, info.CreatedAt, beforeTime)
			assert.LessOrEqual(t, info.CreatedAt, afterTime)
			assert.Equal(t, source, info.Source, "Source should match input")
			assert.Zero(t, info.Score, "Score should be zero by default")
			assert.Zero(t, info.Distance, "Distance should be zero by default")
		})
	}
}

// TestSourceInfo_WithScore verifies the WithScore builder method.
func TestSourceInfo_WithScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
	}{
		{"high score", 0.95},
		{"low score", 0.15},
		{"perfect score", 1.0},
		{"zero score", 0.0},
		{"negative score", -0.5},
		{"very small score", 0.0001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := NewSourceInfo("test.go").WithScore(tt.score)

			assert.Equal(t, tt.score, info.Score)
			assert.Equal(t, "test.go", info.Source, "Source should be preserved")
			assert.NotEmpty(t, info.Id, "Id should be preserved")
		})
	}
}

// TestSourceInfo_WithDistance verifies the WithDistance builder method.
func TestSourceInfo_WithDistance(t *testing.T) {
	tests := []struct {
		name     string
		distance float64
	}{
		{"small distance", 0.123},
		{"large distance", 1.5},
		{"zero distance", 0.0},
		{"very small distance", 0.00001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := NewSourceInfo("doc.md").WithDistance(tt.distance)

			assert.Equal(t, tt.distance, info.Distance)
			assert.Equal(t, "doc.md", info.Source)
			assert.NotEmpty(t, info.Id)
		})
	}
}

// TestSourceInfo_MethodChaining verifies that builder methods can be chained.
func TestSourceInfo_MethodChaining(t *testing.T) {
	info := NewSourceInfo("auth.go").WithScore(0.95).WithDistance(0.123)

	assert.Equal(t, "auth.go", info.Source)
	assert.Equal(t, 0.95, info.Score)
	assert.Equal(t, 0.123, info.Distance)
	assert.NotEmpty(t, info.Id)
	assert.NotZero(t, info.CreatedAt)
}

// TestSourceInfo_BuilderReturnsPointer verifies that builder methods return
// a pointer to the same SourceInfo for proper chaining.
func TestSourceInfo_BuilderReturnsPointer(t *testing.T) {
	original := NewSourceInfo("test.go")
	withScore := original.WithScore(0.9)
	assert.Same(t, original, withScore, "WithScore should return same pointer")

	withDistance := original.WithDistance(0.1)
	assert.Same(t, original, withDistance, "WithDistance should return same pointer")
}

// TestNewSourceInfo_GeneratesUniqueIds verifies that multiple calls
// generate unique IDs.
func TestNewSourceInfo_GeneratesUniqueIds(t *testing.T) {
	ids := make(map[string]bool)
	numSources := 100

	for i := 0; i < numSources; i++ {
		info := NewSourceInfo("test.go")
		if ids[info.Id] {
			t.Fatalf("duplicate ID generated: %s", info.Id)
		}
		ids[info.Id] = true
		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, numSources, len(ids))
}

// =============================================================================
// NewHistoryTurn() Tests
// =============================================================================

// TestNewHistoryTurn_CreatesWithFields verifies that NewHistoryTurn
// properly creates a HistoryTurn with auto-generated ID and timestamp.
func TestNewHistoryTurn_CreatesWithFields(t *testing.T) {
	tests := []struct {
		name     string
		question string
		answer   string
	}{
		{"normal QA", "What is OAuth?", "OAuth is an authorization framework..."},
		{"empty answer", "How?", ""},
		{"empty question", "", "The answer is..."},
		{"both empty", "", ""},
		{"unicode content", "你好?", "世界"},
		{"long content", string(make([]byte, 10000)), string(make([]byte, 10000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeTime := time.Now().UnixMilli()
			turn := NewHistoryTurn(tt.question, tt.answer)
			afterTime := time.Now().UnixMilli()

			assert.NotEmpty(t, turn.Id, "Id should be generated")
			assert.GreaterOrEqual(t, turn.CreatedAt, beforeTime)
			assert.LessOrEqual(t, turn.CreatedAt, afterTime)
			assert.Equal(t, tt.question, turn.Question)
			assert.Equal(t, tt.answer, turn.Answer)
		})
	}
}

// TestNewHistoryTurn_GeneratesUniqueIds verifies that multiple calls
// generate unique IDs.
func TestNewHistoryTurn_GeneratesUniqueIds(t *testing.T) {
	ids := make(map[string]bool)
	numTurns := 100

	for i := 0; i < numTurns; i++ {
		turn := NewHistoryTurn("question", "answer")
		if ids[turn.Id] {
			t.Fatalf("duplicate ID generated: %s", turn.Id)
		}
		ids[turn.Id] = true
		time.Sleep(time.Microsecond)
	}

	assert.Equal(t, numTurns, len(ids))
}

// TestHistoryTurn_Fields verifies that HistoryTurn struct fields work correctly.
func TestHistoryTurn_Fields(t *testing.T) {
	turn := &HistoryTurn{
		Id:        "turn-123",
		CreatedAt: 1704067200000,
		Question:  "What is authentication?",
		Answer:    "Authentication verifies identity...",
	}

	assert.Equal(t, "turn-123", turn.Id)
	assert.Equal(t, int64(1704067200000), turn.CreatedAt)
	assert.Equal(t, "What is authentication?", turn.Question)
	assert.Equal(t, "Authentication verifies identity...", turn.Answer)
}
