// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package control

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// ExtractionConfig configures buffer extraction behavior.
type ExtractionConfig struct {
	// MinFinalResponseLength is the minimum chars for a valid final response.
	MinFinalResponseLength int

	// MaxSummaryLength is the max chars per finding summary.
	MaxSummaryLength int

	// MaxFindings is the max number of findings to include in fallback.
	MaxFindings int

	// IncludeDisclaimer adds a disclaimer to fallback responses.
	IncludeDisclaimer bool
}

// DefaultExtractionConfig returns production defaults.
func DefaultExtractionConfig() ExtractionConfig {
	return ExtractionConfig{
		MinFinalResponseLength: 50,
		MaxSummaryLength:       500,
		MaxFindings:            10,
		IncludeDisclaimer:      true,
	}
}

// BufferExtractor generates fallback responses from accumulated state.
//
// Description:
//
//	When the agent gathers information during exploration but fails to
//	synthesize a response (due to step exhaustion, timeout, or other issues),
//	the BufferExtractor constructs a fallback response from accumulated
//	tool results to avoid returning empty responses.
//
// Thread Safety: Safe for concurrent use (stateless).
type BufferExtractor struct {
	config ExtractionConfig
	logger *slog.Logger
}

// NewBufferExtractor creates a new extractor.
//
// Inputs:
//
//	config - Configuration for extraction behavior.
//	logger - Logger for diagnostic output (nil for default).
//
// Outputs:
//
//	*BufferExtractor - The configured extractor.
func NewBufferExtractor(config ExtractionConfig, logger *slog.Logger) *BufferExtractor {
	if logger == nil {
		logger = slog.Default()
	}
	return &BufferExtractor{
		config: config,
		logger: logger,
	}
}

// ExtractionResult contains the extracted response.
type ExtractionResult struct {
	// Response is the generated response text.
	Response string

	// IsFallback indicates if this is a fallback response.
	IsFallback bool

	// SourceCount is the number of tool results used.
	SourceCount int

	// FileCount is the number of unique files referenced.
	FileCount int
}

// ToolResultInput represents a tool execution result for extraction.
type ToolResultInput struct {
	// ToolName is the name of the tool that was executed.
	ToolName string

	// Content is the output from the tool.
	Content string

	// Success indicates if the tool execution succeeded.
	Success bool
}

// AgentStateInput provides the agent state for extraction.
type AgentStateInput struct {
	// FinalResponse is the synthesized response (if any).
	FinalResponse string

	// ToolResults are the tool execution results.
	ToolResults []ToolResultInput

	// Query is the original user query.
	Query string
}

// Extract generates a response from accumulated state.
//
// Description:
//
//	First checks if there's a valid final response. If not, generates
//	a fallback response from accumulated tool results. If no tool results
//	exist, returns a "no information" message.
//
// Inputs:
//
//	state - Current agent state with tool results.
//
// Outputs:
//
//	ExtractionResult - Generated response with metadata.
//
// Thread Safety: Safe for concurrent use.
func (e *BufferExtractor) Extract(state AgentStateInput) ExtractionResult {
	// Check if we have a valid synthesized response
	if state.FinalResponse != "" && len(state.FinalResponse) >= e.config.MinFinalResponseLength {
		return ExtractionResult{
			Response:   state.FinalResponse,
			IsFallback: false,
		}
	}

	// Collect successful tool results
	successfulResults := e.collectSuccessfulResults(state.ToolResults)
	if len(successfulResults) == 0 {
		e.logger.Warn("no tool results to extract",
			slog.String("query", truncateQuery(state.Query, 100)),
		)
		return ExtractionResult{
			Response:   e.buildNoInfoResponse(state.Query),
			IsFallback: true,
		}
	}

	// Build fallback response
	response, fileCount := e.buildFallbackResponse(successfulResults)

	e.logger.Info("generated fallback response",
		slog.Int("source_count", len(successfulResults)),
		slog.Int("file_count", fileCount),
		slog.Int("response_len", len(response)),
	)

	return ExtractionResult{
		Response:    response,
		IsFallback:  true,
		SourceCount: len(successfulResults),
		FileCount:   fileCount,
	}
}

// collectSuccessfulResults filters for successful tool results.
func (e *BufferExtractor) collectSuccessfulResults(results []ToolResultInput) []ToolResultInput {
	var successful []ToolResultInput
	for _, r := range results {
		if r.Success && len(r.Content) > 0 {
			successful = append(successful, r)
		}
	}
	return successful
}

// buildNoInfoResponse creates a response when no information was gathered.
func (e *BufferExtractor) buildNoInfoResponse(_ string) string {
	return "I explored the codebase but wasn't able to gather sufficient information to answer your question. Please try rephrasing or asking about a specific file or function."
}

