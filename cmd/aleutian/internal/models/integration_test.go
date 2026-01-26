// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package models

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// RetryPolicy Tests
// =============================================================================

// TestDefaultRetryPolicy verifies default policy values.
//
// # Description
//
// Tests that DefaultRetryPolicy returns expected configuration.
func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
	if policy.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", policy.MaxRetries)
	}
	if policy.InitialDelay != 1*time.Second {
		t.Errorf("expected InitialDelay 1s, got %v", policy.InitialDelay)
	}
	if policy.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay 30s, got %v", policy.MaxDelay)
	}
	if policy.JitterFactor != 0.1 {
		t.Errorf("expected JitterFactor 0.1, got %f", policy.JitterFactor)
	}
}

// TestRetryPolicy_CalculateDelay verifies exponential backoff.
//
// # Description
//
// Tests that delay increases exponentially with attempts.
func TestRetryPolicy_CalculateDelay(t *testing.T) {
	t.Run("exponential increase", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   5,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     10 * time.Second,
			JitterFactor: 0, // No jitter for predictable tests
		}

		delay0 := policy.CalculateDelay(0)
		delay1 := policy.CalculateDelay(1)
		delay2 := policy.CalculateDelay(2)

		if delay0 != 100*time.Millisecond {
			t.Errorf("attempt 0: expected 100ms, got %v", delay0)
		}
		if delay1 != 200*time.Millisecond {
			t.Errorf("attempt 1: expected 200ms, got %v", delay1)
		}
		if delay2 != 400*time.Millisecond {
			t.Errorf("attempt 2: expected 400ms, got %v", delay2)
		}
	})

	t.Run("caps at max delay", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   10,
			InitialDelay: 1 * time.Second,
			MaxDelay:     5 * time.Second,
			JitterFactor: 0,
		}

		delay5 := policy.CalculateDelay(5)
		delay10 := policy.CalculateDelay(10)

		if delay5 != 5*time.Second {
			t.Errorf("attempt 5: expected cap at 5s, got %v", delay5)
		}
		if delay10 != 5*time.Second {
			t.Errorf("attempt 10: expected cap at 5s, got %v", delay10)
		}
	})

	t.Run("applies jitter", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxRetries:   3,
			InitialDelay: 1 * time.Second,
			MaxDelay:     30 * time.Second,
			JitterFactor: 0.5, // 50% jitter for obvious variation
		}

		// With 50% jitter, delays should vary between samples
		delays := make([]time.Duration, 10)
		for i := 0; i < 10; i++ {
			delays[i] = policy.CalculateDelay(0)
		}

		// Check that not all delays are identical (jitter applied)
		allSame := true
		for i := 1; i < len(delays); i++ {
			if delays[i] != delays[0] {
				allSame = false
				break
			}
		}
		if allSame {
			t.Error("expected jitter to produce varied delays")
		}
	})

	t.Run("nil policy uses default", func(t *testing.T) {
		var policy *RetryPolicy
		delay := policy.CalculateDelay(0)

		// Should not panic and return reasonable default
		if delay < 500*time.Millisecond || delay > 2*time.Second {
			t.Errorf("nil policy delay out of expected range: %v", delay)
		}
	})
}

// =============================================================================
// ModelStatus Tests
// =============================================================================

