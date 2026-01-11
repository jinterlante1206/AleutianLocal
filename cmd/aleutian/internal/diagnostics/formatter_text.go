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
Package diagnostics provides TextDiagnosticsFormatter for human-readable output.

Text format is the secondary output format for diagnostics, designed for:

  - Interactive terminal display
  - Quick inspection by developers
  - Email/chat sharing when JSON is overkill
  - Accessibility when tools don't support JSON

The text output follows a consistent structure with clear section headers,
making it easy to scan for relevant information.
*/
package diagnostics

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// TextDiagnosticsFormatter Implementation
// -----------------------------------------------------------------------------

// TextDiagnosticsFormatter converts diagnostic data to human-readable text.
//
// This formatter produces structured plain text with clear section headers,
// suitable for terminal display and quick inspection.
//
// # Output Characteristics
//
//   - UTF-8 encoded plain text
//   - Section headers with === delimiters
//   - Timestamps in RFC3339 format for readability
//   - Bullet points for lists
//
// # Thread Safety
//
// TextDiagnosticsFormatter is stateless and safe for concurrent use.
type TextDiagnosticsFormatter struct{}

// NewTextDiagnosticsFormatter creates a text formatter.
//
// # Description
//
// Creates a formatter that produces human-readable text output.
// Use this for interactive terminal display or when sharing via chat/email.
//
// # Outputs
//
//   - *TextDiagnosticsFormatter: Ready-to-use formatter
//
// # Examples
//
//	formatter := NewTextDiagnosticsFormatter()
//	output, err := formatter.Format(data)
//	// output is readable plain text
func NewTextDiagnosticsFormatter() *TextDiagnosticsFormatter {
	return &TextDiagnosticsFormatter{}
}

// Format converts diagnostic data to human-readable text.
//
// # Description
//
// Produces structured plain text with clear section headers and formatted
// data. Each major data category gets its own section.
//
// # Inputs
//
//   - data: Collected diagnostic information to format
//
// # Outputs
//
//   - []byte: Plain text diagnostic output
//   - error: Always nil (text formatting cannot fail)
//
// # Examples
//
//	data := &DiagnosticsData{...}
//	output, err := formatter.Format(data)
//	fmt.Println(string(output))
//	// === Aleutian Diagnostics ===
//	// Version: 1.0.0
//	// ...
//
// # Limitations
//
//   - Less structured than JSON for automation
//   - Large container logs may be truncated for display
//
// # Assumptions
//
//   - Input data is valid and has all required fields populated
func (f *TextDiagnosticsFormatter) Format(data *DiagnosticsData) ([]byte, error) {
	if data == nil {
		return []byte("(no diagnostic data)\n"), nil
	}

	var buf bytes.Buffer

	f.writeHeader(&buf, &data.Header)
	f.writeSystemInfo(&buf, &data.System)
	f.writePodmanInfo(&buf, &data.Podman)

	if len(data.ContainerLogs) > 0 {
		f.writeContainerLogs(&buf, data.ContainerLogs)
	}

	if data.Metrics != nil {
		f.writeMetrics(&buf, data.Metrics)
	}

	if len(data.Tags) > 0 {
		f.writeTags(&buf, data.Tags)
	}

	return buf.Bytes(), nil
}

// writeHeader writes the diagnostic header section.
func (f *TextDiagnosticsFormatter) writeHeader(buf *bytes.Buffer, h *DiagnosticsHeader) {
	buf.WriteString("=== Aleutian Diagnostics ===\n")
	fmt.Fprintf(buf, "Version: %s\n", h.Version)
	fmt.Fprintf(buf, "Timestamp: %s\n", time.UnixMilli(h.TimestampMs).Format(time.RFC3339))

	if h.TraceID != "" {
		fmt.Fprintf(buf, "Trace ID: %s\n", h.TraceID)
	}
	if h.SpanID != "" {
		fmt.Fprintf(buf, "Span ID: %s\n", h.SpanID)
	}

	fmt.Fprintf(buf, "Reason: %s\n", h.Reason)
	if h.Details != "" {
		fmt.Fprintf(buf, "Details: %s\n", h.Details)
	}
	fmt.Fprintf(buf, "Severity: %s\n", h.Severity)

	if h.DurationMs > 0 {
		fmt.Fprintf(buf, "Duration: %dms\n", h.DurationMs)
	}

	buf.WriteString("\n")
}

