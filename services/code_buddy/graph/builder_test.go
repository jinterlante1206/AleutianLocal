// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// Helper function to create a test symbol.
func testSymbol(name string, kind ast.SymbolKind, filePath string, line int) *ast.Symbol {
	return &ast.Symbol{
		ID:        ast.GenerateID(filePath, line, name),
		Name:      name,
		Kind:      kind,
		FilePath:  filePath,
		StartLine: line,
		EndLine:   line + 10,
		StartCol:  0,
		EndCol:    50,
		Language:  "go",
	}
}

// Helper function to create a test parse result.
func testParseResult(filePath string, symbols []*ast.Symbol, imports []ast.Import) *ast.ParseResult {
	return &ast.ParseResult{
		FilePath: filePath,
		Language: "go",
		Symbols:  symbols,
		Imports:  imports,
		Package:  "test",
	}
}

func TestBuilder_NewBuilder(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		builder := NewBuilder()
		if builder == nil {
			t.Fatal("NewBuilder returned nil")
		}
		if builder.options.MaxMemoryMB != DefaultMaxMemoryMB {
			t.Errorf("expected MaxMemoryMB=%d, got %d", DefaultMaxMemoryMB, builder.options.MaxMemoryMB)
		}
		if builder.options.WorkerCount <= 0 {
			t.Error("expected WorkerCount > 0")
		}
	})

	t.Run("custom options", func(t *testing.T) {
		builder := NewBuilder(
			WithProjectRoot("/test/project"),
			WithMaxMemoryMB(1024),
			WithWorkerCount(4),
		)
		if builder.options.ProjectRoot != "/test/project" {
			t.Errorf("expected ProjectRoot=%q, got %q", "/test/project", builder.options.ProjectRoot)
		}
		if builder.options.MaxMemoryMB != 1024 {
			t.Errorf("expected MaxMemoryMB=1024, got %d", builder.options.MaxMemoryMB)
		}
		if builder.options.WorkerCount != 4 {
			t.Errorf("expected WorkerCount=4, got %d", builder.options.WorkerCount)
		}
	})
}

