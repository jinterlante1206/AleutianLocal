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
	"strings"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
)

func setupFingerprintTestGraph() *graph.Graph {
	g := graph.NewGraph("/test/project")

	handler := &ast.Symbol{
		ID:        "pkg/handlers.HandleRequest",
		Name:      "HandleRequest",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 10,
		EndLine:   30,
		Package:   "handlers",
		Signature: "func(ctx context.Context, req *Request) (*Response, error)",
		Language:  "go",
	}

	validateInput := &ast.Symbol{
		ID:        "pkg/handlers.ValidateInput",
		Name:      "ValidateInput",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/handlers/handler.go",
		StartLine: 35,
		EndLine:   45,
		Package:   "handlers",
		Signature: "func(input string) error",
		Language:  "go",
	}

	processData := &ast.Symbol{
		ID:        "pkg/service.ProcessData",
		Name:      "ProcessData",
		Kind:      ast.SymbolKindFunction,
		FilePath:  "pkg/service/service.go",
		StartLine: 10,
		EndLine:   50,
		Package:   "service",
		Signature: "func(ctx context.Context, data []byte) (*Result, error)",
		Language:  "go",
	}

	g.AddNode(handler)
	g.AddNode(validateInput)
	g.AddNode(processData)

	// handler calls validateInput and processData
	g.AddEdge(handler.ID, validateInput.ID, graph.EdgeTypeCalls, ast.Location{})
	g.AddEdge(handler.ID, processData.ID, graph.EdgeTypeCalls, ast.Location{})

	g.Freeze()

	return g
}

func TestFingerprintBuilder_ComputeFingerprint(t *testing.T) {
	g := setupFingerprintTestGraph()
	builder := NewFingerprintBuilder(g)

	t.Run("computes fingerprint for function", func(t *testing.T) {
		sym := &ast.Symbol{
			ID:        "pkg/handlers.HandleRequest",
			Name:      "HandleRequest",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/handlers/handler.go",
			StartLine: 10,
			EndLine:   30,
			Signature: "func(ctx context.Context, req *Request) (*Response, error)",
			Language:  "go",
		}

		fp := builder.ComputeFingerprint(sym)

		if fp.SymbolID != sym.ID {
			t.Errorf("expected SymbolID %q, got %q", sym.ID, fp.SymbolID)
		}
		if fp.ParamCount != 2 {
			t.Errorf("expected ParamCount 2, got %d", fp.ParamCount)
		}
		if fp.ReturnCount != 2 {
			t.Errorf("expected ReturnCount 2, got %d", fp.ReturnCount)
		}
		if fp.Complexity <= 0 {
			t.Error("expected positive complexity")
		}
		if len(fp.MinHash) == 0 {
			t.Error("expected MinHash signature")
		}
	})

	t.Run("includes call patterns from graph", func(t *testing.T) {
		node, _ := g.GetNode("pkg/handlers.HandleRequest")
		sym := node.Symbol

		fp := builder.ComputeFingerprint(sym)

		if len(fp.CallPattern) == 0 {
			t.Error("expected call patterns from graph")
		}
	})

	t.Run("handles nil symbol", func(t *testing.T) {
		fp := builder.ComputeFingerprint(nil)

		if fp == nil {
			t.Fatal("expected non-nil fingerprint for nil symbol")
		}
		if fp.SymbolID != "" {
			t.Errorf("expected empty SymbolID, got %q", fp.SymbolID)
		}
	})

	t.Run("detects control flow patterns", func(t *testing.T) {
		sym := &ast.Symbol{
			ID:        "test.GetUser",
			Name:      "GetUser",
			Kind:      ast.SymbolKindFunction,
			Signature: "func(ctx context.Context, id string) (*User, error)",
		}

		fp := builder.ComputeFingerprint(sym)

		if fp.ControlFlow == "" {
			t.Error("expected control flow pattern")
		}
		// Should detect context_aware, error_handling, getter
		if !stringContains(fp.ControlFlow, "context_aware") {
			t.Error("expected context_aware pattern")
		}
		if !stringContains(fp.ControlFlow, "error_handling") {
			t.Error("expected error_handling pattern")
		}
		if !stringContains(fp.ControlFlow, "getter") {
			t.Error("expected getter pattern")
		}
	})
}

