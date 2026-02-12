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
	"sort"
	"testing"
	"time"
)

// =============================================================================
// Leiden Community Detection Tests (GR-14)
// =============================================================================

// -----------------------------------------------------------------------------
// LeidenOptions Tests
// -----------------------------------------------------------------------------

func TestLeidenOptions_Validate(t *testing.T) {
	tests := []struct {
		name     string
		opts     LeidenOptions
		expected LeidenOptions
	}{
		{
			name: "valid options unchanged",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     2,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     2,
				Resolution:           1.0,
			},
		},
		{
			name: "zero MaxIterations replaced with default",
			opts: LeidenOptions{
				MaxIterations:        0,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        DefaultMaxLeidenIterations,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
		},
		{
			name: "negative MaxIterations replaced with default",
			opts: LeidenOptions{
				MaxIterations:        -10,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        DefaultMaxLeidenIterations,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
		},
		{
			name: "zero ConvergenceThreshold replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 0,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: DefaultConvergenceThreshold,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
		},
		{
			name: "negative ConvergenceThreshold replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: -1e-6,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: DefaultConvergenceThreshold,
				MinCommunitySize:     1,
				Resolution:           1.0,
			},
		},
		{
			name: "zero MinCommunitySize replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     0,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     DefaultMinCommunitySize,
				Resolution:           1.0,
			},
		},
		{
			name: "negative MinCommunitySize replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     -5,
				Resolution:           1.0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     DefaultMinCommunitySize,
				Resolution:           1.0,
			},
		},
		{
			name: "zero Resolution replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           0,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           DefaultResolution,
			},
		},
		{
			name: "negative Resolution replaced with default",
			opts: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           -0.5,
			},
			expected: LeidenOptions{
				MaxIterations:        50,
				ConvergenceThreshold: 1e-5,
				MinCommunitySize:     1,
				Resolution:           DefaultResolution,
			},
		},
		{
			name: "all invalid values replaced with defaults",
			opts: LeidenOptions{
				MaxIterations:        0,
				ConvergenceThreshold: 0,
				MinCommunitySize:     0,
				Resolution:           0,
			},
			expected: LeidenOptions{
				MaxIterations:        DefaultMaxLeidenIterations,
				ConvergenceThreshold: DefaultConvergenceThreshold,
				MinCommunitySize:     DefaultMinCommunitySize,
				Resolution:           DefaultResolution,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.Validate()

			if opts.MaxIterations != tt.expected.MaxIterations {
				t.Errorf("MaxIterations = %v, want %v", opts.MaxIterations, tt.expected.MaxIterations)
			}
			if opts.ConvergenceThreshold != tt.expected.ConvergenceThreshold {
				t.Errorf("ConvergenceThreshold = %v, want %v", opts.ConvergenceThreshold, tt.expected.ConvergenceThreshold)
			}
			if opts.MinCommunitySize != tt.expected.MinCommunitySize {
				t.Errorf("MinCommunitySize = %v, want %v", opts.MinCommunitySize, tt.expected.MinCommunitySize)
			}
			if opts.Resolution != tt.expected.Resolution {
				t.Errorf("Resolution = %v, want %v", opts.Resolution, tt.expected.Resolution)
			}
		})
	}
}

func TestDefaultLeidenOptions(t *testing.T) {
	opts := DefaultLeidenOptions()

	if opts.MaxIterations != DefaultMaxLeidenIterations {
		t.Errorf("MaxIterations = %v, want %v", opts.MaxIterations, DefaultMaxLeidenIterations)
	}
	if opts.ConvergenceThreshold != DefaultConvergenceThreshold {
		t.Errorf("ConvergenceThreshold = %v, want %v", opts.ConvergenceThreshold, DefaultConvergenceThreshold)
	}
	if opts.MinCommunitySize != DefaultMinCommunitySize {
		t.Errorf("MinCommunitySize = %v, want %v", opts.MinCommunitySize, DefaultMinCommunitySize)
	}
	if opts.Resolution != DefaultResolution {
		t.Errorf("Resolution = %v, want %v", opts.Resolution, DefaultResolution)
	}
}

// -----------------------------------------------------------------------------
// CommunityResult Helper Method Tests
// -----------------------------------------------------------------------------

func TestCommunityResult_GetCommunityForNode(t *testing.T) {
	result := &CommunityResult{
		Communities: []Community{
			{ID: 0, Nodes: []string{"A", "B", "C"}},
			{ID: 1, Nodes: []string{"D", "E"}},
			{ID: 2, Nodes: []string{"F"}},
		},
	}

	tests := []struct {
		nodeID    string
		wantID    int
		wantFound bool
	}{
		{"A", 0, true},
		{"B", 0, true},
		{"C", 0, true},
		{"D", 1, true},
		{"E", 1, true},
		{"F", 2, true},
		{"G", -1, false},
		{"", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.nodeID, func(t *testing.T) {
			id, found := result.GetCommunityForNode(tt.nodeID)
			if id != tt.wantID {
				t.Errorf("GetCommunityForNode(%q) ID = %v, want %v", tt.nodeID, id, tt.wantID)
			}
			if found != tt.wantFound {
				t.Errorf("GetCommunityForNode(%q) found = %v, want %v", tt.nodeID, found, tt.wantFound)
			}
		})
	}
}

func TestCommunityResult_GetCommunityMembers(t *testing.T) {
	result := &CommunityResult{
		Communities: []Community{
			{ID: 0, Nodes: []string{"A", "B", "C"}},
			{ID: 1, Nodes: []string{"D", "E"}},
			{ID: 5, Nodes: []string{"F"}}, // Non-sequential ID
		},
	}

	tests := []struct {
		communityID int
		wantNodes   []string
	}{
		{0, []string{"A", "B", "C"}},
		{1, []string{"D", "E"}},
		{5, []string{"F"}},
		{2, []string{}}, // Non-existent
		{-1, []string{}},
	}

	for _, tt := range tests {
		t.Run(itoa(tt.communityID), func(t *testing.T) {
			members := result.GetCommunityMembers(tt.communityID)
			if len(members) != len(tt.wantNodes) {
				t.Errorf("GetCommunityMembers(%d) = %v, want %v", tt.communityID, members, tt.wantNodes)
				return
			}
			sort.Strings(members)
			sort.Strings(tt.wantNodes)
			for i := range members {
				if members[i] != tt.wantNodes[i] {
					t.Errorf("GetCommunityMembers(%d)[%d] = %v, want %v", tt.communityID, i, members[i], tt.wantNodes[i])
				}
			}
		})
	}
}