func TestBuilder_Build_EmptyResults(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	t.Run("nil results slice", func(t *testing.T) {
		result, err := builder.Build(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Graph == nil {
			t.Fatal("expected non-nil graph")
		}
		if result.Graph.NodeCount() != 0 {
			t.Errorf("expected 0 nodes, got %d", result.Graph.NodeCount())
		}
		if result.Graph.EdgeCount() != 0 {
			t.Errorf("expected 0 edges, got %d", result.Graph.EdgeCount())
		}
		if result.Incomplete {
			t.Error("expected Incomplete=false for empty build")
		}
	})

	t.Run("empty results slice", func(t *testing.T) {
		result, err := builder.Build(ctx, []*ast.ParseResult{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Graph.NodeCount() != 0 {
			t.Errorf("expected 0 nodes, got %d", result.Graph.NodeCount())
		}
		if !result.Success() {
			t.Error("expected Success()=true for empty build")
		}
	})
}

func TestBuilder_Build_SingleFile(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	symbols := []*ast.Symbol{
		testSymbol("main", ast.SymbolKindFunction, "main.go", 1),
		testSymbol("helper", ast.SymbolKindFunction, "main.go", 15),
		testSymbol("Config", ast.SymbolKindStruct, "main.go", 30),
	}

	parseResult := testParseResult("main.go", symbols, nil)
	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Graph.NodeCount() != 3 {
		t.Errorf("expected 3 nodes, got %d", result.Graph.NodeCount())
	}

	// Verify all symbols are in the graph
	for _, sym := range symbols {
		node, ok := result.Graph.GetNode(sym.ID)
		if !ok {
			t.Errorf("symbol %s not found in graph", sym.ID)
		}
		if node.Symbol.Name != sym.Name {
			t.Errorf("expected symbol name %s, got %s", sym.Name, node.Symbol.Name)
		}
	}

	if result.Stats.NodesCreated != 3 {
		t.Errorf("expected NodesCreated=3, got %d", result.Stats.NodesCreated)
	}

	if result.Stats.FilesProcessed != 1 {
		t.Errorf("expected FilesProcessed=1, got %d", result.Stats.FilesProcessed)
	}
}

func TestBuilder_Build_WithImports(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	symbols := []*ast.Symbol{
		testSymbol("main", ast.SymbolKindFunction, "main.go", 1),
	}

	imports := []ast.Import{
		{
			Path:  "fmt",
			Alias: "fmt",
			Location: ast.Location{
				FilePath:  "main.go",
				StartLine: 3,
				EndLine:   3,
			},
		},
		{
			Path:  "github.com/pkg/errors",
			Alias: "errors",
			Location: ast.Location{
				FilePath:  "main.go",
				StartLine: 4,
				EndLine:   4,
			},
		},
	}

	parseResult := testParseResult("main.go", symbols, imports)
	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have placeholder nodes for imports
	if result.Stats.PlaceholderNodes < 2 {
		t.Errorf("expected at least 2 placeholder nodes for imports, got %d", result.Stats.PlaceholderNodes)
	}

	// Check that import placeholder exists
	fmtPlaceholder, ok := result.Graph.GetNode("external:fmt:fmt")
	if !ok {
		t.Error("expected placeholder node for fmt import")
	}
	if fmtPlaceholder != nil && fmtPlaceholder.Symbol.Kind != ast.SymbolKindExternal {
		t.Errorf("expected external kind, got %s", fmtPlaceholder.Symbol.Kind)
	}
}

func TestBuilder_Build_WithReceiver(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	structSym := testSymbol("UserService", ast.SymbolKindStruct, "service.go", 10)

	methodSym := testSymbol("Create", ast.SymbolKindMethod, "service.go", 20)
	methodSym.Receiver = "*UserService"

	symbols := []*ast.Symbol{structSym, methodSym}
	parseResult := testParseResult("service.go", symbols, nil)

	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have RECEIVES edge from method to struct
	if result.Stats.EdgesCreated == 0 {
		t.Error("expected at least 1 edge (RECEIVES)")
	}

	// Check the method node has outgoing RECEIVES edge
	methodNode, ok := result.Graph.GetNode(methodSym.ID)
	if !ok {
		t.Fatal("method node not found")
	}

	foundReceives := false
	for _, edge := range methodNode.Outgoing {
		if edge.Type == EdgeTypeReceives {
			foundReceives = true
			break
		}
	}

	if !foundReceives {
		t.Error("expected RECEIVES edge from method to receiver type")
	}
}

func TestBuilder_Build_WithImplements(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	ifaceSym := testSymbol("Reader", ast.SymbolKindInterface, "types.go", 5)

	structSym := testSymbol("FileReader", ast.SymbolKindStruct, "types.go", 15)
	structSym.Metadata = &ast.SymbolMetadata{
		Implements: []string{"Reader"},
	}

	symbols := []*ast.Symbol{ifaceSym, structSym}
	parseResult := testParseResult("types.go", symbols, nil)

	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have IMPLEMENTS edge from struct to interface
	structNode, ok := result.Graph.GetNode(structSym.ID)
	if !ok {
		t.Fatal("struct node not found")
	}

	foundImplements := false
	for _, edge := range structNode.Outgoing {
		if edge.Type == EdgeTypeImplements && edge.ToID == ifaceSym.ID {
			foundImplements = true
			break
		}
	}

	if !foundImplements {
		t.Error("expected IMPLEMENTS edge from FileReader to Reader")
	}
}

func TestBuilder_Build_WithEmbeds(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	baseSym := testSymbol("BaseService", ast.SymbolKindStruct, "base.go", 5)

	childSym := testSymbol("UserService", ast.SymbolKindStruct, "user.go", 10)
	childSym.Metadata = &ast.SymbolMetadata{
		Extends: "BaseService",
	}

	parseResults := []*ast.ParseResult{
		testParseResult("base.go", []*ast.Symbol{baseSym}, nil),
		testParseResult("user.go", []*ast.Symbol{childSym}, nil),
	}

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have EMBEDS edge from child to base
	childNode, ok := result.Graph.GetNode(childSym.ID)
	if !ok {
		t.Fatal("child node not found")
	}

	foundEmbeds := false
	for _, edge := range childNode.Outgoing {
		if edge.Type == EdgeTypeEmbeds {
			foundEmbeds = true
			break
		}
	}

	if !foundEmbeds {
		t.Error("expected EMBEDS edge from UserService to BaseService")
	}
}

func TestBuilder_Build_PlaceholderDeduplication(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	// Multiple files importing same package
	parseResults := []*ast.ParseResult{
		testParseResult("a.go", []*ast.Symbol{testSymbol("A", ast.SymbolKindFunction, "a.go", 1)}, []ast.Import{
			{Path: "fmt", Alias: "fmt", Location: ast.Location{FilePath: "a.go", StartLine: 1}},
		}),
		testParseResult("b.go", []*ast.Symbol{testSymbol("B", ast.SymbolKindFunction, "b.go", 1)}, []ast.Import{
			{Path: "fmt", Alias: "fmt", Location: ast.Location{FilePath: "b.go", StartLine: 1}},
		}),
		testParseResult("c.go", []*ast.Symbol{testSymbol("C", ast.SymbolKindFunction, "c.go", 1)}, []ast.Import{
			{Path: "fmt", Alias: "fmt", Location: ast.Location{FilePath: "c.go", StartLine: 1}},
		}),
	}

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have ONE placeholder for fmt despite 3 imports
	if result.Stats.PlaceholderNodes != 1 {
		t.Errorf("expected 1 placeholder (fmt deduplicated), got %d", result.Stats.PlaceholderNodes)
	}

	// Verify the placeholder exists
	_, ok := result.Graph.GetNode("external:fmt:fmt")
	if !ok {
		t.Error("expected fmt placeholder node")
	}
}

func TestBuilder_Build_NilParseResult(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	validResult1 := testParseResult("valid1.go", []*ast.Symbol{
		testSymbol("Valid1", ast.SymbolKindFunction, "valid1.go", 1),
	}, nil)

	validResult2 := testParseResult("valid2.go", []*ast.Symbol{
		testSymbol("Valid2", ast.SymbolKindFunction, "valid2.go", 1),
	}, nil)

	// Mix of valid and nil results
	parseResults := []*ast.ParseResult{
		validResult1,
		nil, // This should cause a FileError
		validResult2,
	}

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have processed valid files
	if result.Stats.FilesProcessed != 2 {
		t.Errorf("expected 2 files processed, got %d", result.Stats.FilesProcessed)
	}

	// Should have one file error
	if result.Stats.FilesFailed != 1 {
		t.Errorf("expected 1 file failed, got %d", result.Stats.FilesFailed)
	}

	if len(result.FileErrors) != 1 {
		t.Errorf("expected 1 FileError, got %d", len(result.FileErrors))
	}

	// Build should not be marked incomplete for non-fatal errors
	if result.Incomplete {
		t.Error("expected Incomplete=false for non-fatal file errors")
	}
}

func TestBuilder_Build_NilSymbol(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	// Create symbols with unique IDs
	sym1 := testSymbol("Valid", ast.SymbolKindFunction, "test.go", 1)
	sym2 := testSymbol("AlsoValid", ast.SymbolKindFunction, "test.go", 20)

	symbols := []*ast.Symbol{
		sym1,
		nil, // This should be skipped
		sym2,
	}

	parseResult := testParseResult("test.go", symbols, nil)
	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 nodes (nil symbol skipped)
	if result.Graph.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", result.Graph.NodeCount())
	}

	// Verify both valid symbols are in the graph
	if _, ok := result.Graph.GetNode(sym1.ID); !ok {
		t.Errorf("expected symbol %s in graph", sym1.ID)
	}
	if _, ok := result.Graph.GetNode(sym2.ID); !ok {
		t.Errorf("expected symbol %s in graph", sym2.ID)
	}
}

func TestBuilder_Build_ContextCancellation(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))

	// Create many files to process
	var parseResults []*ast.ParseResult
	for i := 0; i < 100; i++ {
		parseResults = append(parseResults, testParseResult(
			"file"+string(rune('a'+i%26))+".go",
			[]*ast.Symbol{testSymbol("Func", ast.SymbolKindFunction, "file.go", i)},
			nil,
		))
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be marked incomplete
	if !result.Incomplete {
		t.Error("expected Incomplete=true when context cancelled")
	}

	// Should still have a valid (partial) graph
	if result.Graph == nil {
		t.Error("expected non-nil graph even with cancellation")
	}
}

func TestBuilder_Build_ContextTimeout(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))

	// Create files
	var parseResults []*ast.ParseResult
	for i := 0; i < 10; i++ {
		parseResults = append(parseResults, testParseResult(
			"file.go",
			[]*ast.Symbol{testSymbol("Func", ast.SymbolKindFunction, "file.go", i+1)},
			nil,
		))
	}

	// Very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for timeout
	time.Sleep(1 * time.Millisecond)

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be marked incomplete
	if !result.Incomplete {
		t.Error("expected Incomplete=true when context timed out")
	}
}

