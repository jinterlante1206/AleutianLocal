// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// RetrieverConfig configures the memory retriever.
type RetrieverConfig struct {
	// MaxResults is the default limit for retrieval queries.
	MaxResults int

	// ConfidenceWeight is the weight for confidence in ranking (0-1).
	ConfidenceWeight float64

	// RecencyWeight is the weight for recency in ranking (0-1).
	RecencyWeight float64

	// RelevanceWeight is the weight for semantic relevance in ranking (0-1).
	RelevanceWeight float64

	// RecencyDecayDays is how many days before recency score drops to 0.5.
	RecencyDecayDays float64

	// AutoMarkUsed automatically updates lastUsed on retrieval.
	AutoMarkUsed bool
}

// DefaultRetrieverConfig returns sensible defaults.
func DefaultRetrieverConfig() RetrieverConfig {
	return RetrieverConfig{
		MaxResults:       10,
		ConfidenceWeight: 0.3,
		RecencyWeight:    0.2,
		RelevanceWeight:  0.5,
		RecencyDecayDays: 30,
		AutoMarkUsed:     true,
	}
}

// MemoryRetriever handles semantic retrieval of code memories.
type MemoryRetriever struct {
	client    *weaviate.Client
	store     *MemoryStore
	config    RetrieverConfig
	dataSpace string
}

// NewMemoryRetriever creates a new memory retriever.
//
// Description:
//
//	Creates a MemoryRetriever for semantic search and ranked retrieval.
//
// Inputs:
//
//	client - Weaviate client. Must not be nil.
//	store - MemoryStore for updates. Must not be nil.
//	dataSpace - Project isolation key.
//
// Outputs:
//
//	*MemoryRetriever - The configured retriever
//	error - Non-nil if client or store is nil
//
// Thread Safety: Retrieve methods are safe for concurrent use.
func NewMemoryRetriever(client *weaviate.Client, store *MemoryStore, dataSpace string) (*MemoryRetriever, error) {
	if client == nil {
		return nil, errors.New("client must not be nil")
	}
	if store == nil {
		return nil, errors.New("store must not be nil")
	}
	return &MemoryRetriever{
		client:    client,
		store:     store,
		config:    DefaultRetrieverConfig(),
		dataSpace: dataSpace,
	}, nil
}

// NewMemoryRetrieverWithConfig creates a retriever with custom configuration.
func NewMemoryRetrieverWithConfig(client *weaviate.Client, store *MemoryStore, dataSpace string, config RetrieverConfig) *MemoryRetriever {
	return &MemoryRetriever{
		client:    client,
		store:     store,
		config:    config,
		dataSpace: dataSpace,
	}
}

// Retrieve performs semantic retrieval of relevant memories.
//
// Description:
//
//	Searches for memories semantically similar to the query, filters by scope,
//	and ranks results by confidence × recency × relevance. Automatically
//	updates lastUsed timestamps for retrieved memories.
//
// Inputs:
//
//	ctx - Context for cancellation
//	opts - Retrieval options including query, scope, and limits
//
// Outputs:
//
//	[]RetrieveResult - Ranked results with scores
//	error - Non-nil if search fails
func (r *MemoryRetriever) Retrieve(ctx context.Context, opts RetrieveOptions) ([]RetrieveResult, error) {
	if opts.Query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = r.config.MaxResults
	}

	// Build where filter
	operands := []*filters.WhereBuilder{
		filters.Where().
			WithPath([]string{"dataSpace"}).
			WithOperator(filters.Equal).
			WithValueString(r.dataSpace),
	}

	if !opts.IncludeArchived {
		operands = append(operands, filters.Where().
			WithPath([]string{"status"}).
			WithOperator(filters.Equal).
			WithValueString(string(StatusActive)))
	}

	if opts.MinConfidence > 0 {
		operands = append(operands, filters.Where().
			WithPath([]string{"confidence"}).
			WithOperator(filters.GreaterThanEqual).
			WithValueNumber(opts.MinConfidence))
	}

	whereFilter := filters.Where().
		WithOperator(filters.And).
		WithOperands(operands)

	// Build nearText for semantic search
	nearText := r.client.GraphQL().NearTextArgBuilder().
		WithConcepts([]string{opts.Query})

	// Add scope to concepts if provided for better matching
	if opts.Scope != "" {
		nearText = nearText.WithConcepts([]string{opts.Query, opts.Scope})
	}

	fields := []graphql.Field{
		{Name: "memoryId"},
		{Name: "content"},
		{Name: "memoryType"},
		{Name: "scope"},
		{Name: "confidence"},
		{Name: "source"},
		{Name: "createdAt"},
		{Name: "lastUsed"},
		{Name: "useCount"},
		{Name: "dataSpace"},
		{Name: "status"},
		{Name: "_additional { certainty distance }"},
	}

	// Fetch more than needed for re-ranking
	fetchLimit := limit * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	result, err := r.client.GraphQL().Get().
		WithClassName(CodeMemoryClassName).
		WithFields(fields...).
		WithWhere(whereFilter).
		WithNearText(nearText).
		WithLimit(fetchLimit).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

	if result.Errors != nil && len(result.Errors) > 0 {
		return nil, fmt.Errorf("search error: %s", result.Errors[0].Message)
	}

	// Parse and rank results
	results, err := r.parseAndRankResults(result, opts.Scope)
	if err != nil {
		return nil, err
	}

	// Limit to requested count
	if len(results) > limit {
		results = results[:limit]
	}

	// Mark memories as used
	if r.config.AutoMarkUsed {
		for _, res := range results {
			if err := r.store.MarkUsed(ctx, res.Memory.MemoryID); err != nil {
				slog.Warn("Failed to mark memory as used",
					"memory_id", res.Memory.MemoryID,
					"error", err)
			}
		}
	}

	slog.Info("Retrieved memories",
		"query", opts.Query,
		"scope", opts.Scope,
		"count", len(results))

	return results, nil
}

