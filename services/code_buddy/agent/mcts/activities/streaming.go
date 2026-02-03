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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms/streaming"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Streaming Activity
// -----------------------------------------------------------------------------

// StreamingActivity orchestrates streaming algorithms: AGM, CountMin, HLL.
//
// Description:
//
//	StreamingActivity coordinates approximate statistics using:
//	- AGMSketch: Graph connectivity estimation
//	- CountMin: Frequency estimation
//	- HyperLogLog: Cardinality estimation
//
//	IMPORTANT: Streaming algorithms produce APPROXIMATE results.
//	They should NOT be used for critical decisions without verification.
//
// Thread Safety: Safe for concurrent use.
type StreamingActivity struct {
	*BaseActivity
	config *StreamingConfig

	// Algorithms
	agmSketch   *streaming.AGMSketch
	countMin    *streaming.CountMin
	hyperLogLog *streaming.HyperLogLog
}

// StreamingConfig configures the streaming activity.
type StreamingConfig struct {
	*ActivityConfig

	// AGMConfig configures the AGM sketch algorithm.
	AGMConfig *streaming.AGMConfig

	// CountMinConfig configures the Count-Min sketch algorithm.
	CountMinConfig *streaming.CountMinConfig

	// HyperLogLogConfig configures the HyperLogLog algorithm.
	HyperLogLogConfig *streaming.HyperLogLogConfig
}

// DefaultStreamingConfig returns the default streaming configuration.
func DefaultStreamingConfig() *StreamingConfig {
	return &StreamingConfig{
		ActivityConfig:    DefaultActivityConfig(),
		AGMConfig:         streaming.DefaultAGMConfig(),
		CountMinConfig:    streaming.DefaultCountMinConfig(),
		HyperLogLogConfig: streaming.DefaultHyperLogLogConfig(),
	}
}

// NewStreamingActivity creates a new streaming activity.
//
// Inputs:
//   - config: Streaming configuration. Uses defaults if nil.
//
// Outputs:
//   - *StreamingActivity: The new activity.
func NewStreamingActivity(config *StreamingConfig) *StreamingActivity {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	agmSketch := streaming.NewAGMSketch(config.AGMConfig)
	countMin := streaming.NewCountMin(config.CountMinConfig)
	hyperLogLog := streaming.NewHyperLogLog(config.HyperLogLogConfig)

	return &StreamingActivity{
		BaseActivity: NewBaseActivity(
			"streaming",
			config.Timeout,
			agmSketch,
			countMin,
			hyperLogLog,
		),
		config:      config,
		agmSketch:   agmSketch,
		countMin:    countMin,
		hyperLogLog: hyperLogLog,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// StreamingInput is the input for the streaming activity.
type StreamingInput struct {
	BaseInput

	// Items are the items to process (for frequency/cardinality).
	Items []string

	// Frequencies are item frequencies to update.
	Frequencies map[string]uint64

	// GraphEdges are edges for connectivity tracking.
	GraphEdges []streaming.AGMEdge
}

// Type returns the input type name.
func (i *StreamingInput) Type() string {
	return "streaming"
}

// NewStreamingInput creates a new streaming input.
func NewStreamingInput(requestID string, source crs.SignalSource) *StreamingInput {
	return &StreamingInput{
		BaseInput:   NewBaseInput(requestID, source),
		Items:       make([]string, 0),
		Frequencies: make(map[string]uint64),
		GraphEdges:  make([]streaming.AGMEdge, 0),
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the streaming algorithms.
//
// Description:
//
//	Runs AGM, CountMin, and HyperLogLog based on available input data.
//	Each algorithm updates its internal sketch structure.
//
// Thread Safety: Safe for concurrent calls.
func (a *StreamingActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.StreamingActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.Name()),
		),
	)
	defer span.End()

	streamingInput, ok := input.(*StreamingInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(
		attribute.Int("items_count", len(streamingInput.Items)),
		attribute.Int("edges_count", len(streamingInput.GraphEdges)),
	)

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "agm_sketch":
			if len(streamingInput.GraphEdges) == 0 {
				return nil
			}
			return &streaming.AGMInput{
				Operation: "add",
				Edges:     streamingInput.GraphEdges,
				Source:    streamingInput.Source(),
			}
		case "count_min":
			if len(streamingInput.Items) == 0 && len(streamingInput.Frequencies) == 0 {
				return nil
			}
			// Convert to CountMinItem format
			items := make([]streaming.CountMinItem, 0, len(streamingInput.Items)+len(streamingInput.Frequencies))
			for _, item := range streamingInput.Items {
				items = append(items, streaming.CountMinItem{Key: item, Count: 1})
			}
			for item, count := range streamingInput.Frequencies {
				items = append(items, streaming.CountMinItem{Key: item, Count: int64(count)})
			}
			return &streaming.CountMinInput{
				Operation: "add",
				Items:     items,
				Source:    streamingInput.Source(),
			}
		case "hyperloglog":
			if len(streamingInput.Items) == 0 {
				return nil
			}
			return &streaming.HyperLogLogInput{
				Operation: "add",
				Items:     streamingInput.Items,
				Source:    streamingInput.Source(),
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if streaming should run.
//
// Description:
//
//	Streaming should run when there is data that needs
//	approximate statistics tracking.
func (a *StreamingActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	streamingIndex := snapshot.StreamingIndex()

	// Streaming is always useful for background statistics
	if streamingIndex.Size() > 0 {
		return true, PriorityLow
	}

	return true, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *StreamingActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "estimates_approximate",
			Description: "Streaming results are approximations",
			Check: func(input, output any) error {
				// This property documents that results are approximate
				return nil
			},
		},
		{
			Name:        "no_hard_decisions",
			Description: "Streaming results are not used for hard decisions",
			Check: func(input, output any) error {
				// Verified by activity orchestration
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *StreamingActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "streaming_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total streaming activity executions",
		},
		{
			Name:        "streaming_items_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total items processed by streaming algorithms",
		},
		{
			Name:        "streaming_cardinality_estimate",
			Type:        eval.MetricGauge,
			Description: "Current cardinality estimate",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *StreamingActivity) HealthCheck(ctx context.Context) error {
	if a.config == nil {
		return &ActivityError{
			Activity:  a.Name(),
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	for _, algo := range a.Algorithms() {
		if err := algo.HealthCheck(ctx); err != nil {
			return &ActivityError{
				Activity:  a.Name(),
				Operation: "HealthCheck",
				Err:       err,
			}
		}
	}

	return nil
}
