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
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
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
	if ctx == nil || tokensToFree <= 0 {
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

	// Sort by age descending (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].age > entries[i].age {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

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
	if ctx == nil || tokensToFree <= 0 {
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

	// Sort by relevance ascending (lowest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].relevance < entries[i].relevance {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

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
}

// NewHybridPolicy creates a hybrid policy with default weights.
func NewHybridPolicy() *HybridPolicy {
	return &HybridPolicy{
		RelevanceWeight: 0.6,
	}
}

// Name returns "hybrid".
func (p *HybridPolicy) Name() string {
	return "hybrid"
}

// SelectForEviction uses a combined score of relevance and recency.
func (p *HybridPolicy) SelectForEviction(ctx *agent.AssembledContext, tokensToFree int, metadata *EvictionMetadata) []string {
	if ctx == nil || tokensToFree <= 0 {
		return nil
	}

	type scored struct {
		id     string
		score  float64
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

		// Normalize age to 0-1 (assuming max age of 100 steps)
		normalizedAge := float64(age) / 100.0
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

	// Sort by score descending (highest eviction score first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].score > entries[i].score {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

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
func GetEvictionPolicy(name string) EvictionPolicy {
	switch name {
	case "lru":
		return &LRUPolicy{}
	case "relevance":
		return &RelevancePolicy{}
	case "hybrid":
		return NewHybridPolicy()
	default:
		return NewHybridPolicy() // Default to hybrid
	}
}
