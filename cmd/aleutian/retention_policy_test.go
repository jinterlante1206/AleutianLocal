// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultRetentionConfig(t *testing.T) {
	config := DefaultRetentionConfig()

	if config.DryRun {
		t.Error("DryRun should default to false")
	}
	if !config.LogActions {
		t.Error("LogActions should default to true")
	}
	if config.MaxConcurrency <= 0 {
		t.Error("MaxConcurrency should be positive")
	}
}

func TestNewRetentionEnforcer(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	if enforcer == nil {
		t.Fatal("NewRetentionEnforcer returned nil")
	}

	// Should have default policies
	policies := enforcer.ListPolicies()
	if len(policies) == 0 {
		t.Error("Should have default policies registered")
	}
}

func TestNewRetentionEnforcer_DefaultsZeroMaxConcurrency(t *testing.T) {
	config := RetentionConfig{
		MaxConcurrency: 0, // Should be set to default
	}
	enforcer := NewRetentionEnforcer(config)

	if enforcer == nil {
		t.Fatal("NewRetentionEnforcer returned nil")
	}
}

func TestRetentionEnforcer_RegisterPolicy(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	err := enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "custom_data",
		RetentionDays: 60,
		Action:        ActionDelete,
		BasePath:      "/custom/path",
		FilePattern:   "*.dat",
	})

	if err != nil {
		t.Errorf("RegisterPolicy failed: %v", err)
	}

	policy, exists := enforcer.GetPolicy("custom_data")
	if !exists {
		t.Error("Registered policy should exist")
	}
	if policy.RetentionDays != 60 {
		t.Errorf("RetentionDays = %d, want 60", policy.RetentionDays)
	}
}

func TestRetentionEnforcer_RegisterPolicy_Validation(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	tests := []struct {
		name    string
		policy  RetentionPolicy
		wantErr bool
	}{
		{
			name: "empty category",
			policy: RetentionPolicy{
				Category: "",
			},
			wantErr: true,
		},
		{
			name: "negative retention",
			policy: RetentionPolicy{
				Category:      "test",
				RetentionDays: -1,
			},
			wantErr: true,
		},
		{
			name: "valid policy",
			policy: RetentionPolicy{
				Category:      "valid",
				RetentionDays: 30,
				Action:        ActionDelete,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforcer.RegisterPolicy(tt.policy)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterPolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRetentionEnforcer_GetPolicy(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	// Default policy should exist
	_, exists := enforcer.GetPolicy("temp_files")
	if !exists {
		t.Error("Default policy 'temp_files' should exist")
	}

	// Non-existent policy
	_, exists = enforcer.GetPolicy("nonexistent")
	if exists {
		t.Error("Non-existent policy should not exist")
	}
}

func TestRetentionEnforcer_ListPolicies(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	policies := enforcer.ListPolicies()

	// Should have multiple default policies
	if len(policies) < 3 {
		t.Errorf("Should have at least 3 default policies, got %d", len(policies))
	}

	// Check that policies have required fields
	for _, p := range policies {
		if p.Category == "" {
			t.Error("Policy category should not be empty")
		}
	}
}

func TestRetentionEnforcer_SetDryRun(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	enforcer.SetDryRun(true)
	// No direct way to check, but should not panic
}

func TestRetentionEnforcer_Enforce_EmptyBasePath(t *testing.T) {
	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	// Most default policies have empty base paths
	result, err := enforcer.Enforce(context.Background())

	if err != nil {
		t.Errorf("Enforce failed: %v", err)
	}

	// Should complete without errors (nothing to do)
	if result.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}
	if result.EndTime.IsZero() {
		t.Error("EndTime should be set")
	}
}

func TestRetentionEnforcer_Enforce_WithFiles(t *testing.T) {
	// Create temp directory with test files
	tempDir := t.TempDir()

	// Create some old files
	oldFile := filepath.Join(tempDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 10 days ago
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a recent file
	newFile := filepath.Join(tempDir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true, // Don't actually delete
	})

	// Register a policy for test files
	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_files",
		RetentionDays: 7, // 7 days
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
		MinFiles:      0,
	})

	result, err := enforcer.EnforcePolicy(context.Background(), "test_files")

	if err != nil {
		t.Errorf("EnforcePolicy failed: %v", err)
	}

	if result.FilesProcessed != 2 {
		t.Errorf("FilesProcessed = %d, want 2", result.FilesProcessed)
	}

	// Should identify 1 file for deletion (old.txt is > 7 days)
	if result.FilesActioned != 1 {
		t.Errorf("FilesActioned = %d, want 1", result.FilesActioned)
	}

	if !result.DryRun {
		t.Error("DryRun should be true")
	}

	// Files should still exist (dry run)
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		t.Error("Old file should still exist in dry run mode")
	}
}

