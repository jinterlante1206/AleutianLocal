// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"sort"
	"strings"
)

// RetrieverConfig configures the hierarchical retriever.
type RetrieverConfig struct {
	// MaxPackages is the maximum packages to consider at Level 1.
	MaxPackages int `json:"max_packages"`

	// MaxFilesPerPkg is the maximum files per package at Level 2.
	MaxFilesPerPkg int `json:"max_files_per_pkg"`

	// MaxSymbolsPerFile is the maximum symbols per file at Level 3.
	MaxSymbolsPerFile int `json:"max_symbols_per_file"`

	// MinRelevanceScore is the minimum score to include a result.
	MinRelevanceScore float64 `json:"min_relevance_score"`
}

// DefaultRetrieverConfig returns sensible defaults for retrieval.
func DefaultRetrieverConfig() RetrieverConfig {
	return RetrieverConfig{
		MaxPackages:       10,
		MaxFilesPerPkg:    5,
		MaxSymbolsPerFile: 10,
		MinRelevanceScore: 0.3,
	}
}

// RetrievalResult contains the results of hierarchical retrieval.
type RetrievalResult struct {
	// Summaries are the relevant summaries found.
	Summaries []*Summary `json:"summaries"`

	// Path is the drill-down path taken.
	// Example: ["project", "pkg/auth", "pkg/auth/validator.go"]
	Path []string `json:"path"`

	// QueryType is how the query was classified.
	QueryType QueryType `json:"query_type"`

	// Classification is the full classification result.
	Classification *QueryClassification `json:"classification,omitempty"`

	// PartialMatch indicates degraded results (some levels failed).
	PartialMatch bool `json:"partial_match"`

	// Warnings contains any issues encountered during retrieval.
	Warnings []string `json:"warnings,omitempty"`

	// TokensUsed is the estimated context tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// LevelsSearched tracks which levels were searched.
	LevelsSearched []int `json:"levels_searched"`
}

// HierarchicalRetriever performs hierarchical context retrieval.
//
// Thread Safety: Safe for concurrent use.
type HierarchicalRetriever struct {
	cache      *SummaryCache
	classifier QueryClassifier
	config     RetrieverConfig
}

// NewHierarchicalRetriever creates a new retriever.
//
// Inputs:
//   - cache: The summary cache to query.
//   - classifier: The query classifier.
//   - config: Retriever configuration.
//
// Outputs:
//   - *HierarchicalRetriever: A new retriever instance.
func NewHierarchicalRetriever(cache *SummaryCache, classifier QueryClassifier, config RetrieverConfig) *HierarchicalRetriever {
	return &HierarchicalRetriever{
		cache:      cache,
		classifier: classifier,
		config:     config,
	}
}

// Retrieve performs hierarchical retrieval for a query.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - query: The user query.
//   - tokenBudget: Maximum tokens to consume.
//
// Outputs:
//   - *RetrievalResult: The retrieval results.
//   - error: Non-nil if retrieval fails completely.
func (r *HierarchicalRetriever) Retrieve(ctx context.Context, query string, tokenBudget int) (*RetrievalResult, error) {
	// Classify the query
	classification := r.classifier.Classify(query)

	result := &RetrievalResult{
		Summaries:      make([]*Summary, 0),
		Path:           make([]string, 0),
		QueryType:      classification.Type,
		Classification: classification,
		LevelsSearched: make([]int, 0),
	}

	// Route based on query type
	switch classification.Type {
	case QueryTypeOverview:
		return r.retrieveOverview(ctx, query, tokenBudget, result)
	case QueryTypeSpecific:
		return r.retrieveSpecific(ctx, query, tokenBudget, result, classification)
	case QueryTypeLocational:
		return r.retrieveLocational(ctx, query, tokenBudget, result, classification)
	default: // QueryTypeConceptual
		return r.retrieveHierarchical(ctx, query, tokenBudget, result)
	}
}

// retrieveOverview handles high-level overview queries.
// Uses only Level 0-1 summaries.
func (r *HierarchicalRetriever) retrieveOverview(
	ctx context.Context,
	query string,
	tokenBudget int,
	result *RetrievalResult,
) (*RetrievalResult, error) {
	// Get project summary (Level 0)
	projectSummaries := r.cache.GetByLevel(0)
	result.LevelsSearched = append(result.LevelsSearched, 0)

	for _, s := range projectSummaries {
		if r.matchesQuery(s, query) {
			result.Summaries = append(result.Summaries, s)
			result.Path = append(result.Path, s.ID)
			result.TokensUsed += r.estimateSummaryTokens(s)
		}
	}

	// Get top package summaries (Level 1)
	packageSummaries := r.cache.GetByLevel(1)
	result.LevelsSearched = append(result.LevelsSearched, 1)

	scored := r.scoreAndSort(packageSummaries, query)
	for i, item := range scored {
		if i >= r.config.MaxPackages {
			break
		}
		if result.TokensUsed+r.estimateSummaryTokens(item.summary) > tokenBudget {
			result.Warnings = append(result.Warnings, "token budget reached at package level")
			break
		}
		result.Summaries = append(result.Summaries, item.summary)
		result.TokensUsed += r.estimateSummaryTokens(item.summary)
	}

	return result, nil
}

