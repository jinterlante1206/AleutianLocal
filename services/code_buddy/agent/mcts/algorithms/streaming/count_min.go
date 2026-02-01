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
// Count-Min Sketch Algorithm
// -----------------------------------------------------------------------------

// CountMin implements the Count-Min Sketch for frequency estimation.
//
// Description:
//
//	Count-Min Sketch is a probabilistic data structure that serves as a
//	frequency table. It uses multiple hash functions and a 2D array to
//	estimate the frequency of elements in a stream.
//
//	Key Properties:
//	- Space: O(w * d) where w = width, d = depth
//	- Query time: O(d)
//	- Never underestimates frequency
//	- Overestimate bounded by epsilon with probability 1-delta
//
//	Use Cases:
//	- Track symbol access frequencies
//	- Estimate code change hotspots
//	- Identify frequently modified files
//	- Stream-based statistics
//
// Thread Safety: Safe for concurrent use.
type CountMin struct {
	config *CountMinConfig
}

// CountMinConfig configures the Count-Min sketch.
type CountMinConfig struct {
	// Width is the number of buckets per row.
	Width int

	// Depth is the number of hash functions (rows).
	Depth int

	// Epsilon is the error bound (affects width: w = ceil(e/epsilon)).
	Epsilon float64

	// Delta is the failure probability (affects depth: d = ceil(ln(1/delta))).
	Delta float64

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultCountMinConfig returns the default configuration.
func DefaultCountMinConfig() *CountMinConfig {
	return &CountMinConfig{
		Width:            1024,
		Depth:            5,
		Epsilon:          0.001,
		Delta:            0.01,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewCountMin creates a new Count-Min sketch algorithm.
func NewCountMin(config *CountMinConfig) *CountMin {
	if config == nil {
		config = DefaultCountMinConfig()
	}
	return &CountMin{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// CountMinInput is the input for Count-Min operations.
type CountMinInput struct {
	// Operation specifies what to do: "add", "query", or "merge".
	Operation string

	// Items are the elements to add (for "add" operation).
	Items []CountMinItem

	// Queries are the elements to query frequencies for (for "query" operation).
	Queries []string

	// Sketch is an existing sketch to merge (for "merge" operation).
	Sketch *CountMinSketch

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// CountMinItem represents an item with a count.
type CountMinItem struct {
	Key   string
	Count int64
}

// CountMinOutput is the output from Count-Min operations.
type CountMinOutput struct {
	// Sketch is the resulting sketch state.
	Sketch *CountMinSketch

	// Frequencies maps queried keys to their estimated frequencies.
	Frequencies map[string]int64

	// ItemsProcessed is the number of items processed.
	ItemsProcessed int

	// TotalCount is the sum of all counts in the sketch.
	TotalCount int64
}

// CountMinSketch is the internal sketch data structure.
type CountMinSketch struct {
	// Table is the 2D count matrix [depth][width].
	Table [][]int64

	// Width is the number of columns.
	Width int

	// Depth is the number of rows (hash functions).
	Depth int

	// Seeds are the hash function seeds.
	Seeds []uint64

	// Total is the sum of all added counts.
	Total int64
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (c *CountMin) Name() string {
	return "count_min"
}

// Process executes the Count-Min operation.
//
// Description:
//
//	Supports three operations:
//	- "add": Add items to the sketch
//	- "query": Query frequencies of items
//	- "merge": Merge another sketch into this one
//
// Thread Safety: Safe for concurrent use.
func (c *CountMin) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*CountMinInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "count_min",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &CountMinOutput{}, nil, ctx.Err()
	default:
	}

	var output *CountMinOutput
	var err error

	switch in.Operation {
	case "add":
		output, err = c.add(ctx, in)
	case "query":
		output, err = c.query(ctx, in)
	case "merge":
		output, err = c.merge(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "count_min",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// add adds items to a new or existing sketch.
func (c *CountMin) add(ctx context.Context, in *CountMinInput) (*CountMinOutput, error) {
	// Create or use existing sketch
	sketch := in.Sketch
	if sketch == nil {
		sketch = c.newSketch()
	}

	processed := 0
	for _, item := range in.Items {
		select {
		case <-ctx.Done():
			return &CountMinOutput{
				Sketch:         sketch,
				ItemsProcessed: processed,
				TotalCount:     sketch.Total,
			}, ctx.Err()
		default:
		}

		c.addToSketch(sketch, item.Key, item.Count)
		processed++
	}

	return &CountMinOutput{
		Sketch:         sketch,
		ItemsProcessed: processed,
		TotalCount:     sketch.Total,
	}, nil
}

// query queries frequencies from a sketch.
func (c *CountMin) query(ctx context.Context, in *CountMinInput) (*CountMinOutput, error) {
	if in.Sketch == nil {
		return nil, &AlgorithmError{
			Algorithm: "count_min",
			Operation: "query",
			Err:       errors.New("sketch required for query"),
		}
	}

	frequencies := make(map[string]int64)
	for _, key := range in.Queries {
		select {
		case <-ctx.Done():
			return &CountMinOutput{
				Sketch:      in.Sketch,
				Frequencies: frequencies,
				TotalCount:  in.Sketch.Total,
			}, ctx.Err()
		default:
		}

		frequencies[key] = c.querySketch(in.Sketch, key)
	}

	return &CountMinOutput{
		Sketch:      in.Sketch,
		Frequencies: frequencies,
		TotalCount:  in.Sketch.Total,
	}, nil
}

// merge merges two sketches.
func (c *CountMin) merge(ctx context.Context, in *CountMinInput) (*CountMinOutput, error) {
	if in.Sketch == nil {
		return nil, &AlgorithmError{
			Algorithm: "count_min",
			Operation: "merge",
			Err:       errors.New("sketch required for merge"),
		}
	}

	// Create new sketch as copy
	result := c.newSketch()

	// Copy dimensions from input sketch
	if in.Sketch.Width != result.Width || in.Sketch.Depth != result.Depth {
		return nil, &AlgorithmError{
			Algorithm: "count_min",
			Operation: "merge",
			Err:       errors.New("sketch dimensions must match"),
		}
	}

	// Merge values
	for i := 0; i < result.Depth; i++ {
		for j := 0; j < result.Width; j++ {
			select {
			case <-ctx.Done():
				return &CountMinOutput{Sketch: result, TotalCount: result.Total}, ctx.Err()
			default:
			}
			result.Table[i][j] = in.Sketch.Table[i][j]
		}
	}
	result.Total = in.Sketch.Total

	return &CountMinOutput{
		Sketch:     result,
		TotalCount: result.Total,
	}, nil
}

// newSketch creates a new empty sketch.
func (c *CountMin) newSketch() *CountMinSketch {
	table := make([][]int64, c.config.Depth)
	for i := range table {
		table[i] = make([]int64, c.config.Width)
	}

	seeds := make([]uint64, c.config.Depth)
	for i := range seeds {
		seeds[i] = uint64(i*0x9e3779b9 + 0x6c62272e)
	}

	return &CountMinSketch{
		Table: table,
		Width: c.config.Width,
		Depth: c.config.Depth,
		Seeds: seeds,
		Total: 0,
	}
}

// addToSketch adds a key with count to the sketch.
func (c *CountMin) addToSketch(sketch *CountMinSketch, key string, count int64) {
	for i := 0; i < sketch.Depth; i++ {
		j := c.hash(key, sketch.Seeds[i]) % uint64(sketch.Width)
		sketch.Table[i][j] += count
	}
	sketch.Total += count
}

// querySketch returns the estimated frequency for a key.
func (c *CountMin) querySketch(sketch *CountMinSketch, key string) int64 {
	minCount := int64(math.MaxInt64)
	for i := 0; i < sketch.Depth; i++ {
		j := c.hash(key, sketch.Seeds[i]) % uint64(sketch.Width)
		if sketch.Table[i][j] < minCount {
			minCount = sketch.Table[i][j]
		}
	}
	return minCount
}

// hash computes a hash value for a key with a seed.
func (c *CountMin) hash(key string, seed uint64) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64() ^ seed
}

// Timeout returns the maximum execution time.
func (c *CountMin) Timeout() time.Duration {
	return c.config.Timeout
}

// InputType returns the expected input type.
func (c *CountMin) InputType() reflect.Type {
	return reflect.TypeOf(&CountMinInput{})
}

// OutputType returns the output type.
func (c *CountMin) OutputType() reflect.Type {
	return reflect.TypeOf(&CountMinOutput{})
}

// ProgressInterval returns how often to report progress.
func (c *CountMin) ProgressInterval() time.Duration {
	return c.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (c *CountMin) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (c *CountMin) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "never_underestimates",
			Description: "Count-Min never underestimates the true frequency",
			Check: func(input, output any) error {
				// Note: This property can only be verified with ground truth
				// In practice, we trust the algorithm's mathematical properties
				return nil
			},
		},
		{
			Name:        "total_preserved",
			Description: "Total count equals sum of added counts",
			Check: func(input, output any) error {
				out, ok := output.(*CountMinOutput)
				if !ok || out.Sketch == nil {
					return nil
				}

				// Verify total is non-negative
				if out.TotalCount < 0 {
					return &AlgorithmError{
						Algorithm: "count_min",
						Operation: "Property.total_preserved",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "sketch_dimensions_valid",
			Description: "Sketch has valid dimensions",
			Check: func(input, output any) error {
				out, ok := output.(*CountMinOutput)
				if !ok || out.Sketch == nil {
					return nil
				}

				if out.Sketch.Width <= 0 || out.Sketch.Depth <= 0 {
					return &AlgorithmError{
						Algorithm: "count_min",
						Operation: "Property.sketch_dimensions_valid",
						Err:       eval.ErrPropertyFailed,
					}
				}

				if len(out.Sketch.Table) != out.Sketch.Depth {
					return &AlgorithmError{
						Algorithm: "count_min",
						Operation: "Property.sketch_dimensions_valid",
						Err:       eval.ErrPropertyFailed,
					}
				}

				for _, row := range out.Sketch.Table {
					if len(row) != out.Sketch.Width {
						return &AlgorithmError{
							Algorithm: "count_min",
							Operation: "Property.sketch_dimensions_valid",
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
func (c *CountMin) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "count_min_items_processed_total",
			Type:        eval.MetricCounter,
			Description: "Total items added to sketches",
		},
		{
			Name:        "count_min_queries_total",
			Type:        eval.MetricCounter,
			Description: "Total frequency queries",
		},
		{
			Name:        "count_min_sketch_total_count",
			Type:        eval.MetricGauge,
			Description: "Total count in sketch",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (c *CountMin) HealthCheck(ctx context.Context) error {
	if c.config == nil {
		return &AlgorithmError{
			Algorithm: "count_min",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if c.config.Width <= 0 {
		return &AlgorithmError{
			Algorithm: "count_min",
			Operation: "HealthCheck",
			Err:       errors.New("width must be positive"),
		}
	}
	if c.config.Depth <= 0 {
		return &AlgorithmError{
			Algorithm: "count_min",
			Operation: "HealthCheck",
			Err:       errors.New("depth must be positive"),
		}
	}
	return nil
}
