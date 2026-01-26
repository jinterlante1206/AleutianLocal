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
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

func setupConfigTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create symbols representing config access patterns

	getEnv := &ast.Symbol{
		ID:        "os.Getenv",
		Name:      "Getenv",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/config/config.go",
		StartLine: 10,
		EndLine:   10,
		Package:   "os",
		Language:  "go",
	}

	viperGet := &ast.Symbol{
		ID:        "github.com/spf13/viper.GetString",
		Name:      "GetString",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/config/config.go",
		StartLine: 20,
		EndLine:   20,
		Package:   "github.com/spf13/viper",
		Language:  "go",
	}

	viperSetDefault := &ast.Symbol{
		ID:        "github.com/spf13/viper.SetDefault",
		Name:      "SetDefault",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/config/config.go",
		StartLine: 25,
		EndLine:   25,
		Package:   "github.com/spf13/viper",
		Language:  "go",
	}

	flagString := &ast.Symbol{
		ID:        "flag.String",
		Name:      "String",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "cmd/main.go",
		StartLine: 15,
		EndLine:   15,
		Package:   "flag",
		Language:  "go",
	}

	processEnv := &ast.Symbol{
		ID:        "process.env",
		Name:      "env",
		Kind:      ast.SymbolKindFunction,
		Receiver:  "process",
		FilePath:  "src/config.ts",
		StartLine: 5,
		EndLine:   5,
		Package:   "",
		Language:  "typescript",
	}

	pythonGetenv := &ast.Symbol{
		ID:        "os.getenv",
		Name:      "getenv",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "config.py",
		StartLine: 10,
		EndLine:   10,
		Package:   "os",
		Language:  "python",
	}

	// Add nodes
	g.AddNode(getEnv)
	g.AddNode(viperGet)
	g.AddNode(viperSetDefault)
	g.AddNode(flagString)
	g.AddNode(processEnv)
	g.AddNode(pythonGetenv)

	g.Freeze()

	// Index all symbols
	idx.Add(getEnv)
	idx.Add(viperGet)
	idx.Add(viperSetDefault)
	idx.Add(flagString)
	idx.Add(processEnv)
	idx.Add(pythonGetenv)

	return g, idx
}

func TestConfigFinder_FindConfigUsage(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("finds config usage", func(t *testing.T) {
		ctx := context.Background()
		usage, err := finder.FindConfigUsage(ctx, "DATABASE_URL")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if usage == nil {
			t.Fatal("expected non-nil usage")
		}
		if usage.ConfigKey != "DATABASE_URL" {
			t.Errorf("expected config key 'DATABASE_URL', got '%s'", usage.ConfigKey)
		}
	})

	t.Run("finds config usage with wildcard", func(t *testing.T) {
		ctx := context.Background()
		usage, err := finder.FindConfigUsage(ctx, "DATABASE_*")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if usage == nil {
			t.Fatal("expected non-nil usage")
		}
		if usage.ConfigKey != "DATABASE_*" {
			t.Errorf("expected config key 'DATABASE_*', got '%s'", usage.ConfigKey)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := finder.FindConfigUsage(nil, "DATABASE_URL")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := finder.FindConfigUsage(ctx, "DATABASE_URL")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestConfigFinder_FindAllConfigAccess(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("finds config access in Go file", func(t *testing.T) {
		ctx := context.Background()
		accesses, err := finder.FindAllConfigAccess(ctx, "pkg/config/config.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find Getenv, GetString, SetDefault
		if len(accesses) == 0 {
			t.Error("expected to find config access points")
		}
	})

	t.Run("finds config access in TypeScript file", func(t *testing.T) {
		ctx := context.Background()
		accesses, err := finder.FindAllConfigAccess(ctx, "src/config.ts")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find process.env
		if len(accesses) == 0 {
			t.Error("expected to find config access in TypeScript file")
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		ctx := context.Background()
		_, err := finder.FindAllConfigAccess(ctx, "nonexistent.go")
		if err != ErrFileNotFound {
			t.Errorf("expected ErrFileNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := finder.FindAllConfigAccess(nil, "pkg/config/config.go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestConfigFinder_FindConfigByFramework(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("finds viper config access", func(t *testing.T) {
		ctx := context.Background()
		accesses, err := finder.FindConfigByFramework(ctx, "viper")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find GetString, SetDefault
		if len(accesses) == 0 {
			t.Error("expected to find viper config access")
		}

		// Check context mentions viper
		for _, access := range accesses {
			if access.Context == "" {
				t.Error("expected non-empty context")
			}
		}
	})

	t.Run("finds stdlib config access", func(t *testing.T) {
		ctx := context.Background()
		accesses, err := finder.FindConfigByFramework(ctx, "stdlib")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find Getenv
		if len(accesses) == 0 {
			t.Error("expected to find stdlib config access")
		}
	})

	t.Run("returns empty for unknown framework", func(t *testing.T) {
		ctx := context.Background()
		accesses, err := finder.FindConfigByFramework(ctx, "unknown_framework")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(accesses) != 0 {
			t.Errorf("expected empty result for unknown framework, got %d", len(accesses))
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := finder.FindConfigByFramework(nil, "viper")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestConfigFinder_FindEnvironmentVariables(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("finds environment variable access", func(t *testing.T) {
		ctx := context.Background()
		envAccesses, err := finder.FindEnvironmentVariables(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find Getenv (Go), env (TypeScript), getenv (Python)
		if len(envAccesses) == 0 {
			t.Error("expected to find environment variable access")
		}

		// Check that context mentions environment
		for _, access := range envAccesses {
			if access.Context != "Environment variable access" {
				t.Errorf("expected context 'Environment variable access', got '%s'", access.Context)
			}
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := finder.FindEnvironmentVariables(nil)
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := finder.FindEnvironmentVariables(ctx)
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestConfigFinder_TraceConfigUsage(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("traces config value flow", func(t *testing.T) {
		ctx := context.Background()
		flow, err := finder.TraceConfigUsage(ctx, "os.Getenv")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if flow == nil {
			t.Fatal("expected non-nil flow")
		}

		// Should have path
		if len(flow.Path) == 0 {
			t.Error("expected non-empty path")
		}
	})

	t.Run("returns error for non-existent symbol", func(t *testing.T) {
		ctx := context.Background()
		_, err := finder.TraceConfigUsage(ctx, "nonexistent.Symbol")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})
}

func TestDefaultGoConfigPatterns(t *testing.T) {
	patterns := DefaultGoConfigPatterns()

	t.Run("has stdlib patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "stdlib" && p.FunctionName == "Getenv" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected stdlib Getenv pattern")
		}
	})

	t.Run("has viper patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "viper" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected viper patterns")
		}
	})

	t.Run("has flag patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "flag" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected flag patterns")
		}
	})
}

func TestDefaultPythonConfigPatterns(t *testing.T) {
	patterns := DefaultPythonConfigPatterns()

	t.Run("has stdlib patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "stdlib" && p.FunctionName == "getenv" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected stdlib getenv pattern")
		}
	})

	t.Run("has dotenv patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "dotenv" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected dotenv patterns")
		}
	})
}

func TestDefaultTypeScriptConfigPatterns(t *testing.T) {
	patterns := DefaultTypeScriptConfigPatterns()

	t.Run("has node env pattern", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "node" && p.FunctionName == "env" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected node env pattern")
		}
	})

	t.Run("has dotenv pattern", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "dotenv" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected dotenv patterns")
		}
	})

	t.Run("has nestjs patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns {
			if p.Framework == "nestjs" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected nestjs patterns")
		}
	})
}