// retrieveHierarchical handles conceptual queries with full drill-down.
func (r *HierarchicalRetriever) retrieveHierarchical(
	ctx context.Context,
	query string,
	tokenBudget int,
	result *RetrievalResult,
) (*RetrievalResult, error) {
	// Level 1: Find relevant packages
	packageSummaries := r.cache.GetByLevel(1)
	result.LevelsSearched = append(result.LevelsSearched, 1)

	scoredPackages := r.scoreAndSort(packageSummaries, query)
	if len(scoredPackages) == 0 {
		result.Warnings = append(result.Warnings, "no packages matched query")
		return result, nil
	}

	// Take top packages
	topPackages := make([]*Summary, 0)
	for i, item := range scoredPackages {
		if i >= r.config.MaxPackages || item.score < r.config.MinRelevanceScore {
			break
		}
		topPackages = append(topPackages, item.summary)
		result.Summaries = append(result.Summaries, item.summary)
		result.Path = append(result.Path, item.summary.ID)
		result.TokensUsed += r.estimateSummaryTokens(item.summary)
	}

	// Level 2: Drill down into files for each top package
	result.LevelsSearched = append(result.LevelsSearched, 2)
	fileSummaries := make([]*Summary, 0)

	for _, pkg := range topPackages {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		children := r.cache.GetChildren(pkg.ID)
		scored := r.scoreAndSort(children, query)

		for i, item := range scored {
			if i >= r.config.MaxFilesPerPkg || item.score < r.config.MinRelevanceScore {
				break
			}
			if result.TokensUsed+r.estimateSummaryTokens(item.summary) > tokenBudget {
				result.Warnings = append(result.Warnings, "token budget reached at file level")
				result.PartialMatch = true
				break
			}
			fileSummaries = append(fileSummaries, item.summary)
			result.Summaries = append(result.Summaries, item.summary)
			result.TokensUsed += r.estimateSummaryTokens(item.summary)
		}
	}

	// Level 3: Get function summaries for top files
	result.LevelsSearched = append(result.LevelsSearched, 3)

	for _, file := range fileSummaries {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		children := r.cache.GetChildren(file.ID)
		scored := r.scoreAndSort(children, query)

		for i, item := range scored {
			if i >= r.config.MaxSymbolsPerFile || item.score < r.config.MinRelevanceScore {
				break
			}
			if result.TokensUsed+r.estimateSummaryTokens(item.summary) > tokenBudget {
				result.Warnings = append(result.Warnings, "token budget reached at symbol level")
				result.PartialMatch = true
				break
			}
			result.Summaries = append(result.Summaries, item.summary)
			result.TokensUsed += r.estimateSummaryTokens(item.summary)
		}
	}

	return result, nil
}

// retrieveSpecific handles specific symbol lookup queries.
func (r *HierarchicalRetriever) retrieveSpecific(
	ctx context.Context,
	query string,
	tokenBudget int,
	result *RetrievalResult,
	classification *QueryClassification,
) (*RetrievalResult, error) {
	// Use extracted terms for direct lookup
	terms := classification.ExtractedTerms
	if len(terms) == 0 {
		// Fall back to word extraction from query
		terms = r.extractSearchTerms(query)
	}

	// Search all levels for matching summaries
	for level := 3; level >= 0; level-- {
		summaries := r.cache.GetByLevel(level)
		result.LevelsSearched = append(result.LevelsSearched, level)

		for _, s := range summaries {
			if r.containsAnyTerm(s, terms) {
				if result.TokensUsed+r.estimateSummaryTokens(s) > tokenBudget {
					result.Warnings = append(result.Warnings, "token budget reached")
					result.PartialMatch = true
					return result, nil
				}
				result.Summaries = append(result.Summaries, s)
				result.Path = append(result.Path, s.ID)
				result.TokensUsed += r.estimateSummaryTokens(s)
			}
		}
	}

	return result, nil
}

