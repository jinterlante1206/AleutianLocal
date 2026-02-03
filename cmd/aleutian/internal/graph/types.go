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
	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// API version for JSON output.
const APIVersion = "1.0"

// Default limits.
const (
	DefaultMaxDepth   = 50
	DefaultMaxResults = 1000
	DefaultTimeout    = 10 // seconds
)

// OutputFormat specifies the output format for query results.
type OutputFormat string

const (
	FormatTree    OutputFormat = "tree"
	FormatFlat    OutputFormat = "flat"
	FormatPaths   OutputFormat = "paths"
	FormatColumns OutputFormat = "columns"
)

// QueryConfig holds configuration for graph queries.
type QueryConfig struct {
	// MaxDepth limits traversal depth. 0 = unlimited (up to DefaultMaxDepth).
	MaxDepth int

	// MaxResults limits number of results. 0 = unlimited (up to DefaultMaxResults).
	MaxResults int

	// IncludeTests includes test files in results.
	IncludeTests bool

	// IncludeStdlib includes standard library calls.
	IncludeStdlib bool

	// Exact requires exact symbol match (no fuzzy).
	Exact bool

	// FailIfEmpty returns error if no results found.
	FailIfEmpty bool
}

// DefaultQueryConfig returns configuration with sensible defaults.
func DefaultQueryConfig() QueryConfig {
	return QueryConfig{
		MaxDepth:      DefaultMaxDepth,
		MaxResults:    DefaultMaxResults,
		IncludeTests:  false,
		IncludeStdlib: false,
		Exact:         false,
		FailIfEmpty:   false,
	}
}

// CallResult represents a single caller or callee result.
type CallResult struct {
	// Symbol information
	SymbolID   string `json:"symbol_id"`
	SymbolName string `json:"symbol_name"`
	Kind       string `json:"kind"`
	FilePath   string `json:"file_path"`
	Line       int    `json:"line"`

	// Depth from the query target (1 = direct caller/callee).
	Depth int `json:"depth"`

	// Path from root to this symbol (for tree output).
	Path []string `json:"path,omitempty"`
}

// QueryResult holds the results of a graph query.
type QueryResult struct {
	APIVersion string `json:"api_version"`
	Query      string `json:"query"`
	Symbol     string `json:"symbol"`

	// Results
	Results []CallResult `json:"results"`

	// Counts
	DirectCount     int `json:"direct_count"`
	TransitiveCount int `json:"transitive_count"`
	TotalCount      int `json:"total_count"`

	// Query info
	MaxDepthUsed int  `json:"max_depth_used"`
	Truncated    bool `json:"truncated,omitempty"`

	// Errors/warnings
	Warnings []string `json:"warnings,omitempty"`
}

// NewQueryResult creates a new QueryResult with defaults.
func NewQueryResult(query, symbol string) *QueryResult {
	return &QueryResult{
		APIVersion: APIVersion,
		Query:      query,
		Symbol:     symbol,
		Results:    make([]CallResult, 0),
		Warnings:   make([]string, 0),
	}
}

// PathResult represents a path between two symbols.
type PathResult struct {
	// Symbols in the path, from source to target.
	Symbols []PathNode `json:"symbols"`

	// Length of the path (number of hops).
	Length int `json:"length"`
}

// PathNode represents a symbol in a path.
type PathNode struct {
	SymbolID   string `json:"symbol_id"`
	SymbolName string `json:"symbol_name"`
	FilePath   string `json:"file_path"`
	Line       int    `json:"line"`
}

// PathQueryResult holds results of a path query.
type PathQueryResult struct {
	APIVersion string `json:"api_version"`
	Query      string `json:"query"`
	From       string `json:"from"`
	To         string `json:"to"`

	// Paths found (may be multiple if --all)
	Paths []PathResult `json:"paths"`

	// Stats
	PathFound bool `json:"path_found"`
	PathCount int  `json:"path_count"`
	Truncated bool `json:"truncated,omitempty"`
	MaxDepth  int  `json:"max_depth"`

	// Errors/warnings
	Warnings []string `json:"warnings,omitempty"`
}

// NewPathQueryResult creates a new PathQueryResult with defaults.
func NewPathQueryResult(from, to string) *PathQueryResult {
	return &PathQueryResult{
		APIVersion: APIVersion,
		Query:      "path",
		From:       from,
		To:         to,
		Paths:      make([]PathResult, 0),
		Warnings:   make([]string, 0),
	}
}

// SymbolResolver resolves symbol names to Symbol objects.
type SymbolResolver interface {
	// ResolveSymbol resolves a symbol input to a Symbol.
	// Returns nil if not found, error on ambiguous match.
	ResolveSymbol(input string, exact bool) (*initializer.Symbol, error)
}
