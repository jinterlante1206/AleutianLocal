// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// graphQueryTracer is the OTel tracer for graph query tools.
var graphQueryTracer = otel.Tracer("trace.tools.graph_query")

// =============================================================================
// find_callers Tool (P0 - Critical for test 17)
// =============================================================================

// findCallersTool wraps graph.FindCallersByName.
//
// Description:
//
//	Finds all functions that call a given function by name. This is essential
//	for understanding code dependencies and answering questions like
//	"Find all functions that call parseConfig".
//
// GR-01 Optimization:
//
//	Uses O(1) SymbolIndex lookup before falling back to O(V) graph scan.
//	When multiple symbols share the same name (e.g., "Setup" in different
//	packages), the limit parameter applies per symbol, not as a global ceiling.
//	Total results may be up to limit × number_of_matching_symbols.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findCallersTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindCallersTool creates the find_callers tool.
//
// Inputs:
//
//	g - The code graph (must not be nil)
//	idx - The symbol index (must not be nil)
//
// Outputs:
//
//	Tool - The find_callers tool implementation
func NewFindCallersTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findCallersTool{
		graph: g,
		index: idx,
	}
}

func (t *findCallersTool) Name() string {
	return "find_callers"
}

func (t *findCallersTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findCallersTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_callers",
		Description: "Find all functions that call a given function by name. " +
			"Essential for understanding code dependencies and impact analysis. " +
			"Use this instead of Grep when you need to find callers of a function. " +
			"Returns the list of callers with file locations and signatures.",
		Parameters: map[string]ParamDef{
			"function_name": {
				Type:        ParamTypeString,
				Description: "Name of the function to find callers for (e.g., 'parseConfig', 'HandleRequest')",
				Required:    true,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of callers to return",
				Required:    false,
				Default:     50,
			},
		},
		Category:    CategoryExploration,
		Priority:    95, // High priority - direct answer to common questions
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findCallersTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// M4: Validate and extract parameters before starting span
	name, _ := params["function_name"].(string)
	if name == "" {
		return &Result{Success: false, Error: "function_name is required"}, nil
	}

	// M1: Cap limit at 1000 to prevent resource exhaustion
	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		if l > 1000 {
			l = 1000
		}
		limit = l
	}

	// M4: Start span with all context upfront
	ctx, span := graphQueryTracer.Start(ctx, "find_callers.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_callers"),
			attribute.String("function_name", name),
			attribute.Int("limit", limit),
			attribute.Bool("index_available", t.index != nil),
		),
	)
	defer span.End()

	// GR-01: Use index first for O(1) lookup instead of O(V) graph scan
	var results map[string]*graph.QueryResult
	var err error
	var queryErrors int // C2: Track query errors

	if t.index != nil {
		// O(1) index lookup
		symbols := t.index.GetByName(name)
		span.SetAttributes(
			attribute.Bool("index_used", true),
			attribute.Int("index_matches", len(symbols)),
		)

		if len(symbols) > 0 {
			results = make(map[string]*graph.QueryResult, len(symbols))
			for _, sym := range symbols {
				// C1: Nil symbol check
				if sym == nil {
					continue
				}
				if err := ctx.Err(); err != nil {
					span.RecordError(err)
					return nil, err
				}
				result, qErr := t.graph.FindCallersByID(ctx, sym.ID, graph.WithLimit(limit))
				if qErr != nil {
					// C2: Track errors instead of silent continue
					queryErrors++
					slog.Warn("graph query failed",
						slog.String("tool", "find_callers"),
						slog.String("operation", "FindCallersByID"),
						slog.String("symbol_id", sym.ID),
						slog.String("error", qErr.Error()),
					)
					continue
				}
				results[sym.ID] = result
			}
			// C2: Record query errors in span
			if queryErrors > 0 {
				span.SetAttributes(attribute.Int("query_errors", queryErrors))
			}
		} else {
			// No matches in index - return empty result (O(1) fail-fast)
			span.SetAttributes(attribute.Bool("fast_not_found", true))
			results = make(map[string]*graph.QueryResult)
		}
	} else {
		// Fallback to O(V) graph scan (index unavailable)
		slog.Warn("graph query fallback",
			slog.String("tool", "find_callers"),
			slog.String("reason", "index_unavailable"),
			slog.String("function_name", name),
		)
		span.SetAttributes(attribute.Bool("index_used", false))
		results, err = t.graph.FindCallersByName(ctx, name, graph.WithLimit(limit))
		if err != nil {
			span.RecordError(err)
			// M2: Improved error wrapping
			return &Result{Success: false, Error: fmt.Sprintf("find callers for '%s': %v", name, err)}, nil
		}
	}

	// Format results
	output := formatCallerResults(name, results)
	outputText := formatCallerResultsText(name, results)

	span.SetAttributes(
		attribute.Int("match_count", len(results)),
		attribute.Int("total_callers", countTotalCallers(results)),
	)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// =============================================================================
// find_callees Tool (P0)
// =============================================================================

// findCalleesTool wraps graph.FindCalleesByName.
//
// Description:
//
//	Finds all functions called by a given function by name. Essential for
//	understanding dependencies and data flow.
//
// GR-01 Optimization:
//
//	Uses O(1) SymbolIndex lookup before falling back to O(V) graph scan.
//	When multiple symbols share the same name, the limit parameter applies
//	per symbol, not as a global ceiling.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findCalleesTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindCalleesTool creates the find_callees tool.
func NewFindCalleesTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findCalleesTool{
		graph: g,
		index: idx,
	}
}

func (t *findCalleesTool) Name() string {
	return "find_callees"
}

func (t *findCalleesTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findCalleesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_callees",
		Description: "Find all functions called by a given function. " +
			"Essential for understanding dependencies and data flow. " +
			"Use this to understand what a function depends on.",
		Parameters: map[string]ParamDef{
			"function_name": {
				Type:        ParamTypeString,
				Description: "Name of the function to find callees for",
				Required:    true,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of callees to return",
				Required:    false,
				Default:     50,
			},
		},
		Category:    CategoryExploration,
		Priority:    94,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findCalleesTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// M4: Validate and extract parameters before starting span
	name, _ := params["function_name"].(string)
	if name == "" {
		return &Result{Success: false, Error: "function_name is required"}, nil
	}

	// M1: Cap limit at 1000 to prevent resource exhaustion
	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		if l > 1000 {
			l = 1000
		}
		limit = l
	}

	// M4: Start span with all context upfront
	ctx, span := graphQueryTracer.Start(ctx, "find_callees.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_callees"),
			attribute.String("function_name", name),
			attribute.Int("limit", limit),
			attribute.Bool("index_available", t.index != nil),
		),
	)
	defer span.End()

	// GR-01: Use index first for O(1) lookup instead of O(V) graph scan
	var results map[string]*graph.QueryResult
	var err error
	var queryErrors int // C2: Track query errors

	if t.index != nil {
		// O(1) index lookup
		symbols := t.index.GetByName(name)
		span.SetAttributes(
			attribute.Bool("index_used", true),
			attribute.Int("index_matches", len(symbols)),
		)

		if len(symbols) > 0 {
			results = make(map[string]*graph.QueryResult, len(symbols))
			for _, sym := range symbols {
				// C1: Nil symbol check
				if sym == nil {
					continue
				}
				if err := ctx.Err(); err != nil {
					span.RecordError(err)
					return nil, err
				}
				result, qErr := t.graph.FindCalleesByID(ctx, sym.ID, graph.WithLimit(limit))
				if qErr != nil {
					// C2: Track errors instead of silent continue
					queryErrors++
					slog.Warn("graph query failed",
						slog.String("tool", "find_callees"),
						slog.String("operation", "FindCalleesByID"),
						slog.String("symbol_id", sym.ID),
						slog.String("error", qErr.Error()),
					)
					continue
				}
				results[sym.ID] = result
			}
			// C2: Record query errors in span
			if queryErrors > 0 {
				span.SetAttributes(attribute.Int("query_errors", queryErrors))
			}
		} else {
			// No matches in index - return empty result (O(1) fail-fast)
			span.SetAttributes(attribute.Bool("fast_not_found", true))
			results = make(map[string]*graph.QueryResult)
		}
	} else {
		// Fallback to O(V) graph scan (index unavailable)
		slog.Warn("graph query fallback",
			slog.String("tool", "find_callees"),
			slog.String("reason", "index_unavailable"),
			slog.String("function_name", name),
		)
		span.SetAttributes(attribute.Bool("index_used", false))
		results, err = t.graph.FindCalleesByName(ctx, name, graph.WithLimit(limit))
		if err != nil {
			span.RecordError(err)
			// M2: Improved error wrapping
			return &Result{Success: false, Error: fmt.Sprintf("find callees for '%s': %v", name, err)}, nil
		}
	}

	output := formatCalleeResults(name, results)
	outputText := formatCalleeResultsText(name, results)

	span.SetAttributes(
		attribute.Int("match_count", len(results)),
		attribute.Int("total_callees", countTotalCallers(results)),
	)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// =============================================================================
// find_implementations Tool (P0)
// =============================================================================

