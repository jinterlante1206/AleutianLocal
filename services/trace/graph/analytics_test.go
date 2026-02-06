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
// Analytics Test Fixtures
// =============================================================================

// buildHotspotGraph creates a graph where some nodes are more connected.
func buildHotspotGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	// Hub function - called by many
	hub := &ast.Symbol{
		ID:       "pkg/hub.go:10:Hub",
		Name:     "Hub",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/hub.go",
		Exported: true,
	}

	// Multiple callers
	caller1 := &ast.Symbol{
		ID:       "pkg/caller.go:10:Caller1",
		Name:     "Caller1",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/caller.go",
	}
	caller2 := &ast.Symbol{
		ID:       "pkg/caller.go:20:Caller2",
		Name:     "Caller2",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/caller.go",
	}
	caller3 := &ast.Symbol{
		ID:       "pkg/caller.go:30:Caller3",
		Name:     "Caller3",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/caller.go",
	}

	// Isolated function
	isolated := &ast.Symbol{
		ID:       "pkg/isolated.go:10:Isolated",
		Name:     "Isolated",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/isolated.go",
	}

	mustAddNode(t, g, hub)
	mustAddNode(t, g, caller1)
	mustAddNode(t, g, caller2)
	mustAddNode(t, g, caller3)
	mustAddNode(t, g, isolated)

	// All callers call hub
	mustAddEdge(t, g, caller1.ID, hub.ID, EdgeTypeCalls)
	mustAddEdge(t, g, caller2.ID, hub.ID, EdgeTypeCalls)
	mustAddEdge(t, g, caller3.ID, hub.ID, EdgeTypeCalls)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

// buildDeadCodeGraph creates a graph with some unreferenced symbols.
func buildDeadCodeGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	// Entry point
	main := &ast.Symbol{
		ID:       "cmd/main.go:10:main",
		Name:     "main",
		Kind:     ast.SymbolKindFunction,
		Package:  "cmd",
		FilePath: "cmd/main.go",
	}

	// Called function
	used := &ast.Symbol{
		ID:       "pkg/used.go:10:UsedFunc",
		Name:     "UsedFunc",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/used.go",
		Exported: true,
	}

	// Unused function (dead code)
	unused := &ast.Symbol{
		ID:       "pkg/unused.go:10:UnusedFunc",
		Name:     "UnusedFunc",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/unused.go",
		Exported: true,
	}

	// Test function (should not be flagged)
	testFunc := &ast.Symbol{
		ID:       "pkg/used_test.go:10:TestUsed",
		Name:     "TestUsed",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg",
		FilePath: "pkg/used_test.go",
	}

	mustAddNode(t, g, main)
	mustAddNode(t, g, used)
	mustAddNode(t, g, unused)
	mustAddNode(t, g, testFunc)

	// main calls used
	mustAddEdge(t, g, main.ID, used.ID, EdgeTypeCalls)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

// buildCouplingGraph creates a graph with known coupling characteristics.
func buildCouplingGraph(t *testing.T) *HierarchicalGraph {
	t.Helper()

	g := NewGraph("/test/project")

	// Stable package (many dependents, no dependencies)
	stableInterface := &ast.Symbol{
		ID:       "pkg/stable/interface.go:10:Core",
		Name:     "Core",
		Kind:     ast.SymbolKindInterface,
		Package:  "pkg/stable",
		FilePath: "pkg/stable/interface.go",
		Exported: true,
	}

	// Unstable package (no dependents, many dependencies)
	unstableFunc := &ast.Symbol{
		ID:       "pkg/unstable/impl.go:10:Impl",
		Name:     "Impl",
		Kind:     ast.SymbolKindStruct,
		Package:  "pkg/unstable",
		FilePath: "pkg/unstable/impl.go",
		Exported: true,
	}

	// Middle package (some of both)
	middleFunc := &ast.Symbol{
		ID:       "pkg/middle/service.go:10:Service",
		Name:     "Service",
		Kind:     ast.SymbolKindFunction,
		Package:  "pkg/middle",
		FilePath: "pkg/middle/service.go",
		Exported: true,
	}

	mustAddNode(t, g, stableInterface)
	mustAddNode(t, g, unstableFunc)
	mustAddNode(t, g, middleFunc)

	// unstable depends on stable
	mustAddEdge(t, g, unstableFunc.ID, stableInterface.ID, EdgeTypeImplements)
	// middle depends on stable
	mustAddEdge(t, g, middleFunc.ID, stableInterface.ID, EdgeTypeReferences)
	// unstable depends on middle
	mustAddEdge(t, g, unstableFunc.ID, middleFunc.ID, EdgeTypeCalls)

	g.Freeze()

	hg, err := WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}

	return hg
}

