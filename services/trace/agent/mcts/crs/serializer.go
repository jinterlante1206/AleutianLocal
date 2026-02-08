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
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Metrics (GR-34)
// -----------------------------------------------------------------------------

var (
	exportDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crs_export_duration_seconds",
		Help:    "Time to export CRS state",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0},
	}, []string{"session_id"})

	exportItemsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crs_export_items_total",
		Help: "Total items exported by type",
	}, []string{"session_id", "index_type"})

	exportTruncatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crs_export_truncated_total",
		Help: "Total exports that were truncated",
	}, []string{"session_id", "index_type"})

	importDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crs_import_duration_seconds",
		Help:    "Time to import CRS state",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0},
	}, []string{"session_id"})

	importErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crs_import_errors_total",
		Help: "Total import errors by type",
	}, []string{"session_id", "error_type"})
)

// -----------------------------------------------------------------------------
// Tracer
// -----------------------------------------------------------------------------

var serializerTracer = otel.Tracer("crs.serializer")

// -----------------------------------------------------------------------------
// Export/Import Options
// -----------------------------------------------------------------------------

// DefaultMaxSimilarityPairs is the default limit for similarity pairs export.
const DefaultMaxSimilarityPairs = 100_000

// DefaultMaxDependencyEdges is the default limit for dependency edges export.
const DefaultMaxDependencyEdges = 100_000

// defaultExportTimeout is the timeout for export operations if caller provides no deadline.
const defaultExportTimeout = 5 * time.Minute

// ExportOptions configures export behavior.
type ExportOptions struct {
	// MaxSimilarityPairs limits how many similarity pairs to export.
	// 0 = use default (100K), -1 = unlimited.
	MaxSimilarityPairs int

	// MaxDependencyEdges limits how many dependency edges to export.
	// 0 = use default (100K), -1 = unlimited.
	MaxDependencyEdges int
}

// ExportResult contains the export data and metadata.
type ExportResult struct {
	// Export is the CRS export data.
	Export *CRSExport

	// Truncated indicates if any index was truncated.
	Truncated bool

	// Warnings contains messages about truncation or other issues.
	Warnings []string
}

// ImportOptions configures import behavior.
type ImportOptions struct {
	// StrictValidation returns error on count mismatches.
	// Default: true.
	StrictValidation bool
}

// DefaultImportOptions returns the default import options.
func DefaultImportOptions() *ImportOptions {
	return &ImportOptions{
		StrictValidation: true,
	}
}

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

	// Timestamp is when this export was created (Unix milliseconds UTC).
	Timestamp int64 `json:"timestamp"`

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

// SimilarityPairExport represents a single similarity pair.
//
// NOTE: Only one direction is exported (FromID < ToID) since similarity
// is symmetric. Import reconstructs both directions.
type SimilarityPairExport struct {
	// FromID is the first node ID (lexicographically smaller).
	FromID string `json:"from_id"`

	// ToID is the second node ID (lexicographically larger).
	ToID string `json:"to_id"`

	// Similarity is the similarity score between the nodes (0.0 to 1.0).
	Similarity float64 `json:"similarity"`
}

// SimilarityIndexExport is the serializable form of the Similarity Index.
type SimilarityIndexExport struct {
	// PairCount is the number of similarity pairs stored.
	PairCount int `json:"pair_count"`

	// Pairs contains all similarity pairs for full export.
	// Only one direction is exported (FromID < ToID) to avoid duplicates.
	Pairs []SimilarityPairExport `json:"pairs,omitempty"`

	// Truncated indicates if Pairs was truncated due to limits.
	Truncated bool `json:"truncated,omitempty"`
}

// DependencyEdgeExport represents a single dependency edge.
type DependencyEdgeExport struct {
	// FromID is the source node (the caller/dependent).
	FromID string `json:"from_id"`

	// ToID is the target node (the callee/dependency).
	ToID string `json:"to_id"`
}

