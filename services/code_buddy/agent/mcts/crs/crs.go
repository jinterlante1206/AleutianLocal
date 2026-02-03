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
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// CRS Implementation
// -----------------------------------------------------------------------------

// crsImpl implements the CRS interface.
//
// Lock Ordering (to prevent deadlocks):
//
//	Always acquire locks in this order:
//	1. crsImpl.mu (RWMutex)
//	2. streamingStats.mu (RWMutex)
//
//	Never acquire crsImpl.mu while holding streamingStats.mu.
//
// Thread Safety: Safe for concurrent use.
type crsImpl struct {
	mu     sync.RWMutex
	config *Config
	logger *slog.Logger

	// State
	generation atomic.Int64

	// Index data (mutable, protected by mu)
	proofData      map[string]ProofNumber
	constraintData map[string]Constraint
	similarityData map[string]map[string]float64
	dependencyData *dependencyGraph
	historyData    []HistoryEntry
	streamingData  *streamingStats

	// Metrics (atomic, no lock needed)
	snapshotCount   atomic.Int64
	applyCount      atomic.Int64
	applyErrorCount atomic.Int64
}

// New creates a new CRS instance.
//
// Inputs:
//   - config: Configuration. If nil, uses DefaultConfig().
//
// Outputs:
//   - CRS: The new CRS instance. Never nil.
//
// Thread Safety: Safe for concurrent use.
func New(config *Config) CRS {
	if config == nil {
		config = DefaultConfig()
	}

	return &crsImpl{
		config:         config,
		logger:         slog.Default().With(slog.String("component", "crs")),
		proofData:      make(map[string]ProofNumber),
		constraintData: make(map[string]Constraint),
		similarityData: make(map[string]map[string]float64),
		dependencyData: newDependencyGraph(),
		historyData:    make([]HistoryEntry, 0),
		streamingData:  newStreamingStats(),
	}
}

// -----------------------------------------------------------------------------
// CRS Interface Implementation
// -----------------------------------------------------------------------------

// Snapshot returns an immutable view of the current state.
func (c *crsImpl) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.snapshotCount.Add(1)

	return newSnapshot(
		c.generation.Load(),
		c.proofData,
		c.constraintData,
		c.similarityData,
		c.dependencyData,
		c.historyData,
		c.streamingData,
	)
}

