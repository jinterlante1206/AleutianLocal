// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// mockLibraryDocProvider implements LibraryDocProvider for testing.
type mockLibraryDocProvider struct {
	docs      []LibraryDoc
	searchErr error
}

func (m *mockLibraryDocProvider) Search(ctx context.Context, query string, limit int) ([]LibraryDoc, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	if limit > len(m.docs) {
		return m.docs, nil
	}
	return m.docs[:limit], nil
}

// createTestGraph creates a simple graph for testing.
func createTestGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create test symbols
	symbols := []*ast.Symbol{
		{
			ID:        "handlers/user.go:10:HandleUser",
			Name:      "HandleUser",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/user.go",
			StartLine: 10,
			EndLine:   30,
			Signature: "func HandleUser(c *gin.Context)",
			Language:  "go",
		},
		{
			ID:        "handlers/user.go:35:HandleAuth",
			Name:      "HandleAuth",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "handlers/user.go",
			StartLine: 35,
			EndLine:   55,
			Signature: "func HandleAuth(c *gin.Context)",
			Language:  "go",
		},
		{
			ID:        "models/user.go:5:User",
			Name:      "User",
			Kind:      ast.SymbolKindStruct,
			FilePath:  "models/user.go",
			StartLine: 5,
			EndLine:   15,
			Signature: "type User struct { ID string; Name string }",
			Language:  "go",
		},
		{
			ID:        "services/user.go:20:UserService",
			Name:      "UserService",
			Kind:      ast.SymbolKindInterface,
			FilePath:  "services/user.go",
			StartLine: 20,
			EndLine:   30,
			Signature: "type UserService interface { GetUser(id string) (*User, error) }",
			Language:  "go",
		},
	}

	// Add symbols to graph and index
	for _, sym := range symbols {
		if _, err := g.AddNode(sym); err != nil {
			t.Fatalf("failed to add node: %v", err)
		}
		if err := idx.Add(sym); err != nil {
			t.Fatalf("failed to add to index: %v", err)
		}
	}

	// Add edges
	// HandleUser calls HandleAuth
	if err := g.AddEdge(symbols[0].ID, symbols[1].ID, graph.EdgeTypeCalls, ast.Location{
		FilePath:  "handlers/user.go",
		StartLine: 15,
	}); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	// HandleUser references User type
	if err := g.AddEdge(symbols[0].ID, symbols[2].ID, graph.EdgeTypeReferences, ast.Location{
		FilePath:  "handlers/user.go",
		StartLine: 12,
	}); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	g.Freeze()

	return g, idx
}

func TestNewAssembler(t *testing.T) {
	g, idx := createTestGraph(t)

	t.Run("creates with defaults", func(t *testing.T) {
		a := NewAssembler(g, idx)

		if a.graph != g {
			t.Error("graph not set")
		}
		if a.index != idx {
			t.Error("index not set")
		}
		if a.options.Timeout != DefaultTimeout {
			t.Errorf("expected timeout %v, got %v", DefaultTimeout, a.options.Timeout)
		}
	})

	t.Run("applies options", func(t *testing.T) {
		timeout := 1 * time.Second
		depth := 5
		maxSymbols := 50

		a := NewAssembler(g, idx,
			WithTimeout(timeout),
			WithGraphDepth(depth),
			WithMaxSymbols(maxSymbols),
			WithLibraryDocs(false),
		)

		if a.options.Timeout != timeout {
			t.Errorf("expected timeout %v, got %v", timeout, a.options.Timeout)
		}
		if a.options.GraphDepth != depth {
			t.Errorf("expected depth %d, got %d", depth, a.options.GraphDepth)
		}
		if a.options.MaxSymbols != maxSymbols {
			t.Errorf("expected max symbols %d, got %d", maxSymbols, a.options.MaxSymbols)
		}
		if a.options.IncludeLibraryDocs {
			t.Error("expected library docs disabled")
		}
	})
}

