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
	"testing"
)

func TestDefaultMetricsSchemaConfig(t *testing.T) {
	config := DefaultMetricsSchemaConfig()

	if config.MaxLabelValues <= 0 {
		t.Error("MaxLabelValues should be positive")
	}
}

func TestNewMetricsSchema(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	if schema == nil {
		t.Fatal("NewMetricsSchema returned nil")
	}

	// Should have default metrics registered
	err := schema.ValidateMetric("aleutian_health_check_duration")
	if err != nil {
		t.Errorf("Default metric should be registered: %v", err)
	}
}

func TestMetricsSchema_ValidateMetric_Known(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: true,
	})

	err := schema.ValidateMetric("aleutian_health_check_duration")
	if err != nil {
		t.Errorf("Known metric should be valid: %v", err)
	}
}

func TestMetricsSchema_ValidateMetric_Unknown_StrictMode(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: true,
	})

	err := schema.ValidateMetric("unknown_metric")
	if err == nil {
		t.Error("Unknown metric should be invalid in strict mode")
	}
}

func TestMetricsSchema_ValidateMetric_Unknown_NonStrict(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: false,
	})

	err := schema.ValidateMetric("unknown_metric")
	if err != nil {
		t.Errorf("Unknown metric should be allowed in non-strict mode: %v", err)
	}
}

func TestMetricsSchema_ValidateLabels_Valid(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	err := schema.ValidateLabels("aleutian_health_check_duration", map[string]string{
		"service":    "weaviate",
		"check_type": "http",
		"status":     "healthy",
	})
	if err != nil {
		t.Errorf("Valid labels should pass: %v", err)
	}
}

func TestMetricsSchema_ValidateLabels_InvalidLabel(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: true,
	})

	err := schema.ValidateLabels("aleutian_health_check_duration", map[string]string{
		"service":       "weaviate",
		"invalid_label": "value",
	})
	if err == nil {
		t.Error("Invalid label should be rejected")
	}
}

func TestMetricsSchema_ValidateLabels_InvalidValue(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: true,
	})

	err := schema.ValidateLabels("aleutian_error_count", map[string]string{
		"service":    "weaviate",
		"error_type": "some_random_error_message", // Not in enum
	})
	if err == nil {
		t.Error("Invalid label value should be rejected")
	}
}

func TestMetricsSchema_NormalizeLabel_Valid(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Known value should be returned as-is
	normalized := schema.NormalizeLabel("error_type", "timeout")
	if normalized != "timeout" {
		t.Errorf("NormalizeLabel = %q, want %q", normalized, "timeout")
	}
}

func TestMetricsSchema_NormalizeLabel_Unknown(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Unknown value should be normalized to "unknown"
	normalized := schema.NormalizeLabel("error_type", "some_random_error")
	if normalized != "unknown" {
		t.Errorf("NormalizeLabel = %q, want %q", normalized, "unknown")
	}
}

func TestMetricsSchema_NormalizeLabel_NoEnum(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Label without enum should return value as-is
	normalized := schema.NormalizeLabel("no_enum_label", "any_value")
	if normalized != "any_value" {
		t.Errorf("NormalizeLabel = %q, want %q", normalized, "any_value")
	}
}

func TestMetricsSchema_RegisterMetric(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		StrictMode: true,
	})

	// Register new metric
	err := schema.RegisterMetric("custom_metric", []string{"label1", "label2"})
	if err != nil {
		t.Errorf("RegisterMetric failed: %v", err)
	}

	// Should be valid now
	err = schema.ValidateMetric("custom_metric")
	if err != nil {
		t.Errorf("Registered metric should be valid: %v", err)
	}

	// Labels should be validated
	err = schema.ValidateLabels("custom_metric", map[string]string{
		"label1": "value1",
	})
	if err != nil {
		t.Errorf("Valid label should pass: %v", err)
	}

	err = schema.ValidateLabels("custom_metric", map[string]string{
		"invalid": "value",
	})
	if err == nil {
		t.Error("Invalid label should be rejected")
	}
}

func TestMetricsSchema_RegisterMetric_Duplicate(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Try to register an already existing metric
	err := schema.RegisterMetric("aleutian_health_check_duration", []string{"foo"})
	if err == nil {
		t.Error("Should not allow duplicate metric registration")
	}
}

func TestMetricsSchema_RegisterLabelEnum(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Register new enum
	err := schema.RegisterLabelEnum("custom_label", []string{"a", "b", "c"})
	if err != nil {
		t.Errorf("RegisterLabelEnum failed: %v", err)
	}

	// Normalize should work
	normalized := schema.NormalizeLabel("custom_label", "a")
	if normalized != "a" {
		t.Errorf("NormalizeLabel = %q, want %q", normalized, "a")
	}

	normalized = schema.NormalizeLabel("custom_label", "invalid")
	if normalized != "unknown" {
		t.Errorf("NormalizeLabel = %q, want %q", normalized, "unknown")
	}
}

func TestMetricsSchema_GetLabelCardinality(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	// Initially 0
	card := schema.GetLabelCardinality("service")
	if card != 0 {
		t.Errorf("Initial cardinality = %d, want 0", card)
	}

	// Validate some labels to track them
	schema.ValidateLabels("aleutian_health_check_duration", map[string]string{
		"service": "weaviate",
		"status":  "healthy",
	})
	schema.ValidateLabels("aleutian_health_check_duration", map[string]string{
		"service": "ollama",
		"status":  "healthy",
	})

	// Should have tracked 2 unique services
	card = schema.GetLabelCardinality("service")
	if card != 2 {
		t.Errorf("Cardinality = %d, want 2", card)
	}
}

func TestMetricsSchema_GetHighCardinalityLabels(t *testing.T) {
	schema := NewMetricsSchema(MetricsSchemaConfig{
		MaxLabelValues: 2, // Very low threshold for testing
	})

	// Register a label without enum (so values aren't validated)
	schema.RegisterMetric("test_metric", []string{"dynamic_label"})

	// Add values to exceed threshold
	for i := 0; i < 5; i++ {
		schema.ValidateLabels("test_metric", map[string]string{
			"dynamic_label": string(rune('a' + i)),
		})
	}

	highCard := schema.GetHighCardinalityLabels()
	if _, exists := highCard["dynamic_label"]; !exists {
		t.Error("dynamic_label should be in high cardinality labels")
	}
}

func TestMetricsSchema_InterfaceCompliance(t *testing.T) {
	var _ MetricsValidator = (*DefaultMetricsSchema)(nil)
}

func TestMetricsSchema_ConcurrentAccess(t *testing.T) {
	schema := NewMetricsSchema(DefaultMetricsSchemaConfig())

	done := make(chan bool, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		go func() {
			schema.ValidateMetric("aleutian_health_check_duration")
			schema.NormalizeLabel("status", "healthy")
			schema.GetLabelCardinality("service")
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 50; i++ {
		go func(n int) {
			schema.ValidateLabels("aleutian_health_check_duration", map[string]string{
				"service": "weaviate",
				"status":  "healthy",
			})
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
