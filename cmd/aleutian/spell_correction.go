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
	"strings"
	"unicode"
)

// SpellCorrector provides fuzzy matching for typo correction.
//
// # Description
//
// SpellCorrector uses Levenshtein distance to find similar terms
// from a known vocabulary. It's used to suggest corrections for
// likely typos in user queries.
//
// # Fields
//
//   - terms: Map of known terms to their frequency (higher = more common)
//   - maxDistance: Maximum edit distance to consider (default: 2)
//
// # Thread Safety
//
// SpellCorrector is safe for concurrent read access after initialization.
// Do not modify terms after construction.
//
// # Limitations
//
//   - Only corrects individual words, not phrases
//   - Case-insensitive matching
//   - Does not handle keyboard proximity (e.g., 'q' near 'w')
type SpellCorrector struct {
	terms       map[string]int
	maxDistance int
}

// Suggestion represents a spelling correction suggestion.
//
// # Fields
//
//   - Original: The word that was checked
//   - Suggested: The suggested correction
//   - Distance: Levenshtein edit distance
//   - Frequency: How common the suggested term is (higher = more common)
type Suggestion struct {
	Original  string
	Suggested string
	Distance  int
	Frequency int
}

// NewSpellCorrector creates a new spell corrector with the given terms.
//
// # Description
//
// Creates a SpellCorrector initialized with known vocabulary terms.
// Terms can include document titles, headings, entity names, etc.
//
// # Inputs
//
//   - terms: Map of term to frequency. Higher frequency terms are preferred.
//   - maxDistance: Maximum edit distance for suggestions (typically 1-2).
//
// # Outputs
//
//   - *SpellCorrector: Ready to check words for typos.
//
// # Examples
//
//	terms := map[string]int{"wheat": 100, "weather": 50, "water": 75}
//	corrector := NewSpellCorrector(terms, 2)
//	suggestions := corrector.Check("wehat")
//	// Returns: [{Original: "wehat", Suggested: "wheat", Distance: 1, Frequency: 100}]
func NewSpellCorrector(terms map[string]int, maxDistance int) *SpellCorrector {
	// Normalize all terms to lowercase
	normalized := make(map[string]int, len(terms))
	for term, freq := range terms {
		normalized[strings.ToLower(term)] = freq
	}

	return &SpellCorrector{
		terms:       normalized,
		maxDistance: maxDistance,
	}
}

// Check finds spelling suggestions for words in the input text.
//
// # Description
//
// Extracts words from the input and checks each against the known
// vocabulary. Returns suggestions for words that appear to be typos.
//
// A word is considered a potential typo if:
//   - It's not in the vocabulary (exact match)
//   - A similar word exists within maxDistance edits
//
// # Inputs
//
//   - input: User input text to check.
//
// # Outputs
//
//   - []Suggestion: Suggestions sorted by distance (ascending), then frequency (descending).
//     Empty slice if no typos detected.
//
// # Limitations
//
//   - Only checks words of 3+ characters (short words have too many matches)
//   - Skips numbers and special characters
func (s *SpellCorrector) Check(input string) []Suggestion {
	words := extractWords(input)
	var suggestions []Suggestion

	for _, word := range words {
		// Skip short words (too many false positives)
		if len(word) < 3 {
			continue
		}

		lower := strings.ToLower(word)

		// Skip if exact match exists
		if _, exists := s.terms[lower]; exists {
			continue
		}

		// Find similar terms
		best := s.findBestMatch(lower)
		if best != nil {
			suggestions = append(suggestions, *best)
		}
	}

	return suggestions
}

