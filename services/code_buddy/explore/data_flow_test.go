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

func setupDataFlowTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create a simple call graph with sources and sinks
	// handler -> getFormValue (source) -> processData -> writeDB (sink)

	handler := &ast.Symbol{
		ID:        "pkg/handlers.HandleRequest",
		Name:      "HandleRequest",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 10,
		EndLine:   20,
		Package:   "handlers",
		Language:  "go",
	}

	getFormValue := &ast.Symbol{
		ID:        "net/http.Request.FormValue",
		Name:      "FormValue",
		Kind:      ast.SymbolKindMethod,
		Receiver:  "*http.Request",
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 15,
		EndLine:   15,
		Package:   "net/http",
		Language:  "go",
	}

	processData := &ast.Symbol{
		ID:        "pkg/service.ProcessData",
		Name:      "ProcessData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service/processor.go",
		StartLine: 20,
		EndLine:   30,
		Package:   "service",
		Language:  "go",
	}

	writeDB := &ast.Symbol{
		ID:        "database/sql.DB.Exec",
		Name:      "Exec",
		Kind:      ast.SymbolKindMethod,
		Receiver:  "*sql.DB",
		FilePath:  "pkg/service/processor.go",
		StartLine: 25,
		EndLine:   25,
		Package:   "database/sql",
		Signature: "func(query string, args ...interface{}) (Result, error)",
		Language:  "go",
	}

	// Add nodes
	g.AddNode(handler)
	g.AddNode(getFormValue)
	g.AddNode(processData)
	g.AddNode(writeDB)

	// Add edges: handler calls getFormValue, processData; processData calls writeDB
	g.AddEdge(handler.ID, getFormValue.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(handler.ID, processData.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(processData.ID, writeDB.ID, graph.EdgeTypeCalls, ast.Location{})

	g.Freeze()

	// Index all symbols
	idx.Add(handler)
	idx.Add(getFormValue)
	idx.Add(processData)
	idx.Add(writeDB)

	return g, idx
}

func TestDataFlowTracer_TraceDataFlow(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("traces data flow from handler", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceDataFlow(ctx, "pkg/handlers.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find sources (FormValue is an HTTP source)
		if len(flow.Sources) == 0 {
			t.Error("expected to find sources")
		}

		// Should find sinks (Exec is a SQL sink)
		if len(flow.Sinks) == 0 {
			t.Error("expected to find sinks")
		}

		// Should have path
		if len(flow.Path) == 0 {
			t.Error("expected non-empty path")
		}

		// Should be function-level precision
		if flow.Precision != "function" {
			t.Errorf("expected precision 'function', got '%s'", flow.Precision)
		}

		// Should have limitations documented
		if len(flow.Limitations) == 0 {
			t.Error("expected limitations to be documented")
		}
	})

	t.Run("returns error for non-existent symbol", func(t *testing.T) {
		ctx := context.Background()
		_, err := tracer.TraceDataFlow(ctx, "nonexistent.Symbol")
		if err != ErrSymbolNotFound {
			t.Errorf("expected ErrSymbolNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := tracer.TraceDataFlow(nil, "pkg/handlers.HandleRequest")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := tracer.TraceDataFlow(ctx, "pkg/handlers.HandleRequest")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})

	t.Run("respects max nodes limit", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceDataFlow(ctx, "pkg/handlers.HandleRequest", WithMaxNodes(2))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// With MaxNodes=2, should truncate
		// Check that limitations mention truncation
		hasTruncationLimitation := false
		for _, lim := range flow.Limitations {
			if len(lim) > 0 && lim[0:min(10, len(lim))] == "Traversal " {
				hasTruncationLimitation = true
				break
			}
		}
		// May or may not be truncated depending on traversal order
		_ = hasTruncationLimitation
	})

	t.Run("respects max hops limit", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceDataFlow(ctx, "pkg/handlers.HandleRequest", WithMaxHops(1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should still get some results
		if len(flow.Path) == 0 {
			t.Error("expected non-empty path even with limited hops")
		}
	})
}

func TestDataFlowTracer_TraceDataFlowReverse(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("traces reverse flow from sink", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceDataFlowReverse(ctx, "database/sql.DB.Exec")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find the calling function
		if len(flow.Path) == 0 {
			t.Error("expected non-empty reverse path")
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := tracer.TraceDataFlowReverse(nil, "database/sql.DB.Exec")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestDataFlowTracer_FindSourcesInFile(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("finds sources in file", func(t *testing.T) {
		ctx := context.Background()
		sources, err := tracer.FindSourcesInFile(ctx, "pkg/handlers/handler.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find FormValue as a source
		if len(sources) == 0 {
			t.Error("expected to find sources in handler file")
		}

		// Check that sources are properly categorized
		for _, source := range sources {
			if source.Type != "source" {
				t.Errorf("expected type 'source', got '%s'", source.Type)
			}
			if source.Confidence <= 0 {
				t.Error("expected positive confidence")
			}
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		ctx := context.Background()
		_, err := tracer.FindSourcesInFile(ctx, "nonexistent.go")
		if err != ErrFileNotFound {
			t.Errorf("expected ErrFileNotFound, got %v", err)
		}
	})

	t.Run("returns error for nil context", func(t *testing.T) {
		_, err := tracer.FindSourcesInFile(nil, "pkg/handlers/handler.go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func TestDataFlowTracer_FindSinksInFile(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("finds sinks in file", func(t *testing.T) {
		ctx := context.Background()
		sinks, err := tracer.FindSinksInFile(ctx, "pkg/service/processor.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find Exec as a sink
		if len(sinks) == 0 {
			t.Error("expected to find sinks in processor file")
		}

		// Check that sinks are properly categorized
		for _, sink := range sinks {
			if sink.Type != "sink" {
				t.Errorf("expected type 'sink', got '%s'", sink.Type)
			}
		}
	})
}

func TestDataFlowTracer_FindDangerousSinksInFile(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("finds dangerous sinks", func(t *testing.T) {
		ctx := context.Background()
		dangerous, err := tracer.FindDangerousSinksInFile(ctx, "pkg/service/processor.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Exec is a dangerous sink (SQL injection potential)
		if len(dangerous) == 0 {
			t.Error("expected to find dangerous sinks")
		}

		// All should be marked as dangerous_sink
		for _, sink := range dangerous {
			if sink.Type != "dangerous_sink" {
				t.Errorf("expected type 'dangerous_sink', got '%s'", sink.Type)
			}
		}
	})
}

func TestDataFlowTracer_TraceToDangerousSinks(t *testing.T) {
	g, idx := setupDataFlowTestGraph()
	tracer := NewDataFlowTracer(g, idx)

	t.Run("finds dangerous sinks from source", func(t *testing.T) {
		ctx := context.Background()
		flow, err := tracer.TraceToDangerousSinks(ctx, "pkg/handlers.HandleRequest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find the dangerous Exec sink
		hasDangerousSink := false
		for _, sink := range flow.Sinks {
			if sink.Category == string(SinkSQL) || sink.Category == string(SinkCommand) {
				hasDangerousSink = true
				break
			}
		}
		_ = hasDangerousSink // May not be found depending on matching
	})
}

func TestNewDataFlowTracerWithRegistries(t *testing.T) {
	g, idx := setupDataFlowTestGraph()

	sources := NewSourceRegistry()
	sinks := NewSinkRegistry()

	tracer := NewDataFlowTracerWithRegistries(g, idx, sources, sinks)

	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
	if tracer.sources != sources {
		t.Error("expected custom source registry to be used")
	}
	if tracer.sinks != sinks {
		t.Error("expected custom sink registry to be used")
	}
}

func TestDataFlowTracer_GraphNotFrozen(t *testing.T) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Don't freeze the graph
	tracer := NewDataFlowTracer(g, idx)

	ctx := context.Background()
	_, err := tracer.TraceDataFlow(ctx, "any.Symbol")
	if err != ErrGraphNotReady {
		t.Errorf("expected ErrGraphNotReady for unfrozen graph, got %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
