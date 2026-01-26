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

func setupMinimalContextTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a test scenario:
	// - Handler function that takes Request and returns Response
	// - Request and Response are types
	// - Handler calls validateInput and processData
	// - Handler's receiver type implements an interface

	// Types
	request := &ast.Symbol{
		ID:        "pkg/types.Request",
		Name:      "Request",
		Kind:      ast.SymbolKindStruct,
		FilePath:  "pkg/types/types.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "types",
		Language:  "go",
	}

	response := &ast.Symbol{
		ID:        "pkg/types.Response",
		Name:      "Response",
		Kind:      ast.SymbolKindStruct,
		FilePath:  "pkg/types/types.go",
		StartLine: 25,
		EndLine:   35,
		Package:   "types",
		Language:  "go",
	}

	// Interface
	handlerInterface := &ast.Symbol{
		ID:        "pkg/types.Handler",
		Name:      "Handler",
		Kind:      ast.SymbolKindInterface,
		FilePath:  "pkg/types/types.go",
		StartLine: 40,
		EndLine:   45,
		Package:   "types",
		Signature: "interface { Handle(Request) Response }",
		Language:  "go",
	}

	// Receiver type (struct implementing the interface)
	serviceStruct := &ast.Symbol{
		ID:        "pkg/service.Service",
		Name:      "Service",
		Kind:      ast.SymbolKindStruct,
		FilePath:  "pkg/service/service.go",
		StartLine: 10,
		EndLine:   15,
		Package:   "service",
		Language:  "go",
	}

	// Main handler function
	handleRequest := &ast.Symbol{
		ID:        "pkg/service.Service.HandleRequest",
		Name:      "HandleRequest",
		Kind:      ast.SymbolKindMethod,
		FilePath:  "pkg/service/service.go",
		StartLine: 20,
		EndLine:   50,
		Package:   "service",
		Signature: "func(req *Request) (*Response, error)",
		Receiver:  "*Service",
		Language:  "go",
	}

	// Callees
	validateInput := &ast.Symbol{
		ID:        "pkg/service.validateInput",
		Name:      "validateInput",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service/service.go",
		StartLine: 55,
		EndLine:   70,
		Package:   "service",
		Signature: "func(req *Request) error",
		Language:  "go",
	}

	processData := &ast.Symbol{
		ID:        "pkg/service.processData",
		Name:      "processData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service/service.go",
		StartLine: 75,
		EndLine:   90,
		Package:   "service",
		Signature: "func(data []byte) (*Response, error)",
		Language:  "go",
	}

	// External callee (should be excluded)
	fmtPrintf := &ast.Symbol{
		ID:        "fmt.Printf",
		Name:      "Printf",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "fmt/print.go",
		StartLine: 1,
		EndLine:   10,
		Package:   "fmt",
		Signature: "func(format string, a ...interface{}) (int, error)",
		Language:  "go",
	}

	// Add nodes
	g.AddNode(request)
	g.AddNode(response)
	g.AddNode(handlerInterface)
	g.AddNode(serviceStruct)
	g.AddNode(handleRequest)
	g.AddNode(validateInput)
	g.AddNode(processData)
	g.AddNode(fmtPrintf)

	// Add edges
	// HandleRequest uses Request and Response types
	g.AddEdge(handleRequest.ID, request.ID, graph.EdgeTypeParameters, ast.Location{})
	g.AddEdge(handleRequest.ID, response.ID, graph.EdgeTypeReturns, ast.Location{})

	// Service implements Handler interface
	g.AddEdge(serviceStruct.ID, handlerInterface.ID, graph.EdgeTypeImplements, ast.Location{})

	// HandleRequest calls validateInput (multiple times), processData, and fmt.Printf
	g.AddEdge(handleRequest.ID, validateInput.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 25})
	g.AddEdge(handleRequest.ID, validateInput.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 30})
	g.AddEdge(handleRequest.ID, processData.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 35})
	g.AddEdge(handleRequest.ID, fmtPrintf.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 40})

	g.Freeze()

	// Index all symbols
	idx.Add(request)
	idx.Add(response)
	idx.Add(handlerInterface)
	idx.Add(serviceStruct)
	idx.Add(handleRequest)
	idx.Add(validateInput)
	idx.Add(processData)
	idx.Add(fmtPrintf)

	return g, idx
}

