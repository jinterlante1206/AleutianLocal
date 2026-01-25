// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ttl

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// SEC-003: Cascading Session Delete Tests
// =============================================================================

// testSession creates a standard test session for use in tests.
func testSession() ExpiredSession {
	return ExpiredSession{
		WeaviateID:   "session-uuid-001",
		SessionID:    "sess-abc-123",
		TTLExpiresAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
		Timestamp:    time.Now().Add(-24 * time.Hour).UnixMilli(),
	}
}

// TestSessionCleaner_HappyPath tests that all three phases succeed cleanly.
func TestSessionCleaner_HappyPath(t *testing.T) {
	batchDelete := func(_ context.Context, className, sessionID string) (BatchDeleteResult, error) {
		if className != "Conversation" {
			t.Errorf("Expected batch delete on 'Conversation', got %q", className)
		}
		if sessionID != "sess-abc-123" {
			t.Errorf("Expected session_id 'sess-abc-123', got %q", sessionID)
		}
		return BatchDeleteResult{Successful: 5, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, sessionID string) ([]string, error) {
		if sessionID != "sess-abc-123" {
			t.Errorf("Expected session_id 'sess-abc-123', got %q", sessionID)
		}
		return []string{"doc-uuid-1", "doc-uuid-2"}, nil
	}

	deletedIDs := make([]string, 0)
	deleteByID := func(_ context.Context, className, id string) error {
		deletedIDs = append(deletedIDs, className+":"+id)
		return nil
	}

	verifier := NewNoopDeletionVerifier()

	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if result.ConversationTurnsDeleted != 5 {
		t.Errorf("Expected 5 turns deleted, got %d", result.ConversationTurnsDeleted)
	}
	if result.SessionScopedDocsDeleted != 2 {
		t.Errorf("Expected 2 docs deleted, got %d", result.SessionScopedDocsDeleted)
	}
	if !result.SessionDeleted {
		t.Error("Expected session to be marked as deleted")
	}
	if result.HasErrors() {
		t.Errorf("Expected no errors, got %d: %v", len(result.Errors), result.Errors)
	}
	if result.TotalDeleted() != 8 { // 5 turns + 2 docs + 1 session
		t.Errorf("Expected total 8 deleted, got %d", result.TotalDeleted())
	}

	// Verify delete order: docs first, then session
	expectedDeletes := []string{"Document:doc-uuid-1", "Document:doc-uuid-2", "Session:session-uuid-001"}
	if len(deletedIDs) != 3 {
		t.Fatalf("Expected 3 deleteByID calls, got %d: %v", len(deletedIDs), deletedIDs)
	}
	for i, expected := range expectedDeletes {
		if deletedIDs[i] != expected {
			t.Errorf("Delete call %d: expected %q, got %q", i, expected, deletedIDs[i])
		}
	}
}

// TestSessionCleaner_Phase1_BatchDeleteFails tests that a batch delete failure
// is recorded as an error but the cascade continues.
func TestSessionCleaner_Phase1_BatchDeleteFails(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{}, fmt.Errorf("weaviate unavailable")
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.ConversationTurnsDeleted != 0 {
		t.Errorf("Expected 0 turns deleted, got %d", result.ConversationTurnsDeleted)
	}
	// Cascade should still delete the session
	if !result.SessionDeleted {
		t.Error("Expected session to still be deleted despite phase 1 failure")
	}
	if !result.HasErrors() {
		t.Error("Expected at least one error from phase 1 failure")
	}
}

// TestSessionCleaner_Phase1_PartialBatchFailure tests that partial batch
// delete failures are tracked.
func TestSessionCleaner_Phase1_PartialBatchFailure(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 8, Failed: 2}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.ConversationTurnsDeleted != 8 {
		t.Errorf("Expected 8 turns deleted, got %d", result.ConversationTurnsDeleted)
	}
	if !result.HasErrors() {
		t.Error("Expected error for partial batch failure")
	}
}

// TestSessionCleaner_Phase2_QueryFails tests that a query failure for
// session-scoped documents is handled gracefully.
func TestSessionCleaner_Phase2_QueryFails(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 3, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, fmt.Errorf("graphql query timeout")
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.ConversationTurnsDeleted != 3 {
		t.Errorf("Expected 3 turns deleted, got %d", result.ConversationTurnsDeleted)
	}
	if result.SessionScopedDocsDeleted != 0 {
		t.Errorf("Expected 0 docs deleted, got %d", result.SessionScopedDocsDeleted)
	}
	// Session should still be deleted
	if !result.SessionDeleted {
		t.Error("Expected session to still be deleted despite phase 2 failure")
	}
	if !result.HasErrors() {
		t.Error("Expected error from query failure")
	}
}

