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

func setupErrorFlowTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a call graph with error handling patterns
	// handler -> validateInput (returns error) -> errors.New (origin)
	// handler -> handleError (handler)

	handler := &ast.Symbol{
		ID:        "pkg/handlers.HandleRequest",
		Name:      "HandleRequest",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 10,
		EndLine:   25,
		Package:   "handlers",
		Signature: "func(w http.ResponseWriter, r *http.Request)",
		Language:  "go",
	}

	validateInput := &ast.Symbol{
		ID:        "pkg/handlers.ValidateInput",
		Name:      "ValidateInput",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 30,
		EndLine:   45,
		Package:   "handlers",
		Signature: "func(input string) error",
		Language:  "go",
	}

	errorsNew := &ast.Symbol{
		ID:        "errors.New",
		Name:      "New",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 35,
		EndLine:   35,
		Package:   "errors",
		Signature: "func(text string) error",
		Language:  "go",
	}

	handleError := &ast.Symbol{
		ID:        "pkg/handlers.handleError",
		Name:      "handleError",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 50,
		EndLine:   60,
		Package:   "handlers",
		Signature: "func(w http.ResponseWriter, err error)",
		Language:  "go",
	}

	fmtErrorf := &ast.Symbol{
		ID:        "fmt.Errorf",
		Name:      "Errorf",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 40,
		EndLine:   40,
		Package:   "fmt",
		Signature: "func(format string, a ...interface{}) error",
		Language:  "go",
	}

	// Add nodes
	g.AddNode(handler)
	g.AddNode(validateInput)
	g.AddNode(errorsNew)
	g.AddNode(handleError)
	g.AddNode(fmtErrorf)

	// Add edges
	g.AddEdge(handler.ID, validateInput.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(handler.ID, handleError.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(validateInput.ID, errorsNew.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(validateInput.ID, fmtErrorf.ID, graph.EdgeTypeCalls, ast.Location{})

	g.Freeze()

	// Index all symbols
	idx.Add(handler)
	idx.Add(validateInput)
	idx.Add(errorsNew)
	idx.Add(handleError)
	idx.Add(fmtErrorf)

	return g, idx
}

func TestErrorFlowTracer_TraceErrorFlow(t *testing.T) {
	g, idx := setupErrorFlowTestGraph()
	tracer := NewErrorFlowTracer(g, idx)

	t.Run("traces error flow from handler", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceErrorFlow(ctx, "pkg/handlers.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find error origins (errors.New, fmt.Errorf)
		if len(flow.Origins) == 0 {
			t.Error("expected to find error origins")
		}

		// Should find error handlers (handleError)
		if len(flow.Handlers) == 0 {
			t.Error("expected to find error handlers")
		}

		// Should find error escapes (functions returning error)
		if len(flow.Escapes) == 0 {
			t.Error("expected to find error escapes")
		}
	})

	t.Run("returns error for non-existent symbol", func(t *testing.T) {
		ctx := context.Background()
		_, err := tracer.TraceErrorFlow(ctx, "nonexistent.Symbol")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := tracer.TraceErrorFlow(nil, "pkg/handlers.HandleRequest")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := tracer.TraceErrorFlow(ctx, "pkg/handlers.HandleRequest")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("respects max nodes limit", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceErrorFlow(ctx, "pkg/handlers.HandleRequest", WithMaxNodes(2))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should still return valid flow
		if flow == nil {
			t.Error("expected non-nil flow")
		}
	})
}

func TestErrorFlowTracer_FindErrorOrigins(t *testing.T) {
	g, idx := setupErrorFlowTestGraph()
	tracer := NewErrorFlowTracer(g, idx)

	t.Run("finds error origins in file", func(t *testing.T) {
		ctx := context.Background()
		origins, err := tracer.FindErrorOrigins(ctx, "pkg/handlers/handler.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// All should be marked as origin type
		for _, origin := range origins {
			if origin.Type != "origin" {
				t.Errorf("expected type 'origin', got '%s'", origin.Type)
			}
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		ctx := context.Background()
		_, err := tracer.FindErrorOrigins(ctx, "nonexistent.go")
		if err != ErrFileNotFound {
			t.Errorf("expected ErrFileNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := tracer.FindErrorOrigins(nil, "pkg/handlers/handler.go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestErrorFlowTracer_FindUnhandledErrors(t *testing.T) {
	g, idx := setupErrorFlowTestGraph()
	tracer := NewErrorFlowTracer(g, idx)

	t.Run("finds unhandled errors", func(t *testing.T) {
		ctx := context.Background()
		unhandled, err := tracer.FindUnhandledErrors(ctx, "pkg/handlers.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find escape points
		if len(unhandled) == 0 {
			t.Error("expected to find unhandled error points")
		}

		// All should be escape type
		for _, point := range unhandled {
			if point.Type != "escape" {
				t.Errorf("expected type 'escape', got '%s'", point.Type)
			}
		}
	})
}

func TestErrorFlowTracer_FindErrorHandlers(t *testing.T) {
	g, idx := setupErrorFlowTestGraph()
	tracer := NewErrorFlowTracer(g, idx)

	t.Run("finds error handlers in file", func(t *testing.T) {
		ctx := context.Background()
		handlers, err := tracer.FindErrorHandlers(ctx, "pkg/handlers/handler.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find handleError
		if len(handlers) == 0 {
			t.Error("expected to find error handlers")
		}

		// Check for handleError specifically
		foundHandler := false
		for _, h := range handlers {
			if h.Function == "handleError" {
				foundHandler = true
				break
			}
		}
		if !foundHandler {
			t.Error("expected to find handleError function")
		}
	})
}

func TestErrorFlowTracer_GetErrorFlowSummary(t *testing.T) {
	g, idx := setupErrorFlowTestGraph()
	tracer := NewErrorFlowTracer(g, idx)

	t.Run("provides error flow summary", func(t *testing.T) {
		ctx := context.Background()
		summary, err := tracer.GetErrorFlowSummary(ctx, "pkg/handlers/handler.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have all three categories
		if summary == nil {
			t.Fatal("expected non-nil summary")
		}

		// Should find handlers
		if len(summary.Handlers) == 0 {
			t.Error("expected to find handlers in summary")
		}

		// Should find escapes (functions returning error)
		if len(summary.Escapes) == 0 {
			t.Error("expected to find escapes in summary")
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		ctx := context.Background()
		_, err := tracer.GetErrorFlowSummary(ctx, "nonexistent.go")
		if err != ErrFileNotFound {
			t.Errorf("expected ErrFileNotFound, got %v", err)
		}
	})
}

func TestErrorFlowTracer_IsErrorOrigin(t *testing.T) {
	tracer := NewErrorFlowTracer(nil, nil)

	t.Run("detects errors.New", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:    "New",
			Package: "errors",
		}
		if !tracer.isErrorOrigin(sym) {
			t.Error("expected errors.New to be detected as error origin")
		}
	})

	t.Run("detects fmt.Errorf", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:    "Errorf",
			Package: "fmt",
		}
		if !tracer.isErrorOrigin(sym) {
			t.Error("expected fmt.Errorf to be detected as error origin")
		}
	})

	t.Run("detects custom error constructor", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:      "NewValidationError",
			Kind:      ast.SymbolKindFunction,
			Signature: "func(msg string) error",
		}
		if !tracer.isErrorOrigin(sym) {
			t.Error("expected NewValidationError to be detected as error origin")
		}
	})

	t.Run("rejects regular function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:    "Process",
			Package: "service",
		}
		if tracer.isErrorOrigin(sym) {
			t.Error("expected regular function not to be detected as error origin")
		}
	})
}

func TestErrorFlowTracer_IsErrorHandler(t *testing.T) {
	tracer := NewErrorFlowTracer(nil, nil)

	t.Run("detects handleError function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name: "handleError",
		}
		if !tracer.isErrorHandler(sym) {
			t.Error("expected handleError to be detected as error handler")
		}
	})

	t.Run("detects HandleError function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name: "HandleError",
		}
		if !tracer.isErrorHandler(sym) {
			t.Error("expected HandleError to be detected as error handler")
		}
	})

	t.Run("detects logError function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name: "logError",
		}
		if !tracer.isErrorHandler(sym) {
			t.Error("expected logError to be detected as error handler")
		}
	})

	t.Run("rejects regular function", func(t *testing.T) {
		sym := &ast.Symbol{
			Name: "processData",
		}
		if tracer.isErrorHandler(sym) {
			t.Error("expected regular function not to be detected as error handler")
		}
	})
}

