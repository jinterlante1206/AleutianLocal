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
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// FileSummarizer provides structured summaries of source files.
//
// Thread Safety:
//
//	FileSummarizer is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type FileSummarizer struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewFileSummarizer creates a new FileSummarizer.
//
// Description:
//
//	Creates a summarizer that can produce structured summaries of files
//	using the provided graph and symbol index.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*FileSummarizer - The configured summarizer.
//
// Example:
//
//	summarizer := NewFileSummarizer(graph, index)
//	summary, err := summarizer.SummarizeFile(ctx, "handlers/user.go")
func NewFileSummarizer(g *graph.Graph, idx *index.SymbolIndex) *FileSummarizer {
	return &FileSummarizer{
		graph: g,
		index: idx,
	}
}

// SummarizeFile produces a structured summary of a file.
//
// Description:
//
//	Extracts types, functions, imports, and metadata from a file
//	to provide a quick overview without reading the entire file.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - Relative path to the file.
//
// Outputs:
//
//	*FileSummary - Structured summary of the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Errors:
//
//	ErrFileNotFound - File not found in the index.
//	ErrContextCanceled - Context was canceled.
//
// Performance:
//
//	Target latency: < 50ms.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *FileSummarizer) SummarizeFile(ctx context.Context, filePath string) (*FileSummary, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Get all symbols in the file
	symbols := s.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	summary := &FileSummary{
		FilePath:  filePath,
		Imports:   make([]string, 0),
		Types:     make([]TypeBrief, 0),
		Functions: make([]FuncBrief, 0),
	}

	// Track max line number for line count estimation
	maxLine := 0

	// Process symbols
	for _, sym := range symbols {
		// Track line count
		if sym.EndLine > maxLine {
			maxLine = sym.EndLine
		}

		// Extract package name
		if summary.Package == "" && sym.Package != "" {
			summary.Package = sym.Package
		}

		switch sym.Kind {
		case ast.SymbolKindImport:
			// Handle import symbols - use ID which contains the import path
			if sym.Name != "" && !containsString(summary.Imports, sym.Name) {
				summary.Imports = append(summary.Imports, sym.Name)
			}

		case ast.SymbolKindStruct, ast.SymbolKindClass, ast.SymbolKindInterface, ast.SymbolKindType:
			tb := TypeBrief{
				Name:    sym.Name,
				Kind:    sym.Kind.String(),
				Fields:  countFields(sym),
				Methods: getMethodNames(sym, symbols),
			}
			summary.Types = append(summary.Types, tb)

		case ast.SymbolKindFunction:
			fb := FuncBrief{
				Name:      sym.Name,
				Signature: sym.Signature,
				Exported:  sym.Exported,
			}
			summary.Functions = append(summary.Functions, fb)

		case ast.SymbolKindMethod:
			// Methods are associated with their receiver type
			// Already handled in getMethodNames
		}
	}

	summary.LineCount = maxLine

	// Infer purpose from file name and content
	summary.Purpose = inferFilePurpose(filePath, summary)

	// Sort imports, types, and functions for consistent output
	sort.Strings(summary.Imports)
	sort.Slice(summary.Types, func(i, j int) bool {
		return summary.Types[i].Name < summary.Types[j].Name
	})
	sort.Slice(summary.Functions, func(i, j int) bool {
		return summary.Functions[i].Name < summary.Functions[j].Name
	})

	return summary, nil
}

// SummarizePackage produces summaries for all files in a package.
//
// Description:
//
//	Produces summaries for all files belonging to a package/module.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	packagePath - Package path to summarize.
//
// Outputs:
//
//	[]*FileSummary - Summaries of all files in the package.
//	error - Non-nil if the operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *FileSummarizer) SummarizePackage(ctx context.Context, packagePath string) ([]*FileSummary, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Find all files in the package
	fileSet := make(map[string]bool)
	stats := s.index.Stats()

	// We need to iterate through all symbols to find files in the package
	// This is O(n) but necessary since we don't have a package-to-file index
	for _, kind := range []ast.SymbolKind{
		ast.SymbolKindFunction, ast.SymbolKindMethod,
		ast.SymbolKindStruct, ast.SymbolKindClass, ast.SymbolKindInterface,
	} {
		symbols := s.index.GetByKind(kind)
		for _, sym := range symbols {
			// Match package by:
			// 1. Exact match (e.g., "main" == "main")
			// 2. File path contains the package directory (e.g., "pkg/handlers/user.go" contains "handlers")
			// 3. Package path suffix (e.g., "github.com/foo/handlers" ends with "handlers")
			if matchesPackage(sym, packagePath) {
				fileSet[sym.FilePath] = true
			}
		}
	}

	if len(fileSet) == 0 {
		return nil, ErrPackageNotFound
	}

	_ = stats // Silence unused variable warning

	// Summarize each file
	summaries := make([]*FileSummary, 0, len(fileSet))
	for filePath := range fileSet {
		if err := ctx.Err(); err != nil {
			return summaries, nil
		}

		summary, err := s.SummarizeFile(ctx, filePath)
		if err == nil {
			summaries = append(summaries, summary)
		}
	}

	// Sort by file path
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].FilePath < summaries[j].FilePath
	})

	return summaries, nil
}

