// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// Package-level error definitions.
var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInvalidConfig = errors.New("invalid config")
)

// AlgorithmError wraps algorithm-specific errors.
type AlgorithmError struct {
	Algorithm string
	Operation string
	Err       error
}

func (e *AlgorithmError) Error() string {
	return e.Algorithm + "." + e.Operation + ": " + e.Err.Error()
}

// -----------------------------------------------------------------------------
// Tarjan's Strongly Connected Components Algorithm
// -----------------------------------------------------------------------------

// TarjanSCC implements Tarjan's algorithm for finding strongly connected components.
//
// Description:
//
//	Tarjan's algorithm finds all strongly connected components (SCCs) in a
//	directed graph. An SCC is a maximal set of vertices such that there is
//	a path from each vertex to every other vertex.
//
//	Key Concepts:
//	- DFS-based single pass algorithm
//	- Uses lowlink values to identify component roots
//	- Linear time complexity O(V + E)
//
//	Use Cases:
//	- Detect cycles in code dependencies
//	- Identify mutually recursive functions
//	- Analyze call graph structure
//	- Find circular imports
//
// Thread Safety: Safe for concurrent use.
type TarjanSCC struct {
	config *TarjanConfig
}

// TarjanConfig configures the Tarjan algorithm.
type TarjanConfig struct {
	// MaxNodes limits the number of nodes to process.
	MaxNodes int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultTarjanConfig returns the default configuration.
func DefaultTarjanConfig() *TarjanConfig {
	return &TarjanConfig{
		MaxNodes:         10000,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewTarjanSCC creates a new Tarjan SCC algorithm.
func NewTarjanSCC(config *TarjanConfig) *TarjanSCC {
	if config == nil {
		config = DefaultTarjanConfig()
	}
	return &TarjanSCC{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// TarjanInput is the input for Tarjan SCC detection.
type TarjanInput struct {
	// Nodes is the list of node IDs in the graph.
	Nodes []string

	// Edges maps each node to its outgoing edges (adjacency list).
	Edges map[string][]string

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// TarjanOutput is the output from Tarjan SCC detection.
type TarjanOutput struct {
	// SCCs is the list of strongly connected components.
	// Each SCC is a list of node IDs.
	SCCs [][]string

	// NodeToSCC maps each node to its SCC index.
	NodeToSCC map[string]int

	// Cyclic is true if any SCC has more than one node.
	Cyclic bool

	// LargestSCCSize is the size of the largest SCC.
	LargestSCCSize int

	// NodesProcessed is the number of nodes visited.
	NodesProcessed int

	// TopologicalOrder is the SCCs in reverse topological order.
	// (each SCC can be treated as a single node in the DAG)
	TopologicalOrder []int
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (t *TarjanSCC) Name() string {
	return "tarjan_scc"
}

// Process finds all strongly connected components.
//
// Description:
//
//	Executes Tarjan's algorithm to find all SCCs in the input graph.
//	Returns SCCs in reverse topological order.
//
// Thread Safety: Safe for concurrent use.
func (t *TarjanSCC) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*TarjanInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "tarjan_scc",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &TarjanOutput{}, nil, ctx.Err()
	default:
	}

	// Check node limit
	if len(in.Nodes) > t.config.MaxNodes {
		return nil, nil, &AlgorithmError{
			Algorithm: "tarjan_scc",
			Operation: "Process",
			Err:       errors.New("too many nodes"),
		}
	}

	// Initialize state
	state := &tarjanState{
		index:     0,
		nodeIndex: make(map[string]int),
		lowlink:   make(map[string]int),
		onStack:   make(map[string]bool),
		stack:     make([]string, 0),
		sccs:      make([][]string, 0),
		edges:     in.Edges,
		ctx:       ctx,
	}

	// Visit all nodes
	for _, node := range in.Nodes {
		select {
		case <-ctx.Done():
			return t.buildPartialOutput(state), nil, ctx.Err()
		default:
		}

		if _, visited := state.nodeIndex[node]; !visited {
			t.strongConnect(state, node)
			if state.cancelled {
				return t.buildPartialOutput(state), nil, ctx.Err()
			}
		}
	}

	return t.buildOutput(state, in.Nodes), nil, nil
}

// tarjanState holds the algorithm state during execution.
type tarjanState struct {
	index     int
	nodeIndex map[string]int
	lowlink   map[string]int
	onStack   map[string]bool
	stack     []string
	sccs      [][]string
	edges     map[string][]string
	ctx       context.Context
	cancelled bool
}

// strongConnect is the recursive DFS function.
func (t *TarjanSCC) strongConnect(state *tarjanState, v string) {
	// Check cancellation
	select {
	case <-state.ctx.Done():
		state.cancelled = true
		return
	default:
	}

	// Set the depth index for v
	state.nodeIndex[v] = state.index
	state.lowlink[v] = state.index
	state.index++
	state.stack = append(state.stack, v)
	state.onStack[v] = true

	// Consider successors of v
	for _, w := range state.edges[v] {
		if state.cancelled {
			return
		}

		if _, visited := state.nodeIndex[w]; !visited {
			// Successor w has not yet been visited; recurse on it
			t.strongConnect(state, w)
			if state.cancelled {
				return
			}
			if state.lowlink[w] < state.lowlink[v] {
				state.lowlink[v] = state.lowlink[w]
			}
		} else if state.onStack[w] {
			// Successor w is in stack and hence in current SCC
			if state.nodeIndex[w] < state.lowlink[v] {
				state.lowlink[v] = state.nodeIndex[w]
			}
		}
	}

	// If v is a root node, pop the stack and generate an SCC
	if state.lowlink[v] == state.nodeIndex[v] {
		scc := make([]string, 0)
		for {
			w := state.stack[len(state.stack)-1]
			state.stack = state.stack[:len(state.stack)-1]
			state.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		state.sccs = append(state.sccs, scc)
	}
}

// buildOutput constructs the final output.
func (t *TarjanSCC) buildOutput(state *tarjanState, nodes []string) *TarjanOutput {
	output := &TarjanOutput{
		SCCs:             state.sccs,
		NodeToSCC:        make(map[string]int),
		Cyclic:           false,
		LargestSCCSize:   0,
		NodesProcessed:   len(state.nodeIndex),
		TopologicalOrder: make([]int, len(state.sccs)),
	}

	// Build node to SCC mapping and find largest
	for i, scc := range state.sccs {
		for _, node := range scc {
			output.NodeToSCC[node] = i
		}
		if len(scc) > output.LargestSCCSize {
			output.LargestSCCSize = len(scc)
		}
		if len(scc) > 1 {
			output.Cyclic = true
		}
		// SCCs are already in reverse topological order
		output.TopologicalOrder[i] = i
	}

	return output
}

// buildPartialOutput constructs a partial output on cancellation.
func (t *TarjanSCC) buildPartialOutput(state *tarjanState) *TarjanOutput {
	output := &TarjanOutput{
		SCCs:             state.sccs,
		NodeToSCC:        make(map[string]int),
		Cyclic:           false,
		LargestSCCSize:   0,
		NodesProcessed:   len(state.nodeIndex),
		TopologicalOrder: make([]int, 0),
	}

	for i, scc := range state.sccs {
		for _, node := range scc {
			output.NodeToSCC[node] = i
		}
		if len(scc) > output.LargestSCCSize {
			output.LargestSCCSize = len(scc)
		}
		if len(scc) > 1 {
			output.Cyclic = true
		}
	}

	return output
}

// Timeout returns the maximum execution time.
func (t *TarjanSCC) Timeout() time.Duration {
	return t.config.Timeout
}

// InputType returns the expected input type.
func (t *TarjanSCC) InputType() reflect.Type {
	return reflect.TypeOf(&TarjanInput{})
}

// OutputType returns the output type.
func (t *TarjanSCC) OutputType() reflect.Type {
	return reflect.TypeOf(&TarjanOutput{})
}

// ProgressInterval returns how often to report progress.
func (t *TarjanSCC) ProgressInterval() time.Duration {
	return t.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (t *TarjanSCC) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (t *TarjanSCC) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "sccs_partition_nodes",
			Description: "SCCs partition all processed nodes",
			Check: func(input, output any) error {
				in, okIn := input.(*TarjanInput)
				out, okOut := output.(*TarjanOutput)
				if !okIn || !okOut {
					return nil
				}

				// Count nodes in all SCCs
				nodeCount := 0
				for _, scc := range out.SCCs {
					nodeCount += len(scc)
				}

				// Should equal processed nodes
				if nodeCount != out.NodesProcessed {
					return &AlgorithmError{
						Algorithm: "tarjan_scc",
						Operation: "Property.sccs_partition_nodes",
						Err:       eval.ErrPropertyFailed,
					}
				}

				_ = in // Input used for validation context
				return nil
			},
		},
		{
			Name:        "node_to_scc_consistent",
			Description: "NodeToSCC mapping matches SCC contents",
			Check: func(input, output any) error {
				out, ok := output.(*TarjanOutput)
				if !ok {
					return nil
				}

				for i, scc := range out.SCCs {
					for _, node := range scc {
						if out.NodeToSCC[node] != i {
							return &AlgorithmError{
								Algorithm: "tarjan_scc",
								Operation: "Property.node_to_scc_consistent",
								Err:       eval.ErrPropertyFailed,
							}
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "cyclic_implies_large_scc",
			Description: "Cyclic is true iff there exists an SCC with size > 1",
			Check: func(input, output any) error {
				out, ok := output.(*TarjanOutput)
				if !ok {
					return nil
				}

				hasLargeSCC := false
				for _, scc := range out.SCCs {
					if len(scc) > 1 {
						hasLargeSCC = true
						break
					}
				}

				if out.Cyclic != hasLargeSCC {
					return &AlgorithmError{
						Algorithm: "tarjan_scc",
						Operation: "Property.cyclic_implies_large_scc",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (t *TarjanSCC) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "tarjan_sccs_found_total",
			Type:        eval.MetricCounter,
			Description: "Total SCCs found",
		},
		{
			Name:        "tarjan_nodes_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total nodes processed",
		},
		{
			Name:        "tarjan_cyclic_graphs_total",
			Type:        eval.MetricCounter,
			Description: "Total graphs with cycles",
		},
		{
			Name:        "tarjan_largest_scc_size",
			Type:        eval.MetricHistogram,
			Description: "Distribution of largest SCC sizes",
			Buckets:     []float64{1, 2, 5, 10, 20, 50, 100},
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (t *TarjanSCC) HealthCheck(ctx context.Context) error {
	if t.config == nil {
		return &AlgorithmError{
			Algorithm: "tarjan_scc",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if t.config.MaxNodes <= 0 {
		return &AlgorithmError{
			Algorithm: "tarjan_scc",
			Operation: "HealthCheck",
			Err:       errors.New("max nodes must be positive"),
		}
	}
	return nil
}
