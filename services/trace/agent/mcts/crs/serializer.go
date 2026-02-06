// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"log/slog"
	"time"
)

// -----------------------------------------------------------------------------
// Export Types
// -----------------------------------------------------------------------------

// CRSExport is the JSON-serializable representation of CRS state.
//
// Description:
//
//	Contains a complete snapshot of CRS state suitable for JSON export.
//	Includes all six indexes and computed summary metrics.
type CRSExport struct {
	// SessionID identifies the session this export belongs to.
	SessionID string `json:"session_id"`

	// Generation is the CRS generation at export time.
	Generation int64 `json:"generation"`

	// Timestamp is when this export was created.
	Timestamp time.Time `json:"timestamp"`

	// Indexes contains all six CRS indexes in exportable form.
	Indexes IndexesExport `json:"indexes"`

	// Summary provides high-level metrics about the reasoning state.
	Summary ReasoningSummary `json:"summary"`
}

// IndexesExport contains all six indexes in JSON-serializable form.
type IndexesExport struct {
	// Proof contains proof/disproof numbers for nodes.
	Proof ProofIndexExport `json:"proof"`

	// Constraint contains active constraints.
	Constraint ConstraintIndexExport `json:"constraint"`

	// Similarity contains node similarity data.
	Similarity SimilarityIndexExport `json:"similarity"`

	// Dependency contains dependency graph data.
	Dependency DependencyIndexExport `json:"dependency"`

	// History contains decision history.
	History HistoryIndexExport `json:"history"`

	// Streaming contains streaming statistics.
	Streaming StreamingIndexExport `json:"streaming"`
}

// ReasoningSummary provides high-level metrics about reasoning progress.
//
// Description:
//
//	Computed from CRS state to give a quick overview of reasoning
//	progress without requiring full index inspection.
type ReasoningSummary struct {
	// NodesExplored is the total number of nodes in the proof index.
	NodesExplored int `json:"nodes_explored"`

	// NodesProven is the count of nodes with PROVEN status.
	NodesProven int `json:"nodes_proven"`

	// NodesDisproven is the count of nodes with DISPROVEN status.
	NodesDisproven int `json:"nodes_disproven"`

	// NodesUnknown is the count of nodes with UNKNOWN or EXPANDED status.
	NodesUnknown int `json:"nodes_unknown"`

	// ConstraintsApplied is the number of active constraints.
	ConstraintsApplied int `json:"constraints_applied"`

	// ExplorationDepth is the number of history entries (proxy for depth).
	ExplorationDepth int `json:"exploration_depth"`

	// ConfidenceScore is the ratio of proven nodes to explored nodes.
	// Value is between 0.0 and 1.0. Use with caution - this is a coverage
	// metric, not a statistical confidence interval.
	ConfidenceScore float64 `json:"confidence_score"`
}

// -----------------------------------------------------------------------------
// Index Export Types
// -----------------------------------------------------------------------------

// ProofIndexExport is the serializable form of the Proof Index.
type ProofIndexExport struct {
	// Entries contains all proof number entries.
	Entries []ProofEntry `json:"entries"`
}

// ProofEntry represents a single proof number entry.
type ProofEntry struct {
	// NodeID is the unique node identifier.
	NodeID string `json:"node_id"`

	// Proof is the proof number (cost to prove).
	Proof uint64 `json:"proof"`

	// Disproof is the disproof number (cost to disprove).
	Disproof uint64 `json:"disproof"`

	// Status is the current proof status: "unknown", "proven", "disproven", "expanded".
	Status string `json:"status"`

	// Source indicates signal source: "unknown", "hard", "soft".
	Source string `json:"source"`

	// UpdatedAt is when this entry was last updated (RFC3339 format).
	UpdatedAt time.Time `json:"updated_at"`
}

// ConstraintIndexExport is the serializable form of the Constraint Index.
type ConstraintIndexExport struct {
	// Constraints contains all active constraints.
	Constraints []ConstraintEntry `json:"constraints"`
}

// ConstraintEntry represents a single constraint.
type ConstraintEntry struct {
	// ID is the unique constraint identifier.
	ID string `json:"id"`

	// Type is the constraint type: "mutual_exclusion", "implication", "ordering", "resource".
	Type string `json:"type"`

	// Nodes are the node IDs affected by this constraint.
	Nodes []string `json:"nodes"`

	// Expression is the constraint expression if any.
	Expression string `json:"expression,omitempty"`

	// Active indicates if the constraint is currently enforced.
	Active bool `json:"active"`

	// Source indicates signal source: "unknown", "hard", "soft".
	Source string `json:"source"`

	// CreatedAt is when this constraint was created.
	CreatedAt time.Time `json:"created_at"`
}

