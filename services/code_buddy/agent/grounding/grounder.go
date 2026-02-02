// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

// DefaultGrounder orchestrates multiple grounding checks.
//
// This is the main entry point for response validation. It coordinates
// all registered Checkers and aggregates their violations into a single Result.
//
// Thread Safety: Safe for concurrent use after construction.
type DefaultGrounder struct {
	config   Config
	checkers []Checker
}

// NewDefaultGrounder creates a new DefaultGrounder with the given checkers.
//
// Inputs:
//
//	config - Configuration for grounding behavior.
//	checkers - The checkers to run (executed in order).
//
// Outputs:
//
//	*DefaultGrounder - The configured grounder.
func NewDefaultGrounder(config Config, checkers ...Checker) *DefaultGrounder {
	return &DefaultGrounder{
		config:   config,
		checkers: checkers,
	}
}

// Validate implements Grounder.
//
// Description:
//
//	Runs all registered checkers against the response and aggregates
//	violations. Supports short-circuit on critical violations if configured.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	response - The LLM response content to validate.
//	assembledCtx - The context that was given to the LLM.
//
// Outputs:
//
//	*Result - The aggregated validation result.
//	error - Non-nil only if validation itself fails (not for violations).
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) Validate(ctx context.Context, response string, assembledCtx *agent.AssembledContext) (*Result, error) {
	if !g.config.Enabled {
		return &Result{
			Grounded:   true,
			Confidence: 1.0,
		}, nil
	}

	start := time.Now()

	// Create timeout context if configured
	if g.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.config.Timeout)
		defer cancel()
	}

	// Build check input from assembled context
	input, err := g.buildCheckInput(response, assembledCtx)
	if err != nil {
		return nil, fmt.Errorf("building check input: %w", err)
	}

	result := &Result{
		Grounded:   true,
		Confidence: 1.0,
	}

	// Run each checker
	for _, checker := range g.checkers {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		violations := checker.Check(ctx, input)
		result.ChecksRun++

		for _, v := range violations {
			result.AddViolation(v)

			// Short-circuit on critical if configured
			if g.config.ShortCircuitOnCritical && v.Severity == SeverityCritical {
				result.Grounded = false
				result.CheckDuration = time.Since(start)
				return result, nil
			}
		}
	}

	// Determine if grounded based on violations and confidence
	if result.CriticalCount > 0 {
		result.Grounded = false
	} else if result.Confidence < g.config.MinConfidence {
		result.Grounded = false
	} else if len(result.Violations) >= g.config.MaxViolationsBeforeReject {
		result.Grounded = false
	}

	result.CheckDuration = time.Since(start)
	return result, nil
}

// ShouldReject implements Grounder.
//
// Description:
//
//	Determines if a validation result warrants rejecting the response.
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) ShouldReject(result *Result) bool {
	if result == nil {
		return false
	}

	if g.config.RejectOnCritical && result.HasCritical() {
		return true
	}

	if len(result.Violations) >= g.config.MaxViolationsBeforeReject {
		return true
	}

	return !result.Grounded
}

// GenerateFootnote implements Grounder.
//
// Description:
//
//	Creates a warning footnote for responses with warnings but no critical
//	violations. Returns empty string if no footnote is needed.
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) GenerateFootnote(result *Result) string {
	if result == nil || !g.config.AddFootnoteOnWarning {
		return ""
	}

	// No footnote for critical violations (response should be rejected)
	if result.HasCritical() {
		return ""
	}

	// No footnote if no warnings
	if !result.HasWarnings() {
		return ""
	}

	var warnings []string
	for _, v := range result.Violations {
		if v.Severity == SeverityWarning {
			warnings = append(warnings, v.Message)
		}
	}

	if len(warnings) == 0 {
		return ""
	}

	return fmt.Sprintf("\n\n---\n⚠️ **Grounding warnings**: %s", strings.Join(warnings, "; "))
}

// buildCheckInput constructs the CheckInput from AssembledContext.
func (g *DefaultGrounder) buildCheckInput(response string, assembledCtx *agent.AssembledContext) (*CheckInput, error) {
	if assembledCtx == nil {
		return &CheckInput{
			Response: response,
		}, nil
	}

	// Truncate response if configured
	scanResponse := response
	if g.config.MaxResponseScanLength > 0 && len(response) > g.config.MaxResponseScanLength {
		scanResponse = response[:g.config.MaxResponseScanLength]
	}

	// Detect primary language from file extensions in context
	projectLang := g.detectProjectLanguage(assembledCtx)

	input := &CheckInput{
		Response:      scanResponse,
		ProjectLang:   projectLang,
		CodeContext:   assembledCtx.CodeContext,
		ToolResults:   convertToolResults(assembledCtx.ToolResults),
		EvidenceIndex: g.buildEvidenceIndex(assembledCtx),
	}

	return input, nil
}

