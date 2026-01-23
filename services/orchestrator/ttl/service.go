// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package ttl provides time-to-live (TTL) management for documents and sessions
// in the Aleutian RAG system. It implements automatic expiration and cleanup
// for GDPR/CCPA compliance.
package ttl

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
	"github.com/weaviate/weaviate/entities/models"
)

// =============================================================================
// TTL Service Implementation
// =============================================================================

// ttlService implements TTLService interface for Weaviate-backed TTL operations.
//
// # Description
//
// Provides methods for querying expired documents and sessions from Weaviate
// and performing batch deletions with rollback support. Thread-safe for use
// with background schedulers.
//
// # Fields
//
//   - client: Weaviate client for database operations.
//   - clockChecker: Validates system clock before time-sensitive operations.
//   - verifier: Performs read-after-delete checks for compliance verification.
//   - sessionCleaner: Performs cascading session deletes (SEC-003).
//
// # Thread Safety
//
// All methods are thread-safe. The Weaviate client handles connection pooling.
type ttlService struct {
	client         *weaviate.Client
	clockChecker   ClockChecker
	verifier       DeletionVerifier
	sessionCleaner SessionCleaner
}

// NewTTLService creates a new TTL service backed by Weaviate.
//
// # Description
//
// Creates a TTL service that queries Weaviate for expired documents and sessions.
// The service implements the TTLService interface and can be used with the
// TTLScheduler for background cleanup operations.
//
// Uses the default clock checker for time validation. To customize, use
// NewTTLServiceWithClock.
//
// # Inputs
//
//   - client: Weaviate client instance. Must not be nil.
//
// # Outputs
//
//   - TTLService: Ready-to-use service implementing TTL operations.
//
// # Examples
//
//	client, _ := weaviate.NewClient(cfg)
//	service := NewTTLService(client)
//
//	expired, err := service.GetExpiredDocuments(ctx, 1000)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Requires Weaviate to have the Document and Session schemas with ttl_expires_at field.
//   - Clock checker may reject operations if system time appears invalid.
//
// # Assumptions
//
//   - Weaviate client is properly configured and connected.
//   - The ttl_expires_at field is indexed for efficient querying.
func NewTTLService(client *weaviate.Client) TTLService {
	verifier := NewDeletionVerifier(newWeaviateExistsFunc(client), 100*time.Millisecond, 3)
	return &ttlService{
		client:       client,
		clockChecker: NewClockChecker(),
		verifier:     verifier,
		sessionCleaner: NewSessionCleaner(
			newWeaviateBatchDeleteFunc(client),
			newWeaviateQuerySessionDocsFunc(client),
			newWeaviateDeleteByIDFunc(client),
			verifier,
		),
	}
}

// NewTTLServiceWithClock creates a new TTL service with a custom clock checker.
//
// # Description
//
// Creates a TTL service with an explicitly provided ClockChecker instance.
// Use this for testing or when custom clock validation bounds are needed.
//
// # Inputs
//
//   - client: Weaviate client instance. Must not be nil.
//   - clockChecker: Clock validation instance. Must not be nil.
//
// # Outputs
//
//   - TTLService: Ready-to-use service implementing TTL operations.
//
// # Example
//
//	checker := NewClockCheckerWithConfig(ClockConfig{...})
//	service := NewTTLServiceWithClock(client, checker)
func NewTTLServiceWithClock(client *weaviate.Client, clockChecker ClockChecker) TTLService {
	verifier := NewDeletionVerifier(newWeaviateExistsFunc(client), 100*time.Millisecond, 3)
	return &ttlService{
		client:       client,
		clockChecker: clockChecker,
		verifier:     verifier,
		sessionCleaner: NewSessionCleaner(
			newWeaviateBatchDeleteFunc(client),
			newWeaviateQuerySessionDocsFunc(client),
			newWeaviateDeleteByIDFunc(client),
			verifier,
		),
	}
}

