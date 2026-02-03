// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package control provides control flow hardening for the agent loop.
//
// This package implements 5 layers of hardening to prevent:
//   - Premature completion (intent detection)
//   - Raw tool output leakage (parser hardening)
//   - Step exhaustion without synthesis (synthesis budget)
//   - Leaked markup in responses (output sanitization)
//   - Empty responses despite gathered information (buffer extraction)
package control

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"sync"
	"time"
)

// IntentClassifier detects when LLM outputs intent rather than answer.
//
// Description:
//
//	Analyzes LLM response text to determine if it represents a statement
//	of future intent ("I will explore...") vs an actual answer. Intent
//	statements should trigger continuation, not completion.
//
// Thread Safety: Safe for concurrent use (stateless methods, thread-safe cache).
type IntentClassifier struct {
	negativePatterns   []*regexp.Regexp // Patterns that indicate answer, not intent
	substantiveMarkers []*regexp.Regexp // Content markers indicating real answer
	highConfidence     []string
	mediumConfidence   []string
	lowConfidence      []string
	ambiguousPatterns  map[string]AmbiguousRule
	config             IntentConfig
	cache              *IntentCache // Cache for repeated classifications
}

// IntentConfig allows tuning intent detection behavior.
type IntentConfig struct {
	// MaxIntentLength is max chars for content to be considered intent (default: 500).
	MaxIntentLength int

	// HighScoreThreshold is score for high confidence (default: 3).
	HighScoreThreshold int

	// ShortResponseLen is "short" response threshold (default: 300).
	ShortResponseLen int

	// EnableCache enables caching of classification results.
	EnableCache bool

	// CacheTTL is TTL for cache entries.
	CacheTTL time.Duration

	// CacheMaxSize is max entries in cache (LRU eviction).
	CacheMaxSize int
}

// DefaultIntentConfig returns production defaults.
func DefaultIntentConfig() IntentConfig {
	return IntentConfig{
		MaxIntentLength:    500,
		HighScoreThreshold: 3,
		ShortResponseLen:   300,
		EnableCache:        true,
		CacheTTL:           5 * time.Minute,
		CacheMaxSize:       1000,
	}
}

// IntentCache caches classification results for unchanged responses.
//
// Thread Safety: Safe for concurrent use via RWMutex.
type IntentCache struct {
	mu      sync.RWMutex
	entries map[string]cachedResult
	order   []string // For LRU eviction
	maxSize int
}

type cachedResult struct {
	isIntent  bool
	score     int
	reason    string
	expiresAt time.Time
}

// AmbiguousRule defines how to handle ambiguous phrases.
type AmbiguousRule struct {
	// FollowedByColon: if true, phrase + colon = intent; false = answer
	FollowedByColon bool
}

// IntentResult contains the result of intent classification.
type IntentResult struct {
	// IsIntent is true if content is an intent statement, not an answer.
	IsIntent bool

	// Score is the confidence score (0-10).
	Score int

	// Reason explains the classification.
	Reason string

	// Cached indicates if result came from cache.
	Cached bool
}

// Package-level compiled regexes for intent detection (compiled once).
var (
	negativePatternStrings = []string{
		`(?i)^(here'?s?|this is) what I found`,
		`(?i)^based on (my |the )?analysis`,
		`(?i)^(the answer|in summary|to summarize)`,
		`(?i)I'll help you understand:\s*\S`,      // Colon followed by content
		"```",                                     // Code block
		`\[?[^\]]*\.(go|py|js|ts|rs|java):\d+\]?`, // File reference with line
		"`[^`]+/[^`]+`",                           // Inline code with path
	}

	substantiveMarkerStrings = []string{
		`(?m)^\s*\d+\.\s+\S`, // Numbered list
		`(?m)^\s*[-*]\s+\S`,  // Bullet list
		`(?m)^#{1,6}\s+\S`,   // Markdown headers
		`\*\*[^*]+\*\*`,      // Bold text (likely emphasis)
	}

	// Pre-compiled patterns (initialized in init)
	compiledNegativePatterns   []*regexp.Regexp
	compiledSubstantiveMarkers []*regexp.Regexp
	patternsOnce               sync.Once
)

