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
	"time"
)

// =============================================================================
// Interfaces
// =============================================================================

// TTLService defines the interface for TTL-related operations.
//
// # Description
//
// Provides methods for calculating TTL expiration timestamps, querying expired
// documents, and performing cleanup operations. Implementations must be
// thread-safe for use with background schedulers.
//
// # Methods
//
// All methods accept context.Context for cancellation support.
//
// # Limitations
//
//   - Batch delete operations may timeout for very large result sets.
//   - Clock synchronization between services is assumed.
//
// # Assumptions
//
//   - Weaviate is available and accessible.
//   - The Document and Session schemas include ttl_expires_at field.
type TTLService interface {
	// GetExpiredDocuments returns document IDs that have passed their TTL.
	//
	// # Description
	//
	// Queries Weaviate for Document objects where ttl_expires_at > 0 AND
	// ttl_expires_at < current time in milliseconds.
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
	// # Example
	//
	//   docs, err := service.GetExpiredDocuments(ctx, 1000)
	//   if err != nil { ... }
	//   for _, doc := range docs {
	//       fmt.Println(doc.WeaviateID, doc.ParentSource)
	//   }
	GetExpiredDocuments(ctx context.Context, limit int) ([]ExpiredDocument, error)

	// GetExpiredSessions returns session IDs that have passed their TTL.
	//
	// # Description
	//
	// Queries Weaviate for Session objects where ttl_expires_at > 0 AND
	// ttl_expires_at < current time in milliseconds.
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
	GetExpiredSessions(ctx context.Context, limit int) ([]ExpiredSession, error)

	// DeleteExpiredBatch deletes a batch of expired documents with rollback support.
	//
	// # Description
	//
	// Attempts to delete all documents in the batch. If any deletion fails,
	// the operation is considered failed and no documents are deleted (rollback).
	// This ensures atomicity at the batch level.
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
	// # Limitations
	//
	//   - Weaviate batch delete is not truly atomic; rollback is best-effort.
	//   - Large batches may timeout; use smaller batch sizes.
	DeleteExpiredBatch(ctx context.Context, docs []ExpiredDocument) (CleanupResult, error)

	// DeleteExpiredSessionBatch deletes a batch of expired sessions with rollback support.
	//
	// # Description
	//
	// Attempts to delete all sessions in the batch. If any deletion fails,
	// the operation is considered failed and no sessions are deleted (rollback).
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
	DeleteExpiredSessionBatch(ctx context.Context, sessions []ExpiredSession) (CleanupResult, error)
}

// TTLScheduler defines the interface for background TTL cleanup scheduling.
//
// # Description
//
// Manages the lifecycle of a background goroutine that periodically runs
// TTL cleanup operations. The scheduler uses the ticker + done channel pattern
// for graceful shutdown.
//
// # Limitations
//
//   - Only one scheduler should run per orchestrator instance.
//   - Scheduler does not persist state between restarts.
//
// # Assumptions
//
//   - The orchestrator manages the scheduler lifecycle.
//   - Context cancellation triggers graceful shutdown.
type TTLScheduler interface {
	// Start begins the background cleanup scheduler.
	//
	// # Description
	//
	// Starts a goroutine that runs cleanup at the configured interval.
	// The scheduler will continue running until Stop() is called or
	// the context is cancelled.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation. When cancelled, scheduler stops.
	//
	// # Outputs
	//
	//   - error: Non-nil if scheduler is already running or fails to start.
	//
	// # Example
	//
	//   err := scheduler.Start(ctx)
	//   if err != nil { ... }
	//   defer scheduler.Stop()
	Start(ctx context.Context) error

	// Stop gracefully stops the scheduler.
	//
	// # Description
	//
	// Signals the scheduler to stop and waits for the current cleanup cycle
	// to complete. Safe to call multiple times.
	//
	// # Outputs
	//
	//   - error: Non-nil if scheduler fails to stop cleanly.
	Stop() error

	// RunNow triggers an immediate cleanup cycle.
	//
	// # Description
	//
	// Performs a cleanup cycle immediately without waiting for the next
	// scheduled interval. Useful for manual invocation or testing.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//
	// # Outputs
	//
	//   - CleanupResult: Summary of the cleanup operation.
	//   - error: Non-nil if cleanup fails.
	RunNow(ctx context.Context) (CleanupResult, error)
}