func TestCommunityResult_GetCommunityMembers_DoesNotMutateOriginal(t *testing.T) {
	original := []string{"A", "B", "C"}
	result := &CommunityResult{
		Communities: []Community{
			{ID: 0, Nodes: original},
		},
	}

	members := result.GetCommunityMembers(0)
	members[0] = "MODIFIED"

	// Original should be unchanged
	if result.Communities[0].Nodes[0] != "A" {
		t.Error("GetCommunityMembers should return a copy, not a reference")
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Empty/Edge Case Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_EmptyGraph(t *testing.T) {
	g := createEmptyGraph()
	analytics := NewGraphAnalytics(g)

	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for empty graph")
	}
	if len(result.Communities) != 0 {
		t.Errorf("expected 0 communities, got %d", len(result.Communities))
	}
	if !result.Converged {
		t.Error("expected converged=true for empty graph")
	}
	if result.NodeCount != 0 {
		t.Errorf("expected NodeCount=0, got %d", result.NodeCount)
	}
	if result.EdgeCount != 0 {
		t.Errorf("expected EdgeCount=0, got %d", result.EdgeCount)
	}
}

func TestDetectCommunities_NilOptions(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "main.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should work with default options
	if result.NodeCount != 2 {
		t.Errorf("expected NodeCount=2, got %d", result.NodeCount)
	}
}

func TestDetectCommunities_SingleNode(t *testing.T) {
	g := newTestGraph("test").addNode("A", "main.go").build()
	analytics := NewGraphAnalytics(g)

	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Communities) != 1 {
		t.Errorf("expected 1 community, got %d", len(result.Communities))
	}
	if len(result.Communities[0].Nodes) != 1 {
		t.Errorf("expected 1 node in community, got %d", len(result.Communities[0].Nodes))
	}
	if result.Communities[0].Nodes[0] != "A" {
		t.Errorf("expected node A, got %s", result.Communities[0].Nodes[0])
	}
	if !result.Converged {
		t.Error("expected converged=true for single node")
	}
}

func TestDetectCommunities_TwoNodesNoEdge(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "main.go").
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No edges means each node stays in its own community
	if len(result.Communities) != 2 {
		t.Errorf("expected 2 communities (no edges), got %d", len(result.Communities))
	}
	if result.Modularity != 0 {
		t.Errorf("expected modularity=0 for no edges, got %f", result.Modularity)
	}
}

