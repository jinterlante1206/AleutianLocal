// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// mockLLMClient is a mock LLM client for testing.
type mockLLMClient struct {
	completeFunc func(ctx context.Context, request *llm.Request) (*llm.Response, error)
	callCount    atomic.Int32
}

func (m *mockLLMClient) Complete(ctx context.Context, request *llm.Request) (*llm.Response, error) {
	m.callCount.Add(1)
	if m.completeFunc != nil {
		return m.completeFunc(ctx, request)
	}
	return &llm.Response{
		Content: `{"is_analytical":false}`,
	}, nil
}

func (m *mockLLMClient) Name() string  { return "mock" }
func (m *mockLLMClient) Model() string { return "mock-model" }

// testToolDefs returns tool definitions for testing.
func testToolDefs() []tools.ToolDefinition {
	return []tools.ToolDefinition{
		{
			Name:        "find_entry_points",
			Description: "Find entry points in the codebase",
			Parameters: map[string]tools.ParamDef{
				"type": {
					Type:     tools.ParamTypeString,
					Required: false,
					Enum:     []any{"test", "main", "api"},
				},
			},
		},
		{
			Name:        "trace_data_flow",
			Description: "Trace data flow through the code",
			Parameters:  map[string]tools.ParamDef{},
		},
	}
}

func TestNewLLMClassifier(t *testing.T) {
	t.Run("valid inputs", func(t *testing.T) {
		client := &mockLLMClient{}
		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}
		if classifier == nil {
			t.Fatal("expected non-nil classifier")
		}
	})

	t.Run("nil client", func(t *testing.T) {
		_, err := NewLLMClassifier(nil, testToolDefs(), DefaultClassifierConfig())
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("empty tool defs", func(t *testing.T) {
		client := &mockLLMClient{}
		_, err := NewLLMClassifier(client, []tools.ToolDefinition{}, DefaultClassifierConfig())
		if err == nil {
			t.Fatal("expected error for empty tool defs")
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		client := &mockLLMClient{}
		config := ClassifierConfig{
			Temperature: -1, // Invalid
			MaxTokens:   256,
			Timeout:     5 * time.Second,
		}
		_, err := NewLLMClassifier(client, testToolDefs(), config)
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
	})
}

func TestClassifierConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultClassifierConfig()
		if err := config.Validate(); err != nil {
			t.Fatalf("validation failed: %v", err)
		}
	})

	t.Run("invalid temperature", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.Temperature = 1.5
		if err := config.Validate(); err == nil {
			t.Fatal("expected error for invalid temperature")
		}
	})

	t.Run("invalid max tokens", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.MaxTokens = 0
		if err := config.Validate(); err == nil {
			t.Fatal("expected error for invalid max tokens")
		}
	})

	t.Run("cache size required when TTL set", func(t *testing.T) {
		config := DefaultClassifierConfig()
		config.CacheTTL = 5 * time.Minute
		config.CacheMaxSize = 0
		if err := config.Validate(); err == nil {
			t.Fatal("expected error when cache TTL set but size is 0")
		}
	})
}

func TestLLMClassifier_Classify(t *testing.T) {
	t.Run("analytical query", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return &llm.Response{
					Content: `{"is_analytical":true,"tool":"find_entry_points","parameters":{"type":"test"},"confidence":0.9,"reasoning":"test query"}`,
				}, nil
			},
		}

		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if !result.IsAnalytical {
			t.Error("expected IsAnalytical=true")
		}
		if result.Tool != "find_entry_points" {
			t.Errorf("expected tool=find_entry_points, got %s", result.Tool)
		}
	})

	t.Run("non-analytical query", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return &llm.Response{
					Content: `{"is_analytical":false,"reasoning":"greeting"}`,
				}, nil
			},
		}

		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "Hello!")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if result.IsAnalytical {
			t.Error("expected IsAnalytical=false")
		}
	})

	t.Run("empty query", func(t *testing.T) {
		client := &mockLLMClient{}
		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if result.IsAnalytical {
			t.Error("expected IsAnalytical=false for empty query")
		}
		if client.callCount.Load() != 0 {
			t.Error("expected no LLM call for empty query")
		}
	})

	t.Run("whitespace query", func(t *testing.T) {
		client := &mockLLMClient{}
		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "   \t\n  ")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if result.IsAnalytical {
			t.Error("expected IsAnalytical=false for whitespace query")
		}
	})

	t.Run("fallback on LLM error", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return nil, errors.New("LLM unavailable")
			},
		}

		config := DefaultClassifierConfig()
		config.MaxRetries = 0 // No retries for faster test

		classifier, err := NewLLMClassifier(client, testToolDefs(), config)
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if !result.FallbackUsed {
			t.Error("expected FallbackUsed=true")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}

		config := DefaultClassifierConfig()
		config.Timeout = 100 * time.Millisecond

		classifier, err := NewLLMClassifier(client, testToolDefs(), config)
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = classifier.Classify(ctx, "What tests exist?")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}
	})
}