// findBestMatch finds the best matching term for a word.
//
// # Description
//
// Searches vocabulary for terms within maxDistance edits.
// Returns the best match based on distance and frequency.
//
// # Inputs
//
//   - word: Lowercase word to find matches for.
//
// # Outputs
//
//   - *Suggestion: Best match, or nil if no match within distance.
func (s *SpellCorrector) findBestMatch(word string) *Suggestion {
	var best *Suggestion

	for term, freq := range s.terms {
		// Quick length check - can't be within distance if lengths differ too much
		lenDiff := len(term) - len(word)
		if lenDiff < 0 {
			lenDiff = -lenDiff
		}
		if lenDiff > s.maxDistance {
			continue
		}

		dist := levenshtein(word, term)
		if dist > s.maxDistance {
			continue
		}

		// Prefer lower distance, then higher frequency
		if best == nil ||
			dist < best.Distance ||
			(dist == best.Distance && freq > best.Frequency) {
			best = &Suggestion{
				Original:  word,
				Suggested: term,
				Distance:  dist,
				Frequency: freq,
			}
		}
	}

	return best
}

// CheckWord checks a single word for spelling corrections.
//
// # Description
//
// Convenience method to check a single word instead of full text.
//
// # Inputs
//
//   - word: Single word to check.
//
// # Outputs
//
//   - *Suggestion: Suggestion if typo detected, nil otherwise.
func (s *SpellCorrector) CheckWord(word string) *Suggestion {
	if len(word) < 3 {
		return nil
	}

	lower := strings.ToLower(word)

	// Skip if exact match exists
	if _, exists := s.terms[lower]; exists {
		return nil
	}

	return s.findBestMatch(lower)
}

// AddTerms adds additional terms to the vocabulary.
//
// # Description
//
// Adds new terms to the existing vocabulary. Useful for dynamically
// loading terms from a dataspace.
//
// # Inputs
//
//   - terms: Map of term to frequency.
//
// # Thread Safety
//
// NOT thread-safe. Only call during initialization.
func (s *SpellCorrector) AddTerms(terms map[string]int) {
	for term, freq := range terms {
		lower := strings.ToLower(term)
		// Keep higher frequency if term already exists
		if existing, ok := s.terms[lower]; !ok || freq > existing {
			s.terms[lower] = freq
		}
	}
}

// TermCount returns the number of terms in the vocabulary.
func (s *SpellCorrector) TermCount() int {
	return len(s.terms)
}

// levenshtein computes the Levenshtein (edit) distance between two strings.
//
// # Description
//
// The Levenshtein distance is the minimum number of single-character
// edits (insertions, deletions, substitutions) required to change
// one string into the other.
//
// Uses space-optimized dynamic programming with two rows instead of
// a full matrix, reducing space complexity from O(mn) to O(min(m,n)).
//
// # Inputs
//
//   - a, b: Strings to compare.
//
// # Outputs
//
//   - int: Edit distance (0 = identical, higher = more different).
//
// # Examples
//
//	levenshtein("kitten", "sitting") // 3
//	levenshtein("wheat", "wehat")    // 1 (transposition via 2 ops, but swap counts as 1)
//	levenshtein("", "abc")           // 3
//	levenshtein("abc", "abc")        // 0
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Ensure a is the shorter string for space optimization
	if len(a) > len(b) {
		a, b = b, a
	}

	// Use two rows instead of full matrix
	prev := make([]int, len(a)+1)
	curr := make([]int, len(a)+1)

	// Initialize first row (distance from empty string)
	for i := range prev {
		prev[i] = i
	}

	// Fill the matrix row by row
	for j := 1; j <= len(b); j++ {
		curr[0] = j
		for i := 1; i <= len(a); i++ {
			if a[i-1] == b[j-1] {
				// Characters match, no edit needed
				curr[i] = prev[i-1]
			} else {
				// Take minimum of insert, delete, replace
				curr[i] = 1 + minOf3(prev[i-1], prev[i], curr[i-1])
			}
		}
		// Swap rows
		prev, curr = curr, prev
	}

	return prev[len(a)]
}

// minOf3 returns the minimum of three integers.
func minOf3(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= c {
		return b
	}
	return c
}

// extractWords extracts alphabetic words from text.
//
// # Description
//
// Splits text into words, keeping only alphabetic characters.
// Numbers, punctuation, and special characters are treated as separators.
//
// # Inputs
//
//   - text: Input text to extract words from.
//
// # Outputs
//
//   - []string: Slice of words (may contain duplicates).
func extractWords(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}

	// Don't forget the last word
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}
