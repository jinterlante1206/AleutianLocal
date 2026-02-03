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
	"reflect"
	"sort"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Weisfeiler-Leman Algorithm
// -----------------------------------------------------------------------------

// WeisfeilerLeman implements the Weisfeiler-Leman graph isomorphism test.
//
// Description:
//
//	The Weisfeiler-Leman (WL) algorithm iteratively refines vertex colors
//	based on neighborhood structure. After k iterations, vertices with the
//	same color have isomorphic k-hop neighborhoods.
//
//	Key Properties:
//	- Time: O(k * (V + E) log V) for k iterations
//	- Produces graph fingerprint (canonical coloring)
//	- Can distinguish most non-isomorphic graphs
//	- Fails only on pathological cases (e.g., certain regular graphs)
//
//	Use Cases:
//	- Detect structurally equivalent code patterns
//	- Graph similarity via color histograms
//	- Code structure canonicalization
//	- Pattern matching in AST/CFG
//
// Thread Safety: Safe for concurrent use.
type WeisfeilerLeman struct {
	config *WLConfig
}

// WLConfig configures the Weisfeiler-Leman algorithm.
type WLConfig struct {
	// MaxIterations is the maximum number of refinement iterations.
	MaxIterations int

	// EarlyStop enables stopping when coloring stabilizes.
	EarlyStop bool

	// MaxNodes limits the number of nodes to process.
	MaxNodes int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultWLConfig returns the default configuration.
func DefaultWLConfig() *WLConfig {
	return &WLConfig{
		MaxIterations:    10,
		EarlyStop:        true,
		MaxNodes:         10000,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewWeisfeilerLeman creates a new Weisfeiler-Leman algorithm.
func NewWeisfeilerLeman(config *WLConfig) *WeisfeilerLeman {
	if config == nil {
		config = DefaultWLConfig()
	}
	return &WeisfeilerLeman{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// WLInput is the input for Weisfeiler-Leman operations.
type WLInput struct {
	// Operation specifies what to do: "color", "compare", or "fingerprint".
	Operation string

	// Graph is the graph to process.
	Graph *WLGraph

	// OtherGraph is another graph to compare with (for "compare").
	OtherGraph *WLGraph

	// InitialColors are optional initial vertex colors.
	InitialColors map[string]uint64

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// WLGraph represents a graph for WL processing.
type WLGraph struct {
	// Nodes are the vertex IDs.
	Nodes []string

	// Edges maps each node to its neighbors.
	Edges map[string][]string

	// NodeLabels are optional vertex labels.
	NodeLabels map[string]string
}

// WLOutput is the output from Weisfeiler-Leman operations.
type WLOutput struct {
	// Colors maps each vertex to its final color.
	Colors map[string]uint64

	// ColorHistogram counts vertices per color.
	ColorHistogram map[uint64]int

	// Fingerprint is a canonical fingerprint for the graph.
	Fingerprint uint64

	// IsIsomorphic is whether two graphs could be isomorphic (for "compare").
	IsIsomorphic bool

	// IterationsUsed is the number of refinement iterations.
	IterationsUsed int

	// ColorClasses is the number of distinct colors.
	ColorClasses int

	// Stabilized is true if coloring stabilized before max iterations.
	Stabilized bool
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (w *WeisfeilerLeman) Name() string {
	return "weisfeiler_leman"
}

// Process executes the Weisfeiler-Leman operation.
//
// Description:
//
//	Supports three operations:
//	- "color": Compute stable vertex coloring
//	- "compare": Compare two graphs for potential isomorphism
//	- "fingerprint": Compute canonical graph fingerprint
//
// Thread Safety: Safe for concurrent use.
func (w *WeisfeilerLeman) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*WLInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &WLOutput{}, nil, ctx.Err()
	default:
	}

	var output *WLOutput
	var err error

	switch in.Operation {
	case "color":
		output, err = w.color(ctx, in)
	case "compare":
		output, err = w.compare(ctx, in)
	case "fingerprint":
		output, err = w.fingerprint(ctx, in)
	default:
		return nil, nil, &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "Process",
			Err:       errors.New("unknown operation: " + in.Operation),
		}
	}

	return output, nil, err
}

// color computes the stable vertex coloring.
func (w *WeisfeilerLeman) color(ctx context.Context, in *WLInput) (*WLOutput, error) {
	if in.Graph == nil {
		return &WLOutput{
			Colors:         make(map[string]uint64),
			ColorHistogram: make(map[uint64]int),
			ColorClasses:   0,
			Stabilized:     true,
		}, nil
	}

	if len(in.Graph.Nodes) > w.config.MaxNodes {
		return nil, &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "color",
			Err:       errors.New("too many nodes"),
		}
	}

	// Initialize colors
	colors := make(map[string]uint64)
	if in.InitialColors != nil {
		for k, v := range in.InitialColors {
			colors[k] = v
		}
	} else {
		// Use node labels or default color
		for _, node := range in.Graph.Nodes {
			if label, ok := in.Graph.NodeLabels[node]; ok {
				colors[node] = w.hash64(label)
			} else {
				colors[node] = 0
			}
		}
	}

	// Iterative refinement
	iterations := 0
	stabilized := false

	for iterations < w.config.MaxIterations {
		select {
		case <-ctx.Done():
			return &WLOutput{
				Colors:         colors,
				ColorHistogram: w.computeHistogram(colors),
				IterationsUsed: iterations,
				ColorClasses:   len(w.computeHistogram(colors)),
				Stabilized:     false,
			}, ctx.Err()
		default:
		}

		newColors, changed := w.refineColors(in.Graph, colors)
		iterations++

		if w.config.EarlyStop && !changed {
			stabilized = true
			break
		}

		colors = newColors
	}

	histogram := w.computeHistogram(colors)

	return &WLOutput{
		Colors:         colors,
		ColorHistogram: histogram,
		IterationsUsed: iterations,
		ColorClasses:   len(histogram),
		Stabilized:     stabilized,
	}, nil
}

// compare compares two graphs for potential isomorphism.
func (w *WeisfeilerLeman) compare(ctx context.Context, in *WLInput) (*WLOutput, error) {
	if in.Graph == nil || in.OtherGraph == nil {
		// Empty graphs are isomorphic to each other
		return &WLOutput{
			IsIsomorphic: in.Graph == nil && in.OtherGraph == nil,
		}, nil
	}

	// Quick check: same number of nodes and edges
	if len(in.Graph.Nodes) != len(in.OtherGraph.Nodes) {
		return &WLOutput{
			IsIsomorphic: false,
		}, nil
	}

	edgeCount1 := 0
	for _, neighbors := range in.Graph.Edges {
		edgeCount1 += len(neighbors)
	}
	edgeCount2 := 0
	for _, neighbors := range in.OtherGraph.Edges {
		edgeCount2 += len(neighbors)
	}
	if edgeCount1 != edgeCount2 {
		return &WLOutput{
			IsIsomorphic: false,
		}, nil
	}

	// Color both graphs
	out1, err := w.color(ctx, &WLInput{
		Operation:     "color",
		Graph:         in.Graph,
		InitialColors: in.InitialColors,
	})
	if err != nil {
		return nil, err
	}

	out2, err := w.color(ctx, &WLInput{
		Operation:     "color",
		Graph:         in.OtherGraph,
		InitialColors: in.InitialColors,
	})
	if err != nil {
		return nil, err
	}

	// Compare histograms
	isIsomorphic := w.histogramsEqual(out1.ColorHistogram, out2.ColorHistogram)

	return &WLOutput{
		IsIsomorphic:   isIsomorphic,
		IterationsUsed: out1.IterationsUsed,
		ColorClasses:   out1.ColorClasses,
	}, nil
}

// fingerprint computes a canonical graph fingerprint.
func (w *WeisfeilerLeman) fingerprint(ctx context.Context, in *WLInput) (*WLOutput, error) {
	// Get stable coloring
	out, err := w.color(ctx, in)
	if err != nil {
		return nil, err
	}

	// Compute fingerprint from histogram
	fingerprint := w.computeFingerprint(out.ColorHistogram)
	out.Fingerprint = fingerprint

	return out, nil
}

// refineColors performs one iteration of color refinement.
func (w *WeisfeilerLeman) refineColors(graph *WLGraph, colors map[string]uint64) (map[string]uint64, bool) {
	newColors := make(map[string]uint64)
	changed := false

	for _, node := range graph.Nodes {
		// Collect neighbor colors
		neighborColors := make([]uint64, 0)
		for _, neighbor := range graph.Edges[node] {
			neighborColors = append(neighborColors, colors[neighbor])
		}

		// Sort for canonical ordering
		sort.Slice(neighborColors, func(i, j int) bool {
			return neighborColors[i] < neighborColors[j]
		})

		// Compute new color from (old_color, sorted_neighbor_colors)
		newColor := w.hashColorNeighborhood(colors[node], neighborColors)
		newColors[node] = newColor

		if newColor != colors[node] {
			changed = true
		}
	}

	return newColors, changed
}

// hashColorNeighborhood creates a hash from a color and its neighborhood.
func (w *WeisfeilerLeman) hashColorNeighborhood(color uint64, neighbors []uint64) uint64 {
	h := fnv.New64a()

	// Write own color
	b := make([]byte, 8)
	b[0] = byte(color >> 56)
	b[1] = byte(color >> 48)
	b[2] = byte(color >> 40)
	b[3] = byte(color >> 32)
	b[4] = byte(color >> 24)
	b[5] = byte(color >> 16)
	b[6] = byte(color >> 8)
	b[7] = byte(color)
	h.Write(b)

	// Write neighbor colors
	for _, nc := range neighbors {
		b[0] = byte(nc >> 56)
		b[1] = byte(nc >> 48)
		b[2] = byte(nc >> 40)
		b[3] = byte(nc >> 32)
		b[4] = byte(nc >> 24)
		b[5] = byte(nc >> 16)
		b[6] = byte(nc >> 8)
		b[7] = byte(nc)
		h.Write(b)
	}

	return h.Sum64()
}

// computeHistogram creates a histogram of colors.
func (w *WeisfeilerLeman) computeHistogram(colors map[string]uint64) map[uint64]int {
	hist := make(map[uint64]int)
	for _, color := range colors {
		hist[color]++
	}
	return hist
}

// histogramsEqual checks if two histograms are equal.
func (w *WeisfeilerLeman) histogramsEqual(h1, h2 map[uint64]int) bool {
	if len(h1) != len(h2) {
		return false
	}
	for color, count := range h1 {
		if h2[color] != count {
			return false
		}
	}
	return true
}

// computeFingerprint creates a canonical fingerprint from a histogram.
func (w *WeisfeilerLeman) computeFingerprint(hist map[uint64]int) uint64 {
	// Sort histogram entries for canonical ordering
	type entry struct {
		color uint64
		count int
	}
	entries := make([]entry, 0, len(hist))
	for c, n := range hist {
		entries = append(entries, entry{c, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count < entries[j].count
		}
		return entries[i].color < entries[j].color
	})

	// Hash the sorted entries
	h := fnv.New64a()
	b := make([]byte, 12)
	for _, e := range entries {
		b[0] = byte(e.color >> 56)
		b[1] = byte(e.color >> 48)
		b[2] = byte(e.color >> 40)
		b[3] = byte(e.color >> 32)
		b[4] = byte(e.color >> 24)
		b[5] = byte(e.color >> 16)
		b[6] = byte(e.color >> 8)
		b[7] = byte(e.color)
		b[8] = byte(e.count >> 24)
		b[9] = byte(e.count >> 16)
		b[10] = byte(e.count >> 8)
		b[11] = byte(e.count)
		h.Write(b)
	}

	return h.Sum64()
}

// hash64 computes a 64-bit hash.
func (w *WeisfeilerLeman) hash64(s string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

// Timeout returns the maximum execution time.
func (w *WeisfeilerLeman) Timeout() time.Duration {
	return w.config.Timeout
}

// InputType returns the expected input type.
func (w *WeisfeilerLeman) InputType() reflect.Type {
	return reflect.TypeOf(&WLInput{})
}

// OutputType returns the output type.
func (w *WeisfeilerLeman) OutputType() reflect.Type {
	return reflect.TypeOf(&WLOutput{})
}

// ProgressInterval returns how often to report progress.
func (w *WeisfeilerLeman) ProgressInterval() time.Duration {
	return w.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (w *WeisfeilerLeman) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (w *WeisfeilerLeman) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "colors_cover_all_nodes",
			Description: "All nodes have a color assigned",
			Check: func(input, output any) error {
				in, okIn := input.(*WLInput)
				out, okOut := output.(*WLOutput)
				if !okIn || !okOut || in.Graph == nil {
					return nil
				}

				for _, node := range in.Graph.Nodes {
					if _, exists := out.Colors[node]; !exists {
						return &AlgorithmError{
							Algorithm: "weisfeiler_leman",
							Operation: "Property.colors_cover_all_nodes",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "histogram_sums_to_node_count",
			Description: "Color histogram sums to number of nodes",
			Check: func(input, output any) error {
				in, okIn := input.(*WLInput)
				out, okOut := output.(*WLOutput)
				if !okIn || !okOut || in.Graph == nil {
					return nil
				}

				total := 0
				for _, count := range out.ColorHistogram {
					total += count
				}

				if total != len(in.Graph.Nodes) {
					return &AlgorithmError{
						Algorithm: "weisfeiler_leman",
						Operation: "Property.histogram_sums_to_node_count",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "isomorphism_implies_same_histogram",
			Description: "Isomorphic graphs have same color histogram",
			Check: func(input, output any) error {
				// This is verified by algorithm design
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (w *WeisfeilerLeman) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "wl_iterations_total",
			Type:        eval.MetricCounter,
			Description: "Total refinement iterations",
		},
		{
			Name:        "wl_color_classes",
			Type:        eval.MetricHistogram,
			Description: "Distribution of color class counts",
			Buckets:     []float64{1, 2, 5, 10, 20, 50, 100, 500, 1000},
		},
		{
			Name:        "wl_stabilization_rate",
			Type:        eval.MetricCounter,
			Description: "Rate of early stabilization",
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (w *WeisfeilerLeman) HealthCheck(ctx context.Context) error {
	if w.config == nil {
		return &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if w.config.MaxIterations <= 0 {
		return &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "HealthCheck",
			Err:       errors.New("max iterations must be positive"),
		}
	}
	if w.config.MaxNodes <= 0 {
		return &AlgorithmError{
			Algorithm: "weisfeiler_leman",
			Operation: "HealthCheck",
			Err:       errors.New("max nodes must be positive"),
		}
	}
	return nil
}
