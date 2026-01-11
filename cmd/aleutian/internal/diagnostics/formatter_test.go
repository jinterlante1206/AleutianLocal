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
Package diagnostics_test contains tests for DiagnosticsFormatter implementations.

# Testing Strategy

These tests verify:
  - JSON formatter produces valid, parseable JSON
  - Text formatter produces readable output with correct sections
  - Both formatters handle nil/empty data gracefully
  - Content types and file extensions are correct
*/
package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Test Data Helpers
// -----------------------------------------------------------------------------

// newTestDiagnosticsData creates a fully populated test data structure.
func newTestDiagnosticsData() *DiagnosticsData {
	now := time.Now().UnixMilli()
	return &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: now,
			TraceID:     "trace123abc",
			SpanID:      "span456def",
			Reason:      "test_reason",
			Details:     "test details here",
			Severity:    SeverityError,
			DurationMs:  1500,
		},
		System: SystemInfo{
			OS:              "darwin",
			Arch:            "arm64",
			Hostname:        "test.local",
			GoVersion:       "go1.21.0",
			AleutianVersion: "0.4.0",
		},
		Podman: PodmanInfo{
			Version:   "4.8.0",
			Available: true,
			MachineList: []MachineInfo{
				{
					Name:     "podman-machine-default",
					State:    "running",
					CPUs:     6,
					MemoryMB: 20480,
					DiskGB:   100,
					Mounts:   []string{"/Users", "/Volumes/External"},
				},
			},
			Containers: []ContainerInfo{
				{
					ID:          "abc123",
					Name:        "aleutian-go-orchestrator",
					State:       "running",
					Image:       "aleutian/orchestrator:latest",
					ServiceType: "orchestrator",
					Health:      "healthy",
				},
				{
					ID:          "def456",
					Name:        "aleutian-weaviate",
					State:       "running",
					Image:       "weaviate/weaviate:latest",
					ServiceType: "vectordb",
					Health:      "healthy",
				},
			},
		},
		ContainerLogs: []ContainerLog{
			{
				Name:      "aleutian-go-orchestrator",
				Logs:      "2024-01-05T10:00:00Z INFO Starting orchestrator...\n2024-01-05T10:00:01Z INFO Ready\n",
				LineCount: 2,
			},
			{
				Name:      "aleutian-weaviate",
				Logs:      "2024-01-05T10:00:00Z INFO Weaviate starting...\n",
				LineCount: 1,
				Truncated: true,
			},
		},
		Metrics: &SystemMetrics{
			CPUUsagePercent: 45.5,
			MemoryUsedMB:    8192,
			MemoryTotalMB:   16384,
			MemoryPercent:   50.0,
			DiskUsedGB:      120,
			DiskTotalGB:     500,
			DiskPercent:     24.0,
		},
		Tags: map[string]string{
			"environment": "development",
			"component":   "stack",
		},
	}
}

// newMinimalTestData creates minimal test data structure.
func newMinimalTestData() *DiagnosticsData {
	return &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			Reason:      "minimal_test",
			Severity:    SeverityInfo,
		},
		System: SystemInfo{
			OS:        "darwin",
			Arch:      "arm64",
			Hostname:  "test.local",
			GoVersion: "go1.21.0",
		},
		Podman: PodmanInfo{
			Available: false,
			Error:     "podman not found",
		},
	}
}

// -----------------------------------------------------------------------------
// JSONDiagnosticsFormatter Tests
// -----------------------------------------------------------------------------

// TestJSONDiagnosticsFormatter_Format_FullData verifies JSON output with all fields.
func TestJSONDiagnosticsFormatter_Format_FullData(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()
	data := newTestDiagnosticsData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	// Verify it's valid JSON
	var parsed DiagnosticsData
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify key fields survived round-trip
	if parsed.Header.Version != DiagnosticsVersion {
		t.Errorf("Header.Version = %q, want %q", parsed.Header.Version, DiagnosticsVersion)
	}
	if parsed.Header.Reason != "test_reason" {
		t.Errorf("Header.Reason = %q, want %q", parsed.Header.Reason, "test_reason")
	}
	if parsed.System.OS != "darwin" {
		t.Errorf("System.OS = %q, want %q", parsed.System.OS, "darwin")
	}
	if len(parsed.Podman.Containers) != 2 {
		t.Errorf("len(Containers) = %d, want 2", len(parsed.Podman.Containers))
	}
	if len(parsed.ContainerLogs) != 2 {
		t.Errorf("len(ContainerLogs) = %d, want 2", len(parsed.ContainerLogs))
	}
	if parsed.Metrics == nil {
		t.Error("Metrics should not be nil")
	}
}

