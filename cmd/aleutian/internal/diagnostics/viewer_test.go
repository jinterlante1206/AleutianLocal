// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package diagnostics_test provides tests for FileDiagnosticsViewer.

These tests validate the FOSS-tier viewer implementation:

  - Get by path, filename, and trace ID
  - List with filtering and pagination
  - GetByTraceID (the "Support Ticket Revolution")
  - Cache behavior and clearing
  - Context cancellation handling

# Test Strategy

All tests use temporary directories to avoid side effects. Test data is
created using real FileDiagnosticsStorage for authentic file operations.
*/
package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// newTestViewer creates a viewer with test storage in a temp directory.
//
// # Description
//
// Creates a FileDiagnosticsViewer backed by a temporary directory.
// Returns the viewer, storage, and a cleanup function.
//
// # Inputs
//
//   - t: Test instance for cleanup registration
//
// # Outputs
//
//   - *FileDiagnosticsViewer: Ready viewer
//   - *FileDiagnosticsStorage: Backing storage for test data creation
//   - func(): Cleanup function to remove temp directory
//
// # Examples
//
//	viewer, storage, cleanup := newTestViewer(t)
//	defer cleanup()
//
// # Limitations
//
//   - Temporary directory is OS-specific
//
// # Assumptions
//
//   - OS temp directory is writable
func newTestViewer(t *testing.T) (*FileDiagnosticsViewer, *FileDiagnosticsStorage, func()) {
	t.Helper()

	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}

	viewer := NewFileDiagnosticsViewer(storage)

	cleanup := func() {
		// t.TempDir() handles cleanup automatically
	}

	return viewer, storage, cleanup
}

// createTestDiagnostic creates a diagnostic file with specified properties.
//
// # Description
//
// Creates a diagnostic file in the test storage with the given parameters.
// Returns the full path to the created file.
//
// # Inputs
//
//   - t: Test instance
//   - storage: Storage backend to use
//   - traceID: Trace ID for the diagnostic
//   - reason: Collection reason
//   - severity: Severity level
//   - timestampMs: Timestamp in milliseconds
//
// # Outputs
//
//   - string: Path to the created file
//
// # Examples
//
//	path := createTestDiagnostic(t, storage, "trace-123", "startup", SeverityError, time.Now().UnixMilli())
//
// # Limitations
//
//   - Creates minimal diagnostic data
//
// # Assumptions
//
//   - Storage is initialized and writable
func createTestDiagnostic(
	t *testing.T,
	storage *FileDiagnosticsStorage,
	traceID string,
	reason string,
	severity DiagnosticsSeverity,
	timestampMs int64,
) string {
	t.Helper()

	data := &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: timestampMs,
			TraceID:     traceID,
			SpanID:      "span-" + traceID,
			Reason:      reason,
			Severity:    severity,
		},
		System: SystemInfo{
			OS:              "darwin",
			Arch:            "arm64",
			AleutianVersion: "test",
		},
		Podman: PodmanInfo{
			Available: true,
			Version:   "4.8.0",
		},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal test diagnostic: %v", err)
	}

	ctx := context.Background()
	location, err := storage.Store(ctx, jsonBytes, StorageMetadata{
		FilenameHint: reason,
		ContentType:  "application/json",
	})
	if err != nil {
		t.Fatalf("failed to store test diagnostic: %v", err)
	}

	return location
}

