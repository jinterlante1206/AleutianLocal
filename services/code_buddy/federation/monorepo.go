// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package federation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// MonorepoAnalyzer detects and analyzes package boundaries in a monorepo.
//
// # Description
//
// Identifies packages within a monorepo using go.mod, package.json, and other
// manifest files. Tracks internal vs public APIs and cross-package dependencies.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type MonorepoAnalyzer struct {
	root     string
	mu       sync.RWMutex
	packages []Package
	edges    []PackageEdge
}

// Package represents a package within a monorepo.
type Package struct {
	// ID is a unique identifier for this package.
	ID string `json:"id"`

	// Name is the package name.
	Name string `json:"name"`

	// Path is the relative path from monorepo root.
	Path string `json:"path"`

	// Type is the package type (go, npm, python).
	Type PackageType `json:"type"`

	// PublicAPIs are symbols exported for use by other packages.
	PublicAPIs []string `json:"public_apis,omitempty"`

	// InternalAPIs are symbols for internal use only.
	InternalAPIs []string `json:"internal_apis,omitempty"`

	// Dependencies are direct package dependencies.
	Dependencies []string `json:"dependencies"`
}

// PackageType represents the type of package.
type PackageType string

const (
	PackageTypeGo     PackageType = "go"
	PackageTypeNPM    PackageType = "npm"
	PackageTypePython PackageType = "python"
)

// PackageEdge represents a dependency between packages.
type PackageEdge struct {
	// From is the dependent package ID.
	From string `json:"from"`

	// To is the dependency package ID.
	To string `json:"to"`

	// IsInternal indicates if this uses internal APIs.
	IsInternal bool `json:"is_internal"`

	// Symbols are the specific symbols used.
	Symbols []string `json:"symbols,omitempty"`
}

// NewMonorepoAnalyzer creates a new monorepo analyzer.
//
// # Inputs
//
//   - root: Path to the monorepo root.
//
// # Outputs
//
//   - *MonorepoAnalyzer: Ready-to-use analyzer.
func NewMonorepoAnalyzer(root string) *MonorepoAnalyzer {
	return &MonorepoAnalyzer{
		root:     root,
		packages: make([]Package, 0),
		edges:    make([]PackageEdge, 0),
	}
}

// DiscoverPackages finds all packages in the monorepo.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (m *MonorepoAnalyzer) DiscoverPackages(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.packages = make([]Package, 0)

	// Walk the tree looking for package manifests
	err := filepath.WalkDir(m.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			name := d.Name()
			// Skip common non-source directories
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "__pycache__" || name == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check for package manifests
		name := d.Name()
		dir := filepath.Dir(path)
		relDir, _ := filepath.Rel(m.root, dir)

		switch name {
		case "go.mod":
			pkg, err := m.parseGoMod(path, relDir)
			if err == nil {
				m.packages = append(m.packages, pkg)
			}
		case "package.json":
			pkg, err := m.parsePackageJSON(path, relDir)
			if err == nil {
				m.packages = append(m.packages, pkg)
			}
		case "setup.py", "pyproject.toml":
			pkg, err := m.parsePythonPackage(path, relDir)
			if err == nil {
				m.packages = append(m.packages, pkg)
			}
		}

		return nil
	})

	return err
}

// parseGoMod parses a go.mod file for package info.
func (m *MonorepoAnalyzer) parseGoMod(path, relDir string) (Package, error) {
	file, err := os.Open(path)
	if err != nil {
		return Package{}, err
	}
	defer file.Close()

	pkg := Package{
		Path:         relDir,
		Type:         PackageTypeGo,
		Dependencies: make([]string, 0),
	}

	scanner := bufio.NewScanner(file)
	var inRequire bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "module ") {
			pkg.Name = strings.TrimPrefix(line, "module ")
			pkg.ID = pkg.Name
		}

		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}

		if inRequire {
			if line == ")" {
				inRequire = false
				continue
			}
			// Parse dependency line
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				dep := parts[0]
				// Only track internal monorepo dependencies
				if m.isInternalDep(dep) {
					pkg.Dependencies = append(pkg.Dependencies, dep)
				}
			}
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") && !inRequire {
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 1 {
				dep := parts[0]
				if m.isInternalDep(dep) {
					pkg.Dependencies = append(pkg.Dependencies, dep)
				}
			}
		}
	}

	if pkg.Name == "" {
		return Package{}, fmt.Errorf("no module name found")
	}

	return pkg, scanner.Err()
}

