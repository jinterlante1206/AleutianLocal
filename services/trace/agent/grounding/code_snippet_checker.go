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
	"regexp"
	"strings"
)

// CodeClassification categorizes how a code snippet matches evidence.
type CodeClassification int

const (
	// ClassificationVerbatim means the code is an exact or near-exact match.
	// Similarity >= VerbatimThreshold (default 90%).
	ClassificationVerbatim CodeClassification = iota

	// ClassificationModified means similar code was found but differs.
	// ModifiedThreshold <= Similarity < VerbatimThreshold (default 50-90%).
	ClassificationModified

	// ClassificationFabricated means no matching code in evidence.
	// Similarity < ModifiedThreshold (default < 50%).
	ClassificationFabricated

	// ClassificationSkipped means the code was skipped (suggestion phrase or too short).
	ClassificationSkipped
)

// String returns the string representation of a CodeClassification.
func (c CodeClassification) String() string {
	switch c {
	case ClassificationVerbatim:
		return "verbatim"
	case ClassificationModified:
		return "modified"
	case ClassificationFabricated:
		return "fabricated"
	case ClassificationSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// CodeBlock represents an extracted code block from response.
type CodeBlock struct {
	// Content is the code content without the fences.
	Content string

	// Language is the language specifier (e.g., "go", "python"), may be empty.
	Language string

	// Position is the character offset in the response where this block starts.
	Position int

	// EndPosition is the character offset where this block ends.
	EndPosition int

	// ContextBefore is the text immediately before the code block.
	ContextBefore string

	// IsInline indicates if this is inline code (single backticks) vs fenced.
	IsInline bool
}

// CodeBlockMatch represents the result of matching a code block against evidence.
type CodeBlockMatch struct {
	// Block is the original code block.
	Block CodeBlock

	// Classification is how the code was classified.
	Classification CodeClassification

	// Similarity is the best similarity score found (0.0-1.0).
	Similarity float64

	// MatchedFile is the file path where the best match was found.
	MatchedFile string

	// SkipReason explains why the block was skipped (if Classification == Skipped).
	SkipReason string
}

// suggestionPhrases are phrases that indicate suggested/example code.
// When these appear before a code block, skip validation.
var suggestionPhrases = []string{
	"you could",
	"you might",
	"consider",
	"example of how",
	"one way to",
	"here's how you might",
	"a typical pattern",
	"something like",
	"for example",
	"would look like",
	"you can",
	"try this",
	"here's an example",
	"sample code",
	"template",
	"could be",
	"might be",
	"should look like",
	"implement it like",
	"written as",
	"like this",
	"as follows",
	"suggested",
	"recommended",
	"proposed",
}

// Compiled regex patterns for code block extraction.
var (
	// Matches fenced code blocks: ```lang ... ``` or ``` ... ```
	fencedCodePattern = regexp.MustCompile("(?s)```(\\w*)\\n?(.*?)```")

	// Matches inline code: `code` (not backticks inside code blocks)
	inlineCodePattern = regexp.MustCompile("`([^`]+)`")
)

// CodeSnippetChecker validates code blocks in responses against actual file contents.
//
// This checker detects fabricated code snippets:
// - Code blocks that don't exist in the codebase
// - "Improved" code shown as original
// - Plausible-looking code that was never written
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type CodeSnippetChecker struct {
	config *CodeSnippetCheckerConfig
}

// NewCodeSnippetChecker creates a new code snippet checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*CodeSnippetChecker - The configured checker.
func NewCodeSnippetChecker(config *CodeSnippetCheckerConfig) *CodeSnippetChecker {
	if config == nil {
		config = DefaultCodeSnippetCheckerConfig()
	}
	return &CodeSnippetChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *CodeSnippetChecker) Name() string {
	return "code_snippet_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts code blocks from the response and validates them against
//	the EvidenceIndex.FileContents. Detects fabricated code by comparing
//	snippets against actual file contents using fuzzy matching.
//
// Thread Safety: Safe for concurrent use.
func (c *CodeSnippetChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Need evidence index with file contents to validate
	if input.EvidenceIndex == nil || len(input.EvidenceIndex.FileContents) == 0 {
		return nil
	}

	var violations []Violation

	// Extract code blocks from response
	blocks := c.extractCodeBlocks(input.Response)

	// Limit blocks to check
	if c.config.MaxCodeBlocksToCheck > 0 && len(blocks) > c.config.MaxCodeBlocksToCheck {
		blocks = blocks[:c.config.MaxCodeBlocksToCheck]
	}

	// Validate each block
	for _, block := range blocks {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		match := c.matchCodeBlock(ctx, block, input)

		// Skip if classified as verbatim or skipped
		if match.Classification == ClassificationVerbatim || match.Classification == ClassificationSkipped {
			continue
		}

		// Generate violation for modified or fabricated code
		v := c.generateViolation(match)
		if v != nil {
			RecordFabricatedCode(ctx, match.Classification.String(), match.Similarity, len(block.Content))
			violations = append(violations, *v)
		}
	}

	return violations
}

// extractCodeBlocks extracts all code blocks from response text.
func (c *CodeSnippetChecker) extractCodeBlocks(response string) []CodeBlock {
	var blocks []CodeBlock

	// Extract fenced code blocks
	matches := fencedCodePattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		// match[0]:match[1] is full match
		// match[2]:match[3] is language (group 1)
		// match[4]:match[5] is content (group 2)

		lang := ""
		if match[2] != -1 && match[3] != -1 {
			lang = response[match[2]:match[3]]
		}

		content := ""
		if match[4] != -1 && match[5] != -1 {
			content = response[match[4]:match[5]]
		}

		// Get context before the code block
		contextStart := match[0] - 500 // Look back up to 500 chars
		if contextStart < 0 {
			contextStart = 0
		}
		contextBefore := response[contextStart:match[0]]

		blocks = append(blocks, CodeBlock{
			Content:       content,
			Language:      lang,
			Position:      match[0],
			EndPosition:   match[1],
			ContextBefore: contextBefore,
			IsInline:      false,
		})
	}

	// Extract inline code if enabled
	if c.config.CheckInlineCode {
		inlineMatches := inlineCodePattern.FindAllStringSubmatchIndex(response, -1)
		for _, match := range inlineMatches {
			if len(match) < 4 {
				continue
			}

			content := ""
			if match[2] != -1 && match[3] != -1 {
				content = response[match[2]:match[3]]
			}

			// Skip if this position is inside a fenced block
			isInsideFenced := false
			for _, b := range blocks {
				if match[0] >= b.Position && match[1] <= b.EndPosition {
					isInsideFenced = true
					break
				}
			}
			if isInsideFenced {
				continue
			}

			contextStart := match[0] - 200
			if contextStart < 0 {
				contextStart = 0
			}

			blocks = append(blocks, CodeBlock{
				Content:       content,
				Position:      match[0],
				EndPosition:   match[1],
				ContextBefore: response[contextStart:match[0]],
				IsInline:      true,
			})
		}
	}

	return blocks
}

// matchCodeBlock matches a code block against evidence.
func (c *CodeSnippetChecker) matchCodeBlock(ctx context.Context, block CodeBlock, input *CheckInput) CodeBlockMatch {
	result := CodeBlockMatch{
		Block:          block,
		Classification: ClassificationFabricated,
		Similarity:     0.0,
	}

	// Skip if too short
	if len(block.Content) < c.config.MinSnippetLength {
		result.Classification = ClassificationSkipped
		result.SkipReason = "snippet too short"
		return result
	}

	// Skip if suggestion phrase detected in context
	if c.hasSuggestionPhrase(block.ContextBefore) {
		result.Classification = ClassificationSkipped
		result.SkipReason = "suggestion phrase detected"
		return result
	}

	// Truncate if too long
	content := block.Content
	if len(content) > c.config.MaxSnippetLength {
		content = content[:c.config.MaxSnippetLength]
	}

	// Normalize the snippet for comparison
	normalizedSnippet := c.normalize(content)
	if len(normalizedSnippet) < c.config.MinSnippetLength {
		result.Classification = ClassificationSkipped
		result.SkipReason = "snippet too short after normalization"
		return result
	}

	// Search all file contents for best match
	var bestSimilarity float64
	var bestFile string

	for filePath, fileContent := range input.EvidenceIndex.FileContents {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		similarity := c.findBestSimilarity(normalizedSnippet, fileContent)
		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestFile = filePath
		}

		// Early exit if exact match found
		if similarity >= 1.0 {
			break
		}
	}

	result.Similarity = bestSimilarity
	result.MatchedFile = bestFile

	// Classify based on thresholds
	if bestSimilarity >= c.config.VerbatimThreshold {
		result.Classification = ClassificationVerbatim
	} else if bestSimilarity >= c.config.ModifiedThreshold {
		result.Classification = ClassificationModified
	} else {
		result.Classification = ClassificationFabricated
	}

	return result
}

// hasSuggestionPhrase checks if context contains a suggestion phrase.
func (c *CodeSnippetChecker) hasSuggestionPhrase(context string) bool {
	lowerContext := strings.ToLower(context)

	// Only check the last N lines based on config
	lines := strings.Split(lowerContext, "\n")
	if len(lines) > c.config.SuggestionContextLines {
		lines = lines[len(lines)-c.config.SuggestionContextLines:]
	}
	relevantContext := strings.Join(lines, "\n")

	for _, phrase := range suggestionPhrases {
		if strings.Contains(relevantContext, phrase) {
			return true
		}
	}
	return false
}

// normalize normalizes code based on the configured normalization level.
func (c *CodeSnippetChecker) normalize(code string) string {
	switch c.config.NormalizationLevel {
	case NormNone:
		return code
	case NormWhitespace:
		return normalizeWhitespace(code)
	case NormFull:
		return normalizeFull(code)
	default:
		return normalizeWhitespace(code)
	}
}

// normalizeWhitespace normalizes whitespace while preserving comments.
func normalizeWhitespace(code string) string {
	// Replace tabs with spaces
	code = strings.ReplaceAll(code, "\t", " ")

	// Collapse multiple spaces to single space
	for strings.Contains(code, "  ") {
		code = strings.ReplaceAll(code, "  ", " ")
	}

	// Normalize line endings
	code = strings.ReplaceAll(code, "\r\n", "\n")
	code = strings.ReplaceAll(code, "\r", "\n")

	// Trim whitespace from each line
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}

	// Remove empty lines at start and end
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}