// DependencyIndexExport is the serializable form of the Dependency Index.
//
// Export behavior depends on the index implementation:
//   - Legacy dependencyGraph: Full edges are exported.
//   - GraphBackedDependencyIndex (GR-32): Only EdgeCount is exported; edges
//     live in the graph which has its own persistence.
type DependencyIndexExport struct {
	// EdgeCount is the number of dependency edges.
	EdgeCount int `json:"edge_count"`

	// Edges contains all dependency edges for full export.
	// Empty for graph-backed indexes (use graph persistence instead).
	Edges []DependencyEdgeExport `json:"edges,omitempty"`

	// Source indicates the index implementation type.
	// Values: "legacy" (edges exported) or "graph_backed" (edges not exported).
	Source string `json:"source,omitempty"`

	// Truncated indicates if Edges was truncated due to limits.
	Truncated bool `json:"truncated,omitempty"`
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

	// Timestamp is when this decision was made (Unix milliseconds UTC).
	Timestamp int64 `json:"timestamp"`

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

// ExportFull creates a full export of CRS state with options.
//
// Description:
//
//	Creates a JSON-serializable export of all CRS indexes, including
//	the previously incomplete SimilarityIndex and DependencyIndex.
//	Operates on an immutable snapshot for thread safety.
//
// Inputs:
//   - ctx: Context for cancellation and tracing. Must not be nil.
//   - snapshot: Immutable CRS snapshot to export. Must not be nil.
//   - sessionID: Session identifier for the export.
//   - opts: Export options. If nil, uses defaults (100K pair limit).
//
// Outputs:
//   - *ExportResult: Export data and metadata. Never nil on success.
//   - error: Non-nil if context is nil, snapshot is nil, or export fails.
//
// Example:
//
//	result, err := serializer.ExportFull(ctx, snap, "session-123", nil)
//	if err != nil {
//	    return fmt.Errorf("export: %w", err)
//	}
//	if result.Truncated {
//	    log.Warn("export truncated", "warnings", result.Warnings)
//	}
//
// Thread Safety: Safe for concurrent use (reads immutable snapshot).
func (s *Serializer) ExportFull(ctx context.Context, snapshot Snapshot, sessionID string, opts *ExportOptions) (*ExportResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	// Add default timeout if context has no deadline to prevent indefinite hangs
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultExportTimeout)
		defer cancel()
	}

	start := time.Now()
	ctx, span := serializerTracer.Start(ctx, "crs.Serializer.ExportFull",
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
		),
	)
	defer span.End()

	logger := s.logger.With(
		slog.String("session_id", sessionID),
	)

	result := &ExportResult{
		Export: &CRSExport{
			SessionID: sessionID,
			Timestamp: time.Now().UnixMilli(),
		},
		Warnings: make([]string, 0),
	}

	if snapshot == nil {
		logger.Debug("exporting nil snapshot")
		return result, nil
	}

	result.Export.Generation = snapshot.Generation()
	span.SetAttributes(attribute.Int64("generation", snapshot.Generation()))

	logger = logger.With(slog.Int64("generation", snapshot.Generation()))
	logger.Info("starting CRS export",
		slog.Int("similarity_size", snapshot.SimilarityIndex().Size()),
		slog.Int("dependency_size", snapshot.DependencyIndex().Size()),
	)

	// Apply defaults to options
	effectiveOpts := opts
	if effectiveOpts == nil {
		effectiveOpts = &ExportOptions{}
	}
	maxSimPairs := effectiveOpts.MaxSimilarityPairs
	if maxSimPairs == 0 {
		maxSimPairs = DefaultMaxSimilarityPairs
	}
	maxDepEdges := effectiveOpts.MaxDependencyEdges
	if maxDepEdges == 0 {
		maxDepEdges = DefaultMaxDependencyEdges
	}

	// Export each index (check context cancellation between each)
	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	result.Export.Indexes.Proof = s.exportProofIndex(snapshot.ProofIndex())
	exportItemsTotal.WithLabelValues(sessionID, "proof").Add(float64(len(result.Export.Indexes.Proof.Entries)))

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	result.Export.Indexes.Constraint = s.exportConstraintIndex(snapshot.ConstraintIndex())
	exportItemsTotal.WithLabelValues(sessionID, "constraint").Add(float64(len(result.Export.Indexes.Constraint.Constraints)))

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	simExport, simTruncated := s.exportSimilarityIndexFull(ctx, snapshot.SimilarityIndex(), maxSimPairs)
	result.Export.Indexes.Similarity = simExport
	if simTruncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("similarity pairs truncated to %d", maxSimPairs))
		exportTruncatedTotal.WithLabelValues(sessionID, "similarity").Inc()
	}
	exportItemsTotal.WithLabelValues(sessionID, "similarity").Add(float64(len(simExport.Pairs)))

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	depExport, depTruncated := s.exportDependencyIndexFull(ctx, snapshot.DependencyIndex(), maxDepEdges)
	result.Export.Indexes.Dependency = depExport
	if depTruncated {
		result.Truncated = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("dependency edges truncated to %d", maxDepEdges))
		exportTruncatedTotal.WithLabelValues(sessionID, "dependency").Inc()
	}
	exportItemsTotal.WithLabelValues(sessionID, "dependency").Add(float64(len(depExport.Edges)))

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	result.Export.Indexes.History = s.exportHistoryIndex(snapshot.HistoryIndex())
	exportItemsTotal.WithLabelValues(sessionID, "history").Add(float64(result.Export.Indexes.History.EntryCount))

	result.Export.Indexes.Streaming = s.exportStreamingIndex(snapshot.StreamingIndex())

	// Compute summary
	result.Export.Summary = s.ComputeSummary(snapshot)

	duration := time.Since(start)
	exportDuration.WithLabelValues(sessionID).Observe(duration.Seconds())

	span.SetAttributes(
		attribute.Int("similarity_pairs", len(result.Export.Indexes.Similarity.Pairs)),
		attribute.Int("dependency_edges", len(result.Export.Indexes.Dependency.Edges)),
		attribute.Int("proof_entries", len(result.Export.Indexes.Proof.Entries)),
		attribute.Int("constraint_entries", len(result.Export.Indexes.Constraint.Constraints)),
		attribute.Bool("truncated", result.Truncated),
	)

	if result.Truncated {
		span.AddEvent("export_truncated",
			trace.WithAttributes(
				attribute.StringSlice("warnings", result.Warnings),
			),
		)
	}

	logger.Info("CRS export complete",
		slog.Duration("duration", duration),
		slog.Int("similarity_pairs_exported", len(result.Export.Indexes.Similarity.Pairs)),
		slog.Int("dependency_edges_exported", len(result.Export.Indexes.Dependency.Edges)),
		slog.Bool("truncated", result.Truncated),
	)

	return result, nil
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
		Timestamp: time.Now().UnixMilli(),
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
	if idx == nil {
		return ProofIndexExport{
			Entries: make([]ProofEntry, 0),
		}
	}

	proofData := idx.All()
	export := ProofIndexExport{
		Entries: make([]ProofEntry, 0, len(proofData)),
	}

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
	if idx == nil {
		return ConstraintIndexExport{
			Constraints: make([]ConstraintEntry, 0),
		}
	}

	constraintData := idx.All()
	export := ConstraintIndexExport{
		Constraints: make([]ConstraintEntry, 0, len(constraintData)),
	}

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
			Timestamp: entry.Timestamp,
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

