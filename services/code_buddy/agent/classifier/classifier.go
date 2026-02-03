// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package classifier provides query classification for tool forcing decisions.
//
// The classifier determines whether a user query requires tool exploration
// (analytical query) versus simple conversation (non-analytical query).
// This enables the agent to force tool usage for questions that require
// codebase exploration.
package classifier

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ToolSuggestion contains a tool recommendation with specific search instructions.
//
// This enables targeted exploration by providing not just which tool to use,
// but HOW to use it with specific parameters and search patterns.
type ToolSuggestion struct {
	// ToolName is the suggested tool to call.
	ToolName string

	// SearchHint provides specific search instructions for the LLM.
	// This tells the LLM WHAT to search for, not just which tool to use.
	// Example: "Call find_entry_points with type='test' to find test functions"
	SearchHint string

	// SearchPatterns lists specific patterns the LLM should search for.
	// These are concrete search terms relevant to the question.
	// Example: ["*_test.go", "func Test", "t.Run"]
	SearchPatterns []string
}

// QueryClassifier classifies user queries for tool forcing decisions.
//
// Thread Safety: Implementations must be safe for concurrent use.
type QueryClassifier interface {
	// IsAnalytical determines if a query requires tool exploration.
	//
	// Description:
	//   Analyzes the query text to determine if it's asking about code
	//   structure, behavior, or properties that require tool exploration
	//   to answer accurately.
	//
	// Inputs:
	//   ctx - Context for tracing and cancellation. Must not be nil.
	//   query - The user's question text. Empty queries return false.
	//
	// Outputs:
	//   bool - True if the query requires tool exploration.
	//
	// Example:
	//   isAnalytical := classifier.IsAnalytical(ctx, "What tests exist?")
	//   // isAnalytical == true
	//
	// Thread Safety: This method is safe for concurrent use.
	IsAnalytical(ctx context.Context, query string) bool

	// SuggestTool recommends a starting tool based on query type.
	//
	// Description:
	//   Analyzes the query to suggest an appropriate first tool,
	//   validating that the suggested tool exists in the available set.
	//
	// Inputs:
	//   ctx - Context for tracing and cancellation. Must not be nil.
	//   query - The user's question text.
	//   available - List of available tool names to choose from.
	//
	// Outputs:
	//   string - The suggested tool name, or empty if no suggestion.
	//   bool - True if a valid suggestion was found.
	//
	// Example:
	//   tool, ok := classifier.SuggestTool(ctx, "What tests exist?",
	//       []string{"find_entry_points", "trace_data_flow"})
	//   // tool == "find_entry_points", ok == true
	//
	// Thread Safety: This method is safe for concurrent use.
	SuggestTool(ctx context.Context, query string, available []string) (string, bool)

	// SuggestToolWithHint recommends a tool with specific search instructions.
	//
	// Description:
	//   Enhanced version of SuggestTool that provides targeted search
	//   instructions. This enables Fix 2 (Targeted Exploration) by telling
	//   the LLM not just WHICH tool to use, but WHAT to search for.
	//
	// Inputs:
	//   ctx - Context for tracing and cancellation. Must not be nil.
	//   query - The user's question text.
	//   available - List of available tool names to choose from.
	//
	// Outputs:
	//   *ToolSuggestion - The suggestion with search instructions, or nil.
	//   bool - True if a valid suggestion was found.
	//
	// Example:
	//   suggestion, ok := classifier.SuggestToolWithHint(ctx, "What tests exist?",
	//       []string{"find_entry_points", "trace_data_flow"})
	//   // suggestion.ToolName == "find_entry_points"
	//   // suggestion.SearchHint == "Call find_entry_points with type='test'..."
	//   // suggestion.SearchPatterns == ["*_test.go", "func Test"]
	//
	// Thread Safety: This method is safe for concurrent use.
	SuggestToolWithHint(ctx context.Context, query string, available []string) (*ToolSuggestion, bool)
}

