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
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

// AnchoredSynthesisConfig configures tool-anchored synthesis prompt building.
type AnchoredSynthesisConfig struct {
	// Enabled determines if anchored synthesis is active.
	Enabled bool

	// MaxEvidenceTokens is the maximum token budget for evidence in prompt.
	MaxEvidenceTokens int

	// MaxResultLength is the maximum length per individual result.
	MaxResultLength int

	// MinResultsToInclude ensures at least this many results are included.
	MinResultsToInclude int

	// RecencyWeight scales the recency score contribution (0.0-1.0).
	RecencyWeight float64

	// RelevanceWeight scales the relevance score contribution (0.0-1.0).
	RelevanceWeight float64

	// IncludeNegativeExamples adds "DO NOT" instructions to the prompt.
	IncludeNegativeExamples bool

	// EnforceLanguage adds language-specific guidance to the prompt.
	EnforceLanguage bool
}

// DefaultAnchoredSynthesisConfig returns sensible defaults.
//
// Outputs:
//
//	*AnchoredSynthesisConfig - The default configuration.
func DefaultAnchoredSynthesisConfig() *AnchoredSynthesisConfig {
	return &AnchoredSynthesisConfig{
		Enabled:                 true,
		MaxEvidenceTokens:       8000, // ~32KB of evidence
		MaxResultLength:         2000,
		MinResultsToInclude:     3,
		RecencyWeight:           0.5,
		RelevanceWeight:         0.5,
		IncludeNegativeExamples: true,
		EnforceLanguage:         true,
	}
}

// ScoredEvidence represents a tool result with computed scores.
type ScoredEvidence struct {
	// Index is the original position in the ToolResults slice.
	Index int

	// InvocationID links to the original tool invocation.
	InvocationID string

	// Output is the tool result content.
	Output string

	// RecencyScore ranges from 0.0 (oldest) to 0.5 (most recent).
	RecencyScore float64

	// RelevanceScore ranges from 0.0 (not relevant) to 0.5 (highly relevant).
	RelevanceScore float64

	// TotalScore is RecencyScore + RelevanceScore.
	TotalScore float64

	// EstimatedTokens is the approximate token count for this result.
	EstimatedTokens int
}

// AnchoredSynthesisBuilder builds tool-anchored synthesis prompts.
//
// This interface allows phases to use anchored synthesis without
// depending on the concrete implementation.
//
// Thread Safety: Implementations must be safe for concurrent use.
type AnchoredSynthesisBuilder interface {
	// BuildAnchoredSynthesisPrompt creates a synthesis prompt grounded in evidence.
	//
	// Inputs:
	//   ctx - Context for cancellation and metrics.
	//   assembledCtx - The context with tool results and code context.
	//   userQuestion - The user's original question.
	//   projectLang - The detected project language (e.g., "go", "python").
	//
	// Outputs:
	//   string - The anchored synthesis prompt.
	BuildAnchoredSynthesisPrompt(ctx context.Context, assembledCtx *agent.AssembledContext, userQuestion string, projectLang string) string

	// ScoreEvidence scores all tool results for relevance and recency.
	//
	// Inputs:
	//   toolResults - The tool results to score.
	//   userQuestion - The user's question for relevance scoring.
	//
	// Outputs:
	//   []ScoredEvidence - Scored evidence sorted by total score (descending).
	ScoreEvidence(toolResults []agent.ToolResult, userQuestion string) []ScoredEvidence

	// SelectTopEvidence selects the best evidence within token budget.
	//
	// Inputs:
	//   scored - Scored evidence (already sorted).
	//   maxTokens - Maximum token budget.
	//
	// Outputs:
	//   []ScoredEvidence - Selected evidence within budget.
	SelectTopEvidence(scored []ScoredEvidence, maxTokens int) []ScoredEvidence
}

// DefaultAnchoredSynthesisBuilder implements AnchoredSynthesisBuilder.
type DefaultAnchoredSynthesisBuilder struct {
	config *AnchoredSynthesisConfig
}

