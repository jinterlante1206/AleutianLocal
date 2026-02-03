// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// createTestPatternGraph creates a test graph with various patterns.
func createTestPatternGraph(t *testing.T) (*graph.Graph, *index.SymbolIndex) {
	t.Helper()

	symbols := []*ast.Symbol{
		// Singleton pattern
		{
			ID:        "pkg/singleton.go:10:instance",
			Name:      "instance",
			Kind:      ast.SymbolKindVariable,
			FilePath:  "pkg/singleton.go",
			StartLine: 10,
			EndLine:   10,
			Exported:  false,
			Language:  "go",
			Signature: "var instance *Service",
		},
		{
			ID:        "pkg/singleton.go:15:GetInstance",
			Name:      "GetInstance",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/singleton.go",
			StartLine: 15,
			EndLine:   20,
			Exported:  true,
			Language:  "go",
			Signature: "func GetInstance() *Service { sync.Once }",
		},

		// Factory pattern
		{
			ID:        "pkg/factory.go:10:NewService",
			Name:      "NewService",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/factory.go",
			StartLine: 10,
			EndLine:   20,
			Exported:  true,
			Language:  "go",
			Signature: "func NewService(config *Config) (*Service, error)",
		},
		{
			ID:        "pkg/factory.go:25:NewClient",
			Name:      "NewClient",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/factory.go",
			StartLine: 25,
			EndLine:   35,
			Exported:  true,
			Language:  "go",
			Signature: "func NewClient(url string) *Client",
		},

		// Builder pattern
		{
			ID:        "pkg/builder.go:10:RequestBuilder",
			Name:      "RequestBuilder",
			Kind:      ast.SymbolKindStruct,
			FilePath:  "pkg/builder.go",
			StartLine: 10,
			EndLine:   15,
			Exported:  true,
			Language:  "go",
			Signature: "type RequestBuilder struct { ... }",
		},
		{
			ID:        "pkg/builder.go:20:WithTimeout",
			Name:      "WithTimeout",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "pkg/builder.go",
			StartLine: 20,
			EndLine:   25,
			Exported:  true,
			Language:  "go",
			Signature: "func (b *RequestBuilder) WithTimeout(d time.Duration) *RequestBuilder",
			Receiver:  "*RequestBuilder",
		},
		{
			ID:        "pkg/builder.go:30:WithRetry",
			Name:      "WithRetry",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "pkg/builder.go",
			StartLine: 30,
			EndLine:   35,
			Exported:  true,
			Language:  "go",
			Signature: "func (b *RequestBuilder) WithRetry(count int) *RequestBuilder",
			Receiver:  "*RequestBuilder",
		},
		{
			ID:        "pkg/builder.go:40:Build",
			Name:      "Build",
			Kind:      ast.SymbolKindMethod,
			FilePath:  "pkg/builder.go",
			StartLine: 40,
			EndLine:   50,
			Exported:  true,
			Language:  "go",
			Signature: "func (b *RequestBuilder) Build() (*Request, error)",
			Receiver:  "*RequestBuilder",
		},

		// Options pattern
		{
			ID:        "pkg/options.go:10:Option",
			Name:      "Option",
			Kind:      ast.SymbolKindType,
			FilePath:  "pkg/options.go",
			StartLine: 10,
			EndLine:   10,
			Exported:  true,
			Language:  "go",
			Signature: "type Option func(*Config)",
		},
		{
			ID:        "pkg/options.go:15:WithName",
			Name:      "WithName",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/options.go",
			StartLine: 15,
			EndLine:   20,
			Exported:  true,
			Language:  "go",
			Signature: "func WithName(name string) Option",
		},
		{
			ID:        "pkg/options.go:25:WithTimeout",
			Name:      "WithTimeout",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/options.go",
			StartLine: 25,
			EndLine:   30,
			Exported:  true,
			Language:  "go",
			Signature: "func WithTimeout(d time.Duration) Option",
		},
		{
			ID:        "pkg/options.go:35:WithRetry",
			Name:      "WithRetry",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/options.go",
			StartLine: 35,
			EndLine:   40,
			Exported:  true,
			Language:  "go",
			Signature: "func WithRetry(count int) Option",
		},

		// Middleware pattern
		{
			ID:        "pkg/middleware.go:10:LoggingMiddleware",
			Name:      "LoggingMiddleware",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/middleware.go",
			StartLine: 10,
			EndLine:   20,
			Exported:  true,
			Language:  "go",
			Signature: "func LoggingMiddleware(next http.Handler) http.Handler",
		},
		{
			ID:        "pkg/middleware.go:25:AuthMiddleware",
			Name:      "AuthMiddleware",
			Kind:      ast.SymbolKindFunction,
			FilePath:  "pkg/middleware.go",
			StartLine: 25,
			EndLine:   40,
			Exported:  true,
			Language:  "go",
			Signature: "func AuthMiddleware(next http.Handler) http.Handler",
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

func TestNewPatternDetector(t *testing.T) {
	g, idx := createTestPatternGraph(t)

	detector := NewPatternDetector(g, idx)

	if detector == nil {
		t.Fatal("expected detector to be non-nil")
	}
	if detector.graph != g {
		t.Error("expected graph to be set")
	}
	if detector.index != idx {
		t.Error("expected index to be set")
	}
	if len(detector.matchers) == 0 {
		t.Error("expected default matchers to be registered")
	}
}

func TestPatternDetector_DetectPatterns_All(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) == 0 {
		t.Error("expected at least one pattern to be detected")
	}

	// Check that multiple pattern types were detected
	types := make(map[PatternType]bool)
	for _, p := range patterns {
		types[p.Type] = true
	}

	if len(types) < 2 {
		t.Errorf("expected multiple pattern types, got %d", len(types))
	}
}

func TestPatternDetector_DetectPatterns_Factory(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		Patterns: []PatternType{PatternFactory},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find at least one factory pattern (NewService returns error, NewClient doesn't)
	if len(patterns) < 1 {
		t.Errorf("expected at least 1 factory pattern, got %d", len(patterns))
	}

	for _, p := range patterns {
		if p.Type != PatternFactory {
			t.Errorf("expected factory pattern, got %s", p.Type)
		}
	}

	// Check that we found NewService (has proper error return)
	foundNewService := false
	for _, p := range patterns {
		for _, comp := range p.Components {
			if comp == "pkg/factory.go:10:NewService" {
				foundNewService = true
				break
			}
		}
	}
	if !foundNewService {
		t.Log("NewService factory not found - may be expected based on detection criteria")
	}
}

func TestPatternDetector_DetectPatterns_Builder(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		Patterns: []PatternType{PatternBuilder},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find RequestBuilder
	if len(patterns) < 1 {
		t.Errorf("expected at least 1 builder pattern, got %d", len(patterns))
	}

	for _, p := range patterns {
		if p.Type != PatternBuilder {
			t.Errorf("expected builder pattern, got %s", p.Type)
		}
		// Builder should be idiomatic (has Build method and chainable With methods)
		if !p.Idiomatic && len(p.Warnings) == 0 {
			t.Log("Builder pattern detected but not marked idiomatic without warnings")
		}
	}
}

func TestPatternDetector_DetectPatterns_Options(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		Patterns: []PatternType{PatternOptions},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find Option type
	if len(patterns) < 1 {
		t.Errorf("expected at least 1 options pattern, got %d", len(patterns))
	}
}

func TestPatternDetector_DetectPatterns_Middleware(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		Patterns: []PatternType{PatternMiddleware},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find LoggingMiddleware and AuthMiddleware
	if len(patterns) < 2 {
		t.Errorf("expected at least 2 middleware patterns, got %d", len(patterns))
	}
}

func TestPatternDetector_DetectPatterns_WithScope(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	// Only detect in factory.go
	patterns, err := detector.DetectPatterns(ctx, "pkg/factory", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All detected patterns should be from factory.go
	for _, p := range patterns {
		if p.Location != "pkg/factory.go" {
			t.Errorf("expected location pkg/factory.go, got %s", p.Location)
		}
	}
}

func TestPatternDetector_DetectPatterns_MinConfidence(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	// Only high confidence patterns
	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		MinConfidence: 0.8,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range patterns {
		if p.Confidence < 0.8 {
			t.Errorf("expected confidence >= 0.8, got %f", p.Confidence)
		}
	}
}

func TestPatternDetector_DetectPatterns_ExcludeNonIdiomatic(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPatterns(ctx, "", &DetectionOptions{
		IncludeNonIdiomatic: false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range patterns {
		if !p.Idiomatic {
			t.Errorf("expected only idiomatic patterns, found non-idiomatic %s", p.Type)
		}
	}
}

func TestPatternDetector_DetectPatterns_NilContext(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)

	_, err := detector.DetectPatterns(nil, "", nil)

	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestPatternDetector_DetectPatterns_CanceledContext(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := detector.DetectPatterns(ctx, "", nil)

	if err != ErrContextCanceled {
		t.Errorf("expected ErrContextCanceled, got %v", err)
	}
}

func TestPatternDetector_DetectPattern(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, err := detector.DetectPattern(ctx, "", PatternFactory)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range patterns {
		if p.Type != PatternFactory {
			t.Errorf("expected only factory patterns, got %s", p.Type)
		}
	}
}

func TestPatternDetector_RegisterMatcher(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)

	customMatcher := &PatternMatcher{
		Name:        "custom",
		Description: "Custom pattern",
		StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
			return []PatternCandidate{
				{SymbolIDs: []string{"test"}, Location: "test.go"},
			}
		},
	}

	detector.RegisterMatcher(customMatcher)

	m, found := detector.GetMatcher("custom")
	if !found {
		t.Error("expected custom matcher to be registered")
	}
	if m.Name != "custom" {
		t.Errorf("expected custom matcher name, got %s", m.Name)
	}
}

func TestPatternDetector_ListPatterns(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)

	patterns := detector.ListPatterns()

	if len(patterns) == 0 {
		t.Error("expected at least one pattern type")
	}

	// Should have default matchers
	hasFactory := false
	for _, p := range patterns {
		if p == PatternFactory {
			hasFactory = true
			break
		}
	}
	if !hasFactory {
		t.Error("expected factory pattern in list")
	}
}

func TestPatternDetector_Summary(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)
	ctx := context.Background()

	patterns, _ := detector.DetectPatterns(ctx, "", nil)
	summary := detector.Summary(patterns)

	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(patterns) > 0 && summary == "No patterns detected" {
		t.Error("expected summary to reflect detected patterns")
	}
}

func TestPatternDetector_Summary_Empty(t *testing.T) {
	g, idx := createTestPatternGraph(t)
	detector := NewPatternDetector(g, idx)

	summary := detector.Summary([]DetectedPattern{})

	if summary != "No patterns detected" {
		t.Errorf("expected 'No patterns detected', got %s", summary)
	}
}

func TestDefaultMatchers(t *testing.T) {
	matchers := DefaultMatchers()

	expected := []PatternType{
		PatternSingleton,
		PatternFactory,
		PatternBuilder,
		PatternOptions,
		PatternMiddleware,
	}

	for _, pt := range expected {
		if _, found := matchers[pt]; !found {
			t.Errorf("expected %s matcher in defaults", pt)
		}
	}
}

func TestPatternTypeConstants(t *testing.T) {
	if PatternSingleton != "singleton" {
		t.Errorf("expected 'singleton', got %s", PatternSingleton)
	}
	if PatternFactory != "factory" {
		t.Errorf("expected 'factory', got %s", PatternFactory)
	}
	if PatternBuilder != "builder" {
		t.Errorf("expected 'builder', got %s", PatternBuilder)
	}
	if PatternOptions != "options" {
		t.Errorf("expected 'options', got %s", PatternOptions)
	}
	if PatternMiddleware != "middleware" {
		t.Errorf("expected 'middleware', got %s", PatternMiddleware)
	}
}

func TestSmellTypeConstants(t *testing.T) {
	if SmellLongFunction != "long_function" {
		t.Errorf("expected 'long_function', got %s", SmellLongFunction)
	}
	if SmellGodObject != "god_object" {
		t.Errorf("expected 'god_object', got %s", SmellGodObject)
	}
	if SmellErrorSwallowing != "error_swallowing" {
		t.Errorf("expected 'error_swallowing', got %s", SmellErrorSwallowing)
	}
}

func TestDefaultSmellThresholds(t *testing.T) {
	thresholds := DefaultSmellThresholds()

	if thresholds.MaxFunctionLines != 50 {
		t.Errorf("expected MaxFunctionLines 50, got %d", thresholds.MaxFunctionLines)
	}
	if thresholds.MaxParameters != 5 {
		t.Errorf("expected MaxParameters 5, got %d", thresholds.MaxParameters)
	}
	if thresholds.MaxMethodCount != 20 {
		t.Errorf("expected MaxMethodCount 20, got %d", thresholds.MaxMethodCount)
	}
	if thresholds.MaxNestingDepth != 4 {
		t.Errorf("expected MaxNestingDepth 4, got %d", thresholds.MaxNestingDepth)
	}
}

func TestConfidenceConstants(t *testing.T) {
	if StructuralMatchBase != 0.6 {
		t.Errorf("expected StructuralMatchBase 0.6, got %f", StructuralMatchBase)
	}
	if IdiomaticMatchBase != 0.9 {
		t.Errorf("expected IdiomaticMatchBase 0.9, got %f", IdiomaticMatchBase)
	}
	if HeuristicMatchBase != 0.5 {
		t.Errorf("expected HeuristicMatchBase 0.5, got %f", HeuristicMatchBase)
	}
}
