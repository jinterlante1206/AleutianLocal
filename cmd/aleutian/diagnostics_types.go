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
Package main provides type definitions for the Distributed Health Agent.

This file contains all data types used by the DiagnosticsCollector and related
interfaces. Types are designed for:

  - JSON serialization for Grafana/Loki ingestion
  - OpenTelemetry trace correlation
  - Prometheus metric labeling
  - GDPR-compliant audit logging
*/
package main

import (
	"time"
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

// DefaultRetentionDays is the default retention period for diagnostics.
// Aligns with GDPR Data Minimization, NIST SP 800-92, ISO 27001, CCPA.
const DefaultRetentionDays = 30

// DefaultContainerLogLines is the default number of log lines per container.
const DefaultContainerLogLines = 50

// DiagnosticsVersion is the current schema version for diagnostic output.
const DiagnosticsVersion = "1.0.0"

// -----------------------------------------------------------------------------
// Severity Types
// -----------------------------------------------------------------------------

// DiagnosticsSeverity indicates the urgency level of a diagnostic collection.
//
// Severity affects:
//   - Prometheus metric labels for alerting
//   - Display priority in viewer/dashboard
//   - Retention behavior (critical may be kept longer)
type DiagnosticsSeverity string

const (
	// SeverityInfo indicates routine diagnostic collection.
	// Example: Manual `aleutian diagnose` for health check.
	SeverityInfo DiagnosticsSeverity = "info"

	// SeverityWarning indicates a recoverable issue was detected.
	// Example: Machine mount drift that was auto-fixed.
	SeverityWarning DiagnosticsSeverity = "warning"

	// SeverityError indicates an operation failed.
	// Example: Stack startup failed with compose error.
	SeverityError DiagnosticsSeverity = "error"

	// SeverityCritical indicates a crash or data loss scenario.
	// Example: Panic recovery captured diagnostics.
	SeverityCritical DiagnosticsSeverity = "critical"
)

// IsValid returns true if the severity is a known value.
//
// # Examples
//
//	if !severity.IsValid() {
//	    severity = SeverityInfo
//	}
func (s DiagnosticsSeverity) IsValid() bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityError, SeverityCritical:
		return true
	default:
		return false
	}
}

// String returns the string representation of the severity.
func (s DiagnosticsSeverity) String() string {
	return string(s)
}

// -----------------------------------------------------------------------------
// Collection Options
// -----------------------------------------------------------------------------

// CollectOptions configures a diagnostic collection operation.
//
// All fields have sensible defaults; only Reason is required for meaningful
// diagnostics.
type CollectOptions struct {
	// Reason describes why diagnostics are being collected.
	// Used for Prometheus labels and filtering.
	// Examples: "startup_failure", "manual_request", "machine_drift"
	Reason string

	// Details provides additional context about the situation.
	// Included in diagnostic output but not used for labeling.
	Details string

	// Severity indicates urgency level.
	// Default: SeverityInfo
	Severity DiagnosticsSeverity

	// IncludeContainerLogs enables container log collection.
	// Default: false (logs can be large)
	IncludeContainerLogs bool

	// ContainerLogLines limits log lines per container.
	// Default: 50
	ContainerLogLines int

	// IncludeSystemMetrics enables system resource metrics (CPU, memory, disk).
	// Default: false
	IncludeSystemMetrics bool

	// Tags for categorization and filtering.
	// Added to storage metadata and JSON output.
	// Example: {"component": "stack", "environment": "development"}
	Tags map[string]string
}

// WithDefaults returns a copy of options with defaults applied.
//
// # Description
//
// Ensures all optional fields have sensible values.
//
// # Examples
//
//	opts := CollectOptions{Reason: "test"}.WithDefaults()
//	// opts.Severity == SeverityInfo
//	// opts.ContainerLogLines == 50
func (o CollectOptions) WithDefaults() CollectOptions {
	if !o.Severity.IsValid() {
		o.Severity = SeverityInfo
	}
	if o.ContainerLogLines <= 0 {
		o.ContainerLogLines = DefaultContainerLogLines
	}
	if o.Tags == nil {
		o.Tags = make(map[string]string)
	}
	return o
}

// -----------------------------------------------------------------------------
// Collection Result
// -----------------------------------------------------------------------------

// DiagnosticsResult contains the outcome of a collection operation.
//
// This is returned by DiagnosticsCollector.Collect and contains all
// information needed for support tickets and debugging.
type DiagnosticsResult struct {
	// Location is the storage path or URI where diagnostics were saved.
	// Format depends on storage backend:
	//   - File: "/path/to/diag-20240105-100000.json"
	//   - GCS: "gs://bucket/diagnostics/diag-xxx.json"
	//   - Loki: "loki://stream-id"
	Location string

	// TraceID is the OpenTelemetry trace ID for Jaeger correlation.
	// This is the "Support Ticket Revolution" - users provide this ID
	// instead of pasting 500 lines of logs.
	TraceID string

	// SpanID is the OpenTelemetry span ID for this specific collection.
	SpanID string

	// TimestampMs is when collection started (Unix milliseconds).
	TimestampMs int64

	// DurationMs is how long collection took (milliseconds).
	DurationMs int64

	// Format is the output format used ("json" or "text").
	Format string

	// SizeBytes is the size of the formatted output.
	SizeBytes int64

	// Error contains error message if collection partially failed.
	// Empty string if fully successful.
	Error string
}

