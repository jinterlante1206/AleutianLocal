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
Package diagnostics_test contains tests for Distributed Health Agent interfaces and types.

# Testing Strategy

These tests verify:
  - Interface method signatures are correct
  - Type helper methods work as expected
  - Default values are properly applied
  - Constants have expected values
*/
package diagnostics

import (
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Constants Tests
// -----------------------------------------------------------------------------

// TestConstants verifies constant values are as expected.
func TestDiagnosticsConstants(t *testing.T) {
	if DefaultRetentionDays != 30 {
		t.Errorf("DefaultRetentionDays = %d, want 30", DefaultRetentionDays)
	}

	if DefaultContainerLogLines != 50 {
		t.Errorf("DefaultContainerLogLines = %d, want 50", DefaultContainerLogLines)
	}

	if DiagnosticsVersion != "1.0.0" {
		t.Errorf("DiagnosticsVersion = %q, want %q", DiagnosticsVersion, "1.0.0")
	}
}

// -----------------------------------------------------------------------------
// DiagnosticsSeverity Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsSeverity_IsValid verifies severity validation.
func TestDiagnosticsSeverity_IsValid(t *testing.T) {
	tests := []struct {
		severity DiagnosticsSeverity
		valid    bool
	}{
		{SeverityInfo, true},
		{SeverityWarning, true},
		{SeverityError, true},
		{SeverityCritical, true},
		{DiagnosticsSeverity("unknown"), false},
		{DiagnosticsSeverity(""), false},
		{DiagnosticsSeverity("INFO"), false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if got := tt.severity.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// TestDiagnosticsSeverity_String verifies string conversion.
func TestDiagnosticsSeverity_String(t *testing.T) {
	tests := []struct {
		severity DiagnosticsSeverity
		expected string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{SeverityCritical, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// CollectOptions Tests
// -----------------------------------------------------------------------------

// TestCollectOptions_WithDefaults verifies default value application.
func TestCollectOptions_WithDefaults(t *testing.T) {
	t.Run("empty options get defaults", func(t *testing.T) {
		opts := CollectOptions{}.WithDefaults()

		if opts.Severity != SeverityInfo {
			t.Errorf("Severity = %q, want %q", opts.Severity, SeverityInfo)
		}
		if opts.ContainerLogLines != DefaultContainerLogLines {
			t.Errorf("ContainerLogLines = %d, want %d", opts.ContainerLogLines, DefaultContainerLogLines)
		}
		if opts.Tags == nil {
			t.Error("Tags should not be nil after WithDefaults")
		}
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		opts := CollectOptions{
			Reason:               "test",
			Severity:             SeverityError,
			ContainerLogLines:    100,
			IncludeContainerLogs: true,
			Tags:                 map[string]string{"key": "value"},
		}.WithDefaults()

		if opts.Reason != "test" {
			t.Errorf("Reason = %q, want %q", opts.Reason, "test")
		}
		if opts.Severity != SeverityError {
			t.Errorf("Severity = %q, want %q", opts.Severity, SeverityError)
		}
		if opts.ContainerLogLines != 100 {
			t.Errorf("ContainerLogLines = %d, want %d", opts.ContainerLogLines, 100)
		}
		if !opts.IncludeContainerLogs {
			t.Error("IncludeContainerLogs should be true")
		}
		if opts.Tags["key"] != "value" {
			t.Errorf("Tags[key] = %q, want %q", opts.Tags["key"], "value")
		}
	})

	t.Run("invalid severity gets default", func(t *testing.T) {
		opts := CollectOptions{
			Severity: DiagnosticsSeverity("invalid"),
		}.WithDefaults()

		if opts.Severity != SeverityInfo {
			t.Errorf("Severity = %q, want %q", opts.Severity, SeverityInfo)
		}
	})

	t.Run("zero log lines gets default", func(t *testing.T) {
		opts := CollectOptions{
			ContainerLogLines: 0,
		}.WithDefaults()

		if opts.ContainerLogLines != DefaultContainerLogLines {
			t.Errorf("ContainerLogLines = %d, want %d", opts.ContainerLogLines, DefaultContainerLogLines)
		}
	})

	t.Run("negative log lines gets default", func(t *testing.T) {
		opts := CollectOptions{
			ContainerLogLines: -10,
		}.WithDefaults()

		if opts.ContainerLogLines != DefaultContainerLogLines {
			t.Errorf("ContainerLogLines = %d, want %d", opts.ContainerLogLines, DefaultContainerLogLines)
		}
	})
}

// -----------------------------------------------------------------------------
// DiagnosticsResult Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsResult_Timestamp verifies timestamp conversion.
func TestDiagnosticsResult_Timestamp(t *testing.T) {
	now := time.Now()
	result := &DiagnosticsResult{
		TimestampMs: now.UnixMilli(),
	}

	got := result.Timestamp()
	// Allow 1ms tolerance
	if got.Sub(now).Abs() > time.Millisecond {
		t.Errorf("Timestamp() differs by more than 1ms from original")
	}
}

// TestDiagnosticsResult_Duration verifies duration conversion.
func TestDiagnosticsResult_Duration(t *testing.T) {
	result := &DiagnosticsResult{
		DurationMs: 1500,
	}

	expected := 1500 * time.Millisecond
	if got := result.Duration(); got != expected {
		t.Errorf("Duration() = %v, want %v", got, expected)
	}
}

// TestDiagnosticsResult_IsSuccess verifies success detection.
func TestDiagnosticsResult_IsSuccess(t *testing.T) {
	t.Run("success when no error", func(t *testing.T) {
		result := &DiagnosticsResult{Error: ""}
		if !result.IsSuccess() {
			t.Error("IsSuccess() = false, want true")
		}
	})

	t.Run("failure when error present", func(t *testing.T) {
		result := &DiagnosticsResult{Error: "something failed"}
		if result.IsSuccess() {
			t.Error("IsSuccess() = true, want false")
		}
	})
}

// -----------------------------------------------------------------------------
// DiagnosticsHeader Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsHeader_Timestamp verifies header timestamp conversion.
func TestDiagnosticsHeader_Timestamp(t *testing.T) {
	now := time.Now()
	header := &DiagnosticsHeader{
		TimestampMs: now.UnixMilli(),
	}

	got := header.Timestamp()
	if got.Sub(now).Abs() > time.Millisecond {
		t.Errorf("Timestamp() differs by more than 1ms from original")
	}
}

// -----------------------------------------------------------------------------
// ListOptions Tests
// -----------------------------------------------------------------------------

// TestListOptions_WithDefaults verifies default value application.
func TestListOptions_WithDefaults(t *testing.T) {
	t.Run("empty options get defaults", func(t *testing.T) {
		opts := ListOptions{}.WithDefaults()

		if opts.Limit != 20 {
			t.Errorf("Limit = %d, want 20", opts.Limit)
		}
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		opts := ListOptions{
			Limit:    50,
			Offset:   10,
			Severity: SeverityError,
		}.WithDefaults()

		if opts.Limit != 50 {
			t.Errorf("Limit = %d, want 50", opts.Limit)
		}
		if opts.Offset != 10 {
			t.Errorf("Offset = %d, want 10", opts.Offset)
		}
		if opts.Severity != SeverityError {
			t.Errorf("Severity = %q, want %q", opts.Severity, SeverityError)
		}
	})

	t.Run("limit capped at 100", func(t *testing.T) {
		opts := ListOptions{Limit: 500}.WithDefaults()

		if opts.Limit != 100 {
			t.Errorf("Limit = %d, want 100 (capped)", opts.Limit)
		}
	})

	t.Run("zero limit gets default", func(t *testing.T) {
		opts := ListOptions{Limit: 0}.WithDefaults()

		if opts.Limit != 20 {
			t.Errorf("Limit = %d, want 20", opts.Limit)
		}
	})

	t.Run("negative limit gets default", func(t *testing.T) {
		opts := ListOptions{Limit: -5}.WithDefaults()

		if opts.Limit != 20 {
			t.Errorf("Limit = %d, want 20", opts.Limit)
		}
	})
}

// -----------------------------------------------------------------------------
// DiagnosticsSummary Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsSummary_Timestamp verifies summary timestamp conversion.
func TestDiagnosticsSummary_Timestamp(t *testing.T) {
	now := time.Now()
	summary := &DiagnosticsSummary{
		TimestampMs: now.UnixMilli(),
	}

	got := summary.Timestamp()
	if got.Sub(now).Abs() > time.Millisecond {
		t.Errorf("Timestamp() differs by more than 1ms from original")
	}
}

// -----------------------------------------------------------------------------
// Type Field Tests
// -----------------------------------------------------------------------------

// TestDiagnosticsData_Fields verifies DiagnosticsData structure.
func TestDiagnosticsData_Fields(t *testing.T) {
	data := DiagnosticsData{
		Header: DiagnosticsHeader{
			Version:     DiagnosticsVersion,
			TimestampMs: time.Now().UnixMilli(),
			TraceID:     "trace123",
			SpanID:      "span456",
			Reason:      "test",
			Details:     "test details",
			Severity:    SeverityInfo,
		},
		System: SystemInfo{
			OS:        "darwin",
			Arch:      "arm64",
			Hostname:  "test.local",
			GoVersion: "go1.21.0",
		},
		Podman: PodmanInfo{
			Version:   "4.8.0",
			Available: true,
		},
		Tags: map[string]string{"env": "test"},
	}

	if data.Header.Version != DiagnosticsVersion {
		t.Errorf("Header.Version = %q, want %q", data.Header.Version, DiagnosticsVersion)
	}
	if data.System.OS != "darwin" {
		t.Errorf("System.OS = %q, want %q", data.System.OS, "darwin")
	}
	if !data.Podman.Available {
		t.Error("Podman.Available should be true")
	}
	if data.Tags["env"] != "test" {
		t.Errorf("Tags[env] = %q, want %q", data.Tags["env"], "test")
	}
}

// TestContainerInfo_Fields verifies ContainerInfo structure.
func TestContainerInfo_Fields(t *testing.T) {
	info := ContainerInfo{
		ID:          "abc123",
		Name:        "aleutian-go-orchestrator",
		State:       "running",
		Image:       "aleutian/orchestrator:latest",
		ServiceType: "orchestrator",
		Health:      "healthy",
		CreatedAt:   time.Now().UnixMilli(),
		StartedAt:   time.Now().UnixMilli(),
	}

	if info.Name != "aleutian-go-orchestrator" {
		t.Errorf("Name = %q, want %q", info.Name, "aleutian-go-orchestrator")
	}
	if info.ServiceType != "orchestrator" {
		t.Errorf("ServiceType = %q, want %q", info.ServiceType, "orchestrator")
	}
}

// TestStorageMetadata_Fields verifies StorageMetadata structure.
func TestStorageMetadata_Fields(t *testing.T) {
	meta := StorageMetadata{
		FilenameHint: "diag-20240105.json",
		ContentType:  "application/json",
		Tags:         map[string]string{"severity": "error"},
	}

	if meta.ContentType != "application/json" {
		t.Errorf("ContentType = %q, want %q", meta.ContentType, "application/json")
	}
	if meta.Tags["severity"] != "error" {
		t.Errorf("Tags[severity] = %q, want %q", meta.Tags["severity"], "error")
	}
}

// -----------------------------------------------------------------------------
// Interface Existence Tests
// -----------------------------------------------------------------------------

// TestInterfaces_Exist verifies all interfaces are defined.
// These tests ensure the interfaces compile and have the expected methods.
func TestInterfaces_Exist(t *testing.T) {
	// This test verifies the interfaces exist and compile.
	// Actual implementations will be tested in their own files.

	t.Run("DiagnosticsCollector interface exists", func(t *testing.T) {
		var _ DiagnosticsCollector = nil
		// Interface exists if this compiles
	})

	t.Run("DiagnosticsFormatter interface exists", func(t *testing.T) {
		var _ DiagnosticsFormatter = nil
	})

	t.Run("DiagnosticsStorage interface exists", func(t *testing.T) {
		var _ DiagnosticsStorage = nil
	})

	t.Run("DiagnosticsMetrics interface exists", func(t *testing.T) {
		var _ DiagnosticsMetrics = nil
	})

	t.Run("DiagnosticsViewer interface exists", func(t *testing.T) {
		var _ DiagnosticsViewer = nil
	})

	t.Run("PanicRecoveryHandler interface exists", func(t *testing.T) {
		var _ PanicRecoveryHandler = nil
	})
}
