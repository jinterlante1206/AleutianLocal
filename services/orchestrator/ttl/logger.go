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
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// =============================================================================
// TTL Logger Implementation
// =============================================================================

// GenesisHash is the initial hash value for the first record in the chain.
// This allows verification that the chain starts from a known state.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// auditLogFileMode restricts read/write to owner only (0600).
//
// # Security Rationale
//
// The audit log contains deletion records with content hashes, Weaviate IDs,
// data space names, and timestamps. This metadata reveals what sensitive data
// existed and when it was deleted, which is itself sensitive information.
// Restricting to owner-only access prevents other system users from reading
// this compliance-critical data.
//
// # GDPR/HIPAA Compliance
//
// Audit logs are considered security-relevant data. World-readable permissions
// (0644) would violate the principle of least privilege and potentially expose
// protected information about data subjects.
const auditLogFileMode = 0600

// ttlLogger implements TTLLogger with dual output and hash chain integrity.
//
// # Description
//
// Provides dual-output logging for TTL cleanup operations with hash chain
// integrity for compliance requirements. Structured logs go to slog (stdout/JSON)
// for general monitoring. Deletion records with cryptographic proof go to a
// dedicated audit file.
//
// # Hash Chain
//
// Each deletion record includes a hash of the previous record, creating a
// tamper-evident chain. If any record is modified, the chain will break
// during verification.
//
// # Fields
//
//   - logFile: Handle to the dedicated audit log file.
//   - fileMu: Mutex protecting file writes.
//   - sequence: Monotonically increasing sequence number.
//   - prevHash: Hash of the previous entry (for chain linking).
//
// # Thread Safety
//
// All methods are thread-safe. File writes are serialized via mutex.
type ttlLogger struct {
	logFile  *os.File
	logPath  string
	fileMu   sync.Mutex
	sequence int64
	prevHash string
}

// NewTTLLogger creates a logger that writes to both slog and a dedicated file.
//
// # Description
//
// Creates a dual-output logger for TTL audit compliance. Structured logs go
// to slog (stdout/JSON), audit records go to dedicated file. The logger
// initializes the hash chain by reading the last entry from an existing file
// or starting fresh with the genesis hash.
//
// # Inputs
//
//   - logPath: Path to dedicated log file. Created if not exists.
//
// # Outputs
//
//   - TTLLogger: Ready to use logger.
//   - error: Non-nil if file creation or chain initialization fails.
//
// # Examples
//
//	logger, err := NewTTLLogger("/var/log/aleutian/ttl_cleanup.log")
//	if err != nil {
//	    return fmt.Errorf("failed to create TTL logger: %w", err)
//	}
//	defer logger.Close()
//
// # Limitations
//
//   - Log rotation must be handled externally (e.g., logrotate).
//   - File is opened in append mode.
//   - Chain verification after rotation requires preserving old files.
//
// # Assumptions
//
//   - The log file path is writable.
//   - Caller handles log file rotation.
//   - System clock is reasonably accurate for timestamps.
func NewTTLLogger(logPath string) (TTLLogger, error) {
	// Open file in append mode, create if doesn't exist
	// Use restricted permissions (0600) to prevent unauthorized access to audit data
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, auditLogFileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open TTL log file: %w", err)
	}

	logger := &ttlLogger{
		logFile:  file,
		logPath:  logPath,
		prevHash: GenesisHash,
		sequence: 0,
	}

	// Initialize chain state from existing file
	if err := logger.initializeChainState(logPath); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to initialize chain state: %w", err)
	}

	slog.Info("TTL audit logger initialized",
		"log_path", logPath,
		"starting_sequence", logger.sequence,
		"chain_initialized", true,
	)

	return logger, nil
}