// NewTTLServiceWithVerifier creates a TTL service with custom verifier and cleaner.
//
// # Description
//
// Creates a TTL service with explicitly provided ClockChecker,
// DeletionVerifier, and SessionCleaner instances. Use this for testing
// or when custom behavior is needed.
//
// # Inputs
//
//   - client: Weaviate client instance. Must not be nil.
//   - clockChecker: Clock validation instance. Must not be nil.
//   - verifier: Deletion verification instance. Must not be nil.
//   - sessionCleaner: Session cascade deletion instance. Must not be nil.
//
// # Outputs
//
//   - TTLService: Ready-to-use service implementing TTL operations.
func NewTTLServiceWithVerifier(client *weaviate.Client, clockChecker ClockChecker, verifier DeletionVerifier, sessionCleaner SessionCleaner) TTLService {
	return &ttlService{
		client:         client,
		clockChecker:   clockChecker,
		verifier:       verifier,
		sessionCleaner: sessionCleaner,
	}
}

// GetExpiredDocuments returns document IDs that have passed their TTL.
//
// # Description
//
// Queries Weaviate for Document objects where ttl_expires_at > 0 AND
// ttl_expires_at < current time in milliseconds. Documents with ttl_expires_at = 0
// are considered non-expiring and are never returned.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - limit: Maximum number of expired documents to return.
//
// # Outputs
//
//   - []ExpiredDocument: Slice of expired documents with their metadata.
//   - error: Non-nil if query fails.
//
// # Examples
//
//	docs, err := service.GetExpiredDocuments(ctx, 1000)
//	if err != nil {
//	    return fmt.Errorf("failed to query expired docs: %w", err)
//	}
//	for _, doc := range docs {
//	    fmt.Printf("Expired: %s (parent: %s)\n", doc.WeaviateID, doc.ParentSource)
//	}
//
// # Limitations
//
//   - Large limits may result in slow queries or timeouts.
//   - Returns only document metadata, not content.
//
// # Assumptions
//
//   - Weaviate is available and accessible.
//   - The Document schema includes ttl_expires_at field indexed for filtering.
func (s *ttlService) GetExpiredDocuments(ctx context.Context, limit int) ([]ExpiredDocument, error) {
	currentTimeMs, err := s.clockChecker.CurrentTimeMs()
	if err != nil {
		return nil, fmt.Errorf("clock sanity check failed, refusing TTL query: %w", err)
	}

	// Build filter: ttl_expires_at > 0 AND ttl_expires_at < currentTimeMs
	// This ensures we only get documents with TTL set (not 0) that have expired
	where := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"ttl_expires_at"}).
				WithOperator(filters.GreaterThan).
				WithValueNumber(0),
			filters.Where().
				WithPath([]string{"ttl_expires_at"}).
				WithOperator(filters.LessThan).
				WithValueNumber(float64(currentTimeMs)),
		})

	result, err := s.client.GraphQL().Get().
		WithClassName("Document").
		WithWhere(where).
		WithLimit(limit).
		WithFields(
			graphql.Field{Name: "_additional { id }"},
			graphql.Field{Name: "parent_source"},
			graphql.Field{Name: "data_space"},
			graphql.Field{Name: "ttl_expires_at"},
			graphql.Field{Name: "ingested_at"},
		).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to query expired documents: %w", err)
	}

	return parseExpiredDocuments(result)
}

