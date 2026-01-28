// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust_flow

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// createTestGraph creates a graph with a simple vulnerable flow:
// HTTP input -> process -> SQL query (no sanitizer)
func createTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// HTTP handler - source of untrusted input
	httpHandler := &ast.Symbol{
		ID:        "handlers.HandleSearch",
		Name:      "HandleSearch",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/search.go",
		Package:   "handlers",
		StartLine: 10,
		Signature: "func HandleSearch(c *gin.Context)",
	}

	// Query parameter access - recognized source
	queryAccess := &ast.Symbol{
		ID:        "handlers.HandleSearch.Query",
		Name:      "Query",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/search.go",
		Package:   "handlers",
		StartLine: 11,
		Receiver:  "*gin.Context",
		Signature: "func (c *gin.Context) Query(key string) string",
	}

	// Process function (transform)
	processFunc := &ast.Symbol{
		ID:        "handlers.processSearch",
		Name:      "processSearch",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/search.go",
		Package:   "handlers",
		StartLine: 20,
		Signature: "func processSearch(query string) string",
	}

	// SQL query - dangerous sink
	sqlExec := &ast.Symbol{
		ID:        "db.Exec",
		Name:      "Exec",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "db/queries.go",
		Package:   "database/sql",
		StartLine: 50,
		Receiver:  "*sql.DB",
		Signature: "func (db *sql.DB) Exec(query string, args ...interface{}) (Result, error)",
	}

	// Add nodes
	g.AddNode(httpHandler)
	g.AddNode(queryAccess)
	g.AddNode(processFunc)
	g.AddNode(sqlExec)

	// Add to index
	idx.Add(httpHandler)
	idx.Add(queryAccess)
	idx.Add(processFunc)
	idx.Add(sqlExec)

	// Add edges: handler -> queryAccess -> process -> sqlExec
	g.AddEdge(httpHandler.ID, queryAccess.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 11})
	g.AddEdge(queryAccess.ID, processFunc.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 12})
	g.AddEdge(processFunc.ID, sqlExec.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 25})

	g.Freeze()
	return g, idx
}

// createTestGraphWithSanitizer creates a graph with a safe flow:
// HTTP input -> sanitizer -> SQL query (sanitized)
func createTestGraphWithSanitizer() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Query parameter access - recognized source
	queryAccess := &ast.Symbol{
		ID:        "handlers.HandleSearch.Query",
		Name:      "Query",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/search.go",
		Package:   "handlers",
		StartLine: 11,
		Receiver:  "*gin.Context",
		Signature: "func (c *gin.Context) Query(key string) string",
	}

	// Integer parsing - sanitizer
	atoi := &ast.Symbol{
		ID:        "strconv.Atoi",
		Name:      "Atoi",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "strconv/atoi.go",
		Package:   "strconv",
		StartLine: 100,
		Signature: "func Atoi(s string) (int, error)",
	}

	// SQL query - dangerous sink
	sqlExec := &ast.Symbol{
		ID:        "db.Exec",
		Name:      "Exec",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "db/queries.go",
		Package:   "database/sql",
		StartLine: 50,
		Receiver:  "*sql.DB",
		Signature: "func (db *sql.DB) Exec(query string, args ...interface{}) (Result, error)",
	}

	// Add nodes
	g.AddNode(queryAccess)
	g.AddNode(atoi)
	g.AddNode(sqlExec)

	// Add to index
	idx.Add(queryAccess)
	idx.Add(atoi)
	idx.Add(sqlExec)

	// Add edges: queryAccess -> atoi -> sqlExec
	g.AddEdge(queryAccess.ID, atoi.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 12})
	g.AddEdge(atoi.ID, sqlExec.ID, graph.EdgeTypeCalls, ast.Location{StartLine: 15})

	g.Freeze()
	return g, idx
}