// TestModelStatus_String verifies string conversion.
//
// # Description
//
// Tests that ModelStatus.String() returns correct values.
func TestModelStatus_String(t *testing.T) {
	tests := []struct {
		status   ModelStatus
		expected string
	}{
		{StatusUnknown, "unknown"},
		{StatusPresent, "present"},
		{StatusPulling, "pulling"},
		{StatusNotFound, "not_found"},
		{StatusBlocked, "blocked"},
		{ModelStatus(99), "unknown"}, // Invalid value
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("ModelStatus(%d).String() = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// LocalModelInfo Tests
// =============================================================================

// TestLocalModelInfo_FullName verifies name formatting.
//
// # Description
//
// Tests that FullName combines name and tag correctly.
func TestLocalModelInfo_FullName(t *testing.T) {
	t.Run("with tag", func(t *testing.T) {
		info := LocalModelInfo{Name: "llama3", Tag: "8b"}
		if got := info.FullName(); got != "llama3:8b" {
			t.Errorf("FullName() = %q, want %q", got, "llama3:8b")
		}
	})

	t.Run("with latest tag", func(t *testing.T) {
		info := LocalModelInfo{Name: "llama3", Tag: "latest"}
		if got := info.FullName(); got != "llama3" {
			t.Errorf("FullName() = %q, want %q", got, "llama3")
		}
	})

	t.Run("with empty tag", func(t *testing.T) {
		info := LocalModelInfo{Name: "llama3", Tag: ""}
		if got := info.FullName(); got != "llama3" {
			t.Errorf("FullName() = %q, want %q", got, "llama3")
		}
	})
}

// =============================================================================
// NewDefaultModelManager Tests
// =============================================================================

// TestNewDefaultModelManager verifies constructor.
//
// # Description
//
// Tests various configuration scenarios.
func TestNewDefaultModelManager(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manager == nil {
			t.Fatal("expected non-nil manager")
		}
	})

	t.Run("missing querier", func(t *testing.T) {
		selector := NewMockModelSelector()

		_, err := NewDefaultModelManager(ModelManagerConfig{
			Selector: selector,
		})

		if err == nil {
			t.Error("expected error for missing querier")
		}
	})

	t.Run("missing selector", func(t *testing.T) {
		querier := NewMockModelInfoProvider()

		_, err := NewDefaultModelManager(ModelManagerConfig{
			Querier: querier,
		})

		if err == nil {
			t.Error("expected error for missing selector")
		}
	})

	t.Run("applies defaults", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify defaults were applied
		if manager.baseURL != "http://localhost:11434" {
			t.Errorf("expected default baseURL, got %q", manager.baseURL)
		}
		if manager.defaultTimeout != 30*time.Minute {
			t.Errorf("expected default timeout 30m, got %v", manager.defaultTimeout)
		}
		if manager.cacheTTL != 24*time.Hour {
			t.Errorf("expected default cacheTTL 24h, got %v", manager.cacheTTL)
		}
	})

	t.Run("with allowlist", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:   querier,
			Selector:  selector,
			Allowlist: []string{"llama3:8b", "mistral"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manager.allowlist == nil {
			t.Error("expected allowlist to be set")
		}
		if len(manager.allowlist) != 2 {
			t.Errorf("expected 2 allowlist entries, got %d", len(manager.allowlist))
		}
	})
}

// =============================================================================
// EnsureModel Tests
// =============================================================================

// TestDefaultModelManager_EnsureModel_ExistingModel verifies existing model handling.
//
// # Description
//
// Tests that existing models are returned without pulling.
func TestDefaultModelManager_EnsureModel_ExistingModel(t *testing.T) {
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		return &ModelInfo{
			Name:   "llama3:8b",
			Digest: "sha256:abc123",
			Size:   4_000_000_000,
		}, nil
	}

	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	result, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WasPulled {
		t.Error("expected WasPulled=false for existing model")
	}
	if result.Model != "llama3:8b" {
		t.Errorf("expected model 'llama3:8b', got '%s'", result.Model)
	}
	if result.Digest != "sha256:abc123" {
		t.Errorf("expected digest 'sha256:abc123', got '%s'", result.Digest)
	}
}

// TestDefaultModelManager_EnsureModel_BlockedModel verifies blocklist enforcement.
//
// # Description
//
// Tests that blocked models return error immediately.
func TestDefaultModelManager_EnsureModel_BlockedModel(t *testing.T) {
	querier := NewMockModelInfoProvider()
	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
		Allowlist:   []string{"llama3:8b"}, // Only llama3:8b allowed
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.EnsureModel(ctx, "mistral:7b", EnsureOpts{})

	if err == nil {
		t.Error("expected error for blocked model")
	}
	if !errors.Is(err, ErrModelBlocked) {
		t.Errorf("expected ErrModelBlocked, got %v", err)
	}

	// Verify block was logged
	if len(auditLogger.BlockEvents) != 1 {
		t.Errorf("expected 1 block event, got %d", len(auditLogger.BlockEvents))
	}
}

