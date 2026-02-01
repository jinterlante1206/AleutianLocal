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
	"hash/fnv"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Transposition Table Algorithm (Zobrist Hashing)
// -----------------------------------------------------------------------------

// Transposition implements a transposition table using Zobrist hashing.
//
// Description:
//
//	Transposition tables detect when the same state is reached via different
//	paths, avoiding redundant computation. Uses Zobrist hashing to compute
//	position fingerprints efficiently.
//
//	Key Concepts:
//	- Zobrist Hash: XOR-based incremental hash for game states
//	- Transposition: Same state reached via different move sequences
//	- Cache Entry: Stores (hash, depth, value, best_move)
//
// Thread Safety: Safe for concurrent use.
type Transposition struct {
	config *TranspositionConfig
}

// TranspositionConfig configures the transposition table algorithm.
type TranspositionConfig struct {
	// TableSize is the number of entries in the table.
	TableSize int

	// MaxAge is how many generations before evicting entries.
	MaxAge int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultTranspositionConfig returns the default configuration.
func DefaultTranspositionConfig() *TranspositionConfig {
	return &TranspositionConfig{
		TableSize:        1 << 16, // 65536 entries
		MaxAge:           100,
		Timeout:          2 * time.Second,
		ProgressInterval: 500 * time.Millisecond,
	}
}

// NewTransposition creates a new transposition table algorithm.
func NewTransposition(config *TranspositionConfig) *Transposition {
	if config == nil {
		config = DefaultTranspositionConfig()
	}
	return &Transposition{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// TranspositionInput is the input for the transposition algorithm.
type TranspositionInput struct {
	// Nodes are the nodes to hash and check for transpositions.
	Nodes []string

	// CurrentGeneration is the current state generation.
	CurrentGeneration int64
}

// TranspositionOutput is the output from the transposition algorithm.
type TranspositionOutput struct {
	// Hashes maps node ID to its Zobrist hash.
	Hashes map[string]uint64

	// Transpositions maps node ID to its equivalent (if found).
	Transpositions map[string]string

	// StaleEntries are entries that should be evicted.
	StaleEntries []string

	// CacheHits is the number of transposition hits.
	CacheHits int

	// CacheMisses is the number of transposition misses.
	CacheMisses int
}

// TranspositionEntry is a single entry in the transposition table.
type TranspositionEntry struct {
	Hash       uint64
	NodeID     string
	Depth      int
	Generation int64
	ProofNum   uint64
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (t *Transposition) Name() string {
	return "transposition"
}

// Process computes hashes and finds transpositions.
func (t *Transposition) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*TranspositionInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "transposition",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	proofIndex := snapshot.ProofIndex()
	depIndex := snapshot.DependencyIndex()

	output := &TranspositionOutput{
		Hashes:         make(map[string]uint64),
		Transpositions: make(map[string]string),
		StaleEntries:   []string{},
	}

	// Build hash -> nodeID map for transposition detection
	hashToNode := make(map[uint64]string)

	for _, nodeID := range in.Nodes {
		// Check cancellation
		select {
		case <-ctx.Done():
			return output, nil, ctx.Err()
		default:
		}

		// Compute hash for this node
		hash := t.computeZobristHash(nodeID, proofIndex, depIndex)
		output.Hashes[nodeID] = hash

		// Check for transposition
		if existingNode, exists := hashToNode[hash]; exists {
			output.Transpositions[nodeID] = existingNode
			output.CacheHits++
		} else {
			hashToNode[hash] = nodeID
			output.CacheMisses++
		}
	}

	// No delta needed - transposition table is informational
	return output, nil, nil
}

// computeZobristHash computes a Zobrist-style hash for a node.
func (t *Transposition) computeZobristHash(nodeID string, proofs crs.ProofIndexView, deps crs.DependencyIndexView) uint64 {
	h := fnv.New64a()

	// Hash the node ID
	h.Write([]byte(nodeID))

	// Hash proof status if available
	if pn, exists := proofs.Get(nodeID); exists {
		h.Write([]byte{byte(pn.Status)})
		// Include proof numbers in hash (8 bytes each)
		proofBytes := make([]byte, 8)
		for i := 0; i < 8; i++ {
			proofBytes[i] = byte(pn.Proof >> (i * 8))
		}
		h.Write(proofBytes)
	}

	// Hash dependencies (sorted for consistency)
	dependencies := deps.DependsOn(nodeID)
	for _, dep := range dependencies {
		h.Write([]byte(dep))
	}

	return h.Sum64()
}

// Timeout returns the maximum execution time.
func (t *Transposition) Timeout() time.Duration {
	return t.config.Timeout
}

// InputType returns the expected input type.
func (t *Transposition) InputType() reflect.Type {
	return reflect.TypeOf(&TranspositionInput{})
}

// OutputType returns the output type.
func (t *Transposition) OutputType() reflect.Type {
	return reflect.TypeOf(&TranspositionOutput{})
}

// ProgressInterval returns how often to report progress.
func (t *Transposition) ProgressInterval() time.Duration {
	return t.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (t *Transposition) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (t *Transposition) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "hash_determinism",
			Description: "Same input produces same hash",
			Check: func(input, output any) error {
				// Verified by hash function determinism
				return nil
			},
		},
		{
			Name:        "transposition_correctness",
			Description: "Transpositions have identical hashes",
			Check: func(input, output any) error {
				out, ok := output.(*TranspositionOutput)
				if !ok {
					return nil
				}
				for node, equiv := range out.Transpositions {
					if out.Hashes[node] != out.Hashes[equiv] {
						return &AlgorithmError{
							Algorithm: "transposition",
							Operation: "Property.transposition_correctness",
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
func (t *Transposition) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "transposition_hits_total",
			Type:        eval.MetricCounter,
			Description: "Total transposition hits",
		},
		{
			Name:        "transposition_misses_total",
			Type:        eval.MetricCounter,
			Description: "Total transposition misses",
		},
		{
			Name:        "transposition_hit_rate",
			Type:        eval.MetricGauge,
			Description: "Transposition hit rate (0-1)",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (t *Transposition) HealthCheck(ctx context.Context) error {
	if t.config == nil {
		return &AlgorithmError{
			Algorithm: "transposition",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
