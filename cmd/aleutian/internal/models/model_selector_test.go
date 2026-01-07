package models

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// SelectionOpts Tests
// =============================================================================

func TestSelectionOpts_HasCapabilityRequirement(t *testing.T) {
	tests := []struct {
		name     string
		opts     SelectionOpts
		expected bool
	}{
		{
			name:     "empty capabilities",
			opts:     SelectionOpts{},
			expected: false,
		},
		{
			name:     "nil capabilities",
			opts:     SelectionOpts{RequiredCapabilities: nil},
			expected: false,
		},
		{
			name:     "has capabilities",
			opts:     SelectionOpts{RequiredCapabilities: []string{"code"}},
			expected: true,
		},
		{
			name:     "multiple capabilities",
			opts:     SelectionOpts{RequiredCapabilities: []string{"code", "math"}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.opts.HasCapabilityRequirement()
			if result != tt.expected {
				t.Errorf("HasCapabilityRequirement() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// ModelCatalogEntry Tests
// =============================================================================

func TestModelCatalogEntry_HasCapability(t *testing.T) {
	entry := ModelCatalogEntry{
		Name:         "llama3:8b",
		Capabilities: []string{"general", "code", "math"},
	}

	tests := []struct {
		name       string
		capability string
		expected   bool
	}{
		{
			name:       "exact match",
			capability: "code",
			expected:   true,
		},
		{
			name:       "case insensitive",
			capability: "CODE",
			expected:   true,
		},
		{
			name:       "mixed case",
			capability: "Code",
			expected:   true,
		},
		{
			name:       "not present",
			capability: "vision",
			expected:   false,
		},
		{
			name:       "empty string",
			capability: "",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entry.HasCapability(tt.capability)
			if result != tt.expected {
				t.Errorf("HasCapability(%q) = %v, expected %v", tt.capability, result, tt.expected)
			}
		})
	}
}

func TestModelCatalogEntry_HasAllCapabilities(t *testing.T) {
	entry := ModelCatalogEntry{
		Capabilities: []string{"general", "code", "math"},
	}

	tests := []struct {
		name     string
		caps     []string
		expected bool
	}{
		{
			name:     "empty list",
			caps:     []string{},
			expected: true,
		},
		{
			name:     "nil list",
			caps:     nil,
			expected: true,
		},
		{
			name:     "single present",
			caps:     []string{"code"},
			expected: true,
		},
		{
			name:     "all present",
			caps:     []string{"code", "math"},
			expected: true,
		},
		{
			name:     "one missing",
			caps:     []string{"code", "vision"},
			expected: false,
		},
		{
			name:     "all missing",
			caps:     []string{"vision", "audio"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entry.HasAllCapabilities(tt.caps)
			if result != tt.expected {
				t.Errorf("HasAllCapabilities(%v) = %v, expected %v", tt.caps, result, tt.expected)
			}
		})
	}
}

func TestModelCatalogEntry_FitsRAM(t *testing.T) {
	entry := ModelCatalogEntry{
		MinRAM_GB: 8,
	}

	tests := []struct {
		name     string
		maxGB    int
		expected bool
	}{
		{
			name:     "zero limit (no constraint)",
			maxGB:    0,
			expected: true,
		},
		{
			name:     "negative limit (no constraint)",
			maxGB:    -1,
			expected: true,
		},
		{
			name:     "exact fit",
			maxGB:    8,
			expected: true,
		},
		{
			name:     "plenty of room",
			maxGB:    16,
			expected: true,
		},
		{
			name:     "not enough RAM",
			maxGB:    4,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entry.FitsRAM(tt.maxGB)
			if result != tt.expected {
				t.Errorf("FitsRAM(%d) = %v, expected %v", tt.maxGB, result, tt.expected)
			}
		})
	}
}

func TestModelCatalogEntry_MeetsContextRequirement(t *testing.T) {
	entry := ModelCatalogEntry{
		ContextLength: 8192,
	}

	tests := []struct {
		name       string
		minContext int
		expected   bool
	}{
		{
			name:       "zero requirement (no constraint)",
			minContext: 0,
			expected:   true,
		},
		{
			name:       "negative requirement (no constraint)",
			minContext: -1,
			expected:   true,
		},
		{
			name:       "exact match",
			minContext: 8192,
			expected:   true,
		},
		{
			name:       "less than available",
			minContext: 4096,
			expected:   true,
		},
		{
			name:       "more than available",
			minContext: 16384,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entry.MeetsContextRequirement(tt.minContext)
			if result != tt.expected {
				t.Errorf("MeetsContextRequirement(%d) = %v, expected %v", tt.minContext, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// SystemHardwareDetector Tests
// =============================================================================

func TestNewSystemHardwareDetector(t *testing.T) {
	detector := NewSystemHardwareDetector()
	if detector == nil {
		t.Fatal("NewSystemHardwareDetector returned nil")
	}

	// Should start undetected
	if detector.detected {
		t.Error("New detector should not be detected yet")
	}
}

func TestSystemHardwareDetector_GetTotalRAM_GB(t *testing.T) {
	detector := NewSystemHardwareDetector()
	total := detector.GetTotalRAM_GB()

	// Should return at least 1 GB
	if total < 1 {
		t.Errorf("GetTotalRAM_GB() = %d, expected >= 1", total)
	}

	// Should be reasonable (< 1TB)
	if total > 1024 {
		t.Errorf("GetTotalRAM_GB() = %d, suspiciously high", total)
	}

	// Second call should return same value (cached)
	total2 := detector.GetTotalRAM_GB()
	if total2 != total {
		t.Errorf("Second call returned %d, expected %d (cached)", total2, total)
	}
}

func TestSystemHardwareDetector_GetAvailableRAM_GB(t *testing.T) {
	detector := NewSystemHardwareDetector()
	available := detector.GetAvailableRAM_GB()

	// Should return at least 1 GB
	if available < 1 {
		t.Errorf("GetAvailableRAM_GB() = %d, expected >= 1", available)
	}

	// Should not exceed total
	total := detector.GetTotalRAM_GB()
	if available > total {
		t.Errorf("GetAvailableRAM_GB() = %d, exceeds total %d", available, total)
	}
}

func TestSystemHardwareDetector_CachesValues(t *testing.T) {
	detector := NewSystemHardwareDetector()

	// First call triggers detection
	_ = detector.GetTotalRAM_GB()

	// Should be marked as detected
	detector.mu.RLock()
	detected := detector.detected
	detector.mu.RUnlock()

	if !detected {
		t.Error("Expected detector.detected = true after GetTotalRAM_GB")
	}
}

// =============================================================================
// DefaultModelSelector Tests
// =============================================================================

func TestNewDefaultModelSelector(t *testing.T) {
	selector := NewDefaultModelSelector(nil)
	if selector == nil {
		t.Fatal("NewDefaultModelSelector returned nil")
	}

	// Should have catalog populated
	if len(selector.catalog) == 0 {
		t.Error("Selector catalog should not be empty")
	}

	// Should have hardware detector
	if selector.hardware == nil {
		t.Error("Selector should have hardware detector")
	}
}

func TestNewDefaultModelSelector_WithHardware(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 32, AvailableRAM: 24}
	selector := NewDefaultModelSelector(mock)

	if selector.hardware != mock {
		t.Error("Selector should use provided hardware detector")
	}
}

func TestNewDefaultModelSelectorWithCatalog(t *testing.T) {
	customCatalog := map[string][]ModelCatalogEntry{
		"test": {{Name: "test:model", MinRAM_GB: 4, Priority: 100}},
	}
	selector := NewDefaultModelSelectorWithCatalog(customCatalog, nil)

	if len(selector.catalog["test"]) != 1 {
		t.Error("Selector should use custom catalog")
	}
}

func TestDefaultModelSelector_SelectModel(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 16, AvailableRAM: 12}
	selector := NewDefaultModelSelector(mock)
	ctx := context.Background()

	// Test basic selection
	model, err := selector.SelectModel(ctx, "llm", SelectionOpts{})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}
	if model == "" {
		t.Error("SelectModel() returned empty model")
	}
	t.Logf("Selected model for 12GB available: %s", model)

	// Should select a model that fits in 12GB
	// Based on catalog, llama3:8b requires 8GB and has high priority
}

func TestDefaultModelSelector_SelectModel_WithRAMConstraint(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 64, AvailableRAM: 48}
	selector := NewDefaultModelSelector(mock)
	ctx := context.Background()

	// Test with explicit RAM constraint
	model, err := selector.SelectModel(ctx, "llm", SelectionOpts{
		MaxRAM_GB: 4,
	})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}

	// Should select a model that fits in 4GB (phi3:mini or tinyllama)
	t.Logf("Selected model for 4GB constraint: %s", model)
}

func TestDefaultModelSelector_SelectModel_WithCapabilities(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 16, AvailableRAM: 12}
	selector := NewDefaultModelSelector(mock)
	ctx := context.Background()

	// Test with capability requirement
	model, err := selector.SelectModel(ctx, "llm", SelectionOpts{
		RequiredCapabilities: []string{"code"},
	})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}

	// Verify selected model has code capability
	catalog := selector.GetModelCatalog("llm")
	var found bool
	for _, entry := range catalog {
		if entry.Name == model {
			if !entry.HasCapability("code") {
				t.Errorf("Selected model %s doesn't have 'code' capability", model)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Selected model %s not found in catalog", model)
	}
}

func TestDefaultModelSelector_SelectModel_UnknownCategory(t *testing.T) {
	selector := NewDefaultModelSelector(nil)
	ctx := context.Background()

	_, err := selector.SelectModel(ctx, "nonexistent", SelectionOpts{})
	if !errors.Is(err, ErrNoSuitableModel) {
		t.Errorf("Expected ErrNoSuitableModel, got: %v", err)
	}
}

func TestDefaultModelSelector_SelectModel_ImpossibleConstraints(t *testing.T) {
	selector := NewDefaultModelSelector(nil)
	ctx := context.Background()

	// Require capability that no LLM has
	_, err := selector.SelectModel(ctx, "llm", SelectionOpts{
		RequiredCapabilities: []string{"nonexistent_capability_xyz"},
	})
	if !errors.Is(err, ErrNoSuitableModel) {
		t.Errorf("Expected ErrNoSuitableModel for impossible constraints, got: %v", err)
	}
}

func TestDefaultModelSelector_SelectModelWithFallbacks(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 16, AvailableRAM: 12}
	selector := NewDefaultModelSelector(mock)
	ctx := context.Background()

	chain, err := selector.SelectModelWithFallbacks(ctx, "llm", SelectionOpts{}, 2)
	if err != nil {
		t.Fatalf("SelectModelWithFallbacks() error: %v", err)
	}

	if chain.Primary == "" {
		t.Error("Chain should have primary model")
	}

	if len(chain.Fallbacks) > 2 {
		t.Errorf("Chain should have at most 2 fallbacks, got %d", len(chain.Fallbacks))
	}

	// Fallbacks should be different from primary
	for _, fb := range chain.Fallbacks {
		if fb == chain.Primary {
			t.Errorf("Fallback %s should not equal primary %s", fb, chain.Primary)
		}
	}

	t.Logf("Chain: primary=%s, fallbacks=%v", chain.Primary, chain.Fallbacks)
}

func TestDefaultModelSelector_SelectModelWithFallbacks_ZeroFallbacks(t *testing.T) {
	selector := NewDefaultModelSelector(nil)
	ctx := context.Background()

	chain, err := selector.SelectModelWithFallbacks(ctx, "llm", SelectionOpts{}, 0)
	if err != nil {
		t.Fatalf("SelectModelWithFallbacks() error: %v", err)
	}

	if chain.Primary == "" {
		t.Error("Chain should have primary model")
	}

	if len(chain.Fallbacks) != 0 {
		t.Errorf("Expected 0 fallbacks, got %d", len(chain.Fallbacks))
	}
}

func TestDefaultModelSelector_GetModelCatalog(t *testing.T) {
	selector := NewDefaultModelSelector(nil)

	// Test existing category
	catalog := selector.GetModelCatalog("llm")
	if len(catalog) == 0 {
		t.Error("LLM catalog should not be empty")
	}

	// Verify it's a copy
	originalLen := len(catalog)
	catalog = append(catalog, ModelCatalogEntry{Name: "test"})
	catalog2 := selector.GetModelCatalog("llm")
	if len(catalog2) != originalLen {
		t.Error("GetModelCatalog should return a copy")
	}

	// Test unknown category
	catalog = selector.GetModelCatalog("nonexistent")
	if catalog != nil {
		t.Error("Unknown category should return nil")
	}

	// Test case insensitivity
	catalog = selector.GetModelCatalog("LLM")
	if len(catalog) == 0 {
		t.Error("Category lookup should be case-insensitive")
	}
}

func TestDefaultModelSelector_FilterModels(t *testing.T) {
	selector := NewDefaultModelSelector(nil)

	// Test with RAM constraint
	matches := selector.FilterModels("llm", SelectionOpts{
		MaxRAM_GB: 8,
	})

	for _, m := range matches {
		if m.MinRAM_GB > 8 {
			t.Errorf("Model %s (MinRAM=%d) should not match MaxRAM=8", m.Name, m.MinRAM_GB)
		}
	}

	// Test with exclude list
	matches = selector.FilterModels("llm", SelectionOpts{
		ExcludeModels: []string{"llama3:8b", "phi3:mini"},
	})

	for _, m := range matches {
		if m.Name == "llama3:8b" || m.Name == "phi3:mini" {
			t.Errorf("Model %s should be excluded", m.Name)
		}
	}

	// Test with preferred family
	matches = selector.FilterModels("llm", SelectionOpts{
		PreferredFamily: "llama",
	})

	for _, m := range matches {
		if m.Family != "llama" {
			t.Errorf("Model %s (family=%s) should not match PreferredFamily=llama", m.Name, m.Family)
		}
	}
}

func TestDefaultModelSelector_FilterModels_ContextWindow(t *testing.T) {
	selector := NewDefaultModelSelector(nil)

	matches := selector.FilterModels("llm", SelectionOpts{
		MinContextWindow: 16000,
	})

	for _, m := range matches {
		if m.ContextLength < 16000 {
			t.Errorf("Model %s (context=%d) should not match MinContextWindow=16000",
				m.Name, m.ContextLength)
		}
	}
}

func TestDefaultModelSelector_AddModel(t *testing.T) {
	selector := NewDefaultModelSelector(nil)

	// Get original count
	original := selector.GetModelCatalog("llm")
	originalLen := len(original)

	// Add new model
	selector.AddModel("llm", ModelCatalogEntry{
		Name:      "test:custom",
		MinRAM_GB: 4,
		Priority:  50,
	})

	// Verify it was added
	updated := selector.GetModelCatalog("llm")
	if len(updated) != originalLen+1 {
		t.Errorf("Expected %d models, got %d", originalLen+1, len(updated))
	}

	// Verify new model is present
	var found bool
	for _, m := range updated {
		if m.Name == "test:custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Added model not found in catalog")
	}
}

func TestDefaultModelSelector_AddModel_NewCategory(t *testing.T) {
	selector := NewDefaultModelSelector(nil)

	// Add model to new category
	selector.AddModel("custom", ModelCatalogEntry{
		Name:      "custom:model",
		MinRAM_GB: 4,
	})

	catalog := selector.GetModelCatalog("custom")
	if len(catalog) != 1 {
		t.Errorf("Expected 1 model in new category, got %d", len(catalog))
	}
}

// =============================================================================
// DefaultModelCatalog Tests
// =============================================================================

func TestDefaultModelCatalog_HasExpectedCategories(t *testing.T) {
	expectedCategories := []string{"llm", "embedding", "vision"}

	for _, cat := range expectedCategories {
		if _, ok := DefaultModelCatalog[cat]; !ok {
			t.Errorf("DefaultModelCatalog missing category: %s", cat)
		}
	}
}

func TestDefaultModelCatalog_HasExpectedModels(t *testing.T) {
	expectedModels := map[string][]string{
		"llm":       {"llama3:8b", "phi3:mini", "tinyllama"},
		"embedding": {"nomic-embed-text-v2-moe"},
	}

	for category, models := range expectedModels {
		catalog := DefaultModelCatalog[category]
		for _, model := range models {
			var found bool
			for _, entry := range catalog {
				if entry.Name == model {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("DefaultModelCatalog[%s] missing model: %s", category, model)
			}
		}
	}
}

func TestDefaultModelCatalog_ModelsHaveRequiredFields(t *testing.T) {
	for category, entries := range DefaultModelCatalog {
		for _, entry := range entries {
			if entry.Name == "" {
				t.Errorf("Model in %s has empty name", category)
			}
			if entry.MinRAM_GB <= 0 {
				t.Errorf("Model %s has invalid MinRAM_GB: %d", entry.Name, entry.MinRAM_GB)
			}
			if entry.Priority <= 0 {
				t.Errorf("Model %s has invalid Priority: %d", entry.Name, entry.Priority)
			}
		}
	}
}

// =============================================================================
// MockModelSelector Tests
// =============================================================================

func TestNewMockModelSelector(t *testing.T) {
	mock := NewMockModelSelector()
	if mock == nil {
		t.Fatal("NewMockModelSelector returned nil")
	}

	if mock.DefaultModel == "" {
		t.Error("Mock should have default model")
	}

	if mock.DefaultChain == nil {
		t.Error("Mock should have default chain")
	}
}

func TestMockModelSelector_SelectModel(t *testing.T) {
	mock := NewMockModelSelector()
	ctx := context.Background()

	model, err := mock.SelectModel(ctx, "llm", SelectionOpts{})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}

	if model != mock.DefaultModel {
		t.Errorf("SelectModel() = %q, expected %q", model, mock.DefaultModel)
	}

	// Verify call tracking
	if len(mock.SelectModelCalls) != 1 {
		t.Errorf("Expected 1 SelectModelCall, got %d", len(mock.SelectModelCalls))
	}
	if mock.SelectModelCalls[0].Category != "llm" {
		t.Errorf("Expected category 'llm', got %q", mock.SelectModelCalls[0].Category)
	}
}

func TestMockModelSelector_SelectModel_CustomFunc(t *testing.T) {
	mock := NewMockModelSelector()
	mock.SelectModelFunc = func(ctx context.Context, category string, opts SelectionOpts) (string, error) {
		return "custom:model", nil
	}

	ctx := context.Background()
	model, err := mock.SelectModel(ctx, "llm", SelectionOpts{})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}

	if model != "custom:model" {
		t.Errorf("SelectModel() = %q, expected 'custom:model'", model)
	}
}

func TestMockModelSelector_SelectModel_Error(t *testing.T) {
	mock := NewMockModelSelector()
	expectedErr := errors.New("test error")
	mock.DefaultErr = expectedErr

	ctx := context.Background()
	_, err := mock.SelectModel(ctx, "llm", SelectionOpts{})
	if err != expectedErr {
		t.Errorf("SelectModel() error = %v, expected %v", err, expectedErr)
	}
}

func TestMockModelSelector_Reset(t *testing.T) {
	mock := NewMockModelSelector()
	ctx := context.Background()

	// Make some calls
	mock.SelectModel(ctx, "llm", SelectionOpts{})
	mock.GetModelCatalog("llm")

	if len(mock.SelectModelCalls) != 1 || len(mock.GetCatalogCalls) != 1 {
		t.Fatal("Expected calls to be tracked")
	}

	mock.Reset()

	if len(mock.SelectModelCalls) != 0 || len(mock.GetCatalogCalls) != 0 {
		t.Error("Reset should clear all call tracking")
	}
}

// =============================================================================
// MockHardwareDetector Tests
// =============================================================================

func TestMockHardwareDetector(t *testing.T) {
	mock := &MockHardwareDetector{TotalRAM: 32, AvailableRAM: 24}

	if mock.GetTotalRAM_GB() != 32 {
		t.Errorf("GetTotalRAM_GB() = %d, expected 32", mock.GetTotalRAM_GB())
	}

	if mock.GetAvailableRAM_GB() != 24 {
		t.Errorf("GetAvailableRAM_GB() = %d, expected 24", mock.GetAvailableRAM_GB())
	}
}

// =============================================================================
// Integration-like Tests
// =============================================================================

func TestModelSelection_EndToEnd(t *testing.T) {
	// Simulate different hardware configurations
	configs := []struct {
		name        string
		totalRAM    int
		availRAM    int
		expectModel string // Partial match
	}{
		{"low_end_4gb", 4, 3, ""},      // Should get smallest model
		{"mid_range_16gb", 16, 12, ""}, // Should get medium model
		{"high_end_64gb", 64, 48, ""},  // Should get larger model
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			mock := &MockHardwareDetector{TotalRAM: cfg.totalRAM, AvailableRAM: cfg.availRAM}
			selector := NewDefaultModelSelector(mock)
			ctx := context.Background()

			model, err := selector.SelectModel(ctx, "llm", SelectionOpts{})
			if err != nil {
				t.Fatalf("SelectModel() error: %v", err)
			}

			t.Logf("Config %s (RAM=%dGB): selected %s", cfg.name, cfg.availRAM, model)

			// Verify selected model fits available RAM
			catalog := selector.GetModelCatalog("llm")
			for _, entry := range catalog {
				if entry.Name == model {
					if entry.MinRAM_GB > cfg.availRAM {
						t.Errorf("Selected model %s (MinRAM=%d) doesn't fit in %dGB",
							model, entry.MinRAM_GB, cfg.availRAM)
					}
					break
				}
			}
		})
	}
}

func TestModelSelection_EmbeddingModels(t *testing.T) {
	selector := NewDefaultModelSelector(nil)
	ctx := context.Background()

	model, err := selector.SelectModel(ctx, "embedding", SelectionOpts{})
	if err != nil {
		t.Fatalf("SelectModel() error: %v", err)
	}

	// Should select nomic-embed-text as it has highest priority
	t.Logf("Selected embedding model: %s", model)

	// Verify it's an embedding model
	catalog := selector.GetModelCatalog("embedding")
	var found bool
	for _, entry := range catalog {
		if entry.Name == model {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Selected model %s not found in embedding catalog", model)
	}
}