// TestSessionCleaner_Phase2_IndividualDocDeleteFails tests that individual
// document delete failures don't stop the cascade.
func TestSessionCleaner_Phase2_IndividualDocDeleteFails(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 0, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return []string{"doc-1", "doc-2", "doc-3"}, nil
	}

	deleteByID := func(_ context.Context, className, id string) error {
		if className == "Document" && id == "doc-2" {
			return fmt.Errorf("permission denied")
		}
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.SessionScopedDocsDeleted != 2 {
		t.Errorf("Expected 2 docs deleted (1 failed), got %d", result.SessionScopedDocsDeleted)
	}
	if !result.SessionDeleted {
		t.Error("Expected session to still be deleted despite doc failure")
	}
	if !result.HasErrors() {
		t.Error("Expected error from failed doc delete")
	}
}

// TestSessionCleaner_Phase3_SessionDeleteFails tests that a session delete
// failure is reported correctly.
func TestSessionCleaner_Phase3_SessionDeleteFails(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 2, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	deleteByID := func(_ context.Context, className, _ string) error {
		if className == "Session" {
			return fmt.Errorf("delete failed: locked")
		}
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.SessionDeleted {
		t.Error("Expected session NOT to be marked as deleted")
	}
	if result.ConversationTurnsDeleted != 2 {
		t.Errorf("Expected 2 turns still deleted, got %d", result.ConversationTurnsDeleted)
	}
	if !result.HasErrors() {
		t.Error("Expected error from session delete failure")
	}
}

// TestSessionCleaner_Phase3_VerificationFails tests that a verification failure
// after session delete is reported correctly.
func TestSessionCleaner_Phase3_VerificationFails(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 0, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		return nil
	}

	// Verifier that always fails
	failVerifier := NewDeletionVerifier(
		func(_ context.Context, _, _ string) (bool, error) {
			return true, nil // Object still exists
		},
		1*time.Millisecond,
		2,
	)

	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, failVerifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no fatal error, got: %v", err)
	}
	if result.SessionDeleted {
		t.Error("Expected session NOT to be marked as deleted when verification fails")
	}
	if !result.HasErrors() {
		t.Error("Expected error from verification failure")
	}
}

// TestSessionCleaner_ContextCancelled_BeforePhase1 tests that context
// cancellation before phase 1 returns early.
func TestSessionCleaner_ContextCancelled_BeforePhase1(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		t.Error("batchDelete should not be called after context cancel")
		return BatchDeleteResult{}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		t.Error("queryDocs should not be called after context cancel")
		return nil, nil
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		t.Error("deleteByID should not be called after context cancel")
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	_, err := cleaner.DeleteSessionWithCascade(ctx, testSession())

	if err == nil {
		t.Fatal("Expected error from cancelled context")
	}
}

// TestSessionCleaner_ContextCancelled_DuringPhase2 tests that context
// cancellation during document deletion stops further deletes.
func TestSessionCleaner_ContextCancelled_DuringPhase2(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 1, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return []string{"doc-1", "doc-2", "doc-3", "doc-4"}, nil
	}

	deleteCount := 0
	deleteByID := func(_ context.Context, className, _ string) error {
		if className == "Document" {
			deleteCount++
			if deleteCount == 2 {
				cancel() // Cancel after second doc delete
			}
		}
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(ctx, testSession())

	// Context cancellation between phases returns error
	if err == nil {
		// If the cancel happened but ctx.Err() wasn't checked yet, we might get here
		// The important thing is that not all 4 docs were deleted
		if result.SessionScopedDocsDeleted == 4 {
			t.Error("Expected fewer than 4 docs deleted due to cancellation")
		}
	}

	if result.ConversationTurnsDeleted != 1 {
		t.Errorf("Expected 1 turn deleted before cancel, got %d", result.ConversationTurnsDeleted)
	}
}

