package main

import (
	"strconv"
	"testing"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
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
