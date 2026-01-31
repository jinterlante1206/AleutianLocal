// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"testing"
)

func TestGoHierarchy_Language(t *testing.T) {
	h := &GoHierarchy{}
	if h.Language() != "go" {
		t.Errorf("expected 'go', got %q", h.Language())
	}
}

func TestGoHierarchy_LevelCount(t *testing.T) {
	h := &GoHierarchy{}
	if h.LevelCount() != 4 {
		t.Errorf("expected 4 levels, got %d", h.LevelCount())
	}
}

func TestGoHierarchy_LevelName(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		level    int
		expected string
	}{
		{0, "project"},
		{1, "package"},
		{2, "file"},
		{3, "function"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		if got := h.LevelName(tt.level); got != tt.expected {
			t.Errorf("LevelName(%d) = %q, want %q", tt.level, got, tt.expected)
		}
	}
}

func TestGoHierarchy_EntityLevel(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		entityID string
		expected int
	}{
		{"", 0},                      // Project
		{"pkg/auth", 1},              // Package
		{"internal/db", 1},           // Package
		{"pkg/auth/validator.go", 2}, // File
		{"main.go", 2},               // File (root)
		{"pkg/auth/validator.go#ValidateToken", 3}, // Symbol
	}

	for _, tt := range tests {
		if got := h.EntityLevel(tt.entityID); got != tt.expected {
			t.Errorf("EntityLevel(%q) = %d, want %d", tt.entityID, got, tt.expected)
		}
	}
}

func TestGoHierarchy_ParentOf(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		entityID   string
		wantParent string
		wantErr    bool
	}{
		// Symbol → File
		{"pkg/auth/validator.go#ValidateToken", "pkg/auth/validator.go", false},
		// File → Package
		{"pkg/auth/validator.go", "pkg/auth", false},
		// Package → Parent package
		{"pkg/auth", "pkg", false},
		// Top-level package → Project
		{"pkg", "", false},
		// Project → Error
		{"", "", true},
	}

	for _, tt := range tests {
		parent, err := h.ParentOf(tt.entityID)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParentOf(%q) expected error, got nil", tt.entityID)
			}
		} else {
			if err != nil {
				t.Errorf("ParentOf(%q) unexpected error: %v", tt.entityID, err)
			}
			if parent != tt.wantParent {
				t.Errorf("ParentOf(%q) = %q, want %q", tt.entityID, parent, tt.wantParent)
			}
		}
	}
}

func TestGoHierarchy_ParseEntityID(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		entityID string
		wantPkg  string
		wantFile string
		wantSym  string
		wantLvl  int
	}{
		{"", "", "", "", 0},
		{"pkg/auth", "pkg/auth", "", "", 1},
		{"pkg/auth/validator.go", "pkg/auth", "validator.go", "", 2},
		{"pkg/auth/validator.go#ValidateToken", "pkg/auth", "validator.go", "ValidateToken", 3},
		{"main.go", "", "main.go", "", 2},
	}

	for _, tt := range tests {
		c := h.ParseEntityID(tt.entityID)
		if c.Package != tt.wantPkg {
			t.Errorf("ParseEntityID(%q).Package = %q, want %q", tt.entityID, c.Package, tt.wantPkg)
		}
		if c.File != tt.wantFile {
			t.Errorf("ParseEntityID(%q).File = %q, want %q", tt.entityID, c.File, tt.wantFile)
		}
		if c.Symbol != tt.wantSym {
			t.Errorf("ParseEntityID(%q).Symbol = %q, want %q", tt.entityID, c.Symbol, tt.wantSym)
		}
		if c.Level != tt.wantLvl {
			t.Errorf("ParseEntityID(%q).Level = %d, want %d", tt.entityID, c.Level, tt.wantLvl)
		}
	}
}

func TestGoHierarchy_BuildEntityID(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		components EntityComponents
		expected   string
	}{
		{EntityComponents{Level: 0}, ""},
		{EntityComponents{Package: "pkg/auth", Level: 1}, "pkg/auth"},
		{EntityComponents{Package: "pkg/auth", File: "validator.go", Level: 2}, "pkg/auth/validator.go"},
		{EntityComponents{Package: "pkg/auth", File: "validator.go", Symbol: "ValidateToken", Level: 3}, "pkg/auth/validator.go#ValidateToken"},
		{EntityComponents{File: "main.go", Level: 2}, "main.go"},
	}

	for _, tt := range tests {
		got := h.BuildEntityID(tt.components)
		if got != tt.expected {
			t.Errorf("BuildEntityID(%+v) = %q, want %q", tt.components, got, tt.expected)
		}
	}
}

func TestGoHierarchy_RootMarkers(t *testing.T) {
	h := &GoHierarchy{}
	markers := h.RootMarkers()

	if len(markers) == 0 {
		t.Error("expected root markers")
	}

	// Should include go.mod
	found := false
	for _, m := range markers {
		if m == "go.mod" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'go.mod' in root markers")
	}
}