// Timestamp returns the collection start time as time.Time.
func (r *DiagnosticsResult) Timestamp() time.Time {
	return time.UnixMilli(r.TimestampMs)
}

// Duration returns the collection duration as time.Duration.
func (r *DiagnosticsResult) Duration() time.Duration {
	return time.Duration(r.DurationMs) * time.Millisecond
}

// IsSuccess returns true if collection completed without critical errors.
func (r *DiagnosticsResult) IsSuccess() bool {
	return r.Error == ""
}

// -----------------------------------------------------------------------------
// Diagnostic Data Structures
// -----------------------------------------------------------------------------

// DiagnosticsData contains all collected diagnostic information.
//
// This is the primary data structure passed to DiagnosticsFormatter.
// Designed for JSON serialization with Grafana/Loki compatibility.
type DiagnosticsData struct {
	// Header contains metadata about the collection.
	Header DiagnosticsHeader `json:"header"`

	// System contains OS and architecture information.
	System SystemInfo `json:"system"`

	// Podman contains Podman/container state.
	Podman PodmanInfo `json:"podman"`

	// ContainerLogs contains logs from each container.
	// Only populated if CollectOptions.IncludeContainerLogs is true.
	ContainerLogs []ContainerLog `json:"container_logs,omitempty"`

	// Metrics contains system resource usage.
	// Only populated if CollectOptions.IncludeSystemMetrics is true.
	Metrics *SystemMetrics `json:"metrics,omitempty"`

	// Tags are custom key-value pairs for categorization.
	Tags map[string]string `json:"tags,omitempty"`
}

