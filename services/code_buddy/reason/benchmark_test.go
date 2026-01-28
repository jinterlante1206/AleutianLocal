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
	"fmt"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// setupBenchmarkGraph creates a graph with the specified number of functions
// and a realistic call pattern for benchmarking.
func setupBenchmarkGraph(numFuncs int) (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test/project")
	idx := index.NewSymbolIndex()

	// Create functions
	funcs := make([]*ast.Symbol, numFuncs)
	for i := 0; i < numFuncs; i++ {
		funcName := fmt.Sprintf("Function%d", i)
		if i < numFuncs/10 {
			funcName = fmt.Sprintf("TestFunction%d", i) // 10% are test functions
		}

		sym := &ast.Symbol{
			ID:        fmt.Sprintf("pkg/file%d.go:%d:%s", i/10, (i%10)*10+1, funcName),
			Name:      funcName,
			Kind:      ast.SymbolKindFunction,
			FilePath:  fmt.Sprintf("pkg/file%d.go", i/10),
			StartLine: (i%10)*10 + 1,
			EndLine:   (i%10)*10 + 9,
			Package:   "pkg",
			Signature: "func() error",
			Language:  "go",
			Exported:  i%2 == 0,
		}
		funcs[i] = sym
		g.AddNode(sym)
		idx.Add(sym)
	}

	// Create call edges: each function calls ~3 others
	for i := 0; i < numFuncs; i++ {
		// Call next few functions to create a realistic call graph
		for j := 1; j <= 3 && i+j < numFuncs; j++ {
			g.AddEdge(funcs[i].ID, funcs[i+j].ID, graph.EdgeTypeCalls, ast.Location{
				FilePath:  funcs[i].FilePath,
				StartLine: funcs[i].StartLine + j,
			})
		}
	}

	g.Freeze()
	return g, idx
}

// BenchmarkSignatureParser benchmarks signature parsing.
func BenchmarkSignatureParser(b *testing.B) {
	parser := NewSignatureParser()
	signatures := []struct {
		name string
		sig  string
		lang string
	}{
		{"simple_go", "func() error", "go"},
		{"complex_go", "func(ctx context.Context, req *Request, opts ...Option) (*Response, error)", "go"},
		{"method_go", "func (s *Service) Handle(ctx context.Context) error", "go"},
		{"simple_python", "def hello():", "python"},
		{"typed_python", "def process(x: int, y: str) -> bool:", "python"},
		{"simple_ts", "function hello(): void {}", "typescript"},
		{"typed_ts", "function process(x: number, y: string): boolean {}", "typescript"},
	}

	for _, tc := range signatures {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parser.ParseSignature(tc.sig, tc.lang)
			}
		})
	}
}

// BenchmarkBreakingChangeAnalyzer benchmarks breaking change detection.
func BenchmarkBreakingChangeAnalyzer(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		g, idx := setupBenchmarkGraph(size)
		analyzer := NewBreakingChangeAnalyzer(g, idx)
		ctx := context.Background()

		// Get a symbol ID to analyze
		targetID := fmt.Sprintf("pkg/file5.go:1:Function50")

		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = analyzer.AnalyzeBreaking(ctx, targetID,
					"func(ctx context.Context) error")
			}
		})
	}
}

// BenchmarkChangeSimulator benchmarks change simulation.
func BenchmarkChangeSimulator(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		g, idx := setupBenchmarkGraph(size)
		simulator := NewChangeSimulator(g, idx)
		ctx := context.Background()

		targetID := fmt.Sprintf("pkg/file5.go:1:Function50")

		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = simulator.SimulateChange(ctx, targetID,
					"func(ctx context.Context, opts Options) error")
			}
		})
	}
}