// GetExpiredSessions returns session IDs that have passed their TTL.
//
// # Description
//
// Queries Weaviate for Session objects where ttl_expires_at > 0 AND
// ttl_expires_at < current time in milliseconds. Sessions with ttl_expires_at = 0
// are considered non-expiring and are never returned.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - limit: Maximum number of expired sessions to return.
//
// # Outputs
//
//   - []ExpiredSession: Slice of expired sessions with their metadata.
//   - error: Non-nil if query fails.
//
// # Examples
//
//	sessions, err := service.GetExpiredSessions(ctx, 100)
//	if err != nil {
//	    return fmt.Errorf("failed to query expired sessions: %w", err)
//	}
//	for _, sess := range sessions {
//	    fmt.Printf("Expired session: %s\n", sess.SessionID)
//	}
//
// # Limitations
//
//   - Does not delete associated conversation turns - caller must handle cascade.
//   - Large limits may result in slow queries.
//
// # Assumptions
//
//   - Weaviate is available and accessible.
//   - The Session schema includes ttl_expires_at field indexed for filtering.
func (s *ttlService) GetExpiredSessions(ctx context.Context, limit int) ([]ExpiredSession, error) {
	currentTimeMs, err := s.clockChecker.CurrentTimeMs()
	if err != nil {
		return nil, fmt.Errorf("clock sanity check failed, refusing TTL query: %w", err)
	}

	// Build filter: ttl_expires_at > 0 AND ttl_expires_at < currentTimeMs
	where := filters.Where().
		WithOperator(filters.And).
		WithOperands([]*filters.WhereBuilder{
			filters.Where().
				WithPath([]string{"ttl_expires_at"}).
				WithOperator(filters.GreaterThan).
				WithValueNumber(0),
			filters.Where().
				WithPath([]string{"ttl_expires_at"}).
				WithOperator(filters.LessThan).
				WithValueNumber(float64(currentTimeMs)),
		})

	result, err := s.client.GraphQL().Get().
		WithClassName("Session").
		WithWhere(where).
		WithLimit(limit).
		WithFields(
			graphql.Field{Name: "_additional { id }"},
			graphql.Field{Name: "session_id"},
			graphql.Field{Name: "ttl_expires_at"},
			graphql.Field{Name: "timestamp"},
		).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to query expired sessions: %w", err)
	}

	return parseExpiredSessions(result)
}

// DeleteExpiredBatch deletes a batch of expired documents with rollback support.
//
// # Description
//
// Attempts to delete all documents in the batch. If any deletion fails,
// the operation is considered failed. Weaviate batch delete is not truly
// atomic, so rollback is best-effort - documents that were successfully
// deleted before the failure will remain deleted.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - docs: Slice of expired documents to delete.
//
// # Outputs
//
//   - CleanupResult: Summary of the cleanup operation.
//   - error: Non-nil if batch delete fails entirely.
//
// # Examples
//
//	result, err := service.DeleteExpiredBatch(ctx, expiredDocs)
//	if err != nil {
//	    log.Printf("Batch delete failed: %v", err)
//	}
//	if result.HasErrors() {
//	    for _, e := range result.Errors {
//	        log.Printf("Failed to delete %s: %s", e.WeaviateID, e.Reason)
//	    }
//	}
//
// # Limitations
//
//   - Weaviate batch delete is not truly atomic; rollback is best-effort.
//   - Large batches may timeout; use smaller batch sizes.
//   - Documents are deleted individually, not via batch delete API.
//
// # Assumptions
//
//   - Weaviate is available and accessible.
//   - Documents exist in Weaviate (no-op if already deleted).
func (s *ttlService) DeleteExpiredBatch(ctx context.Context, docs []ExpiredDocument) (CleanupResult, error) {
	result := CleanupResult{
		StartTime:      time.Now(),
		DocumentsFound: len(docs),
		Errors:         make([]CleanupError, 0),
	}

	for _, doc := range docs {
		err := s.client.Data().Deleter().
			WithClassName("Document").
			WithID(doc.WeaviateID).
			Do(ctx)

		if err != nil {
			slog.Warn("Failed to delete expired document",
				"weaviate_id", doc.WeaviateID,
				"parent_source", doc.ParentSource,
				"error", err,
			)
			result.Errors = append(result.Errors, CleanupError{
				WeaviateID: doc.WeaviateID,
				Reason:     err.Error(),
			})
		} else {
			// SEC-005: Verify deletion actually occurred
			verified, verifyErr := s.verifier.VerifyDocumentDeleted(ctx, doc.WeaviateID)
			if verifyErr != nil || !verified {
				slog.Warn("Deletion verification failed",
					"weaviate_id", doc.WeaviateID,
					"parent_source", doc.ParentSource,
					"error", verifyErr,
				)
				result.Errors = append(result.Errors, CleanupError{
					WeaviateID: doc.WeaviateID,
					Reason:     "deletion not verified",
				})
			} else {
				result.DocumentsDeleted++
				slog.Debug("Deleted and verified expired document",
					"weaviate_id", doc.WeaviateID,
					"parent_source", doc.ParentSource,
					"data_space", doc.DataSpace,
				)
			}
		}
	}

	result.EndTime = time.Now()

	// Check if we should consider this batch as rolled back (partial failure)
	if result.HasErrors() && result.DocumentsDeleted > 0 {
		// Partial failure - some documents deleted, some failed
		// Mark as rolled back to indicate incomplete operation
		result.RolledBack = true
		slog.Warn("Partial batch delete failure",
			"deleted", result.DocumentsDeleted,
			"failed", len(result.Errors),
			"total", result.DocumentsFound,
		)
	}

	return result, nil
}

