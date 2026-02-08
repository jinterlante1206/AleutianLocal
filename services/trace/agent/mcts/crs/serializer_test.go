// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package crs

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// mockProofIndexView implements ProofIndexView for testing.
type mockProofIndexView struct {
	data map[string]ProofNumber
}

func (m *mockProofIndexView) Get(nodeID string) (ProofNumber, bool) {
	p, ok := m.data[nodeID]
	return p, ok
}

func (m *mockProofIndexView) All() map[string]ProofNumber {
	result := make(map[string]ProofNumber, len(m.data))
	for k, v := range m.data {
		result[k] = v
	}
	return result
}

func (m *mockProofIndexView) Size() int {
	return len(m.data)
}

// mockConstraintIndexView implements ConstraintIndexView for testing.
type mockConstraintIndexView struct {
	data map[string]Constraint
}

func (m *mockConstraintIndexView) Get(constraintID string) (Constraint, bool) {
	c, ok := m.data[constraintID]
	return c, ok
}

func (m *mockConstraintIndexView) FindByType(constraintType ConstraintType) []Constraint {
	return nil
}

func (m *mockConstraintIndexView) FindByNode(nodeID string) []Constraint {
	return nil
}

func (m *mockConstraintIndexView) All() map[string]Constraint {
	result := make(map[string]Constraint, len(m.data))
	for k, v := range m.data {
		result[k] = v
	}
	return result
}

func (m *mockConstraintIndexView) Size() int {
	return len(m.data)
}

// CRS-04: Clause methods
func (m *mockConstraintIndexView) GetClause(clauseID string) (*Clause, bool) {
	return nil, false
}

func (m *mockConstraintIndexView) AllClauses() map[string]*Clause {
	return make(map[string]*Clause)
}

func (m *mockConstraintIndexView) ClauseCount() int {
	return 0
}

func (m *mockConstraintIndexView) CheckAssignment(assignment map[string]bool) ClauseCheckResult {
	return ClauseCheckResult{}
}

// mockSimilarityIndexView implements SimilarityIndexView for testing.
type mockSimilarityIndexView struct {
	pairCount int
	pairs     map[string]map[string]float64
}

func (m *mockSimilarityIndexView) Distance(node1, node2 string) (float64, bool) {
	if m.pairs != nil {
		if inner, ok := m.pairs[node1]; ok {
			if dist, ok := inner[node2]; ok {
				return dist, true
			}
		}
	}
	return 0, false
}

func (m *mockSimilarityIndexView) NearestNeighbors(nodeID string, k int) []SimilarityMatch {
	return nil
}

func (m *mockSimilarityIndexView) Size() int {
	return m.pairCount
}

func (m *mockSimilarityIndexView) AllPairs() map[string]map[string]float64 {
	if m.pairs == nil {
		return make(map[string]map[string]float64)
	}
	// Return deep copy
	result := make(map[string]map[string]float64, len(m.pairs))
	for k, v := range m.pairs {
		result[k] = make(map[string]float64, len(v))
		for k2, v2 := range v {
			result[k][k2] = v2
		}
	}
	return result
}

func (m *mockSimilarityIndexView) AllPairsFiltered(maxPairs int) ([]SimilarityPairData, bool) {
	if m.pairs == nil {
		return make([]SimilarityPairData, 0), false
	}

	result := make([]SimilarityPairData, 0)
	pairCount := 0
	truncated := false

	for fromID, inner := range m.pairs {
		for toID, similarity := range inner {
			if fromID < toID {
				if maxPairs > 0 && pairCount >= maxPairs {
					truncated = true
					break
				}
				result = append(result, SimilarityPairData{
					FromID:     fromID,
					ToID:       toID,
					Similarity: similarity,
				})
				pairCount++
			}
		}
		if truncated {
			break
		}
	}
	return result, truncated
}

// mockDependencyIndexView implements DependencyIndexView for testing.
type mockDependencyIndexView struct {
	edgeCount   int
	edges       map[string][]string
	graphBacked bool
}

func (m *mockDependencyIndexView) DependsOn(nodeID string) []string {
	if m.edges != nil {
		return m.edges[nodeID]
	}
	return nil
}

func (m *mockDependencyIndexView) DependedBy(nodeID string) []string {
	return nil
}

func (m *mockDependencyIndexView) HasCycle(nodeID string) bool {
	return false
}

func (m *mockDependencyIndexView) Size() int {
	return m.edgeCount
}

