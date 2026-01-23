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
// AuditEventSink Interface (FOSS/Enterprise Integration Point)
// =============================================================================

// AuditEventSink allows external systems to receive audit events.
//
// # Description
//
// FOSS provides a default no-op implementation. Enterprise injects a BigQuery-backed
// implementation that captures all events for compliance verification and proof
// generation.
//
// This interface is the integration point between FOSS and Enterprise audit
// architectures. FOSS handles local tamper-evident logging; Enterprise captures
// events for forensic-grade compliance reporting.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use from multiple goroutines.
//
// # Error Handling
//
// Sink errors should not block TTL operations. Implementations should handle
// their own retry logic. Callers log errors but do not fail deletions.
//
// # Limitations
//
//   - Events are fire-and-forget from FOSS perspective
//   - No guaranteed delivery (Enterprise handles persistence)
//   - No backpressure mechanism
//
// # Assumptions
//
//   - Enterprise implementation handles its own connection management
//   - Events may arrive out of order in distributed deployments
type AuditEventSink interface {
	// OnTTLDeletion is called when a document or session is deleted due to TTL expiry.
	//
	// # Description
	//
	// Called after successful deletion from Weaviate. The event contains all
	// metadata needed for compliance tracking.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation (Enterprise may need network calls)
	//   - event: TTLDeletionEvent containing deletion details
	//
	// # Outputs
	//
	//   - error: Non-nil if sink fails (logged but not fatal to deletion)
	//
	// # Example
	//
	//   err := sink.OnTTLDeletion(ctx, TTLDeletionEvent{
	//       Timestamp:   time.Now(),
	//       DataspaceID: "work",
	//       ObjectType:  "document",
	//       WeaviateID:  "abc-123",
	//       ContentHash: "sha256:...",
	//   })
	OnTTLDeletion(ctx context.Context, event TTLDeletionEvent) error

	// OnSessionDeleted is called when a session is explicitly deleted (not TTL).
	//
	// # Description
	//
	// Called for user-initiated or admin-initiated session deletions, not
	// automatic TTL expiry (use OnTTLDeletion for that).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - event: SessionDeletionEvent containing deletion details
	//
	// # Outputs
	//
	//   - error: Non-nil if sink fails (logged but not fatal)
	OnSessionDeleted(ctx context.Context, event SessionDeletionEvent) error

	// OnDataspaceDeleted is called when a dataspace is deleted.
	//
	// # Description
	//
	// Called when an entire dataspace is removed. The event includes counts
	// of affected documents and sessions for audit trail purposes.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - event: DataspaceDeletionEvent containing deletion details
	//
	// # Outputs
	//
	//   - error: Non-nil if sink fails (logged but not fatal)
	OnDataspaceDeleted(ctx context.Context, event DataspaceDeletionEvent) error

	// OnConfigChanged is called when dataspace configuration changes.
	//
	// # Description
	//
	// Called when retention settings, TTL defaults, or other dataspace
	// configuration is modified. Useful for compliance audit trails.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - event: ConfigChangeEvent containing change details
	//
	// # Outputs
	//
	//   - error: Non-nil if sink fails (logged but not fatal)
	OnConfigChanged(ctx context.Context, event ConfigChangeEvent) error
}

// =============================================================================
// Event Types
// =============================================================================

// TTLDeletionEvent contains information about a TTL-triggered deletion.
//
// # Description
//
// Emitted when the TTL scheduler deletes an expired document or session.
// Contains all metadata needed for compliance tracking and audit trails.
//
// # Fields
//
//   - Timestamp: When the deletion occurred (server time)
//   - DataspaceID: Data space the object belonged to
//   - ObjectType: "document" or "session"
//   - WeaviateID: UUID of the deleted object
//   - ContentHash: SHA-256 hash of deleted content (hex encoded)
//   - TTLDuration: Original TTL that was set
//   - ParentSource: Original filename (for documents)
//   - SessionID: Session identifier (for sessions)
type TTLDeletionEvent struct {
	Timestamp    time.Time
	DataspaceID  string
	ObjectType   string // "document" or "session"
	WeaviateID   string
	ContentHash  string
	TTLDuration  time.Duration
	ParentSource string // For documents
	SessionID    string // For sessions
}

