// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package explore provides high-level exploration tools for code analysis.
//
// These tools compose lower-level graph queries to answer complex questions
// about codebases, such as finding entry points, tracing data flow, and
// building minimal context for understanding functions.
//
// Thread Safety:
//
//	All tools in this package are designed for concurrent use. They perform
//	read-only operations on the graph and index.
package explore

// EntryPointType categorizes different kinds of entry points.
type EntryPointType string

const (
	// EntryPointMain represents main() or __main__ entry points.
	EntryPointMain EntryPointType = "main"

	// EntryPointHandler represents HTTP/REST handlers.
	EntryPointHandler EntryPointType = "handler"

	// EntryPointCommand represents CLI commands.
	EntryPointCommand EntryPointType = "command"

	// EntryPointTest represents test functions.
	EntryPointTest EntryPointType = "test"

	// EntryPointLambda represents serverless function handlers.
	EntryPointLambda EntryPointType = "lambda"

	// EntryPointGRPC represents gRPC service implementations.
	EntryPointGRPC EntryPointType = "grpc"

	// EntryPointAll is a special value for finding all entry point types.
	EntryPointAll EntryPointType = "all"
)

// EntryPoint represents a code entry point discovered in the codebase.
type EntryPoint struct {
	// ID is the unique symbol ID.
	ID string `json:"id"`

	// Name is the symbol name.
	Name string `json:"name"`

	// Type categorizes the entry point (main, handler, command, test, lambda, grpc).
	Type EntryPointType `json:"type"`

	// Framework identifies the framework if detected (gin, echo, cobra, fastapi, etc.).
	Framework string `json:"framework,omitempty"`

	// FilePath is the relative path to the file containing this entry point.
	FilePath string `json:"file_path"`

	// Line is the 1-indexed line number where the entry point is defined.
	Line int `json:"line"`

	// Signature is the function/method signature.
	Signature string `json:"signature"`

	// DocComment contains any documentation comment.
	DocComment string `json:"doc_comment,omitempty"`

	// Package is the package/module containing this entry point.
	Package string `json:"package,omitempty"`
}

// EntryPointResult contains the result of an entry point search.
type EntryPointResult struct {
	// EntryPoints contains all discovered entry points.
	EntryPoints []EntryPoint `json:"entry_points"`

	// Truncated indicates if results were truncated due to limits.
	Truncated bool `json:"truncated,omitempty"`

	// TotalFound is the total count before truncation.
	TotalFound int `json:"total_found"`
}

// SourceCategory categorizes data input sources.
type SourceCategory string

const (
	// SourceHTTP represents HTTP request data (body, query, headers, form).
	SourceHTTP SourceCategory = "http_input"

	// SourceEnv represents environment variable reads.
	SourceEnv SourceCategory = "env_var"

	// SourceFile represents file reads.
	SourceFile SourceCategory = "file_read"

	// SourceCLI represents command line arguments.
	SourceCLI SourceCategory = "cli_arg"

	// SourceDB represents database query results.
	SourceDB SourceCategory = "db_result"

	// SourceWebSocket represents WebSocket message data.
	SourceWebSocket SourceCategory = "websocket"

	// SourceUnknown represents unclassified sources.
	SourceUnknown SourceCategory = "unknown"
)

// SinkCategory categorizes data output sinks.
type SinkCategory string

const (
	// SinkResponse represents HTTP response writes.
	SinkResponse SinkCategory = "response"

	// SinkDatabase represents database writes.
	SinkDatabase SinkCategory = "database"

	// SinkFile represents file writes.
	SinkFile SinkCategory = "file"

	// SinkLog represents logging calls.
	SinkLog SinkCategory = "log"

	// SinkNetwork represents network calls.
	SinkNetwork SinkCategory = "network"

	// SinkCommand represents command execution (dangerous sink).
	SinkCommand SinkCategory = "command"

	// SinkSQL represents SQL query execution (dangerous sink).
	SinkSQL SinkCategory = "sql"

	// SinkUnknown represents unclassified sinks.
	SinkUnknown SinkCategory = "unknown"
)

// DataPoint represents a point in the data flow.
type DataPoint struct {
	// ID is the symbol ID.
	ID string `json:"id"`

	// Type indicates what kind of data point (param, return, field, variable).
	Type string `json:"type"`

	// Name is the symbol name.
	Name string `json:"name"`

	// Location is the file:line reference.
	Location string `json:"location"`

	// Category is the source/sink category (http_input, env_var, etc.).
	Category string `json:"category"`

	// Confidence indicates how certain we are about this classification (0.0-1.0).
	Confidence float64 `json:"confidence"`
}