// initPatterns compiles patterns once at first use.
func initPatterns() {
	patternsOnce.Do(func() {
		compiledNegativePatterns = make([]*regexp.Regexp, len(negativePatternStrings))
		for i, p := range negativePatternStrings {
			compiledNegativePatterns[i] = regexp.MustCompile(p)
		}

		compiledSubstantiveMarkers = make([]*regexp.Regexp, len(substantiveMarkerStrings))
		for i, p := range substantiveMarkerStrings {
			compiledSubstantiveMarkers[i] = regexp.MustCompile(p)
		}
	})
}

// NewIntentClassifier creates a classifier with default indicators.
//
// Inputs:
//
//	config - Configuration for intent detection behavior.
//
// Outputs:
//
//	*IntentClassifier - The configured classifier.
func NewIntentClassifier(config IntentConfig) *IntentClassifier {
	initPatterns()

	return &IntentClassifier{
		config:             config,
		negativePatterns:   compiledNegativePatterns,
		substantiveMarkers: compiledSubstantiveMarkers,
		highConfidence: []string{
			"let me start by",
			"i'll begin with",
			"first, i need to",
			"i will explore",
			"let me check",
			"let me search",
			"i'll look for",
			"let me find",
		},
		mediumConfidence: []string{
			"let me look at",
			"i'm going to",
			"starting with",
			"going to check",
			"i'll examine",
		},
		lowConfidence: []string{
			"i will",
			"let's",
			"i'll",
		},
		ambiguousPatterns: map[string]AmbiguousRule{
			"i'll help you": {
				FollowedByColon: false, // colon+content = answer, not intent
			},
		},
		cache: newIntentCache(config.CacheMaxSize),
	}
}

// IsIntent returns true if content appears to be intent, not answer.
//
// Inputs:
//
//	content - LLM response text to classify.
//
// Outputs:
//
//	IntentResult - Classification result with score and reason.
//
// Thread Safety: Safe for concurrent use.
func (c *IntentClassifier) IsIntent(content string) IntentResult {
	// Check cache first
	if c.config.EnableCache {
		if result, ok := c.checkCache(content); ok {
			return IntentResult{
				IsIntent: result.isIntent,
				Score:    result.score,
				Reason:   "cached: " + result.reason,
				Cached:   true,
			}
		}
	}

	// Early exit: long responses are likely answers
	if len(content) > c.config.MaxIntentLength {
		c.cacheResult(content, false, 0, "response too long")
		return IntentResult{
			IsIntent: false,
			Score:    0,
			Reason:   "response too long",
		}
	}

	lower := strings.ToLower(content)

	// Step 1: Check negative patterns (these override everything)
	for _, pattern := range c.negativePatterns {
		if pattern.MatchString(content) {
			c.cacheResult(content, false, 0, "negative pattern match")
			return IntentResult{
				IsIntent: false,
				Score:    0,
				Reason:   "negative pattern match",
			}
		}
	}

	// Step 2: Check for substantive content markers
	for _, pattern := range c.substantiveMarkers {
		if pattern.MatchString(content) {
			c.cacheResult(content, false, 0, "substantive content found")
			return IntentResult{
				IsIntent: false,
				Score:    0,
				Reason:   "substantive content found",
			}
		}
	}

	// Step 3: Score positive indicators
	score := 0

	for _, phrase := range c.highConfidence {
		if strings.Contains(lower, phrase) {
			score += 3
		}
	}
	for _, phrase := range c.mediumConfidence {
		if strings.Contains(lower, phrase) {
			score += 2
		}
	}
	for _, phrase := range c.lowConfidence {
		if strings.Contains(lower, phrase) {
			score += 1
		}
	}

	// Step 4: Handle ambiguous patterns
	for phrase, rule := range c.ambiguousPatterns {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			afterPhrase := content[idx+len(phrase):]
			hasColon := strings.HasPrefix(strings.TrimSpace(afterPhrase), ":")
			if hasColon != rule.FollowedByColon {
				// Pattern indicates answer, not intent
				c.cacheResult(content, false, score, "ambiguous pattern resolved to answer")
				return IntentResult{
					IsIntent: false,
					Score:    score,
					Reason:   "ambiguous pattern resolved to answer",
				}
			}
		}
	}

	// Step 5: Apply threshold logic
	isIntent := false
	reason := "below threshold"

	if score >= c.config.HighScoreThreshold && len(content) < c.config.ShortResponseLen {
		isIntent = true
		reason = fmt.Sprintf("high score (%d) + short length (%d)", score, len(content))
	} else if score >= 4 && len(content) < c.config.MaxIntentLength {
		isIntent = true
		reason = fmt.Sprintf("very high score (%d)", score)
	} else if score >= 2 && len(content) < 150 && !strings.ContainsAny(content, ".!?") {
		isIntent = true
		reason = "short, no terminal punctuation"
	}

	c.cacheResult(content, isIntent, score, reason)
	return IntentResult{
		IsIntent: isIntent,
		Score:    score,
		Reason:   reason,
	}
}

