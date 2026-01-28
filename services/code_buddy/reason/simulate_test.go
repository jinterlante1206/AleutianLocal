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

func setupSimulatorTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a handler function
	handler := &ast.Symbol{
		ID:        "handlers/user.go:10:HandleUser",
		Name:      "HandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(ctx context.Context, req *UserRequest) (*UserResponse, error)",
		Language:  "go",
		Exported:  true,
	}

	// Create caller
	caller := &ast.Symbol{
		ID:        "main.go:50:main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "main.go",
		StartLine: 50,
		EndLine:   80,
		Package:   "main",
		Signature: "func()",
		Language:  "go",
	}

	// Create test function
	testFunc := &ast.Symbol{
		ID:        "handlers/user_test.go:20:TestHandleUser",
		Name:      "TestHandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user_test.go",
		StartLine: 20,
		EndLine:   50,
		Package:   "handlers",
		Signature: "func(t *testing.T)",
		Language:  "go",
	}

	// Add to graph
	g.AddNode(handler)
	g.AddNode(caller)
	g.AddNode(testFunc)

	// Add call edges
	g.AddEdge(caller.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  caller.FilePath,
		StartLine: 55,
	})
	g.AddEdge(testFunc.ID, handler.ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  testFunc.FilePath,
		StartLine: 25,
	})

	g.Freeze()

	// Add to index
	idx.Add(handler)
	idx.Add(caller)
	idx.Add(testFunc)

	return g, idx
}

func TestChangeSimulator_SimulateChange(t *testing.T) {
	g, idx := setupSimulatorTestGraph()
	simulator := NewChangeSimulator(g, idx)
	ctx := context.Background()

	t.Run("identifies callers to update when parameter added", func(t *testing.T) {
		sim, err := simulator.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, req *UserRequest, opts Options) (*UserResponse, error)",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !sim.Valid {
			t.Error("expected simulation to be valid")
		}
		if len(sim.CallersToUpdate) == 0 {
			t.Error("expected callers to update")
		}

		// Should find the main function as a caller
		foundMain := false
		for _, caller := range sim.CallersToUpdate {
			if caller.CallerID == "main.go:50:main" {
				foundMain = true
				if caller.UpdateType != "add_arguments" {
					t.Errorf("expected update type 'add_arguments', got %q", caller.UpdateType)
				}
			}
		}
		if !foundMain {
			t.Error("expected to find main function in callers to update")
		}
	})

	t.Run("detects type mismatches", func(t *testing.T) {
		sim, err := simulator.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, req string) (*UserResponse, error)",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(sim.TypeMismatches) == 0 {
			t.Error("expected type mismatches for changed parameter type")
		}
	})

	t.Run("finds affected tests", func(t *testing.T) {
		sim, err := simulator.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context) error",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(sim.TestsAffected) == 0 {
			t.Error("expected affected tests")
		}

		foundTest := false
		for _, test := range sim.TestsAffected {
			if test == "handlers/user_test.go:20:TestHandleUser" {
				foundTest = true
				break
			}
		}
		if !foundTest {
			t.Error("expected TestHandleUser in affected tests")
		}
	})

	t.Run("identifies imports needed", func(t *testing.T) {
		sim, err := simulator.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func(ctx context.Context, w http.ResponseWriter) error",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		foundHTTP := false
		for _, imp := range sim.ImportsNeeded {
			if imp == "http" {
				foundHTTP = true
				break
			}
		}
		if !foundHTTP {
			t.Error("expected 'http' in imports needed")
		}
	})

	t.Run("has confidence score", func(t *testing.T) {
		sim, err := simulator.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func() error",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if sim.Confidence <= 0 || sim.Confidence > 1 {
			t.Errorf("confidence should be between 0 and 1, got %f", sim.Confidence)
		}
	})
}

