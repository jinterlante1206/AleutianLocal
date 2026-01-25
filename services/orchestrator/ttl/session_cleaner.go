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
	"log/slog"
)

// =============================================================================
// SEC-003: Cascading Session Deletes
// =============================================================================

// BatchDeleteResult contains the outcome of a batch delete operation.
//
// # Description
//
// Returned by BatchDeleteFunc to report how many objects were successfully
// deleted and how many failed.
//
// # Fields
//
//   - Successful: Number of objects successfully deleted.
//   - Failed: Number of objects that failed to delete.
type BatchDeleteResult struct {
	Successful int
	Failed     int
}

// BatchDeleteByFilterFunc deletes objects by class name and session_id filter.
//
// # Description
//
// Decouples the session cleaner from the concrete Weaviate client, allowing
// unit tests to inject mock implementations. This function performs a
// Weaviate batch delete WHERE session_id = <sessionID>.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - className: Weaviate class name (e.g., "Conversation").
//   - sessionID: The session_id value to filter on.
//
// # Outputs
//
//   - BatchDeleteResult: Counts of successful and failed deletions.
//   - error: Non-nil if the batch delete operation itself fails.
type BatchDeleteByFilterFunc func(ctx context.Context, className, sessionID string) (BatchDeleteResult, error)

// QuerySessionDocsFunc queries for document IDs that are session-scoped.
//
// # Description
//
// Finds all Document objects that have an inSession cross-reference pointing
// to the given session. Returns their Weaviate UUIDs for subsequent deletion.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - sessionID: The session_id to search for in the reference path.
//
// # Outputs
//
//   - []string: Weaviate UUIDs of session-scoped documents.
//   - error: Non-nil if the query fails.
type QuerySessionDocsFunc func(ctx context.Context, sessionID string) ([]string, error)

// DeleteByIDFunc deletes a single object by class name and ID.
//
// # Description
//
// Performs a single-object delete in Weaviate. Used for session-scoped
// documents that must be deleted individually after a reference query.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - className: Weaviate class name (e.g., "Document", "Session").
//   - id: Weaviate object UUID.
//
// # Outputs
//
//   - error: Non-nil if the delete fails.
type DeleteByIDFunc func(ctx context.Context, className, id string) error

// sessionCleaner implements SessionCleaner with injectable dependencies.
//
// # Description
//
// Performs cascading session deletes in three phases:
//  1. Batch delete Conversation objects by session_id
//  2. Query and delete session-scoped Documents by inSession reference
//  3. Delete the Session object and verify deletion
//
// All operations are injectable via function types for testability.
//
// # Fields
//
//   - batchDelete: Function to batch delete by filter.
//   - querySessionDocs: Function to query session-scoped document IDs.
//   - deleteByID: Function to delete a single object.
//   - verifier: DeletionVerifier for confirming session removal.
//
// # Thread Safety
//
// All methods are safe for concurrent use (no shared mutable state).
type sessionCleaner struct {
	batchDelete      BatchDeleteByFilterFunc
	querySessionDocs QuerySessionDocsFunc
	deleteByID       DeleteByIDFunc
	verifier         DeletionVerifier
}

// NewSessionCleaner creates a session cleaner with cascade support.
//
// # Description
//
// Creates a SessionCleaner that performs cascading deletes using the provided
// functions for Weaviate operations. The verifier is used to confirm the
// session object was actually removed after deletion.
//
// # Inputs
//
//   - batchDelete: Function to batch delete Conversation objects by session_id.
//   - querySessionDocs: Function to find session-scoped document UUIDs.
//   - deleteByID: Function to delete individual objects.
//   - verifier: DeletionVerifier for session confirmation.
//
// # Outputs
//
//   - SessionCleaner: Ready to perform cascading session deletes.
//
// # Examples
//
//	cleaner := NewSessionCleaner(batchDeleteFn, queryDocsFn, deleteFn, verifier)
//	result, err := cleaner.DeleteSessionWithCascade(ctx, expiredSession)
//	if err != nil {
//	    log.Printf("Cascade failed: %v", err)
//	}
//	fmt.Printf("Deleted %d turns, %d docs, session=%v\n",
//	    result.ConversationTurnsDeleted,
//	    result.SessionScopedDocsDeleted,
//	    result.SessionDeleted)
func NewSessionCleaner(
	batchDelete BatchDeleteByFilterFunc,
	querySessionDocs QuerySessionDocsFunc,
	deleteByID DeleteByIDFunc,
	verifier DeletionVerifier,
) SessionCleaner {
	return &sessionCleaner{
		batchDelete:      batchDelete,
		querySessionDocs: querySessionDocs,
		deleteByID:       deleteByID,
		verifier:         verifier,
	}
}

