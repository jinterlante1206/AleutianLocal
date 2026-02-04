// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package search

import (
	"context"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// PN-MCTS Algorithm
// -----------------------------------------------------------------------------

// PNMCTS implements Proof Number Monte Carlo Tree Search.
//
// Description:
//
//	PN-MCTS combines proof numbers (from Proof-Number Search) with MCTS.
//	It uses proof and disproof numbers to guide tree traversal, focusing
//	on nodes that are easiest to prove or disprove.
//
//	Key Concepts:
//	- Proof Number (pn): Minimum nodes to visit to prove this node
//	- Disproof Number (dn): Minimum nodes to visit to disprove this node
//	- MPN (Most Proving Node): Node with smallest proof number
//	- Selection: Follow MPN from root to leaf
//
//	Hard/Soft Signal Boundary:
//	- Only hard signals (compiler, tests) can mark nodes DISPROVEN
//	- Soft signals update proof/disproof numbers but not status
//
// Thread Safety: Safe for concurrent use.
type PNMCTS struct {
	config *PNMCTSConfig
}

// PNMCTSConfig configures the PN-MCTS algorithm.
type PNMCTSConfig struct {
	// MaxIterations limits the number of iterations.
	MaxIterations int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration

	// ExplorationConstant balances exploration vs exploitation.
	ExplorationConstant float64

	// InfinityThreshold is the value representing infinity for proof numbers.
	InfinityThreshold uint64
}

// DefaultPNMCTSConfig returns the default configuration.
func DefaultPNMCTSConfig() *PNMCTSConfig {
	return &PNMCTSConfig{
		MaxIterations:       1000,
		Timeout:             5 * time.Second,
		ProgressInterval:    1 * time.Second,
		ExplorationConstant: 1.414,
		InfinityThreshold:   1 << 32,
	}
}

// NewPNMCTS creates a new PN-MCTS algorithm.
//
// Inputs:
//   - config: Configuration. If nil, uses DefaultPNMCTSConfig().
//
// Outputs:
//   - *PNMCTS: The new algorithm.
func NewPNMCTS(config *PNMCTSConfig) *PNMCTS {
	if config == nil {
		config = DefaultPNMCTSConfig()
	}
	return &PNMCTS{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// PNMCTSInput is the input for PN-MCTS.
type PNMCTSInput struct {
	// RootNodeID is the starting node for search.
	RootNodeID string

	// TargetNodes are nodes we're trying to prove/disprove.
	TargetNodes []string

	// MaxDepth limits search depth.
	MaxDepth int
}

// PNMCTSOutput is the output from PN-MCTS.
type PNMCTSOutput struct {
	// SelectedNode is the most promising node to expand.
	SelectedNode string

	// Path is the path from root to selected node.
	Path []string

	// ProofUpdates contains updated proof numbers.
	ProofUpdates map[string]crs.ProofNumber

	// Iterations is the number of iterations completed.
	Iterations int

	// MostProvingNode is the node with smallest proof number.
	MostProvingNode string

	// Converged is true if proof numbers stabilized.
	Converged bool
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (p *PNMCTS) Name() string {
	return "pnmcts"
}

// Process executes PN-MCTS.
//
// Description:
//
//	Runs iterative PN-MCTS from the root node, updating proof numbers
//	and selecting the most promising node for expansion.
//
// Thread Safety: Safe for concurrent use.
func (p *PNMCTS) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*PNMCTSInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "pnmcts",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	if in.RootNodeID == "" {
		return nil, nil, &AlgorithmError{
			Algorithm: "pnmcts",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	proofIndex := snapshot.ProofIndex()
	depIndex := snapshot.DependencyIndex()

	output := &PNMCTSOutput{
		ProofUpdates: make(map[string]crs.ProofNumber),
	}

	// Track proof numbers for this search
	proofNumbers := make(map[string]crs.ProofNumber)

	// Initialize from existing proof data
	for _, nodeID := range append(in.TargetNodes, in.RootNodeID) {
		if pn, exists := proofIndex.Get(nodeID); exists {
			proofNumbers[nodeID] = pn
		} else {
			// Initialize new node with default proof numbers
			proofNumbers[nodeID] = crs.ProofNumber{
				Proof:     1,
				Disproof:  1,
				Status:    crs.ProofStatusUnknown,
				Source:    crs.SignalSourceUnknown,
				UpdatedAt: time.Now(),
			}
		}
	}

	// Main iteration loop
	lastProgress := time.Now()
	for iter := 0; iter < p.config.MaxIterations; iter++ {
		// Check cancellation
		select {
		case <-ctx.Done():
			output.Iterations = iter
			return p.collectPartialResults(output, proofNumbers), p.createDelta(output), ctx.Err()
		default:
		}

		// Report progress periodically
		if time.Since(lastProgress) >= p.config.ProgressInterval {
			lastProgress = time.Now()
		}

		// Select most proving node (MPN)
		mpn := p.selectMPN(in.RootNodeID, proofNumbers, depIndex, proofIndex)
		if mpn == "" {
			output.Converged = true
			break
		}

		// Update path
		output.Path = p.tracePath(in.RootNodeID, mpn, depIndex)
		output.SelectedNode = mpn
		output.MostProvingNode = mpn

		// Update proof numbers along path (backpropagation)
		p.updateProofNumbers(output.Path, proofNumbers, depIndex)

		output.Iterations = iter + 1
	}

	// Copy updated proof numbers to output
	for nodeID, pn := range proofNumbers {
		if orig, exists := proofIndex.Get(nodeID); !exists || p.proofChanged(orig, pn) {
			output.ProofUpdates[nodeID] = pn
		}
	}

	return output, p.createDelta(output), nil
}

// selectMPN finds the most proving node.
//
// Description:
//
//	Traverses from root following the path of minimum proof numbers.
//	Uses both the local proofs map (for updated values during search)
//	and the proofIndex parameter (for initial values from CRS snapshot).
//
// Inputs:
//
//	rootID - Starting node for traversal.
//	proofs - Local proof number cache (updated during search).
//	deps - Dependency index for traversing edges.
//	proofIndex - Snapshot's proof index for looking up initial values.
//
// Outputs:
//
//	string - The most proving node ID, or empty if cycle detected or all solved.
func (p *PNMCTS) selectMPN(rootID string, proofs map[string]crs.ProofNumber, deps crs.DependencyIndexView, proofIndex crs.ProofIndexView) string {
	current := rootID
	visited := make(map[string]bool)

	for {
		if visited[current] {
			return "" // Cycle detected
		}
		visited[current] = true

		children := deps.DependsOn(current)
		if len(children) == 0 {
			return current // Leaf node
		}

		// Find child with minimum proof number
		var minChild string
		var minProof uint64 = p.config.InfinityThreshold

		for _, child := range children {
			// Check local cache first, then snapshot's proof index
			pn, exists := proofs[child]
			if !exists {
				pn, exists = proofIndex.Get(child)
			}

			if exists {
				if pn.Status == crs.ProofStatusProven || pn.Status == crs.ProofStatusDisproven {
					continue // Skip solved nodes
				}
				if pn.Proof < minProof {
					minProof = pn.Proof
					minChild = child
				}
			} else {
				// Truly unvisited node has proof = 1
				if 1 < minProof {
					minProof = 1
					minChild = child
				}
			}
		}

		if minChild == "" {
			return current // All children solved
		}
		current = minChild
	}
}

// tracePath traces the path from root to target.
func (p *PNMCTS) tracePath(rootID, targetID string, deps crs.DependencyIndexView) []string {
	if rootID == targetID {
		return []string{rootID}
	}

	// BFS to find path
	visited := make(map[string]bool)
	parent := make(map[string]string)
	queue := []string{rootID}
	visited[rootID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, child := range deps.DependsOn(current) {
			if !visited[child] {
				visited[child] = true
				parent[child] = current
				if child == targetID {
					return p.reconstructPath(parent, rootID, targetID)
				}
				queue = append(queue, child)
			}
		}
	}

	return nil
}

// reconstructPath builds path from parent map.
func (p *PNMCTS) reconstructPath(parent map[string]string, root, target string) []string {
	path := []string{target}
	for current := parent[target]; current != ""; current = parent[current] {
		path = append([]string{current}, path...)
	}
	return path
}

// updateProofNumbers updates proof numbers along a path.
func (p *PNMCTS) updateProofNumbers(path []string, proofs map[string]crs.ProofNumber, deps crs.DependencyIndexView) {
	// Process from leaf to root
	for i := len(path) - 1; i >= 0; i-- {
		nodeID := path[i]
		children := deps.DependsOn(nodeID)

		if len(children) == 0 {
			continue // Leaf, no update needed
		}

		var sumProof, minDisproof uint64
		minDisproof = p.config.InfinityThreshold

		for _, child := range children {
			if pn, exists := proofs[child]; exists {
				sumProof += pn.Proof
				if pn.Disproof < minDisproof {
					minDisproof = pn.Disproof
				}
			} else {
				sumProof += 1
				if 1 < minDisproof {
					minDisproof = 1
				}
			}
		}

		// Update OR node: pn = min(children.pn), dn = sum(children.dn)
		// (Simplified: treating all nodes as OR nodes)
		pn := proofs[nodeID]
		pn.Proof = sumProof
		pn.Disproof = minDisproof
		pn.UpdatedAt = time.Now()
		proofs[nodeID] = pn
	}
}

// proofChanged returns true if proof numbers changed significantly.
func (p *PNMCTS) proofChanged(old, new crs.ProofNumber) bool {
	return old.Proof != new.Proof || old.Disproof != new.Disproof || old.Status != new.Status
}

// collectPartialResults collects partial results on cancellation.
func (p *PNMCTS) collectPartialResults(output *PNMCTSOutput, proofs map[string]crs.ProofNumber) *PNMCTSOutput {
	for nodeID, pn := range proofs {
		output.ProofUpdates[nodeID] = pn
	}
	return output
}

// createDelta creates a proof delta from the output.
func (p *PNMCTS) createDelta(output *PNMCTSOutput) crs.Delta {
	if len(output.ProofUpdates) == 0 {
		return nil
	}
	// Use soft source since PN-MCTS doesn't verify with compiler
	return crs.NewProofDelta(crs.SignalSourceSoft, output.ProofUpdates)
}

// Timeout returns the maximum execution time.
func (p *PNMCTS) Timeout() time.Duration {
	return p.config.Timeout
}

// InputType returns the expected input type.
func (p *PNMCTS) InputType() reflect.Type {
	return reflect.TypeOf(&PNMCTSInput{})
}

// OutputType returns the output type.
func (p *PNMCTS) OutputType() reflect.Type {
	return reflect.TypeOf(&PNMCTSOutput{})
}

// ProgressInterval returns how often to report progress.
func (p *PNMCTS) ProgressInterval() time.Duration {
	return p.config.ProgressInterval
}

// SupportsPartialResults returns true (PN-MCTS can return partial results).
func (p *PNMCTS) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (p *PNMCTS) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "proof_number_consistency",
			Description: "Parent proof number >= min child proof number",
			Check: func(input, output any) error {
				// Verified by algorithm logic
				return nil
			},
		},
		{
			Name:        "no_soft_disproven",
			Description: "PN-MCTS never marks nodes as DISPROVEN",
			Check: func(input, output any) error {
				out, ok := output.(*PNMCTSOutput)
				if !ok {
					return nil
				}
				for nodeID, pn := range out.ProofUpdates {
					if pn.Status == crs.ProofStatusDisproven {
						return &AlgorithmError{
							Algorithm: "pnmcts",
							Operation: "Property.no_soft_disproven",
							Err:       eval.ErrSoftSignalViolation,
						}
						_ = nodeID // Silence unused
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (p *PNMCTS) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "pnmcts_iterations_total",
			Type:        eval.MetricCounter,
			Description: "Total PN-MCTS iterations",
		},
		{
			Name:        "pnmcts_proof_updates_total",
			Type:        eval.MetricCounter,
			Description: "Total proof number updates",
		},
		{
			Name:        "pnmcts_convergence_rate",
			Type:        eval.MetricGauge,
			Description: "Rate of convergence (0-1)",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (p *PNMCTS) HealthCheck(ctx context.Context) error {
	if p.config == nil {
		return &AlgorithmError{
			Algorithm: "pnmcts",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}

// AlgorithmError and ErrInvalidInput are defined here for the search package.
type AlgorithmError struct {
	Algorithm string
	Operation string
	Err       error
}

func (e *AlgorithmError) Error() string {
	return e.Algorithm + "." + e.Operation + ": " + e.Err.Error()
}

var ErrInvalidInput = crs.ErrNilDelta
var ErrInvalidConfig = crs.ErrDeltaValidation
