// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// buildTestGraph creates a graph with known structure for testing.
// Structure:
//
//	pkg/auth/
//	  ├── validator.go
//	  │   ├── Validate (func)
//	  │   └── Token (struct)
//	  └── handler.go
//	      └── AuthHandler (func)
//	pkg/core/
//	  ├── service.go
//	  │   └── Service (interface)
//	  └── types.go
//	      └── Config (struct)
//
// Edges:
//   - AuthHandler -> Validate (calls)
//   - Validate -> Token (uses)
//   - Service -> Config (references)
func buildTestGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	// Package: pkg/auth
	validate := &ast.Symbol{
		ID:       "pkg/auth/validator.go:10:Validate",
		Name:     "Validate",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/auth",
		FilePath: "pkg/auth/validator.go",
		Exported: true,
	}
	token := &ast.Symbol{
		ID:       "pkg/auth/validator.go:20:Token",
		Name:     "Token",
		Kind:     ast.SymbolKindStruct,
		Package:  "pkg/auth",
		FilePath: "pkg/auth/validator.go",
		Exported: true,
	}
	authHandler := &ast.Symbol{
		ID:       "pkg/auth/handler.go:10:AuthHandler",
		Name:     "AuthHandler",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/auth",
		FilePath: "pkg/auth/handler.go",
		Exported: true,
	}

	// Package: pkg/core
	service := &ast.Symbol{
		ID:       "pkg/core/service.go:5:Service",
		Name:     "Service",
		Kind:     ast.SymbolKindInterface,
		Package:  "pkg/core",
		FilePath: "pkg/core/service.go",
		Exported: true,
	}
	config := &ast.Symbol{
		ID:       "pkg/core/types.go:10:Config",
		Name:     "Config",
		Kind:     ast.SymbolKindStruct,
		Package:  "pkg/core",
		FilePath: "pkg/core/types.go",
		Exported: true,
	}

	// Add nodes
	mustAddNode(t, g, validate)
	mustAddNode(t, g, token)
	mustAddNode(t, g, authHandler)
	mustAddNode(t, g, service)
	mustAddNode(t, g, config)

	// Add edges
	// AuthHandler calls Validate (same package)
	mustAddEdge(t, g, authHandler.ID, validate.ID, EdgeTypeCalls)
	// Validate uses Token (same package)
	mustAddEdge(t, g, validate.ID, token.ID, EdgeTypeReferences)
	// Service references Config (same package)
	mustAddEdge(t, g, service.ID, config.ID, EdgeTypeReferences)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

// buildCrossPackageGraph creates a graph with cross-package dependencies.
func buildCrossPackageGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	// pkg/a depends on pkg/b
	funcA := &ast.Symbol{
		ID:       "pkg/a/main.go:10:FuncA",
		Name:     "FuncA",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/a",
		FilePath: "pkg/a/main.go",
		Exported: true,
	}
	funcB := &ast.Symbol{
		ID:       "pkg/b/lib.go:10:FuncB",
		Name:     "FuncB",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/b",
		FilePath: "pkg/b/lib.go",
		Exported: true,
	}

	mustAddNode(t, g, funcA)
	mustAddNode(t, g, funcB)

	// Cross-package call
	mustAddEdge(t, g, funcA.ID, funcB.ID, EdgeTypeCalls)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

// buildCyclicGraph creates a graph with a cycle for testing.
func buildCyclicGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	funcA := &ast.Symbol{
		ID:       "pkg/cycle/a.go:10:FuncA",
		Name:     "FuncA",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/cycle",
		FilePath: "pkg/cycle/a.go",
	}
	funcB := &ast.Symbol{
		ID:       "pkg/cycle/b.go:10:FuncB",
		Name:     "FuncB",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/cycle",
		FilePath: "pkg/cycle/b.go",
	}
	funcC := &ast.Symbol{
		ID:       "pkg/cycle/c.go:10:FuncC",
		Name:     "FuncC",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/cycle",
		FilePath: "pkg/cycle/c.go",
	}

	mustAddNode(t, g, funcA)
	mustAddNode(t, g, funcB)
	mustAddNode(t, g, funcC)

	// Create cycle: A -> B -> C -> A
	mustAddEdge(t, g, funcA.ID, funcB.ID, EdgeTypeCalls)
	mustAddEdge(t, g, funcB.ID, funcC.ID, EdgeTypeCalls)
	mustAddEdge(t, g, funcC.ID, funcA.ID, EdgeTypeCalls)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