// normalizeFull normalizes whitespace and removes comments.
// Note: This is a best-effort removal that may incorrectly strip # or //
// inside strings. Use NormWhitespace (default) to preserve such content.
func normalizeFull(code string) string {
	// First normalize whitespace
	code = normalizeWhitespace(code)

	// Remove single-line comments (// and #)
	// Note: This is naive and will incorrectly strip # or // inside strings.
	// A proper implementation would need language-aware parsing.
	lines := strings.Split(code, "\n")
	var result []string
	for _, line := range lines {
		// Remove // comments (Go, JS, Java, etc.)
		// Skip if // appears inside quotes (simple heuristic: count quotes before //)
		if idx := findCommentIndex(line, "//"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		// Remove # comments (Python, Ruby, Bash)
		// Skip if # appears inside quotes
		if idx := findCommentIndex(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// findCommentIndex finds the index of a comment marker, ignoring those inside strings.
// This is a simple heuristic that counts quote characters before the marker.
// Returns -1 if the marker is not found or is inside a string.
func findCommentIndex(line, marker string) int {
	idx := strings.Index(line, marker)
	if idx == -1 {
		return -1
	}

	// Count unescaped double and single quotes before the marker
	prefix := line[:idx]
	doubleQuotes := 0
	singleQuotes := 0

	for i := 0; i < len(prefix); i++ {
		if prefix[i] == '"' && (i == 0 || prefix[i-1] != '\\') {
			doubleQuotes++
		} else if prefix[i] == '\'' && (i == 0 || prefix[i-1] != '\\') {
			singleQuotes++
		}
	}

	// If we have an odd number of quotes, the marker is inside a string
	if doubleQuotes%2 == 1 || singleQuotes%2 == 1 {
		return -1
	}

	return idx
}

// findBestSimilarity finds the best similarity between snippet and file content.
// Uses multi-stage matching for performance.
func (c *CodeSnippetChecker) findBestSimilarity(normalizedSnippet string, fileContent string) float64 {
	// Normalize file content
	normalizedFile := c.normalize(fileContent)

	// Stage 1: Exact substring check (O(n))
	if strings.Contains(normalizedFile, normalizedSnippet) {
		return 1.0
	}

	// Stage 2: Check if file is significantly smaller than snippet
	// If the file is much smaller, similarity will be low anyway
	if len(normalizedFile) < len(normalizedSnippet)/2 {
		return 0.0
	}

	// Stage 3: LCS-based similarity
	// For large files, use sliding window approach
	if len(normalizedFile) > 10000 {
		return c.slidingWindowSimilarity(normalizedSnippet, normalizedFile)
	}

	return lcsSimilarity(normalizedSnippet, normalizedFile)
}

// slidingWindowSimilarity computes similarity using overlapping windows for large files.
func (c *CodeSnippetChecker) slidingWindowSimilarity(snippet string, fileContent string) float64 {
	windowSize := len(snippet) * 2 // Window is 2x snippet size
	if windowSize < 500 {
		windowSize = 500
	}

	step := windowSize / 2 // 50% overlap
	var bestSimilarity float64

	for i := 0; i < len(fileContent); i += step {
		end := i + windowSize
		if end > len(fileContent) {
			end = len(fileContent)
		}

		window := fileContent[i:end]
		similarity := lcsSimilarity(snippet, window)
		if similarity > bestSimilarity {
			bestSimilarity = similarity
		}

		// Early exit if good match found
		if bestSimilarity >= 0.9 {
			break
		}

		// Stop if we've reached the end
		if end == len(fileContent) {
			break
		}
	}

	return bestSimilarity
}

// lcsSimilarity computes similarity using Longest Common Subsequence.
// Returns value between 0.0 (no similarity) and 1.0 (identical).
func lcsSimilarity(a, b string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	lcsLen := longestCommonSubsequence(a, b)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	return float64(lcsLen) / float64(maxLen)
}

// longestCommonSubsequence computes the LCS length between two strings.
// Uses optimized space complexity O(min(m, n)).
func longestCommonSubsequence(a, b string) int {
	// Ensure a is the shorter string to minimize space
	if len(a) > len(b) {
		a, b = b, a
	}

	m, n := len(a), len(b)
	if m == 0 {
		return 0
	}

	// Only need two rows at a time
	prev := make([]int, m+1)
	curr := make([]int, m+1)

	for j := 1; j <= n; j++ {
		for i := 1; i <= m; i++ {
			if a[i-1] == b[j-1] {
				curr[i] = prev[i-1] + 1
			} else {
				if prev[i] > curr[i-1] {
					curr[i] = prev[i]
				} else {
					curr[i] = curr[i-1]
				}
			}
		}
		// Swap rows
		prev, curr = curr, prev
	}

	return prev[m]
}

// generateViolation creates a violation for a code block match.
func (c *CodeSnippetChecker) generateViolation(match CodeBlockMatch) *Violation {
	var severity Severity
	var message string
	var code string

	switch match.Classification {
	case ClassificationModified:
		severity = SeverityWarning
		code = "FABRICATED_CODE_MODIFIED"
		message = fmt.Sprintf(
			"Code snippet appears modified from actual code (%.0f%% similar to %s)",
			match.Similarity*100,
			match.MatchedFile,
		)
		if match.MatchedFile == "" {
			message = fmt.Sprintf(
				"Code snippet appears modified (%.0f%% similar to evidence)",
				match.Similarity*100,
			)
		}
	case ClassificationFabricated:
		severity = SeverityCritical
		code = "FABRICATED_CODE_INVENTED"
		message = "Code snippet does not exist in the codebase"
		if match.Similarity > 0 {
			message = fmt.Sprintf(
				"Code snippet appears fabricated (only %.0f%% similar to any evidence)",
				match.Similarity*100,
			)
		}
	default:
		return nil
	}

	// Truncate evidence for display
	evidence := match.Block.Content
	if len(evidence) > 200 {
		evidence = evidence[:200] + "..."
	}

	suggestion := "Use verbatim code from file contents or clearly mark code as suggestions/examples"

	return &Violation{
		Type:           ViolationFabricatedCode,
		Severity:       severity,
		Code:           code,
		Message:        message,
		Evidence:       evidence,
		Expected:       "Code matching actual file contents",
		LocationOffset: match.Block.Position,
		Suggestion:     suggestion,
	}
}