// DeleteExpiredSessionBatch deletes a batch of expired sessions with rollback support.
//
// # Description
//
// Attempts to delete all sessions in the batch. If any deletion fails,
// the operation is considered failed. Associated conversation turns and
// session-scoped documents should be handled by cascade delete or
// separate cleanup.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - sessions: Slice of expired sessions to delete.
//
// # Outputs
//
//   - CleanupResult: Summary of the cleanup operation.
//   - error: Non-nil if batch delete fails entirely.
//
// # Examples
//
//	result, err := service.DeleteExpiredSessionBatch(ctx, expiredSessions)
//	if err != nil {
//	    log.Printf("Session batch delete failed: %v", err)
//	}
//
// # Limitations
//
//   - Does not delete associated conversation turns or session-scoped documents.
//   - Caller must handle cascade deletion separately.
//   - Weaviate batch delete is not truly atomic.
//
// # Assumptions
//
//   - Weaviate is available and accessible.
//   - Sessions exist in Weaviate (no-op if already deleted).
func (s *ttlService) DeleteExpiredSessionBatch(ctx context.Context, sessions []ExpiredSession) (CleanupResult, error) {
	result := CleanupResult{
		StartTime:     time.Now(),
		SessionsFound: len(sessions),
		Errors:        make([]CleanupError, 0),
	}

	for _, sess := range sessions {
		// SEC-003: Use cascading delete (conversations + docs + session + verify)
		cascadeResult, err := s.sessionCleaner.DeleteSessionWithCascade(ctx, sess)
		if err != nil {
			// Catastrophic failure (e.g., context cancelled)
			slog.Error("Session cascade delete failed catastrophically",
				"session_id", sess.SessionID,
				"weaviate_id", sess.WeaviateID,
				"error", err,
			)
			result.Errors = append(result.Errors, CleanupError{
				WeaviateID: sess.WeaviateID,
				Reason:     fmt.Sprintf("cascade failed: %v", err),
			})
			continue
		}

		if cascadeResult.SessionDeleted {
			result.SessionsDeleted++
		}

		// Accumulate child deletion counts into the documents counters
		// (conversation turns are counted as documents for reporting)
		result.DocumentsFound += cascadeResult.ConversationTurnsDeleted + cascadeResult.SessionScopedDocsDeleted
		result.DocumentsDeleted += cascadeResult.ConversationTurnsDeleted + cascadeResult.SessionScopedDocsDeleted

		// Accumulate errors from the cascade
		result.Errors = append(result.Errors, cascadeResult.Errors...)
	}

	result.EndTime = time.Now()

	// Check for partial failure
	if result.HasErrors() && result.SessionsDeleted > 0 {
		result.RolledBack = true
		slog.Warn("Partial session batch delete failure",
			"deleted", result.SessionsDeleted,
			"failed", len(result.Errors),
			"total", result.SessionsFound,
		)
	}

	return result, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// newWeaviateBatchDeleteFunc creates a BatchDeleteByFilterFunc backed by Weaviate.
//
// # Description
//
// Returns a function that performs a batch delete WHERE session_id = <value>.
// Uses Weaviate's batch ObjectsBatchDeleter API with a text equality filter.
//
// # Inputs
//
//   - client: Weaviate client instance.
//
// # Outputs
//
//   - BatchDeleteByFilterFunc: Function to batch delete by session_id.
func newWeaviateBatchDeleteFunc(client *weaviate.Client) BatchDeleteByFilterFunc {
	return func(ctx context.Context, className, sessionID string) (BatchDeleteResult, error) {
		where := filters.Where().
			WithPath([]string{"session_id"}).
			WithOperator(filters.Equal).
			WithValueText(sessionID)

		resp, err := client.Batch().ObjectsBatchDeleter().
			WithClassName(className).
			WithWhere(where).
			WithOutput("minimal").
			Do(ctx)

		if err != nil {
			return BatchDeleteResult{}, fmt.Errorf("batch delete failed for %s: %w", className, err)
		}

		if resp == nil || resp.Results == nil {
			return BatchDeleteResult{}, nil
		}

		return BatchDeleteResult{
			Successful: int(resp.Results.Successful),
			Failed:     int(resp.Results.Failed),
		}, nil
	}
}

// newWeaviateQuerySessionDocsFunc creates a QuerySessionDocsFunc backed by Weaviate.
//
// # Description
//
// Returns a function that queries for Document objects whose inSession
// cross-reference points to a Session with the given session_id.
// Uses a cross-reference path filter in the GraphQL query.
//
// # Inputs
//
//   - client: Weaviate client instance.
//
// # Outputs
//
//   - QuerySessionDocsFunc: Function to find session-scoped document UUIDs.
func newWeaviateQuerySessionDocsFunc(client *weaviate.Client) QuerySessionDocsFunc {
	return func(ctx context.Context, sessionID string) ([]string, error) {
		where := filters.Where().
			WithPath([]string{"inSession", "Session", "session_id"}).
			WithOperator(filters.Equal).
			WithValueText(sessionID)

		resp, err := client.GraphQL().Get().
			WithClassName("Document").
			WithWhere(where).
			WithFields(graphql.Field{Name: "_additional { id }"}).
			Do(ctx)

		if err != nil {
			return nil, fmt.Errorf("query session-scoped documents failed: %w", err)
		}

		return parseDocumentIDs(resp)
	}
}

// newWeaviateDeleteByIDFunc creates a DeleteByIDFunc backed by Weaviate.
//
// # Description
//
// Returns a function that deletes a single object by class name and UUID.
//
// # Inputs
//
//   - client: Weaviate client instance.
//
// # Outputs
//
//   - DeleteByIDFunc: Function to delete a single object.
func newWeaviateDeleteByIDFunc(client *weaviate.Client) DeleteByIDFunc {
	return func(ctx context.Context, className, id string) error {
		return client.Data().Deleter().
			WithClassName(className).
			WithID(id).
			Do(ctx)
	}
}

// newWeaviateExistsFunc creates an ObjectExistsFunc backed by a Weaviate client.
//
// # Description
//
// Returns a function that checks object existence using the Weaviate Data API.
// The function queries by class name and ID, returning true if the object exists.
//
// # Inputs
//
//   - client: Weaviate client instance.
//
// # Outputs
//
//   - ObjectExistsFunc: Function that checks object existence.
func newWeaviateExistsFunc(client *weaviate.Client) ObjectExistsFunc {
	return func(ctx context.Context, className, id string) (bool, error) {
		result, err := client.Data().ObjectsGetter().
			WithClassName(className).
			WithID(id).
			Do(ctx)

		if err != nil {
			// Weaviate returns an error for "not found" in some versions
			// Check if the error indicates the object doesn't exist
			if isNotFoundError(err) {
				return false, nil
			}
			return false, err
		}

		return result != nil && len(result) > 0, nil
	}
}

// isNotFoundError checks if a Weaviate error indicates an object was not found.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "404") ||
		strings.Contains(errMsg, "does not exist")
}

