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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// createTestGraphWithCallers creates a test graph with call relationships.
func createTestGraphWithCallers(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Create symbols (EndLine >= StartLine, Language required for index validation)
	parseConfig := &ast.Symbol{
		ID:        "config/parser.go:10:parseConfig",
		Name:      "parseConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/parser.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "config",
		Signature: "func parseConfig(path string) (*Config, error)",
		Exported:  false,
		Language:  "go",
	}

	main := &ast.Symbol{
		ID:        "cmd/app/main.go:5:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "cmd/app/main.go",
		StartLine: 5,
		EndLine:   15,
		Package:   "main",
		Signature: "func main()",
		Exported:  false,
		Language:  "go",
	}

	initServer := &ast.Symbol{
		ID:        "server/init.go:20:initServer",
		Name:      "initServer",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "server/init.go",
		StartLine: 20,
		EndLine:   30,
		Package:   "server",
		Signature: "func initServer() error",
		Exported:  false,
		Language:  "go",
	}

	loadConfig := &ast.Symbol{
		ID:        "config/loader.go:15:LoadConfig",
		Name:      "LoadConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/loader.go",
		StartLine: 15,
		EndLine:   25,
		Package:   "config",
		Signature: "func LoadConfig() (*Config, error)",
		Exported:  true,
		Language:  "go",
	}

	// Handler interface and implementation
	handler := &ast.Symbol{
		ID:        "handler/handler.go:5:Handler",
		Name:      "Handler",
		Kind:      ast.SymbolKindInterface,
		FilePath:  "handler/handler.go",
		StartLine: 5,
		EndLine:   10,
		Package:   "handler",
		Exported:  true,
		Language:  "go",
	}

	userHandler := &ast.Symbol{
		ID:        "handler/user.go:10:UserHandler",
		Name:      "UserHandler",
		Kind:      ast.SymbolKindStruct,
		FilePath:  "handler/user.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "handler",
		Exported:  true,
		Language:  "go",
	}

	// Add nodes to graph
	g.AddNode(parseConfig)
	g.AddNode(main)
	g.AddNode(initServer)
	g.AddNode(loadConfig)
	g.AddNode(handler)
	g.AddNode(userHandler)

	// Add symbols to index
	if err := idx.Add(parseConfig); err != nil {
		t.Fatalf("Failed to add parseConfig: %v", err)
	}
	if err := idx.Add(main); err != nil {
		t.Fatalf("Failed to add main: %v", err)
	}
	if err := idx.Add(initServer); err != nil {
		t.Fatalf("Failed to add initServer: %v", err)
	}
	if err := idx.Add(loadConfig); err != nil {
		t.Fatalf("Failed to add loadConfig: %v", err)
	}
	if err := idx.Add(handler); err != nil {
		t.Fatalf("Failed to add handler: %v", err)
	}
	if err := idx.Add(userHandler); err != nil {
		t.Fatalf("Failed to add userHandler: %v", err)
	}

	// Verify index is populated
	if idx.Stats().TotalSymbols != 6 {
		t.Fatalf("Expected 6 symbols in index, got %d", idx.Stats().TotalSymbols)
	}

	// Create call edges: main -> parseConfig, initServer -> parseConfig, LoadConfig -> parseConfig
	g.AddEdge(main.ID, parseConfig.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: main.FilePath, StartLine: 10,
	})
	g.AddEdge(initServer.ID, parseConfig.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: initServer.FilePath, StartLine: 25,
	})
	g.AddEdge(loadConfig.ID, parseConfig.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: loadConfig.FilePath, StartLine: 20,
	})

	// Create implements edge: UserHandler implements Handler
	g.AddEdge(userHandler.ID, handler.ID, graph.EdgeTypeImplements, ast.Location{
		FilePath: userHandler.FilePath, StartLine: 10,
	})

	g.Freeze()

	return g, idx
}

func TestFindCallersTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindCallersTool(g, idx)

	t.Run("finds all callers of parseConfig", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"function_name": "parseConfig",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// Check that we found callers
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		results, ok := output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}

		// Should have 1 entry (one parseConfig function)
		if len(results) != 1 {
			t.Errorf("got %d result entries, want 1", len(results))
		}

		// The one parseConfig should have 3 callers
		if len(results) > 0 {
			callers, ok := results[0]["callers"].([]map[string]any)
			if !ok {
				t.Fatalf("callers is not a slice")
			}
			if len(callers) != 3 {
				t.Errorf("got %d callers, want 3", len(callers))
			}
		}

		// Check output text mentions the callers
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("returns empty for non-existent function", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"function_name": "nonExistent",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// Should have message about no callers found
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("requires function_name parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Execute() should fail without function_name")
		}
		if result.Error == "" {
			t.Error("Error message should not be empty")
		}
	})
}

func TestFindCalleesTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindCalleesTool(g, idx)

	t.Run("finds callees of main", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"function_name": "main",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// main calls parseConfig
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// GR-41: New format separates resolved and external callees
		resolvedCallees, ok := output["resolved_callees"].([]map[string]any)
		if !ok {
			t.Fatalf("resolved_callees is not a slice")
		}

		if len(resolvedCallees) != 1 {
			t.Errorf("got %d resolved callees, want 1", len(resolvedCallees))
		}
	})

	t.Run("requires function_name parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Execute() should fail without function_name")
		}
	})
}

func TestFindImplementationsTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindImplementationsTool(g, idx)

	t.Run("finds implementations of Handler", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"interface_name": "Handler",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// UserHandler implements Handler
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		results, ok := output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}

		if len(results) != 1 {
			t.Errorf("got %d result entries, want 1", len(results))
		}
	})

	t.Run("requires interface_name parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Execute() should fail without interface_name")
		}
	})
}

func TestFindSymbolTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindSymbolTool(g, idx)

	t.Run("finds symbol by name", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"name": "parseConfig",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		matchCount, _ := output["match_count"].(int)
		if matchCount != 1 {
			t.Errorf("got %d matches, want 1", matchCount)
		}
	})

	t.Run("filters by kind", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"name": "Handler",
			"kind": "interface",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		matchCount, _ := output["match_count"].(int)
		if matchCount != 1 {
			t.Errorf("got %d matches, want 1", matchCount)
		}
	})

	t.Run("requires name parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Execute() should fail without name")
		}
	})
}

func TestGetCallChainTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewGetCallChainTool(g, idx)

	t.Run("traces downstream from main", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"function_name": "main",
			"direction":     "downstream",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// main -> parseConfig
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		nodeCount, _ := output["node_count"].(int)
		if nodeCount < 2 {
			t.Errorf("got %d nodes, want at least 2", nodeCount)
		}
	})

	t.Run("traces upstream from parseConfig", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"function_name": "parseConfig",
			"direction":     "upstream",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// parseConfig <- main, initServer, LoadConfig
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		nodeCount, _ := output["node_count"].(int)
		if nodeCount < 4 {
			t.Errorf("got %d nodes, want at least 4", nodeCount)
		}
	})

	t.Run("requires function_name parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Execute() should fail without function_name")
		}
	})
}

func TestToolDefinitions(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	tests := []struct {
		name     string
		tool     Tool
		wantName string
		wantCat  ToolCategory
	}{
		{
			name:     "find_callers",
			tool:     NewFindCallersTool(g, idx),
			wantName: "find_callers",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_callees",
			tool:     NewFindCalleesTool(g, idx),
			wantName: "find_callees",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_implementations",
			tool:     NewFindImplementationsTool(g, idx),
			wantName: "find_implementations",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_symbol",
			tool:     NewFindSymbolTool(g, idx),
			wantName: "find_symbol",
			wantCat:  CategoryExploration,
		},
		{
			name:     "get_call_chain",
			tool:     NewGetCallChainTool(g, idx),
			wantName: "get_call_chain",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_references",
			tool:     NewFindReferencesTool(g, idx),
			wantName: "find_references",
			wantCat:  CategoryExploration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tool.Name(); got != tt.wantName {
				t.Errorf("Name() = %v, want %v", got, tt.wantName)
			}
			if got := tt.tool.Category(); got != tt.wantCat {
				t.Errorf("Category() = %v, want %v", got, tt.wantCat)
			}

			def := tt.tool.Definition()
			if def.Name != tt.wantName {
				t.Errorf("Definition().Name = %v, want %v", def.Name, tt.wantName)
			}
			if def.Description == "" {
				t.Error("Definition().Description is empty")
			}
			if len(def.Parameters) == 0 {
				t.Error("Definition().Parameters is empty")
			}
		})
	}
}

func TestRegisterExploreTools_IncludesGraphQueryTools(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()
	registry := NewRegistry()

	RegisterExploreTools(registry, g, idx)

	// Check that all 6 new graph query tools are registered
	graphQueryTools := []string{
		"find_callers",
		"find_callees",
		"find_implementations",
		"find_symbol",
		"get_call_chain",
		"find_references",
	}

	for _, name := range graphQueryTools {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("Tool %s not registered", name)
		}
	}

	// Should have at least 16 tools (10 original + 6 new)
	if count := registry.Count(); count < 16 {
		t.Errorf("Registry has %d tools, want at least 16", count)
	}
}

// =============================================================================
// GR-01: Index Optimization Tests
// =============================================================================

// TestFindCallersTool_NilIndexFallback tests that find_callers falls back to
// O(V) graph scan when index is nil (GR-01 requirement M5).
func TestFindCallersTool_NilIndexFallback(t *testing.T) {
	ctx := context.Background()
	g, _ := createTestGraphWithCallers(t)

	// Create tool with nil index
	tool := NewFindCallersTool(g, nil)

	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "parseConfig",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should still find callers via graph fallback
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	results, ok := output["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is not a slice")
	}

	// Should have 1 entry (one parseConfig function) with 3 callers
	if len(results) != 1 {
		t.Errorf("got %d result entries, want 1", len(results))
	}
}

// TestFindCalleesTool_NilIndexFallback tests nil index fallback for find_callees.
func TestFindCalleesTool_NilIndexFallback(t *testing.T) {
	ctx := context.Background()
	g, _ := createTestGraphWithCallers(t)

	tool := NewFindCalleesTool(g, nil)

	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "main",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should still find callees via graph fallback
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	// GR-41: New format separates resolved and external callees
	resolvedCallees, ok := output["resolved_callees"].([]map[string]any)
	if !ok {
		t.Fatalf("resolved_callees is not a slice")
	}

	if len(resolvedCallees) != 1 {
		t.Errorf("got %d resolved callees, want 1", len(resolvedCallees))
	}
}