// retrieveLocational handles path-based lookup queries.
func (r *HierarchicalRetriever) retrieveLocational(
	ctx context.Context,
	query string,
	tokenBudget int,
	result *RetrievalResult,
	classification *QueryClassification,
) (*RetrievalResult, error) {
	// Extract path from classification or query
	var targetPath string
	if len(classification.ExtractedTerms) > 0 {
		targetPath = classification.ExtractedTerms[0]
	}

	// If no path extracted, search by keyword
	if targetPath == "" {
		return r.retrieveHierarchical(ctx, query, tokenBudget, result)
	}

	// Look for summaries matching the path
	for level := 1; level <= 3; level++ {
		summaries := r.cache.GetByLevel(level)
		result.LevelsSearched = append(result.LevelsSearched, level)

		for _, s := range summaries {
			if strings.Contains(strings.ToLower(s.ID), strings.ToLower(targetPath)) {
				if result.TokensUsed+r.estimateSummaryTokens(s) > tokenBudget {
					result.Warnings = append(result.Warnings, "token budget reached")
					result.PartialMatch = true
					return result, nil
				}
				result.Summaries = append(result.Summaries, s)
				result.Path = append(result.Path, s.ID)
				result.TokensUsed += r.estimateSummaryTokens(s)

				// Also get children
				children := r.cache.GetChildren(s.ID)
				for _, child := range children {
					if result.TokensUsed+r.estimateSummaryTokens(child) <= tokenBudget {
						result.Summaries = append(result.Summaries, child)
						result.TokensUsed += r.estimateSummaryTokens(child)
					}
				}
			}
		}
	}

	return result, nil
}

// scoredSummary pairs a summary with its relevance score.
type scoredSummary struct {
	summary *Summary
	score   float64
}

// scoreAndSort scores summaries by relevance and returns sorted results.
func (r *HierarchicalRetriever) scoreAndSort(summaries []*Summary, query string) []scoredSummary {
	scored := make([]scoredSummary, 0, len(summaries))
	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	for _, s := range summaries {
		score := r.calculateRelevance(s, queryLower, queryWords)
		if score > 0 {
			scored = append(scored, scoredSummary{summary: s, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	return scored
}

// calculateRelevance calculates a relevance score for a summary.
func (r *HierarchicalRetriever) calculateRelevance(s *Summary, queryLower string, queryWords []string) float64 {
	score := 0.0
	contentLower := strings.ToLower(s.Content)
	idLower := strings.ToLower(s.ID)

	// Check ID match
	for _, word := range queryWords {
		if strings.Contains(idLower, word) {
			score += 0.5
		}
	}

	// Check content match
	for _, word := range queryWords {
		if strings.Contains(contentLower, word) {
			score += 0.3
		}
	}

	// Check keyword match
	for _, kw := range s.Keywords {
		kwLower := strings.ToLower(kw)
		for _, word := range queryWords {
			if kwLower == word || strings.Contains(kwLower, word) {
				score += 0.4
			}
		}
	}

	// Boost for exact phrase match
	if strings.Contains(contentLower, queryLower) {
		score += 0.5
	}

	return score
}

// matchesQuery checks if a summary matches a query (simple check).
func (r *HierarchicalRetriever) matchesQuery(s *Summary, query string) bool {
	queryLower := strings.ToLower(query)
	return strings.Contains(strings.ToLower(s.Content), queryLower) ||
		strings.Contains(strings.ToLower(s.ID), queryLower)
}

// containsAnyTerm checks if a summary contains any of the given terms.
func (r *HierarchicalRetriever) containsAnyTerm(s *Summary, terms []string) bool {
	idLower := strings.ToLower(s.ID)
	contentLower := strings.ToLower(s.Content)

	for _, term := range terms {
		termLower := strings.ToLower(term)
		if strings.Contains(idLower, termLower) || strings.Contains(contentLower, termLower) {
			return true
		}
		for _, kw := range s.Keywords {
			if strings.EqualFold(kw, term) {
				return true
			}
		}
	}
	return false
}

// extractSearchTerms extracts significant words from a query.
func (r *HierarchicalRetriever) extractSearchTerms(query string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "with": true, "by": true, "from": true, "about": true,
		"where": true, "what": true, "how": true, "when": true, "why": true,
		"which": true, "who": true, "this": true, "that": true, "these": true,
		"show": true, "me": true, "find": true, "locate": true, "get": true,
	}

	words := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0)

	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:'\"")
		if len(word) > 2 && !stopWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}

// estimateSummaryTokens estimates tokens for a summary.
func (r *HierarchicalRetriever) estimateSummaryTokens(s *Summary) int {
	// Rough estimate: ~4 chars per token for English
	return len(s.Content)/4 + len(s.ID)/4 + len(s.Keywords)*2
}
