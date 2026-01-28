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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// createTestSemanticGraph creates a test graph for semantic similarity tests.
func createTestSemanticGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	symbols := []*ast.Symbol{
		{
			ID:         "pkg/a.go:10:FuncA",
			Name:       "FuncA",
			Kind:       ast.SymbolKindFunction,
			FilePath:   "pkg/a.go",
			StartLine:  10,
			EndLine:    20,
			Exported:   true,
			Language:   "go",
			Signature:  "func FuncA(ctx context.Context, data []byte) error",
			DocComment: "FuncA processes data",
		},
		{
			ID:         "pkg/b.go:10:FuncB",
			Name:       "FuncB",
			Kind:       ast.SymbolKindFunction,
			FilePath:   "pkg/b.go",
			StartLine:  10,
			EndLine:    20,
			Exported:   true,
			Language:   "go",
			Signature:  "func FuncB(ctx context.Context, input []byte) error",
			DocComment: "FuncB handles input data",
		},
		{
			ID:         "pkg/c.go:10:FuncC",
			Name:       "FuncC",
			Kind:       ast.SymbolKindFunction,
			FilePath:   "pkg/c.go",
			StartLine:  10,
			EndLine:    20,
			Exported:   true,
			Language:   "go",
			Signature:  "func FuncC(w http.ResponseWriter, r *http.Request)",
			DocComment: "FuncC is an HTTP handler",
		},
	}

	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	for _, sym := range symbols {
		idx.Add(sym)
		g.AddNode(sym)
	}

	g.Freeze()

	return g, idx
}