// TTLLogger defines the interface for TTL audit logging with tamper-evident chain.
//
// # Description
//
// Provides dual-output logging for TTL cleanup operations with hash chain
// integrity for basic tamper detection. Structured logs go to slog (stdout/JSON)
// for general monitoring. Deletion records go to a dedicated audit file.
//
// # FOSS vs Enterprise
//
// FOSS provides basic tamper-evident logging (hash chain, file permissions).
// Enterprise provides advanced compliance features (HMAC signing, deletion
// proofs, external timestamp anchoring) via the AuditEventSink interface.
//
// # Hash Chain
//
// Each deletion record includes a hash of the previous record, creating a
// tamper-evident chain. If any record is modified, the chain will break
// during verification.
//
// # Limitations
//
//   - Log rotation must be handled externally (e.g., logrotate).
//   - File writes are synchronous; may impact performance on slow disks.
//   - Basic verification only (full forensic proofs require Enterprise).
//
// # Assumptions
//
//   - The log file path is writable.
//   - Caller handles log file rotation.
//   - System clock is reasonably accurate for timestamps.
type TTLLogger interface {
	// LogDeletion records a document or session deletion to the audit log.
	//
	// # Description
	//
	// Creates a DeletionRecord with the content hash of the deleted item,
	// links it to the previous record in the hash chain, and writes to both
	// slog and the audit file.
	//
	// # Inputs
	//
	//   - content: The content that was deleted (used to compute content hash).
	//   - weaviateID: UUID of the deleted object.
	//   - operation: Type of deletion ("delete_document" or "delete_session").
	//   - metadata: Additional fields (parent_source, session_id, data_space).
	//
	// # Outputs
	//
	//   - DeletionRecord: The record that was created and logged.
	//   - error: Non-nil if logging fails.
	LogDeletion(content []byte, weaviateID string, operation string, metadata DeletionMetadata) (DeletionRecord, error)

	// LogCleanup records a cleanup cycle summary to the audit log.
	//
	// # Description
	//
	// Writes a structured log entry containing the cleanup result to both
	// slog (for observability) and the dedicated audit file (for compliance).
	//
	// # Inputs
	//
	//   - result: CleanupResult containing operation details.
	//
	// # Outputs
	//
	//   - error: Non-nil if logging fails.
	LogCleanup(result CleanupResult) error

	// LogError records a cleanup error to the audit log.
	//
	// # Description
	//
	// Writes an error entry to both slog and the audit file. Includes
	// context string for debugging.
	//
	// # Inputs
	//
	//   - err: The error that occurred.
	//   - context: Description of what operation was being performed.
	//
	// # Outputs
	//
	//   - error: Non-nil if logging fails.
	LogError(err error, context string) error

	// VerifyChain performs basic verification of the hash chain integrity.
	//
	// # Description
	//
	// Reads deletion records and verifies that each record's PrevHash
	// matches the previous record's EntryHash. This is a basic tamper
	// detection mechanism for FOSS users.
	//
	// # FOSS vs Enterprise
	//
	// FOSS: Basic chain verification (hash linkage only)
	// Enterprise: Full forensic verification with HMAC, timestamps, proofs
	//
	// # Outputs
	//
	//   - valid: True if the chain linkage is valid.
	//   - breakIndex: Index of first broken link (-1 if valid).
	//   - error: Non-nil if verification fails to complete.
	VerifyChain() (valid bool, breakIndex int64, err error)

	// GetEntryCount returns the number of entries in the audit log.
	//
	// # Description
	//
	// Returns the count of deletion records for status reporting.
	// Used by `aleutian audit status` command.
	//
	// # Outputs
	//
	//   - count: Number of deletion records in the log.
	//   - error: Non-nil if reading fails.
	GetEntryCount() (int64, error)

	// GetLastEntry returns the most recent deletion record.
	//
	// # Description
	//
	// Returns the last entry for status reporting. Used by
	// `aleutian audit status` command.
	//
	// # Outputs
	//
	//   - record: The most recent DeletionRecord (nil if empty).
	//   - error: Non-nil if reading fails.
	GetLastEntry() (*DeletionRecord, error)

	// LogConfigChange records a dataspace configuration change to the audit log.
	//
	// # Description
	//
	// Creates an audit record when retention policies or other dataspace
	// configuration is modified. Essential for compliance — proves what
	// policy was in effect at the time of any deletion.
	//
	// Config change records are NOT part of the hash chain (they are
	// informational, not deletion events).
	//
	// # Inputs
	//
	//   - change: The configuration change details.
	//
	// # Outputs
	//
	//   - error: Non-nil if logging fails.
	LogConfigChange(change ConfigChangeRecord) error

	// ReopenLogFile closes and reopens the log file for rotation support.
	//
	// # Description
	//
	// Supports external log rotation (e.g., logrotate) by closing the current
	// file handle and opening a new one at the same path. Typically called in
	// response to SIGHUP signal after logrotate has moved the old file.
	// The hash chain state (sequence, prevHash) is preserved in memory.
	//
	// # Outputs
	//
	//   - error: Non-nil if reopen fails.
	//
	// # Limitations
	//
	//   - After rotation, the new file will not contain old records.
	//   - Chain verification across rotated files requires processing
	//     files in chronological order externally.
	ReopenLogFile() error

	// CheckLogSize returns the current log file size in bytes.
	//
	// # Description
	//
	// Returns the size of the audit log file for monitoring. Can be used
	// to trigger warnings when the file grows beyond an expected threshold.
	//
	// # Outputs
	//
	//   - int64: File size in bytes.
	//   - error: Non-nil if stat fails.
	CheckLogSize() (int64, error)

	// Close closes the audit log file.
	//
	// # Description
	//
	// Flushes pending writes and closes the file handle. Should be called
	// during graceful shutdown.
	//
	// # Outputs
	//
	//   - error: Non-nil if close fails.
	Close() error

	// VerifyFilePermissions checks that the audit log file has restricted permissions.
	//
	// # Description
	//
	// Verifies that the audit log file permissions have not been changed from the
	// expected restricted mode (0600). This detects external tampering or
	// misconfiguration that could expose sensitive audit data.
	//
	// # Outputs
	//
	//   - error: Non-nil if permissions are incorrect or verification fails.
	//
	// # Limitations
	//
	//   - Only checks Unix permission bits, not ACLs.
	//   - Does not verify ownership (use OS-level tools for that).
	//
	// # Assumptions
	//
	//   - The log file exists and is accessible.
	//   - Running on a Unix-like system with standard permission model.
	VerifyFilePermissions() error
}