// TestJSONDiagnosticsFormatter_Format_MinimalData verifies JSON output with minimal fields.
func TestJSONDiagnosticsFormatter_Format_MinimalData(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()
	data := newMinimalTestData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	var parsed DiagnosticsData
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if parsed.Header.Reason != "minimal_test" {
		t.Errorf("Header.Reason = %q, want %q", parsed.Header.Reason, "minimal_test")
	}
	if parsed.Podman.Available {
		t.Error("Podman.Available should be false")
	}
}

// TestJSONDiagnosticsFormatter_Format_NilData verifies nil handling.
func TestJSONDiagnosticsFormatter_Format_NilData(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()

	output, err := formatter.Format(nil)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if string(output) != "null" {
		t.Errorf("Format(nil) = %q, want %q", output, "null")
	}
}

// TestJSONDiagnosticsFormatter_Format_Indented verifies indented output.
func TestJSONDiagnosticsFormatter_Format_Indented(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()
	data := newMinimalTestData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	// Indented JSON should have newlines
	if !strings.Contains(string(output), "\n") {
		t.Error("Indented output should contain newlines")
	}

	// Should have 2-space indent
	if !strings.Contains(string(output), "  ") {
		t.Error("Indented output should contain 2-space indents")
	}
}

// TestJSONDiagnosticsFormatter_Format_Compact verifies compact output.
func TestJSONDiagnosticsFormatter_Format_Compact(t *testing.T) {
	formatter := NewCompactJSONDiagnosticsFormatter()
	data := newMinimalTestData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	// Compact JSON should be single line
	if strings.Count(string(output), "\n") > 0 {
		t.Error("Compact output should not contain newlines")
	}
}

// TestJSONDiagnosticsFormatter_ContentType verifies MIME type.
func TestJSONDiagnosticsFormatter_ContentType(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()

	if got := formatter.ContentType(); got != "application/json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/json")
	}
}

// TestJSONDiagnosticsFormatter_FileExtension verifies file extension.
func TestJSONDiagnosticsFormatter_FileExtension(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()

	if got := formatter.FileExtension(); got != ".json" {
		t.Errorf("FileExtension() = %q, want %q", got, ".json")
	}
}

// -----------------------------------------------------------------------------
// TextDiagnosticsFormatter Tests
// -----------------------------------------------------------------------------

// TestTextDiagnosticsFormatter_Format_FullData verifies text output with all fields.
func TestTextDiagnosticsFormatter_Format_FullData(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()
	data := newTestDiagnosticsData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	text := string(output)

	// Verify all sections present
	sections := []string{
		"=== Aleutian Diagnostics ===",
		"=== System Info ===",
		"=== Podman Info ===",
		"=== Container Logs",
		"=== System Metrics ===",
		"=== Tags ===",
	}

	for _, section := range sections {
		if !strings.Contains(text, section) {
			t.Errorf("Output missing section: %q", section)
		}
	}

	// Verify key data present
	expectedContent := []string{
		"Version: 1.0.0",
		"Trace ID: trace123abc",
		"Reason: test_reason",
		"Severity: error",
		"OS: darwin",
		"Arch: arm64",
		"podman-machine-default",
		"aleutian-go-orchestrator",
		"CPU Usage: 45.5%",
		"environment: development",
	}

	for _, content := range expectedContent {
		if !strings.Contains(text, content) {
			t.Errorf("Output missing content: %q", content)
		}
	}
}

// TestTextDiagnosticsFormatter_Format_MinimalData verifies text output with minimal fields.
func TestTextDiagnosticsFormatter_Format_MinimalData(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()
	data := newMinimalTestData()

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	text := string(output)

	// Should have basic sections
	if !strings.Contains(text, "=== Aleutian Diagnostics ===") {
		t.Error("Missing header section")
	}
	if !strings.Contains(text, "=== System Info ===") {
		t.Error("Missing system info section")
	}
	if !strings.Contains(text, "=== Podman Info ===") {
		t.Error("Missing podman info section")
	}

	// Should NOT have optional sections
	if strings.Contains(text, "=== Container Logs") {
		t.Error("Should not have container logs section when empty")
	}
	if strings.Contains(text, "=== System Metrics ===") {
		t.Error("Should not have metrics section when nil")
	}
	if strings.Contains(text, "=== Tags ===") {
		t.Error("Should not have tags section when empty")
	}

	// Should show Podman unavailable
	if !strings.Contains(text, "NOT AVAILABLE") {
		t.Error("Should indicate Podman not available")
	}
}

