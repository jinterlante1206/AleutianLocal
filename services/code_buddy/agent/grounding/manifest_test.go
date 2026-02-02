// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewFileManifest(t *testing.T) {
	t.Run("default extensions", func(t *testing.T) {
		m := NewFileManifest(nil)
		if m == nil {
			t.Fatal("NewFileManifest returned nil")
		}
		if m.files == nil {
			t.Error("files map not initialized")
		}
		if m.basenames == nil {
			t.Error("basenames map not initialized")
		}
		// Should use default extensions
		if !m.extensions[".go"] {
			t.Error("default extensions should include .go")
		}
	})

	t.Run("custom extensions", func(t *testing.T) {
		m := NewFileManifest([]string{".xyz", "abc"}) // one with dot, one without
		if !m.extensions[".xyz"] {
			t.Error("custom extension .xyz should be included")
		}
		if !m.extensions[".abc"] {
			t.Error("custom extension .abc should be included (dot added)")
		}
		if m.extensions[".go"] {
			t.Error("default extensions should not be included when custom provided")
		}
	})
}

func TestFileManifest_ScanDir(t *testing.T) {
	// Create a temp directory with test files
	tmpDir := t.TempDir()

	// Create test directory structure
	dirs := []string{
		"pkg/server",
		"internal/handler",
		".hidden",
		"node_modules/pkg",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatalf("creating dir %s: %v", dir, err)
		}
	}

	// Create test files
	files := map[string]string{
		"main.go":                     "package main",
		"pkg/server/server.go":        "package server",
		"pkg/server/server_test.go":   "package server",
		"internal/handler/handler.go": "package handler",
		"README.md":                   "# Test",
		".hidden/secret.go":           "package secret", // should be skipped
		"node_modules/pkg/pkg.js":     "export {}",      // should be skipped
	}
	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("creating file %s: %v", path, err)
		}
	}

	t.Run("scans directory successfully", func(t *testing.T) {
		m := NewFileManifest(nil)
		err := m.ScanDir(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("ScanDir failed: %v", err)
		}

		// Should have found the non-hidden, non-vendor files
		expectedFiles := []string{
			"main.go",
			"pkg/server/server.go",
			"pkg/server/server_test.go",
			"internal/handler/handler.go",
			"README.md",
		}
		for _, ef := range expectedFiles {
			if !m.Contains(ef) {
				t.Errorf("expected file %s not found in manifest", ef)
			}
		}

		// Should NOT have found hidden or vendor files
		unexpectedFiles := []string{
			".hidden/secret.go",
			"node_modules/pkg/pkg.js",
		}
		for _, uf := range unexpectedFiles {
			if m.Contains(uf) {
				t.Errorf("unexpected file %s found in manifest (should be skipped)", uf)
			}
		}
	})

	t.Run("sets metadata correctly", func(t *testing.T) {
		m := NewFileManifest(nil)
		before := time.Now()
		err := m.ScanDir(context.Background(), tmpDir)
		after := time.Now()

		if err != nil {
			t.Fatalf("ScanDir failed: %v", err)
		}

		if m.Root() != tmpDir {
			t.Errorf("Root() = %q, want %q", m.Root(), tmpDir)
		}

		loadedAt := m.LoadedAt()
		if loadedAt.Before(before) || loadedAt.After(after) {
			t.Errorf("LoadedAt() = %v, want between %v and %v", loadedAt, before, after)
		}

		if m.FileCount() == 0 {
			t.Error("FileCount() should be > 0")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		m := NewFileManifest(nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := m.ScanDir(ctx, tmpDir)
		if err == nil {
			// May or may not error depending on timing, but shouldn't panic
		}
	})

	t.Run("clears previous data", func(t *testing.T) {
		m := NewFileManifest(nil)

		// First scan
		err := m.ScanDir(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("first ScanDir failed: %v", err)
		}
		firstCount := m.FileCount()

		// Create a different temp dir with fewer files
		tmpDir2 := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir2, "only.go"), []byte("package only"), 0644); err != nil {
			t.Fatal(err)
		}

		// Second scan should clear previous data
		err = m.ScanDir(context.Background(), tmpDir2)
		if err != nil {
			t.Fatalf("second ScanDir failed: %v", err)
		}

		if m.FileCount() >= firstCount {
			t.Errorf("FileCount() = %d, should be less than %d after re-scan", m.FileCount(), firstCount)
		}
		if !m.Contains("only.go") {
			t.Error("new file should be present after re-scan")
		}
	})
}

func TestFileManifest_Contains(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	files := []string{"main.go", "pkg/server/server.go"}
	for _, f := range files {
		dir := filepath.Dir(filepath.Join(tmpDir, f))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	m := NewFileManifest(nil)
	if err := m.ScanDir(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"exact match", "main.go", true},
		{"nested path", "pkg/server/server.go", true},
		{"with leading ./", "./main.go", true},
		{"with leading /", "/main.go", true}, // normalizePath strips leading /
		{"basename only", "server.go", true}, // matches by basename
		{"non-existent", "does_not_exist.go", false},
		{"wrong extension", "main.py", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Contains(tt.path)
			if got != tt.expected {
				t.Errorf("Contains(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestFileManifest_IsStale(t *testing.T) {
	t.Run("uninitialized is stale", func(t *testing.T) {
		m := NewFileManifest(nil)
		if !m.IsStale(time.Hour) {
			t.Error("uninitialized manifest should be stale")
		}
	})

	t.Run("recently loaded is fresh", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		m := NewFileManifest(nil)
		if err := m.ScanDir(context.Background(), tmpDir); err != nil {
			t.Fatal(err)
		}

		if m.IsStale(time.Hour) {
			t.Error("recently loaded manifest should not be stale")
		}
	})

	t.Run("respects TTL", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		m := NewFileManifest(nil)
		if err := m.ScanDir(context.Background(), tmpDir); err != nil {
			t.Fatal(err)
		}

		// With very short TTL, should be stale after brief wait
		time.Sleep(10 * time.Millisecond)
		if !m.IsStale(1 * time.Nanosecond) {
			t.Error("manifest should be stale with 1ns TTL after 10ms")
		}
	})
}

func TestFileManifest_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewFileManifest(nil)
	if err := m.ScanDir(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			_ = m.Contains("test.go")
		}()

		go func() {
			defer wg.Done()
			_ = m.IsStale(time.Hour)
		}()

		go func() {
			defer wg.Done()
			_ = m.FileCount()
		}()
	}

	wg.Wait()
	// Test passes if no race conditions detected
}

func TestFileManifest_ToKnownFiles(t *testing.T) {
	tmpDir := t.TempDir()
	files := []string{"main.go", "util.go"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	m := NewFileManifest(nil)
	if err := m.ScanDir(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	knownFiles := m.ToKnownFiles()

	// Should contain the files
	if !knownFiles["main.go"] {
		t.Error("ToKnownFiles should contain main.go")
	}
	if !knownFiles["util.go"] {
		t.Error("ToKnownFiles should contain util.go")
	}

	// Should be a copy, not the original
	knownFiles["fake.go"] = true
	if m.Contains("fake.go") {
		t.Error("ToKnownFiles should return a copy, not the original map")
	}
}