func TestAssembler_Assemble_Validation(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	t.Run("returns ErrGraphNotInitialized for nil graph", func(t *testing.T) {
		a := NewAssembler(nil, idx)
		_, err := a.Assemble(ctx, "test query", 1000)
		if !errors.Is(err, ErrGraphNotInitialized) {
			t.Errorf("expected ErrGraphNotInitialized, got %v", err)
		}
	})

	t.Run("returns ErrGraphNotInitialized for unfrozen graph", func(t *testing.T) {
		unfrozen := graph.NewGraph("/test")
		a := NewAssembler(unfrozen, idx)
		_, err := a.Assemble(ctx, "test query", 1000)
		if !errors.Is(err, ErrGraphNotInitialized) {
			t.Errorf("expected ErrGraphNotInitialized, got %v", err)
		}
	})

	t.Run("returns ErrEmptyQuery for empty query", func(t *testing.T) {
		a := NewAssembler(g, idx)
		_, err := a.Assemble(ctx, "", 1000)
		if !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("expected ErrEmptyQuery, got %v", err)
		}
	})

	t.Run("returns ErrEmptyQuery for whitespace query", func(t *testing.T) {
		a := NewAssembler(g, idx)
		_, err := a.Assemble(ctx, "   \t\n", 1000)
		if !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("expected ErrEmptyQuery, got %v", err)
		}
	})

	t.Run("returns ErrQueryTooLong for long query", func(t *testing.T) {
		a := NewAssembler(g, idx)
		longQuery := make([]byte, MaxQueryLength+1)
		for i := range longQuery {
			longQuery[i] = 'a'
		}
		_, err := a.Assemble(ctx, string(longQuery), 1000)
		if !errors.Is(err, ErrQueryTooLong) {
			t.Errorf("expected ErrQueryTooLong, got %v", err)
		}
	})

	t.Run("returns ErrInvalidBudget for zero budget", func(t *testing.T) {
		a := NewAssembler(g, idx)
		_, err := a.Assemble(ctx, "test query", 0)
		if !errors.Is(err, ErrInvalidBudget) {
			t.Errorf("expected ErrInvalidBudget, got %v", err)
		}
	})

	t.Run("returns ErrInvalidBudget for negative budget", func(t *testing.T) {
		a := NewAssembler(g, idx)
		_, err := a.Assemble(ctx, "test query", -100)
		if !errors.Is(err, ErrInvalidBudget) {
			t.Errorf("expected ErrInvalidBudget, got %v", err)
		}
	})
}

func TestAssembler_Assemble_ExactMatch(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	a := NewAssembler(g, idx)
	result, err := a.Assemble(ctx, "HandleUser", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Context == "" {
		t.Error("expected non-empty context")
	}

	if len(result.SymbolsIncluded) == 0 {
		t.Error("expected at least one symbol included")
	}

	// Should include HandleUser
	found := false
	for _, id := range result.SymbolsIncluded {
		if id == "handlers/user.go:10:HandleUser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected HandleUser to be included")
	}

	if result.TokensUsed <= 0 {
		t.Error("expected positive token count")
	}
}

