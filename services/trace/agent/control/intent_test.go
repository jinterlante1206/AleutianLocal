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
	"sync"
	"testing"
	"time"
)

func TestIntentClassifier_HighConfidenceIntent(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "let me start by",
			content: "Let me start by exploring the codebase",
			want:    true,
		},
		{
			name:    "i'll begin with",
			content: "I'll begin with checking the main.go file",
			want:    true,
		},
		{
			name:    "first, i need to",
			content: "First, I need to understand the structure",
			want:    true,
		},
		{
			name:    "i will explore",
			content: "I will explore the repository",
			want:    true,
		},
		{
			name:    "let me check",
			content: "Let me check the configuration",
			want:    true,
		},
		{
			name:    "let me search",
			content: "Let me search for relevant files",
			want:    true,
		},
		{
			name:    "i'll look for",
			content: "I'll look for the entry point",
			want:    true,
		},
		{
			name:    "let me find",
			content: "Let me find the database connection code",
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsIntent(tc.content)
			if result.IsIntent != tc.want {
				t.Errorf("IsIntent(%q) = %v (score=%d, reason=%s), want %v",
					tc.content, result.IsIntent, result.Score, result.Reason, tc.want)
			}
		})
	}
}

func TestIntentClassifier_NegativePatterns(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	// These should NOT be classified as intent (negative patterns override)
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "here's what I found",
			content: "Here's what I found in the codebase: there are 5 main packages.",
		},
		{
			name:    "based on my analysis",
			content: "Based on my analysis, the main entry point is in cmd/server/main.go",
		},
		{
			name:    "the answer is",
			content: "The answer is that the function uses a recursive algorithm.",
		},
		{
			name:    "in summary",
			content: "In summary, the architecture follows a microservices pattern.",
		},
		{
			name:    "code block present",
			content: "Let me start by showing you:\n```go\nfunc main() {}\n```",
		},
		{
			name:    "file reference with line",
			content: "I'll help you understand: the code is in main.go:42",
		},
		{
			name:    "inline code with path",
			content: "Let me check `services/api/handler.go` for more details.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsIntent(tc.content)
			if result.IsIntent {
				t.Errorf("IsIntent(%q) should be false (negative pattern), got true (score=%d, reason=%s)",
					tc.content, result.Score, result.Reason)
			}
		})
	}
}

func TestIntentClassifier_SubstantiveContentMarkers(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	// Content with substantive markers should NOT be classified as intent
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "numbered list",
			content: "Let me explain:\n1. First point\n2. Second point",
		},
		{
			name:    "bullet list",
			content: "I'll help you understand:\n- Item A\n- Item B",
		},
		{
			name:    "markdown headers",
			content: "Let me start by explaining:\n## Overview\nThe system...",
		},
		{
			name:    "bold text",
			content: "I'll analyze the code: The **main function** is responsible for...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsIntent(tc.content)
			if result.IsIntent {
				t.Errorf("IsIntent(%q) should be false (substantive content), got true (score=%d, reason=%s)",
					tc.content, result.Score, result.Reason)
			}
		})
	}
}

func TestIntentClassifier_AmbiguousPatterns(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "i'll help you with colon and content",
			content: "I'll help you: here is the answer to your question about the code.",
			want:    false, // Colon + content = answer
		},
		{
			name:    "i'll help you without colon - short no punctuation",
			content: "I'll help you with that",
			want:    false, // Too short + low score + has punctuation = not intent
		},
		{
			name:    "i'll help you with high confidence phrase",
			content: "I'll help you. Let me start by exploring the codebase",
			want:    true, // High confidence phrase triggers intent
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsIntent(tc.content)
			if result.IsIntent != tc.want {
				t.Errorf("IsIntent(%q) = %v (score=%d, reason=%s), want %v",
					tc.content, result.IsIntent, result.Score, result.Reason, tc.want)
			}
		})
	}
}

func TestIntentClassifier_LongResponseNotIntent(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	// Long responses (>500 chars) should never be classified as intent
	// This content has no intent phrases, just filler to exceed the length
	longContent := "This is a response about the codebase that provides detailed information. " +
		"The system consists of multiple packages organized in a microservices architecture. " +
		"Each service handles a specific domain responsibility and communicates via gRPC. " +
		"The main entry point initializes the configuration and starts the HTTP server. " +
		"Error handling is centralized through a middleware layer that logs and traces errors. " +
		"Database connections are pooled and managed by a connection manager component. " +
		"This detailed response exceeds the maximum intent length threshold of 500 characters."

	if len(longContent) <= 500 {
		t.Fatalf("Test content should be >500 chars, got %d", len(longContent))
	}

	result := classifier.IsIntent(longContent)
	if result.IsIntent {
		t.Errorf("Long content (%d chars) should not be classified as intent, got true (score=%d, reason=%s)",
			len(longContent), result.Score, result.Reason)
	}
}

func TestIntentClassifier_CacheHit(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = true
	config.CacheTTL = 1 * time.Minute
	classifier := NewIntentClassifier(config)

	content := "Let me start by exploring the codebase"

	// First call - cache miss
	result1 := classifier.IsIntent(content)
	if result1.Cached {
		t.Error("First call should not be cached")
	}

	// Second call - cache hit
	result2 := classifier.IsIntent(content)
	if !result2.Cached {
		t.Error("Second call should be cached")
	}

	// Results should match
	if result1.IsIntent != result2.IsIntent || result1.Score != result2.Score {
		t.Errorf("Cached result mismatch: first=%v/%d, second=%v/%d",
			result1.IsIntent, result1.Score, result2.IsIntent, result2.Score)
	}
}

