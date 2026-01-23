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
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// SEC-001: File Permissions Tests
// =============================================================================

// TestNewTTLLogger_CreatesFileWithRestrictedPermissions verifies that new log files
// are created with 0600 permissions (owner read/write only).
//
// # Description
//
// Tests that the audit log file is created with restricted permissions to prevent
// unauthorized access to sensitive deletion metadata.
func TestNewTTLLogger_CreatesFileWithRestrictedPermissions(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	// Create the logger (which creates the file)
	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Check file permissions
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	mode := info.Mode().Perm()
	expectedMode := os.FileMode(0600)

	if mode != expectedMode {
		t.Errorf("File permissions incorrect: expected %04o, got %04o", expectedMode, mode)
	}
}

// TestNewTTLLogger_ExistingFileRetainsPermissions verifies that opening an existing
// log file does not change its permissions.
//
// # Description
//
// Tests that if a log file already exists with correct permissions, opening it
// again does not alter those permissions.
func TestNewTTLLogger_ExistingFileRetainsPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	// Create file manually with correct permissions
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	file.Close()

	// Now create logger which opens the existing file
	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Verify permissions unchanged
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	mode := info.Mode().Perm()
	expectedMode := os.FileMode(0600)

	if mode != expectedMode {
		t.Errorf("File permissions changed: expected %04o, got %04o", expectedMode, mode)
	}
}

// TestTTLLogger_VerifyFilePermissions_ValidPermissions tests that VerifyFilePermissions
// returns nil when permissions are correct.
//
// # Description
//
// Tests the happy path where the audit log has correct 0600 permissions.
func TestTTLLogger_VerifyFilePermissions_ValidPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Verify permissions - should succeed
	err = logger.VerifyFilePermissions()
	if err != nil {
		t.Errorf("VerifyFilePermissions failed unexpectedly: %v", err)
	}
}

// TestTTLLogger_VerifyFilePermissions_DetectsChange tests that VerifyFilePermissions
// detects when file permissions have been changed externally.
//
// # Description
//
// Tests that security monitoring can detect if someone changes the audit log
// permissions to a less secure mode.
func TestTTLLogger_VerifyFilePermissions_DetectsChange(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Externally change permissions to world-readable (simulating security issue)
	err = os.Chmod(logPath, 0644)
	if err != nil {
		t.Fatalf("Failed to chmod log file: %v", err)
	}

	// Now verify should detect the change
	err = logger.VerifyFilePermissions()
	if err == nil {
		t.Error("VerifyFilePermissions should have detected permission change")
	}

	// Verify the error message is descriptive
	expectedSubstring := "permissions changed"
	if err != nil && !containsSubstring(err.Error(), expectedSubstring) {
		t.Errorf("Error message should mention permissions: got %v", err)
	}
}

// TestTTLLogger_VerifyFilePermissions_ClosedFile tests that VerifyFilePermissions
// returns an error when the logger is closed.
//
// # Description
//
// Tests the error case where verification is attempted on a closed logger.
func TestTTLLogger_VerifyFilePermissions_ClosedFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}

	// Close the logger
	err = logger.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Now verify should fail
	err = logger.VerifyFilePermissions()
	if err == nil {
		t.Error("VerifyFilePermissions should fail on closed logger")
	}
}

// =============================================================================
// Hash Chain Tests
// =============================================================================

