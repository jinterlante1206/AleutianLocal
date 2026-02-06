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
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
// Thread Safety: Safe for concurrent use (graph queries are read-only).
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
	ctx, span := graphQueryTracer.Start(ctx, "find_callers.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_callers"),
		),
	)
	defer span.End()

	name, _ := params["function_name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "function_name is required"}, nil
	}

	span.SetAttributes(attribute.String("function_name", name))

	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		limit = l
	}
	span.SetAttributes(attribute.Int("limit", limit))

	results, err := t.graph.FindCallersByName(ctx, name, graph.WithLimit(limit))
	if err != nil {
		span.RecordError(err)
		return &Result{Success: false, Error: fmt.Sprintf("graph query failed: %v", err)}, nil
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
	ctx, span := graphQueryTracer.Start(ctx, "find_callees.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_callees"),
		),
	)
	defer span.End()

	name, _ := params["function_name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "function_name is required"}, nil
	}

	span.SetAttributes(attribute.String("function_name", name))

	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		limit = l
	}

	results, err := t.graph.FindCalleesByName(ctx, name, graph.WithLimit(limit))
	if err != nil {
		span.RecordError(err)
		return &Result{Success: false, Error: fmt.Sprintf("graph query failed: %v", err)}, nil
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
	ctx, span := graphQueryTracer.Start(ctx, "find_implementations.Execute",
		trace.WithAttributes(
			attribute.String("tool", "find_implementations"),
		),
	)
	defer span.End()

	name, _ := params["interface_name"].(string)
	if name == "" {
		span.SetAttributes(attribute.Bool("error", true))
		return &Result{Success: false, Error: "interface_name is required"}, nil
	}

	span.SetAttributes(attribute.String("interface_name", name))

	limit := 50
	if l, ok := getIntParam(params, "limit"); ok && l > 0 {
		limit = l
	}

	results, err := t.graph.FindImplementationsByName(ctx, name, graph.WithLimit(limit))
	if err != nil {
		span.RecordError(err)
		return &Result{Success: false, Error: fmt.Sprintf("graph query failed: %v", err)}, nil
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
func formatCalleeResults(name string, results map[string]*graph.QueryResult) map[string]any {
	output := map[string]any{
		"function_name": name,
		"match_count":   len(results),
	}

	var callees []map[string]any
	for symbolID, result := range results {
		if result == nil {
			continue
		}

		entry := map[string]any{
			"source_id":    symbolID,
			"callee_count": len(result.Symbols),
		}

		var calleeList []map[string]any
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			calleeList = append(calleeList, map[string]any{
				"name":      sym.Name,
				"file":      sym.FilePath,
				"line":      sym.StartLine,
				"package":   sym.Package,
				"signature": sym.Signature,
			})
		}
		entry["callees"] = calleeList
		callees = append(callees, entry)
	}
	output["results"] = callees

	return output
}

// formatCalleeResultsText formats callee results as readable text.
func formatCalleeResultsText(name string, results map[string]*graph.QueryResult) string {
	var sb strings.Builder

	totalCallees := countTotalCallers(results)
	if totalCallees == 0 {
		sb.WriteString(fmt.Sprintf("No callees found for function '%s'.\n", name))
		sb.WriteString("This could mean:\n")
		sb.WriteString("  - The function doesn't call any other functions\n")
		sb.WriteString("  - The function name doesn't exist in the codebase\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Function '%s' calls %d functions:\n\n", name, totalCallees))

	for symbolID, result := range results {
		if result == nil || len(result.Symbols) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("From: %s\n", symbolID))
		for _, sym := range result.Symbols {
			if sym == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("  → %s() in %s:%d\n", sym.Name, sym.FilePath, sym.StartLine))
		}
		sb.WriteString("\n")
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
