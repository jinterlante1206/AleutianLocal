// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// DataFlowTracer traces data flow through function calls.
//
// Thread Safety:
//
//	DataFlowTracer is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type DataFlowTracer struct {
	graph   *graph.Graph
	index   *index.SymbolIndex
	sources *SourceRegistry
	sinks   *SinkRegistry
}

// NewDataFlowTracer creates a new DataFlowTracer.
//
// Description:
//
//	Creates a tracer that can follow data flow through function calls,
//	identifying sources (where data enters), transforms (where data
//	is processed), and sinks (where data exits).
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*DataFlowTracer - The configured tracer.
//
// Example:
//
//	tracer := NewDataFlowTracer(graph, index)
//	flow, err := tracer.TraceDataFlow(ctx, "handlers.HandleRequest", opts...)
func NewDataFlowTracer(g *graph.Graph, idx *index.SymbolIndex) *DataFlowTracer {
	return &DataFlowTracer{
		graph:   g,
		index:   idx,
		sources: NewSourceRegistry(),
		sinks:   NewSinkRegistry(),
	}
}

// NewDataFlowTracerWithRegistries creates a DataFlowTracer with custom registries.
//
// Description:
//
//	Creates a tracer with custom source and sink registries. Useful for
//	specialized analysis or when sharing registries with trust flow analysis.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//	sources - Custom source registry.
//	sinks - Custom sink registry.
//
// Outputs:
//
//	*DataFlowTracer - The configured tracer.
func NewDataFlowTracerWithRegistries(
	g *graph.Graph,
	idx *index.SymbolIndex,
	sources *SourceRegistry,
	sinks *SinkRegistry,
) *DataFlowTracer {
	return &DataFlowTracer{
		graph:   g,
		index:   idx,
		sources: sources,
		sinks:   sinks,
	}
}

// TraceDataFlow traces data flow starting from a symbol.
//
// Description:
//
//	Performs BFS traversal from the starting symbol, following CALLS edges
//	to identify where data flows. Classifies each visited symbol as a
//	source, transform, or sink based on pattern matching.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID to start tracing from.
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	*DataFlow - The traced data flow with sources, transforms, and sinks.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Errors:
//
//	ErrSymbolNotFound - Symbol not found in the graph.
//	ErrContextCanceled - Context was canceled.
//	ErrGraphNotReady - Graph is not frozen.
//	ErrTraversalLimitReached - Max nodes limit was reached.
//
// Performance:
//
//	Target latency: < 500ms for typical call graphs.
//	Max nodes: 1000 (configurable via options).
//
// Limitations:
//
//   - Function-level precision only (not variable-level)
//   - Cannot track data through interface calls reliably
//   - May miss flows through reflection or dynamic dispatch
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) TraceDataFlow(ctx context.Context, symbolID string, opts ...ExploreOption) (*DataFlow, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	ctx, span := startTraceSpan(ctx, "TraceDataFlow", symbolID)
	defer span.End()
	start := time.Now()

	if err := ctx.Err(); err != nil {
		setTraceSpanResult(span, 0, 0, false)
		recordTraceMetrics(ctx, "trace_data_flow", time.Since(start), 0, 0, false)
		return nil, ErrContextCanceled
	}

	if !t.graph.IsFrozen() {
		setTraceSpanResult(span, 0, 0, false)
		recordTraceMetrics(ctx, "trace_data_flow", time.Since(start), 0, 0, false)
		return nil, ErrGraphNotReady
	}

	options := applyOptions(opts)
	if options.MaxNodes <= 0 {
		options.MaxNodes = 1000
	}

	// Verify starting node exists
	startNode, exists := t.graph.GetNode(symbolID)
	if !exists {
		return nil, ErrSymbolNotFound
	}

	flow := &DataFlow{
		Sources:    make([]DataPoint, 0),
		Transforms: make([]DataPoint, 0),
		Sinks:      make([]DataPoint, 0),
		Path:       make([]string, 0),
		Precision:  "function",
		Limitations: []string{
			"Function-level precision only; does not track variable assignments",
			"May miss flows through interface calls or dynamic dispatch",
			"Does not track data through closures or callbacks",
		},
	}

	// BFS traversal
	visited := make(map[string]bool)
	type queueItem struct {
		nodeID string
		depth  int
	}
	queue := []queueItem{{symbolID, 0}}
	visited[symbolID] = true
	nodesVisited := 0
	truncated := false

	// Check the starting node itself
	t.classifyNode(startNode, flow)
	flow.Path = append(flow.Path, symbolID)

	for len(queue) > 0 {
		// Check context periodically
		if nodesVisited%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, ErrContextCanceled
			}
		}

		// Check node limit
		if nodesVisited >= options.MaxNodes {
			truncated = true
			break
		}

		item := queue[0]
		queue = queue[1:]
		nodesVisited++

		// Check depth limit
		if item.depth >= options.MaxHops {
			continue
		}

		node, exists := t.graph.GetNode(item.nodeID)
		if !exists {
			continue
		}

		// Follow outgoing CALLS edges
		for _, edge := range node.Outgoing {
			if edge.Type != graph.EdgeTypeCalls {
				continue
			}

			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			targetNode, exists := t.graph.GetNode(edge.ToID)
			if !exists {
				continue
			}

			// Classify the target node
			t.classifyNode(targetNode, flow)
			flow.Path = append(flow.Path, edge.ToID)

			// Add to queue for further traversal
			queue = append(queue, queueItem{edge.ToID, item.depth + 1})
		}
	}

	if truncated {
		flow.Limitations = append(flow.Limitations,
			fmt.Sprintf("Traversal truncated at %d nodes", options.MaxNodes))
	}

	setTraceSpanResult(span, nodesVisited, len(flow.Sinks), true)
	recordTraceMetrics(ctx, "trace_data_flow", time.Since(start), nodesVisited, len(flow.Sinks), true)

	return flow, nil
}