func TestBuilder_Build_ProgressCallback(t *testing.T) {
	var progressUpdates []BuildProgress

	builder := NewBuilder(
		WithProjectRoot("/test"),
		WithProgressCallback(func(p BuildProgress) {
			progressUpdates = append(progressUpdates, p)
		}),
	)

	symbols := []*ast.Symbol{
		testSymbol("A", ast.SymbolKindFunction, "a.go", 1),
		testSymbol("B", ast.SymbolKindFunction, "b.go", 1),
	}

	parseResults := []*ast.ParseResult{
		testParseResult("a.go", []*ast.Symbol{symbols[0]}, nil),
		testParseResult("b.go", []*ast.Symbol{symbols[1]}, nil),
	}

	ctx := context.Background()
	_, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have received progress updates
	if len(progressUpdates) == 0 {
		t.Error("expected progress updates")
	}

	// Check that we got updates for both phases
	hasCollecting := false
	hasExtracting := false
	hasFinalizing := false

	for _, p := range progressUpdates {
		switch p.Phase {
		case ProgressPhaseCollecting:
			hasCollecting = true
		case ProgressPhaseExtractingEdges:
			hasExtracting = true
		case ProgressPhaseFinalizing:
			hasFinalizing = true
		}
	}

	if !hasCollecting {
		t.Error("expected collecting phase progress")
	}
	if !hasExtracting {
		t.Error("expected extracting edges phase progress")
	}
	if !hasFinalizing {
		t.Error("expected finalizing phase progress")
	}
}