func mustAddNode(t *testing.T, g *Graph, sym *ast.Symbol) {
	t.Helper()
	if _, err := g.AddNode(sym); err != nil {
		t.Fatalf("AddNode(%s) failed: %v", sym.ID, err)
	}
}

func mustAddEdge(t *testing.T, g *Graph, from, to string, edgeType EdgeType) {
	t.Helper()
	if err := g.AddEdge(from, to, edgeType, ast.Location{}); err != nil {
		t.Fatalf("AddEdge(%s -> %s) failed: %v", from, to, err)
	}
}

// =============================================================================
// WrapGraph Tests
// =============================================================================

func TestWrapGraph(t *testing.T) {
	t.Run("wraps frozen graph", func(t *testing.T) {
		g := NewGraph("/test")
		g.Freeze()

		hg, err := WrapGraph(g)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if hg == nil {
			t.Fatal("expected non-nil HierarchicalGraph")
		}
	})

	t.Run("fails on unfrozen graph", func(t *testing.T) {
		g := NewGraph("/test")
		// Not frozen

		_, err := WrapGraph(g)
		if err == nil {
			t.Fatal("expected error for unfrozen graph")
		}
		if err != ErrGraphNotFrozen {
			t.Errorf("expected ErrGraphNotFrozen, got %v", err)
		}
	})

	t.Run("fails on nil graph", func(t *testing.T) {
		_, err := WrapGraph(nil)
		if err == nil {
			t.Fatal("expected error for nil graph")
		}
		if err != ErrNilGraph {
			t.Errorf("expected ErrNilGraph, got %v", err)
		}
	})
}

// =============================================================================
// Package Index Tests
// =============================================================================

func TestGetPackages(t *testing.T) {
	hg := buildTestGraph(t)

	packages := hg.GetPackages()

	if len(packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(packages))
	}

	// Check package names
	pkgNames := make(map[string]bool)
	for _, pkg := range packages {
		pkgNames[pkg.Name] = true
	}

	if !pkgNames["pkg/auth"] {
		t.Error("expected pkg/auth in packages")
	}
	if !pkgNames["pkg/core"] {
		t.Error("expected pkg/core in packages")
	}
}

func TestGetPackageInfo(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("existing package", func(t *testing.T) {
		info := hg.GetPackageInfo("pkg/auth")
		if info == nil {
			t.Fatal("expected PackageInfo for pkg/auth")
		}

		if info.Name != "pkg/auth" {
			t.Errorf("expected Name='pkg/auth', got '%s'", info.Name)
		}
		if info.NodeCount != 3 {
			t.Errorf("expected NodeCount=3, got %d", info.NodeCount)
		}
		if len(info.Files) != 2 {
			t.Errorf("expected 2 files, got %d", len(info.Files))
		}
	})

	t.Run("non-existent package", func(t *testing.T) {
		info := hg.GetPackageInfo("pkg/nonexistent")
		if info != nil {
			t.Error("expected nil for non-existent package")
		}
	})
}

func TestGetNodesInPackage(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("existing package", func(t *testing.T) {
		nodes := hg.GetNodesInPackage("pkg/auth")
		if len(nodes) != 3 {
			t.Errorf("expected 3 nodes in pkg/auth, got %d", len(nodes))
		}
	})

	t.Run("non-existent package", func(t *testing.T) {
		nodes := hg.GetNodesInPackage("pkg/nonexistent")
		if len(nodes) != 0 {
			t.Errorf("expected 0 nodes, got %d", len(nodes))
		}
	})

	t.Run("returns defensive copy", func(t *testing.T) {
		nodes1 := hg.GetNodesInPackage("pkg/auth")
		nodes2 := hg.GetNodesInPackage("pkg/auth")

		// Modify nodes1
		if len(nodes1) > 0 {
			nodes1[0] = nil
		}

		// nodes2 should be unaffected
		if nodes2[0] == nil {
			t.Error("expected defensive copy, but mutation propagated")
		}
	})
}

