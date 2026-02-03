// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lsp

import (
	"sync"
	"testing"
)

func TestNewConfigRegistry(t *testing.T) {
	r := NewConfigRegistry()

	// Should have default languages registered
	langs := r.Languages()
	if len(langs) == 0 {
		t.Error("expected default languages to be registered")
	}

	// Check Go is registered
	config, ok := r.Get("go")
	if !ok {
		t.Error("Go should be registered by default")
	}
	if config.Command != "gopls" {
		t.Errorf("Go command = %q, want gopls", config.Command)
	}
}

func TestConfigRegistry_Register(t *testing.T) {
	r := NewConfigRegistry()

	config := LanguageConfig{
		Language:   "custom",
		Command:    "custom-lsp",
		Args:       []string{"--stdio"},
		Extensions: []string{".custom", ".cust"},
		RootFiles:  []string{"custom.config"},
	}

	r.Register(config)

	// Check it was registered
	got, ok := r.Get("custom")
	if !ok {
		t.Error("custom language should be registered")
	}
	if got.Command != "custom-lsp" {
		t.Errorf("Command = %q, want custom-lsp", got.Command)
	}
	if len(got.Extensions) != 2 {
		t.Errorf("Extensions count = %d, want 2", len(got.Extensions))
	}
}

func TestConfigRegistry_Get(t *testing.T) {
	r := NewConfigRegistry()

	t.Run("existing language", func(t *testing.T) {
		config, ok := r.Get("go")
		if !ok {
			t.Error("Go should exist")
		}
		if config.Language != "go" {
			t.Errorf("Language = %q, want go", config.Language)
		}
	})

	t.Run("nonexistent language", func(t *testing.T) {
		_, ok := r.Get("nonexistent")
		if ok {
			t.Error("should return false for nonexistent language")
		}
	})
}

func TestConfigRegistry_GetByExtension(t *testing.T) {
	r := NewConfigRegistry()

	t.Run("Go extension", func(t *testing.T) {
		config, ok := r.GetByExtension(".go")
		if !ok {
			t.Error(".go should be mapped")
		}
		if config.Language != "go" {
			t.Errorf("Language = %q, want go", config.Language)
		}
	})

	t.Run("Python extension", func(t *testing.T) {
		config, ok := r.GetByExtension(".py")
		if !ok {
			t.Error(".py should be mapped")
		}
		if config.Language != "python" {
			t.Errorf("Language = %q, want python", config.Language)
		}
	})

	t.Run("TypeScript extension", func(t *testing.T) {
		config, ok := r.GetByExtension(".ts")
		if !ok {
			t.Error(".ts should be mapped")
		}
		if config.Language != "typescript" {
			t.Errorf("Language = %q, want typescript", config.Language)
		}
	})

	t.Run("unknown extension", func(t *testing.T) {
		_, ok := r.GetByExtension(".unknown")
		if ok {
			t.Error("should return false for unknown extension")
		}
	})
}

func TestConfigRegistry_Languages(t *testing.T) {
	r := NewConfigRegistry()

	langs := r.Languages()

	// Should contain at least the defaults
	expected := map[string]bool{
		"go":         false,
		"python":     false,
		"typescript": false,
		"javascript": false,
	}

	for _, lang := range langs {
		if _, ok := expected[lang]; ok {
			expected[lang] = true
		}
	}

	for lang, found := range expected {
		if !found {
			t.Errorf("expected %s to be in Languages()", lang)
		}
	}
}

func TestConfigRegistry_Extensions(t *testing.T) {
	r := NewConfigRegistry()

	exts := r.Extensions()

	if len(exts) == 0 {
		t.Error("expected extensions to be registered")
	}

	// Check some expected extensions
	extMap := make(map[string]bool)
	for _, ext := range exts {
		extMap[ext] = true
	}

	expected := []string{".go", ".py", ".ts", ".js"}
	for _, ext := range expected {
		if !extMap[ext] {
			t.Errorf("expected %s in Extensions()", ext)
		}
	}
}

func TestConfigRegistry_LanguageForExtension(t *testing.T) {
	r := NewConfigRegistry()

	t.Run("known extension", func(t *testing.T) {
		lang, ok := r.LanguageForExtension(".go")
		if !ok {
			t.Error("should find .go")
		}
		if lang != "go" {
			t.Errorf("Language = %q, want go", lang)
		}
	})

	t.Run("unknown extension", func(t *testing.T) {
		_, ok := r.LanguageForExtension(".xyz")
		if ok {
			t.Error("should not find .xyz")
		}
	})
}

func TestConfigRegistry_RegisterOverwrite(t *testing.T) {
	r := NewConfigRegistry()

	// Overwrite Go config
	r.Register(LanguageConfig{
		Language:   "go",
		Command:    "custom-gopls",
		Args:       []string{"custom"},
		Extensions: []string{".go"},
	})

	config, ok := r.Get("go")
	if !ok {
		t.Error("Go should still exist")
	}
	if config.Command != "custom-gopls" {
		t.Errorf("Command = %q, want custom-gopls", config.Command)
	}
}

func TestConfigRegistry_Concurrent(t *testing.T) {
	r := NewConfigRegistry()

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Get("go")
			_, _ = r.GetByExtension(".go")
			_ = r.Languages()
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.Register(LanguageConfig{
				Language:   "test",
				Command:    "test-lsp",
				Extensions: []string{".test"},
			})
		}(i)
	}

	wg.Wait()
}

func TestConfigRegistry_DefaultLanguages(t *testing.T) {
	r := NewConfigRegistry()

	// Test all default languages have expected configurations
	tests := []struct {
		language   string
		command    string
		extensions []string
	}{
		{"go", "gopls", []string{".go"}},
		{"python", "pyright-langserver", []string{".py", ".pyi"}},
		{"typescript", "typescript-language-server", []string{".ts", ".tsx"}},
		{"javascript", "typescript-language-server", []string{".js", ".jsx", ".mjs", ".cjs"}},
		{"rust", "rust-analyzer", []string{".rs"}},
	}

	for _, tc := range tests {
		t.Run(tc.language, func(t *testing.T) {
			config, ok := r.Get(tc.language)
			if !ok {
				t.Errorf("%s should be registered", tc.language)
				return
			}
			if config.Command != tc.command {
				t.Errorf("Command = %q, want %q", config.Command, tc.command)
			}
			for _, ext := range tc.extensions {
				byExt, ok := r.GetByExtension(ext)
				if !ok {
					t.Errorf("extension %s should be registered", ext)
					continue
				}
				if byExt.Language != tc.language {
					t.Errorf("extension %s maps to %s, want %s", ext, byExt.Language, tc.language)
				}
			}
		})
	}
}