// DataFlow represents the flow of data through the codebase.
type DataFlow struct {
	// Sources contains where data enters the system.
	Sources []DataPoint `json:"sources"`

	// Transforms contains where data is modified.
	Transforms []DataPoint `json:"transforms"`

	// Sinks contains where data exits the system.
	Sinks []DataPoint `json:"sinks"`

	// Path contains the ordered function calls in the flow.
	Path []string `json:"path"`

	// Precision indicates analysis precision ("function" or "variable").
	Precision string `json:"precision"`

	// Limitations documents what we couldn't track.
	Limitations []string `json:"limitations,omitempty"`
}

// ErrorPoint represents a point in the error flow.
type ErrorPoint struct {
	// Function is the function name.
	Function string `json:"function"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// Line is the 1-indexed line number.
	Line int `json:"line"`

	// Type indicates the error handling type (origin, handler, propagate, escape).
	Type string `json:"type"`

	// Code is the error handling code snippet.
	Code string `json:"code,omitempty"`
}

// ErrorFlow represents error propagation through the codebase.
type ErrorFlow struct {
	// Origins contains where errors are created.
	Origins []ErrorPoint `json:"origins"`

	// Handlers contains where errors are caught/handled.
	Handlers []ErrorPoint `json:"handlers"`

	// Escapes contains unhandled error paths.
	Escapes []ErrorPoint `json:"escapes"`
}

// CodeBlock represents a code snippet with context.
type CodeBlock struct {
	// ID is the symbol ID.
	ID string `json:"id"`

	// Name is the symbol name.
	Name string `json:"name"`

	// Kind is the symbol kind (function, struct, interface, etc.).
	Kind string `json:"kind"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// StartLine is the 1-indexed starting line.
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed ending line.
	EndLine int `json:"end_line"`

	// Signature is the symbol signature.
	Signature string `json:"signature,omitempty"`

	// Code is the actual code content.
	Code string `json:"code,omitempty"`

	// TokenEstimate is an estimated token count for the code.
	TokenEstimate int `json:"token_estimate,omitempty"`
}

// MinimalContext contains the minimum code needed to understand a function.
type MinimalContext struct {
	// Target is the function being analyzed.
	Target CodeBlock `json:"target"`

	// Types contains required type definitions.
	Types []CodeBlock `json:"types"`

	// Interfaces contains implemented interfaces.
	Interfaces []CodeBlock `json:"interfaces"`

	// KeyCallees contains must-understand dependencies.
	KeyCallees []CodeBlock `json:"key_callees"`

	// TotalTokens is the total estimated token count.
	TotalTokens int `json:"total_tokens"`
}

// APISymbol represents a public API symbol.
type APISymbol struct {
	// Name is the symbol name.
	Name string `json:"name"`

	// Signature is the symbol signature.
	Signature string `json:"signature"`

	// DocString is the documentation comment.
	DocString string `json:"doc_string,omitempty"`
}

// PackageAPI represents the public API of a package.
type PackageAPI struct {
	// Package is the package/module path.
	Package string `json:"package"`

	// Types contains exported types.
	Types []APISymbol `json:"types"`

	// Functions contains exported functions.
	Functions []APISymbol `json:"functions"`

	// Constants contains exported constants.
	Constants []APISymbol `json:"constants"`

	// Variables contains exported variables.
	Variables []APISymbol `json:"variables"`
}

// TypeBrief provides a brief summary of a type.
type TypeBrief struct {
	// Name is the type name.
	Name string `json:"name"`

	// Kind is the type kind (struct, interface, alias).
	Kind string `json:"kind"`

	// Fields is the number of fields (for structs).
	Fields int `json:"fields"`

	// Methods lists method names.
	Methods []string `json:"methods"`
}

// FuncBrief provides a brief summary of a function.
type FuncBrief struct {
	// Name is the function name.
	Name string `json:"name"`

	// Signature is the function signature.
	Signature string `json:"signature"`

	// Exported indicates if the function is exported.
	Exported bool `json:"exported"`
}

// FileSummary provides a structured summary of a file.
type FileSummary struct {
	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// Package is the package/module name.
	Package string `json:"package"`

	// Imports lists import paths.
	Imports []string `json:"imports"`

	// Types contains type summaries.
	Types []TypeBrief `json:"types"`

	// Functions contains function summaries.
	Functions []FuncBrief `json:"functions"`

	// LineCount is the total number of lines.
	LineCount int `json:"line_count"`

	// Purpose is an inferred description of the file's purpose.
	Purpose string `json:"purpose,omitempty"`
}

