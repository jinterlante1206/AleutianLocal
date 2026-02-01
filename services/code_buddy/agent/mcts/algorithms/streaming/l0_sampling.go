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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// L0 Sampling Algorithm
// -----------------------------------------------------------------------------

// L0Sampling implements L0 sampling for sparse recovery.
//
// Description:
//
//	L0 Sampling uniformly samples from the non-zero entries of a sparse
//	vector in a streaming setting. It uses multiple levels of sampling
//	with geometric probabilities to achieve uniform sampling.
//
//	Key Properties:
//	- Space: O(k log^2 n) for k samples from n possible entries
//	- Supports updates (increment/decrement)
//	- Returns uniform samples from non-zero entries
//	- Works with turnstile streams (positive and negative updates)
//
//	Use Cases:
//	- Sample active code paths
//	- Random symbol selection
//	- Sparse feature recovery
//	- Approximate distinct sampling
//
// Thread Safety: Safe for concurrent use.
type L0Sampling struct {
	config *L0Config
}

// L0Config configures the L0 sampling algorithm.
type L0Config struct {
	// NumLevels is the number of sampling levels.
	NumLevels int

	// NumSamples is the number of samples to maintain.
	NumSamples int

	// MaxItems is the maximum number of distinct items.
	MaxItems int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultL0Config returns the default configuration.
func DefaultL0Config() *L0Config {
	return &L0Config{
		NumLevels:        20,
		NumSamples:       10,
		MaxItems:         100000,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewL0Sampling creates a new L0 sampling algorithm.
func NewL0Sampling(config *L0Config) *L0Sampling {
	if config == nil {
		config = DefaultL0Config()
	}
	return &L0Sampling{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// L0Input is the input for L0 sampling operations.
type L0Input struct {
	// Operation specifies what to do: "update", "sample", or "merge".
	Operation string

	// Updates are the items to update (for "update" operation).
	Updates []L0Update

	// State is an existing L0 sampling state.
	State *L0State

	// OtherState is another state to merge with.
	OtherState *L0State

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// L0Update represents an update to an item.
type L0Update struct {
	Key   string
	Delta int64 // Can be positive or negative
}

// L0Output is the output from L0 sampling operations.
type L0Output struct {
	// State is the resulting L0 sampling state.
	State *L0State

	// Samples are the sampled items.
	Samples []L0Sample

	// UpdatesProcessed is the number of updates processed.
	UpdatesProcessed int

	// NonZeroEstimate is the estimated number of non-zero entries.
	NonZeroEstimate int64
}

// L0Sample represents a sampled item.
type L0Sample struct {
	Key   string
	Value int64
	Level int
}

// L0State is the internal L0 sampling state.
type L0State struct {
	// Levels holds the sampling state at each level.
	// Each level samples with probability 2^(-level).
	Levels []L0Level

	// NumLevels is the number of levels.
	NumLevels int

	// NumSamples is the number of samples to maintain.
	NumSamples int

	// Seeds for hash functions.
	Seeds []uint64

	// TotalUpdates is the total number of updates applied.
	TotalUpdates int64
}

// L0Level is a single level of the L0 sampler.
type L0Level struct {
	// Samples holds the current samples at this level.
	Samples map[string]int64

	// Count is the number of items that hashed to this level.
	Count int64
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (l *L0Sampling) Name() string {
	return "l0_sampling"
}

// Process executes the L0 sampling operation.
//
// Description:
//
//	Supports three operations:
//	- "update": Apply updates to the sampler
//	- "sample": Get current samples
//	- "merge": Merge two samplers
//
// Thread Safety: Safe for concurrent use.
func (l *L0Sampling) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*L0Input)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &L0Output{}, nil, ctx.Err()
	default:
	}

	var output *L0Output
	var err error

	switch in.Operation {
	case "update":
		output, err = l.update(ctx, in)
	case "sample":
		output, err = l.sample(ctx, in)
	case "merge":
		output, err = l.merge(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// update applies updates to the sampler.
func (l *L0Sampling) update(ctx context.Context, in *L0Input) (*L0Output, error) {
	state := in.State
	if state == nil {
		state = l.newState()
	}

	processed := 0
	for _, upd := range in.Updates {
		select {
		case <-ctx.Done():
			return &L0Output{
				State:            state,
				UpdatesProcessed: processed,
			}, ctx.Err()
		default:
		}

		l.applyUpdate(state, upd)
		processed++
	}

	return &L0Output{
		State:            state,
		UpdatesProcessed: processed,
	}, nil
}

// sample gets the current samples.
func (l *L0Sampling) sample(ctx context.Context, in *L0Input) (*L0Output, error) {
	if in.State == nil {
		return &L0Output{
			Samples:         []L0Sample{},
			NonZeroEstimate: 0,
		}, nil
	}

	samples := l.getSamples(in.State)
	nonZero := l.estimateNonZero(in.State)

	return &L0Output{
		State:           in.State,
		Samples:         samples,
		NonZeroEstimate: nonZero,
	}, nil
}

// merge merges two states.
func (l *L0Sampling) merge(ctx context.Context, in *L0Input) (*L0Output, error) {
	if in.State == nil && in.OtherState == nil {
		return &L0Output{
			State: l.newState(),
		}, nil
	}

	if in.State == nil {
		return &L0Output{
			State: in.OtherState,
		}, nil
	}

	if in.OtherState == nil {
		return &L0Output{
			State: in.State,
		}, nil
	}

	// Check dimensions
	if in.State.NumLevels != in.OtherState.NumLevels {
		return nil, &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "merge",
			Err:       errors.New("state dimensions must match"),
		}
	}

	// Create merged state
	result := l.newState()
	result.TotalUpdates = in.State.TotalUpdates + in.OtherState.TotalUpdates

	// Merge each level
	for level := 0; level < result.NumLevels; level++ {
		select {
		case <-ctx.Done():
			return &L0Output{State: result}, ctx.Err()
		default:
		}

		// Merge samples from both states
		for key, val := range in.State.Levels[level].Samples {
			result.Levels[level].Samples[key] += val
		}
		for key, val := range in.OtherState.Levels[level].Samples {
			result.Levels[level].Samples[key] += val
		}

		// Remove zero entries
		for key, val := range result.Levels[level].Samples {
			if val == 0 {
				delete(result.Levels[level].Samples, key)
			}
		}

		result.Levels[level].Count = in.State.Levels[level].Count + in.OtherState.Levels[level].Count
	}

	return &L0Output{
		State: result,
	}, nil
}

// newState creates a new empty L0 state.
func (l *L0Sampling) newState() *L0State {
	levels := make([]L0Level, l.config.NumLevels)
	for i := range levels {
		levels[i] = L0Level{
			Samples: make(map[string]int64),
			Count:   0,
		}
	}

	seeds := make([]uint64, l.config.NumLevels)
	for i := range seeds {
		seeds[i] = uint64(i*0x9e3779b9 + 0x6c62272e)
	}

	return &L0State{
		Levels:       levels,
		NumLevels:    l.config.NumLevels,
		NumSamples:   l.config.NumSamples,
		Seeds:        seeds,
		TotalUpdates: 0,
	}
}

// applyUpdate applies an update to the state.
func (l *L0Sampling) applyUpdate(state *L0State, upd L0Update) {
	keyHash := l.hash64(upd.Key)

	// Find the level this key belongs to (geometric sampling)
	level := l.findLevel(keyHash, state)

	if level >= 0 && level < state.NumLevels {
		state.Levels[level].Samples[upd.Key] += upd.Delta

		// Remove if becomes zero
		if state.Levels[level].Samples[upd.Key] == 0 {
			delete(state.Levels[level].Samples, upd.Key)
		}

		state.Levels[level].Count++
	}

	state.TotalUpdates++
}

// findLevel finds the appropriate level for a key hash.
func (l *L0Sampling) findLevel(hash uint64, state *L0State) int {
	// Use leading zeros to determine level (geometric distribution)
	for level := 0; level < state.NumLevels; level++ {
		h := hash ^ state.Seeds[level]
		threshold := uint64(math.MaxUint64) >> level

		if h < threshold {
			return level
		}
	}
	return state.NumLevels - 1
}

// getSamples returns current samples from the state.
func (l *L0Sampling) getSamples(state *L0State) []L0Sample {
	samples := make([]L0Sample, 0, l.config.NumSamples)

	// Find the lowest level with samples
	for level := 0; level < state.NumLevels && len(samples) < l.config.NumSamples; level++ {
		for key, val := range state.Levels[level].Samples {
			if val != 0 {
				samples = append(samples, L0Sample{
					Key:   key,
					Value: val,
					Level: level,
				})

				if len(samples) >= l.config.NumSamples {
					break
				}
			}
		}
	}

	return samples
}

// estimateNonZero estimates the number of non-zero entries.
func (l *L0Sampling) estimateNonZero(state *L0State) int64 {
	// Find the first level with reasonable sample count
	for level := 0; level < state.NumLevels; level++ {
		count := int64(len(state.Levels[level].Samples))
		if count > 0 && count <= int64(l.config.NumSamples*2) {
			// Scale by sampling probability
			return count << level
		}
	}

	// If all levels have too many samples, use the last level
	lastLevel := state.NumLevels - 1
	return int64(len(state.Levels[lastLevel].Samples)) << lastLevel
}

// hash64 computes a 64-bit hash.
func (l *L0Sampling) hash64(s string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

// Timeout returns the maximum execution time.
func (l *L0Sampling) Timeout() time.Duration {
	return l.config.Timeout
}

// InputType returns the expected input type.
func (l *L0Sampling) InputType() reflect.Type {
	return reflect.TypeOf(&L0Input{})
}

// OutputType returns the output type.
func (l *L0Sampling) OutputType() reflect.Type {
	return reflect.TypeOf(&L0Output{})
}

// ProgressInterval returns how often to report progress.
func (l *L0Sampling) ProgressInterval() time.Duration {
	return l.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (l *L0Sampling) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (l *L0Sampling) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "samples_are_non_zero",
			Description: "All samples have non-zero values",
			Check: func(input, output any) error {
				out, ok := output.(*L0Output)
				if !ok {
					return nil
				}

				for _, s := range out.Samples {
					if s.Value == 0 {
						return &AlgorithmError{
							Algorithm: "l0_sampling",
							Operation: "Property.samples_are_non_zero",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "estimate_non_negative",
			Description: "Non-zero estimate is non-negative",
			Check: func(input, output any) error {
				out, ok := output.(*L0Output)
				if !ok {
					return nil
				}

				if out.NonZeroEstimate < 0 {
					return &AlgorithmError{
						Algorithm: "l0_sampling",
						Operation: "Property.estimate_non_negative",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "levels_have_valid_structure",
			Description: "State has valid level structure",
			Check: func(input, output any) error {
				out, ok := output.(*L0Output)
				if !ok || out.State == nil {
					return nil
				}

				if len(out.State.Levels) != out.State.NumLevels {
					return &AlgorithmError{
						Algorithm: "l0_sampling",
						Operation: "Property.levels_have_valid_structure",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (l *L0Sampling) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "l0_updates_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total updates processed",
		},
		{
			Name:        "l0_samples_returned_total",
			Type:        eval.MetricCounter,
			Description: "Total samples returned",
		},
		{
			Name:        "l0_nonzero_estimate",
			Type:        eval.MetricGauge,
			Description: "Estimated non-zero entries",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (l *L0Sampling) HealthCheck(ctx context.Context) error {
	if l.config == nil {
		return &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if l.config.NumLevels <= 0 {
		return &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "HealthCheck",
			Err:       errors.New("num levels must be positive"),
		}
	}
	if l.config.NumSamples <= 0 {
		return &AlgorithmError{
			Algorithm: "l0_sampling",
			Operation: "HealthCheck",
			Err:       errors.New("num samples must be positive"),
		}
	}
	return nil
}
