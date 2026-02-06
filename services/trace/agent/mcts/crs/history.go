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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ErrDeltaHistoryClosed is returned when querying a closed delta history worker.
var ErrDeltaHistoryClosed = errors.New("delta history worker is closed")

// -----------------------------------------------------------------------------
// Metrics
// -----------------------------------------------------------------------------

var (
	deltaHistoryRecordsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crs_delta_history_records_total",
		Help: "Total number of delta records added to history",
	})

	deltaHistorySizeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "crs_delta_history_size",
		Help: "Current number of records in delta history",
	})

	deltaHistoryQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crs_delta_history_query_duration_seconds",
		Help:    "Duration of delta history queries",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
	}, []string{"query_type"})

	deltaHistoryChannelFull = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crs_delta_history_channel_full_total",
		Help: "Number of records dropped due to full channel",
	})
)

// -----------------------------------------------------------------------------
// DeltaRecord
// -----------------------------------------------------------------------------

// DeltaRecord captures a single delta application with full context.
//
// Description:
//
//	Stores metadata about when and why a delta was applied, along with
//	the serialized delta itself. Uses stable string IDs for indexing
//	to avoid invalidation issues with slice-based indexing.
//
// Thread Safety: Immutable after creation.
type DeltaRecord struct {
	// ID is a stable identifier for this record (not slice index).
	ID string `json:"id"`

	// Generation is the CRS generation after this delta was applied.
	Generation int64 `json:"generation"`

	// Timestamp is when this delta was applied (Unix milliseconds UTC).
	Timestamp int64 `json:"timestamp"`

	// DeltaType identifies the type of delta (proof, constraint, etc.).
	DeltaType DeltaType `json:"delta_type"`

	// DeltaBytes contains the JSON-serialized delta.
	// Storing as bytes avoids interface serialization issues.
	DeltaBytes []byte `json:"delta_bytes"`

	// Source identifies what caused this delta (activity name, tool, etc.).
	Source string `json:"source"`

	// SessionID identifies the session that applied this delta.
	SessionID string `json:"session_id"`

	// Metadata contains additional context about the delta.
	Metadata map[string]string `json:"metadata,omitempty"`

	// AffectedNodes lists node IDs affected by this delta.
	AffectedNodes []string `json:"affected_nodes"`
}

// -----------------------------------------------------------------------------
// Channel-based Request/Response Types
// -----------------------------------------------------------------------------

// deltaRecordRequest is sent to the worker to record a new delta.
type deltaRecordRequest struct {
	delta     Delta
	gen       int64
	source    string
	sessionID string
	metadata  map[string]string
}

// deltaQueryType identifies the type of history query.
type deltaQueryType int

const (
	queryTypeRange deltaQueryType = iota
	queryTypeByNode
	queryTypeByGeneration
	queryTypeExplain
	queryTypeSize
	queryTypeAll
)

// deltaQueryRequest is sent to the worker to query history.
type deltaQueryRequest struct {
	ctx       context.Context
	queryType deltaQueryType
	fromGen   int64
	toGen     int64
	nodeID    string
	resultCh  chan deltaQueryResult
}

// deltaQueryResult is the response from the worker.
type deltaQueryResult struct {
	records []DeltaRecord
	size    int
	err     error
}

// -----------------------------------------------------------------------------
// DeltaHistoryWorker
// -----------------------------------------------------------------------------

// DeltaHistoryWorker manages delta history using a channel-based architecture.
//
// Description:
//
//	Uses buffered channels for parallel delta recording and queries.
//	A single goroutine owns all history state, eliminating mutex contention.
//	This design follows the "share memory by communicating" principle.
//
// Thread Safety: Safe for concurrent use. All operations go through channels.
type DeltaHistoryWorker struct {
	recordCh chan deltaRecordRequest
	queryCh  chan deltaQueryRequest
	closeCh  chan struct{}
	doneCh   chan struct{}

	maxRecords int
	logger     *slog.Logger

	// Atomic counter for generating stable IDs
	nextID atomic.Uint64
}

// DefaultMaxDeltaRecords is the default maximum number of delta records to keep.
const DefaultMaxDeltaRecords = 1000

// DefaultDeltaRecordChannelSize is the buffer size for the record channel.
const DefaultDeltaRecordChannelSize = 100

// DefaultDeltaQueryChannelSize is the buffer size for the query channel.
const DefaultDeltaQueryChannelSize = 10

