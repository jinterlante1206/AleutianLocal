// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
)

// PackageNode represents a package in the dependency graph.
type PackageNode struct {
	// Path is the package import path.
	Path string

	// Files lists the source files in this package.
	Files []string

	// Imports lists packages this package imports.
	Imports []string

	// ImportedBy lists packages that import this package.
	ImportedBy []string

	// IsInternal indicates if this is an internal package.
	IsInternal bool

	// IsStdLib indicates if this is a standard library package.
	IsStdLib bool
}

// PackageGraph provides package-level dependency tracking.
//
// # Description
//
// Our code graph has file and symbol nodes, but not package nodes.
// PackageGraph aggregates file imports into package-level dependencies,
// enabling circular dependency detection at the package level.
//
// # Thread Safety
//
// This type is NOT safe for concurrent modification. It is designed
// to be built once and then queried concurrently.
type PackageGraph struct {
	packages map[string]*PackageNode
	edges    map[string][]string // Package → dependencies (import edges)
}

// NewPackageGraph creates an empty package graph.
func NewPackageGraph() *PackageGraph {
	return &PackageGraph{
		packages: make(map[string]*PackageNode),
		edges:    make(map[string][]string),
	}
}

// BuildPackageGraph derives package dependencies from file imports.
//
// # Description
//
// Scans all file nodes in the code graph and aggregates their imports
// into package-level dependencies. Internal packages are identified
// by the "internal" path segment.
//
// # Inputs
//
//   - g: The code graph to analyze.
//   - projectModule: The module path (e.g., "github.com/user/repo").
//
// # Outputs
//
//   - *PackageGraph: The constructed package dependency graph.
func BuildPackageGraph(g *graph.Graph, projectModule string) *PackageGraph {
	pg := NewPackageGraph()

	// First pass: collect all packages from files
	for _, node := range g.Nodes() {
		if node.Symbol == nil {
			continue
		}

		// Determine package from file path
		pkgPath := packageFromFile(node.Symbol.FilePath, projectModule)
		if pkgPath == "" {
			continue
		}

		// Get or create package node
		pkgNode := pg.getOrCreatePackage(pkgPath)
		pkgNode.Files = append(pkgNode.Files, node.Symbol.FilePath)

		// Check if internal
		if strings.Contains(pkgPath, "/internal/") || strings.HasSuffix(pkgPath, "/internal") {
			pkgNode.IsInternal = true
		}
	}

	// Second pass: collect imports from edges
	for _, edge := range g.Edges() {
		if edge.Type != graph.EdgeTypeImports {
			continue
		}

		fromNode, fromOK := g.GetNode(edge.FromID)
		toNode, toOK := g.GetNode(edge.ToID)

		if !fromOK || !toOK {
			continue
		}

		if fromNode.Symbol == nil || toNode.Symbol == nil {
			continue
		}

		fromPkg := packageFromFile(fromNode.Symbol.FilePath, projectModule)
		toPkg := toNode.Symbol.Package

		if fromPkg == "" || toPkg == "" {
			continue
		}

		// Add import relationship
		pg.addImport(fromPkg, toPkg)
	}

	// Third pass: scan symbol imports directly
	for _, node := range g.Nodes() {
		if node.Symbol == nil || node.Symbol.Kind != ast.SymbolKindImport {
			continue
		}

		// Get package containing this import
		importerPkg := packageFromFile(node.Symbol.FilePath, projectModule)
		if importerPkg == "" {
			continue
		}

		// The import's name contains the imported package path
		importedPkg := node.Symbol.Package
		if importedPkg == "" {
			// Try to get from signature (import path)
			importedPkg = node.Symbol.Signature
		}

		if importedPkg != "" && importedPkg != importerPkg {
			pg.addImport(importerPkg, importedPkg)
		}
	}

	return pg
}

// getOrCreatePackage gets or creates a package node.
func (pg *PackageGraph) getOrCreatePackage(path string) *PackageNode {
	if node, exists := pg.packages[path]; exists {
		return node
	}

	node := &PackageNode{
		Path:       path,
		Files:      make([]string, 0),
		Imports:    make([]string, 0),
		ImportedBy: make([]string, 0),
		IsStdLib:   isStdLibPackage(path),
	}
	pg.packages[path] = node
	pg.edges[path] = make([]string, 0)

	return node
}

