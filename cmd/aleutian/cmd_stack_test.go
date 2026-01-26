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
	"strconv"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/config"
)

func TestCalculateOptimizedEnv(t *testing.T) {
	// Test cases use config.BuiltInHardwareProfiles as the source of truth
	tests := []struct {
		name        string
		ramMB       int
		profileName string // Expected profile to be selected
	}{
		{"LowSpec", 8192, "low"},
		{"Standard", 24576, "standard"},
		{"Performance", 49152, "performance"},
		{"Ultra", 131072, "ultra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := calculateOptimizedEnv(tt.ramMB)
			expectedProfile := config.BuiltInHardwareProfiles[tt.profileName]

			if env["OLLAMA_MODEL"] != expectedProfile.OllamaModel {
				t.Errorf("Expected model %s for %d MB RAM, got %s",
					expectedProfile.OllamaModel, tt.ramMB, env["OLLAMA_MODEL"])
			}

			expectedTokens := strconv.Itoa(expectedProfile.MaxTokens)
			if env["LLM_DEFAULT_MAX_TOKENS"] != expectedTokens {
				t.Errorf("Expected %s tokens, got %s", expectedTokens, env["LLM_DEFAULT_MAX_TOKENS"])
			}
		})
	}
}
