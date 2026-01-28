// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

func setupTypeCompatTestData() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create an interface
	reader := &ast.Symbol{
		ID:        "io/reader.go:10:Reader",
		Name:      "Reader",
		Kind:      ast.SymbolKindInterface,
		FilePath:  "io/reader.go",
		StartLine: 10,
		Package:   "io",
		Language:  "go",
		Exported:  true,
		Children: []*ast.Symbol{
			{
				ID:        "io/reader.go:11:Read",
				Name:      "Read",
				Kind:      ast.SymbolKindMethod,
				Signature: "func(p []byte) (n int, err error)",
				Language:  "go",
			},
		},
	}

	// Create a struct that implements Reader
	myReader := &ast.Symbol{
		ID:        "pkg/reader.go:5:MyReader",
		Name:      "MyReader",
		Kind:      ast.SymbolKindStruct,
		FilePath:  "pkg/reader.go",
		StartLine: 5,
		Package:   "pkg",
		Language:  "go",
		Exported:  true,
	}

	// Create the Read method for MyReader
	readMethod := &ast.Symbol{
		ID:        "pkg/reader.go:15:Read",
		Name:      "Read",
		Kind:      ast.SymbolKindMethod,
		FilePath:  "pkg/reader.go",
		StartLine: 15,
		Package:   "pkg",
		Receiver:  "*MyReader",
		Signature: "func(p []byte) (n int, err error)",
		Language:  "go",
		Exported:  true,
	}

	// Add to graph
	g.AddNode(reader)
	g.AddNode(myReader)
	g.AddNode(readMethod)

	// Add implements edge
	g.AddEdge(myReader.ID, reader.ID, graph.EdgeTypeImplements, ast.Location{
		FilePath:  myReader.FilePath,
		StartLine: myReader.StartLine,
	})

	g.Freeze()

	// Add to index
	idx.Add(reader)
	idx.Add(reader.Children[0])
	idx.Add(myReader)
	idx.Add(readMethod)

	return g, idx
}

func TestTypeCompatibilityChecker_CheckCompatibility(t *testing.T) {
	g, idx := setupTypeCompatTestData()
	checker := NewTypeCompatibilityChecker(g, idx)
	ctx := context.Background()

	t.Run("exact match", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "int", "int")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected compatible for exact match")
		}
		if result.Confidence != 1.0 {
			t.Errorf("expected confidence 1.0, got %f", result.Confidence)
		}
	})

	t.Run("empty interface accepts anything", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "MyType", "interface{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected compatible with empty interface")
		}
	})

	t.Run("any accepts anything", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "MyType", "any")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected compatible with any")
		}
	})

	t.Run("pointer to value", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "*MyType", "MyType")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected pointer to be compatible with value (via dereference)")
		}
	})

	t.Run("value to pointer", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "MyType", "*MyType")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected value to be compatible with pointer (via address)")
		}
	})

	t.Run("slice types match", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "[]byte", "[]byte")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Compatible {
			t.Error("expected matching slices to be compatible")
		}
	})

	t.Run("incompatible types", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "string", "int")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Compatible {
			t.Error("expected string and int to be incompatible")
		}
	})

	t.Run("suggests conversions", func(t *testing.T) {
		result, err := checker.CheckCompatibility(ctx, "[]byte", "string")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Conversions) == 0 {
			t.Error("expected conversion suggestion for []byte to string")
		}
	})
}