// NewDeltaHistoryWorker creates a new delta history worker.
//
// Description:
//
//	Starts a background goroutine that owns all history state.
//	The worker accepts record and query requests via buffered channels.
//
// Inputs:
//   - maxRecords: Maximum records to keep (uses DefaultMaxDeltaRecords if <= 0).
//   - logger: Logger instance. If nil, uses slog.Default().
//
// Outputs:
//   - *DeltaHistoryWorker: The new worker. Never nil.
//
// Thread Safety: Safe for concurrent use.
func NewDeltaHistoryWorker(maxRecords int, logger *slog.Logger) *DeltaHistoryWorker {
	if maxRecords <= 0 {
		maxRecords = DefaultMaxDeltaRecords
	}
	if logger == nil {
		logger = slog.Default()
	}

	w := &DeltaHistoryWorker{
		recordCh:   make(chan deltaRecordRequest, DefaultDeltaRecordChannelSize),
		queryCh:    make(chan deltaQueryRequest, DefaultDeltaQueryChannelSize),
		closeCh:    make(chan struct{}),
		doneCh:     make(chan struct{}),
		maxRecords: maxRecords,
		logger:     logger.With(slog.String("component", "delta_history")),
	}

	go w.run()
	return w
}

// run is the main loop that owns all history state.
// Single goroutine - no mutex needed.
func (w *DeltaHistoryWorker) run() {
	defer close(w.doneCh)

	// All state owned by this goroutine
	records := make(map[string]*DeltaRecord, w.maxRecords)
	orderedIDs := make([]string, 0, w.maxRecords)
	byNode := make(map[string][]string) // nodeID -> []recordID
	byGen := make(map[int64]string)     // generation -> recordID

	for {
		select {
		case <-w.closeCh:
			w.logger.Debug("delta history worker shutting down",
				slog.Int("records", len(records)),
			)
			return

		case req := <-w.recordCh:
			// Generate stable ID
			id := fmt.Sprintf("delta_%d", w.nextID.Add(1))

			// Evict oldest if at capacity
			if len(orderedIDs) >= w.maxRecords {
				oldID := orderedIDs[0]
				if old := records[oldID]; old != nil {
					// Remove from byGen index
					delete(byGen, old.Generation)
					// Remove from byNode index
					for _, nodeID := range old.AffectedNodes {
						ids := byNode[nodeID]
						for i, oid := range ids {
							if oid == oldID {
								byNode[nodeID] = append(ids[:i], ids[i+1:]...)
								break
							}
						}
						// Clean up empty entries
						if len(byNode[nodeID]) == 0 {
							delete(byNode, nodeID)
						}
					}
				}
				delete(records, oldID)
				orderedIDs = orderedIDs[1:]
			}

			// Serialize delta
			deltaBytes, err := serializeDelta(req.delta)
			if err != nil {
				w.logger.Warn("failed to serialize delta for history",
					slog.String("error", err.Error()),
					slog.Int64("generation", req.gen),
				)
				continue
			}

			// Extract affected nodes
			affectedNodes := extractAffectedNodes(req.delta)

			// Create record
			record := &DeltaRecord{
				ID:            id,
				Generation:    req.gen,
				Timestamp:     time.Now().UnixMilli(),
				DeltaType:     req.delta.Type(),
				DeltaBytes:    deltaBytes,
				Source:        req.source,
				SessionID:     req.sessionID,
				Metadata:      req.metadata,
				AffectedNodes: affectedNodes,
			}

			// Add to storage
			records[id] = record
			orderedIDs = append(orderedIDs, id)
			byGen[req.gen] = id

			// Index by affected nodes
			for _, nodeID := range affectedNodes {
				byNode[nodeID] = append(byNode[nodeID], id)
			}

			// Update metrics
			deltaHistoryRecordsTotal.Inc()
			deltaHistorySizeGauge.Set(float64(len(records)))

			w.logger.Debug("delta recorded in history",
				slog.String("id", id),
				slog.Int64("generation", req.gen),
				slog.String("type", req.delta.Type().String()),
				slog.Int("affected_nodes", len(affectedNodes)),
				slog.String("source", req.source),
			)

		case q := <-w.queryCh:
			// Check context cancellation
			if q.ctx != nil {
				select {
				case <-q.ctx.Done():
					q.resultCh <- deltaQueryResult{err: q.ctx.Err()}
					continue
				default:
				}
			}

			var result deltaQueryResult

			switch q.queryType {
			case queryTypeRange:
				var results []DeltaRecord
				for _, id := range orderedIDs {
					if r := records[id]; r != nil {
						if r.Generation > q.fromGen && r.Generation <= q.toGen {
							results = append(results, *r)
						}
					}
				}
				// Sort by generation (should already be ordered, but ensure)
				sort.Slice(results, func(i, j int) bool {
					return results[i].Generation < results[j].Generation
				})
				result.records = results

			case queryTypeByNode:
				var results []DeltaRecord
				for _, id := range byNode[q.nodeID] {
					if r := records[id]; r != nil {
						results = append(results, *r)
					}
				}
				// Sort chronologically
				sort.Slice(results, func(i, j int) bool {
					return results[i].Generation < results[j].Generation
				})
				result.records = results

			case queryTypeByGeneration:
				if id, ok := byGen[q.fromGen]; ok {
					if r := records[id]; r != nil {
						result.records = []DeltaRecord{*r}
					}
				}

			case queryTypeExplain:
				// Same as byNode but with causality ordering
				var results []DeltaRecord
				for _, id := range byNode[q.nodeID] {
					if r := records[id]; r != nil {
						results = append(results, *r)
					}
				}
				sort.Slice(results, func(i, j int) bool {
					return results[i].Generation < results[j].Generation
				})
				result.records = results

			case queryTypeSize:
				result.size = len(records)

			case queryTypeAll:
				results := make([]DeltaRecord, 0, len(orderedIDs))
				for _, id := range orderedIDs {
					if r := records[id]; r != nil {
						results = append(results, *r)
					}
				}
				result.records = results
			}

			q.resultCh <- result
		}
	}
}