// addImport adds an import relationship.
func (pg *PackageGraph) addImport(from, to string) {
	// Get or create both packages
	fromNode := pg.getOrCreatePackage(from)
	toNode := pg.getOrCreatePackage(to)

	// Check if already exists
	for _, imp := range fromNode.Imports {
		if imp == to {
			return
		}
	}

	fromNode.Imports = append(fromNode.Imports, to)
	toNode.ImportedBy = append(toNode.ImportedBy, from)
	pg.edges[from] = append(pg.edges[from], to)
}

// packageFromFile derives the package path from a file path.
func packageFromFile(filePath, projectModule string) string {
	// Remove file name, keep directory
	dir := filepath.Dir(filePath)
	if dir == "." {
		return projectModule
	}

	// Combine with project module
	if projectModule != "" {
		return projectModule + "/" + dir
	}

	return dir
}

// isStdLibPackage checks if a package is from the Go standard library.
func isStdLibPackage(path string) bool {
	// Standard library packages don't have dots in the first segment
	// (e.g., "fmt", "net/http" vs "github.com/...")
	if strings.Contains(path, ".") {
		return false
	}

	// Common std lib packages
	stdPrefixes := []string{
		"archive/", "bufio", "bytes", "compress/", "container/",
		"context", "crypto/", "database/", "debug/", "embed",
		"encoding/", "errors", "expvar", "flag", "fmt", "go/",
		"hash/", "html/", "image/", "index/", "io", "log",
		"math/", "mime/", "net/", "os", "path", "plugin",
		"reflect", "regexp", "runtime/", "sort", "strconv",
		"strings", "sync", "syscall/", "testing", "text/",
		"time", "unicode/", "unsafe",
	}

	for _, prefix := range stdPrefixes {
		if strings.HasPrefix(path, prefix) || path == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}

	return false
}

// GetPackage retrieves a package node by path.
func (pg *PackageGraph) GetPackage(path string) (*PackageNode, bool) {
	node, exists := pg.packages[path]
	return node, exists
}