// LogDeletion records a document or session deletion with cryptographic proof.
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
//
// # Examples
//
//	record, err := logger.LogDeletion(
//	    []byte("document content"),
//	    "abc123-uuid",
//	    "delete_document",
//	    DeletionMetadata{ParentSource: "report.md", DataSpace: "work"},
//	)
//	if err != nil {
//	    return fmt.Errorf("failed to log deletion: %w", err)
//	}
//	fmt.Printf("Logged deletion: sequence=%d, content_hash=%s\n",
//	    record.Sequence, record.ContentHash)
//
// # Limitations
//
//   - Requires content to compute hash; if content unavailable, pass empty.
//   - File writes are synchronous; may impact performance on slow disks.
//
// # Assumptions
//
//   - The log file is open and writable.
//   - System clock provides accurate timestamps.
func (l *ttlLogger) LogDeletion(content []byte, weaviateID string, operation string, metadata DeletionMetadata) (DeletionRecord, error) {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	// Increment sequence
	l.sequence++

	// Compute content hash
	contentHash := computeSHA256(content)

	// Build record
	record := DeletionRecord{
		Sequence:     l.sequence,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Operation:    operation,
		ContentHash:  contentHash,
		WeaviateID:   weaviateID,
		ParentSource: metadata.ParentSource,
		SessionID:    metadata.SessionID,
		DataSpace:    metadata.DataSpace,
		PrevHash:     l.prevHash,
	}

	// Compute entry hash (hash of this entire record)
	record.EntryHash = computeRecordHash(record)

	// Write to file
	if err := l.writeRecord(record); err != nil {
		return DeletionRecord{}, fmt.Errorf("failed to write deletion record: %w", err)
	}

	// Update chain state
	l.prevHash = record.EntryHash

	// Also log to slog for observability
	slog.Info("ttl.deletion.logged",
		"sequence", record.Sequence,
		"operation", record.Operation,
		"weaviate_id", record.WeaviateID,
		"content_hash", record.ContentHash[:16]+"...",
		"data_space", record.DataSpace,
	)

	return record, nil
}

// LogCleanup records a cleanup cycle summary to the audit log.
//
// # Description
//
// Writes a structured log entry containing the cleanup result to both
// slog (for observability) and the dedicated audit file (for compliance).
// This is a summary record, not part of the hash chain (uses separate format).
//
// # Inputs
//
//   - result: CleanupResult containing operation details.
//
// # Outputs
//
//   - error: Non-nil if logging fails.
//
// # Examples
//
//	err := logger.LogCleanup(cleanupResult)
//	if err != nil {
//	    slog.Warn("failed to log cleanup", "error", err)
//	}
//
// # Limitations
//
//   - Summary records are not part of the hash chain.
//   - Large error lists may create verbose log entries.
func (l *ttlLogger) LogCleanup(result CleanupResult) error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	// Create cleanup summary record
	summaryRecord := cleanupSummaryRecord{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		Operation:        "cleanup_cycle",
		DocumentsFound:   result.DocumentsFound,
		DocumentsDeleted: result.DocumentsDeleted,
		SessionsFound:    result.SessionsFound,
		SessionsDeleted:  result.SessionsDeleted,
		DurationMs:       result.DurationMs(),
		RolledBack:       result.RolledBack,
		ErrorCount:       len(result.Errors),
	}

	// Write to file
	jsonBytes, err := json.Marshal(summaryRecord)
	if err != nil {
		return fmt.Errorf("failed to marshal cleanup summary: %w", err)
	}

	if _, err := l.logFile.Write(append(jsonBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write cleanup summary: %w", err)
	}

	return nil
}

// LogError records a cleanup error to the audit log.
//
// # Description
//
// Writes an error entry to both slog and the audit file. Includes
// context string for debugging. Error records are not part of the hash chain.
//
// # Inputs
//
//   - err: The error that occurred.
//   - context: Description of what operation was being performed.
//
// # Outputs
//
//   - error: Non-nil if logging fails.
//
// # Examples
//
//	logErr := logger.LogError(weaviateErr, "batch_delete")
//	if logErr != nil {
//	    slog.Warn("failed to log error", "error", logErr)
//	}
func (l *ttlLogger) LogError(err error, context string) error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	errorRecord := errorLogRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Operation: "error",
		Context:   context,
		Error:     err.Error(),
	}

	jsonBytes, marshalErr := json.Marshal(errorRecord)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal error record: %w", marshalErr)
	}

	if _, writeErr := l.logFile.Write(append(jsonBytes, '\n')); writeErr != nil {
		return fmt.Errorf("failed to write error record: %w", writeErr)
	}

	// Also log to slog
	slog.Error("ttl.cleanup.error",
		"context", context,
		"error", err.Error(),
	)

	return nil
}