func (m *mockDependencyIndexView) AllEdges() map[string][]string {
	if m.graphBacked {
		return nil
	}
	if m.edges == nil {
		return make(map[string][]string)
	}
	// Return deep copy
	result := make(map[string][]string, len(m.edges))
	for k, v := range m.edges {
		result[k] = make([]string, len(v))
		copy(result[k], v)
	}
	return result
}

func (m *mockDependencyIndexView) IsGraphBacked() bool {
	return m.graphBacked
}

// mockHistoryIndexView implements HistoryIndexView for testing.
type mockHistoryIndexView struct {
	entries []HistoryEntry
}

func (m *mockHistoryIndexView) Trace(nodeID string) []HistoryEntry {
	return nil
}

func (m *mockHistoryIndexView) Recent(n int) []HistoryEntry {
	if n >= len(m.entries) {
		return m.entries
	}
	return m.entries[len(m.entries)-n:]
}

func (m *mockHistoryIndexView) Size() int {
	return len(m.entries)
}

// mockStreamingIndexView implements StreamingIndexView for testing.
type mockStreamingIndexView struct {
	cardinality uint64
	size        int
}

func (m *mockStreamingIndexView) Estimate(item string) uint64 {
	return 0
}

func (m *mockStreamingIndexView) Cardinality() uint64 {
	return m.cardinality
}

func (m *mockStreamingIndexView) Size() int {
	return m.size
}

// mockSnapshot implements Snapshot for testing.
type mockSnapshot struct {
	generation int64
	createdAt  int64 // Unix milliseconds UTC
	proof      ProofIndexView
	constraint ConstraintIndexView
	similarity SimilarityIndexView
	dependency DependencyIndexView
	history    HistoryIndexView
	streaming  StreamingIndexView
}

func (m *mockSnapshot) Generation() int64                    { return m.generation }
func (m *mockSnapshot) CreatedAt() int64                     { return m.createdAt }
func (m *mockSnapshot) ProofIndex() ProofIndexView           { return m.proof }
func (m *mockSnapshot) ConstraintIndex() ConstraintIndexView { return m.constraint }
func (m *mockSnapshot) SimilarityIndex() SimilarityIndexView { return m.similarity }
func (m *mockSnapshot) DependencyIndex() DependencyIndexView { return m.dependency }
func (m *mockSnapshot) HistoryIndex() HistoryIndexView       { return m.history }
func (m *mockSnapshot) StreamingIndex() StreamingIndexView   { return m.streaming }
func (m *mockSnapshot) Query() QueryAPI                      { return nil }
func (m *mockSnapshot) GraphQuery() GraphQuery               { return nil }

// GR-31: Analytics methods
func (m *mockSnapshot) AnalyticsHistory() []*AnalyticsRecord              { return nil }
func (m *mockSnapshot) LastAnalytics(AnalyticsQueryType) *AnalyticsRecord { return nil }
func (m *mockSnapshot) HasRunAnalytics(AnalyticsQueryType) bool           { return false }

func TestNewSerializer(t *testing.T) {
	t.Run("creates serializer with nil logger", func(t *testing.T) {
		s := NewSerializer(nil)
		if s == nil {
			t.Fatal("Expected non-nil serializer")
		}
		if s.logger == nil {
			t.Error("Expected default logger to be set")
		}
	})
}

func TestSerializer_Export_NilSnapshot(t *testing.T) {
	s := NewSerializer(nil)

	export := s.Export(nil, "test-session")

	if export == nil {
		t.Fatal("Expected non-nil export")
	}
	if export.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", export.SessionID, "test-session")
	}
	if export.Generation != 0 {
		t.Errorf("Generation = %d, want 0", export.Generation)
	}
	if export.Summary.NodesExplored != 0 {
		t.Errorf("NodesExplored = %d, want 0", export.Summary.NodesExplored)
	}
}