// SimilarResult represents a single similar code match.
type SimilarResult struct {
	// ID is the symbol ID.
	ID string `json:"id"`

	// Similarity is the similarity score (0.0-1.0).
	Similarity float64 `json:"similarity"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// Code is the code content.
	Code string `json:"code,omitempty"`

	// Why explains the similarity.
	Why string `json:"why,omitempty"`

	// MatchedTraits lists what aspects are similar.
	MatchedTraits []string `json:"matched_traits,omitempty"`
}

// SimilarCode contains the result of a similarity search.
type SimilarCode struct {
	// Query is the target symbol that was searched for.
	Query string `json:"query"`

	// Results contains similar code matches.
	Results []SimilarResult `json:"results"`

	// Method indicates the similarity method used ("structural" or "semantic").
	Method string `json:"method"`
}

// ASTFingerprint enables O(n log n) structural similarity via LSH.
type ASTFingerprint struct {
	// SymbolID is the unique symbol identifier.
	SymbolID string `json:"symbol_id"`

	// NodeTypes is the sequence of AST node types.
	NodeTypes []string `json:"node_types"`

	// ControlFlow is the abstracted control flow pattern.
	ControlFlow string `json:"control_flow"`

	// CallPattern lists called function signatures (abstracted).
	CallPattern []string `json:"call_pattern"`

	// ParamCount is the number of parameters.
	ParamCount int `json:"param_count"`

	// ReturnCount is the number of return values.
	ReturnCount int `json:"return_count"`

	// Complexity is the cyclomatic complexity.
	Complexity int `json:"complexity"`

	// MinHash is the locality-sensitive hash for O(1) similarity lookup.
	MinHash []uint64 `json:"min_hash,omitempty"`
}

// ConfigUse represents a usage of a configuration value.
type ConfigUse struct {
	// Function is the function name where config is used.
	Function string `json:"function"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// Line is the 1-indexed line number.
	Line int `json:"line"`

	// Context describes how the config is used.
	Context string `json:"context"`
}

// ConfigUsage represents usage of a configuration key.
type ConfigUsage struct {
	// ConfigKey is the configuration key or pattern.
	ConfigKey string `json:"config_key"`

	// UsedIn contains all usage locations.
	UsedIn []ConfigUse `json:"used_in"`

	// DefaultVal is the default value if found.
	DefaultVal string `json:"default_value,omitempty"`
}

// ExploreOptions configures exploration tool behavior.
type ExploreOptions struct {
	// MaxNodes is the maximum nodes to visit during traversal.
	MaxNodes int

	// MaxHops is the maximum depth for data flow tracing.
	MaxHops int

	// TokenBudget is the maximum token budget for context building.
	TokenBudget int

	// IncludeCode indicates whether to include source code in results.
	IncludeCode bool

	// UseCache indicates whether to use the exploration cache.
	UseCache bool
}

// DefaultExploreOptions returns sensible defaults.
func DefaultExploreOptions() ExploreOptions {
	return ExploreOptions{
		MaxNodes:    1000,
		MaxHops:     5,
		TokenBudget: 4000,
		IncludeCode: true,
		UseCache:    true,
	}
}

// ExploreOption is a functional option for configuring exploration.
type ExploreOption func(*ExploreOptions)

// WithMaxNodes sets the maximum nodes to visit.
func WithMaxNodes(n int) ExploreOption {
	return func(o *ExploreOptions) {
		o.MaxNodes = n
	}
}

// WithMaxHops sets the maximum traversal depth.
func WithMaxHops(h int) ExploreOption {
	return func(o *ExploreOptions) {
		o.MaxHops = h
	}
}

// WithTokenBudget sets the token budget for context building.
func WithTokenBudget(t int) ExploreOption {
	return func(o *ExploreOptions) {
		o.TokenBudget = t
	}
}

// WithIncludeCode sets whether to include source code.
func WithIncludeCode(include bool) ExploreOption {
	return func(o *ExploreOptions) {
		o.IncludeCode = include
	}
}

// WithUseCache sets whether to use caching.
func WithUseCache(use bool) ExploreOption {
	return func(o *ExploreOptions) {
		o.UseCache = use
	}
}

// applyOptions applies functional options and returns the configured options.
func applyOptions(opts []ExploreOption) ExploreOptions {
	options := DefaultExploreOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