// documentQueryResult represents the structure of a Document GraphQL query result.
type documentQueryResult struct {
	Get struct {
		Document []documentResultItem `json:"Document"`
	} `json:"Get"`
}

// documentResultItem represents a single document from a query.
type documentResultItem struct {
	ParentSource string `json:"parent_source"`
	DataSpace    string `json:"data_space"`
	TTLExpiresAt int64  `json:"ttl_expires_at"`
	IngestedAt   int64  `json:"ingested_at"`
	Additional   struct {
		ID string `json:"id"`
	} `json:"_additional"`
}

// sessionQueryResult represents the structure of a Session GraphQL query result.
type sessionQueryResult struct {
	Get struct {
		Session []sessionResultItem `json:"Session"`
	} `json:"Get"`
}

// sessionResultItem represents a single session from a query.
type sessionResultItem struct {
	SessionID    string `json:"session_id"`
	TTLExpiresAt int64  `json:"ttl_expires_at"`
	Timestamp    int64  `json:"timestamp"`
	Additional   struct {
		ID string `json:"id"`
	} `json:"_additional"`
}

// parseExpiredDocuments converts a GraphQL response to ExpiredDocument slice.
//
// # Description
//
// Takes raw Weaviate GraphQL response and unmarshals it into typed
// ExpiredDocument structs for further processing.
//
// # Inputs
//
//   - resp: Raw GraphQL response from Weaviate client.
//
// # Outputs
//
//   - []ExpiredDocument: Parsed documents.
//   - error: Non-nil if parsing fails.
func parseExpiredDocuments(resp *models.GraphQLResponse) ([]ExpiredDocument, error) {
	if resp == nil || resp.Data == nil {
		return []ExpiredDocument{}, nil
	}

	// Marshal and unmarshal to convert map to typed struct
	jsonBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var result documentQueryResult
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal document query result: %w", err)
	}

	expired := make([]ExpiredDocument, 0, len(result.Get.Document))
	for _, doc := range result.Get.Document {
		expired = append(expired, ExpiredDocument{
			WeaviateID:   doc.Additional.ID,
			ParentSource: doc.ParentSource,
			DataSpace:    doc.DataSpace,
			TTLExpiresAt: doc.TTLExpiresAt,
			IngestedAt:   doc.IngestedAt,
		})
	}

	return expired, nil
}

