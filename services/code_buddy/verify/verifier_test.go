// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/manifest"
)

// computeTestHash is a helper for computing SHA256 of content.
func computeTestHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// createTestFile creates a file with the given content and returns its FileEntry.
func createTestFile(t *testing.T, dir, name string, content []byte) manifest.FileEntry {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	stat, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}

	return manifest.FileEntry{
		Path:  name,
		Hash:  computeTestHash(content),
		Mtime: stat.ModTime().UnixNano(),
		Size:  stat.Size(),
	}
}

func TestNewVerifier(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		v := NewVerifier()

		if v == nil {
			t.Fatal("NewVerifier returned nil")
		}
		if v.mtimeResolution != DefaultMtimeResolution {
			t.Errorf("mtimeResolution = %v, want %v", v.mtimeResolution, DefaultMtimeResolution)
		}
		if v.parallelLimit != DefaultParallelLimit {
			t.Errorf("parallelLimit = %d, want %d", v.parallelLimit, DefaultParallelLimit)
		}
		if v.cache == nil {
			t.Error("cache should not be nil")
		}
		if v.manifestManager == nil {
			t.Error("manifestManager should not be nil")
		}
	})

	t.Run("custom options", func(t *testing.T) {
		customCache := NewVerificationCache()
		callback := func(progress RebuildProgress) {}

		v := NewVerifier(
			WithMtimeResolution(5*time.Second),
			WithParallelLimit(20),
			WithVerificationCache(customCache),
			WithRebuildCallback(callback),
		)

		if v.mtimeResolution != 5*time.Second {
			t.Errorf("mtimeResolution = %v, want 5s", v.mtimeResolution)
		}
		if v.parallelLimit != 20 {
			t.Errorf("parallelLimit = %d, want 20", v.parallelLimit)
		}
		if v.cache != customCache {
			t.Error("cache not set correctly")
		}
		if v.rebuildCallback == nil {
			t.Error("rebuildCallback should be set")
		}
	})
}

func TestVerifier_FastVerify(t *testing.T) {
	t.Run("fresh file (unchanged)", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("package main\n")
		entry := createTestFile(t, dir, "main.go", content)

		// Wait to ensure we're outside mtime resolution
		time.Sleep(10 * time.Millisecond)

		v := NewVerifier(WithMtimeResolution(5 * time.Millisecond))
		ctx := context.Background()

		result, err := v.FastVerify(ctx, dir, "main.go", entry)
		if err != nil {
			t.Fatalf("FastVerify failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("Status = %v, want StatusFresh", result.Status)
		}
		if !result.AllFresh {
			t.Error("AllFresh should be true")
		}
		if len(result.StaleFiles) > 0 {
			t.Errorf("StaleFiles = %v, want empty", result.StaleFiles)
		}
	})

	t.Run("deleted file", func(t *testing.T) {
		dir := t.TempDir()
		entry := manifest.FileEntry{
			Path:  "deleted.go",
			Hash:  "abc123",
			Mtime: time.Now().UnixNano(),
			Size:  100,
		}

		v := NewVerifier()
		ctx := context.Background()

		result, err := v.FastVerify(ctx, dir, "deleted.go", entry)
		if err != nil {
			t.Fatalf("FastVerify failed: %v", err)
		}

		if result.Status != StatusStale {
			t.Errorf("Status = %v, want StatusStale", result.Status)
		}
		if len(result.DeletedFiles) != 1 || result.DeletedFiles[0] != "deleted.go" {
			t.Errorf("DeletedFiles = %v, want [deleted.go]", result.DeletedFiles)
		}
	})

	t.Run("modified file (content changed)", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("package main\n")
		entry := createTestFile(t, dir, "main.go", content)

		// Modify the file
		time.Sleep(10 * time.Millisecond)
		newContent := []byte("package main\n\nfunc foo() {}\n")
		if err := os.WriteFile(filepath.Join(dir, "main.go"), newContent, 0644); err != nil {
			t.Fatalf("failed to modify file: %v", err)
		}

		v := NewVerifier(WithMtimeResolution(5 * time.Millisecond))
		ctx := context.Background()

		result, err := v.FastVerify(ctx, dir, "main.go", entry)
		if err != nil {
			t.Fatalf("FastVerify failed: %v", err)
		}

		if result.Status != StatusStale {
			t.Errorf("Status = %v, want StatusStale", result.Status)
		}
		if len(result.StaleFiles) != 1 || result.StaleFiles[0] != "main.go" {
			t.Errorf("StaleFiles = %v, want [main.go]", result.StaleFiles)
		}
	})

	t.Run("verification cache hit", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("package main\n")
		entry := createTestFile(t, dir, "main.go", content)

		v := NewVerifier(WithMtimeResolution(1 * time.Millisecond))
		ctx := context.Background()

		// Wait to get outside mtime resolution
		time.Sleep(5 * time.Millisecond)

		// First verify
		result1, err := v.FastVerify(ctx, dir, "main.go", entry)
		if err != nil {
			t.Fatalf("first FastVerify failed: %v", err)
		}
		if result1.Status != StatusFresh {
			t.Errorf("first Status = %v, want StatusFresh", result1.Status)
		}

		// Second verify should hit cache
		result2, err := v.FastVerify(ctx, dir, "main.go", entry)
		if err != nil {
			t.Fatalf("second FastVerify failed: %v", err)
		}
		if result2.Status != StatusFresh {
			t.Errorf("second Status = %v, want StatusFresh", result2.Status)
		}
		// The second call should be faster (cache hit)
		// We can't easily test timing, but at least verify no error
	})
}

