// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// DEFAULT LINTER CONFIGS
// =============================================================================

// DefaultGoConfig is the configuration for golangci-lint.
//
// Description:
//
//	golangci-lint is the standard Go linter aggregator.
//	It runs multiple linters in parallel and provides unified JSON output.
var DefaultGoConfig = LinterConfig{
	Language: "go",
	Command:  "golangci-lint",
	Args: []string{
		"run",
		"--out-format=json",
		"--issues-exit-code=0", // Don't fail on lint issues
		"--timeout=30s",
	},
	Extensions: []string{".go"},
	Timeout:    30 * time.Second,
	FixArgs: []string{
		"run",
		"--fix",
		"--out-format=json",
		"--issues-exit-code=0",
		"--timeout=30s",
	},
}

// DefaultPythonConfig is the configuration for Ruff.
//
// Description:
//
//	Ruff is an extremely fast Python linter written in Rust.
//	It provides pyflakes, pycodestyle, and more in one tool.
var DefaultPythonConfig = LinterConfig{
	Language: "python",
	Command:  "ruff",
	Args: []string{
		"check",
		"--output-format=json",
		"--exit-zero", // Don't fail on lint issues
	},
	Extensions:    []string{".py", ".pyi"},
	Timeout:       10 * time.Second,
	SupportsStdin: true,
	FixArgs: []string{
		"check",
		"--fix",
		"--output-format=json",
		"--exit-zero",
	},
}

// DefaultTSConfig is the configuration for ESLint (TypeScript/JavaScript).
//
// Description:
//
//	ESLint is the standard linter for JavaScript and TypeScript.
//	Note: ESLint requires a project config (.eslintrc) to work properly.
var DefaultTSConfig = LinterConfig{
	Language: "typescript",
	Command:  "eslint",
	Args: []string{
		"--format=json",
		"--no-error-on-unmatched-pattern",
	},
	Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
	Timeout:    30 * time.Second,
	FixArgs: []string{
		"--fix",
		"--format=json",
		"--no-error-on-unmatched-pattern",
	},
}

// DefaultJSConfig is an alias for TypeScript config (same linter).
var DefaultJSConfig = LinterConfig{
	Language: "javascript",
	Command:  "eslint",
	Args: []string{
		"--format=json",
		"--no-error-on-unmatched-pattern",
	},
	Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
	Timeout:    30 * time.Second,
	FixArgs: []string{
		"--fix",
		"--format=json",
		"--no-error-on-unmatched-pattern",
	},
}

// =============================================================================
// CONFIG REGISTRY
// =============================================================================

// ConfigRegistry manages linter configurations for different languages.
//
// Thread Safety: Safe for concurrent use after initialization.
type ConfigRegistry struct {
	mu      sync.RWMutex
	configs map[string]*LinterConfig

	// extensionMap maps file extensions to languages for quick lookup.
	extensionMap map[string]string
}

// NewConfigRegistry creates a new registry with default configurations.
func NewConfigRegistry() *ConfigRegistry {
	r := &ConfigRegistry{
		configs:      make(map[string]*LinterConfig),
		extensionMap: make(map[string]string),
	}
	r.registerDefaults()
	return r
}

// registerDefaults adds the default linter configurations.
func (r *ConfigRegistry) registerDefaults() {
	r.Register(&DefaultGoConfig)
	r.Register(&DefaultPythonConfig)
	r.Register(&DefaultTSConfig)
	r.Register(&DefaultJSConfig)
}

// Register adds or updates a linter configuration.
//
// Description:
//
//	Registers a linter configuration for a language.
//	Also updates the extension map for language detection.
//
// Inputs:
//
//	config - The linter configuration to register
//
// Thread Safety: Safe for concurrent use.
func (r *ConfigRegistry) Register(config *LinterConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.configs[config.Language] = config.Clone()

	// Update extension map
	for _, ext := range config.Extensions {
		r.extensionMap[ext] = config.Language
	}
}

// Get returns the configuration for a language.
//
// Description:
//
//	Returns a clone of the configuration for thread safety.
//	Returns nil if no configuration exists for the language.
//
// Inputs:
//
//	language - The language identifier (e.g., "go", "python")
//
// Outputs:
//
//	*LinterConfig - The configuration, or nil if not found
//
// Thread Safety: Safe for concurrent use.
func (r *ConfigRegistry) Get(language string) *LinterConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[language]
	if !ok {
		return nil
	}
	return config.Clone()
}

// GetByExtension returns the configuration for a file extension.
//
// Description:
//
//	Looks up the language from the extension and returns the config.
//	Returns nil if no linter handles the extension.
//
// Inputs:
//
//	ext - File extension including dot (e.g., ".go", ".py")
//
// Outputs:
//
//	*LinterConfig - The configuration, or nil if not found
//
// Thread Safety: Safe for concurrent use.
func (r *ConfigRegistry) GetByExtension(ext string) *LinterConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lang, ok := r.extensionMap[ext]
	if !ok {
		return nil
	}

	config, ok := r.configs[lang]
	if !ok {
		return nil
	}
	return config.Clone()
}

// Languages returns all registered language names.
//
// Thread Safety: Safe for concurrent use.
func (r *ConfigRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	langs := make([]string, 0, len(r.configs))
	for lang := range r.configs {
		langs = append(langs, lang)
	}
	return langs
}

// SetAvailable marks a linter as available or unavailable.
//
// Description:
//
//	Updates the Available flag for a linter configuration.
//	Called by the runner after detecting installed linters.
//
// Inputs:
//
//	language - The language identifier
//	available - Whether the linter is available
//
// Thread Safety: Safe for concurrent use.
func (r *ConfigRegistry) SetAvailable(language string, available bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if config, ok := r.configs[language]; ok {
		config.Available = available
	}
}

// =============================================================================
// LANGUAGE DETECTION
// =============================================================================

// LanguageFromPath detects the language from a file path.
//
// Description:
//
//	Determines the programming language based on file extension.
//	Returns empty string for unknown extensions.
//
// Inputs:
//
//	filePath - Path to the file (can be relative or absolute)
//
// Outputs:
//
//	string - Language identifier (e.g., "go", "python") or empty string
func LanguageFromPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	default:
		return ""
	}
}

// ExtensionForLanguage returns the primary file extension for a language.
//
// Description:
//
//	Returns the most common file extension for the language.
//	Used when creating temp files for content linting.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	string - File extension with dot (e.g., ".go") or empty string
func ExtensionForLanguage(language string) string {
	switch language {
	case "go":
		return ".go"
	case "python":
		return ".py"
	case "typescript":
		return ".ts"
	case "javascript":
		return ".js"
	default:
		return ""
	}
}
