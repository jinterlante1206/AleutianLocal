// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/dag"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
)

// BuildGraphNode builds a code graph from parsed AST results.
//
// Description:
//
//	Takes parsed AST results and constructs a code graph with nodes
//	for symbols and edges for relationships (calls, imports, etc.).
//
// Inputs (from map[string]any):
//
//	"parse_results" ([]*ast.ParseResult): Results from PARSE_FILES node. Required.
//	"project_root" (string): Absolute path to project root. Required.
//
// Outputs:
//
//	*BuildGraphOutput containing:
//	  - Graph: The constructed code graph (frozen)
//	  - BuildResult: Detailed build statistics and errors
//	  - Duration: Build time
//
// Thread Safety:
//
//	Safe for concurrent use.
type BuildGraphNode struct {
	dag.BaseNode
	builder *graph.Builder
}

// BuildGraphOutput contains the result of building a code graph.
type BuildGraphOutput struct {
	// Graph is the constructed code graph.
	Graph *graph.Graph

	// BuildResult contains detailed build statistics.
	BuildResult *graph.BuildResult

	// Duration is the build time.
	Duration time.Duration
}

// NewBuildGraphNode creates a new build graph node.
//
// Inputs:
//
//	builder - The graph builder to use. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*BuildGraphNode - The configured node.
func NewBuildGraphNode(builder *graph.Builder, deps []string) *BuildGraphNode {
	return &BuildGraphNode{
		BaseNode: dag.BaseNode{
			NodeName:         "BUILD_GRAPH",
			NodeDependencies: deps,
			NodeTimeout:      3 * time.Minute,
			NodeRetryable:    false,
		},
		builder: builder,
	}
}

// Execute builds the code graph from parse results.
//
// Description:
//
//	Uses the graph.Builder to construct a code graph from the provided
//	AST parse results. The resulting graph is frozen and ready for queries.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "parse_results" and "project_root".
//
// Outputs:
//
//	*BuildGraphOutput - The build result.
//	error - Non-nil if building fails completely.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *BuildGraphNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.builder == nil {
		return nil, fmt.Errorf("%w: graph builder", ErrNilDependency)
	}

	// Extract inputs
	parseResults, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	if len(parseResults) == 0 {
		return nil, ErrNoFilesToProcess
	}

	start := time.Now()

	// Build graph
	buildResult, err := n.builder.Build(ctx, parseResults)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBuildFailed, err)
	}

	return &BuildGraphOutput{
		Graph:       buildResult.Graph,
		BuildResult: buildResult,
		Duration:    time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *BuildGraphNode) extractInputs(inputs map[string]any) ([]*ast.ParseResult, error) {
	// Try to get parse_results directly
	resultsRaw, ok := inputs["parse_results"]
	if !ok {
		// Try to get from PARSE_FILES output
		if parseOutput, ok := inputs["PARSE_FILES"]; ok {
			if output, ok := parseOutput.(*ParseFilesOutput); ok {
				return output.Results, nil
			}
		}
		return nil, fmt.Errorf("%w: parse_results", ErrMissingInput)
	}

	// Handle []*ast.ParseResult
	if results, ok := resultsRaw.([]*ast.ParseResult); ok {
		return results, nil
	}

	// Handle []any
	if resultsAny, ok := resultsRaw.([]any); ok {
		results := make([]*ast.ParseResult, 0, len(resultsAny))
		for i, r := range resultsAny {
			if pr, ok := r.(*ast.ParseResult); ok {
				results = append(results, pr)
			} else if pr != nil {
				return nil, fmt.Errorf("%w: parse_results[%d] is not *ast.ParseResult", ErrInvalidInputType, i)
			}
		}
		return results, nil
	}

	return nil, fmt.Errorf("%w: parse_results must be []*ast.ParseResult", ErrInvalidInputType)
}
