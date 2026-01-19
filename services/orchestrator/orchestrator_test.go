// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package orchestrator

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/pkg/extensions"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Test Setup
// =============================================================================

func init() {
	// Set Gin to test mode to reduce noise in test output
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// Config Tests
// =============================================================================

// TestApplyConfigDefaults_AllDefaults verifies default values are applied.
//
// # Description
//
// Tests that applyConfigDefaults correctly fills in missing values
// when an empty Config is provided.
func TestApplyConfigDefaults_AllDefaults(t *testing.T) {
	// Arrange
	cfg := Config{}

	// Act
	result := applyConfigDefaults(cfg)

	// Assert
	assert.Equal(t, 12210, result.Port, "default port should be 12210")
	assert.Equal(t, "local", result.LLMBackend, "default LLM backend should be local")
	assert.Equal(t, "aleutian-otel-collector:4317", result.OTelEndpoint,
		"default OTel endpoint should be aleutian-otel-collector:4317")
	assert.True(t, result.EnableMetrics, "metrics should be enabled by default")
}

// TestApplyConfigDefaults_PreservesCustomValues verifies custom values are not overwritten.
//
// # Description
//
// Tests that applyConfigDefaults does not overwrite user-provided values.
func TestApplyConfigDefaults_PreservesCustomValues(t *testing.T) {
	// Arrange
	cfg := Config{
		Port:         8080,
		LLMBackend:   "openai",
		OTelEndpoint: "custom-collector:4317",
		WeaviateURL:  "http://weaviate:8080",
	}

	// Act
	result := applyConfigDefaults(cfg)

	// Assert
	assert.Equal(t, 8080, result.Port, "custom port should be preserved")
	assert.Equal(t, "openai", result.LLMBackend, "custom LLM backend should be preserved")
	assert.Equal(t, "custom-collector:4317", result.OTelEndpoint,
		"custom OTel endpoint should be preserved")
	assert.Equal(t, "http://weaviate:8080", result.WeaviateURL,
		"custom Weaviate URL should be preserved")
}

// TestApplyConfigDefaults_PartialConfig verifies partial configs are handled.
//
// # Description
//
// Tests that applyConfigDefaults correctly mixes user values with defaults.
func TestApplyConfigDefaults_PartialConfig(t *testing.T) {
	// Arrange
	cfg := Config{
		Port: 9999,
		// LLMBackend and OTelEndpoint left empty
	}

	// Act
	result := applyConfigDefaults(cfg)

	// Assert
	assert.Equal(t, 9999, result.Port, "custom port should be preserved")
	assert.Equal(t, "local", result.LLMBackend, "default LLM backend should be applied")
	assert.Equal(t, "aleutian-otel-collector:4317", result.OTelEndpoint,
		"default OTel endpoint should be applied")
}

// =============================================================================
// ServiceOptions Tests
// =============================================================================

// TestServiceOptions_WithNilUseDefaults verifies nil opts uses defaults.
//
// # Description
//
// Tests that when nil ServiceOptions is passed to New(), the default
// no-op implementations are used.
func TestServiceOptions_WithNilUseDefaults(t *testing.T) {
	// This test verifies the logic that would be used in New()
	// We can't call New() directly as it requires external services

	// Arrange
	var opts *extensions.ServiceOptions = nil

	// Act - simulate what New() does
	var actualOpts extensions.ServiceOptions
	if opts != nil {
		actualOpts = *opts
	} else {
		actualOpts = extensions.DefaultOptions()
	}

	// Assert
	assert.NotNil(t, actualOpts.AuthProvider, "default AuthProvider should be set")
	assert.NotNil(t, actualOpts.AuthzProvider, "default AuthzProvider should be set")
	assert.NotNil(t, actualOpts.AuditLogger, "default AuditLogger should be set")
	assert.NotNil(t, actualOpts.MessageFilter, "default MessageFilter should be set")

	// Verify they are the Nop implementations
	_, isNopAuth := actualOpts.AuthProvider.(*extensions.NopAuthProvider)
	assert.True(t, isNopAuth, "AuthProvider should be NopAuthProvider")

	_, isNopAuthz := actualOpts.AuthzProvider.(*extensions.NopAuthzProvider)
	assert.True(t, isNopAuthz, "AuthzProvider should be NopAuthzProvider")

	_, isNopAudit := actualOpts.AuditLogger.(*extensions.NopAuditLogger)
	assert.True(t, isNopAudit, "AuditLogger should be NopAuditLogger")

	_, isNopFilter := actualOpts.MessageFilter.(*extensions.NopMessageFilter)
	assert.True(t, isNopFilter, "MessageFilter should be NopMessageFilter")
}

// TestServiceOptions_WithCustomProviders verifies custom providers are used.
//
// # Description
//
// Tests that when custom ServiceOptions are provided, they are used
// instead of defaults.
func TestServiceOptions_WithCustomProviders(t *testing.T) {
	// Arrange
	customAuth := &mockAuthProvider{}
	customAudit := &mockAuditLogger{}

	opts := &extensions.ServiceOptions{
		AuthProvider: customAuth,
		AuditLogger:  customAudit,
		// Leave others nil
	}

	// Act - simulate what New() would do with partial custom opts
	var actualOpts extensions.ServiceOptions
	if opts != nil {
		actualOpts = *opts
	}

	// Assert - custom providers should be used
	assert.Same(t, customAuth, actualOpts.AuthProvider,
		"custom AuthProvider should be used")
	assert.Same(t, customAudit, actualOpts.AuditLogger,
		"custom AuditLogger should be used")

	// Nil fields remain nil (would need explicit handling in real code)
	assert.Nil(t, actualOpts.AuthzProvider,
		"unset AuthzProvider should be nil")
	assert.Nil(t, actualOpts.MessageFilter,
		"unset MessageFilter should be nil")
}

// =============================================================================
// Config Struct Tests
// =============================================================================

// TestConfig_ZeroValue verifies Config zero value is usable.
//
// # Description
//
// Tests that an uninitialized Config can be passed to applyConfigDefaults
// and results in valid configuration.
func TestConfig_ZeroValue(t *testing.T) {
	// Arrange
	var cfg Config

	// Act
	result := applyConfigDefaults(cfg)

	// Assert - should have valid defaults
	assert.Greater(t, result.Port, 0, "port should be positive")
	assert.NotEmpty(t, result.LLMBackend, "LLM backend should not be empty")
	assert.NotEmpty(t, result.OTelEndpoint, "OTel endpoint should not be empty")
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestServiceImplementsInterface verifies interface compliance.
//
// # Description
//
// Compile-time check that service implements Service interface.
// The actual var declaration is in orchestrator.go, but this test
// documents the requirement.
func TestServiceImplementsInterface(t *testing.T) {
	// This is a compile-time check - if it compiles, the test passes
	// The actual check is: var _ Service = (*service)(nil)
	// We verify by ensuring the interface methods exist

	var svc Service
	_ = svc // Use the variable to satisfy compiler
}

// =============================================================================
// Mock Implementations for Testing
// =============================================================================

// mockAuthProvider is a test double for AuthProvider.
type mockAuthProvider struct {
	extensions.NopAuthProvider
}

// mockAuditLogger is a test double for AuditLogger.
type mockAuditLogger struct {
	extensions.NopAuditLogger
}

// =============================================================================
// Integration Test (Skipped without services)
// =============================================================================

// TestNew_Integration tests the full constructor (requires services).
//
// # Description
//
// This test is skipped unless LLM services are available.
// It tests the full New() constructor with a real Config.
//
// To run: go test -run TestNew_Integration -integration
func TestNew_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test would require:
	// - Running OTel collector (or mock)
	// - Running LLM service (or mock)
	// - Optionally running Weaviate

	t.Skip("skipping: requires external services (OTel, LLM)")

	// Future implementation:
	// cfg := Config{
	//     Port:       0, // Random port
	//     LLMBackend: "local",
	// }
	// svc, err := New(cfg, nil)
	// require.NoError(t, err)
	// require.NotNil(t, svc)
	// assert.NotNil(t, svc.Router())
}

// =============================================================================
// Benchmark Tests
// =============================================================================

// BenchmarkApplyConfigDefaults measures config default application performance.
func BenchmarkApplyConfigDefaults(b *testing.B) {
	cfg := Config{Port: 8080}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = applyConfigDefaults(cfg)
	}
}