// Record adds a delta to history.
//
// Description:
//
//	Non-blocking operation using a buffered channel. If the channel is full,
//	the record is dropped and a warning is logged.
//
// Inputs:
//   - delta: The delta that was applied. Must not be nil.
//   - gen: The CRS generation after applying the delta.
//   - source: What caused this delta (activity name, tool name, etc.).
//   - sessionID: The session that applied this delta.
//   - metadata: Optional additional context.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) Record(delta Delta, gen int64, source, sessionID string, metadata map[string]string) {
	if delta == nil {
		return
	}

	req := deltaRecordRequest{
		delta:     delta,
		gen:       gen,
		source:    source,
		sessionID: sessionID,
		metadata:  metadata,
	}

	select {
	case w.recordCh <- req:
		// Successfully queued
	default:
		// Channel full - log and drop
		deltaHistoryChannelFull.Inc()
		w.logger.Warn("delta history channel full, dropping record",
			slog.Int64("generation", gen),
			slog.String("source", source),
		)
	}
}

// GetRange returns deltas between two generations (exclusive start, inclusive end).
//
// Description:
//
//	Returns all deltas where fromGen < generation <= toGen, ordered by generation.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - fromGen: Exclusive lower bound (deltas after this generation).
//   - toGen: Inclusive upper bound (deltas up to and including this generation).
//
// Outputs:
//   - []DeltaRecord: Matching records, ordered by generation. Empty if none found.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) GetRange(ctx context.Context, fromGen, toGen int64) ([]DeltaRecord, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return nil, ErrDeltaHistoryClosed
	default:
	}

	ctx, span := otel.Tracer("crs").Start(ctx, "crs.DeltaHistory.GetRange",
		trace.WithAttributes(
			attribute.Int64("from_gen", fromGen),
			attribute.Int64("to_gen", toGen),
		),
	)
	defer span.End()

	timer := prometheus.NewTimer(deltaHistoryQueryDuration.WithLabelValues("range"))
	defer timer.ObserveDuration()

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeRange,
		fromGen:   fromGen,
		toGen:     toGen,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			span.RecordError(result.err)
			span.SetStatus(codes.Error, result.err.Error())
			return nil, result.err
		}
		span.SetAttributes(attribute.Int("result_count", len(result.records)))
		return result.records, nil
	}
}

// GetByNode returns all deltas that affected a specific node.
//
// Description:
//
//	Returns all deltas where the node appears in AffectedNodes, ordered chronologically.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - nodeID: The node to look up.
//
// Outputs:
//   - []DeltaRecord: Matching records, ordered by generation. Empty if none found.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) GetByNode(ctx context.Context, nodeID string) ([]DeltaRecord, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return nil, ErrDeltaHistoryClosed
	default:
	}

	ctx, span := otel.Tracer("crs").Start(ctx, "crs.DeltaHistory.GetByNode",
		trace.WithAttributes(
			attribute.String("node_id", nodeID),
		),
	)
	defer span.End()

	timer := prometheus.NewTimer(deltaHistoryQueryDuration.WithLabelValues("by_node"))
	defer timer.ObserveDuration()

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeByNode,
		nodeID:    nodeID,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			span.RecordError(result.err)
			span.SetStatus(codes.Error, result.err.Error())
			return nil, result.err
		}
		span.SetAttributes(attribute.Int("result_count", len(result.records)))
		return result.records, nil
	}
}