func TestBuilder_Build_InvalidFilePath(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	// Path traversal attempt
	parseResult := &ast.ParseResult{
		FilePath: "../etc/passwd",
		Language: "go",
		Symbols:  []*ast.Symbol{testSymbol("Evil", ast.SymbolKindFunction, "../etc/passwd", 1)},
	}

	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have a file error for path traversal
	if len(result.FileErrors) == 0 {
		t.Error("expected FileError for path traversal attempt")
	}

	if result.Stats.FilesFailed != 1 {
		t.Errorf("expected 1 file failed, got %d", result.Stats.FilesFailed)
	}
}

func TestBuilder_Build_GraphIsFrozen(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	parseResult := testParseResult("test.go", []*ast.Symbol{
		testSymbol("Test", ast.SymbolKindFunction, "test.go", 1),
	}, nil)

	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Graph should be frozen after build
	if !result.Graph.IsFrozen() {
		t.Error("expected graph to be frozen after build")
	}

	// Attempting to add node should fail
	_, addErr := result.Graph.AddNode(testSymbol("New", ast.SymbolKindFunction, "new.go", 1))
	if addErr == nil {
		t.Error("expected error when adding to frozen graph")
	}
}

func TestBuilder_Build_StatsAccuracy(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	symbols := []*ast.Symbol{
		testSymbol("A", ast.SymbolKindFunction, "a.go", 1),
		testSymbol("B", ast.SymbolKindFunction, "a.go", 10),
		testSymbol("C", ast.SymbolKindStruct, "a.go", 20),
	}

	parseResult := testParseResult("a.go", symbols, nil)
	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Stats.FilesProcessed != 1 {
		t.Errorf("expected FilesProcessed=1, got %d", result.Stats.FilesProcessed)
	}

	if result.Stats.FilesFailed != 0 {
		t.Errorf("expected FilesFailed=0, got %d", result.Stats.FilesFailed)
	}

	if result.Stats.NodesCreated != 3 {
		t.Errorf("expected NodesCreated=3, got %d", result.Stats.NodesCreated)
	}

	// DurationMilli may be 0 for very fast builds, just verify it's non-negative
	if result.Stats.DurationMilli < 0 {
		t.Error("expected DurationMilli >= 0")
	}
}