// writeSystemInfo writes the system information section.
func (f *TextDiagnosticsFormatter) writeSystemInfo(buf *bytes.Buffer, s *SystemInfo) {
	buf.WriteString("=== System Info ===\n")
	fmt.Fprintf(buf, "OS: %s\n", s.OS)
	fmt.Fprintf(buf, "Arch: %s\n", s.Arch)
	fmt.Fprintf(buf, "Hostname: %s\n", s.Hostname)
	fmt.Fprintf(buf, "Go Version: %s\n", s.GoVersion)

	if s.AleutianVersion != "" {
		fmt.Fprintf(buf, "Aleutian Version: %s\n", s.AleutianVersion)
	}

	buf.WriteString("\n")
}

// writePodmanInfo writes the Podman information section.
func (f *TextDiagnosticsFormatter) writePodmanInfo(buf *bytes.Buffer, p *PodmanInfo) {
	buf.WriteString("=== Podman Info ===\n")

	if !p.Available {
		buf.WriteString("Status: NOT AVAILABLE\n")
		if p.Error != "" {
			fmt.Fprintf(buf, "Error: %s\n", p.Error)
		}
		buf.WriteString("\n")
		return
	}

	fmt.Fprintf(buf, "Version: %s\n", p.Version)

	if len(p.MachineList) > 0 {
		buf.WriteString("\nMachines:\n")
		for _, m := range p.MachineList {
			fmt.Fprintf(buf, "  - %s (%s, %d CPUs, %d MB)\n",
				m.Name, m.State, m.CPUs, m.MemoryMB)
			if len(m.Mounts) > 0 {
				fmt.Fprintf(buf, "    Mounts: %s\n", strings.Join(m.Mounts, ", "))
			}
		}
	}

	if len(p.Containers) > 0 {
		buf.WriteString("\nContainers:\n")
		for _, c := range p.Containers {
			status := c.State
			if c.Health != "" && c.Health != "none" {
				status = fmt.Sprintf("%s, %s", c.State, c.Health)
			}
			fmt.Fprintf(buf, "  - %s (%s)\n", c.Name, status)
			if c.ServiceType != "" {
				fmt.Fprintf(buf, "    Service Type: %s\n", c.ServiceType)
			}
		}
	}

	if p.Error != "" {
		fmt.Fprintf(buf, "\nError: %s\n", p.Error)
	}

	buf.WriteString("\n")
}

// writeContainerLogs writes the container logs section.
func (f *TextDiagnosticsFormatter) writeContainerLogs(buf *bytes.Buffer, logs []ContainerLog) {
	fmt.Fprintf(buf, "=== Container Logs (last %d lines) ===\n", DefaultContainerLogLines)

	for _, log := range logs {
		fmt.Fprintf(buf, "\n--- %s ---\n", log.Name)

		if log.Error != "" {
			fmt.Fprintf(buf, "Error: %s\n", log.Error)
			continue
		}

		if log.Logs == "" {
			buf.WriteString("(no logs)\n")
			continue
		}

		buf.WriteString(log.Logs)
		if !strings.HasSuffix(log.Logs, "\n") {
			buf.WriteString("\n")
		}

		if log.Truncated {
			buf.WriteString("... (truncated)\n")
		}
	}

	buf.WriteString("\n")
}

// writeMetrics writes the system metrics section.
func (f *TextDiagnosticsFormatter) writeMetrics(buf *bytes.Buffer, m *SystemMetrics) {
	buf.WriteString("=== System Metrics ===\n")
	fmt.Fprintf(buf, "CPU Usage: %.1f%%\n", m.CPUUsagePercent)
	fmt.Fprintf(buf, "Memory: %d MB / %d MB (%.1f%%)\n",
		m.MemoryUsedMB, m.MemoryTotalMB, m.MemoryPercent)
	fmt.Fprintf(buf, "Disk: %d GB / %d GB (%.1f%%)\n",
		m.DiskUsedGB, m.DiskTotalGB, m.DiskPercent)
	buf.WriteString("\n")
}

// writeTags writes the custom tags section.
func (f *TextDiagnosticsFormatter) writeTags(buf *bytes.Buffer, tags map[string]string) {
	buf.WriteString("=== Tags ===\n")
	for k, v := range tags {
		fmt.Fprintf(buf, "%s: %s\n", k, v)
	}
	buf.WriteString("\n")
}

// ContentType returns the MIME type for text format.
//
// # Description
//
// Returns "text/plain" for HTTP headers and storage metadata.
//
// # Outputs
//
//   - string: "text/plain; charset=utf-8"
func (f *TextDiagnosticsFormatter) ContentType() string {
	return "text/plain; charset=utf-8"
}

// FileExtension returns the file extension for text format.
//
// # Description
//
// Returns ".txt" for diagnostic file naming.
//
// # Outputs
//
//   - string: ".txt"
func (f *TextDiagnosticsFormatter) FileExtension() string {
	return ".txt"
}

// Compile-time interface compliance check.
var _ DiagnosticsFormatter = (*TextDiagnosticsFormatter)(nil)