func TestVerifier_VerifyFiles(t *testing.T) {
	t.Run("all fresh files", func(t *testing.T) {
		dir := t.TempDir()

		entries := make(map[string]manifest.FileEntry)
		for i := 0; i < 5; i++ {
			name := filepath.Join("src", "file"+string(rune('a'+i))+".go")
			content := []byte("package main\n// file " + name + "\n")
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0644); err != nil {
				t.Fatal(err)
			}
			stat, _ := os.Lstat(path)
			entries[name] = manifest.FileEntry{
				Path:  name,
				Hash:  computeTestHash(content),
				Mtime: stat.ModTime().UnixNano(),
				Size:  stat.Size(),
			}
		}

		// Wait to ensure outside mtime resolution
		time.Sleep(10 * time.Millisecond)

		v := NewVerifier(WithMtimeResolution(5 * time.Millisecond))
		ctx := context.Background()

		result, err := v.VerifyFiles(ctx, dir, entries)
		if err != nil {
			t.Fatalf("VerifyFiles failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("Status = %v, want StatusFresh", result.Status)
		}
		if !result.AllFresh {
			t.Error("AllFresh should be true")
		}
		if result.FilesChecked != 5 {
			t.Errorf("FilesChecked = %d, want 5", result.FilesChecked)
		}
	})

	t.Run("empty entries", func(t *testing.T) {
		dir := t.TempDir()
		v := NewVerifier()
		ctx := context.Background()

		result, err := v.VerifyFiles(ctx, dir, map[string]manifest.FileEntry{})
		if err != nil {
			t.Fatalf("VerifyFiles failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("Status = %v, want StatusFresh", result.Status)
		}
		if !result.AllFresh {
			t.Error("AllFresh should be true for empty entries")
		}
	})

	t.Run("some stale files", func(t *testing.T) {
		dir := t.TempDir()

		// Create files
		entries := make(map[string]manifest.FileEntry)
		for i := 0; i < 3; i++ {
			name := "file" + string(rune('a'+i)) + ".go"
			content := []byte("package main\n// file " + name + "\n")
			if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
				t.Fatal(err)
			}
			stat, _ := os.Lstat(filepath.Join(dir, name))
			entries[name] = manifest.FileEntry{
				Path:  name,
				Hash:  computeTestHash(content),
				Mtime: stat.ModTime().UnixNano(),
				Size:  stat.Size(),
			}
		}

		// Wait and modify one file
		time.Sleep(10 * time.Millisecond)
		newContent := []byte("package main\n// MODIFIED\n")
		if err := os.WriteFile(filepath.Join(dir, "filea.go"), newContent, 0644); err != nil {
			t.Fatal(err)
		}

		v := NewVerifier(WithMtimeResolution(5 * time.Millisecond))
		ctx := context.Background()

		result, err := v.VerifyFiles(ctx, dir, entries)
		if err != nil {
			t.Fatalf("VerifyFiles failed: %v", err)
		}

		if result.Status != StatusPartiallyStale && result.Status != StatusStale {
			t.Errorf("Status = %v, want StatusPartiallyStale or StatusStale", result.Status)
		}
		if result.AllFresh {
			t.Error("AllFresh should be false")
		}
		if len(result.StaleFiles) != 1 {
			t.Errorf("len(StaleFiles) = %d, want 1", len(result.StaleFiles))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		dir := t.TempDir()

		entries := make(map[string]manifest.FileEntry)
		for i := 0; i < 10; i++ {
			name := "file" + string(rune('a'+i)) + ".go"
			content := []byte("package main\n")
			if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
				t.Fatal(err)
			}
			stat, _ := os.Lstat(filepath.Join(dir, name))
			entries[name] = manifest.FileEntry{
				Path:  name,
				Hash:  computeTestHash(content),
				Mtime: stat.ModTime().UnixNano(),
				Size:  stat.Size(),
			}
		}

		// Cancel immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		v := NewVerifier()

		result, err := v.VerifyFiles(ctx, dir, entries)
		if err == nil {
			// Might or might not error depending on timing
			// Just verify result is not nil
			if result == nil {
				t.Error("expected non-nil result")
			}
		}
	})
}