// GetByGeneration returns the delta applied at a specific generation.
//
// Description:
//
//	Returns the single delta that was applied to reach the specified generation.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - gen: The generation to look up.
//
// Outputs:
//   - DeltaRecord: The matching record (zero value if not found).
//   - bool: True if a record was found.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) GetByGeneration(ctx context.Context, gen int64) (DeltaRecord, bool, error) {
	if ctx == nil {
		return DeltaRecord{}, false, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return DeltaRecord{}, false, ErrDeltaHistoryClosed
	default:
	}

	ctx, span := otel.Tracer("crs").Start(ctx, "crs.DeltaHistory.GetByGeneration",
		trace.WithAttributes(
			attribute.Int64("generation", gen),
		),
	)
	defer span.End()

	timer := prometheus.NewTimer(deltaHistoryQueryDuration.WithLabelValues("by_generation"))
	defer timer.ObserveDuration()

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeByGeneration,
		fromGen:   gen,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return DeltaRecord{}, false, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return DeltaRecord{}, false, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			span.RecordError(result.err)
			span.SetStatus(codes.Error, result.err.Error())
			return DeltaRecord{}, false, result.err
		}
		if len(result.records) == 0 {
			span.SetAttributes(attribute.Bool("found", false))
			return DeltaRecord{}, false, nil
		}
		span.SetAttributes(attribute.Bool("found", true))
		return result.records[0], true, nil
	}
}

// Explain returns the causality chain for a node's current state.
//
// Description:
//
//	Returns all deltas that affected the node, in chronological order.
//	This provides a "reasoning trace" showing how the node reached its current state.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - nodeID: The node to explain.
//
// Outputs:
//   - []DeltaRecord: All deltas affecting this node, ordered chronologically.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) Explain(ctx context.Context, nodeID string) ([]DeltaRecord, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return nil, ErrDeltaHistoryClosed
	default:
	}

	ctx, span := otel.Tracer("crs").Start(ctx, "crs.DeltaHistory.Explain",
		trace.WithAttributes(
			attribute.String("node_id", nodeID),
		),
	)
	defer span.End()

	timer := prometheus.NewTimer(deltaHistoryQueryDuration.WithLabelValues("explain"))
	defer timer.ObserveDuration()

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeExplain,
		nodeID:    nodeID,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			span.RecordError(result.err)
			span.SetStatus(codes.Error, result.err.Error())
			return nil, result.err
		}
		span.SetAttributes(attribute.Int("result_count", len(result.records)))
		return result.records, nil
	}
}

// Size returns the current number of records in history.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - int: Number of records.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) Size(ctx context.Context) (int, error) {
	if ctx == nil {
		return 0, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return 0, ErrDeltaHistoryClosed
	default:
	}

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeSize,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case result := <-resultCh:
		return result.size, result.err
	}
}

// All returns all records in chronological order.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//
// Outputs:
//   - []DeltaRecord: All records in chronological order.
//   - error: Non-nil on context cancellation.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) All(ctx context.Context) ([]DeltaRecord, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	// Check if worker is closed before attempting to query
	select {
	case <-w.closeCh:
		return nil, ErrDeltaHistoryClosed
	default:
	}

	resultCh := make(chan deltaQueryResult, 1)
	req := deltaQueryRequest{
		ctx:       ctx,
		queryType: queryTypeAll,
		resultCh:  resultCh,
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case w.queryCh <- req:
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.records, result.err
	}
}

// Close stops the worker goroutine.
//
// Description:
//
//	Signals the worker to stop and waits for it to finish.
//	Safe to call multiple times.
//
// Thread Safety: Safe for concurrent use.
func (w *DeltaHistoryWorker) Close() {
	select {
	case <-w.closeCh:
		// Already closed
		return
	default:
		close(w.closeCh)
	}
	// Wait for worker to finish
	<-w.doneCh
}

// -----------------------------------------------------------------------------
// Delta Serialization
// -----------------------------------------------------------------------------

// serializeDelta converts a Delta to JSON bytes.
func serializeDelta(delta Delta) ([]byte, error) {
	// Create a wrapper that includes the type
	wrapper := struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}{
		Type: delta.Type().String(),
		Data: delta,
	}
	return json.Marshal(wrapper)
}