// TestTTLLogger_LogDeletion_CreatesValidRecord tests that LogDeletion creates
// properly structured records with valid hash chain links.
func TestTTLLogger_LogDeletion_CreatesValidRecord(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Log a deletion
	content := []byte("test document content")
	record, err := logger.LogDeletion(content, "test-uuid-123", "delete_document", DeletionMetadata{
		ParentSource: "test.md",
		DataSpace:    "test-space",
	})

	if err != nil {
		t.Fatalf("LogDeletion failed: %v", err)
	}

	// Verify record fields
	if record.Sequence != 1 {
		t.Errorf("Expected sequence 1, got %d", record.Sequence)
	}

	if record.WeaviateID != "test-uuid-123" {
		t.Errorf("Expected WeaviateID 'test-uuid-123', got '%s'", record.WeaviateID)
	}

	if record.Operation != "delete_document" {
		t.Errorf("Expected operation 'delete_document', got '%s'", record.Operation)
	}

	if record.ParentSource != "test.md" {
		t.Errorf("Expected ParentSource 'test.md', got '%s'", record.ParentSource)
	}

	if record.PrevHash != GenesisHash {
		t.Errorf("First record should have genesis PrevHash")
	}

	if record.EntryHash == "" {
		t.Error("EntryHash should not be empty")
	}

	if record.ContentHash == "" {
		t.Error("ContentHash should not be empty")
	}
}

// TestTTLLogger_LogDeletion_ChainLinks tests that multiple deletions create
// a properly linked hash chain.
func TestTTLLogger_LogDeletion_ChainLinks(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Log first deletion
	record1, err := logger.LogDeletion([]byte("doc1"), "uuid-1", "delete_document", DeletionMetadata{})
	if err != nil {
		t.Fatalf("First LogDeletion failed: %v", err)
	}

	// Log second deletion
	record2, err := logger.LogDeletion([]byte("doc2"), "uuid-2", "delete_document", DeletionMetadata{})
	if err != nil {
		t.Fatalf("Second LogDeletion failed: %v", err)
	}

	// Verify chain link
	if record2.PrevHash != record1.EntryHash {
		t.Error("Second record's PrevHash should equal first record's EntryHash")
	}

	if record2.Sequence != 2 {
		t.Errorf("Expected sequence 2, got %d", record2.Sequence)
	}
}

// TestTTLLogger_VerifyChain_ValidChain tests that VerifyChain returns true
// for a properly linked chain.
func TestTTLLogger_VerifyChain_ValidChain(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Create several deletions
	for i := 0; i < 5; i++ {
		_, err := logger.LogDeletion([]byte("content"), "uuid", "delete_document", DeletionMetadata{})
		if err != nil {
			t.Fatalf("LogDeletion %d failed: %v", i, err)
		}
	}

	// Verify chain
	valid, breakIndex, err := logger.VerifyChain()
	if err != nil {
		t.Fatalf("VerifyChain failed: %v", err)
	}

	if !valid {
		t.Errorf("Chain should be valid, break at index %d", breakIndex)
	}

	if breakIndex != -1 {
		t.Errorf("Break index should be -1 for valid chain, got %d", breakIndex)
	}
}

// TestTTLLogger_GetDeletionProof_FindsRecord tests that GetDeletionProof
// can find a deletion record by content hash.
//
// NOTE: GetDeletionProof is kept for Enterprise compatibility but is not
// part of the FOSS TTLLogger interface. This test uses type assertion.
func TestTTLLogger_GetDeletionProof_FindsRecord(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Type assert to concrete type for Enterprise-compatibility method
	concreteLogger, ok := logger.(*ttlLogger)
	if !ok {
		t.Fatal("Failed to type assert to *ttlLogger")
	}

	// Log a deletion and capture the content hash
	content := []byte("unique test content for proof")
	record, err := logger.LogDeletion(content, "proof-uuid", "delete_document", DeletionMetadata{
		DataSpace: "proof-test",
	})
	if err != nil {
		t.Fatalf("LogDeletion failed: %v", err)
	}

	// Get deletion proof using the content hash (Enterprise compatibility)
	proof, err := concreteLogger.GetDeletionProof(record.ContentHash)
	if err != nil {
		t.Fatalf("GetDeletionProof failed: %v", err)
	}

	if proof.Record.WeaviateID != "proof-uuid" {
		t.Errorf("Expected WeaviateID 'proof-uuid', got '%s'", proof.Record.WeaviateID)
	}

	if !proof.ChainValid {
		t.Error("Chain should be valid")
	}
}

