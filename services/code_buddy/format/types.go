// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package format

import "io"

// FormatType represents the type of output format.
type FormatType string

const (
	// FormatJSON is full JSON output (default).
	FormatJSON FormatType = "json"

	// FormatOutline is tree-style text output.
	FormatOutline FormatType = "outline"

	// FormatCompact is minimal token JSON output.
	FormatCompact FormatType = "compact"

	// FormatMermaid is graph diagram output.
	FormatMermaid FormatType = "mermaid"

	// FormatMarkdown is table/list output.
	FormatMarkdown FormatType = "markdown"
)

// FormatVersion is the current version of the format specification.
const FormatVersion = "1"

// Formatter formats query results into different output representations.
type Formatter interface {
	// Format converts the result to a formatted string.
	Format(result interface{}) (string, error)

	// Name returns the format name.
	Name() FormatType

	// TokenEstimate estimates the number of tokens in the output.
	// tokenizer can be "gpt4", "claude", "llama", or empty for default.
	TokenEstimate(result interface{}, tokenizer ...string) int

	// IsReversible returns whether the output can be parsed back to original.
	IsReversible() bool

	// SupportsStreaming returns whether the format supports streaming output.
	SupportsStreaming() bool

	// FormatStreaming writes formatted output to a writer (if supported).
	FormatStreaming(result interface{}, w io.Writer) error
}

// FormatMetadata contains metadata about the formatted output.
type FormatMetadata struct {
	// Type is the format type.
	Type FormatType `json:"type"`

	// Version is the format specification version.
	Version string `json:"version"`

	// Reversible indicates if the output can be parsed back.
	Reversible bool `json:"reversible"`

	// Note is additional information about the format.
	Note string `json:"note,omitempty"`
}

// FormatCapability defines the limits of a format.
type FormatCapability struct {
	// MaxNodes is the maximum number of nodes for visual formats.
	MaxNodes int

	// MaxRows is the maximum number of rows for table formats.
	MaxRows int

	// MaxTokens is the maximum estimated tokens.
	MaxTokens int

	// SupportsStreaming indicates if the format supports streaming.
	SupportsStreaming bool
}

// TokenRatio maps tokenizer names to characters per token.
var TokenRatio = map[string]float64{
	"gpt4":    4.0,
	"claude":  3.5,
	"llama":   4.5,
	"default": 4.0,
}

// GraphResult represents a graph query result for formatting.
type GraphResult struct {
	// Query is the original query.
	Query string `json:"query"`

	// FocusNodes are the main nodes of interest.
	FocusNodes []Node `json:"focus_nodes"`

	// Graph contains the full graph data.
	Graph GraphStats `json:"graph"`

	// KeyNodes are important nodes with annotations.
	KeyNodes []KeyNode `json:"key_nodes"`

	// Risk is the risk assessment.
	Risk RiskAssessment `json:"risk,omitempty"`
}

// Node represents a code symbol node.
type Node struct {
	// ID is the unique node identifier.
	ID string `json:"id"`

	// Name is the display name.
	Name string `json:"name"`

	// Location is the file:line location.
	Location string `json:"location"`

	// Type is the symbol type (function, type, etc.).
	Type string `json:"type"`

	// Package is the package name.
	Package string `json:"package,omitempty"`
}

// GraphStats contains graph statistics.
type GraphStats struct {
	// NodeCount is the total number of nodes.
	NodeCount int `json:"node_count"`

	// EdgeCount is the total number of edges.
	EdgeCount int `json:"edge_count"`

	// Depth is the maximum graph depth.
	Depth int `json:"depth"`
}

// KeyNode represents an important node with additional info.
type KeyNode struct {
	// Node is the base node information.
	Node

	// Callers is the number of callers.
	Callers int `json:"callers"`

	// Callees is the number of callees.
	Callees int `json:"callees"`

	// Flag indicates special status (e.g., "shared", "entry").
	Flag string `json:"flag,omitempty"`

	// Warning is a warning message about this node.
	Warning string `json:"warning,omitempty"`

	// Connections are connected nodes.
	Connections []Connection `json:"connections,omitempty"`
}

// Connection represents a connection between nodes.
type Connection struct {
	// TargetID is the target node ID.
	TargetID string `json:"target_id"`

	// Type is the connection type (calls, called_by, implements, etc.).
	Type string `json:"type"`
}

// RiskAssessment contains risk information.
type RiskAssessment struct {
	// Level is the risk level (low, medium, high).
	Level string `json:"level"`

	// DirectImpact is the number of directly affected files.
	DirectImpact int `json:"direct_impact"`

	// IndirectImpact is the number of indirectly affected files.
	IndirectImpact int `json:"indirect_impact"`

	// Warnings are specific risk warnings.
	Warnings []string `json:"warnings,omitempty"`
}

// ResultStats analyzes a result for capability checking.
type ResultStats struct {
	// NodeCount is the number of nodes.
	NodeCount int

	// RowCount is the number of data rows.
	RowCount int

	// EstimatedTokens is the estimated token count.
	EstimatedTokens int
}

// AnalyzeResult extracts stats from a result for capability checking.
func AnalyzeResult(result interface{}) ResultStats {
	stats := ResultStats{}

	switch r := result.(type) {
	case *GraphResult:
		stats.NodeCount = r.Graph.NodeCount
		stats.RowCount = len(r.KeyNodes)
	case GraphResult:
		stats.NodeCount = r.Graph.NodeCount
		stats.RowCount = len(r.KeyNodes)
	}

	return stats
}