// findImplementationsTool wraps graph.FindImplementationsByName.
//
// Description:
//
//	Finds all types that implement a given interface by name. Essential for
//	understanding polymorphism and interface usage.
//
// GR-01 Optimization:
//
//	Uses O(1) SymbolIndex lookup before falling back to O(V) graph scan.
//	Only symbols with Kind=SymbolKindInterface are queried; other matching
//	names are filtered out with debug logging.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findImplementationsTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindImplementationsTool creates the find_implementations tool.
func NewFindImplementationsTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findImplementationsTool{
		graph: g,
		index: idx,
	}
}

func (t *findImplementationsTool) Name() string {
	return "find_implementations"
}

func (t *findImplementationsTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findImplementationsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_implementations",
		Description: "Find all types that implement a given interface. " +
			"Essential for understanding polymorphism and interface usage. " +
			"Use this to find concrete implementations of an interface.",
		Parameters: map[string]ParamDef{
			"interface_name": {
				Type:        ParamTypeString,
				Description: "Name of the interface to find implementations for (e.g., 'Reader', 'Handler')",
				Required:    true,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of implementations to return",
				Required:    false,
				Default:     50,
			},
		},
		Category:    CategoryExploration,
		Priority:    93,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findImplementationsTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// M4: Validate and extract parameters before starting span
	name, _ := params["interface_name"].(string)
	if name == "" {
		return &Result{Success: false, Error: "interface_name is required"}, nil
	}

	// M1: Cap limit at 1000 to prevent resource exhaustion
	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		if l > 1000 {
			l = 1000
		}
		limit = l
	}

	// M4: Start span with all context upfront
	ctx, span := graphQueryTracer.Start(ctx, "find_implementations.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_implementations"),
			attribute.String("interface_name", name),
			attribute.Int("limit", limit),
			attribute.Bool("index_available", t.index != nil),
		),
	)
	defer span.End()

	// GR-01: Use index first for O(1) lookup instead of O(V) graph scan
	var results map[string]*graph.QueryResult
	var err error
	var queryErrors int // C2: Track query errors

	if t.index != nil {
		// O(1) index lookup
		symbols := t.index.GetByName(name)

		// Filter to only interface symbols (per review finding H3)
		var interfaces []*ast.Symbol
		var nonInterfaces int
		for _, sym := range symbols {
			// C1: Nil symbol check
			if sym == nil {
				continue
			}
			if sym.Kind == ast.SymbolKindInterface {
				interfaces = append(interfaces, sym)
			} else {
				nonInterfaces++
			}
		}

		// H3: Log filtered symbols for debugging
		if nonInterfaces > 0 {
			slog.Debug("filtered non-interface symbols",
				slog.String("tool", "find_implementations"),
				slog.String("interface_name", name),
				slog.Int("filtered_count", nonInterfaces),
				slog.Int("interfaces_found", len(interfaces)),
			)
		}

		span.SetAttributes(
			attribute.Bool("index_used", true),
			attribute.Int("index_matches", len(symbols)),
			attribute.Int("interfaces_found", len(interfaces)),
			attribute.Int("non_interfaces_filtered", nonInterfaces),
		)

		if len(interfaces) > 0 {
			results = make(map[string]*graph.QueryResult, len(interfaces))
			for _, sym := range interfaces {
				if err := ctx.Err(); err != nil {
					span.RecordError(err)
					return nil, err
				}
				result, qErr := t.graph.FindImplementationsByID(ctx, sym.ID, graph.WithLimit(limit))
				if qErr != nil {
					// C2: Track errors instead of silent continue
					queryErrors++
					slog.Warn("graph query failed",
						slog.String("tool", "find_implementations"),
						slog.String("operation", "FindImplementationsByID"),
						slog.String("symbol_id", sym.ID),
						slog.String("error", qErr.Error()),
					)
					continue
				}
				results[sym.ID] = result
			}
			// C2: Record query errors in span
			if queryErrors > 0 {
				span.SetAttributes(attribute.Int("query_errors", queryErrors))
			}
		} else {
			// No matching interfaces in index - return empty result (O(1) fail-fast)
			span.SetAttributes(attribute.Bool("fast_not_found", true))
			results = make(map[string]*graph.QueryResult)
		}
	} else {
		// Fallback to O(V) graph scan (index unavailable)
		slog.Warn("graph query fallback",
			slog.String("tool", "find_implementations"),
			slog.String("reason", "index_unavailable"),
			slog.String("interface_name", name),
		)
		span.SetAttributes(attribute.Bool("index_used", false))
		results, err = t.graph.FindImplementationsByName(ctx, name, graph.WithLimit(limit))
		if err != nil {
			span.RecordError(err)
			// M2: Improved error wrapping
			return &Result{Success: false, Error: fmt.Sprintf("find implementations for '%s': %v", name, err)}, nil
		}
	}

	output := formatImplementationResults(name, results)
	outputText := formatImplementationResultsText(name, results)

	span.SetAttributes(
		attribute.Int("interface_count", len(results)),
		attribute.Int("total_implementations", countTotalCallers(results)),
	)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// =============================================================================
// find_symbol Tool (P1)
// =============================================================================

// findSymbolTool looks up symbols by name.
type findSymbolTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindSymbolTool creates the find_symbol tool.
func NewFindSymbolTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findSymbolTool{
		graph: g,
		index: idx,
	}
}

func (t *findSymbolTool) Name() string {
	return "find_symbol"
}

func (t *findSymbolTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findSymbolTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_symbol",
		Description: "Look up a symbol (function, type, variable) by name. " +
			"Returns symbol details including location, signature, and documentation. " +
			"Use this to resolve ambiguous names or find where something is defined.",
		Parameters: map[string]ParamDef{
			"name": {
				Type:        ParamTypeString,
				Description: "Name of the symbol to find",
				Required:    true,
			},
			"kind": {
				Type:        ParamTypeString,
				Description: "Filter by symbol kind: function, type, interface, variable, constant, method, or all",
				Required:    false,
				Default:     "all",
				Enum:        []any{"function", "type", "interface", "variable", "constant", "method", "all"},
			},
			"package": {
				Type:        ParamTypeString,
				Description: "Filter by package path (optional)",
				Required:    false,
			},
		},
		Category:    CategoryExploration,
		Priority:    92,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findSymbolTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	ctx, span := graphQueryTracer.Start(ctx, "find_symbol.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_symbol"),
		),
	)
	defer span.End()

	name, _ := params["name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "name is required"}, nil
	}

	span.SetAttributes(attribute.String("name", name))

	kindFilter, _ := params["kind"].(string)
	if kindFilter == "" {
		kindFilter = "all"
	}
	span.SetAttributes(attribute.String("kind", kindFilter))

	packageFilter, _ := params["package"].(string)
	if packageFilter != "" {
		span.SetAttributes(attribute.String("package", packageFilter))
	}

	// Use index to find symbols by name
	var matches []*ast.Symbol
	if t.index != nil {
		matches = t.index.GetByName(name)
	}

	// Apply filters
	var filtered []*ast.Symbol
	for _, sym := range matches {
		if sym == nil {
			continue
		}

		// Filter by kind
		if kindFilter != "all" && !matchesKind(sym.Kind, kindFilter) {
			continue
		}

		// Filter by package
		if packageFilter != "" && sym.Package != packageFilter {
			continue
		}

		filtered = append(filtered, sym)
	}

	output := formatSymbolResults(name, filtered)
	outputText := formatSymbolResultsText(name, filtered)

	span.SetAttributes(attribute.Int("match_count", len(filtered)))

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// =============================================================================
// get_call_chain Tool (P1)
// =============================================================================

// getCallChainTool wraps graph.GetCallGraph and GetReverseCallGraph.
type getCallChainTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewGetCallChainTool creates the get_call_chain tool.
func NewGetCallChainTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &getCallChainTool{
		graph: g,
		index: idx,
	}
}

func (t *getCallChainTool) Name() string {
	return "get_call_chain"
}