// TestTTLLogger_GetDeletionProof_NotFound tests that GetDeletionProof
// returns an error for non-existent content hash.
//
// NOTE: GetDeletionProof is kept for Enterprise compatibility but is not
// part of the FOSS TTLLogger interface. This test uses type assertion.
func TestTTLLogger_GetDeletionProof_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Type assert to concrete type for Enterprise-compatibility method
	concreteLogger, ok := logger.(*ttlLogger)
	if !ok {
		t.Fatal("Failed to type assert to *ttlLogger")
	}

	// Try to find a non-existent record (Enterprise compatibility)
	_, err = concreteLogger.GetDeletionProof("nonexistent-hash-value")
	if err == nil {
		t.Error("GetDeletionProof should return error for non-existent hash")
	}
}

// =============================================================================
// Status Reporting Tests (FOSS)
// =============================================================================

// TestTTLLogger_GetEntryCount_EmptyLog tests that GetEntryCount returns 0
// for an empty audit log.
func TestTTLLogger_GetEntryCount_EmptyLog(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	count, err := logger.GetEntryCount()
	if err != nil {
		t.Fatalf("GetEntryCount failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected count 0 for empty log, got %d", count)
	}
}

// TestTTLLogger_GetEntryCount_WithRecords tests that GetEntryCount returns
// the correct count of deletion records.
func TestTTLLogger_GetEntryCount_WithRecords(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Add some deletion records
	for i := 0; i < 5; i++ {
		_, err := logger.LogDeletion([]byte("content"), "uuid", "delete_document", DeletionMetadata{})
		if err != nil {
			t.Fatalf("LogDeletion failed: %v", err)
		}
	}

	// Add a cleanup summary (should not be counted)
	err = logger.LogCleanup(CleanupResult{})
	if err != nil {
		t.Fatalf("LogCleanup failed: %v", err)
	}

	count, err := logger.GetEntryCount()
	if err != nil {
		t.Fatalf("GetEntryCount failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}
}

// TestTTLLogger_GetLastEntry_EmptyLog tests that GetLastEntry returns nil
// for an empty audit log.
func TestTTLLogger_GetLastEntry_EmptyLog(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	record, err := logger.GetLastEntry()
	if err != nil {
		t.Fatalf("GetLastEntry failed: %v", err)
	}

	if record != nil {
		t.Error("Expected nil record for empty log")
	}
}

// TestTTLLogger_GetLastEntry_ReturnsLastRecord tests that GetLastEntry
// returns the most recent deletion record.
func TestTTLLogger_GetLastEntry_ReturnsLastRecord(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	// Add some deletion records
	for i := 1; i <= 3; i++ {
		_, err := logger.LogDeletion([]byte("content"), fmt.Sprintf("uuid-%d", i), "delete_document", DeletionMetadata{
			DataSpace: fmt.Sprintf("space-%d", i),
		})
		if err != nil {
			t.Fatalf("LogDeletion failed: %v", err)
		}
	}

	record, err := logger.GetLastEntry()
	if err != nil {
		t.Fatalf("GetLastEntry failed: %v", err)
	}

	if record == nil {
		t.Fatal("Expected non-nil record")
	}

	if record.WeaviateID != "uuid-3" {
		t.Errorf("Expected WeaviateID 'uuid-3', got '%s'", record.WeaviateID)
	}

	if record.DataSpace != "space-3" {
		t.Errorf("Expected DataSpace 'space-3', got '%s'", record.DataSpace)
	}

	if record.Sequence != 3 {
		t.Errorf("Expected Sequence 3, got %d", record.Sequence)
	}
}

// =============================================================================
// Cleanup Summary Tests
// =============================================================================

// TestTTLLogger_LogCleanup_WritesRecord tests that LogCleanup writes
// a cleanup summary to the audit log.
func TestTTLLogger_LogCleanup_WritesRecord(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	result := CleanupResult{
		DocumentsFound:   10,
		DocumentsDeleted: 8,
		SessionsFound:    5,
		SessionsDeleted:  5,
		RolledBack:       false,
	}

	err = logger.LogCleanup(result)
	if err != nil {
		t.Fatalf("LogCleanup failed: %v", err)
	}

	// Verify file has content
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Log file should not be empty after LogCleanup")
	}
}

// TestTTLLogger_LogError_WritesRecord tests that LogError writes
// an error record to the audit log.
func TestTTLLogger_LogError_WritesRecord(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("NewTTLLogger failed: %v", err)
	}
	defer logger.Close()

	testErr := os.ErrNotExist
	err = logger.LogError(testErr, "test_context")
	if err != nil {
		t.Fatalf("LogError failed: %v", err)
	}

	// Verify file has content
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Log file should not be empty after LogError")
	}
}

