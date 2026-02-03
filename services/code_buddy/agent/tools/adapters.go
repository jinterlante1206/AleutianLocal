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
	"encoding/json"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/explore"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// RegisterExploreTools registers all CB-20 explore tools with the registry.
//
// Description:
//
//	Registers adapters for the exploration tools from the explore package.
//	These tools require a graph and index to be initialized.
//
// Inputs:
//
//	registry - The tool registry
//	g - The code graph
//	idx - The symbol index
func RegisterExploreTools(registry *Registry, g *graph.Graph, idx *index.SymbolIndex) {
	registry.Register(NewFindEntryPointsTool(g, idx))
	registry.Register(NewTraceDataFlowTool(g, idx))
	registry.Register(NewTraceErrorFlowTool(g, idx))
	registry.Register(NewBuildMinimalContextTool(g, idx))
	registry.Register(NewFindSimilarCodeTool(g, idx))
	registry.Register(NewSummarizeFileTool(g, idx))
	registry.Register(NewFindConfigUsageTool(g, idx))
}

// ============================================================================
// find_entry_points Tool
// ============================================================================

// findEntryPointsTool wraps explore.EntryPointFinder.
type findEntryPointsTool struct {
	finder *explore.EntryPointFinder
}

// NewFindEntryPointsTool creates the find_entry_points tool.
func NewFindEntryPointsTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findEntryPointsTool{
		finder: explore.NewEntryPointFinder(g, idx),
	}
}

func (t *findEntryPointsTool) Name() string {
	return "find_entry_points"
}

func (t *findEntryPointsTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findEntryPointsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "find_entry_points",
		Description: "Discovers entry points in the codebase (main functions, HTTP handlers, CLI commands, tests, etc.)",
		Parameters: map[string]ParamDef{
			"type": {
				Type:        ParamTypeString,
				Description: "Type of entry point to find: 'main', 'handler', 'command', 'test', 'lambda', 'grpc', or 'all'",
				Required:    false,
				Default:     "all",
				Enum:        []any{"main", "handler", "command", "test", "lambda", "grpc", "all"},
			},
			"package": {
				Type:        ParamTypeString,
				Description: "Filter results to a specific package path",
				Required:    false,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of results to return",
				Required:    false,
				Default:     100,
			},
			"include_tests": {
				Type:        ParamTypeBool,
				Description: "Include test entry points in results",
				Required:    false,
				Default:     false,
			},
		},
		Category:    CategoryExploration,
		Priority:    90,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *findEntryPointsTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	opts := explore.DefaultEntryPointOptions()

	if typeStr, ok := params["type"].(string); ok && typeStr != "" {
		opts.Type = explore.EntryPointType(typeStr)
	}

	if pkg, ok := params["package"].(string); ok {
		opts.Package = pkg
	}

	if limit, ok := getIntParam(params, "limit"); ok {
		opts.Limit = limit
	}

	if includeTests, ok := params["include_tests"].(bool); ok {
		opts.IncludeTests = includeTests
	}

	result, err := t.finder.FindEntryPoints(ctx, opts)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// trace_data_flow Tool
// ============================================================================

// traceDataFlowTool wraps explore.DataFlowTracer.
type traceDataFlowTool struct {
	tracer *explore.DataFlowTracer
}

// NewTraceDataFlowTool creates the trace_data_flow tool.
func NewTraceDataFlowTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &traceDataFlowTool{
		tracer: explore.NewDataFlowTracer(g, idx),
	}
}

func (t *traceDataFlowTool) Name() string {
	return "trace_data_flow"
}

func (t *traceDataFlowTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *traceDataFlowTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "trace_data_flow",
		Description: "Traces data flow through function calls, identifying sources, transforms, and sinks",
		Parameters: map[string]ParamDef{
			"symbol_id": {
				Type:        ParamTypeString,
				Description: "The symbol ID to start tracing from",
				Required:    true,
			},
			"max_hops": {
				Type:        ParamTypeInt,
				Description: "Maximum depth to trace",
				Required:    false,
				Default:     5,
			},
		},
		Category:    CategoryExploration,
		Priority:    85,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     15 * time.Second,
	}
}

func (t *traceDataFlowTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	symbolID, ok := params["symbol_id"].(string)
	if !ok || symbolID == "" {
		return &Result{Success: false, Error: "symbol_id is required"}, nil
	}

	var opts []explore.ExploreOption
	if maxHops, ok := getIntParam(params, "max_hops"); ok {
		opts = append(opts, explore.WithMaxHops(maxHops))
	}

	result, err := t.tracer.TraceDataFlow(ctx, symbolID, opts...)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// trace_error_flow Tool
// ============================================================================

// traceErrorFlowTool wraps explore.ErrorFlowTracer.
type traceErrorFlowTool struct {
	tracer *explore.ErrorFlowTracer
}

// NewTraceErrorFlowTool creates the trace_error_flow tool.
func NewTraceErrorFlowTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &traceErrorFlowTool{
		tracer: explore.NewErrorFlowTracer(g, idx),
	}
}