// -----------------------------------------------------------------------------
// Full Index Export Helpers (GR-34)
// -----------------------------------------------------------------------------

// exportSimilarityIndexFull exports all similarity pairs with truncation support.
//
// Description:
//
//	Exports all similarity pairs from the index. Only exports one direction
//	(FromID < ToID) since similarity is symmetric. Uses AllPairsFiltered for
//	memory efficiency - avoids creating a full bidirectional copy.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - idx: Similarity index view. May be nil.
//   - maxPairs: Maximum pairs to export. -1 for unlimited.
//
// Outputs:
//   - SimilarityIndexExport: Export data with pairs.
//   - bool: True if truncated.
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) exportSimilarityIndexFull(ctx context.Context, idx SimilarityIndexView, maxPairs int) (SimilarityIndexExport, bool) {
	export := SimilarityIndexExport{
		PairCount: 0,
		Pairs:     make([]SimilarityPairExport, 0),
	}

	if idx == nil {
		return export, false
	}

	// Check context before starting
	select {
	case <-ctx.Done():
		export.Truncated = true
		return export, true
	default:
	}

	export.PairCount = idx.Size()

	// Use AllPairsFiltered for memory efficiency - only allocates one direction
	pairs, truncated := idx.AllPairsFiltered(maxPairs)

	// Convert to export format
	export.Pairs = make([]SimilarityPairExport, 0, len(pairs))
	for _, pair := range pairs {
		export.Pairs = append(export.Pairs, SimilarityPairExport{
			FromID:     pair.FromID,
			ToID:       pair.ToID,
			Similarity: pair.Similarity,
		})
	}

	export.Truncated = truncated
	return export, truncated
}