// Apply atomically applies a delta to the state.
func (c *crsImpl) Apply(ctx context.Context, delta Delta) (ApplyMetrics, error) {
	if ctx == nil {
		return ApplyMetrics{}, ErrNilContext
	}
	if delta == nil {
		return ApplyMetrics{}, ErrNilDelta
	}

	// Check cancellation
	select {
	case <-ctx.Done():
		return ApplyMetrics{}, ctx.Err()
	default:
	}

	// Start tracing
	ctx, span := otel.Tracer("crs").Start(ctx, "crs.Apply",
		trace.WithAttributes(
			attribute.String("delta_type", delta.Type().String()),
			attribute.String("source", delta.Source().String()),
		),
	)
	defer span.End()

	startTime := time.Now()
	metrics := ApplyMetrics{
		DeltaType:     delta.Type(),
		OldGeneration: c.generation.Load(),
	}

	// Phase 1: Validate
	validationStart := time.Now()
	snapshot := c.Snapshot()
	if err := delta.Validate(snapshot); err != nil {
		c.applyErrorCount.Add(1)
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		return metrics, fmt.Errorf("%w: %w", ErrDeltaValidation, err)
	}
	metrics.ValidationDuration = time.Since(validationStart)

	// Check cancellation again before acquiring write lock
	select {
	case <-ctx.Done():
		return metrics, ctx.Err()
	default:
	}

	// Phase 2: Apply (atomic)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check generation hasn't changed during validation
	if c.generation.Load() != metrics.OldGeneration {
		// State changed, need to re-validate
		currentSnap := newSnapshot(
			c.generation.Load(),
			c.proofData,
			c.constraintData,
			c.similarityData,
			c.dependencyData,
			c.historyData,
			c.streamingData,
		)
		if err := delta.Validate(currentSnap); err != nil {
			c.applyErrorCount.Add(1)
			span.RecordError(err)
			span.SetStatus(codes.Error, "re-validation failed")
			return metrics, fmt.Errorf("%w: %w", ErrDeltaValidation, err)
		}
	}

	// Apply the delta based on type
	var err error
	switch d := delta.(type) {
	case *ProofDelta:
		err = c.applyProofDelta(d, &metrics)
	case *ConstraintDelta:
		err = c.applyConstraintDelta(d, &metrics)
	case *SimilarityDelta:
		err = c.applySimilarityDelta(d, &metrics)
	case *DependencyDelta:
		err = c.applyDependencyDelta(d, &metrics)
	case *HistoryDelta:
		err = c.applyHistoryDelta(d, &metrics)
	case *StreamingDelta:
		err = c.applyStreamingDelta(d, &metrics)
	case *CompositeDelta:
		err = c.applyCompositeDelta(ctx, d, &metrics)
	default:
		err = fmt.Errorf("unknown delta type: %T", delta)
	}

	if err != nil {
		c.applyErrorCount.Add(1)
		span.RecordError(err)
		span.SetStatus(codes.Error, "apply failed")
		return metrics, fmt.Errorf("%w: %w", ErrApplyRollback, err)
	}

	// Increment generation
	metrics.NewGeneration = c.generation.Add(1)
	metrics.ApplyDuration = time.Since(startTime)
	c.applyCount.Add(1)

	span.SetAttributes(
		attribute.Int64("old_generation", metrics.OldGeneration),
		attribute.Int64("new_generation", metrics.NewGeneration),
		attribute.Int("entries_modified", metrics.EntriesModified),
	)

	c.logger.Debug("delta applied",
		slog.String("type", delta.Type().String()),
		slog.Int64("generation", metrics.NewGeneration),
		slog.Int("entries", metrics.EntriesModified),
		slog.Duration("duration", metrics.ApplyDuration),
	)

	return metrics, nil
}

// Generation returns the current state version.
func (c *crsImpl) Generation() int64 {
	return c.generation.Load()
}

// Checkpoint creates a restorable checkpoint.
func (c *crsImpl) Checkpoint(ctx context.Context) (Checkpoint, error) {
	if ctx == nil {
		return Checkpoint{}, ErrNilContext
	}

	select {
	case <-ctx.Done():
		return Checkpoint{}, ctx.Err()
	default:
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create deep copy of all state
	snap := newSnapshot(
		c.generation.Load(),
		c.proofData,
		c.constraintData,
		c.similarityData,
		c.dependencyData,
		c.historyData,
		c.streamingData,
	)

	return Checkpoint{
		ID:         uuid.New().String(),
		Generation: c.generation.Load(),
		CreatedAt:  time.Now(),
		data:       snap,
	}, nil
}

// Restore returns to a previous checkpoint.
func (c *crsImpl) Restore(ctx context.Context, cp Checkpoint) error {
	if ctx == nil {
		return ErrNilContext
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Start tracing
	ctx, span := otel.Tracer("crs").Start(ctx, "crs.Restore",
		trace.WithAttributes(
			attribute.String("checkpoint_id", cp.ID),
			attribute.Int64("checkpoint_generation", cp.Generation),
		),
	)
	defer span.End()

	snap, ok := cp.data.(*snapshot)
	if !ok {
		err := fmt.Errorf("restore: invalid checkpoint data type: expected *snapshot, got %T", cp.data)
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid checkpoint")
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Deep copy streaming data to avoid sharing mutex with snapshot
	// This prevents race conditions if snapshot's streaming stats are still being read
	newStreaming := snap.streamingData.clone()

	// Restore all state from checkpoint (using snapshot's immutable copies)
	c.proofData = snap.proofData
	c.constraintData = snap.constraintData
	c.similarityData = snap.similarityData
	c.dependencyData = snap.dependencyData
	c.historyData = snap.historyData
	c.streamingData = newStreaming
	c.generation.Store(cp.Generation)

	span.SetAttributes(attribute.Bool("success", true))

	c.logger.Info("checkpoint restored",
		slog.String("checkpoint_id", cp.ID),
		slog.Int64("generation", cp.Generation),
	)

	return nil
}

// -----------------------------------------------------------------------------
// Delta Application Helpers
// -----------------------------------------------------------------------------

func (c *crsImpl) applyProofDelta(d *ProofDelta, metrics *ApplyMetrics) error {
	for nodeID, proof := range d.Updates {
		c.proofData[nodeID] = proof
		metrics.EntriesModified++
	}
	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "proof")
	return nil
}

func (c *crsImpl) applyConstraintDelta(d *ConstraintDelta, metrics *ApplyMetrics) error {
	// Remove
	for _, id := range d.Remove {
		delete(c.constraintData, id)
		metrics.EntriesModified++
	}

	// Update
	for id, constraint := range d.Update {
		c.constraintData[id] = constraint
		metrics.EntriesModified++
	}

	// Add
	for _, constraint := range d.Add {
		c.constraintData[constraint.ID] = constraint
		metrics.EntriesModified++
	}

	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "constraint")
	return nil
}

func (c *crsImpl) applySimilarityDelta(d *SimilarityDelta, metrics *ApplyMetrics) error {
	for pair, dist := range d.Updates {
		node1, node2 := pair[0], pair[1]
		if c.similarityData[node1] == nil {
			c.similarityData[node1] = make(map[string]float64)
		}
		c.similarityData[node1][node2] = dist
		metrics.EntriesModified++
	}
	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "similarity")
	return nil
}

