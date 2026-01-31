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
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
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

// =============================================================================
// Agent API Types (CB-11 Agent Loop)
// =============================================================================

// AgentRunRequest is the request body for POST /v1/codebuddy/agent/run.
type AgentRunRequest struct {
	// ProjectRoot is the absolute path to the project root directory.
	// Required.
	ProjectRoot string `json:"project_root" binding:"required"`

	// Query is the user's question or task description. Required.
	Query string `json:"query" binding:"required"`

	// Config is optional session configuration overrides.
	Config *agent.SessionConfig `json:"config,omitempty"`
}

// AgentRunResponse is the response for POST /v1/codebuddy/agent/run.
type AgentRunResponse struct {
	// SessionID is the unique identifier for this session.
	SessionID string `json:"session_id"`

	// State is the current session state (IDLE, INIT, PLAN, EXECUTE, etc.).
	State string `json:"state"`

	// StepsTaken is the number of agent steps completed.
	StepsTaken int `json:"steps_taken"`

	// TokensUsed is the total tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// Response is the agent's final response (if complete).
	Response string `json:"response,omitempty"`

	// NeedsClarify contains clarification details if state is CLARIFY.
	NeedsClarify *agent.ClarifyRequest `json:"needs_clarify,omitempty"`

	// Error is the error message if state is ERROR.
	Error string `json:"error,omitempty"`

	// DegradedMode indicates if the session is running with limited capabilities.
	DegradedMode bool `json:"degraded_mode"`
}

// AgentContinueRequest is the request body for POST /v1/codebuddy/agent/continue.
type AgentContinueRequest struct {
	// SessionID is the session to continue. Required.
	SessionID string `json:"session_id" binding:"required"`

	// Clarification is the user's response to the clarification request. Required.
	Clarification string `json:"clarification" binding:"required"`
}

// AgentAbortRequest is the request body for POST /v1/codebuddy/agent/abort.
type AgentAbortRequest struct {
	// SessionID is the session to abort. Required.
	SessionID string `json:"session_id" binding:"required"`
}

// AgentStateResponse is the response for GET /v1/codebuddy/agent/:id.
type AgentStateResponse struct {
	// SessionID is the unique session identifier.
	SessionID string `json:"session_id"`

	// ProjectRoot is the project root path.
	ProjectRoot string `json:"project_root"`

	// GraphID is the code graph ID (if initialized).
	GraphID string `json:"graph_id,omitempty"`

	// State is the current session state.
	State string `json:"state"`

	// StepCount is the number of steps completed.
	StepCount int `json:"step_count"`

	// TokensUsed is the total tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// CreatedAt is the Unix timestamp of session creation.
	CreatedAt int64 `json:"created_at"`

	// LastActiveAt is the Unix timestamp of last activity.
	LastActiveAt int64 `json:"last_active_at"`

	// DegradedMode indicates if running with limited capabilities.
	DegradedMode bool `json:"degraded_mode"`
}

// =============================================================================
// AGENTIC TOOL TYPES (CB-20/21/22/23 Tool Endpoints)
// =============================================================================
// Request and response types for the agentic reasoning layer tools.
// These wrap the internal package types for HTTP API consumption.

// ToolsResponse is the response for GET /v1/codebuddy/tools.
type ToolsResponse struct {
	Tools []ToolDefinition `json:"tools"`
}

// --- Exploration Tool Types ---

// FindEntryPointsRequest is the request for POST /v1/codebuddy/explore/entry_points.
type FindEntryPointsRequest struct {
	GraphID      string `json:"graph_id" binding:"required"`
	Type         string `json:"type"`
	Package      string `json:"package"`
	Limit        int    `json:"limit"`
	IncludeTests bool   `json:"include_tests"`
}

// TraceDataFlowRequest is the request for POST /v1/codebuddy/explore/data_flow.
type TraceDataFlowRequest struct {
	GraphID     string `json:"graph_id" binding:"required"`
	SourceID    string `json:"source_id" binding:"required"`
	MaxHops     int    `json:"max_hops"`
	IncludeCode bool   `json:"include_code"`
}

// TraceErrorFlowRequest is the request for POST /v1/codebuddy/explore/error_flow.
type TraceErrorFlowRequest struct {
	GraphID string `json:"graph_id" binding:"required"`
	Scope   string `json:"scope" binding:"required"`
	MaxHops int    `json:"max_hops"`
}

