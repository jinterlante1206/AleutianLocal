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
Package diagnostics provides JSONDiagnosticsFormatter for machine-readable output.

JSON format is the primary output format for diagnostics, designed for:

  - Grafana/Loki ingestion and structured queries
  - Machine parsing by automation tools
  - Fleet management via MDM (Jamf/Intune)
  - Enterprise SIEM integration (Splunk, DataDog)

The JSON structure is stable and versioned via the "version" field in the header.
Changes to the schema should increment the version and maintain backward compatibility.
*/
package diagnostics

import (
	"encoding/json"
)

// -----------------------------------------------------------------------------
// JSONDiagnosticsFormatter Implementation
// -----------------------------------------------------------------------------

// JSONDiagnosticsFormatter converts diagnostic data to JSON format.
//
// This is the default formatter, producing machine-readable output suitable
// for Grafana dashboards, Loki queries, and enterprise SIEM systems.
//
// # Output Characteristics
//
//   - Indented JSON for human readability (2 spaces)
//   - UTF-8 encoded
//   - All timestamps in Unix milliseconds
//   - Null/empty fields omitted via `omitempty` tags
//
// # Thread Safety
//
// JSONDiagnosticsFormatter is stateless and safe for concurrent use.
type JSONDiagnosticsFormatter struct {
	// indent controls JSON indentation. Empty string for compact output.
	indent string
}

// NewJSONDiagnosticsFormatter creates a formatter with default settings.
//
// # Description
//
// Creates a JSON formatter that produces indented output (2 spaces).
// This is the recommended formatter for most use cases.
//
// # Outputs
//
//   - *JSONDiagnosticsFormatter: Ready-to-use formatter
//
// # Examples
//
//	formatter := NewJSONDiagnosticsFormatter()
//	output, err := formatter.Format(data)
//	// output is indented JSON
func NewJSONDiagnosticsFormatter() *JSONDiagnosticsFormatter {
	return &JSONDiagnosticsFormatter{
		indent: "  ", // 2-space indent for readability
	}
}

// NewCompactJSONDiagnosticsFormatter creates a formatter without indentation.
//
// # Description
//
// Creates a JSON formatter that produces compact output (no indentation).
// Use this for streaming to Loki or when bandwidth is a concern.
//
// # Outputs
//
//   - *JSONDiagnosticsFormatter: Compact JSON formatter
//
// # Examples
//
//	formatter := NewCompactJSONDiagnosticsFormatter()
//	output, err := formatter.Format(data)
//	// output is single-line JSON
func NewCompactJSONDiagnosticsFormatter() *JSONDiagnosticsFormatter {
	return &JSONDiagnosticsFormatter{
		indent: "", // No indent for compact output
	}
}

// Format converts diagnostic data to JSON format.
//
// # Description
//
// Serializes the DiagnosticsData struct to JSON. Uses standard encoding/json
// with configurable indentation.
//
// # Inputs
//
//   - data: Collected diagnostic information to format
//
// # Outputs
//
//   - []byte: JSON-encoded diagnostic data
//   - error: Non-nil if marshaling fails (rare with valid data)
//
// # Examples
//
//	data := &DiagnosticsData{
//	    Header: DiagnosticsHeader{
//	        Version: DiagnosticsVersion,
//	        Reason:  "test",
//	    },
//	}
//	output, err := formatter.Format(data)
//	// {"header":{"version":"1.0.0","reason":"test",...},...}
//
// # Limitations
//
//   - Large container logs may produce large output
//   - Binary data in logs may not encode cleanly
//
// # Assumptions
//
//   - Input data is valid and has all required fields populated
func (f *JSONDiagnosticsFormatter) Format(data *DiagnosticsData) ([]byte, error) {
	if data == nil {
		return []byte("null"), nil
	}

	if f.indent == "" {
		// Compact output
		return json.Marshal(data)
	}

	// Indented output for readability
	return json.MarshalIndent(data, "", f.indent)
}

// ContentType returns the MIME type for JSON format.
//
// # Description
//
// Returns "application/json" for HTTP headers and storage metadata.
//
// # Outputs
//
//   - string: "application/json"
func (f *JSONDiagnosticsFormatter) ContentType() string {
	return "application/json"
}

// FileExtension returns the file extension for JSON format.
//
// # Description
//
// Returns ".json" for diagnostic file naming.
//
// # Outputs
//
//   - string: ".json"
func (f *JSONDiagnosticsFormatter) FileExtension() string {
	return ".json"
}

// Compile-time interface compliance check.
var _ DiagnosticsFormatter = (*JSONDiagnosticsFormatter)(nil)