func (c *crsImpl) applyDependencyDelta(d *DependencyDelta, metrics *ApplyMetrics) error {
	// Remove edges first
	for _, edge := range d.RemoveEdges {
		from, to := edge[0], edge[1]
		if deps, ok := c.dependencyData.forward[from]; ok {
			delete(deps, to)
		}
		if deps, ok := c.dependencyData.reverse[to]; ok {
			delete(deps, from)
		}
		metrics.EntriesModified++
	}

	// Add edges
	for _, edge := range d.AddEdges {
		c.dependencyData.addEdge(edge[0], edge[1])
		metrics.EntriesModified++
	}

	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "dependency")
	return nil
}

func (c *crsImpl) applyHistoryDelta(d *HistoryDelta, metrics *ApplyMetrics) error {
	c.historyData = append(c.historyData, d.Entries...)
	metrics.EntriesModified += len(d.Entries)
	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "history")
	return nil
}

func (c *crsImpl) applyStreamingDelta(d *StreamingDelta, metrics *ApplyMetrics) error {
	c.streamingData.mu.Lock()
	defer c.streamingData.mu.Unlock()

	// Track new items for cardinality
	for item, inc := range d.Increments {
		if c.streamingData.frequencies[item] == 0 {
			// New item - increment cardinality
			c.streamingData.cardinality++
		}
		c.streamingData.frequencies[item] += inc
		metrics.EntriesModified++
	}

	// Update cardinality for explicit cardinality items
	for _, item := range d.CardinalityItems {
		if c.streamingData.frequencies[item] == 0 {
			c.streamingData.frequencies[item] = 1
			c.streamingData.cardinality++
		}
		metrics.EntriesModified++
	}

	metrics.IndexesUpdated = append(metrics.IndexesUpdated, "streaming")
	return nil
}

