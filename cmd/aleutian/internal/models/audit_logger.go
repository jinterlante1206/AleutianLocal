// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package models

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// =============================================================================
// ModelAuditLogger Interface
// =============================================================================

// ModelAuditLogger records model operations for compliance.
//
// # Description
//
// This interface abstracts audit logging for model operations, enabling
// GDPR/HIPAA/CCPA compliance by tracking who downloaded what, when.
// All audit events are structured for machine parsing and compliance queries.
//
// # Security
//
//   - Logs must not contain sensitive user data beyond identifiers
//   - Digest information is logged for forensics
//   - Timestamps must be accurate (UTC)
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ModelAuditLogger interface {
	// LogModelPull records a model download operation.
	//
	// # Description
	//
	// Creates an audit log entry for a model pull operation.
	// Should be called at start (Success=false) and completion (with actual values).
	//
	// # Inputs
	//
	//   - event: Audit event details
	//
	// # Outputs
	//
	//   - error: Logging failure (should not block operation)
	//
	// # Examples
	//
	//   err := logger.LogModelPull(ModelAuditEvent{
	//       Action:     "pull_complete",
	//       Model:      "llama3:8b",
	//       Success:    true,
	//       BytesTotal: 4_000_000_000,
	//       Duration:   5 * time.Minute,
	//       Digest:     "sha256:abc123...",
	//   })
	//
	// # Limitations
	//
	//   - Logging failures are non-fatal
	//   - User identification is best-effort
	//
	// # Assumptions
	//
	//   - Log destination is configured and writable
	LogModelPull(event ModelAuditEvent) error

	// LogModelVerify records an integrity verification operation.
	//
	// # Description
	//
	// Creates an audit log entry when a model's digest is verified.
	// Records whether verification passed or failed.
	//
	// # Inputs
	//
	//   - event: Audit event details (Action should be "verify")
	//
	// # Outputs
	//
	//   - error: Logging failure (non-fatal)
	//
	// # Examples
	//
	//   err := logger.LogModelVerify(ModelAuditEvent{
	//       Action:  "verify",
	//       Model:   "llama3:8b",
	//       Success: true,
	//       Digest:  "sha256:abc123...",
	//   })
	//
	// # Limitations
	//
	//   - Logging failures are non-fatal
	//
	// # Assumptions
	//
	//   - Event contains valid digest for successful verifications
	LogModelVerify(event ModelAuditEvent) error

	// LogModelBlock records a blocked model request.
	//
	// # Description
	//
	// Creates an audit log entry when a model request is blocked
	// by the allowlist policy. Important for security monitoring.
	//
	// # Inputs
	//
	//   - event: Audit event details (Action should be "block")
	//
	// # Outputs
	//
	//   - error: Logging failure (non-fatal)
	//
	// # Examples
	//
	//   err := logger.LogModelBlock(ModelAuditEvent{
	//       Action:       "block",
	//       Model:        "unauthorized-model",
	//       Success:      false,
	//       ErrorMessage: "model not in allowlist",
	//   })
	//
	// # Limitations
	//
	//   - Logging failures are non-fatal
	//
	// # Assumptions
	//
	//   - ErrorMessage explains why model was blocked
	LogModelBlock(event ModelAuditEvent) error
}

// =============================================================================
// ModelAuditEvent Struct
// =============================================================================

