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

// GoHierarchy implements LanguageHierarchy for Go projects.
//
// Go hierarchy structure:
// - Level 0: Project (directory containing go.mod)
// - Level 1: Package (directory containing .go files)
// - Level 2: File (.go file)
// - Level 3: Symbol (function, type, method, etc.)
//
// Entity ID format:
// - Project: "" (empty, represents root)
// - Package: "pkg/auth" or "internal/db"
// - File: "pkg/auth/validator.go"
// - Symbol: "pkg/auth/validator.go#ValidateToken"
//
// Thread Safety: Safe for concurrent use (stateless).
type GoHierarchy struct{}

// Language returns "go".
func (h *GoHierarchy) Language() string {
	return "go"
}

// LevelCount returns 4 (project, package, file, symbol).
func (h *GoHierarchy) LevelCount() int {
	return 4
}

// LevelName returns the name for each level.
func (h *GoHierarchy) LevelName(level int) string {
	switch level {
	case 0:
		return "project"
	case 1:
		return "package"
	case 2:
		return "file"
	case 3:
		return "function"
	default:
		return "unknown"
	}
}

// ParentOf returns the parent entity ID.
func (h *GoHierarchy) ParentOf(entityID string) (string, error) {
	if entityID == "" {
		return "", ErrNoParent
	}

	components := h.ParseEntityID(entityID)

	switch components.Level {
	case 3: // Symbol → File
		return components.Package + "/" + components.File, nil
	case 2: // File → Package
		return components.Package, nil
	case 1: // Package → Project
		// Check if it's a nested package
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
// Note: This returns structural children based on ID pattern,
// not actual filesystem children.
func (h *GoHierarchy) ChildrenOf(entityID string) ([]string, error) {
	// This method returns empty since we can't know children
	// without filesystem access. The actual children should be
	// retrieved from the graph or manifest.
	return []string{}, nil
}

// EntityLevel determines the hierarchy level from an entity ID.
func (h *GoHierarchy) EntityLevel(entityID string) int {
	if entityID == "" {
		return 0 // Project
	}

	// Check for symbol (contains #)
	if strings.Contains(entityID, "#") {
		return 3 // Symbol
	}

	// Check for file (ends with .go)
	if strings.HasSuffix(entityID, ".go") {
		return 2 // File
	}

	// Otherwise it's a package
	return 1
}

// RootMarkers returns Go project root markers.
func (h *GoHierarchy) RootMarkers() []string {
	return []string{"go.mod", "go.sum"}
}

// ParseEntityID extracts components from a Go entity ID.
func (h *GoHierarchy) ParseEntityID(entityID string) EntityComponents {
	if entityID == "" {
		return EntityComponents{Level: 0}
	}

	// Check for symbol: "pkg/auth/validator.go#ValidateToken"
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

	// Check for file: "pkg/auth/validator.go"
	if strings.HasSuffix(entityID, ".go") {
		dir := filepath.Dir(entityID)
		file := filepath.Base(entityID)

		// Handle root-level files
		if dir == "." {
			dir = ""
		}

		return EntityComponents{
			Package: dir,
			File:    file,
			Level:   2,
		}
	}

	// Package: "pkg/auth"
	return EntityComponents{
		Package: entityID,
		Level:   1,
	}
}

// BuildEntityID constructs an entity ID from components.
func (h *GoHierarchy) BuildEntityID(c EntityComponents) string {
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

// IsTestFile returns true if the file is a Go test file.
func (h *GoHierarchy) IsTestFile(filePath string) bool {
	return strings.HasSuffix(filePath, "_test.go")
}

// IsInternalPackage returns true if the package is internal.
func (h *GoHierarchy) IsInternalPackage(pkgPath string) bool {
	parts := strings.Split(pkgPath, "/")
	for _, part := range parts {
		if part == "internal" {
			return true
		}
	}
	return false
}

// PackageFromFile extracts the package path from a file path.
func (h *GoHierarchy) PackageFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	if dir == "." {
		return ""
	}
	return dir
}