// TestDefaultModelManager_EnsureModel_NotFoundNoAllowPull verifies AllowPull=false behavior.
//
// # Description
//
// Tests that missing models return error when AllowPull is false.
func TestDefaultModelManager_EnsureModel_NotFoundNoAllowPull(t *testing.T) {
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		return nil, ErrModelNotFound
	}

	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
		AllowPull: false,
	})

	if err == nil {
		t.Error("expected error for missing model with AllowPull=false")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

// TestDefaultModelManager_EnsureModel_Fallback verifies fallback chain behavior.
//
// # Description
//
// Tests that fallback models are tried when primary fails.
func TestDefaultModelManager_EnsureModel_Fallback(t *testing.T) {
	callCount := 0
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		callCount++
		if model == "llama3:8b" {
			return nil, ErrModelNotFound
		}
		if model == "llama3:latest" {
			return &ModelInfo{
				Name:   "llama3:latest",
				Digest: "sha256:fallback",
				Size:   3_000_000_000,
			}, nil
		}
		return nil, ErrModelNotFound
	}

	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	result, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
		AllowPull:      false,
		FallbackModels: []string{"llama3:latest"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.UsedFallback {
		t.Error("expected UsedFallback=true")
	}
	if result.Model != "llama3:latest" {
		t.Errorf("expected fallback model 'llama3:latest', got '%s'", result.Model)
	}
	if result.FallbackReason == "" {
		t.Error("expected FallbackReason to be set")
	}
}

// TestDefaultModelManager_EnsureModel_AllFallbacksFailed verifies error when all fallbacks fail.
//
// # Description
//
// Tests that appropriate error is returned when all models fail.
func TestDefaultModelManager_EnsureModel_AllFallbacksFailed(t *testing.T) {
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		return nil, ErrModelNotFound
	}

	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
		AllowPull:      false,
		FallbackModels: []string{"mistral:7b", "phi:2b"},
	})

	if err == nil {
		t.Error("expected error when all fallbacks fail")
	}
	if !errors.Is(err, ErrAllFallbacksFailed) {
		t.Errorf("expected ErrAllFallbacksFailed, got %v", err)
	}
}

// TestDefaultModelManager_EnsureModel_RetryOnNetworkError verifies retry behavior.
//
// # Description
//
// Tests that network errors trigger retries with backoff.
func TestDefaultModelManager_EnsureModel_RetryOnNetworkError(t *testing.T) {
	callCount := 0
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		callCount++
		if callCount < 3 {
			return nil, errors.New("connection refused")
		}
		return &ModelInfo{
			Name:   "llama3:8b",
			Digest: "sha256:success",
			Size:   4_000_000_000,
		}, nil
	}

	selector := NewMockModelSelector()
	auditLogger := NewMockModelAuditLogger()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:     querier,
		Selector:    selector,
		AuditLogger: auditLogger,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	result, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
		RetryPolicy: &RetryPolicy{
			MaxRetries:   5,
			InitialDelay: 1 * time.Millisecond, // Fast for tests
			MaxDelay:     10 * time.Millisecond,
			JitterFactor: 0,
		},
	})

	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 retries), got %d", callCount)
	}
	if result.Digest != "sha256:success" {
		t.Errorf("expected success digest, got '%s'", result.Digest)
	}
}

// =============================================================================
// SelectOptimalModel Tests
// =============================================================================

// TestDefaultModelManager_SelectOptimalModel verifies selection delegation.
//
// # Description
//
// Tests that selection is delegated to ModelSelector.
func TestDefaultModelManager_SelectOptimalModel(t *testing.T) {
	querier := NewMockModelInfoProvider()
	selector := NewMockModelSelector()
	selector.SelectModelFunc = func(ctx context.Context, category string, opts SelectionOpts) (string, error) {
		if category == "code" {
			return "deepseek-coder:6.7b", nil
		}
		return "llama3:8b", nil
	}

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:  querier,
		Selector: selector,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	model, err := manager.SelectOptimalModel(ctx, "code")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "deepseek-coder:6.7b" {
		t.Errorf("expected 'deepseek-coder:6.7b', got '%s'", model)
	}
}

// TestDefaultModelManager_SelectOptimalModel_BlockedResult verifies blocked selection handling.
//
// # Description
//
// Tests that selected models are checked against allowlist.
func TestDefaultModelManager_SelectOptimalModel_BlockedResult(t *testing.T) {
	querier := NewMockModelInfoProvider()
	selector := NewMockModelSelector()
	selector.SelectModelFunc = func(ctx context.Context, category string, opts SelectionOpts) (string, error) {
		return "mistral:7b", nil // Will be blocked
	}

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:   querier,
		Selector:  selector,
		Allowlist: []string{"llama3:8b"}, // Only llama3 allowed
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	_, err = manager.SelectOptimalModel(ctx, "chat")

	if err == nil {
		t.Error("expected error for blocked selected model")
	}
}

