// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManifestManager_Scan(t *testing.T) {
	t.Run("empty directory returns empty manifest", func(t *testing.T) {
		tmpDir := t.TempDir()
		manager := NewManifestManager(WithIncludes("**/*"))

		manifest, err := manager.Scan(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		if manifest.Files == nil {
			t.Error("Files is nil, want empty map")
		}
		if len(manifest.Files) != 0 {
			t.Errorf("len(Files) = %d, want 0", len(manifest.Files))
		}
		if manifest.ProjectRoot != tmpDir {
			t.Errorf("ProjectRoot = %s, want %s", manifest.ProjectRoot, tmpDir)
		}
	})

	t.Run("directory with files returns all matching files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test files
		files := map[string]string{
			"main.go":       "package main",
			"utils/util.go": "package utils",
			"readme.md":     "# README",
		}
		for path, content := range files {
			fullPath := filepath.Join(tmpDir, path)
			os.MkdirAll(filepath.Dir(fullPath), 0755)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}

		manager := NewManifestManager(WithIncludes("**/*.go"))
		manifest, err := manager.Scan(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		// Should have 2 .go files
		if len(manifest.Files) != 2 {
			t.Errorf("len(Files) = %d, want 2", len(manifest.Files))
		}

		// Verify files are present
		if _, ok := manifest.Files["main.go"]; !ok {
			t.Error("main.go not in manifest")
		}
		if _, ok := manifest.Files[filepath.Join("utils", "util.go")]; !ok {
			t.Error("utils/util.go not in manifest")
		}
	})

	t.Run("excludes are respected", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files including vendor
		files := []string{
			"main.go",
			"vendor/dep/dep.go",
		}
		for _, path := range files {
			fullPath := filepath.Join(tmpDir, path)
			os.MkdirAll(filepath.Dir(fullPath), 0755)
			if err := os.WriteFile(fullPath, []byte("package x"), 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}

		manager := NewManifestManager(
			WithIncludes("**/*.go"),
			WithExcludes("vendor/**"),
		)
		manifest, err := manager.Scan(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		if len(manifest.Files) != 1 {
			t.Errorf("len(Files) = %d, want 1", len(manifest.Files))
		}
		if _, ok := manifest.Files["main.go"]; !ok {
			t.Error("main.go should be in manifest")
		}
	})

	t.Run("large file is skipped with error", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a large file
		largePath := filepath.Join(tmpDir, "large.go")
		if err := os.WriteFile(largePath, make([]byte, 200), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		manager := NewManifestManager(
			WithIncludes("**/*.go"),
			WithMaxFileSize(100),
		)
		manifest, err := manager.Scan(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		// File should not be in Files but in Errors
		if len(manifest.Files) != 0 {
			t.Errorf("len(Files) = %d, want 0", len(manifest.Files))
		}
		if len(manifest.Errors) != 1 {
			t.Errorf("len(Errors) = %d, want 1", len(manifest.Errors))
		}
		if !errors.Is(manifest.Errors[0].Err, ErrFileTooLarge) {
			t.Errorf("error = %v, want ErrFileTooLarge", manifest.Errors[0].Err)
		}
	})

	t.Run("invalid root returns error", func(t *testing.T) {
		manager := NewManifestManager()
		_, err := manager.Scan(context.Background(), "/nonexistent/path")
		if err == nil {
			t.Error("Scan = nil, want error for invalid root")
		}
		if !errors.Is(err, ErrInvalidRoot) {
			t.Errorf("error = %v, want ErrInvalidRoot", err)
		}
	})

	t.Run("file as root returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "file.txt")
		if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		manager := NewManifestManager()
		_, err := manager.Scan(context.Background(), filePath)
		if err == nil {
			t.Error("Scan = nil, want error for file as root")
		}
	})

	t.Run("context cancellation returns partial manifest", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create many files
		for i := 0; i < 100; i++ {
			path := filepath.Join(tmpDir, filepath.FromSlash("file"+string(rune('0'+i%10))+".go"))
			if err := os.WriteFile(path, []byte("package x"), 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		manager := NewManifestManager(WithIncludes("**/*.go"))
		manifest, err := manager.Scan(ctx, tmpDir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}

		if !manifest.Incomplete {
			t.Error("Incomplete = false, want true")
		}
	})
}

func TestManifestManager_Diff(t *testing.T) {
	t.Run("nil old manifest treats all as added", func(t *testing.T) {
		manager := NewManifestManager()
		newManifest := NewManifest("/test")
		newManifest.Files["a.go"] = FileEntry{Path: "a.go", Hash: "abc123"}
		newManifest.Files["b.go"] = FileEntry{Path: "b.go", Hash: "def456"}

		changes := manager.Diff(nil, newManifest)

		if len(changes.Added) != 2 {
			t.Errorf("len(Added) = %d, want 2", len(changes.Added))
		}
		if len(changes.Modified) != 0 {
			t.Errorf("len(Modified) = %d, want 0", len(changes.Modified))
		}
		if len(changes.Deleted) != 0 {
			t.Errorf("len(Deleted) = %d, want 0", len(changes.Deleted))
		}
	})

	t.Run("no changes returns empty", func(t *testing.T) {
		manager := NewManifestManager()
		hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

		old := NewManifest("/test")
		old.Files["a.go"] = FileEntry{Path: "a.go", Hash: hash}

		new := NewManifest("/test")
		new.Files["a.go"] = FileEntry{Path: "a.go", Hash: hash}

		changes := manager.Diff(old, new)

		if changes.HasChanges() {
			t.Error("HasChanges() = true, want false")
		}
	})

	t.Run("added file detected", func(t *testing.T) {
		manager := NewManifestManager()

		old := NewManifest("/test")
		old.Files["a.go"] = FileEntry{Path: "a.go", Hash: "hash1"}

		new := NewManifest("/test")
		new.Files["a.go"] = FileEntry{Path: "a.go", Hash: "hash1"}
		new.Files["b.go"] = FileEntry{Path: "b.go", Hash: "hash2"}

		changes := manager.Diff(old, new)

		if len(changes.Added) != 1 {
			t.Errorf("len(Added) = %d, want 1", len(changes.Added))
		}
		if changes.Added[0] != "b.go" {
			t.Errorf("Added[0] = %s, want b.go", changes.Added[0])
		}
	})

	t.Run("modified file detected", func(t *testing.T) {
		manager := NewManifestManager()

		old := NewManifest("/test")
		old.Files["a.go"] = FileEntry{Path: "a.go", Hash: "oldhash"}

		new := NewManifest("/test")
		new.Files["a.go"] = FileEntry{Path: "a.go", Hash: "newhash"}

		changes := manager.Diff(old, new)

		if len(changes.Modified) != 1 {
			t.Errorf("len(Modified) = %d, want 1", len(changes.Modified))
		}
		if changes.Modified[0] != "a.go" {
			t.Errorf("Modified[0] = %s, want a.go", changes.Modified[0])
		}
	})

	t.Run("deleted file detected", func(t *testing.T) {
		manager := NewManifestManager()

		old := NewManifest("/test")
		old.Files["a.go"] = FileEntry{Path: "a.go", Hash: "hash1"}
		old.Files["b.go"] = FileEntry{Path: "b.go", Hash: "hash2"}

		new := NewManifest("/test")
		new.Files["a.go"] = FileEntry{Path: "a.go", Hash: "hash1"}

		changes := manager.Diff(old, new)

		if len(changes.Deleted) != 1 {
			t.Errorf("len(Deleted) = %d, want 1", len(changes.Deleted))
		}
		if changes.Deleted[0] != "b.go" {
			t.Errorf("Deleted[0] = %s, want b.go", changes.Deleted[0])
		}
	})
}

func TestManifestManager_QuickCheck(t *testing.T) {
	t.Run("unchanged file returns false", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// Get file info
		info, _ := os.Stat(path)

		manager := NewManifestManager()
		hasher := NewSHA256Hasher(0)
		hash, _ := hasher.HashFile(path)

		entry := FileEntry{
			Path:  "test.go",
			Hash:  hash,
			Mtime: info.ModTime().UnixNano(),
			Size:  info.Size(),
		}

		changed, err := manager.QuickCheck(context.Background(), tmpDir, entry)
		if err != nil {
			t.Fatalf("QuickCheck: %v", err)
		}
		if changed {
			t.Error("changed = true, want false")
		}
	})

	t.Run("modified file returns true", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// Get initial hash
		manager := NewManifestManager()
		hasher := NewSHA256Hasher(0)
		hash, _ := hasher.HashFile(path)

		entry := FileEntry{
			Path:  "test.go",
			Hash:  hash,
			Mtime: time.Now().Add(-1 * time.Hour).UnixNano(), // Old mtime
			Size:  7,                                         // Old size
		}

		// Wait a bit and modify
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(path, []byte("new content"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		changed, err := manager.QuickCheck(context.Background(), tmpDir, entry)
		if err != nil {
			t.Fatalf("QuickCheck: %v", err)
		}
		if !changed {
			t.Error("changed = false, want true")
		}
	})

	t.Run("deleted file returns true", func(t *testing.T) {
		tmpDir := t.TempDir()

		manager := NewManifestManager()
		entry := FileEntry{
			Path:  "deleted.go",
			Hash:  "somehash",
			Mtime: time.Now().UnixNano(),
			Size:  100,
		}

		changed, err := manager.QuickCheck(context.Background(), tmpDir, entry)
		if err != nil {
			t.Fatalf("QuickCheck: %v", err)
		}
		if !changed {
			t.Error("changed = false, want true for deleted file")
		}
	})

	t.Run("path traversal returns error", func(t *testing.T) {
		tmpDir := t.TempDir()

		manager := NewManifestManager()
		entry := FileEntry{
			Path: "../../../etc/passwd",
			Hash: "somehash",
		}

		_, err := manager.QuickCheck(context.Background(), tmpDir, entry)
		if err == nil {
			t.Error("QuickCheck = nil, want ErrPathTraversal")
		}
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("error = %v, want ErrPathTraversal", err)
		}
	})
}

func TestValidatePath(t *testing.T) {
	t.Run("normal relative path passes", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := validatePath(tmpDir, "src/main.go"); err != nil {
			t.Errorf("validatePath = %v, want nil", err)
		}
	})

	t.Run("path with .. fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := validatePath(tmpDir, "../etc/passwd")
		if err == nil {
			t.Error("validatePath = nil, want ErrPathTraversal")
		}
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("error = %v, want ErrPathTraversal", err)
		}
	})

	t.Run("absolute path inside root passes", func(t *testing.T) {
		tmpDir := t.TempDir()
		absPath := filepath.Join(tmpDir, "src", "main.go")
		if err := validatePath(tmpDir, absPath); err != nil {
			t.Errorf("validatePath = %v, want nil", err)
		}
	})

	t.Run("absolute path outside root fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := validatePath(tmpDir, "/etc/passwd")
		if err == nil {
			t.Error("validatePath = nil, want ErrPathTraversal")
		}
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("error = %v, want ErrPathTraversal", err)
		}
	})
}
