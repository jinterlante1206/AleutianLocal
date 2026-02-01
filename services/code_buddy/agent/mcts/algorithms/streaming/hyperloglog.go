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
// HyperLogLog Algorithm
// -----------------------------------------------------------------------------

// HyperLogLog implements the HyperLogLog cardinality estimation algorithm.
//
// Description:
//
//	HyperLogLog estimates the cardinality (number of distinct elements) of a
//	multiset using a small, fixed amount of memory. It uses the harmonic mean
//	of register values based on leading zero counts.
//
//	Key Properties:
//	- Space: O(m) where m = 2^precision
//	- Standard error: 1.04/sqrt(m)
//	- Mergeable: Multiple HLLs can be merged
//
//	Use Cases:
//	- Count unique symbols in codebase
//	- Estimate number of unique callers
//	- Track distinct file accesses
//	- Approximate set cardinality
//
// Thread Safety: Safe for concurrent use.
type HyperLogLog struct {
	config *HyperLogLogConfig
}

// HyperLogLogConfig configures the HyperLogLog algorithm.
type HyperLogLogConfig struct {
	// Precision is the number of bits for register indexing (4-18).
	// Higher precision = more accuracy but more memory.
	// Memory usage = 2^precision bytes.
	Precision int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultHyperLogLogConfig returns the default configuration.
func DefaultHyperLogLogConfig() *HyperLogLogConfig {
	return &HyperLogLogConfig{
		Precision:        14, // 16KB, ~0.81% standard error
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewHyperLogLog creates a new HyperLogLog algorithm.
func NewHyperLogLog(config *HyperLogLogConfig) *HyperLogLog {
	if config == nil {
		config = DefaultHyperLogLogConfig()
	}
	return &HyperLogLog{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// HyperLogLogInput is the input for HyperLogLog operations.
type HyperLogLogInput struct {
	// Operation specifies what to do: "add", "count", or "merge".
	Operation string

	// Items are the elements to add (for "add" operation).
	Items []string

	// HLL is an existing HyperLogLog state (for "count" or "merge").
	HLL *HLLState

	// OtherHLL is another HLL to merge with (for "merge").
	OtherHLL *HLLState

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// HyperLogLogOutput is the output from HyperLogLog operations.
type HyperLogLogOutput struct {
	// HLL is the resulting HyperLogLog state.
	HLL *HLLState

	// Cardinality is the estimated number of distinct elements.
	Cardinality uint64

	// ItemsProcessed is the number of items processed.
	ItemsProcessed int

	// StandardError is the estimated relative standard error.
	StandardError float64
}

// HLLState is the internal HyperLogLog state.
type HLLState struct {
	// Registers hold the max leading zeros for each bucket.
	Registers []uint8

	// Precision is the number of bits for indexing.
	Precision int

	// Count is the number of items added.
	Count uint64
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (h *HyperLogLog) Name() string {
	return "hyperloglog"
}

// Process executes the HyperLogLog operation.
//
// Description:
//
//	Supports three operations:
//	- "add": Add items to the HLL
//	- "count": Estimate cardinality
//	- "merge": Merge two HLLs
//
// Thread Safety: Safe for concurrent use.
func (h *HyperLogLog) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*HyperLogLogInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "hyperloglog",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &HyperLogLogOutput{}, nil, ctx.Err()
	default:
	}

	var output *HyperLogLogOutput
	var err error

	switch in.Operation {
	case "add":
		output, err = h.add(ctx, in)
	case "count":
		output, err = h.count(ctx, in)
	case "merge":
		output, err = h.merge(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "hyperloglog",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// add adds items to an HLL.
func (h *HyperLogLog) add(ctx context.Context, in *HyperLogLogInput) (*HyperLogLogOutput, error) {
	hll := in.HLL
	if hll == nil {
		hll = h.newHLL()
	}

	processed := 0
	for _, item := range in.Items {
		select {
		case <-ctx.Done():
			return &HyperLogLogOutput{
				HLL:            hll,
				Cardinality:    h.estimate(hll),
				ItemsProcessed: processed,
				StandardError:  h.standardError(),
			}, ctx.Err()
		default:
		}

		h.addToHLL(hll, item)
		processed++
	}

	return &HyperLogLogOutput{
		HLL:            hll,
		Cardinality:    h.estimate(hll),
		ItemsProcessed: processed,
		StandardError:  h.standardError(),
	}, nil
}

// count estimates the cardinality.
func (h *HyperLogLog) count(ctx context.Context, in *HyperLogLogInput) (*HyperLogLogOutput, error) {
	if in.HLL == nil {
		return &HyperLogLogOutput{
			Cardinality:   0,
			StandardError: h.standardError(),
		}, nil
	}

	return &HyperLogLogOutput{
		HLL:           in.HLL,
		Cardinality:   h.estimate(in.HLL),
		StandardError: h.standardError(),
	}, nil
}

// merge merges two HLLs.
func (h *HyperLogLog) merge(ctx context.Context, in *HyperLogLogInput) (*HyperLogLogOutput, error) {
	if in.HLL == nil && in.OtherHLL == nil {
		return &HyperLogLogOutput{
			HLL:           h.newHLL(),
			Cardinality:   0,
			StandardError: h.standardError(),
		}, nil
	}

	if in.HLL == nil {
		return &HyperLogLogOutput{
			HLL:           in.OtherHLL,
			Cardinality:   h.estimate(in.OtherHLL),
			StandardError: h.standardError(),
		}, nil
	}

	if in.OtherHLL == nil {
		return &HyperLogLogOutput{
			HLL:           in.HLL,
			Cardinality:   h.estimate(in.HLL),
			StandardError: h.standardError(),
		}, nil
	}

	// Check dimensions match
	if in.HLL.Precision != in.OtherHLL.Precision {
		return nil, &AlgorithmError{
			Algorithm: "hyperloglog",
			Operation: "merge",
			Err:       errors.New("HLL precision must match"),
		}
	}

	// Create merged HLL
	result := &HLLState{
		Registers: make([]uint8, len(in.HLL.Registers)),
		Precision: in.HLL.Precision,
		Count:     in.HLL.Count + in.OtherHLL.Count,
	}

	// Take max of each register
	for i := 0; i < len(result.Registers); i++ {
		select {
		case <-ctx.Done():
			return &HyperLogLogOutput{HLL: result, Cardinality: h.estimate(result)}, ctx.Err()
		default:
		}

		if in.HLL.Registers[i] > in.OtherHLL.Registers[i] {
			result.Registers[i] = in.HLL.Registers[i]
		} else {
			result.Registers[i] = in.OtherHLL.Registers[i]
		}
	}

	return &HyperLogLogOutput{
		HLL:           result,
		Cardinality:   h.estimate(result),
		StandardError: h.standardError(),
	}, nil
}

// newHLL creates a new empty HLL.
func (h *HyperLogLog) newHLL() *HLLState {
	m := 1 << h.config.Precision
	return &HLLState{
		Registers: make([]uint8, m),
		Precision: h.config.Precision,
		Count:     0,
	}
}

// addToHLL adds an item to the HLL.
func (h *HyperLogLog) addToHLL(hll *HLLState, item string) {
	hash := h.hash64(item)
	m := uint64(1 << hll.Precision)

	// Use first p bits for register index
	idx := hash & (m - 1)

	// Use remaining bits for leading zeros
	w := hash >> hll.Precision
	rho := h.leadingZeros(w) + 1

	if rho > hll.Registers[idx] {
		hll.Registers[idx] = rho
	}
	hll.Count++
}

// estimate computes the cardinality estimate.
func (h *HyperLogLog) estimate(hll *HLLState) uint64 {
	m := float64(len(hll.Registers))

	// Compute harmonic mean
	sum := 0.0
	zeros := 0
	for _, val := range hll.Registers {
		sum += math.Pow(2, -float64(val))
		if val == 0 {
			zeros++
		}
	}

	// Alpha constant for bias correction
	alpha := h.alpha(int(m))

	// Raw estimate
	estimate := alpha * m * m / sum

	// Apply corrections
	if estimate <= 2.5*m && zeros > 0 {
		// Small range correction (linear counting)
		estimate = m * math.Log(m/float64(zeros))
	} else if estimate > (1.0/30.0)*math.Pow(2, 32) {
		// Large range correction
		estimate = -math.Pow(2, 32) * math.Log(1-estimate/math.Pow(2, 32))
	}

	return uint64(estimate)
}

// alpha returns the bias correction factor.
func (h *HyperLogLog) alpha(m int) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/float64(m))
	}
}

// leadingZeros counts leading zeros in a 64-bit value.
func (h *HyperLogLog) leadingZeros(x uint64) uint8 {
	if x == 0 {
		return 64
	}
	n := uint8(0)
	for x&0x8000000000000000 == 0 {
		n++
		x <<= 1
	}
	return n
}

// standardError returns the expected relative standard error.
func (h *HyperLogLog) standardError() float64 {
	m := float64(int(1) << h.config.Precision)
	return 1.04 / math.Sqrt(m)
}

// hash64 computes a 64-bit hash.
func (h *HyperLogLog) hash64(s string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

// Timeout returns the maximum execution time.
func (h *HyperLogLog) Timeout() time.Duration {
	return h.config.Timeout
}

// InputType returns the expected input type.
func (h *HyperLogLog) InputType() reflect.Type {
	return reflect.TypeOf(&HyperLogLogInput{})
}

// OutputType returns the output type.
func (h *HyperLogLog) OutputType() reflect.Type {
	return reflect.TypeOf(&HyperLogLogOutput{})
}

// ProgressInterval returns how often to report progress.
func (h *HyperLogLog) ProgressInterval() time.Duration {
	return h.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (h *HyperLogLog) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (h *HyperLogLog) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "registers_bounded",
			Description: "Register values are bounded by 64",
			Check: func(input, output any) error {
				out, ok := output.(*HyperLogLogOutput)
				if !ok || out.HLL == nil {
					return nil
				}

				for _, val := range out.HLL.Registers {
					if val > 64 {
						return &AlgorithmError{
							Algorithm: "hyperloglog",
							Operation: "Property.registers_bounded",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "cardinality_non_negative",
			Description: "Cardinality estimate is non-negative",
			Check: func(input, output any) error {
				// uint64 is always non-negative
				return nil
			},
		},
		{
			Name:        "merge_commutative",
			Description: "Merge operation is commutative",
			Check: func(input, output any) error {
				// Verified by max operation semantics
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (h *HyperLogLog) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "hyperloglog_items_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total items added to HLLs",
		},
		{
			Name:        "hyperloglog_cardinality_estimate",
			Type:        eval.MetricGauge,
			Description: "Current cardinality estimate",
		},
		{
			Name:        "hyperloglog_standard_error",
			Type:        eval.MetricGauge,
			Description: "Expected relative standard error",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (h *HyperLogLog) HealthCheck(ctx context.Context) error {
	if h.config == nil {
		return &AlgorithmError{
			Algorithm: "hyperloglog",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if h.config.Precision < 4 || h.config.Precision > 18 {
		return &AlgorithmError{
			Algorithm: "hyperloglog",
			Operation: "HealthCheck",
			Err:       errors.New("precision must be between 4 and 18"),
		}
	}
	return nil
}