// =============================================================================
// VerifyModel Tests
// =============================================================================

// TestDefaultModelManager_VerifyModel verifies verification behavior.
//
// # Description
//
// Tests successful and failed verification scenarios.
func TestDefaultModelManager_VerifyModel(t *testing.T) {
	t.Run("successful verification", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return &ModelInfo{
				Name:   "llama3:8b",
				Digest: "sha256:abc123",
			}, nil
		}

		selector := NewMockModelSelector()
		auditLogger := NewMockModelAuditLogger()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:     querier,
			Selector:    selector,
			AuditLogger: auditLogger,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		ctx := context.Background()
		result, err := manager.VerifyModel(ctx, "llama3:8b")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Verified {
			t.Error("expected Verified=true")
		}
		if result.Digest != "sha256:abc123" {
			t.Errorf("expected digest 'sha256:abc123', got '%s'", result.Digest)
		}

		// Verify audit log
		if len(auditLogger.VerifyEvents) != 1 {
			t.Errorf("expected 1 verify event, got %d", len(auditLogger.VerifyEvents))
		}
	})

	t.Run("model not found", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return nil, ErrModelNotFound
		}

		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		ctx := context.Background()
		result, err := manager.VerifyModel(ctx, "nonexistent:model")

		if err == nil {
			t.Error("expected error for missing model")
		}
		if result.Error == nil {
			t.Error("expected result.Error to be set")
		}
	})
}

// =============================================================================
// GetModelStatus Tests
// =============================================================================

// TestDefaultModelManager_GetModelStatus verifies status detection.
//
// # Description
//
// Tests various model status scenarios.
func TestDefaultModelManager_GetModelStatus(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return &ModelInfo{Name: "llama3:8b"}, nil
		}

		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		ctx := context.Background()
		status, err := manager.GetModelStatus(ctx, "llama3:8b")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != StatusPresent {
			t.Errorf("expected StatusPresent, got %v", status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return nil, errors.New("model not found")
		}

		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		ctx := context.Background()
		status, err := manager.GetModelStatus(ctx, "nonexistent:model")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != StatusNotFound {
			t.Errorf("expected StatusNotFound, got %v", status)
		}
	})

	t.Run("blocked", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:   querier,
			Selector:  selector,
			Allowlist: []string{"llama3:8b"},
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		ctx := context.Background()
		status, err := manager.GetModelStatus(ctx, "mistral:7b")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != StatusBlocked {
			t.Errorf("expected StatusBlocked, got %v", status)
		}
	})
}

// =============================================================================
// ListAvailableModels Tests
// =============================================================================

// TestDefaultModelManager_ListAvailableModels_Cache verifies caching behavior.
//
// # Description
//
// Tests that cache is used and respects TTL.
func TestDefaultModelManager_ListAvailableModels_Cache(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
			CacheTTL: 1 * time.Hour,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Pre-populate cache
		manager.mu.Lock()
		manager.modelCache = []LocalModelInfo{
			{Name: "llama3", Tag: "8b"},
		}
		manager.modelCacheTime = time.Now()
		manager.mu.Unlock()

		ctx := context.Background()
		models, err := manager.ListAvailableModels(ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(models) != 1 {
			t.Errorf("expected 1 cached model, got %d", len(models))
		}
	})

	t.Run("returns copy of cache", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		selector := NewMockModelSelector()

		manager, err := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}

		// Pre-populate cache
		manager.mu.Lock()
		manager.modelCache = []LocalModelInfo{
			{Name: "llama3", Tag: "8b"},
		}
		manager.modelCacheTime = time.Now()
		manager.mu.Unlock()

		ctx := context.Background()
		models1, _ := manager.ListAvailableModels(ctx)
		models2, _ := manager.ListAvailableModels(ctx)

		// Modify first result
		models1[0].Name = "modified"

		// Second result should be unchanged
		if models2[0].Name != "llama3" {
			t.Error("cache modification leaked to subsequent calls")
		}
	})
}