func TestMinimalContextBuilder_BuildMinimalContext(t *testing.T) {
	g, idx := setupMinimalContextTestGraph()
	builder := NewMinimalContextBuilder(g, idx)

	t.Run("builds minimal context for function", func(t *testing.T) {
		ctx := context.Background()
		result, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have target
		if result.Target.ID != "pkg/service.Service.HandleRequest" {
			t.Errorf("expected target ID 'pkg/service.Service.HandleRequest', got '%s'", result.Target.ID)
		}

		// Should have types (Request, Response)
		if len(result.Types) == 0 {
			t.Error("expected to find types")
		}

		// Should have token estimate
		if result.TotalTokens <= 0 {
			t.Error("expected positive token count")
		}
	})

	t.Run("finds implemented interfaces", func(t *testing.T) {
		ctx := context.Background()
		result, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find Handler interface
		foundInterface := false
		for _, iface := range result.Interfaces {
			if iface.Name == "Handler" {
				foundInterface = true
				break
			}
		}
		if !foundInterface {
			t.Error("expected to find Handler interface")
		}
	})

	t.Run("finds key callees sorted by frequency", func(t *testing.T) {
		ctx := context.Background()
		result, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have callees
		if len(result.KeyCallees) == 0 {
			t.Error("expected to find key callees")
		}

		// validateInput should be first (called twice)
		if len(result.KeyCallees) > 0 && result.KeyCallees[0].Name != "validateInput" {
			t.Errorf("expected first callee to be 'validateInput', got '%s'", result.KeyCallees[0].Name)
		}

		// Should NOT include fmt.Printf (stdlib)
		for _, callee := range result.KeyCallees {
			if callee.Name == "Printf" {
				t.Error("expected stdlib functions to be excluded")
			}
		}
	})

	t.Run("respects token budget", func(t *testing.T) {
		ctx := context.Background()

		// Use a large budget and a small budget
		largeBudget := 10000
		smallBudget := 500

		largeResult, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest", WithTokenBudget(largeBudget))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		smallResult, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest", WithTokenBudget(smallBudget))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Small budget should have fewer items or smaller total
		// (at minimum, target is always included)
		smallTotal := len(smallResult.Types) + len(smallResult.Interfaces) + len(smallResult.KeyCallees)
		largeTotal := len(largeResult.Types) + len(largeResult.Interfaces) + len(largeResult.KeyCallees)

		// With a smaller budget, we should include fewer items
		if smallTotal > largeTotal {
			t.Errorf("expected small budget to include fewer items: small=%d, large=%d", smallTotal, largeTotal)
		}
	})

	t.Run("returns error for non-existent symbol", func(t *testing.T) {
		ctx := context.Background()
		_, err := builder.BuildMinimalContext(ctx, "nonexistent.Symbol")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := builder.BuildMinimalContext(nil, "pkg/service.Service.HandleRequest")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("returns error for empty symbolID", func(t *testing.T) {
		ctx := context.Background()
		_, err := builder.BuildMinimalContext(ctx, "")
		if err == nil {
			t.Error("expected error for empty symbolID")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := builder.BuildMinimalContext(ctx, "pkg/service.Service.HandleRequest")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestMinimalContextBuilder_GraphNotFrozen(t *testing.T) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Don't freeze the graph
	builder := NewMinimalContextBuilder(g, idx)

	ctx := context.Background()
	_, err := builder.BuildMinimalContext(ctx, "any.Symbol")
	if err != ErrGraphNotReady {
		t.Errorf("expected ErrGraphNotReady for unfrozen graph, got %v", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Run("returns minimum for short code", func(t *testing.T) {
		tokens := EstimateTokens("a")
		if tokens < minTokensPerBlock {
			t.Errorf("expected at least %d tokens, got %d", minTokensPerBlock, tokens)
		}
	})

	t.Run("returns zero for empty code", func(t *testing.T) {
		tokens := EstimateTokens("")
		if tokens != 0 {
			t.Errorf("expected 0 tokens for empty code, got %d", tokens)
		}
	})

	t.Run("scales with code length", func(t *testing.T) {
		shortCode := "func foo() {}"
		longCode := "func foo() {\n\t// This is a longer function\n\tfmt.Println(\"hello\")\n\treturn\n}"

		shortTokens := EstimateTokens(shortCode)
		longTokens := EstimateTokens(longCode)

		if longTokens <= shortTokens {
			t.Error("expected longer code to have more tokens")
		}
	})
}

func TestEstimateTokensForSymbol(t *testing.T) {
	t.Run("returns zero for nil symbol", func(t *testing.T) {
		tokens := EstimateTokensForSymbol(nil)
		if tokens != 0 {
			t.Errorf("expected 0 tokens for nil symbol, got %d", tokens)
		}
	})

	t.Run("estimates based on line count", func(t *testing.T) {
		shortSym := &ast.Symbol{
			StartLine: 1,
			EndLine:   5,
		}
		longSym := &ast.Symbol{
			StartLine: 1,
			EndLine:   100,
		}

		shortTokens := EstimateTokensForSymbol(shortSym)
		longTokens := EstimateTokensForSymbol(longSym)

		if longTokens <= shortTokens {
			t.Error("expected longer symbol to have more tokens")
		}
	})

	t.Run("includes signature tokens", func(t *testing.T) {
		withSig := &ast.Symbol{
			StartLine: 1,
			EndLine:   5,
			Signature: "func(ctx context.Context, input *Input) (*Output, error)",
		}
		withoutSig := &ast.Symbol{
			StartLine: 1,
			EndLine:   5,
		}

		withSigTokens := EstimateTokensForSymbol(withSig)
		withoutSigTokens := EstimateTokensForSymbol(withoutSig)

		if withSigTokens <= withoutSigTokens {
			t.Error("expected symbol with signature to have more tokens")
		}
	})
}

func TestExtractTypeNamesFromSignature(t *testing.T) {
	t.Run("extracts simple types", func(t *testing.T) {
		types := extractTypeNamesFromSignature("func(req *Request) (*Response, error)")

		hasRequest := false
		hasResponse := false
		for _, typ := range types {
			if typ == "Request" {
				hasRequest = true
			}
			if typ == "Response" {
				hasResponse = true
			}
		}

		if !hasRequest {
			t.Error("expected to find Request type")
		}
		if !hasResponse {
			t.Error("expected to find Response type")
		}
	})

	t.Run("excludes builtin types", func(t *testing.T) {
		types := extractTypeNamesFromSignature("func(s string, i int) error")

		for _, typ := range types {
			if typ == "string" || typ == "int" || typ == "error" {
				t.Errorf("expected builtin type '%s' to be excluded", typ)
			}
		}
	})

	t.Run("handles qualified types", func(t *testing.T) {
		types := extractTypeNamesFromSignature("func(ctx context.Context) http.Response")

		hasContext := false
		hasResponse := false
		for _, typ := range types {
			if typ == "Context" {
				hasContext = true
			}
			if typ == "Response" {
				hasResponse = true
			}
		}

		if !hasContext {
			t.Error("expected to find Context type")
		}
		if !hasResponse {
			t.Error("expected to find Response type")
		}
	})

	t.Run("handles empty signature", func(t *testing.T) {
		types := extractTypeNamesFromSignature("")
		if len(types) != 0 {
			t.Errorf("expected empty result for empty signature, got %v", types)
		}
	})
}

func TestCleanReceiverType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"*Service", "Service"},
		{"Service", "Service"},
		{"*http.Request", "Request"},
		{"http.Request", "Request"},
		{"*pkg/handlers.Handler", "Handler"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cleanReceiverType(tt.input)
			if result != tt.expected {
				t.Errorf("cleanReceiverType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsStdLibOrExternal(t *testing.T) {
	tests := []struct {
		pkg      string
		expected bool
	}{
		{"fmt", true},
		{"os", true},
		{"net/http", true},
		{"context", true},
		{"encoding/json", true},
		{"myapp/handlers", false},
		{"github.com/user/pkg", false},
		{"service", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			sym := &ast.Symbol{Package: tt.pkg}
			result := isStdLibOrExternal(sym)
			if result != tt.expected {
				t.Errorf("isStdLibOrExternal(%q) = %v, want %v", tt.pkg, result, tt.expected)
			}
		})
	}
}

func TestIsTypeSymbol(t *testing.T) {
	tests := []struct {
		kind     ast.SymbolKind
		expected bool
	}{
		{ast.SymbolKindStruct, true},
		{ast.SymbolKindInterface, true},
		{ast.SymbolKindType, true},
		{ast.SymbolKindFunction, false},
		{ast.SymbolKindMethod, false},
		{ast.SymbolKindVariable, false},
		{ast.SymbolKindConstant, false},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			sym := &ast.Symbol{Kind: tt.kind}
			result := isTypeSymbol(sym)
			if result != tt.expected {
				t.Errorf("isTypeSymbol(%q) = %v, want %v", tt.kind.String(), result, tt.expected)
			}
		})
	}
}