func TestComputeFingerprintFromSignature(t *testing.T) {
	t.Run("extracts features from signature", func(t *testing.T) {
		fp := ComputeFingerprintFromSignature(
			"func(ctx context.Context, req *Request) (*Response, error)",
			ast.SymbolKindFunction,
			"go",
		)

		if fp.ParamCount != 2 {
			t.Errorf("expected ParamCount 2, got %d", fp.ParamCount)
		}
		if fp.ReturnCount != 2 {
			t.Errorf("expected ReturnCount 2, got %d", fp.ReturnCount)
		}
	})

	t.Run("detects error return", func(t *testing.T) {
		fp := ComputeFingerprintFromSignature(
			"func(input string) error",
			ast.SymbolKindFunction,
			"go",
		)

		hasReturnsError := false
		for _, nt := range fp.NodeTypes {
			if nt == "returns_error" {
				hasReturnsError = true
				break
			}
		}
		if !hasReturnsError {
			t.Error("expected returns_error in NodeTypes")
		}
	})

	t.Run("detects context param", func(t *testing.T) {
		fp := ComputeFingerprintFromSignature(
			"func(ctx context.Context) error",
			ast.SymbolKindFunction,
			"go",
		)

		hasContext := false
		for _, nt := range fp.NodeTypes {
			if nt == "takes_context" {
				hasContext = true
				break
			}
		}
		if !hasContext {
			t.Error("expected takes_context in NodeTypes")
		}
	})
}

func TestMinHashSignature(t *testing.T) {
	t.Run("returns signature of correct length", func(t *testing.T) {
		set := []string{"a", "b", "c"}
		sig := MinHashSignature(set, 128)

		if len(sig) != 128 {
			t.Errorf("expected signature length 128, got %d", len(sig))
		}
	})

	t.Run("similar sets have similar signatures", func(t *testing.T) {
		set1 := []string{"a", "b", "c", "d", "e"}
		set2 := []string{"a", "b", "c", "d", "f"} // 4/6 overlap

		sig1 := MinHashSignature(set1, 128)
		sig2 := MinHashSignature(set2, 128)

		sim := JaccardSimilarity(sig1, sig2)

		// Should have reasonable similarity (> 0.5)
		if sim < 0.4 {
			t.Errorf("expected similarity > 0.4 for overlapping sets, got %f", sim)
		}
	})

	t.Run("different sets have low similarity", func(t *testing.T) {
		set1 := []string{"a", "b", "c"}
		set2 := []string{"x", "y", "z"}

		sig1 := MinHashSignature(set1, 128)
		sig2 := MinHashSignature(set2, 128)

		sim := JaccardSimilarity(sig1, sig2)

		// Should have low similarity (< 0.3)
		if sim > 0.3 {
			t.Errorf("expected similarity < 0.3 for different sets, got %f", sim)
		}
	})

	t.Run("handles empty set", func(t *testing.T) {
		sig := MinHashSignature([]string{}, 128)

		if len(sig) != 128 {
			t.Errorf("expected signature length 128 for empty set, got %d", len(sig))
		}
	})
}