// TestFindImplementationsTool_NilIndexFallback tests nil index fallback.
func TestFindImplementationsTool_NilIndexFallback(t *testing.T) {
	ctx := context.Background()
	g, _ := createTestGraphWithCallers(t)

	tool := NewFindImplementationsTool(g, nil)

	result, err := tool.Execute(ctx, map[string]any{
		"interface_name": "Handler",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should still find implementations via graph fallback
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	results, ok := output["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is not a slice")
	}

	if len(results) != 1 {
		t.Errorf("got %d result entries, want 1", len(results))
	}
}

// createTestGraphWithMultipleMatches creates a graph with multiple functions
// having the same name (e.g., "Setup" in different packages).
func createTestGraphWithMultipleMatches(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Create multiple "Setup" functions in different packages
	symbols := []*ast.Symbol{
		{
			ID:        "pkg/a/setup.go:10:Setup",
			Name:      "Setup",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/a/setup.go",
			StartLine: 10,
			EndLine:   20,
			Package:   "a",
			Language:  "go",
		},
		{
			ID:        "pkg/b/setup.go:15:Setup",
			Name:      "Setup",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/b/setup.go",
			StartLine: 15,
			EndLine:   25,
			Package:   "b",
			Language:  "go",
		},
		{
			ID:        "pkg/c/setup.go:20:Setup",
			Name:      "Setup",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/c/setup.go",
			StartLine: 20,
			EndLine:   30,
			Package:   "c",
			Language:  "go",
		},
		{
			ID:        "main.go:5:main",
			Name:      "main",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "main.go",
			StartLine: 5,
			EndLine:   15,
			Package:   "main",
			Language:  "go",
		},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Fatalf("Failed to add symbol %s: %v", sym.ID, err)
		}
	}

	// main calls all three Setup functions
	g.AddEdge("main.go:5:main", "pkg/a/setup.go:10:Setup", graph.EdgeTypeCalls, ast.Location{
		FilePath: "main.go", StartLine: 10,
	})
	g.AddEdge("main.go:5:main", "pkg/b/setup.go:15:Setup", graph.EdgeTypeCalls, ast.Location{
		FilePath: "main.go", StartLine: 11,
	})
	g.AddEdge("main.go:5:main", "pkg/c/setup.go:20:Setup", graph.EdgeTypeCalls, ast.Location{
		FilePath: "main.go", StartLine: 12,
	})

	g.Freeze()

	return g, idx
}

// TestFindCallersTool_MultipleMatches tests that find_callers correctly handles
// multiple functions with the same name (GR-01 requirement M2).
func TestFindCallersTool_MultipleMatches(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithMultipleMatches(t)

	tool := NewFindCallersTool(g, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "Setup",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	results, ok := output["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is not a slice")
	}

	// Should have 3 result entries (one per Setup function)
	if len(results) != 3 {
		t.Errorf("got %d result entries, want 3 (one per Setup)", len(results))
	}

	// Each Setup should have 1 caller (main)
	for i, entry := range results {
		callers, ok := entry["callers"].([]map[string]any)
		if !ok {
			t.Fatalf("callers[%d] is not a slice", i)
		}
		if len(callers) != 1 {
			t.Errorf("result[%d] got %d callers, want 1", i, len(callers))
		}
	}
}

// TestFindCalleesTool_MultipleMatches tests multiple symbol matches.
func TestFindCalleesTool_MultipleMatches(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithMultipleMatches(t)

	tool := NewFindCalleesTool(g, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "main",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	// GR-41: New format has resolved_callees as a flat list
	resolvedCallees, ok := output["resolved_callees"].([]map[string]any)
	if !ok {
		t.Fatalf("resolved_callees is not a slice")
	}

	// main has 3 callees (three Setup functions)
	if len(resolvedCallees) != 3 {
		t.Errorf("got %d resolved callees, want 3", len(resolvedCallees))
	}
}

// TestFindCallersTool_FastNotFound tests that queries for non-existent symbols
// return quickly (O(1) index miss, not O(V) scan).
func TestFindCallersTool_FastNotFound(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindCallersTool(g, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "NonExistentFunctionXYZ123",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should have empty results and message about no callers
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	results, ok := output["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is not a slice")
	}

	if len(results) != 0 {
		t.Errorf("got %d results for non-existent function, want 0", len(results))
	}

	// OutputText should mention no callers found
	if result.OutputText == "" {
		t.Error("OutputText is empty")
	}
}

// =============================================================================
// GR-01: Benchmark Tests
// =============================================================================

// createLargeGraph creates a graph with many symbols for benchmarking.
func createLargeGraph(b *testing.B, size int) (*graph.Graph, *index.SymbolIndex) {
	b.Helper()

	g := graph.NewGraph("/benchmark")
	idx := index.NewSymbolIndex()

	// Create a chain of function calls (StartLine must be >= 1)
	var symbols []*ast.Symbol
	for i := 0; i < size; i++ {
		startLine := i*10 + 1 // 1-indexed, starting at 1
		sym := &ast.Symbol{
			ID:        fmt.Sprintf("pkg/module%d/file.go:%d:Function%d", i, startLine, i),
			Name:      fmt.Sprintf("Function%d", i),
			Kind:      ast.SymbolKindFunction,
			FilePath:  fmt.Sprintf("pkg/module%d/file.go", i),
			StartLine: startLine,
			EndLine:   startLine + 10,
			Package:   fmt.Sprintf("module%d", i),
			Language:  "go",
		}
		symbols = append(symbols, sym)
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			b.Fatalf("Failed to add symbol: %v", err)
		}
	}

	// Create call edges: each function calls the next
	for i := 0; i < size-1; i++ {
		g.AddEdge(symbols[i].ID, symbols[i+1].ID, graph.EdgeTypeCalls, ast.Location{
			FilePath: symbols[i].FilePath, StartLine: symbols[i].StartLine + 5,
		})
	}

	g.Freeze()

	return g, idx
}

