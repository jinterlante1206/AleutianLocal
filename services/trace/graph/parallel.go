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
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Parallel BFS configuration constants.
const (
	// parallelThreshold is the minimum level size to trigger parallel processing.
	// Levels with fewer nodes use sequential processing for better cache locality.
	parallelThreshold = 32

	// maxParallelWorkers caps the number of goroutines regardless of CPU count.
	// Memory-bound graph traversal doesn't benefit from excessive parallelism.
	maxParallelWorkers = 8
)

var parallelTracer = otel.Tracer("graph.parallel")

// GetCallGraphParallel returns the call tree using parallel BFS for wide graphs.
//
// Description:
//
//	Performs level-synchronous parallel BFS traversal. Automatically chooses
//	parallel or sequential mode based on level width (threshold: 32 nodes).
//	For narrow graphs, falls back to sequential for better cache locality.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: Root function ID. Must exist in graph.
//   - opts: Query options (MaxDepth, Limit).
//
// Outputs:
//   - *TraversalResult: Visited nodes (order may vary in parallel mode) and edges.
//   - error: Non-nil if root not found.
//
// Performance:
//
//	| Graph Type    | Complexity                    |
//	|---------------|-------------------------------|
//	| Sequential    | O(V + E)                      |
//	| Parallel wide | O((V + E) / P + D * barrier)  |
//
//	where P = min(level_size, NumCPU, 8), D = depth
//
// Limitations:
//   - VisitedNodes order is non-deterministic when parallel mode is used
//   - Parallel mode only engaged for levels with >32 nodes
//   - Memory overhead: ~O(level_size) per level for work distribution
//
// Thread Safety: Safe for concurrent use.
func (g *Graph) GetCallGraphParallel(ctx context.Context, symbolID string, opts ...QueryOption) (*TraversalResult, error) {
	ctx, span := parallelTracer.Start(ctx, "graph.GetCallGraphParallel",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	options := applyOptions(opts)
	span.SetAttributes(
		attribute.Int("max_depth", options.MaxDepth),
		attribute.Int("limit", options.Limit),
	)

	root, ok := g.nodes[symbolID]
	if !ok {
		err := fmt.Errorf("root node not found: %s", symbolID)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	result := &TraversalResult{
		StartNode:    symbolID,
		VisitedNodes: make([]string, 0),
		Edges:        make([]*Edge, 0),
	}

	visited := make(map[string]bool)
	var mu sync.RWMutex

	currentLevel := []*Node{root}
	visited[symbolID] = true

	parallelLevels := 0
	sequentialLevels := 0

	for depth := 0; len(currentLevel) > 0 && depth < options.MaxDepth; depth++ {
		if err := ctx.Err(); err != nil {
			result.Truncated = true
			span.SetAttributes(attribute.Bool("context_cancelled", true))
			break
		}

		// Collect current level nodes, respecting limit
		for _, node := range currentLevel {
			result.VisitedNodes = append(result.VisitedNodes, node.ID)
			if len(result.VisitedNodes) >= options.Limit {
				result.Truncated = true
				span.SetAttributes(attribute.Bool("limit_reached", true))
				break
			}
		}

		if result.Truncated {
			break
		}

		// Choose parallel or sequential based on level size
		var nextLevel []*Node
		levelSize := len(currentLevel)
		if levelSize > parallelThreshold {
			slog.Debug("using parallel mode for BFS level",
				slog.Int("depth", depth),
				slog.Int("level_size", levelSize),
				slog.Int("threshold", parallelThreshold),
			)
			nextLevel = g.processLevelParallel(ctx, currentLevel, visited, &mu, result)
			parallelLevels++
		} else {
			nextLevel = g.processLevelSequential(currentLevel, visited, result)
			sequentialLevels++
		}

		currentLevel = nextLevel
		result.Depth = depth + 1
	}

	span.SetAttributes(
		attribute.Int("total_nodes", len(result.VisitedNodes)),
		attribute.Int("total_edges", len(result.Edges)),
		attribute.Int("depth", result.Depth),
		attribute.Int("parallel_levels", parallelLevels),
		attribute.Int("sequential_levels", sequentialLevels),
		attribute.Bool("truncated", result.Truncated),
	)
	span.SetStatus(codes.Ok, "")

	slog.Debug("parallel BFS completed",
		slog.String("symbol_id", symbolID),
		slog.Int("total_nodes", len(result.VisitedNodes)),
		slog.Int("parallel_levels", parallelLevels),
		slog.Int("sequential_levels", sequentialLevels),
	)

	return result, nil
}

// GetReverseCallGraphParallel returns the callers tree using parallel BFS.
//
// Description:
//
//	Performs level-synchronous parallel BFS traversal following CALLS edges
//	backwards (finding callers). Automatically chooses parallel or sequential
//	mode based on level width.
//
// Inputs:
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolID: Root function ID. Must exist in graph.
//   - opts: Query options (MaxDepth, Limit).
//
// Outputs:
//   - *TraversalResult: Caller nodes (order may vary in parallel mode) and edges.
//   - error: Non-nil if root not found.
//
// Limitations:
//   - VisitedNodes order is non-deterministic when parallel mode is used
//   - Parallel mode only engaged for levels with >32 nodes
//
// Thread Safety: Safe for concurrent use.
func (g *Graph) GetReverseCallGraphParallel(ctx context.Context, symbolID string, opts ...QueryOption) (*TraversalResult, error) {
	ctx, span := parallelTracer.Start(ctx, "graph.GetReverseCallGraphParallel",
		trace.WithAttributes(
			attribute.String("symbol_id", symbolID),
		),
	)
	defer span.End()

	options := applyOptions(opts)
	span.SetAttributes(
		attribute.Int("max_depth", options.MaxDepth),
		attribute.Int("limit", options.Limit),
	)

	root, ok := g.nodes[symbolID]
	if !ok {
		err := fmt.Errorf("root node not found: %s", symbolID)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	result := &TraversalResult{
		StartNode:    symbolID,
		VisitedNodes: make([]string, 0),
		Edges:        make([]*Edge, 0),
	}

	visited := make(map[string]bool)
	var mu sync.RWMutex

	currentLevel := []*Node{root}
	visited[symbolID] = true

	parallelLevels := 0
	sequentialLevels := 0

	for depth := 0; len(currentLevel) > 0 && depth < options.MaxDepth; depth++ {
		if err := ctx.Err(); err != nil {
			result.Truncated = true
			span.SetAttributes(attribute.Bool("context_cancelled", true))
			break
		}

		// Collect current level nodes, respecting limit
		for _, node := range currentLevel {
			result.VisitedNodes = append(result.VisitedNodes, node.ID)
			if len(result.VisitedNodes) >= options.Limit {
				result.Truncated = true
				span.SetAttributes(attribute.Bool("limit_reached", true))
				break
			}
		}

		if result.Truncated {
			break
		}

		// Choose parallel or sequential based on level size
		var nextLevel []*Node
		levelSize := len(currentLevel)
		if levelSize > parallelThreshold {
			slog.Debug("using parallel mode for reverse BFS level",
				slog.Int("depth", depth),
				slog.Int("level_size", levelSize),
				slog.Int("threshold", parallelThreshold),
			)
			nextLevel = g.processReverseLevelParallel(ctx, currentLevel, visited, &mu, result)
			parallelLevels++
		} else {
			nextLevel = g.processReverseLevelSequential(currentLevel, visited, result)
			sequentialLevels++
		}

		currentLevel = nextLevel
		result.Depth = depth + 1
	}

	span.SetAttributes(
		attribute.Int("total_nodes", len(result.VisitedNodes)),
		attribute.Int("total_edges", len(result.Edges)),
		attribute.Int("depth", result.Depth),
		attribute.Int("parallel_levels", parallelLevels),
		attribute.Int("sequential_levels", sequentialLevels),
		attribute.Bool("truncated", result.Truncated),
	)
	span.SetStatus(codes.Ok, "")

	slog.Debug("parallel reverse BFS completed",
		slog.String("symbol_id", symbolID),
		slog.Int("total_nodes", len(result.VisitedNodes)),
		slog.Int("parallel_levels", parallelLevels),
		slog.Int("sequential_levels", sequentialLevels),
	)

	return result, nil
}

// processLevelSequential processes a BFS level sequentially (forward direction).
// Used when level size is at or below parallelThreshold for better cache locality.
//
// Thread Safety: NOT safe for concurrent use - caller must synchronize access to
// visited map and result.
// Used when level size is below parallelThreshold for better cache locality.
func (g *Graph) processLevelSequential(level []*Node, visited map[string]bool, result *TraversalResult) []*Node {
	var nextLevel []*Node

	for _, node := range level {
		for _, edge := range node.Outgoing {
			if edge.Type != EdgeTypeCalls {
				continue
			}
			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			if nextNode, ok := g.nodes[edge.ToID]; ok {
				nextLevel = append(nextLevel, nextNode)
				result.Edges = append(result.Edges, edge)
			}
		}
	}

	return nextLevel
}

// processLevelParallel processes a BFS level in parallel using a worker pool.
//
// Uses per-worker local slices to minimize lock contention. Each worker
// maintains its own slice of discovered nodes and edges, which are merged
// after all workers complete. The visited map uses RWMutex with double-check
// locking pattern for thread-safe access.
//
// Thread Safety: Safe for concurrent use with proper synchronization via mu.
func (g *Graph) processLevelParallel(ctx context.Context, level []*Node, visited map[string]bool, mu *sync.RWMutex, result *TraversalResult) []*Node {
	workers := min(len(level), min(runtime.NumCPU(), maxParallelWorkers))

	// Per-worker local results to avoid lock contention
	type localResult struct {
		nodes []*Node
		edges []*Edge
	}
	localResults := make([]localResult, workers)

	// Buffered work channel
	workChan := make(chan *Node, min(len(level), 256))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Panic recovery to prevent crashes - log and continue
			defer func() {
				if r := recover(); r != nil {
					// Get stack trace for debugging
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					slog.Error("panic in parallel BFS worker",
						slog.Int("worker_id", workerID),
						slog.Any("panic", r),
						slog.String("stack", string(buf[:n])),
					)
				}
			}()

			local := &localResults[workerID]
			local.nodes = make([]*Node, 0, len(level)/workers+1)
			local.edges = make([]*Edge, 0, len(level)/workers+1)

			for node := range workChan {
				// Check context periodically
				if ctx.Err() != nil {
					return
				}

				for _, edge := range node.Outgoing {
					if edge.Type != EdgeTypeCalls {
						continue
					}

					// Read-lock first to check, then write-lock to add
					mu.RLock()
					alreadyVisited := visited[edge.ToID]
					mu.RUnlock()

					if alreadyVisited {
						continue
					}

					// Double-check under write lock
					mu.Lock()
					if visited[edge.ToID] {
						mu.Unlock()
						continue
					}
					visited[edge.ToID] = true
					mu.Unlock()

					if nextNode, ok := g.nodes[edge.ToID]; ok {
						local.nodes = append(local.nodes, nextNode)
						local.edges = append(local.edges, edge)
					}
				}
			}
		}(i)
	}

	// Feed work to channel
	for _, node := range level {
		workChan <- node
	}
	close(workChan)
	wg.Wait()

	// Merge results from all workers
	var nextLevel []*Node
	for _, local := range localResults {
		nextLevel = append(nextLevel, local.nodes...)
		result.Edges = append(result.Edges, local.edges...)
	}

	return nextLevel
}

// processReverseLevelSequential processes a BFS level sequentially (reverse direction).
// Follows Incoming CALLS edges to find callers.
//
// Thread Safety: NOT safe for concurrent use - caller must synchronize access to
// visited map and result.
func (g *Graph) processReverseLevelSequential(level []*Node, visited map[string]bool, result *TraversalResult) []*Node {
	var nextLevel []*Node

	for _, node := range level {
		for _, edge := range node.Incoming {
			if edge.Type != EdgeTypeCalls {
				continue
			}
			if visited[edge.FromID] {
				continue
			}
			visited[edge.FromID] = true

			if nextNode, ok := g.nodes[edge.FromID]; ok {
				nextLevel = append(nextLevel, nextNode)
				result.Edges = append(result.Edges, edge)
			}
		}
	}

	return nextLevel
}

// processReverseLevelParallel processes a reverse BFS level in parallel.
//
// Uses per-worker local slices to minimize lock contention. Each worker
// maintains its own slice of discovered callers and edges, which are merged
// after all workers complete.
//
// Thread Safety: Safe for concurrent use with proper synchronization via mu.
func (g *Graph) processReverseLevelParallel(ctx context.Context, level []*Node, visited map[string]bool, mu *sync.RWMutex, result *TraversalResult) []*Node {
	workers := min(len(level), min(runtime.NumCPU(), maxParallelWorkers))

	type localResult struct {
		nodes []*Node
		edges []*Edge
	}
	localResults := make([]localResult, workers)

	workChan := make(chan *Node, min(len(level), 256))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// Panic recovery to prevent crashes - log and continue
			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					slog.Error("panic in parallel reverse BFS worker",
						slog.Int("worker_id", workerID),
						slog.Any("panic", r),
						slog.String("stack", string(buf[:n])),
					)
				}
			}()

			local := &localResults[workerID]
			local.nodes = make([]*Node, 0, len(level)/workers+1)
			local.edges = make([]*Edge, 0, len(level)/workers+1)

			for node := range workChan {
				if ctx.Err() != nil {
					return
				}

				for _, edge := range node.Incoming {
					if edge.Type != EdgeTypeCalls {
						continue
					}

					mu.RLock()
					alreadyVisited := visited[edge.FromID]
					mu.RUnlock()

					if alreadyVisited {
						continue
					}

					mu.Lock()
					if visited[edge.FromID] {
						mu.Unlock()
						continue
					}
					visited[edge.FromID] = true
					mu.Unlock()

					if nextNode, ok := g.nodes[edge.FromID]; ok {
						local.nodes = append(local.nodes, nextNode)
						local.edges = append(local.edges, edge)
					}
				}
			}
		}(i)
	}

	for _, node := range level {
		workChan <- node
	}
	close(workChan)
	wg.Wait()

	var nextLevel []*Node
	for _, local := range localResults {
		nextLevel = append(nextLevel, local.nodes...)
		result.Edges = append(result.Edges, local.edges...)
	}

	return nextLevel
}