func TestInputTracer_TraceUserInput_DetectsVulnerability(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query")

	if err != nil {
		t.Fatalf("TraceUserInput failed: %v", err)
	}

	// Should detect SQL sink
	if len(trace.Sinks) == 0 {
		t.Error("Expected to find SQL sink")
	}

	// Should detect vulnerability (untrusted input -> SQL without sanitizer)
	if len(trace.Vulnerabilities) == 0 {
		t.Error("Expected to find vulnerability")
	}

	// Check vulnerability details
	for _, vuln := range trace.Vulnerabilities {
		// Both "sql" and "database" categories map to CWE-89
		if vuln.CWE != "CWE-89" {
			t.Errorf("Expected CWE-89 (SQL Injection), got %s", vuln.CWE)
		}
		// Both "sql" and "database" categories map to CRITICAL severity
		if vuln.Severity != safety.SeverityCritical {
			t.Errorf("Expected CRITICAL severity, got %s", vuln.Severity)
		}
		if !vuln.DataFlowProven {
			t.Error("Expected DataFlowProven to be true")
		}
	}
}

func TestInputTracer_TraceUserInput_NoVulnerabilityWithSanitizer(t *testing.T) {
	g, idx := createTestGraphWithSanitizer()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query")

	if err != nil {
		t.Fatalf("TraceUserInput failed: %v", err)
	}

	// Should detect sanitizer
	if len(trace.Sanitizers) == 0 {
		t.Error("Expected to find sanitizer (strconv.Atoi)")
	}

	// Should NOT detect vulnerability (data is sanitized)
	if len(trace.Vulnerabilities) > 0 {
		t.Errorf("Expected no vulnerabilities with sanitizer, got %d", len(trace.Vulnerabilities))
	}
}

func TestInputTracer_TraceUserInput_ContextCancellation(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query")

	if err != safety.ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
}

func TestInputTracer_TraceUserInput_SymbolNotFound(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	_, err := tracer.TraceUserInput(ctx, "nonexistent.Symbol")

	if err != safety.ErrSymbolNotFound {
		t.Errorf("Expected ErrSymbolNotFound, got %v", err)
	}
}

func TestInputTracer_TraceUserInput_GraphNotFrozen(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	// Don't freeze the graph
	sym := &ast.Symbol{ID: "test", Name: "test", Kind: ast.SymbolKindFunction, Language: "go"}
	g.AddNode(sym)
	idx.Add(sym)

	tracer := NewInputTracer(g, idx)
	ctx := context.Background()

	_, err := tracer.TraceUserInput(ctx, "test")

	if err != safety.ErrGraphNotReady {
		t.Errorf("Expected ErrGraphNotReady, got %v", err)
	}
}

func TestInputTracer_TraceUserInput_WithOptions(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	// Test with both sql and database categories since explore package uses "database"
	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query",
		safety.WithMaxDepth(5),
		safety.WithMaxNodes(100),
		safety.WithSinkCategories("sql", "database"),
	)

	if err != nil {
		t.Fatalf("TraceUserInput failed: %v", err)
	}

	// Should still detect SQL/database sink with limited depth
	sinkFound := false
	for _, sink := range trace.Sinks {
		if sink.Category == "sql" || sink.Category == "database" {
			sinkFound = true
			break
		}
	}
	if !sinkFound {
		t.Errorf("Expected to find SQL/database sink with category filter, got sinks: %+v", trace.Sinks)
	}
}

func TestInputTracer_TraceUserInput_Performance(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	start := time.Now()

	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query")

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("TraceUserInput failed: %v", err)
	}

	// Target: < 200ms
	if elapsed > 200*time.Millisecond {
		t.Errorf("TraceUserInput took %v, expected < 200ms", elapsed)
	}

	// Check duration is recorded
	if trace.Duration == 0 {
		t.Error("Expected duration to be recorded")
	}
}

func TestInputTracer_TraceUserInput_Timeout(t *testing.T) {
	g, idx := createTestGraph()
	tracer := NewInputTracer(g, idx)

	ctx := context.Background()
	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleSearch.Query",
		safety.WithResourceLimits(safety.ResourceLimits{
			Timeout:  1 * time.Nanosecond, // Very short timeout
			MaxNodes: 1000,
			MaxDepth: 10,
		}),
	)

	// Should return partial results, not error
	if err != nil && err != safety.ErrContextCanceled {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Trace should exist (partial or complete)
	if trace == nil {
		t.Error("Expected trace result even with timeout")
	}
}