func TestBuilder_Build_ChildSymbols(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	classSym := testSymbol("UserService", ast.SymbolKindClass, "service.go", 10)
	classSym.Children = []*ast.Symbol{
		testSymbol("Create", ast.SymbolKindMethod, "service.go", 15),
		testSymbol("Delete", ast.SymbolKindMethod, "service.go", 25),
	}

	parseResult := testParseResult("service.go", []*ast.Symbol{classSym}, nil)
	result, err := builder.Build(ctx, []*ast.ParseResult{parseResult})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 nodes: class + 2 methods
	if result.Graph.NodeCount() != 3 {
		t.Errorf("expected 3 nodes (1 class + 2 methods), got %d", result.Graph.NodeCount())
	}

	// Verify all nodes exist
	for _, child := range classSym.Children {
		if _, ok := result.Graph.GetNode(child.ID); !ok {
			t.Errorf("child symbol %s not found in graph", child.ID)
		}
	}
}

func TestBuilder_Build_ReturnTypeEdges(t *testing.T) {
	builder := NewBuilder(WithProjectRoot("/test"))
	ctx := context.Background()

	userSym := testSymbol("User", ast.SymbolKindStruct, "types.go", 5)

	funcSym := testSymbol("GetUser", ast.SymbolKindFunction, "handlers.go", 10)
	funcSym.Metadata = &ast.SymbolMetadata{
		ReturnType: "*User",
	}

	parseResults := []*ast.ParseResult{
		testParseResult("types.go", []*ast.Symbol{userSym}, nil),
		testParseResult("handlers.go", []*ast.Symbol{funcSym}, nil),
	}

	result, err := builder.Build(ctx, parseResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have RETURNS edge from function to User type
	funcNode, ok := result.Graph.GetNode(funcSym.ID)
	if !ok {
		t.Fatal("function node not found")
	}

	foundReturns := false
	for _, edge := range funcNode.Outgoing {
		if edge.Type == EdgeTypeReturns {
			foundReturns = true
			break
		}
	}

	if !foundReturns {
		t.Error("expected RETURNS edge from GetUser to User")
	}
}

func TestBuildResult_Methods(t *testing.T) {
	t.Run("HasErrors", func(t *testing.T) {
		result := &BuildResult{}
		if result.HasErrors() {
			t.Error("expected HasErrors=false for empty result")
		}

		result.FileErrors = append(result.FileErrors, FileError{FilePath: "test.go"})
		if !result.HasErrors() {
			t.Error("expected HasErrors=true with file error")
		}
	})

	t.Run("TotalErrors", func(t *testing.T) {
		result := &BuildResult{
			FileErrors: []FileError{{FilePath: "a.go"}, {FilePath: "b.go"}},
			EdgeErrors: []EdgeError{{FromID: "x"}},
		}
		if result.TotalErrors() != 3 {
			t.Errorf("expected TotalErrors=3, got %d", result.TotalErrors())
		}
	})

	t.Run("Success", func(t *testing.T) {
		result := &BuildResult{}
		if !result.Success() {
			t.Error("expected Success=true for clean build")
		}

		result.Incomplete = true
		if result.Success() {
			t.Error("expected Success=false when incomplete")
		}

		result.Incomplete = false
		result.FileErrors = append(result.FileErrors, FileError{})
		if result.Success() {
			t.Error("expected Success=false with errors")
		}
	})
}

func TestExtractTypeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"User", "User"},
		{"*User", "User"},
		{"[]User", "User"},
		{"[]*User", "User"},
		{"map[string]User", "User"},
		{"chan User", "User"},
		{"<-chan User", "User"},
		{"chan<- User", "User"},
		{"string", ""}, // Built-in
		{"int", ""},    // Built-in
		{"error", ""},  // Built-in
		{"Response[T]", "Response"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := extractTypeName(tc.input)
			if result != tc.expected {
				t.Errorf("extractTypeName(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestExtractDir(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"handlers/user.go", "handlers"},
		{"pkg/service/auth.go", "pkg/service"},
		{"main.go", ""},
		{"a/b/c/d.go", "a/b/c"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := extractDir(tc.input)
			if result != tc.expected {
				t.Errorf("extractDir(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

// Fix the typo in earlier test - parseResults -> []*ast.ParseResult{parseResult}
func init() {
	// This is just to make sure the tests compile
}