func (t *getCallChainTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *getCallChainTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "get_call_chain",
		Description: "Get the transitive call chain for a function. " +
			"Can trace either 'downstream' (what does this call, recursively) or " +
			"'upstream' (what calls this, recursively). " +
			"Useful for impact analysis and understanding full code flow.",
		Parameters: map[string]ParamDef{
			"function_name": {
				Type:        ParamTypeString,
				Description: "Name of the function to trace",
				Required:    true,
			},
			"direction": {
				Type:        ParamTypeString,
				Description: "Direction to trace: 'downstream' (callees) or 'upstream' (callers)",
				Required:    false,
				Default:     "downstream",
				Enum:        []any{"downstream", "upstream"},
			},
			"max_depth": {
				Type:        ParamTypeInt,
				Description: "Maximum depth to traverse (1-10)",
				Required:    false,
				Default:     5,
			},
		},
		Category:    CategoryExploration,
		Priority:    88,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *getCallChainTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	ctx, span := graphQueryTracer.Start(ctx, "get_call_chain.Execute",
		trace.WithAttributes(
			attribute.String("tool", "get_call_chain"),
		),
	)
	defer span.End()

	name, _ := params["function_name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "function_name is required"}, nil
	}

	span.SetAttributes(attribute.String("function_name", name))

	direction, _ := params["direction"].(string)
	if direction == "" {
		direction = "downstream"
	}
	span.SetAttributes(attribute.String("direction", direction))

	maxDepth := 5
	if d, ok := getIntParam(params, "max_depth"); ok && d > 0 {
		if d > 10 {
			d = 10 // Cap at 10
		}
		maxDepth = d
	}
	span.SetAttributes(attribute.Int("max_depth", maxDepth))

	// First, find the symbol ID
	var symbolID string
	if t.index != nil {
		matches := t.index.GetByName(name)
		for _, sym := range matches {
			if sym != nil && (sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod) {
				symbolID = sym.ID
				break
			}
		}
	}

	if symbolID == "" {
		return &Result{
			Success:    true,
			Output:     map[string]any{"message": fmt.Sprintf("No function named '%s' found", name)},
			OutputText: fmt.Sprintf("No function named '%s' found in the codebase.", name),
			TokensUsed: 10,
		}, nil
	}

	// Execute the appropriate traversal
	var traversal *graph.TraversalResult
	var err error

	if direction == "upstream" {
		traversal, err = t.graph.GetReverseCallGraph(ctx, symbolID, graph.WithMaxDepth(maxDepth))
	} else {
		traversal, err = t.graph.GetCallGraph(ctx, symbolID, graph.WithMaxDepth(maxDepth))
	}

	if err != nil {
		span.RecordError(err)
		return &Result{Success: false, Error: fmt.Sprintf("traversal failed: %v", err)}, nil
	}

	output := formatCallChainResults(name, direction, traversal, t.index)
	outputText := formatCallChainResultsText(name, direction, traversal, t.index)

	span.SetAttributes(
		attribute.Int("nodes_visited", len(traversal.VisitedNodes)),
		attribute.Int("depth", traversal.Depth),
		attribute.Bool("truncated", traversal.Truncated),
	)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// =============================================================================
// find_references Tool (P1)
// =============================================================================

// findReferencesTool wraps graph.FindReferencesByID.
type findReferencesTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindReferencesTool creates the find_references tool.
func NewFindReferencesTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findReferencesTool{
		graph: g,
		index: idx,
	}
}

func (t *findReferencesTool) Name() string {
	return "find_references"
}

func (t *findReferencesTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findReferencesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_references",
		Description: "Find all references to a symbol (function, type, variable). " +
			"Returns all locations where the symbol is used, not just calls. " +
			"Useful for refactoring and understanding symbol usage.",
		Parameters: map[string]ParamDef{
			"symbol_name": {
				Type:        ParamTypeString,
				Description: "Name of the symbol to find references for",
				Required:    true,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of references to return",
				Required:    false,
				Default:     100,
			},
		},
		Category:    CategoryExploration,
		Priority:    87,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findReferencesTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	ctx, span := graphQueryTracer.Start(ctx, "find_references.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_references"),
		),
	)
	defer span.End()

	name, _ := params["symbol_name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "symbol_name is required"}, nil
	}

	span.SetAttributes(attribute.String("symbol_name", name))

	limit := 100
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		limit = l
	}

	// Find symbol IDs first
	var allLocations []referenceLocation
	if t.index != nil {
		matches := t.index.GetByName(name)
		for _, sym := range matches {
			if sym == nil {
				continue
			}

			locations, err := t.graph.FindReferencesByID(ctx, sym.ID, graph.WithLimit(limit))
			if err != nil {
				continue
			}

			for _, loc := range locations {
				allLocations = append(allLocations, referenceLocation{
					SymbolID:   sym.ID,
					SymbolName: sym.Name,
					Package:    sym.Package,
					Location:   loc,
				})

				if len(allLocations) >= limit {
					break
				}
			}

			if len(allLocations) >= limit {
				break
			}
		}
	}

	output := formatReferenceResults(name, allLocations)
	outputText := formatReferenceResultsText(name, allLocations)

	span.SetAttributes(attribute.Int("reference_count", len(allLocations)))

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// referenceLocation pairs a symbol with a reference location.
type referenceLocation struct {
	SymbolID   string
	SymbolName string
	Package    string
	Location   ast.Location
}

// =============================================================================
// Helper Functions
// =============================================================================

// matchesKind checks if a symbol kind matches a filter string.
func matchesKind(kind ast.SymbolKind, filter string) bool {
	switch filter {
	case "function":
		return kind == ast.SymbolKindFunction
	case "method":
		return kind == ast.SymbolKindMethod
	case "type":
		return kind == ast.SymbolKindType || kind == ast.SymbolKindStruct
	case "interface":
		return kind == ast.SymbolKindInterface
	case "variable":
		return kind == ast.SymbolKindVariable
	case "constant":
		return kind == ast.SymbolKindConstant
	default:
		return true
	}
}

// countTotalCallers counts total symbols across all query results.
func countTotalCallers(results map[string]*graph.QueryResult) int {
	total := 0
	for _, r := range results {
		if r != nil {
			total += len(r.Symbols)
		}
	}
	return total
}

// formatCallerResults formats caller results as a map.
func formatCallerResults(name string, results map[string]*graph.QueryResult) map[string]any {
	output := map[string]any{
		"function_name": name,
		"match_count":   len(results),
	}

	var callers []map[string]any
	for symbolID, result := range results {
		if result == nil {
			continue
		}

		entry := map[string]any{
			"target_id":    symbolID,
			"caller_count": len(result.Symbols),
		}

		var callerList []map[string]any
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			callerList = append(callerList, map[string]any{
				"name":      sym.Name,
				"file":      sym.FilePath,
				"line":      sym.StartLine,
				"package":   sym.Package,
				"signature": sym.Signature,
			})
		}
		entry["callers"] = callerList
		callers = append(callers, entry)
	}
	output["results"] = callers

	return output
}

// formatCallerResultsText formats caller results as readable text.
func formatCallerResultsText(name string, results map[string]*graph.QueryResult) string {
	var sb strings.Builder

	totalCallers := countTotalCallers(results)
	if totalCallers == 0 {
		sb.WriteString(fmt.Sprintf("No callers found for function '%s'.\n", name))
		sb.WriteString("This could mean:\n")
		sb.WriteString("  - The function is not called anywhere (dead code)\n")
		sb.WriteString("  - The function is called via interface/function pointer\n")
		sb.WriteString("  - The function name doesn't exist in the codebase\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d callers of '%s':\n\n", totalCallers, name))

	for symbolID, result := range results {
		if result == nil || len(result.Symbols) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("Target: %s\n", symbolID))
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("  • %s() in %s:%d\n", sym.Name, sym.FilePath, sym.StartLine))
			if sym.Package != "" {
				sb.WriteString(fmt.Sprintf("    Package: %s\n", sym.Package))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatCalleeResults formats callee results as a map.
//
// GR-41: Separates resolved (in-codebase) callees from external/placeholder ones.
func formatCalleeResults(name string, results map[string]*graph.QueryResult) map[string]any {
	var resolvedCallees []map[string]any
	var externalCallees []string

	for symbolID, result := range results {
		if result == nil {
			continue
		}

		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			// External/placeholder symbols have empty FilePath or Kind=External
			if sym.FilePath == "" || sym.Kind == ast.SymbolKindExternal {
				externalCallees = append(externalCallees, sym.Name)
			} else {
				resolvedCallees = append(resolvedCallees, map[string]any{
					"name":      sym.Name,
					"file":      sym.FilePath,
					"line":      sym.StartLine,
					"package":   sym.Package,
					"signature": sym.Signature,
					"source_id": symbolID,
				})
			}
		}
	}

	// Deduplicate external callees
	seen := make(map[string]bool)
	var uniqueExternal []string
	for _, name := range externalCallees {
		if !seen[name] {
			seen[name] = true
			uniqueExternal = append(uniqueExternal, name)
		}
	}

	return map[string]any{
		"function_name":    name,
		"resolved_count":   len(resolvedCallees),
		"external_count":   len(uniqueExternal),
		"total_count":      len(resolvedCallees) + len(uniqueExternal),
		"resolved_callees": resolvedCallees,
		"external_callees": uniqueExternal,
	}
}

