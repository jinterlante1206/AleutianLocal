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
	"testing"
)

func TestFileEntry_Validate(t *testing.T) {
	t.Run("valid entry passes", func(t *testing.T) {
		entry := FileEntry{
			Path:  "src/main.go",
			Hash:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Mtime: 1234567890,
			Size:  100,
		}
		if err := entry.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("empty path fails", func(t *testing.T) {
		entry := FileEntry{
			Path: "",
			Hash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}
		err := entry.Validate()
		if err == nil {
			t.Error("Validate() = nil, want error for empty path")
		}
	})

	t.Run("short hash fails", func(t *testing.T) {
		entry := FileEntry{
			Path: "src/main.go",
			Hash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8", // 63 chars
		}
		err := entry.Validate()
		if err == nil {
			t.Error("Validate() = nil, want error for short hash")
		}
		if !errors.Is(err, ErrInvalidHash) {
			t.Errorf("error = %v, want ErrInvalidHash", err)
		}
	})

	t.Run("uppercase hash fails", func(t *testing.T) {
		entry := FileEntry{
			Path: "src/main.go",
			Hash: "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
		}
		err := entry.Validate()
		if err == nil {
			t.Error("Validate() = nil, want error for uppercase hash")
		}
		if !errors.Is(err, ErrInvalidHash) {
			t.Errorf("error = %v, want ErrInvalidHash", err)
		}
	})

	t.Run("invalid character in hash fails", func(t *testing.T) {
		entry := FileEntry{
			Path: "src/main.go",
			Hash: "g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // 'g' is invalid
		}
		err := entry.Validate()
		if err == nil {
			t.Error("Validate() = nil, want error for invalid hash character")
		}
		if !errors.Is(err, ErrInvalidHash) {
			t.Errorf("error = %v, want ErrInvalidHash", err)
		}
	})
}

func TestManifest_Methods(t *testing.T) {
	t.Run("NewManifest creates empty manifest", func(t *testing.T) {
		m := NewManifest("/test/project")
		if m.ProjectRoot != "/test/project" {
			t.Errorf("ProjectRoot = %s, want /test/project", m.ProjectRoot)
		}
		if m.Files == nil {
			t.Error("Files is nil, want empty map")
		}
		if len(m.Files) != 0 {
			t.Errorf("len(Files) = %d, want 0", len(m.Files))
		}
		if m.CreatedAtMilli == 0 {
			t.Error("CreatedAtMilli = 0, want non-zero")
		}
	})

	t.Run("FileCount returns correct count", func(t *testing.T) {
		m := NewManifest("/test")
		m.Files["a.go"] = FileEntry{Path: "a.go"}
		m.Files["b.go"] = FileEntry{Path: "b.go"}
		if m.FileCount() != 2 {
			t.Errorf("FileCount() = %d, want 2", m.FileCount())
		}
	})

	t.Run("ErrorCount returns correct count", func(t *testing.T) {
		m := NewManifest("/test")
		m.Errors = append(m.Errors, ScanError{Path: "c.go"})
		if m.ErrorCount() != 1 {
			t.Errorf("ErrorCount() = %d, want 1", m.ErrorCount())
		}
	})

	t.Run("HasErrors returns correct value", func(t *testing.T) {
		m := NewManifest("/test")
		if m.HasErrors() {
			t.Error("HasErrors() = true, want false")
		}
		m.Errors = append(m.Errors, ScanError{Path: "x.go"})
		if !m.HasErrors() {
			t.Error("HasErrors() = false, want true")
		}
	})
}

func TestChanges_Methods(t *testing.T) {
	t.Run("HasChanges with no changes", func(t *testing.T) {
		c := &Changes{}
		if c.HasChanges() {
			t.Error("HasChanges() = true, want false")
		}
	})

	t.Run("HasChanges with added files", func(t *testing.T) {
		c := &Changes{Added: []string{"a.go"}}
		if !c.HasChanges() {
			t.Error("HasChanges() = false, want true")
		}
	})

	t.Run("HasChanges with modified files", func(t *testing.T) {
		c := &Changes{Modified: []string{"a.go"}}
		if !c.HasChanges() {
			t.Error("HasChanges() = false, want true")
		}
	})

	t.Run("HasChanges with deleted files", func(t *testing.T) {
		c := &Changes{Deleted: []string{"a.go"}}
		if !c.HasChanges() {
			t.Error("HasChanges() = false, want true")
		}
	})

	t.Run("Count returns total changes", func(t *testing.T) {
		c := &Changes{
			Added:    []string{"a.go", "b.go"},
			Modified: []string{"c.go"},
			Deleted:  []string{"d.go", "e.go", "f.go"},
		}
		if c.Count() != 6 {
			t.Errorf("Count() = %d, want 6", c.Count())
		}
	})

	t.Run("IsEmpty is inverse of HasChanges", func(t *testing.T) {
		c := &Changes{}
		if !c.IsEmpty() {
			t.Error("IsEmpty() = false, want true")
		}
		c.Added = []string{"a.go"}
		if c.IsEmpty() {
			t.Error("IsEmpty() = true, want false")
		}
	})
}