// parsePackageJSON parses a package.json file.
func (m *MonorepoAnalyzer) parsePackageJSON(path, relDir string) (Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Package{}, err
	}

	var pkgJSON struct {
		Name         string            `json:"name"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		return Package{}, err
	}

	if pkgJSON.Name == "" {
		return Package{}, fmt.Errorf("no package name found")
	}

	pkg := Package{
		ID:           pkgJSON.Name,
		Name:         pkgJSON.Name,
		Path:         relDir,
		Type:         PackageTypeNPM,
		Dependencies: make([]string, 0),
	}

	// Track workspace/internal dependencies
	for dep := range pkgJSON.Dependencies {
		if m.isInternalNPMDep(dep) {
			pkg.Dependencies = append(pkg.Dependencies, dep)
		}
	}

	return pkg, nil
}

// parsePythonPackage parses Python package metadata.
func (m *MonorepoAnalyzer) parsePythonPackage(path, relDir string) (Package, error) {
	// Get package name from directory
	dirName := filepath.Base(relDir)
	if dirName == "." {
		dirName = filepath.Base(m.root)
	}

	pkg := Package{
		ID:           dirName,
		Name:         dirName,
		Path:         relDir,
		Type:         PackageTypePython,
		Dependencies: make([]string, 0),
	}

	// Try to parse pyproject.toml or setup.py for more info
	if strings.HasSuffix(path, "pyproject.toml") {
		data, err := os.ReadFile(path)
		if err == nil {
			// Simple name extraction from pyproject.toml
			namePattern := regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
			if matches := namePattern.FindSubmatch(data); len(matches) > 1 {
				pkg.Name = string(matches[1])
				pkg.ID = pkg.Name
			}
		}
	}

	return pkg, nil
}

// isInternalDep checks if a Go dependency is internal to the monorepo.
func (m *MonorepoAnalyzer) isInternalDep(dep string) bool {
	// Check if any existing package matches this import
	for _, pkg := range m.packages {
		if strings.HasPrefix(dep, pkg.Name) {
			return true
		}
	}
	return false
}

// isInternalNPMDep checks if an NPM dependency is internal (workspace).
func (m *MonorepoAnalyzer) isInternalNPMDep(dep string) bool {
	// Common workspace patterns
	if strings.HasPrefix(dep, "@") {
		// Scoped packages might be internal
		for _, pkg := range m.packages {
			if pkg.Name == dep {
				return true
			}
		}
	}
	return false
}

// AnalyzeDependencies discovers cross-package dependencies.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (m *MonorepoAnalyzer) AnalyzeDependencies(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.edges = make([]PackageEdge, 0)

	// Build package index
	pkgIndex := make(map[string]*Package)
	for i := range m.packages {
		pkgIndex[m.packages[i].ID] = &m.packages[i]
	}

	// Analyze each package's imports
	for _, pkg := range m.packages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pkgPath := filepath.Join(m.root, pkg.Path)

		err := filepath.WalkDir(pkgPath, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".js" && ext != ".ts" && ext != ".py" {
				return nil
			}

			edges, _ := m.analyzeFileImports(path, pkg.ID, pkgIndex)
			m.edges = append(m.edges, edges...)
			return nil
		})

		if err != nil {
			continue
		}
	}

	return nil
}

// analyzeFileImports finds imports in a file.
func (m *MonorepoAnalyzer) analyzeFileImports(path, pkgID string, pkgIndex map[string]*Package) ([]PackageEdge, error) {
	var edges []PackageEdge

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	ext := filepath.Ext(path)

	var importPattern *regexp.Regexp
	switch ext {
	case ".go":
		importPattern = regexp.MustCompile(`import\s+(?:\(\s*[\s\S]*?\)|"([^"]+)")`)
	case ".js", ".ts":
		importPattern = regexp.MustCompile(`(?:import|require)\s*\(?["']([^"']+)["']`)
	case ".py":
		importPattern = regexp.MustCompile(`(?:from|import)\s+([^\s]+)`)
	}

	if importPattern == nil {
		return nil, nil
	}

	content := make([]byte, 0)
	for scanner.Scan() {
		content = append(content, scanner.Bytes()...)
		content = append(content, '\n')
	}

	matches := importPattern.FindAllStringSubmatch(string(content), -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		importPath := match[1]

		// Check if this import is from another internal package
		for targetID, targetPkg := range pkgIndex {
			if targetID == pkgID {
				continue
			}

			// Check if import matches package
			if strings.Contains(importPath, targetPkg.Name) ||
				strings.HasPrefix(importPath, targetPkg.Name) {
				edges = append(edges, PackageEdge{
					From:       pkgID,
					To:         targetID,
					IsInternal: m.isInternalImport(importPath, targetPkg),
				})
				break
			}
		}
	}

	return edges, scanner.Err()
}