// createTestDiagnosticSet creates multiple diagnostics for list/filter testing.
//
// # Description
//
// Creates a predefined set of diagnostics with varying severities, reasons,
// and timestamps for testing filtering and pagination.
//
// # Inputs
//
//   - t: Test instance
//   - storage: Storage backend
//
// # Outputs
//
//   - []string: Paths to created files
//
// # Examples
//
//	paths := createTestDiagnosticSet(t, storage)
//
// # Limitations
//
//   - Fixed test data set
//
// # Assumptions
//
//   - Storage is initialized
func createTestDiagnosticSet(t *testing.T, storage *FileDiagnosticsStorage) []string {
	t.Helper()

	baseTime := time.Now().UnixMilli()
	paths := make([]string, 0, 5)

	// Create diagnostics with different properties
	testCases := []struct {
		traceID  string
		reason   string
		severity DiagnosticsSeverity
		ageMs    int64
	}{
		{"trace-001", "startup_failure", SeverityError, 0},
		{"trace-002", "machine_drift", SeverityWarning, -1000},
		{"trace-003", "manual_request", SeverityInfo, -2000},
		{"trace-004", "startup_failure", SeverityCritical, -3000},
		{"trace-005", "health_check", SeverityInfo, -4000},
	}

	for _, tc := range testCases {
		// Add small delay to ensure unique filenames
		time.Sleep(time.Millisecond)
		path := createTestDiagnostic(t, storage, tc.traceID, tc.reason, tc.severity, baseTime+tc.ageMs)
		paths = append(paths, path)
	}

	return paths
}

// -----------------------------------------------------------------------------
// Get Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_Get_ByFullPath tests retrieval by full file path.
//
// # Description
//
// Verifies that Get() can retrieve a diagnostic using its full absolute path.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create a diagnostic file
//  3. Get by full path
//  4. Verify retrieved data matches
func TestFileDiagnosticsViewer_Get_ByFullPath(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostic
	path := createTestDiagnostic(t, storage, "trace-full-path", "test_get", SeverityInfo, time.Now().UnixMilli())

	// Get by full path
	ctx := context.Background()
	data, err := viewer.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	// Verify
	if data.Header.TraceID != "trace-full-path" {
		t.Errorf("TraceID = %q, want %q", data.Header.TraceID, "trace-full-path")
	}
	if data.Header.Reason != "test_get" {
		t.Errorf("Reason = %q, want %q", data.Header.Reason, "test_get")
	}
}

// TestFileDiagnosticsViewer_Get_ByFilename tests retrieval by filename only.
//
// # Description
//
// Verifies that Get() can retrieve a diagnostic using just the filename
// (relative to storage directory).
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create a diagnostic file
//  3. Get by filename only
//  4. Verify retrieved data matches
func TestFileDiagnosticsViewer_Get_ByFilename(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostic
	path := createTestDiagnostic(t, storage, "trace-filename", "test_filename", SeverityWarning, time.Now().UnixMilli())

	// Get by filename only
	filename := filepath.Base(path)
	ctx := context.Background()
	data, err := viewer.Get(ctx, filename)
	if err != nil {
		t.Fatalf("Get() by filename failed: %v", err)
	}

	// Verify
	if data.Header.TraceID != "trace-filename" {
		t.Errorf("TraceID = %q, want %q", data.Header.TraceID, "trace-filename")
	}
}

// TestFileDiagnosticsViewer_Get_ByTraceID tests retrieval by trace ID.
//
// # Description
//
// Verifies that Get() delegates to GetByTraceID when the ID looks like a trace ID.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create a diagnostic file with a known trace ID
//  3. Call Get() with trace ID format
//  4. Verify it finds the correct diagnostic
func TestFileDiagnosticsViewer_Get_ByTraceID(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostic with trace ID format
	traceID := "1704463200000000000-12345"
	createTestDiagnostic(t, storage, traceID, "trace_test", SeverityError, time.Now().UnixMilli())

	// Get by trace ID
	ctx := context.Background()
	data, err := viewer.Get(ctx, traceID)
	if err != nil {
		t.Fatalf("Get() by trace ID failed: %v", err)
	}

	// Verify
	if data.Header.TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", data.Header.TraceID, traceID)
	}
}