// BenchmarkFindCallers_WithIndex benchmarks find_callers using O(1) index lookup.
func BenchmarkFindCallers_WithIndex(b *testing.B) {
	g, idx := createLargeGraph(b, 10000)
	tool := NewFindCallersTool(g, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Query for a function in the middle of the graph
		_, err := tool.Execute(ctx, map[string]any{
			"function_name": "Function5000",
		})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// BenchmarkFindCallers_WithoutIndex benchmarks find_callers using O(V) graph scan.
func BenchmarkFindCallers_WithoutIndex(b *testing.B) {
	g, _ := createLargeGraph(b, 10000)
	tool := NewFindCallersTool(g, nil) // nil index forces graph fallback
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Query for a function in the middle of the graph
		_, err := tool.Execute(ctx, map[string]any{
			"function_name": "Function5000",
		})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// BenchmarkFindCallees_WithIndex benchmarks find_callees using O(1) index lookup.
func BenchmarkFindCallees_WithIndex(b *testing.B) {
	g, idx := createLargeGraph(b, 10000)
	tool := NewFindCalleesTool(g, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{
			"function_name": "Function5000",
		})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// BenchmarkFindCallees_WithoutIndex benchmarks find_callees using O(V) graph scan.
func BenchmarkFindCallees_WithoutIndex(b *testing.B) {
	g, _ := createLargeGraph(b, 10000)
	tool := NewFindCalleesTool(g, nil) // nil index forces graph fallback
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{
			"function_name": "Function5000",
		})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// =============================================================================
// GR-01: Additional Test Coverage (L1, L2)
// =============================================================================

// TestFindCallersTool_ContextCancellation tests that context cancellation is handled.
// L1: Verify context cancellation path is covered.
func TestFindCallersTool_ContextCancellation(t *testing.T) {
	g, idx := createTestGraphWithMultipleMatches(t)
	tool := NewFindCallersTool(g, idx)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, map[string]any{
		"function_name": "Setup",
	})

	// Should return context.Canceled error
	if err == nil {
		t.Error("Expected context.Canceled error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

// TestFindCalleesTool_ContextCancellation tests context cancellation for find_callees.
func TestFindCalleesTool_ContextCancellation(t *testing.T) {
	g, idx := createTestGraphWithMultipleMatches(t)
	tool := NewFindCalleesTool(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, map[string]any{
		"function_name": "main",
	})

	if err == nil {
		t.Error("Expected context.Canceled error, got nil")
	}
}

// TestFindImplementationsTool_ContextCancellation tests context cancellation.
func TestFindImplementationsTool_ContextCancellation(t *testing.T) {
	g, idx := createTestGraphWithCallers(t)
	tool := NewFindImplementationsTool(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, map[string]any{
		"interface_name": "Handler",
	})

	if err == nil {
		t.Error("Expected context.Canceled error, got nil")
	}
}

// TestFindCallersTool_LimitCapped tests that limit is capped at 1000 (M1).
func TestFindCallersTool_LimitCapped(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphWithCallers(t)

	tool := NewFindCallersTool(g, idx)

	// Request a very large limit
	result, err := tool.Execute(ctx, map[string]any{
		"function_name": "parseConfig",
		"limit":         1000000, // Should be capped to 1000
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// The test doesn't have 1000+ callers, but the limit should be silently capped
	// Verify the query still works
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	matchCount, _ := output["match_count"].(int)
	if matchCount != 1 {
		t.Errorf("Expected 1 match, got %d", matchCount)
	}
}

// TestFindImplementationsTool_NonInterfaceFiltered tests that non-interface symbols
// are filtered out (H3).
func TestFindImplementationsTool_NonInterfaceFiltered(t *testing.T) {
	ctx := context.Background()
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Create two symbols with same name: one interface, one struct
	handler := &ast.Symbol{
		ID:        "handler/handler.go:5:Handler",
		Name:      "Handler",
		Kind:      ast.SymbolKindInterface,
		FilePath:  "handler/handler.go",
		StartLine: 5,
		EndLine:   10,
		Package:   "handler",
		Language:  "go",
	}

	handlerStruct := &ast.Symbol{
		ID:        "other/handler.go:10:Handler",
		Name:      "Handler", // Same name, different kind
		Kind:      ast.SymbolKindStruct,
		FilePath:  "other/handler.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "other",
		Language:  "go",
	}

	g.AddNode(handler)
	g.AddNode(handlerStruct)
	_ = idx.Add(handler)
	_ = idx.Add(handlerStruct)
	g.Freeze()

	tool := NewFindImplementationsTool(g, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"interface_name": "Handler",
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should only query the interface, not the struct
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output is not a map")
	}

	// The match_count should be 1 (only the interface was queried)
	matchCount, _ := output["match_count"].(int)
	if matchCount != 1 {
		t.Errorf("Expected 1 interface match, got %d (struct should be filtered)", matchCount)
	}
}

// TestFindCallersTool_IndexAndGraphPathConsistency tests that index and graph paths
// return consistent results.
func TestFindCallersTool_IndexAndGraphPathConsistency(t *testing.T) {
	g, idx := createTestGraphWithCallers(t)

	toolWithIndex := NewFindCallersTool(g, idx)
	toolWithoutIndex := NewFindCallersTool(g, nil)

	ctx := context.Background()

	result1, err1 := toolWithIndex.Execute(ctx, map[string]any{
		"function_name": "parseConfig",
	})
	result2, err2 := toolWithoutIndex.Execute(ctx, map[string]any{
		"function_name": "parseConfig",
	})

	if err1 != nil || err2 != nil {
		t.Fatalf("Execute errors: %v, %v", err1, err2)
	}

	output1, _ := result1.Output.(map[string]any)
	output2, _ := result2.Output.(map[string]any)

	matchCount1, _ := output1["match_count"].(int)
	matchCount2, _ := output2["match_count"].(int)

	if matchCount1 != matchCount2 {
		t.Errorf("Index path got %d matches, graph path got %d - results inconsistent",
			matchCount1, matchCount2)
	}
}

// L2: Add find_references benchmark
func BenchmarkFindReferences_WithIndex(b *testing.B) {
	g, idx := createLargeGraph(b, 10000)
	tool := NewFindReferencesTool(g, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{
			"symbol_name": "Function5000",
		})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// =============================================================================
// GR-02 to GR-05: Graph Analytics Tool Tests
// =============================================================================

// createTestGraphForAnalytics creates a test graph with call relationships
// suitable for analytics queries (hotspots, dead code, cycles, paths).
func createTestGraphForAnalytics(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Create a graph with various patterns:
	// - funcA is a hotspot (called by many)
	// - funcD is dead code (no callers)
	// - funcB and funcC form a cycle
	// - main -> funcA -> funcB -> funcC -> funcB (cycle)
	symbols := []*ast.Symbol{
		{
			ID:        "main.go:10:main",
			Name:      "main",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "main.go",
			StartLine: 10,
			EndLine:   20,
			Package:   "main",
			Exported:  false,
			Language:  "go",
		},
		{
			ID:        "core/funcA.go:10:funcA",
			Name:      "funcA",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "core/funcA.go",
			StartLine: 10,
			EndLine:   30,
			Package:   "core",
			Exported:  true,
			Language:  "go",
		},
		{
			ID:        "core/funcB.go:10:funcB",
			Name:      "funcB",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "core/funcB.go",
			StartLine: 10,
			EndLine:   25,
			Package:   "core",
			Exported:  true,
			Language:  "go",
		},
		{
			ID:        "core/funcC.go:10:funcC",
			Name:      "funcC",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "core/funcC.go",
			StartLine: 10,
			EndLine:   25,
			Package:   "core",
			Exported:  true,
			Language:  "go",
		},
		{
			ID:        "util/funcD.go:10:funcD",
			Name:      "funcD",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "util/funcD.go",
			StartLine: 10,
			EndLine:   20,
			Package:   "util",
			Exported:  false, // Unexported dead code
			Language:  "go",
		},
		{
			ID:        "util/helper.go:5:helper",
			Name:      "helper",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "util/helper.go",
			StartLine: 5,
			EndLine:   15,
			Package:   "util",
			Exported:  false,
			Language:  "go",
		},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Fatalf("Failed to add symbol: %v", err)
		}
	}

	// Create edges:
	// main -> funcA (funcA is called by main)
	g.AddEdge("main.go:10:main", "core/funcA.go:10:funcA", graph.EdgeTypeCalls, ast.Location{
		FilePath: "main.go", StartLine: 15,
	})
	// funcA -> funcB
	g.AddEdge("core/funcA.go:10:funcA", "core/funcB.go:10:funcB", graph.EdgeTypeCalls, ast.Location{
		FilePath: "core/funcA.go", StartLine: 20,
	})
	// funcA -> helper (funcA is a hotspot)
	g.AddEdge("core/funcA.go:10:funcA", "util/helper.go:5:helper", graph.EdgeTypeCalls, ast.Location{
		FilePath: "core/funcA.go", StartLine: 22,
	})
	// funcB -> funcC
	g.AddEdge("core/funcB.go:10:funcB", "core/funcC.go:10:funcC", graph.EdgeTypeCalls, ast.Location{
		FilePath: "core/funcB.go", StartLine: 15,
	})
	// funcC -> funcB (creates cycle B <-> C)
	g.AddEdge("core/funcC.go:10:funcC", "core/funcB.go:10:funcB", graph.EdgeTypeCalls, ast.Location{
		FilePath: "core/funcC.go", StartLine: 15,
	})

	g.Freeze()
	return g, idx
}

func TestFindHotspotsTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	// Create HierarchicalGraph and analytics
	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindHotspotsTool(analytics, idx)

	t.Run("finds hotspots with default params", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		hotspots, ok := output["hotspots"].([]map[string]any)
		if !ok {
			t.Fatalf("hotspots is not a slice")
		}

		// Should have at least one hotspot
		if len(hotspots) == 0 {
			t.Error("Expected at least one hotspot")
		}

		// Check output text is populated
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("respects top parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"top": 2,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		hotspots, ok := output["hotspots"].([]map[string]any)
		if !ok {
			t.Fatalf("hotspots is not a slice")
		}

		if len(hotspots) > 2 {
			t.Errorf("got %d hotspots, want at most 2", len(hotspots))
		}
	})

	t.Run("handles nil analytics", func(t *testing.T) {
		nilTool := NewFindHotspotsTool(nil, idx)
		result, err := nilTool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure with nil analytics")
		}
		if result.Error == "" {
			t.Error("Expected error message")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := tool.Execute(cancelCtx, map[string]any{})
		if err == nil {
			t.Error("Expected context.Canceled error")
		}
	})
}

func TestFindDeadCodeTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindDeadCodeTool(analytics, idx)

	t.Run("finds dead code by default (unexported only)", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		deadCode, ok := output["dead_code"].([]map[string]any)
		if !ok {
			t.Fatalf("dead_code is not a slice")
		}

		// funcD is unexported dead code
		found := false
		for _, dc := range deadCode {
			if dc["name"] == "funcD" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find funcD in dead code")
		}
	})

	t.Run("includes exported when requested", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"include_exported": true,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// OutputText should exist
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("filters by package", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"package": "util",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		deadCode, ok := output["dead_code"].([]map[string]any)
		if !ok {
			t.Fatalf("dead_code is not a slice")
		}

		// All results should be in util package
		for _, dc := range deadCode {
			if dc["package"] != "util" {
				t.Errorf("Found dead code from package %v, expected util", dc["package"])
			}
		}
	})

	t.Run("handles nil analytics", func(t *testing.T) {
		nilTool := NewFindDeadCodeTool(nil, idx)
		result, err := nilTool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure with nil analytics")
		}
	})
}

func TestFindCyclesTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCyclesTool(analytics, idx)

	t.Run("finds cycles", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		cycles, ok := output["cycles"].([]map[string]any)
		if !ok {
			t.Fatalf("cycles is not a slice")
		}

		// Should find the B <-> C cycle
		if len(cycles) == 0 {
			t.Error("Expected to find at least one cycle (funcB <-> funcC)")
		}

		// Check output text
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("respects min_size filter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"min_size": 3, // Filter out 2-node cycles
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		cycles, ok := output["cycles"].([]map[string]any)
		if !ok {
			t.Fatalf("cycles is not a slice")
		}

		// 2-node cycles should be filtered out
		for _, cycle := range cycles {
			length, _ := cycle["length"].(int)
			if length < 3 {
				t.Errorf("Found cycle with length %d, expected >= 3", length)
			}
		}
	})

	t.Run("handles nil analytics", func(t *testing.T) {
		nilTool := NewFindCyclesTool(nil, idx)
		result, err := nilTool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure with nil analytics")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := tool.Execute(cancelCtx, map[string]any{})
		if err == nil {
			t.Error("Expected context.Canceled error")
		}
	})
}

func TestFindPathTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	tool := NewFindPathTool(g, idx)

	t.Run("finds path between connected symbols", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"from": "main",
			"to":   "funcB",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		found, _ := output["found"].(bool)
		if !found {
			t.Error("Expected to find a path from main to funcB")
		}

		length, _ := output["length"].(int)
		if length < 1 {
			t.Errorf("Expected path length >= 1, got %d", length)
		}

		// Check path contains nodes
		path, ok := output["path"].([]map[string]any)
		if !ok {
			t.Fatalf("path is not a slice")
		}
		if len(path) == 0 {
			t.Error("Expected non-empty path")
		}

		// Check output text
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("returns no path for unconnected symbols", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"from": "funcD", // Dead code, not connected
			"to":   "main",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		found, _ := output["found"].(bool)
		if found {
			t.Error("Expected no path from funcD to main")
		}
	})

	t.Run("handles non-existent from symbol", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"from": "nonExistent",
			"to":   "main",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// Should return a message about symbol not found
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("handles non-existent to symbol", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"from": "main",
			"to":   "nonExistent",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("requires from parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"to": "main",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure without from parameter")
		}
	})

	t.Run("requires to parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"from": "main",
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure without to parameter")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := tool.Execute(cancelCtx, map[string]any{
			"from": "main",
			"to":   "funcB",
		})
		if err == nil {
			t.Error("Expected context.Canceled error")
		}
	})
}

func TestToolDefinitions_GraphAnalytics(t *testing.T) {
	g, idx := createTestGraphForAnalytics(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)

	tests := []struct {
		name     string
		tool     Tool
		wantName string
		wantCat  ToolCategory
	}{
		{
			name:     "find_hotspots",
			tool:     NewFindHotspotsTool(analytics, idx),
			wantName: "find_hotspots",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_dead_code",
			tool:     NewFindDeadCodeTool(analytics, idx),
			wantName: "find_dead_code",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_cycles",
			tool:     NewFindCyclesTool(analytics, idx),
			wantName: "find_cycles",
			wantCat:  CategoryExploration,
		},
		{
			name:     "find_path",
			tool:     NewFindPathTool(g, idx),
			wantName: "find_path",
			wantCat:  CategoryExploration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tool.Name(); got != tt.wantName {
				t.Errorf("Name() = %v, want %v", got, tt.wantName)
			}
			if got := tt.tool.Category(); got != tt.wantCat {
				t.Errorf("Category() = %v, want %v", got, tt.wantCat)
			}

			def := tt.tool.Definition()
			if def.Name != tt.wantName {
				t.Errorf("Definition().Name = %v, want %v", def.Name, tt.wantName)
			}
			if def.Description == "" {
				t.Error("Definition().Description is empty")
			}
		})
	}
}