func TestLLMClassifier_Caching(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		callCount := 0
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				callCount++
				return &llm.Response{
					Content: `{"is_analytical":true,"tool":"find_entry_points","confidence":0.9}`,
				}, nil
			},
		}

		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		// First call - should hit LLM
		result1, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("First classify failed: %v", err)
		}

		// Second call - should hit cache
		result2, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Second classify failed: %v", err)
		}

		if callCount != 1 {
			t.Errorf("expected 1 LLM call, got %d", callCount)
		}
		if !result2.Cached {
			t.Error("expected second result to be cached")
		}
		if result1.IsAnalytical != result2.IsAnalytical {
			t.Error("cached result should match original")
		}
	})

	t.Run("cache stats", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return &llm.Response{
					Content: `{"is_analytical":true,"confidence":0.9}`,
				}, nil
			},
		}

		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		// Initial stats
		hitRate, size := classifier.CacheStats()
		if hitRate != 0 || size != 0 {
			t.Error("expected empty cache initially")
		}

		// Add entry
		_, _ = classifier.Classify(context.Background(), "What tests exist?")
		_, size = classifier.CacheStats()
		if size != 1 {
			t.Errorf("expected cache size 1, got %d", size)
		}

		// Hit cache
		_, _ = classifier.Classify(context.Background(), "What tests exist?")
		hitRate, _ = classifier.CacheStats()
		if hitRate != 0.5 {
			t.Errorf("expected hit rate 0.5, got %f", hitRate)
		}
	})
}

func TestLLMClassifier_Singleflight(t *testing.T) {
	t.Run("concurrent requests coalesced", func(t *testing.T) {
		var callCount atomic.Int32
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				callCount.Add(1)
				time.Sleep(50 * time.Millisecond) // Simulate delay
				return &llm.Response{
					Content: `{"is_analytical":true,"confidence":0.9}`,
				}, nil
			},
		}

		config := DefaultClassifierConfig()
		config.CacheTTL = 0 // Disable cache to test singleflight

		classifier, err := NewLLMClassifier(client, testToolDefs(), config)
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		// Launch concurrent requests
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = classifier.Classify(context.Background(), "What tests exist?")
			}()
		}
		wg.Wait()

		// Should have only 1 LLM call due to singleflight
		if callCount.Load() != 1 {
			t.Errorf("expected 1 LLM call due to singleflight, got %d", callCount.Load())
		}
	})
}

func TestLLMClassifier_Retry(t *testing.T) {
	t.Run("retry on failure", func(t *testing.T) {
		var callCount atomic.Int32
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				count := callCount.Add(1)
				if count < 3 {
					return nil, errors.New("temporary error")
				}
				return &llm.Response{
					Content: `{"is_analytical":true,"confidence":0.9}`,
				}, nil
			},
		}

		config := DefaultClassifierConfig()
		config.MaxRetries = 2
		config.RetryBackoff = 10 * time.Millisecond

		classifier, err := NewLLMClassifier(client, testToolDefs(), config)
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if !result.IsAnalytical {
			t.Error("expected successful classification after retries")
		}
		if callCount.Load() != 3 {
			t.Errorf("expected 3 LLM calls (1 + 2 retries), got %d", callCount.Load())
		}
	})
}

func TestLLMClassifier_HallucinationHandling(t *testing.T) {
	t.Run("hallucinated tool triggers fallback", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return &llm.Response{
					Content: `{"is_analytical":true,"tool":"nonexistent_tool","confidence":0.9}`,
				}, nil
			},
		}

		classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if !result.FallbackUsed {
			t.Error("expected FallbackUsed=true for hallucinated tool")
		}
	})
}

func TestLLMClassifier_LowConfidenceFallback(t *testing.T) {
	t.Run("low confidence triggers fallback", func(t *testing.T) {
		client := &mockLLMClient{
			completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
				return &llm.Response{
					Content: `{"is_analytical":true,"tool":"find_entry_points","confidence":0.3}`,
				}, nil
			},
		}

		config := DefaultClassifierConfig()
		config.ConfidenceThreshold = 0.7

		classifier, err := NewLLMClassifier(client, testToolDefs(), config)
		if err != nil {
			t.Fatalf("NewLLMClassifier failed: %v", err)
		}

		result, err := classifier.Classify(context.Background(), "What tests exist?")
		if err != nil {
			t.Fatalf("Classify failed: %v", err)
		}

		if !result.FallbackUsed {
			t.Error("expected FallbackUsed=true for low confidence")
		}
	})
}

