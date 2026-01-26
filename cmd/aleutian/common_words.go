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
	_ "embed"
	"strings"
)

//go:embed common_words.txt
var commonWordsData string

// GetCommonWords returns a map of the most common English words.
// These are used by the spell corrector to avoid false corrections.
// Words are mapped to frequency rank (1 = most common).
func GetCommonWords() map[string]int {
	words := make(map[string]int)
	lines := strings.Split(commonWordsData, "\n")
	for i, line := range lines {
		word := strings.TrimSpace(strings.ToLower(line))
		if word != "" && !strings.HasPrefix(word, "#") {
			words[word] = i + 1 // Rank as frequency (lower = more common)
		}
	}
	return words
}
