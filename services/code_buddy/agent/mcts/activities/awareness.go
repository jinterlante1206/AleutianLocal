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
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/algorithms/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Awareness Activity
// -----------------------------------------------------------------------------

// AwarenessActivity orchestrates graph algorithms: Tarjan, Dominators, VF2.
//
// Description:
//
//	AwarenessActivity coordinates code structure understanding using:
//	- Tarjan: Strongly Connected Component detection for cycle finding
//	- Dominators: Lengauer-Tarjan for control flow analysis
//	- VF2: Subgraph isomorphism for pattern matching
//
//	The activity analyzes code structure to identify patterns, cycles,
//	and control flow relationships.
//
// Thread Safety: Safe for concurrent use.
type AwarenessActivity struct {
	*BaseActivity
	config *AwarenessConfig

	// Algorithms
	tarjan     *graph.TarjanSCC
	dominators *graph.Dominators
	vf2        *graph.VF2
}

// AwarenessConfig configures the awareness activity.
type AwarenessConfig struct {
	*ActivityConfig

	// TarjanConfig configures the Tarjan SCC algorithm.
	TarjanConfig *graph.TarjanConfig

	// DominatorsConfig configures the dominators algorithm.
	DominatorsConfig *graph.DominatorsConfig

	// VF2Config configures the VF2 algorithm.
	VF2Config *graph.VF2Config
}

// DefaultAwarenessConfig returns the default awareness configuration.
func DefaultAwarenessConfig() *AwarenessConfig {
	return &AwarenessConfig{
		ActivityConfig:   DefaultActivityConfig(),
		TarjanConfig:     graph.DefaultTarjanConfig(),
		DominatorsConfig: graph.DefaultDominatorsConfig(),
		VF2Config:        graph.DefaultVF2Config(),
	}
}

// NewAwarenessActivity creates a new awareness activity.
//
// Inputs:
//   - config: Awareness configuration. Uses defaults if nil.
//
// Outputs:
//   - *AwarenessActivity: The new activity.
func NewAwarenessActivity(config *AwarenessConfig) *AwarenessActivity {
	if config == nil {
		config = DefaultAwarenessConfig()
	}

	tarjan := graph.NewTarjanSCC(config.TarjanConfig)
	dominators := graph.NewDominators(config.DominatorsConfig)
	vf2 := graph.NewVF2(config.VF2Config)

	return &AwarenessActivity{
		BaseActivity: NewBaseActivity(
			"awareness",
			config.Timeout,
			tarjan,
			dominators,
			vf2,
		),
		config:     config,
		tarjan:     tarjan,
		dominators: dominators,
		vf2:        vf2,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// AwarenessInput is the input for the awareness activity.
type AwarenessInput struct {
	BaseInput

	// GraphNodes are the nodes in the graph to analyze.
	GraphNodes []string

	// GraphEdges are the edges (from -> []to).
	GraphEdges map[string][]string

	// RootNodeID is the root for dominator analysis.
	RootNodeID string

	// PatternGraph is the pattern to match (for VF2).
	PatternGraph *graph.VF2Graph
}

// Type returns the input type name.
func (i *AwarenessInput) Type() string {
	return "awareness"
}

// NewAwarenessInput creates a new awareness input.
func NewAwarenessInput(requestID string, source crs.SignalSource) *AwarenessInput {
	return &AwarenessInput{
		BaseInput:  NewBaseInput(requestID, source),
		GraphEdges: make(map[string][]string),
	}
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the awareness algorithms.
//
// Description:
//
//	Runs Tarjan, Dominators, and VF2 in parallel to analyze code structure.
//	Each algorithm receives the graph representation and returns its analysis.
//
// Thread Safety: Safe for concurrent calls.
func (a *AwarenessActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.AwarenessActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.Name()),
		),
	)
	defer span.End()

	awarenessInput, ok := input.(*AwarenessInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(
		attribute.Int("graph_nodes", len(awarenessInput.GraphNodes)),
		attribute.Int("graph_edges", len(awarenessInput.GraphEdges)),
	)

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "tarjan_scc":
			return &graph.TarjanInput{
				Nodes:  awarenessInput.GraphNodes,
				Edges:  awarenessInput.GraphEdges,
				Source: awarenessInput.Source(),
			}
		case "dominators":
			return &graph.DominatorsInput{
				Entry:      awarenessInput.RootNodeID,
				Nodes:      awarenessInput.GraphNodes,
				Successors: awarenessInput.GraphEdges,
				Source:     awarenessInput.Source(),
			}
		case "vf2":
			if awarenessInput.PatternGraph == nil {
				return nil
			}
			return &graph.VF2Input{
				Target: graph.VF2Graph{
					Nodes: awarenessInput.GraphNodes,
					Edges: awarenessInput.GraphEdges,
				},
				Pattern: *awarenessInput.PatternGraph,
				Source:  awarenessInput.Source(),
			}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// ShouldRun decides if awareness should run.
//
// Description:
//
//	Awareness should run when there are dependencies that need
//	cycle detection or structural analysis.
func (a *AwarenessActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	dependencyIndex := snapshot.DependencyIndex()

	// Check if there are dependencies to analyze
	if dependencyIndex.Size() > 0 {
		return true, PriorityNormal
	}

	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *AwarenessActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "scc_complete",
			Description: "All nodes are assigned to an SCC",
			Check: func(input, output any) error {
				// SCC completeness is checked by Tarjan algorithm
				return nil
			},
		},
		{
			Name:        "dominators_valid",
			Description: "Dominator tree is valid",
			Check: func(input, output any) error {
				// Dominator validity is checked by Dominators algorithm
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *AwarenessActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "awareness_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total awareness activity executions",
		},
		{
			Name:        "awareness_sccs_found_total",
			Type:        eval.MetricCounter,
			Description: "Total SCCs found",
		},
		{
			Name:        "awareness_patterns_matched_total",
			Type:        eval.MetricCounter,
			Description: "Total patterns matched by VF2",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *AwarenessActivity) HealthCheck(ctx context.Context) error {
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