func TestGetNodesInFile(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("existing file", func(t *testing.T) {
		nodes := hg.GetNodesInFile("pkg/auth/validator.go")
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes in validator.go, got %d", len(nodes))
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		nodes := hg.GetNodesInFile("pkg/auth/nonexistent.go")
		if len(nodes) != 0 {
			t.Errorf("expected 0 nodes, got %d", len(nodes))
		}
	})
}

func TestGetNodesByKind(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("functions", func(t *testing.T) {
		nodes := hg.GetNodesByKind(ast.SymbolKindFunction)
		if len(nodes) != 2 {
			t.Errorf("expected 2 functions, got %d", len(nodes))
		}
	})

	t.Run("structs", func(t *testing.T) {
		nodes := hg.GetNodesByKind(ast.SymbolKindStruct)
		if len(nodes) != 2 {
			t.Errorf("expected 2 structs, got %d", len(nodes))
		}
	})

	t.Run("interfaces", func(t *testing.T) {
		nodes := hg.GetNodesByKind(ast.SymbolKindInterface)
		if len(nodes) != 1 {
			t.Errorf("expected 1 interface, got %d", len(nodes))
		}
	})
}

// =============================================================================
// Cross-Package Edge Tests
// =============================================================================

func TestGetCrossPackageEdges(t *testing.T) {
	hg := buildCrossPackageGraph(t)

	edges := hg.GetCrossPackageEdges()
	if len(edges) != 1 {
		t.Errorf("expected 1 cross-package edge, got %d", len(edges))
	}

	if len(edges) > 0 {
		edge := edges[0]
		if edge.Type != EdgeTypeCalls {
			t.Errorf("expected EdgeTypeCalls, got %v", edge.Type)
		}
	}
}

func TestGetInternalEdges(t *testing.T) {
	hg := buildTestGraph(t)

	internal := hg.GetInternalEdges()
	// All edges in buildTestGraph are within same packages
	if len(internal) != 3 {
		t.Errorf("expected 3 internal edges, got %d", len(internal))
	}
}

// =============================================================================
// Navigation Tests
// =============================================================================

func TestDrillDown(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("project to packages", func(t *testing.T) {
		nodes := hg.DrillDown(LevelProject, "")
		if len(nodes) != 2 {
			t.Errorf("expected 2 package representatives, got %d", len(nodes))
		}
	})

	t.Run("package to files", func(t *testing.T) {
		nodes := hg.DrillDown(LevelPackage, "pkg/auth")
		if len(nodes) != 2 {
			t.Errorf("expected 2 file representatives, got %d", len(nodes))
		}
	})

	t.Run("file to symbols", func(t *testing.T) {
		nodes := hg.DrillDown(LevelFile, "pkg/auth/validator.go")
		if len(nodes) != 2 {
			t.Errorf("expected 2 symbols, got %d", len(nodes))
		}
	})

	t.Run("symbol has no children", func(t *testing.T) {
		nodes := hg.DrillDown(LevelSymbol, "pkg/auth/validator.go:10:Validate")
		if len(nodes) != 0 {
			t.Errorf("expected 0 children for symbol, got %d", len(nodes))
		}
	})
}

func TestRollUp(t *testing.T) {
	hg := buildTestGraph(t)

	t.Run("symbol to file", func(t *testing.T) {
		parent, level := hg.RollUp("pkg/auth/validator.go:10:Validate")
		if parent != "pkg/auth/validator.go" {
			t.Errorf("expected parent='pkg/auth/validator.go', got '%s'", parent)
		}
		if level != LevelFile {
			t.Errorf("expected level=LevelFile, got %d", level)
		}
	})

	t.Run("non-existent node", func(t *testing.T) {
		parent, level := hg.RollUp("nonexistent")
		if parent != "" {
			t.Errorf("expected empty parent, got '%s'", parent)
		}
		if level != LevelProject {
			t.Errorf("expected level=LevelProject, got %d", level)
		}
	})
}