func TestSerializer_Export_WithData(t *testing.T) {
	s := NewSerializer(nil)

	nowMillis := time.Now().UnixMilli()
	snapshot := &mockSnapshot{
		generation: 42,
		createdAt:  nowMillis,
		proof: &mockProofIndexView{
			data: map[string]ProofNumber{
				"node1": {Proof: 0, Disproof: 1000, Status: ProofStatusProven, Source: SignalSourceHard, UpdatedAt: nowMillis},
				"node2": {Proof: 1000, Disproof: 0, Status: ProofStatusDisproven, Source: SignalSourceSoft, UpdatedAt: nowMillis},
				"node3": {Proof: 100, Disproof: 100, Status: ProofStatusUnknown, Source: SignalSourceUnknown, UpdatedAt: nowMillis},
			},
		},
		constraint: &mockConstraintIndexView{
			data: map[string]Constraint{
				"c1": {ID: "c1", Type: ConstraintTypeMutualExclusion, Nodes: []string{"node1", "node2"}, Active: true, Source: SignalSourceHard, CreatedAt: nowMillis},
			},
		},
		similarity: &mockSimilarityIndexView{pairCount: 5},
		dependency: &mockDependencyIndexView{edgeCount: 10},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{
				{ID: "h1", NodeID: "node1", Action: "explore", Result: "success", Source: SignalSourceHard, Timestamp: nowMillis, Metadata: map[string]string{"key": "value"}},
				{ID: "h2", NodeID: "node2", Action: "analyze", Result: "found_issue", Source: SignalSourceSoft, Timestamp: nowMillis},
			},
		},
		streaming: &mockStreamingIndexView{cardinality: 100, size: 1024},
	}

	export := s.Export(snapshot, "test-session-with-data")

	// Verify basic fields
	if export.SessionID != "test-session-with-data" {
		t.Errorf("SessionID = %q, want %q", export.SessionID, "test-session-with-data")
	}
	if export.Generation != 42 {
		t.Errorf("Generation = %d, want 42", export.Generation)
	}

	// Verify proof index export
	if len(export.Indexes.Proof.Entries) != 3 {
		t.Errorf("Proof entries = %d, want 3", len(export.Indexes.Proof.Entries))
	}

	// Verify constraint export
	if len(export.Indexes.Constraint.Constraints) != 1 {
		t.Errorf("Constraints = %d, want 1", len(export.Indexes.Constraint.Constraints))
	}
	if export.Indexes.Constraint.Constraints[0].Type != "mutual_exclusion" {
		t.Errorf("Constraint type = %q, want %q", export.Indexes.Constraint.Constraints[0].Type, "mutual_exclusion")
	}

	// Verify similarity export
	if export.Indexes.Similarity.PairCount != 5 {
		t.Errorf("Similarity pair count = %d, want 5", export.Indexes.Similarity.PairCount)
	}

	// Verify dependency export
	if export.Indexes.Dependency.EdgeCount != 10 {
		t.Errorf("Dependency edge count = %d, want 10", export.Indexes.Dependency.EdgeCount)
	}

	// Verify history export
	if export.Indexes.History.EntryCount != 2 {
		t.Errorf("History entry count = %d, want 2", export.Indexes.History.EntryCount)
	}
	if len(export.Indexes.History.RecentEntries) != 2 {
		t.Errorf("Recent entries = %d, want 2", len(export.Indexes.History.RecentEntries))
	}
	if export.Indexes.History.RecentEntries[0].Metadata["key"] != "value" {
		t.Error("Expected metadata to be preserved")
	}

	// Verify streaming export
	if export.Indexes.Streaming.Cardinality != 100 {
		t.Errorf("Cardinality = %d, want 100", export.Indexes.Streaming.Cardinality)
	}
	if export.Indexes.Streaming.ApproximateBytes != 1024 {
		t.Errorf("ApproximateBytes = %d, want 1024", export.Indexes.Streaming.ApproximateBytes)
	}

	// Verify summary
	if export.Summary.NodesExplored != 3 {
		t.Errorf("NodesExplored = %d, want 3", export.Summary.NodesExplored)
	}
	if export.Summary.NodesProven != 1 {
		t.Errorf("NodesProven = %d, want 1", export.Summary.NodesProven)
	}
	if export.Summary.NodesDisproven != 1 {
		t.Errorf("NodesDisproven = %d, want 1", export.Summary.NodesDisproven)
	}
	if export.Summary.NodesUnknown != 1 {
		t.Errorf("NodesUnknown = %d, want 1", export.Summary.NodesUnknown)
	}
	if export.Summary.ConstraintsApplied != 1 {
		t.Errorf("ConstraintsApplied = %d, want 1", export.Summary.ConstraintsApplied)
	}
	if export.Summary.ExplorationDepth != 2 {
		t.Errorf("ExplorationDepth = %d, want 2", export.Summary.ExplorationDepth)
	}

	// Confidence score should be 1/3 (one proven out of three explored)
	expectedConfidence := 1.0 / 3.0
	if export.Summary.ConfidenceScore < expectedConfidence-0.01 || export.Summary.ConfidenceScore > expectedConfidence+0.01 {
		t.Errorf("ConfidenceScore = %f, want ~%f", export.Summary.ConfidenceScore, expectedConfidence)
	}
}