// formatCalleeResultsText formats callee results as readable text.
//
// Description:
//
//	Separates in-codebase callees (with file locations) from external/stdlib
//	callees (placeholders without file paths). This helps the agent understand
//	which callees are navigable and which are external dependencies.
//
// GR-41: Improved to distinguish resolved vs external callees for agent clarity.
func formatCalleeResultsText(name string, results map[string]*graph.QueryResult) string {
	var sb strings.Builder

	// Separate resolved (in-codebase) and external callees
	var resolvedCallees []struct {
		name     string
		file     string
		line     int
		sourceID string
	}
	var externalCallees []string

	for symbolID, result := range results {
		if result == nil {
			continue
		}
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			// External/placeholder symbols have empty FilePath or Kind=External
			if sym.FilePath == "" || sym.Kind == ast.SymbolKindExternal {
				externalCallees = append(externalCallees, sym.Name)
			} else {
				resolvedCallees = append(resolvedCallees, struct {
					name     string
					file     string
					line     int
					sourceID string
				}{
					name:     sym.Name,
					file:     sym.FilePath,
					line:     sym.StartLine,
					sourceID: symbolID,
				})
			}
		}
	}

	totalResolved := len(resolvedCallees)
	totalExternal := len(externalCallees)
	totalCallees := totalResolved + totalExternal

	if totalCallees == 0 {
		sb.WriteString(fmt.Sprintf("No callees found for function '%s'.\n", name))
		sb.WriteString("This could mean:\n")
		sb.WriteString("  - The function doesn't call any other functions\n")
		sb.WriteString("  - The function name doesn't exist in the codebase\n")
		return sb.String()
	}

	// Header with clear breakdown
	sb.WriteString(fmt.Sprintf("Function '%s' calls %d functions", name, totalCallees))
	if totalResolved > 0 && totalExternal > 0 {
		sb.WriteString(fmt.Sprintf(" (%d in-codebase, %d external/stdlib)", totalResolved, totalExternal))
	}
	sb.WriteString(":\n\n")

	// Show resolved (in-codebase) callees first - these are actionable
	if totalResolved > 0 {
		sb.WriteString("## In-Codebase Callees (navigable)\n")
		for _, callee := range resolvedCallees {
			sb.WriteString(fmt.Sprintf("  → %s() in %s:%d\n", callee.name, callee.file, callee.line))
		}
		sb.WriteString("\n")
	}

	// Summarize external callees without cluttering - these aren't navigable
	if totalExternal > 0 {
		sb.WriteString("## External/Stdlib Callees (not in codebase)\n")
		// Deduplicate and sort external names
		seen := make(map[string]bool)
		var uniqueExternal []string
		for _, name := range externalCallees {
			if !seen[name] {
				seen[name] = true
				uniqueExternal = append(uniqueExternal, name)
			}
		}
		// Show up to 10 external calls, summarize the rest
		if len(uniqueExternal) <= 10 {
			for _, name := range uniqueExternal {
				sb.WriteString(fmt.Sprintf("  → %s() (external)\n", name))
			}
		} else {
			for _, name := range uniqueExternal[:10] {
				sb.WriteString(fmt.Sprintf("  → %s() (external)\n", name))
			}
			sb.WriteString(fmt.Sprintf("  ... and %d more external calls\n", len(uniqueExternal)-10))
		}
	}

	return sb.String()
}

// formatImplementationResults formats implementation results as a map.
func formatImplementationResults(name string, results map[string]*graph.QueryResult) map[string]any {
	output := map[string]any{
		"interface_name": name,
		"match_count":    len(results),
	}

	var impls []map[string]any
	for interfaceID, result := range results {
		if result == nil {
			continue
		}

		entry := map[string]any{
			"interface_id": interfaceID,
			"impl_count":   len(result.Symbols),
		}

		var implList []map[string]any
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			implList = append(implList, map[string]any{
				"name":    sym.Name,
				"file":    sym.FilePath,
				"line":    sym.StartLine,
				"package": sym.Package,
				"kind":    sym.Kind.String(),
			})
		}
		entry["implementations"] = implList
		impls = append(impls, entry)
	}
	output["results"] = impls

	return output
}

// formatImplementationResultsText formats implementation results as readable text.
func formatImplementationResultsText(name string, results map[string]*graph.QueryResult) string {
	var sb strings.Builder

	totalImpls := countTotalCallers(results)
	if totalImpls == 0 {
		sb.WriteString(fmt.Sprintf("No implementations found for interface '%s'.\n", name))
		sb.WriteString("This could mean:\n")
		sb.WriteString("  - No types implement this interface\n")
		sb.WriteString("  - The interface name doesn't exist in the codebase\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d implementations of interface '%s':\n\n", totalImpls, name))

	for interfaceID, result := range results {
		if result == nil || len(result.Symbols) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("Interface: %s\n", interfaceID))
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("  • %s (%s) in %s:%d\n", sym.Name, sym.Kind, sym.FilePath, sym.StartLine))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatSymbolResults formats symbol lookup results as a map.
func formatSymbolResults(name string, symbols []*ast.Symbol) map[string]any {
	output := map[string]any{
		"search_name": name,
		"match_count": len(symbols),
	}

	var matches []map[string]any
	for _, sym := range symbols {
		if sym == nil {
			continue
		}
		matches = append(matches, map[string]any{
			"id":        sym.ID,
			"name":      sym.Name,
			"kind":      sym.Kind.String(),
			"file":      sym.FilePath,
			"line":      sym.StartLine,
			"package":   sym.Package,
			"signature": sym.Signature,
			"exported":  sym.Exported,
		})
	}
	output["symbols"] = matches

	return output
}

// formatSymbolResultsText formats symbol lookup results as readable text.
func formatSymbolResultsText(name string, symbols []*ast.Symbol) string {
	var sb strings.Builder

	if len(symbols) == 0 {
		sb.WriteString(fmt.Sprintf("No symbols found matching '%s'.\n", name))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d symbols matching '%s':\n\n", len(symbols), name))

	for _, sym := range symbols {
		if sym == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("• %s (%s)\n", sym.Name, sym.Kind))
		sb.WriteString(fmt.Sprintf("  Location: %s:%d\n", sym.FilePath, sym.StartLine))
		sb.WriteString(fmt.Sprintf("  Package: %s\n", sym.Package))
		if sym.Signature != "" {
			sb.WriteString(fmt.Sprintf("  Signature: %s\n", sym.Signature))
		}
		if sym.Exported {
			sb.WriteString("  Exported: yes\n")
		}
		sb.WriteString(fmt.Sprintf("  ID: %s\n", sym.ID))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatCallChainResults formats call chain traversal results as a map.
func formatCallChainResults(name, direction string, traversal *graph.TraversalResult, idx *index.SymbolIndex) map[string]any {
	output := map[string]any{
		"function_name": name,
		"direction":     direction,
		"depth":         traversal.Depth,
		"truncated":     traversal.Truncated,
		"node_count":    len(traversal.VisitedNodes),
		"edge_count":    len(traversal.Edges),
	}

	// Build node details
	var nodes []map[string]any
	for _, nodeID := range traversal.VisitedNodes {
		entry := map[string]any{"id": nodeID}

		// Try to get symbol info
		if idx != nil {
			if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
				entry["name"] = sym.Name
				entry["file"] = sym.FilePath
				entry["line"] = sym.StartLine
				entry["package"] = sym.Package
			}
		}
		nodes = append(nodes, entry)
	}
	output["nodes"] = nodes

	return output
}

// formatCallChainResultsText formats call chain results as readable text.
func formatCallChainResultsText(name, direction string, traversal *graph.TraversalResult, idx *index.SymbolIndex) string {
	var sb strings.Builder

	if len(traversal.VisitedNodes) == 0 {
		sb.WriteString(fmt.Sprintf("No call chain found for '%s' (%s).\n", name, direction))
		return sb.String()
	}

	dirLabel := "calls"
	if direction == "upstream" {
		dirLabel = "is called by"
	}

	sb.WriteString(fmt.Sprintf("Call chain for '%s' (%s):\n", name, direction))
	sb.WriteString(fmt.Sprintf("Depth: %d, Nodes: %d", traversal.Depth, len(traversal.VisitedNodes)))
	if traversal.Truncated {
		sb.WriteString(" (truncated)")
	}
	sb.WriteString("\n\n")

	for i, nodeID := range traversal.VisitedNodes {
		indent := strings.Repeat("  ", i)
		prefix := "→"
		if direction == "upstream" {
			prefix = "←"
		}

		nodeName := nodeID
		if idx != nil {
			if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
				nodeName = fmt.Sprintf("%s() [%s:%d]", sym.Name, sym.FilePath, sym.StartLine)
			}
		}

		if i == 0 {
			sb.WriteString(fmt.Sprintf("%s%s (root) %s\n", indent, nodeName, dirLabel))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s %s\n", indent, prefix, nodeName))
		}
	}

	return sb.String()
}

// formatReferenceResults formats reference results as a map.
func formatReferenceResults(name string, refs []referenceLocation) map[string]any {
	output := map[string]any{
		"symbol_name":     name,
		"reference_count": len(refs),
	}

	var locations []map[string]any
	for _, ref := range refs {
		locations = append(locations, map[string]any{
			"symbol_id": ref.SymbolID,
			"package":   ref.Package,
			"file":      ref.Location.FilePath,
			"line":      ref.Location.StartLine,
			"column":    ref.Location.StartCol,
		})
	}
	output["references"] = locations

	return output
}

// formatReferenceResultsText formats reference results as readable text.
func formatReferenceResultsText(name string, refs []referenceLocation) string {
	var sb strings.Builder

	if len(refs) == 0 {
		sb.WriteString(fmt.Sprintf("No references found for '%s'.\n", name))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d references to '%s':\n\n", len(refs), name))

	for _, ref := range refs {
		sb.WriteString(fmt.Sprintf("• %s:%d:%d\n", ref.Location.FilePath, ref.Location.StartLine, ref.Location.StartCol))
		if ref.Package != "" {
			sb.WriteString(fmt.Sprintf("  Package: %s\n", ref.Package))
		}
	}

	return sb.String()
}

// =============================================================================
// find_hotspots Tool (GR-02)
// =============================================================================

// findHotspotsTool wraps graph.GraphAnalytics.HotSpots.
//
// Description:
//
//	Finds the most-connected nodes in the graph (hotspots). Hotspots are
//	symbols with many incoming and outgoing edges, indicating high coupling
//	and potential refactoring targets.
//
// GR-01 Optimization:
//
//	Uses GraphAnalytics which operates on the HierarchicalGraph with O(V log k)
//	complexity for top-k hotspots.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findHotspotsTool struct {
	analytics *graph.GraphAnalytics
	index     *index.SymbolIndex
}