// SimilarityIndexExport is the serializable form of the Similarity Index.
type SimilarityIndexExport struct {
	// PairCount is the number of similarity pairs stored.
	PairCount int `json:"pair_count"`

	// Note: Full similarity matrix export is deferred for performance.
	// Use QueryAPI for specific similarity lookups.
}

// DependencyIndexExport is the serializable form of the Dependency Index.
type DependencyIndexExport struct {
	// EdgeCount is the number of dependency edges.
	EdgeCount int `json:"edge_count"`

	// Note: Full dependency graph export is deferred for performance.
	// Use QueryAPI for specific dependency lookups.
}

// HistoryIndexExport is the serializable form of the History Index.
type HistoryIndexExport struct {
	// EntryCount is the total number of history entries.
	EntryCount int `json:"entry_count"`

	// RecentEntries contains the most recent history entries.
	RecentEntries []HistoryEntryExport `json:"recent_entries,omitempty"`
}

// HistoryEntryExport represents a single history entry.
type HistoryEntryExport struct {
	// ID is the unique entry identifier.
	ID string `json:"id"`

	// NodeID is the node this decision was about.
	NodeID string `json:"node_id"`

	// Action is the action taken.
	Action string `json:"action"`

	// Result is the outcome of the action.
	Result string `json:"result"`

	// Source indicates signal source.
	Source string `json:"source"`

	// Timestamp is when this decision was made.
	Timestamp time.Time `json:"timestamp"`

	// Metadata contains additional context.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// StreamingIndexExport is the serializable form of the Streaming Index.
type StreamingIndexExport struct {
	// Cardinality is the estimated unique item count.
	Cardinality uint64 `json:"cardinality"`

	// ApproximateBytes is the approximate memory usage.
	ApproximateBytes int `json:"approximate_bytes"`
}

// -----------------------------------------------------------------------------
// Serializer
// -----------------------------------------------------------------------------

// Serializer converts CRS snapshots to exportable JSON formats.
//
// Thread Safety: Safe for concurrent use (stateless).
type Serializer struct {
	logger *slog.Logger
}

// NewSerializer creates a new CRS serializer.
//
// Inputs:
//
//	logger - Logger for serialization events. Uses default if nil.
//
// Outputs:
//
//	*Serializer - The configured serializer.
func NewSerializer(logger *slog.Logger) *Serializer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Serializer{logger: logger}
}

// Export converts a CRS snapshot to exportable JSON format.
//
// Description:
//
//	Takes an immutable CRS snapshot and converts all indexes
//	to JSON-serializable structures. Computes summary metrics.
//
// Inputs:
//
//	snapshot - Immutable CRS snapshot. If nil, returns empty export.
//	sessionID - Session identifier for the export.
//
// Outputs:
//
//	*CRSExport - Serializable CRS state. Never nil.
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) Export(snapshot Snapshot, sessionID string) *CRSExport {
	export := &CRSExport{
		SessionID: sessionID,
		Timestamp: time.Now(),
	}

	if snapshot == nil {
		s.logger.Debug("exporting nil snapshot",
			slog.String("session_id", sessionID),
		)
		return export
	}

	export.Generation = snapshot.Generation()

	// Export each index
	export.Indexes.Proof = s.exportProofIndex(snapshot.ProofIndex())
	export.Indexes.Constraint = s.exportConstraintIndex(snapshot.ConstraintIndex())
	export.Indexes.Similarity = s.exportSimilarityIndex(snapshot.SimilarityIndex())
	export.Indexes.Dependency = s.exportDependencyIndex(snapshot.DependencyIndex())
	export.Indexes.History = s.exportHistoryIndex(snapshot.HistoryIndex())
	export.Indexes.Streaming = s.exportStreamingIndex(snapshot.StreamingIndex())

	// Compute summary
	export.Summary = s.ComputeSummary(snapshot)

	return export
}

// ExportSummaryOnly returns just the summary without full indexes.
//
// Description:
//
//	Lightweight export for including in API responses.
//	Computes metrics without serializing full indexes.
//
// Inputs:
//
//	snapshot - Immutable CRS snapshot. If nil, returns empty summary.
//
// Outputs:
//
//	ReasoningSummary - High-level metrics.
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) ExportSummaryOnly(snapshot Snapshot) ReasoningSummary {
	if snapshot == nil {
		return ReasoningSummary{}
	}
	return s.ComputeSummary(snapshot)
}

