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
	"fmt"
	"sort"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

// EvictionPolicy defines the interface for context eviction strategies.
type EvictionPolicy interface {
	// Name returns the policy name.
	Name() string

	// SelectForEviction selects entries to evict to free the target tokens.
	//
	// Inputs:
	//   ctx - Current context
	//   tokensToFree - Target tokens to free
	//   metadata - Additional metadata about entries
	//
	// Outputs:
	//   []string - IDs of entries to evict
	SelectForEviction(ctx *agent.AssembledContext, tokensToFree int, metadata *EvictionMetadata) []string
}

// EvictionMetadata provides additional information for eviction decisions.
type EvictionMetadata struct {
	// AddedAt maps entry ID to the step when it was added.
	AddedAt map[string]int

	// LastAccessed maps entry ID to the last step it was accessed.
	LastAccessed map[string]int

	// CurrentStep is the current agent step.
	CurrentStep int

	// ProtectedIDs are entries that should not be evicted.
	ProtectedIDs map[string]bool
}

// NewEvictionMetadata creates a new metadata instance.
func NewEvictionMetadata() *EvictionMetadata {
	return &EvictionMetadata{
		AddedAt:      make(map[string]int),
		LastAccessed: make(map[string]int),
		ProtectedIDs: make(map[string]bool),
	}
}

// LRUPolicy implements least-recently-used eviction.
type LRUPolicy struct{}

// Name returns "lru".
func (p *LRUPolicy) Name() string {
	return "lru"
}

// SelectForEviction selects the oldest entries.
func (p *LRUPolicy) SelectForEviction(ctx *agent.AssembledContext, tokensToFree int, metadata *EvictionMetadata) []string {
	if ctx == nil || tokensToFree <= 0 || metadata == nil {
		return nil
	}

	type scored struct {
		id     string
		age    int
		tokens int
	}

	var entries []scored
	for _, entry := range ctx.CodeContext {
		if metadata.ProtectedIDs[entry.ID] {
			continue
		}

		addedAt := metadata.AddedAt[entry.ID]
		lastAccessed := metadata.LastAccessed[entry.ID]
		if lastAccessed == 0 {
			lastAccessed = addedAt
		}

		age := metadata.CurrentStep - lastAccessed
		entries = append(entries, scored{
			id:     entry.ID,
			age:    age,
			tokens: entry.Tokens,
		})
	}

	// Sort by age descending (oldest first) - O(n log n)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].age > entries[j].age
	})

	// Select entries until target freed
	var result []string
	freed := 0
	for _, e := range entries {
		if freed >= tokensToFree {
			break
		}
		result = append(result, e.id)
		freed += e.tokens
	}

	return result
}

// RelevancePolicy implements relevance-based eviction.
type RelevancePolicy struct{}

// Name returns "relevance".
func (p *RelevancePolicy) Name() string {
	return "relevance"
}

// SelectForEviction selects the lowest relevance entries.
func (p *RelevancePolicy) SelectForEviction(ctx *agent.AssembledContext, tokensToFree int, metadata *EvictionMetadata) []string {
	if ctx == nil || tokensToFree <= 0 || metadata == nil {
		return nil
	}

	type scored struct {
		id        string
		relevance float64
		tokens    int
	}

	var entries []scored
	for _, entry := range ctx.CodeContext {
		if metadata.ProtectedIDs[entry.ID] {
			continue
		}

		entries = append(entries, scored{
			id:        entry.ID,
			relevance: entry.Relevance,
			tokens:    entry.Tokens,
		})
	}

	// Sort by relevance ascending (lowest first) - O(n log n)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relevance < entries[j].relevance
	})

	// Select entries until target freed
	var result []string
	freed := 0
	for _, e := range entries {
		if freed >= tokensToFree {
			break
		}
		result = append(result, e.id)
		freed += e.tokens
	}

	return result
}

// HybridPolicy combines LRU and relevance.
type HybridPolicy struct {
	// RelevanceWeight is the weight for relevance (0.0-1.0).
	// The remaining weight goes to recency.
	RelevanceWeight float64

	// MaxAgeSteps is the maximum age (in steps) used for normalization.
	// Ages beyond this are capped at 1.0. Default is 100.
	MaxAgeSteps int
}

// NewHybridPolicy creates a hybrid policy with default weights.
func NewHybridPolicy() *HybridPolicy {
	return &HybridPolicy{
		RelevanceWeight: 0.6,
		MaxAgeSteps:     100,
	}
}

// Name returns "hybrid".
func (p *HybridPolicy) Name() string {
	return "hybrid"
}

// SelectForEviction uses a combined score of relevance and recency.
func (p *HybridPolicy) SelectForEviction(ctx *agent.AssembledContext, tokensToFree int, metadata *EvictionMetadata) []string {
	if ctx == nil || tokensToFree <= 0 || metadata == nil {
		return nil
	}

	type scored struct {
		id     string
		score  float64
		tokens int
	}

	// Use configured max age or default to 100
	maxAge := p.MaxAgeSteps
	if maxAge <= 0 {
		maxAge = 100
	}

	var entries []scored
	for _, entry := range ctx.CodeContext {
		if metadata.ProtectedIDs[entry.ID] {
			continue
		}

		addedAt := metadata.AddedAt[entry.ID]
		lastAccessed := metadata.LastAccessed[entry.ID]
		if lastAccessed == 0 {
			lastAccessed = addedAt
		}

		age := metadata.CurrentStep - lastAccessed

		// Normalize age to 0-1 using configurable max age
		normalizedAge := float64(age) / float64(maxAge)
		if normalizedAge > 1.0 {
			normalizedAge = 1.0
		}

		// Higher score = more likely to evict
		// Low relevance + high age = high eviction score
		recencyScore := normalizedAge
		relevanceScore := 1.0 - entry.Relevance // Invert so low relevance = high score

		score := p.RelevanceWeight*relevanceScore + (1.0-p.RelevanceWeight)*recencyScore

		entries = append(entries, scored{
			id:     entry.ID,
			score:  score,
			tokens: entry.Tokens,
		})
	}

	// Sort by score descending (highest eviction score first) - O(n log n)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	// Select entries until target freed
	var result []string
	freed := 0
	for _, e := range entries {
		if freed >= tokensToFree {
			break
		}
		result = append(result, e.id)
		freed += e.tokens
	}

	return result
}

// GetEvictionPolicy returns an eviction policy by name.
//
// Returns an error if the policy name is not recognized.
// Valid names: "lru", "relevance", "hybrid"
func GetEvictionPolicy(name string) (EvictionPolicy, error) {
	switch name {
	case "lru":
		return &LRUPolicy{}, nil
	case "relevance":
		return &RelevancePolicy{}, nil
	case "hybrid":
		return NewHybridPolicy(), nil
	default:
		return nil, fmt.Errorf("unknown eviction policy: %q (valid: lru, relevance, hybrid)", name)
	}
}

// MustGetEvictionPolicy returns an eviction policy by name, defaulting to hybrid
// if the name is not recognized.
//
// Use GetEvictionPolicy if you want to handle unknown policy names explicitly.
func MustGetEvictionPolicy(name string) EvictionPolicy {
	policy, err := GetEvictionPolicy(name)
	if err != nil {
		return NewHybridPolicy()
	}
	return policy
}