func TestIntentClassifier_CacheDisabled(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = false
	classifier := NewIntentClassifier(config)

	content := "Let me start by exploring the codebase"

	result1 := classifier.IsIntent(content)
	result2 := classifier.IsIntent(content)

	// Neither should be cached
	if result1.Cached || result2.Cached {
		t.Error("No results should be cached when cache is disabled")
	}
}

func TestIntentClassifier_CacheEviction(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = true
	config.CacheMaxSize = 2
	classifier := NewIntentClassifier(config)

	// Fill cache
	classifier.IsIntent("Content 1")
	classifier.IsIntent("Content 2")
	classifier.IsIntent("Content 3") // Should evict "Content 1"

	// Verify "Content 1" was evicted (would need internal access to verify)
	// For now, just ensure no panic and cache still works
	result := classifier.IsIntent("Content 2")
	if !result.Cached {
		t.Error("Content 2 should still be cached")
	}
}

func TestIntentClassifier_ClearCache(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = true
	classifier := NewIntentClassifier(config)

	content := "Let me start by exploring"
	classifier.IsIntent(content)

	// Verify cached
	result1 := classifier.IsIntent(content)
	if !result1.Cached {
		t.Error("Should be cached before clear")
	}

	// Clear cache
	classifier.ClearCache()

	// Verify not cached
	result2 := classifier.IsIntent(content)
	if result2.Cached {
		t.Error("Should not be cached after clear")
	}
}

func TestIntentClassifier_CacheStats(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = true
	config.CacheMaxSize = 100
	classifier := NewIntentClassifier(config)

	size, capacity := classifier.CacheStats()
	if size != 0 || capacity != 100 {
		t.Errorf("Initial stats: size=%d (want 0), capacity=%d (want 100)", size, capacity)
	}

	classifier.IsIntent("Content 1")
	classifier.IsIntent("Content 2")

	size, capacity = classifier.CacheStats()
	if size != 2 || capacity != 100 {
		t.Errorf("After 2 items: size=%d (want 2), capacity=%d (want 100)", size, capacity)
	}
}

func TestIntentClassifier_ThreadSafety(t *testing.T) {
	config := DefaultIntentConfig()
	config.EnableCache = true
	classifier := NewIntentClassifier(config)

	contents := []string{
		"Let me start by exploring",
		"I'll begin with checking",
		"Here's what I found",
		"Based on my analysis",
		"First, I need to understand",
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := contents[idx%len(contents)]
			classifier.IsIntent(content)
		}(i)
	}
	wg.Wait()

	// Should not panic, verify cache is consistent
	size, _ := classifier.CacheStats()
	if size > len(contents) {
		t.Errorf("Cache size %d exceeds unique content count %d", size, len(contents))
	}
}

func TestIntentClassifier_ScoreThresholds(t *testing.T) {
	classifier := NewIntentClassifier(DefaultIntentConfig())

	tests := []struct {
		name        string
		content     string
		wantIntent  bool
		minScore    int
		description string
	}{
		{
			name:        "high score short response",
			content:     "Let me start by exploring",
			wantIntent:  true,
			minScore:    3,
			description: "Short + high confidence phrase",
		},
		{
			name:        "medium score short response",
			content:     "I'm going to check",
			wantIntent:  true, // Score 4 (two medium phrases) + short = intent
			minScore:    4,
			description: "Multiple medium confidence phrases",
		},
		{
			name:        "low score alone not enough",
			content:     "I will do it.",
			wantIntent:  false, // Score 1 + terminal punctuation = not intent
			minScore:    1,
			description: "Low confidence with terminal punctuation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifier.IsIntent(tc.content)
			if result.IsIntent != tc.wantIntent {
				t.Errorf("IsIntent(%q) = %v (score=%d, reason=%s), want %v; %s",
					tc.content, result.IsIntent, result.Score, result.Reason, tc.wantIntent, tc.description)
			}
			if result.Score < tc.minScore {
				t.Errorf("Score for %q = %d, want at least %d",
					tc.content, result.Score, tc.minScore)
			}
		})
	}
}

func TestDefaultIntentConfig(t *testing.T) {
	config := DefaultIntentConfig()

	if config.MaxIntentLength != 500 {
		t.Errorf("MaxIntentLength = %d, want 500", config.MaxIntentLength)
	}
	if config.HighScoreThreshold != 3 {
		t.Errorf("HighScoreThreshold = %d, want 3", config.HighScoreThreshold)
	}
	if config.ShortResponseLen != 300 {
		t.Errorf("ShortResponseLen = %d, want 300", config.ShortResponseLen)
	}
	if !config.EnableCache {
		t.Error("EnableCache should be true by default")
	}
	if config.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5m", config.CacheTTL)
	}
	if config.CacheMaxSize != 1000 {
		t.Errorf("CacheMaxSize = %d, want 1000", config.CacheMaxSize)
	}
}
