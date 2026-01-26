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
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256Hasher_HashFile(t *testing.T) {
	t.Run("produces consistent 64 char lowercase hex", func(t *testing.T) {
		// Create temp file
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		hasher := NewSHA256Hasher(0)
		hash, err := hasher.HashFile(path)
		if err != nil {
			t.Fatalf("HashFile: %v", err)
		}

		// Verify hash format
		if len(hash) != 64 {
			t.Errorf("len(hash) = %d, want 64", len(hash))
		}

		// Verify lowercase hex
		for _, c := range hash {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("invalid character %c in hash", c)
			}
		}

		// Verify consistent
		hash2, err := hasher.HashFile(path)
		if err != nil {
			t.Fatalf("HashFile second call: %v", err)
		}
		if hash != hash2 {
			t.Errorf("hashes differ: %s vs %s", hash, hash2)
		}

		// Known hash for "hello world"
		expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
		if hash != expectedHash {
			t.Errorf("hash = %s, want %s", hash, expectedHash)
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		hasher := NewSHA256Hasher(0)
		_, err := hasher.HashFile("/nonexistent/path/file.txt")
		if err == nil {
			t.Error("HashFile = nil, want error for non-existent file")
		}
	})

	t.Run("file exceeding maxFileSize returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "large.txt")
		// Create a 100 byte file
		if err := os.WriteFile(path, make([]byte, 100), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		hasher := NewSHA256Hasher(50) // 50 byte limit
		_, err := hasher.HashFile(path)
		if err == nil {
			t.Error("HashFile = nil, want ErrFileTooLarge")
		}
		if !errors.Is(err, ErrFileTooLarge) {
			t.Errorf("error = %v, want ErrFileTooLarge", err)
		}
	})

	t.Run("empty file produces known hash", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		hasher := NewSHA256Hasher(0)
		hash, err := hasher.HashFile(path)
		if err != nil {
			t.Fatalf("HashFile: %v", err)
		}

		// SHA256 of empty string
		expectedHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		if hash != expectedHash {
			t.Errorf("hash = %s, want %s", hash, expectedHash)
		}
	})
}

func TestSHA256Hasher_HashFileAtomic(t *testing.T) {
	t.Run("stable file returns valid entry", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "stable.txt")
		content := []byte("stable content")
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		hasher := NewSHA256Hasher(0)
		entry, err := hasher.HashFileAtomic(path, 3)
		if err != nil {
			t.Fatalf("HashFileAtomic: %v", err)
		}

		// Verify entry
		if entry.Path != path {
			t.Errorf("Path = %s, want %s", entry.Path, path)
		}
		if len(entry.Hash) != 64 {
			t.Errorf("len(Hash) = %d, want 64", len(entry.Hash))
		}
		if entry.Size != int64(len(content)) {
			t.Errorf("Size = %d, want %d", entry.Size, len(content))
		}
		if entry.Mtime == 0 {
			t.Error("Mtime = 0, want non-zero")
		}

		// Validate entry
		entry.Path = "stable.txt" // Relative path for validation
		if err := entry.Validate(); err != nil {
			t.Errorf("Validate: %v", err)
		}
	})

	t.Run("file exceeding limit returns ErrFileTooLarge", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "large.txt")
		if err := os.WriteFile(path, make([]byte, 200), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		hasher := NewSHA256Hasher(100)
		_, err := hasher.HashFileAtomic(path, 3)
		if err == nil {
			t.Error("HashFileAtomic = nil, want ErrFileTooLarge")
		}
		if !errors.Is(err, ErrFileTooLarge) {
			t.Errorf("error = %v, want ErrFileTooLarge", err)
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		hasher := NewSHA256Hasher(0)
		_, err := hasher.HashFileAtomic("/nonexistent/file.txt", 3)
		if err == nil {
			t.Error("HashFileAtomic = nil, want error")
		}
	})
}

func TestNewSHA256Hasher(t *testing.T) {
	t.Run("negative maxFileSize uses default", func(t *testing.T) {
		hasher := NewSHA256Hasher(-1)
		if hasher.maxFileSize != DefaultMaxFileSize {
			t.Errorf("maxFileSize = %d, want %d", hasher.maxFileSize, DefaultMaxFileSize)
		}
	})

	t.Run("zero maxFileSize means no limit", func(t *testing.T) {
		hasher := NewSHA256Hasher(0)
		if hasher.maxFileSize != 0 {
			t.Errorf("maxFileSize = %d, want 0", hasher.maxFileSize)
		}
	})

	t.Run("positive maxFileSize is used", func(t *testing.T) {
		hasher := NewSHA256Hasher(1024)
		if hasher.maxFileSize != 1024 {
			t.Errorf("maxFileSize = %d, want 1024", hasher.maxFileSize)
		}
	})
}