// isInternalImport checks if an import uses internal APIs.
func (m *MonorepoAnalyzer) isInternalImport(importPath string, targetPkg *Package) bool {
	// Common patterns for internal packages
	return strings.Contains(importPath, "/internal/") ||
		strings.Contains(importPath, "/private/") ||
		strings.Contains(importPath, "/_") // underscore prefix
}

// Packages returns all discovered packages.
func (m *MonorepoAnalyzer) Packages() []Package {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Package, len(m.packages))
	copy(result, m.packages)
	return result
}

// Edges returns all package edges.
func (m *MonorepoAnalyzer) Edges() []PackageEdge {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PackageEdge, len(m.edges))
	copy(result, m.edges)
	return result
}

// GetPackage returns a package by ID.
func (m *MonorepoAnalyzer) GetPackage(id string) (*Package, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.packages {
		if m.packages[i].ID == id {
			return &m.packages[i], true
		}
	}
	return nil, false
}

// GetDependentsOf returns packages that depend on the given package.
func (m *MonorepoAnalyzer) GetDependentsOf(pkgID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var dependents []string
	seen := make(map[string]bool)

	for _, edge := range m.edges {
		if edge.To == pkgID && !seen[edge.From] {
			dependents = append(dependents, edge.From)
			seen[edge.From] = true
		}
	}

	return dependents
}

// GetDependenciesOf returns packages that the given package depends on.
func (m *MonorepoAnalyzer) GetDependenciesOf(pkgID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var deps []string
	seen := make(map[string]bool)

	for _, edge := range m.edges {
		if edge.From == pkgID && !seen[edge.To] {
			deps = append(deps, edge.To)
			seen[edge.To] = true
		}
	}

	return deps
}

// PackageBlastRadius calculates the blast radius for a package change.
type PackageBlastRadius struct {
	// Package is the changed package.
	Package string `json:"package"`

	// AffectedPackages are packages that depend on the changed package.
	AffectedPackages []AffectedPackage `json:"affected_packages"`

	// TotalAffected is the total count of affected packages.
	TotalAffected int `json:"total_affected"`

	// HasInternalUsage indicates if internal APIs are used externally.
	HasInternalUsage bool `json:"has_internal_usage"`
}

// AffectedPackage represents an affected package.
type AffectedPackage struct {
	// ID is the package identifier.
	ID string `json:"id"`

	// UsesInternal indicates if it uses internal APIs.
	UsesInternal bool `json:"uses_internal"`

	// TransitiveDepth is how many hops away this package is.
	TransitiveDepth int `json:"transitive_depth"`
}

// CalculatePackageBlastRadius computes blast radius for a package change.
func (m *MonorepoAnalyzer) CalculatePackageBlastRadius(ctx context.Context, pkgID string) (*PackageBlastRadius, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result := &PackageBlastRadius{
		Package:          pkgID,
		AffectedPackages: make([]AffectedPackage, 0),
	}

	// BFS to find all affected packages
	visited := make(map[string]int) // pkg -> depth
	queue := []string{pkgID}
	visited[pkgID] = 0

	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		current := queue[0]
		queue = queue[1:]
		currentDepth := visited[current]

		for _, edge := range m.edges {
			if edge.To == current {
				if _, seen := visited[edge.From]; !seen {
					visited[edge.From] = currentDepth + 1
					queue = append(queue, edge.From)

					result.AffectedPackages = append(result.AffectedPackages, AffectedPackage{
						ID:              edge.From,
						UsesInternal:    edge.IsInternal,
						TransitiveDepth: currentDepth + 1,
					})

					if edge.IsInternal {
						result.HasInternalUsage = true
					}
				}
			}
		}
	}

	result.TotalAffected = len(result.AffectedPackages)
	return result, nil
}

// DetectInternalAPILeaks finds cases where internal APIs are used externally.
func (m *MonorepoAnalyzer) DetectInternalAPILeaks() []PackageEdge {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var leaks []PackageEdge
	for _, edge := range m.edges {
		if edge.IsInternal {
			leaks = append(leaks, edge)
		}
	}
	return leaks
}
