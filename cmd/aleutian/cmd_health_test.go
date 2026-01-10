// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// BOX DRAWING TESTS
// =============================================================================

func TestVisibleLength(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "plain text",
			input:    "Hello World",
			expected: 11,
		},
		{
			name:     "text with green color",
			input:    "\033[32mHello\033[0m",
			expected: 5,
		},
		{
			name:     "text with multiple colors",
			input:    "\033[31mRed\033[0m \033[32mGreen\033[0m",
			expected: 9, // "Red Green" = 3 + 1 + 5 = 9
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "only escape codes",
			input:    "\033[0m\033[31m",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := visibleLength(tt.input)
			if result != tt.expected {
				t.Errorf("visibleLength(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		expected []string
	}{
		{
			name:     "short text",
			text:     "Hello",
			width:    20,
			expected: []string{"Hello"},
		},
		{
			name:     "exact fit",
			text:     "Hello World",
			width:    11,
			expected: []string{"Hello World"},
		},
		{
			name:     "needs wrapping",
			text:     "Hello World Test",
			width:    10,
			expected: []string{"Hello", "World Test"},
		},
		{
			name:     "empty string",
			text:     "",
			width:    20,
			expected: nil,
		},
		{
			name:     "multiple wraps",
			text:     "One Two Three Four Five",
			width:    10,
			expected: []string{"One Two", "Three Four", "Five"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapText(tt.text, tt.width)
			if len(result) != len(tt.expected) {
				t.Errorf("wrapText(%q, %d) returned %d lines, want %d",
					tt.text, tt.width, len(result), len(tt.expected))
				return
			}
			for i, line := range result {
				if line != tt.expected[i] {
					t.Errorf("wrapText(%q, %d)[%d] = %q, want %q",
						tt.text, tt.width, i, line, tt.expected[i])
				}
			}
		})
	}
}

// =============================================================================
// STATE/ALERT FORMATTING TESTS
// =============================================================================

func TestGetStateIcon(t *testing.T) {
	tests := []struct {
		state    IntelligentHealthState
		expected string
	}{
		{IntelligentStateHealthy, "✓"},
		{IntelligentStateDegraded, "◐"},
		{IntelligentStateAtRisk, "⚠"},
		{IntelligentStateCritical, "✗"},
		{IntelligentStateUnknown, "?"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			result := getStateIcon(tt.state)
			if result != tt.expected {
				t.Errorf("getStateIcon(%q) = %q, want %q", tt.state, result, tt.expected)
			}
		})
	}
}

func TestGetStateColor(t *testing.T) {
	tests := []struct {
		state    IntelligentHealthState
		expected string
	}{
		{IntelligentStateHealthy, colorGreen},
		{IntelligentStateDegraded, colorYellow},
		{IntelligentStateAtRisk, colorYellow},
		{IntelligentStateCritical, colorRed},
		{IntelligentStateUnknown, colorCyan},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			result := getStateColor(tt.state)
			if result != tt.expected {
				t.Errorf("getStateColor(%q) = %q, want %q", tt.state, result, tt.expected)
			}
		})
	}
}