// exportDependencyIndexFull exports all dependency edges with truncation support.
//
// Description:
//
//	Exports all dependency edges from the index. For GraphBackedDependencyIndex
//	(GR-32), skips edge export since edges live in the graph. Supports truncation
//	and context cancellation.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - idx: Dependency index view. May be nil.
//   - maxEdges: Maximum edges to export. -1 for unlimited.
//
// Outputs:
//   - DependencyIndexExport: Export data with edges (or just count for graph-backed).
//   - bool: True if truncated.
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) exportDependencyIndexFull(ctx context.Context, idx DependencyIndexView, maxEdges int) (DependencyIndexExport, bool) {
	export := DependencyIndexExport{
		EdgeCount: 0,
		Edges:     make([]DependencyEdgeExport, 0),
	}

	if idx == nil {
		return export, false
	}

	export.EdgeCount = idx.Size()

	// Check if graph-backed (GR-32)
	if idx.IsGraphBacked() {
		export.Source = "graph_backed"
		s.logger.Info("dependency index is graph-backed; edges not exported (use graph persistence)",
			slog.Int("edge_count", export.EdgeCount),
		)
		return export, false
	}

	export.Source = "legacy"

	// Get all edges (deep copy from snapshot)
	allEdges := idx.AllEdges()
	if allEdges == nil {
		// Graph-backed fallback (shouldn't happen if IsGraphBacked is correct)
		return export, false
	}

	// Pre-allocate based on expected size
	expectedSize := export.EdgeCount
	if maxEdges > 0 && expectedSize > maxEdges {
		expectedSize = maxEdges
	}
	export.Edges = make([]DependencyEdgeExport, 0, expectedSize)

	truncated := false
	edgeCount := 0

	for fromID, toIDs := range allEdges {
		// Check context cancellation every outer iteration
		select {
		case <-ctx.Done():
			// Context cancelled - return what we have
			export.Truncated = true
			return export, true
		default:
		}

		for _, toID := range toIDs {
			// Check truncation limit
			if maxEdges > 0 && edgeCount >= maxEdges {
				truncated = true
				break
			}

			export.Edges = append(export.Edges, DependencyEdgeExport{
				FromID: fromID,
				ToID:   toID,
			})
			edgeCount++
		}

		if truncated {
			break
		}
	}

	export.Truncated = truncated
	return export, truncated
}

// -----------------------------------------------------------------------------
// Import Functions (GR-34)
// -----------------------------------------------------------------------------