// DiagnosticsHeader contains metadata about the collection.
//
// Fields are designed for indexing and filtering in Grafana/Loki.
type DiagnosticsHeader struct {
	// Version is the diagnostic schema version.
	Version string `json:"version"`

	// TimestampMs is when collection started (Unix milliseconds).
	TimestampMs int64 `json:"timestamp_ms"`

	// TraceID is the OpenTelemetry trace ID for Jaeger correlation.
	TraceID string `json:"trace_id"`

	// SpanID is the OpenTelemetry span ID for this collection.
	SpanID string `json:"span_id"`

	// Reason describes why diagnostics were collected.
	Reason string `json:"reason"`

	// Details provides additional context.
	Details string `json:"details"`

	// Severity indicates the urgency level.
	Severity DiagnosticsSeverity `json:"severity"`

	// DurationMs is how long collection took.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// Timestamp returns the header timestamp as time.Time.
func (h *DiagnosticsHeader) Timestamp() time.Time {
	return time.UnixMilli(h.TimestampMs)
}

// SystemInfo contains OS and runtime information.
type SystemInfo struct {
	// OS is the operating system (e.g., "darwin", "linux", "windows").
	OS string `json:"os"`

	// Arch is the CPU architecture (e.g., "amd64", "arm64").
	Arch string `json:"arch"`

	// Hostname is the machine hostname.
	Hostname string `json:"hostname"`

	// GoVersion is the Go runtime version.
	GoVersion string `json:"go_version"`

	// AleutianVersion is the Aleutian CLI version.
	AleutianVersion string `json:"aleutian_version,omitempty"`
}

// PodmanInfo contains Podman state information.
type PodmanInfo struct {
	// Version is the Podman version string.
	Version string `json:"version"`

	// Available indicates if Podman is installed and accessible.
	Available bool `json:"available"`

	// MachineList contains Podman machines (macOS/Windows only).
	MachineList []MachineInfo `json:"machines,omitempty"`

	// Containers contains running/stopped Aleutian containers.
	Containers []ContainerInfo `json:"containers,omitempty"`

	// Error contains error message if Podman commands failed.
	Error string `json:"error,omitempty"`
}

// MachineInfo contains Podman machine state (macOS/Windows).
type MachineInfo struct {
	// Name is the machine identifier.
	Name string `json:"name"`

	// State is the machine state ("running", "stopped", etc.).
	State string `json:"state"`

	// CPUs is the number of allocated CPUs.
	CPUs int `json:"cpus"`

	// MemoryMB is the allocated memory in megabytes.
	MemoryMB int64 `json:"memory_mb"`

	// DiskGB is the allocated disk space in gigabytes.
	DiskGB int64 `json:"disk_gb,omitempty"`

	// Mounts lists mounted volumes.
	Mounts []string `json:"mounts,omitempty"`
}

// ContainerInfo contains information about a single container.
type ContainerInfo struct {
	// ID is the container ID (short form).
	ID string `json:"id"`

	// Name is the container name.
	Name string `json:"name"`

	// State is the container state ("running", "exited", etc.).
	State string `json:"state"`

	// Image is the container image name.
	Image string `json:"image"`

	// ServiceType categorizes the service (for Prometheus labels).
	// Examples: "orchestrator", "vectordb", "rag", "embedding"
	ServiceType string `json:"service_type,omitempty"`

	// Health is the health check status ("healthy", "unhealthy", "none").
	Health string `json:"health,omitempty"`

	// CreatedAt is when the container was created (Unix milliseconds).
	CreatedAt int64 `json:"created_at,omitempty"`

	// StartedAt is when the container started (Unix milliseconds).
	StartedAt int64 `json:"started_at,omitempty"`
}

// ContainerLog contains logs from a single container.
type ContainerLog struct {
	// Name is the container name.
	Name string `json:"name"`

	// Logs is the captured log content.
	Logs string `json:"logs"`

	// LineCount is the number of lines captured.
	LineCount int `json:"line_count"`

	// Truncated indicates if logs were cut off.
	Truncated bool `json:"truncated,omitempty"`

	// Error contains error message if log collection failed.
	Error string `json:"error,omitempty"`
}

// SystemMetrics contains resource usage information.
type SystemMetrics struct {
	// CPUUsagePercent is the current CPU usage (0-100+).
	CPUUsagePercent float64 `json:"cpu_usage_percent"`

	// MemoryUsedMB is the used memory in megabytes.
	MemoryUsedMB int64 `json:"memory_used_mb"`

	// MemoryTotalMB is the total system memory in megabytes.
	MemoryTotalMB int64 `json:"memory_total_mb"`

	// MemoryPercent is the memory usage percentage.
	MemoryPercent float64 `json:"memory_percent"`

	// DiskUsedGB is the used disk space in gigabytes.
	DiskUsedGB int64 `json:"disk_used_gb"`

	// DiskTotalGB is the total disk space in gigabytes.
	DiskTotalGB int64 `json:"disk_total_gb"`

	// DiskPercent is the disk usage percentage.
	DiskPercent float64 `json:"disk_percent"`
}

// -----------------------------------------------------------------------------
// Storage Types
// -----------------------------------------------------------------------------

// StorageMetadata provides hints for storage operations.
type StorageMetadata struct {
	// FilenameHint is the suggested filename (without path).
	// Storage backend may modify this (e.g., add timestamp).
	FilenameHint string

	// ContentType is the MIME type of the data.
	// Examples: "application/json", "text/plain"
	ContentType string

	// Tags are key-value pairs for organization/filtering.
	// May be stored as object metadata (GCS) or labels (Loki).
	Tags map[string]string
}

// -----------------------------------------------------------------------------
// Viewer Types
// -----------------------------------------------------------------------------

// ListOptions configures diagnostic listing.
type ListOptions struct {
	// Limit is the maximum results to return.
	// Default: 20, Max: 100
	Limit int

	// Offset is the number of results to skip (for pagination).
	Offset int

	// Severity filters to this severity level.
	// Empty string returns all severities.
	Severity DiagnosticsSeverity

	// Since filters to diagnostics after this time (Unix milliseconds).
	// Zero returns all.
	Since int64

	// Until filters to diagnostics before this time (Unix milliseconds).
	// Zero returns all.
	Until int64

	// Reason filters to this reason string.
	// Empty string returns all.
	Reason string
}

// WithDefaults returns a copy with defaults applied.
func (o ListOptions) WithDefaults() ListOptions {
	if o.Limit <= 0 {
		o.Limit = 20
	}
	if o.Limit > 100 {
		o.Limit = 100
	}
	return o
}

// DiagnosticsSummary contains metadata for listing without full content.
type DiagnosticsSummary struct {
	// ID is a unique identifier for the diagnostic.
	ID string `json:"id"`

	// TraceID is the OpenTelemetry trace ID.
	TraceID string `json:"trace_id"`

	// TimestampMs is when collection started (Unix milliseconds).
	TimestampMs int64 `json:"timestamp_ms"`

	// Reason describes why diagnostics were collected.
	Reason string `json:"reason"`

	// Severity indicates the urgency level.
	Severity DiagnosticsSeverity `json:"severity"`

	// Location is the storage path or URI.
	Location string `json:"location"`

	// SizeBytes is the size of the diagnostic file.
	SizeBytes int64 `json:"size_bytes"`
}

// Timestamp returns the summary timestamp as time.Time.
func (s *DiagnosticsSummary) Timestamp() time.Time {
	return time.UnixMilli(s.TimestampMs)
}