// analyticalPatterns defines word-boundary patterns for analytical queries.
// Patterns are grouped by category for maintainability.
var analyticalPatterns = []string{
	// Structural questions (what exists)
	`\bwhat tests\b`,
	`\bwhat functions\b`,
	`\bwhat packages\b`,
	`\bwhat files\b`,
	`\bwhat classes\b`,
	`\bwhat methods\b`,
	`\bwhat interfaces\b`,
	`\bentry point`,
	`\bmain function\b`,
	`\bhandler\b`,
	`\bproject structure\b`,
	`\bdirectory structure\b`,
	`\bcode organization\b`,

	// Flow questions (how things work)
	`\bhow does\b`,
	`\bhow is\b`,
	`\bhow are\b`,
	`\btrace\b`,
	`\bdata flow\b`,
	`\bcall graph\b`,
	`\bcall chain\b`,
	`\binvoke\b`,
	`\btrigger\b`,

	// Analysis questions (security, quality)
	`\bsecurity\b`,
	`\bvulnerability\b`,
	`\bissue\b`,
	`\bconcern\b`,
	`\berror handling\b`,
	`\bexception\b`,
	`\bpanic\b`,
	`\blogging\b`,
	`\bobservability\b`,
	`\bmetrics\b`,
	`\btracing\b`,
	`\bvalidation\b`,
	`\bsanitize\b`,

	// Exploration questions (find things)
	`\bwhere is\b`,
	`\bwhere are\b`,
	`\bfind\b`,
	`\bshow me\b`,
	`\blist\b`,
	`\benumerate\b`,
	`\bconfiguration\b`,
	`\bconfig\b`,
	`\benv\b`,
}

// toolSuggestionRules maps query patterns to suggested tools.
// Order matters - first match wins.
var toolSuggestionRules = []struct {
	pattern *regexp.Regexp
	tool    string
}{
	{regexp.MustCompile(`(?i)\btest`), "find_entry_points"},
	{regexp.MustCompile(`(?i)\bentry|main\b`), "find_entry_points"},
	{regexp.MustCompile(`(?i)\berror|exception|panic\b`), "trace_error_flow"},
	{regexp.MustCompile(`(?i)\bflow|trace|path\b`), "trace_data_flow"},
	{regexp.MustCompile(`(?i)\bconfig|log|env\b`), "find_config_usage"},
	{regexp.MustCompile(`(?i)\bsecurity|vulnerab`), "check_security"},
}

// targetedSearchRules provides enhanced tool suggestions with specific search instructions.
// These rules enable Fix 2 (Targeted Exploration) from CB-28d-6 by telling the LLM
// not just WHICH tool to use but WHAT patterns to search for.
//
// Order matters - first match wins. More specific patterns should come first.
var targetedSearchRules = []struct {
	pattern        *regexp.Regexp
	tool           string
	searchHint     string
	searchPatterns []string
}{
	// Tests - specific patterns to find test files and functions
	{
		pattern:        regexp.MustCompile(`(?i)\bwhat tests\b|\btests exist\b|\btest files?\b|\btest functions?\b`),
		tool:           "find_entry_points",
		searchHint:     "Call find_entry_points with type='test' to find test files. Look for *_test.go files and functions starting with 'func Test'.",
		searchPatterns: []string{"*_test.go", "func Test", "t.Run(", "t.Parallel()"},
	},
	// Generic test mention (less specific)
	{
		pattern:        regexp.MustCompile(`(?i)\btest\b`),
		tool:           "find_entry_points",
		searchHint:     "Use find_entry_points with type='test' to discover test coverage.",
		searchPatterns: []string{"*_test.go", "func Test"},
	},
	// Entry points and main functions
	{
		pattern:        regexp.MustCompile(`(?i)\bentry\s*point|\bmain\s*function|\bwhere.*start`),
		tool:           "find_entry_points",
		searchHint:     "Call find_entry_points with type='main' to find application entry points. Look for func main() in main packages.",
		searchPatterns: []string{"func main()", "package main", "cmd/"},
	},
	// Logging and observability - very specific patterns
	{
		pattern:        regexp.MustCompile(`(?i)\blogging\b|\blog\s*pattern|\bobservability|\bmetric|\btrac(e|ing)\b`),
		tool:           "find_config_usage",
		searchHint:     "Search for logging patterns using grep. Look for 'log.', 'slog.', 'Logger', 'metrics', 'tracing', 'span' in the codebase.",
		searchPatterns: []string{"log.", "slog.", "Logger", "metrics", "tracing", "span", "otel.", "prometheus"},
	},
	// Configuration and environment
	{
		pattern:        regexp.MustCompile(`(?i)\bconfig(uration)?\b|\bsetting|\benv(ironment)?\s*(var)?|\boption`),
		tool:           "find_config_usage",
		searchHint:     "Search for configuration patterns. Look for 'os.Getenv', 'flag.', config structs, '.env', 'viper', 'yaml' loading.",
		searchPatterns: []string{"os.Getenv", "flag.", "Config{", ".env", "viper.", "yaml.Unmarshal"},
	},
	// Exported functions
	{
		pattern:        regexp.MustCompile(`(?i)\bexport(ed)?\s*(function|func|method)|\bpublic\s*(function|func|method)`),
		tool:           "find_entry_points",
		searchHint:     "Search for exported functions using the pattern '^func [A-Z]'. In Go, exported functions start with a capital letter.",
		searchPatterns: []string{"func [A-Z]", "type [A-Z]"},
	},
	// Error handling
	{
		pattern:        regexp.MustCompile(`(?i)\berror\s*handl|\bexception|\bpanic|\brecover`),
		tool:           "trace_error_flow",
		searchHint:     "Use trace_error_flow to find error handling patterns. Look for 'if err != nil', 'panic(', 'recover()', error wrapping with fmt.Errorf.",
		searchPatterns: []string{"if err != nil", "panic(", "recover()", "fmt.Errorf", "errors.Wrap"},
	},
	// Data flow and tracing
	{
		pattern:        regexp.MustCompile(`(?i)\bdata\s*flow|\bcall\s*(graph|chain)|\btrace.*flow|\bhow.*work`),
		tool:           "trace_data_flow",
		searchHint:     "Use trace_data_flow to understand how data moves through the system. Identify the starting function and trace its callees.",
		searchPatterns: []string{},
	},
	// Security concerns
	{
		pattern:        regexp.MustCompile(`(?i)\bsecurity|\bvulnerab|\baudit|\binject|\bxss|\bsql`),
		tool:           "check_security",
		searchHint:     "Search for security-sensitive patterns. Look for SQL queries, user input handling, authentication, authorization checks.",
		searchPatterns: []string{"sql.", "exec.Command", "http.Get", "password", "token", "auth"},
	},
}

