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
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

// SanitizeConfig configures output sanitization behavior.
type SanitizeConfig struct {
	// Model is the LLM model type for model-aware sanitization.
	Model agent.ModelType

	// PreserveCodeBlocks preserves markdown code blocks during sanitization.
	PreserveCodeBlocks bool

	// PreserveInlineCode preserves inline code during sanitization.
	PreserveInlineCode bool

	// StripThinkTags removes <think> tags.
	StripThinkTags bool

	// StripReasoningTags removes <thought>, <reasoning>, <reflection> tags.
	StripReasoningTags bool
}

// DefaultSanitizeConfig returns production defaults.
func DefaultSanitizeConfig() SanitizeConfig {
	return SanitizeConfig{
		Model:              agent.ModelGeneric,
		PreserveCodeBlocks: true,
		PreserveInlineCode: true,
		StripThinkTags:     true,
		StripReasoningTags: true,
	}
}

// OutputSanitizer removes leaked tool markup from responses.
//
// Description:
//
//	Provides a final defense layer that strips leaked tool markup
//	before returning responses to the user. Model-aware to avoid
//	stripping native formats for their respective models.
//
// Thread Safety: Safe for concurrent use (stateless, compiled regex).
type OutputSanitizer struct {
	model           agent.ModelType
	combinedPattern *regexp.Regexp
	preservePattern *regexp.Regexp
	config          SanitizeConfig
}

// NewOutputSanitizer creates a sanitizer for the given model.
//
// Description:
//
//	Compiles combined regex at construction time for O(n) performance.
//	The sanitizer is model-aware to preserve native formats.
//
// Inputs:
//
//	config - Configuration for sanitization behavior.
//
// Outputs:
//
//	*OutputSanitizer - The configured sanitizer.
func NewOutputSanitizer(config SanitizeConfig) *OutputSanitizer {
	s := &OutputSanitizer{
		model:  config.Model,
		config: config,
	}
	s.combinedPattern = s.buildCombinedPattern()
	s.preservePattern = s.buildPreservePattern()
	return s
}

// buildCombinedPattern creates a single regex for all patterns to strip.
func (s *OutputSanitizer) buildCombinedPattern() *regexp.Regexp {
	var patterns []string

	// Always strip tool call markup
	patterns = append(patterns,
		`<tool_call>.*?</tool_call>`,
		`<execute>.*?</execute>`,
	)

	// Strip think/reasoning tags if configured
	if s.config.StripThinkTags {
		patterns = append(patterns, `<think>.*?</think>`)
	}

	if s.config.StripReasoningTags {
		patterns = append(patterns,
			`<thought>.*?</thought>`,
			`<reasoning>.*?</reasoning>`,
			`<reflection>.*?</reflection>`,
		)
	}

	// Add Anthropic patterns only if NOT Claude
	// Claude uses these natively, so we shouldn't strip them
	if s.model != agent.ModelClaude {
		patterns = append(patterns,
			`<(?:antml:)?function_calls>.*?</(?:antml:)?function_calls>`,
			`<(?:antml:)?invoke\s+name="[^"]*">.*?</(?:antml:)?invoke>`,
		)
	}

	combined := "(?s)(" + strings.Join(patterns, "|") + ")"
	return regexp.MustCompile(combined)
}

// buildPreservePattern creates regex for preservation zones.
func (s *OutputSanitizer) buildPreservePattern() *regexp.Regexp {
	var patterns []string

	if s.config.PreserveCodeBlocks {
		patterns = append(patterns, "```[\\s\\S]*?```")
	}

	if s.config.PreserveInlineCode {
		patterns = append(patterns, "`[^`]+`")
	}

	if len(patterns) == 0 {
		// No preservation, return a pattern that never matches
		return regexp.MustCompile(`^$`)
	}

	combined := "(?s)(" + strings.Join(patterns, "|") + ")"
	return regexp.MustCompile(combined)
}

// SanitizeResult contains the result of sanitization.
type SanitizeResult struct {
	// Content is the sanitized text.
	Content string

	// Stripped indicates if any content was stripped.
	Stripped bool

	// StrippedCount is the number of patterns stripped.
	StrippedCount int
}