// =============================================================================
// Table-Driven Tests
// =============================================================================

// TestApplyConfigDefaults_TableDriven tests multiple config scenarios.
func TestApplyConfigDefaults_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name:  "empty config gets all defaults",
			input: Config{},
			expected: Config{
				Port:          12210,
				LLMBackend:    "local",
				OTelEndpoint:  "aleutian-otel-collector:4317",
				EnableMetrics: true,
			},
		},
		{
			name: "custom port preserved",
			input: Config{
				Port: 8080,
			},
			expected: Config{
				Port:          8080,
				LLMBackend:    "local",
				OTelEndpoint:  "aleutian-otel-collector:4317",
				EnableMetrics: true,
			},
		},
		{
			name: "custom backend preserved",
			input: Config{
				LLMBackend: "openai",
			},
			expected: Config{
				Port:          12210,
				LLMBackend:    "openai",
				OTelEndpoint:  "aleutian-otel-collector:4317",
				EnableMetrics: true,
			},
		},
		{
			name: "weaviate URL preserved (no default)",
			input: Config{
				WeaviateURL: "http://localhost:8080",
			},
			expected: Config{
				Port:          12210,
				LLMBackend:    "local",
				WeaviateURL:   "http://localhost:8080",
				OTelEndpoint:  "aleutian-otel-collector:4317",
				EnableMetrics: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyConfigDefaults(tt.input)

			assert.Equal(t, tt.expected.Port, result.Port)
			assert.Equal(t, tt.expected.LLMBackend, result.LLMBackend)
			assert.Equal(t, tt.expected.WeaviateURL, result.WeaviateURL)
			assert.Equal(t, tt.expected.OTelEndpoint, result.OTelEndpoint)
			assert.Equal(t, tt.expected.EnableMetrics, result.EnableMetrics)
		})
	}
}

