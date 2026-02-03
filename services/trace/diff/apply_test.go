// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package diff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewApplier(t *testing.T) {
	t.Run("valid_path", func(t *testing.T) {
		tmpDir := t.TempDir()
		applier, err := NewApplier(tmpDir, DefaultApplyOptions())
		if err != nil {
			t.Fatalf("NewApplier() error = %v", err)
		}
		if applier == nil {
			t.Fatal("NewApplier() returned nil")
		}
	})

	t.Run("relative_path_rejected", func(t *testing.T) {
		_, err := NewApplier("relative/path", DefaultApplyOptions())
		if err == nil {
			t.Fatal("Expected error for relative path")
		}
	})

	t.Run("nonexistent_path_rejected", func(t *testing.T) {
		_, err := NewApplier("/nonexistent/path/12345", DefaultApplyOptions())
		if err == nil {
			t.Fatal("Expected error for nonexistent path")
		}
	})

	t.Run("file_path_rejected", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := NewApplier(tmpFile, DefaultApplyOptions())
		if err == nil {
			t.Fatal("Expected error for file path (not directory)")
		}
	})
}

func TestApplier_Apply_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "new_file.go",
		NewContent: "package main\n",
		IsNew:      true,
		Hunks: []*Hunk{
			{
				Status: HunkAccepted,
				Lines: []DiffLine{
					{Type: LineAdded, Content: "package main"},
				},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false, error = %s", result.Error)
	}
	if !result.Applied {
		t.Error("Apply() applied = false")
	}

	// Verify file was created
	content, err := os.ReadFile(filepath.Join(tmpDir, "new_file.go"))
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("File content = %q, want %q", string(content), "package main")
	}
}

