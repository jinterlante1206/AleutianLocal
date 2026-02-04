// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package crs

import (
	"encoding/json"
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
}

func (m *mockSimilarityIndexView) Distance(node1, node2 string) (float64, bool) {
	return 0, false
}

func (m *mockSimilarityIndexView) NearestNeighbors(nodeID string, k int) []SimilarityMatch {
	return nil
}

func (m *mockSimilarityIndexView) Size() int {
	return m.pairCount
}

// mockDependencyIndexView implements DependencyIndexView for testing.
type mockDependencyIndexView struct {
	edgeCount int
}

func (m *mockDependencyIndexView) DependsOn(nodeID string) []string {
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
	createdAt  time.Time
	proof      ProofIndexView
	constraint ConstraintIndexView
	similarity SimilarityIndexView
	dependency DependencyIndexView
	history    HistoryIndexView
	streaming  StreamingIndexView
}

func (m *mockSnapshot) Generation() int64                    { return m.generation }
func (m *mockSnapshot) CreatedAt() time.Time                 { return m.createdAt }
func (m *mockSnapshot) ProofIndex() ProofIndexView           { return m.proof }
func (m *mockSnapshot) ConstraintIndex() ConstraintIndexView { return m.constraint }
func (m *mockSnapshot) SimilarityIndex() SimilarityIndexView { return m.similarity }
func (m *mockSnapshot) DependencyIndex() DependencyIndexView { return m.dependency }
func (m *mockSnapshot) HistoryIndex() HistoryIndexView       { return m.history }
func (m *mockSnapshot) StreamingIndex() StreamingIndexView   { return m.streaming }
func (m *mockSnapshot) Query() QueryAPI                      { return nil }

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

	now := time.Now()
	snapshot := &mockSnapshot{
		generation: 42,
		createdAt:  now,
		proof: &mockProofIndexView{
			data: map[string]ProofNumber{
				"node1": {Proof: 0, Disproof: 1000, Status: ProofStatusProven, Source: SignalSourceHard, UpdatedAt: now},
				"node2": {Proof: 1000, Disproof: 0, Status: ProofStatusDisproven, Source: SignalSourceSoft, UpdatedAt: now},
				"node3": {Proof: 100, Disproof: 100, Status: ProofStatusUnknown, Source: SignalSourceUnknown, UpdatedAt: now},
			},
		},
		constraint: &mockConstraintIndexView{
			data: map[string]Constraint{
				"c1": {ID: "c1", Type: ConstraintTypeMutualExclusion, Nodes: []string{"node1", "node2"}, Active: true, Source: SignalSourceHard, CreatedAt: now},
			},
		},
		similarity: &mockSimilarityIndexView{pairCount: 5},
		dependency: &mockDependencyIndexView{edgeCount: 10},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{
				{ID: "h1", NodeID: "node1", Action: "explore", Result: "success", Source: SignalSourceHard, Timestamp: now, Metadata: map[string]string{"key": "value"}},
				{ID: "h2", NodeID: "node2", Action: "analyze", Result: "found_issue", Source: SignalSourceSoft, Timestamp: now},
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

	now := time.Now()
	snapshot := &mockSnapshot{
		generation: 1,
		proof: &mockProofIndexView{
			data: map[string]ProofNumber{
				"node1": {Proof: 0, Disproof: 1000, Status: ProofStatusProven, Source: SignalSourceHard, UpdatedAt: now},
			},
		},
		constraint: &mockConstraintIndexView{
			data: map[string]Constraint{
				"c1": {ID: "c1", Type: ConstraintTypeImplication, Nodes: []string{"a", "b"}, Active: true, Source: SignalSourceSoft, CreatedAt: now},
			},
		},
		similarity: &mockSimilarityIndexView{pairCount: 1},
		dependency: &mockDependencyIndexView{edgeCount: 1},
		history: &mockHistoryIndexView{
			entries: []HistoryEntry{{ID: "h1", NodeID: "node1", Action: "test", Timestamp: now}},
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
