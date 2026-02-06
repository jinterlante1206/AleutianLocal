// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package benchmark

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func createTestResult() *Result {
	return &Result{
		Name:          "test_component",
		Iterations:    100,
		TotalDuration: 1 * time.Second,
		Latency: LatencyStats{
			Min:    5 * time.Millisecond,
			Max:    50 * time.Millisecond,
			Mean:   10 * time.Millisecond,
			Median: 9 * time.Millisecond,
			StdDev: 3 * time.Millisecond,
			P50:    9 * time.Millisecond,
			P90:    20 * time.Millisecond,
			P95:    30 * time.Millisecond,
			P99:    45 * time.Millisecond,
			P999:   48 * time.Millisecond,
		},
		Throughput: ThroughputStats{
			OpsPerSecond: 100.0,
		},
		Memory: &MemoryStats{
			HeapAllocBefore: 1024 * 1024,
			HeapAllocAfter:  1024 * 1024 * 2,
			HeapAllocDelta:  1024 * 1024,
			GCPauses:        2,
			GCPauseTotal:    5 * time.Millisecond,
		},
		Errors:    5,
		ErrorRate: 0.05,
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func createTestComparison() *ComparisonResult {
	return &ComparisonResult{
		Results: map[string]*Result{
			"fast": {
				Name: "fast",
				Latency: LatencyStats{
					Mean: 5 * time.Millisecond,
					P99:  10 * time.Millisecond,
				},
				Samples: []time.Duration{5 * time.Millisecond, 6 * time.Millisecond},
			},
			"slow": {
				Name: "slow",
				Latency: LatencyStats{
					Mean: 50 * time.Millisecond,
					P99:  100 * time.Millisecond,
				},
				Samples: []time.Duration{50 * time.Millisecond, 51 * time.Millisecond},
			},
		},
		Winner:             "fast",
		Speedup:            10.0,
		Significant:        true,
		PValue:             0.001,
		ConfidenceLevel:    0.95,
		EffectSize:         2.5,
		EffectSizeCategory: EffectLarge,
		Ranking:            []string{"fast", "slow"},
	}
}

func TestNewConsoleReporter(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewConsoleReporter(&buf, true)

	if reporter == nil {
		t.Fatal("NewConsoleReporter returned nil")
	}
	if reporter.out != &buf {
		t.Error("Reporter output not set correctly")
	}
	if reporter.verbose != true {
		t.Error("Reporter verbose not set correctly")
	}
}

func TestConsoleReporter_Report(t *testing.T) {
	t.Run("basic report", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewConsoleReporter(&buf, false)
		result := createTestResult()

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		output := buf.String()

		// Check key elements are present
		if !strings.Contains(output, "test_component") {
			t.Error("Output should contain component name")
		}
		if !strings.Contains(output, "Iterations: 100") {
			t.Error("Output should contain iterations")
		}
		if !strings.Contains(output, "Latency:") {
			t.Error("Output should contain latency section")
		}
		if !strings.Contains(output, "Percentiles:") {
			t.Error("Output should contain percentiles section")
		}
		if !strings.Contains(output, "Ops/sec:") {
			t.Error("Output should contain throughput")
		}
		// Memory should NOT be present without verbose
		if strings.Contains(output, "Heap Before:") {
			t.Error("Output should not contain memory stats without verbose")
		}
	})

	t.Run("verbose report with memory", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewConsoleReporter(&buf, true)
		result := createTestResult()

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		output := buf.String()

		// Memory should be present with verbose
		if !strings.Contains(output, "Memory:") {
			t.Error("Verbose output should contain memory section")
		}
		if !strings.Contains(output, "Heap Before:") {
			t.Error("Verbose output should contain heap before")
		}
		if !strings.Contains(output, "GC Pauses:") {
			t.Error("Verbose output should contain GC pauses")
		}
	})

	t.Run("report without memory stats", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewConsoleReporter(&buf, true)
		result := createTestResult()
		result.Memory = nil

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "Heap Before:") {
			t.Error("Output should not contain memory stats when nil")
		}
	})
}

func TestConsoleReporter_ReportComparison(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewConsoleReporter(&buf, false)
	comparison := createTestComparison()

	err := reporter.ReportComparison(comparison)
	if err != nil {
		t.Fatalf("ReportComparison failed: %v", err)
	}

	output := buf.String()

	// Check key elements
	if !strings.Contains(output, "Benchmark Comparison") {
		t.Error("Output should contain header")
	}
	if !strings.Contains(output, "fast") {
		t.Error("Output should contain 'fast' component")
	}
	if !strings.Contains(output, "slow") {
		t.Error("Output should contain 'slow' component")
	}
	if !strings.Contains(output, "Speedup:") {
		t.Error("Output should contain speedup")
	}
	if !strings.Contains(output, "P-Value:") {
		t.Error("Output should contain p-value")
	}
	if !strings.Contains(output, "Effect Size:") {
		t.Error("Output should contain effect size")
	}
	if !strings.Contains(output, "Winner: fast") {
		t.Error("Output should contain winner")
	}
}

