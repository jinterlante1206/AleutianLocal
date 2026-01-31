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
	"path/filepath"
	"sync"
)

// =============================================================================
// LANGUAGE CONFIGURATION
// =============================================================================

// LanguageConfig defines test execution settings for a language.
type LanguageConfig struct {
	// Language is the language identifier (e.g., "go", "python").
	Language string

	// TestCommand is the test runner command (e.g., "go", "pytest").
	TestCommand string

	// TestArgs are arguments for running a single test.
	// Use {name} as placeholder for test name, {file} for file path.
	TestArgs []string

	// SuiteArgs are arguments for running the full test suite.
	// Use {package} as placeholder for package/directory path.
	SuiteArgs []string

	// TestFilePattern is the glob pattern for test files.
	TestFilePattern string

	// TestNameFlag is the flag for specifying test name (e.g., "-run").
	TestNameFlag string

	// Extensions are file extensions for this language.
	Extensions []string
}

// =============================================================================
// LANGUAGE CONFIG REGISTRY
// =============================================================================

// LanguageConfigRegistry manages test configurations for different languages.
//
// Thread Safety: Safe for concurrent reads after initialization.
// Register operations should only be done during setup.
type LanguageConfigRegistry struct {
	mu      sync.RWMutex
	configs map[string]*LanguageConfig
}

// NewLanguageConfigRegistry creates a registry with default configurations.
//
// Outputs:
//
//	*LanguageConfigRegistry - Registry with Go, Python, TypeScript configs
func NewLanguageConfigRegistry() *LanguageConfigRegistry {
	r := &LanguageConfigRegistry{
		configs: make(map[string]*LanguageConfig),
	}
	r.registerDefaults()
	return r
}

// registerDefaults adds the default language configurations.
func (r *LanguageConfigRegistry) registerDefaults() {
	// Go configuration
	r.configs["go"] = &LanguageConfig{
		Language:        "go",
		TestCommand:     "go",
		TestArgs:        []string{"test", "-v", "-run", "{name}", "{package}"},
		SuiteArgs:       []string{"test", "-v", "{package}/..."},
		TestFilePattern: "*_test.go",
		TestNameFlag:    "-run",
		Extensions:      []string{".go"},
	}

	// Python configuration
	r.configs["python"] = &LanguageConfig{
		Language:        "python",
		TestCommand:     "pytest",
		TestArgs:        []string{"-v", "-k", "{name}", "{file}"},
		SuiteArgs:       []string{"-v", "{package}"},
		TestFilePattern: "test_*.py",
		TestNameFlag:    "-k",
		Extensions:      []string{".py"},
	}

	// TypeScript configuration
	r.configs["typescript"] = &LanguageConfig{
		Language:        "typescript",
		TestCommand:     "npx",
		TestArgs:        []string{"jest", "--testNamePattern", "{name}", "{file}"},
		SuiteArgs:       []string{"jest", "{package}"},
		TestFilePattern: "*.test.ts",
		TestNameFlag:    "--testNamePattern",
		Extensions:      []string{".ts", ".tsx"},
	}

	// JavaScript configuration (same as TypeScript but different extensions)
	r.configs["javascript"] = &LanguageConfig{
		Language:        "javascript",
		TestCommand:     "npx",
		TestArgs:        []string{"jest", "--testNamePattern", "{name}", "{file}"},
		SuiteArgs:       []string{"jest", "{package}"},
		TestFilePattern: "*.test.js",
		TestNameFlag:    "--testNamePattern",
		Extensions:      []string{".js", ".jsx", ".mjs"},
	}
}

// Get returns the configuration for a language.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	*LanguageConfig - The configuration, or nil if not found
//	bool - True if configuration exists
//
// Thread Safety: Safe for concurrent use.
func (r *LanguageConfigRegistry) Get(language string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[language]
	return cfg, ok
}

// Register adds or updates a language configuration.
//
// Inputs:
//
//	cfg - The configuration to register
//
// Thread Safety: Safe for concurrent use, but should only be called during setup.
func (r *LanguageConfigRegistry) Register(cfg *LanguageConfig) {
	if cfg == nil || cfg.Language == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[cfg.Language] = cfg
}

// Languages returns all registered language names.
//
// Outputs:
//
//	[]string - Slice of language names
//
// Thread Safety: Safe for concurrent use.
func (r *LanguageConfigRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	langs := make([]string, 0, len(r.configs))
	for lang := range r.configs {
		langs = append(langs, lang)
	}
	return langs
}

// GetByExtension returns the configuration for a file extension.
//
// Inputs:
//
//	ext - The file extension including dot (e.g., ".go")
//
// Outputs:
//
//	*LanguageConfig - The configuration, or nil if not found
//	bool - True if configuration exists
//
// Thread Safety: Safe for concurrent use.
func (r *LanguageConfigRegistry) GetByExtension(ext string) (*LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cfg := range r.configs {
		for _, e := range cfg.Extensions {
			if e == ext {
				return cfg, true
			}
		}
	}
	return nil, false
}

// LanguageForFile returns the language for a file path.
//
// Inputs:
//
//	filePath - The file path
//
// Outputs:
//
//	string - The language identifier, or empty if unknown
func (r *LanguageConfigRegistry) LanguageForFile(filePath string) string {
	ext := filepath.Ext(filePath)
	if cfg, ok := r.GetByExtension(ext); ok {
		return cfg.Language
	}
	return ""
}

// =============================================================================
// DEFAULT REGISTRY
// =============================================================================

// DefaultLanguageConfigs is the shared language config registry.
var DefaultLanguageConfigs = NewLanguageConfigRegistry()

// GetLanguageConfig is a convenience function using the default registry.
func GetLanguageConfig(language string) (*LanguageConfig, bool) {
	return DefaultLanguageConfigs.Get(language)
}

// LanguageForFile is a convenience function using the default registry.
func LanguageForFile(filePath string) string {
	return DefaultLanguageConfigs.LanguageForFile(filePath)
}