// NewFindHotspotsTool creates the find_hotspots tool.
//
// Description:
//
//	Creates a tool that finds the most-connected symbols in the codebase
//	(hotspots). Hotspots are nodes with high connectivity scores, indicating
//	central points in the code that many other components depend on.
//
// Inputs:
//
//   - analytics: The GraphAnalytics instance for hotspot detection. Must not be nil.
//   - idx: The symbol index for name lookups. Must not be nil.
//
// Outputs:
//
//   - Tool: The find_hotspots tool implementation.
//
// Limitations:
//
//   - Connectivity score uses formula: inDegree*2 + outDegree (favors callee-heavy nodes)
//   - Maximum 100 results per query to prevent excessive output
//   - When filtering by kind, results may be fewer than requested if not enough match
//
// Assumptions:
//
//   - Graph is frozen and indexed before tool creation
//   - Analytics wraps a HierarchicalGraph for O(V log k) complexity
func NewFindHotspotsTool(analytics *graph.GraphAnalytics, idx *index.SymbolIndex) Tool {
	return &findHotspotsTool{
		analytics: analytics,
		index:     idx,
	}
}

func (t *findHotspotsTool) Name() string {
	return "find_hotspots"
}

func (t *findHotspotsTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findHotspotsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_hotspots",
		Description: "Find the most-connected symbols in the codebase (hotspots). " +
			"Hotspots indicate high coupling and potential refactoring targets. " +
			"Returns symbols ranked by connectivity score (inDegree*2 + outDegree).",
		Parameters: map[string]ParamDef{
			"top": {
				Type:        ParamTypeInt,
				Description: "Number of hotspots to return (1-100)",
				Required:    false,
				Default:     10,
			},
			"kind": {
				Type:        ParamTypeString,
				Description: "Filter by symbol kind: function, type, or all",
				Required:    false,
				Default:     "all",
				Enum:        []any{"function", "type", "all"},
			},
		},
		Category:    CategoryExploration,
		Priority:    86,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     5 * time.Second,
	}
}

func (t *findHotspotsTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// Validate analytics is available
	if t.analytics == nil {
		return &Result{Success: false, Error: "graph analytics not initialized"}, nil
	}

	// Parse and validate parameters
	top := 10
	if topParam, ok := getIntParam(params, "top"); ok && topParam > 0 {
		if topParam > 100 {
			topParam = 100 // Cap at 100 to prevent excessive output
		}
		top = topParam
	}

	kindFilter, _ := params["kind"].(string)
	if kindFilter == "" {
		kindFilter = "all"
	}

	// C1 FIX: Validate kind parameter against allowed values
	validKinds := map[string]bool{"function": true, "type": true, "all": true}
	if !validKinds[kindFilter] {
		slog.Warn("invalid kind filter, defaulting to all",
			slog.String("tool", "find_hotspots"),
			slog.String("invalid_kind", kindFilter),
		)
		kindFilter = "all"
	}

	// Start span with context
	ctx, span := graphQueryTracer.Start(ctx, "find_hotspots.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_hotspots"),
			attribute.Int("top", top),
			attribute.String("kind", kindFilter),
		),
	)
	defer span.End()

	// Check context cancellation before expensive operation
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// M1 FIX: Adaptive request size based on filter
	// When filtering by kind, we expect roughly 50% to be filtered out (functions vs types)
	// When using "all", no filtering needed so request exact count
	requestCount := top
	if kindFilter != "all" {
		requestCount = top * 3 // Request 3x when filtering to ensure enough results after filter
		slog.Debug("hotspot request adjusted for filtering",
			slog.String("tool", "find_hotspots"),
			slog.String("kind_filter", kindFilter),
			slog.Int("top_requested", top),
			slog.Int("request_count", requestCount),
		)
	}

	// Get hotspots using CRS-enabled method for tracing
	hotspots, traceStep := t.analytics.HotSpotsWithCRS(ctx, requestCount)

	span.SetAttributes(
		attribute.Int("raw_hotspots", len(hotspots)),
		attribute.String("trace_action", traceStep.Action),
	)

	// Filter by kind if needed
	var filtered []graph.HotspotNode
	if kindFilter == "all" {
		filtered = hotspots
	} else {
		for _, hs := range hotspots {
			if hs.Node == nil || hs.Node.Symbol == nil {
				continue
			}
			if matchesKind(hs.Node.Symbol.Kind, kindFilter) {
				filtered = append(filtered, hs)
			}
		}
	}

	// Trim to requested count
	if len(filtered) > top {
		filtered = filtered[:top]
	}

	span.SetAttributes(attribute.Int("filtered_hotspots", len(filtered)))

	// Structured logging for edge cases
	if len(hotspots) > 0 && len(filtered) == 0 {
		slog.Debug("all hotspots filtered by kind",
			slog.String("tool", "find_hotspots"),
			slog.Int("raw_count", len(hotspots)),
			slog.String("kind_filter", kindFilter),
		)
	} else if len(filtered) < top && kindFilter != "all" {
		slog.Debug("fewer hotspots than requested after filtering",
			slog.String("tool", "find_hotspots"),
			slog.Int("requested", top),
			slog.Int("returned", len(filtered)),
			slog.String("kind_filter", kindFilter),
		)
	}

	// Format output
	output := formatHotspotResults(filtered)
	outputText := formatHotspotResultsText(filtered)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// formatHotspotResults formats hotspot results as a map.
func formatHotspotResults(hotspots []graph.HotspotNode) map[string]any {
	results := make([]map[string]any, 0, len(hotspots))

	for i, hs := range hotspots {
		if hs.Node == nil || hs.Node.Symbol == nil {
			continue
		}
		sym := hs.Node.Symbol
		results = append(results, map[string]any{
			"rank":       i + 1,
			"name":       sym.Name,
			"kind":       sym.Kind.String(),
			"file":       sym.FilePath,
			"line":       sym.StartLine,
			"package":    sym.Package,
			"score":      hs.Score,
			"in_degree":  hs.InDegree,
			"out_degree": hs.OutDegree,
		})
	}

	return map[string]any{
		"hotspot_count": len(results),
		"hotspots":      results,
	}
}

// formatHotspotResultsText formats hotspot results as readable text.
func formatHotspotResultsText(hotspots []graph.HotspotNode) string {
	var sb strings.Builder

	if len(hotspots) == 0 {
		sb.WriteString("No hotspots found.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Top %d Hotspots by Connectivity:\n\n", len(hotspots)))

	for i, hs := range hotspots {
		if hs.Node == nil || hs.Node.Symbol == nil {
			continue
		}
		sym := hs.Node.Symbol
		sb.WriteString(fmt.Sprintf("%d. %s (score: %d)\n", i+1, sym.Name, hs.Score))
		sb.WriteString(fmt.Sprintf("   %s:%d\n", sym.FilePath, sym.StartLine))
		sb.WriteString(fmt.Sprintf("   InDegree: %d, OutDegree: %d\n", hs.InDegree, hs.OutDegree))
		if sym.Kind != ast.SymbolKindUnknown {
			sb.WriteString(fmt.Sprintf("   Kind: %s\n", sym.Kind))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// =============================================================================
// find_dead_code Tool (GR-03)
// =============================================================================

// findDeadCodeTool wraps graph.GraphAnalytics.DeadCode.
//
// Description:
//
//	Finds symbols with no incoming edges (potential unused code).
//	Excludes entry points (main, init, Test*) and interface methods.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findDeadCodeTool struct {
	analytics *graph.GraphAnalytics
	index     *index.SymbolIndex
}

// NewFindDeadCodeTool creates the find_dead_code tool.
//
// Description:
//
//	Creates a tool that finds potentially unused code (symbols with no callers).
//	Automatically excludes entry points (main, init, Test*) and interface methods.
//
// Inputs:
//
//   - analytics: The GraphAnalytics instance for dead code detection. Must not be nil.
//   - idx: The symbol index for name lookups. Must not be nil.
//
// Outputs:
//
//   - Tool: The find_dead_code tool implementation.
//
// Limitations:
//
//   - Cannot detect usage via reflection or dynamic calls
//   - Exported symbols may be used by external packages (use include_exported=true carefully)
//   - Maximum 500 results per query
//   - Package filter uses exact match on package path
//
// Assumptions:
//
//   - Graph is frozen before tool creation
//   - Entry points (main, init, Test*) are pre-filtered by analytics
func NewFindDeadCodeTool(analytics *graph.GraphAnalytics, idx *index.SymbolIndex) Tool {
	return &findDeadCodeTool{
		analytics: analytics,
		index:     idx,
	}
}

func (t *findDeadCodeTool) Name() string {
	return "find_dead_code"
}

func (t *findDeadCodeTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findDeadCodeTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_dead_code",
		Description: "Find potentially unused code (symbols with no callers). " +
			"Excludes entry points (main, init, Test*) and interface methods. " +
			"By default only shows unexported symbols; use include_exported=true for all.",
		Parameters: map[string]ParamDef{
			"include_exported": {
				Type:        ParamTypeBool,
				Description: "Include exported symbols (by default only unexported are shown)",
				Required:    false,
				Default:     false,
			},
			"package": {
				Type:        ParamTypeString,
				Description: "Filter to a specific package path",
				Required:    false,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of results to return",
				Required:    false,
				Default:     50,
			},
		},
		Category:    CategoryExploration,
		Priority:    84,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *findDeadCodeTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	if t.analytics == nil {
		return &Result{Success: false, Error: "graph analytics not initialized"}, nil
	}

	// Parse parameters
	includeExported := false
	if v, ok := params["include_exported"].(bool); ok {
		includeExported = v
	}

	packageFilter, _ := params["package"].(string)

	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		if l > 500 {
			l = 500 // Cap at 500
		}
		limit = l
	}

	ctx, span := graphQueryTracer.Start(ctx, "find_dead_code.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_dead_code"),
			attribute.Bool("include_exported", includeExported),
			attribute.String("package_filter", packageFilter),
			attribute.Int("limit", limit),
		),
	)
	defer span.End()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Get dead code
	deadCode, traceStep := t.analytics.DeadCodeWithCRS(ctx)

	span.SetAttributes(
		attribute.Int("raw_dead_code_count", len(deadCode)),
		attribute.String("trace_action", traceStep.Action),
	)

	// Apply filters
	var filtered []graph.DeadCodeNode
	for _, dc := range deadCode {
		if dc.Node == nil || dc.Node.Symbol == nil {
			continue
		}
		sym := dc.Node.Symbol

		// Filter by exported status
		if !includeExported && sym.Exported {
			continue
		}

		// Filter by package
		if packageFilter != "" && sym.Package != packageFilter {
			continue
		}

		filtered = append(filtered, dc)
		if len(filtered) >= limit {
			break
		}
	}

	span.SetAttributes(attribute.Int("filtered_count", len(filtered)))

	// Structured logging for edge cases
	if len(deadCode) > 0 && len(filtered) == 0 {
		slog.Debug("all dead code filtered out",
			slog.String("tool", "find_dead_code"),
			slog.Int("raw_count", len(deadCode)),
			slog.Bool("include_exported", includeExported),
			slog.String("package_filter", packageFilter),
		)
	} else if len(filtered) >= limit {
		slog.Debug("dead code results limited",
			slog.String("tool", "find_dead_code"),
			slog.Int("raw_count", len(deadCode)),
			slog.Int("limit", limit),
			slog.Int("returned", len(filtered)),
		)
	}

	output := formatDeadCodeResults(filtered)
	outputText := formatDeadCodeResultsText(filtered, len(deadCode))

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// formatDeadCodeResults formats dead code results as a map.
func formatDeadCodeResults(deadCode []graph.DeadCodeNode) map[string]any {
	results := make([]map[string]any, 0, len(deadCode))

	for _, dc := range deadCode {
		if dc.Node == nil || dc.Node.Symbol == nil {
			continue
		}
		sym := dc.Node.Symbol
		results = append(results, map[string]any{
			"name":     sym.Name,
			"kind":     sym.Kind.String(),
			"file":     sym.FilePath,
			"line":     sym.StartLine,
			"package":  sym.Package,
			"exported": sym.Exported,
			"reason":   dc.Reason,
		})
	}

	return map[string]any{
		"dead_code_count": len(results),
		"dead_code":       results,
	}
}

