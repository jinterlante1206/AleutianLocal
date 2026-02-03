// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package streaming

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Locality-Sensitive Hashing (LSH) Algorithm
// -----------------------------------------------------------------------------

// LSH implements Locality-Sensitive Hashing for approximate nearest neighbor search.
//
// Description:
//
//	LSH hashes similar items into the same buckets with high probability.
//	It uses banding technique with MinHash signatures to find candidate
//	pairs with similarity above a threshold.
//
//	Key Properties:
//	- Sublinear query time for approximate nearest neighbors
//	- Tunable threshold via bands (b) and rows (r) parameters
//	- Threshold ≈ (1/b)^(1/r) for b bands of r rows each
//
//	Use Cases:
//	- Find similar code snippets
//	- Near-duplicate detection
//	- Clustering similar symbols
//	- Code search acceleration
//
// Thread Safety: Safe for concurrent use.
type LSH struct {
	config *LSHConfig
}

// LSHConfig configures the LSH algorithm.
type LSHConfig struct {
	// NumBands is the number of bands to split the signature into.
	NumBands int

	// RowsPerBand is the number of rows per band.
	RowsPerBand int

	// SignatureSize is the total signature size (NumBands * RowsPerBand).
	SignatureSize int

	// Threshold is the similarity threshold for candidate pairs.
	Threshold float64

	// MaxCandidates limits the number of candidates returned.
	MaxCandidates int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultLSHConfig returns the default configuration.
func DefaultLSHConfig() *LSHConfig {
	return &LSHConfig{
		NumBands:         20,
		RowsPerBand:      5,
		SignatureSize:    100,
		Threshold:        0.5,
		MaxCandidates:    100,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewLSH creates a new LSH algorithm.
func NewLSH(config *LSHConfig) *LSH {
	if config == nil {
		config = DefaultLSHConfig()
	}
	return &LSH{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// LSHInput is the input for LSH operations.
type LSHInput struct {
	// Operation specifies what to do: "index", "query", or "candidates".
	Operation string

	// ID is the identifier for the item being indexed.
	ID string

	// Signature is the MinHash signature to index or query.
	Signature *MinHashSignature

	// Index is an existing LSH index (for "query" or "index" operations).
	Index *LSHIndex

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// LSHOutput is the output from LSH operations.
type LSHOutput struct {
	// Index is the resulting LSH index.
	Index *LSHIndex

	// Candidates are the IDs of potential similar items.
	Candidates []string

	// CandidateCount is the number of candidates found.
	CandidateCount int

	// BucketsUsed is the number of buckets with items.
	BucketsUsed int
}

// LSHIndex is the LSH index structure.
type LSHIndex struct {
	// Buckets maps band hashes to item IDs.
	// buckets[bandIdx][bandHash] = []itemID
	Buckets []map[uint64][]string

	// Signatures stores the full signatures for similarity verification.
	Signatures map[string]*MinHashSignature

	// NumBands is the number of bands.
	NumBands int

	// RowsPerBand is the rows per band.
	RowsPerBand int

	// ItemCount is the number of indexed items.
	ItemCount int
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (l *LSH) Name() string {
	return "lsh"
}

// Process executes the LSH operation.
//
// Description:
//
//	Supports three operations:
//	- "index": Add a signature to the index
//	- "query": Find candidates similar to a signature
//	- "candidates": Get all candidate pairs from the index
//
// Thread Safety: Safe for concurrent use.
func (l *LSH) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*LSHInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "lsh",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &LSHOutput{}, nil, ctx.Err()
	default:
	}

	var output *LSHOutput
	var err error

	switch in.Operation {
	case "index":
		output, err = l.index(ctx, in)
	case "query":
		output, err = l.query(ctx, in)
	case "candidates":
		output, err = l.allCandidates(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "lsh",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// index adds a signature to the index.
func (l *LSH) index(ctx context.Context, in *LSHInput) (*LSHOutput, error) {
	if in.Signature == nil {
		return nil, &AlgorithmError{
			Algorithm: "lsh",
			Operation: "index",
			Err:       errors.New("signature required"),
		}
	}

	if in.ID == "" {
		return nil, &AlgorithmError{
			Algorithm: "lsh",
			Operation: "index",
			Err:       errors.New("ID required"),
		}
	}

	idx := in.Index
	if idx == nil {
		idx = l.newIndex()
	}

	// Store signature
	idx.Signatures[in.ID] = in.Signature

	// Hash into bands
	bucketsUsed := 0
	for b := 0; b < idx.NumBands; b++ {
		select {
		case <-ctx.Done():
			return &LSHOutput{Index: idx, BucketsUsed: bucketsUsed}, ctx.Err()
		default:
		}

		bandHash := l.hashBand(in.Signature, b, idx.RowsPerBand)

		if idx.Buckets[b][bandHash] == nil {
			bucketsUsed++
		}
		idx.Buckets[b][bandHash] = append(idx.Buckets[b][bandHash], in.ID)
	}

	idx.ItemCount++

	return &LSHOutput{
		Index:       idx,
		BucketsUsed: bucketsUsed,
	}, nil
}

// query finds candidates similar to a signature.
func (l *LSH) query(ctx context.Context, in *LSHInput) (*LSHOutput, error) {
	if in.Signature == nil {
		return nil, &AlgorithmError{
			Algorithm: "lsh",
			Operation: "query",
			Err:       errors.New("signature required"),
		}
	}

	if in.Index == nil {
		return &LSHOutput{
			Candidates:     []string{},
			CandidateCount: 0,
		}, nil
	}

	// Collect candidates from all bands
	candidateSet := make(map[string]bool)

	for b := 0; b < in.Index.NumBands; b++ {
		select {
		case <-ctx.Done():
			candidates := l.setToSlice(candidateSet)
			return &LSHOutput{
				Index:          in.Index,
				Candidates:     candidates,
				CandidateCount: len(candidates),
			}, ctx.Err()
		default:
		}

		bandHash := l.hashBand(in.Signature, b, in.Index.RowsPerBand)

		for _, id := range in.Index.Buckets[b][bandHash] {
			candidateSet[id] = true

			if len(candidateSet) >= l.config.MaxCandidates {
				candidates := l.setToSlice(candidateSet)
				return &LSHOutput{
					Index:          in.Index,
					Candidates:     candidates,
					CandidateCount: len(candidates),
				}, nil
			}
		}
	}

	candidates := l.setToSlice(candidateSet)

	return &LSHOutput{
		Index:          in.Index,
		Candidates:     candidates,
		CandidateCount: len(candidates),
	}, nil
}

// allCandidates returns all candidate pairs from the index.
func (l *LSH) allCandidates(ctx context.Context, in *LSHInput) (*LSHOutput, error) {
	if in.Index == nil {
		return &LSHOutput{
			Candidates:     []string{},
			CandidateCount: 0,
		}, nil
	}

	// Collect all IDs that share a bucket
	candidateSet := make(map[string]bool)

	for b := 0; b < in.Index.NumBands; b++ {
		for _, ids := range in.Index.Buckets[b] {
			select {
			case <-ctx.Done():
				candidates := l.setToSlice(candidateSet)
				return &LSHOutput{
					Index:          in.Index,
					Candidates:     candidates,
					CandidateCount: len(candidates),
				}, ctx.Err()
			default:
			}

			// If multiple items in bucket, they're candidates
			if len(ids) > 1 {
				for _, id := range ids {
					candidateSet[id] = true
				}
			}
		}
	}

	candidates := l.setToSlice(candidateSet)

	return &LSHOutput{
		Index:          in.Index,
		Candidates:     candidates,
		CandidateCount: len(candidates),
	}, nil
}

// newIndex creates a new empty LSH index.
func (l *LSH) newIndex() *LSHIndex {
	buckets := make([]map[uint64][]string, l.config.NumBands)
	for i := range buckets {
		buckets[i] = make(map[uint64][]string)
	}

	return &LSHIndex{
		Buckets:     buckets,
		Signatures:  make(map[string]*MinHashSignature),
		NumBands:    l.config.NumBands,
		RowsPerBand: l.config.RowsPerBand,
		ItemCount:   0,
	}
}

// hashBand computes the hash for a band of the signature.
func (l *LSH) hashBand(sig *MinHashSignature, bandIdx, rowsPerBand int) uint64 {
	start := bandIdx * rowsPerBand
	end := start + rowsPerBand

	if end > len(sig.Values) {
		end = len(sig.Values)
	}

	// Combine values in this band
	var hash uint64 = 0x9e3779b97f4a7c15
	for i := start; i < end; i++ {
		hash ^= sig.Values[i]
		hash *= 0x6c62272e07bb0142
	}

	return hash
}

// setToSlice converts a set to a slice.
func (l *LSH) setToSlice(set map[string]bool) []string {
	result := make([]string, 0, len(set))
	for k := range set {
		result = append(result, k)
	}
	return result
}

// Timeout returns the maximum execution time.
func (l *LSH) Timeout() time.Duration {
	return l.config.Timeout
}

// InputType returns the expected input type.
func (l *LSH) InputType() reflect.Type {
	return reflect.TypeOf(&LSHInput{})
}

// OutputType returns the output type.
func (l *LSH) OutputType() reflect.Type {
	return reflect.TypeOf(&LSHOutput{})
}

// ProgressInterval returns how often to report progress.
func (l *LSH) ProgressInterval() time.Duration {
	return l.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (l *LSH) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (l *LSH) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "candidates_in_index",
			Description: "All candidates exist in the index",
			Check: func(input, output any) error {
				out, ok := output.(*LSHOutput)
				if !ok || out.Index == nil {
					return nil
				}

				for _, cand := range out.Candidates {
					if _, exists := out.Index.Signatures[cand]; !exists {
						return &AlgorithmError{
							Algorithm: "lsh",
							Operation: "Property.candidates_in_index",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "candidate_count_matches",
			Description: "CandidateCount matches Candidates length",
			Check: func(input, output any) error {
				out, ok := output.(*LSHOutput)
				if !ok {
					return nil
				}

				if out.CandidateCount != len(out.Candidates) {
					return &AlgorithmError{
						Algorithm: "lsh",
						Operation: "Property.candidate_count_matches",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "buckets_have_valid_bands",
			Description: "Index has correct number of bands",
			Check: func(input, output any) error {
				out, ok := output.(*LSHOutput)
				if !ok || out.Index == nil {
					return nil
				}

				if len(out.Index.Buckets) != out.Index.NumBands {
					return &AlgorithmError{
						Algorithm: "lsh",
						Operation: "Property.buckets_have_valid_bands",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (l *LSH) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "lsh_items_indexed_total",
			Type:        eval.MetricCounter,
			Description: "Total items indexed",
		},
		{
			Name:        "lsh_queries_total",
			Type:        eval.MetricCounter,
			Description: "Total queries performed",
		},
		{
			Name:        "lsh_candidates_returned_total",
			Type:        eval.MetricCounter,
			Description: "Total candidates returned",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (l *LSH) HealthCheck(ctx context.Context) error {
	if l.config == nil {
		return &AlgorithmError{
			Algorithm: "lsh",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if l.config.NumBands <= 0 {
		return &AlgorithmError{
			Algorithm: "lsh",
			Operation: "HealthCheck",
			Err:       errors.New("num bands must be positive"),
		}
	}
	if l.config.RowsPerBand <= 0 {
		return &AlgorithmError{
			Algorithm: "lsh",
			Operation: "HealthCheck",
			Err:       errors.New("rows per band must be positive"),
		}
	}
	return nil
}
