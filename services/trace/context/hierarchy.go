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
	"path/filepath"
	"strings"
	"sync"
)

// LanguageHierarchy defines how a language organizes code into levels.
//
// Different languages have different hierarchy structures:
// - Go: project → package → file → symbol
// - Python: project → package → module → symbol
// - TypeScript: project → module → file → symbol
//
// Thread Safety: Implementations must be safe for concurrent use.
type LanguageHierarchy interface {
	// Language returns the language identifier (e.g., "go", "python").
	Language() string

	// LevelCount returns the number of hierarchy levels (typically 4).
	LevelCount() int

	// LevelName returns human-readable name for a level.
	//
	// Inputs:
	//   - level: Hierarchy level (0-3 typically).
	//
	// Outputs:
	//   - string: Level name (e.g., "package", "module", "file").
	LevelName(level int) string

	// ParentOf returns the parent entity ID for a given entity.
	//
	// Inputs:
	//   - entityID: The entity ID (e.g., "pkg/auth/validator.go").
	//
	// Outputs:
	//   - string: Parent entity ID (e.g., "pkg/auth").
	//   - error: ErrNoParent if entity is root, ErrInvalidEntityID if malformed.
	ParentOf(entityID string) (string, error)

	// ChildrenOf returns child entity IDs for a given entity.
	// This is a structural query - it returns possible children based on ID,
	// not by scanning the filesystem.
	//
	// Inputs:
	//   - entityID: The entity ID.
	//
	// Outputs:
	//   - []string: Child entity IDs.
	//   - error: ErrInvalidEntityID if malformed.
	//
	// Note: For actual file system children, use the graph/manifest instead.
	ChildrenOf(entityID string) ([]string, error)

	// EntityLevel returns the hierarchy level for an entity.
	//
	// Inputs:
	//   - entityID: The entity ID.
	//
	// Outputs:
	//   - int: Level (0=project, 1=package, 2=file, 3=function).
	EntityLevel(entityID string) int

	// RootMarkers returns files that indicate project/package roots.
	//
	// Outputs:
	//   - []string: Marker file names (e.g., ["go.mod"], ["pyproject.toml"]).
	RootMarkers() []string

	// ParseEntityID extracts components from an entity ID.
	//
	// Inputs:
	//   - entityID: The entity ID.
	//
	// Outputs:
	//   - EntityComponents: Parsed components.
	ParseEntityID(entityID string) EntityComponents

	// BuildEntityID constructs an entity ID from components.
	//
	// Inputs:
	//   - components: The components to join.
	//
	// Outputs:
	//   - string: The constructed entity ID.
	BuildEntityID(components EntityComponents) string
}

// EntityComponents contains the parsed parts of an entity ID.
type EntityComponents struct {
	// ProjectRoot is the project identifier (Level 0).
	ProjectRoot string

	// Package is the package/module identifier (Level 1).
	Package string

	// File is the file name (Level 2).
	File string

	// Symbol is the symbol name (Level 3).
	Symbol string

	// Level is the inferred hierarchy level.
	Level int
}

// HierarchyRegistry manages language hierarchy implementations.
//
// Thread Safety: Safe for concurrent use.
type HierarchyRegistry struct {
	hierarchies map[string]LanguageHierarchy
	mu          sync.RWMutex
}

// NewHierarchyRegistry creates a new registry with default hierarchies.
func NewHierarchyRegistry() *HierarchyRegistry {
	r := &HierarchyRegistry{
		hierarchies: make(map[string]LanguageHierarchy),
	}
	r.registerDefaults()
	return r
}

// registerDefaults adds the built-in language hierarchies.
func (r *HierarchyRegistry) registerDefaults() {
	r.Register(&GoHierarchy{})
	r.Register(&PythonHierarchy{})
}

// Register adds a language hierarchy to the registry.
func (r *HierarchyRegistry) Register(h LanguageHierarchy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hierarchies[h.Language()] = h
}

// Get returns the hierarchy for a language.
//
// Inputs:
//   - language: The language identifier.
//
// Outputs:
//   - LanguageHierarchy: The hierarchy, or nil if not found.
//   - bool: True if found.
func (r *HierarchyRegistry) Get(language string) (LanguageHierarchy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hierarchies[language]
	return h, ok
}

// Languages returns all registered language identifiers.
func (r *HierarchyRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	langs := make([]string, 0, len(r.hierarchies))
	for lang := range r.hierarchies {
		langs = append(langs, lang)
	}
	return langs
}

// DetectLanguage attempts to detect the language from a file path.
//
// Inputs:
//   - filePath: The file path to analyze.
//
// Outputs:
//   - string: The detected language ("go", "python", etc.) or empty if unknown.
func (r *HierarchyRegistry) DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	default:
		return ""
	}
}

// GetForFile returns the appropriate hierarchy for a file.
//
// Inputs:
//   - filePath: The file path.
//
// Outputs:
//   - LanguageHierarchy: The hierarchy, or nil if language not supported.
func (r *HierarchyRegistry) GetForFile(filePath string) LanguageHierarchy {
	lang := r.DetectLanguage(filePath)
	if lang == "" {
		return nil
	}
	h, _ := r.Get(lang)
	return h
}

// DefaultHierarchyRegistry is the global default registry.
var DefaultHierarchyRegistry = NewHierarchyRegistry()

// GetHierarchyForLanguage is a convenience function to get a hierarchy.
func GetHierarchyForLanguage(language string) (LanguageHierarchy, bool) {
	return DefaultHierarchyRegistry.Get(language)
}