// Sanitize removes leaked tool markup while preserving legitimate content.
//
// Description:
//
//	Removes tool markup patterns from the response while preserving
//	markdown code blocks and inline code. Uses a single-pass approach
//	for O(n) performance.
//
// Inputs:
//
//	content - Response text potentially containing leaked markup.
//
// Outputs:
//
//	SanitizeResult - Sanitized text and metadata.
//
// Thread Safety: Safe for concurrent use.
func (s *OutputSanitizer) Sanitize(content string) SanitizeResult {
	result := SanitizeResult{
		Content: content,
	}

	// Early exit: no < means no XML to strip
	if !strings.Contains(content, "<") {
		return result
	}

	// Find preservation zones (code blocks)
	preserveZones := s.preservePattern.FindAllStringIndex(content, -1)

	// If no code blocks, simple single-pass replacement
	if len(preserveZones) == 0 {
		beforeLen := len(content)
		sanitized := s.combinedPattern.ReplaceAllString(content, "")
		result.Content = s.cleanupWhitespace(sanitized)
		result.Stripped = len(sanitized) < beforeLen
		if result.Stripped {
			result.StrippedCount = len(s.combinedPattern.FindAllStringIndex(content, -1))
		}
		return result
	}

	// Build result, skipping preservation zones
	var builder strings.Builder
	lastEnd := 0

	for _, zone := range preserveZones {
		// Process text before this zone
		before := content[lastEnd:zone[0]]
		sanitizedBefore := s.combinedPattern.ReplaceAllString(before, "")
		if len(sanitizedBefore) < len(before) {
			result.Stripped = true
			result.StrippedCount++
		}
		builder.WriteString(sanitizedBefore)

		// Preserve the zone content as-is
		builder.WriteString(content[zone[0]:zone[1]])
		lastEnd = zone[1]
	}

	// Process remaining text after last zone
	if lastEnd < len(content) {
		remaining := content[lastEnd:]
		sanitizedRemaining := s.combinedPattern.ReplaceAllString(remaining, "")
		if len(sanitizedRemaining) < len(remaining) {
			result.Stripped = true
			result.StrippedCount++
		}
		builder.WriteString(sanitizedRemaining)
	}

	result.Content = s.cleanupWhitespace(builder.String())
	return result
}

// SanitizeString is a convenience method that returns just the sanitized string.
//
// Inputs:
//
//	content - Response text to sanitize.
//
// Outputs:
//
//	string - Sanitized text.
//
// Thread Safety: Safe for concurrent use.
func (s *OutputSanitizer) SanitizeString(content string) string {
	return s.Sanitize(content).Content
}

// cleanupWhitespace removes excessive whitespace from sanitized content.
func (s *OutputSanitizer) cleanupWhitespace(content string) string {
	// Collapse multiple newlines to at most two
	content = multipleNewlinesRegex.ReplaceAllString(content, "\n\n")

	// Trim leading/trailing whitespace
	return strings.TrimSpace(content)
}

// Package-level compiled regex for whitespace cleanup.
var multipleNewlinesRegex = regexp.MustCompile(`\n{3,}`)

// Package-level cached sanitizer for QuickSanitize (generic model).
var (
	defaultSanitizer     *OutputSanitizer
	defaultSanitizerOnce sync.Once
)

func getDefaultSanitizer() *OutputSanitizer {
	defaultSanitizerOnce.Do(func() {
		defaultSanitizer = NewOutputSanitizer(DefaultSanitizeConfig())
	})
	return defaultSanitizer
}

// ContainsLeakedMarkup checks if content contains leaked tool markup.
//
// Description:
//
//	Quick check to determine if sanitization is needed. Useful for
//	metrics and logging without performing full sanitization.
//
// Inputs:
//
//	content - Response text to check.
//
// Outputs:
//
//	bool - True if leaked markup is detected.
//
// Thread Safety: Safe for concurrent use.
func (s *OutputSanitizer) ContainsLeakedMarkup(content string) bool {
	if !strings.Contains(content, "<") {
		return false
	}
	return s.combinedPattern.MatchString(content)
}

// GetModel returns the configured model type.
func (s *OutputSanitizer) GetModel() agent.ModelType {
	return s.model
}

// QuickSanitize is a package-level function for one-off sanitization.
//
// Description:
//
//	Uses a cached sanitizer with default config for efficient repeated use.
//	For model-specific sanitization, use QuickSanitizeForModel instead.
//
// Inputs:
//
//	content - Response text to sanitize.
//
// Outputs:
//
//	string - Sanitized text.
//
// Thread Safety: Safe for concurrent use.
func QuickSanitize(content string) string {
	return getDefaultSanitizer().SanitizeString(content)
}

// QuickSanitizeForModel sanitizes with model awareness.
//
// Inputs:
//
//	content - Response text to sanitize.
//	model - The LLM model type.
//
// Outputs:
//
//	string - Sanitized text.
func QuickSanitizeForModel(content string, model agent.ModelType) string {
	config := DefaultSanitizeConfig()
	config.Model = model
	sanitizer := NewOutputSanitizer(config)
	return sanitizer.SanitizeString(content)
}
