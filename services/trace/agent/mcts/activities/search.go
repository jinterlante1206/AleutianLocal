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
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms/search"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Search Activity
// -----------------------------------------------------------------------------

// SearchActivity orchestrates search algorithms: PN-MCTS, Transposition, UnitProp.
//
// Description:
//
//	SearchActivity coordinates the exploration of the solution space using:
//	- PN-MCTS: Proof-number guided MCTS for finding solutions
//	- Transposition: Zobrist hashing for state deduplication
//	- UnitProp: Forced move detection for pruning
//
//	The activity runs algorithms in parallel and merges their deltas
//	to update proof numbers and discover forced moves.
//
// Thread Safety: Safe for concurrent use.
type SearchActivity struct {
	*BaseActivity
	config *SearchConfig

	// Algorithms
	pnmcts        *search.PNMCTS
	transposition *search.Transposition
	unitProp      *search.UnitPropagation
}

// SearchConfig configures the search activity.
type SearchConfig struct {
	*ActivityConfig

	// PNMCTSConfig configures the PN-MCTS algorithm.
	PNMCTSConfig *search.PNMCTSConfig

	// TranspositionConfig configures the transposition algorithm.
	TranspositionConfig *search.TranspositionConfig

	// UnitPropConfig configures the unit propagation algorithm.
	UnitPropConfig *search.UnitPropConfig
}

// DefaultSearchConfig returns the default search configuration.
func DefaultSearchConfig() *SearchConfig {
	return &SearchConfig{
		ActivityConfig:      DefaultActivityConfig(),
		PNMCTSConfig:        search.DefaultPNMCTSConfig(),
		TranspositionConfig: search.DefaultTranspositionConfig(),
		UnitPropConfig:      search.DefaultUnitPropConfig(),
	}
}

// NewSearchActivity creates a new search activity.
//
// Inputs:
//   - config: Search configuration. Uses defaults if nil.
//
// Outputs:
//   - *SearchActivity: The new activity.
func NewSearchActivity(config *SearchConfig) *SearchActivity {
	if config == nil {
		config = DefaultSearchConfig()
	}

	pnmcts := search.NewPNMCTS(config.PNMCTSConfig)
	transposition := search.NewTransposition(config.TranspositionConfig)
	unitProp := search.NewUnitPropagation(config.UnitPropConfig)

	return &SearchActivity{
		BaseActivity: NewBaseActivity(
			"search",
			config.Timeout,
			pnmcts,
			transposition,
			unitProp,
		),
		config:        config,
		pnmcts:        pnmcts,
		transposition: transposition,
		unitProp:      unitProp,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// SearchInput is the input for the search activity.
type SearchInput struct {
	BaseInput

	// RootNodeID is the node to start searching from.
	RootNodeID string

	// MaxExpansions limits the number of nodes to expand.
	MaxExpansions int

	// TargetNodeIDs are specific nodes to try to prove.
	TargetNodeIDs []string

	// ExcludedNodeIDs are nodes to skip during search.
	ExcludedNodeIDs []string
}

// Type returns the input type name.
func (i *SearchInput) Type() string {
	return "search"
}

// NewSearchInput creates a new search input.
func NewSearchInput(requestID, rootNodeID string, source crs.SignalSource) *SearchInput {
	return &SearchInput{
		BaseInput:     NewBaseInput(requestID, source),
		RootNodeID:    rootNodeID,
		MaxExpansions: 100,
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the search algorithms.
//
// Description:
//
//	Runs PN-MCTS, Transposition, and UnitProp in parallel. Each algorithm
//	receives the snapshot and search input, and returns deltas that update
//	proof numbers and track state.
//
// Thread Safety: Safe for concurrent calls.
func (a *SearchActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.SearchActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.Name()),
		),
	)
	defer span.End()

	searchInput, ok := input.(*SearchInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(attribute.String("root_node", searchInput.RootNodeID))

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "pnmcts":
			return &search.PNMCTSInput{
				RootNodeID:  searchInput.RootNodeID,
				TargetNodes: searchInput.TargetNodeIDs,
				MaxDepth:    searchInput.MaxExpansions, // Use MaxExpansions as depth limit
			}
		case "transposition":
			return &search.TranspositionInput{
				Nodes:             []string{searchInput.RootNodeID},
				CurrentGeneration: snapshot.Generation(),
			}
		case "unit_propagation":
			return &search.UnitPropInput{
				FocusNodes: []string{searchInput.RootNodeID},
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if search should run.
//
// Description:
//
//	Search should run when there are unexplored nodes with
//	unknown proof status.
func (a *SearchActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	proofIndex := snapshot.ProofIndex()

	// Check if there are nodes with unknown status
	for _, proof := range proofIndex.All() {
		if proof.Status == crs.ProofStatusUnknown || proof.Status == crs.ProofStatusExpanded {
			return true, PriorityNormal
		}
	}

	// No unexplored nodes
	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *SearchActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "algorithms_executed",
			Description: "All algorithms were executed",
			Check: func(input, output any) error {
				result, ok := output.(ActivityResult)
				if !ok {
					return nil
				}
				if len(result.AlgorithmResults) != 3 {
					return &ActivityError{
						Activity:  a.Name(),
						Operation: "Property.algorithms_executed",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "no_critical_failures",
			Description: "No critical algorithm failures occurred",
			Check: func(input, output any) error {
				result, ok := output.(ActivityResult)
				if !ok {
					return nil
				}
				// At least one algorithm should succeed
				if result.SuccessCount() == 0 {
					return &ActivityError{
						Activity:  a.Name(),
						Operation: "Property.no_critical_failures",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *SearchActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "search_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total search activity executions",
		},
		{
			Name:        "search_activity_duration_seconds",
			Type:        eval.MetricHistogram,
			Description: "Search activity duration",
		},
		{
			Name:        "search_algorithms_success_total",
			Type:        eval.MetricCounter,
			Description: "Successful algorithm executions",
		},
		{
			Name:        "search_algorithms_failed_total",
			Type:        eval.MetricCounter,
			Description: "Failed algorithm executions",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *SearchActivity) HealthCheck(ctx context.Context) error {
	if a.config == nil {
		return &ActivityError{
			Activity:  a.Name(),
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	// Check each algorithm's health
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