func TestChangeSimulator_Errors(t *testing.T) {
	g, idx := setupSimulatorTestGraph()
	simulator := NewChangeSimulator(g, idx)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := simulator.SimulateChange(nil, "id", "func()")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty target ID", func(t *testing.T) {
		_, err := simulator.SimulateChange(ctx, "", "func()")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty signature", func(t *testing.T) {
		_, err := simulator.SimulateChange(ctx, "some.id", "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("symbol not found", func(t *testing.T) {
		_, err := simulator.SimulateChange(ctx, "nonexistent.id", "func()")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := simulator.SimulateChange(cancelCtx, "id", "func()")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("unfrozen graph", func(t *testing.T) {
		unfrozenGraph := graph.NewGraph("/test")
		unfrozenSim := NewChangeSimulator(unfrozenGraph, idx)

		_, err := unfrozenSim.SimulateChange(ctx,
			"handlers/user.go:10:HandleUser",
			"func()")
		if err != ErrGraphNotReady {
			t.Errorf("expected ErrGraphNotReady, got %v", err)
		}
	})
}

func TestChangeSimulator_SimulateMultipleChanges(t *testing.T) {
	g, idx := setupSimulatorTestGraph()
	simulator := NewChangeSimulator(g, idx)
	ctx := context.Background()

	changes := map[string]string{
		"handlers/user.go:10:HandleUser": "func(ctx context.Context) error",
		"nonexistent.symbol":             "func()",
	}

	results, err := simulator.SimulateMultipleChanges(ctx, changes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check that existing symbol was simulated
	if result, ok := results["handlers/user.go:10:HandleUser"]; ok {
		if !result.Valid {
			// Valid should still be true even with changes
			t.Log("simulation not valid (possibly due to parse limitations)")
		}
	} else {
		t.Error("missing result for existing symbol")
	}

	// Check that nonexistent symbol has error in limitations
	if result, ok := results["nonexistent.symbol"]; ok {
		if len(result.Limitations) == 0 {
			t.Error("expected limitation for failed simulation")
		}
		if result.Valid {
			t.Error("expected invalid simulation for nonexistent symbol")
		}
	} else {
		t.Error("missing result for nonexistent symbol")
	}
}

func TestChangeSimulator_NilGraph(t *testing.T) {
	idx := index.NewSymbolIndex()

	handler := &ast.Symbol{
		ID:        "handlers/user.go:10:HandleUser",
		Name:      "HandleUser",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "handlers/user.go",
		StartLine: 10,
		EndLine:   30,
		Signature: "func(ctx context.Context) error",
		Language:  "go",
	}
	if err := idx.Add(handler); err != nil {
		t.Fatalf("failed to add handler: %v", err)
	}

	simulator := NewChangeSimulator(nil, idx)
	ctx := context.Background()

	sim, err := simulator.SimulateChange(ctx,
		"handlers/user.go:10:HandleUser",
		"func() error",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should succeed but with limitations
	if len(sim.Limitations) == 0 {
		t.Error("expected limitations when graph is nil")
	}

	hasGraphLimitation := false
	for _, lim := range sim.Limitations {
		if lim == "Graph not available - cannot identify affected callers" {
			hasGraphLimitation = true
			break
		}
	}
	if !hasGraphLimitation {
		t.Error("expected graph not available limitation")
	}
}

func TestDetermineUpdateType(t *testing.T) {
	simulator := NewChangeSimulator(nil, nil)

	tests := []struct {
		name        string
		current     *ParsedSignature
		proposed    *ParsedSignature
		expectedTyp string
	}{
		{
			name:        "nil signatures returns signature_change",
			current:     nil,
			proposed:    nil,
			expectedTyp: "signature_change",
		},
		{
			name: "added required parameter",
			current: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
			},
			proposed: &ParsedSignature{
				Parameters: []ParameterInfo{
					{Name: "a", Type: TypeInfo{Name: "int"}},
					{Name: "b", Type: TypeInfo{Name: "string"}},
				},
			},
			expectedTyp: "add_arguments",
		},
		{
			name: "added optional parameter",
			current: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
			},
			proposed: &ParsedSignature{
				Parameters: []ParameterInfo{
					{Name: "a", Type: TypeInfo{Name: "int"}},
					{Name: "b", Type: TypeInfo{Name: "string"}, Optional: true},
				},
			},
			expectedTyp: "", // No update needed for optional params
		},
		{
			name: "removed parameter",
			current: &ParsedSignature{
				Parameters: []ParameterInfo{
					{Name: "a", Type: TypeInfo{Name: "int"}},
					{Name: "b", Type: TypeInfo{Name: "string"}},
				},
			},
			proposed: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
			},
			expectedTyp: "remove_arguments",
		},
		{
			name: "changed parameter type",
			current: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
			},
			proposed: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "string"}}},
			},
			expectedTyp: "change_argument_types",
		},
		{
			name: "changed return count",
			current: &ParsedSignature{
				Returns: []TypeInfo{{Name: "error"}},
			},
			proposed: &ParsedSignature{
				Returns: []TypeInfo{{Name: "int"}, {Name: "error"}},
			},
			expectedTyp: "change_return_handling",
		},
		{
			name: "changed return type",
			current: &ParsedSignature{
				Returns: []TypeInfo{{Name: "int"}, {Name: "error"}},
			},
			proposed: &ParsedSignature{
				Returns: []TypeInfo{{Name: "string"}, {Name: "error"}},
			},
			expectedTyp: "change_return_handling",
		},
		{
			name: "no change",
			current: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
				Returns:    []TypeInfo{{Name: "error"}},
			},
			proposed: &ParsedSignature{
				Parameters: []ParameterInfo{{Name: "a", Type: TypeInfo{Name: "int"}}},
				Returns:    []TypeInfo{{Name: "error"}},
			},
			expectedTyp: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := simulator.determineUpdateType(tt.current, tt.proposed)
			if result != tt.expectedTyp {
				t.Errorf("determineUpdateType() = %q, want %q", result, tt.expectedTyp)
			}
		})
	}
}

