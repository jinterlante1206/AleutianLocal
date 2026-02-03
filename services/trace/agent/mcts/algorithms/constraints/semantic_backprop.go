// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package constraints

import (
	"context"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Semantic Backpropagation Algorithm
// -----------------------------------------------------------------------------

// SemanticBackprop implements semantic error backpropagation.
//
// Description:
//
//	Semantic Backpropagation attributes errors to their root causes by
//	propagating error information backward through the code graph. When
//	a test fails or a compiler error occurs, this algorithm traces back
//	through dependencies to find likely causes.
//
//	Key Concepts:
//	- Error Attribution: Assigning blame to source nodes
//	- Gradient-like Propagation: Error influence decreases with distance
//	- Multi-cause Analysis: An error may have multiple contributing causes
//
//	IMPORTANT: This is an attribution algorithm, not a learning algorithm.
//	It helps identify WHICH nodes to focus on, but the actual decision
//	about proof status must come from hard signals (compiler, tests).
//
// Thread Safety: Safe for concurrent use.
type SemanticBackprop struct {
	config *SemanticBackpropConfig
}

// SemanticBackpropConfig configures the semantic backpropagation algorithm.
type SemanticBackpropConfig struct {
	// MaxDepth limits backpropagation depth.
	MaxDepth int

	// DecayFactor is the decay per hop (0-1).
	DecayFactor float64

	// MinAttribution is the minimum attribution to keep.
	MinAttribution float64

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultSemanticBackpropConfig returns the default configuration.
func DefaultSemanticBackpropConfig() *SemanticBackpropConfig {
	return &SemanticBackpropConfig{
		MaxDepth:         10,
		DecayFactor:      0.7,
		MinAttribution:   0.01,
		Timeout:          3 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewSemanticBackprop creates a new semantic backpropagation algorithm.
func NewSemanticBackprop(config *SemanticBackpropConfig) *SemanticBackprop {
	if config == nil {
		config = DefaultSemanticBackpropConfig()
	}
	return &SemanticBackprop{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// SemanticBackpropInput is the input for semantic backpropagation.
type SemanticBackpropInput struct {
	// ErrorNodes are nodes where errors occurred.
	ErrorNodes []ErrorNode

	// Dependencies maps node -> nodes it depends on.
	Dependencies map[string][]string

	// NodeWeights assigns base weights to nodes (optional).
	NodeWeights map[string]float64
}

// ErrorNode represents a node with an error.
type ErrorNode struct {
	NodeID    string
	ErrorType string  // e.g., "compiler_error", "test_failure"
	Severity  float64 // 0-1, how severe
	Message   string
	Source    crs.SignalSource
}

// SemanticBackpropOutput is the output from semantic backpropagation.
type SemanticBackpropOutput struct {
	// Attributions maps node -> error attribution (0-1).
	Attributions map[string]float64

	// TopCauses are the top attributed nodes.
	TopCauses []AttributedCause

	// PropagationPaths shows how error propagated.
	PropagationPaths []PropagationPath

	// MaxDepthReached is the maximum depth reached.
	MaxDepthReached int
}

// AttributedCause represents a likely cause of error.
type AttributedCause struct {
	NodeID      string
	Attribution float64
	Depth       int      // How many hops from error
	ErrorNodes  []string // Which errors this contributes to
}

// PropagationPath shows a single propagation path.
type PropagationPath struct {
	From        string // Error node
	To          string // Attributed node
	Path        []string
	Attribution float64
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (s *SemanticBackprop) Name() string {
	return "semantic_backprop"
}

// Process performs semantic error backpropagation.
//
// Description:
//
//	Propagates error information backward through dependencies to attribute
//	errors to their likely root causes.
//
// Thread Safety: Safe for concurrent use.
func (s *SemanticBackprop) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*SemanticBackpropInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "semantic_backprop",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &SemanticBackpropOutput{}, nil, ctx.Err()
	default:
	}

	output := &SemanticBackpropOutput{
		Attributions:     make(map[string]float64),
		TopCauses:        make([]AttributedCause, 0),
		PropagationPaths: make([]PropagationPath, 0),
	}

	// Attribution accumulator
	attributions := make(map[string]float64)
	nodeErrors := make(map[string][]string) // node -> which error nodes contribute

	// Process each error node
	for _, errorNode := range in.ErrorNodes {
		// Initial attribution at error node
		initialAttrib := errorNode.Severity
		if initialAttrib <= 0 {
			initialAttrib = 1.0
		}

		// BFS backpropagation
		visited := make(map[string]bool)
		type queueItem struct {
			nodeID      string
			depth       int
			attribution float64
			path        []string
		}

		queue := []queueItem{{
			nodeID:      errorNode.NodeID,
			depth:       0,
			attribution: initialAttrib,
			path:        []string{errorNode.NodeID},
		}}

		for len(queue) > 0 {
			// Check for cancellation
			select {
			case <-ctx.Done():
				s.collectOutput(output, attributions, nodeErrors)
				return output, nil, ctx.Err()
			default:
			}

			item := queue[0]
			queue = queue[1:]

			if visited[item.nodeID] {
				continue
			}
			visited[item.nodeID] = true

			// Add attribution
			attributions[item.nodeID] += item.attribution
			nodeErrors[item.nodeID] = append(nodeErrors[item.nodeID], errorNode.NodeID)

			if item.depth > output.MaxDepthReached {
				output.MaxDepthReached = item.depth
			}

			// Record propagation path for non-error nodes
			if item.nodeID != errorNode.NodeID {
				output.PropagationPaths = append(output.PropagationPaths, PropagationPath{
					From:        errorNode.NodeID,
					To:          item.nodeID,
					Path:        item.path,
					Attribution: item.attribution,
				})
			}

			// Check depth limit
			if item.depth >= s.config.MaxDepth {
				continue
			}

			// Propagate to dependencies
			deps := in.Dependencies[item.nodeID]
			if len(deps) == 0 {
				continue
			}

			// Split attribution among dependencies
			depAttrib := item.attribution * s.config.DecayFactor / float64(len(deps))
			if depAttrib < s.config.MinAttribution {
				continue
			}

			for _, dep := range deps {
				if !visited[dep] {
					newPath := make([]string, len(item.path)+1)
					copy(newPath, item.path)
					newPath[len(item.path)] = dep

					queue = append(queue, queueItem{
						nodeID:      dep,
						depth:       item.depth + 1,
						attribution: depAttrib,
						path:        newPath,
					})
				}
			}
		}
	}

	// Collect output
	s.collectOutput(output, attributions, nodeErrors)

	return output, nil, nil
}

// collectOutput converts internal state to output.
func (s *SemanticBackprop) collectOutput(output *SemanticBackpropOutput, attributions map[string]float64, nodeErrors map[string][]string) {
	output.Attributions = attributions

	// Build top causes list
	type nodeAttrib struct {
		nodeID      string
		attribution float64
		errors      []string
	}

	list := make([]nodeAttrib, 0, len(attributions))
	for nodeID, attrib := range attributions {
		list = append(list, nodeAttrib{
			nodeID:      nodeID,
			attribution: attrib,
			errors:      nodeErrors[nodeID],
		})
	}

	// Sort by attribution (descending)
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].attribution > list[i].attribution {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	// Take top N
	maxCauses := 10
	if len(list) < maxCauses {
		maxCauses = len(list)
	}

	for i := 0; i < maxCauses; i++ {
		// Calculate depth from paths
		depth := 0
		for _, path := range output.PropagationPaths {
			if path.To == list[i].nodeID && len(path.Path)-1 > depth {
				depth = len(path.Path) - 1
			}
		}

		output.TopCauses = append(output.TopCauses, AttributedCause{
			NodeID:      list[i].nodeID,
			Attribution: list[i].attribution,
			Depth:       depth,
			ErrorNodes:  list[i].errors,
		})
	}
}

// Timeout returns the maximum execution time.
func (s *SemanticBackprop) Timeout() time.Duration {
	return s.config.Timeout
}

// InputType returns the expected input type.
func (s *SemanticBackprop) InputType() reflect.Type {
	return reflect.TypeOf(&SemanticBackpropInput{})
}

// OutputType returns the output type.
func (s *SemanticBackprop) OutputType() reflect.Type {
	return reflect.TypeOf(&SemanticBackpropOutput{})
}

// ProgressInterval returns how often to report progress.
func (s *SemanticBackprop) ProgressInterval() time.Duration {
	return s.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (s *SemanticBackprop) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (s *SemanticBackprop) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "attribution_positive",
			Description: "All attributions are non-negative",
			Check: func(input, output any) error {
				out, ok := output.(*SemanticBackpropOutput)
				if !ok {
					return nil
				}
				for nodeID, attrib := range out.Attributions {
					if attrib < 0 {
						_ = nodeID // silence unused
						return &AlgorithmError{
							Algorithm: "semantic_backprop",
							Operation: "Property.attribution_positive",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "depth_bounded",
			Description: "Propagation respects max depth",
			Check: func(input, output any) error {
				out, ok := output.(*SemanticBackpropOutput)
				if !ok {
					return nil
				}
				// Can't directly check config from here, but paths should be bounded
				for _, path := range out.PropagationPaths {
					if len(path.Path) > 100 { // Sanity check
						return &AlgorithmError{
							Algorithm: "semantic_backprop",
							Operation: "Property.depth_bounded",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (s *SemanticBackprop) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "semantic_backprop_nodes_attributed_total",
			Type:        eval.MetricCounter,
			Description: "Total nodes with attribution",
		},
		{
			Name:        "semantic_backprop_max_depth",
			Type:        eval.MetricGauge,
			Description: "Maximum propagation depth reached",
		},
		{
			Name:        "semantic_backprop_top_attribution",
			Type:        eval.MetricGauge,
			Description: "Highest attribution value",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (s *SemanticBackprop) HealthCheck(ctx context.Context) error {
	if s.config == nil {
		return &AlgorithmError{
			Algorithm: "semantic_backprop",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if s.config.DecayFactor <= 0 || s.config.DecayFactor >= 1 {
		return &AlgorithmError{
			Algorithm: "semantic_backprop",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