// DeletionVerifier defines methods for verifying deletions actually occurred.
//
// # Description
//
// Performs read-after-delete checks to confirm that objects have been removed
// from Weaviate. This is essential for compliance: the audit log should not
// claim a document was deleted if it is still queryable.
//
// # Security Context
//
// Without verification, the delete API may succeed but the object could remain
// due to replication lag, partial network failures, or storage-level issues.
// This violates GDPR Article 17 (Right to Erasure) if the data subject's data
// is still accessible after a confirmed deletion.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type DeletionVerifier interface {
	// VerifyDocumentDeleted confirms a document no longer exists in Weaviate.
	//
	// # Description
	//
	// Performs a read-after-delete check with retry logic to handle
	// replication lag. Returns true if the document is confirmed gone.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - weaviateID: UUID of the supposedly deleted document.
	//
	// # Outputs
	//
	//   - bool: True if document is confirmed deleted (not found).
	//   - error: Non-nil if verification check itself fails after retries.
	VerifyDocumentDeleted(ctx context.Context, weaviateID string) (bool, error)

	// VerifySessionDeleted confirms a session no longer exists in Weaviate.
	//
	// # Description
	//
	// Performs a read-after-delete check with retry logic to handle
	// replication lag. Returns true if the session is confirmed gone.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - weaviateID: UUID of the supposedly deleted session.
	//
	// # Outputs
	//
	//   - bool: True if session is confirmed deleted (not found).
	//   - error: Non-nil if verification check itself fails after retries.
	VerifySessionDeleted(ctx context.Context, weaviateID string) (bool, error)
}

// SessionCleaner defines methods for complete session cleanup including cascades.
//
// # Description
//
// Provides cascading session deletion that includes all related data:
// conversation turns and session-scoped documents. This ensures GDPR
// compliance by preventing orphaned personal data after session expiration.
//
// # Cascade Order
//
//  1. Delete all Conversation objects for the session (batch by session_id)
//  2. Delete all session-scoped Document objects (query inSession ref, then delete)
//  3. Delete the Session object itself
//  4. Verify the session deletion (SEC-005)
//
// # Security Context
//
// Without cascading deletes, expired sessions leave orphaned conversation turns
// and session-scoped documents that remain queryable. This violates GDPR Article 17
// (Right to Erasure) as the user's questions and context documents persist after
// the session they belonged to has been removed.
//
// # Limitations
//
//   - Cascade is not truly atomic; Weaviate does not support transactions.
//   - Uses best-effort approach: if children are deleted but session fails,
//     the next cleanup cycle will retry the session deletion.
//   - Partial failures are logged for compliance reporting.
//
// # Thread Safety
//
// All methods are safe for concurrent use.
type SessionCleaner interface {
	// DeleteSessionWithCascade deletes a session and all related data.
	//
	// # Description
	//
	// Performs a cascading delete: first removes all conversation turns
	// and session-scoped documents, then deletes the session itself.
	// Each phase is logged for audit compliance.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout.
	//   - session: The expired session to delete.
	//
	// # Outputs
	//
	//   - SessionCleanupResult: Detailed counts of deleted objects per phase.
	//   - error: Non-nil if cleanup fails catastrophically (e.g., context cancelled).
	DeleteSessionWithCascade(ctx context.Context, session ExpiredSession) (SessionCleanupResult, error)
}