// buildFallbackResponse constructs a response from tool results.
func (e *BufferExtractor) buildFallbackResponse(results []ToolResultInput) (string, int) {
	var sb strings.Builder

	sb.WriteString("Based on my exploration, here's what I found:\n\n")

	// Collect unique files
	files := make(map[string]bool)
	for _, r := range results {
		if filePath := e.extractFilePath(r.Content); filePath != "" {
			files[filePath] = true
		}
	}

	if len(files) > 0 {
		// Sort files for deterministic output
		sortedFiles := make([]string, 0, len(files))
		for f := range files {
			sortedFiles = append(sortedFiles, f)
		}
		sort.Strings(sortedFiles)

		sb.WriteString("**Files examined:**\n")
		for i, f := range sortedFiles {
			if i >= e.config.MaxFindings {
				sb.WriteString(fmt.Sprintf("- ... and %d more files\n", len(sortedFiles)-e.config.MaxFindings))
				break
			}
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		sb.WriteString("\n")
	}

	// Extract key findings
	sb.WriteString("**Key findings:**\n")
	findingCount := 0
	for _, r := range results {
		if findingCount >= e.config.MaxFindings {
			break
		}

		summary := e.summarizeContent(r)
		if summary != "" {
			sb.WriteString(fmt.Sprintf("- %s\n", summary))
			findingCount++
		}
	}

	if e.config.IncludeDisclaimer {
		sb.WriteString("\n*Note: This response was generated from exploration results as the synthesis phase did not complete normally.*")
	}

	return sb.String(), len(files)
}

// Package-level compiled patterns for file path extraction.
var filePathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^File:\s*(.+)$`),
	regexp.MustCompile(`(?i)reading file[:\s]+([^\s]+)`),
	regexp.MustCompile(`([a-zA-Z0-9_/.-]+\.(go|py|js|ts|rs|java|c|cpp|h|hpp))`),
}

// extractFilePath attempts to extract a file path from content.
func (e *BufferExtractor) extractFilePath(content string) string {
	for _, p := range filePathPatterns {
		if match := p.FindStringSubmatch(content); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

// summarizeContent creates a summary of tool output.
func (e *BufferExtractor) summarizeContent(result ToolResultInput) string {
	content := result.Content

	// Skip empty or very short content
	if len(content) < 10 {
		return ""
	}

	// Extract first meaningful line if possible
	lines := strings.Split(content, "\n")
	var summary string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and common header patterns
		if line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===") {
			continue
		}
		// Skip file path only lines
		if isFilePathOnly(line) {
			continue
		}
		summary = line
		break
	}

	if summary == "" {
		// Fall back to first non-empty portion
		summary = strings.TrimSpace(content)
	}

	// Truncate if needed
	return e.truncateSummary(summary)
}

// isFilePathOnly checks if a line is just a file path.
var filePathOnlyRegex = regexp.MustCompile(`^[a-zA-Z0-9_/.-]+\.(go|py|js|ts|rs|java|c|cpp|h|hpp)$`)

func isFilePathOnly(line string) bool {
	return filePathOnlyRegex.MatchString(strings.TrimSpace(line))
}

// truncateSummary truncates a summary to the configured max length.
func (e *BufferExtractor) truncateSummary(content string) string {
	// Remove excessive whitespace
	content = strings.TrimSpace(content)
	content = whitespaceCollapseRegex.ReplaceAllString(content, " ")

	if len(content) <= e.config.MaxSummaryLength {
		return content
	}

	// Truncate at word boundary
	truncated := content[:e.config.MaxSummaryLength]
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > e.config.MaxSummaryLength/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}

// Package-level regex for whitespace collapse.
var whitespaceCollapseRegex = regexp.MustCompile(`\s+`)

// truncateQuery truncates a query for logging.
func truncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen] + "..."
}

// HasUsableContent checks if there's enough content for extraction.
//
// Description:
//
//	Quick check to determine if extraction would produce meaningful
//	output. Useful for deciding whether to attempt fallback extraction.
//
// Inputs:
//
//	state - Agent state to check.
//
// Outputs:
//
//	bool - True if meaningful extraction is possible.
//
// Thread Safety: Safe for concurrent use.
func (e *BufferExtractor) HasUsableContent(state AgentStateInput) bool {
	// Valid final response
	if len(state.FinalResponse) >= e.config.MinFinalResponseLength {
		return true
	}

	// Any successful tool results
	for _, r := range state.ToolResults {
		if r.Success && len(r.Content) > 0 {
			return true
		}
	}

	return false
}