// formatDeadCodeResultsText formats dead code results as readable text.
func formatDeadCodeResultsText(deadCode []graph.DeadCodeNode, totalCount int) string {
	var sb strings.Builder

	if len(deadCode) == 0 {
		sb.WriteString("No potentially dead code found.\n")
		sb.WriteString("This is good news! All symbols appear to be used.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d potentially dead code symbols", len(deadCode)))
	if len(deadCode) < totalCount {
		sb.WriteString(fmt.Sprintf(" (showing %d of %d total)", len(deadCode), totalCount))
	}
	sb.WriteString(":\n\n")

	for i, dc := range deadCode {
		if dc.Node == nil || dc.Node.Symbol == nil {
			continue
		}
		sym := dc.Node.Symbol
		sb.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, sym.Name, sym.Kind))
		sb.WriteString(fmt.Sprintf("   %s:%d\n", sym.FilePath, sym.StartLine))
		sb.WriteString(fmt.Sprintf("   Reason: %s\n", dc.Reason))
		sb.WriteString("\n")
	}

	return sb.String()
}

// =============================================================================
// find_cycles Tool (GR-04)
// =============================================================================

// findCyclesTool wraps graph.GraphAnalytics.CyclicDependencies.
//
// Description:
//
//	Finds circular dependencies in the codebase using Tarjan's SCC algorithm.
//	Cycles indicate tight coupling that can make code harder to maintain.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findCyclesTool struct {
	analytics *graph.GraphAnalytics
	index     *index.SymbolIndex
}

// NewFindCyclesTool creates the find_cycles tool.
//
// Description:
//
//	Creates a tool that finds circular dependencies in the codebase using
//	Tarjan's SCC algorithm. Cycles indicate tight coupling that can make
//	code harder to maintain, test, and understand.
//
// Inputs:
//
//   - analytics: The GraphAnalytics instance for cycle detection. Must not be nil.
//   - idx: The symbol index for resolving node IDs to symbol names. Must not be nil.
//
// Outputs:
//
//   - Tool: The find_cycles tool implementation.
//
// Limitations:
//
//   - Only detects call-graph cycles, not import cycles or data flow cycles
//   - Maximum 100 cycles per query to prevent excessive output
//   - Large cycles (many nodes) may be harder to visualize in text output
//
// Assumptions:
//
//   - Graph is frozen before tool creation
//   - Tarjan's algorithm runs in O(V+E) time
func NewFindCyclesTool(analytics *graph.GraphAnalytics, idx *index.SymbolIndex) Tool {
	return &findCyclesTool{
		analytics: analytics,
		index:     idx,
	}
}

func (t *findCyclesTool) Name() string {
	return "find_cycles"
}

func (t *findCyclesTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findCyclesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_cycles",
		Description: "Find circular dependencies in the codebase. " +
			"Uses Tarjan's SCC algorithm to detect cycles. " +
			"Cycles indicate tight coupling that can make code harder to maintain.",
		Parameters: map[string]ParamDef{
			"min_size": {
				Type:        ParamTypeInt,
				Description: "Minimum cycle size to report (default: 2)",
				Required:    false,
				Default:     2,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of cycles to return",
				Required:    false,
				Default:     20,
			},
		},
		Category:    CategoryExploration,
		Priority:    82,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     15 * time.Second,
	}
}

func (t *findCyclesTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	if t.analytics == nil {
		return &Result{Success: false, Error: "graph analytics not initialized"}, nil
	}

	// Parse parameters
	minSize := 2
	if m, ok := getIntParam(params, "min_size"); ok && m >= 2 {
		minSize = m
	}

	limit := 20
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		if l > 100 {
			l = 100
		}
		limit = l
	}

	ctx, span := graphQueryTracer.Start(ctx, "find_cycles.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_cycles"),
			attribute.Int("min_size", minSize),
			attribute.Int("limit", limit),
		),
	)
	defer span.End()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Get cycles
	cycles, traceStep := t.analytics.CyclicDependenciesWithCRS(ctx)

	span.SetAttributes(
		attribute.Int("raw_cycles_count", len(cycles)),
		attribute.String("trace_action", traceStep.Action),
	)

	// Filter by min_size and apply limit
	var filtered []graph.CyclicDependency
	for _, cycle := range cycles {
		if cycle.Length >= minSize {
			filtered = append(filtered, cycle)
		}
		if len(filtered) >= limit {
			break
		}
	}

	span.SetAttributes(attribute.Int("filtered_cycles_count", len(filtered)))

	// Structured logging for edge cases
	if len(cycles) > 0 && len(filtered) == 0 {
		slog.Debug("all cycles filtered by min_size",
			slog.String("tool", "find_cycles"),
			slog.Int("raw_count", len(cycles)),
			slog.Int("min_size", minSize),
		)
	} else if len(filtered) >= limit {
		slog.Debug("cycle results limited",
			slog.String("tool", "find_cycles"),
			slog.Int("raw_count", len(cycles)),
			slog.Int("limit", limit),
			slog.Int("returned", len(filtered)),
		)
	}

	output := formatCycleResults(filtered, t.index)
	outputText := formatCycleResultsText(filtered, t.index)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// formatCycleResults formats cycle results as a map.
func formatCycleResults(cycles []graph.CyclicDependency, idx *index.SymbolIndex) map[string]any {
	results := make([]map[string]any, 0, len(cycles))

	for i, cycle := range cycles {
		// Resolve node names from IDs
		nodes := make([]map[string]any, 0, len(cycle.Nodes))
		for _, nodeID := range cycle.Nodes {
			nodeInfo := map[string]any{"id": nodeID}
			if idx != nil {
				if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
					nodeInfo["name"] = sym.Name
					nodeInfo["file"] = sym.FilePath
					nodeInfo["line"] = sym.StartLine
				}
			}
			nodes = append(nodes, nodeInfo)
		}

		results = append(results, map[string]any{
			"cycle_number": i + 1,
			"length":       cycle.Length,
			"packages":     cycle.Packages,
			"nodes":        nodes,
		})
	}

	return map[string]any{
		"cycle_count": len(results),
		"cycles":      results,
	}
}