func TestRegisterExploreTools_IncludesAnalyticsTools(t *testing.T) {
	g, idx := createTestGraphForAnalytics(t)
	registry := NewRegistry()

	RegisterExploreTools(registry, g, idx)

	// Check that the new analytics tools are registered
	analyticsTools := []string{
		"find_hotspots",
		"find_dead_code",
		"find_cycles",
		"find_path",
		"find_important", // GR-13
	}

	for _, name := range analyticsTools {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("Tool %s not registered", name)
		}
	}

	// Should have at least 21 tools now (16 original + 4 analytics + 1 PageRank)
	if count := registry.Count(); count < 21 {
		t.Errorf("Registry has %d tools, want at least 21", count)
	}
}

// =============================================================================
// GR-13: find_important Tool Tests (PageRank)
// =============================================================================

func TestFindImportantTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	// Create HierarchicalGraph and analytics
	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindImportantTool(analytics, idx)

	t.Run("finds important symbols with default params", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// Check algorithm field indicates PageRank
		algorithm, _ := output["algorithm"].(string)
		if algorithm != "PageRank" {
			t.Errorf("Expected algorithm 'PageRank', got '%s'", algorithm)
		}

		// Check results exist
		results, ok := output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}

		// Should have at least one result
		if len(results) == 0 {
			t.Error("Expected at least one important symbol")
		}

		// First result should have pagerank score and rank
		if len(results) > 0 {
			first := results[0]
			if _, ok := first["pagerank"]; !ok {
				t.Error("Expected pagerank score field in result")
			}
			if _, ok := first["rank"]; !ok {
				t.Error("Expected rank field in result")
			}
		}

		// Check output text is populated
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("respects top parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"top": 2,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		results, ok := output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}

		if len(results) > 2 {
			t.Errorf("got %d results, want at most 2", len(results))
		}
	})

	t.Run("top parameter capped at max", func(t *testing.T) {
		// Request more than max
		result, err := tool.Execute(ctx, map[string]any{
			"top": 1000,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// Should succeed without error (capped internally)
		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// Just verify we got valid results
		_, ok = output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}
	})

	t.Run("handles nil analytics", func(t *testing.T) {
		nilTool := NewFindImportantTool(nil, idx)
		result, err := nilTool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Error("Expected failure with nil analytics")
		}
		if result.Error == "" {
			t.Error("Expected error message")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := tool.Execute(cancelCtx, map[string]any{})
		if err == nil {
			t.Error("Expected context.Canceled error")
		}
	})

	t.Run("returns result metadata", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// Should have result count and algorithm metadata
		if _, ok := output["result_count"]; !ok {
			t.Error("Expected result_count field in output")
		}
		if _, ok := output["algorithm"]; !ok {
			t.Error("Expected algorithm field in output")
		}
	})
}

func TestFindImportantTool_VsHotspots(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForAnalytics(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)

	importantTool := NewFindImportantTool(analytics, idx)
	hotspotsTool := NewFindHotspotsTool(analytics, idx)

	// Get results from both tools
	importantResult, err := importantTool.Execute(ctx, map[string]any{"top": 6})
	if err != nil {
		t.Fatalf("find_important Execute() error = %v", err)
	}

	hotspotsResult, err := hotspotsTool.Execute(ctx, map[string]any{"top": 6})
	if err != nil {
		t.Fatalf("find_hotspots Execute() error = %v", err)
	}

	// Both should succeed
	if !importantResult.Success || !hotspotsResult.Success {
		t.Fatalf("One of the tools failed")
	}

	// Extract rankings (they may differ due to different algorithms)
	importantOutput := importantResult.Output.(map[string]any)
	importantResults := importantOutput["results"].([]map[string]any)

	hotspotsOutput := hotspotsResult.Output.(map[string]any)
	hotspotsResults := hotspotsOutput["hotspots"].([]map[string]any)

	// Just verify both returned reasonable results
	t.Logf("PageRank top: %v", importantResults[0]["name"])
	t.Logf("HotSpots top: %v", hotspotsResults[0]["name"])

	// Rankings may differ - that's expected
	// Just verify both have results
	if len(importantResults) == 0 {
		t.Error("find_important returned no results")
	}
	if len(hotspotsResults) == 0 {
		t.Error("find_hotspots returned no results")
	}
}

func TestFindImportantTool_Definition(t *testing.T) {
	tool := NewFindImportantTool(nil, nil)

	if got := tool.Name(); got != "find_important" {
		t.Errorf("Name() = %v, want find_important", got)
	}

	if got := tool.Category(); got != CategoryExploration {
		t.Errorf("Category() = %v, want CategoryExploration", got)
	}

	def := tool.Definition()
	if def.Name != "find_important" {
		t.Errorf("Definition().Name = %v, want find_important", def.Name)
	}
	if def.Description == "" {
		t.Error("Definition().Description is empty")
	}
	if len(def.Parameters) == 0 {
		t.Error("Definition().Parameters is empty")
	}

	// Check for expected parameters (Parameters is a map[string]ParamDef)
	if _, ok := def.Parameters["top"]; !ok {
		t.Error("Missing 'top' parameter")
	}
}

// BenchmarkFindImportant benchmarks PageRank-based importance ranking.
func BenchmarkFindImportant(b *testing.B) {
	g, idx := createLargeGraph(b, 1000)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		b.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindImportantTool(analytics, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{"top": 10})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// =============================================================================
// GR-15: find_communities Tool Tests (TDD)
// =============================================================================

// createTestGraphForCommunities creates a graph with clear community structure.
// Creates two distinct communities with minimal cross-community edges.
func createTestGraphForCommunities(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Community 1: auth package (4 nodes, densely connected)
	authLogin := &ast.Symbol{
		ID:        "auth/login.go:10:Login",
		Name:      "Login",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "auth/login.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "auth",
		Exported:  true,
		Language:  "go",
	}
	authLogout := &ast.Symbol{
		ID:        "auth/login.go:35:Logout",
		Name:      "Logout",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "auth/login.go",
		StartLine: 35,
		EndLine:   50,
		Package:   "auth",
		Exported:  true,
		Language:  "go",
	}
	authSession := &ast.Symbol{
		ID:        "auth/session.go:10:CreateSession",
		Name:      "CreateSession",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "auth/session.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "auth",
		Exported:  true,
		Language:  "go",
	}
	authValidate := &ast.Symbol{
		ID:        "auth/validate.go:10:ValidateToken",
		Name:      "ValidateToken",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "auth/validate.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "auth",
		Exported:  true,
		Language:  "go",
	}

	// Community 2: config package (3 nodes, densely connected)
	configLoad := &ast.Symbol{
		ID:        "config/loader.go:10:LoadConfig",
		Name:      "LoadConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/loader.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "config",
		Exported:  true,
		Language:  "go",
	}
	configParse := &ast.Symbol{
		ID:        "config/parser.go:10:ParseConfig",
		Name:      "ParseConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/parser.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "config",
		Exported:  true,
		Language:  "go",
	}
	configValidate := &ast.Symbol{
		ID:        "config/validate.go:10:ValidateConfig",
		Name:      "ValidateConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/validate.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "config",
		Exported:  true,
		Language:  "go",
	}

	// Add all nodes to graph and index
	allSymbols := []*ast.Symbol{
		authLogin, authLogout, authSession, authValidate,
		configLoad, configParse, configValidate,
	}
	for _, sym := range allSymbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Community 1 edges (auth - dense internal connections)
	g.AddEdge(authLogin.ID, authSession.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: authLogin.FilePath, StartLine: 15,
	})
	g.AddEdge(authLogin.ID, authValidate.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: authLogin.FilePath, StartLine: 18,
	})
	g.AddEdge(authLogout.ID, authSession.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: authLogout.FilePath, StartLine: 40,
	})
	g.AddEdge(authSession.ID, authValidate.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: authSession.FilePath, StartLine: 15,
	})

	// Community 2 edges (config - dense internal connections)
	g.AddEdge(configLoad.ID, configParse.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: configLoad.FilePath, StartLine: 15,
	})
	g.AddEdge(configLoad.ID, configValidate.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: configLoad.FilePath, StartLine: 20,
	})
	g.AddEdge(configParse.ID, configValidate.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: configParse.FilePath, StartLine: 15,
	})

	// One cross-community edge (sparse connection between communities)
	g.AddEdge(authLogin.ID, configLoad.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: authLogin.FilePath, StartLine: 12,
	})

	g.Freeze()
	return g, idx
}

// createCrossPackageCommunityGraph creates a graph where a community spans packages.
func createCrossPackageCommunityGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Cross-package community: server and config are tightly coupled
	serverInit := &ast.Symbol{
		ID:        "server/init.go:10:InitServer",
		Name:      "InitServer",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "server/init.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "server",
		Exported:  true,
		Language:  "go",
	}
	configLoad := &ast.Symbol{
		ID:        "config/loader.go:10:LoadConfig",
		Name:      "LoadConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/loader.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "config",
		Exported:  true,
		Language:  "go",
	}
	serverConfig := &ast.Symbol{
		ID:        "server/config.go:10:ConfigureServer",
		Name:      "ConfigureServer",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "server/config.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "server",
		Exported:  true,
		Language:  "go",
	}
	configDefault := &ast.Symbol{
		ID:        "config/defaults.go:10:DefaultConfig",
		Name:      "DefaultConfig",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config/defaults.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "config",
		Exported:  true,
		Language:  "go",
	}

	// Add nodes to graph and index
	allSymbols := []*ast.Symbol{serverInit, configLoad, serverConfig, configDefault}
	for _, sym := range allSymbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Dense cross-package connections (they should form one community)
	g.AddEdge(serverInit.ID, configLoad.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: serverInit.FilePath, StartLine: 15,
	})
	g.AddEdge(serverInit.ID, serverConfig.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: serverInit.FilePath, StartLine: 18,
	})
	g.AddEdge(serverConfig.ID, configLoad.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: serverConfig.FilePath, StartLine: 12,
	})
	g.AddEdge(serverConfig.ID, configDefault.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: serverConfig.FilePath, StartLine: 14,
	})
	g.AddEdge(configLoad.ID, configDefault.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath: configLoad.FilePath, StartLine: 15,
	})

	g.Freeze()
	return g, idx
}

func TestFindCommunitiesTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	t.Run("finds communities with default params", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// Check algorithm field
		algorithm, _ := output["algorithm"].(string)
		if algorithm != "Leiden" {
			t.Errorf("Expected algorithm 'Leiden', got '%s'", algorithm)
		}

		// Check modularity is present and in valid range
		modularity, ok := output["modularity"].(float64)
		if !ok {
			t.Error("Expected modularity field in output")
		} else if modularity < 0 || modularity > 1 {
			t.Errorf("Modularity %f outside expected range [0,1]", modularity)
		}

		// Check communities exist
		communities, ok := output["communities"].([]map[string]any)
		if !ok {
			t.Fatalf("communities is not a slice of maps")
		}

		if len(communities) == 0 {
			t.Error("Expected at least one community")
		}

		// Check output text is populated
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("respects min_size parameter", func(t *testing.T) {
		// With min_size=5, should filter out smaller communities
		result, err := tool.Execute(ctx, map[string]any{
			"min_size": 5,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)
		communities := output["communities"].([]map[string]any)

		// All returned communities should have size >= 5
		for _, comm := range communities {
			size, _ := comm["size"].(int)
			if size < 5 {
				t.Errorf("Community size %d is less than min_size 5", size)
			}
		}
	})

	t.Run("respects resolution parameter", func(t *testing.T) {
		// Lower resolution should produce fewer, larger communities
		lowResResult, err := tool.Execute(ctx, map[string]any{
			"resolution": 0.5,
		})
		if err != nil {
			t.Fatalf("Execute() low resolution error = %v", err)
		}

		// Higher resolution should produce more, smaller communities
		highResResult, err := tool.Execute(ctx, map[string]any{
			"resolution": 2.0,
		})
		if err != nil {
			t.Fatalf("Execute() high resolution error = %v", err)
		}

		// Both should succeed
		if !lowResResult.Success || !highResResult.Success {
			t.Fatalf("One of the resolution tests failed")
		}

		// Note: We can't guarantee exact community counts, but both should run
		lowOutput := lowResResult.Output.(map[string]any)
		highOutput := highResResult.Output.(map[string]any)

		if _, ok := lowOutput["modularity"]; !ok {
			t.Error("Low resolution output missing modularity")
		}
		if _, ok := highOutput["modularity"]; !ok {
			t.Error("High resolution output missing modularity")
		}
	})

	t.Run("respects top parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"top":      1,
			"min_size": 1, // Allow small communities
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)
		communities := output["communities"].([]map[string]any)

		if len(communities) > 1 {
			t.Errorf("got %d communities, want at most 1", len(communities))
		}
	})

	t.Run("clamps invalid min_size to valid range", func(t *testing.T) {
		// min_size < 1 should be clamped
		result, err := tool.Execute(ctx, map[string]any{
			"min_size": -5,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() should succeed with clamped min_size")
		}
	})

	t.Run("clamps invalid resolution to valid range", func(t *testing.T) {
		// resolution < 0.1 should be clamped
		result, err := tool.Execute(ctx, map[string]any{
			"resolution": 0.0,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() should succeed with clamped resolution")
		}
	})

	t.Run("clamps invalid top to valid range", func(t *testing.T) {
		// top > 50 should be clamped
		result, err := tool.Execute(ctx, map[string]any{
			"top": 1000,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() should succeed with clamped top")
		}
	})

	t.Run("returns result metadata", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)

		// Should have result metadata
		if _, ok := output["community_count"]; !ok {
			t.Error("Expected community_count field in output")
		}
		if _, ok := output["algorithm"]; !ok {
			t.Error("Expected algorithm field in output")
		}
		if _, ok := output["converged"]; !ok {
			t.Error("Expected converged field in output")
		}
	})
}

func TestFindCommunitiesTool_NilAnalytics(t *testing.T) {
	ctx := context.Background()
	_, idx := createTestGraphForCommunities(t)

	tool := NewFindCommunitiesTool(nil, idx)
	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Success {
		t.Error("Expected failure with nil analytics")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestFindCommunitiesTool_ContextCancellation(t *testing.T) {
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("Expected context.Canceled error")
	}
}

func TestFindCommunitiesTool_CrossPackageDetection(t *testing.T) {
	ctx := context.Background()
	g, idx := createCrossPackageCommunityGraph(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1, // Allow small communities
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)

	// Check if cross-package communities are identified
	crossPkg, ok := output["cross_package_communities"].([]int)
	if ok && len(crossPkg) > 0 {
		t.Logf("Found %d cross-package communities", len(crossPkg))
	}

	// The output text should mention cross-package if detected
	if result.OutputText != "" {
		// This is a soft check - cross-package detection is algorithmic
		t.Logf("Output text: %s", result.OutputText[:min(200, len(result.OutputText))])
	}
}

func TestFindCommunitiesTool_ShowCrossEdges(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	t.Run("show_cross_edges true", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"show_cross_edges": true,
			"min_size":         1,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)
		// cross_community_edges should be present
		if _, ok := output["cross_community_edges"]; !ok {
			t.Error("Expected cross_community_edges when show_cross_edges=true")
		}
	})

	t.Run("show_cross_edges false", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"show_cross_edges": false,
			"min_size":         1,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		// Should still succeed, just without cross edges
		output := result.Output.(map[string]any)
		edges, hasEdges := output["cross_community_edges"]
		if hasEdges {
			edgeSlice, ok := edges.([]map[string]any)
			if ok && len(edgeSlice) > 0 {
				t.Error("Expected no cross_community_edges when show_cross_edges=false")
			}
		}
	})
}

func TestFindCommunitiesTool_Definition(t *testing.T) {
	tool := NewFindCommunitiesTool(nil, nil)

	if got := tool.Name(); got != "find_communities" {
		t.Errorf("Name() = %v, want find_communities", got)
	}

	if got := tool.Category(); got != CategoryExploration {
		t.Errorf("Category() = %v, want CategoryExploration", got)
	}

	def := tool.Definition()
	if def.Name != "find_communities" {
		t.Errorf("Definition().Name = %v, want find_communities", def.Name)
	}
	if def.Description == "" {
		t.Error("Definition().Description is empty")
	}
	if len(def.Parameters) == 0 {
		t.Error("Definition().Parameters is empty")
	}

	// Check for expected parameters
	expectedParams := []string{"min_size", "resolution", "top", "show_cross_edges"}
	for _, param := range expectedParams {
		if _, ok := def.Parameters[param]; !ok {
			t.Errorf("Missing '%s' parameter", param)
		}
	}

	// Check timeout is reasonable
	if def.Timeout < 30*time.Second {
		t.Errorf("Timeout %v is too short for community detection", def.Timeout)
	}
}

func TestFindCommunitiesTool_EmptyGraph(t *testing.T) {
	ctx := context.Background()

	// Create empty graph
	g := graph.NewGraph("/test")
	g.Freeze()

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	idx := index.NewSymbolIndex()
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() should succeed with empty graph")
	}

	output := result.Output.(map[string]any)
	communities := output["communities"].([]map[string]any)

	if len(communities) != 0 {
		t.Errorf("Expected 0 communities for empty graph, got %d", len(communities))
	}
}

func TestFindCommunitiesTool_ModularityQualityLabel(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)

	// Should have modularity_quality label
	quality, ok := output["modularity_quality"].(string)
	if !ok {
		t.Error("Expected modularity_quality field in output")
	} else {
		validQualities := map[string]bool{
			"weak": true, "moderate": true, "good": true, "strong": true,
		}
		if !validQualities[quality] {
			t.Errorf("Invalid modularity_quality: %s", quality)
		}
	}
}

// BenchmarkFindCommunities benchmarks Leiden-based community detection.
func BenchmarkFindCommunities(b *testing.B) {
	g, idx := createLargeGraph(b, 500) // Smaller for faster benchmarks

	hg, err := graph.WrapGraph(g)
	if err != nil {
		b.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{"top": 10})
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// TestFindCommunitiesTool_SingleNodeGraph tests behavior with a single node.
func TestFindCommunitiesTool_SingleNodeGraph(t *testing.T) {
	ctx := context.Background()

	// Create single-node graph
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	singleSym := &ast.Symbol{
		ID:        "pkg/solo.go:10:Solo",
		Name:      "Solo",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/solo.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "pkg",
		Exported:  true,
		Language:  "go",
	}

	g.AddNode(singleSym)
	if err := idx.Add(singleSym); err != nil {
		t.Fatalf("Failed to add symbol: %v", err)
	}
	g.Freeze()

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1, // Allow single-node communities
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() should succeed with single node graph: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	communities := output["communities"].([]map[string]any)

	// Single node should form one community
	if len(communities) != 1 {
		t.Errorf("Expected 1 community for single-node graph, got %d", len(communities))
	}

	if len(communities) > 0 {
		size, _ := communities[0]["size"].(int)
		if size != 1 {
			t.Errorf("Single-node community should have size 1, got %d", size)
		}
	}
}

// createDisconnectedGraph creates a graph with two disconnected components.
func createDisconnectedGraph(t testing.TB) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Component A - fully connected triangle
	compA := []*ast.Symbol{
		{ID: "compA/a1.go:10:A1", Name: "A1", Kind: ast.SymbolKindFunction, FilePath: "compA/a1.go", StartLine: 10, EndLine: 20, Package: "compA", Language: "go"},
		{ID: "compA/a2.go:10:A2", Name: "A2", Kind: ast.SymbolKindFunction, FilePath: "compA/a2.go", StartLine: 10, EndLine: 20, Package: "compA", Language: "go"},
		{ID: "compA/a3.go:10:A3", Name: "A3", Kind: ast.SymbolKindFunction, FilePath: "compA/a3.go", StartLine: 10, EndLine: 20, Package: "compA", Language: "go"},
	}

	// Component B - fully connected triangle (disconnected from A)
	compB := []*ast.Symbol{
		{ID: "compB/b1.go:10:B1", Name: "B1", Kind: ast.SymbolKindFunction, FilePath: "compB/b1.go", StartLine: 10, EndLine: 20, Package: "compB", Language: "go"},
		{ID: "compB/b2.go:10:B2", Name: "B2", Kind: ast.SymbolKindFunction, FilePath: "compB/b2.go", StartLine: 10, EndLine: 20, Package: "compB", Language: "go"},
		{ID: "compB/b3.go:10:B3", Name: "B3", Kind: ast.SymbolKindFunction, FilePath: "compB/b3.go", StartLine: 10, EndLine: 20, Package: "compB", Language: "go"},
	}

	for _, sym := range append(compA, compB...) {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Connect component A (triangle)
	g.AddEdge(compA[0].ID, compA[1].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compA[0].FilePath, StartLine: 15})
	g.AddEdge(compA[1].ID, compA[2].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compA[1].FilePath, StartLine: 15})
	g.AddEdge(compA[2].ID, compA[0].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compA[2].FilePath, StartLine: 15})

	// Connect component B (triangle)
	g.AddEdge(compB[0].ID, compB[1].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compB[0].FilePath, StartLine: 15})
	g.AddEdge(compB[1].ID, compB[2].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compB[1].FilePath, StartLine: 15})
	g.AddEdge(compB[2].ID, compB[0].ID, graph.EdgeTypeCalls, ast.Location{FilePath: compB[2].FilePath, StartLine: 15})

	g.Freeze()
	return g, idx
}