// FindConfigUsageRequest is the request for POST /v1/codebuddy/explore/config_usage.
type FindConfigUsageRequest struct {
	GraphID         string `json:"graph_id" binding:"required"`
	ConfigKey       string `json:"config_key" binding:"required"`
	IncludeDefaults bool   `json:"include_defaults"`
}

// FindSimilarCodeRequest is the request for POST /v1/codebuddy/explore/similar_code.
type FindSimilarCodeRequest struct {
	GraphID       string  `json:"graph_id" binding:"required"`
	SymbolID      string  `json:"symbol_id" binding:"required"`
	MinSimilarity float64 `json:"min_similarity"`
	Limit         int     `json:"limit"`
}

// BuildMinimalContextRequest is the request for POST /v1/codebuddy/explore/minimal_context.
type BuildMinimalContextRequest struct {
	GraphID        string `json:"graph_id" binding:"required"`
	SymbolID       string `json:"symbol_id" binding:"required"`
	TokenBudget    int    `json:"token_budget"`
	IncludeCallees bool   `json:"include_callees"`
}

// SummarizeFileRequest is the request for POST /v1/codebuddy/explore/summarize_file.
type SummarizeFileRequest struct {
	GraphID  string `json:"graph_id" binding:"required"`
	FilePath string `json:"file_path" binding:"required"`
}

// SummarizePackageRequest is the request for POST /v1/codebuddy/explore/summarize_package.
type SummarizePackageRequest struct {
	GraphID string `json:"graph_id" binding:"required"`
	Package string `json:"package" binding:"required"`
}

// AnalyzeChangeImpactRequest is the request for POST /v1/codebuddy/explore/change_impact.
type AnalyzeChangeImpactRequest struct {
	GraphID    string `json:"graph_id" binding:"required"`
	SymbolID   string `json:"symbol_id" binding:"required"`
	ChangeType string `json:"change_type"`
}

// --- Reasoning Tool Types ---

// CheckBreakingChangesRequest is the request for POST /v1/codebuddy/reason/breaking_changes.
type CheckBreakingChangesRequest struct {
	GraphID           string `json:"graph_id" binding:"required"`
	SymbolID          string `json:"symbol_id" binding:"required"`
	ProposedSignature string `json:"proposed_signature" binding:"required"`
}

// SimulateChangeRequest is the request for POST /v1/codebuddy/reason/simulate_change.
type SimulateChangeRequest struct {
	GraphID       string                 `json:"graph_id" binding:"required"`
	SymbolID      string                 `json:"symbol_id" binding:"required"`
	ChangeType    string                 `json:"change_type" binding:"required"`
	ChangeDetails map[string]interface{} `json:"change_details" binding:"required"`
}

// ValidateChangeRequest is the request for POST /v1/codebuddy/reason/validate_change.
type ValidateChangeRequest struct {
	Code     string `json:"code" binding:"required"`
	Language string `json:"language" binding:"required"`
}

// FindTestCoverageRequest is the request for POST /v1/codebuddy/reason/test_coverage.
type FindTestCoverageRequest struct {
	GraphID         string `json:"graph_id" binding:"required"`
	SymbolID        string `json:"symbol_id" binding:"required"`
	IncludeIndirect bool   `json:"include_indirect"`
}

// DetectSideEffectsRequest is the request for POST /v1/codebuddy/reason/side_effects.
type DetectSideEffectsRequest struct {
	GraphID    string `json:"graph_id" binding:"required"`
	SymbolID   string `json:"symbol_id" binding:"required"`
	Transitive bool   `json:"transitive"`
}

// SuggestRefactorRequest is the request for POST /v1/codebuddy/reason/suggest_refactor.
type SuggestRefactorRequest struct {
	GraphID  string `json:"graph_id" binding:"required"`
	SymbolID string `json:"symbol_id" binding:"required"`
}

// --- Coordination Tool Types ---

// PlanMultiFileChangeRequest is the request for POST /v1/codebuddy/coordinate/plan_changes.
type PlanMultiFileChangeRequest struct {
	GraphID      string `json:"graph_id" binding:"required"`
	TargetID     string `json:"target_id" binding:"required"`
	ChangeType   string `json:"change_type" binding:"required"`
	NewSignature string `json:"new_signature"`
	NewName      string `json:"new_name"`
	Description  string `json:"description"`
	IncludeTests bool   `json:"include_tests"`
}