// TestSessionCleaner_EmptySession tests that a session with no turns and no
// docs is handled cleanly.
func TestSessionCleaner_EmptySession(t *testing.T) {
	batchDelete := func(_ context.Context, _, _ string) (BatchDeleteResult, error) {
		return BatchDeleteResult{Successful: 0, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, _ string) ([]string, error) {
		return nil, nil // No session-scoped docs
	}

	deleteByID := func(_ context.Context, _, _ string) error {
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if result.ConversationTurnsDeleted != 0 {
		t.Errorf("Expected 0 turns, got %d", result.ConversationTurnsDeleted)
	}
	if result.SessionScopedDocsDeleted != 0 {
		t.Errorf("Expected 0 docs, got %d", result.SessionScopedDocsDeleted)
	}
	if !result.SessionDeleted {
		t.Error("Expected session to be deleted")
	}
	if result.TotalDeleted() != 1 { // Just the session
		t.Errorf("Expected total 1 deleted, got %d", result.TotalDeleted())
	}
	if result.HasErrors() {
		t.Errorf("Expected no errors, got: %v", result.Errors)
	}
}

// TestSessionCleaner_SessionID_PassedCorrectly tests that the session_id
// is passed correctly through all phases.
func TestSessionCleaner_SessionID_PassedCorrectly(t *testing.T) {
	session := ExpiredSession{
		WeaviateID:   "wv-uuid-999",
		SessionID:    "custom-session-id-xyz",
		TTLExpiresAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}

	var batchSessionID, querySessionID string

	batchDelete := func(_ context.Context, _, sessionID string) (BatchDeleteResult, error) {
		batchSessionID = sessionID
		return BatchDeleteResult{Successful: 0, Failed: 0}, nil
	}

	queryDocs := func(_ context.Context, sessionID string) ([]string, error) {
		querySessionID = sessionID
		return nil, nil
	}

	var deletedClassName, deletedID string
	deleteByID := func(_ context.Context, className, id string) error {
		deletedClassName = className
		deletedID = id
		return nil
	}

	verifier := NewNoopDeletionVerifier()
	cleaner := NewSessionCleaner(batchDelete, queryDocs, deleteByID, verifier)
	result, _ := cleaner.DeleteSessionWithCascade(context.Background(), session)

	if batchSessionID != "custom-session-id-xyz" {
		t.Errorf("Expected batch delete session_id 'custom-session-id-xyz', got %q", batchSessionID)
	}
	if querySessionID != "custom-session-id-xyz" {
		t.Errorf("Expected query session_id 'custom-session-id-xyz', got %q", querySessionID)
	}
	if deletedClassName != "Session" {
		t.Errorf("Expected delete className 'Session', got %q", deletedClassName)
	}
	if deletedID != "wv-uuid-999" {
		t.Errorf("Expected delete ID 'wv-uuid-999', got %q", deletedID)
	}
	if result.SessionID != "custom-session-id-xyz" {
		t.Errorf("Expected result.SessionID 'custom-session-id-xyz', got %q", result.SessionID)
	}
}

// TestNoopSessionCleaner_AlwaysSucceeds tests that the no-op cleaner
// always reports success.
func TestNoopSessionCleaner_AlwaysSucceeds(t *testing.T) {
	cleaner := NewNoopSessionCleaner()
	result, err := cleaner.DeleteSessionWithCascade(context.Background(), testSession())

	if err != nil {
		t.Fatalf("Noop cleaner should not error, got: %v", err)
	}
	if !result.SessionDeleted {
		t.Error("Noop cleaner should report session as deleted")
	}
	if result.HasErrors() {
		t.Error("Noop cleaner should have no errors")
	}
	if result.SessionID != "sess-abc-123" {
		t.Errorf("Expected session_id preserved, got %q", result.SessionID)
	}
}

// TestSessionCleanupResult_HasErrors tests the HasErrors helper method.
func TestSessionCleanupResult_HasErrors(t *testing.T) {
	r := SessionCleanupResult{Errors: make([]CleanupError, 0)}
	if r.HasErrors() {
		t.Error("Empty errors should return false")
	}

	r.Errors = append(r.Errors, CleanupError{WeaviateID: "x", Reason: "y"})
	if !r.HasErrors() {
		t.Error("Non-empty errors should return true")
	}
}

// TestSessionCleanupResult_TotalDeleted tests the TotalDeleted helper method.
func TestSessionCleanupResult_TotalDeleted(t *testing.T) {
	r := SessionCleanupResult{
		SessionDeleted:           true,
		ConversationTurnsDeleted: 10,
		SessionScopedDocsDeleted: 3,
	}
	if r.TotalDeleted() != 14 {
		t.Errorf("Expected 14, got %d", r.TotalDeleted())
	}

	r.SessionDeleted = false
	if r.TotalDeleted() != 13 {
		t.Errorf("Expected 13 without session, got %d", r.TotalDeleted())
	}
}