// detectProjectLanguage determines the primary language from context files.
func (g *DefaultGrounder) detectProjectLanguage(assembledCtx *agent.AssembledContext) string {
	if assembledCtx == nil {
		return ""
	}

	// Count occurrences of each language
	langCounts := make(map[string]int)

	for _, entry := range assembledCtx.CodeContext {
		lang := detectLanguageFromPath(entry.FilePath)
		if lang != "" {
			langCounts[lang]++
		}
	}

	// Find the most common language
	maxCount := 0
	primaryLang := ""
	for lang, count := range langCounts {
		if count > maxCount {
			maxCount = count
			primaryLang = lang
		}
	}

	return primaryLang
}

// buildEvidenceIndex creates an EvidenceIndex from the AssembledContext.
//
// This index tracks exactly what was shown to the LLM so we can verify
// that claims in the response are grounded in actual evidence.
func (g *DefaultGrounder) buildEvidenceIndex(assembledCtx *agent.AssembledContext) *EvidenceIndex {
	if assembledCtx == nil {
		return NewEvidenceIndex()
	}

	idx := NewEvidenceIndex()
	var contentBuilder strings.Builder

	// Index code context
	for _, entry := range assembledCtx.CodeContext {
		// Add file paths
		idx.Files[entry.FilePath] = true
		idx.Files[normalizePath(entry.FilePath)] = true
		idx.FileBasenames[filepath.Base(entry.FilePath)] = true

		// Store content for line validation
		idx.FileContents[entry.FilePath] = entry.Content
		idx.FileContents[normalizePath(entry.FilePath)] = entry.Content

		// Add symbols
		if entry.SymbolName != "" {
			idx.Symbols[entry.SymbolName] = true
		}

		// Extract symbols from content (basic extraction)
		extractSymbols(entry.Content, idx.Symbols)

		// Detect language from extension
		lang := detectLanguageFromPath(entry.FilePath)
		if lang != "" {
			idx.Languages[lang] = true
		}

		// Accumulate raw content
		contentBuilder.WriteString(entry.Content)
		contentBuilder.WriteString("\n")
	}

	// Index tool results
	for _, result := range assembledCtx.ToolResults {
		contentBuilder.WriteString(result.Output)
		contentBuilder.WriteString("\n")

		// Extract file paths from tool output
		extractFilePathsFromText(result.Output, idx.Files, idx.FileBasenames)
	}

	idx.RawContent = contentBuilder.String()

	return idx
}

// convertToolResults converts agent.ToolResult to grounding.ToolResult.
func convertToolResults(agentResults []agent.ToolResult) []ToolResult {
	results := make([]ToolResult, len(agentResults))
	for i, r := range agentResults {
		results[i] = ToolResult{
			InvocationID: r.InvocationID,
			Output:       r.Output,
		}
	}
	return results
}

// detectLanguageFromPath returns the language based on file extension.
func detectLanguageFromPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".jsx":
		return "javascript"
	case ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	default:
		return ""
	}
}

// extractSymbols performs basic symbol extraction from code.
// This is a simplified extraction; more sophisticated analysis uses AST parsing.
func extractSymbols(content string, symbols map[string]bool) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Go function definitions
		if strings.HasPrefix(line, "func ") {
			// Extract function name
			rest := strings.TrimPrefix(line, "func ")
			// Handle method receivers: func (r *Receiver) Name(
			if strings.HasPrefix(rest, "(") {
				if idx := strings.Index(rest, ")"); idx != -1 {
					rest = strings.TrimSpace(rest[idx+1:])
				}
			}
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}

		// Go type definitions
		if strings.HasPrefix(line, "type ") {
			rest := strings.TrimPrefix(line, "type ")
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				symbols[fields[0]] = true
			}
		}

		// Python function definitions
		if strings.HasPrefix(line, "def ") {
			rest := strings.TrimPrefix(line, "def ")
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}

		// Python class definitions
		if strings.HasPrefix(line, "class ") {
			rest := strings.TrimPrefix(line, "class ")
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			} else if idx := strings.Index(rest, ":"); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}
	}
}

// extractFilePathsFromText extracts file paths mentioned in text.
func extractFilePathsFromText(text string, files map[string]bool, basenames map[string]bool) {
	// Look for common file path patterns
	words := strings.Fields(text)
	for _, word := range words {
		// Clean up word
		word = strings.Trim(word, "\"'`()[]{},:;")

		// Check if it looks like a file path
		if strings.Contains(word, "/") || strings.Contains(word, "\\") {
			// Has path separator
			if hasCodeExtension(word) {
				files[word] = true
				files[normalizePath(word)] = true
				basenames[filepath.Base(word)] = true
			}
		} else if hasCodeExtension(word) {
			// Just a filename with code extension
			basenames[word] = true
		}
	}
}

// hasCodeExtension checks if a path has a recognized code file extension.
func hasCodeExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".rs",
		".c", ".cpp", ".h", ".hpp", ".cc", ".md", ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