func (t *traceErrorFlowTool) Name() string {
	return "trace_error_flow"
}

func (t *traceErrorFlowTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *traceErrorFlowTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "trace_error_flow",
		Description: "Traces error propagation paths through the codebase, identifying origins, handlers, and escapes",
		Parameters: map[string]ParamDef{
			"symbol_id": {
				Type:        ParamTypeString,
				Description: "The symbol ID to start tracing from",
				Required:    true,
			},
			"max_hops": {
				Type:        ParamTypeInt,
				Description: "Maximum depth to trace",
				Required:    false,
				Default:     5,
			},
		},
		Category:    CategoryExploration,
		Priority:    80,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     15 * time.Second,
	}
}

func (t *traceErrorFlowTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	symbolID, ok := params["symbol_id"].(string)
	if !ok || symbolID == "" {
		return &Result{Success: false, Error: "symbol_id is required"}, nil
	}

	var opts []explore.ExploreOption
	if maxHops, ok := getIntParam(params, "max_hops"); ok {
		opts = append(opts, explore.WithMaxHops(maxHops))
	}

	result, err := t.tracer.TraceErrorFlow(ctx, symbolID, opts...)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// build_minimal_context Tool
// ============================================================================

// buildMinimalContextTool wraps explore.MinimalContextBuilder.
type buildMinimalContextTool struct {
	builder *explore.MinimalContextBuilder
}

// NewBuildMinimalContextTool creates the build_minimal_context tool.
func NewBuildMinimalContextTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &buildMinimalContextTool{
		builder: explore.NewMinimalContextBuilder(g, idx),
	}
}

func (t *buildMinimalContextTool) Name() string {
	return "build_minimal_context"
}

func (t *buildMinimalContextTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *buildMinimalContextTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "build_minimal_context",
		Description: "Builds minimal context needed to understand a function, including types and key callees",
		Parameters: map[string]ParamDef{
			"symbol_id": {
				Type:        ParamTypeString,
				Description: "The symbol ID to build context for",
				Required:    true,
			},
			"token_budget": {
				Type:        ParamTypeInt,
				Description: "Maximum token budget for the context",
				Required:    false,
				Default:     4000,
			},
		},
		Category:    CategoryExploration,
		Priority:    95,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *buildMinimalContextTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	symbolID, ok := params["symbol_id"].(string)
	if !ok || symbolID == "" {
		return &Result{Success: false, Error: "symbol_id is required"}, nil
	}

	var opts []explore.ExploreOption
	if tokenBudget, ok := getIntParam(params, "token_budget"); ok {
		opts = append(opts, explore.WithTokenBudget(tokenBudget))
	}

	result, err := t.builder.BuildMinimalContext(ctx, symbolID, opts...)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// find_similar_code Tool
// ============================================================================

// findSimilarCodeTool wraps explore.SimilarityEngine.
type findSimilarCodeTool struct {
	engine *explore.SimilarityEngine
}

// NewFindSimilarCodeTool creates the find_similar_code tool.
func NewFindSimilarCodeTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findSimilarCodeTool{
		engine: explore.NewSimilarityEngine(g, idx),
	}
}

func (t *findSimilarCodeTool) Name() string {
	return "find_similar_code"
}

func (t *findSimilarCodeTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findSimilarCodeTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "find_similar_code",
		Description: "Finds code structurally or semantically similar to a target symbol",
		Parameters: map[string]ParamDef{
			"symbol_id": {
				Type:        ParamTypeString,
				Description: "The symbol ID to find similar code for",
				Required:    true,
			},
			"limit": {
				Type:        ParamTypeInt,
				Description: "Maximum number of similar code blocks to return",
				Required:    false,
				Default:     10,
			},
			"min_similarity": {
				Type:        ParamTypeFloat,
				Description: "Minimum similarity score (0.0-1.0)",
				Required:    false,
				Default:     0.5,
			},
		},
		Category:    CategoryExploration,
		Priority:    75,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     20 * time.Second,
	}
}

