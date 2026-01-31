// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// FILE MANAGER TESTS
// =============================================================================

func TestNewFileManager(t *testing.T) {
	fm := NewFileManager("/project/root", nil)

	if fm.projectRoot != "/project/root" {
		t.Errorf("projectRoot = %v, want /project/root", fm.projectRoot)
	}
	if fm.backups == nil {
		t.Error("backups map should be initialized")
	}
	if fm.createdFiles == nil {
		t.Error("createdFiles map should be initialized")
	}
	if fm.logger == nil {
		t.Error("logger should be set to default")
	}
}

func TestFileManager_WriteTest(t *testing.T) {
	t.Run("writes new test file", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		tc := &TestCase{
			Name:     "TestExample",
			FilePath: "example_test.go",
			Content:  "package example\n\nfunc TestExample(t *testing.T) {}\n",
			Language: "go",
		}

		err := fm.WriteTest(tc)
		if err != nil {
			t.Fatalf("WriteTest() error = %v", err)
		}

		// Verify file was written
		fullPath := filepath.Join(dir, "example_test.go")
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}

		if string(content) != tc.Content {
			t.Errorf("file content = %q, want %q", string(content), tc.Content)
		}

		// Verify it's tracked as created
		if fm.CreatedCount() != 1 {
			t.Errorf("CreatedCount() = %d, want 1", fm.CreatedCount())
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		tc := &TestCase{
			Name:     "TestDeep",
			FilePath: "subdir/deep/test_test.go",
			Content:  "package deep\n",
			Language: "go",
		}

		err := fm.WriteTest(tc)
		if err != nil {
			t.Fatalf("WriteTest() error = %v", err)
		}

		fullPath := filepath.Join(dir, "subdir/deep/test_test.go")
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Error("expected file to exist")
		}
	})

	t.Run("backs up existing file", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Create existing file
		existingPath := filepath.Join(dir, "existing_test.go")
		originalContent := "// original content\n"
		if err := os.WriteFile(existingPath, []byte(originalContent), 0644); err != nil {
			t.Fatal(err)
		}

		tc := &TestCase{
			Name:     "TestOverwrite",
			FilePath: "existing_test.go",
			Content:  "// new content\n",
			Language: "go",
		}

		err := fm.WriteTest(tc)
		if err != nil {
			t.Fatalf("WriteTest() error = %v", err)
		}

		// Verify backup was made
		if fm.BackupCount() != 1 {
			t.Errorf("BackupCount() = %d, want 1", fm.BackupCount())
		}

		// Verify file has new content
		content, _ := os.ReadFile(existingPath)
		if string(content) != tc.Content {
			t.Errorf("file content = %q, want %q", string(content), tc.Content)
		}
	})

	t.Run("validates test case", func(t *testing.T) {
		fm := NewFileManager(t.TempDir(), nil)

		tc := &TestCase{
			Name:     "",
			FilePath: "test.go",
			Content:  "content",
			Language: "go",
		}

		err := fm.WriteTest(tc)
		if err != ErrInvalidTestCase {
			t.Errorf("expected ErrInvalidTestCase, got %v", err)
		}
	})

	t.Run("handles absolute path", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager("/different/root", nil)

		absolutePath := filepath.Join(dir, "absolute_test.go")
		tc := &TestCase{
			Name:     "TestAbsolute",
			FilePath: absolutePath,
			Content:  "package test\n",
			Language: "go",
		}

		err := fm.WriteTest(tc)
		if err != nil {
			t.Fatalf("WriteTest() error = %v", err)
		}

		if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
			t.Error("expected file at absolute path")
		}
	})
}

func TestFileManager_ApplyPatch(t *testing.T) {
	t.Run("applies patch to existing file", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Create original file
		filePath := filepath.Join(dir, "code.go")
		originalContent := "package code\n\nfunc Broken() {}\n"
		if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
			t.Fatal(err)
		}

		patch := &Patch{
			FilePath:   "code.go",
			NewContent: "package code\n\nfunc Fixed() {}\n",
		}

		err := fm.ApplyPatch(patch)
		if err != nil {
			t.Fatalf("ApplyPatch() error = %v", err)
		}

		// Verify new content
		content, _ := os.ReadFile(filePath)
		if string(content) != patch.NewContent {
			t.Errorf("file content = %q, want %q", string(content), patch.NewContent)
		}

		// Verify backup was made
		if fm.BackupCount() != 1 {
			t.Errorf("BackupCount() = %d, want 1", fm.BackupCount())
		}

		// Verify OldContent was set
		if patch.OldContent != originalContent {
			t.Errorf("OldContent = %q, want %q", patch.OldContent, originalContent)
		}

		// Verify Applied flag
		if !patch.Applied {
			t.Error("Applied should be true")
		}
	})

	t.Run("creates new file", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		patch := &Patch{
			FilePath:   "new_file.go",
			NewContent: "package new\n",
		}

		err := fm.ApplyPatch(patch)
		if err != nil {
			t.Fatalf("ApplyPatch() error = %v", err)
		}

		// Verify file was created
		fullPath := filepath.Join(dir, "new_file.go")
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Error("expected file to be created")
		}

		// Verify tracked as created
		if fm.CreatedCount() != 1 {
			t.Errorf("CreatedCount() = %d, want 1", fm.CreatedCount())
		}
	})

	t.Run("validates patch", func(t *testing.T) {
		fm := NewFileManager(t.TempDir(), nil)

		patch := &Patch{
			FilePath:   "",
			NewContent: "content",
		}

		err := fm.ApplyPatch(patch)
		if err != ErrInvalidPatch {
			t.Errorf("expected ErrInvalidPatch, got %v", err)
		}
	})
}