// TestTextDiagnosticsFormatter_Format_NilData verifies nil handling.
func TestTextDiagnosticsFormatter_Format_NilData(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()

	output, err := formatter.Format(nil)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(string(output), "no diagnostic data") {
		t.Errorf("Format(nil) should indicate no data, got %q", output)
	}
}

// TestTextDiagnosticsFormatter_Format_ContainerLogError verifies error handling in logs.
func TestTextDiagnosticsFormatter_Format_ContainerLogError(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()
	data := &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			Reason:      "test",
			Severity:    SeverityInfo,
		},
		System: SystemInfo{OS: "darwin", Arch: "arm64", Hostname: "test", GoVersion: "go1.21"},
		Podman: PodmanInfo{Available: true, Version: "4.8.0"},
		ContainerLogs: []ContainerLog{
			{
				Name:  "failed-container",
				Error: "container not found",
			},
		},
	}

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(string(output), "Error: container not found") {
		t.Error("Output should include container log error")
	}
}

// TestTextDiagnosticsFormatter_Format_EmptyLogs verifies empty log handling.
func TestTextDiagnosticsFormatter_Format_EmptyLogs(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()
	data := &DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			Reason:      "test",
			Severity:    SeverityInfo,
		},
		System: SystemInfo{OS: "darwin", Arch: "arm64", Hostname: "test", GoVersion: "go1.21"},
		Podman: PodmanInfo{Available: true, Version: "4.8.0"},
		ContainerLogs: []ContainerLog{
			{
				Name: "empty-container",
				Logs: "",
			},
		},
	}

	output, err := formatter.Format(data)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.Contains(string(output), "(no logs)") {
		t.Error("Output should indicate no logs")
	}
}

// TestTextDiagnosticsFormatter_ContentType verifies MIME type.
func TestTextDiagnosticsFormatter_ContentType(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()

	if got := formatter.ContentType(); got != "text/plain; charset=utf-8" {
		t.Errorf("ContentType() = %q, want %q", got, "text/plain; charset=utf-8")
	}
}

// TestTextDiagnosticsFormatter_FileExtension verifies file extension.
func TestTextDiagnosticsFormatter_FileExtension(t *testing.T) {
	formatter := NewTextDiagnosticsFormatter()

	if got := formatter.FileExtension(); got != ".txt" {
		t.Errorf("FileExtension() = %q, want %q", got, ".txt")
	}
}

// -----------------------------------------------------------------------------
// Interface Compliance Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsFormatter_InterfaceCompliance verifies interface implementations.
func TestDiagnosticsFormatter_InterfaceCompliance(t *testing.T) {
	// These will fail to compile if interfaces aren't implemented correctly
	var _ DiagnosticsFormatter = (*JSONDiagnosticsFormatter)(nil)
	var _ DiagnosticsFormatter = (*TextDiagnosticsFormatter)(nil)
}

// -----------------------------------------------------------------------------
// Round-Trip Tests
// -----------------------------------------------------------------------------

// TestJSONDiagnosticsFormatter_RoundTrip verifies data survives JSON round-trip.
func TestJSONDiagnosticsFormatter_RoundTrip(t *testing.T) {
	formatter := NewJSONDiagnosticsFormatter()
	original := newTestDiagnosticsData()

	// Format to JSON
	jsonBytes, err := formatter.Format(original)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	// Parse back
	var parsed DiagnosticsData
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	// Verify critical fields
	if parsed.Header.TraceID != original.Header.TraceID {
		t.Errorf("TraceID mismatch: got %q, want %q", parsed.Header.TraceID, original.Header.TraceID)
	}
	if len(parsed.Podman.Containers) != len(original.Podman.Containers) {
		t.Errorf("Container count mismatch: got %d, want %d",
			len(parsed.Podman.Containers), len(original.Podman.Containers))
	}
	if parsed.Tags["environment"] != original.Tags["environment"] {
		t.Errorf("Tag mismatch: got %q, want %q",
			parsed.Tags["environment"], original.Tags["environment"])
	}
}