func TestApplier_Apply_NewFile_AllRejected(t *testing.T) {
	tmpDir := t.TempDir()
	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "rejected_file.go",
		NewContent: "package main\n",
		IsNew:      true,
		Hunks: []*Hunk{
			{
				Status: HunkRejected,
				Lines: []DiffLine{
					{Type: LineAdded, Content: "package main"},
				},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false")
	}
	if result.Applied {
		t.Error("Apply() applied = true, expected false for rejected changes")
	}

	// Verify file was NOT created
	_, err = os.Stat(filepath.Join(tmpDir, "rejected_file.go"))
	if !os.IsNotExist(err) {
		t.Error("File should not exist when all hunks rejected")
	}
}

func TestApplier_Apply_Modification(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial file
	initialContent := "line1\nline2\nline3\n"
	filePath := filepath.Join(tmpDir, "existing.go")
	if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "existing.go",
		OldContent: initialContent,
		NewContent: "line1\nmodified\nline3\n",
		Hunks: []*Hunk{
			{
				OldStart: 1,
				OldCount: 3,
				NewStart: 1,
				NewCount: 3,
				Status:   HunkAccepted,
				Lines: []DiffLine{
					{Type: LineContext, Content: "line1", OldNum: 1, NewNum: 1},
					{Type: LineRemoved, Content: "line2", OldNum: 2},
					{Type: LineAdded, Content: "modified", NewNum: 2},
					{Type: LineContext, Content: "line3", OldNum: 3, NewNum: 3},
				},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false, error = %s", result.Error)
	}
	if result.HunksApplied != 1 {
		t.Errorf("HunksApplied = %d, want 1", result.HunksApplied)
	}

	// Verify content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	expected := "line1\nmodified\nline3\n"
	if string(content) != expected {
		t.Errorf("File content = %q, want %q", string(content), expected)
	}
}

func TestApplier_Apply_Delete(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file to delete
	filePath := filepath.Join(tmpDir, "to_delete.go")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "to_delete.go",
		OldContent: "content",
		IsDelete:   true,
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false, error = %s", result.Error)
	}
	if !result.Applied {
		t.Error("Apply() applied = false")
	}

	// Verify file was deleted
	_, err = os.Stat(filePath)
	if !os.IsNotExist(err) {
		t.Error("File should not exist after deletion")
	}
}

func TestApplier_Apply_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	options := DefaultApplyOptions()
	options.DryRun = true

	applier, err := NewApplier(tmpDir, options)
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "dryrun.go",
		NewContent: "package main",
		IsNew:      true,
		Hunks: []*Hunk{
			{
				Status: HunkAccepted,
				Lines: []DiffLine{
					{Type: LineAdded, Content: "package main"},
				},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false")
	}
	if result.Applied {
		t.Error("Apply() applied = true, expected false for dry run")
	}

	// Verify file was NOT created
	_, err = os.Stat(filepath.Join(tmpDir, "dryrun.go"))
	if !os.IsNotExist(err) {
		t.Error("File should not exist in dry run mode")
	}
}

func TestApplier_Apply_WithBackup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial file
	filePath := filepath.Join(tmpDir, "backup_test.go")
	originalContent := "original"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	options := DefaultApplyOptions()
	options.CreateBackups = true

	applier, err := NewApplier(tmpDir, options)
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath:   "backup_test.go",
		OldContent: originalContent,
		NewContent: "modified",
		Hunks: []*Hunk{
			{
				OldStart: 1,
				OldCount: 1,
				NewStart: 1,
				NewCount: 1,
				Status:   HunkAccepted,
				Lines: []DiffLine{
					{Type: LineRemoved, Content: "original", OldNum: 1},
					{Type: LineAdded, Content: "modified", NewNum: 1},
				},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.BackupPath == "" {
		t.Error("Expected backup path to be set")
	}

	// Verify backup exists with original content
	backupContent, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("Failed to read backup: %v", err)
	}
	if string(backupContent) != originalContent {
		t.Errorf("Backup content = %q, want %q", string(backupContent), originalContent)
	}
}

func TestApplier_PathSecurity(t *testing.T) {
	tmpDir := t.TempDir()
	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	// Attempt path traversal
	change := &ProposedChange{
		FilePath:   "../../../etc/passwd",
		NewContent: "malicious",
		IsNew:      true,
		Hunks: []*Hunk{
			{
				Status: HunkAccepted,
				Lines: []DiffLine{
					{Type: LineAdded, Content: "malicious"},
				},
			},
		},
	}

	_, err = applier.Apply(context.Background(), change)
	if err == nil {
		t.Fatal("Expected error for path traversal attempt")
	}
}

func TestApplier_ApplyAll(t *testing.T) {
	tmpDir := t.TempDir()
	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	changes := []*ProposedChange{
		{
			FilePath: "file1.go",
			IsNew:    true,
			Hunks: []*Hunk{
				{
					Status: HunkAccepted,
					Lines: []DiffLine{
						{Type: LineAdded, Content: "package file1"},
					},
				},
			},
		},
		{
			FilePath: "file2.go",
			IsNew:    true,
			Hunks: []*Hunk{
				{
					Status: HunkAccepted,
					Lines: []DiffLine{
						{Type: LineAdded, Content: "package file2"},
					},
				},
			},
		},
	}

	results, err := applier.ApplyAll(context.Background(), changes)
	if err != nil {
		t.Fatalf("ApplyAll() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	for i, result := range results {
		if !result.Success {
			t.Errorf("Result %d success = false, error = %s", i, result.Error)
		}
	}
}

func TestApplier_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	applier, err := NewApplier(tmpDir, DefaultApplyOptions())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	change := &ProposedChange{
		FilePath: "cancelled.go",
		IsNew:    true,
		Hunks: []*Hunk{
			{
				Status: HunkAccepted,
				Lines:  []DiffLine{{Type: LineAdded, Content: "test"}},
			},
		},
	}

	_, err = applier.Apply(ctx, change)
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
}

func TestApplier_CreateDirs(t *testing.T) {
	tmpDir := t.TempDir()

	options := DefaultApplyOptions()
	options.CreateDirs = true

	applier, err := NewApplier(tmpDir, options)
	if err != nil {
		t.Fatal(err)
	}

	change := &ProposedChange{
		FilePath: "nested/dir/file.go",
		IsNew:    true,
		Hunks: []*Hunk{
			{
				Status: HunkAccepted,
				Lines:  []DiffLine{{Type: LineAdded, Content: "package nested"}},
			},
		},
	}

	result, err := applier.Apply(context.Background(), change)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Apply() success = false, error = %s", result.Error)
	}

	// Verify nested directories were created
	_, err = os.Stat(filepath.Join(tmpDir, "nested", "dir", "file.go"))
	if err != nil {
		t.Errorf("Nested file not created: %v", err)
	}
}

func TestDefaultApplyOptions(t *testing.T) {
	opts := DefaultApplyOptions()

	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
	if opts.CreateBackups {
		t.Error("CreateBackups should be false by default")
	}
	if opts.BackupSuffix != ".orig" {
		t.Errorf("BackupSuffix = %q, want %q", opts.BackupSuffix, ".orig")
	}
	if !opts.CreateDirs {
		t.Error("CreateDirs should be true by default")
	}
	if opts.FileMode != 0644 {
		t.Errorf("FileMode = %o, want %o", opts.FileMode, 0644)
	}
	if opts.DirMode != 0755 {
		t.Errorf("DirMode = %o, want %o", opts.DirMode, 0755)
	}
}