// DeleteSessionWithCascade deletes a session and all related data.
//
// # Description
//
// Performs a cascading delete in the following order:
//  1. Batch delete all Conversation objects where session_id matches
//  2. Query and delete all Documents with inSession reference to this session
//  3. Delete the Session object itself
//  4. Verify the Session deletion via SEC-005
//
// If Phase 1 or 2 encounters errors, the cascade continues to the next phase
// but errors are accumulated in the result. If Phase 3 fails, the session
// remains and will be retried on the next cleanup cycle.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - session: The expired session to delete.
//
// # Outputs
//
//   - SessionCleanupResult: Detailed counts of deleted objects per phase.
//   - error: Non-nil only if context is cancelled or a catastrophic failure occurs.
//
// # Limitations
//
//   - Not truly atomic; partial cascades may occur.
//   - If context is cancelled mid-cascade, partial cleanup is logged.
//   - Large sessions with many turns may be slow.
//
// # Assumptions
//
//   - Conversation objects have session_id text field.
//   - Documents use inSession cross-reference for session scope.
//   - Session exists in Weaviate (no-op delete if already gone).
func (c *sessionCleaner) DeleteSessionWithCascade(ctx context.Context, session ExpiredSession) (SessionCleanupResult, error) {
	result := SessionCleanupResult{
		SessionID: session.SessionID,
		Errors:    make([]CleanupError, 0),
	}

	slog.Info("ttl.session_cleaner: starting cascade delete",
		"session_id", session.SessionID,
		"weaviate_id", session.WeaviateID,
	)

	// Phase 1: Delete conversation turns
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("context cancelled before phase 1: %w", err)
	}
	c.deleteConversationTurns(ctx, session.SessionID, &result)

	// Phase 2: Delete session-scoped documents
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("context cancelled before phase 2: %w", err)
	}
	c.deleteSessionScopedDocuments(ctx, session.SessionID, &result)

	// Phase 3: Delete the session object itself
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("context cancelled before phase 3: %w", err)
	}
	c.deleteSession(ctx, session, &result)

	slog.Info("ttl.session_cleaner: cascade complete",
		"session_id", session.SessionID,
		"turns_deleted", result.ConversationTurnsDeleted,
		"docs_deleted", result.SessionScopedDocsDeleted,
		"session_deleted", result.SessionDeleted,
		"errors", len(result.Errors),
	)

	return result, nil
}

// deleteConversationTurns performs Phase 1: batch delete all Conversation objects
// for the given session.
//
// # Description
//
// Uses the batch delete API with a session_id filter to remove all
// conversation turns belonging to this session in a single operation.
func (c *sessionCleaner) deleteConversationTurns(ctx context.Context, sessionID string, result *SessionCleanupResult) {
	batchResult, err := c.batchDelete(ctx, "Conversation", sessionID)
	if err != nil {
		slog.Warn("ttl.session_cleaner: failed to batch delete conversation turns",
			"session_id", sessionID,
			"error", err,
		)
		result.Errors = append(result.Errors, CleanupError{
			WeaviateID: sessionID,
			Reason:     fmt.Sprintf("batch delete conversations failed: %v", err),
		})
		return
	}

	result.ConversationTurnsDeleted = batchResult.Successful

	if batchResult.Failed > 0 {
		slog.Warn("ttl.session_cleaner: some conversation turns failed to delete",
			"session_id", sessionID,
			"successful", batchResult.Successful,
			"failed", batchResult.Failed,
		)
		result.Errors = append(result.Errors, CleanupError{
			WeaviateID: sessionID,
			Reason:     fmt.Sprintf("%d conversation turns failed to delete", batchResult.Failed),
		})
	}

	if batchResult.Successful > 0 {
		slog.Debug("ttl.session_cleaner: conversation turns deleted",
			"session_id", sessionID,
			"count", batchResult.Successful,
		)
	}
}

