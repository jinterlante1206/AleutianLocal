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
	"testing"
)

// =============================================================================
// LANGUAGE CONFIG TESTS
// =============================================================================

func TestLanguageConfig(t *testing.T) {
	cfg := &LanguageConfig{
		Language:        "go",
		TestCommand:     "go",
		TestArgs:        []string{"test", "-v", "-run", "{name}"},
		SuiteArgs:       []string{"test", "./..."},
		TestFilePattern: "*_test.go",
		TestNameFlag:    "-run",
		Extensions:      []string{".go"},
	}

	if cfg.Language != "go" {
		t.Errorf("Language = %q, want go", cfg.Language)
	}
	if cfg.TestCommand != "go" {
		t.Errorf("TestCommand = %q, want go", cfg.TestCommand)
	}
	if len(cfg.TestArgs) != 4 {
		t.Errorf("TestArgs length = %d, want 4", len(cfg.TestArgs))
	}
}

// =============================================================================
// LANGUAGE CONFIG REGISTRY TESTS
// =============================================================================

func TestNewLanguageConfigRegistry(t *testing.T) {
	r := NewLanguageConfigRegistry()

	// Should have default languages
	langs := r.Languages()
	if len(langs) < 4 {
		t.Errorf("expected at least 4 languages, got %d", len(langs))
	}

	// Check each default language
	expectedLangs := []string{"go", "python", "typescript", "javascript"}
	for _, expected := range expectedLangs {
		cfg, ok := r.Get(expected)
		if !ok {
			t.Errorf("expected %q to be registered", expected)
			continue
		}
		if cfg.Language != expected {
			t.Errorf("cfg.Language = %q, want %q", cfg.Language, expected)
		}
		if cfg.TestCommand == "" {
			t.Errorf("%s: TestCommand should not be empty", expected)
		}
		if len(cfg.TestArgs) == 0 {
			t.Errorf("%s: TestArgs should not be empty", expected)
		}
	}
}

func TestLanguageConfigRegistry_Get(t *testing.T) {
	r := NewLanguageConfigRegistry()

	t.Run("existing language", func(t *testing.T) {
		cfg, ok := r.Get("go")
		if !ok {
			t.Fatal("expected go config to exist")
		}
		if cfg.Language != "go" {
			t.Errorf("Language = %q, want go", cfg.Language)
		}
		if cfg.TestCommand != "go" {
			t.Errorf("TestCommand = %q, want go", cfg.TestCommand)
		}
	})

	t.Run("non-existing language", func(t *testing.T) {
		_, ok := r.Get("rust")
		if ok {
			t.Error("expected rust to not exist")
		}
	})
}

func TestLanguageConfigRegistry_Register(t *testing.T) {
	r := NewLanguageConfigRegistry()

	t.Run("register new language", func(t *testing.T) {
		cfg := &LanguageConfig{
			Language:    "rust",
			TestCommand: "cargo",
			TestArgs:    []string{"test", "--", "{name}"},
			SuiteArgs:   []string{"test"},
		}

		r.Register(cfg)

		retrieved, ok := r.Get("rust")
		if !ok {
			t.Fatal("expected rust to be registered")
		}
		if retrieved.TestCommand != "cargo" {
			t.Errorf("TestCommand = %q, want cargo", retrieved.TestCommand)
		}
	})

	t.Run("register nil config does nothing", func(t *testing.T) {
		before := len(r.Languages())
		r.Register(nil)
		after := len(r.Languages())

		if after != before {
			t.Error("registering nil should not change registry")
		}
	})

	t.Run("register empty language does nothing", func(t *testing.T) {
		before := len(r.Languages())
		r.Register(&LanguageConfig{Language: ""})
		after := len(r.Languages())

		if after != before {
			t.Error("registering empty language should not change registry")
		}
	})

	t.Run("overwrite existing language", func(t *testing.T) {
		cfg := &LanguageConfig{
			Language:    "go",
			TestCommand: "custom-go",
			TestArgs:    []string{"custom-test"},
		}

		r.Register(cfg)

		retrieved, _ := r.Get("go")
		if retrieved.TestCommand != "custom-go" {
			t.Errorf("TestCommand = %q, want custom-go", retrieved.TestCommand)
		}
	})
}

func TestLanguageConfigRegistry_Languages(t *testing.T) {
	r := NewLanguageConfigRegistry()

	langs := r.Languages()

	// Check that it's a new slice each time (not the internal map keys)
	langs2 := r.Languages()
	if &langs[0] == &langs2[0] {
		t.Error("Languages() should return new slice each call")
	}

	// Check expected languages are present
	langMap := make(map[string]bool)
	for _, l := range langs {
		langMap[l] = true
	}

	expected := []string{"go", "python", "typescript", "javascript"}
	for _, e := range expected {
		if !langMap[e] {
			t.Errorf("expected %q in languages list", e)
		}
	}
}