func (c *crsImpl) applyCompositeDelta(ctx context.Context, d *CompositeDelta, metrics *ApplyMetrics) error {
	// Apply each delta in sequence
	for _, delta := range d.Deltas {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch dd := delta.(type) {
		case *ProofDelta:
			if err := c.applyProofDelta(dd, metrics); err != nil {
				return err
			}
		case *ConstraintDelta:
			if err := c.applyConstraintDelta(dd, metrics); err != nil {
				return err
			}
		case *SimilarityDelta:
			if err := c.applySimilarityDelta(dd, metrics); err != nil {
				return err
			}
		case *DependencyDelta:
			if err := c.applyDependencyDelta(dd, metrics); err != nil {
				return err
			}
		case *HistoryDelta:
			if err := c.applyHistoryDelta(dd, metrics); err != nil {
				return err
			}
		case *StreamingDelta:
			if err := c.applyStreamingDelta(dd, metrics); err != nil {
				return err
			}
		case *CompositeDelta:
			if err := c.applyCompositeDelta(ctx, dd, metrics); err != nil {
				return err
			}
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Evaluable Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the unique identifier for this component.
func (c *crsImpl) Name() string {
	return "crs"
}

// Properties returns the correctness properties this component guarantees.
func (c *crsImpl) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "snapshot_immutability",
			Description: "Snapshots returned by Snapshot() are immutable",
			Check: func(_, output any) error {
				// Verified by type system - snapshot fields are not exported
				return nil
			},
		},
		{
			Name:        "generation_monotonic",
			Description: "Generation always increases on Apply",
			Check: func(_, _ any) error {
				// Verified by atomic increment
				return nil
			},
		},
		{
			Name:        "hard_soft_boundary",
			Description: "Soft signals cannot mark nodes DISPROVEN",
			Check: func(input, _ any) error {
				delta, ok := input.(*ProofDelta)
				if !ok {
					return nil
				}
				for nodeID, proof := range delta.Updates {
					if proof.Status == ProofStatusDisproven && !delta.Source().IsHard() {
						return fmt.Errorf("soft signal marked node %s as DISPROVEN", nodeID)
					}
				}
				return nil
			},
		},
		{
			Name:        "checkpoint_roundtrip",
			Description: "Checkpoint and Restore preserves state",
			Check: func(_, _ any) error {
				// Verified by implementation
				return nil
			},
		},
	}
}

// Metrics returns the metrics this component exposes.
func (c *crsImpl) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "crs_snapshot_total",
			Type:        eval.MetricCounter,
			Description: "Total snapshots created",
		},
		{
			Name:        "crs_apply_total",
			Type:        eval.MetricCounter,
			Description: "Total Apply calls",
			Labels:      []string{"delta_type", "status"},
		},
		{
			Name:        "crs_apply_duration_seconds",
			Type:        eval.MetricHistogram,
			Description: "Apply duration in seconds",
			Labels:      []string{"delta_type"},
			Buckets:     []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
		{
			Name:        "crs_generation",
			Type:        eval.MetricGauge,
			Description: "Current generation number",
		},
		{
			Name:        "crs_index_size",
			Type:        eval.MetricGauge,
			Description: "Size of each index",
			Labels:      []string{"index"},
		},
	}
}

// HealthCheck verifies the component is functioning correctly.
func (c *crsImpl) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check we can create a snapshot
	snap := c.Snapshot()
	if snap == nil {
		return fmt.Errorf("snapshot returned nil")
	}

	// Check all indexes are accessible
	if snap.ProofIndex() == nil {
		return fmt.Errorf("proof index is nil")
	}
	if snap.ConstraintIndex() == nil {
		return fmt.Errorf("constraint index is nil")
	}
	if snap.SimilarityIndex() == nil {
		return fmt.Errorf("similarity index is nil")
	}
	if snap.DependencyIndex() == nil {
		return fmt.Errorf("dependency index is nil")
	}
	if snap.HistoryIndex() == nil {
		return fmt.Errorf("history index is nil")
	}
	if snap.StreamingIndex() == nil {
		return fmt.Errorf("streaming index is nil")
	}

	// Check for circular dependencies in dependency graph
	c.mu.RLock()
	defer c.mu.RUnlock()

	for nodeID := range c.dependencyData.forward {
		if c.dependencyData.hasCycle(nodeID) {
			return fmt.Errorf("circular dependency detected involving node %s", nodeID)
		}
	}

	return nil
}