func TestGetSiblings(t *testing.T) {
	hg := buildTestGraph(t)

	siblings := hg.GetSiblings("pkg/auth/validator.go:10:Validate")
	// Token is in the same file
	if len(siblings) != 1 {
		t.Errorf("expected 1 sibling, got %d", len(siblings))
	}
	if len(siblings) > 0 && siblings[0].Symbol.Name != "Token" {
		t.Errorf("expected sibling='Token', got '%s'", siblings[0].Symbol.Name)
	}
}

// =============================================================================
// Package Dependency Tests
// =============================================================================

func TestGetPackageDependencies(t *testing.T) {
	hg := buildCrossPackageGraph(t)

	deps := hg.GetPackageDependencies("pkg/a")
	if len(deps) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(deps))
	}
	if len(deps) > 0 && deps[0] != "pkg/b" {
		t.Errorf("expected dependency='pkg/b', got '%s'", deps[0])
	}
}

func TestGetPackageDependents(t *testing.T) {
	hg := buildCrossPackageGraph(t)

	dependents := hg.GetPackageDependents("pkg/b")
	if len(dependents) != 1 {
		t.Errorf("expected 1 dependent, got %d", len(dependents))
	}
	if len(dependents) > 0 && dependents[0] != "pkg/a" {
		t.Errorf("expected dependent='pkg/a', got '%s'", dependents[0])
	}
}

// =============================================================================
// CRS Integration Tests
// =============================================================================

func TestGetNodesInPackageWithCRS(t *testing.T) {
	hg := buildTestGraph(t)
	ctx := context.Background()

	nodes, traceStep := hg.GetNodesInPackageWithCRS(ctx, "pkg/auth")

	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}

	if traceStep.Action != "graph_query" {
		t.Errorf("expected Action='graph_query', got '%s'", traceStep.Action)
	}
	if traceStep.Target != "pkg:pkg/auth" {
		t.Errorf("expected Target='pkg:pkg/auth', got '%s'", traceStep.Target)
	}
	if traceStep.Metadata["navigation_level"] != "1" {
		t.Errorf("expected navigation_level='1', got '%s'", traceStep.Metadata["navigation_level"])
	}
}

func TestGetPackagesWithCRS(t *testing.T) {
	hg := buildTestGraph(t)
	ctx := context.Background()

	packages, traceStep := hg.GetPackagesWithCRS(ctx)

	if len(packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(packages))
	}

	if traceStep.Action != "graph_query" {
		t.Errorf("expected Action='graph_query', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["navigation_level"] != "0" {
		t.Errorf("expected navigation_level='0', got '%s'", traceStep.Metadata["navigation_level"])
	}
}

func TestDrillDownWithCRS(t *testing.T) {
	hg := buildTestGraph(t)
	ctx := context.Background()

	nodes, traceStep := hg.DrillDownWithCRS(ctx, LevelPackage, "pkg/auth")

	if len(nodes) != 2 {
		t.Errorf("expected 2 files, got %d", len(nodes))
	}

	if traceStep.Action != "drill_down" {
		t.Errorf("expected Action='drill_down', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["from_level"] != "1" {
		t.Errorf("expected from_level='1', got '%s'", traceStep.Metadata["from_level"])
	}
	if traceStep.Metadata["to_level"] != "2" {
		t.Errorf("expected to_level='2', got '%s'", traceStep.Metadata["to_level"])
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestLevelName(t *testing.T) {
	tests := []struct {
		level    int
		expected string
	}{
		{LevelProject, "project"},
		{LevelPackage, "package"},
		{LevelFile, "file"},
		{LevelSymbol, "symbol"},
		{99, "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := LevelName(tc.level)
			if result != tc.expected {
				t.Errorf("LevelName(%d) = '%s', expected '%s'", tc.level, result, tc.expected)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-5, "-5"},
		{12345, "12345"},
	}

	for _, tc := range tests {
		result := itoa(tc.input)
		if result != tc.expected {
			t.Errorf("itoa(%d) = '%s', expected '%s'", tc.input, result, tc.expected)
		}
	}
}