// TestFileDiagnosticsViewer_Get_NotFound tests error handling for missing files.
//
// # Description
//
// Verifies that Get() returns an error when the file doesn't exist.
//
// # Test Steps
//
//  1. Create test viewer
//  2. Call Get() with non-existent file
//  3. Verify error is returned
func TestFileDiagnosticsViewer_Get_NotFound(t *testing.T) {
	viewer, _, cleanup := newTestViewer(t)
	defer cleanup()

	ctx := context.Background()
	_, err := viewer.Get(ctx, "nonexistent-file.json")
	if err == nil {
		t.Error("Get() should return error for missing file")
	}
}

// TestFileDiagnosticsViewer_Get_InvalidJSON tests error handling for malformed JSON.
//
// # Description
//
// Verifies that Get() returns an error when the file contains invalid JSON.
//
// # Test Steps
//
//  1. Create test viewer
//  2. Write invalid JSON to storage directory
//  3. Call Get()
//  4. Verify error is returned
func TestFileDiagnosticsViewer_Get_InvalidJSON(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Write invalid JSON directly
	invalidPath := filepath.Join(storage.BaseDir(), "diag-invalid.json")
	if err := os.WriteFile(invalidPath, []byte("not valid json"), 0640); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	ctx := context.Background()
	_, err := viewer.Get(ctx, invalidPath)
	if err == nil {
		t.Error("Get() should return error for invalid JSON")
	}
}