// createMockEmbeddingServer creates a mock embedding service.
func createMockEmbeddingServer(t *testing.T) *httptest.Server {
	t.Helper()

	// Track call count for deterministic embeddings
	callCount := 0

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			json.NewEncoder(w).Encode(healthResponse{
				Status: "ok",
				Model:  "test-model",
			})

		case "/batch_embed":
			var req embeddingRequest
			json.NewDecoder(r.Body).Decode(&req)

			// Generate deterministic embeddings based on text content
			vectors := make([][]float32, len(req.Texts))
			for i := range req.Texts {
				// Simple deterministic embedding based on text length
				vectors[i] = []float32{
					float32(len(req.Texts[i])%10) / 10.0,
					float32(callCount%5) / 5.0,
					0.5,
					0.3,
				}
				callCount++
			}

			json.NewEncoder(w).Encode(embeddingResponse{
				ID:        "test-id",
				Timestamp: time.Now().Unix(),
				Model:     "test-model",
				Vectors:   vectors,
				Dim:       4,
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestNewSemanticSimilarityEngine(t *testing.T) {
	g, idx := createTestSemanticGraph(t)

	t.Run("without embedding client", func(t *testing.T) {
		engine := NewSemanticSimilarityEngine(g, idx, nil)

		if engine == nil {
			t.Fatal("expected engine to be non-nil")
		}
		if engine.embedEnabled {
			t.Error("expected embeddings to be disabled without client")
		}
		if engine.defaultMethod != SimilarityStructural {
			t.Errorf("expected default method structural, got %s", engine.defaultMethod)
		}
	})

	t.Run("with embedding client", func(t *testing.T) {
		server := createMockEmbeddingServer(t)
		defer server.Close()

		client := NewEmbeddingClient(server.URL)
		engine := NewSemanticSimilarityEngine(g, idx, client)

		if !engine.embedEnabled {
			t.Error("expected embeddings to be enabled with client")
		}
	})
}

func TestSemanticSimilarityEngine_Build(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	t.Run("structural only", func(t *testing.T) {
		engine := NewSemanticSimilarityEngine(g, idx, nil)

		err := engine.Build(ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !engine.IsBuilt() {
			t.Error("expected engine to be built")
		}
		if engine.IsEmbeddingEnabled() {
			t.Error("expected embeddings to be disabled")
		}
	})

	t.Run("with embeddings", func(t *testing.T) {
		server := createMockEmbeddingServer(t)
		defer server.Close()

		client := NewEmbeddingClient(server.URL)
		engine := NewSemanticSimilarityEngine(g, idx, client)

		err := engine.Build(ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !engine.IsBuilt() {
			t.Error("expected engine to be built")
		}
		if !engine.IsEmbeddingEnabled() {
			t.Error("expected embeddings to be enabled")
		}

		// Check embeddings were computed
		stats := engine.Stats()
		if stats.EmbeddingCount == 0 {
			t.Error("expected embeddings to be computed")
		}
	})
}

func TestSemanticSimilarityEngine_FindSimilarCode_Structural(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	engine := NewSemanticSimilarityEngine(g, idx, nil)
	if err := engine.Build(ctx); err != nil {
		t.Fatalf("build error: %v", err)
	}

	result, err := engine.FindSimilarCode(ctx, "pkg/a.go:10:FuncA", SimilarityStructural)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result to be non-nil")
	}
	if result.Method != string(SimilarityStructural) {
		t.Errorf("expected method structural, got %s", result.Method)
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_Semantic(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	server := createMockEmbeddingServer(t)
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	engine := NewSemanticSimilarityEngine(g, idx, client)

	if err := engine.Build(ctx); err != nil {
		t.Fatalf("build error: %v", err)
	}

	result, err := engine.FindSimilarCode(ctx, "pkg/a.go:10:FuncA", SimilaritySemantic)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result to be non-nil")
	}
	if result.Method != string(SimilaritySemantic) {
		t.Errorf("expected method semantic, got %s", result.Method)
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_Hybrid(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	server := createMockEmbeddingServer(t)
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	engine := NewSemanticSimilarityEngine(g, idx, client)

	if err := engine.Build(ctx); err != nil {
		t.Fatalf("build error: %v", err)
	}

	result, err := engine.FindSimilarCode(ctx, "pkg/a.go:10:FuncA", SimilarityHybrid)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result to be non-nil")
	}
	if result.Method != string(SimilarityHybrid) {
		t.Errorf("expected method hybrid, got %s", result.Method)
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_FallbackToStructural(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	// No embedding client - should fall back
	engine := NewSemanticSimilarityEngine(g, idx, nil)
	if err := engine.Build(ctx); err != nil {
		t.Fatalf("build error: %v", err)
	}

	// Request semantic but should fall back to structural
	result, err := engine.FindSimilarCode(ctx, "pkg/a.go:10:FuncA", SimilaritySemantic)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Method != string(SimilarityStructural) {
		t.Errorf("expected fallback to structural, got %s", result.Method)
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_NilContext(t *testing.T) {
	g, idx := createTestSemanticGraph(t)

	engine := NewSemanticSimilarityEngine(g, idx, nil)

	_, err := engine.FindSimilarCode(nil, "pkg/a.go:10:FuncA", SimilarityStructural)

	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_EmptySymbolID(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	engine := NewSemanticSimilarityEngine(g, idx, nil)
	engine.Build(ctx)

	_, err := engine.FindSimilarCode(ctx, "", SimilarityStructural)

	if err == nil {
		t.Error("expected error for empty symbolID")
	}
}

func TestSemanticSimilarityEngine_FindSimilarCode_NotBuilt(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	engine := NewSemanticSimilarityEngine(g, idx, nil)
	// Don't build

	_, err := engine.FindSimilarCode(ctx, "pkg/a.go:10:FuncA", SimilarityStructural)

	if err != ErrGraphNotReady {
		t.Errorf("expected ErrGraphNotReady, got %v", err)
	}
}

func TestSemanticSimilarityEngine_GetEmbedding(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	server := createMockEmbeddingServer(t)
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	engine := NewSemanticSimilarityEngine(g, idx, client)
	engine.Build(ctx)

	t.Run("existing embedding", func(t *testing.T) {
		embed, exists := engine.GetEmbedding("pkg/a.go:10:FuncA")

		if !exists {
			t.Error("expected embedding to exist")
		}
		if len(embed) == 0 {
			t.Error("expected non-empty embedding")
		}
	})

	t.Run("non-existing embedding", func(t *testing.T) {
		_, exists := engine.GetEmbedding("nonexistent:symbol")

		if exists {
			t.Error("expected embedding to not exist")
		}
	})
}

func TestSemanticSimilarityEngine_Stats(t *testing.T) {
	g, idx := createTestSemanticGraph(t)
	ctx := context.Background()

	server := createMockEmbeddingServer(t)
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	engine := NewSemanticSimilarityEngine(g, idx, client)
	engine.Build(ctx)

	stats := engine.Stats()

	if stats.EmbeddingCount == 0 {
		t.Error("expected non-zero embedding count")
	}
	if !stats.EmbeddingEnabled {
		t.Error("expected embedding enabled")
	}
	if !stats.EmbeddingBuilt {
		t.Error("expected embedding built")
	}
	if stats.DefaultMethod != string(SimilarityStructural) {
		t.Errorf("expected default method structural, got %s", stats.DefaultMethod)
	}
}

func TestSemanticSimilarityEngine_WithDefaultMethod(t *testing.T) {
	g, idx := createTestSemanticGraph(t)

	engine := NewSemanticSimilarityEngine(g, idx, nil).
		WithDefaultMethod(SimilarityHybrid)

	if engine.defaultMethod != SimilarityHybrid {
		t.Errorf("expected default method hybrid, got %s", engine.defaultMethod)
	}
}

func TestSemanticSimilarityEngine_WithHybridWeights(t *testing.T) {
	g, idx := createTestSemanticGraph(t)

	weights := HybridWeights{
		Structural: 0.7,
		Semantic:   0.3,
	}

	engine := NewSemanticSimilarityEngine(g, idx, nil).
		WithHybridWeights(weights)

	if engine.hybridWeights.Structural != 0.7 {
		t.Errorf("expected structural weight 0.7, got %f", engine.hybridWeights.Structural)
	}
}

func TestDefaultHybridWeights(t *testing.T) {
	weights := DefaultHybridWeights()

	if weights.Structural != 0.6 {
		t.Errorf("expected structural 0.6, got %f", weights.Structural)
	}
	if weights.Semantic != 0.4 {
		t.Errorf("expected semantic 0.4, got %f", weights.Semantic)
	}
}

func TestSimilarityMethodConstants(t *testing.T) {
	if SimilarityStructural != "structural" {
		t.Errorf("expected 'structural', got %s", SimilarityStructural)
	}
	if SimilaritySemantic != "semantic" {
		t.Errorf("expected 'semantic', got %s", SimilaritySemantic)
	}
	if SimilarityHybrid != "hybrid" {
		t.Errorf("expected 'hybrid', got %s", SimilarityHybrid)
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single",
			input:    []string{"a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := uniqueStrings(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
			}

			// Check all expected values present
			resultSet := make(map[string]struct{})
			for _, s := range result {
				resultSet[s] = struct{}{}
			}

			for _, e := range tt.expected {
				if _, ok := resultSet[e]; !ok {
					t.Errorf("expected %s to be in result", e)
				}
			}
		})
	}
}