func TestLanguageConfigRegistry_GetByExtension(t *testing.T) {
	r := NewLanguageConfigRegistry()

	tests := []struct {
		ext      string
		wantLang string
		wantOk   bool
	}{
		{".go", "go", true},
		{".py", "python", true},
		{".ts", "typescript", true},
		{".tsx", "typescript", true},
		{".js", "javascript", true},
		{".jsx", "javascript", true},
		{".mjs", "javascript", true},
		{".rs", "", false},
		{".txt", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			cfg, ok := r.GetByExtension(tt.ext)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && cfg.Language != tt.wantLang {
				t.Errorf("Language = %q, want %q", cfg.Language, tt.wantLang)
			}
		})
	}
}

func TestLanguageConfigRegistry_LanguageForFile(t *testing.T) {
	r := NewLanguageConfigRegistry()

	tests := []struct {
		filePath string
		want     string
	}{
		{"main.go", "go"},
		{"path/to/file.go", "go"},
		{"test_example.py", "python"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"script.js", "javascript"},
		{"module.jsx", "javascript"},
		{"esm.mjs", "javascript"},
		{"README.md", ""},
		{"config.yaml", ""},
		{"noextension", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got := r.LanguageForFile(tt.filePath)
			if got != tt.want {
				t.Errorf("LanguageForFile(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

// =============================================================================
// DEFAULT REGISTRY TESTS
// =============================================================================

func TestDefaultLanguageConfigs(t *testing.T) {
	// Verify it's initialized
	if DefaultLanguageConfigs == nil {
		t.Fatal("DefaultLanguageConfigs should not be nil")
	}

	// Verify it has default configs
	cfg, ok := DefaultLanguageConfigs.Get("go")
	if !ok {
		t.Error("DefaultLanguageConfigs should have go config")
	}
	if cfg.TestCommand != "go" {
		t.Errorf("go TestCommand = %q, want go", cfg.TestCommand)
	}
}

func TestGetLanguageConfig(t *testing.T) {
	// This is a convenience function for DefaultLanguageConfigs.Get
	cfg, ok := GetLanguageConfig("go")
	if !ok {
		t.Fatal("expected go config")
	}
	if cfg.Language != "go" {
		t.Errorf("Language = %q, want go", cfg.Language)
	}
}

// =============================================================================
// GO LANGUAGE CONFIG DETAILS
// =============================================================================

func TestGoLanguageConfig(t *testing.T) {
	cfg, ok := GetLanguageConfig("go")
	if !ok {
		t.Fatal("expected go config")
	}

	if cfg.TestCommand != "go" {
		t.Errorf("TestCommand = %q, want go", cfg.TestCommand)
	}

	// Check test args contain placeholders
	found := false
	for _, arg := range cfg.TestArgs {
		if arg == "{name}" || arg == "{package}" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TestArgs should contain {name} or {package} placeholder")
	}

	// Check extensions
	if len(cfg.Extensions) == 0 {
		t.Error("Extensions should not be empty")
	}
	if cfg.Extensions[0] != ".go" {
		t.Errorf("Extensions[0] = %q, want .go", cfg.Extensions[0])
	}
}

// =============================================================================
// PYTHON LANGUAGE CONFIG DETAILS
// =============================================================================

func TestPythonLanguageConfig(t *testing.T) {
	cfg, ok := GetLanguageConfig("python")
	if !ok {
		t.Fatal("expected python config")
	}

	if cfg.TestCommand != "pytest" {
		t.Errorf("TestCommand = %q, want pytest", cfg.TestCommand)
	}

	if cfg.TestNameFlag != "-k" {
		t.Errorf("TestNameFlag = %q, want -k", cfg.TestNameFlag)
	}
}

// =============================================================================
// TYPESCRIPT/JAVASCRIPT CONFIG DETAILS
// =============================================================================

func TestTypeScriptLanguageConfig(t *testing.T) {
	cfg, ok := GetLanguageConfig("typescript")
	if !ok {
		t.Fatal("expected typescript config")
	}

	if cfg.TestCommand != "npx" {
		t.Errorf("TestCommand = %q, want npx", cfg.TestCommand)
	}

	// Should have multiple extensions
	if len(cfg.Extensions) < 2 {
		t.Errorf("expected at least 2 extensions, got %d", len(cfg.Extensions))
	}
}

func TestJavaScriptLanguageConfig(t *testing.T) {
	cfg, ok := GetLanguageConfig("javascript")
	if !ok {
		t.Fatal("expected javascript config")
	}

	// JavaScript should have same test command as TypeScript (Jest via npx)
	if cfg.TestCommand != "npx" {
		t.Errorf("TestCommand = %q, want npx", cfg.TestCommand)
	}

	// Check for jsx and mjs extensions
	extMap := make(map[string]bool)
	for _, ext := range cfg.Extensions {
		extMap[ext] = true
	}

	if !extMap[".js"] {
		t.Error("expected .js extension")
	}
	if !extMap[".jsx"] {
		t.Error("expected .jsx extension")
	}
	if !extMap[".mjs"] {
		t.Error("expected .mjs extension")
	}
}