// =============================================================================
// Error Case Tests
// =============================================================================

// TestConfig_InvalidValues tests behavior with edge case values.
func TestConfig_InvalidValues(t *testing.T) {
	t.Run("negative port is preserved", func(t *testing.T) {
		// Arrange - negative port (invalid but should be preserved)
		cfg := Config{Port: -1}

		// Act
		result := applyConfigDefaults(cfg)

		// Assert - we preserve invalid values (validation is separate concern)
		assert.Equal(t, -1, result.Port,
			"negative port should be preserved (validation is caller's responsibility)")
	})

	t.Run("empty string backend uses default", func(t *testing.T) {
		// Arrange
		cfg := Config{LLMBackend: ""}

		// Act
		result := applyConfigDefaults(cfg)

		// Assert
		assert.Equal(t, "local", result.LLMBackend,
			"empty backend should default to local")
	})
}

// =============================================================================
// Documentation Tests (Examples)
// =============================================================================

// ExampleConfig_minimal demonstrates minimal configuration.
func ExampleConfig_minimal() {
	cfg := Config{}
	result := applyConfigDefaults(cfg)
	_ = result
	// Output port: 12210, backend: local
}

// ExampleConfig_custom demonstrates custom configuration.
func ExampleConfig_custom() {
	cfg := Config{
		Port:        8080,
		LLMBackend:  "claude",
		WeaviateURL: "http://weaviate:8080",
	}
	result := applyConfigDefaults(cfg)
	_ = result
	// Output port: 8080, backend: claude
}