func TestRetentionEnforcer_Enforce_ActualDelete(t *testing.T) {
	// Create temp directory with test files
	tempDir := t.TempDir()

	// Create an old file
	oldFile := filepath.Join(tempDir, "delete_me.txt")
	if err := os.WriteFile(oldFile, []byte("delete me"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set modification time to 10 days ago
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldFile, oldTime, oldTime)

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: false, // Actually delete
	})

	// Register a policy
	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_delete",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
		MinFiles:      0,
	})

	_, err := enforcer.EnforcePolicy(context.Background(), "test_delete")

	if err != nil {
		t.Errorf("EnforcePolicy failed: %v", err)
	}

	// File should be deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Old file should have been deleted")
	}
}

func TestRetentionEnforcer_Enforce_MinFiles(t *testing.T) {
	// Create temp directory with test files
	tempDir := t.TempDir()

	// Create 3 old files
	for i := 0; i < 3; i++ {
		file := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		// Make all files old
		oldTime := time.Now().AddDate(0, 0, -10-i)
		os.Chtimes(file, oldTime, oldTime)
	}

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	// Register policy with MinFiles = 2
	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_min_files",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
		MinFiles:      2, // Keep at least 2 files
	})

	result, err := enforcer.EnforcePolicy(context.Background(), "test_min_files")

	if err != nil {
		t.Errorf("EnforcePolicy failed: %v", err)
	}

	// Should only action 1 file (keep 2)
	if result.FilesActioned != 1 {
		t.Errorf("FilesActioned = %d, want 1 (should keep 2 minimum)", result.FilesActioned)
	}
}

func TestRetentionEnforcer_Enforce_Recursive(t *testing.T) {
	// Create temp directory with subdirectories
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	os.MkdirAll(subDir, 0755)

	// Create files in both directories
	rootFile := filepath.Join(tempDir, "root.txt")
	subFile := filepath.Join(subDir, "sub.txt")

	os.WriteFile(rootFile, []byte("root"), 0644)
	os.WriteFile(subFile, []byte("sub"), 0644)

	// Make both old
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(rootFile, oldTime, oldTime)
	os.Chtimes(subFile, oldTime, oldTime)

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	// Non-recursive policy
	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "non_recursive",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
		Recursive:     false,
	})

	result, _ := enforcer.EnforcePolicy(context.Background(), "non_recursive")
	if result.FilesProcessed != 1 {
		t.Errorf("Non-recursive: FilesProcessed = %d, want 1", result.FilesProcessed)
	}

	// Recursive policy
	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "recursive",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
		Recursive:     true,
	})

	result, _ = enforcer.EnforcePolicy(context.Background(), "recursive")
	if result.FilesProcessed != 2 {
		t.Errorf("Recursive: FilesProcessed = %d, want 2", result.FilesProcessed)
	}
}

func TestRetentionEnforcer_Enforce_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create several files
	for i := 0; i < 10; i++ {
		file := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(file, []byte("content"), 0644)
		oldTime := time.Now().AddDate(0, 0, -10)
		os.Chtimes(file, oldTime, oldTime)
	}

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_cancel",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
	})

	// Cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := enforcer.Enforce(ctx)
	if err == nil {
		t.Error("Should return error when context is cancelled")
	}
}

func TestRetentionEnforcer_EnforcePolicy_NotFound(t *testing.T) {
	enforcer := NewRetentionEnforcer(DefaultRetentionConfig())

	_, err := enforcer.EnforcePolicy(context.Background(), "nonexistent")

	if err == nil {
		t.Error("Should return error for non-existent policy")
	}
}

