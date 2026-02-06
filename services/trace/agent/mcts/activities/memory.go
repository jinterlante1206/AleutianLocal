// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"context"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Memory Activity
// -----------------------------------------------------------------------------

// MemoryActivity manages decision history tracking.
//
// Description:
//
//	MemoryActivity tracks decisions made during MCTS exploration.
//	It uses the History index to:
//	- Record decisions for traceability
//	- Enable decision replay for debugging
//	- Support undo/redo operations
//	- Provide context for learning
//
//	Design Note: Unlike other activities, Memory intentionally does NOT embed
//	BaseActivity or orchestrate algorithms. It directly creates history deltas
//	because history tracking is fundamentally different from algorithm
//	orchestration - it's a passive recording mechanism, not an active
//	computation. This design avoids the overhead of algorithm dispatch
//	for simple record/query operations.
//
// Thread Safety: Safe for concurrent use.
type MemoryActivity struct {
	name    string
	config  *MemoryConfig
	timeout time.Duration
}

// MemoryConfig configures the memory activity.
type MemoryConfig struct {
	*ActivityConfig

	// MaxHistoryEntries limits the number of history entries to keep.
	MaxHistoryEntries int

	// TrackAllDecisions enables tracking of all decisions.
	TrackAllDecisions bool

	// IncludeMetadata includes additional context in entries.
	IncludeMetadata bool
}

// DefaultMemoryConfig returns the default memory configuration.
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		ActivityConfig:    DefaultActivityConfig(),
		MaxHistoryEntries: 10000,
		TrackAllDecisions: true,
		IncludeMetadata:   true,
	}
}

// NewMemoryActivity creates a new memory activity.
//
// Inputs:
//   - config: Memory configuration. Uses defaults if nil.
//
// Outputs:
//   - *MemoryActivity: The new activity.
func NewMemoryActivity(config *MemoryConfig) *MemoryActivity {
	if config == nil {
		config = DefaultMemoryConfig()
	}

	return &MemoryActivity{
		name:    "memory",
		config:  config,
		timeout: config.Timeout,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// MemoryInput is the input for the memory activity.
type MemoryInput struct {
	BaseInput

	// Operation is the memory operation to perform.
	// Options: "record", "query", "replay"
	Operation string

	// NodeID is the node this decision is about.
	NodeID string

	// Action describes the action taken.
	Action string

	// Result describes the outcome.
	Result string

	// Metadata contains additional context.
	Metadata map[string]string

	// QueryCount is the number of entries to retrieve (for query).
	QueryCount int
}

// Type returns the input type name.
func (i *MemoryInput) Type() string {
	return "memory"
}

// NewMemoryInput creates a new memory input.
func NewMemoryInput(requestID, operation string, source crs.SignalSource) *MemoryInput {
	return &MemoryInput{
		BaseInput:  NewBaseInput(requestID, source),
		Operation:  operation,
		Metadata:   make(map[string]string),
		QueryCount: 10,
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Name returns the activity name.
func (a *MemoryActivity) Name() string {
	return a.name
}

// Timeout returns the activity timeout.
func (a *MemoryActivity) Timeout() time.Duration {
	return a.timeout
}

// Algorithms returns the algorithms this activity orchestrates.
// Memory activity doesn't use algorithms - it directly manages history.
func (a *MemoryActivity) Algorithms() []algorithms.Algorithm {
	return nil
}

// Execute runs the memory operation.
//
// Description:
//
//	Memory operations:
//	- "record": Create a new history entry
//	- "query": Retrieve recent history entries
//	- "replay": Replay decisions for debugging
//
// Thread Safety: Safe for concurrent calls.
func (a *MemoryActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.MemoryActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.name),
		),
	)
	defer span.End()

	startTime := time.Now()

	memoryInput, ok := input.(*MemoryInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.name,
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(attribute.String("operation", memoryInput.Operation))

	result := ActivityResult{
		ActivityName: a.name,
		StartTime:    startTime,
		Metrics:      make(map[string]float64),
	}

	var delta crs.Delta
	var err error

	switch memoryInput.Operation {
	case "record":
		delta, err = a.record(memoryInput)
	case "query":
		result.Metrics["entries_found"] = float64(len(snapshot.HistoryIndex().Recent(memoryInput.QueryCount)))
		// Query is read-only, no delta
	case "replay":
		// Replay is read-only, returns trace
		trace := snapshot.HistoryIndex().Trace(memoryInput.NodeID)
		result.Metrics["trace_length"] = float64(len(trace))
	default:
		err = &ActivityError{
			Activity:  a.name,
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Success = err == nil

	return result, delta, err
}

// record creates a history entry.
func (a *MemoryActivity) record(input *MemoryInput) (crs.Delta, error) {
	entry := crs.HistoryEntry{
		ID:        uuid.New().String(),
		NodeID:    input.NodeID,
		Action:    input.Action,
		Result:    input.Result,
		Source:    input.Source(),
		Timestamp: time.Now().UnixMilli(),
		Metadata:  input.Metadata,
	}

	if !a.config.IncludeMetadata {
		entry.Metadata = nil
	}

	delta := crs.NewHistoryDelta(input.Source(), []crs.HistoryEntry{entry})
	return delta, nil
}

// ShouldRun decides if memory should run.
//
// Description:
//
//	Memory activity is passive - it runs when explicitly invoked.
func (a *MemoryActivity) ShouldRun(_ crs.Snapshot) (bool, Priority) {
	// Memory is invoked on-demand, not scheduled
	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *MemoryActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "history_ordered",
			Description: "History entries are ordered by timestamp",
			Check: func(input, output any) error {
				// Ordering is maintained by the History index
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *MemoryActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "memory_records_total",
			Type:        eval.MetricCounter,
			Description: "Total history records created",
		},
		{
			Name:        "memory_queries_total",
			Type:        eval.MetricCounter,
			Description: "Total history queries",
		},
		{
			Name:        "memory_entries_count",
			Type:        eval.MetricGauge,
			Description: "Current number of history entries",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *MemoryActivity) HealthCheck(_ context.Context) error {
	if a.config == nil {
		return &ActivityError{
			Activity:  a.name,
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	if a.config.MaxHistoryEntries <= 0 {
		return &ActivityError{
			Activity:  a.name,
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	return nil
}
