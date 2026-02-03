// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/tools"
)

func TestNewClassifier(t *testing.T) {
	t.Run("empty type creates regex classifier", func(t *testing.T) {
		c, err := NewClassifier(FactoryConfig{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := c.(*RegexClassifier); !ok {
			t.Errorf("expected *RegexClassifier, got %T", c)
		}
	})

	t.Run("regex type creates regex classifier", func(t *testing.T) {
		c, err := NewClassifier(FactoryConfig{Type: ClassifierTypeRegex})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := c.(*RegexClassifier); !ok {
			t.Errorf("expected *RegexClassifier, got %T", c)
		}
	})

	t.Run("llm type without client returns error", func(t *testing.T) {
		_, err := NewClassifier(FactoryConfig{
			Type:            ClassifierTypeLLM,
			ToolDefinitions: []tools.ToolDefinition{{Name: "test"}},
		})
		if err == nil {
			t.Error("expected error for missing LLMClient")
		}
	})

	t.Run("llm type without tools returns error", func(t *testing.T) {
		_, err := NewClassifier(FactoryConfig{
			Type:      ClassifierTypeLLM,
			LLMClient: &factoryMockLLMClient{},
		})
		if err == nil {
			t.Error("expected error for missing ToolDefinitions")
		}
	})

	t.Run("llm type with valid config creates llm classifier", func(t *testing.T) {
		c, err := NewClassifier(FactoryConfig{
			Type:            ClassifierTypeLLM,
			LLMClient:       &factoryMockLLMClient{},
			ToolDefinitions: []tools.ToolDefinition{{Name: "test", Description: "test tool"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := c.(*LLMClassifier); !ok {
			t.Errorf("expected *LLMClassifier, got %T", c)
		}
	})

	t.Run("llm type with custom config uses that config", func(t *testing.T) {
		customConfig := ClassifierConfig{
			Temperature:         0.5,
			MaxTokens:           512,
			Timeout:             10e9, // 10 seconds
			MaxRetries:          5,
			RetryBackoff:        200e6, // 200ms
			CacheTTL:            0,     // Disable cache
			CacheMaxSize:        0,
			ConfidenceThreshold: 0.9,
			FallbackToRegex:     false,
			MaxConcurrent:       5,
		}

		c, err := NewClassifier(FactoryConfig{
			Type:            ClassifierTypeLLM,
			LLMClient:       &factoryMockLLMClient{},
			ToolDefinitions: []tools.ToolDefinition{{Name: "test", Description: "test tool"}},
			LLMConfig:       &customConfig,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		llmC := c.(*LLMClassifier)
		if llmC.config.Temperature != 0.5 {
			t.Errorf("expected Temperature 0.5, got %f", llmC.config.Temperature)
		}
		if llmC.config.MaxRetries != 5 {
			t.Errorf("expected MaxRetries 5, got %d", llmC.config.MaxRetries)
		}
	})

	t.Run("unknown type returns error", func(t *testing.T) {
		_, err := NewClassifier(FactoryConfig{Type: "unknown"})
		if err == nil {
			t.Error("expected error for unknown type")
		}
	})
}

func TestMustNewClassifier(t *testing.T) {
	t.Run("valid config returns classifier", func(t *testing.T) {
		c := MustNewClassifier(FactoryConfig{Type: ClassifierTypeRegex})
		if c == nil {
			t.Error("expected non-nil classifier")
		}
	})

	t.Run("invalid config panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid config")
			}
		}()
		MustNewClassifier(FactoryConfig{Type: "invalid"})
	})
}

func TestClassifierTypeConstants(t *testing.T) {
	if ClassifierTypeRegex != "regex" {
		t.Errorf("expected ClassifierTypeRegex to be 'regex', got '%s'", ClassifierTypeRegex)
	}
	if ClassifierTypeLLM != "llm" {
		t.Errorf("expected ClassifierTypeLLM to be 'llm', got '%s'", ClassifierTypeLLM)
	}
}

// factoryMockLLMClient implements llm.Client for factory testing.
type factoryMockLLMClient struct {
	response string
}

func (m *factoryMockLLMClient) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: m.response}, nil
}

func (m *factoryMockLLMClient) Name() string {
	return "test-provider"
}

func (m *factoryMockLLMClient) Model() string {
	return "test-model"
}
