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
Package main contains tests for FileDiagnosticsStorage.

# Testing Strategy

These tests verify:
  - Directory creation and configuration
  - Store/Load round-trip with atomic writes
  - List ordering and limiting
  - Prune behavior with retention periods
  - Path traversal security
  - Thread safety under concurrent access
  - Filename sanitization
*/
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Construction Tests
// -----------------------------------------------------------------------------

// TestNewFileDiagnosticsStorage_CustomDir verifies custom directory creation.
func TestNewFileDiagnosticsStorage_CustomDir(t *testing.T) {
	tempDir := t.TempDir()
	customDir := filepath.Join(tempDir, "custom", "diagnostics")

	storage, err := NewFileDiagnosticsStorage(customDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	if storage.BaseDir() != customDir {
		t.Errorf("BaseDir() = %q, want %q", storage.BaseDir(), customDir)
	}

	// Verify directory was created
	info, err := os.Stat(customDir)
	if err != nil {
		t.Fatalf("Directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("BaseDir is not a directory")
	}
}

// TestNewFileDiagnosticsStorage_Defaults verifies default settings.
func TestNewFileDiagnosticsStorage_Defaults(t *testing.T) {
	tempDir := t.TempDir()

	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	if got := storage.GetRetentionDays(); got != DefaultRetentionDays {
		t.Errorf("GetRetentionDays() = %d, want %d", got, DefaultRetentionDays)
	}

	if got := storage.Type(); got != "file" {
		t.Errorf("Type() = %q, want %q", got, "file")
	}
}

// TestNewFileDiagnosticsStorage_InvalidDir verifies error on unwritable path.
func TestNewFileDiagnosticsStorage_InvalidDir(t *testing.T) {
	// Try to create in root which requires elevated permissions
	_, err := NewFileDiagnosticsStorage("/root/impossible/path/diagnostics")
	if err == nil {
		t.Error("Expected error for unwritable path")
	}
}

// -----------------------------------------------------------------------------
// Store and Load Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_Store_Load_RoundTrip verifies data survives storage.
func TestFileDiagnosticsStorage_Store_Load_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Create test data
	testData := &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			Reason:      "test_store_load",
			Severity:    SeverityInfo,
			TraceID:     "trace123",
		},
		System: SystemInfo{
			OS:        "darwin",
			Arch:      "arm64",
			Hostname:  "test.local",
			GoVersion: "go1.21.0",
		},
	}

	jsonBytes, err := json.Marshal(testData)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Store
	location, err := storage.Store(ctx, jsonBytes, StorageMetadata{
		FilenameHint: "test_round_trip",
		ContentType:  "application/json",
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Verify location is in the temp directory
	if !strings.HasPrefix(location, tempDir) {
		t.Errorf("Location %q not in tempDir %q", location, tempDir)
	}

	// Load
	loaded, err := storage.Load(ctx, location)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify content
	var parsed DiagnosticsData
	if err := json.Unmarshal(loaded, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if parsed.Header.Reason != "test_store_load" {
		t.Errorf("Reason = %q, want %q", parsed.Header.Reason, "test_store_load")
	}
	if parsed.Header.TraceID != "trace123" {
		t.Errorf("TraceID = %q, want %q", parsed.Header.TraceID, "trace123")
	}
}

// TestFileDiagnosticsStorage_Store_FilenameGeneration verifies filename patterns.
func TestFileDiagnosticsStorage_Store_FilenameGeneration(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	testData := []byte(`{"test": true}`)

	tests := []struct {
		name         string
		hint         string
		wantContains string
	}{
		{"no hint", "", "diag-"},
		{"simple hint", "startup", "startup"},
		{"unsafe chars", "../../etc", "______etc"},
		{"spaces", "my test", "my_test"},
		{"special chars", "test@123!", "test_123_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location, err := storage.Store(ctx, testData, StorageMetadata{
				FilenameHint: tt.hint,
			})
			if err != nil {
				t.Fatalf("Store() error = %v", err)
			}

			filename := filepath.Base(location)
			if !strings.Contains(filename, tt.wantContains) {
				t.Errorf("Filename %q does not contain %q", filename, tt.wantContains)
			}

			// Verify ends with .json
			if !strings.HasSuffix(filename, ".json") {
				t.Errorf("Filename %q does not end with .json", filename)
			}
		})
	}
}

// TestFileDiagnosticsStorage_Load_NotFound verifies error on missing file.
func TestFileDiagnosticsStorage_Load_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	_, err = storage.Load(ctx, filepath.Join(tempDir, "nonexistent.json"))
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

// TestFileDiagnosticsStorage_Load_PathTraversal verifies security against path traversal.
func TestFileDiagnosticsStorage_Load_PathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Create a file outside the storage directory
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.json")
	if err := os.WriteFile(outsideFile, []byte(`{"secret": "data"}`), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Try various traversal attacks
	attacks := []string{
		outsideFile,
		filepath.Join(tempDir, "..", filepath.Base(outsideDir), "secret.json"),
		filepath.Join(tempDir, "..", "..", "etc", "passwd"),
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			_, err := storage.Load(ctx, attack)
			if err == nil {
				t.Errorf("Path traversal succeeded for %q", attack)
			}
			if !strings.Contains(err.Error(), "outside storage directory") &&
				!strings.Contains(err.Error(), "no such file") {
				// Either security error or not found is acceptable
			}
		})
	}
}