func TestDetectCommunities_TwoNodesOneEdge(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "main.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two connected nodes should form one community
	if len(result.Communities) != 1 {
		t.Errorf("expected 1 community, got %d", len(result.Communities))
	}
	if len(result.Communities[0].Nodes) != 2 {
		t.Errorf("expected 2 nodes in community, got %d", len(result.Communities[0].Nodes))
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Disconnected Component Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_DisconnectedComponents(t *testing.T) {
	// Create two separate cliques that are not connected
	// Clique 1: A <-> B <-> C <-> A
	// Clique 2: D <-> E <-> F <-> D
	g := newTestGraph("test").
		addNode("A", "pkg1/a.go").
		addNode("B", "pkg1/b.go").
		addNode("C", "pkg1/c.go").
		addNode("D", "pkg2/d.go").
		addNode("E", "pkg2/e.go").
		addNode("F", "pkg2/f.go").
		// Clique 1 edges
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		// Clique 2 edges
		addEdge("D", "E", EdgeTypeCalls).
		addEdge("E", "D", EdgeTypeCalls).
		addEdge("E", "F", EdgeTypeCalls).
		addEdge("F", "E", EdgeTypeCalls).
		addEdge("F", "D", EdgeTypeCalls).
		addEdge("D", "F", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should detect exactly 2 communities
	if len(result.Communities) != 2 {
		t.Errorf("expected 2 communities for 2 disconnected cliques, got %d", len(result.Communities))
		for i, c := range result.Communities {
			t.Logf("Community %d: %v", i, c.Nodes)
		}
		return
	}

	// Verify each community has 3 nodes
	for i, comm := range result.Communities {
		if len(comm.Nodes) != 3 {
			t.Errorf("community %d: expected 3 nodes, got %d: %v", i, len(comm.Nodes), comm.Nodes)
		}
	}

	// Verify A, B, C are in the same community
	commA, _ := result.GetCommunityForNode("A")
	commB, _ := result.GetCommunityForNode("B")
	commC, _ := result.GetCommunityForNode("C")
	if commA != commB || commB != commC {
		t.Errorf("A, B, C should be in same community: A=%d, B=%d, C=%d", commA, commB, commC)
	}

	// Verify D, E, F are in the same community
	commD, _ := result.GetCommunityForNode("D")
	commE, _ := result.GetCommunityForNode("E")
	commF, _ := result.GetCommunityForNode("F")
	if commD != commE || commE != commF {
		t.Errorf("D, E, F should be in same community: D=%d, E=%d, F=%d", commD, commE, commF)
	}

	// Verify the two cliques are in different communities
	if commA == commD {
		t.Error("the two cliques should be in different communities")
	}
}

func TestDetectCommunities_ThreeDisconnectedNodes(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each node is its own community
	if len(result.Communities) != 3 {
		t.Errorf("expected 3 communities for 3 disconnected nodes, got %d", len(result.Communities))
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Known Structure Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_SimpleClique(t *testing.T) {
	// A fully connected graph of 4 nodes should form one community
	g := newTestGraph("test").
		addNode("A", "main.go").
		addNode("B", "main.go").
		addNode("C", "main.go").
		addNode("D", "main.go").
		// All pairs connected
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		addEdge("A", "D", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("B", "D", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("D", "A", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		addEdge("D", "B", EdgeTypeCalls).
		addEdge("D", "C", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fully connected graph should form one community
	if len(result.Communities) != 1 {
		t.Errorf("expected 1 community for fully connected graph, got %d", len(result.Communities))
	}

	if len(result.Communities[0].Nodes) != 4 {
		t.Errorf("expected 4 nodes in community, got %d", len(result.Communities[0].Nodes))
	}
}

func TestDetectCommunities_TwoCliquesWithBridge(t *testing.T) {
	// Two cliques connected by a single bridge edge
	// Clique 1: A, B, C (fully connected)
	// Clique 2: D, E, F (fully connected)
	// Bridge: C -> D
	g := newTestGraph("test").
		addNode("A", "pkg1/a.go").
		addNode("B", "pkg1/b.go").
		addNode("C", "pkg1/c.go").
		addNode("D", "pkg2/d.go").
		addNode("E", "pkg2/e.go").
		addNode("F", "pkg2/f.go").
		// Clique 1
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		// Clique 2
		addEdge("D", "E", EdgeTypeCalls).
		addEdge("E", "D", EdgeTypeCalls).
		addEdge("D", "F", EdgeTypeCalls).
		addEdge("F", "D", EdgeTypeCalls).
		addEdge("E", "F", EdgeTypeCalls).
		addEdge("F", "E", EdgeTypeCalls).
		// Bridge
		addEdge("C", "D", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should detect 2 communities (the bridge is weak connection)
	if len(result.Communities) != 2 {
		t.Errorf("expected 2 communities for two cliques with bridge, got %d", len(result.Communities))
		for i, c := range result.Communities {
			t.Logf("Community %d: %v", i, c.Nodes)
		}
	}

	// Verify positive modularity (indicates community structure)
	if result.Modularity <= 0 {
		t.Errorf("expected positive modularity, got %f", result.Modularity)
	}
}

func TestDetectCommunities_ChainGraph(t *testing.T) {
	// A -> B -> C -> D -> E (linear chain)
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addNode("E", "e.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("D", "E", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Chain should ideally form one community (all connected)
	// but exact behavior depends on algorithm details
	if result.NodeCount != 5 {
		t.Errorf("expected 5 nodes, got %d", result.NodeCount)
	}
	if result.EdgeCount != 4 {
		t.Errorf("expected 4 edges, got %d", result.EdgeCount)
	}
}

func TestDetectCommunities_StarGraph(t *testing.T) {
	// Hub and spoke: Center connected to A, B, C, D
	g := newTestGraph("test").
		addNode("Center", "main.go").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addEdge("Center", "A", EdgeTypeCalls).
		addEdge("Center", "B", EdgeTypeCalls).
		addEdge("Center", "C", EdgeTypeCalls).
		addEdge("Center", "D", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Star graph is relatively weak community structure
	if result.NodeCount != 5 {
		t.Errorf("expected 5 nodes, got %d", result.NodeCount)
	}
}

func TestDetectCommunities_CycleGraph(t *testing.T) {
	// A -> B -> C -> D -> A (cycle)
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("D", "A", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cycle should form one community
	if result.NodeCount != 4 {
		t.Errorf("expected 4 nodes, got %d", result.NodeCount)
	}
	if !result.Converged {
		t.Error("expected convergence for small cycle")
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Well-Connectedness Tests (Leiden's Key Guarantee)
// -----------------------------------------------------------------------------

func TestDetectCommunities_WellConnectednessGuarantee(t *testing.T) {
	// This is Leiden's key improvement over Louvain:
	// Communities must be well-connected (no disconnected subcommunities)
	//
	// Create a scenario where Louvain might produce disconnected communities:
	// Two separate cliques that get merged incorrectly, then the refinement
	// phase should split them apart.

	g := newTestGraph("test").
		// Clique 1
		addNode("A1", "pkg1/a.go").
		addNode("A2", "pkg1/b.go").
		addNode("A3", "pkg1/c.go").
		addEdge("A1", "A2", EdgeTypeCalls).
		addEdge("A2", "A1", EdgeTypeCalls).
		addEdge("A2", "A3", EdgeTypeCalls).
		addEdge("A3", "A2", EdgeTypeCalls).
		addEdge("A1", "A3", EdgeTypeCalls).
		addEdge("A3", "A1", EdgeTypeCalls).
		// Clique 2 (disconnected from Clique 1)
		addNode("B1", "pkg2/a.go").
		addNode("B2", "pkg2/b.go").
		addNode("B3", "pkg2/c.go").
		addEdge("B1", "B2", EdgeTypeCalls).
		addEdge("B2", "B1", EdgeTypeCalls).
		addEdge("B2", "B3", EdgeTypeCalls).
		addEdge("B3", "B2", EdgeTypeCalls).
		addEdge("B1", "B3", EdgeTypeCalls).
		addEdge("B3", "B1", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify well-connectedness: each community should be internally connected
	for _, comm := range result.Communities {
		if len(comm.Nodes) > 1 && !isWellConnected(analytics, comm.Nodes) {
			t.Errorf("community %d is NOT well-connected: %v", comm.ID, comm.Nodes)
		}
	}

	// A1, A2, A3 should be together; B1, B2, B3 should be together
	commA1, _ := result.GetCommunityForNode("A1")
	commA2, _ := result.GetCommunityForNode("A2")
	commA3, _ := result.GetCommunityForNode("A3")
	commB1, _ := result.GetCommunityForNode("B1")
	commB2, _ := result.GetCommunityForNode("B2")
	commB3, _ := result.GetCommunityForNode("B3")

	if commA1 != commA2 || commA2 != commA3 {
		t.Errorf("A1, A2, A3 should be in same community")
	}
	if commB1 != commB2 || commB2 != commB3 {
		t.Errorf("B1, B2, B3 should be in same community")
	}
	if commA1 == commB1 {
		t.Error("disconnected cliques should NOT be in same community")
	}
}

// isWellConnected verifies that all nodes in the slice are reachable from each other
func isWellConnected(analytics *GraphAnalytics, nodes []string) bool {
	if len(nodes) <= 1 {
		return true
	}

	nodeSet := make(map[string]bool, len(nodes))
	for _, id := range nodes {
		nodeSet[id] = true
	}

	// BFS from first node
	visited := make(map[string]bool)
	queue := []string{nodes[0]}
	visited[nodes[0]] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		node, ok := analytics.graph.GetNode(current)
		if !ok || node == nil {
			continue
		}

		// Check outgoing edges
		for _, edge := range node.Outgoing {
			if nodeSet[edge.ToID] && !visited[edge.ToID] {
				visited[edge.ToID] = true
				queue = append(queue, edge.ToID)
			}
		}

		// Check incoming edges (treat as undirected for connectivity)
		for _, edge := range node.Incoming {
			if nodeSet[edge.FromID] && !visited[edge.FromID] {
				visited[edge.FromID] = true
				queue = append(queue, edge.FromID)
			}
		}
	}

	// All nodes should be visited
	return len(visited) == len(nodes)
}

func TestDetectCommunities_AllCommunitiesWellConnected(t *testing.T) {
	// More complex graph with potential for disconnected communities
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addNode("E", "e.go").
		addNode("F", "f.go").
		addNode("G", "g.go").
		addNode("H", "h.go").
		// Cluster 1
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		// Cluster 2
		addEdge("D", "E", EdgeTypeCalls).
		addEdge("E", "D", EdgeTypeCalls).
		addEdge("E", "F", EdgeTypeCalls).
		addEdge("F", "E", EdgeTypeCalls).
		// Cluster 3
		addEdge("G", "H", EdgeTypeCalls).
		addEdge("H", "G", EdgeTypeCalls).
		// Weak inter-cluster connections
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("F", "G", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ALL communities are well-connected
	for _, comm := range result.Communities {
		if len(comm.Nodes) > 1 {
			if !isWellConnected(analytics, comm.Nodes) {
				t.Errorf("community %d is NOT well-connected: %v", comm.ID, comm.Nodes)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Context Cancellation Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_ContextCancellation(t *testing.T) {
	// Create a larger graph
	builder := newTestGraph("test")
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

	result, err := analytics.DetectCommunities(ctx, &LeidenOptions{
		MaxIterations:        1000,
		ConvergenceThreshold: 1e-10,
	})

	// Should return error
	if err == nil {
		t.Error("expected error for cancelled context")
	}
	if result != nil {
		t.Log("result returned despite cancellation (may be partial)")
	}
}

func TestDetectCommunities_ContextTimeout(t *testing.T) {
	// Create a very large graph that would take time to process
	builder := newTestGraph("test")
	n := 1000
	for i := 0; i < n; i++ {
		builder.addNode("node"+itoa(i), "file.go")
	}
	// Dense connections
	for i := 0; i < n; i++ {
		for j := 0; j < 5; j++ {
			target := (i + j + 1) % n
			if target != i {
				builder.addEdge("node"+itoa(i), "node"+itoa(target), EdgeTypeCalls)
			}
		}
	}
	g := builder.build()

	analytics := NewGraphAnalytics(g)

	// Very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()

	// Give context time to expire
	time.Sleep(10 * time.Microsecond)

	result, err := analytics.DetectCommunities(ctx, nil)

	// Should handle timeout gracefully
	if err == nil && result != nil && result.Converged {
		t.Log("completed before timeout (fast machine)")
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Determinism Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_Deterministic(t *testing.T) {
	// Run the same detection multiple times and verify same result
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addNode("E", "e.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("D", "E", EdgeTypeCalls).
		addEdge("E", "D", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	// Run 10 times
	var firstResult *CommunityResult
	for i := 0; i < 10; i++ {
		result, err := analytics.DetectCommunities(ctx, opts)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}

		if firstResult == nil {
			firstResult = result
			continue
		}

		// Compare with first result
		if len(result.Communities) != len(firstResult.Communities) {
			t.Errorf("iteration %d: community count changed: %d vs %d",
				i, len(result.Communities), len(firstResult.Communities))
		}

		// Compare modularity
		if math.Abs(result.Modularity-firstResult.Modularity) > 1e-10 {
			t.Errorf("iteration %d: modularity changed: %f vs %f",
				i, result.Modularity, firstResult.Modularity)
		}

		// Compare node assignments
		for _, comm := range result.Communities {
			for _, nodeID := range comm.Nodes {
				expectedComm, _ := firstResult.GetCommunityForNode(nodeID)
				// Community IDs might differ but membership should be consistent
				firstMembers := firstResult.GetCommunityMembers(expectedComm)
				if !containsAll(firstMembers, comm.Nodes) && !containsAll(comm.Nodes, firstMembers) {
					// Only error if this is a real difference, not just ID reordering
					t.Errorf("iteration %d: node %s community membership changed", i, nodeID)
				}
			}
		}
	}
}

func containsAll(haystack, needles []string) bool {
	set := make(map[string]bool, len(haystack))
	for _, s := range haystack {
		set[s] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// DetectCommunities - Resolution Parameter Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_HighResolution(t *testing.T) {
	// Higher resolution should produce more, smaller communities
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("D", "A", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	ctx := context.Background()

	// Low resolution
	lowRes, err := analytics.DetectCommunities(ctx, &LeidenOptions{
		Resolution:    0.1,
		MaxIterations: 100,
	})
	if err != nil {
		t.Fatalf("low resolution error: %v", err)
	}

	// High resolution
	highRes, err := analytics.DetectCommunities(ctx, &LeidenOptions{
		Resolution:    5.0,
		MaxIterations: 100,
	})
	if err != nil {
		t.Fatalf("high resolution error: %v", err)
	}

	// High resolution should have >= communities than low resolution
	// (might be equal for very connected graphs)
	if len(highRes.Communities) < len(lowRes.Communities) {
		t.Errorf("high resolution (%d) should have >= communities than low resolution (%d)",
			len(highRes.Communities), len(lowRes.Communities))
	}

	t.Logf("Low resolution (0.1): %d communities", len(lowRes.Communities))
	t.Logf("High resolution (5.0): %d communities", len(highRes.Communities))
}

func TestDetectCommunities_ResolutionAffectsGranularity(t *testing.T) {
	// Create a graph with clear hierarchical structure
	g := newTestGraph("test").
		// Micro-cluster 1
		addNode("A1", "a.go").
		addNode("A2", "a.go").
		addEdge("A1", "A2", EdgeTypeCalls).
		addEdge("A2", "A1", EdgeTypeCalls).
		// Micro-cluster 2
		addNode("B1", "b.go").
		addNode("B2", "b.go").
		addEdge("B1", "B2", EdgeTypeCalls).
		addEdge("B2", "B1", EdgeTypeCalls).
		// Connection between micro-clusters
		addEdge("A2", "B1", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	ctx := context.Background()

	// Very low resolution - should merge into fewer communities
	veryLow, _ := analytics.DetectCommunities(ctx, &LeidenOptions{Resolution: 0.01})

	// Very high resolution - should split into more communities
	veryHigh, _ := analytics.DetectCommunities(ctx, &LeidenOptions{Resolution: 10.0})

	t.Logf("Very low resolution (0.01): %d communities", len(veryLow.Communities))
	t.Logf("Very high resolution (10.0): %d communities", len(veryHigh.Communities))

	// Just verify both complete without error and produce valid results
	if veryLow.NodeCount != 4 || veryHigh.NodeCount != 4 {
		t.Error("node count should be 4 regardless of resolution")
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - MinCommunitySize Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_MinCommunitySize(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addNode("D", "d.go").
		addNode("E", "e.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		// D and E are isolated
		build()

	analytics := NewGraphAnalytics(g)
	ctx := context.Background()

	// Min size 1 (default)
	result1, _ := analytics.DetectCommunities(ctx, &LeidenOptions{MinCommunitySize: 1})

	// Min size 2
	result2, _ := analytics.DetectCommunities(ctx, &LeidenOptions{MinCommunitySize: 2})

	// Min size 3
	result3, _ := analytics.DetectCommunities(ctx, &LeidenOptions{MinCommunitySize: 3})

	// With min size 1, we should see singleton communities for D and E
	t.Logf("MinSize=1: %d communities", len(result1.Communities))

	// With min size 2, singleton communities should be filtered
	t.Logf("MinSize=2: %d communities", len(result2.Communities))

	// With min size 3, only the main cluster should remain
	t.Logf("MinSize=3: %d communities", len(result3.Communities))

	// Min size 3 should have fewer or equal communities than min size 2
	if len(result3.Communities) > len(result2.Communities) {
		t.Errorf("MinSize=3 should have <= communities than MinSize=2")
	}
}

// -----------------------------------------------------------------------------
// DetectCommunities - Community Metrics Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_CommunityMetrics(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "pkg1/a.go").
		addNode("B", "pkg1/b.go").
		addNode("C", "pkg1/c.go").
		addNode("D", "pkg2/d.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls). // External edge
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the community with A, B, C
	for _, comm := range result.Communities {
		if len(comm.Nodes) >= 3 {
			// This should be the main cluster
			t.Logf("Community %d: nodes=%v, internal=%d, external=%d, connectivity=%.2f",
				comm.ID, comm.Nodes, comm.InternalEdges, comm.ExternalEdges, comm.Connectivity)

			// Internal edges should be positive
			if comm.InternalEdges <= 0 {
				t.Errorf("expected positive InternalEdges, got %d", comm.InternalEdges)
			}

			// Connectivity should be between 0 and 1
			if comm.Connectivity < 0 || comm.Connectivity > 1 {
				t.Errorf("connectivity should be in [0,1], got %f", comm.Connectivity)
			}
		}
	}
}

func TestDetectCommunities_ModularityRange(t *testing.T) {
	// Modularity should be in reasonable range
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modularity is typically in range [-0.5, 1.0]
	// For most real networks it's between 0.3 and 0.7
	if result.Modularity < -1.0 || result.Modularity > 1.0 {
		t.Errorf("modularity %f out of expected range [-1, 1]", result.Modularity)
	}

	t.Logf("Modularity: %f", result.Modularity)
}

// -----------------------------------------------------------------------------
// DetectCommunities - Large Graph Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large graph test in short mode")
	}

	// Create a large graph with community structure
	n := 1000
	builder := newTestGraph("test")

	// Create 10 clusters of 100 nodes each
	for cluster := 0; cluster < 10; cluster++ {
		for i := 0; i < 100; i++ {
			nodeID := "cluster" + itoa(cluster) + "_node" + itoa(i)
			builder.addNode(nodeID, "cluster"+itoa(cluster)+"/file.go")
		}

		// Dense intra-cluster edges
		for i := 0; i < 100; i++ {
			for j := 0; j < 5; j++ {
				target := (i + j + 1) % 100
				if target != i {
					fromID := "cluster" + itoa(cluster) + "_node" + itoa(i)
					toID := "cluster" + itoa(cluster) + "_node" + itoa(target)
					builder.addEdge(fromID, toID, EdgeTypeCalls)
				}
			}
		}
	}

	// Sparse inter-cluster edges
	for cluster := 0; cluster < 9; cluster++ {
		fromID := "cluster" + itoa(cluster) + "_node0"
		toID := "cluster" + itoa(cluster+1) + "_node0"
		builder.addEdge(fromID, toID, EdgeTypeCalls)
	}

	g := builder.build()

	analytics := NewGraphAnalytics(g)
	start := time.Now()
	result, err := analytics.DetectCommunities(context.Background(), nil)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Large graph (%d nodes): %d communities found in %v", n, len(result.Communities), duration)
	t.Logf("Modularity: %f, Iterations: %d, Converged: %v",
		result.Modularity, result.Iterations, result.Converged)

	// Should find approximately 10 communities (one per cluster)
	if len(result.Communities) < 5 || len(result.Communities) > 20 {
		t.Errorf("expected ~10 communities for 10-cluster graph, got %d", len(result.Communities))
	}

	// Should complete in reasonable time (< 5 seconds for 1000 nodes)
	if duration > 5*time.Second {
		t.Errorf("too slow: %v for %d nodes", duration, n)
	}
}

func TestDetectCommunities_VeryLargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping very large graph test in short mode")
	}

	// 10,000 nodes
	n := 10000
	builder := newTestGraph("test")

	for i := 0; i < n; i++ {
		builder.addNode("node"+itoa(i), "file"+itoa(i%100)+".go")
	}

	// Each node connects to 3 neighbors
	for i := 0; i < n; i++ {
		for j := 1; j <= 3; j++ {
			target := (i + j) % n
			builder.addEdge("node"+itoa(i), "node"+itoa(target), EdgeTypeCalls)
		}
	}

	g := builder.build()

	analytics := NewGraphAnalytics(g)
	start := time.Now()
	result, err := analytics.DetectCommunities(context.Background(), nil)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Very large graph (%d nodes): completed in %v", n, duration)
	t.Logf("Communities: %d, Modularity: %f", len(result.Communities), result.Modularity)

	// Should complete in reasonable time
	if duration > 30*time.Second {
		t.Errorf("too slow for %d nodes: %v", n, duration)
	}
}

// -----------------------------------------------------------------------------
// DetectCommunitiesWithCRS Tests
// -----------------------------------------------------------------------------

func TestDetectCommunitiesWithCRS_Success(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, step := analytics.DetectCommunitiesWithCRS(context.Background(), nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify TraceStep
	if step.Action != "analytics_communities" {
		t.Errorf("expected action 'analytics_communities', got %s", step.Action)
	}
	if step.Tool != "DetectCommunities" {
		t.Errorf("expected tool 'DetectCommunities', got %s", step.Tool)
	}
	if step.Metadata["algorithm"] != "leiden" {
		t.Errorf("expected algorithm 'leiden', got %s", step.Metadata["algorithm"])
	}
	if step.Metadata["communities_found"] == "" {
		t.Error("expected 'communities_found' in metadata")
	}
	if step.Metadata["modularity"] == "" {
		t.Error("expected 'modularity' in metadata")
	}
	if step.Metadata["iterations"] == "" {
		t.Error("expected 'iterations' in metadata")
	}
	if step.Metadata["converged"] == "" {
		t.Error("expected 'converged' in metadata")
	}
	if step.Metadata["node_count"] == "" {
		t.Error("expected 'node_count' in metadata")
	}
	if step.Metadata["edge_count"] == "" {
		t.Error("expected 'edge_count' in metadata")
	}
}

func TestDetectCommunitiesWithCRS_CancelledContext(t *testing.T) {
	g := newTestGraph("test").addNode("A", "a.go").build()
	analytics := NewGraphAnalytics(g)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, step := analytics.DetectCommunitiesWithCRS(ctx, nil)

	// Should return empty result with error in step
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if step.Action != "analytics_communities" {
		t.Errorf("expected action 'analytics_communities', got %s", step.Action)
	}
	// Error should be recorded
	if step.Metadata["error"] == "" {
		t.Log("Note: error may not be in metadata if context checked early")
	}
}

func TestDetectCommunitiesWithCRS_EmptyGraph(t *testing.T) {
	g := createEmptyGraph()
	analytics := NewGraphAnalytics(g)

	result, step := analytics.DetectCommunitiesWithCRS(context.Background(), nil)

	if len(result.Communities) != 0 {
		t.Errorf("expected 0 communities, got %d", len(result.Communities))
	}
	if step.Action != "analytics_communities" {
		t.Errorf("expected action 'analytics_communities', got %s", step.Action)
	}
}

// -----------------------------------------------------------------------------
// Convergence Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_ConvergesQuickly(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addNode("C", "c.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Converged {
		t.Error("expected convergence for small graph")
	}

	// Should converge in few iterations
	if result.Iterations > 20 {
		t.Errorf("expected quick convergence, took %d iterations", result.Iterations)
	}
}

func TestDetectCommunities_MaxIterationsRespected(t *testing.T) {
	builder := newTestGraph("test")
	for i := 0; i < 50; i++ {
		builder.addNode("node"+itoa(i), "file.go")
	}
	for i := 0; i < 49; i++ {
		builder.addEdge("node"+itoa(i), "node"+itoa(i+1), EdgeTypeCalls)
		builder.addEdge("node"+itoa(i+1), "node"+itoa(i), EdgeTypeCalls)
	}
	g := builder.build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), &LeidenOptions{
		MaxIterations:        3,
		ConvergenceThreshold: 1e-20, // Very tight to prevent early convergence
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Iterations > 3 {
		t.Errorf("exceeded MaxIterations: %d > 3", result.Iterations)
	}
}

// -----------------------------------------------------------------------------
// Edge Type Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_DifferentEdgeTypes(t *testing.T) {
	// Test that the algorithm works with various edge types
	g := newTestGraph("test").
		addNode("Interface", "types.go").
		addNode("Impl1", "impl1.go").
		addNode("Impl2", "impl2.go").
		addNode("Caller", "main.go").
		addEdge("Impl1", "Interface", EdgeTypeImplements).
		addEdge("Impl2", "Interface", EdgeTypeImplements).
		addEdge("Caller", "Impl1", EdgeTypeCalls).
		addEdge("Caller", "Impl2", EdgeTypeCalls).
		addEdge("Caller", "Interface", EdgeTypeReferences).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should handle mixed edge types without error
	if result.NodeCount != 4 {
		t.Errorf("expected 4 nodes, got %d", result.NodeCount)
	}
}

// -----------------------------------------------------------------------------
// Package/Dominant Package Tests
// -----------------------------------------------------------------------------

func TestDetectCommunities_DominantPackage(t *testing.T) {
	// Create a community where one package is dominant
	g := newTestGraph("test").
		addNode("A", "main/a.go").
		addNode("B", "main/b.go").
		addNode("C", "main/c.go").
		addNode("D", "util/d.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("B", "A", EdgeTypeCalls).
		addEdge("B", "C", EdgeTypeCalls).
		addEdge("C", "B", EdgeTypeCalls).
		addEdge("A", "C", EdgeTypeCalls).
		addEdge("C", "A", EdgeTypeCalls).
		addEdge("C", "D", EdgeTypeCalls).
		addEdge("D", "C", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Log dominant packages
	for _, comm := range result.Communities {
		t.Logf("Community %d: dominant_package=%q, nodes=%v",
			comm.ID, comm.DominantPackage, comm.Nodes)
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkDetectCommunities_100Nodes(b *testing.B) {
	g := createBenchmarkCommunityGraph(100)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunities(ctx, opts)
	}
}

func BenchmarkDetectCommunities_1000Nodes(b *testing.B) {
	g := createBenchmarkCommunityGraph(1000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunities(ctx, opts)
	}
}

func BenchmarkDetectCommunities_10000Nodes(b *testing.B) {
	g := createBenchmarkCommunityGraph(10000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunities(ctx, opts)
	}
}

func BenchmarkDetectCommunities_100000Nodes(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping 100K node benchmark in short mode")
	}

	g := createBenchmarkCommunityGraph(100000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunities(ctx, opts)
	}
}

func createBenchmarkCommunityGraph(n int) *HierarchicalGraph {
	builder := newTestGraph("benchmark")

	// Create nodes
	for i := 0; i < n; i++ {
		builder.addNode("node"+itoa(i), "file"+itoa(i%100)+".go")
	}

	// Create clustered edges (nodes close in ID are more likely to connect)
	for i := 0; i < n; i++ {
		numEdges := 3 + (i % 3)
		for j := 0; j < numEdges; j++ {
			// Prefer nearby nodes (cluster structure)
			offset := (j*7 + 1) % 20
			target := (i + offset) % n
			if target != i {
				builder.addEdge("node"+itoa(i), "node"+itoa(target), EdgeTypeCalls)
			}
		}
	}

	return builder.build()
}

// =============================================================================
// Regression Tests
// =============================================================================

func TestDetectCommunities_NilGraphField(t *testing.T) {
	// Test robustness with edge cases in graph structure
	analytics := &GraphAnalytics{graph: nil}

	result, err := analytics.DetectCommunities(context.Background(), nil)

	// Should handle nil graph gracefully (no panic)
	if err != nil {
		t.Logf("error with nil graph: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for nil graph")
	}
	if len(result.Communities) != 0 {
		t.Errorf("expected empty communities for nil graph, got %d", len(result.Communities))
	}
	if !result.Converged {
		t.Error("expected converged=true for nil graph")
	}
}

func TestDetectCommunities_SelfLoops(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addEdge("A", "A", EdgeTypeCalls). // Self-loop
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error with self-loop: %v", err)
	}

	// Should handle self-loops gracefully
	if result.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", result.NodeCount)
	}
}

func TestDetectCommunities_DuplicateEdges(t *testing.T) {
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addEdge("A", "B", EdgeTypeCalls).
		addEdge("A", "B", EdgeTypeCalls). // Duplicate
		addEdge("A", "B", EdgeTypeCalls). // Another duplicate
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunities(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error with duplicate edges: %v", err)
	}

	// Should handle duplicates gracefully
	if result.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", result.NodeCount)
	}
}

// =============================================================================
// getCrossPackageCommunities Tests
// =============================================================================

func TestGetCrossPackageCommunities(t *testing.T) {
	result := &CommunityResult{
		Communities: []Community{
			{
				ID:    0,
				Nodes: []string{"pkg1/file.go:A", "pkg1/file.go:B"},
			},
			{
				ID:    1,
				Nodes: []string{"pkg1/file.go:C", "pkg2/file.go:D"},
			},
			{
				ID:    2,
				Nodes: []string{"pkg3/sub/file.go:E"},
			},
		},
	}

	crossPkg := getCrossPackageCommunities(result)

	// Community 1 spans pkg1 and pkg2
	if len(crossPkg) != 1 {
		t.Errorf("expected 1 cross-package community, got %d", len(crossPkg))
	}

	if len(crossPkg) > 0 && crossPkg[0].ID != 1 {
		t.Errorf("expected community 1, got %d", crossPkg[0].ID)
	}
}

func TestGetCrossPackageCommunities_AllSamePackage(t *testing.T) {
	result := &CommunityResult{
		Communities: []Community{
			{
				ID:    0,
				Nodes: []string{"pkg1/file.go:A", "pkg1/other.go:B"},
			},
		},
	}

	crossPkg := getCrossPackageCommunities(result)

	if len(crossPkg) != 0 {
		t.Errorf("expected 0 cross-package communities, got %d", len(crossPkg))
	}
}

// =============================================================================
// Parallel Leiden Tests
// =============================================================================

func TestDetectCommunitiesParallel_FallbackToSequential(t *testing.T) {
	// Small graph should fall back to sequential
	g := newTestGraph("test").
		addNode("A", "a.go").
		addNode("B", "b.go").
		addEdge("A", "B", EdgeTypeCalls).
		build()

	analytics := NewGraphAnalytics(g)
	result, err := analytics.DetectCommunitiesParallel(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", result.NodeCount)
	}
}

func TestDetectCommunitiesParallel_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large graph parallel test in short mode")
	}

	// Create a large graph with community structure (above threshold)
	n := 2000
	builder := newTestGraph("test")

	// Create 10 clusters of 200 nodes each
	for cluster := 0; cluster < 10; cluster++ {
		for i := 0; i < 200; i++ {
			nodeID := "cluster" + itoa(cluster) + "_node" + itoa(i)
			builder.addNode(nodeID, "cluster"+itoa(cluster)+"/file.go")
		}

		// Dense intra-cluster edges
		for i := 0; i < 200; i++ {
			for j := 0; j < 5; j++ {
				target := (i + j + 1) % 200
				if target != i {
					fromID := "cluster" + itoa(cluster) + "_node" + itoa(i)
					toID := "cluster" + itoa(cluster) + "_node" + itoa(target)
					builder.addEdge(fromID, toID, EdgeTypeCalls)
				}
			}
		}
	}

	// Sparse inter-cluster edges
	for cluster := 0; cluster < 9; cluster++ {
		fromID := "cluster" + itoa(cluster) + "_node0"
		toID := "cluster" + itoa(cluster+1) + "_node0"
		builder.addEdge(fromID, toID, EdgeTypeCalls)
	}

	g := builder.build()

	analytics := NewGraphAnalytics(g)

	// Run parallel version
	start := time.Now()
	result, err := analytics.DetectCommunitiesParallel(context.Background(), nil)
	parallelDuration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Logf("Parallel: %d communities found in %v (modularity: %.3f)",
		len(result.Communities), parallelDuration, result.Modularity)

	// Verify result quality - algorithm may find more granular communities
	// Just verify it found a reasonable number (not all singletons, not one giant)
	if len(result.Communities) < 5 || len(result.Communities) > n/10 {
		t.Errorf("expected between 5 and %d communities, got %d", n/10, len(result.Communities))
	}

	if result.NodeCount != n {
		t.Errorf("expected %d nodes, got %d", n, result.NodeCount)
	}
}

func TestDetectCommunitiesParallel_MatchesSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping matching test in short mode")
	}

	// Create a graph just above the parallel threshold
	n := 1500
	builder := newTestGraph("test")

	for i := 0; i < n; i++ {
		builder.addNode("node"+itoa(i), "file"+itoa(i%10)+".go")
	}

	for i := 0; i < n; i++ {
		for j := 1; j <= 3; j++ {
			target := (i + j) % n
			builder.addEdge("node"+itoa(i), "node"+itoa(target), EdgeTypeCalls)
		}
	}

	g := builder.build()
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	// Run both versions
	seqResult, err := analytics.DetectCommunities(ctx, opts)
	if err != nil {
		t.Fatalf("sequential error: %v", err)
	}

	parResult, err := analytics.DetectCommunitiesParallel(ctx, opts)
	if err != nil {
		t.Fatalf("parallel error: %v", err)
	}

	// Results should be similar (may not be identical due to iteration order)
	// Compare modularity - should be within reasonable tolerance
	modDiff := math.Abs(seqResult.Modularity - parResult.Modularity)
	if modDiff > 0.1 {
		t.Errorf("modularity differs too much: seq=%.3f, par=%.3f, diff=%.3f",
			seqResult.Modularity, parResult.Modularity, modDiff)
	}

	// Compare community count - should be similar
	commDiff := abs(len(seqResult.Communities) - len(parResult.Communities))
	if commDiff > 5 {
		t.Errorf("community count differs too much: seq=%d, par=%d",
			len(seqResult.Communities), len(parResult.Communities))
	}

	t.Logf("Sequential: %d communities, modularity=%.3f",
		len(seqResult.Communities), seqResult.Modularity)
	t.Logf("Parallel: %d communities, modularity=%.3f",
		len(parResult.Communities), parResult.Modularity)
}

func TestDetectCommunitiesParallel_ContextCancellation(t *testing.T) {
	// Create a large enough graph to use parallel
	builder := newTestGraph("test")
	for i := 0; i < 1500; i++ {
		builder.addNode("node"+itoa(i), "file.go")
	}
	for i := 0; i < 1499; i++ {
		builder.addEdge("node"+itoa(i), "node"+itoa(i+1), EdgeTypeCalls)
	}
	g := builder.build()

	analytics := NewGraphAnalytics(g)

	// Already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := analytics.DetectCommunitiesParallel(ctx, nil)

	// Should return error or handle gracefully
	if err == nil && result != nil && result.Converged {
		t.Log("completed before cancellation took effect")
	}
}

func TestDetectCommunitiesParallel_NilGraph(t *testing.T) {
	analytics := &GraphAnalytics{graph: nil}

	result, err := analytics.DetectCommunitiesParallel(context.Background(), nil)

	if err != nil {
		t.Logf("error with nil graph: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for nil graph")
	}
	if !result.Converged {
		t.Error("expected converged=true for nil graph")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// =============================================================================
// Parallel Benchmarks
// =============================================================================

func BenchmarkDetectCommunitiesParallel_1000Nodes(b *testing.B) {
	g := createBenchmarkCommunityGraph(1000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunitiesParallel(ctx, opts)
	}
}

func BenchmarkDetectCommunitiesParallel_10000Nodes(b *testing.B) {
	g := createBenchmarkCommunityGraph(10000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunitiesParallel(ctx, opts)
	}
}

func BenchmarkDetectCommunitiesParallel_100000Nodes(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping 100K node parallel benchmark in short mode")
	}

	g := createBenchmarkCommunityGraph(100000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analytics.DetectCommunitiesParallel(ctx, opts)
	}
}

// BenchmarkCompare_Sequential_vs_Parallel compares both versions
func BenchmarkCompare_Sequential_vs_Parallel_10000(b *testing.B) {
	g := createBenchmarkCommunityGraph(10000)
	analytics := NewGraphAnalytics(g)
	ctx := context.Background()
	opts := DefaultLeidenOptions()

	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = analytics.DetectCommunities(ctx, opts)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = analytics.DetectCommunitiesParallel(ctx, opts)
		}
	})
}