// Packages returns all package paths.
func (pg *PackageGraph) Packages() []string {
	paths := make([]string, 0, len(pg.packages))
	for path := range pg.packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// Dependencies returns the direct dependencies of a package.
func (pg *PackageGraph) Dependencies(path string) []string {
	return pg.edges[path]
}

// Dependents returns packages that depend on the given package.
func (pg *PackageGraph) Dependents(path string) []string {
	node, exists := pg.packages[path]
	if !exists {
		return nil
	}
	return node.ImportedBy
}

// FindCycles finds all strongly connected components (cycles) using Tarjan's algorithm.
//
// # Description
//
// Implements Tarjan's algorithm for finding strongly connected components.
// Returns all cycles (SCCs with more than one node).
//
// # Outputs
//
//   - [][]string: Each element is a cycle of package paths.
func (pg *PackageGraph) FindCycles() [][]string {
	// Tarjan's algorithm state
	index := 0
	stack := make([]string, 0)
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlinks := make(map[string]int)
	var sccs [][]string

	// Tarjan's recursive function
	var strongConnect func(v string)
	strongConnect = func(v string) {
		// Set the depth index for v
		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		// Consider successors of v
		for _, w := range pg.edges[v] {
			// Skip std lib packages
			if node, exists := pg.packages[w]; exists && node.IsStdLib {
				continue
			}

			if _, visited := indices[w]; !visited {
				// Successor w has not yet been visited; recurse on it
				strongConnect(w)
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				// Successor w is in stack and hence in the current SCC
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		// If v is a root node, pop the stack and generate an SCC
		if lowlinks[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			// Only include SCCs with more than one node (actual cycles)
			if len(scc) > 1 {
				sccs = append(sccs, scc)
			}
		}
	}

	// Run Tarjan's algorithm from each unvisited node
	for v := range pg.packages {
		if _, visited := indices[v]; !visited {
			strongConnect(v)
		}
	}

	return sccs
}

// FindShortestCycle finds the shortest cycle containing a package.
//
// # Description
//
// Uses BFS to find the shortest cycle that includes the specified package.
// Returns nil if no cycle exists.
//
// # Inputs
//
//   - pkgPath: The package to find cycles for.
//
// # Outputs
//
//   - []string: The cycle path, or nil if no cycle exists.
func (pg *PackageGraph) FindShortestCycle(pkgPath string) []string {
	if _, exists := pg.packages[pkgPath]; !exists {
		return nil
	}

	// BFS to find shortest path back to pkgPath
	type bfsNode struct {
		path    string
		parents []string // Path from pkgPath to here
	}

	visited := make(map[string]bool)
	queue := []bfsNode{{path: pkgPath, parents: []string{pkgPath}}}
	visited[pkgPath] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, next := range pg.edges[current.path] {
			// Skip std lib
			if node, exists := pg.packages[next]; exists && node.IsStdLib {
				continue
			}

			// Found cycle back to start
			if next == pkgPath {
				cycle := make([]string, len(current.parents)+1)
				copy(cycle, current.parents)
				cycle[len(cycle)-1] = pkgPath
				return cycle
			}

			if !visited[next] {
				visited[next] = true
				newParents := make([]string, len(current.parents)+1)
				copy(newParents, current.parents)
				newParents[len(newParents)-1] = next
				queue = append(queue, bfsNode{path: next, parents: newParents})
			}
		}
	}

	return nil
}

// TopologicalSort returns packages in topological order.
//
// # Description
//
// Returns packages such that if A depends on B, B comes before A.
// Returns nil if cycles exist (no valid topological order).
//
// # Outputs
//
//   - []string: Packages in topological order, or nil if cycles exist.
func (pg *PackageGraph) TopologicalSort() []string {
	// Kahn's algorithm
	inDegree := make(map[string]int)
	for path := range pg.packages {
		inDegree[path] = 0
	}

	// Count incoming edges (only for non-std lib)
	for from, tos := range pg.edges {
		for _, to := range tos {
			if node, exists := pg.packages[to]; exists && !node.IsStdLib {
				inDegree[to]++
			}
			_ = from // Avoid unused warning
		}
	}

	// Start with nodes that have no incoming edges
	queue := make([]string, 0)
	for path, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, path)
		}
	}

	var result []string

	for len(queue) > 0 {
		// Pop from queue
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		// Reduce in-degree for neighbors
		for _, next := range pg.edges[node] {
			if _, exists := inDegree[next]; !exists {
				continue
			}
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	// If not all nodes included, there's a cycle
	if len(result) != len(pg.packages) {
		return nil
	}

	return result
}

// Stats returns statistics about the package graph.
func (pg *PackageGraph) Stats() PackageGraphStats {
	totalEdges := 0
	maxDeps := 0
	maxDependents := 0
	internalCount := 0
	stdLibCount := 0

	for _, node := range pg.packages {
		deps := len(node.Imports)
		dependents := len(node.ImportedBy)

		totalEdges += deps
		if deps > maxDeps {
			maxDeps = deps
		}
		if dependents > maxDependents {
			maxDependents = dependents
		}
		if node.IsInternal {
			internalCount++
		}
		if node.IsStdLib {
			stdLibCount++
		}
	}

	return PackageGraphStats{
		PackageCount:  len(pg.packages),
		EdgeCount:     totalEdges,
		MaxDeps:       maxDeps,
		MaxDependents: maxDependents,
		InternalCount: internalCount,
		StdLibCount:   stdLibCount,
	}
}

// PackageGraphStats contains statistics about the package graph.
type PackageGraphStats struct {
	// PackageCount is the number of packages.
	PackageCount int

	// EdgeCount is the number of import edges.
	EdgeCount int

	// MaxDeps is the maximum dependencies for any package.
	MaxDeps int

	// MaxDependents is the maximum dependents for any package.
	MaxDependents int

	// InternalCount is the number of internal packages.
	InternalCount int

	// StdLibCount is the number of std lib packages.
	StdLibCount int
}
