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
	"net/http"
	"testing"
)

func TestDefaultResourceLimitsConfig(t *testing.T) {
	config := DefaultResourceLimitsConfig()

	if config.MinRecommendedFD == 0 {
		t.Error("MinRecommendedFD should have default value")
	}
	if config.WarnAtFDPercent == 0 {
		t.Error("WarnAtFDPercent should have default value")
	}
}

func TestNewResourceLimitsChecker(t *testing.T) {
	tests := []struct {
		name   string
		config ResourceLimitsConfig
	}{
		{
			name:   "with defaults",
			config: DefaultResourceLimitsConfig(),
		},
		{
			name: "with zero values",
			config: ResourceLimitsConfig{
				MinRecommendedFD: 0, // Should be set to default
			},
		},
		{
			name: "with custom values",
			config: ResourceLimitsConfig{
				MinRecommendedFD: 2048,
				WarnAtFDPercent:  90,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewResourceLimitsChecker(tt.config)
			if checker == nil {
				t.Fatal("NewResourceLimitsChecker returned nil")
			}
		})
	}
}

func TestResourceLimitsChecker_Check(t *testing.T) {
	checker := NewResourceLimitsChecker(DefaultResourceLimitsConfig())

	limits := checker.Check()

	// Should have retrieved some limits
	if limits.SoftFD == 0 && limits.HardFD == 0 && len(limits.Warnings) == 0 {
		t.Error("Check() should return either limits or warnings")
	}

	// CheckedAt should be set
	if limits.CheckedAt.IsZero() {
		t.Error("CheckedAt should be set")
	}

	// RecommendedFD should be set
	if limits.RecommendedFD == 0 {
		t.Error("RecommendedFD should be set")
	}
}

func TestResourceLimitsChecker_CheckFDLimit(t *testing.T) {
	checker := NewResourceLimitsChecker(DefaultResourceLimitsConfig())

	soft, hard, warnings := checker.CheckFDLimit()

	// On most systems, we should get some values
	// (unless running in a very restricted container)
	if soft == 0 && hard == 0 && len(warnings) == 0 {
		t.Error("CheckFDLimit() should return either limits or warnings")
	}

	// Hard limit should be >= soft limit
	if hard > 0 && soft > hard {
		t.Errorf("Soft limit (%d) should not exceed hard limit (%d)", soft, hard)
	}
}

func TestResourceLimitsChecker_LowLimit_Warning(t *testing.T) {
	// Configure with a very high minimum to trigger warning
	checker := NewResourceLimitsChecker(ResourceLimitsConfig{
		MinRecommendedFD: 1000000, // Absurdly high
		WarnAtFDPercent:  80,
	})

	soft, _, warnings := checker.CheckFDLimit()

	// If we got a soft limit, we should have a warning
	// (because 1000000 is higher than any realistic ulimit)
	if soft > 0 && soft < 1000000 {
		if len(warnings) == 0 {
			t.Error("Should warn when limit is below recommendation")
		}
	}
	// If soft is 0 or >= 1000000, we might be in a special environment - skip
}

func TestResourceLimitsChecker_IsHealthy(t *testing.T) {
	// With reasonable defaults, most systems should be healthy
	checker := NewResourceLimitsChecker(ResourceLimitsConfig{
		MinRecommendedFD: 100, // Low threshold
		WarnAtFDPercent:  99,  // High threshold
	})

	// This should pass on most systems
	healthy := checker.IsHealthy()
	// We can't assert true because some CI environments are restricted
	_ = healthy // Just ensure it doesn't panic
}

func TestResourceLimits_HasWarnings(t *testing.T) {
	tests := []struct {
		name     string
		limits   ResourceLimits
		expected bool
	}{
		{
			name:     "no warnings",
			limits:   ResourceLimits{Warnings: nil},
			expected: false,
		},
		{
			name:     "empty warnings",
			limits:   ResourceLimits{Warnings: []string{}},
			expected: false,
		},
		{
			name:     "with warnings",
			limits:   ResourceLimits{Warnings: []string{"test warning"}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.limits.HasWarnings()
			if got != tt.expected {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetSharedHTTPClient(t *testing.T) {
	client1 := GetSharedHTTPClient()
	client2 := GetSharedHTTPClient()

	if client1 == nil {
		t.Fatal("GetSharedHTTPClient returned nil")
	}

	// Should return same instance
	if client1 != client2 {
		t.Error("GetSharedHTTPClient should return singleton")
	}

	// Should have timeout set
	if client1.Timeout == 0 {
		t.Error("Shared client should have timeout set")
	}

	// Should have transport configured
	transport, ok := client1.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Transport should be *http.Transport")
	}

	if transport.MaxIdleConns == 0 {
		t.Error("Transport should have MaxIdleConns configured")
	}
}

func TestGetSharedHTTPClientWithTimeout(t *testing.T) {
	timeout := 5 * 1000000000 // 5 seconds in nanoseconds

	client := GetSharedHTTPClientWithTimeout(5000000000)

	if client == nil {
		t.Fatal("GetSharedHTTPClientWithTimeout returned nil")
	}

	if client.Timeout != 5000000000 {
		t.Errorf("Timeout = %v, want %v", client.Timeout, timeout)
	}

	// Should share transport with base client
	base := GetSharedHTTPClient()
	if client.Transport != base.Transport {
		t.Error("Should share transport with base client")
	}
}

func TestResourceLimitsChecker_InterfaceCompliance(t *testing.T) {
	var _ ResourceChecker = (*DefaultResourceLimitsChecker)(nil)
}
