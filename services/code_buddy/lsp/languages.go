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

import "sync"

// LanguageConfig contains configuration for an LSP server.
type LanguageConfig struct {
	// Language is the language identifier (e.g., "go", "python").
	Language string

	// Command is the executable name or path.
	Command string

	// Args are command-line arguments to pass to the server.
	Args []string

	// Extensions are file extensions this server handles (e.g., ".go").
	Extensions []string

	// RootFiles are files that indicate a project root (e.g., "go.mod").
	RootFiles []string

	// InitializationOptions are custom options passed during initialize.
	InitializationOptions interface{}
}

// ConfigRegistry manages LSP configurations for different languages.
//
// Thread Safety: Safe for concurrent use.
type ConfigRegistry struct {
	mu         sync.RWMutex
	byLanguage map[string]LanguageConfig
	byExt      map[string]string // extension -> language
}

// NewConfigRegistry creates a registry with default configurations.
//
// Description:
//
//	Creates a new configuration registry pre-populated with configurations
//	for common languages: Go (gopls), Python (pyright), TypeScript, and JavaScript.
//
// Outputs:
//
//	*ConfigRegistry - The configured registry
func NewConfigRegistry() *ConfigRegistry {
	r := &ConfigRegistry{
		byLanguage: make(map[string]LanguageConfig),
		byExt:      make(map[string]string),
	}
	r.registerDefaults()
	return r
}

// registerDefaults adds default language server configurations.
func (r *ConfigRegistry) registerDefaults() {
	// Go - gopls
	r.Register(LanguageConfig{
		Language:   "go",
		Command:    "gopls",
		Args:       []string{"serve"},
		Extensions: []string{".go"},
		RootFiles:  []string{"go.mod", "go.sum"},
	})

	// Python - pyright
	r.Register(LanguageConfig{
		Language:   "python",
		Command:    "pyright-langserver",
		Args:       []string{"--stdio"},
		Extensions: []string{".py", ".pyi"},
		RootFiles:  []string{"pyproject.toml", "requirements.txt", "setup.py"},
	})

	// TypeScript
	r.Register(LanguageConfig{
		Language:   "typescript",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		Extensions: []string{".ts", ".tsx"},
		RootFiles:  []string{"tsconfig.json", "package.json"},
	})

	// JavaScript
	r.Register(LanguageConfig{
		Language:   "javascript",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		RootFiles:  []string{"package.json", "jsconfig.json"},
	})

	// Rust - rust-analyzer
	r.Register(LanguageConfig{
		Language:   "rust",
		Command:    "rust-analyzer",
		Args:       []string{},
		Extensions: []string{".rs"},
		RootFiles:  []string{"Cargo.toml"},
	})

	// Java - jdtls
	r.Register(LanguageConfig{
		Language:   "java",
		Command:    "jdtls",
		Args:       []string{},
		Extensions: []string{".java"},
		RootFiles:  []string{"pom.xml", "build.gradle", "build.gradle.kts"},
	})

	// C/C++ - clangd
	r.Register(LanguageConfig{
		Language:   "c",
		Command:    "clangd",
		Args:       []string{},
		Extensions: []string{".c", ".h"},
		RootFiles:  []string{"compile_commands.json", "CMakeLists.txt", "Makefile"},
	})

	r.Register(LanguageConfig{
		Language:   "cpp",
		Command:    "clangd",
		Args:       []string{},
		Extensions: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"},
		RootFiles:  []string{"compile_commands.json", "CMakeLists.txt", "Makefile"},
	})
}

// Register adds or updates a language configuration.
//
// Description:
//
//	Registers a language server configuration. If a configuration already
//	exists for the language, it is replaced. Also updates the extension
//	mapping for quick lookups.
//
// Inputs:
//
//	config - The language configuration to register
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) Register(config LanguageConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byLanguage[config.Language] = config

	// Update extension mapping
	for _, ext := range config.Extensions {
		r.byExt[ext] = config.Language
	}
}

// Get returns the configuration for a language.
//
// Description:
//
//	Looks up the configuration for the specified language identifier.
//
// Inputs:
//
//	language - The language identifier (e.g., "go", "python")
//
// Outputs:
//
//	LanguageConfig - The configuration (zero value if not found)
//	bool - True if the configuration was found
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) Get(language string) (LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	config, ok := r.byLanguage[language]
	return config, ok
}

// GetByExtension returns the configuration for a file extension.
//
// Description:
//
//	Looks up the configuration for the language that handles the given
//	file extension.
//
// Inputs:
//
//	ext - The file extension including dot (e.g., ".go", ".py")
//
// Outputs:
//
//	LanguageConfig - The configuration (zero value if not found)
//	bool - True if the configuration was found
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) GetByExtension(ext string) (LanguageConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lang, ok := r.byExt[ext]
	if !ok {
		return LanguageConfig{}, false
	}
	config, ok := r.byLanguage[lang]
	return config, ok
}

// Languages returns all registered language names.
//
// Description:
//
//	Returns a slice of all language identifiers that have configurations.
//
// Outputs:
//
//	[]string - Language identifiers
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	langs := make([]string, 0, len(r.byLanguage))
	for lang := range r.byLanguage {
		langs = append(langs, lang)
	}
	return langs
}

// Extensions returns all file extensions mapped to a language.
//
// Description:
//
//	Returns a slice of all file extensions that have configurations.
//
// Outputs:
//
//	[]string - File extensions including dots
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) Extensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	exts := make([]string, 0, len(r.byExt))
	for ext := range r.byExt {
		exts = append(exts, ext)
	}
	return exts
}

// LanguageForExtension returns the language identifier for a file extension.
//
// Description:
//
//	Maps a file extension to its language identifier.
//
// Inputs:
//
//	ext - The file extension including dot (e.g., ".go")
//
// Outputs:
//
//	string - The language identifier (empty if not found)
//	bool - True if a mapping was found
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *ConfigRegistry) LanguageForExtension(ext string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lang, ok := r.byExt[ext]
	return lang, ok
}