func TestFileManager_Rollback(t *testing.T) {
	t.Run("restores backed up files", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Create original file
		filePath := filepath.Join(dir, "code.go")
		originalContent := "// original\n"
		if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Apply a patch
		patch := &Patch{
			FilePath:   "code.go",
			NewContent: "// modified\n",
		}
		fm.ApplyPatch(patch)

		// Rollback
		err := fm.Rollback()
		if err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		// Verify original content restored
		content, _ := os.ReadFile(filePath)
		if string(content) != originalContent {
			t.Errorf("content after rollback = %q, want %q", string(content), originalContent)
		}

		// Verify backups cleared
		if fm.BackupCount() != 0 {
			t.Errorf("BackupCount() = %d, want 0", fm.BackupCount())
		}
	})

	t.Run("removes created files", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Write a new test file
		tc := &TestCase{
			Name:     "TestNew",
			FilePath: "new_test.go",
			Content:  "package test\n",
			Language: "go",
		}
		fm.WriteTest(tc)

		fullPath := filepath.Join(dir, "new_test.go")
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Fatal("file should exist before rollback")
		}

		// Rollback
		err := fm.Rollback()
		if err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		// Verify file removed
		if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
			t.Error("file should be removed after rollback")
		}

		// Verify tracking cleared
		if fm.CreatedCount() != 0 {
			t.Errorf("CreatedCount() = %d, want 0", fm.CreatedCount())
		}
	})

	t.Run("handles mixed backup and created files", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Create existing file
		existingPath := filepath.Join(dir, "existing.go")
		existingContent := "// existing\n"
		os.WriteFile(existingPath, []byte(existingContent), 0644)

		// Modify existing file via patch
		fm.ApplyPatch(&Patch{
			FilePath:   "existing.go",
			NewContent: "// modified existing\n",
		})

		// Create new file via patch
		fm.ApplyPatch(&Patch{
			FilePath:   "new.go",
			NewContent: "// new\n",
		})

		newPath := filepath.Join(dir, "new.go")

		// Rollback
		err := fm.Rollback()
		if err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		// Verify existing file restored
		content, _ := os.ReadFile(existingPath)
		if string(content) != existingContent {
			t.Errorf("existing file not restored")
		}

		// Verify new file removed
		if _, err := os.Stat(newPath); !os.IsNotExist(err) {
			t.Error("new file should be removed")
		}
	})
}

func TestFileManager_Cleanup(t *testing.T) {
	t.Run("removes only created files", func(t *testing.T) {
		dir := t.TempDir()
		fm := NewFileManager(dir, nil)

		// Create existing file and back it up
		existingPath := filepath.Join(dir, "existing.go")
		existingContent := "// existing\n"
		os.WriteFile(existingPath, []byte(existingContent), 0644)

		fm.ApplyPatch(&Patch{
			FilePath:   "existing.go",
			NewContent: "// modified\n",
		})

		// Create new file
		fm.WriteTest(&TestCase{
			Name:     "TestNew",
			FilePath: "new_test.go",
			Content:  "package test\n",
			Language: "go",
		})

		newPath := filepath.Join(dir, "new_test.go")

		// Cleanup
		err := fm.Cleanup()
		if err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}

		// Verify new file removed
		if _, err := os.Stat(newPath); !os.IsNotExist(err) {
			t.Error("new file should be removed")
		}

		// Verify existing file still has modified content (NOT restored)
		content, _ := os.ReadFile(existingPath)
		if string(content) == existingContent {
			t.Error("existing file should NOT be restored by Cleanup")
		}
	})
}

func TestFileManager_HasBackups(t *testing.T) {
	dir := t.TempDir()
	fm := NewFileManager(dir, nil)

	if fm.HasBackups() {
		t.Error("HasBackups() should be false initially")
	}

	// Create and modify a file
	filePath := filepath.Join(dir, "code.go")
	os.WriteFile(filePath, []byte("original"), 0644)

	fm.ApplyPatch(&Patch{
		FilePath:   "code.go",
		NewContent: "modified",
	})

	if !fm.HasBackups() {
		t.Error("HasBackups() should be true after patch")
	}
}

func TestFileManager_BackupCount(t *testing.T) {
	dir := t.TempDir()
	fm := NewFileManager(dir, nil)

	if fm.BackupCount() != 0 {
		t.Error("BackupCount() should be 0 initially")
	}

	// Create and modify files
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644)

	fm.ApplyPatch(&Patch{FilePath: "a.go", NewContent: "modified a"})
	fm.ApplyPatch(&Patch{FilePath: "b.go", NewContent: "modified b"})

	if fm.BackupCount() != 2 {
		t.Errorf("BackupCount() = %d, want 2", fm.BackupCount())
	}
}

func TestFileManager_CreatedCount(t *testing.T) {
	dir := t.TempDir()
	fm := NewFileManager(dir, nil)

	if fm.CreatedCount() != 0 {
		t.Error("CreatedCount() should be 0 initially")
	}

	fm.WriteTest(&TestCase{
		Name:     "TestA",
		FilePath: "a_test.go",
		Content:  "package a",
		Language: "go",
	})

	fm.WriteTest(&TestCase{
		Name:     "TestB",
		FilePath: "b_test.go",
		Content:  "package b",
		Language: "go",
	})

	if fm.CreatedCount() != 2 {
		t.Errorf("CreatedCount() = %d, want 2", fm.CreatedCount())
	}
}
