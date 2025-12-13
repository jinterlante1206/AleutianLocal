package main

import (
	"testing"
)

func TestCalculateOptimizedEnv(t *testing.T) {
	tests := []struct {
		name     string
		ramMB    int
		expected string // Expected OLLAMA_MODEL value
	}{
		{"LowSpec", 8192, "gemma3:4b"},
		{"Standard", 24576, "gemma3:12b"},     // 24GB
		{"Performance", 49152, "gpt-oss:20b"}, // 48GB
		{"Ultra", 98304, "llama3:70b"},        // 96GB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := calculateOptimizedEnv(tt.ramMB)
			if env["OLLAMA_MODEL"] != tt.expected {
				t.Errorf("Expected model %s for %d MB RAM, got %s", tt.expected, tt.ramMB, env["OLLAMA_MODEL"])
			}
			// Verify context window scaling
			if tt.name == "Ultra" && env["LLM_DEFAULT_MAX_TOKENS"] != "32768" {
				t.Error("Ultra profile should have 32k context")
			}
		})
	}
}
