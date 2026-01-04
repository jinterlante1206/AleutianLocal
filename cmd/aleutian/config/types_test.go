// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package config contains unit tests for configuration types.

# Testing Strategy

These tests verify:
  - Default values are correctly applied
  - Getter methods return expected values
  - ConfigMeta is properly initialized
  - ForecastMode validation works correctly
*/
package config

import (
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// ForecastMode Tests
// -----------------------------------------------------------------------------

// TestForecastMode_IsValid verifies the IsValid method.
func TestForecastMode_IsValid(t *testing.T) {
	tests := []struct {
		mode     ForecastMode
		expected bool
	}{
		{ForecastModeStandalone, true},
		{ForecastModeSapheneia, true},
		{ForecastMode("invalid"), false},
		{ForecastMode(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.expected {
				t.Errorf("ForecastMode(%q).IsValid() = %v, want %v",
					tt.mode, got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// OllamaConfig Tests
// -----------------------------------------------------------------------------

// TestOllamaConfig_GetEmbeddingModel verifies default fallback.
func TestOllamaConfig_GetEmbeddingModel(t *testing.T) {
	tests := []struct {
		name     string
		config   OllamaConfig
		expected string
	}{
		{
			name:     "returns configured value",
			config:   OllamaConfig{EmbeddingModel: "custom-embed"},
			expected: "custom-embed",
		},
		{
			name:     "returns default when empty",
			config:   OllamaConfig{EmbeddingModel: ""},
			expected: DefaultEmbeddingModel,
		},
		{
			name:     "returns default for zero value",
			config:   OllamaConfig{},
			expected: DefaultEmbeddingModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetEmbeddingModel(); got != tt.expected {
				t.Errorf("GetEmbeddingModel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestOllamaConfig_GetDiskLimitGB verifies default fallback.
func TestOllamaConfig_GetDiskLimitGB(t *testing.T) {
	tests := []struct {
		name     string
		config   OllamaConfig
		expected int64
	}{
		{
			name:     "returns configured value",
			config:   OllamaConfig{DiskLimitGB: 100},
			expected: 100,
		},
		{
			name:     "returns default when zero",
			config:   OllamaConfig{DiskLimitGB: 0},
			expected: DefaultDiskLimitGB,
		},
		{
			name:     "returns default when negative",
			config:   OllamaConfig{DiskLimitGB: -10},
			expected: DefaultDiskLimitGB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetDiskLimitGB(); got != tt.expected {
				t.Errorf("GetDiskLimitGB() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestOllamaConfig_GetBaseURL verifies default fallback.
func TestOllamaConfig_GetBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		config   OllamaConfig
		expected string
	}{
		{
			name:     "returns configured value",
			config:   OllamaConfig{BaseURL: "http://custom:11434"},
			expected: "http://custom:11434",
		},
		{
			name:     "returns default when empty",
			config:   OllamaConfig{BaseURL: ""},
			expected: DefaultOllamaHostURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetBaseURL(); got != tt.expected {
				t.Errorf("GetBaseURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ConfigMeta Tests
// -----------------------------------------------------------------------------

// TestNewConfigMeta verifies metadata initialization.
func TestNewConfigMeta(t *testing.T) {
	before := time.Now().UnixMilli()
	meta := newConfigMeta()
	after := time.Now().UnixMilli()

	// Check version
	if meta.Version != CurrentConfigVersion {
		t.Errorf("Version = %q, want %q", meta.Version, CurrentConfigVersion)
	}

	// Check ModifiedBy
	if meta.ModifiedBy != "aleutian-cli" {
		t.Errorf("ModifiedBy = %q, want %q", meta.ModifiedBy, "aleutian-cli")
	}

	// Verify timestamps are within bounds
	if meta.CreatedAt < before || meta.CreatedAt > after {
		t.Errorf("CreatedAt %d not between %d and %d", meta.CreatedAt, before, after)
	}

	if meta.ModifiedAt < before || meta.ModifiedAt > after {
		t.Errorf("ModifiedAt %d not between %d and %d", meta.ModifiedAt, before, after)
	}

	// CreatedAt and ModifiedAt should be equal for new config
	if meta.CreatedAt != meta.ModifiedAt {
		t.Errorf("CreatedAt (%d) != ModifiedAt (%d) for new config",
			meta.CreatedAt, meta.ModifiedAt)
	}
}

// TestConfigMeta_TimeConversion verifies timestamp helper methods.
func TestConfigMeta_TimeConversion(t *testing.T) {
	now := time.Now()
	meta := ConfigMeta{
		CreatedAt:  now.UnixMilli(),
		ModifiedAt: now.UnixMilli(),
	}

	createdTime := meta.CreatedAtTime()
	modifiedTime := meta.ModifiedAtTime()

	// Allow 1ms tolerance due to conversion precision
	if createdTime.Sub(now).Abs() > time.Millisecond {
		t.Errorf("CreatedAtTime() differs by more than 1ms from original")
	}

	if modifiedTime.Sub(now).Abs() > time.Millisecond {
		t.Errorf("ModifiedAtTime() differs by more than 1ms from original")
	}
}

// -----------------------------------------------------------------------------
// DefaultConfig Tests
// -----------------------------------------------------------------------------

// TestDefaultConfig_HasMeta verifies metadata is included.
func TestDefaultConfig_HasMeta(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Meta.Version == "" {
		t.Error("Meta.Version should not be empty")
	}

	if cfg.Meta.CreatedAt == 0 {
		t.Error("Meta.CreatedAt should not be zero")
	}

	if cfg.Meta.ModifiedAt == 0 {
		t.Error("Meta.ModifiedAt should not be zero")
	}

	if cfg.Meta.ModifiedBy == "" {
		t.Error("Meta.ModifiedBy should not be empty")
	}
}

// TestDefaultConfig_OllamaDefaults verifies Ollama configuration.
func TestDefaultConfig_OllamaDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ModelBackend.Type != "ollama" {
		t.Errorf("ModelBackend.Type = %q, want %q", cfg.ModelBackend.Type, "ollama")
	}

	if cfg.ModelBackend.Ollama.EmbeddingModel != DefaultEmbeddingModel {
		t.Errorf("Ollama.EmbeddingModel = %q, want %q",
			cfg.ModelBackend.Ollama.EmbeddingModel, DefaultEmbeddingModel)
	}

	if cfg.ModelBackend.Ollama.DiskLimitGB != DefaultDiskLimitGB {
		t.Errorf("Ollama.DiskLimitGB = %d, want %d",
			cfg.ModelBackend.Ollama.DiskLimitGB, DefaultDiskLimitGB)
	}

	if cfg.ModelBackend.Ollama.BaseURL != DefaultOllamaHostURL {
		t.Errorf("Ollama.BaseURL = %q, want %q",
			cfg.ModelBackend.Ollama.BaseURL, DefaultOllamaHostURL)
	}
}

// TestDefaultConfig_MachineDefaults verifies machine configuration.
func TestDefaultConfig_MachineDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Machine.Id != "podman-machine-default" {
		t.Errorf("Machine.Id = %q, want %q", cfg.Machine.Id, "podman-machine-default")
	}

	if cfg.Machine.CPUCount != 6 {
		t.Errorf("Machine.CPUCount = %d, want %d", cfg.Machine.CPUCount, 6)
	}

	if cfg.Machine.MemoryAmount != 20480 {
		t.Errorf("Machine.MemoryAmount = %d, want %d", cfg.Machine.MemoryAmount, 20480)
	}
}

// TestDefaultConfig_FeatureDefaults verifies feature toggles.
func TestDefaultConfig_FeatureDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Features.Observability {
		t.Error("Features.Observability should be true by default")
	}

	if !cfg.Features.RagEngine {
		t.Error("Features.RagEngine should be true by default")
	}
}

// TestDefaultConfig_ForecastDefaults verifies forecast configuration.
func TestDefaultConfig_ForecastDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Forecast.Enabled {
		t.Error("Forecast.Enabled should be true by default")
	}

	if cfg.Forecast.Mode != ForecastModeStandalone {
		t.Errorf("Forecast.Mode = %q, want %q",
			cfg.Forecast.Mode, ForecastModeStandalone)
	}
}

// TestDefaultConfig_ProfilesEmpty verifies profiles start empty.
func TestDefaultConfig_ProfilesEmpty(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.Profiles) != 0 {
		t.Errorf("Profiles should be empty, got %d items", len(cfg.Profiles))
	}
}

// -----------------------------------------------------------------------------
// Constants Tests
// -----------------------------------------------------------------------------

// TestConstants verifies constant values are as expected.
func TestConstants(t *testing.T) {
	if DefaultEmbeddingModel != "nomic-embed-text-v2-moe" {
		t.Errorf("DefaultEmbeddingModel = %q, want %q",
			DefaultEmbeddingModel, "nomic-embed-text-v2-moe")
	}

	if DefaultLLMModel != "gpt-oss" {
		t.Errorf("DefaultLLMModel = %q, want %q", DefaultLLMModel, "gpt-oss")
	}

	if DefaultDiskLimitGB != 50 {
		t.Errorf("DefaultDiskLimitGB = %d, want %d", DefaultDiskLimitGB, 50)
	}

	if DefaultOllamaHostURL != "http://localhost:11434" {
		t.Errorf("DefaultOllamaHostURL = %q, want %q",
			DefaultOllamaHostURL, "http://localhost:11434")
	}

	if DefaultOllamaContainerURL != "http://host.containers.internal:11434" {
		t.Errorf("DefaultOllamaContainerURL = %q, want %q",
			DefaultOllamaContainerURL, "http://host.containers.internal:11434")
	}

	if CurrentConfigVersion != "1.0.0" {
		t.Errorf("CurrentConfigVersion = %q, want %q",
			CurrentConfigVersion, "1.0.0")
	}
}

// -----------------------------------------------------------------------------
// ProfileConfig Tests
// -----------------------------------------------------------------------------

// TestProfileConfig_Fields verifies profile field assignment.
func TestProfileConfig_Fields(t *testing.T) {
	profile := ProfileConfig{
		Name:          "test-profile",
		OllamaModel:   "llama3:70b",
		MaxTokens:     32768,
		RerankerModel: "cross-encoder/ms-marco-MiniLM-L-6-v2",
		MinRAM_MB:     65536,
	}

	if profile.Name != "test-profile" {
		t.Errorf("Name = %q, want %q", profile.Name, "test-profile")
	}

	if profile.OllamaModel != "llama3:70b" {
		t.Errorf("OllamaModel = %q, want %q", profile.OllamaModel, "llama3:70b")
	}

	if profile.MaxTokens != 32768 {
		t.Errorf("MaxTokens = %d, want %d", profile.MaxTokens, 32768)
	}

	if profile.MinRAM_MB != 65536 {
		t.Errorf("MinRAM_MB = %d, want %d", profile.MinRAM_MB, 65536)
	}
}