// Import reconstructs CRS state from an export.
//
// Description:
//
//	Parses a CRSExport and constructs internal data structures that can be
//	used to restore CRS state. Uses transactional semantics: all data is
//	validated and built before constructing the result.
//
//	For similarity pairs, both directions are reconstructed since only one
//	direction is exported. For dependency edges, duplicates are deduplicated.
//
// Inputs:
//   - ctx: Context for cancellation and tracing. Must not be nil.
//   - export: The CRS export to import. Must not be nil.
//   - opts: Import options. If nil, uses defaults (strict validation).
//
// Outputs:
//   - *ImportedState: The imported state data. Never nil on success.
//   - error: Non-nil if validation fails or import errors occur.
//
// Example:
//
//	state, err := serializer.Import(ctx, export, nil)
//	if err != nil {
//	    return fmt.Errorf("import: %w", err)
//	}
//	// Use state to restore CRS...
//
// Thread Safety: Safe for concurrent use.
func (s *Serializer) Import(ctx context.Context, export *CRSExport, opts *ImportOptions) (*ImportedState, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if export == nil {
		return nil, fmt.Errorf("export must not be nil")
	}

	start := time.Now()
	sessionID := export.SessionID

	ctx, span := serializerTracer.Start(ctx, "crs.Serializer.Import",
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
			attribute.Int64("generation", export.Generation),
		),
	)
	defer span.End()

	logger := s.logger.With(
		slog.String("session_id", sessionID),
		slog.Int64("generation", export.Generation),
	)

	logger.Info("starting CRS import",
		slog.Int("similarity_pairs", len(export.Indexes.Similarity.Pairs)),
		slog.Int("dependency_edges", len(export.Indexes.Dependency.Edges)),
	)

	effectiveOpts := opts
	if effectiveOpts == nil {
		effectiveOpts = DefaultImportOptions()
	}

	state := &ImportedState{
		Generation: export.Generation,
		SessionID:  sessionID,
	}

	// Phase 1: Build all data (validation phase)
	// If any step fails, we haven't modified anything

	// Import proof data
	proofData, err := s.importProofIndex(export.Indexes.Proof)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "import proof index failed")
		importErrorsTotal.WithLabelValues(sessionID, "proof").Inc()
		return nil, fmt.Errorf("importing proof index: %w", err)
	}

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	// Import constraint data
	constraintData, err := s.importConstraintIndex(export.Indexes.Constraint)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "import constraint index failed")
		importErrorsTotal.WithLabelValues(sessionID, "constraint").Inc()
		return nil, fmt.Errorf("importing constraint index: %w", err)
	}

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	// Import similarity data (reconstructs both directions)
	similarityData, actualSimPairs, err := s.importSimilarityIndex(export.Indexes.Similarity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "import similarity index failed")
		importErrorsTotal.WithLabelValues(sessionID, "similarity").Inc()
		return nil, fmt.Errorf("importing similarity index: %w", err)
	}

	// Validate count (if strict and not truncated)
	if effectiveOpts.StrictValidation && !export.Indexes.Similarity.Truncated {
		// actualSimPairs is the count from exported pairs (one direction)
		// PairCount may count both directions, so we compare to pairs length
		if len(export.Indexes.Similarity.Pairs) > 0 && actualSimPairs != len(export.Indexes.Similarity.Pairs) {
			err := fmt.Errorf("similarity count mismatch: expected %d pairs, got %d",
				len(export.Indexes.Similarity.Pairs), actualSimPairs)
			span.RecordError(err)
			span.SetStatus(codes.Error, "similarity count mismatch")
			importErrorsTotal.WithLabelValues(sessionID, "validation").Inc()
			return nil, err
		}
	}

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	// Import dependency data (deduplicates and builds forward/reverse maps)
	dependencyForward, dependencyReverse, actualEdges, err := s.importDependencyIndex(export.Indexes.Dependency)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "import dependency index failed")
		importErrorsTotal.WithLabelValues(sessionID, "dependency").Inc()
		return nil, fmt.Errorf("importing dependency index: %w", err)
	}

	// Validate count (if strict, not truncated, and not graph-backed)
	if effectiveOpts.StrictValidation &&
		!export.Indexes.Dependency.Truncated &&
		export.Indexes.Dependency.Source != "graph_backed" {
		if len(export.Indexes.Dependency.Edges) > 0 && actualEdges != len(export.Indexes.Dependency.Edges) {
			err := fmt.Errorf("dependency count mismatch: expected %d edges, got %d",
				len(export.Indexes.Dependency.Edges), actualEdges)
			span.RecordError(err)
			span.SetStatus(codes.Error, "dependency count mismatch")
			importErrorsTotal.WithLabelValues(sessionID, "validation").Inc()
			return nil, err
		}
	}

	select {
	case <-ctx.Done():
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	default:
	}

	// Import history data
	historyData, err := s.importHistoryIndex(export.Indexes.History)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "import history index failed")
		importErrorsTotal.WithLabelValues(sessionID, "history").Inc()
		return nil, fmt.Errorf("importing history index: %w", err)
	}

	// Phase 2: Assign all data to result (commit phase)
	state.ProofData = proofData
	state.ConstraintData = constraintData
	state.SimilarityData = similarityData
	state.DependencyForward = dependencyForward
	state.DependencyReverse = dependencyReverse
	state.HistoryData = historyData

	duration := time.Since(start)
	importDuration.WithLabelValues(sessionID).Observe(duration.Seconds())

	span.SetAttributes(
		attribute.Int("proof_entries", len(proofData)),
		attribute.Int("constraint_entries", len(constraintData)),
		attribute.Int("similarity_pairs", actualSimPairs),
		attribute.Int("dependency_edges", actualEdges),
		attribute.Int("history_entries", len(historyData)),
	)

	logger.Info("CRS import complete",
		slog.Duration("duration", duration),
		slog.Int("proof_entries", len(proofData)),
		slog.Int("similarity_pairs", actualSimPairs),
		slog.Int("dependency_edges", actualEdges),
	)

	return state, nil
}