func TestSanitizerRegistry_MatchSanitizer(t *testing.T) {
	registry := NewSanitizerRegistry()

	tests := []struct {
		name          string
		symbol        *ast.Symbol
		expectMatch   bool
		expectSafeFor []string
	}{
		{
			name: "strconv.Atoi is a sanitizer",
			symbol: &ast.Symbol{
				Name:     "Atoi",
				Package:  "strconv",
				Language: "go",
			},
			expectMatch:   true,
			expectSafeFor: []string{"sql", "command", "path"},
		},
		{
			name: "html.EscapeString is XSS sanitizer",
			symbol: &ast.Symbol{
				Name:     "EscapeString",
				Package:  "html",
				Language: "go",
			},
			expectMatch:   true,
			expectSafeFor: []string{"xss"},
		},
		{
			name: "filepath.Clean is partial path sanitizer",
			symbol: &ast.Symbol{
				Name:     "Clean",
				Package:  "filepath",
				Language: "go",
			},
			expectMatch:   true,
			expectSafeFor: []string{"path"},
		},
		{
			name: "fmt.Sprintf is not a sanitizer",
			symbol: &ast.Symbol{
				Name:     "Sprintf",
				Package:  "fmt",
				Language: "go",
			},
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, matched := registry.MatchSanitizer(tt.symbol)

			if matched != tt.expectMatch {
				t.Errorf("Expected match=%v, got %v", tt.expectMatch, matched)
			}

			if matched && pattern != nil {
				if len(pattern.MakesSafeFor) != len(tt.expectSafeFor) {
					t.Errorf("Expected MakesSafeFor %v, got %v", tt.expectSafeFor, pattern.MakesSafeFor)
				}
			}
		})
	}
}

func TestSanitizerPattern_IsComplete(t *testing.T) {
	registry := NewSanitizerRegistry()
	patterns := registry.GetPatterns("go")

	for _, pattern := range patterns {
		if pattern.FunctionName == "Clean" && pattern.Package == "filepath" {
			if pattern.IsComplete() {
				t.Error("filepath.Clean should NOT be complete (requires context)")
			}
		}

		if pattern.FunctionName == "Atoi" && pattern.Package == "strconv" {
			if !pattern.IsComplete() {
				t.Error("strconv.Atoi should be complete")
			}
		}
	}
}

func TestCWEMapping(t *testing.T) {
	// Verify all expected CWE mappings exist
	expected := map[string]string{
		"sql":         "CWE-89",
		"command":     "CWE-78",
		"xss":         "CWE-79",
		"path":        "CWE-22",
		"ssrf":        "CWE-918",
		"deserialize": "CWE-502",
	}

	for category, expectedCWE := range expected {
		if CWEMapping[category] != expectedCWE {
			t.Errorf("CWEMapping[%q] = %q, expected %q", category, CWEMapping[category], expectedCWE)
		}
	}
}

func TestMergeTaints(t *testing.T) {
	tests := []struct {
		name     string
		taints   []safety.DataTaint
		expected safety.DataTaint
	}{
		{
			name:     "all clean returns clean",
			taints:   []safety.DataTaint{safety.TaintClean, safety.TaintClean},
			expected: safety.TaintClean,
		},
		{
			name:     "untrusted overrides clean",
			taints:   []safety.DataTaint{safety.TaintClean, safety.TaintUntrusted},
			expected: safety.TaintUntrusted,
		},
		{
			name:     "mixed is less conservative than untrusted",
			taints:   []safety.DataTaint{safety.TaintMixed, safety.TaintClean},
			expected: safety.TaintMixed,
		},
		{
			name:     "untrusted is most conservative",
			taints:   []safety.DataTaint{safety.TaintMixed, safety.TaintUntrusted, safety.TaintClean},
			expected: safety.TaintUntrusted,
		},
		{
			name:     "unknown returns unknown",
			taints:   []safety.DataTaint{safety.TaintUnknown, safety.TaintUnknown},
			expected: safety.TaintUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safety.MergeTaints(tt.taints...)
			if result != tt.expected {
				t.Errorf("MergeTaints(%v) = %v, expected %v", tt.taints, result, tt.expected)
			}
		})
	}
}
