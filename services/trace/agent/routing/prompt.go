// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"bytes"
	"strings"
	"text/template"
)

// =============================================================================
// Prompt Builder
// =============================================================================

// PromptBuilder constructs dynamic system prompts for tool routing.
//
// # Description
//
// Builds prompts that include available tools, their descriptions, and
// context about the codebase. The prompt instructs the router to output
// a JSON object with the selected tool and confidence score.
//
// # Thread Safety
//
// PromptBuilder is safe for concurrent use.
type PromptBuilder struct {
	tmpl *template.Template
}

// PromptData contains the data for prompt template rendering.
type PromptData struct {
	// Tools is the list of available tools.
	Tools []ToolSpec

	// Context contains optional codebase context.
	Context *CodeContext

	// Query is the user's question/request.
	Query string
}

// systemPromptTemplate is the template for the routing system prompt.
const systemPromptTemplate = `You are a tool router for a code assistant. Your job is to select the SINGLE BEST tool for the user's query.

## Available Tools
{{range .Tools}}
### {{.Name}}
{{.Description}}
{{- if .BestFor}}
Best for: {{join .BestFor ", "}}
{{- end}}
{{- if .Params}}
Parameters: {{join .Params ", "}}
{{- end}}
{{- if .Category}}
Category: {{.Category}}
{{- end}}

{{end}}

## Current Context
{{- if .Context}}
{{- if .Context.Language}}
- Language: {{.Context.Language}}
{{- end}}
{{- if gt .Context.Files 0}}
- Files indexed: {{.Context.Files}}
{{- end}}
{{- if gt .Context.Symbols 0}}
- Symbols available: {{.Context.Symbols}}
{{- end}}
{{- if .Context.CurrentFile}}
- Current file: {{.Context.CurrentFile}}
{{- end}}
{{- if .Context.RecentTools}}
- Recent tools used: {{join .Context.RecentTools ", "}}
{{- end}}
{{- if .Context.PreviousErrors}}

## IMPORTANT: Failed Tools
The following tools recently failed. DO NOT suggest them again unless you can solve the error:
{{range .Context.PreviousErrors}}
- {{.Tool}}: {{.Error}}
{{- end}}
Choose a DIFFERENT tool that can accomplish the same goal.
{{- end}}
{{- else}}
- No additional context provided
{{- end}}

## Instructions
1. Analyze the user's query
2. Select the SINGLE most appropriate tool
3. Be decisive - pick one tool even if multiple might work
4. Consider the context when making your selection
5. AVOID tools that recently failed (see Failed Tools section if present)

## Output Format
Respond with ONLY a JSON object. No explanation, no markdown, just JSON:
{"tool": "<tool_name>", "confidence": <0.0-1.0>, "reasoning": "<brief explanation>"}

Example outputs:
{"tool": "find_symbol_usages", "confidence": 0.95, "reasoning": "Query asks about function callers"}
{"tool": "read_file", "confidence": 0.8, "reasoning": "User wants to see file contents"}
{"tool": "grep_codebase", "confidence": 0.7, "reasoning": "Searching for text pattern"}`

// userPromptTemplate is the template for the user message.
const userPromptTemplate = `User query: {{.Query}}

Select the best tool and respond with JSON only.`

// NewPromptBuilder creates a new PromptBuilder.
//
// # Outputs
//
//   - *PromptBuilder: Configured builder.
//   - error: Non-nil if template parsing fails.
func NewPromptBuilder() (*PromptBuilder, error) {
	funcMap := template.FuncMap{
		"join": strings.Join,
	}

	tmpl, err := template.New("system").Funcs(funcMap).Parse(systemPromptTemplate)
	if err != nil {
		return nil, err
	}

	return &PromptBuilder{
		tmpl: tmpl,
	}, nil
}

// BuildSystemPrompt generates the system prompt for the router.
//
// # Description
//
// Creates a system prompt that includes all available tools, their
// descriptions, and instructions for JSON output format.
//
// # Inputs
//
//   - tools: Available tools for selection.
//   - context: Optional codebase context.
//
// # Outputs
//
//   - string: The rendered system prompt.
//   - error: Non-nil if template rendering fails.
func (p *PromptBuilder) BuildSystemPrompt(tools []ToolSpec, context *CodeContext) (string, error) {
	data := PromptData{
		Tools:   tools,
		Context: context,
	}

	var buf bytes.Buffer
	if err := p.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// BuildUserPrompt generates the user message containing the query.
//
// # Inputs
//
//   - query: The user's question/request.
//
// # Outputs
//
//   - string: The rendered user prompt.
func (p *PromptBuilder) BuildUserPrompt(query string) string {
	return "User query: " + query + "\n\nSelect the best tool and respond with JSON only."
}

// =============================================================================
// Default Tool Specs
// =============================================================================

// DefaultToolSpecs returns the standard Code Buddy tool specifications.
//
// # Description
//
// Returns ToolSpec entries for the core Code Buddy tools. These can be
// filtered based on what's actually available in the current session.
//
// # Outputs
//
//   - []ToolSpec: Standard tool specifications.
func DefaultToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "find_symbol",
			Description: "Find a symbol (function, type, variable) by name in the codebase.",
			BestFor:     []string{"finding where something is defined", "locating a function", "finding a type"},
			Params:      []string{"name", "kind"},
			Category:    "search",
		},
		{
			Name:        "find_symbol_usages",
			Description: "Find all places where a symbol is used/called.",
			BestFor:     []string{"finding callers", "finding references", "impact analysis"},
			Params:      []string{"symbol_id"},
			Category:    "search",
		},
		{
			Name:        "find_config_usage",
			Description: "Find configuration settings and environment variables.",
			BestFor:     []string{"finding config options", "environment variables", "settings"},
			Params:      []string{"pattern"},
			Category:    "search",
		},
		{
			Name:        "read_file",
			Description: "Read the contents of a specific file.",
			BestFor:     []string{"viewing file contents", "reading code", "examining implementation"},
			Params:      []string{"path", "start_line", "end_line"},
			Category:    "read",
		},
		{
			Name:        "read_symbol",
			Description: "Read the source code of a specific symbol.",
			BestFor:     []string{"viewing function code", "reading type definition", "examining symbol"},
			Params:      []string{"symbol_id"},
			Category:    "read",
		},
		{
			Name:        "grep_codebase",
			Description: "Search for text patterns across the codebase.",
			BestFor:     []string{"finding text", "searching strings", "regex search"},
			Params:      []string{"pattern", "file_pattern"},
			Category:    "search",
		},
		{
			Name:        "list_files",
			Description: "List files in a directory or matching a pattern.",
			BestFor:     []string{"exploring directory structure", "finding files", "listing contents"},
			Params:      []string{"path", "pattern"},
			Category:    "explore",
		},
		{
			Name:        "get_symbol_graph",
			Description: "Get the dependency graph for a symbol.",
			BestFor:     []string{"understanding dependencies", "call graph", "type hierarchy"},
			Params:      []string{"symbol_id", "depth"},
			Category:    "analyze",
		},
		{
			Name:        "answer",
			Description: "Provide a direct answer when no tool is needed.",
			BestFor:     []string{"general questions", "explanations", "no code lookup needed"},
			Params:      []string{},
			Category:    "respond",
		},
	}
}