// SessionCleanupResult contains detailed cleanup statistics for a session cascade.
//
// # Description
//
// Tracks the outcome of each phase of the cascading delete operation.
// Used for audit logging and compliance reporting.
//
// # Fields
//
//   - SessionID: The session_id of the deleted session (for logging).
//   - SessionDeleted: Whether the Session object was successfully deleted.
//   - ConversationTurnsDeleted: Count of Conversation objects removed.
//   - SessionScopedDocsDeleted: Count of session-scoped Documents removed.
//   - Errors: Any errors encountered during the cascade.
type SessionCleanupResult struct {
	SessionID                string
	SessionDeleted           bool
	ConversationTurnsDeleted int
	SessionScopedDocsDeleted int
	Errors                   []CleanupError
}

// HasErrors returns true if any errors occurred during the cascade.
func (r *SessionCleanupResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// TotalDeleted returns the total number of objects deleted across all phases.
func (r *SessionCleanupResult) TotalDeleted() int {
	total := r.ConversationTurnsDeleted + r.SessionScopedDocsDeleted
	if r.SessionDeleted {
		total++
	}
	return total
}

// DeletionMetadata contains optional metadata for a deletion record.
type DeletionMetadata struct {
	ParentSource string
	SessionID    string
	DataSpace    string
}

// =============================================================================
// Types
// =============================================================================

// TTLFormat indicates which TTL duration format was parsed.
//
// # Description
//
// Used to track whether the user provided a simple format (30d, 24h) or
// ISO 8601 format (P30D, PT24H) for logging and debugging purposes.
type TTLFormat int

const (
	// TTLFormatSimple indicates a simple duration format (e.g., 30d, 24h, 1w).
	TTLFormatSimple TTLFormat = iota

	// TTLFormatISO8601 indicates ISO 8601 duration format (e.g., P30D, PT24H).
	TTLFormatISO8601
)

// String returns the human-readable name of the TTL format.
func (f TTLFormat) String() string {
	switch f {
	case TTLFormatSimple:
		return "simple"
	case TTLFormatISO8601:
		return "ISO8601"
	default:
		return "unknown"
	}
}

// TTLParseResult contains the parsed TTL information.
//
// # Description
//
// Returned by ParseTTLDuration, contains the parsed duration, calculated
// expiration timestamp, human-readable description, and detected format.
//
// # Fields
//
//   - Duration: The parsed time.Duration value.
//   - ExpiresAt: Unix milliseconds timestamp when the document/session expires.
//   - Description: Human-readable description (e.g., "90 days").
//   - Format: Which format was detected (simple or ISO 8601).
type TTLParseResult struct {
	Duration    time.Duration
	ExpiresAt   int64
	Description string
	Format      TTLFormat
}

// ExpiredDocument represents a document that has passed its TTL.
//
// # Description
//
// Contains the metadata needed to identify and delete an expired document
// from Weaviate.
//
// # Fields
//
//   - WeaviateID: The Weaviate object UUID for deletion.
//   - ParentSource: Original file name for logging.
//   - DataSpace: Data space for scoped reporting.
//   - TTLExpiresAt: Unix milliseconds when it expired.
//   - IngestedAt: Unix milliseconds when it was ingested.
type ExpiredDocument struct {
	WeaviateID   string
	ParentSource string
	DataSpace    string
	TTLExpiresAt int64
	IngestedAt   int64
}

// ExpiredSession represents a session that has passed its TTL.
//
// # Description
//
// Contains the metadata needed to identify and delete an expired session
// from Weaviate.
//
// # Fields
//
//   - WeaviateID: The Weaviate object UUID for deletion.
//   - SessionID: The session identifier for logging.
//   - TTLExpiresAt: Unix milliseconds when it expired.
//   - Timestamp: Unix milliseconds when session was created.
type ExpiredSession struct {
	WeaviateID   string
	SessionID    string
	TTLExpiresAt int64
	Timestamp    int64
}

// CleanupResult summarizes a TTL cleanup operation.
//
// # Description
//
// Contains timing information, counts of deleted items, any errors that
// occurred, and whether a rollback was performed.
//
// # Fields
//
//   - StartTime: When the cleanup cycle started.
//   - EndTime: When the cleanup cycle completed.
//   - DocumentsFound: Number of expired documents found.
//   - DocumentsDeleted: Number of documents successfully deleted.
//   - SessionsFound: Number of expired sessions found.
//   - SessionsDeleted: Number of sessions successfully deleted.
//   - Errors: Slice of individual cleanup errors.
//   - RolledBack: True if the batch was rolled back due to errors.
type CleanupResult struct {
	StartTime        time.Time
	EndTime          time.Time
	DocumentsFound   int
	DocumentsDeleted int
	SessionsFound    int
	SessionsDeleted  int
	Errors           []CleanupError
	RolledBack       bool
}

// Duration returns the total duration of the cleanup operation.
func (r *CleanupResult) Duration() time.Duration {
	return r.EndTime.Sub(r.StartTime)
}

// DurationMs returns the duration in milliseconds for logging.
func (r *CleanupResult) DurationMs() int64 {
	return r.Duration().Milliseconds()
}

// HasErrors returns true if any errors occurred during cleanup.
func (r *CleanupResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// CleanupError represents a single cleanup failure.
//
// # Description
//
// Records which Weaviate object failed to delete and why.
//
// # Fields
//
//   - WeaviateID: The UUID of the object that failed to delete.
//   - Reason: Human-readable error description.
type CleanupError struct {
	WeaviateID string
	Reason     string
}

// =============================================================================
// Hash Chain Types for Cryptographic Deletion Proof
// =============================================================================

// DeletionRecord represents a cryptographically validated deletion entry.
//
// # Description
//
// Each deletion is recorded with a hash of the deleted content and linked
// to the previous deletion record, creating a tamper-evident chain. If any
// record is modified after the fact, the chain will break during verification.
//
// # Fields
//
//   - Sequence: Monotonically increasing sequence number.
//   - Timestamp: RFC3339 formatted timestamp of deletion.
//   - Operation: Type of operation ("delete_document", "delete_session").
//   - ContentHash: SHA-256 hash of the deleted content (hex encoded).
//   - WeaviateID: UUID of the deleted object.
//   - ParentSource: Original file name (for documents).
//   - DataSpace: Data space the object belonged to.
//   - PrevHash: SHA-256 hash of the previous DeletionRecord (hex encoded).
//   - EntryHash: SHA-256 hash of this entire record (hex encoded).
//
// # Hash Chain Verification
//
// To verify the chain integrity:
//  1. Start from the first record (PrevHash should be genesis hash)
//  2. Recompute EntryHash from record fields
//  3. Verify computed hash matches stored EntryHash
//  4. Verify next record's PrevHash matches this EntryHash
//  5. Repeat for all records
type DeletionRecord struct {
	Sequence     int64  `json:"sequence"`
	Timestamp    string `json:"timestamp"`
	Operation    string `json:"operation"`
	ContentHash  string `json:"content_hash"`
	WeaviateID   string `json:"weaviate_id"`
	ParentSource string `json:"parent_source,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	DataSpace    string `json:"data_space,omitempty"`
	PrevHash     string `json:"prev_hash"`
	EntryHash    string `json:"entry_hash"`
}

// DeletionProof contains the information needed to verify a specific deletion.
//
// # Description
//
// Contains the deletion record and chain verification status.
//
// # FOSS vs Enterprise
//
// This type is defined in FOSS for compatibility but full proof generation
// (with cryptographic signatures, external timestamps) requires Enterprise.
// FOSS can verify the record exists in the chain; Enterprise provides
// forensic-grade proofs for auditors.
//
// # Fields
//
//   - Record: The DeletionRecord for the deleted item.
//   - ChainValid: Whether the hash chain is valid up to this record.
//   - VerifiedAt: Timestamp when verification was performed.
type DeletionProof struct {
	Record     DeletionRecord `json:"record"`
	ChainValid bool           `json:"chain_valid"`
	VerifiedAt string         `json:"verified_at"`
}
