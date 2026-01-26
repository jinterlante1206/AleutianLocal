// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"testing"
)

// =============================================================================
// Levenshtein Distance Tests
// =============================================================================

func TestLevenshtein_IdenticalStrings(t *testing.T) {
	tests := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"single char", "a"},
		{"word", "wheat"},
		{"phrase", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist := levenshtein(tt.s, tt.s)
			if dist != 0 {
				t.Errorf("levenshtein(%q, %q) = %d, want 0", tt.s, tt.s, dist)
			}
		})
	}
}

func TestLevenshtein_EmptyStrings(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"both empty", "", "", 0},
		{"a empty", "", "abc", 3},
		{"b empty", "abc", "", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist := levenshtein(tt.a, tt.b)
			if dist != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, dist, tt.want)
			}
		})
	}
}

func TestLevenshtein_SingleEdits(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"substitution", "cat", "bat", 1},
		{"insertion", "cat", "cart", 1},
		{"deletion", "cart", "cat", 1},
		{"transposition needs 2", "ab", "ba", 2}, // Levenshtein doesn't have transposition
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist := levenshtein(tt.a, tt.b)
			if dist != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, dist, tt.want)
			}
		})
	}
}

func TestLevenshtein_TypoExamples(t *testing.T) {
	tests := []struct {
		name    string
		typo    string
		correct string
		want    int
	}{
		{"wehat -> wheat", "wehat", "wheat", 2}, // Transposition = 2 in Levenshtein
		{"teh -> the", "teh", "the", 2},
		{"hte -> the", "hte", "the", 2},
		{"wheta -> wheat", "wheta", "wheat", 2},
		{"whaet -> wheat", "whaet", "wheat", 2},
		{"wheatt -> wheat", "wheatt", "wheat", 1}, // Extra letter
		{"whet -> wheat", "whet", "wheat", 1},     // Missing letter
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist := levenshtein(tt.typo, tt.correct)
			if dist != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.typo, tt.correct, dist, tt.want)
			}
		})
	}
}

func TestLevenshtein_Symmetry(t *testing.T) {
	pairs := [][2]string{
		{"kitten", "sitting"},
		{"wheat", "wehat"},
		{"hello", "world"},
	}

	for _, pair := range pairs {
		d1 := levenshtein(pair[0], pair[1])
		d2 := levenshtein(pair[1], pair[0])
		if d1 != d2 {
			t.Errorf("levenshtein(%q, %q) = %d, but levenshtein(%q, %q) = %d",
				pair[0], pair[1], d1, pair[1], pair[0], d2)
		}
	}
}

// =============================================================================
// SpellCorrector Tests
// =============================================================================

func TestSpellCorrector_ExactMatch_NoSuggestion(t *testing.T) {
	terms := map[string]int{"wheat": 100, "barley": 50}
	corrector := NewSpellCorrector(terms, 2)

	suggestions := corrector.Check("Tell me about wheat")
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for exact match, got %v", suggestions)
	}
}

func TestSpellCorrector_Typo_SingleEdit(t *testing.T) {
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	suggestions := corrector.Check("Tell me about whet") // Missing 'a'
	if len(suggestions) == 0 {
		t.Fatal("expected suggestion for typo 'whet'")
	}
	if suggestions[0].Suggested != "wheat" {
		t.Errorf("expected suggestion 'wheat', got %q", suggestions[0].Suggested)
	}
	if suggestions[0].Distance != 1 {
		t.Errorf("expected distance 1, got %d", suggestions[0].Distance)
	}
}

func TestSpellCorrector_Typo_DoubleEdit(t *testing.T) {
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	suggestions := corrector.Check("Tell me about wehat") // Transposition
	if len(suggestions) == 0 {
		t.Fatal("expected suggestion for typo 'wehat'")
	}
	if suggestions[0].Suggested != "wheat" {
		t.Errorf("expected suggestion 'wheat', got %q", suggestions[0].Suggested)
	}
	if suggestions[0].Distance != 2 {
		t.Errorf("expected distance 2, got %d", suggestions[0].Distance)
	}
}

func TestSpellCorrector_NoMatch_BeyondMaxDistance(t *testing.T) {
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	// "water" has distance 3 from "wheat"
	suggestions := corrector.Check("Tell me about water")
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for 'water' (distance 3), got %v", suggestions)
	}
}

