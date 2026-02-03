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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// VF2 Subgraph Isomorphism Algorithm
// -----------------------------------------------------------------------------

// VF2 implements the VF2 algorithm for subgraph isomorphism.
//
// Description:
//
//	VF2 determines if a pattern graph is isomorphic to a subgraph of a
//	target graph. It uses a state-space representation with feasibility
//	rules to prune the search space.
//
//	Key Concepts:
//	- State: Partial mapping from pattern nodes to target nodes
//	- Candidate pairs: Nodes that can extend the current mapping
//	- Feasibility: Rules to check if a pair can be added
//	- Backtracking: Systematic search with pruning
//
//	Use Cases:
//	- Finding code patterns in AST
//	- Detecting design patterns
//	- Clone detection
//	- API usage pattern matching
//
// Thread Safety: Safe for concurrent use.
type VF2 struct {
	config *VF2Config
}

// VF2Config configures the VF2 algorithm.
type VF2Config struct {
	// MaxMatches limits the number of matches to find.
	// Set to 0 for unlimited (find all matches).
	MaxMatches int

	// MaxIterations limits the search iterations.
	MaxIterations int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultVF2Config returns the default configuration.
func DefaultVF2Config() *VF2Config {
	return &VF2Config{
		MaxMatches:       100,
		MaxIterations:    100000,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewVF2 creates a new VF2 algorithm.
func NewVF2(config *VF2Config) *VF2 {
	if config == nil {
		config = DefaultVF2Config()
	}
	return &VF2{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// VF2Input is the input for VF2 subgraph isomorphism.
type VF2Input struct {
	// Pattern is the smaller graph to find.
	Pattern VF2Graph

	// Target is the larger graph to search in.
	Target VF2Graph

	// NodeMatcher is an optional function to check node compatibility.
	// If nil, all nodes are considered compatible.
	NodeMatcher func(patternNode, targetNode string, patternLabels, targetLabels map[string]string) bool

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// VF2Graph represents a labeled directed graph.
type VF2Graph struct {
	// Nodes is the list of node IDs.
	Nodes []string

	// Edges maps each node to its outgoing edges.
	Edges map[string][]string

	// NodeLabels maps node IDs to their labels (for semantic matching).
	NodeLabels map[string]string
}

// VF2Output is the output from VF2.
type VF2Output struct {
	// IsIsomorphic is true if at least one match was found.
	IsIsomorphic bool

	// Matches is the list of found isomorphisms.
	// Each match maps pattern node IDs to target node IDs.
	Matches []map[string]string

	// MatchCount is the total number of matches found.
	MatchCount int

	// IterationsUsed is the number of search iterations.
	IterationsUsed int

	// PrunedStates is the number of states pruned by feasibility rules.
	PrunedStates int

	// SearchComplete is true if the entire search space was explored.
	SearchComplete bool
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (v *VF2) Name() string {
	return "vf2"
}

// Process performs subgraph isomorphism search.
//
// Description:
//
//	Uses the VF2 algorithm to find all subgraphs in target that are
//	isomorphic to the pattern graph.
//
// Thread Safety: Safe for concurrent use.
func (v *VF2) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*VF2Input)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "vf2",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &VF2Output{}, nil, ctx.Err()
	default:
	}

	// Validate input
	if len(in.Pattern.Nodes) == 0 {
		return &VF2Output{
			IsIsomorphic:   true, // Empty pattern matches everywhere
			Matches:        []map[string]string{{}},
			MatchCount:     1,
			SearchComplete: true,
		}, nil, nil
	}

	if len(in.Pattern.Nodes) > len(in.Target.Nodes) {
		// Pattern larger than target - no match possible
		return &VF2Output{
			IsIsomorphic:   false,
			Matches:        []map[string]string{},
			MatchCount:     0,
			SearchComplete: true,
		}, nil, nil
	}

	// Initialize state
	state := &vf2State{
		pattern:       &in.Pattern,
		target:        &in.Target,
		mapping:       make(map[string]string),
		reverseMap:    make(map[string]bool),
		matches:       make([]map[string]string, 0),
		iterations:    0,
		prunedStates:  0,
		maxMatches:    v.config.MaxMatches,
		maxIterations: v.config.MaxIterations,
		nodeMatcher:   in.NodeMatcher,
		ctx:           ctx,
		cancelled:     false,
	}

	// Build adjacency sets for faster lookups
	state.patternInEdges = buildInEdges(in.Pattern.Edges)
	state.targetInEdges = buildInEdges(in.Target.Edges)

	// Start recursive matching
	v.match(state)

	return &VF2Output{
		IsIsomorphic:   len(state.matches) > 0,
		Matches:        state.matches,
		MatchCount:     len(state.matches),
		IterationsUsed: state.iterations,
		PrunedStates:   state.prunedStates,
		SearchComplete: !state.cancelled && (state.maxMatches == 0 || len(state.matches) < state.maxMatches),
	}, nil, nil
}

// vf2State holds the algorithm state during execution.
type vf2State struct {
	pattern        *VF2Graph
	target         *VF2Graph
	mapping        map[string]string // pattern node -> target node
	reverseMap     map[string]bool   // target nodes already mapped
	patternInEdges map[string][]string
	targetInEdges  map[string][]string
	matches        []map[string]string
	iterations     int
	prunedStates   int
	maxMatches     int
	maxIterations  int
	nodeMatcher    func(string, string, map[string]string, map[string]string) bool
	ctx            context.Context
	cancelled      bool
}

// buildInEdges builds a map of incoming edges.
func buildInEdges(outEdges map[string][]string) map[string][]string {
	inEdges := make(map[string][]string)
	for node, successors := range outEdges {
		for _, succ := range successors {
			inEdges[succ] = append(inEdges[succ], node)
		}
	}
	return inEdges
}

// match performs the recursive VF2 matching.
func (v *VF2) match(state *vf2State) {
	// Check cancellation
	select {
	case <-state.ctx.Done():
		state.cancelled = true
		return
	default:
	}

	// Check iteration limit
	state.iterations++
	if state.iterations > state.maxIterations {
		state.cancelled = true
		return
	}

	// Check if match limit reached
	if state.maxMatches > 0 && len(state.matches) >= state.maxMatches {
		return
	}

	// Check if complete match
	if len(state.mapping) == len(state.pattern.Nodes) {
		// Found a complete match
		match := make(map[string]string, len(state.mapping))
		for k, val := range state.mapping {
			match[k] = val
		}
		state.matches = append(state.matches, match)
		return
	}

	// Get candidate pairs
	candidates := v.getCandidatePairs(state)

	// Try each candidate pair
	for _, pair := range candidates {
		patternNode := pair[0]
		targetNode := pair[1]

		if v.isFeasible(state, patternNode, targetNode) {
			// Add to mapping
			state.mapping[patternNode] = targetNode
			state.reverseMap[targetNode] = true

			// Recurse
			v.match(state)

			// Backtrack
			delete(state.mapping, patternNode)
			delete(state.reverseMap, targetNode)

			if state.cancelled {
				return
			}
		} else {
			state.prunedStates++
		}
	}
}

// getCandidatePairs returns pairs of (pattern node, target node) to try.
func (v *VF2) getCandidatePairs(state *vf2State) [][2]string {
	// Find first unmapped pattern node
	var patternNode string
	for _, node := range state.pattern.Nodes {
		if _, mapped := state.mapping[node]; !mapped {
			patternNode = node
			break
		}
	}

	if patternNode == "" {
		return nil
	}

	// Find candidate target nodes
	candidates := make([][2]string, 0)

	// Try nodes connected to already-mapped nodes first (optimization)
	connectedTargets := make(map[string]bool)

	// Check nodes connected via out-edges from mapped pattern nodes
	for pNode, tNode := range state.mapping {
		for _, pSucc := range state.pattern.Edges[pNode] {
			if pSucc == patternNode {
				// Pattern node is successor of a mapped node
				for _, tSucc := range state.target.Edges[tNode] {
					if !state.reverseMap[tSucc] {
						connectedTargets[tSucc] = true
					}
				}
			}
		}
		// Check in-edges
		for _, pPred := range state.patternInEdges[patternNode] {
			if pPred == pNode {
				for _, tPred := range state.targetInEdges[tNode] {
					if !state.reverseMap[tPred] {
						connectedTargets[tPred] = true
					}
				}
			}
		}
	}

	// If connected targets exist, only try those
	if len(connectedTargets) > 0 {
		for tNode := range connectedTargets {
			candidates = append(candidates, [2]string{patternNode, tNode})
		}
	} else {
		// No connections yet (first node), try all unmapped target nodes
		for _, tNode := range state.target.Nodes {
			if !state.reverseMap[tNode] {
				candidates = append(candidates, [2]string{patternNode, tNode})
			}
		}
	}

	return candidates
}

// isFeasible checks if adding (patternNode, targetNode) is feasible.
func (v *VF2) isFeasible(state *vf2State, patternNode, targetNode string) bool {
	// Check node compatibility via custom matcher
	if state.nodeMatcher != nil {
		if !state.nodeMatcher(patternNode, targetNode, state.pattern.NodeLabels, state.target.NodeLabels) {
			return false
		}
	} else {
		// Default: check label equality if labels exist
		if state.pattern.NodeLabels != nil && state.target.NodeLabels != nil {
			pLabel := state.pattern.NodeLabels[patternNode]
			tLabel := state.target.NodeLabels[targetNode]
			if pLabel != "" && tLabel != "" && pLabel != tLabel {
				return false
			}
		}
	}

	// Check structural consistency
	// For all mapped pattern nodes, verify edge consistency
	for pNode, tNode := range state.mapping {
		// Check out-edges: if pattern has edge, target must have edge
		pHasEdge := false
		for _, succ := range state.pattern.Edges[pNode] {
			if succ == patternNode {
				pHasEdge = true
				break
			}
		}

		if pHasEdge {
			tHasEdge := false
			for _, succ := range state.target.Edges[tNode] {
				if succ == targetNode {
					tHasEdge = true
					break
				}
			}
			if !tHasEdge {
				return false
			}
		}

		// Check reverse: if pattern has edge from new node to mapped node
		pHasReverseEdge := false
		for _, succ := range state.pattern.Edges[patternNode] {
			if succ == pNode {
				pHasReverseEdge = true
				break
			}
		}

		if pHasReverseEdge {
			tHasReverseEdge := false
			for _, succ := range state.target.Edges[targetNode] {
				if succ == tNode {
					tHasReverseEdge = true
					break
				}
			}
			if !tHasReverseEdge {
				return false
			}
		}
	}

	return true
}

// Timeout returns the maximum execution time.
func (v *VF2) Timeout() time.Duration {
	return v.config.Timeout
}

// InputType returns the expected input type.
func (v *VF2) InputType() reflect.Type {
	return reflect.TypeOf(&VF2Input{})
}

// OutputType returns the output type.
func (v *VF2) OutputType() reflect.Type {
	return reflect.TypeOf(&VF2Output{})
}

// ProgressInterval returns how often to report progress.
func (v *VF2) ProgressInterval() time.Duration {
	return v.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (v *VF2) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (v *VF2) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "mapping_is_bijection",
			Description: "Each match is a bijective mapping",
			Check: func(input, output any) error {
				out, ok := output.(*VF2Output)
				if !ok {
					return nil
				}

				for _, match := range out.Matches {
					// Check no duplicate target nodes
					targets := make(map[string]bool)
					for _, tNode := range match {
						if targets[tNode] {
							return &AlgorithmError{
								Algorithm: "vf2",
								Operation: "Property.mapping_is_bijection",
								Err:       eval.ErrPropertyFailed,
							}
						}
						targets[tNode] = true
					}
				}
				return nil
			},
		},
		{
			Name:        "mapping_preserves_edges",
			Description: "Mappings preserve edge structure",
			Check: func(input, output any) error {
				in, okIn := input.(*VF2Input)
				out, okOut := output.(*VF2Output)
				if !okIn || !okOut {
					return nil
				}

				for _, match := range out.Matches {
					// Check each pattern edge exists in target
					for pNode, tNode := range match {
						for _, pSucc := range in.Pattern.Edges[pNode] {
							if tSucc, ok := match[pSucc]; ok {
								// Check edge exists in target
								edgeExists := false
								for _, succ := range in.Target.Edges[tNode] {
									if succ == tSucc {
										edgeExists = true
										break
									}
								}
								if !edgeExists {
									return &AlgorithmError{
										Algorithm: "vf2",
										Operation: "Property.mapping_preserves_edges",
										Err:       eval.ErrPropertyFailed,
									}
								}
							}
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "complete_pattern_coverage",
			Description: "Each match covers all pattern nodes",
			Check: func(input, output any) error {
				in, okIn := input.(*VF2Input)
				out, okOut := output.(*VF2Output)
				if !okIn || !okOut {
					return nil
				}

				for _, match := range out.Matches {
					if len(match) != len(in.Pattern.Nodes) {
						return &AlgorithmError{
							Algorithm: "vf2",
							Operation: "Property.complete_pattern_coverage",
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
func (v *VF2) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "vf2_matches_found_total",
			Type:        eval.MetricCounter,
			Description: "Total matches found",
		},
		{
			Name:        "vf2_iterations_total",
			Type:        eval.MetricCounter,
			Description: "Total search iterations",
		},
		{
			Name:        "vf2_pruned_states_total",
			Type:        eval.MetricCounter,
			Description: "Total states pruned by feasibility",
		},
		{
			Name:        "vf2_search_complete_total",
			Type:        eval.MetricCounter,
			Description: "Total searches that completed",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (v *VF2) HealthCheck(ctx context.Context) error {
	if v.config == nil {
		return &AlgorithmError{
			Algorithm: "vf2",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if v.config.MaxIterations <= 0 {
		return &AlgorithmError{
			Algorithm: "vf2",
			Operation: "HealthCheck",
			Err:       errors.New("max iterations must be positive"),
		}
	}
	return nil
}
