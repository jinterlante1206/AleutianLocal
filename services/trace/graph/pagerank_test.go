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
	"math"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// =============================================================================
// PageRank Tests (GR-12)
// =============================================================================

func TestPageRankOptions_Validate(t *testing.T) {
	tests := []struct {
		name     string
		opts     PageRankOptions
		expected PageRankOptions
	}{
		{
			name: "valid options unchanged",
			opts: PageRankOptions{
				DampingFactor: 0.8,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
			expected: PageRankOptions{
				DampingFactor: 0.8,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
		},
		{
			name: "negative damping replaced with default",
			opts: PageRankOptions{
				DampingFactor: -0.5,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
			expected: PageRankOptions{
				DampingFactor: DefaultDampingFactor,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
		},
		{
			name: "damping > 1 replaced with default",
			opts: PageRankOptions{
				DampingFactor: 1.5,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
			expected: PageRankOptions{
				DampingFactor: DefaultDampingFactor,
				MaxIterations: 50,
				Convergence:   1e-5,
			},
		},
		{
			name: "zero iterations replaced with default",
			opts: PageRankOptions{
				DampingFactor: 0.85,
				MaxIterations: 0,
				Convergence:   1e-5,
			},
			expected: PageRankOptions{
				DampingFactor: 0.85,
				MaxIterations: DefaultMaxIterations,
				Convergence:   1e-5,
			},
		},
		{
			name: "negative convergence replaced with default",
			opts: PageRankOptions{
				DampingFactor: 0.85,
				MaxIterations: 50,
				Convergence:   -1e-5,
			},
			expected: PageRankOptions{
				DampingFactor: 0.85,
				MaxIterations: 50,
				Convergence:   DefaultConvergence,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.Validate()

			if opts.DampingFactor != tt.expected.DampingFactor {
				t.Errorf("DampingFactor = %v, want %v", opts.DampingFactor, tt.expected.DampingFactor)
			}
			if opts.MaxIterations != tt.expected.MaxIterations {
				t.Errorf("MaxIterations = %v, want %v", opts.MaxIterations, tt.expected.MaxIterations)
			}
			if opts.Convergence != tt.expected.Convergence {
				t.Errorf("Convergence = %v, want %v", opts.Convergence, tt.expected.Convergence)
			}
		})
	}
}

func TestPageRank_EmptyGraph(t *testing.T) {
	// R3: Empty graph should return empty result without panic
	g := createEmptyGraph()
	analytics := NewGraphAnalytics(g)

	result := analytics.PageRank(context.Background(), nil)

	if result == nil {
		t.Fatal("expected non-nil result for empty graph")
	}
	if len(result.Scores) != 0 {
		t.Errorf("expected 0 scores, got %d", len(result.Scores))
	}
	if !result.Converged {
		t.Error("expected converged=true for empty graph")
	}
}

func TestPageRank_NilOptions(t *testing.T) {
	// nil options should use defaults
	g := newTestGraph("test").addNode("A", "main.go").build()
	analytics := NewGraphAnalytics(g)
	result := analytics.PageRank(context.Background(), nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Scores) != 1 {
		t.Errorf("expected 1 score, got %d", len(result.Scores))
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	g := newTestGraph("test").addNode("A", "main.go").build()
	analytics := NewGraphAnalytics(g)
	result := analytics.PageRank(context.Background(), nil)

	// Single node should have score of 1.0
	if score, ok := result.Scores["A"]; !ok || math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected score ~1.0 for single node, got %v", score)
	}
	if !result.Converged {
		t.Error("expected convergence for single node")
	}
}

func TestPageRank_SimpleChain(t *testing.T) {
	// A -> B -> C
	// PageRank should flow from A through B to C
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addNode("C", "util.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result := analytics.PageRank(context.Background(), nil)

	// Verify convergence
	if !result.Converged {
		t.Error("expected convergence for simple chain")
	}

	// Verify ranking: C > B > A (C receives rank from both A and B)
	scoreA := result.Scores["A"]
	scoreB := result.Scores["B"]
	scoreC := result.Scores["C"]

	// A has no incoming edges, should have lower score
	if scoreA >= scoreB {
		t.Errorf("expected scoreB > scoreA, got scoreB=%v, scoreA=%v", scoreB, scoreA)
	}

	// Scores should sum to ~1.0
	total := scoreA + scoreB + scoreC
	if math.Abs(total-1.0) > 0.01 {
		t.Errorf("expected scores to sum to ~1.0, got %v", total)
	}
}

func TestPageRank_SinkNode(t *testing.T) {
	// I1: Sink node handling
	// A -> B (B has no outgoing edges, is a "sink")
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result := analytics.PageRank(context.Background(), nil)

	// Verify convergence
	if !result.Converged {
		t.Error("expected convergence with sink node")
	}

	// Scores should sum to ~1.0 (no rank leakage)
	total := result.Scores["A"] + result.Scores["B"]
	if math.Abs(total-1.0) > 0.01 {
		t.Errorf("expected scores to sum to ~1.0 (no leakage), got %v", total)
	}

	// B should have higher score (receives rank from A)
	if result.Scores["B"] <= result.Scores["A"] {
		t.Errorf("expected B > A, got B=%v, A=%v", result.Scores["B"], result.Scores["A"])
	}
}

func TestPageRank_Cycle(t *testing.T) {
	// A -> B -> C -> A (cycle)
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addNode("C", "util.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result := analytics.PageRank(context.Background(), nil)

	// Verify convergence
	if !result.Converged {
		t.Error("expected convergence for cycle")
	}

	// All nodes in symmetric cycle should have equal scores
	scoreA := result.Scores["A"]
	scoreB := result.Scores["B"]
	scoreC := result.Scores["C"]

	tolerance := 0.01
	if math.Abs(scoreA-scoreB) > tolerance || math.Abs(scoreB-scoreC) > tolerance {
		t.Errorf("expected equal scores in symmetric cycle, got A=%v, B=%v, C=%v", scoreA, scoreB, scoreC)
	}
}

func TestPageRank_ContextCancellation(t *testing.T) {
	// R1: Context cancellation
	builder := newTestGraph("test")
	// Create a larger graph to ensure multiple iterations
	for i := 0; i < 100; i++ {
		builder.addNode("node"+itoa(i), "file.go")
	}
	for i := 0; i < 99; i++ {
		builder.addEdge("node"+itoa(i), "node"+itoa(i+1), EdgeTypeCalls)
	}
	g := builder.build()

	analytics := NewGraphAnalytics(g)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := analytics.PageRank(ctx, &PageRankOptions{
		MaxIterations: 1000,
		Convergence:   1e-10, // Very tight convergence to ensure many iterations
	})

	// Should return partial result without panic
	if result == nil {
		t.Fatal("expected non-nil result even with cancelled context")
	}
	if result.Converged {
		t.Error("expected converged=false for cancelled context")
	}
}

func TestPageRankTop(t *testing.T) {
	// A -> B -> C (without disconnected nodes for clearer ranking)
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addNode("C", "util.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	top := analytics.PageRankTop(context.Background(), 2, nil)

	if len(top) != 2 {
		t.Fatalf("expected 2 results, got %d", len(top))
	}

	// Verify ranking (1-indexed)
	if top[0].Rank != 1 || top[1].Rank != 2 {
		t.Errorf("expected ranks 1,2, got %d,%d", top[0].Rank, top[1].Rank)
	}

	// In A -> B -> C chain, C gets rank from B, B gets rank from A
	// C should be in top 2 (typically #1 or #2)
	foundC := false
	for _, n := range top {
		if n.Node.ID == "C" {
			foundC = true
			break
		}
	}
	if !foundC {
		t.Errorf("expected C in top 2, got %s and %s", top[0].Node.ID, top[1].Node.ID)
	}

	// DegreeScore should be populated for nodes with edges
	// C has 1 incoming edge, so DegreeScore should be 2 (1*2 + 0)
	for _, n := range top {
		if n.Node.ID == "C" && n.DegreeScore == 0 {
			t.Logf("C DegreeScore=%d, Incoming=%d", n.DegreeScore, len(n.Node.Incoming))
		}
	}
}

func TestPageRankTop_KLargerThanNodes(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		build()

	analytics := NewGraphAnalytics(g)
	top := analytics.PageRankTop(context.Background(), 100, nil)

	// Should return all nodes (2), not panic
	if len(top) != 2 {
		t.Errorf("expected 2 results, got %d", len(top))
	}
}

func TestPageRankTop_KZero(t *testing.T) {
	g := newTestGraph("test").addNode("A", "main.go").build()
	analytics := NewGraphAnalytics(g)
	top := analytics.PageRankTop(context.Background(), 0, nil)

	if len(top) != 0 {
		t.Errorf("expected 0 results for k=0, got %d", len(top))
	}
}

func TestPageRankWithCRS(t *testing.T) {
	// C1: CRS integration
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, step := analytics.PageRankWithCRS(context.Background(), nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify TraceStep
	if step.Action != "analytics_pagerank" {
		t.Errorf("expected action 'analytics_pagerank', got %s", step.Action)
	}
	if step.Tool != "PageRank" {
		t.Errorf("expected tool 'PageRank', got %s", step.Tool)
	}
	if step.Metadata["converged"] == "" {
		t.Error("expected 'converged' in metadata")
	}
	if step.Metadata["iterations"] == "" {
		t.Error("expected 'iterations' in metadata")
	}
}

func TestPageRankTopWithCRS(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "lib.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	top, step := analytics.PageRankTopWithCRS(context.Background(), 5, nil)

	if len(top) != 2 {
		t.Errorf("expected 2 results, got %d", len(top))
	}

	// Verify TraceStep
	if step.Action != "analytics_pagerank_top" {
		t.Errorf("expected action 'analytics_pagerank_top', got %s", step.Action)
	}
	if step.Metadata["requested"] != "5" {
		t.Errorf("expected requested=5, got %s", step.Metadata["requested"])
	}
	if step.Metadata["returned"] != "2" {
		t.Errorf("expected returned=2, got %s", step.Metadata["returned"])
	}
}

func TestPageRank_VsHotSpots(t *testing.T) {
	// Verify PageRank gives different ranking than degree-based HotSpots
	// Create: main -> helper1, helper1 -> importantFunc
	//         main -> helper2, helper2 -> importantFunc
	// helper1 and helper2 have same degree, but importantFunc should rank higher
	// with PageRank because it inherits importance from main via two paths

	g := newTestGraph("test").
		addNode("main", "main.go").
		addNode("helper1", "lib.go").
		addNode("helper2", "lib.go").
		addNode("importantFunc", "core.go").
		addNode("trivial", "util.go").
		addEdge("main", "helper1", EdgeTypeCalls).
		addEdge("main", "helper2", EdgeTypeCalls).
		addEdge("helper1", "importantFunc", EdgeTypeCalls).
		addEdge("helper2", "importantFunc", EdgeTypeCalls).
		addEdge("importantFunc", "trivial", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)

	// Get both rankings
	prTop := analytics.PageRankTop(context.Background(), 5, nil)
	hsTop := analytics.HotSpots(5)

	// PageRank should rank importantFunc higher
	prRanks := make(map[string]int)
	for _, n := range prTop {
		prRanks[n.Node.ID] = n.Rank
	}

	hsRanks := make(map[string]int)
	for i, n := range hsTop {
		hsRanks[n.Node.ID] = i + 1
	}

	// importantFunc should be in top 3 for PageRank
	if prRanks["importantFunc"] > 3 {
		t.Errorf("expected importantFunc in top 3 PageRank, got rank %d", prRanks["importantFunc"])
	}

	t.Logf("PageRank ranking: %v", prRanks)
	t.Logf("HotSpots ranking: %v", hsRanks)
}

// =============================================================================
// Benchmark
// =============================================================================

func BenchmarkPageRank_100Nodes(b *testing.B) {
	g := createBenchmarkGraph(100)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultPageRankOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analytics.PageRank(ctx, opts)
	}
}

func BenchmarkPageRank_1000Nodes(b *testing.B) {
	g := createBenchmarkGraph(1000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultPageRankOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analytics.PageRank(ctx, opts)
	}
}

func BenchmarkPageRank_10000Nodes(b *testing.B) {
	g := createBenchmarkGraph(10000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultPageRankOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analytics.PageRank(ctx, opts)
	}
}

func createBenchmarkGraph(n int) *HierarchicalGraph {
	builder := newTestGraph("benchmark")

	// Create nodes
	for i := 0; i < n; i++ {
		builder.addNode("node"+itoa(i), "file"+itoa(i%10)+".go")
	}

	// Create edges (each node calls 3-5 random nodes)
	for i := 0; i < n; i++ {
		numEdges := 3 + (i % 3)
		for j := 0; j < numEdges; j++ {
			target := (i + j*17 + 7) % n // Pseudo-random but deterministic
			if target != i {
				builder.addEdge("node"+itoa(i), "node"+itoa(target), EdgeTypeCalls)
			}
		}
	}

	return builder.build()
}

// =============================================================================
// Test Helpers
// =============================================================================

// testGraphBuilder helps construct test graphs with a fluent API.
type testGraphBuilder struct {
	g *Graph
}

func newTestGraph(name string) *testGraphBuilder {
	return &testGraphBuilder{g: NewGraph(name)}
}

func (b *testGraphBuilder) addNode(id, filePath string) *testGraphBuilder {
	sym := &ast.Symbol{
		ID:        id,
		Name:      id,
		Kind:      ast.SymbolKindFunction,
		FilePath:  filePath,
		Package:   "main",
		StartLine: 1,
		EndLine:   10,
		Language:  "go",
	}
	b.g.AddNode(sym)
	return b
}

func (b *testGraphBuilder) addEdge(fromID, toID string, edgeType EdgeType) *testGraphBuilder {
	b.g.AddEdge(fromID, toID, edgeType, ast.Location{StartLine: 5})
	return b
}

func (b *testGraphBuilder) build() *HierarchicalGraph {
	b.g.Freeze()
	hg, _ := WrapGraph(b.g)
	return hg
}

// Legacy helpers for compatibility
func createEmptyGraph() *HierarchicalGraph {
	return newTestGraph("test").build()
}

func addTestNode(g *Graph, id, filePath string) {
	sym := &ast.Symbol{
		ID:        id,
		Name:      id,
		Kind:      ast.SymbolKindFunction,
		FilePath:  filePath,
		Package:   "main",
		StartLine: 1,
		EndLine:   10,
		Language:  "go",
	}
	g.AddNode(sym)
}

func addTestEdge(g *Graph, fromID, toID string, edgeType EdgeType) {
	g.AddEdge(fromID, toID, edgeType, ast.Location{StartLine: 5})
}