// TestDefaultModelManager_InvalidateCache verifies cache invalidation.
//
// # Description
//
// Tests that InvalidateCache clears the cache.
func TestDefaultModelManager_InvalidateCache(t *testing.T) {
	querier := NewMockModelInfoProvider()
	selector := NewMockModelSelector()

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:  querier,
		Selector: selector,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Pre-populate cache
	manager.mu.Lock()
	manager.modelCache = []LocalModelInfo{{Name: "test"}}
	manager.modelCacheTime = time.Now()
	manager.mu.Unlock()

	manager.InvalidateCache()

	manager.mu.RLock()
	cacheIsNil := manager.modelCache == nil
	cacheTimeIsZero := manager.modelCacheTime.IsZero()
	manager.mu.RUnlock()

	if !cacheIsNil {
		t.Error("expected modelCache to be nil after invalidation")
	}
	if !cacheTimeIsZero {
		t.Error("expected modelCacheTime to be zero after invalidation")
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

// TestNormalizeModelNameForLookup verifies model name normalization.
//
// # Description
//
// Tests various normalization scenarios.
func TestNormalizeModelNameForLookup(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"llama3:8b", "llama3:8b"},
		{"LLAMA3:8B", "llama3:8b"},
		{"llama3", "llama3:latest"},
		{"  llama3:8b  ", "llama3:8b"},
		{"Mistral", "mistral:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeModelNameForLookup(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeModelNameForLookup(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSplitModelName verifies model name splitting.
//
// # Description
//
// Tests name/tag separation.
func TestSplitModelName(t *testing.T) {
	tests := []struct {
		input        string
		expectedName string
		expectedTag  string
	}{
		{"llama3:8b", "llama3", "8b"},
		{"llama3", "llama3", "latest"},
		{"mistral:7b-instruct-q4", "mistral", "7b-instruct-q4"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, tag := splitModelName(tt.input)
			if name != tt.expectedName {
				t.Errorf("name = %q, want %q", name, tt.expectedName)
			}
			if tag != tt.expectedTag {
				t.Errorf("tag = %q, want %q", tag, tt.expectedTag)
			}
		})
	}
}

// TestIsRetryableError verifies error classification.
//
// # Description
//
// Tests network error detection.
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"no such host", errors.New("no such host"), true},
		{"i/o timeout", errors.New("i/o timeout"), true},
		{"dial tcp", errors.New("dial tcp: connection refused"), true},
		{"EOF", errors.New("EOF"), true},
		{"context deadline", context.DeadlineExceeded, true},
		{"model not found", ErrModelNotFound, false},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// TestIsNotFoundError verifies not found detection.
//
// # Description
//
// Tests model not found error detection.
func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"not found", errors.New("model not found"), true},
		{"does not exist", errors.New("model does not exist"), true},
		{"NOT FOUND uppercase", errors.New("NOT FOUND"), true},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotFoundError(tt.err)
			if got != tt.expected {
				t.Errorf("isNotFoundError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// MockModelManager Tests
// =============================================================================

// TestMockModelManager verifies mock behavior.
//
// # Description
//
// Tests that mock records calls and returns configured results.
func TestMockModelManager(t *testing.T) {
	t.Run("records ensure calls", func(t *testing.T) {
		mock := NewMockModelManager()

		ctx := context.Background()
		_, _ = mock.EnsureModel(ctx, "llama3:8b", EnsureOpts{AllowPull: true})
		_, _ = mock.EnsureModel(ctx, "mistral:7b", EnsureOpts{})

		if len(mock.EnsureCalls) != 2 {
			t.Errorf("expected 2 ensure calls, got %d", len(mock.EnsureCalls))
		}
		if mock.EnsureCalls[0].Model != "llama3:8b" {
			t.Errorf("expected first call model 'llama3:8b', got '%s'", mock.EnsureCalls[0].Model)
		}
	})

	t.Run("uses function override", func(t *testing.T) {
		mock := NewMockModelManager()
		mock.EnsureModelFunc = func(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
			return ModelResult{Model: "custom-result", WasPulled: true}, nil
		}

		ctx := context.Background()
		result, _ := mock.EnsureModel(ctx, "any-model", EnsureOpts{})

		if result.Model != "custom-result" {
			t.Errorf("expected custom result, got '%s'", result.Model)
		}
	})

	t.Run("returns default error", func(t *testing.T) {
		mock := NewMockModelManager()
		expectedErr := errors.New("default error")
		mock.DefaultErr = expectedErr

		ctx := context.Background()
		_, err := mock.EnsureModel(ctx, "any-model", EnsureOpts{})

		if err != expectedErr {
			t.Errorf("expected default error, got %v", err)
		}
	})

	t.Run("reset clears calls", func(t *testing.T) {
		mock := NewMockModelManager()

		ctx := context.Background()
		_, _ = mock.EnsureModel(ctx, "test", EnsureOpts{})

		mock.Reset()

		if len(mock.EnsureCalls) != 0 {
			t.Error("expected ensure calls to be cleared after reset")
		}
	})
}

// =============================================================================
// Concurrency Tests
// =============================================================================

// TestDefaultModelManager_ConcurrentAccess verifies thread safety.
//
// # Description
//
// Tests that concurrent operations are safe.
func TestDefaultModelManager_ConcurrentAccess(t *testing.T) {
	querier := NewMockModelInfoProvider()
	querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		return &ModelInfo{Name: "llama3:8b"}, nil
	}

	selector := NewMockModelSelector()
	selector.SelectModelFunc = func(ctx context.Context, category string, opts SelectionOpts) (string, error) {
		return "llama3:8b", nil
	}

	manager, err := NewDefaultModelManager(ModelManagerConfig{
		Querier:  querier,
		Selector: selector,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Pre-populate cache
	manager.mu.Lock()
	manager.modelCache = []LocalModelInfo{{Name: "llama3", Tag: "8b"}}
	manager.modelCacheTime = time.Now()
	manager.mu.Unlock()

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 4)

	ctx := context.Background()

	// Concurrent EnsureModel
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{})
			}
		}()
	}

	// Concurrent SelectOptimalModel
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = manager.SelectOptimalModel(ctx, "chat")
			}
		}()
	}

	// Concurrent GetModelStatus
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = manager.GetModelStatus(ctx, "llama3:8b")
			}
		}()
	}

	// Concurrent ListAvailableModels + InvalidateCache
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = manager.ListAvailableModels(ctx)
				if j%10 == 0 {
					manager.InvalidateCache()
				}
			}
		}()
	}

	wg.Wait()
	// Test passes if no race conditions detected
}

