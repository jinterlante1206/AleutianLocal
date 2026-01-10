// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package resilience

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// DefaultBackupConfig Tests
// =============================================================================

func TestDefaultBackupConfig(t *testing.T) {
	config := DefaultBackupConfig()

	if config.MaxBackups <= 0 {
		t.Error("MaxBackups should be positive")
	}
	if config.BackupSuffix == "" {
		t.Error("BackupSuffix should have default value")
	}
	if config.TimeFormat == "" {
		t.Error("TimeFormat should have default value")
	}
}

// =============================================================================
// NewBackupManager Tests
// =============================================================================

func TestNewBackupManager(t *testing.T) {
	tests := []struct {
		name   string
		config BackupConfig
	}{
		{
			name:   "with defaults",
			config: DefaultBackupConfig(),
		},
		{
			name: "with zero values",
			config: BackupConfig{
				MaxBackups: 0, // Should be set to default
			},
		},
		{
			name: "with custom values",
			config: BackupConfig{
				MaxBackups:   10,
				BackupSuffix: ".bak",
				TimeFormat:   "20060102",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewBackupManager(tt.config)
			if mgr == nil {
				t.Fatal("NewBackupManager returned nil")
			}
		})
	}
}

// =============================================================================
// BackupBeforeOverwrite Tests
// =============================================================================

func TestBackupManager_BackupBeforeOverwrite_NonExistent(t *testing.T) {
	mgr := NewBackupManager(DefaultBackupConfig())

	// Backup of non-existent path should return empty string, no error
	backupPath, err := mgr.BackupBeforeOverwrite("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("BackupBeforeOverwrite returned error: %v", err)
	}
	if backupPath != "" {
		t.Errorf("BackupBeforeOverwrite returned path for non-existent file: %s", backupPath)
	}
}

func TestBackupManager_BackupBeforeOverwrite_File(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file
	content := []byte("test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	// Backup the file
	backupPath, err := mgr.BackupBeforeOverwrite(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeOverwrite failed: %v", err)
	}

	if backupPath == "" {
		t.Fatal("BackupBeforeOverwrite returned empty path")
	}

	// Verify backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("Backup file does not exist: %s", backupPath)
	}

	// Verify backup has correct content
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("Failed to read backup: %v", err)
	}
	if string(backupContent) != string(content) {
		t.Errorf("Backup content = %q, want %q", backupContent, content)
	}

	// Original should still exist
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Original file should still exist")
	}
}

func TestBackupManager_BackupBeforeOverwrite_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "testdir")

	// Create test directory with file
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	testFile := filepath.Join(testDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	// Backup the directory
	backupPath, err := mgr.BackupBeforeOverwrite(testDir)
	if err != nil {
		t.Fatalf("BackupBeforeOverwrite failed: %v", err)
	}

	if backupPath == "" {
		t.Fatal("BackupBeforeOverwrite returned empty path")
	}

	// Verify backup exists
	info, err := os.Stat(backupPath)
	if os.IsNotExist(err) {
		t.Errorf("Backup directory does not exist: %s", backupPath)
	}
	if !info.IsDir() {
		t.Error("Backup should be a directory")
	}

	// Verify backup contains file
	backupFile := filepath.Join(backupPath, "file.txt")
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		t.Error("Backup should contain the original file")
	}

	// Original directory should NOT exist (it was renamed)
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("Original directory should have been renamed to backup")
	}
}

// =============================================================================
// ListBackups Tests
// =============================================================================

func TestBackupManager_ListBackups(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Use millisecond precision to ensure unique timestamps
	mgr := NewBackupManager(BackupConfig{
		MaxBackups:   10,
		BackupSuffix: ".backup",
		TimeFormat:   "2006-01-02_150405.000",
	})

	// Create multiple backups
	var createdBackups []string
	for i := 0; i < 3; i++ {
		backupPath, err := mgr.BackupBeforeOverwrite(testFile)
		if err != nil {
			t.Fatalf("BackupBeforeOverwrite failed: %v", err)
		}
		createdBackups = append(createdBackups, backupPath)
		time.Sleep(50 * time.Millisecond) // Ensure different timestamps
	}

	// List backups
	backups, err := mgr.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 3 {
		t.Errorf("ListBackups returned %d backups, want 3", len(backups))
	}

	// Should be sorted newest first
	for i := 1; i < len(backups); i++ {
		if backups[i].CreatedAt.After(backups[i-1].CreatedAt) {
			t.Error("Backups should be sorted newest first")
		}
	}
}