// deleteSessionScopedDocuments performs Phase 2: query and delete session-scoped
// documents.
//
// # Description
//
// First queries for document UUIDs that reference this session via the
// inSession cross-reference, then deletes each document individually.
// Individual failures are logged but do not halt the cascade.
func (c *sessionCleaner) deleteSessionScopedDocuments(ctx context.Context, sessionID string, result *SessionCleanupResult) {
	docIDs, err := c.querySessionDocs(ctx, sessionID)
	if err != nil {
		slog.Warn("ttl.session_cleaner: failed to query session-scoped documents",
			"session_id", sessionID,
			"error", err,
		)
		result.Errors = append(result.Errors, CleanupError{
			WeaviateID: sessionID,
			Reason:     fmt.Sprintf("query session docs failed: %v", err),
		})
		return
	}

	if len(docIDs) == 0 {
		return
	}

	slog.Debug("ttl.session_cleaner: found session-scoped documents",
		"session_id", sessionID,
		"count", len(docIDs),
	)

	for _, docID := range docIDs {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, CleanupError{
				WeaviateID: docID,
				Reason:     "context cancelled during document deletion",
			})
			return
		}

		if err := c.deleteByID(ctx, "Document", docID); err != nil {
			slog.Warn("ttl.session_cleaner: failed to delete session-scoped document",
				"session_id", sessionID,
				"document_id", docID,
				"error", err,
			)
			result.Errors = append(result.Errors, CleanupError{
				WeaviateID: docID,
				Reason:     fmt.Sprintf("delete document failed: %v", err),
			})
		} else {
			result.SessionScopedDocsDeleted++
		}
	}
}

// deleteSession performs Phase 3: delete the Session object and verify.
//
// # Description
//
// Deletes the Session object by ID, then uses the DeletionVerifier (SEC-005)
// to confirm it was actually removed from Weaviate.
func (c *sessionCleaner) deleteSession(ctx context.Context, session ExpiredSession, result *SessionCleanupResult) {
	if err := c.deleteByID(ctx, "Session", session.WeaviateID); err != nil {
		slog.Warn("ttl.session_cleaner: failed to delete session object",
			"session_id", session.SessionID,
			"weaviate_id", session.WeaviateID,
			"error", err,
		)
		result.Errors = append(result.Errors, CleanupError{
			WeaviateID: session.WeaviateID,
			Reason:     fmt.Sprintf("delete session failed: %v", err),
		})
		return
	}

	// Verify deletion (SEC-005)
	verified, verifyErr := c.verifier.VerifySessionDeleted(ctx, session.WeaviateID)
	if verifyErr != nil || !verified {
		slog.Warn("ttl.session_cleaner: session deletion not verified",
			"session_id", session.SessionID,
			"weaviate_id", session.WeaviateID,
			"error", verifyErr,
		)
		result.Errors = append(result.Errors, CleanupError{
			WeaviateID: session.WeaviateID,
			Reason:     "session deletion not verified",
		})
		return
	}

	result.SessionDeleted = true
}

// NewNoopSessionCleaner creates a session cleaner that does nothing.
//
// # Description
//
// Returns a no-op SessionCleaner that reports zero deletions. Use this
// when cascading deletes should be disabled (e.g., testing the scheduler
// without side effects).
//
// # Outputs
//
//   - SessionCleaner: Always returns empty SessionCleanupResult.
func NewNoopSessionCleaner() SessionCleaner {
	return &noopSessionCleaner{}
}

// noopSessionCleaner does nothing and always succeeds.
type noopSessionCleaner struct{}

func (c *noopSessionCleaner) DeleteSessionWithCascade(_ context.Context, session ExpiredSession) (SessionCleanupResult, error) {
	return SessionCleanupResult{
		SessionID:      session.SessionID,
		SessionDeleted: true,
		Errors:         make([]CleanupError, 0),
	}, nil
}
