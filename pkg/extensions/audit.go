// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package extensions

import (
	"context"
	"time"
)

// AuditEvent represents a security-relevant event for compliance logging.
//
// This struct captures the essential information needed for security audits,
// compliance reporting (GDPR, HIPAA, SOC2), and incident investigation.
//
// # Event Categories
//
// Events are categorized by type for filtering and alerting:
//   - Authentication: "auth.login", "auth.logout", "auth.failed"
//   - Authorization: "authz.denied", "authz.granted"
//   - Data Access: "data.read", "data.write", "data.delete"
//   - System: "system.start", "system.stop", "system.error"
//   - Chat: "chat.message", "chat.response", "chat.blocked"
//
// # Compliance Fields
//
// For regulatory compliance, always populate:
//   - UserID: Required for GDPR right-to-know requests
//   - Timestamp: Required for audit trail integrity
//   - ResourceType/ResourceID: Required for data lineage
//
// Example:
//
//	event := AuditEvent{
//	    EventType:    "chat.message",
//	    Timestamp:    time.Now().UTC(),
//	    UserID:       authInfo.UserID,
//	    Action:       "send",
//	    ResourceType: "message",
//	    ResourceID:   messageID,
//	    Outcome:      "success",
//	    Metadata: map[string]any{
//	        "session_id": sessionID,
//	        "model":      "claude-3",
//	    },
//	}
type AuditEvent struct {
	// EventType categorizes the event for filtering and alerting.
	// Format: "category.action" (e.g., "auth.login", "chat.message")
	EventType string

	// Timestamp is when the event occurred (always use UTC).
	// If zero, implementations should set to time.Now().UTC().
	Timestamp time.Time

	// UserID identifies who performed the action.
	// Use "system" for automated actions, "anonymous" if unknown.
	UserID string

	// Action describes what operation was attempted.
	// Common values: "create", "read", "update", "delete", "send", "receive"
	Action string

	// ResourceType is the category of resource involved.
	// Examples: "message", "session", "evaluation", "model"
	ResourceType string

	// ResourceID is the specific resource instance (optional).
	// Examples: "msg-123", "sess-456", "eval-789"
	ResourceID string

	// Outcome indicates the result of the action.
	// Values: "success", "failure", "blocked", "error"
	Outcome string

	// Metadata holds additional event-specific data.
	// This is where implementation-specific details go.
	//
	// Common metadata keys:
	//   - "error": error message if Outcome is "failure" or "error"
	//   - "ip_address": client IP for security analysis
	//   - "user_agent": client identifier
	//   - "duration_ms": operation duration for performance analysis
	//   - "model": AI model used
	//   - "session_id": conversation session
	Metadata map[string]any
}

// AuditFilter defines criteria for querying audit events.
//
// All fields are optional - only non-zero values are used as filters.
// Multiple fields are combined with AND logic.
//
// Example:
//
//	// Find all failed auth events in the last hour
//	filter := AuditFilter{
//	    EventTypes: []string{"auth.failed"},
//	    StartTime:  time.Now().Add(-time.Hour),
//	    EndTime:    time.Now(),
//	}
//	events, err := auditor.Query(ctx, filter)
type AuditFilter struct {
	// EventTypes limits results to specific event types.
	// If empty, all event types are included.
	EventTypes []string

	// UserID limits results to events from a specific user.
	// If empty, events from all users are included.
	UserID string

	// StartTime is the earliest event timestamp to include (inclusive).
	// If zero, no lower bound is applied.
	StartTime time.Time

	// EndTime is the latest event timestamp to include (exclusive).
	// If zero, no upper bound is applied.
	EndTime time.Time

	// ResourceType limits results to events involving specific resource types.
	// If empty, all resource types are included.
	ResourceType string

	// ResourceID limits results to events involving a specific resource.
	// If empty, all resources are included.
	ResourceID string

	// Outcome limits results to events with specific outcomes.
	// If empty, all outcomes are included.
	Outcome string

	// Limit is the maximum number of events to return.
	// If zero, implementation-specific default is used.
	Limit int

	// Offset is the number of events to skip (for pagination).
	Offset int
}

// AuditLogger records security-relevant events for compliance and analysis.
//
// Implementations must be safe for concurrent use by multiple goroutines.
// The Log method should be non-blocking or have reasonable timeouts to
// avoid impacting application performance.
//
// # Open Source Behavior
//
// The default NopAuditLogger discards all events. This is appropriate for
// local single-user deployments where audit trails aren't required.
//
// # Enterprise Implementation
//
// Enterprise versions send events to SIEM systems (Splunk, Datadog, ELK),
// cloud logging (CloudWatch, Stackdriver), or compliance databases.
//
// Example enterprise implementation:
//
//	type SplunkAuditLogger struct {
//	    client *splunk.Client
//	    index  string
//	}
//
//	func (l *SplunkAuditLogger) Log(ctx context.Context, event AuditEvent) error {
//	    if event.Timestamp.IsZero() {
//	        event.Timestamp = time.Now().UTC()
//	    }
//	    return l.client.Index(ctx, l.index, event)
//	}
//
// # Async vs Sync Logging
//
// Implementations may choose sync or async logging:
//   - Sync: Blocks until event is persisted (safer, slower)
//   - Async: Returns immediately, buffers events (faster, may lose events)
//
// For compliance-critical events, sync logging is recommended.
type AuditLogger interface {
	// Log records a security-relevant event.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - event: The audit event to record
	//
	// Returns:
	//   - error: nil on success, error if logging failed
	//
	// Implementations should:
	//   1. Set Timestamp if zero
	//   2. Validate required fields (EventType, UserID)
	//   3. Persist or transmit the event
	//   4. Return quickly (use async if needed)
	Log(ctx context.Context, event AuditEvent) error

	// Query retrieves audit events matching the filter criteria.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - filter: Criteria for selecting events
	//
	// Returns:
	//   - []AuditEvent: Matching events, ordered by Timestamp descending
	//   - error: nil on success, error if query failed
	//
	// Note: NopAuditLogger returns empty slice (no events stored).
	Query(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)

	// Flush ensures all buffered events are persisted.
	//
	// Call this before application shutdown to prevent event loss.
	// For sync implementations, this may be a no-op.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//
	// Returns:
	//   - error: nil on success, error if flush failed
	Flush(ctx context.Context) error
}

// NopAuditLogger is the default audit logger for open source.
//
// It discards all events without recording them. This is appropriate
// for local single-user deployments where audit trails aren't required.
//
// Thread-safe: This implementation has no mutable state.
//
// Example:
//
//	logger := &NopAuditLogger{}
//	err := logger.Log(ctx, AuditEvent{
//	    EventType: "chat.message",
//	    UserID:    "local-user",
//	})
//	// err == nil (event discarded)
//
//	events, err := logger.Query(ctx, AuditFilter{})
//	// events == []AuditEvent{} (always empty)
type NopAuditLogger struct{}

// Log discards the event without recording it.
//
// Always returns nil (success) regardless of event content.
func (l *NopAuditLogger) Log(ctx context.Context, event AuditEvent) error {
	return nil
}

// Query returns an empty slice (no events are stored).
//
// Always returns nil error with empty results.
func (l *NopAuditLogger) Query(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	return []AuditEvent{}, nil
}

// Flush is a no-op since nothing is buffered.
//
// Always returns nil (success).
func (l *NopAuditLogger) Flush(ctx context.Context) error {
	return nil
}

// Compile-time interface compliance check.
// This ensures NopAuditLogger implements AuditLogger.
var _ AuditLogger = (*NopAuditLogger)(nil)