func TestVerifier_VerifyManifest(t *testing.T) {
	t.Run("nil manifest returns fresh", func(t *testing.T) {
		v := NewVerifier()
		ctx := context.Background()

		result, err := v.VerifyManifest(ctx, "/some/path", nil)
		if err != nil {
			t.Fatalf("VerifyManifest failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("Status = %v, want StatusFresh", result.Status)
		}
		if !result.AllFresh {
			t.Error("AllFresh should be true for nil manifest")
		}
	})

	t.Run("manifest with files", func(t *testing.T) {
		dir := t.TempDir()

		m := manifest.NewManifest(dir)
		content := []byte("package main\n")
		if err := os.WriteFile(filepath.Join(dir, "main.go"), content, 0644); err != nil {
			t.Fatal(err)
		}
		stat, _ := os.Lstat(filepath.Join(dir, "main.go"))
		m.Files["main.go"] = manifest.FileEntry{
			Path:  "main.go",
			Hash:  computeTestHash(content),
			Mtime: stat.ModTime().UnixNano(),
			Size:  stat.Size(),
		}

		// Wait to get outside mtime resolution
		time.Sleep(10 * time.Millisecond)

		v := NewVerifier(WithMtimeResolution(5 * time.Millisecond))
		ctx := context.Background()

		result, err := v.VerifyManifest(ctx, dir, m)
		if err != nil {
			t.Fatalf("VerifyManifest failed: %v", err)
		}

		if result.Status != StatusFresh {
			t.Errorf("Status = %v, want StatusFresh", result.Status)
		}
	})
}

func TestVerifier_InvalidateCache(t *testing.T) {
	t.Run("invalidate all", func(t *testing.T) {
		v := NewVerifier()

		// Add some entries to cache
		v.cache.MarkVerified("a.go")
		v.cache.MarkVerified("b.go")

		v.InvalidateCache()

		if v.cache.Size() != 0 {
			t.Errorf("cache.Size() = %d, want 0", v.cache.Size())
		}
	})

	t.Run("invalidate single path", func(t *testing.T) {
		v := NewVerifier()

		v.cache.MarkVerified("a.go")
		v.cache.MarkVerified("b.go")

		v.InvalidatePath("a.go")

		if v.cache.Size() != 1 {
			t.Errorf("cache.Size() = %d, want 1", v.cache.Size())
		}
		if !v.cache.NeedsVerification("a.go") {
			t.Error("a.go should need verification")
		}
		if v.cache.NeedsVerification("b.go") {
			t.Error("b.go should not need verification")
		}
	})
}

func TestVerifyResult(t *testing.T) {
	t.Run("HasChanges", func(t *testing.T) {
		tests := []struct {
			name   string
			result VerifyResult
			want   bool
		}{
			{
				name:   "no changes",
				result: VerifyResult{},
				want:   false,
			},
			{
				name:   "stale files",
				result: VerifyResult{StaleFiles: []string{"a.go"}},
				want:   true,
			},
			{
				name:   "deleted files",
				result: VerifyResult{DeletedFiles: []string{"b.go"}},
				want:   true,
			},
			{
				name:   "both stale and deleted",
				result: VerifyResult{StaleFiles: []string{"a.go"}, DeletedFiles: []string{"b.go"}},
				want:   true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.result.HasChanges()
				if got != tt.want {
					t.Errorf("HasChanges() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("StaleCount", func(t *testing.T) {
		result := VerifyResult{
			StaleFiles:   []string{"a.go", "b.go"},
			DeletedFiles: []string{"c.go"},
		}

		if result.StaleCount() != 3 {
			t.Errorf("StaleCount() = %d, want 3", result.StaleCount())
		}
	})
}

func TestVerifyStatus_String(t *testing.T) {
	tests := []struct {
		status VerifyStatus
		want   string
	}{
		{StatusFresh, "fresh"},
		{StatusStale, "stale"},
		{StatusPartiallyStale, "partially_stale"},
		{StatusError, "error"},
		{VerifyStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHashesEqual(t *testing.T) {
	t.Run("equal hashes", func(t *testing.T) {
		hash := "abc123def456"
		if !hashesEqual(hash, hash) {
			t.Error("identical hashes should be equal")
		}
	})

	t.Run("different hashes", func(t *testing.T) {
		if hashesEqual("abc", "def") {
			t.Error("different hashes should not be equal")
		}
	})

	t.Run("different lengths", func(t *testing.T) {
		if hashesEqual("abc", "abcd") {
			t.Error("different length hashes should not be equal")
		}
	})

	t.Run("empty hashes", func(t *testing.T) {
		if !hashesEqual("", "") {
			t.Error("empty hashes should be equal")
		}
	})
}