// TraceDataFlowReverse traces data flow backwards from a sink.
//
// Description:
//
//	Performs reverse BFS traversal from the starting symbol, following
//	incoming CALLS edges to find where data came from. Useful for
//	understanding the provenance of data at a particular point.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID to start tracing from (typically a sink).
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	*DataFlow - The traced data flow with sources, transforms, and sinks.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) TraceDataFlowReverse(ctx context.Context, symbolID string, opts ...ExploreOption) (*DataFlow, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	if !t.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	options := applyOptions(opts)
	if options.MaxNodes <= 0 {
		options.MaxNodes = 1000
	}

	// Verify starting node exists
	startNode, exists := t.graph.GetNode(symbolID)
	if !exists {
		return nil, ErrSymbolNotFound
	}

	flow := &DataFlow{
		Sources:    make([]DataPoint, 0),
		Transforms: make([]DataPoint, 0),
		Sinks:      make([]DataPoint, 0),
		Path:       make([]string, 0),
		Precision:  "function",
		Limitations: []string{
			"Function-level precision only; does not track variable assignments",
			"May miss flows through interface calls or dynamic dispatch",
			"Reverse traversal only shows callers, not actual data origin",
		},
	}

	// BFS traversal (reverse direction)
	visited := make(map[string]bool)
	type queueItem struct {
		nodeID string
		depth  int
	}
	queue := []queueItem{{symbolID, 0}}
	visited[symbolID] = true
	nodesVisited := 0
	truncated := false

	// Check the starting node itself
	t.classifyNode(startNode, flow)
	flow.Path = append(flow.Path, symbolID)

	for len(queue) > 0 {
		// Check context periodically
		if nodesVisited%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, ErrContextCanceled
			}
		}

		// Check node limit
		if nodesVisited >= options.MaxNodes {
			truncated = true
			break
		}

		item := queue[0]
		queue = queue[1:]
		nodesVisited++

		// Check depth limit
		if item.depth >= options.MaxHops {
			continue
		}

		node, exists := t.graph.GetNode(item.nodeID)
		if !exists {
			continue
		}

		// Follow incoming CALLS edges (reverse direction)
		for _, edge := range node.Incoming {
			if edge.Type != graph.EdgeTypeCalls {
				continue
			}

			if visited[edge.FromID] {
				continue
			}
			visited[edge.FromID] = true

			sourceNode, exists := t.graph.GetNode(edge.FromID)
			if !exists {
				continue
			}

			// Classify the source node
			t.classifyNode(sourceNode, flow)
			flow.Path = append(flow.Path, edge.FromID)

			// Add to queue for further traversal
			queue = append(queue, queueItem{edge.FromID, item.depth + 1})
		}
	}

	if truncated {
		flow.Limitations = append(flow.Limitations,
			fmt.Sprintf("Traversal truncated at %d nodes", options.MaxNodes))
	}

	return flow, nil
}