// checkCache looks up a cached result.
func (c *IntentClassifier) checkCache(content string) (cachedResult, bool) {
	if c.cache == nil {
		return cachedResult{}, false
	}

	c.cache.mu.RLock()
	key := hashContent(content)
	result, ok := c.cache.entries[key]
	c.cache.mu.RUnlock()

	if !ok {
		return cachedResult{}, false
	}

	// Check TTL expiration
	if time.Now().After(result.expiresAt) {
		// Expired - remove from cache (upgrade to write lock)
		c.cache.mu.Lock()
		delete(c.cache.entries, key)
		c.cache.mu.Unlock()
		return cachedResult{}, false
	}

	return result, true
}

// cacheResult stores a classification result.
func (c *IntentClassifier) cacheResult(content string, isIntent bool, score int, reason string) {
	if !c.config.EnableCache || c.cache == nil {
		return
	}

	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	key := hashContent(content)

	// Check if key already exists (update case - don't add to order again)
	_, exists := c.cache.entries[key]

	// LRU eviction if at capacity and this is a new key
	if !exists && len(c.cache.entries) >= c.cache.maxSize {
		// Evict oldest entries until we have space
		for len(c.cache.order) > 0 && len(c.cache.entries) >= c.cache.maxSize {
			oldest := c.cache.order[0]
			c.cache.order = c.cache.order[1:]
			delete(c.cache.entries, oldest)
		}
	}

	c.cache.entries[key] = cachedResult{
		isIntent:  isIntent,
		score:     score,
		reason:    reason,
		expiresAt: time.Now().Add(c.config.CacheTTL),
	}

	// Only append to order for new keys
	if !exists {
		c.cache.order = append(c.cache.order, key)
	}
}

// hashContent creates a hash key for cache lookup.
func hashContent(content string) string {
	h := fnv.New64a()
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum64())
}

// newIntentCache creates a new LRU cache.
func newIntentCache(maxSize int) *IntentCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &IntentCache{
		entries: make(map[string]cachedResult),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// ClearCache clears the intent cache.
//
// Thread Safety: Safe for concurrent use.
func (c *IntentClassifier) ClearCache() {
	if c.cache == nil {
		return
	}

	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.entries = make(map[string]cachedResult)
	c.cache.order = c.cache.order[:0]
}

// CacheStats returns cache statistics.
//
// Thread Safety: Safe for concurrent use.
func (c *IntentClassifier) CacheStats() (size int, capacity int) {
	if c.cache == nil {
		return 0, 0
	}

	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	return len(c.cache.entries), c.cache.maxSize
}