// =============================================================================
// Allowlist Tests
// =============================================================================

// TestDefaultModelManager_Allowlist verifies allowlist enforcement.
//
// # Description
//
// Tests various allowlist scenarios.
func TestDefaultModelManager_Allowlist(t *testing.T) {
	t.Run("allows exact match", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return &ModelInfo{Name: "llama3:8b"}, nil
		}
		selector := NewMockModelSelector()

		manager, _ := NewDefaultModelManager(ModelManagerConfig{
			Querier:   querier,
			Selector:  selector,
			Allowlist: []string{"llama3:8b"},
		})

		ctx := context.Background()
		_, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{})

		if err != nil {
			t.Errorf("expected allowed model to succeed, got %v", err)
		}
	})

	t.Run("allows base name match", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return &ModelInfo{Name: "llama3:70b"}, nil
		}
		selector := NewMockModelSelector()

		manager, _ := NewDefaultModelManager(ModelManagerConfig{
			Querier:   querier,
			Selector:  selector,
			Allowlist: []string{"llama3"}, // Base name only
		})

		ctx := context.Background()
		_, err := manager.EnsureModel(ctx, "llama3:70b", EnsureOpts{})

		if err != nil {
			t.Errorf("expected base name match to succeed, got %v", err)
		}
	})

	t.Run("no allowlist allows all", func(t *testing.T) {
		querier := NewMockModelInfoProvider()
		querier.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
			return &ModelInfo{Name: "any:model"}, nil
		}
		selector := NewMockModelSelector()

		manager, _ := NewDefaultModelManager(ModelManagerConfig{
			Querier:  querier,
			Selector: selector,
			// No Allowlist
		})

		ctx := context.Background()
		_, err := manager.EnsureModel(ctx, "any:model", EnsureOpts{})

		if err != nil {
			t.Errorf("expected no allowlist to allow all, got %v", err)
		}
	})
}