// ImportedState contains the data reconstructed from a CRS export.
//
// Description:
//
//	This struct holds all the data needed to restore CRS state. It uses
//	the internal data structures that CRS expects, allowing direct
//	restoration without additional transformation.
type ImportedState struct {
	// Generation is the CRS generation from the export.
	Generation int64

	// SessionID is the session identifier from the export.
	SessionID string

	// ProofData contains proof numbers keyed by node ID.
	ProofData map[string]ProofNumber

	// ConstraintData contains constraints keyed by constraint ID.
	ConstraintData map[string]Constraint

	// SimilarityData contains similarity scores (bidirectional).
	// Format: map[fromID]map[toID]similarity
	SimilarityData map[string]map[string]float64

	// DependencyForward contains forward edges: node -> nodes it depends on.
	DependencyForward map[string]map[string]struct{}

	// DependencyReverse contains reverse edges: node -> nodes that depend on it.
	DependencyReverse map[string]map[string]struct{}

	// HistoryData contains history entries in order.
	HistoryData []HistoryEntry
}

// -----------------------------------------------------------------------------
// Import Helpers
// -----------------------------------------------------------------------------

func (s *Serializer) importProofIndex(export ProofIndexExport) (map[string]ProofNumber, error) {
	result := make(map[string]ProofNumber, len(export.Entries))

	for _, entry := range export.Entries {
		if entry.NodeID == "" {
			return nil, fmt.Errorf("proof entry has empty node_id")
		}

		result[entry.NodeID] = ProofNumber{
			Proof:     entry.Proof,
			Disproof:  entry.Disproof,
			Status:    parseProofStatus(entry.Status),
			Source:    parseSignalSource(entry.Source),
			UpdatedAt: entry.UpdatedAt.UnixMilli(),
		}
	}

	return result, nil
}

func (s *Serializer) importConstraintIndex(export ConstraintIndexExport) (map[string]Constraint, error) {
	result := make(map[string]Constraint, len(export.Constraints))

	for _, entry := range export.Constraints {
		if entry.ID == "" {
			return nil, fmt.Errorf("constraint entry has empty id")
		}

		// Deep copy nodes slice
		nodesCopy := make([]string, len(entry.Nodes))
		copy(nodesCopy, entry.Nodes)

		result[entry.ID] = Constraint{
			ID:         entry.ID,
			Type:       parseConstraintType(entry.Type),
			Nodes:      nodesCopy,
			Expression: entry.Expression,
			Active:     entry.Active,
			Source:     parseSignalSource(entry.Source),
			CreatedAt:  entry.CreatedAt.UnixMilli(),
		}
	}

	return result, nil
}