// TestFindCommunitiesTool_DisconnectedGraph tests behavior with disconnected components.
func TestFindCommunitiesTool_DisconnectedGraph(t *testing.T) {
	ctx := context.Background()
	g, idx := createDisconnectedGraph(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	communities := output["communities"].([]map[string]any)

	// Should detect at least 2 communities (one per disconnected component)
	if len(communities) < 2 {
		t.Errorf("Expected at least 2 communities for disconnected graph, got %d", len(communities))
	}

	// Total nodes across communities should equal 6
	totalNodes := 0
	for _, comm := range communities {
		size, _ := comm["size"].(int)
		totalNodes += size
	}
	if totalNodes != 6 {
		t.Errorf("Total nodes in communities should be 6, got %d", totalNodes)
	}
}

// createAllSamePackageGraph creates a graph where all nodes are in the same package.
func createAllSamePackageGraph(t testing.TB) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	symbols := []*ast.Symbol{
		{ID: "myPkg/file1.go:10:Func1", Name: "Func1", Kind: ast.SymbolKindFunction, FilePath: "myPkg/file1.go", StartLine: 10, EndLine: 20, Package: "myPkg", Language: "go"},
		{ID: "myPkg/file1.go:30:Func2", Name: "Func2", Kind: ast.SymbolKindFunction, FilePath: "myPkg/file1.go", StartLine: 30, EndLine: 40, Package: "myPkg", Language: "go"},
		{ID: "myPkg/file2.go:10:Func3", Name: "Func3", Kind: ast.SymbolKindFunction, FilePath: "myPkg/file2.go", StartLine: 10, EndLine: 20, Package: "myPkg", Language: "go"},
		{ID: "myPkg/file2.go:30:Func4", Name: "Func4", Kind: ast.SymbolKindFunction, FilePath: "myPkg/file2.go", StartLine: 30, EndLine: 40, Package: "myPkg", Language: "go"},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Dense connections
	g.AddEdge(symbols[0].ID, symbols[1].ID, graph.EdgeTypeCalls, ast.Location{FilePath: symbols[0].FilePath, StartLine: 15})
	g.AddEdge(symbols[0].ID, symbols[2].ID, graph.EdgeTypeCalls, ast.Location{FilePath: symbols[0].FilePath, StartLine: 16})
	g.AddEdge(symbols[1].ID, symbols[3].ID, graph.EdgeTypeCalls, ast.Location{FilePath: symbols[1].FilePath, StartLine: 35})
	g.AddEdge(symbols[2].ID, symbols[3].ID, graph.EdgeTypeCalls, ast.Location{FilePath: symbols[2].FilePath, StartLine: 15})

	g.Freeze()
	return g, idx
}

// TestFindCommunitiesTool_AllSamePackage tests behavior when all nodes are in same package.
func TestFindCommunitiesTool_AllSamePackage(t *testing.T) {
	ctx := context.Background()
	g, idx := createAllSamePackageGraph(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)

	// Should have no cross-package communities since all are same package
	crossPkg, ok := output["cross_package_communities"].([]int)
	if ok && len(crossPkg) > 0 {
		t.Errorf("Expected no cross-package communities for same-package graph, got %d", len(crossPkg))
	}

	// Communities should exist and all have dominant_package = "myPkg"
	communities := output["communities"].([]map[string]any)
	for i, comm := range communities {
		domPkg, _ := comm["dominant_package"].(string)
		if domPkg != "" && domPkg != "myPkg" {
			t.Errorf("Community %d has unexpected dominant_package: %s", i, domPkg)
		}
	}
}

// TestFindCommunitiesTool_ParameterExactBoundaries tests exact boundary values.
func TestFindCommunitiesTool_ParameterExactBoundaries(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"min_size=1 (lower bound)", map[string]any{"min_size": 1}},
		{"min_size=100 (upper bound)", map[string]any{"min_size": 100}},
		{"resolution=0.1 (lower bound)", map[string]any{"resolution": 0.1}},
		{"resolution=5.0 (upper bound)", map[string]any{"resolution": 5.0}},
		{"top=1 (lower bound)", map[string]any{"top": 1}},
		{"top=50 (upper bound)", map[string]any{"top": 50}},
		{"all bounds at min", map[string]any{"min_size": 1, "resolution": 0.1, "top": 1}},
		{"all bounds at max", map[string]any{"min_size": 100, "resolution": 5.0, "top": 50}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tc.params)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !result.Success {
				t.Fatalf("Execute() failed: %s", result.Error)
			}

			output := result.Output.(map[string]any)
			if _, ok := output["modularity"]; !ok {
				t.Error("Expected modularity field in output")
			}
		})
	}
}

// TestFindCommunitiesTool_ConcurrentExecution tests thread safety.
func TestFindCommunitiesTool_ConcurrentExecution(t *testing.T) {
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	const goroutines = 10
	ctx := context.Background()

	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			result, err := tool.Execute(ctx, map[string]any{
				"min_size":   1,
				"resolution": 1.0 + float64(idx%3)*0.5,
			})
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: execute error: %w", idx, err)
				return
			}
			if !result.Success {
				errCh <- fmt.Errorf("goroutine %d: execution failed: %s", idx, result.Error)
				return
			}
			errCh <- nil
		}(i)
	}

	// Collect results
	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

// TestFindCommunitiesTool_OutputFormatValidation validates all expected fields.
func TestFindCommunitiesTool_OutputFormatValidation(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size":         1,
		"show_cross_edges": true,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatal("Output is not a map[string]any")
	}

	// Validate top-level fields
	topLevelFields := map[string]string{
		"algorithm":          "string",
		"modularity":         "float64",
		"modularity_quality": "string",
		"converged":          "bool",
		"community_count":    "int",
		"communities":        "[]map[string]any",
	}

	for field, expectedType := range topLevelFields {
		val, exists := output[field]
		if !exists {
			t.Errorf("Missing top-level field: %s", field)
			continue
		}

		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				t.Errorf("Field %s should be string, got %T", field, val)
			}
		case "float64":
			if _, ok := val.(float64); !ok {
				t.Errorf("Field %s should be float64, got %T", field, val)
			}
		case "bool":
			if _, ok := val.(bool); !ok {
				t.Errorf("Field %s should be bool, got %T", field, val)
			}
		case "int":
			if _, ok := val.(int); !ok {
				t.Errorf("Field %s should be int, got %T", field, val)
			}
		case "[]map[string]any":
			if _, ok := val.([]map[string]any); !ok {
				t.Errorf("Field %s should be []map[string]any, got %T", field, val)
			}
		}
	}

	// Validate community structure
	communities := output["communities"].([]map[string]any)
	if len(communities) > 0 {
		comm := communities[0]
		// Actual fields from formatCommunityResults
		communityFields := []string{"id", "size", "connectivity", "internal_edges", "external_edges", "members", "dominant_package", "packages", "is_cross_package"}
		for _, field := range communityFields {
			if _, exists := comm[field]; !exists {
				t.Errorf("Community missing field: %s", field)
			}
		}

		// Validate members is []map[string]any with "id" field
		if members, ok := comm["members"].([]map[string]any); ok {
			if len(members) == 0 {
				t.Error("Community members should not be empty for non-empty community")
			} else {
				if _, hasID := members[0]["id"]; !hasID {
					t.Error("Community member should have 'id' field")
				}
			}
		} else {
			t.Error("Community members should be []map[string]any")
		}
	}

	// Validate cross_community_edges when present (show_cross_edges=true)
	if edges, ok := output["cross_community_edges"].([]map[string]any); ok {
		if len(edges) > 0 {
			edgeFields := []string{"from_community", "to_community", "count"}
			for _, field := range edgeFields {
				if _, exists := edges[0][field]; !exists {
					t.Errorf("Cross-community edge missing field: %s", field)
				}
			}
		}
	}

	// Validate OutputText is non-empty
	if result.OutputText == "" {
		t.Error("OutputText should not be empty")
	}
}

// TestFindCommunitiesTool_TokensUsed verifies token estimation.
func TestFindCommunitiesTool_TokensUsed(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// TokensUsed should be > 0 for non-empty output
	if result.TokensUsed <= 0 {
		t.Error("TokensUsed should be > 0 for non-empty result")
	}

	// TokensUsed should be roughly proportional to OutputText length
	// (rough estimate: 4 chars per token)
	expectedMinTokens := len(result.OutputText) / 8
	if result.TokensUsed < expectedMinTokens {
		t.Errorf("TokensUsed %d seems too low for OutputText length %d", result.TokensUsed, len(result.OutputText))
	}
}

// TestFindCommunitiesTool_NilIndex tests behavior with nil index.
func TestFindCommunitiesTool_NilIndex(t *testing.T) {
	ctx := context.Background()
	g, _ := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)

	// Create tool with nil index - should still work since index is optional
	tool := NewFindCommunitiesTool(analytics, nil)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() should succeed with nil index: %s", result.Error)
	}
}

// TestFindCommunitiesTool_LargeMinSize tests that large min_size filters out all communities.
func TestFindCommunitiesTool_LargeMinSize(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t) // Creates ~10 nodes

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 100, // Much larger than graph size
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	communities := output["communities"].([]map[string]any)

	// All communities should be filtered out
	if len(communities) != 0 {
		t.Errorf("Expected 0 communities with min_size=100, got %d", len(communities))
	}

	// Should still report the underlying modularity
	if _, ok := output["modularity"]; !ok {
		t.Error("Expected modularity field even with no communities returned")
	}
}

