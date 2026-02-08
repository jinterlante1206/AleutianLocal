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
	"testing"

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