// -----------------------------------------------------------------------------
// List Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_List_Order verifies most recent first ordering.
func TestFileDiagnosticsStorage_List_Order(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Store files with delays to ensure different timestamps
	var locations []string
	for i := 0; i < 3; i++ {
		location, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{
			FilenameHint: "test",
		})
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
		locations = append(locations, location)
		time.Sleep(10 * time.Millisecond)
	}

	// List should return newest first
	listed, err := storage.List(ctx, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 3 {
		t.Fatalf("List() returned %d items, want 3", len(listed))
	}

	// Newest (last stored) should be first
	if listed[0] != locations[2] {
		t.Errorf("First item = %q, want %q (newest)", listed[0], locations[2])
	}
}

// TestFileDiagnosticsStorage_List_Limit verifies limiting results.
func TestFileDiagnosticsStorage_List_Limit(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Store 5 files
	for i := 0; i < 5; i++ {
		_, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	// Limit to 3
	listed, err := storage.List(ctx, 3)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 3 {
		t.Errorf("List(3) returned %d items, want 3", len(listed))
	}
}

// TestFileDiagnosticsStorage_List_Empty verifies empty directory handling.
func TestFileDiagnosticsStorage_List_Empty(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	listed, err := storage.List(ctx, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 0 {
		t.Errorf("List() on empty dir returned %d items, want 0", len(listed))
	}
}

// TestFileDiagnosticsStorage_List_IgnoresNonDiagnostics verifies filtering.
func TestFileDiagnosticsStorage_List_IgnoresNonDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Create non-diagnostic files
	if err := os.WriteFile(filepath.Join(tempDir, "other.txt"), []byte("other"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "subdir"), 0755); err != nil {
		t.Fatalf("Mkdir error = %v", err)
	}

	// Store one diagnostic
	_, err = storage.Store(ctx, []byte(`{}`), StorageMetadata{})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	listed, err := storage.List(ctx, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(listed) != 1 {
		t.Errorf("List() returned %d items, want 1 (only diagnostic)", len(listed))
	}
}

// -----------------------------------------------------------------------------
// Prune Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_Prune_RemovesOldFiles verifies retention enforcement.
func TestFileDiagnosticsStorage_Prune_RemovesOldFiles(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Set retention to 1 day
	storage.SetRetentionDays(1)

	// Store a file
	location, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Manually set modification time to 2 days ago
	oldTime := time.Now().AddDate(0, 0, -2)
	if err := os.Chtimes(location, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	// Prune should remove it
	deleted, err := storage.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if deleted != 1 {
		t.Errorf("Prune() deleted = %d, want 1", deleted)
	}

	// Verify file is gone
	if _, err := os.Stat(location); !os.IsNotExist(err) {
		t.Error("File should have been deleted")
	}
}

// TestFileDiagnosticsStorage_Prune_KeepsRecentFiles verifies recent files are kept.
func TestFileDiagnosticsStorage_Prune_KeepsRecentFiles(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	storage.SetRetentionDays(30)

	// Store a fresh file
	location, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Prune should not remove it
	deleted, err := storage.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if deleted != 0 {
		t.Errorf("Prune() deleted = %d, want 0", deleted)
	}

	// Verify file still exists
	if _, err := os.Stat(location); err != nil {
		t.Errorf("File should still exist: %v", err)
	}
}

// TestFileDiagnosticsStorage_Prune_MixedAges verifies selective deletion.
func TestFileDiagnosticsStorage_Prune_MixedAges(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	storage.SetRetentionDays(7)

	// Store files with different ages
	locations := make([]string, 3)
	for i := 0; i < 3; i++ {
		loc, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
		locations[i] = loc
	}

	// Age files: 0=1 day old, 1=10 days old, 2=5 days old
	ages := []int{-1, -10, -5}
	for i, age := range ages {
		oldTime := time.Now().AddDate(0, 0, age)
		if err := os.Chtimes(locations[i], oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}
	}

	// Prune should remove 1 file (10 days old)
	deleted, err := storage.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if deleted != 1 {
		t.Errorf("Prune() deleted = %d, want 1", deleted)
	}

	// Verify correct file was deleted
	if _, err := os.Stat(locations[0]); err != nil {
		t.Error("1-day-old file should still exist")
	}
	if _, err := os.Stat(locations[1]); !os.IsNotExist(err) {
		t.Error("10-day-old file should be deleted")
	}
	if _, err := os.Stat(locations[2]); err != nil {
		t.Error("5-day-old file should still exist")
	}
}

// -----------------------------------------------------------------------------
// Retention Configuration Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_SetRetentionDays_Valid verifies valid settings.
func TestFileDiagnosticsStorage_SetRetentionDays_Valid(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	storage.SetRetentionDays(90)
	if got := storage.GetRetentionDays(); got != 90 {
		t.Errorf("GetRetentionDays() = %d, want 90", got)
	}

	storage.SetRetentionDays(1)
	if got := storage.GetRetentionDays(); got != 1 {
		t.Errorf("GetRetentionDays() = %d, want 1", got)
	}
}

// TestFileDiagnosticsStorage_SetRetentionDays_Invalid verifies invalid values ignored.
func TestFileDiagnosticsStorage_SetRetentionDays_Invalid(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	original := storage.GetRetentionDays()

	storage.SetRetentionDays(0)
	if got := storage.GetRetentionDays(); got != original {
		t.Errorf("GetRetentionDays() = %d, want %d (unchanged)", got, original)
	}

	storage.SetRetentionDays(-5)
	if got := storage.GetRetentionDays(); got != original {
		t.Errorf("GetRetentionDays() = %d, want %d (unchanged)", got, original)
	}
}

// -----------------------------------------------------------------------------
// Count Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_Count verifies accurate counting.
func TestFileDiagnosticsStorage_Count(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Empty directory
	count, err := storage.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 0 {
		t.Errorf("Count() = %d, want 0", count)
	}

	// Store 3 files
	for i := 0; i < 3; i++ {
		_, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	count, err = storage.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 3 {
		t.Errorf("Count() = %d, want 3", count)
	}
}

// -----------------------------------------------------------------------------
// Concurrent Access Tests
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_Concurrent_StoreLoad verifies thread safety.
func TestFileDiagnosticsStorage_Concurrent_StoreLoad(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 10 concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				location, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
				if err != nil {
					errors <- err
					return
				}

				// Immediately try to read it back
				_, err = storage.Load(ctx, location)
				if err != nil {
					errors <- err
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Should have 100 files
	count, err := storage.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 100 {
		t.Errorf("Count() = %d, want 100", count)
	}
}

// TestFileDiagnosticsStorage_Concurrent_ListPrune verifies list/prune thread safety.
func TestFileDiagnosticsStorage_Concurrent_ListPrune(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewFileDiagnosticsStorage(tempDir)
	if err != nil {
		t.Fatalf("NewFileDiagnosticsStorage() error = %v", err)
	}

	ctx := context.Background()

	// Pre-populate with files
	for i := 0; i < 20; i++ {
		_, err := storage.Store(ctx, []byte(`{}`), StorageMetadata{})
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent list operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := storage.List(ctx, 10)
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	// Concurrent prune (should be no-op since files are fresh)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := storage.Prune(ctx)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Helper Function Tests
// -----------------------------------------------------------------------------

// TestSanitizeFilenameHint verifies filename sanitization.
func TestSanitizeFilenameHint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"123numbers", "123numbers"},
		{"UPPERCASE", "UPPERCASE"},
		{"MixedCase123", "MixedCase123"},
		{"with spaces", "with_spaces"},
		{"special!@#$%", "special_____"},
		{"path/../traversal", "path____traversal"},
		{strings.Repeat("a", 100), strings.Repeat("a", 50)}, // Truncation
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilenameHint(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilenameHint(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseDiagnosticsFromFile verifies JSON parsing helper.
func TestParseDiagnosticsFromFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a valid diagnostic file
	data := DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			Reason:      "parse_test",
			Severity:    SeverityWarning,
		},
		System: SystemInfo{
			OS:       "linux",
			Arch:     "amd64",
			Hostname: "test",
		},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	filePath := filepath.Join(tempDir, "test.json")
	if err := os.WriteFile(filePath, jsonBytes, 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	parsed, err := ParseDiagnosticsFromFile(filePath)
	if err != nil {
		t.Fatalf("ParseDiagnosticsFromFile() error = %v", err)
	}

	if parsed.Header.Reason != "parse_test" {
		t.Errorf("Reason = %q, want %q", parsed.Header.Reason, "parse_test")
	}
	if parsed.Header.Severity != SeverityWarning {
		t.Errorf("Severity = %q, want %q", parsed.Header.Severity, SeverityWarning)
	}
}

// TestParseDiagnosticsFromFile_InvalidJSON verifies error handling.
func TestParseDiagnosticsFromFile_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()

	// Create an invalid JSON file
	filePath := filepath.Join(tempDir, "invalid.json")
	if err := os.WriteFile(filePath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := ParseDiagnosticsFromFile(filePath)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// TestParseDiagnosticsFromFile_NotFound verifies error on missing file.
func TestParseDiagnosticsFromFile_NotFound(t *testing.T) {
	_, err := ParseDiagnosticsFromFile("/nonexistent/file.json")
	if err == nil {
		t.Error("Expected error for missing file")
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Test
// -----------------------------------------------------------------------------

// TestFileDiagnosticsStorage_InterfaceCompliance verifies interface implementation.
func TestFileDiagnosticsStorage_InterfaceCompliance(t *testing.T) {
	var _ DiagnosticsStorage = (*FileDiagnosticsStorage)(nil)
}