func TestSerializer_ExportSummaryOnly(t *testing.T) {
	s := NewSerializer(nil)

	t.Run("nil snapshot returns empty summary", func(t *testing.T) {
		summary := s.ExportSummaryOnly(nil)
		if summary.NodesExplored != 0 {
			t.Errorf("NodesExplored = %d, want 0", summary.NodesExplored)
		}
	})

	t.Run("computes summary from snapshot", func(t *testing.T) {
		snapshot := &mockSnapshot{
			proof: &mockProofIndexView{
				data: map[string]ProofNumber{
					"node1": {Status: ProofStatusProven},
					"node2": {Status: ProofStatusProven},
					"node3": {Status: ProofStatusDisproven},
				},
			},
			constraint: &mockConstraintIndexView{
				data: map[string]Constraint{
					"c1": {},
					"c2": {},
				},
			},
			similarity: &mockSimilarityIndexView{},
			dependency: &mockDependencyIndexView{},
			history:    &mockHistoryIndexView{entries: make([]HistoryEntry, 10)},
			streaming:  &mockStreamingIndexView{},
		}

		summary := s.ExportSummaryOnly(snapshot)

		if summary.NodesExplored != 3 {
			t.Errorf("NodesExplored = %d, want 3", summary.NodesExplored)
		}
		if summary.NodesProven != 2 {
			t.Errorf("NodesProven = %d, want 2", summary.NodesProven)
		}
		if summary.NodesDisproven != 1 {
			t.Errorf("NodesDisproven = %d, want 1", summary.NodesDisproven)
		}
		if summary.ConstraintsApplied != 2 {
			t.Errorf("ConstraintsApplied = %d, want 2", summary.ConstraintsApplied)
		}
		if summary.ExplorationDepth != 10 {
			t.Errorf("ExplorationDepth = %d, want 10", summary.ExplorationDepth)
		}

		expectedConfidence := 2.0 / 3.0
		if summary.ConfidenceScore < expectedConfidence-0.01 || summary.ConfidenceScore > expectedConfidence+0.01 {
			t.Errorf("ConfidenceScore = %f, want ~%f", summary.ConfidenceScore, expectedConfidence)
		}
	})
}

func TestSerializer_Export_JSONSerializable(t *testing.T) {
	s := NewSerializer(nil)

	nowMillis := time.Now().UnixMilli()
	snapshot := &mockSnapshot{
		generation: 1,
		proof: &mockProofIndexView{
			data: map[string]ProofNumber{
				"node1": {Proof: 0, Disproof: 1000, Status: ProofStatusProven, Source: SignalSourceHard, UpdatedAt: nowMillis},
			},
		},
		constraint: &mockConstraintIndexView{
			data: map[string]Constraint{
				"c1": {ID: "c1", Type: ConstraintTypeImplication, Nodes: []string{"a", "b"}, Active: true, Source: SignalSourceSoft, CreatedAt: nowMillis},
			},
		},
		similarity: &mockSimilarityIndexView{pairCount: 1},
		dependency: &mockDependencyIndexView{edgeCount: 1},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{{ID: "h1", NodeID: "node1", Action: "test", Timestamp: nowMillis}},
		},
		streaming: &mockStreamingIndexView{cardinality: 1, size: 100},
	}

	export := s.Export(snapshot, "json-test")

	// Verify it can be marshaled to JSON
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("Failed to marshal export to JSON: %v", err)
	}

	// Verify it can be unmarshaled back
	var parsed CRSExport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Verify round-trip preserved key fields
	if parsed.SessionID != export.SessionID {
		t.Errorf("SessionID mismatch after round-trip")
	}
	if parsed.Generation != export.Generation {
		t.Errorf("Generation mismatch after round-trip")
	}
	if len(parsed.Indexes.Proof.Entries) != len(export.Indexes.Proof.Entries) {
		t.Errorf("Proof entries mismatch after round-trip")
	}
	if parsed.Summary.NodesExplored != export.Summary.NodesExplored {
		t.Errorf("Summary mismatch after round-trip")
	}
}

func TestSerializer_Export_NilIndexViews(t *testing.T) {
	s := NewSerializer(nil)

	// Snapshot with all nil index views
	snapshot := &mockSnapshot{
		generation: 1,
		proof:      nil,
		constraint: nil,
		similarity: nil,
		dependency: nil,
		history:    nil,
		streaming:  nil,
	}

	// Should not panic
	export := s.Export(snapshot, "nil-indexes-test")

	if len(export.Indexes.Proof.Entries) != 0 {
		t.Errorf("Expected empty proof entries for nil index")
	}
	if len(export.Indexes.Constraint.Constraints) != 0 {
		t.Errorf("Expected empty constraints for nil index")
	}
	if export.Summary.NodesExplored != 0 {
		t.Errorf("Expected zero nodes explored for nil indexes")
	}
}