// -----------------------------------------------------------------------------
// Affected Nodes Extraction
// -----------------------------------------------------------------------------

// extractAffectedNodes returns node IDs affected by a delta.
//
// Description:
//
//	Extracts all node IDs that would be modified by applying this delta.
//	Handles all known delta types including composite deltas.
//
// Inputs:
//   - delta: The delta to analyze.
//
// Outputs:
//   - []string: Affected node IDs. Returns empty slice (not nil) for unknown types.
//
// Thread Safety: Safe for concurrent use (does not modify delta).
func extractAffectedNodes(delta Delta) []string {
	if delta == nil {
		return []string{}
	}

	switch d := delta.(type) {
	case *ProofDelta:
		nodes := make([]string, 0, len(d.Updates))
		for nodeID := range d.Updates {
			nodes = append(nodes, nodeID)
		}
		return nodes

	case *ConstraintDelta:
		seen := make(map[string]struct{})
		// Nodes from added constraints
		for _, c := range d.Add {
			for _, n := range c.Nodes {
				seen[n] = struct{}{}
			}
		}
		// Constraint IDs being removed
		for _, id := range d.Remove {
			seen[id] = struct{}{}
		}
		// Nodes from updated constraints
		for _, c := range d.Update {
			for _, n := range c.Nodes {
				seen[n] = struct{}{}
			}
		}
		nodes := make([]string, 0, len(seen))
		for n := range seen {
			nodes = append(nodes, n)
		}
		return nodes

	case *SimilarityDelta:
		seen := make(map[string]struct{})
		for pair := range d.Updates {
			seen[pair[0]] = struct{}{}
			seen[pair[1]] = struct{}{}
		}
		nodes := make([]string, 0, len(seen))
		for n := range seen {
			nodes = append(nodes, n)
		}
		return nodes

	case *DependencyDelta:
		seen := make(map[string]struct{})
		for _, edge := range d.AddEdges {
			seen[edge[0]] = struct{}{}
			seen[edge[1]] = struct{}{}
		}
		for _, edge := range d.RemoveEdges {
			seen[edge[0]] = struct{}{}
			seen[edge[1]] = struct{}{}
		}
		nodes := make([]string, 0, len(seen))
		for n := range seen {
			nodes = append(nodes, n)
		}
		return nodes

	case *HistoryDelta:
		nodes := make([]string, 0, len(d.Entries))
		for _, e := range d.Entries {
			if e.NodeID != "" {
				nodes = append(nodes, e.NodeID)
			}
		}
		return nodes

	case *StreamingDelta:
		// Streaming deltas track items, not nodes in the traditional sense
		nodes := make([]string, 0, len(d.Increments)+len(d.CardinalityItems))
		for item := range d.Increments {
			nodes = append(nodes, item)
		}
		for _, item := range d.CardinalityItems {
			nodes = append(nodes, item)
		}
		return nodes

	case *CompositeDelta:
		// Flatten all sub-deltas
		seen := make(map[string]struct{})
		for _, sub := range d.Deltas {
			for _, n := range extractAffectedNodes(sub) {
				seen[n] = struct{}{}
			}
		}
		nodes := make([]string, 0, len(seen))
		for n := range seen {
			nodes = append(nodes, n)
		}
		return nodes

	default:
		// Unknown delta type - return empty slice, not nil
		return []string{}
	}
}

// -----------------------------------------------------------------------------
// DeltaHistoryView Interface
// -----------------------------------------------------------------------------

// DeltaHistoryView provides read-only access to delta history from snapshots.
//
// Thread Safety: All methods are safe for concurrent use.
type DeltaHistoryView interface {
	// GetRange returns deltas between two generations (exclusive start, inclusive end).
	GetRange(ctx context.Context, fromGen, toGen int64) ([]DeltaRecord, error)

	// GetByNode returns all deltas that affected a specific node.
	GetByNode(ctx context.Context, nodeID string) ([]DeltaRecord, error)

	// GetByGeneration returns the delta applied at a specific generation.
	GetByGeneration(ctx context.Context, gen int64) (DeltaRecord, bool, error)

	// Explain returns the causality chain for a node's current state.
	Explain(ctx context.Context, nodeID string) ([]DeltaRecord, error)

	// Size returns the current number of records in history.
	Size(ctx context.Context) (int, error)
}

// Verify DeltaHistoryWorker implements DeltaHistoryView
var _ DeltaHistoryView = (*DeltaHistoryWorker)(nil)