// formatCycleResultsText formats cycle results as readable text.
func formatCycleResultsText(cycles []graph.CyclicDependency, idx *index.SymbolIndex) string {
	var sb strings.Builder

	if len(cycles) == 0 {
		sb.WriteString("No circular dependencies found.\n")
		sb.WriteString("This is good news! The codebase has no detectable cycles.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Found %d circular dependencies:\n\n", len(cycles)))

	for i, cycle := range cycles {
		sb.WriteString(fmt.Sprintf("Cycle %d (%d nodes):\n", i+1, cycle.Length))

		// Show the cycle path
		for j, nodeID := range cycle.Nodes {
			prefix := "  "
			if j < len(cycle.Nodes)-1 {
				prefix = "  ↓ "
			}

			nodeName := nodeID
			nodeFile := ""
			if idx != nil {
				if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
					nodeName = sym.Name + "()"
					nodeFile = fmt.Sprintf(" [%s:%d]", sym.FilePath, sym.StartLine)
				}
			}

			if j == 0 {
				sb.WriteString(fmt.Sprintf("  %s%s\n", nodeName, nodeFile))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s%s\n", prefix, nodeName, nodeFile))
			}
		}

		// Show closing edge back to first node
		if len(cycle.Nodes) > 0 {
			firstNode := cycle.Nodes[0]
			firstName := firstNode
			if idx != nil {
				if sym, ok := idx.GetByID(firstNode); ok && sym != nil {
					firstName = sym.Name + "()"
				}
			}
			sb.WriteString(fmt.Sprintf("  ↓ %s (cycle back)\n", firstName))
		}

		if len(cycle.Packages) > 1 {
			sb.WriteString(fmt.Sprintf("  Packages involved: %s\n", strings.Join(cycle.Packages, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// =============================================================================
// find_path Tool (GR-05)
// =============================================================================

// findPathTool wraps graph.Graph.ShortestPath.
//
// Description:
//
//	Finds the shortest path between two symbols in the graph.
//	Uses BFS to find the minimum-edge path considering all edge types.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findPathTool struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFindPathTool creates the find_path tool.
//
// Description:
//
//	Creates a tool that finds the shortest path between two symbols in the
//	code graph. Uses BFS to find the minimum-hop path considering all edge types.
//
// Inputs:
//
//   - g: The code graph. Must not be nil.
//   - idx: The symbol index for name-to-ID resolution. Must not be nil.
//
// Outputs:
//
//   - Tool: The find_path tool implementation.
//
// Limitations:
//
//   - Symbol names must match exactly (no fuzzy matching)
//   - When multiple symbols share a name, uses first function/method found
//   - Returns only one path even if multiple shortest paths exist
//   - Path length is measured in hops, not weighted by call frequency
//
// Assumptions:
//
//   - Graph is frozen before tool creation
//   - BFS runs in O(V+E) time
//   - Caller handles disambiguation via package filter if needed
func NewFindPathTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findPathTool{
		graph: g,
		index: idx,
	}
}

func (t *findPathTool) Name() string {
	return "find_path"
}

func (t *findPathTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findPathTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_path",
		Description: "Find the shortest path between two symbols. " +
			"Uses BFS to find the minimum-hop path. " +
			"Useful for understanding how two pieces of code are connected.",
		Parameters: map[string]ParamDef{
			"from": {
				Type:        ParamTypeString,
				Description: "Starting symbol name (e.g., 'main', 'parseConfig')",
				Required:    true,
			},
			"to": {
				Type:        ParamTypeString,
				Description: "Target symbol name",
				Required:    true,
			},
		},
		Category:    CategoryExploration,
		Priority:    83,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *findPathTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// C2 FIX: Validate graph is available
	if t.graph == nil {
		return &Result{Success: false, Error: "graph not initialized"}, nil
	}

	// Validate parameters
	fromName, _ := params["from"].(string)
	if fromName == "" {
		return &Result{Success: false, Error: "from is required"}, nil
	}

	toName, _ := params["to"].(string)
	if toName == "" {
		return &Result{Success: false, Error: "to is required"}, nil
	}

	ctx, span := graphQueryTracer.Start(ctx, "find_path.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_path"),
			attribute.String("from", fromName),
			attribute.String("to", toName),
		),
	)
	defer span.End()

	// Resolve symbol names to IDs using index
	var fromID, toID string
	var fromPackage, toPackage string // M4 FIX: Track selected package for disambiguation logging

	if t.index != nil {
		fromSymbols := t.index.GetByName(fromName)
		for _, sym := range fromSymbols {
			if sym != nil && (sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod) {
				fromID = sym.ID
				fromPackage = sym.Package
				break
			}
		}
		// If no function found, try any symbol
		if fromID == "" && len(fromSymbols) > 0 && fromSymbols[0] != nil {
			fromID = fromSymbols[0].ID
			fromPackage = fromSymbols[0].Package
		}

		// M4 FIX: Log disambiguation when multiple symbols match
		if len(fromSymbols) > 1 && fromID != "" {
			slog.Info("multiple symbols matched 'from', using first function",
				slog.String("tool", "find_path"),
				slog.String("symbol_name", fromName),
				slog.String("selected_package", fromPackage),
				slog.String("selected_id", fromID),
				slog.Int("total_matches", len(fromSymbols)),
			)
		}

		toSymbols := t.index.GetByName(toName)
		for _, sym := range toSymbols {
			if sym != nil && (sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod) {
				toID = sym.ID
				toPackage = sym.Package
				break
			}
		}
		if toID == "" && len(toSymbols) > 0 && toSymbols[0] != nil {
			toID = toSymbols[0].ID
			toPackage = toSymbols[0].Package
		}

		// M4 FIX: Log disambiguation when multiple symbols match
		if len(toSymbols) > 1 && toID != "" {
			slog.Info("multiple symbols matched 'to', using first function",
				slog.String("tool", "find_path"),
				slog.String("symbol_name", toName),
				slog.String("selected_package", toPackage),
				slog.String("selected_id", toID),
				slog.Int("total_matches", len(toSymbols)),
			)
		}
	}

	if fromID == "" {
		return &Result{
			Success:    true,
			Output:     map[string]any{"message": fmt.Sprintf("Symbol '%s' not found", fromName)},
			OutputText: fmt.Sprintf("Symbol '%s' not found in the codebase.", fromName),
			TokensUsed: 10,
		}, nil
	}

	if toID == "" {
		return &Result{
			Success:    true,
			Output:     map[string]any{"message": fmt.Sprintf("Symbol '%s' not found", toName)},
			OutputText: fmt.Sprintf("Symbol '%s' not found in the codebase.", toName),
			TokensUsed: 10,
		}, nil
	}

	span.SetAttributes(
		attribute.String("from_id", fromID),
		attribute.String("from_package", fromPackage),
		attribute.String("to_id", toID),
		attribute.String("to_package", toPackage),
	)

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// M5 FIX: Track timing for CRS TraceStep
	start := time.Now()

	// Find shortest path
	pathResult, err := t.graph.ShortestPath(ctx, fromID, toID)
	duration := time.Since(start)

	// M5 FIX: Create CRS TraceStep for path finding
	var traceStep crs.TraceStep
	if err != nil {
		traceStep = crs.NewTraceStepBuilder().
			WithAction("graph_shortest_path").
			WithTarget(fmt.Sprintf("%s→%s", fromName, toName)).
			WithTool("ShortestPath").
			WithDuration(duration).
			WithError(err.Error()).
			Build()
		span.RecordError(err)
		span.SetAttributes(attribute.String("trace_action", traceStep.Action))
		return &Result{Success: false, Error: fmt.Sprintf("path search failed: %v", err)}, nil
	}

	// M5 FIX: Build success TraceStep
	traceStep = crs.NewTraceStepBuilder().
		WithAction("graph_shortest_path").
		WithTarget(fmt.Sprintf("%s→%s", fromName, toName)).
		WithTool("ShortestPath").
		WithDuration(duration).
		WithMetadata("path_length", fmt.Sprintf("%d", pathResult.Length)).
		WithMetadata("from_id", fromID).
		WithMetadata("to_id", toID).
		Build()

	span.SetAttributes(
		attribute.Int("path_length", pathResult.Length),
		attribute.Int("path_nodes", len(pathResult.Path)),
		attribute.String("trace_action", traceStep.Action),
		attribute.Int64("trace_duration_ms", duration.Milliseconds()),
	)

	// Structured logging for edge cases
	if pathResult.Length < 0 {
		slog.Debug("no path found between symbols",
			slog.String("tool", "find_path"),
			slog.String("from", fromName),
			slog.String("to", toName),
			slog.String("from_id", fromID),
			slog.String("to_id", toID),
			slog.Int64("search_duration_ms", duration.Milliseconds()),
		)
	}

	output := formatPathResults(fromName, toName, pathResult, t.index)
	outputText := formatPathResultsText(fromName, toName, pathResult, t.index)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// formatPathResults formats path results as a map.
func formatPathResults(fromName, toName string, result *graph.PathResult, idx *index.SymbolIndex) map[string]any {
	output := map[string]any{
		"from":   fromName,
		"to":     toName,
		"length": result.Length,
		"found":  result.Length >= 0,
	}

	if result.Length < 0 {
		output["message"] = fmt.Sprintf("No path found between '%s' and '%s'", fromName, toName)
		return output
	}

	// Build path details
	path := make([]map[string]any, 0, len(result.Path))
	for i, nodeID := range result.Path {
		nodeInfo := map[string]any{
			"hop": i,
			"id":  nodeID,
		}
		if idx != nil {
			if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
				nodeInfo["name"] = sym.Name
				nodeInfo["file"] = sym.FilePath
				nodeInfo["line"] = sym.StartLine
				nodeInfo["kind"] = sym.Kind.String()
			}
		}
		path = append(path, nodeInfo)
	}
	output["path"] = path

	return output
}

// formatPathResultsText formats path results as readable text.
func formatPathResultsText(fromName, toName string, result *graph.PathResult, idx *index.SymbolIndex) string {
	var sb strings.Builder

	if result.Length < 0 {
		sb.WriteString(fmt.Sprintf("No path found between '%s' and '%s'.\n", fromName, toName))
		sb.WriteString("This could mean:\n")
		sb.WriteString("  - The symbols are not connected through call relationships\n")
		sb.WriteString("  - They exist in separate parts of the codebase\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Path from %s to %s (%d hops):\n\n", fromName, toName, result.Length))

	for i, nodeID := range result.Path {
		nodeName := nodeID
		nodeFile := ""

		if idx != nil {
			if sym, ok := idx.GetByID(nodeID); ok && sym != nil {
				nodeName = fmt.Sprintf("%s()", sym.Name)
				nodeFile = fmt.Sprintf(" (%s:%d)", sym.FilePath, sym.StartLine)
			}
		}

		sb.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, nodeName, nodeFile))

		// Add arrow except for last node
		if i < len(result.Path)-1 {
			sb.WriteString("   ↓ calls\n")
		}
	}

	return sb.String()
}