// =============================================================================
// Chain Initialization Tests
// =============================================================================

// TestTTLLogger_InitializesFromExistingFile tests that a new logger
// correctly reads the chain state from an existing log file.
func TestTTLLogger_InitializesFromExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_audit.log")

	// Create first logger and add some records
	logger1, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("First NewTTLLogger failed: %v", err)
	}

	record1, err := logger1.LogDeletion([]byte("doc1"), "uuid-1", "delete_document", DeletionMetadata{})
	if err != nil {
		t.Fatalf("LogDeletion failed: %v", err)
	}

	logger1.Close()

	// Create second logger from same file
	logger2, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Second NewTTLLogger failed: %v", err)
	}
	defer logger2.Close()

	// Add another record
	record2, err := logger2.LogDeletion([]byte("doc2"), "uuid-2", "delete_document", DeletionMetadata{})
	if err != nil {
		t.Fatalf("LogDeletion failed: %v", err)
	}

	// Verify sequence continues
	if record2.Sequence != 2 {
		t.Errorf("Expected sequence 2, got %d", record2.Sequence)
	}

	// Verify chain link is correct
	if record2.PrevHash != record1.EntryHash {
		t.Error("Chain should continue from previous file state")
	}

	// Verify full chain
	valid, _, err := logger2.VerifyChain()
	if err != nil {
		t.Fatalf("VerifyChain failed: %v", err)
	}

	if !valid {
		t.Error("Chain should be valid after reopening file")
	}
}

// =============================================================================
// SEC-007: Log Rotation Tests
// =============================================================================

// TestTTLLogger_ReopenLogFile_Success tests that ReopenLogFile successfully
// closes and reopens the file, and new writes go to the new file.
func TestTTLLogger_ReopenLogFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_rotation.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write a record before rotation
	_, err = logger.LogDeletion(
		[]byte("before rotation"),
		"uuid-before",
		"delete_document",
		DeletionMetadata{DataSpace: "test"},
	)
	if err != nil {
		t.Fatalf("Failed to log before rotation: %v", err)
	}

	// Simulate logrotate: rename the file
	rotatedPath := logPath + ".1"
	if err := os.Rename(logPath, rotatedPath); err != nil {
		t.Fatalf("Failed to rename log file: %v", err)
	}

	// Reopen the log file (creates a new file at the original path)
	if err := logger.ReopenLogFile(); err != nil {
		t.Fatalf("ReopenLogFile failed: %v", err)
	}

	// Write a record after rotation
	record, err := logger.LogDeletion(
		[]byte("after rotation"),
		"uuid-after",
		"delete_document",
		DeletionMetadata{DataSpace: "test"},
	)
	if err != nil {
		t.Fatalf("Failed to log after rotation: %v", err)
	}

	// Verify the new file exists and has content
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("New log file should exist: %v", err)
	}
	if info.Size() == 0 {
		t.Error("New log file should have content after write")
	}

	// Verify the rotated file still exists
	_, err = os.Stat(rotatedPath)
	if err != nil {
		t.Fatalf("Rotated file should still exist: %v", err)
	}

	// Verify chain continuity - sequence should continue
	if record.Sequence != 2 {
		t.Errorf("Expected sequence 2 after rotation, got %d", record.Sequence)
	}
}