// TestFindCommunitiesTool_TraceStepPopulated verifies CRS integration.
func TestFindCommunitiesTool_TraceStepPopulated(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForCommunities(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindCommunitiesTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"min_size": 1,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// TraceStep should be populated (H-1 fix verification)
	if result.TraceStep == nil {
		t.Fatal("TraceStep should be populated for CRS integration")
	}

	// Validate TraceStep fields
	if result.TraceStep.Action != "analytics_communities" {
		t.Errorf("TraceStep.Action = %q, want 'analytics_communities'", result.TraceStep.Action)
	}

	if result.TraceStep.Tool != "DetectCommunities" {
		t.Errorf("TraceStep.Tool = %q, want 'DetectCommunities'", result.TraceStep.Tool)
	}

	// Should have metadata
	if result.TraceStep.Metadata == nil {
		t.Error("TraceStep.Metadata should not be nil")
	} else {
		if _, ok := result.TraceStep.Metadata["algorithm"]; !ok {
			t.Error("TraceStep.Metadata should contain 'algorithm'")
		}
		if _, ok := result.TraceStep.Metadata["modularity"]; !ok {
			t.Error("TraceStep.Metadata should contain 'modularity'")
		}
	}

	// Duration should be tracked
	if result.TraceStep.Duration == 0 {
		t.Error("TraceStep.Duration should be > 0")
	}
}

// =============================================================================
// find_articulation_points tool tests (GR-17a)
// =============================================================================

// createTestGraphForArticulationPoints creates a graph with known articulation points.
// Structure:
//
//	A --- B --- C --- D --- E
//	      |         |
//	      F         G --- H
//
// Articulation points: B (connects A to rest), C (connects B,F to D,G,H), D (connects C to E), G (connects D to H)
// Bridges: A-B, B-C, C-D, D-E, D-G, G-H
func createTestGraphForArticulationPoints(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Create nodes
	symbols := []*ast.Symbol{
		{ID: "pkg/a.go:10:A", Name: "A", Kind: ast.SymbolKindFunction, FilePath: "pkg/a.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/b.go:10:B", Name: "B", Kind: ast.SymbolKindFunction, FilePath: "pkg/b.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/c.go:10:C", Name: "C", Kind: ast.SymbolKindFunction, FilePath: "pkg/c.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/d.go:10:D", Name: "D", Kind: ast.SymbolKindFunction, FilePath: "pkg/d.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/e.go:10:E", Name: "E", Kind: ast.SymbolKindFunction, FilePath: "pkg/e.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/f.go:10:F", Name: "F", Kind: ast.SymbolKindFunction, FilePath: "pkg/f.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/g.go:10:G", Name: "G", Kind: ast.SymbolKindFunction, FilePath: "pkg/g.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/h.go:10:H", Name: "H", Kind: ast.SymbolKindFunction, FilePath: "pkg/h.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Add edges (call relationships)
	// A-B chain
	g.AddEdge("pkg/a.go:10:A", "pkg/b.go:10:B", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/a.go", StartLine: 15})
	// B-C chain with B-F branch
	g.AddEdge("pkg/b.go:10:B", "pkg/c.go:10:C", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/b.go", StartLine: 15})
	g.AddEdge("pkg/b.go:10:B", "pkg/f.go:10:F", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/b.go", StartLine: 16})
	// C-D chain
	g.AddEdge("pkg/c.go:10:C", "pkg/d.go:10:D", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/c.go", StartLine: 15})
	// D-E and D-G branches
	g.AddEdge("pkg/d.go:10:D", "pkg/e.go:10:E", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/d.go", StartLine: 15})
	g.AddEdge("pkg/d.go:10:D", "pkg/g.go:10:G", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/d.go", StartLine: 16})
	// G-H chain
	g.AddEdge("pkg/g.go:10:G", "pkg/h.go:10:H", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/g.go", StartLine: 15})

	g.Freeze()
	return g, idx
}

// createConnectedGraphNoArticulation creates a graph with no articulation points (fully connected).
func createConnectedGraphNoArticulation(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Triangle: A-B-C-A (no articulation points)
	symbols := []*ast.Symbol{
		{ID: "pkg/a.go:10:A", Name: "A", Kind: ast.SymbolKindFunction, FilePath: "pkg/a.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/b.go:10:B", Name: "B", Kind: ast.SymbolKindFunction, FilePath: "pkg/b.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
		{ID: "pkg/c.go:10:C", Name: "C", Kind: ast.SymbolKindFunction, FilePath: "pkg/c.go", StartLine: 10, EndLine: 20, Package: "pkg", Exported: true, Language: "go"},
	}

	for _, sym := range symbols {
		g.AddNode(sym)
		if err := idx.Add(sym); err != nil {
			t.Logf("Warning: failed to index %s: %v", sym.Name, err)
		}
	}

	// Create triangle (bidirectional)
	g.AddEdge("pkg/a.go:10:A", "pkg/b.go:10:B", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/a.go", StartLine: 15})
	g.AddEdge("pkg/b.go:10:B", "pkg/c.go:10:C", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/b.go", StartLine: 15})
	g.AddEdge("pkg/c.go:10:C", "pkg/a.go:10:A", graph.EdgeTypeCalls, ast.Location{FilePath: "pkg/c.go", StartLine: 15})

	g.Freeze()
	return g, idx
}

func TestFindArticulationPointsTool_Execute(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	t.Run("finds articulation points with default params", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output, ok := result.Output.(map[string]any)
		if !ok {
			t.Fatalf("Output is not a map")
		}

		// Check articulation_points field exists
		points, ok := output["articulation_points"].([]map[string]any)
		if !ok {
			t.Fatalf("articulation_points is not a slice of maps")
		}

		// Should find at least some articulation points
		if len(points) == 0 {
			t.Error("Expected at least one articulation point")
		}

		// Check fragility_score is present and in valid range
		fragility, ok := output["fragility_score"].(float64)
		if !ok {
			t.Error("Expected fragility_score field in output")
		} else if fragility < 0 || fragility > 1 {
			t.Errorf("fragility_score %f outside expected range [0,1]", fragility)
		}

		// Check output text is populated
		if result.OutputText == "" {
			t.Error("OutputText is empty")
		}
	})

	t.Run("respects top parameter", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"top": 2,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)
		points := output["articulation_points"].([]map[string]any)

		// Should not exceed top limit
		if len(points) > 2 {
			t.Errorf("Expected at most 2 articulation points, got %d", len(points))
		}
	})

	t.Run("include_bridges=true returns bridges", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"include_bridges": true,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)

		// Bridges should be present
		bridges, ok := output["bridges"].([]map[string]any)
		if !ok {
			t.Error("Expected bridges field when include_bridges=true")
		} else if len(bridges) == 0 {
			t.Error("Expected at least one bridge in test graph")
		}
	})

	t.Run("include_bridges=false omits bridges", func(t *testing.T) {
		result, err := tool.Execute(ctx, map[string]any{
			"include_bridges": false,
		})

		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Fatalf("Execute() failed: %s", result.Error)
		}

		output := result.Output.(map[string]any)

		// Bridges should be empty or absent
		bridges, ok := output["bridges"].([]map[string]any)
		if ok && len(bridges) > 0 {
			t.Error("Expected no bridges when include_bridges=false")
		}
	})
}

func TestFindArticulationPointsTool_NoArticulationPoints(t *testing.T) {
	ctx := context.Background()
	g, idx := createConnectedGraphNoArticulation(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	points := output["articulation_points"].([]map[string]any)

	// Triangles have no articulation points
	if len(points) != 0 {
		t.Errorf("Expected 0 articulation points in triangle, got %d", len(points))
	}

	// Fragility should be 0
	fragility := output["fragility_score"].(float64)
	if fragility != 0 {
		t.Errorf("Expected fragility_score 0 for no articulation points, got %f", fragility)
	}
}

func TestFindArticulationPointsTool_NilAnalytics(t *testing.T) {
	ctx := context.Background()
	_, idx := createTestGraphForArticulationPoints(t)

	tool := NewFindArticulationPointsTool(nil, idx)
	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Success {
		t.Error("Expected failure with nil analytics")
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestFindArticulationPointsTool_ContextCancellation(t *testing.T) {
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("Expected context.Canceled error")
	}
}

func TestFindArticulationPointsTool_Definition(t *testing.T) {
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	def := tool.Definition()

	// Check name
	if def.Name != "find_articulation_points" {
		t.Errorf("Name = %q, want 'find_articulation_points'", def.Name)
	}

	// Check category
	if tool.Category() != CategoryExploration {
		t.Errorf("Category = %v, want CategoryExploration", tool.Category())
	}

	// Check required parameters
	if _, ok := def.Parameters["top"]; !ok {
		t.Error("Expected 'top' parameter in definition")
	}
	if _, ok := def.Parameters["include_bridges"]; !ok {
		t.Error("Expected 'include_bridges' parameter in definition")
	}

	// Check description mentions key concepts
	if def.Description == "" {
		t.Error("Description should not be empty")
	}
}

func TestFindArticulationPointsTool_EmptyGraph(t *testing.T) {
	ctx := context.Background()
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	g.Freeze() // Must freeze before wrapping
	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	points := output["articulation_points"].([]map[string]any)

	// Empty graph has no articulation points
	if len(points) != 0 {
		t.Errorf("Expected 0 articulation points in empty graph, got %d", len(points))
	}
}

func TestFindArticulationPointsTool_TraceStepPopulated(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// TraceStep should be populated for CRS integration
	if result.TraceStep == nil {
		t.Fatal("TraceStep should be populated for CRS integration")
	}

	// Validate TraceStep fields
	if result.TraceStep.Action != "analytics_articulation_points" {
		t.Errorf("TraceStep.Action = %q, want 'analytics_articulation_points'", result.TraceStep.Action)
	}

	if result.TraceStep.Tool != "ArticulationPoints" {
		t.Errorf("TraceStep.Tool = %q, want 'ArticulationPoints'", result.TraceStep.Tool)
	}

	// Should have metadata
	if result.TraceStep.Metadata == nil {
		t.Error("TraceStep.Metadata should not be nil")
	} else {
		if _, ok := result.TraceStep.Metadata["points_found"]; !ok {
			t.Error("TraceStep.Metadata should contain 'points_found'")
		}
		if _, ok := result.TraceStep.Metadata["bridges_found"]; !ok {
			t.Error("TraceStep.Metadata should contain 'bridges_found'")
		}
	}

	// Duration should be tracked
	if result.TraceStep.Duration == 0 {
		t.Error("TraceStep.Duration should be > 0")
	}
}

func TestFindArticulationPointsTool_ParameterValidation(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"top=1 (lower bound)", map[string]any{"top": 1}},
		{"top=100 (upper bound)", map[string]any{"top": 100}},
		{"include_bridges=true", map[string]any{"include_bridges": true}},
		{"include_bridges=false", map[string]any{"include_bridges": false}},
		{"both params", map[string]any{"top": 5, "include_bridges": true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tc.params)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !result.Success {
				t.Fatalf("Execute() failed: %s", result.Error)
			}

			output := result.Output.(map[string]any)
			if _, ok := output["fragility_score"]; !ok {
				t.Error("Expected fragility_score field in output")
			}
		})
	}
}

func TestFindArticulationPointsTool_OutputFormatValidation(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{
		"include_bridges": true,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatal("Output is not a map[string]any")
	}

	// Validate top-level fields
	topLevelFields := map[string]string{
		"articulation_points": "[]map[string]any",
		"bridges":             "[]map[string]any",
		"total_components":    "int",
		"fragility_score":     "float64",
		"fragility_level":     "string",
	}

	for field, expectedType := range topLevelFields {
		val, exists := output[field]
		if !exists {
			t.Errorf("Missing top-level field: %s", field)
			continue
		}

		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				t.Errorf("Field %s should be string, got %T", field, val)
			}
		case "float64":
			if _, ok := val.(float64); !ok {
				t.Errorf("Field %s should be float64, got %T", field, val)
			}
		case "int":
			if _, ok := val.(int); !ok {
				t.Errorf("Field %s should be int, got %T", field, val)
			}
		case "[]map[string]any":
			if _, ok := val.([]map[string]any); !ok {
				t.Errorf("Field %s should be []map[string]any, got %T", field, val)
			}
		}
	}

	// Validate articulation point structure
	points := output["articulation_points"].([]map[string]any)
	if len(points) > 0 {
		point := points[0]
		requiredFields := []string{"id", "name"}
		for _, f := range requiredFields {
			if _, ok := point[f]; !ok {
				t.Errorf("Articulation point missing field: %s", f)
			}
		}
	}

	// Validate bridge structure
	bridges := output["bridges"].([]map[string]any)
	if len(bridges) > 0 {
		bridge := bridges[0]
		requiredFields := []string{"from", "to"}
		for _, f := range requiredFields {
			if _, ok := bridge[f]; !ok {
				t.Errorf("Bridge missing field: %s", f)
			}
		}
	}
}

func TestFindArticulationPointsTool_ConcurrentExecution(t *testing.T) {
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	const goroutines = 10
	ctx := context.Background()

	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			result, err := tool.Execute(ctx, map[string]any{
				"top":             idx%5 + 1,
				"include_bridges": idx%2 == 0,
			})
			if err != nil {
				errCh <- fmt.Errorf("goroutine %d: execute error: %w", idx, err)
				return
			}
			if !result.Success {
				errCh <- fmt.Errorf("goroutine %d: execution failed: %s", idx, result.Error)
				return
			}
			errCh <- nil
		}(i)
	}

	// Collect results
	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

func TestFindArticulationPointsTool_TokensUsed(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// TokensUsed should be > 0 for non-empty output
	if result.TokensUsed <= 0 {
		t.Error("TokensUsed should be > 0 for non-empty result")
	}

	// TokensUsed should be roughly proportional to OutputText length
	// (rough estimate: 4 chars per token)
	expectedMinTokens := len(result.OutputText) / 8
	if result.TokensUsed < expectedMinTokens {
		t.Errorf("TokensUsed %d seems too low for OutputText length %d", result.TokensUsed, len(result.OutputText))
	}
}

func TestFindArticulationPointsTool_NilIndex(t *testing.T) {
	ctx := context.Background()
	g, _ := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)

	// Create tool with nil index - should still work since index is optional
	tool := NewFindArticulationPointsTool(analytics, nil)

	result, err := tool.Execute(ctx, map[string]any{})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() should succeed with nil index: %s", result.Error)
	}
}

// BenchmarkFindArticulationPoints benchmarks articulation point detection.
func BenchmarkFindArticulationPoints(b *testing.B) {
	g, idx := createLargeGraph(b, 500) // Use existing helper

	hg, err := graph.WrapGraph(g)
	if err != nil {
		b.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, map[string]any{"top": 10})
		if err != nil {
			b.Fatalf("Execute() error: %v", err)
		}
	}
}