// SessionDeletionEvent contains information about explicit session deletion.
//
// # Description
//
// Emitted when a session is deleted by user request or admin action,
// not by automatic TTL expiry.
//
// # Fields
//
//   - Timestamp: When the deletion occurred
//   - SessionID: The session identifier
//   - DataspaceID: Data space the session belonged to
//   - Reason: Why the session was deleted ("user_request", "admin_action", etc.)
type SessionDeletionEvent struct {
	Timestamp   time.Time
	SessionID   string
	DataspaceID string
	Reason      string
}

// DataspaceDeletionEvent contains information about dataspace deletion.
//
// # Description
//
// Emitted when an entire dataspace is removed. Includes counts of
// affected data for audit trail completeness.
//
// # Fields
//
//   - Timestamp: When the deletion occurred
//   - DataspaceID: The dataspace that was deleted
//   - DocumentCount: Number of documents that were in the dataspace
//   - SessionCount: Number of sessions that were in the dataspace
//   - Reason: Why the dataspace was deleted
type DataspaceDeletionEvent struct {
	Timestamp     time.Time
	DataspaceID   string
	DocumentCount int
	SessionCount  int
	Reason        string
}

// ConfigChangeEvent contains information about configuration changes.
//
// # Description
//
// Emitted when dataspace configuration is modified. Captures before/after
// values for compliance audit trails.
//
// # Fields
//
//   - Timestamp: When the change occurred
//   - DataspaceID: Affected dataspace
//   - Field: Configuration field that changed
//   - OldValue: Previous value (empty string if new)
//   - NewValue: New value
//   - ChangedBy: Source of change ("cli", "api", "migration")
type ConfigChangeEvent struct {
	Timestamp   time.Time
	DataspaceID string
	Field       string
	OldValue    string
	NewValue    string
	ChangedBy   string
}

// =============================================================================
// Default FOSS Implementation
// =============================================================================

// noopAuditSink is the default FOSS implementation.
//
// # Description
//
// All methods are no-ops because FOSS handles audit logging separately via
// the TTLLogger. This sink exists as the integration point for Enterprise
// to inject its BigQuery-backed implementation.
//
// # Thread Safety
//
// Safe for concurrent use (stateless).
type noopAuditSink struct{}

// OnTTLDeletion is a no-op in FOSS (local TTLLogger handles this).
func (n *noopAuditSink) OnTTLDeletion(ctx context.Context, event TTLDeletionEvent) error {
	return nil
}

// OnSessionDeleted is a no-op in FOSS.
func (n *noopAuditSink) OnSessionDeleted(ctx context.Context, event SessionDeletionEvent) error {
	return nil
}

// OnDataspaceDeleted is a no-op in FOSS.
func (n *noopAuditSink) OnDataspaceDeleted(ctx context.Context, event DataspaceDeletionEvent) error {
	return nil
}

// OnConfigChanged is a no-op in FOSS.
func (n *noopAuditSink) OnConfigChanged(ctx context.Context, event ConfigChangeEvent) error {
	return nil
}

// DefaultAuditSink is the FOSS no-op implementation.
//
// # Description
//
// Use this as the default when no Enterprise license is present.
// Enterprise replaces this with its BigQuery-backed implementation.
//
// # Example
//
//	sink := ttl.DefaultAuditSink
//	// or
//	sink = enterpriseAuditSink // when Enterprise license detected
var DefaultAuditSink AuditEventSink = &noopAuditSink{}

// NewNoopAuditSink returns a new no-op audit sink instance.
//
// # Description
//
// Useful for testing or when you need a fresh instance rather than
// the package-level DefaultAuditSink.
func NewNoopAuditSink() AuditEventSink {
	return &noopAuditSink{}
}