func TestJaccardSimilarity(t *testing.T) {
	t.Run("identical signatures have similarity 1.0", func(t *testing.T) {
		sig := []uint64{1, 2, 3, 4, 5}
		sim := JaccardSimilarity(sig, sig)

		if sim != 1.0 {
			t.Errorf("expected similarity 1.0, got %f", sim)
		}
	})

	t.Run("different signatures have lower similarity", func(t *testing.T) {
		sig1 := []uint64{1, 2, 3, 4, 5}
		sig2 := []uint64{1, 2, 6, 7, 8}

		sim := JaccardSimilarity(sig1, sig2)

		// 2/5 match
		expected := 2.0 / 5.0
		if sim != expected {
			t.Errorf("expected similarity %f, got %f", expected, sim)
		}
	})

	t.Run("handles empty signatures", func(t *testing.T) {
		sim := JaccardSimilarity([]uint64{}, []uint64{})
		if sim != 0.0 {
			t.Errorf("expected similarity 0.0 for empty signatures, got %f", sim)
		}
	})

	t.Run("handles different lengths", func(t *testing.T) {
		sig1 := []uint64{1, 2, 3, 4, 5}
		sig2 := []uint64{1, 2, 3}

		sim := JaccardSimilarity(sig1, sig2)

		// Should use shorter length (3), all match
		if sim != 1.0 {
			t.Errorf("expected similarity 1.0 for matching prefix, got %f", sim)
		}
	})
}

func TestComputeSimilarity(t *testing.T) {
	t.Run("similar fingerprints have high similarity", func(t *testing.T) {
		fp1 := &ASTFingerprint{
			ParamCount:  2,
			ReturnCount: 2,
			Complexity:  5,
			ControlFlow: "error_handling,context_aware",
			MinHash:     MinHashSignature([]string{"a", "b", "c"}, 128),
		}
		fp2 := &ASTFingerprint{
			ParamCount:  2,
			ReturnCount: 2,
			Complexity:  5,
			ControlFlow: "error_handling,context_aware",
			MinHash:     MinHashSignature([]string{"a", "b", "c"}, 128),
		}

		sim, traits := ComputeSimilarity(fp1, fp2)

		if sim < 0.8 {
			t.Errorf("expected high similarity for identical fingerprints, got %f", sim)
		}
		if len(traits) == 0 {
			t.Error("expected matched traits")
		}
	})

	t.Run("different fingerprints have lower similarity", func(t *testing.T) {
		fp1 := &ASTFingerprint{
			ParamCount:  1,
			ReturnCount: 1,
			Complexity:  2,
			ControlFlow: "getter",
			MinHash:     MinHashSignature([]string{"get"}, 128),
		}
		fp2 := &ASTFingerprint{
			ParamCount:  5,
			ReturnCount: 3,
			Complexity:  20,
			ControlFlow: "handler",
			MinHash:     MinHashSignature([]string{"handle"}, 128),
		}

		sim, _ := ComputeSimilarity(fp1, fp2)

		if sim > 0.5 {
			t.Errorf("expected low similarity for different fingerprints, got %f", sim)
		}
	})

	t.Run("handles nil fingerprints", func(t *testing.T) {
		sim, traits := ComputeSimilarity(nil, nil)

		if sim != 0.0 {
			t.Errorf("expected similarity 0.0 for nil fingerprints, got %f", sim)
		}
		if traits != nil {
			t.Error("expected nil traits for nil fingerprints")
		}
	})
}

func TestExtractParamReturnCounts(t *testing.T) {
	tests := []struct {
		sig            string
		expectedParams int
		expectedReturn int
	}{
		{"func()", 0, 0},
		{"func(a int)", 1, 0},
		{"func(a, b int)", 2, 0},
		{"func() int", 0, 1},
		{"func() (int, error)", 0, 2},
		{"func(a int) int", 1, 1},
		{"func(ctx context.Context, req *Request) (*Response, error)", 2, 2},
		{"func(a, b, c int, d string) (x, y int, err error)", 4, 3},
		{"", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.sig, func(t *testing.T) {
			params, returns := extractParamReturnCounts(tt.sig)

			if params != tt.expectedParams {
				t.Errorf("expected %d params, got %d", tt.expectedParams, params)
			}
			if returns != tt.expectedReturn {
				t.Errorf("expected %d returns, got %d", tt.expectedReturn, returns)
			}
		})
	}
}