// NewAnchoredSynthesisBuilder creates a new anchored synthesis builder.
//
// Inputs:
//
//	config - Configuration for the builder. Uses defaults if nil.
//
// Outputs:
//
//	*DefaultAnchoredSynthesisBuilder - The configured builder.
func NewAnchoredSynthesisBuilder(config *AnchoredSynthesisConfig) *DefaultAnchoredSynthesisBuilder {
	if config == nil {
		config = DefaultAnchoredSynthesisConfig()
	}
	return &DefaultAnchoredSynthesisBuilder{config: config}
}

// BuildAnchoredSynthesisPrompt implements AnchoredSynthesisBuilder.
//
// Description:
//
//	Creates a synthesis prompt that explicitly grounds the LLM in the
//	tool evidence, includes negative examples to prevent hallucination,
//	and enforces the project language.
//
// Inputs:
//
//	ctx - Context for cancellation and metrics.
//	assembledCtx - The context with tool results and code context.
//	userQuestion - The user's original question.
//	projectLang - The detected project language (e.g., "go", "python").
//
// Outputs:
//
//	string - The anchored synthesis prompt.
//
// Thread Safety: Safe for concurrent use (stateless function).
func (b *DefaultAnchoredSynthesisBuilder) BuildAnchoredSynthesisPrompt(ctx context.Context, assembledCtx *agent.AssembledContext, userQuestion string, projectLang string) string {
	if !b.config.Enabled {
		// Return basic prompt if not enabled
		return "Based on the tools you used and information you gathered, please provide a concise summary answering the user's original question. Focus on the key findings and insights."
	}

	var sb strings.Builder

	// Extract and score tool results
	var toolResults []agent.ToolResult
	if assembledCtx != nil {
		toolResults = assembledCtx.ToolResults
	}

	scored := b.ScoreEvidence(toolResults, userQuestion)
	selected := b.SelectTopEvidence(scored, b.config.MaxEvidenceTokens)

	// Record metrics
	RecordSynthesisPromptWithEvidence(ctx, len(selected), projectLang)
	for _, evidence := range selected {
		RecordEvidenceRelevanceScore(ctx, evidence.TotalScore)
	}

	// Language header
	if b.config.EnforceLanguage && projectLang != "" {
		sb.WriteString(fmt.Sprintf("PROJECT LANGUAGE: %s\n\n", strings.ToUpper(projectLang)))
	}

	// Evidence section
	if len(selected) > 0 {
		sb.WriteString("TOOL EVIDENCE (from your exploration):\n")
		sb.WriteString("─────────────────────────────────────\n")
		for i, evidence := range selected {
			sb.WriteString(fmt.Sprintf("\n[Evidence %d] (relevance: %.2f)\n", i+1, evidence.TotalScore))
			truncated := b.truncateOutput(evidence.Output, b.config.MaxResultLength)
			sb.WriteString(truncated)
			sb.WriteString("\n")
		}
		sb.WriteString("\n─────────────────────────────────────\n\n")
	} else {
		sb.WriteString("NOTE: No tool evidence available. Answer based on general knowledge but be clear about uncertainty.\n\n")
	}

	// Instructions
	sb.WriteString("INSTRUCTIONS:\n")
	sb.WriteString("- Base your response ONLY on the evidence above\n")
	sb.WriteString("- Cite specific tool outputs when making claims\n")
	sb.WriteString("- If the evidence doesn't answer the question, explicitly say so\n")
	sb.WriteString("- Use concrete file paths and code from the evidence, not generic examples\n")

	// Negative examples (DO NOT section)
	if b.config.IncludeNegativeExamples {
		sb.WriteString("\nDO NOT:\n")
		negatives := b.getNegativeExamples(projectLang)
		for _, neg := range negatives {
			sb.WriteString(fmt.Sprintf("- %s\n", neg))
		}
	}

	sb.WriteString("\nNow, please answer the user's question based on the evidence above.")

	return sb.String()
}