// RegexClassifier implements QueryClassifier using compiled regex patterns.
//
// Thread Safety: This type is safe for concurrent use after initialization.
type RegexClassifier struct {
	// analyticalPattern is a compiled regex combining all analytical patterns.
	analyticalPattern *regexp.Regexp
}

// NewRegexClassifier creates a new RegexClassifier with compiled patterns.
//
// Description:
//
//	Compiles all analytical query patterns into a single regex for
//	efficient matching. The regex uses case-insensitive matching
//	and word boundaries to reduce false positives.
//
// Outputs:
//
//	*RegexClassifier - A new classifier ready for use.
//
// Example:
//
//	classifier := NewRegexClassifier()
//	isAnalytical := classifier.IsAnalytical(ctx, "What tests exist?")
//
// Thread Safety: The returned classifier is safe for concurrent use.
func NewRegexClassifier() *RegexClassifier {
	// Join all patterns with OR, compile case-insensitive
	combined := "(?i)(" + strings.Join(analyticalPatterns, "|") + ")"
	compiled := regexp.MustCompile(combined)

	return &RegexClassifier{
		analyticalPattern: compiled,
	}
}

// IsAnalytical determines if a query requires tool exploration.
//
// Description:
//
//	Uses precompiled regex with word boundaries to identify analytical
//	queries. Creates a trace span for observability.
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	query - The user's question. Empty queries return false.
//
// Outputs:
//
//	bool - True if the query matches analytical patterns.
//
// Example:
//
//	classifier := NewRegexClassifier()
//	result := classifier.IsAnalytical(ctx, "How does authentication work?")
//	// result == true
//
// Thread Safety: This method is safe for concurrent use.
func (c *RegexClassifier) IsAnalytical(ctx context.Context, query string) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := otel.Tracer("classifier").Start(ctx, "classifier.RegexClassifier.IsAnalytical",
		trace.WithAttributes(
			attribute.Int("query_length", len(query)),
		),
	)
	defer span.End()

	if query == "" {
		span.SetAttributes(attribute.Bool("result", false))
		return false
	}

	result := c.analyticalPattern.MatchString(query)
	span.SetAttributes(
		attribute.Bool("result", result),
		attribute.String("matched_by", "regex_pattern"),
	)

	return result
}