// =============================================================================
// GraphAnalytics Constructor Tests
// =============================================================================

func TestNewGraphAnalytics(t *testing.T) {
	t.Run("valid graph", func(t *testing.T) {
		hg := buildTestGraph(t)
		analytics := NewGraphAnalytics(hg)
		if analytics == nil {
			t.Fatal("expected non-nil analytics")
		}
	})

	t.Run("nil graph", func(t *testing.T) {
		analytics := NewGraphAnalytics(nil)
		if analytics != nil {
			t.Error("expected nil for nil graph")
		}
	})
}

// =============================================================================
// HotSpots Tests
// =============================================================================

func TestHotSpots(t *testing.T) {
	hg := buildHotspotGraph(t)
	analytics := NewGraphAnalytics(hg)

	t.Run("returns top N", func(t *testing.T) {
		hotspots := analytics.HotSpots(2)
		if len(hotspots) != 2 {
			t.Errorf("expected 2 hotspots, got %d", len(hotspots))
		}
	})

	t.Run("hub is highest", func(t *testing.T) {
		hotspots := analytics.HotSpots(5)
		if len(hotspots) == 0 {
			t.Fatal("expected at least 1 hotspot")
		}

		// Hub has 3 incoming edges, so should have highest score
		top := hotspots[0]
		if top.Node.Symbol.Name != "Hub" {
			t.Errorf("expected Hub as top hotspot, got %s", top.Node.Symbol.Name)
		}
		if top.InDegree != 3 {
			t.Errorf("expected InDegree=3, got %d", top.InDegree)
		}
	})

	t.Run("zero top returns empty", func(t *testing.T) {
		hotspots := analytics.HotSpots(0)
		if len(hotspots) != 0 {
			t.Errorf("expected empty result, got %d", len(hotspots))
		}
	})

	t.Run("negative top returns empty", func(t *testing.T) {
		hotspots := analytics.HotSpots(-1)
		if len(hotspots) != 0 {
			t.Errorf("expected empty result, got %d", len(hotspots))
		}
	})
}

func TestHotSpotsWithCRS(t *testing.T) {
	hg := buildHotspotGraph(t)
	analytics := NewGraphAnalytics(hg)
	ctx := context.Background()

	hotspots, traceStep := analytics.HotSpotsWithCRS(ctx, 3)

	if len(hotspots) != 3 {
		t.Errorf("expected 3 hotspots, got %d", len(hotspots))
	}

	if traceStep.Action != "analytics_hotspots" {
		t.Errorf("expected Action='analytics_hotspots', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["requested"] != "3" {
		t.Errorf("expected requested='3', got '%s'", traceStep.Metadata["requested"])
	}
}

// =============================================================================
// DeadCode Tests
// =============================================================================

func TestDeadCode(t *testing.T) {
	hg := buildDeadCodeGraph(t)
	analytics := NewGraphAnalytics(hg)

	deadCode := analytics.DeadCode()

	t.Run("finds unused function", func(t *testing.T) {
		found := false
		for _, dc := range deadCode {
			if dc.Node.Symbol.Name == "UnusedFunc" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected UnusedFunc to be flagged as dead code")
		}
	})

	t.Run("excludes entry points", func(t *testing.T) {
		for _, dc := range deadCode {
			if dc.Node.Symbol.Name == "main" {
				t.Error("main should not be flagged as dead code")
			}
			if dc.Node.Symbol.Name == "TestUsed" {
				t.Error("test functions should not be flagged as dead code")
			}
		}
	})

	t.Run("excludes used functions", func(t *testing.T) {
		for _, dc := range deadCode {
			if dc.Node.Symbol.Name == "UsedFunc" {
				t.Error("UsedFunc has callers, should not be flagged")
			}
		}
	})
}

func TestDeadCodeWithCRS(t *testing.T) {
	hg := buildDeadCodeGraph(t)
	analytics := NewGraphAnalytics(hg)
	ctx := context.Background()

	deadCode, traceStep := analytics.DeadCodeWithCRS(ctx)

	if traceStep.Action != "analytics_dead_code" {
		t.Errorf("expected Action='analytics_dead_code', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["dead_code_count"] == "" {
		t.Error("expected dead_code_count in metadata")
	}

	// Verify we found at least the unused function
	if len(deadCode) == 0 {
		t.Error("expected at least one dead code item")
	}
}

func TestIsEntryPoint(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"main", true},
		{"init", true},
		{"TestFoo", true},
		{"BenchmarkBar", true},
		{"FuzzBaz", true},
		{"ExampleQux", true},
		{"ServeHTTP", false}, // Only method with this name is entry point
		{"RegularFunc", false},
		{"Test", false},      // Exactly "Test" is not an entry point
		{"Benchmark", false}, // Exactly "Benchmark" is not an entry point
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sym := &ast.Symbol{
				ID:   "test.go:1:" + tc.name,
				Name: tc.name,
				Kind: ast.SymbolKindFunction,
			}
			result := isEntryPoint(sym)
			if result != tc.expected {
				t.Errorf("isEntryPoint(%s) = %v, expected %v", tc.name, result, tc.expected)
			}
		})
	}

	t.Run("ServeHTTP method", func(t *testing.T) {
		sym := &ast.Symbol{
			ID:   "test.go:1:ServeHTTP",
			Name: "ServeHTTP",
			Kind: ast.SymbolKindMethod,
		}
		if !isEntryPoint(sym) {
			t.Error("ServeHTTP method should be entry point")
		}
	})

	t.Run("nil symbol", func(t *testing.T) {
		if isEntryPoint(nil) {
			t.Error("nil symbol should not be entry point")
		}
	})
}