func TestComputeSummary_ZeroNodesExplored(t *testing.T) {
	s := NewSerializer(nil)

	snapshot := &mockSnapshot{
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{},
		dependency: &mockDependencyIndexView{},
		history:    &mockHistoryIndexView{},
		streaming:  &mockStreamingIndexView{},
	}

	summary := s.ComputeSummary(snapshot)

	// ConfidenceScore should be 0 when no nodes explored (not NaN or Inf)
	if summary.ConfidenceScore != 0 {
		t.Errorf("ConfidenceScore = %f, want 0 for zero nodes explored", summary.ConfidenceScore)
	}
}

// -----------------------------------------------------------------------------
// GR-34: Full Index Export/Import Tests
// -----------------------------------------------------------------------------

func TestSerializer_ExportFull_WithSimilarityPairs(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	nowMillis := time.Now().UnixMilli()
	snapshot := &mockSnapshot{
		generation: 10,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{
			pairCount: 2, // Size() returns this
			pairs: map[string]map[string]float64{
				"nodeA": {"nodeB": 0.8, "nodeC": 0.3},
				"nodeB": {"nodeA": 0.8}, // Reverse direction (shouldn't be duplicated in export)
			},
		},
		dependency: &mockDependencyIndexView{edgeCount: 0},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{
				{ID: "h1", NodeID: "nodeA", Action: "test", Timestamp: nowMillis},
			},
		},
		streaming: &mockStreamingIndexView{},
	}

	result, err := s.ExportFull(ctx, snapshot, "test-full", nil)
	if err != nil {
		t.Fatalf("ExportFull failed: %v", err)
	}

	// Verify similarity pairs are exported
	if len(result.Export.Indexes.Similarity.Pairs) != 2 {
		t.Errorf("Similarity pairs = %d, want 2", len(result.Export.Indexes.Similarity.Pairs))
	}

	// Verify only one direction is exported (fromID < toID)
	for _, pair := range result.Export.Indexes.Similarity.Pairs {
		if pair.FromID >= pair.ToID {
			t.Errorf("Pair exported in wrong direction: from=%s, to=%s", pair.FromID, pair.ToID)
		}
	}

	// Verify truncation not set
	if result.Truncated {
		t.Error("Export should not be truncated")
	}
}

func TestSerializer_ExportFull_WithDependencyEdges(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	snapshot := &mockSnapshot{
		generation: 10,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{pairCount: 0},
		dependency: &mockDependencyIndexView{
			edgeCount: 3,
			edges: map[string][]string{
				"funcA": {"funcB", "funcC"},
				"funcB": {"funcD"},
			},
			graphBacked: false,
		},
		history:   &mockHistoryIndexView{},
		streaming: &mockStreamingIndexView{},
	}

	result, err := s.ExportFull(ctx, snapshot, "test-edges", nil)
	if err != nil {
		t.Fatalf("ExportFull failed: %v", err)
	}

	// Verify edges are exported
	if len(result.Export.Indexes.Dependency.Edges) != 3 {
		t.Errorf("Dependency edges = %d, want 3", len(result.Export.Indexes.Dependency.Edges))
	}

	// Verify source is legacy
	if result.Export.Indexes.Dependency.Source != "legacy" {
		t.Errorf("Dependency source = %q, want %q", result.Export.Indexes.Dependency.Source, "legacy")
	}
}

func TestSerializer_ExportFull_GraphBackedDependency(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	snapshot := &mockSnapshot{
		generation: 10,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{pairCount: 0},
		dependency: &mockDependencyIndexView{
			edgeCount:   100,
			edges:       nil, // Edges in graph
			graphBacked: true,
		},
		history:   &mockHistoryIndexView{},
		streaming: &mockStreamingIndexView{},
	}

	result, err := s.ExportFull(ctx, snapshot, "test-graph-backed", nil)
	if err != nil {
		t.Fatalf("ExportFull failed: %v", err)
	}

	// Verify edges are NOT exported for graph-backed
	if len(result.Export.Indexes.Dependency.Edges) != 0 {
		t.Errorf("Graph-backed should not export edges, got %d", len(result.Export.Indexes.Dependency.Edges))
	}

	// Verify source is graph_backed
	if result.Export.Indexes.Dependency.Source != "graph_backed" {
		t.Errorf("Dependency source = %q, want %q", result.Export.Indexes.Dependency.Source, "graph_backed")
	}

	// EdgeCount should still be populated
	if result.Export.Indexes.Dependency.EdgeCount != 100 {
		t.Errorf("EdgeCount = %d, want 100", result.Export.Indexes.Dependency.EdgeCount)
	}
}