func TestErrorFlowTracer_IsErrorEscape(t *testing.T) {
	tracer := NewErrorFlowTracer(nil, nil)

	t.Run("detects function returning error", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:      "ValidateInput",
			Kind:      ast.SymbolKindFunction,
			Signature: "func(input string) error",
		}
		if !tracer.isErrorEscape(sym) {
			t.Error("expected function returning error to be detected as escape")
		}
	})

	t.Run("rejects function not returning error", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:      "GetValue",
			Kind:      ast.SymbolKindFunction,
			Signature: "func() string",
		}
		if tracer.isErrorEscape(sym) {
			t.Error("expected function not returning error not to be detected as escape")
		}
	})

	t.Run("rejects error handler", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:      "handleError",
			Kind:      ast.SymbolKindFunction,
			Signature: "func(err error)",
		}
		// handleError is a handler, not an escape
		if tracer.isErrorEscape(sym) {
			t.Error("expected error handler not to be detected as escape")
		}
	})
}

func TestErrorFlowTracer_GraphNotFrozen(t *testing.T) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Don't freeze the graph
	tracer := NewErrorFlowTracer(g, idx)

	ctx := context.Background()
	_, err := tracer.TraceErrorFlow(ctx, "any.Symbol")
	if err != ErrGraphNotReady {
		t.Errorf("expected ErrGraphNotReady for unfrozen graph, got %v", err)
	}
}