func TestLLMClassifier_IsAnalytical(t *testing.T) {
	client := &mockLLMClient{
		completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"is_analytical":true,"confidence":0.9}`,
			}, nil
		},
	}

	classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
	if err != nil {
		t.Fatalf("NewLLMClassifier failed: %v", err)
	}

	if !classifier.IsAnalytical(context.Background(), "What tests exist?") {
		t.Error("expected IsAnalytical=true")
	}
}

func TestLLMClassifier_SuggestTool(t *testing.T) {
	client := &mockLLMClient{
		completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"is_analytical":true,"tool":"find_entry_points","confidence":0.9}`,
			}, nil
		},
	}

	classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
	if err != nil {
		t.Fatalf("NewLLMClassifier failed: %v", err)
	}

	tool, ok := classifier.SuggestTool(context.Background(), "What tests exist?",
		[]string{"find_entry_points", "trace_data_flow"})

	if !ok {
		t.Error("expected tool suggestion")
	}
	if tool != "find_entry_points" {
		t.Errorf("expected find_entry_points, got %s", tool)
	}
}

func TestLLMClassifier_SuggestToolWithHint(t *testing.T) {
	client := &mockLLMClient{
		completeFunc: func(ctx context.Context, request *llm.Request) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"is_analytical":true,"tool":"find_entry_points","parameters":{"type":"test"},"search_patterns":["*_test.go"],"confidence":0.9}`,
			}, nil
		},
	}

	classifier, err := NewLLMClassifier(client, testToolDefs(), DefaultClassifierConfig())
	if err != nil {
		t.Fatalf("NewLLMClassifier failed: %v", err)
	}

	suggestion, ok := classifier.SuggestToolWithHint(context.Background(), "What tests exist?",
		[]string{"find_entry_points", "trace_data_flow"})

	if !ok {
		t.Error("expected tool suggestion")
	}
	if suggestion.ToolName != "find_entry_points" {
		t.Errorf("expected find_entry_points, got %s", suggestion.ToolName)
	}
	if len(suggestion.SearchPatterns) == 0 {
		t.Error("expected search patterns")
	}
}

// Test ClassificationResult.ToToolSuggestion
func TestClassificationResult_ToToolSuggestion(t *testing.T) {
	t.Run("analytical with tool", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical:   true,
			Tool:           "find_entry_points",
			Parameters:     map[string]any{"type": "test"},
			SearchPatterns: []string{"*_test.go"},
		}

		suggestion := result.ToToolSuggestion()
		if suggestion == nil {
			t.Fatal("expected non-nil suggestion")
		}
		if suggestion.ToolName != "find_entry_points" {
			t.Errorf("expected find_entry_points, got %s", suggestion.ToolName)
		}
	})

	t.Run("non-analytical returns nil", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: false,
		}

		suggestion := result.ToToolSuggestion()
		if suggestion != nil {
			t.Error("expected nil suggestion for non-analytical")
		}
	})

	t.Run("no tool returns nil", func(t *testing.T) {
		result := &ClassificationResult{
			IsAnalytical: true,
			Tool:         "",
		}

		suggestion := result.ToToolSuggestion()
		if suggestion != nil {
			t.Error("expected nil suggestion when no tool")
		}
	})
}

// Test JSON extraction edge cases
func TestExtractJSON_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		validate func(t *testing.T, result []byte)
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "clean JSON",
			input:   `{"is_analytical":true}`,
			wantErr: false,
			validate: func(t *testing.T, result []byte) {
				var r map[string]any
				if err := json.Unmarshal(result, &r); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if r["is_analytical"] != true {
					t.Error("expected is_analytical=true")
				}
			},
		},
		{
			name:    "markdown wrapped",
			input:   "```json\n{\"is_analytical\":true}\n```",
			wantErr: false,
		},
		{
			name:    "with preamble",
			input:   "Here is the classification:\n{\"is_analytical\":true}",
			wantErr: false,
		},
		{
			name:    "generic code block",
			input:   "```\n{\"is_analytical\":true}\n```",
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "nested braces in strings",
			input:   `{"reasoning":"something with {braces}"}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}
