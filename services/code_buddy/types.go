// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package code_buddy

import (
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	cbcontext "github.com/AleutianAI/AleutianFOSS/services/code_buddy/context"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// InitRequest is the request body for POST /v1/codebuddy/init.
type InitRequest struct {
	// ProjectRoot is the absolute path to the project root directory.
	// Required.
	ProjectRoot string `json:"project_root" binding:"required"`

	// Languages is the list of languages to parse. Default: ["go"].
	Languages []string `json:"languages"`

	// ExcludePatterns is a list of glob patterns to exclude. Default: ["vendor/*", "*_test.go"].
	ExcludePatterns []string `json:"exclude_patterns"`
}

// InitResponse is the response for POST /v1/codebuddy/init.
type InitResponse struct {
	// GraphID is the unique identifier for this graph.
	GraphID string `json:"graph_id"`

	// IsRefresh indicates if this replaced an existing graph.
	IsRefresh bool `json:"is_refresh"`

	// PreviousID is the ID of the replaced graph (if IsRefresh is true).
	PreviousID string `json:"previous_id,omitempty"`

	// FilesParsed is the number of files successfully parsed.
	FilesParsed int `json:"files_parsed"`

	// SymbolsExtracted is the total number of symbols extracted.
	SymbolsExtracted int `json:"symbols_extracted"`

	// EdgesBuilt is the total number of edges created.
	EdgesBuilt int `json:"edges_built"`

	// ParseTimeMs is the total parse time in milliseconds.
	ParseTimeMs int64 `json:"parse_time_ms"`

	// Errors contains non-fatal errors encountered during parsing.
	Errors []string `json:"errors,omitempty"`
}

// ContextRequest is the request body for POST /v1/codebuddy/context.
type ContextRequest struct {
	// GraphID is the graph to query. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// Query is the search query or task description. Required.
	Query string `json:"query" binding:"required"`

	// TokenBudget is the maximum tokens to use. Default: 8000.
	TokenBudget int `json:"token_budget"`

	// IncludeLibraryDocs enables library documentation lookup. Default: true.
	IncludeLibraryDocs *bool `json:"include_library_docs"`
}

// ContextResponse is the response for POST /v1/codebuddy/context.
type ContextResponse struct {
	// Context is the formatted markdown context for LLM consumption.
	Context string `json:"context"`

	// TokensUsed is the estimated number of tokens used.
	TokensUsed int `json:"tokens_used"`

	// SymbolsIncluded lists the IDs of symbols included in context.
	SymbolsIncluded []string `json:"symbols_included"`

	// LibraryDocsIncluded lists the IDs of library docs included.
	LibraryDocsIncluded []string `json:"library_docs_included"`

	// Suggestions provides "also consider" hints.
	Suggestions []string `json:"suggestions"`
}

// CallersRequest is the query params for GET /v1/codebuddy/callers.
type CallersRequest struct {
	// GraphID is the graph to query. Required.
	GraphID string `form:"graph_id" binding:"required"`

	// Function is the function name to find callers for. Required.
	Function string `form:"function" binding:"required"`

	// Limit is the maximum number of results. Default: 50.
	Limit int `form:"limit"`
}

// CallersResponse is the response for GET /v1/codebuddy/callers.
type CallersResponse struct {
	// Function is the function name that was searched.
	Function string `json:"function"`

	// Callers is the list of symbols that call the function.
	Callers []*SymbolInfo `json:"callers"`
}

// ImplementationsRequest is the query params for GET /v1/codebuddy/implementations.
type ImplementationsRequest struct {
	// GraphID is the graph to query. Required.
	GraphID string `form:"graph_id" binding:"required"`

	// Interface is the interface name to find implementations for. Required.
	Interface string `form:"interface" binding:"required"`

	// Limit is the maximum number of results. Default: 50.
	Limit int `form:"limit"`
}

// ImplementationsResponse is the response for GET /v1/codebuddy/implementations.
type ImplementationsResponse struct {
	// Interface is the interface name that was searched.
	Interface string `json:"interface"`

	// Implementations is the list of types that implement the interface.
	Implementations []*SymbolInfo `json:"implementations"`
}

// SymbolRequest is the query params for GET /v1/codebuddy/symbol/:id.
type SymbolRequest struct {
	// GraphID is the graph to query. Required.
	GraphID string `form:"graph_id" binding:"required"`
}

// SymbolResponse is the response for GET /v1/codebuddy/symbol/:id.
type SymbolResponse struct {
	// Symbol is the detailed symbol information.
	Symbol *SymbolInfo `json:"symbol"`
}

// SymbolInfo is a simplified symbol representation for API responses.
type SymbolInfo struct {
	// ID is the unique symbol identifier.
	ID string `json:"id"`

	// Name is the symbol name.
	Name string `json:"name"`

	// Kind is the symbol kind (function, struct, interface, etc.).
	Kind string `json:"kind"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// StartLine is the 1-indexed line where the symbol starts.
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed line where the symbol ends.
	EndLine int `json:"end_line"`

	// Signature is the type signature.
	Signature string `json:"signature,omitempty"`

	// DocComment is the documentation comment.
	DocComment string `json:"doc_comment,omitempty"`

	// Package is the package name.
	Package string `json:"package,omitempty"`

	// Exported indicates if the symbol is publicly visible.
	Exported bool `json:"exported"`
}

// SeedRequest is the request body for POST /v1/codebuddy/seed.
type SeedRequest struct {
	// ProjectRoot is the absolute path to the project root. Required.
	ProjectRoot string `json:"project_root" binding:"required"`

	// DataSpace is the Weaviate data space for isolation. Required.
	DataSpace string `json:"data_space" binding:"required"`
}

// SeedResponse is the response for POST /v1/codebuddy/seed.
type SeedResponse struct {
	// DependenciesFound is the number of dependencies discovered.
	DependenciesFound int `json:"dependencies_found"`

	// DocsIndexed is the number of documentation entries indexed.
	DocsIndexed int `json:"docs_indexed"`

	// Errors contains non-fatal errors encountered during seeding.
	Errors []string `json:"errors,omitempty"`
}

// HealthResponse is the response for GET /v1/codebuddy/health.
type HealthResponse struct {
	// Status is "healthy" or "degraded".
	Status string `json:"status"`

	// Version is the service version.
	Version string `json:"version"`
}

// ReadyResponse is the response for GET /v1/codebuddy/ready.
type ReadyResponse struct {
	// Ready is true if the service is ready to accept requests.
	Ready bool `json:"ready"`

	// GraphCount is the number of cached graphs.
	GraphCount int `json:"graph_count"`

	// WeaviateOK is true if Weaviate connection is healthy.
	WeaviateOK bool `json:"weaviate_ok"`
}

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	// Error is the error message.
	Error string `json:"error"`

	// Code is the error code (optional).
	Code string `json:"code,omitempty"`

	// Details provides additional error context (optional).
	Details string `json:"details,omitempty"`
}

// CachedGraph holds a graph and its associated data.
type CachedGraph struct {
	// Graph is the code graph.
	Graph *graph.Graph

	// Index is the symbol index.
	Index *index.SymbolIndex

	// Assembler is the context assembler.
	Assembler *cbcontext.Assembler

	// BuiltAtMilli is when the graph was built.
	BuiltAtMilli int64

	// ProjectRoot is the project root path.
	ProjectRoot string

	// ExpiresAtMilli is when the graph expires (0 = never).
	ExpiresAtMilli int64
}

// SymbolInfoFromAST converts an ast.Symbol to SymbolInfo.
func SymbolInfoFromAST(s *ast.Symbol) *SymbolInfo {
	if s == nil {
		return nil
	}
	return &SymbolInfo{
		ID:         s.ID,
		Name:       s.Name,
		Kind:       s.Kind.String(),
		FilePath:   s.FilePath,
		StartLine:  s.StartLine,
		EndLine:    s.EndLine,
		Signature:  s.Signature,
		DocComment: s.DocComment,
		Package:    s.Package,
		Exported:   s.Exported,
	}
}