// ModelAuditEvent records a model operation for audit.
//
// # Description
//
// Structured log entry for compliance and forensics. Contains all
// information needed to answer "who did what, when, with what result".
//
// # Thread Safety
//
// ModelAuditEvent is immutable after creation and safe for concurrent read.
//
// # JSON Format
//
// Events are serialized as JSON for machine parsing:
//
//	{
//	  "timestamp": "2026-01-06T12:00:00Z",
//	  "action": "pull_complete",
//	  "model": "llama3:8b",
//	  "user": "developer",
//	  "hostname": "dev-machine",
//	  "success": true,
//	  "bytes_total": 4000000000,
//	  "duration_ms": 300000,
//	  "digest": "sha256:abc123..."
//	}
type ModelAuditEvent struct {
	// Timestamp of the event (UTC)
	Timestamp time.Time `json:"timestamp"`

	// Action is the operation type
	// Values: "pull_start", "pull_complete", "verify", "block", "delete"
	Action string `json:"action"`

	// Model is the model identifier (e.g., "llama3:8b")
	Model string `json:"model"`

	// User is the system user who initiated the operation
	// Populated from $USER environment variable
	User string `json:"user,omitempty"`

	// Hostname is the machine name
	// Populated from os.Hostname()
	Hostname string `json:"hostname,omitempty"`

	// Success indicates if operation succeeded
	Success bool `json:"success"`

	// ErrorMessage contains failure reason if Success=false
	ErrorMessage string `json:"error_message,omitempty"`

	// BytesTotal is the total bytes transferred
	BytesTotal int64 `json:"bytes_total,omitempty"`

	// Duration is how long the operation took
	Duration time.Duration `json:"-"`

	// DurationMs is Duration in milliseconds for JSON serialization
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Digest is the model hash (for verification)
	// Format: "sha256:abc123..."
	Digest string `json:"digest,omitempty"`

	// Source is where the model came from (registry URL)
	Source string `json:"source,omitempty"`
}

// =============================================================================
// ModelAuditEvent Methods
// =============================================================================

// WithTimestamp sets the timestamp and returns the event.
//
// # Description
//
// Convenience method for builder-style event construction.
// If timestamp is zero, uses current UTC time.
//
// # Inputs
//
//   - t: Timestamp to set (zero = now)
//
// # Outputs
//
//   - ModelAuditEvent: The event with timestamp set
//
// # Examples
//
//	event := ModelAuditEvent{Action: "pull_complete"}.WithTimestamp(time.Time{})
//	// event.Timestamp is now set to current UTC time
func (e ModelAuditEvent) WithTimestamp(t time.Time) ModelAuditEvent {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	e.Timestamp = t
	return e
}

// WithHostInfo populates User and Hostname from system.
//
// # Description
//
// Fills in User from $USER environment variable and Hostname
// from os.Hostname(). Errors are silently ignored (best-effort).
//
// # Outputs
//
//   - ModelAuditEvent: The event with host info set
//
// # Examples
//
//	event := ModelAuditEvent{Action: "pull_complete"}.WithHostInfo()
//	// event.User and event.Hostname are now populated
//
// # Limitations
//
//   - User detection is best-effort ($USER may be empty)
//   - Hostname detection may fail on some systems
func (e ModelAuditEvent) WithHostInfo() ModelAuditEvent {
	e.User = os.Getenv("USER")
	if e.User == "" {
		e.User = os.Getenv("USERNAME") // Windows fallback
	}
	hostname, err := os.Hostname()
	if err == nil {
		e.Hostname = hostname
	}
	return e
}

// WithDuration sets the duration and DurationMs fields.
//
// # Description
//
// Sets both Duration (for Go code) and DurationMs (for JSON serialization).
//
// # Inputs
//
//   - d: Duration to set
//
// # Outputs
//
//   - ModelAuditEvent: The event with duration set
//
// # Examples
//
//	event := ModelAuditEvent{Action: "pull_complete"}.WithDuration(5 * time.Minute)
//	// event.Duration = 5m, event.DurationMs = 300000
func (e ModelAuditEvent) WithDuration(d time.Duration) ModelAuditEvent {
	e.Duration = d
	e.DurationMs = d.Milliseconds()
	return e
}