// classifyNode determines if a node is a source, transform, or sink.
func (t *DataFlowTracer) classifyNode(node *graph.Node, flow *DataFlow) {
	if node == nil || node.Symbol == nil {
		return
	}

	sym := node.Symbol
	location := fmt.Sprintf("%s:%d", sym.FilePath, sym.StartLine)

	// Check if it's a source
	if sourcePattern, ok := t.sources.MatchSource(sym); ok {
		flow.Sources = append(flow.Sources, DataPoint{
			ID:         sym.ID,
			Type:       "source",
			Name:       sym.Name,
			Location:   location,
			Category:   string(sourcePattern.Category),
			Confidence: sourcePattern.Confidence,
		})
		return
	}

	// Check if it's a sink
	if sinkPattern, ok := t.sinks.MatchSink(sym); ok {
		flow.Sinks = append(flow.Sinks, DataPoint{
			ID:         sym.ID,
			Type:       "sink",
			Name:       sym.Name,
			Location:   location,
			Category:   string(sinkPattern.Category),
			Confidence: sinkPattern.Confidence,
		})
		return
	}

	// Otherwise, it's a transform
	flow.Transforms = append(flow.Transforms, DataPoint{
		ID:         sym.ID,
		Type:       "transform",
		Name:       sym.Name,
		Location:   location,
		Category:   "function",
		Confidence: 0.5, // Lower confidence for transforms
	})
}

// FindSourcesInFile finds all data sources in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies which ones are data sources.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]DataPoint - All data sources found in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) FindSourcesInFile(ctx context.Context, filePath string) ([]DataPoint, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	sources := make([]DataPoint, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return sources, ErrContextCanceled
		}

		if sourcePattern, ok := t.sources.MatchSource(sym); ok {
			sources = append(sources, DataPoint{
				ID:         sym.ID,
				Type:       "source",
				Name:       sym.Name,
				Location:   fmt.Sprintf("%s:%d", sym.FilePath, sym.StartLine),
				Category:   string(sourcePattern.Category),
				Confidence: sourcePattern.Confidence,
			})
		}
	}

	return sources, nil
}

// FindSinksInFile finds all data sinks in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies which ones are data sinks.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]DataPoint - All data sinks found in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) FindSinksInFile(ctx context.Context, filePath string) ([]DataPoint, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	sinks := make([]DataPoint, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return sinks, ErrContextCanceled
		}

		if sinkPattern, ok := t.sinks.MatchSink(sym); ok {
			sinks = append(sinks, DataPoint{
				ID:         sym.ID,
				Type:       "sink",
				Name:       sym.Name,
				Location:   fmt.Sprintf("%s:%d", sym.FilePath, sym.StartLine),
				Category:   string(sinkPattern.Category),
				Confidence: sinkPattern.Confidence,
			})
		}
	}

	return sinks, nil
}

// FindDangerousSinksInFile finds all dangerous sinks in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies which ones are dangerous
//	sinks (command execution, SQL injection, etc.). Useful for security
//	auditing.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]DataPoint - All dangerous sinks found in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) FindDangerousSinksInFile(ctx context.Context, filePath string) ([]DataPoint, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	dangerousSinks := make([]DataPoint, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return dangerousSinks, ErrContextCanceled
		}

		if sinkPattern, ok := t.sinks.MatchSink(sym); ok && sinkPattern.IsDangerous {
			dangerousSinks = append(dangerousSinks, DataPoint{
				ID:         sym.ID,
				Type:       "dangerous_sink",
				Name:       sym.Name,
				Location:   fmt.Sprintf("%s:%d", sym.FilePath, sym.StartLine),
				Category:   string(sinkPattern.Category),
				Confidence: sinkPattern.Confidence,
			})
		}
	}

	return dangerousSinks, nil
}

// TraceToDangerousSinks traces from a source to find paths to dangerous sinks.
//
// Description:
//
//	Performs BFS from the starting symbol, looking for paths that end at
//	dangerous sinks. This is useful for identifying potential security
//	vulnerabilities.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID to start tracing from (typically a source).
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	*DataFlow - The traced data flow, with only dangerous sinks included.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *DataFlowTracer) TraceToDangerousSinks(ctx context.Context, symbolID string, opts ...ExploreOption) (*DataFlow, error) {
	flow, err := t.TraceDataFlow(ctx, symbolID, opts...)
	if err != nil {
		return nil, err
	}

	// Filter to only include dangerous sinks
	dangerousSinks := make([]DataPoint, 0)
	for _, sink := range flow.Sinks {
		// Look up the symbol to check if the matched pattern is dangerous
		sym, found := t.index.GetByID(sink.ID)
		if found && sym != nil {
			if sinkPattern, ok := t.sinks.MatchSink(sym); ok && sinkPattern.IsDangerous {
				dangerousSinks = append(dangerousSinks, sink)
			}
		}
	}
	flow.Sinks = dangerousSinks

	return flow, nil
}