func (t *findSimilarCodeTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	symbolID, ok := params["symbol_id"].(string)
	if !ok || symbolID == "" {
		return &Result{Success: false, Error: "symbol_id is required"}, nil
	}

	var opts []explore.ExploreOption
	if limit, ok := getIntParam(params, "limit"); ok {
		opts = append(opts, explore.WithMaxNodes(limit))
	}

	result, err := t.engine.FindSimilarCode(ctx, symbolID, opts...)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	// Filter by minimum similarity if specified
	if minSim, ok := params["min_similarity"].(float64); ok && minSim > 0 {
		filtered := make([]explore.SimilarResult, 0)
		for _, r := range result.Results {
			if r.Similarity >= minSim {
				filtered = append(filtered, r)
			}
		}
		result.Results = filtered
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// summarize_file Tool
// ============================================================================

// summarizeFileTool wraps explore.FileSummarizer.
type summarizeFileTool struct {
	summarizer *explore.FileSummarizer
}

// NewSummarizeFileTool creates the summarize_file tool.
func NewSummarizeFileTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &summarizeFileTool{
		summarizer: explore.NewFileSummarizer(g, idx),
	}
}

func (t *summarizeFileTool) Name() string {
	return "summarize_file"
}

func (t *summarizeFileTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *summarizeFileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "summarize_file",
		Description: "Provides a structured summary of a file including types, functions, and purpose",
		Parameters: map[string]ParamDef{
			"file_path": {
				Type:        ParamTypeString,
				Description: "The relative path to the file to summarize",
				Required:    true,
			},
		},
		Category:    CategoryExploration,
		Priority:    70,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *summarizeFileTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	filePath, ok := params["file_path"].(string)
	if !ok || filePath == "" {
		return &Result{Success: false, Error: "file_path is required"}, nil
	}

	result, err := t.summarizer.SummarizeFile(ctx, filePath)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// find_config_usage Tool
// ============================================================================

// findConfigUsageTool wraps explore.ConfigFinder.
type findConfigUsageTool struct {
	finder *explore.ConfigFinder
}

// NewFindConfigUsageTool creates the find_config_usage tool.
func NewFindConfigUsageTool(g *graph.Graph, idx *index.SymbolIndex) Tool {
	return &findConfigUsageTool{
		finder: explore.NewConfigFinder(g, idx),
	}
}

func (t *findConfigUsageTool) Name() string {
	return "find_config_usage"
}

func (t *findConfigUsageTool) Category() ToolCategory {
	return CategoryExploration
}

func (t *findConfigUsageTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "find_config_usage",
		Description: "Finds where configuration values are used in the codebase",
		Parameters: map[string]ParamDef{
			"config_key": {
				Type:        ParamTypeString,
				Description: "The configuration key or pattern to search for",
				Required:    true,
			},
		},
		Category:    CategoryExploration,
		Priority:    65,
		Requires:    []string{"graph_initialized"},
		SideEffects: false,
		Timeout:     10 * time.Second,
	}
}

func (t *findConfigUsageTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	configKey, ok := params["config_key"].(string)
	if !ok || configKey == "" {
		return &Result{Success: false, Error: "config_key is required"}, nil
	}

	result, err := t.finder.FindConfigUsage(ctx, configKey)
	if err != nil {
		return &Result{Success: false, Error: err.Error()}, nil
	}

	output, _ := json.Marshal(result)
	return &Result{
		Success:    true,
		Output:     result,
		OutputText: string(output),
		TokensUsed: estimateTokens(string(output)),
	}, nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// estimateTokens estimates the token count for a string.
// Approximation: ~4 characters per token for code.
func estimateTokens(s string) int {
	return len(s) / 4
}

// getIntParam extracts an int parameter from the params map.
// Handles both int and float64 (JSON unmarshaling produces float64).
func getIntParam(params map[string]any, key string) (int, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}

	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}

// ============================================================================
// Mock Tool for Testing
// ============================================================================

// MockTool is a simple tool implementation for testing.
type MockTool struct {
	name        string
	category    ToolCategory
	definition  ToolDefinition
	ExecuteFunc func(ctx context.Context, params map[string]any) (*Result, error)
}

// NewMockTool creates a mock tool for testing.
func NewMockTool(name string, category ToolCategory) *MockTool {
	return &MockTool{
		name:     name,
		category: category,
		definition: ToolDefinition{
			Name:        name,
			Description: fmt.Sprintf("Mock tool: %s", name),
			Category:    category,
			Parameters:  make(map[string]ParamDef),
		},
		ExecuteFunc: func(ctx context.Context, params map[string]any) (*Result, error) {
			return &Result{
				Success:    true,
				OutputText: fmt.Sprintf("Mock result from %s", name),
			}, nil
		},
	}
}

func (t *MockTool) Name() string               { return t.name }
func (t *MockTool) Category() ToolCategory     { return t.category }
func (t *MockTool) Definition() ToolDefinition { return t.definition }
func (t *MockTool) WithDefinition(d ToolDefinition) *MockTool {
	t.definition = d
	return t
}

func (t *MockTool) Execute(ctx context.Context, params map[string]any) (*Result, error) {
	return t.ExecuteFunc(ctx, params)
}

// ============================================================================
// Static Tool Definitions for Classification
// ============================================================================

// StaticToolDefinitions returns tool definitions without needing a graph.
//
// Description:
//
//	Returns the definitions for all exploration tools. These can be used
//	for query classification without initializing the full tool system.
//	The definitions include tool names, descriptions, and parameter schemas.
//
// Outputs:
//
//	[]ToolDefinition - The static tool definitions.
//
// Example:
//
//	defs := tools.StaticToolDefinitions()
//	classifier, _ := classifier.NewLLMClassifier(client, defs, config)
//
// Thread Safety: This function is safe for concurrent use.
func StaticToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "find_entry_points",
			Description: "Discovers entry points in the codebase (main functions, HTTP handlers, CLI commands, tests, etc.)",
			Parameters: map[string]ParamDef{
				"type": {
					Type:        ParamTypeString,
					Description: "Type of entry point to find: 'main', 'handler', 'command', 'test', 'lambda', 'grpc', or 'all'",
					Required:    false,
					Default:     "all",
					Enum:        []any{"main", "handler", "command", "test", "lambda", "grpc", "all"},
				},
				"package": {
					Type:        ParamTypeString,
					Description: "Filter results to a specific package path",
					Required:    false,
				},
				"limit": {
					Type:        ParamTypeInt,
					Description: "Maximum number of results to return",
					Required:    false,
					Default:     100,
				},
				"include_tests": {
					Type:        ParamTypeBool,
					Description: "Include test entry points in results",
					Required:    false,
					Default:     false,
				},
			},
			Category:    CategoryExploration,
			Priority:    90,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     10 * time.Second,
		},
		{
			Name:        "trace_data_flow",
			Description: "Traces data flow through function calls, identifying sources, transforms, and sinks",
			Parameters: map[string]ParamDef{
				"symbol_id": {
					Type:        ParamTypeString,
					Description: "The symbol ID to start tracing from",
					Required:    true,
				},
				"max_hops": {
					Type:        ParamTypeInt,
					Description: "Maximum depth to trace",
					Required:    false,
					Default:     5,
				},
			},
			Category:    CategoryExploration,
			Priority:    85,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     15 * time.Second,
		},
		{
			Name:        "trace_error_flow",
			Description: "Traces error propagation paths to understand how errors are handled and escalated",
			Parameters: map[string]ParamDef{
				"symbol_id": {
					Type:        ParamTypeString,
					Description: "The symbol ID to start tracing from",
					Required:    true,
				},
				"max_depth": {
					Type:        ParamTypeInt,
					Description: "Maximum depth to trace",
					Required:    false,
					Default:     5,
				},
			},
			Category:    CategoryExploration,
			Priority:    80,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     15 * time.Second,
		},
		{
			Name:        "build_minimal_context",
			Description: "Builds minimal context required to understand a symbol, filtering by relevance",
			Parameters: map[string]ParamDef{
				"symbol_id": {
					Type:        ParamTypeString,
					Description: "The symbol ID to build context for",
					Required:    true,
				},
				"max_tokens": {
					Type:        ParamTypeInt,
					Description: "Maximum tokens for context",
					Required:    false,
					Default:     4000,
				},
			},
			Category:    CategoryExploration,
			Priority:    75,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     10 * time.Second,
		},
		{
			Name:        "find_similar_code",
			Description: "Finds code similar to a given symbol or pattern",
			Parameters: map[string]ParamDef{
				"symbol_id": {
					Type:        ParamTypeString,
					Description: "The symbol ID to find similar code for",
					Required:    true,
				},
				"threshold": {
					Type:        ParamTypeFloat,
					Description: "Similarity threshold (0.0-1.0)",
					Required:    false,
					Default:     0.7,
				},
				"limit": {
					Type:        ParamTypeInt,
					Description: "Maximum number of results",
					Required:    false,
					Default:     20,
				},
			},
			Category:    CategoryExploration,
			Priority:    70,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     10 * time.Second,
		},
		{
			Name:        "summarize_file",
			Description: "Generates a summary of a file's contents and structure",
			Parameters: map[string]ParamDef{
				"path": {
					Type:        ParamTypeString,
					Description: "The file path to summarize",
					Required:    true,
				},
			},
			Category:    CategoryExploration,
			Priority:    65,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     10 * time.Second,
		},
		{
			Name:        "find_config_usage",
			Description: "Finds where configuration values are defined and used",
			Parameters: map[string]ParamDef{
				"pattern": {
					Type:        ParamTypeString,
					Description: "Pattern to search for in config names",
					Required:    false,
				},
				"include_env": {
					Type:        ParamTypeBool,
					Description: "Include environment variable references",
					Required:    false,
					Default:     true,
				},
			},
			Category:    CategoryExploration,
			Priority:    60,
			Requires:    []string{"graph_initialized"},
			SideEffects: false,
			Timeout:     10 * time.Second,
		},
	}
}
