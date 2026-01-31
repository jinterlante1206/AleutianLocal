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
)

// PythonHierarchy implements LanguageHierarchy for Python projects.
//
// Python hierarchy structure:
// - Level 0: Project (directory containing pyproject.toml or setup.py)
// - Level 1: Package (directory containing __init__.py)
// - Level 2: Module (.py file)
// - Level 3: Symbol (function, class, method, etc.)
//
// Entity ID format:
// - Project: "" (empty, represents root)
// - Package: "myapp/auth" or "src/handlers"
// - Module: "myapp/auth/validator.py"
// - Symbol: "myapp/auth/validator.py#validate_token"
//
// Thread Safety: Safe for concurrent use (stateless).
type PythonHierarchy struct{}

// Language returns "python".
func (h *PythonHierarchy) Language() string {
	return "python"
}

// LevelCount returns 4 (project, package, module, symbol).
func (h *PythonHierarchy) LevelCount() int {
	return 4
}

// LevelName returns the name for each level.
func (h *PythonHierarchy) LevelName(level int) string {
	switch level {
	case 0:
		return "project"
	case 1:
		return "package"
	case 2:
		return "module"
	case 3:
		return "symbol"
	default:
		return "unknown"
	}
}

// ParentOf returns the parent entity ID.
func (h *PythonHierarchy) ParentOf(entityID string) (string, error) {
	if entityID == "" {
		return "", ErrNoParent
	}

	components := h.ParseEntityID(entityID)

	switch components.Level {
	case 3: // Symbol → Module
		return components.Package + "/" + components.File, nil
	case 2: // Module → Package
		return components.Package, nil
	case 1: // Package → Project or Parent Package
		parent := filepath.Dir(components.Package)
		if parent == "." || parent == "" {
			return "", nil // Return project root
		}
		return parent, nil
	case 0: // Project has no parent
		return "", ErrNoParent
	default:
		return "", ErrInvalidEntityID
	}
}

// ChildrenOf returns possible child entity IDs.
// Note: Returns empty since we can't know children without filesystem access.
func (h *PythonHierarchy) ChildrenOf(entityID string) ([]string, error) {
	return []string{}, nil
}

// EntityLevel determines the hierarchy level from an entity ID.
func (h *PythonHierarchy) EntityLevel(entityID string) int {
	if entityID == "" {
		return 0 // Project
	}

	// Check for symbol (contains #)
	if strings.Contains(entityID, "#") {
		return 3 // Symbol
	}

	// Check for module (ends with .py or .pyi)
	if strings.HasSuffix(entityID, ".py") || strings.HasSuffix(entityID, ".pyi") {
		return 2 // Module
	}

	// Otherwise it's a package
	return 1
}

// RootMarkers returns Python project root markers.
func (h *PythonHierarchy) RootMarkers() []string {
	return []string{
		"pyproject.toml",
		"setup.py",
		"setup.cfg",
		"requirements.txt",
		"Pipfile",
	}
}

// ParseEntityID extracts components from a Python entity ID.
func (h *PythonHierarchy) ParseEntityID(entityID string) EntityComponents {
	if entityID == "" {
		return EntityComponents{Level: 0}
	}

	// Check for symbol: "myapp/auth/validator.py#validate_token"
	if idx := strings.LastIndex(entityID, "#"); idx != -1 {
		filePath := entityID[:idx]
		symbol := entityID[idx+1:]

		dir := filepath.Dir(filePath)
		file := filepath.Base(filePath)

		return EntityComponents{
			Package: dir,
			File:    file,
			Symbol:  symbol,
			Level:   3,
		}
	}

	// Check for module: "myapp/auth/validator.py"
	if strings.HasSuffix(entityID, ".py") || strings.HasSuffix(entityID, ".pyi") {
		dir := filepath.Dir(entityID)
		file := filepath.Base(entityID)

		// Handle root-level modules
		if dir == "." {
			dir = ""
		}

		return EntityComponents{
			Package: dir,
			File:    file,
			Level:   2,
		}
	}

	// Package: "myapp/auth"
	return EntityComponents{
		Package: entityID,
		Level:   1,
	}
}

// BuildEntityID constructs an entity ID from components.
func (h *PythonHierarchy) BuildEntityID(c EntityComponents) string {
	switch c.Level {
	case 0:
		return ""
	case 1:
		return c.Package
	case 2:
		if c.Package == "" {
			return c.File
		}
		return c.Package + "/" + c.File
	case 3:
		path := c.File
		if c.Package != "" {
			path = c.Package + "/" + c.File
		}
		return path + "#" + c.Symbol
	default:
		return ""
	}
}

// IsTestFile returns true if the file is a Python test file.
func (h *PythonHierarchy) IsTestFile(filePath string) bool {
	base := filepath.Base(filePath)
	return strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		base == "conftest.py"
}

// IsPrivateModule returns true if the module is private (starts with _).
func (h *PythonHierarchy) IsPrivateModule(modulePath string) bool {
	base := filepath.Base(modulePath)
	// __init__.py and __main__.py are not private
	if base == "__init__.py" || base == "__main__.py" {
		return false
	}
	return strings.HasPrefix(base, "_")
}

// IsPackageInit returns true if the file is __init__.py.
func (h *PythonHierarchy) IsPackageInit(filePath string) bool {
	return filepath.Base(filePath) == "__init__.py"
}

// PackageFromFile extracts the package path from a file path.
func (h *PythonHierarchy) PackageFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	if dir == "." {
		return ""
	}
	return dir
}

// ModuleNameFromFile returns the Python module name from a file path.
// e.g., "myapp/auth/validator.py" → "myapp.auth.validator"
func (h *PythonHierarchy) ModuleNameFromFile(filePath string) string {
	// Remove .py extension
	name := strings.TrimSuffix(filePath, ".py")
	name = strings.TrimSuffix(name, ".pyi")

	// Replace path separator with dots
	name = strings.ReplaceAll(name, "/", ".")
	name = strings.ReplaceAll(name, "\\", ".")

	// Handle __init__ - just use the package name
	if strings.HasSuffix(name, ".__init__") {
		name = strings.TrimSuffix(name, ".__init__")
	}

	return name
}