// TestTTLLogger_ReopenLogFile_ChainContinuity tests that the hash chain
// continues correctly across a file rotation.
func TestTTLLogger_ReopenLogFile_ChainContinuity(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_chain_rotation.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write record 1
	record1, err := logger.LogDeletion(
		[]byte("record one"),
		"uuid-1",
		"delete_document",
		DeletionMetadata{DataSpace: "test"},
	)
	if err != nil {
		t.Fatalf("Failed to log record 1: %v", err)
	}

	// Rotate
	if err := os.Rename(logPath, logPath+".1"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if err := logger.ReopenLogFile(); err != nil {
		t.Fatalf("ReopenLogFile failed: %v", err)
	}

	// Write record 2 after rotation
	record2, err := logger.LogDeletion(
		[]byte("record two"),
		"uuid-2",
		"delete_document",
		DeletionMetadata{DataSpace: "test"},
	)
	if err != nil {
		t.Fatalf("Failed to log record 2: %v", err)
	}

	// PrevHash of record 2 should be EntryHash of record 1
	if record2.PrevHash != record1.EntryHash {
		t.Errorf("Chain broken across rotation: record2.PrevHash=%s, expected record1.EntryHash=%s",
			record2.PrevHash[:16], record1.EntryHash[:16])
	}
}

// TestTTLLogger_ReopenLogFile_PermissionsPreserved tests that the new file
// after rotation has the correct restricted permissions.
func TestTTLLogger_ReopenLogFile_PermissionsPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_perms_rotation.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Rotate
	if err := os.Rename(logPath, logPath+".1"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if err := logger.ReopenLogFile(); err != nil {
		t.Fatalf("ReopenLogFile failed: %v", err)
	}

	// Check permissions on new file
	if err := logger.VerifyFilePermissions(); err != nil {
		t.Errorf("New file should have correct permissions: %v", err)
	}
}

// TestTTLLogger_CheckLogSize_ReturnsSize tests that CheckLogSize returns
// the correct file size.
func TestTTLLogger_CheckLogSize_ReturnsSize(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_size.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Empty file should have size 0
	size, err := logger.CheckLogSize()
	if err != nil {
		t.Fatalf("CheckLogSize failed: %v", err)
	}
	if size != 0 {
		t.Errorf("Expected size 0 for empty file, got %d", size)
	}

	// Write some records
	for i := 0; i < 5; i++ {
		_, err := logger.LogDeletion(
			[]byte(fmt.Sprintf("content %d", i)),
			fmt.Sprintf("uuid-%d", i),
			"delete_document",
			DeletionMetadata{DataSpace: "test"},
		)
		if err != nil {
			t.Fatalf("Failed to log record %d: %v", i, err)
		}
	}

	// Size should now be > 0
	size, err = logger.CheckLogSize()
	if err != nil {
		t.Fatalf("CheckLogSize failed after writes: %v", err)
	}
	if size == 0 {
		t.Error("Expected non-zero size after writing records")
	}
	// Each JSON record is roughly 300-500 bytes, so 5 records should be > 1000
	if size < 1000 {
		t.Errorf("Expected at least 1000 bytes for 5 records, got %d", size)
	}
}

// TestTTLLogger_CheckLogSize_AfterClose tests that CheckLogSize returns
// an error when the file is closed.
func TestTTLLogger_CheckLogSize_AfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_size_closed.log")

	logger, err := NewTTLLogger(logPath)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	logger.Close()

	_, err = logger.CheckLogSize()
	if err == nil {
		t.Error("Expected error when checking size of closed file")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