// ValidatePlanRequest is the request for POST /v1/codebuddy/coordinate/validate_plan.
type ValidatePlanRequest struct {
	PlanID string `json:"plan_id" binding:"required"`
}

// PreviewChangesRequest is the request for POST /v1/codebuddy/coordinate/preview_changes.
type PreviewChangesRequest struct {
	PlanID       string `json:"plan_id" binding:"required"`
	ContextLines int    `json:"context_lines"`
}

// --- Pattern Tool Types ---

// DetectPatternsRequest is the request for POST /v1/codebuddy/patterns/detect.
type DetectPatternsRequest struct {
	GraphID       string   `json:"graph_id" binding:"required"`
	Scope         string   `json:"scope"`
	Patterns      []string `json:"patterns"`
	MinConfidence float64  `json:"min_confidence"`
}

// FindCodeSmellsRequest is the request for POST /v1/codebuddy/patterns/code_smells.
type FindCodeSmellsRequest struct {
	GraphID      string `json:"graph_id" binding:"required"`
	Scope        string `json:"scope"`
	MinSeverity  string `json:"min_severity"`
	IncludeTests bool   `json:"include_tests"`
}

// FindDuplicationRequest is the request for POST /v1/codebuddy/patterns/duplication.
type FindDuplicationRequest struct {
	GraphID       string  `json:"graph_id" binding:"required"`
	Scope         string  `json:"scope"`
	MinSimilarity float64 `json:"min_similarity"`
	Type          string  `json:"type"`
	IncludeTests  bool    `json:"include_tests"`
}

// FindCircularDepsRequest is the request for POST /v1/codebuddy/patterns/circular_deps.
type FindCircularDepsRequest struct {
	GraphID string `json:"graph_id" binding:"required"`
	Scope   string `json:"scope"`
	Level   string `json:"level"`
}

// ExtractConventionsRequest is the request for POST /v1/codebuddy/patterns/conventions.
type ExtractConventionsRequest struct {
	GraphID      string   `json:"graph_id" binding:"required"`
	Scope        string   `json:"scope"`
	Types        []string `json:"types"`
	IncludeTests bool     `json:"include_tests"`
}

// FindDeadCodeRequest is the request for POST /v1/codebuddy/patterns/dead_code.
type FindDeadCodeRequest struct {
	GraphID         string `json:"graph_id" binding:"required"`
	Scope           string `json:"scope"`
	IncludeExported bool   `json:"include_exported"`
}

// --- Common Response Wrapper ---

// AgenticResponse wraps all agentic tool responses with latency tracking.
type AgenticResponse struct {
	// Result contains the actual response data.
	Result interface{} `json:"result"`

	// LatencyMs is the time taken to process the request in milliseconds.
	LatencyMs int64 `json:"latency_ms"`

	// Warnings contains non-fatal warnings if any.
	Warnings []string `json:"warnings,omitempty"`

	// Limitations documents what couldn't be analyzed.
	Limitations []string `json:"limitations,omitempty"`
}

// =============================================================================
// LSP INTEGRATION TYPES (CB-24)
// =============================================================================

// LSPDefinitionRequest is the request for POST /v1/codebuddy/lsp/definition.
type LSPDefinitionRequest struct {
	// GraphID is the graph to use for project context. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// FilePath is the absolute path to the file. Required.
	FilePath string `json:"file_path" binding:"required"`

	// Line is the 1-indexed line number. Required.
	Line int `json:"line" binding:"required,min=1"`

	// Column is the 0-indexed column number. Required.
	Column int `json:"column" binding:"required,min=0"`
}

// LSPReferencesRequest is the request for POST /v1/codebuddy/lsp/references.
type LSPReferencesRequest struct {
	// GraphID is the graph to use for project context. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// FilePath is the absolute path to the file. Required.
	FilePath string `json:"file_path" binding:"required"`

	// Line is the 1-indexed line number. Required.
	Line int `json:"line" binding:"required,min=1"`

	// Column is the 0-indexed column number. Required.
	Column int `json:"column" binding:"required,min=0"`

	// IncludeDeclaration includes the declaration in results.
	IncludeDeclaration bool `json:"include_declaration"`
}