func TestConfigFinder_RegisterPatterns(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	customPatterns := []ConfigPattern{
		{FunctionName: "customConfig", Framework: "custom", Description: "Custom config"},
	}
	finder.RegisterPatterns("custom", customPatterns)

	patterns := finder.GetPatterns("custom")
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0].FunctionName != "customConfig" {
		t.Errorf("expected customConfig, got %s", patterns[0].FunctionName)
	}
}

func TestConfigFinder_IsConfigAccessor(t *testing.T) {
	g, idx := setupConfigTestGraph()
	finder := NewConfigFinder(g, idx)

	t.Run("detects os.Getenv as config accessor", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "Getenv",
			Package:  "os",
			Language: "go",
		}
		if !finder.isConfigAccessor(sym) {
			t.Error("expected os.Getenv to be detected as config accessor")
		}
	})

	t.Run("detects viper.GetString as config accessor", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "GetString",
			Package:  "github.com/spf13/viper",
			Language: "go",
		}
		if !finder.isConfigAccessor(sym) {
			t.Error("expected viper.GetString to be detected as config accessor")
		}
	})

	t.Run("detects process.env as config accessor", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "env",
			Receiver: "process",
			Language: "typescript",
		}
		if !finder.isConfigAccessor(sym) {
			t.Error("expected process.env to be detected as config accessor")
		}
	})

	t.Run("rejects non-config function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "ProcessData",
			Package:  "service",
			Language: "go",
		}
		if finder.isConfigAccessor(sym) {
			t.Error("expected non-config function not to be detected")
		}
	})

	t.Run("handles nil symbol", func(t *testing.T) {
		if finder.isConfigAccessor(nil) {
			t.Error("expected false for nil symbol")
		}
	})
}

func TestConfigPattern_Match(t *testing.T) {
	t.Run("matches by function name", func(t *testing.T) {
		pattern := ConfigPattern{FunctionName: "Getenv"}
		sym := &ast.Symbol{Name: "Getenv", Language: "go"}

		finder := &ConfigFinder{}
		if !finder.matchesPattern(sym, &pattern) {
			t.Error("expected pattern to match")
		}
	})

	t.Run("matches with wildcard in function name", func(t *testing.T) {
		pattern := ConfigPattern{FunctionName: "Get*"}
		sym := &ast.Symbol{Name: "GetString", Language: "go"}

		finder := &ConfigFinder{}
		if !finder.matchesPattern(sym, &pattern) {
			t.Error("expected wildcard pattern to match")
		}
	})

	t.Run("matches with wildcard in package", func(t *testing.T) {
		pattern := ConfigPattern{FunctionName: "Get", Package: "*viper*"}
		sym := &ast.Symbol{Name: "Get", Package: "github.com/spf13/viper", Language: "go"}

		finder := &ConfigFinder{}
		if !finder.matchesPattern(sym, &pattern) {
			t.Error("expected wildcard package pattern to match")
		}
	})

	t.Run("matches by receiver", func(t *testing.T) {
		pattern := ConfigPattern{FunctionName: "env", Receiver: "process"}
		sym := &ast.Symbol{Name: "env", Receiver: "process", Language: "typescript"}

		finder := &ConfigFinder{}
		if !finder.matchesPattern(sym, &pattern) {
			t.Error("expected receiver pattern to match")
		}
	})

	t.Run("does not match different function", func(t *testing.T) {
		pattern := ConfigPattern{FunctionName: "Getenv"}
		sym := &ast.Symbol{Name: "Setenv", Language: "go"}

		finder := &ConfigFinder{}
		if finder.matchesPattern(sym, &pattern) {
			t.Error("expected pattern not to match different function")
		}
	})
}