// BenchmarkChangeValidator benchmarks code validation.
func BenchmarkChangeValidator(b *testing.B) {
	idx := index.NewSymbolIndex()
	validator := NewChangeValidator(idx)
	ctx := context.Background()

	codes := []struct {
		name    string
		content string
		lang    string
	}{
		{"small_go", `package main

func main() {
	fmt.Println("Hello")
}
`, "main.go"},
		{"medium_go", `package main

import (
	"context"
	"fmt"
)

type Service struct {
	name string
}

func (s *Service) Handle(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	fmt.Printf("Handling: %s\n", s.name)
	return nil
}

func main() {
	svc := &Service{name: "test"}
	_ = svc.Handle(context.Background())
}
`, "main.go"},
		{"python", `def hello(name: str) -> str:
    return f"Hello, {name}!"

if __name__ == "__main__":
    print(hello("World"))
`, "main.py"},
		{"typescript", `function greet(name: string): string {
    return "Hello, " + name;
}

console.log(greet("World"));
`, "main.ts"},
	}

	for _, tc := range codes {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = validator.ValidateChange(ctx, tc.lang, tc.content)
			}
		})
	}
}

// BenchmarkTestCoverageFinder benchmarks test coverage analysis.
func BenchmarkTestCoverageFinder(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		g, idx := setupBenchmarkGraph(size)
		finder := NewTestCoverageFinder(g, idx)
		ctx := context.Background()

		// Get a non-test symbol ID
		targetID := fmt.Sprintf("pkg/file5.go:1:Function50")

		b.Run(fmt.Sprintf("FindTestCoverage_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = finder.FindTestCoverage(ctx, targetID)
			}
		})
	}
}

// BenchmarkConfidenceCalibration benchmarks confidence calculations.
func BenchmarkConfidenceCalibration(b *testing.B) {
	b.Run("single_adjustment", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = CalibrateConfidence(0.8, AdjustmentExportedSymbol)
		}
	})

	b.Run("multiple_adjustments", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = CalibrateConfidence(0.8,
				AdjustmentExportedSymbol,
				AdjustmentManyCallers,
				AdjustmentStaticAnalysisOnly,
			)
		}
	})

	b.Run("calibration_struct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cal := NewConfidenceCalibration(0.8)
			cal.ApplyIf(true, AdjustmentExportedSymbol)
			cal.ApplyIf(false, AdjustmentInTestFile)
			cal.Apply(AdjustmentStaticAnalysisOnly)
			_ = cal.FinalScore
		}
	})
}

// BenchmarkTypeCompatibility benchmarks type compatibility checking.
func BenchmarkTypeCompatibility(b *testing.B) {
	g, idx := setupBenchmarkGraph(100)
	checker := NewTypeCompatibilityChecker(g, idx)
	ctx := context.Background()

	typePairs := []struct {
		name   string
		source string
		target string
	}{
		{"same_type", "string", "string"},
		{"pointer_compat", "*User", "User"},
		{"interface_any", "MyType", "interface{}"},
		{"slice_types", "[]int", "[]int"},
	}

	for _, tc := range typePairs {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = checker.CheckCompatibility(ctx, tc.source, tc.target)
			}
		})
	}
}

// BenchmarkSideEffectAnalyzer benchmarks side effect detection.
func BenchmarkSideEffectAnalyzer(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		g, idx := setupBenchmarkGraph(size)
		analyzer := NewSideEffectAnalyzer(g, idx)
		ctx := context.Background()

		targetID := fmt.Sprintf("pkg/file5.go:1:Function50")

		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = analyzer.FindSideEffects(ctx, targetID)
			}
		})
	}
}

// BenchmarkRefactorSuggester benchmarks refactoring suggestions.
func BenchmarkRefactorSuggester(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		g, idx := setupBenchmarkGraph(size)
		suggester := NewRefactorSuggester(g, idx)
		ctx := context.Background()

		targetID := fmt.Sprintf("pkg/file5.go:1:Function50")

		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = suggester.SuggestRefactor(ctx, targetID)
			}
		})
	}
}

// BenchmarkSideEffectPatterns benchmarks pattern lookup.
func BenchmarkSideEffectPatterns(b *testing.B) {
	languages := []string{"go", "python", "typescript"}

	for _, lang := range languages {
		b.Run(lang, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				patterns := GetPatternsForLanguage(lang)
				_ = patterns.GetAllPatterns()
			}
		})
	}
}