// ScoreEvidence implements AnchoredSynthesisBuilder.
//
// Description:
//
//	Scores tool results based on recency (position in slice) and relevance
//	(keyword match with user question, presence of file paths, code snippets).
//
// Inputs:
//
//	toolResults - The tool results to score.
//	userQuestion - The user's question for relevance scoring.
//
// Outputs:
//
//	[]ScoredEvidence - Scored evidence sorted by total score (descending).
//
// Thread Safety: Safe for concurrent use (stateless function).
func (b *DefaultAnchoredSynthesisBuilder) ScoreEvidence(toolResults []agent.ToolResult, userQuestion string) []ScoredEvidence {
	if len(toolResults) == 0 {
		return nil
	}

	// Extract keywords from user question for relevance scoring
	keywords := extractKeywords(userQuestion)

	scored := make([]ScoredEvidence, len(toolResults))
	maxIdx := float64(len(toolResults) - 1)

	for i, result := range toolResults {
		evidence := ScoredEvidence{
			Index:           i,
			InvocationID:    result.InvocationID,
			Output:          result.Output,
			EstimatedTokens: estimateTokens(result.Output),
		}

		// Recency score: 0.0 (oldest) to 0.5 (newest)
		// More recent results get higher scores
		if maxIdx > 0 {
			evidence.RecencyScore = (float64(i) / maxIdx) * b.config.RecencyWeight
		} else {
			evidence.RecencyScore = b.config.RecencyWeight // Single result gets full recency
		}

		// Relevance score: 0.0 to 0.5
		evidence.RelevanceScore = b.calculateRelevanceScore(result.Output, keywords)

		// Total score
		evidence.TotalScore = evidence.RecencyScore + evidence.RelevanceScore

		scored[i] = evidence
	}

	// Sort by total score (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].TotalScore > scored[j].TotalScore
	})

	return scored
}

// SelectTopEvidence implements AnchoredSynthesisBuilder.
//
// Description:
//
//	Selects evidence within the token budget, ensuring at least
//	MinResultsToInclude results are included.
//
// Inputs:
//
//	scored - Scored evidence (already sorted by score descending).
//	maxTokens - Maximum token budget.
//
// Outputs:
//
//	[]ScoredEvidence - Selected evidence within budget.
//
// Thread Safety: Safe for concurrent use (stateless function).
func (b *DefaultAnchoredSynthesisBuilder) SelectTopEvidence(scored []ScoredEvidence, maxTokens int) []ScoredEvidence {
	if len(scored) == 0 {
		return nil
	}

	var selected []ScoredEvidence
	usedTokens := 0

	for _, evidence := range scored {
		// Estimate tokens after truncation
		truncatedLen := len(evidence.Output)
		if truncatedLen > b.config.MaxResultLength {
			truncatedLen = b.config.MaxResultLength
		}
		tokens := truncatedLen / 4 // Rough estimate: 4 chars per token

		// Check if we can fit this result
		if usedTokens+tokens <= maxTokens || len(selected) < b.config.MinResultsToInclude {
			selected = append(selected, evidence)
			usedTokens += tokens
		}

		// Stop if we've exceeded budget and have minimum results
		if usedTokens >= maxTokens && len(selected) >= b.config.MinResultsToInclude {
			break
		}
	}

	return selected
}

// calculateRelevanceScore computes relevance based on keyword match and content features.
//
// Inputs:
//
//	output - The tool result output.
//	keywords - Keywords extracted from user question.
//
// Outputs:
//
//	float64 - Relevance score from 0.0 to 0.5.
func (b *DefaultAnchoredSynthesisBuilder) calculateRelevanceScore(output string, keywords []string) float64 {
	score := 0.0
	lowerOutput := strings.ToLower(output)

	// Keyword match: +0.2 if any keyword found
	for _, kw := range keywords {
		if strings.Contains(lowerOutput, strings.ToLower(kw)) {
			score += 0.2
			break
		}
	}

	// File path presence: +0.1
	if containsFilePath(output) {
		score += 0.1
	}

	// Code snippet presence: +0.1
	if containsCodeSnippet(output) {
		score += 0.1
	}

	// Successful result bonus: +0.1
	// (We can't easily tell if a result was successful from just the output,
	// but longer non-error outputs tend to be more useful)
	if len(output) > 100 && !strings.Contains(lowerOutput, "error") && !strings.Contains(lowerOutput, "not found") {
		score += 0.1
	}

	// Cap at RelevanceWeight
	if score > b.config.RelevanceWeight {
		score = b.config.RelevanceWeight
	}

	return score
}