func TestRetentionEnforcer_Archive(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	archiveDir := filepath.Join(tempDir, "archive")
	os.MkdirAll(archiveDir, 0755)

	sourceDir := filepath.Join(tempDir, "source")
	os.MkdirAll(sourceDir, 0755)

	// Create an old file
	oldFile := filepath.Join(sourceDir, "archive_me.txt")
	os.WriteFile(oldFile, []byte("archive content"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldFile, oldTime, oldTime)

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun:      false,
		ArchivePath: archiveDir,
	})

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_archive",
		RetentionDays: 7,
		Action:        ActionArchive,
		BasePath:      sourceDir,
		FilePattern:   "*.txt",
	})

	_, err := enforcer.EnforcePolicy(context.Background(), "test_archive")

	if err != nil {
		t.Errorf("EnforcePolicy failed: %v", err)
	}

	// File should be moved to archive
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Original file should have been moved")
	}

	archivedFile := filepath.Join(archiveDir, "archive_me.txt")
	if _, err := os.Stat(archivedFile); os.IsNotExist(err) {
		t.Error("File should exist in archive")
	}
}

func TestRetentionEnforcer_ArchiveNoPath(t *testing.T) {
	tempDir := t.TempDir()

	oldFile := filepath.Join(tempDir, "test.txt")
	os.WriteFile(oldFile, []byte("content"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldFile, oldTime, oldTime)

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun:      false,
		ArchivePath: "", // No archive path
	})

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "test_no_archive",
		RetentionDays: 7,
		Action:        ActionArchive,
		BasePath:      tempDir,
		FilePattern:   "*.txt",
	})

	result, _ := enforcer.EnforcePolicy(context.Background(), "test_no_archive")

	// Should have errors because archive path is not configured
	if len(result.Errors) == 0 {
		t.Error("Should have errors when archive path is not configured")
	}
}

func TestRetentionAction_String(t *testing.T) {
	tests := []struct {
		action   RetentionAction
		expected string
	}{
		{ActionDelete, "delete"},
		{ActionArchive, "archive"},
		{ActionAnonymize, "anonymize"},
		{RetentionAction(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.action.String()
		if got != tt.expected {
			t.Errorf("%d.String() = %q, want %q", tt.action, got, tt.expected)
		}
	}
}

func TestRetentionEnforcer_InterfaceCompliance(t *testing.T) {
	var _ RetentionEnforcer = (*DefaultRetentionEnforcer)(nil)
}

func TestRetentionEnforcer_NonExistentBasePath(t *testing.T) {
	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "nonexistent_path",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      "/nonexistent/path/that/does/not/exist",
		FilePattern:   "*.txt",
	})

	result, err := enforcer.EnforcePolicy(context.Background(), "nonexistent_path")

	// Should not error - just nothing to do
	if err != nil {
		t.Errorf("Should not error for non-existent path: %v", err)
	}
	if result.FilesProcessed != 0 {
		t.Errorf("FilesProcessed = %d, want 0", result.FilesProcessed)
	}
}

func TestEnforcementResult_Aggregation(t *testing.T) {
	// Create temp directories for multiple policies
	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	// Create files in each
	for _, dir := range []string{tempDir1, tempDir2} {
		file := filepath.Join(dir, "test.txt")
		os.WriteFile(file, []byte("content"), 0644)
		oldTime := time.Now().AddDate(0, 0, -10)
		os.Chtimes(file, oldTime, oldTime)
	}

	enforcer := NewRetentionEnforcer(RetentionConfig{
		DryRun: true,
	})

	// Clear defaults and add our test policies
	enforcer.policies = make(map[string]RetentionPolicy)

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "policy1",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir1,
		FilePattern:   "*.txt",
	})

	enforcer.RegisterPolicy(RetentionPolicy{
		Category:      "policy2",
		RetentionDays: 7,
		Action:        ActionDelete,
		BasePath:      tempDir2,
		FilePattern:   "*.txt",
	})

	result, err := enforcer.Enforce(context.Background())

	if err != nil {
		t.Errorf("Enforce failed: %v", err)
	}

	// Should have results for both policies
	if len(result.Results) != 2 {
		t.Errorf("Results count = %d, want 2", len(result.Results))
	}

	// Totals should be aggregated
	if result.TotalFilesProcessed < 2 {
		t.Errorf("TotalFilesProcessed = %d, want >= 2", result.TotalFilesProcessed)
	}
}