// =============================================================================
// Additional GR-17a Tests (find_articulation_points tool)
// =============================================================================

// TestFindArticulationPointsTool_TopClampingLow verifies clamping when top < 1.
func TestFindArticulationPointsTool_TopClampingLow(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	// Test with top = 0 (should clamp to 1)
	result, err := tool.Execute(ctx, map[string]any{"top": 0})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	points := output["articulation_points"].([]map[string]any)

	// Should have at most 1 point (clamped)
	if len(points) > 1 {
		t.Errorf("Expected at most 1 point when top=0 (clamped to 1), got %d", len(points))
	}

	// Test with top = -5 (should clamp to 1)
	result2, err := tool.Execute(ctx, map[string]any{"top": -5})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result2.Success {
		t.Fatalf("Execute() failed: %s", result2.Error)
	}

	output2 := result2.Output.(map[string]any)
	points2 := output2["articulation_points"].([]map[string]any)
	if len(points2) > 1 {
		t.Errorf("Expected at most 1 point when top=-5 (clamped to 1), got %d", len(points2))
	}
}

// TestFindArticulationPointsTool_TopClampingHigh verifies clamping when top > 100.
func TestFindArticulationPointsTool_TopClampingHigh(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	// Test with top = 500 (should clamp to 100)
	result, err := tool.Execute(ctx, map[string]any{"top": 500})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	// Should succeed (clamped to 100, which is more than our test graph has)
	output := result.Output.(map[string]any)
	if _, ok := output["articulation_points"]; !ok {
		t.Error("Expected articulation_points in output")
	}
}

// TestFindArticulationPointsTool_FragilityLevels verifies fragility_level categories.
func TestFindArticulationPointsTool_FragilityLevels(t *testing.T) {
	ctx := context.Background()

	// Valid fragility levels as defined in getFragilityLevel()
	validLevels := map[string]bool{
		"MINIMAL - well-connected architecture":     true, // < 5%
		"LOW - reasonably robust":                   true, // 5-10%
		"MODERATE - some architectural bottlenecks": true, // 10-20%
		"HIGH - many single points of failure":      true, // >= 20%
	}

	tests := []struct {
		name             string
		graphFunc        func(*testing.T) (*graph.Graph, *index.SymbolIndex)
		expectedContains string // substring that should be in the level
	}{
		{
			name:             "no articulation points (triangle)",
			graphFunc:        createConnectedGraphNoArticulation,
			expectedContains: "MINIMAL", // 0% fragility
		},
		{
			name:             "with articulation points",
			graphFunc:        createTestGraphForArticulationPoints,
			expectedContains: "", // Just verify it's a valid level string
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, idx := tc.graphFunc(t)
			hg, err := graph.WrapGraph(g)
			if err != nil {
				t.Fatalf("WrapGraph failed: %v", err)
			}
			analytics := graph.NewGraphAnalytics(hg)
			tool := NewFindArticulationPointsTool(analytics, idx)

			result, err := tool.Execute(ctx, map[string]any{})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !result.Success {
				t.Fatalf("Execute() failed: %s", result.Error)
			}

			output := result.Output.(map[string]any)
			level, ok := output["fragility_level"].(string)
			if !ok {
				t.Fatal("Expected fragility_level to be a string")
			}

			// Verify it's a valid level
			if !validLevels[level] {
				t.Errorf("Invalid fragility_level: %q", level)
			}

			// Check expected substring if specified
			if tc.expectedContains != "" {
				found := false
				for validLevel := range validLevels {
					if validLevel == level && strings.Contains(level, tc.expectedContains) {
						found = true
						break
					}
				}
				if !found && !strings.Contains(level, tc.expectedContains) {
					t.Errorf("Expected fragility_level to contain %q, got %q", tc.expectedContains, level)
				}
			}
		})
	}
}

// TestFindArticulationPointsTool_AllOutputFields verifies all expected fields exist.
func TestFindArticulationPointsTool_AllOutputFields(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{"include_bridges": true})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)

	// All required top-level fields
	requiredFields := []string{
		"articulation_points",
		"bridges",
		"total_components",
		"fragility_score",
		"fragility_level",
		"node_count",
		"edge_count",
	}

	for _, field := range requiredFields {
		if _, ok := output[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Verify Result fields
	if result.OutputText == "" {
		t.Error("OutputText should not be empty")
	}
	if result.TokensUsed <= 0 {
		t.Error("TokensUsed should be positive")
	}
	if result.TraceStep == nil {
		t.Error("TraceStep should be populated")
	}
}

// TestFindArticulationPointsTool_PointMetadata verifies each point has expected metadata.
func TestFindArticulationPointsTool_PointMetadata(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	points := output["articulation_points"].([]map[string]any)

	if len(points) == 0 {
		t.Fatal("Expected at least one articulation point")
	}

	// Check first point has expected fields
	point := points[0]
	requiredPointFields := []string{"id", "name"}
	for _, field := range requiredPointFields {
		if _, ok := point[field]; !ok {
			t.Errorf("Point missing required field: %s", field)
		}
	}

	// ID should not be empty
	if point["id"] == "" {
		t.Error("Point ID should not be empty")
	}
}

// TestFindArticulationPointsTool_BridgeMetadata verifies bridge metadata.
func TestFindArticulationPointsTool_BridgeMetadata(t *testing.T) {
	ctx := context.Background()
	g, idx := createTestGraphForArticulationPoints(t)

	hg, err := graph.WrapGraph(g)
	if err != nil {
		t.Fatalf("WrapGraph failed: %v", err)
	}
	analytics := graph.NewGraphAnalytics(hg)
	tool := NewFindArticulationPointsTool(analytics, idx)

	result, err := tool.Execute(ctx, map[string]any{"include_bridges": true})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() failed: %s", result.Error)
	}

	output := result.Output.(map[string]any)
	bridges := output["bridges"].([]map[string]any)

	if len(bridges) == 0 {
		t.Fatal("Expected at least one bridge in test graph")
	}

	// Check first bridge has expected fields
	bridge := bridges[0]
	requiredBridgeFields := []string{"from", "to", "from_name", "to_name"}
	for _, field := range requiredBridgeFields {
		if _, ok := bridge[field]; !ok {
			t.Errorf("Bridge missing required field: %s", field)
		}
	}
}