// parseExpiredSessions converts a GraphQL response to ExpiredSession slice.
//
// # Description
//
// Takes raw Weaviate GraphQL response and unmarshals it into typed
// ExpiredSession structs for further processing.
//
// # Inputs
//
//   - resp: Raw GraphQL response from Weaviate client.
//
// # Outputs
//
//   - []ExpiredSession: Parsed sessions.
//   - error: Non-nil if parsing fails.
func parseExpiredSessions(resp *models.GraphQLResponse) ([]ExpiredSession, error) {
	if resp == nil || resp.Data == nil {
		return []ExpiredSession{}, nil
	}

	// Marshal and unmarshal to convert map to typed struct
	jsonBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var result sessionQueryResult
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session query result: %w", err)
	}

	expired := make([]ExpiredSession, 0, len(result.Get.Session))
	for _, sess := range result.Get.Session {
		expired = append(expired, ExpiredSession{
			WeaviateID:   sess.Additional.ID,
			SessionID:    sess.SessionID,
			TTLExpiresAt: sess.TTLExpiresAt,
			Timestamp:    sess.Timestamp,
		})
	}

	return expired, nil
}

// documentIDQueryResult represents the structure of a Document ID-only query result.
type documentIDQueryResult struct {
	Get struct {
		Document []struct {
			Additional struct {
				ID string `json:"id"`
			} `json:"_additional"`
		} `json:"Document"`
	} `json:"Get"`
}

// parseDocumentIDs extracts document UUIDs from a GraphQL response.
//
// # Description
//
// Parses a GraphQL response that only requested _additional { id } and returns
// the document UUIDs as strings. Used by the session cascade to find
// session-scoped documents for deletion.
//
// # Inputs
//
//   - resp: Raw GraphQL response from Weaviate client.
//
// # Outputs
//
//   - []string: Document UUIDs.
//   - error: Non-nil if parsing fails.
func parseDocumentIDs(resp *models.GraphQLResponse) ([]string, error) {
	if resp == nil || resp.Data == nil {
		return nil, nil
	}

	jsonBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var result documentIDQueryResult
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal document ID query: %w", err)
	}

	ids := make([]string, 0, len(result.Get.Document))
	for _, doc := range result.Get.Document {
		if doc.Additional.ID != "" {
			ids = append(ids, doc.Additional.ID)
		}
	}

	return ids, nil
}
