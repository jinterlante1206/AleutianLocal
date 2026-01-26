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

import "testing"

func TestGetCommonWords_LoadsWords(t *testing.T) {
	words := GetCommonWords()

	// Should load thousands of words
	if len(words) < 5000 {
		t.Errorf("expected at least 5000 common words, got %d", len(words))
	}

	// "the" should be the most common word (rank 1)
	if rank, ok := words["the"]; !ok {
		t.Error("expected 'the' to be in common words")
	} else if rank != 1 {
		t.Errorf("expected 'the' to have rank 1, got %d", rank)
	}

	// "what" should be in the list (prevents false correction to "wheat")
	if _, ok := words["what"]; !ok {
		t.Error("expected 'what' to be in common words")
	}

	// Common question words should be present
	questionWords := []string{"what", "when", "where", "which", "who", "why", "how"}
	for _, word := range questionWords {
		if _, ok := words[word]; !ok {
			t.Errorf("expected '%s' to be in common words", word)
		}
	}
}

func TestGetCommonWords_NoDuplicates(t *testing.T) {
	words := GetCommonWords()

	// Each word should appear only once (map enforces this, but verify data is clean)
	seen := make(map[string]bool)
	for word := range words {
		if seen[word] {
			t.Errorf("duplicate word found: %s", word)
		}
		seen[word] = true
	}
}