// LSPHoverRequest is the request for POST /v1/codebuddy/lsp/hover.
type LSPHoverRequest struct {
	// GraphID is the graph to use for project context. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// FilePath is the absolute path to the file. Required.
	FilePath string `json:"file_path" binding:"required"`

	// Line is the 1-indexed line number. Required.
	Line int `json:"line" binding:"required,min=1"`

	// Column is the 0-indexed column number. Required.
	Column int `json:"column" binding:"required,min=0"`
}

// LSPRenameRequest is the request for POST /v1/codebuddy/lsp/rename.
type LSPRenameRequest struct {
	// GraphID is the graph to use for project context. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// FilePath is the absolute path to the file. Required.
	FilePath string `json:"file_path" binding:"required"`

	// Line is the 1-indexed line number. Required.
	Line int `json:"line" binding:"required,min=1"`

	// Column is the 0-indexed column number. Required.
	Column int `json:"column" binding:"required,min=0"`

	// NewName is the new name for the symbol. Required.
	NewName string `json:"new_name" binding:"required"`
}

// LSPWorkspaceSymbolRequest is the request for POST /v1/codebuddy/lsp/symbols.
type LSPWorkspaceSymbolRequest struct {
	// GraphID is the graph to use for project context. Required.
	GraphID string `json:"graph_id" binding:"required"`

	// Query is the symbol search query. Required.
	Query string `json:"query" binding:"required"`

	// Language is the language to search (defaults to project primary language).
	Language string `json:"language"`
}

// LSPLocation represents a location in a document.
type LSPLocation struct {
	// FilePath is the absolute path to the file.
	FilePath string `json:"file_path"`

	// StartLine is the 1-indexed start line.
	StartLine int `json:"start_line"`

	// StartColumn is the 0-indexed start column.
	StartColumn int `json:"start_column"`

	// EndLine is the 1-indexed end line.
	EndLine int `json:"end_line"`

	// EndColumn is the 0-indexed end column.
	EndColumn int `json:"end_column"`
}

// LSPDefinitionResponse is the response for POST /v1/codebuddy/lsp/definition.
type LSPDefinitionResponse struct {
	// Locations contains the definition location(s).
	Locations []LSPLocation `json:"locations"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// LSPReferencesResponse is the response for POST /v1/codebuddy/lsp/references.
type LSPReferencesResponse struct {
	// Locations contains the reference location(s).
	Locations []LSPLocation `json:"locations"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// LSPHoverResponse is the response for POST /v1/codebuddy/lsp/hover.
type LSPHoverResponse struct {
	// Content is the hover content (documentation, type info).
	Content string `json:"content"`

	// Kind is the content format ("plaintext" or "markdown").
	Kind string `json:"kind"`

	// Range is the range this hover applies to (optional).
	Range *LSPLocation `json:"range,omitempty"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// LSPRenameResponse is the response for POST /v1/codebuddy/lsp/rename.
type LSPRenameResponse struct {
	// Edits is a map from file path to list of text edits.
	Edits map[string][]LSPTextEdit `json:"edits"`

	// FileCount is the number of files affected.
	FileCount int `json:"file_count"`

	// EditCount is the total number of edits.
	EditCount int `json:"edit_count"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// LSPTextEdit represents a text edit.
type LSPTextEdit struct {
	// Range is the range to replace.
	Range LSPLocation `json:"range"`

	// NewText is the replacement text.
	NewText string `json:"new_text"`
}

// LSPSymbolInfo represents information about a workspace symbol.
type LSPSymbolInfo struct {
	// Name is the symbol name.
	Name string `json:"name"`

	// Kind is the symbol kind (function, class, etc.).
	Kind string `json:"kind"`

	// Location is where the symbol is defined.
	Location LSPLocation `json:"location"`

	// ContainerName is the name of the containing symbol.
	ContainerName string `json:"container_name,omitempty"`
}

// LSPWorkspaceSymbolResponse is the response for POST /v1/codebuddy/lsp/symbols.
type LSPWorkspaceSymbolResponse struct {
	// Symbols contains the matching symbols.
	Symbols []LSPSymbolInfo `json:"symbols"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// LSPStatusResponse is the response for GET /v1/codebuddy/lsp/status.
type LSPStatusResponse struct {
	// Available indicates if LSP is available for the project.
	Available bool `json:"available"`

	// RunningServers lists languages with running servers.
	RunningServers []string `json:"running_servers"`

	// SupportedLanguages lists all supported languages.
	SupportedLanguages []string `json:"supported_languages"`
}