func TestGoHierarchy_IsTestFile(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		path     string
		expected bool
	}{
		{"validator_test.go", true},
		{"validator.go", false},
		{"test_validator.go", false},
		{"pkg/auth/validator_test.go", true},
	}

	for _, tt := range tests {
		if got := h.IsTestFile(tt.path); got != tt.expected {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestGoHierarchy_IsInternalPackage(t *testing.T) {
	h := &GoHierarchy{}

	tests := []struct {
		path     string
		expected bool
	}{
		{"internal/db", true},
		{"pkg/internal/service", true},
		{"pkg/auth", false},
		{"internals", false},
	}

	for _, tt := range tests {
		if got := h.IsInternalPackage(tt.path); got != tt.expected {
			t.Errorf("IsInternalPackage(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestPythonHierarchy_Language(t *testing.T) {
	h := &PythonHierarchy{}
	if h.Language() != "python" {
		t.Errorf("expected 'python', got %q", h.Language())
	}
}

func TestPythonHierarchy_LevelName(t *testing.T) {
	h := &PythonHierarchy{}

	tests := []struct {
		level    int
		expected string
	}{
		{0, "project"},
		{1, "package"},
		{2, "module"},
		{3, "symbol"},
	}

	for _, tt := range tests {
		if got := h.LevelName(tt.level); got != tt.expected {
			t.Errorf("LevelName(%d) = %q, want %q", tt.level, got, tt.expected)
		}
	}
}

func TestPythonHierarchy_EntityLevel(t *testing.T) {
	h := &PythonHierarchy{}

	tests := []struct {
		entityID string
		expected int
	}{
		{"", 0},                                 // Project
		{"myapp/auth", 1},                       // Package
		{"myapp/auth/validator.py", 2},          // Module
		{"myapp/auth/validator.py#validate", 3}, // Symbol
		{"script.pyi", 2},                       // Type stub file
	}

	for _, tt := range tests {
		if got := h.EntityLevel(tt.entityID); got != tt.expected {
			t.Errorf("EntityLevel(%q) = %d, want %d", tt.entityID, got, tt.expected)
		}
	}
}

func TestPythonHierarchy_RootMarkers(t *testing.T) {
	h := &PythonHierarchy{}
	markers := h.RootMarkers()

	if len(markers) == 0 {
		t.Error("expected root markers")
	}

	// Should include pyproject.toml
	found := false
	for _, m := range markers {
		if m == "pyproject.toml" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'pyproject.toml' in root markers")
	}
}

func TestPythonHierarchy_IsTestFile(t *testing.T) {
	h := &PythonHierarchy{}

	tests := []struct {
		path     string
		expected bool
	}{
		{"test_validator.py", true},
		{"validator_test.py", true},
		{"conftest.py", true},
		{"validator.py", false},
	}

	for _, tt := range tests {
		if got := h.IsTestFile(tt.path); got != tt.expected {
			t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestPythonHierarchy_ModuleNameFromFile(t *testing.T) {
	h := &PythonHierarchy{}

	tests := []struct {
		path     string
		expected string
	}{
		{"myapp/auth/validator.py", "myapp.auth.validator"},
		{"myapp/__init__.py", "myapp"},
		{"script.py", "script"},
	}

	for _, tt := range tests {
		if got := h.ModuleNameFromFile(tt.path); got != tt.expected {
			t.Errorf("ModuleNameFromFile(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestHierarchyRegistry_Get(t *testing.T) {
	r := NewHierarchyRegistry()

	// Go should be registered by default
	h, ok := r.Get("go")
	if !ok {
		t.Error("expected 'go' to be registered")
	}
	if h.Language() != "go" {
		t.Errorf("expected Go hierarchy, got %q", h.Language())
	}

	// Python should be registered by default
	h, ok = r.Get("python")
	if !ok {
		t.Error("expected 'python' to be registered")
	}
	if h.Language() != "python" {
		t.Errorf("expected Python hierarchy, got %q", h.Language())
	}

	// Unknown language
	_, ok = r.Get("unknown")
	if ok {
		t.Error("expected 'unknown' to not be registered")
	}
}

func TestHierarchyRegistry_DetectLanguage(t *testing.T) {
	r := NewHierarchyRegistry()

	tests := []struct {
		filePath string
		expected string
	}{
		{"main.go", "go"},
		{"validator.py", "python"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"script.js", "javascript"},
		{"lib.rs", "rust"},
		{"unknown.xyz", ""},
	}

	for _, tt := range tests {
		if got := r.DetectLanguage(tt.filePath); got != tt.expected {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tt.filePath, got, tt.expected)
		}
	}
}

func TestHierarchyRegistry_Languages(t *testing.T) {
	r := NewHierarchyRegistry()
	langs := r.Languages()

	if len(langs) < 2 {
		t.Errorf("expected at least 2 languages, got %d", len(langs))
	}

	// Should include go and python
	hasGo, hasPy := false, false
	for _, l := range langs {
		if l == "go" {
			hasGo = true
		}
		if l == "python" {
			hasPy = true
		}
	}
	if !hasGo {
		t.Error("expected 'go' in languages")
	}
	if !hasPy {
		t.Error("expected 'python' in languages")
	}
}