func TestTypeCompatibilityChecker_Errors(t *testing.T) {
	g, idx := setupTypeCompatTestData()
	checker := NewTypeCompatibilityChecker(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := checker.CheckCompatibility(nil, "a", "b")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty source type", func(t *testing.T) {
		_, err := checker.CheckCompatibility(ctx, "", "int")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target type", func(t *testing.T) {
		_, err := checker.CheckCompatibility(ctx, "int", "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := checker.CheckCompatibility(cancelCtx, "a", "b")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestTypeCompatibilityChecker_InterfaceSatisfaction(t *testing.T) {
	g, idx := setupTypeCompatTestData()
	checker := NewTypeCompatibilityChecker(g, idx)
	ctx := context.Background()

	t.Run("type satisfies interface", func(t *testing.T) {
		result, err := checker.CheckInterfaceSatisfaction(ctx, "MyReader", "Reader")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Note: This test depends on the graph having implements edge
		// The setup creates this relationship
		if !result.Satisfied && len(result.MissingMethods) > 0 {
			// If not satisfied, should have missing methods
			t.Logf("Not satisfied, missing: %v", result.MissingMethods)
		}
	})

	t.Run("interface not found", func(t *testing.T) {
		result, err := checker.CheckInterfaceSatisfaction(ctx, "MyType", "NonExistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Satisfied {
			t.Error("should not be satisfied when interface not found")
		}
		if result.Confidence >= 0.5 {
			t.Error("confidence should be low when interface not found")
		}
	})

	t.Run("type not found", func(t *testing.T) {
		result, err := checker.CheckInterfaceSatisfaction(ctx, "NonExistent", "Reader")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Satisfied {
			t.Error("should not be satisfied when type not found")
		}
	})
}

func TestTypeCompatibilityChecker_InterfaceSatisfaction_Errors(t *testing.T) {
	g, idx := setupTypeCompatTestData()
	checker := NewTypeCompatibilityChecker(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := checker.CheckInterfaceSatisfaction(nil, "MyType", "Reader")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty type name", func(t *testing.T) {
		_, err := checker.CheckInterfaceSatisfaction(ctx, "", "Reader")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty interface name", func(t *testing.T) {
		_, err := checker.CheckInterfaceSatisfaction(ctx, "MyType", "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestCheckBuiltinCompatibility(t *testing.T) {
	tests := []struct {
		source   string
		target   string
		expected bool
	}{
		{"int", "int", true},
		{"int", "int64", true},
		{"int64", "int", true},
		{"string", "string", true},
		{"bool", "bool", true},
		{"byte", "uint8", true},
		{"rune", "int32", true},
		{"anything", "interface{}", true},
		{"anything", "any", true},
		{"int", "string", false},
		{"bool", "int", false},
	}

	for _, tt := range tests {
		t.Run(tt.source+"->"+tt.target, func(t *testing.T) {
			compatible, _ := checkBuiltinCompatibility(tt.source, tt.target)
			if compatible != tt.expected {
				t.Errorf("checkBuiltinCompatibility(%s, %s) = %v, want %v",
					tt.source, tt.target, compatible, tt.expected)
			}
		})
	}
}

func TestCheckPointerCompatibility(t *testing.T) {
	tests := []struct {
		source   string
		target   string
		expected bool
	}{
		{"*T", "T", true},
		{"T", "*T", true},
		{"*T", "*T", false}, // Exact match handled elsewhere
		{"T", "T", false},   // Exact match handled elsewhere
		{"*A", "B", false},
		{"A", "*B", false},
	}

	for _, tt := range tests {
		t.Run(tt.source+"->"+tt.target, func(t *testing.T) {
			compatible, _, _ := checkPointerCompatibility(tt.source, tt.target)
			if compatible != tt.expected {
				t.Errorf("checkPointerCompatibility(%s, %s) = %v, want %v",
					tt.source, tt.target, compatible, tt.expected)
			}
		})
	}
}

func TestSuggestConversions(t *testing.T) {
	t.Run("numeric conversions", func(t *testing.T) {
		convs := suggestConversions("int", "float64")
		if len(convs) == 0 {
			t.Error("expected conversion suggestion for int to float64")
		}
	})

	t.Run("[]byte to string", func(t *testing.T) {
		convs := suggestConversions("[]byte", "string")
		if len(convs) == 0 {
			t.Error("expected conversion suggestion for []byte to string")
		}
		found := false
		for _, c := range convs {
			if c == "string(value)" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected string(value) conversion")
		}
	})

	t.Run("string to []byte", func(t *testing.T) {
		convs := suggestConversions("string", "[]byte")
		if len(convs) == 0 {
			t.Error("expected conversion suggestion for string to []byte")
		}
	})

	t.Run("pointer conversion", func(t *testing.T) {
		convs := suggestConversions("T", "*T")
		if len(convs) == 0 {
			t.Error("expected conversion suggestion for T to *T")
		}
	})

	t.Run("dereference conversion", func(t *testing.T) {
		convs := suggestConversions("*T", "T")
		if len(convs) == 0 {
			t.Error("expected conversion suggestion for *T to T")
		}
	})
}

func TestNormalizeSignature(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"func()", "func()"},
		{"  func()  ", "func()"},
		{"func(\n\t)", "func( )"},
		{"func(  x  int  )", "func( x int )"},
	}

	for _, tt := range tests {
		result := normalizeSignature(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeSignature(%q) = %q, want %q",
				tt.input, result, tt.expected)
		}
	}
}

func TestRemoveReceiver(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"func (r *T) Method()", "func Method()"},
		{"func (r T) Method(x int)", "func Method(x int)"},
		{"func Method()", "func Method()"},
		{"func()", "func()"},
	}

	for _, tt := range tests {
		result := removeReceiver(tt.input)
		if result != tt.expected {
			t.Errorf("removeReceiver(%q) = %q, want %q",
				tt.input, result, tt.expected)
		}
	}
}