func TestConsoleReporter_ReportComparison_NoWinner(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewConsoleReporter(&buf, false)
	comparison := createTestComparison()
	comparison.Winner = ""
	comparison.Significant = false

	err := reporter.ReportComparison(comparison)
	if err != nil {
		t.Fatalf("ReportComparison failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No statistically significant winner") {
		t.Error("Output should indicate no significant winner")
	}
}

func TestConsoleReporter_ReportAll(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewConsoleReporter(&buf, false)

	results := []*Result{
		{Name: "component_1", Iterations: 100},
		{Name: "component_2", Iterations: 200},
	}

	err := reporter.ReportAll(results)
	if err != nil {
		t.Fatalf("ReportAll failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "component_1") {
		t.Error("Output should contain first component")
	}
	if !strings.Contains(output, "component_2") {
		t.Error("Output should contain second component")
	}
}

func TestNewJSONReporter(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewJSONReporter(&buf, true)

	if reporter == nil {
		t.Fatal("NewJSONReporter returned nil")
	}
	if reporter.out != &buf {
		t.Error("Reporter output not set correctly")
	}
	if reporter.pretty != true {
		t.Error("Reporter pretty not set correctly")
	}
}

func TestJSONReporter_Report(t *testing.T) {
	t.Run("basic JSON report", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewJSONReporter(&buf, false)
		result := createTestResult()

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		// Verify valid JSON
		var parsed jsonResult
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("Output is not valid JSON: %v", err)
		}

		if parsed.Name != "test_component" {
			t.Errorf("Name = %s, want test_component", parsed.Name)
		}
		if parsed.Iterations != 100 {
			t.Errorf("Iterations = %d, want 100", parsed.Iterations)
		}
	})

	t.Run("pretty JSON report", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewJSONReporter(&buf, true)
		result := createTestResult()

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		output := buf.String()
		// Pretty JSON should have newlines and indentation
		if !strings.Contains(output, "\n  ") {
			t.Error("Pretty JSON should have indentation")
		}
	})

	t.Run("JSON with memory stats", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewJSONReporter(&buf, false)
		result := createTestResult()

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		var parsed jsonResult
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("Output is not valid JSON: %v", err)
		}

		if parsed.Memory == nil {
			t.Error("Memory should be present")
		}
		if parsed.Memory.HeapAllocBefore != 1024*1024 {
			t.Errorf("HeapAllocBefore = %d, want %d", parsed.Memory.HeapAllocBefore, 1024*1024)
		}
	})

	t.Run("JSON without memory stats", func(t *testing.T) {
		var buf bytes.Buffer
		reporter := NewJSONReporter(&buf, false)
		result := createTestResult()
		result.Memory = nil

		err := reporter.Report(result)
		if err != nil {
			t.Fatalf("Report failed: %v", err)
		}

		var parsed jsonResult
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("Output is not valid JSON: %v", err)
		}

		if parsed.Memory != nil {
			t.Error("Memory should be nil")
		}
	})
}

func TestJSONReporter_ReportComparison(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewJSONReporter(&buf, false)
	comparison := createTestComparison()

	err := reporter.ReportComparison(comparison)
	if err != nil {
		t.Fatalf("ReportComparison failed: %v", err)
	}

	// Parse the JSON
	var parsed struct {
		Results            map[string]jsonResult `json:"results"`
		Winner             string                `json:"winner"`
		Speedup            float64               `json:"speedup"`
		Significant        bool                  `json:"significant"`
		PValue             float64               `json:"p_value"`
		EffectSizeCategory string                `json:"effect_size_category"`
		Ranking            []string              `json:"ranking"`
	}

	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if parsed.Winner != "fast" {
		t.Errorf("Winner = %s, want fast", parsed.Winner)
	}
	if parsed.Speedup != 10.0 {
		t.Errorf("Speedup = %f, want 10.0", parsed.Speedup)
	}
	if !parsed.Significant {
		t.Error("Significant should be true")
	}
	if len(parsed.Results) != 2 {
		t.Errorf("Results count = %d, want 2", len(parsed.Results))
	}
	if len(parsed.Ranking) != 2 {
		t.Errorf("Ranking count = %d, want 2", len(parsed.Ranking))
	}
	if parsed.EffectSizeCategory != "large" {
		t.Errorf("EffectSizeCategory = %s, want large", parsed.EffectSizeCategory)
	}
}

func TestJSONReporter_ReportAll(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewJSONReporter(&buf, false)

	results := []*Result{
		{Name: "component_1", Iterations: 100},
		{Name: "component_2", Iterations: 200},
	}

	err := reporter.ReportAll(results)
	if err != nil {
		t.Fatalf("ReportAll failed: %v", err)
	}

	// Parse as array
	var parsed []jsonResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON array: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("Array length = %d, want 2", len(parsed))
	}
	if parsed[0].Name != "component_1" {
		t.Errorf("First name = %s, want component_1", parsed[0].Name)
	}
	if parsed[1].Name != "component_2" {
		t.Errorf("Second name = %s, want component_2", parsed[1].Name)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.expected {
				t.Errorf("formatBytes(%d) = %s, want %s", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestFormatBytesDelta(t *testing.T) {
	tests := []struct {
		delta    int64
		expected string
	}{
		{0, "+0 B"},
		{1024, "+1.0 KB"},
		{-1024, "-1.0 KB"},
		{1024 * 1024, "+1.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatBytesDelta(tt.delta)
			if got != tt.expected {
				t.Errorf("formatBytesDelta(%d) = %s, want %s", tt.delta, got, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"exactly11ch", 10, "exactly..."},
		{"this is a very long string", 10, "this is..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
			}
		})
	}
}

func TestReporterInterface(t *testing.T) {
	// Verify both reporters implement the interface
	var _ Reporter = (*ConsoleReporter)(nil)
	var _ Reporter = (*JSONReporter)(nil)
}