func (s *Serializer) importSimilarityIndex(export SimilarityIndexExport) (map[string]map[string]float64, int, error) {
	result := make(map[string]map[string]float64)
	actualPairs := 0

	for _, pair := range export.Pairs {
		if pair.FromID == "" || pair.ToID == "" {
			return nil, 0, fmt.Errorf("similarity pair has empty from_id or to_id")
		}

		// Validate similarity is in valid range [0.0, 1.0]
		if pair.Similarity < 0 || pair.Similarity > 1 {
			return nil, 0, fmt.Errorf("similarity value out of range [0,1]: %f for pair %s->%s",
				pair.Similarity, pair.FromID, pair.ToID)
		}

		// Store forward direction
		if result[pair.FromID] == nil {
			result[pair.FromID] = make(map[string]float64)
		}
		result[pair.FromID][pair.ToID] = pair.Similarity

		// Store reverse direction (similarity is symmetric)
		if result[pair.ToID] == nil {
			result[pair.ToID] = make(map[string]float64)
		}
		result[pair.ToID][pair.FromID] = pair.Similarity

		actualPairs++
	}

	return result, actualPairs, nil
}

func (s *Serializer) importDependencyIndex(export DependencyIndexExport) (map[string]map[string]struct{}, map[string]map[string]struct{}, int, error) {
	// Skip import for graph-backed (edges come from graph, not CRS)
	if export.Source == "graph_backed" {
		s.logger.Debug("skipping dependency edge import for graph-backed index")
		return make(map[string]map[string]struct{}), make(map[string]map[string]struct{}), 0, nil
	}

	// Use maps for deduplication during import
	forward := make(map[string]map[string]struct{})
	reverse := make(map[string]map[string]struct{})
	actualEdges := 0

	for _, edge := range export.Edges {
		if edge.FromID == "" || edge.ToID == "" {
			return nil, nil, 0, fmt.Errorf("dependency edge has empty from_id or to_id")
		}

		// Check for duplicate (deduplication)
		if forward[edge.FromID] == nil {
			forward[edge.FromID] = make(map[string]struct{})
		}
		if _, exists := forward[edge.FromID][edge.ToID]; exists {
			// Duplicate edge, skip
			continue
		}

		// Store forward edge
		forward[edge.FromID][edge.ToID] = struct{}{}

		// Store reverse edge
		if reverse[edge.ToID] == nil {
			reverse[edge.ToID] = make(map[string]struct{})
		}
		reverse[edge.ToID][edge.FromID] = struct{}{}

		actualEdges++
	}

	return forward, reverse, actualEdges, nil
}

func (s *Serializer) importHistoryIndex(export HistoryIndexExport) ([]HistoryEntry, error) {
	result := make([]HistoryEntry, 0, len(export.RecentEntries))

	for _, entry := range export.RecentEntries {
		if entry.ID == "" {
			return nil, fmt.Errorf("history entry has empty id")
		}

		// Deep copy metadata
		var metadataCopy map[string]string
		if entry.Metadata != nil {
			metadataCopy = make(map[string]string, len(entry.Metadata))
			for k, v := range entry.Metadata {
				metadataCopy[k] = v
			}
		}

		result = append(result, HistoryEntry{
			ID:        entry.ID,
			NodeID:    entry.NodeID,
			Action:    entry.Action,
			Result:    entry.Result,
			Source:    parseSignalSource(entry.Source),
			Timestamp: entry.Timestamp,
			Metadata:  metadataCopy,
		})
	}

	return result, nil
}

// -----------------------------------------------------------------------------
// Parse Helpers
// -----------------------------------------------------------------------------

func parseProofStatus(s string) ProofStatus {
	switch s {
	case "proven":
		return ProofStatusProven
	case "disproven":
		return ProofStatusDisproven
	case "expanded":
		return ProofStatusExpanded
	default:
		return ProofStatusUnknown
	}
}

func parseSignalSource(s string) SignalSource {
	switch s {
	case "hard":
		return SignalSourceHard
	case "soft":
		return SignalSourceSoft
	case "safety":
		return SignalSourceSafety
	default:
		return SignalSourceUnknown
	}
}

func parseConstraintType(s string) ConstraintType {
	switch s {
	case "mutual_exclusion":
		return ConstraintTypeMutualExclusion
	case "implication":
		return ConstraintTypeImplication
	case "ordering":
		return ConstraintTypeOrdering
	case "resource":
		return ConstraintTypeResource
	default:
		return ConstraintTypeUnknown
	}
}