func TestBackupManager_ListBackups_NoBackups(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file but no backups
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	backups, err := mgr.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("ListBackups returned %d backups, want 0", len(backups))
	}
}

// =============================================================================
// RestoreBackup Tests
// =============================================================================

func TestBackupManager_RestoreBackup(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file
	originalContent := []byte("original content")
	if err := os.WriteFile(testFile, originalContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	// Create backup
	backupPath, err := mgr.BackupBeforeOverwrite(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeOverwrite failed: %v", err)
	}

	// Modify original
	newContent := []byte("modified content")
	if err := os.WriteFile(testFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Verify content changed
	content, _ := os.ReadFile(testFile)
	if string(content) != "modified content" {
		t.Error("File should have been modified")
	}

	// Restore backup
	if err := mgr.RestoreBackup(backupPath); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// Verify original content restored
	content, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}
	if string(content) != string(originalContent) {
		t.Errorf("Restored content = %q, want %q", content, originalContent)
	}

	// Backup should no longer exist
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("Backup should have been removed after restore")
	}
}

// =============================================================================
// CleanOldBackups Tests
// =============================================================================

func TestBackupManager_CleanOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	// Create backup
	_, err := mgr.BackupBeforeOverwrite(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeOverwrite failed: %v", err)
	}

	// Clean with very long maxAge (shouldn't remove anything)
	removed, err := mgr.CleanOldBackups(testFile, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanOldBackups failed: %v", err)
	}
	if removed != 0 {
		t.Errorf("CleanOldBackups removed %d, want 0", removed)
	}

	// Clean with 0 maxAge (should remove all)
	removed, err = mgr.CleanOldBackups(testFile, 0)
	if err != nil {
		t.Fatalf("CleanOldBackups failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("CleanOldBackups removed %d, want 1", removed)
	}
}

// =============================================================================
// Backup Rotation Tests
// =============================================================================

func TestBackupManager_BackupRotation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create test file
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Use small MaxBackups
	mgr := NewBackupManager(BackupConfig{
		MaxBackups:   2,
		BackupSuffix: ".backup",
		TimeFormat:   "2006-01-02_150405.000", // Include milliseconds
	})

	// Create more backups than MaxBackups
	for i := 0; i < 5; i++ {
		_, err := mgr.BackupBeforeOverwrite(testFile)
		if err != nil {
			t.Fatalf("BackupBeforeOverwrite failed: %v", err)
		}
		time.Sleep(5 * time.Millisecond) // Ensure different timestamps
	}

	// Should only have MaxBackups remaining
	backups, err := mgr.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) > 2 {
		t.Errorf("Should have at most 2 backups after rotation, got %d", len(backups))
	}
}

// =============================================================================
// Backup Path Format Tests
// =============================================================================

func TestBackupManager_BackupPath_Format(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	mgr := NewBackupManager(DefaultBackupConfig())

	backupPath, err := mgr.BackupBeforeOverwrite(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeOverwrite failed: %v", err)
	}

	// Backup should be in same directory
	if filepath.Dir(backupPath) != tmpDir {
		t.Errorf("Backup dir = %s, want %s", filepath.Dir(backupPath), tmpDir)
	}

	// Backup should have correct prefix
	if !strings.HasPrefix(filepath.Base(backupPath), "config.yaml.backup.") {
		t.Errorf("Backup name = %s, should start with 'config.yaml.backup.'", filepath.Base(backupPath))
	}
}

// =============================================================================
// Convenience Function Tests
// =============================================================================

func TestBackupBeforeOverwrite_Convenience(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	backupPath, err := BackupBeforeOverwriteFunc(testFile)
	if err != nil {
		t.Fatalf("BackupBeforeOverwriteFunc failed: %v", err)
	}

	if backupPath == "" {
		t.Error("BackupBeforeOverwriteFunc returned empty path")
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestBackupManager_InterfaceCompliance(t *testing.T) {
	var _ BackupManager = (*DefaultBackupManager)(nil)
}