func TestSerializer_ExportFull_Truncation(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	// Create large dataset that will be truncated
	pairs := make(map[string]map[string]float64)
	for i := 0; i < 150; i++ {
		fromID := fmt.Sprintf("node%d", i)
		pairs[fromID] = make(map[string]float64)
		for j := i + 1; j < i+10 && j < 150; j++ {
			toID := fmt.Sprintf("node%d", j)
			pairs[fromID][toID] = 0.5
		}
	}

	snapshot := &mockSnapshot{
		generation: 1,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{
			pairCount: 1000, // Large count
			pairs:     pairs,
		},
		dependency: &mockDependencyIndexView{edgeCount: 0},
		history:    &mockHistoryIndexView{},
		streaming:  &mockStreamingIndexView{},
	}

	// Set low limit for truncation
	opts := &ExportOptions{MaxSimilarityPairs: 10}
	result, err := s.ExportFull(ctx, snapshot, "test-truncation", opts)
	if err != nil {
		t.Fatalf("ExportFull failed: %v", err)
	}

	// Verify truncation occurred
	if !result.Truncated {
		t.Error("Export should be truncated")
	}
	if len(result.Export.Indexes.Similarity.Pairs) > 10 {
		t.Errorf("Pairs should be capped at 10, got %d", len(result.Export.Indexes.Similarity.Pairs))
	}
	if !result.Export.Indexes.Similarity.Truncated {
		t.Error("Similarity.Truncated should be true")
	}
	if len(result.Warnings) == 0 {
		t.Error("Expected warning about truncation")
	}
}