// ToJSON serializes the event to JSON.
//
// # Description
//
// Converts the event to a JSON byte slice for logging.
// Returns empty slice on error (should not happen with valid data).
//
// # Outputs
//
//   - []byte: JSON representation
//
// # Examples
//
//	jsonBytes := event.ToJSON()
//	fmt.Println(string(jsonBytes))
//
// # Limitations
//
//   - Returns empty slice on marshal error
func (e *ModelAuditEvent) ToJSON() []byte {
	data, err := json.Marshal(e)
	if err != nil {
		return []byte{}
	}
	return data
}

// String returns a human-readable representation.
//
// # Description
//
// Formats the event for human-readable logs. Not intended for
// machine parsing - use ToJSON() for that.
//
// # Outputs
//
//   - string: Human-readable event description
//
// # Examples
//
//	fmt.Println(event.String())
//	// "2026-01-06T12:00:00Z [pull_complete] model=llama3:8b user=developer success=true"
func (e *ModelAuditEvent) String() string {
	status := "success"
	if !e.Success {
		status = "failed"
		if e.ErrorMessage != "" {
			status = fmt.Sprintf("failed: %s", e.ErrorMessage)
		}
	}
	return fmt.Sprintf("%s [%s] model=%s user=%s %s",
		e.Timestamp.Format(time.RFC3339),
		e.Action,
		e.Model,
		e.User,
		status)
}

// =============================================================================
// LogWriter Interface
// =============================================================================

// LogWriter abstracts log output for audit events.
//
// # Description
//
// Interface for writing audit log entries. Allows plugging in
// different log destinations (file, syslog, DiagnosticsCollector).
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type LogWriter interface {
	// WriteLog writes a log entry with the given level.
	//
	// # Inputs
	//
	//   - level: Log level ("info", "warn", "error")
	//   - message: Log message
	//   - fields: Structured fields as JSON
	WriteLog(level, message string, fields []byte)
}

// =============================================================================
// DefaultModelAuditLogger Struct
// =============================================================================

// DefaultModelAuditLogger implements ModelAuditLogger using LogWriter.
//
// # Description
//
// Production implementation that writes audit events to the configured
// log destination. Events are formatted as structured JSON.
//
// # Thread Safety
//
// DefaultModelAuditLogger is safe for concurrent use.
//
// # Configuration
//
// Audit logging can be disabled via the enabled flag.
type DefaultModelAuditLogger struct {
	// writer is the log destination
	writer LogWriter

	// enabled controls whether logging is active
	enabled bool

	// mu protects configuration
	mu sync.RWMutex
}

// =============================================================================
// DefaultModelAuditLogger Constructor
// =============================================================================

// NewDefaultModelAuditLogger creates an audit logger with the given writer.
//
// # Description
//
// Creates an enabled audit logger that writes to the provided LogWriter.
// If writer is nil, creates a no-op logger.
//
// # Inputs
//
//   - writer: Log destination (nil for no-op)
//
// # Outputs
//
//   - *DefaultModelAuditLogger: Ready-to-use logger
//
// # Examples
//
//	logger := NewDefaultModelAuditLogger(diagnosticsCollector)
//	logger.LogModelPull(event)
//
// # Assumptions
//
//   - Writer is configured for appropriate log rotation
func NewDefaultModelAuditLogger(writer LogWriter) *DefaultModelAuditLogger {
	return &DefaultModelAuditLogger{
		writer:  writer,
		enabled: writer != nil,
	}
}

// =============================================================================
// DefaultModelAuditLogger Methods
// =============================================================================

// LogModelPull records a model download operation.
//
// # Description
//
// Writes a structured audit log entry for model pull operations.
// Automatically adds timestamp and host info if not present.
//
// # Inputs
//
//   - event: Audit event details
//
// # Outputs
//
//   - error: Always nil (logging failures are silent)
//
// # Examples
//
//	logger.LogModelPull(ModelAuditEvent{
//	    Action:  "pull_complete",
//	    Model:   "llama3:8b",
//	    Success: true,
//	})
//
// # Limitations
//
//   - Errors are silently ignored to not block operations
func (l *DefaultModelAuditLogger) LogModelPull(event ModelAuditEvent) error {
	return l.logEvent("model.pull", event)
}