// getNegativeExamples returns "DO NOT" examples based on project language.
//
// Inputs:
//
//	projectLang - The detected project language.
//
// Outputs:
//
//	[]string - List of things to avoid.
func (b *DefaultAnchoredSynthesisBuilder) getNegativeExamples(projectLang string) []string {
	common := []string{
		"Invent files or directories not shown in the evidence",
		"Describe generic patterns instead of project-specific code",
		"Make claims about code you haven't seen",
	}

	switch strings.ToLower(projectLang) {
	case "go":
		return append(common,
			"Use Python patterns (def, import os, pip, Flask, Django)",
			"Use JavaScript patterns (require(), npm, const, let, async/await)",
			"Reference node_modules, package.json, or __init__.py",
		)
	case "python":
		return append(common,
			"Use Go patterns (func, package main, go.mod, goroutine)",
			"Use JavaScript patterns (require(), npm, const, let)",
			"Reference go.mod, go.sum, or main.go unless in evidence",
		)
	case "javascript", "typescript":
		return append(common,
			"Use Go patterns (func, package main, go.mod)",
			"Use Python patterns (def, import os, pip, __init__.py)",
			"Reference go.mod, go.sum, requirements.txt, or setup.py",
		)
	default:
		return common
	}
}

// truncateOutput truncates output to maxLength with an indicator.
func (b *DefaultAnchoredSynthesisBuilder) truncateOutput(output string, maxLength int) string {
	if len(output) <= maxLength {
		return output
	}
	return output[:maxLength] + "\n... [truncated]"
}

// extractKeywords extracts meaningful keywords from a question.
func extractKeywords(question string) []string {
	// Simple keyword extraction: split by space, filter short words and stop words
	words := strings.Fields(strings.ToLower(question))

	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true,
		"what": true, "where": true, "how": true, "why": true, "when": true,
		"can": true, "could": true, "would": true, "should": true,
		"do": true, "does": true, "did": true, "have": true, "has": true,
		"be": true, "been": true, "being": true, "was": true, "were": true,
		"to": true, "for": true, "of": true, "in": true, "on": true, "at": true,
		"with": true, "by": true, "from": true, "about": true,
		"this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "i": true, "you": true, "we": true, "they": true,
		"my": true, "your": true, "our": true, "their": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"me": true, "tell": true, "show": true, "find": true, "please": true,
	}

	var keywords []string
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, "?.,!;:'\"")
		// Skip stop words and short words
		if len(word) >= 3 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// containsFilePath checks if output contains file path patterns.
func containsFilePath(output string) bool {
	// Look for common file path patterns
	patterns := []string{
		".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".h",
		".json", ".yaml", ".yml", ".md", ".txt",
		"/", "\\",
	}
	for _, p := range patterns {
		if strings.Contains(output, p) {
			return true
		}
	}
	return false
}

// containsCodeSnippet checks if output contains code-like content.
func containsCodeSnippet(output string) bool {
	// Look for code indicators
	indicators := []string{
		"func ", "def ", "function ", "class ",
		"package ", "import ", "from ",
		"const ", "let ", "var ",
		"type ", "struct ", "interface ",
		"return ", "if ", "for ", "while ",
	}
	lowerOutput := strings.ToLower(output)
	for _, ind := range indicators {
		if strings.Contains(lowerOutput, ind) {
			return true
		}
	}
	return false
}

// estimateTokens estimates token count from text.
func estimateTokens(text string) int {
	// Rough estimate: 4 characters per token
	return len(text) / 4
}

// ExtractUserQuestion finds the user's original question from context.
//
// Description:
//
//	Searches the conversation history for the first user message,
//	which typically contains the original question.
//
// Inputs:
//
//	assembledCtx - The context with conversation history.
//
// Outputs:
//
//	string - The user's original question, or empty string if not found.
func ExtractUserQuestion(assembledCtx *agent.AssembledContext) string {
	if assembledCtx == nil || len(assembledCtx.ConversationHistory) == 0 {
		return ""
	}

	// Find the first user message
	for _, msg := range assembledCtx.ConversationHistory {
		if msg.Role == "user" && msg.Content != "" {
			return msg.Content
		}
	}

	return ""
}