// RetrieveForFiles retrieves memories relevant to specific files.
//
// Description:
//
//	Convenience method that searches for memories matching the given
//	file paths. Checks each memory's scope against the files.
//
// Inputs:
//
//	ctx - Context for cancellation
//	query - Semantic search query
//	files - File paths to check scopes against
//	limit - Maximum results
//
// Outputs:
//
//	[]RetrieveResult - Ranked results relevant to the files
//	error - Non-nil if search fails
func (r *MemoryRetriever) RetrieveForFiles(ctx context.Context, query string, files []string, limit int) ([]RetrieveResult, error) {
	if len(files) == 0 {
		return r.Retrieve(ctx, RetrieveOptions{
			Query: query,
			Limit: limit,
		})
	}

	// Combine file paths into scope for better matching
	scope := files[0]
	if len(files) > 1 {
		// Find common prefix
		scope = commonPrefix(files)
	}

	return r.Retrieve(ctx, RetrieveOptions{
		Query: query,
		Scope: scope,
		Limit: limit,
	})
}

// parseAndRankResults parses results and calculates combined scores.
func (r *MemoryRetriever) parseAndRankResults(result *models.GraphQLResponse, scope string) ([]RetrieveResult, error) {
	data, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		return []RetrieveResult{}, nil
	}

	objects, ok := data[CodeMemoryClassName].([]interface{})
	if !ok {
		return []RetrieveResult{}, nil
	}

	results := make([]RetrieveResult, 0, len(objects))
	now := time.Now().UnixMilli()

	for _, obj := range objects {
		m, ok := obj.(map[string]interface{})
		if !ok {
			continue // skip malformed objects
		}

		memory := CodeMemory{
			MemoryID:   getString(m, "memoryId"),
			Content:    getString(m, "content"),
			MemoryType: MemoryType(getString(m, "memoryType")),
			Scope:      getString(m, "scope"),
			Confidence: getFloat64(m, "confidence"),
			Source:     MemorySource(getString(m, "source")),
			UseCount:   getInt(m, "useCount"),
			DataSpace:  getString(m, "dataSpace"),
			Status:     MemoryStatus(getString(m, "status")),
		}

		if createdStr := getString(m, "createdAt"); createdStr != "" {
			if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
				memory.CreatedAt = t.UnixMilli()
			}
		}

		if lastUsedStr := getString(m, "lastUsed"); lastUsedStr != "" {
			if t, err := time.Parse(time.RFC3339, lastUsedStr); err == nil {
				memory.LastUsed = t.UnixMilli()
			}
		}

		// Extract relevance score from _additional
		relevance := 0.5 // default
		if additional, ok := m["_additional"].(map[string]interface{}); ok {
			if certainty, ok := additional["certainty"].(float64); ok {
				relevance = certainty
			}
		}

		// Calculate combined score
		score := r.calculateScore(memory, relevance, now, scope)

		results = append(results, RetrieveResult{
			Memory: memory,
			Score:  score,
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// calculateScore computes the combined ranking score.
func (r *MemoryRetriever) calculateScore(memory CodeMemory, relevance float64, now int64, scope string) float64 {
	// Confidence score (0-1)
	confidenceScore := memory.Confidence

	// Recency score (0-1) with exponential decay
	daysSinceUse := (time.Duration(now-memory.LastUsed) * time.Millisecond).Hours() / 24
	recencyScore := math.Exp(-daysSinceUse / r.config.RecencyDecayDays)

	// Relevance score (0-1) from semantic search
	relevanceScore := relevance

	// Scope boost (1.0-1.5) if scope matches
	scopeBoost := 1.0
	if scope != "" && matchScope(memory.Scope, scope) {
		scopeBoost = 1.5
	}

	// Combined weighted score
	score := (r.config.ConfidenceWeight*confidenceScore +
		r.config.RecencyWeight*recencyScore +
		r.config.RelevanceWeight*relevanceScore) * scopeBoost

	return score
}

// matchScope checks if a memory scope matches a target path.
func matchScope(memoryScope, targetPath string) bool {
	// Simple glob matching
	if memoryScope == "*" {
		return true
	}

	// Check if target path contains the scope pattern
	// This is a simplified check - a full implementation would use glob matching
	if len(memoryScope) > 0 && memoryScope[len(memoryScope)-1] == '*' {
		prefix := memoryScope[:len(memoryScope)-1]
		return len(targetPath) >= len(prefix) && targetPath[:len(prefix)] == prefix
	}

	return memoryScope == targetPath
}

// commonPrefix finds the common path prefix among file paths.
func commonPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return paths[0]
	}

	prefix := paths[0]
	for _, p := range paths[1:] {
		// Find character-level common prefix first
		minLen := len(prefix)
		if len(p) < minLen {
			minLen = len(p)
		}

		commonLen := 0
		for i := 0; i < minLen; i++ {
			if prefix[i] == p[i] {
				commonLen = i + 1
			} else {
				break
			}
		}

		prefix = prefix[:commonLen]

		// Trim to last path separator to get directory boundary
		lastSep := -1
		for i := len(prefix) - 1; i >= 0; i-- {
			if prefix[i] == '/' {
				lastSep = i
				break
			}
		}

		if lastSep >= 0 {
			prefix = prefix[:lastSep]
		} else {
			prefix = ""
		}
	}

	return prefix
}