// LogModelVerify records an integrity verification operation.
//
// # Description
//
// Writes a structured audit log entry for model verification.
//
// # Inputs
//
//   - event: Audit event details
//
// # Outputs
//
//   - error: Always nil (logging failures are silent)
//
// # Examples
//
//	logger.LogModelVerify(ModelAuditEvent{
//	    Action:  "verify",
//	    Model:   "llama3:8b",
//	    Success: true,
//	    Digest:  "sha256:abc123",
//	})
func (l *DefaultModelAuditLogger) LogModelVerify(event ModelAuditEvent) error {
	return l.logEvent("model.verify", event)
}

// LogModelBlock records a blocked model request.
//
// # Description
//
// Writes a structured audit log entry for blocked model requests.
// These events are logged at "warn" level for visibility.
//
// # Inputs
//
//   - event: Audit event details
//
// # Outputs
//
//   - error: Always nil (logging failures are silent)
//
// # Examples
//
//	logger.LogModelBlock(ModelAuditEvent{
//	    Action:       "block",
//	    Model:        "unauthorized",
//	    Success:      false,
//	    ErrorMessage: "not in allowlist",
//	})
func (l *DefaultModelAuditLogger) LogModelBlock(event ModelAuditEvent) error {
	return l.logEventWithLevel("warn", "model.block", event)
}

// logEvent writes an event at info level.
func (l *DefaultModelAuditLogger) logEvent(category string, event ModelAuditEvent) error {
	return l.logEventWithLevel("info", category, event)
}

// logEventWithLevel writes an event at the specified level.
func (l *DefaultModelAuditLogger) logEventWithLevel(level, category string, event ModelAuditEvent) error {
	l.mu.RLock()
	enabled := l.enabled
	writer := l.writer
	l.mu.RUnlock()

	if !enabled || writer == nil {
		return nil
	}

	// Ensure event has required fields
	event = l.enrichEvent(event)

	// Format message
	message := fmt.Sprintf("[AUDIT] %s: %s", category, event.Action)

	// Write log
	writer.WriteLog(level, message, event.ToJSON())

	return nil
}

// enrichEvent adds timestamp and host info if missing.
func (l *DefaultModelAuditLogger) enrichEvent(event ModelAuditEvent) ModelAuditEvent {
	if event.Timestamp.IsZero() {
		event = event.WithTimestamp(time.Time{})
	}
	if event.User == "" && event.Hostname == "" {
		event = event.WithHostInfo()
	}
	if event.Duration > 0 && event.DurationMs == 0 {
		event.DurationMs = event.Duration.Milliseconds()
	}
	return event
}