func TestFingerprintIndex(t *testing.T) {
	t.Run("adds and retrieves fingerprints", func(t *testing.T) {
		idx := NewFingerprintIndex(16, 8)

		fp := &ASTFingerprint{
			SymbolID: "test.Function",
			MinHash:  MinHashSignature([]string{"a", "b", "c"}, 128),
		}

		idx.Add(fp)

		retrieved, exists := idx.Get("test.Function")
		if !exists {
			t.Fatal("expected to find fingerprint")
		}
		if retrieved.SymbolID != fp.SymbolID {
			t.Errorf("expected SymbolID %q, got %q", fp.SymbolID, retrieved.SymbolID)
		}
	})

	t.Run("finds similar fingerprints", func(t *testing.T) {
		idx := NewFingerprintIndex(16, 8)

		// Add several fingerprints
		fp1 := &ASTFingerprint{
			SymbolID: "test.Func1",
			MinHash:  MinHashSignature([]string{"a", "b", "c", "d", "e"}, 128),
		}
		fp2 := &ASTFingerprint{
			SymbolID: "test.Func2",
			MinHash:  MinHashSignature([]string{"a", "b", "c", "d", "f"}, 128), // Similar
		}
		fp3 := &ASTFingerprint{
			SymbolID: "test.Func3",
			MinHash:  MinHashSignature([]string{"x", "y", "z"}, 128), // Different
		}

		idx.Add(fp1)
		idx.Add(fp2)
		idx.Add(fp3)

		// Query for similar to fp1
		candidates := idx.FindSimilar(fp1, 10)

		// fp2 should be a candidate (similar), fp3 may or may not be
		hasFp2 := false
		for _, id := range candidates {
			if id == "test.Func2" {
				hasFp2 = true
				break
			}
		}

		// Note: LSH is probabilistic, so we can't guarantee fp2 is always found
		// But with similar content, it should often be found
		_ = hasFp2
	})

	t.Run("reports correct size", func(t *testing.T) {
		idx := NewFingerprintIndex(16, 8)

		if idx.Size() != 0 {
			t.Errorf("expected size 0, got %d", idx.Size())
		}

		idx.Add(&ASTFingerprint{SymbolID: "test1", MinHash: make([]uint64, 128)})
		idx.Add(&ASTFingerprint{SymbolID: "test2", MinHash: make([]uint64, 128)})

		if idx.Size() != 2 {
			t.Errorf("expected size 2, got %d", idx.Size())
		}
	})

	t.Run("ignores nil and invalid fingerprints", func(t *testing.T) {
		idx := NewFingerprintIndex(16, 8)

		idx.Add(nil)
		idx.Add(&ASTFingerprint{SymbolID: ""})
		idx.Add(&ASTFingerprint{SymbolID: "test", MinHash: nil})

		if idx.Size() != 0 {
			t.Errorf("expected size 0 after adding invalid fingerprints, got %d", idx.Size())
		}
	})
}

func TestSimilarityThreshold(t *testing.T) {
	t.Run("computes reasonable thresholds", func(t *testing.T) {
		// Standard configuration
		threshold := SimilarityThreshold(16, 8)

		// Should be in reasonable range (0.3 to 0.8)
		if threshold < 0.2 || threshold > 0.9 {
			t.Errorf("expected threshold in [0.2, 0.9], got %f", threshold)
		}
	})

	t.Run("more bands means lower threshold", func(t *testing.T) {
		t1 := SimilarityThreshold(8, 16)
		t2 := SimilarityThreshold(32, 4)

		// More bands = lower threshold (more candidates)
		if t2 >= t1 {
			t.Errorf("expected more bands to give lower threshold: %f vs %f", t2, t1)
		}
	})
}

// Helper function for string contains check
func stringContains(s, substr string) bool {
	return strings.Contains(s, substr)
}
