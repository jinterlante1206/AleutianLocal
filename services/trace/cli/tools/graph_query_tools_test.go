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

		results, ok := output["results"].([]map[string]any)
		if !ok {
			t.Fatalf("results is not a slice")
		}

		if len(results) != 1 {
			t.Errorf("got %d result entries, want 1", len(results))
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