func TestGetAlertIcon(t *testing.T) {
	tests := []struct {
		severity AlertSeverity
		expected string
	}{
		{AlertSeverityInfo, "ℹ"},
		{AlertSeverityWarning, "⚠"},
		{AlertSeverityError, "✗"},
		{AlertSeverityCritical, "🔥"},
		{AlertSeverity("unknown"), "•"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			result := getAlertIcon(tt.severity)
			if result != tt.expected {
				t.Errorf("getAlertIcon(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestGetAlertColor(t *testing.T) {
	tests := []struct {
		severity AlertSeverity
		expected string
	}{
		{AlertSeverityInfo, colorBlue},
		{AlertSeverityWarning, colorYellow},
		{AlertSeverityError, colorRed},
		{AlertSeverityCritical, colorRed},
		{AlertSeverity("unknown"), colorReset},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			result := getAlertColor(tt.severity)
			if result != tt.expected {
				t.Errorf("getAlertColor(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// HELPER FUNCTION TESTS
// =============================================================================

func TestCreateMetricsStore(t *testing.T) {
	store := createMetricsStore("/tmp/test")
	if store == nil {
		t.Fatal("createMetricsStore returned nil")
	}

	// Test basic functionality
	now := time.Now()
	store.Record("test-service", "latency", 100.0, now)

	points := store.Query("test-service", "latency", now.Add(-1*time.Minute), now.Add(1*time.Minute))
	if len(points) != 1 {
		t.Errorf("Expected 1 point, got %d", len(points))
	}
}

func TestGetAleutianStackDir(t *testing.T) {
	dir := getAleutianStackDir()
	if dir == "" {
		t.Error("getAleutianStackDir returned empty string")
	}
}

// =============================================================================
// OUTPUT FORMATTING TESTS
// =============================================================================

func TestOutputHealthJSON(t *testing.T) {
	report := &IntelligentHealthReport{
		ID:           "test-report",
		Timestamp:    time.Now(),
		OverallState: IntelligentStateHealthy,
		Services: []ServiceInsights{
			{
				ID:               "svc-1",
				Name:             "Test Service",
				IntelligentState: IntelligentStateHealthy,
			},
		},
		CreatedAt: time.Now(),
	}

	// Capture output by replacing stdout temporarily
	// Note: This is a basic test; production would use dependency injection
	// For now we just verify it doesn't panic
	outputHealthJSON(report)
}

func TestOutputHealthReport(t *testing.T) {
	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		Timestamp:    time.Now(),
		OverallState: IntelligentStateDegraded,
		Summary:      "Test summary message for health analysis.",
		Services: []ServiceInsights{
			{
				ID:               GenerateID(),
				Name:             "Orchestrator",
				IntelligentState: IntelligentStateHealthy,
				BasicHealth:      HealthStatus{State: HealthStateHealthy},
				LatencyP99:       150 * time.Millisecond,
				LatencyTrend:     TrendStable,
			},
			{
				ID:               GenerateID(),
				Name:             "Weaviate",
				IntelligentState: IntelligentStateDegraded,
				BasicHealth:      HealthStatus{State: HealthStateHealthy},
				LatencyP99:       800 * time.Millisecond,
				LatencyTrend:     TrendIncreasing,
				RecentErrors:     3,
				Insights:         []string{"Latency increasing", "Connection retries detected"},
			},
		},
		Alerts: []HealthAlert{
			{
				ID:       GenerateID(),
				Severity: AlertSeverityWarning,
				Service:  "Weaviate",
				Title:    "Elevated latency",
			},
		},
		FreshnessReports: []FreshnessReport{
			{
				ID:          GenerateID(),
				ServiceName: "Orchestrator",
				IsStale:     false,
			},
		},
		Recommendations: []string{
			"Check Weaviate connection pool settings",
		},
		Duration:  100 * time.Millisecond,
		CreatedAt: time.Now(),
	}

	// This will output to stdout; just verify no panic
	outputHealthReport(report)
}

// =============================================================================
// INTEGRATION TESTS
// =============================================================================

func TestHealthCommandIntegration_MockedDependencies(t *testing.T) {
	// Create mock dependencies
	mockProc := &MockProcessManager{
		RunInDirFunc: func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
			// Mock container inspection
			if name == "podman" && len(args) > 0 && args[0] == "inspect" {
				return "true", "", 0, nil
			}
			// Mock git log
			if name == "git" && len(args) > 0 && args[0] == "log" {
				return "1704067200", "", 0, nil
			}
			return "", "", 0, nil
		},
	}

	mockChecker := &MockHealthChecker{
		CheckAllServicesFunc: func(ctx context.Context, services []ServiceDefinition) ([]HealthStatus, error) {
			statuses := make([]HealthStatus, len(services))
			for i, svc := range services {
				statuses[i] = HealthStatus{
					ID:          GenerateID(),
					Name:        svc.Name,
					State:       HealthStateHealthy,
					LastChecked: time.Now(),
				}
			}
			return statuses, nil
		},
		CheckServiceFunc: func(ctx context.Context, service ServiceDefinition) (*HealthStatus, error) {
			return &HealthStatus{
				ID:          GenerateID(),
				Name:        service.Name,
				State:       HealthStateHealthy,
				LastChecked: time.Now(),
			}, nil
		},
	}

	mockTextGen := &MockHealthTextGenerator{
		GenerateFunc: func(ctx context.Context, model, prompt string) (string, error) {
			return "All services are healthy. No issues detected.", nil
		},
	}

	config := DefaultIntelligenceConfig("/tmp/test")
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())
	metricsStore, _ := NewEphemeralMetricsStore(MetricsStoreConfig{InMemoryOnly: true})

	intel := NewDefaultHealthIntelligence(mockChecker, mockProc, mockTextGen, metricsStore, sanitizer, config)

	ctx := context.Background()
	opts := AnalysisOptions{
		ID:                GenerateID(),
		TimeWindow:        5 * time.Minute,
		Services:          DefaultServiceDefinitions(),
		IncludeLLMSummary: true,
		MaxLogLines:       100,
		CreatedAt:         time.Now(),
	}

	report, err := intel.AnalyzeHealth(ctx, opts)
	if err != nil {
		t.Fatalf("AnalyzeHealth failed: %v", err)
	}

	if report.ID == "" {
		t.Error("Report ID should not be empty")
	}
	if report.Summary == "" {
		t.Error("Summary should not be empty when LLM is enabled")
	}
	if len(report.Services) == 0 {
		t.Error("Should have service insights")
	}
}

// =============================================================================
// BOX DRAWING OUTPUT TESTS
// =============================================================================

func TestPrintBoxFunctions(t *testing.T) {
	// These functions output to stdout
	// We verify they don't panic and produce expected patterns

	// Capture would require more complex setup, so just verify no panic
	t.Run("printBoxTop", func(t *testing.T) {
		printBoxTop(40)
	})

	t.Run("printBoxBottom", func(t *testing.T) {
		printBoxBottom(40)
	})

	t.Run("printBoxSeparator", func(t *testing.T) {
		printBoxSeparator(40)
	})

	t.Run("printBoxLine", func(t *testing.T) {
		printBoxLine("Test content", 40)
	})

	t.Run("printBoxCenter", func(t *testing.T) {
		printBoxCenter("Centered", 40)
	})
}

// =============================================================================
// COMMAND FLAG TESTS
// =============================================================================

func TestHealthCommandFlags(t *testing.T) {
	// Verify flags are registered
	flags := []string{"ai", "json", "window", "verbose"}

	for _, flagName := range flags {
		flag := healthCmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("Flag %q not registered", flagName)
		}
	}
}

func TestHealthCommandShortFlags(t *testing.T) {
	// Verify short flags
	shortFlags := map[string]string{
		"w": "window",
		"v": "verbose",
	}

	for short, long := range shortFlags {
		flag := healthCmd.Flags().ShorthandLookup(short)
		if flag == nil {
			t.Errorf("Short flag -%s not registered", short)
			continue
		}
		if flag.Name != long {
			t.Errorf("Short flag -%s maps to %q, want %q", short, flag.Name, long)
		}
	}
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestOutputHealthReport_EmptyReport(t *testing.T) {
	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		Timestamp:    time.Now(),
		OverallState: IntelligentStateUnknown,
		CreatedAt:    time.Now(),
	}

	// Should not panic with empty data
	outputHealthReport(report)
}

func TestOutputHealthReport_LongSummary(t *testing.T) {
	report := &IntelligentHealthReport{
		ID:           GenerateID(),
		Timestamp:    time.Now(),
		OverallState: IntelligentStateHealthy,
		Summary:      strings.Repeat("This is a very long summary that should be wrapped properly. ", 10),
		CreatedAt:    time.Now(),
	}

	// Should handle long summary without panic
	outputHealthReport(report)
}

func TestHealthCommand_InterfaceCompliance(t *testing.T) {
	// Verify the health command is properly configured
	if healthCmd.Use != "health" {
		t.Errorf("healthCmd.Use = %q, want %q", healthCmd.Use, "health")
	}
	if healthCmd.Run == nil {
		t.Error("healthCmd.Run is nil")
	}
}

// =============================================================================
// MOCK HELPERS
// =============================================================================

// captureOutput captures stdout during function execution.
// Note: This is a simplified version; production would use proper DI.
func captureOutput(f func()) string {
	var buf bytes.Buffer
	// This is a placeholder - proper implementation would redirect os.Stdout
	f()
	return buf.String()
}
