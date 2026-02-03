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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms/streaming"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Similarity Activity
// -----------------------------------------------------------------------------

// SimilarityActivity orchestrates similarity algorithms: MinHash, LSH, WL, L0.
//
// Description:
//
//	SimilarityActivity coordinates pattern finding using:
//	- MinHash: Set similarity via Jaccard estimation
//	- LSH: Locality-sensitive hashing for near-neighbor search
//	- WeisfeilerLeman: Graph isomorphism detection
//	- L0Sampling: Sparse recovery for change detection
//
//	The activity finds similar code patterns, detects isomorphic
//	structures, and tracks sparse changes.
//
// Thread Safety: Safe for concurrent use.
type SimilarityActivity struct {
	*BaseActivity
	config *SimilarityConfig

	// Algorithms
	minhash         *streaming.MinHash
	lsh             *streaming.LSH
	weisfeilerLeman *streaming.WeisfeilerLeman
	l0Sampling      *streaming.L0Sampling
}

// SimilarityConfig configures the similarity activity.
type SimilarityConfig struct {
	*ActivityConfig

	// MinHashConfig configures the MinHash algorithm.
	MinHashConfig *streaming.MinHashConfig

	// LSHConfig configures the LSH algorithm.
	LSHConfig *streaming.LSHConfig

	// WeisfeilerLemanConfig configures the WL algorithm.
	WeisfeilerLemanConfig *streaming.WLConfig

	// L0SamplingConfig configures the L0 sampling algorithm.
	L0SamplingConfig *streaming.L0Config
}

// DefaultSimilarityConfig returns the default similarity configuration.
func DefaultSimilarityConfig() *SimilarityConfig {
	return &SimilarityConfig{
		ActivityConfig:        DefaultActivityConfig(),
		MinHashConfig:         streaming.DefaultMinHashConfig(),
		LSHConfig:             streaming.DefaultLSHConfig(),
		WeisfeilerLemanConfig: streaming.DefaultWLConfig(),
		L0SamplingConfig:      streaming.DefaultL0Config(),
	}
}

// NewSimilarityActivity creates a new similarity activity.
//
// Inputs:
//   - config: Similarity configuration. Uses defaults if nil.
//
// Outputs:
//   - *SimilarityActivity: The new activity.
func NewSimilarityActivity(config *SimilarityConfig) *SimilarityActivity {
	if config == nil {
		config = DefaultSimilarityConfig()
	}

	minhash := streaming.NewMinHash(config.MinHashConfig)
	lsh := streaming.NewLSH(config.LSHConfig)
	weisfeilerLeman := streaming.NewWeisfeilerLeman(config.WeisfeilerLemanConfig)
	l0Sampling := streaming.NewL0Sampling(config.L0SamplingConfig)

	return &SimilarityActivity{
		BaseActivity: NewBaseActivity(
			"similarity",
			config.Timeout,
			minhash,
			lsh,
			weisfeilerLeman,
			l0Sampling,
		),
		config:          config,
		minhash:         minhash,
		lsh:             lsh,
		weisfeilerLeman: weisfeilerLeman,
		l0Sampling:      l0Sampling,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// SimilarityInput is the input for the similarity activity.
type SimilarityInput struct {
	BaseInput

	// Sets are the sets to compute similarity for (MinHash/LSH).
	Sets [][]string

	// SetIDs are the identifiers for each set.
	SetIDs []string

	// Graph is the graph to analyze (WL).
	Graph *streaming.WLGraph

	// SparseUpdates are the updates for L0 sampling.
	SparseUpdates []streaming.L0Update
}

// Type returns the input type name.
func (i *SimilarityInput) Type() string {
	return "similarity"
}

// NewSimilarityInput creates a new similarity input.
func NewSimilarityInput(requestID string, source crs.SignalSource) *SimilarityInput {
	return &SimilarityInput{
		BaseInput: NewBaseInput(requestID, source),
		Sets:      make([][]string, 0),
		SetIDs:    make([]string, 0),
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the similarity algorithms.
//
// Description:
//
//	Runs MinHash, LSH, WL, and L0 based on available input data.
//	Each algorithm processes its relevant portion of the input.
//
// Thread Safety: Safe for concurrent calls.
func (a *SimilarityActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.SimilarityActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.Name()),
		),
	)
	defer span.End()

	similarityInput, ok := input.(*SimilarityInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(attribute.Int("sets_count", len(similarityInput.Sets)))

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "minhash":
			if len(similarityInput.Sets) == 0 {
				return nil
			}
			return &streaming.MinHashInput{
				Operation: "signature",
				Set:       similarityInput.Sets[0],
				Source:    similarityInput.Source(),
			}
		case "lsh":
			if len(similarityInput.Sets) == 0 || len(similarityInput.SetIDs) == 0 {
				return nil
			}
			// First create signature, then index
			return &streaming.LSHInput{
				Operation: "index",
				ID:        similarityInput.SetIDs[0],
				Source:    similarityInput.Source(),
			}
		case "weisfeiler_leman":
			if similarityInput.Graph == nil {
				return nil
			}
			return &streaming.WLInput{
				Operation: "color",
				Graph:     similarityInput.Graph,
				Source:    similarityInput.Source(),
			}
		case "l0_sampling":
			if len(similarityInput.SparseUpdates) == 0 {
				return nil
			}
			return &streaming.L0Input{
				Operation: "update",
				Updates:   similarityInput.SparseUpdates,
				Source:    similarityInput.Source(),
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if similarity should run.
//
// Description:
//
//	Similarity should run when there are items in the similarity index
//	that may benefit from pattern detection.
func (a *SimilarityActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	similarityIndex := snapshot.SimilarityIndex()

	// Check if there are items to compare
	if similarityIndex.Size() > 0 {
		return true, PriorityLow
	}

	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *SimilarityActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "similarity_bounded",
			Description: "Similarity scores are in [0, 1]",
			Check: func(input, output any) error {
				// Similarity bounds are checked by individual algorithms
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *SimilarityActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "similarity_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total similarity activity executions",
		},
		{
			Name:        "similarity_pairs_computed_total",
			Type:        eval.MetricCounter,
			Description: "Total similarity pairs computed",
		},
		{
			Name:        "similarity_patterns_found_total",
			Type:        eval.MetricCounter,
			Description: "Total similar patterns found",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *SimilarityActivity) HealthCheck(ctx context.Context) error {
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