// countFields counts the number of fields in a struct/class symbol.
func countFields(sym *ast.Symbol) int {
	if sym.Children == nil {
		return 0
	}

	count := 0
	for _, child := range sym.Children {
		if child.Kind == ast.SymbolKindField {
			count++
		}
	}
	return count
}

// getMethodNames extracts method names for a type from the symbol list.
func getMethodNames(typeSym *ast.Symbol, allSymbols []*ast.Symbol) []string {
	methods := make([]string, 0)

	// Check children first
	if typeSym.Children != nil {
		for _, child := range typeSym.Children {
			if child.Kind == ast.SymbolKindMethod {
				methods = append(methods, child.Name)
			}
		}
	}

	// Also check for methods with matching receiver in all symbols
	for _, sym := range allSymbols {
		if sym.Kind == ast.SymbolKindMethod && sym.Receiver == typeSym.Name {
			if !containsString(methods, sym.Name) {
				methods = append(methods, sym.Name)
			}
		}
	}

	sort.Strings(methods)
	return methods
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// inferFilePurpose attempts to infer the purpose of a file from its name and content.
func inferFilePurpose(filePath string, summary *FileSummary) string {
	// Common patterns in file names
	lowerPath := strings.ToLower(filePath)

	// Check for test files
	if strings.Contains(lowerPath, "_test") || strings.Contains(lowerPath, "test_") {
		return "Unit tests"
	}

	// Check for common file names
	baseName := getBaseName(filePath)
	baseNameLower := strings.ToLower(baseName)

	switch {
	case baseNameLower == "main":
		return "Application entry point"
	case baseNameLower == "types":
		return "Type definitions"
	case baseNameLower == "errors":
		return "Error definitions"
	case baseNameLower == "utils" || baseNameLower == "util" || baseNameLower == "helpers":
		return "Utility functions"
	case baseNameLower == "config" || baseNameLower == "configuration":
		return "Configuration management"
	case baseNameLower == "constants" || baseNameLower == "consts":
		return "Constant definitions"
	case baseNameLower == "models" || baseNameLower == "model":
		return "Data models"
	case strings.Contains(baseNameLower, "handler"):
		return "Request handlers"
	case strings.Contains(baseNameLower, "controller"):
		return "Controllers"
	case strings.Contains(baseNameLower, "service"):
		return "Service layer"
	case strings.Contains(baseNameLower, "repository") || strings.Contains(baseNameLower, "repo"):
		return "Data access layer"
	case strings.Contains(baseNameLower, "middleware"):
		return "Middleware"
	case strings.Contains(baseNameLower, "router") || strings.Contains(baseNameLower, "routes"):
		return "Route definitions"
	case strings.Contains(baseNameLower, "database") || strings.Contains(baseNameLower, "db"):
		return "Database operations"
	case strings.Contains(baseNameLower, "auth"):
		return "Authentication/authorization"
	case strings.Contains(baseNameLower, "api"):
		return "API definitions"
	case strings.Contains(baseNameLower, "client"):
		return "Client implementation"
	case strings.Contains(baseNameLower, "server"):
		return "Server implementation"
	}

	// Infer from content
	// Check for interface-heavy files first
	interfaceCount := 0
	for _, t := range summary.Types {
		if t.Kind == "interface" {
			interfaceCount++
		}
	}
	if interfaceCount > 0 && interfaceCount >= len(summary.Types)/2 {
		return "Interface definitions"
	}

	if len(summary.Types) > 0 && len(summary.Functions) == 0 {
		return "Type definitions"
	}
	if len(summary.Functions) > 0 && len(summary.Types) == 0 {
		return "Function implementations"
	}

	return ""
}

// getBaseName extracts the base name from a file path without extension.
func getBaseName(filePath string) string {
	// Find the last path separator
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		lastSlash = strings.LastIndex(filePath, "\\")
	}

	baseName := filePath
	if lastSlash >= 0 {
		baseName = filePath[lastSlash+1:]
	}

	// Remove extension
	lastDot := strings.LastIndex(baseName, ".")
	if lastDot > 0 {
		baseName = baseName[:lastDot]
	}

	return baseName
}