func TestSpellCorrector_CaseInsensitive(t *testing.T) {
	terms := map[string]int{"Wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	suggestions := corrector.Check("Tell me about WHET")
	if len(suggestions) == 0 {
		t.Fatal("expected case-insensitive match")
	}
	if suggestions[0].Suggested != "wheat" {
		t.Errorf("expected 'wheat' (lowercase), got %q", suggestions[0].Suggested)
	}
}

func TestSpellCorrector_ShortWords_Skipped(t *testing.T) {
	terms := map[string]int{"to": 100, "of": 100, "an": 100}
	corrector := NewSpellCorrector(terms, 2)

	// Short words (< 3 chars) should be skipped - "ot" and "fo" are 2 chars
	suggestions := corrector.Check("ot fo sentence")
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for short words, got %v", suggestions)
	}
}

func TestSpellCorrector_PreferHigherFrequency(t *testing.T) {
	terms := map[string]int{
		"wheat": 100,
		"whet":  50, // Same distance from "whea" (both distance 1)
	}
	corrector := NewSpellCorrector(terms, 2)

	// "whea" is distance 1 from both "wheat" (add 't') and "whet" (add 't')
	// But wheat has higher frequency so should be preferred
	suggestions := corrector.Check("Tell me about whea")
	if len(suggestions) == 0 {
		t.Fatal("expected suggestion")
	}
	// With same distance, higher frequency should win
	if suggestions[0].Suggested != "wheat" {
		t.Errorf("expected 'wheat' (higher frequency), got %q", suggestions[0].Suggested)
	}
}

func TestSpellCorrector_PreferLowerDistance(t *testing.T) {
	terms := map[string]int{
		"wheat":   50,  // Distance 1 from "whea"
		"weather": 100, // Distance 4 from "whea" (higher freq but farther)
	}
	corrector := NewSpellCorrector(terms, 2)

	suggestions := corrector.Check("Tell me about whea")
	if len(suggestions) == 0 {
		t.Fatal("expected suggestion")
	}
	// wheat has lower distance, should be preferred over higher-freq weather
	if suggestions[0].Suggested != "wheat" {
		t.Errorf("expected 'wheat' (lower distance), got %q", suggestions[0].Suggested)
	}
}

func TestSpellCorrector_CheckWord(t *testing.T) {
	terms := map[string]int{"wheat": 100}
	corrector := NewSpellCorrector(terms, 2)

	suggestion := corrector.CheckWord("whet")
	if suggestion == nil {
		t.Fatal("expected suggestion")
	}
	if suggestion.Suggested != "wheat" {
		t.Errorf("expected 'wheat', got %q", suggestion.Suggested)
	}
}

func TestSpellCorrector_AddTerms(t *testing.T) {
	corrector := NewSpellCorrector(map[string]int{"wheat": 100}, 2)

	// Add new terms
	corrector.AddTerms(map[string]int{"barley": 50, "oats": 75})

	if corrector.TermCount() != 3 {
		t.Errorf("expected 3 terms, got %d", corrector.TermCount())
	}

	// New term should be found
	suggestion := corrector.CheckWord("barly")
	if suggestion == nil || suggestion.Suggested != "barley" {
		t.Errorf("expected to find 'barley', got %v", suggestion)
	}
}

// =============================================================================
// extractWords Tests
// =============================================================================

func TestExtractWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"simple", "hello world", []string{"hello", "world"}},
		{"with punctuation", "Hello, world!", []string{"Hello", "world"}},
		{"with numbers", "file123.txt", []string{"file", "txt"}},
		{"multiple spaces", "hello   world", []string{"hello", "world"}},
		{"empty", "", nil},
		{"only punctuation", "!@#$%", nil},
		{"mixed", "Tell me about wheat.", []string{"Tell", "me", "about", "wheat"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractWords(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("extractWords(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractWords(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// minOf3 Tests
// =============================================================================

func TestMinOf3(t *testing.T) {
	tests := []struct {
		a, b, c int
		want    int
	}{
		{1, 2, 3, 1},
		{3, 2, 1, 1},
		{2, 1, 3, 1},
		{5, 5, 5, 5},
		{0, 1, 2, 0},
		{-1, 0, 1, -1},
	}

	for _, tt := range tests {
		got := minOf3(tt.a, tt.b, tt.c)
		if got != tt.want {
			t.Errorf("minOf3(%d, %d, %d) = %d, want %d", tt.a, tt.b, tt.c, got, tt.want)
		}
	}
}