func TestAssembler_Assemble_FuzzyMatch(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	a := NewAssembler(g, idx)
	result, err := a.Assemble(ctx, "user handler", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Context == "" {
		t.Error("expected non-empty context")
	}

	if len(result.SymbolsIncluded) == 0 {
		t.Error("expected symbols to be included via fuzzy match")
	}
}

func TestAssembler_Assemble_GraphWalk(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	a := NewAssembler(g, idx, WithGraphDepth(2))
	result, err := a.Assemble(ctx, "HandleUser", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include HandleUser and related symbols (HandleAuth, User)
	if len(result.SymbolsIncluded) < 2 {
		t.Errorf("expected at least 2 symbols, got %d", len(result.SymbolsIncluded))
	}
}

func TestAssembler_Assemble_BudgetConstraint(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	// Very small budget
	a := NewAssembler(g, idx)
	result, err := a.Assemble(ctx, "HandleUser", 50)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should respect budget
	if result.TokensUsed > 50 {
		t.Errorf("exceeded budget: used %d tokens, budget was 50", result.TokensUsed)
	}
}

func TestAssembler_Assemble_NoMatches(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	a := NewAssembler(g, idx)
	result, err := a.Assemble(ctx, "NonExistentSymbol12345", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.SymbolsIncluded) != 0 {
		t.Errorf("expected no symbols, got %d", len(result.SymbolsIncluded))
	}

	if len(result.Suggestions) == 0 {
		t.Error("expected suggestions for no matches")
	}
}

func TestAssembler_Assemble_LibraryDocs(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	mockDocs := &mockLibraryDocProvider{
		docs: []LibraryDoc{
			{
				DocID:      "gin-context-1",
				Library:    "github.com/gin-gonic/gin",
				Version:    "v1.9.1",
				SymbolPath: "gin.Context.JSON",
				SymbolKind: "method",
				Signature:  "func (c *Context) JSON(code int, obj interface{})",
				DocContent: "JSON serializes the given struct as JSON into the response body.",
			},
		},
	}

	a := NewAssembler(g, idx, WithLibraryDocs(true)).WithLibraryDocProvider(mockDocs)
	result, err := a.Assemble(ctx, "HandleUser gin context", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.LibraryDocsIncluded) == 0 {
		t.Error("expected library docs to be included")
	}

	// Check library docs appear in context
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestAssembler_Assemble_LibraryDocsDisabled(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	mockDocs := &mockLibraryDocProvider{
		docs: []LibraryDoc{
			{DocID: "test-doc"},
		},
	}

	a := NewAssembler(g, idx, WithLibraryDocs(false)).WithLibraryDocProvider(mockDocs)
	result, err := a.Assemble(ctx, "HandleUser", 8000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.LibraryDocsIncluded) != 0 {
		t.Error("expected no library docs when disabled")
	}
}

func TestAssembler_Assemble_LibraryDocsError(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	mockDocs := &mockLibraryDocProvider{
		searchErr: errors.New("weaviate unavailable"),
	}

	a := NewAssembler(g, idx, WithLibraryDocs(true)).WithLibraryDocProvider(mockDocs)
	result, err := a.Assemble(ctx, "HandleUser", 8000)

	// Should succeed with graceful degradation
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still have code context
	if result.Context == "" {
		t.Error("expected non-empty context despite library docs error")
	}
}

func TestAssembler_Assemble_ContextCancellation(t *testing.T) {
	g, idx := createTestGraph(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	a := NewAssembler(g, idx)
	result, err := a.Assemble(ctx, "HandleUser", 8000)

	// Should not return error, but may have incomplete results
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// May have partial or no results due to cancellation
	_ = result
}

func TestBudgetAllocation_Validate(t *testing.T) {
	t.Run("valid allocation", func(t *testing.T) {
		alloc := BudgetAllocation{
			CodePercent:    60,
			TypesPercent:   20,
			LibDocsPercent: 20,
		}
		if !alloc.Validate() {
			t.Error("expected valid allocation")
		}
	})

	t.Run("invalid allocation", func(t *testing.T) {
		alloc := BudgetAllocation{
			CodePercent:    50,
			TypesPercent:   20,
			LibDocsPercent: 20,
		}
		if alloc.Validate() {
			t.Error("expected invalid allocation (90% total)")
		}
	})
}

func TestExtractQueryTerms(t *testing.T) {
	tests := []struct {
		query    string
		expected int // Minimum expected terms
	}{
		{"HandleUser", 1},
		{"add authentication to HandleAgent", 2}, // HandleAgent, authentication
		{"the a an to for", 0},                   // All common words
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			terms := extractQueryTerms(tt.query)
			if len(terms) < tt.expected {
				t.Errorf("expected at least %d terms, got %d: %v", tt.expected, len(terms), terms)
			}
		})
	}
}

func TestSymbolImportance(t *testing.T) {
	tests := []struct {
		kind     ast.SymbolKind
		expected float64
	}{
		{ast.SymbolKindFunction, 1.0},
		{ast.SymbolKindMethod, 0.95},
		{ast.SymbolKindInterface, 0.85},
		{ast.SymbolKindStruct, 0.80},
		{ast.SymbolKindField, 0.50},
		{ast.SymbolKindUnknown, 0.40},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			importance := SymbolImportance(tt.kind)
			if importance != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, importance)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text      string
		minTokens int
		maxTokens int
	}{
		{"", 0, 0},
		{"func main() {}", 3, 5},
		{"type User struct { ID string; Name string }", 10, 15},
	}

	for _, tt := range tests {
		t.Run(tt.text[:min(20, len(tt.text))], func(t *testing.T) {
			tokens := estimateTokens(tt.text)
			if tokens < tt.minTokens || tokens > tt.maxTokens {
				t.Errorf("expected %d-%d tokens, got %d", tt.minTokens, tt.maxTokens, tokens)
			}
		})
	}
}

func TestAssembler_Assemble_Performance(t *testing.T) {
	g, idx := createTestGraph(t)
	ctx := context.Background()

	a := NewAssembler(g, idx, WithTimeout(500*time.Millisecond))

	start := time.Now()
	result, err := a.Assemble(ctx, "HandleUser", 8000)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should complete well under 500ms for small test graph
	if duration > 100*time.Millisecond {
		t.Errorf("assembly took too long: %v", duration)
	}

	// AssemblyDurationMs can be 0 for very fast operations (<1ms)
	// Just verify it's non-negative
	if result.AssemblyDurationMs < 0 {
		t.Error("expected non-negative assembly duration")
	}
}

// min function removed - using the one from summarizer.go