// -----------------------------------------------------------------------------
// List Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_List_NoFilter tests listing all diagnostics.
//
// # Description
//
// Verifies that List() returns all diagnostics without filters.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create multiple diagnostics
//  3. List without filters
//  4. Verify all diagnostics returned
func TestFileDiagnosticsViewer_List_NoFilter(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test set
	paths := createTestDiagnosticSet(t, storage)

	// List all
	ctx := context.Background()
	summaries, err := viewer.List(ctx, ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Verify count
	if len(summaries) != len(paths) {
		t.Errorf("List() returned %d items, want %d", len(summaries), len(paths))
	}
}

// TestFileDiagnosticsViewer_List_SeverityFilter tests filtering by severity.
//
// # Description
//
// Verifies that List() correctly filters by severity level.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create diagnostics with different severities
//  3. List with severity filter
//  4. Verify only matching severities returned
func TestFileDiagnosticsViewer_List_SeverityFilter(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test set (contains 2 Info, 1 Warning, 1 Error, 1 Critical)
	createTestDiagnosticSet(t, storage)

	// List errors only
	ctx := context.Background()
	summaries, err := viewer.List(ctx, ListOptions{
		Limit:    100,
		Severity: SeverityError,
	})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Verify all are errors
	for _, s := range summaries {
		if s.Severity != SeverityError {
			t.Errorf("Severity filter returned %s, want %s", s.Severity, SeverityError)
		}
	}

	// Should have exactly 1 error
	if len(summaries) != 1 {
		t.Errorf("Expected 1 error, got %d", len(summaries))
	}
}

// TestFileDiagnosticsViewer_List_ReasonFilter tests filtering by reason.
//
// # Description
//
// Verifies that List() correctly filters by reason string.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create diagnostics with different reasons
//  3. List with reason filter
//  4. Verify only matching reasons returned
func TestFileDiagnosticsViewer_List_ReasonFilter(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test set (contains 2 with "startup_failure")
	createTestDiagnosticSet(t, storage)

	// List startup_failure only
	ctx := context.Background()
	summaries, err := viewer.List(ctx, ListOptions{
		Limit:  100,
		Reason: "startup_failure",
	})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Verify all match reason
	for _, s := range summaries {
		if s.Reason != "startup_failure" {
			t.Errorf("Reason filter returned %s, want startup_failure", s.Reason)
		}
	}

	// Should have exactly 2 startup_failure entries
	if len(summaries) != 2 {
		t.Errorf("Expected 2 startup_failure, got %d", len(summaries))
	}
}

// TestFileDiagnosticsViewer_List_Pagination tests limit and offset.
//
// # Description
//
// Verifies that List() correctly applies pagination.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create multiple diagnostics
//  3. List with limit
//  4. List with offset
//  5. Verify pagination works correctly
func TestFileDiagnosticsViewer_List_Pagination(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test set (5 items)
	createTestDiagnosticSet(t, storage)

	ctx := context.Background()

	// Test limit
	summaries, err := viewer.List(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("List() with limit failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("Limit returned %d items, want 2", len(summaries))
	}

	// Test offset
	summaries, err = viewer.List(ctx, ListOptions{Limit: 10, Offset: 3})
	if err != nil {
		t.Fatalf("List() with offset failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("Offset returned %d items, want 2 (5 total - 3 skipped)", len(summaries))
	}

	// Test offset beyond range
	summaries, err = viewer.List(ctx, ListOptions{Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("List() with large offset failed: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("Large offset returned %d items, want 0", len(summaries))
	}
}

// TestFileDiagnosticsViewer_List_SortOrder tests that results are newest first.
//
// # Description
//
// Verifies that List() returns results sorted by timestamp descending.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create diagnostics with different timestamps
//  3. List all
//  4. Verify newest is first
func TestFileDiagnosticsViewer_List_SortOrder(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test set (timestamps are decreasing: 0, -1000, -2000, -3000, -4000)
	createTestDiagnosticSet(t, storage)

	ctx := context.Background()
	summaries, err := viewer.List(ctx, ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Verify descending order
	for i := 1; i < len(summaries); i++ {
		if summaries[i].TimestampMs > summaries[i-1].TimestampMs {
			t.Errorf("List() not sorted by timestamp: [%d]=%d > [%d]=%d",
				i, summaries[i].TimestampMs, i-1, summaries[i-1].TimestampMs)
		}
	}
}

// TestFileDiagnosticsViewer_List_EmptyStorage tests listing empty storage.
//
// # Description
//
// Verifies that List() returns empty slice for empty storage.
//
// # Test Steps
//
//  1. Create test viewer with empty storage
//  2. List
//  3. Verify empty slice returned
func TestFileDiagnosticsViewer_List_EmptyStorage(t *testing.T) {
	viewer, _, cleanup := newTestViewer(t)
	defer cleanup()

	ctx := context.Background()
	summaries, err := viewer.List(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if len(summaries) != 0 {
		t.Errorf("List() on empty storage returned %d items, want 0", len(summaries))
	}
}

// -----------------------------------------------------------------------------
// GetByTraceID Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_GetByTraceID_Found tests successful trace ID lookup.
//
// # Description
//
// Verifies that GetByTraceID() can find a diagnostic by trace ID.
// This is the "Support Ticket Revolution" feature.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create diagnostic with known trace ID
//  3. Call GetByTraceID()
//  4. Verify correct diagnostic returned
func TestFileDiagnosticsViewer_GetByTraceID_Found(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostics
	targetTraceID := "target-trace-id-12345"
	createTestDiagnosticSet(t, storage) // Creates other diagnostics
	createTestDiagnostic(t, storage, targetTraceID, "support_ticket", SeverityError, time.Now().UnixMilli())

	// Find by trace ID
	ctx := context.Background()
	data, err := viewer.GetByTraceID(ctx, targetTraceID)
	if err != nil {
		t.Fatalf("GetByTraceID() failed: %v", err)
	}

	// Verify
	if data.Header.TraceID != targetTraceID {
		t.Errorf("TraceID = %q, want %q", data.Header.TraceID, targetTraceID)
	}
	if data.Header.Reason != "support_ticket" {
		t.Errorf("Reason = %q, want support_ticket", data.Header.Reason)
	}
}

// TestFileDiagnosticsViewer_GetByTraceID_NotFound tests error for missing trace ID.
//
// # Description
//
// Verifies that GetByTraceID() returns an error when trace ID doesn't exist.
//
// # Test Steps
//
//  1. Create test viewer with some diagnostics
//  2. Call GetByTraceID() with non-existent trace
//  3. Verify error returned
func TestFileDiagnosticsViewer_GetByTraceID_NotFound(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create some diagnostics (but not with our target trace ID)
	createTestDiagnosticSet(t, storage)

	// Try to find non-existent trace ID
	ctx := context.Background()
	_, err := viewer.GetByTraceID(ctx, "nonexistent-trace-id")
	if err == nil {
		t.Error("GetByTraceID() should return error for missing trace ID")
	}
}

// TestFileDiagnosticsViewer_GetByTraceID_ContextCancellation tests cancellation.
//
// # Description
//
// Verifies that GetByTraceID() respects context cancellation during search.
//
// # Test Steps
//
//  1. Create test viewer with many diagnostics
//  2. Cancel context
//  3. Call GetByTraceID()
//  4. Verify context error returned
func TestFileDiagnosticsViewer_GetByTraceID_ContextCancellation(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create several diagnostics
	for i := 0; i < 10; i++ {
		createTestDiagnostic(t, storage, "trace-"+string(rune('a'+i)), "test", SeverityInfo, time.Now().UnixMilli())
		time.Sleep(time.Millisecond) // Ensure unique filenames
	}

	// Cancel context before search
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Search should fail with context error
	_, err := viewer.GetByTraceID(ctx, "nonexistent")
	if err != context.Canceled {
		t.Errorf("GetByTraceID() with cancelled context returned %v, want context.Canceled", err)
	}
}

// -----------------------------------------------------------------------------
// Cache Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_Cache_HitOnSecondGet tests cache behavior.
//
// # Description
//
// Verifies that Get() uses cache on second access.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Create a diagnostic
//  3. Get once (populates cache)
//  4. Delete underlying file
//  5. Get again (should return cached data)
func TestFileDiagnosticsViewer_Cache_HitOnSecondGet(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostic
	path := createTestDiagnostic(t, storage, "trace-cache", "cache_test", SeverityInfo, time.Now().UnixMilli())

	ctx := context.Background()

	// First get (populates cache)
	data1, err := viewer.Get(ctx, path)
	if err != nil {
		t.Fatalf("First Get() failed: %v", err)
	}

	// Delete underlying file
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	// Second get (should use cache)
	data2, err := viewer.Get(ctx, path)
	if err != nil {
		t.Fatalf("Second Get() failed (cache miss?): %v", err)
	}

	// Verify same data
	if data1.Header.TraceID != data2.Header.TraceID {
		t.Error("Cache returned different data")
	}
}

// TestFileDiagnosticsViewer_ClearCache tests cache clearing.
//
// # Description
//
// Verifies that ClearCache() removes cached entries.
//
// # Test Steps
//
//  1. Create test viewer and storage
//  2. Get a diagnostic (populates cache)
//  3. Delete underlying file
//  4. Clear cache
//  5. Get again (should fail - file missing and cache cleared)
func TestFileDiagnosticsViewer_ClearCache(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	// Create test diagnostic
	path := createTestDiagnostic(t, storage, "trace-clear", "clear_test", SeverityInfo, time.Now().UnixMilli())

	ctx := context.Background()

	// Populate cache
	_, err := viewer.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	// Delete file and clear cache
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}
	viewer.ClearCache()

	// Get should now fail
	_, err = viewer.Get(ctx, path)
	if err == nil {
		t.Error("Get() should fail after ClearCache() and file deletion")
	}
}

// TestFileDiagnosticsViewer_Cache_Eviction tests cache size limiting.
//
// # Description
//
// Verifies that cache evicts entries when limit is reached.
//
// # Test Steps
//
//  1. Create test viewer
//  2. Create and get 101 diagnostics (exceeds 100 limit)
//  3. First diagnostic should be evicted from cache
func TestFileDiagnosticsViewer_Cache_Eviction(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	ctx := context.Background()
	paths := make([]string, 101)

	// Create and cache 101 diagnostics
	for i := 0; i < 101; i++ {
		traceID := "trace-evict-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		path := createTestDiagnostic(t, storage, traceID, "eviction_test", SeverityInfo, time.Now().UnixMilli())
		paths[i] = path

		_, err := viewer.Get(ctx, path)
		if err != nil {
			t.Fatalf("Get() failed for item %d: %v", i, err)
		}

		time.Sleep(time.Millisecond) // Ensure unique filenames
	}

	// Delete first file - if evicted from cache, Get should fail
	if err := os.Remove(paths[0]); err != nil {
		t.Fatalf("failed to remove first file: %v", err)
	}

	// First diagnostic should have been evicted (cache cleared at 100)
	_, err := viewer.Get(ctx, paths[0])
	if err == nil {
		t.Error("First diagnostic should have been evicted from cache")
	}
}

// -----------------------------------------------------------------------------
// Heuristic Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_looksLikeTraceID tests trace ID detection heuristic.
//
// # Description
//
// Verifies the heuristic correctly identifies trace IDs vs file paths.
//
// # Test Cases
//
//   - Trace ID format: should return true
//   - File path: should return false
//   - Filename with extension: should return false
func TestFileDiagnosticsViewer_looksLikeTraceID(t *testing.T) {
	viewer, _, cleanup := newTestViewer(t)
	defer cleanup()

	testCases := []struct {
		input    string
		expected bool
	}{
		{"1704463200000000000-12345", true},
		{"1234567890-99999", true},
		{"/path/to/file.json", false},
		{"diag-20240105-100000.json", false},
		{"simple.json", false},
		{"C:\\Windows\\file.json", false},
		{"short", false},
		{"", false},
		{"trace-001", false}, // Too short digit count
	}

	for _, tc := range testCases {
		result := viewer.looksLikeTraceID(tc.input)
		if result != tc.expected {
			t.Errorf("looksLikeTraceID(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

// -----------------------------------------------------------------------------
// Integration Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsViewer_Integration_FullWorkflow tests complete viewer workflow.
//
// # Description
//
// Tests a realistic workflow: create diagnostics, list, filter, view details.
//
// # Test Steps
//
//  1. Create viewer and storage
//  2. Store multiple diagnostics
//  3. List with filters
//  4. Get specific diagnostic by trace ID
//  5. Verify all operations work together
func TestFileDiagnosticsViewer_Integration_FullWorkflow(t *testing.T) {
	viewer, storage, cleanup := newTestViewer(t)
	defer cleanup()

	ctx := context.Background()

	// Store several diagnostics
	traceID1 := "integration-trace-001"
	traceID2 := "integration-trace-002"
	createTestDiagnostic(t, storage, traceID1, "startup_failure", SeverityError, time.Now().UnixMilli())
	time.Sleep(2 * time.Millisecond)
	createTestDiagnostic(t, storage, traceID2, "manual_request", SeverityInfo, time.Now().UnixMilli())

	// List all
	summaries, err := viewer.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("Expected 2 diagnostics, got %d", len(summaries))
	}

	// List errors only
	errors, err := viewer.List(ctx, ListOptions{Limit: 10, Severity: SeverityError})
	if err != nil {
		t.Fatalf("List() with filter failed: %v", err)
	}
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}

	// Get by trace ID (Support Ticket Revolution)
	data, err := viewer.GetByTraceID(ctx, traceID1)
	if err != nil {
		t.Fatalf("GetByTraceID() failed: %v", err)
	}
	if data.Header.Reason != "startup_failure" {
		t.Errorf("Reason = %q, want startup_failure", data.Header.Reason)
	}

	// Get from list summary
	if len(summaries) > 0 {
		detail, err := viewer.Get(ctx, summaries[0].Location)
		if err != nil {
			t.Fatalf("Get() from summary failed: %v", err)
		}
		if detail.Header.TraceID != summaries[0].TraceID {
			t.Error("Detail trace ID doesn't match summary")
		}
	}
}

// Compile-time interface verification.
var _ DiagnosticsViewer = (*FileDiagnosticsViewer)(nil)