// PackageAPISummarizer provides public API summaries for packages.
type PackageAPISummarizer struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewPackageAPISummarizer creates a new PackageAPISummarizer.
func NewPackageAPISummarizer(g *graph.Graph, idx *index.SymbolIndex) *PackageAPISummarizer {
	return &PackageAPISummarizer{
		graph: g,
		index: idx,
	}
}

// FindPackageAPI extracts the public API of a package.
//
// Description:
//
//	Returns all exported types, functions, constants, and variables
//	in a package.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	packagePath - Package path to analyze.
//
// Outputs:
//
//	*PackageAPI - The public API of the package.
//	error - Non-nil if the package is not found or operation was canceled.
//
// Performance:
//
//	Target latency: < 50ms.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *PackageAPISummarizer) FindPackageAPI(ctx context.Context, packagePath string) (*PackageAPI, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	api := &PackageAPI{
		Package:   packagePath,
		Types:     make([]APISymbol, 0),
		Functions: make([]APISymbol, 0),
		Constants: make([]APISymbol, 0),
		Variables: make([]APISymbol, 0),
	}

	// Process each relevant symbol kind
	kindProcessors := []struct {
		kind ast.SymbolKind
		add  func(APISymbol)
	}{
		{ast.SymbolKindStruct, func(s APISymbol) { api.Types = append(api.Types, s) }},
		{ast.SymbolKindClass, func(s APISymbol) { api.Types = append(api.Types, s) }},
		{ast.SymbolKindInterface, func(s APISymbol) { api.Types = append(api.Types, s) }},
		{ast.SymbolKindType, func(s APISymbol) { api.Types = append(api.Types, s) }},
		{ast.SymbolKindFunction, func(s APISymbol) { api.Functions = append(api.Functions, s) }},
		{ast.SymbolKindConstant, func(s APISymbol) { api.Constants = append(api.Constants, s) }},
		{ast.SymbolKindVariable, func(s APISymbol) { api.Variables = append(api.Variables, s) }},
	}

	found := false
	for _, kp := range kindProcessors {
		if err := ctx.Err(); err != nil {
			return api, nil
		}

		symbols := s.index.GetByKind(kp.kind)
		for _, sym := range symbols {
			// Use flexible package matching (handles path variants)
			if !matchesPackage(sym, packagePath) {
				continue
			}
			if !sym.Exported {
				continue
			}

			found = true
			kp.add(APISymbol{
				Name:      sym.Name,
				Signature: sym.Signature,
				DocString: sym.DocComment,
			})
		}
	}

	if !found {
		return nil, ErrPackageNotFound
	}

	// Sort all slices
	sortAPISymbols(api.Types)
	sortAPISymbols(api.Functions)
	sortAPISymbols(api.Constants)
	sortAPISymbols(api.Variables)

	return api, nil
}

// sortAPISymbols sorts a slice of APISymbol by name.
func sortAPISymbols(symbols []APISymbol) {
	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].Name < symbols[j].Name
	})
}

// matchesPackage checks if a symbol belongs to the given package path.
//
// Description:
//
//	Matches packages using multiple strategies to handle different naming conventions:
//	1. Exact match (e.g., "main" == "main")
//	2. Package name matches the last component of a path (e.g., "handlers" matches "pkg/handlers")
//	3. File path contains the package directory (e.g., "pkg/handlers/user.go" for "handlers")
//
// Inputs:
//
//	sym - The symbol to check
//	packagePath - The package path to match against
//
// Outputs:
//
//	bool - True if the symbol belongs to the package
func matchesPackage(sym *ast.Symbol, packagePath string) bool {
	if sym == nil {
		return false
	}

	// Strategy 1: Exact package name match
	if sym.Package == packagePath {
		return true
	}

	// Strategy 2: Package name matches the last component
	// e.g., "handlers" should match symbols with package "handlers"
	// where the file is in "pkg/handlers/user.go"
	lastSlash := strings.LastIndex(packagePath, "/")
	if lastSlash >= 0 {
		lastComponent := packagePath[lastSlash+1:]
		if sym.Package == lastComponent {
			// Verify the file path matches the directory structure
			if strings.Contains(sym.FilePath, packagePath+"/") || strings.HasPrefix(sym.FilePath, packagePath+"/") {
				return true
			}
		}
	}

	// Strategy 3: File path contains the package directory
	// This handles cases where packagePath is a directory like "handlers"
	// and the file is "pkg/handlers/user.go"
	if strings.Contains(sym.FilePath, "/"+packagePath+"/") || strings.HasPrefix(sym.FilePath, packagePath+"/") {
		return true
	}

	// Strategy 4: Package ends with the given path component
	// e.g., "github.com/foo/handlers" ends with "/handlers"
	if strings.HasSuffix(sym.Package, "/"+packagePath) {
		return true
	}

	return false
}