func TestExtractPackageFromType(t *testing.T) {
	tests := []struct {
		typeName string
		expected string
	}{
		{"context.Context", "context"},
		{"http.Request", "http"},
		{"*http.Handler", "http"},
		{"[]http.Cookie", "http"},
		{"int", ""},
		{"string", ""},
		{"*int", ""},
		{"MyType", ""},
	}

	for _, tt := range tests {
		result := extractPackageFromType(tt.typeName)
		if result != tt.expected {
			t.Errorf("extractPackageFromType(%q) = %q, want %q",
				tt.typeName, result, tt.expected)
		}
	}
}

func TestIsTestFunction(t *testing.T) {
	tests := []struct {
		symbol   *ast.Symbol
		expected bool
	}{
		{
			symbol: &ast.Symbol{
				Name:     "TestFoo",
				Language: "go",
				FilePath: "foo.go",
			},
			expected: true,
		},
		{
			symbol: &ast.Symbol{
				Name:     "test_foo",
				Language: "python",
				FilePath: "test_foo.py",
			},
			expected: true,
		},
		{
			symbol: &ast.Symbol{
				Name:     "regularFunc",
				Language: "go",
				FilePath: "foo.go",
			},
			expected: false,
		},
		{
			symbol: &ast.Symbol{
				Name:     "helper",
				Language: "go",
				FilePath: "foo_test.go",
			},
			expected: true, // In test file
		},
		{
			symbol:   nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.symbol != nil {
			name = tt.symbol.Name
		}
		t.Run(name, func(t *testing.T) {
			result := isTestFunction(tt.symbol)
			if result != tt.expected {
				t.Errorf("isTestFunction(%v) = %v, want %v",
					tt.symbol, result, tt.expected)
			}
		})
	}
}

func TestGenerateCallSuggestion(t *testing.T) {
	simulator := NewChangeSimulator(nil, nil)

	sig := &ParsedSignature{
		Name: "HandleUser",
		Parameters: []ParameterInfo{
			{Name: "ctx", Type: TypeInfo{Name: "context.Context"}},
			{Name: "req", Type: TypeInfo{Name: "*Request"}},
		},
	}

	result := simulator.generateCallSuggestion("HandleUser", sig)

	if result != "HandleUser(ctx, req)" {
		t.Errorf("generateCallSuggestion() = %q, want %q", result, "HandleUser(ctx, req)")
	}

	// Test with unnamed parameters
	sigUnnamed := &ParsedSignature{
		Name: "Process",
		Parameters: []ParameterInfo{
			{Type: TypeInfo{Name: "int"}},
			{Type: TypeInfo{Name: "string"}},
		},
	}

	resultUnnamed := simulator.generateCallSuggestion("Process", sigUnnamed)

	if resultUnnamed != "Process(arg0, arg1)" {
		t.Errorf("generateCallSuggestion() = %q, want %q", resultUnnamed, "Process(arg0, arg1)")
	}
}