// ComputeSummary computes summary metrics from a snapshot.
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) ComputeSummary(snapshot Snapshot) ReasoningSummary {
	if snapshot == nil {
		return ReasoningSummary{}
	}

	summary := ReasoningSummary{}

	// Count proof statuses
	proofIndex := snapshot.ProofIndex()
	if proofIndex != nil {
		proofData := proofIndex.All()
		for _, proof := range proofData {
			summary.NodesExplored++
			switch proof.Status {
			case ProofStatusProven:
				summary.NodesProven++
			case ProofStatusDisproven:
				summary.NodesDisproven++
			default:
				summary.NodesUnknown++
			}
		}
	}

	// Count constraints
	constraintIndex := snapshot.ConstraintIndex()
	if constraintIndex != nil {
		summary.ConstraintsApplied = constraintIndex.Size()
	}

	// Compute exploration depth from history
	historyIndex := snapshot.HistoryIndex()
	if historyIndex != nil {
		summary.ExplorationDepth = historyIndex.Size()
	}

	// Compute confidence score (coverage metric)
	if summary.NodesExplored > 0 {
		summary.ConfidenceScore = float64(summary.NodesProven) / float64(summary.NodesExplored)
	}

	return summary
}

// -----------------------------------------------------------------------------
// Index Export Helpers
// -----------------------------------------------------------------------------

func (s *Serializer) exportProofIndex(idx ProofIndexView) ProofIndexExport {
	export := ProofIndexExport{
		Entries: make([]ProofEntry, 0),
	}

	if idx == nil {
		return export
	}

	proofData := idx.All()
	for nodeID, proof := range proofData {
		export.Entries = append(export.Entries, ProofEntry{
			NodeID:    nodeID,
			Proof:     proof.Proof,
			Disproof:  proof.Disproof,
			Status:    proof.Status.String(),
			Source:    proof.Source.String(),
			UpdatedAt: time.UnixMilli(proof.UpdatedAt).UTC(),
		})
	}

	return export
}

func (s *Serializer) exportConstraintIndex(idx ConstraintIndexView) ConstraintIndexExport {
	export := ConstraintIndexExport{
		Constraints: make([]ConstraintEntry, 0),
	}

	if idx == nil {
		return export
	}

	constraintData := idx.All()
	for _, c := range constraintData {
		// Make a copy of Nodes slice to avoid sharing
		nodesCopy := make([]string, len(c.Nodes))
		copy(nodesCopy, c.Nodes)

		export.Constraints = append(export.Constraints, ConstraintEntry{
			ID:         c.ID,
			Type:       c.Type.String(),
			Nodes:      nodesCopy,
			Expression: c.Expression,
			Active:     c.Active,
			Source:     c.Source.String(),
			CreatedAt:  time.UnixMilli(c.CreatedAt).UTC(),
		})
	}

	return export
}

func (s *Serializer) exportSimilarityIndex(idx SimilarityIndexView) SimilarityIndexExport {
	export := SimilarityIndexExport{
		PairCount: 0,
	}

	if idx != nil {
		export.PairCount = idx.Size()
	}

	return export
}

func (s *Serializer) exportDependencyIndex(idx DependencyIndexView) DependencyIndexExport {
	export := DependencyIndexExport{
		EdgeCount: 0,
	}

	if idx != nil {
		export.EdgeCount = idx.Size()
	}

	return export
}

func (s *Serializer) exportHistoryIndex(idx HistoryIndexView) HistoryIndexExport {
	export := HistoryIndexExport{
		EntryCount:    0,
		RecentEntries: make([]HistoryEntryExport, 0),
	}

	if idx == nil {
		return export
	}

	export.EntryCount = idx.Size()

	// Include recent entries (last 100)
	recentLimit := 100
	recent := idx.Recent(recentLimit)
	for _, entry := range recent {
		// Deep copy metadata map
		var metadataCopy map[string]string
		if entry.Metadata != nil {
			metadataCopy = make(map[string]string, len(entry.Metadata))
			for k, v := range entry.Metadata {
				metadataCopy[k] = v
			}
		}

		export.RecentEntries = append(export.RecentEntries, HistoryEntryExport{
			ID:        entry.ID,
			NodeID:    entry.NodeID,
			Action:    entry.Action,
			Result:    entry.Result,
			Source:    entry.Source.String(),
			Timestamp: time.UnixMilli(entry.Timestamp).UTC(),
			Metadata:  metadataCopy,
		})
	}

	return export
}

func (s *Serializer) exportStreamingIndex(idx StreamingIndexView) StreamingIndexExport {
	export := StreamingIndexExport{
		Cardinality:      0,
		ApproximateBytes: 0,
	}

	if idx != nil {
		export.Cardinality = idx.Cardinality()
		export.ApproximateBytes = idx.Size()
	}

	return export
}
