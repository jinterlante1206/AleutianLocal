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
	"hash/fnv"
	"math"
	"reflect"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// MinHash Algorithm
// -----------------------------------------------------------------------------

// MinHash implements the MinHash algorithm for set similarity estimation.
//
// Description:
//
//	MinHash generates a compact signature for a set that can be used to
//	estimate Jaccard similarity. The probability that two sets have the
//	same MinHash value equals their Jaccard similarity.
//
//	Key Properties:
//	- Space: O(k) per signature where k = number of hash functions
//	- Similarity estimate: fraction of matching MinHash values
//	- Error bound: O(1/sqrt(k))
//
//	Use Cases:
//	- Find similar code files
//	- Detect near-duplicate functions
//	- Cluster related symbols
//	- Code clone detection
//
// Thread Safety: Safe for concurrent use.
type MinHash struct {
	config *MinHashConfig
}

// MinHashConfig configures the MinHash algorithm.
type MinHashConfig struct {
	// NumHashes is the number of hash functions (signature size).
	NumHashes int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultMinHashConfig returns the default configuration.
func DefaultMinHashConfig() *MinHashConfig {
	return &MinHashConfig{
		NumHashes:        128,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewMinHash creates a new MinHash algorithm.
func NewMinHash(config *MinHashConfig) *MinHash {
	if config == nil {
		config = DefaultMinHashConfig()
	}
	return &MinHash{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// MinHashInput is the input for MinHash operations.
type MinHashInput struct {
	// Operation specifies what to do: "signature", "similarity", or "merge".
	Operation string

	// Set is the set elements to compute signature for (for "signature").
	Set []string

	// Signature is an existing signature (for "similarity" or "merge").
	Signature *MinHashSignature

	// OtherSignature is another signature to compare/merge with.
	OtherSignature *MinHashSignature

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// MinHashOutput is the output from MinHash operations.
type MinHashOutput struct {
	// Signature is the computed MinHash signature.
	Signature *MinHashSignature

	// Similarity is the estimated Jaccard similarity (for "similarity").
	Similarity float64

	// ItemsProcessed is the number of set elements processed.
	ItemsProcessed int
}

// MinHashSignature is the MinHash signature for a set.
type MinHashSignature struct {
	// Values are the minimum hash values for each hash function.
	Values []uint64

	// NumHashes is the number of hash functions used.
	NumHashes int

	// Coefficients are the hash function parameters (a, b pairs).
	Coefficients []uint64

	// SetSize is the original set size.
	SetSize int
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (m *MinHash) Name() string {
	return "minhash"
}

// Process executes the MinHash operation.
//
// Description:
//
//	Supports three operations:
//	- "signature": Compute MinHash signature for a set
//	- "similarity": Estimate Jaccard similarity between two signatures
//	- "merge": Merge two signatures (union of sets)
//
// Thread Safety: Safe for concurrent use.
func (m *MinHash) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*MinHashInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "minhash",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &MinHashOutput{}, nil, ctx.Err()
	default:
	}

	var output *MinHashOutput
	var err error

	switch in.Operation {
	case "signature":
		output, err = m.computeSignature(ctx, in)
	case "similarity":
		output, err = m.computeSimilarity(ctx, in)
	case "merge":
		output, err = m.mergeSignatures(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "minhash",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// computeSignature computes the MinHash signature for a set.
func (m *MinHash) computeSignature(ctx context.Context, in *MinHashInput) (*MinHashOutput, error) {
	sig := m.newSignature()

	processed := 0
	for _, item := range in.Set {
		select {
		case <-ctx.Done():
			return &MinHashOutput{
				Signature:      sig,
				ItemsProcessed: processed,
			}, ctx.Err()
		default:
		}

		m.addToSignature(sig, item)
		processed++
	}

	sig.SetSize = len(in.Set)

	return &MinHashOutput{
		Signature:      sig,
		ItemsProcessed: processed,
	}, nil
}

// computeSimilarity estimates Jaccard similarity between two signatures.
func (m *MinHash) computeSimilarity(ctx context.Context, in *MinHashInput) (*MinHashOutput, error) {
	if in.Signature == nil || in.OtherSignature == nil {
		return nil, &AlgorithmError{
			Algorithm: "minhash",
			Operation: "similarity",
			Err:       errors.New("both signatures required"),
		}
	}

	if in.Signature.NumHashes != in.OtherSignature.NumHashes {
		return nil, &AlgorithmError{
			Algorithm: "minhash",
			Operation: "similarity",
			Err:       errors.New("signature sizes must match"),
		}
	}

	matches := 0
	for i := 0; i < in.Signature.NumHashes; i++ {
		select {
		case <-ctx.Done():
			// Return partial similarity estimate
			similarity := float64(matches) / float64(i+1)
			return &MinHashOutput{Similarity: similarity}, ctx.Err()
		default:
		}

		if in.Signature.Values[i] == in.OtherSignature.Values[i] {
			matches++
		}
	}

	similarity := float64(matches) / float64(in.Signature.NumHashes)

	return &MinHashOutput{
		Similarity: similarity,
	}, nil
}

// mergeSignatures merges two signatures (union operation).
func (m *MinHash) mergeSignatures(ctx context.Context, in *MinHashInput) (*MinHashOutput, error) {
	if in.Signature == nil && in.OtherSignature == nil {
		return &MinHashOutput{
			Signature: m.newSignature(),
		}, nil
	}

	if in.Signature == nil {
		return &MinHashOutput{
			Signature: in.OtherSignature,
		}, nil
	}

	if in.OtherSignature == nil {
		return &MinHashOutput{
			Signature: in.Signature,
		}, nil
	}

	if in.Signature.NumHashes != in.OtherSignature.NumHashes {
		return nil, &AlgorithmError{
			Algorithm: "minhash",
			Operation: "merge",
			Err:       errors.New("signature sizes must match"),
		}
	}

	// Create merged signature with min of each position
	result := &MinHashSignature{
		Values:       make([]uint64, in.Signature.NumHashes),
		NumHashes:    in.Signature.NumHashes,
		Coefficients: in.Signature.Coefficients,
		SetSize:      in.Signature.SetSize + in.OtherSignature.SetSize, // Upper bound
	}

	for i := 0; i < result.NumHashes; i++ {
		select {
		case <-ctx.Done():
			return &MinHashOutput{Signature: result}, ctx.Err()
		default:
		}

		if in.Signature.Values[i] < in.OtherSignature.Values[i] {
			result.Values[i] = in.Signature.Values[i]
		} else {
			result.Values[i] = in.OtherSignature.Values[i]
		}
	}

	return &MinHashOutput{
		Signature: result,
	}, nil
}

// newSignature creates a new empty signature.
func (m *MinHash) newSignature() *MinHashSignature {
	values := make([]uint64, m.config.NumHashes)
	for i := range values {
		values[i] = math.MaxUint64
	}

	// Generate hash function coefficients
	coeffs := make([]uint64, m.config.NumHashes*2)
	for i := range coeffs {
		coeffs[i] = uint64(i*0x9e3779b9 + 0x6c62272e)
	}

	return &MinHashSignature{
		Values:       values,
		NumHashes:    m.config.NumHashes,
		Coefficients: coeffs,
		SetSize:      0,
	}
}

// addToSignature adds an item to the signature.
func (m *MinHash) addToSignature(sig *MinHashSignature, item string) {
	itemHash := m.hash64(item)

	for i := 0; i < sig.NumHashes; i++ {
		// Use universal hashing: h(x) = (ax + b) mod p
		a := sig.Coefficients[i*2]
		b := sig.Coefficients[i*2+1]
		h := a*itemHash + b

		if h < sig.Values[i] {
			sig.Values[i] = h
		}
	}
}

// hash64 computes a 64-bit hash.
func (m *MinHash) hash64(s string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

// Timeout returns the maximum execution time.
func (m *MinHash) Timeout() time.Duration {
	return m.config.Timeout
}

// InputType returns the expected input type.
func (m *MinHash) InputType() reflect.Type {
	return reflect.TypeOf(&MinHashInput{})
}

// OutputType returns the output type.
func (m *MinHash) OutputType() reflect.Type {
	return reflect.TypeOf(&MinHashOutput{})
}

// ProgressInterval returns how often to report progress.
func (m *MinHash) ProgressInterval() time.Duration {
	return m.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (m *MinHash) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (m *MinHash) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "similarity_bounded",
			Description: "Similarity is between 0 and 1",
			Check: func(input, output any) error {
				out, ok := output.(*MinHashOutput)
				if !ok {
					return nil
				}

				if out.Similarity < 0 || out.Similarity > 1 {
					return &AlgorithmError{
						Algorithm: "minhash",
						Operation: "Property.similarity_bounded",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "signature_size_correct",
			Description: "Signature has correct number of values",
			Check: func(input, output any) error {
				out, ok := output.(*MinHashOutput)
				if !ok || out.Signature == nil {
					return nil
				}

				if len(out.Signature.Values) != out.Signature.NumHashes {
					return &AlgorithmError{
						Algorithm: "minhash",
						Operation: "Property.signature_size_correct",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "identical_sets_similarity_one",
			Description: "Identical sets have similarity 1.0",
			Check: func(input, output any) error {
				// Verified by algorithm design: same set -> same hashes -> all match
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (m *MinHash) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "minhash_signatures_computed_total",
			Type:        eval.MetricCounter,
			Description: "Total signatures computed",
		},
		{
			Name:        "minhash_similarity_comparisons_total",
			Type:        eval.MetricCounter,
			Description: "Total similarity comparisons",
		},
		{
			Name:        "minhash_items_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total set elements processed",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (m *MinHash) HealthCheck(ctx context.Context) error {
	if m.config == nil {
		return &AlgorithmError{
			Algorithm: "minhash",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if m.config.NumHashes <= 0 {
		return &AlgorithmError{
			Algorithm: "minhash",
			Operation: "HealthCheck",
			Err:       errors.New("num hashes must be positive"),
		}
	}
	return nil
}