// SetEnabled enables or disables audit logging.
//
// # Description
//
// Allows runtime control of audit logging. When disabled,
// all Log* methods become no-ops.
//
// # Inputs
//
//   - enabled: Whether logging should be active
//
// # Examples
//
//	logger.SetEnabled(false) // Disable audit logging
//	logger.SetEnabled(true)  // Re-enable
func (l *DefaultModelAuditLogger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

// IsEnabled returns whether audit logging is active.
//
// # Description
//
// Checks if audit logging is currently enabled.
//
// # Outputs
//
//   - bool: True if logging is enabled
//
// # Examples
//
//	if logger.IsEnabled() {
//	    // Logging is active
//	}
func (l *DefaultModelAuditLogger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

// =============================================================================
// MockModelAuditLogger Struct
// =============================================================================

// MockModelAuditLogger implements ModelAuditLogger for testing.
//
// # Description
//
// Test double that records all logged events for verification.
// Does not write to any actual log destination.
//
// # Thread Safety
//
// MockModelAuditLogger is safe for concurrent use.
type MockModelAuditLogger struct {
	// Function overrides
	LogModelPullFunc   func(event ModelAuditEvent) error
	LogModelVerifyFunc func(event ModelAuditEvent) error
	LogModelBlockFunc  func(event ModelAuditEvent) error

	// mu protects event slices
	mu sync.Mutex

	// Recorded events
	PullEvents   []ModelAuditEvent
	VerifyEvents []ModelAuditEvent
	BlockEvents  []ModelAuditEvent

	// DefaultErr is returned if set and no func override
	DefaultErr error
}

// =============================================================================
// MockModelAuditLogger Constructor
// =============================================================================

// NewMockModelAuditLogger creates a mock for testing.
//
// # Description
//
// Creates a mock that records all events for later inspection.
//
// # Outputs
//
//   - *MockModelAuditLogger: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockModelAuditLogger()
//	mock.LogModelPull(event)
//	assert.Len(t, mock.PullEvents, 1)
func NewMockModelAuditLogger() *MockModelAuditLogger {
	return &MockModelAuditLogger{}
}

// =============================================================================
// MockModelAuditLogger Methods
// =============================================================================

// LogModelPull implements ModelAuditLogger.
func (m *MockModelAuditLogger) LogModelPull(event ModelAuditEvent) error {
	m.mu.Lock()
	m.PullEvents = append(m.PullEvents, event)
	m.mu.Unlock()

	if m.LogModelPullFunc != nil {
		return m.LogModelPullFunc(event)
	}
	return m.DefaultErr
}

// LogModelVerify implements ModelAuditLogger.
func (m *MockModelAuditLogger) LogModelVerify(event ModelAuditEvent) error {
	m.mu.Lock()
	m.VerifyEvents = append(m.VerifyEvents, event)
	m.mu.Unlock()

	if m.LogModelVerifyFunc != nil {
		return m.LogModelVerifyFunc(event)
	}
	return m.DefaultErr
}

// LogModelBlock implements ModelAuditLogger.
func (m *MockModelAuditLogger) LogModelBlock(event ModelAuditEvent) error {
	m.mu.Lock()
	m.BlockEvents = append(m.BlockEvents, event)
	m.mu.Unlock()

	if m.LogModelBlockFunc != nil {
		return m.LogModelBlockFunc(event)
	}
	return m.DefaultErr
}

// Reset clears all recorded events.
//
// # Description
//
// Clears all recorded events for reuse between test cases.
//
// # Examples
//
//	mock.Reset()
//	// All event slices are now empty
func (m *MockModelAuditLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PullEvents = nil
	m.VerifyEvents = nil
	m.BlockEvents = nil
}

// AllEvents returns all recorded events in order.
//
// # Description
//
// Returns a combined slice of all event types for convenience.
// Order is: pull events, then verify events, then block events.
//
// # Outputs
//
//   - []ModelAuditEvent: All recorded events
//
// # Examples
//
//	all := mock.AllEvents()
//	assert.Len(t, all, 3)
func (m *MockModelAuditLogger) AllEvents() []ModelAuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	all := make([]ModelAuditEvent, 0, len(m.PullEvents)+len(m.VerifyEvents)+len(m.BlockEvents))
	all = append(all, m.PullEvents...)
	all = append(all, m.VerifyEvents...)
	all = append(all, m.BlockEvents...)
	return all
}

// =============================================================================
// MockLogWriter Struct
// =============================================================================

// MockLogWriter implements LogWriter for testing.
//
// # Description
//
// Simple test double that captures log entries.
type MockLogWriter struct {
	mu      sync.Mutex
	Entries []MockLogEntry
}

// MockLogEntry captures a single log write.
type MockLogEntry struct {
	Level   string
	Message string
	Fields  []byte
}

// WriteLog implements LogWriter.
func (w *MockLogWriter) WriteLog(level, message string, fields []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Entries = append(w.Entries, MockLogEntry{
		Level:   level,
		Message: message,
		Fields:  fields,
	})
}

// Reset clears captured entries.
func (w *MockLogWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Entries = nil
}
