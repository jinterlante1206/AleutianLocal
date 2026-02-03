// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package seeder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// ResolveDependencies finds all dependencies for a project.
//
// Description:
//
//	Parses the project's dependency manifest (go.mod for Go) and returns
//	a list of dependencies with their resolved local cache paths.
//
// Inputs:
//
//	ctx - Context for cancellation
//	projectRoot - Absolute path to the project root
//
// Outputs:
//
//	[]Dependency - List of resolved dependencies
//	error - Non-nil if parsing fails
//
// Errors:
//
//	ErrNoGoMod - No go.mod file found
func ResolveDependencies(ctx context.Context, projectRoot string) ([]Dependency, error) {
	// Validate project root
	if err := ValidateProjectRoot(projectRoot); err != nil {
		return nil, fmt.Errorf("invalid project root: %w", err)
	}

	// Check for go.mod
	goModPath := filepath.Join(projectRoot, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return nil, ErrNoGoMod
	}

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	deps, err := ParseGoMod(content)
	if err != nil {
		return nil, err
	}

	// Resolve local paths
	for i := range deps {
		if err := ctx.Err(); err != nil {
			return deps[:i], err
		}

		localPath, err := ResolveLocalPath(deps[i])
		if err != nil {
			// Not cached - skip but don't fail
			continue
		}
		deps[i].LocalPath = localPath
	}

	return deps, nil
}

// ParseGoMod parses a go.mod file and returns dependencies.
//
// Description:
//
//	Uses the official Go module parser to extract require statements.
//	Skips indirect dependencies to keep scope manageable.
//
// Inputs:
//
//	content - Raw go.mod file content
//
// Outputs:
//
//	[]Dependency - List of direct dependencies
//	error - Non-nil if parsing fails
func ParseGoMod(content []byte) ([]Dependency, error) {
	f, err := modfile.Parse("go.mod", content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}

	var deps []Dependency
	for _, req := range f.Require {
		deps = append(deps, Dependency{
			ModulePath: req.Mod.Path,
			Version:    req.Mod.Version,
			Language:   "go",
			Indirect:   req.Indirect,
		})
	}

	return deps, nil
}

// ResolveLocalPath finds the local cache path for a Go module.
//
// Description:
//
//	Locates the module in the Go module cache. Handles path escaping
//	for modules with capital letters or special characters.
//
// Inputs:
//
//	dep - The dependency to resolve
//
// Outputs:
//
//	string - Absolute path to the cached module
//	error - ErrModuleNotCached if not found
func ResolveLocalPath(dep Dependency) (string, error) {
	if dep.Language != "go" {
		return "", ErrUnsupportedLanguage
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		gopath = filepath.Join(home, "go")
	}

	// Go mod download escapes special chars: capital letters become !lowercase
	escaped, err := module.EscapePath(dep.ModulePath)
	if err != nil {
		return "", fmt.Errorf("escape module path: %w", err)
	}

	version, err := module.EscapeVersion(dep.Version)
	if err != nil {
		return "", fmt.Errorf("escape version: %w", err)
	}

	localPath := filepath.Join(gopath, "pkg", "mod", escaped+"@"+version)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s@%s", ErrModuleNotCached, dep.ModulePath, dep.Version)
	}

	return localPath, nil
}

// FilterDirectDependencies returns only direct (non-indirect) dependencies.
func FilterDirectDependencies(deps []Dependency) []Dependency {
	var direct []Dependency
	for _, dep := range deps {
		if !dep.Indirect {
			direct = append(direct, dep)
		}
	}
	return direct
}