// =============================================================================
// find_important Tool (GR-13)
// =============================================================================

// findImportantTool wraps graph.GraphAnalytics.PageRankTop.
//
// Description:
//
//	Finds the most important symbols using PageRank algorithm. Unlike
//	find_hotspots (which uses degree counting), PageRank considers the
//	importance of callers: a function called by one important function
//	may rank higher than one called by many trivial helpers.
//
// GR-12/GR-13 Implementation:
//
//	Uses PageRank with power iteration. Complexity: O(k × E) where k
//	is iterations (~20 typical). Sink nodes are handled by redistributing
//	their rank evenly.
//
// Thread Safety: Safe for concurrent use. All operations are read-only.
type findImportantTool struct {
	analytics *graph.GraphAnalytics
	index     *index.SymbolIndex
}

// NewFindImportantTool creates the find_important tool.
//
// Description:
//
//	Creates a tool that finds the most important symbols using PageRank
//	algorithm. PageRank provides better importance ranking than simple
//	degree counting by considering the importance of callers transitively.
//
// Inputs:
//
//   - analytics: The GraphAnalytics instance for PageRank computation. Must not be nil.
//   - idx: The symbol index for name lookups. Must not be nil.
//
// Outputs:
//
//   - Tool: The find_important tool implementation.
//
// Limitations:
//
//   - PageRank is more expensive than degree counting O(k × E) vs O(V)
//   - Maximum 100 results per query to prevent excessive output
//   - When filtering by kind, results may be fewer than requested
//
// Assumptions:
//
//   - Graph is frozen and indexed before tool creation
//   - Analytics wraps a HierarchicalGraph
func NewFindImportantTool(analytics *graph.GraphAnalytics, idx *index.SymbolIndex) Tool {
	return &findImportantTool{
		analytics: analytics,
		index:     idx,
	}
}

func (t *findImportantTool) Name() string {
	return "find_important"
}

func (t *findImportantTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findImportantTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "find_important",
		Description: "Find the most important symbols using PageRank algorithm. " +
			"Unlike find_hotspots (which counts connections), this considers the " +
			"importance of callers. A function called by one critical function " +
			"may rank higher than one called by many trivial helpers.",
		Parameters: map[string]ParamDef{
			"top": {
				Type:        ParamTypeInt,
				Description: "Number of important symbols to return (1-100)",
				Required:    false,
				Default:     10,
			},
			"kind": {
				Type:        ParamTypeString,
				Description: "Filter by symbol kind: function, type, or all",
				Required:    false,
				Default:     "all",
				Enum:        []any{"function", "type", "all"},
			},
		},
		Category:    CategoryExploration,
		Priority:    89, // Higher than find_hotspots (86) since more sophisticated
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     30 * time.Second, // PageRank needs more time than degree counting
	}
}

func (t *findImportantTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	// Validate analytics is available
	if t.analytics == nil {
		return &Result{Success: false, Error: "graph analytics not initialized"}, nil
	}

	// Parse and validate parameters
	top := 10
	if topParam, ok := getIntParam(params, "top"); ok && topParam > 0 {
		if topParam > 100 {
			topParam = 100 // Cap at 100 to prevent excessive output
		}
		top = topParam
	}

	kindFilter, _ := params["kind"].(string)
	if kindFilter == "" {
		kindFilter = "all"
	}

	// Validate kind parameter against allowed values
	validKinds := map[string]bool{"function": true, "type": true, "all": true}
	if !validKinds[kindFilter] {
		slog.Warn("invalid kind filter, defaulting to all",
			slog.String("tool", "find_important"),
			slog.String("invalid_kind", kindFilter),
		)
		kindFilter = "all"
	}

	// Start span with context
	ctx, span := graphQueryTracer.Start(ctx, "find_important.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_important"),
			attribute.Int("top", top),
			attribute.String("kind", kindFilter),
		),
	)
	defer span.End()

	// Check context cancellation before expensive operation
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Adaptive request size based on filter
	requestCount := top
	if kindFilter != "all" {
		requestCount = top * 3 // Request 3x when filtering to ensure enough results
		slog.Debug("pagerank request adjusted for filtering",
			slog.String("tool", "find_important"),
			slog.String("kind_filter", kindFilter),
			slog.Int("top_requested", top),
			slog.Int("request_count", requestCount),
		)
	}

	// Get PageRank results using CRS-enabled method for tracing
	pageRankNodes, traceStep := t.analytics.PageRankTopWithCRS(ctx, requestCount, nil)

	span.SetAttributes(
		attribute.Int("raw_results", len(pageRankNodes)),
		attribute.String("trace_action", traceStep.Action),
	)

	// Filter by kind if needed
	var filtered []graph.PageRankNode
	if kindFilter == "all" {
		filtered = pageRankNodes
	} else {
		for _, prn := range pageRankNodes {
			if prn.Node == nil || prn.Node.Symbol == nil {
				continue
			}
			if matchesKind(prn.Node.Symbol.Kind, kindFilter) {
				filtered = append(filtered, prn)
			}
		}
	}

	// Trim to requested count
	if len(filtered) > top {
		filtered = filtered[:top]
	}

	span.SetAttributes(attribute.Int("filtered_results", len(filtered)))

	// Structured logging for edge cases
	if len(pageRankNodes) > 0 && len(filtered) == 0 {
		slog.Debug("all PageRank results filtered by kind",
			slog.String("tool", "find_important"),
			slog.Int("raw_count", len(pageRankNodes)),
			slog.String("kind_filter", kindFilter),
		)
	} else if len(filtered) < top && kindFilter != "all" {
		slog.Debug("fewer results than requested after filtering",
			slog.String("tool", "find_important"),
			slog.Int("requested", top),
			slog.Int("returned", len(filtered)),
			slog.String("kind_filter", kindFilter),
		)
	}

	// Format output
	output := formatPageRankResults(filtered)
	outputText := formatPageRankResultsText(filtered)

	return &Result{
		Success:    true,
		Output:     output,
		OutputText: outputText,
		TokensUsed: estimateTokens(outputText),
	}, nil
}

// formatPageRankResults formats PageRank results as a map.
func formatPageRankResults(nodes []graph.PageRankNode) map[string]any {
	results := make([]map[string]any, 0, len(nodes))

	for _, prn := range nodes {
		if prn.Node == nil || prn.Node.Symbol == nil {
			continue
		}
		sym := prn.Node.Symbol
		results = append(results, map[string]any{
			"rank":         prn.Rank,
			"name":         sym.Name,
			"kind":         sym.Kind.String(),
			"file":         sym.FilePath,
			"line":         sym.StartLine,
			"package":      sym.Package,
			"pagerank":     prn.Score,
			"degree_score": prn.DegreeScore,
		})
	}

	return map[string]any{
		"result_count": len(results),
		"results":      results,
		"algorithm":    "PageRank",
	}
}

// formatPageRankResultsText formats PageRank results as readable text.
func formatPageRankResultsText(nodes []graph.PageRankNode) string {
	var sb strings.Builder

	if len(nodes) == 0 {
		sb.WriteString("No important symbols found.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Top %d Most Important Symbols (PageRank):\n\n", len(nodes)))

	for _, prn := range nodes {
		if prn.Node == nil || prn.Node.Symbol == nil {
			continue
		}
		sym := prn.Node.Symbol

		// Show PageRank score and comparison to degree-based score
		sb.WriteString(fmt.Sprintf("%d. %s (PageRank: %.6f)\n", prn.Rank, sym.Name, prn.Score))
		sb.WriteString(fmt.Sprintf("   %s:%d\n", sym.FilePath, sym.StartLine))
		sb.WriteString(fmt.Sprintf("   Degree score: %d (for comparison with find_hotspots)\n", prn.DegreeScore))
		if sym.Kind != ast.SymbolKindUnknown {
			sb.WriteString(fmt.Sprintf("   Kind: %s\n", sym.Kind))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Note: PageRank considers caller importance, not just caller count.\n")

	return sb.String()
}