// LogConfigChange records a dataspace configuration change to the audit log.
//
// # Description
//
// Creates an audit record when retention policies are modified. Writes to
// both slog (for observability) and the dedicated audit file (for compliance).
// Config changes are NOT part of the hash chain — they are informational records.
//
// # Inputs
//
//   - change: The configuration change details. Timestamp is auto-set if empty.
//
// # Outputs
//
//   - error: Non-nil if logging fails.
//
// # Examples
//
//	err := logger.LogConfigChange(ttl.ConfigChangeRecord{
//	    DataSpace:    "work",
//	    FieldChanged: "retention_days",
//	    OldValue:     "90",
//	    NewValue:     "30",
//	    ChangedBy:    "admin@example.com",
//	    Reason:       "Reduced retention per new policy",
//	})
//
// # Limitations
//
//   - Not part of the hash chain (informational only).
//   - Does not validate field values.
//
// # Assumptions
//
//   - The log file is open and writable.
func (l *ttlLogger) LogConfigChange(change ConfigChangeRecord) error {
	if change.Timestamp == "" {
		change.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	// Log to slog for observability
	slog.Info("ttl.config_change.logged",
		"data_space", change.DataSpace,
		"field", change.FieldChanged,
		"old_value", change.OldValue,
		"new_value", change.NewValue,
		"changed_by", change.ChangedBy,
	)

	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	// Build the audit record with explicit type field
	record := configChangeAuditRecord{
		Type:         "config_change",
		Timestamp:    change.Timestamp,
		DataSpace:    change.DataSpace,
		FieldChanged: change.FieldChanged,
		OldValue:     change.OldValue,
		NewValue:     change.NewValue,
		ChangedBy:    change.ChangedBy,
		Reason:       change.Reason,
	}

	jsonBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal config change: %w", err)
	}

	if _, err := l.logFile.Write(append(jsonBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write config change: %w", err)
	}

	return nil
}

// VerifyChain verifies the integrity of the hash chain.
//
// # Description
//
// Reads all deletion records and verifies that each record's PrevHash
// matches the previous record's EntryHash. Returns the verification
// result and any breaks found in the chain.
//
// # Outputs
//
//   - valid: True if the entire chain is valid.
//   - breakIndex: Index of first broken link (-1 if valid).
//   - error: Non-nil if verification fails to complete.
//
// # Examples
//
//	valid, breakIndex, err := logger.VerifyChain()
//	if err != nil {
//	    return fmt.Errorf("verification failed: %w", err)
//	}
//	if !valid {
//	    fmt.Printf("Chain broken at index %d\n", breakIndex)
//	}
//
// # Limitations
//
//   - Requires reading the entire log file.
//   - Non-deletion records (cleanup summaries, errors) are skipped.
//   - May be slow for very large log files.
//
// # Assumptions
//
//   - The log file exists and is readable.
//   - File format is valid JSON lines.
func (l *ttlLogger) VerifyChain() (valid bool, breakIndex int64, err error) {
	l.fileMu.Lock()
	logPath := l.logFile.Name()
	l.fileMu.Unlock()

	// Open file for reading (separate handle)
	file, err := os.Open(logPath)
	if err != nil {
		return false, -1, fmt.Errorf("failed to open log file for verification: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var prevHash = GenesisHash
	var recordIndex int64 = 0

	for scanner.Scan() {
		line := scanner.Bytes()

		// Try to parse as deletion record
		var record DeletionRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue // Skip non-deletion records
		}

		// Check if this is a deletion record (has Sequence > 0)
		if record.Sequence == 0 {
			continue // Skip summary/error records
		}

		// Verify chain link
		if record.PrevHash != prevHash {
			return false, recordIndex, nil
		}

		// Verify entry hash
		computedHash := computeRecordHash(record)
		if computedHash != record.EntryHash {
			return false, recordIndex, nil
		}

		prevHash = record.EntryHash
		recordIndex++
	}

	if err := scanner.Err(); err != nil {
		return false, -1, fmt.Errorf("error reading log file: %w", err)
	}

	return true, -1, nil
}

// GetEntryCount returns the number of deletion records in the audit log.
//
// # Description
//
// Counts all deletion records (entries with Sequence > 0) in the log file.
// Used by `aleutian audit status` command for basic health reporting.
//
// # Outputs
//
//   - count: Number of deletion records in the log.
//   - error: Non-nil if reading fails.
//
// # Examples
//
//	count, err := logger.GetEntryCount()
//	if err != nil {
//	    return fmt.Errorf("failed to count entries: %w", err)
//	}
//	fmt.Printf("Audit log contains %d deletion records\n", count)
func (l *ttlLogger) GetEntryCount() (int64, error) {
	l.fileMu.Lock()
	logPath := l.logFile.Name()
	l.fileMu.Unlock()

	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var count int64 = 0

	for scanner.Scan() {
		line := scanner.Bytes()
		var record DeletionRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Sequence > 0 {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading log file: %w", err)
	}

	return count, nil
}

// GetLastEntry returns the most recent deletion record from the audit log.
//
// # Description
//
// Returns the last deletion record (highest sequence number) for status
// reporting. Used by `aleutian audit status` command.
//
// # Outputs
//
//   - record: The most recent DeletionRecord (nil if log is empty).
//   - error: Non-nil if reading fails.
//
// # Examples
//
//	record, err := logger.GetLastEntry()
//	if err != nil {
//	    return fmt.Errorf("failed to get last entry: %w", err)
//	}
//	if record != nil {
//	    fmt.Printf("Last deletion: %s at %s\n", record.WeaviateID, record.Timestamp)
//	}
func (l *ttlLogger) GetLastEntry() (*DeletionRecord, error) {
	l.fileMu.Lock()
	logPath := l.logFile.Name()
	l.fileMu.Unlock()

	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lastRecord *DeletionRecord

	for scanner.Scan() {
		line := scanner.Bytes()
		var record DeletionRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Sequence > 0 {
			recordCopy := record
			lastRecord = &recordCopy
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	return lastRecord, nil
}

// GetDeletionProof retrieves proof that a specific document was deleted.
//
// # FOSS vs Enterprise
//
// This method provides basic proof lookup for Enterprise compatibility.
// FOSS users can verify a deletion record exists in the chain.
// Full forensic proof generation (with HMAC signatures, external timestamps)
// requires Aleutian Enterprise.
//
// # Description
//
// Searches the audit log for a deletion record matching the content hash
// and verifies the chain up to that record.
//
// # Inputs
//
//   - contentHash: SHA-256 hash of the content (hex encoded).
//
// # Outputs
//
//   - DeletionProof: Proof of deletion if found.
//   - error: Non-nil if not found or verification fails.
//
// # Examples
//
//	proof, err := logger.GetDeletionProof(contentHash)
//	if err != nil {
//	    return fmt.Errorf("no deletion proof found: %w", err)
//	}
//	if proof.ChainValid {
//	    fmt.Printf("Deletion verified at %s\n", proof.Record.Timestamp)
//	}
//
// # Limitations
//
//   - Requires reading the entire log file up to the matching record.
//   - Content hash must exactly match (case-sensitive hex).
//   - FOSS proofs are not suitable for regulatory auditors (use Enterprise).
//
// # Assumptions
//
//   - The log file exists and is readable.
//   - Content hash is a valid SHA-256 hex string.
func (l *ttlLogger) GetDeletionProof(contentHash string) (DeletionProof, error) {
	l.fileMu.Lock()
	logPath := l.logFile.Name()
	l.fileMu.Unlock()

	file, err := os.Open(logPath)
	if err != nil {
		return DeletionProof{}, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var prevHash = GenesisHash
	var chainValid = true

	for scanner.Scan() {
		line := scanner.Bytes()

		var record DeletionRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}

		if record.Sequence == 0 {
			continue
		}

		// Verify chain as we go
		if record.PrevHash != prevHash {
			chainValid = false
		}
		computedHash := computeRecordHash(record)
		if computedHash != record.EntryHash {
			chainValid = false
		}

		// Check if this is the record we're looking for
		if record.ContentHash == contentHash {
			return DeletionProof{
				Record:     record,
				ChainValid: chainValid,
				VerifiedAt: time.Now().UTC().Format(time.RFC3339),
			}, nil
		}

		prevHash = record.EntryHash
	}

	if err := scanner.Err(); err != nil {
		return DeletionProof{}, fmt.Errorf("error reading log file: %w", err)
	}

	return DeletionProof{}, fmt.Errorf("no deletion record found for content hash: %s", contentHash)
}

// ReopenLogFile closes and reopens the log file for rotation support.
//
// # Description
//
// Supports external log rotation by closing the current file handle and
// opening a new one at the configured path. The hash chain state (sequence
// number, previous hash) is preserved in memory, so the chain continues
// seamlessly across the rotation boundary.
//
// # Usage
//
// Typically called from a SIGHUP signal handler after logrotate has moved
// the old file:
//
//	sigs := make(chan os.Signal, 1)
//	signal.Notify(sigs, syscall.SIGHUP)
//	go func() {
//	    for range sigs {
//	        if err := logger.ReopenLogFile(); err != nil {
//	            slog.Error("Failed to reopen log file", "error", err)
//	        }
//	    }
//	}()
//
// # Outputs
//
//   - error: Non-nil if reopen fails.
//
// # Limitations
//
//   - After rotation, the new file will not contain previous records.
//   - Chain verification across rotated files requires external tooling.
//   - If reopen fails, the logger is left in a closed state.
//
// # Assumptions
//
//   - The log path is still writable after rotation.
//   - The caller handles SIGHUP signal registration.
func (l *ttlLogger) ReopenLogFile() error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	// Close the old file handle
	if l.logFile != nil {
		if err := l.logFile.Close(); err != nil {
			slog.Warn("ttl.logger: error closing old log file during rotation",
				"path", l.logPath,
				"error", err,
			)
		}
		l.logFile = nil
	}

	// Open a new file handle at the same path
	file, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, auditLogFileMode)
	if err != nil {
		return fmt.Errorf("failed to reopen log file: %w", err)
	}

	l.logFile = file

	slog.Info("ttl.logger: reopened audit log file",
		"path", l.logPath,
		"sequence", l.sequence,
	)

	return nil
}

// CheckLogSize returns the current log file size in bytes.
//
// # Description
//
// Returns the size of the audit log file for operational monitoring.
// Can be used to trigger warnings when the file grows beyond an expected
// threshold, indicating rotation may not be working correctly.
//
// # Outputs
//
//   - int64: File size in bytes.
//   - error: Non-nil if stat fails or file is not open.
//
// # Examples
//
//	size, err := logger.CheckLogSize()
//	if err != nil {
//	    slog.Warn("Failed to check log size", "error", err)
//	} else if size > 100*1024*1024 { // 100MB
//	    slog.Warn("Audit log file is large, rotation may not be configured",
//	        "size_bytes", size)
//	}
//
// # Limitations
//
//   - Size may not reflect pending buffered writes.
//   - Returns the size of the file at the current path, which may be a
//     new file after rotation.
func (l *ttlLogger) CheckLogSize() (int64, error) {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if l.logFile == nil {
		return 0, fmt.Errorf("log file is not open")
	}

	info, err := l.logFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat audit log: %w", err)
	}

	return info.Size(), nil
}

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
//
// # Examples
//
//	if err := logger.Close(); err != nil {
//	    slog.Warn("failed to close TTL logger", "error", err)
//	}
func (l *ttlLogger) Close() error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if l.logFile != nil {
		if err := l.logFile.Close(); err != nil {
			return fmt.Errorf("failed to close log file: %w", err)
		}
		l.logFile = nil
	}
	return nil
}

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
// # Examples
//
//	if err := logger.VerifyFilePermissions(); err != nil {
//	    slog.Error("Audit log permissions compromised", "error", err)
//	    // Alert security team
//	}
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
func (l *ttlLogger) VerifyFilePermissions() error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if l.logFile == nil {
		return fmt.Errorf("log file is not open")
	}

	info, err := l.logFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat audit log: %w", err)
	}

	mode := info.Mode().Perm()
	if mode != auditLogFileMode {
		return fmt.Errorf("audit log permissions changed: expected %04o, got %04o", auditLogFileMode, mode)
	}

	return nil
}