// =============================================================================
// CyclicDependencies Tests
// =============================================================================

func TestCyclicDependencies(t *testing.T) {
	t.Run("finds cycle", func(t *testing.T) {
		hg := buildCyclicGraph(t)
		analytics := NewGraphAnalytics(hg)

		cycles := analytics.CyclicDependencies()

		if len(cycles) != 1 {
			t.Errorf("expected 1 cycle, got %d", len(cycles))
		}

		if len(cycles) > 0 {
			cycle := cycles[0]
			if cycle.Length != 3 {
				t.Errorf("expected cycle length=3, got %d", cycle.Length)
			}
		}
	})

	t.Run("no cycles in acyclic graph", func(t *testing.T) {
		hg := buildTestGraph(t)
		analytics := NewGraphAnalytics(hg)

		cycles := analytics.CyclicDependencies()

		if len(cycles) != 0 {
			t.Errorf("expected 0 cycles, got %d", len(cycles))
		}
	})
}

func TestCyclicDependenciesWithCRS(t *testing.T) {
	hg := buildCyclicGraph(t)
	analytics := NewGraphAnalytics(hg)
	ctx := context.Background()

	cycles, traceStep := analytics.CyclicDependenciesWithCRS(ctx)

	if len(cycles) != 1 {
		t.Errorf("expected 1 cycle, got %d", len(cycles))
	}

	if traceStep.Action != "analytics_cycles" {
		t.Errorf("expected Action='analytics_cycles', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["cycles_found"] != "1" {
		t.Errorf("expected cycles_found='1', got '%s'", traceStep.Metadata["cycles_found"])
	}
}

// =============================================================================
// PackageCoupling Tests
// =============================================================================

func TestPackageCoupling(t *testing.T) {
	hg := buildCouplingGraph(t)
	analytics := NewGraphAnalytics(hg)

	metrics := analytics.PackageCoupling()

	t.Run("has all packages", func(t *testing.T) {
		if len(metrics) != 3 {
			t.Errorf("expected 3 packages, got %d", len(metrics))
		}
	})

	t.Run("stable package metrics", func(t *testing.T) {
		stable, ok := metrics["pkg/stable"]
		if !ok {
			t.Fatal("expected metrics for pkg/stable")
		}

		// Stable has 2 dependents (middle and unstable), 0 dependencies
		if stable.Afferent != 2 {
			t.Errorf("expected Afferent=2, got %d", stable.Afferent)
		}
		if stable.Efferent != 0 {
			t.Errorf("expected Efferent=0, got %d", stable.Efferent)
		}
		if stable.Instability != 0.0 {
			t.Errorf("expected Instability=0.0, got %f", stable.Instability)
		}
	})

	t.Run("unstable package metrics", func(t *testing.T) {
		unstable, ok := metrics["pkg/unstable"]
		if !ok {
			t.Fatal("expected metrics for pkg/unstable")
		}

		// Unstable has 0 dependents, 2 dependencies (stable and middle)
		if unstable.Afferent != 0 {
			t.Errorf("expected Afferent=0, got %d", unstable.Afferent)
		}
		if unstable.Efferent != 2 {
			t.Errorf("expected Efferent=2, got %d", unstable.Efferent)
		}
		if unstable.Instability != 1.0 {
			t.Errorf("expected Instability=1.0, got %f", unstable.Instability)
		}
	})

	t.Run("abstractness calculation", func(t *testing.T) {
		stable, ok := metrics["pkg/stable"]
		if !ok {
			t.Fatal("expected metrics for pkg/stable")
		}

		// stable only has an interface (abstract)
		if stable.AbstractTypes != 1 {
			t.Errorf("expected AbstractTypes=1, got %d", stable.AbstractTypes)
		}
		if stable.Abstractness != 1.0 {
			t.Errorf("expected Abstractness=1.0, got %f", stable.Abstractness)
		}
	})
}

func TestPackageCouplingWithCRS(t *testing.T) {
	hg := buildCouplingGraph(t)
	analytics := NewGraphAnalytics(hg)
	ctx := context.Background()

	metrics, traceStep := analytics.PackageCouplingWithCRS(ctx)

	if len(metrics) != 3 {
		t.Errorf("expected 3 packages, got %d", len(metrics))
	}

	if traceStep.Action != "analytics_coupling" {
		t.Errorf("expected Action='analytics_coupling', got '%s'", traceStep.Action)
	}
	if traceStep.Metadata["packages_analyzed"] != "3" {
		t.Errorf("expected packages_analyzed='3', got '%s'", traceStep.Metadata["packages_analyzed"])
	}
}

func TestGetCouplingForPackage(t *testing.T) {
	hg := buildCouplingGraph(t)
	analytics := NewGraphAnalytics(hg)

	t.Run("existing package", func(t *testing.T) {
		metrics := analytics.GetCouplingForPackage("pkg/stable")
		if metrics == nil {
			t.Fatal("expected metrics for pkg/stable")
		}
		if metrics.Package != "pkg/stable" {
			t.Errorf("expected Package='pkg/stable', got '%s'", metrics.Package)
		}
	})

	t.Run("non-existent package", func(t *testing.T) {
		metrics := analytics.GetCouplingForPackage("pkg/nonexistent")
		// Should return metrics with zero values, not nil
		if metrics == nil {
			t.Fatal("expected non-nil metrics")
		}
		if metrics.Afferent != 0 || metrics.Efferent != 0 {
			t.Error("expected zero coupling for non-existent package")
		}
	})
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHotSpots(b *testing.B) {
	// Build a larger graph for benchmarking
	g := NewGraph("/bench")
	for i := 0; i < 1000; i++ {
		sym := &ast.Symbol{
			ID:       "pkg/bench.go:" + itoa(i) + ":Func" + itoa(i),
			Name:     "Func" + itoa(i),
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg",
			FilePath: "pkg/bench.go",
		}
		g.AddNode(sym)
	}

	// Add random edges
	for i := 0; i < 500; i++ {
		from := "pkg/bench.go:" + itoa(i) + ":Func" + itoa(i)
		to := "pkg/bench.go:" + itoa((i+1)%1000) + ":Func" + itoa((i+1)%1000)
		g.AddEdge(from, to, EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()
	hg, _ := WrapGraph(g)
	analytics := NewGraphAnalytics(hg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analytics.HotSpots(10)
	}
}

func BenchmarkCyclicDependencies(b *testing.B) {
	// Build a graph without cycles for worst-case Tarjan's
	g := NewGraph("/bench")
	for i := 0; i < 1000; i++ {
		sym := &ast.Symbol{
			ID:       "pkg/bench.go:" + itoa(i) + ":Func" + itoa(i),
			Name:     "Func" + itoa(i),
			Kind:     ast.SymbolKindFunction,
			Package:  "pkg",
			FilePath: "pkg/bench.go",
		}
		g.AddNode(sym)
	}

	// Linear chain (no cycles)
	for i := 0; i < 999; i++ {
		from := "pkg/bench.go:" + itoa(i) + ":Func" + itoa(i)
		to := "pkg/bench.go:" + itoa(i+1) + ":Func" + itoa(i+1)
		g.AddEdge(from, to, EdgeTypeCalls, ast.Location{})
	}

	g.Freeze()
	hg, _ := WrapGraph(g)
	analytics := NewGraphAnalytics(hg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analytics.CyclicDependencies()
	}
}