func TestSerializer_ExportFull_ContextCancellation(t *testing.T) {
	s := NewSerializer(nil)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot := &mockSnapshot{
		generation: 1,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{pairCount: 0},
		dependency: &mockDependencyIndexView{edgeCount: 0},
		history:    &mockHistoryIndexView{},
		streaming:  &mockStreamingIndexView{},
	}

	_, err := s.ExportFull(ctx, snapshot, "test-cancel", nil)
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestSerializer_ExportFull_NilContext(t *testing.T) {
	s := NewSerializer(nil)

	snapshot := &mockSnapshot{}

	_, err := s.ExportFull(nil, snapshot, "test-nil-ctx", nil)
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestSerializer_Import_Roundtrip(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	nowMillis := time.Now().UnixMilli()
	snapshot := &mockSnapshot{
		generation: 42,
		proof: &mockProofIndexView{
			data: map[string]ProofNumber{
				"node1": {Proof: 10, Disproof: 20, Status: ProofStatusExpanded, Source: SignalSourceHard, UpdatedAt: nowMillis},
				"node2": {Proof: 0, Disproof: 100, Status: ProofStatusProven, Source: SignalSourceSoft, UpdatedAt: nowMillis},
			},
		},
		constraint: &mockConstraintIndexView{
			data: map[string]Constraint{
				"c1": {ID: "c1", Type: ConstraintTypeMutualExclusion, Nodes: []string{"a", "b"}, Active: true, Source: SignalSourceHard, CreatedAt: nowMillis},
			},
		},
		similarity: &mockSimilarityIndexView{
			pairCount: 2,
			pairs: map[string]map[string]float64{
				"alpha": {"beta": 0.9, "gamma": 0.5},
			},
		},
		dependency: &mockDependencyIndexView{
			edgeCount: 2,
			edges: map[string][]string{
				"funcA": {"funcB"},
				"funcB": {"funcC"},
			},
			graphBacked: false,
		},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{
				{ID: "h1", NodeID: "node1", Action: "explore", Result: "success", Source: SignalSourceHard, Timestamp: nowMillis, Metadata: map[string]string{"key": "value"}},
			},
		},
		streaming: &mockStreamingIndexView{cardinality: 50, size: 512},
	}

	// Export
	exportResult, err := s.ExportFull(ctx, snapshot, "roundtrip-test", nil)
	if err != nil {
		t.Fatalf("ExportFull failed: %v", err)
	}

	// Import
	importedState, err := s.Import(ctx, exportResult.Export, nil)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify imported data matches exported data
	if importedState.Generation != 42 {
		t.Errorf("Generation = %d, want 42", importedState.Generation)
	}

	if len(importedState.ProofData) != 2 {
		t.Errorf("ProofData count = %d, want 2", len(importedState.ProofData))
	}

	if len(importedState.ConstraintData) != 1 {
		t.Errorf("ConstraintData count = %d, want 1", len(importedState.ConstraintData))
	}

	// Similarity should have both directions reconstructed
	if len(importedState.SimilarityData) == 0 {
		t.Error("SimilarityData should not be empty")
	}

	// History should be imported
	if len(importedState.HistoryData) == 0 {
		t.Error("HistoryData should not be empty")
	}

	// Verify metadata preserved in history
	if len(importedState.HistoryData) > 0 && importedState.HistoryData[0].Metadata["key"] != "value" {
		t.Error("History metadata not preserved")
	}
}

func TestSerializer_Import_NilExport(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	_, err := s.Import(ctx, nil, nil)
	if err == nil {
		t.Error("Expected error for nil export")
	}
}

func TestSerializer_Import_NilContext(t *testing.T) {
	s := NewSerializer(nil)

	export := &CRSExport{SessionID: "test"}

	_, err := s.Import(nil, export, nil)
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestSerializer_Import_ValidationErrors(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	t.Run("empty proof node_id", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test",
			Indexes: IndexesExport{
				Proof: ProofIndexExport{
					Entries: []ProofEntry{{NodeID: "", Proof: 10}},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for empty node_id")
		}
	})

	t.Run("empty constraint id", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test",
			Indexes: IndexesExport{
				Constraint: ConstraintIndexExport{
					Constraints: []ConstraintEntry{{ID: ""}},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for empty constraint id")
		}
	})

	t.Run("empty similarity pair ids", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test",
			Indexes: IndexesExport{
				Similarity: SimilarityIndexExport{
					Pairs: []SimilarityPairExport{{FromID: "", ToID: "b"}},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for empty from_id")
		}
	})

	t.Run("empty dependency edge ids", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test",
			Indexes: IndexesExport{
				Dependency: DependencyIndexExport{
					Source: "legacy",
					Edges:  []DependencyEdgeExport{{FromID: "a", ToID: ""}},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for empty to_id")
		}
	})

	t.Run("empty history entry id", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test",
			Indexes: IndexesExport{
				History: HistoryIndexExport{
					RecentEntries: []HistoryEntryExport{{ID: ""}},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for empty history id")
		}
	})
}

func TestSerializer_Import_DuplicateEdgeDeduplication(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	// Export with duplicate edges
	export := &CRSExport{
		SessionID: "test-dedupe",
		Indexes: IndexesExport{
			Dependency: DependencyIndexExport{
				Source: "legacy",
				Edges: []DependencyEdgeExport{
					{FromID: "a", ToID: "b"},
					{FromID: "a", ToID: "b"}, // Duplicate
					{FromID: "a", ToID: "b"}, // Duplicate
					{FromID: "a", ToID: "c"},
				},
			},
		},
	}

	importedState, err := s.Import(ctx, export, &ImportOptions{StrictValidation: false})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Should only have 2 unique edges (a->b and a->c)
	forwardEdges := importedState.DependencyForward["a"]
	if len(forwardEdges) != 2 {
		t.Errorf("Forward edges from 'a' = %d, want 2 (after deduplication)", len(forwardEdges))
	}
}

func TestSerializer_Import_GraphBackedSkipsEdges(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	export := &CRSExport{
		SessionID: "test-graph-backed",
		Indexes: IndexesExport{
			Dependency: DependencyIndexExport{
				Source:    "graph_backed",
				EdgeCount: 100,
				Edges:     nil, // No edges for graph-backed
			},
		},
	}

	importedState, err := s.Import(ctx, export, nil)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Forward and reverse should be empty maps (not nil)
	if importedState.DependencyForward == nil {
		t.Error("DependencyForward should not be nil")
	}
	if len(importedState.DependencyForward) != 0 {
		t.Errorf("DependencyForward should be empty for graph-backed, got %d", len(importedState.DependencyForward))
	}
}

func TestSerializer_Import_SymmetricSimilarity(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	// Export with one-direction similarity
	export := &CRSExport{
		SessionID: "test-symmetric",
		Indexes: IndexesExport{
			Similarity: SimilarityIndexExport{
				Pairs: []SimilarityPairExport{
					{FromID: "a", ToID: "b", Similarity: 0.9},
				},
			},
		},
	}

	importedState, err := s.Import(ctx, export, nil)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Both directions should be present
	if importedState.SimilarityData["a"]["b"] != 0.9 {
		t.Error("Expected a->b = 0.9")
	}
	if importedState.SimilarityData["b"]["a"] != 0.9 {
		t.Error("Expected b->a = 0.9 (symmetric)")
	}
}

func TestSerializer_Import_SimilarityRangeValidation(t *testing.T) {
	s := NewSerializer(nil)
	ctx := context.Background()

	t.Run("negative similarity rejected", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test-negative",
			Indexes: IndexesExport{
				Similarity: SimilarityIndexExport{
					Pairs: []SimilarityPairExport{
						{FromID: "a", ToID: "b", Similarity: -0.1},
					},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for negative similarity")
		}
	})

	t.Run("similarity > 1 rejected", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test-over-one",
			Indexes: IndexesExport{
				Similarity: SimilarityIndexExport{
					Pairs: []SimilarityPairExport{
						{FromID: "a", ToID: "b", Similarity: 1.5},
					},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err == nil {
			t.Error("Expected error for similarity > 1")
		}
	})

	t.Run("valid range accepted", func(t *testing.T) {
		export := &CRSExport{
			SessionID: "test-valid",
			Indexes: IndexesExport{
				Similarity: SimilarityIndexExport{
					Pairs: []SimilarityPairExport{
						{FromID: "a", ToID: "b", Similarity: 0.0},
						{FromID: "c", ToID: "d", Similarity: 1.0},
						{FromID: "e", ToID: "f", Similarity: 0.5},
					},
				},
			},
		}

		_, err := s.Import(ctx, export, nil)
		if err != nil {
			t.Errorf("Valid similarity should be accepted: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkExportFull_10KPairs(b *testing.B) {
	s := NewSerializer(nil)
	ctx := context.Background()

	// Create snapshot with 10K similarity pairs
	pairs := make(map[string]map[string]float64)
	for i := 0; i < 100; i++ {
		fromID := fmt.Sprintf("node%d", i)
		pairs[fromID] = make(map[string]float64)
		for j := i + 1; j < 200 && j < i+101; j++ {
			toID := fmt.Sprintf("node%d", j)
			pairs[fromID][toID] = float64(i+j) / 300.0
		}
	}

	snapshot := &mockSnapshot{
		generation: 1,
		proof:      &mockProofIndexView{data: map[string]ProofNumber{}},
		constraint: &mockConstraintIndexView{data: map[string]Constraint{}},
		similarity: &mockSimilarityIndexView{
			pairCount: 10000,
			pairs:     pairs,
		},
		dependency: &mockDependencyIndexView{edgeCount: 0},
		history:    &mockHistoryIndexView{},
		streaming:  &mockStreamingIndexView{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.ExportFull(ctx, snapshot, "bench-session", nil)
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
	}
}

func BenchmarkImport_10KPairs(b *testing.B) {
	s := NewSerializer(nil)
	ctx := context.Background()

	// Create export with 10K similarity pairs
	pairs := make([]SimilarityPairExport, 0, 10000)
	for i := 0; i < 10000; i++ {
		pairs = append(pairs, SimilarityPairExport{
			FromID:     fmt.Sprintf("node%d", i),
			ToID:       fmt.Sprintf("node%d", i+10000),
			Similarity: float64(i) / 10000.0,
		})
	}

	export := &CRSExport{
		SessionID:  "bench-session",
		Generation: 1,
		Indexes: IndexesExport{
			Similarity: SimilarityIndexExport{
				PairCount: 10000,
				Pairs:     pairs,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Import(ctx, export, nil)
		if err != nil {
			b.Fatalf("Import failed: %v", err)
		}
	}
}

func BenchmarkAllPairsFiltered_10KPairs(b *testing.B) {
	// Create 10K pairs
	pairs := make(map[string]map[string]float64)
	for i := 0; i < 100; i++ {
		fromID := fmt.Sprintf("node%d", i)
		pairs[fromID] = make(map[string]float64)
		for j := i + 1; j < 200 && j < i+101; j++ {
			toID := fmt.Sprintf("node%d", j)
			pairs[fromID][toID] = float64(i+j) / 300.0
		}
	}

	idx := &mockSimilarityIndexView{
		pairCount: 10000,
		pairs:     pairs,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.AllPairsFiltered(-1)
	}
}