// =============================================================================
// Internal Types and Functions
// =============================================================================

// cleanupSummaryRecord represents a cleanup cycle summary (not part of hash chain).
type cleanupSummaryRecord struct {
	Timestamp        string `json:"timestamp"`
	Operation        string `json:"operation"`
	DocumentsFound   int    `json:"documents_found"`
	DocumentsDeleted int    `json:"documents_deleted"`
	SessionsFound    int    `json:"sessions_found"`
	SessionsDeleted  int    `json:"sessions_deleted"`
	DurationMs       int64  `json:"duration_ms"`
	RolledBack       bool   `json:"rolled_back"`
	ErrorCount       int    `json:"error_count"`
}

// errorLogRecord represents an error entry (not part of hash chain).
type errorLogRecord struct {
	Timestamp string `json:"timestamp"`
	Operation string `json:"operation"`
	Context   string `json:"context"`
	Error     string `json:"error"`
}

// configChangeAuditRecord represents a config change in the audit log (not part of hash chain).
type configChangeAuditRecord struct {
	Type         string `json:"type"`
	Timestamp    string `json:"timestamp"`
	DataSpace    string `json:"data_space"`
	FieldChanged string `json:"field_changed"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	ChangedBy    string `json:"changed_by,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// initializeChainState reads the existing log file to find the last sequence and hash.
//
// # Description
//
// Called during logger initialization to continue the hash chain from where
// it left off. If the file is empty or doesn't exist, starts with genesis values.
func (l *ttlLogger) initializeChainState(logPath string) error {
	// Try to open for reading
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, start fresh
			return nil
		}
		return fmt.Errorf("failed to open log file for reading: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lastRecord DeletionRecord

	for scanner.Scan() {
		line := scanner.Bytes()
		var record DeletionRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		// Only track deletion records (have Sequence > 0)
		if record.Sequence > 0 {
			lastRecord = record
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	// Update state from last record
	if lastRecord.Sequence > 0 {
		l.sequence = lastRecord.Sequence
		l.prevHash = lastRecord.EntryHash
	}

	return nil
}

// writeRecord writes a DeletionRecord to the audit file as JSON.
func (l *ttlLogger) writeRecord(record DeletionRecord) error {
	jsonBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	if _, err := l.logFile.Write(append(jsonBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	return nil
}

// computeSHA256 computes the SHA-256 hash of content and returns hex string.
func computeSHA256(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// computeRecordHash computes the hash of a DeletionRecord for chain linking.
//
// # Description
//
// Hashes the record's fields (excluding EntryHash) to produce a deterministic
// hash that can be used for chain verification. Uses a stable field order.
func computeRecordHash(record DeletionRecord) string {
	// Create a deterministic string from record fields (excluding EntryHash)
	// Use a consistent format for reproducibility
	data := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s|%s|%s",
		record.Sequence,
		record.Timestamp,
		record.Operation,
		record.ContentHash,
		record.WeaviateID,
		record.ParentSource,
		record.SessionID,
		record.DataSpace,
		record.PrevHash,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
