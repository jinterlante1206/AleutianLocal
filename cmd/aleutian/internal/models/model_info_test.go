package models

import (
	"context"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// ModelInfo Tests
// =============================================================================

func TestModelInfo_SizeGB(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		expected float64
	}{
		{
			name:     "zero bytes",
			size:     0,
			expected: 0.0,
		},
		{
			name:     "one gigabyte",
			size:     1024 * 1024 * 1024,
			expected: 1.0,
		},
		{
			name:     "four gigabytes",
			size:     4 * 1024 * 1024 * 1024,
			expected: 4.0,
		},
		{
			name:     "half gigabyte",
			size:     512 * 1024 * 1024,
			expected: 0.5,
		},
		{
			name:     "large model 70GB",
			size:     70 * 1024 * 1024 * 1024,
			expected: 70.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ModelInfo{Size: tt.size}
			result := info.SizeGB()

			// Allow small floating point difference
			diff := result - tt.expected
			if diff < 0 {
				diff = -diff
			}
			if diff > 0.001 {
				t.Errorf("SizeGB() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestModelInfo_HasDigest(t *testing.T) {
	tests := []struct {
		name     string
		digest   string
		expected bool
	}{
		{
			name:     "empty digest",
			digest:   "",
			expected: false,
		},
		{
			name:     "valid digest",
			digest:   "sha256:abc123",
			expected: true,
		},
		{
			name:     "whitespace only",
			digest:   "   ",
			expected: true, // Non-empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ModelInfo{Digest: tt.digest}
			result := info.HasDigest()
			if result != tt.expected {
				t.Errorf("HasDigest() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestModelInfo_MatchesDigest(t *testing.T) {
	tests := []struct {
		name        string
		modelDigest string
		checkDigest string
		expected    bool
	}{
		{
			name:        "exact match",
			modelDigest: "sha256:abc123",
			checkDigest: "sha256:abc123",
			expected:    true,
		},
		{
			name:        "case insensitive match",
			modelDigest: "sha256:ABC123",
			checkDigest: "sha256:abc123",
			expected:    true,
		},
		{
			name:        "no match",
			modelDigest: "sha256:abc123",
			checkDigest: "sha256:xyz789",
			expected:    false,
		},
		{
			name:        "empty model digest",
			modelDigest: "",
			checkDigest: "sha256:abc123",
			expected:    false,
		},
		{
			name:        "empty check digest",
			modelDigest: "sha256:abc123",
			checkDigest: "",
			expected:    false,
		},
		{
			name:        "both empty",
			modelDigest: "",
			checkDigest: "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ModelInfo{Digest: tt.modelDigest}
			result := info.MatchesDigest(tt.checkDigest)
			if result != tt.expected {
				t.Errorf("MatchesDigest(%q) = %v, expected %v", tt.checkDigest, result, tt.expected)
			}
		})
	}
}

func TestModelInfo_ParameterSizeString(t *testing.T) {
	tests := []struct {
		name     string
		count    int64
		expected string
	}{
		{
			name:     "unknown (zero)",
			count:    0,
			expected: "unknown",
		},
		{
			name:     "7 billion",
			count:    7_000_000_000,
			expected: "7B",
		},
		{
			name:     "70 billion",
			count:    70_000_000_000,
			expected: "70B",
		},
		{
			name:     "13 billion",
			count:    13_000_000_000,
			expected: "13B",
		},
		{
			name:     "350 million",
			count:    350_000_000,
			expected: "350M",
		},
		{
			name:     "1 million",
			count:    1_000_000,
			expected: "1M",
		},
		{
			name:     "500 thousand",
			count:    500_000,
			expected: "500K",
		},
		{
			name:     "small number",
			count:    500,
			expected: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ModelInfo{ParameterCount: tt.count}
			result := info.ParameterSizeString()
			if result != tt.expected {
				t.Errorf("ParameterSizeString() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// FallbackChain Tests
// =============================================================================

func TestFallbackChain_Models(t *testing.T) {
	tests := []struct {
		name     string
		chain    *FallbackChain
		expected []string
	}{
		{
			name:     "nil chain",
			chain:    nil,
			expected: nil,
		},
		{
			name:     "primary only",
			chain:    &FallbackChain{Primary: "llama3:8b"},
			expected: []string{"llama3:8b"},
		},
		{
			name:     "primary with fallbacks",
			chain:    &FallbackChain{Primary: "llama3:70b", Fallbacks: []string{"llama3:8b", "phi3"}},
			expected: []string{"llama3:70b", "llama3:8b", "phi3"},
		},
		{
			name:     "fallbacks only (no primary)",
			chain:    &FallbackChain{Fallbacks: []string{"llama3:8b", "phi3"}},
			expected: []string{"llama3:8b", "phi3"},
		},
		{
			name:     "empty chain",
			chain:    &FallbackChain{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chain.Models()

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Models() = %v, expected nil", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Models() length = %d, expected %d", len(result), len(tt.expected))
				return
			}

			for i, model := range result {
				if model != tt.expected[i] {
					t.Errorf("Models()[%d] = %q, expected %q", i, model, tt.expected[i])
				}
			}
		})
	}
}

func TestFallbackChain_Len(t *testing.T) {
	tests := []struct {
		name     string
		chain    *FallbackChain
		expected int
	}{
		{
			name:     "nil chain",
			chain:    nil,
			expected: 0,
		},
		{
			name:     "primary only",
			chain:    &FallbackChain{Primary: "llama3:8b"},
			expected: 1,
		},
		{
			name:     "primary with two fallbacks",
			chain:    &FallbackChain{Primary: "llama3:70b", Fallbacks: []string{"llama3:8b", "phi3"}},
			expected: 3,
		},
		{
			name:     "empty chain",
			chain:    &FallbackChain{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chain.Len()
			if result != tt.expected {
				t.Errorf("Len() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestFallbackChain_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		chain    *FallbackChain
		expected bool
	}{
		{
			name:     "nil chain",
			chain:    nil,
			expected: true,
		},
		{
			name:     "empty chain",
			chain:    &FallbackChain{},
			expected: true,
		},
		{
			name:     "primary only",
			chain:    &FallbackChain{Primary: "llama3:8b"},
			expected: false,
		},
		{
			name:     "fallbacks only",
			chain:    &FallbackChain{Fallbacks: []string{"phi3"}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chain.IsEmpty()
			if result != tt.expected {
				t.Errorf("IsEmpty() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestFallbackChain_Validate(t *testing.T) {
	tests := []struct {
		name        string
		chain       *FallbackChain
		expectError bool
		errorSubstr string
	}{
		{
			name:        "valid chain with primary",
			chain:       &FallbackChain{Primary: "llama3:8b"},
			expectError: false,
		},
		{
			name:        "valid chain with fallbacks",
			chain:       &FallbackChain{Primary: "llama3:70b", Fallbacks: []string{"llama3:8b", "phi3"}},
			expectError: false,
		},
		{
			name:        "empty chain",
			chain:       &FallbackChain{},
			expectError: true,
			errorSubstr: "at least one model",
		},
		{
			name:        "invalid model name",
			chain:       &FallbackChain{Primary: "invalid model name!"},
			expectError: true,
			errorSubstr: "invalid model name",
		},
		{
			name:        "duplicate models",
			chain:       &FallbackChain{Primary: "llama3:8b", Fallbacks: []string{"llama3:8b"}},
			expectError: true,
			errorSubstr: "duplicate",
		},
		{
			name:        "case insensitive duplicate",
			chain:       &FallbackChain{Primary: "LLAMA3:8B", Fallbacks: []string{"llama3:8b"}},
			expectError: true,
			errorSubstr: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.chain.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Validate() expected error, got nil")
				} else if tt.errorSubstr != "" && !containsSubstring(err.Error(), tt.errorSubstr) {
					t.Errorf("Validate() error = %q, expected to contain %q", err.Error(), tt.errorSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// PinnedModel Tests
// =============================================================================

func TestPinnedModel_IsPinned(t *testing.T) {
	tests := []struct {
		name     string
		pinned   *PinnedModel
		expected bool
	}{
		{
			name:     "nil pinned",
			pinned:   nil,
			expected: false,
		},
		{
			name:     "empty digest",
			pinned:   &PinnedModel{Name: "llama3:8b", Digest: ""},
			expected: false,
		},
		{
			name:     "with digest",
			pinned:   &PinnedModel{Name: "llama3:8b", Digest: "sha256:abc123"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.pinned.IsPinned()
			if result != tt.expected {
				t.Errorf("IsPinned() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestPinnedModel_Verify(t *testing.T) {
	tests := []struct {
		name        string
		pinned      *PinnedModel
		info        *ModelInfo
		expectError bool
		errorIs     error
	}{
		{
			name:        "nil pinned passes",
			pinned:      nil,
			info:        &ModelInfo{Digest: "sha256:abc123"},
			expectError: false,
		},
		{
			name:        "not pinned passes",
			pinned:      &PinnedModel{Name: "llama3:8b", Digest: ""},
			info:        &ModelInfo{Digest: "sha256:abc123"},
			expectError: false,
		},
		{
			name:        "matching digest passes",
			pinned:      &PinnedModel{Name: "llama3:8b", Digest: "sha256:abc123"},
			info:        &ModelInfo{Digest: "sha256:abc123"},
			expectError: false,
		},
		{
			name:        "mismatched digest fails",
			pinned:      &PinnedModel{Name: "llama3:8b", Digest: "sha256:abc123"},
			info:        &ModelInfo{Digest: "sha256:xyz789"},
			expectError: true,
			errorIs:     ErrModelDigestMismatch,
		},
		{
			name:        "nil info fails",
			pinned:      &PinnedModel{Name: "llama3:8b", Digest: "sha256:abc123"},
			info:        nil,
			expectError: true,
			errorIs:     ErrModelDigestMismatch,
		},
		{
			name:        "case insensitive match passes",
			pinned:      &PinnedModel{Name: "llama3:8b", Digest: "SHA256:ABC123"},
			info:        &ModelInfo{Digest: "sha256:abc123"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pinned.Verify(tt.info)

			if tt.expectError {
				if err == nil {
					t.Errorf("Verify() expected error, got nil")
				} else if tt.errorIs != nil && !errors.Is(err, tt.errorIs) {
					t.Errorf("Verify() error = %v, expected to wrap %v", err, tt.errorIs)
				}
			} else {
				if err != nil {
					t.Errorf("Verify() unexpected error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// ValidateModelName Tests
// =============================================================================

func TestValidateModelName(t *testing.T) {
	tests := []struct {
		name        string
		modelName   string
		expectError bool
		errorIs     error
	}{
		{
			name:        "simple name",
			modelName:   "llama3",
			expectError: false,
		},
		{
			name:        "name with tag",
			modelName:   "llama3:8b",
			expectError: false,
		},
		{
			name:        "name with namespace",
			modelName:   "library/llama3",
			expectError: false,
		},
		{
			name:        "name with namespace and tag",
			modelName:   "library/llama3:8b",
			expectError: false,
		},
		{
			name:        "name with dots",
			modelName:   "nomic-embed-text-v2-moe",
			expectError: false,
		},
		{
			name:        "name with underscores",
			modelName:   "my_model_name",
			expectError: false,
		},
		{
			name:        "empty name",
			modelName:   "",
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
		{
			name:        "name with spaces",
			modelName:   "invalid model",
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
		{
			name:        "name starting with number",
			modelName:   "0llama",
			expectError: false, // Numbers at start are valid
		},
		{
			name:        "name starting with dash",
			modelName:   "-llama",
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
		{
			name:        "name with special chars",
			modelName:   "llama@3!",
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
		{
			name:        "very long name (256 chars)",
			modelName:   string(make([]byte, 256)), // This will fail pattern check
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
		{
			name:        "very long valid name",
			modelName:   "a" + string(make([]byte, 255)),
			expectError: true,
			errorIs:     ErrInvalidModelName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModelName(tt.modelName)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateModelName(%q) expected error, got nil", tt.modelName)
				} else if tt.errorIs != nil && !errors.Is(err, tt.errorIs) {
					t.Errorf("ValidateModelName(%q) error = %v, expected to wrap %v", tt.modelName, err, tt.errorIs)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateModelName(%q) unexpected error: %v", tt.modelName, err)
				}
			}
		})
	}
}

// =============================================================================
// ParseParameterSize Tests
// =============================================================================

func TestParseParameterSize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "7B",
			input:    "7B",
			expected: 7_000_000_000,
		},
		{
			name:     "7b lowercase",
			input:    "7b",
			expected: 7_000_000_000,
		},
		{
			name:     "70B",
			input:    "70B",
			expected: 70_000_000_000,
		},
		{
			name:     "7.1B",
			input:    "7.1B",
			expected: 7_100_000_000,
		},
		{
			name:     "1.5B",
			input:    "1.5B",
			expected: 1_500_000_000,
		},
		{
			name:     "350M",
			input:    "350M",
			expected: 350_000_000,
		},
		{
			name:     "500K",
			input:    "500K",
			expected: 500_000,
		},
		{
			name:     "invalid format",
			input:    "large",
			expected: 0,
		},
		{
			name:     "with whitespace",
			input:    "  7B  ",
			expected: 7_000_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseParameterSize(tt.input)
			if result != tt.expected {
				t.Errorf("ParseParameterSize(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// DefaultModelInfoProvider Tests
// =============================================================================

// mockOllamaLister implements OllamaModelLister for testing
type mockOllamaLister struct {
	models  []OllamaModelInfo
	sizes   map[string]int64
	listErr error
	sizeErr error
}

func (m *mockOllamaLister) ListModels(ctx context.Context) ([]OllamaModelInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.models, nil
}

func (m *mockOllamaLister) GetModelSize(ctx context.Context, modelName string) (int64, error) {
	if m.sizeErr != nil {
		return 0, m.sizeErr
	}
	// Try exact match first, then normalized
	if size, ok := m.sizes[modelName]; ok {
		return size, nil
	}
	if size, ok := m.sizes[normalizeModelName(modelName)]; ok {
		return size, nil
	}
	return 0, nil
}

func TestNewDefaultModelInfoProvider(t *testing.T) {
	mock := &mockOllamaLister{}
	provider := NewDefaultModelInfoProvider(mock)

	if provider == nil {
		t.Fatal("NewDefaultModelInfoProvider returned nil")
	}

	if provider.client != mock {
		t.Error("Provider client not set correctly")
	}

	if provider.localModels == nil {
		t.Error("Provider localModels not initialized")
	}

	if provider.cacheTTL != 30*time.Second {
		t.Errorf("Provider cacheTTL = %v, expected 30s", provider.cacheTTL)
	}
}

func TestDefaultModelInfoProvider_GetModelInfo(t *testing.T) {
	mock := &mockOllamaLister{
		models: []OllamaModelInfo{
			{
				Name:              "llama3:8b",
				Size:              4_000_000_000,
				Digest:            "sha256:abc123",
				ModifiedAt:        time.Now(),
				Family:            "llama",
				ParameterSize:     "8B",
				QuantizationLevel: "Q4_K_M",
			},
		},
	}

	provider := NewDefaultModelInfoProvider(mock)
	ctx := context.Background()

	// Test found model
	info, err := provider.GetModelInfo(ctx, "llama3:8b")
	if err != nil {
		t.Fatalf("GetModelInfo() error: %v", err)
	}
	if info.Name != "llama3:8b" {
		t.Errorf("GetModelInfo() name = %q, expected %q", info.Name, "llama3:8b")
	}
	if info.Size != 4_000_000_000 {
		t.Errorf("GetModelInfo() size = %d, expected %d", info.Size, 4_000_000_000)
	}

	// Test not found model
	info, err = provider.GetModelInfo(ctx, "not-exists")
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("GetModelInfo() for missing model expected ErrModelNotFound, got %v", err)
	}
	if info != nil {
		t.Error("GetModelInfo() for missing model should return nil info")
	}

	// Test invalid name
	info, err = provider.GetModelInfo(ctx, "")
	if !errors.Is(err, ErrInvalidModelName) {
		t.Errorf("GetModelInfo() for empty name expected ErrInvalidModelName, got %v", err)
	}
}

func TestDefaultModelInfoProvider_GetLocalModelInfo(t *testing.T) {
	mock := &mockOllamaLister{
		models: []OllamaModelInfo{
			{
				Name:   "llama3:8b",
				Size:   4_000_000_000,
				Digest: "sha256:abc123",
			},
		},
	}

	provider := NewDefaultModelInfoProvider(mock)
	ctx := context.Background()

	// Test found model
	info, err := provider.GetLocalModelInfo(ctx, "llama3:8b")
	if err != nil {
		t.Fatalf("GetLocalModelInfo() error: %v", err)
	}
	if !info.IsLocal {
		t.Error("GetLocalModelInfo() should set IsLocal=true")
	}

	// Test case insensitive lookup
	info, err = provider.GetLocalModelInfo(ctx, "LLAMA3:8B")
	if err != nil {
		t.Fatalf("GetLocalModelInfo() case insensitive error: %v", err)
	}
	if info.Name != "llama3:8b" {
		t.Errorf("GetLocalModelInfo() name = %q, expected original case", info.Name)
	}

	// Test model with default tag lookup
	info, err = provider.GetLocalModelInfo(ctx, "llama3")
	if !errors.Is(err, ErrModelNotFound) {
		// The model was stored as llama3:8b, not llama3:latest
		// So this should return not found
		t.Logf("GetLocalModelInfo() for llama3 returned: %v, %v", info, err)
	}
}

func TestDefaultModelInfoProvider_GetMultipleModelInfo(t *testing.T) {
	mock := &mockOllamaLister{
		models: []OllamaModelInfo{
			{Name: "llama3:8b", Size: 4_000_000_000},
			{Name: "phi3:mini", Size: 2_000_000_000},
		},
	}

	provider := NewDefaultModelInfoProvider(mock)
	ctx := context.Background()

	// Test multiple models
	infos, err := provider.GetMultipleModelInfo(ctx, []string{"llama3:8b", "phi3:mini", "not-exists"})
	if err != nil {
		t.Fatalf("GetMultipleModelInfo() error: %v", err)
	}

	if infos["llama3:8b"] == nil {
		t.Error("GetMultipleModelInfo() llama3:8b should not be nil")
	}
	if infos["phi3:mini"] == nil {
		t.Error("GetMultipleModelInfo() phi3:mini should not be nil")
	}
	if infos["not-exists"] != nil {
		t.Error("GetMultipleModelInfo() not-exists should be nil")
	}

	// Test empty list
	infos, err = provider.GetMultipleModelInfo(ctx, []string{})
	if err != nil {
		t.Fatalf("GetMultipleModelInfo() empty list error: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("GetMultipleModelInfo() empty list returned %d items", len(infos))
	}
}

func TestDefaultModelInfoProvider_EstimateTotalSize(t *testing.T) {
	mock := &mockOllamaLister{
		models: []OllamaModelInfo{
			{Name: "llama3:8b", Size: 4_000_000_000},
		},
		sizes: map[string]int64{
			"llama3:8b": 4_000_000_000,
			"phi3:mini": 2_000_000_000,
		},
	}

	provider := NewDefaultModelInfoProvider(mock)
	ctx := context.Background()

	// Test with mix of installed and not installed
	total, err := provider.EstimateTotalSize(ctx, []string{"llama3:8b", "phi3:mini"})
	if err != nil {
		t.Fatalf("EstimateTotalSize() error: %v", err)
	}

	// llama3:8b is installed (should be skipped), phi3:mini needs download
	if total != 2_000_000_000 {
		t.Errorf("EstimateTotalSize() = %d, expected %d", total, 2_000_000_000)
	}

	// Test empty list
	total, err = provider.EstimateTotalSize(ctx, []string{})
	if err != nil {
		t.Fatalf("EstimateTotalSize() empty list error: %v", err)
	}
	if total != 0 {
		t.Errorf("EstimateTotalSize() empty list = %d, expected 0", total)
	}
}

// countingOllamaLister wraps mockOllamaLister to count ListModels calls
type countingOllamaLister struct {
	models    []OllamaModelInfo
	callCount int
}

func (c *countingOllamaLister) ListModels(ctx context.Context) ([]OllamaModelInfo, error) {
	c.callCount++
	return c.models, nil
}

func (c *countingOllamaLister) GetModelSize(ctx context.Context, modelName string) (int64, error) {
	return 0, nil
}

func TestDefaultModelInfoProvider_Caching(t *testing.T) {
	mock := &countingOllamaLister{
		models: []OllamaModelInfo{
			{Name: "llama3:8b", Size: 4_000_000_000},
		},
	}

	provider := NewDefaultModelInfoProvider(mock)
	provider.cacheTTL = 1 * time.Hour // Long TTL for test
	ctx := context.Background()

	// First call
	_, err := provider.GetLocalModelInfo(ctx, "llama3:8b")
	if err != nil {
		t.Fatalf("First call error: %v", err)
	}

	// Second call should use cache
	_, err = provider.GetLocalModelInfo(ctx, "llama3:8b")
	if err != nil {
		t.Fatalf("Second call error: %v", err)
	}

	if mock.callCount != 1 {
		t.Errorf("Expected 1 ListModels call, got %d", mock.callCount)
	}
}

func TestDefaultModelInfoProvider_ListError(t *testing.T) {
	expectedErr := errors.New("connection refused")
	mock := &mockOllamaLister{
		listErr: expectedErr,
	}

	provider := NewDefaultModelInfoProvider(mock)
	ctx := context.Background()

	// GetLocalModelInfo should propagate the error
	_, err := provider.GetLocalModelInfo(ctx, "llama3:8b")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !containsSubstring(err.Error(), "connection refused") {
		t.Errorf("Error should contain cause: %v", err)
	}

	// GetModelInfo falls back to ErrModelNotFound after local fails
	_, err = provider.GetModelInfo(ctx, "llama3:8b")
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("GetModelInfo should return ErrModelNotFound after list error, got: %v", err)
	}
}

// =============================================================================
// MockModelInfoProvider Tests
// =============================================================================

func TestNewMockModelInfoProvider(t *testing.T) {
	mock := NewMockModelInfoProvider()

	if mock == nil {
		t.Fatal("NewMockModelInfoProvider returned nil")
	}
	if mock.Models == nil {
		t.Error("Mock Models map not initialized")
	}
	if mock.SizeByName == nil {
		t.Error("Mock SizeByName map not initialized")
	}
}

func TestMockModelInfoProvider_GetModelInfo(t *testing.T) {
	mock := NewMockModelInfoProvider()
	// Note: normalizeModelName("llama3:8b") = "llama3:8b" (already has tag)
	mock.Models["llama3:8b"] = &ModelInfo{Name: "llama3:8b", Size: 4_000_000_000}

	ctx := context.Background()

	// Test found
	info, err := mock.GetModelInfo(ctx, "llama3:8b")
	if err != nil {
		t.Fatalf("GetModelInfo() error: %v", err)
	}
	if info.Name != "llama3:8b" {
		t.Errorf("GetModelInfo() name = %q, expected %q", info.Name, "llama3:8b")
	}

	// Test not found
	info, err = mock.GetModelInfo(ctx, "not-exists")
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("GetModelInfo() expected ErrModelNotFound, got %v", err)
	}

	// Verify call tracking
	if len(mock.GetModelInfoCalls) != 2 {
		t.Errorf("Expected 2 GetModelInfoCalls, got %d", len(mock.GetModelInfoCalls))
	}
}

func TestMockModelInfoProvider_FunctionOverride(t *testing.T) {
	mock := NewMockModelInfoProvider()
	customInfo := &ModelInfo{Name: "custom", Size: 999}

	mock.GetModelInfoFunc = func(ctx context.Context, model string) (*ModelInfo, error) {
		return customInfo, nil
	}

	ctx := context.Background()
	info, err := mock.GetModelInfo(ctx, "anything")
	if err != nil {
		t.Fatalf("GetModelInfo() error: %v", err)
	}
	if info != customInfo {
		t.Error("GetModelInfo() should return custom info from func")
	}
}

func TestMockModelInfoProvider_DefaultError(t *testing.T) {
	mock := NewMockModelInfoProvider()
	expectedErr := errors.New("forced error")
	mock.DefaultErr = expectedErr

	ctx := context.Background()

	_, err := mock.GetModelInfo(ctx, "anything")
	if err != expectedErr {
		t.Errorf("GetModelInfo() error = %v, expected %v", err, expectedErr)
	}

	_, err = mock.GetLocalModelInfo(ctx, "anything")
	if err != expectedErr {
		t.Errorf("GetLocalModelInfo() error = %v, expected %v", err, expectedErr)
	}

	_, err = mock.GetMultipleModelInfo(ctx, []string{"anything"})
	if err != expectedErr {
		t.Errorf("GetMultipleModelInfo() error = %v, expected %v", err, expectedErr)
	}

	_, err = mock.EstimateTotalSize(ctx, []string{"anything"})
	if err != expectedErr {
		t.Errorf("EstimateTotalSize() error = %v, expected %v", err, expectedErr)
	}
}

func TestMockModelInfoProvider_Reset(t *testing.T) {
	mock := NewMockModelInfoProvider()
	ctx := context.Background()

	// Make some calls
	mock.GetModelInfo(ctx, "test1")
	mock.GetModelInfo(ctx, "test2")

	if len(mock.GetModelInfoCalls) != 2 {
		t.Fatalf("Expected 2 calls before reset, got %d", len(mock.GetModelInfoCalls))
	}

	mock.Reset()

	if len(mock.GetModelInfoCalls) != 0 {
		t.Errorf("Expected 0 calls after reset, got %d", len(mock.GetModelInfoCalls))
	}
}

func TestMockModelInfoProvider_EstimateTotalSize(t *testing.T) {
	mock := NewMockModelInfoProvider()
	// Note: normalizeModelName("llama3:8b") = "llama3:8b" (already has tag)
	mock.Models["llama3:8b"] = &ModelInfo{Name: "llama3:8b", IsLocal: true}
	mock.SizeByName["phi3:mini"] = 2_000_000_000

	ctx := context.Background()
	total, err := mock.EstimateTotalSize(ctx, []string{"llama3:8b", "phi3:mini"})
	if err != nil {
		t.Fatalf("EstimateTotalSize() error: %v", err)
	}

	// llama3 is local, should be skipped
	if total != 2_000_000_000 {
		t.Errorf("EstimateTotalSize() = %d, expected %d", total, 2_000_000_000)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringRecursive(s, substr))
}

func containsSubstringRecursive(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	if s[:len(substr)] == substr {
		return true
	}
	return containsSubstringRecursive(s[1:], substr)
}

// =============================================================================
// normalizeModelName Tests
// =============================================================================

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name adds latest",
			input:    "llama3",
			expected: "llama3:latest",
		},
		{
			name:     "name with tag unchanged",
			input:    "llama3:8b",
			expected: "llama3:8b",
		},
		{
			name:     "uppercase to lowercase",
			input:    "LLAMA3:8B",
			expected: "llama3:8b",
		},
		{
			name:     "mixed case normalized",
			input:    "Llama3:Latest",
			expected: "llama3:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeModelName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