// SuggestTool recommends a starting tool based on query type.
//
// Description:
//
//	Matches query against tool suggestion rules, then validates
//	the suggestion exists in the available tools list.
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	query - The user's question text.
//	available - List of available tool names. If nil/empty, returns no suggestion.
//
// Outputs:
//
//	string - Suggested tool name, or empty if no valid suggestion.
//	bool - True if a valid tool was suggested.
//
// Example:
//
//	classifier := NewRegexClassifier()
//	tool, ok := classifier.SuggestTool(ctx, "What tests exist?",
//	    []string{"find_entry_points", "trace_data_flow"})
//	// tool == "find_entry_points", ok == true
//
// Thread Safety: This method is safe for concurrent use.
func (c *RegexClassifier) SuggestTool(ctx context.Context, query string, available []string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := otel.Tracer("classifier").Start(ctx, "classifier.RegexClassifier.SuggestTool",
		trace.WithAttributes(
			attribute.Int("query_length", len(query)),
			attribute.Int("available_tools", len(available)),
		),
	)
	defer span.End()

	if len(available) == 0 {
		span.SetAttributes(attribute.String("result", "no_tools_available"))
		return "", false
	}

	// Build set of available tools for O(1) lookup
	availableSet := make(map[string]struct{}, len(available))
	for _, tool := range available {
		availableSet[tool] = struct{}{}
	}

	// Find first matching rule with available tool
	for _, rule := range toolSuggestionRules {
		if rule.pattern.MatchString(query) {
			if _, exists := availableSet[rule.tool]; exists {
				span.SetAttributes(
					attribute.String("suggested_tool", rule.tool),
					attribute.Bool("tool_available", true),
				)
				return rule.tool, true
			}
		}
	}

	// Default fallback: suggest first available tool
	if len(available) > 0 {
		// Prefer find_entry_points if available
		if _, exists := availableSet["find_entry_points"]; exists {
			span.SetAttributes(
				attribute.String("suggested_tool", "find_entry_points"),
				attribute.String("suggestion_type", "default_fallback"),
			)
			return "find_entry_points", true
		}

		// Otherwise use first available
		span.SetAttributes(
			attribute.String("suggested_tool", available[0]),
			attribute.String("suggestion_type", "first_available"),
		)
		return available[0], true
	}

	span.SetAttributes(attribute.String("result", "no_suggestion"))
	return "", false
}

// SuggestToolWithHint recommends a tool with specific search instructions.
//
// Description:
//
//	Enhanced version of SuggestTool that provides targeted search instructions.
//	Matches query against targetedSearchRules to find the best tool AND
//	specific search patterns the LLM should use.
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	query - The user's question text.
//	available - List of available tool names. If nil/empty, returns nil.
//
// Outputs:
//
//	*ToolSuggestion - The suggestion with search instructions, or nil.
//	bool - True if a valid suggestion was found.
//
// Example:
//
//	classifier := NewRegexClassifier()
//	suggestion, ok := classifier.SuggestToolWithHint(ctx, "What tests exist?",
//	    []string{"find_entry_points", "trace_data_flow"})
//	// suggestion.ToolName == "find_entry_points"
//	// suggestion.SearchHint contains specific instructions
//
// Thread Safety: This method is safe for concurrent use.
func (c *RegexClassifier) SuggestToolWithHint(ctx context.Context, query string, available []string) (*ToolSuggestion, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := otel.Tracer("classifier").Start(ctx, "classifier.RegexClassifier.SuggestToolWithHint",
		trace.WithAttributes(
			attribute.Int("query_length", len(query)),
			attribute.Int("available_tools", len(available)),
		),
	)
	defer span.End()

	if len(available) == 0 {
		span.SetAttributes(attribute.String("result", "no_tools_available"))
		return nil, false
	}

	// Build set of available tools for O(1) lookup
	availableSet := make(map[string]struct{}, len(available))
	for _, tool := range available {
		availableSet[tool] = struct{}{}
	}

	// Find first matching rule with available tool (using enhanced rules)
	for _, rule := range targetedSearchRules {
		if rule.pattern.MatchString(query) {
			if _, exists := availableSet[rule.tool]; exists {
				suggestion := &ToolSuggestion{
					ToolName:       rule.tool,
					SearchHint:     rule.searchHint,
					SearchPatterns: rule.searchPatterns,
				}

				span.SetAttributes(
					attribute.String("suggested_tool", rule.tool),
					attribute.String("search_hint", rule.searchHint),
					attribute.Int("pattern_count", len(rule.searchPatterns)),
					attribute.Bool("tool_available", true),
				)
				return suggestion, true
			}
		}
	}

	// Fallback to basic tool suggestion without enhanced hints
	tool, ok := c.SuggestTool(ctx, query, available)
	if ok {
		suggestion := &ToolSuggestion{
			ToolName:       tool,
			SearchHint:     "Use " + tool + " to explore the codebase and gather information before answering.",
			SearchPatterns: nil,
		}
		span.SetAttributes(
			attribute.String("suggested_tool", tool),
			attribute.String("suggestion_type", "fallback_basic"),
		)
		return suggestion, true
	}

	span.SetAttributes(attribute.String("result", "no_suggestion"))
	return nil, false
}

// Ensure RegexClassifier implements QueryClassifier.
var _ QueryClassifier = (*RegexClassifier)(nil)
